# Auth Facade Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Construir uma API Go/Gin que faz fachada para Ory Hydra (introspection OAuth2, HTTP pass-through) e SpiceDB (verificação de permissões, gRPC com cache Redis de ZedToken).

**Architecture:** Duas fachadas independentes em pacotes internos separados (`hydra/`, `spicedb/`), sem dependência cruzada entre si. Injeção de dependência explícita via construtores — toda composição acontece em `main.go`. Hydra: HTTP pass-through puro. SpiceDB: gRPC com `TokenCache` Redis para consistência monotônica entre checks.

**Tech Stack:** Go 1.23+, Gin v1, authzed-go v1 (gRPC), go-redis/v9, miniredis/v2 (testes), Docker Compose

## Global Constraints

- Module path: `github.com/yanuehara/gophercon-2026`
- Hydra v2.x — path de introspection: `/admin/oauth2/introspect` (porta 4445 — Admin API)
- SpiceDB gRPC sem TLS (`insecure.NewCredentials()`) para dev/demo
- Sem globals, sem singletons — toda dependência injetada via construtor
- Testes são table-driven usando apenas stdlib `testing` (sem testify)
- ZedToken cache key: `"spicedb:zedtoken"` — sem TTL, sempre sobrescrita com token mais recente
- Nenhum pacote interno importa outro pacote interno — apenas `internal/config`

---

### Task 1: Project Scaffold + Config

**Files:**
- Create: `go.mod`
- Create: `internal/config/config.go`
- Create: `internal/config/config_test.go`
- Create: `cmd/server/main.go` (skeleton)

**Interfaces:**
- Produces: `config.Config{Port, HydraURL, SpiceDBAddr, SpiceDBToken, RedisAddr string}`; `config.Load() Config`

- [ ] **Step 1: Inicializar módulo Go e criar estrutura de diretórios**

```bash
cd /home/yanuehara/projetos/gophercon-2026
go mod init github.com/yanuehara/gophercon-2026
mkdir -p cmd/server internal/config internal/hydra internal/spicedb scripts configs/oathkeeper configs/spicedb
```

Expected: `go.mod` criado com `module github.com/yanuehara/gophercon-2026`.

- [ ] **Step 2: Escrever o teste que falha para config**

Criar `internal/config/config_test.go`:

```go
package config

import (
	"os"
	"testing"
)

func TestLoad_Defaults(t *testing.T) {
	os.Unsetenv("SERVER_PORT")
	os.Unsetenv("HYDRA_URL")
	os.Unsetenv("SPICEDB_ADDR")
	os.Unsetenv("SPICEDB_TOKEN")
	os.Unsetenv("REDIS_ADDR")

	cfg := Load()

	tests := []struct {
		name string
		got  string
		want string
	}{
		{"Port", cfg.Port, "8080"},
		{"HydraURL", cfg.HydraURL, "http://hydra:4445"},
		{"SpiceDBAddr", cfg.SpiceDBAddr, "spicedb:50051"},
		{"SpiceDBToken", cfg.SpiceDBToken, ""},
		{"RedisAddr", cfg.RedisAddr, "redis:6379"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Errorf("got %q, want %q", tt.got, tt.want)
			}
		})
	}
}

func TestLoad_EnvOverride(t *testing.T) {
	t.Setenv("SERVER_PORT", "9090")
	t.Setenv("HYDRA_URL", "http://localhost:4445")

	cfg := Load()

	if cfg.Port != "9090" {
		t.Errorf("Port: got %q, want %q", cfg.Port, "9090")
	}
	if cfg.HydraURL != "http://localhost:4445" {
		t.Errorf("HydraURL: got %q, want %q", cfg.HydraURL, "http://localhost:4445")
	}
}
```

- [ ] **Step 3: Rodar para verificar que falha**

```bash
go test ./internal/config/...
```

Expected: FAIL — `Load` undefined.

- [ ] **Step 4: Implementar config**

Criar `internal/config/config.go`:

