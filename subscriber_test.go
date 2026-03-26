package billow_test

import (
	"context"
	"errors"
	"testing"

	billow "github.com/nulllvoid/billow"
)

func TestSubscribe(t *testing.T) {
	mgr := newTestManager(t)
	plan := createTestPlan(t, mgr)

	sub, err := mgr.Subscribe(context.Background(), billow.SubscribeInput{
		SubscriberID: "user_1",
		PlanID:       plan.ID,
	})
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	if sub.ID == "" {
		t.Error("expected non-empty subscription ID")
	}
	if sub.SubscriberID != "user_1" {
		t.Errorf("SubscriberID = %q, want %q", sub.SubscriberID, "user_1")
	}
	if sub.Status != billow.StatusActive {
		t.Errorf("Status = %q, want %q", sub.Status, billow.StatusActive)
	}
	if !sub.IsActive() {
		t.Error("IsActive() = false, want true")
	}
	if sub.ProviderID == "" {
		t.Error("expected ProviderID to be set")
	}
}

func TestSubscribe_WithTrial(t *testing.T) {
	mgr := newTestManager(t)
	ctx := context.Background()

	plan, err := mgr.CreatePlan(ctx, billow.CreatePlanInput{
		Name:      "Trial Plan",
		Amount:    1000,
		Currency:  "usd",
		Interval:  billow.PlanIntervalMonth,
		TrialDays: 14,
	})
	if err != nil {
		t.Fatal(err)
	}

	sub, err := mgr.Subscribe(ctx, billow.SubscribeInput{
		SubscriberID: "user_trial",
		PlanID:       plan.ID,
	})
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	if sub.Status != billow.StatusTrialing {
		t.Errorf("Status = %q, want %q", sub.Status, billow.StatusTrialing)
	}
	if sub.TrialStart == nil || sub.TrialEnd == nil {
		t.Error("expected TrialStart and TrialEnd to be set")
	}
	if !sub.IsActive() {
		t.Error("IsActive() = false during trial, want true")
	}
}

func TestSubscribe_AlreadySubscribed(t *testing.T) {
	mgr := newTestManager(t)
	plan := createTestPlan(t, mgr)

	testSubscribe(t, mgr, "user_2", plan.ID)

	_, err := mgr.Subscribe(context.Background(), billow.SubscribeInput{
		SubscriberID: "user_2",
		PlanID:       plan.ID,
	})
	if !errors.Is(err, billow.ErrAlreadySubscribed) {
		t.Errorf("err = %v, want ErrAlreadySubscribed", err)
	}
}

func TestSubscribe_PlanNotFound(t *testing.T) {
	mgr := newTestManager(t)
	_, err := mgr.Subscribe(context.Background(), billow.SubscribeInput{
		SubscriberID: "user_3",
		PlanID:       "nonexistent",
	})
	if !errors.Is(err, billow.ErrPlanNotFound) {
		t.Errorf("err = %v, want ErrPlanNotFound", err)
	}
}

func TestGetSubscription(t *testing.T) {
	mgr := newTestManager(t)
	plan := createTestPlan(t, mgr)
	created := testSubscribe(t, mgr, "user_4", plan.ID)

	got, err := mgr.GetSubscription(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("GetSubscription: %v", err)
	}
	if got.ID != created.ID {
		t.Errorf("ID = %q, want %q", got.ID, created.ID)
	}
}

func TestGetSubscription_NotFound(t *testing.T) {
	mgr := newTestManager(t)
	_, err := mgr.GetSubscription(context.Background(), "nonexistent")
	if !errors.Is(err, billow.ErrSubscriptionNotFound) {
		t.Errorf("err = %v, want ErrSubscriptionNotFound", err)
	}
}

func TestGetActiveSubscription(t *testing.T) {
	mgr := newTestManager(t)
	plan := createTestPlan(t, mgr)
	testSubscribe(t, mgr, "user_5", plan.ID)

	active, err := mgr.GetActiveSubscription(context.Background(), "user_5")
	if err != nil {
		t.Fatalf("GetActiveSubscription: %v", err)
	}
	if active.SubscriberID != "user_5" {
		t.Errorf("SubscriberID = %q, want %q", active.SubscriberID, "user_5")
	}
}

func TestGetActiveSubscription_NotFound(t *testing.T) {
	mgr := newTestManager(t)
	_, err := mgr.GetActiveSubscription(context.Background(), "nobody")
	if !errors.Is(err, billow.ErrNotSubscribed) {
		t.Errorf("err = %v, want ErrNotSubscribed", err)
	}
}

func TestListSubscriptions(t *testing.T) {
	mgr := newTestManager(t)
	plan := createTestPlan(t, mgr)
	testSubscribe(t, mgr, "user_6", plan.ID)

	subs, err := mgr.ListSubscriptions(context.Background(), "user_6")
	if err != nil {
		t.Fatal(err)
	}
	if len(subs) != 1 {
		t.Errorf("len(subs) = %d, want 1", len(subs))
	}
}

func TestCancel_Immediately(t *testing.T) {
	mgr := newTestManager(t)
	plan := createTestPlan(t, mgr)
	sub := testSubscribe(t, mgr, "user_7", plan.ID)

	canceled, err := mgr.Cancel(context.Background(), billow.CancelInput{
		SubscriptionID: sub.ID,
		Immediately:    true,
	})
	if err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	if canceled.Status != billow.StatusCanceled {
		t.Errorf("Status = %q, want %q", canceled.Status, billow.StatusCanceled)
	}
	if canceled.CanceledAt == nil {
		t.Error("expected CanceledAt to be set")
	}
}

