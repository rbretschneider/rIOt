# Architecture Decision Document

- **Story ID:** EXTRA-UPD-001
- **FRD Reference:** `docs/requirements/EXTRA-UPD-001-frd.md` (revision 2)
- **Security review:** `docs/security/EXTRA-UPD-001-security-review.md`. Revision 2 was APPROVED. Revision 3 changes only the SEC-001 mechanism (OPS-001) and needs the reviewer's re-check of AD-007.
- **Related ADDs:** MAINT-RECOVERY-001, PATCH-FLOW-001, REBOOT-DPKG-001, SHUTDOWN-SUDO-001
- **Author:** Architect Agent
- **Date:** 2026-10-26
- **Status:** DRAFT, revision 3. Ryan was not reachable. Every open question is decided here on Ryan's behalf and listed in §0.3 as a "Decision for Ryan's review", with the recommended default that this ADD is written to.

## Changelog

### Revision 3 (2026-10-26): OPS-001, mount propagation

**Defect in revision 2.** `PrivateTmp=yes` put the whole unit in a private mount namespace with slave propagation. DietPi's
9→10 Raspberry Pi step (`Patch_10_0` → `dietpi-set_software rpi_firmware_migration`, `RPi_Firmware_Migration` L847–870)
does three things:

1. `umount -l /boot`;
2. rewrites `/etc/fstab`;
3. `mount /boot/firmware`.

Inside that namespace the host kept FAT at `/boot`, so host `/boot/dietpi` disappeared until a reboot. That breaks:
- DietPi's cron, postboot and banner;
- rIOt's own detection (FR-008).

**Fix.**
- The drop-in no longer sets `PrivateTmp`. It carries only `RuntimeMaxSec`.
- The DietPi step runs through a fixed launcher mode of the worker binary, `riot-maintenance --run-extra dietpi`. The launcher:
  - calls `unshare(CLONE_NEWNS)` and **keeps** the shared propagation of every other mount, so all of DietPi's mounts and unmounts reach the host exactly as in an unconfined root run;
  - makes only the `/tmp` mount private and read-only, then stacks a fresh tmpfs on it;
  - verifies isolation and propagation against pid 1's mount table;
  - `exec`s the fixed ApplySpec.
- New skip reason `mount_propagation_unverified` (OPS-001).
- `/var/tmp` is shared again. A host `/var/tmp/dietpi` must pass the root trust walk, and it must exist when `Patch_10_4` is pending.

**Sections changed.** D-10, AD-003, AD-005, AD-007, AD-010, §4, §5, §6, §8, §10, §11 and §12 are updated.

### Revision 2 (2026-10-26): security review conditions

SEC-001 to SEC-007 resolved:
- private `/tmp` with a pre-start proof;
- DietPi busy scan;
- effective-backstop check and verified installer write order;
- DietPi-identical source resolution, with only owner `MichaIng` run;
- root trust walk;
- server event dedupe;
- a 60-minute minimum interval;
- the hardening notes.

New decisions D-10 to D-12.

### Revision 1 (2026-10-26)

First version.

---

## 0. Answers, flags and decisions

### 0.1 Answers to FRD §11.2

| Item | Answer | Where |
|---|---|---|
| **OQ-A1** evidence of a completed check with nothing found | **None exists.** `dietpi-update 2` removes the flag when nothing is found (L155–176) and leaves no marker. Row D3 cannot occur in production. A host with no flag and `CONFIG_CHECK_DIETPI_UPDATES=1` reports `unknown` / `not_checked_since_boot`. The `CheckEvidence` seam (always false in production) keeps D3 unit-tested | AD-004 |
| **OQ-A2** how the root opt-in reaches the privileged path | A root-owned policy file, `/etc/riot-maintenance/extra-updaters.json`. Only `install.sh` writes it, and the worker enforces it. The v2 request can only ask. No sudoers line | AD-001–AD-003 |
| **OQ-A3** bounds | Opted-in hosts: step 40 min, threshold 47m30s, worker 30 min apt part and 80 min overall, backstop 85 min (drop-in), agent hard max 88 min, deadline 95 min. Other hosts unchanged (30 / 35 / 31m30s / 40). The host reservation lasts up to about 87 min. EXPECT-001 uses the same `commands` row | AD-006 |
| **OQ-A4** termination and the DietPi lock dir | Process-group SIGTERM at the step limit, SIGKILL after 90 s, and `KillMode=control-group` at service exit. DietPi's working dir `/tmp/DietPi-Update` is inside a root-created tmpfs that exists only in the step's mount namespace. No non-root user can reach it, and it disappears when the last step process exits, so no stale dir survives a kill. DietPi's guard cannot see host-namespace runs, so the worker refuses while another DietPi program runs (`dietpi_busy`) | AD-007, AD-008 |
| **OQ-A5** scheduled reboot signal | A pseudo-entry `dietpi` is added to `RebootClassApplied` only when all four conditions hold: `include_reboot_class`, overall success, updater `success`, and `reboot_required_after` | AD-009 |
| **OQ-A8** non-rIOt hold with an update | Yes: intersect `other_holds.packages` with `updates[].name` on lists that are not stale. Otherwise the reason is information | AD-012 |
| **OQ-A6** | Accepted. The installer prints a transparency line | §10 |
| **OQ-A7** | Only owner `MichaIng`, any branch (SEC-003) | AD-004, AD-005 |

Assumptions:
- **A-002** and **A-004** are confirmed (L507–513, L469).
- **A-007** is confirmed (`FleetOverview.tsx` L47).
- **A-003** and **A-005** cannot be verified without SSH. Doctor reports A-003.

### 0.2 FRD deviations and clarifications (architect flags)

