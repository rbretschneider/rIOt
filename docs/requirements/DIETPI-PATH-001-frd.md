# DIETPI-PATH-001 — Formal Requirements Document (bugfix)

**Story:** EXTRA-UPD-001 follow-up. The DietPi step refuses to run on stock DietPi, and a refused step paints a successful apt patch red.
**Status:** Draft for security review
**Date:** 2026-10-26
**Fleet impact:** PiCUPS and PiVPN (DietPi 9.20.1, arm64). hawaii and vermont are unaffected.

## 1. Problem

On PiCUPS (rIOt v2.80.0), the first real DietPi run returned:

```
status failed, step verify
apt: succeeded; dietpi: skipped (unsafe_dietpi_path)
Package state verified; maintenance completed
dietpi: skipped (unsafe_dietpi_path); /run/dietpi is writable by group or others (mode 0777)
```

Doctor was green beforehand.

## 2. Facts (MichaIng/DietPi, master 65a12a6 and tag v9.20)

| # | Fact | Source |
|---|------|--------|
| F-1 | `/run/dietpi` is created at every boot as `d /run/dietpi 0777`: world-writable, **no sticky bit**, root-owned. This has been the case since v6.29 (2020-03). The current trust walk therefore fails on every DietPi host. | `rootfs/etc/tmpfiles.d/dietpi.conf` |
| F-2 | `dietpi-update 1`, running as root, **never reads** `/run/dietpi`. It only writes or removes flag files there. | `dietpi/dietpi-update` |
| F-3 | Root's writes into `/run/dietpi` during `dietpi-update 1` are:<br>• `echo VER > /run/dietpi/.update_available`, when an update is available;<br>• `G_AGUP -f`, which writes `.apt_updates`;<br>• `rm` of `.update_available` and `.live_patches`.<br>Each write follows a symlink. `fs.protected_symlinks` and `fs.protected_regular` do not apply, because the directory is not sticky. | `dietpi/dietpi-update` L162-173, 188, 223, 469; `dietpi/func/dietpi-globals` L1734 |
| F-4 | DietPi's own `cron.daily` runs `dietpi-update 2` as root every day. That run performs the same `echo > /run/dietpi/.update_available`. The symlink-clobber exposure in F-3 therefore exists on every stock DietPi host, with or without rIOt. | `rootfs/etc/cron.daily/dietpi` |
| F-5 | On a successful `dietpi-update 1` run, DietPi removes `.update_available` and `.live_patches` itself. | `dietpi/dietpi-update` L173, L223, L469 |
| F-6 | DietPi 9 keeps its logs in `/var/tmp/dietpi/logs`, created by root with umask 0022 (0755). With DietPi-RAMlog (the default), `logs/dietpi-ramlog_store` mirrors `/var/log` with its original ownership and modes (`cp -a --attributes-only`). That includes `root:utmp 0664` (`wtmp`, `lastlog`) and service-owned directories. The current full-tree walk of `/var/tmp/dietpi` would therefore also fail on DietPi 9 hosts. | `.build/images/dietpi-installer` L963; `dietpi/func/dietpi-ramlog` (v9.20) |
| F-7 | Patch_10_4, as root:<br>• `cp -an /var/tmp/dietpi/logs/* /var/lib/dietpi/logs/` copies the logs as data, with `-a` (no dereference, ownership preserved);<br>• `cat /var/tmp/dietpi/dietpi-update_reinstalls >> /var/lib/dietpi/dietpi-update_reinstalls` appends a list that root later acts on. That file is the only content root interprets. | `.update/patches` L2402-2413 |
| F-8 | EXTRA-UPD-001 FR-038 makes any skip that is not neutral fail the whole `os_update`, even when apt succeeded and DietPi never started. | `docs/requirements/EXTRA-UPD-001-frd.md` |

## 3. Functional requirements

### Group A — path trust that stock DietPi passes

- **FR-001:** The root-side trust check must pass on a stock DietPi 9.20 or 10.x host where:
  - `/run/dietpi` is `0777 root:root`;
  - DietPi-RAMlog keeps copies of `/var/log` in `/var/tmp/dietpi/logs`.
