package collectors

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeHoldRunner records every invocation and scripts responses for the
// commands the HoldManager runs. Privileged file operations (the sudo
// install/rm on the dnf fragment) are simulated inside the temp dir so
// tests can assert on real fragment content without an OS.
type fakeHoldRunner struct {
	mu    sync.Mutex
	calls [][]string

	showhold      string // output for `apt-mark showhold`
	dnfVersion    string // output for `dnf --version`
	installedDpkg string // output for `dpkg --get-selections`
	installedRpm  string // output for `rpm -qa --qf '%{NAME}\n'`

	failProbe   bool            // `sudo -n -l …` fails (no sudoers rule)
	failOps     map[string]bool // "hold", "unhold", "install", "rm" → fail
	fragment    string          // fragment path for simulated install/rm
	markerAtOp  map[string]string
	markerState string // state path to snapshot released_for_command at op time
}

func (f *fakeHoldRunner) runner() CommandRunner {
	return func(ctx context.Context, name string, args ...string) ([]byte, error) {
		f.mu.Lock()
		f.calls = append(f.calls, append([]string{name}, args...))
		f.mu.Unlock()

		switch {
		case strings.HasSuffix(name, "apt-mark") && len(args) > 0 && args[0] == "showhold":
			return []byte(f.showhold), nil
		case name == "dnf" && len(args) > 0 && args[0] == "--version":
			return []byte(f.dnfVersion), nil
		case name == "dpkg":
			return []byte(f.installedDpkg), nil
		case name == "rpm":
			return []byte(f.installedRpm), nil
		case name == "sudo" && len(args) > 1 && args[1] == "-l":
			if f.failProbe {
				return nil, errors.New("sudo: a password is required")
			}
			return nil, nil
		case name == "sudo":
			op := f.sudoOp(args)
			f.snapshotMarker(op)
			if f.failOps[op] {
				return nil, fmt.Errorf("%s failed", op)
			}
			f.simulate(op, args)
			return nil, nil
		}
		return nil, nil
	}
}

// sudoOp classifies a non-probe sudo invocation by its operation.
func (f *fakeHoldRunner) sudoOp(args []string) string {
	for i, a := range args {
		switch {
		case strings.HasSuffix(a, "apt-mark") && i+1 < len(args):
			return args[i+1] // hold / unhold
		case strings.HasSuffix(a, "install"):
			return "install"
		case strings.HasSuffix(a, string(filepath.Separator)+"rm") || a == "rm" || strings.HasSuffix(a, "/rm"):
			return "rm"
		}
	}
	return "unknown"
}

// snapshotMarker records the released_for_command marker value at the time
// a privileged operation runs — used to assert marker-first ordering.
func (f *fakeHoldRunner) snapshotMarker(op string) {
	if f.markerState == "" {
		return
	}
	if f.markerAtOp == nil {
		f.markerAtOp = make(map[string]string)
	}
	data, err := os.ReadFile(f.markerState)
	if err != nil {
		f.markerAtOp[op] = "<no state file>"
		return
	}
	var st struct {
		ReleasedForCommand string `json:"released_for_command"`
	}
	_ = json.Unmarshal(data, &st)
	f.markerAtOp[op] = st.ReleasedForCommand
}

// simulate performs the effect of the privileged fragment operations inside
// the temp dir.
func (f *fakeHoldRunner) simulate(op string, args []string) {
	switch op {
	case "install":
		// args: -n <install> -m 0644 -o root -g root <staged> <fragment>
		staged := args[len(args)-2]
		dest := args[len(args)-1]
		if data, err := os.ReadFile(staged); err == nil {
			_ = os.WriteFile(dest, data, 0644)
		}
	case "rm":
		_ = os.Remove(args[len(args)-1])
	}
}

func (f *fakeHoldRunner) sudoCalls() [][]string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out [][]string
	for _, c := range f.calls {
		if c[0] == "sudo" {
			out = append(out, c)
		}
	}
	return out
}

func (f *fakeHoldRunner) callsMatching(sub string) [][]string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out [][]string
	for _, c := range f.calls {
		if strings.Contains(strings.Join(c, " "), sub) {
			out = append(out, c)
		}
	}
	return out
}

