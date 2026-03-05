package db

import "context"

// AdminRepo handles admin configuration database operations.
type AdminRepo struct {
	db *DB
}

func NewAdminRepo(db *DB) *AdminRepo {
	return &AdminRepo{db: db}
}

// GetPasswordHash reads the admin password hash from admin_config.
func (r *AdminRepo) GetPasswordHash(ctx context.Context) (string, error) {
	var hash string
	err := r.db.Pool.QueryRow(ctx,
		`SELECT value FROM admin_config WHERE key='admin_password_hash'`).Scan(&hash)
	if err != nil {
		return "", err
	}
	return hash, nil
}

// SetPasswordHash upserts the admin password hash in admin_config.
func (r *AdminRepo) SetPasswordHash(ctx context.Context, hash string) error {
	_, err := r.db.Pool.Exec(ctx,
		`INSERT INTO admin_config (key, value, updated_at) VALUES ('admin_password_hash', $1, NOW())
		 ON CONFLICT (key) DO UPDATE SET value=$1, updated_at=NOW()`, hash)
	return err
}
