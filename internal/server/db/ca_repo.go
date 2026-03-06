package db

import (
	"context"
	"time"

	"github.com/DesyncTheThird/rIOt/internal/models"
)

// CARepo handles CA, device certificate, and bootstrap key database operations.
type CARepo struct {
	db *DB
}

func NewCARepo(db *DB) *CARepo {
	return &CARepo{db: db}
}

// --- CA ---

func (r *CARepo) GetCA(ctx context.Context) (certPEM, keyPEM string, err error) {
	err = r.db.Pool.QueryRow(ctx,
		`SELECT ca_cert_pem, ca_key_pem FROM ca_config WHERE id = 1`).Scan(&certPEM, &keyPEM)
	return
}

func (r *CARepo) StoreCA(ctx context.Context, certPEM, keyPEM string) error {
	_, err := r.db.Pool.Exec(ctx,
		`INSERT INTO ca_config (id, ca_cert_pem, ca_key_pem) VALUES (1, $1, $2)
		 ON CONFLICT (id) DO UPDATE SET ca_cert_pem = $1, ca_key_pem = $2`,
		certPEM, keyPEM)
	return err
}

// --- Device Certificates ---

func (r *CARepo) StoreCert(ctx context.Context, cert *models.DeviceCert) error {
	return r.db.Pool.QueryRow(ctx,
		`INSERT INTO device_certs (device_id, serial_number, cert_pem, not_before, not_after)
		 VALUES ($1, $2, $3, $4, $5) RETURNING id`,
		cert.DeviceID, cert.SerialNumber, cert.CertPEM, cert.NotBefore, cert.NotAfter).Scan(&cert.ID)
}

func (r *CARepo) GetCertByDevice(ctx context.Context, deviceID string) (*models.DeviceCert, error) {
	var c models.DeviceCert
	err := r.db.Pool.QueryRow(ctx,
		`SELECT id, device_id, serial_number, cert_pem, not_before, not_after, revoked, revoked_at, created_at
		 FROM device_certs WHERE device_id = $1 ORDER BY created_at DESC LIMIT 1`, deviceID).Scan(
		&c.ID, &c.DeviceID, &c.SerialNumber, &c.CertPEM, &c.NotBefore, &c.NotAfter,
		&c.Revoked, &c.RevokedAt, &c.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func (r *CARepo) GetCertBySerial(ctx context.Context, serial string) (*models.DeviceCert, error) {
	var c models.DeviceCert
	err := r.db.Pool.QueryRow(ctx,
		`SELECT id, device_id, serial_number, cert_pem, not_before, not_after, revoked, revoked_at, created_at
		 FROM device_certs WHERE serial_number = $1`, serial).Scan(
		&c.ID, &c.DeviceID, &c.SerialNumber, &c.CertPEM, &c.NotBefore, &c.NotAfter,
		&c.Revoked, &c.RevokedAt, &c.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func (r *CARepo) ListCerts(ctx context.Context) ([]models.DeviceCert, error) {
	rows, err := r.db.Pool.Query(ctx,
		`SELECT id, device_id, serial_number, '', not_before, not_after, revoked, revoked_at, created_at
		 FROM device_certs ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var certs []models.DeviceCert
	for rows.Next() {
		var c models.DeviceCert
		if err := rows.Scan(&c.ID, &c.DeviceID, &c.SerialNumber, &c.CertPEM, &c.NotBefore, &c.NotAfter,
			&c.Revoked, &c.RevokedAt, &c.CreatedAt); err != nil {
			return nil, err
		}
		certs = append(certs, c)
	}
	return certs, nil
}

func (r *CARepo) RevokeCert(ctx context.Context, serial string) error {
	_, err := r.db.Pool.Exec(ctx,
		`UPDATE device_certs SET revoked = true, revoked_at = NOW() WHERE serial_number = $1`, serial)
	return err
}

func (r *CARepo) ListRevokedSerials(ctx context.Context) ([]string, error) {
	rows, err := r.db.Pool.Query(ctx,
		`SELECT serial_number FROM device_certs WHERE revoked = true`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var serials []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		serials = append(serials, s)
	}
	return serials, nil
}

// --- Bootstrap Keys ---

func (r *CARepo) CreateBootstrapKey(ctx context.Context, keyHash, label string, expiresAt time.Time) error {
	_, err := r.db.Pool.Exec(ctx,
		`INSERT INTO bootstrap_keys (key_hash, label, expires_at) VALUES ($1, $2, $3)`,
		keyHash, label, expiresAt)
	return err
}

func (r *CARepo) LookupBootstrapKey(ctx context.Context, keyHash string) (*models.BootstrapKey, error) {
	var k models.BootstrapKey
	err := r.db.Pool.QueryRow(ctx,
		`SELECT key_hash, label, used, COALESCE(used_by_device, ''), created_at, expires_at
		 FROM bootstrap_keys WHERE key_hash = $1`, keyHash).Scan(
		&k.KeyHash, &k.Label, &k.Used, &k.UsedByDevice, &k.CreatedAt, &k.ExpiresAt)
	if err != nil {
		return nil, err
	}
	return &k, nil
}

func (r *CARepo) MarkBootstrapKeyUsed(ctx context.Context, keyHash, deviceID string) error {
	_, err := r.db.Pool.Exec(ctx,
		`UPDATE bootstrap_keys SET used = true, used_by_device = $2 WHERE key_hash = $1`,
		keyHash, deviceID)
	return err
}

func (r *CARepo) ListBootstrapKeys(ctx context.Context) ([]models.BootstrapKey, error) {
	rows, err := r.db.Pool.Query(ctx,
		`SELECT key_hash, label, used, COALESCE(used_by_device, ''), created_at, expires_at
		 FROM bootstrap_keys ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var keys []models.BootstrapKey
	for rows.Next() {
		var k models.BootstrapKey
		if err := rows.Scan(&k.KeyHash, &k.Label, &k.Used, &k.UsedByDevice, &k.CreatedAt, &k.ExpiresAt); err != nil {
			return nil, err
		}
		keys = append(keys, k)
	}
	return keys, nil
}

func (r *CARepo) DeleteBootstrapKey(ctx context.Context, keyHash string) error {
	_, err := r.db.Pool.Exec(ctx, `DELETE FROM bootstrap_keys WHERE key_hash = $1`, keyHash)
	return err
}
