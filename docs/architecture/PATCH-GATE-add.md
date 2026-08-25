# Architecture Decision Document

- **Story ID:** PATCH-GATE
- **FRD Reference:** `docs/requirements/PATCH-GATE-frd.md`
- **Author:** Architect Agent
- **Date:** 2026-08-25
- **Status:** FINAL

---

## 1. Summary

Add a shared reboot-class package classifier (`gpu_driver` / `kernel`) to the
updates collector, driven by a single pattern table used by both the apt and
dnf paths. When the two-sided opt-in is active (server-side
`AutomationConfig.OSPatch.RebootClass: "gated"` **and** agent-side
`commands.hold_reboot_class: true`), a new `HoldManager` keeps every installed
reboot-class package held at the OS level — `apt-mark hold` on apt,
a rIOt-owned `excludepkgs` drop-in fragment at
`/etc/dnf/libdnf5.conf.d/60-riot-holds.conf` on dnf5 — reconciled once per
telemetry cycle with crash recovery via a `released_for_command` marker in a
state file at `/var/lib/riot/holds.json`. The `os_update` command gains an
`include_reboot_class` boolean: only in-window automated dispatches carry it,
and the agent releases its own holds for exactly the duration of that run,
re-applying them on **every** exit path via `defer`. A run that actually
upgraded reboot-class packages ends in an automatic reboot when
`commands.allow_reboot: true`, otherwise in a reboot-required event. The story
also populates the dead `PendingKernelUpdate`/`PendingKernelVersion` fields
(un-breaking the scoring engine's silent pass), adds reboot-required detection
(`/var/run/reboot-required` on apt, `dnf needs-restarting -r` on dnf), flags
GPU-dependent containers from `HostConfig.DeviceRequests`/`Devices` inside the
existing inspect loop, and surfaces all of it in the dashboard. No database
migration: telemetry fields ride the existing JSONB blob, the policy toggle
rides the `admin_config` JSON blob.

Per the security review (`docs/security/PATCH-GATE-security-review.md`,
SEC-PATCH-GATE-001/-002): the agent runs as the unprivileged `riot` user, so
this story **widens the sudoers allowlist** in `scripts/install.sh` with
exact, fixed-path rules (`apt-mark hold/unhold`; a fixed-source/fixed-dest
`install` rule for the dnf fragment plus a fixed-path `rm`). The agent
**preflight-verifies** those privileges (`sudo -n -l` probes) before
enforcing holds and reports a `hold_enforcement` status in telemetry —
fail-closed and operator-visible: an enabled-but-unprivileged host shows
"enforcement inactive", never a silent no-op.

---

## 2. Technical Context

### Existing machinery we compose (verified on `main`)

- **Updates collector** — `internal/agent/collectors/updates.go`. apt path
  parses `apt list --upgradable` (`collectAPT`, lines 39–78) and already runs
  `dpkg --get-selections` for the installed count; dnf path parses
  `dnf check-update -q` (`collectDNF` + `parseDNFCheckUpdate`, lines 80–139)
  and already runs `rpm -qa` for the installed count. Classification can be
  pure string matching on names both paths already extract (NFR-004, A-003).
- **Models** — `internal/models/telemetry.go`: `UpdateInfo` (lines 257–267)
  carries `PendingKernelUpdate` / `PendingKernelVersion` which **nothing
  populates**; `PendingUpdate` (lines 269–274) has `Name/CurrentVer/NewVer/
  IsSecurity`; `ContainerInfo` (lines 317–341) is the container model.
- **Scoring engine** — `internal/server/scoring/engine.go:201–220`: the
  kernel finding logic is already correct (`noKernel := !upd.PendingKernelUpdate`);
  the silent false pass is purely a data problem. FR-027 is fixed agent-side.
- **Maintenance windows** — `internal/models/automation.go`
  (`AutomationConfig{OSPatch, DockerUpdate MaintenanceWindow}`,
  `DefaultAutomationConfig()` ships both `disabled`); evaluation in
  `internal/server/handlers/auto_update.go` (`inMaintenanceWindow` :171–197,
  `frequencyAllowsDay` :204–224, validation in `SetAutomationConfig` :92–133,
  load via `loadAutomationConfig` :151–168 from the `automation_config` key in
  the `admin_config` blob). `checkAutoPatch` (:445–507) dispatches
  `os_update` with `Params: {"mode": "full"}` when in window, with cooldown
  dedup against recent commands. Reused unchanged per A-007.
- **Command dispatch** — server allowlist `internal/server/handlers/commands.go:15–29`
  (`os_update` already allowed — **no allowlist change needed**); WS dispatch
  with queued fallback; manual bulk path `internal/server/handlers/fleet.go`
  (`BulkPatchDevices` :151–238). Agent switch `internal/agent/commands.go:51–86`;
  `handleOSUpdateWithOutput` :186–267 (gated `commands.allow_patching`,
  detached 30-min context, `sudo apt-get dist-upgrade` / `sudo dnf update`,
  summary via `parseAptSummary`/`parseDnfSummary` — note `aptHeldRe` already
  counts "not upgraded", i.e. held packages surface in the summary today);
  `handleReboot` :148–164 (gated `commands.allow_reboot`,
  `sudo systemctl reboot`). Post-result restart precedent: `handleTriggerUpdate`
  sends the result first, then acts after a 1 s `go func` delay (:652–656).
- **Agent config** — `internal/agent/config.go`: `CommandsConfig` (:31–36)
  holds the `allow_*` opt-ins; `defaultConfigTemplate` (:146–199) is the
  user-facing commented YAML; path helpers precedent (`BufferPath()` →
  `/var/lib/riot/...`).
- **Privilege boundary** — the agent runs as the unprivileged `riot` system
  user (`scripts/install.sh` systemd unit, `User=riot`) and reaches root only
  through the exact, argument-matched sudoers allowlist written by
  `install.sh:424–476` to `/etc/sudoers.d/riot-agent`: `apt-get
  update/upgrade/dist-upgrade` (locked flags), `dnf makecache/update/
  --security update`, `systemctl reboot`, the agent-update lines, `nginx
  -t/-T`, `smartctl`. **The FRD's A-002 ("the agent runs with sufficient
  privilege… as it already does") is false for this story's new operations**:
  there is no rule for `apt-mark`, and the `riot` user cannot write the
  root-owned `/etc/dnf/libdnf5.conf.d/` directory
  (SEC-PATCH-GATE-001). This ADD therefore treats the privilege widening as
  a first-class design decision (AD-015) and adds `scripts/install.sh` to the
  change set. `dnf needs-restarting -r` reads `/proc` and the dnf history DB
  and runs unprivileged — it gets **no** sudo rule; if it fails, detection
  degrades to `false` (AD-010, per the review's SEC-001 mitigation 2).
- **In-place upgrades skip the installer** — `handleTriggerUpdate`
  (`commands.go:603–659`) replaces the binary and restarts; it never re-runs
  `install.sh`, so upgraded fleets will not have the new sudoers rules until
  the installer is re-run (SEC-PATCH-GATE-004). The runtime preflight
  (AD-015) makes that state visible instead of silently unenforced.
- **Agent wiring** — `internal/agent/agent.go:61–86`: `New()` builds the
  registry (`RegisterDefaultsWithDocker`) and configures collectors via
  registry setters (`SetSMARTInterval`, `SetDockerCachePath`,
  `SetNginxAccessLog` precedents in `collectors/collector.go:106–137`).
  `Agent.triggerTelemetry()` (`docker_update.go:437`) forces an immediate
  telemetry push — reused after a patch run so reboot-required state reaches
  the server promptly.
- **Docker collector** — `internal/agent/collectors/docker.go`: exactly two
  `ContainerInspect` call sites — stats phase (:301, running containers) and
  network-mode phase (:354, everything the stats phase didn't inspect). Both
  already read `inspect.HostConfig`; adding `DeviceRequests`/`Devices` reads
  costs zero extra API calls (Section 11).
- **Events** — `internal/server/events/generator.go`:
  `CheckTelemetryThresholds` (:348–406) fans out to per-subsystem checks;
  `CheckUPSAlerts` (:558–652) is the established **state-transition** pattern
  (a `deviceID:ups_was_on_battery` key in the `lastSent` map, refreshed every
  cycle while the state holds, deleted on clear → exactly-once-per-transition
  events). `pruneStaleEntries` (:86–107) drops `lastSent` entries older than
  1 h — safe for keys refreshed every 60 s telemetry cycle. Templates in
  `events/templates.go`; seeded rules in `server.go:646–670`; event type
  constants in `internal/models/events.go`.
- **Telemetry ingest** — `internal/server/handlers/handlers.go:452–469`:
  `CheckTelemetryThresholds` → `checkAutoUpdates` → `checkAutoPatch`, in that
  order, per snapshot.
- **Frontend** — `web/src/pages/DeviceDetail.tsx` Pending Updates table
  (:831–854, columns Package/New Version/Security); reboot button flow
  :263–275. Fleet patch panel `web/src/pages/FleetOverview.tsx:380–460` fed by
  `GET /api/v1/fleet/patch-status` (`internal/server/handlers/fleet.go:12–57`,
  `devicePatchInfo` response struct). Container tiles
  `web/src/components/CompactContainerTile.tsx` (used by
  `web/src/pages/DeviceContainers.tsx`). Maintenance-window editor
  `web/src/pages/settings/AgentManagement.tsx` (:234–260, `updateWindow('os_patch', …)`).
  TS mirrors in `web/src/types/models.ts` (`UpdateInfo` :206, `PendingUpdate`
  :218, `ContainerInfo` :262, `MaintenanceWindow` :721). Demo data in
  `web/src/api/demo-data.ts` (`getPatchStatus()` :692).

### What is missing

- No package classification, no `Class` field, no reboot-class count.
- Nothing populates `PendingKernelUpdate`/`PendingKernelVersion`.
- No hold enforcement anywhere; no state tracking of rIOt-created holds.
- No reboot-required detection; no `reboot_required` event type or template.
- `os_update` has only a `mode` param; the orchestrator has no reboot-class
  policy knob; `MaintenanceWindow` has no `RebootClass` field.
- Docker collector never reads `HostConfig.DeviceRequests`/`Devices`;
  `ContainerInfo` has no GPU flag.
- UI shows no classification badges, holds, reboot-required state, or GPU
  container counts.
- The sudoers allowlist grants none of the new privileged operations
  (`apt-mark`, dnf fragment write/delete); no preflight or
  enforcement-status mechanism exists.

---

## 3. Architecture Decisions

### AD-001: One shared pattern table in `internal/agent/collectors/rebootclass.go`

**Decision.** Create `rebootclass.go` in package `collectors` exporting:

```go
const (
    ClassGPUDriver = "gpu_driver"
    ClassKernel    = "kernel"
)

// ClassifyPackage returns ClassGPUDriver, ClassKernel, or "" (standard).
func ClassifyPackage(name string) string
```

One ordered rule table drives both package managers (FR-007):

1. **Exclusions first** (explicit, tested — NFR-006): exact `linux-firmware`;
   prefix `libnvidia-container` (the container-toolkit user-space:
   `libnvidia-container1`, `libnvidia-container-tools`); prefix
   `nvidia-container-toolkit`; exact `nvidia-docker2`. These match GPU-looking
   prefixes but are *not* driver packages — upgrading them creates no
   kernel/user-space mismatch and requires no reboot. → `""`.
2. **GPU driver patterns** (FR-002, FR-003 — prefix matches unless noted):
   `nvidia-driver`, `nvidia-dkms`, `nvidia-kernel`, `libnvidia-`,
   `nvidia-utils`, `xserver-xorg-video-nvidia`, `akmod-nvidia`, `kmod-nvidia`,
   `xorg-x11-drv-nvidia`, `nvidia-kmod`, `amdgpu` (prefix — covers
   `amdgpu-dkms`, `amdgpu-pro`, dnf `amdgpu` stack), exact `rock-dkms`, exact
   `rocm-dkms`, prefix `rocm-dkms`, and any `rocm*` name that also ends in
   `-dkms` or contains `kmod` (ROCm kernel-module packages; plain `rocm-*`
   user-space libs stay standard). → `ClassGPUDriver`.
3. **Kernel patterns** (FR-004, FR-005): prefix `linux-image-`,
   `linux-headers-`, `linux-modules-`, `linux-generic` (covers flavor
   metapackages), exact `linux-image-generic` (already covered by prefix, kept
   in tests); exact `kernel`, `kernel-core`, `kernel-headers`, `kernel-devel`,
   prefix `kernel-modules`; **suffix** `-dkms` for anything not already
   matched by rule 2. → `ClassKernel`.

Rule order gives GPU precedence over kernel for free: `nvidia-dkms-550`
matches rule 2 before the `-dkms` suffix rule (FR-006, AC-001).

**Rationale.** FR-007 mandates a single table so apt and dnf cannot drift.
Package collectors is the right package: both the updates collector (pending
+ installed classification) and the hold manager (AD-003) consume it, and it
keeps the function unit-testable with zero OS interaction.

**Alternatives considered.**
- *Two per-package-manager tables with shared helpers.* Rejected — exactly
  the drift FR-007 forbids.
- *Regex table.* Rejected — prefix/exact/suffix matching covers every FRD
  pattern; regexes add compile cost on a hot path and reviewer burden.
- *Classifying `libnvidia-container*` as `gpu_driver` (literal FR-002
  `libnvidia-*` reading).* Rejected — NFR-006 explicitly demands this be a
  deliberate choice: the container toolkit is user-space plumbing for Docker,
  not part of the driver ABI pair; holding it would block routine toolkit
  fixes for no safety gain. Exclusion documented and tested (AC-005).

**Consequences.** `linux-firmware` and ROCm user-space libraries remain
standard (upgradeable any time). A future Intel GPU story only touches this
table.

---

### AD-002: Primary kernel package selection for `PendingKernelVersion`

**Decision.** `rebootclass.go` also exports
`SelectPrimaryKernel(updates []models.PendingUpdate) (name, version string)`.
Among pending updates classified `ClassKernel`, pick by rank:

| Rank | apt | dnf |
|------|-----|-----|
| 0 | `linux-image-*` (non-meta, i.e. contains a digit) | exact `kernel` |
| 1 | `linux-image-generic*` / `linux-generic*` meta | `kernel-core` |
| 2 | any other kernel-class package | any other kernel-class package |

Lowest rank wins; ties broken by lexicographically smallest name
(deterministic). The winner's `NewVer` becomes
`UpdateInfo.PendingKernelVersion`; `PendingKernelUpdate = true` iff at least
one kernel-class pending update exists (FR-008, AC-006). Both fields are
reset/empty otherwise.

**Rationale.** The versioned `linux-image-6.8.0-45-generic` / `kernel` package
carries the actual new kernel version string operators recognize; metapackages
and headers are noise. A fixed rank table makes the choice testable and
stable across cycles.

**Alternatives considered.** *First kernel package encountered* — rejected,
parse order is package-manager-output order, not deterministic across runs.

**Consequences.** The scoring engine (`engine.go:201–220`) needs **no code
change** — see AD-013.

---

### AD-003: `HoldManager` in `internal/agent/collectors/holds.go`, state file at `/var/lib/riot/holds.json`

**Decision.** New type owned by the agent, shared by two consumers:

```go
type HoldManager struct {
    mu        sync.Mutex
    Enabled   bool          // from commands.hold_reboot_class
    StatePath string        // HoldStatePath() = /var/lib/riot/holds.json
    run       CommandRunner // injectable for tests; default wraps exec.CommandContext
}
```

`Agent.New()` constructs it, passes it to the updates collector via a new
registry setter `Registry.SetHoldManager(hm *HoldManager)` (mirrors
`SetDockerCachePath`), and keeps the same pointer on the `Agent` struct for
the `os_update` command path. All public methods take the mutex — the
telemetry loop and the command handler run in different goroutines.

State file (JSON, 0600, written atomically via temp-file + rename):

```json
{
  "version": 1,
  "pm": "apt",
  "apt_holds": ["nvidia-driver-550", "linux-image-generic"],
  "dnf_excludes": [],
  "released_for_command": "",
  "updated_at": "2026-08-25T02:00:00Z"
}
```

Only packages **rIOt itself held** appear in `apt_holds`/`dnf_excludes`
(FR-013, BR-004). `released_for_command` is the crash-recovery marker
(AD-007).

**Rationale.** A state file is the only bookkeeping that survives agent
restarts and distinguishes rIOt-managed holds from operator holds — deriving
"ours" from the pattern table alone would release an operator's pre-existing
`apt-mark hold nvidia-driver-550` on disable, violating BR-004/AC-011.
`/var/lib/riot/` is the established mutable-state location (`BufferPath()`),
and `performUninstall` already removes it. Placing the manager in package
`collectors` follows the shared-component precedent (`authFailureCounter`)
and lets the updates collector report `HeldPackages` without an import cycle.

**Alternatives considered.**
- *Derive managed-set from pattern table each run (no state file).* Rejected —
  cannot honor BR-004 (see above) and loses crash-recovery state.
- *Manager in package `agent` with a callback into the collector.* Rejected —
  the collector needs held-package data every cycle; inverted dependency is
  clumsier than the existing setter pattern.
- *Store state in agent.yaml.* Rejected — config is user-owned input, not
  agent-mutable state; `Config.Save` would clobber user comments.

**Consequences.** One small new file under `/var/lib/riot/`. Windows/macOS:
entire feature is inert behind the existing `runtime.GOOS != "linux"` guard.

---

### AD-004: apt mechanism — `apt-mark`, operator holds untouched

**Decision.** Reconciliation on apt (FR-011–FR-014):

1. `apt-mark showhold` → set of currently-held packages.
2. Installed reboot-class set = `ClassifyPackage != ""` over installed
   package names (reused from the `dpkg --get-selections` output the
   collector already has — no new invocation; see AD-010 note).
3. **Add:** for each installed reboot-class package neither in `showhold` nor
   in `apt_holds`: `apt-mark hold <pkg>`, then record in `apt_holds`. A
   package already in `showhold` but *not* in `apt_holds` is an operator
   hold — leave it alone and do **not** record it (so disable never touches
   it).
4. **Remove:** for each `apt_holds` entry no longer installed:
   `apt-mark unhold <pkg>`, drop from state (FR-012 cleanup).
5. **Disable path** (`Enabled == false` but state file has entries):
   `apt-mark unhold` every `apt_holds` entry, clear state (FR-014, AC-011).

Privilege mechanics (SEC-PATCH-GATE-001/-002): `apt-mark showhold` reads the
world-readable dpkg database and runs **unprivileged** (no sudo). `apt-mark
hold`/`unhold` require root and run via `sudo -n` under the two new
argument-locked sudoers rules specified in AD-015 — batched (single
invocation with all package names) and idempotent (holding a held package is
a no-op, NFR-003). Every name is charset-validated per AD-016 before it
reaches argv, and reconciliation only mutates at all after the AD-015
preflight has confirmed the sudo rules exist (fail-closed otherwise).

**Rationale.** `apt-mark hold` sets the dpkg selection state that `apt
upgrade`, `apt dist-upgrade`, and `unattended-upgrades` all respect by
default (A-001) — exactly the US-1 enforcement. The `showhold` pre-check is
what makes BR-004 airtight.

**Alternatives considered.** *dpkg `--set-selections hold` directly* —
rejected, `apt-mark` is the porcelain and matches how operators inspect state.
*apt preferences pinning (`Pin-Priority: -1`)* — rejected, writes a config
fragment apt tooling doesn't surface via `showhold`, and confuses operators
who check holds the normal way.

**Consequences.** `apt upgrade` on the host reports held packages as "not
upgraded" — the existing `aptHeldRe` summary already surfaces this count in
os_update results with no change.

---

### AD-005: dnf mechanism — rIOt-owned drop-in fragment; dnf4 degrades safely

**Decision.** On dnf systems the hold set is written as a dedicated fragment:

```
/etc/dnf/libdnf5.conf.d/60-riot-holds.conf
```

```ini
# Managed by riot-agent (PATCH-GATE). Do not edit — regenerated every cycle.
[main]
excludepkgs=akmod-nvidia,kernel-core,...
```

The `riot` user cannot write the root-owned `/etc/dnf/libdnf5.conf.d/`
directory (SEC-PATCH-GATE-001), so the write is a **fixed-path staged
install** (SEC-PATCH-GATE-002 — no `sh -c`, no `tee`, no wildcard paths, and
explicitly **not** the un-allowlisted `sudo tee` pattern at
`internal/agent/commands.go:407`):

1. The agent renders the full fragment content to the staging file
   `/var/lib/riot/dnf-holds.staged` (riot-owned, 0600, atomic temp+rename
   within `/var/lib/riot/` — unprivileged).
2. `sudo -n /usr/bin/install -m 0644 -o root -g root
   /var/lib/riot/dnf-holds.staged /etc/dnf/libdnf5.conf.d/60-riot-holds.conf`
   — a sudoers rule with **both paths fixed** (AD-015): the privileged
   operation has zero variable arguments; the `riot` user can place content
   into exactly one root-owned file, ever.
3. Delete-on-disable/empty: `sudo -n /usr/bin/rm -f
   /etc/dnf/libdnf5.conf.d/60-riot-holds.conf` — likewise an exact-path rule.

The fragment is regenerated from the full desired set on every drift
(idempotent by construction — NFR-003; steady state is an unprivileged read
+ compare of the fragment, no sudo). Disable or empty set ⇒ the fragment is
**deleted**, never left empty. No user-owned file (`/etc/dnf/dnf.conf` or
anything else) is ever read-modified-written (FR-011, AC-009). Residual:
`install(1)` copies rather than renames, so a dnf process reading the
fragment during the copy could momentarily see a truncated exclude list —
a sub-millisecond window on a <1 KB file, same bounded-exposure class the
review accepted in SEC-PATCH-GATE-006, and strictly better than the
root-write primitives a variable-path rule would create.

libdnf5 conf.d support exists only in dnf5 (Fedora 41+, RHEL 10+). The
manager detects the major version via `dnf --version` once per reconcile; on
dnf4 hosts hold enforcement is **unsupported**: log one WARN per agent
process lifetime ("OS-level dnf holds require dnf5; hold enforcement inactive
on this host"), report `HeldPackages` as empty, and never write anywhere.
Classification, reboot-required detection, and the in-window orchestration
still work on dnf4 — only the OS-level lock is absent.

**Rationale.** The design mandate is a rIOt-managed `excludepkgs` fragment,
and a conf.d drop-in is the only mechanism that satisfies "never edit
user-owned files" — dnf4 simply has no main-config drop-in directory.
Degrading to *honestly unsupported* (empty `HeldPackages`, visible warning)
beats silently claiming protection that isn't enforced.

**Alternatives considered.**
- *Edit `/etc/dnf/dnf.conf` `excludepkgs`.* Rejected — user-owned file;
  explicitly forbidden by the FRD.
- *`dnf versionlock` plugin for dnf4.* Rejected — requires
  `python3-dnf-plugin-versionlock` to be installed (conditional behavior,
  effectively a new dependency against D-007's spirit), and locks to exact
  versions, which complicates release/re-apply around a run.
- *Per-repo `exclude=` in `.repo` files.* Rejected — those files are
  user/vendor-owned and per-repo excludes don't cover all repos.
- *`sudo tee <path>` or `sudo sh -c` for the fragment write (the
  `commands.go:407` pattern).* Rejected — SEC-PATCH-GATE-002: a
  wildcard/variable-path or shell sudoers rule is an
  arbitrary-root-file-write primitive (local root escalation for anything
  running as `riot`). The existing `enable_auto_updates` tee is a latent
  over-grant, not precedent.

**Consequences.** dnf4 homelab hosts get classification + visibility but not
OS-level locking until they move to dnf5. Documented in README (§12) and
visible in the agent log.

---

### AD-006: Agent-side opt-in flag — `commands.hold_reboot_class`

**Decision.** Add to `CommandsConfig` (`internal/agent/config.go:31–36`):

```go
HoldRebootClass bool `yaml:"hold_reboot_class"` // opt-in: hold GPU-driver/kernel packages outside maintenance windows
```

Default `false` (Go zero value — BR-006). Add a commented line to
`defaultConfigTemplate` under `commands:`:

```yaml
  hold_reboot_class: false # Hold GPU driver + kernel packages at the OS level (release only during rIOt maintenance-window patch runs)
```

**Rationale.** FR-010 delegates the name; `commands.` is where every other
system-mutating opt-in lives (`allow_reboot`, `allow_patching`), and hold
enforcement is the same trust category. Disabling = edit YAML + restart
agent, the established config-change flow (FR-014's "restart/reload").

**Alternatives considered.** *Under `collectors:`* — rejected; it mutates the
OS, it is not a collection setting.

---

### AD-007: Reconciliation every telemetry cycle + crash recovery via `released_for_command`

**Decision.** `HoldManager.Reconcile(ctx)` runs at the **start** of every
`UpdatesCollector.Collect()` (default 60 s cadence — satisfies FR-012's
"at least once per telemetry cycle"). Sequence:

1. Load state file (missing file = empty state).
2. **Crash recovery:** if `released_for_command != ""`, a previous
   `os_update` run died between release and re-apply (agent crash, OOM,
   power loss mid-run). Log WARN with the stale command ID, treat as normal
   drift, continue — step 4 re-asserts everything; clear the marker on
   successful re-assert.
3. If `Enabled == false`: run the disable path (AD-004 step 5 / AD-005
   delete-fragment) if state is non-empty; return.
4. If `Enabled == true`: converge per AD-004/AD-005; persist state.

Additionally, `Reconcile` runs once at agent startup (from `Agent.Run`) so a
crashed-mid-run host is re-protected within seconds of the agent restarting,
not up to one poll interval later.

Reconcile failures (command error, unwritable state file) log WARN and leave
the previous state; the rest of `Collect()` proceeds (NFR-005).

**Rationale.** The dangerous failure mode is "agent crashes while holds are
released": the host is unprotected until something re-asserts. The state-file
marker makes the exposure window explicit (from crash until next agent start
+ first reconcile) and auditable (WARN log). Piggybacking on the collector
cycle adds no new goroutine or timer and naturally feeds `HeldPackages`
telemetry (FR-016) from the same pass.

**Alternatives considered.** *Dedicated reconcile goroutine on its own
timer* — rejected, a second cadence to reason about with no benefit at 60 s
granularity. *systemd ExecStopPost hook to re-hold* — rejected, doesn't cover
SIGKILL/power loss and adds packaging surface.

**Consequences.** Worst-case unprotected window after a mid-run crash =
agent downtime + ≤1 cycle. Acceptable: the same crash also interrupted the
package manager itself, and the window is logged.

---

### AD-008: `os_update` gains `include_reboot_class`; release scoped by `defer`; applied-detection by before/after version snapshot

**Decision.** `handleOSUpdateWithOutput` (`internal/agent/commands.go:186–267`)
changes:

1. Parse `includeRC, _ := payload.Params["include_reboot_class"].(bool)`
   (absent ⇒ `false` — FR-020 validation rule).
2. `releaseNeeded := includeRC && a.holdMgr.Enabled && len(managedHolds) > 0`.
   When `false`, behavior is byte-for-byte today's (AC-023): holds (if any)
   simply cause the package manager to skip those packages.
3. When `releaseNeeded`:
   a. `released := a.holdMgr.ReleaseForRun(ctx, payload.CommandID)` —
      unholds/removes-from-fragment **only** the rIOt-managed set, writes
      `released_for_command = <id>` to the state file *before* releasing
      (NFR-001, crash marker ordering). Two fail-closed guards
      (SEC-PATCH-GATE-005): a state-file load/parse error **aborts the
      release entirely** (nothing unheld; the run proceeds with holds in
      place and the result message says so — an empty-on-error state is
      never interpreted as "release all"); and each candidate name is
      unheld only if it **both** appears in `apt_holds`/`dnf_excludes`
      **and** currently classifies reboot-class via `ClassifyPackage`
      (defense-in-depth against a poisoned state file listing an
      operator-critical package like `postgresql-16`) — anything else is
      skipped with a WARN.
   b. `before := installedVersions(ctx, released)` — one
      `dpkg-query -W -f='${Package} ${Version}\n' pkgs...` (apt) or
      `rpm -q --qf '%{NAME} %{VERSION}-%{RELEASE}\n' pkgs...` (dnf).
   c. `defer a.holdMgr.ReapplyAfterRun(context.Background(), payload.CommandID)`
      — registered immediately after release, uses a fresh background
      context (the run context may already be cancelled/expired on failure
      paths), reconciles holds against the *post-run* installed set, clears
      the marker. This single `defer` covers success, refresh failure,
      upgrade failure, partial apply, and panic (FR-015, AC-010).
4. Refresh + upgrade run exactly as today.
5. After a successful upgrade: `after := installedVersions(ctx, released)`;
   `applied := packages whose version changed` — the reboot-class packages
   actually upgraded (FR-024/FR-025 need *actually upgraded*, not *pending*).
6. Result message (FR-026) is extended, e.g.
   `"Updated 14 packages (reboot-class applied: nvidia-driver-550, linux-image-6.8.0-45-generic; reboot initiated)"`
   or `"...; reboot required but not permitted (commands.allow_reboot: false)"`.
   Plain string — old servers render it unchanged (AC-024).
7. Reboot decision, in `handleCommand` *after* the command result is sent
   (mirrors `handleTriggerUpdate`'s post-result pattern): if
   `len(applied) > 0 && a.config.Commands.AllowReboot` →
   `go func(){ time.Sleep(2s); sudo systemctl reboot }` reusing the
   `handleReboot` exec path. Else if `len(applied) > 0` →
   `a.triggerTelemetry()` so the reboot-required state (now true on disk)
   reaches the server within seconds and fires the FR-019 event (AC-015).
   `commandExecResult` gains two internal fields
   (`RebootClassApplied []string`, `RebootPending bool`) so `handleCommand`
   can make this decision — the wire-format `models.CommandResult` is
   **unchanged**.

The agent-side veto is absolute: no param combination reaches
`systemctl reboot` without `AllowReboot` (NFR-002), and nothing releases
holds without `hold_reboot_class: true` (BR-002).

**Rationale.** `defer` is the only construct that provably covers every exit
path of the handler including panics — the FRD's NFR-001 in one line.
Before/after version snapshots make "actually upgraded" robust against
apt/dnf output-format drift, unlike parsing upgrade logs; the released set is
small (typically < 15 packages), so two batched query invocations are
negligible against a 30-minute upgrade.

**Alternatives considered.**
- *Parse upgrade output for applied packages.* Rejected — locale/format
  fragile; the existing summary regexes only extract counts for good reason.
- *Reboot inside the handler before reporting.* Rejected — the result would
  never reach the server; `handleTriggerUpdate` established report-then-act.
- *New `os_update_gated` action.* Rejected — a param on the existing action
  keeps the allowlist, agent switch, and old-agent compat trivial (old agents
  ignore unknown params: `Params["include_reboot_class"]` simply isn't read —
  and with no holds present the run behaves exactly as today).

**Consequences.** An old agent receiving `include_reboot_class: true` ignores
it — but an old agent also never created holds, so the run correctly applies
everything, and never auto-reboots. Compatible in both directions (AC-024).

---

### AD-009: Server policy — `RebootClass` on `MaintenanceWindow`; orchestrator gate; manual dispatches stripped

**Decision.**

1. `internal/models/automation.go`, `MaintenanceWindow` gains:

   ```go
   // RebootClass controls whether automated OS-patch runs may include
   // reboot-class (GPU driver / kernel) packages: "off" (default) or "gated".
   // Only meaningful on the OSPatch window; ignored for DockerUpdate.
   RebootClass string `json:"reboot_class,omitempty"`
   ```

   `DefaultAutomationConfig()` leaves it empty (≡ `"off"`, BR-006). Stored in
   the existing `automation_config` JSON blob — no migration (FR-036).
2. `SetAutomationConfig` validation (auto_update.go:92–133) accepts
   `"", "off", "gated"` for `cfg.OSPatch.RebootClass` and rejects anything
   else with 400; a non-empty `DockerUpdate.RebootClass` is rejected
   (`"reboot_class is only valid on os_patch"`) to keep the blob clean.
3. `checkAutoPatch` (auto_update.go:445–507): after the existing
   `inMaintenanceWindow(cfg.OSPatch)` gate passes, build params as

   ```go
   params := map[string]interface{}{"mode": "full"}
   if cfg.OSPatch.RebootClass == "gated" {
       params["include_reboot_class"] = true
   }
   ```

   Because the function already returned early when out of window, this
   yields exactly FR-021's truth table: window mode + in window + gated ⇒
   include; anytime mode + gated ⇒ include (the FR-021 "explicit opt-in"
   carve-out — `inMaintenanceWindow` returns true for `anytime`); policy off
   ⇒ never include. Out-of-window ⇒ no dispatch at all (AC-013).
4. **Manual dispatches never carry the flag** (FR-023, BR-005):
   `SendCommand` (`handlers/commands.go`) deletes
   `include_reboot_class` from `req.Params` when `Action == "os_update"`
   before creating the command; `BulkPatchDevices` builds its own params and
   simply never sets it (no change needed there beyond a comment).

**Rationale.** Adding the field to `MaintenanceWindow` (rather than
restructuring `OSPatch` into a distinct type) is the smallest additive change
to a JSON blob two frontends already mirror; the DockerUpdate-side rejection
in validation prevents the shared-type field from ever meaning anything
there. Server-side stripping makes BR-005 ("the only release path is the
in-window automated run") enforced at the API boundary instead of relying on
every future UI caller behaving.

**Alternatives considered.**
- *Separate `OSPatchWindow` type embedding `MaintenanceWindow`.* Rejected —
  breaks the stored JSON shape and the TS mirror for zero behavioral gain.
- *Boolean `IncludeRebootClass`.* Rejected — the FRD names an enum
  (`"gated" | "off"`) and an enum leaves room for a future `"manual"` mode
  without another migration of meaning.
- *Allow manual `include_reboot_class` pass-through.* Rejected — BR-005 is
  explicit; an operator who truly wants out-of-band driver updates uses
  `apt-mark unhold` themselves (A-001's deliberate-override escape hatch).

---

### AD-010: Reboot-required detection + all new telemetry fields live on `UpdateInfo`, produced by the updates collector

**Decision.** The updates collector (already the owner of package-manager
interaction) produces everything; no new collector, so **no
collector-whitelist change** (D-008 — existing configs with `updates` enabled
get all of this automatically).

Detection per cycle (FR-017):
- **apt:** `os.Stat("/var/run/reboot-required")` — exists ⇒ `true`; read
  `/var/run/reboot-required.pkgs` lines (deduplicated) as
  `RebootRequiredReasons`. Stat/read errors ⇒ `false`, debug log only.
- **dnf:** `dnf needs-restarting -r` under the cycle context — exit 1 ⇒
  `true`, exit 0 ⇒ `false`, any other exit / missing plugin / error ⇒
  `false` with a **debug**-level log (no spam — AC-018).

Field placement (FR-009, FR-016, FR-018 — all `omitempty`, Section 5 for the
full struct):
- `PendingUpdate.Class string` — `"gpu_driver" | "kernel" | ""`.
- `UpdateInfo.PendingRebootClassCount int` — aggregate, survives truncated
  `Updates` lists (FR-009).
- `UpdateInfo.HeldPackages []string` — from `HoldManager.HeldPackages()`
  after reconcile (sorted for stable JSON).
- `UpdateInfo.RebootRequired bool`, `UpdateInfo.RebootRequiredReasons []string`.

Installed-package classification input (AD-004 step 2) reuses invocations the
collector already makes, with one substitution: the dnf path switches
`rpm -qa` → `rpm -qa --qf '%{NAME}\n'` (same single invocation, names instead
of name-version-release strings; `TotalInstalled` count unchanged). The apt
path parses names from the existing `dpkg --get-selections` output. Net new
process spawns per cycle: **one** (`dnf needs-restarting -r`, dnf only) plus
`apt-mark showhold` when hold enforcement is enabled — within NFR-004's
budget (file stat is free on apt).

**Rationale.** `UpdateInfo` is where every consumer (scoring, patch-status
endpoint, DeviceDetail) already looks; an adjacent struct would force three
call sites to learn a second location. `omitempty` on everything gives FR-035
both directions for free (old agent ⇒ fields absent ⇒ Go zero values; new
agent ⇒ old server's decoder drops unknown keys — the ingest path has no
`DisallowUnknownFields`).

**Alternatives considered.** *Detection in a new `patching` collector* —
rejected; a new collector name would hit the whitelist problem (every
existing agent config would need editing — MEMORY.md rule) for zero benefit.

---

### AD-011: Reboot-required event — UPS-style transition tracking, `reboot_required` template + seeded rule

**Decision.**

1. `internal/models/events.go`: `EventRebootRequired EventType = "reboot_required"`.
2. `internal/server/events/generator.go`: new method

   ```go
   func (g *Generator) CheckRebootRequired(ctx context.Context, deviceID, hostname string, upd *models.UpdateInfo)
   ```

   called from `CheckTelemetryThresholds` when `data.Updates != nil`
   (inserted after the Hardware/GPU checks). Transition logic mirrors
   `CheckUPSAlerts` exactly: key `deviceID + ":reboot_required_active"` in
   the `lastSent` map. While `upd.RebootRequired` is true: if the key is
   absent → emit exactly one event (severity warning, message
   `"Reboot required on <hostname>"` + reasons when present) and set the
   key; if present → refresh the timestamp only (keeps `pruneStaleEntries`'
   1 h cutoff at bay, same as the UPS key). When false → delete the key, so
   the next false→true transition fires again (FR-019, AC-019 all three
   clauses).
   Rule matching: `findMatchingRule(ctx, "reboot_required", …, 1)` — with a
   matching rule the event goes through `createEventAndNotify` (notification
   fan-out); with none, `createEvent` (event log only). Identical shape to
   the UPS on-battery path.
3. `internal/server/events/templates.go`: template
   `{ID: "reboot_required", Name: "Reboot Required", Category: "system",
   Metric: "reboot_required", Operator: "==", Threshold: 1,
   TargetState: "any", Severity: "warning", CooldownSeconds: 86400,
   NeedsTargetName: false, Description: "Fires when a device starts requiring
   a reboot to activate updates"}`.
4. `internal/server/server.go` `seedDefaultAlertRules` defaults slice gains
   `{Name: "Reboot Required", Enabled: true, Metric: "reboot_required",
   Operator: "==", Threshold: 1, Severity: "warning", CooldownSeconds: 86400,
   Notify: true, TemplateID: "reboot_required"}` — fresh installs notify out
   of the box; existing installs (rules already seeded) add it via the
   template in one click.

**Rationale.** The UPS pattern is the codebase's proven
once-per-transition mechanism and already handles the prune interaction.
In-memory transition state means a server restart can re-fire one event for a
still-true state — accepted, identical to existing UPS semantics, and the
rule cooldown (24 h) absorbs it.

**Alternatives considered.** *Persist transition state in DB* — rejected;
FR-036 forbids migrations and the in-memory semantics match the existing
system. *Agent-side event push* — rejected; the server sees state from
telemetry anyway and server-side keeps old agents (which never report the
field ⇒ always false ⇒ no events) working with zero special-casing.

---

### AD-012: GPU container detection — `usesGPU` helper inside the existing inspect calls

**Decision.** `internal/agent/collectors/docker.go` gains an unexported pure
helper:

```go
// usesGPU reports whether a container's HostConfig requests GPU access:
// a DeviceRequest with Driver == "nvidia" or a "gpu" capability, or a
// host device mapping under /dev/nvidia*, /dev/dri, or /dev/kfd.
func usesGPU(hc *container.HostConfig) bool
```

Checks: any `hc.DeviceRequests` entry where `Driver == "nvidia"` **or** any
capability set contains `"gpu"` (covers `--gpus all` and compose
`device_requests`); any `hc.Devices` entry whose `PathOnHost` has prefix
`/dev/nvidia`, `/dev/dri`, or `/dev/kfd` (AMD/ROCm and legacy NVIDIA device
mappings). Called at **both** existing `ContainerInspect` sites — stats phase
(:301 block) and network-mode phase (:354 block) — setting
`containers[idx].UsesGPU`. Every container is inspected by exactly one of the
two phases today, so coverage is total with **zero** additional Docker API
calls (FR-028, NFR/Section 11).

`ContainerInfo` gains `UsesGPU bool \`json:"uses_gpu,omitempty"\``.

**Rationale.** A pure function over `HostConfig` is unit-testable with
literal fixtures (no Docker daemon), and the two call sites are the FRD's
A-005 confirmed data source.

**Alternatives considered.** *NVIDIA-runtime name check
(`hc.Runtime == "nvidia"`)* — folded in? Rejected as a primary signal (modern
setups use DeviceRequests even with the nvidia runtime) but the
`DeviceRequests` check covers the FRD's "NVIDIA runtime requests" phrasing
since the runtime injects device requests; keep scope to the three checks
above and revisit only if QA finds a real-world gap.

---

### AD-013: Scoring fix is data-side only — no engine change, new tests lock it

**Decision.** `internal/server/scoring/engine.go` is **not modified**. The
kernel finding (:201–220) already fails when `PendingKernelUpdate` is true;
FR-027's "silent false pass" is eliminated by AD-002 populating the fields.
Add regression tests to `internal/server/scoring/engine_test.go` asserting
both directions (AC-007) so the check can never silently pass again even if
the finding logic is later refactored.

**Rationale.** Smallest change that satisfies the FR; touching working
scoring code to "fix" a data bug would be churn.

---

### AD-014: UI surfacing

**Decision.**

- **Badges (FR-030, AC-020)** — `DeviceDetail.tsx` Pending Updates table
  gains a class badge next to the package name: violet pill `GPU driver`
  (`bg-violet-500/15 text-violet-300`), amber pill `kernel`
  (`bg-amber-500/15 text-amber-300`); standard packages unchanged. Same
  badges in the FleetOverview patch panel's expanded package table.
- **Held packages (FR-031, AC-021)** — a subsection under the Pending
  Updates section (rendered whenever `updates.held_packages?.length`):
  "Held by rIOt (N)" with the package list and copy "Held until the next
  maintenance-window patch run".
- **Reboot-required (FR-032, AC-021)** — amber indicator chip
  "Reboot required" in the DeviceDetail header area (next to the existing
  status badges), with reasons in a `title` tooltip; in the FleetOverview
  patch panel, an amber "reboot required" tag on the device row, and a
  reboot-class count (`N reboot-class`) next to the existing security count.
  `PatchStatus` (fleet.go) response gains `reboot_class_count int` and
  `reboot_required bool` per device (additive JSON — SEC-001 precedent says
  unknown-field consumers are unaffected).
- **GPU containers (FR-029, AC-022)** — `CompactContainerTile.tsx` gains a
  small `GPU` badge when `c.uses_gpu`; `DeviceDetail.tsx` Docker summary
  line shows "N GPU container(s)" when N > 0 (count derived client-side from
  `docker.containers` — no new endpoint). Zero-GPU devices render nothing.
- **Settings toggle (FR-033, AC-013 setup)** — `AgentManagement.tsx` OS
  patch window editor gains a "Reboot-class packages" select
  (`Off` / `Apply in window + auto-reboot`) bound to
  `os_patch.reboot_class`, with helper copy: "Two-sided opt-in: also set
  `commands.hold_reboot_class: true` (and `allow_reboot` for auto-reboot) in
  each agent's /etc/riot/agent.yaml. Defaults off."
- **Demo data** — `demo-data.ts`: give `pi-cameras` a `linux-image-*` pending
  update with `class: "kernel"`, `nvidia-driver-550` with
  `class: "gpu_driver"` + `held_packages` + `reboot_required: true` on one
  device, and `uses_gpu: true` on one demo container, so demo mode exercises
  every new UI state.

**Rationale.** Every placement extends an element that already renders the
adjacent data; no new pages, no new endpoints, one additive response change.

---

### AD-015: Privilege-boundary widening — exact sudoers rules, runtime preflight, fail-closed enforcement status (SEC-PATCH-GATE-001/-002/-004)

**Decision.** The FRD's A-002 is corrected: the new operations are **not**
covered by the existing allowlist, and this story deliberately widens the
`riot` sudo boundary with exact rules in `scripts/install.sh` (inside the
existing per-package-manager blocks at :437–465, same
resolved-full-path/locked-argument style; `install.sh` joins the change set).

Inside the apt block (after the `apt-get` rules):

```sh
APTMARK_PATH="$(command -v apt-mark 2>/dev/null || echo /usr/bin/apt-mark)"
echo "riot ALL=(root) NOPASSWD: ${APTMARK_PATH} hold *"
echo "riot ALL=(root) NOPASSWD: ${APTMARK_PATH} unhold *"
```

Inside the dnf block (after the `dnf` rules):

```sh
INSTALL_PATH="$(command -v install 2>/dev/null || echo /usr/bin/install)"
RM_PATH="$(command -v rm 2>/dev/null || echo /usr/bin/rm)"
echo "riot ALL=(root) NOPASSWD: ${INSTALL_PATH} -m 0644 -o root -g root /var/lib/riot/dnf-holds.staged /etc/dnf/libdnf5.conf.d/60-riot-holds.conf"
echo "riot ALL=(root) NOPASSWD: ${RM_PATH} -f /etc/dnf/libdnf5.conf.d/60-riot-holds.conf"
```

Constraints (SEC-PATCH-GATE-002, binding on implementation): **no `sh -c`, no
`tee`, no wildcard in any path component, no variable destination.** The only
wildcards are the `apt-mark hold|unhold` package-name arguments, where the
subcommand is locked so the rule cannot reach any other `apt-mark` operation;
option injection through a name is blocked by AD-016's
leading-alphanumeric charset rule. The dnf rules have zero variable
arguments. `apt-mark showhold` and `dnf needs-restarting -r` run
unprivileged and get **no** rules. No sudoers change on hosts without the
respective package manager (existing conditional structure).

**Runtime preflight (fail-closed, visible).** `HoldManager.VerifyPrivileges(ctx)`
runs at agent startup and at the top of every reconcile while
`hold_reboot_class: true`:

- apt: `sudo -n -l <APTMARK_PATH> hold riot-preflight-probe` — exit 0 iff
  the rule exists; nothing is executed or mutated (`-l` only lists).
- dnf5: `sudo -n -l <INSTALL_PATH> -m 0644 -o root -g root
  /var/lib/riot/dnf-holds.staged /etc/dnf/libdnf5.conf.d/60-riot-holds.conf`.

On failure: ERROR log (once per state transition, not per cycle —
"hold enforcement enabled but sudo rules missing; re-run the installer"),
**no hold mutation is attempted**, and telemetry reports the state via a new
field `UpdateInfo.HoldEnforcement` (Section 5):

| Value | Meaning |
|-------|---------|
| *(absent)* | Feature disabled on the agent, or pre-PATCH-GATE agent |
| `"active"` | Preflight passed; reconciliation enforcing |
| `"no_privilege"` | Enabled but sudoers rules missing (SEC-001/SEC-004 state) |
| `"unsupported"` | Enabled but mechanism unavailable (dnf4 — AD-005) |

The UI (AD-014) renders a red "Hold enforcement inactive" warning for the
two failure values — an empty `HeldPackages` list must **never** read as
"protected." This is also the runtime backstop for SEC-PATCH-GATE-004:
in-place `agent_update` never re-runs `install.sh`, so upgraded fleets land
in `no_privilege` until the operator re-runs the installer — loudly, on the
device page and in the log, on day one instead of during an outage.
`doctor.go` performs the same probes and prints pass/fail with the
re-run-installer remediation.

**Rationale.** Argument-locked full-path rules are the file's established
least-privilege idiom; the fixed-source/fixed-dest `install` rule is the
narrowest known mechanism for an unprivileged daemon to maintain exactly one
root-owned file. `sudo -n -l` probes are free, non-mutating, and turn the
review's fail-open hazard into a visible, telemetry-carried state.

**Alternatives considered.**
- *Grant `sudo apt-mark` unrestricted.* Rejected — allows `apt-mark
  showhold`-unrelated subcommands and future surface; lock the two
  subcommands used.
- *`sudo tee`/`sh -c` writer, or a setuid helper binary.* Rejected —
  SEC-002's escalation primitive / a whole new audit surface respectively.
- *Detect privilege lazily from `apt-mark hold` failures.* Rejected —
  indistinguishable from transient errors, retries as log spam, and no
  distinct status for the UI. The explicit probe is deterministic.

**Consequences.** §10's original "no new privilege tier" claim is replaced —
the tier is widened by four narrow rules, enumerated and reviewed. Fresh
installs work out of the box; upgraded fleets need one installer re-run
(runbook §12).

---

### AD-016: Package-name validation and release-path cross-check are enforced controls (SEC-PATCH-GATE-003/-005)

**Decision.** `rebootclass.go` exports:

```go
// validPackageName reports whether a package name is safe to pass to
// privileged package-manager argv and to embed in the dnf fragment.
// Must start with an alphanumeric; charset per dpkg/rpm naming rules.
var packageNameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9.+:~_-]*$`)
func ValidPackageName(name string) bool
```

Enforced (skip + WARN on failure) at every boundary where a name leaves the
agent's data plane: before `apt-mark hold`/`unhold` argv, before inclusion
in the staged dnf fragment, and before being recorded in `holds.json`. The
leading-alphanumeric anchor blocks option injection (`-o Dir::…` can never
be passed to `apt-mark` as a "package name"); the charset excludes `,`,
newline, `[`, `]`, `=`, `#`, and all whitespace, so no name can break out of
the single `excludepkgs=` ini value (the review's full metacharacter list).
This is a **tested requirement**, not prose: SEC-AC-003 in §8 names the
injection test.

The release path applies the SEC-005 cross-check specified in AD-008:
state-load parse error ⇒ abort release (nothing unheld); each name unheld
only if state-tracked **and** `ClassifyPackage(name) != ""`; violations
skipped + WARNed. The same classify cross-check applies to the disable-path
cleanup? **No** — deliberately not: disable must remove every hold rIOt
recorded even if the pattern table has since changed (e.g. a future release
narrows a pattern; the recorded hold must still be releasable), and the
state file is the BR-004 authority there. The release path gets the stricter
rule because it runs immediately before a privileged full upgrade.

**Rationale.** Names originate from repositories — the one genuinely
untrusted-data-into-privileged-command path this story adds. dpkg policy
names are `[a-z0-9.+-]` (min 2 chars, alnum start) and rpm adds `_` and
case; the regex is a strict superset of legal names and a strict subset of
safe strings, so false rejections are impossible for real packages and every
rejected name is attacker-shaped.

**Consequences.** A hostile repo can no longer influence anything beyond
"its weird package doesn't get held" (WARN-logged). Poisoned bookkeeping
can no longer unhold operator-pinned non-reboot-class packages.

---

## 4. Component Changes

| Action | File | Purpose |
|--------|------|---------|
| CREATE | `internal/agent/collectors/rebootclass.go` | Shared classification pattern table: `ClassifyPackage`, `SelectPrimaryKernel`, class constants, exclusion list; `ValidPackageName` charset allowlist (AD-001, AD-002, AD-016). |
| CREATE | `internal/agent/collectors/rebootclass_test.go` | Classification tests: AC-001–AC-006 names, exclusion decisions (`linux-firmware`, `libnvidia-container-tools`), GPU-over-kernel precedence, primary-kernel selection; `ValidPackageName` truth table incl. metacharacter/leading-dash rejections (SEC-AC-003). |
| CREATE | `internal/agent/collectors/holds.go` | `HoldManager`: state file load/save (atomic), apt `apt-mark` reconcile with operator-hold respect, dnf5 staged-file + fixed-path `sudo install` fragment writer / fixed-path `sudo rm` delete + dnf4 unsupported path, `VerifyPrivileges` (`sudo -n -l` probes) + `HoldEnforcement` status, name validation at every privileged boundary, release-path state-tracked ∧ reboot-class cross-check with fail-closed parse-error abort, `Reconcile`, `ReleaseForRun`, `ReapplyAfterRun`, `HeldPackages`, injectable `CommandRunner` (AD-003–AD-005, AD-007, AD-015, AD-016). |
| CREATE | `internal/agent/collectors/holds_test.go` | Hold tests with fake runner + temp state dir: AC-008–AC-012 mechanics, idempotency (NFR-003), crash-marker recovery, disable cleanup preserving operator holds; preflight-denied ⇒ no mutation + `no_privilege` status (SEC-AC-001), fixed-path-only privileged argv assertions (SEC-AC-002), fragment/argv injection-name exclusion (SEC-AC-003), poisoned-state release refusal + parse-error release abort (SEC-AC-005). |
| MODIFY | `internal/agent/collectors/updates.go` | Classify every `PendingUpdate` (apt + dnf paths), populate `PendingKernelUpdate`/`PendingKernelVersion` via `SelectPrimaryKernel`, `PendingRebootClassCount`; call `HoldManager.Reconcile` + report `HeldPackages` and `HoldEnforcement` status; reboot-required detection (stat `/var/run/reboot-required` + `.pkgs`; unprivileged `dnf needs-restarting -r`); switch `rpm -qa` to `--qf '%{NAME}\n'`; parse installed names from existing `dpkg --get-selections` output (AD-010, AD-015). |
| MODIFY | `internal/agent/collectors/updates_test.go` | Extend: AC-005/AC-006 collector-level assertions, reboot-required parsing (AC-017/AC-018 file/exit-code branches via injected runner/paths). |
| MODIFY | `internal/agent/collectors/collector.go` | `Registry.SetHoldManager(*HoldManager)` setter finding the `UpdatesCollector` (mirrors `SetDockerCachePath`) (AD-003). |
| MODIFY | `internal/agent/collectors/docker.go` | `usesGPU(hc)` helper; set `UsesGPU` at both `ContainerInspect` sites (stats phase ~:301, network-mode phase ~:354) (AD-012). |
| MODIFY | `internal/agent/collectors/docker_test.go` | Unit tests for `usesGPU`: nvidia DeviceRequest, `gpu` capability, `/dev/dri` device, `/dev/kfd`, plain container ⇒ false (AC-022 agent half). |
| MODIFY | `internal/models/telemetry.go` | `PendingUpdate.Class`; `UpdateInfo` + `PendingRebootClassCount`, `HeldPackages`, `HoldEnforcement`, `RebootRequired`, `RebootRequiredReasons` (all `omitempty`); `ContainerInfo.UsesGPU` (Section 5, AD-015). |
| MODIFY | `internal/models/automation.go` | `MaintenanceWindow.RebootClass string \`json:"reboot_class,omitempty"\`` + doc comment (AD-009). |
| MODIFY | `internal/models/events.go` | `EventRebootRequired EventType = "reboot_required"` (AD-011). |
| MODIFY | `internal/agent/config.go` | `CommandsConfig.HoldRebootClass bool \`yaml:"hold_reboot_class"\``; `defaultConfigTemplate` commented line; `HoldStatePath()` helper → `/var/lib/riot/holds.json` (AD-003, AD-006). |
| MODIFY | `internal/agent/agent.go` | Construct `HoldManager` in `New()` from config, wire via `SetHoldManager`, keep `holdMgr` field on `Agent`; call `holdMgr.Reconcile` once at startup in `Run()` (AD-003, AD-007). |
| MODIFY | `internal/agent/commands.go` | `handleOSUpdateWithOutput`: parse `include_reboot_class`, `ReleaseForRun` + `defer ReapplyAfterRun`, before/after version snapshot → `RebootClassApplied`, extended result message; `handleCommand`: post-result reboot (gated `AllowReboot`) or `triggerTelemetry()` (AD-008). |
| MODIFY | `internal/agent/commands_test.go` | Extend: AC-010 defer-on-failure, AC-014/AC-015/AC-016 reboot-decision matrix (fake runner, no real reboot), AC-023 no-param passthrough. |
| MODIFY | `internal/agent/doctor.go` | Print hold-enforcement status (`commands.hold_reboot_class`); run the AD-015 `sudo -n -l` probes and report pass/fail for the `apt-mark hold/unhold` and dnf `install`/`rm` sudoers rules with "re-run the install script" remediation; on dnf hosts, report dnf5 fragment-mechanism support (SEC-PATCH-GATE-004; no collectorDeps change — no new collector). |
| MODIFY | `scripts/install.sh` | Extend the sudoers drop-in (:437–465 block) with the four exact AD-015 rules — `apt-mark hold *` / `apt-mark unhold *` in the apt branch; fixed-source/fixed-dest `install -m 0644 -o root -g root /var/lib/riot/dnf-holds.staged /etc/dnf/libdnf5.conf.d/60-riot-holds.conf` and fixed-path `rm -f /etc/dnf/libdnf5.conf.d/60-riot-holds.conf` in the dnf branch — full resolved paths, no `sh -c`, no wildcard path components (SEC-PATCH-GATE-001/-002). Existing `visudo -cf` validation covers the new lines. |
| MODIFY | `internal/server/handlers/auto_update.go` | `SetAutomationConfig`: validate `OSPatch.RebootClass ∈ {"", "off", "gated"}`, reject non-empty `DockerUpdate.RebootClass`; `checkAutoPatch`: add `include_reboot_class: true` to params when policy is `"gated"` (AD-009). |
| MODIFY | `internal/server/handlers/auto_update_test.go` | Extend: AC-013 (gated + in-window ⇒ param present; policy off ⇒ absent; out-of-window ⇒ no dispatch), AC-023 (default config dispatch params unchanged), validation cases. |
| MODIFY | `internal/server/handlers/commands.go` | `SendCommand`: strip `include_reboot_class` from params when `Action == "os_update"` (manual dispatches never carry it — FR-023, AD-009). |
| MODIFY | `internal/server/handlers/commands_test.go` | Extend: manual `os_update` with the param ⇒ stored command params lack it. |
| MODIFY | `internal/server/handlers/fleet.go` | `PatchStatus`: add `RebootClassCount` / `RebootRequired` to `devicePatchInfo` from `s.Updates` (AD-014). |
| MODIFY | `internal/server/handlers/fleet_handler_test.go` | Extend: new fields populated from summaries; absent update info ⇒ zero values. |
| MODIFY | `internal/server/handlers/handlers_test.go` | Extend: AC-024 compat — POST telemetry without any new fields ⇒ 2xx, zero-value decode, no warnings (reuse existing ingest-test harness). |
| MODIFY | `internal/server/events/generator.go` | `CheckRebootRequired` (UPS-pattern transition tracking); call from `CheckTelemetryThresholds` when `data.Updates != nil` (AD-011). |
| CREATE | `internal/server/events/reboot_required_test.go` | AC-019: one event per false→true, none while true persists, re-fires after clear; rule vs no-rule notification paths. |
| MODIFY | `internal/server/events/templates.go` | `reboot_required` template, category `system` (AD-011). |
| MODIFY | `internal/server/server.go` | `seedDefaultAlertRules`: add "Reboot Required" default rule (AD-011). |
| MODIFY | `internal/server/scoring/engine_test.go` | AC-007 regression tests: `PendingKernelUpdate` true ⇒ kernel finding fails; false ⇒ passes (AD-013 — engine.go itself untouched). |
| MODIFY | `web/src/types/models.ts` | Mirror: `PendingUpdate.class?`, `UpdateInfo` new optionals (incl. `hold_enforcement?`), `ContainerInfo.uses_gpu?`, `MaintenanceWindow.reboot_class?` (Section 5). |
| MODIFY | `web/src/api/client.ts` | `PatchStatus` row type: `reboot_class_count`, `reboot_required` (AD-014). |
| MODIFY | `web/src/pages/DeviceDetail.tsx` | Class badges in Pending Updates table; "Held by rIOt" subsection; red "Hold enforcement inactive" warning when `hold_enforcement` is `no_privilege`/`unsupported` (empty held list must never read as protected — AD-015); reboot-required header chip; "N GPU containers" in Docker summary (AD-014). |
| MODIFY | `web/src/pages/DeviceDetail.test.tsx` | Extend: AC-020 badge rendering (all three classes), AC-021 holds + reboot chip, AC-022 GPU count, absence cases; SEC-AC-001 enforcement-inactive warning renders for `no_privilege` and `unsupported`, absent for `active`. |
| MODIFY | `web/src/components/CompactContainerTile.tsx` | Per-container `GPU` badge when `uses_gpu` (AD-014). |
| MODIFY | `web/src/pages/FleetOverview.tsx` | Patch panel: per-device reboot-class count + reboot-required tag; class badges in expanded package table (AD-014). |
| MODIFY | `web/src/pages/FleetOverview.test.tsx` | Extend: AC-021 fleet-level reboot-required tag, reboot-class count rendering. |
| MODIFY | `web/src/pages/settings/AgentManagement.tsx` | Reboot-class policy select on the OS-patch window editor + two-sided opt-in helper copy (AD-014, FR-033). |
| MODIFY | `web/src/api/demo-data.ts` | Demo fixtures: classified updates, held packages, reboot-required device, `uses_gpu` container (AD-014). |

> Files deliberately **not** modified: `internal/server/scoring/engine.go`
> (AD-013), `internal/server/handlers/commands.go` allowlist map (`os_update`
> already present), any `cmd/riot-server/migrations/` file (FR-036), the
> heartbeat path, `internal/agent/collectors/gpu.go` (runtime metrics are
> GPU-001's, unrelated), `BulkPatchDevices` params (already excludes the flag
> by construction).

---

## 5. Data Model Changes

### Schema migration — none

All new telemetry fields ride the `telemetry_snapshots.data` JSONB blob; the
policy toggle rides the `automation_config` value in the `admin_config`
table. `omitempty` everywhere ⇒ old agents' payloads are byte-identical and
old rows decode to zero values (FR-035, FR-036, AC-024).

### `PendingUpdate` — before / after (`internal/models/telemetry.go:269`)

```go
type PendingUpdate struct {
    Name       string `json:"name"`
    CurrentVer string `json:"current_ver"`
    NewVer     string `json:"new_ver"`
    IsSecurity bool   `json:"is_security"`
    Class      string `json:"class,omitempty"` // NEW: "gpu_driver" | "kernel" | "" (standard)
}
```

### `UpdateInfo` — after (`internal/models/telemetry.go:257`)

```go
type UpdateInfo struct {
    PackageManager          string          `json:"package_manager,omitempty"`
    TotalInstalled          int             `json:"total_installed"`
    PendingUpdates          int             `json:"pending_updates"`
    PendingSecurityCount    int             `json:"pending_security_count"`
    PendingKernelUpdate     bool            `json:"pending_kernel_update"`            // NOW POPULATED (FR-008)
    PendingKernelVersion    string          `json:"pending_kernel_version,omitempty"` // NOW POPULATED (FR-008)
    PendingRebootClassCount int             `json:"pending_reboot_class_count,omitempty"` // NEW (FR-009)
    HeldPackages            []string        `json:"held_packages,omitempty"`              // NEW (FR-016)
    HoldEnforcement         string          `json:"hold_enforcement,omitempty"`           // NEW (AD-015): "active" | "no_privilege" | "unsupported"; absent = feature off / old agent
    RebootRequired          bool            `json:"reboot_required,omitempty"`            // NEW (FR-018)
    RebootRequiredReasons   []string        `json:"reboot_required_reasons,omitempty"`    // NEW (FR-018)
    Updates                 []PendingUpdate `json:"updates,omitempty"`
    LastCheckTime           *time.Time      `json:"last_check_time,omitempty"`
    UnattendedUpgrades      bool            `json:"unattended_upgrades"`
}
```

Invariants (FRD §7): `Class ∈ {"", "gpu_driver", "kernel"}`;
`PendingKernelVersion != ""` iff `PendingKernelUpdate`; `HeldPackages` sorted,
may be empty/absent. `HoldEnforcement ∈ {"", "active", "no_privilege",
"unsupported"}`; `HeldPackages` is non-empty only when `HoldEnforcement ==
"active"` (fail-closed: an unenforced state never claims holds).

### `ContainerInfo` — addition (`internal/models/telemetry.go:317`)

```go
UsesGPU bool `json:"uses_gpu,omitempty"` // NEW: HostConfig requests GPU devices (FR-028)
```

Placed after `NetworkMode`.

### `MaintenanceWindow` — addition (`internal/models/automation.go:10`)

```go
RebootClass string `json:"reboot_class,omitempty"` // "off" (default/empty) or "gated"; OSPatch only
```

### Agent YAML (`internal/agent/config.go`)

```go
// CommandsConfig
HoldRebootClass bool `yaml:"hold_reboot_class"` // opt-in: OS-level holds on reboot-class packages
```

New path helpers: `HoldStatePath()` → `/var/lib/riot/holds.json` and
`DNFHoldsStagedPath()` → `/var/lib/riot/dnf-holds.staged` (the fixed sudoers
source path of AD-015 — must match `install.sh` byte-for-byte; Linux;
Windows paths defined for symmetry but the feature is Linux-gated).

### Hold state file (new, agent-owned, `/var/lib/riot/holds.json`)

```json
{
  "version": 1,
  "pm": "apt",
  "apt_holds": ["..."],
  "dnf_excludes": ["..."],
  "released_for_command": "",
  "updated_at": "RFC3339"
}
```

### dnf fragment (new, root-owned, dnf5 hosts only)

`/etc/dnf/libdnf5.conf.d/60-riot-holds.conf` — `[main]` + one
`excludepkgs=` line; deleted (not emptied) when the set is empty or the
feature is disabled. Maintained exclusively through the AD-015 fixed-path
`sudo install` / `sudo rm` rules from the riot-owned staging file
`/var/lib/riot/dnf-holds.staged`; every name charset-validated per AD-016
before staging.

### TypeScript mirrors (`web/src/types/models.ts`)

```ts
// PendingUpdate
class?: 'gpu_driver' | 'kernel'
// UpdateInfo
pending_reboot_class_count?: number
held_packages?: string[]
hold_enforcement?: 'active' | 'no_privilege' | 'unsupported'
reboot_required?: boolean
reboot_required_reasons?: string[]
// ContainerInfo
uses_gpu?: boolean
// MaintenanceWindow
reboot_class?: 'off' | 'gated'
```

---

## 6. API / Interface Contract

### `os_update` command params (WS `command` payload / queued command)

| Param | Type | Semantics |
|-------|------|-----------|
| `mode` | string | Unchanged: `"full"` (default) / `"security"`. |
| `include_reboot_class` | bool, optional | **NEW.** Absent/false ⇒ agent never releases holds (current behavior; held packages are skipped by the package manager). True ⇒ agent releases **rIOt-managed** holds for the duration of the run, iff `commands.hold_reboot_class: true`; otherwise ignored. Set **only** by `checkAutoPatch` when `OSPatch.RebootClass == "gated"`; stripped by `SendCommand` on manual dispatch. |

`models.CommandResult` wire shape: **unchanged**. Reboot-class outcome is
carried in `Message` (FR-026), e.g.
`"Updated 14 packages (reboot-class applied: nvidia-driver-550; reboot initiated)"`.

### `GET/PUT /api/v1/settings/automation`

Request/response gain the optional `os_patch.reboot_class` key
(`"off"`/`"gated"`; absent ≡ off). PUT validation: 400 on any other value, or
on a non-empty `docker_update.reboot_class`. Existing stored blobs (no key)
decode to `""` ≡ off — no data migration.

### `GET /api/v1/fleet/patch-status`

Per-device row gains (additive):

```json
{ "reboot_class_count": 2, "reboot_required": true }
```

`?detail=true` package entries carry `class` when present (rides
`models.PendingUpdate`).

### Telemetry ingest (`POST /api/v1/devices/{id}/telemetry`)

No path/status/validation change. The `updates` object gains the optional
keys of Section 5; the `docker.containers[]` entries gain optional
`uses_gpu`. The decoder is permissive in both directions (AC-024). The server
performs no validation of the new fields — a malicious agent already holds a
device key and could lie about any telemetry (same trust model as
`cpu_percent`).

### Events

New event type `reboot_required` (severity warning) appears in the event log,
WS broadcast, and notification fan-out when a matching rule exists. New alert
template id `reboot_required` served by `GET /api/v1/settings/alert-templates`.

---

## 7. Sequence / Flow

### A. Steady state — hold reconciliation (every telemetry cycle, feature enabled)

1. `UpdatesCollector.Collect(ctx)` starts (60 s cadence).
2. `HoldManager.Reconcile(ctx)`:
   1. Load `/var/lib/riot/holds.json`.
   2. **Preflight** (enabled only): `VerifyPrivileges` — `sudo -n -l` probe
      for the AD-015 rules (+ dnf5 mechanism check). Failure ⇒ set
      `HoldEnforcement = "no_privilege"` (or `"unsupported"`), ERROR log on
      state transition, **no mutation attempted**, return (AD-015).
   3. `released_for_command != ""` ⇒ WARN "re-asserting holds after
      interrupted run <id>"; continue (converged by step 5), clear marker on
      success.
   4. Disabled + non-empty state ⇒ unhold ours / delete fragment
      (fixed-path `sudo rm`), clear state, INFO log, return.
   5. Enabled: installed reboot-class set (pattern table over installed
      names, each `ValidPackageName`-checked) ∪ current holds
      (`apt-mark showhold` unprivileged / fragment content) → hold missing
      ones (skipping operator-held), unhold ours-no-longer-installed,
      re-stage + `sudo install` fragment on drift (dnf5), persist state,
      set `HoldEnforcement = "active"`. INFO log with package lists on any
      change (NFR-007); silent when converged.
3. Collector proceeds: pending-update parse → classify each entry →
   `PendingKernelUpdate`/`Version` (AD-002) → `PendingRebootClassCount` →
   `HeldPackages` from manager → reboot-required detection (AD-010).
4. Telemetry POSTs; server ingest → `CheckTelemetryThresholds` →
   `CheckRebootRequired` (transition logic) → `checkAutoPatch`.

### B. In-window automated run (the release path — AC-014)

1. Telemetry arrives; `checkAutoPatch`: device auto-patch on, pending > 0,
   `inMaintenanceWindow(OSPatch)` true, cooldown clear.
2. `RebootClass == "gated"` ⇒ dispatch `os_update`
   `{"mode":"full","include_reboot_class":true}` via WS (or queue).
3. Agent `handleOSUpdateWithOutput`:
   1. `allow_patching` gate (unchanged).
   2. `include_reboot_class && holdMgr.Enabled` ⇒
      `ReleaseForRun(cmdID)`: write `released_for_command = cmdID` to state
      **then** `apt-mark unhold <ours>` / delete fragment. INFO log.
   3. `before := installedVersions(releasedSet)`.
   4. `defer ReapplyAfterRun(bgCtx, cmdID)` — re-hold against post-run
      installed set, clear marker. Covers **all** exits.
   5. Refresh + upgrade (unchanged, 30-min context).
   6. Success ⇒ `after := installedVersions(releasedSet)`;
      `applied := diff(before, after)`.
   7. Return result with `RebootClassApplied`; the `defer` fires — holds
      re-applied (now covering any new package names, e.g.
      `linux-image-6.8.0-46-generic`).
4. `handleCommand` sends `command_result` to the server (auditable message —
   FR-026), then:
   - `applied ≠ ∅ && allow_reboot` ⇒ 2 s later `sudo systemctl reboot`
     (AC-014).
   - `applied ≠ ∅ && !allow_reboot` ⇒ `triggerTelemetry()`; next cycle
     reports `reboot_required: true`; server fires the event (AC-015).
   - `applied == ∅` ⇒ nothing (AC-016).

### C. Failure modes of an in-window run

| Point of failure | What happens | Holds after |
|------------------|--------------|-------------|
| Refresh command fails | Handler returns error; `defer` re-applies | Held |
| Upgrade fails / partial apply | Error result; `defer` re-applies against whatever is now installed | Held |
| Upgrade succeeds, before/after query fails | `applied` treated as empty ⇒ **no reboot** (fail-safe, FR-025); message notes detection failure; `defer` re-applies | Held |
| Agent panics in handler | `defer` still runs | Held |
| Agent process dies (OOM/kill/power) between release and re-apply | Marker `released_for_command` persisted **before** release; on agent restart, startup `Reconcile` (and every subsequent cycle) re-asserts holds + WARN | Held after restart (window = downtime + ≤1 cycle) |
| State file unwritable at release time | `ReleaseForRun` **aborts before unholding** (marker-first ordering) ⇒ run proceeds with holds in place, reboot-class packages skipped, result notes it | Held |
| State file unparseable at release time | `ReleaseForRun` aborts — **nothing unheld** (fail-closed, SEC-005); run proceeds with holds in place | Held |
| State entry fails reboot-class cross-check at release | That name skipped + WARN (poisoned-state defense, SEC-005); rest of release proceeds | Operator/pinned packages stay held |
| Sudoers rules missing (fresh flag on upgraded fleet) | Preflight fails ⇒ ERROR log + `hold_enforcement: "no_privilege"` in telemetry ⇒ red UI warning; no mutation ever attempted (SEC-001/-004) | Never held — **visibly** unenforced, not silently |
| Server restarts mid-run | Command result may miss WS; run + re-hold complete agent-side regardless | Held |

### D. Disable flow (AC-011)

Operator sets `hold_reboot_class: false`, restarts agent → startup
`Reconcile` sees disabled + non-empty state → releases exactly the recorded
rIOt holds (operator's `postgresql-16` hold untouched — never in state),
deletes the dnf fragment, clears state, INFO log.

---

## 8. Acceptance Criteria Mapping

| AC ID | Fulfilled By | Test Strategy |
|-------|--------------|---------------|
| AC-001 | `rebootclass.go` GPU pattern rules + rule-order precedence (AD-001) | `rebootclass_test.go` — `[AC-001] apt GPU driver packages classified gpu_driver`: `nvidia-driver-550`, `libnvidia-compute-550`, `nvidia-dkms-550`, `rock-dkms` ⇒ all `gpu_driver`; explicit sub-test that `nvidia-dkms-550` is not `kernel`. |
| AC-002 | `rebootclass.go` kernel patterns incl. `-dkms` suffix rule | `rebootclass_test.go` — `[AC-002]`: `linux-image-6.8.0-45-generic`, `linux-headers-…`, `linux-modules-…`, `zfs-dkms` ⇒ all `kernel`. |
| AC-003 | Same table, dnf names (AD-001) | `rebootclass_test.go` — `[AC-003]`: `akmod-nvidia`, `xorg-x11-drv-nvidia-cuda`, `kmod-nvidia-latest-dkms` ⇒ `gpu_driver`. |
| AC-004 | Same table, dnf kernel names | `rebootclass_test.go` — `[AC-004]`: `kernel`, `kernel-core`, `kernel-modules-extra`, `kernel-devel` ⇒ `kernel`. |
| AC-005 | Exclusion rules + single shared function (FR-007) | `rebootclass_test.go` — `[AC-005]`: `curl`, `openssl`, `linux-firmware`, `docker-ce`, `libnvidia-container-tools` ⇒ `""`; `updates_test.go` asserts both `collectAPT` and `collectDNF` paths call `ClassifyPackage` (one table — verified structurally: no second table exists to drift). |
| AC-006 | `SelectPrimaryKernel` + `collectAPT`/`collectDNF` wiring (AD-002) | `updates_test.go` — `[AC-006]`: pending `linux-image-6.8.0-45-generic → 6.8.0-45.45` ⇒ `PendingKernelUpdate=true`, `PendingKernelVersion="6.8.0-45.45"`; no kernel pending ⇒ `false`/empty. |
| AC-007 | Populated fields (AD-002) consumed by unchanged `engine.go:201–220` (AD-013) | `engine_test.go` — `[AC-007]`: device with `PendingKernelUpdate: true` ⇒ `no-kernel-update` finding `Passed=false` and score penalized; `false` ⇒ `Passed=true`. |
| AC-008 | `HoldManager` apt reconcile (AD-004, AD-007) | `holds_test.go` — `[AC-008]`: fake runner reports installed `nvidia-driver-550`, `linux-image-generic`, empty `showhold` ⇒ runner receives `apt-mark hold` for both; state file records both as rIOt-managed. |
| AC-009 | dnf5 fragment writer (AD-005) | `holds_test.go` — `[AC-009]`: temp-dir fragment path; installed `akmod-nvidia`, `kernel-core` ⇒ fragment exists with `excludepkgs=` covering both; assert no other file in temp dir touched; regenerate is byte-identical (NFR-003). |
| AC-010 | `ReleaseForRun`/`defer ReapplyAfterRun` in `handleOSUpdateWithOutput` (AD-008) | `commands_test.go` — `[AC-010]`: fake runner sequence assert unhold-before-upgrade and re-hold-after; failure variant: upgrade command errors ⇒ re-hold still invoked before handler returns; `holds_test.go` covers marker write-before-release ordering. |
| AC-011 | State-file bookkeeping + disable path + `showhold` operator-respect (AD-003, AD-004) | `holds_test.go` — `[AC-011]`: state holds `nvidia-driver-550`; `showhold` reports it **and** `postgresql-16`; disable ⇒ runner sees `unhold nvidia-driver-550` only; dnf variant asserts fragment deleted. |
| AC-012 | OS-level enforcement is delegated to apt/dnf hold semantics (A-001) — rIOt's obligation is that holds exist outside runs | Covered by AC-008/AC-009 (holds present) + AC-010 (re-applied after runs) + AC-013 (never released out-of-window). Live `unattended-upgrades` behavior is out of unit-test reach (A-001 assumption); QA notes manual verification optional. |
| AC-013 | `checkAutoPatch` early-return out-of-window + param only when gated; `SendCommand` strip (AD-009) | `auto_update_test.go` — `[AC-013]`: gated policy + out-of-window ⇒ no command created; gated + in-window ⇒ command params contain `include_reboot_class: true`; policy off + in-window ⇒ param absent. `commands_test.go` (server) — manual dispatch with the param ⇒ stored params lack it. Agent half: `commands_test.go` (agent) — no param ⇒ no `ReleaseForRun` call. |
| AC-014 | AD-008 flow steps 3–4: applied-detection + post-result reboot | `commands_test.go` (agent) — `[AC-014]`: fake runner, `include_reboot_class: true`, `AllowReboot: true`, before/after versions differ for `nvidia-driver-550` ⇒ result message contains "reboot-class applied" + "reboot initiated"; reboot exec invoked (captured by fake runner, not executed). |
| AC-015 | AD-008 else-branch (`triggerTelemetry`) + AD-010 detection + AD-011 event | `commands_test.go` — `[AC-015]`: same but `AllowReboot: false` ⇒ no reboot exec; telemetry trigger fired; message notes "not permitted". `reboot_required_test.go` — ingesting `RebootRequired: true` after false emits the event. |
| AC-016 | `applied == ∅` guard (AD-008 step 5, FR-025) | `commands_test.go` — `[AC-016]`: run where before/after versions of released set are identical (only standard packages upgraded) ⇒ no reboot exec, no reboot text in message; failure-before-apply variant ⇒ same. |
| AC-017 | apt detection (AD-010) | `updates_test.go` — `[AC-017]`: injected paths (temp dir): marker file + `.pkgs` listing `linux-image-6.8.0-45-generic` ⇒ `RebootRequired=true`, reason present; file absent ⇒ `false`. |
| AC-018 | dnf detection exit-code mapping (AD-010) | `updates_test.go` — `[AC-018]`: fake runner exit 1 ⇒ `true`; exit 0 ⇒ `false`; command-not-found error ⇒ `false` and no WARN/ERROR log record (capture slog output). |
| AC-019 | `CheckRebootRequired` transition tracking (AD-011) | `reboot_required_test.go` — `[AC-019]`: sequence false→true ⇒ 1 event; true→true (×3) ⇒ still 1; true→false→true ⇒ 2nd event; with matching rule ⇒ dispatcher invoked (notification eligibility). |
| AC-020 | DeviceDetail badge rendering (AD-014) | `DeviceDetail.test.tsx` — `[AC-020]`: telemetry fixture with `gpu_driver`, `kernel`, and standard updates ⇒ two badge variants rendered, standard row has none. |
| AC-021 | Held list + reboot chip (DeviceDetail) + fleet tag (FleetOverview + `PatchStatus` fields) (AD-014) | `DeviceDetail.test.tsx` — `[AC-021]`: `held_packages` ⇒ "Held by rIOt" section with names + window copy; `reboot_required: true` ⇒ header chip. `FleetOverview.test.tsx` — patch row shows reboot-required tag. `fleet_handler_test.go` — response fields populated. |
| AC-022 | `usesGPU` helper + both inspect sites + tile badge + device count (AD-012, AD-014) | `docker_test.go` — `[AC-022]` helper truth table (5 fixtures). `DeviceDetail.test.tsx` — 3 of 5 containers `uses_gpu` ⇒ "3 GPU containers"; zero ⇒ indicator absent; tile test asserts per-container badge. |
| AC-023 | Defaults: `RebootClass` empty ⇒ `checkAutoPatch` params `{"mode":"full"}` exactly; `HoldRebootClass` false ⇒ `Reconcile` no-ops (no state file created); agent no-param path untouched (AD-006–AD-009) | `auto_update_test.go` — `[AC-023]`: default config dispatch params deep-equal `{"mode":"full"}`. `holds_test.go` — disabled + no state ⇒ zero runner invocations, no file writes. `commands_test.go` — os_update without param ⇒ no hold calls, no reboot, result identical to pre-story shape. |
| AC-024 | `omitempty` fields + permissive decoders + no migration (Section 5, AD-010) | `handlers_test.go` — `[AC-024]`: POST telemetry JSON lacking every new key ⇒ 2xx, decoded `UpdateInfo` zero-valued, no WARN logs. Reverse: marshal new `UpdateInfo`, unmarshal into an old-shape fixture struct ⇒ no error. DoD check: `git status cmd/riot-server/migrations/` clean. |

### Security-review ACs (required mitigations — `docs/security/PATCH-GATE-security-review.md`)

| AC ID | Finding | Fulfilled By | Test Strategy |
|-------|---------|--------------|---------------|
| SEC-AC-001 | SEC-PATCH-GATE-001 (HIGH) | `install.sh` sudoers additions + `VerifyPrivileges` preflight + `HoldEnforcement` status, fail-closed (AD-015) | `holds_test.go` — `[SEC-AC-001] preflight denial blocks enforcement visibly`: fake runner returns non-zero for `sudo -n -l` ⇒ zero mutating invocations, status `no_privilege`, exactly one ERROR log per transition; success path ⇒ status `active`. `DeviceDetail.test.tsx` — inactive warning renders for `no_privilege`/`unsupported`, never for `active`. Manual QA: fresh apt install ⇒ `/etc/sudoers.d/riot-agent` contains the two `apt-mark` rules and `visudo -cf` passes. |
| SEC-AC-002 | SEC-PATCH-GATE-002 (HIGH) | Fixed-path staged `install`/`rm` fragment mechanism (AD-005, AD-015); no `sh -c`/`tee`/wildcard-path rule anywhere | `holds_test.go` — `[SEC-AC-002] privileged argv is fixed-shape`: capture every runner invocation across a full reconcile + release + disable cycle; assert the only sudo argv forms are `apt-mark hold/unhold <names…>`, the exact `install -m 0644 -o root -g root <staged> <fragment>` pair, and the exact `rm -f <fragment>`; assert fragment content never appears in argv (stdin/staging only). Code review + QA grep of `install.sh` diff: no `sh -c`, no `tee`, no `*` in any path component of new rules. |
| SEC-AC-003 | SEC-PATCH-GATE-003 (MEDIUM) | `ValidPackageName` (`^[A-Za-z0-9][A-Za-z0-9.+:~_-]*$`) enforced before argv, fragment staging, and state recording (AD-016) | `rebootclass_test.go` — `[SEC-AC-003] package-name charset allowlist`: accepts `nvidia-driver-550`, `libc6:amd64`, `kernel-core`, `zfs-dkms`, `1password`; rejects names containing `,`, newline, `[`, `]`, `=`, `#`, space, and any leading `-` (option-injection shape). `holds_test.go` — `[SEC-AC-003] injection names never reach privileged sinks`: installed set includes `evil,pkg`, `bad\nname`, `[main]x`, `-o` ⇒ none appear in any `apt-mark` argv, staged fragment, or `holds.json`; each skip WARN-logged. |
| SEC-AC-004 | SEC-PATCH-GATE-004 (MEDIUM) | Runtime preflight (SEC-AC-001) is the enforcement backstop; `doctor.go` sudo-rule probes with re-run-installer remediation; §12 runbook step | `doctor_test.go` (or manual doctor run in QA) — doctor reports the sudoers probes with pass/fail and remediation text. Docs review: runbook step 2 includes the installer re-run for upgraded fleets. |
| SEC-AC-005 | SEC-PATCH-GATE-005 (MEDIUM) | Release-path fail-closed rules: parse-error ⇒ abort release; unhold only state-tracked ∧ `ClassifyPackage != ""` (AD-008, AD-016) | `holds_test.go` — `[SEC-AC-005] poisoned state never unholds operator packages`: state `apt_holds` contains `postgresql-16` (non-reboot-class) + `nvidia-driver-550` ⇒ release unholds only the driver, WARNs the skip; corrupt-JSON state file ⇒ `ReleaseForRun` returns released=∅, zero unhold invocations, run continues. |

---

## 9. Error Handling

| Failure mode | Detection | Response |
|--------------|-----------|----------|
| `apt-mark` / fragment write fails during reconcile | non-zero exit / write error | WARN with package list; keep previous state; retry naturally next cycle (NFR-005). Collector continues. |
| Sudoers rules missing (preflight fails) | `sudo -n -l` probe non-zero | ERROR once per state transition; `HoldEnforcement = "no_privilege"`; no mutation attempted; UI shows enforcement-inactive warning (fail-closed, never silent — SEC-001/-004). |
| State file corrupt/unparseable at reconcile | JSON error on load | WARN once; treat as empty state **but do not release anything** (empty state ⇒ nothing is "ours" to unhold); reconcile re-holds and rewrites a fresh state file. Operator holds unaffected. |
| State file corrupt/unparseable at `ReleaseForRun` | JSON error on load | **Abort the release** — nothing unheld; run proceeds with holds intact; result message notes reboot-class packages were skipped (SEC-005 fail-closed). |
| State entry not reboot-class at release | `ClassifyPackage(name) == ""` cross-check | Skip + WARN; never unheld (SEC-005 poisoned-state defense). |
| Package name fails charset validation | `ValidPackageName == false` | Skip + WARN; excluded from `apt-mark` argv, fragment, and state (SEC-003). |
| State file unwritable at `ReleaseForRun` | write error | Abort release (marker-first ordering) — run proceeds with holds intact; result message notes reboot-class packages were skipped. |
| Agent crash between release and re-apply | `released_for_command` marker on next start | Startup + per-cycle `Reconcile` re-asserts; WARN with stale command ID (§7C). |
| `dnf needs-restarting` missing / errors | exec error / unexpected exit | `RebootRequired=false`, DEBUG log only (AC-018 no-spam). |
| dnf4 host with hold enforcement enabled | `dnf --version` major < 5 | One WARN per process; `HeldPackages` empty; no filesystem writes (AD-005). |
| Before/after version query fails post-upgrade | exec error | `applied` = empty ⇒ **no reboot** (fail-safe); message records detection failure; holds still re-applied by defer. |
| `ContainerInspect` fails for a container | existing per-call error path | `UsesGPU` stays false for that container — same degradation as `NetworkMode`/`HealthStatus` today. No new handling. |
| Invalid `reboot_class` in PUT automation | validation (AD-009) | 400 `{"error":"invalid reboot_class: …"}` — matches existing validation style. |
| Manual `os_update` with `include_reboot_class` | `SendCommand` strip | Param silently removed (BR-005); command otherwise processed normally. |
| Old agent receives `include_reboot_class` | unknown param unread | Ignored; run behaves as today (no holds exist on that host). |
| Reboot exec fails (`systemctl` error) | `cmd.Start` error after result sent | WARN log; reboot-required state remains true ⇒ FR-019 event surfaces the condition on next cycle. |

No new HTTP status codes. All hold mutations log at INFO with package lists
(NFR-007); failures at WARN; detection noise at DEBUG.

---

## 10. Security Considerations

- **Privilege surface — deliberately widened, exactly specified**
  (SEC-PATCH-GATE-001/-002; corrects FRD A-002, which wrongly assumed the
  existing sudoers grants sufficed). The `riot` user's allowlist in
  `/etc/sudoers.d/riot-agent` gains exactly four rules, written by
  `scripts/install.sh` in the same resolved-full-path, argument-locked style
  as the existing entries:

  | Branch | Rule |
  |--------|------|
  | apt | `riot ALL=(root) NOPASSWD: <apt-mark> hold *` |
  | apt | `riot ALL=(root) NOPASSWD: <apt-mark> unhold *` |
  | dnf | `riot ALL=(root) NOPASSWD: <install> -m 0644 -o root -g root /var/lib/riot/dnf-holds.staged /etc/dnf/libdnf5.conf.d/60-riot-holds.conf` |
  | dnf | `riot ALL=(root) NOPASSWD: <rm> -f /etc/dnf/libdnf5.conf.d/60-riot-holds.conf` |

  Hard constraints: no `sh -c`, no `tee`, no wildcard in any **path**
  component, no variable destination — a wildcard/shell rule here is an
  arbitrary-root-file-write ⇒ local root escalation (SEC-002 exploit
  scenario). The only wildcards are the `apt-mark` package-name arguments;
  the locked subcommands prevent any other `apt-mark` operation, and
  AD-016's leading-alphanumeric rule prevents a "name" from smuggling an
  option (`-o …`). The existing un-allowlisted `sudo tee` in
  `enable_auto_updates` (`commands.go:407`) is a latent over-grant and is
  explicitly **not** precedent for this story. `apt-mark showhold` and
  `dnf needs-restarting -r` run unprivileged — no rules. The agent
  preflight-verifies the rules (`sudo -n -l`, non-mutating) and fails closed
  with a telemetry-visible `hold_enforcement` status when they are absent
  (AD-015) — enforcement is never silently inactive.
- **Command injection via package names.** Names flow package-manager →
  classifier → back to the same package manager as argv elements. A
  hostile repo could publish a bizarrely-named package, but it lands in
  `exec.CommandContext` argv, not a shell — and every name must first pass
  the enforced `ValidPackageName` allowlist
  (`^[A-Za-z0-9][A-Za-z0-9.+:~_-]*$`, AD-016, tested by SEC-AC-003) before
  reaching `apt-mark` argv, the dnf fragment, or the state file. The
  charset excludes `,`, newlines, `[`, `]`, `=`, `#`, and whitespace, so no
  name can break out of the fragment's single `excludepkgs=` ini value; the
  leading-alphanumeric anchor blocks option injection.
- **Server cannot force a reboot.** NFR-002 holds structurally: the only
  reboot call sites remain gated on `a.config.Commands.AllowReboot`, which no
  command param reaches. The new param can at most *release rIOt's own
  holds*, and only when the operator set `hold_reboot_class: true`.
- **Two-sided opt-in integrity.** A compromised server flipping
  `RebootClass: "gated"` gains nothing on agents without the YAML flag
  (BR-002); a compromised agent config gains nothing without the server
  policy because no dispatch ever carries the param.
- **Telemetry trust.** New fields (`held_packages`, `reboot_required`,
  `uses_gpu`) are device-key-authenticated agent claims, same trust class as
  all existing telemetry; the server renders but never acts destructively on
  them (the only actuation path — dispatching os_update — keys off
  `pending_updates`, as today).
- **State file.** `0600`, contains only package names. Fragment `0644` (dnf
  must read it), content is operator-visible policy, not secret. The state
  file is the sole unhold authority (BR-004), so the release path treats it
  as fail-closed input: parse errors abort the release outright, and every
  candidate must also classify reboot-class before it is unheld — a
  poisoned entry (`postgresql-16`) can never be released
  (SEC-PATCH-GATE-005, SEC-AC-005).
- **Manual-dispatch strip** (AD-009) closes the API path to out-of-window
  hold release, keeping BR-005 enforceable server-side.

---

## 11. Performance Considerations

### Agent (per telemetry cycle, feature enabled)

- **Classification:** pure string matching over names already parsed
  (NFR-004). ~40 prefix/exact comparisons × (pending + installed) packages;
  for 2 000 installed packages this is sub-millisecond. Zero new
  package-manager invocations for classification itself.
- **Reboot-required:** apt = one `os.Stat` (+ one small file read when
  present) — effectively free. dnf = one `dnf needs-restarting -r` per
  cycle under the cycle context (NFR-004's allowance; typically 1–3 s, well
  inside the 60 s cadence; a hang is bounded by the existing per-cycle
  context timeout).
- **Hold reconciliation frequency:** once per telemetry cycle + once at
  startup (AD-007). Converged steady state costs: one `sudo -n -l` preflight
  probe (~10 ms, no privileged execution — AD-015); apt = one
  `apt-mark showhold` (~50 ms); dnf5 = one in-memory set compare against the
  fragment (re-written only on drift; steady state is a read + compare).
  Mutating invocations (`apt-mark hold/unhold`, fragment rewrite) occur only
  on drift — normally only right after a patch run or a package
  install/removal. Disabled (default): `Reconcile` returns after one
  `os.Stat` of the state file — nanoseconds-to-microseconds, preserving
  AC-023's "unchanged behavior".
- **In-window run overhead:** two batched version queries
  (`dpkg-query -W` / `rpm -q` over ≤ ~20 names, tens of ms) against a
  multi-minute upgrade — noise.
- **Docker:** `usesGPU` reads fields of the `ContainerInspect` response the
  collector **already fetches** at both call sites — zero additional Docker
  API calls, zero extra goroutines (explicit Section-11 mandate honored).
- **Payload size:** worst realistic case (~15 reboot-class pending, ~10
  held, reasons list) adds ~600 bytes to a telemetry document that already
  carries full container/process/service inventories — negligible. All
  fields `omitempty` ⇒ zero bytes on unaffected/old hosts.

### Server

- **No new queries.** `CheckRebootRequired` is in-memory map work on the
  existing per-snapshot fan-out; rule lookup rides the existing 5 s
  `rulesCache`. `PatchStatus` reads fields from summaries it already
  decodes. `checkAutoPatch` adds one map insert.
- **No new indexes, no migration, no new endpoints.**

### Frontend

- Badges/chips are conditional spans on existing lists; GPU count is a
  single `filter().length` over containers already in memory. No new
  queries, no new polling.

---

## 12. Implementation Notes for Engineers

1. **Rule-order is load-bearing** in `ClassifyPackage`: exclusions → GPU →
   kernel. Table-driven (`[]rule{kind, match, class}`) so tests can assert
   precedence structurally. Every FRD-named package in AC-001–AC-005 must
   appear literally in the test corpus, plus the exclusion decisions.
2. **`CommandRunner` injection** (`holds.go`):
   `type CommandRunner func(ctx context.Context, name string, args ...string) ([]byte, error)`.
   Production default wraps `exec.CommandContext(...).CombinedOutput()`.
   Tests record invocations and script outputs (`showhold`, exit codes).
   The updates collector's reboot-required check accepts the same injection
   (package-level var or collector field) so AC-017/AC-018 run without an OS.
   The apt marker path is a field (`rebootRequiredPath`) defaulting to
   `/var/run/reboot-required` for temp-dir tests.
3. **Marker-first ordering in `ReleaseForRun`:** persist
   `released_for_command` (fsync via rename) **before** the first unhold.
   If the persist fails, abort the release entirely. This is what makes the
   §7C crash table true — do not reorder.
4. **`ReapplyAfterRun` uses `context.Background()`** with its own ~2-min
   timeout — the run's context may be dead on the failure paths the defer
   exists for.
5. **Batch `apt-mark` calls** — one invocation with all names per
   operation, not per package.
6. **Do not touch `handleReboot`'s gate** or add any code path to
   `systemctl reboot` that doesn't read `a.config.Commands.AllowReboot`
   directly (NFR-002). The auto-reboot reuses the same exec construction;
   factor a tiny `execReboot()` helper both call.
7. **`checkAutoPatch` param construction** must not mutate shared maps —
   build a fresh `map[string]interface{}` per dispatch (the current literal
   already does).
8. **`SendCommand` strip:** `if req.Action == "os_update" { delete(req.Params, "include_reboot_class") }`
   before `commandRepo.Create`. Add the server-side test first — it's the
   BR-005 boundary.
9. **Generator transition key** must be refreshed (timestamp updated) on
   every cycle where `RebootRequired` is true — copy the UPS
   `wasOnBatteryKey` handling verbatim, including the lock scope.
10. **Frontend `class` is a reserved-feeling word** — in TSX destructuring
    use `u.class` via bracket/property access carefully
    (`u['class']` or rename in a local: `const cls = u.class`); it is legal
    as a property but not as a bare identifier.
11. **`scrollbar-thin`** on any new overflow container (project convention).
12. **Test naming:** every new test carries its `[AC-XXX]` prefix per the
    engineering standards; hold tests that cover NFRs reference
    `[NFR-001]`/`[NFR-003]` in sub-test names for the QA audit.
13. **Blocker protocol:** if `handleOSUpdateWithOutput` or `checkAutoPatch`
    no longer match the shapes cited in §2 when work begins, or dnf5
    fragment behavior can't be validated, write
    `docs/architecture/PATCH-GATE-blockers.md` and stop.
14. **Do NOT copy the `sudo tee` pattern** at
    `internal/agent/commands.go:407` for the fragment write — it is an
    un-allowlisted latent over-grant, not precedent
    (SEC-PATCH-GATE-002). The only privileged file operations are the two
    fixed-path sudoers rules of AD-015. Do not "fix" the existing :407 tee
    in this story either — out of scope; it is flagged for its own task.
15. **Path constants must match `install.sh` byte-for-byte.** The staged
    path (`/var/lib/riot/dnf-holds.staged`) and fragment path
    (`/etc/dnf/libdnf5.conf.d/60-riot-holds.conf`) appear in both the Go
    helpers and the sudoers rules; any drift makes the sudo rule never
    match (preflight will catch it, but as a broken feature). Define them
    once in `config.go` and reference the literal strings in an
    `install.sh` comment pointing back.
16. **Preflight uses `sudo -n -l <cmd> <args…>`** — it lists, never
    executes; `-n` guarantees no password prompt hang under the
    non-interactive agent. Cache the result per reconcile pass only (a
    just-fixed sudoers file should be picked up within one cycle). Log the
    ERROR only on status transitions, not every cycle.
17. **`HoldEnforcement` is set on every enabled cycle** (`active` /
    `no_privilege` / `unsupported`) and left empty when the agent flag is
    off — the UI relies on "absent = feature off, present-non-active =
    broken" (AD-015 table).

### Rollout & configuration documentation (for the technical writer, and README obligations on this story)

- **README:** new subsection under agent configuration documenting
  `commands.hold_reboot_class` (default false, what it holds, dnf5
  requirement and dnf4 limitation, interaction with `allow_patching` /
  `allow_reboot`), the server-side `Reboot-class packages` setting, the
  two-sided opt-in table (BR-002), the hold state file path, the dnf
  fragment path, and the explicit note that disabling removes only
  rIOt-created holds.
- **Enablement runbook (README or docs/):** 1) upgrade the agent, 2)
  **re-run the install script** (`curl … | sudo bash` — idempotent; it
  rewrites `/etc/sudoers.d/riot-agent` with the new `apt-mark`/fragment
  rules). This step is **mandatory for existing fleets**: in-place
  `agent_update` never touches sudoers, and without it hold enforcement
  reports `no_privilege` and does nothing (SEC-PATCH-GATE-004). 3) set
  `hold_reboot_class: true` (+ `allow_patching`, optionally `allow_reboot`)
  in `/etc/riot/agent.yaml`, restart agent, 4) configure OSPatch window
  (mode `window`) + set Reboot-class to `gated` in Settings → Agent
  Management, 5) verify the device page shows held packages **and no
  "Hold enforcement inactive" warning** (run `riot-agent doctor` if it
  does — it names the missing sudoers rules).
- **CHANGELOG:** entry under the release version listing: classification +
  populated kernel fields (note: security scores may drop on devices with
  pending kernel updates — expected, the check was silently passing),
  OS-level holds, in-window apply + auto-reboot, reboot-required events,
  GPU-container flag, new automation setting, new agent config flag. Tag
  required — this is a code change (MEMORY.md rule).
- **No agent-config mass-edit needed:** no new collector name — the
  `updates` collector already in every whitelist carries the feature
  (AD-010). Call this out explicitly so operators don't hunt for a
  collector to enable; only the `commands.hold_reboot_class` flag is new.

---

## 13. Definition of Done

- [ ] Every §4 component change implemented; every §8 AC has named
      `[AC-XXX]` tests passing.
- [ ] `ClassifyPackage` is the **only** pattern table (grep for a second
      table returns nothing); GPU precedence and all exclusion decisions
      tested.
- [ ] `PendingKernelUpdate`/`PendingKernelVersion` populated by the
      collector; `engine.go` unmodified; AC-007 regression tests present.
- [ ] `HoldManager` with state file, marker-first release ordering,
      defer-based re-apply, operator-hold respect, dnf5 fragment, dnf4
      safe-degrade; idempotency (NFR-003) tested.
- [ ] `scripts/install.sh` sudoers block extended with exactly the four
      AD-015 rules; `visudo -cf` still validates; diff contains no `sh -c`,
      no `tee`, no wildcard path components (SEC-AC-002 review check).
- [ ] Preflight (`sudo -n -l`) gates all hold mutation; `hold_enforcement`
      status (`active`/`no_privilege`/`unsupported`) in telemetry and
      rendered in the UI; `[SEC-AC-001]` tests green — enabled-but-
      unprivileged is a visible failure, never a silent no-op.
- [ ] `ValidPackageName` enforced before `apt-mark` argv, fragment staging,
      and state recording; `[SEC-AC-003]` injection tests green.
- [ ] Release path unholds only state-tracked ∧ reboot-class names and
      aborts on state parse errors; `[SEC-AC-005]` tests green.
- [ ] `doctor.go` reports the sudoers probes with remediation; runbook
      includes the installer re-run step for upgraded fleets (SEC-AC-004).
- [ ] `os_update` honors `include_reboot_class`; manual `SendCommand` strips
      it; `checkAutoPatch` adds it only under `"gated"` policy; default
      dispatch params byte-identical to pre-story (`{"mode":"full"}`).
- [ ] Auto-reboot fires only when reboot-class packages actually changed
      version **and** `allow_reboot: true`; otherwise reboot-required event
      path verified (transition-once semantics).
- [ ] `uses_gpu` set from both inspect sites; **zero** new Docker API calls
      (code review check).
- [ ] `reboot_required` event type, template, and seeded rule present.
- [ ] All new telemetry/config JSON fields `omitempty`; AC-024 compat tests
      green; `cmd/riot-server/migrations/` untouched.
- [ ] UI: class badges, held-packages section, reboot-required chips
      (device + fleet), GPU container count/badges, AgentManagement toggle
      with two-sided opt-in copy; demo data exercises all states.
- [ ] `make test` green (Go + frontend); no new lint errors; no new
      dependencies in `go.mod`/`package.json`.
- [ ] README + CHANGELOG updates per §12 rollout notes staged for the
      technical-writer pass.
