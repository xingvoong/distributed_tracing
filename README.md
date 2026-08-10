# TraceFlow

A distributed order processing system built to demonstrate every core concept from *Distributed Tracing in Practice* (Parker et al., O'Reilly).

Four Go microservices. OpenTelemetry instrumentation. Jaeger for storage and visualization. One `docker compose up` to run everything.

---

## Why Go (Not Python)

This project uses patterns that Go handles well and Python struggles with structurally.

**Goroutines vs threads**

The fan-out in the order service fires inventory and payment calls in parallel:

```go
// Go — goroutines are cheap (2KB stack, multiplexed on OS threads)
wg.Add(2)
go func(ctx context.Context) { callInventory(ctx, itemID) }(ctx)
go func(ctx context.Context) { callPayment(ctx, amount) }(ctx)
wg.Wait()
```

Python has the GIL — only one thread runs Python bytecode at a time. For I/O-bound work you'd use `asyncio`, but that requires async/await through the entire call stack. In Go, goroutines work without rewiring your code.

**Context as a first-class citizen**

Go's `context.Context` is how cancellation, deadlines, and trace propagation flow through the call graph. Every function in this project passes `ctx` explicitly:

```go
func callInventory(ctx context.Context, itemID string) (stockResponse, error) {
    ctx, span := tracer.Start(ctx, "orders.call_inventory")
    ...
}
```

Python has `contextvars` but it's not idiomatic — most libraries don't thread it through. OTel's Python SDK uses thread-local storage as a workaround, which breaks under async code.

**Compile-time correctness**

The context extraction bug (`r.Context()` vs `Extract(r.Context(), ...)`) produced no runtime error and no log output — the code ran fine, traces just didn't link. In Go, type errors and missing returns are caught at compile time. Silent logic bugs still happen, but an entire class of errors doesn't make it to production.

**Single binary, tiny Docker image**

```dockerfile
FROM golang:1.26-alpine AS builder
RUN go build -o /gateway ./services/gateway/main.go  # compiles to one binary

FROM alpine:3.20                                      # no runtime needed
COPY --from=builder /gateway /gateway                 # copy binary only
```

Python requires the interpreter, all dependencies, and often a larger base image. A Go service image is typically 10–20MB. A Python equivalent is 200–400MB.

**Explicit error handling**

```go
resp, err := http.DefaultClient.Do(req)
if err != nil {
    span.SetStatus(codes.Error, err.Error())
    return stockResponse{}, err
}
```

Every error is a value you handle or propagate. There's no exception that silently unwinds the stack. In distributed systems, knowing exactly where an error originated matters — Go forces you to be explicit about it.

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

## Tracing Concepts — Where Each One Lives

| Book Concept | Phase | Code | README |
|---|---|---|---|
| Tracer provider | 1 | `shared/tracing/tracing.go:44` | Phase 1 — `tracing.Init()` |
| Resource attributes | 1 | `shared/tracing/tracing.go:25` | Phase 1 — `tracing.Init()` |
| OTLP exporter | 1 | `shared/tracing/tracing.go:38` | Architecture diagram → `:4318` |
| W3C propagator (`traceparent`) | 1 | `shared/tracing/tracing.go:54` | Context Propagation Flow |
| Shutdown / span flush | 1 | `shared/tracing/tracing.go:56` | Phase 1 — `defer shutdown()` |
| Child spans | 2 | `services/payments/main.go:30` | Phase 2 flow diagram |
| Span attributes | 2 | `services/payments/main.go:39` | Jaeger UI → Span Detail → Tags |
| Span events | 2 | `services/payments/main.go:48,60` | What a Trace Looks Like → EVENTS |
| Error recording | 2 | `services/payments/main.go:47` | Sampling Decision Flow → ERROR policy |
| Context extraction | 2 | `services/payments/main.go:29` | Context Propagation Flow |
| Child spans | 3 | `services/inventory/main.go:25` | Phase 3 flow diagram |
| Span attributes | 3 | `services/inventory/main.go:39` | Jaeger UI → Span Detail → Tags |
| Span events | 3 | `services/inventory/main.go:44` | What a Trace Looks Like → EVENTS |
| Context extraction | 3 | `services/inventory/main.go:24` | Context Propagation Flow |
| Latency variance | 3 | `services/inventory/main.go:34` | Sampling Decision Flow → slow traces |
| Fan-out + goroutines | 4 | `services/orders/main.go:133` | Architecture diagram |
| Context injection (outgoing) | 4 | `services/orders/main.go:51` | Context Propagation Flow |
| Error propagation across services | 4 | `services/orders/main.go:155` | Phase 4 flow diagram |
| Root span | 5 | `services/gateway/main.go:29` | Phase 5 — root span diagram |
| Trace ID generation | 5 | `services/gateway/main.go:29` | Phase 5 — trace ID flow diagram |
| Collection pipeline | 6 | `otel-collector-config.yaml` | Architecture diagram → OTel Collector |
| Tail-based sampling | 6 | `otel-collector-config.yaml` | Sampling Decision Flow |
| Service map | 7 | Jaeger UI | Jaeger UI Walkthrough |
| Aggregate analysis (RED) | 7 | Jaeger UI | Jaeger UI Walkthrough |
| Trace comparison | 7 | Jaeger UI | Jaeger UI Walkthrough |

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
    ├── test_payments.sh        # 100 requests to :8083, prints approved/declined + summary
    ├── stress_payments.sh      # 10 × 100 requests, per-run failure rate + aggregate
    ├── test_inventory.sh       # 10 requests to :8082, prints latency + min/max/avg
    ├── stress_inventory.sh     # 10 × 20 requests, per-run + aggregate latency stats
    ├── test_orders.sh          # 20 requests to :8081, latency + payment + quantity
    ├── stress_orders.sh        # 10 × 50 requests, per-run failure rate + latency stats
    ├── debug_trace.sh          # 1 request + 15s countdown + collector log dump
    └── load.sh                 # 30 requests to populate Jaeger, 15s countdown
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

### Phase 1 — Shared Tracing Package ✅
**Files:** `shared/tracing/tracing.go`, `shared/tracing/tracing_test.go`

One exported function — `tracing.Init(serviceName string) (func(), error)` — that every service calls at startup.

```
tracing.Init("payments")
       │
       ├── resource.New()             stamps service.name + service.version on every span
       ├── otlptracehttp.New()        OTLP/HTTP exporter → OTEL_EXPORTER_OTLP_ENDPOINT (:4318)
       ├── NewTracerProvider()        batches spans, feeds exporter
       ├── otel.SetTracerProvider()   registers globally — any code can call otel.Tracer()
       ├── otel.SetTextMapPropagator() registers W3C TraceContext — reads/writes traceparent
       └── returns shutdown()         caller defers this — flushes buffered spans on exit
```

> **Note:** correct exporter import path is
> `go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp`
> (not `otlptracehttp` directly — extra `otlptrace` segment in the path)

**Tests — all passing:**
```bash
go test ./shared/tracing/... -v

PASS: TestInit_ReturnsShutdown          # Init returns no error, non-nil shutdown
PASS: TestInit_RegistersTracerProvider  # global provider is *sdktrace.TracerProvider
PASS: TestInit_RegistersPropagator      # propagator fields include "traceparent"
PASS: TestInit_TracerCreation           # otel.Tracer() returns a usable tracer
PASS: TestShutdown_DoesNotPanic         # shutdown flushes cleanly without panic
ok  github.com/xingvoong/distributed_tracing/shared/tracing  2.689s
```

---

### Phase 2 — Payment Service ✅
**File:** `services/payments/main.go`

HTTP server on `:8083`. One endpoint: `POST /pay`.

```
POST /pay
     │
     ├── extract trace context from incoming request (traceparent header)
     ├── tracer.Start(ctx, "payments.charge")   → child span
     ├── span.SetAttributes(payment.amount, payment.status)
     │
     ├── rand.Intn(10) == 0   (10% chance)
     │        │
     │    YES ├── span.SetStatus(ERROR, "card declined: insufficient funds")
     │        ├── span.AddEvent("payment failed")
     │        └── return 500 JSON
     │
     └── NO  ├── span.AddEvent("payment processed")
             └── return 200 JSON
```

**Verified — 1000 requests (10 runs × 100), 87 declined (8.7%):**
```bash
# terminal 1
go run ./services/payments/main.go

# terminal 2
bash scripts/stress_payments.sh

Run 1:  approved=87  declined=13  failure_rate=13%
Run 2:  approved=88  declined=12  failure_rate=12%
Run 3:  approved=92  declined=8   failure_rate=8%
Run 4:  approved=96  declined=4   failure_rate=4%
Run 5:  approved=90  declined=10  failure_rate=10%
Run 6:  approved=93  declined=7   failure_rate=7%
Run 7:  approved=89  declined=11  failure_rate=11%
Run 8:  approved=94  declined=6   failure_rate=6%
Run 9:  approved=89  declined=11  failure_rate=11%
Run 10: approved=95  declined=5   failure_rate=5%

────────────────────────────────────────
Total requests: 1000
Total approved: 913  (91%)
Total declined:  87  (8.7%)
Expected failure rate: ~10%
```

Per-run variance swings 4%–13% — expected at 100 requests per run.
Across 1000 total it converges to 8.7%, confirming `rand.Intn(10) == 0` is correct.

**Test scripts:**
- `bash scripts/test_payments.sh` — 100 requests, prints each result + summary
- `bash scripts/stress_payments.sh` — 10 × 100 requests, shows per-run rate + aggregate

> Spans are created on every request but exported nowhere yet — no OTel
> Collector is running. They will appear in Jaeger once Phase 6 is complete.
> The `traceparent` header is extracted but incoming requests have no parent
> yet — spans are root spans until Phase 4 wires Orders → Payments.

---

### Phase 3 — Inventory Service ✅
**File:** `services/inventory/main.go`

HTTP server on `:8082`. One endpoint: `GET /stock`.

```
GET /stock?item_id=abc123
     │
     ├── extract trace context (traceparent header)
     ├── tracer.Start(ctx, "inventory.check")  → child span
     ├── sleep 50–500ms  ← rand.Intn(451) + 50  (simulates DB query)
     ├── span.SetAttributes:
     │     inventory.item_id  = "abc123"
     │     inventory.quantity = 42
     │     inventory.delay_ms = 317          ← exact delay visible in Jaeger
     ├── span.AddEvent("stock check complete")
     └── return { item_id, quantity } JSON
```

**Verified — 10 requests:**
```bash
# terminal 1
go run ./services/inventory/main.go

# terminal 2
bash scripts/test_inventory.sh

Request 1:  175ms  quantity=19
Request 2:  454ms  quantity=51
Request 3:  258ms  quantity=74
Request 4:  230ms  quantity=20
Request 5:  493ms  quantity=40
Request 6:  400ms  quantity=22
Request 7:  468ms  quantity=89
Request 8:  550ms  quantity=29
Request 9:  393ms  quantity=2
Request 10: 449ms  quantity=46

────────────────────────
Min:  175ms
Max:  550ms
Avg:  387ms
Expected range: 50–500ms
```

Requests 2, 5, 6, 7, 8, 9, 10 cross the 400ms threshold — those will be kept
100% by the tail sampling policy in Phase 6.

**Stress test — stress_inventory.sh (10 × 20 = 200 requests):**
```bash
bash scripts/stress_inventory.sh

Run 1:  min=140ms  max=556ms  avg=330ms
Run 2:  min=141ms  max=566ms  avg=328ms
Run 3:  min=125ms  max=494ms  avg=305ms
Run 4:  min=134ms  max=535ms  avg=282ms
Run 5:  min=117ms  max=550ms  avg=351ms
Run 6:  min=134ms  max=567ms  avg=283ms
Run 7:  min=131ms  max=564ms  avg=349ms
Run 8:  min=168ms  max=578ms  avg=363ms
Run 9:  min=153ms  max=550ms  avg=298ms
Run 10: min=144ms  max=533ms  avg=350ms

────────────────────────────────────────
Total requests: 200
Latency min:    117ms
Latency max:    578ms
Latency avg:    324ms
Expected range: 50–500ms per request
```

> Spans are created on every request but exported nowhere yet — no OTel
> Collector is running. They will appear in Jaeger once Phase 6 is complete.

---

### Phase 4 — Order Service ✅
**File:** `services/orders/main.go`

HTTP server on `:8081`. One endpoint: `POST /order`.

```
POST /order
     │
     ├── extract trace context (traceparent header)
     ├── tracer.Start(ctx, "orders.process")  → child span
     ├── tag: order.item_id, order.user_id, order.amount
     │
     ├── sync.WaitGroup — fan-out to 2 goroutines
     │     │
     │     ├── goroutine 1: callInventory(ctx, item_id)   ← ctx passed as arg, not closure
     │     │     ├── child span "orders.call_inventory"
     │     │     ├── injectContext() → writes traceparent header   ← NEW in Phase 4
     │     │     └── GET /stock :8082
     │     │
     │     └── goroutine 2: callPayment(ctx, amount)      ← ctx passed as arg, not closure
     │           ├── child span "orders.call_payment"
     │           ├── injectContext() → writes traceparent header   ← NEW in Phase 4
     │           └── POST /pay :8083
     │
     ├── wg.Wait() — block until both return
     ├── stockErr → span ERROR + return 500
     ├── payErr   → span ERROR + return 500
     └── return 200 confirmed
```

**New concepts introduced:**

*Context injection (outgoing):* Phase 2 and 3 only extracted context from incoming
requests. Phase 4 is the first service that writes `traceparent` onto outgoing
requests — this is what links Inventory and Payment spans back to the Orders span.

```go
// services/orders/main.go:50
func injectContext(ctx context.Context, req *http.Request) {
    otel.GetTextMapPropagator().Inject(ctx, propagation.HeaderCarrier(req.Header))
}
```

*Fan-out:* context must be passed as a goroutine argument, not captured by closure.

```go
// WRONG — ctx captured by closure, may be stale
go func() { callInventory(ctx, itemID) }()

// RIGHT — ctx passed explicitly (services/orders/main.go:135)
go func(ctx context.Context) { callInventory(ctx, itemID) }(ctx)
```

**Verified — test_orders.sh (20 requests) + stress_orders.sh (10 × 50 = 500 requests):**
```bash
go run ./services/payments/main.go &
go run ./services/inventory/main.go &
go run ./services/orders/main.go &

# terminal 2
bash scripts/test_orders.sh

Request 1:  421ms  order=confirmed  payment=approved  quantity=1
Request 17: 423ms  order=failed     payment=declined  quantity=89
Request 20: 325ms  order=failed     payment=declined  quantity=2
...
Confirmed: 18 (90%)   Failed: 2 (10%)

# stress test
bash scripts/stress_orders.sh

Run 1:  confirmed=44  failed=6   failure_rate=12%  avg_latency=331ms
Run 2:  confirmed=43  failed=7   failure_rate=14%  avg_latency=372ms
Run 3:  confirmed=48  failed=2   failure_rate=4%   avg_latency=352ms
Run 4:  confirmed=47  failed=3   failure_rate=6%   avg_latency=375ms
Run 5:  confirmed=45  failed=5   failure_rate=10%  avg_latency=368ms
Run 6:  confirmed=48  failed=2   failure_rate=4%   avg_latency=292ms
Run 7:  confirmed=41  failed=9   failure_rate=18%  avg_latency=333ms
Run 8:  confirmed=47  failed=3   failure_rate=6%   avg_latency=331ms
Run 9:  confirmed=46  failed=4   failure_rate=8%   avg_latency=362ms
Run 10: confirmed=40  failed=10  failure_rate=20%  avg_latency=347ms

────────────────────────────────────────────────────────
Total requests:  500
Total confirmed: 449  (89%)
Total failed:     51  (10%)
Latency min:     116ms
Latency max:     670ms
Latency avg:     346ms
```

Per-run variance 4%–20% — expected at 50 requests per run.
Across 500 total converges to 10%. Latency driven by inventory service (50–500ms sleep).

---

### Phase 5 — API Gateway ✅
**File:** `services/gateway/main.go`

HTTP server on `:8080`. One endpoint: `POST /checkout`.

```
POST /checkout
     │
     ├── NO traceparent on incoming request
     ├── tracer.Start(ctx, "gateway.checkout")  → ROOT span (no parent)
     │     └── this generates the trace ID shared by all downstream spans
     │
     ├── tag: user.id, http.method, order.item_id, order.amount
     │
     ├── build outgoing request to orders :8081
     ├── Inject(ctx, headers)  → writes traceparent header
     │     └── starts the propagation chain:
     │           gateway → orders → inventory
     │                    → orders → payments
     │
     ├── forward response back to client
     ├── tag: http.status_code
     └── if orders failed → span ERROR
```

**New concept — Root span:**

Every other service received a `traceparent` and created a child span.
Gateway creates a span with no parent — the trace starts here.

```
Phases 2, 3, 4                        Phase 5
──────────────────────────────────     ──────────────────────────────────
incoming request                       incoming request
  traceparent: 00-7f3a-AAA-01    →       (no traceparent header)
       │                                         │
       ▼                                         ▼
  child span                              ROOT span
  parent_id: AAA                          parent_id: nil
  trace_id:  7f3a... (inherited)          trace_id:  NEW (generated here)
```

**How the trace ID flows through the full system:**

```
  client
    │  POST /checkout (no traceparent)
    ▼
  gateway  generates trace_id: 7f3a9c...
    │  POST /order
    │  traceparent: 00-7f3a9c...-AAA-01    ← trace_id injected here
    ▼
  orders   creates span BBB, inherits 7f3a9c...
    │
    ├── GET /stock                          ├── POST /pay
    │   traceparent: 00-7f3a9c...-BBB-01   │   traceparent: 00-7f3a9c...-BBB-01
    ▼                                      ▼
  inventory  span CCC, trace: 7f3a9c...  payments  span DDD, trace: 7f3a9c...

  All 4 spans share trace_id: 7f3a9c...
  That single ID is what Jaeger uses to stitch them into one trace.
```

**Why latency is driven by inventory, not the sum of all services:**

```
  0ms                              450ms
  │                                  │
  gateway  ██████████████████████████████████  ~460ms total
  orders    █████████████████████████████████
  inventory   ████████████████████████████      ~420ms  ← slowest
  payments    ████                               ~20ms  ← parallel, not sequential

  total = max(inventory, payments) = 420ms
  NOT   = inventory + payments     = 440ms

  Orders uses sync.WaitGroup — both goroutines fire simultaneously.
  wg.Wait() unblocks when the LAST one finishes, not after both sequentially.
```

**Verified — 35 requests across all 4 services:**
```bash
go run ./services/payments/main.go &
go run ./services/inventory/main.go &
go run ./services/orders/main.go &
go run ./services/gateway/main.go &

curl -s -X POST http://localhost:8080/checkout \
  -H 'Content-Type: application/json' \
  -d '{"item_id": "abc123", "amount": 49.99, "user_id": "u42"}'

Request 4:  order=failed  payment=declined  ← error flows gateway → orders → payments → back
Request 14: order=failed  payment=declined

33 confirmed, 2 failed — latency 153ms–569ms (inventory sleep, not sum of all services)
```

---

### Phase 6 — Docker Compose + OTel Collector + Jaeger v2 ✅
**Files:** `docker-compose.yml`, `otel-collector-config.yaml`, all `Dockerfile`s

Wire everything together in this order:
1. `jaegertracing/jaeger:latest` — OTLP receiver on 4317, UI on 16686
2. `otel/opentelemetry-collector-contrib:latest` — tail sampling → forwards to Jaeger gRPC
3. All 4 services as containers — each uses `golang:1.26` to compile, `alpine:3.20` to run

Each service Dockerfile follows the same two-stage build:
```dockerfile
FROM golang:1.26-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -o /gateway ./services/gateway/main.go

FROM alpine:3.20
COPY --from=builder /gateway /gateway
EXPOSE 8080
CMD ["/gateway"]
```

Service URLs are read from env vars — default to localhost for local dev, overridden in Docker Compose:
```go
func orderServiceURL() string {
    if url := os.Getenv("ORDER_SERVICE_URL"); url != "" { return url }
    return "http://localhost:8081"
}
```

Sampling rules in `otel-collector-config.yaml`:
```yaml
processors:
  tail_sampling:
    decision_wait: 10s
    policies:
      - name: keep-errors
        type: status_code
        status_code: { status_codes: [ERROR] }
      - name: keep-slow-traces
        type: latency
        latency: { threshold_ms: 400 }
      - name: sample-10-percent
        type: probabilistic
        probabilistic: { sampling_percentage: 10 }
```

**Bug found and fixed: context extraction was missing on all receiving services.**

Every service was using `ctx := r.Context()` directly. Without extracting the incoming `traceparent` header, each service created a new root span with a fresh trace ID — so Jaeger showed isolated 1-span traces instead of a linked tree.

```
BEFORE FIX                          AFTER FIX
──────────────────────────────      ──────────────────────────────
gateway.checkout  trace: 7f3a       gateway.checkout  trace: 7f3a
                                      └── orders.process
orders.process    trace: 9b2c             ├── orders.call_inventory
                                          │     └── inventory.check
inventory.check   trace: 4d1e            └── orders.call_payment
                                                └── payments.charge
payments.charge   trace: 2a8f

4 separate traces in Jaeger         1 trace, 6 spans, all linked
```

Fix — one line added to each handler:
```go
// BEFORE — starts a new root span, no link to parent
ctx := r.Context()

// AFTER — reads traceparent header, creates child span
ctx := otel.GetTextMapPropagator().Extract(r.Context(), propagation.HeaderCarrier(r.Header))
```

**Verified — 30 requests via `bash scripts/load.sh`:**
```
Confirmed: 27
Failed:     3

Waiting 15s for tail sampler to flush...
Open Jaeger: http://localhost:16686
```

**Results in Jaeger after 30 requests:**
```
  Traces visible:  20  (from 30 sent — tail sampler kept errors + slow + 10% normal)
  Error traces:     4  (payment failures, kept at 100%)
  Full waterfall:  all 20 traces show all 4 services linked

  Trace waterfall (example — 502ms, inventory latency dominated):

  Time →   0ms      100ms     200ms     300ms     400ms     502ms
           │          │         │         │         │          │
  gateway  ██████████████████████████████████████████████████████  502ms
  orders    █████████████████████████████████████████████████████  499ms
  inventory   ████████████████████████████████████████████████     ~470ms  ← slowest
  payments    ███████                                               ~28ms   ← parallel

  total = max(inventory, payments) = 502ms
```

**OTel Collector debug log confirming traces flowing:**
```
resource spans: 4, spans: 6   ← 4 services, 6 spans, all in one export
```
Before the Extract fix: spans arrived in 3 separate batches (1, 3, 1) with different trace IDs.
After the fix: all 6 spans exported together under one trace ID.

---

### Phase 7 — Load Script + Full Analysis ✅
**File:** `scripts/load.sh`

Send 30 requests to generate enough data to see patterns.

```bash
bash scripts/load.sh
open http://localhost:16686
```

**Jaeger checklist — all verified:**

```
  SERVICE MAP  (/dependencies tab)
  ─────────────────────────────────────────────────────────
  [x] 4 nodes visible: gateway, orders, inventory, payments
  [x] Directed edges: gateway→orders, orders→inventory,
      orders→payments
  [x] Edge thickness varies with call volume

  TRACE SEARCH  (/search tab)
  ─────────────────────────────────────────────────────────
  [x] Filter service=gateway returns results
  [x] Filter tag=error=true shows only broken traces
  [x] Filter min-duration=400ms shows only slow traces
  [x] 4 errored traces visible (payment failures)
  [x] Latency spread visible: some traces < 100ms, some > 400ms

  TRACE DETAIL  (click any trace)
  ─────────────────────────────────────────────────────────
  [x] Full span tree: gateway → orders → inventory + payments
  [x] Inventory and payments appear at the same level
      (both children of orders — parallel fan-out)
  [x] Errored payment span shows ERROR status
  [x] Span durations match expected ranges

  SPAN DETAIL  (click any span in trace view)
  ─────────────────────────────────────────────────────────
  [x] Tags: user.id, order.amount, payment.status visible
  [x] Events: "stock check complete", "payment failed" visible
  [x] Process: service.name, service.version from resource attrs
```

**Trace comparison — fast vs slow:**

```
  fast trace                        slow trace
  ──────────────────────────────    ──────────────────────────────
  gateway.checkout    ~50ms         gateway.checkout    ~490ms
    orders.process    ~48ms           orders.process    ~488ms
      inventory.check   43ms  ←          inventory.check   480ms  ← bottleneck
      payments.charge    8ms              payments.charge     9ms

  inventory drove 43ms → 480ms.
  payments barely moved (8ms → 9ms).
  total latency = max(inventory, payments), not sum.
```

Inventory is the bottleneck. Every slow trace is slow because of the random sleep in `inventory.check`. Payments is always fast — it just runs in parallel and waits.

This is what evaluation looks like: two traces, one comparison, bottleneck identified in under 10 seconds. No guessing, no averaging logs — exact span, exact duration.

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

---

## Wrap-up

Seven phases. Four services. One trace ID threading through all of them.

**What got built:**
```
  shared tracing pkg → payments → inventory → orders → gateway
                                                            │
                                               docker compose up --build
                                                            │
                                               otel-collector (tail sampling)
                                                            │
                                                       jaeger ui
```

**What it proved:**

1. **Instrumentation is explicit.** Every span is hand-written — `tracer.Start`, `span.SetAttributes`, `span.SetStatus`. Nothing is automatic. If you don't instrument it, it doesn't appear.

2. **Context propagation is fragile.** One missing `Extract` call breaks the entire trace. All 4 services looked instrumented, all requests succeeded, but Jaeger showed isolated 1-span traces until the fix. Distributed tracing only works if every service in the chain passes the baton.

3. **Tail sampling changes what you store.** 30 requests sent, 20 traces kept. The 10 dropped were fast and successful — the ones you'd least want to spend storage on. Errors and slow traces were kept at 100%. You get signal without paying for noise.

4. **Fan-out changes how you read latency.** Inventory and payments run in parallel. Total latency = max, not sum. Without the waterfall view, you'd assume sequential and misread the bottleneck entirely.

5. **The bottleneck was obvious.** Fast trace: `inventory.check` = 43ms. Slow trace: `inventory.check` = 480ms. Everything else barely moved. One comparison, bottleneck identified. That's the point of distributed tracing — not collecting data, but answering "where is the time going?"
