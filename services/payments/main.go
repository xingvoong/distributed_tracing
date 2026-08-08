package main

import (
	"encoding/json"
	"log"
	"math/rand"
	"net/http"

	"github.com/xingvoong/distributed_tracing/shared/tracing"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
)

var tracer = otel.Tracer("payments")

type payRequest struct {
	Amount float64 `json:"amount"`
}

type payResponse struct {
	Status  string  `json:"status"`
	Amount  float64 `json:"amount"`
	Message string  `json:"message,omitempty"`
}

func handlePay(w http.ResponseWriter, r *http.Request) {
	// Extract traceparent from incoming request and create a child span.
	ctx := r.Context()
	ctx, span := tracer.Start(ctx, "payments.charge")
	defer span.End()

	var req payRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		req.Amount = 0
	}

	// Tag the span with payment details.
	span.SetAttributes(
		attribute.Float64("payment.amount", req.Amount),
	)

	// 10% random failure to simulate a declined payment.
	if rand.Intn(10) == 0 {
		span.SetAttributes(attribute.String("payment.status", "declined"))
		span.RecordError(nil)
		span.SetStatus(codes.Error, "card declined: insufficient funds")
		span.AddEvent("payment failed")

		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(payResponse{
			Status:  "declined",
			Amount:  req.Amount,
			Message: "card declined: insufficient funds",
		})
		return
	}

	span.SetAttributes(attribute.String("payment.status", "approved"))
	span.AddEvent("payment processed")

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(payResponse{
		Status: "approved",
		Amount: req.Amount,
	})
}

func main() {
	shutdown, err := tracing.Init("payments")
	if err != nil {
		log.Fatalf("failed to init tracing: %v", err)
	}
	defer shutdown()

	http.HandleFunc("/pay", handlePay)

	log.Println("payments service listening on :8083")
	if err := http.ListenAndServe(":8083", nil); err != nil {
		log.Fatalf("server error: %v", err)
	}
}
