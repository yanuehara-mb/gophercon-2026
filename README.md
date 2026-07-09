# gophercon-2026

Implementação de referência apresentada na palestra **"Autenticação e Autorização em Go com Ory e SpiceDB"** na [GopherCon LatAm 2026](https://gopherconlatam.org).

O projeto demonstra como construir uma facade em Go (Gin) que integra **Ory Hydra** (OAuth2/OIDC) e **SpiceDB** (autorização baseada em relacionamentos), com **Ory Oathkeeper** protegendo rotas via introspection + policy enforcement.

## Arquitetura

```
Cliente
  → Bearer token → Oathkeeper :4455/demo/get (ou /demo/post)
      → [authn] oauth2_introspection → app:8080/oauth2/introspect → Hydra :4445
      → [authz] remote_json         → app:8080/authorization/verify → SpiceDB :50051
      → [proxy]                     → httpbin:8080/get (ou /post)
```

### Endpoints da facade (`app`, porta 8080)

| Método | Path | Descrição |
|--------|------|-----------|
| `POST` | `/oauth2/introspect` | Repassa para o Admin API do Hydra; compatível com RFC 7662 |
| `POST` | `/authorization/verify` | Verifica permissão no SpiceDB com cache Redis |
| `GET` | `/healthz` | Health check |

### Rotas protegidas via Oathkeeper (porta 4455)

| Método | Path | Upstream |
|--------|------|----------|
| `GET` | `/demo/get` | httpbin `/get` |
| `POST` | `/demo/post` | httpbin `/post` |

## Pré-requisitos

- Docker + Docker Compose
- Go 1.21+
- `curl` e `jq`

## Playbook completo (do zero)

```bash
# 1. Subir o stack
docker compose up -d

# 2. Criar os clientes OAuth2 no Hydra
#    (aguarda Hydra estar pronto automaticamente)
./scripts/create-clients.sh

# 3. Criar o schema e a permissão no SpiceDB
#    ALLOWED_SUBJECT controla qual client_id recebe acesso (padrão: client-alice)
ALLOWED_SUBJECT=client-alice go run scripts/seed.go

# 4. Mintar tokens
TOKEN_ALICE=$(./scripts/mint-token.sh client-alice secret-alice)
TOKEN_BOB=$(./scripts/mint-token.sh client-bob secret-bob)

# 5. Autorizado — client-alice tem permissão no SpiceDB
curl -H "Authorization: Bearer $TOKEN_ALICE" http://localhost:4455/demo/get
# → 200 com resposta JSON do httpbin

# 6. Negado — client-bob não tem permissão
curl -H "Authorization: Bearer $TOKEN_BOB" http://localhost:4455/demo/get
# → 403

# 7. Não autenticado
curl http://localhost:4455/demo/get
# → 401
```

## Scripts

| Script | Descrição |
|--------|-----------|
| `scripts/create-clients.sh` | Cria `client-alice` e `client-bob` no Hydra. Idempotente (409 = skip). |
| `scripts/mint-token.sh <client-id> <client-secret>` | Minta um token `client_credentials` e imprime na stdout. |
| `scripts/seed.go` | Escreve o schema e a relação no SpiceDB. `ALLOWED_SUBJECT` (padrão `client-alice`) controla o sujeito autorizado. Usa `OPERATION_TOUCH` — seguro para re-execução. |

### Variáveis de ambiente dos scripts

| Variável | Padrão | Descrição |
|----------|--------|-----------|
| `HYDRA_ADMIN` | `http://localhost:4445` | Admin API do Hydra |
| `HYDRA_PUBLIC` | `http://localhost:4444` | Public API do Hydra |
| `SPICEDB_ADDR` | `localhost:50051` | Endereço gRPC do SpiceDB |
| `SPICEDB_TOKEN` | `somerandomkeyhere` | Pre-shared key do SpiceDB |
| `ALLOWED_SUBJECT` | `client-alice` | Subject que recebe permissão `reader` em `document:readme` |

## Serviços

| Serviço | Porta | Descrição |
|---------|-------|-----------|
| `app` | 8080 | Facade Go/Gin |
| `hydra` | 4444 / 4445 | Ory Hydra (public / admin) |
| `spicedb` | 50051 | SpiceDB gRPC |
| `oathkeeper` | 4455 | Ory Oathkeeper proxy |
| `httpbin` | 8081 | Upstream de demonstração |
| `postgres` | — | Banco de dados (Hydra + SpiceDB) |
| `redis` | 6379 | Cache de autorização |

## Testes

```bash
go test ./...
```
