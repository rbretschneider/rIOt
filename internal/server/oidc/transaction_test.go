package oidc

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testTxKey() []byte {
	return DeriveTransactionKey([]byte("test-jwt-secret"))
}

func validTransaction(issuedAt int64) Transaction {
	return Transaction{
		State:        "state-value",
		Nonce:        "nonce-value",
		CodeVerifier: "verifier-value",
		ReturnPath:   "/",
		IssuedAt:     issuedAt,
	}
}

func TestTransaction_EncodeDecode_RoundTrip(t *testing.T) {
	key := testTxKey()
	now := time.Unix(1_800_000_000, 0)
	tx := validTransaction(now.Unix())

	encoded, err := tx.Encode(key)
	require.NoError(t, err)

	decoded, err := DecodeTransaction(encoded, key, now)
	require.NoError(t, err)
	assert.Equal(t, tx, decoded)
}

// [AC-015] Missing or expired transaction is rejected: an absent transaction
// value is malformed (the handler maps this, along with MAC failure, to
// sso_expired/no_transaction).
func TestAC015_DecodeTransaction_Absent(t *testing.T) {
	_, err := DecodeTransaction("", testTxKey(), time.Now())
	assert.ErrorIs(t, err, ErrTransactionMalformed)
}

// [AC-015] A transaction older than its 300s lifetime is expired.
func TestAC015_DecodeTransaction_ExpiredJustOverBoundary(t *testing.T) {
	key := testTxKey()
	issued := time.Unix(1_800_000_000, 0)
	now := issued.Add(301 * time.Second)
	tx := validTransaction(issued.Unix())
	encoded, err := tx.Encode(key)
	require.NoError(t, err)

	_, err = DecodeTransaction(encoded, key, now)
	assert.ErrorIs(t, err, ErrTransactionExpired)
}

// [AC-015] A transaction presented exactly at the 300s boundary is still accepted.
func TestAC015_DecodeTransaction_ExactBoundaryAccepted(t *testing.T) {
	key := testTxKey()
	issued := time.Unix(1_800_000_000, 0)
	now := issued.Add(300 * time.Second)
	tx := validTransaction(issued.Unix())
	encoded, err := tx.Encode(key)
	require.NoError(t, err)

	decoded, err := DecodeTransaction(encoded, key, now)
	require.NoError(t, err)
	assert.Equal(t, tx, decoded)
}

// [AC-015] A transaction one second before the boundary is accepted.
func TestAC015_DecodeTransaction_OneSecondBeforeBoundary(t *testing.T) {
	key := testTxKey()
	issued := time.Unix(1_800_000_000, 0)
	now := issued.Add(299 * time.Second)
	tx := validTransaction(issued.Unix())
	encoded, err := tx.Encode(key)
	require.NoError(t, err)

	_, err = DecodeTransaction(encoded, key, now)
	assert.NoError(t, err)
}

// [AC-016] Once a transaction has been consumed/cleared, replaying an empty
// (cleared) value never yields a decodable transaction.
func TestAC016_DecodeTransaction_ClearedValueNeverDecodes(t *testing.T) {
	_, err := DecodeTransaction("", testTxKey(), time.Now())
	assert.Error(t, err, "a cleared transaction cookie must never decode to a valid transaction")
}

// [SEC-004] A riot_session JWT (HS256, signed with the same underlying
// secret) presented as riot_oidc_tx must be rejected AT THE MAC CHECK, not at
// a later decode step — proving the transaction key is cryptographically
// separated from the session signing key (AD-005).
func TestSEC004_RiotSessionJWTAsTransaction_FailsAtMACCheck(t *testing.T) {
	jwtSecret := []byte("shared-secret-material")
	txKey := DeriveTransactionKey(jwtSecret)

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub": "admin",
		"exp": time.Now().Add(24 * time.Hour).Unix(),
		"iat": time.Now().Unix(),
	})
	tokenStr, err := token.SignedString(jwtSecret)
	require.NoError(t, err)

	_, err = DecodeTransaction(tokenStr, txKey, time.Now())
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrTransactionMAC), "must fail at the MAC check, got: %v", err)
}

// [SEC-004] The derived transaction key must not equal the raw JWT secret.
func TestSEC004_DeriveTransactionKey_NotEqualToRawSecret(t *testing.T) {
	secret := []byte("some-jwt-secret-value")
	derived := DeriveTransactionKey(secret)
	assert.NotEqual(t, secret, derived)
}

// Key derivation is deterministic given the same input.
func TestDeriveTransactionKey_Deterministic(t *testing.T) {
	secret := []byte("some-jwt-secret-value")
	a := DeriveTransactionKey(secret)
	b := DeriveTransactionKey(secret)
	assert.Equal(t, a, b)
}

// Different secrets must derive different keys.
func TestDeriveTransactionKey_DifferentSecretsDifferentKeys(t *testing.T) {
	a := DeriveTransactionKey([]byte("secret-a"))
	b := DeriveTransactionKey([]byte("secret-b"))
	assert.NotEqual(t, a, b)
}

// A tampered MAC segment is rejected.
func TestDecodeTransaction_TamperedMAC(t *testing.T) {
	key := testTxKey()
	now := time.Unix(1_800_000_000, 0)
	tx := validTransaction(now.Unix())
	encoded, err := tx.Encode(key)
	require.NoError(t, err)

	idx := len(encoded) - 1
	tampered := encoded[:idx] + flipBase64Char(encoded[idx])

	_, err = DecodeTransaction(tampered, key, now)
	assert.ErrorIs(t, err, ErrTransactionMAC)
}

// A wrong key fails at the MAC check.
func TestDecodeTransaction_WrongKey(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	tx := validTransaction(now.Unix())
	encoded, err := tx.Encode(testTxKey())
	require.NoError(t, err)

	_, err = DecodeTransaction(encoded, DeriveTransactionKey([]byte("different-secret")), now)
	assert.ErrorIs(t, err, ErrTransactionMAC)
}

// A value with no '.' separator is malformed.
func TestDecodeTransaction_NoDotSeparator(t *testing.T) {
	_, err := DecodeTransaction("nodothere", testTxKey(), time.Now())
	assert.ErrorIs(t, err, ErrTransactionMalformed)
}

// A payload segment that fails base64 decoding (but whose MAC matches what
// we compute over the literal prefix string) fails at the decode step, after
// the MAC step has already passed — demonstrating step ordering.
func TestDecodeTransaction_MACValidButPayloadNotBase64(t *testing.T) {
	key := testTxKey()
	prefix := "not-valid-base64!!!"
	mac := hmacSum(key, prefix)
	value := prefix + "." + base64.RawURLEncoding.EncodeToString(mac)

	_, err := DecodeTransaction(value, key, time.Now())
	assert.ErrorIs(t, err, ErrTransactionMalformed)
}

// A well-formed, MAC-valid payload with an empty required field is malformed.
func TestDecodeTransaction_MissingField(t *testing.T) {
	key := testTxKey()
	now := time.Unix(1_800_000_000, 0)
	tx := Transaction{State: "", Nonce: "n", CodeVerifier: "v", ReturnPath: "/", IssuedAt: now.Unix()}
	encoded, err := tx.Encode(key)
	require.NoError(t, err)

	_, err = DecodeTransaction(encoded, key, now)
	assert.ErrorIs(t, err, ErrTransactionMalformed)
}

func flipBase64Char(c byte) string {
	if c == 'A' {
		return "B"
	}
	return "A"
}

func hmacSum(key []byte, msg string) []byte {
	h := hmac.New(sha256.New, key)
	h.Write([]byte(msg))
	return h.Sum(nil)
}
