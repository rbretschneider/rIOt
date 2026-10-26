# Security Review Report

**Story ID:** SHUTDOWN-SUDO-001
**Reviewer:** Security Researcher Agent
**Date:** 2026-10-25
**Verdict:** APPROVED WITH CONDITIONS

> Disclosure note: this repository is public. Exploit-grade detail for the
> pre-existing config-write issue (SEC-OBS-1 in the ADD) and for any escalation
> path is deliberately kept out of this file and recorded in a private
> out-of-repo note handed to the pipeline. What remains here is sufficient to
> implement and verify the fixes without handing an attacker a recipe.

---

## Threat Model Summary

### Assets
- The rIOt sudoers drop-in `/etc/sudoers.d/riot-agent` — the single file that
  decides what the unprivileged `riot` user may do as root. Gaining an extra
  verb here is the whole prize.
- Host availability — the new capability is `systemctl poweroff`, whose misuse
  is a denial-of-availability (the host powers off).
- `/etc/riot/agent.yaml` — riot-writable; read as root by the installer to
  decide the grant. Its integrity drives a root decision.
- Installer run integrity — the atomicity of the drop-in/binary/config sequence,
  so no run leaves a grant its config does not justify.

### Threat Actors
- **Compromised `riot` agent (authenticated low-privilege, local).** Primary
  actor. Can write `agent.yaml`, can create/relink entries in `/etc/riot`
  between runs, already holds `systemctl reboot` NOPASSWD and (on Docker hosts)
  docker-group root-equivalence.
- **Operator (Ryan, root).** Runs `install.sh`, answers the prompt, edits the
  config. Trusted, but the design must make a tampered config visible to him.
- **Remote dashboard attacker.** Out of scope for this story: the server, API
  and dashboard are unchanged (FR-034); the agent-side `allow_shutdown` gate and
  clearance flow are byte-identical (FR-032).
- **Supply chain.** No new third-party dependency, no new network request
  (NFR-002/003, D-010). Not applicable.

### Attack Surface Introduced
- One new sudoers line on opted-in hosts: `riot ALL=(root) NOPASSWD:
  <systemctl> poweroff`.
- A new root-run bash+awk reader of a riot-writable YAML file
  (`config_enables_shutdown`).
- A relocation of the installer's interactive prompt block to before any
  privileged mutation.
- One new, listing-only doctor probe (`sudo -n -l systemctl poweroff`, rewritten
  to `-U riot` under root).
- No new endpoints, no new persistent state, no schema change, no new secret.

---

## Findings

### CRITICAL
None.

### HIGH
None.

### MEDIUM

#### SEC-001: AD-007's non-regular-file refusal does not cover hard links
**Severity:** MEDIUM
**Domain:** Input validation / privileged file handling (TOCTOU)
**Location:** ADD AD-007; `scripts/install.sh` config read + fresh-write block
**Description:** AD-007 refuses a path that exists but is not a regular file
(dangling symlink, directory, FIFO, socket, device) and the classifier refuses
to read symlinks. A **hard link** to a regular file is itself a regular, non-symlink
file, so it passes both gates. The classifier will therefore dereference a
riot-created hard link and read whatever inode it points at.
**Attack Scenario (scrubbed):** The agent user hard-links `agent.yaml` to another
regular file it can legitimately link to, so that a later root run reads that
inode through the installer. Impact is contained by three independent factors:
the classifier output is a boolean held in root-only `$STAGE` and the file
content is never printed (no disclosure); the fresh-write branch is skipped when
the path already exists as a regular file, so no root write or ownership change
flows through the link; and the kernel default `fs.protected_hardlinks=1` on the
fleet's distros prevents linking to files riot cannot write. The residual is a
read of an attacker-chosen inode reduced to one enabled/not-enabled bit.
**Required Resolution:** Accept as a documented residual, OR (preferred,
defence-in-depth) have the classifier's pre-read checks and/or the AD-007
predicate also refuse `agent.yaml` when its link count is greater than one
(`stat -c %h`). Either way, QA must confirm no code path performs a root
write, `chown` or `chmod` of `agent.yaml` when the path pre-exists (i.e. on any
reinstall or any planted-file case). Not an architectural blocker.
**Blocks:** Not implementation. Must be resolved/confirmed before QA sign-off.

