package billow_test

import (
	"context"
	"testing"

	billow "github.com/nulllvoid/billow"
	"github.com/nulllvoid/billow/store/memory"
)

func TestNewManager_PanicsWithoutPlans(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic when Plans is nil")
		}
	}()
	billow.NewManager(billow.Options{
		Subs:  memory.NewSubscriptionStore(),
		Usage: memory.NewUsageStore(),
	})
}

func TestNewManager_PanicsWithoutSubs(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic when Subs is nil")
		}
	}()
	billow.NewManager(billow.Options{
		Plans: memory.NewPlanStore(),
		Usage: memory.NewUsageStore(),
	})
}

func TestNewManager_PanicsWithoutUsage(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic when Usage is nil")
		}
	}()
	billow.NewManager(billow.Options{
		Plans: memory.NewPlanStore(),
		Subs:  memory.NewSubscriptionStore(),
	})
}

func TestNewManager_CustomIDGenerator(t *testing.T) {
	ids := []string{"id-plan", "id-sub", "id-usage"}
	idx := 0
	mgr := billow.NewManager(billow.Options{
		Plans: memory.NewPlanStore(),
		Subs:  memory.NewSubscriptionStore(),
		Usage: memory.NewUsageStore(),
		IDGenerator: func() string {
			id := ids[idx]
			idx++
			return id
		},
	})

	plan, err := mgr.CreatePlan(context.Background(), billow.CreatePlanInput{
		Name:     "Test",
		Amount:   0,
		Currency: "usd",
		Interval: billow.PlanIntervalMonth,
	})
	if err != nil {
		t.Fatal(err)
	}
	if plan.ID != "id-plan" {
		t.Errorf("plan.ID = %q, want %q", plan.ID, "id-plan")
	}
}

func TestNewManager_NoProvider(t *testing.T) {
	// Manager without a provider must still work for local-only operations.
	mgr := billow.NewManager(billow.Options{
		Plans: memory.NewPlanStore(),
		Subs:  memory.NewSubscriptionStore(),
		Usage: memory.NewUsageStore(),
	})

	plan, err := mgr.CreatePlan(context.Background(), billow.CreatePlanInput{
		Name:     "Free",
		Amount:   0,
		Currency: "usd",
		Interval: billow.PlanIntervalMonth,
	})
	if err != nil {
		t.Fatalf("CreatePlan without provider: %v", err)
	}
	if plan.ProviderID != "" {
		t.Errorf("expected empty ProviderID without provider, got %q", plan.ProviderID)
	}
}
