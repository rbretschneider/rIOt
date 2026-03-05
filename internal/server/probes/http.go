package probes

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/DesyncTheThird/rIOt/internal/models"
)

// executeHTTP runs an HTTP probe.
// Config: url, method (default GET), expected_status (default 200), body (optional),
// headers (optional map), tls_verify (default true).
func executeHTTP(ctx context.Context, probe models.Probe) *models.ProbeResult {
	url, _ := probe.Config["url"].(string)
	if url == "" {
		return &models.ProbeResult{
			ProbeID:  probe.ID,
			Success:  false,
			ErrorMsg: "url not configured",
			Metadata: make(map[string]interface{}),
		}
	}

	method := "GET"
	if m, ok := probe.Config["method"].(string); ok && m != "" {
		method = strings.ToUpper(m)
	}

	expectedStatus := 200
	if s, ok := probe.Config["expected_status"].(float64); ok && s > 0 {
		expectedStatus = int(s)
	}

	tlsVerify := true
	if v, ok := probe.Config["tls_verify"].(bool); ok {
		tlsVerify = v
	}

	transport := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: !tlsVerify},
	}
	client := &http.Client{
		Transport: transport,
		Timeout:   time.Duration(probe.TimeoutSeconds) * time.Second,
	}
	if client.Timeout == 0 {
		client.Timeout = 10 * time.Second
	}

	var bodyReader io.Reader
	if body, ok := probe.Config["body"].(string); ok && body != "" {
		bodyReader = strings.NewReader(body)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, bodyReader)
	if err != nil {
		return &models.ProbeResult{
			ProbeID:  probe.ID,
			Success:  false,
			ErrorMsg: err.Error(),
			Metadata: make(map[string]interface{}),
		}
	}

	if headers, ok := probe.Config["headers"].(map[string]interface{}); ok {
		for k, v := range headers {
			if s, ok := v.(string); ok {
				req.Header.Set(k, s)
			}
		}
	}

	start := time.Now()
	resp, err := client.Do(req)
	latency := time.Since(start)

	if err != nil {
		return &models.ProbeResult{
			ProbeID:   probe.ID,
			Success:   false,
			LatencyMs: float64(latency) / float64(time.Millisecond),
			ErrorMsg:  err.Error(),
			Metadata:  make(map[string]interface{}),
		}
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)

	statusCode := resp.StatusCode
	success := statusCode == expectedStatus

	metadata := map[string]interface{}{
		"status_code": statusCode,
		"latency_ms":  float64(latency) / float64(time.Millisecond),
	}

	// Check TLS certificate expiry
	if resp.TLS != nil && len(resp.TLS.PeerCertificates) > 0 {
		cert := resp.TLS.PeerCertificates[0]
		daysUntilExpiry := time.Until(cert.NotAfter).Hours() / 24
		metadata["tls_expiry_days"] = daysUntilExpiry
		metadata["tls_issuer"] = cert.Issuer.CommonName
		metadata["tls_subject"] = cert.Subject.CommonName
	}

	errorMsg := ""
	if !success {
		errorMsg = fmt.Sprintf("expected status %d, got %d", expectedStatus, statusCode)
	}

	return &models.ProbeResult{
		ProbeID:    probe.ID,
		Success:    success,
		LatencyMs:  float64(latency) / float64(time.Millisecond),
		StatusCode: &statusCode,
		ErrorMsg:   errorMsg,
		Metadata:   metadata,
	}
}
