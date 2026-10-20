# QA report — MAINT-RECOVERY-001

Date: 2026-10-20. Reviewed development commits: `75ed334` and `e6b7aae`, against base `ba02585` on `codex/maintenance-recovery`.

**Current verdict: PROVISIONAL — earlier PASS WITH NOTES superseded.** After the review below, the root reviewer demonstrated a privilege escalation in the retained legacy `/usr/bin/apt-mark hold *` sudo rule: caller-supplied `-o Dir::Bin::dpkg=...` options can select an arbitrary root executable. The demonstration used a harmless marker inside an isolated, network-disabled Debian 12 container. No production probe was performed. The earlier conclusion that no blocking implementation defect remained is withdrawn. Technical writing and final QA approval must wait for the committed restriction and independent regression review.

The proposed correction restricts hold/unhold arguments using anchored sudo argument expressions, requires a sudo version that supports that matching, and adds read-only doctor probes plus actual sudo matching fixtures. This report does not yet approve that correction. The evidence below describes the original reviewed commits and remains useful only within that scope. The real-systemd smoke test also remains a production rollout gate.

## Independence and scope

QA began after development was committed and the checkout was clean. Read the root engineering pipeline, complete FRD/ADD and security review, implementation report, validation transcript and packaging evidence. Inspected the changed privilege boundary, fixed runner, safe file/lock implementation, worker persistence, hold adapter/barrier, agent observation/startup/outbox/reboot paths, updater acknowledgement, installer, units and release/CI packaging. No implementation files were changed by QA.

The existing implementation evidence records passing pre-change Go baseline, final Windows Go suite, full Linux race suite, Go vet, 1,541 frontend tests, TypeScript check, eighteen architecture builds and fourteen installer fixtures. QA audited this evidence and independently reran the security-sensitive Linux suites and real package fixtures rather than repeating the unchanged frontend suite. Database integration gates still belong to CI; they were not independently run here.

## Independent execution

All commands below exited zero. Source was read-only in the containers. No production SSH, package command, service restart or deployment was performed.

```text
docker exec -w /src riot-maintenance-test-20261020 go test -race -count=1 ./internal/maintenance ./cmd/riot-agent-apply ./internal/agent ./internal/agent/collectors ./scripts
ok github.com/DesyncTheThird/rIOt/internal/maintenance       4.600s
ok github.com/DesyncTheThird/rIOt/cmd/riot-agent-apply       1.374s
ok github.com/DesyncTheThird/rIOt/internal/agent             66.370s
ok github.com/DesyncTheThird/rIOt/internal/agent/collectors  81.767s
ok github.com/DesyncTheThird/rIOt/scripts                    1.019s

docker exec -w /src riot-maintenance-test-20261020 go test -race -count=1 ./cmd/riot-maintenance
ok github.com/DesyncTheThird/rIOt/cmd/riot-maintenance 1.018s

docker exec -e RIOT_INSTALLER_FIXTURE=1 riot-installer-test-20261020 python3 /src/scripts/test_install_maintenance.py
Ran 14 tests in 2.093s
OK

docker run --rm --network none --mount type=bind,src=D:\Repos\rIOt,dst=/src,readonly debian:bookworm-slim bash /src/scripts/tests/maintenance-apt-fixture.sh --disposable-container
[MAINT-RECOVERY AC-003] interrupted configuration repaired and audited
[MAINT-RECOVERY AC-003] one no-removal dependency repair installed the missing provider
E: Packages need to be removed but remove is disabled.
[MAINT-RECOVERY AC-004] apt refused a removal-required repair and retained the package

git diff --check ba02585..HEAD
No diagnostics.
```

The apt fixture intentionally produces failing maintainer-script/configuration and dependency checks before proving recovery. Its private local repository uses a fixture-only trusted source with no network; production does not receive that override. This independently establishes actual apt no-removal behavior, not production repository authentication or the full root-service lifecycle. Installer fixtures use real OpenSSL, visudo and systemd-analyze validators, with recording lifecycle commands and fail-on-mutation package stubs.

## Acceptance and security coverage

