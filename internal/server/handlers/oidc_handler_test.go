package handlers

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DesyncTheThird/rIOt/internal/server/middleware"
	"github.com/DesyncTheThird/rIOt/internal/server/oidc"
	"github.com/DesyncTheThird/rIOt/internal/testutil"
	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"
)

const (
	oidcTestJWTSecret    = "test-jwt-secret-for-oidc-handlers"
	oidcTestClientID     = "riot-client"
	oidcTestClientSecret = "riot-secret"
	oidcTestHost         = "riot.example:7331"
	oidcTestRedirectURI  = "https://" + oidcTestHost + "/api/v1/auth/oidc/callback"
)

// oidcTestSetup bundles everything a handler-level OIDC test needs.
type oidcTestSetup struct {
	h             *Handlers
	externalRepo  *testutil.MockExternalIdentityRepo
	adminRepo     *testutil.MockAdminRepo
	setupComplete *atomic.Bool
}

// newOIDCTestSetup builds a *Handlers wired for OIDC-001 tests. Pass idp=nil
// to leave SSO dormant (no issuer configured); pass clientSecret="" to test
// the partial-configuration case with an issuer otherwise present.
func newOIDCTestSetup(t *testing.T, idp *stubIDP, clientID, clientSecret string, setupComplete bool) *oidcTestSetup {
	t.Helper()

	issuerURL := ""
	if idp != nil {
		issuerURL = idp.issuer()
	}

	var complete atomic.Bool
	complete.Store(setupComplete)

	svc := oidc.New(oidc.Options{
		IssuerURL:    issuerURL,
		ClientID:     clientID,
		ClientSecret: clientSecret,
		JWTSecret:    []byte(oidcTestJWTSecret),
	})

	hash, _ := bcrypt.GenerateFromPassword([]byte("correct-password"), bcrypt.MinCost)
	adminRepo := testutil.NewMockAdminRepo(string(hash))
	if setupComplete {
		adminRepo.Config["setup_complete"] = "true"
	}

	externalRepo := testutil.NewMockExternalIdentityRepo()

	h := &Handlers{
		jwtSecret:            []byte(oidcTestJWTSecret),
		oidc:                 svc,
		externalIdentityRepo: externalRepo,
		setupComplete:        &complete,
		adminRepo:            adminRepo,
	}

	return &oidcTestSetup{h: h, externalRepo: externalRepo, adminRepo: adminRepo, setupComplete: &complete}
}

func newOIDCRequest(method, target string) *http.Request {
	req := httptest.NewRequest(method, "https://"+oidcTestHost+target, nil)
	req.Host = oidcTestHost
	req.TLS = &tls.ConnectionState{}
	return req
}

func cookieByName(t *testing.T, rec *httptest.ResponseRecorder, name string) *http.Cookie {
	t.Helper()
	for _, c := range rec.Result().Cookies() {
		if c.Name == name {
			return c
		}
	}
	return nil
}

func captureLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	original := slog.Default()
	slog.SetDefault(logger)
	t.Cleanup(func() { slog.SetDefault(original) })
	return &buf
}

// ---------------------------------------------------------------------
// AC-001 / AC-002 / AC-003 — dormancy
// ---------------------------------------------------------------------

