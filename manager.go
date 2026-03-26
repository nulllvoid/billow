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
//	// Register webhook event handlers (goroutine-safe; may be called at any time)
//	mgr.OnWebhookEvent(billow.EventPaymentFailed, func(e *billow.WebhookEvent) error {
//	    // send email, suspend access, etc.
//	    return nil
//	})
package billow

import (
	"context"
	"crypto/rand"
	"fmt"
	"io"
	"sync/atomic"
	"time"

	"github.com/nulllvoid/billow/provider"
	"github.com/nulllvoid/billow/store"
)

// handlerMap is the immutable snapshot type stored via atomic pointer.
type handlerMap map[WebhookEventType][]WebhookHandler

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
	// usage records. Defaults to a UUID v7 generator backed by crypto/rand;
	// safe across multiple processes and instances.
	IDGenerator func() string

	// EventTypeMapper translates provider-native event names to canonical
	// WebhookEventType values. When set it replaces the built-in Stripe/Razorpay
	// mappings entirely — call BuiltinEventTypes() to start from the defaults
	// and extend them. When nil the built-in mappings are used.
	EventTypeMapper func(providerType string) WebhookEventType

	// PlanCacheTTL controls how long fetched plans are kept in the in-process
	// cache. Plans are admin-only resources that rarely change; caching them
	// eliminates a store round-trip on every CheckLimit / CheckLimits call.
	// Defaults to 5 minutes when zero. Set to a negative value to disable.
	PlanCacheTTL time.Duration

	// DispatchWorkers is the number of async webhook-dispatch goroutines.
	// When > 0, HandleWebhook enqueues the event and returns immediately;
	// handlers run in the background. When 0 (default), dispatch is synchronous.
	DispatchWorkers int

	// DispatchQueueDepth is the per-worker channel buffer size. Defaults to 256.
	// Only meaningful when DispatchWorkers > 0.
	DispatchQueueDepth int

	// UsageReportInterval is how often the persist-then-report sweeper runs.
	// Defaults to 30 s. Only active when Usage implements store.ReportableUsageStore
	// and a Provider is set.
	UsageReportInterval time.Duration

	// UsageReportBatch is the maximum number of records flushed per sweep.
	// Defaults to 100.
	UsageReportBatch int

	// Metrics is an optional instrumentation backend. When nil all metrics
	// calls are no-ops.
	Metrics Metrics
}

// closer is the interface implemented by background components that must be
// stopped gracefully. Manager.Close calls each in registration order.
type closer interface {
	close()
}

// Manager is the central object backend developers interact with.
// Create one at startup and keep it for the lifetime of the application.
// Call Close when the application shuts down to drain in-flight work.
type Manager struct {
	provider        provider.PaymentProvider
	plans           store.PlanStore
	subs            store.SubscriptionStore
	atomicSubs      store.AtomicSubscriptionStore // non-nil when subs supports atomic ops
	usage           store.UsageStore
	newID           func() string
	planCache       *planCache                    // nil when PlanCacheTTL < 0
	eventTypes      map[string]WebhookEventType   // pre-built at construction; read-only after
	eventTypeMapper func(string) WebhookEventType // nil when using built-in map
	handlersPtr     atomic.Pointer[handlerMap]    // copy-on-write; lock-free reads
	pool            *dispatchPool                 // nil when DispatchWorkers == 0
	reporter        *usageReporter                // nil when no ReportableUsageStore/Provider
	metrics         Metrics                       // never nil (noopMetrics when not configured)
	closers         []closer                      // stopped in order by Close
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
		idGen = newUUIDv7
	}

	// Build the event-type lookup map once so mapEventType is a cheap O(1) read.
	evtTypes := make(map[string]WebhookEventType)
	if opts.EventTypeMapper == nil {
		for k, v := range BuiltinEventTypes() {
			evtTypes[k] = v
		}
	}

	// Build plan cache.
	cacheTTL := opts.PlanCacheTTL
	if cacheTTL == 0 {
		cacheTTL = 5 * time.Minute
	}
	var pc *planCache
	if cacheTTL > 0 {
		pc = newPlanCache(cacheTTL)
	}

	// Resolve metrics backend — never store nil so callers skip nil-checks.
	var met Metrics = noopMetrics{}
	if opts.Metrics != nil {
		met = opts.Metrics
	}

	m := &Manager{
		provider:        opts.Provider,
		plans:           opts.Plans,
		subs:            opts.Subs,
		usage:           opts.Usage,
		newID:           idGen,
		planCache:       pc,
		eventTypes:      evtTypes,
		eventTypeMapper: opts.EventTypeMapper,
		metrics:         met,
	}
	// Detect optional atomic interface for TOCTOU-safe Subscribe.
	if a, ok := opts.Subs.(store.AtomicSubscriptionStore); ok {
		m.atomicSubs = a
	}

	// Start async dispatch pool if requested.
	if opts.DispatchWorkers > 0 {
		m.pool = newDispatchPool(opts.DispatchWorkers, opts.DispatchQueueDepth, m)
		m.closers = append(m.closers, m.pool)
	}

	// Start persist-then-report sweeper when both provider and a
	// ReportableUsageStore are available.
	if opts.Provider != nil {
		if rs, ok := opts.Usage.(store.ReportableUsageStore); ok {
			m.reporter = newUsageReporter(m, rs, opts.UsageReportInterval, opts.UsageReportBatch)
			m.closers = append(m.closers, m.reporter)
		}
	}

	empty := make(handlerMap)
	m.handlersPtr.Store(&empty)

	return m
}

