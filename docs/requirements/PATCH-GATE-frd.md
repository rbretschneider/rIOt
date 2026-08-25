# Formal Requirements Document

- **Story ID:** PATCH-GATE
- **Title:** Reboot-Class Package Gating — OS-Level Holds, Maintenance-Window Apply, and Auto-Reboot
- **Author:** Business Developer Agent
- **Date:** 2026-08-25
- **Status:** FINAL

---

## 1. Executive Summary

Introduce a "reboot-class" package category — GPU driver packages (NVIDIA/AMD) and kernel packages — that is enforced at the OS level: when the feature is enabled on a device, the rIOt agent keeps these packages held (`apt-mark hold` on apt systems, a rIOt-managed `excludepkgs` configuration fragment on dnf systems) at all times outside a maintenance-window patch run. Holds are released only during an in-window automated patch run; after a run that applied reboot-class packages, the agent reboots the host automatically (gated on the existing `commands.allow_reboot` agent config) or raises a reboot-required event if reboot is not permitted. The story also adds reboot-required detection (`/var/run/reboot-required`, `dnf needs-restarting -r`), populates the currently dead `PendingKernelUpdate`/`PendingKernelVersion` telemetry fields (fixing a silent false pass in the security scoring engine), and surfaces holds, reboot-required state, and GPU-dependent container counts in the dashboard so operators understand blast radius before a driver update lands.

## 2. Background & Context

The motivating incident: an operator manually patched the NVIDIA GPU driver on a host running GPU-dependent Docker containers (NVIDIA container runtime). The kernel-module/user-space-library version mismatch broke every GPU container on the host until the machine was rebooted — an unplanned outage caused by a routine `apt upgrade`. Nothing in rIOt today prevents this: the updates collector (`internal/agent/collectors/updates.go`) only distinguishes security vs non-security packages, the auto-patch orchestration (`internal/server/handlers/auto_update.go`) applies everything upgradable when in window, and nothing stops a manual `apt upgrade` or `unattended-upgrades` from pulling a driver or kernel package at 2 PM on a workday.

Three latent gaps compound the problem:

1. The `UpdateInfo` model (internal/models/telemetry.go) carries `PendingKernelUpdate` / `PendingKernelVersion` fields that **no collector populates** — yet the security scoring engine (internal/server/scoring/engine.go) consumes them, so the kernel-update check silently always passes.
2. No reboot-required detection exists anywhere in the agent — a host can run for weeks on a stale kernel with no signal.
3. The Docker collector never reads `HostConfig.DeviceRequests`, so there is no way to see which containers depend on the GPU (the blast radius of a driver update).

Maintenance windows, remote patching (`os_update` command gated on `commands.allow_patching`), remote reboot (gated on `commands.allow_reboot`), and the events/alerting system all already exist. This story composes them with OS-level enforcement so reboot-class packages can only move during a window, and moving them always ends in a reboot.

### User Stories

- **US-1:** As a homelab operator, I want GPU driver and kernel packages held at the OS level so that neither I (running a casual `apt upgrade` over SSH) nor `unattended-upgrades` can accidentally apply them outside a maintenance window and break my GPU containers mid-day.
- **US-2:** As a homelab operator, I want reboot-class packages applied automatically during my configured maintenance window, followed by an automatic reboot, so the driver and kernel module are never left mismatched.
- **US-3:** As a homelab operator, I want to see on the dashboard which pending updates are reboot-class, which packages are currently held, and whether a host needs a reboot, so I can plan windows deliberately.
- **US-4:** As a homelab operator, I want to see how many containers on a device depend on the GPU so I understand the blast radius of a driver update before it happens.
- **US-5:** As a homelab operator, I want the security score to actually reflect pending kernel updates instead of silently passing, so a stale kernel shows up as the risk it is.
- **US-6:** As a cautious operator, I want all of this off by default and vetoable per device (`allow_reboot`, `allow_patching`), so nothing reboots a machine I did not opt in.

## 3. Actors

