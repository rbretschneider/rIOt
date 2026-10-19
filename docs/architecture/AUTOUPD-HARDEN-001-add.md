# Architecture Decision Document

**Story ID:** AUTOUPD-HARDEN-001 (SEC-002)
**FRD Reference:** docs/requirements/AUTOUPD-HARDEN-001-frd.md
**Security review:** docs/security/AUTOUPD-HARDEN-001-security-review.md (REVISE). Revision 2 addresses SEC-001 to SEC-007 and conditions C-1 to C-8.
**Builds on:** docs/architecture/CMD-LIFECYCLE-001-add.md (AD-004 target keys, AD-012 locks, SEC C-5, SEC C-9)
**Author:** Architect Agent
**Date:** 2026-10-19
**Status:** FINAL (revision 2, with the revision 3 addendum below)

> **Revision 3 addendum (after QA round 1).** These decisions supersede the matching text below:
>
> - **AD-011, server skip removed.** A stack policy whose project shows more than one working directory in telemetry is no longer skipped, and the `stack_update_skipped` event was dropped. The server dispatches `docker_update` with `compose_project` only. The agent is authoritative: it resolves the labels and refuses with `ambiguous_project` if the directories really differ. That refusal is visible as a failed command with backoff. Old agents get no path and fail closed with their existing "required" precheck. A project-only dispatch holds the compose keys of every working directory the device reports for that project, so a new command is not queued every telemetry cycle. Every mention of the skip in §4, §8 (AC-014) and R-1 is superseded by this paragraph.
> - **AD-006/AD-007, symlink-equivalent labels (QA-D1).** Working-dir labels are grouped by their symlink-resolved directory. A label that does not exist is grouped by its cleaned path. Labels that resolve to one directory count as one directory for project resolution, the `compose_work_dir` cross-check, update scope and post-update verification.
> - **AD-014, unreadable mount sources (QA-D2).** The trusted-owner EACCES fallback in the security review's "QA-D2 ruling" replaces the strict "cannot resolve" refusal, under that ruling's five conditions.
> - **AD-015, `.env` parsing (QA-D3/D4).** A leading UTF-8 BOM is stripped, and only the exact keys `COMPOSE_FILE` and `COMPOSE_PATH_SEPARATOR` are parsed.

---

## 1. Summary

A new agent-side validator, `composeTrust.Check`, is the only way to get a
`trustedDir`. `composeArgs` accepts only a `trustedDir`, so no compose
invocation and no sanitized-copy write can skip the check.

The validator decides three things against a fresh snapshot of daemon state:

- **Which containers count as compose sources.** A container whose image itself
  defines the compose working-dir label does not.
- **Whether the directory and files are free of untrusted writers.** Untrusted
  writers are host accounts without Docker rights, and containers through their
  read-write mounts.
- **Which exact files compose will load.** The agent passes them as explicit
  `-f` tokens, so compose loads exactly the files that were validated.

Compose runs at the resolved path, with the project name pinned and a pinned
child environment. Stack updates resolve by `compose_project`. A path from the
server is only a cross-check.

## 2. Technical Context

- **Runtime user.** The agent runs as `riot`, a member of the `docker` group,
  under `ProtectSystem=strict`. That user is root-equivalent through the daemon,
  but it cannot read files such as a `.env` with mode 0640 owned by an operator.
  Release binaries are built without cgo.
- **Today's state.** The compose working directory comes from container labels.
  Labels include the image's own `LABEL` values. `updateStack` checks only that
  the path is absolute and that some label carries it, and `composeArgs`
  (SEC C-9) checks only the argv shape.
- **Compose call sites.** There are four: in `composeCore`, pull, the sanitized
  retry pull and up; and `recreateComposeDependent`. The sanitized copy is
  written by `sanitizeComposeFile` and removed by `removeSanitized`,
  `reconcileInterrupted` and `cleanupSanitizedLeftovers`.
- **Patterns followed.**
  - Test seams are `Agent` fields where nil means production (`dockerFactory`,
    `procFactory`).
  - Platform code uses `_unix.go`/`_windows.go` pairs (`fileowner_*.go`).
  - Doctor uses report functions with injected dependencies and
    `captureStdout` tests.
- **Test platforms.** CI runs `go test -race ./...` on ubuntu. Local runs use
  `go test ./...` on Windows. Validator tests must not depend on real ownership.
- **Docker SDK v28.3.3.** The fields used are `system.Info.DockerRootDir`,
  `container.Summary.Mounts` (Type, Name, Source, Destination, Driver, RW),
  `image.InspectResponse.Config.Labels`, and `volume.Volume.Options`.
- **Verified compose behavior.** This was observed on a real fleet. Compose v2
  (5.x) builds images (`build:`) with the labels `com.docker.compose.project`,
  `.service` and `.version`. It does **not** set
  `com.docker.compose.project.working_dir` on images. The working-dir label is
  only ever set on containers, at create time.
- **No DB migration.**

## 3. Architecture Decisions

### AD-001: `trustedDir` is the only input `composeArgs` accepts (single gate, FR-001)

**Decision:** New file `internal/agent/compose_trust.go`:

```go
type trustedDir struct{ dir string; files []string } // resolved dir + the exact -f set; zero value invalid
func (t trustedDir) Path() string
func (t trustedDir) Files() []string
type trustRequest struct {
    Dir      string // label or param value, verbatim
    Project  string // project passed to --project-name; "" only in recovery (derive the unique one)
    SourceID string // container whose labels named Dir; "" for stack updates
}
func (a *Agent) composeTrust() *composeTrust
func (t *composeTrust) Check(snap daemonSnapshot, req trustRequest) (trustedDir, error) // *composeTrustError
func composeArgs(td trustedDir, project, sanitizedFile string, sub []string, services ...string) ([]string, error)
```

`composeArgs` refuses a zero `trustedDir` or an empty `Files()`. It emits these
tokens, in order:

1. `--project-directory=<td.Path()>`
2. `--project-name=<project>`
3. Either one `-f=<file>` per entry of `td.Files()`, or `-f=<sanitizedFile>`
   alone when the sanitize retry is in use
