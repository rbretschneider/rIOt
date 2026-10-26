# Security Review Report

**Story ID:** DIETPI-PATH-001 (bugfix, EXTRA-UPD-001 follow-up)
**Reviewer:** Security Researcher Agent
**Date:** 2026-10-26
**Inputs:** `docs/requirements/DIETPI-PATH-001-frd.md` and `docs/architecture/DIETPI-PATH-001-add.md` (both at 4d9a1ca), the EXTRA-UPD-001 ADD and security review, the current `internal/extraupd`, `internal/maintenance`, `scripts/install.sh` code, DietPi master and v9.20 sources, and the PiCUPS host facts.
**Verdict:** APPROVED WITH CONDITIONS

There are no CRITICAL or HIGH findings. Two MEDIUM findings change the design slightly: the badge class of one reason (DP-SEC-001), and where the flag-trust check is made (DP-SEC-002). Both are small and non-architectural. They must land in this story, and QA must verify them. The LOW items are conditions for implementation and QA.

## Threat model (short)

- **Assets:**
  - integrity of host files that root writes during `dietpi-update 1`;
  - integrity of the DietPi code and settings root executes (`/boot/dietpi`, `/boot/dietpi.txt`) and of `dietpi-update_reinstalls`;
  - the truthfulness of detection flags, which drive auto-patch;
  - the truthfulness of the badge and command status, the monitoring signal Ryan relies on.
