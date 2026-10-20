# DOCKER-UX-001 QA report

## Resource workspace redesign — 2026-10-20

Product direction supersedes the earlier nested fleet-tab layout: use Komodo's resource-oriented navigation and neutral, dense presentation, integrated with rIOt diagnostics. Compared against the official public Komodo demo (`https://demo.komo.do`, v2.3.3 at review) and [resource documentation](https://komo.do/docs/resources). This is an independently implemented adaptation; no Komodo source was copied.

The candidate replaces repeated Docker headings/tabs/breadcrumbs with persistent grouped navigation, a wider content area and one resource header. Hosts exposes stored/live log support and inventory state, with links scoped to each host. Containers have a compact default column set, persisted column chooser, fixed widths, pinned identity, resource meters and direct Logs shortcuts. Detail return links preserve collection filters; switching containers retains the diagnostic tab. Stack path settings are on Compose and auto-update is on Services; unsafe path warnings remain visible. The shell renders the selected route without waiting for host-status data.

Stored/live log implementation, retention, gap markers, stream filtering, download, masking, action confirmation and cleanup previews are preserved. Compose remains read-only under the settled product requirement. Git-backed deployment, mutable desired-state deployment definitions, builds, registries, resource sync and Swarm are not introduced by this frontend pass; those require new backend/product design rather than cosmetic parity controls.

Browser checks used simulated data at 390, 1280 and 1920 px: the page remains within the viewport, wide data scrolls inside the table, selection stays 44 px, pinned Name stays 200 px with a 44 px offset, and actual row height is 44 px. Browser computed scrollbar width is `thin`. Container Logs retains stored history and its Live toggle in the redesigned frame. These checks do not claim a production deployment or a live agent streaming test.

New regressions cover virtual scroll/filter/resize and keyboard behavior, nested controls, saved column choices, filter-preserving direct Logs navigation, host capabilities/search/scoped links, feature gating, and child-route rendering during host status loading/failure.

Validation: **80 frontend files / 1,499 tests pass**. Production TypeScript/Vite build passes; the existing bundle-size advisory remains. Anonymous login regressions are included. Preview uses the repository's simulated data, with no production lifecycle or cleanup action invoked.

## Post-deployment Docker query timeout

Hawaii exposed another gap in the small-fixture/demo validation: `/api/v1/docker/hosts` returned 500 after 30.49–30.52 seconds, while `/api/v1/fleet/containers` returned 200 after 53.80 seconds. These reads do not contact agents. The global `DISTINCT ON` latest-snapshot queries inspected historical JSON before selecting one row per device; the hosts read exceeded its 30-second transaction timeout.

The replacement uses a lateral, timestamp-ordered `LIMIT 1` lookup for each registered device against the existing device/time index. Docker omission fallback, future-time guards and the allowlist applied before database transfer remain intact. No migration or production database write is needed.

Read-only `EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON)` of the complete replacement SQL on Hawaii, bounded to five seconds: hosts **2.072 ms**, containers **7.488 ms**, four device rows each. These are database execution measurements, not end-to-end browser timings. Regression coverage checks multiple hosts, no history, an explicit Docker-disabled snapshot, omitted Docker and a future-dated snapshot. Deployment is separate from this candidate-query verification.

## Post-deployment login regression and hotfix

The first production deployment exposed a gap in the earlier demo-based browser review: App mounted the protected feature-settings query before authentication. Its expected 401 invoked the API client's full-page reload, repeating the request and preventing the public OIDC availability result from rendering. OIDC itself remained configured and available.

The hotfix disables App's feature query until authentication has finished successfully and setup is complete. Three real-App regression tests cover anonymous startup with OIDC, a pending-to-authenticated transition with feature toggles applied, and initial setup. The full frontend suite passes: **79 files / 1,484 tests**; the production TypeScript/Vite build passes (existing bundle-size advisory only).

The production bundle was also checked in a real browser through a loopback preview proxy to Hawaii's public auth endpoints. The login and authentik button remained visible; the proxy recorded only `/api/v1/auth/check` and `/api/v1/auth/oidc`, with no protected settings request. This verifies the candidate bundle; the live hotfix requires its release/deployment.

