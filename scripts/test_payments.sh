#!/bin/bash

for i in $(seq 1 20); do
  result=$(curl -s -X POST http://localhost:8083/pay \
    -H 'Content-Type: application/json' \
    -d '{"amount": 49.99}')
  status=$(echo "$result" | python3 -c "import sys,json; print(json.load(sys.stdin)['status'])")
  echo "Request $i: $status"
done
