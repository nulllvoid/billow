# Changelog

All notable changes to billow will be documented here.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

---

## [Unreleased]

## [0.1.0] — 2026-03-26

### Added
- `Manager` — central entry point for plan, subscription, usage, and webhook operations
- Plan CRUD — `CreatePlan`, `GetPlan`, `ListPlans`, `UpdatePlan`, `DeletePlan`
- Subscription lifecycle — `Subscribe`, `Cancel`, `Pause`, `Resume`, `ChangePlan`
- Metered usage — `RecordUsage`, `GetCurrentUsage`, `CheckLimit`, `GetUsageReport`
- Webhook handling — `HandleWebhook`, `OnWebhookEvent`, canonical event types
- Built-in Stripe and Razorpay event name mapping
- `provider.PaymentProvider` interface for gateway adapters
- `store.PlanStore`, `store.SubscriptionStore`, `store.UsageStore` persistence interfaces
- `store/memory` — thread-safe in-memory implementations for testing
- `examples/basic` — end-to-end demo with a mock provider

[Unreleased]: https://github.com/nulllvoid/billow/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/nulllvoid/billow/releases/tag/v0.1.0
