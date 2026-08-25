# Security Review Report

- **Story ID:** PATCH-GATE
- **Reviewer:** Security Researcher Agent
- **Date:** 2026-08-25
- **Verdict:** APPROVED WITH REQUIRED MITIGATIONS

---

## Threat Model Summary

### Assets

- **Host patch-state integrity.** Whether GPU-driver/kernel packages are held
  or released determines whether a driver/kernel-module version mismatch can
  occur. This is the whole point of the story: an incorrectly-released hold, or
  a hold that silently never took effect, re-creates the outage the feature
  exists to prevent.
- **Host availability.** The agent can now initiate an unattended
  `systemctl reboot` as a *side effect of a patch run* rather than only from an
  explicit `reboot` command. A reboot loop or an ill-timed reboot degrades the
  monitored host.
- **The `riot` privilege boundary.** The agent runs as the unprivileged `riot`
  system user (`scripts/install.sh:164-168`, systemd `User=riot`) and reaches
  root **only** through an explicit, per-command sudoers allowlist
  (`/etc/sudoers.d/riot-agent`, written at `install.sh:437-465`). Every new
  root-capable operation this story adds (`apt-mark hold/unhold`, writing a
  root-owned `/etc/dnf/...` fragment) either widens that boundary or fails —
  and *how* it is widened is the primary new security decision.
- **Hold bookkeeping state file** (`/var/lib/riot/holds.json`). The sole record
  of which holds rIOt created; it drives what gets released before a run and
  what gets unheld on disable.

### Threat Actors

- **Compromised or buggy agent (valid `X-rIOt-Key`).** Reports
  `held_packages`, `reboot_required`, `uses_gpu` — all unvalidated telemetry,
  same trust class as `cpu_percent`. In scope only where a new field could
  drive a *server* action; it does not (see Positive Observations).
- **Compromised server / MITM'd WebSocket.** Can set
  `include_reboot_class: true` and dispatch `os_update` at will. The story's
  central safety claim is that this actor **still cannot** force a reboot or a
  hold-release without the agent-side opt-ins. This claim is verified below.
- **Local unprivileged attacker on the monitored host.** Cannot write
  `/var/lib/riot/` (0700-ish, `chown riot:riot` at `install.sh:212`) or the
  root-owned dnf fragment. Relevant only for poisoning bookkeeping *if* they
  already hold the `riot` uid — at which point they own the agent anyway.
- **Malicious/compromised package repository.** Supplies attacker-chosen
  *package names* that flow from `apt list --upgradable` / `dnf check-update`
  into the classifier and back out as `apt-mark`/fragment arguments. This is
  the story's one genuinely new untrusted-data-into-privileged-command path.

### Attack Surface Introduced

- New privileged agent operations: `sudo apt-mark hold/unhold`, a root-owned
  `excludepkgs` fragment write at `/etc/dnf/libdnf5.conf.d/60-riot-holds.conf`,
  `dnf needs-restarting -r`, and a patch-run-initiated `systemctl reboot`.
- New `os_update` command parameter `include_reboot_class` (WS/queued payload).
- New agent-owned state file `/var/lib/riot/holds.json`.
- New unvalidated telemetry keys (`class`, `held_packages`, `reboot_required`,
  `reboot_required_reasons`, `pending_reboot_class_count`, `uses_gpu`) and one
  new `AutomationConfig` enum (`reboot_class`).
- One new event type/template/seeded rule (`reboot_required`).
- No new endpoints, no new auth paths, no DB migration.

---

## Findings

### CRITICAL

None.

---

### HIGH

#### SEC-PATCH-GATE-001: ADD Assumption A-002 is false — the new privileged operations are not in the sudoers allowlist, and `scripts/install.sh` is missing from the change set

- **Severity:** HIGH
- **Domain:** Privilege Boundary / Broken Enforcement (fail-open)
- **Location:** ADD Assumption A-002, ADD §10 "Privilege surface", ADD §4
  Component Changes; `scripts/install.sh:437-465`; `internal/agent/collectors/holds.go` (to be created)
