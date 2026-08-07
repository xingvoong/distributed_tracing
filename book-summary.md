# Book Summary: Distributed Tracing in Practice

**Authors:** Austin Parker, Daniel Spoonhower, Jonathan Mace, Ben Sigelman, Rebecca Isaacs
**Publisher:** O'Reilly Media
**ISBN:** 9781492056638

---

## The Core Argument

Logs tell you *what* happened. Metrics tell you *how often*. Traces tell you *why it was slow* and *where it broke*.

```
┌─────────────────────────────────────────────────────────────┐
│                  THE OBSERVABILITY GAP                      │
│                                                             │
│   LOGS              METRICS             TRACES              │
│   ─────             ───────             ──────              │
│  "what happened"   "how often"      "why & where"          │
│                                                             │
│  2026-08-07        req_rate: 120/s   gateway (5ms)         │
│  ERROR: timeout    error_rate: 2%      └─ orders (95ms)    │
│  in payment svc    p99: 340ms              ├─ inventory OK  │
│                                            └─ payments ❌  │
│                                                             │
│  ← tells you WHAT   tells you HOW →    tells you WHERE →   │
│    happened          bad it is          it broke            │
└─────────────────────────────────────────────────────────────┘
```

In a monolith, a stack trace is enough. In a microservices system, a request touches 10 services before returning — and any one of them could be the problem. You can't debug that with logs and metrics alone. Distributed tracing gives you the causal chain across service boundaries.

---

## Three Pillars

The book organizes everything around three problems:

```
┌──────────────────────────────────────────────────────────────────┐
│                     DISTRIBUTED TRACING                          │
│                                                                  │
│  ┌──────────────────┐    ┌─────────────────┐    ┌────────────┐  │
│  │  INSTRUMENTATION │───►│ DATA COLLECTION │───►│  ANALYSIS  │  │
│  │                  │    │                 │    │            │  │
│  │ · Generate spans │    │ · OTel Collector│    │ · Jaeger   │  │
│  │ · Propagate ctx  │    │ · Batch & sample│    │ · Svc map  │  │
│  │ · Tag attributes │    │ · Route backend │    │ · RED      │  │
│  │ · Record errors  │    │ · Filter/enrich │    │ · Diff     │  │
│  └──────────────────┘    └─────────────────┘    └────────────┘  │
│         Ch 1–6                 Ch 7–9               Ch 10–13     │
└──────────────────────────────────────────────────────────────────┘
```

Every chapter maps back to one of these three.

---

## Core Concepts

### Spans — The Atomic Unit

A span represents one named operation. Everything a trace knows lives inside spans.

```
┌─────────────────────────────────────────────────────────────┐
│  span: "POST /order"                                        │
├─────────────────────────────────────────────────────────────┤
│  IDENTITY                                                   │
│    trace_id:  7f3a9c4b2e1d8a6f3b9c2d1e4f5a6b7c8d          │
│    span_id:   2a3b4c5d6e7f8a9b                              │
│    parent_id: 1a2b3c4d5e6f7a8b  (nil if root span)         │
├─────────────────────────────────────────────────────────────┤
│  TIMING                                                     │
│    start:    2026-08-07T10:00:00.000Z                       │
│    end:      2026-08-07T10:00:00.115Z                       │
│    duration: 115ms                                          │
├─────────────────────────────────────────────────────────────┤
│  ATTRIBUTES (searchable key-value metadata)                 │
│    http.method        = POST                                │
│    http.status_code   = 200                                 │
│    user.id            = u42                                 │
│    order.id           = ord-991                             │
│    payment.amount     = 49.99                               │
├─────────────────────────────────────────────────────────────┤
│  EVENTS (timestamped logs attached to this span)            │
│    +5ms   "validating order"                                │
│    +30ms  "stock confirmed"                                 │
│    +110ms "payment processed"                               │
├─────────────────────────────────────────────────────────────┤
│  STATUS:  OK                                                │
└─────────────────────────────────────────────────────────────┘
```

### Traces — The Full Request Journey

A trace is a tree of spans sharing a **trace ID**. It represents one complete end-to-end request.

```
Trace ID: 7f3a9c4b...
│
├─ Trace Tree (how it looks in code / storage)
│
│  span: gateway.checkout          [0ms ──────────────── 120ms]
│    └── span: orders.process      [5ms ─────────────── 115ms]
│             ├── span: inventory  [8ms ──────── 80ms]
│             └── span: payments   [8ms ── 30ms] ← ERROR
│
└─ Trace Timeline (how it looks in Jaeger UI)

   Time →    0ms      30ms      60ms      90ms     120ms
             |         |         |         |         |
   gateway   ██████████████████████████████████████████  120ms
   orders     █████████████████████████████████████      115ms
   inventory    ████████████████████████████             72ms
   payments     ████████                                 22ms ❌
```

