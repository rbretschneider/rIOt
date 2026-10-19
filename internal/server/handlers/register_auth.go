package handlers

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/tls"
	"log/slog"
	"net/http"

	"github.com/DesyncTheThird/rIOt/internal/server/db"
	"github.com/DesyncTheThird/rIOt/internal/server/middleware"
)

// REG-HARDEN-001: proof-of-device for re-registration.
//
// POST /api/v1/devices/register is public. A device UUID is an identifier, not
// a secret, so re-registering an existing device (which updates its record and
// may mint an API key) must be authorized by a credential that proves the
// caller is that device. This file holds the whole decision:
//
//   - collectRegCredentials gathers facts with read-only I/O.
//   - decideReRegistration is pure and encodes the precedence rules.
//   - RegisterDevice must not mutate anything until the decision accepts.

// maxRegistrationBodyBytes caps the registration body. The body is parsed
// before any credential is evaluated (so the device_id is known), which would
// otherwise let an unauthenticated caller make the server decode an unbounded
// payload. The real payload is a few KB.
const maxRegistrationBodyBytes = 1 << 20

// Fixed response bodies. Every 401 and every 403 from the registration
// endpoint uses exactly one of these, regardless of the rejection reason or
// whether the named device exists. They carry no identifiers, key material or
// reason.
const (
	regBodyUnauthorized = `{"error":"unauthorized"}`
	regBodyForbidden    = `{"error":"forbidden"}`
)

// Rejection classes. These appear only in the server log, never in a response.
const (
	rejectNoCredential              = "no-credential"
	rejectUnknownKey                = "unknown-key"
	rejectWrongDeviceKey            = "wrong-device-key"
	rejectWrongDeviceCert           = "wrong-device-cert"
	rejectRegKeyDeviceHasKeys       = "regkey-device-has-keys"
	rejectRegKeyKeycheckError       = "regkey-keycheck-error"
	rejectCertRevoked               = "cert-revoked"
	rejectCertRevocationUnavailable = "cert-revocation-unavailable"
	rejectCertEmptyCN               = "cert-empty-cn"
	rejectCertUnverified            = "cert-unverified"
	rejectNewDeviceRegKeyMismatch   = "new-device-regkey-mismatch"
)

// regPath is the outcome of the re-registration decision.
type regPath int

const (
	regReject401 regPath = iota
	regReject403
	regPathOwnKey // path (a): X-rIOt-Key bound to the named device
	regPathCert   // path (b): handshake-verified, unrevoked client certificate
	regPathRegKey // path (c): registration key, only for a device with no API key
)

// certState describes what the TLS connection says about a client certificate.
// It is used for the rejection log class only; the identity itself comes from
// the returned CN.
type certState int

const (
	certAbsent certState = iota
	certQualifying
	certUnverified
	certEmptyCN
	certRevoked
	certRevocationUnavailable
)

// regPathName is the label logged when a key is issued. It names the proof that
// was accepted, never any credential value.
func regPathName(p regPath) string {
	switch p {
	case regPathOwnKey:
		return "own-key"
	case regPathCert:
		return "certificate"
	case regPathRegKey:
		return "registration-key"
	default:
		return "none"
	}
}

// regCredentials is the set of facts gathered for one re-registration request.
type regCredentials struct {
	// KeyPresented is true when the X-rIOt-Key header is non-empty.
	KeyPresented bool
	// KeyDeviceID is the device the presented key resolves to ("" when the key
	// is absent, unknown, or the lookup failed).
	KeyDeviceID string
	// CertDeviceID is the CN of a qualifying certificate ("" otherwise).
	CertDeviceID string
	CertState    certState
	// RegKeyMatch is true when a registration key is configured and the
	// presented value equals it (constant-time).
	RegKeyMatch bool
	// DeviceKeyless is true when HasAPIKey returned (false, nil). Computed only
	// when RegKeyMatch.
	DeviceKeyless bool
	// KeylessCheckFailed is true when HasAPIKey returned an error.
	KeylessCheckFailed bool
}

