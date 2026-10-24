# Formal Requirements Document

- **Story ID:** PATCH-FLOW-001
- **Title:** Logically sound manual patch flow — truthful "reboot needed", opt-in reboot on manual Patch, and no phantom pending counts
- **Author:** Business Developer Agent
- **Date:** 2026-10-23
- **Status:** DRAFT (all product decisions made by Ryan; open questions in Section 11 carry recommended defaults)
- **Amends:** PATCH-GATE FR-023, BR-005 and its Out-of-Scope item "manual patch reboot-class now" (see BR-003). The per-host patch badge decision rule NY-10 (`internal/server/handlers/patch_badge.go`) changes. REBOOT-ETA-001 indicators are confirmed, not changed (FR-007).

---

## 1. Executive Summary

The manual patch flow currently contradicts itself. It tells the operator to reboot before the kernel is even installed. A run that rewrites apt configuration leaves behind a pending count of packages that do not exist. And the operator cannot authorize, as part of a manual patch, the reboot the patch will need. This story makes the flow consistent from start to finish. "Reboot needed" means a reboot is required now. A pending kernel/GPU update is described as a future reboot. The manual Patch confirmation gains an opt-in checkbox, "Reboot automatically if the patch requires it". When ticked, the checkbox also releases rIOt's reboot-class holds for that one run, and the agent reboots only if a reboot is actually required after the run. Stale apt package lists are reported as stale instead of as an actionable "Patch (N)". The maintenance helper refreshes the lists after a run that changed apt configuration. The agent reports fresh update state right after a patch run and right after it starts.

## 2. Background & Context

### User story (Ryan)

"I'm lost reading the patch button. It said the host needs a reboot. I rebooted. I installed the patches. Then there were MORE patches and it wanted ANOTHER reboot. Make the patch flow logically sound. If it needs a reboot, add a checkbox to confirm reboot as part of the process."

### Incident evidence (host `vermont`, Debian 13 + OpenMediaVault, 2026-10-23 local time)

| Time | Observed state | Problem |
|------|----------------|---------|
| until 22:44 | `pending_updates=7`, `pending_reboot_class_count=1` (linux-image 7.2.9 pending), `reboot_required=false`. The badge raised "A reboot is needed and no automatic reboot is set up ... Reboot this host by hand." | **D1.** The badge computes `rebootExpected := RebootRequired \|\| PendingRebootClassCount > 0`, so it asked for a reboot that would do nothing. The kernel was not installed yet. |
| 22:44–22:47 | Manual Patch (`os_update`, `{mode: full}`) through the `riot-maintenance` helper: configure, verify, refresh (`apt-get update`), `dist-upgrade`. Kernel 7.2.9 and 8 other packages installed. Telemetry then showed `reboot_required=true`, pending 0. | Correct. But the operator had no way to authorize the reboot inside the same action (**F1**). |
| 22:49–22:51 | Reboot through rIOt; host booted 7.2.9. | — |
| 22:53–22:57 | Agent reported `pending_updates=38`, all trixie-backports versions. UI showed "Patch (38)". | **D2.** These updates were phantom. At 22:46:32 the `openmediavault-omvextrasorg` postinst rewrote `/etc/apt/sources.list.d/omvextras.sources` and `/etc/apt/preferences.d/omvextras.pref`. That was after the run's refresh step, so `apt list --upgradable` read lists that did not match the configuration. |
| 22:57 | Second manual Patch: refresh, then `dist-upgrade` → "0 upgraded". Telemetry back to 0. | The only fix was to run a patch that did nothing. |

This story fixes two defects (D1, D2) and adds one missing feature (F1). It also closes the multi-minute stale-telemetry window seen after the run and after the reboot (D6).

## 3. Actors

| Actor | Description | Permissions |
|-------|-------------|-------------|
| Operator (Ryan / dashboard user) | Authenticated dashboard user who opens the Patch confirmation, may tick the reboot checkbox, and may use the Reboot action. | Dispatches manual `os_update` and `reboot` commands through the existing authenticated dashboard API. |
| rIOt Server | Validates manual dispatches, derives `include_reboot_class`, renders the per-host patch badge text, and serves patch-status data. | Creates commands. Never trusts agent-reported facts to widen what a command may do (BR-007). |
| rIOt Agent | Collects update telemetry, carries out `os_update`, decides whether to reboot after an authorized run, and reports results. | Bound by the agent-side vetoes `commands.allow_patching` and `commands.allow_reboot`, which nothing overrides. |
| Maintenance helper (`riot-maintenance`) | Privileged helper that runs the apt steps of a patch run. Updated only when `install.sh` is re-run on the device. | Runs package-manager commands within its existing budget and bounded output. |
| Older agent | An agent released before this story. It does not report list freshness or support for reboot authorization. | Keeps working with the behavior defined in FR-011, FR-018 and FR-037. |
| Older helper | A helper installed before this story (`install.sh` not re-run). | Performs no post-run refresh. The agent's stale flag (FR-031) covers this case. |

