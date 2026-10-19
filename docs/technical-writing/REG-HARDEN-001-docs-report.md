# Documentation Report

**Story ID:** REG-HARDEN-001
**Author:** Technical Writer Agent
**Date:** 2026-10-18

---

## Artifacts Updated

| File | Change Type | Summary |
|------|-------------|---------|
| `README.md` | Updated | Added a **Device Registration and Re-registration** section under Agent Installation, directly after the registration-key paragraph. It covers: new-device rules (unchanged); the six-rule re-registration decision table (other device's key 403, other device's certificate 403, own key accepted with no new key, verified unrevoked mTLS certificate accepted with a new key, registration key accepted only for a device with no API key, everything else 401); notes on each path; the fixed `400`/`401`/`403` bodies and the one-line `WARN` log; the 10/min rate limit; the **Recovering an agent that lost its API key** subsection (FR-027 / AC-023: Option 1 rotate key and paste into `server.api_key` then restart; Option 2 delete the ID file `/etc/riot/id` and clear `agent.device_id` so the agent registers as a new device, then delete the stale device; mTLS devices recover through their certificate); and **Future-dated timestamps** (5-minute clamp, once-per-hour log, read-side guard, `0s ago` in the fleet table). **Corrected the `RIOT_API_KEY` row** in the Server Environment Variables table (security condition C8): it is loaded but not enforced by device registration; only the Settings registration key (database) gates registration; enforcement is a tracked follow-up. Updated the `server.api_key` row of the Agent Config Reference, and the `POST /api/v1/devices/register` and `POST /api/v1/devices/:id/rotate-key` rows of the API tables. |
| `CHANGELOG.md` | Updated | Added `[REG-HARDEN-001]` entries at the top of the existing `[Unreleased]` section under `### Added` (proof-of-device re-registration; recovery procedure; future-timestamp clamp and read guard; internal notes), `### Fixed` (keyed agents' 24-hour re-registration no longer 401s on a server with a registration key; knowing a device ID no longer lets a caller overwrite the record or obtain a key; negative relative age in the fleet table), and `### Changed` (a **BREAKING:** behavior-change entry for the open-server wiped-config agent: it used to be silently re-keyed and now gets `401` until an admin rotates the key; the corrected periodic re-registration on registration-key servers; the changed rejection body text; the `RIOT_API_KEY` documentation correction). Each entry states that existing agents need no configuration change. No version number was invented; the file uses an `[Unreleased]` section and the same `- [STORY-ID] ...` style as earlier entries. |
| `docs/technical-writing/REG-HARDEN-001-docs-report.md` | Created | This report. |

## Artifacts Reviewed, Not Changed

| File | Reason |
|------|--------|
| Inline comments in `register_auth.go`, `ingest_clamp.go`, `handlers.go` (`RegisterDevice`, `Heartbeat`, `Telemetry`), `telemetry_repo.go` | Read against the code. Every comment explains a rule, a security property, or a reason for an ordering (no mutation before the decision, `VerifiedChains` only, guard inside the `DISTINCT ON` subquery, fail-closed on list errors). None restates the code. None is inaccurate, so none was edited. The `TODO(HARDEN-002)` comment on `regKeyEqual` already cites the story (QA fixed it in `901ccd5`). |
| `.env.example` | No new environment variable. |
| `CONTRIBUTING.md` | Does not exist in this repository (same call as earlier stories). The story adds no contributor workflow. The gated disposable-database tests are described in the CHANGELOG internal entry. |
| OpenAPI / GraphQL schema | None exist. The README API tables are the endpoint reference and were updated. |
| `docs/security/REG-HARDEN-001-security-review.md`, FRD, ADD, impl report, QA report | Read in full. Left as written: they are pipeline records, not user documentation. Discrepancies are logged below. |

---

## Operator-Facing Changes (summary)

1. **Re-registering an existing device needs proof.** The device's own key, a verified unrevoked mTLS certificate for that device, or (for a device with no key) the registration key. A device ID alone proves nothing.
2. **Fixed `401`/`403` bodies.** `{"error":"unauthorized"}` and `{"error":"forbidden"}`. The text `invalid registration key` is gone.
3. **Keyed agents on a registration-key server now refresh successfully.** Their 24-hour re-registration used to fail with `401`.
4. **An agent that lost its API key but kept its ID file is no longer re-keyed silently on an open server.** It gets `401` until the key is rotated or the ID file is removed.
5. **Future-dated snapshots and heartbeats are clamped at 5 minutes ahead,** and reads skip already-stored rows beyond that.
6. **No agent configuration change is required** for agents that already hold a key.

---

## Release Gates From QA (status)

| Gate | Status |
|------|--------|
| README registration/auth section (new rules, clamp, new-device rules) | Done |
| README recovery subsection (FR-027 / AC-023) | Done. Paths verified against the code (see Accuracy Flags 1 and 2) |
| README `RIOT_API_KEY` claim (C8) | Done |
| CHANGELOG entry with the behavior-change call-outs (FR-028) | Done |

---

## Stale Content Found (Not Fixed)

Items found outside the scope of this story that need a documentation pass:

| File | Issue | Recommended Action |
|------|-------|--------------------|
| `scripts/install.sh` line 403 | The bootstrap-key reset runs `rm -f "$RIOT_DATA_DIR/device-id"` (`/var/lib/riot/device-id`). The agent reads and writes its ID at `/etc/riot/id` (`IDPath()` in `internal/agent/config.go`), so this line removes nothing. A re-enrollment with `--bootstrap-key` therefore keeps the old ID file. Not changed: it is shell logic. | Raise as a small installer story: point the line at `/etc/riot/id`, then re-test the re-enrollment flow. |
| `docs/requirements/REG-HARDEN-001-frd.md`, `docs/architecture/REG-HARDEN-001-add.md`, `docs/qa/REG-HARDEN-001-qa-report.md` | Name `/var/lib/riot/device-id` as the device ID file. That path is wrong (same stale path as `install.sh`). The README uses `/etc/riot/id`. | Correct the paths in the pipeline documents, or leave them as a record and rely on the README. |
| `docs/requirements/REG-HARDEN-001-frd.md` (recovery, "existing UI button"), QA report Action Required 1 | State that an admin "Rotate key" button exists in the dashboard. `grep -ri rotate web/src` finds no such control. Only the `POST /api/v1/devices/:id/rotate-key` endpoint exists. | Either add a Rotate key action in the device page (a feature story) and then update the README recovery steps, or leave the README as is. |
| `README.md` line 18 | `> **README last updated for v2.38.0**` marker is old. Carried forward from earlier reports. | Update at the next release tag or remove the marker. |
| `README.md` line 12 | "relatively stable at version 2.24" banner is old. | Review wording at the next release. |
| `README.md` Features, "Open registration" and "Per-device API keys" bullets | Still accurate in outline but do not mention that re-registration needs proof. | Optional one-line addition at the next README pass. |
| `README.md` Sudoers Rules table, `sudo systemctl poweroff` row | `install.sh` writes no `poweroff` rule. Carried forward from the AGENT-HARDEN-001 report. | Determine how remote shutdown gains privilege, then correct or delete the row. |
| `scripts/uninstall.sh` | Does not remove `/usr/local/lib/riot/` or `/etc/sudoers.d/riot-agent`. Carried forward from the AGENT-HARDEN-001 report. | Installer-maintenance story. |
| `docs/technical-writing/` | OIDC-001 and AGENT-HARDEN-001 carried-forward items are unchanged and still open. | See those reports. |

---

## Accuracy Flags

Discrepancies between the ADD/FRD/QA documents and what the code does:

| Discrepancy | Location | Documented As |
|-------------|----------|---------------|
| The pipeline documents give the device ID file as `/var/lib/riot/device-id`. The agent uses `/etc/riot/id` on Linux and macOS, and `%PROGRAMDATA%\riot\id` on Windows. `/var/lib/riot/device-id` appears only in `install.sh`'s bootstrap-key reset, which therefore deletes nothing. | FRD FR-027, QA report Action Required 1, `internal/agent/config.go` `IDPath()`, `scripts/install.sh:403` | README uses `/etc/riot/id` and the Windows path (what the code reads). |
| The agent reads `agent.device_id` from `agent.yaml` before the ID file. Deleting only the ID file does not make the agent register as new if `agent.device_id` is set. The FRD recovery text does not mention it. | `internal/agent/register.go` lines 15-20 | README tells the operator to remove `agent.device_id` too. |
| The FRD and QA report say the admin recovery uses an existing UI "Rotate key" button. No such control exists in `web/src`. The API endpoint exists and is admin-authenticated. | FRD sections on actors and FR-026, QA Action Required 1; `internal/server/router.go:190` | README documents the API call (login, then `POST .../rotate-key`) and says plainly that the dashboard has no Rotate key button. |
| QA report C8 says README line 139 claimed `RIOT_API_KEY` gates registration. The code does load it (`internal/server/config.go:62`; `server.go:307` lets it block the database value in `Config.RegistrationKey`), but `RegisterDevice` reads only `adminRepo.GetConfig("registration_key")` (`handlers.go:181`), never `Config.RegistrationKey`. | `internal/server/handlers/handlers.go:181` | README states the variable is loaded but not enforced by device registration, and that enforcement is a tracked follow-up. |
| The implementation report names the merge comment `TODO(merge)`. QA renamed it `TODO(HARDEN-002)`. | `internal/server/handlers/register_auth.go` | No documentation impact. |

---

## Notes for Future Writers

**Where the registration rules live.** There is no separate registration document. The operator material is the README section "Device Registration and Re-registration" (under Agent Installation), including the recovery subsection. If enforcement of `RIOT_API_KEY` ships, update the callout in that section and the `RIOT_API_KEY` row in the environment variable table together.

**Keep two statements precise.** (1) The registration key is read from the database setting only. (2) A device ID is not a secret and never proves identity. Earlier README wording blurred the first.

**Do not call the endpoint "authenticated".** It stays public: new-device registration is open unless a registration key is set. Only re-registration of an existing device requires proof.

**Paths.** Verify any file path against the agent code, not against `install.sh` or the pipeline documents. The installer has at least one stale path (see Stale Content).

**CHANGELOG order.** New entries go at the top of each subsection. The file has older `### Changed` and `### Added` blocks further down for released versions. The new entries went into the first `[Unreleased]` blocks only.

**Line endings.** `README.md` and `CHANGELOG.md` are CRLF in this Windows checkout (`core.autocrlf=true`); the index is LF. Edits preserved the existing endings (the diff touches only intended lines).

**Verified.** Every behavior in the README section was read from `register_auth.go`, `handlers.go` (`RegisterDevice`, `RotateKey`, `Login`), `ingest_clamp.go`, `telemetry_repo.go`, `internal/agent/register.go`, `internal/agent/config.go`, `internal/agent/agent.go` (24-hour loop), `internal/agent/http_client.go`, and `web/src/utils/format.ts`. The `curl` recovery commands were not run against a live server (no server is available in this environment); they use the login and rotate-key routes and JSON shapes read from `auth.go` and `handlers.go`.