- **FR-002:** The check must still refuse the run with `unsafe_dietpi_path` when either of these is true:
  - any file or directory under `/boot/dietpi` or `/boot/dietpi.txt` is not root-owned or is group- or world-writable;
  - any ancestor of those paths is not root-owned or is group- or world-writable (without the sticky bit).

  (These are the code DietPi executes and the settings it reads.)
- **FR-003:** Under `/var/tmp/dietpi`, only what root interprets is checked:
  - `/var/tmp/dietpi`, a directory;
  - `/var/tmp/dietpi/logs`, a directory, when present;
  - `/var/tmp/dietpi/dietpi-update_reinstalls`, a regular file, when present.

  Each must be root-owned, not a symlink, and not group- or world-writable. The file must also have exactly one link. The contents of `logs/` are not walked.
- **FR-004:** The existing requirement stays: when the installed version is below 10.4, `/var/tmp/dietpi` must exist (D-10).
- **FR-005:** `/run/dietpi` must no longer be part of the trust walk. `/run` itself stays a checked ancestor.

### Group B — no root write through `/run/dietpi` during a rIOt run

- **FR-006:** During the DietPi step, `/run/dietpi` must be a private, empty, root-owned 0755 tmpfs inside the step's mount namespace, so root's writes there (F-3) cannot follow a symlink planted by a local user. Nothing else about the step's view of `/run` may change.
- **FR-007:** If the launcher cannot set up or prove FR-006, the step must not start. It is recorded as skipped with the new reason `rundir_not_private`. Nothing on the host changes.
- **FR-008:** After a DietPi step with outcome `success`, the worker must remove the host's `/run/dietpi/.update_available` and `/run/dietpi/.live_patches`, mirroring F-5.
  - Removal must never follow a symlink, and must happen only inside a root-owned `/run/dietpi`.
  - A missing file is not an error. Any other failure adds a note to the updater result and never changes the outcome.
  - `.apt_updates` and `.dietpi_motd` are not touched.
- **FR-009:** Post-run detection must happen after FR-008.

### Group C — flags are believed only when root wrote them

- **FR-010:** Detection, in both the agent and the worker, must treat `.update_available` and `.live_patches` as absent unless the flag is all of:
  - a regular file;
  - owned by the trusted owner (root in production);
  - not group- or world-writable.
