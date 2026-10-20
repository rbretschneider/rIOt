package db

import "context"

// TerminalRepo handles terminal session audit logging.
type TerminalRepo struct {
	db *DB
}

func NewTerminalRepo(db *DB) *TerminalRepo {
	return &TerminalRepo{db: db}
}

// LogSessionStart records the start of a terminal session.
func (r *TerminalRepo) LogSessionStart(ctx context.Context, deviceID, containerID, sessionID, remoteAddr string) error {
	_, err := r.db.Pool.Exec(ctx,
		`INSERT INTO docker_audit(device_id,kind,action,data) VALUES ($1,'terminal','open',jsonb_build_object('container',$2::text,'session_id',$3::text,'remote_addr',$4::text))`,
		deviceID, containerID, sessionID, remoteAddr)
	return err
}

// LogSessionEnd records the end of a terminal session.
func (r *TerminalRepo) LogSessionEnd(ctx context.Context, sessionID string) error {
	_, err := r.db.Pool.Exec(ctx,
		`UPDATE docker_audit SET data=jsonb_set(data,'{ended_at}',to_jsonb(now())) WHERE kind='terminal' AND data->>'session_id'=$1 AND data->>'ended_at' IS NULL`, sessionID)
	return err
}
