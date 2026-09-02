package hydra

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
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

			client := NewClient(srv.URL, srv.URL)
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

func TestClient_Token(t *testing.T) {
	var (
		gotPath   string
		gotMethod string
		gotCT     string
		gotAuth   string
		gotForm   url.Values
	)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotMethod = r.URL.Path, r.Method
		gotCT = r.Header.Get("Content-Type")
		gotAuth = r.Header.Get("Authorization")
		if err := r.ParseForm(); err != nil {
			t.Fatalf("ParseForm: %v", err)
		}
		gotForm = r.PostForm

		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		io.WriteString(w, `{"access_token":"tok","token_type":"bearer"}`)
	}))
	defer srv.Close()

	form := url.Values{"grant_type": {"client_credentials"}, "scope": {"read"}}
	// Admin URL deliberately bogus: the token endpoint must be reached through
	// the public one.
	client := NewClient("http://unused.invalid", srv.URL)
	resp, err := client.Token(context.Background(), form, "Basic Zm9vOmJhcg==")
	if err != nil {
		t.Fatalf("Token returned error: %v", err)
	}
	defer resp.Body.Close()

	checks := []struct {
		name string
		got  string
		want string
	}{
		{"path", gotPath, "/oauth2/token"},
		{"method", gotMethod, http.MethodPost},
		{"Content-Type", gotCT, "application/x-www-form-urlencoded"},
		{"Authorization", gotAuth, "Basic Zm9vOmJhcg=="},
		{"grant_type", gotForm.Get("grant_type"), "client_credentials"},
		{"scope", gotForm.Get("scope"), "read"},
		{"response Cache-Control", resp.Header.Get("Cache-Control"), "no-store"},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, c.got, c.want)
		}
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if !strings.Contains(string(body), `"access_token":"tok"`) {
		t.Errorf("body: got %q, want it to contain the access token", string(body))
	}
}

func TestClient_Token_OmitsEmptyAuthorization(t *testing.T) {
	// Client credentials may travel in the form instead of a Basic header
	// (RFC 6749 §2.3.1); the facade must not invent an empty header.
	var hadAuth bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, hadAuth = r.Header["Authorization"]
		io.WriteString(w, `{}`)
	}))
	defer srv.Close()

	client := NewClient(srv.URL, srv.URL)
	resp, err := client.Token(context.Background(), url.Values{"grant_type": {"client_credentials"}}, "")
	if err != nil {
		t.Fatalf("Token returned error: %v", err)
	}
	defer resp.Body.Close()

	if hadAuth {
		t.Error("Authorization header was sent upstream despite being empty")
	}
}

func TestClient_Introspect_ConnectionError(t *testing.T) {
	// Use a server that is immediately closed to simulate connection refused
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := srv.URL
	srv.Close()

	client := NewClient(url, url)
	_, err := client.Introspect(context.Background(), "some-token")
	if err == nil {
		t.Error("expected error when server is unavailable, got nil")
	}
}
