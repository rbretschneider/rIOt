package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/DesyncTheThird/rIOt/internal/models"
	"github.com/DesyncTheThird/rIOt/internal/server/db"
	"github.com/DesyncTheThird/rIOt/internal/server/notify"
)

const serverErrorAlertConfigKey = "server_error_alert_config"

// serverErrorAlertWorker watches the server's own ERROR log rate and fires a
// notification to the user-selected channel when it exceeds the configured
// threshold. Intended to catch runaway cascades (e.g. DB pool exhaustion)
// without requiring the operator to keep the Server Logs page open.
type serverErrorAlertWorker struct {
	adminRepo  db.AdminRepository
	logRepo    db.LogRepository
	notifyRepo db.NotifyRepository
	dispatcher *notify.Dispatcher

	mu          sync.Mutex
	lastFiredAt time.Time
}

func newServerErrorAlertWorker(adminRepo db.AdminRepository, logRepo db.LogRepository, notifyRepo db.NotifyRepository, dispatcher *notify.Dispatcher) *serverErrorAlertWorker {
	return &serverErrorAlertWorker{
		adminRepo:  adminRepo,
		logRepo:    logRepo,
		notifyRepo: notifyRepo,
		dispatcher: dispatcher,
	}
}

func (w *serverErrorAlertWorker) run(ctx context.Context) {
	// Evaluate once shortly after startup, then every minute. A 1-minute tick
	// gives reasonable responsiveness while avoiding extra DB load — and the
	// rolling window itself (minutes) provides the real smoothing.
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	// Short initial delay lets the server finish booting before the first check.
	timer := time.NewTimer(30 * time.Second)
	defer timer.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			w.evaluate(ctx)
		case <-ticker.C:
			w.evaluate(ctx)
		}
	}
}

func (w *serverErrorAlertWorker) evaluate(ctx context.Context) {
	cfg, ok := w.loadConfig(ctx)
	if !ok || !cfg.Enabled || cfg.ChannelID <= 0 {
		return
	}

	since := time.Now().Add(-time.Duration(cfg.WindowMinutes) * time.Minute)
	count, err := w.logRepo.CountSince(ctx, "ERROR", since)
	if err != nil {
		// Don't log here: a DB-count failure is likely the same cascade we're
		// trying to detect, and logging would just amplify the error rate.
		return
	}
	if count < cfg.Threshold {
		return
	}

	w.mu.Lock()
	cooldown := time.Duration(cfg.CooldownMinutes) * time.Minute
	if cfg.CooldownMinutes <= 0 {
		cooldown = 30 * time.Minute
	}
	if !w.lastFiredAt.IsZero() && time.Since(w.lastFiredAt) < cooldown {
		w.mu.Unlock()
		return
	}
	w.lastFiredAt = time.Now()
	w.mu.Unlock()

	msg := fmt.Sprintf("rIOt server logged %d ERROR entries in the last %d minutes (threshold: %d). Check the Server Logs page for details.",
		count, cfg.WindowMinutes, cfg.Threshold)
	alert := models.Alert{
		Event: &models.Event{
			Type:      "server_error_rate",
			Severity:  models.SeverityWarning,
			Message:   msg,
			CreatedAt: time.Now().UTC(),
		},
	}
	if err := w.dispatcher.SendToChannel(ctx, cfg.ChannelID, alert); err != nil {
		slog.Error("server-error-alert: send failed", "error", err.Error())
	}
}

func (w *serverErrorAlertWorker) loadConfig(ctx context.Context) (models.ServerErrorAlertConfig, bool) {
	cfg := models.DefaultServerErrorAlertConfig()
	raw, err := w.adminRepo.GetConfig(ctx, serverErrorAlertConfigKey)
	if err != nil || raw == "" {
		return cfg, false
	}
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		return cfg, false
	}
	return cfg, true
}
