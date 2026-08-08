#!/bin/bash
# Requires all three services running:
#   go run ./services/payments/main.go &
#   go run ./services/inventory/main.go &
#   go run ./services/orders/main.go &

runs=10
requests_per_run=50
total_confirmed=0
total_failed=0
total_ms=0
min_ms=99999
max_ms=0

echo "Running $runs x $requests_per_run requests against order service..."
echo ""

for run in $(seq 1 $runs); do
  confirmed=0
  failed=0
  run_ms=0

  for i in $(seq 1 $requests_per_run); do
    start=$(python3 -c "import time; print(int(time.time() * 1000))")
    result=$(curl -s -X POST http://localhost:8081/order \
      -H 'Content-Type: application/json' \
      -d '{"item_id": "abc123", "amount": 49.99, "user_id": "u42"}')
    end=$(python3 -c "import time; print(int(time.time() * 1000))")
    elapsed=$(( end - start ))

    status=$(echo "$result" | python3 -c "import sys,json; print(json.load(sys.stdin)['status'])")

    run_ms=$(( run_ms + elapsed ))
    [ $elapsed -lt $min_ms ] && min_ms=$elapsed
    [ $elapsed -gt $max_ms ] && max_ms=$elapsed

    if [ "$status" = "confirmed" ]; then
      ((confirmed++))
    else
      ((failed++))
    fi
  done

  avg_ms=$(( run_ms / requests_per_run ))
  total_confirmed=$(( total_confirmed + confirmed ))
  total_failed=$(( total_failed + failed ))
  total_ms=$(( total_ms + run_ms ))

  echo "Run $run: confirmed=$confirmed  failed=$failed  failure_rate=$(( failed * 100 / requests_per_run ))%  avg_latency=${avg_ms}ms"
done

total=$(( runs * requests_per_run ))
overall_avg=$(( total_ms / total ))

echo ""
echo "────────────────────────────────────────────────────────"
echo "Total requests:  $total"
echo "Total confirmed: $total_confirmed  ($(( total_confirmed * 100 / total ))%)"
echo "Total failed:    $total_failed     ($(( total_failed * 100 / total ))%)"
echo "Latency min:     ${min_ms}ms"
echo "Latency max:     ${max_ms}ms"
echo "Latency avg:     ${overall_avg}ms"
echo "Expected failure rate: ~10% (driven by payment service)"
echo "Expected latency:      50–500ms+ (driven by inventory service)"
