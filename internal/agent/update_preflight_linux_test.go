//go:build linux

package agent

import (
	"context"
	"errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"os"
	"path/filepath"
	"testing"
)

func TestUpdateInstallAC002_MissingHelperDoesNotProbeOrLaunchPrivilege(t *testing.T) {
	run := &recordingRunner{}
	err := probeUpdateInstallation(context.Background(), run.run, func() error { return errors.New("not installed") })
	require.ErrorContains(t, err, "one-time root installer migration")
	assert.Empty(t, run.recorded())
}

func TestUpdateInstallAC003_OnlyProbesFixedSudoRuleWithoutExecutingIt(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "systemd-run"), []byte("fixture"), 0755))
	t.Setenv("PATH", dir)
	run := &recordingRunner{}
	require.NoError(t, probeUpdateInstallation(context.Background(), run.run, func() error { return nil }))
	calls := run.recorded()
	require.Len(t, calls, 1)
	assert.Equal(t, []string{"sudo", "-n", "-l"}, calls[0][:3])
	assert.Equal(t, []string{"--unit=" + updateUnit, applyBinPath}, calls[0][4:])
}

func TestUpdateInstallAC004_MissingPolicyExplainsMigration(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "systemd-run"), []byte("fixture"), 0755))
	t.Setenv("PATH", dir)
	run := &recordingRunner{failWhen: func([]string) error { return errors.New("not allowed") }}
	err := probeUpdateInstallation(context.Background(), run.run, func() error { return nil })
	require.ErrorContains(t, err, "fixed signed-apply sudo permission is unavailable")
	require.Len(t, run.recorded(), 1)
}
