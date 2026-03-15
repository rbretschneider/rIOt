package server

import (
	"context"
	"crypto/tls"
	"fmt"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"crypto/x509"

	"github.com/DesyncTheThird/rIOt/internal/models"
	"github.com/DesyncTheThird/rIOt/internal/server/ca"
	"github.com/DesyncTheThird/rIOt/internal/server/db"
	"github.com/DesyncTheThird/rIOt/internal/server/events"
	"github.com/DesyncTheThird/rIOt/internal/server/logstore"
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
	CARepo         *db.CARepo
	LogRepo        *db.LogRepo
	DeviceLogRepo  *db.DeviceLogRepo
	AutoUpdateRepo      *db.AutoUpdateRepo
	ContainerMetricRepo *db.ContainerMetricRepo
	DeviceProbeRepo     *db.DeviceProbeRepo
	LogHandler     *logstore.DBHandler
	CA             *ca.CA
	ProbeRunner    *probes.Runner
	Hub            *websocket.Hub
	EventGen       *events.Generator
	Dispatcher     *notify.Dispatcher
	UpdateChecker  *updates.Checker
	JWTSecret      []byte
	SetupComplete  atomic.Bool
	httpServer     *http.Server
	listener       net.Listener
	router         http.Handler
	done           chan os.Signal
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
	s.CARepo = db.NewCARepo(s.DB)
	s.LogRepo = db.NewLogRepo(s.DB)
	s.DeviceLogRepo = db.NewDeviceLogRepo(s.DB)
	s.AutoUpdateRepo = db.NewAutoUpdateRepo(s.DB)
	s.ContainerMetricRepo = db.NewContainerMetricRepo(s.DB)
	s.DeviceProbeRepo = db.NewDeviceProbeRepo(s.DB)

	// Set up database log handler (stores WARN+ logs to DB alongside stdout)
	logStoreLevel := slog.LevelWarn
	if lvl := os.Getenv("RIOT_LOG_STORE_LEVEL"); lvl != "" {
		switch strings.ToUpper(lvl) {
		case "INFO":
			logStoreLevel = slog.LevelInfo
		case "WARN", "WARNING":
			logStoreLevel = slog.LevelWarn
		case "ERROR":
			logStoreLevel = slog.LevelError
		}
	}
	s.LogHandler = logstore.NewDBHandler(s.LogRepo, logStoreLevel)
	jsonHandler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})
	multiHandler := logstore.NewMultiHandler(jsonHandler, s.LogHandler)
	slog.SetDefault(slog.New(multiHandler))

	// Load config from DB, with env vars taking precedence
	s.loadDBConfig(ctx)

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
			slog.Warn("no admin password configured — setup wizard will appear on first visit")
		}
	}

	// Determine setup state
	s.determineSetupState(ctx)

	// Initialize WebSocket hub
	s.Hub = websocket.NewHub()
	go s.Hub.Run()

	// Initialize event generator and notification dispatcher
	s.Dispatcher = notify.NewDispatcher(s.NotifyRepo)
	s.EventGen = events.NewGenerator(s.EventRepo, s.Hub, s.AlertRuleRepo, s.Dispatcher, s.CommandRepo)

	// Initialize probe runner
	s.ProbeRunner = probes.NewRunner(s.ProbeRepo, s.EventRepo, s.Hub)

	// Load or create CA for mTLS
	if s.Config.MTLSEnabled {
		if err := s.loadOrCreateCA(ctx); err != nil {
			return fmt.Errorf("mTLS CA: %w", err)
		}
	}

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
	s.router = router
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
	s.done = make(chan os.Signal, 1)
	signal.Notify(s.done, os.Interrupt, syscall.SIGTERM)

	go func() {
		if s.Config.TLSEnabled {
			slog.Info("server starting with TLS", "port", s.Config.Port)
			var err error
			if s.Config.TLSDomain != "" {
				// autocert handles cert files
				err = s.httpServer.ListenAndServeTLS("", "")
			} else if s.Config.TLSCertFile != "" && s.Config.TLSKeyFile != "" {
				err = s.httpServer.ListenAndServeTLS(s.Config.TLSCertFile, s.Config.TLSKeyFile)
			} else {
				// DB-stored cert: TLSConfig already configured with cert
				err = s.httpServer.ListenAndServeTLS("", "")
			}
			if err != nil && err != http.ErrServerClosed {
				slog.Error("server error", "error", err)
				os.Exit(1)
			}
		} else {
			if !s.SetupComplete.Load() {
				slog.Info("server starting in setup mode (no TLS)", "port", s.Config.Port)
			} else {
				slog.Info("server starting", "port", s.Config.Port)
			}
			if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				slog.Error("server error", "error", err)
				os.Exit(1)
			}
		}
	}()

	<-s.done
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

