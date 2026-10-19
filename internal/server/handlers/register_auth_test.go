package handlers

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DesyncTheThird/rIOt/internal/models"
	"github.com/DesyncTheThird/rIOt/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// regTestLeaf builds a client certificate literal with the given CN and serial,
// mirroring makeSelfSignedCert in the middleware tests.
func regTestLeaf(cn string, serial int64) *x509.Certificate {
	return &x509.Certificate{
		SerialNumber: big.NewInt(serial),
		Subject:      pkix.Name{CommonName: cn},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
	}
}

// regTestVerifiedTLS is the connection state Go produces after a client
// certificate verified against ClientCAs during the server's own handshake.
func regTestVerifiedTLS(leaf *x509.Certificate) *tls.ConnectionState {
	return &tls.ConnectionState{
		PeerCertificates: []*x509.Certificate{leaf},
		VerifiedChains:   [][]*x509.Certificate{{leaf}},
	}
}

// ---------------------------------------------------------------------------
// Pure decision table (ADD section 8): every row asserts Path, MintKey and
// RejectClass.
// ---------------------------------------------------------------------------

func TestDecideReRegistration_AC003_AC012_DecisionTable(t *testing.T) {
	const named = "dev-named"
	const other = "dev-other"

	rows := []struct {
		name  string
		creds regCredentials
		path  regPath
		mint  bool
		class string
	}{
		{"[AC-003] V-01 key->other => 403",
			regCredentials{KeyPresented: true, KeyDeviceID: other},
			regReject403, false, rejectWrongDeviceKey},
		{"[AC-003] V-01 key->other wins over RK match on a keyless device",
			regCredentials{KeyPresented: true, KeyDeviceID: other, RegKeyMatch: true, DeviceKeyless: true},
			regReject403, false, rejectWrongDeviceKey},
		{"[AC-003] PO-1 key->other + cert->this => 403",
			regCredentials{KeyPresented: true, KeyDeviceID: other, CertDeviceID: named, CertState: certQualifying},
			regReject403, false, rejectWrongDeviceKey},
		{"[AC-003] key->other + cert->other2 => 403 key class first",
			regCredentials{KeyPresented: true, KeyDeviceID: other, CertDeviceID: "dev-third", CertState: certQualifying},
			regReject403, false, rejectWrongDeviceKey},
		{"[AC-012] V-02 cert->other, no key => 403",
			regCredentials{CertDeviceID: other, CertState: certQualifying},
			regReject403, false, rejectWrongDeviceCert},
		{"[AC-012] V-02 cert->other, unknown key => 403",
			regCredentials{KeyPresented: true, CertDeviceID: other, CertState: certQualifying},
			regReject403, false, rejectWrongDeviceCert},
		{"[AC-012] PO-1 key->this + cert->other => 403",
			regCredentials{KeyPresented: true, KeyDeviceID: named, CertDeviceID: other, CertState: certQualifying},
			regReject403, false, rejectWrongDeviceCert},
		{"[AC-012] cert->other wins over RK match on a keyless device",
			regCredentials{KeyPresented: true, CertDeviceID: other, CertState: certQualifying, RegKeyMatch: true, DeviceKeyless: true},
			regReject403, false, rejectWrongDeviceCert},

		{"[AC-001] V-03 key->this => own-key, no mint",
			regCredentials{KeyPresented: true, KeyDeviceID: named},
			regPathOwnKey, false, ""},
		{"[AC-002] key->this with RK configured but not presented => own-key, no mint",
			regCredentials{KeyPresented: true, KeyDeviceID: named, RegKeyMatch: false},
			regPathOwnKey, false, ""},
		{"[AC-009] V-05 cert+key->this => own-key, no mint",
			regCredentials{KeyPresented: true, KeyDeviceID: named, CertDeviceID: named, CertState: certQualifying},
			regPathOwnKey, false, ""},

		{"[AC-008] V-04 cert->this, no key => cert, mint",
			regCredentials{CertDeviceID: named, CertState: certQualifying},
			regPathCert, true, ""},
		{"[AC-010] V-04 cert->this + unknown key => cert, mint",
			regCredentials{KeyPresented: true, CertDeviceID: named, CertState: certQualifying},
			regPathCert, true, ""},
		{"[AC-008] V-04 cert->this + RK on keyless device => cert path precedes reg key",
			regCredentials{KeyPresented: true, CertDeviceID: named, CertState: certQualifying, RegKeyMatch: true, DeviceKeyless: true},
			regPathCert, true, ""},
		{"[AC-008] V-04 cert->this + RK on keyed device => cert, mint",
			regCredentials{KeyPresented: true, CertDeviceID: named, CertState: certQualifying, RegKeyMatch: true},
			regPathCert, true, ""},

		{"[AC-006] V-06 RK keyless => regkey, mint",
			regCredentials{KeyPresented: true, RegKeyMatch: true, DeviceKeyless: true},
			regPathRegKey, true, ""},
		{"[AC-007] V-07 RK has keys => 401",
			regCredentials{KeyPresented: true, RegKeyMatch: true},
			regReject401, false, rejectRegKeyDeviceHasKeys},
		{"[AC-007] RK + keycheck error => 401 (fail closed)",
			regCredentials{KeyPresented: true, RegKeyMatch: true, KeylessCheckFailed: true},
			regReject401, false, rejectRegKeyKeycheckError},

		{"[AC-004] V-08 nothing, open server => 401 no-credential",
			regCredentials{},
			regReject401, false, rejectNoCredential},
		{"[AC-014] V-09 nothing, RK configured => 401 no-credential",
			regCredentials{RegKeyMatch: false},
			regReject401, false, rejectNoCredential},
		{"[AC-005] V-10 unknown key => 401 unknown-key",
			regCredentials{KeyPresented: true},
			regReject401, false, rejectUnknownKey},

		{"[AC-011] V-11 revoked cert only => 401 cert-revoked",
			regCredentials{CertState: certRevoked},
			regReject401, false, rejectCertRevoked},
		{"[AC-011] revoked cert + unknown key => 401 cert-revoked",
			regCredentials{KeyPresented: true, CertState: certRevoked},
			regReject401, false, rejectCertRevoked},
		{"[AC-013] revocation list unavailable => 401",
			regCredentials{CertState: certRevocationUnavailable},
			regReject401, false, rejectCertRevocationUnavailable},
		{"[AC-011] empty CN => 401 cert-empty-cn",
			regCredentials{CertState: certEmptyCN},
			regReject401, false, rejectCertEmptyCN},
		{"[AC-011] unverified cert => 401 cert-unverified",
			regCredentials{CertState: certUnverified},
			regReject401, false, rejectCertUnverified},
		{"[AC-011] revoked cert + RK on keyless device => regkey path (cert confers nothing)",
			regCredentials{KeyPresented: true, CertState: certRevoked, RegKeyMatch: true, DeviceKeyless: true},
			regPathRegKey, true, ""},
		{"[AC-007] revoked cert + RK on keyed device => 401 regkey-device-has-keys",
			regCredentials{KeyPresented: true, CertState: certRevoked, RegKeyMatch: true},
			regReject401, false, rejectRegKeyDeviceHasKeys},
	}

	for _, row := range rows {
		row := row
		t.Run(row.name, func(t *testing.T) {
			dec := decideReRegistration(named, row.creds)
			assert.Equal(t, row.path, dec.Path, "decision path")
			assert.Equal(t, row.mint, dec.MintKey, "MintKey")
			assert.Equal(t, row.class, dec.RejectClass, "RejectClass")
		})
	}
}