## 4. Functional Requirements

### A. "Reboot needed" means required now (D1)

- **FR-001:** The server must treat a host as "reboot needed now" if, and only if, the host's latest updates telemetry reports `reboot_required == true`.
- **FR-002:** The agent must report `reboot_required == true` when at least one of these holds:
  - (apt) the reboot-required marker exists;
  - (apt) the newest installed kernel version is newer than the running kernel version;
  - (dnf) `dnf needs-restarting -r` reports that a reboot is required.

  Otherwise the agent must report `false`. A detection source that cannot be evaluated counts as "not required" for that source, as in PATCH-GATE FR-017.
- **FR-003:** The per-host patch badge must not show the manual-reboot needs-you reason, or any text telling the operator to reboot, when `reboot_required` is false. This includes hosts whose `pending_reboot_class_count` is greater than zero.
- **FR-004:** When `reboot_required` is false and `pending_reboot_class_count` is greater than zero (and package lists are not stale, FR-036), the badge's Reboot line must say that the pending patch includes a kernel/GPU update and that a reboot will be needed after it installs. The line must also say whether that reboot will happen automatically (an automatic reboot path is set up) or not.
- **FR-005:** When `reboot_required` is true and no automatic reboot path applies, the badge must show the manual-reboot needs-you reason. Its action must tell the operator to reboot the host now.
- **FR-006:** The cause list in the manual-reboot reason must not contain a statement that contradicts the telemetry it was derived from. For example, it must not offer "no kernel or GPU update is pending" as a cause for a reboot that is required now.
- **FR-007:** The reboot-required indicators defined by REBOOT-ETA-001 (Fleet Overview panel and grid) and PATCH-GATE FR-032 must stay keyed on `reboot_required` only. They must not appear for a host whose only reboot-related signal is a pending reboot-class update.

### B. Manual Patch with opt-in reboot (F1)

- **FR-008:** The manual Patch confirmation dialog must contain a checkbox labelled exactly "Reboot automatically if the patch requires it".
- **FR-009:** The checkbox must be unchecked every time the dialog opens. Its state must not persist between dialog openings, devices, or sessions.
- **FR-010:** When the host's latest telemetry reports `allow_reboot == false`, the checkbox must be disabled and the dialog must show the reason: reboots are turned off on this agent (`commands.allow_reboot: false`).
- **FR-011:** The checkbox must be disabled when the host's latest telemetry does not report `allow_reboot`, or does not report support for reboot authorization (older agent). The dialog must show the reason: the agent must be updated to use this option.
- **FR-012:** The dialog must say what happens to pending kernel/GPU (reboot-class) updates in the current situation:
  - (a) rIOt holds are active and the box is unticked: the kernel/GPU updates will be held back and not installed.
  - (b) The box is ticked: the kernel/GPU updates will be installed, and the host will reboot after the run if a reboot is required.
  - (c) rIOt holds are not active and the box is unticked: the kernel/GPU updates will be installed, a reboot will be needed afterwards, and rIOt will not reboot the host.

  When no reboot-class update is pending, none of these statements is shown.
- **FR-013:** When `reboot_required` is already true at the time the dialog opens, the dialog must say that a reboot is already required. With the box ticked, the host will reboot after the run.
- **FR-014:** A manual `os_update` dispatched with the box ticked must carry an explicit reboot-authorization parameter set to true. A dispatch with the box unticked must carry it as false or omit it.
- **FR-015:** The server must set `include_reboot_class = true` on a manual `os_update` if, and only if, the request carries reboot authorization = true and passes FR-017 and FR-018. This amends PATCH-GATE FR-023/BR-005.
- **FR-016:** The server must keep removing any client-supplied `include_reboot_class` from manual `os_update` requests. The value must come only from FR-015.
- **FR-017:** The server must reject a manual `os_update` whose reboot-authorization parameter is present but not a boolean. The response is HTTP 400 `invalid_request`, and no command is created.
- **FR-018:** The server must reject a manual `os_update` that carries reboot authorization = true when the host's latest telemetry reports `allow_reboot == false`, or does not report support for reboot authorization. The response is HTTP 400 with an error code distinct from `invalid_request` that names the reason. No command is created.
- **FR-019:** When an `os_update` carrying reboot authorization = true finishes with a successful outcome, the agent must evaluate reboot-required using the FR-002 definition. It must reboot the host if, and only if, the result is true and `commands.allow_reboot` is true.
- **FR-020:** The agent must not reboot after an authorized run when reboot-required is false after the run.
- **FR-021:** The agent must not reboot after an authorized run whose outcome is failed, timed out, or interrupted (see OQ-001).
- **FR-022:** The agent must not reboot when `commands.allow_reboot` is false, whatever the command parameters say. PATCH-GATE NFR-002 still applies.
- **FR-023:** The agent must report the command result to the server before it starts a reboot. The result must state:
  - whether a reboot was authorized;
  - whether a reboot was required after the run;
  - whether a reboot was started;
  - when no reboot was started, the reason: not required, `allow_reboot` off, or run not successful.
