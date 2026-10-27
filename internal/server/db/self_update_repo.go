package db

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/DesyncTheThird/rIOt/internal/models"
	"github.com/DesyncTheThird/rIOt/internal/redact"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// EXPECT-002: persistence of agent self-update records. A record is a commands
// row with origin agent_self_update, created directly in running (migration
// 000034). All of the methods are on CommandRepo.
//
// Bounds (AD-013): at most one non-terminal record per device (the unique
// partial index idx_commands_self_update_open), at most
// models.SelfUpdateRateCap created per device per models.SelfUpdateRateWindow
// (checked inside CreateSelfUpdate), at most one rate-cap event per device per
// 24 h. Rows leave with the command retention (90 days).

var (
	// selfUpdateOpenStatusList is the open (non-terminal) states of a record.
	selfUpdateOpenStatusList = models.SQLStatusList(models.CommandRunning, models.CommandVerifying)
	// selfUpdateOriginLiteral is the typed constant rendered for a SQL literal;
	// it is never request data.
	selfUpdateOriginLiteral = "'" + models.OriginAgentSelfUpdate + "'"
)

// selfUpdateQuerier is the part of a pool or a transaction CreateSelfUpdate's
// reads and writes need.
type selfUpdateQuerier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// CreateSelfUpdate recognises one validated start in a single transaction under
// the per-device command advisory lock (the key CreateIfNoConflict uses), so it
// is serialised with every command creation on the device and 50 concurrent
// starts produce one row (AD-002):
//
//  0. EXPECT-007 FR-024: a start whose attempt ID a record of the device owns is
//     a repeat and only links to that record;
//  1. FR-004: attribution to an agent_update command that was dispatched and is
//     in flight, or finished at most 10 minutes before the start;
//  2. C-4 (b): a still-open record whose bound passed more than 1 hour ago is
//     retired (timed_out / unresolved) through the ActorSelfUpdate SQL builder;
//  3. FR-006: an open record absorbs the start;
//  4. FR-007: the 24 h cap, with one rate-cap warning event per device per 24 h;
//  5. the insert (AD-001).
func (r *CommandRepo) CreateSelfUpdate(ctx context.Context, s models.SelfUpdateStart) (models.SelfUpdateCreateResult, error) {
	res, err := r.createSelfUpdate(ctx, s)
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		// The unique index fired (a concurrent writer that did not take the
		// advisory lock): the device has an open record now.
		open, oerr := r.OpenSelfUpdate(ctx, s.DeviceID)
		if oerr == nil && open != nil {
			return models.SelfUpdateCreateResult{Disposition: models.SelfUpdateOpenRecord, LinkID: open.ID}, nil
		}
	}
	return res, err
}

