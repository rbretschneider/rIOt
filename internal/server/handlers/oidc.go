package handlers

import (
	"context"
	"crypto/subtle"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/DesyncTheThird/rIOt/internal/models"
	"github.com/DesyncTheThird/rIOt/internal/server/middleware"
	"github.com/DesyncTheThird/rIOt/internal/server/oidc"
)

// constantTimeStringsEqual compares two strings in constant time (§12 note
// 7 — used for the callback's state comparison, not ==).
func constantTimeStringsEqual(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

// loginLandingPath is the dashboard root — the only URL the browser is ever
// redirected to on an SSO failure, carrying a machine-readable ?sso_error=
// code (OIDC-001 AD-014, A-6). No /login route exists in this app.
const loginLandingPath = "/"

// requestOrigin returns scheme://host for the incoming request, using the
// same trust-gated scheme resolution as the riot_session and riot_oidc_tx
// Secure flags (middleware.RequestScheme, AD-020). X-Forwarded-Host is never
// consulted (AD-009) — r.Host is used verbatim, including any port.
func requestOrigin(r *http.Request) string {
	return middleware.RequestScheme(r) + "://" + r.Host
}

// oidcCallbackRedirectURI returns the exact redirect_uri sent to the IdP on
// /start and presented again at /callback (FR-015) — one function, both call
// sites, so the two can never drift.
func oidcCallbackRedirectURI(r *http.Request) string {
	return requestOrigin(r) + "/api/v1/auth/oidc/callback"
}

// ssoErrorRedirect sends a 302 to the login landing URL carrying the given
// §7.4 error code. This, the redirect to the IdP, and 404 (dormant/setup
// incomplete) are the only three response shapes /start and /callback ever
// produce — never JSON, an HTML error page, a stack trace, or a raw IdP
// error (FR-024, AD-010).
func ssoErrorRedirect(w http.ResponseWriter, r *http.Request, code string) {
	http.Redirect(w, r, loginLandingPath+"?sso_error="+code, http.StatusFound)
}

// OIDCAvailability handles GET /api/v1/auth/oidc. Answers from local
// configuration only — no DB read, no network call (FR-011, NFR-001) — and
// is reachable without a riot_session cookie (FR-009).
func (h *Handlers) OIDCAvailability(w http.ResponseWriter, r *http.Request) {
	available := h.oidc.Enabled() && h.isSetupComplete()
	label := ""
	if available {
		label = h.oidc.ButtonLabel()
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"available": available,
		"label":     label,
	})
}

// OIDCStart handles GET /api/v1/auth/oidc/start.
func (h *Handlers) OIDCStart(w http.ResponseWriter, r *http.Request) {
	if !h.oidc.Enabled() || !h.isSetupComplete() {
		http.Error(w, `{"error":"not found"}`, http.StatusNotFound)
		return
	}

	ip := middleware.ClientIP(r)
	returnPath := oidc.SafeReturnPath(r.URL.Query().Get("returnUrl"))
	redirectURI := oidcCallbackRedirectURI(r)

	authURL, tx, err := h.oidc.BeginLogin(r.Context(), redirectURI, returnPath, time.Now())
	if err != nil {
		code, reason := oidcErrorCodeAndReason(err)
		slog.Warn("sso login failed", "outcome", "failure", "reason", reason, "code", code, "ip", ip)
		ssoErrorRedirect(w, r, code)
		return
	}

	txKey := h.oidc.TransactionKey()
	encoded, err := tx.Encode(txKey)
	if err != nil {
		slog.Warn("sso login failed", "outcome", "failure", "reason", "encode_failed", "code", oidc.CodeFailed, "ip", ip)
		ssoErrorRedirect(w, r, oidc.CodeFailed)
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     oidc.TransactionCookieName,
		Value:    encoded,
		Path:     oidc.TransactionCookiePath,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   middleware.RequestScheme(r) == "https",
		MaxAge:   oidc.TransactionMaxAgeSeconds,
	})

	http.Redirect(w, r, authURL, http.StatusFound)
}

// clearTransactionCookie clears riot_oidc_tx with the same name/path/
// attributes it was set with, on every callback outcome — success, failure,
// and rejection (FR-022, AC-016).
func clearTransactionCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     oidc.TransactionCookieName,
		Value:    "",
		Path:     oidc.TransactionCookiePath,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   middleware.RequestScheme(r) == "https",
		MaxAge:   -1,
	})
}

