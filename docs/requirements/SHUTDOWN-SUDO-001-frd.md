# Formal Requirements Document

- **Story ID:** SHUTDOWN-SUDO-001
- **Title:** Opt-in poweroff grant in the installer, so remote shutdown works on installer-managed Linux hosts
- **Author:** Business Developer Agent
- **Date:** 2026-10-25
- **Revision:** 1
- **Status:** DRAFT. Product decisions are defaults set on Ryan's behalf because Ryan was unavailable. Ryan reviews them in the PR. Section 11.1 lists each one, and the FRD is written to them.
- **Origin:** Follow-up D-009 of SHUTDOWN-DPKG-001 (`docs/requirements/SHUTDOWN-DPKG-001-frd.md`), merged in PR #35

## Changelog

- **Revision 1 (2026-10-25):** First draft.

---

## 1. Executive Summary

The dashboard's Shutdown command has never worked on Linux hosts set up by `scripts/install.sh`. The agent runs `sudo systemctl poweroff`, but the installer's sudoers drop-in grants only `systemctl reboot`, so sudo refuses. Since SHUTDOWN-DPKG-001 the agent reports this refusal as a failed result. This story makes remote shutdown work, as an opt-in. The installer gains a new prompt, "Allow remote shutdown from dashboard?", which defaults to no. Only when the answer is yes, or when a preserved config already has `commands.allow_shutdown: true`, does the installer add exactly one sudoers line for `systemctl poweroff`. It also makes sure the final agent config agrees with that line. `riot-agent doctor` gains a check for the grant. Nothing else is granted, and the agent's shutdown code, the server and the dashboard do not change.

## 2. Background & Context

- **Defect:** the agent's shutdown path runs `sudo systemctl poweroff` (non-interactive, no TTY). The sudoers drop-in that `install.sh` writes (`/etc/sudoers.d/riot-agent`) grants `<systemctl path> reboot` unconditionally and has never granted `poweroff`. On every host whose rIOt sudo rules come only from the installer, shutdown is refused. Since SHUTDOWN-DPKG-001 (FR-027 to FR-032) the result truthfully says `shutdown: <err>` (failed).
- **Ryan's intent:** these are servers, and Ryan mostly uses reboot. Remote shutdown must work where Ryan wants it, and must be off unless Ryan chooses it.
- **Current installer behaviour (load-bearing):**
  - The feature prompts (reboot, patching, host terminal, Docker exec) are asked only when `/etc/riot/agent.yaml` does not exist when the installer runs ("fresh install"). They all default to no.
  - `-y` / `--yes` answers yes to every prompt and is documented as "enable all remote features". `--non-interactive` and a missing terminal take each prompt's default.
  - A reinstall with an existing `agent.yaml` preserves the config and asks no prompts.
  - A reinstall with `--bootstrap-key` and an existing config deletes the config and writes a new one. The prompts were skipped, so every remote feature is off in the new config. This is existing behaviour.
  - The sudoers drop-in is rewritten in full on every Linux run, validated before it is installed, and backed up under `/var/backups/riot_install.*`. Re-running `install.sh` as root is therefore how a host gets a new grant.
  - Today the drop-in is generated before the prompts are asked. The poweroff answer must be known when the drop-in is generated, or the drop-in must be produced later. The architect decides how (FR-023, Section 11.2).
- **Agent config:** `commands.allow_shutdown` already exists in the agent config, defaults to false, and gates the shutdown command. The server never sees it.
- **Doctor:** the `Permissions` section prints `Shutdown: <bool>` only. Other privileged features already have non-interactive `sudo -n -l` probes. When doctor runs as root, the maintenance probe checks the `riot` user's policy, because root's own listing would falsely pass.
- **AGENT-PRIV:** the capability-manifest work, which would derive sudoers from config, exists only on an unmerged branch. It is not in `main`. This story therefore changes the installer's fixed sudoers block. It records how shutdown maps onto the manifest later (NFR-006) and does not build that mapping.
- **Fleet:** the installer-managed Linux servers are hawaii, vermont, PiVPN and PiCUPS. AGENT-HARDEN-001 and PATCH-FLOW-001 already need a per-server `install.sh` re-run. If Ryan does that re-run after this story is released, the same re-run also applies the shutdown grant on servers where `allow_shutdown` is true.

## 3. Actors

| Actor | Description | Permissions |
|-------|-------------|-------------|
| Ryan (operator, root on each host) | Runs `install.sh` as root, answers the prompts, edits `/etc/riot/agent.yaml`, runs `riot-agent doctor`, issues Shutdown from the dashboard. | Root on the host. Existing authenticated dashboard access. Nothing changes. |
| Installer (`scripts/install.sh`) | Asks the prompts, writes the agent config on a fresh install, generates and installs the sudoers drop-in. | Runs as root. |
| rIOt Agent | Runs the shutdown command when `commands.allow_shutdown` is true. Runs `doctor`. | Runs unprivileged as user `riot`. May use only the sudo rules the installer granted. Gains at most the one `systemctl poweroff` rule, and only on opted-in hosts. |
| sudo / sudoers policy | Decides whether `riot` may run `systemctl poweroff`. | System component. Out of rIOt's control except for the rIOt drop-in. |
| rIOt Server and Dashboard | Dispatch the shutdown command and show its result. | Unchanged (FR-034). |

