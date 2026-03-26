package billow_test

import (
	"context"
	"errors"
	"testing"
	"time"

	billow "github.com/nulllvoid/billow"
)

func TestCreatePlan(t *testing.T) {
	mgr := newTestManager(t)
	plan, err := mgr.CreatePlan(context.Background(), billow.CreatePlanInput{
		Name:          "Pro",
		Description:   "Pro plan",
		Amount:        2900,
		Currency:      "usd",
		Interval:      billow.PlanIntervalMonth,
		IntervalCount: 1,
		Features:      []string{"Unlimited API calls"},
		Limits:        map[string]int64{"api_calls": 0},
	})
	if err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}
	if plan.ID == "" {
		t.Error("expected non-empty ID")
	}
	if plan.Name != "Pro" {
		t.Errorf("Name = %q, want %q", plan.Name, "Pro")
	}
	if plan.ProviderID == "" {
		t.Error("expected ProviderID to be set by provider")
	}
	if !plan.Active {
		t.Error("expected plan to be active")
	}
	if plan.IntervalCount != 1 {
		t.Errorf("IntervalCount = %d, want 1", plan.IntervalCount)
	}
	if plan.CreatedAt.IsZero() {
		t.Error("expected CreatedAt to be set")
	}
}

func TestCreatePlan_DefaultIntervalCount(t *testing.T) {
	mgr := newTestManager(t)
	plan, err := mgr.CreatePlan(context.Background(), billow.CreatePlanInput{
		Name:     "Free",
		Amount:   0,
		Currency: "usd",
		Interval: billow.PlanIntervalMonth,
		// IntervalCount intentionally omitted — should default to 1.
	})
	if err != nil {
		t.Fatal(err)
	}
	if plan.IntervalCount != 1 {
		t.Errorf("IntervalCount = %d, want 1", plan.IntervalCount)
	}
}

func TestCreatePlan_AllIntervals(t *testing.T) {
	intervals := []billow.PlanInterval{
		billow.PlanIntervalDay,
		billow.PlanIntervalWeek,
		billow.PlanIntervalMonth,
		billow.PlanIntervalYear,
	}
	for _, iv := range intervals {
		iv := iv
		t.Run(string(iv), func(t *testing.T) {
			mgr := newTestManager(t)
			plan, err := mgr.CreatePlan(context.Background(), billow.CreatePlanInput{
				Name:     "plan",
				Currency: "usd",
				Interval: iv,
			})
			if err != nil {
				t.Fatalf("CreatePlan(%s): %v", iv, err)
			}
			if plan.Interval != iv {
				t.Errorf("Interval = %q, want %q", plan.Interval, iv)
			}
		})
	}
}

func TestGetPlan(t *testing.T) {
	mgr := newTestManager(t)
	created := createTestPlan(t, mgr)

	got, err := mgr.GetPlan(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("GetPlan: %v", err)
	}
	if got.ID != created.ID {
		t.Errorf("ID = %q, want %q", got.ID, created.ID)
	}
	if got.Name != created.Name {
		t.Errorf("Name = %q, want %q", got.Name, created.Name)
	}
}

func TestGetPlan_NotFound(t *testing.T) {
	mgr := newTestManager(t)
	_, err := mgr.GetPlan(context.Background(), "nonexistent")
	if !errors.Is(err, billow.ErrPlanNotFound) {
		t.Errorf("err = %v, want ErrPlanNotFound", err)
	}
}

func TestListPlans(t *testing.T) {
	mgr := newTestManager(t)
	ctx := context.Background()

	createTestPlan(t, mgr)
	createTestPlan(t, mgr)

	plans, err := mgr.ListPlans(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(plans) != 2 {
		t.Errorf("len(plans) = %d, want 2", len(plans))
	}
}

func TestListPlans_ActiveOnly(t *testing.T) {
	mgr := newTestManager(t)
	ctx := context.Background()

	p1 := createTestPlan(t, mgr)
	createTestPlan(t, mgr)

	inactive := false
	if _, err := mgr.UpdatePlan(ctx, p1.ID, billow.UpdatePlanInput{Active: &inactive}); err != nil {
		t.Fatal(err)
	}

	active, err := mgr.ListPlans(ctx, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(active) != 1 {
		t.Errorf("len(active) = %d, want 1", len(active))
	}
}

func TestUpdatePlan(t *testing.T) {
	mgr := newTestManager(t)
	ctx := context.Background()
	created := createTestPlan(t, mgr)

	time.Sleep(2 * time.Millisecond) // ensure UpdatedAt is strictly after CreatedAt
	newName := "Pro Plus"
	newDesc := "Even better"
	updated, err := mgr.UpdatePlan(ctx, created.ID, billow.UpdatePlanInput{
		Name:        &newName,
		Description: &newDesc,
		Features:    []string{"All features"},
		Limits:      map[string]int64{"api_calls": 5000},
	})
	if err != nil {
		t.Fatalf("UpdatePlan: %v", err)
	}
	if updated.Name != newName {
		t.Errorf("Name = %q, want %q", updated.Name, newName)
	}
	if updated.Description != newDesc {
		t.Errorf("Description = %q, want %q", updated.Description, newDesc)
	}
	if updated.Limits["api_calls"] != 5000 {
		t.Errorf("Limits[api_calls] = %d, want 5000", updated.Limits["api_calls"])
	}
	if !updated.UpdatedAt.After(created.UpdatedAt) {
		t.Error("expected UpdatedAt to be bumped")
	}
}

func TestUpdatePlan_PartialPatch(t *testing.T) {
	mgr := newTestManager(t)
	ctx := context.Background()
	created := createTestPlan(t, mgr)

	// Only update the name — other fields should stay unchanged.
	newName := "Pro v2"
	updated, err := mgr.UpdatePlan(ctx, created.ID, billow.UpdatePlanInput{Name: &newName})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Amount != created.Amount {
		t.Errorf("Amount changed unexpectedly: got %d, want %d", updated.Amount, created.Amount)
	}
}

func TestUpdatePlan_NotFound(t *testing.T) {
	mgr := newTestManager(t)
	_, err := mgr.UpdatePlan(context.Background(), "nonexistent", billow.UpdatePlanInput{})
	if !errors.Is(err, billow.ErrPlanNotFound) {
		t.Errorf("err = %v, want ErrPlanNotFound", err)
	}
}

func TestDeletePlan(t *testing.T) {
	mgr := newTestManager(t)
	ctx := context.Background()
	created := createTestPlan(t, mgr)

	if err := mgr.DeletePlan(ctx, created.ID); err != nil {
		t.Fatalf("DeletePlan: %v", err)
	}

	_, err := mgr.GetPlan(ctx, created.ID)
	if !errors.Is(err, billow.ErrPlanNotFound) {
		t.Errorf("expected plan to be gone, got err=%v", err)
	}
}

func TestDeletePlan_NotFound(t *testing.T) {
	mgr := newTestManager(t)
	err := mgr.DeletePlan(context.Background(), "nonexistent")
	if !errors.Is(err, billow.ErrPlanNotFound) {
		t.Errorf("err = %v, want ErrPlanNotFound", err)
	}
}
