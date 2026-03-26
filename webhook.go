package billow

import (
	"fmt"
	"net/http"
	"time"
)

// HandleWebhook validates and processes an incoming webhook from the payment
// provider. It looks up the affected subscription, updates its state, and
// dispatches registered event handlers.
//
// Mount this on your HTTP router:
//
//	http.HandleFunc("/webhooks/payments", func(w http.ResponseWriter, r *http.Request) {
//	    if err := mgr.HandleWebhook(r); err != nil {
//	        http.Error(w, err.Error(), http.StatusBadRequest)
//	        return
//	    }
//	    w.WriteHeader(http.StatusNoContent)
//	})
func (m *Manager) HandleWebhook(r *http.Request) error {
	if err := m.requireProvider(); err != nil {
		return err
	}

	raw, err := m.provider.ParseWebhook(r)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidWebhook, err)
	}

	event := &WebhookEvent{
		ID:            raw.ID,
		ProviderSubID: raw.ProviderSubID,
		Data:          raw.Data,
		OccurredAt:    raw.OccurredAt,
	}

	// Map provider-native event name → canonical WebhookEventType.
	event.Type = m.mapEventType(raw.Type)

	// Look up the local subscription if we have a provider subscription ID.
	if raw.ProviderSubID != "" {
		ss, err := m.subs.GetSubscriptionByProviderID(r.Context(), raw.ProviderSubID)
		if err == nil {
			sub := subFromStore(ss)
			event.Subscription = sub

			// Mutate subscription state based on event type.
			updated := m.applyEventToSubscription(sub, event)
			if updated {
				sub.UpdatedAt = time.Now().UTC()
				if saveErr := m.subs.SaveSubscription(r.Context(), subToStore(sub)); saveErr != nil {
					return saveErr
				}
			}
		}
		// If not found, we still dispatch — the app may handle unknown subs.
	}

	return m.dispatch(event)
}

// applyEventToSubscription updates the subscription's status/fields in place
// based on the canonical event. Returns true when a save is required.
func (m *Manager) applyEventToSubscription(sub *Subscription, event *WebhookEvent) bool {
	switch event.Type {
	case EventSubscriptionCanceled:
		sub.Status = StatusCanceled
		now := time.Now().UTC()
		sub.CanceledAt = &now
		return true

	case EventSubscriptionPaused:
		sub.Status = StatusPaused
		now := time.Now().UTC()
		sub.PausedAt = &now
		return true

	case EventSubscriptionResumed:
		sub.Status = StatusActive
		sub.PausedAt = nil
		return true

	case EventSubscriptionRenewed:
		sub.Status = StatusActive
		// The new period boundaries are typically in the event's Data map.
		// Providers set these differently; extract if present.
		if start, ok := timeFromData(event.Data, "current_period_start"); ok {
			sub.CurrentPeriodStart = start
		}
		if end, ok := timeFromData(event.Data, "current_period_end"); ok {
			sub.CurrentPeriodEnd = end
		}
		return true

	case EventPaymentFailed:
		sub.Status = StatusPastDue
		return true

	case EventPaymentSucceeded:
		if sub.Status == StatusPastDue {
			sub.Status = StatusActive
			return true
		}

	case EventTrialEnded:
		if sub.Status == StatusTrialing {
			sub.Status = StatusActive
			return true
		}
	}
	return false
}

// dispatch calls all registered handlers for the event's type.
func (m *Manager) dispatch(event *WebhookEvent) error {
	for _, h := range m.handlers[event.Type] {
		if err := h(event); err != nil {
			return err
		}
	}
	return nil
}

// mapEventType converts a provider-native event name to a canonical
// WebhookEventType. The default implementation handles common Stripe names;
// override by embedding Manager or extending this method in your own package.
func (m *Manager) mapEventType(providerType string) WebhookEventType {
	switch providerType {
	// Stripe names
	case "customer.subscription.created":
		return EventSubscriptionCreated
	case "customer.subscription.updated":
		return EventSubscriptionUpdated
	case "customer.subscription.deleted":
		return EventSubscriptionCanceled
	case "customer.subscription.paused":
		return EventSubscriptionPaused
	case "customer.subscription.resumed":
		return EventSubscriptionResumed
	case "invoice.payment_succeeded":
		return EventPaymentSucceeded
	case "invoice.payment_failed":
		return EventPaymentFailed
	case "customer.subscription.trial_will_end":
		return EventTrialEnding

	// Razorpay names
	case "subscription.activated":
		return EventSubscriptionCreated
	case "subscription.charged":
		return EventPaymentSucceeded
	case "subscription.cancelled":
		return EventSubscriptionCanceled
	case "subscription.paused":
		return EventSubscriptionPaused
	case "subscription.resumed":
		return EventSubscriptionResumed
	case "subscription.completed":
		return EventSubscriptionCanceled

	default:
		return WebhookEventType(providerType)
	}
}

// timeFromData extracts a Unix timestamp from a webhook data map.
func timeFromData(data map[string]any, key string) (time.Time, bool) {
	v, ok := data[key]
	if !ok {
		return time.Time{}, false
	}
	switch t := v.(type) {
	case int64:
		return time.Unix(t, 0).UTC(), true
	case float64:
		return time.Unix(int64(t), 0).UTC(), true
	}
	return time.Time{}, false
}
