package db

import (
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5"
	"time"
)

// StartLogAudit serializes attachment accounting per authenticated device. A
// reconnect reopens the same row; floods produce one bounded summary per minute.
func (r *DockerRepo) StartLogAudit(ctx context.Context, device, name, subject, remote string) (int64, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	tx, e := r.db.Pool.Begin(ctx)
	if e != nil {
		return 0, e
	}
	defer tx.Rollback(ctx)
	if _, e = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('riot.logs.audit.'||$1,0))`, device); e != nil {
		return 0, e
	}
	var id int64
	e = tx.QueryRow(ctx, `SELECT id FROM docker_audit WHERE device_id=$1 AND kind='logs' AND actor_sub=$2 AND data->>'container'=$3 AND data->>'remote_addr'=$4 AND (data->>'ended_at' IS NULL OR (data->>'ended_at')::timestamptz>now()-interval '2 minutes') ORDER BY created_at DESC LIMIT 1`, device, subject, name, remote).Scan(&id)
	if e == nil {
		_, e = tx.Exec(ctx, `UPDATE docker_audit SET data=data-'ended_at' WHERE id=$1`, id)
	} else if e == pgx.ErrNoRows {
		var n int
		e = tx.QueryRow(ctx, `SELECT count(*) FROM docker_audit WHERE device_id=$1 AND kind='logs' AND created_at>=date_trunc('minute',now())`, device).Scan(&n)
		if e != nil {
			return 0, e
		}
		if n >= 32 {
			e = tx.QueryRow(ctx, `SELECT id FROM docker_audit WHERE device_id=$1 AND kind='logs_overflow' AND created_at>=date_trunc('minute',now()) LIMIT 1`, device).Scan(&id)
			if e == nil {
				_, e = tx.Exec(ctx, `UPDATE docker_audit SET data=jsonb_set(data,'{suppressed}',to_jsonb(COALESCE((data->>'suppressed')::int,0)+1)) WHERE id=$1`, id)
			} else if e == pgx.ErrNoRows {
				e = tx.QueryRow(ctx, `INSERT INTO docker_audit(device_id,kind,action,data) VALUES($1,'logs_overflow','attach',jsonb_build_object('container','*','suppressed',1,'ended_at',now())) RETURNING id`, device).Scan(&id)
			}
			id = 0
		} else {
			raw, _ := json.Marshal(map[string]interface{}{"container": name, "remote_addr": remote})
			e = tx.QueryRow(ctx, `INSERT INTO docker_audit(device_id,kind,action,actor_sub,data) VALUES($1,'logs','attach',$2,$3) RETURNING id`, device, subject, raw).Scan(&id)
		}
	}
	if e != nil {
		return 0, e
	}
	return id, tx.Commit(ctx)
}
func (r *DockerRepo) EndLogAudit(ctx context.Context, id int64) error {
	if id == 0 {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	_, e := r.db.Pool.Exec(ctx, `UPDATE docker_audit SET data=jsonb_set(data,'{ended_at}',to_jsonb(now())) WHERE id=$1 AND kind='logs'`, id)
	return e
}
