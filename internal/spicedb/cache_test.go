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
