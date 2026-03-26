package billow

import (
	"context"
	"errors"
	"time"

	"github.com/nulllvoid/billow/store"
)

// SubscribeInput is the input for creating a new subscription.
type SubscribeInput struct {
	SubscriberID string            // your user/tenant ID
	PlanID       string
	Metadata     map[string]string
}

// Subscribe creates a new subscription for a subscriber on the given plan.
// Returns ErrAlreadySubscribed if the subscriber already has an active
// or trialing subscription.
//
// When the configured store implements store.AtomicSubscriptionStore, the
// duplicate-subscriber check and the insert are performed atomically,
// eliminating the TOCTOU race. Otherwise a best-effort two-step check is
// used (safe for single-instance deployments).
func (m *Manager) Subscribe(ctx context.Context, in SubscribeInput) (*Subscription, error) {
	sp, err := m.plans.GetPlan(ctx, in.PlanID)
	if err != nil {
		if isNotFound(err) {
			return nil, ErrPlanNotFound
		}
		return nil, err
	}
	plan := planFromStore(sp)

	now := time.Now().UTC()
	status := StatusActive
	var trialStart, trialEnd *time.Time
	if plan.TrialDays > 0 {
		status = StatusTrialing
		ts := now
		te := now.AddDate(0, 0, plan.TrialDays)
		trialStart = &ts
		trialEnd = &te
	}

	sub := &Subscription{
		ID:                 m.newID(),
		SubscriberID:       in.SubscriberID,
		PlanID:             in.PlanID,
		Status:             status,
		CurrentPeriodStart: now,
		CurrentPeriodEnd:   periodEnd(now, plan),
		TrialStart:         trialStart,
		TrialEnd:           trialEnd,
		Metadata:           in.Metadata,
		CreatedAt:          now,
		UpdatedAt:          now,
	}

	if m.provider != nil {
		providerID, err := m.provider.CreateSubscription(ctx, subToProviderSub(sub), planToProviderPlan(plan))
		if err != nil {
			return nil, err
		}
		sub.ProviderID = providerID
	}

	if m.atomicSubs != nil {
		// Atomic path: check + insert under a single store-level lock.
		// Eliminates the TOCTOU race for both single- and multi-instance deployments.
		if err := m.atomicSubs.CreateSubscriptionIfNotActive(ctx, subToStore(sub)); err != nil {
			var alreadyExists *store.ErrAlreadyExists
			if errors.As(err, &alreadyExists) {
				return nil, ErrAlreadySubscribed
			}
			return nil, err
		}
	} else {
		// Fallback two-step path — safe for single-instance, racy across instances.
		_, err := m.subs.GetActiveSubscription(ctx, in.SubscriberID)
		if err == nil {
			return nil, ErrAlreadySubscribed
		}
		if !isNotFound(err) {
			return nil, err
		}
		if err := m.subs.SaveSubscription(ctx, subToStore(sub)); err != nil {
			return nil, err
		}
	}
	return sub, nil
}

// GetSubscription returns a subscription by its internal ID.
func (m *Manager) GetSubscription(ctx context.Context, id string) (*Subscription, error) {
	ss, err := m.subs.GetSubscription(ctx, id)
	if err != nil {
		if isNotFound(err) {
			return nil, ErrSubscriptionNotFound
		}
		return nil, err
	}
	return subFromStore(ss), nil
}

// GetActiveSubscription returns the active/trialing subscription for a subscriber.
func (m *Manager) GetActiveSubscription(ctx context.Context, subscriberID string) (*Subscription, error) {
	ss, err := m.subs.GetActiveSubscription(ctx, subscriberID)
	if err != nil {
		if isNotFound(err) {
			return nil, ErrNotSubscribed
		}
		return nil, err
	}
	return subFromStore(ss), nil
}

// ListSubscriptions lists subscriptions for a subscriber.
func (m *Manager) ListSubscriptions(ctx context.Context, subscriberID string) ([]*Subscription, error) {
	sss, err := m.subs.ListSubscriptions(ctx, store.SubscriptionFilter{SubscriberID: subscriberID})
	if err != nil {
		return nil, err
	}
	out := make([]*Subscription, len(sss))
	for i, ss := range sss {
		out[i] = subFromStore(ss)
	}
	return out, nil
}

// CancelInput is the input for cancelling a subscription.
type CancelInput struct {
	SubscriptionID string
	Immediately    bool // false = cancel at end of current period
}

