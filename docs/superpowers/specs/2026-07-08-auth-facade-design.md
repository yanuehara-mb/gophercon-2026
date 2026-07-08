# Auth Facade — Design Spec

**Date:** 2026-07-08  
**Context:** Implementação de referência para GopherCon 2026. Clean room do que é usado internamente. Demonstra integração de autenticação (Ory Hydra) e autorização (SpiceDB) numa API Go/Gin.

---

## Objetivo

Construir uma API Go/Gin que serve como fachada para dois sistemas distintos:

1. **Ory Hydra** — introspection de tokens OAuth2
2. **SpiceDB** — verificação de permissões (Zanzibar-style)

As duas integrações são tratadas como fachadas independentes, visualmente simétricas na estrutura do projeto, para evidenciar que são naturezas diferentes (HTTP vs gRPC).

---

## Estrutura de Pacotes

```
cmd/server/
  main.go                  # wiring: lê config, instancia clients, monta router

internal/
  config/
    config.go              # struct Config, leitura via os.Getenv

  hydra/
    client.go              # interface Introspector + implementação HTTP
    handler.go             # handler Gin: recebe token, delega ao Introspector

  spicedb/
    client.go              # interface Checker + implementação gRPC
    cache.go               # interface TokenCache + implementação Redis
    handler.go             # handler Gin: recebe subject/object/permission, delega ao Checker

docker-compose.yml
configs/
  hydra/                   # config mínima de bootstrap do Hydra
  oathkeeper/              # rules e config do Oathkeeper
```

Nenhum pacote interno importa outro pacote interno — apenas `config`. Isso mantém as duas fachadas completamente desacopladas.

---

## Configuração

Leitura exclusivamente via variáveis de ambiente. Sem biblioteca de config externa.

```go
type Config struct {
    Port         string // SERVER_PORT       — default "8080"
    HydraURL     string // HYDRA_URL         — ex: "http://hydra:4445"
    SpiceDBAddr  string // SPICEDB_ADDR      — ex: "spicedb:50051"
    SpiceDBToken string // SPICEDB_TOKEN
    RedisAddr    string // REDIS_ADDR        — ex: "redis:6379"
}
```

---

## Endpoints

### `POST /oauth2/introspect`

Fachada pass-through para o Ory Hydra.

- Aceita `token` como form field (conforme OAuth2 Token Introspection — RFC 7662)
- Repassa a requisição ao Hydra (`HYDRA_URL/admin/oauth2/introspect`) — path da Admin API do Hydra v2.x
- Devolve o status code e body JSON brutos do Hydra sem transformação
- Endpoint aberto — sem autenticação própria (confia no caller ser serviço interno)

### `POST /authorization/verify`

Fachada para verificação de permissões no SpiceDB. Projetada para ser chamada pelo Ory Oathkeeper como authorizer externo com payload configurado via template.

**Request body:**
```json
{
  "subject":    "user:alice",
  "object":     "document:readme",
  "permission": "read"
}
```

**Responses:**

| Situação | Status |
|---|---|
| Permissão concedida | `200 OK` |
| Permissão negada | `403 Forbidden` |
| Request inválido (campos faltando) | `400 Bad Request` |
| Falha de comunicação com SpiceDB | `502 Bad Gateway` |

O status code é o sinal de autorização — compatível com o authorizer `remote_json` do Oathkeeper.

### `GET /healthz`

Retorna `200 OK` com `{"status": "ok"}`. Usado pelo docker-compose e útil em demos ao vivo.

---

## Cliente Hydra

**Interface:**
```go
type Introspector interface {
    Introspect(ctx context.Context, token string) (*http.Response, error)
}
```

- Implementação usa `net/http` puro (sem biblioteca extra)
- `*http.Response` é retornado direto para o handler, que é responsável por: fechar o body (`defer resp.Body.Close()`), copiar o `Content-Type`, e copiar o status code para o response Gin
- Projeto usa Hydra v2.x. Path de introspection: `/admin/oauth2/introspect` na porta 4445 (Admin API). Fixar a image tag do Hydra no docker-compose.

---

## Cliente SpiceDB

