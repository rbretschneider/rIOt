package db

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/DesyncTheThird/rIOt/internal/models"
	"github.com/DesyncTheThird/rIOt/internal/redact"
	"github.com/jackc/pgx/v5"
	"time"
)

type DockerRepo struct{ db *DB }

func NewDockerRepo(d *DB) *DockerRepo {
	if d == nil {
		return nil
	}
	return &DockerRepo{d}
}
func (r *DockerRepo) Logs(ctx context.Context, device string, names []string, from, to time.Time, before time.Time, beforeID int64, ascending bool, limit int) ([]models.ContainerLogEntry, error) {
	return dockerRead(ctx, r.db, func(tx pgx.Tx) ([]models.ContainerLogEntry, error) {
		q := `SELECT id,container_id,container_name,timestamp,stream,line,agent_ts FROM container_logs WHERE device_id=$1 AND container_name=ANY($2) AND timestamp>=$3 AND timestamp<=$4 AND ($5::timestamptz IS NULL OR (timestamp,id)<($5,$6)) ORDER BY timestamp DESC,id DESC LIMIT $7`
		var cursor interface{}
		if !before.IsZero() {
			cursor = before
		}
		if ascending {
			q = `SELECT id,container_id,container_name,timestamp,stream,line,agent_ts FROM container_logs WHERE device_id=$1 AND container_name=ANY($2) AND timestamp>=$3 AND timestamp<=$4 AND ($5::timestamptz IS NULL OR (timestamp,id)<($5,$6)) ORDER BY timestamp ASC,id ASC LIMIT $7`
		}
		rows, err := tx.Query(ctx, q, device, names, from, to, cursor, beforeID, limit)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		out := []models.ContainerLogEntry{}
		for rows.Next() {
			var e models.ContainerLogEntry
			if err := rows.Scan(&e.ID, &e.ContainerID, &e.ContainerName, &e.Timestamp, &e.Stream, &e.Line, &e.AgentTS); err != nil {
				return nil, err
			}
			out = append(out, e)
		}
		return out, rows.Err()

	})
}
func (r *DockerRepo) Stack(ctx context.Context, device, project string) (map[string]interface{}, error) {
	return dockerRead(ctx, r.db, func(tx pgx.Tx) (map[string]interface{}, error) {
		var raw []byte
		err := tx.QueryRow(ctx, `SELECT data FROM docker_stacks WHERE device_id=$1 AND project=$2`, device, project).Scan(&raw)
		var out map[string]interface{}
		if err == nil {
			err = json.Unmarshal(raw, &out)
		}
		return out, err

	})
}
func (r *DockerRepo) SaveStack(ctx context.Context, device, project string, v interface{}) error {
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	_, err = r.db.Pool.Exec(ctx, `INSERT INTO docker_stacks(device_id,project,data) SELECT $1,$2,$3 WHERE (SELECT count(*) FROM docker_stacks WHERE device_id=$1)<500 OR EXISTS(SELECT 1 FROM docker_stacks WHERE device_id=$1 AND project=$2) ON CONFLICT(device_id,project) DO UPDATE SET data=EXCLUDED.data,updated_at=now() WHERE docker_stacks.data IS DISTINCT FROM EXCLUDED.data`, device, project, raw)
	return err
}
func (r *DockerRepo) Stacks(ctx context.Context) ([]map[string]interface{}, error) {
	return dockerRead(ctx, r.db, func(tx pgx.Tx) ([]map[string]interface{}, error) {
		rows, err := tx.Query(ctx, `SELECT data FROM docker_stacks ORDER BY device_id,project LIMIT 5000`)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		out := []map[string]interface{}{}
		for rows.Next() {
			var raw []byte
			if err := rows.Scan(&raw); err != nil {
				return nil, err
			}
			var v map[string]interface{}
			if err := json.Unmarshal(raw, &v); err != nil {
				return nil, err
			}
			out = append(out, v)
		}
		return out, rows.Err()

	})
}
func (r *DockerRepo) Audit(ctx context.Context, device, kind, action, subject string, v interface{}) error {
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	_, err = r.db.Pool.Exec(ctx, `INSERT INTO docker_audit(device_id,kind,action,actor_sub,data) VALUES($1,$2,$3,$4,$5)`, device, kind, action, subject, raw)
	return err
}

