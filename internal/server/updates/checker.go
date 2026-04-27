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

// defaultStartupBackoff governs how aggressively Start() retries the first
// release-feed fetch when the server has just booted. A single transient
// failure (e.g. Docker DNS not yet warm at container start) must not pin the
// server in a "no release info" state for the full 6h steady-state interval,
// since the fleet outdated badge and agent_update command both depend on
// c.latest being populated.
var defaultStartupBackoff = []time.Duration{
	1 * time.Minute,
	5 * time.Minute,
	15 * time.Minute,
	30 * time.Minute,
	1 * time.Hour,
}

// Checker polls GitHub Releases for new versions.
type Checker struct {
	repo           string // "owner/repo"
	currentVersion string
	httpClient     *http.Client

	// startupBackoff is the schedule used by Start() to retry the first
	// successful fetch. Exposed for tests; defaults to defaultStartupBackoff.
	startupBackoff []time.Duration

	mu     sync.RWMutex
	latest *GitHubRelease
}

// NewChecker creates a new update checker for the given GitHub repo.
func NewChecker(repo, currentVersion string) *Checker {
	return &Checker{
		repo:           repo,
		currentVersion: currentVersion,
		httpClient:     &http.Client{Timeout: 15 * time.Second},
		startupBackoff: defaultStartupBackoff,
	}
}

// Start begins periodic polling for new releases. On startup it retries with
// backoff until the first successful fetch, then falls back to the 6h cadence.
func (c *Checker) Start(ctx context.Context) {
	awaitFirstSuccess(ctx, c.check, c.startupBackoff)

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

// awaitFirstSuccess invokes fn until it returns true or ctx is canceled,
// waiting between attempts according to the backoff schedule. Once the
// schedule is exhausted, retries continue at the final interval.
func awaitFirstSuccess(ctx context.Context, fn func() bool, backoff []time.Duration) {
	if fn() {
		return
	}
	for i := 0; ; i++ {
		wait := backoff[len(backoff)-1]
		if i < len(backoff) {
			wait = backoff[i]
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
		if fn() {
			return
		}
	}
}

// check fetches the latest release. Returns true on success (c.latest is now
// populated), false on any failure path. The bool drives Start()'s retry loop —
// silently failing without a signal would leave the server in the broken state
// it just spent code complexity preventing.
func (c *Checker) check() bool {
	url := fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", c.repo)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		slog.Warn("update check: failed to create request", "error", err.Error())
		return false
	}
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	req.Header.Set("User-Agent", "rIOt-server/"+c.currentVersion)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		slog.Warn("update check: request failed", "error", err.Error())
		return false
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		slog.Warn("update check: unexpected status", "status", resp.StatusCode)
		return false
	}

	var release GitHubRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		slog.Warn("update check: decode failed", "error", err.Error())
		return false
	}

	c.mu.Lock()
	c.latest = &release
	c.mu.Unlock()

	latestVer := normalizeVersion(release.TagName)
	if latestVer != c.currentVersion && c.currentVersion != "dev" {
		slog.Info("new version available", "current", c.currentVersion, "latest", latestVer)
	}
	return true
}

// AgentUpdateInfo returns update info for an agent with the given version, OS, and arch.
// LatestVersion is left empty when the server has not yet successfully reached the
// release feed — callers must treat empty LatestVersion as "unknown", not as
// "agent is current," to avoid masquerading an unreachable feed as a successful update.
func (c *Checker) AgentUpdateInfo(agentVersion, goos, goarch, goarm string) *UpdateInfo {
	info := &UpdateInfo{
		CurrentVersion: agentVersion,
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
// LatestVersion is left empty when the release feed has not yet been reached
// successfully. Callers must treat empty LatestVersion as "unknown" — defaulting
// to currentVersion would let the dashboard flag every agent older than the
// server as outdated even when we have no real latest to compare against.
func (c *Checker) ServerUpdateInfo() *UpdateInfo {
	info := &UpdateInfo{
		CurrentVersion: c.currentVersion,
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
