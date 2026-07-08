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
