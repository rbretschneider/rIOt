package collectors

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Hold-enforcement status values reported in UpdateInfo.HoldEnforcement
// (PATCH-GATE AD-015). Empty means the feature is disabled on the agent.
const (
	HoldStatusActive      = "active"
	HoldStatusNoPrivilege = "no_privilege"
	HoldStatusUnsupported = "unsupported"
)

// DNFFragmentPath is the fixed destination of the rIOt-owned excludepkgs
// drop-in on dnf5 hosts. It appears verbatim in the sudoers rules written by
// scripts/install.sh — any drift makes the sudo rule never match (ADD
// Implementation Note #15).
const DNFFragmentPath = "/etc/dnf/libdnf5.conf.d/60-riot-holds.conf"

// reapplyTimeout bounds ReapplyAfterRun's background context (ADD
// Implementation Note #4).
const reapplyTimeout = 2 * time.Minute

// CommandRunner executes an external command and returns its combined
// output. Injectable so hold tests run without an OS (ADD Note #2).
type CommandRunner func(ctx context.Context, name string, args ...string) ([]byte, error)

func defaultRunner(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).CombinedOutput()
}

// holdState is the on-disk bookkeeping at /var/lib/riot/holds.json — the
// sole record of which holds rIOt created, as distinct from operator holds
// (FR-013, BR-004). ReleasedForCommand is the crash-recovery marker: set
// before holds are released for a run, cleared when they are re-applied.
type holdState struct {
	Version            int       `json:"version"`
	PM                 string    `json:"pm"`
	AptHolds           []string  `json:"apt_holds"`
	DnfExcludes        []string  `json:"dnf_excludes"`
	ReleasedForCommand string    `json:"released_for_command"`
	UpdatedAt          time.Time `json:"updated_at"`
}

// recorded returns the rIOt-managed hold list for the state's package manager.
func (st *holdState) recorded() []string {
	if st.PM == "dnf" {
		return st.DnfExcludes
	}
	return st.AptHolds
}

// HoldManager keeps installed reboot-class packages held at the OS level
// while commands.hold_reboot_class is enabled: apt-mark holds on apt, a
// rIOt-owned excludepkgs drop-in fragment on dnf5 (PATCH-GATE AD-003–AD-005).
// All public methods are safe for concurrent use — the telemetry loop and
// the command handler run in different goroutines.
type HoldManager struct {
	mu      sync.Mutex
	Enabled bool

	StatePath    string // /var/lib/riot/holds.json
	StagedPath   string // /var/lib/riot/dnf-holds.staged (fixed sudoers source path)
	FragmentPath string // DNFFragmentPath (fixed sudoers destination path)

	// Resolved binary paths for privileged argv; overridable in tests.
	aptMarkPath string
	installPath string
	rmPath      string

	run CommandRunner

	status     string   // "", HoldStatusActive, HoldStatusNoPrivilege, HoldStatusUnsupported
	held       []string // sorted rIOt-managed holds as of the last reconcile (only when active)
	warnedDNF4 bool     // one WARN per process lifetime (AD-005)
}

// NewHoldManager builds a HoldManager. enabled comes from the agent's
// commands.hold_reboot_class flag; statePath and stagedPath come from the
// agent config path helpers.
func NewHoldManager(enabled bool, statePath, stagedPath string) *HoldManager {
	return &HoldManager{
		Enabled:      enabled,
		StatePath:    statePath,
		StagedPath:   stagedPath,
		FragmentPath: DNFFragmentPath,
		aptMarkPath:  lookPathOr("apt-mark", "/usr/bin/apt-mark"),
		installPath:  lookPathOr("install", "/usr/bin/install"),
		rmPath:       lookPathOr("rm", "/usr/bin/rm"),
		run:          defaultRunner,
	}
}

func lookPathOr(name, fallback string) string {
	if p, err := exec.LookPath(name); err == nil {
		return p
	}
	return fallback
}

// Status returns the current hold-enforcement status for telemetry
// (AD-015 table). Empty when the feature is disabled.
func (hm *HoldManager) Status() string {
	hm.mu.Lock()
	defer hm.mu.Unlock()
	return hm.status
}

