package config

import (
	"os"
	"time"
)

type Config struct {
	DatabasePath     string
	JWTSecret        string
	JWTExpiration    time.Duration
	ServerPort       string
	InviteExpiration time.Duration
}

func Load() *Config {
	return &Config{
		DatabasePath:     getEnv("DATABASE_PATH", "overtime.db"),
		JWTSecret:        getEnv("JWT_SECRET", "your-super-secret-key-change-in-production"),
		JWTExpiration:    24 * time.Hour,
		ServerPort:       getEnv("SERVER_PORT", "8080"),
		InviteExpiration: 7 * 24 * time.Hour, // 7 days
	}
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
