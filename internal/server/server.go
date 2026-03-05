package server

import (
	"context"
	"crypto/tls"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/DesyncTheThird/rIOt/internal/models"
	"github.com/DesyncTheThird/rIOt/internal/server/db"
	"github.com/DesyncTheThird/rIOt/internal/server/events"
	"github.com/DesyncTheThird/rIOt/internal/server/notify"
	"github.com/DesyncTheThird/rIOt/internal/server/probes"
	"github.com/DesyncTheThird/rIOt/internal/server/updates"
	"github.com/DesyncTheThird/rIOt/internal/server/websocket"
	"golang.org/x/crypto/acme/autocert"
)

type Server struct {
	Config         *Config
	Version        string
	MigrationsFS   fs.FS
	FrontendFS     fs.FS
	DB             *db.DB
	DeviceRepo     *db.DeviceRepo
	TelemetryRepo  *db.TelemetryRepo
	EventRepo      *db.EventRepo
	AdminRepo      *db.AdminRepo
	TerminalRepo   *db.TerminalRepo
	AlertRuleRepo  *db.AlertRuleRepo
	NotifyRepo     *db.NotifyRepo
	CommandRepo    *db.CommandRepo
	ProbeRepo      *db.ProbeRepo
	ProbeRunner    *probes.Runner
	Hub            *websocket.Hub
	EventGen       *events.Generator
	Dispatcher     *notify.Dispatcher
	UpdateChecker  *updates.Checker
	JWTSecret      []byte
	httpServer     *http.Server
}

func New(cfg *Config) *Server {
	return &Server{Config: cfg}
}

func (s *Server) Start() error {
	ctx := context.Background()
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	// Connect to database
	slog.Info("connecting to database")
	database, err := db.New(ctx, s.Config.DBUrl)
	if err != nil {
		return fmt.Errorf("database: %w", err)
	}
	s.DB = database

	// Run migrations
	if s.MigrationsFS != nil {
		slog.Info("running database migrations")
		if err := s.DB.RunMigrations(s.MigrationsFS, s.Config.DBUrl); err != nil {
			return fmt.Errorf("migrations: %w", err)
		}
	}

	// Initialize repos
	s.DeviceRepo = db.NewDeviceRepo(s.DB)
	s.TelemetryRepo = db.NewTelemetryRepo(s.DB)
	s.EventRepo = db.NewEventRepo(s.DB)
	s.AdminRepo = db.NewAdminRepo(s.DB)
	s.TerminalRepo = db.NewTerminalRepo(s.DB)
	s.AlertRuleRepo = db.NewAlertRuleRepo(s.DB)
	s.NotifyRepo = db.NewNotifyRepo(s.DB)
	s.CommandRepo = db.NewCommandRepo(s.DB)
	s.ProbeRepo = db.NewProbeRepo(s.DB)

	// Store JWT secret
	s.JWTSecret = []byte(s.Config.JWTSecret)

	// Set up admin password
	if s.Config.AdminPasswordHash != "" {
		if err := s.AdminRepo.SetPasswordHash(ctx, s.Config.AdminPasswordHash); err != nil {
			slog.Error("failed to store admin password hash", "error", err)
		} else {
			slog.Info("admin password configured")
		}
	} else {
		// Check if one exists in DB already
		hash, err := s.AdminRepo.GetPasswordHash(ctx)
		if err != nil || hash == "" {
			slog.Warn("no admin password configured — set RIOT_ADMIN_PASSWORD env var to enable dashboard authentication")
		}
	}

	// Initialize WebSocket hub
	s.Hub = websocket.NewHub()
	go s.Hub.Run()

	// Initialize event generator and notification dispatcher
	s.Dispatcher = notify.NewDispatcher(s.NotifyRepo)
	s.EventGen = events.NewGenerator(s.EventRepo, s.Hub, s.AlertRuleRepo, s.Dispatcher)

	// Initialize probe runner
	s.ProbeRunner = probes.NewRunner(s.ProbeRepo, s.EventRepo, s.Hub)

	// Seed default alert rules on first run
	s.seedDefaultAlertRules(ctx)

	// Initialize update checker
	s.UpdateChecker = updates.NewChecker(s.Config.GitHubRepo, s.Version)
	go s.UpdateChecker.Start(ctx)

	// Start retention worker
	go s.retentionWorker(ctx)

	// Start offline detection worker
	go s.offlineDetectionWorker(ctx)

	// Start probe runner
	go s.ProbeRunner.Start(ctx)

	// Set up HTTP server
	router := s.setupRouter()
	s.httpServer = &http.Server{
		Addr:        fmt.Sprintf(":%d", s.Config.Port),
		Handler:     router,
		ReadTimeout: 15 * time.Second,
		// WriteTimeout is intentionally omitted — it sets a deadline on the
		// underlying net.Conn that persists after WebSocket hijack, killing
		// all long-lived connections (dashboard WS, agent WS, terminal).
		// Per-handler write deadlines are managed by gorilla/websocket instead.
		IdleTimeout: 60 * time.Second,
	}

	// Configure TLS if enabled
	if s.Config.TLSEnabled {
		if err := s.configureTLS(); err != nil {
			return fmt.Errorf("tls: %w", err)
		}
	}

	// Graceful shutdown
	done := make(chan os.Signal, 1)
	signal.Notify(done, os.Interrupt, syscall.SIGTERM)

	go func() {
		if s.Config.TLSEnabled {
			slog.Info("server starting with TLS", "port", s.Config.Port)
			var err error
			if s.Config.TLSDomain != "" {
				// autocert handles cert files
				err = s.httpServer.ListenAndServeTLS("", "")
			} else {
				err = s.httpServer.ListenAndServeTLS(s.Config.TLSCertFile, s.Config.TLSKeyFile)
			}
			if err != nil && err != http.ErrServerClosed {
				slog.Error("server error", "error", err)
				os.Exit(1)
			}
		} else {
			slog.Info("server starting", "port", s.Config.Port)
			if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				slog.Error("server error", "error", err)
				os.Exit(1)
			}
		}
	}()

	<-done
	slog.Info("shutting down server")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := s.httpServer.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutdown: %w", err)
	}
	s.DB.Close()
	slog.Info("server stopped")
	return nil
}

