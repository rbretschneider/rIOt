package agent

import (
	"context"
	"log/slog"
	"net/http"
	"time"
)

// DeadManConfig controls the dead man's switch heartbeat.
type DeadManConfig struct {
	Enabled         bool   `yaml:"enabled"`
	URL             string `yaml:"url"`               // healthcheck ping URL (e.g. https://hc-ping.com/<uuid>)
	IntervalSeconds int    `yaml:"interval_seconds"`   // default 60
}

// deadManLoop sends periodic pings to a healthcheck URL.
// On consecutive failures, it increases retry frequency.
func (a *Agent) deadManLoop(ctx context.Context) {
	cfg := a.config.DeadMan
	if !cfg.Enabled || cfg.URL == "" {
		return
	}

	interval := time.Duration(cfg.IntervalSeconds) * time.Second
	if interval == 0 {
		interval = 60 * time.Second
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	consecutiveFails := 0

	// Initial ping
	if err := a.deadManPing(ctx, cfg.URL); err != nil {
		consecutiveFails++
		slog.Warn("dead man's switch: initial ping failed", "error", err)
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := a.deadManPing(ctx, cfg.URL); err != nil {
				consecutiveFails++
				slog.Warn("dead man's switch: ping failed", "consecutive", consecutiveFails, "error", err)

				// Increase retry frequency on sustained failure
				if consecutiveFails >= 2 {
					ticker.Reset(interval / 2)
				}
			} else {
				if consecutiveFails > 0 {
					slog.Info("dead man's switch: recovered after failures", "consecutive", consecutiveFails)
					ticker.Reset(interval) // restore normal interval
				}
				consecutiveFails = 0
			}
		}
	}
}

func (a *Agent) deadManPing(ctx context.Context, url string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}
