package db

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/DesyncTheThird/rIOt/internal/models"
	"github.com/jackc/pgx/v5"
)

// ErrCommandNotFound is returned by GetByID and GetForDevice when no row
// matches. A foreign device's command is indistinguishable from a missing one
// (GetForDevice scopes the read to the device in SQL).
var ErrCommandNotFound = errors.New("command not found")

// CommandRepo handles commands database operations.
type CommandRepo struct {
	db *DB
}

func NewCommandRepo(db *DB) *CommandRepo {
	return &CommandRepo{db: db}
}

// Status lists that must match the partial indexes of migration 000023 are
// rendered from typed constants (models.SQLStatusList panics on anything else).
// Request-supplied filters never reach SQL text: they are bound as parameters
// (buildFilteredQuery, SEC C-6).
var (
	activeStatusList   = models.SQLStatusList(models.ExpandAliases(models.ActiveStates()...)...)
	queuedStatusList   = models.SQLStatusList(models.CommandQueued, models.LegacyPending)
	inflightStatusList = models.SQLStatusList(
		models.CommandDispatched, models.LegacySent, models.CommandAcknowledged,
		models.CommandRunning, models.CommandVerifying)
)

const commandColumns = `id, device_id, action, params, status, result_msg, duration_ms, exit_code, created_at, updated_at,
	expires_at, exec_deadline_s, deadline_at, dispatched_at, acknowledged_at, started_at, verifying_at, finished_at,
	step, progress, failure_step, stderr_tail, origin, policy_id, target_keys, pre_lifecycle, send_ambiguous, actor_kind, actor_auth, actor_name, actor_sub, steps, target_results`

// scanCommand scans a single command row selected with commandColumns. The
// result is normalized, so legacy status values read as canonical ones
// (FR-083) and the derived fields (TargetKey, Origin) are filled.
func scanCommand(scan func(dest ...interface{}) error) (models.Command, error) {
	var cmd models.Command
	var paramsJSON []byte
	if err := scan(&cmd.ID, &cmd.DeviceID, &cmd.Action, &paramsJSON, &cmd.Status, &cmd.ResultMsg,
		&cmd.DurationMs, &cmd.ExitCode, &cmd.CreatedAt, &cmd.UpdatedAt,
		&cmd.ExpiresAt, &cmd.ExecDeadlineS, &cmd.DeadlineAt, &cmd.DispatchedAt, &cmd.AcknowledgedAt,
		&cmd.StartedAt, &cmd.VerifyingAt, &cmd.FinishedAt,
		&cmd.Step, &cmd.Progress, &cmd.FailureStep, &cmd.StderrTail, &cmd.Origin, &cmd.PolicyID,
		&cmd.TargetKeys, &cmd.PreLifecycle, &cmd.SendAmbiguous, &cmd.ActorKind, &cmd.ActorAuth, &cmd.ActorName, &cmd.ActorSub, &cmd.Steps, &cmd.TargetResults); err != nil {
		return cmd, err
	}
	cmd.Params = make(map[string]interface{})
	if len(paramsJSON) > 0 {
		if err := json.Unmarshal(paramsJSON, &cmd.Params); err != nil {
			return cmd, fmt.Errorf("decode params of command %s: %w", cmd.ID, err)
		}
	}
	cmd.Normalize()
	return cmd, nil
}

