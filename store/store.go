// Package store defines the persistence interfaces used by the Manager.
// Implement these interfaces against your database of choice (Postgres,
// MongoDB, DynamoDB, etc.) or use the bundled store/memory package for
// testing and quick-start prototyping.
package store

import (
	"context"
	"time"
)

// ---------------------------------------------------------------------------
// Filter types
// ---------------------------------------------------------------------------

// PlanFilter narrows the set of plans returned by ListPlans.
type PlanFilter struct {
	ActiveOnly bool
}

// SubscriptionFilter narrows the set of subscriptions returned by
// ListSubscriptions.
type SubscriptionFilter struct {
	SubscriberID string
	PlanID       string
	Status       string // empty = all statuses
}

// ---------------------------------------------------------------------------
// Entity types
// ---------------------------------------------------------------------------
// These are thin persistence-layer versions of the domain types. The Manager
// maps between store types and the public API types in types.go.

// Plan is the stored representation of a subscription plan.
type Plan struct {
	ID            string
	ProviderID    string
	Name          string
	Description   string
	Amount        int64
	Currency      string
	Interval      string
	IntervalCount int
	TrialDays     int
	Features      []string
	Limits        map[string]int64
	Metadata      map[string]string
	Active        bool
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// Subscription is the stored representation of a subscription.
type Subscription struct {
	ID                 string
	ProviderID         string
	SubscriberID       string
	PlanID             string
	Status             string
	CurrentPeriodStart time.Time
	CurrentPeriodEnd   time.Time
	TrialStart         *time.Time
	TrialEnd           *time.Time
	CanceledAt         *time.Time
	PausedAt           *time.Time
	Metadata           map[string]string
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

// UsageRecord is a single metered usage event.
type UsageRecord struct {
	ID                 string
	SubscriptionID     string
	Metric             string
	Quantity           int64
	RecordedAt         time.Time
	Metadata           map[string]string
	// ProviderReportedAt is set (non-zero) once the record has been
	// successfully reported to the payment provider. A zero value means the
	// record is pending reporting. Used by the persist-then-report sweeper.
	ProviderReportedAt *time.Time
}

// ---------------------------------------------------------------------------
// Interfaces
// ---------------------------------------------------------------------------

// PlanStore persists subscription plans.
type PlanStore interface {
	// SavePlan creates or updates a plan (upsert on ID).
	SavePlan(ctx context.Context, plan *Plan) error

	// GetPlan returns the plan with the given ID or ErrNotFound.
	GetPlan(ctx context.Context, id string) (*Plan, error)

	// ListPlans returns plans matching the filter.
	ListPlans(ctx context.Context, filter PlanFilter) ([]*Plan, error)

	// DeletePlan removes the plan permanently.
	DeletePlan(ctx context.Context, id string) error
}

// SubscriptionStore persists subscriptions.
type SubscriptionStore interface {
	// SaveSubscription creates or updates a subscription (upsert on ID).
	SaveSubscription(ctx context.Context, sub *Subscription) error

	// GetSubscription returns the subscription with the given ID or ErrNotFound.
	GetSubscription(ctx context.Context, id string) (*Subscription, error)

	// GetSubscriptionByProviderID looks up a subscription by the provider's ID.
	GetSubscriptionByProviderID(ctx context.Context, providerID string) (*Subscription, error)

	// GetActiveSubscription returns the active/trialing subscription for a
	// subscriber, or ErrNotFound if none exists.
	GetActiveSubscription(ctx context.Context, subscriberID string) (*Subscription, error)

	// ListSubscriptions returns subscriptions matching the filter.
	ListSubscriptions(ctx context.Context, filter SubscriptionFilter) ([]*Subscription, error)

	// DeleteSubscription hard-deletes a subscription record.
	DeleteSubscription(ctx context.Context, id string) error
}

// UsageStore persists metered usage records.
type UsageStore interface {
	// SaveUsageRecord appends a new usage record.
	SaveUsageRecord(ctx context.Context, record *UsageRecord) error

	// SumUsage returns the total quantity for a metric within [from, to).
	SumUsage(ctx context.Context, subscriptionID, metric string, from, to time.Time) (int64, error)

	// ListUsageRecords returns all records for a subscription within [from, to).
	ListUsageRecords(ctx context.Context, subscriptionID string, from, to time.Time) ([]*UsageRecord, error)
}

// ErrNotFound is returned by store implementations when a record does not exist.
// Callers compare with errors.Is.
type ErrNotFound struct{ Entity string }

func (e *ErrNotFound) Error() string { return "store: " + e.Entity + " not found" }

// ErrAlreadyExists is returned by atomic store operations (e.g.
// CreateSubscriptionIfNotActive) when the record already exists.
type ErrAlreadyExists struct{ Entity string }

func (e *ErrAlreadyExists) Error() string { return "store: " + e.Entity + " already exists" }

// ReportableUsageStore is an optional extension of UsageStore for the
// persist-then-report pattern. Implementations that want crash-safe provider
// reporting should implement this interface.
//
// The Manager detects it at construction time via type assertion. When absent
// the usage reporter falls back to fire-and-forget (same as before).
type ReportableUsageStore interface {
	UsageStore
	// ListUnreportedUsage returns up to limit records that have not yet been
	// reported to the provider (ProviderReportedAt is nil/zero).
	ListUnreportedUsage(ctx context.Context, limit int) ([]*UsageRecord, error)
	// MarkUsageReported sets ProviderReportedAt = reportedAt for the given record ID.
	MarkUsageReported(ctx context.Context, id string, reportedAt time.Time) error
}

// AtomicSubscriptionStore is an optional extension of SubscriptionStore that
// provides an atomic check-and-insert for the subscribe guard. Implementations
// that embed a real database SHOULD implement this interface using a transaction
// or conditional write (Postgres: INSERT … WHERE NOT EXISTS; DynamoDB: conditional
// PutItem) to eliminate the TOCTOU race between the duplicate-subscriber check
// and the insert.
//
// The Manager detects this interface at construction time via type assertion.
// Store adapters that do not implement it fall back to a two-step check+save,
// which is safe for single-instance deployments but racy in multi-instance ones.
type AtomicSubscriptionStore interface {
	SubscriptionStore
	// CreateSubscriptionIfNotActive inserts sub only when no active or trialing
	// subscription already exists for sub.SubscriberID. Returns *ErrAlreadyExists
	// on conflict. The check and insert MUST be atomic.
	CreateSubscriptionIfNotActive(ctx context.Context, sub *Subscription) error
}
