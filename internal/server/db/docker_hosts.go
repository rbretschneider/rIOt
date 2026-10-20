package db

import (
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5"
)

type DockerHostProjection struct {
	DeviceID             string
	Available            bool
	Capabilities         []string
	InventoryEnabled     bool
	ContainerLogsEnabled *bool
	SelfRole             string
}

// GetDockerHostProjection extracts just host capabilities and ownership in SQL.
// It never decodes a telemetry snapshot or transfers labels/env to the caller.
func (r *TelemetryRepo) GetDockerHostProjection(ctx context.Context) ([]DockerHostProjection, error) {
	out := []DockerHostProjection{}
	err := r.db.ReadTx(ctx, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT device_id,COALESCE((docker->>'available')::boolean,false),COALESCE(docker->'capabilities','[]'::jsonb),COALESCE((docker->>'inventory_enabled')::boolean,false),(docker->>'container_logs_enabled')::boolean,
 CASE WHEN EXISTS(SELECT 1 FROM jsonb_array_elements(COALESCE(docker->'containers','[]'::jsonb)) c WHERE c->>'image' ILIKE '%riot-server%' OR c->'riot'->>'role'='server' OR c->'labels'->>'riot.role'='server') THEN 'server'
 WHEN EXISTS(SELECT 1 FROM jsonb_array_elements(COALESCE(docker->'containers','[]'::jsonb)) c WHERE c->'riot'->>'role'='database' OR c->'labels'->>'riot.role'='database') THEN 'database' ELSE '' END
 FROM(SELECT DISTINCT ON(device_id) device_id,data->'docker' AS docker FROM telemetry_snapshots WHERE timestamp<=now()+interval '5 minutes' AND NOT COALESCE(data->'dropped_sections' ? 'docker',false) ORDER BY device_id,timestamp DESC) latest`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var row DockerHostProjection
			var raw []byte
			if err = rows.Scan(&row.DeviceID, &row.Available, &raw, &row.InventoryEnabled, &row.ContainerLogsEnabled, &row.SelfRole); err != nil {
				return err
			}
			if err = json.Unmarshal(raw, &row.Capabilities); err != nil {
				return err
			}
			out = append(out, row)
		}
		return rows.Err()
	})
	return out, err
}
