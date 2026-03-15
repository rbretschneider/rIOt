package agent

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/DesyncTheThird/rIOt/internal/models"
)

// enroll performs mTLS enrollment with the server using a bootstrap key.
// It generates a key pair, creates a CSR, sends it to the server, and saves
// the signed certificate, key, and CA cert to disk.
func (a *Agent) enroll() error {
	slog.Info("starting mTLS enrollment")

	hostname, _ := os.Hostname()

	// Generate ECDSA P-256 key pair
	privKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return fmt.Errorf("generate key: %w", err)
	}

	// Create CSR
	csrTemplate := &x509.CertificateRequest{
		Subject: pkix.Name{
			CommonName: hostname,
		},
	}
	csrDER, err := x509.CreateCertificateRequest(rand.Reader, csrTemplate, privKey)
	if err != nil {
		return fmt.Errorf("create CSR: %w", err)
	}
	csrPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: csrDER})

	// Send enrollment request
	enrollReq := models.EnrollRequest{
		CSRPEM:       string(csrPEM),
		BootstrapKey: a.config.Server.BootstrapKey,
		Hostname:     hostname,
		Arch:         runtime.GOARCH,
	}
	body, _ := json.Marshal(enrollReq)

	// Build a TLS-aware client using the pinned server cert (from TOFU) if available
	client := &http.Client{Timeout: 30 * time.Second}
	if a.config.Server.CACertFile != "" {
		caCert, err := os.ReadFile(a.config.Server.CACertFile)
		if err == nil {
			pool := x509.NewCertPool()
			pool.AppendCertsFromPEM(caCert)
			client.Transport = &http.Transport{
				TLSClientConfig: &tls.Config{RootCAs: pool},
			}
		}
	}

	resp, err := client.Post(a.config.Server.URL+"/api/v1/enroll", "application/json", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("enrollment request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("enrollment failed (HTTP %d): %s", resp.StatusCode, string(b))
	}

	var enrollResp models.EnrollResponse
	if err := json.NewDecoder(resp.Body).Decode(&enrollResp); err != nil {
		return fmt.Errorf("decode enrollment response: %w", err)
	}

	// Ensure config directory exists
	certPath := CertPath()
	configDir := filepath.Dir(certPath)
	if err := os.MkdirAll(configDir, 0700); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}

	// Save client certificate
	if err := os.WriteFile(certPath, []byte(enrollResp.CertPEM), 0600); err != nil {
		return fmt.Errorf("save client cert: %w", err)
	}

	// Save client key
	keyDER, err := x509.MarshalECPrivateKey(privKey)
	if err != nil {
		return fmt.Errorf("marshal key: %w", err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	if err := os.WriteFile(KeyPath(), keyPEM, 0600); err != nil {
		return fmt.Errorf("save client key: %w", err)
	}

	// Save CA certificate
	if err := os.WriteFile(CACertPath(), []byte(enrollResp.CACertPEM), 0644); err != nil {
		return fmt.Errorf("save CA cert: %w", err)
	}

	// Update config
	a.config.Server.ClientCert = certPath
	a.config.Server.ClientKey = KeyPath()
	a.config.Server.CACertFile = CACertPath()
	a.config.Server.BootstrapKey = "" // clear after use
	a.config.Agent.DeviceID = enrollResp.DeviceID

	// Save updated config to disk
	if err := a.config.Save(a.configPath); err != nil {
		slog.Warn("failed to save config after enrollment", "error", err)
	}

	slog.Info("mTLS enrollment complete", "device_id", enrollResp.DeviceID)
	return nil
}

// renewCertIfNeeded checks the client certificate expiry and renews it if
// it expires within 30 days. The server issues a new certificate via the
// /api/v1/renew endpoint, which requires a valid mTLS client cert.
func (a *Agent) renewCertIfNeeded() error {
	certPEM, err := os.ReadFile(a.config.Server.ClientCert)
	if err != nil {
		return nil // no cert to renew
	}

	block, _ := pem.Decode(certPEM)
	if block == nil {
		return nil
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil
	}

	daysLeft := time.Until(cert.NotAfter).Hours() / 24
	if daysLeft > 30 {
		slog.Info("client certificate valid", "expires_in_days", int(daysLeft))
		return nil
	}

	slog.Info("client certificate expiring soon, attempting renewal", "days_left", int(daysLeft))

	// Generate new key pair and CSR
	privKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return fmt.Errorf("generate key: %w", err)
	}

	hostname, _ := os.Hostname()
	csrTemplate := &x509.CertificateRequest{
		Subject: pkix.Name{
			CommonName: hostname,
		},
	}
	csrDER, err := x509.CreateCertificateRequest(rand.Reader, csrTemplate, privKey)
	if err != nil {
		return fmt.Errorf("create CSR: %w", err)
	}
	csrPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: csrDER})

	// Build mTLS client for renewal request
	renewReq := struct {
		CSRPEM string `json:"csr_pem"`
	}{CSRPEM: string(csrPEM)}
	body, _ := json.Marshal(renewReq)

	tlsCert, err := tls.LoadX509KeyPair(a.config.Server.ClientCert, a.config.Server.ClientKey)
	if err != nil {
		return fmt.Errorf("load client cert for renewal: %w", err)
	}

	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{tlsCert},
	}
	if a.config.Server.CACertFile != "" {
		caCert, err := os.ReadFile(a.config.Server.CACertFile)
		if err == nil {
			pool := x509.NewCertPool()
			pool.AppendCertsFromPEM(caCert)
			tlsConfig.RootCAs = pool
		}
	}

	client := &http.Client{
		Timeout:   30 * time.Second,
		Transport: &http.Transport{TLSClientConfig: tlsConfig},
	}

	resp, err := client.Post(a.config.Server.URL+"/api/v1/renew", "application/json", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("renewal request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("renewal failed (HTTP %d): %s", resp.StatusCode, string(b))
	}

	var renewResp models.EnrollResponse
	if err := json.NewDecoder(resp.Body).Decode(&renewResp); err != nil {
		return fmt.Errorf("decode renewal response: %w", err)
	}

	// Save new certificate
	if err := os.WriteFile(CertPath(), []byte(renewResp.CertPEM), 0600); err != nil {
		return fmt.Errorf("save renewed cert: %w", err)
	}

	// Save new key
	keyDER, err := x509.MarshalECPrivateKey(privKey)
	if err != nil {
		return fmt.Errorf("marshal key: %w", err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	if err := os.WriteFile(KeyPath(), keyPEM, 0600); err != nil {
		return fmt.Errorf("save renewed key: %w", err)
	}

	// Update CA cert if provided
	if renewResp.CACertPEM != "" {
		if err := os.WriteFile(CACertPath(), []byte(renewResp.CACertPEM), 0644); err != nil {
			slog.Warn("failed to update CA cert after renewal", "error", err)
		}
	}

	slog.Info("client certificate renewed successfully")
	return nil
}