## 4. Functional Requirements

Terms used below:

- **Fresh install:** a Linux installer run where `/etc/riot/agent.yaml` does not exist when the run starts.
- **Reinstall:** a Linux installer run where `/etc/riot/agent.yaml` exists when the run starts.
- **Poweroff grant:** the sudoers line `riot ALL=(root) NOPASSWD: <S> poweroff`, where `<S>` is the same absolute `systemctl` path the installer uses in its existing `reboot` line.
- **Final config / final drop-in:** `/etc/riot/agent.yaml` and `/etc/sudoers.d/riot-agent` as they are when an installer run completes successfully.
- **Shutdown enabled (in a config):** defined in FR-017.

### A. Installer prompt (fresh install)

- **FR-001:** On a Linux fresh install, the installer must ask exactly one new question, with the text `Allow remote shutdown from dashboard?` and the hint `[y/N]`.
- **FR-002:** The shutdown question must come directly after `Allow remote reboot from dashboard?` and directly before `Allow remote OS patching from dashboard?`. The text, hints, defaults and relative order of all other prompts must not change.
- **FR-003:** The shutdown answer must be independent of the reboot answer. All four combinations (reboot yes or no, shutdown yes or no) must be possible, and neither answer may change the other.
- **FR-004:** In interactive mode, the answer must be read the same way as for the other prompts: `y` or `yes` (case-insensitive) means yes, and `n`, `no`, an empty answer, an unreadable terminal or any other text means no.
- **FR-005:** With `-y` or `--yes`, the installer must answer the shutdown question yes without waiting for input, and must print the same "(auto)" line that it prints for the other prompts.
- **FR-006:** With `--non-interactive`, or when no terminal is available, the installer must answer the shutdown question no.
- **FR-007:** On a reinstall, the installer must not ask the shutdown question, whatever the flags (this includes `-y`).

### B. Fresh install results

- **FR-008:** When the shutdown answer is yes, the final config must contain `allow_shutdown: true` in its `commands` section, whatever the reboot and patching answers.
- **FR-009:** When the shutdown answer is no, the final config must not enable shutdown: `allow_shutdown` must be absent or `false`.
- **FR-010:** For the same answers to the other prompts, the final config must give the agent the same values as before this story for `allow_reboot`, `allow_patching`, `host_terminal` and every `docker` setting. The rest of the config must not change.
- **FR-011:** When the shutdown answer is yes, the final drop-in must contain the poweroff grant exactly once.
- **FR-012:** When the shutdown answer is no, the final drop-in must contain no line that grants `poweroff`.

### C. Sudoers content and validation

- **FR-013:** Apart from the poweroff grant, the final drop-in must be byte-identical to the drop-in the pre-story installer writes on the same host in the same state. Every existing line must keep its content and its relative order. Removing the poweroff grant line from a "yes" drop-in must give exactly the "no" drop-in.
- **FR-014:** The story must grant nothing else. The drop-in must not grant: `halt`; the `shutdown` or `poweroff` binaries; any `systemctl` verb that was not granted before this story, apart from `poweroff`; `systemctl poweroff` with extra arguments (for example `--force` or `-i`); a wildcard or argument pattern on the new line; or anything to a principal other than `riot`.
- **FR-015:** The drop-in with the poweroff grant must go through the same validation as today: the candidate policy is checked, and the effective policy is checked after install. If validation fails, the installer must stop and keep or restore the previous managed policy, as it does today.

### D. Reinstall with a preserved config

- **FR-016:** On a reinstall without `--bootstrap-key`, the final drop-in must contain the poweroff grant if and only if the preserved config has shutdown enabled (FR-017). This applies with `-y`, with `--non-interactive`, and with or without a terminal. No flag overrides the config.
- **FR-017:** A config has **shutdown enabled** only when the top-level `commands` mapping contains the key `allow_shutdown` with the YAML boolean value `true`. Each of these counts as **not** enabled (fail closed):
  - the key or the `commands` section is absent;
  - the value is `false`, or any value the installer cannot classify as boolean true;
  - the line is commented out;
  - `allow_shutdown` appears under any section other than the top-level `commands`;
  - the file cannot be parsed as YAML, cannot be read, or is not a regular file.

  The installer must never treat a config as shutdown-enabled when the agent would read `commands.allow_shutdown` as false. If the agent would read a spelling as true and the installer does not recognise it, the installer counts it as not enabled. Doctor then reports the missing grant (FR-027).
- **FR-018:** On a reinstall without `--bootstrap-key`, the installer must not rewrite, edit, truncate, re-serialise or reformat the preserved config. Its bytes, owner and mode must be unchanged by the run.
- **FR-019:** A reinstall where the previous drop-in had the poweroff grant and the preserved config does not have shutdown enabled must remove the grant. Re-running the installer is how Ryan revokes the grant.