```go
package config

import "os"

type Config struct {
	Port         string
	HydraURL     string
	SpiceDBAddr  string
	SpiceDBToken string
	RedisAddr    string
}

func Load() Config {
	return Config{
		Port:         getEnv("SERVER_PORT", "8080"),
		HydraURL:     getEnv("HYDRA_URL", "http://hydra:4445"),
		SpiceDBAddr:  getEnv("SPICEDB_ADDR", "spicedb:50051"),
		SpiceDBToken: getEnv("SPICEDB_TOKEN", ""),
		RedisAddr:    getEnv("REDIS_ADDR", "redis:6379"),
	}
}

func getEnv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
```

Criar `cmd/server/main.go` (skeleton):

```go
package main

func main() {}
```

- [ ] **Step 5: Rodar para verificar que passa**

```bash
go test ./internal/config/...
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git init
git add go.mod cmd/ internal/
git commit -m "feat: project scaffold and config package"
```

---

### Task 2: Hydra Client + Handler

**Files:**
- Create: `internal/hydra/client.go`
- Create: `internal/hydra/client_test.go`
- Create: `internal/hydra/handler.go`
- Create: `internal/hydra/handler_test.go`

**Interfaces:**
- Consumes: nada de tasks anteriores (standalone)
- Produces:
  - `hydra.Introspector` interface: `Introspect(ctx context.Context, token string) (*http.Response, error)`
  - `hydra.NewClient(hydraURL string) *Client` — implementa `Introspector`; faz POST para `hydraURL + "/admin/oauth2/introspect"` com `token` como form field
  - `hydra.NewHandler(i Introspector) *Handler`
  - `(*hydra.Handler).Introspect(c *gin.Context)` — handler POST; fecha o body do upstream, copia `Content-Type` e status code brutos para o response

- [ ] **Step 1: Adicionar dependência Gin**

```bash
go get github.com/gin-gonic/gin
```

- [ ] **Step 2: Escrever os testes que falham para o handler**

Criar `internal/hydra/handler_test.go`:

```go
package hydra

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

type mockIntrospector struct {
	resp *http.Response
	err  error
}

func (m *mockIntrospector) Introspect(_ context.Context, _ string) (*http.Response, error) {
	return m.resp, m.err
}

func setupHydraRouter(i Introspector) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	h := NewHandler(i)
	r.POST("/oauth2/introspect", h.Introspect)
	return r
}

func TestHandler_Introspect(t *testing.T) {
	tests := []struct {
		name       string
		formBody   string
		mockResp   *http.Response
		mockErr    error
		wantStatus int
		wantBody   string
	}{
		{
			name:     "passes through hydra 200 response",
			formBody: "token=mytoken",
			mockResp: &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": {"application/json"}},
				Body:       io.NopCloser(strings.NewReader(`{"active":true}`)),
			},
			wantStatus: http.StatusOK,
			wantBody:   `{"active":true}`,
		},
		{
			name:     "passes through hydra 401 response",
			formBody: "token=badtoken",
			mockResp: &http.Response{
				StatusCode: http.StatusUnauthorized,
				Header:     http.Header{"Content-Type": {"application/json"}},
				Body:       io.NopCloser(strings.NewReader(`{"active":false}`)),
			},
			wantStatus: http.StatusUnauthorized,
			wantBody:   `{"active":false}`,
		},
		{
			name:       "missing token returns 400",
			formBody:   "",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "upstream error returns 502",
			formBody:   "token=mytoken",
			mockErr:    errors.New("connection refused"),
			wantStatus: http.StatusBadGateway,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := setupHydraRouter(&mockIntrospector{resp: tt.mockResp, err: tt.mockErr})

			req := httptest.NewRequest(http.MethodPost, "/oauth2/introspect",
				strings.NewReader(tt.formBody))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			if w.Code != tt.wantStatus {
				t.Errorf("status: got %d, want %d", w.Code, tt.wantStatus)
			}
			if tt.wantBody != "" && w.Body.String() != tt.wantBody {
				t.Errorf("body: got %q, want %q", w.Body.String(), tt.wantBody)
			}
		})
	}
}
```

- [ ] **Step 3: Rodar para verificar que falha**

```bash
go test ./internal/hydra/...
```

Expected: FAIL — `NewHandler`, `Introspector` undefined.

- [ ] **Step 4: Implementar o client**