func (s *Server) retentionWorker(ctx context.Context) {
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.runRetention(ctx)
		}
	}
}

func (s *Server) runRetention(ctx context.Context) {
	now := time.Now().UTC()

	hbDeleted, err := s.TelemetryRepo.PurgeHeartbeats(ctx, now.AddDate(0, 0, -7))
	if err != nil {
		slog.Error("purge heartbeats failed", "error", err)
	} else if hbDeleted > 0 {
		slog.Info("purged old heartbeats", "count", hbDeleted)
	}

	snapDeleted, err := s.TelemetryRepo.PurgeSnapshots(ctx, now.AddDate(0, 0, -s.Config.RetentionDays))
	if err != nil {
		slog.Error("purge snapshots failed", "error", err)
	} else if snapDeleted > 0 {
		slog.Info("purged old snapshots", "count", snapDeleted)
	}

	evtDeleted, err := s.EventRepo.Purge(ctx, now.AddDate(0, 0, -90))
	if err != nil {
		slog.Error("purge events failed", "error", err)
	} else if evtDeleted > 0 {
		slog.Info("purged old events", "count", evtDeleted)
	}

	prDeleted, err := s.ProbeRepo.PurgeResults(ctx, now.AddDate(0, 0, -s.Config.RetentionDays))
	if err != nil {
		slog.Error("purge probe results failed", "error", err)
	} else if prDeleted > 0 {
		slog.Info("purged old probe results", "count", prDeleted)
	}

	nlDeleted, err := s.NotifyRepo.PurgeNotificationLog(ctx, now.AddDate(0, 0, -90))
	if err != nil {
		slog.Error("purge notification log failed", "error", err)
	} else if nlDeleted > 0 {
		slog.Info("purged old notification log entries", "count", nlDeleted)
	}
}

