# CMD-LIFECYCLE-001 — Implementation Report (index)

The story was implemented as four work packages (ADD §13.3). Each has its own detailed report:

| Work package | Scope | Report |
|---|---|---|
| WP-MODELS | `internal/models` shared contract (state machine, policy tables, wire types) | commit 421ffb3 (tests in `internal/models/*_test.go`) |
| WP-GO-SERVER | migration 000023, `server/db`, `server/commands`, handlers, hub, router, CI Postgres | [CMD-LIFECYCLE-001-impl-report-server.md](CMD-LIFECYCLE-001-impl-report-server.md) |
| WP-GO-AGENT | WS writer, outbox, lifecycle, target locks, step limits, docker update rewrite, recovery, freshness | [CMD-LIFECYCLE-001-impl-report-agent.md](CMD-LIFECYCLE-001-impl-report-agent.md) |
| WP-WEB | types, API client, telemetry merge, command tracker, ContainerDetail/ComposeStackSection/ActivityLog | [CMD-LIFECYCLE-001-impl-report-web.md](CMD-LIFECYCLE-001-impl-report-web.md) |

## Detected stack
Go 1.24 (chi v5, pgx v5, golang-migrate, gorilla/websocket, Docker SDK); PostgreSQL 16; React + TypeScript + TanStack Query + Tailwind, vitest + Testing Library.

## Integrated verification (orchestrator, after all packages committed)
- `go build ./...` and `go vet ./...`: clean
- `go test -count=1 ./...`: every package `ok` (Windows host, no `-race`; the agent package was also run with `-race` on Linux by WP-GO-AGENT)
- PG-gated repository/migration tests: run by WP-GO-SERVER against a throwaway `postgres:16` container, all pass; they skip cleanly without `RIOT_TEST_PG_URL`
- `cd web && npx tsc --noEmit`: clean
- `cd web && npm run test:run`: 32 files, 590 tests passed (baseline 405)
- No new Go or npm dependencies (`go.mod`, `go.sum`, `web/package*.json` unchanged)

## Cross-package contract checks
The agent and web packages each listed what they assume about the server. The server package checked every point and added tests for them (see the "agent contract" and "web contract" sections in the server report). The only difference is that WS `command_receipt` is sent for `command_result` only, as ADD §6.4 specifies. The agent uses receipts just to clear its outbox, and the outbox only ever holds final results.