Criar `internal/hydra/client.go`:

```go
package hydra

import (
	"context"
	"net/http"
	"net/url"
	"strings"
)

type Introspector interface {
	Introspect(ctx context.Context, token string) (*http.Response, error)
}

type Client struct {
	hydraURL   string
	httpClient *http.Client
}

func NewClient(hydraURL string) *Client {
	return &Client{
		hydraURL:   hydraURL,
		httpClient: &http.Client{},
	}
}

func (c *Client) Introspect(ctx context.Context, token string) (*http.Response, error) {
	form := url.Values{"token": {token}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.hydraURL+"/admin/oauth2/introspect",
		strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return c.httpClient.Do(req)
}
```

- [ ] **Step 5: Implementar o handler**

Criar `internal/hydra/handler.go`:

```go
package hydra

import (
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
)

type Handler struct {
	introspector Introspector
}

func NewHandler(i Introspector) *Handler {
	return &Handler{introspector: i}
}

func (h *Handler) Introspect(c *gin.Context) {
	token := c.PostForm("token")
	if token == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "token is required"})
		return
	}

	resp, err := h.introspector.Introspect(c.Request.Context(), token)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "upstream error"})
		return
	}
	defer resp.Body.Close()

	c.Header("Content-Type", resp.Header.Get("Content-Type"))
	c.Writer.WriteHeader(resp.StatusCode)
	io.Copy(c.Writer, resp.Body)
}
```

- [ ] **Step 6: Rodar para verificar que passa**

```bash
go test ./internal/hydra/...
```

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/hydra/ go.mod go.sum
git commit -m "feat: hydra introspect facade with pass-through handler"
```

---

### Task 3: SpiceDB TokenCache (Redis)

**Files:**
- Create: `internal/spicedb/cache.go`
- Create: `internal/spicedb/cache_test.go`

**Interfaces:**
- Consumes: nada
- Produces:
  - `spicedb.TokenCache` interface: `Get(ctx context.Context) (string, error)`; `Set(ctx context.Context, token string) error`
  - `spicedb.NewTokenCache(addr string) *redisCache` — implementa `TokenCache`; retorna `("", nil)` em cache miss; sem TTL no Set

- [ ] **Step 1: Adicionar dependências**

```bash
go get github.com/redis/go-redis/v9
go get github.com/alicebob/miniredis/v2
```

- [ ] **Step 2: Escrever os testes que falham**

Criar `internal/spicedb/cache_test.go`:

```go
package spicedb

import (
	"context"
	"testing"

	"github.com/alicebob/miniredis/v2"
)

func TestTokenCache(t *testing.T) {
	mr := miniredis.RunT(t)
	cache := NewTokenCache(mr.Addr())
	ctx := context.Background()

	t.Run("Get returns empty string on cache miss", func(t *testing.T) {
		token, err := cache.Get(ctx)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if token != "" {
			t.Errorf("got %q, want empty string", token)
		}
	})

	t.Run("Set then Get returns stored token", func(t *testing.T) {
		if err := cache.Set(ctx, "zedtoken-abc"); err != nil {
			t.Fatalf("Set failed: %v", err)
		}
		token, err := cache.Get(ctx)
		if err != nil {
			t.Fatalf("Get failed: %v", err)
		}
		if token != "zedtoken-abc" {
			t.Errorf("got %q, want %q", token, "zedtoken-abc")
		}
	})

	t.Run("Set overwrites existing token", func(t *testing.T) {
		if err := cache.Set(ctx, "zedtoken-newer"); err != nil {
			t.Fatalf("Set failed: %v", err)
		}
		token, err := cache.Get(ctx)
		if err != nil {
			t.Fatalf("Get failed: %v", err)
		}
		if token != "zedtoken-newer" {
			t.Errorf("got %q, want %q", token, "zedtoken-newer")
		}
	})
}
```

- [ ] **Step 3: Rodar para verificar que falha**

```bash
go test ./internal/spicedb/...
```

Expected: FAIL — `NewTokenCache` undefined.

- [ ] **Step 4: Implementar o cache**

Criar `internal/spicedb/cache.go`:

```go
package spicedb