4. The sub-command
5. `--`
6. The services

`project` is mandatory and must match `^[a-z0-9][a-z0-9_-]*$`.

**Rationale:** If the trusted value is a required input, the compiler enforces
the gate, and `composeArgs` stays pure. Explicit `-f` tokens disable compose's
own file discovery, including `COMPOSE_FILE` from `.env` and the parent walk.
"Files compose loads" therefore equals "files validated" by construction
(AD-015).

**Consequences:** No same-package code may build a `trustedDir{}` literal by
hand outside `compose_trust.go`. Review enforces this, backed by a DoD grep.

### AD-002: Where and when the check runs

**Decision:** The order inside one `docker_update` is:

1. `restartInProgress`
2. Target resolution (AD-007)
3. Target lock
4. **Check #1** at `precheck`
5. pull
6. If the sanitize retry is needed: **Check #2**, before `markDocker` records
   `SanitizedFile`
7. **Check #3**, after pull and before `stopDependents`
8. up

- Each check loads a fresh `daemonSnapshot` (AD-013).
- `composeCore` scopes containers from the check #1 snapshot.
- If a later check resolves a different directory or file set, the result is
  refused with `dir_changed`.
- Every refusal returns `Step: precheck`, because no container has changed yet.
- A snapshot load failure returns rule `daemon_unavailable`.
- `recreateComposeDependent(ctx, api, insp)` gains `api`. It loads a snapshot
  and runs `Check` with `SourceID = insp.ID` and the dependent's project label.
  A refusal is that dependent's restore failure.
- The in-flight marker is written at intake, as today. Every check runs before
  the first `markDocker` that its step needs.

### AD-003: Unix host-account trust model (FR-003; C-1, C-3, C-7, UPG)

Trust is computed once per `Check`. There is no cache across calls.

**Account database.** The validator parses `/etc/passwd` and `/etc/group`
itself, through `trustFS.ReadFile`, and does not use `os/user`.

- A malformed line, or a read failure, marks the database unavailable. The
  validator then fails closed.
- Member enumeration is enabled only when `/etc/nsswitch.conf` is missing, or
  when its `passwd:` and `group:` sources are all in `{files, compat, systemd}`.
  Other sources (ldap, sss, and so on) can hold users the files do not list.

**Socket.** The path is `DockerConfig.SocketPath`, or `/var/run/docker.sock`
when it is empty. It is checked with `trustFS.Stat`, which follows symlinks.
The socket's group is effective only when the socket mode includes `0060`
(C-3).

**Trusted UIDs:**

- `0`
- the euid
- the socket owner
- while the socket group is effective and the database is available, any UID
  whose passwd primary GID is the socket GID, or whose user name is in the
  socket group's member list

**Trusted GIDs:**

- `0`
- the socket GID, while the socket group is effective
- **(UPG rule)** any GID that has a `/etc/group` entry and at least one member,
  where every member is a trusted UID. Members are the names in the group's
  member list (a name with no passwd entry makes the group untrusted) plus every
  passwd entry whose primary GID is that group. This rule needs the database
  available and enumeration enabled.

**Fail closed:**

- If the socket `Stat` fails, the trusted set is UIDs `{0, euid}` and GID `{0}`.
- If the database is unavailable or enumeration is disabled, the trusted set is
  UIDs `{0, euid, socket owner}` and GIDs `{0, socket GID if effective}`.

**Component walk.** The walk covers the resolved directory D and every ancestor
up to the root. It also covers the directory chain of any `COMPOSE_FILE` entry
outside D. Each component is checked with `Lstat`:

| Check | Leaf directory | Ancestor |
|---|---|---|
| A symlink or a non-directory (C-7) | `not_directory` | `not_directory` |
| Owner UID not trusted | `owner` | `owner` |
| Group-writable (`0020`) and GID not trusted | `group_writable` | allowed only with the sticky bit |
| Group-writable and `trustFS.HasAccessACL` is true or errors (C-1) | `acl` | `acl` |
| World-writable (`0002`) | `world_writable`, sticky or not | allowed only with the sticky bit |

- **Files** use the leaf rule: trusted owner, no untrusted group-write, ACL rule,
  no world-write.
- **ACL check:** only paths with the group-write bit set are checked. On Linux it
  reads xattr `system.posix_acl_access`; `ENODATA` or `ENOTSUP` means no ACL.
  On other Unix systems it returns an error, so a group-writable path there
  fails closed.
- **Why sticky never rescues the leaf:** an untrusted user could still create a
  new `compose.override.yml` or `.env` in it.

### AD-004: Testability seam

```go
type fileMeta struct { Mode os.FileMode; UID, GID uint32; HasOwner bool }
type trustFS interface {
    Lstat(path string) (fileMeta, error)
    Stat(path string) (fileMeta, error)
    EvalSymlinks(path string) (string, error)
    ReadFile(path string, max int64) ([]byte, error) // passwd, group, nsswitch.conf, project .env
    HasAccessACL(path string) (bool, error)
    EUID() int // -1 on Windows
}
```

- **Production:** `osTrustFS` in `compose_trust_unix.go` (`syscall.Stat_t`,
  `syscall.Getxattr` on Linux) and in `compose_trust_windows.go` (no owner).
- **Agent seams:**
  - `trustFS` (nil means `osTrustFS{}`)
  - `trustPlatform` (empty means `runtime.GOOS`)
  - `procFactory`, whose signature becomes `func(name string, env []string, args ...string)`
- **Unit tests:** build `composeTrust` with a map-backed `fakeTrustFS` and a
  hand-built `daemonSnapshot`. Paths are rooted at
  `filepath.VolumeName(os.TempDir())+separator`, so the same tests run on
  Windows and Linux.
- **Harness:** `newDockerHarness` uses `rootOwnedFS`. It reads the real
  filesystem, reports UID/GID 0 with modes `0755`/`0644`, never reports an ACL,
  returns an identity `EvalSymlinks`, and serves fixed passwd and group text.
  `fakeDocker` gains `Info` (data root `/var/lib/docker`), `VolumeInspect`, and
  per-image labels.