// loadDBConfig loads configuration from the database, allowing env vars to override.
func (s *Server) loadDBConfig(ctx context.Context) {
	keys := []string{"jwt_secret", "tls_enabled", "tls_mode", "tls_domain", "mtls_enabled", "registration_key"}
	dbCfg, err := s.AdminRepo.GetConfigMap(ctx, keys)
	if err != nil {
		slog.Debug("could not load config from DB", "error", err)
		return
	}

	// JWT secret: env var overrides DB
	if s.Config.JWTSecret == "" || isRandomJWTSecret(s.Config.JWTSecret) {
		if v := dbCfg["jwt_secret"]; v != "" {
			s.Config.JWTSecret = v
			slog.Info("loaded JWT secret from database")
		}
	}

	// TLS: env vars override DB
	if os.Getenv("RIOT_TLS_ENABLED") == "" && os.Getenv("RIOT_TLS_CERT_FILE") == "" && os.Getenv("RIOT_TLS_DOMAIN") == "" {
		if dbCfg["tls_enabled"] == "true" {
			s.Config.TLSEnabled = true
			s.Config.TLSMode = dbCfg["tls_mode"]
			if dbCfg["tls_domain"] != "" {
				s.Config.TLSDomain = dbCfg["tls_domain"]
			}

			// For self-signed mode, load cert from DB
			if dbCfg["tls_mode"] == "self-signed" {
				certPEM, keyPEM, err := s.AdminRepo.GetServerTLSCert(ctx)
				if err == nil && certPEM != "" && keyPEM != "" {
					slog.Info("loaded TLS certificate from database")
				}
			}
		}
	}

	// mTLS: env var overrides DB
	if os.Getenv("RIOT_MTLS_ENABLED") == "" {
		if dbCfg["mtls_enabled"] == "true" {
			s.Config.MTLSEnabled = true
		}
	}

	// Registration key: env var overrides DB
	if os.Getenv("RIOT_API_KEY") == "" {
		if v := dbCfg["registration_key"]; v != "" {
			s.Config.RegistrationKey = v
		}
	}
}

// isRandomJWTSecret checks if the JWT secret appears to be auto-generated
// (64 hex chars = 32 random bytes). This is a heuristic.
func isRandomJWTSecret(s string) bool {
	if len(s) != 64 {
		return false
	}
	for _, c := range s {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return false
		}
	}
	return true
}

// determineSetupState checks if setup is complete and sets the atomic flag.
func (s *Server) determineSetupState(ctx context.Context) {
	// If env var password is set, auto-mark setup as complete (backwards compat)
	if s.Config.AdminPasswordHash != "" {
		s.SetupComplete.Store(true)
		s.Config.SetupComplete = true
		// Also persist to DB so future starts without env var still work
		s.AdminRepo.SetConfig(ctx, "setup_complete", "true")
		return
	}

	// Check DB
	complete, _ := s.AdminRepo.IsSetupComplete(ctx)
	if complete {
		s.SetupComplete.Store(true)
		s.Config.SetupComplete = true
		return
	}

	// Check if password exists in DB (legacy setup)
	hash, err := s.AdminRepo.GetPasswordHash(ctx)
	if err == nil && hash != "" {
		s.SetupComplete.Store(true)
		s.Config.SetupComplete = true
		s.AdminRepo.SetConfig(ctx, "setup_complete", "true")
		return
	}

	// Setup not complete — wizard mode
	s.SetupComplete.Store(false)
	s.Config.SetupComplete = false
	slog.Info("setup not complete — wizard will be shown on first visit")
}