- **Description:** A-002 and ADD §10 both assert the agent "already runs with
  sufficient privilege" for `apt-mark`, the dnf fragment write, and reboot
  "as it already does for the existing `os_update` and `reboot` command paths."
  This is verifiably wrong. The agent is the unprivileged `riot` user and
  reaches root only through the **exact, argument-matched** sudoers rules in
  `/etc/sudoers.d/riot-agent`. That file (generated at `install.sh:437-465`)
  grants precisely: `apt-get update`, `apt-get -y dist-upgrade …`,
  `apt-get -y upgrade …`, `dnf makecache`, `dnf -y update`,
  `dnf -y --security update`, `systemctl reboot`, the agent-update `sh -c` line,
  `systemctl reset-failed riot-agent-update`, `nginx -t/-T`, and `smartctl`.
  There is **no** entry for `apt-mark`, none for writing anywhere under
  `/etc/dnf/`, and none for `dnf needs-restarting`.

  Consequences of shipping as designed:
  1. `sudo apt-mark hold <pkg>` is not allowlisted → non-interactive sudo
     fails (no tty for a password) → **every reconcile cycle fails**. Per
     NFR-005 the collector continues, but hold enforcement never actually
     happens.
  2. The dnf fragment is described (AD-005) as written by the agent "atomically
     (temp + rename, 0644)." The `riot` user cannot create a file in the
     root-owned `/etc/dnf/libdnf5.conf.d/` directory, and no `sudo` helper for
     that write is allowlisted. The write fails.
  3. `scripts/install.sh` — the file that must gain the new sudoers rules — is
     **not in ADD §4's Component Changes table at all.** The engineering team
     has no instruction to touch it, and the ADD explicitly lists the files
     "deliberately not modified" without acknowledging this gap.

  The dangerous property is that this **fails open on a safety feature**: an
  operator sets `hold_reboot_class: true`, sees no error on the dashboard
  (holds simply never appear), believes GPU/kernel packages are protected, and
  a mid-day `apt upgrade` pulls the NVIDIA driver — the exact incident the
  story was written to prevent, now re-created by silent non-enforcement.
- **Exploit / Failure Scenario:** No attacker needed for the base failure — it
  is the default outcome on a correctly-installed agent. Adversarially: a user
  who trusts the feature stops manually gating driver updates; the first
  unattended-upgrades or casual `apt upgrade` after a driver lands mid-window
  breaks every GPU container until reboot.
- **Required Mitigation (engineering team must implement):**
  1. Add `scripts/install.sh` to the change set. Extend the sudoers block
     (`install.sh:437-465`) with the **exact** new rules, gated on the detected
     package manager, matching the argument-locked style already used there —
     e.g. for apt: `riot ALL=(root) NOPASSWD: ${APTMARK_PATH} hold *` and
     `… ${APTMARK_PATH} unhold *` (see SEC-PATCH-GATE-002 on why even these
     wildcards need scrutiny), and a **dedicated fragment-writer** invocation
     rather than a general file write (see SEC-PATCH-GATE-002).
  2. `dnf needs-restarting -r` must be confirmed to run as the unprivileged
     `riot` user (it reads `/proc` and the dnf history DB; it generally does
     **not** require root). If it does not, do **not** grant it sudo — degrade
     to `reboot_required = false` (AD-010 already tolerates this).
  3. The agent must **distinguish "enforcement failed" from "nothing held."**
     Add an explicit hold-enforcement status to telemetry (e.g.
     `HoldEnforcementActive bool` / an error reason) so the UI shows
     enforcement is **not** in effect when the sudoers rule is missing or the
     dnf5 fragment is unsupported (AD-005 already defines a dnf4-unsupported
     state — reuse that surfacing for the failure case). Empty `HeldPackages`
     must never read as "protected."
  4. Update A-002 and ADD §10 to state the boundary is being *widened* and to
     enumerate the new rules, so this is a reviewed decision rather than an
     assumed non-event.
- **Blocks:** Implementation. This is the load-bearing gap; without it the
  feature does not function and misrepresents its own state.

#### SEC-PATCH-GATE-002: The privilege-widening must not be delegated to implementation as a wildcard or shell sudoers rule

- **Severity:** HIGH
- **Domain:** Local Privilege Escalation
- **Location:** ADD §10, `scripts/install.sh:437-465` (fix site for SEC-001);
  `internal/agent/collectors/holds.go` (dnf fragment writer)