- **FR-024:** The agent must start at most one reboot per command. This holds when the command is redelivered and when the agent restarts after that reboot.
- **FR-025:** A manual `os_update` without reboot authorization must behave as it does today: `include_reboot_class` is removed and the run never starts a reboot.
- **FR-026:** The automated OS-patch orchestration (`checkAutoPatch`) and its reboot behavior (PATCH-GATE FR-021, FR-024) must not change.
- **FR-027:** When the host reports zero actionable pending updates (lists not stale) and `reboot_required == true`, the device page's primary action must be Reboot, using the existing `reboot` command and its existing confirmation. Patch must not be the primary action in that state.
- **FR-028:** In the FR-027 state, when the host reports `allow_reboot == false`, the Reboot action must be disabled with a visible reason saying the host must be rebooted outside rIOt.

### C. Stale package lists, agent and server side (D2, ships with the agent update)

- **FR-029:** On apt hosts, the agent must judge the package lists stale when any file in `/etc/apt/sources.list`, `/etc/apt/sources.list.d/` or `/etc/apt/preferences.d/` was modified after the package lists were last refreshed.
- **FR-030:** The agent must judge the lists stale when no package lists exist.
- **FR-031:** The agent must report package-list freshness in updates telemetry as one of `fresh`, `stale` or `unknown`. `unknown` is reported when the check could not finish (error or the NFR-004 bound reached).
- **FR-032:** When the lists are stale, the agent must still report the raw upgradable count and list for diagnostics, together with the `stale` flag.
- **FR-033:** The server must accept only the values defined in FR-031. It must treat an absent field (older agent) or any other value as `unknown`.
- **FR-034:** When freshness is `stale`, the device page's Patch button must show "Package lists out of date — refresh pending" instead of "Patch (N)", with no package count. The Patch action must stay available, because a run refreshes the lists first.
- **FR-035:** When freshness is `stale`, the device's row in the Fleet Overview Pending OS Patches panel must show the stale label instead of a pending count.
- **FR-036:** When freshness is `stale`, the server-rendered badge:
  - must not show the FR-004 pending kernel/GPU wording;
  - must not raise any needs-you reason derived only from the pending update list;
  - must add a note that package lists are out of date and pending updates will be recounted after the next refresh.
- **FR-037:** When freshness is `fresh` or `unknown`, all pending-count displays and badge text must behave as specified elsewhere in this FRD. Older agents therefore keep today's count display.
- **FR-038:** On dnf hosts the agent must not report `stale`. dnf hosts report `fresh` or omit the field.
- **FR-039:** Freshness must be display-only. It must not change any automated dispatch decision.

### D. Post-run list refresh, helper side (D2, ships only when `install.sh` is re-run)

- **FR-040:** The maintenance helper must detect whether any file in the FR-029 locations changed between the start of the run's refresh step and the end of its upgrade step.
- **FR-041:** When FR-040 detects a change, the helper must run one more package-list refresh after the upgrade step and before it reports completion.
- **FR-042:** The helper must run at most one additional refresh per run.
- **FR-043:** The helper must not run the additional refresh when no change was detected, or when the run's mode does not include an upgrade step (`repair`).
- **FR-044:** When the additional refresh fails, the run's outcome must stay as the upgrade and verification decided it. The run result must state that the post-run list refresh failed (see OQ-002).
- **FR-045:** The output of the additional refresh must be appended to the run output under the existing output bound.

### E. Fresh update state after a run and after startup (D6)

- **FR-046:** After an `os_update` command reaches a terminal outcome on the agent, the agent must collect and send updates telemetry within 60 seconds of reporting the result. This applies to every outcome, except when the agent is starting a reboot under FR-019.
- **FR-047:** After the agent starts (including after a host boot), it must collect and send its first updates telemetry within 60 seconds.
- **FR-048:** Each FR-046 or FR-047 trigger must cause at most one extra updates collection. An extra collection must never run at the same time as another updates collection; if they would overlap, they are merged into one collection.

## 5. Non-Functional Requirements

- **NFR-001 [Safety]:** No server setting, command parameter, dialog state, or telemetry value may override the agent-side `commands.allow_reboot` or `commands.allow_patching` vetoes.
- **NFR-002 [Safety]:** Holds released for an authorized manual run must be restored when the run ends, on every outcome. PATCH-GATE NFR-001 applies to manual runs exactly as it does to automated ones.
- **NFR-003 [Security]:** The server must derive `include_reboot_class` for manual dispatches only from its own validation (FR-015 to FR-018). Agent-reported facts may narrow what the server allows (reject a dispatch). They must never widen it.
- **NFR-004 [Bounded growth — agent]:**
  - The freshness check must examine at most 1,000 entries per collection and must finish within 2 seconds; otherwise it reports `unknown`.
  - It must create no persistent files.
  - It must log at most one line per collection, at debug level.
