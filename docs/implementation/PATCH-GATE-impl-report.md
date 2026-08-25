# PATCH-GATE Implementation Report

**Story:** PATCH-GATE — reboot-class (GPU driver / kernel) packages must wait for maintenance windows and trigger a reboot
**Status:** Implementation complete; tests green; ready for QA review.

## Detected Stack

- **Backend:** Go 1.x, chi v5, pgx v5, slog. Agent collectors shell out to apt/dnf/apt-mark/nvidia-smi via `exec.Command` (arg slices, no shell).
- **Frontend:** React + TypeScript, Tailwind v4, vitest + Testing Library.
- **DB:** PostgreSQL 16 — **no migration**. All new telemetry/automation/event fields are additive JSON with `omitempty`; automation policy rides the existing `admin_config` JSON blob.

## What the rule does

The incident: a GPU driver was patched out-of-band while Docker containers depended on the NVIDIA runtime, breaking them until a reboot. PATCH-GATE closes that gap with a two-sided opt-in:

1. **Classification** — the updates collector classifies each pending package as `gpu_driver`, `kernel`, or standard, via a single shared pattern table.
2. **OS-level holds** — when hold enforcement is enabled (server policy `OSPatch.reboot_class = "gated"` **and** the agent's `commands.hold_enforcement` flag), the agent keeps installed reboot-class packages continuously held (`apt-mark hold` / a rIOt-owned dnf `excludepkgs` fragment) so a manual `apt upgrade` or unattended-upgrades cannot pull them outside a window.
3. **In-window apply + auto-reboot** — the auto-patch orchestrator sets `include_reboot_class` on `os_update` only when the maintenance-window gate passes. The agent releases rIOt-managed holds, applies, re-applies holds on every exit path, and — if the run actually upgraded a reboot-class package — reboots automatically (absolutely gated on `commands.allow_reboot`; otherwise it raises a `reboot_required` event).
4. **Reboot-required detection** — the agent reports `/var/run/reboot-required` (apt) / `dnf needs-restarting -r` (dnf); the server emits a once-per-transition `reboot_required` event and the scoring engine's kernel check now works (the previously dead `PendingKernelUpdate` field is populated).
5. **Blast-radius visibility** — the Docker collector reads `HostConfig.DeviceRequests`/`Devices` to flag GPU-dependent containers, surfaced as a per-container GPU badge.

Defaults ship fully off; behavior is byte-compatible with the prior release when disabled.

## Files Changed

### Created (backend)

| File | Purpose |
|---|---|
| `internal/agent/collectors/rebootclass.go` (+`_test.go`) | Shared classification pattern table (`ClassifyPackage`) and `ValidPackageName` allowlist regex (SEC-AC-003) |
| `internal/agent/collectors/holds.go` (+`_test.go`) | `HoldManager`: privilege preflight, apt-mark/dnf-fragment enforcement, `holds.json` bookkeeping, marker-first release + defer-scoped re-apply, disable/reconcile |
| `internal/server/events/reboot_required_test.go` | Once-per-transition `reboot_required` event coverage (AC-019) |

### Created (frontend)

| File | Purpose |
|---|---|
| `web/src/components/RebootClassBadge.tsx` (+`.test.tsx`) | Violet GPU-driver / amber kernel badge on pending-update rows (AC-020) |
| `web/src/components/CompactContainerTile.test.tsx` | GPU-badge surfacing tests (AC-022) |

### Modified (backend)

| File | Purpose |
|---|---|
| `internal/models/telemetry.go` | `PendingUpdate.Class`; `UpdateInfo` reboot-class count, `HoldEnforcement`, `RebootRequired`/reasons, populated `PendingKernelUpdate`/`Version`; container `UsesGPU` |
| `internal/models/automation.go` | `MaintenanceWindow.RebootClass` (`off`/`gated`) policy |
| `internal/models/events.go` | `reboot_required` event type |
| `internal/agent/collectors/updates.go` (+`_test.go`) | Classify each update, populate kernel fields, reboot-required detection, report hold-enforcement status |
| `internal/agent/collectors/docker.go` (+`_test.go`) | `usesGPU` helper at existing inspect sites (zero extra Docker API calls) |
| `internal/agent/collectors/collector.go` | Wire HoldManager reconcile into the collection cycle |
| `internal/agent/agent.go` | Startup hold reconciliation |
| `internal/agent/commands.go` (+`_test.go`) | `include_reboot_class` handling, before/after version snapshot, post-run auto-reboot gated on `allow_reboot` |
| `internal/agent/config.go` | `commands.hold_enforcement` flag (default off) |
| `internal/agent/doctor.go` (+`_test.go`) | Sudoers-rule preflight probes with re-run-installer remediation (SEC-AC-004) |
| `internal/server/handlers/auto_update.go` (+`_test.go`) | Window gate sets `include_reboot_class` only in-window |
| `internal/server/handlers/commands.go` (+`_test.go`) | Strip `include_reboot_class` from manual dispatches (BR-005) |
| `internal/server/handlers/fleet.go` (+`fleet_handler_test.go`) | `reboot_class_count` / `reboot_required` in patch-status payload (AC-021) |
| `internal/server/handlers/handlers_test.go` | Old-agent/old-server compatibility (AC-024) |
| `internal/server/events/generator.go`, `templates.go` | `reboot_required` metric, template, once-per-transition emission |
| `internal/server/server.go` | Seeded `reboot_required` alert rule |
| `internal/server/scoring/engine_test.go` | Kernel-check regression now that `PendingKernelUpdate` is populated (AC-007) |
| `scripts/install.sh` | Four exact, argument-locked sudoers rules for apt-mark + fixed-path dnf fragment writer (SEC-AC-001/002) |

### Modified (frontend)

| File | Purpose |
|---|---|
| `web/src/types/models.ts` | Mirror new fields (`class`, reboot-class count, `hold_enforcement`, `reboot_required`, `uses_gpu`, `reboot_class` policy) |
| `web/src/api/client.ts` | `DevicePatchInfo` reboot-class/reboot-required fields; `getPatchStatusDetail` |
| `web/src/api/demo-data.ts` | Demo patch-status entries with reboot-class scenarios |
| `web/src/pages/FleetOverview.tsx` (+`.test.tsx`) | Reboot-required badge, reboot-class count, `RebootClassBadge` in the patch modal (AC-021) |
| `web/src/pages/DeviceDetail.tsx` | Held packages, reboot-required flag, hold-enforcement-inactive warning |
| `web/src/components/CompactContainerTile.tsx` | GPU badge when `uses_gpu` (AC-022) |

## AC-to-Test Mapping

| AC | Coverage |
|---|---|
| AC-001–004 (apt/dnf gpu_driver + kernel classification, precedence) | `rebootclass_test.go` |
| AC-005 (standard packages unclassified; shared table) | `rebootclass_test.go`, `updates_test.go` |
| AC-006 (PendingKernelUpdate/Version populated) | `rebootclass_test.go`, `updates_test.go` |
| AC-007 (scoring kernel check fixed) | `scoring/engine_test.go` |
| AC-008–011 (apt holds applied; dnf fragment; released in-window + re-applied all exit paths; disable removes only ours) | `holds_test.go` |
| AC-012 (out-of-window manual/unattended blocked) | `holds_test.go` |
| AC-013 (out-of-window runs exclude reboot-class; window gate) | `commands_test.go`, `auto_update_test.go`, `commands_test.go` (server strip) |
| AC-014 (in-window apply + auto-reboot when allow_reboot) | `commands_test.go` |
| AC-015 (allow_reboot false → event, no reboot) | `commands_test.go` |
| AC-016 (no reboot when no reboot-class package applied) | `commands_test.go` |
| AC-017–018 (reboot-required detection apt/dnf) | `updates_test.go` |
| AC-019 (reboot_required event once per transition) | `events/reboot_required_test.go` |
| AC-020 (reboot-class badges on update rows) | `RebootClassBadge.test.tsx` |
| AC-021 (held packages + reboot-required flag in UI/API) | `fleet_handler_test.go`, `FleetOverview.test.tsx` |
| AC-022 (GPU-container count + per-container badge) | `docker_test.go`, `CompactContainerTile.test.tsx` |
| AC-023 (feature off → behavior unchanged) | `holds_test.go`, `commands_test.go`, `auto_update_test.go` |
| AC-024 (old-agent/old-server compatibility, no migration) | `handlers_test.go` |
| SEC-AC-001 (fail-closed privilege preflight, HoldEnforcement status) | `holds_test.go` |
| SEC-AC-002 (fixed-path, argument-locked writer; no wildcard/sh -c/tee) | `holds_test.go` + `scripts/install.sh` inspection |
| SEC-AC-003 (package-name charset validation before argv/fragment) | `rebootclass_test.go`, `holds_test.go` |
| SEC-AC-004 (doctor sudoers probes + remediation) | `doctor_test.go` |
| SEC-AC-005 (release cross-check state∧reboot-class; fail closed on parse error) | `holds_test.go` |

## Test Results

```
Go (go test ./...):        all packages pass (exit 0)
go vet ./...:              clean (exit 0)
go build ./...:            clean (exit 0)
Frontend (npm run test:run): 24 files, 338 tests passed
TypeScript (tsc --noEmit): clean (exit 0)
```

New frontend tests added this pass: `RebootClassBadge.test.tsx` (4), `CompactContainerTile.test.tsx` (3), `FleetOverview.test.tsx` (+2 AC-021).

## Notable Design Decisions

1. **Two-sided opt-in.** Holds require both the server policy (`OSPatch.reboot_class = "gated"`) and the agent flag (`commands.hold_enforcement`). A compromised server alone cannot force reboot-class changes, and the agent's `allow_patching`/`allow_reboot` remain the final veto.
2. **Fail-closed privilege preflight (SEC-AC-001).** Before mutating hold state the agent runs `sudo -n` probes; on failure it never attempts the mutation, logs an ERROR, and reports `hold_enforcement = "no_privilege"` (surfaced as a red UI warning) — the feature fails visibly, never silently. This also backstops upgraded fleets where `agent_update` did not re-run `install.sh` (SEC-AC-004).
3. **Marker-first release + defer-scoped re-apply.** The release marker is persisted before any unhold, and re-apply is `defer`red across every exit path (including panic). A mid-run crash is reconciled on the next collection cycle and at startup.
4. **No new collector.** Reboot-required detection and classification ride the existing `updates` collector, so no agent collector-whitelist change is required.
5. **Zero extra Docker API calls.** GPU-container detection reads `HostConfig` already fetched in the existing `ContainerInspect` loop.

## Deploy-time action items (for the technical writer / operator)

These are configuration/rollout steps, not code — they must be documented and performed per host:

- **Re-run `scripts/install.sh`** on existing agents to install the new sudoers rules (an in-place `agent_update` does **not** do this). The doctor check and runtime preflight will flag hosts that are missing them.
- **Agent config**: set `commands.hold_enforcement: true` and ensure `commands.allow_patching: true` / `commands.allow_reboot: true` on hosts where the rule should fully self-drive. Without `allow_reboot`, the agent raises a `reboot_required` event instead of rebooting.
- **Server policy**: set the OS-patch maintenance window's reboot-class policy to `gated` and configure the window (Settings → Agent Management).

## Remaining pipeline stages

- QA validation (`docs/qa/PATCH-GATE-qa-report.md`)
- Technical writing: README, CHANGELOG, agent-config docs (the deploy-time items above), API docs for the extended patch-status payload.
