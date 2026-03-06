package handlers

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/DesyncTheThird/rIOt/internal/server/ca"
	"github.com/DesyncTheThird/rIOt/internal/server/db"
	"golang.org/x/crypto/bcrypt"
)

// SetupHandler handles first-time setup wizard endpoints.
type SetupHandler struct {
	adminRepo db.AdminRepository
	caRepo    db.CARepository
	onComplete func() // callback to trigger TLS restart
}

// NewSetupHandler creates a new SetupHandler.
func NewSetupHandler(adminRepo db.AdminRepository, caRepo db.CARepository, onComplete func()) *SetupHandler {
	return &SetupHandler{
		adminRepo:  adminRepo,
		caRepo:     caRepo,
		onComplete: onComplete,
	}
}

// Status handles GET /api/v1/setup/status.
func (h *SetupHandler) Status(w http.ResponseWriter, r *http.Request) {
	complete, _ := h.adminRepo.IsSetupComplete(r.Context())
	hasPassword := false
	hash, err := h.adminRepo.GetPasswordHash(r.Context())
	if err == nil && hash != "" {
		hasPassword = true
	}
	writeJSON(w, http.StatusOK, map[string]bool{
		"needs_setup":  !complete,
		"has_password": hasPassword,
	})
}

// Complete handles POST /api/v1/setup/complete.
func (h *SetupHandler) Complete(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Verify setup not already done
	complete, _ := h.adminRepo.IsSetupComplete(ctx)
	if complete {
		http.Error(w, `{"error":"setup already complete"}`, http.StatusConflict)
		return
	}

	var req struct {
		Password    string `json:"password"`
		TLSMode     string `json:"tls_mode"`     // "self-signed", "letsencrypt", "none"
		TLSDomain   string `json:"tls_domain"`   // for letsencrypt
		MTLSEnabled bool   `json:"mtls_enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}

	if req.Password == "" {
		http.Error(w, `{"error":"password is required"}`, http.StatusBadRequest)
		return
	}
	if req.TLSMode == "" {
		req.TLSMode = "none"
	}

	// 1. Hash and store password
	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		slog.Error("setup: hash password", "error", err)
		http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
		return
	}
	if err := h.adminRepo.SetPasswordHash(ctx, string(hash)); err != nil {
		slog.Error("setup: store password", "error", err)
		http.Error(w, `{"error":"failed to store password"}`, http.StatusInternalServerError)
		return
	}

	// 2. Generate and persist JWT secret
	jwtBytes := make([]byte, 32)
	rand.Read(jwtBytes)
	jwtSecret := hex.EncodeToString(jwtBytes)
	if err := h.adminRepo.SetConfig(ctx, "jwt_secret", jwtSecret); err != nil {
		slog.Error("setup: store jwt secret", "error", err)
		http.Error(w, `{"error":"failed to store jwt secret"}`, http.StatusInternalServerError)
		return
	}

	// 3. TLS configuration
	if err := h.adminRepo.SetConfig(ctx, "tls_mode", req.TLSMode); err != nil {
		slog.Error("setup: store tls_mode", "error", err)
		http.Error(w, `{"error":"failed to store tls config"}`, http.StatusInternalServerError)
		return
	}

	switch req.TLSMode {
	case "self-signed":
		if err := h.adminRepo.SetConfig(ctx, "tls_enabled", "true"); err != nil {
			slog.Error("setup: store tls_enabled", "error", err)
			http.Error(w, `{"error":"failed to store tls config"}`, http.StatusInternalServerError)
			return
		}
		hostname := r.Host
		// Strip port if present
		for i := len(hostname) - 1; i >= 0; i-- {
			if hostname[i] == ':' {
				hostname = hostname[:i]
				break
			}
		}
		certPEM, keyPEM, err := ca.GenerateServerTLS(hostname, nil, 3650)
		if err != nil {
			slog.Error("setup: generate server TLS cert", "error", err)
			http.Error(w, `{"error":"failed to generate TLS certificate"}`, http.StatusInternalServerError)
			return
		}
		if err := h.adminRepo.StoreServerTLSCert(ctx, string(certPEM), string(keyPEM)); err != nil {
			slog.Error("setup: store TLS cert", "error", err)
			http.Error(w, `{"error":"failed to store TLS certificate"}`, http.StatusInternalServerError)
			return
		}
		slog.Info("setup: generated self-signed TLS certificate", "hostname", hostname)

	case "letsencrypt":
		if req.TLSDomain == "" {
			http.Error(w, `{"error":"domain is required for Let's Encrypt"}`, http.StatusBadRequest)
			return
		}
		if err := h.adminRepo.SetConfig(ctx, "tls_enabled", "true"); err != nil {
			slog.Error("setup: store tls_enabled", "error", err)
			http.Error(w, `{"error":"failed to store tls config"}`, http.StatusInternalServerError)
			return
		}
		if err := h.adminRepo.SetConfig(ctx, "tls_domain", req.TLSDomain); err != nil {
			slog.Error("setup: store tls_domain", "error", err)
			http.Error(w, `{"error":"failed to store tls config"}`, http.StatusInternalServerError)
			return
		}
		slog.Info("setup: configured Let's Encrypt TLS", "domain", req.TLSDomain)

	default: // "none"
		if err := h.adminRepo.SetConfig(ctx, "tls_enabled", "false"); err != nil {
			slog.Error("setup: store tls_enabled", "error", err)
		}
	}

	// 4. mTLS preference
	mtlsVal := "false"
	if req.MTLSEnabled {
		mtlsVal = "true"
	}
	if err := h.adminRepo.SetConfig(ctx, "mtls_enabled", mtlsVal); err != nil {
		slog.Error("setup: store mtls_enabled", "error", err)
	}

	// 5. Mark setup complete
	if err := h.adminRepo.SetConfig(ctx, "setup_complete", "true"); err != nil {
		slog.Error("setup: mark complete", "error", err)
		http.Error(w, `{"error":"failed to finalize setup"}`, http.StatusInternalServerError)
		return
	}

	slog.Info("setup wizard completed")

	// 6. Trigger TLS restart if callback is set
	if h.onComplete != nil {
		go h.onComplete()
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"ok":          true,
		"tls_mode":    req.TLSMode,
		"tls_enabled": req.TLSMode != "none",
	})
}