- **NFR-005 [Bounded growth — helper]:**
  - At most one additional refresh per run (FR-042).
  - The run's total output stays within the existing helper output bound.
  - The additional refresh runs within the existing worker time budget.
- **NFR-006 [Bounded growth — reboots]:** At most one reboot per command (FR-024). An authorized command can never cause a reboot loop.
- **NFR-007 [Bounded growth — telemetry]:** At most one extra updates collection per patch run and per agent start (FR-048).
- **NFR-008 [Bounded growth — server]:**
  - New telemetry fields are a bounded enum and a boolean.
  - The new command parameter is a boolean.
  - No new table, no database migration, and no new retained record type. Command records keep their existing retention.
- **NFR-009 [Compatibility]:**
  - Telemetry from older agents must be accepted with `2xx`, with new fields treated as defined in FR-011, FR-018 and FR-033, and with no warnings logged.
  - Telemetry from new agents must be accepted by older servers, which ignore unknown fields.
- **NFR-010 [Auditability]:** The command record of an authorized manual run must show that reboot authorization was given, that `include_reboot_class` was set, and the FR-023 result fields. The existing command-sent event must carry the authorization parameter.
- **NFR-011 [Performance]:** The dialog checkbox state, its disabled reasons, the FR-012/FR-013 statements and the FR-027 primary action must use data the device page already loads. No additional network request is allowed.

## 6. Business Rules

- **BR-001:** "Reboot needed" means a reboot is required now, as defined in FR-002. A pending, not yet installed, kernel/GPU update is a future reboot. The operator is never told to reboot because of it.
- **BR-002:** Reboot authorization is explicit and applies to one command. It is off by default, never persisted, and never inferred.
- **BR-003 (amends PATCH-GATE FR-023 and BR-005):** There are now two release paths for reboot-class holds: the in-window automated run (unchanged), and a manual run whose operator ticked the reboot checkbox. Rationale: holds exist so that kernels do not install without a planned reboot, and the tick is the operator's explicit authorization of that reboot. Without the tick, the server still removes `include_reboot_class`.
- **BR-004:** An authorized run reboots only if a reboot is required after the run. Ticking the box never means "reboot unconditionally".
- **BR-005:** A pending count read from stale package lists is not actionable and must never be shown as "Patch (N)".
- **BR-006:** Agents are untrusted. The server re-validates every agent-reported fact it uses, and those facts can only narrow what the server allows.
- **BR-007:** The per-host patch badge text is rendered by the server.
- **BR-008:** Every failure path has a stated bound (NFR-004 to NFR-008). No path may grow disk, database, logs or reboots without limit.

## 7. Data Requirements

### Entities involved

- **UpdateInfo (updates telemetry):**
  - `reboot_required` gets the widened definition in FR-002 (no schema change).
  - Gains a package-list freshness field (`fresh` | `stale` | `unknown`, absent = `unknown`).
  - Gains a capability fact saying the agent supports reboot authorization (absent = not supported).
  - Field names are the architect's choice. All fields are additive and `omitempty`.
- **`os_update` command params:**
  - Gain a boolean reboot-authorization parameter (absent = false).
  - `include_reboot_class` stays server-derived (FR-015, FR-016).
- **Command result (agent → server):** Gains the four FR-023 facts, carried in the existing result structure or message within existing size limits.
- **Patch badge (server-rendered):** The Reboot line, the needs-you reasons and the notes change as described in FR-003 to FR-006 and FR-036. The response shape is unchanged or additive.

### Validation rules

- Reboot authorization: boolean or absent. Anything else is rejected with 400 (FR-017).
- Freshness: exactly `fresh`, `stale` or `unknown`. Anything else is treated as `unknown` (FR-033).
- Capability fact: boolean or absent. Anything else is treated as not supported.

### Device patch-state decision table (device page)

| # | Freshness | Actionable pending | `reboot_required` | Primary action | Patch label | Badge reboot wording |
|---|-----------|--------------------|-------------------|----------------|-------------|----------------------|
| S1 | fresh/unknown | 0 | false | none (up to date) | as today | Not needed |
| S2 | fresh/unknown | N > 0, no reboot-class | false | Patch | Patch (N) | Not needed |
| S3 | fresh/unknown | N > 0, includes reboot-class | false | Patch (dialog offers checkbox) | Patch (N) | "Includes a kernel/GPU update; a reboot will be needed after it installs" + auto/not auto (FR-004) |
| S4 | fresh/unknown | 0 | true | Reboot (FR-027) | Patch is secondary | Reboot needed now (FR-005) |
| S5 | fresh/unknown | N > 0 | true | Patch (dialog states reboot already required, FR-013) | Patch (N) | Reboot needed now |
| S6 | stale | any | any | Patch (refreshes first) | "Package lists out of date — refresh pending" | Stale note; `reboot_required` wording still applies if true |

### Authorized manual run lifecycle

