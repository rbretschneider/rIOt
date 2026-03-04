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

	// Find the download URL for our platform
	suffix := platformSuffix(goarm)
	downloadURL, ok := resp.Assets[suffix]
	if !ok {
		slog.Warn("no binary available for this platform", "platform", suffix)
		return
	}

	if err := a.performUpdate(ctx, downloadURL, resp.ChecksumURL, suffix); err != nil {
		slog.Error("update failed", "error", err)
		return
	}

	slog.Info("update applied successfully, restarting")
	a.restartSelf()
}

func (a *Agent) performUpdate(ctx context.Context, downloadURL, checksumURL, suffix string) error {
	// Download the new binary to a temp file
	tmpFile, err := os.CreateTemp("", "riot-agent-update-*")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath) // Clean up on failure

	slog.Info("downloading update", "url", downloadURL)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, downloadURL, nil)
	if err != nil {
		tmpFile.Close()
		return fmt.Errorf("create request: %w", err)
	}

	client := &http.Client{Timeout: 5 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		tmpFile.Close()
		return fmt.Errorf("download: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		tmpFile.Close()
		return fmt.Errorf("download failed: status %d", resp.StatusCode)
	}

	// Write and compute checksum simultaneously
	hash := sha256.New()
	writer := io.MultiWriter(tmpFile, hash)
	if _, err := io.Copy(writer, resp.Body); err != nil {
		tmpFile.Close()
		return fmt.Errorf("write binary: %w", err)
	}
	tmpFile.Close()
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

	// Make executable
	if err := os.Chmod(tmpPath, 0755); err != nil {
		return fmt.Errorf("chmod: %w", err)
	}

	// Replace current binary
	currentBinary, err := os.Executable()
	if err != nil {
		return fmt.Errorf("get executable path: %w", err)
	}

	// On Linux, we can rename over the running binary
	if err := os.Rename(tmpPath, currentBinary); err != nil {
		// Rename across filesystems doesn't work, fall back to copy
		if err := copyFile(tmpPath, currentBinary); err != nil {
			return fmt.Errorf("replace binary: %w", err)
		}
	}

	return nil
}

func (a *Agent) restartSelf() {
	// Try systemd restart first
	if runtime.GOOS == "linux" {
		cmd := exec.Command("systemctl", "restart", "riot-agent")
		if err := cmd.Run(); err == nil {
			return
		}
	}

	// Fallback: exit with code 0 and let the service manager restart us
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

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0755)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	return err
}
