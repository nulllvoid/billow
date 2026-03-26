// Package provider defines the PaymentProvider interface.
// Implement this interface once for each payment gateway (Stripe, Razorpay,
// PayPal, etc.) and pass the implementation to subscriptions.NewManager.
package provider

import (
	"context"
	"net/http"
	"time"
)

// Plan mirrors subscriptions.Plan but is defined here to avoid a circular
// import. The Manager bridges between the two representations.
type Plan struct {
	ID            string
	ProviderID    string
	Name          string
	Amount        int64
	Currency      string
	Interval      string
	IntervalCount int
	TrialDays     int
	Metadata      map[string]string
}

// Subscription mirrors subscriptions.Subscription for the same reason.
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
	Metadata           map[string]string
}

// WebhookEvent is the raw normalised event returned by ParseWebhook.
type WebhookEvent struct {
	ID            string
	Type          string         // provider-native event name
	ProviderSubID string
	Data          map[string]any
	OccurredAt    time.Time
}

// PaymentProvider is the single interface you implement per payment gateway.
// Each method receives a context so you can respect timeouts and cancellations.
type PaymentProvider interface {
	// ---- Plans ----

	// CreatePlan creates the plan on the payment provider and returns the
	// provider-assigned ID.
	CreatePlan(ctx context.Context, plan *Plan) (providerID string, err error)

	// UpdatePlan updates a mutable plan field on the provider (e.g. metadata).
	// Note: most providers do not allow amount/interval changes; create a new
	// plan and migrate subscribers instead.
	UpdatePlan(ctx context.Context, plan *Plan) error

	// DeletePlan archives or removes the plan on the provider.
	DeletePlan(ctx context.Context, providerPlanID string) error

	// ---- Subscriptions ----

	// CreateSubscription creates a new subscription on the provider for the
	// given subscriber + plan and returns the provider-assigned subscription ID.
	CreateSubscription(ctx context.Context, sub *Subscription, plan *Plan) (providerID string, err error)

	// CancelSubscription cancels the subscription.
	// If immediately is false the subscription stays active until period end.
	CancelSubscription(ctx context.Context, providerSubID string, immediately bool) error

	// PauseSubscription pauses billing (provider must support pause).
	PauseSubscription(ctx context.Context, providerSubID string) error

	// ResumeSubscription resumes a paused subscription.
	ResumeSubscription(ctx context.Context, providerSubID string) error

	// ChangeSubscriptionPlan upgrades or downgrades to a different plan.
	ChangeSubscriptionPlan(ctx context.Context, providerSubID string, newPlan *Plan) error

	// ---- Webhooks ----

	// ParseWebhook validates the incoming HTTP request (signature check, etc.)
	// and returns a normalised WebhookEvent. Return ErrInvalidWebhook when
	// validation fails.
	ParseWebhook(r *http.Request) (*WebhookEvent, error)

	// ---- Usage / Metered billing ----

	// ReportUsage reports metered usage to the provider (only needed for
	// providers that support metered billing, e.g. Stripe metered prices).
	// Implementations that do not support this may return nil.
	ReportUsage(ctx context.Context, providerSubID string, metric string, quantity int64) error
}
