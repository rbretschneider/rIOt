// Package storage measures fleet storage budgets and preserves disk headroom.
package storage

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/DesyncTheThird/rIOt/internal/server/partition"
	"github.com/DesyncTheThird/rIOt/internal/server/quota"
	"github.com/jackc/pgx/v5/pgxpool"
)

const GiB int64 = 1 << 30

type Options struct {
	ProbePath                                                                string
	DBMaxGB, LowGB, EmergencyGB, LogBudgetGB, CommandBudgetGB, RetentionDays int
}
type State struct {
	Source                 string `json:"source"`
	Mode                   string `json:"mode"`
	FreeBytes              int64  `json:"free_bytes"`
	TotalBytes             int64  `json:"total_bytes"`
	RetentionLimitedBy     string `json:"retention_limited_by,omitempty"`
	EffectiveRetentionDays int    `json:"effective_retention_days"`
}
type Guard struct {
	pool                     *pgxpool.Pool
	partitions               *partition.Manager
	quota                    *quota.Manager
	opts                     Options
	mu                       sync.Mutex
	state                    State
	previousDB, previousUsed int64
	previousAt, recoverSince time.Time
	fairnessSince            time.Time
	limited                  map[string]bool
	eventActive              map[string]bool
}

func New(pool *pgxpool.Pool, p *partition.Manager, q *quota.Manager, o Options) *Guard {
	return &Guard{pool: pool, partitions: p, quota: q, opts: o, state: State{Source: "none", Mode: "normal", EffectiveRetentionDays: o.RetentionDays}, limited: map[string]bool{}, eventActive: map[string]bool{}}
}
func (g *Guard) State() State { g.mu.Lock(); defer g.mu.Unlock(); return g.state }
func Thresholds(total int64, lowGB, emergencyGB int) (low, emergency int64) {
	low = max(4*GiB, min(total*15/100, 50*GiB))
	emergency = max(GiB, min(total*5/100, 10*GiB))
	if lowGB > 0 {
		low = int64(lowGB) * GiB
	}
	if emergencyGB > 0 {
		emergency = int64(emergencyGB) * GiB
	}
	return
}
func Mode(free, total int64, lowGB, emergencyGB int) string {
	low, emergency := Thresholds(total, lowGB, emergencyGB)
	if free < 512<<20 {
		return "critical"
	}
	if free < emergency {
		return "emergency"
	}
	if free < low {
		return "low"
	}
	return "normal"
}
func ProbeMatches(db, used, previousDB, previousUsed int64) bool {
	if db > used {
		return false
	}
	return db-previousDB < 4<<20 || used-previousUsed >= (db-previousDB)/2
}
func (g *Guard) measure(ctx context.Context, now time.Time) (State, error) {
	s := State{Source: "none", Mode: "normal", EffectiveRetentionDays: g.opts.RetentionDays}
	var database int64
	if err := g.pool.QueryRow(ctx, `SELECT pg_database_size(current_database())`).Scan(&database); err != nil {
		return s, err
	}
	if g.opts.ProbePath != "" {
		free, total, err := diskSpace(g.opts.ProbePath)
		if err == nil {
			used := total - free
			prevDB, prevUsed := g.previousDB, g.previousUsed
			if g.previousAt.IsZero() {
				prevDB = database
				prevUsed = used
			}
			if ProbeMatches(database, used, prevDB, prevUsed) {
				s.Source = "statfs"
				s.FreeBytes = free
				s.TotalBytes = total
			} else {
				s.Source = "mismatch"
			}
			if g.previousAt.IsZero() || now.Sub(g.previousAt) >= 5*time.Minute {
				g.previousDB = database
				g.previousUsed = used
				g.previousAt = now
			}
		} else {
			slog.Warn("database filesystem probe unavailable")
		}
	}
	if s.Source != "statfs" && g.opts.DBMaxGB > 0 {
		s.Source = "configured"
		s.TotalBytes = int64(g.opts.DBMaxGB) * GiB
		s.FreeBytes = max(0, s.TotalBytes-database)
	}
	if s.Source == "statfs" || s.Source == "configured" {
		s.Mode = Mode(s.FreeBytes, s.TotalBytes, g.opts.LowGB, g.opts.EmergencyGB)
	}
	return s, nil
}
func (g *Guard) logBytes(ctx context.Context) (int64, error) {
	var size int64
	err := g.pool.QueryRow(ctx, `SELECT COALESCE(sum(pg_total_relation_size(c.oid)),0)::bigint FROM pg_class c JOIN pg_inherits i ON i.inhrelid=c.oid WHERE i.inhparent='container_logs'::regclass`).Scan(&size)
	return size, err
}
func (g *Guard) transition(ctx context.Context, key string, active bool, device string) {
	if !active {
		delete(g.eventActive, key+device)
		return
	}
	if g.eventActive[key+device] {
		return
	}
	g.eventActive[key+device] = true
	_, err := g.pool.Exec(ctx, `INSERT INTO events(device_id,type,severity,message,created_at) SELECT id,$2,'warning',$3,now() FROM devices WHERE $1='' OR id=$1 ORDER BY created_at LIMIT 1`, device, key, "Storage guard: "+key)
	if err != nil {
		slog.Warn("storage event could not be recorded", "type", key)
	}
}
func (g *Guard) fairShares(ctx context.Context, budget int64, now time.Time) error {
	rows, err := g.pool.Query(ctx, `SELECT device_id::text,avg((state->'buckets'->'logs'->>'used')::bigint)::bigint FROM device_storage_usage WHERE day>=$1::date GROUP BY device_id ORDER BY avg((state->'buckets'->'logs'->>'used')::bigint)`, now.UTC().AddDate(0, 0, -2))
	if err != nil {
		return err
	}
	defer rows.Close()
	type use struct {
		id    string
		bytes int64
	}
	uses := []use{}
	for rows.Next() {
		var u use
		if err = rows.Scan(&u.id, &u.bytes); err != nil {
			return err
		}
		uses = append(uses, u)
	}
	if err = rows.Err(); err != nil {
		return err
	}
	if len(uses) == 0 {
		return nil
	}
	median := uses[len(uses)/2].bytes
	share := budget * 9 / 10 / int64(max(1, g.opts.RetentionDays+1)) / int64(len(uses))
	for _, u := range uses {
		if u.bytes > share && u.bytes > median {
			g.quota.SetLogAllowance(u.id, max(share, 32<<20), now)
			g.limited[u.id] = true
			g.transition(ctx, "log_fairness_limited", true, u.id)
		}
	}
	return nil
}
func (g *Guard) Check(ctx context.Context, now time.Time) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	ctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	s, err := g.measure(ctx, now)
	if err != nil {
		return err
	}
	previous := g.state.Mode
	if s.Mode == "normal" && (previous == "emergency" || previous == "critical" || previous == "low") {
		if g.recoverSince.IsZero() {
			g.recoverSince = now
		}
		if now.Sub(g.recoverSince) < 10*time.Minute {
			s.Mode = previous
		}
	} else if s.Mode != "normal" {
		g.recoverSince = time.Time{}
	}
	g.quota.SetMode(s.Mode)
	g.transition(ctx, "db_disk_low", s.Mode != "normal", "")
	g.transition(ctx, "db_disk_emergency", s.Mode == "emergency" || s.Mode == "critical", "")
	if s.Mode == "emergency" || s.Mode == "critical" {
		slog.Error("database disk headroom emergency", "free_bytes", s.FreeBytes)
		g.cancelOwnBlockers(ctx)
	}
	usage, err := g.logBytes(ctx)
	if err != nil {
		return err
	}
	budget := int64(max(g.opts.LogBudgetGB, 1)) * GiB
	if s.Source == "statfs" || s.Source == "configured" {
		budget = min(budget, (usage+s.FreeBytes)/2)
	}
	if usage > budget {
		if err = g.fairShares(ctx, budget, now); err != nil {
			return err
		}
		g.fairnessSince = time.Time{}
	} else if usage <= budget*8/10 {
		if g.fairnessSince.IsZero() {
			g.fairnessSince = now
		}
		if now.Sub(g.fairnessSince) >= 24*time.Hour {
			for id := range g.limited {
				g.quota.SetLogAllowance(id, 512<<20, now)
				delete(g.limited, id)
			}
		}
	}

	pressure := s.Mode != "normal"
	if usage > budget*11/10 || pressure {
		for n := 0; n < 24; n++ {
			if usage <= budget*9/10 && !pressure {
				break
			}
			dropped, e := g.partitions.EvictOldest(ctx, now, 2, "container_logs", 1)
			if e != nil {
				g.reportBlockers(ctx)
				return e
			}
			if dropped == 0 {
				break
			}
			if pressure {
				s.RetentionLimitedBy = "disk_headroom"
			} else {
				s.RetentionLimitedBy = "storage_budget"
			}
			usage, err = g.logBytes(ctx)
			if err != nil {
				return err
			}
			if pressure {
				measured, e := g.measure(ctx, now)
				if e != nil {
					return e
				}
				low, _ := Thresholds(measured.TotalBytes, g.opts.LowGB, g.opts.EmergencyGB)
				pressure = measured.FreeBytes < low
				s.FreeBytes = measured.FreeBytes
			}
		}
	}
	commandUsage, err := g.commandBytes(ctx)
	if err != nil {
		return err
	}
	commandBudget := int64(max(g.opts.CommandBudgetGB, 1)) * GiB
	if s.Source == "statfs" || s.Source == "configured" {
		commandBudget = min(commandBudget, (commandUsage+s.FreeBytes)/2)
	}
	if commandUsage > commandBudget {
		if _, err = g.purgeCommandResults(ctx, commandBudget*9/10, nil); err != nil {
			return err
		}
	}

	if s.Mode == "emergency" || s.Mode == "critical" {
		for n := 0; n < 24; n++ {
			measured, e := g.measure(ctx, now)
			if e != nil {
				return e
			}
			low, _ := Thresholds(measured.TotalBytes, g.opts.LowGB, g.opts.EmergencyGB)
			if measured.FreeBytes >= low {
				break
			}
			dropped, e := g.partitions.EvictOldest(ctx, now, 2, "", 1)
			if e != nil {
				g.reportBlockers(ctx)
				return e
			}
			if dropped == 0 {
				break
			}
			s.RetentionLimitedBy = "disk_headroom"
		}
		// Re-check after partition drops; row purges preserve a seven-day floor.
		measured, measureErr := g.measure(ctx, now)
		if measureErr != nil {
			return measureErr
		}
		low, _ := Thresholds(measured.TotalBytes, g.opts.LowGB, g.opts.EmergencyGB)
		if measured.FreeBytes < low {
			floor := now.AddDate(0, 0, -7)
			if _, err = g.purgeCommandResults(ctx, -1, &floor); err != nil {
				return err
			}
			// DELETE makes existing pages reusable. Partition drops above release disk.
			_, err = g.pool.Exec(ctx, `DELETE FROM events WHERE id IN(SELECT id FROM events WHERE created_at<$1 ORDER BY created_at LIMIT 1000)`, now.AddDate(0, 0, -7))
			if err != nil {
				return err
			}
		}
		if s.Mode == "critical" && measured.FreeBytes < 512<<20 {
			if _, err = g.partitions.EvictOldest(ctx, now, 1, "", 24); err != nil {
				g.reportBlockers(ctx)
				return err
			}
			s.RetentionLimitedBy = "disk_headroom"
		}
	}
	if s.RetentionLimitedBy == "" && g.state.RetentionLimitedBy != "" {
		s.RetentionLimitedBy = g.state.RetentionLimitedBy
	}
	if s.RetentionLimitedBy != "" {
		var oldest *time.Time
		if err = g.pool.QueryRow(ctx, `SELECT min(to_date(substring(c.relname from '_p([0-9]{8})$'),'YYYYMMDD')) FROM pg_class c JOIN pg_inherits i ON i.inhrelid=c.oid WHERE i.inhparent='container_logs'::regclass AND c.relname ~ '_p[0-9]{8}$'`).Scan(&oldest); err == nil && oldest != nil {
			s.EffectiveRetentionDays = min(g.opts.RetentionDays, max(1, int(now.Sub(*oldest).Hours()/24)))
		}
	}
	if s.EffectiveRetentionDays >= g.opts.RetentionDays {
		s.RetentionLimitedBy = ""
	}
	g.state = s
	return nil
}

