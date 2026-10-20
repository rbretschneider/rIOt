package db

import (
	"context"
	"encoding/json"
	"github.com/DesyncTheThird/rIOt/internal/server/quota"
)

type UsageRepo struct{ db *DB }

func NewUsageRepo(db *DB) *UsageRepo { return &UsageRepo{db} }
func (r *UsageRepo) Load(ctx context.Context) ([]quota.Record, error) {
	rows, err := r.db.Pool.Query(ctx, `SELECT DISTINCT ON(device_id) state FROM device_storage_usage ORDER BY device_id,day DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []quota.Record{}
	for rows.Next() {
		var data []byte
		if err = rows.Scan(&data); err != nil {
			return nil, err
		}
		var record quota.Record
		if err = json.Unmarshal(data, &record); err != nil {
			return nil, err
		}
		out = append(out, record)
	}
	return out, rows.Err()
}
func (r *UsageRepo) Save(ctx context.Context, record quota.Record) error {
	data, err := json.Marshal(record)
	if err != nil {
		return err
	}
	_, err = r.db.Pool.Exec(ctx, `INSERT INTO device_storage_usage(device_id,day,updated_at,state) VALUES($1,$2,$3,$4) ON CONFLICT(device_id,day) DO UPDATE SET updated_at=EXCLUDED.updated_at,state=EXCLUDED.state`, record.DeviceID, record.Day, record.At, data)
	return err
}
