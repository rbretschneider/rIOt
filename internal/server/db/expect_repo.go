package db

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/DesyncTheThird/rIOt/internal/models"
	"github.com/DesyncTheThird/rIOt/internal/redact"
	"github.com/jackc/pgx/v5"
)

// ExpectRepo is the persistence of expected-activity windows (EXPECT-001):
// the window-processed marker on commands, the exactly-once escalation markers
// and the powered-off state. Every value is a bind parameter; the only SQL
// literal lists are rendered from typed constants (models.ExpectActionsSQL,
// models.SQLStatusList).
//
// Bounds (AD-012): command_escalations holds at most 22 rows per command and
// cascades with the command; device_powered_off holds at most 1 row per device
// and is deleted on any evidence of life.
type ExpectRepo struct {
	db *DB
}

func NewExpectRepo(db *DB) *ExpectRepo { return &ExpectRepo{db: db} }

// expectActionList renders the window actions for the partial index predicate.
var expectActionList = models.ExpectActionsSQL()

// ListOpenWindows returns the in-scope commands whose window is not processed
// and that are active, or finished at most 24 h before now (newest first).
// Queued commands are included: they only produce a label.
func (r *ExpectRepo) ListOpenWindows(ctx context.Context, now time.Time, limit int) ([]models.Command, error) {
	rows, err := r.db.Pool.Query(ctx,
		`SELECT `+commandColumns+` FROM commands
		 WHERE window_closed_at IS NULL AND action IN (`+expectActionList+`)
		   AND (status IN (`+activeStatusList+`) OR COALESCE(finished_at, updated_at) >= $1)
		 ORDER BY created_at DESC LIMIT $2`, now.Add(-models.ExpectDeferredRecheckBound), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanCommands(rows)
}

// ListDueBacklog returns terminal in-scope commands whose window is not
// processed, oldest finish first.
func (r *ExpectRepo) ListDueBacklog(ctx context.Context, _ time.Time, limit int) ([]models.Command, error) {
	rows, err := r.db.Pool.Query(ctx,
		`SELECT `+commandColumns+` FROM commands
		 WHERE window_closed_at IS NULL AND action IN (`+expectActionList+`)
		   AND status NOT IN (`+activeStatusList+`)
		 ORDER BY COALESCE(finished_at, updated_at), created_at LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanCommands(rows)
}

// ListLapseCandidates returns unprocessed terminal in-scope commands that
// finished before cutoff, or that finished without ever being dispatched.
func (r *ExpectRepo) ListLapseCandidates(ctx context.Context, cutoff time.Time, limit int) ([]models.Command, error) {
	rows, err := r.db.Pool.Query(ctx,
		`SELECT `+commandColumns+` FROM commands
		 WHERE window_closed_at IS NULL AND action IN (`+expectActionList+`)
		   AND status NOT IN (`+activeStatusList+`)
		   AND (COALESCE(finished_at, updated_at) < $1 OR dispatched_at IS NULL)
		 ORDER BY COALESCE(finished_at, updated_at), created_at LIMIT $2`, cutoff, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanCommands(rows)
}

// CloseWindow marks the window processed. applied is false when it already was
// (the guard makes every retry, crash and restart harmless).
func (r *ExpectRepo) CloseWindow(ctx context.Context, commandID string, at time.Time, outcome models.WindowOutcome) (bool, error) {
	tag, err := r.db.Pool.Exec(ctx,
		`UPDATE commands SET window_closed_at=$2, window_outcome=$3 WHERE id=$1 AND window_closed_at IS NULL`,
		commandID, at, string(outcome))
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

// RecordEscalation stores the escalation marker and its event in one
// transaction. inserted is false when the marker already existed (the
// escalation was raised before, possibly before a restart); then no event is
// written. The caller publishes only when inserted.
func (r *ExpectRepo) RecordEscalation(ctx context.Context, esc *models.Escalation, evt *models.Event) (bool, error) {
	tx, err := r.db.Pool.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	tag, err := tx.Exec(ctx,
		`INSERT INTO command_escalations (command_id, target, device_id, created_at)
		 VALUES ($1, $2, $3, $4) ON CONFLICT (command_id, target) DO NOTHING`,
		esc.CommandID, esc.Target, esc.DeviceID, esc.CreatedAt)
	if err != nil {
		return false, fmt.Errorf("insert escalation marker: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return false, nil
	}
	evt.Message, _ = redact.MaskLine(models.TruncateHead(models.SanitizeText(evt.Message), 2048))
	if err := tx.QueryRow(ctx,
		`INSERT INTO events (device_id, type, severity, message, container_id, container_name, created_at, command_id, command_attr)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9) RETURNING id`,
		evt.DeviceID, evt.Type, evt.Severity, evt.Message, evt.ContainerID, evt.ContainerName, evt.CreatedAt, evt.CommandID, evt.CommandAttr,
	).Scan(&evt.ID); err != nil {
		return false, fmt.Errorf("insert escalation event: %w", err)
	}
	if _, err := tx.Exec(ctx,
		`UPDATE command_escalations SET event_id=$3 WHERE command_id=$1 AND target=$2`,
		esc.CommandID, esc.Target, evt.ID); err != nil {
		return false, fmt.Errorf("link escalation event: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("commit: %w", err)
	}
	id := evt.ID
	esc.EventID = &id
	return true, nil
}

// ListEscalationTargets returns the marker targets of a command (at most 22).
func (r *ExpectRepo) ListEscalationTargets(ctx context.Context, commandID string) ([]string, error) {
	rows, err := r.db.Pool.Query(ctx,
		`SELECT target FROM command_escalations WHERE command_id=$1 ORDER BY target`, commandID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// ListOpenDeviceEscalations returns the uncleared device escalations created at
// or after since (the failure label of the dashboard, FR-037).
func (r *ExpectRepo) ListOpenDeviceEscalations(ctx context.Context, since time.Time) ([]models.Escalation, error) {
	rows, err := r.db.Pool.Query(ctx,
		`SELECT command_id, target, device_id, event_id, created_at, cleared_at FROM command_escalations
		 WHERE target='device' AND cleared_at IS NULL AND created_at >= $1 ORDER BY created_at DESC`, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []models.Escalation{}
	for rows.Next() {
		var e models.Escalation
		if err := rows.Scan(&e.CommandID, &e.Target, &e.DeviceID, &e.EventID, &e.CreatedAt, &e.ClearedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// ClearDeviceEscalations clears the device's open escalations (it came back).
func (r *ExpectRepo) ClearDeviceEscalations(ctx context.Context, deviceID string, at time.Time) (int64, error) {
	tag, err := r.db.Pool.Exec(ctx,
		`UPDATE command_escalations SET cleared_at=$2 WHERE device_id=$1 AND target='device' AND cleared_at IS NULL`,
		deviceID, at)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// SuppressedDeaths returns, per container name, the time of the last
// container_died event that this command suppressed (idx_events_command).
func (r *ExpectRepo) SuppressedDeaths(ctx context.Context, deviceID, commandID string, limit int) ([]models.ContainerDeath, error) {
	rows, err := r.db.Pool.Query(ctx,
		`SELECT container_name, max(created_at) FROM events
		 WHERE command_id=$1 AND device_id=$2 AND type='container_died' AND command_attr=$3
		 GROUP BY container_name ORDER BY container_name LIMIT $4`,
		commandID, deviceID, models.CommandAttrExpected, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []models.ContainerDeath{}
	for rows.Next() {
		var d models.ContainerDeath
		if err := rows.Scan(&d.ContainerName, &d.At); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// ContainerStartsSince returns, per container name as stored, the time of the
// latest container_started event of the device since the given time.
func (r *ExpectRepo) ContainerStartsSince(ctx context.Context, deviceID string, since time.Time, limit int) (map[string]time.Time, error) {
	rows, err := r.db.Pool.Query(ctx,
		`SELECT container_name, max(created_at) AS at FROM events
		 WHERE device_id=$1 AND type='container_started' AND created_at >= $2
		 GROUP BY container_name ORDER BY at DESC LIMIT $3`, deviceID, since, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]time.Time{}
	for rows.Next() {
		var name string
		var at time.Time
		if err := rows.Scan(&name, &at); err != nil {
			return nil, err
		}
		out[name] = at
	}
	return out, rows.Err()
}

// UpsertPoweredOff stores the device's powered-off state. A newer shutdown
// replaces an older row (FR-032); the same command never refreshes its row, so
// a later offline transition cannot re-stamp offline_at (AD-007, C-1 d).
func (r *ExpectRepo) UpsertPoweredOff(ctx context.Context, p *models.PoweredOffState) (bool, error) {
	if p.CreatedAt.IsZero() {
		p.CreatedAt = p.OfflineAt
	}
	tag, err := r.db.Pool.Exec(ctx,
		`INSERT INTO device_powered_off (device_id, command_id, action_label, actor_phrase, shutdown_at, offline_at, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)
		 ON CONFLICT (device_id) DO UPDATE SET command_id=EXCLUDED.command_id, action_label=EXCLUDED.action_label,
		   actor_phrase=EXCLUDED.actor_phrase, shutdown_at=EXCLUDED.shutdown_at, offline_at=EXCLUDED.offline_at,
		   created_at=EXCLUDED.created_at
		 WHERE device_powered_off.command_id <> EXCLUDED.command_id AND EXCLUDED.shutdown_at > device_powered_off.shutdown_at`,
		p.DeviceID, p.CommandID, p.ActionLabel, p.ActorPhrase, p.ShutdownAt, p.OfflineAt, p.CreatedAt)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

// DeletePoweredOff removes the device's powered-off row and returns it, or nil
// when there was none.
func (r *ExpectRepo) DeletePoweredOff(ctx context.Context, deviceID string) (*models.PoweredOffState, error) {
	var p models.PoweredOffState
	err := r.db.Pool.QueryRow(ctx,
		`DELETE FROM device_powered_off WHERE device_id=$1
		 RETURNING device_id, command_id, action_label, actor_phrase, shutdown_at, offline_at, created_at`, deviceID,
	).Scan(&p.DeviceID, &p.CommandID, &p.ActionLabel, &p.ActorPhrase, &p.ShutdownAt, &p.OfflineAt, &p.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// PurgeInvalidPoweredOff deletes every powered-off row whose device shows
// evidence of life: a status other than offline, or a heartbeat newer than the
// row's offline_at (AD-007, C-1 b). It never depends on observing a transition.
func (r *ExpectRepo) PurgeInvalidPoweredOff(ctx context.Context) (int64, error) {
	tag, err := r.db.Pool.Exec(ctx,
		`DELETE FROM device_powered_off p USING devices d
		 WHERE d.id = p.device_id AND (d.status <> 'offline' OR d.last_heartbeat > p.offline_at)`)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// ListPoweredOff returns the valid powered-off rows only: the predicate of
// PurgeInvalidPoweredOff negated, so a live device never has a row here.
func (r *ExpectRepo) ListPoweredOff(ctx context.Context) ([]models.PoweredOffState, error) {
	rows, err := r.db.Pool.Query(ctx,
		`SELECT p.device_id, p.command_id, p.action_label, p.actor_phrase, p.shutdown_at, p.offline_at, p.created_at
		 FROM device_powered_off p JOIN devices d ON d.id = p.device_id
		 WHERE d.status = 'offline' AND (d.last_heartbeat IS NULL OR d.last_heartbeat <= p.offline_at)
		 ORDER BY p.device_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []models.PoweredOffState{}
	for rows.Next() {
		var p models.PoweredOffState
		if err := rows.Scan(&p.DeviceID, &p.CommandID, &p.ActionLabel, &p.ActorPhrase, &p.ShutdownAt, &p.OfflineAt, &p.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// CreateWarningEvent is a plain event insert for the events that are not
// routed through alert rules (expect_backstop_skipped, the held summary).
func (r *ExpectRepo) CreateWarningEvent(ctx context.Context, e *models.Event) error {
	e.Message, _ = redact.MaskLine(models.TruncateHead(models.SanitizeText(e.Message), 2048))
	return r.db.Pool.QueryRow(ctx,
		`INSERT INTO events (device_id, type, severity, message, container_id, container_name, created_at, command_id, command_attr)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9) RETURNING id`,
		e.DeviceID, e.Type, e.Severity, e.Message, e.ContainerID, e.ContainerName, e.CreatedAt, e.CommandID, e.CommandAttr,
	).Scan(&e.ID)
}

// PendingWindowExemptionSQL is the predicate that keeps a command whose window
// is not processed out of a DELETE (EXPECT-001 C-2 b). The action list is
// rendered from typed constants. Retention and the command-results budget both
// append it to their commands DELETE.
func PendingWindowExemptionSQL() string {
	return `NOT (window_closed_at IS NULL AND action IN (` + expectActionList + `))`
}
