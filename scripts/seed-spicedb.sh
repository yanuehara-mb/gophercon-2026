#!/bin/bash
# Requires zed CLI: go install github.com/authzed/zed@latest
set -e

SPICEDB_ADDR=${SPICEDB_ADDR:-localhost:50051}
SPICEDB_TOKEN=${SPICEDB_TOKEN:-somerandomkeyhere}

echo "Writing SpiceDB schema..."
zed schema write configs/spicedb/schema.zed \
  --endpoint "$SPICEDB_ADDR" \
  --token "$SPICEDB_TOKEN" \
  --insecure

echo "Creating relationship: user:alice reader document:readme..."
zed relationship create document:readme reader user:alice \
  --endpoint "$SPICEDB_ADDR" \
  --token "$SPICEDB_TOKEN" \
  --insecure

echo "SpiceDB seeded successfully."
