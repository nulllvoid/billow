// Package memory provides a thread-safe in-memory implementation of the
// store.PlanStore, store.SubscriptionStore, and store.UsageStore interfaces.
// It is intended for testing, local development, and quick-start prototyping.
// Do NOT use it in production — data is lost when the process restarts.
package memory

import (
	"context"
	"hash/fnv"
	"sync"
	"time"

	"github.com/nulllvoid/billow/store"
)

// isActiveStatus reports whether the given subscription status string
// qualifies as "active" for the purposes of the activeBySubscriber index.
func isActiveStatus(status string) bool {
	return status == "active" || status == "trialing"
}

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

// SubscriptionStore is an in-memory SubscriptionStore with two secondary
// indexes for O(1) lookups by provider ID and active subscriber.
//
// Index invariants (maintained by SaveSubscription and DeleteSubscription):
//   - byProviderID[pid]       == id  iff  subs[id].ProviderID == pid  (pid != "")
//   - activeBySubscriber[sid] == id  iff  subs[id].SubscriberID == sid
//     AND isActiveStatus(subs[id].Status)
type SubscriptionStore struct {
	mu sync.RWMutex

	// Primary index: subscriptionID → subscription.
	subs map[string]*store.Subscription

	// Secondary index 1: providerID → subscriptionID (sparse: only set when ProviderID != "").
	byProviderID map[string]string

	// Secondary index 2: subscriberID → subscriptionID of the active/trialing sub.
	activeBySubscriber map[string]string
}

// NewSubscriptionStore returns an empty SubscriptionStore.
func NewSubscriptionStore() *SubscriptionStore {
	return &SubscriptionStore{
		subs:               make(map[string]*store.Subscription),
		byProviderID:       make(map[string]string),
		activeBySubscriber: make(map[string]string),
	}
}

// SaveSubscription creates or updates a subscription and keeps both secondary
// indexes consistent with the primary map. All three map writes happen under
// a single write lock so no read can observe a partially-updated state.
func (s *SubscriptionStore) SaveSubscription(_ context.Context, sub *store.Subscription) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	old, exists := s.subs[sub.ID]

	// ── byProviderID index ────────────────────────────────────────────────────
	// Remove the stale entry (handles "" → "pid" and "pid1" → "pid2" transitions).
	if exists && old.ProviderID != "" {
		delete(s.byProviderID, old.ProviderID)
	}
	if sub.ProviderID != "" {
		s.byProviderID[sub.ProviderID] = sub.ID
	}

	// ── activeBySubscriber index ──────────────────────────────────────────────
	newActive := isActiveStatus(sub.Status)
	if exists {
		oldActive := isActiveStatus(old.Status)
		if oldActive && !newActive {
			// Transition to non-active: evict only if this record is still indexed.
			if s.activeBySubscriber[old.SubscriberID] == sub.ID {
				delete(s.activeBySubscriber, old.SubscriberID)
			}
		}
	}
	if newActive {
		s.activeBySubscriber[sub.SubscriberID] = sub.ID
	}

	// ── Primary store ─────────────────────────────────────────────────────────
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

// GetSubscriptionByProviderID is O(1) via the byProviderID index.
func (s *SubscriptionStore) GetSubscriptionByProviderID(_ context.Context, providerID string) (*store.Subscription, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	id, ok := s.byProviderID[providerID]
	if !ok {
		return nil, &store.ErrNotFound{Entity: "subscription"}
	}
	cp := *s.subs[id]
	return &cp, nil
}

