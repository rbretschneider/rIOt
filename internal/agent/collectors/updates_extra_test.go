package collectors

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DesyncTheThird/rIOt/internal/extraupd"
	"github.com/DesyncTheThird/rIOt/internal/models"
)

// SC-001 / SC-004: fixture trees only. dietpi-update is a non-executable
// tripwire and nothing here starts a process or touches the network.

func put(t *testing.T, root, rel, body string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
	require.NoError(t, os.WriteFile(p, []byte(body), 0o644))
}

const (
	versionFile = "G_DIETPI_VERSION_CORE=9\nG_DIETPI_VERSION_SUB=20\nG_DIETPI_VERSION_RC=1\nG_GITBRANCH='master'\nG_GITOWNER='MichaIng'\n"
	tripwire    = "#!/bin/sh\necho DIETPI TRIPWIRE >&2\nexit 97\n"
	release     = "6.12.47+rpt-rpi-v8"
)

// dietpiTree is a DietPi host: 9.20.1 with 10.8.2 pending and the kernel's
// modules directory present.
func dietpiTree(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	put(t, root, "boot/dietpi/.version", versionFile)
	put(t, root, "boot/dietpi/dietpi-update", tripwire)
	put(t, root, "boot/dietpi.txt", "CONFIG_CHECK_DIETPI_UPDATES=1\n")
	put(t, root, "run/dietpi/.update_available", "10.8.2\n")
	put(t, root, "proc/sys/kernel/osrelease", release+"\n")
	require.NoError(t, os.MkdirAll(filepath.Join(root, "lib", "modules", release), 0o755))
	return root
}

func TestUpdatesCollector_EU_AC001_EmptyListWithoutDietPi(t *testing.T) {
	c := &UpdatesCollector{kernelRoot: t.TempDir()}
	assert.Empty(t, c.detectExtraUpdaters(context.Background()), "[AC-001] hawaii and vermont report an empty extra-updater list")
	onlyOne := t.TempDir()
	put(t, onlyOne, "boot/dietpi/.version", versionFile)
	assert.Empty(t, (&UpdatesCollector{kernelRoot: onlyOne}).detectExtraUpdaters(context.Background()), "[AC-001] one of the two files is not DietPi")
	tree := dietpiTree(t)
	got := (&UpdatesCollector{kernelRoot: tree, extraFS: extraupd.DirFSOwnedByCaller(tree)}).detectExtraUpdaters(context.Background())
	require.Len(t, got, 1, "[AC-001] both files report one dietpi entry")
	assert.Equal(t, "dietpi", got[0].ID)
	assert.Equal(t, models.ExtraStatusUpdateAvailable, got[0].Status)
	assert.Empty(t, got[0].Readiness, "the collector leaves readiness to the agent")
}

func TestUpdatesCollector_EU_AC010_MissingModulesRebootRequired(t *testing.T) {
	t.Run("[AC-010] a missing modules directory means reboot required", func(t *testing.T) {
		root := dietpiTree(t)
		put(t, root, "proc/sys/kernel/osrelease", "6.12.99+rpt-rpi-v8\n")
		c := &UpdatesCollector{kernelRoot: root, rebootRequiredPath: filepath.Join(t.TempDir(), "no-marker"), rebootRequiredPkgsPath: filepath.Join(t.TempDir(), "no-pkgs")}
		info := &models.UpdateInfo{}
		c.detectRebootRequiredAPT(info)
		assert.True(t, info.RebootRequired)
		assert.Equal(t, []string{"kernel modules for running kernel 6.12.99+rpt-rpi-v8 are missing"}, info.RebootRequiredReasons)
	})
	t.Run("[AC-010] with the directory present the PATCH-FLOW-001 rule is unchanged", func(t *testing.T) {
		c := &UpdatesCollector{kernelRoot: dietpiTree(t), rebootRequiredPath: filepath.Join(t.TempDir(), "no-marker"), rebootRequiredPkgsPath: filepath.Join(t.TempDir(), "no-pkgs")}
		info := &models.UpdateInfo{}
		c.detectRebootRequiredAPT(info)
		assert.False(t, info.RebootRequired)
		assert.Empty(t, info.RebootRequiredReasons)
	})
	t.Run("[AC-010] a non-DietPi host never reports it", func(t *testing.T) {
		root := t.TempDir()
		put(t, root, "proc/sys/kernel/osrelease", "6.1.0-13-amd64\n")
		c := &UpdatesCollector{kernelRoot: root, rebootRequiredPath: filepath.Join(t.TempDir(), "no-marker")}
		info := &models.UpdateInfo{}
		c.detectRebootRequiredAPT(info)
		assert.False(t, info.RebootRequired)
	})
	t.Run("[AC-010] the apt marker still works beside the new rule", func(t *testing.T) {
		marker := filepath.Join(t.TempDir(), "reboot-required")
		require.NoError(t, os.WriteFile(marker, nil, 0o644))
		c := &UpdatesCollector{kernelRoot: dietpiTree(t), rebootRequiredPath: marker, rebootRequiredPkgsPath: filepath.Join(t.TempDir(), "no-pkgs")}
		info := &models.UpdateInfo{}
		c.detectRebootRequiredAPT(info)
		assert.True(t, info.RebootRequired)
	})
}

func TestRebootRequiredNow_EU_AC010_SameRule(t *testing.T) {
	root := dietpiTree(t)
	probe := rebootProbe{root: root, markerPath: filepath.Join(t.TempDir(), "no-marker")}
	assert.False(t, probe.required(context.Background(), "apt"))
	put(t, root, "proc/sys/kernel/osrelease", "6.12.99+rpt-rpi-v8\n")
	assert.True(t, probe.required(context.Background(), "apt"), "[AC-010] the authorized-reboot decision uses the collector's rule, so they can never disagree")
}

func TestTelemetry_EU_AC011_DietPiEntryWithTodaysCollectorList(t *testing.T) {
	// The updates collector's name is already in every agent's collector.enabled
	// list, and detection rides on it: the entry appears without a config change.
	tree := dietpiTree(t)
	c := &UpdatesCollector{kernelRoot: tree, extraFS: extraupd.DirFSOwnedByCaller(tree)}
	assert.Equal(t, "updates", c.Name())
	got := c.detectExtraUpdaters(context.Background())
	require.Len(t, got, 1)
	assert.Equal(t, "9.20.1", got[0].InstalledVersion)
	assert.Equal(t, "10.8.2", got[0].AvailableVersion)
	assert.Equal(t, 1, got[0].PendingCount)
	assert.True(t, got[0].HasNotice(models.ExtraNoticeMajorUpgrade))
}