// Close drains all background goroutines and waits for them to finish.
// It should be called once when the application shuts down.
// After Close returns, no further background work will be performed.
func (m *Manager) Close() {
	for _, c := range m.closers {
		c.close()
	}
}

// OnWebhookEvent registers a handler for a specific webhook event type.
// Multiple handlers can be registered for the same event; they are called in
// registration order. Safe to call concurrently at any time, including after
// the Manager has started serving traffic.
func (m *Manager) OnWebhookEvent(eventType WebhookEventType, h WebhookHandler) {
	for {
		old := m.handlersPtr.Load()
		newMap := make(handlerMap, len(*old)+1)
		for k, v := range *old {
			newMap[k] = append([]WebhookHandler{}, v...) // copy each slice
		}
		newMap[eventType] = append(newMap[eventType], h)
		if m.handlersPtr.CompareAndSwap(old, &newMap) {
			return
		}
		// Another goroutine registered concurrently — retry with fresh snapshot.
	}
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

// getPlanCached fetches a plan from the in-process cache when available,
// falling back to the store on a miss and populating the cache on a hit.
func (m *Manager) getPlanCached(ctx context.Context, planID string) (*Plan, error) {
	if m.planCache != nil {
		if p, ok := m.planCache.get(planID); ok {
			m.metrics.PlanCacheHit(true)
			return p, nil
		}
		m.metrics.PlanCacheHit(false)
	}
	sp, err := m.plans.GetPlan(ctx, planID)
	if err != nil {
		return nil, err
	}
	p := planFromStore(sp)
	if m.planCache != nil {
		m.planCache.set(p)
	}
	return p, nil
}

// newUUIDv7 returns a time-ordered UUID v7 (RFC 9562) backed by crypto/rand.
// The 48-bit millisecond timestamp prefix gives excellent B-tree locality when
// used as a database primary key. Safe across multiple processes and instances.
func newUUIDv7() string {
	var b [16]byte
	if _, err := io.ReadFull(rand.Reader, b[:]); err != nil {
		panic("billow: crypto/rand unavailable: " + err.Error())
	}
	ms := uint64(time.Now().UnixMilli())
	b[0] = byte(ms >> 40)
	b[1] = byte(ms >> 32)
	b[2] = byte(ms >> 24)
	b[3] = byte(ms >> 16)
	b[4] = byte(ms >> 8)
	b[5] = byte(ms)
	b[6] = (b[6] & 0x0f) | 0x70 // version 7
	b[8] = (b[8] & 0x3f) | 0x80 // RFC 4122 variant
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
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