// [AC-001] Dormant by default: no button — availability reports
// {"available": false, "label": ""} when no OIDC vars are configured.
func TestAC001_OIDCAvailability_DormantByDefault(t *testing.T) {
	setup := newOIDCTestSetup(t, nil, "", "", true)

	req := newOIDCRequest("GET", "/api/v1/auth/oidc")
	rec := httptest.NewRecorder()
	setup.h.OIDCAvailability(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var resp map[string]interface{}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.Equal(t, false, resp["available"])
	assert.Equal(t, "", resp["label"])
}

// [AC-002] Dormant by default: endpoints 404 — /start and /callback both
// respond 404 with no cookie set.
func TestAC002_OIDCStartAndCallback_404WhenDormant(t *testing.T) {
	setup := newOIDCTestSetup(t, nil, "", "", true)

	startReq := newOIDCRequest("GET", "/api/v1/auth/oidc/start")
	startRec := httptest.NewRecorder()
	setup.h.OIDCStart(startRec, startReq)
	assert.Equal(t, http.StatusNotFound, startRec.Code)
	assert.Empty(t, startRec.Result().Cookies())

	cbReq := newOIDCRequest("GET", "/api/v1/auth/oidc/callback")
	cbRec := httptest.NewRecorder()
	setup.h.OIDCCallback(cbRec, cbReq)
	assert.Equal(t, http.StatusNotFound, cbRec.Code)
	assert.Empty(t, cbRec.Result().Cookies())
}

// [AC-003] Partial configuration stays dormant: issuer + client ID set but
// secret empty leaves SSO dormant and /start 404.
func TestAC003_PartialConfig_DormantAndStartNotFound(t *testing.T) {
	idp := newStubIDP(t)
	setup := newOIDCTestSetup(t, idp, oidcTestClientID, "", true) // secret empty

	req := newOIDCRequest("GET", "/api/v1/auth/oidc")
	rec := httptest.NewRecorder()
	setup.h.OIDCAvailability(rec, req)
	var resp map[string]interface{}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.Equal(t, false, resp["available"])
	assert.Equal(t, "", resp["label"])

	startReq := newOIDCRequest("GET", "/api/v1/auth/oidc/start")
	startRec := httptest.NewRecorder()
	setup.h.OIDCStart(startRec, startReq)
	assert.Equal(t, http.StatusNotFound, startRec.Code)
}

// ---------------------------------------------------------------------
// AC-004 / AC-005 — configured availability + label
// ---------------------------------------------------------------------

// [AC-004] Configured: availability and default label.
func TestAC004_OIDCAvailability_ConfiguredDefaultLabel(t *testing.T) {
	idp := newStubIDP(t)
	setup := newOIDCTestSetup(t, idp, oidcTestClientID, oidcTestClientSecret, true)

	req := newOIDCRequest("GET", "/api/v1/auth/oidc")
	rec := httptest.NewRecorder()
	setup.h.OIDCAvailability(rec, req)

	var resp map[string]interface{}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.Equal(t, true, resp["available"])
	assert.Equal(t, "Sign in with SSO", resp["label"])
}

// [AC-005] Custom button label is echoed by the availability endpoint.
func TestAC005_OIDCAvailability_CustomLabel(t *testing.T) {
	idp := newStubIDP(t)
	svc := oidc.New(oidc.Options{
		IssuerURL: idp.issuer(), ClientID: oidcTestClientID, ClientSecret: oidcTestClientSecret,
		ButtonLabel: "Sign in with authentik", JWTSecret: []byte(oidcTestJWTSecret),
	})
	var complete atomic.Bool
	complete.Store(true)
	h := &Handlers{jwtSecret: []byte(oidcTestJWTSecret), oidc: svc, setupComplete: &complete}

	req := newOIDCRequest("GET", "/api/v1/auth/oidc")
	rec := httptest.NewRecorder()
	h.OIDCAvailability(rec, req)

	var resp map[string]interface{}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.Equal(t, "Sign in with authentik", resp["label"])
}

// ---------------------------------------------------------------------
// AC-006 — /start shape
// ---------------------------------------------------------------------

// [AC-006] Start redirects to the IdP with PKCE, state, and nonce; the
// redirect_uri is exact and the transaction cookie is HttpOnly with
// MaxAge <= 300.
func TestAC006_OIDCStart_RedirectsWithPKCEStateNonce(t *testing.T) {
	idp := newStubIDP(t)
	setup := newOIDCTestSetup(t, idp, oidcTestClientID, oidcTestClientSecret, true)

	req := newOIDCRequest("GET", "/api/v1/auth/oidc/start")
	rec := httptest.NewRecorder()
	setup.h.OIDCStart(rec, req)

	require.Equal(t, http.StatusFound, rec.Code)
	loc, err := url.Parse(rec.Header().Get("Location"))
	require.NoError(t, err)
	q := loc.Query()
	assert.Equal(t, "code", q.Get("response_type"))
	assert.Equal(t, "S256", q.Get("code_challenge_method"))
	assert.NotEmpty(t, q.Get("code_challenge"))
	assert.NotEmpty(t, q.Get("state"))
	assert.NotEmpty(t, q.Get("nonce"))
	assert.Contains(t, q.Get("scope"), "openid")
	assert.Equal(t, oidcTestRedirectURI, q.Get("redirect_uri"), "redirect_uri must be exact")

	txCookie := cookieByName(t, rec, oidc.TransactionCookieName)
	require.NotNil(t, txCookie)
	assert.True(t, txCookie.HttpOnly)
	assert.True(t, txCookie.Secure, "Secure must be set over https")
	assert.LessOrEqual(t, txCookie.MaxAge, 300)
	assert.Equal(t, oidc.TransactionCookiePath, txCookie.Path)
}

// ---------------------------------------------------------------------
// Full round trip helper
// ---------------------------------------------------------------------

// beginAndCompleteLogin drives /start then /callback against the stub IdP
// end to end, returning the callback recorder for assertion.
func beginAndCompleteLogin(t *testing.T, setup *oidcTestSetup, idp *stubIDP, claims stubIDTokenClaims, returnURLParam string) *httptest.ResponseRecorder {
	t.Helper()

	startTarget := "/api/v1/auth/oidc/start"
	if returnURLParam != "" {
		startTarget += "?returnUrl=" + url.QueryEscape(returnURLParam)
	}
	startReq := newOIDCRequest("GET", startTarget)
	startRec := httptest.NewRecorder()
	setup.h.OIDCStart(startRec, startReq)
	require.Equal(t, http.StatusFound, startRec.Code)

	loc, err := url.Parse(startRec.Header().Get("Location"))
	require.NoError(t, err)
	state := loc.Query().Get("state")
	nonce := loc.Query().Get("nonce")

	txCookie := cookieByName(t, startRec, oidc.TransactionCookieName)
	require.NotNil(t, txCookie)

	claims.Nonce = nonce
	code := idp.issueCode(oidcTestClientID, claims)

	cbReq := newOIDCRequest("GET", "/api/v1/auth/oidc/callback?code="+code+"&state="+state)
	cbReq.AddCookie(txCookie)
	cbRec := httptest.NewRecorder()
	setup.h.OIDCCallback(cbRec, cbReq)
	return cbRec
}

// ---------------------------------------------------------------------
// AC-008 / AC-009 — successful login mints the standard session cookie
// ---------------------------------------------------------------------

// [AC-008] Successful login mints the standard session cookie: name, claims,
// signing key, lifetime, and cookie attributes identical to password login;
// a subsequent AuthCheck reports authenticated.
func TestAC008_SuccessfulLogin_MintsStandardSessionCookie(t *testing.T) {
	idp := newStubIDP(t)
	setup := newOIDCTestSetup(t, idp, oidcTestClientID, oidcTestClientSecret, true)

	email := "person@example.com"
	rec := beginAndCompleteLogin(t, setup, idp, stubIDTokenClaims{
		Subject: "user-abc", HasEmail: true, Email: email, HasVerified: true, EmailVerified: true,
	}, "")

	require.Equal(t, http.StatusFound, rec.Code)
	assert.Equal(t, "/", rec.Header().Get("Location"))

	sessionCookie := cookieByName(t, rec, "riot_session")
	require.NotNil(t, sessionCookie)
	assert.True(t, sessionCookie.HttpOnly)
	assert.Equal(t, http.SameSiteLaxMode, sessionCookie.SameSite, "AC-009/SEC-001")
	assert.True(t, sessionCookie.Secure)
	assert.Equal(t, "/", sessionCookie.Path)
	assert.Equal(t, 86400, sessionCookie.MaxAge)

	token, err := jwt.Parse(sessionCookie.Value, func(tok *jwt.Token) (interface{}, error) {
		return []byte(oidcTestJWTSecret), nil
	})
	require.NoError(t, err)
	claims := token.Claims.(jwt.MapClaims)
	assert.Equal(t, "admin", claims["sub"])

	checkReq := newOIDCRequest("GET", "/api/v1/auth/check")
	checkReq.AddCookie(sessionCookie)
	checkRec := httptest.NewRecorder()
	setup.h.AuthCheck(checkRec, checkReq)
	var checkResp map[string]interface{}
	require.NoError(t, json.NewDecoder(checkRec.Body).Decode(&checkResp))
	assert.Equal(t, true, checkResp["authenticated"])
	assert.Equal(t, false, checkResp["needs_setup"])
}

// ---------------------------------------------------------------------
// AC-010 / AC-011 — identity audit
// ---------------------------------------------------------------------

// [AC-010] Identity audit row written on first login.
func TestAC010_FirstLogin_WritesAuditRow(t *testing.T) {
	idp := newStubIDP(t)
	setup := newOIDCTestSetup(t, idp, oidcTestClientID, oidcTestClientSecret, true)

	t0 := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	setup.h.oidcClock = func() time.Time { return t0 }

	email := "person@example.com"
	rec := beginAndCompleteLogin(t, setup, idp, stubIDTokenClaims{
		Subject: "user-first", HasEmail: true, Email: email,
	}, "")
	require.Equal(t, http.StatusFound, rec.Code)

	assert.Equal(t, 1, setup.externalRepo.Count())
	record := setup.externalRepo.Get(idp.issuer(), "user-first")
	require.NotNil(t, record)
	require.NotNil(t, record.Email)
	assert.Equal(t, email, *record.Email)
	assert.True(t, record.FirstLoginAt.Equal(t0))
	assert.True(t, record.LastLoginAt.Equal(t0))
}

// [AC-011] Repeat login updates rather than duplicates: first_login_at is
// preserved, last_login_at is updated.
func TestAC011_RepeatLogin_UpdatesNotDuplicates(t *testing.T) {
	idp := newStubIDP(t)
	setup := newOIDCTestSetup(t, idp, oidcTestClientID, oidcTestClientSecret, true)

	t0 := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	t1 := t0.Add(24 * time.Hour)

	setup.h.oidcClock = func() time.Time { return t0 }
	rec1 := beginAndCompleteLogin(t, setup, idp, stubIDTokenClaims{Subject: "user-repeat"}, "")
	require.Equal(t, http.StatusFound, rec1.Code)

	setup.h.oidcClock = func() time.Time { return t1 }
	rec2 := beginAndCompleteLogin(t, setup, idp, stubIDTokenClaims{Subject: "user-repeat"}, "")
	require.Equal(t, http.StatusFound, rec2.Code)

	assert.Equal(t, 1, setup.externalRepo.Count(), "still exactly one record")
	record := setup.externalRepo.Get(idp.issuer(), "user-repeat")
	require.NotNil(t, record)
	assert.True(t, record.FirstLoginAt.Equal(t0), "first_login_at must stay T0")
	assert.True(t, record.LastLoginAt.Equal(t1), "last_login_at must become T1")
}

// ---------------------------------------------------------------------
// AC-012 — any IdP-authorized identity becomes admin
// ---------------------------------------------------------------------

// [AC-012] Any IdP-authorized identity becomes admin: a second, previously
// unseen identity's session passes AdminAuth with no allowlist involved.
func TestAC012_UnseenIdentity_BecomesAdmin(t *testing.T) {
	idp := newStubIDP(t)
	setup := newOIDCTestSetup(t, idp, oidcTestClientID, oidcTestClientSecret, true)

	rec := beginAndCompleteLogin(t, setup, idp, stubIDTokenClaims{Subject: "never-seen-before"}, "")
	require.Equal(t, http.StatusFound, rec.Code)
	sessionCookie := cookieByName(t, rec, "riot_session")
	require.NotNil(t, sessionCookie)

	var reached bool
	stubAdminOnly := middleware.AdminAuth([]byte(oidcTestJWTSecret))(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached = true
		w.WriteHeader(http.StatusOK)
	}))

	req := newOIDCRequest("GET", "/api/v1/devices")
	req.AddCookie(sessionCookie)
	adminRec := httptest.NewRecorder()
	stubAdminOnly.ServeHTTP(adminRec, req)

	assert.Equal(t, http.StatusOK, adminRec.Code)
	assert.True(t, reached, "an unseen identity's session must reach an admin-only handler with no allowlist")
}

