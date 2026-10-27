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
	"github.com/DesyncTheThird/rIOt/internal/logbudget"
	"github.com/DesyncTheThird/rIOt/internal/server/ca"
	"github.com/DesyncTheThird/rIOt/internal/server/commands"
	"github.com/DesyncTheThird/rIOt/internal/server/db"
	"github.com/DesyncTheThird/rIOt/internal/server/events"
	"github.com/DesyncTheThird/rIOt/internal/server/expect"
	"github.com/DesyncTheThird/rIOt/internal/server/handlers"
	"github.com/DesyncTheThird/rIOt/internal/server/logstore"
	"github.com/DesyncTheThird/rIOt/internal/server/notify"
	"github.com/DesyncTheThird/rIOt/internal/server/partition"
	"github.com/DesyncTheThird/rIOt/internal/server/probes"
	"github.com/DesyncTheThird/rIOt/internal/server/quota"
	"github.com/DesyncTheThird/rIOt/internal/server/storage"
	"github.com/DesyncTheThird/rIOt/internal/server/updates"
	"github.com/DesyncTheThird/rIOt/internal/server/websocket"
	"golang.org/x/crypto/acme/autocert"
)

type Server struct {
	Config               *Config
	Version              string
	MigrationsFS         fs.FS
	FrontendFS           fs.FS
	DB                   *db.DB
	DeviceRepo           *db.DeviceRepo
	TelemetryRepo        *db.TelemetryRepo
	EventRepo            *db.EventRepo
	AdminRepo            *db.AdminRepo
	TerminalRepo         *db.TerminalRepo
	AlertRuleRepo        *db.AlertRuleRepo
	NotifyRepo           *db.NotifyRepo
	CommandRepo          *db.CommandRepo
	Commands             *commands.Service
	ProbeRepo            *db.ProbeRepo
	CARepo               *db.CARepo
	LogRepo              *db.LogRepo
	DeviceLogRepo        *db.DeviceLogRepo
	AutoUpdateRepo       *db.AutoUpdateRepo
	ContainerLogRepo     *db.ContainerLogRepo
	ContainerMetricRepo  *db.ContainerMetricRepo
	DeviceProbeRepo      *db.DeviceProbeRepo
	ExternalIdentityRepo *db.ExternalIdentityRepo
	LogHandler           *logstore.DBHandler
	CA                   *ca.CA
	ProbeRunner          *probes.Runner
	Hub                  *websocket.Hub
	EventGen             *events.Generator
	// Expect derives the expected-activity windows from the commands and tells
	// the event generator what is expected (EXPECT-001). It is built once in
	// Start so it survives a router rebuild.
	Expect     *expect.Tracker
	Dispatcher *notify.Dispatcher
	UpdateChecker        *updates.Checker
	// PartitionManager pre-creates upcoming daily partitions and drops
	// aged-out ones (including the legacy partition) for the six high-churn
	// tables (PART-001, AD-005). EnsureAll runs synchronously before the
	// server serves traffic; Run(ctx) then loops hourly.
	PartitionManager *partition.Manager
	StorageQuota     *quota.Manager
	StorageGuard     *storage.Guard
	JWTSecret        []byte
	SetupComplete    atomic.Bool
	// MigrationsComplete is set once schema migrations have finished in this
	// process (or there are none to run). /readyz reports it (HARDEN-002 AD-011).
	MigrationsComplete atomic.Bool
	httpServer         *http.Server
	pprof              *pprofServer
	listener           net.Listener
	router             http.Handler
	done               chan os.Signal
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
	s.MigrationsComplete.Store(true)

	// Initialize repos
	s.DeviceRepo = db.NewDeviceRepo(s.DB)
	s.DeviceRepo.SetRegistrationLimits(s.Config.MaxDevices,s.Config.RegistrationsPerDay)
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
	s.ContainerLogRepo = db.NewContainerLogRepo(s.DB)
	s.StorageQuota = quota.New(db.NewUsageRepo(s.DB))
	if err := s.StorageQuota.Seed(ctx); err != nil {
		return fmt.Errorf("load storage quotas: %w", err)
	}
	s.ContainerMetricRepo = db.NewContainerMetricRepo(s.DB)
	s.DeviceProbeRepo = db.NewDeviceProbeRepo(s.DB)
	s.ExternalIdentityRepo = db.NewExternalIdentityRepo(s.DB)

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
	slog.SetDefault(slog.New(logbudget.New(multiHandler,1000)))

	// Build the partition manager and ensure forward partition coverage for
	// the six high-churn tables BEFORE the router accepts any insert
	// (PART-001, AD-005/§7-B). This is an optimization, not a correctness
	// gate: the forward DEFAULT catch partition created by the conversion
	// migration already guarantees every insertable row has a home
	// (AD-008), so a failure here is logged (through the DB log handler
	// just configured above, so a persistent stall is visible to the
	// operator) and does not abort boot.
	specs := partition.BuildSpecs(s.Config.RetentionDays)
	for i := range specs {
		if specs[i].Name == "container_logs" {
			specs[i].Retention = time.Duration(s.EffectiveContainerLogRetention()) * 24 * time.Hour
		}
	}
	s.PartitionManager = partition.NewManager(s.DB.Pool, specs, slog.Default())
	if err := s.PartitionManager.StartupEnsure(ctx); err != nil {
		slog.Error("partition manager: initial EnsureAll failed", "error", err.Error())
	}
	s.StorageGuard = storage.New(s.DB.Pool, s.PartitionManager, s.StorageQuota, storage.Options{ProbePath: s.Config.DBFSProbePath, DBMaxGB: s.Config.DBMaxGB, LowGB: s.Config.DBFreeLowGB, EmergencyGB: s.Config.DBFreeEmergencyGB, LogBudgetGB: s.Config.ContainerLogMaxGB, CommandBudgetGB: s.Config.CommandResultsMaxGB, RetentionDays: s.EffectiveContainerLogRetention()})
	if s.Config.DBFSProbePath == "" && s.Config.DBMaxGB == 0 {
		slog.Info("database disk headroom checks disabled; storage budgets and quotas remain active")
	}

	// Load config from DB, with env vars taking precedence
	s.loadDBConfig(ctx)

	// Store JWT secret
	s.JWTSecret = []byte(s.Config.JWTSecret)

	// Set up admin password
	if s.Config.AdminPasswordHash != "" {
		if err := s.AdminRepo.SetPasswordHash(ctx, s.Config.AdminPasswordHash); err != nil {
			slog.Error("failed to store admin password hash", "error", err.Error())
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
	s.EventGen.StartCleanup(ctx)

	// Offline detection pauses after a start (EXPECT-001 AD-008, C-7). The end of
	// the pause is computed before any worker runs, from the durable restart
	// series, and bounds a restart loop to 4 minutes without detection.
	graceEnd := restartSeriesGrace(ctx, s.AdminRepo, time.Now().UTC(), slog.Default())

	// The expected-activity tracker (EXPECT-001, AD-002). Its readiness is the
	// fail-open guard: until Load succeeds every lookup says "undecidable" and the
	// generator alerts as it did before the feature.
	s.Expect = expect.New(expect.Options{
		Repo:        db.NewExpectRepo(s.DB),
		Devices:     s.DeviceRepo,
		Policies:    s.AutoUpdateRepo,
		Telemetry:   s.TelemetryRepo,
		Commands:    s.CommandRepo,
		Escalator:   s.EventGen,
		Broadcaster: s.Hub,
		NewestAgentVersion: func() string {
			if s.UpdateChecker == nil {
				return ""
			}
			return s.UpdateChecker.ServerUpdateInfo().LatestVersion
		},
		GraceEnd: graceEnd,
		Logger:   slog.Default(),
	})
	s.EventGen.SetExpectedActivity(s.Expect)

	// The command service is built once here (not in setupRouter, which
	// applyTLSAndRestart calls again) so its limiter, progress gate and log
	// throttle survive a router rebuild. The sweeper resolves stale commands at
	// startup and then every minute.
	s.Commands = commands.New(commands.Options{
		Quota:       s.StorageQuota,
		Commands:    s.CommandRepo,
		Policies:    s.AutoUpdateRepo,
		Telemetry:   s.TelemetryRepo,
		Agents:      handlers.Agents(),
		Broadcaster: s.Hub,
		Events:      handlers.NewCompletionEvents(s.EventGen, s.DeviceRepo),
		Observer:    s.Expect,
		// EXPECT-002: agent self-update records live in the commands table.
		SelfUpdates: s.CommandRepo,
		// EXPECT-008: installer reinstall records live in the commands table too.
		Reinstalls: s.CommandRepo,
	})
	// The tracker resolves a self-update record at its bound through the service,
	// so the resolver is wired before Load (and never a typed nil).
	s.Expect.SetSelfUpdateResolver(s.Commands)
	s.Expect.SetReinstallResolver(s.Commands)
	if err := s.Expect.Load(ctx); err != nil {
		slog.Warn("expected-activity load failed; alerting as usual until it succeeds", "error", err.Error())
	}
	go s.runCommandSweeper(ctx)

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

	// Start the expected-activity loop (1 s evaluation, 30 s reconcile on its own
	// goroutine, bounded publisher). After the update checker, whose latest
	// version the activity reports.
	go s.Expect.Run(ctx)

	// Start retention worker
	go s.retentionWorker(ctx)

	// Start partition manager loop (hourly create-ahead + drop-aged, incl.
	// the legacy partition). EnsureAll already ran synchronously above;
	// this is the steady-state schedule (PART-001, AD-005).
	go s.PartitionManager.Run(ctx)
	go s.storageQuotaWorker(ctx)

	// Start offline detection worker: paused until graceEnd, then every 30 s, with
	// a one-shot check at graceEnd itself.
	detector := &offlineDetector{
		devices: s.DeviceRepo, events: s.EventGen, observer: s.Expect, graceEnd: graceEnd,
		now: func() time.Time { return time.Now().UTC() },
	}
	go detector.run(ctx)

	// Start server-error-rate alert worker — notifies the user when the server
	// itself starts logging too many errors in a short window.
	go newServerErrorAlertWorker(s.AdminRepo, s.LogRepo, s.NotifyRepo, s.Dispatcher).run(ctx)

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

	// HARDEN-002 AD-002: opt-in loopback-only pprof listener. Returns nil
	// (and logs) when disabled, rejected or the bind fails; never aborts startup.
	s.pprof = startPprof(s.Config.PprofAddr, s.Config.Port, slog.Default())

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
				slog.Error("server error", "error", err.Error())
				os.Exit(1)
			}
		} else {
			if !s.SetupComplete.Load() {
				slog.Info("server starting in setup mode (no TLS)", "port", s.Config.Port)
			} else {
				slog.Info("server starting", "port", s.Config.Port)
			}
			if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				slog.Error("server error", "error", err.Error())
				os.Exit(1)
			}
		}
	}()

	<-s.done
	slog.Info("shutting down server")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Stop pprof first, and only log its failure: a slow /profile download must
	// never stop the main server from shutting down. A nil s.pprof is a no-op.
	if err := s.pprof.Shutdown(shutdownCtx); err != nil {
		slog.Warn("pprof shutdown", "error", err.Error())
	}
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
		slog.Debug("could not load config from DB", "error", err.Error())
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
			slog.Error("post-setup: failed to load/create CA", "error", err.Error())
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
		slog.Error("post-setup: TLS configuration failed", "error", err.Error())
		// Fall back to plain HTTP
		go func() {
			slog.Info("falling back to HTTP", "port", s.Config.Port)
			if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				slog.Error("server error", "error", err.Error())
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
			slog.Error("server error after TLS restart", "error", err.Error())
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

// runRetention purges the five out-of-scope tables only (events,
// notification log, server logs, device probe results, stale bootstrap
// keys). The six high-churn tables (heartbeats, telemetry_snapshots,
// device_logs, container_metrics, container_logs, probe_results) are no
// longer purged here — PART-001 replaced their row-level DELETE retention
// with drop-partition retention, handled entirely by PartitionManager
// (AD-006). Removing those six calls (and their now-orphaned repo methods)
// is deliberate: leaving them in would reintroduce the exact dead-tuple
// churn this story eliminates (NFR-004), and would double-retain against
// the partition manager's drop-based retention (AC-014).
func (s *Server) runRetention(ctx context.Context) {
	runRetention(ctx, s.Config, s.EventRepo, s.NotifyRepo, s.LogRepo, s.DeviceProbeRepo, s.CARepo)
	if err := db.NewDockerRepo(s.DB).Purge(ctx, time.Now().UTC()); err != nil {
		slog.Warn("Docker history retention failed")
	}
}

// runRetention is the package-level, interface-driven implementation so it
// can be unit-tested with mock repos and no live database (AC-014, AC-015).
// The concrete repo types on Server already satisfy these interfaces
// (compile-time-checked in internal/server/db/interfaces.go).
func runRetention(
	ctx context.Context,
	cfg *Config,
	eventRepo db.EventRepository,
	notifyRepo db.NotifyRepository,
	logRepo db.LogRepository,
	deviceProbeRepo db.DeviceProbeRepository,
	caRepo db.CARepository,
) {
	now := time.Now().UTC()

	evtDeleted, err := eventRepo.Purge(ctx, now.AddDate(0, 0, -90))
	if err != nil {
		slog.Error("purge events failed", "error", err.Error())
	} else if evtDeleted > 0 {
		slog.Info("purged old events", "count", evtDeleted)
	}

	nlDeleted, err := notifyRepo.PurgeNotificationLog(ctx, now.AddDate(0, 0, -90))
	if err != nil {
		slog.Error("purge notification log failed", "error", err.Error())
	} else if nlDeleted > 0 {
		slog.Info("purged old notification log entries", "count", nlDeleted)
	}

	logDeleted, err := logRepo.Purge(ctx, now.AddDate(0, 0, -7))
	if err != nil {
		slog.Error("purge server logs failed", "error", err.Error())
	} else if logDeleted > 0 {
		slog.Info("purged old server logs", "count", logDeleted)
	}

	dpDeleted, err := deviceProbeRepo.PurgeResults(ctx, now.AddDate(0, 0, -cfg.RetentionDays))
	if err != nil {
		slog.Error("purge device probe results failed", "error", err.Error())
	} else if dpDeleted > 0 {
		slog.Info("purged old device probe results", "count", dpDeleted)
	}

	bkDeleted, err := caRepo.PurgeStaleBootstrapKeys(ctx)
	if err != nil {
		slog.Error("purge stale bootstrap keys failed", "error", err.Error())
	} else if bkDeleted > 0 {
		slog.Info("purged stale bootstrap keys", "count", bkDeleted)
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
		slog.Error("seed alert rules: list", "error", err.Error())
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
		{Name: "Reboot Required", Enabled: true, Metric: "reboot_required", Operator: "==", Threshold: 1, Severity: "warning", CooldownSeconds: 86400, Notify: true, TemplateID: "reboot_required"},
	}
	for _, rule := range defaults {
		if err := s.AlertRuleRepo.Create(ctx, &rule); err != nil {
			slog.Error("seed alert rule", "name", rule.Name, "error", err.Error())
		}
	}
	slog.Info("seeded default alert rules", "count", len(defaults))
}

// runCommandSweeper runs the command sweeper until ctx ends: one pass at
// startup, then one per commands.SweepInterval.
func (s *Server) runCommandSweeper(ctx context.Context) {
	ticker := time.NewTicker(commands.SweepInterval)
	defer ticker.Stop()
	s.Commands.RunSweeper(ctx, ticker.C)
}
