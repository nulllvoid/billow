package memory_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/nulllvoid/billow/store"
	"github.com/nulllvoid/billow/store/memory"
)

// ---------------------------------------------------------------------------
// PlanStore
// ---------------------------------------------------------------------------

func TestPlanStore_SaveAndGet(t *testing.T) {
	s := memory.NewPlanStore()
	ctx := context.Background()

	plan := &store.Plan{ID: "p1", Name: "Pro", Active: true}
	if err := s.SavePlan(ctx, plan); err != nil {
		t.Fatalf("SavePlan: %v", err)
	}

	got, err := s.GetPlan(ctx, "p1")
	if err != nil {
		t.Fatalf("GetPlan: %v", err)
	}
	if got.Name != "Pro" {
		t.Errorf("Name = %q, want %q", got.Name, "Pro")
	}
}

func TestPlanStore_Upsert(t *testing.T) {
	s := memory.NewPlanStore()
	ctx := context.Background()

	_ = s.SavePlan(ctx, &store.Plan{ID: "p1", Name: "v1"})
	_ = s.SavePlan(ctx, &store.Plan{ID: "p1", Name: "v2"}) // update

	got, _ := s.GetPlan(ctx, "p1")
	if got.Name != "v2" {
		t.Errorf("Name = %q after upsert, want %q", got.Name, "v2")
	}
}

func TestPlanStore_IsolatedCopies(t *testing.T) {
	s := memory.NewPlanStore()
	ctx := context.Background()

	original := &store.Plan{ID: "p1", Name: "Original"}
	_ = s.SavePlan(ctx, original)

	// Mutate the original struct after saving — store should be unaffected.
	original.Name = "Mutated"

	got, _ := s.GetPlan(ctx, "p1")
	if got.Name != "Original" {
		t.Errorf("store returned mutated value %q, want isolated copy", got.Name)
	}
}

func TestPlanStore_GetNotFound(t *testing.T) {
	s := memory.NewPlanStore()
	_, err := s.GetPlan(context.Background(), "nonexistent")
	var notFound *store.ErrNotFound
	if !errors.As(err, &notFound) {
		t.Errorf("err type = %T, want *store.ErrNotFound", err)
	}
}

func TestPlanStore_List(t *testing.T) {
	s := memory.NewPlanStore()
	ctx := context.Background()

	_ = s.SavePlan(ctx, &store.Plan{ID: "p1", Active: true})
	_ = s.SavePlan(ctx, &store.Plan{ID: "p2", Active: false})

	all, _ := s.ListPlans(ctx, store.PlanFilter{})
	if len(all) != 2 {
		t.Errorf("len(all) = %d, want 2", len(all))
	}

	active, _ := s.ListPlans(ctx, store.PlanFilter{ActiveOnly: true})
	if len(active) != 1 {
		t.Errorf("len(active) = %d, want 1", len(active))
	}
}

func TestPlanStore_Delete(t *testing.T) {
	s := memory.NewPlanStore()
	ctx := context.Background()
	_ = s.SavePlan(ctx, &store.Plan{ID: "p1"})

	if err := s.DeletePlan(ctx, "p1"); err != nil {
		t.Fatalf("DeletePlan: %v", err)
	}

	_, err := s.GetPlan(ctx, "p1")
	var notFound *store.ErrNotFound
	if !errors.As(err, &notFound) {
		t.Errorf("expected ErrNotFound after delete, got %v", err)
	}
}

func TestPlanStore_DeleteNotFound(t *testing.T) {
	s := memory.NewPlanStore()
	err := s.DeletePlan(context.Background(), "nonexistent")
	var notFound *store.ErrNotFound
	if !errors.As(err, &notFound) {
		t.Errorf("err type = %T, want *store.ErrNotFound", err)
	}
}

// ---------------------------------------------------------------------------
// SubscriptionStore
// ---------------------------------------------------------------------------