#### SEC-002: The C-4 amendment (AD-011) is only safe if the reference-rule test is implemented exactly as specified
**Severity:** MEDIUM
**Domain:** Defense-in-depth control integrity (power-path guarantee)
**Location:** ADD AD-011, AD-010; `internal/agent/power_paths_test.go`,
`internal/agent/shutdown_doctor_test.go`, `internal/agent/shutdown_doctor.go`
**Description:** The C-4 power-path scan folds string constants but does not fold
the *return value* of a function. AD-011 introduces a data-only function
`shutdownSudoCommand()` returning the `poweroff` verb and whitelists it from the
string rule. That whitelist is safe ONLY because a second AST test
(`TestShutdownDoctor_SS_C4_ListingCommandOnlyFeedsTheProbe`) restricts where
`shutdownSudoCommand`/`shutdownProbeArgs` may be referenced, so nobody can later
write `exec.Command("sudo", shutdownSudoCommand()...)` to launch a real poweroff
that the folder cannot see. If that reference-rule test is weakened, omitted, or
its allow-list of call sites is widened, the C-4 guarantee (only `execReboot`/
`execShutdown` may launch a power action) is silently broken.
**Attack Scenario:** A future change (or a malicious contributor) adds a code
path that calls the data-only function to actually launch poweroff. The string
rule is bypassed (the verb never appears as a literal at the launch site) and,
without the reference rule, CI stays green while a new unaudited power-launch
site exists.
**Required Resolution:** Implement AD-011 additively exactly as specified: the
shape check (`listingFuncShapeProblem`), the `powerListingFuncs` allowance
limited to dir `.`/package `agent`, the self-test cases (including the
collectors-package and wrong-shape negatives), AND the reference-rule test that
pins `shutdownSudoCommand` to exactly two references and `shutdownProbeArgs` to
one, with the probe passed only as `run(ctx, "sudo", shutdownProbeArgs()...)`.
The existing `TestPowerPaths_SD_C4_*` functions must remain byte-identical and
still pass. QA must diff `power_paths_test.go` to confirm only additions.
**Blocks:** Not architecture; a mandatory implementation+QA condition.

#### SEC-003: The runtime agreement/refusal checks (AD-005, AD-007 late) are load-bearing and their ordering must be verified
**Severity:** MEDIUM
**Domain:** Business-logic integrity / TOCTOU
**Location:** ADD AD-005, AD-006, AD-007; `scripts/install.sh`
**Description:** The drop-in is built and installed from an EARLY read of
`agent.yaml`, taken while the agent is still running and `/etc/riot` is still
riot-owned. The protection against a racing/compromised agent flipping the value
after that read is entirely in (a) the AD-005 agreement re-read performed AFTER
`/etc/riot` becomes `root:root 0700` and the agent is stopped, which `fail`s and
`restore_policy`s on any mismatch, (b) the AD-007 *late* refusal placed after the
same lockdown, and (c) the AD-006 trap that restores a pending grant on any
abnormal exit in the install→agreement window. If any of these is placed before
the directory lockdown, or the `GRANT_PENDING` flag is not set/cleared at exactly
the specified points, the race window or an orphaned grant reopens.
**Attack Scenario (scrubbed):** The agent user changes the config between the
early read and the stop so the installed drop-in and the host's real config
disagree. The AD-005 re-read under the locked directory is what catches this and
restores the previous policy; without correct placement a grant could persist
against a config that does not justify it.
**Required Resolution:** QA must verify in the fixture and by static check that:
the AD-007 late check and the AD-005 agreement check both run after the
`/etc/riot` `root:root 0700` lockdown and before any `agent.yaml` write/bootstrap
`rm`; `GRANT_PENDING` is set true immediately after the drop-in install and
cleared at every explicit `restore_policy` site and on agreement success; and the
`cleanup` trap restores policy before killing the lock helper. The ADD specifies
all of this correctly — this finding is to ensure the implementation matches,
not to change the design.
**Blocks:** Not implementation start; required before QA sign-off.

### LOW / INFORMATIONAL

#### SEC-004: OQ-A3 accepted residual depends on the AD-008 transparency line actually shipping
**Severity:** LOW
**Domain:** Authorization transparency
**Location:** ADD AD-008, §10 (OQ-A3); NFR-008
**Description:** `agent.yaml` is riot-writable, so a compromised agent can set
`allow_shutdown: true` and have the next root re-run render the grant. The
security team accepts this: the incremental capability is poweroff
(availability only), riot already holds `systemctl reboot` unconditionally and
is docker-group/root-equivalent on Docker hosts, and it needs both a compromised
agent and Ryan's deliberate re-run. The sole mitigation is the AD-008 line that
tells Ryan, at re-run time, what was read and whether a grant is being added.
**Required Resolution:** Accept. Condition: the AD-008 reinstall line must be
implemented and asserted in the fixture (it is, per AC-010/AC-015). Confirm the
"not enabled" variant also explains why a `yes`/`True` spelling did not count.
**Blocks:** Nothing.

