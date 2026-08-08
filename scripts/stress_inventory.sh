#!/bin/bash
# Requires inventory service running:
#   go run ./services/inventory/main.go &

runs=10
requests_per_run=20
total_ms=0
overall_min=99999
overall_max=0

echo "Running $runs x $requests_per_run requests against inventory service..."
echo ""

for run in $(seq 1 $runs); do
  run_ms=0
  run_min=99999
  run_max=0

  for i in $(seq 1 $requests_per_run); do
    start=$(python3 -c "import time; print(int(time.time() * 1000))")
    curl -s "http://localhost:8082/stock?item_id=abc123" > /dev/null
    end=$(python3 -c "import time; print(int(time.time() * 1000))")
    elapsed=$(( end - start ))

    run_ms=$(( run_ms + elapsed ))
    [ $elapsed -lt $run_min ] && run_min=$elapsed
    [ $elapsed -gt $run_max ] && run_max=$elapsed
    [ $elapsed -lt $overall_min ] && overall_min=$elapsed
    [ $elapsed -gt $overall_max ] && overall_max=$elapsed
  done

  run_avg=$(( run_ms / requests_per_run ))
  total_ms=$(( total_ms + run_ms ))

  echo "Run $run: min=${run_min}ms  max=${run_max}ms  avg=${run_avg}ms"
done

total=$(( runs * requests_per_run ))
overall_avg=$(( total_ms / total ))

echo ""
echo "────────────────────────────────────────"
echo "Total requests: $total"
echo "Latency min:    ${overall_min}ms"
echo "Latency max:    ${overall_max}ms"
echo "Latency avg:    ${overall_avg}ms"
echo "Expected range: 50–500ms per request"
