#!/usr/bin/env bash
set -euo pipefail
HYDRA_ADMIN=${HYDRA_ADMIN:-http://localhost:4445}

echo "Waiting for Hydra to be ready..."
until curl -sf "$HYDRA_ADMIN/health/ready" > /dev/null 2>&1; do sleep 1; done
echo "Hydra ready."

for client in alice bob; do
  response=$(curl -sf -w "\n%{http_code}" -X POST "$HYDRA_ADMIN/admin/clients" \
    -H "Content-Type: application/json" \
    -d "{\"client_id\":\"client-$client\",\"client_secret\":\"secret-$client\",\"grant_types\":[\"client_credentials\"]}")
  http_code=$(echo "$response" | tail -n1)
  body=$(echo "$response" | head -n-1)

  if [ "$http_code" = "201" ]; then
    echo "  → client-$client created ($(echo "$body" | jq -r '.client_id'))"
  elif [ "$http_code" = "409" ]; then
    echo "  → client-$client already exists, skipping"
  else
    echo "  ✗ client-$client failed (HTTP $http_code)" >&2
    exit 1
  fi
done
