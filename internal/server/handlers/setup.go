package handlers

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net"
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
		Password    string   `json:"password"`
		TLSMode     string   `json:"tls_mode"`     // "self-signed", "letsencrypt", "none"
		TLSDomain   string   `json:"tls_domain"`   // for letsencrypt
		MTLSEnabled bool     `json:"mtls_enabled"`
		ExtraSANs   []string `json:"extra_sans"`   // additional hostnames/IPs for cert SANs
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
		slog.Error("setup: hash password", "error", err.Error())
		http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
		return
	}
	if err := h.adminRepo.SetPasswordHash(ctx, string(hash)); err != nil {
		slog.Error("setup: store password", "error", err.Error())
		http.Error(w, `{"error":"failed to store password"}`, http.StatusInternalServerError)
		return
	}

	// 2. Generate and persist JWT secret
	jwtBytes := make([]byte, 32)
	rand.Read(jwtBytes)
	jwtSecret := hex.EncodeToString(jwtBytes)
	if err := h.adminRepo.SetConfig(ctx, "jwt_secret", jwtSecret); err != nil {
		slog.Error("setup: store jwt secret", "error", err.Error())
		http.Error(w, `{"error":"failed to store jwt secret"}`, http.StatusInternalServerError)
		return
	}

	// 3. TLS configuration
	if err := h.adminRepo.SetConfig(ctx, "tls_mode", req.TLSMode); err != nil {
		slog.Error("setup: store tls_mode", "error", err.Error())
		http.Error(w, `{"error":"failed to store tls config"}`, http.StatusInternalServerError)
		return
	}

	switch req.TLSMode {
	case "self-signed":
		if err := h.adminRepo.SetConfig(ctx, "tls_enabled", "true"); err != nil {
			slog.Error("setup: store tls_enabled", "error", err.Error())
			http.Error(w, `{"error":"failed to store tls config"}`, http.StatusInternalServerError)
			return
		}
		hostname := stripPort(r.Host)
		// Collect extra IPs: the host the admin used (important when server
		// runs in Docker — container interfaces won't include the host IP)
		var extraIPs []net.IP
		if ip := net.ParseIP(hostname); ip != nil {
			extraIPs = append(extraIPs, ip)
		}
		// Also include X-Real-IP if set (from reverse proxy)
		if realIP := r.Header.Get("X-Real-Ip"); realIP != "" {
			if ip := net.ParseIP(realIP); ip != nil {
				extraIPs = append(extraIPs, ip)
			}
		}
		// Validate and persist extra SANs (DDNS hostnames, external IPs)
		if len(req.ExtraSANs) > 0 {
			if err := ca.ValidateSANs(req.ExtraSANs); err != nil {
				http.Error(w, `{"error":"invalid extra SAN: `+err.Error()+`"}`, http.StatusBadRequest)
				return
			}
			sansJSON, _ := json.Marshal(req.ExtraSANs)
			if err := h.adminRepo.SetConfig(ctx, "tls_extra_sans", string(sansJSON)); err != nil {
				slog.Error("setup: store tls_extra_sans", "error", err.Error())
			}
		}
		certPEM, keyPEM, err := ca.GenerateServerTLS(hostname, extraIPs, 3650, req.ExtraSANs...)
		if err != nil {
			slog.Error("setup: generate server TLS cert", "error", err.Error())
			http.Error(w, `{"error":"failed to generate TLS certificate"}`, http.StatusInternalServerError)
			return
		}
		if err := h.adminRepo.StoreServerTLSCert(ctx, string(certPEM), string(keyPEM)); err != nil {
			slog.Error("setup: store TLS cert", "error", err.Error())
			http.Error(w, `{"error":"failed to store TLS certificate"}`, http.StatusInternalServerError)
			return
		}
		slog.Info("setup: generated self-signed TLS certificate", "hostname", hostname, "extra_sans", req.ExtraSANs)

	case "letsencrypt":
		if req.TLSDomain == "" {
			http.Error(w, `{"error":"domain is required for Let's Encrypt"}`, http.StatusBadRequest)
			return
		}
		if err := h.adminRepo.SetConfig(ctx, "tls_enabled", "true"); err != nil {
			slog.Error("setup: store tls_enabled", "error", err.Error())
			http.Error(w, `{"error":"failed to store tls config"}`, http.StatusInternalServerError)
			return
		}
		if err := h.adminRepo.SetConfig(ctx, "tls_domain", req.TLSDomain); err != nil {
			slog.Error("setup: store tls_domain", "error", err.Error())
			http.Error(w, `{"error":"failed to store tls config"}`, http.StatusInternalServerError)
			return
		}
		slog.Info("setup: configured Let's Encrypt TLS", "domain", req.TLSDomain)

	default: // "none"
		if err := h.adminRepo.SetConfig(ctx, "tls_enabled", "false"); err != nil {
			slog.Error("setup: store tls_enabled", "error", err.Error())
		}
	}

	// 4. mTLS preference
	mtlsVal := "false"
	if req.MTLSEnabled {
		mtlsVal = "true"
	}
	if err := h.adminRepo.SetConfig(ctx, "mtls_enabled", mtlsVal); err != nil {
		slog.Error("setup: store mtls_enabled", "error", err.Error())
	}

	// 5. Mark setup complete
	if err := h.adminRepo.SetConfig(ctx, "setup_complete", "true"); err != nil {
		slog.Error("setup: mark complete", "error", err.Error())
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

// RegenerateTLS handles POST /api/v1/settings/tls/regenerate.
// Generates a new server TLS certificate signed by the rIOt CA (if available)
// or self-signed, stores it, and triggers a TLS restart.
func (h *SetupHandler) RegenerateTLS(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	tlsMode, _ := h.adminRepo.GetConfig(ctx, "tls_mode")
	if tlsMode != "self-signed" {
		http.Error(w, `{"error":"TLS regeneration only available in self-signed mode"}`, http.StatusBadRequest)
		return
	}

	hostname := stripPort(r.Host)

	var extraIPs []net.IP
	if ip := net.ParseIP(hostname); ip != nil {
		extraIPs = append(extraIPs, ip)
	}
	if realIP := r.Header.Get("X-Real-Ip"); realIP != "" {
		if ip := net.ParseIP(realIP); ip != nil {
			extraIPs = append(extraIPs, ip)
		}
	}

	// Load persisted extra SANs
	extraSANs := h.loadExtraSANs(ctx)

	// Try CA-signed first (agents already trust the CA cert)
	var certPEM, keyPEM []byte
	var err error

	// Load CA from DB to try CA-signed cert
	caCertPEM, caKeyPEM, caErr := h.caRepo.GetCA(ctx)
	if caErr == nil && caCertPEM != "" {
		authority, loadErr := ca.LoadCA([]byte(caCertPEM), []byte(caKeyPEM))
		if loadErr == nil {
			certPEM, keyPEM, err = ca.GenerateServerTLSWithCA(authority, hostname, extraIPs, 3650, extraSANs...)
			if err == nil {
				slog.Info("regenerated CA-signed server TLS certificate", "hostname", hostname, "extra_sans", extraSANs)
			}
		}
	}

	// Fall back to self-signed if CA not available
	if certPEM == nil {
		certPEM, keyPEM, err = ca.GenerateServerTLS(hostname, extraIPs, 3650, extraSANs...)
		if err != nil {
			slog.Error("regenerate TLS: generate cert", "error", err.Error())
			http.Error(w, `{"error":"failed to generate TLS certificate"}`, http.StatusInternalServerError)
			return
		}
		slog.Info("regenerated self-signed server TLS certificate", "hostname", hostname, "extra_sans", extraSANs)
	}

	if err := h.adminRepo.StoreServerTLSCert(ctx, string(certPEM), string(keyPEM)); err != nil {
		slog.Error("regenerate TLS: store cert", "error", err.Error())
		http.Error(w, `{"error":"failed to store TLS certificate"}`, http.StatusInternalServerError)
		return
	}

	// Trigger TLS restart
	if h.onComplete != nil {
		go h.onComplete()
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"ok":      true,
		"message": "TLS certificate regenerated, server restarting with new certificate",
	})
}