// ---------------------------------------------------------------------
// AC-013 / AC-014 — state / nonce mismatch
// ---------------------------------------------------------------------

// [AC-013] State mismatch is rejected: no session cookie, 302 to
// /?sso_error=sso_failed, and the body is not JSON.
func TestAC013_StateMismatch_Rejected(t *testing.T) {
	idp := newStubIDP(t)
	setup := newOIDCTestSetup(t, idp, oidcTestClientID, oidcTestClientSecret, true)

	startReq := newOIDCRequest("GET", "/api/v1/auth/oidc/start")
	startRec := httptest.NewRecorder()
	setup.h.OIDCStart(startRec, startReq)
	txCookie := cookieByName(t, startRec, oidc.TransactionCookieName)
	require.NotNil(t, txCookie)

	code := idp.issueCode(oidcTestClientID, stubIDTokenClaims{Subject: "user-x"})
	cbReq := newOIDCRequest("GET", "/api/v1/auth/oidc/callback?code="+code+"&state=WRONG-STATE")
	cbReq.AddCookie(txCookie)
	cbRec := httptest.NewRecorder()
	setup.h.OIDCCallback(cbRec, cbReq)

	assert.Equal(t, http.StatusFound, cbRec.Code)
	assert.Equal(t, "/?sso_error=sso_failed", cbRec.Header().Get("Location"))
	assert.Nil(t, cookieByName(t, cbRec, "riot_session"))
	assert.NotContains(t, cbRec.Header().Get("Content-Type"), "json")
}

