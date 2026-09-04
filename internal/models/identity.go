package models

import "time"

// ExternalIdentity is an audit record of an OIDC identity that has
// successfully signed in. It is keyed on (Issuer, Subject) and is never
// consulted to make an access decision (OIDC-001 D-4, BR-002, A-8).
type ExternalIdentity struct {
	Issuer        string
	Subject       string
	Email         *string
	EmailVerified *bool
	LoginAt       time.Time // injected clock — used for both first_login_at and last_login_at
}