// regDecision is the result of decideReRegistration.
type regDecision struct {
	Path        regPath
	MintKey     bool
	RejectClass string
}

// decideReRegistration encodes the re-registration precedence. It is pure: no
// I/O, no clock. namedDeviceID is the canonical ID of the existing device (the
// database value, not the raw request string). The first matching rule wins:
//
//  1. a presented key bound to a different device -> 403
//  2. a qualifying certificate for a different device -> 403
//  3. a key bound to the named device -> accept, no new key
//  4. a qualifying certificate for the named device -> accept, mint a key
//  5. registration key matched and the device has no API key -> accept, mint
//  6. anything else -> 401
//
// Rules 1 and 2 precede every accept rule, so a mixed-identity request is
// refused even when another credential would have succeeded.
func decideReRegistration(namedDeviceID string, c regCredentials) regDecision {
	if c.KeyDeviceID != "" && c.KeyDeviceID != namedDeviceID {
		return regDecision{Path: regReject403, RejectClass: rejectWrongDeviceKey}
	}
	if c.CertDeviceID != "" && c.CertDeviceID != namedDeviceID {
		return regDecision{Path: regReject403, RejectClass: rejectWrongDeviceCert}
	}
	if c.KeyDeviceID == namedDeviceID && c.KeyDeviceID != "" {
		return regDecision{Path: regPathOwnKey, MintKey: false}
	}
	if c.CertDeviceID == namedDeviceID && c.CertDeviceID != "" {
		return regDecision{Path: regPathCert, MintKey: true}
	}
	if c.RegKeyMatch && c.DeviceKeyless {
		return regDecision{Path: regPathRegKey, MintKey: true}
	}

	// Rule 6: reject with 401. Pick the most specific class for the log.
	class := rejectNoCredential
	switch {
	case c.RegKeyMatch && c.KeylessCheckFailed:
		class = rejectRegKeyKeycheckError
	case c.RegKeyMatch:
		class = rejectRegKeyDeviceHasKeys
	case c.CertState == certRevoked:
		class = rejectCertRevoked
	case c.CertState == certRevocationUnavailable:
		class = rejectCertRevocationUnavailable
	case c.CertState == certEmptyCN:
		class = rejectCertEmptyCN
	case c.CertState == certUnverified:
		class = rejectCertUnverified
	case c.KeyPresented:
		class = rejectUnknownKey
	}
	return regDecision{Path: regReject401, RejectClass: class}
}

// collectRegCredentials gathers the facts decideReRegistration needs. It only
// reads: LookupAPIKey, ListRevokedSerials (via qualifyingCertDeviceID) and
// HasAPIKey (only when the registration key matched). Every I/O error removes a
// grant and never adds one.
func (h *Handlers) collectRegCredentials(ctx context.Context, r *http.Request, namedDeviceID, presentedKey, regKey string) regCredentials {
	c := regCredentials{KeyPresented: presentedKey != ""}

	if presentedKey != "" {
		// LookupAPIKey cannot tell "unknown" from "database error"; both leave
		// the key unresolved, which can only remove a grant.
		if id, err := h.devices.LookupAPIKey(ctx, presentedKey); err == nil {
			c.KeyDeviceID = id
		}
	}

	c.CertDeviceID, c.CertState = qualifyingCertDeviceID(ctx, r.TLS, h.caRepo)

	c.RegKeyMatch = regKey != "" && regKeyEqual(presentedKey, regKey)
	if c.RegKeyMatch {
		has, err := h.devices.HasAPIKey(ctx, namedDeviceID)
		switch {
		case err != nil:
			c.KeylessCheckFailed = true
		case !has:
			c.DeviceKeyless = true
		}
	}
	return c
}