// Cancel cancels a subscription.
func (m *Manager) Cancel(ctx context.Context, in CancelInput) (*Subscription, error) {
	ss, err := m.subs.GetSubscription(ctx, in.SubscriptionID)
	if err != nil {
		if isNotFound(err) {
			return nil, ErrSubscriptionNotFound
		}
		return nil, err
	}

	if m.provider != nil && ss.ProviderID != "" {
		if err := m.provider.CancelSubscription(ctx, ss.ProviderID, in.Immediately); err != nil {
			return nil, err
		}
	}

	now := time.Now().UTC()
	ss.CanceledAt = &now
	ss.UpdatedAt = now
	if in.Immediately {
		ss.Status = string(StatusCanceled)
	}
	// If not immediately, leave status as active until provider webhook fires.

	if err := m.subs.SaveSubscription(ctx, ss); err != nil {
		return nil, err
	}
	return subFromStore(ss), nil
}

// Pause pauses billing on a subscription.
func (m *Manager) Pause(ctx context.Context, subscriptionID string) (*Subscription, error) {
	ss, err := m.subs.GetSubscription(ctx, subscriptionID)
	if err != nil {
		if isNotFound(err) {
			return nil, ErrSubscriptionNotFound
		}
		return nil, err
	}

	if m.provider != nil && ss.ProviderID != "" {
		if err := m.provider.PauseSubscription(ctx, ss.ProviderID); err != nil {
			return nil, err
		}
	}

	now := time.Now().UTC()
	ss.Status = string(StatusPaused)
	ss.PausedAt = &now
	ss.UpdatedAt = now

	if err := m.subs.SaveSubscription(ctx, ss); err != nil {
		return nil, err
	}
	return subFromStore(ss), nil
}

// Resume resumes a paused subscription.
func (m *Manager) Resume(ctx context.Context, subscriptionID string) (*Subscription, error) {
	ss, err := m.subs.GetSubscription(ctx, subscriptionID)
	if err != nil {
		if isNotFound(err) {
			return nil, ErrSubscriptionNotFound
		}
		return nil, err
	}

	if m.provider != nil && ss.ProviderID != "" {
		if err := m.provider.ResumeSubscription(ctx, ss.ProviderID); err != nil {
			return nil, err
		}
	}

	ss.Status = string(StatusActive)
	ss.PausedAt = nil
	ss.UpdatedAt = time.Now().UTC()

	if err := m.subs.SaveSubscription(ctx, ss); err != nil {
		return nil, err
	}
	return subFromStore(ss), nil
}

// ChangePlanInput is the input for upgrading or downgrading a subscription.
type ChangePlanInput struct {
	SubscriptionID string
	NewPlanID      string
}

// ChangePlan upgrades or downgrades a subscription to a different plan.
func (m *Manager) ChangePlan(ctx context.Context, in ChangePlanInput) (*Subscription, error) {
	ss, err := m.subs.GetSubscription(ctx, in.SubscriptionID)
	if err != nil {
		if isNotFound(err) {
			return nil, ErrSubscriptionNotFound
		}
		return nil, err
	}

	newSP, err := m.plans.GetPlan(ctx, in.NewPlanID)
	if err != nil {
		if isNotFound(err) {
			return nil, ErrPlanNotFound
		}
		return nil, err
	}
	newPlan := planFromStore(newSP)

	if m.provider != nil && ss.ProviderID != "" {
		if err := m.provider.ChangeSubscriptionPlan(ctx, ss.ProviderID, planToProviderPlan(newPlan)); err != nil {
			return nil, err
		}
	}

	ss.PlanID = in.NewPlanID
	ss.UpdatedAt = time.Now().UTC()

	if err := m.subs.SaveSubscription(ctx, ss); err != nil {
		return nil, err
	}
	return subFromStore(ss), nil
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func periodEnd(start time.Time, plan *Plan) time.Time {
	n := plan.IntervalCount
	if n == 0 {
		n = 1
	}
	switch plan.Interval {
	case PlanIntervalDay:
		return start.AddDate(0, 0, n)
	case PlanIntervalWeek:
		return start.AddDate(0, 0, n*7)
	case PlanIntervalMonth:
		return start.AddDate(0, n, 0)
	case PlanIntervalYear:
		return start.AddDate(n, 0, 0)
	default:
		return start.AddDate(0, 1, 0)
	}
}