func (s *Server) offlineDetectionWorker(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.checkOfflineDevices(ctx)
		}
	}
}

func (s *Server) checkOfflineDevices(ctx context.Context) {
	devices, err := s.DeviceRepo.List(ctx)
	if err != nil {
		return
	}
	threshold := time.Now().UTC().Add(-45 * time.Second) // 3x 15s heartbeat interval
	for _, d := range devices {
		if d.Status == "online" && d.LastHeartbeat != nil && d.LastHeartbeat.Before(threshold) {
			s.DeviceRepo.SetStatus(ctx, d.ID, "offline")
			s.EventGen.DeviceOffline(ctx, d.ID, d.Hostname)
		}
	}
}

// configureTLS sets up TLS on the HTTP server.
func (s *Server) configureTLS() error {
	if s.Config.TLSDomain != "" {
		// Let's Encrypt autocert
		certDir := s.Config.TLSCertDir
		if certDir == "" {
			certDir = "certs"
		}
		m := &autocert.Manager{
			Prompt:     autocert.AcceptTOS,
			HostPolicy: autocert.HostWhitelist(s.Config.TLSDomain),
			Cache:      autocert.DirCache(certDir),
		}
		s.httpServer.TLSConfig = m.TLSConfig()
		slog.Info("TLS configured with Let's Encrypt autocert", "domain", s.Config.TLSDomain, "cache", certDir)
		return nil
	}

	// Manual certificate
	if s.Config.TLSCertFile == "" || s.Config.TLSKeyFile == "" {
		return fmt.Errorf("TLS enabled but no domain or cert/key files configured")
	}
	cert, err := tls.LoadX509KeyPair(s.Config.TLSCertFile, s.Config.TLSKeyFile)
	if err != nil {
		return fmt.Errorf("load cert: %w", err)
	}
	s.httpServer.TLSConfig = &tls.Config{
		Certificates: []tls.Certificate{cert},
	}
	slog.Info("TLS configured with manual certificate", "cert", s.Config.TLSCertFile)
	return nil
}

// seedDefaultAlertRules creates default alert rules if none exist yet.
func (s *Server) seedDefaultAlertRules(ctx context.Context) {
	existing, err := s.AlertRuleRepo.List(ctx)
	if err != nil {
		slog.Error("seed alert rules: list", "error", err)
		return
	}
	if len(existing) > 0 {
		return
	}

	defaults := []models.AlertRule{
		{Name: "High Memory Usage", Enabled: true, Metric: "mem_percent", Operator: ">", Threshold: 90, Severity: "warning", CooldownSeconds: 3600, Notify: true},
		{Name: "High Disk Usage", Enabled: true, Metric: "disk_percent", Operator: ">", Threshold: 90, Severity: "critical", CooldownSeconds: 3600, Notify: true},
		{Name: "Updates Available", Enabled: true, Metric: "updates", Operator: ">", Threshold: 0, Severity: "info", CooldownSeconds: 86400, Notify: true},
		{Name: "Container Died", Enabled: true, Metric: "container_died", Operator: "==", Threshold: 1, Severity: "warning", CooldownSeconds: 900, Notify: true},
		{Name: "Container OOM Killed", Enabled: true, Metric: "container_oom", Operator: "==", Threshold: 1, Severity: "critical", CooldownSeconds: 900, Notify: true},
		{Name: "Device Offline", Enabled: true, Metric: "device_offline", Operator: "==", Threshold: 1, Severity: "warning", CooldownSeconds: 300, Notify: true},
	}
	for _, rule := range defaults {
		if err := s.AlertRuleRepo.Create(ctx, &rule); err != nil {
			slog.Error("seed alert rule", "name", rule.Name, "error", err)
		}
	}
	slog.Info("seeded default alert rules", "count", len(defaults))
}
