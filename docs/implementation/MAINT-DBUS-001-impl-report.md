# Implementation Report

- **Story ID:** MAINT-DBUS-001
- **Title:** Install the D-Bus system bus during onboarding and name the cause of maintenance refusals
- **Date:** 2026-10-23
- **Stack:** Go 1.24 agent (testify), bash installer with Python root-container fixtures. No server, schema, API or frontend change.

## Problem

PiVPN (DietPi, armv7) refused every host action after its 2.74.2 root install with
`independent OS maintenance is active or requires recovery`, and its automatic agent
update deferred indefinitely at INFO level. No maintenance had ever run.

Root cause: DietPi minimal images do not ship `dbus`. The agent runs as `riot`, and
unprivileged `systemctl` reaches systemd only over `/run/dbus/system_bus_socket`.
`systemctl show riot-maintenance.service` therefore failed, and `maintenanceBusy`
treats an uninspectable unit as busy (intended fail-closed behavior). Root uses
systemd's private socket, so the installer and a sudo-run `doctor` never saw it.
Debian's `systemd` only Recommends `dbus`; distributions where systemd hard-depends
on it are unaffected.

## Changes

| File | Change |
|---|---|
| `scripts/install.sh` | Linux: when the bus socket is missing, install `dbus` (apt: `--no-install-recommends`, one `apt-get update` retry; dnf), `systemctl enable --now dbus.socket` (fallback `start dbus.service`), wait up to 5 s for the socket, else refuse. Runs after release verification and before the maintenance lock, native package locks, policy install and agent stop, so a failure changes nothing managed. |
| `internal/agent/maintenance.go` | `maintenanceBlocker` returns the reason (running, uninspectable unit, unreadable state, unfinished run, unrestored holds); `maintenanceBusy` wraps it. `systemctlProbeError` keeps systemctl's first output line and adds a D-Bus hint. |
| `internal/agent/commands.go` | Host-action refusal message is `<reason>; refusing conflicting host action`. |
| `internal/agent/updater.go` | Deferred automatic update logs the reason; WARN unless the worker is genuinely running. |
| `internal/agent/maintenance_linux.go` | Readiness first requires the bus socket (independent of the caller's uid). |
| `internal/agent/maintenance_doctor.go` | Doctor prints the blocker reason. |
| README, `docs/ops/os-maintenance.md`, CHANGELOG | Document the single package exception and the new messages. |

Cross-platform: maintenance presence is Linux-only (`present()` is false elsewhere, readiness
returns `ErrUnsupported` on non-Linux), the bus check lives in `maintenance_linux.go`, and the
installer change is inside its Linux branch. macOS/Windows behavior is unchanged; the agent
builds for linux, darwin and windows.

Bounded growth: one package install per installer run, at most one list refresh; no loops
beyond a 50 x 100 ms socket wait; no new persisted state.

## Acceptance criteria and tests

| AC | Test |
|---|---|
| AC-001 missing bus refusal names the cause | `TestMaintenance_DBUS001_AC001_MissingSystemBusRefusalNamesTheCause` |
| AC-002 each blocker has a distinct reason; clean states do not block | `TestMaintenance_DBUS001_AC002_EveryBlockerReportsADistinctReason` |
| AC-003 probe errors keep systemctl's diagnosis | `TestMaintenance_DBUS001_AC003_ProbeErrorsKeepSystemctlDiagnosis` |
| AC-004 readiness requires a real socket | `TestMaintenance_DBUS001_AC004_ReadinessRequiresTheSystemBusSocket` (linux) |
| AC-005 present bus: no package operation | `test_DBUS001_AC005_present_system_bus_needs_no_package_operation` |
| AC-006 missing bus: dbus installed and started before agent stop | `test_DBUS001_AC006_missing_system_bus_is_installed_and_started_before_stop` |
| AC-007 failed dbus install preserves the running install | `test_DBUS001_AC007_failed_dbus_install_preserves_running_install` |

## Test runs

- Baseline `go test ./...` (Windows): green before changes.
- `go test ./...` (Windows): green after changes; cross-build linux/darwin/windows OK.
- golang:1.24 container: `go vet` + `go test -race` for `internal/agent`, `internal/maintenance`, `cmd/riot-agent-apply`, `cmd/riot-maintenance`, `scripts`: all ok; touched files gofmt-clean.
- debian:bookworm-slim installer fixture (CI step): 19/19 pass.
- Real package check: `dbus` installs and ships `dbus.socket` on debian:bookworm, debian:trixie, ubuntu:24.04 (apt) and fedora:41 (dnf).

## Not covered

A real systemd PID 1 run of the dbus branch (containers have no systemd); validated on
PiVPN by the manual equivalent (`apt-get install dbus` + `enable --now dbus.socket`) which
restored `LoadState=loaded / ActiveState=inactive` for the `riot` user.
