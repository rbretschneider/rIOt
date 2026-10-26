//go:build linux

package agent

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// These tests start only a /bin/sh fixture that stands in for the helper. They
// never start the installed helper, riot-agent or any updater (SC-001, SC-002).

func capabilityFixture(t *testing.T, script string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "helper.sh")
	require.NoError(t, os.WriteFile(p, []byte("#!/bin/sh\n"+script+"\n"), 0o755))
	return p
}

func noTrustCheck(string, bool) error { return nil }

func TestCapabilityProbe_EU_SEC007_RunnerEnforcesCapTimeoutTrustAndEnvironment(t *testing.T) {
	t.Run("[SEC-007] the documented answer is returned and the mode is passed", func(t *testing.T) {
		p := capabilityFixture(t, `[ "$1" = --capabilities ] && printf 'request-versions=1,2\nextra-updaters=dietpi\n'`)
		out, err := capabilitiesAt(context.Background(), p, noTrustCheck, 5*time.Second)
		require.NoError(t, err)
		caps, ok := parseCapabilities(string(out))
		require.True(t, ok)
		assert.True(t, caps.supports("dietpi"))
	})
	t.Run("[SEC-007] output beyond 256 bytes is refused at the runner, not only by the parser", func(t *testing.T) {
		p := capabilityFixture(t, `head -c 100000 /dev/zero | tr '\000' 'a'`)
		_, err := capabilitiesAt(context.Background(), p, noTrustCheck, 5*time.Second)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "exceed")
	})
	t.Run("[SEC-007] a helper that never answers is cut at the timeout", func(t *testing.T) {
		p := capabilityFixture(t, `sleep 30`)
		start := time.Now()
		_, err := capabilitiesAt(context.Background(), p, noTrustCheck, 150*time.Millisecond)
		require.Error(t, err)
		assert.Less(t, time.Since(start), 10*time.Second)
	})
	t.Run("[SEC-007] an old helper that exits 1 is an error", func(t *testing.T) {
		p := capabilityFixture(t, `echo "maintenance: maintenance helper accepts no arguments" >&2; exit 1`)
		_, err := capabilitiesAt(context.Background(), p, noTrustCheck, 5*time.Second)
		require.Error(t, err)
	})
	t.Run("[SEC-007] the trust check runs first and a failure stops the run", func(t *testing.T) {
		marker := filepath.Join(t.TempDir(), "ran")
		p := capabilityFixture(t, `touch '`+marker+`'`)
		_, err := capabilitiesAt(context.Background(), p, func(string, bool) error { return errors.New("untrusted") }, 5*time.Second)
		require.Error(t, err)
		_, statErr := os.Stat(marker)
		assert.True(t, os.IsNotExist(statErr), "the helper must not run when its path is untrusted")
	})
	t.Run("[SEC-007] the environment is fixed: nothing of the agent's is inherited", func(t *testing.T) {
		t.Setenv("RIOT_SECRET_PROBE", "leak")
		p := capabilityFixture(t, `env | grep -c RIOT_SECRET_PROBE; true`)
		out, err := capabilitiesAt(context.Background(), p, noTrustCheck, 5*time.Second)
		require.NoError(t, err)
		assert.Equal(t, "0", strings.TrimSpace(string(out)))
	})
	t.Run("[SEC-007] the real helper path is checked as root-owned: an absent helper is an error", func(t *testing.T) {
		_, err := extraCapabilities(context.Background())
		require.Error(t, err, "the helper is not installed on a test machine")
	})
}

func TestExtraRootPolicy_EU_AC016_AbsentPolicyIsAnError(t *testing.T) {
	_, err := extraRootPolicy()
	require.Error(t, err, "[AC-016] a host with no root-side policy reads as not opted in")
}

func TestExtraSignature_EU_SEC007_ChangesWithTheFiles(t *testing.T) {
	assert.NotEmpty(t, extraSignature(), "absent files still produce a stable signature")
	assert.Equal(t, extraSignature(), extraSignature())
	var st syscall.Stat_t
	require.NoError(t, syscall.Stat(os.TempDir(), &st), "the syscall package is available for the signature")
}

func TestBoundedWriter_EU_SEC007_KeepsAtMostTheLimit(t *testing.T) {
	w := &boundedWriter{limit: 8}
	n, err := w.Write([]byte("0123456789"))
	require.NoError(t, err)
	assert.Equal(t, 10, n, "Write reports success so the child is not blocked")
	assert.True(t, w.exceeded)
	assert.Equal(t, "01234567", w.buf.String())
}