| Actor | Description | Permissions |
|-------|-------------|-------------|
| rIOt Agent | Go daemon on monitored devices; classifies updates, enforces holds, detects reboot-required state, executes patch runs and reboots. | Runs package-manager commands; writes rIOt-managed hold state; reboots host only when `commands.allow_reboot: true`. |
| rIOt Server | Ingests telemetry, evaluates maintenance windows, dispatches in-window `os_update` commands, evaluates scoring and alert rules. | Full read/write to database; dispatches commands to agents. |
| Dashboard User | Operator viewing the web UI and configuring automation settings. | Views classification/hold/reboot-required state; configures maintenance windows and automation. |
| Alert Engine / Events | Server subsystem generating events and notifications. | Creates reboot-required and patch-run events; fans out notifications. |
| Older Agent (pre-PATCH-GATE) | Agents not yet upgraded. | Continue to push telemetry without classification or reboot-required fields. |

## 4. Functional Requirements

### Agent — Update Classification

- **FR-001:** The updates collector must classify every pending update into exactly one class: `gpu_driver`, `kernel`, or unclassified (standard). The class must be carried on each `PendingUpdate` entry (e.g., a new `Class` field with JSON tag `class,omitempty`, empty for standard packages).
- **FR-002:** On apt systems, packages must be classified `gpu_driver` when the package name matches NVIDIA driver patterns (`nvidia-driver-*`, `nvidia-dkms-*`, `nvidia-kernel-*`, `libnvidia-*`, `nvidia-utils-*`, `xserver-xorg-video-nvidia-*`) or AMD driver patterns (`amdgpu-*` driver stack packages, `rock-dkms`, `rocm-dkms`, and ROCm kernel-module packages).
- **FR-003:** On dnf systems, packages must be classified `gpu_driver` when the package name matches NVIDIA patterns (`nvidia-driver*`, `akmod-nvidia*`, `kmod-nvidia*`, `xorg-x11-drv-nvidia*`, `nvidia-kmod*`) or AMD patterns (`amdgpu*` driver stack packages, `rocm*` kernel-module packages).
- **FR-004:** On apt systems, packages must be classified `kernel` when the package name matches `linux-image-*`, `linux-headers-*`, `linux-modules-*`, `linux-generic*` (and flavor metapackages such as `linux-image-generic`), or any package name ending in `-dkms` not already classified `gpu_driver`.
- **FR-005:** On dnf systems, packages must be classified `kernel` when the package name matches `kernel`, `kernel-core`, `kernel-modules*`, `kernel-headers`, `kernel-devel`, or any package name ending in `-dkms` not already classified `gpu_driver`.
- **FR-006:** A package matching both GPU and kernel patterns (e.g., `nvidia-dkms-550`) must be classified `gpu_driver` (GPU patterns take precedence).
- **FR-007:** The term "reboot-class" means class `gpu_driver` or `kernel`. Classification logic must be shared between apt and dnf paths (single pattern table) so the two package managers cannot drift.
- **FR-008:** When one or more pending updates are classified `kernel`, the collector must set `UpdateInfo.PendingKernelUpdate = true` and populate `UpdateInfo.PendingKernelVersion` with the new version of the primary kernel package (architect selects which package name wins when several kernel packages are pending). When no kernel updates are pending, `PendingKernelUpdate` must be `false` and `PendingKernelVersion` empty.
- **FR-009:** `UpdateInfo` must additionally expose an aggregate count of pending reboot-class updates (e.g., `PendingRebootClassCount`) so list payloads that truncate `Updates` still surface the signal.

### Agent — OS-Level Hold Enforcement

