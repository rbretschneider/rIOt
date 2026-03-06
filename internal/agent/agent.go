package agent

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/DesyncTheThird/rIOt/internal/agent/collectors"
)

type Agent struct {
	config     *Config
	configPath string
	version    string
	registry   *collectors.Registry
	buffer     *Buffer
	client     *HTTPClient
	wsClient   *agentWSClient
}

func New(configPath, version string) (*Agent, error) {
	cfg, err := LoadConfig(configPath)
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}

	registry := collectors.NewRegistry()
	registry.RegisterDefaultsWithDocker(collectors.DockerOptions{
		CollectStats: cfg.Docker.CollectStats,
		SocketPath:   cfg.Docker.SocketPath,
	})

	return &Agent{
		config:     cfg,
		configPath: configPath,
		version:    version,
		registry:   registry,
	}, nil
}

func (a *Agent) Run() error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	// mTLS enrollment: if no client cert exists but a bootstrap key is set, enroll first
	if a.config.Server.ClientCert == "" && a.config.Server.BootstrapKey != "" {
		if err := a.enroll(); err != nil {
			return fmt.Errorf("mTLS enrollment failed: %w", err)
		}
	}

	// TOFU: pin server cert on first HTTPS connection
	if strings.HasPrefix(a.config.Server.URL, "https://") {
		if err := a.trustServerCert(); err != nil {
			slog.Warn("TOFU cert pinning failed, continuing with current TLS settings", "error", err)
		}
	}

	// Initialize HTTP client
	if a.config.Server.ClientCert != "" || a.config.Server.CACertFile != "" || !a.config.Server.TLSVerify {
		a.client = NewHTTPClientWithTLS(a.config.Server)
	} else {
		a.client = NewHTTPClient(a.config.Server.URL, a.config.Server.APIKey)
	}

	// Initialize offline buffer
	buf, err := NewBuffer(BufferPath())
	if err != nil {
		slog.Warn("failed to init offline buffer, continuing without", "error", err)
	} else {
		a.buffer = buf
		defer a.buffer.Close()
	}

	// Register with server
	if err := a.register(ctx); err != nil {
		slog.Error("registration failed, will retry", "error", err)
	}

	// Filter collectors by config
	a.registry.FilterEnabled(a.config.Collector.Enabled)

	// Start loops
	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		a.heartbeatLoop(ctx)
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		a.telemetryLoop(ctx)
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		a.updateCheckLoop(ctx)
	}()

	// Start Docker event watcher if Docker is available
	if shouldEnableDocker(a.config.Docker) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			slog.Info("starting Docker event watcher")
			a.dockerEventLoop(ctx)
		}()
	}

	// Always start agent WebSocket client (for commands + terminal relay)
	a.wsClient = newAgentWSClient(a)
	wg.Add(1)
	go func() {
		defer wg.Done()
		slog.Info("starting agent WebSocket client")
		a.wsClient.run(ctx)
	}()

	// Start dead man's switch if configured
	if a.config.DeadMan.Enabled && a.config.DeadMan.URL != "" {
		wg.Add(1)
		go func() {
			defer wg.Done()
			slog.Info("starting dead man's switch", "url", a.config.DeadMan.URL)
			a.deadManLoop(ctx)
		}()
	}

	// Graceful shutdown
	done := make(chan os.Signal, 1)
	signal.Notify(done, os.Interrupt, syscall.SIGTERM)

	<-done
	slog.Info("shutting down agent")
	cancel()
	wg.Wait()
	slog.Info("agent stopped")
	return nil
}

func (a *Agent) heartbeatLoop(ctx context.Context) {
	interval := time.Duration(a.config.Agent.HeartbeatInterval) * time.Second
	if interval == 0 {
		interval = 15 * time.Second
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// Send first heartbeat immediately
	a.sendHeartbeat(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			a.sendHeartbeat(ctx)
		}
	}
}

func (a *Agent) telemetryLoop(ctx context.Context) {
	interval := time.Duration(a.config.Agent.PollInterval) * time.Second
	if interval == 0 {
		interval = 60 * time.Second
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// Send first telemetry immediately
	a.sendTelemetry(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			a.sendTelemetry(ctx)
		}
	}
}

// trustServerCert implements SSH-like TOFU for the server's TLS certificate.
// If a pinned cert already exists on disk, use it. Otherwise, fetch from
// the server's /api/v1/server-cert endpoint and pin it.
func (a *Agent) trustServerCert() error {
	certPath := ServerCertPath()

	// Already have a pinned cert — use it
	if _, err := os.Stat(certPath); err == nil {
		a.config.Server.CACertFile = certPath
		a.config.Server.TLSVerify = true
		slog.Info("using pinned server certificate", "path", certPath)
		return nil
	}

	// Fetch server cert via insecure one-shot client
	slog.Info("no pinned server certificate found, performing TOFU", "url", a.config.Server.URL)

	insecureClient := &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
	}

	resp, err := insecureClient.Get(a.config.Server.URL + "/api/v1/server-cert")
	if err != nil {
		return fmt.Errorf("fetch server cert: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("server-cert endpoint returned %d", resp.StatusCode)
	}

	var certResp struct {
		CertPEM     string `json:"cert_pem"`
		Fingerprint string `json:"fingerprint"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&certResp); err != nil {
		return fmt.Errorf("decode server cert response: %w", err)
	}

	if certResp.CertPEM == "" {
		slog.Info("server has no self-signed cert to pin (not in self-signed mode)")
		return nil
	}

	// Verify fingerprint if we have a pin configured
	if a.config.Server.ServerCertPin != "" {
		if certResp.Fingerprint != a.config.Server.ServerCertPin {
			return fmt.Errorf("server cert fingerprint mismatch: expected %s, got %s",
				a.config.Server.ServerCertPin, certResp.Fingerprint)
		}
		slog.Info("server cert fingerprint verified", "fingerprint", certResp.Fingerprint)
	}

	// Verify the PEM is a valid certificate
	block, _ := pem.Decode([]byte(certResp.CertPEM))
	if block == nil {
		return fmt.Errorf("invalid PEM data from server")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return fmt.Errorf("parse server cert: %w", err)
	}

	// Compute fingerprint for logging and config
	hash := sha256.Sum256(cert.Raw)
	fingerprint := fmt.Sprintf("SHA256:%s", hex.EncodeToString(hash[:]))

	// Save cert to disk
	if err := os.WriteFile(certPath, []byte(certResp.CertPEM), 0644); err != nil {
		return fmt.Errorf("write pinned cert: %w", err)
	}

	// Update config with fingerprint and save
	a.config.Server.ServerCertPin = fingerprint
	a.config.Server.CACertFile = certPath
	a.config.Server.TLSVerify = true
	if err := a.config.Save(a.configPath); err != nil {
		slog.Warn("failed to save config with cert pin", "error", err)
	}

	slog.Info("TOFU: pinned server certificate", "fingerprint", fingerprint, "path", certPath)
	return nil
}