// Reports never contain SQL text or connection identifiers. Cancellation is
// restricted to active, old transactions of this server's own role and app.
func (g *Guard) blockers(ctx context.Context, cancel bool) {
	rows, err := g.pool.Query(ctx, `SELECT DISTINCT a.pid,a.application_name,a.state,EXTRACT(epoch FROM(now()-a.xact_start))::bigint,a.usename=current_user FROM pg_stat_activity a JOIN pg_locks l ON l.pid=a.pid JOIN pg_class c ON c.oid=l.relation WHERE l.granted AND c.relname LIKE 'container_logs%' AND a.pid<>pg_backend_pid() LIMIT 16`)
	if err != nil {
		return
	}
	type b struct {
		pid        int32
		app, state string
		age        *int64
		own        bool
	}
	all := []b{}
	for rows.Next() {
		var v b
		if rows.Scan(&v.pid, &v.app, &v.state, &v.age, &v.own) == nil {
			all = append(all, v)
		}
	}
	rows.Close()
	for _, v := range all {
		if cancel && v.own && v.app == "riot-server" && v.state == "active" && v.age != nil && *v.age > 30 {
			g.pool.Exec(ctx, `SELECT pg_cancel_backend($1)`, v.pid)
		}
		if !cancel {
			slog.Warn("partition rotation blocked", "pid", v.pid, "application", v.app, "state", v.state, "transaction_age_s", v.age, "own_role", v.own)
		}
	}
}
func (g *Guard) reportBlockers(ctx context.Context)    { g.blockers(ctx, false) }
func (g *Guard) cancelOwnBlockers(ctx context.Context) { g.blockers(ctx, true) }