- **Description:** SEC-PATCH-GATE-001 forces a sudoers change, and the ADD gives
  the engineering team no specification for its safe form. The dnf case is the
  hazard: there is no argv-only way to make an unprivileged process write a
  root-owned file, so the naive fixes are all dangerous. If an engineer reaches
  for `sudo tee /etc/dnf/libdnf5.conf.d/60-riot-holds.conf` with a wildcard, or
  `sudo sh -c …`, or a broad `sudo cp * /etc/dnf/…`, they hand the `riot` user
  (and therefore anyone who compromises any collector) an arbitrary-root-file-
  write primitive — a direct local root escalation (write a `.repo` file, a
  cron entry via a writable drop-in, or a malicious `/etc/dnf` plugin path).
  Note the existing `enable_auto_updates` path already uses an un-allowlisted
  `sudo tee /etc/apt/apt.conf.d/20auto-upgrades` (`commands.go:407`) — do **not**
  copy that pattern as precedent; it works today only on hosts with broader
  sudo than the least-privilege installer grants, and it is itself a latent
  over-grant.
- **Exploit Scenario:** A wildcard `sudo tee /etc/dnf/*` (or `sudo sh -c *`)
  rule is added. An attacker who achieves code execution as `riot` (e.g. via a
  future collector-parsing bug) runs `sudo tee /etc/dnf/plugins/x.conf` or
  overwrites `/etc/sudoers.d/riot-agent` itself, escalating to root without a
  password.
- **Required Mitigation:** The ADD must specify a **fixed-path, fixed-shape**
  writer. Recommended: a tiny helper form allowlisted to a single exact target,
  e.g. `riot ALL=(root) NOPASSWD: /usr/bin/tee /etc/dnf/libdnf5.conf.d/60-riot-holds.conf`
  (no wildcard in the path) with the content piped on stdin, plus a matching
  exact-path `rm` rule for the delete-on-disable case. For `apt-mark`, prefer
  the narrowest match the sudoers syntax allows; the package-name argument is
  attacker-influenceable (SEC-PATCH-GATE-003), so the rule must not also permit
  arbitrary `apt-mark` subcommands. No `sh -c`, no wildcard directory
  component, no `tee` to a variable path. Add this as a new subsection to
  ADD §10 and reference it from the AD-004/AD-005 mechanism decisions.
- **Blocks:** Implementation (must be specified before the sudoers rule is
  written).

---

### MEDIUM

#### SEC-PATCH-GATE-003: Attacker-controlled package names reach `apt-mark` argv and the dnf fragment — argv path is safe, fragment path needs the documented reject-list enforced

- **Severity:** MEDIUM
- **Domain:** Command / Configuration Injection
- **Location:** ADD §10 "Command injection via package names", AD-004, AD-005;
  `internal/agent/collectors/holds.go`, `rebootclass.go`
- **Description:** Package names originate from a repository (`apt list
  --upgradable`, `dnf check-update`, `dpkg --get-selections`, `rpm -qa`), pass
  through `ClassifyPackage`, and are handed back to the package manager. The ADD
  correctly reasons that the `apt-mark` path is safe because names land in
  `exec.CommandContext` argv (no shell) — this is sound and matches the
  established `nvidia-smi`/`upsc` pattern (confirmed in the GPU-001 review). The
  residual risk is the **dnf fragment**: names are joined with `,` into an ini
  `excludepkgs=` value, so a name containing `,`, a newline, `[`, or `=` could
  break out of the value and inject a fragment directive (e.g. a second
  `[main]` section or an unrelated option). ADD §10 states names with
  `,`/newlines are "rejected (skipped + WARN) before writing," but this control
  is described only in prose — it is not pinned to an AC or a DoD checkbox, and
  the reject set as written omits `[`, `]`, `=`, `#`, and leading/trailing
  whitespace.
- **Exploit Scenario:** A hostile or typosquatted repo publishes a package whose
  name embeds ini syntax; on a host that has it installed and classified
  reboot-class, the agent writes a fragment whose parsed meaning differs from
  the intended exclude list — at minimum corrupting dnf config, at worst
  disabling the very exclude that protects a driver (turning the injection into
  a hold bypass).
