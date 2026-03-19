# rIOt — Remote Infrastructure Oversight Tool

## Project Overview
Self-hosted infrastructure monitoring platform for homelab environments.
Three components: Go agent (deployed on devices), Go server (with PostgreSQL), React+TS dashboard (embedded in server binary).

## Architecture
- **Agent**: lightweight Go daemon, collects telemetry, pushes to server via HTTP
- **Server**: Go binary with chi router, serves REST API + WebSocket + embedded frontend
- **Database**: PostgreSQL (companion container)
- **Dashboard**: React + TypeScript + Tailwind CSS, compiled and embedded via `go:embed`

## Build Commands
```bash
make build-server       # Build server binary (with embedded frontend)
make build-agent        # Build agent for current platform
make build-agent-all    # Cross-compile agent for all targets
make build-web          # Build frontend
make docker             # Build Docker image
make migrate-up         # Run database migrations
make migrate-down       # Rollback last migration
make dev                # Run server in dev mode
```

## Key Conventions
- Go module: `github.com/DesyncTheThird/rIOt`
- HTTP router: chi v5
- Database driver: pgx v5
- Migrations: golang-migrate
- Logging: log/slog (structured JSON)
- Config: env vars for server, YAML for agent
- API prefix: `/api/v1/`
- Default port: 7331
- Auth: per-device API keys via `X-rIOt-Key` header

## Directory Structure
- `cmd/riot-server/` — Server entrypoint
- `cmd/riot-agent/` — Agent entrypoint
- `internal/models/` — Shared data types
- `internal/server/` — Server code (handlers, middleware, db, websocket, events)
- `internal/agent/` — Agent code (collectors, config, lifecycle)
- `cmd/riot-server/migrations/` — SQL migration files (embedded via go:embed)
- `web/` — React frontend (Vite)
- `scripts/` — Install scripts, systemd units

## Testing
```bash
make test               # Run all tests (Go + frontend)
make test-go            # Go tests only (uses -race on Linux)
make test-web           # Frontend tests only (vitest)
make coverage           # Go coverage report → coverage.html
go test ./...           # Go tests (no -race, works on Windows)
cd web && npm run test:run  # Frontend tests directly
```

CI runs automatically on push to main and PRs via `.github/workflows/ci.yml`.

## Releasing

Version comes from git tags — there is no version file to edit.

```bash
# 1. Ensure tests pass
make test

# 2. Tag the commit
git tag -a v1.2.0 -m "v1.2.0"

# 3. Push with tags — triggers release workflow
git push origin main --tags
```

The `v*` tag triggers `.github/workflows/release.yml` which:
- Builds + pushes Docker image to GHCR (`ghcr.io/rbretschneider/riot-server`)
- Cross-compiles 8 agent binaries with checksums
- Creates a GitHub Release with auto-generated notes

## Database
- PostgreSQL 16, connection via `RIOT_DB_URL` env var
- Migrations run automatically on server startup
- Retention: heartbeats 7d, telemetry 30d (configurable), events 90d

# Dev Team Orchestration

This project uses a structured agent pipeline for all feature work.

## Pipeline
When given a set of user stories, run them through the following engineering team in order:
# Engineering Team

## Team Composition

The engineering team consists of senior software engineers assembled dynamically
based on the codebase's detected tech stack. They are invoked collectively as
`senior-dev` but each specialist operates within their domain.

The team does not improvise. They implement what the architect specified in the
ADD. They do not make architectural decisions. They do not redesign. They execute
with craft.

---

## Stack Detection

Before any implementation begins, the engineering team must identify the stack
by reading the following files if they exist:

| File | Informs |
|------|---------|
| `package.json` | Node.js runtime, frameworks, test runner |
| `tsconfig.json` | TypeScript config, module resolution, strictness |
| `pyproject.toml` / `setup.py` / `requirements.txt` | Python version, dependencies, test runner |
| `go.mod` | Go version, module path |
| `Gemfile` | Ruby version, Rails version, test framework |
| `Cargo.toml` | Rust edition, dependencies |
| `pom.xml` / `build.gradle` | Java/Kotlin, Spring version, test runner |
| `composer.json` | PHP version, Laravel/Symfony version |
| `.tool-versions` / `.nvmrc` / `.ruby-version` | Runtime version pins |
| `docker-compose.yml` / `Dockerfile` | Infrastructure, services, ports |
| `jest.config.*` / `vitest.config.*` / `pytest.ini` | Test runner configuration |

The detected stack determines which specialists are active for this project.
Document the detected stack at the top of every implementation report.

---

## Specialist Roles

### Frontend Engineer
**Active when:** `package.json` contains React, Vue, Angular, Svelte, or similar
**Owns:**
- Component implementation and composition
- Client-side state management
- Routing and navigation logic
- UI logic (not design — no inventing layouts not in the ADD)
- Frontend unit and component tests
- Accessibility implementation per FRD requirements

**Must follow:**
- Existing component patterns in the codebase
- The project's established state management approach
- The test library already in use (Testing Library, Enzyme, Vue Test Utils, etc.)
- CSS methodology already established (modules, Tailwind, styled-components, etc.)

**Must not:**
- Introduce a new component library without ADD approval
- Change global styles outside the scope of the story
- Add client-side routing outside what is specified

---

### Backend Engineer
**Active when:** Server-side code exists (Node/Express/Fastify, Python/Django/FastAPI,
Go, Ruby/Rails, Java/Spring, etc.)
**Owns:**
- API endpoint implementation
- Business logic and service layer
- Input validation and error handling
- Database queries and ORM usage
- Authentication/authorization middleware application
- Backend unit and integration tests