import (
	"context"

	"github.com/redis/go-redis/v9"
)

const zedTokenKey = "spicedb:zedtoken"

type TokenCache interface {
	Get(ctx context.Context) (string, error)
	Set(ctx context.Context, token string) error
}

type redisCache struct {
	client *redis.Client
}

func NewTokenCache(addr string) *redisCache {
	return &redisCache{
		client: redis.NewClient(&redis.Options{Addr: addr}),
	}
}

func (c *redisCache) Get(ctx context.Context) (string, error) {
	val, err := c.client.Get(ctx, zedTokenKey).Result()
	if err == redis.Nil {
		return "", nil
	}
	return val, err
}

func (c *redisCache) Set(ctx context.Context, token string) error {
	return c.client.Set(ctx, zedTokenKey, token, 0).Err()
}
```

- [ ] **Step 5: Rodar para verificar que passa**

```bash
go test ./internal/spicedb/...
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/spicedb/cache.go internal/spicedb/cache_test.go go.mod go.sum
git commit -m "feat: spicedb zedtoken cache backed by redis"
```

---

### Task 4: SpiceDB Client + Handler

**Files:**
- Create: `internal/spicedb/client.go`
- Create: `internal/spicedb/client_test.go`
- Create: `internal/spicedb/handler.go`
- Create: `internal/spicedb/handler_test.go`

**Interfaces:**
- Consumes: `spicedb.TokenCache` (Task 3)
- Produces:
  - `spicedb.Checker` interface: `Check(ctx context.Context, subject, object, permission string) (bool, error)`
  - `spicedb.NewClient(addr, token string, cache TokenCache) (*Client, error)` — implementa `Checker`; usa `AtLeastAsFresh` se cache tiver token, `FullyConsistent` caso contrário; atualiza cache com `CheckedAt` do response; falha no cache → fallback silencioso para `FullyConsistent`
  - `spicedb.NewHandler(c Checker) *Handler`
  - `(*spicedb.Handler).Verify(c *gin.Context)` — handler POST `/authorization/verify`; retorna 200 (allowed), 403 (denied), 400 (body inválido), 502 (erro SpiceDB)

- [ ] **Step 1: Adicionar dependências authzed-go**

```bash
go get github.com/authzed/authzed-go/v1
go get github.com/authzed/grpcutil
```

- [ ] **Step 2: Escrever testes que falham para parseRef e selectConsistency**

Criar `internal/spicedb/client_test.go`:

```go
package spicedb

import (
	"context"
	"errors"
	"testing"

	v1 "github.com/authzed/authzed-go/proto/authzed/api/v1"
)

func TestParseRef(t *testing.T) {
	tests := []struct {
		input    string
		wantType string
		wantID   string
		wantErr  bool
	}{
		{"user:alice", "user", "alice", false},
		{"document:readme", "document", "readme", false},
		{"invalid", "", "", true},
		{"", "", "", true},
		{":notype", "", "", true},
		{"noid:", "", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			gotType, gotID, err := parseRef(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if gotType != tt.wantType || gotID != tt.wantID {
				t.Errorf("got (%q, %q), want (%q, %q)", gotType, gotID, tt.wantType, tt.wantID)
			}
		})
	}
}

// stubCache implements TokenCache for testing selectConsistency without Redis.
type stubCache struct {
	token string
	err   error
}

func (s *stubCache) Get(_ context.Context) (string, error) { return s.token, s.err }
func (s *stubCache) Set(_ context.Context, _ string) error  { return nil }