### AD-005: Files checked, and the exact `-f` set (FR-004, SEC-002)

**Checked in D:**

- the 4 default names
- the 4 override names
- `.env`

Each name that exists is checked with `Lstat`:

- A symlink must resolve inside D (`symlink_escape`) to a regular file
  (`not_regular`). Intermediate directories follow the leaf rule.
- The file rule of AD-003 applies.
- AD-014 applies.

The ownership checks never read file contents.

**`.env` read.** This is the only file the validator reads, and only after it
passes the checks above. The read is capped at 1 MiB, and the content is never
echoed.

- A read error, including `EACCES`, refuses with `env_unreadable`, using the
  message `the agent user cannot read "<path>"; compose must read it too —
  grant the agent user read access (e.g. group docker, mode 0640)`. This is not
  a regression: compose, running as the same user, would fail on the same file.
- Over the cap: `env_unparsed`.

**The `-f` set (AD-015).**

- **`.env` defines `COMPOSE_FILE`:** the entries, in order.
- **Otherwise:** the first existing default name, followed by the first
  existing override name. Each list uses compose's own order, copied verbatim
  from compose-go `DefaultFileNames` and `DefaultOverrideFileNames` of the
  installed compose version (recorded in the implementation report).
- **No default file and no `COMPOSE_FILE`:** `no_compose_file`.

All `-f` paths are passed **resolved and absolute**.

### AD-006: Resolved path, pinned project name

- `Check` returns `R = EvalSymlinks(Clean(dir))`, and compose runs there.
- `composeSpec{WorkDir, Project, Service, SourceID}` describes the target.
- `projectContainers` matches the project label **and** a working-dir key equal
  to `TargetKeyCompose(WorkDir)` or `TargetKeyCompose(R)`. The second key covers
  the label compose writes after running at R.
- Lock keys and `markerKey` stay on `WorkDir`.
- **Why pin the project:** without `--project-name`, a symlinked directory with
  a different basename would create a duplicate project.

### AD-007: Stack target resolution (FR-006, FR-007)

- `updateTarget` gains `Project`.
- `dockerUpdate` requires one of `container`, `container_id`, `compose_project`
  or `compose_work_dir`. An empty string counts as absent.
- Resolution uses only the **compose sources** of the snapshot (AD-013), so a
  container with an image-defined label cannot cause `ambiguous_project`.

| Input | Steps |
|---|---|
| `P`+`W` | `W` must be absolute → lock `TargetKeyCompose(W)` → snapshot → resolve `P` to `D` → `Clean(W)==Clean(D)`, else `work_dir_mismatch` |
| `P` only | snapshot → resolve `P` to `D` → lock `TargetKeyCompose(D)` |
| `W` only | `W` must be absolute → lock `TargetKeyCompose(W)` → snapshot → `projectsForDir(W)` must be exactly one, which becomes `P`; `D=W` |

- **Resolve `P`:**
  1. `P` must match the project charset, else `invalid_project`.
  2. Take the compose sources whose project label is `P`.
  3. Their distinct working-dir keys must number exactly one, else
     `project_not_found` (0) or `ambiguous_project` (>1). `D` is the label
     value, verbatim.
- The last step calls `composeUpdate(composeSpec{WorkDir: D, Project: P})`.
- `dockerUpdateOne` passes the container's own project label and
  `SourceID = container ID`. Bulk updates go through `dockerUpdateOne`.
- `markerTarget` adds `compose_project` as its last fallback key.

### AD-008: `docker.compose_roots` (FR-005)

- `DockerConfig.ComposeRoots []string`, with tag
  `yaml:"compose_roots,omitempty"`. `LoadConfig` calls
  `normalizeComposeRoots()`: each entry must be non-empty and absolute, and is
  stored cleaned.
- An invalid entry does not fail the load. It is logged at Error level and kept
  in the unexported `rootsErr`. Every `Check` then refuses with
  `compose_roots_invalid`, and the rest of the agent keeps running.
- An empty list counts as unset.
- Roots go through `EvalSymlinks` at check time. A root that fails to resolve is
  skipped.
- **Containment is separator-aware.** The test is `rel := filepath.Rel(root, p)`
  followed by `rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+sep))`.
  So `/opt/stacks-x` is not inside `/opt/stacks`, and `/opt/stacks/..foo` is
  inside. On Windows both sides are lower-cased after resolution.
- Containment applies to D and to every `COMPOSE_FILE` entry
  (`outside_compose_roots`).
- The sample config gains the commented line `# compose_roots: []`.

### AD-009: Windows (FR-011, C-2)

- If `compose_roots` is unset, every `Check` refuses with
  `windows_requires_roots`, right after `not_absolute`. The doctor prints
  `compose updates on Windows require docker.compose_roots (ownership cannot be
  verified)`.
- When roots are set, these still apply: resolution and the component type
  check (C-7), roots compared case-insensitively, provenance (AD-013),
  container-writable (AD-014), AD-005 placement and the `-f` set, and the
  project mapping.
- Owner, mode and ACL checks are skipped.

### AD-010: Sanitized copy and recovery (FR-001, AC-013)

- **Write:** only `sanitizeComposeFile(td)` writes the copy, after check #2. It
  sanitizes `td.Files()[0]` and writes `<td.Path()>/.compose.riot-sanitized.yml`.
  The retry passes only that file, which is today's behavior: the other `-f`
  files are dropped on retry, as before.
- **`reconcileInterrupted`:** runs a snapshot and
  `Check(Dir: filepath.Dir(rec.SanitizedFile), Project: "")` before
  `removeSanitized`. On refusal it skips the removal and adds a summary line.
- **`cleanupSanitizedLeftovers`:** iterates over the compose sources only, runs
  `Check(Dir: label, Project: label)` on each, skips a refused directory, then
  calls `removeLeftoverSanitized(td.Path())`.
