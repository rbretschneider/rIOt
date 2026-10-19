# DOCKER-UX-001 QA report

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