### E. Reinstall with `--bootstrap-key`

- **FR-020:** On a reinstall with `--bootstrap-key`, where the config is deleted and written again, the final drop-in must contain the poweroff grant if and only if the newly written final config has shutdown enabled.
- **FR-021:** In that path, the newly written config must handle shutdown the same way it handles the other remote features today: no prompt is asked, and `allow_shutdown` must be absent or `false`. A host that had shutdown enabled before re-enrolment therefore ends with neither `allow_shutdown: true` nor the grant (Section 11.1, DEC-007).

### F. Agreement invariant

- **FR-022:** After every successful Linux installer run (fresh install, reinstall or bootstrap reinstall, with any mode flag), the final drop-in must contain the poweroff grant if and only if the final config has shutdown enabled. A grant without `allow_shutdown: true` must never occur, and `allow_shutdown: true` in a config the installer wrote must never occur without the grant.
- **FR-023:** FR-022 must hold even though the installer today generates the drop-in before it asks the prompts and before the bootstrap reset. How the ordering is resolved is the architect's decision, stated in the ADD.
- **FR-024:** Non-Linux installs must not change: no shutdown question, no `allow_shutdown` written, and the same output and files as before this story.

### G. Doctor

- **FR-025:** On Linux, when `commands.allow_shutdown` is true, `riot-agent doctor` must check whether the `riot` user may run the agent's shutdown command (`systemctl poweroff` via sudo) without a password. The check must not run the command.
- **FR-026:** When doctor runs as root, the check must evaluate the `riot` user's sudo policy, not root's. When doctor runs as a non-root user, it must use the same approach as the existing doctor sudo checks.
- **FR-027:** When the check finds the grant, doctor must print a pass line that names the poweroff grant. When the check finds no grant, or cannot complete (sudo missing, password required, error or timeout), doctor must print a fail line plus guidance to set `allow_shutdown: true` and re-run the install script as root.
- **FR-028:** The check must never prompt for a password, and must finish within a fixed timeout that the ADD states. The timeout must not exceed the 15-second bound of the existing doctor sudo checks.
- **FR-029:** When `commands.allow_shutdown` is false, or the OS is not Linux, doctor must not run the check and must print no shutdown pass or fail line. The existing `Shutdown: <bool>` line in `Permissions` must stay as it is.
- **FR-030:** Doctor must not run `poweroff`, `reboot`, `halt`, `shutdown` or any other command that changes the host's power state, whatever the check's outcome.
- **FR-031:** The check must ask about the same command and arguments that the agent's shutdown path runs, taken from the same source, so it cannot pass where the agent's real shutdown would be refused.

### H. Unchanged behaviour

- **FR-032:** The agent's shutdown command path must not change: its `allow_shutdown` gate, the dpkg wait, the guards, the poweroff command and its arguments, and the result outcomes from SHUTDOWN-DPKG-001.
- **FR-033:** The reboot question, its default, its config mapping and the existing `systemctl reboot` grant must not change.
- **FR-034:** The server, the API and the dashboard behaviour must not change.
- **FR-035:** The installer's flags, their documented meanings (including "`-y` enables all remote features") and the usage text must not change, except for documentation of the new question.

### I. Documentation

- **FR-036:** The README must describe:
  - the new installer question, its default (no), and that `-y` answers yes while `--non-interactive` and no terminal answer no;
  - that the question is asked only on a fresh install;
  - how to enable shutdown later: Ryan sets `commands.allow_shutdown: true` in `/etc/riot/agent.yaml`, then re-runs `install.sh` as root;
  - how to revoke it: Ryan sets the value to false (or removes it), then re-runs `install.sh` as root;
  - that agent-only updates do not add or remove the grant;
  - the new doctor check.
- **FR-037:** The README and the CHANGELOG must say which servers need action. Every installer-managed Linux server where Ryan wants remote shutdown (the fleet is hawaii, vermont, PiVPN and PiCUPS) needs Ryan to set `allow_shutdown: true` and then re-run `install.sh` as root. Servers where Ryan does not want remote shutdown need nothing.
- **FR-038:** The README must no longer say that shutdown "reports failed until SHUTDOWN-SUDO-001" or that the installer grants no poweroff rule. No user-facing documentation may keep either statement.
- **FR-039:** The CHANGELOG must have an entry for this story that names the new question, its default, and the single sudoers line it adds.

## 5. Non-Functional Requirements

- **NFR-001 [Security, least privilege]:** Opting in adds exactly one sudoers line (`<systemctl path> poweroff`, no extra arguments, for `riot` only). Not opting in adds nothing. The grant follows the config, and remote shutdown is off by default.
- **NFR-002 [Bounded growth]:**
  - No new persistent state: no new files, directories, database tables, columns, migrations, telemetry fields or event types.
  - The drop-in grows by at most one line.
  - The installer prints at most one extra prompt line per run.
  - The existing backup under `/var/backups/riot_install.*` is unchanged in what it contains. It gains no new file.
  - Doctor runs at most one extra sudo check per run, bounded by its timeout (FR-028), and writes nothing to disk.
  - No failure path creates a file, log or record that grows with retries.
