#!/bin/bash

runs=10
requests_per_run=100
total_approved=0
total_declined=0

for run in $(seq 1 $runs); do
  approved=0
  declined=0

  for i in $(seq 1 $requests_per_run); do
    result=$(curl -s -X POST http://localhost:8083/pay \
      -H 'Content-Type: application/json' \
      -d '{"amount": 49.99}')
    status=$(echo "$result" | python3 -c "import sys,json; print(json.load(sys.stdin)['status'])")

    if [ "$status" = "approved" ]; then
      ((approved++))
    else
      ((declined++))
    fi
  done

  total_approved=$((total_approved + approved))
  total_declined=$((total_declined + declined))

  echo "Run $run: approved=$approved  declined=$declined  failure_rate=$(( declined * 100 / requests_per_run ))%"
done

total=$(( runs * requests_per_run ))
echo ""
echo "────────────────────────────────────────"
echo "Total requests: $total"
echo "Total approved: $total_approved  ($(( total_approved * 100 / total ))%)"
echo "Total declined: $total_declined  ($(( total_declined * 100 / total ))%)"
echo "Expected failure rate: ~10%"