#### SEC-005: Pre-existing `checkUpdatePrivilege` can false-pass as root (no `-U riot`)
**Severity:** LOW / INFORMATIONAL
**Domain:** Doctor accuracy (pre-existing, out of scope)
**Location:** `internal/agent/doctor.go` `checkUpdatePrivilege`; ADD §10 observation
**Description:** Unlike the maintenance check and the new shutdown check, the
existing update-privilege doctor probe lists the invoking user's own policy
without the `-U riot` rewrite, so when doctor runs as root it can report a pass
that does not reflect riot's real policy. This story correctly uses the
`agentPolicyRunner` rewrite for the new shutdown probe (FR-026, AC-021), so it
does not inherit the bug, and the ADD records the pre-existing issue as a
follow-up. No action in this story.
**Required Resolution:** Track for a separate follow-up story. Do not expand this
story's scope.
**Blocks:** Nothing.

#### SEC-006: Accepted fail-closed residuals R-1..R-5
**Severity:** LOW / INFORMATIONAL
**Domain:** Defense in depth
**Description:** The ADD's residuals are reasonable and all fail closed:
- R-1 (a YAML error outside the classifier grammar lets the classifier say
  enabled while the agent cannot load the file): no agent runs, so the grant is
  unusable; doctor reports a load error.
- R-2 (SIGKILL/power-loss in the AD-006 window): no agent runs with
  `allow_shutdown: true`; any re-run reconciles.
- R-4 (a hand-added password-requiring `riot` rule could make the `-n -l`
  listing pass while the agent is refused): fleet sudoers come only from the
  installer (A-005); narrow.
- R-5 (a `systemctl` shadowed in `/usr/local/{s,}bin`): the grant is ineffective
  and fails closed; doctor reports fail.
Also note, as positive defense in depth: even if the classifier over-reads a
config as enabled (a false positive on the grant side), the agent's own
`Commands.AllowShutdown` gate plus the C-3 clearance call-site checks still refuse
`execShutdown`, so a classifier false-positive is not itself an escalation.
**Required Resolution:** Accept as documented.
**Blocks:** Nothing.

---

## Domain assessment (what was checked)

- **Grant scope / sudo exact-argv (T-1, A-004).** The new line specifies the
  command with the single literal argument `poweroff`. Sudoers matches commands
  with arguments by exact argument vector, so `poweroff --force`, `poweroff -i`,
  `-i poweroff`, `halt`, `kexec`, `suspend`, and the `shutdown`/`poweroff`/`halt`
  binaries are all unmatched and denied. The line carries no wildcard or argument
  pattern and names only `riot`. The existing reboot line is unchanged and the
  poweroff echo is strictly additive (FR-013), so removing it yields the
  pre-story drop-in byte-for-byte. AC-008 verifies the refused variants
  empirically by listing only. Sound.
- **Reading a riot-writable file as root.** Symlinks are never dereferenced
  (classifier check 1); reads are bounded by `timeout 5` and a 64 KiB cap; NUL
  bytes are rejected; content is reduced to a boolean in root-only `$STAGE` and
  never printed. The fail-closed awk grammar is reject-first and its
  classifier-enabled⇒agent-enabled direction is pinned against the real
  `agent.LoadConfig` by the differential test (AD-004). The only gap is hard
  links (SEC-001), which is contained. The security-relevant over-grant direction
  is additionally neutralised by the agent's own gate (SEC-006).
- **Prompt-block relocation (T-7).** `ask_yn` and `/dev/tty` semantics are
  unchanged; prompts now run before any mutation, so a slow answer or Ctrl-C
  leaves the host untouched — a net improvement. No new privileged action becomes
  reachable before the existing prerequisite and policy validations; a user may
  now answer and then see a refusal, which is harmless.
- **Failure atomicity (OQ-A2 / AD-006).** The pending-grant rollback is correctly
  scoped to runs that introduced a grant, `$BACKUP` is always set before
  `GRANT_PENDING` can be true, and the restore is ordered before the lock-helper
  kill. Correct (see SEC-003 for the ordering verification condition).
