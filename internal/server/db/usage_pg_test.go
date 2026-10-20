package db

import (
	"context"
	"testing"
	"time"

	"github.com/DesyncTheThird/rIOt/internal/models"
	"github.com/DesyncTheThird/rIOt/internal/server/quota"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
)

func TestDUX_AC164_QuotaLevelsSurviveRestart(t *testing.T) {
	e := newPGEnv(t, 0)
	e.seedDevice("quota-fixture")
	repo := NewUsageRepo(&DB{Pool: e.Pool})
	ctx := context.Background()
	now := time.Now().UTC()
	m := quota.New(repo)
	_, _ = m.CommandOutput("quota-fixture", "bytes stored", now)
	require.NoError(t, m.Flush(ctx, now))
	next := quota.New(repo)
	require.NoError(t, next.Seed(ctx))
	status := next.Status("quota-fixture", now)
	used := status["used"].(map[string]int64)
	require.Equal(t, int64(len("bytes stored")), used["commands"])
}

func TestDUX_AC171_ReadTxCannotHoldWriteLocks(t *testing.T) {
	e := newPGEnv(t, 0)
	database := &DB{Pool: e.Pool}
	err := database.ReadTx(context.Background(), func(tx pgx.Tx) error {
		_, err := tx.Exec(context.Background(), `INSERT INTO admin_config(key,value) VALUES('forbidden','write')`)
		return err
	})
	require.Error(t, err)
}

func TestDUX_D58_DeviceRegistrationCap(t *testing.T) {
	e := newPGEnv(t, 0)
	repo := NewDeviceRepo(&DB{Pool: e.Pool})
	repo.SetRegistrationLimits(1, 100)
	now := time.Now().UTC()
	ctx := context.Background()
	d := &models.Device{ID: "device-one", ShortID: "device", Hostname: "host-a", Tags: []string{}, CreatedAt: now, UpdatedAt: now}
	require.NoError(t, repo.Create(ctx, d))
	d.ID = "device-two"
	require.ErrorIs(t, repo.Create(ctx, d), ErrDeviceLimit)
}