// ---------------------------------------------------------------------------
// AC-018: constant-time registration key comparison
// ---------------------------------------------------------------------------

func TestRegKeyEqual_AC018_ConstantTimeCompare(t *testing.T) {
	const rk = "registration-key-0123456789abcdef"

	t.Run("[AC-018] equal values match", func(t *testing.T) {
		assert.True(t, regKeyEqual(rk, rk))
	})
	t.Run("[AC-018] differing in the first byte does not match", func(t *testing.T) {
		assert.False(t, regKeyEqual("X"+rk[1:], rk))
	})
	t.Run("[AC-018] differing in the last byte does not match", func(t *testing.T) {
		assert.False(t, regKeyEqual(rk[:len(rk)-1]+"X", rk))
	})
	t.Run("[AC-018] different lengths do not match", func(t *testing.T) {
		assert.False(t, regKeyEqual(rk+"x", rk), "longer provided value")
		assert.False(t, regKeyEqual(rk[:len(rk)-1], rk), "shorter provided value")
		assert.False(t, regKeyEqual("", rk), "empty provided value")
	})
	t.Run("[AC-018] empty configured key never matches", func(t *testing.T) {
		assert.False(t, regKeyEqual("", ""), "both empty must not match: no key is configured")
		assert.False(t, regKeyEqual("anything", ""))
	})
}

