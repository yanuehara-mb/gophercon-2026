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
