package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/DesyncTheThird/rIOt/internal/models"
)

// handleRunDeviceProbe executes a single device probe on demand.
func (a *Agent) handleRunDeviceProbe(ctx context.Context, payload models.CommandPayload) (string, string) {
	if !a.config.Commands.AllowProbes {
		return "error", "device probes not allowed by agent config (set commands.allow_probes: true)"
	}

	// Extract probe parameters from payload
	probeIDFloat, _ := payload.Params["probe_id"].(float64)
	probeID := int64(probeIDFloat)
	probeType, _ := payload.Params["type"].(string)
	configRaw, _ := payload.Params["config"].(map[string]interface{})
	timeoutFloat, _ := payload.Params["timeout_seconds"].(float64)
	timeout := int(timeoutFloat)
	if timeout <= 0 {
		timeout = 10
	}

	// Parse assertions
	var assertions []models.ProbeAssertion
	if assertRaw, ok := payload.Params["assertions"]; ok {
		assertJSON, _ := json.Marshal(assertRaw)
		json.Unmarshal(assertJSON, &assertions)
	}

	if probeType == "" {
		return "error", "probe type is required"
	}
	if configRaw == nil {
		configRaw = make(map[string]interface{})
	}

	probeCtx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()

	start := time.Now()
	output, execErr := a.executeProbe(probeCtx, probeType, configRaw)
	latencyMs := float64(time.Since(start).Microseconds()) / 1000.0

	// Check assertions
	failedAssertions := checkAssertions(assertions, output)

	success := execErr == nil && len(failedAssertions) == 0

	errMsg := ""
	if execErr != nil {
		errMsg = execErr.Error()
	}

	result := models.DeviceProbeResult{
		ProbeID:          probeID,
		DeviceID:         a.config.Agent.DeviceID,
		Success:          success,
		LatencyMs:        latencyMs,
		Output:           output,
		FailedAssertions: failedAssertions,
		ErrorMsg:         errMsg,
	}

	// Report results back to server
	go func() {
		reportCtx, reportCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer reportCancel()
		if err := a.sendDeviceProbeResults(reportCtx, []models.DeviceProbeResult{result}); err != nil {
			slog.Error("failed to send device probe results", "probe_id", probeID, "error", err)
		}
	}()

	if success {
		return "success", fmt.Sprintf("probe %d completed successfully (%.1fms)", probeID, latencyMs)
	}
	return "success", fmt.Sprintf("probe %d completed with failures (%.1fms): %s", probeID, latencyMs, errMsg)
}

// executeProbe dispatches to the appropriate executor based on probe type.
func (a *Agent) executeProbe(ctx context.Context, probeType string, config map[string]interface{}) (map[string]interface{}, error) {
	switch probeType {
	case "shell":
		return a.executeShellProbe(ctx, config)
	case "http":
		return a.executeHTTPProbe(ctx, config)
	case "port":
		return a.executePortProbe(ctx, config)
	case "file":
		return a.executeFileProbe(ctx, config)
	case "container_exec":
		return a.executeContainerExecProbe(ctx, config)
	default:
		return nil, fmt.Errorf("unsupported probe type: %s", probeType)
	}
}

// executeShellProbe runs a shell command and captures exit_code, stdout, stderr.
func (a *Agent) executeShellProbe(ctx context.Context, config map[string]interface{}) (map[string]interface{}, error) {
	command, _ := config["command"].(string)
	if command == "" {
		return nil, fmt.Errorf("shell probe requires 'command' in config")
	}
	shell, _ := config["shell"].(string)
	if shell == "" {
		shell = "/bin/sh"
	}

	cmd := exec.CommandContext(ctx, shell, "-c", command)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			return map[string]interface{}{
				"exit_code": -1,
				"stdout":    "",
				"stderr":    err.Error(),
			}, err
		}
	}

	return map[string]interface{}{
		"exit_code": exitCode,
		"stdout":    truncateStr(stdout.String(), 4000),
		"stderr":    truncateStr(stderr.String(), 4000),
	}, nil
}

// executeHTTPProbe makes an HTTP request and captures status_code, body, latency_ms.
func (a *Agent) executeHTTPProbe(ctx context.Context, config map[string]interface{}) (map[string]interface{}, error) {
	url, _ := config["url"].(string)
	if url == "" {
		return nil, fmt.Errorf("http probe requires 'url' in config")
	}
	method, _ := config["method"].(string)
	if method == "" {
		method = "GET"
	}

	var bodyReader io.Reader
	if bodyStr, ok := config["body"].(string); ok && bodyStr != "" {
		bodyReader = strings.NewReader(bodyStr)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	// Add headers
	if headers, ok := config["headers"].(map[string]interface{}); ok {
		for k, v := range headers {
			if vs, ok := v.(string); ok {
				req.Header.Set(k, vs)
			}
		}
	}

	client := &http.Client{Timeout: 30 * time.Second}
	start := time.Now()
	resp, err := client.Do(req)
	latency := float64(time.Since(start).Microseconds()) / 1000.0
	if err != nil {
		return map[string]interface{}{
			"status_code": 0,
			"body":        "",
			"latency_ms":  latency,
		}, err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))

	return map[string]interface{}{
		"status_code": resp.StatusCode,
		"body":        truncateStr(string(respBody), 4000),
		"latency_ms":  latency,
	}, nil
}

