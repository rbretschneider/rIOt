package updates

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestNormalizeVersion(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"v1.2.3", "1.2.3"},
		{"1.2.3", "1.2.3"},
		{"v0.1.0", "0.1.0"},
		{"", ""},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.want, normalizeVersion(tt.input))
		})
	}
}

func TestAssetSuffix(t *testing.T) {
	tests := []struct {
		name   string
		goos   string
		goarch string
		goarm  string
		want   string
	}{
		{"linux amd64", "linux", "amd64", "", "linux-amd64"},
		{"linux arm64", "linux", "arm64", "", "linux-arm64"},
		{"linux armv7", "linux", "arm", "7", "linux-armv7"},
		{"linux armv6", "linux", "arm", "6", "linux-armv6"},
		{"darwin amd64", "darwin", "amd64", "", "darwin-amd64"},
		{"goarm zero treated as no arm", "linux", "amd64", "0", "linux-amd64"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, assetSuffix(tt.goos, tt.goarch, tt.goarm))
		})
	}
}

func TestAgentUpdateInfo_NoRelease(t *testing.T) {
	c := NewChecker("test/repo", "1.0.0")
	info := c.AgentUpdateInfo("1.0.0", "linux", "amd64", "")

	assert.Equal(t, "1.0.0", info.CurrentVersion)
	assert.False(t, info.UpdateAvail)
}

func TestAgentUpdateInfo_WithRelease(t *testing.T) {
	c := NewChecker("test/repo", "1.0.0")
	c.latest = &GitHubRelease{
		TagName:   "v1.1.0",
		HTMLURL:   "https://github.com/test/repo/releases/v1.1.0",
		Published: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		Assets: []GitHubAsset{
			{Name: "riot-agent-linux-amd64", BrowserDownloadURL: "https://example.com/linux-amd64"},
			{Name: "checksums.txt", BrowserDownloadURL: "https://example.com/checksums.txt"},
		},
	}

	info := c.AgentUpdateInfo("1.0.0", "linux", "amd64", "")
	assert.True(t, info.UpdateAvail)
	assert.Equal(t, "1.1.0", info.LatestVersion)
	assert.Equal(t, "https://example.com/linux-amd64", info.Assets["linux-amd64"])
	assert.Equal(t, "https://example.com/checksums.txt", info.ChecksumURL)
}

func TestAgentUpdateInfo_DevVersion(t *testing.T) {
	c := NewChecker("test/repo", "1.0.0")
	c.latest = &GitHubRelease{TagName: "v1.1.0"}

	info := c.AgentUpdateInfo("dev", "linux", "amd64", "")
	assert.False(t, info.UpdateAvail, "dev versions should not show updates")
}

func TestServerUpdateInfo_NoRelease(t *testing.T) {
	c := NewChecker("test/repo", "1.0.0")
	info := c.ServerUpdateInfo()

	assert.Equal(t, "1.0.0", info.CurrentVersion)
	// LatestVersion must stay empty when the release feed has not been reached:
	// defaulting to CurrentVersion would let the dashboard flag agents older
	// than the server as outdated even when we have no real latest to compare to.
	assert.Equal(t, "", info.LatestVersion)
	assert.False(t, info.UpdateAvail)
}

func TestAgentUpdateInfo_NoReleaseLeavesLatestEmpty(t *testing.T) {
	c := NewChecker("test/repo", "2.57.0")
	info := c.AgentUpdateInfo("2.56.0", "linux", "amd64", "")

	// Agent is older than the server's currentVersion, but with no release
	// data we must not claim a latest — empty signals "unknown" so the agent
	// does not report a fake "already up to date" success.
	assert.Equal(t, "2.56.0", info.CurrentVersion)
	assert.Equal(t, "", info.LatestVersion)
	assert.False(t, info.UpdateAvail)
}

func TestServerUpdateInfo_UpdateAvailable(t *testing.T) {
	c := NewChecker("test/repo", "1.0.0")
	c.latest = &GitHubRelease{
		TagName: "v2.0.0",
		HTMLURL: "https://github.com/test/repo/releases/v2.0.0",
	}

	info := c.ServerUpdateInfo()
	assert.True(t, info.UpdateAvail)
	assert.Equal(t, "2.0.0", info.LatestVersion)
}

func TestAgentUpdateInfo_NoDowngrade(t *testing.T) {
	c := NewChecker("test/repo", "1.0.0")
	c.latest = &GitHubRelease{TagName: "v2.6.5"}

	info := c.AgentUpdateInfo("2.6.6", "linux", "amd64", "")
	assert.False(t, info.UpdateAvail, "agent ahead of latest release should not trigger update")
}

func TestAwaitFirstSuccess_ReturnsImmediatelyOnFirstSuccess(t *testing.T) {
	var calls atomic.Int32
	fn := func() bool {
		calls.Add(1)
		return true
	}
	start := time.Now()
	awaitFirstSuccess(context.Background(), fn, []time.Duration{time.Second})
	elapsed := time.Since(start)

	assert.Equal(t, int32(1), calls.Load(), "fn should be called exactly once on immediate success")
	assert.Less(t, elapsed, 100*time.Millisecond, "must not sleep when fn succeeds first try")
}

func TestAwaitFirstSuccess_RetriesUntilSuccess(t *testing.T) {
	var calls atomic.Int32
	// Fail twice, then succeed — covers the case where DNS or network is briefly
	// unavailable at container startup but recovers within the backoff window.
	fn := func() bool {
		return calls.Add(1) >= 3
	}
	awaitFirstSuccess(context.Background(), fn, []time.Duration{
		1 * time.Millisecond,
		1 * time.Millisecond,
		1 * time.Millisecond,
	})

	assert.Equal(t, int32(3), calls.Load(), "fn should be retried until it returns true")
}

func TestAwaitFirstSuccess_StopsOnContextCancel(t *testing.T) {
	var calls atomic.Int32
	fn := func() bool {
		calls.Add(1)
		return false
	}
	ctx, cancel := context.WithCancel(context.Background())
	// Cancel after a short delay so a few retries can run with a tight backoff.
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	done := make(chan struct{})
	go func() {
		awaitFirstSuccess(ctx, fn, []time.Duration{1 * time.Millisecond})
		close(done)
	}()

	select {
	case <-done:
		// expected
	case <-time.After(time.Second):
		t.Fatal("awaitFirstSuccess did not return after context cancel")
	}
	assert.Greater(t, calls.Load(), int32(0), "fn should have been called at least once before cancel")
}

func TestIsNewer(t *testing.T) {
	tests := []struct {
		candidate string
		current   string
		want      bool
	}{
		{"2.6.7", "2.6.5", true},
		{"2.6.5", "2.6.6", false},
		{"2.6.5", "2.6.5", false},
		{"3.0.0", "2.9.9", true},
		{"1.1.0", "1.0.9", true},
		{"1.0.0", "1.0.0", false},
	}
	for _, tt := range tests {
		t.Run(tt.candidate+"_vs_"+tt.current, func(t *testing.T) {
			assert.Equal(t, tt.want, isNewer(tt.candidate, tt.current))
		})
	}
}
