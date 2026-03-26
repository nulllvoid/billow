// Package main demonstrates how to wire up and use the subscriptions package
// without any real payment provider (using a mock provider and the in-memory
// store).
package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"time"

	billow "github.com/nulllvoid/billow"
	"github.com/nulllvoid/billow/provider"
	"github.com/nulllvoid/billow/store/memory"
)

func main() {
	ctx := context.Background()

	// 1. Create the Manager with a mock provider and in-memory stores.
	mgr := billow.NewManager(billow.Options{
		Provider: &MockProvider{},
		Plans:    memory.NewPlanStore(),
		Subs:     memory.NewSubscriptionStore(),
		Usage:    memory.NewUsageStore(),
	})

	// 2. Register webhook event handlers.
	mgr.OnWebhookEvent(billow.EventPaymentFailed, func(e *billow.WebhookEvent) error {
		if e.Subscription != nil {
			log.Printf("[webhook] payment failed for subscriber %s — consider sending an email\n",
				e.Subscription.SubscriberID)
		}
		return nil
	})

	mgr.OnWebhookEvent(billow.EventSubscriptionRenewed, func(e *billow.WebhookEvent) error {
		log.Printf("[webhook] subscription %s renewed\n", e.ProviderSubID)
		return nil
	})

	// -----------------------------------------------------------------------
	// 3. Plan management
	// -----------------------------------------------------------------------
	free, err := mgr.CreatePlan(ctx, billow.CreatePlanInput{
		Name:          "Free",
		Description:   "For individuals getting started",
		Amount:        0,
		Currency:      "usd",
		Interval:      billow.PlanIntervalMonth,
		Features:      []string{"Up to 1,000 API calls/month", "Community support"},
		Limits:        map[string]int64{"api_calls": 1000},
	})
	mustNil(err)
	log.Printf("created plan: %s (id=%s)\n", free.Name, free.ID)

	pro, err := mgr.CreatePlan(ctx, billow.CreatePlanInput{
		Name:          "Pro",
		Description:   "For growing teams",
		Amount:        2900, // $29.00
		Currency:      "usd",
		Interval:      billow.PlanIntervalMonth,
		TrialDays:     14,
		Features:      []string{"Unlimited API calls", "Priority support", "Custom webhooks"},
		Limits:        map[string]int64{"api_calls": 0, "seats": 10}, // 0 = unlimited api_calls
	})
	mustNil(err)
	log.Printf("created plan: %s (id=%s)\n", pro.Name, pro.ID)

	plans, _ := mgr.ListPlans(ctx, true)
	log.Printf("active plans: %d\n", len(plans))

	// -----------------------------------------------------------------------
	// 4. Subscriber lifecycle
	// -----------------------------------------------------------------------
	sub, err := mgr.Subscribe(ctx, billow.SubscribeInput{
		SubscriberID: "user_42",
		PlanID:       pro.ID,
	})
	mustNil(err)
	log.Printf("subscribed user_42 → plan %s, status=%s, trial_end=%v\n",
		sub.PlanID, sub.Status, sub.TrialEnd)

	// Verify active check
	active, err := mgr.GetActiveSubscription(ctx, "user_42")
	mustNil(err)
	log.Printf("user_42 active: %v\n", active.IsActive())

	// Upgrade to a new plan (in production, this would be a different plan)
	_, err = mgr.ChangePlan(ctx, billow.ChangePlanInput{
		SubscriptionID: sub.ID,
		NewPlanID:      free.ID,
	})
	mustNil(err)
	log.Printf("downgraded user_42 → plan %s\n", free.ID)

	// Pause and resume
	paused, err := mgr.Pause(ctx, sub.ID)
	mustNil(err)
	log.Printf("paused: status=%s\n", paused.Status)

	resumed, err := mgr.Resume(ctx, sub.ID)
	mustNil(err)
	log.Printf("resumed: status=%s\n", resumed.Status)

	// Cancel at period end
	canceled, err := mgr.Cancel(ctx, billow.CancelInput{
		SubscriptionID: sub.ID,
		Immediately:    false,
	})
	mustNil(err)
	log.Printf("cancel scheduled at period end, canceled_at=%v\n", canceled.CanceledAt)

	// -----------------------------------------------------------------------
	// 5. Usage tracking
	// -----------------------------------------------------------------------
	// Re-subscribe on free plan for usage demo.
	sub2, err := mgr.Subscribe(ctx, billow.SubscribeInput{
		SubscriberID: "user_99",
		PlanID:       free.ID,
	})
	mustNil(err)

	_, err = mgr.RecordUsage(ctx, billow.RecordUsageInput{
		SubscriptionID:   sub2.ID,
		Metric:           "api_calls",
		Quantity:         500,
		ReportToProvider: false,
	})
	mustNil(err)

	total, err := mgr.GetCurrentUsage(ctx, sub2.ID, "api_calls")
	mustNil(err)
	log.Printf("user_99 api_calls this period: %d\n", total)

	// Check limit before allowing another 600 calls (would exceed 1000 limit).
	if err := mgr.CheckLimit(ctx, sub2.ID, "api_calls", 600); err != nil {
		log.Printf("limit check: %v\n", err)
	}

	// -----------------------------------------------------------------------
	// 6. Webhook endpoint (illustrative — not actually listening)
	// -----------------------------------------------------------------------
	http.HandleFunc("/webhooks/payments", func(w http.ResponseWriter, r *http.Request) {
		if err := mgr.HandleWebhook(r); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

	fmt.Println("\nAll done. Mount /webhooks/payments on your router and start serving.")
}

func mustNil(err error) {
	if err != nil {
		log.Fatalf("unexpected error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// MockProvider — a no-op provider for local development and tests.
// Replace with a real provider (e.g. a Stripe implementation) in production.
// ---------------------------------------------------------------------------

type MockProvider struct{}

func (p *MockProvider) CreatePlan(_ context.Context, plan *provider.Plan) (string, error) {
	return "mock_plan_" + plan.Name, nil
}

func (p *MockProvider) UpdatePlan(_ context.Context, _ *provider.Plan) error { return nil }

func (p *MockProvider) DeletePlan(_ context.Context, _ string) error { return nil }

func (p *MockProvider) CreateSubscription(_ context.Context, sub *provider.Subscription, _ *provider.Plan) (string, error) {
	return "mock_sub_" + sub.SubscriberID, nil
}

func (p *MockProvider) CancelSubscription(_ context.Context, _ string, _ bool) error { return nil }

func (p *MockProvider) PauseSubscription(_ context.Context, _ string) error { return nil }

func (p *MockProvider) ResumeSubscription(_ context.Context, _ string) error { return nil }

func (p *MockProvider) ChangeSubscriptionPlan(_ context.Context, _ string, _ *provider.Plan) error {
	return nil
}

func (p *MockProvider) ParseWebhook(r *http.Request) (*provider.WebhookEvent, error) {
	// A real implementation validates the provider signature here.
	return &provider.WebhookEvent{
		ID:            "mock_evt_1",
		Type:          "invoice.payment_succeeded",
		ProviderSubID: r.Header.Get("X-Mock-Sub-ID"),
		OccurredAt:    time.Now(),
	}, nil
}

func (p *MockProvider) ReportUsage(_ context.Context, _ string, _ string, _ int64) error {
	return nil
}