- **Required Mitigation:** Promote the reject-list to an implemented, tested
  control. Validate every package name before it is written to the fragment or
  passed to `apt-mark` against a strict allowlist charset (dpkg/rpm package
  names are `[A-Za-z0-9.+_-]`, plus `:` for apt arch-qualified names); skip +
  WARN anything else. Add a named unit test (reference it under NFR-006 in
  AD-005's test list) that feeds a name containing `,`, newline, `[`, and `=`
  and asserts it is excluded from the fragment and from `apt-mark` argv. Anchor
  it to a DoD item in ADD §13.
- **Blocks:** No — must be implemented and tested during the impl phase.

#### SEC-PATCH-GATE-004: In-place agent upgrades never re-run `install.sh`, so existing fleets get the feature code without the sudoers rules

- **Severity:** MEDIUM
- **Domain:** Broken Enforcement / Deployment (fail-open)
- **Location:** `internal/agent/commands.go:603-659` (`handleTriggerUpdate`,
  self-update replaces the binary and restarts — it does **not** re-run
  `scripts/install.sh`); ADD §12 "Rollout & configuration documentation"
- **Description:** Even after SEC-PATCH-GATE-001 adds the sudoers rules to
  `install.sh`, the dominant upgrade path for an existing fleet is the built-in
  `agent_update` action, which downloads the new binary and restarts in place.
  It never touches `/etc/sudoers.d/riot-agent`. So every already-deployed host
  that upgrades to the PATCH-GATE agent and enables `hold_reboot_class: true`
  hits exactly the SEC-001 failure mode (sudo denied, holds silently inactive),
  even though a *fresh* install would work. The enablement runbook in ADD §12
  says "upgrade agent, set `hold_reboot_class: true`" with no step to refresh
  the sudoers drop-in.
- **Exploit Scenario:** Operator with an established fleet follows the runbook,
  enables holds, sees no error, is unprotected. Same outage as SEC-001, but on
  the majority (upgraded, not freshly-installed) population.
- **Required Mitigation:** (a) The runbook (ADD §12) must include an explicit
  step to re-run the installer (or a documented `curl … | sudo bash` reinstall,
  which `install.sh` supports idempotently and which re-writes the sudoers
  file) after upgrading to a version with hold support; and (b) the SEC-001
  status field must make the missing-privilege state visible on the device page
  so operators discover the gap immediately rather than after an outage. The
  agent should also self-check at startup whether the required sudo rule is
  present (a cheap `sudo -n -l` probe or a dry `apt-mark showhold` vs
  `apt-mark hold` capability test) and reflect it in that status field.
- **Blocks:** No — documentation + the SEC-001 status surfacing cover it; must
  be addressed in the impl/docs phase.

#### SEC-PATCH-GATE-005: `holds.json` is the sole authority for what gets unheld — its corrupt/poison handling must fail closed on the release path

- **Severity:** MEDIUM
- **Domain:** State Integrity / Bookkeeping
- **Location:** AD-003, ADD §9 "State file corrupt/unparseable"; `holds.json`
- **Description:** `ReleaseForRun` unholds exactly the packages listed in
  `apt_holds`/`dnf_excludes`, and the disable path unholds them too. The file is
  the only thing separating "rIOt's holds" from "the operator's holds"
  (BR-004). Two integrity concerns: (1) a corrupt/truncated file, and (2) a
  file whose `apt_holds` array has been made to *include an operator's
  security-critical hold* (writable only by `riot`/root, so this is a
  post-riot-compromise or bug scenario, consistent with the trust model). The
  ADD's §9 handling is correct for corruption — "treat as empty state but do
  **not** release anything… reconcile re-holds and rewrites" — which fails
  closed. This is good and should be kept. The gap is that the release path
  (`ReleaseForRun`) is not explicitly stated to apply the same "if the state is
  untrusted, release nothing" discipline, and there is no cross-check that a
  name about to be unheld is actually reboot-class per the pattern table.
- **Exploit Scenario:** A bug (or a `riot`-level compromise) writes a
  non-reboot-class, operator-critical package (e.g. `openssh-server`,
  `postgresql-16`) into `apt_holds`. The next in-window run unholds it right
  before `dist-upgrade`, silently upgrading a package the operator had pinned.
- **Required Mitigation:** On the release path, unhold only names that both
  appear in `apt_holds`/`dnf_excludes` **and** classify as reboot-class via
  `ClassifyPackage` (defense-in-depth cross-check); skip + WARN anything else.
  State explicitly in AD-003/AD-008 that a failed/parse-error state load aborts
  the release (nothing unheld) rather than proceeding with an empty set that
  might later be interpreted as "release all." Add a test asserting a poisoned
  non-reboot-class entry is never unheld.
- **Blocks:** No — hardening; implement during the impl phase.

---

### LOW / INFORMATIONAL

#### SEC-PATCH-GATE-006: Reapply-failure exposure window is bounded and documented; reboot-loop risk is low

- **Severity:** LOW
- **Domain:** Availability
- **Location:** AD-007, AD-008, ADD §7C failure table
- **Description:** If `ReapplyAfterRun` fails (e.g. the `apt-mark hold` sudo
  call fails — see SEC-001), holds are left released until the next reconcile
  (≤1 telemetry cycle, ~60 s), during which a concurrent `unattended-upgrades`
  could pull a driver. The design bounds and logs this (marker-first ordering,
  per-cycle + startup re-assert) and it is acceptable. Reboot-loop risk is low:
  auto-reboot fires only when a reboot-class package's version *actually
  changed* (before/after snapshot, AD-008 step 5), and post-run reconcile plus
  the `checkAutoPatch` cooldown prevent immediate re-dispatch; after a
  successful upgrade the package is no longer pending, so the next window finds
  nothing to apply. No finding beyond confirming the analysis.
- **Required Mitigation:** None; ensure the ADD §7C guarantees survive
  implementation (covered by AC-010's defer-on-failure test). The `applied ==
  ∅ ⇒ no reboot` fail-safe (also used when the before/after query fails) is the
  correct default and must be preserved (DoD §13 already lists it).
- **Blocks:** No.

#### SEC-PATCH-GATE-007: New telemetry fields are agent-authored claims — server correctly never actuates on them

- **Severity:** INFORMATIONAL
- **Domain:** Data Exposure / Trust Model
- **Location:** ADD §6 "Telemetry ingest", §10 "Telemetry trust"
- **Description:** `held_packages`, `reboot_required[_reasons]`, `uses_gpu`,
  `class`, and `pending_reboot_class_count` are unvalidated, device-key-
  authenticated claims — the same trust class as all existing telemetry. Package
  names and GPU-dependency flags are inventory data, not secrets, and travel the
  already-authenticated pipeline (consistent with SEC-006 in the GPU-001
  review). Critically, the only server-side *actuation* — dispatching
  `os_update` from `checkAutoPatch` — keys off `PendingUpdates`, not any new
  field, so a lying agent cannot use the new fields to trigger a run or a reboot
  on itself beyond what it could already do. The `reboot_required` event is
  advisory (event log + optional notify), not actuating. No finding.
- **Required Mitigation:** None. Confirm `CheckRebootRequired` (AD-011) only
  *emits*, never dispatches a command.
- **Blocks:** No.

#### SEC-PATCH-GATE-008: `reboot_required` event dedup depends on the UPS transition-key pattern being copied exactly

- **Severity:** INFORMATIONAL
- **Domain:** Availability / Alert Correctness
- **Location:** AD-011, `internal/server/events/generator.go:558-652`
  (`CheckUPSAlerts`), `:86-107` (`pruneStaleEntries`)
- **Description:** AD-011 reuses the UPS on-battery transition pattern: a
  `deviceID:reboot_required_active` key in `lastSent`, refreshed every cycle
  while true, deleted on clear. This is the codebase's proven once-per-
  transition mechanism. The correctness hazard flagged in the ADD's own
  implementation note #9 is real: the key **must** be refreshed on every
  true-cycle or `pruneStaleEntries` (1 h cutoff) will evict it and the next
  still-true cycle re-fires the event — an event storm on a host that legitimately
  stays reboot-required for days. A flapping `reboot_required` (agent toggling
  the field) would also produce one event per false→true edge; the 24 h rule
  cooldown absorbs this for notifications.
- **Required Mitigation:** None beyond faithfully copying the UPS key handling
  (refresh-while-true, delete-on-clear, same lock scope) per ADD note #9, and
  covering the "true for many cycles ⇒ one event" case in
  `reboot_required_test.go` (AC-019 already requires the persists-true clause).
- **Blocks:** No.

#### SEC-PATCH-GATE-009: Server-side manual-dispatch strip is the right boundary; verify no other os_update dispatch path bypasses it

- **Severity:** INFORMATIONAL
- **Domain:** Authorization / Defense in Depth
- **Location:** AD-009, `internal/server/handlers/commands.go:32-101`
  (`SendCommand`), `checkAutoPatch` (`auto_update.go:445-507`), `BulkPatchDevices`
- **Description:** Stripping `include_reboot_class` from manual `os_update`
  dispatches at the API boundary (AD-009 / note #8) correctly enforces BR-005
  server-side rather than trusting UI callers. I confirmed the only three
  os_update creation sites are `SendCommand` (to be stripped), `checkAutoPatch`
  (the sole intended setter, gated on `RebootClass == "gated"`), and
  `BulkPatchDevices` (builds its own params, never sets the flag). The
  two-sided-opt-in safety claim holds: a compromised server setting
  `RebootClass: "gated"` achieves nothing on an agent without
  `hold_reboot_class: true` (no holds exist to release, and `AllowReboot` still
  gates the reboot), and NFR-002's reboot veto is structurally intact — the only
  `systemctl reboot` call sites read `a.config.Commands.AllowReboot` directly
  and no command parameter reaches them.
- **Required Mitigation:** None; add the server-side strip test first (note #8)
  and keep the `execReboot()` helper (note #6) reading `AllowReboot` with no
  alternate path. If any future queued-command replay path is added, ensure it
  also passes through the strip.
- **Blocks:** No.

---

## Positive Observations

- **Command-injection surface is correctly reasoned for the argv path.** Names
  reach `apt-mark`/`dpkg-query`/`rpm` as discrete `exec.CommandContext`
  arguments, never a shell string — consistent with the reviewed
  `nvidia-smi`/`upsc`/`smartctl` patterns. Only the ini-fragment join needs the
  extra validation of SEC-PATCH-GATE-003.
- **The reboot veto is structural, not conventional.** NFR-002 is enforced by
  the single `AllowReboot`-gated exec site; no server input can reach it. The
  design's insistence on a shared `execReboot()` helper (note #6) preserves this.
- **`defer`-based hold re-application covers every handler exit** including
  panic and cancelled-context failure paths, with marker-first ordering for the
  crash window — the strongest available guarantee for NFR-001, and the §7C
  failure table is honest about the residual (downtime + ≤1 cycle) window.
- **Fail-closed corruption handling.** ADD §9 treats an unparseable state file
  as "release nothing," which is the correct safety direction (SEC-005 only asks
  to extend the same discipline to the release path explicitly).
- **No new endpoints, no migration, all new fields `omitempty`.** Old-agent/
  old-server compatibility (AC-024) rides the existing permissive decoder
  (confirmed no `DisallowUnknownFields` on the ingest path in prior reviews),
  and the server performs no destructive action on any new field.
- **dnf4 degrade is honest.** AD-005 reports empty `HeldPackages` + a visible
  WARN rather than silently claiming protection — the same surfacing SEC-001
  asks to reuse for the sudo-denied case.

---

## Verdict Rationale

**APPROVED WITH REQUIRED MITIGATIONS.** No CRITICAL findings, and the overall
design is sound: the reboot veto is structurally enforced, the two-sided
opt-in holds against a compromised server, hold re-application is `defer`-hard,
and state corruption fails closed.

However, the design under-specifies the one new privilege-boundary it depends
on. Assumption A-002 is factually wrong — the new `apt-mark` and dnf-fragment
operations are **not** covered by the least-privilege sudoers allowlist, and
`scripts/install.sh` (the file that must grant them) is absent from the change
set. Left unaddressed this ships a safety feature that silently does nothing
(SEC-PATCH-GATE-001), and the obvious fixes for the dnf fragment write are
local-root-escalation primitives if done with a wildcard/shell sudoers rule
(SEC-PATCH-GATE-002). Both must be resolved, with the exact sudoers rules
specified in the ADD before the engineering team writes them. Two further
MEDIUM items (the in-place-upgrade sudoers gap, SEC-004; and fragment/argv name
validation, SEC-003) and a state-poison hardening (SEC-005) must be implemented
and tested during the impl phase.

Implementation may proceed once SEC-PATCH-GATE-001 and -002 are folded into the
ADD (privilege boundary + exact sudoers rules + an enforcement-status telemetry
field). QA must verify all five substantive findings before approving.

---

## Routing

- **Verdict:** APPROVED WITH REQUIRED MITIGATIONS
- **Next agent:** `senior-dev` (ADD should first be amended for SEC-001/002 —
  add `scripts/install.sh` to §4, specify the exact new sudoers rules and the
  fixed-path fragment writer in §10, and add an enforcement-status field to §5)
- **Pass alongside ADD:** this security report
- **QA must verify:**
  - SEC-PATCH-GATE-001: sudoers rules present for the detected PM; enabled-but-
    unprivileged and dnf4 states surface a distinct "enforcement inactive"
    signal (empty `HeldPackages` never reads as protected).
  - SEC-PATCH-GATE-002: no wildcard-path or `sh -c`/`tee`-to-variable-path
    sudoers rule; fragment writer is fixed-path.
  - SEC-PATCH-GATE-003: package-name charset validation on both the fragment and
    `apt-mark` argv paths, with a named injection test.
  - SEC-PATCH-GATE-004: enablement runbook includes a sudoers-refresh step; agent
    self-checks and reports missing sudo capability.
  - SEC-PATCH-GATE-005: release path unholds only names that are both state-
    tracked and classify reboot-class; parse-error load aborts release.
