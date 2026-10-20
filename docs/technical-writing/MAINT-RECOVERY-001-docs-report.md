# Documentation report — MAINT-RECOVERY-001

Date: 2026-10-20. Technical-writing handoff follows independent QA **PASS WITH NOTES** against corrective revision `e12436a`.

## Inputs and scope

Reviewed the paired FRD/ADD, security review and apt-mark correction, implementation report, packaging evidence and corrected QA report. Cross-checked the implemented protocol/limits, fixed process plan/environment, recovery entrypoint, doctor readiness/effective policy, signed installer, service units, release assets and CLI parser. Documentation only; no implementation, tests, upstream contracts, production state or Hawaii repository was changed by this stage.

## Artifacts

| File | Change |
|---|---|
| `README.md` | Added recovery entry and operator-guide links; replaced direct apt/wildcard hold advice, optional missing-helper behavior and stale inline-apt limitation. Documented prerequisites, explicit released root migration, truthful updater handoff, signed assets and separately installed SMART tooling. |
| `CHANGELOG.md` | Added unreleased MAINT-RECOVERY-001 behavior, migration, security policy, limits and rollout gate. No release version invented. |
| `docs/ops/os-maintenance.md` | Complete operator guide: modes and existing repair API; finite no-removal transaction; fixed root boundary; holds/reboot/outbox; apply exclusion and digest/time acknowledgement; migration matrix; read-only diagnostics/results/history; real-systemd gate and residual package-script risks. |
| This report | Pipeline completion evidence and stale-content log. |

The guide says v2.72.0 lacks the new assets, requires matching reviewed released installer/manifest and retains the real-systemd PID 1 smoke gate. Agent-only updates cannot update privileged assets. It distinguishes already verified Vermont legacy hardening from the unperformed worker deployment and pending OpenMediaVault writecache repair. No new UI button, endpoint, schema or runtime dependency is claimed. Apt security mode is explicitly conservative upgrade behavior, not origin filtering; DNF remains inline.

## Checks

All documentation checks passed:

```text
Maintenance documentation local-link checks passed: 10
Documented hold/unhold sudo expressions exactly match installer output: 2
Doctor explicit-config argument order verified against CLI parser
git diff --check -- README.md CHANGELOG.md docs/ops/os-maintenance.md
No diagnostics.
```

The two published anchored sudo expressions were compared literally with installer output, including package-name grammar and anchors. Doctor examples put `-config` before `doctor`, matching Go flag parsing. Reviewed documented constants against `internal/maintenance/protocol.go`, including 4096-byte requests, 256 KiB captured output, 32-record/16 MiB capacity with next-record reservation, 30-minute worker, two-minute cleanup and 90-second termination grace. Unit times/ownership/path statements were checked against templates and safe-IO implementation. The existing authenticated API repair JSON was checked against the command handler; no credential or endpoint was invented. No code tests were rerun for this documentation-only stage; implementation and independent QA evidence remain in their reports.

## Stale content found

- Installer re-enrollment is preexisting separate behavior: `--bootstrap-key` on an existing config deletes enrollment/config state. The installer accepts an omitted server URL for reinstall, but combining an empty URL with explicit bootstrap re-enrollment can recreate an unusable empty server URL. This was not changed in code. The maintenance guide requires the explicit existing URL and excludes bootstrap reset from ordinary migration. Track the installer edge case separately.
- README's global “last updated for v2.38.0” stamp and older version guidance are stale and outside this feature's release bookkeeping. They were not changed to an invented release.
- README's old signing setup text still says the public key is a placeholder despite the configured release trust key and prior signed releases. That broader signing/bootstrap documentation needs a separate accuracy pass; this story uses the established pinned-key trust process and does not change keys.
- Historical changelog entries describe the former direct-dpkg self-heal behavior. They remain historical records; the new unreleased entry documents the replacement.

## Completion

Required technical-writing artifacts are present. PR review may proceed with QA's explicit production lifecycle gate intact. No release, production package transaction, repair, reboot or deployment is implied by this report.