| From | Event | To | Action |
|------|-------|----|--------|
| Dialog open | Operator ticks box and confirms | Dispatch requested | Server validates (FR-017, FR-018) |
| Dispatch requested | Validation fails | Rejected | 400 with reason. No command created |
| Dispatch requested | Validation passes | Queued | Command created with authorization = true and `include_reboot_class` = true |
| Running | Run ends: failed, timed out or interrupted | Terminal (no reboot) | Holds restored. Result states "run not successful" (FR-021) |
| Running | Run succeeds, reboot not required | Terminal (no reboot) | Result states "not required". Fresh telemetry ≤ 60 s (FR-046) |
| Running | Run succeeds, reboot required, `allow_reboot` false | Terminal (no reboot) | Result states "`allow_reboot` off". Reboot-required event fires (PATCH-GATE FR-019) |
| Running | Run succeeds, reboot required, `allow_reboot` true | Terminal → Rebooting | Result reported first (FR-023), then a single reboot (FR-024) |
| Rebooting | Agent starts | Reporting | Updates telemetry ≤ 60 s after start (FR-047). No second reboot |

## 8. Acceptance Criteria

### AC-001: Pending kernel does not trigger "reboot by hand" [Maps to FR-001, FR-003]
- **Given** a host with `reboot_required=false`, `pending_reboot_class_count=1`, fresh lists, and no automatic reboot path
- **When** the server computes the patch badge
- **Then** no manual-reboot needs-you reason is present
- **And** no badge text tells the operator to reboot the host

### AC-002: Pending kernel is described as a future reboot [Maps to FR-004]
- **Given** the host from AC-001
- **When** the server computes the patch badge
- **Then** the Reboot line says that the pending patch includes a kernel/GPU update and a reboot will be needed after it installs
- **And** the line says the reboot will not happen automatically

### AC-003: Pending kernel with automatic reboot set up is described as automatic [Maps to FR-004]
- **Given** a host with `reboot_required=false`, `pending_reboot_class_count=1`, fresh lists, auto-patch on, `reboot_class` gated, and `allow_reboot=true`
- **When** the server computes the patch badge
- **Then** the Reboot line describes the future reboot as automatic

### AC-004: Required-now reboot raises the manual-reboot reason [Maps to FR-001, FR-005, FR-006]
- **Given** a host with `reboot_required=true`, `pending_reboot_class_count=0`, and no automatic reboot path
- **When** the server computes the patch badge
- **Then** the manual-reboot needs-you reason is present and its action tells the operator to reboot now
- **And** the cause list does not include "no kernel or GPU update is pending"

### AC-005: Installed-but-not-running kernel counts as reboot required [Maps to FR-002]
- **Given** an apt host with no reboot-required marker whose newest installed kernel is newer than the running kernel
- **When** the agent collects updates telemetry
- **Then** `reboot_required` is true
- **And** when the running kernel is the newest installed kernel and no marker exists, `reboot_required` is false

### AC-006: Reboot-required indicators ignore pending-only state [Maps to FR-007]
- **Given** a host with `reboot_required=false` and `pending_reboot_class_count=1`
- **When** the operator views Fleet Overview and the device page
- **Then** no reboot-required indicator is shown for the host

### AC-007: Checkbox present and unchecked by default [Maps to FR-008, FR-009]
- **Given** a host with `allow_reboot=true` and an agent that reports support for reboot authorization
- **When** the operator opens the Patch dialog, ticks the box, closes the dialog without confirming, and opens it again
- **Then** the checkbox labelled "Reboot automatically if the patch requires it" is shown each time
- **And** it is unchecked each time the dialog opens

### AC-008: Checkbox disabled when allow_reboot is false [Maps to FR-010]
- **Given** a host whose latest telemetry reports `allow_reboot=false`
- **When** the operator opens the Patch dialog
- **Then** the checkbox is disabled
- **And** the dialog shows that reboots are turned off on this agent (`commands.allow_reboot: false`)

### AC-009: Checkbox disabled for an older agent [Maps to FR-011]
- **Given** a host whose agent reports neither `allow_reboot` nor support for reboot authorization
- **When** the operator opens the Patch dialog
- **Then** the checkbox is disabled
- **And** the dialog shows that the agent must be updated to use this option

### AC-010: Dialog explains what happens to kernel/GPU updates [Maps to FR-012]
- **Given** a host with one pending reboot-class update
- **When** the operator opens the Patch dialog in each of these situations: holds active and unticked; ticked; holds not active and unticked
- **Then** the dialog shows statement (a), (b) or (c) of FR-012 respectively
- **And** for a host with no pending reboot-class update, none of these statements is shown

### AC-011: Dialog states an already-required reboot [Maps to FR-013]
- **Given** a host with `reboot_required=true` and pending updates
- **When** the operator opens the Patch dialog
- **Then** the dialog says a reboot is already required and that, with the box ticked, the host will reboot after the run

### AC-012: Ticked dispatch carries authorization and gets include_reboot_class [Maps to FR-014, FR-015]
- **Given** a host with `allow_reboot=true` and an agent that reports support for reboot authorization
- **When** the operator confirms the Patch dialog with the box ticked
- **Then** the created `os_update` command carries reboot authorization = true
- **And** the server has set `include_reboot_class = true` on it

