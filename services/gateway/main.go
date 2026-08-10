package main

import (
	"bytes"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"

	"github.com/xingvoong/distributed_tracing/shared/tracing"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
)

func orderServiceURL() string {
	if url := os.Getenv("ORDER_SERVICE_URL"); url != "" {
		return url
	}
	return "http://localhost:8081"
}

var tracer = otel.Tracer("gateway")

type checkoutRequest struct {
	ItemID string  `json:"item_id"`
	Amount float64 `json:"amount"`
	UserID string  `json:"user_id"`
}

func handleCheckout(w http.ResponseWriter, r *http.Request) {
	// No traceparent on incoming request — this creates the ROOT span.
	// There is no parent. This is the entry point of the entire trace.
	ctx := r.Context()
	ctx, span := tracer.Start(ctx, "gateway.checkout")
	defer span.End()

	var req checkoutRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		span.SetStatus(codes.Error, "invalid request body")
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	span.SetAttributes(
		attribute.String("user.id", req.UserID),
		attribute.String("http.method", r.Method),
		attribute.String("order.item_id", req.ItemID),
		attribute.Float64("order.amount", req.Amount),
	)

	// Forward the request to the order service.
	body, _ := json.Marshal(req)
	outgoing, _ := http.NewRequestWithContext(ctx, http.MethodPost,
		orderServiceURL()+"/order", bytes.NewBuffer(body))
	outgoing.Header.Set("Content-Type", "application/json")

	// Inject traceparent — this is what starts the chain of context propagation
	// through orders → inventory and orders → payments.
	otel.GetTextMapPropagator().Inject(ctx, propagation.HeaderCarrier(outgoing.Header))

	resp, err := http.DefaultClient.Do(outgoing)
	if err != nil {
		span.SetStatus(codes.Error, err.Error())
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	defer resp.Body.Close()

	span.SetAttributes(attribute.Int("http.status_code", resp.StatusCode))

	if resp.StatusCode != http.StatusOK {
		span.SetStatus(codes.Error, "order failed")
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}

func main() {
	shutdown, err := tracing.Init("gateway")
	if err != nil {
		log.Fatalf("failed to init tracing: %v", err)
	}
	defer shutdown()

	http.HandleFunc("/checkout", handleCheckout)

	log.Println("gateway service listening on :8080")
	if err := http.ListenAndServe(":8080", nil); err != nil {
		log.Fatalf("server error: %v", err)
	}
}
