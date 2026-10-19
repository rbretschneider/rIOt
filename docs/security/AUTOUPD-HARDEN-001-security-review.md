# Security Review Report
**Story ID:** AUTOUPD-HARDEN-001 (fix for SEC-002)
**Reviewer:** Security Researcher Agent
**Date:** 2026-10-19
**Verdict:** REVISE (ARCHITECT), superseded by the Re-review (revision 2) section: APPROVED WITH CONDITIONS

Inputs: FRD and ADD for this story; `docker_update.go`, `docker_deps.go`, `compose_sanitize.go`,
`docker_recovery.go`, `commands.go`, `config.go`, `doctor.go`, `docker_api.go`, `docker_events.go`,
`scripts/riot-agent.service`, `scripts/install.sh`, server `auto_update.go`, `commands/targets.go`,
`models/command_policy.go`, and `web/src/components/ComposeStackSection.tsx`.

---

## Threat Model Summary

### Assets
- Host root. The agent (`riot`, docker group) runs `docker compose pull/up`, which is root-equivalent through the daemon.
- The integrity of the compose files and `.env` that compose loads.
- The availability of nightly stack auto-updates.

### Threat Actors
- A hostile or compromised image. Its `LABEL` values become container labels, and its processes
  write to host bind mounts and volumes as UID 0 or as a configured PUID. **This is the primary actor.**
- An unprivileged local account that can write somewhere on the host.
- A holder of a forged server or device key who can send commands or telemetry.
- Supply chain: a third-party image that gains `com.docker.compose.*` labels in a later tag.

### Attack Surface Introduced or Changed
- The new `compose_project` param. Its project name reaches argv as `--project-name=`.
- The new `docker.compose_roots` config key.
- The validator's reads of filesystem metadata for label-chosen paths. The refusal text goes back to the server.
- Server key fallback by project name.

---

## Findings

### HIGH

