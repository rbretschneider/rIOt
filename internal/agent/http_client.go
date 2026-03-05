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
	"os"
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

// NewHTTPClientWithTLS creates an HTTP client with custom TLS settings.
func NewHTTPClientWithTLS(serverCfg ServerConfig) *HTTPClient {
	transport := &http.Transport{}
	tlsCfg := &tls.Config{}

	if !serverCfg.TLSVerify {
		tlsCfg.InsecureSkipVerify = true
	}

	if serverCfg.CACertFile != "" {
		caCert, err := os.ReadFile(serverCfg.CACertFile)
		if err != nil {
			slog.Error("failed to read CA cert file", "path", serverCfg.CACertFile, "error", err)
		} else {
			pool := x509.NewCertPool()
			pool.AppendCertsFromPEM(caCert)
			tlsCfg.RootCAs = pool
			slog.Info("loaded custom CA certificate", "path", serverCfg.CACertFile)
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

func (c *HTTPClient) SendHeartbeat(ctx context.Context, deviceID string, hb *models.Heartbeat) error {
	body, _ := json.Marshal(hb)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		fmt.Sprintf("%s/api/v1/devices/%s/heartbeat", c.baseURL, deviceID), bytes.NewReader(body))
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
		return fmt.Errorf("heartbeat failed: %s", string(b))
	}
	return nil
}

// UpdateCheckResponse mirrors the server's update info response.
type UpdateCheckResponse struct {
	CurrentVersion string            `json:"current_version"`
	LatestVersion  string            `json:"latest_version"`
	UpdateAvail    bool              `json:"update_available"`
	ReleaseURL     string            `json:"release_url,omitempty"`
	Assets         map[string]string `json:"assets,omitempty"`
	ChecksumURL    string            `json:"checksum_url,omitempty"`
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

	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("telemetry push failed: %s", string(b))
	}
	return nil
}
