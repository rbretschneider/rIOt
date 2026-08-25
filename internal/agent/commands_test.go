package agent

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DesyncTheThird/rIOt/internal/agent/collectors"
)

// fakeAgentRunner scripts every external command the os_update path and the
// HoldManager execute, recording invocation order.
type fakeAgentRunner struct {
	mu    sync.Mutex
	calls [][]string

	showhold    string   // apt-mark showhold output
	installed   string   // dpkg --get-selections output
	versionOuts []string // successive dpkg-query outputs (before, after)
	versionIdx  int
	upgradeErr  error // error for the upgrade command
	upgradeOut  string
}

func (f *fakeAgentRunner) runner() collectors.CommandRunner {
	return func(ctx context.Context, name string, args ...string) ([]byte, error) {
		f.mu.Lock()
		f.calls = append(f.calls, append([]string{name}, args...))
		f.mu.Unlock()

		joined := name + " " + strings.Join(args, " ")
		switch {
		case strings.HasSuffix(name, "apt-mark") && len(args) > 0 && args[0] == "showhold":
			return []byte(f.showhold), nil
		case name == "dpkg" && len(args) > 0 && args[0] == "--get-selections":
			return []byte(f.installed), nil
		case name == "dpkg-query":
			f.mu.Lock()
			defer f.mu.Unlock()
			if f.versionIdx < len(f.versionOuts) {
				out := f.versionOuts[f.versionIdx]
				f.versionIdx++
				return []byte(out), nil
			}
			return nil, errors.New("no scripted dpkg-query output")
		case strings.Contains(joined, "dist-upgrade") || strings.Contains(joined, "-y upgrade"):
			if f.upgradeErr != nil {
				return []byte("upgrade blew up"), f.upgradeErr
			}
			return []byte(f.upgradeOut), nil
		}
		return nil, nil
	}
}

// callIndex returns the index of the first recorded call whose joined argv
// contains sub, or -1.
func (f *fakeAgentRunner) callIndex(sub string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i, c := range f.calls {
		if strings.Contains(strings.Join(c, " "), sub) {
			return i
		}
	}
	return -1
}

func (f *fakeAgentRunner) countCalls(sub string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, c := range f.calls {
		if strings.Contains(strings.Join(c, " "), sub) {
			n++
		}
	}
	return n
}

// newOSUpdateTestAgent builds an Agent + enabled HoldManager sharing one
// fake runner, with a pre-seeded apt hold on nvidia-driver-550.
func newOSUpdateTestAgent(t *testing.T, allowReboot bool) (*Agent, *fakeAgentRunner) {
	t.Helper()
	dir := t.TempDir()
	fake := &fakeAgentRunner{
		installed:  "nvidia-driver-550\tinstall\n",
		upgradeOut: "1 upgraded, 0 newly installed, 0 to remove and 0 not upgraded.",
		versionOuts: []string{
			"nvidia-driver-550 550.90-1\n", // before
			"nvidia-driver-550 550.95-1\n", // after (changed → applied)
		},
	}

	statePath := filepath.Join(dir, "holds.json")
	seed := map[string]interface{}{
		"version": 1, "pm": "apt",
		"apt_holds": []string{"nvidia-driver-550"},
	}
	data, _ := json.Marshal(seed)
	require.NoError(t, os.WriteFile(statePath, data, 0600))

	hm := collectors.NewHoldManager(true, statePath, filepath.Join(dir, "dnf-holds.staged"))
	hm.SetRunner(fake.runner())

	a := &Agent{
		config: &Config{
			Commands: CommandsConfig{
				AllowPatching:   true,
				AllowReboot:     allowReboot,
				HoldRebootClass: true,
			},
		},
		holdMgr:      hm,
		runner:       fake.runner(),
		rebootDelay:  time.Millisecond,
		telemetryNow: make(chan struct{}, 1),
	}
	return a, fake
}

// aptTestPlan returns an os_update plan with deterministic apt argv.
func aptTestPlan(includeRC bool) osUpdatePlan {
	return osUpdatePlan{
		Mode:               "full",
		IncludeRebootClass: includeRC,
		PM:                 "apt",
		RefreshArgs:        []string{"sudo", "/usr/bin/apt-get", "update"},
		UpgradeArgs:        []string{"sudo", "/usr/bin/apt-get", "-y", "dist-upgrade"},
	}
}