// GetActiveSubscription is O(1) via the activeBySubscriber index.
func (s *SubscriptionStore) GetActiveSubscription(_ context.Context, subscriberID string) (*store.Subscription, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	id, ok := s.activeBySubscriber[subscriberID]
	if !ok {
		return nil, &store.ErrNotFound{Entity: "subscription"}
	}
	cp := *s.subs[id]
	return &cp, nil
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

// DeleteSubscription removes the subscription and cleans up both secondary indexes.
func (s *SubscriptionStore) DeleteSubscription(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	sub, ok := s.subs[id]
	if !ok {
		return &store.ErrNotFound{Entity: "subscription"}
	}
	if sub.ProviderID != "" {
		delete(s.byProviderID, sub.ProviderID)
	}
	// Only evict the active index if this subscription is the one currently indexed.
	if s.activeBySubscriber[sub.SubscriberID] == id {
		delete(s.activeBySubscriber, sub.SubscriberID)
	}
	delete(s.subs, id)
	return nil
}

// CreateSubscriptionIfNotActive atomically checks whether the subscriber
// already has an active or trialing subscription and, if not, inserts sub.
// Returns *store.ErrAlreadyExists when a conflict is detected.
// The entire check+insert runs under a single write lock — no TOCTOU window.
func (s *SubscriptionStore) CreateSubscriptionIfNotActive(_ context.Context, sub *store.Subscription) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// O(1) check via the secondary index.
	if _, exists := s.activeBySubscriber[sub.SubscriberID]; exists {
		return &store.ErrAlreadyExists{Entity: "subscription"}
	}

	// Insert into primary and both secondary indexes.
	cp := *sub
	s.subs[sub.ID] = &cp
	if sub.ProviderID != "" {
		s.byProviderID[sub.ProviderID] = sub.ID
	}
	if isActiveStatus(sub.Status) {
		s.activeBySubscriber[sub.SubscriberID] = sub.ID
	}
	return nil
}

// ---------------------------------------------------------------------------
// UsageStore
// ---------------------------------------------------------------------------

// UsageStore is an in-memory UsageStore backed by a nested index.
//
// The index has the shape:
//
//	index[subscriptionID][metric] → []*store.UsageRecord (ordered by insertion)
//
// This reduces SumUsage and ListUsageRecords from O(N_total) to O(k) where k
// is the number of records for the specific subscription+metric — typically
// orders of magnitude smaller than the total record count.
//
// Production adapters MUST create a composite index on
// (subscription_id, metric, recorded_at) to achieve equivalent performance.
type UsageStore struct {
	mu    sync.RWMutex
	index map[string]map[string][]*store.UsageRecord // [subID][metric][]records
}

// NewUsageStore returns an empty UsageStore.
func NewUsageStore() *UsageStore {
	return &UsageStore{
		index: make(map[string]map[string][]*store.UsageRecord),
	}
}

func (s *UsageStore) SaveUsageRecord(_ context.Context, record *store.UsageRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := *record
	if _, ok := s.index[cp.SubscriptionID]; !ok {
		s.index[cp.SubscriptionID] = make(map[string][]*store.UsageRecord)
	}
	s.index[cp.SubscriptionID][cp.Metric] = append(s.index[cp.SubscriptionID][cp.Metric], &cp)
	return nil
}

// SumUsage scans only the bucket for (subscriptionID, metric) — O(k) where k
// is the number of records for that pair, not O(N_total).
func (s *UsageStore) SumUsage(_ context.Context, subscriptionID, metric string, from, to time.Time) (int64, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	metricMap, ok := s.index[subscriptionID]
	if !ok {
		return 0, nil
	}
	bucket, ok := metricMap[metric]
	if !ok {
		return 0, nil
	}
	var total int64
	for _, r := range bucket {
		if !r.RecordedAt.Before(from) && r.RecordedAt.Before(to) {
			total += r.Quantity
		}
	}
	return total, nil
}

// ListUsageRecords scans only the records belonging to subscriptionID — O(m)
// where m is the total records for that subscription across all metrics.
func (s *UsageStore) ListUsageRecords(_ context.Context, subscriptionID string, from, to time.Time) ([]*store.UsageRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	metricMap, ok := s.index[subscriptionID]
	if !ok {
		return nil, nil
	}
	var out []*store.UsageRecord
	for _, bucket := range metricMap {
		for _, r := range bucket {
			if !r.RecordedAt.Before(from) && r.RecordedAt.Before(to) {
				cp := *r
				out = append(out, &cp)
			}
		}
	}
	return out, nil
}