### AC-013: Unticked dispatch is unchanged from today [Maps to FR-014, FR-025]
- **Given** any host
- **When** the operator confirms the Patch dialog with the box unticked
- **Then** the created command carries no reboot authorization (false or absent) and no `include_reboot_class`
- **And** the run does not start a reboot

### AC-014: Client-supplied include_reboot_class is still removed [Maps to FR-016]
- **Given** a manual `os_update` API request that sets `include_reboot_class: true` without reboot authorization
- **When** the server creates the command
- **Then** the stored command has no `include_reboot_class`

### AC-015: Non-boolean authorization is rejected [Maps to FR-017]
- **Given** a manual `os_update` request whose reboot-authorization value is a string or a number
- **When** the server handles it
- **Then** the response is HTTP 400 `invalid_request`
- **And** no command is created

### AC-016: Authorization rejected when allow_reboot is false or support is missing [Maps to FR-018, NFR-003]
- **Given** a host whose latest telemetry reports `allow_reboot=false`, and a second host whose agent does not report support for reboot authorization
- **When** a manual `os_update` with reboot authorization = true is sent for each host
- **Then** each response is HTTP 400 with an error code distinct from `invalid_request` that names the reason
- **And** no command is created for either host

### AC-017: Authorized run reboots only when required afterwards [Maps to FR-019, FR-023, BR-004]
- **Given** an authorized run on a host with `allow_reboot=true` that installs a kernel, so reboot-required is true after the run
- **When** the run succeeds
- **Then** the agent reports the command result, stating a reboot was authorized, required and started
- **And** only after that does the agent reboot the host

### AC-018: Authorized run with no reboot needed does not reboot [Maps to FR-020, FR-023]
- **Given** an authorized run on a host with `allow_reboot=true` where reboot-required is false after the run
- **When** the run succeeds
- **Then** the host is not rebooted
- **And** the result states the reboot was authorized but not required

### AC-019: Failed authorized run does not reboot [Maps to FR-021, FR-023, NFR-002]
- **Given** an authorized run on a host with `allow_reboot=true` and reboot-required true
- **When** the run fails, times out, or is interrupted
- **Then** the host is not rebooted
- **And** the result states the reason "run not successful"
- **And** the rIOt-managed holds are restored

### AC-020: Agent veto beats authorization [Maps to FR-022, NFR-001]
- **Given** a command carrying reboot authorization = true delivered to an agent whose `commands.allow_reboot` is false (for example, the config changed after the dispatch)
- **When** the run succeeds with reboot-required true
- **Then** the host is not rebooted
- **And** the result states the reason "`allow_reboot` off"
- **And** the reboot-required event fires per PATCH-GATE FR-019

### AC-021: At most one reboot per command [Maps to FR-024, NFR-006]
- **Given** an authorized command that already caused a reboot
- **When** the agent starts after the reboot and the command is redelivered or re-examined
- **Then** the agent starts no further reboot for that command

### AC-022: Automated orchestration unchanged [Maps to FR-026]
- **Given** existing automated OS-patch configurations, inside and outside the window
- **When** `checkAutoPatch` dispatches
- **Then** the dispatched commands and the post-run reboot behavior match the PATCH-GATE behavior that existed before this story

### AC-023: Reboot is the primary action when only a reboot is needed [Maps to FR-027]
- **Given** a host with zero actionable pending updates, fresh lists, `reboot_required=true` and `allow_reboot=true`
- **When** the operator views the device page
- **Then** Reboot is the primary action and uses the existing reboot confirmation
- **And** Patch is not the primary action

### AC-024: Reboot action disabled when allow_reboot is false [Maps to FR-028]
- **Given** the host from AC-023, but with `allow_reboot=false`
- **When** the operator views the device page
- **Then** the Reboot action is disabled
- **And** a visible reason says the host must be rebooted outside rIOt

### AC-025: Config newer than lists reports stale [Maps to FR-029, FR-031, FR-032]
- **Given** an apt host where a file in `/etc/apt/sources.list.d/` or `/etc/apt/preferences.d/` was modified after the last package-list refresh, and `apt list --upgradable` shows 38 entries
- **When** the agent collects updates telemetry
- **Then** freshness is `stale`
- **And** the raw count of 38 is still reported
- **And** when no configuration file is newer than the lists, freshness is `fresh`

### AC-026: Missing lists report stale; check failure reports unknown [Maps to FR-030, FR-031, NFR-004]
- **Given** an apt host with no package lists, and a second apt host where the freshness check errors or exceeds its bound
- **When** the agent collects updates telemetry
- **Then** the first host reports `stale` and the second reports `unknown`
- **And** the agent creates no persistent file and logs at most one debug line per collection

