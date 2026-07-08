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
