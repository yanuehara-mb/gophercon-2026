#!/bin/bash
# Requires hydra CLI: go install github.com/ory/hydra/v2@latest
set -e

HYDRA_ADMIN=${HYDRA_ADMIN:-http://localhost:4445}
HYDRA_PUBLIC=${HYDRA_PUBLIC:-http://localhost:4444}

echo "Creating OAuth2 client..."
CLIENT_JSON=$(hydra create client \
  --endpoint "$HYDRA_ADMIN" \
  --grant-type client_credentials \
  --name demo-client \
  --format json)

CLIENT_ID=$(echo "$CLIENT_JSON" | grep -o '"client_id":"[^"]*"' | cut -d'"' -f4)
CLIENT_SECRET=$(echo "$CLIENT_JSON" | grep -o '"client_secret":"[^"]*"' | cut -d'"' -f4)

echo "Client ID: $CLIENT_ID"
echo "Requesting token..."

hydra perform client-credentials \
  --endpoint "$HYDRA_PUBLIC" \
  --client-id "$CLIENT_ID" \
  --client-secret "$CLIENT_SECRET"
