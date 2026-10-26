# Architecture Decision Document

- **Story ID:** EXTRA-UPD-001
- **FRD Reference:** `docs/requirements/EXTRA-UPD-001-frd.md` (revision 2)
- **Related ADDs:** MAINT-RECOVERY-001, PATCH-FLOW-001, REBOOT-DPKG-001, SHUTDOWN-SUDO-001
- **Author:** Architect Agent
- **Date:** 2026-10-26
- **Status:** DRAFT, revision 1. Ryan was not reachable. Every open question is decided here on Ryan's behalf and
  listed in §0.3 as a "Decision for Ryan's review" with the recommended default the ADD is written to.

---

## 0. Answers, flags and decisions

### 0.1 Answers to FRD §11.2

| Item | Answer | Where |
|---|---|---|
| **OQ-A1** evidence of a completed DietPi check with nothing found | **None exists.** Verified in DietPi @65a12a6: `dietpi-update 2` writes `/run/dietpi/.update_available` only when an update exists and *removes* it otherwise (`Check_DietPi_Update` L155–176); `G_AGUP -f` likewise removes `.apt_updates` when the count is 0; no timestamp or marker is left. **Row D3 cannot occur in production.** A DietPi host with no flag and `CONFIG_CHECK_DIETPI_UPDATES=1` reports `unknown` / `not_checked_since_boot`. The detector keeps a `CheckEvidence` seam (production: always false) so D3 is unit-tested and can be enabled if DietPi ever leaves a marker | AD-004 |
| **OQ-A2** how the root-side opt-in reaches the privileged path | A **root-owned policy file** `/etc/riot-maintenance/extra-updaters.json` written only by `install.sh`, read and enforced by the root worker. The agent's request (new protocol v2) can only *ask* for an updater; the worker intersects the ask with the policy. **No sudoers line is added.** | AD-001, AD-002, AD-003 |
| **OQ-A3** bounds | DietPi step 40 min; start threshold 47 min 30 s; worker 30 min apt + 80 min overall when extended; service backstop 85 min (drop-in, opted-in hosts only); agent hard max 88 min; command deadline 95 min. Hosts without an enabled updater keep 30/35/31m30s/40 min. The host reservation (host target key) lasts the whole run, up to about 87 min on a DietPi host; EXPECT-001 will read the same `commands` row (dispatched → terminal) as the expected-downtime window | AD-006 |
| **OQ-A4** process-tree termination and the DietPi lock dir | Process-group SIGTERM at the step limit, SIGKILL after 90 s, then `KillMode=control-group` when the service exits. SIGTERM runs DietPi's `G_EXIT` trap, which removes `/tmp/DietPi-Update`. After a SIGKILL the dir stays, but the **next** `dietpi-update` removes it itself when no other instance runs (`G_INIT` L111–114). rIOt reports `timed_out` with "DietPi state uncertain" and never touches the dir. The unit must keep a shared `/tmp` (no `PrivateTmp`), or DietPi's concurrency guard would not see the cron check | AD-007 |
| **OQ-A5** scheduled reboot signal without widening apt-only runs | The worker records `reboot_required_after` per updater. The agent adds the pseudo-entry `dietpi` to `RebootClassApplied` **only when** the record carries `include_reboot_class: true` (the scheduler sets it only under `reboot_class: gated`), the overall outcome is success, the updater ran with `success`, and `reboot_required_after` is true. Apt-only runs never get the entry, so `maybeRebootAfterPatch` is unchanged for them | AD-009 |
| **OQ-A8** can telemetry tell a non-rIOt hold has an update | **Yes, without an agent change.** `apt list --upgradable` lists held packages. NY-5 is an action item when lists are not `stale` and a name in `other_holds.packages` (minus rIOt-managed holds) also appears in `updates[].name`. No intersection, a truncated list, stale lists, dnf, or `other_holds` unavailable ⇒ information | AD-012 |
| **OQ-A6 / OQ-A7** (security) | Proposed: accept, with a transparency line printed on every Linux reinstall (§10). Goes to the security reviewer | §10 |

Assumptions: **A-002 confirmed** (`dietpi-update 1` with no version update runs `Check_Live_Patches` → `Apply_Live_Patches`,
L507–513; not when the update source is non-default, L188). **A-004 confirmed** (`rm /run/dietpi/.update_available` after
the run, L469, and the version is saved). **A-007 confirmed** (only `FleetOverview.tsx` L47 counts `kind === 'needs_you'`).
A-003 and A-005 cannot be verified without SSH to the Pis; doctor reports A-003, and A-005 does not affect correctness
(the worker is a separate service; a restarted agent recovers the result).

### 0.2 FRD deviations and clarifications (architect flags)

| Flag | FRD text | Decision |
|---|---|---|
| **AF-1** | FR-006 / FR-010 / AC-006 row D3 | D3 is unreachable in production (OQ-A1). AC-006 D3 is tested through the `CheckEvidence` seam |
| **AF-2** | FR-052 needs an information item for `unknown`; FR-051 needs the "how applied" text for pending entries | Two information-only codes are added: `extra_update_unknown` and `extra_update_pending`. FRD §7 named only `extra_update_not_applied` |
| **AF-3** | FR-079 / AC-055 "each reason code at most once" vs NFR-002 "one `extra_update_not_applied` item per updater entry" | Uniqueness is on (code, updater). Each badge item gains an optional `updater` field. Legacy codes NY-1..NY-10 stay unique by code |
| **AF-4** | FR-066 "on hosts with no enabled updater ... outputs must not change" vs AC-012 / AC-022 (skip lines on DietPi hosts that are not enabled) | FR-066 is read as "no updater **present**". hawaii and vermont are byte-identical. A DietPi host that is not enabled gets a skip line in its result (AC-012, AC-022) and still sends the unchanged v1 request |
| **AF-5** | FR-045 events carry the origin | The agent sends command ID, updater and outcome. The **server** resolves the origin from the command row and writes it into the stored event. An agent cannot claim an origin |
| **AF-6** | AC-019 "the policy listing refuses `dietpi-update` with any other argument" | No sudoers line exists, so the listing refuses **every** DietPi program and argument, including `dietpi-update 1`. The test asserts exactly that. The exact-argv rule is enforced by the worker allowlist (Go test) |
| **AF-7** | FR-038 lists the reasons that make the overall outcome `failed` | Adds one agent-side case: an enabled, pending updater the command could not run because the server did not extend the deadline (old server, or telemetry not yet showing `enabled`). It is recorded `skipped (insufficient_time)` and the overall outcome is `failed`. This is truthful, and it is rare (§0.3 D-6) |

### 0.3 Decisions for Ryan's review

1. **D-1: Run DietPi inside the existing root worker** (`riot-maintenance`). Do not add a sudoers line. Opt-in is a root-owned policy file plus a unit drop-in, both written by `install.sh`. *Default: as described (AD-001).*
2. **D-2: Bounds.** DietPi gets 40 min. On opted-in hosts only: worker 80 min, service backstop 85 min, command deadline 95 min. Ryan should give PiVPN and PiCUPS a patch window of at least 90 minutes, because the run may continue past the window end (same as today, but longer). *Default: these values (AD-006).*
3. **D-3: A DietPi host without a pending update shows `unknown`, never "up to date"** (OQ-A1). It is neutral information in the badge. *Default: accept.*
4. **D-4: A failed, timed-out or interrupted DietPi step makes the command `failed`, so the "reboot if required" option does not reboot** (PATCH-FLOW-001 rule "run not successful"), even when a reboot is now required. NY-10 then asks Ryan to reboot. *Default: keep. DietPi's state is uncertain after a failure.*
5. **D-5: Two information-only badge codes** `extra_update_pending` and `extra_update_unknown` (AF-2), and per-updater uniqueness (AF-3). *Default: accept.*
6. **D-6: The agent never extends its own run without the server's budget stamp** (AF-7). *Default: record `insufficient_time` and fail. Alternative: treat it as `not_enabled` and stay neutral (hides the miss).*
7. **D-7: Fleet "Bulk patch" also patches hosts whose only pending update is an enabled extra updater.** *Default: yes (consistent with FR-055).*
8. **D-8: Installer leniency.** A misspelled `allow_extra_updaters` value (quoted items, unknown shape) records no updaters but does **not** reject the rest of the file. The shutdown grant is unaffected. Structural problems still reject the whole file, as today. *Default: accept.*
9. **D-9: Nothing new on hawaii and vermont.** The policy file and the unit drop-in are written only when an updater is recorded, or when they already exist (they are then rewritten, never deleted). *Default: accept.*

---

## 1. Summary

A new package `internal/extraupd` defines the extra-updater contract: a read-only detector per updater, plus one fixed,
compile-time apply spec. DietPi is the only entry. The `updates` collector reports detections. The agent stamps each
entry's readiness from its config, the installed helper's capabilities and the root policy file. The root worker
`riot-maintenance` gains **request protocol v2**. After the apt part has finished and the holds have been restored, it
re-detects each requested updater. It runs `/boot/dietpi/dietpi-update 1` at most once, and only if the root policy
lists the updater, the update is pending, enough time is left and the dpkg lock is free. The step has fixed argv, env
and cwd, null stdin, a 40-minute limit, process-group kill and a 128 KiB output section. The record carries a truthful
outcome per updater. Reboots go only through the existing PATCH-FLOW-001 / PATCH-GATE gate. On the server, the scheduler
also fires for enabled pending updaters, and the dispatcher extends the deadline only for hosts with an enabled updater.
The patch badge follows group K: "Needs you" only when work is waiting, a new neutral `manual` kind, and a separate list
of information items.

## 2. Technical Context (verified at `186a575`)