**Interface:**
```go
type Checker interface {
    Check(ctx context.Context, subject, object, permission string) (bool, error)
}
```

- Implementação usa o SDK oficial `authzed-go` via gRPC
- Injetado no handler via construtor

### Cache de ZedToken (Redis)

O SpiceDB retorna um ZedToken em cada resposta de Check. Esse token representa o estado mais recente do banco e é usado para garantir consistência nas verificações subsequentes.

**Interface:**
```go
type TokenCache interface {
    Get(ctx context.Context) (string, error)
    Set(ctx context.Context, token string) error
}
```

**Chave Redis:** `"spicedb:zedtoken"` — chave global única, sempre sobrescrita com o token mais recente (sem TTL).

**Fluxo no Check:**
1. Busca ZedToken no Redis
2. Se existe: envia Check com consistência `AtLeastAsFresh(token)`
3. Se não existe: envia Check com `FullyConsistent`
4. Armazena o ZedToken retornado no Redis

**Degradação graciosa:** se o Redis estiver inacessível, a app faz fallback para `FullyConsistent` sem retornar erro ao caller. Redis é otimização de consistência, não dependência crítica.

**Limite do modelo de consistência:** o token em cache é capturado de respostas de Check — não de escritas de relacionamento. Portanto, a consistência garantida é *monotônica entre checks* (cada check é pelo menos tão fresco quanto o anterior), e não proteção contra o "new enemy problem" (que requer captura do token no momento da escrita, por objeto). Para um demo de referência, esse nível é adequado. O wording da apresentação deve refletir isso.

---

## Injeção de Dependência

Toda a composição acontece em `main.go`. Sem globals, sem singletons, sem framework de DI.

```
Config → HydraClient      → HydraHandler    ┐
Config → RedisClient      → TokenCache  ┐   │
Config → SpiceDBClient ───────────────── ┤ → Gin router → HTTP server
                          SpiceDBHandler ┘
```

---

## Docker Compose

| Serviço | Porta exposta | Observação |
|---|---|---|
| `app` | `8080` | a fachada |
| `hydra` | `4444`, `4445` | public + admin API |
| `spicedb` | `50051` | gRPC |
| `redis` | `6379` | cache de ZedToken |
| `oathkeeper` | `4455` | proxy reverso |

O repositório inclui configs de bootstrap para rodar o demo end-to-end. O plano de implementação deve tratar cada um destes como passo explícito:

- **SpiceDB:** schema definindo os tipos (`user`, `document`) e a relação `read`; seed de relacionamento `user:alice → document:readme` para o demo funcionar
- **Hydra:** configuração de um OAuth2 client + script para mintar um token real, para que o `/oauth2/introspect` tenha algo concreto a introspectar em demo ao vivo
- **Oathkeeper:** access rule com authorizer `remote_json` apontando para `app:8080/authorization/verify`, incluindo o payload template que emite `{subject, object, permission}`

---

## Error Handling

| Origem | Condição | Status HTTP |
|---|---|---|
| Handler | Body inválido / campos faltando | `400` |
| Hydra | Serviço inacessível | `502` |
| SpiceDB | Serviço inacessível | `502` |
| Redis | Inacessível | fallback silencioso (sem falha) |

---

## Testes

A injeção de dependência explícita viabiliza testes unitários sem dependências externas.

- **Handler Hydra:** mockar `Introspector`; testar que status code e body do Hydra são repassados sem alteração; testar `502` quando o client retorna erro
- **Handler SpiceDB:** mockar `Checker`; testar mapeamento `true → 200`, `false → 403`, erro → `502`, body inválido → `400`; testar fallback de Redis inacessível (`FullyConsistent` é usado, sem erro ao caller)
- **Cliente Redis (TokenCache):** testar `Get` com cache vazio vs. token presente; testar `Set` sobrescreve valor anterior
- Testes são table-driven seguindo o padrão Go idiomático

---

## Fora de Escopo

- Banco de dados próprio
- Autenticação no endpoint `/oauth2/introspect`
- Transformação/filtragem da resposta do Hydra
- Middleware de rate limiting ou logging estruturado (podem ser adicionados depois)
