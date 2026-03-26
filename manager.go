// Package billow provides a provider-agnostic subscription engine for
// Go backend services.
//
// Quick start:
//
//	mgr := billow.NewManager(billow.Options{
//	    Provider: myStripeProvider,  // implements provider.PaymentProvider
//	    Plans:    memory.NewPlanStore(),
//	    Subs:     memory.NewSubscriptionStore(),
//	    Usage:    memory.NewUsageStore(),
//	})
//
//	// Register webhook event handlers
//	mgr.OnWebhookEvent(billow.EventPaymentFailed, func(e *billow.WebhookEvent) error {
//	    // send email, suspend access, etc.
//	    return nil
//	})
package billow

import (
	"fmt"
	"sync/atomic"
	"time"

	"github.com/nulllvoid/billow/provider"
	"github.com/nulllvoid/billow/store"
)

// Options configures a Manager.
type Options struct {
	// Provider is the payment gateway adapter. Required.
	Provider provider.PaymentProvider

	// Plans, Subs, and Usage are the persistence stores.
	// Use store/memory for development; supply your own for production.
	Plans store.PlanStore
	Subs  store.SubscriptionStore
	Usage store.UsageStore

	// IDGenerator is called to create new IDs for plans, subscriptions, and
	// usage records. Defaults to a simple timestamp+random string generator.
	IDGenerator func() string
}

// Manager is the central object backend developers interact with.
// Create one at startup and keep it for the lifetime of the application.
type Manager struct {
	provider provider.PaymentProvider
	plans    store.PlanStore
	subs     store.SubscriptionStore
	usage    store.UsageStore
	newID    func() string
	handlers map[WebhookEventType][]WebhookHandler
}

// NewManager creates a Manager from the given options.
// It panics when required fields are missing so the mistake is caught at
// startup, not at the first API call.
func NewManager(opts Options) *Manager {
	if opts.Plans == nil {
		panic("subscriptions: Options.Plans store is required")
	}
	if opts.Subs == nil {
		panic("subscriptions: Options.Subs store is required")
	}
	if opts.Usage == nil {
		panic("subscriptions: Options.Usage store is required")
	}
	idGen := opts.IDGenerator
	if idGen == nil {
		idGen = defaultIDGenerator()
	}
	return &Manager{
		provider: opts.Provider,
		plans:    opts.Plans,
		subs:     opts.Subs,
		usage:    opts.Usage,
		newID:    idGen,
		handlers: make(map[WebhookEventType][]WebhookHandler),
	}
}

// OnWebhookEvent registers a handler for a specific webhook event type.
// Multiple handlers can be registered for the same event; they are called in
// registration order. Registering handlers is not goroutine-safe; call before
// serving traffic.
func (m *Manager) OnWebhookEvent(eventType WebhookEventType, h WebhookHandler) {
	m.handlers[eventType] = append(m.handlers[eventType], h)
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func (m *Manager) requireProvider() error {
	if m.provider == nil {
		return ErrProviderNotSet
	}
	return nil
}

func isNotFound(err error) bool {
	_, ok := err.(*store.ErrNotFound)
	return ok
}

// defaultIDGenerator returns a function that produces IDs of the form
// "<unix_nano>_<counter>" — good enough for development; replace with
// UUID/ULID in production via Options.IDGenerator.
func defaultIDGenerator() func() string {
	var counter int64
	return func() string {
		n := atomic.AddInt64(&counter, 1)
		return fmt.Sprintf("%d_%d", time.Now().UnixNano(), n)
	}
}

// ---------------------------------------------------------------------------
// Domain → Store and Store → Domain mapping helpers
// ---------------------------------------------------------------------------

func planToStore(p *Plan) *store.Plan {
	return &store.Plan{
		ID:            p.ID,
		ProviderID:    p.ProviderID,
		Name:          p.Name,
		Description:   p.Description,
		Amount:        p.Amount,
		Currency:      p.Currency,
		Interval:      string(p.Interval),
		IntervalCount: p.IntervalCount,
		TrialDays:     p.TrialDays,
		Features:      p.Features,
		Limits:        p.Limits,
		Metadata:      p.Metadata,
		Active:        p.Active,
		CreatedAt:     p.CreatedAt,
		UpdatedAt:     p.UpdatedAt,
	}
}

func planFromStore(p *store.Plan) *Plan {
	return &Plan{
		ID:            p.ID,
		ProviderID:    p.ProviderID,
		Name:          p.Name,
		Description:   p.Description,
		Amount:        p.Amount,
		Currency:      p.Currency,
		Interval:      PlanInterval(p.Interval),
		IntervalCount: p.IntervalCount,
		TrialDays:     p.TrialDays,
		Features:      p.Features,
		Limits:        p.Limits,
		Metadata:      p.Metadata,
		Active:        p.Active,
		CreatedAt:     p.CreatedAt,
		UpdatedAt:     p.UpdatedAt,
	}
}

func subToStore(s *Subscription) *store.Subscription {
	return &store.Subscription{
		ID:                 s.ID,
		ProviderID:         s.ProviderID,
		SubscriberID:       s.SubscriberID,
		PlanID:             s.PlanID,
		Status:             string(s.Status),
		CurrentPeriodStart: s.CurrentPeriodStart,
		CurrentPeriodEnd:   s.CurrentPeriodEnd,
		TrialStart:         s.TrialStart,
		TrialEnd:           s.TrialEnd,
		CanceledAt:         s.CanceledAt,
		PausedAt:           s.PausedAt,
		Metadata:           s.Metadata,
		CreatedAt:          s.CreatedAt,
		UpdatedAt:          s.UpdatedAt,
	}
}

func subFromStore(s *store.Subscription) *Subscription {
	return &Subscription{
		ID:                 s.ID,
		ProviderID:         s.ProviderID,
		SubscriberID:       s.SubscriberID,
		PlanID:             s.PlanID,
		Status:             SubscriptionStatus(s.Status),
		CurrentPeriodStart: s.CurrentPeriodStart,
		CurrentPeriodEnd:   s.CurrentPeriodEnd,
		TrialStart:         s.TrialStart,
		TrialEnd:           s.TrialEnd,
		CanceledAt:         s.CanceledAt,
		PausedAt:           s.PausedAt,
		Metadata:           s.Metadata,
		CreatedAt:          s.CreatedAt,
		UpdatedAt:          s.UpdatedAt,
	}
}

func planToProviderPlan(p *Plan) *provider.Plan {
	return &provider.Plan{
		ID:            p.ID,
		ProviderID:    p.ProviderID,
		Name:          p.Name,
		Amount:        p.Amount,
		Currency:      p.Currency,
		Interval:      string(p.Interval),
		IntervalCount: p.IntervalCount,
		TrialDays:     p.TrialDays,
		Metadata:      p.Metadata,
	}
}

func subToProviderSub(s *Subscription) *provider.Subscription {
	return &provider.Subscription{
		ID:                 s.ID,
		ProviderID:         s.ProviderID,
		SubscriberID:       s.SubscriberID,
		PlanID:             s.PlanID,
		Status:             string(s.Status),
		CurrentPeriodStart: s.CurrentPeriodStart,
		CurrentPeriodEnd:   s.CurrentPeriodEnd,
		TrialStart:         s.TrialStart,
		TrialEnd:           s.TrialEnd,
		Metadata:           s.Metadata,
	}
}