| Criteria | QA result and evidence |
|---|---|
| AC-001 | PASS. Explicit four-field worker protocol rejects missing, duplicate, unknown, null, malformed, oversized, trailing and injection inputs. Agent validates/defaults the closed mode before handoff. Helper rejects argv. |
| AC-002 | PASS. One exact `sudo -n /usr/bin/systemctl start --no-block riot-maintenance.service` handoff. Package commands use absolute fixed executables/argv, a replaced environment, `/` working directory and null stdin. Recording and sink-negative tests passed. |
| AC-003 | PASS. Configure precedes refresh; one guarded repair is allowed, followed by verification; repair-only stops before refresh/upgrade/hold release. Runner matrix and real Debian 12 configuration/dependency fixtures passed. |
| AC-004 | PASS. Recorded failure matrix refuses repair/refresh continuation and widening fallback. Real apt removal-required fixture aborted while preserving the unpacked package. Native lock exclusion was separately exercised by installer fixtures. Repository/authentication failures are recording-runner coverage, not live network tests. |
| AC-005 | PASS. Real kernel flock contention, independent process contender, apply/boot refusal and eventual successful apply tests passed. Installer root/native lock contention refuses before stop. Host/Docker reservation and fresh digest-bound acknowledgement regressions passed; accepted launch alone is not completion. |
| AC-006 | PASS WITH ENVIRONMENT NOTE. Independent harmless worker survives observer SIGKILL; matching UUID recovery, stale-current startup reservation and once-only outbox tests passed. Unknown/vanished worker is not replayed. Actual systemd activation/cgroup independence remains unexecuted. |
| AC-007 | PASS WITH ENVIRONMENT NOTE. Real process-group TERM/KILL, descendant lifetime and bounded output draining tests passed; observer cancellation does not signal the worker. Fixed unit runtime/stop policy validates. Real systemd timeout enforcement remains unexecuted. |
| AC-008 | PASS. Existing operator/managed hold regressions, strict detached metadata/classification, durable release intent, independent cleanup failures and the reconciliation mutex barrier passed. Agent-owned historical hold ownership remains an explicit trust limitation. |
| AC-009 | PASS. Version/result/reboot regressions passed. Repair/failed/uncertain outcomes do not request automatic reboot; recovered results suppress replay. Durable intent plus host reservation protects the reboot delay, with a fresh maintenance check before the existing reboot call. |
| AC-010 | PASS. Root-safe IO tests reject unsafe owners/modes/links/types/parents and swapped symlinks; bounded request/output/records and UUID/body matching passed. History capacity refuses work without deleting evidence. Result redaction remains in the existing completion path. |
| AC-011 | PASS. Signed asset and manifest-order tests passed. Invalid signature, changed/missing helper or invalid signed unit preserves the running install. Reported actual cross-build evidence covers five Linux architectures for all three binaries and three existing non-Linux agent targets. |
| AC-012 | PASS. Fourteen independent installer scenarios passed, including legacy fresh-inode migration preserving identity, candidate/effective policy failure restoration, unsafe paths and unresolved state. Doctor negative privilege probes list permissions only; effective unit/sudo policy tests passed. Existing DNF/portable regression evidence is green. |

SEC-1 through SEC-7 are covered by the corresponding rows. The review found no new caller-selected command, package operand, root output path, environment or service-name execution surface. Root publication remains signature/checksum-gated; the installed agent alone cannot grant the new helper/policy capabilities. Terminal history is immutable and capacity-limited rather than automatically expired.

## Required rollout checks and limitations

Before enabling this worker in production, use a disposable or explicitly approved supported host with **systemd as PID 1** to verify: the signed installer and read-only doctor; actual fixed-unit activation; agent stop/restart while harmless maintenance output continues independently; reconnect/result delivery for the same UUID; apply/boot refusal during active and deactivating cleanup; and service runtime/TERM/KILL cgroup cleanup. Record the host/tool versions and results. Existing containers use PID 1 `sleep`; process/descriptor fixtures and unit syntax validation do not establish those end-to-end service properties.

Keep the explicit root migration requirement in user documentation. An agent-only upgrade must fail closed on old/missing helpers or legacy broad policy. Package scripts can restart services, migrate applications or change mounts; no-removal is not rollback. Power loss, host OOM and privileged administrator interference remain external risks. Existing reboot permission is separate from this worker and is not a newly constrained reboot gateway. The retained apt `security` mode is conservative upgrade behavior, not security-origin filtering.

No Vermont/OpenMediaVault recovery was attempted. Its pending writecache migration requires separate impact review and operational authorization. No release or production rollout follows automatically from this provisional report.