- **NFR-003 [Performance]:** The installer makes no extra network requests. Reading the preserved config for FR-016 takes a bounded time and does not depend on the network. The doctor check finishes within its stated timeout of 15 seconds or less.
- **NFR-004 [Compatibility]:**
  - Hosts where `install.sh` is not re-run keep their current drop-in and behaviour.
  - A new agent with an old drop-in and `allow_shutdown: true`: shutdown still reports failed, truthfully, and doctor reports fail with re-run guidance.
  - No server or agent upgrade order is required.
- **NFR-005 [Testability]:** Every installer requirement must be verifiable inside a disposable container fixture by inspecting files, the installer's output and `sudo -l` policy listings, without any power action. Every doctor requirement must be verifiable with an injected command runner.
- **NFR-006 [Forward compatibility, AGENT-PRIV]:** When the capability manifest from AGENT-PRIV lands in `main`, shutdown must map to its own capability. The capability is derived from `commands.allow_shutdown` and grants exactly `<systemctl path> poweroff`, with no other verb and no extra arguments. This story records the mapping and does not build it. It must not touch the AGENT-PRIV branch.
- **NFR-007 [Accepted residual, stale grant]:** If Ryan sets `allow_shutdown` to false and does not re-run the installer, the grant stays until the next run. The agent still refuses shutdown because its own gate is false. Doctor does not check when the value is false (FR-029), so it does not report the stale grant. Revocation is documented (FR-036).
- **NFR-008 [Security review input, config writable by the agent user]:** `/etc/riot/agent.yaml` is owned by and writable by `riot`. A compromised agent could set `allow_shutdown: true`, and the next root re-run of the installer would then grant `systemctl poweroff`. The exposure is limited to poweroff and needs Ryan's root re-run. The security reviewer decides whether to accept it (Section 11.2, OQ-A3).

### Hard safety constraints (binding on every pipeline agent, test and helper)

- **SC-001:** No test, helper, fixture or pipeline agent may ever run a real `shutdown`, `poweroff`, `halt` or `reboot`, or any equivalent (for example `systemctl poweroff`, `systemctl reboot`, `systemctl halt`, `init 0`, `telinit`), on any host. This includes Ryan's Windows PC (`shutdown` there), Hawaii, vermont, PiVPN, PiCUPS and the CI runners. Policy may be checked only with listing commands such as `sudo -n -l` or `sudo -n -U riot -l`, which never run the command.
- **SC-002:** No pipeline agent may run an installed `riot-agent` binary on any host, including `riot-agent doctor` on a fleet server. Doctor behaviour is verified through unit tests with injected runners. On real hosts, Ryan runs doctor.
- **SC-003:** The real `install.sh` may run only inside a disposable container fixture, never on a fleet host or on Ryan's PC. Re-running the installer on fleet servers is Ryan's action (FR-037).
- **SC-004:** No prose inside Bash heredocs. Text is written to a file first and then passed by path.
- **SC-005:** The AGENT-PRIV branch must not be modified (NFR-006).

## 6. Business Rules

- **BR-001:** Remote shutdown is opt-in. Every way of setting up a host defaults to off, except `-y`, which is documented as "enable all remote features".
- **BR-002:** The poweroff grant exists on a host only when that host's agent config enables shutdown. The config is the single source of truth, and the installer makes the drop-in follow it (FR-022).
- **BR-003:** When in doubt, no grant (fail closed, FR-017).
- **BR-004:** Only a root run of `install.sh` changes the rIOt sudoers drop-in. Agent updates never add or remove the grant.
- **BR-005:** The installer never edits a preserved config. Ryan changes the config by hand, and the installer only reads it (FR-018).
- **BR-006:** Shutdown and reboot are separate permissions with separate questions, separate config keys and separate sudoers lines.

## 7. Data Requirements

- **Entities:**
  - Installer answer to the shutdown question: transient, held only for the run, never stored.
  - `commands.allow_shutdown` in `/etc/riot/agent.yaml`: existing boolean, default false. No new key.
  - `/etc/sudoers.d/riot-agent`: existing file. Opted-in hosts gain the poweroff grant line.
- **Validation:**
  - "Shutdown enabled" is defined only by FR-017.
  - The new sudoers line is exactly `riot ALL=(root) NOPASSWD: <S> poweroff`, where `<S>` is the absolute `systemctl` path from the existing reboot line.
  - The whole drop-in passes the existing candidate and effective policy validation (FR-015).
- **Host shutdown-capability states** (derived from the config and the drop-in; nothing new is stored):

| State | `allow_shutdown` | Poweroff grant | Shutdown result | Doctor |
|-------|------------------|----------------|-----------------|--------|
| Off | false or absent | absent | Refused by the agent gate | No check |
| Enabled | true | present | Works (SHUTDOWN-DPKG-001 outcomes) | Pass |
| Config only | true | absent | `shutdown: <err>` (failed) | Fail plus re-run guidance |
| Stale grant | false or absent | present | Refused by the agent gate | No check (NFR-007) |