Scope: combined P0–P4 implementation on Windows, with PostgreSQL 16 integration tests and Linux agent/collector/redaction tests in a disposable container. Review uses the existing FRD, ADD and security review; it does not replace their production deployment gates.

## Results

| Check | Result |
|---|---|
| Baseline Go / frontend | PASS; frontend 1,410 tests |
| Final frontend | PASS; 78 files, 1,481 tests |
| Production frontend build | PASS |
| Static-demo frontend build | PASS |
| Agent, collectors and redaction on Windows/Linux | PASS |
| Server focused Docker/security/storage tests | PASS |
| PostgreSQL feature/storage and lifecycle downgrade tests | PASS |
| Real live WebSocket expiry | PASS |
| Static analysis | PASS (`go vet ./...`) |

Final combined Go/PostgreSQL run: PASS. Output is appended below. Focused results are not represented as exhaustive certification of every FRD acceptance criterion.

## Defects found and corrected

- Migration numbering collision with lifecycle 23; schema introspection crossing test schemas; existing partition autovacuum ALTERs; storage-usage device key type; lifecycle downgrade target assumptions.
- Quota flush losing dirty records after a persistence failure; missing command-result budget; disk-pressure eviction below ordinary byte budget; omitted telemetry sections losing known state; downsampling permitting older live state.
- NUL-bearing log batches and masking expansion escaping byte accounting; pre-mask priority loss; sub-microsecond collection cursor loss; credential leakage through otherwise safe environment values and structural label values.
- Compose source reserialization and incomplete aliases/dotenv/argv handling; scoped checks previously invalidating the entire host cache.
- History/live container identity mismatch, ignored backfill markers, self-stack confirmation mismatch, canonical Activity filters, incomplete stack services and Down-stack membership.
- Fleet projections transferring arbitrary labels; importer integration assertions looking at another schema's relation IDs during overlapping PostgreSQL suites.

## Browser review

The static demo visibly renders the Docker sub-navigation, sortable fleet table, human status/update badges and older-agent state. Canonical detail URLs retain tab state. Live starts off. Retained Down stacks offer Up; masked Compose reads reveal a redacted token while preserving source lines. Inventory requires one host before cleanup preview and lists exact candidates before removal; Cancel returns to the inventory. No real resource removal was performed during browser review.

## Limits and release notes

Verdict: PASS WITH NOTES for local implementation validation. Local automated checks and reviewed flows pass. No production backup, schema reconciliation, deployment or force operation was performed. Complete the per-database upgrade runbook before deployment. Destructive Docker calls were verified through fakes, not against a managed production daemon. Production-scale sustained throughput/disk-pressure testing and a full screen-reader/mobile-browser audit remain outside the evidence collected here. The existing frontend bundle-size advisory remains.

## Final combined output

Command: `go test -p 1 -tags integration ./... -count=1` with both test database variables set to the disposable PostgreSQL instance.