func (r *CommandRepo) createSelfUpdate(ctx context.Context, s models.SelfUpdateStart) (models.SelfUpdateCreateResult, error) {
	var res models.SelfUpdateCreateResult
	tx, err := r.db.Pool.Begin(ctx)
	if err != nil {
		return res, fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx,
		`SELECT pg_advisory_xact_lock(hashtextextended('riot.cmd.' || $1::text, 0))`, s.DeviceID); err != nil {
		return res, fmt.Errorf("lock device commands: %w", err)
	}

	// 0. EXPECT-007 FR-024: a start whose attempt ID a record of this device
	// already owns (open or closed, any age) is a repeat. It creates nothing,
	// counts toward no cap, extends no window and retires nothing; the result
	// carries LinkID only, so the caller announces nothing (security condition
	// C-3 a). Under the advisory lock, concurrent starts with one attempt ID see
	// the first one's row and so produce one record (AD-006 step 0).
	if s.AttemptID != "" {
		var matchID string
		err = tx.QueryRow(ctx,
			`SELECT id FROM commands
			 WHERE device_id=$1 AND origin=`+selfUpdateOriginLiteral+` AND params->>'attempt_id' = $2
			 ORDER BY created_at DESC LIMIT 1`,
			s.DeviceID, s.AttemptID).Scan(&matchID)
		switch {
		case err == nil:
			res.Disposition, res.LinkID = models.SelfUpdateAttemptMatch, matchID
			return res, tx.Commit(ctx)
		case !errors.Is(err, pgx.ErrNoRows):
			return res, fmt.Errorf("match attempt: %w", err)
		}
	}

	// 1. FR-004. A queued command (dispatched_at NULL, also after a revert) never counts.
	var cmdID string
	err = tx.QueryRow(ctx,
		`SELECT id FROM commands
		 WHERE device_id=$1 AND action='agent_update' AND origin <> `+selfUpdateOriginLiteral+`
		   AND origin <> `+reinstallOriginLiteral+`
		   AND dispatched_at IS NOT NULL
		   AND (status IN (`+inflightStatusList+`) OR finished_at >= $2)
		 ORDER BY created_at DESC, id LIMIT 1`,
		s.DeviceID, s.At.Add(-models.SelfUpdateAttributionWindow)).Scan(&cmdID)
	switch {
	case err == nil:
		return models.SelfUpdateCreateResult{Disposition: models.SelfUpdateAttributedCommand, LinkID: cmdID}, nil
	case !errors.Is(err, pgx.ErrNoRows):
		return res, fmt.Errorf("attribute to command: %w", err)
	}

	// 2. C-4 (b): retire a stale open record in this transaction.
	open, err := selfUpdateOpen(ctx, tx, s.DeviceID)
	if err != nil {
		return res, err
	}
	if open != nil && open.DeadlineAt != nil && open.DeadlineAt.Before(s.At.Add(-models.SelfUpdateStuckAfter)) {
		msg, step := models.SelfUpdateStaleResultMessage, models.SelfUpdateFailureUnresolved
		query, args, ok := buildTransitionSQL(models.CommandTransition{
			ID: open.ID, DeviceID: s.DeviceID, Actor: models.ActorSelfUpdate, To: models.CommandTimedOut,
			ResultMsg: &msg, FailureStep: &step, Now: s.At,
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
			if open, err = selfUpdateOpen(ctx, tx, s.DeviceID); err != nil {
				return res, err
			}
		}
	}

	// 3. FR-006: the open record absorbs the start; the window is not extended.
	if open != nil {
		res.Disposition, res.LinkID = models.SelfUpdateOpenRecord, open.ID
		return res, tx.Commit(ctx)
	}

	// 4. FR-007.
	var count int
	if err := tx.QueryRow(ctx,
		`SELECT count(*) FROM commands WHERE device_id=$1 AND origin=`+selfUpdateOriginLiteral+` AND created_at > $2`,
		s.DeviceID, s.At.Add(-models.SelfUpdateRateWindow)).Scan(&count); err != nil {
		return res, fmt.Errorf("count records: %w", err)
	}
	if count >= models.SelfUpdateRateCap {
		res.Disposition = models.SelfUpdateCapped
		var seen bool
		// Only a rate-cap event (not linked to a record) counts: a stuck-record
		// event of the same type must not silence the cap warning.
		if err := tx.QueryRow(ctx,
			`SELECT EXISTS (SELECT 1 FROM events WHERE device_id=$1 AND type=$2 AND command_id='' AND created_at > $3)`,
			s.DeviceID, string(models.EventAgentSelfUpdatePaused), s.At.Add(-models.SelfUpdateRateWindow)).Scan(&seen); err != nil {
			return res, fmt.Errorf("check rate-cap event: %w", err)
		}
		if !seen {
			msg, _ := redact.MaskLine(models.TruncateHead(models.SanitizeText(models.SelfUpdateRateCapMessage(s.Hostname)), 2048))
			evt := models.Event{DeviceID: s.DeviceID, Type: models.EventAgentSelfUpdatePaused, Severity: models.SeverityWarning,
				Message: msg, CreatedAt: s.At}
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
	// docker_audit row is written (AD-001).
	paramsJSON, err := json.Marshal(models.SelfUpdateParams(s.From, s.To, s.AttemptID))
	if err != nil {
		return res, fmt.Errorf("encode params: %w", err)
	}
	id := uuid.New().String()
	deadline := s.At.Add(models.ExpectSelfUpdateBound)
	if _, err := tx.Exec(ctx,
		`INSERT INTO commands (id, device_id, action, params, status, result_msg, created_at, updated_at,
			expires_at, exec_deadline_s, deadline_at, dispatched_at, started_at, step, origin, policy_id,
			target_keys, actor_kind, actor_auth, actor_name, actor_sub)
		 VALUES ($1, $2, $3, $4, $5, '', $6, $6, $6, $7, $8, $6, $6, $9, $10, NULL, '{}', 'system', '', '', '')`,
		id, s.DeviceID, models.ActionAgentUpdate, paramsJSON, string(models.CommandRunning), s.At,
		models.SelfUpdateExecDeadlineS, deadline, models.SelfUpdateStepWaitingForRestart, models.OriginAgentSelfUpdate); err != nil {
		return res, fmt.Errorf("insert self-update record: %w", err)
	}
	created, err := scanCommand(func(dest ...interface{}) error {
		return tx.QueryRow(ctx, `SELECT `+commandColumns+` FROM commands WHERE id=$1`, id).Scan(dest...)
	})
	if err != nil {
		return res, fmt.Errorf("read self-update record: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return res, fmt.Errorf("commit: %w", err)
	}
	res.Disposition, res.Command, res.LinkID = models.SelfUpdateCreated, &created, id
	return res, nil
}

// selfUpdateOpen reads the device's open record, or nil when there is none.
func selfUpdateOpen(ctx context.Context, q selfUpdateQuerier, deviceID string) (*models.Command, error) {
	cmd, err := scanCommand(func(dest ...interface{}) error {
		return q.QueryRow(ctx,
			`SELECT `+commandColumns+` FROM commands
			 WHERE device_id=$1 AND origin=`+selfUpdateOriginLiteral+` AND status IN (`+selfUpdateOpenStatusList+`)
			 ORDER BY created_at DESC LIMIT 1`, deviceID).Scan(dest...)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read open self-update record: %w", err)
	}
	return &cmd, nil
}

// OpenSelfUpdate returns the device's open (running or verifying) record, or
// nil when it has none. The unique index guarantees at most one.
func (r *CommandRepo) OpenSelfUpdate(ctx context.Context, deviceID string) (*models.Command, error) {
	return selfUpdateOpen(ctx, r.db.Pool, deviceID)
}

// LatestSelfUpdateSince returns the device's newest record created at or after
// since, or nil.
func (r *CommandRepo) LatestSelfUpdateSince(ctx context.Context, deviceID string, since time.Time) (*models.Command, error) {
	cmd, err := scanCommand(func(dest ...interface{}) error {
		return r.db.Pool.QueryRow(ctx,
			`SELECT `+commandColumns+` FROM commands
			 WHERE device_id=$1 AND origin=`+selfUpdateOriginLiteral+` AND created_at >= $2
			 ORDER BY created_at DESC LIMIT 1`, deviceID, since).Scan(dest...)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read latest self-update record: %w", err)
	}
	return &cmd, nil
}

// SelfUpdateByAttempt returns the device's record that owns attemptID, whatever
// its age or state, or nil (EXPECT-007 AD-006 step 4). It uses the partial index
// idx_commands_self_update_recent (device_id, created_at), which holds at most
// models.SelfUpdateRateCap records per device per day, about 540 over the
// retention. attemptID must already have passed models.ValidRefToken.
func (r *CommandRepo) SelfUpdateByAttempt(ctx context.Context, deviceID, attemptID string) (*models.Command, error) {
	cmd, err := scanCommand(func(dest ...interface{}) error {
		return r.db.Pool.QueryRow(ctx,
			`SELECT `+commandColumns+` FROM commands
			 WHERE device_id=$1 AND origin=`+selfUpdateOriginLiteral+` AND params->>'attempt_id' = $2
			 ORDER BY created_at DESC LIMIT 1`, deviceID, attemptID).Scan(dest...)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read self-update record by attempt: %w", err)
	}
	return &cmd, nil
}