// [AC-014] Nonce mismatch is rejected at the handler level: the ID token's
// nonce claim does not match the transaction's nonce.
func TestAC014_NonceMismatch_Rejected(t *testing.T) {
	idp := newStubIDP(t)
	setup := newOIDCTestSetup(t, idp, oidcTestClientID, oidcTestClientSecret, true)

	startReq := newOIDCRequest("GET", "/api/v1/auth/oidc/start")
	startRec := httptest.NewRecorder()
	setup.h.OIDCStart(startRec, startReq)
	loc, _ := url.Parse(startRec.Header().Get("Location"))
	state := loc.Query().Get("state")
	txCookie := cookieByName(t, startRec, oidc.TransactionCookieName)

	// ID token minted with a DIFFERENT nonce than the transaction holds.
	code := idp.issueCode(oidcTestClientID, stubIDTokenClaims{Subject: "user-x", Nonce: "some-other-nonce"})

	cbReq := newOIDCRequest("GET", "/api/v1/auth/oidc/callback?code="+code+"&state="+state)
	cbReq.AddCookie(txCookie)
	cbRec := httptest.NewRecorder()
	setup.h.OIDCCallback(cbRec, cbReq)

	assert.Equal(t, http.StatusFound, cbRec.Code)
	assert.Equal(t, "/?sso_error=sso_failed", cbRec.Header().Get("Location"))
	assert.Nil(t, cookieByName(t, cbRec, "riot_session"))
}