func TestSubscriptionStore_SaveAndGet(t *testing.T) {
	s := memory.NewSubscriptionStore()
	ctx := context.Background()

	sub := &store.Subscription{
		ID:           "s1",
		SubscriberID: "user_1",
		PlanID:       "plan_1",
		Status:       "active",
		ProviderID:   "prov_sub_1",
	}
	if err := s.SaveSubscription(ctx, sub); err != nil {
		t.Fatalf("SaveSubscription: %v", err)
	}

	got, err := s.GetSubscription(ctx, "s1")
	if err != nil {
		t.Fatalf("GetSubscription: %v", err)
	}
	if got.SubscriberID != "user_1" {
		t.Errorf("SubscriberID = %q, want %q", got.SubscriberID, "user_1")
	}
}

func TestSubscriptionStore_GetNotFound(t *testing.T) {
	s := memory.NewSubscriptionStore()
	_, err := s.GetSubscription(context.Background(), "nonexistent")
	var notFound *store.ErrNotFound
	if !errors.As(err, &notFound) {
		t.Errorf("err type = %T, want *store.ErrNotFound", err)
	}
}

func TestSubscriptionStore_GetByProviderID(t *testing.T) {
	s := memory.NewSubscriptionStore()
	ctx := context.Background()
	_ = s.SaveSubscription(ctx, &store.Subscription{ID: "s1", ProviderID: "prov_1", Status: "active", SubscriberID: "u1"})

	got, err := s.GetSubscriptionByProviderID(ctx, "prov_1")
	if err != nil {
		t.Fatalf("GetSubscriptionByProviderID: %v", err)
	}
	if got.ID != "s1" {
		t.Errorf("ID = %q, want %q", got.ID, "s1")
	}
}

func TestSubscriptionStore_GetByProviderID_NotFound(t *testing.T) {
	s := memory.NewSubscriptionStore()
	_, err := s.GetSubscriptionByProviderID(context.Background(), "nonexistent")
	var notFound *store.ErrNotFound
	if !errors.As(err, &notFound) {
		t.Errorf("err type = %T, want *store.ErrNotFound", err)
	}
}

func TestSubscriptionStore_GetActive(t *testing.T) {
	s := memory.NewSubscriptionStore()
	ctx := context.Background()

	_ = s.SaveSubscription(ctx, &store.Subscription{ID: "s1", SubscriberID: "u1", Status: "canceled"})
	_ = s.SaveSubscription(ctx, &store.Subscription{ID: "s2", SubscriberID: "u1", Status: "active"})

	got, err := s.GetActiveSubscription(ctx, "u1")
	if err != nil {
		t.Fatalf("GetActiveSubscription: %v", err)
	}
	if got.ID != "s2" {
		t.Errorf("ID = %q, want %q", got.ID, "s2")
	}
}

func TestSubscriptionStore_GetActiveTrialing(t *testing.T) {
	s := memory.NewSubscriptionStore()
	ctx := context.Background()
	_ = s.SaveSubscription(ctx, &store.Subscription{ID: "s1", SubscriberID: "u1", Status: "trialing"})

	got, err := s.GetActiveSubscription(ctx, "u1")
	if err != nil {
		t.Fatalf("GetActiveSubscription: %v", err)
	}
	if got.Status != "trialing" {
		t.Errorf("Status = %q, want trialing", got.Status)
	}
}

func TestSubscriptionStore_GetActive_NotFound(t *testing.T) {
	s := memory.NewSubscriptionStore()
	_, err := s.GetActiveSubscription(context.Background(), "nobody")
	var notFound *store.ErrNotFound
	if !errors.As(err, &notFound) {
		t.Errorf("err type = %T, want *store.ErrNotFound", err)
	}
}

func TestSubscriptionStore_List_FilterBySubscriber(t *testing.T) {
	s := memory.NewSubscriptionStore()
	ctx := context.Background()

	_ = s.SaveSubscription(ctx, &store.Subscription{ID: "s1", SubscriberID: "u1", Status: "active"})
	_ = s.SaveSubscription(ctx, &store.Subscription{ID: "s2", SubscriberID: "u2", Status: "active"})
	_ = s.SaveSubscription(ctx, &store.Subscription{ID: "s3", SubscriberID: "u1", Status: "canceled"})

	subs, _ := s.ListSubscriptions(ctx, store.SubscriptionFilter{SubscriberID: "u1"})
	if len(subs) != 2 {
		t.Errorf("len = %d, want 2", len(subs))
	}
}