// SaveInventory treats capability tokens as untrusted: counts, field sizes, total
// serialized bytes and the write cadence are enforced independently on the server.
func (r *DockerRepo) SaveInventory(ctx context.Context, device string, v *models.DockerInventory) error {
	if v == nil {
		return nil
	}
	if v.Unchanged {
		_, err := r.db.Pool.Exec(ctx, `UPDATE docker_inventory SET collected_at=now(),data=jsonb_set(data,'{collected_at}',to_jsonb(now())) WHERE device_id=$1 AND data->>'hash'=$2`, device, models.TruncateHead(v.Hash, 512))
		return err
	}
	bounded := *v
	bounded.Images = append([]models.DockerImage(nil), v.Images...)
	bounded.Volumes = append([]models.DockerVolume(nil), v.Volumes...)
	bounded.Networks = append([]models.DockerNetwork(nil), v.Networks...)
	capSlice := func(n, max int) int {
		if n > max {
			bounded.Truncated = true
			return max
		}
		return n
	}
	bounded.Images = bounded.Images[:capSlice(len(bounded.Images), 2000)]
	bounded.Volumes = bounded.Volumes[:capSlice(len(bounded.Volumes), 2000)]
	bounded.Networks = bounded.Networks[:capSlice(len(bounded.Networks), 500)]
	clean := func(s string) string {
		if len(s) > 512 {
			bounded.Truncated = true
		}
		s = models.TruncateHead(models.SanitizeText(s), 512)
		safe, _ := redact.MaskLine(s)
		return safe
	}
	cleanSlice := func(values []string, limit int) []string {
		copy := append([]string(nil), values...)
		copy = copy[:capSlice(len(copy), limit)]
		for i := range copy {
			copy[i] = clean(copy[i])
		}
		return copy
	}
	bounded.Hash = clean(bounded.Hash)
	usage := map[string]models.DockerDiskUsage{}
	for _, kind := range []string{"images", "volumes", "build_cache", "networks"} {
		if value, ok := bounded.DiskUsage[kind]; ok {
			if value.Total < 0 {
				value.Total = 0
			}
			if value.Reclaimable < 0 {
				value.Reclaimable = 0
			}
			usage[kind] = value
		}
	}
	bounded.DiskUsage = usage
	for i := range bounded.Images {
		item := &bounded.Images[i]
		item.ID = clean(item.ID)
		item.Digests = cleanSlice(item.Digests, 32)
		item.Tags = append([]string(nil), item.Tags...)
		item.Tags = item.Tags[:capSlice(len(item.Tags), 32)]
		for j := range item.Tags {
			item.Tags[j] = clean(item.Tags[j])
		}
		if item.Size < 0 {
			item.Size = 0
		}
	}
	for i := range bounded.Volumes {
		item := &bounded.Volumes[i]
		item.Name = clean(item.Name)
		item.Driver = clean(item.Driver)
		item.Mountpoint = clean(item.Mountpoint)
		item.Project = clean(item.Project)
		item.UsedBy = cleanSlice(item.UsedBy, 64)
		item.CreatedAt = clean(item.CreatedAt)
		if item.Size < 0 {
			item.Size = 0
		}
	}
	for i := range bounded.Networks {
		item := &bounded.Networks[i]
		item.ID = clean(item.ID)
		item.Name = clean(item.Name)
		item.Driver = clean(item.Driver)
		item.Scope = clean(item.Scope)
		item.Containers = cleanSlice(item.Containers, 256)
		item.Subnets = append([]models.DockerSubnet(nil), item.Subnets...)
		item.Subnets = item.Subnets[:capSlice(len(item.Subnets), 16)]
		for j := range item.Subnets {
			item.Subnets[j].Subnet = clean(item.Subnets[j].Subnet)
			item.Subnets[j].Gateway = clean(item.Subnets[j].Gateway)
		}
	}
	bounded.CollectedAt = time.Now().UTC()
	raw, err := json.Marshal(bounded)
	if err != nil {
		return err
	}
	if len(raw) > 4<<20 {
		return errors.New("inventory exceeds maximum size")
	}
	_, err = r.db.Pool.Exec(ctx, `INSERT INTO docker_inventory(device_id,data,collected_at) VALUES($1,$2,$3) ON CONFLICT(device_id) DO UPDATE SET data=EXCLUDED.data,collected_at=EXCLUDED.collected_at WHERE docker_inventory.data-'collected_at'=EXCLUDED.data-'collected_at' OR docker_inventory.collected_at<now()-interval '14 minutes' OR EXISTS(SELECT 1 FROM commands WHERE device_id=$1 AND action='docker_inventory_refresh' AND created_at>now()-interval '2 minutes')`, device, raw, bounded.CollectedAt)
	return err
}

