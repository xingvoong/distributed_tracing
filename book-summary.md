# Book Summary: Distributed Tracing in Practice

**Authors:** Austin Parker, Daniel Spoonhower, Jonathan Mace, Ben Sigelman, Rebecca Isaacs
**Publisher:** O'Reilly Media
**ISBN:** 9781492056638

---

## The Core Argument

Logs tell you *what* happened. Metrics tell you *how often*. Traces tell you *why it was slow* and *where it broke*.

In a monolith, a stack trace is enough. In a microservices system, a request touches 10 services before returning — and any one of them could be the problem. You can't debug that with logs and metrics alone. Distributed tracing gives you the causal chain across service boundaries.

---

## Three Pillars

The book organizes everything around three problems:

1. **Instrumentation** — how to generate trace data from your code
2. **Data Collection** — how to gather, transmit, and store that data efficiently
3. **Analysis** — how to turn raw traces into actionable insights

Every chapter maps back to one of these three.

---

## Core Concepts

### Spans

The atomic unit of tracing. A span represents one named operation with:
- A name (e.g., `"POST /order"`)
- Start time and end time (duration)
- Tags/attributes — key-value metadata (`user.id = u42`, `http.status_code = 500`)
- Events — timestamped logs attached to the span (`"payment failed"`)
- Status — OK or Error, with an optional error message

Every operation you care about becomes a span.

### Traces

A trace is a tree of spans that share a **trace ID**. It represents one complete end-to-end request through your system — from the first service that received it to the last service that touched it.

```
Trace: 7f3a9c...
└── span: gateway.checkout         (120ms)
    └── span: orders.process       (115ms)
        ├── span: inventory.check  ( 80ms)
        └── span: payments.charge  ( 30ms, ERROR)
```

The tree structure shows you causality. The durations tell you where time went.

### Context Propagation

For spans to form a trace, each service must pass the trace context to the next one. In HTTP systems this is the `traceparent` header:

```
traceparent: 00-7f3a9c4b...-2a3b...-01
                 ^trace ID   ^span ID
```

The receiving service extracts the trace ID and parent span ID, creates its own child span, and injects the updated header into any outgoing calls.

**This is the hardest part to get right.** If propagation breaks anywhere — a missing header, an async queue that doesn't forward context, a goroutine that doesn't inherit the parent context — the trace splits and you lose the causal link.

### Instrumentation Approaches

The book maps out several design axes:

**White box vs. black box**
- White box: instrument from inside your code using an SDK. Full control, more work.
- Black box: intercept at the network layer (service mesh, eBPF). Zero code changes, less detail.

**Library vs. agent**
- Library: in-process SDK, spans created directly in your code.
- Agent: out-of-process sidecar that intercepts traffic and generates spans automatically.

**Application vs. system level**
- Application: your code emits spans about business logic (`"payment processed"`).
- System: infrastructure emits spans about transport (`"TCP connection established"`).

Most production setups use a combination. The SDK gives you rich business context; the sidecar handles the infrastructure layer.

### OpenTelemetry

The industry standard for instrumentation. OpenTelemetry (OTel) is the merger of OpenTracing and OpenCensus — it provides:
- **APIs** — language-specific interfaces for creating spans
- **SDKs** — implementations of those APIs with configurable exporters
- **Collector** — a standalone service that receives spans, processes them, and routes to any backend
- **Semantic conventions** — standard attribute names (`http.method`, `db.statement`) so tooling works across languages

OTel is vendor-neutral. You instrument once and can switch backends (Jaeger, Zipkin, Tempo, commercial tools) without touching your code.

---

## Sampling

You cannot trace every request at scale. At 10,000 requests per second, storing every span is prohibitively expensive. Sampling is how you decide what to keep.

### Head-based sampling
Decide at the start of a request whether to trace it. Fast and cheap. The problem: you decide before you know if the request will be interesting. You'll discard slow requests and errors at the same rate as normal ones.

### Tail-based sampling
Collect all spans for a trace, then decide whether to keep the trace after it completes. Expensive (you buffer spans in memory) but smart — you can keep 100% of errored and slow traces, and drop most normal ones.

A practical sampling policy:
- Keep 100% of traces with any error span
- Keep 100% of traces where any span duration > 400ms
- Keep 10% of everything else

The OTel Collector supports tail-based sampling natively.

---

## Data Collection Pipeline

```
Service A ──┐
Service B ──┤──► OTel Collector ──► Jaeger / Tempo / Zipkin
Service C ──┘         ^
                       |
              (batching, sampling,
               filtering, routing)
```

Services export spans to a local agent or directly to the OTel Collector via OTLP (OpenTelemetry Protocol). The collector handles:
- Batching spans before writing to storage
- Applying sampling rules
- Routing to multiple backends simultaneously
- Filtering sensitive attributes before storage

Don't export spans directly from services to your storage backend in production. The collector decouples your services from your observability infrastructure.

---

## Analysis Patterns

### Single-trace debugging
Find one bad request and follow it through the system. The trace tree shows you exactly which service added latency, which call failed, and what the error was. No log grepping across 10 services.

### Aggregate analysis — RED metrics
Derive three metrics from trace data across all requests:
- **Rate** — requests per second per service
- **Errors** — error rate per service
- **Duration** — latency distribution (p50, p95, p99) per service

These surface which service is degraded without you having to look at individual traces.

### Service dependency maps
Auto-generated from trace relationships. Every parent-child span relationship becomes an edge in the service graph. No manual documentation needed — the map updates itself as your architecture changes.

### Trace comparison
Compare a slow trace against a normal trace for the same endpoint. The diff shows you exactly which span took longer and by how much. Useful for debugging a regression after a deploy.

---

## Microservices-Specific Challenges

**Fan-out**
One request spawns many parallel calls. All child spans must share the parent's trace context, even when the calls happen in separate goroutines or threads.

**Async workflows**
Message queues break the request/response model. You need to inject trace context into the message payload and extract it on the consumer side to link producer and consumer spans into one trace.

**Service meshes**
Tools like Istio and Linkerd can auto-instrument at the infrastructure layer — no code changes needed. But they only see transport-level data. You still need application instrumentation for business context.

---

## Beyond Tracing

The final chapters connect traces to the rest of your observability stack.

**Logs + traces**
Inject the trace ID and span ID into every log line. Now you can jump from a log entry to the exact trace that produced it, or from a trace to all logs generated during that span. Log correlation closes the gap between structured logs and distributed traces.

**Metrics + traces**
When a RED metric alerts, use traces to find representative examples of the slow or errored requests. Metrics tell you something is wrong; traces tell you why.

**Continuous profiling**
Attach CPU and memory profiles to specific spans. When a span is slow, you can see exactly which functions consumed the time — not just that the span was slow, but *why* it was slow at the code level.

---

## Key Takeaways

- The span tree gives you causality. That's what logs and metrics can't give you.
- Context propagation is the load-bearing wall. If it breaks, nothing else works.
- OTel is the right default in 2026. Don't build on a vendor-specific SDK.
- Tail-based sampling is worth the complexity. Head-based sampling discards the traces you actually need.
- Start with instrumentation, then collection, then analysis. In that order.