### AC-027: Stale lists replace "Patch (N)" on the device page and the fleet panel [Maps to FR-034, FR-035, BR-005]
- **Given** a host reporting freshness `stale` with a raw count of 38
- **When** the operator views the device page and the Fleet Overview Pending OS Patches panel
- **Then** the Patch button reads "Package lists out of date — refresh pending" and shows no number
- **And** the Patch action can still be used
- **And** the fleet panel row shows the stale label instead of 38

### AC-028: Stale lists change badge wording [Maps to FR-036]
- **Given** a host reporting `stale` with `pending_reboot_class_count=1` and `reboot_required=false`
- **When** the server computes the patch badge
- **Then** the FR-004 pending kernel/GPU wording is absent
- **And** a note says package lists are out of date and pending updates will be recounted after the next refresh

### AC-029: Older agents and unknown freshness keep today's display [Maps to FR-033, FR-037, NFR-009]
- **Given** an older agent's telemetry with no freshness field, and a telemetry payload with freshness `bogus`
- **When** the server ingests both
- **Then** both are accepted with `2xx` and no warnings are logged
- **And** both are treated as `unknown`, and the device page shows "Patch (N)" as today

### AC-030: dnf hosts never report stale [Maps to FR-038]
- **Given** a dnf host
- **When** the agent collects updates telemetry
- **Then** freshness is `fresh` or absent, never `stale`

### AC-031: Freshness never changes automated dispatch [Maps to FR-039]
- **Given** two otherwise identical hosts, one `stale` and one `fresh`, both eligible for automated patching
- **When** `checkAutoPatch` evaluates them
- **Then** the dispatch decision is the same for both

### AC-032: Helper refreshes once after a run that changed apt configuration [Maps to FR-040, FR-041, FR-042, FR-045, NFR-005]
- **Given** a helper run in which a package postinst rewrites a file in `/etc/apt/sources.list.d/` after the refresh step
- **When** the upgrade and verification finish
- **Then** the helper runs exactly one additional package-list refresh before reporting completion
- **And** its output appears in the run output within the existing output bound
- **And** the next agent collection reports freshness `fresh` and no phantom pending updates

### AC-033: Helper skips the extra refresh when nothing changed or in repair mode [Maps to FR-043]
- **Given** a full-mode run in which no apt configuration file changed, and a `repair`-mode run
- **When** each run finishes
- **Then** neither run performs an additional refresh

### AC-034: Failed post-run refresh does not change the run outcome [Maps to FR-044]
- **Given** a run whose upgrade and verification succeed but whose additional refresh fails
- **When** the run finishes
- **Then** the run outcome is success
- **And** the result states that the post-run list refresh failed

### AC-035: Fresh telemetry within 60 s after a run [Maps to FR-046, FR-048, NFR-007]
- **Given** an `os_update` that reaches any terminal outcome without starting a reboot
- **When** the agent reports the result
- **Then** new updates telemetry reaches the server within 60 seconds
- **And** exactly one extra updates collection ran for that trigger, not overlapping another collection

### AC-036: Fresh telemetry within 60 s after agent start [Maps to FR-047, FR-048]
- **Given** a host that has just booted, or an agent that has just been restarted
- **When** the agent starts
- **Then** the first updates telemetry reaches the server within 60 seconds

### AC-037: Audit trail of an authorized run [Maps to NFR-010, FR-023]
- **Given** a completed authorized manual run
- **When** the operator inspects the command record and the command-sent event
- **Then** the record shows reboot authorization = true, `include_reboot_class` = true, and the four FR-023 result facts
- **And** the command-sent event carries the authorization parameter

### AC-038: The vermont sequence is now coherent (end-to-end) [Maps to FR-003, FR-004, FR-019, FR-034, FR-041, FR-046, FR-047]
- **Given** a host in the vermont state before the run: 7 pending, 1 reboot-class pending, `reboot_required=false`, holds active, `allow_reboot=true`, current agent and helper
- **When** the operator views the badge, then runs Patch with the box ticked, and a package rewrites apt configuration during the run
- **Then** before the run, the badge never tells the operator to reboot by hand
- **And** the run installs the kernel, refreshes the lists once more, reports its result and reboots once
- **And** within 60 seconds of the agent starting, telemetry reports pending 0, freshness `fresh` and `reboot_required=false`, with no "Patch (N)" for phantom updates

## 9. Out of Scope

- Any change to the automated OS-patch orchestration, its window logic, or its post-run reboot rule (PATCH-GATE FR-021, FR-024).
- Scheduling a reboot for later, or a reboot countdown (the authorized reboot happens right after the run).
- An unconditional "reboot after patch" option.
- Remembering the checkbox state, or a per-device default for it.
- The agent running a package-list refresh on its own outside a patch run (see OQ-004).
- Freshness detection for dnf, pacman, apk or other package managers.
- Treating changes under `/etc/apt/apt.conf.d/`, keyrings or `trusted.gpg.d` as making the lists stale (see OQ-007).
- Changes to the security scoring engine's handling of stale data (see OQ-005).
- Fleet-wide or bulk Patch with reboot authorization.
- Changes to REBOOT-ETA-001 tooltip content.
- Windows and macOS.

