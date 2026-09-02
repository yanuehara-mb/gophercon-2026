package hydra

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

// mockHydra stands in for both upstream calls and records what Token was
// handed, so the passthrough contract can be asserted.
type mockHydra struct {
	resp *http.Response
	err  error

	gotForm url.Values
	gotAuth string
}

func (m *mockHydra) Introspect(_ context.Context, _ string) (*http.Response, error) {
	return m.resp, m.err
}

func (m *mockHydra) Token(_ context.Context, form url.Values, authorization string) (*http.Response, error) {
	m.gotForm = form
	m.gotAuth = authorization
	return m.resp, m.err
}

func setupHydraRouter(m *mockHydra) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	h := NewHandler(m, m)
	r.POST("/oauth2/introspect", h.Introspect)
	r.POST("/oauth2/token", h.Token)
	return r
}

func TestHandler_Introspect(t *testing.T) {
	tests := []struct {
		name            string
		formBody        string
		mockResp        *http.Response
		mockErr         error
		wantStatus      int
		wantBody        string
		wantContentType string
	}{
		{
			name:     "passes through hydra 200 response",
			formBody: "token=mytoken",
			mockResp: &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": {"application/json"}},
				Body:       io.NopCloser(strings.NewReader(`{"active":true}`)),
			},
			wantStatus:      http.StatusOK,
			wantBody:        `{"active":true}`,
			wantContentType: "application/json",
		},
		{
			name:     "passes through hydra 401 response",
			formBody: "token=badtoken",
			mockResp: &http.Response{
				StatusCode: http.StatusUnauthorized,
				Header:     http.Header{"Content-Type": {"application/json"}},
				Body:       io.NopCloser(strings.NewReader(`{"active":false}`)),
			},
			wantStatus:      http.StatusUnauthorized,
			wantBody:        `{"active":false}`,
			wantContentType: "application/json",
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
			r := setupHydraRouter(&mockHydra{resp: tt.mockResp, err: tt.mockErr})

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
			if tt.wantContentType != "" {
				if ct := w.Header().Get("Content-Type"); ct != tt.wantContentType {
					t.Errorf("Content-Type: got %q, want %q", ct, tt.wantContentType)
				}
			}
		})
	}
}

func TestHandler_Token(t *testing.T) {
	tests := []struct {
		name        string
		target      string
		formBody    string
		authHeader  string
		mockResp    *http.Response
		mockErr     error
		wantStatus  int
		wantBody    string
		wantHeaders map[string]string
		wantForm    url.Values
		wantAuth    string
	}{
		{
			name:       "forwards form and basic credentials, passes response through",
			target:     "/oauth2/token",
			formBody:   "grant_type=client_credentials&scope=read",
			authHeader: "Basic Y2xpZW50LWFsaWNlOnNlY3JldC1hbGljZQ==",
			mockResp: &http.Response{
				StatusCode: http.StatusOK,
				Header:     header("Content-Type", "application/json", "Cache-Control", "no-store", "Pragma", "no-cache"),
				Body:       io.NopCloser(strings.NewReader(`{"access_token":"tok","token_type":"bearer","expires_in":3599}`)),
			},
			wantStatus: http.StatusOK,
			wantBody:   `{"access_token":"tok","token_type":"bearer","expires_in":3599}`,
			wantHeaders: map[string]string{
				"Content-Type":  "application/json",
				"Cache-Control": "no-store",
				"Pragma":        "no-cache",
			},
			wantForm: url.Values{"grant_type": {"client_credentials"}, "scope": {"read"}},
			wantAuth: "Basic Y2xpZW50LWFsaWNlOnNlY3JldC1hbGljZQ==",
		},
		{
			name:       "query parameters do not leak into the upstream form",
			target:     "/oauth2/token?scope=admin&client_id=attacker",
			formBody:   "grant_type=client_credentials",
			mockResp:   jsonResponse(http.StatusOK, `{"access_token":"tok"}`),
			wantStatus: http.StatusOK,
			wantForm:   url.Values{"grant_type": {"client_credentials"}},
		},
		{
			name:       "passes through hydra 401 with the challenge header",
			target:     "/oauth2/token",
			formBody:   "grant_type=client_credentials",
			authHeader: "Basic YmFkOmNyZWRz",
			mockResp: &http.Response{
				StatusCode: http.StatusUnauthorized,
				Header:     header("Content-Type", "application/json", "WWW-Authenticate", `Basic realm="hydra"`),
				Body:       io.NopCloser(strings.NewReader(`{"error":"invalid_client"}`)),
			},
			wantStatus:  http.StatusUnauthorized,
			wantBody:    `{"error":"invalid_client"}`,
			wantHeaders: map[string]string{"WWW-Authenticate": `Basic realm="hydra"`},
		},
		{
			name:       "unsupported grant is decided upstream, not locally",
			target:     "/oauth2/token",
			formBody:   "grant_type=password",
			mockResp:   jsonResponse(http.StatusBadRequest, `{"error":"unsupported_grant_type"}`),
			wantStatus: http.StatusBadRequest,
			wantBody:   `{"error":"unsupported_grant_type"}`,
		},
		{
			name:       "empty body is decided upstream, not locally",
			target:     "/oauth2/token",
			formBody:   "",
			mockResp:   jsonResponse(http.StatusBadRequest, `{"error":"invalid_request"}`),
			wantStatus: http.StatusBadRequest,
			wantBody:   `{"error":"invalid_request"}`,
		},
		{
			name:       "upstream error returns an oauth2-shaped 502",
			target:     "/oauth2/token",
			formBody:   "grant_type=client_credentials",
			mockErr:    errors.New("connection refused"),
			wantStatus: http.StatusBadGateway,
			wantBody:   `{"error":"server_error","error_description":"token endpoint unavailable"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &mockHydra{resp: tt.mockResp, err: tt.mockErr}
			r := setupHydraRouter(m)

			req := httptest.NewRequest(http.MethodPost, tt.target, strings.NewReader(tt.formBody))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			if tt.authHeader != "" {
				req.Header.Set("Authorization", tt.authHeader)
			}
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			if w.Code != tt.wantStatus {
				t.Errorf("status: got %d, want %d", w.Code, tt.wantStatus)
			}
			if tt.wantBody != "" && w.Body.String() != tt.wantBody {
				t.Errorf("body: got %q, want %q", w.Body.String(), tt.wantBody)
			}
			for name, want := range tt.wantHeaders {
				if got := w.Header().Get(name); got != want {
					t.Errorf("header %s: got %q, want %q", name, got, want)
				}
			}
			if tt.wantForm != nil && !reflect.DeepEqual(m.gotForm, tt.wantForm) {
				t.Errorf("upstream form: got %v, want %v", m.gotForm, tt.wantForm)
			}
			if tt.wantAuth != "" && m.gotAuth != tt.wantAuth {
				t.Errorf("upstream Authorization: got %q, want %q", m.gotAuth, tt.wantAuth)
			}
		})
	}
}

func jsonResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     header("Content-Type", "application/json"),
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

// header builds a response header from key/value pairs via Set, so keys are
// canonicalised the way net/http does when parsing a real response.
func header(kv ...string) http.Header {
	h := http.Header{}
	for i := 0; i < len(kv); i += 2 {
		h.Set(kv[i], kv[i+1])
	}
	return h
}
