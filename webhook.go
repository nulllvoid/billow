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
		if sub.Status == StatusPastDue || sub.Status == StatusTrialing {
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

// mapEventType converts a provider-native event name to a canonical
// WebhookEventType. Uses Options.EventTypeMapper when set, otherwise
// falls back to the built-in Stripe/Razorpay map. Unknown names pass
// through as-is.
func (m *Manager) mapEventType(providerType string) WebhookEventType {
	if m.eventTypeMapper != nil {
		return m.eventTypeMapper(providerType)
	}
	if t, ok := m.eventTypes[providerType]; ok {
		return t
	}
	return WebhookEventType(providerType)
}

// BuiltinEventTypes returns a fresh copy of the default Stripe and Razorpay
// event-name → WebhookEventType mappings. Use this as a starting point when
// constructing a custom Options.EventTypeMapper:
//
//	m := billow.BuiltinEventTypes()
//	m["paddle.subscription.created"] = billow.EventSubscriptionCreated
//	mgr := billow.NewManager(billow.Options{
//	    EventTypeMapper: func(t string) billow.WebhookEventType {
//	        if v, ok := m[t]; ok { return v }
//	        return billow.WebhookEventType(t)
//	    },
//	})
func BuiltinEventTypes() map[string]WebhookEventType {
	return map[string]WebhookEventType{
		// Stripe
		"customer.subscription.created":       EventSubscriptionCreated,
		"customer.subscription.updated":       EventSubscriptionUpdated,
		"customer.subscription.deleted":       EventSubscriptionCanceled,
		"customer.subscription.paused":        EventSubscriptionPaused,
		"customer.subscription.resumed":       EventSubscriptionResumed,
		"invoice.payment_succeeded":           EventPaymentSucceeded,
		"invoice.payment_failed":              EventPaymentFailed,
		"customer.subscription.trial_will_end": EventTrialEnding,
		// Razorpay
		"subscription.activated": EventSubscriptionCreated,
		"subscription.charged":   EventPaymentSucceeded,
		"subscription.cancelled": EventSubscriptionCanceled,
		"subscription.paused":    EventSubscriptionPaused,
		"subscription.resumed":   EventSubscriptionResumed,
		"subscription.completed": EventSubscriptionCanceled,
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
