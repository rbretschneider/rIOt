package handlers

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/DesyncTheThird/rIOt/internal/models"
	"github.com/DesyncTheThird/rIOt/internal/server/ca"
	"github.com/DesyncTheThird/rIOt/internal/server/db"
	"github.com/DesyncTheThird/rIOt/internal/server/middleware"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// EnrollHandler handles mTLS enrollment.
type EnrollHandler struct {
	ca      *ca.CA
	caRepo  db.CARepository
	devices db.DeviceRepository
}

// NewEnrollHandler creates a new enrollment handler.
func NewEnrollHandler(c *ca.CA, caRepo db.CARepository, devices db.DeviceRepository) *EnrollHandler {
	return &EnrollHandler{ca: c, caRepo: caRepo, devices: devices}
}

// Enroll handles POST /api/v1/enroll.
func (h *EnrollHandler) Enroll(w http.ResponseWriter, r *http.Request) {
	var req models.EnrollRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}
	if req.CSRPEM == "" || req.BootstrapKey == "" {
		http.Error(w, `{"error":"csr_pem and bootstrap_key are required"}`, http.StatusBadRequest)
		return
	}

	// Hash and validate bootstrap key
	keyHash := hashKey(req.BootstrapKey)
	bk, err := h.caRepo.LookupBootstrapKey(r.Context(), keyHash)
	if err != nil {
		http.Error(w, `{"error":"invalid bootstrap key"}`, http.StatusUnauthorized)
		return
	}
	if bk.Used {
		http.Error(w, `{"error":"bootstrap key already used"}`, http.StatusUnauthorized)
		return
	}
	if time.Now().After(bk.ExpiresAt) {
		http.Error(w, `{"error":"bootstrap key expired"}`, http.StatusUnauthorized)
		return
	}

	// Create or find device
	deviceID := uuid.New().String()
	hostname := req.Hostname
	if hostname == "" {
		hostname = "unknown"
	}

	device := &models.Device{
		ID:       deviceID,
		ShortID:  deviceID[:8],
		Hostname: hostname,
		Arch:     req.Arch,
		Status:   models.DeviceStatusOnline,
		Tags:     []string{},
	}
	now := time.Now().UTC()
	device.CreatedAt = now
	device.UpdatedAt = now

	if err := h.devices.Create(r.Context(), device); err != nil {
		slog.Error("enroll: create device", "error", err.Error())
		http.Error(w, `{"error":"failed to create device"}`, http.StatusInternalServerError)
		return
	}

	// Sign CSR
	validity := 10 * 365 * 24 * time.Hour // 10 years
	certPEM, serialHex, notBefore, notAfter, err := h.ca.SignCSR([]byte(req.CSRPEM), deviceID, validity)
	if err != nil {
		slog.Error("enroll: sign CSR", "error", err.Error())
		http.Error(w, `{"error":"failed to sign certificate"}`, http.StatusInternalServerError)
		return
	}

	// Store cert record
	deviceCert := &models.DeviceCert{
		DeviceID:     deviceID,
		SerialNumber: serialHex,
		CertPEM:      string(certPEM),
		NotBefore:    notBefore,
		NotAfter:     notAfter,
	}
	if err := h.caRepo.StoreCert(r.Context(), deviceCert); err != nil {
		slog.Error("enroll: store cert", "error", err.Error())
		http.Error(w, `{"error":"failed to store certificate"}`, http.StatusInternalServerError)
		return
	}

	// Mark bootstrap key used
	if err := h.caRepo.MarkBootstrapKeyUsed(r.Context(), keyHash, deviceID); err != nil {
		slog.Error("enroll: mark key used", "error", err.Error())
	}

	slog.Info("device enrolled via mTLS", "device_id", deviceID, "hostname", hostname, "serial", serialHex)

	writeJSON(w, http.StatusCreated, models.EnrollResponse{
		DeviceID:  deviceID,
		CertPEM:   string(certPEM),
		CACertPEM: string(h.ca.CertPEM()),
	})
}

// CACert handles GET /api/v1/ca.pem.
func (h *EnrollHandler) CACert(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/x-pem-file")
	w.Write(h.ca.CertPEM())
}

// ListCerts handles GET /api/v1/settings/certs.
func (h *EnrollHandler) ListCerts(w http.ResponseWriter, r *http.Request) {
	certs, err := h.caRepo.ListCerts(r.Context())
	if err != nil {
		http.Error(w, `{"error":"failed to list certs"}`, http.StatusInternalServerError)
		return
	}
	if certs == nil {
		certs = []models.DeviceCert{}
	}
	writeJSON(w, http.StatusOK, certs)
}

