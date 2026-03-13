package middleware

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DesyncTheThird/rIOt/internal/models"
	"github.com/DesyncTheThird/rIOt/internal/testutil"
	"github.com/stretchr/testify/assert"
)

func makeSelfSignedCert(t *testing.T, cn string, serial int64) *x509.Certificate {
	t.Helper()
	return &x509.Certificate{
		SerialNumber: big.NewInt(serial),
		Subject:      pkix.Name{CommonName: cn},
		NotBefore:    time.Now().Add(-1 * time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
	}
}

func TestMTLSDeviceAuth_NoCert(t *testing.T) {
	devices := testutil.NewMockDeviceRepo()
	caRepo := testutil.NewMockCARepo()

	handler := MTLSDeviceAuth(devices, caRepo)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// No TLS at all
	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Contains(t, rec.Body.String(), "client certificate required")
}

func TestMTLSDeviceAuth_EmptyPeerCerts(t *testing.T) {
	devices := testutil.NewMockDeviceRepo()
	caRepo := testutil.NewMockCARepo()

	handler := MTLSDeviceAuth(devices, caRepo)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	req.TLS = &tls.ConnectionState{PeerCertificates: []*x509.Certificate{}}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestMTLSDeviceAuth_EmptyCN(t *testing.T) {
	devices := testutil.NewMockDeviceRepo()
	caRepo := testutil.NewMockCARepo()

	handler := MTLSDeviceAuth(devices, caRepo)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	cert := makeSelfSignedCert(t, "", 1)
	req := httptest.NewRequest("GET", "/test", nil)
	req.TLS = &tls.ConnectionState{PeerCertificates: []*x509.Certificate{cert}}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Contains(t, rec.Body.String(), "certificate has no CN")
}

func TestMTLSDeviceAuth_RevokedCert(t *testing.T) {
	devices := testutil.NewMockDeviceRepo()
	devices.Devices["device-1"] = &models.Device{ID: "device-1", Hostname: "test"}

	caRepo := testutil.NewMockCARepo()
	caRepo.Certs = []models.DeviceCert{
		{DeviceID: "device-1", SerialNumber: "a", Revoked: true},
	}

	handler := MTLSDeviceAuth(devices, caRepo)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Serial "a" in hex = 0xa = 10 decimal
	cert := makeSelfSignedCert(t, "device-1", 0xa)
	req := httptest.NewRequest("GET", "/test", nil)
	req.TLS = &tls.ConnectionState{PeerCertificates: []*x509.Certificate{cert}}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Contains(t, rec.Body.String(), "certificate revoked")
}

func TestMTLSDeviceAuth_DeviceNotFound(t *testing.T) {
	devices := testutil.NewMockDeviceRepo()
	caRepo := testutil.NewMockCARepo()

	handler := MTLSDeviceAuth(devices, caRepo)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	cert := makeSelfSignedCert(t, "unknown-device", 1)
	req := httptest.NewRequest("GET", "/test", nil)
	req.TLS = &tls.ConnectionState{PeerCertificates: []*x509.Certificate{cert}}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Contains(t, rec.Body.String(), "device not found")
}

func TestMTLSDeviceAuth_ValidCert(t *testing.T) {
	devices := testutil.NewMockDeviceRepo()
	devices.Devices["device-1"] = &models.Device{ID: "device-1", Hostname: "test"}
	caRepo := testutil.NewMockCARepo()

	var gotDeviceID string
	handler := MTLSDeviceAuth(devices, caRepo)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotDeviceID = DeviceIDFromMTLS(r.Context())
		w.WriteHeader(http.StatusOK)
	}))

	// Generate a real key to make a proper cert with serializable serial
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	cert := makeSelfSignedCert(t, "device-1", 42)
	certDER, _ := x509.CreateCertificate(rand.Reader, cert, cert, &key.PublicKey, key)
	parsedCert, _ := x509.ParseCertificate(certDER)

	req := httptest.NewRequest("GET", "/test", nil)
	req.TLS = &tls.ConnectionState{PeerCertificates: []*x509.Certificate{parsedCert}}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "device-1", gotDeviceID)
}

func TestDeviceIDFromMTLS_NoValue(t *testing.T) {
	req := httptest.NewRequest("GET", "/test", nil)
	assert.Equal(t, "", DeviceIDFromMTLS(req.Context()))
}