// GetTLSSANs handles GET /api/v1/settings/tls/sans.
func (h *SetupHandler) GetTLSSANs(w http.ResponseWriter, r *http.Request) {
	sans := h.loadExtraSANs(r.Context())
	writeJSON(w, http.StatusOK, map[string]interface{}{"sans": sans})
}

// SetTLSSANs handles PUT /api/v1/settings/tls/sans.
func (h *SetupHandler) SetTLSSANs(w http.ResponseWriter, r *http.Request) {
	var req struct {
		SANs []string `json:"sans"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}

	// Trim whitespace from each entry
	for i := range req.SANs {
		req.SANs[i] = trimSAN(req.SANs[i])
	}
	// Remove empty entries
	filtered := req.SANs[:0]
	for _, s := range req.SANs {
		if s != "" {
			filtered = append(filtered, s)
		}
	}
	req.SANs = filtered

	if err := ca.ValidateSANs(req.SANs); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	sansJSON, _ := json.Marshal(req.SANs)
	if err := h.adminRepo.SetConfig(r.Context(), "tls_extra_sans", string(sansJSON)); err != nil {
		slog.Error("set TLS SANs: store", "error", err.Error())
		http.Error(w, `{"error":"failed to store SANs"}`, http.StatusInternalServerError)
		return
	}

	slog.Info("updated TLS extra SANs", "sans", req.SANs)
	writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "sans": req.SANs})
}

func (h *SetupHandler) loadExtraSANs(ctx context.Context) []string {
	raw, _ := h.adminRepo.GetConfig(ctx, "tls_extra_sans")
	if raw == "" {
		return nil
	}
	var sans []string
	json.Unmarshal([]byte(raw), &sans)
	return sans
}

func trimSAN(s string) string {
	// Trim whitespace — using a loop to avoid importing strings just for this
	for len(s) > 0 && (s[0] == ' ' || s[0] == '\t' || s[0] == '\n' || s[0] == '\r') {
		s = s[1:]
	}
	for len(s) > 0 && (s[len(s)-1] == ' ' || s[len(s)-1] == '\t' || s[len(s)-1] == '\n' || s[len(s)-1] == '\r') {
		s = s[:len(s)-1]
	}
	return s
}

func stripPort(host string) string {
	for i := len(host) - 1; i >= 0; i-- {
		if host[i] == ':' {
			return host[:i]
		}
	}
	return host
}
