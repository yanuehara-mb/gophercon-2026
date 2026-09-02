#!/usr/bin/env bash
set -euo pipefail
CLIENT_ID=${1:?usage: mint-token.sh <client-id> <client-secret> [FACADE_URL=http://localhost:8080]}
CLIENT_SECRET=${2:?usage: mint-token.sh <client-id> <client-secret> [FACADE_URL=http://localhost:8080]}
FACADE_URL=${FACADE_URL:-http://localhost:8080}

curl -sf -X POST "$FACADE_URL/oauth2/token" \
  -u "$CLIENT_ID:$CLIENT_SECRET" \
  -d "grant_type=client_credentials" \
  | jq -r '.access_token'
