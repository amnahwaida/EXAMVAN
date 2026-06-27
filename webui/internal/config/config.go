package config

import (
	"os"
	"strconv"
)

const (
	DefaultPort         = 5000
	DefaultAdminUser    = "superadmin"
	DefaultStoragePath  = "/app/storage"
	DefaultMaxFileSize  = 5 * 1024 * 1024 // 5 MB
	DefaultVersion      = "2.2.3"
)

type Config struct {
	ServerPort  int
	DatabaseURL string
	RedisURL    string
	SecretKey   string
	AdminUser   string
	AdminPass   string
	StoragePath string
	MaxFileSize int64
	Version     string
}

func Load() *Config {
	cfg := &Config{
		ServerPort:  envInt("PORT", DefaultPort),
		DatabaseURL: os.Getenv("DATABASE_URL"),
		RedisURL:    os.Getenv("REDIS_URL"),
		SecretKey:   os.Getenv("EXAMVAN_SECRET"),
		AdminUser:   envStr("EXAMVAN_ADMIN_USER", DefaultAdminUser),
		AdminPass:   os.Getenv("EXAMVAN_ADMIN_PASS"),
		StoragePath: envStr("STORAGE_PATH", DefaultStoragePath),
		MaxFileSize: DefaultMaxFileSize,
		Version:     DefaultVersion,
	}

	if cfg.StoragePath == "" {
		cfg.StoragePath = DefaultStoragePath
	}

	return cfg
}

func (c *Config) IsDevelopment() bool {
	return os.Getenv("APP_ENV") == "development"
}

func envStr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			return i
		}
	}
	return fallback
}
