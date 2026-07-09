# Demo Authenticated Route Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `/demo/get` and `/demo/post` route protected by Oathkeeper (OAuth2 introspection via Hydra + SpiceDB authorization), proxying to httpbin, with two Hydra clients where only one is authorized.

**Architecture:** No new Go code. Oathkeeper is reconfigured to use `oauth2_introspection` as authenticator (pointing to our existing `/oauth2/introspect` facade) and `remote_json` as authorizer (pointing to our existing `/authorization/verify` facade) with a dynamic subject derived from the token. Two bash scripts create Hydra clients and mint tokens; `seed.go` is updated to accept `ALLOWED_SUBJECT` env var and use `OPERATION_TOUCH` for idempotent re-runs.

**Tech Stack:** Ory Oathkeeper v0.40.7, `mccutchen/go-httpbin:latest`, Bash + curl + jq, authzed-go v1 (existing dep)

## Global Constraints

- Oathkeeper version: v0.40.7 — do not change the image tag
- httpbin image: `mccutchen/go-httpbin:latest`, listens on port 8080
- Protected paths: `GET /demo/get` and `POST /demo/post` — no other paths
- `oauth2_introspection` introspection_url: `http://app:8080/oauth2/introspect`
- Authorizer remote: `http://app:8080/authorization/verify`
- Authorizer payload subject: `"user:{{ print .Subject }}"` — exact template, no variation
- `ALLOWED_SUBJECT` env var default: `client-alice`
- Hydra clients: `client-alice`/`secret-alice` and `client-bob`/`secret-bob`
- `seed.go` writes use `OPERATION_TOUCH` — not `OPERATION_CREATE`
- Scripts use only `curl` and `jq` — no other CLI tools
- Git author on every commit: `yanuehara-mb <yan.uehara@mb.com.br>`
- All commits via `git commit --author="yanuehara-mb <yan.uehara@mb.com.br>"`

---

## File Map

| File | Action | Responsibility |
|------|--------|----------------|
| `docker-compose.yml` | Modify | Add `httpbin` service; add `httpbin` to oathkeeper `depends_on` |
| `configs/oathkeeper/config.yml` | Modify | Enable `oauth2_introspection` authenticator |
| `configs/oathkeeper/access-rules.yml` | Modify | Replace `demo-authorization` with `demo-httpbin` |
| `scripts/seed.go` | Modify | Accept `ALLOWED_SUBJECT` env var; use `OPERATION_TOUCH` |
| `scripts/create-clients.sh` | Create | Create two Hydra clients via Admin API |
| `scripts/mint-token.sh` | Create | Mint a client_credentials token for a given client |

---

### Task 1: Add httpbin to Docker Compose and update Oathkeeper config

**Files:**
- Modify: `docker-compose.yml`
- Modify: `configs/oathkeeper/config.yml`

**Interfaces:**
- Produces: `httpbin` reachable at `http://httpbin:8080` inside the compose network; `oauth2_introspection` authenticator enabled in Oathkeeper

- [ ] **Step 1: Add httpbin service to docker-compose.yml**

The full updated `docker-compose.yml` — add the `httpbin` block and update `oathkeeper.depends_on`:

