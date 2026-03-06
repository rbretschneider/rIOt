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

// GetConfig reads a single key from admin_config.
func (r *AdminRepo) GetConfig(ctx context.Context, key string) (string, error) {
	var val string
	err := r.db.Pool.QueryRow(ctx,
		`SELECT value FROM admin_config WHERE key=$1`, key).Scan(&val)
	if err != nil {
		return "", err
	}
	return val, nil
}

// SetConfig upserts a key-value pair in admin_config.
func (r *AdminRepo) SetConfig(ctx context.Context, key, value string) error {
	_, err := r.db.Pool.Exec(ctx,
		`INSERT INTO admin_config (key, value, updated_at) VALUES ($1, $2, NOW())
		 ON CONFLICT (key) DO UPDATE SET value=$2, updated_at=NOW()`, key, value)
	return err
}

// GetConfigMap reads multiple keys from admin_config in one query.
func (r *AdminRepo) GetConfigMap(ctx context.Context, keys []string) (map[string]string, error) {
	result := make(map[string]string)
	rows, err := r.db.Pool.Query(ctx,
		`SELECT key, value FROM admin_config WHERE key = ANY($1)`, keys)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		result[k] = v
	}
	return result, rows.Err()
}

// IsSetupComplete checks whether initial setup has been completed.
func (r *AdminRepo) IsSetupComplete(ctx context.Context) (bool, error) {
	val, err := r.GetConfig(ctx, "setup_complete")
	if err != nil {
		return false, nil // not found means not complete
	}
	return val == "true", nil
}

// GetServerTLSCert retrieves stored server TLS cert and key PEM from admin_config.
func (r *AdminRepo) GetServerTLSCert(ctx context.Context) (certPEM, keyPEM string, err error) {
	m, err := r.GetConfigMap(ctx, []string{"tls_cert_pem", "tls_key_pem"})
	if err != nil {
		return "", "", err
	}
	return m["tls_cert_pem"], m["tls_key_pem"], nil
}

// StoreServerTLSCert persists server TLS cert and key PEM in admin_config.
func (r *AdminRepo) StoreServerTLSCert(ctx context.Context, certPEM, keyPEM string) error {
	if err := r.SetConfig(ctx, "tls_cert_pem", certPEM); err != nil {
		return err
	}
	return r.SetConfig(ctx, "tls_key_pem", keyPEM)
}