| Flag | FRD text | Decision |
|---|---|---|
| **AF-1** | D3 / AC-006 | D3 is unreachable in production; it is tested through the seam |
| **AF-2** | FR-051 / FR-052 | Information codes `extra_update_pending` and `extra_update_unknown` |
| **AF-3** | FR-079 / AC-055 vs NFR-002 | Items are unique on (code, updater) |
| **AF-4** | FR-066 vs AC-012 / AC-022 | FR-066 is read as "no updater present" |
| **AF-5** | FR-045 origin | The server resolves it from the command row |
| **AF-6** | AC-019 listing | No sudoers line, so the listing refuses every DietPi program |
| **AF-7** | FR-038 | Agent-side `insufficient_time` when the server did not stamp the budget |
| **AF-8** | FR-024 / FR-013 | New readiness value `non_default_update_source`. The source comes from `dietpi.txt` (DietPi's rule) |
| **AF-9** | FRD §7 skip reasons | New failing reasons: `tmp_not_private`, `mount_propagation_unverified`, `dietpi_busy`, `non_default_update_source`, `unsafe_dietpi_path`, `min_interval` |

### 0.3 Decisions for Ryan's review

1. **D-1:** DietPi runs in the root worker. No sudoers line; root policy file plus unit drop-in.
2. **D-2:** Bounds per AD-006. PiVPN and PiCUPS need a patch window of at least 90 minutes that avoids the `cron.daily` time (06:25 by default).
3. **D-3:** "Unknown", never "up to date", when nothing is pending.
4. **D-4:** A failed DietPi step makes the command `failed`, so "reboot if required" does not reboot.
5. **D-5:** Information-only badge codes, and per-updater uniqueness.
6. **D-6:** A missing budget stamp leads to `insufficient_time` and a failed command.
7. **D-7:** Bulk patch includes hosts where only an enabled extra updater is pending.
8. **D-8:** A misspelled `allow_extra_updaters` value records nothing and does not reject the file.
9. **D-9:** Nothing new on hawaii and vermont.
10. **D-10 (rewritten in revision 3):** isolation is limited to the step's `/tmp`, through a launcher mount namespace (AD-007). The rest of the system, including `/var/tmp` and every mount, behaves exactly as an unconfined root run.
    - DietPi's `/var/tmp/dietpi` → `/var/lib/dietpi` migration (`Patch_10_4`) works normally. No manual log copy is needed.
    - To keep `riot` from injecting into that migration, a host `/var/tmp/dietpi` must pass the root trust walk. If the installed version is below 10.4 (the patch is pending), the directory must also exist, root-owned. Otherwise the step is refused with `unsafe_dietpi_path`.
    - The residual race with a mid-run `cron.daily` check stays documented (AD-008).
    - *Default: accept.* Follow-up option: refuse steps that would overlap the parsed `cron.daily` time.
11. **D-11:** At least 60 minutes between DietPi starts per host. *Default 60 min.*
12. **D-12:** DietPi is never run from a non-`MichaIng` `DEV_GITOWNER`. *Default: refuse.*

### 0.4 Security and operations conditions: where each is resolved

| ID | Resolution | ADD | Tests (§8) |
|---|---|---|---|
| SEC-001 | Launcher-private `/tmp` (fresh tmpfs over a private, read-only `/tmp` copy) with a `(dev, ino)` proof; DietPi busy scan; residual race documented | AD-007, AD-008 | `…_SEC001_…` |
| SEC-002 | The worker verifies its effective `RuntimeMaxUSec`. The installer writes drop-in → reload → verify → policy, and the reverse on revocation | AD-003, AD-005 | `…_SEC002_…` |
| SEC-003 | DietPi-identical source resolution; only owner `MichaIng`; readiness and skip; source recorded | AD-004, AD-005, AD-010 | `…_SEC003_…` |
| SEC-004 | Root trust walk, including `/var/tmp/dietpi` | AD-005 | `…_SEC004_…` |
| SEC-005 | Server dedupe and known-ID check | AD-011 | `…_SEC005_…` |
| SEC-006 | 60-minute minimum interval from root history | AD-005 | `…_SEC006_…` |
| SEC-007 | Hardening | AD-002, AD-008, §12 | `…_SEC007_…` |
| **OPS-001** | DietPi's mounts and unmounts reach the host: no unit-level mount namespace; the launcher keeps `/` and `/boot` shared, proves they are peers of pid 1's mounts, and refuses with `mount_propagation_unverified` otherwise | AD-003, AD-007 | `…_OPS001_…` |

---

## 1. Summary

**Contract.** A new package, `internal/extraupd`, defines the extra-updater contract: a read-only detector and a fixed,
compile-time apply spec per updater. DietPi is the only entry.

**Telemetry.** The `updates` collector reports detections. The agent stamps readiness from:
- the config;
- the helper capability;
- the root policy;
- the update source.

**The DietPi step.** The root worker `riot-maintenance` gains **request protocol v2**. After the apt part and hold
restoration, it runs `/boot/dietpi/dietpi-update 1` at most once, and only when all of these hold:
- the policy lists the updater and the update is pending;
- the source is `MichaIng`;
- the DietPi files are root-trusted;
- the effective backstop is extended and enough time is left;
- the last start was at least 60 minutes ago;
- no other DietPi program runs and the dpkg lock is free.

The worker starts the step through its own launcher mode. The launcher gives DietPi a private `/tmp` and leaves every
other mount shared with the host.

**Results and reboots.** The record holds a truthful outcome for each updater. Reboots go only through the existing gate.

**Server.** The scheduler and dispatcher learn about enabled extra updaters, and the badge follows group K.

## 2. Technical Context (verified at `186a575`)

**Root worker** (`internal/maintenance`, `cmd/riot-maintenance`)
- **Request:** a closed four-field object, `ProtocolVersion = 1`.
- **Record:** strict `DisallowUnknownFields` parsing on both sides.
- **Execute:** apt steps under 30 min; hold restoration, `Changes` and finalisation in a deferred finisher with 2 minutes of cleanup.
- **ExecRunner:** exact argv, fixed env, `Dir=/`, null stdin, `Setpgid`, TERM then KILL after 90 s.
- **Store:** `Begin` refuses repeats. `RecoverInterrupted` only restores holds. `capacity()` parses at most 32 records.

**Service** (`scripts/riot-maintenance.service`)
- `RuntimeMaxSec=35min` and `KillMode=control-group`.
- **No mount sandboxing.** The worker lives in the host mount namespace, and systemd makes `/` and its submounts `MS_SHARED` at boot.
- The agent pins `RuntimeMaxUSec` to 35 min.

**Constraints**
- `ProtocolVersion` is shared with `UpdateStatus` and `--maintenance-protocol`, so it must not change.
- **Installer:**
  - the classifier rejects list values under `commands:`;
  - the `current.json` validator asserts v1;
  - `daemon-reload` runs at L944;
  - the sudoers golden pins the drop-in.
- **Agent:** `observeMaintenance` and `recoverMaintenanceCommand`; `AgentHardMax` is 31m30s; `RebootRequiredNow` is shared by the collector and the reboot path.
- **Server:** `ReceiveAgentEvent` allows only `agent_update_*` types; `Dispatch` uses a static deadline; `checkAutoPatch` needs `PendingUpdates > 0`; the badge treats any reason as needs-you.

**DietPi facts that matter here** (@65a12a6)
- **Working dir.**
  - `G_INIT` works in `/tmp/DietPi-Update`.
  - It runs `rm -R` on a stale dir when `pidof -x` finds no other instance, then `mkdir -p` and `cd`.
  - From there it downloads and sources `version`, runs `pre-patches` and `cp -a`s the tarball.
  - **Relative paths in patches resolve under it.** For example `RPi_Firmware_Migration` runs `mkdir -p rootfs; mount $G_ROOTFS_DEV rootfs` in the cwd.
- **Mounts and fstab.** `RPi_Firmware_Migration` (L847–870):
  - `cp -an /boot/dietpi* rootfs/boot/`;
  - `umount -l /boot`;
  - `sed` on `/etc/fstab` (`/boot` → `/boot/firmware`);
  - `systemctl daemon-reload`;
  - `mount /boot/firmware`.
- **Update source.** `DEV_GITOWNER` and `DEV_GITBRANCH` come from the first matching `dietpi.txt` line (`sed …;q`), with defaults `MichaIng` and `master` (L48–51).
- **`/var/tmp` migration.** `Patch_10_4` migrates `/var/tmp/dietpi` → `/var/lib/dietpi`. That includes `cat …/dietpi-update_reinstalls >> /var/lib/dietpi/…`, whose content is later passed to `dietpi-software reinstall` (L2402–2412, L2975–2981).
- **`/tmp` mount.** DietPi mounts a tmpfs on `/tmp` through its fstab, so on DietPi `/tmp` is its own mount point.
- **`/boot` layout.** On Bookworm Raspberry Pi images, FAT is (after migration) at `/boot/firmware` and `/boot/dietpi` is on the ext4 root. Before migration, FAT is at `/boot`. Other boards may use vfat with permissions taken from mount options.

---

## 3. Architecture Decisions

### AD-001: The DietPi step runs under the root maintenance worker

**Decision.** `riot-maintenance` runs the extra-updater phase in the same service start, after hold restoration. The
agent never executes DietPi, and no sudoers line is added.

**Rationale.** The worker already provides:
- independence from agent restarts and from the agent's memory cap;
- the exclusive lock;
- a durable root record with persist-before-act and at-most-once;
- capped output and a cgroup backstop.

**Alternatives considered.**
- An exact sudoers line run by the agent. Rejected: it dies with agent restarts, runs in the agent's cgroup, needs a second at-most-once mechanism and races the worker.
- A second unit. Rejected: a second grant and a second store.

**Consequences.** `Execute` is restructured (AD-005), the record grows (AD-002), and longer bounds apply on opted-in
hosts only (AD-006).

### AD-002: Request and record protocol v2, negotiated by a helper capability probe

**Decision.**
- `ProtocolVersion` stays `1`.
- `RequestVersionExtra = 2` adds `extra_updaters`: 1–8 registry IDs, mode `full` or `security` only.
- **Parsing.** v1 has exactly 4 keys. v2 has exactly 5. A v1 `WriteRequest` is byte-identical to today's.
- **Record.** New `omitempty` fields (§5.3), empty on v1 records.
- **Agent use of v2.** The agent sends v2 only when `riot-maintenance --capabilities` prints `request-versions=1,2` and `extra-updaters=dietpi`. The probe:
  - is handled first in `main`, before any env, file, lock or root access;
  - is run by the agent after a `trustedMaintenanceFile` check, with a 5 s timeout and a 256-byte cap;
  - is cached on the helper's (dev, ino, size, mtime, ctime).

| Combination | Behaviour |
|---|---|
| New agent, old helper | `unsupported_helper`, v1 request, apt as today, line `skipped (not_enabled)` |
| Old agent, new helper | Identical v1 record |
| New agent and helper, not enabled | v1 request, identical |
| New agent and helper, enabled, budget stamped | v2 request |

**Rationale.** The closed parsers stay strict, and the probe tests the installed binary.

**Alternatives considered.**
- Bump `ProtocolVersion`. Rejected: it breaks `UpdateStatus` and old agents.
- Infer support from the policy file. Rejected: it is stale after an older installer.

**Consequences.**
- `install.sh` must accept v2 records (§12 note 5).
- A downgraded helper refuses v2 history until an operator archives it.

### AD-003: Root-side opt-in, a root-owned policy file plus a unit drop-in, written in a verified order

**Decision.** `install.sh` derives the list from the **final** agent config and from DietPi's presence. It writes fixed
literals only:

| File | Owner and mode | Content |
|---|---|---|
| `/etc/systemd/system/riot-maintenance.service.d/50-riot-extra-updaters.conf` | `root:root 0644` | `[Service]` / `RuntimeMaxSec=85min` when opted in, `35min` when revoked. **No sandboxing directive** (OPS-001) |
| `/etc/riot-maintenance/extra-updaters.json` (dir `0755`) | `root:root 0644` | `{"version":1,"extra_updaters":["dietpi"]}` or `[]` |

**Write order (SEC-002).**
- **Opt-in:**
  1. Write the drop-in.
  2. `daemon-reload` (L944).
  3. Verify `systemctl show riot-maintenance.service -p RuntimeMaxUSec --value` is `1h 25min`. Also verify that `-p PrivateTmp --value` is `no` and `-p MountFlags --value` is empty (OPS-001).
  4. Only then write the non-empty policy.
- **Revocation:**
  1. Write the policy `[]` before any unit change.
  2. Rewrite the drop-in to `35min`.
  3. Reload and verify.
- Files are written only when the list is non-empty or the file already exists (D-9). Nothing is deleted.

**Worker parse.** As in revision 2:
- safe-open: root-owned parents, `O_NOFOLLOW|O_NONBLOCK`, regular file, uid 0, mode within `0644`, `nlink == 1`;
- at most 512 B, no duplicate keys, the exact key set, at most 8 IDs;
- any error means no updater is enabled.

**Rationale.**
- The files sit outside `riot`-owned `/etc/riot`.
- The backstop changes only on opted-in hosts.
- The verified order guarantees that a non-empty policy implies an extended unit.
- The unit gets no mount namespace, so DietPi's mounts stay host mounts.

**Alternatives considered.**
- A request field. Rejected: `riot` writes the request.
- A unit change for every host. Rejected by FR-066.
- `PrivateTmp=yes`, as in revision 2. Rejected by OPS-001.

### AD-004: The generic contract, `internal/extraupd`

**Decision.** The package is the same as in revision 2. `ApplySpec` gains one field: `PrivateTmp bool` (DietPi: `true`).
That field is what makes the worker launch the step through the launcher (AD-007).

```text
type Updater interface { ID() string; Detect(ctx, FS) (models.ExtraUpdater, bool); Source(FS) Source; Apply() ApplySpec }
type Source struct{ Owner, Branch string; Readable bool }
type ApplySpec struct {
    Path string; Args []string; Env []string; Dir string
    StepLimit time.Duration; OutputCap int; DescendantSettle time.Duration
    LogPath, Label string
    TrustRoots []string; RequiredRoots func(installed models.ExtraUpdater) []string
    BusyMatch func(argv0, argv1 string) bool; TrustedOwner string; PrivateTmp bool
}
```

**DietPi detector.** Presence, `.version` as data, and the D1–D7 table are unchanged from revision 2. Notices are
unchanged too: `non_default_update_source` uses the resolved source, plus the installed `.version` owner and branch for
FR-013. `Source()` follows DietPi's own rule:
- the first `^[ \t]*KEY=` line wins;
- the value is taken verbatim;
- empty means `MichaIng` / `master`;
- an unreadable file means `Readable=false`.

**DietPi `Apply()`**
- Path, args and env:
  - `/boot/dietpi/dietpi-update` with args `["1"]`, Dir `/`;
  - Env `FixedEnvironment()` + `G_INTERACTIVE=0`.
- Limits: StepLimit 40m, OutputCap 128 KiB, settle 10s. Log path `/var/lib/dietpi/logs/dietpi-update.log`.
- **TrustRoots:** `/boot/dietpi` (tree), `/boot/dietpi.txt`, `/run/dietpi` (tree, when present), `/var/tmp/dietpi` (tree, when present; revision 3).
- **RequiredRoots:** `["/var/tmp/dietpi"]` when the installed version is below `10.4.0`, that is, while `Patch_10_4` is pending. Otherwise none.
- **BusyMatch:** argv0 or argv1 starts with `/boot/dietpi/` (except `dietpi-cloudshell`), or equals `/etc/cron.daily/dietpi`.
- `TrustedOwner: "MichaIng"`, `PrivateTmp: true`.

**Rationale.** One detector and source resolver is shared by the collector, the agent readiness and the worker. The specs
are compiled in (FR-029, NFR-001, NFR-006).

### AD-005: Worker execution order and outcome (restructured `Execute`)

**Decision.**

**Split.**
- `executeApt` is today's body without `Finish`.
- `finalizeV1` is unchanged, so v1 is byte-identical.

**v2 outcome.**
- `AptOutcome` is recorded.
- `Outcome = success` iff the apt part succeeded and every updater either succeeded or was skipped for a neutral reason (`not_enabled`, `status_unknown`, `nothing_pending`). Anything else gives `error`.
- `interrupted` covers only an apt interruption or a cancelled service context.
- `ExitCode` and `Summary` are as in §6.2.

**Per present updater.** The checks run in registry order, and the first check that fails decides the result:

| # | Check | On failure |
|---|---|---|
| 1 | The apt part succeeded | `apt_not_successful` |
| 2 | In `req.ExtraUpdaters` and in the policy | `not_enabled` |
| 3 | Fresh status | `status_unknown` / `nothing_pending` |
| 4 | Source readable, and the owner equals `TrustedOwner` exactly (resolved owner and branch go in the note) | `non_default_update_source` |
| 5 | Trust walk: `/`, `/boot`, `/run`, `/var`, `/var/tmp` (sticky `/var/tmp` allowed only as a parent), every TrustRoot and its parents, and every RequiredRoot (which must exist). Trees are walked with `Lstat` to depth ≤ 4 and ≤ 4096 entries. Every entry must be uid 0 with no group or world write; only root-owned symlinks are allowed. A bound reached means failure. vfat gets no special case. The flags from check 3 are believed only once this check passes | `unsafe_dietpi_path` |
| 6 | Effective backstop: root `systemctl show riot-maintenance.service -p RuntimeMaxUSec -p PrivateTmp -p MountFlags` (fixed argv, 5 s, 4 KiB). The strict timespan must be ≥ 83m30s, `PrivateTmp` must be `no`, and `MountFlags` must be empty. A `PrivateTmp`/`MountFlags` mismatch maps to `mount_propagation_unverified` | `insufficient_time` |
| 7 | Remaining extended budget ≥ 47m30s | `insufficient_time` |
| 8 | Minimum interval of 60 minutes, measured from root history (`Store.LatestExtraStart`). Records dated in the future are ignored, with a note. A read error refuses | `min_interval` |
| 9 | Busy and lock wait (AD-008), at most 5 minutes | `dietpi_busy` / `dpkg_locked` |
| 10 | Persist `{id, running, started_at, source}` in phase `extra_run:<id>`. If the save fails, the result is `failed` "state not persisted" and nothing is started | — |
| 11 | Start the launcher (AD-007). If launcher setup fails, the step is not run | `tmp_not_private`, `mount_propagation_unverified`, or `failed` "launcher setup failed" |
| 12 | After exit: `RebootIndicators`, `version_after`, outcome | — |

**Never retried.** `RecoverInterrupted` marks a running entry `interrupted` and runs nothing.

**Rationale.** The apt part stays byte-identical. At-most-once comes from persist-before-act plus `Begin`. Every gate
is checked root-side.

### AD-006: Bounds, strictly nested

The values are unchanged from revision 2.

| Bound | Default host | Opted-in host, command with budget stamp |
|---|---|---|
| DietPi step limit | n/a | 40 min |
| Grace and drain | 90 s / 5 s | 90 s / 5 s |
| Settle after the leader exits | 0 | 10 s |
| Busy/lock wait (poll every 5 s, scan ≤ 2 s) | n/a | ≤ 5 min |
| Backstop probe | n/a | 5 s, 4 KiB |
| Launcher setup (from start to the `ok` line) | n/a | ≤ 10 s, then the launcher is killed and the step `failed` |
| Start threshold | n/a | 47m30s |
| Minimum interval | n/a | 60 min |
| Apt part | 30 + 2 min | 30 + 2 min |
| Overall worker budget | 30 (+2) min | 80 min |
| Service backstop | 35 min | 85 min (verified to be ≥ 83m30s) |
| Agent hard max | 31m30s | 88 min |
| Command deadline | 40 min | 95 min |

**Worst case.** The apt part ends at 32 min, the wait at 37 min, and launcher setup takes up to 10 s. The step ends by
about 78.8 min, which is under 80. Nesting holds: 40 < 80 < 85 (killed at 86.5) < 88 < 95, and 88 + 5 ≤ 95.

**Extension.** `Dispatch` decides it. It first strips every case variant of `extra_updater_budget` from the params, for
every action. The agent extends only when the stamp is present, and the worker's check 6 has the final say.

### AD-007: DietPi step launcher: private `/tmp` only, all other mounts shared (SEC-001, OPS-001)

**Decision.** For specs with `PrivateTmp: true`, the worker starts the step as follows:
- binary: `HelperPath`;
- argv exactly `["--run-extra", "<id>"]`;
- `Setpgid`, null stdin, env `RIOT_EXTRA_LAUNCH=1` only;
- `ExtraFiles = [setup pipe write end]`, which becomes fd 3.

`ExtraCommandAllowed` admits exactly `(HelperPath, ["--run-extra","dietpi"])`. The launcher resolves `dietpi` through
`extraupd.Lookup` and executes only that spec's compiled Path, Args and Env.

**Launcher steps** (`cmd/riot-maintenance`, `runExtra(ops nsOps, spec)`). Any failure writes one line
`err <reason> <detail ≤ 200 B>` to fd 3 and exits 125 without exec:

1. **Preconditions.**
   - argv is exact and the ID is known;
   - euid is 0;
   - fd 3 is a FIFO;
   - `/proc/<ppid>/exe` resolves to `HelperPath`;
   - `RIOT_EXTRA_LAUNCH=1` is set.

   Otherwise the result is `err launcher_refused`. Anyone able to pass these checks is already root. A non-root caller also gets `EPERM` from `unshare`.
2. `runtime.LockOSThread()`, then `unshare(CLONE_NEWNS)`. **Propagation of `/` is not changed.** The raw syscall copies shared mounts into the same peer groups. Only the `unshare(1)` tool defaults to private.
3. **Propagation proof (OPS-001).**
   - Parse `/proc/self/mountinfo` and `/proc/1/mountinfo`, each ≤ 1 MiB and ≤ 8192 lines, strict field parser.
   - For `/`, the mount containing `/boot` (longest prefix), and `/boot/firmware` if mounted: the self entry must carry `shared:N`, and pid 1's entry for the same mount point must carry the same `N`.

   Otherwise: `err mount_propagation_unverified`.
4. **`/tmp` must be its own mount point** in self mountinfo. If it is not, the result is `err tmp_not_private`. The launcher never bind-mounts it: a new mount under the shared `/` would propagate to the host.
5. `mount(NULL, "/tmp", NULL, MS_REC|MS_PRIVATE)`. This changes only the namespace's copy.
6. `mount("/tmp", "/tmp", NULL, MS_REMOUNT|MS_BIND|MS_RDONLY|<existing nosuid,nodev,noexec>)`. The underlying copy (the same superblock as the host `/tmp`) becomes read-only for this mount only. If DietPi ever unmounts `/tmp`, it then reaches a read-only host view and fails, and cannot be fed planted files that it would execute.
7. `mount("tmpfs", "/tmp", "tmpfs", MS_NOSUID|MS_NODEV, "mode=1777,size=50%")`.
   - The parent is the private `/tmp` mount, so nothing propagates.
   - Mode `1777` keeps DietPi's child processes that run as other users working. Only processes in this namespace can see it. Non-root host users cannot reach it: `/proc/<pid>/root` needs ptrace rights over the step's processes.
8. **Isolation proof (SEC-001).**
   - `statfs("/tmp")` returns `TMPFS_MAGIC`;
   - `/tmp` is empty and owned by uid 0;
   - its `(dev, ino)` differs from `/proc/1/root/tmp`;
   - self mountinfo shows the top `/tmp` mount with no `shared:` tag.

   Otherwise: `err tmp_not_private`.
9. `chdir("/")`. Set `FD_CLOEXEC` on fd 3, write `ok\n`, then `syscall.Exec(spec.Path, [spec.Path]+spec.Args, spec.Env)`.

**Worker side.**
- It reads fd 3 for at most 10 s.
  - `ok` followed by EOF at exec: the step is running.
  - `err tmp_not_private` / `err mount_propagation_unverified`: the matching skip reason.
  - Anything else: `failed` with "launcher setup failed: …". The process group is killed.
- The process group, step limit, TERM/KILL, settle, output caps, outcome and log-path tail line are as in revision 2 (`runProcessSpec`).
- The launcher's `exec` keeps the pid and pgid, so termination covers DietPi's whole tree.

**What reaches the host.** Exactly what an unconfined root run would do, except for `/tmp`:
- `umount -l /boot`, `mount /boot/firmware`, fstab edits and package operations all act on shared mounts and propagate (`mount_namespaces(7)`: mount and unmount events propagate between peers);
- DietPi's temporary `rootfs` mount is made in its cwd under the private `/tmp`, so it stays inside the namespace. It is unmounted by DietPi anyway.

**What does not.** DietPi's working files, and any change DietPi makes to `/tmp` itself. A `mount -o remount /tmp` would
apply to the private tmpfs. `dietpi-update` does not do this; the residual is documented.

**When the step ends,** the last process exits and the namespace, with its tmpfs, is freed. No stale
`/tmp/DietPi-Update` survives.

**Seams.** `nsOps` is an interface: `Unshare`, `Mount`, `Statfs`, `Stat`, `ReadFile(mountinfo)`, `Getppid`/`Readlink`,
`Fstat(fd)`, `Exec`. Unit tests use a fake that records the call sequence and returns scripted results.

**Alternatives considered.**
- `PrivateTmp=yes` (revision 2). Rejected by OPS-001.
- `TemporaryFileSystem=/tmp`. Rejected: same unit-wide mount namespace, so the same OPS-001 defect.
- Bind-mounting a private directory on `/tmp`. Rejected: under a shared `/` it propagates to the host.
- Setting `TMPDIR` only. Rejected: DietPi hard-codes `/tmp/DietPi-Update`.

### AD-008: Root-side busy check and dpkg-lock pre-wait

Unchanged from revision 2:
- **Busy scan:** host `/proc` (the worker shares the host pid namespace), ≤ 32768 entries, ≤ 2 s, cmdline ≤ 4 KiB, matched with `BusyMatch`. Vanished pids are skipped. Other errors or bounds count as busy.
- **dpkg lock:** `O_RDONLY|O_NOFOLLOW|O_NONBLOCK|O_CLOEXEC`, root-owned regular files, `F_OFD_GETLK`.
- **Wait:** both checks must pass in one poll; poll every 5 s for at most 5 min; busy takes precedence over locked.

**Residual race** (accepted and documented). A host DietPi run that starts after the last poll is not seen by DietPi's
`/tmp` guard. In practice that is the daily `cron.daily/dietpi` check, or an admin run. The check-only run may:
- rewrite the flag or `.version` live-patch lines;
- with `CONFIG_CHECK_APT_UPDATES` set to 1 or 2, run apt, so whichever run then meets the lock fails at once, truthfully.

The overlap is bounded to the single daily `cron.daily` instant within a step of at most 40 minutes. The docs advise a
window that avoids 06:25.

### AD-009: Reboot integration

Unchanged:
- the step never reboots;
- `ModulesMissing` feeds `RebootRequiredNow` and the collector;
- the OQ-A5 pseudo-entry rule applies;
- recovered results never reboot.

After the migration, the host's `/boot/dietpi` lives on the ext4 root and `/boot/firmware` is FAT. Detection reads
`/boot/dietpi` on the host, so it keeps working (OPS-001).

### AD-010: Agent plan, readiness and result lines

**Readiness order:**
1. `not_opted_in`
2. `patching_disabled`
3. `unsupported_helper`
4. `root_side_missing`
5. `non_default_update_source`
6. `enabled`

At run time, `enabled` also requires `RuntimeMaxUSec` of 85 min from the readiness probe. The probe also shows
`PrivateTmp=no` and empty `MountFlags`; if they differ, the reason is `root_side_missing`.

The plan, the v1 lines, the message and the config are unchanged from revision 2.

### AD-011: Progress steps and agent events

Unchanged from revision 2:
- step sink;
- durable `ExtraEvents` (≤ 16);
- the new `AgentEvent` fields;
- server validation: canonical command ID of an `os_update` on this device, known updater IDs only (`dietpi`), outcome enum;
- server dedupe through `EventRepository.HasCommandEvent` under a handler mutex; duplicates return `200 {"status":"duplicate"}`;
- server-composed message, with the origin taken from the command row.

### AD-012: Group K badge rule

Unchanged from revision 2:
- **Codes:** `extra_update_not_applied` (action), `extra_update_pending` (info), `extra_update_unknown` (info).
- **Kinds:** `needs_you` iff there is an action item; otherwise `manual` or the auto kinds.
- **Reboot line:** fed by the legacy count.

---

## 4. Component Changes

| Action | File Path | Purpose |
|---|---|---|
| CREATE | `internal/extraupd/extraupd.go`, `fs.go`, `dietpi.go`, `reboot.go`, `policy.go` (+ `*_test.go`, `testdata/dietpi/<case>/…` with 0644 tripwire `dietpi-update`) | Contract, DietPi detector, source, `ApplySpec` (incl. `PrivateTmp`, `RequiredRoots`), reboot indicators, policy codec |
| CREATE | `internal/models/extra_updaters.go` (+ test) | Telemetry types, enums, lenient list |
| MODIFY | `internal/models/telemetry.go`, `command_policy.go`, `events.go` | Field, params and bound constants, event types and fields |
| MODIFY | `internal/maintenance/protocol.go` | v2 request and record, `UpdaterResult`, constants |
| CREATE | `internal/maintenance/extra.go` | `executeExtras`, checks 1–12, `finalizeV2`, summary, `ExtraEnv` seams |
| CREATE | `internal/maintenance/extra_linux.go` / `extra_other.go` | `ReadExtraPolicy`, `DpkgLockState`, `DietPiBusy`, `EffectiveUnit` (backstop + `PrivateTmp` + `MountFlags`), `TrustWalk`, `parseSystemdTimespan`, `ExtraLaunchRunner` (fd-3 setup pipe, 10 s setup bound), `ExtraCommandAllowed` |
| CREATE | `internal/maintenance/nsops_linux.go` / `nsops_other.go` | `nsOps` interface and real implementation (`unix.Unshare`, `unix.Mount`, `unix.Statfs`, `syscall.Exec`), `ParseMountinfo` |
| CREATE | `internal/maintenance/launcher.go` (+ `launcher_test.go`, `launcher_linux_test.go`) | `RunExtra(ops, id)` launcher logic (AD-007 steps 1–9) |
| MODIFY | `internal/maintenance/io_linux.go` | `Store.LatestExtraStart` |
| MODIFY | `internal/maintenance/worker.go`, `runner_linux.go` | `Execute` split, `RecoverInterrupted`, `runProcessSpec` |
| CREATE/MODIFY | `internal/maintenance/extra_test.go`, `extra_linux_test.go`, `protocol_test.go`, `worker_test.go`, `io_linux_test.go` | AC, SEC and OPS tests |
| MODIFY | `cmd/riot-maintenance/main_linux.go` (+ test) | Argv dispatch order: `--capabilities` first, then `--run-extra <id>` (launcher), then the no-argument worker. Wires `ExtraEnv` |
| MODIFY | `scripts/install.sh` | Classifier, presence, question, `EXTRA_GRANT` + transparency line, drop-in (`RuntimeMaxSec` only) → reload → verify (`RuntimeMaxUSec`, `PrivateTmp=no`, `MountFlags` empty) → policy, the reverse on revocation, backup list, closed v2 `current.json` validator, config line |
| MODIFY | `scripts/test_install_maintenance.py` | Fake `/boot/dietpi` with tripwire; `systemctl` stub answers from the drop-in; AC-015 to AC-019, AC-044, AC-045, SEC-002, OPS-001 drop-in content |
| CREATE | `scripts/extra_updaters_docs_test.go` | AC-043 |
| MODIFY | `internal/agent/config.go` (+ test) | `AllowExtraUpdaters` |
| CREATE | `internal/agent/extra_updaters.go` (+ test), `extra_updaters_linux.go` / `_other.go`, `extra_updaters_doctor.go` (+ test) | Readiness, plan, lines, events, caches, capability probe, doctor |
| MODIFY | `internal/agent/doctor.go`, `maintenance.go`, `maintenance_linux.go` (+ test), `commands.go`, `command_lifecycle.go`, `cmdstate.go`, `telemetry.go`, `collectors/updates.go`, `collectors/kernelreboot.go` | As in revision 2. `maintenance_linux.go` reads `-p PrivateTmp -p MountFlags` and requires `no` / empty |
| CREATE | `internal/agent/collectors/updates_extra_test.go`, `internal/agent/extra_safety_test.go` | AC-001, AC-010, AC-011, AC-045 |
| MODIFY | `internal/server/commands/service.go`, `handlers/os_update_auth.go`, `auto_update.go`, `patch_badge.go`, `fleet.go`, `handlers.go`, `events/generator.go`, `db/event_repo.go`, `db/interfaces.go`, `internal/testutil/mocks.go` (+ tests) | Server side, as in revision 2 |
| MODIFY | `web/src/types/models.ts`, `web/src/api/client.ts`, `web/src/utils/patchFlow.ts`, `web/src/components/PatchBadge.tsx`, `web/src/components/PatchConfirmOptions.tsx`, `web/src/pages/DeviceDetail.tsx`, `web/src/pages/FleetOverview.tsx`, `web/src/api/demo-data.ts`; CREATE `web/src/components/ExtraUpdatersList.tsx` (+ tests) | UI |
| MODIFY | `docs/ops/os-maintenance.md`, `README.md`, `CHANGELOG.md` | FR-063 to FR-065, plus: the SEC-001 residual and the upstream note, the cron advice, the owner rule, the `/var/tmp/dietpi` trust rule, and "DietPi's own mounts take effect normally" |
| UNCHANGED (asserted) | `scripts/riot-maintenance.service`, `cmd/riot-agent-apply/*`, sudoers drop-in content | FR-066 |

---

## 5. Data Model Changes

There is no database migration.

**§5.1 Telemetry.** As in revision 2. Readiness includes `non_default_update_source`. The server normalises the data
(≤ 8 entries, sanitised and cut strings), never errors and never logs.

**§5.2 Request.** v1 is unchanged. v2 adds `"extra_updaters":[…]` with `"version":2`.

**§5.3 Record v2 additions** (`omitempty`, absent in v1):

```text
requested_extra_updaters []string; apt_outcome string
extra_updaters []UpdaterResult (≤ 8)
UpdaterResult { id; outcome running|success|failed|timed_out|interrupted|skipped
  reason (skipped only): apt_not_successful|not_enabled|status_unknown|nothing_pending|insufficient_time|
          non_default_update_source|unsafe_dietpi_path|min_interval|tmp_not_private|
          mount_propagation_unverified|dietpi_busy|dpkg_locked
  exit_code *int; started_at, finished_at; version_before, version_after (≤ 32)
  source_owner, source_branch (≤ 64); reboot_required_after bool; note (≤ 256) }
```

**§5.4 `ExtraEvents`.** At most 16 entries.

---

## 6. API / Interface Contract

### 6.1 `riot-maintenance` argv modes

| argv | Requires | Behaviour |
|---|---|---|
| `--capabilities` | nothing | stdout `request-versions=1,2\nextra-updaters=dietpi\n`, exit 0. No env, file, lock or root access |
| `--run-extra <id>` | euid 0, fd 3 FIFO, parent exe `HelperPath`, `RIOT_EXTRA_LAUNCH=1` | Launcher (AD-007). Writes `ok\n` and then `exec`s the spec, or writes `err <reason> <detail>\n` and exits 125. Reasons: `launcher_refused`, `tmp_not_private`, `mount_propagation_unverified`, `setup_failed` |
| none | root | Worker, as today |
| anything else | — | Error, exit 1, as today |

### 6.2 Summary and message

Same format as in revision 2:
- the first line is `apt: <…>; dietpi: <outcome words>`;
- detail lines give the source owner and branch, the version change, whether a reboot is required, and the log path;
- the summary is at most 4096 bytes.

### 6.3 `os_update` params

| Param | Rule |
|---|---|
| `exclude_extra_updaters` | Optional bool. A non-bool value, or a variant of the key, gives 400 `invalid_request` |
| `extra_updater_budget` | Server-only. All variants are stripped for every action |

The command-sent message carries the suffix ` (extra updaters excluded)` when the operator excluded them.

### 6.4 Agent event

| Response | Meaning |
|---|---|
| `200 {"status":"ok"}` | Accepted |
| `200 {"status":"duplicate"}` | Already recorded; no new row |
| `400 {"error":"invalid extra updater event"}` | Rejected |

### 6.5 Root files

| File | Exact bytes |
|---|---|
| Policy | `{"version":1,"extra_updaters":["dietpi"]}\n` or `{"version":1,"extra_updaters":[]}\n` |
| Drop-in | `[Service]\nRuntimeMaxSec=85min\n` (opted in) or `[Service]\nRuntimeMaxSec=35min\n` (revoked) |

### 6.6 Patch badge

As in revision 2, including the `non_default_update_source` action text.

---

## 7. Sequence / Flow

### 7.1 Worker `Execute` (v2)

1. Validate the request and call `Begin`. `extCtx` = start + 80m.
2. `executeApt` (30 min + 2 min).
3. Read the policy and detect.
4. For each present updater, run checks 1–10 of AD-005.
5. Start the launcher:
   - pipe with a 10 s setup bound;
   - the launcher runs `unshare`, proves propagation, makes `/tmp` private and read-only, adds the fresh tmpfs, proves isolation, writes `ok` and `exec`s `dietpi-update 1`;
   - the worker captures output for at most 40 min, with TERM and KILL;
   - then reboot indicators, version and outcome.
6. `finalizeV2`, then `Finish`. The namespace and its tmpfs end with the last step process. The unit's control-group kill removes stragglers.
7. If the service was killed mid-step, `RecoverInterrupted` on the next start marks the entry `interrupted` and does not re-run it.

### 7.2 Agent and 7.3 Server

As in revision 2.

---

## 8. Acceptance Criteria Mapping

**Naming.**

| Suite | Pattern |
|---|---|
| Go | `Test<Unit>_EU_AC0NN_…`, `…_EU_SEC00N_…`, `…_EU_OPS001_…` |
| Web | `[EXTRA-UPD-001 AC-0NN]` |
| Python | `test_EU_…` |

**No test executes `dietpi-update`.** Launcher tests either use the fake `nsOps`, or call `RunExtra` with a test-only
spec whose Path is a `/bin/sh` fixture. The production entry `--run-extra` admits only the registry spec.

| AC ID | Fulfilled By | Test Strategy |
|---|---|---|
| AC-001 to AC-009 | DietPi detector | `internal/extraupd/dietpi_test.go` `TestDietPiDetect_EU_AC001_…` through `…_AC009_…` (as in revision 2) |
| AC-010 | Modules missing | `TestUpdatesCollector_EU_AC010_MissingModulesRebootRequired`; `TestRebootRequiredNow_EU_AC010_SameRule` |
| AC-011 | No new collector | `TestTelemetry_EU_AC011_DietPiEntryWithTodaysCollectorList` |
| AC-012, AC-013 | Readiness and plan | `TestOSUpdate_EU_AC012_AgentSideOnlyNeverRunsDietPi`; `TestReadiness_EU_AC013_FiveReasons` |
| AC-014 | Unknown IDs | `TestLoadConfig_EU_AC014_UnknownIDOneWarning`; `TestDoctorExtra_EU_AC014_UnknownIDWarns` |
| AC-015 to AC-018 | Installer | `test_EU_AC015_fresh_question_dietpi_only`, `test_EU_AC016_reinstall_fail_closed_shapes`, `test_EU_AC017_non_dietpi_records_nothing_and_root_owned`, `test_EU_AC018_revocation_policy_first_then_35min`; `TestReadiness_EU_AC018_NotOptedInAfterRevocation` |
| AC-019 | Exact command | `TestExtras_EU_AC019_ExactArgvEnvNullStdin` (launcher argv, plus the spec the launcher would exec); `TestExtraCommandAllowed_EU_AC019_OnlyLauncherArgv`; `TestRunExtra_EU_AC019_OnlyRegistrySpec` (unknown ID or extra args → `launcher_refused`, fake exec never called); `TestParseRequest_EU_AC019_NoArgvField`; `test_EU_AC019_sudo_lists_no_dietpi_program` |
| AC-020 to AC-030 | Worker | `internal/maintenance/extra_test.go` `TestExtras_EU_AC020_…` through `…_AC030_…`; `TestRunProcessSpec_EU_AC023_GroupTermThenKill`; `TestDpkgLockState_EU_AC026_OFDQuery`; `TestRecover_EU_AC029_NoSecondRequest` (as in revision 2) |
| AC-031 to AC-034 | Agent reboot and events | `TestOSUpdate_EU_AC031_…`, `…_AC032_…`, `TestTelemetry_EU_AC033_…`, `TestOSUpdate_EU_AC034_TwoEventsAndSteps`, `TestReceiveAgentEvent_EU_AC034_OriginFromCommand`, `TestNoExpectCode_EU_AC034` |
| AC-035 to AC-038 | Server | `TestExtraUpdaterList_EU_AC035_Bounds`, `TestIngest_EU_AC035_NoWarning2xx`, `TestCheckAutoPatch_EU_AC036_ExtraOnlyHost`, `TestManualOSUpdate_EU_AC037_ExclusionParam`, `TestDispatch_EU_AC037_BudgetStamp`, `TestPatchBadge_EU_AC038_ExtraItems`, `TestPatchStatus_EU_AC038_PanelListsDietPi` |
| AC-039 to AC-041 | Web | `ExtraUpdatersList.test.tsx`, `patchFlow.test.ts`, `PatchConfirmOptions.test.tsx`, `DeviceDetail.test.tsx`, `demo-data.test.ts` |
| AC-042 | Doctor | `TestDoctorExtra_EU_AC042_PassFailNotApplicable` |
| AC-043 | Docs | `TestDocs_EU_AC043_*` (also the SEC-001 residual, the owner rule and the `/var/tmp/dietpi` rule) |
| AC-044 | Unchanged paths | `TestExecute_EU_AC044_V1Golden`, `TestOSUpdate_EU_AC044_NonDietPiRequestV1`, `test_EU_AC044_sudoers_golden_unchanged` |
| AC-045 | Safety scan | `TestSafety_EU_AC045_NoForbiddenProcess` (also asserts no test calls `RunExtra` with a `/boot/dietpi` path) |
| AC-046 to AC-055 | Group K | `patch_badge_test.go` `TestPatchBadge_EU_AC046_…` through `…_AC055_…`; AC-050 in `FleetOverview.test.tsx` and `PatchBadge.test.tsx` |
| **SEC-001** | Launcher isolation, busy scan | `launcher_test.go` `TestRunExtra_EU_SEC001_PrivateTmpSequence` (fake `nsOps` records exactly: unshare → `MS_PRIVATE` on `/tmp` → RO bind-remount → tmpfs mount → `ok` → exec, with no bind mount and no propagation change on `/`); `TestRunExtra_EU_SEC001_TmpNotMountPointRefused`; `TestRunExtra_EU_SEC001_SameDevInoRefused`; `TestRunExtra_EU_SEC001_NonEmptyOrNonTmpfsRefused`; `TestExtras_EU_SEC001_LauncherErrMapsToSkip`; `TestDietPiBusy_EU_SEC001_ScanMatchesAndBounds`; `TestExtras_EU_SEC001_BusyAfterWaitSkips` |
| **SEC-002** | Backstop and installer order | `TestExtras_EU_SEC002_Effective35minRefused`; `TestParseSystemdTimespan_EU_SEC002`; `test_EU_SEC002_write_order_optin_and_revoke` |
| **SEC-003** | Source gate | `TestDietPiSource_EU_SEC003_MatchesDietPiSedRule`; `TestExtras_EU_SEC003_NonMichaIngOwnerRefused`; `TestReadiness_EU_SEC003_NonDefaultSourceNotEnabled`; `TestPatchBadge_EU_SEC003_SourceActionItem` |
| **SEC-004** | Trust walk | `TestTrustWalk_EU_SEC004_Layouts` (ext4 passes; vfat 0777 fails; group-writable globals fails; non-root `/run/dietpi` fails; non-root entry in `/var/tmp/dietpi` fails; `/var/tmp/dietpi` absent with installed `9.20.1` fails, with `10.8.2` passes; 4097 entries fails); `TestExtras_EU_SEC004_ForgedFlagNotBelieved` |
| **SEC-005** | Event dedupe | `TestReceiveAgentEvent_EU_SEC005_DuplicateIsNoop`; `TestReceiveAgentEvent_EU_SEC005_UnknownUpdaterRejected` |
| **SEC-006** | Minimum interval | `TestExtras_EU_SEC006_MinInterval`; `TestStore_EU_SEC006_LatestExtraStart` |
| **SEC-007** | Hardening | `TestDpkgLockState_EU_SEC007_NonblockAndOwnership`; `TestCapabilities_EU_SEC007_FirstAndNoSideEffects`; `TestCapabilityProbe_EU_SEC007_CapAndCacheKey`; `TestDispatch_EU_SEC007_BudgetVariantsStrippedAllActions`; `test_EU_SEC007_current_json_v2_closed`; `TestRunExtra_EU_SEC007_RefusesWithoutRootFifoParentToken` |
| **OPS-001** | Shared propagation | `TestParseMountinfo_EU_OPS001_StrictAndBounded`; `TestRunExtra_EU_OPS001_RequiresSharedPeerOfPid1` (self `shared:5` vs pid 1 `shared:5` passes; `master:5` only, a different N, `/boot` not shared, or `/boot/firmware` mismatch → `mount_propagation_unverified`, and exec is never called); `TestExtras_EU_OPS001_UnitSandboxRefused` (show reports `PrivateTmp=yes` or `MountFlags=slave` → `mount_propagation_unverified`); `test_EU_OPS001_dropin_has_no_sandboxing` (drop-in bytes exactly §6.5; effective `PrivateTmp=no`). Linux-only `launcher_linux_test.go` `TestRunExtra_EU_OPS001_RealNamespace`: runs only when `RIOT_NS_FIXTURE=1`, euid 0 and inside a disposable container with `CAP_SYS_ADMIN` (otherwise `t.Skip`). It runs the real `unshare` + tmpfs with a `/bin/sh` fixture spec and checks that (a) a mount made by the fixture under a shared test mount appears in the parent namespace, (b) a file written to `/tmp` by the fixture is not visible in the parent's `/tmp`, and (c) the parent `/tmp` is untouched |

---

## 9. Error Handling

As in revision 2, plus:

| Failure | Handling | Surfaces as |
|---|---|---|
| `/tmp` not a mount point, not isolated, not tmpfs or not empty after setup | No exec | `tmp_not_private`, failed |
| `/`, `/boot` or `/boot/firmware` not a shared peer of pid 1's mounts, or the unit has `PrivateTmp` / `MountFlags` | No exec | `mount_propagation_unverified`, failed |
| Launcher refused or setup error, or no `ok` within 10 s | Process group killed, no exec | `failed` "launcher setup failed" |
| Untrusted `/var/tmp/dietpi`, or missing while `Patch_10_4` is pending | No run | `unsafe_dietpi_path`, failed |

Every case in this table means DietPi has not started, so nothing has been changed on the host.

---

## 10. Security Considerations

**Privilege added.** The root worker may start exactly `riot-maintenance --run-extra dietpi`. That launcher may only
`exec` `/boot/dietpi/dietpi-update 1`, with the compiled env, `Dir=/` and null stdin, and only under every gate in
AD-005. No sudoers line exists. The launcher refuses a non-root caller (and `unshare` would fail with `EPERM` anyway), an
unknown ID, a missing fd-3 FIFO, a foreign parent and a missing token. It adds no capability to a caller that is not
already root.

**SEC-001 (revision 3 mechanism).**
- DietPi's working dir lives in a root-created tmpfs visible only in the step's mount namespace. The host `/tmp` underneath is private and read-only in that namespace. Both properties are proven before `exec`.
- Remaining and documented:
  - the `cron.daily` / admin race (AD-008);
  - DietPi's own pre-existing exposure: its cron and postboot run `dietpi-update 2` in the shared `/tmp`. The docs advise reporting it upstream;
  - `/dev/shm` is not used by DietPi today;
  - `/var/tmp` is shared, guarded by the trust rule (D-10).

**OPS-001.** The design deliberately does not isolate any mount other than `/tmp`, because DietPi's patches must change
host mounts. A unit-level mount namespace (`PrivateTmp`, `MountFlags`, `Protect*`) is refused at run time.

**SEC-002 to SEC-007.** As in revision 2. A compromised agent gets at most one DietPi start per 60 minutes, only from
`MichaIng`, and only while an update is pending.

**OQ-A6 / OQ-A7.** The transparency line, and `MichaIng` only.

**Installer.** Fixed literals, verified order, `atomic_install`, backup, no deletion.

## 11. Performance Considerations

As in revision 2, plus the launcher: two mountinfo reads of at most 1 MiB, three mounts, and a 10 s setup bound, once
per step.

**Bounded growth** (the table from revision 2 still applies):
- telemetry: ≤ 8 entries;
- output: ≤ 128 KiB within 256 KiB;
- record: ≤ 8 entries, note 256 B;
- events: ≤ 2 per updater per command, deduplicated on both sides;
- policy: ≤ 512 B;
- runs: ≤ 1 per command and 1 per 60 min;
- goroutines: ≤ 1 stuck detection;
- badge: ≤ 10 + 24 items.

The step's tmpfs is capped at 50 % of RAM, the same as DietPi's own `/tmp`, and is freed when the step ends.

## 12. Implementation Notes for Engineers

1. **Never execute `dietpi-*`.** Seams everywhere. Real-process tests use `/bin/sh` fixtures only. The real-namespace test is opt-in (`RIOT_NS_FIXTURE=1`, root, disposable container) and never runs on Ryan's PC or on fleet hosts. `GOCACHE=D:\rIOt_gocache`, `GOTMPDIR=D:\rIOt_fuzz_tmp`.
2. Write the v1 golden first, then restructure.
3. Do not touch `ProtocolVersion`, `UpdateStatus` or `--maintenance-protocol`.
4. Settle stays 0 for apt.
5. **`current.json` validator.** Closed: v1 or v2, exact keys and enums (including the new reasons), no `running`, the size bounds.
6. **Classifier.** As in revision 2.
7. **Installer order.**
   - Revocation: write the policy `[]` before installing the units.
   - Opt-in: write the drop-in with the units; after `daemon-reload` verify `RuntimeMaxUSec`, `PrivateTmp=no` and empty `MountFlags`; then write the policy before `systemctl start riot-agent`.
8. **The launcher must use the raw `unix.Unshare(unix.CLONE_NEWNS)`.**
   - Never call `mount(… "/", MS_REC|MS_PRIVATE)` (the `unshare(1)` default). Never bind-mount onto `/tmp`. Never unmount the private `/tmp` before stacking the tmpfs: the new mount's parent would then be the shared `/`.
   - Lock the OS thread before `unshare` and do not start goroutines until `exec`.
   - The remount flags in step 6 must include the existing `nosuid`, `nodev` and `noexec` flags of `/tmp`, read from mountinfo.
9. **Mountinfo parser.** Strict: the optional fields are terminated by `-`, and mount points use octal escapes (`\040`). The pid 1 comparison matches entries by mount point and requires the same `shared:N`.
10. **Unit file.** It must not gain `PrivateTmp`, `MountFlags`, `ProtectSystem`, `ProtectHome`, `ReadOnlyPaths`, `InaccessiblePaths` or `ProtectProc`. A test asserts the unit plus the drop-in.
11. **Docs.**
    - Per-server steps in Ryan's format.
    - For PiVPN and PiCUPS: confirm `DEV_GITOWNER` is absent or `MichaIng`; use a patch window of at least 90 minutes that avoids 06:25; the first 9→10 run migrates the boot partition to `/boot/firmware` and needs a reboot.
    - `os-maintenance.md` explains the launcher and the residuals.

## 13. Definition of Done

- [ ] Every §4 change is implemented, and the UNCHANGED rows are verified.
- [ ] Every AC, SEC and OPS row in §8 has its named test and passes: `go test ./...` (`-race` on Linux CI), `npm run test:run`, and the installer fixture in a disposable container.
- [ ] The v1 golden and the sudoers golden are unchanged. `ProtocolVersion == 1`.
- [ ] The bounds test passes, and the `DeadlineInvariant` holds for both tiers.
- [ ] The safety scan passes. No test starts `dietpi-*`, mutating apt or dpkg commands, power verbs or `riot-agent`.
- [ ] No new lint errors. No DB migration.
- [ ] Docs are updated (AC-043), and `doctor.go` is updated.
- [ ] The PR body states every bound, D-1 to D-12, the SEC-001 to SEC-007 resolutions and OPS-001. The security reviewer re-checks AD-007.