// mutatingCalls returns the privileged (non-probe) sudo invocations whose
// operation matches op ("hold", "unhold", "install", "rm").
func (f *fakeHoldRunner) mutatingCalls(op string) [][]string {
	var out [][]string
	for _, c := range f.sudoCalls() {
		if len(c) > 2 && c[2] == "-l" {
			continue
		}
		if f.sudoOp(c[1:]) == op {
			out = append(out, c)
		}
	}
	return out
}

// newTestHoldManager builds an enabled HoldManager rooted in a temp dir
// with deterministic binary paths and the fake runner injected.
func newTestHoldManager(t *testing.T, enabled bool) (*HoldManager, *fakeHoldRunner, string) {
	t.Helper()
	dir := t.TempDir()
	f := &fakeHoldRunner{
		dnfVersion: "dnf5 version 5.2.1",
		fragment:   filepath.Join(dir, "60-riot-holds.conf"),
	}
	hm := &HoldManager{
		Enabled:      enabled,
		StatePath:    filepath.Join(dir, "holds.json"),
		StagedPath:   filepath.Join(dir, "dnf-holds.staged"),
		FragmentPath: filepath.Join(dir, "60-riot-holds.conf"),
		aptMarkPath:  "/usr/bin/apt-mark",
		installPath:  "/usr/bin/install",
		rmPath:       "/usr/bin/rm",
		run:          f.runner(),
	}
	f.markerState = hm.StatePath
	return hm, f, dir
}

// captureHoldLogs redirects slog to a buffer for log-content assertions.
func captureHoldLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(old) })
	return &buf
}

func readHoldState(t *testing.T, path string) holdState {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	var st holdState
	require.NoError(t, json.Unmarshal(data, &st))
	return st
}

// [AC-008] Holds applied on apt when feature enabled: installed reboot-class
// packages are held via apt-mark and recorded as rIOt-managed.
func TestHoldManager_AC008_AptHoldsApplied(t *testing.T) {
	hm, f, _ := newTestHoldManager(t, true)

	hm.Reconcile(context.Background(), "apt", []string{"nvidia-driver-550", "linux-image-generic", "curl"})

	holds := f.mutatingCalls("hold")
	require.Len(t, holds, 1, "[AC-008] one batched apt-mark hold invocation")
	assert.Equal(t, []string{"sudo", "-n", "/usr/bin/apt-mark", "hold", "linux-image-generic", "nvidia-driver-550"}, holds[0])

	st := readHoldState(t, hm.StatePath)
	assert.Equal(t, []string{"linux-image-generic", "nvidia-driver-550"}, st.AptHolds, "[AC-008] holds recorded as rIOt-managed")
	assert.Equal(t, "apt", st.PM)
	assert.Equal(t, HoldStatusActive, hm.Status())
	assert.Equal(t, []string{"linux-image-generic", "nvidia-driver-550"}, hm.HeldPackages())
}

// [AC-009] Excludes applied on dnf5: rIOt-managed fragment written via the
// fixed-path staged install; no user-owned file touched; regeneration is
// byte-identical (NFR-003).
func TestHoldManager_AC009_DnfFragmentApplied(t *testing.T) {
	hm, f, dir := newTestHoldManager(t, true)

	hm.Reconcile(context.Background(), "dnf", []string{"akmod-nvidia", "kernel-core", "bash"})

	data, err := os.ReadFile(hm.FragmentPath)
	require.NoError(t, err, "[AC-009] fragment must exist")
	assert.Contains(t, string(data), "excludepkgs=akmod-nvidia,kernel-core")
	assert.Contains(t, string(data), "[main]")

	// No file other than the fragment, staged file, and state file was created.
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	assert.ElementsMatch(t, []string{"60-riot-holds.conf", "dnf-holds.staged", "holds.json"}, names,
		"[AC-009] no user-owned dnf configuration file was modified")

	st := readHoldState(t, hm.StatePath)
	assert.Equal(t, []string{"akmod-nvidia", "kernel-core"}, st.DnfExcludes)

	// [NFR-003] Second reconcile with the same set: steady state is a read +
	// compare — no second install invocation, byte-identical fragment.
	before, _ := os.ReadFile(hm.FragmentPath)
	installsBefore := len(f.mutatingCalls("install"))
	hm.Reconcile(context.Background(), "dnf", []string{"akmod-nvidia", "kernel-core", "bash"})
	after, _ := os.ReadFile(hm.FragmentPath)
	assert.Equal(t, string(before), string(after), "[NFR-003] regenerate is byte-identical")
	assert.Equal(t, installsBefore, len(f.mutatingCalls("install")), "[NFR-003] converged state performs no privileged writes")
}

