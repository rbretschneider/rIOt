package db

import (
	"context"
	"errors"
	"fmt"

	"github.com/DesyncTheThird/rIOt/internal/models"
	"github.com/DesyncTheThird/rIOt/internal/redact"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// EXPECT-008: persistence of installer reinstall records. A record is a commands
// row with origin installer, created directly in running (migration 000035). All
// of the methods are on CommandRepo.
//
// Bounds (AD-012): at most one running record per device (the unique partial
// index idx_commands_reinstall_open), at most models.ReinstallRateCap created
// per device per models.ReinstallRateWindow (checked inside CreateReinstall), at
// most one rate-cap event per device per 24 h. Rows leave with the command
// retention (90 days). A refused or rejected notice stores no row and no event.

var (
	// reinstallOpenStatus is the one open state of a record.
	reinstallOpenStatus = "'" + string(models.CommandRunning) + "'"
	// reinstallOriginLiteral is the typed constant rendered for a SQL literal; it
	// is never request data.
	reinstallOriginLiteral = "'" + models.OriginInstaller + "'"
)

// CreateReinstall recognises one authenticated notice in a single transaction
// under the per-device command advisory lock (the key CreateIfNoConflict and
// CreateSelfUpdate use), so it is serialised with every command creation on the
// device and 50 concurrent notices produce one row (AD-004):
//
//  1. FR-008 a / DEC-4: a device that is not live on the server gets no record;
//  2. NFR-004: a still-open record whose bound passed more than 1 hour ago is
//     retired (timed_out / unresolved) through the ActorReinstall SQL builder;
//  3. FR-009: an open record absorbs the notice;
//  4. FR-010: the 24 h cap, with one rate-cap warning event per device per 24 h;
//  5. the insert (AD-003).
func (r *CommandRepo) CreateReinstall(ctx context.Context, n models.ReinstallNotice) (models.ReinstallCreateResult, error) {
	res, err := r.createReinstall(ctx, n)
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		// The unique index fired (a concurrent writer that did not take the
		// advisory lock): the device has an open record now.
		open, oerr := r.OpenReinstall(ctx, n.DeviceID)
		if oerr == nil && open != nil {
			return models.ReinstallCreateResult{Disposition: models.ReinstallOpenRecord}, nil
		}
	}
	return res, err
}