- **Doctor probe (`sudo -n -U riot -l`).** The root→riot rewrite is extracted
  into `agentPolicyRunner` and shared with the maintenance check so they cannot
  drift; `-n` prevents any password prompt; a 10 s `CommandContext` timeout
  bounds and kills the child; the probe is listing-only and the argv is pinned to
  `execShutdown` at build time (AD-010). The check runs only when
  `allow_shutdown` is true and only on Linux. Sound.
- **C-4 amendment (AQ-1 / AD-011).** The amendment is additive and does not weaken
  the guarantee **provided** the reference-rule test ships as specified
  (SEC-002). Moving the literal to an unscanned package or obfuscating it were
  correctly rejected as evasion.
- **SEC-OBS-1 closure (AD-007).** The dangling-symlink / non-regular-file
  write-through and ownership-change vectors are closed by the early and
  (crucially) late refusals, backed by the classifier's symlink refusal and the
  fresh-write skip when the path pre-exists. Bootstrap `rm -f` on a planted link
  removes the link, not the target. Hard links are the one uncovered sub-case
  (SEC-001), and it does not reopen the write-through. The late check's placement
  after the `/etc/riot` lockdown is load-bearing (SEC-003).
- **AGENT-PRIV (NFR-006 / AD-012).** Confirmed read-only: the unmerged
  `AGENT-PRIV` manifest has `CapReboot` and no shutdown capability; nothing in
  this story touches that branch. The recorded forward mapping (derive
  `CapShutdown` from the agent's own `allow_shutdown`, render exactly `systemctl
  poweroff`) is consistent and documentation-only.

---

## Positive Observations
- One decision variable (`SHUTDOWN_GRANT`) driving both the sudoers line and the
  config line makes agreement true by construction, and the runtime agreement
  check (AD-005) plus pending-grant rollback (AD-006) turn FR-022 into a runtime
  guarantee for both the race and the failure paths. This is a notably strong
  design for a privilege-granting change.
- Least privilege is respected precisely: exactly one verb, no arguments, no
  wildcard, `riot` only, opt-in, off by default, and the grant follows the
  config rather than any flag.
- The fail-closed classifier is deliberately narrow (reject-first grammar) and is
  tested against the real agent loader, which is the right way to satisfy the
  FR-017 "never grant where the agent reads false" obligation.
- Defense in depth is layered: even a classifier false-positive is caught by the
  agent's own `allow_shutdown` gate and the C-3 clearance call-site checks.
- The relocation of the prompt block out of the lock window removes a real
  operational hazard (a human wait tripping the 15-minute lock-helper SIGTERM
  mid-replacement) with no security cost.

---

## Verdict Rationale

**APPROVED WITH CONDITIONS.** There are no CRITICAL or HIGH findings. The design
is sound and, in several places, materially stronger than the baseline. However,
the security posture of this change rests heavily on implementation details being
realised exactly as the ADD specifies, so the following conditions are binding on
`senior_dev` and must be verified by the QA engineer before sign-off:

1. **SEC-002** — implement the AD-011 C-4 reference-rule test and self-tests
   exactly as specified; keep the existing `TestPowerPaths_SD_C4_*` functions
   byte-identical; the whole power-path guarantee depends on it.
2. **SEC-003** — verify at implementation and in the fixture that the AD-007 late
   refusal and the AD-005 agreement check both run after the `/etc/riot`
   `root:root 0700` lockdown and before any `agent.yaml` write, and that
   `GRANT_PENDING` is set/cleared and the trap ordered exactly as the ADD states.
3. **SEC-001** — either refuse `agent.yaml` with a link count > 1, or
   explicitly confirm (by test) that no root write/`chown`/`chmod` of
   `agent.yaml` occurs when the path pre-exists; document the hard-link residual.
4. **SEC-004** — ship and assert the AD-008 transparency line on every reinstall.

None of these require changes to the ADD or FRD; they are implementation and
test-verification obligations. SEC-005 (pre-existing root false-pass in
`checkUpdatePrivilege`) and SEC-006 (residuals R-1..R-5) are documented for
awareness and tracked as out-of-scope follow-ups.

---

## Routing
APPROVED WITH CONDITIONS → proceed to `senior_dev`, passing this report
alongside the ADD. The QA engineer must verify conditions SEC-001..SEC-004 as
part of acceptance. Do not route back to the architect or business developer.
