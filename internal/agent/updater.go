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
	defer os.Remove(stagingPath) // Clean up on failure

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

	// Replace the running binary atomically using sudo install.
	// The riot user can't write to /usr/local/bin/ directly, and opening
	// a running binary for writing gives ETXTBSY. `install` creates a new
	// inode (temp + rename), so the running process keeps its old inode
	// until it exits. The sudoers drop-in whitelists this exact command.
	currentBinary, err := os.Executable()
	if err != nil {
		return fmt.Errorf("get executable path: %w", err)
	}

	installCmd := exec.CommandContext(ctx, "sudo", "install", "-m", "755",
		stagingPath, currentBinary)
	if out, err := installCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("install binary: %s: %s", err, strings.TrimSpace(string(out)))
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