func TestSelectConsistency(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name      string
		cache     TokenCache
		wantType  string // "fully" or "fresh"
		wantToken string // only checked when wantType == "fresh"
	}{
		{
			name:     "no cached token uses FullyConsistent",
			cache:    &stubCache{token: ""},
			wantType: "fully",
		},
		{
			name:      "cached token uses AtLeastAsFresh",
			cache:     &stubCache{token: "zed-abc"},
			wantType:  "fresh",
			wantToken: "zed-abc",
		},
		{
			name:     "cache error falls back to FullyConsistent",
			cache:    &stubCache{err: errors.New("redis down")},
			wantType: "fully",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := selectConsistency(ctx, tt.cache)
			switch tt.wantType {
			case "fully":
				if _, ok := c.Requirement.(*v1.Consistency_FullyConsistent); !ok {
					t.Errorf("expected FullyConsistent, got %T", c.Requirement)
				}
			case "fresh":
				fresh, ok := c.Requirement.(*v1.Consistency_AtLeastAsFresh)
				if !ok {
					t.Errorf("expected AtLeastAsFresh, got %T", c.Requirement)
					return
				}
				if fresh.AtLeastAsFresh.Token != tt.wantToken {
					t.Errorf("token: got %q, want %q", fresh.AtLeastAsFresh.Token, tt.wantToken)
				}
			}
		})
	}
}
```

- [ ] **Step 3: Escrever testes que falham para o handler**

Criar `internal/spicedb/handler_test.go`:

```go
package spicedb

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

type mockChecker struct {
	allowed bool
	err     error
}

func (m *mockChecker) Check(_ context.Context, _, _, _ string) (bool, error) {
	return m.allowed, m.err
}

func setupSpiceDBRouter(c Checker) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	h := NewHandler(c)
	r.POST("/authorization/verify", h.Verify)
	return r
}

func TestHandler_Verify(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		allowed    bool
		checkErr   error
		wantStatus int
	}{
		{
			name:       "allowed returns 200",
			body:       `{"subject":"user:alice","object":"document:readme","permission":"read"}`,
			allowed:    true,
			wantStatus: http.StatusOK,
		},
		{
			name:       "denied returns 403",
			body:       `{"subject":"user:alice","object":"document:readme","permission":"read"}`,
			allowed:    false,
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "missing subject returns 400",
			body:       `{"object":"document:readme","permission":"read"}`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "missing object returns 400",
			body:       `{"subject":"user:alice","permission":"read"}`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "missing permission returns 400",
			body:       `{"subject":"user:alice","object":"document:readme"}`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "spicedb error returns 502",
			body:       `{"subject":"user:alice","object":"document:readme","permission":"read"}`,
			checkErr:   errors.New("connection refused"),
			wantStatus: http.StatusBadGateway,
		},
		{
			name:       "malformed subject ref returns 400",
			body:       `{"subject":"alice","object":"document:readme","permission":"read"}`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "malformed object ref returns 400",
			body:       `{"subject":"user:alice","object":"readme","permission":"read"}`,
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := setupSpiceDBRouter(&mockChecker{allowed: tt.allowed, err: tt.checkErr})

			req := httptest.NewRequest(http.MethodPost, "/authorization/verify",
				strings.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			if w.Code != tt.wantStatus {
				t.Errorf("status: got %d, want %d", w.Code, tt.wantStatus)
			}
		})
	}
}
```

- [ ] **Step 4: Rodar para verificar que falha**

```bash
go test ./internal/spicedb/...
```

Expected: FAIL — `NewHandler`, `Checker`, `parseRef` undefined.

- [ ] **Step 5: Implementar o client**

Criar `internal/spicedb/client.go`:

```go
package spicedb

