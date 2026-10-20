//go:build linux

package collectors

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/DesyncTheThird/rIOt/internal/maintenance"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const detachedState = `{"version":1,"pm":"apt","apt_holds":["linux-image-amd64"],"dnf_excludes":null,"released_for_command":"","updated_at":"2026-10-20T00:00:00Z"}`

func TestDetachedHolds_AC008_OnlyManagedClassifiedHeldPackagesReleased(t *testing.T) {
	var calls [][]string
	h := &detachedAptHolds{statePath: "fixed", read: func(string, int64) ([]byte, error) { return []byte(detachedState), nil }, run: func(_ context.Context, name string, args ...string) ([]byte, error) {
		calls = append(calls, append([]string{name}, args...))
		if name == "/usr/bin/dpkg-query" {
			return []byte("linux-image-amd64\t1\tinstalled\nlinux-headers-amd64\t1\tinstalled\ncurl\t1\tinstalled\n"), nil
		}
		if len(args) > 0 && args[0] == "showhold" {
			return []byte("linux-image-amd64\nlinux-headers-amd64\n"), nil
		}
		return nil, nil
	}}
	snapshot, err := h.Snapshot(context.Background())
	require.NoError(t, err)
	assert.Equal(t, []string{"linux-image-amd64"}, snapshot.Packages)
	require.NoError(t, h.Release(context.Background(), snapshot))
	assert.Equal(t, []string{"/usr/bin/apt-mark", "unhold", "linux-image-amd64"}, calls[len(calls)-1], "[MAINT-RECOVERY AC-008] operator-only header hold is untouched")
	require.NoError(t, h.Restore(context.Background(), snapshot))
	assert.Equal(t, []string{"/usr/bin/apt-mark", "hold", "linux-image-amd64"}, calls[len(calls)-1])
}

func TestDetachedHolds_AC008_CorruptAndUnsafeStateNeverReleases(t *testing.T) {
	for _, data := range []string{
		strings.Replace(detachedState, `"version":1`, `"version":1,"version":1`, 1),
		strings.Replace(detachedState, `"version":1`, `"unknown":true,"version":1`, 1),
		strings.Replace(detachedState, `"linux-image-amd64"`, `"--config=/tmp/evil"`, 1),
		strings.Replace(detachedState, `"linux-image-amd64"`, `"curl"`, 1),
		strings.Replace(detachedState, `"apt_holds":["linux-image-amd64"]`, `"apt_holds":"linux-image-amd64"`, 1),
	} {
		t.Run(data, func(t *testing.T) {
			calls := 0
			h := &detachedAptHolds{statePath: "fixed", read: func(string, int64) ([]byte, error) { return []byte(data), nil }, run: func(context.Context, string, ...string) ([]byte, error) { calls++; return nil, nil }}
			_, err := h.Snapshot(context.Background())
			require.Error(t, err)
			assert.LessOrEqual(t, calls, 2, "[MAINT-RECOVERY AC-008] invalid state cannot reach any mutation")
		})
	}
}

func TestDetachedHolds_AC009_VersionProofAndRestoreFailure(t *testing.T) {
	h := &detachedAptHolds{run: func(_ context.Context, name string, args ...string) ([]byte, error) {
		if name == "/usr/bin/dpkg-query" {
			return []byte("linux-image-amd64\t2\tinstalled\n"), nil
		}
		return nil, errors.New("hold write failed")
	}}
	snapshot := maintenance.HoldSnapshot{Packages: []string{"linux-image-amd64"}, Versions: map[string]string{"linux-image-amd64": "1"}}
	changes, err := h.Changes(context.Background(), snapshot)
	require.NoError(t, err)
	assert.Equal(t, []maintenance.VersionChange{{Package: "linux-image-amd64", Before: "1", After: "2"}}, changes)
	assert.ErrorContains(t, h.Restore(context.Background(), snapshot), "hold write failed")
	assert.Error(t, h.Release(context.Background(), maintenance.HoldSnapshot{Packages: []string{"bash"}, Versions: map[string]string{"bash": "1"}}))
}
