# Architecture Decision Document

- **Story ID:** SHUTDOWN-SUDO-001
- **FRD Reference:** `docs/requirements/SHUTDOWN-SUDO-001-frd.md` (revision 1)
- **Origin:** follow-up D-009 of SHUTDOWN-DPKG-001 (`docs/architecture/SHUTDOWN-DPKG-001-add.md`, FINAL revision 3)
- **Security review:** pending. OQ-A2 and OQ-A3 are answered here and go to the security researcher with §10.
- **Author:** Architect Agent
- **Date:** 2026-10-25
- **Status:** DRAFT, revision 1. Ryan was unavailable, so every open question was decided by the architect on Ryan's
  behalf. Ryan reviews the decisions in the PR. They are listed in §0.

## Changelog

### Revision 1

First version. It answers FRD §11.2 OQ-A1 to OQ-A5, confirms or qualifies assumptions A-002 and A-003, and raises
architect flags AQ-1 to AQ-5 (§0.2) where the FRD's wording cannot be met literally.

---

## 0. Decisions taken on Ryan's behalf, and FRD flags

### 0.1 Answers to the FRD's open questions

| Item | Decision | Where |
|---|---|---|
| **OQ-A1** Ordering | The whole feature-prompt block (`ask_yn`, Docker detection, every prompt) moves out of the lock window, to directly after asset verification and before any privileged check or mutation. The shutdown decision `SHUTDOWN_GRANT` is computed once, right after the prompts, and drives **both** the sudoers line and the `allow_shutdown` line of a written config. A final agreement check runs after the config write and before any binary is replaced. The lock window loses the human wait and gains only one bounded local read (milliseconds, 5 s worst case) | AD-001, AD-002, AD-005 |
| **OQ-A2** Failure after the drop-in is installed | **Mitigated, narrowly.** When this run installed a drop-in that carries the poweroff grant, and the run ends before the agreement check passes, the `EXIT` trap restores the previous managed drop-in. No binary has been replaced at that point, so the host returns to its pre-run policy. Other fail paths are unchanged (FRD §9) | AD-006 |
| **OQ-A3** Config writable by `riot` | **Accepted residual with a transparency mitigation.** On every Linux reinstall the installer prints one line that says what it read and whether it grants poweroff. The incremental exposure is availability only, and needs a compromised agent plus Ryan's root re-run. The `riot` user already holds an unconditional `systemctl reboot` grant, and on Docker hosts it is in the `docker` group, which is root-equivalent | AD-008, §10 |
| **OQ-A4** Recognised spellings of true | Exactly one: the plain, unquoted, lowercase `true`, as `allow_shutdown: true` in a flat `commands:` block of a structurally plain file. Every other spelling is "not enabled". The agent's loader (`gopkg.in/yaml.v3` v3.0.1) also reads `True`, `TRUE`, `y`, `yes`, `on` and their case variants, quoted or not, as true for a `bool` field, so the installer's set is a strict subset. A differential Go test pins this against the real `agent.LoadConfig` | AD-003, AD-004 |
| **OQ-A5** Doctor timeout | **10 seconds** (`shutdownProbeTimeout`), below the 15-second bound | AD-009 |
| **A-002** | **Confirmed.** In `main`, `scripts/install.sh` is the only writer of `/etc/sudoers.d/riot-agent` (`atomic_install` at L501 and `restore_policy` at L183). `cmd/riot-agent-apply`, `cmd/riot-maintenance` and the agent only mention it in comments and messages | §2 |
| **A-003** | **Confirmed by mechanism, verified in fixtures, left for doctor on the fleet.** The agent's `sudo systemctl …` and the installer's `${SYSTEMCTL_PATH}` already agree for `reboot`, through the same resolution, on every host where remote reboot works (vermont: `/usr/bin/systemctl reboot` with `allow_reboot: true`). `poweroff` uses the identical path and resolution. sudo also matches commands by inode, so merged-`/usr` aliases (`/bin` → `/usr/bin`) match. The only divergence is a `systemctl` shadowed in `/usr/local/sbin` or `/usr/local/bin`. It fails closed (refused, and doctor reports fail). The fixture checks the bare form on bookworm and trixie (Debian 13). Ryan runs doctor on hawaii, PiVPN and PiCUPS | AD-010, §12 note 9 |

### 0.2 Architect flags (FRD wording that cannot be met literally)

| Flag | FRD text | Decision |
|---|---|---|
| **AQ-1** | AC-025: the power-path scan "passes unchanged" | The C-4 scan (`power_paths_test.go`) flags every `poweroff` / `power-off` / `halt` string literal outside `execReboot` / `execShutdown`, including package-level ones. FR-025 and FR-031 need doctor to ask sudo about `systemctl poweroff`, so the verb must appear in non-exempt code. It cannot be done honestly without editing the scan. Moving the literal into a package that is not scanned would hide it from the scan rather than satisfy the rule, so that was rejected. **Decision:** an *additive* amendment. The existing test functions and every existing case stay byte-identical. One new allowance covers a single data-only function, `shutdownSudoCommand`, with a structural rule and its own self-tests (AD-011). The security reviewer approves or rejects it |
| **AQ-2** | FR-031 / AC-023: the probe's command is "taken from the same source" as the shutdown path | `execShutdown` stays byte-identical (FR-032 / AC-025 take priority). Doctor's argv comes from `shutdownSudoCommand()`. An AST test parses `execShutdown`'s two Linux launch sites and fails CI unless both equal `sudo` + `shutdownSudoCommand()`. The guarantee FR-031 wants (doctor cannot pass where shutdown is refused because the argv differs) holds at build time. The literal wording "taken from" does not hold (AD-010) |
| **AQ-3** | NFR-002: "at most one extra prompt line per run" | The OQ-A3 transparency line is printed only on reinstall runs, and those ask no prompt. Every run therefore gains **at most one** line of output in total. Read as a line budget, the bound holds |
| **AQ-4** | FRD §9: fixing the post-install fail path is out of scope | AD-006 fixes it **only** for a newly installed poweroff grant, which closes OQ-A2. The general fail path is unchanged |
| **AQ-5** | (scope) | AD-007 adds a pre-mutation refusal when `/etc/riot/agent.yaml` exists but is not a regular file (or a symlink to one). It makes AC-011 (h) deterministic and closes a pre-existing root write-through (SEC-OBS-1, §10). It changes behaviour only for that corner state. Ryan or the security reviewer may drop it, and the rest of the ADD stands without it |

---

## 1. Summary

The installer gains one Linux-only question, "Allow remote shutdown from dashboard?" (default no), asked directly after
the reboot question. To make the answer available before the sudoers drop-in is built, the whole prompt block moves
out of the lock window, to before any privileged work. One variable, `SHUTDOWN_GRANT`, is decided once:
- the answer on a fresh install;
- a strict, fail-closed reading of the preserved config on a reinstall;
- false on a bootstrap re-enrolment.

That one variable drives both the single new sudoers line `riot ALL=(root) NOPASSWD: ${SYSTEMCTL_PATH} poweroff` and
the `allow_shutdown: true` line of a newly written config. A final agreement check and a narrowly scoped trap make sure
that no run, successful or failed, leaves a new grant that its config does not justify. `riot-agent doctor` gains a
listing-only `sudo -n -l systemctl poweroff` check, run as the `riot` user when doctor runs as root. The agent's
shutdown path, the server and the dashboard do not change.

## 2. Technical Context

