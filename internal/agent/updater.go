package agent

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/DesyncTheThird/rIOt/internal/models"
)

func (a *Agent) updateCheckLoop(ctx context.Context) {
	// Wait a bit before first check to let registration complete
	select {
	case <-ctx.Done():
		return
	case <-time.After(30 * time.Second):
	}

	a.checkAndUpdate(ctx)

	ticker := time.NewTicker(6 * time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			a.checkAndUpdate(ctx)
		}
	}
}

func (a *Agent) checkAndUpdate(ctx context.Context) {
	if a.version == "dev" {
		return // Don't auto-update dev builds
	}

	if a.config.Agent.AutoUpdate != nil && !*a.config.Agent.AutoUpdate {
		slog.Debug("auto-update disabled by config")
		return
	}

	goarm := ""
	if runtime.GOARCH == "arm" {
		goarm = goarmVersion()
	}

	resp, err := a.client.CheckForUpdate(ctx, a.version, runtime.GOOS, runtime.GOARCH, goarm)
	if err != nil {
		slog.Warn("update check failed", "error", err)
		return
	}

	if !resp.UpdateAvail {
		slog.Debug("agent is up to date", "version", a.version)
		return
	}

	slog.Info("update available", "current", a.version, "latest", resp.LatestVersion)
	a.reportUpdateEvent(ctx, models.EventAgentUpdateAvail, models.SeverityInfo,
		fmt.Sprintf("Agent update available: %s → %s", a.version, resp.LatestVersion))

	// Find the download URL for our platform
	suffix := platformSuffix(goarm)
	downloadURL, ok := resp.Assets[suffix]
	if !ok {
		slog.Warn("no binary available for this platform", "platform", suffix)
		return
	}

	a.reportUpdateEvent(ctx, models.EventAgentUpdateStarted, models.SeverityInfo,
		fmt.Sprintf("Agent update started: %s → %s", a.version, resp.LatestVersion))

	if err := a.performUpdate(ctx, downloadURL, resp.ChecksumURL, suffix); err != nil {
		slog.Error("update failed", "error", err)
		a.reportUpdateEvent(ctx, models.EventAgentUpdateFailed, models.SeverityWarning,
			fmt.Sprintf("Agent update failed: %v", err))
		return
	}

	a.reportUpdateEvent(ctx, models.EventAgentUpdateCompleted, models.SeverityInfo,
		fmt.Sprintf("Agent updated to %s", resp.LatestVersion))
	slog.Info("update applied, restarting")
	a.restartSelf()
}

// stagingPath is where the new binary is downloaded before being installed.
const stagingPath = "/var/lib/riot/riot-agent.update"