// ---------------------------------------------------------------------------
// AC-011 / AC-013 / C4: certificate identity
// ---------------------------------------------------------------------------

func TestQualifyingCert_AC011_NoIdentityCases(t *testing.T) {
	ctx := context.Background()

	t.Run("[AC-011] qualifying certificate yields its CN", func(t *testing.T) {
		caRepo := testutil.NewMockCARepo()
		id, state := qualifyingCertDeviceID(ctx, regTestVerifiedTLS(regTestLeaf("dev-1", 7)), caRepo)
		assert.Equal(t, "dev-1", id)
		assert.Equal(t, certQualifying, state)
	})

	t.Run("[AC-011] revoked serial confers nothing", func(t *testing.T) {
		caRepo := testutil.NewMockCARepo()
		// Serials are stored as %x (equal to big.Int.Text(16)); 255 -> "ff".
		caRepo.Certs = []models.DeviceCert{{SerialNumber: fmt.Sprintf("%x", 255), Revoked: true}}
		id, state := qualifyingCertDeviceID(ctx, regTestVerifiedTLS(regTestLeaf("dev-1", 255)), caRepo)
		assert.Equal(t, "", id)
		assert.Equal(t, certRevoked, state)
	})

	t.Run("[AC-011] a different serial being revoked does not affect this certificate", func(t *testing.T) {
		caRepo := testutil.NewMockCARepo()
		caRepo.Certs = []models.DeviceCert{{SerialNumber: "ff", Revoked: true}}
		id, state := qualifyingCertDeviceID(ctx, regTestVerifiedTLS(regTestLeaf("dev-1", 254)), caRepo)
		assert.Equal(t, "dev-1", id)
		assert.Equal(t, certQualifying, state)
	})

	t.Run("[AC-011] empty CN confers nothing", func(t *testing.T) {
		id, state := qualifyingCertDeviceID(ctx, regTestVerifiedTLS(regTestLeaf("", 1)), testutil.NewMockCARepo())
		assert.Equal(t, "", id)
		assert.Equal(t, certEmptyCN, state)
	})

	t.Run("[AC-011] PeerCertificates without VerifiedChains confers nothing", func(t *testing.T) {
		cs := &tls.ConnectionState{PeerCertificates: []*x509.Certificate{regTestLeaf("dev-1", 1)}}
		id, state := qualifyingCertDeviceID(ctx, cs, testutil.NewMockCARepo())
		assert.Equal(t, "", id, "an unverified peer certificate must never confer identity")
		assert.Equal(t, certUnverified, state)
	})

	t.Run("[AC-011] empty VerifiedChains entry confers nothing", func(t *testing.T) {
		cs := &tls.ConnectionState{
			PeerCertificates: []*x509.Certificate{regTestLeaf("dev-1", 1)},
			VerifiedChains:   [][]*x509.Certificate{{}},
		}
		id, state := qualifyingCertDeviceID(ctx, cs, testutil.NewMockCARepo())
		assert.Equal(t, "", id)
		assert.Equal(t, certUnverified, state)
	})

	t.Run("[AC-011] identity comes from VerifiedChains, not PeerCertificates", func(t *testing.T) {
		cs := &tls.ConnectionState{
			PeerCertificates: []*x509.Certificate{regTestLeaf("dev-peer", 1)},
			VerifiedChains:   [][]*x509.Certificate{{regTestLeaf("dev-verified", 2)}},
		}
		id, _ := qualifyingCertDeviceID(ctx, cs, testutil.NewMockCARepo())
		assert.Equal(t, "dev-verified", id)
	})

	t.Run("[AC-011] nil TLS (plain HTTP or TLS-terminating proxy) confers nothing", func(t *testing.T) {
		id, state := qualifyingCertDeviceID(ctx, nil, testutil.NewMockCARepo())
		assert.Equal(t, "", id)
		assert.Equal(t, certAbsent, state)
	})

	t.Run("[AC-011] no peer certificates confers nothing", func(t *testing.T) {
		id, state := qualifyingCertDeviceID(ctx, &tls.ConnectionState{}, testutil.NewMockCARepo())
		assert.Equal(t, "", id)
		assert.Equal(t, certAbsent, state)
	})

	t.Run("[AC-011] nil caRepo with a verified chain confers nothing (mTLS inactive)", func(t *testing.T) {
		id, state := qualifyingCertDeviceID(ctx, regTestVerifiedTLS(regTestLeaf("dev-1", 1)), nil)
		assert.Equal(t, "", id, "a stray verified chain must not confer identity on a non-mTLS router")
		assert.Equal(t, certAbsent, state)
	})
}