- **Actors:**
  - a local non-root account on a DietPi host. The realistic case is a compromised service account (e.g. CUPS's `lp` on PiCUPS), not a human login;
  - a compromised agent (out of scope: it can already lie about any result);
  - none remote. No new endpoint; one new optional event field.
- **New surface:**
  - host flag removal in a world-writable directory (AD-3);
  - one more mount in the step namespace (AD-2);
  - flag trust (AD-4);
  - an event `Reason` field and text parsing of `ResultMsg` on the server (AD-5).

## Findings

| ID | Severity | Area | Required change |
|---|---|---|---|
| DP-SEC-001 | MEDIUM | AD-5 badge | `unsafe_dietpi_path` must not be in the Blocked class. After this fix, stock hosts pass the trust walk, so this reason means DietPi's executed code, its settings or `/var/tmp/dietpi` is non-root-owned or writable by others: possible tampering, and it never clears by retrying. It must raise a "Needs you" action item that is counted in "N hosts need you". The action asks Ryan to investigate ownership and modes of the named path and must not say "Nothing to do". Keep the warning event. For the persistent environment reasons (`tmp_not_private`, `mount_propagation_unverified`, `rundir_not_private`), the Blocked text must not claim the next run will fix them; say it persists until the host layout changes. |
| DP-SEC-002 | MEDIUM | AD-4 flag trust | As designed, the owner and mode are checked on an `Lstat` and the content is then read by name, in a world-writable non-sticky directory. A local user can swap in a forged `.update_available` between the two steps. Root-owned hard links also pass the check when `fs.protected_hardlinks` is 0, and an unparseable flag already counts as "update available". The trust properties must hold on the descriptor that is actually read: regular file, owner equals the trusted uid, mode `& 0o022 == 0`, **link count 1**. This is the same pattern `validateFile` / `readSafeMode` already use. `.live_patches` (presence only) needs the same properties, including link count 1. Add test cases: hard-linked root file, and a swap between stat and read (a fake FS is enough). |
| DP-SEC-003 | LOW | AD-2 launcher | (a) Any error from the `/run/dietpi` existence probe other than "does not exist" must fail closed with `rundir_not_private`, not skip to exec. (b) Before mounting, `/run/dietpi` must be proved to be a real directory owned by root, not a symlink (no-follow stat). (c) Re-run the OPS-001 `verifyPropagation` check after the AD-2 mounts, so a regression cannot silently stop `/boot` changes from reaching the host. (d) The FRD FR-006 wording "nothing else about /run may change" must acknowledge the namespace-local propagation change and its bounded side effect (see Q3). |
| DP-SEC-004 | LOW | AD-5 outcome and FR-016 | (a) `Failing()` must stay keyed on outcome. A launcher `setup_failed` / `launcher_refused` result (outcome `failed`, never started) must still fail the command; it must not be read as "skipped before start" under the FR-012 wording. Add a test. (b) The FR-016 reinterpretation of an old worker's failed run may clear NY-9 only when the refusal is the sole failure cause. Any other failure signal in the stored result (apt not succeeded, hold restore error, version verification error, interruption, timeout) must keep NY-9. Add a test with a refusal plus one such signal. |
| DP-SEC-005 | LOW | AD-1 rationale and tests | The rationale lists only what root reads. During the 9.20 run, root also writes into `logs/`, following symlinks: `tee` to `dietpi-update.log` and `dietpi-upgrade_apt.log`, an append to `G_BACKUP_FP.db`, and Patch_10_4's root glob over `logs/*`. These are safe only because `logs/` itself is proved root-only, and the ADD must state this. AC-003 must also cover `logs` and `/var/tmp/dietpi` as root-owned mode `0777` and `1777`; sticky is not acceptable for exact roots. |
| DP-SEC-006 | LOW | AD-4 seam | The guard that "production never uses `DirFSOwnedBy`" must catch any reference to the identifier, including function values and aliases, in all non-test Go under `cmd/` and `internal/`, not only call syntax. A build-tag or test-only placement that production builds cannot link is preferable. Separately, the ops doc must state that a containerised agent with a user-namespace mapping is unsupported for DietPi detection. With such a mapping, the uid seen as 0 in the container may not be host root. |
| DP-SEC-007 | LOW | AD-5 bounds | The ADD gives two different bounds for `parseRefusals`: 2048 bytes (AD-5) and 512 bytes (section 3). Pick one, state it once, and test it at the bound. The parser must use Go's linear-time regexp only, and rendered detail must stay React text (the existing static guard). |
| DP-SEC-008 | INFO | Host, upstream | Residual exposure outside rIOt: DietPi's own daily cron writes into the world-writable `/run/dietpi` (F-4). On these hosts `fs.protected_symlinks` and `fs.protected_regular` are also 0, which leaves every sticky temp directory without kernel protection. These are recommendations for Ryan's hosts, not rIOt changes (DEC-3 stands): a local tmpfiles override to `1777` for `/run/dietpi`; `fs.protected_symlinks=1`, `fs.protected_hardlinks=1` and `fs.protected_regular=2`; and the upstream report. |

## Answers

**1. Residual risk of the world-writable non-sticky `/run/dietpi`, and AD-2.**
- **Impact:** a local non-root account can make a root `dietpi-update` run truncate and overwrite an arbitrary file with a version string or a package count (integrity and denial of service, e.g. a broken auth or config file). It is not a direct code-execution primitive, because the attacker does not control the content. `rm` of the flags only removes the link, so it is harmless.
- **Kernel protection:** none applies here, because the directory is not sticky. On these hosts the sysctls are off anyway.
- **Precondition:** any local account. On PiCUPS the realistic one is a compromised network-facing service account.
- **AD-2:** it closes this for rIOt-started runs. In the step's namespace, every write DietPi makes under `/run/dietpi` lands on a fresh root-only tmpfs. The proof covers tmpfs type, uid 0, mode 0755, empty, not the host's inode, and not shared, and the step fails closed otherwise.
- **What remains:**
  - DietPi's daily `dietpi-update 2` and the motd download, both upstream (F-4, DP-SEC-008);
  - when `/run/dietpi` is absent, DietPi creates it as root with 0755, which is safe.

**2. AD-1.** Sound.
- **Parts of the 9→10 path that touch `/var/tmp/dietpi`:**
  - Patch_10_4's `cp -an logs/* …`, a data copy with no dereference that preserves ownership;
  - the `cat >>` of `dietpi-update_reinstalls`, the only content root later acts on;
  - the 9.20 `dietpi-update`'s own log writes into `logs/` (DP-SEC-005).
- **Nothing else:** no other pre-patch or patch reads `/var/tmp/dietpi`.
- **The three exact checks are stable until use.** `/var/tmp` is sticky. `/var/tmp/dietpi` and `logs` are root 0755. The file is root-owned with one link, so a non-root user cannot replace, add or modify any of them between the check and Patch_10_4.
- **Below `logs/`:** recursing into service-owned mirror directories only lets such an owner affect what root copies. The copies keep the source owner and mode, so there is no disclosure or write gain over what that owner already has. This is upstream's design.

**3. AD-2.** Sound, with DP-SEC-003.
- **OPS-001:** `MS_REC|MS_PRIVATE` applied at `/run` covers only `/run` and its submounts in this namespace. `/`, `/boot` and `/boot/firmware` (vfat, shared:82) are outside that subtree and keep their peer groups. DP-SEC-003(c) re-proves this.
- **Host-visible change:** none. Changing propagation inside the namespace changes only this namespace's copies, and the tmpfs is mounted under a private parent.
- **systemd / dietpi-services:** unaffected. Sockets and `/run/lock` are files on the same tmpfs superblocks, and services are started by systemd in the host namespace.
- **Side effect:** host mount or unmount events under `/run` during the step no longer reach the namespace. For example, a stopped unit's `/run/credentials/*` or a logged-out `/run/user/N` stay pinned, root-only, until the step exits. This is bounded by the 85-minute backstop and is the same class as the existing private `/tmp`.
- **DietPi:** creates no mounts under `/run`.

**4. AD-3.** Sound.
- **Directory handle:** `/run` is root 0755, so the `O_NOFOLLOW|O_DIRECTORY` open of `/run/dietpi` cannot be redirected, and `fstat` uid 0 pins it.
- **Unlink:** `unlinkat` with flags 0 removes the named entry itself, and a symlink is never followed. A non-root user can only re-create a flag afterwards, which AD-4 handles. Only two fixed names are touched, and only after `success`.
- **Ordering:** reconciliation runs in the worker's host namespace before the post-run `Detect`. That order is correct.

**5. AD-4.** The intent is right, but the check as designed is bypassable (DP-SEC-002).
- **Can production trust a non-root owner?** Not through the code paths: `OSRoot` and `DirFS` hard-code uid 0, and `DirFSOwnedBy` is test-only. There are two edges:
  - a weak guard (DP-SEC-006);
  - a containerised agent under a user-namespace mapping, where the uid shown as 0 may be a host non-root user. Without such a mapping, host root appears as the overflow uid, so flags are never trusted. That fails safe, as unknown.
- **Non-linux:** skipping the owner check there is acceptable, because DietPi detection cannot match on those platforms.

**6. AD-5.**
- **Pre-start refusal as success:** acceptable. Nothing ran and nothing on the host changed. The signal moves to the warning event and the badge. That is safe only if the badge does not downgrade a security-relevant refusal (DP-SEC-001), and only if launcher setup failures still fail the command (DP-SEC-004).
- **`parseRefusals`:** safe if bounded as stated: one bound (DP-SEC-007), at most 8 entries, a linear-time regexp, control characters stripped, 160-byte detail, React text rendering. A host's own agent can only affect that host's badge, which it could already do.
- **Reason allowlist:** sufficient. The server accepts only the closed `SkipReasons()` set, and the stored message is composed from server-side words, never from agent text.
- **"Blocked" for `unsafe_dietpi_path`:** yes, it hides a possible tampering signal. Change it as in DP-SEC-001.

**7. Root-side rIOt writes in world-writable directories** (locations only):
- `scripts/install.sh`:
  - `mktemp -d /tmp/riot_install.XXXXXXXX`, then everything inside that 0700 directory;
  - `/run/lock/riot-maintenance.lock`: created with noclobber, then owner, link count and mode checked.
- `internal/maintenance/io_linux.go` `AcquireLockAt` (worker and `riot-agent-apply`): `/run/lock/riot-maintenance.lock`. It uses a pinned parent directory and `O_CREAT|O_NOFOLLOW`, then `fstat` owner, mode and link count 1. A pre-created foreign file makes it fail closed, which is a denial of service only.
- **New, AD-3:** `/run/dietpi` (world-writable, non-sticky). It only unlinks, through a directory descriptor, and never creates or writes.
- **Launcher:** writes only into its own fresh private tmpfs mounts (`/tmp`, and `/run/dietpi` under AD-2).
- **Test only:** the opt-in `RIOT_NS_FIXTURE` test uses `os.MkdirTemp("/var/tmp", …)`.

**8. AD-6.** Sound.
- **The risky pairing (new worker, old agent):**
  - it cannot arise in place, because `install.sh` installs both at one version;
  - `install.sh` refuses to replace the binaries while `riot-maintenance` is active, so there is no mid-run skew;
  - agent self-update moves only the agent forward.
- **New agent with an old worker:** degrades to today's red, and FR-016 handles it on the server.
- **One note for the ops doc:** a later downgrade of the worker or agent below this version may reject stored history records that are `success` with a refusal. A downgrade should be preceded by archiving `current.json`.

## Positive observations

- Exact `lstat` checks replace a tree walk that could never pass on DietPi 9, and they keep the checks that matter: the root-interpreted file, its directory chain, and D-10.
- AD-2 fails closed, with an explicit proof, and leaves no trace on the host. AD-3 uses directory-descriptor unlinks and never follows a symlink.
- DEC-3: rIOt does not change DietPi-owned system state.
- The server never stores agent-supplied reason text.

## Verdict rationale

The verdict is APPROVED WITH CONDITIONS: there are no CRITICAL or HIGH findings. DP-SEC-001 and DP-SEC-002 are MEDIUM, with small and clear resolutions in the ADD (a badge class, and the descriptor that is checked). They must be resolved in this story. DP-SEC-003 to DP-SEC-007 are conditions for implementation, and QA must verify them. DP-SEC-008 is a host and upstream recommendation for Ryan.
