package db

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5/pgconn"
	"log/slog"
	"time"
)

// Purge bounds each independent purge and retries timeouts with a smaller batch.
// Outputs precede commands, so the parent deletion never cascades large results.
func (r *DockerRepo) Purge(ctx context.Context, now time.Time) error {
	if r == nil {
		return nil
	}
	queries := []struct {
		sql, kind string
		days      int
	}{
		{`DELETE FROM command_output WHERE id IN(SELECT id FROM command_output WHERE created_at<$1 ORDER BY created_at LIMIT $2)`, "command_output", 30},
		{`DELETE FROM commands WHERE id IN(SELECT id FROM commands WHERE COALESCE(finished_at,updated_at)<$1 AND status IN('succeeded','failed','timed_out','cancelled','expired','success','error') AND NOT EXISTS(SELECT 1 FROM command_output WHERE command_id=commands.id) ORDER BY created_at LIMIT $2)`, "commands", 90},
		{`DELETE FROM docker_audit WHERE id IN(SELECT id FROM docker_audit WHERE created_at<$1 AND kind IN('terminal','command','device_delete','stack_paths','policy') AND (kind<>'terminal' OR data->>'ended_at' IS NOT NULL) ORDER BY created_at LIMIT $2)`, "long_audit", 180},
		{`DELETE FROM docker_audit WHERE id IN(SELECT id FROM docker_audit WHERE created_at<$1 AND kind NOT IN('terminal','command','device_delete','stack_paths','policy') AND data->>'ended_at' IS NOT NULL ORDER BY created_at LIMIT $2)`, "short_audit", 90},
		{`DELETE FROM terminal_sessions WHERE id IN(SELECT id FROM terminal_sessions WHERE started_at<$1 AND ended_at IS NOT NULL ORDER BY started_at LIMIT $2)`, "legacy_terminal", 180},
	}
	var first error
	for _, q := range queries {
		deleted, err := r.adaptivePurge(ctx, q.sql, now.Add(-time.Duration(q.days)*24*time.Hour))
		if deleted > 0 {
			slog.Info("purged Docker history", "kind", q.kind, "deleted", deleted, "retention_days", q.days)
		}
		if err != nil && first == nil {
			first = err
		}
	}
	return first
}
func (r *DockerRepo) adaptivePurge(parent context.Context, query string, before time.Time) (int64, error) {
	ctx, cancel := context.WithTimeout(parent, 30*time.Second)
	defer cancel()
	batch := 5000
	var deleted int64
	for {
		tx, err := r.db.Pool.Begin(ctx)
		if err != nil {
			return deleted, err
		}
		if _, err = tx.Exec(ctx, `SET LOCAL lock_timeout='5s'; SET LOCAL statement_timeout='10s'`); err != nil {
			tx.Rollback(ctx)
			return deleted, err
		}
		tag, err := tx.Exec(ctx, query, before, batch)
		if err == nil {
			err = tx.Commit(ctx)
		} else {
			tx.Rollback(ctx)
		}
		if err != nil {
			var pgerr *pgconn.PgError
			if errors.As(err, &pgerr) && pgerr.Code == "57014" && batch > 1 {
				batch = max(1, batch/2)
				continue
			}
			return deleted, err
		}
		deleted += tag.RowsAffected()
		if tag.RowsAffected() < int64(batch) {
			return deleted, nil
		}
		batch = min(5000, batch*2)
	}
}
