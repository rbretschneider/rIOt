> **RESOLVED by ADD revision 3** (`docs/architecture/SHUTDOWN-DPKG-001-add.md`, Changelog): the architect accepted both AQ-1 and AQ-2 as implemented.

# Blockers and ARCH-QUESTIONs

- **Story ID:** SHUTDOWN-DPKG-001
- **Author:** Senior Dev Agent
- **Date:** 2026-10-25
- **Status:** NO BLOCKERS. Two non-blocking ARCH-QUESTIONs, each resolved with the most conservative assumption. Implementation was not stopped. Architect review requested (see the implementation report, Deviations).

## AQ-1: C-4(b) forbids the poweroff text that §4 places in `poweroff.go`

- **Conflict.** ADD §4 lists `errPoweroffStillRunning` in `internal/agent/poweroff.go`, and AD-007 fixes its text as `a previous poweroff command is still running`. AD-006.3 rule (b) (security condition C-4) flags any string matching `poweroff` or `halt` anywhere under `internal/agent/...` outside the `execReboot` and `execShutdown` function bodies. The package-level variable is such a string, so the C-4 scan would fail on the code the ADD specifies.
- **Assumption (conservative).** Keep C-4 exactly as written, and change the code. The error is created inside `execShutdown`, which is exempt. The result text is unchanged: `shutdown: a previous poweroff command is still running`. The unclassified-launch fallback text in `classifyPoweroff` was reworded from "poweroff command did not start" to "shutdown command did not start" for the same reason. The `errPoweroffStillRunning` variable no longer exists.
- **Ask.** Confirm, or relax rule (b) for `poweroff.go`.

## AQ-2: the reaper Warn has no command id

- **Conflict.** ADD §6 shows the late-failure log line with `command_id=<uuid>`, but AD-002 and AD-006.1 fix `execShutdown(p powerPermit) error` with one argument, and the permit carries no command id.
- **Assumption (conservative).** Keep the signature. The Warn line carries `error` only: `shutdown: poweroff command failed after the start check error="<sanitized>"`. At most one such line per command, and at most one child per agent, so the line is still attributable in practice.
- **Ask.** Confirm, or approve adding a command id to the permit or to a second `execShutdown` argument.
