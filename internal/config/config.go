package config

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"strconv"
	"time"
)

// Config holds runtime configuration loaded from environment variables.
type Config struct {
	Port         string
	DatabasePath string
	Timezone     string
	Location     *time.Location

	// IngestToken authorizes POST /api/v1/sms only (iPhone Shortcut).
	IngestToken string
	// AdminToken authorizes management APIs via Bearer (optional; login preferred).
	AdminToken string
	// AdminPassword enables browser login (session cookie).
	AdminPassword string
	// SessionSecret signs/identifies sessions.
	SessionSecret string
	// SecureCookie: set Cookie Secure flag (true behind HTTPS).
	SecureCookie bool
	// SessionTTL hours.
	SessionTTL time.Duration
}

// Load reads configuration from environment variables with sensible defaults.
func Load() (Config, error) {
	// Backward compatible: API_TOKEN alone still works for both roles.
	legacy := os.Getenv("API_TOKEN")
	ingest := firstNonEmpty(os.Getenv("INGEST_TOKEN"), legacy)
	adminTok := firstNonEmpty(os.Getenv("ADMIN_TOKEN"), legacy)
	adminPass := os.Getenv("ADMIN_PASSWORD")

	cfg := Config{
		Port:          getEnv("PORT", "8080"),
		DatabasePath:  getEnv("DATABASE_PATH", "./data/cashpulse.db"),
		Timezone:      getEnv("TZ_NAME", "Asia/Shanghai"),
		IngestToken:   ingest,
		AdminToken:    adminTok,
		AdminPassword: adminPass,
		SessionSecret: os.Getenv("SESSION_SECRET"),
		SecureCookie:  os.Getenv("SECURE_COOKIE") == "1" || os.Getenv("SECURE_COOKIE") == "true",
		SessionTTL:    30 * 24 * time.Hour, // stay logged in ~1 month
	}

	if h := os.Getenv("SESSION_TTL_HOURS"); h != "" {
		if n, err := strconv.Atoi(h); err == nil && n > 0 {
			cfg.SessionTTL = time.Duration(n) * time.Hour
		}
	}

	if cfg.IngestToken == "" {
		return Config{}, fmt.Errorf("INGEST_TOKEN or API_TOKEN is required (for SMS upload)")
	}
	if cfg.AdminToken == "" && cfg.AdminPassword == "" {
		return Config{}, fmt.Errorf("ADMIN_PASSWORD or ADMIN_TOKEN or API_TOKEN is required (for admin access)")
	}
	if cfg.SessionSecret == "" {
		// Derive a process-local secret if not provided (sessions reset on restart).
		b := make([]byte, 32)
		if _, err := rand.Read(b); err != nil {
			return Config{}, err
		}
		cfg.SessionSecret = hex.EncodeToString(b)
	}
	if _, err := strconv.Atoi(cfg.Port); err != nil {
		return Config{}, fmt.Errorf("invalid PORT %q: %w", cfg.Port, err)
	}

	loc, err := time.LoadLocation(cfg.Timezone)
	if err != nil {
		loc = time.FixedZone("CST", 8*3600)
		cfg.Timezone = "CST+8"
	}
	cfg.Location = loc
	return cfg, nil
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