```yaml
services:
  postgres:
    image: postgres:16-alpine
    environment:
      POSTGRES_USER: postgres
      POSTGRES_PASSWORD: postgres
    volumes:
      - ./configs/postgres/init.sql:/docker-entrypoint-initdb.d/init.sql
    healthcheck:
      test: ["CMD", "pg_isready", "-U", "postgres"]
      interval: 5s
      timeout: 3s
      retries: 10

  hydra-migrate:
    image: oryd/hydra:v2.2.0
    command: migrate sql --yes postgres://postgres:postgres@postgres:5432/hydra?sslmode=disable
    depends_on:
      postgres:
        condition: service_healthy
    restart: on-failure

  spicedb-migrate:
    image: authzed/spicedb:v1.35.0
    command: >
      migrate head
      --datastore-engine=postgres
      --datastore-conn-uri=postgres://postgres:postgres@postgres:5432/spicedb?sslmode=disable
    depends_on:
      postgres:
        condition: service_healthy
    restart: on-failure

  hydra:
    image: oryd/hydra:v2.2.0
    command: serve all --dev
    environment:
      DSN: postgres://postgres:postgres@postgres:5432/hydra?sslmode=disable
      URLS_SELF_ISSUER: http://hydra:4444/
      SECRETS_SYSTEM: youReallyNeedToChangeThis
      LOG_LEVEL: warn
    ports:
      - "4444:4444"
      - "4445:4445"
    depends_on:
      hydra-migrate:
        condition: service_completed_successfully
    healthcheck:
      test: ["CMD", "wget", "-qO-", "http://localhost:4445/health/ready"]
      interval: 5s
      timeout: 3s
      retries: 12

  spicedb:
    image: authzed/spicedb:v1.35.0
    command: >
      serve
      --grpc-preshared-key=somerandomkeyhere
      --datastore-engine=postgres
      --datastore-conn-uri=postgres://postgres:postgres@postgres:5432/spicedb?sslmode=disable
    ports:
      - "50051:50051"
    depends_on:
      spicedb-migrate:
        condition: service_completed_successfully

  app:
    build: .
    ports:
      - "8080:8080"
    environment:
      HYDRA_URL: http://hydra:4445
      SPICEDB_ADDR: spicedb:50051
      SPICEDB_TOKEN: somerandomkeyhere
      REDIS_ADDR: redis:6379
    depends_on:
      hydra:
        condition: service_healthy
      spicedb:
        condition: service_started
      redis:
        condition: service_healthy

  redis:
    image: redis:7-alpine
    ports:
      - "6379:6379"
    healthcheck:
      test: ["CMD", "redis-cli", "ping"]
      interval: 5s
      timeout: 3s
      retries: 5

  httpbin:
    image: mccutchen/go-httpbin:latest
    ports:
      - "8081:8080"

  oathkeeper:
    image: oryd/oathkeeper:v0.40.7
    command: serve --config /etc/oathkeeper/config.yml
    volumes:
      - ./configs/oathkeeper:/etc/oathkeeper
    ports:
      - "4455:4455"
    depends_on:
      - app
      - httpbin
```

- [ ] **Step 2: Enable oauth2_introspection in Oathkeeper config**

Full replacement of `configs/oathkeeper/config.yml`:

```yaml
serve:
  proxy:
    port: 4455

log:
  level: warn

access_rules:
  repositories:
    - file:///etc/oathkeeper/access-rules.yml

authenticators:
  noop:
    enabled: true
  oauth2_introspection:
    enabled: true
    config:
      introspection_url: http://app:8080/oauth2/introspect
      scope_strategy: none
      pre_authorization:
        enabled: false

authorizers:
  remote_json:
    enabled: true
    config:
      remote: http://app:8080/authorization/verify
      payload: |
        {}

mutators:
  noop:
    enabled: true

errors:
  handlers:
    json:
      enabled: true
```

- [ ] **Step 3: Verify Oathkeeper starts cleanly with the new config**

```bash
docker compose up -d oathkeeper
sleep 3
docker compose logs oathkeeper --tail=10
```

Expected: no `level=fatal` lines. Container status must be `Up`.

- [ ] **Step 4: Commit**

```bash
git add docker-compose.yml configs/oathkeeper/config.yml
git commit --author="yanuehara-mb <yan.uehara@mb.com.br>" -m "feat: add httpbin service and enable oauth2_introspection in oathkeeper"
```

---

### Task 2: Replace Oathkeeper access rule with demo-httpbin

**Files:**
- Modify: `configs/oathkeeper/access-rules.yml`

**Interfaces:**
- Consumes: `oauth2_introspection` authenticator (Task 1); `http://app:8080/authorization/verify` (existing)
- Produces: `GET /demo/get` and `POST /demo/post` protected by authn + authz, upstream to `http://httpbin:8080`

- [ ] **Step 1: Replace access-rules.yml**

Full replacement of `configs/oathkeeper/access-rules.yml`:

```yaml
- id: demo-httpbin
  upstream:
    url: http://httpbin:8080
    strip_path: /demo
  match:
    url: http://localhost:4455/demo/<(get|post)>
    methods:
      - GET
      - POST
  authenticators:
    - handler: oauth2_introspection
  authorizer:
    handler: remote_json
    config:
      remote: http://app:8080/authorization/verify
      payload: |
        {
          "subject": "user:{{ print .Subject }}",
          "object": "document:readme",
          "permission": "read"
        }
  mutators:
    - handler: noop
```

- [ ] **Step 2: Restart Oathkeeper and verify no rule errors**

```bash
docker compose restart oathkeeper
sleep 3
docker compose logs oathkeeper --tail=15
```

Expected: no `level=error` or `level=fatal` lines. No "misconfigured rule" messages.

- [ ] **Step 3: Verify unauthenticated request returns 401**

```bash
curl -s -o /dev/null -w "%{http_code}" http://localhost:4455/demo/get
```

Expected: `401`

- [ ] **Step 4: Commit**

```bash
git add configs/oathkeeper/access-rules.yml
git commit --author="yanuehara-mb <yan.uehara@mb.com.br>" -m "feat: replace demo-authorization with demo-httpbin access rule"
```

---

### Task 3: Update seed.go and create client scripts

**Files:**
- Modify: `scripts/seed.go`
- Create: `scripts/create-clients.sh`
- Create: `scripts/mint-token.sh`

**Interfaces:**
- Produces: `ALLOWED_SUBJECT` env var controls which subject gets `reader` on `document:readme`; `create-clients.sh` creates two Hydra clients; `mint-token.sh <client-id> <client-secret>` prints a raw access token

- [ ] **Step 1: Update scripts/seed.go**

Replace `scripts/seed.go` with the following (only two changes from the original: `ALLOWED_SUBJECT` env var + `OPERATION_TOUCH`):

```go
//go:build ignore

package main

import (
	"context"
	"log"
	"os"

	authzed "github.com/authzed/authzed-go/v1"
	v1 "github.com/authzed/authzed-go/proto/authzed/api/v1"
	"github.com/authzed/grpcutil"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func main() {
	addr := "localhost:50051"
	token := "somerandomkeyhere"
	if v := os.Getenv("SPICEDB_ADDR"); v != "" {
		addr = v
	}
	if v := os.Getenv("SPICEDB_TOKEN"); v != "" {
		token = v
	}

	allowedSubject := "client-alice"
	if v := os.Getenv("ALLOWED_SUBJECT"); v != "" {
		allowedSubject = v
	}

	client, err := authzed.NewClient(addr,
		grpcutil.WithInsecureBearerToken(token),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		log.Fatalf("connect: %v", err)
	}

	ctx := context.Background()

	schema := `definition user {}

definition document {
  relation reader: user
  permission read = reader
}`

	_, err = client.WriteSchema(ctx, &v1.WriteSchemaRequest{Schema: schema})
	if err != nil {
		log.Fatalf("write schema: %v", err)
	}
	log.Println("schema written")

	_, err = client.WriteRelationships(ctx, &v1.WriteRelationshipsRequest{
		Updates: []*v1.RelationshipUpdate{
			{
				Operation: v1.RelationshipUpdate_OPERATION_TOUCH,
				Relationship: &v1.Relationship{
					Resource: &v1.ObjectReference{ObjectType: "document", ObjectId: "readme"},
					Relation: "reader",
					Subject:  &v1.SubjectReference{Object: &v1.ObjectReference{ObjectType: "user", ObjectId: allowedSubject}},
				},
			},
		},
	})
	if err != nil {
		log.Fatalf("write relationship: %v", err)
	}
	log.Printf("relationship created: user:%s reader document:readme", allowedSubject)
	log.Println("SpiceDB seeded successfully.")
}
```

- [ ] **Step 2: Verify seed.go compiles**

```bash
~/go/bin/go build -v scripts/seed.go 2>&1 || true
```

Expected: no output (build ignore tag skips it) or no error. The `//go:build ignore` tag means `go build` won't compile it as part of the module — that's expected behavior.

- [ ] **Step 3: Create scripts/create-clients.sh**