| From | Event | To |
|------|-------|----|
| (no install) | Fresh install, answer yes | Enabled |
| (no install) | Fresh install, answer no | Off |
| Off | Ryan sets `allow_shutdown: true`, no re-run | Config only |
| Config only | Ryan re-runs `install.sh` as root | Enabled |
| Config only (every fleet server today that has the flag set) | No action | Config only |
| Enabled | Ryan sets `allow_shutdown: false`, no re-run | Stale grant |
| Stale grant | Ryan re-runs `install.sh` as root | Off |
| Any | Re-enrolment with `--bootstrap-key` (existing config) | Off (FR-021) |
| Any | Agent-only update | Unchanged (BR-004) |

## 8. Acceptance Criteria

Installer criteria are checked in a disposable root container fixture (SC-003). Policy is checked only by listing (SC-001).

### AC-001: Fresh install, answer yes [Maps to FR-001, FR-004, FR-008, FR-011]
- **Given** a Linux container with no `/etc/riot/agent.yaml`, run interactively
- **When** Ryan answers `y` to "Allow remote shutdown from dashboard?" and `n` to every other question
- **Then** the final config has `commands.allow_shutdown: true`
- **And** the final drop-in contains the poweroff grant exactly once
- **And** `sudo -n -U riot -l <S> poweroff` (listing only) reports the command as allowed

### AC-002: Fresh install, empty or unrecognised answer means no [Maps to FR-004, FR-009, FR-012]
- **Given** a Linux fresh install, run interactively
- **When** the shutdown answer is empty, and in separate runs `n`, `no`, `maybe` and an unreadable terminal
- **Then** in each run the final config does not have `allow_shutdown: true`
- **And** the final drop-in contains no line that grants `poweroff`

### AC-003: Question text and position [Maps to FR-001, FR-002]
- **Given** a Linux fresh install, run interactively
- **When** the installer output is captured
- **Then** it shows `Allow remote shutdown from dashboard? [y/N]` directly after the reboot question and directly before the OS patching question
- **And** the text, hints and order of the other questions match the pre-story installer

### AC-004: Shutdown and reboot are independent [Maps to FR-003, FR-008, FR-009, FR-010, BR-006]
- **Given** four Linux fresh installs
- **When** reboot and shutdown are answered (y,y), (y,n), (n,y) and (n,n)
- **Then** each final config has `allow_reboot` and `allow_shutdown` matching its answers
- **And** each final drop-in has the reboot grant (always, as today) and has the poweroff grant only when shutdown was answered y
- **And** for the same answers, `allow_patching`, `host_terminal` and `docker` read the same as with the pre-story installer

### AC-005: `-y` answers yes [Maps to FR-005, FR-008, FR-011, BR-001]
- **Given** a Linux fresh install run with `-y`
- **When** the installer completes
- **Then** the output shows the shutdown question answered "y (auto)"
- **And** the final config has `allow_shutdown: true` and the final drop-in contains the poweroff grant

### AC-006: `--non-interactive` and no terminal answer no [Maps to FR-006, FR-009, FR-012]
- **Given** a Linux fresh install run with `--non-interactive`, and separately one with no terminal available
- **When** each installer run completes
- **Then** neither final config has `allow_shutdown: true`
- **And** neither final drop-in contains a line that grants `poweroff`

### AC-007: Existing sudoers lines are byte-identical [Maps to FR-013]
- **Given** the same container image, installed once with the pre-story installer, once with shutdown answered no, and once with shutdown answered yes
- **When** the three drop-ins are compared
- **Then** the "no" drop-in is byte-identical to the pre-story drop-in
- **And** the "yes" drop-in with the poweroff grant line removed is byte-identical to the "no" drop-in

### AC-008: Nothing else is granted [Maps to FR-014, NFR-001]
- **Given** a Linux fresh install with shutdown answered yes
- **When** the `riot` policy is listed for each command below (listing only)
- **Then** `<S> poweroff` is allowed
- **And** these are refused: `<S> poweroff --force`, `<S> poweroff -i`, `<S> halt`, `<S> kexec`, `<S> suspend`, the `shutdown` binary, the `poweroff` binary, the `halt` binary
- **And** the new line contains no wildcard or argument pattern and names `riot` as its only principal

### AC-009: Policy validation still guards the install [Maps to FR-015]
- **Given** a Linux fresh install with shutdown answered yes, where the candidate policy is made invalid in the fixture
- **When** the installer runs
- **Then** it stops with the existing "policy is invalid" error
- **And** the previous managed drop-in (or none, if there was none) is in place

### AC-010: Reinstall with shutdown enabled emits the grant, with no question [Maps to FR-007, FR-016]
- **Given** a Linux host with an existing config containing `commands:` / `allow_shutdown: true` and a drop-in without the poweroff grant
- **When** Ryan re-runs the installer with no server URL, and separately with `-y`
- **Then** neither run asks the shutdown question
- **And** after each run the final drop-in contains the poweroff grant exactly once

