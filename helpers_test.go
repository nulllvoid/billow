package billow_test

import (
	"context"
	"net/http"
	"testing"

	billow "github.com/nulllvoid/billow"
	"github.com/nulllvoid/billow/provider"
	"github.com/nulllvoid/billow/store/memory"
)

// newTestManager returns a Manager wired with a noopProvider and in-memory stores.
func newTestManager(t *testing.T) *billow.Manager {
	t.Helper()
	return billow.NewManager(billow.Options{
		Provider: &noopProvider{},
		Plans:    memory.NewPlanStore(),
		Subs:     memory.NewSubscriptionStore(),
		Usage:    memory.NewUsageStore(),
	})
}

// createTestPlan creates a Pro plan with a 1 000 api_calls limit.
func createTestPlan(t *testing.T, mgr *billow.Manager) *billow.Plan {
	t.Helper()
	plan, err := mgr.CreatePlan(context.Background(), billow.CreatePlanInput{
		Name:     "Pro",
		Amount:   2900,
		Currency: "usd",
		Interval: billow.PlanIntervalMonth,
		Limits:   map[string]int64{"api_calls": 1000},
	})
	if err != nil {
		t.Fatalf("createTestPlan: %v", err)
	}
	return plan
}

// testSubscribe subscribes subscriberID to planID and returns the subscription.
func testSubscribe(t *testing.T, mgr *billow.Manager, subscriberID, planID string) *billow.Subscription {
	t.Helper()
	sub, err := mgr.Subscribe(context.Background(), billow.SubscribeInput{
		SubscriberID: subscriberID,
		PlanID:       planID,
	})
	if err != nil {
		t.Fatalf("testSubscribe(%s): %v", subscriberID, err)
	}
	return sub
}

// noopProvider is a no-op PaymentProvider for use in tests.
// ParseWebhook reads X-Event-Type and X-Sub-ID request headers so tests can
// simulate any provider event without a real HTTP payload.
type noopProvider struct{}

func (p *noopProvider) CreatePlan(_ context.Context, plan *provider.Plan) (string, error) {
	return "prov_plan_" + plan.Name, nil
}
func (p *noopProvider) UpdatePlan(_ context.Context, _ *provider.Plan) error { return nil }
func (p *noopProvider) DeletePlan(_ context.Context, _ string) error         { return nil }
func (p *noopProvider) CreateSubscription(_ context.Context, sub *provider.Subscription, _ *provider.Plan) (string, error) {
	return "prov_sub_" + sub.SubscriberID, nil
}
func (p *noopProvider) CancelSubscription(_ context.Context, _ string, _ bool) error { return nil }
func (p *noopProvider) PauseSubscription(_ context.Context, _ string) error          { return nil }
func (p *noopProvider) ResumeSubscription(_ context.Context, _ string) error         { return nil }
func (p *noopProvider) ChangeSubscriptionPlan(_ context.Context, _ string, _ *provider.Plan) error {
	return nil
}
func (p *noopProvider) ParseWebhook(r *http.Request) (*provider.WebhookEvent, error) {
	return &provider.WebhookEvent{
		ID:            "evt_test",
		Type:          r.Header.Get("X-Event-Type"),
		ProviderSubID: r.Header.Get("X-Sub-ID"),
	}, nil
}
func (p *noopProvider) ReportUsage(_ context.Context, _ string, _ string, _ int64) error {
	return nil
}