// ListUnreportedUsage returns up to limit records whose ProviderReportedAt is nil.
// Implements store.ReportableUsageStore.
func (s *UsageStore) ListUnreportedUsage(_ context.Context, limit int) ([]*store.UsageRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []*store.UsageRecord
	for _, metricMap := range s.index {
		for _, bucket := range metricMap {
			for _, r := range bucket {
				if r.ProviderReportedAt == nil {
					cp := *r
					out = append(out, &cp)
					if len(out) >= limit {
						return out, nil
					}
				}
			}
		}
	}
	return out, nil
}

// MarkUsageReported sets ProviderReportedAt on the record with the given ID.
// Implements store.ReportableUsageStore.
func (s *UsageStore) MarkUsageReported(_ context.Context, id string, reportedAt time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, metricMap := range s.index {
		for _, bucket := range metricMap {
			for _, r := range bucket {
				if r.ID == id {
					r.ProviderReportedAt = &reportedAt
					return nil
				}
			}
		}
	}
	return &store.ErrNotFound{Entity: "usage_record"}
}

// ---------------------------------------------------------------------------
// ShardedSubscriptionStore — Phase 5
// ---------------------------------------------------------------------------
//
// ShardedSubscriptionStore splits the primary subscription map across 256
// shards, each guarded by its own RWMutex. This allows writes to different
// subscriptions to proceed in parallel instead of serialising behind a single
// global lock — critical at high subscriber counts (>100 k concurrent writes).
//
// Two global secondary indexes live outside the shards so that cross-shard
// lookups (byProviderID, activeBySubscriber) remain O(1) and do not require
// acquiring multiple shard locks simultaneously.
//
// Index invariants are identical to SubscriptionStore.
//
// ShardedSubscriptionStore implements both store.SubscriptionStore and
// store.AtomicSubscriptionStore, so the Manager's TOCTOU-safe path is used
// automatically.

const numShards = 256

// subShard is one of the 256 independent shards.
type subShard struct {
	mu   sync.RWMutex
	subs map[string]*store.Subscription
}

// ShardedSubscriptionStore is the high-throughput replacement for
// SubscriptionStore. Use NewShardedSubscriptionStore() to create one.
type ShardedSubscriptionStore struct {
	shards [numShards]subShard

	// Global secondary indexes — each guarded by its own mutex so reads on
	// one index do not block reads on the other.
	pidMu        sync.RWMutex
	byProviderID map[string]string // providerID → subID

	activeMu           sync.RWMutex
	activeBySubscriber map[string]string // subscriberID → subID (active/trialing)
}

// NewShardedSubscriptionStore returns an initialised ShardedSubscriptionStore.
func NewShardedSubscriptionStore() *ShardedSubscriptionStore {
	s := &ShardedSubscriptionStore{
		byProviderID:       make(map[string]string),
		activeBySubscriber: make(map[string]string),
	}
	for i := range s.shards {
		s.shards[i].subs = make(map[string]*store.Subscription)
	}
	return s
}

// shardFor returns the shard responsible for the given subscription ID.
func (s *ShardedSubscriptionStore) shardFor(id string) *subShard {
	h := fnv.New32a()
	_, _ = h.Write([]byte(id))
	return &s.shards[h.Sum32()%numShards]
}

// SaveSubscription upserts a subscription and keeps both global indexes consistent.
func (s *ShardedSubscriptionStore) SaveSubscription(_ context.Context, sub *store.Subscription) error {
	sh := s.shardFor(sub.ID)

	sh.mu.Lock()
	old, exists := sh.subs[sub.ID]
	cp := *sub
	sh.subs[sub.ID] = &cp
	sh.mu.Unlock()

	// ── byProviderID ──────────────────────────────────────────────────────────
	s.pidMu.Lock()
	if exists && old.ProviderID != "" {
		delete(s.byProviderID, old.ProviderID)
	}
	if sub.ProviderID != "" {
		s.byProviderID[sub.ProviderID] = sub.ID
	}
	s.pidMu.Unlock()

	// ── activeBySubscriber ────────────────────────────────────────────────────
	newActive := isActiveStatus(sub.Status)
	s.activeMu.Lock()
	if exists && isActiveStatus(old.Status) && !newActive {
		if s.activeBySubscriber[old.SubscriberID] == sub.ID {
			delete(s.activeBySubscriber, old.SubscriberID)
		}
	}
	if newActive {
		s.activeBySubscriber[sub.SubscriberID] = sub.ID
	}
	s.activeMu.Unlock()

	return nil
}

