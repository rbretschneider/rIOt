package collectors

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DesyncTheThird/rIOt/internal/models"
)

func TestUpdatesCollectorName(t *testing.T) {
	c := &UpdatesCollector{}
	assert.Equal(t, "updates", c.Name())
}

func TestParseDNFCheckUpdate_WithUpdates(t *testing.T) {
	output := `kernel-core.x86_64                       6.7.5-200.fc39                updates
glibc.x86_64                              2.38-16.fc39                  updates
vim-enhanced.x86_64                       9.1.100-1.fc39                updates
`
	updates := parseDNFCheckUpdate(output)

	assert.Len(t, updates, 3)

	assert.Equal(t, "kernel-core", updates[0].Name)
	assert.Equal(t, "6.7.5-200.fc39", updates[0].NewVer)

	assert.Equal(t, "glibc", updates[1].Name)
	assert.Equal(t, "2.38-16.fc39", updates[1].NewVer)

	assert.Equal(t, "vim-enhanced", updates[2].Name)
	assert.Equal(t, "9.1.100-1.fc39", updates[2].NewVer)
}

func TestParseDNFCheckUpdate_Empty(t *testing.T) {
	updates := parseDNFCheckUpdate("")
	assert.Empty(t, updates)
}

func TestParseDNFCheckUpdate_BlankLines(t *testing.T) {
	output := `
kernel.x86_64                             6.7.5-200.fc39                updates

bash.x86_64                               5.2.26-1.fc39                 updates

`
	updates := parseDNFCheckUpdate(output)
	assert.Len(t, updates, 2)
	assert.Equal(t, "kernel", updates[0].Name)
	assert.Equal(t, "bash", updates[1].Name)
}

func TestParseDNFCheckUpdate_NoArchSuffix(t *testing.T) {
	// Edge case: package name without arch (unlikely but handle gracefully)
	output := `simplepackage                             1.0-1                         updates
`
	updates := parseDNFCheckUpdate(output)
	assert.Len(t, updates, 1)
	assert.Equal(t, "simplepackage", updates[0].Name)
	assert.Equal(t, "1.0-1", updates[0].NewVer)
}

func TestParseDNFCheckUpdate_NoarchPackages(t *testing.T) {
	output := `python3-docs.noarch                       3.12.2-1.fc39                 updates
tzdata.noarch                             2024a-1.fc39                  updates
`
	updates := parseDNFCheckUpdate(output)
	assert.Len(t, updates, 2)
	assert.Equal(t, "python3-docs", updates[0].Name)
	assert.Equal(t, "tzdata", updates[1].Name)
}

func TestParseDNFCheckUpdate_ShortLine(t *testing.T) {
	// Lines with fewer than 2 fields should be skipped
	output := `kernel-core.x86_64                        6.7.5-200.fc39                updates
badline
`
	updates := parseDNFCheckUpdate(output)
	assert.Len(t, updates, 1)
	assert.Equal(t, "kernel-core", updates[0].Name)
}

// [AC-005] The collector classifies via the single shared table: standard
// packages stay unclassified, reboot-class entries carry their class, and
// the same classifyUpdates pass serves both the apt and dnf paths
// (structurally: there is no second table to drift).
func TestClassifyUpdates_AC005_MixedClasses(t *testing.T) {
	info := &models.UpdateInfo{
		Updates: []models.PendingUpdate{
			{Name: "curl", NewVer: "8.5.0"},
			{Name: "linux-firmware", NewVer: "2024.1"},
			{Name: "nvidia-driver-550", NewVer: "550.90"},
			{Name: "linux-image-6.8.0-45-generic", NewVer: "6.8.0-45.45"},
			{Name: "docker-ce", NewVer: "26.0"},
		},
	}

	classifyUpdates(info)

	assert.Empty(t, info.Updates[0].Class, "[AC-005] curl stays standard")
	assert.Empty(t, info.Updates[1].Class, "[AC-005] linux-firmware stays standard")
	assert.Equal(t, ClassGPUDriver, info.Updates[2].Class)
	assert.Equal(t, ClassKernel, info.Updates[3].Class)
	assert.Empty(t, info.Updates[4].Class)
	assert.Equal(t, 2, info.PendingRebootClassCount, "[AC-005] aggregate counts reboot-class only")
}

// [AC-006] The dead kernel fields are populated from pending updates and
// reset when no kernel package is pending.
func TestClassifyUpdates_AC006_KernelFieldsPopulated(t *testing.T) {
	t.Run("[AC-006] pending kernel update populates both fields", func(t *testing.T) {
		info := &models.UpdateInfo{
			Updates: []models.PendingUpdate{
				{Name: "linux-image-6.8.0-45-generic", NewVer: "6.8.0-45.45"},
				{Name: "curl", NewVer: "8.5.0"},
			},
		}
		classifyUpdates(info)
		assert.True(t, info.PendingKernelUpdate)
		assert.Equal(t, "6.8.0-45.45", info.PendingKernelVersion)
	})

	t.Run("[AC-006] no kernel pending leaves fields false/empty", func(t *testing.T) {
		info := &models.UpdateInfo{
			Updates: []models.PendingUpdate{{Name: "curl", NewVer: "8.5.0"}},
		}
		classifyUpdates(info)
		assert.False(t, info.PendingKernelUpdate)
		assert.Empty(t, info.PendingKernelVersion)
	})
}

