# Implementation report — MAINT-RECOVERY-001

Date: 2026-10-20. Base: `ba02585`. Branch: `codex/maintenance-recovery`.

## Stack and scope

Go 1.24 agent/server, Linux systemd and apt/dpkg, Bash installer, existing Ed25519 release signing. The unchanged dashboard uses React 19.2, TypeScript 5.9, Tailwind 4.2, Vite 6.4 and Vitest 4. No Go/npm dependency, server endpoint, database schema or UI change is introduced. Existing `golang.org/x/sys/unix` supplies Linux no-follow IO/flock operations. The root installer requires preinstalled Python 3 for native apt/dpkg lock exclusion and either OpenSSL 3 or Python cryptography to authenticate release assets. It never installs its own verifier or runs an implicit package transaction.

Implementation follows the paired FRD/ADD and approved security review. Source baseline `go test ./...` passed before development. Linux-only behavior remains behind build tags; DNF retains its existing supported command plan.

## Result

An apt `os_update` now hands a closed request to one fixed root-owned service. The independent worker owns configure, audit/check, at most one no-removal dependency repair, refresh/upgrade, final verification and managed hold restoration. The observer cannot cancel that transaction by disconnecting or restarting. Repair-only preserves holds and never refreshes, upgrades or requests reboot. Security mode retains the previous conservative `upgrade` behavior; it is not a security-origin filter.

The root boundary accepts only version, canonical command UUID, mode and the policy-controlled reboot-class flag. Requests cannot supply packages, paths, repositories, apt flags, environment or systemd properties. Every mutating apt invocation includes `--no-remove`; no remove/purge/autoremove, force-unlock, unauthenticated, downgrade or held-change override is added. Package tools receive a fixed environment and null stdin.

The worker, signed apply helper, boot fallback and installer share a protected nonblocking flock. Unit/current-record checks also cover systemd cleanup after a worker exits. The installer additionally holds all four native POSIX package locks during legacy migration. Signed agent replacement is refused before stopping/replacing the agent while maintenance is active or unresolved. Durable updater acknowledgement distinguishes accepted launch, deferral, failure and installation; staging alone cannot claim success or make the agent exit.

Root state uses bounded regular-file reads, no-follow pinned directory traversal, validated owners/modes, fresh exclusive temporary inodes, fsync and atomic rename. UUID terminal records are immutable and matched to their body UUID. A 16 MiB/32-record capacity gate refuses new work without deleting history. Current output is capped at 256 KiB; existing result redaction/sanitization runs before server reporting. Journald retains local full output subject to host policy.

Startup reattaches the exact maintenance command, reserves active host work and uses the existing durable outbox. A vanished worker is reported as interrupted, never replayed. A subsequent explicit request can run bounded prior hold cleanup; it cannot silently resume the old upgrade. Read-only doctor inspects the effective service user, unit, helper trust, sudo policy and lock-aware apply protocol without running repair or starting services. Missing/legacy privileged installation fails closed with a root-installer hint.

Manual and automatic signed-update handoffs persist the expected SHA-256 and request timestamp before launching the verifier. An accepted handoff has a terminal watcher, so a later failure before agent stop releases the reservation and reports failure. A different update cannot complete an earlier command. Host reservation ignores an old terminal current UUID when a new worker is activating. Hold reconciliation is drained behind a mutex barrier after host reservation and suppressed until the transaction ends. Post-patch reboot claims are durable and never replayed; a host reservation covers the delay and a fresh maintenance check suppresses reboot if work became active.

The release workflow signs and publishes both helpers for all five Linux architectures plus the fixed service asset. Exact basename verification and agent-first manifest ordering preserve old agent lookup compatibility. Installer failure fixtures demonstrate preservation of the existing binary, unit, config/identity and managed policy; unrelated administrator rules are not removed.

The new CI boundary job runs root IO/process/flock fixtures in an unprivileged Linux container, signed installer fixtures with real OS validators in a separate Debian container, and real apt recovery fixtures in another network-disabled container. Repository/module mounts are read-only and no host Docker socket or privileged cgroup mount is exposed.

## Verification and evidence

The complete command outputs are retained in the accompanying validation text and [packaging evidence](MAINT-RECOVERY-001-packaging-evidence.md). Packaging evidence includes fourteen installer scenarios, real OpenSSL/visudo/systemd-analyze validators, manifest ordering and eighteen actual cross-builds.

Acceptance coverage:

| Criteria | Evidence |
|---|---|
| AC-001/002 | Strict protocol and closed argv/environment tests; fixed service handoff recording; malformed/duplicate/extra/type/UUID/size rejection before execution. |
| AC-003/004 | Finite runner matrix plus actual disposable Debian 12 and 13 package fixtures: failed postinst/configure, successful configuration recovery, missing dependency repair and removal-required refusal retaining the package. |
| AC-005 | Real Linux flock contention, apply/boot refusal without service/binary/staging mutation, updater host/Docker reservations and installer/native package lock fixtures. |
| AC-006/007 | Independent harmless worker survives observer SIGKILL; restart/outbox tests; child process-group lifetime, bounded output drain, timeout/TERM/KILL and unknown-outcome tests. |
| AC-008/009 | Managed/operator hold and detached adapter regressions, cleanup error handling, startup exclusion, repair/no-reboot and durable result/reboot tests. |
| AC-010 | Symlink/FIFO/hardlink/wrong owner/mode/parent/oversize/file-swap fixtures, UUID/body mismatch rejection, bounded immutable history/capacity refusal and existing result redaction. |
| AC-011/012 | All Linux helper/agent cross-builds and existing non-Linux targets; signed installer failure/migration cases; effective unit/sudo policy and non-mutating doctor tests. |

## Operational boundary and limitations

No production package upgrade, OpenMediaVault repair, reboot, schema change or new release deployment was performed by this implementation. Vermont's separate, already verified legacy privilege hardening is an operational action, not evidence that the new worker has been deployed there.

The Linux container exercises real descriptors, process groups, observer death, package management and policy validators. It does **not** run systemd as PID 1: service activation is recorded in installer fixtures, while the production unit is checked with real `systemd-analyze verify`. A real systemd host smoke test remains a rollout gate before claiming end-to-end service/cgroup behavior. No privileged host cgroup mounts or production package tests were used to bypass that limitation.

Package maintainer scripts can still restart services, migrate applications or change mounts. No-removal is not rollback. Host OOM, power loss and root administrator intervention can interrupt work. Existing exact reboot capability remains an explicitly separate privilege; agent coordination does not prevent a privileged administrator or a compromised account already granted reboot from forcing a reboot.

Two local environment artifacts were diagnosed rather than treated as product regressions: Windows sandbox restrictions blocked test-server sockets/cache renames; those tests passed outside that sandbox. An early Linux run crossed a wall-clock minute in the unchanged quota sampling test; the complete subsequent suite passed. Mid-edit interface compilation failures were rerun after integration settled.

## Handoff

Independent QA and technical writing follow the completed, committed implementation. They must retain the real-systemd rollout limitation and the explicit root migration requirement; agent-only updates do not grant new privileged capabilities.
