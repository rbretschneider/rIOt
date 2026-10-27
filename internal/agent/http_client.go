package agent

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/DesyncTheThird/rIOt/internal/models"
)

type HTTPClient struct {
	baseURL string
	apiKey  string
	client  *http.Client

	// now is the injected clock for the telemetry back-off; nil = time.Now.
	now func() time.Time

	// Telemetry back-off (GROWTH-001 AD-017). It is client-wide for telemetry
	// only: heartbeats, events and command reports are never gated by it, so a
	// throttled telemetry route cannot silence liveness.
	backoffMu    sync.Mutex
	backoffUntil time.Time
	backoffStep  time.Duration
}

// Telemetry back-off bounds (GROWTH-001 AD-017, 429 handling).
const (
	retryAfterMin     = time.Second
	retryAfterMax     = time.Hour
	backoffInitial    = 30 * time.Second
	backoffCap        = 10 * time.Minute
	maxErrorBodyBytes = 4 << 10
)

// ErrBackoff is returned by SendTelemetry, without touching the network, while
// a 429, 401, 403 (or a 5xx with Retry-After) back-off is in force. The caller
// keeps the item; this is not a rejection.
var ErrBackoff = errors.New("telemetry back-off in force")

// StatusError is a non-success HTTP answer. RetryAfter is zero when the server
// sent no usable Retry-After.
type StatusError struct {
	Op         string
	StatusCode int
	RetryAfter time.Duration
	Body       string
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("%s failed: status %d: %s", e.Op, e.StatusCode, e.Body)
}

func (c *HTTPClient) clockNow() time.Time {
	if c.now != nil {
		return c.now()
	}
	return time.Now()
}

// newStatusError reads a bounded part of the error body and the Retry-After
// header (seconds or an HTTP date).
func (c *HTTPClient) newStatusError(op string, resp *http.Response) *StatusError {
	b, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBodyBytes))
	return &StatusError{
		Op:         op,
		StatusCode: resp.StatusCode,
		RetryAfter: parseRetryAfter(resp.Header.Get("Retry-After"), c.clockNow()),
		Body:       strings.TrimSpace(string(b)),
	}
}

// parseRetryAfter returns 0 for an absent, malformed or non-positive value.
func parseRetryAfter(v string, now time.Time) time.Duration {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0
	}
	if secs, err := strconv.ParseInt(v, 10, 64); err == nil {
		if secs <= 0 {
			return 0
		}
		if secs > int64(retryAfterMax/time.Second) {
			return retryAfterMax + 1 // caller clamps; avoids Duration overflow
		}
		return time.Duration(secs) * time.Second
	}
	if t, err := http.ParseTime(v); err == nil {
		if d := t.Sub(now); d > 0 {
			return d
		}
	}
	return 0
}

// telemetryBlocked reports whether a back-off is in force.
func (c *HTTPClient) telemetryBlocked() (time.Time, bool) {
	c.backoffMu.Lock()
	defer c.backoffMu.Unlock()
	if until := c.backoffUntil; !until.IsZero() && c.clockNow().Before(until) {
		return until, true
	}
	return time.Time{}, false
}

// noteTelemetryOutcome updates the back-off after a telemetry answer.
func (c *HTTPClient) noteTelemetryOutcome(err *StatusError) {
	c.backoffMu.Lock()
	defer c.backoffMu.Unlock()
	if err == nil {
		c.backoffUntil = time.Time{}
		c.backoffStep = 0
		return
	}
	now := c.clockNow()
	switch {
	case err.StatusCode == http.StatusTooManyRequests,
		err.StatusCode == http.StatusUnauthorized,
		err.StatusCode == http.StatusForbidden:
		if err.RetryAfter > 0 {
			c.backoffUntil = now.Add(min(max(err.RetryAfter, retryAfterMin), retryAfterMax))
			return
		}
		if c.backoffStep == 0 {
			c.backoffStep = backoffInitial
		} else {
			c.backoffStep = min(c.backoffStep*2, backoffCap)
		}
		c.backoffUntil = now.Add(c.backoffStep)
	case err.StatusCode >= 500 && err.RetryAfter > 0:
		// A 5xx honours Retry-After when present; otherwise the next tick retries.
		c.backoffUntil = now.Add(min(max(err.RetryAfter, retryAfterMin), retryAfterMax))
	}
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
		// EXPECT-007 AD-010: a *StatusError lets the update-event sender tell a
		// retryable answer (5xx, 429) from a final one. The body read is bounded.
		return c.newStatusError("event report", resp)
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

// SendTelemetry pushes one snapshot. A non-success answer is a *StatusError. While
// a back-off is in force it returns ErrBackoff without sending anything.
func (c *HTTPClient) SendTelemetry(ctx context.Context, deviceID string, snap *models.TelemetrySnapshot) error {
	if until, blocked := c.telemetryBlocked(); blocked {
		return fmt.Errorf("%w until %s", ErrBackoff, until.UTC().Format(time.RFC3339))
	}
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
		statusErr := c.newStatusError("telemetry push", resp)
		c.noteTelemetryOutcome(statusErr)
		return statusErr
	}
	c.noteTelemetryOutcome(nil)
	return nil
}