import (
	"context"
	"fmt"
	"strings"

	authzed "github.com/authzed/authzed-go/v1"
	v1 "github.com/authzed/authzed-go/proto/authzed/api/v1"
	"github.com/authzed/grpcutil"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type Checker interface {
	Check(ctx context.Context, subject, object, permission string) (bool, error)
}

type Client struct {
	authzed *authzed.Client
	cache   TokenCache
}

func NewClient(addr, token string, cache TokenCache) (*Client, error) {
	c, err := authzed.NewClient(addr,
		grpcutil.WithInsecureBearerToken(token),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return nil, err
	}
	return &Client{authzed: c, cache: cache}, nil
}

func (c *Client) Check(ctx context.Context, subject, object, permission string) (bool, error) {
	subjectType, subjectID, err := parseRef(subject)
	if err != nil {
		return false, fmt.Errorf("invalid subject: %w", err)
	}
	objectType, objectID, err := parseRef(object)
	if err != nil {
		return false, fmt.Errorf("invalid object: %w", err)
	}

	resp, err := c.authzed.CheckPermission(ctx, &v1.CheckPermissionRequest{
		Consistency: selectConsistency(ctx, c.cache),
		Resource: &v1.ObjectReference{
			ObjectType: objectType,
			ObjectId:   objectID,
		},
		Permission: permission,
		Subject: &v1.SubjectReference{
			Object: &v1.ObjectReference{
				ObjectType: subjectType,
				ObjectId:   subjectID,
			},
		},
	})
	if err != nil {
		return false, err
	}

	if resp.CheckedAt != nil {
		_ = c.cache.Set(ctx, resp.CheckedAt.Token)
	}

	return resp.Permissionship == v1.CheckPermissionResponse_PERMISSIONSHIP_HAS_PERMISSION, nil
}

// selectConsistency returns AtLeastAsFresh if a cached ZedToken exists, FullyConsistent otherwise.
// Cache errors are silently ignored — FullyConsistent is a safe fallback.
func selectConsistency(ctx context.Context, cache TokenCache) *v1.Consistency {
	if token, err := cache.Get(ctx); err == nil && token != "" {
		return &v1.Consistency{
			Requirement: &v1.Consistency_AtLeastAsFresh{
				AtLeastAsFresh: &v1.ZedToken{Token: token},
			},
		}
	}
	return &v1.Consistency{
		Requirement: &v1.Consistency_FullyConsistent{FullyConsistent: true},
	}
}

func parseRef(s string) (objectType, objectID string, err error) {
	parts := strings.SplitN(s, ":", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("invalid reference format %q, expected type:id", s)
	}
	return parts[0], parts[1], nil
}
```

- [ ] **Step 6: Implementar o handler**

Criar `internal/spicedb/handler.go`:

```go
package spicedb

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

type Handler struct {
	checker Checker
}

func NewHandler(c Checker) *Handler {
	return &Handler{checker: c}
}

type verifyRequest struct {
	Subject    string `json:"subject"    binding:"required"`
	Object     string `json:"object"     binding:"required"`
	Permission string `json:"permission" binding:"required"`
}

func (h *Handler) Verify(c *gin.Context) {
	var req verifyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if _, _, err := parseRef(req.Subject); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid subject: " + err.Error()})
		return
	}
	if _, _, err := parseRef(req.Object); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid object: " + err.Error()})
		return
	}

	allowed, err := h.checker.Check(c.Request.Context(), req.Subject, req.Object, req.Permission)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "upstream error"})
		return
	}

	if allowed {
		c.Status(http.StatusOK)
	} else {
		c.Status(http.StatusForbidden)
	}
}
```

- [ ] **Step 7: Rodar para verificar que passa**

```bash
go test ./internal/spicedb/...
```

Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add internal/spicedb/ go.mod go.sum
git commit -m "feat: spicedb checker facade with zedtoken cache and oathkeeper-compatible handler"
```

---

### Task 5: main.go Wiring + Healthz

**Files:**
- Modify: `cmd/server/main.go`

**Interfaces:**
- Consumes:
  - `config.Load() Config` com campos `Port`, `HydraURL`, `SpiceDBAddr`, `SpiceDBToken`, `RedisAddr` (Task 1)
  - `hydra.NewClient(hydraURL string) *Client` (Task 2)
  - `hydra.NewHandler(i Introspector) *Handler`; `(*hydra.Handler).Introspect` (Task 2)
  - `spicedb.NewTokenCache(addr string) *redisCache` (Task 3)
  - `spicedb.NewClient(addr, token string, cache TokenCache) (*Client, error)` (Task 4)
  - `spicedb.NewHandler(c Checker) *Handler`; `(*spicedb.Handler).Verify` (Task 4)
- Produces: servidor HTTP na porta `cfg.Port` com rotas POST `/oauth2/introspect`, POST `/authorization/verify`, GET `/healthz`

- [ ] **Step 1: Implementar main.go**

Substituir o skeleton em `cmd/server/main.go`:

