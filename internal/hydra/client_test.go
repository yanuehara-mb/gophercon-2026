package hydra

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestClient_Introspect(t *testing.T) {
	tests := []struct {
		name           string
		token          string
		serverStatus   int
		serverBody     string
		serverCT       string
		wantStatus     int
		wantBodySubstr string
	}{
		{
			name:           "active token returns 200",
			token:          "valid-token",
			serverStatus:   http.StatusOK,
			serverBody:     `{"active":true}`,
			serverCT:       "application/json",
			wantStatus:     http.StatusOK,
			wantBodySubstr: `{"active":true}`,
		},
		{
			name:           "inactive token returns 200 with active false",
			token:          "expired-token",
			serverStatus:   http.StatusOK,
			serverBody:     `{"active":false}`,
			serverCT:       "application/json",
			wantStatus:     http.StatusOK,
			wantBodySubstr: `{"active":false}`,
		},
		{
			name:           "server returns 401",
			token:          "bad-token",
			serverStatus:   http.StatusUnauthorized,
			serverBody:     `{"error":"unauthorized"}`,
			serverCT:       "application/json",
			wantStatus:     http.StatusUnauthorized,
			wantBodySubstr: `unauthorized`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// Verify the request method and path
				if r.Method != http.MethodPost {
					t.Errorf("method: got %s, want POST", r.Method)
				}
				if r.URL.Path != "/admin/oauth2/introspect" {
					t.Errorf("path: got %s, want /admin/oauth2/introspect", r.URL.Path)
				}
				if ct := r.Header.Get("Content-Type"); ct != "application/x-www-form-urlencoded" {
					t.Errorf("Content-Type: got %s, want application/x-www-form-urlencoded", ct)
				}
				if err := r.ParseForm(); err != nil {
					t.Fatalf("ParseForm: %v", err)
				}
				if got := r.FormValue("token"); got != tt.token {
					t.Errorf("token form field: got %q, want %q", got, tt.token)
				}

				w.Header().Set("Content-Type", tt.serverCT)
				w.WriteHeader(tt.serverStatus)
				io.WriteString(w, tt.serverBody)
			}))
			defer srv.Close()

			client := NewClient(srv.URL)
			resp, err := client.Introspect(context.Background(), tt.token)
			if err != nil {
				t.Fatalf("Introspect returned error: %v", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != tt.wantStatus {
				t.Errorf("status: got %d, want %d", resp.StatusCode, tt.wantStatus)
			}

			body, err := io.ReadAll(resp.Body)
			if err != nil {
				t.Fatalf("ReadAll: %v", err)
			}
			if !strings.Contains(string(body), tt.wantBodySubstr) {
				t.Errorf("body: got %q, want it to contain %q", string(body), tt.wantBodySubstr)
			}
		})
	}
}

func TestClient_Introspect_ConnectionError(t *testing.T) {
	// Use a server that is immediately closed to simulate connection refused
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := srv.URL
	srv.Close()

	client := NewClient(url)
	_, err := client.Introspect(context.Background(), "some-token")
	if err == nil {
		t.Error("expected error when server is unavailable, got nil")
	}
}
