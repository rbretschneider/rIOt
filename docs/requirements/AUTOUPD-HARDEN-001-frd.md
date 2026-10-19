# Formal Requirements Document

**Story ID:** AUTOUPD-HARDEN-001
**Title:** Compose updates never run in an untrusted directory (SEC-002)
**Author:** Business Developer (brief FRD, written from the security finding; no interview)
**Date:** 2026-10-19
**Status:** APPROVED FOR DESIGN (amended 2026-10-19 after security review: FR-004, FR-011, §6, AC-006; FR-012 and AC-018 to AC-028 added)

---

## 1. Executive Summary

The agent runs as root. When it updates a Docker Compose service or stack, it runs
`docker compose --project-directory=<dir> pull` and then `up -d` in a directory it takes
from the container label `com.docker.compose.project.working_dir`. The only check today is
that some live container carries that label value. Anything that can set a container label
can therefore choose the directory where root runs compose. That includes an image's own
`LABEL` instruction, a `docker run --label`, a compose file from an untrusted repository,
and a server or device key that forges telemetry or commands. Compose then loads whatever
compose file and `.env` sit in that directory, or in a parent directory if the chosen one has
none, and starts the containers they describe with full daemon rights.

This story makes the agent the authority on where compose may run. Before any
`docker compose` invocation, and before it writes or removes its sanitized compose copy, the
agent validates the project directory and the files compose will read. They must be
controlled by accounts that already have Docker (root-equivalent) rights, must not be
writable by anyone else, and must not escape through symlinks. An optional allowlist of
compose roots in `agent.yaml` narrows this further. The server stops relying on a path for
stack auto-updates and names the compose project instead. The agent resolves that name
against the live daemon and checks it against any path an older server still sends.

Severity: moderate. Container-start rights are already root-equivalent, so the main
exposure is a hostile image or a hostile compose repository, plus a world- or user-writable
directory on the host. Even so, a root agent must never run compose in a directory chosen by
untrusted input.

---

## 2. Background & Context

**Where the directory comes from today.**

| Path | Source of `workDir` | File |
|---|---|---|
| Stack auto-update | Server reads `com.docker.compose.project.working_dir` from agent-reported telemetry and dispatches `docker_update {compose_work_dir}` | `internal/server/handlers/auto_update.go` (`checkAutoUpdates`) |
| Manual stack update (UI) | Dashboard sends `docker_update {compose_work_dir: stack.workDir}`, taken from the same label | `web/src/components/ComposeStackSection.tsx` |
| Stack update on the agent | `compose_work_dir` param, accepted when it is absolute and some live container has that label value | `internal/agent/docker_update.go` (`updateStack`) |
| Single compose service update | Label of the inspected container | `docker_update.go` (`dockerUpdateOne`) |
| Dependent recreate (network restore) | Label of the dependent container | `internal/agent/docker_deps.go` (`recreateComposeDependent`) |
| Sanitized-copy write and crash cleanup | Same `workDir` (write); every daemon-known working-dir label (cleanup) | `compose_sanitize.go`, `docker_recovery.go` |

Every compose invocation is built by `composeArgs` (`docker_update.go`). CMD-LIFECYCLE-001
(SEC C-9) already enforces an absolute path, a validated service charset, and single-token
`=` flags. This story adds the trust check that C-9 left open.

**Compose behavior that matters.** With `--project-directory` and no `-f`, compose looks for
`compose.yaml`, `compose.yml`, `docker-compose.yaml`, or `docker-compose.yml` in that
directory. It also merges a matching `*.override.*` file, reads `.env` for interpolation,
and, if none of the files are found there, **walks up parent directories**. A validated
directory with no compose file of its own can therefore still make compose load an
untrusted file from a parent.

**Why labels cannot be the authority.** The Docker daemon keeps no compose metadata beyond
container labels, and image labels are copied into container labels. Resolving "from the
live daemon" still yields label values. The control that matters is validating the
directory and files on disk. Project-name resolution only adds consistency, so that a server
path and the daemon's view must agree.

---

## 3. Stakeholders

- **Product owner:** runs the fleet and relies on nightly stack auto-updates. Wants this
  hardening to cause no regressions in normal setups (stacks owned by root or by a
  docker-group user).
- **Device administrators:** may keep stacks under a home directory or a dedicated service
  user. They need a clear failure message and a `riot-agent doctor` line telling them why a
  stack is refused and how to fix it.

---

## 4. Functional Requirements