// [NFR-003] apt reconcile is idempotent: a converged second pass makes no
// mutating apt-mark calls.
func TestHoldManager_NFR003_AptIdempotent(t *testing.T) {
	hm, f, _ := newTestHoldManager(t, true)
	installed := []string{"nvidia-driver-550", "linux-image-generic"}

	hm.Reconcile(context.Background(), "apt", installed)
	f.showhold = "nvidia-driver-550\nlinux-image-generic\n" // now held on the OS
	holdCallsBefore := len(f.mutatingCalls("hold"))

	hm.Reconcile(context.Background(), "apt", installed)
	assert.Equal(t, holdCallsBefore, len(f.mutatingCalls("hold")), "[NFR-003] converged pass must not re-hold")
	assert.Empty(t, f.mutatingCalls("unhold"), "[NFR-003] converged pass must not unhold")
}

// [AC-010] Marker-first ordering: released_for_command is persisted before
// the unhold runs, and a failed unhold leaves the marker set for recovery.
func TestHoldManager_AC010_ReleaseMarkerFirstOrdering(t *testing.T) {
	hm, f, _ := newTestHoldManager(t, true)
	hm.Reconcile(context.Background(), "apt", []string{"nvidia-driver-550"})

	released := hm.ReleaseForRun(context.Background(), "cmd-42")
	assert.Equal(t, []string{"nvidia-driver-550"}, released)
	assert.Equal(t, "cmd-42", f.markerAtOp["unhold"], "[AC-010] marker must be on disk before the unhold executes")

	// Recovery: next reconcile re-asserts and clears the marker.
	logs := captureHoldLogs(t)
	f.showhold = "" // holds were released
	hm.Reconcile(context.Background(), "apt", []string{"nvidia-driver-550"})
	assert.Contains(t, logs.String(), "re-asserting holds after interrupted run")
	st := readHoldState(t, hm.StatePath)
	assert.Empty(t, st.ReleasedForCommand, "[AC-010] marker cleared after re-assert")
	assert.Equal(t, []string{"nvidia-driver-550"}, st.AptHolds)
}

// [AC-010] Release aborts when the state file cannot be written (nothing unheld).
func TestHoldManager_AC010_ReleaseAbortsWhenStateUnwritable(t *testing.T) {
	hm, f, _ := newTestHoldManager(t, true)
	hm.Reconcile(context.Background(), "apt", []string{"nvidia-driver-550"})

	// Make the state path unwritable by turning it into a directory path
	// component that cannot be created.
	hm.StatePath = filepath.Join(hm.StatePath, "impossible", "holds.json")
	released := hm.ReleaseForRun(context.Background(), "cmd-1")
	assert.Nil(t, released, "[AC-010] release must abort when the marker cannot be persisted")
	assert.Empty(t, f.mutatingCalls("unhold"), "[AC-010] nothing unheld on abort")
}

// [AC-011] Disabling the feature removes only rIOt-managed holds; operator
// holds survive; on dnf the fragment is deleted.
func TestHoldManager_AC011_DisableRemovesOnlyRiotHolds(t *testing.T) {
	t.Run("[AC-011] apt: operator hold preserved", func(t *testing.T) {
		hm, f, _ := newTestHoldManager(t, true)
		// Operator has already held postgresql-16; rIOt holds the driver.
		f.showhold = "postgresql-16\n"
		hm.Reconcile(context.Background(), "apt", []string{"nvidia-driver-550", "postgresql-16"})
		st := readHoldState(t, hm.StatePath)
		assert.Equal(t, []string{"nvidia-driver-550"}, st.AptHolds, "operator hold never recorded as ours")

		// Disable + reconcile (config change + restart flow).
		hm.Enabled = false
		hm.Reconcile(context.Background(), "apt", nil)

		unholds := f.mutatingCalls("unhold")
		require.Len(t, unholds, 1)
		assert.Equal(t, []string{"sudo", "-n", "/usr/bin/apt-mark", "unhold", "nvidia-driver-550"}, unholds[0],
			"[AC-011] only the rIOt-managed hold is released; postgresql-16 untouched")
		_, err := os.Stat(hm.StatePath)
		assert.True(t, os.IsNotExist(err), "[AC-011] state file removed on disable")
		assert.Empty(t, hm.Status())
	})

	t.Run("[AC-011] dnf: fragment deleted", func(t *testing.T) {
		hm, f, _ := newTestHoldManager(t, true)
		hm.Reconcile(context.Background(), "dnf", []string{"akmod-nvidia"})
		_, err := os.Stat(hm.FragmentPath)
		require.NoError(t, err)

		hm.Enabled = false
		hm.Reconcile(context.Background(), "dnf", nil)

		require.NotEmpty(t, f.mutatingCalls("rm"))
		_, err = os.Stat(hm.FragmentPath)
		assert.True(t, os.IsNotExist(err), "[AC-011] fragment deleted on disable")
	})
}