#### SEC-001: File ownership is treated as provenance, but containers write files as trusted UIDs
**Severity:** HIGH (blocks; a design-level change to AD-003, AD-001 and AD-007)
**Domain:** Authorization / trust model
**Location:** ADD AD-003, AD-005, AD-007, §10 ("Closed by this story"); `dockerUpdateOne`, `recreateComposeDependent`, `cleanupSanitizedLeftovers`
**Description:**
The gate trusts any directory or file owned by UID 0, the socket owner, or a docker-group member.
Container processes run as exactly those UIDs: root by default, or a docker-group PUID in
common images. They write freely into bind-mount sources and volumes. Container labels also
inherit the image's `LABEL` values. A hostile image therefore controls both inputs the gate
relies on: the label that selects the directory, and the trusted-owned files inside that
directory. The ADD's claim that an image label "can no longer send root-equivalent compose into
a directory ... an untrusted account can write" is true only for host accounts. It does not
hold for containers, and containers are the headline actor in the FRD.
**Attack Scenario:**
1. The victim runs a third-party image as a standalone container with `-v /srv/appdata/app:/config`. The container has a container-level auto-update policy, which is a core product feature.
2. A compromised tag adds `LABEL com.docker.compose.project=x`, `...service=pwn` and `...project.working_dir=/srv/appdata/app/.x`. `standaloneUpdate` recreates the container from `insp.Config`, and the daemon merges in the new image labels.
3. The container, running as root, writes `/config/.x/compose.yml` with `privileged: true` and a `/:/host` mount. On the host that file is root-owned, mode 0644, in a root-owned directory under `/srv/appdata` (owned by root or a docker-group user).
4. On the next update of that container, `dockerUpdateOne` now sees `isCompose` and calls `composeUpdate` with the label directory. `Check` passes every rule: owner, mode, placement, roots unset. The project `x` matches `composeProjectRe`.
5. `docker compose up -d -- pwn` starts a privileged container, and the attacker has host root.
The same steering works through bulk update and through `recreateComposeDependent` when the hostile container is a stale network dependent.
**Required Resolution (properties the ADD must specify):**
- (a) **Label provenance.** The agent must never accept a compose working directory (and, at
  the architect's discretion, the project and service) from a container whose image config
  defines `com.docker.compose.project.working_dir`. This applies on every path that reads
  compose labels: single-service, bulk, dependent recreate, P/W resolution in `updateStack`
  (exclude such containers from resolution so they also cannot cause `ambiguous_project`),
  sanitized-leftover cleanup, and doctor. If the image cannot be inspected, the check fails
  closed. The ADD must state whether compose-built images carry any `com.docker.compose.*` labels
  and make sure the rule does not refuse them by mistake.
- (b) **Container-writable locations.** The resolved directory and every checked file must not be at or under:
  - the Docker data root;
  - the host source of any read-write bind mount, or the mountpoint of any volume, of any
    container known to the daemon (running or stopped).

  Containers that themselves mount the Docker socket are exempt, because they are already
  root-equivalent (for example, stack managers that own the stacks directory). If the needed
  daemon data cannot be listed, the check fails closed. (b) also covers a legitimate stack that
  mounts `.` read-write into its own containers. A compromised image in that stack could
  rewrite `compose.yml` or `.env`, and the next auto-update would run the change.
- Update §10 so it no longer claims the image-label vector is closed by ownership alone.
**Blocks:** Implementation

#### SEC-002: `COMPOSE_FILE` in the project `.env` makes compose load files the gate never checks
**Severity:** HIGH
**Domain:** Input validation / gate coverage
**Location:** ADD AD-005, §10 R-2(b); FRD §6 (non-goal assumes "the agent never passes -f or COMPOSE_FILE")
**Description:**
Compose v2 reads `.env` in the project directory and honours `COMPOSE_FILE` and
`COMPOSE_PATH_SEPARATOR` from it. When `COMPOSE_FILE` is set, compose loads exactly the listed
files. It does not load the default names or the override files that AD-005 validates. The
FRD's non-goal rests on the agent never passing `COMPOSE_FILE`, but the trusted `.env`
effectively passes it. AD-005 states that it checks "the compose files compose would load". With
`COMPOSE_FILE` set, it checks files compose will not load and skips the ones it will. The
`no_compose_file` guarantee (no parent walk) also loses its meaning. This is inconsistent with
the ADD's own decision to refuse an untrusted-writable `compose.override.yml`.
**Attack Scenario:**
A common multi-file layout has a `.env` (trusted, 0644) containing `COMPOSE_FILE=compose.yml:compose.prod.yml`.
`compose.prod.yml` was created with a loose umask (0666), or it is owned by a non-docker
deploy user. An unprivileged local user, or a container that can reach the file through SEC-001(b),
adds a privileged service to `compose.prod.yml`. The validator passes, because `compose.yml`
and `.env` are clean. The next auto-update starts the injected service as root.
The same applies when `COMPOSE_FILE` names `../shared/base.yml` or an absolute path outside D.
**Required Resolution:**
Every file compose will load must pass the AD-005 file rule. That includes each entry of a
`COMPOSE_FILE` defined in the project `.env`, resolved the way compose resolves it, relative to D.
An entry outside D must also have its directory chain pass the AD-003 component rule (leaf rule
for its own directory). If the agent cannot decide the effective `COMPOSE_FILE` with certainty, the
check fails closed with a new rule. Examples are interpolation, `export` syntax, and quoting the
agent does not model. The same holds for `COMPOSE_PATH_SEPARATOR`. A refusal may name the
offending path but must echo no other `.env` content. The architect may instead neutralise
`.env`-sourced `COMPOSE_FILE` for the child process, provided the files compose actually loads
are the ones validated. Update the FRD non-goal wording accordingly.
**Blocks:** Implementation

---

### MEDIUM

#### SEC-003: A POSIX ACL can grant write access while the mode bits still pass
**Severity:** MEDIUM
**Domain:** Authorization
**Location:** ADD AD-003 (group-writable rule), R-5
**Description:** On a file or directory that has an extended ACL, the group bits in `st_mode`
are the ACL mask. The directory `/opt/stacks/app` with `root:root 0775` plus `user:alice:rwx`
shows group-write with a trusted GID (0), so the validator accepts it. A named-user or
named-group ACL entry can only grant write when the mask contains `w`. The gap is therefore
exactly the case "group-write bit set and an access ACL present".
**Attack Scenario:** An administrator grants a non-docker user write access to a stack directory with
`setfacl`. That user plants `compose.override.yml`, and the next update runs it as root.
**Required Resolution:** If a checked path has the group-write bit set, the validator must refuse it
when an extended access ACL exists. A read error other than "no ACL" or "not supported" also fails closed.
A path without group-write needs no ACL inspection.

#### SEC-004: Windows keeps label steering open
**Severity:** MEDIUM
**Domain:** Authorization
**Location:** ADD AD-009, R-3
**Description:** Without owner and ACL checks, any directory that holds a default compose file
passes on Windows. That includes a user profile directory or a bind-mount source. A hostile
image label still selects where compose runs, so SEC-002 stays open in practice.
**Attack Scenario:** An image label points at `C:\Users\Public\x`, which contains a hostile
`compose.yml`. The update runs it with full daemon rights.
**Required Resolution:** On Windows, compose operations must be refused unless
`docker.compose_roots` is set, with a new rule name. Doctor must say why. Roots on Windows must be
compared case-insensitively and after resolution, and the SEC-001 provenance rule still applies.

---

### LOW / INFORMATIONAL

#### SEC-005: Docker-group members are trusted even when the socket does not grant the group access
**Severity:** LOW. **Location:** AD-003.
A socket with mode `0600` or `0640` gives its group no Docker rights. Members of that group are
then not root-equivalent, yet the validator trusts them. For example, a group member's home stack
directory steered by an image label would pass.
**Required:** Trust members of the socket group only when the socket mode grants the group both
read and write (`0060`). Otherwise the trusted set is root, the euid and the socket owner.

#### SEC-006: Project name is not tied to the directory on single-service and dependent paths
**Severity:** LOW. **Location:** AD-006, AD-007, `recreateComposeDependent`.
A container with labels `project=evil` and `working_dir=<a trusted stack>` makes the agent run the
trusted compose file under a second project name. That starts duplicate containers that share
the same bind-mounted data, which risks data corruption. SEC-001(a) removes the image-label
source of this problem, but the invariant should hold everywhere.
**Required:** On every path, the resolved directory must map to exactly one project among the
provenance-accepted live containers, and that project must equal the project that is passed to
`--project-name`. This is the rule AD-007 already applies to the W-only form.

#### SEC-007: The refusal text is a label-driven filesystem oracle and an injection channel
**Severity:** LOW. **Location:** ADD §6 message format.
The message reflects label-sourced paths verbatim, which may include control characters or
newlines. It also returns owner, mode and existence facts for arbitrary paths to the server and
the dashboard.
**Required:** Format every label-derived value with Go `%q` or strip control characters. Limit the
detail to the offending path, the rule, the numeric uid and the mode. Collapse `Lstat` errno
detail to "cannot stat".

#### SEC-008: FUSE mounts with `user_allow_other` report ownership the attacker chooses
**Severity:** LOW (requires a non-default `/etc/fuse.conf`).
**Recommendation:** Refuse D, or any checked file, that sits on a FUSE filesystem (statfs magic),
or document the gap under R-5.

#### INFO-1: Transitive references are accepted as residual risk
`include`, `extends`, `env_file` and build contexts in a trusted file are trusted transitively.
That is acceptable because a trusted admin chose them, and the admin's own manual
`docker compose up` has the same exposure. Document it in the README beside `compose_roots`.

#### INFO-2: The compose child process environment is not pinned
The child inherits the agent environment, and the Docker CLI context in the `riot` home.
The trusted set is derived from `DockerConfig.SocketPath`, but the compose CLI may talk to a
different daemon. Recommendation: give the child an explicit environment. Set `DOCKER_HOST` to
the configured socket, and remove inherited `COMPOSE_*` and `DOCKER_CONTEXT` variables.

#### INFO-3: The server-side key fallback is acceptable
The fallback by `compose_project` only affects conflict detection on the server. The agent lock
is authoritative, and an ambiguous project returning `nil` is safe. The server always sends `W`.
Optional hygiene: reject a `compose_project` that fails `composeProjectRe` at dispatch.

#### INFO-4: Compatibility windows
- R-1: an agent older than this story stays fully exposed when a new server sends it work. The
  server picks `W` from the first container with the policy's project label, so a hostile
  container carrying that project label can redirect an old agent. A new agent refuses that as
  ambiguous. The product owner should decide whether stack auto-update dispatch should require a
  minimum agent version.
- The old-server path (W only) is sound once SEC-001(a) filters resolution.

#### INFO-5: Functional note, not a security issue
Under `ProtectSystem=strict`, the sanitized copy cannot be written to most stack directories,
because only `/var/lib/riot` and `/etc/riot` are writable. The gate does not change this.

---

## Assessment of the specific questions

| Question | Assessment |
|---|---|
| Gate on every invocation and sanitized write/remove | Structurally yes. All 4 `composeArgs` sites, the sanitize write, `reconcileInterrupted` and `cleanupSanitizedLeftovers` sit behind `Check`. Dependents in recovery are covered too, because `Check` lives inside `recreateComposeDependent`. Coverage is complete, but what the gate trusts is not (SEC-001, SEC-002). |
| Symlink races / TOCTOU | Sound against host accounts. The properties are structural (no untrusted writer on any component), compose runs at the resolved path, and `dir_changed` re-checks. Required: refuse explicitly when `Lstat` reports a symlink or a non-directory for any component, rather than assuming it cannot happen (C-7). |
| Parent-directory walk | Closed by `no_compose_file`, except when `COMPOSE_FILE` is set (SEC-002). |
| `.env` / override files | Checked. `COMPOSE_PROJECT_NAME` in `.env` is overridden by the `--project-name` flag. `COMPOSE_PROFILES` only selects services from trusted content. `COMPOSE_FILE` is the gap (SEC-002). |
| `include` / `extends` | Accepted residual risk (INFO-1). |
| `--project-name` injection | Closed. `^[a-z0-9][a-z0-9_-]*$` excludes a leading `-`, `=` and whitespace, and the value is emitted as a single `=` token. |
| Trusting the euid | Acceptable. `riot` is already in the docker group, and no agent feature writes server-chosen content to a server-chosen path. |
| Trusting the socket owner / group | Acceptable, once SEC-005 adds the socket-mode condition. |
| Sticky ancestors | Correct. The next component is checked, so it cannot be renamed by a non-owner, and sticky never rescues the leaf. |
| Fail closed | Correct for an unresolved UID, a group lookup failure, a socket `Stat` failure, `Lstat` errors and invalid roots. Note: `/var/lib/docker` is mode 0710, so volume paths fail closed with `stat` for `riot`, but they pass for a root-run doctor. SEC-001(b) closes that difference. |
| Root-prefix checks | `filepath.Rel` with a `..` test is correct. QA must verify that the test is separator-aware: `..foo` is not an escape, and a sibling `/opt/stacks-x` is not inside `/opt/stacks`. |

---

## Positive Observations
- A typed `trustedDir` as the only input to `composeArgs`, with a DoD grep, is a strong single-gate design.
- Re-checking at each change point, plus `dir_changed`, keeps the TOCTOU window narrow.
- The validator never reads file contents, so refusals cannot leak them.
- An invalid `compose_roots` fails closed without taking the agent offline.
- Resolving by project, with the server path used only as a cross-check, and pinning `--project-name`, removes server authority over paths.
- The leaf-versus-ancestor sticky rule is correctly reasoned.
- Recovery takes "pass the gate" rather than "argue it is safe".

---

## Verdict Rationale

**REVISE (ARCHITECT).** SEC-001 is a design-level flaw. The trust model equates "owned by a
Docker-rights UID" with "written by a trusted party", and containers break that equation. The
hostile-image vector, which is the primary vector in the FRD, therefore remains exploitable for
container-to-host escalation through standalone, bulk and dependent updates. SEC-002 leaves a
second load path outside the gate. Both need new rules in AD-003, AD-005 and AD-007, plus a §10
correction, before implementation.

### Required ADD revisions (block implementation)
1. **SEC-001(a):** add a label-provenance rule on every path that reads compose labels, and use it to filter P/W resolution. Fail closed when the image cannot be inspected.
2. **SEC-001(b):** refuse directories and files that are at or under the Docker data root, or under any read-write bind-mount source or volume mountpoint of a container without Docker-socket access. Fail closed when the needed daemon data is unavailable.
3. **SEC-002:** validate every file named by `COMPOSE_FILE` in the project `.env`, as defined above, or neutralise that variable. Fail closed when the value cannot be determined.
4. Correct ADD §10 and the FRD §6 non-goal to match.

### Conditions carried into implementation (QA verifies)
- C-1 (SEC-003): refuse a group-writable path that carries an extended access ACL. Fail closed on errors.
- C-2 (SEC-004): on Windows, refuse compose operations unless `compose_roots` is set; doctor explains why.
- C-3 (SEC-005): trust socket-group members only when the socket mode is `0060` or wider for the group.
- C-4 (SEC-006): on every path, the directory must map to exactly one provenance-accepted project, and that project must equal `--project-name`.
- C-5 (SEC-007): quote label-derived values in messages, and do not return raw errno detail.
- C-6 (INFO-2): give the compose child an explicit environment, with `DOCKER_HOST` set to the configured socket and inherited `COMPOSE_*` and `DOCKER_CONTEXT` removed.
- C-7: refuse explicitly when any walked component is a symlink or a non-directory.
- C-8: add tests for SEC-001 to SEC-006, C-7 and the separator-aware root checks.

---

## Re-review (revision 2)

**Date:** 2026-10-19. **Inputs:** ADD revision 2 and the amended FRD (FR-012, AC-018 to AC-028).
**Verdict:** APPROVED WITH CONDITIONS. This supersedes the REVISE verdict above.

### Disposition of the earlier findings

| Item | Status | Where |
|---|---|---|
| SEC-001a, label provenance | Resolved | AD-013 builds a snapshot that excludes a container from every label read when its image defines the working-dir label or cannot be inspected. AD-017 closes laundering through a standalone recreate. |
| SEC-001b, container-writable locations | Resolved | AD-014 refuses the data root and read-write bind or volume sources, including stopped containers and bind-device volumes. The socket-mount exemption cannot be influenced by an image. |
| SEC-002, `COMPOSE_FILE` | Resolved | AD-001 and AD-015 pass an explicit, validated `-f` set. A parser mis-read now fails functionally, not as a security gap. This relies on condition 1. |
| SEC-003 to SEC-007 | Resolved | AD-003 (ACL; socket `0060`), AD-009, AD-013 (project mapping), §6 |
| SEC-008 (FUSE) | Accepted residual | R-5 |
| C-1 to C-8 | Met | AD-003, AD-009, AD-013, AD-016, §6, AC-018 to AC-028 |
| INFO-2 | Resolved | AD-016. `DOCKER_HOST` in the environment also outranks a CLI context and any `.env` value. |

### New decisions assessed

- **AD-013 (snapshot):** sound. Each check takes a fresh snapshot, and an inspect failure
  excludes the container. A container labelled before this story by an old agent through the
  laundering path still has to pass AD-014. Its planting mount is in the snapshot, unless the
  writer container has since been removed. That leftover is LOW, and R-2 accepts it.
- **AD-014:** the direction is correct: `./config:/config` is accepted and `.:/app` is refused.
  One gap: when a mount source cannot be resolved, the check falls back to `Clean`. If the
  source contains a symlink that the agent cannot resolve for a reason other than ENOENT, a
  directory reached by another path is wrongly accepted (condition 2).
- **AD-015:** acceptable. The strict grammar and refusal on any doubt are the right posture,
  and the content is never echoed. Its security rests entirely on `-f` overriding `.env`'s
  `COMPOSE_FILE` on the installed compose version (condition 1).
- **AD-017:** correct and minimal. Removing only the working-dir key cannot break a
  standalone container, because a full triple would take the compose path.
- **AD-003 (UPG and nsswitch):** the logic is sound for `files`. Two sources can hold
  accounts that the parser does not see:
  - The `systemd` source exposes userdb records and membership drop-ins, and systemd-homed
    users. Enumeration would then miss members, and the UPG rule would over-trust.
  - In `compat` mode, `+` and `-` lines in passwd or group pull in NIS entries.

  See condition 3. Also not visible, and to be documented as residual risk:
  - unit-level `Group=` / `SupplementaryGroups=` on services that do not run as Docker-rights users;
  - group passwords in `gshadow`, which the agent cannot read.
- **AD-011 (server skip):** this narrows the old-agent redirect. However, the server cannot
  see provenance. A hostile image that copies a policy's project label with another working
  directory therefore stops that stack's auto-updates for **every** agent, and only a server
  log records it. That is a silent denial of service (condition 4).

### Conditions (the senior dev must implement them; QA verifies)

1. **Verification gate.** Before merge, the three verification items in §12 must pass on a
   real host, with an automated test where feasible:
   - explicit `-f` overrides `.env`'s `COMPOSE_FILE`;
   - `.env` is still read from the project directory;
   - `--project-name=` and repeated `-f=` are accepted.

   If `-f` does not override `.env`'s `COMPOSE_FILE`, implementation stops and the story goes
   back to the architect. Recording a failure in the report is not enough.
2. **AD-014, unresolvable sources.** If a writable mount source outside the data root fails
   `EvalSymlinks` for any reason other than not-exist, `Check` refuses with
   `container_writable`. The message reports "cannot resolve" for that source. The check must
   not fall back to a lexical comparison.
3. **AD-003, account enumeration.**
   - With `systemd` listed in nsswitch, member enumeration (the UPG rule) is enabled only when
     no userdb user, group or `.membership` records exist in the standard userdb directories,
     and no systemd-homed service socket exists.
   - A passwd or group line that starts with `+` or `-` marks the account database
     unavailable.
   - Add tests for both cases.
4. **AD-011, visible skip.** A stack the server skips as multi-workdir must show in the
   dashboard. Either record it as a policy outcome or event naming the project and the
   conflicting working directories, or carry it in the existing policy-status surface. A
   log-only skip is not acceptable.
5. **Provenance by key presence.** The provenance rule triggers when the image defines the
   key at all, including an empty value. Add a test case for the empty value.
6. **README.** Document these residual risks for the UPG rule:
   - unit-level `Group=` / `SupplementaryGroups=` on services;
   - `gshadow` group passwords;
   - writers that ran in a since-removed container.

No HIGH or CRITICAL findings remain. The conditions above are LOW to MEDIUM and need no
further design change, except condition 1, which sends the story back only if verification
fails.

---

## QA-D2 ruling (re-review condition 2)

**Ruling:** APPROVE WITH CONDITIONS. This replaces condition 2 above with the refinement
below. Condition 2 as written would block every update on common hosts (a source under a home
directory with mode 0700/0750).

The refinement's premise holds only for one directory: the first directory the agent cannot
traverse, called P here. An untrusted host account cannot add a symlink inside P if P and its
ancestors are trusted-owned and not untrusted-writable. A **container**, however, can. If any
container has a read-write mount at or above P, its root user can replace a remainder component
with a symlink, and the file is root-owned, so it counts as trusted. The next start of the
container that mounts the source then follows that symlink, and its bind lands somewhere else,
such as `/opt/stacks`. The lexical comparison misses that.

Conditions:
1. **No other writable source above it.** The lexical fallback is refused when any other
   writable source of a container without a socket mount is a proper ancestor of the
   unresolved source. The ancestor test is made against both the lexical path and the
   resolvable prefix.
2. **Compare the right string.** The comparison uses the resolved prefix up to P, joined with
   the cleaned remainder, not the original string. Symlinks above P are still honoured.
3. **P itself.**
   - EACCES qualifies only when `Lstat` reports P as a real directory.
   - The AD-003 owner, mode and ACL rules apply to P and every ancestor of P.
   - ENOTDIR, ELOOP and any other error refuse as `container_writable` ("cannot resolve").
   - ENOENT keeps the AD-014 fallback.
4. **Doctor.**
   - Doctor warns when its euid differs from the UID of the agent's service user, and names
     that user.
   - Doctor prints an info line for each source that was compared through the fallback.
5. **Tests.** Add these cases:
   - the home-0750 sandbox source is accepted;
   - an untrusted-owned P is refused;
   - a group-writable P with an ACL is refused;
   - a writable source that is an ancestor of the unresolved source is refused;
   - ELOOP and ENOTDIR are refused;
   - the doctor euid warning.