The nesting shows causality. The horizontal bars show where time went. Parallel bars (inventory + payments starting at the same time) show fan-out.

### Context Propagation

For spans to form a trace, each service must pass trace context to the next one via the `traceparent` header.

```
traceparent: 00  -  7f3a9c4b2e1d8a6f...  -  2a3b4c5d...  -  01
             ^^      ^^^^^^^^^^^^^^^^^^      ^^^^^^^^^^^^      ^^
           version      trace ID (128-bit)   parent span ID   flags
                         shared by all spans   (caller's span)
```

How it flows through a real request:

```
  ┌───────────┐
  │  Client   │  POST /checkout
  └───────────┘
        │  (no traceparent — this is the entry point)
        ▼
  ┌───────────┐  creates root span (span_id: AAA)
  │  Gateway  │  injects → traceparent: 00-7f3a...-AAA-01
  └───────────┘
        │  POST /order  +  traceparent: 00-7f3a...-AAA-01
        ▼
  ┌───────────┐  extracts parent=AAA, creates child span (span_id: BBB)
  │  Orders   │  injects → traceparent: 00-7f3a...-BBB-01
  └───────────┘
        │
   ┌────┴─────────────────────────┐
   │  GET /stock                  │  POST /pay
   │  traceparent: 00-7f3a...-BBB │  traceparent: 00-7f3a...-BBB
   ▼                              ▼
┌───────────┐               ┌───────────┐
│ Inventory │ span_id: CCC  │ Payments  │ span_id: DDD
└───────────┘               └───────────┘

Result: one trace, four spans, all linked by trace ID 7f3a...
```

**This is the hardest part to get right.** If propagation breaks anywhere — a missing header, an async queue that drops context, a goroutine that doesn't inherit the parent — the trace splits and you lose the causal link.

### Instrumentation Approaches

The book maps out the design space across three axes:

```
AXIS 1: WHO OWNS THE INSTRUMENTATION CODE
──────────────────────────────────────────
White Box                        Black Box
(you write it)                   (infra does it)
     │                                │
  SDK in your                    Service mesh /
  application                    eBPF intercept
     │                                │
  Full business                  Zero code changes
  context                        Transport only
     │                                │
  More work                      Less detail

AXIS 2: WHERE THE INSTRUMENTATION RUNS
───────────────────────────────────────
Library (in-process)             Agent (out-of-process)
     │                                │
  Same process                    Sidecar / daemon
  as your app                     next to your app
     │                                │
  Lower latency                   Language-agnostic
  Full Go types                   Restarts independently

AXIS 3: WHAT LEVEL OF DETAIL
──────────────────────────────
Application level                System level
     │                                │
  "payment processed"            "TCP connection"
  "stock check failed"           "TLS handshake"
  Business meaning               Infrastructure noise

Most production systems use: White Box SDK (application) + Black Box mesh (system)
```

### OpenTelemetry

The industry standard. OTel is the merger of OpenTracing and OpenCensus.

```
  Your Code  (Go / Python / Java / Node)
      │
      │  uses OTel API (vendor-neutral interfaces)
      ▼
  OTel SDK  (implements the API, buffers spans locally)
      │
      │  exports via OTLP (OpenTelemetry Protocol)
      ▼
  OTel Collector  (receives, processes, routes)
      │
      ├──────────────► Jaeger       (open source)
      ├──────────────► Tempo        (Grafana stack)
      ├──────────────► Zipkin       (open source)
      ├──────────────► Datadog      (commercial)
      └──────────────► Honeycomb    (commercial)

You instrument once. You can swap backends without touching application code.
```

---

## Sampling

You cannot trace every request at scale. At 10,000 req/s, storing every span is prohibitively expensive. Sampling decides what to keep.

```
HEAD-BASED SAMPLING                   TAIL-BASED SAMPLING
───────────────────                   ──────────────────────────
Request arrives                       Request arrives
       │                                     │
       ▼                                     ▼
  Flip coin                           Collect all spans
  (at trace start)                    into a buffer
       │                                     │
   YES │  NO                                 ▼
       │   └──► discard immediately    Wait for trace end
       ▼         (no spans stored)     (decision_wait: 10s)
  Collect spans                               │
  Store trace                                 ▼
                                       Evaluate policies:
                                       ┌─────────────────────┐
                                       │ any span ERROR?      │
                                       │   YES → KEEP (100%) │
                                       │   NO  ↓             │
                                       │ any span > 400ms?   │
                                       │   YES → KEEP (100%) │
                                       │   NO  ↓             │
                                       │ probabilistic        │
                                       │   10% → KEEP        │
                                       │   90% → DISCARD     │
                                       └─────────────────────┘

PROBLEM with head-based:              TRADEOFF with tail-based:
You don't know if a request           All spans must arrive at
will be slow or errored               the SAME collector instance
at the moment you decide.             (no horizontal scaling without
You discard errors and slow           sticky routing or shared storage)
traces at the same rate as
normal ones.
```