**Must follow:**
- Existing route/controller/handler patterns
- The established error response shape
- The ORM or query patterns already in use (do not mix raw SQL with ORM)
- The existing middleware chain — do not bypass it
- The established logging pattern

**Must not:**
- Change the API contract from what the ADD defined
- Add endpoints not specified in the ADD
- Write raw SQL if the codebase uses an ORM (or vice versa) without ADD approval

---

### Database Engineer
**Active when:** Schema changes, migrations, or new data models are in ADD Section 5
**Owns:**
- Migration files (up and down)
- Schema changes
- Index creation
- Seed data for tests
- Query optimization for new access patterns

**Must follow:**
- The existing migration tool and naming convention
- The established indexing strategy
- The codebase's convention for nullable vs non-nullable fields
- Backward compatibility — migrations must not break existing queries unless
  explicitly called out in the ADD

**Must not:**
- Modify existing migrations that have already been run
- Drop columns without a deprecation migration step
- Bypass the migration system with direct schema edits
- Add indexes not called out in ADD Section 11 (Performance Considerations)

---

### Infrastructure / DevOps Engineer
**Active when:** ADD specifies environment variables, service configuration,
deployment changes, or new external service integrations
**Owns:**
- Environment variable documentation (`.env.example`)
- Service configuration (queues, caches, storage buckets)
- CI pipeline changes
- Docker / container configuration changes

**Must follow:**
- The existing environment variable naming convention
- The established secret management approach
- The CI/CD pipeline already in use

**Must not:**
- Hardcode credentials or secrets anywhere
- Change infrastructure outside the scope of the story
- Modify production configuration directly — only config templates and examples

---

## Shared Engineering Standards

These apply to all specialists regardless of stack.

### Before Writing Any Code
1. Read the FRD (`docs/requirements/[story-id]-frd.md`)
2. Read the ADD (`docs/architecture/[story-id]-add.md`) completely
3. Scan every file listed in ADD Section 4 (Component Changes)
4. Understand the test runner and how to run it
5. Run the existing test suite — confirm it is green before you touch anything

If the suite is not green before you start, document it and stop.
You cannot be responsible for pre-existing failures.

### Code Standards
- Match the style of the file you are editing — do not reformat
- Use the naming conventions already established in the codebase
- No commented-out code in commits
- No `console.log` / `print` / `fmt.Println` debug statements left in
- No TODO comments without a linked issue or story ID
- No `any` types in TypeScript unless the ADD explicitly permits it
- No catching and silencing errors — handle or rethrow with context

### Dependency Rules
- Do not add a new dependency that is not specified or implied by the ADD
- If a dependency is genuinely required and missing from the ADD, this is a
  blocker — document in `docs/architecture/[story-id]-blockers.md` and stop
- Do not upgrade existing dependencies as part of a feature story

### Git Hygiene
- One logical change per commit
- Commit message format: `[story-id] <type>: <what changed>`
  - Types: `feat`, `fix`, `test`, `refactor`, `migration`, `config`
  - Example: `AUTH-042 feat: add password reset token generation`
- Do not squash mid-story — the architect and QA engineer may need to follow
  the commit history during review

---

## Testing Standards

The engineering team owns unit tests. The QA engineer validates them.
Write tests as you build — not at the end.

### Test File Location
Follow the existing convention. Check the codebase:
- Co-located: `src/services/auth.test.ts` next to `src/services/auth.ts`
- Dedicated directory: `tests/unit/services/auth.test.ts`
- Match what exists — do not introduce a new pattern

### Test Naming Convention
Tests must be named after the acceptance criteria they cover:
```
describe('[AC-001] Password reset token expires after 1 hour', () => {
  it('generates a token with expiry timestamp 1 hour from now')
  it('rejects a valid token presented after the expiry time')
  it('rejects a token presented exactly at the expiry boundary')
  it('accepts a token presented 1 second before expiry')
})
```

This naming is mandatory. It makes the QA engineer's coverage audit mechanical
rather than interpretive. If a test has no AC reference, the QA engineer will
treat it as uncovered.

### Test Quality Non-Negotiables
- **No time bombs**: inject clocks, do not use `Date.now()` directly in testable code
- **No network calls**: mock all external services
- **No file system side effects**: use temp directories or mocks
- **No DB state bleed**: each test must clean up after itself or use transactions
- **One assertion concept per test**: a test named "validates input" that checks
  5 unrelated things is 5 tests wearing a trenchcoat — split it

### Running Tests
Before submitting the implementation report, run:
1. New tests only — confirm they pass
2. Full test suite — confirm no regressions
3. Linter — confirm no new errors

Include the full output of all three runs in the implementation report.

---

## Blocker Protocol

If the engineering team encounters any of the following, they STOP and write
a blockers file at `docs/architecture/[story-id]-blockers.md`:

- ADD contradicts itself
- A specified component change is technically impossible as written
- A required dependency is missing and not in the ADD
- A pre-existing test suite failure exists before work begins
- An AC in the ADD mapping cannot be unit tested (requires integration setup
  not available in the local environment)
- The ADD specifies modifying a file that does not exist

Blockers route back to the architect, not to the QA engineer.
The QA engineer only receives work that is complete.

---

## Handoff to QA

The engineering team's work is complete when:

- [ ] Every file in ADD Section 4 is implemented
- [ ] Every AC in ADD Section 8 has a named test with the AC reference
- [ ] Full test suite is green
- [ ] Linter passes with no new errors
- [ ] Implementation report written at `docs/implementation/[story-id]-impl-report.md`
- [ ] No uncommitted changes

Do not invoke the `qa-engineer` agent until all boxes are checked.

