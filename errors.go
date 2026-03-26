package billow

import "errors"

var (
	ErrPlanNotFound         = errors.New("subscriptions: plan not found")
	ErrSubscriptionNotFound = errors.New("subscriptions: subscription not found")
	ErrAlreadySubscribed    = errors.New("subscriptions: subscriber already has an active subscription")
	ErrNotSubscribed        = errors.New("subscriptions: subscriber has no active subscription")
	ErrUsageLimitExceeded   = errors.New("subscriptions: usage limit exceeded for metric")
	ErrProviderNotSet       = errors.New("subscriptions: payment provider not configured")
	ErrInvalidWebhook       = errors.New("subscriptions: invalid or unverified webhook payload")
)
