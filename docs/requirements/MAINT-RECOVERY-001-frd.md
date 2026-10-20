# Formal Requirements Document — MAINT-RECOVERY-001

**Date:** 2026-10-20  
**Status:** Proposed implementation contract; security review required before code  
**Scope:** Safe remote OS maintenance and bounded package recovery

## 1. Problem and intended outcome

The user approved remote OS updates with bounded recovery and a small privilege surface. Current apt maintenance refreshes first, attempts `dpkg --configure -a` nonfatally, then upgrades; a broken refresh can prevent recovery, a failed repair can be ignored, and unrestricted dist-upgrade can remove packages. The package processes remain children of the agent's systemd cgroup. A restart, OOM or self-update can interrupt them. Manual commands use the in-memory host lock, but automatic agent updates do not take it. Older installations may still have root-shell sudo wildcards or a blind staged-binary boot copy despite current installer hardening.

Deliver one constrained apt transaction runner independent of agent lifetime, serialize package work with agent replacement, retain trustworthy results after reconnect, and fail clearly when safe automation cannot repair the system. Preserve the existing command API, patch holds, reboot decisions and signed-update trust boundary. This work must not become a general privileged command executor.

## 2. Scope and limits

- New detached maintenance path: Linux, apt 2.x+, systemd 241+ with existing release architectures. Target Debian 11+/Ubuntu 20.04+ class hosts; installer/doctor must verify actual prerequisites. Unsupported apt installations fail closed with an upgrade/install explanation; no unsafe legacy apt fallback.
- Existing DNF update behavior remains supported, including existing hold/reboot semantics. This story does not promise dpkg-style recovery or detached execution on DNF, macOS or Windows. Cross-path auto-update serialization applies wherever supported.
- Retain `os_update` and its existing parameters; accept closed `mode` values `full`, `security`, `repair` on the new apt path. Missing mode remains `full`; wrong types/unknown values fail before privilege escalation. Repair is available through the existing authenticated command API, without a new endpoint or UI requirement.
- Existing apt `security` uses conservative `apt-get upgrade`, which does not guarantee security-origin-only selection. Preserve that compatibility and document it accurately. Designing true distro-specific security-origin filtering is outside this story.
- No arbitrary package selection, shell text, root environment, apt options, repository edits, local deb files, downgrade, removal, lock-file deletion, force flags, service names or filesystem paths may be supplied remotely.
- This design does not authorize live changes. Root/operator migration of Vermont or another legacy host remains a separately authorized operational task.

## 3. Requirements

| ID | Required behavior |
|---|---|
| FR-001 | Existing `commands.allow_patching` and authenticated dispatch remain mandatory. A new signed root-owned maintenance helper accepts a closed request schema and executes only fixed operations. |
| FR-002 | Run the complete apt transaction in one root systemd service outside the agent cgroup. Stdin is null; output goes to journal and bounded durable capture, never an agent-owned pipe/PTY. Agent death/disconnection must not cancel the job. |
| FR-003 | Begin with interrupted-dpkg configuration before a refresh-dependent upgrade. A failed configuration triggers at most one dependency repair with removals forbidden, then one configuration/check verification. No retry loop or unconditional continuation after repair failure. |
| FR-004 | All mutating apt install/upgrade paths explicitly refuse package removals. Repair never uses remove/purge/autoremove, arbitrary install operands, allow-downgrade, allow-change-held, unauthenticated or insecure-repository overrides. Native apt/dpkg locks remain authoritative and are never deleted. |
| FR-005 | `repair` stops after recovery checks and never runs the general upgrade phase. `full` uses existing full-upgrade semantics with a no-removal guard. `security` retains the existing conservative upgrade semantics with the same no-removal guard. |
| FR-006 | Keep one fixed, bounded transaction: 30-minute worker budget, bounded native package-lock wait, SIGTERM and 90-second cleanup grace, then explicit failure if forced termination is unavoidable. Preserve evidence and say that package state may require manual review; no atomic-rollback guarantee. |
| FR-007 | One cross-process root-owned lock excludes maintenance, signed apply, boot apply and installer replacement. Existing in-process host locks also protect manual and automatic update paths. Busy automatic checks defer; busy manual operations report conflict. No path stops/restarts the agent before its exclusion checks. |
| FR-008 | Known active maintenance blocks agent update/uninstall, reboot/shutdown and conflicting maintenance even after an agent restart. A restart must restore reservation/observation of that same job, not declare it failed while it is running or start another copy. Root-admin shutdown/reboot and power loss remain external interruptions. |
| FR-009 | Root-owned durable status identifies the exact command/run, mode, phases, timestamps, bounded output, exit/outcome and hold/version results. Terminal history is never automatically deleted or expired; a fixed capacity cap refuses new maintenance until explicit operator-authorized archival/cleanup. Agent startup reconnects running jobs or reports finished jobs through the existing durable outbox. Old/stale status must not be attributed to a new command. |
| FR-010 | An interrupted worker with no surviving unit is reported interrupted/unknown, not success and not automatically replayed. No automatic reboot follows an interrupted or uncertain result. |
| FR-011 | Preserve rIOt versus operator hold ownership and the existing classifier, include-reboot-class authorization and before/after version checks. The detached worker owns release/reapply for its transaction. Agent startup/periodic hold reconciliation must not race an active worker. Hold restoration failure is visible and prevents a clean-success result. |
| FR-012 | Reapply managed holds on success, failure and timeout, with bounded cleanup and recoverable durable bookkeeping. Never release a hold merely because a request names a package. Repair mode keeps holds and never initiates automatic reboot. |
| FR-013 | Preserve post-result reboot behavior: only proven reboot-class changes on an authorized update, existing allow-reboot policy, and result queued first. A recovered result must not replay a reboot already issued; uncertain reboot delivery is reported rather than retried. |
| FR-014 | Package commands use compile-time absolute paths, fixed working directory and allowlisted environment. Treat request/staging/status paths as hostile: bounded regular files, no symlink following, secure directories, atomic writes and no root write through agent-controlled paths. |
| FR-015 | Install/upgrade verifies helper release signature/checksum before root-owned installation. The signed agent apply verifier remains the sole binary-replacement authority; no root shell, arbitrary `systemd-run`, shell wildcard or blind ExecStartPre copy is permitted. |
| FR-016 | Installer publishes validated sudoers/unit/helper changes safely, retaining the last working policy on failure. `visudo` and read-only `doctor` checks verify exact rules, ownership, supported versions, independent service and active maintenance without running a package repair/update. |
| FR-017 | Explicit migration instructions cover current and legacy units/sudoers, agent-only upgrades, missing helper and incompatible service/policy. Updating only the unprivileged agent cannot silently grant the new privilege path. No automatic removal of unknown administrator policies. |

