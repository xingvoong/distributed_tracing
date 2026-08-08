package tracing

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
)

// Init sets up the OTel tracer provider for a service.
// Call this once at startup and defer the returned shutdown function.
//
//	shutdown, err := tracing.Init("payments")
//	if err != nil { log.Fatal(err) }
//	defer shutdown()
func Init(serviceName string) (func(), error) {
	ctx := context.Background()

	// Describe this service to the backend (shows up in Jaeger as process info)
	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceName(serviceName),
			semconv.ServiceVersion("0.1.0"),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("create resource: %w", err)
	}

	// OTLP/HTTP exporter — sends spans to OTel Collector on port 4318.
	// Endpoint is read from OTEL_EXPORTER_OTLP_ENDPOINT env var,
	// defaulting to http://localhost:4318 when not set.
	exporter, err := otlptracehttp.New(ctx)
	if err != nil {
		return nil, fmt.Errorf("create OTLP exporter: %w", err)
	}

	// Tracer provider: batches spans and sends them via the exporter.
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
	)

	// Register as the global tracer provider so otel.Tracer() works anywhere.
	otel.SetTracerProvider(tp)

	// Register W3C Trace Context propagator — this is what reads/writes
	// the traceparent header on incoming and outgoing HTTP requests.
	otel.SetTextMapPropagator(propagation.TraceContext{})

	shutdown := func() {
		_ = tp.Shutdown(context.Background())
	}

	return shutdown, nil
}