- **FR-010:** Hold enforcement must be gated by a new agent YAML config flag (e.g., `commands.hold_reboot_class`, exact name architect's choice), defaulting to `false`. When `false`, the agent must never create, modify, or remove package holds.
- **FR-011:** When hold enforcement is enabled, the agent must ensure all currently-installed reboot-class packages (matched by the FR-002–FR-006 pattern table against installed packages) are held: `apt-mark hold <pkg>` on apt systems; on dnf systems, an `excludepkgs` entry in a clearly rIOt-managed configuration fragment (exact path/mechanism architect's choice, but it must be a dedicated fragment — never an edit to a user-owned file such as `/etc/dnf/dnf.conf`).
- **FR-012:** Hold state must be reconciled periodically (at least once per telemetry cycle or on a dedicated cadence): newly installed reboot-class packages become held; holds the agent created for packages no longer installed are cleaned up.
- **FR-013:** The agent must track which holds it created (as distinct from operator-created holds) so that disabling the feature removes only rIOt-managed holds. Pre-existing operator holds must never be released by rIOt.
- **FR-014:** When hold enforcement is disabled after having been enabled (config change + agent restart/reload), the agent must remove all rIOt-managed holds and the rIOt-managed dnf fragment, restoring the system to its prior state.
- **FR-015:** During an automated in-window patch run that includes reboot-class packages (FR-020), the agent must release rIOt-managed holds immediately before the package-manager upgrade command, and re-apply holds (per FR-011 reconciliation, against the now-updated installed set) immediately after the run completes — including on failure paths (upgrade error, partial apply). Holds must never be left released after the run exits.
- **FR-016:** The agent must report currently held reboot-class packages in telemetry (e.g., a `HeldPackages []string` field on `UpdateInfo`) so the server and UI can display hold state.

### Agent — Reboot-Required Detection

- **FR-017:** The agent must detect reboot-required state on each telemetry cycle: on apt systems, the existence of `/var/run/reboot-required` (and, when present, package names from `/var/run/reboot-required.pkgs`); on dnf systems, the exit status of `dnf needs-restarting -r` (exit 1 = reboot required). Detection failures (command missing, permission denied) must degrade to "unknown/false" without error spam.
- **FR-018:** Reboot-required state must be carried in telemetry (e.g., `RebootRequired bool` plus optional `RebootRequiredReasons []string` on `UpdateInfo` or an adjacent struct; exact placement architect's choice).
- **FR-019:** The server must emit an event when a device transitions from not-requiring to requiring a reboot, and the event must be eligible for notification fan-out via the existing events system. No duplicate event must be emitted while the state remains continuously true.

### Server — Orchestration & Window Gating

- **FR-020:** The `os_update` command must gain an explicit parameter (e.g., `include_reboot_class: true`) controlling whether reboot-class packages are in scope for the run. When absent or `false`, the agent must not release holds and the run applies only non-held packages (current behavior on a system with holds).
- **FR-021:** The automated OS-patch orchestration (internal/server/handlers/auto_update.go) must set `include_reboot_class: true` only when the dispatch is occurring inside the configured `OSPatch` maintenance window (per existing `inMaintenanceWindow` / `frequencyAllowsDay` evaluation). Runs dispatched under `mode: "anytime"` must not include reboot-class packages unless the operator has explicitly opted in via a new automation setting (FR-022).
- **FR-022:** The server-side `AutomationConfig.OSPatch` settings must gain a reboot-class policy toggle (e.g., `RebootClass: "gated" | "off"`, default `"off"`) surfaced in the Agent Management settings UI. Only when enabled does the orchestrator ever set `include_reboot_class: true`. This is the server-side half of the two-sided opt-in (agent-side half is FR-010).
- **FR-023:** Manually dispatched `os_update` commands from the UI must default to excluding reboot-class packages. (Offering a manual in-window "include reboot-class" option is out of scope; the automated window run is the only release path in this story.)

### Agent — In-Window Apply & Auto-Reboot

- **FR-024:** When an `os_update` run with `include_reboot_class: true` completes and at least one reboot-class package was actually upgraded during the run, the agent must: (a) if `commands.allow_reboot: true`, report the command result to the server and then reboot the host using the existing reboot path; (b) if `commands.allow_reboot: false`, not reboot, and instead ensure the reboot-required condition is reported so the FR-019 event fires.
- **FR-025:** The agent must not reboot when the run applied only non-reboot-class packages, and must not reboot when the run failed before applying any reboot-class package.
- **FR-026:** The command result reported to the server must state whether reboot-class packages were applied and whether a reboot was initiated, so the run is auditable from the events/commands history.

### Server — Scoring Fix

- **FR-027:** The security scoring engine's kernel check (internal/server/scoring/engine.go) must operate on the now-populated `PendingKernelUpdate`/`PendingKernelVersion` fields and correctly penalize devices with pending kernel updates. The current silent false pass (fields never populated) must be eliminated.

### Docker Collector — GPU Container Correlation

- **FR-028:** The Docker collector must read each container's `HostConfig.DeviceRequests` and flag containers that request GPU devices (e.g., a `UsesGPU bool` on the container model; NVIDIA runtime requests and generic `gpu` capability requests both count).
- **FR-029:** The device UI must display a GPU-dependency indicator: the count of GPU-dependent containers on the device (e.g., "3 GPU containers"), and a per-container badge on the container list, so operators can judge the blast radius of a GPU driver update.

### Dashboard / UI

- **FR-030:** Pending-update lists in the device UI must show a visually distinct badge on reboot-class packages, distinguishing `gpu_driver` from `kernel`.
- **FR-031:** The device UI must show currently held packages (from FR-016) with an indication that they are rIOt-managed holds awaiting a maintenance window.
- **FR-032:** The device UI (and fleet-level device listing where update status is shown) must surface the reboot-required flag prominently.
- **FR-033:** The Agent Management settings page must expose the FR-022 reboot-class policy toggle alongside the existing OS-patch maintenance-window settings, with copy explaining the two-sided opt-in (server toggle + per-agent config flag).

### Backward Compatibility & Defaults

- **FR-034:** With the feature disabled on both sides (default state), all existing behavior must be unchanged: no holds are created, `os_update` behaves exactly as today, auto-patch orchestration dispatches the same commands as today, and no reboot is ever initiated by a patch run.
- **FR-035:** The server must accept telemetry from pre-PATCH-GATE agents that lack classification, held-package, and reboot-required fields, treating them as absent/false with no errors or log spam. Updated agents must remain accepted by older servers (unknown JSON fields ignored per existing convention).
- **FR-036:** No database migration must be required; new fields ride the existing telemetry JSON blob and the `AutomationConfig` JSON blob in `admin_config`.

## 5. Non-Functional Requirements

- **NFR-001:** [Safety] Hold release must be strictly scoped to a single in-window run: any code path that releases holds must guarantee re-application before the run's process exits, including on error, timeout, and partial-apply paths.
- **NFR-002:** [Safety] The agent must never reboot a host unless `commands.allow_reboot: true` — no server-side setting, command parameter, or window state may override this agent-side veto.
- **NFR-003:** [Idempotency] Hold reconciliation, hold removal on disable, and the dnf fragment write must be idempotent — running them repeatedly must converge to the same state with no accumulating side effects.
- **NFR-004:** [Performance] Classification must be pure string matching on data the updates collector already parses; it must add no additional package-manager invocations to the collection cycle. Reboot-required detection may add at most one lightweight check per cycle (file stat on apt; one `dnf needs-restarting -r` invocation on dnf, subject to the cycle's context timeout).
- **NFR-005:** [Reliability] A failure in hold enforcement or reboot-required detection must not prevent the rest of the telemetry cycle; the agent logs a warning and continues.
- **NFR-006:** [Test coverage] The classification pattern table must have unit tests per package manager covering representative NVIDIA, AMD, kernel, dkms, and look-alike-but-standard package names (e.g., `linux-firmware`, `libnvidia-container-tools`' classification decision must be an explicit, tested choice).
- **NFR-007:** [Observability] Hold apply/release/re-apply actions and auto-reboot initiation must be logged at INFO with package lists, since these are system-mutating actions an operator will want to audit.

## 6. Business Rules

- **BR-001:** Reboot-class = GPU driver packages (NVIDIA and AMD) plus kernel/dkms packages. No other package (glibc, systemd, container runtimes) is reboot-class in this story.
- **BR-002:** The feature is a two-sided opt-in: the server-side automation toggle (FR-022) and the agent-side config flag (FR-010) must both be enabled for gating to be active on a device. Either side alone changes nothing.
- **BR-003:** Auto-reboot additionally requires `commands.allow_reboot: true`; automated patch runs additionally require the existing `commands.allow_patching: true`. Existing veto semantics are never weakened.
- **BR-004:** rIOt releases only holds it created. Operator-created holds (manual `apt-mark hold`, user-authored dnf excludes) are invisible to and untouched by rIOt.
- **BR-005:** Holds are the steady state; release is the exception. The only release path is an in-window automated patch run with `include_reboot_class: true`. There is no "temporarily unhold" UI action in this story.
- **BR-006:** Defaults ship safe: server toggle `off`, agent flag `false`, consistent with `DefaultAutomationConfig()` shipping OS patching `disabled`.

## 7. Data Requirements

### Entities Involved

- **PendingUpdate** (internal/models/telemetry.go): gains `Class string` (`gpu_driver` | `kernel` | empty).
- **UpdateInfo**: `PendingKernelUpdate` / `PendingKernelVersion` become populated (no schema change); gains pending reboot-class count, held-packages list, and reboot-required state (exact field placement architect's choice; all JSON `omitempty`).
- **Container model** (Docker telemetry): gains `UsesGPU bool` (or equivalent) derived from `HostConfig.DeviceRequests`.
- **AutomationConfig.OSPatch** (internal/models/automation.go): gains the reboot-class policy field; stored in the existing `admin_config` JSON blob.
- **Agent YAML config**: gains the hold-enforcement flag under the existing `commands:` section (or adjacent; architect's choice), documented in the config template in internal/agent/config.go.
- **os_update command params**: gains `include_reboot_class` boolean.

### Validation Rules

- `Class` is one of `""`, `gpu_driver`, `kernel` — no other values.
- `PendingKernelVersion` is non-empty iff `PendingKernelUpdate` is true.
- Held-package names are the exact package-manager names; the list may be empty.
- `include_reboot_class` absent ⇒ false.

### State Transitions (hold lifecycle per device, feature enabled)

| From | Event | To | Action |
|------|-------|----|--------|
| Unmanaged | Agent flag enabled + reconcile | Held | rIOt holds all installed reboot-class packages |
| Held | New reboot-class package installed/appears | Held | Reconcile adds hold for new package |
| Held | In-window run with `include_reboot_class: true` starts | Released (run-scoped) | rIOt-managed holds released |
| Released (run-scoped) | Run completes (success, failure, or partial) | Held | Holds re-applied against updated installed set |
| Held | Agent flag disabled | Unmanaged | All rIOt-managed holds and fragment removed; operator holds untouched |

### Reboot-required state

| From | Event | To | Server Action |
|------|-------|----|---------------|
| false | Detection turns true | true | Emit reboot-required event (once) |
| true | Detection remains true | true | No new event |
| true | Host reboots; detection turns false | false | State clears; next transition may fire again |

## 8. Acceptance Criteria

### AC-001: apt GPU driver packages classified `gpu_driver` [Maps to FR-001, FR-002, FR-006]
- **Given** an apt system with pending updates `nvidia-driver-550`, `libnvidia-compute-550`, `nvidia-dkms-550`, and `rock-dkms`
- **When** the updates collector parses `apt list --upgradable`
- **Then** all four entries carry `class: "gpu_driver"`
- **And** `nvidia-dkms-550` is `gpu_driver`, not `kernel` (GPU precedence)

### AC-002: apt kernel packages classified `kernel` [Maps to FR-001, FR-004]
- **Given** an apt system with pending updates `linux-image-6.8.0-45-generic`, `linux-headers-6.8.0-45-generic`, `linux-modules-6.8.0-45-generic`, and `zfs-dkms`
- **When** the updates collector parses the pending list
- **Then** all four entries carry `class: "kernel"`

### AC-003: dnf GPU driver packages classified `gpu_driver` [Maps to FR-001, FR-003, FR-006]
- **Given** a dnf system with pending updates `akmod-nvidia`, `xorg-x11-drv-nvidia-cuda`, and `kmod-nvidia-latest-dkms`
- **When** the updates collector parses `dnf check-update`
- **Then** all three entries carry `class: "gpu_driver"`

### AC-004: dnf kernel packages classified `kernel` [Maps to FR-001, FR-005]
- **Given** a dnf system with pending updates `kernel`, `kernel-core`, `kernel-modules-extra`, and `kernel-devel`
- **When** the updates collector parses the pending list
- **Then** all four entries carry `class: "kernel"`

### AC-005: Standard packages remain unclassified [Maps to FR-001, FR-007]
- **Given** pending updates `curl`, `openssl`, `linux-firmware`, and `docker-ce` on either package manager
- **When** the updates collector classifies them
- **Then** each entry's `class` is empty (standard)
- **And** the same pattern table produced the decision for both package managers

### AC-006: Dead kernel fields are populated [Maps to FR-008]
- **Given** a pending update `linux-image-6.8.0-45-generic` upgrading to version `6.8.0-45.45`
- **When** the updates collector builds `UpdateInfo`
- **Then** `pending_kernel_update` is `true`
- **And** `pending_kernel_version` is non-empty and reflects the pending kernel version
- **And** with no pending kernel packages, `pending_kernel_update` is `false` and `pending_kernel_version` is empty

### AC-007: Scoring engine kernel check no longer silently passes [Maps to FR-027]
- **Given** a device whose latest telemetry has `pending_kernel_update: true`
- **When** the security scoring engine evaluates the device
- **Then** the kernel check fails/penalizes the score
- **And** a device with `pending_kernel_update: false` passes the kernel check

### AC-008: Holds applied on apt when feature enabled [Maps to FR-010, FR-011, FR-012]
- **Given** an agent with hold enforcement enabled on an apt system with installed packages `nvidia-driver-550` and `linux-image-generic`
- **When** the agent runs its hold reconciliation
- **Then** both packages are held via `apt-mark hold`
- **And** the holds are recorded as rIOt-managed

### AC-009: Excludes applied on dnf when feature enabled [Maps to FR-010, FR-011]
- **Given** an agent with hold enforcement enabled on a dnf system with installed packages `akmod-nvidia` and `kernel-core`
- **When** the agent runs its hold reconciliation
- **Then** a rIOt-managed configuration fragment exists containing `excludepkgs` entries covering both packages
- **And** no user-owned dnf configuration file was modified

### AC-010: Holds released for the in-window run and re-applied after [Maps to FR-015, FR-020, NFR-001]
- **Given** a device with rIOt-managed holds and an in-window `os_update` command with `include_reboot_class: true`
- **When** the agent executes the run
- **Then** rIOt-managed holds are released immediately before the upgrade command
- **And** holds are re-applied against the updated installed set immediately after the run completes
- **And** if the upgrade command fails mid-run, holds are still re-applied before the command handler returns

### AC-011: Disabling the feature removes only rIOt-managed holds [Maps to FR-013, FR-014, BR-004]
- **Given** a device where rIOt holds `nvidia-driver-550` and the operator separately ran `apt-mark hold postgresql-16`
- **When** the agent's hold-enforcement flag is disabled and the agent reloads
- **Then** the `nvidia-driver-550` hold is removed
- **And** the `postgresql-16` hold remains
- **And** on dnf systems the rIOt-managed fragment is deleted

### AC-012: Manual out-of-window upgrade cannot pull reboot-class packages [Maps to FR-011, US-1]
- **Given** a device with hold enforcement active and a pending `nvidia-driver-550` update
- **When** anything other than an in-window rIOt patch run performs an upgrade (operator `apt upgrade` / `dnf upgrade`, or `unattended-upgrades`)
- **Then** the reboot-class packages are skipped by the package manager (held/excluded)
- **And** non-reboot-class packages upgrade normally

### AC-013: Out-of-window automated runs exclude reboot-class packages [Maps to FR-020, FR-021, FR-023]
- **Given** the server-side reboot-class policy is enabled and the current time is outside the OSPatch maintenance window
- **When** the auto-patch orchestrator dispatches (or an operator manually dispatches) an `os_update`
- **Then** the command does not carry `include_reboot_class: true`
- **And** the agent does not release any holds during that run

### AC-014: In-window apply then auto-reboot when allowed [Maps to FR-021, FR-024, FR-026]
- **Given** a device in its OSPatch maintenance window with reboot-class updates pending, the server policy enabled, agent hold enforcement enabled, `commands.allow_patching: true`, and `commands.allow_reboot: true`
- **When** the automated patch run executes and upgrades at least one reboot-class package
- **Then** the agent reports a command result stating reboot-class packages were applied and a reboot is initiated
- **And** the agent reboots the host

### AC-015: In-window apply without reboot permission raises event instead [Maps to FR-019, FR-024, NFR-002]
- **Given** the same setup as AC-014 except `commands.allow_reboot: false`
- **When** the automated patch run applies a reboot-class package
- **Then** the host is not rebooted
- **And** the device's reboot-required state becomes true and a reboot-required event is emitted

### AC-016: No reboot when no reboot-class package was applied [Maps to FR-025]
- **Given** an in-window run with `include_reboot_class: true` where the only pending updates were standard packages (or the run failed before applying anything)
- **When** the run completes
- **Then** the agent does not reboot the host

### AC-017: Reboot-required detection on apt [Maps to FR-017, FR-018]
- **Given** an apt system where `/var/run/reboot-required` exists and `/var/run/reboot-required.pkgs` lists `linux-image-6.8.0-45-generic`
- **When** the agent runs a telemetry cycle
- **Then** telemetry reports reboot-required as true with the package name among the reasons
- **And** when the file is absent, reboot-required is false

### AC-018: Reboot-required detection on dnf [Maps to FR-017, FR-018]
- **Given** a dnf system where `dnf needs-restarting -r` exits 1
- **When** the agent runs a telemetry cycle
- **Then** telemetry reports reboot-required as true
- **And** exit 0 reports false; a missing/failing command reports false without error-level log spam

### AC-019: Reboot-required event fires once per transition [Maps to FR-019]
- **Given** a device whose reboot-required state transitions from false to true
- **When** the server ingests the telemetry
- **Then** exactly one reboot-required event is emitted and is eligible for notification fan-out
- **And** subsequent telemetry with the state still true emits no further events
- **And** after the state clears (post-reboot) and later becomes true again, a new event is emitted

### AC-020: Pending updates show reboot-class badges [Maps to FR-030]
- **Given** a device with pending updates of classes `gpu_driver`, `kernel`, and standard
- **When** the operator views the device's pending updates in the UI
- **Then** GPU driver and kernel entries carry visually distinct badges
- **And** standard entries carry no reboot-class badge

### AC-021: Held packages and reboot-required flag surfaced in UI [Maps to FR-016, FR-031, FR-032]
- **Given** a device reporting held packages and reboot-required true
- **When** the operator views the device in the UI
- **Then** the held packages are listed as rIOt-managed holds awaiting a maintenance window
- **And** a reboot-required indicator is shown on the device view and in fleet-level update status

### AC-022: GPU-dependent containers counted and badged [Maps to FR-028, FR-029]
- **Given** a device running 5 containers of which 3 have GPU `DeviceRequests` in their `HostConfig`
- **When** the operator views the device
- **Then** the device shows a "3 GPU containers" indicator
- **And** each of the 3 containers carries a GPU badge in the container list
- **And** a device with no GPU containers shows no such indicator

### AC-023: Feature off means behavior unchanged [Maps to FR-034, BR-002, BR-006]
- **Given** default configuration (server toggle off, agent flag false)
- **When** telemetry cycles, manual upgrades, and automated in-window patch runs occur
- **Then** no holds or dnf fragments are ever created
- **And** `os_update` dispatches and results are byte-for-byte compatible with pre-PATCH-GATE behavior (no `include_reboot_class` gating effects)
- **And** no patch run ever initiates a reboot

### AC-024: Old agents and old servers remain compatible [Maps to FR-035, FR-036]
- **Given** a pre-PATCH-GATE agent pushing telemetry without classification, held-package, or reboot-required fields
- **When** a PATCH-GATE server ingests it
- **Then** the push is accepted with a `2xx` response, absent fields treated as empty/false, and no warnings logged
- **And** a PATCH-GATE agent's telemetry is accepted by an older server (unknown fields ignored)
- **And** no database migration ran for this story

## 9. Out of Scope

- Windows and macOS hold/patch-gating support (Linux apt/dnf only).
- pacman, apk, zypper, and other package managers.
- GPU driver rollback or downgrade orchestration.
- Container restart/recreation orchestration after reboot (containers restart per their own Docker restart policies).
- A manual "unhold now" / "patch reboot-class now" UI action outside the automated window run.
- Intel GPU driver classification.
- Broadening reboot-class beyond GPU drivers and kernel/dkms (e.g., glibc, systemd).
- Live-patching integration (kpatch, livepatch, kernelcare).
- Reboot scheduling/countdown UX beyond the immediate post-run reboot (no "reboot at end of window" scheduler).
- Detecting driver/library mismatch at runtime (e.g., parsing `nvidia-smi` failures) — this story prevents the mismatch rather than diagnosing it.

## 10. Assumptions

- **A-001:** `apt-mark hold` / `apt-mark unhold` and dnf `excludepkgs` are sufficient OS-level enforcement: apt full-upgrades, `unattended-upgrades`, and dnf upgrades all respect them by default. Force flags (`--allow-change-held-packages`, `--disableexcludes`) are deliberate operator overrides and out of enforcement scope.
- **A-002:** The agent runs with sufficient privilege to execute `apt-mark`, write the dnf fragment, and reboot, as it already does for the existing `os_update` and `reboot` command paths.
- **A-003:** The updates collector's existing apt/dnf parsing provides package names in a form the pattern table can match without additional package-manager queries.
- **A-004:** The existing events system's notification fan-out and any event-deduplication conventions can host the reboot-required event; the architect decides the event type/severity naming to match the seeded alert conventions in internal/server/server.go.
- **A-005:** `HostConfig.DeviceRequests` is available from the Docker API version the collector already negotiates; NVIDIA GPU requests appear as device requests with the `gpu` capability or the nvidia driver.
- **A-006:** Exact config key names, telemetry field placement, dnf fragment path, and the rIOt-managed-hold bookkeeping mechanism (state file vs derived-from-pattern-table) are architect decisions; this FRD constrains behavior, not naming.
- **A-007:** The maintenance-window evaluation (`inMaintenanceWindow`, `frequencyAllowsDay`, cooldowns) is correct as shipped; this story reuses it unchanged for the reboot-class gate.

## 11. Open Questions

None. The three headline decisions (OS-level hold enforcement, auto-reboot in window gated on `commands.allow_reboot`, reboot-class = GPU drivers + kernel) were made with the user and are final. Remaining choices are explicitly delegated to the architect per Assumption A-006.

## 12. Dependencies

- **D-001:** Existing maintenance-window machinery — `AutomationConfig`/`MaintenanceWindow` (internal/models/automation.go) and evaluation in internal/server/handlers/auto_update.go. Confirmed shipped.
- **D-002:** Existing remote-patching path — `os_update` in the server command allowlist (internal/server/handlers/commands.go) and agent execution gated on `commands.allow_patching` (internal/agent/commands.go). Confirmed shipped.
- **D-003:** Existing remote-reboot path gated on `commands.allow_reboot`. Confirmed shipped.
- **D-004:** Existing events/notification system (internal/server/events/) for the reboot-required event.
- **D-005:** Updates collector (internal/agent/collectors/updates.go) apt/dnf parsing — extended, not replaced.
- **D-006:** Docker collector (internal/agent/collectors/docker.go) — extended to read `HostConfig.DeviceRequests`.
- **D-007:** No new third-party libraries, no database migration, no new external services. All enforcement uses OS-native tooling (`apt-mark`, dnf configuration, `dnf needs-restarting`).
- **D-008:** Agent collector whitelist convention — any new/renamed collector behavior must respect the existing `collectors.enabled` whitelist and be reflected in `internal/agent/doctor.go` and agent config documentation.