func (s *ShardedSubscriptionStore) GetSubscription(_ context.Context, id string) (*store.Subscription, error) {
	sh := s.shardFor(id)
	sh.mu.RLock()
	sub, ok := sh.subs[id]
	sh.mu.RUnlock()
	if !ok {
		return nil, &store.ErrNotFound{Entity: "subscription"}
	}
	cp := *sub
	return &cp, nil
}

// GetSubscriptionByProviderID is O(1) via the global byProviderID index.
func (s *ShardedSubscriptionStore) GetSubscriptionByProviderID(_ context.Context, providerID string) (*store.Subscription, error) {
	s.pidMu.RLock()
	id, ok := s.byProviderID[providerID]
	s.pidMu.RUnlock()
	if !ok {
		return nil, &store.ErrNotFound{Entity: "subscription"}
	}
	return s.GetSubscription(context.Background(), id)
}

// GetActiveSubscription is O(1) via the global activeBySubscriber index.
func (s *ShardedSubscriptionStore) GetActiveSubscription(_ context.Context, subscriberID string) (*store.Subscription, error) {
	s.activeMu.RLock()
	id, ok := s.activeBySubscriber[subscriberID]
	s.activeMu.RUnlock()
	if !ok {
		return nil, &store.ErrNotFound{Entity: "subscription"}
	}
	return s.GetSubscription(context.Background(), id)
}

// ListSubscriptions scans all shards. Acquires each shard's read lock in turn.
func (s *ShardedSubscriptionStore) ListSubscriptions(_ context.Context, filter store.SubscriptionFilter) ([]*store.Subscription, error) {
	var out []*store.Subscription
	for i := range s.shards {
		sh := &s.shards[i]
		sh.mu.RLock()
		for _, sub := range sh.subs {
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
		sh.mu.RUnlock()
	}
	return out, nil
}

// DeleteSubscription removes the subscription and evicts both global indexes.
func (s *ShardedSubscriptionStore) DeleteSubscription(_ context.Context, id string) error {
	sh := s.shardFor(id)

	sh.mu.Lock()
	sub, ok := sh.subs[id]
	if !ok {
		sh.mu.Unlock()
		return &store.ErrNotFound{Entity: "subscription"}
	}
	delete(sh.subs, id)
	sh.mu.Unlock()

	s.pidMu.Lock()
	if sub.ProviderID != "" {
		delete(s.byProviderID, sub.ProviderID)
	}
	s.pidMu.Unlock()

	s.activeMu.Lock()
	if s.activeBySubscriber[sub.SubscriberID] == id {
		delete(s.activeBySubscriber, sub.SubscriberID)
	}
	s.activeMu.Unlock()

	return nil
}

// CreateSubscriptionIfNotActive is the atomic check+insert used by Manager.Subscribe.
// It holds the activeBySubscriber write lock for the entire check+insert so no
// concurrent goroutine can slip in between.
func (s *ShardedSubscriptionStore) CreateSubscriptionIfNotActive(_ context.Context, sub *store.Subscription) error {
	s.activeMu.Lock()
	defer s.activeMu.Unlock()

	if _, exists := s.activeBySubscriber[sub.SubscriberID]; exists {
		return &store.ErrAlreadyExists{Entity: "subscription"}
	}

	// Insert into shard.
	sh := s.shardFor(sub.ID)
	cp := *sub
	sh.mu.Lock()
	sh.subs[sub.ID] = &cp
	sh.mu.Unlock()

	// Update global indexes.
	if sub.ProviderID != "" {
		s.pidMu.Lock()
		s.byProviderID[sub.ProviderID] = sub.ID
		s.pidMu.Unlock()
	}
	if isActiveStatus(sub.Status) {
		s.activeBySubscriber[sub.SubscriberID] = sub.ID
	}
	return nil
}
