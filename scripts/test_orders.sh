#!/bin/bash
# Requires all three services running:
#   go run ./services/payments/main.go &
#   go run ./services/inventory/main.go &
#   go run ./services/orders/main.go &

requests=20
confirmed=0
failed=0

echo "Sending $requests requests to order service..."
echo ""

for i in $(seq 1 $requests); do
  start=$(python3 -c "import time; print(int(time.time() * 1000))")
  result=$(curl -s -X POST http://localhost:8081/order \
    -H 'Content-Type: application/json' \
    -d '{"item_id": "abc123", "amount": 49.99, "user_id": "u42"}')
  end=$(python3 -c "import time; print(int(time.time() * 1000))")
  elapsed=$(( end - start ))

  status=$(echo "$result" | python3 -c "import sys,json; print(json.load(sys.stdin)['status'])")
  payment=$(echo "$result" | python3 -c "import sys,json; d=json.load(sys.stdin); print(d.get('payment', d.get('message', 'n/a')))")
  quantity=$(echo "$result" | python3 -c "import sys,json; d=json.load(sys.stdin); print(d.get('quantity', 'n/a'))")

  echo "Request $i: ${elapsed}ms  order=$status  payment=$payment  quantity=$quantity"

  if [ "$status" = "confirmed" ]; then
    ((confirmed++))
  else
    ((failed++))
  fi
done

echo ""
echo "────────────────────────────────────────"
echo "Total:     $requests"
echo "Confirmed: $confirmed  ($(( confirmed * 100 / requests ))%)"
echo "Failed:    $failed     ($(( failed * 100 / requests ))%)"
echo "Expected failure rate: ~10% (driven by payment service)"
