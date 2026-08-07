# TraceFlow

A distributed order processing system built to demonstrate every core concept from *Distributed Tracing in Practice* (Parker et al., O'Reilly).

Four Go microservices. OpenTelemetry instrumentation. Jaeger for storage and visualization. One `docker compose up` to run everything.

---

## Tech Versions (pinned)

| Tool | Version | Notes |
|---|---|---|
| Go | 1.26 | Latest stable as of Aug 2026 |
| go.opentelemetry.io/otel | v1.38.0 | Latest OTel Go SDK |
| go.opentelemetry.io/otel/exporters/otlp/otlptracehttp | v1.38.0 | OTLP/HTTP exporter |
| OTel Collector Contrib | v0.154.0 | Needed for tail sampling processor |
| Jaeger | v2.20.0 (`jaegertracing/jaeger`) | **Jaeger v1 is EOL as of Dec 31, 2025. Use v2.** |

**Why two containers for collection?**
Jaeger v2 is itself built on the OTel Collector framework and accepts OTLP natively. But for tail-based sampling you need the `tailsamplingprocessor`, which lives in OTel Collector Contrib — not in Jaeger's embedded pipeline. So the setup is: services → OTel Collector Contrib (sampling) → Jaeger v2 (storage + UI).

---

## What This Demonstrates

| Book Concept | Implementation |
|---|---|
| Spans + Traces | Each service creates child spans linked by a shared trace ID |
| Context Propagation | `traceparent` HTTP header flows through every service call |
| Fan-out | Order Service calls Inventory + Payment in parallel goroutines |
| Span attributes | `user.id`, `order.id`, `payment.amount` tagged on spans |
| Span events | In-span logs ("stock confirmed", "payment failed") |
| Error recording | Payment failures marked on the span — visible in Jaeger |
| Sampling | OTel Collector: 100% of errored/slow traces, 10% of normal |
| Collection pipeline | OTel Collector → Jaeger |
| Analysis | Service map, latency histograms, trace comparison in Jaeger UI |

---

## Architecture

```
  ┌──────────────────────────────────────────────────────────────┐
  │                        CLIENT                                │
  │                  curl / scripts/load.sh                      │
  └───────────────────────────┬──────────────────────────────────┘
                              │  POST /checkout
                              │  (no traceparent — trace starts here)
                              ▼
  ┌──────────────────────────────────────────────────────────────┐
  │  API GATEWAY  :8080                                          │
  │                                                              │
  │  · Creates ROOT SPAN  (span_id: AAA)                         │
  │  · Tags: user.id, http.method, http.status_code              │
  │  · Injects traceparent: 00-{trace_id}-AAA-01                 │
  └───────────────────────────┬──────────────────────────────────┘
                              │  POST /order
                              │  traceparent: 00-{trace_id}-AAA-01
                              ▼
  ┌──────────────────────────────────────────────────────────────┐
  │  ORDER SERVICE  :8081                                        │
  │                                                              │
  │  · Extracts parent span AAA                                  │
  │  · Creates CHILD SPAN  (span_id: BBB)                        │
  │  · Fan-out: spawns TWO goroutines in parallel                │
  │  · Propagates context into EACH goroutine explicitly         │
  └─────────────────────┬──────────────────────┬────────────────┘
                        │                      │
           GET /stock   │                      │  POST /pay
    traceparent: BBB    │                      │  traceparent: BBB
                        ▼                      ▼
  ┌───────────────────────────┐   ┌───────────────────────────┐
  │  INVENTORY SERVICE :8082  │   │  PAYMENT SERVICE  :8083   │
  │                           │   │                           │
  │  · Child span (CCC)       │   │  · Child span (DDD)       │
  │  · Random sleep 50–500ms  │   │  · 10% random failure     │
  │  · Tags: item_id, qty     │   │  · Tags: amount, status   │
  │  · Event: "stock checked" │   │  · On error: records to   │
  │  · Returns stock count    │   │    span + returns 500     │
  └─────────────┬─────────────┘   └─────────────┬─────────────┘
                │                               │
                └───────────────┬───────────────┘
                                │  All services export spans via
                                │  OTLP/HTTP → port 4318
                                ▼
  ┌──────────────────────────────────────────────────────────────┐
  │  OTEL COLLECTOR CONTRIB  :4318                               │
  │  image: otel/opentelemetry-collector-contrib:0.154.0         │
  │                                                              │
  │  1. Receives spans from all 4 services                       │
  │  2. Buffers until full trace arrives (decision_wait: 10s)    │
  │  3. Evaluates sampling policies:                             │
  │       any span ERROR?     → KEEP  (100%)                     │
  │       any span > 400ms?   → KEEP  (100%)                     │
  │       everything else     → KEEP  (10%), DROP (90%)          │
  │  4. Forwards kept traces to Jaeger via OTLP/gRPC             │
  └───────────────────────────┬──────────────────────────────────┘
                              │  OTLP/gRPC → port 4317
                              ▼
  ┌──────────────────────────────────────────────────────────────┐
  │  JAEGER v2  :16686                                           │
  │  image: jaegertracing/jaeger:2.20.0                          │
  │                                                              │
  │  · Accepts OTLP natively (no agent tier in v2)               │
  │  · Stores traces (in-memory for dev, badger for persistent)  │
  │  · UI: service map, trace search, span detail, trace diff    │
  └──────────────────────────────────────────────────────────────┘
```

> **Jaeger v2 note:** v2 dropped the agent sidecar. Services no longer push to a local agent — spans go directly to the OTel Collector, which forwards to Jaeger. Jaeger v2 accepts OTLP on port 4317 (gRPC) and 4318 (HTTP) natively.

---

## What a Trace Looks Like

After a request completes, this is the span tree you'll see in Jaeger:

```
Trace ID: 7f3a9c4b...    Duration: 118ms    Spans: 5    Status: ERROR

  Time →   0ms     20ms     40ms     60ms     80ms    100ms    120ms
           │        │        │        │        │        │        │
  gateway  ████████████████████████████████████████████████████  118ms
  orders    ████████████████████████████████████████████████     115ms
  inventory   ██████████████████████████████████                  80ms
  payments    ███████████                                 ❌       22ms

Click on payments span:
  ┌────────────────────────────────────────────────────┐
  │ span: payments.charge                              │
  │ status:  ERROR                                     │
  │ message: "card declined: insufficient funds"       │
  │                                                    │
  │ TAGS                                               │
  │   payment.amount   = 49.99                         │
  │   payment.provider = stripe                        │
  │   http.status_code = 500                           │
  │                                                    │
  │ EVENTS                                             │
  │   +2ms  "contacting payment provider"              │
  │   +20ms "payment failed"                           │
  └────────────────────────────────────────────────────┘
```

---

## Context Propagation Flow

How the `traceparent` header threads through every HTTP call:

```
  Gateway creates root span
  ┌──────────────────────────────────┐
  │ span_id: AAA                     │
  │ outgoing header:                 │
  │   traceparent: 00-7f3a-AAA-01   │
  └──────────────┬───────────────────┘
                 │
                 ▼
  Orders extracts header, creates child
  ┌──────────────────────────────────┐
  │ parent_id: AAA                   │
  │ span_id:   BBB                   │
  │ outgoing headers (to both):      │
  │   traceparent: 00-7f3a-BBB-01   │
  └──────┬───────────────────────────┘
         │                │
         ▼                ▼
  Inventory            Payments
  ┌──────────┐        ┌──────────┐
  │ parent:  │        │ parent:  │
  │   BBB    │        │   BBB    │
  │ span: CCC│        │ span: DDD│
  └──────────┘        └──────────┘

All four spans share trace_id: 7f3a...
That's what stitches them into one trace in Jaeger.
```

---

## Sampling Decision Flow

```
  Span arrives at OTel Collector
          │
          ▼
  Buffer spans until trace is complete
  (or decision_wait: 10s elapses)
          │
          ▼
  ┌───────────────────────────────────┐
  │  Does any span have status ERROR? │
  └───────────────────────────────────┘
       YES │            │ NO
           ▼            ▼
        KEEP ✓    ┌─────────────────────────────────┐
                  │ Does any span duration > 400ms? │
                  └─────────────────────────────────┘
                       YES │            │ NO
                           ▼            ▼
                        KEEP ✓    ┌──────────────────┐
                                  │ Probabilistic 10%│
                                  └──────────────────┘
                                   10% │     │ 90%
                                       ▼     ▼
                                    KEEP ✓  DROP ✗

Result: all broken + slow traces preserved. 90% of normal traffic dropped.
Storage costs drop dramatically without losing signal.
```

---

## Project Structure

```
distributed_tracing/
├── README.md
├── book-summary.md
├── docker-compose.yml
├── otel-collector-config.yaml
├── shared/
│   └── tracing/
│       ├── tracing.go          # OTel SDK init — imported by all services
│       └── tracing_test.go     # Unit tests for tracer setup
├── services/
│   ├── gateway/
│   │   ├── main.go             # Root span, traceparent injection
│   │   └── Dockerfile          # golang:1.26 build → alpine:3.20 run
│   ├── orders/
│   │   ├── main.go             # Fan-out, goroutine context propagation
│   │   └── Dockerfile
│   ├── inventory/
│   │   ├── main.go             # Latency variance, span events
│   │   └── Dockerfile
│   └── payments/
│       ├── main.go             # Error recording, random failures
│       └── Dockerfile
└── scripts/
    └── load.sh                 # Fires 50 requests to populate Jaeger
```

---

## Build Plan

We build and verify one piece at a time. Nothing moves forward until the current step works.

```
  Phase 1          Phase 2          Phase 3          Phase 4
  Shared           Payment          Inventory        Orders
  Tracing Pkg  →   Service      →   Service      →   Service
  (foundation)     (simplest)       (latency)        (fan-out)
       │                │                │                │
   go test         curl 10x         curl 5x         curl w/
   passes          ~1 failure       latency         both svcs
                   visible          varies          running
       │                │                │                │
       └────────────────┴────────────────┴────────────────┘
                                 │
                                 ▼
                            Phase 5
                            Gateway
                            (root span)
                                 │
                            curl all 4
                            svcs running
                                 │
                                 ▼
                            Phase 6
                            Docker Compose
                            + OTel Collector
                            + Jaeger v2
                                 │
                            one trace in
                            Jaeger UI
                                 │
                                 ▼
                            Phase 7
                            Load Script
                            50 requests
                                 │
                            service map
                            + checklist ✓
```

---

### Phase 1 — Shared Tracing Package
**File:** `shared/tracing/tracing.go`

Write the OTel SDK initialization function all services will call. It sets up:
- A tracer provider with OTLP/HTTP exporter (`go.opentelemetry.io/otel/exporters/otlp/otlptracehttp v1.38.0`)
- Resource attributes (`service.name`, `service.version`) using `go.opentelemetry.io/otel/sdk/resource`
- W3C Trace Context propagator (the `traceparent` header standard)
- Returns a `shutdown` function the caller defers for graceful flush

**Verify:**
```bash
cd shared/tracing && go test ./...
# Should pass: tracer provider init, tracer creation, shutdown without panic
```

---

### Phase 2 — Payment Service (simplest)
**File:** `services/payments/main.go`

HTTP server on `:8083`. One endpoint: `POST /pay`.
- Creates a child span from incoming context
- Tags span: `payment.amount`, `payment.status`
- 10% of requests: return 500 + record error on span
- 90% of requests: return 200 + record success event on span

**Verify:**
```bash
go run ./services/payments/main.go &

# Run 10 times — expect ~1 failure
for i in $(seq 1 10); do
  curl -s -o /dev/null -w "%{http_code}\n" \
    -X POST http://localhost:8083/pay \
    -d '{"amount": 49.99}'
done

# Expected output: 9x "200", 1x "500" (roughly)
```

---

### Phase 3 — Inventory Service
**File:** `services/inventory/main.go`

HTTP server on `:8082`. One endpoint: `GET /stock`.
- Creates a child span from incoming context
- Tags span: `inventory.item_id`, `inventory.quantity`
- Random sleep 50–500ms to simulate latency variance
- Adds span event: "stock check complete"
- Returns stock count as JSON

**Verify:**
```bash
go run ./services/inventory/main.go &

# Run 5 times — response times should vary noticeably
for i in $(seq 1 5); do
  time curl -s http://localhost:8082/stock?item_id=abc123
done

# Expected: each response between 50ms–500ms, durations differ
```

---

### Phase 4 — Order Service (fan-out)
**File:** `services/orders/main.go`

HTTP server on `:8081`. One endpoint: `POST /order`.
- Extracts trace context from incoming `traceparent` header
- Creates a child span
- Calls Inventory and Payment **in parallel** using goroutines
- Propagates context correctly into each goroutine
- Waits for both, aggregates result
- Records error if either downstream failed

**Verify:**
```bash
# Services from phases 2 & 3 should still be running
go run ./services/orders/main.go &

curl -X POST http://localhost:8081/order \
  -H 'traceparent: 00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01' \
  -d '{"item_id": "abc123", "amount": 49.99, "user_id": "u42"}'

# Confirm: response includes both inventory + payment results
# Confirm: payment errors surface in the order response
```

---

### Phase 5 — API Gateway (root span)
**File:** `services/gateway/main.go`

HTTP server on `:8080`. One endpoint: `POST /checkout`.
- Creates the **root span** — trace entry point, no parent
- Injects `traceparent` into outgoing request to Order Service
- Tags span: `user.id`, `http.method`, `http.status_code`
- Returns the aggregated response to the client

**Verify:**
```bash
# All three downstream services should still be running
go run ./services/gateway/main.go &

curl -X POST http://localhost:8080/checkout \
  -d '{"item_id": "abc123", "amount": 49.99, "user_id": "u42"}'

# Confirm: end-to-end request completes
# Confirm: stdout shows spans from all 4 services with matching trace IDs
```

---

### Phase 6 — Docker Compose + OTel Collector + Jaeger v2
**Files:** `docker-compose.yml`, `otel-collector-config.yaml`, all `Dockerfile`s

Wire everything together in this order:
1. `jaegertracing/jaeger:2.20.0` — OTLP receiver on 4317, UI on 16686
2. `otel/opentelemetry-collector-contrib:0.154.0` — tail sampling → forwards to Jaeger gRPC
3. All 4 services as containers — each uses `golang:1.26` to compile, `alpine:3.20` to run

Sampling rules in `otel-collector-config.yaml`:
```yaml
processors:
  tail_sampling:
    decision_wait: 10s
    policies:
      - name: errors
        type: status_code
        status_code: { status_codes: [ERROR] }
      - name: slow-traces
        type: latency
        latency: { threshold_ms: 400 }
      - name: probabilistic-fallback
        type: probabilistic
        probabilistic: { sampling_percentage: 10 }
```

**Verify:**
```bash
docker compose up --build

curl -X POST http://localhost:8080/checkout \
  -d '{"item_id": "abc123", "amount": 49.99, "user_id": "u42"}'

open http://localhost:16686
# Search for service: gateway
# Confirm one trace appears with 5 spans across all 4 services
```

---

### Phase 7 — Load Script + Full Analysis
**File:** `scripts/load.sh`

Send 50 requests to generate enough data to see patterns.

**Verify:**
```bash
bash scripts/load.sh
open http://localhost:16686
```

Jaeger checklist — confirm all of these before calling it done:

```
  SERVICE MAP  (/dependencies tab)
  ─────────────────────────────────────────────────────────
  [ ] 4 nodes visible: gateway, orders, inventory, payments
  [ ] Directed edges: gateway→orders, orders→inventory,
      orders→payments
  [ ] Edge thickness varies with call volume

  TRACE SEARCH  (/search tab)
  ─────────────────────────────────────────────────────────
  [ ] Filter service=gateway returns results
  [ ] Filter tag=error=true shows only broken traces
  [ ] Filter min-duration=400ms shows only slow traces
  [ ] At least 4–5 errored traces visible (payment failures)
  [ ] Latency spread visible: some traces < 100ms, some > 400ms

  TRACE DETAIL  (click any trace)
  ─────────────────────────────────────────────────────────
  [ ] Full span tree: gateway → orders → inventory + payments
  [ ] Inventory and payments spans appear at the same level
      (both children of orders — parallel fan-out)
  [ ] Errored payment span shows red / ERROR status
  [ ] Span durations on timeline match expected ranges
      (inventory: 50–500ms, payments: 10–30ms)

  SPAN DETAIL  (click any span in trace view)
  ─────────────────────────────────────────────────────────
  [ ] Tags: user.id, order.id, payment.amount visible
  [ ] Events: "stock check complete", "payment failed" visible
  [ ] Process: service.name, service.version from resource attrs
```

---

## Jaeger UI Walkthrough

```
  localhost:16686
  ┌──────────────────────────────────────────────────────────────┐
  │  [Search] [System Architecture] [JSON File]                  │
  └──────────────────────────────────────────────────────────────┘

  SEARCH TAB
  ┌──────────────────────────────────────────────────────────────┐
  │  Service:   [gateway        ▼]  Operation: [all   ▼]         │
  │  Tags:      [ error=true       ]  Lookback: [1 hour ▼]       │
  │  Min Duration: [   ] Max Duration: [   ]   Limit: [20]       │
  │  [Find Traces]                                               │
  ├──────────────────────────────────────────────────────────────┤
  │  ● gateway: checkout  5 spans  118ms  2 min ago       ERROR  │
  │  ● gateway: checkout  5 spans   42ms  2 min ago              │
  │  ● gateway: checkout  5 spans  340ms  3 min ago       SLOW   │
  │  ● gateway: checkout  5 spans   67ms  3 min ago              │
  └──────────────────────────────────────────────────────────────┘

  TRACE DETAIL  (click a trace)
  ┌──────────────────────────────────────────────────────────────┐
  │  Trace 7f3a9c...   118ms   5 Spans   2026-08-07 10:00:00     │
  ├──────────────────────────────────────────────────────────────┤
  │  Service       Span Name          0ms          60ms    118ms  │
  │  ──────────────────────────────────────────────────────────  │
  │  gateway     ▼ checkout          ████████████████████████    │
  │  orders      ▼   process          ███████████████████████    │
  │  inventory       check              ██████████████           │
  │  payments        charge  ❌           ████████               │
  └──────────────────────────────────────────────────────────────┘

  SYSTEM ARCHITECTURE TAB
  ┌──────────────────────────────────────────────────────────────┐
  │                                                              │
  │    [gateway] ──────────────────► [orders]                    │
  │                                     │                        │
  │                          ┌──────────┴──────────┐            │
  │                          ▼                     ▼            │
  │                     [inventory]           [payments]         │
  │                                                              │
  │  Node size = call volume. Red tint = error rate.            │
  └──────────────────────────────────────────────────────────────┘
```

---

## Running the Project

```bash
# Start everything (Docker Compose v2 — no hyphen)
docker compose up --build

# Generate trace data
bash scripts/load.sh

# Open Jaeger UI
open http://localhost:16686

# Tear down
docker compose down
```

---

## Key Concepts Quick Reference

**Span** — one named operation with start/end time, tags, and events. The atomic unit.

**Trace** — a tree of spans sharing a trace ID. Represents one end-to-end request.

**Context Propagation** — the `traceparent` header carries trace ID + span ID across service boundaries. If it breaks anywhere, the trace splits.

**Tail-based Sampling** — the OTel Collector sees the full trace before deciding whether to keep it. Keeps errors and slow traces, drops most normal ones.

**OTLP** — OpenTelemetry Protocol. The wire format services use to send spans to the collector.

**Root span** — the first span in a trace, created by the entry point service. Has no parent span ID.

**Fan-out** — one parent span spawning multiple parallel child spans. Context must be passed explicitly into each goroutine.