// [AC-023] Feature off with no prior state: zero runner invocations, zero
// file writes — behavior byte-for-byte unchanged.
func TestHoldManager_AC023_DisabledIsInert(t *testing.T) {
	hm, f, dir := newTestHoldManager(t, false)

	hm.Reconcile(context.Background(), "apt", []string{"nvidia-driver-550"})

	assert.Empty(t, f.calls, "[AC-023] zero runner invocations when disabled with no state")
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	assert.Empty(t, entries, "[AC-023] no files created when disabled")
	assert.Empty(t, hm.Status())
	assert.Empty(t, hm.HeldPackages())
}

// [SEC-AC-001] Preflight denial blocks enforcement visibly: no mutation is
// attempted, status is no_privilege, and exactly one ERROR is logged per
// state transition (not per cycle).
func TestHoldManager_SECAC001_PreflightDenialBlocksEnforcementVisibly(t *testing.T) {
	logs := captureHoldLogs(t)
	hm, f, _ := newTestHoldManager(t, true)
	f.failProbe = true

	for i := 0; i < 3; i++ {
		hm.Reconcile(context.Background(), "apt", []string{"nvidia-driver-550"})
	}

	assert.Equal(t, HoldStatusNoPrivilege, hm.Status(), "[SEC-AC-001] status must be no_privilege")
	assert.Empty(t, hm.HeldPackages(), "[SEC-AC-001] empty held list must never read as protected")
	// Only the non-mutating `sudo -n -l` probes may have run.
	for _, call := range f.sudoCalls() {
		assert.Equal(t, "-l", call[2], "[SEC-AC-001] zero mutating invocations, got %v", call)
	}
	assert.Equal(t, 1, strings.Count(logs.String(), `"level":"ERROR"`), "[SEC-AC-001] exactly one ERROR per transition")

	// Success path after the sudoers rules are fixed → active.
	f.failProbe = false
	hm.Reconcile(context.Background(), "apt", []string{"nvidia-driver-550"})
	assert.Equal(t, HoldStatusActive, hm.Status(), "[SEC-AC-001] recovers to active once privileged")
}

// [SEC-AC-001] dnf4 host: enforcement honestly unsupported — no writes,
// distinct status, one WARN per process.
func TestHoldManager_SECAC001_Dnf4Unsupported(t *testing.T) {
	logs := captureHoldLogs(t)
	hm, f, dir := newTestHoldManager(t, true)
	f.dnfVersion = "4.14.0\n  Installed: dnf-0:4.14.0-1.fc39.noarch"

	hm.Reconcile(context.Background(), "dnf", []string{"akmod-nvidia"})
	hm.Reconcile(context.Background(), "dnf", []string{"akmod-nvidia"})

	assert.Equal(t, HoldStatusUnsupported, hm.Status())
	assert.Empty(t, hm.HeldPackages())
	assert.Empty(t, f.mutatingCalls("install"), "no filesystem writes on dnf4")
	entries, _ := os.ReadDir(dir)
	assert.Empty(t, entries)
	assert.Equal(t, 1, strings.Count(logs.String(), "hold enforcement inactive on this host"), "one WARN per process")
}

