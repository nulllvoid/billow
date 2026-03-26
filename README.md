# billow

[![CI](https://github.com/nulllvoid/billow/actions/workflows/ci.yml/badge.svg)](https://github.com/nulllvoid/billow/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/nulllvoid/billow.svg)](https://pkg.go.dev/github.com/nulllvoid/billow)
[![Go Report Card](https://goreportcard.com/badge/github.com/nulllvoid/billow)](https://goreportcard.com/report/github.com/nulllvoid/billow)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)
[![Go Version](https://img.shields.io/badge/go-%3E%3D1.21-00ADD8.svg)](https://golang.org/)

**billow** is a provider-agnostic subscription management library for Go backend services.

It handles plan management, subscriber lifecycle (subscribe / cancel / pause / resume / upgrade), metered usage tracking, and webhook dispatching — all behind clean interfaces so you can swap payment gateways without rewriting business logic.

---

## Features

- **Plan CRUD** — create, list, update, and archive subscription plans
- **Full subscription lifecycle** — subscribe, cancel (immediately or at period-end), pause, resume, upgrade/downgrade
- **Metered usage tracking** — record usage events, sum per billing period, enforce plan limits
- **Webhook dispatch** — parse, normalise, and fan-out provider webhook events to your handlers
- **Provider-agnostic** — implement one interface for Stripe, Razorpay, or any other gateway
- **Pluggable persistence** — ships with an in-memory store; bring your own Postgres / Mongo / DynamoDB adapter
- **Zero external dependencies** — only the Go standard library

---

## Installation

```bash
go get github.com/nulllvoid/billow
```

Requires **Go 1.21** or later.

---

## Quick start

```go
package main

import (
    "context"
    "log"
    "net/http"

    billow "github.com/nulllvoid/billow"
    "github.com/nulllvoid/billow/store/memory"
)

func main() {
    mgr := billow.NewManager(billow.Options{
        Provider: myStripeProvider, // implements provider.PaymentProvider
        Plans:    memory.NewPlanStore(),
        Subs:     memory.NewSubscriptionStore(),
        Usage:    memory.NewUsageStore(),
    })

    // Register webhook handlers.
    mgr.OnWebhookEvent(billow.EventPaymentFailed, func(e *billow.WebhookEvent) error {
        log.Printf("payment failed for %s", e.Subscription.SubscriberID)
        return nil
    })

    ctx := context.Background()

    // Create a plan.
    pro, _ := mgr.CreatePlan(ctx, billow.CreatePlanInput{
        Name:      "Pro",
        Amount:    2900, // $29.00 in cents
        Currency:  "usd",
        Interval:  billow.PlanIntervalMonth,
        TrialDays: 14,
        Limits:    map[string]int64{"api_calls": 10_000},
    })

    // Subscribe a user.
    sub, _ := mgr.Subscribe(ctx, billow.SubscribeInput{
        SubscriberID: "user_42",
        PlanID:       pro.ID,
    })

    // Record usage and check limits.
    mgr.RecordUsage(ctx, billow.RecordUsageInput{
        SubscriptionID: sub.ID,
        Metric:         "api_calls",
        Quantity:       250,
    })

    if err := mgr.CheckLimit(ctx, sub.ID, "api_calls", 1); err != nil {
        log.Println("limit exceeded:", err)
    }

    // Mount the webhook endpoint.
    http.HandleFunc("/webhooks/payments", func(w http.ResponseWriter, r *http.Request) {
        if err := mgr.HandleWebhook(r); err != nil {
            http.Error(w, err.Error(), http.StatusBadRequest)
            return
        }
        w.WriteHeader(http.StatusNoContent)
    })

    log.Fatal(http.ListenAndServe(":8080", nil))
}
```

See [examples/basic/](examples/basic/) for a fully working demo using a mock provider and in-memory stores.

---

## Architecture

```
billow.Manager          ← main entry point (plan + subscription + usage + webhook)
├── provider.PaymentProvider   ← implement once per payment gateway
├── store.PlanStore            ← persist plans
├── store.SubscriptionStore    ← persist subscriptions
└── store.UsageStore           ← persist metered usage records

store/memory/           ← in-memory implementations (testing / prototyping)
examples/basic/         ← end-to-end demo
```

---

## Implementing a payment provider

Implement `provider.PaymentProvider` and pass it to `NewManager`:

```go
type PaymentProvider interface {
    CreatePlan(ctx, plan) (providerID string, err error)
    UpdatePlan(ctx, plan) error
    DeletePlan(ctx, providerPlanID) error

    CreateSubscription(ctx, sub, plan) (providerID string, err error)
    CancelSubscription(ctx, providerSubID, immediately) error
    PauseSubscription(ctx, providerSubID) error
    ResumeSubscription(ctx, providerSubID) error
    ChangeSubscriptionPlan(ctx, providerSubID, newPlan) error

    ParseWebhook(r *http.Request) (*WebhookEvent, error)
    ReportUsage(ctx, providerSubID, metric, quantity) error
}
```

The built-in webhook handler maps common **Stripe** and **Razorpay** event names to canonical `WebhookEventType` constants. Unknown event names pass through as-is.

---

## Webhook events

| Constant | Stripe event | Razorpay event |
|---|---|---|
| `EventSubscriptionCreated` | `customer.subscription.created` | `subscription.activated` |
| `EventSubscriptionUpdated` | `customer.subscription.updated` | — |
| `EventSubscriptionCanceled` | `customer.subscription.deleted` | `subscription.cancelled` |
| `EventSubscriptionPaused` | `customer.subscription.paused` | `subscription.paused` |
| `EventSubscriptionResumed` | `customer.subscription.resumed` | `subscription.resumed` |
| `EventSubscriptionRenewed` | — | — |
| `EventPaymentSucceeded` | `invoice.payment_succeeded` | `subscription.charged` |
| `EventPaymentFailed` | `invoice.payment_failed` | — |
| `EventTrialEnding` | `customer.subscription.trial_will_end` | — |
| `EventTrialEnded` | — | — |

---

## Running tests

```bash
go test -race ./...
```

---

## Contributing

Contributions are welcome! Please read [CONTRIBUTING.md](CONTRIBUTING.md) before opening a pull request.

---

## License

billow is released under the [MIT License](LICENSE).
