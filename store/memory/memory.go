// Package memory provides a thread-safe in-memory implementation of the
// store.PlanStore, store.SubscriptionStore, and store.UsageStore interfaces.
// It is intended for testing, local development, and quick-start prototyping.
// Do NOT use it in production — data is lost when the process restarts.
package memory

import (
	"context"
	"sync"
	"time"

	"github.com/nulllvoid/billow/store"
)

// ---------------------------------------------------------------------------
// PlanStore
// ---------------------------------------------------------------------------

// PlanStore is an in-memory PlanStore.
type PlanStore struct {
	mu    sync.RWMutex
	plans map[string]*store.Plan
}

// NewPlanStore returns an empty PlanStore.
func NewPlanStore() *PlanStore {
	return &PlanStore{plans: make(map[string]*store.Plan)}
}

func (s *PlanStore) SavePlan(_ context.Context, plan *store.Plan) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := *plan
	s.plans[plan.ID] = &cp
	return nil
}

func (s *PlanStore) GetPlan(_ context.Context, id string) (*store.Plan, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	p, ok := s.plans[id]
	if !ok {
		return nil, &store.ErrNotFound{Entity: "plan"}
	}
	cp := *p
	return &cp, nil
}

func (s *PlanStore) ListPlans(_ context.Context, filter store.PlanFilter) ([]*store.Plan, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []*store.Plan
	for _, p := range s.plans {
		if filter.ActiveOnly && !p.Active {
			continue
		}
		cp := *p
		out = append(out, &cp)
	}
	return out, nil
}

func (s *PlanStore) DeletePlan(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.plans[id]; !ok {
		return &store.ErrNotFound{Entity: "plan"}
	}
	delete(s.plans, id)
	return nil
}

// ---------------------------------------------------------------------------
// SubscriptionStore
// ---------------------------------------------------------------------------

// SubscriptionStore is an in-memory SubscriptionStore.
type SubscriptionStore struct {
	mu   sync.RWMutex
	subs map[string]*store.Subscription
}

// NewSubscriptionStore returns an empty SubscriptionStore.
func NewSubscriptionStore() *SubscriptionStore {
	return &SubscriptionStore{subs: make(map[string]*store.Subscription)}
}

func (s *SubscriptionStore) SaveSubscription(_ context.Context, sub *store.Subscription) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := *sub
	s.subs[sub.ID] = &cp
	return nil
}

func (s *SubscriptionStore) GetSubscription(_ context.Context, id string) (*store.Subscription, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	sub, ok := s.subs[id]
	if !ok {
		return nil, &store.ErrNotFound{Entity: "subscription"}
	}
	cp := *sub
	return &cp, nil
}

func (s *SubscriptionStore) GetSubscriptionByProviderID(_ context.Context, providerID string) (*store.Subscription, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, sub := range s.subs {
		if sub.ProviderID == providerID {
			cp := *sub
			return &cp, nil
		}
	}
	return nil, &store.ErrNotFound{Entity: "subscription"}
}

func (s *SubscriptionStore) GetActiveSubscription(_ context.Context, subscriberID string) (*store.Subscription, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, sub := range s.subs {
		if sub.SubscriberID == subscriberID &&
			(sub.Status == "active" || sub.Status == "trialing") {
			cp := *sub
			return &cp, nil
		}
	}
	return nil, &store.ErrNotFound{Entity: "subscription"}
}

func (s *SubscriptionStore) ListSubscriptions(_ context.Context, filter store.SubscriptionFilter) ([]*store.Subscription, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []*store.Subscription
	for _, sub := range s.subs {
		if filter.SubscriberID != "" && sub.SubscriberID != filter.SubscriberID {
			continue
		}
		if filter.PlanID != "" && sub.PlanID != filter.PlanID {
			continue
		}
		if filter.Status != "" && sub.Status != filter.Status {
			continue
		}
		cp := *sub
		out = append(out, &cp)
	}
	return out, nil
}

func (s *SubscriptionStore) DeleteSubscription(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.subs[id]; !ok {
		return &store.ErrNotFound{Entity: "subscription"}
	}
	delete(s.subs, id)
	return nil
}

// ---------------------------------------------------------------------------
// UsageStore
// ---------------------------------------------------------------------------

// UsageStore is an in-memory UsageStore.
type UsageStore struct {
	mu      sync.RWMutex
	records []*store.UsageRecord
}

// NewUsageStore returns an empty UsageStore.
func NewUsageStore() *UsageStore {
	return &UsageStore{}
}

func (s *UsageStore) SaveUsageRecord(_ context.Context, record *store.UsageRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := *record
	s.records = append(s.records, &cp)
	return nil
}

func (s *UsageStore) SumUsage(_ context.Context, subscriptionID, metric string, from, to time.Time) (int64, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var total int64
	for _, r := range s.records {
		if r.SubscriptionID == subscriptionID &&
			r.Metric == metric &&
			!r.RecordedAt.Before(from) &&
			r.RecordedAt.Before(to) {
			total += r.Quantity
		}
	}
	return total, nil
}

func (s *UsageStore) ListUsageRecords(_ context.Context, subscriptionID string, from, to time.Time) ([]*store.UsageRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []*store.UsageRecord
	for _, r := range s.records {
		if r.SubscriptionID == subscriptionID &&
			!r.RecordedAt.Before(from) &&
			r.RecordedAt.Before(to) {
			cp := *r
			out = append(out, &cp)
		}
	}
	return out, nil
}
