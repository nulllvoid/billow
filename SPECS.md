# Billow — Architecture Diagrams

A single reference for all architectural diagrams in the **billow** subscription management library. Use this as the starting point when onboarding or contributing.

---

## Table of Contents

1. [High-Level Architecture](#1-high-level-architecture)
2. [Component Interactions](#2-component-interactions)
3. [API Request Authorization Flow](#3-api-request-authorization-flow)
4. [Data Model](#4-data-model)
5. [Component Reference](#5-component-reference)

---

## 1. High-Level Architecture

The top-level view of how external clients interact with the service and how responsibility flows across layers.

![High-Level Architecture](assets/high_level_arch.png)

| Component | Role |
|---|---|
| **Client / API Gateway** | Entry point for all external requests requiring subscription validation |
| **Subscription Manager** | Orchestrator — coordinates checks, logging, and webhook dispatch |
| **Subscription Provider** | Core business logic — validates subscription rules against a plan |
| **Plan / Usage Store** | Manages current subscription state, limits, and plan details (TTL cache) |
| **Metrics Reporter** | Captures key events (API calls, quota changes) for audit and rate-limiting |
| **Storage / Database** | Persistent layer — source of truth for plans, subscriptions, and usage history |
| **Webhook Handler** | Dispatches outbound events to external systems on subscription changes |

---

## 2. Component Interactions

A layered breakdown of which component owns which responsibility and how they depend on each other.

![Component Interactions](assets/component_interactions.png)

**Key design decisions visible in this diagram:**

- The **Manager** never talks to the Store directly — it always goes through the Provider. This keeps business rules centralized.
- The **Store** is a write-through cache. Reads are served from memory; the database is the authority on sync checks.
- **Webhook dispatch** is async and fire-and-forget from the Manager's perspective — the Dispatcher owns reliability.
- **Metrics** flow from Manager → Reporter → Dispatcher, decoupling instrumentation from business logic.

---

## 3. API Request Authorization Flow

Step-by-step sequence for a single inbound API call, covering both the success path and quota-exceeded path.

![API Request Authorization Flow](assets/auth_flow.png)

**Step-by-step notes:**

| Step | From → To | What happens |
|---|---|---|
| 1 | Client → Manager | Inbound request with user context |
| 2 | Manager → Provider | Manager delegates the authorization decision |
| 3 | Provider → Store | Provider checks if the plan allows this action |
| 4 | Store → DB | Cache miss or sync check triggers a DB query |
| 5 | DB → Store | Raw plan and usage data returned |
| 6 | Store → Provider | Quota status (available / exceeded) |
| 7 | Provider → Manager | Authorization result |
| 8 | Manager (internal) | Event logged for audit / metrics |
| 9 | Manager → Provider | On success, trigger quota debit |
| 10–12 | Provider → Store → DB | Usage record persisted |
| 13 | Manager → Client | Final HTTP response |

---

## 4. Data Model

Entity-relationship diagram for the core persistence schema.

![Data Model](assets/data_model.png)

**Entity notes:**

- **USER** — Basic identity record. The subscription system is intentionally thin on user data; richer profiles live upstream.
- **PLAN** — Defines a tier. `available_features` is a JSON blob so new feature flags don't require schema migrations.
- **SUBSCRIPTION** — The join between a user and a plan for a time window. `is_active` is a fast-path flag; `end_date` is authoritative.
- **USAGE_RECORD** — Append-only time-series. `(id, timestamp)` is a composite PK to support high-frequency writes without hot-row contention.

---

## 5. Component Reference

Quick map from diagram node to Go source file.

| Diagram Node | Go File(s) | Notes |
|---|---|---|
| Subscription Manager | [manager.go](manager.go) | Central orchestrator; entry point for all public operations |
| Subscription Provider | [provider/provider.go](provider/provider.go) | Interface only — implement for Stripe, Razorpay, etc. |
| Plan / Usage Store | [store/store.go](store/store.go), [store/memory/memory.go](store/memory/memory.go) | Interface + built-in in-memory implementation |
| Plan Cache (TTL) | [plancache.go](plancache.go) | Wraps the store with a configurable TTL layer |
| Metrics Reporter | [metrics.go](metrics.go), [usage_reporter.go](usage_reporter.go) | Interface + background sweep worker |
| Webhook Dispatcher | [dispatch.go](dispatch.go), [webhook.go](webhook.go) | Async worker pool; maps internal events to provider-specific payloads |
| Core Types | [types.go](types.go) | `Plan`, `Subscription`, `UsageRecord`, `Options` structs |
| Pagination | [pagination.go](pagination.go) | Cursor-based pagination for list operations |

---

> For setup and usage see [README.md](README.md). For performance tuning and load test profiles see [SCALE_TEST.md](SCALE_TEST.md).