// qualifyingCertDeviceID returns the device ID (certificate CN) conferred by the
// request's TLS client certificate, or "" when the certificate confers none.
//
// Identity is read only from the handshake-verified chain
// (cs.VerifiedChains[0][0]), never from cs.PeerCertificates alone and never
// from a request header, so a certificate identity conveyed by a TLS-terminating
// proxy confers nothing. The check fails closed: an unreadable revoked-serials
// list means no identity. It deliberately does not reuse MTLSDeviceAuth, which
// fails open on that error.
//
// caRepo is nil unless mTLS is active for this router; a nil caRepo means
// certificate proof is unavailable.
func qualifyingCertDeviceID(ctx context.Context, cs *tls.ConnectionState, caRepo db.CARepository) (string, certState) {
	if caRepo == nil || cs == nil || len(cs.PeerCertificates) == 0 {
		return "", certAbsent
	}
	if len(cs.VerifiedChains) == 0 || len(cs.VerifiedChains[0]) == 0 {
		return "", certUnverified
	}
	leaf := cs.VerifiedChains[0][0]
	if leaf == nil || leaf.SerialNumber == nil {
		return "", certUnverified
	}
	cn := leaf.Subject.CommonName
	if cn == "" {
		return "", certEmptyCN
	}

	revoked, err := caRepo.ListRevokedSerials(ctx)
	if err != nil {
		return "", certRevocationUnavailable
	}
	serial := leaf.SerialNumber.Text(16)
	for _, s := range revoked {
		if s == serial {
			return "", certRevoked
		}
	}
	return cn, certQualifying
}

// regKeyEqual reports whether provided equals the configured registration key,
// in time independent of where they first differ and of the length of provided.
// An empty configured key never matches (registration key not configured).
//
// Both values are hashed first so ConstantTimeCompare always sees two
// equal-length inputs.
//
// TODO(merge): collapse into HARDEN-002's registrationKeyMatches helper when
// that branch lands (same semantics). Do not declare a second helper with that
// name here.
func regKeyEqual(provided, configured string) bool {
	if configured == "" {
		return false
	}
	p := sha256.Sum256([]byte(provided))
	c := sha256.Sum256([]byte(configured))
	return subtle.ConstantTimeCompare(p[:], c[:]) == 1
}

// writeRegReject writes the fixed 401 or 403 body.
func writeRegReject(w http.ResponseWriter, status int) {
	if status == http.StatusForbidden {
		http.Error(w, regBodyForbidden, http.StatusForbidden)
		return
	}
	http.Error(w, regBodyUnauthorized, http.StatusUnauthorized)
}

// maxLoggedDeviceIDLen bounds the device_id echoed into the log.
const maxLoggedDeviceIDLen = 64

// loggableDeviceID returns the caller-supplied device_id when it is safe to
// write to the (database-persisted) server log, and a fixed placeholder
// otherwise. The device_id is attacker-controlled on the new-device path; a NUL
// or other C0 control character in a persisted log attribute would make the
// batched JSONB insert fail and drop every entry in that batch. Only a short
// string of [A-Za-z0-9._-] (which covers UUIDs) is echoed.
func loggableDeviceID(s string) string {
	if s == "" {
		return ""
	}
	if len(s) > maxLoggedDeviceIDLen {
		return "[invalid]"
	}
	for i := 0; i < len(s); i++ {
		ch := s[i]
		switch {
		case ch >= 'a' && ch <= 'z', ch >= 'A' && ch <= 'Z', ch >= '0' && ch <= '9',
			ch == '-', ch == '_', ch == '.':
		default:
			return "[invalid]"
		}
	}
	return s
}

// logRegReject records one warn-level entry per rejected registration. It takes
// no argument that could carry the X-rIOt-Key value or the registration key, by
// design: callers cannot leak them through this function.
func logRegReject(r *http.Request, suppliedDeviceID, class string, status int) {
	slog.Warn("device registration rejected",
		"device_id", loggableDeviceID(suppliedDeviceID),
		"device_id_len", len(suppliedDeviceID),
		"client", middleware.ClientIP(r),
		"class", class,
		"status", status,
	)
}
