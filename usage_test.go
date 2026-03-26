package billow_test

import (
	"context"
	"errors"
	"testing"

	billow "github.com/nulllvoid/billow"
)

func TestRecordUsage(t *testing.T) {
	mgr := newTestManager(t)
	plan := createTestPlan(t, mgr)
	sub := testSubscribe(t, mgr, "user_u1", plan.ID)

	record, err := mgr.RecordUsage(context.Background(), billow.RecordUsageInput{
		SubscriptionID: sub.ID,
		Metric:         "api_calls",
		Quantity:       100,
	})
	if err != nil {
		t.Fatalf("RecordUsage: %v", err)
	}
	if record.ID == "" {
		t.Error("expected non-empty record ID")
	}
	if record.Quantity != 100 {
		t.Errorf("Quantity = %d, want 100", record.Quantity)
	}
	if record.RecordedAt.IsZero() {
		t.Error("expected RecordedAt to be set")
	}
	if record.Metric != "api_calls" {
		t.Errorf("Metric = %q, want %q", record.Metric, "api_calls")
	}
}

func TestGetCurrentUsage(t *testing.T) {
	mgr := newTestManager(t)
	plan := createTestPlan(t, mgr)
	sub := testSubscribe(t, mgr, "user_u2", plan.ID)
	ctx := context.Background()

	_, _ = mgr.RecordUsage(ctx, billow.RecordUsageInput{SubscriptionID: sub.ID, Metric: "api_calls", Quantity: 300})
	_, _ = mgr.RecordUsage(ctx, billow.RecordUsageInput{SubscriptionID: sub.ID, Metric: "api_calls", Quantity: 200})
	_, _ = mgr.RecordUsage(ctx, billow.RecordUsageInput{SubscriptionID: sub.ID, Metric: "seats", Quantity: 5}) // different metric

	total, err := mgr.GetCurrentUsage(ctx, sub.ID, "api_calls")
	if err != nil {
		t.Fatalf("GetCurrentUsage: %v", err)
	}
	if total != 500 {
		t.Errorf("total = %d, want 500", total)
	}
}

func TestGetCurrentUsage_SubscriptionNotFound(t *testing.T) {
	mgr := newTestManager(t)
	_, err := mgr.GetCurrentUsage(context.Background(), "nonexistent", "api_calls")
	if !errors.Is(err, billow.ErrSubscriptionNotFound) {
		t.Errorf("err = %v, want ErrSubscriptionNotFound", err)
	}
}

func TestCheckLimit_OK(t *testing.T) {
	mgr := newTestManager(t)
	plan := createTestPlan(t, mgr) // api_calls limit = 1000
	sub := testSubscribe(t, mgr, "user_u3", plan.ID)
	ctx := context.Background()

	_, _ = mgr.RecordUsage(ctx, billow.RecordUsageInput{SubscriptionID: sub.ID, Metric: "api_calls", Quantity: 400})

	if err := mgr.CheckLimit(ctx, sub.ID, "api_calls", 500); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestCheckLimit_Exceeded(t *testing.T) {
	mgr := newTestManager(t)
	plan := createTestPlan(t, mgr) // api_calls limit = 1000
	sub := testSubscribe(t, mgr, "user_u4", plan.ID)
	ctx := context.Background()

	_, _ = mgr.RecordUsage(ctx, billow.RecordUsageInput{SubscriptionID: sub.ID, Metric: "api_calls", Quantity: 800})

	err := mgr.CheckLimit(ctx, sub.ID, "api_calls", 300) // 800 + 300 = 1100 > 1000
	if !errors.Is(err, billow.ErrUsageLimitExceeded) {
		t.Errorf("err = %v, want ErrUsageLimitExceeded", err)
	}
}

func TestCheckLimit_Unlimited(t *testing.T) {
	mgr := newTestManager(t)
	ctx := context.Background()

	plan, err := mgr.CreatePlan(ctx, billow.CreatePlanInput{
		Name:     "Unlimited",
		Amount:   5000,
		Currency: "usd",
		Interval: billow.PlanIntervalMonth,
		Limits:   map[string]int64{"api_calls": 0}, // 0 = unlimited
	})
	if err != nil {
		t.Fatal(err)
	}
	sub := testSubscribe(t, mgr, "user_u5", plan.ID)

	_, _ = mgr.RecordUsage(ctx, billow.RecordUsageInput{SubscriptionID: sub.ID, Metric: "api_calls", Quantity: 1_000_000})

	if err := mgr.CheckLimit(ctx, sub.ID, "api_calls", 1_000_000); err != nil {
		t.Errorf("unexpected error for unlimited plan: %v", err)
	}
}

func TestCheckLimit_MetricNotInPlan(t *testing.T) {
	mgr := newTestManager(t)
	plan := createTestPlan(t, mgr) // only has "api_calls" limit
	sub := testSubscribe(t, mgr, "user_u6", plan.ID)

	// "seats" is not in plan Limits — treated as unlimited.
	if err := mgr.CheckLimit(context.Background(), sub.ID, "seats", 999999); err != nil {
		t.Errorf("unexpected error for unlisted metric: %v", err)
	}
}

func TestCheckLimit_SubscriptionNotFound(t *testing.T) {
	mgr := newTestManager(t)
	err := mgr.CheckLimit(context.Background(), "nonexistent", "api_calls", 1)
	if !errors.Is(err, billow.ErrSubscriptionNotFound) {
		t.Errorf("err = %v, want ErrSubscriptionNotFound", err)
	}
}

func TestGetUsageReport(t *testing.T) {
	mgr := newTestManager(t)
	plan := createTestPlan(t, mgr)
	sub := testSubscribe(t, mgr, "user_u7", plan.ID)
	ctx := context.Background()

	_, _ = mgr.RecordUsage(ctx, billow.RecordUsageInput{SubscriptionID: sub.ID, Metric: "api_calls", Quantity: 10})
	_, _ = mgr.RecordUsage(ctx, billow.RecordUsageInput{SubscriptionID: sub.ID, Metric: "seats", Quantity: 2})

	report, err := mgr.GetUsageReport(ctx, sub.ID)
	if err != nil {
		t.Fatalf("GetUsageReport: %v", err)
	}
	if len(report) != 2 {
		t.Errorf("len(report) = %d, want 2", len(report))
	}
}

func TestGetUsageReport_SubscriptionNotFound(t *testing.T) {
	mgr := newTestManager(t)
	_, err := mgr.GetUsageReport(context.Background(), "nonexistent")
	if !errors.Is(err, billow.ErrSubscriptionNotFound) {
		t.Errorf("err = %v, want ErrSubscriptionNotFound", err)
	}
}
