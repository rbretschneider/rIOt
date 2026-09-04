package db

import (
	"context"

	"github.com/DesyncTheThird/rIOt/internal/models"
)

// ExternalIdentityRepo persists the audit-only record of OIDC identities that
// have successfully signed in (OIDC-001 AD-016).
type ExternalIdentityRepo struct {
	db *DB
}

func NewExternalIdentityRepo(db *DB) *ExternalIdentityRepo {
	return &ExternalIdentityRepo{db: db}
}

// RecordLogin upserts the identity keyed on (issuer, subject) and reports
// whether this (issuer, subject) had never been seen before. The upsert and
// the first-seen signal are produced in a single round trip via
// RETURNING (xmax = 0), which is true only for a tuple produced by the
// INSERT and false for one produced by the ON CONFLICT UPDATE (AD-016).
func (r *ExternalIdentityRepo) RecordLogin(ctx context.Context, ident models.ExternalIdentity) (firstSeen bool, err error) {
	err = r.db.Pool.QueryRow(ctx, `
		INSERT INTO external_identities (issuer, subject, email, email_verified, first_login_at, last_login_at)
		VALUES ($1, $2, $3, $4, $5, $5)
		ON CONFLICT (issuer, subject) DO UPDATE
		SET email          = EXCLUDED.email,
		    email_verified = EXCLUDED.email_verified,
		    last_login_at  = EXCLUDED.last_login_at
		RETURNING (xmax = 0) AS inserted
	`, ident.Issuer, ident.Subject, ident.Email, ident.EmailVerified, ident.LoginAt).Scan(&firstSeen)
	if err != nil {
		return false, err
	}
	return firstSeen, nil
}
