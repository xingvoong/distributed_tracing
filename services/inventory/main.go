package main

import (
	"encoding/json"
	"log"
	"math/rand"
	"net/http"
	"time"

	"github.com/xingvoong/distributed_tracing/shared/tracing"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/propagation"
)

var tracer = otel.Tracer("inventory")

type stockResponse struct {
	ItemID   string `json:"item_id"`
	Quantity int    `json:"quantity"`
}

func handleStock(w http.ResponseWriter, r *http.Request) {
	// Extract traceparent from incoming request and create a child span.
	ctx := otel.GetTextMapPropagator().Extract(r.Context(), propagation.HeaderCarrier(r.Header))
	ctx, span := tracer.Start(ctx, "inventory.check")
	defer span.End()

	itemID := r.URL.Query().Get("item_id")
	if itemID == "" {
		itemID = "unknown"
	}

	// Simulate a DB query with random latency 50–500ms.
	delay := time.Duration(50+rand.Intn(451)) * time.Millisecond
	time.Sleep(delay)

	quantity := rand.Intn(100) + 1

	span.SetAttributes(
		attribute.String("inventory.item_id", itemID),
		attribute.Int("inventory.quantity", quantity),
		attribute.Int64("inventory.delay_ms", delay.Milliseconds()),
	)
	span.AddEvent("stock check complete")

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(stockResponse{
		ItemID:   itemID,
		Quantity: quantity,
	})
}

func main() {
	shutdown, err := tracing.Init("inventory")
	if err != nil {
		log.Fatalf("failed to init tracing: %v", err)
	}
	defer shutdown()

	http.HandleFunc("/stock", handleStock)

	log.Println("inventory service listening on :8082")
	if err := http.ListenAndServe(":8082", nil); err != nil {
		log.Fatalf("server error: %v", err)
	}
}
