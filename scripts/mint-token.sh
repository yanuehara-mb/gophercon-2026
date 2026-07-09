#!/usr/bin/env bash
set -euo pipefail
CLIENT_ID=${1:?usage: mint-token.sh <client-id> <client-secret> [HYDRA_PUBLIC=http://localhost:4444]}
CLIENT_SECRET=${2:?usage: mint-token.sh <client-id> <client-secret> [HYDRA_PUBLIC=http://localhost:4444]}
HYDRA_PUBLIC=${HYDRA_PUBLIC:-http://localhost:4444}

curl -sf -X POST "$HYDRA_PUBLIC/oauth2/token" \
  -u "$CLIENT_ID:$CLIENT_SECRET" \
  -d "grant_type=client_credentials" \
  | jq -r '.access_token'
