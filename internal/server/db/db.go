package db

import (
	"context"
	"fmt"
	"io/fs"
	"time"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/jackc/pgx/v5/pgxpool"
)

// DB wraps a pgx connection pool.
type DB struct {
	Pool *pgxpool.Pool
}

// New creates a new database connection pool with tuned settings.
func New(ctx context.Context, connStr string) (*DB, error) {
	config, err := pgxpool.ParseConfig(connStr)
	if err != nil {
		return nil, fmt.Errorf("parse database config: %w", err)
	}

	// Pool tuning: prevent excessive connections while keeping enough for
	// concurrent telemetry ingestion + dashboard queries.
	config.MaxConns = 20
	config.MinConns = 2
	config.MaxConnLifetime = 30 * time.Minute
	config.MaxConnIdleTime = 5 * time.Minute
	config.HealthCheckPeriod = 1 * time.Minute

	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("connect to database: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}
	return &DB{Pool: pool}, nil
}

// RunMigrations applies all pending database migrations.
func (d *DB) RunMigrations(migrationsFS fs.FS, connStr string) error {
	source, err := iofs.New(migrationsFS, ".")
	if err != nil {
		return fmt.Errorf("create migration source: %w", err)
	}
	m, err := migrate.NewWithSourceInstance("iofs", source, connStr)
	if err != nil {
		return fmt.Errorf("create migrator: %w", err)
	}
	if err := m.Up(); err != nil && err != migrate.ErrNoChange {
		return fmt.Errorf("run migrations: %w", err)
	}
	return nil
}

// Close shuts down the connection pool.
func (d *DB) Close() {
	d.Pool.Close()
}

// Healthy checks if the database connection is alive.
func (d *DB) Healthy(ctx context.Context) bool {
	return d.Pool.Ping(ctx) == nil
}
