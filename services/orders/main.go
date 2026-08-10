package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"sync"

	"github.com/xingvoong/distributed_tracing/shared/tracing"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
)

func inventoryServiceURL() string {
	if url := os.Getenv("INVENTORY_SERVICE_URL"); url != "" {
		return url
	}
	return "http://localhost:8082"
}

func paymentServiceURL() string {
	if url := os.Getenv("PAYMENT_SERVICE_URL"); url != "" {
		return url
	}
	return "http://localhost:8083"
}

var tracer = otel.Tracer("orders")

type orderRequest struct {
	ItemID string  `json:"item_id"`
	Amount float64 `json:"amount"`
	UserID string  `json:"user_id"`
}

type stockResponse struct {
	ItemID   string `json:"item_id"`
	Quantity int    `json:"quantity"`
}

type payResponse struct {
	Status  string  `json:"status"`
	Amount  float64 `json:"amount"`
	Message string  `json:"message,omitempty"`
}

type orderResponse struct {
	Status    string  `json:"status"`
	ItemID    string  `json:"item_id"`
	Quantity  int     `json:"quantity"`
	Payment   string  `json:"payment"`
	Amount    float64 `json:"amount"`
	Message   string  `json:"message,omitempty"`
}

// injectContext injects the current trace context into an outgoing HTTP request.
// This is what writes the traceparent header so downstream services
// can create child spans linked to this trace.
func injectContext(ctx context.Context, req *http.Request) {
	otel.GetTextMapPropagator().Inject(ctx, propagation.HeaderCarrier(req.Header))
}

func callInventory(ctx context.Context, itemID string) (stockResponse, error) {
	ctx, span := tracer.Start(ctx, "orders.call_inventory")
	defer span.End()

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet,
		fmt.Sprintf("%s/stock?item_id=%s", inventoryServiceURL(), itemID), nil)
	injectContext(ctx, req)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		span.SetStatus(codes.Error, err.Error())
		return stockResponse{}, err
	}
	defer resp.Body.Close()

	var stock stockResponse
	json.NewDecoder(resp.Body).Decode(&stock)
	span.SetAttributes(attribute.Int("inventory.quantity", stock.Quantity))
	return stock, nil
}

func callPayment(ctx context.Context, amount float64) (payResponse, error) {
	ctx, span := tracer.Start(ctx, "orders.call_payment")
	defer span.End()

	body, _ := json.Marshal(map[string]float64{"amount": amount})
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost,
		paymentServiceURL()+"/pay", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	injectContext(ctx, req)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		span.SetStatus(codes.Error, err.Error())
		return payResponse{}, err
	}
	defer resp.Body.Close()

	var pay payResponse
	json.NewDecoder(resp.Body).Decode(&pay)

	if resp.StatusCode != http.StatusOK {
		span.SetStatus(codes.Error, pay.Message)
		return pay, fmt.Errorf("payment declined: %s", pay.Message)
	}

	span.SetAttributes(attribute.String("payment.status", pay.Status))
	return pay, nil
}

func handleOrder(w http.ResponseWriter, r *http.Request) {
	ctx := otel.GetTextMapPropagator().Extract(r.Context(), propagation.HeaderCarrier(r.Header))
	ctx, span := tracer.Start(ctx, "orders.process")
	defer span.End()

	var req orderRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		span.SetStatus(codes.Error, "invalid request body")
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	span.SetAttributes(
		attribute.String("order.item_id", req.ItemID),
		attribute.String("order.user_id", req.UserID),
		attribute.Float64("order.amount", req.Amount),
	)

	// Fan-out: call inventory and payment in parallel.
	// Context is passed explicitly into each goroutine — this is what
	// links the child spans to this parent span across goroutines.
	var (
		stock    stockResponse
		pay      payResponse
		stockErr error
		payErr   error
		wg       sync.WaitGroup
	)

	wg.Add(2)

	go func(ctx context.Context) {
		defer wg.Done()
		stock, stockErr = callInventory(ctx, req.ItemID)
	}(ctx)

	go func(ctx context.Context) {
		defer wg.Done()
		pay, payErr = callPayment(ctx, req.Amount)
	}(ctx)

	wg.Wait()

	// Surface errors from either downstream service onto this span.
	if stockErr != nil {
		span.SetStatus(codes.Error, fmt.Sprintf("inventory error: %s", stockErr))
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(orderResponse{Status: "failed", Message: stockErr.Error()})
		return
	}

	if payErr != nil {
		span.SetStatus(codes.Error, fmt.Sprintf("payment error: %s", payErr))
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(orderResponse{
			Status:   "failed",
			ItemID:   req.ItemID,
			Quantity: stock.Quantity,
			Payment:  pay.Status,
			Amount:   req.Amount,
			Message:  pay.Message,
		})
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(orderResponse{
		Status:   "confirmed",
		ItemID:   req.ItemID,
		Quantity: stock.Quantity,
		Payment:  pay.Status,
		Amount:   req.Amount,
	})
}

func main() {
	shutdown, err := tracing.Init("orders")
	if err != nil {
		log.Fatalf("failed to init tracing: %v", err)
	}
	defer shutdown()

	http.HandleFunc("/order", handleOrder)

	log.Println("orders service listening on :8081")
	if err := http.ListenAndServe(":8081", nil); err != nil {
		log.Fatalf("server error: %v", err)
	}
}
