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
		`INSERT INTO terminal_sessions (device_id, container_id, session_id, remote_addr, started_at)
		 VALUES ($1, $2, $3, $4, NOW())`,
		deviceID, containerID, sessionID, remoteAddr)
	return err
}

// LogSessionEnd records the end of a terminal session.
func (r *TerminalRepo) LogSessionEnd(ctx context.Context, sessionID string) error {
	_, err := r.db.Pool.Exec(ctx,
		`UPDATE terminal_sessions SET ended_at=NOW() WHERE session_id=$1 AND ended_at IS NULL`, sessionID)
	return err
}
