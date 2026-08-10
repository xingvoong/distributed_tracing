#!/bin/bash
# Sends 30 checkout requests to generate a mix of traces:
# - ~10% payment failures (kept by tail sampler at 100%)
# - ~some slow traces >400ms (kept at 100%)
# - ~10% of normal traces kept by probabilistic sampler

requests=30
success=0
failed=0

echo "Sending $requests checkout requests..."
echo ""

for i in $(seq 1 $requests); do
  response=$(curl -s -X POST http://localhost:8080/checkout \
    -H "Content-Type: application/json" \
    -d "{\"item_id\":\"abc123\",\"amount\":49.99,\"user_id\":\"user-$i\"}")

  status=$(echo "$response" | python3 -c "import sys,json; print(json.load(sys.stdin).get('status','unknown'))" 2>/dev/null)

  if [ "$status" = "confirmed" ]; then
    success=$((success + 1))
  else
    failed=$((failed + 1))
  fi

  echo "[$i/$requests] $status"
done

echo ""
echo "────────────────────────────────────────"
echo "Confirmed: $success"
echo "Failed:    $failed"
echo ""
echo "Waiting 15s for tail sampler to flush..."
for i in $(seq 15 -1 1); do
  printf "\r  %2ds remaining..." $i
  sleep 1
done
echo ""
echo ""
echo "Open Jaeger: http://localhost:16686"