```bash
#!/usr/bin/env bash
set -euo pipefail
HYDRA_ADMIN=${HYDRA_ADMIN:-http://localhost:4445}

for client in alice bob; do
  curl -sf -X POST "$HYDRA_ADMIN/admin/clients" \
    -H "Content-Type: application/json" \
    -d "{\"client_id\":\"client-$client\",\"client_secret\":\"secret-$client\",\"grant_types\":[\"client_credentials\"]}" \
    | jq -r '.client_id'
  echo "  → client-$client created"
done
```

- [ ] **Step 4: Make create-clients.sh executable**

```bash
chmod +x scripts/create-clients.sh
```

- [ ] **Step 5: Create scripts/mint-token.sh**

```bash
#!/usr/bin/env bash
set -euo pipefail
CLIENT_ID=${1:?usage: mint-token.sh <client-id> <client-secret>}
CLIENT_SECRET=${2:?usage: mint-token.sh <client-id> <client-secret>}
HYDRA_PUBLIC=${HYDRA_PUBLIC:-http://localhost:4444}

curl -sf -X POST "$HYDRA_PUBLIC/oauth2/token" \
  -u "$CLIENT_ID:$CLIENT_SECRET" \
  -d "grant_type=client_credentials" \
  | jq -r '.access_token'
```

- [ ] **Step 6: Make mint-token.sh executable**

```bash
chmod +x scripts/mint-token.sh
```

- [ ] **Step 7: Run the full demo playbook to verify end-to-end**

```bash
# Create clients (skip if already created — Hydra returns 409 on conflict, script may error; that's fine)
./scripts/create-clients.sh 2>/dev/null || echo "clients may already exist"

# Seed SpiceDB with client-alice
ALLOWED_SUBJECT=client-alice ~/go/bin/go run scripts/seed.go

# Mint tokens
TOKEN_ALICE=$(./scripts/mint-token.sh client-alice secret-alice)
TOKEN_BOB=$(./scripts/mint-token.sh client-bob secret-bob)

# 200 — allowed
curl -s -o /dev/null -w "%{http_code}" \
  -H "Authorization: Bearer $TOKEN_ALICE" \
  http://localhost:4455/demo/get
# Expected: 200

# 403 — denied
curl -s -o /dev/null -w "%{http_code}" \
  -H "Authorization: Bearer $TOKEN_BOB" \
  http://localhost:4455/demo/get
# Expected: 403

# 401 — unauthenticated
curl -s -o /dev/null -w "%{http_code}" \
  http://localhost:4455/demo/get
# Expected: 401

# POST path also works
curl -s -o /dev/null -w "%{http_code}" \
  -H "Authorization: Bearer $TOKEN_ALICE" \
  -X POST http://localhost:4455/demo/post
# Expected: 200
```

- [ ] **Step 8: Commit**

```bash
git add scripts/seed.go scripts/create-clients.sh scripts/mint-token.sh
git commit --author="yanuehara-mb <yan.uehara@mb.com.br>" -m "feat: parameterize seed.go and add client creation + token minting scripts"
```

---

## Self-Review

**Spec coverage:**
- [x] httpbin in docker-compose — Task 1
- [x] `oauth2_introspection` enabled in Oathkeeper config — Task 1
- [x] `demo-authorization` replaced with `demo-httpbin` — Task 2
- [x] 401 on unauthenticated — Task 2 Step 3
- [x] `seed.go` uses `OPERATION_TOUCH` — Task 3 Step 1
- [x] `ALLOWED_SUBJECT` env var with default `client-alice` — Task 3 Step 1
- [x] `create-clients.sh` creates `client-alice` and `client-bob` — Task 3 Step 3
- [x] `mint-token.sh` prints raw access token — Task 3 Step 5
- [x] 200/403/401 smoke test — Task 3 Step 7
- [x] `POST /demo/post` also tested — Task 3 Step 7

**Placeholder scan:** None found.

**Type consistency:** No Go types involved — all config and bash. Consistent use of `client-alice`/`secret-alice`, `client-bob`/`secret-bob`, and `document:readme` throughout.
