package billow

import "time"

// ---------------------------------------------------------------------------
// Plan
// ---------------------------------------------------------------------------

// PlanInterval represents the billing interval for a plan.
type PlanInterval string

const (
	PlanIntervalDay   PlanInterval = "day"
	PlanIntervalWeek  PlanInterval = "week"
	PlanIntervalMonth PlanInterval = "month"
	PlanIntervalYear  PlanInterval = "year"
)

// Plan defines a subscription tier (e.g. Free, Pro, Enterprise).
type Plan struct {
	ID            string            // internal ID
	ProviderID    string            // payment provider's plan/price ID
	Name          string
	Description   string
	Amount        int64             // smallest currency unit (cents, paise, etc.)
	Currency      string            // ISO 4217 (e.g. "usd", "inr")
	Interval      PlanInterval
	IntervalCount int               // e.g. 3 + Month = billed every 3 months
	TrialDays     int
	Features      []string          // human-readable feature list
	Limits        map[string]int64  // metric name → limit (0 = unlimited)
	Metadata      map[string]string
	Active        bool
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// ---------------------------------------------------------------------------
// Subscription
// ---------------------------------------------------------------------------

// SubscriptionStatus represents the lifecycle state of a subscription.
type SubscriptionStatus string

const (
	StatusTrialing SubscriptionStatus = "trialing"
	StatusActive   SubscriptionStatus = "active"
	StatusPaused   SubscriptionStatus = "paused"
	StatusPastDue  SubscriptionStatus = "past_due"
	StatusCanceled SubscriptionStatus = "canceled"
	StatusExpired  SubscriptionStatus = "expired"
)

// Subscription is an active (or historical) subscription for a subscriber.
type Subscription struct {
	ID                 string
	ProviderID         string             // payment provider's subscription ID
	SubscriberID       string             // your user/tenant/org ID
	PlanID             string
	Status             SubscriptionStatus
	CurrentPeriodStart time.Time
	CurrentPeriodEnd   time.Time
	TrialStart         *time.Time
	TrialEnd           *time.Time
	CanceledAt         *time.Time
	PausedAt           *time.Time
	Metadata           map[string]string
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

// IsActive returns true when the subscription allows feature access.
func (s *Subscription) IsActive() bool {
	return s.Status == StatusActive || s.Status == StatusTrialing
}

// ---------------------------------------------------------------------------
// Usage
// ---------------------------------------------------------------------------

// UsageRecord tracks a single metered usage event.
type UsageRecord struct {
	ID                 string
	SubscriptionID     string
	Metric             string            // e.g. "api_calls", "seats", "gb_storage"
	Quantity           int64
	RecordedAt         time.Time
	Metadata           map[string]string
	// ProviderReportedAt is non-nil once this record has been successfully
	// pushed to the payment provider. Nil means the report is still pending.
	ProviderReportedAt *time.Time
}

// ---------------------------------------------------------------------------
// Webhooks
// ---------------------------------------------------------------------------

// WebhookEventType is the canonical event name emitted by the Manager.
type WebhookEventType string

const (
	EventSubscriptionCreated  WebhookEventType = "subscription.created"
	EventSubscriptionUpdated  WebhookEventType = "subscription.updated"
	EventSubscriptionCanceled WebhookEventType = "subscription.canceled"
	EventSubscriptionPaused   WebhookEventType = "subscription.paused"
	EventSubscriptionResumed  WebhookEventType = "subscription.resumed"
	EventSubscriptionRenewed  WebhookEventType = "subscription.renewed"
	EventPaymentSucceeded     WebhookEventType = "payment.succeeded"
	EventPaymentFailed        WebhookEventType = "payment.failed"
	EventTrialEnding          WebhookEventType = "trial.ending"
	EventTrialEnded           WebhookEventType = "trial.ended"
)

// WebhookEvent is the normalised event the Manager passes to your handlers.
type WebhookEvent struct {
	ID            string           // provider's event ID
	Type          WebhookEventType
	ProviderSubID string           // provider's subscription ID
	Subscription  *Subscription    // nil when not yet matched
	Data          map[string]any   // raw provider payload (for custom handling)
	OccurredAt    time.Time
}

// WebhookHandler is a callback registered for a specific event type.
type WebhookHandler func(event *WebhookEvent) error