func TestQualifyingCert_AC013_RevocationListErrorFailsClosed(t *testing.T) {
	caRepo := testutil.NewMockCARepo()
	caRepo.Err = errors.New("db down")
	id, state := qualifyingCertDeviceID(context.Background(), regTestVerifiedTLS(regTestLeaf("dev-1", 1)), caRepo)
	assert.Equal(t, "", id, "an unreadable revoked-serials list must yield no identity")
	assert.Equal(t, certRevocationUnavailable, state)
}

// C4: identity comes only from the TLS connection. Forwarded-certificate
// headers must be ignored entirely.
func TestCollectRegCredentials_AC011_IgnoresForwardedCertHeaders(t *testing.T) {
	devRepo := testutil.NewMockDeviceRepo()
	h := &Handlers{devices: devRepo, caRepo: testutil.NewMockCARepo()}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/devices/register", nil)
	for _, name := range []string{
		"X-Forwarded-Client-Cert", "X-SSL-Client-CN", "X-SSL-Client-DN",
		"X-Client-Cert", "X-Ssl-Cert", "X-Remote-User", "X-SSL-Client-Verify",
	} {
		req.Header.Set(name, "dev-victim")
	}
	req.TLS = nil

	creds := h.collectRegCredentials(context.Background(), req, "dev-victim", "", "")
	assert.Equal(t, "", creds.CertDeviceID, "a header must never confer a certificate identity")
	assert.Equal(t, certAbsent, creds.CertState)
}

// ---------------------------------------------------------------------------
// collectRegCredentials: which reads happen when
// ---------------------------------------------------------------------------

