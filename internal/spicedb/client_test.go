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
