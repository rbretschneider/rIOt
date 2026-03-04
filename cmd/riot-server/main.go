package main

import (
	"embed"
	"io/fs"
	"log"

	"github.com/DesyncTheThird/rIOt/internal/server"
)

//go:embed all:migrations
var migrationsFS embed.FS

// Frontend is embedded via frontend_embed.go (built with make build-server)
// If not available, the server runs without embedded frontend (dev mode).

var version = "dev"

func main() {
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