func (r *CommandRepo) createReinstall(ctx context.Context, n models.ReinstallNotice) (models.ReinstallCreateResult, error) {
	var res models.ReinstallCreateResult
	tx, err := r.db.Pool.Begin(ctx)
	if err != nil {
		return res, fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx,
		`SELECT pg_advisory_xact_lock(hashtextextended('riot.cmd.' || $1::text, 0))`, n.DeviceID); err != nil {
		return res, fmt.Errorf("lock device commands: %w", err)
	}

	// 1. FR-008 a, DEC-4. No row for the device is the same as offline: nothing to
	// suppress, and no record for a device the server does not know.
	var status string
	err = tx.QueryRow(ctx, `SELECT status FROM devices WHERE id=$1`, n.DeviceID).Scan(&status)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		res.Disposition = models.ReinstallDeviceOffline
		return res, tx.Commit(ctx)
	case err != nil:
		return res, fmt.Errorf("read device status: %w", err)
	}
	if !models.DeviceIsLive(models.DeviceStatus(status)) {
		res.Disposition = models.ReinstallDeviceOffline
		return res, tx.Commit(ctx)
	}

	// 2. NFR-004: retire a stale open record in this transaction.
	open, err := reinstallOpen(ctx, tx, n.DeviceID)
	if err != nil {
		return res, err
	}
	if open != nil && open.DeadlineAt != nil && open.DeadlineAt.Before(n.At.Add(-models.ReinstallStuckAfter)) {
		msg, step := models.ReinstallStaleResultMessage, models.ReinstallFailureUnresolved
		query, args, ok := buildTransitionSQL(models.CommandTransition{
			ID: open.ID, DeviceID: n.DeviceID, Actor: models.ActorReinstall, To: models.CommandTimedOut,
			ResultMsg: &msg, FailureStep: &step, Now: n.At,
		})
		if ok {
			retired, rerr := scanCommand(func(dest ...interface{}) error { return tx.QueryRow(ctx, query, args...).Scan(dest...) })
			switch {
			case rerr == nil:
				res.Retired = &retired
				open = nil
			case !errors.Is(rerr, pgx.ErrNoRows):
				return res, fmt.Errorf("retire stale record: %w", rerr)
			}
		}
		if res.Retired == nil {
			// Resolved by someone else in the meantime: read the current state.
			if open, err = reinstallOpen(ctx, tx, n.DeviceID); err != nil {
				return res, err
			}
		}
	}

	// 3. FR-009: the open record absorbs the notice; the window is not extended.
	if open != nil {
		res.Disposition = models.ReinstallOpenRecord
		return res, tx.Commit(ctx)
	}

	// 4. FR-010.
	var count int
	if err := tx.QueryRow(ctx,
		`SELECT count(*) FROM commands WHERE device_id=$1 AND origin=`+reinstallOriginLiteral+` AND created_at > $2`,
		n.DeviceID, n.At.Add(-models.ReinstallRateWindow)).Scan(&count); err != nil {
		return res, fmt.Errorf("count records: %w", err)
	}
	if count >= models.ReinstallRateCap {
		res.Disposition = models.ReinstallCapped
		var seen bool
		// Only a rate-cap event (not linked to a record) counts: a stuck-record
		// event of the same type must not silence the cap warning.
		if err := tx.QueryRow(ctx,
			`SELECT EXISTS (SELECT 1 FROM events WHERE device_id=$1 AND type=$2 AND command_id='' AND created_at > $3)`,
			n.DeviceID, string(models.EventAgentReinstallPaused), n.At.Add(-models.ReinstallRateWindow)).Scan(&seen); err != nil {
			return res, fmt.Errorf("check rate-cap event: %w", err)
		}
		if !seen {
			msg, _ := redact.MaskLine(models.TruncateHead(models.SanitizeText(models.ReinstallRateCapMessage(n.Hostname)), 2048))
			evt := models.Event{DeviceID: n.DeviceID, Type: models.EventAgentReinstallPaused, Severity: models.SeverityWarning,
				Message: msg, CreatedAt: n.At}
			if err := tx.QueryRow(ctx,
				`INSERT INTO events (device_id, type, severity, message, container_id, container_name, created_at, command_id, command_attr)
				 VALUES ($1, $2, $3, $4, '', '', $5, '', '') RETURNING id`,
				evt.DeviceID, evt.Type, evt.Severity, evt.Message, evt.CreatedAt).Scan(&evt.ID); err != nil {
				return res, fmt.Errorf("insert rate-cap event: %w", err)
			}
			res.CapEvent = &evt
		}
		return res, tx.Commit(ctx)
	}

	// 5. The insert. Every column the shape CHECK constrains is set here; no
	// docker_audit row is written and params stay '{}' (AD-003).
	id := uuid.New().String()
	deadline := n.At.Add(models.ExpectReinstallBound)
	if _, err := tx.Exec(ctx,
		`INSERT INTO commands (id, device_id, action, params, status, result_msg, created_at, updated_at,
			expires_at, exec_deadline_s, deadline_at, dispatched_at, started_at, step, origin, policy_id,
			target_keys, actor_kind, actor_auth, actor_name, actor_sub)
		 VALUES ($1, $2, $3, '{}'::jsonb, $4, '', $5, $5, $5, $6, $7, $5, $5, $8, $9, NULL, '{}', 'system', '', '', '')`,
		id, n.DeviceID, models.ActionAgentUpdate, string(models.CommandRunning), n.At,
		models.ReinstallExecDeadlineS, deadline, models.ReinstallStepWaitingForRestart, models.OriginInstaller); err != nil {
		return res, fmt.Errorf("insert reinstall record: %w", err)
	}
	created, err := scanCommand(func(dest ...interface{}) error {
		return tx.QueryRow(ctx, `SELECT `+commandColumns+` FROM commands WHERE id=$1`, id).Scan(dest...)
	})
	if err != nil {
		return res, fmt.Errorf("read reinstall record: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return res, fmt.Errorf("commit: %w", err)
	}
	res.Disposition, res.Command = models.ReinstallCreated, &created
	return res, nil
}

// reinstallOpen reads the device's open record, or nil when there is none.
func reinstallOpen(ctx context.Context, q selfUpdateQuerier, deviceID string) (*models.Command, error) {
	cmd, err := scanCommand(func(dest ...interface{}) error {
		return q.QueryRow(ctx,
			`SELECT `+commandColumns+` FROM commands
			 WHERE device_id=$1 AND origin=`+reinstallOriginLiteral+` AND status=`+reinstallOpenStatus+`
			 ORDER BY created_at DESC LIMIT 1`, deviceID).Scan(dest...)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read open reinstall record: %w", err)
	}
	return &cmd, nil
}

// OpenReinstall returns the device's open (running) record, or nil when it has
// none. The unique index guarantees at most one.
func (r *CommandRepo) OpenReinstall(ctx context.Context, deviceID string) (*models.Command, error) {
	return reinstallOpen(ctx, r.db.Pool, deviceID)
}
