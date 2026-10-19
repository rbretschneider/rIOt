package agent

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"time"

	"github.com/DesyncTheThird/rIOt/internal/models"
)

type HTTPClient struct {
	baseURL string
	apiKey  string
	client  *http.Client
}

func NewHTTPClient(baseURL, apiKey string) *HTTPClient {
	return &HTTPClient{
		baseURL: baseURL,
		apiKey:  apiKey,
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// buildCertPool loads all trusted CA/server certificates into a single pool.
// This includes the TOFU-pinned server cert and the mTLS CA cert (if present).
func buildCertPool(serverCfg ServerConfig, logLoads bool) *x509.CertPool {
	pool := x509.NewCertPool()
	loaded := 0

	// Load TOFU-pinned server certificate (self-signed TLS cert)
	serverCertPath := ServerCertPath()
	if data, err := os.ReadFile(serverCertPath); err == nil {
		if pool.AppendCertsFromPEM(data) {
			loaded++
			if logLoads {
				slog.Info("loaded TOFU-pinned server certificate", "path", serverCertPath)
			}
		}
	}

	// Load mTLS CA certificate
	if serverCfg.CACertFile != "" {
		if data, err := os.ReadFile(serverCfg.CACertFile); err == nil {
			if pool.AppendCertsFromPEM(data) {
				loaded++
				if logLoads {
					slog.Info("loaded mTLS CA certificate", "path", serverCfg.CACertFile)
				}
			}
		} else {
			slog.Error("failed to read CA cert file", "path", serverCfg.CACertFile, "error", err)
		}
	}

	if loaded > 0 {
		return pool
	}
	return nil
}

// NewHTTPClientWithTLS creates an HTTP client with custom TLS settings.
func NewHTTPClientWithTLS(serverCfg ServerConfig) *HTTPClient {
	transport := &http.Transport{}
	tlsCfg := &tls.Config{}

	if !serverCfg.TLSVerify {
		tlsCfg.InsecureSkipVerify = true
	}

	if pool := buildCertPool(serverCfg, true); pool != nil {
		tlsCfg.RootCAs = pool
	}

	// Load mTLS client certificate if configured
	if serverCfg.ClientCert != "" && serverCfg.ClientKey != "" {
		cert, err := tls.LoadX509KeyPair(serverCfg.ClientCert, serverCfg.ClientKey)
		if err != nil {
			slog.Error("failed to load mTLS client certificate", "error", err)
		} else {
			tlsCfg.Certificates = []tls.Certificate{cert}
			slog.Info("loaded mTLS client certificate", "cert", serverCfg.ClientCert)
		}
	}

	transport.TLSClientConfig = tlsCfg

	return &HTTPClient{
		baseURL: serverCfg.URL,
		apiKey:  serverCfg.APIKey,
		client: &http.Client{
			Timeout:   10 * time.Second,
			Transport: transport,
		},
	}
}

// TLSConfigFromServerConfig returns the TLS configuration for the current server config.
// Used by the WebSocket client to share TLS settings.
func TLSConfigFromServerConfig(serverCfg ServerConfig) *tls.Config {
	tlsCfg := &tls.Config{}

	if !serverCfg.TLSVerify {
		tlsCfg.InsecureSkipVerify = true
	}

	if pool := buildCertPool(serverCfg, false); pool != nil {
		tlsCfg.RootCAs = pool
	}

	if serverCfg.ClientCert != "" && serverCfg.ClientKey != "" {
		cert, err := tls.LoadX509KeyPair(serverCfg.ClientCert, serverCfg.ClientKey)
		if err == nil {
			tlsCfg.Certificates = []tls.Certificate{cert}
		}
	}

	return tlsCfg
}

func (c *HTTPClient) Register(ctx context.Context, reg *models.DeviceRegistration) (*models.DeviceRegistrationResponse, error) {
	body, _ := json.Marshal(reg)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.baseURL+"/api/v1/devices/register", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-rIOt-Key", c.apiKey)

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("registration failed: %s", string(b))
	}

	var result models.DeviceRegistrationResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return &result, nil
}

// HeartbeatResponse is the server's response to a heartbeat, potentially including queued commands.
type HeartbeatResponse struct {
	Status          string                  `json:"status"`
	PendingCommands []models.CommandPayload `json:"pending_commands,omitempty"`
}

func (c *HTTPClient) SendHeartbeat(ctx context.Context, deviceID string, hb *models.Heartbeat) (*HeartbeatResponse, error) {
	body, _ := json.Marshal(hb)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		fmt.Sprintf("%s/api/v1/devices/%s/heartbeat", c.baseURL, deviceID), bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-rIOt-Key", c.apiKey)

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("heartbeat failed: %s", string(b))
	}

	var hbResp HeartbeatResponse
	if err := json.NewDecoder(resp.Body).Decode(&hbResp); err != nil {
		// Non-fatal: old servers may return a different format
		return &HeartbeatResponse{Status: "ok"}, nil
	}
	return &hbResp, nil
}