- With `Project: ""`, `Check` uses the unique accepted project for the
  directory, and zero or more than one is a refusal.

### AD-011: Server dispatch and keys (FR-008)

- **`checkAutoUpdates` stack branch:**
  - Sends `{"compose_project": project, "compose_work_dir": workDir}`.
  - **New:** if the telemetry containers carrying the project label show more
    than one distinct working-dir key, the stack is not dispatched, with a Warn
    log. This narrows the INFO-4 old-agent redirect. A new agent would refuse
    the stack as ambiguous anyway.
  - The container branch is unchanged.
- **`resolveDockerUpdateKeys`:**
  1. `compose_work_dir` maps to `TargetKeyCompose(wd)`.
  2. Otherwise `compose_project` maps to the unique working-dir key in
     telemetry, or `nil` when it is not unique.
  3. Otherwise `container` / `container_id`.
- **Dashboard:** `ComposeStackSection` sends
  `{ compose_project: stack.name, compose_work_dir: stack.workDir }`.

### AD-012: Doctor (FR-010)

`checkComposeTrust(cfg)` sits inside the Docker section. It builds
`sdkDocker`, loads a snapshot with a 10s timeout, and calls
`reportComposeTrust(cfg DockerConfig, snap daemonSnapshot, snapErr error, t *composeTrust)`.
The report prints:

1. The roots line: not set, the list, or `fail` when invalid. On Windows with
   roots unset, a `fail` line with the `windows_requires_roots` explanation.
2. For each container excluded as a compose source:
   `warn("container %q ignored as a compose source: its image defines the compose working-dir label")`.
3. One `pass` or `fail` line per distinct (project, working-dir) pair among the
   compose sources, sorted by project. A `container_writable` failure names the
   container and the mount.
4. If the snapshot fails: `warn("cannot read docker state: …")`.

Doctor usually runs under `sudo` with euid 0. That is harmless, because `riot`
is trusted through the socket group anyway.

### AD-013: Daemon snapshot and label provenance (SEC-001a)

```go
type daemonSnapshot struct {
    DataRoot      string
    Containers    []container.Summary
    excluded      map[string]string // container ID → reason; absent = accepted compose source
    volumeDevices map[string]string // local volume name → bind device path
}
func loadDaemonSnapshot(ctx context.Context, api dockerAPI) (daemonSnapshot, error)
```

`loadDaemonSnapshot` runs within `DockerPrecheckBudget`:

1. `Info`
2. `ContainerList(All: true)`
3. For every container with a working-dir label: `ImageInspect(c.ImageID)`,
   cached per image ID.
4. For every local-driver volume mount: `VolumeInspect`, cached per name.

An `Info`, list or `VolumeInspect` error fails the whole snapshot. The caller
returns `daemon_unavailable`.

**Provenance rule.** A container is **not** a compose source when its image's
`Config.Labels` defines `com.docker.compose.project.working_dir`, or when its
image cannot be inspected (fail closed). Compose-built images do not carry this
label (§2, verified), so `build:` stacks are unaffected.

Non-sources are ignored everywhere labels are read:

- P/W resolution
- `projectsForDir`
- sanitized-leftover cleanup
- doctor

When the target or dependent itself is a non-source (`SourceID`), the request
is refused with `label_provenance`.

**Project mapping (C-4, SEC-006).** Every `Check` requires the following.
`projectsForDir(Dir or R)` is the set of distinct project labels among the
compose sources whose working-dir key matches Dir or R. It must be exactly
`{Project}`: zero is `project_not_found`, more than one is `ambiguous_project`,
and a different one is `project_mismatch`.

### AD-014: Container-writable locations (SEC-001b): refuse

R and every checked file (resolved, including `COMPOSE_FILE` entries) are
refused when they sit at or under either of these:

- **`docker_data_root`:** the resolved `DataRoot`.
- **`container_writable`:** a *writable source* of any container in the
  snapshot, running or stopped. Writable sources are:
  - the `Source` of every mount with `RW == true` and `Type` bind or volume;
  - the `device` option of a local volume whose `o` option contains `bind`.

**Exemptions and details:**

- **Exempt containers.** A container is exempt when it mounts the Docker socket:
  any mount whose resolved `Source` equals the resolved socket path or is an
  ancestor directory of it. Such a container is already root-equivalent.
- **Path resolution.** Sources are resolved with `EvalSymlinks`, falling back
  to `Clean` when resolution fails (for example under the `0710` data root).
  Containment uses AD-008's separator-aware test.
- **Direction.** Only "at or under a source" is refused. A stack directory that
  is an **ancestor** of a bind source (`./config:/config`, source `D/config`)
  stays accepted, because neither D nor its compose files sit under `D/config`.
  A stack that mounts `.` read-write (`.:/app`) is refused.
- **Message.** The detail names the container and the mount:
  `%q is under %q, the read-write mount (%q → %q) of container %q`.

### AD-015: `COMPOSE_FILE` handling (SEC-002)

- **Mechanism.** Explicit `-f` tokens (AD-001) make compose ignore
  `COMPOSE_FILE` from `.env` and from the environment. To keep each stack's
  intended configuration, the agent reads `COMPOSE_FILE` and
  `COMPOSE_PATH_SEPARATOR` from the validated `.env` and turns them into `-f`
  tokens itself.
- **Parser, line by line.** Strip `\r`. A *candidate* is a line whose
  left-trimmed text starts with `COMPOSE_FILE`, `COMPOSE_PATH_SEPARATOR`, or
  `export ` followed by either key. A candidate must match
  `^(COMPOSE_FILE|COMPOSE_PATH_SEPARATOR)=(V)$`, where V is one of:
  - unquoted: `[^\s"'$\\#` + "`" + `]+`
  - single-quoted: `'[^'\n]*'` followed only by optional whitespace
  - double-quoted: `"[^"$\\` + "`" + `]*"` followed only by optional whitespace
- **Refusals (`env_unparsed`).** These all refuse:
  - any other candidate form: `export`, a space before `=`, interpolation,
    backslash or backtick escapes, an inline `#`, or an unterminated quote
  - a duplicate key
  - an empty value
  - an empty entry after splitting
