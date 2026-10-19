# Docker area upgrade procedure

DOCKER-UX-001 adds schema migrations 24–32 after the existing command lifecycle migration 23. The six PART-001 migrations retain their relative order at 24–29. This procedure applies before deployment to each existing database. It is not authorization to run migration force against any deployed instance.

## Before upgrading

1. Stop the server. Keep it stopped until the post-upgrade checks succeed. Agents buffer a bounded amount of telemetry while it is unavailable.
2. Supply PostgreSQL credentials through a protected `PGPASSFILE` or a non-interactive process environment. Do not put passwords in command arguments or shell history.
3. Take a custom-format `pg_dump` outside the repository, with file mode 0600. A dump contains session-signing secrets, device credentials and logs: treat it as a credential, and never attach it to a chat or ticket.
4. Verify it using `pg_restore --list`. Confirm `commands`, `container_logs`, `telemetry_snapshots` and `schema_migrations` are present. Prefer a trial restore to a disposable database, with row counts compared. Record the dump's basename, size and verification result without connection details.
5. Optionally export `terminal_sessions` for an operator-controlled archive. Closed terminal audit rows are retained for 180 days after the upgrade; detailed command output is retained for 30 days and command history for 90.

## Read-only classification

Using the new binary and the configured database environment, run:

```text
riot-server db-fingerprint
```

This command runs before server construction, opens a read-only session and transaction, and never instantiates a migrator. It prints only version, dirty flag, three schema fingerprints, classification and action. Errors omit credentials and connection identifiers.

| State | Action |
|---|---|
| Clean version 22, no fingerprints | Normal upgrade |
| Version 23 with lifecycle columns and constraint | Normal upgrade; never force below 23 |
| Normal main history above 23 with lifecycle fingerprint | Normal upgrade |
| Version 23 with only the old autovacuum fingerprint | Stop for operator reconciliation |
| Branch-numbered PART-001 versions 24–28 without lifecycle fingerprint | Stop for operator reconciliation |
| Dirty, mixed or unexpected state | Stop and investigate; do not force or upgrade |

For a branch-history reconciliation, follow the full [architecture runbook](../architecture/DOCKER-UX-001-add.md#241-reconciliation-runbook-sec-022-d-43-settled). Any force requires that database operator's explicit sign-off and verified backup evidence. Never force a database whose lifecycle migration already ran below version 23. The binary never forces a version automatically. Reapplying the renumbered PART-001 files to an already converted database is covered by a seeded-row preservation test.

## Upgrade without serving

```text
riot-server migrate
riot-server db-fingerprint
```

`migrate` refuses dirty or unreconciled databases and exits without binding an HTTP port. Its diagnostics omit connection identifiers. The post-upgrade fingerprint must be clean, with lifecycle and partition fingerprints present. Record the resulting version and flags. On any unexpected result, leave the server stopped; restoring the backup also requires operator sign-off.

Start the server only after verification. Upgrade agents next. Delete the dump securely or move it to the operator's protected backup store. Do not use a partition downgrade as a production rollback: its historical down migrations do not recover post-conversion writes.

## Operational settings

- Container-log retention: `RIOT_CONTAINER_LOG_RETENTION_DAYS=7`, accepted 1–90. Lowering it deletes whole expired days; raising it restores nothing.
- Log and command-result budgets: `RIOT_CONTAINER_LOG_MAX_GB=16`, `RIOT_COMMAND_RESULTS_MAX_GB=4`.
- Headroom: use a read-only `RIOT_DB_FS_PROBE_PATH` on the database filesystem, or set `RIOT_DB_MAX_GB`. Do not mount PostgreSQL's data volume into the server.
- The probe detects impossible capacity and inconsistent growth; an unrelated filesystem must not trigger destructive eviction. Configured capacity remains available as an explicit fallback.
- Every custom Compose service should use bounded Docker logging, equivalent to `json-file` with `max-size: "10m"` and `max-file: "5"`.

No production database was changed by the implementation task. Local validation uses a disposable PostgreSQL database with synthetic data.