## 4. Acceptance criteria and security matrix

New tests carry `[MAINT-RECOVERY AC-xxx]` names. No real package changes or service restarts in unit tests.

| ID | Acceptance / required negative cases |
|---|---|
| AC-001 | Only full/security/repair accepted; empty/missing defaults full. Reject wrong type, extra keys, path/argv/env injection, malformed UUID, oversize, trailing JSON and unknown protocol version. |
| AC-002 | Root launch is one exact fixed service start; helper receives no caller-supplied argv. A captured invocation matrix contains no shell, arbitrary systemd property/unit, pipe/PTY, repository or package operand. |
| AC-003 | Recording runner proves configure → optional single no-remove repair → configure/check → refresh/upgrade order; repair failure blocks upgrade. Healthy repair-only never refreshes/upgrades. Assert no mutating apt invocation omits the no-remove policy. |
| AC-004 | Removal-required, unauthenticated repo, invalid config, blocked/held dependency, native package-lock contention, failing maintainer script and failed refresh produce bounded explicit errors without force/remove fallback. |
| AC-005 | Worker vs worker and worker vs apply/boot/installer races allow at most one mutator. Auto updater holds the existing host lock and respects Docker locks; no restart/apply event is emitted as success when deferred. |
| AC-006 | Simulated agent SIGTERM/restart loses only the observer: job and output continue; startup reserves host and reconnects exact ID; completion enters outbox once. Unit/runtime death reports interruption and never re-executes. |
| AC-007 | Timeouts signal the package process tree, bound the lock wait/cleanup and report forced interruption honestly; the agent observer timing out does not kill/relaunch a running worker. |
| AC-008 | Operator holds survive; corrupted/unsafe managed-hold metadata releases nothing; only tracked/classified managed holds can be released for authorized updates. Agent reconciliation is suppressed while active. Failed reapply is persistent and visible. |
| AC-009 | Before/after and reboot/outbox regressions remain green; repair/failed/unknown results never reboot. Restart/repeated report delivery never reissues reboot. |
| AC-010 | Symlink, FIFO/device, writable helper/unit/parent, file swap, oversized input/output and forged status are refused or bounded. No root write targets agent-selected paths; persisted output is redacted/capped before server reporting. |
| AC-011 | Packaging covers all Linux release architectures with exact-basename signed checksums and preserves legacy checksum ordering. Invalid signature, missing asset or invalid sudoers/unit leaves the active install/policy intact. |
| AC-012 | Installer/doctor fixture tests distinguish current, missing, legacy-broad and unsupported apt/service setups. Doctor uses privilege-list/status inspection only and never starts maintenance. Existing DNF/non-Linux and command API tests remain green. |

## 5. Delivery gates

Follow `CLAUDE.md` FRD → ADD → security → implementation → QA → technical writing. Security must approve the privileged helper/request/hold boundary and exclusion protocol before code. Baseline tests precede implementation. QA includes Linux service/process lifecycle tests with harmless fixtures plus existing unit/race/API/hold/updater suites. A disposable apt VM/container integration can validate no-remove and broken-package cases; any genuinely mutating test needs an isolated disposable environment, never production. Installation/service changes and residual package-script risks must be documented before release. No database migration is expected.
