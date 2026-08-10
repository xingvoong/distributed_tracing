#!/bin/bash
# Sends one checkout request, waits for the tail sampler to decide (15s),
# then dumps the otel-collector logs to show whether traces arrived.

echo "Sending checkout request..."
curl -s -X POST http://localhost:8080/checkout \
  -H "Content-Type: application/json" \
  -d '{"item_id":"abc123","amount":49.99,"user_id":"user-1"}' | python3 -m json.tool

echo ""
echo "Waiting 15s for tail sampler to flush..."
for i in $(seq 15 -1 1); do
  printf "\r  %2ds remaining..." $i
  sleep 1
done
echo ""

echo ""
echo "=== OTel Collector logs ==="
docker compose logs otel-collector | tail -20
