// Package config loads the service configuration from environment variables.
// Every value has a sane local-development default and can be overridden
// through the environment (no hardcoded secrets or addresses in production).
package config

import (
	"os"
	"time"
)

type Config struct {
	ServerPort string
	MySQLDSN   string
	RedisAddr  string
	JWTSecret  string
	JWTExpiry  time.Duration
	CacheTTL   time.Duration
}

func Load() Config {
	return Config{
		ServerPort: getenv("SERVER_PORT", "8080"),
		MySQLDSN:   getenv("MYSQL_DSN", "taskuser:taskpass@tcp(localhost:3306)/taskdb?parseTime=true&charset=utf8mb4&loc=UTC"),
		RedisAddr:  getenv("REDIS_ADDR", "localhost:6379"),
		JWTSecret:  getenv("JWT_SECRET", "dev-secret-change-me"),
		JWTExpiry:  getenvDuration("JWT_EXPIRY", 24*time.Hour),
		CacheTTL:   getenvDuration("CACHE_TTL", 5*time.Minute),
	}
}

func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func getenvDuration(key string, def time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return def
}