// [AC-010] Holds released immediately before the upgrade and re-applied
// after the run completes — including when the upgrade command fails.
func TestRunOSUpdate_AC010_ReleaseBeforeUpgradeReapplyAfter(t *testing.T) {
	t.Run("[AC-010] success path: unhold before upgrade, re-hold after", func(t *testing.T) {
		a, fake := newOSUpdateTestAgent(t, false)

		r := a.runOSUpdate(context.Background(), "cmd-1", aptTestPlan(true))
		require.Equal(t, "success", r.Status)

		unholdIdx := fake.callIndex("apt-mark unhold")
		upgradeIdx := fake.callIndex("dist-upgrade")
		reholdIdx := fake.callIndex("apt-mark hold")
		require.GreaterOrEqual(t, unholdIdx, 0, "[AC-010] holds must be released")
		require.GreaterOrEqual(t, upgradeIdx, 0)
		require.GreaterOrEqual(t, reholdIdx, 0, "[AC-010] holds must be re-applied")
		assert.Less(t, unholdIdx, upgradeIdx, "[AC-010] release happens before the upgrade command")
		assert.Greater(t, reholdIdx, upgradeIdx, "[AC-010] re-apply happens after the run")
	})

	t.Run("[AC-010] upgrade failure: holds still re-applied before the handler returns", func(t *testing.T) {
		a, fake := newOSUpdateTestAgent(t, false)
		fake.upgradeErr = errors.New("exit status 100")

		r := a.runOSUpdate(context.Background(), "cmd-2", aptTestPlan(true))
		assert.Equal(t, "error", r.Status)
		assert.GreaterOrEqual(t, fake.callIndex("apt-mark unhold"), 0)
		assert.GreaterOrEqual(t, fake.callIndex("apt-mark hold"), 0,
			"[AC-010] defer must re-apply holds on the failure path")
	})
}

// [AC-013 agent half] Without include_reboot_class the agent never releases
// holds during the run.
func TestRunOSUpdate_AC013_NoParamMeansNoRelease(t *testing.T) {
	a, fake := newOSUpdateTestAgent(t, true)

	r := a.runOSUpdate(context.Background(), "cmd-3", aptTestPlan(false))
	require.Equal(t, "success", r.Status)

	assert.Equal(t, 0, fake.countCalls("apt-mark unhold"), "[AC-013] no holds released without the param")
	assert.Empty(t, r.RebootClassApplied)
	assert.False(t, r.RebootPending)
}

// [AC-014] In-window apply then auto-reboot when allowed: result message is
// auditable and the reboot exec fires after the result would be sent.
func TestRunOSUpdate_AC014_AppliedAndRebootInitiated(t *testing.T) {
	a, fake := newOSUpdateTestAgent(t, true)

	r := a.runOSUpdate(context.Background(), "cmd-4", aptTestPlan(true))
	require.Equal(t, "success", r.Status)
	assert.Equal(t, []string{"nvidia-driver-550"}, r.RebootClassApplied)
	assert.True(t, r.RebootPending)
	assert.Contains(t, r.Message, "reboot-class applied: nvidia-driver-550", "[AC-014] auditable message")
	assert.Contains(t, r.Message, "reboot initiated", "[AC-014]")

	a.maybeRebootAfterPatch(r)
	assert.Eventually(t, func() bool {
		return fake.countCalls("systemctl reboot") == 1
	}, 2*time.Second, 5*time.Millisecond, "[AC-014] reboot exec invoked (captured by fake runner)")
}

// [AC-015] Same run without reboot permission: host is not rebooted, the
// message says so, and a telemetry push is triggered so the
// reboot-required event path fires.
func TestRunOSUpdate_AC015_NoRebootPermissionRaisesTelemetry(t *testing.T) {
	a, fake := newOSUpdateTestAgent(t, false)

	r := a.runOSUpdate(context.Background(), "cmd-5", aptTestPlan(true))
	require.Equal(t, "success", r.Status)
	assert.Equal(t, []string{"nvidia-driver-550"}, r.RebootClassApplied)
	assert.False(t, r.RebootPending, "[AC-015] NFR-002 veto holds")
	assert.Contains(t, r.Message, "reboot required but not permitted (commands.allow_reboot: false)")

	a.maybeRebootAfterPatch(r)
	time.Sleep(20 * time.Millisecond)
	assert.Equal(t, 0, fake.countCalls("systemctl reboot"), "[AC-015] host must not reboot")
	select {
	case <-a.telemetryNow:
		// telemetry push triggered — reboot-required state reaches the server
	default:
		t.Fatal("[AC-015] expected an immediate telemetry trigger")
	}
}

// [AC-016] No reboot when no reboot-class package actually changed version,
// and none when the run failed before applying anything.
func TestRunOSUpdate_AC016_NoRebootWithoutAppliedPackages(t *testing.T) {
	t.Run("[AC-016] versions unchanged (only standard packages upgraded)", func(t *testing.T) {
		a, fake := newOSUpdateTestAgent(t, true)
		fake.versionOuts = []string{
			"nvidia-driver-550 550.90-1\n",
			"nvidia-driver-550 550.90-1\n", // unchanged
		}

		r := a.runOSUpdate(context.Background(), "cmd-6", aptTestPlan(true))
		require.Equal(t, "success", r.Status)
		assert.Empty(t, r.RebootClassApplied)
		assert.False(t, r.RebootPending)
		assert.NotContains(t, r.Message, "reboot initiated")

		a.maybeRebootAfterPatch(r)
		time.Sleep(20 * time.Millisecond)
		assert.Equal(t, 0, fake.countCalls("systemctl reboot"))
	})

	t.Run("[AC-016] run failed before applying anything", func(t *testing.T) {
		a, fake := newOSUpdateTestAgent(t, true)
		fake.upgradeErr = errors.New("exit status 100")

		r := a.runOSUpdate(context.Background(), "cmd-7", aptTestPlan(true))
		assert.Equal(t, "error", r.Status)
		assert.Empty(t, r.RebootClassApplied)

		a.maybeRebootAfterPatch(r)
		time.Sleep(20 * time.Millisecond)
		assert.Equal(t, 0, fake.countCalls("systemctl reboot"))
	})
}

