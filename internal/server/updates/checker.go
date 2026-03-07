package updates

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

// GitHubRelease represents the relevant fields from the GitHub Releases API.
type GitHubRelease struct {
	TagName    string         `json:"tag_name"`
	Name       string         `json:"name"`
	Published  time.Time      `json:"published_at"`
	HTMLURL    string         `json:"html_url"`
	Assets     []GitHubAsset  `json:"assets"`
}

type GitHubAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Size               int64  `json:"size"`
}

// UpdateInfo is returned to agents and the dashboard.
type UpdateInfo struct {
	CurrentVersion string            `json:"current_version"`
	LatestVersion  string            `json:"latest_version"`
	UpdateAvail    bool              `json:"update_available"`
	ReleaseURL     string            `json:"release_url,omitempty"`
	PublishedAt    *time.Time        `json:"published_at,omitempty"`
	Assets         map[string]string `json:"assets,omitempty"`   // "linux-amd64" -> download URL
	ChecksumURL    string            `json:"checksum_url,omitempty"`
}

// Checker polls GitHub Releases for new versions.
type Checker struct {
	repo           string // "owner/repo"
	currentVersion string
	httpClient     *http.Client

	mu     sync.RWMutex
	latest *GitHubRelease
}

// NewChecker creates a new update checker for the given GitHub repo.
func NewChecker(repo, currentVersion string) *Checker {
	return &Checker{
		repo:           repo,
		currentVersion: currentVersion,
		httpClient:     &http.Client{Timeout: 15 * time.Second},
	}
}

// Start begins periodic polling for new releases.
func (c *Checker) Start(ctx context.Context) {
	// Check immediately on startup
	c.check()

	ticker := time.NewTicker(6 * time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.check()
		}
	}
}

func (c *Checker) check() {
	url := fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", c.repo)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		slog.Warn("update check: failed to create request", "error", err)
		return
	}
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	req.Header.Set("User-Agent", "rIOt-server/"+c.currentVersion)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		slog.Warn("update check: request failed", "error", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		slog.Warn("update check: unexpected status", "status", resp.StatusCode)
		return
	}

	var release GitHubRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		slog.Warn("update check: decode failed", "error", err)
		return
	}

	c.mu.Lock()
	c.latest = &release
	c.mu.Unlock()

	latestVer := normalizeVersion(release.TagName)
	if latestVer != c.currentVersion && c.currentVersion != "dev" {
		slog.Info("new version available", "current", c.currentVersion, "latest", latestVer)
	}
}

// AgentUpdateInfo returns update info for an agent with the given version, OS, and arch.
func (c *Checker) AgentUpdateInfo(agentVersion, goos, goarch, goarm string) *UpdateInfo {
	info := &UpdateInfo{
		CurrentVersion: agentVersion,
		LatestVersion:  c.currentVersion,
		UpdateAvail:    false,
	}

	c.mu.RLock()
	release := c.latest
	c.mu.RUnlock()

	if release == nil {
		return info
	}

	latestVer := normalizeVersion(release.TagName)
	info.LatestVersion = latestVer
	info.UpdateAvail = agentVersion != "dev" && isNewer(latestVer, agentVersion)
	info.ReleaseURL = release.HTMLURL
	info.PublishedAt = &release.Published

	if info.UpdateAvail {
		// Find matching asset
		suffix := assetSuffix(goos, goarch, goarm)
		info.Assets = make(map[string]string)
		for _, asset := range release.Assets {
			if strings.Contains(asset.Name, suffix) && !strings.HasSuffix(asset.Name, ".txt") {
				info.Assets[suffix] = asset.BrowserDownloadURL
			}
			if asset.Name == "checksums.txt" {
				info.ChecksumURL = asset.BrowserDownloadURL
			}
		}
	}

	return info
}

// ServerUpdateInfo returns update info for the server/dashboard.
func (c *Checker) ServerUpdateInfo() *UpdateInfo {
	info := &UpdateInfo{
		CurrentVersion: c.currentVersion,
		LatestVersion:  c.currentVersion,
		UpdateAvail:    false,
	}

	c.mu.RLock()
	release := c.latest
	c.mu.RUnlock()

	if release == nil {
		return info
	}

	latestVer := normalizeVersion(release.TagName)
	info.LatestVersion = latestVer
	info.UpdateAvail = c.currentVersion != "dev" && isNewer(latestVer, c.currentVersion)
	info.ReleaseURL = release.HTMLURL
	info.PublishedAt = &release.Published

	return info
}

func normalizeVersion(tag string) string {
	return strings.TrimPrefix(tag, "v")
}

// isNewer returns true if candidate is strictly greater than current (semver).
func isNewer(candidate, current string) bool {
	cp := parseSemver(candidate)
	cu := parseSemver(current)
	for i := 0; i < 3; i++ {
		if cp[i] != cu[i] {
			return cp[i] > cu[i]
		}
	}
	return false
}

func parseSemver(v string) [3]int {
	v = strings.TrimPrefix(v, "v")
	parts := strings.SplitN(v, ".", 3)
	var out [3]int
	for i := 0; i < len(parts) && i < 3; i++ {
		// Strip any pre-release suffix (e.g. "1-rc1")
		num := strings.SplitN(parts[i], "-", 2)[0]
		out[i], _ = strconv.Atoi(num)
	}
	return out
}

func assetSuffix(goos, goarch, goarm string) string {
	if goarm != "" && goarm != "0" {
		return fmt.Sprintf("%s-armv%s", goos, goarm)
	}
	return fmt.Sprintf("%s-%s", goos, goarch)
}

// CurrentArch returns the server's OS-arch for reference.
func CurrentArch() string {
	return runtime.GOOS + "-" + runtime.GOARCH
}
