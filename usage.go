package billow

import (
	"context"
	"fmt"
	"time"

	"github.com/nulllvoid/billow/store"
)

// RecordUsageInput is the input for recording a metered usage event.
type RecordUsageInput struct {
	SubscriptionID string
	Metric         string            // e.g. "api_calls", "seats", "gb_storage"
	Quantity       int64
	RecordedAt     time.Time         // defaults to now
	Metadata       map[string]string
	// ReportToProvider controls whether the usage is also pushed to the payment
	// provider (for metered billing). Defaults to false.
	ReportToProvider bool
}

// RecordUsage appends a usage record for the given subscription.
// If in.ReportToProvider is true and a provider is configured, the usage is
// also reported to the provider for metered billing.
func (m *Manager) RecordUsage(ctx context.Context, in RecordUsageInput) (*UsageRecord, error) {
	if in.RecordedAt.IsZero() {
		in.RecordedAt = time.Now().UTC()
	}

	record := &UsageRecord{
		ID:             m.newID(),
		SubscriptionID: in.SubscriptionID,
		Metric:         in.Metric,
		Quantity:       in.Quantity,
		RecordedAt:     in.RecordedAt,
		Metadata:       in.Metadata,
	}

	if err := m.usage.SaveUsageRecord(ctx, usageToStore(record)); err != nil {
		return nil, err
	}

	if in.ReportToProvider && m.provider != nil {
		ss, err := m.subs.GetSubscription(ctx, in.SubscriptionID)
		if err == nil && ss.ProviderID != "" {
			_ = m.provider.ReportUsage(ctx, ss.ProviderID, in.Metric, in.Quantity)
			// Non-fatal: local record is already saved.
		}
	}

	return record, nil
}

// GetCurrentUsage returns the total usage for a metric in the subscription's
// current billing period.
func (m *Manager) GetCurrentUsage(ctx context.Context, subscriptionID, metric string) (int64, error) {
	ss, err := m.subs.GetSubscription(ctx, subscriptionID)
	if err != nil {
		if isNotFound(err) {
			return 0, ErrSubscriptionNotFound
		}
		return 0, err
	}
	return m.usage.SumUsage(ctx, subscriptionID, metric, ss.CurrentPeriodStart, ss.CurrentPeriodEnd)
}

// CheckLimit verifies whether adding delta units of metric would exceed the
// plan's configured limit for that metric.
// Returns ErrUsageLimitExceeded when the limit would be breached.
// Plans with a limit of 0 are treated as unlimited.
func (m *Manager) CheckLimit(ctx context.Context, subscriptionID, metric string, delta int64) error {
	ss, err := m.subs.GetSubscription(ctx, subscriptionID)
	if err != nil {
		if isNotFound(err) {
			return ErrSubscriptionNotFound
		}
		return err
	}

	sp, err := m.plans.GetPlan(ctx, ss.PlanID)
	if err != nil {
		if isNotFound(err) {
			return ErrPlanNotFound
		}
		return err
	}

	limit, hasLimit := sp.Limits[metric]
	if !hasLimit || limit == 0 {
		return nil // unlimited
	}

	current, err := m.usage.SumUsage(ctx, subscriptionID, metric, ss.CurrentPeriodStart, ss.CurrentPeriodEnd)
	if err != nil {
		return err
	}

	if current+delta > limit {
		return fmt.Errorf("%w: %s (current=%d, limit=%d, requested=%d)",
			ErrUsageLimitExceeded, metric, current, limit, delta)
	}
	return nil
}

// GetUsageReport returns all usage records for the current billing period.
func (m *Manager) GetUsageReport(ctx context.Context, subscriptionID string) ([]*UsageRecord, error) {
	ss, err := m.subs.GetSubscription(ctx, subscriptionID)
	if err != nil {
		if isNotFound(err) {
			return nil, ErrSubscriptionNotFound
		}
		return nil, err
	}

	records, err := m.usage.ListUsageRecords(ctx, subscriptionID, ss.CurrentPeriodStart, ss.CurrentPeriodEnd)
	if err != nil {
		return nil, err
	}

	out := make([]*UsageRecord, len(records))
	for i, r := range records {
		out[i] = usageFromStore(r)
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// mapping helpers
// ---------------------------------------------------------------------------

func usageToStore(u *UsageRecord) *store.UsageRecord {
	return &store.UsageRecord{
		ID:             u.ID,
		SubscriptionID: u.SubscriptionID,
		Metric:         u.Metric,
		Quantity:       u.Quantity,
		RecordedAt:     u.RecordedAt,
		Metadata:       u.Metadata,
	}
}

func usageFromStore(u *store.UsageRecord) *UsageRecord {
	return &UsageRecord{
		ID:             u.ID,
		SubscriptionID: u.SubscriptionID,
		Metric:         u.Metric,
		Quantity:       u.Quantity,
		RecordedAt:     u.RecordedAt,
		Metadata:       u.Metadata,
	}
}