### AC-011: Reinstall fails closed for every non-enabled form [Maps to FR-016, FR-017, BR-003]
- **Given** separate Linux reinstalls whose existing config has:
  - (a) no `commands` section;
  - (b) `commands` without `allow_shutdown`;
  - (c) `allow_shutdown: false`;
  - (d) `# allow_shutdown: true` (commented out);
  - (e) `allow_shutdown: true` under `docker:` rather than `commands:`;
  - (f) `allow_shutdown: "maybe"`;
  - (g) an unparseable YAML file;
  - (h) a config path that is not a readable regular file
- **When** each reinstall runs
- **Then** no final drop-in contains a line that grants `poweroff`

### AC-012: Flags do not override the preserved config [Maps to FR-016]
- **Given** a reinstall with `allow_shutdown: false` run with `-y`, and a reinstall with `allow_shutdown: true` run with `--non-interactive`
- **When** each completes
- **Then** the first has no poweroff grant and the second has the poweroff grant

### AC-013: Reinstall revokes a stale grant [Maps to FR-019, NFR-007]
- **Given** a host whose drop-in has the poweroff grant and whose config now has `allow_shutdown: false`
- **When** Ryan re-runs the installer
- **Then** the final drop-in contains no line that grants `poweroff`
- **And** every other line still matches FR-013

### AC-014: Preserved config is untouched [Maps to FR-018, BR-005]
- **Given** a reinstall without `--bootstrap-key`, with an existing config that has `allow_shutdown: true`, unusual formatting and comments
- **When** the installer completes
- **Then** the config's bytes, owner and mode are identical to before the run

### AC-015: Bootstrap re-enrolment never leaves a mismatch [Maps to FR-020, FR-021, FR-022, FR-023]
- **Given** a Linux host whose existing config has `allow_shutdown: true` and whose drop-in has the poweroff grant
- **When** Ryan re-runs the installer with `--bootstrap-key`, once interactively and once with `-y`
- **Then** after each run the rewritten config does not have `allow_shutdown: true`
- **And** the final drop-in contains no line that grants `poweroff`

### AC-016: Bootstrap key on a fresh install behaves as a fresh install [Maps to FR-001, FR-008, FR-011, FR-020]
- **Given** a Linux host with no existing config
- **When** the installer runs with `--bootstrap-key` and shutdown is answered `y`
- **Then** the shutdown question was asked
- **And** the final config has `allow_shutdown: true` and the final drop-in has the poweroff grant

### AC-017: Agreement holds across the run matrix [Maps to FR-022, BR-002]
- **Given** every installer run used in AC-001 to AC-016 that completed successfully
- **When** each final config is read using FR-017, and each final drop-in is inspected
- **Then** in every run, "the drop-in has the poweroff grant" equals "the final config has shutdown enabled"

### AC-018: Non-Linux installs are unchanged [Maps to FR-024]
- **Given** the installer's non-Linux path (tested statically or in a stubbed fixture; no real macOS power action)
- **When** a fresh install runs there
- **Then** no shutdown question is asked, no `allow_shutdown` key is written, and the output and written config match the pre-story installer for the same answers

### AC-019: Doctor passes when the grant exists [Maps to FR-025, FR-027]
- **Given** a Linux agent config with `allow_shutdown: true` and an injected runner that reports the poweroff check as allowed
- **When** doctor runs
- **Then** it prints one pass line naming the poweroff grant

### AC-020: Doctor fails with guidance when the grant is missing or the check cannot complete [Maps to FR-027, FR-028]
- **Given** a Linux agent config with `allow_shutdown: true`
- **When** the injected runner reports, in separate runs, refused, password required, sudo not found, and no answer before the timeout
- **Then** each run prints one fail line plus guidance to re-run the install script as root
- **And** no run waits longer than the stated timeout (15 seconds or less), and no run prompts for a password

### AC-021: Doctor as root checks the riot user [Maps to FR-026]
- **Given** doctor running with effective UID 0 and `allow_shutdown: true`
- **When** the check runs
- **Then** the sudo invocation asks about user `riot`'s policy, not root's, using the same approach as the existing maintenance check
- **And** when doctor runs as a non-root user, the invocation matches the existing non-root doctor sudo checks

### AC-022: Doctor does not check when shutdown is off or the OS is not Linux [Maps to FR-029]
- **Given** `allow_shutdown: false` on Linux, and separately `allow_shutdown: true` on a non-Linux OS
- **When** doctor runs
- **Then** the injected runner receives no poweroff-related sudo call
- **And** no shutdown pass or fail line is printed
- **And** the `Permissions` section still prints `Shutdown: <bool>` as before

### AC-023: The doctor check matches the agent's real command [Maps to FR-031]
- **Given** the agent's shutdown command definition and the doctor check
- **When** both are inspected in a unit test
- **Then** the command and arguments the check asks about equal the command and arguments the shutdown path runs, and both come from the same source

### AC-024: Doctor never changes power state [Maps to FR-030, SC-001]
- **Given** every doctor test case for this story
- **When** the injected runner records every invocation
- **Then** each invocation is a non-executing policy listing (`sudo -n -l ...` or `sudo -n -U riot -l ...`)
- **And** no invocation runs `poweroff`, `reboot`, `halt` or `shutdown`