```text
?   	github.com/DesyncTheThird/rIOt/cmd/riot-agent	[no test files]
?   	github.com/DesyncTheThird/rIOt/cmd/riot-agent-apply	[no test files]
?   	github.com/DesyncTheThird/rIOt/cmd/riot-server	[no test files]
ok  	github.com/DesyncTheThird/rIOt/cmd/riot-signer	0.580s
ok  	github.com/DesyncTheThird/rIOt/internal/agent	17.471s
ok  	github.com/DesyncTheThird/rIOt/internal/agent/collectors	4.433s
ok  	github.com/DesyncTheThird/rIOt/internal/logbudget	0.657s
ok  	github.com/DesyncTheThird/rIOt/internal/models	0.610s
ok  	github.com/DesyncTheThird/rIOt/internal/redact	0.562s
ok  	github.com/DesyncTheThird/rIOt/internal/releasekey	0.480s
ok  	github.com/DesyncTheThird/rIOt/internal/resilient	1.026s
ok  	github.com/DesyncTheThird/rIOt/internal/server	2.843s
ok  	github.com/DesyncTheThird/rIOt/internal/server/auth	0.491s
ok  	github.com/DesyncTheThird/rIOt/internal/server/ca	0.890s
ok  	github.com/DesyncTheThird/rIOt/internal/server/commands	1.054s
ok  	github.com/DesyncTheThird/rIOt/internal/server/db	204.461s
ok  	github.com/DesyncTheThird/rIOt/internal/server/events	0.532s
ok  	github.com/DesyncTheThird/rIOt/internal/server/handlers	9.848s
ok  	github.com/DesyncTheThird/rIOt/internal/server/ingest	0.514s
?   	github.com/DesyncTheThird/rIOt/internal/server/logstore	[no test files]
ok  	github.com/DesyncTheThird/rIOt/internal/server/middleware	0.820s
ok  	github.com/DesyncTheThird/rIOt/internal/server/notify	0.782s
ok  	github.com/DesyncTheThird/rIOt/internal/server/oidc	1.550s
ok  	github.com/DesyncTheThird/rIOt/internal/server/partition	57.511s
ok  	github.com/DesyncTheThird/rIOt/internal/server/probes	0.854s
ok  	github.com/DesyncTheThird/rIOt/internal/server/quota	0.325s
ok  	github.com/DesyncTheThird/rIOt/internal/server/scoring	0.441s
ok  	github.com/DesyncTheThird/rIOt/internal/server/storage	0.900s
ok  	github.com/DesyncTheThird/rIOt/internal/server/summary	0.545s
ok  	github.com/DesyncTheThird/rIOt/internal/server/updates	0.494s
ok  	github.com/DesyncTheThird/rIOt/internal/server/websocket	0.637s
?   	github.com/DesyncTheThird/rIOt/internal/testutil	[no test files]
```

Static checks: `go vet ./...` exited 0 with no output. `go build ./cmd/riot-server ./cmd/riot-agent` exited 0 with no output. Whitespace checks passed.

## Earlier focused output

```text
ok  	github.com/DesyncTheThird/rIOt/internal/redact	0.293s
ok  	github.com/DesyncTheThird/rIOt/internal/models	0.310s
ok  	github.com/DesyncTheThird/rIOt/internal/server/quota	0.329s
ok  	github.com/DesyncTheThird/rIOt/internal/server/storage	0.294s
ok  	github.com/DesyncTheThird/rIOt/internal/server/ingest	0.479s
ok  	github.com/DesyncTheThird/rIOt/internal/server/handlers	7.346s
ok  	github.com/DesyncTheThird/rIOt/internal/server	2.214s
```

## Baseline Go output

```text
?   	github.com/DesyncTheThird/rIOt/cmd/riot-agent	[no test files]
?   	github.com/DesyncTheThird/rIOt/cmd/riot-agent-apply	[no test files]
?   	github.com/DesyncTheThird/rIOt/cmd/riot-server	[no test files]
ok  	github.com/DesyncTheThird/rIOt/cmd/riot-signer	1.379s
ok  	github.com/DesyncTheThird/rIOt/internal/agent	8.581s
ok  	github.com/DesyncTheThird/rIOt/internal/agent/collectors	5.049s
ok  	github.com/DesyncTheThird/rIOt/internal/models	0.651s
ok  	github.com/DesyncTheThird/rIOt/internal/releasekey	0.630s
ok  	github.com/DesyncTheThird/rIOt/internal/resilient	1.138s
ok  	github.com/DesyncTheThird/rIOt/internal/server	3.239s
ok  	github.com/DesyncTheThird/rIOt/internal/server/auth	0.638s
ok  	github.com/DesyncTheThird/rIOt/internal/server/ca	0.690s
ok  	github.com/DesyncTheThird/rIOt/internal/server/commands	0.826s
ok  	github.com/DesyncTheThird/rIOt/internal/server/db	1.199s
ok  	github.com/DesyncTheThird/rIOt/internal/server/events	0.795s
ok  	github.com/DesyncTheThird/rIOt/internal/server/handlers	7.685s
?   	github.com/DesyncTheThird/rIOt/internal/server/logstore	[no test files]
ok  	github.com/DesyncTheThird/rIOt/internal/server/middleware	1.223s
ok  	github.com/DesyncTheThird/rIOt/internal/server/notify	1.136s
ok  	github.com/DesyncTheThird/rIOt/internal/server/oidc	1.988s
ok  	github.com/DesyncTheThird/rIOt/internal/server/probes	1.118s
ok  	github.com/DesyncTheThird/rIOt/internal/server/scoring	0.583s
ok  	github.com/DesyncTheThird/rIOt/internal/server/summary	0.714s
ok  	github.com/DesyncTheThird/rIOt/internal/server/updates	0.601s
ok  	github.com/DesyncTheThird/rIOt/internal/server/websocket	0.767s
?   	github.com/DesyncTheThird/rIOt/internal/testutil	[no test files]
```

