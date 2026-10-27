package main

import (
	"context"
	"embed"
	"io/fs"
	"log"
	"os"
	"runtime/debug"
	_ "time/tzdata" // fallback zone database for RIOT_DISPLAY_TZ on hosts without one (TIME-12H-001 AD-002)

	"github.com/DesyncTheThird/rIOt/internal/server"
	"github.com/DesyncTheThird/rIOt/internal/server/db"
)

//go:embed all:migrations
var migrationsFS embed.FS

// Frontend is embedded via frontend_embed.go (built with make build-server)
// If not available, the server runs without embedded frontend (dev mode).

var version = "dev"

func main() {
	if len(os.Args) > 1 && (os.Args[1] == "db-fingerprint" || os.Args[1] == "migrate") {
		mFS, err := fs.Sub(migrationsFS, "migrations")
		if err != nil {
			os.Exit(1)
		}
		url := os.Getenv("RIOT_DB_URL")
		if url == "" {
			url = "postgres://riot:riot@localhost:5432/riot?sslmode=disable"
		}
		os.Exit(db.Maintenance(context.Background(), os.Args[1], url, mFS, os.Stdout))
	}
	// Set a soft memory limit so Go's GC can batch collections efficiently.
	// Default 2 GiB — appropriate for telemetry workloads with dozens of devices
	// and 48+ containers. A too-low limit causes GC thrashing (90%+ CPU in
	// gcBgMarkWorker). Override with the GOMEMLIMIT env var.
	if os.Getenv("GOMEMLIMIT") == "" {
		debug.SetMemoryLimit(2 * 1024 * 1024 * 1024) // 2 GiB
	}

	cfg := server.LoadConfig()
	srv := server.New(cfg)
	srv.Version = version

	// Set up migrations FS
	mFS, err := fs.Sub(migrationsFS, "migrations")
	if err != nil {
		log.Fatalf("failed to load migrations: %v", err)
	}
	srv.MigrationsFS = mFS

	// Set up frontend FS if embedded
	if frontendFS != nil {
		distFS, err := fs.Sub(frontendFS, "dist")
		if err == nil {
			srv.FrontendFS = distFS
		}
	}

	log.Printf("rIOt server %s starting on port %d", version, cfg.Port)
	if err := srv.Start(); err != nil {
		log.Fatalf("server error: %v", err)
	}
}
