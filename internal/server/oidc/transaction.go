package oidc

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

const (
	// TransactionCookieName is the name of the ephemeral OIDC login-transaction cookie.
	TransactionCookieName = "riot_oidc_tx"
	// TransactionCookiePath scopes the cookie off every other request in the app.
	TransactionCookiePath = "/api/v1/auth/oidc"
	// TransactionMaxAgeSeconds is the cookie's maximum age and the transaction's lifetime.
	TransactionMaxAgeSeconds = 300

	transactionKeyLabel = "riot-oidc-tx-v1"
)

// Exported sentinels so tests can assert *which* check rejected a value
// (AD-005). The browser never sees the difference between them — both map to
// sso_expired — but the log reason and the test assertions do.
var (
	ErrTransactionMAC       = errors.New("oidc: transaction MAC invalid")
	ErrTransactionMalformed = errors.New("oidc: transaction malformed")
	ErrTransactionExpired   = errors.New("oidc: transaction expired")
)

// Transaction carries the state, nonce, PKCE verifier, and intended
// post-login return path for the duration of a login round trip. Never
// persisted to the database (FRD §7.1) — lives only in the riot_oidc_tx
// cookie for at most TransactionMaxAgeSeconds.
type Transaction struct {
	State        string `json:"s"`
	Nonce        string `json:"n"`
	CodeVerifier string `json:"v"`
	ReturnPath   string `json:"r"`
	IssuedAt     int64  `json:"t"` // unix seconds
}

// DeriveTransactionKey derives the transaction MAC key from the JWT signing
// secret with a fixed, distinct context label (AD-005 / SEC-004), so that a
// riot_session JWT (signed with the raw secret) can never validate as a
// riot_oidc_tx value and vice versa, independent of encoding or parse order.
// The raw jwtSecret must never be passed directly to Encode/DecodeTransaction.
func DeriveTransactionKey(jwtSecret []byte) []byte {
	mac := hmac.New(sha256.New, jwtSecret)
	mac.Write([]byte(transactionKeyLabel))
	return mac.Sum(nil)
}

// Encode serializes and MACs the transaction:
// base64url(JSON(payload)) + "." + base64url(HMAC-SHA256(txKey, base64url(JSON(payload)))).
func (t Transaction) Encode(txKey []byte) (string, error) {
	payload, err := json.Marshal(t)
	if err != nil {
		return "", err
	}
	payloadB64 := base64.RawURLEncoding.EncodeToString(payload)

	mac := hmac.New(sha256.New, txKey)
	mac.Write([]byte(payloadB64))
	sigB64 := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))

	return payloadB64 + "." + sigB64, nil
}

// DecodeTransaction verifies and decodes a transaction value. Verification
// order is a contract (AD-005):
//
//  1. Value present, else ErrTransactionMalformed.
//  2. Split on the LAST '.'; compute the MAC over the prefix; hmac.Equal — a
//     mismatch is ErrTransactionMAC. This runs BEFORE any decoding, so a
//     value that is not a transaction at all (e.g. a riot_session JWT) is
//     rejected on cryptographic grounds, never on a downstream parse
//     accident.
//  3. base64.RawURLEncoding decode + JSON unmarshal, else ErrTransactionMalformed.
//  4. All five fields non-empty and IssuedAt > 0, else ErrTransactionMalformed.
//  5. now.Unix() - IssuedAt <= TransactionMaxAgeSeconds, else ErrTransactionExpired.
func DecodeTransaction(raw string, txKey []byte, now time.Time) (Transaction, error) {
	var zero Transaction

	if raw == "" {
		return zero, ErrTransactionMalformed
	}

	idx := strings.LastIndex(raw, ".")
	if idx < 0 {
		return zero, ErrTransactionMalformed
	}
	payloadB64 := raw[:idx]
	macPart := raw[idx+1:]

	expectedMAC := hmac.New(sha256.New, txKey)
	expectedMAC.Write([]byte(payloadB64))
	expectedSig := expectedMAC.Sum(nil)

	givenSig, err := base64.RawURLEncoding.DecodeString(macPart)
	if err != nil || !hmac.Equal(expectedSig, givenSig) {
		return zero, ErrTransactionMAC
	}

	jsonBytes, err := base64.RawURLEncoding.DecodeString(payloadB64)
	if err != nil {
		return zero, ErrTransactionMalformed
	}

	var t Transaction
	if err := json.Unmarshal(jsonBytes, &t); err != nil {
		return zero, ErrTransactionMalformed
	}

	if t.State == "" || t.Nonce == "" || t.CodeVerifier == "" || t.ReturnPath == "" || t.IssuedAt <= 0 {
		return zero, ErrTransactionMalformed
	}

	if now.Unix()-t.IssuedAt > TransactionMaxAgeSeconds {
		return zero, ErrTransactionExpired
	}

	return t, nil
}