```go
package main

import (
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/yanuehara/gophercon-2026/internal/config"
	"github.com/yanuehara/gophercon-2026/internal/hydra"
	"github.com/yanuehara/gophercon-2026/internal/spicedb"
)

func main() {
	cfg := config.Load()

	hydraClient := hydra.NewClient(cfg.HydraURL)
	hydraHandler := hydra.NewHandler(hydraClient)

	tokenCache := spicedb.NewTokenCache(cfg.RedisAddr)
	spicedbClient, err := spicedb.NewClient(cfg.SpiceDBAddr, cfg.SpiceDBToken, tokenCache)
	if err != nil {
		log.Fatalf("failed to connect to spicedb: %v", err)
	}
	spicedbHandler := spicedb.NewHandler(spicedbClient)

	r := gin.Default()
	r.POST("/oauth2/introspect", hydraHandler.Introspect)
	r.POST("/authorization/verify", spicedbHandler.Verify)
	r.GET("/healthz", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	if err := r.Run(":" + cfg.Port); err != nil {
		log.Fatalf("server failed: %v", err)
	}
}
```

- [ ] **Step 2: Verificar que compila**

```bash
go build ./...
```

Expected: sem erros.

- [ ] **Step 3: Rodar todos os testes**

```bash
go test ./...
```

Expected: todos PASS.

- [ ] **Step 4: Commit**

```bash
git add cmd/server/main.go
git commit -m "feat: wire all components in main.go with gin router and healthz"
```

---

### Task 6: Docker Compose + Configs de Bootstrap

**Files:**
- Create: `Dockerfile`
- Create: `docker-compose.yml`
- Create: `configs/postgres/init.sql`
- Create: `configs/oathkeeper/config.yml`
- Create: `configs/oathkeeper/access-rules.yml`
- Create: `configs/spicedb/schema.zed`
- Create: `scripts/seed-spicedb.sh`
- Create: `scripts/mint-hydra-token.sh`

**Interfaces:**
- Produces: `docker compose up` inicia todos os serviços; jobs `hydra-migrate` e `spicedb-migrate` rodam e saem antes de seus respectivos serviços subirem; `scripts/seed-spicedb.sh` escreve schema e relacionamento no SpiceDB; `scripts/mint-hydra-token.sh` cria client OAuth2 e minta um token válido para demo

- [ ] **Step 1: Criar Dockerfile**

```dockerfile
FROM golang:1.23-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -o server ./cmd/server

FROM alpine:3.20
COPY --from=builder /app/server /server
EXPOSE 8080
ENTRYPOINT ["/server"]
```

- [ ] **Step 2: Criar configs/postgres/init.sql**

```sql
CREATE DATABASE hydra;
CREATE DATABASE spicedb;
```

O PostgreSQL executa automaticamente os arquivos em `/docker-entrypoint-initdb.d/` na primeira inicialização.

- [ ] **Step 3: Criar docker-compose.yml**

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
      --grpc-no-tls
      --http-no-tls
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

  oathkeeper:
    image: oryd/oathkeeper:v0.40.7
    command: serve --config /etc/oathkeeper/config.yml
    volumes:
      - ./configs/oathkeeper:/etc/oathkeeper
    ports:
      - "4455:4455"
    depends_on:
      - app
```

Ordem de boot resultante:
1. `postgres` (aguarda healthcheck)
2. `hydra-migrate` e `spicedb-migrate` em paralelo (aguardam postgres; saem com código 0)
3. `hydra` e `spicedb` (aguardam seus respectivos migrate com `service_completed_successfully`)
4. `redis` (paralelo)
5. `app` (aguarda hydra healthy + spicedb started + redis healthy)
6. `oathkeeper` (aguarda app)

- [ ] **Step 4: Criar config do Oathkeeper**

Criar `configs/oathkeeper/config.yml`:

```yaml
serve:
  proxy:
    port: 4455

log:
  level: warning

access_rules:
  repositories:
    - file:///etc/oathkeeper/access-rules.yml

errors:
  handlers:
    json:
      enabled: true
```

Criar `configs/oathkeeper/access-rules.yml`:

```yaml
- id: demo-authorization
  upstream:
    url: http://app:8080
  match:
    url: http://oathkeeper:4455/<**>
    methods:
      - GET
      - POST
      - PUT
      - DELETE
  authenticators:
    - handler: noop
  authorizer:
    handler: remote_json
    config:
      remote: http://app:8080/authorization/verify
      payload: |
        {
          "subject": "user:alice",
          "object": "document:readme",
          "permission": "read"
        }
  mutators:
    - handler: noop