// [SEC-AC-002] Privileged argv is fixed-shape: across a full reconcile +
// release + disable cycle, the only sudo argv forms are apt-mark
// hold/unhold <names…>, the exact install rule pair, the exact rm rule, and
// the -l probes. Fragment content never appears in argv.
func TestHoldManager_SECAC002_PrivilegedArgvIsFixedShape(t *testing.T) {
	assertAllowedSudoForms := func(t *testing.T, hm *HoldManager, f *fakeHoldRunner) {
		t.Helper()
		for _, call := range f.sudoCalls() {
			require.GreaterOrEqual(t, len(call), 3)
			assert.Equal(t, "-n", call[1], "every sudo call is non-interactive: %v", call)
			switch call[2] {
			case "-l":
				// probe — non-mutating
			case hm.aptMarkPath:
				assert.Contains(t, []string{"hold", "unhold"}, call[3], "apt-mark subcommand locked: %v", call)
			case hm.installPath:
				assert.Equal(t, []string{"sudo", "-n", hm.installPath, "-m", "0644", "-o", "root", "-g", "root", hm.StagedPath, hm.FragmentPath},
					call, "install rule has zero variable arguments")
			case hm.rmPath:
				assert.Equal(t, []string{"sudo", "-n", hm.rmPath, "-f", hm.FragmentPath}, call, "rm rule is exact-path")
			default:
				t.Errorf("[SEC-AC-002] unexpected privileged command: %v", call)
			}
			joined := strings.Join(call, " ")
			assert.NotContains(t, joined, "excludepkgs", "fragment content must never appear in argv")
			assert.NotContains(t, joined, "sh -c")
			assert.NotContains(t, joined, "tee")
		}
	}

	t.Run("apt cycle", func(t *testing.T) {
		hm, f, _ := newTestHoldManager(t, true)
		hm.Reconcile(context.Background(), "apt", []string{"nvidia-driver-550", "linux-image-generic"})
		hm.ReleaseForRun(context.Background(), "cmd-1")
		hm.ReapplyAfterRun("cmd-1")
		hm.Enabled = false
		hm.Reconcile(context.Background(), "apt", nil)
		assertAllowedSudoForms(t, hm, f)
	})

	t.Run("dnf cycle", func(t *testing.T) {
		hm, f, _ := newTestHoldManager(t, true)
		hm.Reconcile(context.Background(), "dnf", []string{"akmod-nvidia", "kernel-core"})
		hm.ReleaseForRun(context.Background(), "cmd-2")
		hm.Enabled = false
		hm.Reconcile(context.Background(), "dnf", nil)
		assertAllowedSudoForms(t, hm, f)
	})
}

// [SEC-AC-003] Injection-shaped names never reach privileged sinks: not in
// apt-mark argv, not in the staged fragment, not in holds.json — each skip
// WARN-logged.
func TestHoldManager_SECAC003_InjectionNamesNeverReachPrivilegedSinks(t *testing.T) {
	logs := captureHoldLogs(t)
	injected := []string{"evil,pkg", "bad\nname", "[main]x", "-o", "nvidia-driver-550,extra"}

	t.Run("apt argv and state", func(t *testing.T) {
		hm, f, _ := newTestHoldManager(t, true)
		hm.Reconcile(context.Background(), "apt", append([]string{"nvidia-driver-550"}, injected...))

		for _, call := range f.sudoCalls() {
			for _, bad := range injected {
				assert.NotContains(t, call, bad, "[SEC-AC-003] %q must not reach apt-mark argv", bad)
			}
		}
		st := readHoldState(t, hm.StatePath)
		assert.Equal(t, []string{"nvidia-driver-550"}, st.AptHolds, "[SEC-AC-003] state records only safe names")
	})

	t.Run("dnf fragment", func(t *testing.T) {
		hm, _, _ := newTestHoldManager(t, true)
		hm.Reconcile(context.Background(), "dnf", append([]string{"akmod-nvidia"}, injected...))

		data, err := os.ReadFile(hm.FragmentPath)
		require.NoError(t, err)
		assert.Contains(t, string(data), "excludepkgs=akmod-nvidia\n", "[SEC-AC-003] fragment contains only the safe name")
		for _, bad := range injected {
			assert.NotContains(t, string(data), bad)
		}
	})

	assert.GreaterOrEqual(t, strings.Count(logs.String(), "unsafe name"), len(injected),
		"[SEC-AC-003] each skipped name is WARN-logged")
}

