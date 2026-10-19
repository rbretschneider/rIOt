# Documentation Report
**Story ID:** GROWTH-001, PR-C (agent-side bounds)
**Author:** Technical Writer Agent
**Date:** 2026-10-19

## Audience

- **Developers and operators** of agent hosts: installers, people who edit `agent.yaml`, people debugging a stuck or restarting agent.
- **No end-user (dashboard) impact.** PR-C changes only the agent, the unit file and the update verifier. No API, schema or UI change.

## Artifacts Updated

| File | Change Type | Summary |
|------|-------------|---------|
| README.md | Updated | Rewrote "Agent Resource Bounds" into subsections: configurable limits, container and journal logs, offline buffer, server answers, first registration and config directory, self-update, memory limit. Added the config-directory write requirement (with a table of manual-install setups that break), the pre-flight wait on a read-only directory, 408 in the keep list, 5xx `Retry-After`, the first-pass 1,000-line read, the "payload bytes plus SQLite overhead" wording for the buffer cap, and the doctor Memory Limit checks. Added two bullets to "Until a host is re-run", a pointer in Manual Install step 3, and shortened the doctor paragraph to a pointer |
| CHANGELOG.md | Updated | Tightened the PR-C entries. Added a "Changed" group: atomic `Config.Save` (BREAKING for some manual installs), the memory-limit unit lines with the install.sh re-run and the accepted residual, the first-pass and journal limits, and the identity pre-flight wait |
| internal/agent/config.go | Inline comment | `Config.Save` now says why the config directory must be writable and which setups break |
| internal/agent/doctor.go | Inline comment | Memory Limit section comment says why the check exists (the unit is not rewritten by a binary update) and that the helper sweep cannot be detected |
| scripts/riot-agent.service | Comment | Explains the three memory lines and the cgroup residual, matching `install.sh` |
| docs/technical-writing/GROWTH-001-PR-C-docs-report.md | Created | This report |

No FRD, ADD or security review text was copied into the repository. The text contains no host names, LAN addresses, local paths, domains or personal names (checked by search of the added lines).

## Verification

- `go build ./...` and `go test ./internal/agent/...` pass after the comment edits (both `internal/agent` and `internal/agent/collectors`).
- Every documented number was checked against the code: the six keys' defaults and ranges (`agentLimitSpecs` in `config.go`), the status classification (`classifyPush` in `telemetry.go`), the back-off constants and 5xx `Retry-After` handling (`http_client.go`), the pre-flight probe size and delays (`register.go`), `maxBuffered = 100` (`buffer.go`), the unit lines (`install.sh`, `riot-agent.service`), and the doctor messages (`doctor.go`).
- The installer chowns the config directory to the agent user (`install.sh`, line with `chown "$RIOT_USER:$RIOT_USER" "$RIOT_CONFIG_DIR"`), which is why installer installs are documented as unaffected.
- Not run: the `install.sh` re-run and `systemctl show` on a real host. The Memory Limit text is documented from the code and its tests, not from a live unit.

## Doctor and inline comment review

- **Doctor messages.** The Collection Limits and Memory Limit sections tell the reader what is wrong and what to do (`Re-run the install script`). The messages name the consequence ("a runaway collector can exhaust the host"). No change was needed.
- **Pre-flight log message** (`cannot write the device identity; not registering`) names the state and carries the error and retry delay. It is quoted in the README so an operator can search for it.
- **Existing comments** in `buffer.go`, `telemetry.go`, `register.go`, `updater.go`, `docker_logs.go`, `logs.go` and `cmd/riot-agent-apply/main.go` explain the reason (business rule, fail-safe, ordering) and reference the story. None restates the code and none is a bare TODO, so none was deleted.
- **Gaps closed:** `Config.Save` (the new directory-write requirement had no comment), the doctor Memory Limit comment, and the service file.

## Stale Content Found (Not Fixed)

| File | Issue | Recommended Action |
|------|-------|--------------------|
| README.md "Features" (offline resilience bullet) | Says only that the agent "buffers telemetry locally". It does not mention the caps | Optional one-clause link to Agent Resource Bounds |
| README.md Manual Install (Windows) | The Windows paragraph does not say whether the config directory is writable by the service account | Confirm on a Windows host, then add one sentence. Not verified here |
| docs/implementation/GROWTH-001-PR-C-impl-report.md | Section "Notes for QA" and the bounds table say "at most 50 MiB on disk". QA note 3 says "about 50 MiB plus SQLite overhead" is exact | Leave as a historical record. The README and CHANGELOG use the exact wording |
| Repository line endings | The working tree is CRLF with `core.autocrlf=true` while the index is LF. Editing with LF-only tools adds a Git warning, not a content change | Note for future writers |

## Accuracy Flags

| Discrepancy | Location | Documented As |
|-------------|----------|---------------|
| ADD says a permanent 4xx is anything except 401, 403, 429. Code also keeps 408 | `classifyPush` in `telemetry.go` | 408 keeps the item (README table, CHANGELOG) |
| FRD asks `MemoryMax=256M`. Code and ADD use `MemoryMax=50%`, `GOMEMLIMIT=200MiB`, `OOMPolicy=continue` | `install.sh`, `riot-agent.service` | The code values |
| Implementation report says the bound is "50 MiB on disk". The cap counts payload bytes | `buffer.go` | "about N MiB plus SQLite overhead" |
| The previous README said the unit "uses `KillMode=control-group`". `install.sh` does not set `KillMode`; it is systemd's default | `install.sh` unit | "systemd's default `KillMode=control-group`" |
| Doctor's hint text abbreviates the install command (curl, ellipsis, pipe to sudo bash) | `reinstallHint` in `doctor.go` | Not changed. The README gives the full command under "Upgrading existing agents" |

## Notes for Future Writers

- README and CHANGELOG are long single files. "Agent Resource Bounds" is the home for agent limits. Link to it rather than repeating numbers.
- The CHANGELOG `[Unreleased]` block is grouped Security, Removed, Added, Fixed, Changed. A GROWTH-001 entry for the server side (PR-A1) will land in the same groups.
- Doctor output is the first place operators look. When a change needs an installer re-run, make doctor fail or warn and say so in the "Until a host is re-run" list.
- The accepted residual (an agent restart or a cgroup out-of-memory event can interrupt `apt` or `dpkg`) must stay in the README until AGENT-PRIV runs package managers in their own transient unit. Remove the warning box and the CHANGELOG sentence together with that change.
