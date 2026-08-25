# PATCH-GATE Documentation Report

**Story ID:** PATCH-GATE — Reboot-Class Package Gating (OS-Level Holds, Maintenance-Window Apply, Auto-Reboot)
**Writer:** Technical Writer Agent
**Date:** 2026-08-25
**Trigger:** QA verdict PASS WITH NOTES

---

## Files Touched

### `README.md`

- **New "Reboot-Class Package Gating" section** (placed after Security Score, before Fleet Dashboard). Covers: the motivating incident (GPU driver/kernel-module mismatch breaking GPU containers until reboot); the definition of reboot-class (NVIDIA/AMD GPU drivers + kernel/dkms) with an apt/dnf pattern table and the deliberate exclusions (`linux-firmware`, container toolkit, ROCm user-space); platform support (apt + dnf5 only; dnf4/pacman/apk/Windows/macOS not supported for holds); the **two-sided opt-in** enablement runbook (re-run installer → agent `hold_reboot_class`/`allow_patching`/`allow_reboot` → server `reboot_class: gated`); the in-window release/apply/re-apply/auto-reboot flow; reboot-required detection + the seeded alert; GPU-container blast-radius surfacing; and the `hold_enforcement` status table (`active`/`no_privilege`/`unsupported`).
- **Features list** — added a "Reboot-class package gating" bullet next to "Automation scheduling".
- **Sudoers Rules table** — added the four new argument-locked rules (`apt-mark hold *`, `apt-mark unhold *`, the fixed-path dnf `install`, the exact-path `rm`), plus a **"Reboot-class holds on existing agents"** note that an in-place `agent_update` does not rewrite the sudoers file, so the installer must be re-run, and that the agent fails closed with a "Hold enforcement inactive" warning (`hold_enforcement: no_privilege`) until then.
- **Agent Config Reference table** — added `commands.hold_reboot_class` (default `false`).
- **Manual Install YAML example** — added the `hold_reboot_class: false` line under `commands:`.
- **`updates` collector row** — extended to mention per-update classification and reboot-required detection.
- **API table** — extended the `GET /api/v1/fleet/patch-status` row to document the new `reboot_class_count` / `reboot_required` fields and the `?detail=true` per-update `class` field.

### `CHANGELOG.md`

- Added eight `[PATCH-GATE]` entries under `### Added` covering: classification + `pending_reboot_class_count`; OS-level holds and the two-sided opt-in; in-window apply + auto-reboot gated on `allow_reboot`; reboot-required detection + the seeded `reboot_required` event/rule; the scoring-engine kernel-check fix (dead field now populated); GPU-container blast-radius flagging; fail-closed hold-enforcement status; and the extended patch-status payload / safe-default (off) behavior.
- Added two `[PATCH-GATE]` entries under `### Changed`: the `scripts/install.sh` sudoers widening (with the re-run-installer requirement), and the two new config knobs (`commands.hold_reboot_class`, `os_patch.reboot_class`).

### Inline code comments

Audited the shipped non-test source for missing "why" comments. **None added** — the load-bearing decisions already carry why-comments and adding more would be churn:
- `internal/agent/config.go:275-293` — `HoldStatePath`/`DNFHoldsStagedPath` document that the staged path is the fixed sudoers source that must match install.sh byte-for-byte.
- `scripts/install.sh:446-464` — the apt-mark and dnf-fragment rules carry inline rationale (subcommand locking, fixed paths, "must match the Go constants byte-for-byte", "no sh -c, no tee, no wildcard").
- `internal/models/telemetry.go:264-274` — `PendingRebootClassCount`, `HeldPackages`, and `HoldEnforcement` document the fail-closed "empty never reads as protected" invariant.
- `internal/agent/doctor.go:260-262` — `checkHoldEnforcement` documents that it reuses the runtime preflight probes.

## API Documentation

The repository's API reference is the table in `README.md` (there is no separate `docs/` API doc). The `GET /api/v1/fleet/patch-status` row was updated in place — no new endpoint was added. New fields (`reboot_class_count`, `reboot_required`, per-update `class`) and the automation-config field (`os_patch.reboot_class`, applied via the existing `PUT /api/v1/settings/automation`) all ride existing endpoints and are documented in the README section and changelog.

## Config-Key Verification

Verified every config key named in the docs against the shipped source:
- Agent flag: **`commands.hold_reboot_class`** (`internal/agent/config.go:36`, default `false`; present in `defaultConfigTemplate` at `:196`).
- Server policy: **`os_patch.reboot_class`** = `"off"` (default) | `"gated"` (`internal/models/automation.go:24`).
- Telemetry: `pending_reboot_class_count`, `held_packages`, `hold_enforcement`, `reboot_required`, `reboot_required_reasons`, per-update `class`, container `uses_gpu` (`internal/models/telemetry.go`).
- Sudoers rule shapes and paths verified against `scripts/install.sh:441-469`.

## Stale Content Found

The following are within the PATCH-GATE story scope but are not artifacts the technical writer owns (upstream pipeline doc / implementation code). Flagged here per the stale-documentation protocol rather than edited inline:

1. **Impl report names the wrong config key.** `docs/implementation/PATCH-GATE-impl-report.md` repeatedly refers to the agent flag as `commands.hold_enforcement` (e.g. the "What the rule does" and "Deploy-time action items" sections). The **shipped** flag is `commands.hold_reboot_class` (per `internal/agent/config.go` and the generated `defaultConfigTemplate`). The FRD/ADD both use `hold_reboot_class`; the impl report is the outlier. The user-facing docs were written against the shipped key. The impl report is an upstream document I do not own — recommend the team correct it so future readers are not misled.

2. **FR-033 settings-UI toggle appears undelivered.** FR-022/FR-033 specified a reboot-class policy toggle on the OS-patch window in the Agent Management settings page. The `reboot_class` field exists in the TS model (`web/src/types/models.ts:737`) and the server accepts/validates it, but a grep across `web/src` finds no editor control — `web/src/pages/settings/AgentManagement.tsx`'s `WindowCard` renders schedule/cooldown/stagger only, and the impl report's Files Changed table does not list `AgentManagement.tsx`. The QA report's notes flagged three device-detail test gaps but did not flag this. Because there is no UI control, the README documents enabling the server policy via the automation config / `PUT /api/v1/settings/automation` (which is accurate for the shipped build). Recommend the team confirm whether the toggle is intended for this release; if so it is a functional gap, not just a docs gap.

No stale content found **outside** the PATCH-GATE scope.

## Documentation Status

README and CHANGELOG are current and accurate against the shipped code. Only `README.md` and `CHANGELOG.md` were modified — no `.ts`/`.tsx` or `.go` source was touched, so no `tsc`/`go build` verification was required. The two items above are surfaced for the engineering team.