// ---------------------------------------------------------------------
// AC-015 / AC-016 — transaction cookie handling
// ---------------------------------------------------------------------

// [AC-015] Missing or expired transaction is rejected: no transaction
// cookie present, callback with an otherwise well-formed code/state yields
// sso_expired and clears the transaction cookie.
func TestAC015_MissingTransaction_Rejected(t *testing.T) {
	idp := newStubIDP(t)
	setup := newOIDCTestSetup(t, idp, oidcTestClientID, oidcTestClientSecret, true)

	code := idp.issueCode(oidcTestClientID, stubIDTokenClaims{Subject: "user-x"})
	cbReq := newOIDCRequest("GET", "/api/v1/auth/oidc/callback?code="+code+"&state=some-state")
	cbRec := httptest.NewRecorder()
	setup.h.OIDCCallback(cbRec, cbReq)

	assert.Equal(t, http.StatusFound, cbRec.Code)
	assert.Equal(t, "/?sso_error=sso_expired", cbRec.Header().Get("Location"))
	assert.Nil(t, cookieByName(t, cbRec, "riot_session"))

	txCookie := cookieByName(t, cbRec, oidc.TransactionCookieName)
	require.NotNil(t, txCookie, "the transaction cookie must be cleared on the response even when it was never set")
	assert.Equal(t, -1, txCookie.MaxAge)
}

// [AC-016] Transaction cookie is cleared on success too; replaying the same
// callback URL a second time (without the now-cleared cookie) does not
// produce a session.
func TestAC016_TransactionCookieCleared_ReplayFails(t *testing.T) {
	idp := newStubIDP(t)
	setup := newOIDCTestSetup(t, idp, oidcTestClientID, oidcTestClientSecret, true)

	startReq := newOIDCRequest("GET", "/api/v1/auth/oidc/start")
	startRec := httptest.NewRecorder()
	setup.h.OIDCStart(startRec, startReq)
	loc, _ := url.Parse(startRec.Header().Get("Location"))
	state := loc.Query().Get("state")
	nonce := loc.Query().Get("nonce")
	txCookie := cookieByName(t, startRec, oidc.TransactionCookieName)

	code := idp.issueCode(oidcTestClientID, stubIDTokenClaims{Subject: "user-x", Nonce: nonce})
	target := "/api/v1/auth/oidc/callback?code=" + code + "&state=" + state

	firstReq := newOIDCRequest("GET", target)
	firstReq.AddCookie(txCookie)
	firstRec := httptest.NewRecorder()
	setup.h.OIDCCallback(firstRec, firstReq)
	require.Equal(t, http.StatusFound, firstRec.Code)
	require.Equal(t, "/", firstRec.Header().Get("Location"))

	clearedTx := cookieByName(t, firstRec, oidc.TransactionCookieName)
	require.NotNil(t, clearedTx)
	assert.Equal(t, -1, clearedTx.MaxAge, "the transaction cookie must be cleared on a successful response too")

	// Replay: the browser no longer holds the (cleared) transaction cookie.
	replayReq := newOIDCRequest("GET", target)
	replayRec := httptest.NewRecorder()
	setup.h.OIDCCallback(replayRec, replayReq)
	assert.Equal(t, "/?sso_error=sso_expired", replayRec.Header().Get("Location"))
	assert.Nil(t, cookieByName(t, replayRec, "riot_session"), "a replayed callback must not mint a session")
}

// ---------------------------------------------------------------------
// AC-017 / AC-018 / AC-019 — IdP unreachable / IdP error / rest of server unaffected
// ---------------------------------------------------------------------

