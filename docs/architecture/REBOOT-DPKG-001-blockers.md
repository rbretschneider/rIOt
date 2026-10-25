# Blockers and architect questions

- **Story ID:** REBOOT-DPKG-001
- **Raised by:** Senior Dev Agent, during implementation
- **Date:** 2026-10-24
- **Status:** NOT BLOCKING. The ADD was implemented as written. Each item below needs an architect decision.

## ARCH-QUESTION 1: the `NSpid` check cannot detect a private pid namespace that mounts its own `/proc` (C-1 a)

**What the ADD says.** AD-001 step 4 and R-2 say that the agent "reads `<ProcRoot>/self/status`, counts the fields of
`NSpid`, and refuses (`Unknown`) when there is more than one field", and that "a `/proc` that belongs to another
namespace cannot pass".

**What the kernel does.** `task_state()` in `fs/proc/array.c` prints `NSpid` from the pid namespace **of the procfs
mount being read** down to the task's own namespace:

```
for (g = ns->level; g <= pid->level; g++) ...
```

So the number of fields is `task level - mount level + 1`.

| Reader | `/proc` mount | `NSpid` fields | Locks visible in `/proc/locks` | ADD check |
|---|---|---|---|---|
| Agent in the initial namespace | initial | 1 | all holders | passes (correct) |
| Agent reading an ancestor's `/proc` (for example a bind-mounted host `/proc`) | ancestor | 2 | all holders | refuses (safe, but not the case C-1 worries about) |
| Agent with `PrivatePIDs=yes`, or in a container, with its **own** `/proc` | its own, non-initial | **1** | **only holders inside that namespace** | **passes: fail open** |

The case that SEC-001 (1) describes, a private pid namespace that omits the host's locks, reads `NSpid` with a single
field. The `NSpid` check therefore does not catch it. The probe would read a partial `/proc/locks`, find no match and
return `Free`.

**Impact now.** Latent. The shipped units have no `PrivatePIDs=`, `ProtectProc=` or `ProcSubset=`, and
`TestAgentUnit_RD_C1_NoPrivatePIDs` fails if one is added. A containerised agent is not a supported deployment.

**What was implemented.** The ADD as written: the `NSpid` check (it still refuses the ancestor-`/proc` case and an
unverifiable namespace), plus the unit-file regression test. No extra check was added, because that would change the
`dpkgProbeEnv` contract the ADD fixes.

**Suggested resolution (architect to decide).** Add an unprivileged check that identifies the initial pid namespace
itself: `readlink /proc/self/ns/pid` equals `pid:[4026531836]`. That value is the kernel constant
`PROC_PID_INIT_INO` (0xEFFFFFFC). Reading one's own `/proc/self/ns/pid` needs no privilege. On any other value, or
when the link is missing, the probe returns `Unknown`. It needs one more function in `dpkgProbeEnv` (`Readlink`) and
three tests. The `NSpid` check can stay as a second signal.

## NOTE 2: ADD §8 C-5 test row and AD-001 disagree on `_`

AD-001 and §10 restrict holder names to `[A-Za-z0-9._+:-]`, which keeps `_`. The §8 row for
`TestSanitizeHolderName_RD_C5_CharsetAndLength` lists `_` among the characters that map to `?`.

Implemented: `_` is kept, as in AD-001, §10 and the security review (C-5). The test asserts `a.b_c+d:e-f` survives.
If the architect wants `_` removed, it is a one-line change in `sanitizeHolderName` and one fixture row.

---

## Resolution (Architect, 2026-10-24, ADD revision 3)

**Status: RESOLVED.** Each decision below is recorded in the ADD's revision 3 changelog. One `fix` and one `test`
commit are needed (ADD §4, "Revision 3 delta").

### ARCH-QUESTION 1: adopted, with `NSpid` kept as a secondary check

- **Primary check (new, AD-001 step 4.1).** `Readlink(<ProcRoot>/self/ns/pid)` must equal `pid:[4026531836]`
  (`PROC_PID_INIT_INO`).
  - A read error → `Unknown`, cause `cannot verify the pid namespace: <err>`.
  - A malformed value → `Unknown`, cause `cannot verify the pid namespace: unexpected link`. The value is never
    echoed.
  - Any other well-formed namespace → `Unknown`, cause `agent is not in the host pid namespace`.
- **`dpkgProbeEnv`** gains `Readlink func(string) (string, error)`; production uses `os.Readlink`. It is not called
  when the probe returns `Absent` (non-dpkg host).
- **Secondary check (kept, step 4.2).** `NSpid` must have exactly one field. It is redundant in the initial namespace,
  but it is cheap, already tested, and still refuses a bind-mounted ancestor `/proc`.
- **Alternatives rejected:**
  - comparing with `/proc/1/ns/pid`: needs ptrace access to pid 1, which means root;
  - `stat` of the nsfs inode: equivalent, but needs the nsfs device;
  - process-name heuristics.
- **Tests (ADD §8 C-1 rows):**
  - the init link passes;
  - the **non-init link fixture** `pid:[4026532481]` with a single-field `NSpid` → `Unknown`, and `/proc/locks` is not
    opened;
  - an unreadable link (`ENOENT`, `EACCES`) → `Unknown`;
  - malformed links (table) → `Unknown`;
  - an init link with nested `NSpid` → `Unknown`;
  - Linux `TestDpkgLock_RD_C1_RealSelfNsPidIsInit`;
  - a `requireInitPidNamespace(t)` helper, so that real-`/proc` tests skip with a reason in Docker.
- **R-2** is rewritten. A `PrivatePIDs=` or containerised agent now fails **closed**.
  `TestAgentUnit_RD_C1_NoPrivatePIDs` stays, as an availability guard.

### NOTE 2: `_` is kept

The ADD §8 C-5 row is corrected to match AD-001, §10 and C-5: `a.b_c+d:e-f` survives. The implementation needs no
change.

### Other deviations in the implementation report

| # | Deviation | Decision |
|---|---|---|
| 3 | The probe and the gate land in one commit | Accepted. §12 note 1 is updated |
| 4 | `readSmallFile` trims a capped read to its last complete line | Accepted and specified in AD-001 ("Small reads"). New test `TestReadSmallFile_RD_C1_NoCompleteLineReturnsEmpty` |
| 5 | A blank `/proc/locks` line gives `Unknown`; the gate's `default` case fails closed | Accepted and specified (AD-001 step 6.1; AD-002 `default`, cause `lock state not recognized`) |
| 6 | `ReaderStopsAtCap` asserts ≤ 1 MiB + 1 byte | Accepted. The §8 AC-011 row is updated |
| 7 | Extra tests | Accepted |
| 8 | The AC-004 test assumes the 5 s guard cap | Accepted. The source is `maintenance.go:38` |

### Added hardening (from the implementation report's notes for QA)

`dpkgLockUnknown` becomes the zero value of `dpkgLockState`, so a zero or hand-built `dpkgLockStatus{}` fails closed
instead of reading as `Absent`. Test: `TestDpkgLockState_RD_C1_ZeroValueIsUnknown`.