// UpdateCheckResponse mirrors the server's update info response.
type UpdateCheckResponse struct {
	CurrentVersion string            `json:"current_version"`
	LatestVersion  string            `json:"latest_version"`
	UpdateAvail    bool              `json:"update_available"`
	ReleaseURL     string            `json:"release_url,omitempty"`
	Assets         map[string]string `json:"assets,omitempty"`
	ChecksumURL    string            `json:"checksum_url,omitempty"`
	// SignatureURL locates checksums.txt.sig. It is a locator only: authenticity
	// is decided solely by the compiled-in release public key.
	SignatureURL string `json:"signature_url,omitempty"`
}

func (c *HTTPClient) CheckForUpdate(ctx context.Context, version, goos, goarch, goarm string) (*UpdateCheckResponse, error) {
	url := fmt.Sprintf("%s/api/v1/update/check?version=%s&os=%s&arch=%s", c.baseURL, version, goos, goarch)
	if goarm != "" {
		url += "&arm=" + goarm
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-rIOt-Key", c.apiKey)

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("update check failed: status %d", resp.StatusCode)
	}

	var result UpdateCheckResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (c *HTTPClient) SendDockerEvent(ctx context.Context, deviceID string, evt *models.DockerEvent) error {
	body, _ := json.Marshal(evt)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		fmt.Sprintf("%s/api/v1/devices/%s/docker-events", c.baseURL, deviceID), bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-rIOt-Key", c.apiKey)

	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("docker event push failed: %s", string(b))
	}
	return nil
}

// ReportEvent sends an agent-generated event to the server.
func (c *HTTPClient) ReportEvent(ctx context.Context, deviceID string, evt *models.AgentEvent) error {
	body, _ := json.Marshal(evt)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		fmt.Sprintf("%s/api/v1/devices/%s/events", c.baseURL, deviceID), bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-rIOt-Key", c.apiKey)

	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("event report failed: %s", string(b))
	}
	return nil
}

// SendDeviceLogs pushes log entries directly to the server (used by fetch_logs command).
func (c *HTTPClient) SendDeviceLogs(ctx context.Context, deviceID string, entries []models.LogEntry) error {
	body, _ := json.Marshal(entries)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		fmt.Sprintf("%s/api/v1/devices/%s/logs", c.baseURL, deviceID), bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-rIOt-Key", c.apiKey)

	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("device logs push failed: %s", string(b))
	}
	return nil
}

// maxReportReplyBytes caps how much of a report response the agent reads.
const maxReportReplyBytes = 1 << 20

// ReportReply is the server's answer to a command report. Response is the
// decoded JSON body when there was one; it is zero for a legacy server's
// plain-text 404.
type ReportReply struct {
	StatusCode int
	Response   models.ReportResponse
}

// PostCommandReport sends an ack, progress or final-result report for one
// command over HTTP (AD-010, §6.3). Both IDs are path-escaped so a hostile
// command ID cannot re-target the request (SEC C-7). The returned error covers
// transport failures only; HTTP statuses are reported in the reply so the
// caller can apply the outbox delivery table.
func (c *HTTPClient) PostCommandReport(ctx context.Context, deviceID, commandID string, rep models.CommandReport) (*ReportReply, error) {
	body, err := json.Marshal(rep)
	if err != nil {
		return nil, fmt.Errorf("encode command report: %w", err)
	}
	endpoint := fmt.Sprintf("%s/api/v1/devices/%s/commands/%s/report",
		c.baseURL, url.PathEscape(deviceID), url.PathEscape(commandID))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-rIOt-Key", c.apiKey)

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	reply := &ReportReply{StatusCode: resp.StatusCode}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxReportReplyBytes))
	if err != nil {
		return nil, fmt.Errorf("read command report reply: %w", err)
	}
	// A body that is not the expected JSON (a legacy server's 404 page, a
	// captive portal) leaves Response zero: the outbox treats a 2xx that does
	// not echo the command ID as undelivered.
	if err := json.Unmarshal(raw, &reply.Response); err != nil {
		reply.Response = models.ReportResponse{}
	}
	return reply, nil
}

func (c *HTTPClient) SendTelemetry(ctx context.Context, deviceID string, snap *models.TelemetrySnapshot) error {
	body, _ := json.Marshal(snap)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		fmt.Sprintf("%s/api/v1/devices/%s/telemetry", c.baseURL, deviceID), bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-rIOt-Key", c.apiKey)

	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		retry := time.Minute
		if seconds, e := strconv.Atoi(resp.Header.Get("Retry-After")); e == nil && seconds > 0 {
			retry = time.Duration(seconds) * time.Second
		} else if at, e := http.ParseTime(resp.Header.Get("Retry-After")); e == nil {
			retry = time.Until(at)
		}
		retry = min(max(retry, time.Second), time.Hour)
		return &telemetryHTTPError{Status: resp.StatusCode, RetryAfter: retry}
	}
	return nil
}

type telemetryHTTPError struct {
	Status     int
	RetryAfter time.Duration
}

func (e *telemetryHTTPError) Error() string { return fmt.Sprintf("telemetry HTTP status %d", e.Status) }
