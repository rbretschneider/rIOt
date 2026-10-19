package db

import (
	"context"
	"encoding/json"
	"github.com/DesyncTheThird/rIOt/internal/models"
)

func (r *TelemetryRepo) restoreOmittedSections(ctx context.Context, snap *models.TelemetrySnapshot, raw []byte) error {
	if !snap.Data.Trimmed || len(snap.Data.DroppedSections) == 0 {
		return nil
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return err
	}
	for _, key := range snap.Data.DroppedSections {
		var value []byte
		err := r.db.Pool.QueryRow(ctx, `SELECT data->$2 FROM telemetry_snapshots WHERE device_id=$1 AND data ? $2 AND timestamp<=$3 AND timestamp <= now()+interval '5 minutes' ORDER BY timestamp DESC LIMIT 1`, snap.DeviceID, key, snap.Timestamp).Scan(&value)
		if err == nil {
			fields[key] = value
		}
	}
	resolved, err := json.Marshal(fields)
	if err != nil {
		return err
	}
	return json.Unmarshal(resolved, &snap.Data)
}