// RevokeCert handles POST /api/v1/settings/certs/{serial}/revoke.
func (h *EnrollHandler) RevokeCert(w http.ResponseWriter, r *http.Request) {
	serial := chi.URLParam(r, "serial")
	if err := h.caRepo.RevokeCert(r.Context(), serial); err != nil {
		http.Error(w, `{"error":"failed to revoke cert"}`, http.StatusInternalServerError)
		return
	}
	slog.Info("certificate revoked", "serial", serial)
	writeJSON(w, http.StatusOK, map[string]string{"status": "revoked"})
}

// ListBootstrapKeys handles GET /api/v1/settings/bootstrap-keys.
func (h *EnrollHandler) ListBootstrapKeys(w http.ResponseWriter, r *http.Request) {
	keys, err := h.caRepo.ListBootstrapKeys(r.Context())
	if err != nil {
		http.Error(w, `{"error":"failed to list bootstrap keys"}`, http.StatusInternalServerError)
		return
	}
	if keys == nil {
		keys = []models.BootstrapKey{}
	}
	writeJSON(w, http.StatusOK, keys)
}

// CreateBootstrapKey handles POST /api/v1/settings/bootstrap-keys.
func (h *EnrollHandler) CreateBootstrapKey(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Label     string `json:"label"`
		ExpiresIn int    `json:"expires_in_hours"` // hours until expiry
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}
	if req.ExpiresIn <= 0 {
		req.ExpiresIn = 24 // default 24h
	}

	// Generate a random key
	plaintext := generateAPIKey() // reuse the key generator
	keyHash := hashKey(plaintext)
	expiresAt := time.Now().Add(time.Duration(req.ExpiresIn) * time.Hour)

	if err := h.caRepo.CreateBootstrapKey(r.Context(), keyHash, req.Label, expiresAt); err != nil {
		http.Error(w, `{"error":"failed to create bootstrap key"}`, http.StatusInternalServerError)
		return
	}

	// Return plaintext key only once
	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"key":        plaintext,
		"key_hash":   keyHash,
		"label":      req.Label,
		"expires_at": expiresAt,
	})
}

// DeleteBootstrapKey handles DELETE /api/v1/settings/bootstrap-keys/{hash}.
func (h *EnrollHandler) DeleteBootstrapKey(w http.ResponseWriter, r *http.Request) {
	keyHash := chi.URLParam(r, "hash")
	if err := h.caRepo.DeleteBootstrapKey(r.Context(), keyHash); err != nil {
		http.Error(w, `{"error":"failed to delete bootstrap key"}`, http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// Renew handles POST /api/v1/renew — device presents existing valid mTLS cert,
// server issues a new cert and revokes the old one.
func (h *EnrollHandler) Renew(w http.ResponseWriter, r *http.Request) {
	// Device ID is already validated by MTLSDeviceAuth middleware
	devID := middleware.DeviceIDFromMTLS(r.Context())
	if devID == "" {
		http.Error(w, `{"error":"mTLS authentication required"}`, http.StatusUnauthorized)
		return
	}

	var req struct {
		CSRPEM string `json:"csr_pem"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.CSRPEM == "" {
		http.Error(w, `{"error":"csr_pem is required"}`, http.StatusBadRequest)
		return
	}

	// Get current cert to revoke after renewal
	oldCert, err := h.caRepo.GetCertByDevice(r.Context(), devID)
	if err != nil {
		slog.Error("renew: get old cert", "error", err.Error())
	}

	// Sign new CSR
	validity := 10 * 365 * 24 * time.Hour
	certPEM, serialHex, notBefore, notAfter, err := h.ca.SignCSR([]byte(req.CSRPEM), devID, validity)
	if err != nil {
		slog.Error("renew: sign CSR", "error", err.Error())
		http.Error(w, `{"error":"failed to sign certificate"}`, http.StatusInternalServerError)
		return
	}

	// Store new cert
	deviceCert := &models.DeviceCert{
		DeviceID:     devID,
		SerialNumber: serialHex,
		CertPEM:      string(certPEM),
		NotBefore:    notBefore,
		NotAfter:     notAfter,
	}
	if err := h.caRepo.StoreCert(r.Context(), deviceCert); err != nil {
		slog.Error("renew: store cert", "error", err.Error())
		http.Error(w, `{"error":"failed to store certificate"}`, http.StatusInternalServerError)
		return
	}

	// Revoke old cert
	if oldCert != nil && !oldCert.Revoked {
		if err := h.caRepo.RevokeCert(r.Context(), oldCert.SerialNumber); err != nil {
			slog.Error("renew: revoke old cert", "error", err.Error())
		}
	}

	slog.Info("device certificate renewed", "device_id", devID, "new_serial", serialHex)

	writeJSON(w, http.StatusOK, models.EnrollResponse{
		DeviceID:  devID,
		CertPEM:   string(certPEM),
		CACertPEM: string(h.ca.CertPEM()),
	})
}

func hashKey(key string) string {
	h := sha256.Sum256([]byte(key))
	return hex.EncodeToString(h[:])
}
