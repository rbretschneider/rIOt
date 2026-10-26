package storage

import (
	"context"
	"errors"
	"time"

	"github.com/DesyncTheThird/rIOt/internal/server/db"
	"github.com/jackc/pgx/v5/pgconn"
)

func (g *Guard) commandBytes(ctx context.Context) (int64, error) {
	var bytes int64
	err := g.pool.QueryRow(ctx, `SELECT COALESCE((SELECT sum(bytes) FROM command_output WHERE bytes>0),0)+COALESCE((SELECT sum(result_bytes) FROM commands WHERE result_bytes>0),0)`).Scan(&bytes)
	return bytes, err
}

// purgeCommandResults measures logical retained bytes, never relation size:
// DELETE reuses pages after vacuum; it does not release filesystem space.
func (g *Guard) purgeCommandResults(ctx context.Context, target int64, before *time.Time) (int64, error) {
	usage, err := g.commandBytes(ctx)
	if err != nil {
		return 0, err
	}
	terminal := `('succeeded','failed','timed_out','cancelled','expired','success','error')`
	queries := []string{
		`WITH selected AS(SELECT o.id FROM command_output o JOIN commands c ON c.id=o.command_id WHERE c.status IN ` + terminal + ` AND ($1::timestamptz IS NULL OR o.created_at<$1) AND (o.bytes>0 OR $1::timestamptz IS NOT NULL) ORDER BY o.created_at,o.id LIMIT $2),deleted AS(DELETE FROM command_output WHERE id IN(SELECT id FROM selected) RETURNING bytes) SELECT COALESCE(sum(bytes),0)::bigint,count(*)::bigint FROM deleted`,
		`WITH selected AS(SELECT c.id FROM commands c WHERE c.status IN ` + terminal + ` AND ($1::timestamptz IS NULL OR COALESCE(c.finished_at,c.updated_at)<$1) AND (c.result_bytes>0 OR $1::timestamptz IS NOT NULL) AND NOT EXISTS(SELECT 1 FROM command_output o WHERE o.command_id=c.id) AND ` + db.PendingWindowExemptionSQL() + ` ORDER BY c.created_at,c.id LIMIT $2),deleted AS(DELETE FROM commands WHERE id IN(SELECT id FROM selected) RETURNING result_bytes) SELECT COALESCE(sum(result_bytes),0)::bigint,count(*)::bigint FROM deleted`,
	}
	batch := 5000
	for _, query := range queries {
		for usage > target || before != nil {
			if ctx.Err() != nil {
				return usage, ctx.Err()
			}
			tx, e := g.pool.Begin(ctx)
			if e != nil {
				return usage, e
			}
			if _, e = tx.Exec(ctx, `SET LOCAL lock_timeout='5s'; SET LOCAL statement_timeout='10s'`); e != nil {
				tx.Rollback(ctx)
				return usage, e
			}
			var bytes, count int64
			e = tx.QueryRow(ctx, query, before, batch).Scan(&bytes, &count)
			if e == nil {
				e = tx.Commit(ctx)
			} else {
				tx.Rollback(ctx)
			}
			if e != nil {
				var pgerr *pgconn.PgError
				if errors.As(e, &pgerr) && pgerr.Code == "57014" && batch > 1 {
					batch = max(1, batch/2)
					continue
				}
				return usage, e
			}
			usage = max(0, usage-bytes)
			if count == 0 {
				break
			}
			batch = min(5000, batch*2)
		}
	}
	return usage, nil
}