## Latest-main integration verification

Integrated main at `2addf5c` (GROWTH-001 agent hardening). Resolved six overlapping agent files while retaining corruption recovery/vacuum, HTTP backoff/permanent rejection, journal bounds, payload guards and the Docker UX limits. Updated old collector fixtures for ContainerLogBatch and explicit configurable maxima; added a 4 KiB multibyte/secret regression proving identical stored/live text. Effective defaults and buffer ceiling are documented in README and CHANGELOG.

Final Windows command: `go test ./... -count=1 -timeout=120s`. PASS. This run uses no live PostgreSQL; the earlier full PostgreSQL evidence above remains applicable to the unchanged server schema. `go vet ./...` also passed without output. GitHub CI validates the final merged revision on Linux.

```text
?   	github.com/DesyncTheThird/rIOt/cmd/riot-agent	[no test files]
?   	github.com/DesyncTheThird/rIOt/cmd/riot-agent-apply	[no test files]
?   	github.com/DesyncTheThird/rIOt/cmd/riot-server	[no test files]
ok  	github.com/DesyncTheThird/rIOt/cmd/riot-signer	0.914s
ok  	github.com/DesyncTheThird/rIOt/internal/agent	15.980s
ok  	github.com/DesyncTheThird/rIOt/internal/agent/collectors	13.069s
ok  	github.com/DesyncTheThird/rIOt/internal/logbudget	0.418s
ok  	github.com/DesyncTheThird/rIOt/internal/models	0.474s
ok  	github.com/DesyncTheThird/rIOt/internal/redact	0.455s
ok  	github.com/DesyncTheThird/rIOt/internal/releasekey	0.551s
ok  	github.com/DesyncTheThird/rIOt/internal/resilient	0.962s
ok  	github.com/DesyncTheThird/rIOt/internal/server	2.691s
ok  	github.com/DesyncTheThird/rIOt/internal/server/auth	0.551s
ok  	github.com/DesyncTheThird/rIOt/internal/server/ca	0.562s
ok  	github.com/DesyncTheThird/rIOt/internal/server/commands	1.068s
ok  	github.com/DesyncTheThird/rIOt/internal/server/db	0.908s
ok  	github.com/DesyncTheThird/rIOt/internal/server/events	0.617s
ok  	github.com/DesyncTheThird/rIOt/internal/server/handlers	10.046s
ok  	github.com/DesyncTheThird/rIOt/internal/server/ingest	0.634s
?   	github.com/DesyncTheThird/rIOt/internal/server/logstore	[no test files]
ok  	github.com/DesyncTheThird/rIOt/internal/server/middleware	1.044s
ok  	github.com/DesyncTheThird/rIOt/internal/server/notify	0.984s
ok  	github.com/DesyncTheThird/rIOt/internal/server/oidc	1.971s
ok  	github.com/DesyncTheThird/rIOt/internal/server/partition	0.550s
ok  	github.com/DesyncTheThird/rIOt/internal/server/probes	0.996s
ok  	github.com/DesyncTheThird/rIOt/internal/server/quota	0.475s
ok  	github.com/DesyncTheThird/rIOt/internal/server/scoring	0.590s
ok  	github.com/DesyncTheThird/rIOt/internal/server/storage	0.870s
ok  	github.com/DesyncTheThird/rIOt/internal/server/summary	0.700s
ok  	github.com/DesyncTheThird/rIOt/internal/server/updates	0.607s
ok  	github.com/DesyncTheThird/rIOt/internal/server/websocket	0.817s
?   	github.com/DesyncTheThird/rIOt/internal/testutil	[no test files]
```