func (a *Agent) performUpdate(ctx context.Context, downloadURL, checksumURL, suffix string) error {
	// Download the new binary to the staging path (riot-owned directory).
	// We avoid /tmp because PrivateTmp gives us an isolated namespace,
	// and os.Rename fails across filesystem boundaries.
	stagingFile, err := os.OpenFile(stagingPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0755)
	if err != nil {
		return fmt.Errorf("create staging file: %w", err)
	}
	// Clean up staging on failure only — on success the systemd-run unit needs it.
	staged := false
	defer func() {
		if !staged {
			os.Remove(stagingPath)
		}
	}()

	slog.Info("downloading update", "url", downloadURL)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, downloadURL, nil)
	if err != nil {
		stagingFile.Close()
		return fmt.Errorf("create request: %w", err)
	}

	client := &http.Client{Timeout: 5 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		stagingFile.Close()
		return fmt.Errorf("download: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		stagingFile.Close()
		return fmt.Errorf("download failed: status %d", resp.StatusCode)
	}

	// Write and compute checksum simultaneously
	hash := sha256.New()
	writer := io.MultiWriter(stagingFile, hash)
	if _, err := io.Copy(writer, resp.Body); err != nil {
		stagingFile.Close()
		return fmt.Errorf("write binary: %w", err)
	}
	stagingFile.Close()
	actualSum := hex.EncodeToString(hash.Sum(nil))

	// Verify checksum if available
	if checksumURL != "" {
		expectedSum, err := fetchExpectedChecksum(ctx, checksumURL, suffix)
		if err != nil {
			slog.Warn("could not verify checksum", "error", err)
		} else if expectedSum != actualSum {
			return fmt.Errorf("checksum mismatch: expected %s, got %s", expectedSum, actualSum)
		} else {
			slog.Info("checksum verified")
		}
	}

	currentBinary, err := os.Executable()
	if err != nil {
		return fmt.Errorf("get executable path: %w", err)
	}

	// Strategy 1: systemd-run — schedule a transient unit that stops the
	// service, replaces the binary (not running → no EBUSY), then restarts.
	// Requires sudoers rule from install.sh v2.6.6+.
	_ = exec.Command("sudo", "systemctl", "reset-failed", "riot-agent-update").Run()
	script := fmt.Sprintf(
		"sleep 3 && systemctl stop riot-agent && cp -f %s %s && chmod 755 %s && rm -f %s && systemctl start riot-agent",
		stagingPath, currentBinary,
		currentBinary,
		stagingPath,
	)
	shCmd := exec.CommandContext(ctx, "sudo", "systemd-run",
		"--unit=riot-agent-update",
		"sh", "-c", script,
	)
	if out, err := shCmd.CombinedOutput(); err == nil {
		slog.Info("update scheduled via systemd-run")
		staged = true // keep staging file for the transient unit
		return nil
	} else {
		slog.Debug("systemd-run unavailable, trying direct replacement",
			"error", fmt.Sprintf("%s: %s", err, strings.TrimSpace(string(out))))
	}

	// Strategy 2: direct in-place replacement via mv + cp.
	// Works on most filesystems but fails with EBUSY on some.
	script2 := fmt.Sprintf(
		"mv -f %s %s.old && cp %s %s && chmod 755 %s && rm -f %s.old",
		currentBinary, currentBinary,
		stagingPath, currentBinary,
		currentBinary,
		currentBinary,
	)
	shCmd2 := exec.CommandContext(ctx, "sudo", "sh", "-c", script2)
	if out, err := shCmd2.CombinedOutput(); err == nil {
		slog.Info("binary replaced via direct copy")
		return nil
	} else {
		slog.Debug("direct replacement failed",
			"error", fmt.Sprintf("%s: %s", err, strings.TrimSpace(string(out))))
	}

	// Strategy 3: leave the staging file for the systemd ExecStartPre
	// to apply on next restart. Requires install.sh v2.6.6+ service unit.
	slog.Warn("could not replace binary directly; update staged for next restart — re-run install.sh to enable automatic updates")
	staged = true
	return nil
}

func (a *Agent) restartSelf() {
	slog.Info("exiting for restart")
	os.Exit(0)
}

func fetchExpectedChecksum(ctx context.Context, checksumURL, suffix string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, checksumURL, nil)
	if err != nil {
		return "", err
	}
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		// Format: "<hash>  <filename>"
		parts := strings.Fields(line)
		if len(parts) == 2 && strings.Contains(parts[1], suffix) {
			return parts[0], nil
		}
	}
	return "", fmt.Errorf("checksum not found for %s", suffix)
}

func platformSuffix(goarm string) string {
	if goarm != "" {
		return fmt.Sprintf("%s-armv%s", runtime.GOOS, goarm)
	}
	return fmt.Sprintf("%s-%s", runtime.GOOS, runtime.GOARCH)
}

func goarmVersion() string {
	// Default to v7 on ARM — the build matrix uses v6 and v7
	return "7"
}

// reportUpdateEvent sends an agent update event to the server (best-effort).
func (a *Agent) reportUpdateEvent(ctx context.Context, evtType models.EventType, severity models.EventSeverity, message string) {
	deviceID := a.config.Agent.DeviceID
	if deviceID == "" || a.client == nil {
		return
	}
	evt := &models.AgentEvent{
		Type:     evtType,
		Severity: severity,
		Message:  message,
	}
	if err := a.client.ReportEvent(ctx, deviceID, evt); err != nil {
		slog.Debug("failed to report update event", "type", evtType, "error", err)
	}
}