// HeldPackages returns the sorted rIOt-managed hold list from the last
// reconcile. Empty unless enforcement is active — an unenforced state never
// claims holds (SEC-PATCH-GATE-001 mitigation 3).
func (hm *HoldManager) HeldPackages() []string {
	hm.mu.Lock()
	defer hm.mu.Unlock()
	if hm.status != HoldStatusActive {
		return nil
	}
	out := make([]string, len(hm.held))
	copy(out, hm.held)
	return out
}

// VerifyPrivileges probes (without executing anything privileged) whether
// the AD-015 sudoers rules exist for the given package manager, via
// `sudo -n -l <cmd> <args...>`. Exported so `riot-agent doctor` runs the
// exact same probes (SEC-PATCH-GATE-004).
func (hm *HoldManager) VerifyPrivileges(ctx context.Context, pm string) error {
	var args []string
	if pm == "dnf" {
		args = []string{"-n", "-l", hm.installPath, "-m", "0644", "-o", "root", "-g", "root", hm.StagedPath, hm.FragmentPath}
	} else {
		args = []string{"-n", "-l", hm.aptMarkPath, "hold", "riot-preflight-probe"}
	}
	if out, err := hm.run(ctx, "sudo", args...); err != nil {
		return fmt.Errorf("sudo rule probe failed: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// Reconcile converges OS-level holds against the installed package set
// (FR-011, FR-012). Called once per telemetry cycle by the updates
// collector, which passes the installed names it already parsed (NFR-004 —
// no extra package-manager invocations). Failures log WARN and leave the
// previous state; the rest of the telemetry cycle proceeds (NFR-005).
func (hm *HoldManager) Reconcile(ctx context.Context, pm string, installed []string) {
	hm.mu.Lock()
	defer hm.mu.Unlock()
	hm.reconcileLocked(ctx, pm, installed)
}

// ReconcileStartup re-asserts holds once at agent startup (AD-007) so a
// host that crashed mid-run is re-protected within seconds of the agent
// restarting. Detects the package manager and gathers installed names
// itself — a one-time startup cost.
func (hm *HoldManager) ReconcileStartup(ctx context.Context) {
	hm.mu.Lock()
	defer hm.mu.Unlock()

	st, _ := hm.loadState()
	pm := st.PM
	if pm == "" {
		switch {
		case lookPathExists("apt-mark"):
			pm = "apt"
		case lookPathExists("dnf"):
			pm = "dnf"
		default:
			return
		}
	}

	var installed []string
	if hm.Enabled {
		var err error
		installed, err = hm.listInstalled(ctx, pm)
		if err != nil {
			slog.Warn("holds: startup reconcile could not list installed packages", "error", err)
			return
		}
	}
	hm.reconcileLocked(ctx, pm, installed)
}

func lookPathExists(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

// ReleaseForRun releases rIOt-managed holds for the duration of one
// in-window os_update run (FR-015, AD-008). Marker-first ordering: the
// released_for_command marker is persisted BEFORE anything is unheld, so a
// crash between release and re-apply is recoverable (§7C). Fail-closed
// (SEC-PATCH-GATE-005): a state load/parse error aborts the release
// entirely, and a name is released only if it is both state-tracked AND
// currently classifies reboot-class. Returns the released package names
// (nil when nothing was released).
func (hm *HoldManager) ReleaseForRun(ctx context.Context, commandID string) []string {
	hm.mu.Lock()
	defer hm.mu.Unlock()

	if !hm.Enabled {
		return nil
	}

	st, err := hm.loadState()
	if err != nil {
		slog.Warn("holds: state file unreadable at release; aborting release (fail closed)", "error", err)
		return nil
	}

	var candidates []string
	for _, p := range st.recorded() {
		if !ValidPackageName(p) || ClassifyPackage(p) == "" {
			slog.Warn("holds: refusing to release state entry that is not reboot-class", "package", p)
			continue
		}
		candidates = append(candidates, p)
	}
	if len(candidates) == 0 {
		return nil
	}

	// Persist the crash marker before the first unhold (ADD Note #3).
	st.ReleasedForCommand = commandID
	if err := hm.saveState(st); err != nil {
		slog.Warn("holds: cannot persist release marker; aborting release", "error", err)
		return nil
	}

	if st.PM == "dnf" {
		if out, err := hm.run(ctx, "sudo", "-n", hm.rmPath, "-f", hm.FragmentPath); err != nil {
			slog.Warn("holds: fragment delete failed during release", "error", err, "output", strings.TrimSpace(string(out)))
			return nil
		}
	} else {
		args := append([]string{"-n", hm.aptMarkPath, "unhold"}, candidates...)
		if out, err := hm.run(ctx, "sudo", args...); err != nil {
			slog.Warn("holds: apt-mark unhold failed during release", "error", err, "output", strings.TrimSpace(string(out)))
			return nil
		}
	}

	slog.Info("holds: released rIOt-managed holds for patch run", "command_id", commandID, "packages", candidates)
	return candidates
}

// ReapplyAfterRun re-asserts holds against the post-run installed set and
// clears the release marker. Registered via defer in the os_update handler
// so it covers every exit path (NFR-001); it therefore uses its own
// background context — the run's context may already be dead.
func (hm *HoldManager) ReapplyAfterRun(commandID string) {
	ctx, cancel := context.WithTimeout(context.Background(), reapplyTimeout)
	defer cancel()

	hm.mu.Lock()
	defer hm.mu.Unlock()

	st, err := hm.loadState()
	if err != nil {
		slog.Warn("holds: state unreadable at reapply; next reconcile will re-assert", "command_id", commandID, "error", err)
		return
	}
	pm := st.PM
	if pm == "" {
		return
	}

	installed, err := hm.listInstalled(ctx, pm)
	if err != nil {
		slog.Warn("holds: reapply could not list installed packages; next reconcile will re-assert", "command_id", commandID, "error", err)
		return
	}

	hm.reconcileLocked(ctx, pm, installed)
	slog.Info("holds: re-applied holds after patch run", "command_id", commandID, "packages", hm.held)
}

// ---- internals (callers hold hm.mu) ----

// reconcileLocked is the core convergence pass shared by Reconcile,
// ReconcileStartup, and ReapplyAfterRun.
func (hm *HoldManager) reconcileLocked(ctx context.Context, pm string, installed []string) {
	st, err := hm.loadState()
	if err != nil {
		// Corrupt state fails closed: treat as empty (nothing is "ours" to
		// release) and let convergence re-hold and rewrite a fresh file (§9).
		slog.Warn("holds: state file unreadable, treating as empty (releasing nothing)", "error", err)
		st = &holdState{Version: 1, PM: pm}
	}
	if st.PM == "" {
		st.PM = pm
	}

	if !hm.Enabled {
		hm.setStatusLocked("")
		hm.held = nil
		if len(st.AptHolds) == 0 && len(st.DnfExcludes) == 0 && st.ReleasedForCommand == "" {
			// Nothing to clean up. If a stale empty state file exists, remove
			// it; otherwise this is a pure no-op (AC-023: zero invocations,
			// zero writes with the feature never enabled).
			if _, statErr := os.Stat(hm.StatePath); statErr == nil {
				_ = os.Remove(hm.StatePath)
			}
			return
		}
		hm.disableCleanupLocked(ctx, st)
		return
	}

	if pm == "dnf" && !hm.dnf5Supported(ctx) {
		if !hm.warnedDNF4 {
			slog.Warn("holds: OS-level dnf holds require dnf5; hold enforcement inactive on this host")
			hm.warnedDNF4 = true
		}
		hm.setStatusLocked(HoldStatusUnsupported)
		hm.held = nil
		return
	}

	// Fail-closed preflight (SEC-PATCH-GATE-001): no hold mutation is ever
	// attempted unless the sudoers rules verifiably exist.
	if err := hm.VerifyPrivileges(ctx, pm); err != nil {
		if hm.status != HoldStatusNoPrivilege {
			slog.Error("holds: hold enforcement enabled but sudo rules missing; re-run the installer to update /etc/sudoers.d/riot-agent", "error", err)
		}
		hm.setStatusLocked(HoldStatusNoPrivilege)
		hm.held = nil
		return
	}

	if st.ReleasedForCommand != "" {
		slog.Warn("holds: re-asserting holds after interrupted run", "command_id", st.ReleasedForCommand)
	}

	desired := hm.desiredHoldSet(installed)

	var ok bool
	if pm == "dnf" {
		ok = hm.reconcileDnfLocked(ctx, st, desired)
	} else {
		ok = hm.reconcileAptLocked(ctx, st, desired)
	}
	if !ok {
		return // WARN already logged; previous state retained, retry next cycle
	}

	st.ReleasedForCommand = ""
	st.UpdatedAt = time.Now().UTC()
	if err := hm.saveState(st); err != nil {
		slog.Warn("holds: failed to persist hold state", "error", err)
	}
	hm.setStatusLocked(HoldStatusActive)
	hm.held = append([]string(nil), st.recorded()...)
}

// desiredHoldSet filters installed names to the sorted, charset-validated
// reboot-class set (AD-016: names failing validation are skipped + WARNed
// before they can reach any privileged sink).
func (hm *HoldManager) desiredHoldSet(installed []string) []string {
	var desired []string
	seen := make(map[string]bool)
	for _, name := range installed {
		if seen[name] {
			continue
		}
		seen[name] = true
		if !ValidPackageName(name) {
			slog.Warn("holds: skipping package with unsafe name", "package", name)
			continue
		}
		if ClassifyPackage(name) == "" {
			continue
		}
		desired = append(desired, name)
	}
	sort.Strings(desired)
	return desired
}

// reconcileAptLocked converges apt-mark holds (AD-004). The showhold
// pre-check keeps operator-created holds invisible to rIOt: a package
// already held but not in our state is never recorded, so disable never
// touches it (BR-004). Returns false when a command failed.
func (hm *HoldManager) reconcileAptLocked(ctx context.Context, st *holdState, desired []string) bool {
	out, err := hm.run(ctx, hm.aptMarkPath, "showhold") // unprivileged read
	if err != nil {
		slog.Warn("holds: apt-mark showhold failed", "error", err, "output", strings.TrimSpace(string(out)))
		return false
	}
	heldNow := make(map[string]bool)
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			heldNow[line] = true
		}
	}

	ours := make(map[string]bool, len(st.AptHolds))
	for _, p := range st.AptHolds {
		ours[p] = true
	}

	var toHold, newOurs []string
	desiredSet := make(map[string]bool, len(desired))
	for _, p := range desired {
		desiredSet[p] = true
		switch {
		case ours[p]:
			newOurs = append(newOurs, p)
			if !heldNow[p] {
				toHold = append(toHold, p) // ours but drifted (e.g. manual unhold, interrupted run)
			}
		case heldNow[p]:
			// Operator hold — leave alone, never record (BR-004).
		default:
			toHold = append(toHold, p)
			newOurs = append(newOurs, p)
		}
	}

	var toUnhold []string
	for _, p := range st.AptHolds {
		if !desiredSet[p] {
			toUnhold = append(toUnhold, p) // ours, no longer installed (FR-012)
		}
	}

	if len(toHold) > 0 {
		args := append([]string{"-n", hm.aptMarkPath, "hold"}, toHold...)
		if out, err := hm.run(ctx, "sudo", args...); err != nil {
			slog.Warn("holds: apt-mark hold failed", "packages", toHold, "error", err, "output", strings.TrimSpace(string(out)))
			return false
		}
		slog.Info("holds: held reboot-class packages", "packages", toHold)
	}
	if len(toUnhold) > 0 {
		args := append([]string{"-n", hm.aptMarkPath, "unhold"}, toUnhold...)
		if out, err := hm.run(ctx, "sudo", args...); err != nil {
			slog.Warn("holds: apt-mark unhold failed", "packages", toUnhold, "error", err, "output", strings.TrimSpace(string(out)))
			return false
		}
		slog.Info("holds: released holds for packages no longer installed", "packages", toUnhold)
	}

	sort.Strings(newOurs)
	st.AptHolds = newOurs
	st.DnfExcludes = nil
	return true
}

// reconcileDnfLocked converges the rIOt-owned excludepkgs fragment via the
// fixed-path staged install (AD-005, SEC-PATCH-GATE-002): content is staged
// unprivileged under /var/lib/riot, then placed by the argument-locked
// `sudo install` rule. Steady state is an unprivileged read + compare.
// Returns false when a command failed.
func (hm *HoldManager) reconcileDnfLocked(ctx context.Context, st *holdState, desired []string) bool {
	current, fragmentExists := readFragmentExcludes(hm.FragmentPath)

	if len(desired) == 0 {
		if fragmentExists {
			if out, err := hm.run(ctx, "sudo", "-n", hm.rmPath, "-f", hm.FragmentPath); err != nil {
				slog.Warn("holds: fragment delete failed", "error", err, "output", strings.TrimSpace(string(out)))
				return false
			}
			slog.Info("holds: removed dnf excludes fragment (no reboot-class packages installed)")
		}
		st.DnfExcludes = nil
		st.AptHolds = nil
		return true
	}

	if !stringSlicesEqual(current, desired) {
		if err := writeFileAtomic(hm.StagedPath, renderDNFFragment(desired), 0600); err != nil {
			slog.Warn("holds: failed to stage dnf fragment", "error", err)
			return false
		}
		if out, err := hm.run(ctx, "sudo", "-n", hm.installPath,
			"-m", "0644", "-o", "root", "-g", "root", hm.StagedPath, hm.FragmentPath); err != nil {
			slog.Warn("holds: fragment install failed", "error", err, "output", strings.TrimSpace(string(out)))
			return false
		}
		slog.Info("holds: wrote dnf excludes fragment", "packages", desired)
	}

	st.DnfExcludes = desired
	st.AptHolds = nil
	return true
}

// disableCleanupLocked removes every rIOt-managed hold and the dnf fragment
// when the feature has been disabled (FR-014). Operator holds are untouched
// — only state-recorded names are released (BR-004, AC-011). Deliberately
// does NOT apply the reboot-class cross-check: disable must remove every
// hold rIOt recorded even if the pattern table has since changed (AD-016).
func (hm *HoldManager) disableCleanupLocked(ctx context.Context, st *holdState) {
	if len(st.AptHolds) > 0 {
		valid := st.AptHolds[:0:0]
		for _, p := range st.AptHolds {
			if ValidPackageName(p) {
				valid = append(valid, p)
			} else {
				slog.Warn("holds: skipping unsafe name during disable cleanup", "package", p)
			}
		}
		if len(valid) > 0 {
			args := append([]string{"-n", hm.aptMarkPath, "unhold"}, valid...)
			if out, err := hm.run(ctx, "sudo", args...); err != nil {
				slog.Warn("holds: disable cleanup unhold failed; will retry next cycle", "error", err, "output", strings.TrimSpace(string(out)))
				return
			}
		}
		slog.Info("holds: hold enforcement disabled, released rIOt-managed holds", "packages", valid)
	}
	if len(st.DnfExcludes) > 0 || st.PM == "dnf" {
		if _, exists := readFragmentExcludes(hm.FragmentPath); exists {
			if out, err := hm.run(ctx, "sudo", "-n", hm.rmPath, "-f", hm.FragmentPath); err != nil {
				slog.Warn("holds: disable cleanup fragment delete failed; will retry next cycle", "error", err, "output", strings.TrimSpace(string(out)))
				return
			}
			slog.Info("holds: hold enforcement disabled, removed dnf excludes fragment")
		}
	}
	if err := os.Remove(hm.StatePath); err != nil && !errors.Is(err, os.ErrNotExist) {
		slog.Warn("holds: failed to remove state file on disable", "error", err)
	}
}

// setStatusLocked updates the enforcement status.
func (hm *HoldManager) setStatusLocked(s string) {
	hm.status = s
}

// dnf5Supported reports whether the host's dnf supports libdnf5 conf.d
// drop-ins (major version >= 5) — AD-005 dnf4 safe-degrade.
func (hm *HoldManager) dnf5Supported(ctx context.Context) bool {
	out, err := hm.run(ctx, "dnf", "--version")
	if err != nil {
		return false
	}
	return dnfMajorVersion(string(out)) >= 5
}

// dnfMajorVersion extracts the major version from `dnf --version` output.
// dnf4 prints "4.14.0…" (first digit run = 4); dnf5 prints
// "dnf5 version 5.x…" (first digit run is the 5 in "dnf5") — the first
// digit run in the output is the major version in both formats.
func dnfMajorVersion(output string) int {
	for i := 0; i < len(output); i++ {
		if output[i] >= '0' && output[i] <= '9' {
			major := 0
			for i < len(output) && output[i] >= '0' && output[i] <= '9' {
				major = major*10 + int(output[i]-'0')
				i++
			}
			return major
		}
	}
	return 0
}

// loadState reads the state file; a missing file yields an empty state and
// no error, while a parse error is surfaced so release paths can abort
// (SEC-PATCH-GATE-005).
func (hm *HoldManager) loadState() (*holdState, error) {
	data, err := os.ReadFile(hm.StatePath)
	if errors.Is(err, os.ErrNotExist) {
		return &holdState{Version: 1}, nil
	}
	if err != nil {
		return nil, err
	}
	var st holdState
	if err := json.Unmarshal(data, &st); err != nil {
		return nil, fmt.Errorf("parse %s: %w", hm.StatePath, err)
	}
	return &st, nil
}

// saveState writes the state file atomically (temp + rename), 0600.
func (hm *HoldManager) saveState(st *holdState) error {
	st.Version = 1
	st.UpdatedAt = time.Now().UTC()
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(hm.StatePath, string(data), 0600)
}

// listInstalled gathers installed package names via the package manager.
// Used only by the startup and post-run reapply paths — the per-cycle
// reconcile reuses the names the updates collector already parsed (NFR-004).
func (hm *HoldManager) listInstalled(ctx context.Context, pm string) ([]string, error) {
	if pm == "dnf" {
		out, err := hm.run(ctx, "rpm", "-qa", "--qf", "%{NAME}\n")
		if err != nil {
			return nil, err
		}
		return nonEmptyLines(string(out)), nil
	}
	out, err := hm.run(ctx, "dpkg", "--get-selections")
	if err != nil {
		return nil, err
	}
	return parseDpkgSelections(string(out)), nil
}

// parseDpkgSelections extracts installed package names (arch qualifiers
// stripped) from `dpkg --get-selections` output, keeping only "install"
// selections.
func parseDpkgSelections(output string) []string {
	var names []string
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 || fields[1] != "install" {
			continue
		}
		name := fields[0]
		if idx := strings.Index(name, ":"); idx > 0 {
			name = name[:idx]
		}
		names = append(names, name)
	}
	return names
}

func nonEmptyLines(s string) []string {
	var lines []string
	for _, line := range strings.Split(strings.TrimSpace(s), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

// renderDNFFragment renders the full fragment content for the desired
// exclude set. Regenerated from scratch on every drift — idempotent by
// construction (NFR-003).
func renderDNFFragment(excludes []string) string {
	return "# Managed by riot-agent (PATCH-GATE). Do not edit — regenerated every cycle.\n" +
		"[main]\n" +
		"excludepkgs=" + strings.Join(excludes, ",") + "\n"
}

// readFragmentExcludes reads the current fragment's exclude list (sorted).
// Returns exists=false when the fragment is absent.
func readFragmentExcludes(path string) ([]string, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "excludepkgs=") {
			raw := strings.TrimPrefix(line, "excludepkgs=")
			var names []string
			for _, n := range strings.Split(raw, ",") {
				if n = strings.TrimSpace(n); n != "" {
					names = append(names, n)
				}
			}
			sort.Strings(names)
			return names, true
		}
	}
	return nil, true
}

func stringSlicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// writeFileAtomic writes data to path via a temp file + rename in the same
// directory so readers never observe a partial file.
func writeFileAtomic(path, data string, perm os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.WriteString(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	if err := os.Chmod(tmpName, perm); err != nil {
		os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		os.Remove(tmpName)
		return err
	}
	return nil
}
