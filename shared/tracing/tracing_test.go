package tracing

import (
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

func TestInit_ReturnsShutdown(t *testing.T) {
	shutdown, err := Init("test-service")
	if err != nil {
		t.Fatalf("Init returned error: %v", err)
	}
	if shutdown == nil {
		t.Fatal("Init returned nil shutdown function")
	}
	shutdown()
}

func TestInit_RegistersTracerProvider(t *testing.T) {
	shutdown, err := Init("test-service")
	if err != nil {
		t.Fatalf("Init returned error: %v", err)
	}
	defer shutdown()

	tp, ok := otel.GetTracerProvider().(*sdktrace.TracerProvider)
	if !ok {
		t.Fatal("global tracer provider is not an *sdktrace.TracerProvider")
	}
	if tp == nil {
		t.Fatal("global tracer provider is nil")
	}
}

func TestInit_RegistersPropagator(t *testing.T) {
	shutdown, err := Init("test-service")
	if err != nil {
		t.Fatalf("Init returned error: %v", err)
	}
	defer shutdown()

	prop := otel.GetTextMapPropagator()
	if prop == nil {
		t.Fatal("global propagator is nil")
	}

	// Confirm it is a W3C TraceContext propagator by checking its fields list.
	fields := prop.Fields()
	found := false
	for _, f := range fields {
		if f == "traceparent" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("propagator fields %v do not include 'traceparent'", fields)
	}

	// Also confirm the concrete type.
	_, ok := prop.(propagation.TraceContext)
	if !ok {
		t.Fatalf("propagator is %T, want propagation.TraceContext", prop)
	}
}

func TestInit_TracerCreation(t *testing.T) {
	shutdown, err := Init("test-service")
	if err != nil {
		t.Fatalf("Init returned error: %v", err)
	}
	defer shutdown()

	tracer := otel.Tracer("test")
	if tracer == nil {
		t.Fatal("otel.Tracer returned nil")
	}
}

func TestShutdown_DoesNotPanic(t *testing.T) {
	shutdown, err := Init("test-service")
	if err != nil {
		t.Fatalf("Init returned error: %v", err)
	}

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("shutdown panicked: %v", r)
		}
	}()

	shutdown()
}