func TestSubscriptionStore_List_FilterByStatus(t *testing.T) {
	s := memory.NewSubscriptionStore()
	ctx := context.Background()

	_ = s.SaveSubscription(ctx, &store.Subscription{ID: "s1", SubscriberID: "u1", Status: "active"})
	_ = s.SaveSubscription(ctx, &store.Subscription{ID: "s2", SubscriberID: "u2", Status: "canceled"})

	subs, _ := s.ListSubscriptions(ctx, store.SubscriptionFilter{Status: "active"})
	if len(subs) != 1 {
		t.Errorf("len = %d, want 1", len(subs))
	}
}

func TestSubscriptionStore_Delete(t *testing.T) {
	s := memory.NewSubscriptionStore()
	ctx := context.Background()
	_ = s.SaveSubscription(ctx, &store.Subscription{ID: "s1"})

	if err := s.DeleteSubscription(ctx, "s1"); err != nil {
		t.Fatalf("DeleteSubscription: %v", err)
	}

	_, err := s.GetSubscription(ctx, "s1")
	var notFound *store.ErrNotFound
	if !errors.As(err, &notFound) {
		t.Errorf("expected ErrNotFound after delete, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// UsageStore
// ---------------------------------------------------------------------------

func TestUsageStore_SaveAndSum(t *testing.T) {
	s := memory.NewUsageStore()
	ctx := context.Background()
	now := time.Now()

	_ = s.SaveUsageRecord(ctx, &store.UsageRecord{ID: "r1", SubscriptionID: "s1", Metric: "api_calls", Quantity: 100, RecordedAt: now})
	_ = s.SaveUsageRecord(ctx, &store.UsageRecord{ID: "r2", SubscriptionID: "s1", Metric: "api_calls", Quantity: 200, RecordedAt: now})
	_ = s.SaveUsageRecord(ctx, &store.UsageRecord{ID: "r3", SubscriptionID: "s1", Metric: "seats", Quantity: 5, RecordedAt: now})

	sum, err := s.SumUsage(ctx, "s1", "api_calls", now.Add(-time.Hour), now.Add(time.Hour))
	if err != nil {
		t.Fatalf("SumUsage: %v", err)
	}
	if sum != 300 {
		t.Errorf("sum = %d, want 300", sum)
	}
}

func TestUsageStore_SumUsage_OutsideWindow(t *testing.T) {
	s := memory.NewUsageStore()
	ctx := context.Background()
	now := time.Now()

	past := now.Add(-24 * time.Hour)
	_ = s.SaveUsageRecord(ctx, &store.UsageRecord{ID: "r1", SubscriptionID: "s1", Metric: "m", Quantity: 50, RecordedAt: past})

	sum, _ := s.SumUsage(ctx, "s1", "m", now.Add(-time.Hour), now.Add(time.Hour))
	if sum != 0 {
		t.Errorf("sum = %d, want 0 (record outside window)", sum)
	}
}

func TestUsageStore_ListRecords(t *testing.T) {
	s := memory.NewUsageStore()
	ctx := context.Background()
	now := time.Now()

	_ = s.SaveUsageRecord(ctx, &store.UsageRecord{ID: "r1", SubscriptionID: "s1", Metric: "m", Quantity: 10, RecordedAt: now})
	_ = s.SaveUsageRecord(ctx, &store.UsageRecord{ID: "r2", SubscriptionID: "s2", Metric: "m", Quantity: 20, RecordedAt: now})

	records, err := s.ListUsageRecords(ctx, "s1", now.Add(-time.Hour), now.Add(time.Hour))
	if err != nil {
		t.Fatalf("ListUsageRecords: %v", err)
	}
	if len(records) != 1 {
		t.Errorf("len = %d, want 1", len(records))
	}
	if records[0].Quantity != 10 {
		t.Errorf("Quantity = %d, want 10", records[0].Quantity)
	}
}

func TestUsageStore_ListRecords_Empty(t *testing.T) {
	s := memory.NewUsageStore()
	ctx := context.Background()
	now := time.Now()

	records, err := s.ListUsageRecords(ctx, "nonexistent", now.Add(-time.Hour), now.Add(time.Hour))
	if err != nil {
		t.Fatalf("ListUsageRecords: %v", err)
	}
	if len(records) != 0 {
		t.Errorf("expected empty slice, got %d records", len(records))
	}
}
