# Security Review — MAINT-RECOVERY-001

**Date:** 2026-10-20
**Verdict:** APPROVED FOR IMPLEMENTATION against the current FRD/ADD, with the mandatory verification conditions below. No blocking design changes.

## Evidence and threat boundary

Read root `CLAUDE.md`, the complete paired FRD/ADD, the existing signed apply verifier, updater/command/hold/doctor contracts, installer sudoers/unit generation and SMART collector. Consulted the primary [apt-get manual](https://manpages.debian.org/bookworm/apt/apt-get.8.en.html), [flock manual](https://manpages.debian.org/bookworm/manpages-dev/flock.2.en.html) and [systemd service manual](https://www.freedesktop.org/software/systemd/man/255/systemd.service.html). This is design approval, not a claim that the new implementation or service lifecycle has passed QA.

The agent and its writable inbox/hold bookkeeping are untrusted across the root boundary. Root-owned helpers, installed package tools/configuration, signed release trust anchor, unit, lock and persistent result directory form the trusted boundary. A compromised agent must not gain caller-selected root execution, arbitrary root writes, arbitrary package operands or updater bypass. Package maintainer scripts and administrator-controlled apt configuration remain privileged local policy.

## Mandatory implementation and QA conditions

| ID | Condition |
|---|---|
| SEC-1 | Accept only the bounded protocol and fixed service/argv/environment in ADD sections 3–5. Explicitly reject duplicate JSON keys as well as unknown keys, trailing objects, malformed UUIDs/types and extra argv. `DisallowUnknownFields` alone does not reject duplicate keys. No executable/path/environment/apt option may come from the request. Preserve `allow_patching` and server-side include-reboot-class policy. |
| SEC-2 | Use secure no-follow parent/file handling for the hostile inbox; read and validate one snapshot. Root writes only beneath validated root-owned directories, using exclusive temporary files and atomic replacement. Reject wrong owner, unsafe modes, symlinks, FIFO/device inputs and oversized records. Root-lock creation must refuse a precreated non-root-owned file; never replace/unlink an active lock inode. Retention must only remove validated root-state records. |
| SEC-3 | The fixed unit owns the complete package process tree, outside the agent cgroup, with null stdin and drained bounded output. Preserve lock ownership through transaction, child termination and hold cleanup; do not release it merely when the observer exits or before descendants stop. Test real Linux descriptor/process behavior, including apply/boot contention, rather than relying solely on mocked lock calls. No fallback restart or successful-update event after a refused/deferred apply. |
| SEC-4 | Execute the exact finite recovery plan. Every mutating apt invocation includes the execution-time no-remove guard; no simulated-plan-only protection, force-unlock, remove/purge/autoremove, unauthenticated/downgrade/held-change override or arbitrary operands. Only the existing fixed conffile policy is permitted; it is not permission for generic force flags. A nonempty dpkg audit remains failure even with exit zero. One repair attempt, then fail closed. |
| SEC-5 | Persist exact root-owned UUID/phase/outcome before mutation and match it with the agent marker/unit. Busy/malformed/missing/unknown states reserve or fail closed, never implicitly queue/replay a package transaction. Timeout/forced termination is interrupted/unknown, not success. Preserve bounded output/redaction and exactly-once outbox/reboot-intent handling. Agent reconnect/cancellation cannot kill or relaunch the worker. |
| SEC-6 | Pause agent hold reconciliation while the worker owns maintenance; move release/reapply and version snapshots into the worker. Corrupt/unsafe hold metadata releases nothing; release only tracked, installed, held, classified names on an authorized update. Restore on failure/interruption with bounded durable cleanup; restoration failure prevents clean success. Repair keeps holds and cannot reboot. Agent-writable historical hold ownership remains existing trust, not proof against an already-compromised agent; do not claim otherwise. |
| SEC-7 | Verify every installed helper asset against exact-basename signed checksums, including cross-architecture packaging. Validate candidate and effective sudoers/unit before publication, preserve working policy on failure and remove only reviewed managed legacy grants. New agent code alone cannot assume an old root helper implements the lock protocol. Doctor inspects agent-effective privileges without starting maintenance or invoking dpkg/apt recovery. |

The ADD incorporates these boundaries and AC-001–012 require the relevant negative cases. Implementation may proceed; release requires the stated disposable Linux/package and lifecycle QA evidence. No production package action is authorized by this design approval.

## Separate Vermont operational review

Read-only SSH inspection confirmed the legacy riot-owned agent inode, blind root boot copy, arbitrary-root-shell sudo grants and bare SMART rule. Protected system directories and the root staging directory had safe ownership/modes. `/sys/block` contained sda–sdi. OMV had pending triggers and writecache was half-configured; its postinst invokes configuration migration and workbench triggers. Do not treat `dpkg --configure -a` as a harmless privilege probe.

The separately approved hardening plan is sound with a fresh signed root-owned agent inode, fixed signed root-owned apply helper, cleared/replaced boot precommand, validated exact sudo policy and active-package-lock check before restart. Hash-then-chown alone leaves a file-swap/held-writable-descriptor race; replace from the root-private verified snapshot. Exact SMART `--json=c --all /dev/sda` through `/dev/sdi`, plus exact `--version` for existing doctor, preserves current reads without the arbitrary arguments allowed by a bare command-path rule. New disks require reviewed policy additions. Correcting an unrelated sudoers file's mode while preserving its contents is metadata hardening, not permission to edit its grants. Package repair and OMV migration require their own impact review before execution.

## Residual limitations

No-removal prevents apt-selected removals, not every side effect of a trusted package script: configuration, service restarts, mounts and application migrations can still change. Holds cannot undo already-unpacked packages. Host-wide OOM, root intervention or power loss can interrupt work; bounded recovery is not rollback. The retained apt security mode is conservative upgrade behavior, not a security-origin selector.

## Implementation boundary correction: retained apt-mark grants

Final adversarial testing found that retaining `apt-mark hold *` / `unhold *` violates the compromised-agent boundary. In the disposable, network-disabled Debian 12 fixture, a caller-supplied `-oDir::Bin::dpkg=/tmp/riot-hold-boundary-probe` after `hold` selected a harmless marker-only executable and ran it as root. No production probe was run. Agent-side package-name validation cannot protect a direct sudo invocation by a compromised agent.

The correction narrows the existing grants: require preinstalled sudo 1.9.10 or newer for apt installation and match hold/unhold arguments with anchored POSIX regular expressions accepting only nonempty, space-separated package-name operands. Every operand begins with a lowercase alphanumeric character and continues only with lowercase alphanumerics, plus, dot, underscore, colon or hyphen. Reject leading option tokens, equals signs, slashes, shell syntax, newline/tab separators and empty lists. Do not add a new privileged helper mode, shell or package-management action. Unsupported sudo fails before changing the working installation.

The [sudo project's own sudoers source](https://raw.githubusercontent.com/sudo-project/sudo/main/docs/sudoers.mdoc.in) documents argument regular expressions from version 1.9.10, anchored with caret/dollar. Real visudo validation and real agent-user `sudo -n -l` positive/negative matching are required, including options before and after the subcommand and multiarch/multiple valid names. Read-only doctor must reject the old wildcard or an equivalent option-bearing grant even when the fixed maintenance-start rule is present.

This narrow correction is approved for implementation. The earlier QA verdict must be reissued against the corrected committed policy and tests before the PR is ready for review.
