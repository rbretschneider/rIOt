package agent

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DesyncTheThird/rIOt/internal/extraupd"
)

// SC-001 / SC-002: doctor's extra-updater section reads a fixture tree and talks
// to injected probes. The runner records every call so a test can prove no
// updater was started.
func doctorOutput(t *testing.T, rig *extraRig, cmds CommandsConfig) string {
	t.Helper()
	return captureStdout(func() { reportExtraUpdaters(rig.a, cmds, "linux", 5*time.Second) })
}

func TestDoctorExtra_EU_AC014_UnknownIDWarns(t *testing.T) {
	rig := newExtraRig(t, dietpiRoot(t), CommandsConfig{AllowPatching: true, AllowExtraUpdaters: ExtraUpdaterIDs{"dietpi", "snap"}})
	out := doctorOutput(t, rig, rig.a.config.Commands)
	warnings := outputLinesWith(out, `"snap"`)
	require.Len(t, warnings, 1, "[AC-014] exactly one warning line for snap:\n%s", out)
	assert.Contains(t, warnings[0], "does not implement")
	assert.Empty(t, outputLinesWith(out, `"dietpi"`), "the implemented identifier is not warned about")
}

func TestDoctorExtra_EU_AC042_PassFailNotApplicable(t *testing.T) {
	t.Run("[AC-042] everything in place prints pass lines", func(t *testing.T) {
		rig := newExtraRig(t, dietpiRoot(t), enabledCommands())
		out := doctorOutput(t, rig, rig.a.config.Commands)
		assert.Contains(t, out, "Extra Updaters")
		assert.Contains(t, out, "dietpi detected (9.20.1)")
		assert.Contains(t, out, "the root-side opt-in lists it")
		assert.Contains(t, out, "the maintenance helper can run it (request version 2)")
		assert.Contains(t, out, "the maintenance service allows 1h25m0s with no mount sandbox")
		assert.NotContains(t, out, "✗", "no failure line when everything is in place:\n"+out)
	})
	t.Run("[AC-042] a missing root side prints a fail line with the re-run guidance", func(t *testing.T) {
		rig := newExtraRig(t, dietpiRoot(t), enabledCommands())
		rig.client.rootPolicy = func() ([]byte, error) { return nil, os.ErrNotExist }
		out := doctorOutput(t, rig, rig.a.config.Commands)
		failures := outputLinesWith(out, "root-side opt-in does not list it")
		require.Len(t, failures, 1, out)
		assert.Contains(t, failures[0], "re-run install.sh as root")
		assert.Contains(t, failures[0], "✗")
	})
	t.Run("[AC-042] an older helper prints a fail line", func(t *testing.T) {
		rig := newExtraRig(t, dietpiRoot(t), enabledCommands())
		rig.client.capabilities = func(context.Context) ([]byte, error) { return nil, errors.New("exit status 1") }
		out := doctorOutput(t, rig, rig.a.config.Commands)
		assert.Contains(t, out, "cannot run extra updaters; re-run install.sh as root to install the newer helper")
	})
	t.Run("[AC-042] a 35 minute backstop and a mount sandbox are failures", func(t *testing.T) {
		rig := newExtraRig(t, dietpiRoot(t), enabledCommands())
		rig.client.unitShow = func(context.Context) (string, error) {
			return "RuntimeMaxUSec=35min\nPrivateTmp=no\nMountFlags=\n", nil
		}
		assert.Contains(t, doctorOutput(t, rig, rig.a.config.Commands), "shorter than a DietPi run needs")
		rig = newExtraRig(t, dietpiRoot(t), enabledCommands())
		rig.client.unitShow = func(context.Context) (string, error) {
			return "RuntimeMaxUSec=1h 25min\nPrivateTmp=yes\nMountFlags=\n", nil
		}
		assert.Contains(t, doctorOutput(t, rig, rig.a.config.Commands), "mount sandbox")
	})
	t.Run("[AC-042] a host without DietPi is not applicable", func(t *testing.T) {
		rig := newExtraRig(t, t.TempDir(), enabledCommands())
		out := doctorOutput(t, rig, rig.a.config.Commands)
		assert.Contains(t, out, "dietpi: not applicable here")
		assert.NotContains(t, out, "✗")
	})
	t.Run("[AC-042] allow_patching false is a warning", func(t *testing.T) {
		rig := newExtraRig(t, dietpiRoot(t), CommandsConfig{AllowExtraUpdaters: ExtraUpdaterIDs{"dietpi"}})
		assert.Contains(t, doctorOutput(t, rig, rig.a.config.Commands), "commands.allow_patching is false")
	})
	t.Run("[AC-042] a non-default update source is a failure", func(t *testing.T) {
		root := dietpiRoot(t)
		writeHost(t, root, "boot/dietpi.txt", "CONFIG_CHECK_DIETPI_UPDATES=1\nDEV_GITOWNER=Fork\n")
		rig := newExtraRig(t, root, enabledCommands())
		assert.Contains(t, doctorOutput(t, rig, rig.a.config.Commands), "rIOt will not run it")
	})
	t.Run("[AC-042] the section is silent when nothing is listed or off Linux", func(t *testing.T) {
		rig := newExtraRig(t, dietpiRoot(t), CommandsConfig{AllowPatching: true})
		assert.Empty(t, doctorOutput(t, rig, rig.a.config.Commands))
		rig = newExtraRig(t, dietpiRoot(t), enabledCommands())
		out := captureStdout(func() { reportExtraUpdaters(rig.a, rig.a.config.Commands, "windows", time.Second) })
		assert.Empty(t, out)
	})
	t.Run("[AC-042] the runner never receives dietpi-update or any DietPi program", func(t *testing.T) {
		rig := newExtraRig(t, dietpiRoot(t), enabledCommands())
		doctorOutput(t, rig, rig.a.config.Commands)
		for _, c := range rig.runnerCommands {
			assert.NotContains(t, strings.ToLower(strings.Join(c, " ")), "dietpi", "doctor must not run any updater (FR-061)")
		}
		assert.Empty(t, rig.runnerCommands, "doctor starts no process of its own in this test; every probe is injected")
	})
}

func TestDoctorExtra_EU_AC042_ProbeTimeoutIsBounded(t *testing.T) {
	rig := newExtraRig(t, dietpiRoot(t), enabledCommands())
	rig.client.capabilities = func(ctx context.Context) ([]byte, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	start := time.Now()
	out := captureStdout(func() { reportExtraUpdaters(rig.a, rig.a.config.Commands, "linux", 100*time.Millisecond) })
	assert.Less(t, time.Since(start), 5*time.Second)
	assert.Contains(t, out, "cannot run extra updaters")
}

func TestDoctorExtra_EU_AC042_DietPiIdentifierIsTheRegistryOne(t *testing.T) {
	_, ok := extraupd.Lookup("dietpi")
	require.True(t, ok)
}