func (r *DockerRepo) Inventory(ctx context.Context, device string) (*models.DockerInventory, error) {
	return dockerRead(ctx, r.db, func(tx pgx.Tx) (*models.DockerInventory, error) {
		var raw []byte
		err := tx.QueryRow(ctx, `SELECT data FROM docker_inventory WHERE device_id=$1`, device).Scan(&raw)
		if err != nil {
			return nil, err
		}
		var v models.DockerInventory
		err = json.Unmarshal(raw, &v)
		return &v, err

	})
}

func dockerRead[T any](ctx context.Context, d *DB, read func(pgx.Tx) (T, error)) (T, error) {
	var result T
	err := d.ReadTx(ctx, func(tx pgx.Tx) error { var err error; result, err = read(tx); return err })
	return result, err
}

func (r *DockerRepo) ObserveStack(ctx context.Context, device, project string, v map[string]interface{}) error {
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if len(raw) > 64<<10 {
		return errors.New("stack exceeds maximum size")
	}
	tx, err := r.db.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('riot.stack.'||$1,0))`, device); err != nil {
		return err
	}
	var changed bool
	err = tx.QueryRow(ctx, `SELECT NOT EXISTS(SELECT 1 FROM docker_stacks WHERE device_id=$1 AND project=$2 AND data-'last_seen_at'=$3::jsonb-'last_seen_at' AND updated_at>now()-interval '1 hour')`, device, project, raw).Scan(&changed)
	if err != nil {
		return err
	}
	if !changed {
		return nil
	}
	var writes int
	err = tx.QueryRow(ctx, `INSERT INTO docker_stack_budget(device_id,writes) VALUES($1,1) ON CONFLICT(device_id) DO UPDATE SET window_at=CASE WHEN docker_stack_budget.window_at<now()-interval '1 hour' THEN now() ELSE docker_stack_budget.window_at END,writes=CASE WHEN docker_stack_budget.window_at<now()-interval '1 hour' THEN 1 ELSE docker_stack_budget.writes+1 END WHERE docker_stack_budget.writes<500 OR docker_stack_budget.window_at<now()-interval '1 hour' RETURNING writes`, device).Scan(&writes)
	if err == pgx.ErrNoRows {
		return nil
	}
	if err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `DELETE FROM docker_stacks WHERE (device_id,project) IN(SELECT device_id,project FROM docker_stacks WHERE device_id=$1 AND data->>'state'='down' AND updated_at<now()-interval '90 days' AND (SELECT count(*) FROM docker_stacks WHERE device_id=$1)>=500 AND NOT EXISTS(SELECT 1 FROM docker_stacks WHERE device_id=$1 AND project=$2) ORDER BY updated_at LIMIT 1)`, device, project); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO docker_stacks(device_id,project,data) SELECT $1,$2,$3 WHERE (SELECT count(*) FROM docker_stacks WHERE device_id=$1)<500 OR EXISTS(SELECT 1 FROM docker_stacks WHERE device_id=$1 AND project=$2) ON CONFLICT(device_id,project) DO UPDATE SET data=EXCLUDED.data,updated_at=now()`, device, project, raw)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (r *DockerRepo) StacksForDevice(ctx context.Context, device string) ([]map[string]interface{}, error) {
	return dockerRead(ctx, r.db, func(tx pgx.Tx) ([]map[string]interface{}, error) {
		rows, err := tx.Query(ctx, `SELECT data FROM docker_stacks WHERE device_id=$1 ORDER BY project LIMIT 500`, device)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		result := []map[string]interface{}{}
		for rows.Next() {
			var raw []byte
			if err := rows.Scan(&raw); err != nil {
				return nil, err
			}
			var row map[string]interface{}
			if err := json.Unmarshal(raw, &row); err != nil {
				return nil, err
			}
			result = append(result, row)
		}
		return result, rows.Err()
	})
}

func (r *DockerRepo) HasLogData(ctx context.Context, device string, now time.Time) (bool, error) {
	return dockerRead(ctx, r.db, func(tx pgx.Tx) (bool, error) {
		var exists bool
		err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM container_logs WHERE device_id=$1 AND timestamp<=$2)`, device, now.Add(5*time.Minute)).Scan(&exists)
		return exists, err
	})
}