// [SEC-AC-005] Poisoned state never unholds operator packages, and a
// corrupt state file aborts the release entirely.
func TestHoldManager_SECAC005_PoisonedStateNeverUnholdsOperatorPackages(t *testing.T) {
	t.Run("[SEC-AC-005] non-reboot-class state entry skipped with WARN", func(t *testing.T) {
		logs := captureHoldLogs(t)
		hm, f, _ := newTestHoldManager(t, true)
		st := &holdState{Version: 1, PM: "apt", AptHolds: []string{"postgresql-16", "nvidia-driver-550"}}
		require.NoError(t, hm.saveState(st))

		released := hm.ReleaseForRun(context.Background(), "cmd-9")
		assert.Equal(t, []string{"nvidia-driver-550"}, released, "[SEC-AC-005] only the reboot-class entry released")

		unholds := f.mutatingCalls("unhold")
		require.Len(t, unholds, 1)
		assert.NotContains(t, unholds[0], "postgresql-16", "[SEC-AC-005] operator-critical package never unheld")
		assert.Contains(t, logs.String(), "refusing to release", "[SEC-AC-005] skip is WARN-logged")
	})

	t.Run("[SEC-AC-005] corrupt state aborts release (nothing unheld)", func(t *testing.T) {
		hm, f, _ := newTestHoldManager(t, true)
		require.NoError(t, os.WriteFile(hm.StatePath, []byte("{not json"), 0600))

		released := hm.ReleaseForRun(context.Background(), "cmd-10")
		assert.Nil(t, released, "[SEC-AC-005] release must return empty on parse error")
		assert.Empty(t, f.mutatingCalls("unhold"), "[SEC-AC-005] zero unhold invocations")
	})
}

// [AC-012 support] Reconcile cleans up holds for packages no longer
// installed (FR-012) while keeping the rest held.
func TestHoldManager_ReconcileCleansUpUninstalledPackages(t *testing.T) {
	hm, f, _ := newTestHoldManager(t, true)
	hm.Reconcile(context.Background(), "apt", []string{"nvidia-driver-550", "linux-image-generic"})
	f.showhold = "nvidia-driver-550\nlinux-image-generic\n"

	// linux-image-generic was removed from the system.
	hm.Reconcile(context.Background(), "apt", []string{"nvidia-driver-550"})

	unholds := f.mutatingCalls("unhold")
	require.Len(t, unholds, 1)
	assert.Equal(t, []string{"sudo", "-n", "/usr/bin/apt-mark", "unhold", "linux-image-generic"}, unholds[0])
	st := readHoldState(t, hm.StatePath)
	assert.Equal(t, []string{"nvidia-driver-550"}, st.AptHolds)
}

// ReleaseForRun is a no-op when the feature is disabled (BR-002).
func TestHoldManager_ReleaseForRunDisabledIsNoop(t *testing.T) {
	hm, f, _ := newTestHoldManager(t, false)
	released := hm.ReleaseForRun(context.Background(), "cmd-1")
	assert.Nil(t, released)
	assert.Empty(t, f.calls)
}

// ReapplyAfterRun re-holds against the post-run installed set, covering
// renamed kernel packages (AD-008 step 7).
func TestHoldManager_ReapplyAfterRunUsesPostRunInstalledSet(t *testing.T) {
	hm, f, _ := newTestHoldManager(t, true)
	hm.Reconcile(context.Background(), "apt", []string{"linux-image-6.8.0-45-generic"})
	require.Equal(t, []string{"linux-image-6.8.0-45-generic"}, hm.HeldPackages())

	hm.ReleaseForRun(context.Background(), "cmd-7")

	// Post-run: the upgrade installed a new kernel image alongside the old.
	f.showhold = ""
	f.installedDpkgSet(t, []string{"linux-image-6.8.0-45-generic", "linux-image-6.8.0-46-generic"})
	hm.ReapplyAfterRun("cmd-7")

	st := readHoldState(t, hm.StatePath)
	assert.Equal(t, []string{"linux-image-6.8.0-45-generic", "linux-image-6.8.0-46-generic"}, st.AptHolds,
		"reapply covers new package names from the run")
	assert.Empty(t, st.ReleasedForCommand, "marker cleared after reapply")
	assert.Equal(t, HoldStatusActive, hm.Status())
}

// installedDpkgSet scripts the dpkg --get-selections output for the fake runner.
func (f *fakeHoldRunner) installedDpkgSet(t *testing.T, names []string) {
	t.Helper()
	var b strings.Builder
	for _, n := range names {
		fmt.Fprintf(&b, "%s\tinstall\n", n)
	}
	f.mu.Lock()
	f.installedDpkg = b.String()
	f.mu.Unlock()
}