- **Root worker** (`internal/maintenance`, `cmd/riot-maintenance`):
  - `Request` is a closed four-field object (`ParseRequest` rejects unknown, missing, duplicate, null and trailing fields), `ProtocolVersion = 1`.
  - `Record` is parsed with `DisallowUnknownFields` by both the worker and the agent (`parseRecord`).
  - `Execute` runs hold release → configure/audit/check (+ one repair) → refresh → upgrade → verify → `post_refresh` under `WorkerBudget = 30m`. Hold restoration, `Changes` and finalisation happen in **one deferred finisher** with a 2-minute `CleanupBudget`, which also calls `Store.Finish`.
  - `ExecRunner` enforces `CommandAllowed` (exact argv), `FixedEnvironment()`, `Dir=/`, null stdin, `Setpgid`, SIGTERM→SIGKILL after `TerminationGrace = 90s`, and treats a descendant outliving its leader as an error *immediately*.
  - `Store.Begin` refuses a command ID that was already recorded or completed. `RecoverInterrupted` only restores holds.
- **Service** `scripts/riot-maintenance.service`: `RuntimeMaxSec=35min`, `KillMode=control-group`, no `PrivateTmp`. The agent's readiness check (`maintenanceUnitPolicy`) pins `RuntimeMaxUSec` to 35 min.
- **Protocol constant reuse:** `ProtocolVersion` (1) is also used by `UpdateStatus` (`update.json`) and printed by `riot-agent-apply --maintenance-protocol`, which agent readiness requires to be `1`. **It must not change.**
- **Installer** (`scripts/install.sh`):
  - The awk classifier `config_enables_shutdown` (L203–316) rejects the whole file when any line under `commands:` is not `key: scalar`. A list value such as `allow_extra_updaters: [dietpi]` would therefore **silently drop the shutdown grant today**. The classifier must learn the list shapes.
  - The Python validator of `current.json` (L601–628) asserts `version == 1`. After a DietPi run it would block every reinstall unless it is updated.
  - The sudoers drop-in is pinned by the golden `PRE_STORY_DROPIN` in `scripts/test_install_maintenance.py`.
- **Agent:**
  - `runDetachedMaintenance` writes the request and starts the unit. `observeMaintenance` polls each second and emits `StepVerify` progress.
  - `recoverMaintenanceCommand` re-attaches after a restart.
  - `newCmdRun` bounds the command context with `AgentHardMax` (31m30s for `os_update`). The observer itself runs on `commandContext()`.
  - `collectors.RebootRequiredNow` is the single reboot-required function, shared by the collector and the authorized-reboot decision.
  - Telemetry stamps `allow_*` facts in `telemetry.go`.
- **Events:** `models.AgentEvent` has only type, severity and message. `ReceiveAgentEvent` accepts only the four `agent_update_*` types. Note: the agent's existing `command_completed` agent events are rejected with 400 today. That is pre-existing and out of scope.
- **Server:**
  - `commands.Service.Dispatch` stamps `ExecDeadlineS` from the static `models.ExecDeadline(action)`, and it already has a `TelemetryReader`.
  - `checkAutoPatch` returns early unless `PendingUpdates > 0`.
  - `computePatchBadge` puts every raised reason into `needs_you`, and the kind is `needs_you` whenever any reason exists.
- **Web:** the badge types are in `web/src/api/client.ts` (not `types/models.ts`). `PatchBadge.tsx` keys list items by `code`.
- **DietPi:** facts as in FRD §2, re-verified in source. Additional facts that matter here:
  - `G_INIT` forces its own `PATH`, `LC_ALL=C.UTF-8` and `umask 0022`.
  - The concurrency guard uses `/tmp/DietPi-Update`.
  - The run's own output is duplicated to `/var/lib/dietpi/logs/dietpi-update.log` via `tee` in a process substitution. The `tee` can outlive the leader by a moment, so the existing "descendant outlived parent" rule would turn a good run into a failure.

---

## 3. Architecture Decisions

### AD-001: The DietPi step runs inside the root maintenance worker

**Decision.** `riot-maintenance` runs the extra-updater phase within the same service start as the apt part, after the apt
part's hold restoration. The agent never executes `dietpi-update`. No sudoers line is added; the existing single grant
`systemctl start --no-block riot-maintenance.service` is the only path.

**Rationale.** The worker already has every property FR-029 to FR-040 need:
- it survives agent restarts and the agent's 50 % memory cap;
- it holds the exclusive maintenance lock;
- it writes a durable, root-owned record with persist-before-act phases and once-per-command (`Store.Begin`);
- its output is captured and capped, and it has a cgroup backstop.

Running DietPi after apt in the same transaction also gives the ordering of FR-025 for free.

**Alternatives considered.**
- *Exact sudoers line `/boot/dietpi/dietpi-update 1` run by the agent:* rejected. The run would die with agent restarts or self-updates, would run inside the agent cgroup and memory cap, would need a second durable-state machine for at-most-once, would add a sudoers line (FR-021 allows one, but NFR-001 prefers none), and would race the apt worker for the lock.
- *A second unit `riot-dietpi.service`:* rejected. It needs a second sudo start grant and a second record store, and ordering after hold restoration would cross two services.

**Consequences.** The worker restructures `Execute` (AD-005) and its record gains fields (AD-002). The longest worker run
grows on opted-in hosts only (AD-006).

### AD-002: Request and record protocol v2, negotiated by a helper capability probe

**Decision.**
- `ProtocolVersion` stays `1` (it is shared with `UpdateStatus` and `--maintenance-protocol`).
- A new constant `RequestVersionExtra = 2` adds request field `extra_updaters` (a non-empty list of identifiers).
- `ParseRequest`:
  - version 1 needs exactly the four existing keys;
  - version 2 needs exactly those four plus `extra_updaters`, mode `full` or `security`, 1–8 unique identifiers that the worker registry implements, listed in registry order.
- `WriteRequest` of a v1 request is byte-identical to today (the new struct field is `omitempty` and nil).
- `Record` gains three `omitempty` fields: `requested_extra_updaters`, `apt_outcome` and `extra_updaters` (§5). They must be empty on version-1 records.
- The agent sends v2 only when `riot-maintenance --capabilities` (a new no-root, no-file, no-lock mode, run as `riot`) prints `request-versions=1,2` and an `extra-updaters=` line that lists the identifier. Old helpers exit 1 on any argument, so the agent knows the helper is old. The result is cached and keyed on the helper's (device, inode, size, mtime), so the probe runs at most once per installed helper.

| Combination | Behaviour |
|---|---|
| new agent + old helper | probe fails ⇒ readiness `unsupported_helper` ⇒ v1 request ⇒ apt as today; result line `dietpi: skipped (not_enabled)` |
| old agent + new helper | v1 request ⇒ identical v1 record (no new keys); the old agent's strict parser accepts it |
| new agent + new helper, updater not enabled | v1 request, identical to today |
| new agent + new helper, enabled, server budget stamped | v2 request |

**Rationale.**
- A version bump gated on an explicit capability is the only way the strict closed parsers on both sides stay strict.
- The probe tests the binary that is really installed. A marker file would only show which installer ran last.
- Running the helper as `riot` grants nothing, because it is not setuid. Readiness already runs `riot-agent-apply --maintenance-protocol` the same way.

**Alternatives considered.**
- *Bump `ProtocolVersion` to 2:* rejected. It breaks `UpdateStatus` and every old agent's readiness check (`--maintenance-protocol` must print `1`).
- *Infer capability from the policy file's presence:* rejected. A later run of an older installer leaves the file in place next to an old helper. The old worker would then reject the v2 inbox and write no record, so apt would not run either.

**Consequences.**
- `install.sh`'s `current.json` validator must accept version-2 records (§12 note 5).
- A *downgraded* helper cannot read v2 records left in its history, and refuses with "invalid retained maintenance record" until a root operator archives them. That is acceptable for a downgrade and is documented.

### AD-003: Root-side opt-in is a root-owned policy file plus a unit drop-in, written only by `install.sh`

**Decision.**
- `install.sh` derives the recorded list from the **final** agent config and from DietPi's presence (FR-019), then writes atomically:
  - `/etc/riot-maintenance/extra-updaters.json` (dir `root:root 0755`, file `root:root 0644`), content `{"version":1,"extra_updaters":["dietpi"]}` or `{"version":1,"extra_updaters":[]}`;
  - `/etc/systemd/system/riot-maintenance.service.d/50-riot-extra-updaters.conf` (`root:root 0644`), `[Service]` / `RuntimeMaxSec=85min` when the list is non-empty, `35min` when it is empty.
- Both files are written only when the list is non-empty or the file already exists (D-9). Revocation rewrites them with `[]` and `35min`; nothing is deleted.
- The worker enforces the file. The agent only reads it for readiness.

**Rationale.**
- The file lives outside `/etc/riot`, which the installer re-owns to `riot` (L937), and is never writable by `riot` (FR-019).
- A drop-in raises the systemd backstop only on opted-in hosts, so FR-032 and FR-066 hold on every other host.
- JSON keeps the parser closed and tiny on both sides. The file is world-readable so the unprivileged agent can compute readiness, and it holds no secret.

**Alternatives considered.**
- *A field in the request:* rejected. The request is written by `riot`, so a field there cannot be a root-side opt-in.
- *Changing the signed unit's `RuntimeMaxSec` for every host:* rejected by FR-032 and FR-066.
- *A file under `/etc/riot`:* rejected, because that directory is `riot`-owned.

**Worker parse rules** (`maintenance.ReadExtraPolicy`):
- Safe-open as in `io_linux.go`: `openParent(path, -1)`, so every parent is root-owned and not group- or world-writable (sticky `/tmp` style exceptions do not apply under `/etc`). Then `O_RDONLY|O_NOFOLLOW|O_NONBLOCK|O_CLOEXEC`, `validateFile(f, 0, 0644)` (regular file, uid 0, mode ⊆ 0644, `nlink == 1`), size ≤ **512 bytes**, valid UTF-8, and `rejectDuplicateKeys`.
- Exactly the keys `version` (must be 1) and `extra_updaters` (an array, ≤ 8 unique strings matching `^[a-z][a-z0-9_-]{0,31}$`), with no trailing JSON.
- Identifiers this build does not implement are ignored.
- **Any error means "no updater is root-enabled"** (fail closed). The error text goes into the updater line's note.