```

Nota: o payload usa valores estáticos para o demo. Em produção, substituir por variáveis da sessão Oathkeeper como `{{ print .Subject.ID }}`.

- [ ] **Step 5: Criar schema do SpiceDB**

Criar `configs/spicedb/schema.zed`:

```
definition user {}

definition document {
  relation reader: user
  permission read = reader
}
```

- [ ] **Step 6: Criar script de seed do SpiceDB**

Criar `scripts/seed-spicedb.sh`:

```bash
#!/bin/bash
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
```

```bash
chmod +x scripts/seed-spicedb.sh
```

Requer `zed` CLI: `go install github.com/authzed/zed@latest`

- [ ] **Step 7: Criar script de mint de token Hydra**

Criar `scripts/mint-hydra-token.sh`:

```bash
#!/bin/bash
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
```

```bash
chmod +x scripts/mint-hydra-token.sh
```

Requer `hydra` CLI: `go install github.com/ory/hydra/v2@latest`

- [ ] **Step 8: Build da imagem**

```bash
docker compose build app
```

Expected: imagem construída sem erros.

- [ ] **Step 9: Commit**

```bash
git add Dockerfile docker-compose.yml configs/ scripts/
git commit -m "feat: docker-compose with postgres, hydra, spicedb, redis, oathkeeper and bootstrap scripts"
```

---

### Task 7: Smoke Test End-to-End

**Files:**
- Nenhum arquivo novo — valida o sistema completo rodando

- [ ] **Step 1: Subir todos os serviços**

```bash
docker compose up -d
docker compose ps
```

Expected: `hydra-migrate` e `spicedb-migrate` aparecem como `Exited (0)`; demais serviços como `running`. Aguardar ~30s para Hydra ficar healthy (PostgreSQL precisa estar pronto antes das migrations, que precisam estar prontas antes do serve).

- [ ] **Step 2: Seed do SpiceDB**

```bash
SPICEDB_TOKEN=somerandomkeyhere ./scripts/seed-spicedb.sh
```

Expected: `SpiceDB seeded successfully.`

- [ ] **Step 3: Testar healthz**

```bash
curl -s http://localhost:8080/healthz
```

Expected: `{"status":"ok"}`

- [ ] **Step 4: Mintar token Hydra**

```bash
./scripts/mint-hydra-token.sh
```

Copiar o valor de `access_token` do output.

- [ ] **Step 5: Testar introspect**

```bash
curl -s -X POST http://localhost:8080/oauth2/introspect \
  -d "token=<access_token_do_passo_4>"
```

Expected: JSON do Hydra com `"active": true`.

- [ ] **Step 6: Testar verify — autorizado**

```bash
curl -s -o /dev/null -w "%{http_code}" \
  -X POST http://localhost:8080/authorization/verify \
  -H "Content-Type: application/json" \
  -d '{"subject":"user:alice","object":"document:readme","permission":"read"}'
```

Expected: `200`

- [ ] **Step 7: Testar verify — negado**

```bash
curl -s -o /dev/null -w "%{http_code}" \
  -X POST http://localhost:8080/authorization/verify \
  -H "Content-Type: application/json" \
  -d '{"subject":"user:bob","object":"document:readme","permission":"read"}'
```

Expected: `403` (user:bob não tem relacionamento)

- [ ] **Step 8: Testar via Oathkeeper**

```bash
curl -s -o /dev/null -w "%{http_code}" \
  -H "Host: oathkeeper" \
  http://localhost:4455/healthz
```

Expected: `200` (Oathkeeper faz proxy para app; user:alice é autorizado pelo SpiceDB)

Nota: o `-H "Host: oathkeeper"` é necessário porque a access rule faz match em `http://oathkeeper:4455/<**>` — o Oathkeeper compara o host da requisição, e `localhost` não corresponderia à regra.

- [ ] **Step 9: Commit final**

```bash
git add .
git commit -m "chore: all smoke tests passing end-to-end"
```
