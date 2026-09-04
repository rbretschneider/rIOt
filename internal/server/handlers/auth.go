package handlers

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/DesyncTheThird/rIOt/internal/server/middleware"
	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
)

// sessionCookieName is the name of rIOt's admin session cookie, shared by
// password login (Login) and SSO login (OIDCCallback), minted only through
// issueSessionCookie so the two are identical by construction (OIDC-001 AD-008).
const sessionCookieName = "riot_session"

// issueSessionCookie mints the standard 24h admin JWT and sets it as the
// riot_session cookie. Login and OIDCCallback both call this — no handler
// constructs a riot_session cookie of its own. SameSite=Lax (rather than
// Strict) is required so the cookie survives the cross-site top-level
// navigation that lands the browser back from the IdP after a successful SSO
// callback (FR-036/AC-009). Secure is derived from the resolved request
// scheme (middleware.RequestScheme) so plain-HTTP LAN deployments
// (NFR-010) are unaffected, while an https deployment gets the compensating
// control that pairs with SameSite=Lax (AD-008, SEC-001).
func (h *Handlers) issueSessionCookie(w http.ResponseWriter, r *http.Request, now time.Time) error {
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub": "admin",
		"exp": now.Add(24 * time.Hour).Unix(),
		"iat": now.Unix(),
	})
	tokenStr, err := token.SignedString(h.jwtSecret)
	if err != nil {
		return err
	}

	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    tokenStr,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   middleware.RequestScheme(r) == "https",
		MaxAge:   86400, // 24 hours
	})
	return nil
}

// clearSessionCookie clears the riot_session cookie with the same
// name/path/attributes as issueSessionCookie, so Logout and any OIDC failure
// path clear exactly what was set.
func clearSessionCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   middleware.RequestScheme(r) == "https",
		MaxAge:   -1,
	})
}

// Login handles POST /api/v1/auth/login.
func (h *Handlers) Login(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Password == "" {
		http.Error(w, `{"error":"invalid request"}`, http.StatusBadRequest)
		return
	}

	// Get password hash from database
	hash, err := h.adminRepo.GetPasswordHash(r.Context())
	if err != nil || hash == "" {
		http.Error(w, `{"error":"invalid credentials"}`, http.StatusUnauthorized)
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(req.Password)); err != nil {
		http.Error(w, `{"error":"invalid credentials"}`, http.StatusUnauthorized)
		return
	}

	if err := h.issueSessionCookie(w, r, time.Now()); err != nil {
		http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// Logout handles POST /api/v1/auth/logout. Clears a session established by
// SSO exactly as it clears one established by password (OIDC-001 FR-032) —
// both were minted by the same issueSessionCookie helper, so the same
// clearSessionCookie call matches either.
func (h *Handlers) Logout(w http.ResponseWriter, r *http.Request) {
	clearSessionCookie(w, r)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// AuthCheck handles GET /api/v1/auth/check.
func (h *Handlers) AuthCheck(w http.ResponseWriter, r *http.Request) {
	// Check if setup is needed
	needsSetup := false
	complete, _ := h.adminRepo.IsSetupComplete(r.Context())
	if !complete {
		// Also check if password exists (legacy setups without setup_complete flag)
		hash, err := h.adminRepo.GetPasswordHash(r.Context())
		if err != nil || hash == "" {
			needsSetup = true
		}
	}

	cookie, err := r.Cookie("riot_session")
	if err != nil || cookie.Value == "" {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"authenticated": false,
			"needs_setup":   needsSetup,
		})
		return
	}

	token, err := jwt.Parse(cookie.Value, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, jwt.ErrSignatureInvalid
		}
		return h.jwtSecret, nil
	})
	if err != nil || !token.Valid {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"authenticated": false,
			"needs_setup":   needsSetup,
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"authenticated": true,
		"needs_setup":   false,
	})
}

// ChangePassword handles POST /api/v1/auth/change-password.
func (h *Handlers) ChangePassword(w http.ResponseWriter, r *http.Request) {
	var req struct {
		CurrentPassword string `json:"current_password"`
		NewPassword     string `json:"new_password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.NewPassword == "" {
		http.Error(w, `{"error":"invalid request"}`, http.StatusBadRequest)
		return
	}

	if len(req.NewPassword) < 8 {
		http.Error(w, `{"error":"password must be at least 8 characters"}`, http.StatusBadRequest)
		return
	}

	// Verify current password
	hash, err := h.adminRepo.GetPasswordHash(r.Context())
	if err != nil || hash == "" {
		http.Error(w, `{"error":"no password configured"}`, http.StatusBadRequest)
		return
	}
	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(req.CurrentPassword)); err != nil {
		http.Error(w, `{"error":"current password is incorrect"}`, http.StatusUnauthorized)
		return
	}

	// Hash and store new password
	newHash, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
		return
	}
	if err := h.adminRepo.SetPasswordHash(r.Context(), string(newHash)); err != nil {
		http.Error(w, `{"error":"failed to update password"}`, http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