**FR-001 — Single gate.** Every `docker compose` invocation the agent makes must pass a
project-directory trust check first. That covers stack update, single compose-service
update, bulk update, and dependent recreate. Writing the sanitized compose copy must pass
it too. Crash-recovery removal of a leftover sanitized copy must either pass it or be
provably safe without it (fixed file name, no symlink follow). A failure aborts before any
Docker change.

**FR-002 — Path shape.** The directory must be absolute. The agent resolves symlinks in it
and uses the resolved path for every later check and for compose itself.

**FR-003 — Trusted ownership (Unix).** The resolved directory, and every ancestor up to
`/`, must be owned by a trusted account. None of them may be writable by an untrusted
account (world-writable, or group-writable by an untrusted group). The trusted accounts are
root plus the accounts that already hold Docker daemon rights, since trusting them adds no
privilege: the owner of the Docker socket, and the members of the socket's group. The
architect defines the exact rule for sticky world-writable ancestors and for group
membership lookup. An account that cannot be resolved is not trusted (fail closed).

**FR-004 — Compose files in place.** At least one default compose file must exist directly
in the resolved directory, so compose never walks up to a parent. Each compose file,
override file, and `.env` that compose would load from that directory must be a regular
file. If it is a symlink, it must resolve to a regular file inside the resolved directory.
Each must be owned by a trusted account and not writable by an untrusted one. When the
project `.env` sets `COMPOSE_FILE`, every file it names replaces the default-file
requirement and must pass the same file checks. The agent passes the validated files to
compose explicitly, so compose loads nothing else.

**FR-005 — Optional compose roots.** A new agent config key `docker.compose_roots` (a list
of absolute paths) may be set. When it is set, the resolved directory must sit inside one
of the resolved roots. When it is unset, FR-002 to FR-004 still apply.

**FR-006 — Resolve by project.** `docker_update` accepts a new param `compose_project`. The
agent lists live containers with that `com.docker.compose.project` label. Their working-dir
labels must yield exactly one directory, else the precheck fails as ambiguous or not
found. That directory then goes through FR-001 to FR-005.

**FR-007 — Path from a server is only a cross-check.** If `compose_work_dir` arrives with
`compose_project`, it must match the agent's own resolution after cleaning, else the
precheck fails. If `compose_work_dir` arrives alone (older server, or a dashboard not yet
updated), the agent derives the project from the live containers that carry that directory.
It must find exactly one project, and the directory then goes through FR-001 to FR-005.

**FR-008 — Server sends a project name.** Stack auto-update dispatch sends
`compose_project`. The manual stack update in the dashboard sends it too. Both keep sending
`compose_work_dir` for older agents and for target-key conflict detection, which is keyed
by work dir (CMD-LIFECYCLE-001 AD-004).

**FR-009 — Clear failure.** A rejected directory is a precheck failure (`step = precheck`).
Its message names the path and the rule that failed (owner, mode, symlink escape, missing
compose file, outside compose roots, ambiguous project). It must not echo file contents.
Auto-update backoff treats it like any other failed run.

**FR-010 — Doctor.** `riot-agent doctor` reports the `docker.compose_roots` setting and
runs the same trust check against every compose project directory the local daemon knows.
It shows pass or fail with the reason for each.

**FR-011 — Windows.** Unix ownership and mode checks do not apply on Windows. Compose
operations on Windows are refused unless `docker.compose_roots` is set, and the doctor
explains why. With roots set, the other rules apply as the architect documents.

**FR-012 — Additional trust rules (security review).** The architect specifies each rule
(ADD AD-003, AD-013 to AD-017):

- A container whose image defines the compose working-dir label is never a source of a
  compose directory or project. A standalone recreate must not carry that label over.
- A compose directory or loaded file must not sit in a location that any container can write
  through a read-write mount or volume, nor in the Docker data root. Containers that mount
  the Docker socket are exempt. Refusals name the container and the mount.
- A group is trusted for write access only when all its members are trusted accounts, or
  when it is root's group or the Docker socket's group. The socket's group counts only when
  the socket grants that group read and write. A group-writable path with an extended ACL
  is refused.
- The directory must map to exactly one accepted project, equal to the project compose runs
  under.
- The compose child process gets a pinned Docker endpoint and no inherited `COMPOSE_*`
  variables.
- An unreadable or unparseable `.env` setting of `COMPOSE_FILE` is refused with a message
  that tells the operator what to fix.
- Refusal messages quote every label- or filesystem-derived value.

---

## 5. Compatibility Requirements