## 10. Assumptions

- **A-001:** All product decisions in this FRD were made by Ryan on 2026-10-23: the meaning of "reboot needed", the checkbox and its default, the hold release on tick, the agent and helper parts of the stale-list fix, and fresh reporting.
- **A-002:** The existing helper path already supports `include_reboot_class` (snapshot, release and restore of managed holds), so ticking the box does not depend on re-running `install.sh`. Only the post-run refresh (Section D) does.
- **A-003:** The existing `reboot` command path and its confirmation are reused for FR-027. No new reboot mechanism is introduced.
- **A-004:** Package-list "last refreshed" time and apt configuration modification times can be read by the unprivileged agent. How they are measured is the architect's choice within FR-029, FR-030 and NFR-004.
- **A-005:** The FR-002 kernel comparison may already exist in part. The architect confirms what exists and fills the gap. The vermont evidence (`reboot_required=true` after the install) shows at least one source worked on Debian 13.
- **A-006:** The 60-second bounds in FR-046 and FR-047 are measured from the agent's result report and from agent process start, and include one full updates collection.
- **A-007:** The checkbox is available on both apt and dnf hosts when FR-010 and FR-011 allow it (see OQ-003).

## 11. Open Questions

Each question has a recommended default. The FRD is written to the default.

- **OQ-001:** Should an authorized run that fails after installing a kernel still reboot? **Default: no** (FR-021). Package state after a failure needs review, and a reboot into a half-configured system is riskier than leaving the host up. The result and the badge make the required reboot visible.
- **OQ-002:** If the helper's post-run refresh fails, should the run be marked failed? **Default: no** (FR-044). The packages were applied and verified. The agent's stale flag will show the lists are out of date.
- **OQ-003:** Should the checkbox also apply to dnf hosts? **Default: yes**, with the same semantics. dnf reboot detection and dnf holds already exist (PATCH-GATE).
- **OQ-004:** "Refresh pending" implies something will refresh. Should anything refresh the lists automatically (for example, the agent asking the helper for a refresh-only run)? **Default: no new automatic refresh in this story.** The next manual Patch or automated run refreshes first. The stale label's explanation should say "Running Patch refreshes the package lists first." Ryan to confirm the label wording is enough.
- **OQ-005:** Should the security scoring kernel check ignore `pending_kernel_update` while lists are stale, since a phantom kernel could lower the score? **Default: out of scope.** Raise as its own story if it causes noise.
- **OQ-006:** For a ticked dispatch the server cannot honor (agent `allow_reboot=false` or no support), should the server reject it or downgrade it silently to an unticked run? **Default: reject** (FR-018). The operator asked for a reboot, so a silent downgrade would repeat the original confusion.
- **OQ-007:** Should changes under `/etc/apt/apt.conf.d/` or keyring directories also mark lists stale? **Default: no.** Ryan named three locations. Expand only if a real case appears.
- **OQ-008:** Fleet-level aggregate pending totals, where shown: should stale devices' raw counts be left out? **Default: yes.** Stale counts are not actionable (BR-005). The architect confirms which aggregate surfaces exist.
- **OQ-009:** Clod had no file-search tool in this session. It could not locate the FRD that first defined the per-host patch badge decision table (needs-you rule NY-10, `manual_reboot_needed`). The architect must cross-reference it and record that FR-003 to FR-006 amend it.

## 12. Dependencies

- **D-001:** PATCH-GATE (`docs/requirements/PATCH-GATE-frd.md`): reboot-class classification, holds, reboot-required detection, `include_reboot_class`, and the FR-019 reboot-required event. FR-023 and BR-005 are amended by BR-003.
- **D-002:** REBOOT-ETA-001 (`docs/requirements/REBOOT-ETA-001-frd.md`): reboot-required indicators stay keyed on `reboot_required` (FR-007).
- **D-003:** Per-host patch badge, `internal/server/handlers/patch_badge.go` (`computePatchBadge`, rule NY-10, Reboot line).
- **D-004:** Manual dispatch boundary, `internal/server/handlers/commands.go` (`SendCommand`'s removal of `include_reboot_class`).
- **D-005:** Maintenance helper, `internal/maintenance/worker.go` (refresh, upgrade and verify steps, output bound, worker budget). Section D reaches devices only when `install.sh` is re-run per device.
- **D-006:** Agent updates collector, `internal/agent/collectors/updates.go` (`collectAPT`, reboot-required detection). Section C ships with the normal agent update.
- **D-007:** Device page Patch button and confirmation, `web/src/pages/DeviceDetail.tsx`. Fleet Overview Pending OS Patches panel, `web/src/pages/FleetOverview.tsx`.
- **D-008:** The existing `reboot` command path and the agent's `commands.allow_reboot` / `commands.allow_patching` vetoes.
- **D-009:** No new third-party dependency, no database migration, no new external service.
- **D-010:** Pipeline: architect (ADD), then security-researcher review (this story widens a hold-release path), before engineering.
