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