// applyTLSAndRestart shuts down the current HTTP listener and restarts with TLS.
// Called after setup wizard completes.
func (s *Server) applyTLSAndRestart() {
	ctx := context.Background()

	// Reload config from DB
	s.loadDBConfig(ctx)
	s.JWTSecret = []byte(s.Config.JWTSecret)
	s.SetupComplete.Store(true)
	s.Config.SetupComplete = true

	// Load mTLS CA if now enabled
	if s.Config.MTLSEnabled && s.CA == nil {
		if err := s.loadOrCreateCA(ctx); err != nil {
			slog.Error("post-setup: failed to load/create CA", "error", err)
		}
	}

	// Rebuild router so mTLS enrollment routes are registered
	s.router = s.setupRouter()

	if !s.Config.TLSEnabled {
		slog.Info("setup complete, TLS not enabled — continuing on HTTP")
		// Update the running server's handler to use the rebuilt router
		s.httpServer.Handler = s.router
		return
	}

	slog.Info("setup complete — restarting with TLS")

	// Shut down current server
	shutdownCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	s.httpServer.Shutdown(shutdownCtx)

	// Reconfigure with TLS
	s.httpServer = &http.Server{
		Addr:        fmt.Sprintf(":%d", s.Config.Port),
		Handler:     s.router,
		ReadTimeout: 15 * time.Second,
		IdleTimeout: 60 * time.Second,
	}

	if err := s.configureTLS(); err != nil {
		slog.Error("post-setup: TLS configuration failed", "error", err)
		// Fall back to plain HTTP
		go func() {
			slog.Info("falling back to HTTP", "port", s.Config.Port)
			if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				slog.Error("server error", "error", err)
			}
		}()
		return
	}

	go func() {
		slog.Info("server restarting with TLS", "port", s.Config.Port)
		var err error
		if s.Config.TLSDomain != "" {
			err = s.httpServer.ListenAndServeTLS("", "")
		} else {
			err = s.httpServer.ListenAndServeTLS("", "")
		}
		if err != nil && err != http.ErrServerClosed {
			slog.Error("server error after TLS restart", "error", err)
		}
	}()
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

	logDeleted, err := s.LogRepo.Purge(ctx, now.AddDate(0, 0, -7))
	if err != nil {
		slog.Error("purge server logs failed", "error", err)
	} else if logDeleted > 0 {
		slog.Info("purged old server logs", "count", logDeleted)
	}

	cmDeleted, err := s.ContainerMetricRepo.Purge(ctx, now.AddDate(0, 0, -7))
	if err != nil {
		slog.Error("purge container metrics failed", "error", err)
	} else if cmDeleted > 0 {
		slog.Info("purged old container metrics", "count", cmDeleted)
	}

	dpDeleted, err := s.DeviceProbeRepo.PurgeResults(ctx, now.AddDate(0, 0, -s.Config.RetentionDays))
	if err != nil {
		slog.Error("purge device probe results failed", "error", err)
	} else if dpDeleted > 0 {
		slog.Info("purged old device probe results", "count", dpDeleted)
	}

	dlDeleted, err := s.DeviceLogRepo.Purge(ctx, now.AddDate(0, 0, -s.Config.RetentionDays))
	if err != nil {
		slog.Error("purge device logs failed", "error", err)
	} else if dlDeleted > 0 {
		slog.Info("purged old device logs", "count", dlDeleted)
	}

	bkDeleted, err := s.CARepo.PurgeStaleBootstrapKeys(ctx)
	if err != nil {
		slog.Error("purge stale bootstrap keys failed", "error", err)
	} else if bkDeleted > 0 {
		slog.Info("purged stale bootstrap keys", "count", bkDeleted)
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

	// Manual certificate from files
	if s.Config.TLSCertFile != "" && s.Config.TLSKeyFile != "" {
		cert, err := tls.LoadX509KeyPair(s.Config.TLSCertFile, s.Config.TLSKeyFile)
		if err != nil {
			return fmt.Errorf("load cert: %w", err)
		}
		tlsCfg := &tls.Config{
			Certificates: []tls.Certificate{cert},
		}
		s.addMTLSConfig(tlsCfg)
		s.httpServer.TLSConfig = tlsCfg
		slog.Info("TLS configured with manual certificate", "cert", s.Config.TLSCertFile)
		return nil
	}

	// Self-signed certificate from database
	if s.Config.TLSMode == "self-signed" {
		ctx := context.Background()
		certPEM, keyPEM, err := s.AdminRepo.GetServerTLSCert(ctx)
		if err != nil || certPEM == "" || keyPEM == "" {
			return fmt.Errorf("TLS enabled (self-signed) but no certificate found in database")
		}
		cert, err := tls.X509KeyPair([]byte(certPEM), []byte(keyPEM))
		if err != nil {
			return fmt.Errorf("parse DB cert: %w", err)
		}
		tlsCfg := &tls.Config{
			Certificates: []tls.Certificate{cert},
		}
		s.addMTLSConfig(tlsCfg)
		s.httpServer.TLSConfig = tlsCfg
		slog.Info("TLS configured with self-signed certificate from database")
		return nil
	}

	return fmt.Errorf("TLS enabled but no domain, cert files, or self-signed cert configured")
}

