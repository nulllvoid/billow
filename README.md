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
- **Metered usage tracking** — record usage events, sum per billing period, enforce plan limits with `CheckLimit` / `CheckLimits` (batch)
- **Webhook dispatch** — parse, normalise, and fan-out provider webhook events to your handlers
- **Async webhook dispatch** — optional worker pool routes events by subscription ID, preserving per-subscription ordering
- **Persist-then-report usage** — usage records are written locally first; a background sweeper reports them to the provider crash-safely
- **Cursor-based pagination** — `ListPlansPage` / `ListSubscriptionsPage` with opaque cursors
- **Provider-agnostic** — implement one interface for Stripe, Razorpay, or any other gateway
- **Pluggable persistence** — ships with an in-memory store and a 256-shard high-throughput variant; bring your own Postgres / Mongo / DynamoDB adapter
- **Instrumentation hooks** — optional `Metrics` interface for Prometheus, Datadog, or any custom backend
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
    defer mgr.Close() // drains background goroutines on shutdown

    // Register webhook handlers (safe to call at any time, from any goroutine).
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

    // Record usage and check a single limit.
    mgr.RecordUsage(ctx, billow.RecordUsageInput{
        SubscriptionID: sub.ID,
        Metric:         "api_calls",
        Quantity:       250,
    })
    if err := mgr.CheckLimit(ctx, sub.ID, "api_calls", 1); err != nil {
        log.Println("limit exceeded:", err)
    }

    // Check multiple limits in one call (single subscription + plan fetch).
    if err := mgr.CheckLimits(ctx, billow.CheckLimitsInput{
        SubscriptionID: sub.ID,
        Deltas:         map[string]int64{"api_calls": 100, "seats": 1},
    }); err != nil {
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
billow.Manager               ← main entry point
├── provider.PaymentProvider ← implement once per payment gateway
├── store.PlanStore          ← persist plans
├── store.SubscriptionStore  ← persist subscriptions
├── store.UsageStore         ← persist metered usage records
├── dispatchPool             ← optional async webhook worker pool
├── usageReporter            ← optional persist-then-report sweeper
└── planCache                ← in-process TTL cache for hot CheckLimit path

store/memory/
├── PlanStore                ← simple mutex-guarded map
├── SubscriptionStore        ← RWMutex + secondary indexes (O(1) lookups)
├── ShardedSubscriptionStore ← 256-shard variant for high write throughput
└── UsageStore               ← nested [subID][metric] index (O(k) SumUsage)

examples/basic/              ← end-to-end demo
```

---

## Options reference

```go
billow.NewManager(billow.Options{
    // Required
    Provider: myProvider,          // payment gateway adapter
    Plans:    memory.NewPlanStore(),
    Subs:     memory.NewSubscriptionStore(),
    Usage:    memory.NewUsageStore(),

    // ID generation (default: time-ordered UUID v7 via crypto/rand)
    IDGenerator: func() string { ... },

    // Webhook event mapping (default: built-in Stripe + Razorpay map)
    // Set to replace built-ins entirely; call BuiltinEventTypes() to extend them.
    EventTypeMapper: func(providerType string) billow.WebhookEventType { ... },

    // Plan cache (default: 5 min TTL; set negative to disable)
    PlanCacheTTL: 10 * time.Minute,

    // Async webhook dispatch (default: 0 = synchronous)
    DispatchWorkers:    16,  // goroutines; events for same sub always on same worker
    DispatchQueueDepth: 256, // per-worker channel buffer

    // Persist-then-report usage sweeper
    // Active only when Usage implements store.ReportableUsageStore and Provider is set.
    UsageReportInterval: 30 * time.Second,
    UsageReportBatch:    100,

    // Instrumentation (default: no-op)
    Metrics: myPrometheusMetrics, // implements billow.Metrics
})
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

The built-in webhook handler maps common **Stripe** and **Razorpay** event names to canonical `WebhookEventType` constants. Unknown event names pass through as-is. Call `BuiltinEventTypes()` to get the default map and extend or replace it via `Options.EventTypeMapper`.

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

## Persistence store interfaces

billow ships with two in-memory `SubscriptionStore` implementations:

| Type | Use case |
|---|---|
| `memory.NewSubscriptionStore()` | Development, testing, single-instance deployments |
| `memory.NewShardedSubscriptionStore()` | High-throughput scenarios; 256 independent shards eliminate single-mutex contention |

Both implement `store.AtomicSubscriptionStore`, enabling the TOCTOU-safe subscribe path automatically.

For production, implement `store.PlanStore`, `store.SubscriptionStore`, and `store.UsageStore` against your database. To enable crash-safe provider usage reporting, also implement `store.ReportableUsageStore`.

---

## Pagination

```go
// First page
page, _ := mgr.ListPlansPage(ctx, billow.ListPlansPageInput{
    ActiveOnly: true,
    Limit:      20,
})
for _, p := range page.Items { ... }

// Next page
if page.NextCursor != "" {
    page2, _ := mgr.ListPlansPage(ctx, billow.ListPlansPageInput{
        ActiveOnly: true,
        Limit:      20,
        Cursor:     page.NextCursor,
    })
}
```

`ListSubscriptionsPage` works identically and supports `SubscriberID`, `PlanID`, and `Status` filters.

---

## Instrumentation

Implement `billow.Metrics` and pass it via `Options.Metrics`:

```go
type Metrics interface {
    DispatchDuration(eventType string, success bool, d time.Duration)
    UsageReportDuration(metric string, success bool, d time.Duration)
    PlanCacheHit(hit bool)
    WorkerQueueDepth(workerIndex int, depth int)
}
```

When `nil`, all calls are no-ops with zero allocations.

---

## Graceful shutdown

```go
// Call Close once at application shutdown.
// Waits for the dispatch pool to drain and the usage reporter to flush.
mgr.Close()
```

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