// [AC-017] IdP down: start degrades to the login screen within the
// configured timeout, without a 5xx status or a JSON body.
func TestAC017_StartDegradesWhenIdPDown(t *testing.T) {
	deadIDP := newClosedStubIDP(t)
	setup := newOIDCTestSetup(t, deadIDP, oidcTestClientID, oidcTestClientSecret, true)

	req := newOIDCRequest("GET", "/api/v1/auth/oidc/start")
	rec := httptest.NewRecorder()
	setup.h.OIDCStart(rec, req)

	assert.Equal(t, http.StatusFound, rec.Code)
	assert.Equal(t, "/?sso_error=sso_unavailable", rec.Header().Get("Location"))
	assert.Less(t, rec.Code, 500)
	assert.NotContains(t, rec.Header().Get("Content-Type"), "json")
}

// [AC-018] IdP error response (access_denied) lands back on the login
// screen with sso_denied and clears the transaction; no session is set.
func TestAC018_IdPAccessDenied_LandsOnLoginScreen(t *testing.T) {
	idp := newStubIDP(t)
	setup := newOIDCTestSetup(t, idp, oidcTestClientID, oidcTestClientSecret, true)

	startReq := newOIDCRequest("GET", "/api/v1/auth/oidc/start")
	startRec := httptest.NewRecorder()
	setup.h.OIDCStart(startRec, startReq)
	txCookie := cookieByName(t, startRec, oidc.TransactionCookieName)

	cbReq := newOIDCRequest("GET", "/api/v1/auth/oidc/callback?error=access_denied")
	cbReq.AddCookie(txCookie)
	cbRec := httptest.NewRecorder()
	setup.h.OIDCCallback(cbRec, cbReq)

	assert.Equal(t, http.StatusFound, cbRec.Code)
	assert.Equal(t, "/?sso_error=sso_denied", cbRec.Header().Get("Location"))
	assert.Nil(t, cookieByName(t, cbRec, "riot_session"))

	// Password login continues to work afterwards (D-9 fallback).
	body, _ := json.Marshal(map[string]string{"password": "correct-password"})
	loginReq := httptest.NewRequest("POST", "https://"+oidcTestHost+"/api/v1/auth/login", bytes.NewReader(body))
	loginRec := httptest.NewRecorder()
	setup.h.Login(loginRec, loginReq)
	assert.Equal(t, http.StatusOK, loginRec.Code)
}