---

## Data Collection Pipeline

```
                        SPAN LIFECYCLE
                        ──────────────

  ┌─────────────────────────────────────────────────────────────┐
  │  APPLICATION                                                │
  │                                                             │
  │  func handleCheckout(ctx context.Context) {                 │
  │      ctx, span := tracer.Start(ctx, "checkout")            │
  │      defer span.End()                                       │
  │      span.SetAttributes(attr.String("user.id", userID))    │
  │  }                                                          │
  └───────────────────────┬─────────────────────────────────────┘
                          │ OTLP/HTTP (port 4318)
                          │ or OTLP/gRPC (port 4317)
                          ▼
  ┌─────────────────────────────────────────────────────────────┐
  │  OTEL COLLECTOR CONTRIB                                     │
  │                                                             │
  │  receivers:   otlp (accepts spans from services)            │
  │  processors:  tail_sampling (policy evaluation)             │
  │               batch (group spans before exporting)          │
  │               resource (add/modify attributes)              │
  │  exporters:   otlp (forward to Jaeger)                      │
  └───────────────────────┬─────────────────────────────────────┘
                          │ OTLP/gRPC (port 4317)
                          │ (only kept traces forwarded)
                          ▼
  ┌─────────────────────────────────────────────────────────────┐
  │  JAEGER v2                                                  │
  │                                                             │
  │  ┌──────────────┐  ┌────────────┐  ┌─────────────────────┐ │
  │  │ OTLP Receiver│→ │  Storage   │→ │   Query / UI        │ │
  │  │  :4317 gRPC  │  │  (badger   │  │  :16686             │ │
  │  │  :4318 HTTP  │  │   or mem)  │  │  trace search       │ │
  │  └──────────────┘  └────────────┘  │  service map        │ │
  │                                     │  latency histogram  │ │
  │                                     └─────────────────────┘ │
  └─────────────────────────────────────────────────────────────┘
```

Don't export spans directly from services to your storage backend in production. The collector decouples your services from your observability infrastructure — you can swap Jaeger for Tempo without touching a single service.

---

## Analysis Patterns

### Single-trace debugging

```
TRACE: 7f3a9c...   Duration: 340ms   Status: ERROR   Spans: 5

  0ms                    170ms                   340ms
  │                        │                       │
  gateway.checkout ─────────────────────────────────  340ms
    orders.process   ───────────────────────────────  330ms
      inventory.check  ──────────────                 160ms  ← slow
      payments.charge              ──────────── ❌    170ms  ← error

Click on payments.charge:
  Status:  ERROR
  Message: "card declined: insufficient funds"
  Tags:    payment.amount=149.99, payment.provider=stripe
  Events:
    +5ms   "contacting stripe API"
    +165ms "received decline response"
```

Find one bad request. The trace tree tells you which service added latency, which call failed, and what the error was. No log grepping across 10 services.

### Aggregate analysis — RED metrics

```
Derived from trace data across ALL requests:

SERVICE         RATE          ERRORS        DURATION (p99)
──────────────────────────────────────────────────────────
gateway         120 req/s     2.1%          340ms  ← alert
orders          118 req/s     2.0%          330ms
inventory        118 req/s     0.1%          280ms  ← slow
payments        118 req/s     10.3%         170ms  ← high errors

  R = Rate      (requests/sec — is traffic normal?)
  E = Errors    (error rate   — is something broken?)
  D = Duration  (latency p99  — is something slow?)
```

### Service dependency map

Auto-generated from trace parent-child relationships. No manual documentation.

```
  ┌─────────┐   120/s    ┌────────┐   118/s   ┌───────────┐
  │ gateway │──────────► │ orders │──────────► │ inventory │
  └─────────┘            └────────┘            └───────────┘
                              │
                              │ 118/s
                              ▼
                         ┌──────────┐
                         │ payments │
                         └──────────┘

Edge thickness = call volume. Edge color = error rate.
This updates automatically as your architecture evolves.
```

### Trace comparison (before/after a deploy)

