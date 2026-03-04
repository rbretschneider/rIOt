package server

import (
	"os"
	"strconv"
)

type Config struct {
	Port          int
	DBUrl         string
	MasterAPIKey  string
	RetentionDays int
	GitHubRepo    string
}

func LoadConfig() *Config {
	cfg := &Config{
		Port:          7331,
		DBUrl:         "postgres://riot:riot@localhost:5432/riot?sslmode=disable",
		MasterAPIKey:  "changeme",
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
		cfg.MasterAPIKey = v
	}
	if v := os.Getenv("RIOT_RETENTION_DAYS"); v != "" {
		if d, err := strconv.Atoi(v); err == nil {
			cfg.RetentionDays = d
		}
	}
	if v := os.Getenv("RIOT_GITHUB_REPO"); v != "" {
		cfg.GitHubRepo = v
	}
	return cfg
}