// [AC-019] IdP down does not affect the rest of the server: password login
// still succeeds while SSO is configured against an unreachable issuer.
func TestAC019_IdPDown_PasswordLoginUnaffected(t *testing.T) {
	deadIDP := newClosedStubIDP(t)
	setup := newOIDCTestSetup(t, deadIDP, oidcTestClientID, oidcTestClientSecret, true)

	body, _ := json.Marshal(map[string]string{"password": "correct-password"})
	req := httptest.NewRequest("POST", "https://"+oidcTestHost+"/api/v1/auth/login", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	setup.h.Login(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
}

// ---------------------------------------------------------------------
// AC-020 / AC-021 — setup interaction
// ---------------------------------------------------------------------

// [AC-020] Fresh install runs the setup wizard first: with setup incomplete,
// availability reports false and both routes 404 regardless of configuration.
func TestAC020_SetupIncomplete_OIDCSuppressed(t *testing.T) {
	idp := newStubIDP(t)
	setup := newOIDCTestSetup(t, idp, oidcTestClientID, oidcTestClientSecret, false) // setup incomplete

	req := newOIDCRequest("GET", "/api/v1/auth/oidc")
	rec := httptest.NewRecorder()
	setup.h.OIDCAvailability(rec, req)
	var resp map[string]interface{}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.Equal(t, false, resp["available"])

	startRec := httptest.NewRecorder()
	setup.h.OIDCStart(startRec, newOIDCRequest("GET", "/api/v1/auth/oidc/start"))
	assert.Equal(t, http.StatusNotFound, startRec.Code)

	cbRec := httptest.NewRecorder()
	setup.h.OIDCCallback(cbRec, newOIDCRequest("GET", "/api/v1/auth/oidc/callback"))
	assert.Equal(t, http.StatusNotFound, cbRec.Code)
}

// [AC-021] SSO does not complete setup or set a password: after a
// successful SSO login, the stored password hash and setup-complete flag
// are unchanged, and the original password still logs in.
func TestAC021_SSOLogin_DoesNotTouchPasswordOrSetupFlag(t *testing.T) {
	idp := newStubIDP(t)
	setup := newOIDCTestSetup(t, idp, oidcTestClientID, oidcTestClientSecret, true)

	originalHash := setup.adminRepo.PasswordHash
	originalSetupFlag := setup.adminRepo.Config["setup_complete"]

	rec := beginAndCompleteLogin(t, setup, idp, stubIDTokenClaims{Subject: "user-x"}, "")
	require.Equal(t, http.StatusFound, rec.Code)

	assert.Equal(t, originalHash, setup.adminRepo.PasswordHash)
	assert.Equal(t, originalSetupFlag, setup.adminRepo.Config["setup_complete"])

	body, _ := json.Marshal(map[string]string{"password": "correct-password"})
	loginReq := httptest.NewRequest("POST", "https://"+oidcTestHost+"/api/v1/auth/login", bytes.NewReader(body))
	loginRec := httptest.NewRecorder()
	setup.h.Login(loginRec, loginReq)
	assert.Equal(t, http.StatusOK, loginRec.Code)
}

// ---------------------------------------------------------------------
// AC-023 — no IdP tokens persisted
// ---------------------------------------------------------------------

// [AC-023] No IdP tokens are persisted: the only cookies on the successful
// response are riot_session and the riot_oidc_tx clear; nothing carries the
// stub's access token value.
func TestAC023_NoIdPTokensPersisted(t *testing.T) {
	idp := newStubIDP(t)
	setup := newOIDCTestSetup(t, idp, oidcTestClientID, oidcTestClientSecret, true)

	rec := beginAndCompleteLogin(t, setup, idp, stubIDTokenClaims{Subject: "user-x"}, "")
	require.Equal(t, http.StatusFound, rec.Code)

	cookies := rec.Result().Cookies()
	names := make(map[string]bool)
	for _, c := range cookies {
		names[c.Name] = true
		assert.NotContains(t, c.Value, "test-access-token", "no cookie may contain the IdP access token")
	}
	assert.True(t, names["riot_session"])
	assert.True(t, names[oidc.TransactionCookieName])
	assert.Len(t, cookies, 2, "only riot_session and the riot_oidc_tx clear may be set")

	record := setup.externalRepo.Get(idp.issuer(), "user-x")
	require.NotNil(t, record)
	// models.ExternalIdentity (via MockExternalIdentityRecord) has no
	// token-bearing field at all — enforced at compile time.
}

// ---------------------------------------------------------------------
// AC-024 — client secret never leaks
// ---------------------------------------------------------------------

// [AC-024] Client secret never leaks: it appears in no response and no log
// line across a success and a failure branch.
func TestAC024_ClientSecretNeverLeaks(t *testing.T) {
	const sentinelSecret = "sentinel-super-secret-value-XYZ123"
	idp := newStubIDP(t)
	setup := newOIDCTestSetup(t, idp, oidcTestClientID, sentinelSecret, true)

	logBuf := captureLogs(t)

	availRec := httptest.NewRecorder()
	setup.h.OIDCAvailability(availRec, newOIDCRequest("GET", "/api/v1/auth/oidc"))
	assert.NotContains(t, availRec.Body.String(), sentinelSecret)

	successRec := beginAndCompleteLogin(t, setup, idp, stubIDTokenClaims{Subject: "user-secret-1"}, "")
	assert.NotContains(t, successRec.Body.String(), sentinelSecret)
	for _, c := range successRec.Result().Cookies() {
		assert.NotContains(t, c.Value, sentinelSecret)
	}

	startReq := newOIDCRequest("GET", "/api/v1/auth/oidc/start")
	startRec := httptest.NewRecorder()
	setup.h.OIDCStart(startRec, startReq)
	txCookie := cookieByName(t, startRec, oidc.TransactionCookieName)
	failReq := newOIDCRequest("GET", "/api/v1/auth/oidc/callback?state=wrong")
	failReq.AddCookie(txCookie)
	failRec := httptest.NewRecorder()
	setup.h.OIDCCallback(failRec, failReq)
	assert.NotContains(t, failRec.Body.String(), sentinelSecret)

	assert.NotContains(t, logBuf.String(), sentinelSecret, "the client secret must never appear in a log line")
}

// ---------------------------------------------------------------------
// AC-026 — open redirect refused
// ---------------------------------------------------------------------

// [AC-026] Open redirect is refused: hostile returnUrl values are replaced
// with "/", and a successful callback redirects there, never to the hostile
// target.
func TestAC026_OpenRedirectRefused(t *testing.T) {
	idp := newStubIDP(t)

	for _, hostile := range []string{"https://evil.example", "//evil.example"} {
		t.Run(hostile, func(t *testing.T) {
			setup := newOIDCTestSetup(t, idp, oidcTestClientID, oidcTestClientSecret, true)
			rec := beginAndCompleteLogin(t, setup, idp, stubIDTokenClaims{Subject: "user-redirect"}, hostile)
			require.Equal(t, http.StatusFound, rec.Code)
			loc := rec.Header().Get("Location")
			assert.Equal(t, "/", loc)
			assert.False(t, strings.Contains(loc, "evil.example"))
		})
	}
}

// ---------------------------------------------------------------------
// AC-028 / SEC-003 — audit logging
// ---------------------------------------------------------------------

// [AC-028] Every attempt is auditable in the log: one structured entry per
// attempt, success carries issuer/subject, failures carry the reason, and
// no entry contains a token, secret, or verifier.
func TestAC028_EveryAttemptIsAuditable(t *testing.T) {
	idp := newStubIDP(t)
	setup := newOIDCTestSetup(t, idp, oidcTestClientID, oidcTestClientSecret, true)
	logBuf := captureLogs(t)

	// One successful login.
	successRec := beginAndCompleteLogin(t, setup, idp, stubIDTokenClaims{Subject: "user-audit"}, "")
	require.Equal(t, http.StatusFound, successRec.Code)

	// One state-mismatch rejection.
	startRec := httptest.NewRecorder()
	setup.h.OIDCStart(startRec, newOIDCRequest("GET", "/api/v1/auth/oidc/start"))
	txCookie := cookieByName(t, startRec, oidc.TransactionCookieName)
	mismatchReq := newOIDCRequest("GET", "/api/v1/auth/oidc/callback?code=whatever&state=wrong")
	mismatchReq.AddCookie(txCookie)
	mismatchRec := httptest.NewRecorder()
	setup.h.OIDCCallback(mismatchRec, mismatchReq)
	assert.Equal(t, "/?sso_error=sso_failed", mismatchRec.Header().Get("Location"))

	// One IdP-unreachable attempt.
	deadIDP := newClosedStubIDP(t)
	deadSetup := newOIDCTestSetup(t, deadIDP, oidcTestClientID, oidcTestClientSecret, true)
	deadRec := httptest.NewRecorder()
	deadSetup.h.OIDCStart(deadRec, newOIDCRequest("GET", "/api/v1/auth/oidc/start"))
	assert.Equal(t, "/?sso_error=sso_unavailable", deadRec.Header().Get("Location"))

	logs := logBuf.String()
	lines := strings.Split(strings.TrimSpace(logs), "\n")

	var successEntry, mismatchEntry map[string]interface{}
	for _, line := range lines {
		var entry map[string]interface{}
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			continue
		}
		switch entry["msg"] {
		case "sso login":
			successEntry = entry
		case "sso login failed":
			if entry["reason"] == "state_mismatch" {
				mismatchEntry = entry
			}
		}
	}

	require.NotNil(t, successEntry, "expected one success entry")
	assert.Equal(t, "success", successEntry["outcome"])
	assert.Equal(t, idp.issuer(), successEntry["issuer"])
	assert.Equal(t, "user-audit", successEntry["subject"])
	assert.NotEmpty(t, successEntry["ip"])

	require.NotNil(t, mismatchEntry, "expected one state-mismatch failure entry")
	assert.Equal(t, "failure", mismatchEntry["outcome"])
	assert.Equal(t, "sso_failed", mismatchEntry["code"])

	assert.NotContains(t, logs, oidcTestClientSecret)
	assert.NotContains(t, logs, "test-access-token")
	assert.NotContains(t, logs, "person@example.com")
}

// [SEC-003] A first-ever (issuer, subject) login emits a WARN entry naming
// issuer and subject; a repeat login does not.
func TestSEC003_FirstAdmission_EmitsWarnRepeatDoesNot(t *testing.T) {
	idp := newStubIDP(t)
	setup := newOIDCTestSetup(t, idp, oidcTestClientID, oidcTestClientSecret, true)
	logBuf := captureLogs(t)

	rec1 := beginAndCompleteLogin(t, setup, idp, stubIDTokenClaims{Subject: "user-sec003"}, "")
	require.Equal(t, http.StatusFound, rec1.Code)

	firstLogs := logBuf.String()
	assert.Contains(t, firstLogs, "new SSO identity granted admin")
	assert.Contains(t, firstLogs, "\"level\":\"WARN\"")

	logBuf.Reset()
	rec2 := beginAndCompleteLogin(t, setup, idp, stubIDTokenClaims{Subject: "user-sec003"}, "")
	require.Equal(t, http.StatusFound, rec2.Code)

	repeatLogs := logBuf.String()
	assert.NotContains(t, repeatLogs, "new SSO identity granted admin", "a repeat login must not re-emit the admission warning")
}