func TestCancel_AtPeriodEnd(t *testing.T) {
	mgr := newTestManager(t)
	plan := createTestPlan(t, mgr)
	sub := testSubscribe(t, mgr, "user_8", plan.ID)

	canceled, err := mgr.Cancel(context.Background(), billow.CancelInput{
		SubscriptionID: sub.ID,
		Immediately:    false,
	})
	if err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	// Status stays active when cancel-at-period-end; provider webhook changes it later.
	if canceled.Status != billow.StatusActive {
		t.Errorf("Status = %q, want active (cancel at period end)", canceled.Status)
	}
	if canceled.CanceledAt == nil {
		t.Error("expected CanceledAt to be set")
	}
}

func TestCancel_NotFound(t *testing.T) {
	mgr := newTestManager(t)
	_, err := mgr.Cancel(context.Background(), billow.CancelInput{SubscriptionID: "nonexistent"})
	if !errors.Is(err, billow.ErrSubscriptionNotFound) {
		t.Errorf("err = %v, want ErrSubscriptionNotFound", err)
	}
}

func TestPause(t *testing.T) {
	mgr := newTestManager(t)
	plan := createTestPlan(t, mgr)
	sub := testSubscribe(t, mgr, "user_9", plan.ID)

	paused, err := mgr.Pause(context.Background(), sub.ID)
	if err != nil {
		t.Fatalf("Pause: %v", err)
	}
	if paused.Status != billow.StatusPaused {
		t.Errorf("Status = %q, want %q", paused.Status, billow.StatusPaused)
	}
	if paused.PausedAt == nil {
		t.Error("expected PausedAt to be set")
	}
}

func TestPause_NotFound(t *testing.T) {
	mgr := newTestManager(t)
	_, err := mgr.Pause(context.Background(), "nonexistent")
	if !errors.Is(err, billow.ErrSubscriptionNotFound) {
		t.Errorf("err = %v, want ErrSubscriptionNotFound", err)
	}
}

func TestResume(t *testing.T) {
	mgr := newTestManager(t)
	plan := createTestPlan(t, mgr)
	sub := testSubscribe(t, mgr, "user_10", plan.ID)

	paused, _ := mgr.Pause(context.Background(), sub.ID)

	resumed, err := mgr.Resume(context.Background(), paused.ID)
	if err != nil {
		t.Fatalf("Resume: %v", err)
	}
	if resumed.Status != billow.StatusActive {
		t.Errorf("Status = %q, want %q", resumed.Status, billow.StatusActive)
	}
	if resumed.PausedAt != nil {
		t.Error("expected PausedAt to be cleared on resume")
	}
}

func TestResume_NotFound(t *testing.T) {
	mgr := newTestManager(t)
	_, err := mgr.Resume(context.Background(), "nonexistent")
	if !errors.Is(err, billow.ErrSubscriptionNotFound) {
		t.Errorf("err = %v, want ErrSubscriptionNotFound", err)
	}
}

func TestChangePlan(t *testing.T) {
	mgr := newTestManager(t)
	ctx := context.Background()

	plan1 := createTestPlan(t, mgr)
	plan2, err := mgr.CreatePlan(ctx, billow.CreatePlanInput{
		Name:     "Enterprise",
		Amount:   9900,
		Currency: "usd",
		Interval: billow.PlanIntervalMonth,
	})
	if err != nil {
		t.Fatal(err)
	}

	sub := testSubscribe(t, mgr, "user_11", plan1.ID)

	updated, err := mgr.ChangePlan(ctx, billow.ChangePlanInput{
		SubscriptionID: sub.ID,
		NewPlanID:      plan2.ID,
	})
	if err != nil {
		t.Fatalf("ChangePlan: %v", err)
	}
	if updated.PlanID != plan2.ID {
		t.Errorf("PlanID = %q, want %q", updated.PlanID, plan2.ID)
	}
}

func TestChangePlan_PlanNotFound(t *testing.T) {
	mgr := newTestManager(t)
	plan := createTestPlan(t, mgr)
	sub := testSubscribe(t, mgr, "user_12", plan.ID)

	_, err := mgr.ChangePlan(context.Background(), billow.ChangePlanInput{
		SubscriptionID: sub.ID,
		NewPlanID:      "nonexistent",
	})
	if !errors.Is(err, billow.ErrPlanNotFound) {
		t.Errorf("err = %v, want ErrPlanNotFound", err)
	}
}

func TestChangePlan_SubscriptionNotFound(t *testing.T) {
	mgr := newTestManager(t)
	plan := createTestPlan(t, mgr)

	_, err := mgr.ChangePlan(context.Background(), billow.ChangePlanInput{
		SubscriptionID: "nonexistent",
		NewPlanID:      plan.ID,
	})
	if !errors.Is(err, billow.ErrSubscriptionNotFound) {
		t.Errorf("err = %v, want ErrSubscriptionNotFound", err)
	}
}

func TestIsActive(t *testing.T) {
	cases := []struct {
		status billow.SubscriptionStatus
		want   bool
	}{
		{billow.StatusActive, true},
		{billow.StatusTrialing, true},
		{billow.StatusPaused, false},
		{billow.StatusPastDue, false},
		{billow.StatusCanceled, false},
		{billow.StatusExpired, false},
	}
	for _, tc := range cases {
		sub := &billow.Subscription{Status: tc.status}
		if got := sub.IsActive(); got != tc.want {
			t.Errorf("IsActive(%s) = %v, want %v", tc.status, got, tc.want)
		}
	}
}