func TestCollectRegCredentials_Facts(t *testing.T) {
	ctx := context.Background()

	t.Run("[AC-002] own key resolves to its device and RK is not consulted without a configured key", func(t *testing.T) {
		devRepo := testutil.NewMockDeviceRepo()
		devRepo.APIKeys["riot_k1"] = "dev-1"
		h := &Handlers{devices: devRepo}
		req := httptest.NewRequest(http.MethodPost, "/", nil)

		c := h.collectRegCredentials(ctx, req, "dev-1", "riot_k1", "")
		assert.True(t, c.KeyPresented)
		assert.Equal(t, "dev-1", c.KeyDeviceID)
		assert.False(t, c.RegKeyMatch, "no registration key configured: RegKeyMatch must be false")
		assert.False(t, c.DeviceKeyless, "HasAPIKey must not be consulted unless RK matched")
	})

	t.Run("[AC-006] RK match on a keyless device sets DeviceKeyless", func(t *testing.T) {
		devRepo := testutil.NewMockDeviceRepo()
		h := &Handlers{devices: devRepo}
		req := httptest.NewRequest(http.MethodPost, "/", nil)

		c := h.collectRegCredentials(ctx, req, "dev-1", "the-reg-key", "the-reg-key")
		assert.True(t, c.RegKeyMatch)
		assert.True(t, c.DeviceKeyless)
		assert.False(t, c.KeylessCheckFailed)
	})

	t.Run("[AC-007] RK match on a keyed device leaves DeviceKeyless false", func(t *testing.T) {
		devRepo := testutil.NewMockDeviceRepo()
		devRepo.APIKeys["riot_k1"] = "dev-1"
		h := &Handlers{devices: devRepo}
		req := httptest.NewRequest(http.MethodPost, "/", nil)

		c := h.collectRegCredentials(ctx, req, "dev-1", "the-reg-key", "the-reg-key")
		assert.True(t, c.RegKeyMatch)
		assert.False(t, c.DeviceKeyless)
	})

	t.Run("[AC-007] HasAPIKey error sets KeylessCheckFailed and never DeviceKeyless", func(t *testing.T) {
		devRepo := testutil.NewMockDeviceRepo()
		devRepo.HasAPIKeyErr = errors.New("boom")
		h := &Handlers{devices: devRepo}
		req := httptest.NewRequest(http.MethodPost, "/", nil)

		c := h.collectRegCredentials(ctx, req, "dev-1", "the-reg-key", "the-reg-key")
		assert.True(t, c.KeylessCheckFailed)
		assert.False(t, c.DeviceKeyless, "a failed key check must fail closed")
	})

	t.Run("[AC-005] LookupAPIKey error leaves the key unresolved", func(t *testing.T) {
		devRepo := testutil.NewMockDeviceRepo()
		devRepo.Err = errors.New("db down")
		h := &Handlers{devices: devRepo}
		req := httptest.NewRequest(http.MethodPost, "/", nil)

		c := h.collectRegCredentials(ctx, req, "dev-1", "riot_k1", "")
		assert.True(t, c.KeyPresented)
		assert.Equal(t, "", c.KeyDeviceID, "a lookup error must only remove a grant")
	})
}

// ---------------------------------------------------------------------------
// C1: caller-supplied device_id in rejection logs
// ---------------------------------------------------------------------------

func TestLoggableDeviceID_C1_NoControlCharacters(t *testing.T) {
	const placeholder = "[invalid]"
	uuidish := "0b1c3f1e-6d2a-4c8e-9a47-1f2e3d4c5b6a"

	t.Run("[AC-020] UUID-shaped values are echoed", func(t *testing.T) {
		assert.Equal(t, uuidish, loggableDeviceID(uuidish))
	})
	t.Run("[AC-020] empty stays empty", func(t *testing.T) {
		assert.Equal(t, "", loggableDeviceID(""))
	})
	for name, in := range map[string]string{
		"NUL first":      "\u0000x",
		"NUL embedded":   "abc\u0000def",
		"newline":        "abc\ndef",
		"carriage ret":   "abc\rdef",
		"escape":         "abc\x1bdef",
		"tab":            "abc\tdef",
		"DEL":            "abc\x7fdef",
		"space":          "abc def",
		"quote":          `abc"def`,
		"non-ASCII":      "dév",
		"overlong value": strings.Repeat("a", maxLoggedDeviceIDLen+1),
	} {
		in := in
		t.Run("[AC-020] "+name+" is replaced by the placeholder", func(t *testing.T) {
			assert.Equal(t, placeholder, loggableDeviceID(in))
		})
	}
	t.Run("[AC-020] value at the length limit is echoed", func(t *testing.T) {
		s := strings.Repeat("a", maxLoggedDeviceIDLen)
		assert.Equal(t, s, loggableDeviceID(s))
	})
}

func TestWriteRegReject_AC014_FixedBodies(t *testing.T) {
	rec401 := httptest.NewRecorder()
	writeRegReject(rec401, http.StatusUnauthorized)
	assert.Equal(t, http.StatusUnauthorized, rec401.Code)
	assert.Equal(t, regBodyUnauthorized+"\n", rec401.Body.String())

	rec403 := httptest.NewRecorder()
	writeRegReject(rec403, http.StatusForbidden)
	assert.Equal(t, http.StatusForbidden, rec403.Code)
	assert.Equal(t, regBodyForbidden+"\n", rec403.Body.String())

	require.NotEqual(t, rec401.Body.String(), rec403.Body.String(), "401 and 403 bodies must differ")
}
