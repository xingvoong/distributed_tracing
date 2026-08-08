#!/bin/bash

requests=10
total_ms=0
min_ms=99999
max_ms=0

echo "Sending $requests requests to inventory service..."
echo ""

for i in $(seq 1 $requests); do
  start=$(python3 -c "import time; print(int(time.time() * 1000))")
  result=$(curl -s "http://localhost:8082/stock?item_id=abc123")
  end=$(python3 -c "import time; print(int(time.time() * 1000))")
  elapsed=$(( end - start ))

  quantity=$(echo "$result" | python3 -c "import sys,json; print(json.load(sys.stdin)['quantity'])")
  echo "Request $i: ${elapsed}ms  quantity=$quantity"

  total_ms=$(( total_ms + elapsed ))
  [ $elapsed -lt $min_ms ] && min_ms=$elapsed
  [ $elapsed -gt $max_ms ] && max_ms=$elapsed
done

avg_ms=$(( total_ms / requests ))

echo ""
echo "────────────────────────"
echo "Min:  ${min_ms}ms"
echo "Max:  ${max_ms}ms"
echo "Avg:  ${avg_ms}ms"
echo "Expected range: 50–500ms"
