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
Client (curl / load.sh)
        |
        v
[API Gateway :8080]
  - Creates root span
  - Injects traceparent header
        |
        v
[Order Service :8081]
  - Creates child span
  - Fans out to both services in parallel goroutines
  - Propagates context into each goroutine
        |
        |----------------------------------|
        v                                  v
[Inventory Service :8082]        [Payment Service :8083]
  - Child span                     - Child span
  - Random latency 50–500ms        - 10% random failure rate
  - Returns stock count            - Records error on span
        |                                  |
        v                                  v
        |----------------------------------|
        v
[OTel Collector Contrib :4318]   ← otelcol/opentelemetry-collector-releases v0.154.0
  - Receives spans from all services via OTLP/HTTP
  - Buffers spans until full trace arrives (decision_wait: 10s)
  - Applies tail-based sampling:
      · keep 100% if any span has error
      · keep 100% if any span duration > 400ms
      · keep 10% of everything else
  - Forwards kept traces to Jaeger via OTLP/gRPC
        |
        v
[Jaeger v2 :16686]               ← jaegertracing/jaeger:2.20.0
  - Accepts OTLP on :4317 (gRPC) natively — no agent tier
  - Stores traces in-memory (dev) or badger (persistent)
  - UI: service map, trace search, latency analysis, trace diff
```

> **Jaeger v2 note:** v2 dropped the agent sidecar. Services no longer push to a local agent — spans go directly to the OTel Collector, which forwards to Jaeger. Jaeger v2 itself is built on the OTel Collector framework and accepts OTLP on port 4317 (gRPC) and 4318 (HTTP) out of the box.

---

## Project Structure

```
distributed_tracing/
├── README.md
├── docker-compose.yml
├── otel-collector-config.yaml
├── shared/
│   └── tracing/
│       └── tracing.go          # OTel SDK init — imported by all services
├── services/
│   ├── gateway/
│   │   ├── main.go
│   │   └── Dockerfile
│   ├── orders/
│   │   ├── main.go
│   │   └── Dockerfile
│   ├── inventory/
│   │   ├── main.go
│   │   └── Dockerfile
│   └── payments/
│       ├── main.go
│       └── Dockerfile
└── scripts/
    └── load.sh                 # Fires 50 requests to generate trace data
```

---

## Build Plan

We build and verify one piece at a time. Nothing moves forward until the current step works.

### Phase 1 — Shared Tracing Package
**File:** `shared/tracing/tracing.go`

Write the OTel SDK initialization function all services will call. It sets up:
- A tracer provider with OTLP/HTTP exporter (`go.opentelemetry.io/otel/exporters/otlp/otlptracehttp v1.38.0`)
- Resource attributes (`service.name`, `service.version`) using `go.opentelemetry.io/otel/sdk/resource`
- W3C Trace Context propagator (the `traceparent` header standard)
- Returns a `shutdown` function the caller defers for graceful flush

**Verify:** Unit test that the tracer provider initializes without error and returns a valid tracer.
```bash
cd shared/tracing && go test ./...
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
go run services/payments/main.go
curl -X POST http://localhost:8083/pay -d '{"amount": 49.99}'
# Run 10 times, confirm ~1 failure
# Confirm spans appear in stdout logs
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
go run services/inventory/main.go
curl http://localhost:8082/stock?item_id=abc123
# Run 5 times, confirm latency varies
# Check span duration reflects the sleep
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
# Start inventory and payment services first
go run services/inventory/main.go &
go run services/payments/main.go &
go run services/orders/main.go
curl -X POST http://localhost:8081/order -d '{"item_id": "abc123", "amount": 49.99, "user_id": "u42"}'
# Confirm both downstream calls happen
# Confirm errors from payment surface on order span
```

---

### Phase 5 — API Gateway (root span)
**File:** `services/gateway/main.go`

HTTP server on `:8080`. One endpoint: `POST /checkout`.
- Creates the **root span** — this is the trace entry point
- Injects `traceparent` into outgoing request to Order Service
- Tags span: `user.id`, `http.method`, `http.status_code`
- Returns the aggregated response to the client

**Verify:**
```bash
# Start all three downstream services
go run services/inventory/main.go &
go run services/payments/main.go &
go run services/orders/main.go &
go run services/gateway/main.go
curl -X POST http://localhost:8080/checkout -d '{"item_id": "abc123", "amount": 49.99, "user_id": "u42"}'
# Confirm end-to-end request works
# Confirm trace context flows through all 4 services
```

---

### Phase 6 — Docker Compose + OTel Collector + Jaeger v2
**Files:** `docker-compose.yml`, `otel-collector-config.yaml`, all `Dockerfile`s

Wire everything together in this order:
1. `jaegertracing/jaeger:2.20.0` — configure OTLP receiver on 4317, UI on 16686
2. `otel/opentelemetry-collector-contrib:0.154.0` — tail sampling → forwards to Jaeger gRPC
3. All 4 services as containers — each uses `golang:1.26-alpine` base

All Dockerfiles use a two-stage build: `golang:1.26` to compile, `alpine:3.20` to run.

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
curl -X POST http://localhost:8080/checkout -d '{"item_id": "abc123", "amount": 49.99, "user_id": "u42"}'
open http://localhost:16686
# Confirm trace appears in Jaeger with all 4 services and 5+ spans
```

---

### Phase 7 — Load Script + Full Analysis
**File:** `scripts/load.sh`

Send 50 requests to generate enough data to see patterns:
- Service dependency graph in Jaeger
- Latency histogram (inventory spikes should stand out)
- Errored traces isolated by status
- Span attribute search (`user.id = u42`)

**Verify:**
```bash
bash scripts/load.sh
open http://localhost:16686
```

In Jaeger, confirm:
- [ ] Service map shows all 4 nodes with edges between them
- [ ] Trace search returns results filtered by service name
- [ ] At least 4-5 errored traces visible (payment failures)
- [ ] Latency spread visible on inventory spans (50ms–500ms range)
- [ ] Clicking a trace shows the full span tree: gateway → order → inventory + payment
- [ ] Span attributes (`user.id`, `order.id`, `payment.amount`) visible on spans
- [ ] Span events ("stock check complete", "payment failed") visible in span detail

---

## Jaeger UI Walkthrough

Once traces are flowing, here's what to look for:

**Service Map** (`/dependencies` tab)
- Four nodes: gateway, orders, inventory, payments
- Directed edges show call direction and call volume
- This is auto-generated from trace data — no manual config

**Trace Search** (`/search` tab)
- Filter by service: `gateway`
- Filter by tag: `error=true` to find only broken traces
- Filter by duration: `>400ms` to find slow traces
- Each result shows span count, duration, and timestamp

**Trace Detail** (click any trace)
- Timeline view: nested spans show the call tree
- Parallel spans (inventory + payment) appear at the same vertical level
- Hover a span: see all attributes, events, and error messages
- Compare two traces: select two from search results to diff them

**Span Detail** (click any span in trace view)
- Tags section: `user.id`, `payment.amount`, etc.
- Logs section: span events like "payment failed"
- Process section: `service.name`, `service.version` from resource attributes

---

## Running the Project

```bash
# Start everything (Docker Compose v2 syntax — no hyphen)
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