// [AC-023] os_update without the param produces a result byte-for-byte in
// the pre-story shape: plain summary message, no hold calls, no reboot.
func TestRunOSUpdate_AC023_NoParamPathUnchanged(t *testing.T) {
	a, fake := newOSUpdateTestAgent(t, false)
	// Even with the feature enabled agent-side, an ordinary run is untouched.
	r := a.runOSUpdate(context.Background(), "cmd-8", aptTestPlan(false))

	require.Equal(t, "success", r.Status)
	assert.Equal(t, "Updated 1 packages", r.Message, "[AC-023] pre-story summary shape")
	assert.Equal(t, 0, fake.countCalls("apt-mark"), "[AC-023] no hold machinery invoked")
	assert.Equal(t, 0, fake.countCalls("dpkg-query"), "[AC-023] no version snapshots")
	a.maybeRebootAfterPatch(r)
	time.Sleep(20 * time.Millisecond)
	assert.Equal(t, 0, fake.countCalls("systemctl reboot"))
}

func TestTruncateOutputSmart_Short(t *testing.T) {
	input := "short output"
	result := TruncateOutputSmart(input, 1024)
	assert.Equal(t, input, result)
}

func TestTruncateOutputSmart_ExactLimit(t *testing.T) {
	input := strings.Repeat("x", 256*1024)
	result := TruncateOutputSmart(input, 256*1024)
	assert.Equal(t, input, result)
}

func TestTruncateOutputSmart_Truncated(t *testing.T) {
	input := strings.Repeat("A", 100*1024) + strings.Repeat("B", 100*1024) + strings.Repeat("C", 100*1024)
	result := TruncateOutputSmart(input, 256*1024)

	// Result should be smaller than original
	assert.Less(t, len(result), len(input))
	// Should contain the truncation marker
	assert.Contains(t, result, "... [truncated] ...")
	// Should start with the head of the input
	assert.True(t, strings.HasPrefix(result, strings.Repeat("A", 64*1024)))
	// Should end with the tail of the input
	assert.True(t, strings.HasSuffix(result, strings.Repeat("C", 64*1024)))
}

func TestTruncateOutputSmart_Empty(t *testing.T) {
	result := TruncateOutputSmart("", 1024)
	assert.Equal(t, "", result)
}

func TestTruncateOutputSmart_SmallLimit(t *testing.T) {
	input := strings.Repeat("x", 100)
	result := TruncateOutputSmart(input, 50)
	assert.LessOrEqual(t, len(result), 100) // head(25) + marker + tail(25)
	assert.Contains(t, result, "... [truncated] ...")
}

func TestParseAptSummary_UpToDate(t *testing.T) {
	output := `Reading package lists...
Building dependency tree...
Reading state information...
0 upgraded, 0 newly installed, 0 to remove and 0 not upgraded.`
	result := parseAptSummary(output)
	assert.Equal(t, "System is up to date", result)
}

func TestParseAptSummary_WithUpgrades(t *testing.T) {
	output := `12 upgraded, 0 newly installed, 2 to remove and 1 not upgraded.`
	result := parseAptSummary(output)
	assert.Contains(t, result, "Updated 12 packages")
	assert.Contains(t, result, "2 removed")
	assert.Contains(t, result, "1 held")
}

func TestParseAptSummary_UpgradesAndInstalls(t *testing.T) {
	output := `5 upgraded, 3 newly installed, 0 to remove and 0 not upgraded.`
	result := parseAptSummary(output)
	assert.Contains(t, result, "Updated 8 packages")
}

func TestParseDnfSummary_NothingToDo(t *testing.T) {
	output := `Last metadata expiration check: 0:12:34 ago.
Nothing to do.`
	result := parseDnfSummary(output)
	assert.Equal(t, "System is up to date", result)
}

func TestLastMeaningfulLines(t *testing.T) {
	output := "line1\nline2\n\nline3\n\n"
	result := lastMeaningfulLines(output, 2)
	assert.Equal(t, "line2\nline3", result)
}

func TestLastMeaningfulLines_LessLines(t *testing.T) {
	output := "only one"
	result := lastMeaningfulLines(output, 5)
	assert.Equal(t, "only one", result)
}