### AD-004: The generic contract, `internal/extraupd`

**Decision.** New package (no imports beyond `models` and the standard library):

```text
type Updater interface {
    ID() string                                     // ^[a-z][a-z0-9_-]{0,31}$
    Detect(ctx context.Context, fs FS) (models.ExtraUpdater, bool) // bool = present; Readiness left ""
    Apply() ApplySpec                               // compile-time constant
}
type ApplySpec struct {
    Path string; Args []string; Env []string; Dir string
    StepLimit time.Duration; OutputCap int; DescendantSettle time.Duration
    LogPath string; Label string                    // "DietPi"
}
func Registry() []Updater                           // fixed order: [dietpi]; snap, flatpak append later
func Lookup(id string) (Updater, bool)
type FS interface { ReadFile(name string, max int64) ([]byte, error); Lstat(name string) (fs.FileInfo, error) }
func OSRoot() FS                                    // "/" with O_NOFOLLOW|O_NONBLOCK opens, regular-file check, LimitReader(max+1)
func DirFS(root string) FS                          // fixtures
func DetectAll(ctx context.Context, fs FS) []models.ExtraUpdater // ≤ 8, registry order, absent omitted
func RebootIndicators(fs FS) (required bool, reasons []string)   // /run/reboot-required, /lib/modules/<release> missing
```

