package server

import (
	"log/slog"
	"os"
	"strconv"
)

func parseContainerLogRetention(raw string) int {
	if raw == "" {
		return 7
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 || n > 90 {
		slog.Warn("invalid container log retention; using 7 days")
		return 7
	}
	return n
}

func positiveEnv(key string, fallback int) int {
	raw := os.Getenv(key)
	if raw == "" {
		return fallback
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 || n > 1_000_000 {
		slog.Warn("invalid positive integer setting; using default", "setting", key)
		return fallback
	}
	return n
}

func (s *Server) EffectiveContainerLogRetention() int {
	if s.Config.ContainerLogRetentionDays < 1 || s.Config.ContainerLogRetentionDays > 90 {
		return 7
	}
	return s.Config.ContainerLogRetentionDays
}