// addMTLSConfig adds mTLS client cert verification to the TLS config if enabled.
func (s *Server) addMTLSConfig(tlsCfg *tls.Config) {
	if s.Config.MTLSEnabled && s.CA != nil {
		clientCAs := x509.NewCertPool()
		clientCAs.AddCert(s.CA.Cert())
		tlsCfg.ClientCAs = clientCAs
		tlsCfg.ClientAuth = tls.VerifyClientCertIfGiven
		slog.Info("mTLS client certificate verification enabled")
	}
}

// loadOrCreateCA loads an existing CA from the database or generates a new one.
func (s *Server) loadOrCreateCA(ctx context.Context) error {
	certPEM, keyPEM, err := s.CARepo.GetCA(ctx)
	if err == nil && certPEM != "" {
		loaded, err := ca.LoadCA([]byte(certPEM), []byte(keyPEM))
		if err != nil {
			return fmt.Errorf("load CA: %w", err)
		}
		s.CA = loaded
		slog.Info("loaded existing CA from database")
		return nil
	}

	// Generate new CA
	newCA, err := ca.NewCA()
	if err != nil {
		return fmt.Errorf("generate CA: %w", err)
	}
	if err := s.CARepo.StoreCA(ctx, string(newCA.CertPEM()), string(newCA.KeyPEM())); err != nil {
		return fmt.Errorf("store CA: %w", err)
	}
	s.CA = newCA
	slog.Info("generated new CA and stored in database")
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
		{Name: "High Memory Usage", Enabled: true, Metric: "mem_percent", Operator: ">", Threshold: 90, Severity: "warning", CooldownSeconds: 3600, Notify: true, TemplateID: "high_memory"},
		{Name: "High Disk Usage", Enabled: true, Metric: "disk_percent", Operator: ">", Threshold: 90, Severity: "critical", CooldownSeconds: 3600, Notify: true, TemplateID: "high_disk"},
		{Name: "Updates Available", Enabled: true, Metric: "updates", Operator: ">", Threshold: 0, Severity: "info", CooldownSeconds: 86400, Notify: true},
		{Name: "Container Died", Enabled: true, Metric: "container_died", Operator: "==", Threshold: 1, Severity: "warning", CooldownSeconds: 900, Notify: true, TemplateID: "container_died"},
		{Name: "Container OOM Killed", Enabled: true, Metric: "container_oom", Operator: "==", Threshold: 1, Severity: "critical", CooldownSeconds: 900, Notify: true, TemplateID: "container_oom"},
		{Name: "Device Offline", Enabled: true, Metric: "device_offline", Operator: "==", Threshold: 1, Severity: "warning", CooldownSeconds: 300, Notify: true, TemplateID: "device_offline"},
	}
	for _, rule := range defaults {
		if err := s.AlertRuleRepo.Create(ctx, &rule); err != nil {
			slog.Error("seed alert rule", "name", rule.Name, "error", err)
		}
	}
	slog.Info("seeded default alert rules", "count", len(defaults))
}