| Pairing | Required behavior |
|---|---|
| New server → new agent | `compose_project` + `compose_work_dir`. Agent resolves, cross-checks, validates. |
| New server → old agent | Old agent ignores `compose_project` and behaves as today. Stack updates keep working; the old agent stays unhardened until it is upgraded. |
| Old server → new agent | `compose_work_dir` only. FR-007 path, then validation. Stack updates keep working for trusted directories. |
| Dashboard manual stack and container updates | Keep working on new and old agents. |
| Existing `auto_update_policies.compose_work_dir` column | No migration. Not used for dispatch. |

---

## 6. Non-Goals

- Defending against an account that already has Docker daemon rights (root-equivalent).
- Validating files that trusted compose content itself references (`include`, `extends`,
  `env_file`, build contexts).
- Changing the standalone (non-compose) container update path, except that a standalone
  recreate drops the compose working-dir label (FR-012).
- Any DOCKER-UX-001 work.

---

## 7. Acceptance Criteria

| AC | Criterion |
|---|---|
| AC-001 | A relative or empty compose directory is refused at precheck, and no compose process starts. |
| AC-002 | A directory reached through a symlink is validated, and run, at its resolved path. A symlink whose target fails the checks is refused. |
| AC-003 | A directory, or an ancestor, owned by an untrusted account is refused. |
| AC-004 | A world-writable directory or ancestor is refused, subject to the architect's sticky-bit rule. A directory group-writable by an untrusted group is refused. |
| AC-005 | A directory owned by root, or by a docker-group member, and not writable by others is accepted. |
| AC-006 | A directory with no default compose file of its own, and no `COMPOSE_FILE` in its `.env`, is refused, so compose cannot walk to a parent. |
| AC-007 | A compose, override, or `.env` file that is untrusted-owned, untrusted-writable, or a symlink escaping the directory is refused. |
| AC-008 | With `docker.compose_roots` set, a trusted directory outside every root is refused, and one inside a root is accepted. With it unset, roots are not enforced. |
| AC-009 | `compose_project` resolving to exactly one working directory is used. Zero or more than one is refused at precheck. |
| AC-010 | `compose_project` plus a mismatching `compose_work_dir` is refused. A matching one, including after trailing-slash cleanup, is accepted. |
| AC-011 | `compose_work_dir` alone (old-server form) is accepted only when it maps to exactly one project and passes validation. |
| AC-012 | A single compose-service update, a bulk update, and a dependent recreate all refuse an untrusted directory taken from a container label. |
| AC-013 | The sanitized copy is never written into a directory that failed validation. |
| AC-014 | Server stack auto-update dispatch sends `compose_project` and `compose_work_dir`. A container-level policy sends neither. |
| AC-015 | The dashboard manual stack update sends `compose_project` and `compose_work_dir`. |
| AC-016 | `riot-agent doctor` prints the compose roots setting and a pass or fail line per known compose project directory. |
| AC-017 | A refusal message names the path and the failed rule. |
| AC-018 | A container whose image defines the compose working-dir label is ignored as a compose source and refused as an update or dependent target. It cannot make a project ambiguous. Compose-built images (which carry only project, service and version labels) are unaffected. A standalone recreate drops the label. |
| AC-019 | A directory or loaded file at or under a read-write bind source, a volume mountpoint, a bind-device volume, or the Docker data root is refused, and the message names the container and the mount. Read-only mounts and socket-mounting containers do not count. A stack directory that is only an ancestor of a bind source is accepted. |
| AC-020 | Compose loads exactly the validated files, passed as explicit `-f`. A `COMPOSE_FILE` in `.env` is honoured entry by entry, each validated. A `.env` that cannot be read is refused with an operator hint. One that cannot be parsed with certainty is also refused. |
| AC-021 | On Windows without `docker.compose_roots`, compose operations are refused and the doctor explains why. Roots on Windows compare case-insensitively. |
| AC-022 | A group-writable path with an extended access ACL, or whose ACL cannot be read, is refused. |
| AC-023 | Members of the socket's group are not trusted when the socket does not grant that group read and write. |
| AC-024 | A stack in a user-private group (owner and group both a trusted user, group-writable) is accepted. A path group-writable by a group with any untrusted or unknown member is refused. |
| AC-025 | A directory that maps to a project other than the one compose would run under is refused on every path. |
| AC-026 | The compose child process runs with `DOCKER_HOST` pinned to the configured socket and without inherited `COMPOSE_*` or `DOCKER_CONTEXT`. |
| AC-027 | Refusal messages quote label- and filesystem-derived values and contain no raw errno text. |
| AC-028 | A walked path component that is a symlink or not a directory is refused. |
