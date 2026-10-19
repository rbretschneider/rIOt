package db

import (
	"context"
	"github.com/DesyncTheThird/rIOt/internal/models"
	"github.com/jackc/pgx/v5"
	"time"
)

type DockerActivityFilter struct {
	Device, Container, Stack, Actor, Kind string
	States                                []string
	From, To, Before                      time.Time
	Limit                                 int
	BeforeKind, BeforeID                  string
	TargetKeys                            []string
	Members                               []string
}

func (r *DockerRepo) ActivityCommands(ctx context.Context, f DockerActivityFilter) ([]models.Command, error) {
	if f.States == nil {
		f.States = []string{}
	}
	return dockerRead(ctx, r.db, func(tx pgx.Tx) ([]models.Command, error) {
		rows, err := tx.Query(ctx, `SELECT `+commandColumns+` FROM commands WHERE action LIKE 'docker_%' AND ($1='' OR device_id=$1) AND created_at>=$2 AND created_at<=$3 AND ($4::timestamptz IS NULL OR created_at<$4 OR (created_at=$4 AND $11='command' AND id::text<$12)) AND ($5='' OR target_keys && $13::text[]) AND ($6='' OR params->>'compose_project'=$6) AND (cardinality($7::text[])=0 OR status=ANY($7)) AND ($8='' OR actor_kind=ANY(string_to_array($8,',')) OR ($9<>'' AND actor_sub=$9)) ORDER BY created_at DESC,id DESC LIMIT $10`, f.Device, f.From, f.To, nullableTime(f.Before), f.Container, f.Stack, f.States, f.Actor, actorSubject(f.Actor), f.Limit, f.BeforeKind, f.BeforeID, f.TargetKeys)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		return scanCommands(rows)

	})
}
func nullableTime(t time.Time) interface{} {
	if t.IsZero() {
		return nil
	}
	return t
}
func actorSubject(s string) string {
	if len(s) > 4 && s[:4] == "sub:" {
		return s[4:]
	}
	return ""
}
func (r *DockerRepo) ActivityEvents(ctx context.Context, f DockerActivityFilter) ([]models.Event, error) {
	if f.Members == nil {
		f.Members = []string{}
	}
	return dockerRead(ctx, r.db, func(tx pgx.Tx) ([]models.Event, error) {
		rows, err := tx.Query(ctx, `SELECT id,device_id,type,severity,message,container_id,container_name,created_at,acknowledged_at,command_id,command_attr FROM events WHERE type LIKE 'container_%' AND command_id='' AND ($1='' OR device_id=$1) AND created_at>=$2 AND created_at<=$3 AND ($4::timestamptz IS NULL OR created_at<$4 OR (created_at=$4 AND ($7='command' OR ($7='event' AND id::text<$8)))) AND ($5='' OR container_name=$5) AND (cardinality($9::text[])=0 OR container_name=ANY($9)) ORDER BY created_at DESC,id::text DESC LIMIT $6`, f.Device, f.From, f.To, nullableTime(f.Before), f.Container, f.Limit, f.BeforeKind, f.BeforeID, f.Members)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		return scanEvents(rows)

	})
}

func (r *DockerRepo) CommandEvents(ctx context.Context, device, id string) ([]models.Event, error) {
	return dockerRead(ctx, r.db, func(tx pgx.Tx) ([]models.Event, error) {
		rows, err := tx.Query(ctx, `SELECT id,device_id,type,severity,message,container_id,container_name,created_at,acknowledged_at,command_id,command_attr FROM events WHERE device_id=$1 AND command_id=$2 ORDER BY created_at,id LIMIT 1000`, device, id)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		return scanEvents(rows)
	})
}
