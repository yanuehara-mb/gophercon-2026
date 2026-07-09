#!/usr/bin/env bash
set -euo pipefail
HYDRA_ADMIN=${HYDRA_ADMIN:-http://localhost:4445}

for client in alice bob; do
  curl -sf -X POST "$HYDRA_ADMIN/admin/clients" \
    -H "Content-Type: application/json" \
    -d "{\"client_id\":\"client-$client\",\"client_secret\":\"secret-$client\",\"grant_types\":[\"client_credentials\"]}" \
    | jq -r '.client_id' || true
  echo "  → client-$client created"
done