### AC-025: Agent shutdown path is unchanged [Maps to FR-032]
- **Given** the agent after this story
- **When** the SHUTDOWN-DPKG-001 shutdown tests, the reboot call-site test and the power-path scan are run
- **Then** they pass unchanged
- **And** the poweroff command and its arguments are byte-identical to the previous release

### AC-026: Reboot, server and dashboard are unchanged [Maps to FR-033, FR-034, FR-035]
- **Given** the repository after this story
- **When** the changes are reviewed and the existing tests are run
- **Then** the reboot question, its default and its sudoers line are unchanged
- **And** no server, API or dashboard source file is changed except for documentation text
- **And** the installer's flags and their documented meanings are unchanged

### AC-027: README documents the opt-in, the enable and revoke steps, and doctor [Maps to FR-036, FR-038]
- **Given** the README after this story
- **When** it is read
- **Then** it describes the shutdown question, its default, and its `-y` and `--non-interactive` behaviour
- **And** it gives the enable steps (set `allow_shutdown: true`, re-run `install.sh` as root) and the revoke steps
- **And** it says agent-only updates do not change the grant, and describes the doctor check
- **And** it contains neither "reports failed until SHUTDOWN-SUDO-001" nor any statement that the installer grants no poweroff rule

### AC-028: README and CHANGELOG name the servers that need a re-run [Maps to FR-037, FR-039]
- **Given** the README and the CHANGELOG after this story
- **When** they are read
- **Then** they say that each installer-managed Linux server where Ryan wants remote shutdown (fleet: hawaii, vermont, PiVPN, PiCUPS) needs `allow_shutdown: true` and then an `install.sh` re-run by Ryan as root, and that servers without it need nothing
- **And** the CHANGELOG entry names the new question, its default (no) and the single sudoers line it adds

### AC-029: Bounded growth [Maps to NFR-002, NFR-003]
- **Given** a fresh install answered yes, a reinstall, and a doctor run
- **When** the file system is compared before and after each, outside the drop-in and the existing backup directory
- **Then** no new file or directory exists, apart from what the pre-story installer creates
- **And** the drop-in has at most one more line than the pre-story drop-in
- **And** the backup directory's file list is the same as with the pre-story installer
- **And** the installer made no network request that the pre-story installer did not make

## 9. Out of Scope

- Any change to the agent's shutdown code, the dpkg wait, or shutdown outcomes and budgets (SHUTDOWN-DPKG-001).
- Any change to reboot: its question, grant, code or budgets.
- Server, API, database and dashboard changes, including any "shutdown not granted" indicator in the UI.
- Reporting `allow_shutdown` or grant state to the server.
- The AGENT-PRIV capability manifest and its apply helper. Only the future mapping is recorded (NFR-006).
- Doctor detection of a stale grant when `allow_shutdown` is false (NFR-007).
- Preserving remote-feature settings across a `--bootstrap-key` re-enrolment (existing behaviour, DEC-007).
- Granting `halt`, `shutdown`, `poweroff` with extra arguments, or any other power verb.
- Non-Linux privilege setup.
- Running the installer or doctor on fleet servers. Ryan does this (SC-002, SC-003).
- Fixing the existing fail-path behaviour where a mid-run failure after the drop-in is installed leaves the new drop-in in place (OQ-A2). Recorded only.

## 10. Assumptions