func scanCommands(rows pgx.Rows) ([]models.Command, error) {
	commands := []models.Command{}
	for rows.Next() {
		cmd, err := scanCommand(rows.Scan)
		if err != nil {
			return nil, err
		}
		commands = append(commands, cmd)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return commands, nil
}

// CreateIfNoConflict inserts cmd unless a non-terminal command on the same
// device already holds one of its target keys (BR-008). The check and the
// insert run in one transaction under a per-device advisory lock, because a
// multi-key bulk conflict cannot be expressed as a unique index. A non-nil
// conflict is the oldest holder; no row is created in that case.
func (r *CommandRepo) CreateIfNoConflict(ctx context.Context, cmd *models.Command) (*models.Command, error) {
	paramsJSON, err := json.Marshal(cmd.Params)
	if err != nil {
		return nil, fmt.Errorf("encode params: %w", err)
	}
	keys := models.NormalizeTargetKeys(cmd.TargetKeys...)
	status := string(models.CanonicalStatus(cmd.Status))
	if cmd.Status == "" {
		status = string(models.CommandQueued)
	}
	origin := cmd.Origin
	if origin == "" {
		origin = models.OriginUser
	}
	if cmd.CreatedAt.IsZero() {
		cmd.CreatedAt = time.Now().UTC()
	}
	if cmd.UpdatedAt.IsZero() {
		cmd.UpdatedAt = cmd.CreatedAt
	}

	tx, err := r.db.Pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin: %w", err)
	}
	defer func() {
		if rbErr := tx.Rollback(ctx); rbErr != nil && !errors.Is(rbErr, pgx.ErrTxClosed) {
			slog.Warn("command create rollback failed", "error", rbErr.Error())
		}
	}()

	if len(keys) > 0 || len(cmd.ProbeKeys) > 0 || cmd.IsDocker || cmd.DockerExclusive {
		if _, err := tx.Exec(ctx,
			`SELECT pg_advisory_xact_lock(hashtextextended('riot.cmd.' || $1::text, 0))`, cmd.DeviceID); err != nil {
			return nil, fmt.Errorf("lock device commands: %w", err)
		}
		existing, err := scanCommand(func(dest ...interface{}) error {
			return tx.QueryRow(ctx,
				`SELECT `+commandColumns+` FROM commands
				 WHERE device_id=$1 AND status IN (`+activeStatusList+`) AND (target_keys && $2::text[] OR ($3 AND EXISTS(SELECT 1 FROM unnest(target_keys) k WHERE k LIKE 'docker:%')) OR ($4 AND 'docker:host'=ANY(target_keys)))
				 ORDER BY created_at LIMIT 1`, cmd.DeviceID, models.NormalizeTargetKeys(append(keys, cmd.ProbeKeys...)...), cmd.DockerExclusive, cmd.IsDocker).Scan(dest...)
		})
		switch {
		case err == nil:
			return &existing, nil
		case !errors.Is(err, pgx.ErrNoRows):
			return nil, fmt.Errorf("conflict check: %w", err)
		}
	}

	if _, err := tx.Exec(ctx,
		`INSERT INTO commands (id, device_id, action, params, status, result_msg, created_at, updated_at,
			expires_at, exec_deadline_s, step, origin, policy_id, target_keys, actor_kind, actor_auth, actor_name, actor_sub)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18)`,
		cmd.ID, cmd.DeviceID, cmd.Action, paramsJSON, status, cmd.ResultMsg, cmd.CreatedAt, cmd.UpdatedAt,
		cmd.ExpiresAt, cmd.ExecDeadlineS, cmd.Step, origin, cmd.PolicyID, keys, cmd.ActorKind, cmd.ActorAuth, cmd.ActorName, cmd.ActorSub); err != nil {
		return nil, fmt.Errorf("insert command: %w", err)
	}
	if cmd.ActorKind == "operator" || cmd.Origin == models.OriginUser {
		detail, _ := json.Marshal(map[string]interface{}{"command_id": cmd.ID, "actor_kind": cmd.ActorKind, "actor_auth": cmd.ActorAuth, "actor_name": cmd.ActorName, "target_keys": keys})
		if _, err := tx.Exec(ctx, `INSERT INTO docker_audit(device_id,kind,action,actor_sub,data,created_at) VALUES($1,'command',$2,$3,$4,$5)`, cmd.DeviceID, cmd.Action, cmd.ActorSub, detail, cmd.CreatedAt); err != nil {
			return nil, fmt.Errorf("audit command: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}
	cmd.Status = status
	cmd.Origin = origin
	cmd.TargetKeys = keys
	return nil, nil
}

// GetByID returns a single command, or ErrCommandNotFound.
func (r *CommandRepo) GetByID(ctx context.Context, id string) (*models.Command, error) {
	cmd, err := scanCommand(func(dest ...interface{}) error {
		return r.db.Pool.QueryRow(ctx,
			`SELECT `+commandColumns+` FROM commands WHERE id=$1`, id).Scan(dest...)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrCommandNotFound
	}
	if err != nil {
		return nil, err
	}
	return &cmd, nil
}

// GetForDevice returns a command only when it belongs to deviceID. The device
// scope is part of the SQL, so a foreign command reads exactly like a missing
// one (ErrCommandNotFound).
func (r *CommandRepo) GetForDevice(ctx context.Context, id, deviceID string) (*models.Command, error) {
	cmd, err := scanCommand(func(dest ...interface{}) error {
		return r.db.Pool.QueryRow(ctx,
			`SELECT `+commandColumns+` FROM commands WHERE id=$1 AND device_id=$2`, id, deviceID).Scan(dest...)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrCommandNotFound
	}
	if err != nil {
		return nil, err
	}
	return &cmd, nil
}

// ListByDevice returns recent commands for a device.
func (r *CommandRepo) ListByDevice(ctx context.Context, deviceID string, limit int) ([]models.Command, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := r.db.Pool.Query(ctx,
		`SELECT `+commandColumns+`
		 FROM commands WHERE device_id=$1 ORDER BY created_at DESC LIMIT $2`,
		deviceID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanCommands(rows)
}

// buildFilteredQuery assembles the list query for ListByDeviceFiltered. Every
// request-derived value (statuses, action, targetKey) is a bind parameter; the
// SQL text contains placeholders only (SEC C-6). statuses must already be the
// alias-expanded output of models.ParseStatusFilter.
func buildFilteredQuery(deviceID string, limit, offset int, statuses []string, action, targetKey string) (string, []interface{}) {
	query := `SELECT ` + commandColumns + ` FROM commands WHERE device_id=$1`
	args := []interface{}{deviceID}
	if len(statuses) > 0 {
		args = append(args, statuses)
		query += fmt.Sprintf(` AND status = ANY($%d::text[])`, len(args))
	}
	if action != "" {
		args = append(args, action)
		query += fmt.Sprintf(` AND action=$%d`, len(args))
	}
	if targetKey != "" {
		args = append(args, targetKey)
		query += fmt.Sprintf(` AND $%d = ANY(target_keys)`, len(args))
	}
	args = append(args, limit, offset)
	query += fmt.Sprintf(` ORDER BY created_at DESC LIMIT $%d OFFSET $%d`, len(args)-1, len(args))
	return query, args
}

// ListByDeviceFiltered returns commands with pagination and optional
// filtering by status (alias-expanded, canonical plus legacy), action and
// target key.
func (r *CommandRepo) ListByDeviceFiltered(ctx context.Context, deviceID string, limit, offset int, statuses []string, action, targetKey string) ([]models.Command, error) {
	if limit <= 0 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	query, args := buildFilteredQuery(deviceID, limit, offset, statuses, action, targetKey)
	rows, err := r.db.Pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanCommands(rows)
}

// buildActiveQuery assembles the active-commands query. The status list is
// rendered from typed constants; targetKey is a bind parameter.
func buildActiveQuery(deviceID, targetKey string) (string, []interface{}) {
	query := `SELECT ` + commandColumns + ` FROM commands WHERE device_id=$1 AND status IN (` + activeStatusList + `)`
	args := []interface{}{deviceID}
	if targetKey != "" {
		args = append(args, targetKey)
		query += fmt.Sprintf(` AND $%d = ANY(target_keys)`, len(args))
	}
	query += ` ORDER BY created_at`
	return query, args
}

// ListActive returns the device's non-terminal commands, optionally only those
// holding targetKey (FR-092).
func (r *CommandRepo) ListActive(ctx context.Context, deviceID, targetKey string) ([]models.Command, error) {
	query, args := buildActiveQuery(deviceID, targetKey)
	rows, err := r.db.Pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanCommands(rows)
}

// finishedOSUpdateStatusList names the outcomes that count as a finished
// os_update run. Expired and cancelled commands never ran to a result, so they
// are not outcomes.
var finishedOSUpdateStatusList = models.SQLStatusList(
	models.CommandSucceeded, models.CommandFailed, models.CommandTimedOut)

// LatestFinishedOSUpdates returns, per device, the newest os_update command
// that finished as succeeded, failed or timed_out. Devices with no such
// command are absent from the map. One statement serves the whole fleet; the
// lateral lookup rides idx_commands_device (device_id, created_at DESC).
func (r *CommandRepo) LatestFinishedOSUpdates(ctx context.Context) (map[string]models.Command, error) {
	rows, err := r.db.Pool.Query(ctx, `SELECT `+commandColumns+` FROM devices d
		CROSS JOIN LATERAL (
			SELECT `+commandColumns+` FROM commands
			WHERE device_id = d.id AND action = 'os_update' AND status IN (`+finishedOSUpdateStatusList+`)
			ORDER BY created_at DESC LIMIT 1
		) latest`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	cmds, err := scanCommands(rows)
	if err != nil {
		return nil, err
	}
	out := make(map[string]models.Command, len(cmds))
	for _, c := range cmds {
		out[c.DeviceID] = c
	}
	return out, nil
}

// ClaimQueued is the heartbeat delivery claim: one statement moves up to limit
// queued, unexpired commands of the device to dispatched and stamps their
// deadline. FOR UPDATE SKIP LOCKED plus the repeated outer guard make each
// command claimable by exactly one caller; expires_at > now sits inside the
// predicate, so an expired command is never returned even when the expiry step
// failed (NFR-005). The result is ordered by created_at.
func (r *CommandRepo) ClaimQueued(ctx context.Context, deviceID string, now time.Time, limit int) ([]models.Command, error) {
	if limit <= 0 {
		return []models.Command{}, nil
	}
	rows, err := r.db.Pool.Query(ctx,
		`UPDATE commands
		 SET status='dispatched', dispatched_at=$2, deadline_at=$2 + (exec_deadline_s * INTERVAL '1 second'), updated_at=$2
		 WHERE id IN (
			SELECT id FROM commands
			WHERE device_id=$1 AND status IN (`+queuedStatusList+`) AND expires_at > $2
			ORDER BY created_at LIMIT $3
			FOR UPDATE SKIP LOCKED)
		   AND status IN (`+queuedStatusList+`) AND expires_at > $2
		 RETURNING `+commandColumns, deviceID, now, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	claimed, err := scanCommands(rows)
	if err != nil {
		return nil, err
	}
	sortByCreatedAt(claimed)
	return claimed, nil
}

// ClaimByID is the WS delivery claim for one command: queued and not expired
// becomes dispatched. claimed is false when the command is gone, expired,
// cancelled or already claimed.
func (r *CommandRepo) ClaimByID(ctx context.Context, id string, now time.Time) (*models.Command, bool, error) {
	cmd, err := scanCommand(func(dest ...interface{}) error {
		return r.db.Pool.QueryRow(ctx,
			`UPDATE commands
			 SET status='dispatched', dispatched_at=$2, deadline_at=$2 + (exec_deadline_s * INTERVAL '1 second'), updated_at=$2
			 WHERE id=$1 AND status IN (`+queuedStatusList+`) AND expires_at > $2
			 RETURNING `+commandColumns, id, now).Scan(dest...)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return &cmd, true, nil
}

// buildTransitionSQL renders the guarded UPDATE of Transition. ok is false when
// the edge is not allowed at all (empty guard); the caller then answers "not
// applied" without querying. The guard is the alias-aware status list of
// models.CommandTransition.GuardStates (typed constants only); every value is a
// bind parameter.
func buildTransitionSQL(t models.CommandTransition) (query string, args []interface{}, ok bool) {
	guard := t.GuardStates()
	if len(guard) == 0 || t.Now.IsZero() {
		return "", nil, false
	}
	to := t.To.Canonical()
	// $1 id, $2 status, $3 now.
	args = []interface{}{t.ID, string(to), t.Now}
	sets := []string{"status=$2", "updated_at=$3"}
	set := func(col string, v interface{}) {
		args = append(args, v)
		sets = append(sets, fmt.Sprintf("%s=$%d", col, len(args)))
	}
	if t.Step != nil {
		set("step", *t.Step)
	}
	if t.ProgressSet {
		set("progress", t.Progress)
	}
	if t.ResultMsg != nil {
		set("result_msg", *t.ResultMsg)
	}
	if t.FailureStep != nil {
		set("failure_step", *t.FailureStep)
	}
	if t.StderrTail != nil {
		set("stderr_tail", *t.StderrTail)
	}
	if t.ExitCode != nil {
		set("exit_code", *t.ExitCode)
	}
	if t.DurationMs != nil {
		set("duration_ms", *t.DurationMs)
	}

	if t.ResultBytes != nil {
		set("result_bytes", *t.ResultBytes)
	}
	if t.Steps != nil {
		set("steps", t.Steps)
	}
	if t.TargetResults != nil {
		set("target_results", t.TargetResults)
	}
	switch to {
	case models.CommandAcknowledged:
		sets = append(sets, "acknowledged_at=$3")
	case models.CommandRunning:
		sets = append(sets, "started_at=COALESCE(started_at,$3)")
	case models.CommandVerifying:
		sets = append(sets, "started_at=COALESCE(started_at,$3)", "verifying_at=$3")
	case models.CommandSucceeded, models.CommandFailed, models.CommandTimedOut,
		models.CommandCancelled, models.CommandExpired:
		sets = append(sets, "finished_at=$3")
	case models.CommandQueued:
		// FR-085 revert: the claim is undone, and the earlier send may still have
		// reached the agent (SEC-016).
		sets = append(sets, "dispatched_at=NULL", "deadline_at=NULL", "send_ambiguous=true")
	case models.CommandDispatched:
		sets = append(sets, "dispatched_at=$3", "deadline_at=$3 + (exec_deadline_s * INTERVAL '1 second')")
	}
	if t.Actor == models.ActorAgent {
		// An agent edge out of queued is proof of delivery: fill the dispatch time
		// and the deadline so no active row escapes the sweeper. Progress never
		// moves an existing deadline.
		sets = append(sets,
			"dispatched_at=COALESCE(dispatched_at,$3)",
			"deadline_at=COALESCE(deadline_at,$3 + (exec_deadline_s * INTERVAL '1 second'))")
	}

	where := "id=$1"
	if t.DeviceID != "" {
		args = append(args, t.DeviceID)
		where += fmt.Sprintf(" AND device_id=$%d", len(args))
	}
	where += " AND status IN (" + models.SQLStatusList(guard...) + ")"

	query = "UPDATE commands SET " + strings.Join(sets, ", ") + " WHERE " + where + " RETURNING " + commandColumns
	return query, args, true
}

func sortByCreatedAt(cmds []models.Command) {
	sort.SliceStable(cmds, func(i, j int) bool { return cmds[i].CreatedAt.Before(cmds[j].CreatedAt) })
}

// Transition applies one guarded state change: UPDATE ... WHERE id AND
// [device_id AND] status IN (allowed sources). It returns the updated row and
// applied=true, or (nil, false, nil) when the guard did not match (row missing,
// foreign device, or a source state the edge does not allow). The caller
// classifies a miss with GetForDevice. Under READ COMMITTED the losing side of
// a race re-evaluates the guard and matches zero rows, so exactly one of two
// concurrent transitions wins.
func (r *CommandRepo) Transition(ctx context.Context, t models.CommandTransition) (*models.Command, bool, error) {
	query, args, ok := buildTransitionSQL(t)
	if !ok {
		return nil, false, nil
	}
	cmd, err := scanCommand(func(dest ...interface{}) error {
		return r.db.Pool.QueryRow(ctx, query, args...).Scan(dest...)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return &cmd, true, nil
}

// ListExpiredQueued returns queued commands whose expires_at has passed, oldest
// expiry first. An empty deviceID means every device (the sweeper).
func (r *CommandRepo) ListExpiredQueued(ctx context.Context, deviceID string, now time.Time, limit int) ([]models.Command, error) {
	return r.listDue(ctx, `status IN (`+queuedStatusList+`) AND expires_at <= $1`, `expires_at`, deviceID, now, limit)
}

// ListOverdue returns dispatched/acknowledged/running/verifying commands whose
// deadline_at has passed, oldest deadline first. An empty deviceID means every
// device.
func (r *CommandRepo) ListOverdue(ctx context.Context, deviceID string, now time.Time, limit int) ([]models.Command, error) {
	return r.listDue(ctx, `status IN (`+inflightStatusList+`) AND deadline_at <= $1`, `deadline_at`, deviceID, now, limit)
}

// listDue runs a due-row query. predicate and orderCol are package constants,
// never request data.
func (r *CommandRepo) listDue(ctx context.Context, predicate, orderCol, deviceID string, now time.Time, limit int) ([]models.Command, error) {
	if limit <= 0 {
		limit = 500
	}
	query := `SELECT ` + commandColumns + ` FROM commands WHERE ` + predicate
	args := []interface{}{now}
	if deviceID != "" {
		args = append(args, deviceID)
		query += fmt.Sprintf(` AND device_id=$%d`, len(args))
	}
	args = append(args, limit)
	query += fmt.Sprintf(` ORDER BY %s LIMIT $%d`, orderCol, len(args))
	rows, err := r.db.Pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanCommands(rows)
}

// SaveCommandOutput inserts a command output record.
func (r *CommandRepo) SaveCommandOutput(ctx context.Context, output *models.CommandOutput) error {
	err := r.db.Pool.QueryRow(ctx,
		`INSERT INTO command_output (command_id, stream, content, created_at,bytes)
		 VALUES ($1, $2, $3, NOW(),octet_length($3::text))
		 RETURNING id, created_at`,
		output.CommandID, output.Stream, output.Content).Scan(&output.ID, &output.CreatedAt)
	return err
}

// GetCommandOutput returns all output records for a command.
func (r *CommandRepo) GetCommandOutput(ctx context.Context, commandID string) ([]models.CommandOutput, error) {
	rows, err := r.db.Pool.Query(ctx,
		`SELECT id, command_id, stream, content, created_at
		 FROM command_output WHERE command_id=$1 ORDER BY created_at ASC`,
		commandID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var outputs []models.CommandOutput
	for rows.Next() {
		var o models.CommandOutput
		if err := rows.Scan(&o.ID, &o.CommandID, &o.Stream, &o.Content, &o.CreatedAt); err != nil {
			return nil, err
		}
		outputs = append(outputs, o)
	}
	return outputs, rows.Err()
}

func (r *CommandRepo) KnownDevice(ctx context.Context, id string) (bool, error) {
	var exists bool
	err := r.db.Pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM devices WHERE id=$1)`, id).Scan(&exists)
	return exists, err
}