**DietPi detector** (`dietpi.go`):
- **Present** iff `Lstat` reports a regular file for both `boot/dietpi/.version` and `boot/dietpi/dietpi-update`. A symlink does not count.
- **Every read** is ≤ 64 KiB (`max = 65536`; a longer file is "unreadable"). The whole `Detect` runs under a 1 s deadline in one goroutine. A detector whose previous call is still stuck returns `unknown/detection_failed` **without** starting another read (atomic in-flight flag), so at most one goroutine per updater can leak.
- **`.version` is data.** Lines match `^(G_DIETPI_VERSION_CORE|G_DIETPI_VERSION_SUB|G_DIETPI_VERSION_RC|G_GITBRANCH|G_GITOWNER)=(.*)$`. One surrounding pair of `'` or `"` is stripped, with no escape processing. Version values match `^-?[0-9]{1,9}$`. A missing or duplicate version key means the version is invalid. Every other line is ignored.
- **`dietpi.txt`:** the first `^[ \t]*KEY=` match wins (DietPi's own `sed … q` rule), the value is trimmed of trailing blanks and CR, and the keys read are `CONFIG_CHECK_DIETPI_UPDATES`, `CONFIG_CHECK_APT_UPDATES`, `DEV_GITBRANCH` and `DEV_GITOWNER`.
- **Update flag:** the first line, trimmed, matched against `^([0-9]{1,9})\.([0-9]{1,9})\.(-?[0-9]{1,9})$`. Anything else is "unparseable".
- **Decision order** (FRD §7 table):

| Step | Condition | Result |
|---|---|---|
| D7 | `.version` invalid | `unknown` / `version_unreadable` |
| D1 | flag present and (unparseable, or newer than installed) | `update_available`; the available version is empty if unparseable |
| D2 | `run/dietpi/.live_patches` exists | `update_available` |
| D6 | `dietpi.txt` unreadable | `unknown` / `config_unreadable` |
| D5 | `CONFIG_CHECK_DIETPI_UPDATES != "1"` | `unknown` / `check_disabled` |
| D3 | `CheckEvidence()` | `up_to_date` (production `CheckEvidence` always returns false) |
| D4 | otherwise | `unknown` / `not_checked_since_boot` |
| — | 1 s deadline reached | `unknown` / `detection_failed` |

  - A flag that is parsed and **not newer** is treated as absent.
  - The pending count is (version update ? 1 : 0) + (live patches ? 1 : 0).
- **Notices** (≤ 8, in this order):
  - `live_patches_available`;
  - `dietpi_auto_apt_upgrade` when `CONFIG_CHECK_APT_UPDATES == "2"`;
  - `non_default_update_source` when the effective branch ≠ `master` or the effective owner ≠ `MichaIng`. The effective value is the `dietpi.txt` `DEV_*` value, else the `.version` `G_GIT*` value. A value that is longer than 64 bytes or has a byte outside `0x20–0x7e` is reported as `unreadable`;
  - `major_upgrade` when the available CORE is greater than the installed CORE.
- **`Apply()`:**

```text
Path  /boot/dietpi/dietpi-update
Args  ["1"]
Dir   /
Env   maintenance.FixedEnvironment() + "G_INTERACTIVE=0"   (DietPi's G_INIT overrides PATH and locale itself)
StepLimit 40m, OutputCap 128 KiB, DescendantSettle 10s
LogPath /var/lib/dietpi/logs/dietpi-update.log, Label "DietPi"
```

**Rationale.**
- One read-only detector is shared by the unprivileged collector and the root worker, so the worker's "freshly re-read status" (FR-028) uses the same rules.
- Apply specs are compiled in, so no request, config value or file can supply argv, env or path (FR-029, NFR-001).
- The contract carries a pending count and item names, and allows empty versions and per-updater limits (NFR-006).

**Alternatives considered.** A `Detect` that shells out (rejected by FR-003). Per-updater config keys (rejected by DEC-003).

### AD-005: Worker execution order and outcome (restructured `Execute`)

**Decision.** Split `Execute` into `executeApt` and `executeExtras`. The flow is in §7.1. Rules:

- **`executeApt`** is today's body unchanged, including the deferred hold restoration and `Changes`. It no longer calls `Finish`. It returns `aptErr` and leaves the apt fields of `rec` set exactly as today.
- **v1 requests** are finalised by the unchanged finaliser (`finalizeV1`). The record bytes, `Summary`, `ExitCode`, outcomes and phases equal today's for the same inputs (AC-044 golden).
- **v2 requests:**
  - `rec.AptOutcome` is `success`, `error` or `interrupted`, using today's rules.
  - `executeExtras` runs only if `AptOutcome == success`. Otherwise every present registry updater is recorded `skipped/apt_not_successful`.
  - `finalizeV2`:
    - `Outcome = success` iff the apt part succeeded and no included updater ended `failed`, `timed_out` or `interrupted`, or was skipped for `insufficient_time` or `dpkg_locked`;
    - otherwise `error`;
    - `interrupted` only when the apt part was interrupted, or an updater step was interrupted by the service context (SIGTERM from systemd).
  - `ExitCode` is the apt exit code when apt failed, else the first failing updater's exit code (`1` when there is none). `Interrupted` and `HoldsReleased` keep their apt meaning.
  - `Summary` is built as in §6.2.
- **Per-updater decision** (registry order, present updaters only, FRD §7 "Run decision" minus `excluded_by_operator`, which is agent-side):
  1. apt part not successful → `skipped/apt_not_successful`;
  2. not in `req.ExtraUpdaters`, or not listed by the policy → `skipped/not_enabled`;
  3. fresh status `unknown` → `skipped/status_unknown`; `up_to_date` → `skipped/nothing_pending`;
  4. remaining extended budget < `ExtraStartThreshold` (47m30s) → `skipped/insufficient_time`;
  5. dpkg lock held or unknown after ≤ 5 min → `skipped/dpkg_locked`;
  6. **persist** `{id, outcome: running, started_at}` with phase `extra_run:<id>` → only then start the process. If the save fails, the entry ends `failed` with note "state not persisted"; the process is not started.
  7. After exit, compute `reboot_required_after` with `RebootIndicators(OSRoot())`, re-detect `version_after`, and record the outcome.
- **Safe-exec check** before step 6: `Apply().Path` and its parents are root-owned and not group- or world-writable, the file is regular and executable (the `trustedMaintenanceFile` rule, root side). On failure the entry ends `failed` with exit -1 and note "unsafe DietPi path". `/boot` on DietPi is vfat with the default `umask 0022`, which gives `0755` root, so this passes.
- **Never retried.** `RecoverInterrupted` never runs any step. It marks an entry still `running` as `interrupted` (FR-040) and finishes the record as today.

**Rationale.** The apt part stays byte-identical, so FR-066 and AC-044 can be checked by golden tests. Persist-before-act
plus `Store.Begin` gives at-most-once (FR-035).

**Alternatives considered.** Running DietPi inside the deferred finisher. Rejected: it would hold the 2-minute cleanup
context, and a panic or ordering mistake there would skip hold restoration.

### AD-006: Bounds, strictly nested (FR-030 to FR-033)

Constants live in `internal/maintenance/protocol.go` (worker side) and `internal/models/command_policy.go` (agent and
server side). A test asserts the chain (AC-025).

| Bound | Default host (unchanged) | Host with an enabled updater (command stamped `extra_updater_budget`) |
|---|---|---|
| DietPi step limit `DietPiStepLimit` | n/a | **40 min** |
| SIGTERM→SIGKILL grace / pipe drain | 90 s / 5 s | 90 s / 5 s |
| Descendant settle after leader exit | 0 (apt rule unchanged) | 10 s (DietPi only) |
| dpkg lock pre-wait `ExtraLockWait` (poll 5 s) | n/a | ≤ 5 min |
| Start threshold `ExtraStartThreshold` (checked before the lock wait) | n/a | 47 min 30 s = 40 m + 90 s + 5 m + 60 s margin |
| Apt sub-budget `WorkerBudget` + `CleanupBudget` | 30 min + 2 min | 30 min + 2 min |
| Overall worker budget `ExtendedWorkerBudget` (from `Execute` start) | 30 min (+2) | **80 min** |
| Service backstop `RuntimeMaxSec` (+ `TimeoutStopSec` 90 s) | 35 min | **85 min** (drop-in) |
| Agent hard max `AgentHardMax(os_update)` | 31 min 30 s | **88 min** (`OSUpdateExtraAgentHardMax`) |
| Command deadline `ExecDeadline(os_update)` | 40 min | **95 min** (`OSUpdateExtraExecDeadline`) |

- **Worst case on an extended run:**
  - the apt part ends at 32 min and the remaining time is 48 min, which is at least 47m30s, so DietPi may start;
  - the lock wait ends at 37 min;
  - the step ends by 37 + 40 + 1.5 + 0.1 ≈ 78.6 min, and the record is finished before 80 min;
  - so the step limit (40) < worker budget (80) < backstop (85, killed by 86.5) < agent hard max (88) < deadline (95), and 88 + `DeadlineSlack` 5 = 93 ≤ 95 (the existing `DeadlineInvariant`).
- **Who decides "extended":** `commands.Service.Dispatch`, for `os_update` in mode `full` or `security` when:
  - `exclude_extra_updaters` is not `true`, and
  - the device's latest `updates.extra_updaters` has an entry with readiness `enabled` (status not required, because readiness is stable config state).

  It stores the extended deadline and stamps the server-owned param `extra_updater_budget: true`. Any client-supplied value or case variant of that key is deleted first.
- The agent extends `newCmdRun`'s context and sends a v2 request **only** when the stamp is present.
- The queued TTL and the window cap on `expires_at` are unchanged. A run dispatched near the window end may finish up to about 80 minutes later (D-2).

### AD-007: Process handling of the DietPi step

**Decision.**
- New internal `runProcessSpec(ctx, spec, grace, limit, settle, stdout, stderr)` is factored out of `runProcessBounded`, which becomes a thin wrapper with `settle = 0`. Apt behaviour is unchanged.
- The step context is `WithTimeout(extCtx, 40m)`. Stdin is nil (`/dev/null`), there is no terminal, `Setpgid`, `Dir=/`, and the env is `spec.Env` only.
- When the leader exits, live group members get up to `settle` (10 s) before the existing "descendants outlived parent" rule fires. This covers DietPi's `tee`.
- On step timeout: SIGTERM to the group, then SIGKILL after 90 s. Outcome `timed_out`, note "DietPi state uncertain: services may be stopped; see /var/lib/dietpi/logs/dietpi-update.log".
- Exit 0 → `success`. Non-zero → `failed` with the exit code; the last 20 non-empty output lines are kept in the section tail. Service-context cancellation → `interrupted`.
- `ExtraCommandAllowed(path, args)` admits exactly `("/boot/dietpi/dietpi-update", ["1"])`. `ExecRunner`/`CommandAllowed` stay apt-only and unchanged.
- **Output:** the step's capture is bounded at **128 KiB** with `appendBoundedLimit`, so head and tail are kept and the cut is marked. It is appended to `rec.Output` as `\n[extra:dietpi]\n…\n[extra:dietpi] full log: /var/lib/dietpi/logs/dietpi-update.log\n` through the existing 256 KiB `appendBounded`, which keeps the tail. The log-path line is last, so it always survives truncation.

### AD-008: dpkg-lock pre-wait in the worker

**Decision.** A root-side probe `maintenance.DpkgLockState()` (Linux):
- opens `/var/lib/dpkg/lock-frontend` and `/var/lib/dpkg/lock` with `O_RDONLY|O_NOFOLLOW|O_CLOEXEC` (both must be regular, root-owned files);
- queries each with `fcntl(F_OFD_GETLK, F_WRLCK)` and never takes the lock;
- returns `free`, `held(pid)` or `unknown(cause)`.

The worker polls every 5 s through an injected `Sleep`, for at most `ExtraLockWait` (5 min). If the lock is still held or unknown, the entry is `skipped/dpkg_locked`, with the holder pid or the cause in the note.

**Rationale.** Root can open the 0640 lock files, and `F_OFD_GETLK` is the kernel's own answer with no pid-namespace
caveats. The agent's `/proc/locks` probe exists only because `riot` cannot open the files.

**Alternative considered.** Moving the agent probe into a shared package. Rejected: it refactors REBOOT-DPKG-001 code for
no benefit.

### AD-009: Reboot integration (FR-040 to FR-042, DEC-008)

**Decision.**
- The step never reboots: non-interactive DietPi on install stage 2 answers its YESNO with "no", and the worker issues no power command.
- `collectors.RebootRequiredNow("apt")` and the collector's `detectRebootRequiredAPT` add `extraupd.ModulesMissing`, which is true only when DietPi is present (FR-016). It reads the running release from `proc/sys/kernel/osrelease` (charset `[A-Za-z0-9._+-]{1,64}`; any other value adds nothing) and adds the reason `kernel modules for running kernel <r> are missing`. This single function feeds:
  - `decideAuthorizedReboot`: a manual run with `reboot_if_required` reboots once if the overall run succeeded (D-4);
  - telemetry.
- Scheduled runs: `maintenanceResult` appends the pseudo-entry `"dietpi"` to `RebootClassApplied` under the OQ-A5 conditions. `maybeRebootAfterPatch` then applies its unchanged rules: `allow_reboot`, the dpkg wait (REBOOT-DPKG-001), the claim, and at most one reboot.
- Recovered results never reboot (existing rule).

### AD-010: Agent plan, readiness and result lines

**Decision** (`internal/agent/extra_updaters.go`).
- **Readiness** (telemetry and run time), first match wins:
  1. `not_opted_in` (not in `commands.allow_extra_updaters`);
  2. `patching_disabled`;
  3. `unsupported_helper` (capability probe);
  4. `root_side_missing` (policy file absent, invalid, or not listing the id);
  5. otherwise `enabled`.

  At run time, `enabled` also requires the readiness probe's effective unit `RuntimeMaxUSec` to be the extended 85 min; if it is not, the reason is `root_side_missing`.
  - Root facts are cached for 10 minutes. The cache is invalidated when the stat signature of the policy file, the drop-in or the helper changes.
- **Plan** for one `os_update`:
  - send **v2** iff mode ≠ `repair`, `exclude_extra_updaters` ≠ true, `extra_updater_budget` == true, and at least one present updater is `enabled`. `ExtraUpdaters` = the enabled IDs in registry order;
  - otherwise send v1.
- **Agent-side lines on v1 runs**, for each present updater (detected again after the run), FRD §7 order:
  - repair → no line;
  - apt not successful → `skipped (apt_not_successful)`;
  - excluded → `skipped (excluded_by_operator)`;
  - not enabled → `skipped (not_enabled)`;
  - enabled without the budget stamp → `status_unknown` / `nothing_pending` by fresh status, or `insufficient_time` when pending (AF-7; the overall result is `failed`).
- **Message:** when at least one line exists, the first line is `apt: <succeeded|failed|interrupted>; <id>: <outcome>[ (reason|exit n)]`, followed by the worker summary and then the detail lines (§6.2). On hosts with no present updater, the message is unchanged.
- **Config:** `CommandsConfig.AllowExtraUpdaters []string \`yaml:"allow_extra_updaters"\``. `LoadConfig` logs **one** `slog.Warn` per load that lists the unknown or invalid IDs, and `EnabledExtraUpdaters()` returns the known IDs.

### AD-011: Progress steps and agent events (FR-044 to FR-046)

**Decision.**
- `observeMaintenance` gains a step sink, the `*cmdRun`, which is nil-safe and is also passed on the recovery path.
- When the current record shows phase `extra_run:<id>`, or a terminal record has an entry with `started_at`, the agent emits once:
  - step `extra_<id>` (for example `extra_dietpi`);
  - event `extra_updater_started`.
- On a terminal record, for each entry that ran:
  - step `extra_<id>_<outcome>` (`success|failed|timed_out|interrupted`);
  - event `extra_updater_finished` carrying the outcome.
- Before each event, the agent appends `"<id>:start"` or `"<id>:end"` to the new durable `inflightMarker.ExtraEvents` (≤ 16 entries). If persisting fails, the event is not sent. This gives at most 2 events per updater per command across restarts.
- `models.AgentEvent` gains `command_id`, `updater` and `outcome` (all `omitempty`).
- `ReceiveAgentEvent` accepts the two new types only when:
  - `command_id` is a canonical UUID of an `os_update` command of this device;
  - `updater` matches the identifier regex;
  - `outcome` is in the enum (finished only).

  The server writes `Event{CommandID, Type, Severity, Message}` with a **server-composed** message: `DietPi update started on <host> (command <id>, origin <origin>)`, with the origin taken from the command row. Anything else gets 400 and no event.
- **No EXPECT-001 code.** EXPECT can consume these steps, events and the command row later (D-007).

### AD-012: Group K badge rule (display-only)

**Decision.** `computePatchBadge` keeps every existing condition. It changes only the **classification** of each raised
reason, and the kind:

- **Terms:**
  - `aptPending = u != nil && !stale && u.PendingUpdates > 0`;
  - `extraPending` = any entry with status `update_available`;
  - `waiting = aptPending || extraPending || requiredNow`;
  - `unapplied = (aptPending && !autoPatchSetUp) || any(extra pending && (!autoPatchSetUp || readiness != enabled))`.
- **Classification:** FRD §7 table. NY-5 uses OQ-A8 (`heldWithUpdate`). NY-6 needs `pendingRC`. NY-9 information text appends " Nothing is pending now."
- **Extra-updater items:**

| Code | Kind | When | Text |
|---|---|---|---|
| `extra_update_not_applied` | action | pending entry, readiness not `enabled` | name, version change and readiness in words; per-readiness action (§6.6) |
| `extra_update_pending` | info | pending entry, readiness `enabled` | "will be applied automatically in the patch window" / "any time" / "only by manual Patch" |
| `extra_update_unknown` | info | entry with status `unknown` | reason in words; never "up to date" |

- **Kind:** `needs_you` iff there is at least one action item. Otherwise `manual` ("Manual") when `!autoPatchSetUp`, else the existing auto kinds.
- The summary counts action items only.
- `rebootLine`'s `reasons` input is the count of raised **legacy** reasons (NY-1..NY-10, regardless of classification), so the Automatic, Reboot and As-of lines are byte-identical to pre-story for the same inputs (FR-084, AC-055).
- No dispatch path reads the badge (unchanged).

---

## 4. Component Changes

| Action | File Path | Purpose |
|---|---|---|
| CREATE | `internal/extraupd/extraupd.go` | `Updater`, `ApplySpec`, `Registry`, `Lookup`, `DetectAll`, ID regex |
| CREATE | `internal/extraupd/fs.go` | `FS`, `OSRoot` (bounded O_NOFOLLOW reads), `DirFS` for fixtures |
| CREATE | `internal/extraupd/dietpi.go` | DietPi detector (AD-004) and apply spec |
| CREATE | `internal/extraupd/reboot.go` | `ModulesMissing`, `RebootIndicators` |
| CREATE | `internal/extraupd/policy.go` | Pure policy-file encode and parse (shared by worker, agent and the installer test oracle) |
| CREATE | `internal/extraupd/dietpi_test.go`, `policy_test.go`, `reboot_test.go`, `extraupd_test.go` | AC-001..AC-010, AC-019 (spec constant) |
| CREATE | `internal/extraupd/testdata/dietpi/<case>/{boot,run,lib,proc}/…` | Fixture trees. `dietpi-update` in fixtures is a tripwire: `#!/bin/sh` + `echo DIETPI TRIPWIRE >&2; exit 97`, mode 0644 so it cannot be executed |
| CREATE | `internal/models/extra_updaters.go` (+ `_test.go`) | `ExtraUpdater`, `ExtraUpdaterNotice`, enums, lenient `ExtraUpdaterList.UnmarshalJSON` (≤ 8, normalises, never errors) |
| MODIFY | `internal/models/telemetry.go` | `UpdateInfo.ExtraUpdaters ExtraUpdaterList \`json:"extra_updaters,omitempty"\`` |
| MODIFY | `internal/models/command_policy.go` | `ParamExcludeExtraUpdaters`, `ParamExtraUpdaterBudget`, `OSUpdateExtraExecDeadline = 95m`, `OSUpdateExtraAgentHardMax = 88m`, `ExtraUpdaterBudget(params) bool` |
| MODIFY | `internal/models/events.go` | `EventExtraUpdaterStarted`, `EventExtraUpdaterFinished`; `AgentEvent.CommandID/Updater/Outcome` |
| MODIFY | `internal/maintenance/protocol.go` | `RequestVersionExtra`, `Request.ExtraUpdaters`, v1/v2 `ParseRequest`/`ValidateRequest`, `Record` fields, `UpdaterResult`, v2 `Validate`, bound constants (AD-006) |
| CREATE | `internal/maintenance/extra.go` | `executeExtras`, per-updater decision, `finalizeV2`, summary builder; seams `Extras ExtraEnv` on `Worker` (Detect FS, Policy func, LockProbe, Sleep, Runner, RebootCheck, SafeExec) |
| CREATE | `internal/maintenance/extra_linux.go` / `extra_other.go` | `ReadExtraPolicy`, `DpkgLockState` (F_OFD_GETLK), `ExtraExecRunner` with `ExtraCommandAllowed`, safe-exec check; stubs elsewhere |
| MODIFY | `internal/maintenance/worker.go` | Split `Execute` into `executeApt` + `finalizeV1`/`finalizeV2`; `RecoverInterrupted` marks `running` entries `interrupted` |
| MODIFY | `internal/maintenance/runner_linux.go` | Factor out `runProcessSpec` (env, limit, settle); `runProcessBounded` delegates with settle 0 |
| CREATE | `internal/maintenance/extra_test.go`, `extra_linux_test.go`; MODIFY `protocol_test.go`, `worker_test.go` | AC-019..AC-030, AC-044 |
| MODIFY | `cmd/riot-maintenance/main_linux.go` (+ `main_linux_test.go`) | `--capabilities` mode before the root check (prints `request-versions=1,2` and `extra-updaters=dietpi`, exit 0); wire `ExtraEnv` |
| MODIFY | `scripts/install.sh` | Classifier extension, DietPi presence, question, `EXTRA_GRANT` decision and transparency line, policy file and drop-in write, agreement re-check, backup list, `current.json` v2 validator, config template line |
| MODIFY | `scripts/test_install_maintenance.py` | Fake `/boot/dietpi` tree with tripwire `dietpi-update`; AC-015..AC-019, AC-044 (sudoers golden unchanged), AC-045 trace |
| CREATE | `scripts/extra_updaters_docs_test.go` | AC-043 (pattern of `scripts/shutdown_grant_docs_test.go`) |
| MODIFY | `internal/agent/config.go` (+ `config_test.go`) | `AllowExtraUpdaters`, one-warning validation, `EnabledExtraUpdaters()` |
| CREATE | `internal/agent/extra_updaters.go` (+ `_test.go`) | Readiness, plan, v1 lines, message composition, event emission, root-fact cache |
| CREATE | `internal/agent/extra_updaters_linux.go` / `_other.go` | Helper capability probe, agent-side policy read (root-owned path check, ≤ 512 B) |
| CREATE | `internal/agent/extra_updaters_doctor.go` (+ `_test.go`) | FR-061/062 section, injected runner and filesystem |
| MODIFY | `internal/agent/doctor.go` | Call `reportExtraUpdaters` (doctor sync rule) |
| MODIFY | `internal/agent/maintenance.go` | v1/v2 request build, step sink in `observeMaintenance`, `maintenanceResult` v2 and OQ-A5 pseudo-entry |
| MODIFY | `internal/agent/maintenance_linux.go` (+ test) | `maintenanceUnitPolicy` accepts `35min`/`2100000000` or `1h 25min`/`5100000000`; readiness returns the effective runtime |
| MODIFY | `internal/agent/commands.go` | `handleOSUpdateWithOutput` passes the `*cmdRun` and plan to `runDetachedMaintenance` |
| MODIFY | `internal/agent/command_lifecycle.go` | `newCmdRun` uses `OSUpdateExtraAgentHardMax` when `ExtraUpdaterBudget(params)` |
| MODIFY | `internal/agent/cmdstate.go` | `inflightMarker.ExtraEvents []string \`json:"extra_events,omitempty"\`` + `AddExtraEvent` |
| MODIFY | `internal/agent/telemetry.go` | Stamp readiness on `v.ExtraUpdaters` |
| MODIFY | `internal/agent/collectors/updates.go` | `info.ExtraUpdaters = extraupd.DetectAll(ctx, root)` on Linux; modules-missing reason in `detectRebootRequiredAPT` |
| MODIFY | `internal/agent/collectors/kernelreboot.go` | `rebootProbe.required("apt")` adds `ModulesMissing` |
| CREATE | `internal/agent/collectors/updates_extra_test.go` | AC-001, AC-010, AC-011 |
| CREATE | `internal/agent/extra_safety_test.go` | AC-045 source scan |
| MODIFY | `internal/server/commands/service.go` (+ test) | Budget stamp and extended deadline in `Dispatch` |
| MODIFY | `internal/server/handlers/os_update_auth.go` (+ test) | Validate `exclude_extra_updaters`; reject its case variants; strip `extra_updater_budget` variants |
| MODIFY | `internal/server/handlers/auto_update.go` (+ test) | Trigger also on an enabled pending extra updater |
| MODIFY | `internal/server/handlers/patch_badge.go` (+ test) | Group K (AD-012) |
| MODIFY | `internal/server/handlers/fleet.go` (+ test) | Panel inclusion, `extra_updaters` on the row, bulk patch (D-7) |
| MODIFY | `internal/server/handlers/handlers.go` (+ test) | `ReceiveAgentEvent` new types |
| MODIFY | `internal/server/events/generator.go` (+ test) | `CommandSent` suffix " (extra updaters excluded)" |
| MODIFY | `web/src/types/models.ts` | `ExtraUpdater` types, `UpdateInfo.extra_updaters` |
| MODIFY | `web/src/api/client.ts` | `PatchBadgeKind` + `'manual'`, `explanation.info`, `PatchBadgeReason.updater?`, patch-status row `extra_updaters?` |
| MODIFY | `web/src/utils/patchFlow.ts` (+ test) | `isNeedsYou(kind)`, extra-aware `devicePatchState`, labels, reason words, DietPi dialog statements, `osUpdateParams` |
| MODIFY | `web/src/components/PatchBadge.tsx` (+ test) | Neutral `manual` tone, "Information" list, key `code:updater` |
| MODIFY | `web/src/components/PatchConfirmOptions.tsx` (+ test) | "Apply DietPi update in this run" checkbox and statements |
| CREATE | `web/src/components/ExtraUpdatersList.tsx` (+ test) | Device-page rows next to apt updates |
| MODIFY | `web/src/pages/DeviceDetail.tsx` (+ test) | List, button label, dialog state reset, exclusion param, result message rendered with line breaks |
| MODIFY | `web/src/pages/FleetOverview.tsx` (+ test) | `isNeedsYou`, panel names the updater |
| MODIFY | `web/src/api/demo-data.ts` | DietPi pending host, DietPi unknown host, a `manual` badge |
| MODIFY | `docs/ops/os-maintenance.md`, `README.md`, `CHANGELOG.md` | FR-063..FR-065 |
| UNCHANGED (asserted) | `scripts/riot-maintenance.service`, `cmd/riot-agent-apply/*`, sudoers drop-in content | FR-066 |

---

## 5. Data Model Changes

There is no database migration and no new table. All changes are JSON shapes.

### 5.1 Telemetry (`updates.extra_updaters`, additive, omitted when empty)

```json
{"id":"dietpi","status":"update_available","unknown_reason":"","installed_version":"9.20.1",
 "available_version":"10.8.2","pending_count":1,"pending_items":[],"readiness":"enabled",
 "notices":[{"code":"major_upgrade"},{"code":"non_default_update_source","branch":"dev","owner":"MichaIng"}]}
```

**Server normalisation** (`ExtraUpdaterList.UnmarshalJSON`, used on ingest and on every read):
- A non-array or `null` becomes nil. At most 8 entries are kept, in input order, and the rest are dropped.
- An entry with an ID that does not match `^[a-z][a-z0-9_-]{0,31}$`, or a duplicate ID, is dropped.
- An unknown status, reason or readiness becomes `unknown`. An unknown notice code is dropped. At most 8 notices are kept.
- Versions are cut to 32 bytes; branch, owner and item names to 64; at most 50 items.
- `pending_count` is clamped to 0..10000.
- Strings go through `SanitizeText` before they are cut. The decoder never returns an error and never logs.

### 5.2 Worker request v2

Before (v1, unchanged): `{"version":1,"command_id":"…","mode":"full","include_reboot_class":false}`

After (v2): `{"version":2,"command_id":"…","mode":"full","include_reboot_class":false,"extra_updaters":["dietpi"]}`

### 5.3 Record v2 additions (all `omitempty`; must be absent when `version == 1`)

```text
requested_extra_updaters []string        1..8 ids (v2 only)
apt_outcome              string          success | error | interrupted (v2 only, once the apt part ended)
extra_updaters           []UpdaterResult ≤ 8, unique id
UpdaterResult {
  id string; outcome string    // running|success|failed|timed_out|interrupted|skipped
  reason string                // skipped only: apt_not_successful|not_enabled|status_unknown|nothing_pending|insufficient_time|dpkg_locked
  exit_code *int; started_at, finished_at *time.Time
  version_before, version_after string (≤ 32); reboot_required_after bool; note string (≤ 256)
}
```

`Validate`:
- `running` is allowed only while the record is running and its phase is `extra_run:<id>`;
- `Outcome == success` requires `apt_outcome == success` and no failing entry;
- the existing success-consistency checks are unchanged.

`MaxRecordBytes` (2 MiB) is unchanged.

### 5.4 `inflightMarker.ExtraEvents`

Values are `"<id>:start"` and `"<id>:end"`. There are at most 16 and they are deduplicated.

---

## 6. API / Interface Contract

### 6.1 `riot-maintenance --capabilities`

- Allowed argv: exactly `["--capabilities"]`. It needs no root and touches no lock or file.
- stdout is exactly `request-versions=1,2\nextra-updaters=dietpi\n`, and the exit code is 0.
- Any other argv keeps today's behaviour (`maintenance helper accepts no arguments`, exit 1).
- Agent parser: at most 256 bytes, exactly those two keys, IDs matching the regex. Anything else counts as an old helper.

### 6.2 Worker summary and agent message (v2, or v1 with agent lines)

```text
apt: succeeded; dietpi: failed (exit 1)
Package state verified; maintenance completed
dietpi: failed (exit 1); 9.20.1 -> 10.8.2 not applied; reboot required after DietPi: no; DietPi log: /var/lib/dietpi/logs/dietpi-update.log
```

**Per-updater outcome words:**

| Outcome | Words |
|---|---|
| `success` | `succeeded` |
| `failed` | `failed (exit n)` |
| `timed_out` | `timed out after 40m (DietPi state uncertain)` |
| `interrupted` | `interrupted` |
| `skipped` | `skipped (<reason>)` |

**Apt words:** `succeeded`, `failed` or `interrupted`.

The summary is bounded at 4096 bytes. The authorized-reboot suffix (PATCH-FLOW-001) is still appended last.

### 6.3 `os_update` parameters

| Param | Who | Type | Rule |
|---|---|---|---|
| `exclude_extra_updaters` | operator (manual only) | bool, optional | Absent means included. Non-bool, or a key that differs only in case, gives HTTP 400 `{"error":"invalid_request","code":"invalid_request"}` (existing `dockerError` shape) and no command is created |
| `extra_updater_budget` | server only | bool | Every case variant supplied by a client is deleted. `Dispatch` sets `true` per AD-006 |

The command-sent event message gains ` (extra updaters excluded)` when the operator excluded them.

### 6.4 Agent event `POST /api/v1/devices/{id}/events`

```json
{"type":"extra_updater_finished","severity":"warning","message":"","command_id":"<uuid>","updater":"dietpi","outcome":"failed"}
```

- `200 {"status":"ok"}` when accepted.
- `400 {"error":"unsupported event type"}` or `400 {"error":"invalid extra updater event"}` when validation fails (AD-011).
- Severity: `info` for started and success, `warning` otherwise. The server recomputes it.

### 6.5 Root policy file

See AD-003. Exact bytes as written by the installer: `{"version":1,"extra_updaters":["dietpi"]}\n`.

### 6.6 Patch badge (additive)

```json
{"kind":"manual","label":"Manual","explanation":{"automatic":"No.","reboot":"Not needed.",
 "needs_you":[],"needs_you_summary":"Nothing.",
 "info":[{"code":"auto_patch_off","text":"Automatic patching is off for this host.","action":"Turn on Auto-patch for this host."}],
 "as_of":"…","notes":["unattended-upgrades is installed …"]}}
```

Reason items gain `"updater":"dietpi"` for the `extra_update_*` codes.

**`extra_update_not_applied` action text by readiness:**

| Readiness | Action text |
|---|---|
| `not_opted_in` | "Add dietpi to commands.allow_extra_updaters in /etc/riot/agent.yaml and re-run install.sh as root on this host." |
| `root_side_missing` | "Re-run install.sh as root on this host." |
| `patching_disabled` | "Set commands.allow_patching: true in /etc/riot/agent.yaml." |
| `unsupported_helper` | "Re-run install.sh as root on this host to install the newer maintenance helper." |

The patch-status row gains `"extra_updaters":[…]` (the pending entries only, at most 8).

---

## 7. Sequence / Flow

### 7.1 Worker `Execute` (v2)

1. Validate the request, then `Begin` the record (`requested_extra_updaters` is set). `start := w.now()`, `extCtx := WithDeadline(svcCtx, start + 80m)`.
2. `executeApt(WithTimeout(svcCtx, 30m))`: today's steps and the deferred hold restoration and `Changes` (2-minute cleanup). The result is `aptErr`, and `rec.AptOutcome` is set.
3. Read the policy (`ReadExtraPolicy`; an error means an empty set). For each registry updater, `Detect(OSRoot)`; absent updaters are skipped silently.
4. For each present updater, apply the AD-005 decision.
   - If the decision is "run":
     - check the remaining time (≥ 47m30s);
     - lock wait (≤ 5 min; the phase `extra_wait:<id>` is saved);
     - safe-exec check;
     - persist `running` (phase `extra_run:<id>`);
     - `runProcessSpec` with a 40-minute limit, the outcome, the 128 KiB section appended, `RebootIndicators`, and `version_after`.
5. `finalizeV2`: outcome, exit code and summary, then `Store.Finish`. The service exits, and systemd's control-group kill removes any stragglers.
6. If the service is killed mid-step (backstop, reboot, crash), the next service start runs `RecoverInterrupted`: it restores holds (already done in this case), marks the entry `interrupted` and finishes the record. It never re-runs anything.

### 7.2 Agent `os_update`

1. `executeOSUpdate` → `handleOSUpdateWithOutput(cr)`. The plan is built per AD-010 (fresh detection, readiness, params).
2. `runDetachedMaintenance` writes the v1 or v2 request and starts the unit. `observeMaintenance(sink=cr)` polls; on `extra_run:dietpi` it emits step `extra_dietpi` and event `extra_updater_started` once.
3. On the terminal record: `maintenanceResult`, then v1 lines (if any), then the first-line message, the end step, and event `extra_updater_finished` once.
4. `attachRebootDecision`: authorized → `decideAuthorizedReboot`, which uses `RebootRequiredNow` including modules-missing; unauthorized → `maybeRebootAfterPatch`, which uses the pseudo-entry rule. Both run once per command and go through the dpkg wait.
5. The result is queued, then `triggerTelemetry()`, which re-detects (FR-043).

### 7.3 Server

1. Telemetry ingest normalises `extra_updaters`.
2. `checkAutoPatch` fires when `PendingUpdates > 0 || hasEnabledPendingExtra(u)`. Nothing else about it changes.
3. `Dispatch` stamps the budget and the deadline.
4. Manual `SendCommand` validates the exclusion parameter before `Dispatch`.
5. The patch-status and badge endpoints compute group K on each request.

---

## 8. Acceptance Criteria Mapping

Go test names use `Test<Unit>_EU_AC0NN_<What>`. Web tests use `describe('[EXTRA-UPD-001 AC-0NN] …')`. Python tests use
`test_EU_AC0NN_…`.

| AC ID | Fulfilled By | Test Strategy |
|---|---|---|
| AC-001 | `extraupd.DetectAll`, DietPi `Detect` presence | Unit `internal/extraupd/dietpi_test.go` `TestDietPiDetect_EU_AC001_PresenceNeedsBothFiles`; collector `internal/agent/collectors/updates_extra_test.go` `TestUpdatesCollector_EU_AC001_EmptyListWithoutDietPi` |
| AC-002 | `.version` data parser | `TestDietPiDetect_EU_AC002_VersionParsedAsDataNoProcess` (injection-line fixture; no `exec` import in `extraupd` asserted by AST; temp-dir mtime and contents unchanged) |
| AC-003 | D7 rule | `TestDietPiDetect_EU_AC003_InvalidVersionIsUnknown` |
| AC-004 | D1 + `major_upgrade` | `TestDietPiDetect_EU_AC004_UpdateAvailableMajor` |
| AC-005 | Flag compare and unparseable flag | `TestDietPiDetect_EU_AC005_StaleAndGarbageFlag` |
| AC-006 | D3–D6 and the `CheckEvidence` seam | `TestDietPiDetect_EU_AC006_NeverUpToDateWithoutEvidence` (also asserts the production evidence is false) |
| AC-007 | D2 | `TestDietPiDetect_EU_AC007_LivePatchesOnly` |
| AC-008 | Notices | `TestDietPiDetect_EU_AC008_SettingsNotices` |
| AC-009 | 64 KiB cap, 1 s deadline, in-flight guard | `TestDietPiDetect_EU_AC009_BoundsAndBlockingReader` (fake FS that blocks; a second call does not start a read) |
| AC-010 | `ModulesMissing` in collector and `RebootRequiredNow` | `TestUpdatesCollector_EU_AC010_MissingModulesRebootRequired`; `TestRebootRequiredNow_EU_AC010_SameRule` |
| AC-011 | Detection in the `updates` collector + telemetry stamp | `TestTelemetry_EU_AC011_DietPiEntryWithTodaysCollectorList` (`internal/agent/telemetry_extra_test.go`) |
| AC-012 | Readiness + v1 plan + agent line | `internal/agent/extra_updaters_test.go` `TestOSUpdate_EU_AC012_AgentSideOnlyNeverRunsDietPi` (fake maintenance client records the request; asserts v1 and the line) |
| AC-013 | Readiness order | `TestReadiness_EU_AC013_FiveReasons` |
| AC-014 | Config warning, doctor warning | `internal/agent/config_test.go` `TestLoadConfig_EU_AC014_UnknownIDOneWarning`; `extra_updaters_doctor_test.go` `TestDoctorExtra_EU_AC014_UnknownIDWarns` |
| AC-015 | Installer question | `test_EU_AC015_fresh_question_dietpi_only` (pty, `-y`, `--non-interactive`) |
| AC-016 | Classifier and agreement | `test_EU_AC016_reinstall_fail_closed_shapes` (six configs; bytes, owner and mode unchanged) |
| AC-017 | Presence gate, ownership | `test_EU_AC017_non_dietpi_records_nothing_and_root_owned` (`runuser -u riot -- test -w` fails) |
| AC-018 | Revocation rewrite | `test_EU_AC018_revocation_rewrites_empty_and_35min`; Go `TestReadiness_EU_AC018_NotOptedInAfterRevocation` |
| AC-019 | `ApplySpec` constant, `ExtraCommandAllowed`, request cannot carry argv | `internal/maintenance/extra_test.go` `TestExtras_EU_AC019_ExactArgvEnvNullStdin` (fake runner records the spec); `extra_linux_test.go` `TestExtraCommandAllowed_EU_AC019_OnlyExact`; `protocol_test.go` `TestParseRequest_EU_AC019_NoArgvField`; Python `test_EU_AC019_sudo_lists_no_dietpi_program` (listing only) |
| AC-020 | AD-005 order | `TestExtras_EU_AC020_AfterVerifyPostRefreshAndHoldRestore` (fake runner and holds append to one event log) |
| AC-021 | Repair v1 only, apt failure | `TestExtras_EU_AC021_NeverInRepairOrAfterAptFailure` (upgrade fail, restore fail) |
| AC-022 | Skip reasons neutral | `TestExtras_EU_AC022_SkipReasonsKeepSuccess` (worker); `TestOSUpdate_EU_AC022_ExcludedAndNotEnabledLines` (agent) |
| AC-023 | 40-minute step context, kill with grace | `TestExtras_EU_AC023_TimeoutIsTimedOut` (fake runner blocks on ctx; asserts deadline = 40m); `extra_linux_test.go` `TestRunProcessSpec_EU_AC023_GroupTermThenKill` (`/bin/sh` fixture ignoring TERM; short limits) |
| AC-024 | Threshold | `TestExtras_EU_AC024_InsufficientTimeFails` (injected clock) |
| AC-025 | Nested constants, unchanged defaults | `internal/models/command_policy_test.go` `TestBounds_EU_AC025_StrictlyNested`; `internal/maintenance/protocol_test.go` `TestBounds_EU_AC025_WorkerSide`; `TestExecute_EU_AC025_V1RecordGolden` |
| AC-026 | Lock wait | `TestExtras_EU_AC026_LockWaitHeldUnknown` (fake probe and sleep); `TestDpkgLockState_EU_AC026_OFDQuery` (temp files locked by the test process's child) |
| AC-027 | No retry | `TestExtras_EU_AC027_DietPiFailuresNotRetried` (exit 1 ×3 scenarios; call count 1) |
| AC-028 | `finalizeV2` summary | `TestExtras_EU_AC028_AptSuccessDietPiFailIsFailed` |
| AC-029 | `Begin` refusal, recovery, terminal reuse, event marker | `TestExtras_EU_AC029_RecoverNeverReruns`; agent `TestRecover_EU_AC029_NoSecondRequest` |
| AC-030 | 128 KiB cap within 256 KiB | `TestExtras_EU_AC030_OutputCapKeepsTailAndLogPath` |
| AC-031 | No reboot from the step | `TestOSUpdate_EU_AC031_ManualWithoutAuthNoReboot` (power permit spy) |
| AC-032 | Authorized and scheduled reboot once | `TestOSUpdate_EU_AC032_RebootOnceThroughGate` (manual + gated scheduled; `allow_reboot:false` variant) |
| AC-033 | Post-run telemetry | `TestTelemetry_EU_AC033_FreshStateAfterRun` (fixture updated by the fake runner) |
| AC-034 | Steps and events, server origin | `TestOSUpdate_EU_AC034_TwoEventsAndSteps`; `internal/server/handlers/agent_event_extra_test.go` `TestReceiveAgentEvent_EU_AC034_OriginFromCommand`; `TestNoExpectCode_EU_AC034` (no `expect` package imported) |
| AC-035 | Lenient list | `internal/models/extra_updaters_test.go` `TestExtraUpdaterList_EU_AC035_Bounds`; handler `TestIngest_EU_AC035_NoWarning2xx` (slog capture) |
| AC-036 | Trigger | `auto_update_test.go` `TestCheckAutoPatch_EU_AC036_ExtraOnlyHost` |
| AC-037 | Validation and storage | `os_update_auth_test.go` `TestManualOSUpdate_EU_AC037_ExclusionParam`; `service_test.go` `TestDispatch_EU_AC037_BudgetStamp` |
| AC-038 | Badge items, panel | `patch_badge_test.go` `TestPatchBadge_EU_AC038_ExtraItems`; `fleet_test.go` `TestPatchStatus_EU_AC038_PanelListsDietPi`; web `FleetOverview.test.tsx` |
| AC-039 | Device list and button | `ExtraUpdatersList.test.tsx`, `patchFlow.test.ts` `[AC-039]`, `DeviceDetail.test.tsx` `[AC-039]` |
| AC-040 | Dialog | `PatchConfirmOptions.test.tsx` `[AC-040]`, `DeviceDetail.test.tsx` `[AC-040]` (reset on reopen, param sent) |
| AC-041 | Result view, demo | `DeviceDetail.test.tsx` `[AC-041]`; `demo-data.test.ts` `[AC-041]` |
| AC-042 | Doctor | `TestDoctorExtra_EU_AC042_PassFailNotApplicable` (runner records argv; asserts no `dietpi-update`) |
| AC-043 | Docs | `scripts/extra_updaters_docs_test.go` `TestDocs_EU_AC043_*` |
| AC-044 | Unchanged paths | `TestExecute_EU_AC044_V1Golden`; `TestOSUpdate_EU_AC044_NonDietPiRequestV1`; Python `test_EU_AC044_sudoers_golden_unchanged` (`PRE_STORY_DROPIN`) and no policy file or drop-in on a non-DietPi host; existing dnf, darwin and windows tests unchanged |
| AC-045 | Safety scan | `internal/agent/extra_safety_test.go` `TestSafety_EU_AC045_NoForbiddenProcess` (AST scan of `*_test.go` and helpers for exec of `dietpi-*`, mutating apt/dpkg, power verbs, `riot-agent`); fixture tripwire files are mode 0644; the Python trace asserts no `DIETPI TRIPWIRE` |
| AC-046 | Kind `manual`, information | `TestPatchBadge_EU_AC046_VermontManual` |
| AC-047 | NY-1 action | `TestPatchBadge_EU_AC047_AptPendingAutoOff` |
| AC-048 | DietPi-only cases a/b/c | `TestPatchBadge_EU_AC048_DietPiOnly` |
| AC-049 | Reboot only | `TestPatchBadge_EU_AC049_RebootOnly` |
| AC-050 | Fleet count, tone | `FleetOverview.test.tsx` `[AC-050]`, `PatchBadge.test.tsx` `[AC-050]` |
| AC-051 | NY-2/NY-3 info | `TestPatchBadge_EU_AC051_SettingsAloneInfo` |
| AC-052 | NY-5/6/7 | `TestPatchBadge_EU_AC052_HoldsClassification` |
| AC-053 | NY-4/8/9 | `TestPatchBadge_EU_AC053_ReleaseFailedUnsupported` |
| AC-054 | Stale | `TestPatchBadge_EU_AC054_StaleNotWaiting` |
| AC-055 | Uniqueness and frozen lines | `TestPatchBadge_EU_AC055_UniqueAndLinesUnchanged` (table over AC-046..054; legacy lines compared with a frozen copy of the pre-story function in the test file); existing `checkAutoPatch` and dispatch tests unchanged |

---

## 9. Error Handling

| Failure | Where | Handling | Surfaces as |
|---|---|---|---|
| Detection read error, cap or 1 s | `extraupd` | Facts unknown, never `up_to_date` | `unknown/detection_failed` or `config_unreadable` / `version_unreadable` |
| Policy file missing or invalid | worker | Empty set | `skipped (not_enabled)` with note; agent readiness `root_side_missing`; doctor fail line |
| Helper too old | agent | v1 request | `unsupported_helper`, `skipped (not_enabled)` |
| Budget not stamped | agent | v1 request | `skipped (insufficient_time)`, overall `failed` (AF-7) |
| Lock held or unknown | worker | No run | `skipped (dpkg_locked)`, overall `failed`, holder in note |
| Persist failure before run | worker | No run | `failed` "state not persisted" |
| Unsafe DietPi path | worker | No run | `failed` exit -1 "unsafe DietPi path" |
| DietPi exit ≠ 0 (concurrency, network, time, space, R/O root) | worker | No retry | `failed (exit n)` + tail lines |
| Step timeout | worker | TERM, then KILL after 90 s | `timed_out`, state uncertain |
| Service killed, host reboot | next worker start | `RecoverInterrupted` | `interrupted`, Reason `restarted` |
| Bad agent event | server | 400, no event | none |
| Bad `exclude_extra_updaters` | server | 400 `invalid_request` | no command |
| Malformed telemetry entries | server | Normalised or dropped, 2xx, no log | n/a |

Logging:
- The agent logs one `slog.Info` line per updater step (start and end) and one warning per config load.
- The worker writes its normal journal output.
- The server logs nothing new on normalisation.

---

## 10. Security Considerations

- **Privilege added:** the root worker may execute exactly `/boot/dietpi/dietpi-update 1` (the `ExtraCommandAllowed` allowlist), with a compiled env, `Dir=/` and null stdin, and only when:
  - the root-owned policy lists `dietpi`;
  - the request is v2 and lists it;
  - a fresh root-side detection says `update_available`;
  - it has not run before for this command ID.

  No sudoers line, argument or environment value can be supplied by the agent, the server or the config (NFR-001).
- **Compromised agent:** it can request v2 runs with new UUIDs. That is the same DoS class as repeated apt runs today, bounded by the maintenance lock and by DietPi's own pending state (nothing runs when no update is pending). It cannot enable DietPi on a host whose policy does not list it.
- **OQ-A6, proposed accept + transparency:** `agent.yaml` is `riot`-writable, so a compromised agent can add `dietpi` and wait for Ryan's next root reinstall. On every Linux reinstall the installer prints exactly one line:
  - `==> DietPi updates: commands.allow_extra_updaters lists dietpi in /etc/riot/agent.yaml; recording root-side opt-in`, or
  - `… not listed; no root-side opt-in`, or
  - `… listed but DietPi is not present; nothing recorded`.

  The exposure is the one DietPi command DietPi's own cron would prompt Ryan to run anyway.
- **OQ-A7, proposed accept:** `dietpi-update` fetches and runs DietPi's GitHub code as root, which is the same trust as a manual run. `non_default_update_source` shows a changed source (branch and owner, bounded and printable-only) in the device view.
- **Path integrity:** the root safe-exec check on `/boot/dietpi/dietpi-update` (AD-005), and `O_NOFOLLOW` reads for detection. `.version` is never sourced.
- **Telemetry and event injection:** all agent strings are bounded and sanitised (§5.1). Event messages are composed by the server. Badge text uses server-side `stripUnsafeText` for versions and reasons.
- **Installer:** the classifier stays fail-closed (D-8). The policy file and drop-in are written with `atomic_install` as root, and both paths are added to the backup set. No deletion.

## 11. Performance Considerations

- **Detection:** ≤ 4 file reads of ≤ 64 KiB each, ≤ 1 s, once per `updates` cycle and once per worker step.
- **Root-fact cache:** 10 minutes and stat-keyed. The capability probe runs once per helper inode.
- **Badge:** O(entries ≤ 8) per host per request. There is no new query: the patch-status path already loads `updates`, and `Dispatch` already reads the latest snapshot for docker targets; one extra read happens only for `os_update`.
- **Bounded growth** (NFR-002, feedback rule):

| Item | Bound |
|---|---|
| Telemetry entries | ≤ 8 per host, strings ≤ 64 (versions ≤ 32), notices ≤ 8, items ≤ 50 × 64; existing retention |
| DietPi output section | ≤ 128 KiB inside the run's 256 KiB `Output`; record ≤ 2 MiB; existing 32-record / 16 MiB history cap |
| Record entries | ≤ 8 `UpdaterResult`, note ≤ 256, summary ≤ 4096 |
| Events | ≤ 2 per updater per command (durable marker ≤ 16 strings) |
| Policy file | ≤ 512 B, rewritten not appended; drop-in fixed 2 lines |
| Agent log | 1 warning per config load; 2 info lines per updater step |
| Runs | ≤ 1 DietPi invocation per command; ≤ 1 scheduled command per window (existing cooldown and host-key conflict) |
| Goroutines | ≤ 1 stuck detection per updater per process |
| Badge items | ≤ 10 legacy codes + 3 × 8 extra items |

## 12. Implementation Notes for Engineers

1. **Never execute anything named `dietpi-*` in tests.**
   - Every worker test injects `ExtraEnv.Runner`.
   - The one real-process test (`TestRunProcessSpec_…`) runs `/bin/sh -c` fixtures through `runProcessSpec` directly; `ExtraCommandAllowed` is never bypassed in production code.
   - Fixture `dietpi-update` files are mode 0644 tripwires.
   - Do not run `riot-agent`, `apt-get upgrade`, `reboot`, `shutdown` or `poweroff`.
   - Use `GOCACHE=D:\rIOt_gocache` and `GOTMPDIR=D:\rIOt_fuzz_tmp` on Ryan's PC.
2. **Keep the v1 path byte-identical.** Add the golden test (`TestExecute_EU_AC044_V1Golden`) **before** restructuring `Execute`, then refactor. `Request.ExtraUpdaters` must be `omitempty` and nil for v1; new `Record` fields must be `omitempty`.
3. **Do not touch `ProtocolVersion`, `UpdateStatus` or `--maintenance-protocol`.**
4. **The "descendant outlived parent" rule stays immediate for apt.** Only the DietPi spec passes `settle = 10s`.
5. **`install.sh` `current.json` validator:**
   - accept `version` 1 or 2;
   - for 2, require `requested_extra_updaters` (1..8 valid IDs), `apt_outcome` in the enum, and `extra_updaters` as a list of at most 8 dicts with an enum outcome other than `running` (terminal record);
   - for 1, these keys must be absent.
6. **Installer classifier:**
   - Keep the `# BEGIN/END shutdown-config-classifier` markers and the `config_enables_shutdown` name. Factor the awk into `classify_agent_config` (one pass) that prints `shutdown` and `extra <id>` lines into `$STAGE/config.facts`; `config_enables_shutdown` and the new `config_extra_updaters` read that file.
   - Under `commands:` accept, besides `key: scalar`:
     - `allow_extra_updaters: []`, or `[id(, id)*]` (unquoted IDs, spaces allowed);
     - `allow_extra_updaters:` followed by lines indented deeper than `cmdindent` of the form `- id`.
   - Any other value shape for that key yields no IDs **without** rejecting the file. Duplicate keys still reject.
   - Every existing SS fail-closed case must still be rejected (`test_SS_AC011…` unchanged).
7. **Installer order:**
   - The DietPi presence check is `[ -f /boot/dietpi/.version ] && [ ! -L … ] && [ -f /boot/dietpi/dietpi-update ] && [ ! -L … ]`, computed before the prompts.
   - The question comes right after "Allow remote OS patching from dashboard?" (fresh install, Linux, DietPi only). A yes adds `  allow_extra_updaters: [dietpi]` to `COMMANDS_SECTION`, and the section is emitted when this answer is yes too.
   - `EXTRA_GRANT` is decided next to `SHUTDOWN_GRANT` (fresh: the answer; bootstrap re-enrolment: none; reinstall: the classifier), printed via the transparency line, and re-checked in `require_shutdown_agreement`, which is renamed in effect to cover both facts.
   - The policy file and drop-in are written after the agreement check and before `daemon-reload`, only when `MAINT_SUPPORTED=true`.
8. **Agent `maintenanceUnitPolicy`** must accept systemd's `RuntimeMaxUSec` renderings `35min`, `2100000000`, `1h 25min` and `5100000000`. Return the duration.
9. **Badge web keys:** `${code}:${updater ?? ''}`. `manual` uses the neutral `bg-gray-800/60 text-gray-300 border-gray-700` tone. The "Information" heading is rendered only when `info.length > 0`. Use `scrollbar-thin` on any new overflow list.
10. **Dialog copy** (FR-056/057):
    - "DietPi 9.20.1 → 10.8.2 runs after apt and can take up to 40 minutes."
    - "DietPi stops its managed services (for example CUPS or VPN software) during the run and starts them again at the end."
    - "DietPi runs apt-get autopurge, which removes packages apt considers no longer needed."
    - With `major_upgrade`: "The kernel or firmware may change; a reboot is likely. Use 'Reboot automatically if the patch requires it'."
11. **`checkAutoPatch`:** change only the early-return guard and the log line. `Dispatch` decides the budget, so `checkAutoPatch` sets nothing new.
12. **`AgentEvent` new fields are `omitempty`**, so old servers see the same JSON plus unknown keys, which they ignore.
13. **Docs:**
    - `README.md` per-server steps follow Ryan's format: numbered, plain, server named, commands run on that box (FR-064).
    - `docs/ops/os-maintenance.md` gains "Extra updaters (DietPi)" and updates its "request format" paragraph to describe v2.
    - The CHANGELOG states "new privilege: the root maintenance worker may run `/boot/dietpi/dietpi-update 1` on hosts opted in by install.sh; no sudoers change".

## 13. Definition of Done

- [ ] All component changes in §4 are implemented. The UNCHANGED rows are verified by tests.
- [ ] Every AC in §8 has its named test, and all pass: `go test ./...` (`-race` on Linux CI), `cd web && npm run test:run`, and the installer fixture (`RIOT_INSTALLER_FIXTURE=1` in a disposable Docker container only).
- [ ] The v1 golden (request bytes, record, summary, steps) and the sudoers golden `PRE_STORY_DROPIN` are unchanged.
- [ ] `ProtocolVersion == 1`. `riot-agent-apply --maintenance-protocol` still prints `1`.
- [ ] The bounds test (AC-025) passes, and `DeadlineInvariant` holds for both deadline tiers.
- [ ] The safety scan (AC-045) passes. No test or helper starts `dietpi-*`, mutating apt/dpkg, a power verb or `riot-agent`.
- [ ] No new lint errors (`go vet`, `golangci-lint` as configured, `npm run lint`, `bash -n scripts/install.sh`).
- [ ] No DB migration. Telemetry with and without `extra_updaters` is accepted with 2xx.
- [ ] `docs/ops/os-maintenance.md`, `README.md` and `CHANGELOG.md` are updated (AC-043). `internal/agent/doctor.go` is updated (doctor sync rule).
- [ ] Every bound in §11 is stated in the PR body (bounded-growth rule).
- [ ] D-1..D-9 are listed in the PR for Ryan's review.
