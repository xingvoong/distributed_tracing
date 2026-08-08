#!/bin/bash

approved=0
declined=0
total=100

for i in $(seq 1 $total); do
  result=$(curl -s -X POST http://localhost:8083/pay \
    -H 'Content-Type: application/json' \
    -d '{"amount": 49.99}')
  status=$(echo "$result" | python3 -c "import sys,json; print(json.load(sys.stdin)['status'])")
  echo "Request $i: $status"

  if [ "$status" = "approved" ]; then
    ((approved++))
  else
    ((declined++))
  fi
done

echo ""
echo "────────────────────────"
echo "Total:    $total"
echo "Approved: $approved  ($(( approved * 100 / total ))%)"
echo "Declined: $declined  ($(( declined * 100 / total ))%)"
