package server

import (
	"context"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/DesyncTheThird/rIOt/internal/server/db"
	"github.com/DesyncTheThird/rIOt/internal/server/events"
	"github.com/DesyncTheThird/rIOt/internal/server/websocket"
)

type Server struct {
	Config        *Config
	MigrationsFS  fs.FS
	FrontendFS    fs.FS
	DB            *db.DB
	DeviceRepo    *db.DeviceRepo
	TelemetryRepo *db.TelemetryRepo
	EventRepo     *db.EventRepo
	Hub           *websocket.Hub
	EventGen      *events.Generator
	httpServer    *http.Server
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

	// Initialize WebSocket hub
	s.Hub = websocket.NewHub()
	go s.Hub.Run()

	// Initialize event generator
	s.EventGen = events.NewGenerator(s.EventRepo, s.Hub)

	// Start retention worker
	go s.retentionWorker(ctx)

	// Start offline detection worker
	go s.offlineDetectionWorker(ctx)

	// Set up HTTP server
	router := s.setupRouter()
	s.httpServer = &http.Server{
		Addr:         fmt.Sprintf(":%d", s.Config.Port),
		Handler:      router,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Graceful shutdown
	done := make(chan os.Signal, 1)
	signal.Notify(done, os.Interrupt, syscall.SIGTERM)

	go func() {
		slog.Info("server starting", "port", s.Config.Port)
		if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("server error", "error", err)
			os.Exit(1)
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
