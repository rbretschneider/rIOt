package main

import (
	"embed"
	"io/fs"
	"log"
	"os"
	"runtime/debug"

	"github.com/DesyncTheThird/rIOt/internal/server"
)

//go:embed all:migrations
var migrationsFS embed.FS

// Frontend is embedded via frontend_embed.go (built with make build-server)
// If not available, the server runs without embedded frontend (dev mode).

var version = "dev"

func main() {
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