// [AC-017] apt reboot-required detection: marker file presence flips the
// flag; the .pkgs file feeds the deduplicated reasons list; absence means false.
func TestDetectRebootRequiredAPT_AC017(t *testing.T) {
	t.Run("[AC-017] marker + pkgs file reports true with reasons", func(t *testing.T) {
		dir := t.TempDir()
		marker := filepath.Join(dir, "reboot-required")
		pkgs := filepath.Join(dir, "reboot-required.pkgs")
		require.NoError(t, os.WriteFile(marker, []byte("*** System restart required ***\n"), 0644))
		require.NoError(t, os.WriteFile(pkgs, []byte("linux-image-6.8.0-45-generic\nlinux-base\nlinux-image-6.8.0-45-generic\n"), 0644))

		c := &UpdatesCollector{rebootRequiredPath: marker, rebootRequiredPkgsPath: pkgs}
		info := &models.UpdateInfo{}
		c.detectRebootRequiredAPT(info)

		assert.True(t, info.RebootRequired)
		assert.Equal(t, []string{"linux-image-6.8.0-45-generic", "linux-base"}, info.RebootRequiredReasons,
			"[AC-017] reasons deduplicated, package name present")
	})

	t.Run("[AC-017] absent marker reports false", func(t *testing.T) {
		dir := t.TempDir()
		c := &UpdatesCollector{
			rebootRequiredPath:     filepath.Join(dir, "reboot-required"),
			rebootRequiredPkgsPath: filepath.Join(dir, "reboot-required.pkgs"),
		}
		info := &models.UpdateInfo{}
		c.detectRebootRequiredAPT(info)
		assert.False(t, info.RebootRequired)
		assert.Empty(t, info.RebootRequiredReasons)
	})

	t.Run("[AC-017] marker without pkgs file still reports true", func(t *testing.T) {
		dir := t.TempDir()
		marker := filepath.Join(dir, "reboot-required")
		require.NoError(t, os.WriteFile(marker, []byte(""), 0644))
		c := &UpdatesCollector{
			rebootRequiredPath:     marker,
			rebootRequiredPkgsPath: filepath.Join(dir, "missing.pkgs"),
		}
		info := &models.UpdateInfo{}
		c.detectRebootRequiredAPT(info)
		assert.True(t, info.RebootRequired)
		assert.Empty(t, info.RebootRequiredReasons)
	})
}

// [AC-018] dnf reboot-required detection: exit 1 → true, exit 0 → false,
// missing/failing command → false with no WARN/ERROR log spam.
func TestDetectRebootRequiredDNF_AC018(t *testing.T) {
	run := func(code int, err error) exitRunner {
		return func(ctx context.Context, name string, args ...string) (int, error) {
			assert.Equal(t, "dnf", name)
			assert.Equal(t, []string{"needs-restarting", "-r"}, args)
			return code, err
		}
	}

	t.Run("[AC-018] exit 1 reports true", func(t *testing.T) {
		c := &UpdatesCollector{exitRun: run(1, nil)}
		info := &models.UpdateInfo{}
		c.detectRebootRequiredDNF(context.Background(), info)
		assert.True(t, info.RebootRequired)
	})

	t.Run("[AC-018] exit 0 reports false", func(t *testing.T) {
		c := &UpdatesCollector{exitRun: run(0, nil)}
		info := &models.UpdateInfo{}
		c.detectRebootRequiredDNF(context.Background(), info)
		assert.False(t, info.RebootRequired)
	})

	t.Run("[AC-018] missing command reports false without error-level logs", func(t *testing.T) {
		logs := captureHoldLogs(t)
		c := &UpdatesCollector{exitRun: run(-1, errors.New("exec: \"dnf\": executable file not found"))}
		info := &models.UpdateInfo{}
		c.detectRebootRequiredDNF(context.Background(), info)
		assert.False(t, info.RebootRequired)
		assert.NotContains(t, logs.String(), `"level":"WARN"`, "[AC-018] no WARN spam")
		assert.NotContains(t, logs.String(), `"level":"ERROR"`, "[AC-018] no ERROR spam")
	})
}

// parseDpkgSelections feeds hold reconciliation from the invocation the
// collector already makes (AD-010).
func TestParseDpkgSelections(t *testing.T) {
	output := "nvidia-driver-550\t\t\tinstall\nlibc6:amd64\t\t\tinstall\nold-package\t\t\tdeinstall\n"
	names := parseDpkgSelections(output)
	assert.Equal(t, []string{"nvidia-driver-550", "libc6"}, names,
		"install selections kept, arch qualifiers stripped, deinstall dropped")
	assert.True(t, strings.Contains(output, "deinstall"))
}