- **FR-011:** Tests must be able to name the trusted owner for a fixture tree. Production code paths (`OSRoot`, and `DirFS` for the containerised agent's host root) must trust only uid 0.

### Group D — truthful outcome

- **FR-012 (amends EXTRA-UPD-001 FR-038 and FR-039):** The `os_update` outcome is `failed` only when:
  - the apt part did not succeed; or
  - an extra updater **started** and did not succeed (`failed`, `timed_out` or `interrupted`).

  An updater skipped before it started, for any reason, leaves a successful apt run `success`.
- **FR-013:** The summary's first line still states both parts, e.g. `apt: succeeded; dietpi: skipped (unsafe_dietpi_path)`. The detail line stays.
- **FR-014:** For a skip that is not neutral (any reason except `not_enabled`, `nothing_pending`, `status_unknown`, `excluded_by_operator` and `apt_not_successful`), the agent must send one `extra_updater_finished` event:
  - outcome `skipped`, the skip reason, severity `warning`;
  - at most once per command and updater, across restarts, like the existing events.

  The server must accept the reason only from the known skip-reason set, and otherwise drop it.
- **FR-015 (amended 2026-10-26 by Ryan: "Needs you" is only for something Ryan can act on):** When the latest finished `os_update` refused an updater, and that updater is still pending and enabled, the badge reports the refusal. A refusal means a skip that is not neutral. The run counts whether its status is success, or failed only because of that refusal (a result from an older worker: first line `apt: succeeded; <id>: skipped (<reason>)`).

  The reasons split into two classes (revision 2, security review DP-SEC-001).
  - **Blocked** (rIOt's own isolation check, or a temporary condition; Ryan has nothing to do): `tmp_not_private`, `mount_propagation_unverified`, `rundir_not_private`, `dietpi_busy`, `min_interval`, `dpkg_locked`.
    - Also Blocked: the known stock false positive from a worker older than this fix, `unsafe_dietpi_path` whose detail starts with `/run/dietpi `. Its text says the fix arrives with a root `install.sh` re-run on that host.
    - The badge shows a new warning state, **Blocked**, not "Needs you", and it is not counted in "N hosts need you".
    - The explanation gives the plain reason, e.g. `DietPi update blocked: /run/dietpi permissions`, plus the bounded detail from the run. It never claims the update "will be applied automatically".
    - The texts for `tmp_not_private`, `mount_propagation_unverified` and `rundir_not_private` say the host's mount layout prevented isolation. They must not claim the next run fixes it.
  - **Action** (Ryan can act; "Needs you", counted in "N hosts need you"):
    - Every `unsafe_dietpi_path` other than the false positive above. After this fix, a stock host passes, so this reason means DietPi's code, its settings or `/var/tmp/dietpi` is not root-only: a possible tampering signal that never clears by retrying. The action is "Investigate the named path on this host (owner and permissions); rIOt will not run DietPi until it is root-only."
    - `non_default_update_source` (the host's update source) and `insufficient_time` (the patch window is too short), each with its per-reason action.
- **FR-016:** A failed last run (NY-9) whose only failure is such a refusal (the old worker's FR-038 behaviour) must be classified by FR-015, not as "the last patch run failed". The server fix alone must therefore clear PiCUPS's red badge, with no new patch run and no new agent or worker.
- **FR-017:** Badge precedence: any true action item makes the badge "Needs you". With no action item, a Blocked refusal makes it **Blocked**. Otherwise the badge is Auto, Manual or Unknown, as today. Blocked hosts have their own filter count and are not part of "N hosts need you".

## 4. Acceptance criteria

- **AC-001** [FR-001, FR-005]
  - **Given** a fixture with `/run/dietpi` at `0777 root`, `/boot/dietpi` and `/boot/dietpi.txt` stock and root-owned, and a `/var/tmp/dietpi/logs/dietpi-ramlog_store` holding a `0664` file and a directory owned by uid 1000
  - **When** the trust walk runs for installed 9.20.1
  - **Then** it passes.
- **AC-002** [FR-002]
  - **Given** a group-writable file or a non-root file under `/boot/dietpi`
  - **When** the trust walk runs
  - **Then** it fails with `unsafe_dietpi_path`, as before.
- **AC-003** [FR-003, FR-004]
  - **Given** any one of these:
    - `/var/tmp/dietpi` non-root-owned or `0777`;
    - `logs` owned by uid 1000;
    - `logs` is a symlink;
    - `dietpi-update_reinstalls` owned by uid 1000, `0666`, a symlink, or hard-linked;
    - `/var/tmp/dietpi` absent with 9.20.1 installed
  - **Then** the walk fails.
  - **And** `/var/tmp/dietpi` absent with 10.8.2 installed passes.
- **AC-004** [FR-006]
  - **When** the launcher runs with a fake mount table where `/run` is its own shared mount and `/run/dietpi` exists
  - **Then** it makes `/run` private in its namespace and mounts a tmpfs on `/run/dietpi` with `mode=0755`, `nosuid`, `nodev`, `noexec`.
  - **And** the proof checks: tmpfs, uid 0, mode 0755, empty, not the host's inode, not shared.
  - **And** only then does it exec.
- **AC-005** [FR-007]
  - **Given** any of these:
    - `/run` is not its own mount;
    - making `/run` private fails;
    - mounting the tmpfs fails;
    - the proof fails
  - **Then** the launcher writes `err rundir_not_private ...` and does not exec.
  - **And** the worker records `skipped (rundir_not_private)`.
- **AC-006** [FR-006] When `/run/dietpi` does not exist, the launcher does not create or mount anything there and proceeds.
- **AC-007** [FR-008]
  - **Given** a host `/run/dietpi` holding `.update_available`, `.live_patches`, `.apt_updates` and a symlink `.live_patches2 → /etc/passwd`
  - **When** the step succeeds
  - **Then** the first two are gone, `.apt_updates` stays, and no symlink target is touched.
  - **When** the step fails
  - **Then** nothing is removed.
  - **And** a removal error gives a note and keeps `success`.
- **AC-008** [FR-008] A `/run/dietpi` that is a symlink or not root-owned is not touched, and a note is recorded.
- **AC-009** [FR-010, FR-011]
  - **Given** a flag owned by a non-trusted uid, or with mode `0666`
  - **When** detection runs
  - **Then** the flag counts as absent.
  - **And** a trusted-owner flag still yields `update_available` with its version.
  - **And** `OSRoot()` trusts only uid 0.
- **AC-010** [FR-012, FR-013]
  - **Given** apt succeeds
  - **When** DietPi is skipped for each non-neutral reason (`unsafe_dietpi_path`, `dpkg_locked`, `insufficient_time`, `min_interval`, `dietpi_busy`, `tmp_not_private`, `mount_propagation_unverified`, `rundir_not_private`, `non_default_update_source`)
  - **Then** the record outcome is `success` and the summary's first line names the skip.
  - **When** DietPi starts and returns `failed`, `timed_out` or `interrupted`
  - **Then** the outcome is `failed`, as before.
- **AC-011** [FR-014]
  - **Given** a non-neutral skip
  - **Then** exactly one warning `extra_updater_finished` event with outcome `skipped` and the reason is sent, across an agent restart.
  - **And** a neutral skip sends none.
  - **And** the server drops an unknown reason.
- **AC-012** [FR-015, FR-017]
  - **Given** a last run with status success and first line `apt: succeeded; dietpi: skipped (unsafe_dietpi_path)`, a detail line `dietpi: skipped (unsafe_dietpi_path); /run/dietpi is writable by group or others (mode 0777)`, DietPi still pending and enabled, auto-patch on, and nothing else waiting
  - **Then** the badge kind is `blocked`, not `needs_you`.
  - **And** the explanation holds one item: `DietPi update blocked: /run/dietpi permissions (fixed by this rIOt release; re-run install.sh as root on this host to apply the fix)` (ADD revision 2), with the bounded detail.
  - **And** it is not counted by the "hosts need you" filter.
  - **And** no text says it "will be applied automatically".
- **AC-014** [FR-015] Each Blocked reason gives `blocked`. `non_default_update_source` and `insufficient_time` give `needs_you` with their action. With DietPi no longer pending, the refusal is information only, and the badge is Auto, Manual or Unknown. A neutral skip raises nothing.
- **AC-014a** [FR-015, DP-SEC-001]
  - `unsafe_dietpi_path` with detail `/boot/dietpi/func/dietpi-globals is writable by group or others` gives `needs_you` with the investigate action.
  - The same reason with detail `/run/dietpi is writable by group or others (mode 0777)` gives `blocked`, with the "re-run install.sh as root" text.
- **AC-015** [FR-016] PiCUPS today:
  - **Given** a last run with status `failed`, first line `apt: succeeded; dietpi: skipped (unsafe_dietpi_path)`, DietPi pending and enabled
  - **Then** the badge is `blocked`, with no "last patch run failed" item.
  - **And** a failed run whose apt part failed still raises NY-9 as before.
- **AC-016** [FR-017]
  - **Given** a Blocked refusal plus a true action item (e.g. a reboot required now that nothing will do)
  - **Then** the badge is `needs_you`, and the blocked reason still appears.
- **AC-017** [web] The dashboard renders the `blocked` kind as an amber "Blocked" badge with its explanation. The "N hosts need you" count and filter exclude Blocked hosts. The patch-status filters accept the kind.
- **AC-013** [regression] The PiCUPS layout (F-1, F-6) end to end in the worker rig gives a DietPi step that runs.

## 5. Decisions for Ryan (each with Clod's recommendation)

- **DEC-1:** A skip before start is `success`, with a warning event and a badge action item, not `failed`. Recommend: yes.
  - Why: a refused step changed nothing on the host.
  - Red would mean "something broke"; showing red for a refusal trains Ryan to ignore red.
  - The pending update stays visible as "Needs you" until it is applied.
- **DEC-2:** Isolate `/run/dietpi` during rIOt's run, as well as relaxing the check. Recommend: yes.
  - Cost: a few mount calls in the existing launcher.
  - Benefit: closes the symlink-clobber primitive for rIOt-started runs.
  - DietPi's own daily cron keeps the exposure (F-4). It is reported upstream as a recommendation, not fixed by rIOt.
- **DEC-3:** rIOt does not chmod `/run/dietpi` to `1777` on the host. Recommend: no chmod.
  - It would change DietPi-owned system state outside the step, and tmpfiles resets it at boot anyway.