// executePortProbe TCP dials host:port and captures connected (bool), latency_ms.
func (a *Agent) executePortProbe(ctx context.Context, config map[string]interface{}) (map[string]interface{}, error) {
	host, _ := config["host"].(string)
	if host == "" {
		host = "localhost"
	}
	portFloat, _ := config["port"].(float64)
	port := int(portFloat)
	if port == 0 {
		return nil, fmt.Errorf("port probe requires 'port' in config")
	}

	addr := fmt.Sprintf("%s:%d", host, port)
	start := time.Now()

	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", addr)
	latency := float64(time.Since(start).Microseconds()) / 1000.0

	if err != nil {
		return map[string]interface{}{
			"connected":  false,
			"latency_ms": latency,
		}, nil // Not an execution error, just a failed check
	}
	conn.Close()

	return map[string]interface{}{
		"connected":  true,
		"latency_ms": latency,
	}, nil
}

// executeFileProbe stats/reads a file path and captures exists, size, content.
func (a *Agent) executeFileProbe(ctx context.Context, config map[string]interface{}) (map[string]interface{}, error) {
	path, _ := config["path"].(string)
	if path == "" {
		return nil, fmt.Errorf("file probe requires 'path' in config")
	}

	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]interface{}{
				"exists":  false,
				"size":    0,
				"content": "",
			}, nil
		}
		return nil, err
	}

	output := map[string]interface{}{
		"exists": true,
		"size":   info.Size(),
	}

	// Only read content if requested and file is not too large
	if readContent, ok := config["read_content"].(bool); ok && readContent {
		if info.Size() <= 65536 { // 64KB limit
			data, err := os.ReadFile(path)
			if err == nil {
				output["content"] = truncateStr(string(data), 4000)
			}
		}
	}

	return output, nil
}

// executeContainerExecProbe runs docker exec and captures exit_code, stdout, stderr.
func (a *Agent) executeContainerExecProbe(ctx context.Context, config map[string]interface{}) (map[string]interface{}, error) {
	containerID, _ := config["container_id"].(string)
	if containerID == "" {
		containerID, _ = config["container_name"].(string)
	}
	if containerID == "" {
		return nil, fmt.Errorf("container_exec probe requires 'container_id' or 'container_name' in config")
	}
	command, _ := config["command"].(string)
	if command == "" {
		return nil, fmt.Errorf("container_exec probe requires 'command' in config")
	}

	// Use docker CLI for simplicity and compatibility
	args := []string{"exec", containerID, "sh", "-c", command}
	cmd := exec.CommandContext(ctx, "docker", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			return map[string]interface{}{
				"exit_code": -1,
				"stdout":    "",
				"stderr":    err.Error(),
			}, err
		}
	}

	return map[string]interface{}{
		"exit_code": exitCode,
		"stdout":    truncateStr(stdout.String(), 4000),
		"stderr":    truncateStr(stderr.String(), 4000),
	}, nil
}

// checkAssertions evaluates ProbeAssertions against probe output.
func checkAssertions(assertions []models.ProbeAssertion, output map[string]interface{}) []models.ProbeAssertion {
	var failed []models.ProbeAssertion
	for _, a := range assertions {
		actual, ok := output[a.Field]
		if !ok {
			failed = append(failed, a)
			continue
		}
		if !evaluateAssertion(actual, a.Operator, a.Value) {
			failed = append(failed, a)
		}
	}
	return failed
}

// evaluateAssertion checks a single assertion.
func evaluateAssertion(actual interface{}, operator, expected string) bool {
	actualStr := fmt.Sprintf("%v", actual)

	switch operator {
	case "eq":
		return actualStr == expected
	case "ne":
		return actualStr != expected
	case "contains":
		return strings.Contains(actualStr, expected)
	case "regex":
		re, err := regexp.Compile(expected)
		if err != nil {
			return false
		}
		return re.MatchString(actualStr)
	case "gt":
		av, ae := toFloat(actual)
		ev, ee := strconv.ParseFloat(expected, 64)
		if ae != nil || ee != nil {
			return false
		}
		return av > ev
	case "lt":
		av, ae := toFloat(actual)
		ev, ee := strconv.ParseFloat(expected, 64)
		if ae != nil || ee != nil {
			return false
		}
		return av < ev
	default:
		return false
	}
}

// toFloat converts an interface to float64.
func toFloat(v interface{}) (float64, error) {
	switch val := v.(type) {
	case float64:
		return val, nil
	case float32:
		return float64(val), nil
	case int:
		return float64(val), nil
	case int64:
		return float64(val), nil
	case string:
		return strconv.ParseFloat(val, 64)
	case bool:
		if val {
			return 1, nil
		}
		return 0, nil
	default:
		return strconv.ParseFloat(fmt.Sprintf("%v", v), 64)
	}
}

// truncateStr returns the first maxLen characters of s.
func truncateStr(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "...(truncated)"
}

// sendDeviceProbeResults sends probe results back to the server.
func (a *Agent) sendDeviceProbeResults(ctx context.Context, results []models.DeviceProbeResult) error {
	body, _ := json.Marshal(results)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		fmt.Sprintf("%s/api/v1/devices/%s/probe-results", a.client.baseURL, a.config.Agent.DeviceID),
		bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-rIOt-Key", a.client.apiKey)

	resp, err := a.client.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("probe results push failed: %s", string(b))
	}
	return nil
}