- **Splitting.** Entries split on `COMPOSE_PATH_SEPARATOR` when it is set, else
  on `os.PathListSeparator` (`:` on Unix, `;` on Windows). A relative entry is
  joined to D.
- **Checks per entry, on the resolved path:**
  - the file rule, plus the component walk of its directory chain (leaf rule for
    its own directory; the 9-name `symlink_escape` rule does not apply here)
  - AD-014
  - roots
- **Mis-parse is functional only.** If the parser reads `.env` differently from
  compose (for example a line inside another key's multi-line value), security
  is unaffected: compose loads only the `-f` files, all of which were validated.
  The worst case is a wrong file set, which shows up as a compose error.
- **Messages** name only the offending path and rule, never `.env` content.

### AD-016: Pinned compose child environment (C-6)

`composeEnv(base []string, socketPath, goos string) []string` builds the child
environment:

- Start from `os.Environ()`.
- Drop every key that starts with `COMPOSE_`, and drop `DOCKER_HOST` and
  `DOCKER_CONTEXT`.
- Add `DOCKER_HOST=unix://<SocketPath or /var/run/docker.sock>`. On Windows add
  `npipe://<SocketPath>` only when `SocketPath` is set.
- Keep `DOCKER_CONFIG` and `HOME`, which registry auth needs.

`runCompose` and `recreateComposeDependent` pass this environment through
`newProc(name, env, args...)`, and `newExecProcess` sets `cmd.Env` when the
environment is not nil.

### AD-017: Standalone recreate drops the working-dir label (provenance laundering)

`standaloneCore` passes `ContainerCreate` a copy of `insp.Config` whose `Labels`
map is copied and has `com.docker.compose.project.working_dir` deleted.

**Rationale:** Docker merges image labels into the container config. Without
this, a standalone recreate would copy an image-defined working-dir label into
the replacement's explicit config. A later image tag could then drop the label
and pass the provenance rule.

A standalone container is, by definition, not a complete compose triple, so the
label has no function on it. This is the only change to the standalone path
(FRD §6 is amended).

## 4. Component Changes

| Action | File Path | Purpose |
|---|---|---|
| CREATE | `internal/agent/compose_trust.go` | `trustedDir`, `trustRequest`, `trustFS`, `fileMeta`, `composeTrust`, `Check`, account-db parse, `.env` parser, `composeTrustError`, rules, `composeProjectRe`, `within()` |
| CREATE | `internal/agent/compose_snapshot.go` | `daemonSnapshot`, `loadDaemonSnapshot`, provenance, `projectsForDir`, writable sources, socket exemption |
| CREATE | `internal/agent/compose_trust_unix.go` (`!windows`) | `osTrustFS`: `Stat_t`, `Getxattr` (Linux), error-returning ACL check on other Unix |
| CREATE | `internal/agent/compose_trust_windows.go` | `osTrustFS` without owner or ACL |
| CREATE | `internal/agent/compose_trust_test.go` | Validator unit tests (§8) |
| CREATE | `internal/agent/compose_snapshot_test.go` | Snapshot, provenance and writable-source tests |
| CREATE | `internal/agent/compose_trust_unix_test.go` (`!windows`) | Real `osTrustFS` smoke test (owner UID of a temp dir, `HasAccessACL` false on a fresh dir) |
| CREATE | `internal/agent/docker_update_trust_test.go` | Harness tests (§8) |
| MODIFY | `internal/agent/docker_api.go` | `dockerAPI` gains `Info`, `VolumeInspect`; `newProc` gains `env` |
| MODIFY | `internal/agent/proclimit.go` | `newExecProcess(name, env, args...)` |
| MODIFY | `internal/agent/docker_update.go` | `updateTarget.Project`, required params, `dockerUpdateOne` (project, `SourceID`), `updateStack` (AD-007), `composeSpec`, `composeArgs`, `projectContainers`, `composeCore` checks #1–#3, `runCompose` env, `standaloneCore` label strip (AD-017) |
| MODIFY | `internal/agent/docker_deps.go` | `recreateComposeDependent(ctx, api, insp)` with snapshot, `Check` and env |
| MODIFY | `internal/agent/compose_sanitize.go` | `sanitizeComposeFile(td)` uses `td.Files()[0]` |
| MODIFY | `internal/agent/docker_recovery.go` | Recovery is gated by snapshot and `Check` (AD-010) |
| MODIFY | `internal/agent/command_lifecycle.go` | `markerTarget` adds `compose_project` |
| MODIFY | `internal/agent/agent.go` | Seams `trustFS`, `trustPlatform`; new `procFactory` signature |
| MODIFY | `internal/agent/config.go` | `ComposeRoots`, `rootsErr`, `normalizeComposeRoots`, template line |
| MODIFY | `internal/agent/doctor.go` | `checkComposeTrust`, `reportComposeTrust` |
| MODIFY | `internal/agent/fakes_docker_test.go` | `rootOwnedFS`, `fakeTrustFS`, `fakeDocker.Info`/`VolumeInspect`/image labels, `fakeProcs` records env |
| MODIFY | `internal/agent/docker_deps_test.go`, `docker_update_test.go`, `docker_update_qa_test.go`, `compose_sanitize_test.go`, `docker_recovery_test.go`, `proclimit_test.go` | New signatures. Argv assertions gain `--project-name=stack` and explicit `-f=`. The old "no container has compose working directory" message becomes `project_not_found`. |
| MODIFY | `internal/agent/config_test.go`, `doctor_test.go` | AC-008, AC-016, AC-018, AC-021 |
| MODIFY | `internal/server/handlers/auto_update.go` (+ `_test.go`) | Stack params; skip an ambiguous project. Replace `TestAutoUpdate_StackDispatchSendsComposeWorkDirOnly`. |
| MODIFY | `internal/server/commands/targets.go` (+ `_test.go`) | `compose_project` key fallback |
| MODIFY | `web/src/components/ComposeStackSection.tsx` (+ `.test.tsx`) | Send `compose_project`; update the assertion at about L197 |
| MODIFY | `README.md` | `docker.compose_roots`, the refusal rules, the UPG rule, the `.env` readability note, the Windows roots requirement, and INFO-1 (transitive `include`/`extends`/`env_file` trust) |
| MODIFY | `docs/requirements/AUTOUPD-HARDEN-001-frd.md` | FR-004, FR-011 and §6 amended; FR-012 and AC-018 to AC-028 added (done in this revision) |

No migration and no `models` change.

## 5. Data Model Changes

None in Postgres. The agent config gains `docker.compose_roots`, a list of
absolute paths, optional, shown commented in the template.

## 6. API / Interface Contract

**`docker_update` params:**

| Param | Type | Notes |
|---|---|---|
| `container`, `container_id` | string | unchanged |
| `compose_work_dir` | string, absolute | a cross-check alongside `compose_project`; on its own it is the old-server form |
| `compose_project` | string, `^[a-z0-9][a-z0-9_-]*$` | new; old agents ignore it |

**Refusal (C-5).** `status=error`, `step=precheck`, and the message
`compose directory refused (<rule>): <%q dir>: <detail>`.

- Every label-derived or filesystem-derived value is formatted with `%q`.
- The detail is limited to: the offending path, the rule, a numeric UID/GID, an
  octal mode, and the container name and mount.
- Errors from `Lstat`, `ReadFile` or `EvalSymlinks` collapse to
  `cannot stat %q`, `cannot read %q` and `cannot resolve %q`.
- Raw daemon errors go to the log only.

**Rules (closed set):**

- path shape: `not_absolute`, `windows_requires_roots`, `resolve`, `stat`,
  `not_directory`
- roots: `compose_roots_invalid`, `outside_compose_roots`
- project and provenance: `invalid_project`, `project_not_found`,
  `ambiguous_project`, `project_mismatch`, `work_dir_mismatch`,
  `label_provenance`
- daemon and containers: `daemon_unavailable`, `docker_data_root`,
  `container_writable`
- ownership and mode: `owner`, `group_writable`, `acl`, `world_writable`
- files: `symlink_escape`, `not_regular`, `no_compose_file`, `env_unreadable`,
  `env_unparsed`
- recheck: `dir_changed`

## 7. Sequence: stack update, new server to new agent

1. The server sends `{compose_project: P, compose_work_dir: W}` with key
   `docker:compose:W`.
2. Agent intake writes the marker.
3. The agent checks `W` is absolute and takes the lock.
4. It loads snapshot S1, resolves `P` among the compose sources to `D`, and
   cross-checks `W`.
5. `Check(S1, {D, P})` runs. The order inside `Check`:
   1. `not_absolute`
   2. `windows_requires_roots`
   3. `compose_roots_invalid`
   4. resolve
   5. component type walk
   6. roots
   7. project and provenance
   8. data root and container-writable
   9. ownership, mode and ACL walk
   10. the 9 names and `.env`
   11. the `-f` set
   12. `no_compose_file`

   It returns `td`.
6. Pull runs with `composeArgs(td, P, "", pull)`, using the pinned environment.
7. The sanitize retry, if needed: check #2, then the marker write, the sanitized
   copy, and the retry pull.
8. Check #3, which must equal check #1, else `dir_changed`. Then
   `stopDependents`, up, verify, and restore dependents. Each dependent
   recreate runs its own snapshot and `Check`.

## 8. Acceptance Criteria Mapping

Unit tests live in `compose_trust_test.go` and `compose_snapshot_test.go` and use
`fakeTrustFS` with `unix: true` unless noted. Harness tests live in
`docker_update_trust_test.go`.

| AC | Fulfilled By | Tests |
|---|---|---|
| AC-001 | `Check` `not_absolute`; `composeArgs` zero `td` | `TestComposeTrust_AC001_RelativeOrEmptyDirRefused`; `TestDockerUpdate_AC001_RelativeWorkDirStartsNoComposeProcess` |
| AC-002 | AD-006 | `TestComposeTrust_AC002_SymlinkedDirValidatedAtResolvedPath`; `TestComposeTrust_AC002_SymlinkToUntrustedTargetRefused`; `TestDockerUpdate_AC002_ComposeRunsAtResolvedPathWithPinnedProject`; `TestComposeCore_AC002_VerifyMatchesResolvedWorkingDirLabel` |
| AC-003 | AD-003 owner | `TestComposeTrust_AC003_UntrustedOwnerRefused`; `..._UntrustedAncestorOwnerRefused`; `..._UnknownUIDRefused`; `..._SocketUnavailableTrustsRootOnly`; `..._AccountDBUnreadableFailsClosed`; `..._MalformedPasswdFailsClosed` |
| AC-004 | AD-003 mode and sticky | `TestComposeTrust_AC004_WorldWritableDirRefused`; `..._StickyLeafStillRefused`; `..._WorldWritableStickyAncestorAccepted`; `..._WorldWritableNonStickyAncestorRefused`; `..._GroupWritableUntrustedGroupRefused`; `..._GroupWritableSocketGroupAccepted` |
| AC-005 | AD-003 trusted set | `TestComposeTrust_AC005_RootOwnedAccepted`; `..._SupplementarySocketMemberAccepted`; `..._PrimaryGroupSocketMemberAccepted`; `..._SocketOwnerAccepted` |
| AC-006 | AD-005 | `TestComposeTrust_AC006_NoDefaultFileNoComposeFileRefused`; `..._OverrideOnlyRefused`; `..._ComposeFileWithoutDefaultFileAccepted` |
| AC-007 | AD-005 | `TestComposeTrust_AC007_UntrustedOwnedFileRefused` (table over the 9 names); `..._UntrustedWritableFileRefused`; `..._FileSymlinkEscapeRefused`; `..._FileSymlinkInsideAccepted`; `..._NonRegularFileRefused` |
| AC-008 | AD-008 | `TestComposeTrust_AC008_OutsideRootsRefused`; `..._InsideRootAccepted`; `..._RootsUnsetNotEnforced`; `..._SymlinkedRootResolved`; `..._InvalidRootsFailClosed`; `..._RootPrefixIsSeparatorAware` (`/opt/stacks-x`, `/opt/stacks/..foo`); `TestLoadConfig_AC008_ComposeRootsParsedAndCleaned`; `TestLoadConfig_AC008_RelativeRootLoadsButSetsRootsErr` |
| AC-009 | AD-007 | `TestUpdateStack_AC009_ProjectResolvesToOneDir`; `..._ProjectNotFoundRefused`; `..._ProjectAmbiguousRefused`; `..._InvalidProjectNameRefused` |
| AC-010 | AD-007 | `TestUpdateStack_AC010_MismatchedWorkDirRefused`; `..._TrailingSlashWorkDirAccepted` |
| AC-011 | AD-007 | `TestUpdateStack_AC011_WorkDirAloneMapsToOneProject`; `..._WorkDirAloneTwoProjectsRefused`; `..._WorkDirAloneUntrustedRefused` |
| AC-012 | AD-001, AD-002 | `TestDockerUpdate_AC012_SingleServiceUntrustedLabelRefused`; `TestDockerBulkUpdate_AC012_UntrustedComposeMemberRefused`; `TestRestoreDependents_AC012_UntrustedDependentDirNotRecreated`; `TestComposeArgs_AC012_ZeroTrustedDirRefused` |
| AC-013 | AD-002, AD-010 | `TestComposeCore_AC013_SanitizedNotWrittenWhenRecheckFails`; `..._DirChangedBetweenChecksRefused`; `TestCleanupSanitized_AC013_SkipsRefusedDir`; `TestReconcile_AC013_SanitizedRemovalSkipsRefusedDir` |
| AC-014 | AD-011 | `TestAutoUpdate_AC014_StackDispatchSendsProjectAndWorkDir`; `..._ContainerPolicySendsNeither`; `..._AmbiguousProjectNotDispatched`; `TestResolveDockerUpdateKeys_AC014_ProjectOnlyMapsViaTelemetry`; `..._ProjectOnlyAmbiguousHasNoKey`; `..._WorkDirStillWins` |
| AC-015 | AD-011 | vitest `[AC-015] manual stack update sends compose_project and compose_work_dir` |
| AC-016 | AD-012 | `TestDoctor_AC016_ReportComposeTrustRootsAndPerProjectLines`; `..._InvalidRootsFail`; `..._DockerUnreachableWarns`; `..._ContainerWritableNamesContainerAndMount` |
| AC-017 | §6 format | `TestComposeTrust_AC017_MessageNamesPathAndRule` (a table over every rule); `TestDockerUpdate_AC017_RefusalIsPrecheckWithRule` |
| AC-018 | AD-013, AD-017 (SEC-001a) | `TestSnapshot_AC018_ImageDefinedWorkingDirExcluded`; `..._ComposeBuiltImageLabelsAccepted` (image has project/service/version only); `..._ImageInspectFailureExcludes`; `TestDockerUpdate_AC018_SingleServiceImageLabelRefused`; `TestDockerBulkUpdate_AC018_ImageLabelMemberRefused`; `TestRestoreDependents_AC018_ImageLabelDependentNotRecreated`; `TestUpdateStack_AC018_ImageLabelContainerCannotCauseAmbiguity`; `TestCleanupSanitized_AC018_IgnoresImageLabelDirs`; `TestDoctor_AC018_ListsExcludedContainers`; `TestStandaloneUpdate_AC018_RecreateDropsWorkingDirLabel` |
| AC-019 | AD-014 (SEC-001b) | `TestComposeTrust_AC019_DirUnderRWBindSourceRefused` (message names container and mount); `..._DirEqualsRWBindSourceRefused` (`.:/app`); `..._StackDirAncestorOfBindSourceAccepted` (`./config:/config`); `..._ReadOnlyBindIgnored`; `..._StoppedContainerMountCounts`; `..._VolumeMountpointRefused`; `..._BindDeviceVolumeRefused`; `..._DockerDataRootRefused`; `..._SocketMountingContainerExempt`; `..._ComposeFileEntryUnderBindRefused`; `TestSnapshot_AC019_InfoOrListFailureIsDaemonUnavailable`; `TestSnapshot_AC019_VolumeInspectFailureFailsClosed` |
| AC-020 | AD-001, AD-005, AD-015 (SEC-002) | `TestComposeTrust_AC020_DefaultAndOverrideBecomeExplicitF`; `..._ComposeFileEntriesValidatedInOrder`; `..._ComposeFileEntryUntrustedRefused`; `..._ComposeFileEntryOutsideDirChainChecked`; `..._CustomPathSeparator`; `..._UnparseableComposeFileRefused` (table: `$`, `export`, a space before `=`, `\`, an inline `#`, an unterminated quote, a duplicate, an empty value, an empty entry); `..._EnvUnreadableRefusedWithGuidance` (`EACCES`); `..._MessageDoesNotEchoEnvContent`; `TestComposeArgs_AC020_ExplicitFTokensAndSanitizedReplaces` |
| AC-021 | AD-009 (C-2) | `TestComposeTrust_AC021_WindowsWithoutRootsRefused`; `..._WindowsRootsCaseInsensitive`; `..._WindowsSkipsOwnershipKeepsPlacement`; `TestDoctor_AC021_WindowsExplainsRootsRequirement` |
| AC-022 | AD-003 (C-1) | `TestComposeTrust_AC022_GroupWritableWithACLRefused`; `..._ACLReadErrorFailsClosed`; `..._NoGroupWriteSkipsACLLookup` |
| AC-023 | AD-003 (C-3) | `TestComposeTrust_AC023_SocketWithoutGroupRWDoesNotTrustMembers` |
| AC-024 | AD-003 UPG | `TestComposeTrust_AC024_UserPrivateGroupWritableAccepted` (dir 0775 and file 0664, owned by U:U, U in the socket group); `..._SharedGroupWithUntrustedMemberRefused`; `..._GroupWithUnknownMemberNameRefused`; `..._GroupWithoutFileEntryRefused`; `..._NonFilesNSSDisablesMemberRule` |
| AC-025 | AD-013 (C-4) | `TestComposeTrust_AC025_DirMappedToOtherProjectRefused`; `TestDockerUpdate_AC025_SingleServiceProjectMustOwnDir`; `TestRestoreDependents_AC025_DependentProjectMismatchRefused` |
| AC-026 | AD-016 (C-6) | `TestComposeEnv_AC026_PinsDockerHostAndDropsComposeVars`; `TestDockerUpdate_AC026_ComposeProcessGetsPinnedEnv` |
| AC-027 | §6 (C-5) | `TestComposeTrust_AC027_ControlCharsQuotedAndErrnoCollapsed` |
| AC-028 | AD-003 (C-7) | `TestComposeTrust_AC028_SymlinkComponentRefused`; `..._NonDirectoryComponentRefused` |

## 9. Error Handling

| Failure | Behavior |
|---|---|
| Any `Check` refusal | `precheck` error and a Warn log (rule plus `%q` path). Auto-update backoff treats it as a failed run. |
| Snapshot failure | `daemon_unavailable` refusal; the raw error goes to the log only |
| Refusal in a dependent recreate | Restore failure, so the command fails at `restart_dependents` |
| Refusal in recovery | Skip, add a summary line or a Debug log, continue |
| Invalid `compose_roots` | Error log at load; every check fails closed; the agent stays up |

## 10. Security Considerations

**Closed by this story:**

- **Host accounts.** No account without Docker rights can write D, any ancestor
  (except through a sticky ancestor), or any file compose loads. This includes
  writes through a group or an ACL (AD-003).
- **Containers and images.**
  - A container whose image defines the working-dir label is never a compose
    source (AD-013), and the standalone recreate cannot launder that label
    (AD-017).
  - No compose directory or loaded file may sit in a location any container can
    write through a read-write mount or volume, or in the Docker data root.
    Socket-mounting containers are the exception (AD-014).
- **Load set.** Compose loads exactly the validated `-f` set. `COMPOSE_FILE` and
  the parent walk cannot add files (AD-015).
- **Server.** The server's path is only a cross-check. Every path maps to exactly
  one provenance-accepted project, which equals `--project-name`.

**Residual risks:**

- **R-1, old agents.** An agent released before this story ignores
  `compose_project` and stays fully exposed. AD-011's ambiguity skip narrows the
  redirect but does not close it. There is no version gating (orchestrator
  decision).
- **R-2, transitive references and TOCTOU.**
  - `include`, `extends`, `env_file` and build contexts named by trusted files
    are trusted transitively (INFO-1, README).
  - A Docker-rights account changing things between the check and the exec is a
    non-goal; `dir_changed` catches path changes.
  - A container could gain a new read-write mount between the snapshot and the
    exec. That needs Docker rights.
- **R-3, Windows.** There is no ownership check. Mount sources from Docker
  Desktop's VM rarely match Windows paths, so AD-014 is weak there. The
  required `compose_roots` setting is the control.
- **R-4, label spoofing remnants.** A container that copies the labels of a
  trusted project at runtime needs `docker run --label`, which means Docker
  rights. An image-sourced copy is excluded. Images can still carry a forged
  `com.docker.compose.project`, but without the working-dir label they are not
  sources.
- **R-5, not evaluated.**
  - Linux capabilities.
  - FUSE mounts with `user_allow_other` (SEC-008, needs a non-default
    `fuse.conf`).
  - NSS-only accounts (fail closed).
  - Containers with `privileged` or `CAP_SYS_ADMIN` but no socket mount, which
    can write anywhere. AD-014 only sees their declared mounts.

## 11. Performance Considerations

A snapshot costs one `Info`, one list, one `ImageInspect` per distinct image of
a working-dir-labelled container, and one `VolumeInspect` per distinct local
volume, all on the local socket. It runs at most 3 times per compose command,
plus once per dependent recreate. The account files are parsed once per
`Check`. There is no cache across calls. The cost is negligible next to a pull.

## 12. Implementation Notes for Engineers

- Use `path/filepath` everywhere, never `path`, and rely on `within()` for every
  containment test.
- Copy compose-go `DefaultFileNames` and `DefaultOverrideFileNames` verbatim from
  the installed compose version, and cite the version in the report.
- **Verify on a real host** and record in the report:
  1. `-f` overrides `COMPOSE_FILE` from `.env`.
  2. With `--project-directory`, `.env` is still read from the project
     directory.
  3. `--project-name=` and repeated `-f=` are accepted with `pull` and `up`.
- `-f` paths are passed resolved. An `extends`/`include` that is relative to a
  *symlinked* compose file therefore resolves against the target's directory.
  This edge is accepted.
- `ImageInspect` takes `c.ImageID` (the content ID), not the tag.
- `.env` parsing must never place `.env` bytes in an error, a log or a test
  failure message.
- Under `ProtectSystem=strict` the sanitized copy usually cannot be written
  (INFO-5). This is unchanged and out of scope.
- Test fixtures use generic paths (`/srv/media`, `/opt/stacks`). The repo is
  public.

## 13. Definition of Done

- [ ] All Section 4 changes implemented, including the FRD amendments
- [ ] Every test named in Section 8 exists and passes
- [ ] `go test ./...` passes on Windows and `go test -race ./...` on Linux CI
- [ ] `npm run test:run` passes
- [ ] `go vet` and lint are clean
- [ ] `composeArgs` has exactly 4 call sites, each fed by a `Check` result
- [ ] `trustedDir{` appears only in `compose_trust.go` and tests
- [ ] Every compose child is built with `composeEnv`
- [ ] Section 12 verification items recorded in the implementation report
- [ ] Doctor output reviewed on a Linux host with these stacks:
  - a trusted stack
  - a UPG stack
  - a `.:/app` stack
  - an image-label container
- [ ] README updated (Section 4)
- [ ] No migration
