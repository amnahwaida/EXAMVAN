package config

import (
	"log"
	"os"
	"strconv"
)

const (
	DefaultPort        = 5000
	DefaultStoragePath = "/app/storage"
	DefaultMaxFileSize = 5 * 1024 * 1024 // 5 MB
	TokenLength        = 8
)

// DefaultVersion is shown in the UI and used for asset cache-busting (?v=...).
// It is overridden at build time via -ldflags "-X ..." with a per-build value
// (e.g. a hash of the static assets) so that browsers/Cloudflare never serve
// stale CSS/JS from a previous build.
var DefaultVersion = "2.2.3"

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
	DatabaseMaxConns int

	// Cloudflare R2 (S3-compatible object storage)
	R2AccessKey string
	R2SecretKey string
	R2Bucket    string
	R2Endpoint  string

	// CORS Origins
	CORSOrigins string
}

func Load() *Config {
	cfg := &Config{
		ServerPort:  envInt("PORT", DefaultPort),
		DatabaseURL: os.Getenv("DATABASE_URL"),
		RedisURL:    os.Getenv("REDIS_URL"),
		SecretKey:   os.Getenv("EXAMVAN_SECRET"),
		AdminUser:   os.Getenv("EXAMVAN_ADMIN_USER"),
		AdminPass:   os.Getenv("EXAMVAN_ADMIN_PASS"),
		StoragePath: envStr("STORAGE_PATH", DefaultStoragePath),
		MaxFileSize: DefaultMaxFileSize,
		Version:     DefaultVersion,
		DatabaseMaxConns: envInt("DATABASE_MAX_CONNS", 100),
		R2AccessKey: os.Getenv("R2_ACCESS_KEY_ID"),
		R2SecretKey: os.Getenv("R2_SECRET_ACCESS_KEY"),
		R2Bucket:    envStr("R2_BUCKET", "examvan-pdfs"),
		R2Endpoint:  os.Getenv("R2_ENDPOINT"),
		CORSOrigins: os.Getenv("EXAMVAN_CORS_ORIGINS"),
	}

	if cfg.StoragePath == "" {
		cfg.StoragePath = DefaultStoragePath
	}

	if cfg.AdminUser == "" {
		log.Fatalf("EXAMVAN_ADMIN_USER environment variable is required and must not be empty.")
	}

	if cfg.AdminPass == "" {
		log.Fatalf("EXAMVAN_ADMIN_PASS environment variable is required and must not be empty.")
	}

	// Cloudflare R2 Mandatory configuration validation
	if cfg.R2AccessKey == "" || cfg.R2SecretKey == "" || cfg.R2Endpoint == "" {
		log.Fatalf("Cloudflare R2 is MANDATORY: R2_ACCESS_KEY_ID, R2_SECRET_ACCESS_KEY, and R2_ENDPOINT must all be set in .env.")
	}

	if len(cfg.SecretKey) < 32 {
		log.Fatalf("EXAMVAN_SECRET must be at least 32 characters long for security purposes.")
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