Verified on `feat/shutdown-sudo-001` (base `71a4dea`, which contains PR #35).

- **Installer order today** (`scripts/install.sh`):
  - L154–252: root check, `PATH=/usr/sbin:/usr/bin:/sbin:/bin`, signed manifest download and verification (network).
  - L268–330 (Linux): tool and version checks; `dbus` install if missing (the only package operation); the
    `riot-maintenance.lock` `flock` (L331).
  - L373–416: the native-lock helper, which holds the four apt/dpkg locks, with a 15-minute self-termination bound.
  - L419–458: sudoers generated and validated (the reboot line is L436).
  - L490: `BACKUP`.
  - L501: drop-in installed. L502: effective policy check, with restore on failure.
  - L514: `systemctl stop riot-agent`. L519–521: `/etc/riot` becomes `root:root 0700`.
  - L536–625: `ask_yn`, Docker detection, and the prompts (only if `! -f /etc/riot/agent.yaml`). **The human
    answers while the agent is stopped and the package locks are held.**
  - L628–634: `COMMANDS_SECTION` (only when reboot or patching is true).
  - L656–662: bootstrap reset (`rm -f agent.yaml` and enrolment files).
  - L665–703: config write (only if `! -f`), `chown riot:riot` and `chmod 600`.
  - L720–745: binaries, units and hook installed; final `visudo`; start.
  - The `EXIT` trap (`cleanup`, L166) kills the lock helper and removes `$STAGE`.
- **Tools the installer can rely on, on every Linux path:** bash, `awk` (used unconditionally at L234), coreutils
  (`head`, `wc`, `tr`, `stat`, `timeout`, `install`, `mktemp`). **`python3` is required only on apt hosts** (L296).
  DNF hosts need not have it, so the config reader must not use Python. No YAML library is guaranteed anywhere.
- **Agent config** (`internal/agent/config.go`): `CommandsConfig.AllowShutdown bool \`yaml:"allow_shutdown"\``,
  loaded by `yaml.Unmarshal` (`gopkg.in/yaml.v3` v3.0.1, non-strict, so unknown keys are ignored and duplicate keys
  are an error). `Config.Save` re-serialises the whole struct with yaml.v3 (4-space indentation).
- **Shutdown path** (`internal/agent/commands.go` L435–494): `execShutdown` launches `a.runner(…, "sudo",
  "systemctl", "poweroff")` (seam) or `exec.Command("sudo", "systemctl", "poweroff")` (Linux), with a bare
  `systemctl`.
- **C-4 scan** (`internal/agent/power_paths_test.go`): `powerStringRE` flags any constant-folded string that matches
  `(?i)(^|[^a-z0-9])(power-?off|halt)([^a-z0-9]|$)` in any declaration outside `func execReboot` / `func execShutdown`
  of package `agent` (directory `.`), across `internal/agent/...` and `cmd/riot-agent`. **A doctor message or probe
  containing the text `poweroff` fails this test.** `shutdown` and `reboot` strings are not flagged. Launches of
  `systemctl reboot` and similar are caught by `checkCall` instead.
- **Doctor**:
  - `Permissions` prints `Shutdown: <bool>` (doctor.go L267).
  - `checkMaintenancePolicy` (maintenance_doctor.go) rewrites `sudo -n -l …` to `sudo -n -U riot -l …` when euid is 0,
    as an inline closure.
  - `reportUpdatePrivilege` is the testable pattern: an injected `collectors.CommandRunner`, `captureStdout` in tests,
    and a 15 s timeout.
  - `execRunner` is `exec.CommandContext(...).CombinedOutput()`, so the deadline kills the child.
- **Fixtures**:
  - `scripts/test_install_maintenance.py` runs the real installer as root in `debian:bookworm-slim` (CI step "Signed
    installer and legacy migration fixtures").
  - Its stubs: `curl` serves `/fixture/assets`; `systemctl` logs every call to `/fixture/trace`; `sudo` wraps
    `sudo.fixture-real`; `visudo` has failure flags.
  - `setUp` creates a *reinstall* state (a legacy `agent.yaml`, and a drop-in containing `/bin/sh -c *`).
  - Today every run is `--non-interactive` and there is no pty.
- **AGENT-PRIV:** only on the unmerged branch `AGENT-PRIV` (`internal/privileges`, `cmd/riot-apply-privileges`). Per
  the grounding note, its manifest has `CapReboot` → `systemctl reboot` and no shutdown capability. The architect
  session has no shell, so the branch was not re-read. Nothing in this story touches it.

## 3. Architecture Decisions

### AD-001: Move the prompt block out of the lock window (OQ-A1, FR-002, FR-023)

**Decision.** These lines move, unchanged in content, to directly after the asset-verification block (after L252,
before `secure_dir` is defined):
- the `ask_yn` definition (L531–565);
- Docker detection (L567–572);
- the prompt block with its `ALLOW_*` initialisations (L593–625).

The new shutdown prompt is inserted inside the block, directly after the reboot prompt, guarded by
`[ "$OS" = linux ]`. It sets `ALLOW_SHUTDOWN=true` only on a yes. The "Build optional config sections" block
(L574–591), `COMMANDS_SECTION` and everything after it stay where they are.

The fresh-install predicate is unchanged (`[ ! -f "$RIOT_CONFIG_DIR/agent.yaml" ]`). It is evaluated once at the new
position and stored as `CONFIG_EXISTED=true|false`, because nothing between the new and old positions creates or
removes `agent.yaml`.

**Rationale.**
- FR-002 requires the shutdown prompt to appear directly between the reboot and patching prompts, so the prompts
  cannot be split.
- The answer must exist before L420.
- The earliest point where every input is known, and nothing has been mutated, is right after verification.
- It removes the human wait from the window in which the agent is stopped and the package locks are held. A slow
  answer can no longer trip the lock helper's 15-minute SIGTERM in the middle of a replacement.
- Ctrl-C at a prompt now leaves the host untouched.

**Alternatives considered.**
- *Generate the drop-in after the prompts (move L420–505 down).* Rejected. The effective policy must be validated
  before the agent is stopped (L499 comment), and the window would gain the human wait.
- *Ask only the shutdown question early.* Rejected, because it violates FR-002 adjacency.
- *Place the block just before L420, inside the window.* Rejected, because the human wait stays inside the lock
  window.

**Consequences.**
- The prompts now appear before prerequisite failures such as an old sudo or a busy maintenance service. A user may
  answer and then see the run refuse, with no change made.
- The "Docker detected" line prints earlier.
- On non-Linux the output order is identical, because nothing printed between the old and new positions.
- Prompt texts, hints, defaults and relative order do not change.

### AD-002: One decision variable, `SHUTDOWN_GRANT`, drives drop-in and config (FR-008 to FR-012, FR-016, FR-020 to FR-022)

**Decision.** Directly after the prompt block, on Linux only:

| Case | `SHUTDOWN_GRANT` |
|---|---|
| `CONFIG_EXISTED=false` (fresh, including fresh with `--bootstrap-key`) | `$ALLOW_SHUTDOWN` (the prompt answer; `-y` → true; `--non-interactive` / no terminal → false) |
| `CONFIG_EXISTED=true` and `RIOT_BOOTSTRAP_KEY` non-empty | `false`: the config is deleted and rewritten with every remote feature off (DEC-007) |
| `CONFIG_EXISTED=true`, no bootstrap key | `true` only if `config_enables_shutdown "$RIOT_CONFIG_DIR/agent.yaml"` succeeds (AD-003) |

On non-Linux, `SHUTDOWN_GRANT=false` and nothing else runs.

`SHUTDOWN_GRANT` is the only input to both outputs:
- **Sudoers:** directly after the existing reboot echo (L436), and only when `"$SHUTDOWN_GRANT" = true`, the block
  echoes exactly `riot ALL=(root) NOPASSWD: ${SYSTEMCTL_PATH} poweroff`. No other line changes.
- **Config:** `COMMANDS_SECTION` is emitted when `ALLOW_REBOOT`, `SHUTDOWN_GRANT` or `ALLOW_PATCHING` is true. Its
  lines are `allow_reboot`, then `  allow_shutdown: true` **only when `SHUTDOWN_GRANT=true`**, then `allow_patching`.
  When `SHUTDOWN_GRANT=false`, the section is byte-identical to the pre-story section, so a "no" config equals the
  pre-story config.

**Rationale.** Agreement by construction: one variable, two outputs. Driving the config line from `SHUTDOWN_GRANT`
rather than the raw answer means that no path can write `allow_shutdown: true` without the grant (FR-022, second
half).

**Alternatives considered.**
- *Always write `allow_shutdown: false` when the answer is no.* Rejected, because it changes the "no" config's bytes
  for no gain (FR-009 allows absent).
- *Re-read the written config to decide the drop-in.* Rejected as the primary mechanism, because the drop-in is
  built before the config is written. Re-reading is used as a check instead (AD-005).

**Consequences.**
- Fresh "yes" with reboot and patching "no" writes a `commands:` section with `allow_reboot: false`,
  `allow_shutdown: true` and `allow_patching: false`.
- The drop-in grows by exactly one line on opted-in hosts.

### AD-003: Fail-closed config classifier in bash + POSIX awk (FR-017, OQ-A4)

**Decision.** Add a bash function `config_enables_shutdown PATH`, defined after `restore_policy` (L189) between the
marker comments `# BEGIN shutdown-config-classifier` and `# END shutdown-config-classifier`. It returns 0 (enabled)
or 1 (not enabled), prints nothing, and never exits the installer. Every internal failure returns 1. It is always
called in an `if` / `&&` context, so `set -e` does not apply inside it, and **every step must end in `|| return 1`
explicitly.**

Pre-read checks:
1. `[ -f "$p" ] && [ ! -L "$p" ]`, otherwise return 1. Symlinks are never read.
2. Snapshot: `timeout 5 head -c 65537 -- "$p" > "$STAGE/shutdown-config.snapshot"`. A non-zero status (timeout 124,
   read error) returns 1. The 5 s bound stops a FIFO swapped in after check 1 from hanging the installer.
3. Size ≤ 65536 bytes, otherwise return 1. A real config is about 2 KiB.
4. No NUL byte: the byte count of `LC_ALL=C tr -d '\000' < snapshot` must equal the snapshot size, otherwise return 1.
5. `LC_ALL=C awk '<program>' snapshot`. Return 0 if and only if awk exits 0 **and** its entire stdout is the single
   line `enabled`.

The awk program is line-based over bytes. Terms used below:
- a *blank* line matches `^ *$`;
- a *comment* line matches `^ *#`;
- every other line is a *content* line;
- *indent* is the count of leading spaces (`match($0, /^ +/)` → `RLENGTH`, or 0).

It rejects the whole file (prints nothing) on any of these:

| Rule | Rejects | Why |
|---|---|---|
| F1 | A line whose leading whitespace contains a TAB (`^ *\t`) | YAML forbids tabs in indentation, so yaml.v3 fails to load |
| F2 | A `---` line after the first content line; a `---` line carrying content (anything but `^--- *$` or `^--- +#`); any `...` line (`^\.\.\.( \|$)`); any directive line (`^%`) | yaml.v3 decodes only the first document. A later document's `commands` would be invisible to the agent |
| F3 | A column-0 content line that does not match `^[A-Za-z_][A-Za-z0-9_]*:( .*)?$` | Rejects sequences, `?` complex keys, quoted keys, `commands :`, anchors, `<<:` merges and flow mappings at the top level |
| F4 | A duplicate column-0 key | yaml.v3 errors on duplicate keys |
| F5 | Any content line whose *value* (after stripping indent and any leading `- ` items, then taking the text after the first `: ` or a trailing `:`; or the whole remainder for a sequence item) starts with `\|`, `>`, `&`, `*`, `!`, `%`, `@` or a backtick | Block scalars, anchors, aliases and tags can hide structure from a line reader. A real agent config has none (Save emits block scalars only for strings with newlines) |
| F6 | A content line whose value starts with `"` but the value contains fewer than 2 `"`; starts with `'` but contains fewer than 2 `'`; starts with `[` but `[` and `]` counts differ; or starts with `{` but `{` and `}` counts differ | A quoted or flow value that continues onto later lines could hide a column-0 `commands:` inside a scalar. Apostrophes **inside** plain values (`device_name: Ryan's box`) are not affected |
| C1 | More than one `commands` top-level key, or its line is not `^commands: *$` or `^commands: +#` | An inline value (`commands: {…}`, `commands: *x`) means the block is not a plain mapping |
| C2 | Inside the `commands` block (the lines after `commands:` up to the next column-0 content line; blank lines and comment lines at any column are skipped), any content line that does not match `^ +[a-z_]+: +[A-Za-z0-9_.-]+ *$` or `^ +[a-z_]+: +[A-Za-z0-9_.-]+ +#` | The block must be a flat map of plain scalars. Quotes, tags, nesting and flow are all rejected |
| C3 | Content lines in the block with differing indents | Nesting or bad indentation |
| C4 | A duplicate key inside the block | yaml.v3 error |

If no rule fires, awk prints `enabled` if and only if the block contains the key `allow_shutdown` exactly once and
its value is exactly `true`. Any other value (`false`, `True`, `yes`, `on`, `1`, `maybe`) prints nothing.

**Portability constraints on the awk program** (mawk 1.3.3 on Debian / Raspberry Pi OS / DietPi, gawk on Fedora,
busybox awk):
- No regex interval expressions (`{n}`).
- No gawk extensions: no `gensub`, `IGNORECASE`, `\s`, `\y` or 3-argument `match`.
- No `length(array)`.
- The program text contains **no single-quote character**. Count `'` with `sq = sprintf("%c", 39)` and `gsub` on a
  copy.
- Run with `LC_ALL=C`.

**Rationale.**
- awk is already a hard dependency; Python is not guaranteed on DNF hosts, and no YAML parser is guaranteed anywhere.
- A tiny, deliberately narrow grammar can be audited.
- Every rule rejects. None widens.
- It accepts both formats real hosts carry: the installer's own config (2-space indentation, quoted URL/keys) and an
  agent-`Save`d config (4-space indentation, `""` and `[]` values).

**Alternatives considered.**
- *grep for `allow_shutdown: true`.* Rejected, because it misreads commented, nested, duplicate and multi-document
  files (AC-011).
- *python3 with PyYAML.* Rejected: PyYAML is not installed on bookworm-slim, and Python is not required on DNF hosts.
- *Run the staged `riot-agent` binary to classify.* Rejected:
  - it means parsing riot-controlled input as root with a large binary;
  - unknown arguments start a second daemon (house rule);
  - it would need a new agent subcommand.

**Consequences.**
- Valid YAML that the agent would read as enabled, in an unusual form (`yes`, `True`, quoted, flow, anchors, CRLF,
  BOM before `commands`), is classified "not enabled". This is the FRD's accepted fail-closed direction: the "Config
  only" state, which doctor reports (FR-027).
- Residual R-1 (§10): a YAML error **outside** the rules above, for example a duplicate nested key under `server:`,
  stops the agent from loading the file at all while the classifier may say enabled. In that state no agent runs, so
  rIOt cannot use the grant. Doctor prints `Config load error`. The next fix to the file restores agreement.

### AD-004: Differential test against the real agent loader (FR-017 "never", OQ-A4)

**Decision.** `scripts/shutdown_grant_test.go` (package `scripts`) adds `TestInstallSh_SS_FR017_ClassifierNeverExceedsAgentLoader`:
1. Extract the text between the classifier markers from `install.sh`.
2. For each corpus file, run `bash -c '<function>; config_enables_shutdown "$1"' _ <file>` with
   `STAGE=<t.TempDir()>`. The test is skipped when no Linux bash or awk is available (same pattern as
   `TestMaintenanceReleaseChecksums`).
3. When `mawk` and/or `busybox` are on PATH, run once more with each, as `awk` through a PATH shim directory.
4. For each case, call `agent.LoadConfig(file)`. "Agent enabled" means a nil error and `Commands.AllowShutdown`.
5. Assert, for every case: **classifier enabled ⇒ agent enabled**.
6. Assert each case's pinned expected classifier result, so that regressions in either direction fail.

Corpus (each case is a named Go string):
- **Expected enabled:**
  - the fresh-install template with `allow_shutdown: true`;
  - `yaml.Marshal` of a `Config` with `AllowShutdown: true` (Save format);
  - the agent default template edited to `allow_shutdown: true    # Allow remote shutdown commands`;
  - 4-space indentation;
  - a leading `---`;
  - comment lines and blank lines inside the block, and a column-0 comment inside the block.
- **Expected not enabled:**
  - commands absent, key absent, `false`, commented out, under `docker:`, `"maybe"`;
  - `True`, `TRUE`, `yes`, `on`, `"true"`, `'true'`, `!!bool true`;
  - `commands: {allow_shutdown: true}`, `commands: &c`, a merge key in the block;
  - duplicate `commands`, duplicate `allow_shutdown`;
  - tab indentation, CRLF line endings, a BOM on the first line;
  - a second document holding `commands`, a `...` marker, a `%YAML` directive;
  - an embedded NUL, a size of 65 537 bytes;
  - an unterminated `"` value before `commands:`, a multi-line flow sequence before `commands:`;
  - nested `commands.sub.allow_shutdown`, mixed indentation in the block, `commands :`, `"commands":`;
  - `allow_shutdown:true` (no space), `allow_shutdown: true#x`, a `|` block scalar elsewhere in the file.

**Rationale.** FR-017's "must never treat a config as enabled when the agent would read false" is a claim about two
parsers, so it is tested against both. The real `agent.LoadConfig` is used, not a mirror struct, so the test follows
the loader if it ever changes.

**Consequences.** `scripts` tests import `internal/agent`. This is allowed inside the module, and the package is
test-only.

### AD-005: Final agreement check before anything is replaced (FR-022, TOCTOU)

**Decision.**
- Insert a check on Linux after the config-write block (after L703) and before the first binary `atomic_install`
  (L720).
- The check computes `FINAL` = `config_enables_shutdown "$RIOT_CONFIG_DIR/agent.yaml"`, as true or false.
- If `FINAL` ≠ `SHUTDOWN_GRANT`:
  1. set `GRANT_PENDING=false` (AD-006);
  2. `restore_policy`;
  3. `fail "agent.yaml commands.allow_shutdown changed during installation; previous managed policy restored; the
     agent is left stopped; re-run the installer"`.
- Otherwise set `GRANT_PENDING=false` and continue.

**Rationale.**
- The preserved config is read before the agent is stopped (AD-002), and the running agent, as `riot`, can rewrite
  it until L514.
- The check runs while the agent is stopped and `/etc/riot` is `root:root 0700` (L520), so no `riot` process can
  change the file between the check and the start.
- It turns FR-022 into a runtime guarantee for the reinstall path as well as the fresh path.

**Alternatives considered.**
- *Read the config after the stop and build the drop-in then.* Rejected, because the policy must be validated before
  the stop (AD-001).
- *No check.* Rejected: a compromised or racing agent could make the drop-in and the final config disagree.

**Consequences.**
- A mismatch leaves the host on its previous policy, with the old binaries and the agent stopped. That is the same
  shape as the existing late failures (for example L517).
- A normal agent `Save` (for example on enrolment) preserves the value, and the classifier accepts Save output, so
  ordinary runs never hit the check.

### AD-006: Roll back a newly installed grant when the run fails before agreement (OQ-A2)

**Decision.**
- Add `GRANT_PENDING=false` near `NATIVE_LOCK_PID`.
- Directly after the drop-in `atomic_install` (L501), set `GRANT_PENDING="$SHUTDOWN_GRANT"`.
- `cleanup()` gains a first step: if `GRANT_PENDING` is `true`, run
  `restore_policy || echo "WARNING: could not restore the previous rIOt sudo policy" >&2`, before the lock helper is
  killed. The `$STAGE` removal follows.
- The explicit restore paths (L503, L736 and AD-005) set `GRANT_PENDING=false` before they call `restore_policy`, so
  the policy is never restored twice.

**Rationale.**
- Before AD-005 passes, no binary, unit or hook has been replaced (L720 onwards comes later), so the pre-run policy is
  consistent with what is installed.
- The trap covers `fail`, `set -e` aborts, INT, TERM and the lock helper's 15-minute SIGTERM.
- It is scoped to runs that **introduced** a grant, so the general fail path stays as FRD §9 requires (AQ-4).

**Alternatives considered.**
- *Accept the residual.* Rejected: the fix is four lines and has no side effect on runs without a grant.
- *A general rollback of every partial run.* Rejected as out of scope. After L720 it would pair new binaries with an
  old policy.

**Consequences.** After a failed fresh "yes" run, the host has its previous drop-in (on a clean host, none), never a
grant without a config. What remains is SIGKILL or power loss in the window: residual R-2.

### AD-007: Refuse a non-regular `agent.yaml` before and after the config directory is locked (AQ-5, AC-011 h)

**Decision.** On Linux, refuse when the path exists in any form (`-e` or `-L`) but `[ -f "$p" ]` is false (a dangling
symlink, a symlink to a non-regular file, a directory, a FIFO, a socket or a device):
- **Early:** directly before `CONFIG_EXISTED` is computed (AD-001), with the message
  `fail "${RIOT_CONFIG_DIR}/agent.yaml exists but is not a regular file; move it aside and re-run; no files changed"`.
  Nothing has been mutated at this point.
- **Late:** directly after L521 (`chmod 0700 "$RIOT_CONFIG_DIR"`), with the same predicate and
  `fail "... changed during installation ..."`. The AD-006 trap restores a pending grant.

A symlink to a regular file still counts as a reinstall, as it does today. The classifier refuses to read it, so it
is not enabled.

**Rationale.**
- It makes the "not a regular file" forms of AC-011 (h) deterministic: no grant, no write.
- It closes SEC-OBS-1 (§10). The late check covers a path planted after the early check, while the agent was still
  running. After L520 the agent user can no longer create entries in `/etc/riot`.

**Consequences.** A host whose `agent.yaml` is a dangling symlink or a directory, which the installer mishandles today
anyway, now gets a clear refusal. Non-Linux is unchanged.

### AD-008: One transparency line on reinstall (OQ-A3)

**Decision.** When `CONFIG_EXISTED=true` on Linux, after `SHUTDOWN_GRANT` is decided, print exactly one of:
- `==> Remote shutdown: commands.allow_shutdown is true in /etc/riot/agent.yaml; granting sudo systemctl poweroff`
- `==> Remote shutdown: not enabled in /etc/riot/agent.yaml; no poweroff grant (only a plain "allow_shutdown: true" under "commands:" in a regular file enables it)`
- `==> Remote shutdown: re-enrolment resets remote features; no poweroff grant` (bootstrap key with an existing config)

Fresh installs print nothing extra: the prompt line, or nothing under `--non-interactive`, is the record.

**Rationale.**
- Ryan sees, at the moment of the root re-run, that a grant is being added and why. A config tampered with by a
  compromised agent becomes visible.
- The second line also tells Ryan why a `yes` / `True` spelling did not count.

**Consequences.** At most one extra line per run (AQ-3). The paths are the fixed `${RIOT_CONFIG_DIR}` values.
`SYSTEMCTL_PATH` is not yet known at this point, so the line says `systemctl`.

### AD-009: Doctor check, with an injected runner and the `riot` rewrite reused (FR-025 to FR-030, OQ-A5)

**Decision.** New file `internal/agent/shutdown_doctor.go`:
- `const shutdownProbeTimeout = 10 * time.Second`.
- `func shutdownSudoCommand() []string`: the data-only C-4 listing function (AD-011). Its body is exactly
  `return []string{"systemctl", "poweroff"}`.
- `func shutdownProbeArgs() []string`: its body is exactly
  `return append([]string{"-n", "-l"}, shutdownSudoCommand()...)`.
- `func reportShutdownPrivilege(goos string, allow bool, run collectors.CommandRunner, timeout time.Duration)`:
  - If `goos != "linux" || !allow`, return, printing nothing (FR-029).
  - Otherwise print an empty line and `section("Remote Shutdown")`.
  - Create a context with `timeout` and call `run(ctx, "sudo", shutdownProbeArgs()...)` once.
  - Let `cmd := strings.Join(shutdownSudoCommand(), " ")`.
  - On a nil error: `pass("shutdown sudo rule present: riot may run %s without a password", cmd)`.
  - On an error, print `fail("shutdown sudo rule missing or unverifiable (%s): %s; remote shutdown will report failed", cmd, why)`.
    `why` is:
    - `"timed out after 10s"` when `ctx.Err()` is `DeadlineExceeded`;
    - `"sudo not found"` when `errors.Is(err, exec.ErrNotFound)`;
    - otherwise `"not allowed without a password"`.
  - After the fail line, print
    `warn("  Set commands.allow_shutdown: true in agent.yaml (it is set here) and re-run the install script as root (curl … | sudo bash); agent-only updates do not change /etc/sudoers.d/riot-agent")`.

New helper in `internal/agent/maintenance_doctor.go`:
- `func agentPolicyRunner(euid int, inner collectors.CommandRunner) collectors.CommandRunner`.
- It is the existing closure, moved without change: when `euid == 0`, `name == "sudo"`, `args[0] == "-n"` and
  `args[1] == "-l"`, it rewrites the arguments to `-n -U riot -l …`. Otherwise it passes through.
- `checkMaintenancePolicy` uses `agentPolicyRunner(os.Geteuid(), execRunner)`, and its behaviour is identical.

Wiring in `doctor.go`, directly after the `Permissions` block (after L271) and before Hold Enforcement:
`reportShutdownPrivilege(runtime.GOOS, cfg.Commands.AllowShutdown, agentPolicyRunner(os.Geteuid(), execRunner), shutdownProbeTimeout)`.

**Constraints.**
- No string literal in `shutdown_doctor.go` or `doctor.go` may contain `poweroff`, `power-off` or `halt`. The verb
  reaches output only through `shutdownSudoCommand()`.
- Nothing is written to disk.
- There is exactly one sudo invocation per doctor run. A sudo listing does not fork, so the `CommandContext` kill at
  the deadline ends it.

**Rationale.**
- This follows the `reportUpdatePrivilege` pattern (an injected runner and stdout capture).
- The listing never executes the command.
- `-U riot` is what makes a root doctor run truthful (FR-026). Extracting it means the maintenance check and the
  shutdown check cannot drift apart.
- 10 s is about 100 times a local sudoers listing, leaves room for slow NSS/LDAP, and keeps doctor's total runtime
  bounded.

**Alternatives considered.**
- *Parse `sudo -l` output for `NOPASSWD`.* Rejected:
  - `riot` is a system user with no usable password;
  - the drop-in is NOPASSWD-only;
  - parsing adds locale and format fragility.
  This leaves residual R-4.
- *Reuse 15 s.* Rejected as needless. Either value meets FR-028.

### AD-010: The probe's argv is pinned to `execShutdown` by an AST test (FR-031, AQ-2, A-003)

**Decision.** `TestShutdownDoctor_SS_AC023_ProbeMatchesExecShutdownArgv`:
1. Parse `commands.go` and find `func (a *Agent) execShutdown`.
2. Collect every call inside it whose function is `a.runner` or `exec.Command`, whose string arguments all fold to
   literals, and whose first string argument is `"sudo"`.
3. Require exactly 2 such calls (the seam and the Linux branch).
4. Require, for each, that the arguments after `"sudo"` equal `shutdownSudoCommand()`.
5. Require that `shutdownProbeArgs()` equals `["-n", "-l"]` + `shutdownSudoCommand()`.

The bare `systemctl` is kept on purpose. sudo resolves the probe's command name exactly as it resolves the agent's
(through `secure_path`, or the caller's PATH), so the listing answers for the command the agent really runs. An
absolute path would be wrong (A-003).

**Rationale.** Any edit to `execShutdown`'s argv, or to the doctor's, fails CI. That is the build-time form of "cannot
drift".

**Alternatives considered.** Refactor `execShutdown` to call `shutdownSudoCommand()`. Rejected for this story: FR-032
and AC-025 keep the most heavily reviewed function byte-identical. A later story may adopt it once the security
reviewer is satisfied.

### AD-011: C-4 amendment: one data-only listing allowance (AQ-1, FR-030)

**Decision.** The amendment to `internal/agent/power_paths_test.go` is additive. No existing line inside an existing
test function changes.
1. A new map, `powerListingFuncs = map[string]bool{"shutdownSudoCommand": true}`.
2. A new helper, `listingFuncShapeProblem(fn *ast.FuncDecl) string`. It returns "" only when **all** of these hold:
   - there is no receiver and there are no parameters;
   - the single result type is `[]string`;
   - the body is exactly one `return` of one `[]string{…}` composite literal;
   - the literal has exactly two elements, both string literals, and the first is `"systemctl"`.
3. In `problems()`, the top-level `FuncDecl` loop gains one branch. It applies when the declaration is a `FuncDecl`
   whose name is in `powerListingFuncs`, with `info.dir == "."` and `info.file.Name.Name == "agent"`:
   - flag `"listing function %s must only return a two-element systemctl literal"` if the shape check fails;
   - then `continue`, which skips the string rule for that declaration only.
   The shape rule guarantees the function contains no calls, so `checkCall` has nothing to see.
4. A new test function, `TestPowerPaths_SS_C4_ListingAllowanceSelfTest`:
   - **flags:**
     - the function with an `exec.Command` inside;
     - the function with an extra statement;
     - the function returning a variable;
     - a *method* with the same name;
     - the same function in `collectors/x.go`, where the string rule applies;
     - a differently named function returning the same literal;
   - **allows:** the exact shape in `shutdown_doctor.go`.
5. The reference rule lives in `shutdown_doctor_test.go`:
   - `TestShutdownDoctor_SS_C4_ListingCommandOnlyFeedsTheProbe` uses AST over every non-test file in
     `internal/agent/...` and `cmd/riot-agent`;
   - `shutdownSudoCommand` may be referenced exactly twice: once in `shutdownProbeArgs` and once in
     `reportShutdownPrivilege` (the message join);
   - `shutdownProbeArgs` may be referenced only in `reportShutdownPrivilege`;
   - in `reportShutdownPrivilege`, the probe may only be passed as `run(ctx, "sudo", shutdownProbeArgs()...)`.

**Rationale.**
- The only non-test path to the verb is a data function whose sole consumers are a `sudo -n -l` listing and a printed
  message.
- A future `exec.Command("sudo", shutdownSudoCommand()...)` elsewhere fails the reference rule, which closes the hole
  that a non-literal source would otherwise open in C-4's constant folder.
- `TestPowerPaths_SD_C4_OnlyExecRebootAndExecShutdownLaunchPowerActions` and `TestPowerPaths_SD_C4_SelfTest` stay
  byte-identical and keep passing.

**Alternatives considered.**
- *Put the literal in a package the scan does not cover (for example `internal/models`).* Rejected as evasion of C-4.
- *Obfuscate the string* (`"power"+…` through a non-constant). Rejected for the same reason.
- *A second method named `execShutdown`.* Rejected: it games the name exemption and breaks the call-site count.

### AD-012: AGENT-PRIV forward mapping, recorded only (NFR-006)

**Decision.** When AGENT-PRIV is rebased onto `main`:
- Its manifest gains `CapShutdown`. It is derived from `commands.allow_shutdown` as read by the **agent's** loader,
  and renders exactly `riot ALL=(root) NOPASSWD: <ResolveTool("systemctl")> poweroff`, with no other verb, no
  arguments and no wildcard.
- `ResolveTool` must yield the same path sudo resolves for the agent's bare `systemctl` (A-003).
- The fixed `if [ "$SHUTDOWN_GRANT" = true ]` echo in `install.sh` is then removed together with the rest of the
  hardcoded block. The prompt and the config line (AD-001, AD-002) stay.
- `config_enables_shutdown` and AD-005 / AD-006 are retired with the hardcoded block, because the agent-side
  derivation agrees with the agent by construction. It will then also grant for `yes` / `True`, which is acceptable
  because it is the agent's own reading.
- AGENT-PRIV's manifest package must be reconciled with C-4. The literal `poweroff` would need the same treatment as
  AD-011, or an amendment of its own.

Nothing on branch `AGENT-PRIV` is modified (SC-005).

---

## 4. Component Changes

| Action | File Path | Purpose |
|---|---|---|
| MODIFY | `scripts/install.sh` | Each change, by line in the current file:<br>• L165: `GRANT_PENDING=false`.<br>• `cleanup()`: pending-grant restore first (AD-006).<br>• After L189: `config_enables_shutdown` between the markers (AD-003).<br>• After L252: the early `agent.yaml` refusal (AD-007); `CONFIG_EXISTED`; the moved `ask_yn`, Docker detection and prompt block, with the new Linux-only shutdown prompt after the reboot prompt (AD-001); the `SHUTDOWN_GRANT` decision and the transparency line (AD-002, AD-008).<br>• L436+: the conditional poweroff echo.<br>• After L501: `GRANT_PENDING="$SHUTDOWN_GRANT"`.<br>• L503: clear `GRANT_PENDING`.<br>• After L521: the late refusal (AD-007).<br>• L628–634: `COMMANDS_SECTION` with the conditional `allow_shutdown: true`.<br>• After L703: the agreement check (AD-005).<br>• L736: clear `GRANT_PENDING`.<br>Unchanged: the header usage comment (L1–17), the flags, every other prompt's text, hint and default, and every other sudoers line |
| MODIFY | `scripts/test_install_maintenance.py` | The stubs `curl` (append `DOWNLOAD <name>` to the trace) and `systemctl` (`/fixture/fail-stop` makes `stop` exit 1; the new flag is added to `setUp`'s cleanup list). `tearDown` calls `assert_no_power_action()`. New helpers: `fresh()` (remove `agent.yaml`), `install_tty(answers, *args)` (pty harness, §12 note 6), `run_installer(*args, stdin=None)`, `dropin()`, `grant_lines()`, `riot_lists(argv)`, `assert_agreement(expected)`. The constants `PRE_STORY_DROPIN` and `PRE_STORY_DARWIN_CONFIG` (§12 note 7). Every `test_SS_*` method in §8 |
| CREATE | `scripts/shutdown_grant_test.go` | The Go static tests and the differential test (§8). Package `scripts`; imports `internal/agent` for `LoadConfig` and `Config` |
| CREATE | `internal/agent/shutdown_doctor.go` | `shutdownProbeTimeout`, `shutdownSudoCommand`, `shutdownProbeArgs`, `reportShutdownPrivilege` (AD-009) |
| CREATE | `internal/agent/shutdown_doctor_test.go` | Every `TestShutdownDoctor_SS_*` and `TestAgentPolicyRunner_SS_*` test (§8) |
| MODIFY | `internal/agent/doctor.go` | One call after the Permissions block (AD-009). The `Shutdown: <bool>` line is unchanged |
| MODIFY | `internal/agent/maintenance_doctor.go` | Extract `agentPolicyRunner`; `checkMaintenancePolicy` uses it. Behaviour is identical |
| MODIFY | `internal/agent/power_paths_test.go` | Additive C-4 listing allowance and its self-test function (AD-011) |
| MODIFY | `.github/workflows/ci.yml` | Step "Signed installer and legacy migration fixtures": run the same `docker run … python3 /src/scripts/test_install_maintenance.py -v` a second time with `debian:trixie-slim` (A-003 for Debian 13) |
| MODIFY | `README.md` | Technical writer, FR-036 to FR-038:<br>• a new "Installer questions" subsection after "Optional flags" (L239–256);<br>• row L603;<br>• the Sudoers Rules table (L544–552): add the opt-in `systemctl poweroff` row, which supersedes SHUTDOWN-DPKG-001 INFO-4;<br>• rewrite "Shutdown on Linux and the sudo rule" (L2767–2777), L2653 and Known limits L2825;<br>• remove every "until SHUTDOWN-SUDO-001" and "grants no poweroff rule" statement (L60 must be checked too);<br>• the doctor check, next to L377–387 |
| MODIFY | `CHANGELOG.md` | `[Unreleased]`: an entry (FR-039) and fleet actions (FR-037). Earlier released entries (L33, L46) are history and are not edited |

**Explicitly unchanged** (AC-025, AC-026):
- `internal/agent/commands.go`, `reboot_clearance.go`, `poweroff.go`, `powerguard.go`, `config.go`;
- `internal/agent/reboot_callsites_test.go`, `shutdown_clearance_test.go`, `shutdown_qa_test.go`;
- `internal/server/**`, `web/**`, `cmd/**`, migrations, `go.mod` / `go.sum`;
- `scripts/riot-maintenance.service`;
- the `AGENT-PRIV` branch.

## 5. Data Model Changes

- **Postgres, telemetry, events, API:** none.
- **Agent config:** no new key. `commands.allow_shutdown` already exists.

  | Installer-written `commands` section | Before | After |
  |---|---|---|
  | reboot=n, shutdown=n, patch=n | absent | absent (identical) |
  | reboot=y, shutdown=n, patch=n | `allow_reboot: true` / `allow_patching: false` | identical |
  | reboot=n, shutdown=y, patch=n | absent | `allow_reboot: false` / `allow_shutdown: true` / `allow_patching: false` |
  | reboot=y, shutdown=y, patch=y | `allow_reboot: true` / `allow_patching: true` | `allow_reboot: true` / `allow_shutdown: true` / `allow_patching: true` |

- **Sudoers drop-in:** opted-in hosts gain one line, directly after `riot ALL=(root) NOPASSWD: ${SYSTEMCTL_PATH} reboot`:
  `riot ALL=(root) NOPASSWD: ${SYSTEMCTL_PATH} poweroff`.
- **Installer transient state:** these shell variables, plus `$STAGE/shutdown-config.snapshot`, which is overwritten
  per call and removed with `$STAGE`:

  | Variable | Values |
  |---|---|
  | `CONFIG_EXISTED` | `true` / `false` |
  | `ALLOW_SHUTDOWN` | `true` / `false` |
  | `SHUTDOWN_GRANT` | `true` / `false` |
  | `GRANT_PENDING` | `true` / `false` |

- **No new persistent state anywhere** (NFR-002). The backup directory's content is unchanged.

## 6. API / Interface Contract

No HTTP API change.

### 6.1 Installer CLI (unchanged)

The flags, their meanings and the usage text are unchanged (FR-035).

### 6.2 New prompt (Linux, fresh install only)

| Mode | Output | Answer |
|---|---|---|
| Interactive (`/dev/tty` readable) | `    Allow remote shutdown from dashboard? [y/N]: ` | `y` / `yes` (any case) → yes; anything else, empty, or an unreadable tty → no |
| `-y` / `--yes` | `    Allow remote shutdown from dashboard? [y/N]: y (auto)` | yes |
| `--non-interactive` | nothing | no |
| No terminal (`! -t 0` and no `/dev/tty`) | nothing | no |

Position: directly after `Allow remote reboot from dashboard? [y/N]` and directly before
`Allow remote OS patching from dashboard? [y/N]`.

### 6.3 New installer output lines (Linux)

- Reinstall: exactly one AD-008 line.
- Failures:
  - the AD-005 agreement message;
  - the AD-007 messages;
  - the trap warning `WARNING: could not restore the previous rIOt sudo policy` (stderr, only when a restore fails).

### 6.4 Doctor output (Linux, `allow_shutdown: true` only)

```
── Remote Shutdown ──
  ✓ shutdown sudo rule present: riot may run systemctl poweroff without a password
```
or
```
── Remote Shutdown ──
  ✗ shutdown sudo rule missing or unverifiable (systemctl poweroff): <not allowed without a password | sudo not found | timed out after 10s>; remote shutdown will report failed
  !   Set commands.allow_shutdown: true in agent.yaml (it is set here) and re-run the install script as root (curl … | sudo bash); agent-only updates do not change /etc/sudoers.d/riot-agent
```

The sudo invocation, never anything else:

| Doctor runs as | Invocation |
|---|---|
| Non-root | `sudo -n -l systemctl poweroff` |
| Root | `sudo -n -U riot -l systemctl poweroff` |

## 7. Sequence / Flow

### 7.1 Fresh install, interactive, shutdown yes (AC-001)

1. Verify the signed assets.
2. The `agent.yaml` path is absent, so `CONFIG_EXISTED=false`. The prompts run: reboot n, shutdown y, patch n,
   terminal n.
3. `SHUTDOWN_GRANT=true`.
4. Prerequisites, `dbus`, `flock`, then the native locks are taken.
5. The sudoers block emits the reboot line and then the poweroff line. `visudo` validates the candidate.
6. `BACKUP`, then the drop-in is installed. `GRANT_PENDING=true`. The effective policy check passes.
7. Stop, `/etc/riot` becomes `root:root 0700`, and the late refusal check passes.
8. The config is written with `allow_shutdown: true`.
9. The agreement check: `FINAL=true` equals `SHUTDOWN_GRANT`, so `GRANT_PENDING=false`.
10. Binaries, units, the final `visudo` and the start.

### 7.2 Reinstall, `allow_shutdown: true`, `-y` (AC-010)

1. The path is a regular file, so `CONFIG_EXISTED=true`. No prompt is asked.
2. The classifier returns enabled, so `SHUTDOWN_GRANT=true`, and the installer prints
   `==> Remote shutdown: commands.allow_shutdown is true …; granting …`.
3. The flow continues as 7.1 steps 4 to 7.
4. The config is preserved (L702 message).
5. The agreement check re-reads the stopped agent's config: still enabled, so the run passes.
6. The flow continues as 7.1 step 10.

### 7.3 Reinstall with `--bootstrap-key` and an existing config (AC-015)

1. `CONFIG_EXISTED=true` and a key is set, so `SHUTDOWN_GRANT=false`, and the installer prints
   `==> Remote shutdown: re-enrolment resets remote features; …`.
2. The drop-in has no poweroff line.
3. The bootstrap reset deletes the config, and a new config is written with every remote feature off.
4. The agreement check: false equals false.

### 7.4 Fresh "yes" run that fails after the drop-in is installed (OQ-A2)

1. 7.1 steps 1 to 6 run.
2. `systemctl stop` fails, so `set -e` exits.
3. The `EXIT` trap sees `GRANT_PENDING=true`, so `restore_policy` puts back the pre-run drop-in (or removes it). The
   lock helper is killed and `$STAGE` is removed.
4. Result: the pre-run policy, no grant, and no new binaries.

### 7.5 Agent rewrites the config between the read and the stop (TOCTOU)

1. `SHUTDOWN_GRANT=true` from the read.
2. Before L514, the agent sets `allow_shutdown: false`.
3. The agreement check finds `FINAL=false`.
4. `GRANT_PENDING=false`, `restore_policy`, then `fail`. The agent is left stopped, and Ryan re-runs.

### 7.6 Doctor as root (AC-021)

1. `cfg.Commands.AllowShutdown` is true and the OS is Linux.
2. `reportShutdownPrivilege` builds `sudo -n -l systemctl poweroff`.
3. `agentPolicyRunner` (euid 0) rewrites it to `sudo -n -U riot -l systemctl poweroff`.
4. The result is a pass or a fail, within 10 s.

## 8. Acceptance Criteria Mapping

Python fixture tests are methods of `InstallerFixtures` in `scripts/test_install_maintenance.py`. They run only in a
disposable root container, under the existing `setUpClass` guard. Go tests are in the files named.

Every successful `test_SS_*` run ends with `self.assert_agreement(expected)`. That helper checks:
- the drop-in has the poweroff grant exactly `expected ? 1 : 0` times;
- an independent strict Python check of the final config agrees.

The Python check matches the known shapes the test itself wrote or the installer writes: a column-0 `commands:` line,
then a same-block line `^\s+allow_shutdown: true\s*$`, with no other `commands:` key.

| AC ID | Fulfilled By | Test Strategy |
|---|---|---|
| AC-001 | AD-001 prompt, AD-002 decision, sudoers echo, `COMMANDS_SECTION` | Fixture `test_SS_AC001_fresh_yes_grants_poweroff_and_config_agrees`: pty with answers reboot n / shutdown y / patch n / terminal n; config has `allow_shutdown: true`; exactly 1 grant line; `sudo.fixture-real -n -U riot -l /usr/bin/systemctl poweroff` exits 0 |
| AC-002 | `ask_yn` (unchanged), AD-002 | Fixture `test_SS_AC002_fresh_no_or_unrecognised_answer_grants_nothing`: subtests for the answers `""`, `n`, `no` and `maybe` (pty), and an unreadable tty (no pty, `start_new_session=True`, `stdin=DEVNULL`). Each run has no `allow_shutdown: true` and no line matching `poweroff` in the drop-in |
| AC-003 | AD-001 block order | Fixture `test_SS_AC003_prompt_text_and_order`: pty with a `/usr/bin/docker` stub (removed in `finally`). The recorded prompt sequence equals exactly `[reboot, shutdown, patching, terminal, docker exec]`, with texts and `[y/N]` hints as listed. Go static `TestInstallSh_SS_AC003_PromptOrder`: file order reboot < shutdown < patching; shutdown `ask_yn` inside `[ "$OS" = linux ]` |
| AC-004 | AD-002 | Fixture `test_SS_AC004_reboot_and_shutdown_independent`: 4 subtests (y,y), (y,n), (n,y), (n,n). Each checks `allow_reboot` / `allow_shutdown` against its answers; the reboot grant is always present; the poweroff grant only for shutdown y; for shutdown n the config bytes equal the pre-story template rendered with the same answers |
| AC-005 | `ask_yn` yes-all | Fixture `test_SS_AC005_yes_flag_answers_yes`: fresh `-y`; stdout contains `    Allow remote shutdown from dashboard? [y/N]: y (auto)`; config and grant present |
| AC-006 | `ask_yn` defaults and no-tty branch | Fixture `test_SS_AC006_non_interactive_and_no_terminal_answer_no`: subtests for `--non-interactive`, and for no terminal (`/dev/tty` renamed aside and restored in `finally`, `start_new_session=True`, `stdin=DEVNULL`). Neither run has `allow_shutdown: true` or a grant |
| AC-007 | AD-002 (only a conditional line is added) | Fixture `test_SS_AC007_existing_lines_byte_identical`: the "no" drop-in == `PRE_STORY_DROPIN`; the "yes" drop-in with the grant line removed == the "no" drop-in. Go static `TestInstallSh_SS_AC007_RebootLineUnchangedAndPoweroffLineExact`: the reboot echo occurs exactly once, byte-identical; the poweroff echo occurs exactly once, is the line after it, and is inside `if [ "$SHUTDOWN_GRANT" = true ]` |
| AC-008 | The exact sudoers line | Fixture `test_SS_AC008_only_exact_poweroff_is_granted`, listing only via `runuser -u riot -- /usr/bin/sudo.fixture-real -n -l …`. Allowed: `/usr/bin/systemctl poweroff`. Refused: `poweroff --force`, `poweroff -i`, `halt`, `kexec`, `suspend` (each as `/usr/bin/systemctl <x>`), and `/usr/sbin/shutdown`, `/usr/sbin/poweroff`, `/usr/sbin/halt`. The drop-in has exactly one line containing `poweroff`; it equals the exact grant, starts `riot `, and has no `*`, `^` or `$`; no line contains `halt`, `kexec`, `suspend` or `/shutdown`. Go static `TestInstallSh_SS_AC008_NoOtherPowerGrant` |
| AC-009 | Existing L458 / L502 validation, AD-006 | Fixture `test_SS_AC009_policy_validation_still_guards_yes_install`: subtests for fresh `-y` with `/fixture/fail-policy` (stderr `Candidate sudo policy is invalid`; the drop-in equals the pre-run bytes) and with `/fixture/fail-merged-policy` (`previous managed policy restored`; pre-run bytes) |
| AC-010 | AD-002 classifier, AD-001 (`CONFIG_EXISTED`) | Fixture `test_SS_AC010_reinstall_true_emits_grant_without_question`: subtests for a run with no URL (pty, so an unexpected prompt would be seen) and for `-y`. No `Allow remote` text in the output; exactly 1 grant; the AD-008 "granting" line printed |
| AC-011 | AD-003, AD-007 | Fixture `test_SS_AC011_reinstall_fails_closed`, with subtests:<br>(a) no `commands`;<br>(b) no key;<br>(c) `false`;<br>(d) commented out;<br>(e) under `docker:`;<br>(f) `"maybe"`;<br>(g1) a tab-indented key line;<br>(g2) an unterminated `"` before `commands:`;<br>(g3) a duplicate `commands:`;<br>(g4) a NUL byte;<br>(h1) `agent.yaml` is a symlink to a regular file holding `allow_shutdown: true`: run succeeds, no grant;<br>(h2) `agent.yaml` is a dangling symlink: run refused before mutation;<br>(h3) `agent.yaml` is a directory: run refused before mutation.<br>No final drop-in has a `poweroff` line. For h2 and h3 the drop-in is byte-identical to the pre-run drop-in and the trace has no `systemctl stop`. Go `TestInstallSh_SS_FR017_ClassifierNeverExceedsAgentLoader` (AD-004) |
| AC-012 | AD-002 | Fixture `test_SS_AC012_flags_do_not_override_config`: subtests for `false` with `-y` (no grant) and `true` with `--non-interactive` (grant) |
| AC-013 | Full drop-in rewrite plus AD-002 | Fixture `test_SS_AC013_reinstall_revokes_stale_grant`: a pre-run drop-in with the grant, and config `false`. After the run there is no grant, and the drop-in == `PRE_STORY_DROPIN` |
| AC-014 | AD-002 (read-only snapshot), no write path | Fixture `test_SS_AC014_preserved_config_untouched`: `allow_shutdown: true`, 4-space indentation, comments, owner `riot:riot`, mode 0640. Bytes, uid, gid and mode are equal after the run |
| AC-015 | AD-002 bootstrap row, AD-005 | Fixture `test_SS_AC015_bootstrap_reenrolment_resets_shutdown`: subtests for interactive (pty; the run must ask no prompt) and `-y`. The rewritten config has no `allow_shutdown: true`; no grant; the AD-008 re-enrolment line printed |
| AC-016 | AD-002 fresh row | Fixture `test_SS_AC016_bootstrap_on_fresh_install_asks_and_grants`: fresh, `--bootstrap-key K`, pty shutdown y. The prompt was seen; config and grant present |
| AC-017 | AD-002, AD-005 | `assert_agreement()` at the end of every successful `test_SS_*` run (listed above), plus AD-005 at run time |
| AC-018 | AD-001 Linux guard, AD-002 non-Linux row | Go static `TestInstallSh_SS_AC018_ShutdownIsLinuxOnly`: the shutdown `ask_yn`, every `SHUTDOWN_GRANT=true` assignment and the classifier call are inside Linux-only branches. Fixture `test_SS_AC018_stubbed_darwin_install_unchanged`: a PATH-prepended `uname` stub reports `Darwin`/`arm64`, with a `darwin-arm64` asset, fresh, `-y`. The output has no shutdown prompt; the config == `PRE_STORY_DARWIN_CONFIG`; no `allow_shutdown` |
| AC-019 | `reportShutdownPrivilege` | `TestShutdownDoctor_SS_AC019_PassNamesTheGrant`: the runner returns nil; the output has exactly one `✓` line containing `systemctl poweroff`, and no `✗` |
| AC-020 | AD-009 error mapping, `shutdownProbeTimeout` | `TestShutdownDoctor_SS_AC020_FailWithGuidance`: subtests for refused (exit 1), password required (output `a password is required`, exit 1), `exec.ErrNotFound`, and a timeout (the runner blocks on `ctx.Done()`; called with a 50 ms timeout; the test asserts it returns in < 2 s). Each prints one `✗` line plus `re-run the install script as root`. `TestShutdownDoctor_SS_AC020_TimeoutBounded`: `shutdownProbeTimeout == 10*time.Second` and `≤ 15*time.Second`; the runner's `ctx.Deadline()` is within `timeout` of the call. No password prompt is possible: `-n` is asserted in every recorded argv (AC-024) |
| AC-021 | `agentPolicyRunner` | `TestShutdownDoctor_SS_AC021_RootAsksAboutRiot`: `agentPolicyRunner(0, rec)` gives `sudo -n -U riot -l systemctl poweroff`; `agentPolicyRunner(1000, rec)` gives `sudo -n -l systemctl poweroff`. `TestAgentPolicyRunner_SS_RewriteMatchesMaintenance`: non-`sudo` names and non-`-n -l` argv pass through unchanged (the maintenance behaviour is preserved) |
| AC-022 | The AD-009 gate | `TestShutdownDoctor_SS_AC022_NoCheckWhenOffOrNotLinux`: subtests for (`linux`, false), (`darwin`, true) and (`windows`, true). The recorder gets 0 calls; the output is empty. `Shutdown: <bool>` is unchanged (`doctor.go` L267 untouched, by diff review) |
| AC-023 | AD-010 | `TestShutdownDoctor_SS_AC023_ProbeMatchesExecShutdownArgv` |
| AC-024 | AD-009 / AD-011 | `TestShutdownDoctor_SS_AC024_OnlyListingInvocations`: a shared recorder across the AC-019 to AC-022 cases; every call is `sudo` with `-n` first and `-l` at index 1 (or `-U riot -l` at 1 to 3); no argv lacks `-l`. `TestShutdownDoctor_SS_C4_ListingCommandOnlyFeedsTheProbe`. `TestPowerPaths_SS_C4_ListingAllowanceSelfTest`. The fixture `tearDown` `assert_no_power_action()` checks `/fixture/trace` has no `systemctl (poweroff\|reboot\|halt\|kexec)` line |
| AC-025 | §4 "unchanged" list | Existing `TestShutdownCallSites_SD_*`, `TestPowerCallSites_SD_*`, `TestPowerPaths_SD_C4_*` and `shutdown_clearance` / `shutdown_qa` tests pass. DoD diff check: `git diff --exit-code main -- internal/agent/commands.go internal/agent/reboot_callsites_test.go internal/agent/shutdown_clearance_test.go internal/agent/shutdown_qa_test.go` |
| AC-026 | §4 | `TestInstallSh_SS_AC026_FlagsAndUsageUnchanged`: the header comment block L1–17 and the `case` flag arms equal pinned text; the reboot prompt line and reboot echo are unchanged. DoD: `git diff --stat main -- internal/server web cmd` is empty |
| AC-027 | README (technical writer) | `TestDocs_SS_AC027_NoStaleShutdownStatements` (in `scripts/shutdown_grant_test.go`, reads `../README.md`): no `until SHUTDOWN-SUDO-001`, no `grants the \`riot\` user no sudo rule for \`systemctl poweroff\``, and it contains `Allow remote shutdown from dashboard?`. Plus the reviewer checklist in §13 |
| AC-028 | README, CHANGELOG | `TestDocs_SS_AC028_FleetActionNamed`: README and the CHANGELOG `[Unreleased]` section each contain `hawaii`, `vermont`, `PiVPN`, `PiCUPS` and `allow_shutdown: true`; the CHANGELOG contains `riot ALL=(root) NOPASSWD:` with `poweroff` |
| AC-029 | AD-002, §5 | Fixture `test_SS_AC029_bounded_growth`: fresh "yes" against fresh "no" in clean states, with path sets under `/etc /var /usr/local /run` (excluding `/run/lock` and the backup directory's random name). The sets are equal; the backup file lists are equal; the drop-in line counts differ by exactly 1; the `DOWNLOAD` trace equals `[checksums.txt, checksums.txt.sig, riot-agent-linux-amd64, riot-agent-apply-linux-amd64, riot-maintenance-linux-amd64, riot-maintenance.service]` for both runs. Doctor writes nothing: AD-009 has no file I/O (reviewed; `shutdown_doctor.go` imports no `os` write API) |
| OQ-A2 | AD-006 | Fixture `test_SS_OQA2_failure_before_agreement_restores_previous_policy`: fresh `-y` with `/fixture/fail-stop`. The run fails; the drop-in == the pre-run bytes; no `poweroff` line |
| A-003 | AD-010 | Fixture `test_SS_A003_bare_systemctl_resolves_to_granted_path` (yes install): `runuser -u riot -- sudo.fixture-real -n -l systemctl poweroff` exits 0 and prints `/usr/bin/systemctl poweroff`. It runs on bookworm and trixie (§4 `ci.yml`) |
| OQ-A1 | AD-001 | Go static `TestInstallSh_SS_OQA1_PromptsPrecedeLocksAndStop`: the first `ask_yn "` call's offset is less than the offsets of `flock -n 9`, `NATIVE_LOCK_PID=$!`, `atomic_install "$STAGE/riot-agent.sudoers"` and `systemctl stop riot-agent` |
| AD-003 portability | AD-003 | Go static `TestInstallSh_SS_ClassifierIsPortableAwk`: the markers occur exactly once each; the awk program has no `'`, no `gensub`, `IGNORECASE`, `\s`, `\y` or `{` interval inside a regex; the function calls `timeout 5 head -c 65537` |

## 9. Error Handling

| Failure | Where | Handling | Host state after |
|---|---|---|---|
| `agent.yaml` exists but is not a regular file | AD-007 early | `fail`, "no files changed" | Unchanged |
| The same, appearing during the run | AD-007 late | `fail`; the trap restores the policy if a grant is pending | Previous policy, agent stopped (as L517) |
| Classifier read error, timeout, oversize, NUL or unrecognised form | AD-003 | Returns 1, so not enabled; reinstall prints the AD-008 "not enabled" line | No grant; "Config only" if the agent reads true; doctor reports it |
| Candidate drop-in invalid | L458 (unchanged) | `fail`, before any mutation | Unchanged |
| Effective policy invalid | L502 (unchanged) | Clear `GRANT_PENDING`, `restore_policy`, `fail` | Previous policy |
| Any failure between L501 and the agreement check | AD-006 trap | `restore_policy` when `GRANT_PENDING=true` | Previous policy; no new binaries |
| Agreement mismatch | AD-005 | Clear the pending flag, `restore_policy`, `fail` with guidance | Previous policy, agent stopped |
| Trap restore itself fails | AD-006 | One stderr warning; the exit status of the original failure is kept | Possibly the new drop-in; Ryan re-runs (R-2) |
| Doctor: sudo refused, password required, not found or timed out | AD-009 | One `✗` line plus one guidance line; doctor continues | Nothing written |

No failure path retries, loops or logs to disk. Installer output goes to the terminal only.

## 10. Security Considerations

**Hard safety constraints (binding on every pipeline agent, test and helper; copied from FRD SC-001 to SC-005):**
- **SC-001:** No test, helper, fixture or pipeline agent may ever run a real `shutdown`, `poweroff`, `halt` or
  `reboot`, or any equivalent (`systemctl poweroff|reboot|halt`, `init 0`, `telinit`), on any host:
  - Ryan's Windows PC (`shutdown` there);
  - Hawaii, vermont, PiVPN and PiCUPS;
  - the CI runners.

  Policy may be checked only by listing (`sudo -n -l …`, `sudo -n -U riot -l …`) or through injected runners. In the
  fixture, `systemctl` is a stub, and `assert_no_power_action()` is a tripwire.
- **SC-002:** No pipeline agent runs an installed `riot-agent` binary on any host (doctor included). Doctor behaviour
  is verified by unit tests with injected runners. Ryan runs doctor on real hosts.
- **SC-003:** The real `install.sh` runs only inside a disposable container fixture, never on a fleet host or on
  Ryan's PC.
- **SC-004:** No prose in Bash heredocs. Text is written to a file first and then passed by path.
- **SC-005:** The `AGENT-PRIV` branch is not modified.
- **Story constraints:**
  - existing sudoers lines stay byte-identical;
  - the only new grant is `riot ALL=(root) NOPASSWD: ${SYSTEMCTL_PATH} poweroff`;
  - no new persistent state, logs or files.

**Threats and mitigations.**

| ID | Threat | Mitigation |
|---|---|---|
| T-1 | The grant is wider than poweroff | Exact command, no arguments, so sudo allows only exactly `systemctl poweroff` (A-004). AC-008 lists the refused variants. Static tests pin the single echo |
| T-2 | A grant without config agreement | One variable (AD-002), the agreement check (AD-005), the pending-grant rollback (AD-006) |
| T-3 | The installer misreads a config as enabled | The narrow reject-first grammar (AD-003). The differential test against `agent.LoadConfig` (AD-004) |
| T-4 | A riot-controlled path read as root (symlink, FIFO, huge file) | No symlink reads; a 5 s `timeout`; a 64 KiB cap; content goes only to `$STAGE` and becomes a boolean, never printed |
| T-5 | Doctor executes power | A data-only argv source, reachable only from a `-n -l` listing (AD-011 reference rule); every recorded invocation asserted to be a listing (AC-024) |
| T-6 | Doctor false-pass as root | `agentPolicyRunner` lists `riot`'s policy (FR-026) |
| T-7 | Interactive prompt hijack | Unchanged `ask_yn` and `/dev/tty` semantics; prompts now run before any mutation |

**OQ-A3 (accepted, with AD-008).** `/etc/riot/agent.yaml` is `riot`-writable. A compromised agent can set
`allow_shutdown: true`, and the next root re-run grants poweroff. What that adds:
- Availability only: the host stays off until someone powers it on locally or out-of-band.
- It needs a compromised agent **and** Ryan's re-run.
- `riot` already holds `systemctl reboot` unconditionally.
- On Docker hosts, `riot` is in the `docker` group, which is root-equivalent.
- The AD-008 line shows the decision at re-run time.

**OQ-A2 (mitigated, AD-006).** Residual R-2: SIGKILL or power loss between L501 and AD-005 can leave a grant with no
config. No agent runs with `allow_shutdown: true` in that state. Any re-run reconciles it.

**SEC-OBS-1 (pre-existing, closed by AD-007).** On the fresh-install path, a non-regular `agent.yaml` path planted by
the agent user before the run could redirect the installer's root-owned config write and its ownership change. The
details were handed to the pipeline privately and are deliberately not described here. AD-007 refuses that state
before any mutation, and again after `/etc/riot` is locked.

**Observation (not changed).** `checkUpdatePrivilege` lists the invoking user's policy, without `-U riot`, so as root
it can false-pass. This is out of scope and recorded for a follow-up.

**Residuals.**

| ID | Residual |
|---|---|
| R-1 | A YAML error outside the classifier's rules makes the agent unable to load the config while the classifier says enabled. No agent runs, so the grant is unusable by rIOt. Doctor shows `Config load error` |
| R-2 | SIGKILL or power loss during the AD-006 window, or a failed trap restore |
| R-3 | A stale grant after Ryan sets `false` without a re-run (NFR-007, accepted in the FRD) |
| R-4 | A hand-added password-requiring `riot` rule could make the doctor listing pass while the agent is refused. Fleet rules come only from the installer (A-005) |
| R-5 | A `systemctl` shadowed in `/usr/local/{s,}bin` makes the grant ineffective (fail closed, doctor fails) |

## 11. Performance Considerations

- **Installer:**
  - one classifier call before the window (fresh: none; reinstall: one) and one inside it (AD-005);
  - each is bounded by `timeout 5` and 64 KiB, and is milliseconds in practice;
  - no new network request;
  - the human wait leaves the lock window (net gain).
- **Doctor:** at most one extra `sudo` process per run, at most 10 s, typically under 100 ms.
- **Fixture CI:** about 30 extra installer runs at a few seconds each, plus one trixie pass of the whole fixture.
  Acceptable for the existing step.

## 12. Implementation Notes for Engineers

1. **Bash in functions called from `if`.** `set -e` is suspended inside `config_enables_shutdown`. Write
   `cmd || return 1` on every line. With `pipefail`, `head` closing early is not an issue, because the snapshot is
   written to a file, not piped.
2. **awk.** The program must work in mawk 1.3.3. Compute indents with `match()` / `RLENGTH`. Count characters with
   `gsub` on a copy. The program must contain no `'` (the Go extractor and the bash quoting both depend on it). Print
   `enabled` only from `END`, and only if no reject flag was set.
3. **Keep the moved blocks byte-identical** apart from the inserted shutdown prompt, so the diff reads as a move. Do
   not touch the "Configure remote management features" header text.
4. **`GRANT_PENDING` ordering in `cleanup()`.** Restore the policy first (it reads `$BACKUP`, not `$STAGE`), then
   kill the lock helper, then remove `$STAGE`.
5. **C-4.** Never type `poweroff`, `power-off` or `halt` in a non-test string or constant under `internal/agent/...`
   or `cmd/riot-agent`, except in the `shutdownSudoCommand` literal. Build messages with
   `strings.Join(shutdownSudoCommand(), " ")`. Comments are not scanned. Run
   `go test ./internal/agent -run 'PowerPaths|ShutdownDoctor|ShutdownCallSites'` before pushing.
6. **pty harness** (`install_tty`):
   - Use `pty.fork()`. The child does `os.execv('/bin/bash', ['bash', ROOT/'install.sh', *args])`.
   - The parent reads the master fd with `select` (10 s per read, 180 s total).
   - It keeps a buffer. When the buffer ends with a known prompt (`<text> [y/N]: `), it writes `answer + '\n'` and
     records the prompt in order.
   - Any `[y/N]: ` or `[Y/n]: ` not in `answers` fails the test.
   - Wait for exit and return (rc, output, prompts).
   - Output has `\r\n` line endings, so normalise them.
7. **Golden constants.** Generate `PRE_STORY_DROPIN` and `PRE_STORY_DARWIN_CONFIG` once by running the same fixture
   scenario with `scripts/install.sh` from `71a4dea` (pre-story `main`), inside the container, and paste the bytes as
   Python literals with a comment naming the commit. Do not regenerate them from the new installer.
8. **Fixture fresh state.** `setUp` creates a reinstall state. Fresh tests must remove `/etc/riot/agent.yaml` (and
   keep or remove the legacy drop-in as the AC needs) and pass a URL (`https://new.test:7331`).
9. **A-003.** Keep the doctor's bare `systemctl`. Do not use `exec.LookPath`; an absolute path would ask about a
   different command string than the one the agent sends. Merged-`/usr` aliases still match because sudo compares
   inodes.
10. **Never** run `sudo systemctl poweroff`, `sudo -n systemctl poweroff` or a `riot-agent` binary while testing.
    Listing (`-l`) only. On Windows, the Go installer tests skip without Git Bash; the doctor tests are pure.
11. **README and CHANGELOG** (technical writer): name the fleet (hawaii, vermont, PiVPN, PiCUPS), with each host's
    action as "set `allow_shutdown: true`, then re-run `install.sh` as root, only where remote shutdown is wanted".
    Say that the same re-run also covers the pending AGENT-HARDEN-001 and PATCH-FLOW-001 re-runs. Say that agent-only
    updates never change the grant. Give the revoke steps. Describe the AD-008 line and the doctor section.

## 13. Definition of Done

- [ ] All component changes in §4 implemented; nothing in the "explicitly unchanged" list modified.
- [ ] Every AC row in §8 has its named test, and every test passes:
  - `go test ./...`;
  - `go test -race ./internal/agent/...` on Linux;
  - the fixture on `debian:bookworm-slim` **and** `debian:trixie-slim`, run locally in Docker as well as in CI.
- [ ] `git diff --exit-code main -- internal/agent/commands.go internal/agent/reboot_callsites_test.go internal/agent/shutdown_clearance_test.go internal/agent/shutdown_qa_test.go` is clean.
- [ ] `git diff --stat main -- internal/server web cmd` is empty.
- [ ] In `power_paths_test.go`, existing test functions are byte-identical; only additions (AD-011).
- [ ] `bash -n scripts/install.sh` and `shellcheck scripts/install.sh` add no new findings.
- [ ] No new lint errors (`golangci-lint run`).
- [ ] The differential test ran with mawk at least once (CI log or local container) and with the default awk.
- [ ] No migration (none needed).
- [ ] README and CHANGELOG updated per FR-036 to FR-039; `TestDocs_SS_*` pass.
- [ ] The PR body lists §0 for Ryan's review: OQ-A1 to OQ-A5, AQ-1 to AQ-5, and the C-4 amendment for the security
      reviewer.
- [ ] No pipeline agent ran a power command, the installer outside a container, or an installed `riot-agent`
      (SC-001 to SC-003).
- [ ] Tag rule: this is a code change, so release with a tag. Fleet re-runs are Ryan's action (FR-037).
