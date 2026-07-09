# Demo Authenticated Route Implementation Design

## Goal

Add a route to the GopherCon 2026 demo that is both authenticated (via Ory Hydra OAuth2 introspection through our facade) and authorized (via SpiceDB through our facade), proxying traffic to httpbin. Two credentials are created — only one is granted permission — demonstrating the full allow/deny flow.

## Architecture

```
Client
  → Bearer token → Oathkeeper :4455/demo/get (or /demo/post)
      → [authn] oauth2_introspection → app:8080/oauth2/introspect → Hydra :4445
      → [authz] remote_json         → app:8080/authorization/verify → SpiceDB :50051
      → [proxy]                     → httpbin:8080/get (or /post)
```

No new Go code. Changes are confined to Oathkeeper config, docker-compose, and scripts.

## Tech Stack

- Ory Oathkeeper v0.40.7 — `oauth2_introspection` authenticator + `remote_json` authorizer
- `mccutchen/go-httpbin:latest` — lightweight httpbin upstream (confirmed: listens on 8080/tcp)
- Bash + curl + jq — client creation and token minting scripts (jq confirmed available)
- `scripts/seed.go` (updated) — parameterizable SpiceDB seeding via `ALLOWED_SUBJECT` env var, using `OPERATION_TOUCH` for idempotency

## Global Constraints

- Oathkeeper version: v0.40.7
- httpbin image: `mccutchen/go-httpbin:latest`, listens on port 8080
- Protected paths: `GET /demo/get` and `POST /demo/post` (matched via Oathkeeper regex `<(get|post)>`)
- Authenticator: `oauth2_introspection` pointing to `http://app:8080/oauth2/introspect`
- Authorizer: `remote_json` pointing to `http://app:8080/authorization/verify` with dynamic subject `"user:{{ print .Subject }}"`
- Hydra v2 sets `sub = client_id` for client_credentials tokens (verified)
- `ALLOWED_SUBJECT` env var controls which subject receives SpiceDB permission; default: `client-alice`
- Two Hydra clients: `client-alice`/`secret-alice` and `client-bob`/`secret-bob`; only `client-alice` granted permission
- All scripts use `curl` + `jq` only (no other CLI tools)
- `seed.go` relationship writes use `OPERATION_TOUCH` — safe to re-run

---

## Components

### 1. docker-compose.yml

Add `httpbin` service:

```yaml
httpbin:
  image: mccutchen/go-httpbin:latest
  ports:
    - "8081:8080"
```

Update `oathkeeper` depends_on to include `httpbin`.

### 2. configs/oathkeeper/config.yml

Enable `oauth2_introspection` authenticator globally:

```yaml
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
```

No changes to authorizers or mutators sections.

### 3. configs/oathkeeper/access-rules.yml

**Replace** the existing `demo-authorization` rule entirely with `demo-httpbin`. The old rule matched `http://localhost:4455/<.*>` — keeping it alongside a `/demo/<(get|post)>` rule causes Oathkeeper to error ("found more than one matching rule") for overlapping requests.

New sole rule:

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

`strip_path: /demo` causes Oathkeeper to forward `GET /demo/get` → `GET /get` on httpbin.

### 4. scripts/seed.go (updated)

Two changes:

**a) Parameterize subject via `ALLOWED_SUBJECT` env var** (default: `client-alice`):

```go
allowedSubject := "client-alice"
if v := os.Getenv("ALLOWED_SUBJECT"); v != "" {
    allowedSubject = v
}
```

**b) Switch `OPERATION_CREATE` → `OPERATION_TOUCH`** so re-runs are idempotent:

```go
Operation: v1.RelationshipUpdate_OPERATION_TOUCH,
```

`OPERATION_TOUCH` creates the relationship if absent or no-ops if it already exists — SpiceDB does not return an error on re-run.

### 5. scripts/create-clients.sh (new)

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

### 6. scripts/mint-token.sh (new)

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

Prints the raw access token to stdout; caller captures it with `$(...)`.

---

## Demo Playbook

```bash
# 1. Start the stack
docker compose up -d

# 2. Create Hydra clients
./scripts/create-clients.sh

# 3. Seed SpiceDB (OPERATION_TOUCH — safe to re-run)
ALLOWED_SUBJECT=client-alice go run scripts/seed.go

# 4. Mint tokens
TOKEN_ALICE=$(./scripts/mint-token.sh client-alice secret-alice)
TOKEN_BOB=$(./scripts/mint-token.sh client-bob secret-bob)

# 5. Demo — allowed (client-alice has SpiceDB permission)
curl -H "Authorization: Bearer $TOKEN_ALICE" http://localhost:4455/demo/get
# → 200 with httpbin JSON response

# 6. Demo — denied (client-bob has no SpiceDB permission)
curl -H "Authorization: Bearer $TOKEN_BOB" http://localhost:4455/demo/get
# → 403

# 7. Demo — unauthenticated
curl http://localhost:4455/demo/get
# → 401
```

---

## Error Responses

| Condition | Status | Source |
|-----------|--------|--------|
| Missing or invalid Bearer token | 401 | Oathkeeper (introspection → `active: false`) |
| Token active but subject not in SpiceDB | 403 | Oathkeeper (authorizer returns 403) |
| Token active and subject authorized | 200 | httpbin response |
| Hydra unreachable | 502 | Oathkeeper |

---

## Testing

No new Go code — no new unit tests.

Smoke tests are the three demo playbook calls (steps 5–7): 200 allowed, 403 denied, 401 unauthenticated. All three must pass before the conference.
