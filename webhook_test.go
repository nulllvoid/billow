package billow_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	billow "github.com/nulllvoid/billow"
	"github.com/nulllvoid/billow/store/memory"
)

func webhookRequest(eventType, providerSubID string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/webhooks/payments", nil)
	r.Header.Set("X-Event-Type", eventType)
	r.Header.Set("X-Sub-ID", providerSubID)
	return r
}

func TestHandleWebhook_NoProvider(t *testing.T) {
	mgr := billow.NewManager(billow.Options{
		Plans: memory.NewPlanStore(),
		Subs:  memory.NewSubscriptionStore(),
		Usage: memory.NewUsageStore(),
	})
	err := mgr.HandleWebhook(webhookRequest("invoice.payment_succeeded", ""))
	if err == nil {
		t.Fatal("expected error when no provider is configured")
	}
}

func TestHandleWebhook_DispatchesHandler(t *testing.T) {
	mgr := newTestManager(t)

	handled := false
	mgr.OnWebhookEvent(billow.EventPaymentSucceeded, func(e *billow.WebhookEvent) error {
		handled = true
		return nil
	})

	if err := mgr.HandleWebhook(webhookRequest("invoice.payment_succeeded", "")); err != nil {
		t.Fatalf("HandleWebhook: %v", err)
	}
	if !handled {
		t.Error("expected handler to be called")
	}
}

func TestHandleWebhook_PaymentFailed_SetsStatusPastDue(t *testing.T) {
	mgr := newTestManager(t)
	ctx := context.Background()

	plan := createTestPlan(t, mgr)
	sub := testSubscribe(t, mgr, "user_w1", plan.ID)

	if err := mgr.HandleWebhook(webhookRequest("invoice.payment_failed", sub.ProviderID)); err != nil {
		t.Fatalf("HandleWebhook: %v", err)
	}

	updated, err := mgr.GetSubscription(ctx, sub.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Status != billow.StatusPastDue {
		t.Errorf("Status = %q, want %q", updated.Status, billow.StatusPastDue)
	}
}

func TestHandleWebhook_PaymentSucceeded_ClearsPastDue(t *testing.T) {
	mgr := newTestManager(t)
	ctx := context.Background()

	plan := createTestPlan(t, mgr)
	sub := testSubscribe(t, mgr, "user_w2", plan.ID)

	// First simulate payment failure.
	_ = mgr.HandleWebhook(webhookRequest("invoice.payment_failed", sub.ProviderID))

	// Then simulate payment success.
	if err := mgr.HandleWebhook(webhookRequest("invoice.payment_succeeded", sub.ProviderID)); err != nil {
		t.Fatalf("HandleWebhook: %v", err)
	}

	updated, _ := mgr.GetSubscription(ctx, sub.ID)
	if updated.Status != billow.StatusActive {
		t.Errorf("Status = %q, want active after payment succeeded", updated.Status)
	}
}

func TestHandleWebhook_Canceled(t *testing.T) {
	mgr := newTestManager(t)
	ctx := context.Background()

	plan := createTestPlan(t, mgr)
	sub := testSubscribe(t, mgr, "user_w3", plan.ID)

	if err := mgr.HandleWebhook(webhookRequest("customer.subscription.deleted", sub.ProviderID)); err != nil {
		t.Fatalf("HandleWebhook: %v", err)
	}

	updated, _ := mgr.GetSubscription(ctx, sub.ID)
	if updated.Status != billow.StatusCanceled {
		t.Errorf("Status = %q, want canceled", updated.Status)
	}
	if updated.CanceledAt == nil {
		t.Error("expected CanceledAt to be set")
	}
}

func TestHandleWebhook_Paused(t *testing.T) {
	mgr := newTestManager(t)
	ctx := context.Background()

	plan := createTestPlan(t, mgr)
	sub := testSubscribe(t, mgr, "user_w4", plan.ID)

	if err := mgr.HandleWebhook(webhookRequest("customer.subscription.paused", sub.ProviderID)); err != nil {
		t.Fatalf("HandleWebhook: %v", err)
	}

	updated, _ := mgr.GetSubscription(ctx, sub.ID)
	if updated.Status != billow.StatusPaused {
		t.Errorf("Status = %q, want paused", updated.Status)
	}
}

func TestHandleWebhook_Resumed(t *testing.T) {
	mgr := newTestManager(t)
	ctx := context.Background()

	plan := createTestPlan(t, mgr)
	sub := testSubscribe(t, mgr, "user_w5", plan.ID)

	_ = mgr.HandleWebhook(webhookRequest("customer.subscription.paused", sub.ProviderID))
	if err := mgr.HandleWebhook(webhookRequest("customer.subscription.resumed", sub.ProviderID)); err != nil {
		t.Fatalf("HandleWebhook: %v", err)
	}

	updated, _ := mgr.GetSubscription(ctx, sub.ID)
	if updated.Status != billow.StatusActive {
		t.Errorf("Status = %q, want active after resume", updated.Status)
	}
	if updated.PausedAt != nil {
		t.Error("expected PausedAt to be cleared")
	}
}

func TestHandleWebhook_TrialEnded(t *testing.T) {
	mgr := newTestManager(t)
	ctx := context.Background()

	trialPlan, err := mgr.CreatePlan(ctx, billow.CreatePlanInput{
		Name:      "Trial",
		Amount:    999,
		Currency:  "usd",
		Interval:  billow.PlanIntervalMonth,
		TrialDays: 14,
	})
	if err != nil {
		t.Fatal(err)
	}
	sub := testSubscribe(t, mgr, "user_w6", trialPlan.ID)

	// Simulate trial ending via Razorpay event name.
	if err := mgr.HandleWebhook(webhookRequest("subscription.charged", sub.ProviderID)); err != nil {
		t.Fatalf("HandleWebhook: %v", err)
	}
	// EventPaymentSucceeded during trialing — should become active.
	updated, _ := mgr.GetSubscription(ctx, sub.ID)
	if updated.Status != billow.StatusActive {
		t.Errorf("Status = %q, want active after trial charge", updated.Status)
	}
}

func TestHandleWebhook_MultipleHandlers(t *testing.T) {
	mgr := newTestManager(t)

	count := 0
	mgr.OnWebhookEvent(billow.EventSubscriptionCreated, func(_ *billow.WebhookEvent) error { count++; return nil })
	mgr.OnWebhookEvent(billow.EventSubscriptionCreated, func(_ *billow.WebhookEvent) error { count++; return nil })

	// Razorpay "subscription.activated" maps to EventSubscriptionCreated.
	if err := mgr.HandleWebhook(webhookRequest("subscription.activated", "")); err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Errorf("count = %d, want 2 (both handlers called)", count)
	}
}

func TestHandleWebhook_UnknownSubID_StillDispatches(t *testing.T) {
	mgr := newTestManager(t)

	handled := false
	mgr.OnWebhookEvent(billow.EventPaymentFailed, func(_ *billow.WebhookEvent) error {
		handled = true
		return nil
	})

	// ProviderSubID that doesn't exist in the store — handler still fires.
	if err := mgr.HandleWebhook(webhookRequest("invoice.payment_failed", "unknown_sub")); err != nil {
		t.Fatalf("HandleWebhook: %v", err)
	}
	if !handled {
		t.Error("expected handler to be called even for unknown subscription")
	}
}