// OIDCCallback handles GET /api/v1/auth/oidc/callback.
func (h *Handlers) OIDCCallback(w http.ResponseWriter, r *http.Request) {
	if !h.oidc.Enabled() || !h.isSetupComplete() {
		http.Error(w, `{"error":"not found"}`, http.StatusNotFound)
		return
	}

	ip := middleware.ClientIP(r)

	// Clear the transaction cookie before any branch, so every outcome
	// clears it unconditionally (FR-022, AC-016, §12 note 4).
	clearTransactionCookie(w, r)

	fail := func(code, reason string) {
		slog.Warn("sso login failed", "outcome", "failure", "reason", reason, "code", code, "ip", ip)
		ssoErrorRedirect(w, r, code)
	}

	var rawTx string
	if c, err := r.Cookie(oidc.TransactionCookieName); err == nil {
		rawTx = c.Value
	}

	txKey := h.oidc.TransactionKey()
	tx, err := oidc.DecodeTransaction(rawTx, txKey, time.Now())
	if err != nil {
		switch {
		case errors.Is(err, oidc.ErrTransactionExpired):
			fail(oidc.CodeExpired, oidc.ReasonTransactionExpired)
		default:
			// ErrTransactionMAC and ErrTransactionMalformed both map to
			// sso_expired/no_transaction (AD-005).
			fail(oidc.CodeExpired, oidc.ReasonNoTransaction)
		}
		return
	}

	if idpErr := r.URL.Query().Get("error"); idpErr != "" {
		if idpErr == "access_denied" {
			fail(oidc.CodeDenied, oidc.ReasonIdPError)
		} else {
			fail(oidc.CodeFailed, oidc.ReasonIdPError)
		}
		return
	}

	state := r.URL.Query().Get("state")
	if !constantTimeStringsEqual(state, tx.State) {
		fail(oidc.CodeFailed, oidc.ReasonStateMismatch)
		return
	}

	code := r.URL.Query().Get("code")
	redirectURI := oidcCallbackRedirectURI(r)

	claims, err := h.oidc.CompleteLogin(r.Context(), redirectURI, code, tx)
	if err != nil {
		errCode, reason := oidcErrorCodeAndReason(err)
		fail(errCode, reason)
		return
	}

	// Best-effort audit write (AD-015): detached from request cancellation
	// so a fast client disconnect doesn't lose the row, bounded so a slow
	// database can't hang the login, and never blocking session issuance
	// (FR-028, OQ-2).
	firstSeen := h.recordExternalIdentity(claims, ip)

	if err := h.issueSessionCookie(w, r, time.Now()); err != nil {
		slog.Error("issue session cookie after sso login", "error", err.Error())
		ssoErrorRedirect(w, r, oidc.CodeFailed)
		return
	}

	if firstSeen {
		slog.Warn("new SSO identity granted admin", "issuer", claims.Issuer, "subject", claims.Subject, "ip", ip)
	}
	slog.Info("sso login", "outcome", "success", "ip", ip, "issuer", claims.Issuer, "subject", claims.Subject)

	http.Redirect(w, r, tx.ReturnPath, http.StatusFound)
}

// recordExternalIdentity upserts the audit row for a successful SSO login
// and reports whether the (issuer, subject) had never been seen before. A
// write failure is logged at error level and does not prevent the session
// from being issued (FR-028, AD-015); in that case first-seen status is
// genuinely unknown, which the log entry states explicitly.
func (h *Handlers) recordExternalIdentity(claims oidc.Claims, ip string) bool {
	if h.externalIdentityRepo == nil {
		return false
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(context.Background()), 5*time.Second)
	defer cancel()

	now := time.Now
	if h.oidcClock != nil {
		now = h.oidcClock
	}

	firstSeen, err := h.externalIdentityRepo.RecordLogin(ctx, models.ExternalIdentity{
		Issuer:        claims.Issuer,
		Subject:       claims.Subject,
		Email:         claims.Email,
		EmailVerified: claims.EmailVerified,
		LoginAt:       now().UTC(),
	})
	if err != nil {
		slog.Error("record external identity", "error", err.Error(), "issuer", claims.Issuer, "subject", claims.Subject, "first_seen_unknown", true, "ip", ip)
		return false
	}
	return firstSeen
}

// oidcErrorCodeAndReason extracts the browser-visible code and log-only
// reason from an error returned by the oidc package. A non-*LoginError
// reaching here is a bug — it is logged at error level and mapped to the
// generic sso_failed code.
func oidcErrorCodeAndReason(err error) (code, reason string) {
	var loginErr *oidc.LoginError
	if errors.As(err, &loginErr) {
		return loginErr.Code, loginErr.Reason
	}
	slog.Error("unexpected non-LoginError from oidc package", "error", err.Error())
	return oidc.CodeFailed, "internal_error"
}
