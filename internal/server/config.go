package server

import (
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"os"
	"strconv"
	"strings"

	"golang.org/x/crypto/bcrypt"
)

type Config struct {
	Port              int
	DBUrl             string
	RegistrationKey   string // optional key to gate device registration (empty = open)
	RetentionDays     int
	GitHubRepo        string
	AdminPasswordHash string
	JWTSecret         string
	AllowedOrigins    []string
	TLSEnabled        bool
	TLSMode           string // "self-signed", "letsencrypt", "manual", or ""
	TLSDomain         string // Let's Encrypt autocert domain
	TLSCertDir        string // autocert cache directory
	TLSCertFile       string // manual TLS cert file
	TLSKeyFile        string // manual TLS key file
	MTLSEnabled       bool   // enable mTLS device authentication
	SetupComplete     bool   // whether initial setup wizard has been completed
}

func LoadConfig() *Config {
	cfg := &Config{
		Port:          7331,
		DBUrl:         "postgres://riot:riot@localhost:5432/riot?sslmode=disable",
		RetentionDays: 30,
		GitHubRepo:    "rbretschneider/rIOt",
	}

	if v := os.Getenv("RIOT_PORT"); v != "" {
		if p, err := strconv.Atoi(v); err == nil {
			cfg.Port = p
		}
	}
	if v := os.Getenv("RIOT_DB_URL"); v != "" {
		cfg.DBUrl = v
	}
	if v := os.Getenv("RIOT_API_KEY"); v != "" {
		cfg.RegistrationKey = v
	}
	if v := os.Getenv("RIOT_RETENTION_DAYS"); v != "" {
		if d, err := strconv.Atoi(v); err == nil {
			cfg.RetentionDays = d
		}
	}
	if v := os.Getenv("RIOT_GITHUB_REPO"); v != "" {
		cfg.GitHubRepo = v
	}

	// Admin password — bcrypt hash computed at load time
	if v := os.Getenv("RIOT_ADMIN_PASSWORD"); v != "" {
		hash, err := bcrypt.GenerateFromPassword([]byte(v), bcrypt.DefaultCost)
		if err != nil {
			slog.Error("failed to hash admin password", "error", err.Error())
		} else {
			cfg.AdminPasswordHash = string(hash)
		}
	}

	// JWT secret — auto-generated if not provided
	if v := os.Getenv("RIOT_JWT_SECRET"); v != "" {
		cfg.JWTSecret = v
	} else {
		b := make([]byte, 32)
		rand.Read(b)
		cfg.JWTSecret = hex.EncodeToString(b)
		slog.Info("generated random JWT secret (set RIOT_JWT_SECRET for stable sessions across restarts)")
	}

	// TLS
	if v := os.Getenv("RIOT_TLS_ENABLED"); v == "true" || v == "1" {
		cfg.TLSEnabled = true
	}
	if v := os.Getenv("RIOT_TLS_DOMAIN"); v != "" {
		cfg.TLSDomain = v
		cfg.TLSEnabled = true // domain implies TLS
	}
	if v := os.Getenv("RIOT_TLS_CERT_DIR"); v != "" {
		cfg.TLSCertDir = v
	}
	if v := os.Getenv("RIOT_TLS_CERT_FILE"); v != "" {
		cfg.TLSCertFile = v
	}
	if v := os.Getenv("RIOT_TLS_KEY_FILE"); v != "" {
		cfg.TLSKeyFile = v
	}

	// mTLS
	if v := os.Getenv("RIOT_MTLS_ENABLED"); v == "true" || v == "1" {
		cfg.MTLSEnabled = true
	}

	// Allowed CORS origins
	if v := os.Getenv("RIOT_ALLOWED_ORIGINS"); v != "" {
		for _, o := range strings.Split(v, ",") {
			o = strings.TrimSpace(o)
			if o != "" {
				cfg.AllowedOrigins = append(cfg.AllowedOrigins, o)
			}
		}
	}

	return cfg
}