- **A-001:** The decisions in Section 11.1 were set by Clod on Ryan's behalf on 2026-10-25, because Ryan was unavailable. Ryan reviews them in the PR.
- **A-002:** In `main`, `install.sh` is the only component that writes `/etc/sudoers.d/riot-agent`. The AGENT-HARDEN-001 apply helper and agent updates do not write it. The architect confirms this.
- **A-003:** The agent invokes `sudo systemctl poweroff` with a bare `systemctl`. Sudo resolves it to the same absolute path that the installer writes in the drop-in (`command -v systemctl` under the installer's fixed `PATH`). The architect confirms this on Debian 13 (hawaii), Raspberry Pi OS / DietPi (PiVPN, PiCUPS) and vermont's OS, because a path mismatch would make the grant ineffective. AC-001 and AC-023 would expose it.
- **A-004:** A sudoers command with arguments allows only exactly those arguments. The poweroff grant therefore does not allow `poweroff --force` or other variants (AC-008).
- **A-005:** Fleet servers are installer-managed. Their rIOt sudo rules come only from the installer's drop-in.

## 11. Open Questions

### 11.1 Decisions taken on the owner's behalf

Each item is decided by its default, and the FRD is written to it. Status of every item: **decided (default), Ryan reviews in PR.**

- **DEC-001: Opt-in question.** New question "Allow remote shutdown from dashboard?", default no, separate from the reboot question and directly after it. Asked only on a fresh install (FR-001, FR-002, FR-007).
- **DEC-002: Mode flags.** `-y` / `--yes` answers yes, consistent with its documented "enable all remote features". `--non-interactive` and no terminal answer no (FR-005, FR-006).
- **DEC-003: Exactly one new line.** `riot ALL=(root) NOPASSWD: <systemctl path> poweroff`, and only on opt-in. All existing lines are byte-identical in content and order. No `halt`, no `shutdown` binary, no wildcard, no extra arguments (FR-011 to FR-014).
- **DEC-004: Reinstall follows the preserved config.** No question is asked. The grant is emitted if and only if the existing config has `commands.allow_shutdown: true`. No flag overrides it (FR-016).
- **DEC-005: Enabling later.** Ryan sets `allow_shutdown: true` in `/etc/riot/agent.yaml` and re-runs `install.sh` as root. The installer never edits the preserved config (FR-018, FR-036).
- **DEC-006: Fail closed on unclear config.** Any absent, false, commented, misplaced, unclassifiable, malformed or unreadable value counts as not enabled (FR-017).
- **DEC-007: Bootstrap re-enrolment.** Shutdown follows the existing reset of all remote features: the rewritten config does not enable shutdown, so the grant is not emitted. Agreement (FR-022) is the hard requirement. Carrying the old setting across re-enrolment is out of scope (FR-021).
- **DEC-008: Doctor check.** Only on Linux and only when `allow_shutdown` is true. It is a non-interactive listing, against `riot` when run as root. It reports pass, or fail plus re-run guidance. There is no check when the value is false (FR-025 to FR-029).
- **DEC-009: Non-Linux.** No question and no config key. Behaviour is unchanged (FR-024).
- **DEC-010: Revocation.** Ryan sets the value to false and re-runs the installer, which removes the grant. A stale grant without a re-run is accepted and documented (FR-019, NFR-007).
- **DEC-011: AGENT-PRIV.** Implement in the installer's fixed sudoers block now. Record the future mapping: a shutdown capability, derived from `commands.allow_shutdown`, grants exactly `systemctl poweroff` (NFR-006).
- **DEC-012: Fleet action.** Documentation tells Ryan to set `allow_shutdown: true` and re-run `install.sh` as root on each server where remote shutdown is wanted. The FRD does not decide which of hawaii, vermont, PiVPN and PiCUPS those are. Ryan chooses (FR-037).

### 11.2 Questions for the architect and security reviewer

These are not product questions. They must be answered in the ADD or the security review before engineering starts.

- **OQ-A1 (architect): Ordering.** How the shutdown answer, or the preserved config value, and the bootstrap reset reach drop-in generation so that FR-022 holds. Today the drop-in is generated before the prompts. Observation: the prompts are currently asked after the old agent is stopped and while the installer holds the package locks, with a 15-minute self-termination bound on the lock helper. Any reordering must not make that window longer.
- **OQ-A2 (architect / security): Failure after the drop-in is installed.** Today, `fail` after the drop-in is installed does not restore the previous drop-in, except at the explicit policy-validation points. FR-022 covers successful runs only. The ADD must state the state left behind when a fresh install with "yes" fails between installing the drop-in and writing the config: a grant can be left with no config. The security reviewer decides whether that is acceptable (recorded in Out of Scope).
- **OQ-A3 (security): Config writable by `riot` influences a root-written grant** (NFR-008). Accept, or require a mitigation (for example, the installer showing the value it read before granting). Any mitigation becomes an FRD revision.
- **OQ-A4 (architect): Recognised spellings of true.** FR-017 requires at least the literal `true` and fail-closed for everything else. The ADD states the exact set the installer recognises, and confirms that it is a subset of what the agent's config loader reads as true.
- **OQ-A5 (architect): Doctor timeout value** (FR-028), 15 seconds or less.

## 12. Dependencies

- **D-001:** SHUTDOWN-DPKG-001 (PR #35, merged): the truthful shutdown result that makes the missing grant visible, and the shutdown tests that must keep passing (FR-032, AC-025).
- **D-002:** The existing installer sudoers block, its validation and backup and restore (`scripts/install.sh`), and the installer container fixture used in CI (`scripts/test_install_maintenance.py`) together with the static installer checks (`scripts/maintenance_packaging_test.go`).
- **D-003:** The existing doctor sudo-check pattern, including the root-to-`riot` rewrite (`internal/agent/maintenance_doctor.go`, `internal/agent/doctor.go`).
- **D-004:** `commands.allow_shutdown` in the agent config (`internal/agent/config.go`). It exists already, and no new key is needed.
- **D-005:** AGENT-PRIV (unmerged branch): future capability mapping only (NFR-006). Not a blocker.
- **D-006:** AGENT-HARDEN-001 and PATCH-FLOW-001: both already need an `install.sh` re-run per server. A re-run after this release covers all three.
- **D-007:** A tagged release, so that the installer and the doctor change reach the servers. Ryan then re-runs the installer on the servers where shutdown is wanted (FR-037).
- **D-008:** Technical writer: README and CHANGELOG (FR-036 to FR-039).
- **D-009:** Pipeline: architect (ADD), then security-researcher review (OQ-A2, OQ-A3), then engineering.
- **D-010:** No new third-party dependency, no database migration and no new external service.