```
NORMAL TRACE (p50)              SLOW TRACE (p99)
────────────────────            ─────────────────────────
gateway    ████  40ms           gateway    ██████████  340ms
orders     ███   35ms           orders     █████████   330ms
inventory  ██    20ms           inventory  ████████    280ms  ← diff
payments   █     10ms           payments   ██          50ms

Diff: inventory span grew 260ms after the v2.1.0 deploy.
The inventory service introduced a new DB query without an index.
```

---

## Microservices-Specific Challenges

### Fan-out

```
request arrives at orders service
         │
         ├──────────────────────────────┐
         │                              │
         ▼                              ▼
  goroutine 1                    goroutine 2
  GET /stock                     POST /pay
         │                              │
         ▼                              ▼
  inventory span                 payments span

CRITICAL: context must be passed INTO each goroutine explicitly.
Go's context.Context is not inherited automatically across goroutines.
If you forget, the child spans become orphaned root spans — a broken trace.

  WRONG:  go func() { callInventory() }()
  RIGHT:  go func(ctx context.Context) { callInventory(ctx) }(ctx)
```

### Async workflows (message queues)

```
PRODUCER SERVICE                       CONSUMER SERVICE
────────────────                       ─────────────────
span: "publish order"                  span: "process order"
  │                                      │
  │  inject trace context                │  extract trace context
  │  into message payload                │  from message payload
  ▼                                      ▼
┌─────────────────────┐           ┌─────────────────────┐
│ Message: {          │           │ Message: {          │
│   order_id: "991"   │──────────►│   order_id: "991"   │
│   traceparent:      │  queue    │   traceparent:      │
│     "00-7f3a-AAA-01"│           │     "00-7f3a-AAA-01"│
│ }                   │           │ }                   │
└─────────────────────┘           └─────────────────────┘

Without injecting context into the payload, the consumer span
becomes an orphaned root span — you lose the producer→consumer link.
```

### Service meshes

```
                     ┌──────────────────────────────────┐
                     │         YOUR POD                 │
                     │                                  │
  inbound traffic ──►│  ┌──────────┐   ┌────────────┐  │
                     │  │  Envoy   │──►│  Your App  │  │
                     │  │ sidecar  │   │            │  │
  outbound traffic ◄─│  │(Istio)   │◄──│            │  │
                     │  └──────────┘   └────────────┘  │
                     │   auto-instruments               │
                     │   HTTP/gRPC spans                │
                     └──────────────────────────────────┘

Mesh gives you: service map, latency, error rate — zero code changes.
Mesh can't give you: business context, DB queries, user IDs, order amounts.
You still need the OTel SDK for application-level spans.
```

---

## Beyond Tracing

```
┌──────────────────────────────────────────────────────────────────┐
│                    FULL OBSERVABILITY STACK                      │
│                                                                  │
│  ┌──────────────┐   ┌──────────────┐   ┌──────────────────────┐ │
│  │    TRACES    │   │     LOGS     │   │       METRICS        │ │
│  │              │   │              │   │                      │ │
│  │ trace_id:    │◄──│ trace_id:    │   │  RED alerts fire     │ │
│  │  7f3a...     │   │  7f3a...     │   │  → jump to traces   │ │
│  │              │   │ "card        │   │  → find examples    │ │
│  │ span detail  │──►│  declined"   │   │                      │ │
│  │ → jump to    │   │              │   │                      │ │
│  │   log lines  │   │              │   │                      │ │
│  └──────────────┘   └──────────────┘   └──────────────────────┘ │
│          │                                                       │
│          ▼                                                       │
│  ┌──────────────────────────────────┐                           │
│  │       CONTINUOUS PROFILING       │                           │
│  │                                  │                           │
│  │  slow span → attach CPU profile  │                           │
│  │  see which function ate 280ms    │                           │
│  └──────────────────────────────────┘                           │
└──────────────────────────────────────────────────────────────────┘

Log correlation: inject trace_id + span_id into every log line.
Jump from a slow span directly to all logs produced during it.

Metrics → traces: when p99 latency alerts, search for traces
with duration > threshold. Get from "something is wrong" to
"here is a specific example of the broken request" in seconds.

Profiling: attach CPU/memory profiles to specific spans.
Know not just that a span was slow — know which line of code caused it.
```

---

## Key Takeaways

- The span tree gives you causality. Logs and metrics can't.
- Context propagation is the load-bearing wall. If it breaks, nothing else works.
- OTel is the right default in 2026. Don't build on a vendor-specific SDK.
- Tail-based sampling is worth the complexity. Head-based discards the traces you actually need.
- Start with instrumentation, then collection, then analysis. In that order.
