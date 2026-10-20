# MAINT-RECOVERY-001 — Packaging and installer evidence

**Date:** 2026-10-20
**Detected stack:** Go 1.24, Linux systemd/Bash installer, GitHub Actions release matrix, Ed25519-signed release manifest; Python3 standard-library installer fixtures. No dependency added to the application. This supplements the root implementation report, not the post-QA user guide.

## Implemented scope

- `scripts/riot-maintenance.service`: one fixed root service, independent of agent lifecycle/cgroup limits, null stdin/journal output, no restart or boot enablement, 35-minute backstop and process-tree termination policy.
- `scripts/riot-agent.service`: signed boot verifier hook and accurate cgroup comments.
- `Makefile`: local Linux apply/maintenance helper builds, all five Linux helper architectures, helpers included in `build-agent-all`.
- Release workflow: all Linux maintenance assets and fixed service asset built/published/signed; explicit real-agent-first checksum ordering preserves legacy platform lookup.
- Installer: fixed-key signature verification of manifest and exact asset checksums before installed-file mutation; preinstalled OpenSSL3 or Python cryptography; no apt/pip dependency bootstrap or automatic SMART package installation. Apt migration additionally requires preinstalled Python3 for secure native POSIX lock holding and sudo>=1.9.10 for anchored package-hold operand regexes.
- Installer exclusion: root flock plus active-unit/strict unfinished-record checks, all four native apt/dpkg POSIX locks held through stop/replacement, trusted parent/file ownership and single-link checks. Native lock holder exits with its parent and has a 15-minute budget that terminates the installer before releasing exclusion.
- Policy publication: validate candidates and existing policy, preserve root-only backups, publish and validate effective policy before stopping/replacing agent/unit; restore original managed policy on effective-policy failure. Fresh signed root-owned binary inodes replace legacy riot-writable files. Reviewed boot precommands are cleared by a preserved/validated drop-in; config/device identity remain intact.

## Environments and limits

`riot-installer-test-20261020` is a disposable Debian bookworm container. Only the repository is bind-mounted, read-only at `/src`; system path mutations belong exclusively to that container. Fixture release keys are generated inside it. Tests replace the public key only in a copied installer; production has no key override.

Real tools:

```text
OpenSSL 3.0.22 25 Aug 2026 (Library: OpenSSL 3.0.22 25 Aug 2026)
visudo.fixture-real version 1.9.13p3
visudo.fixture-real grammar version 50
systemd 252 (252.39-1~deb12u2)
GNU bash, version 5.2.15(1)-release (x86_64-pc-linux-gnu)
Python 3.11.2
PID1: sleep
```

The fixture uses real OpenSSL verification, real visudo parsing and real `systemd-analyze verify`. `systemctl` is a recording stub; PID1 is **not systemd**. These tests do not prove actual service startup/stop, process-tree timeout behavior, journal delivery or independence from a real agent service. Those remain Linux lifecycle QA obligations. A signed-but-invalid service is rejected by the real unit validator before agent stop. Package executables are not used for repair/update; the apt stub accepts only its version query and fails any attempted package operation.

## Initial installer fixtures — complete output

Command:

```sh
docker exec -e RIOT_INSTALLER_FIXTURE=1 riot-installer-test-20261020 \
  python3 /src/scripts/test_install_maintenance.py
```

```text
test_AC011_changed_signed_asset_preserves_running_install (__main__.InstallerFixtures.test_AC011_changed_signed_asset_preserves_running_install) ... ok
test_AC011_invalid_signature_preserves_running_install (__main__.InstallerFixtures.test_AC011_invalid_signature_preserves_running_install) ... ok
test_AC011_missing_helper_preserves_running_install (__main__.InstallerFixtures.test_AC011_missing_helper_preserves_running_install) ... ok
test_AC012_active_worker_refuses_before_stop (__main__.InstallerFixtures.test_AC012_active_worker_refuses_before_stop) ... ok
test_AC012_candidate_policy_failure_preserves_old_policy (__main__.InstallerFixtures.test_AC012_candidate_policy_failure_preserves_old_policy) ... ok
test_AC012_duplicate_status_keys_refuse_before_stop (__main__.InstallerFixtures.test_AC012_duplicate_status_keys_refuse_before_stop) ... ok
test_AC012_effective_policy_failure_restores_old_policy_before_stop (__main__.InstallerFixtures.test_AC012_effective_policy_failure_restores_old_policy_before_stop) ... ok
test_AC012_invalid_signed_unit_preserves_binary_unit_policy (__main__.InstallerFixtures.test_AC012_invalid_signed_unit_preserves_binary_unit_policy) ... ok
test_AC012_legacy_migration_uses_fresh_signed_inode_and_preserves_identity (__main__.InstallerFixtures.test_AC012_legacy_migration_uses_fresh_signed_inode_and_preserves_identity) ... ok
test_AC012_multilink_root_lock_refuses_before_stop (__main__.InstallerFixtures.test_AC012_multilink_root_lock_refuses_before_stop) ... ok
test_AC012_native_dpkg_lock_refuses_before_stop (__main__.InstallerFixtures.test_AC012_native_dpkg_lock_refuses_before_stop) ... ok
test_AC012_root_lock_contention_preserves_running_install (__main__.InstallerFixtures.test_AC012_root_lock_contention_preserves_running_install) ... ok
test_AC012_symlink_helper_directory_refuses_before_stop (__main__.InstallerFixtures.test_AC012_symlink_helper_directory_refuses_before_stop) ... ok
test_AC012_unfinished_root_record_refuses_before_stop (__main__.InstallerFixtures.test_AC012_unfinished_root_record_refuses_before_stop) ... ok

----------------------------------------------------------------------
Ran 14 tests in 2.193s

OK
```

Failure cases compare original binary, unit and policy bytes and assert no service stop/package operation. The successful legacy migration additionally proves a new root-owned signed inode, preserved config/device ID, fixed sudo grants and reset boot hook. Its recorded stop attempts a real POSIX lock acquisition in another process, proving exclusion is held at stop; a post-install acquisition proves release on exit. No host package operation, service restart or production SSH mutation occurs in this suite.

## Packaging regressions

Source: `scripts/maintenance_packaging_test.go`. One test verifies the unit's fixed command/lifecycle boundary; the second executes the **actual extracted release checksum shell body** against distinct fixture bytes for every agent and both Linux helpers, verifies all digests and checks legacy first-platform-match behavior.

Windows host command (Git Bash executes the checksum script):

```sh
go test ./scripts -count=1 -v
```

Complete output:

```text
=== RUN   TestMaintenanceServiceIsolation
=== RUN   TestMaintenanceServiceIsolation/[MAINT-RECOVERY_AC-002]_fixed_service_owns_transaction_independently_of_agent
--- PASS: TestMaintenanceServiceIsolation (0.00s)
    --- PASS: TestMaintenanceServiceIsolation/[MAINT-RECOVERY_AC-002]_fixed_service_owns_transaction_independently_of_agent (0.00s)
=== RUN   TestMaintenanceReleaseChecksums
=== RUN   TestMaintenanceReleaseChecksums/[MAINT-RECOVERY_AC-011]_legacy_platform_lookup_selects_signed_agent_before_both_helpers
--- PASS: TestMaintenanceReleaseChecksums (0.07s)
    --- PASS: TestMaintenanceReleaseChecksums/[MAINT-RECOVERY_AC-011]_legacy_platform_lookup_selects_signed_agent_before_both_helpers (0.07s)
PASS
ok  github.com/DesyncTheThird/rIOt/scripts  0.430s
```

`bash -n /src/scripts/install.sh` passed in the Debian validator container. `git diff --check` passed for the owned packaging/installer files. Go test source was gofmt-formatted. No standalone ShellCheck linter is configured/run; real Bash syntax and candidate validators supply the stated checks.

## Offline release architecture builds

Builds run in isolated `riot-maintenance-test-20261020` (Go 1.24), `/src` read-only, `GOPROXY=off GOSUMDB=off`, outputs only under `/tmp/riot_packaging_builds.DFwHq9cj`. Each Linux target builds agent, signed apply helper and maintenance worker. Apply links the exact `main.targetAssetName=riot-agent-linux-<suffix>`. Non-Linux builds include only the existing agent targets. No artifacts are written to the repository.

Command body, executed with `docker exec -w /src riot-maintenance-test-20261020 bash -c`:

```sh
set -eu
export GOPROXY=off GOSUMDB=off
out=$(mktemp -d /tmp/riot_packaging_builds.XXXXXXXX)
printf 'output: %s\n' "$out"
for target in amd64 386 arm64 armv7 armv6; do
  arch=$target; arm=
  case "$target" in armv7) arch=arm; arm=7;; armv6) arch=arm; arm=6;; esac
  for component in riot-agent riot-agent-apply riot-maintenance; do
    flags='-s -w'
    if [ "$component" = riot-agent-apply ]; then
      flags="$flags -X main.targetAssetName=riot-agent-linux-$target"
    fi
    env GOOS=linux GOARCH="$arch" ${arm:+GOARM="$arm"} \
      go build -ldflags "$flags" -o "$out/$component-linux-$target" "./cmd/$component"
    printf 'PASS linux/%s %s\n' "$target" "$component"
  done
done
for target in darwin-amd64 darwin-arm64 windows-amd64; do
  os=${target%-*}; arch=${target#*-}
  env GOOS="$os" GOARCH="$arch" go build -o "$out/riot-agent-$target" ./cmd/riot-agent
  printf 'PASS %s riot-agent\n' "$target"
done
go test ./scripts -count=1 -v
```

Complete final output (exit 0):

```text
output: /tmp/riot_packaging_builds.DFwHq9cj
PASS linux/amd64 riot-agent
PASS linux/amd64 riot-agent-apply
PASS linux/amd64 riot-maintenance
PASS linux/386 riot-agent
PASS linux/386 riot-agent-apply
PASS linux/386 riot-maintenance
PASS linux/arm64 riot-agent
PASS linux/arm64 riot-agent-apply
PASS linux/arm64 riot-maintenance
PASS linux/armv7 riot-agent
PASS linux/armv7 riot-agent-apply
PASS linux/armv7 riot-maintenance
PASS linux/armv6 riot-agent
PASS linux/armv6 riot-agent-apply
PASS linux/armv6 riot-maintenance
PASS darwin-amd64 riot-agent
PASS darwin-arm64 riot-agent
PASS windows-amd64 riot-agent
=== RUN   TestMaintenanceServiceIsolation
=== RUN   TestMaintenanceServiceIsolation/[MAINT-RECOVERY_AC-002]_fixed_service_owns_transaction_independently_of_agent
--- PASS: TestMaintenanceServiceIsolation (0.00s)
    --- PASS: TestMaintenanceServiceIsolation/[MAINT-RECOVERY_AC-002]_fixed_service_owns_transaction_independently_of_agent (0.00s)
=== RUN   TestMaintenanceReleaseChecksums
=== RUN   TestMaintenanceReleaseChecksums/[MAINT-RECOVERY_AC-011]_legacy_platform_lookup_selects_signed_agent_before_both_helpers
--- PASS: TestMaintenanceReleaseChecksums (0.01s)
    --- PASS: TestMaintenanceReleaseChecksums/[MAINT-RECOVERY_AC-011]_legacy_platform_lookup_selects_signed_agent_before_both_helpers (0.01s)
PASS
ok  github.com/DesyncTheThird/rIOt/scripts  0.014s
```

This is 18 successful actual builds: 15 Linux agent/helper outputs plus the three existing non-Linux agent outputs. The matrix also executed both packaging regressions natively on Linux, with no skips. The container retains these temporary artifacts for integration inspection; no release tag or publication was performed.

## Post-QA apt-mark authority correction — final installer evidence

The retained wildcard `apt-mark hold *` / `unhold *` grants were unsafe: arguments could inject apt configuration options instead of naming packages. They are replaced by these exact argument regexes, with a preinstalled sudo>=1.9.10 gate before installed-file mutation:

```text
riot ALL=(root) NOPASSWD: /usr/bin/apt-mark ^hold [a-z0-9][a-z0-9+._:-]*( [a-z0-9][a-z0-9+._:-]*)*$
riot ALL=(root) NOPASSWD: /usr/bin/apt-mark ^unhold [a-z0-9][a-z0-9+._:-]*( [a-z0-9][a-z0-9+._:-]*)*$
```

The sudo project's [primary sudoers manual source](https://raw.githubusercontent.com/sudo-project/sudo/main/docs/sudoers.mdoc.in) specifies POSIX extended command argument regexes starting with `^` and ending with `$`, available since 1.9.10; special sudoers characters inside regexes need no escaping except `#`. Actual Debian visudo validates these unescaped expressions, and effective `sudo -n -l` output preserves each literally on one line.

The new matrix runs the real setuid sudo executable **as riot**, using only `-n -l` permission queries. Twelve positive cases cover normal, versioned, multiarch and multiple package operands for hold/unhold. Twenty-seven negative cases cover `-oDir::Bin::dpkg=/tmp/evil` before the operation, immediately after it and after a normal operand; `--file`; environment operands/wrappers; paths; semicolons; newlines; empty lists/arguments and option separators. No apt-mark or supplied executable is run. An unsupported sudo1.9.9 fixture additionally proves refusal leaves the original binary, unit and policy unchanged, without stopping the service.

Command remains:

```sh
docker exec -e RIOT_INSTALLER_FIXTURE=1 riot-installer-test-20261020 \
  python3 /src/scripts/test_install_maintenance.py
```

Complete updated output:

```text
test_AC011_changed_signed_asset_preserves_running_install (__main__.InstallerFixtures.test_AC011_changed_signed_asset_preserves_running_install) ... ok
test_AC011_invalid_signature_preserves_running_install (__main__.InstallerFixtures.test_AC011_invalid_signature_preserves_running_install) ... ok
test_AC011_missing_helper_preserves_running_install (__main__.InstallerFixtures.test_AC011_missing_helper_preserves_running_install) ... ok
test_AC012_active_worker_refuses_before_stop (__main__.InstallerFixtures.test_AC012_active_worker_refuses_before_stop) ... ok
test_AC012_candidate_policy_failure_preserves_old_policy (__main__.InstallerFixtures.test_AC012_candidate_policy_failure_preserves_old_policy) ... ok
test_AC012_duplicate_status_keys_refuse_before_stop (__main__.InstallerFixtures.test_AC012_duplicate_status_keys_refuse_before_stop) ... ok
test_AC012_effective_policy_failure_restores_old_policy_before_stop (__main__.InstallerFixtures.test_AC012_effective_policy_failure_restores_old_policy_before_stop) ... ok
test_AC012_invalid_signed_unit_preserves_binary_unit_policy (__main__.InstallerFixtures.test_AC012_invalid_signed_unit_preserves_binary_unit_policy) ... ok
test_AC012_legacy_migration_uses_fresh_signed_inode_and_preserves_identity (__main__.InstallerFixtures.test_AC012_legacy_migration_uses_fresh_signed_inode_and_preserves_identity) ... ok
test_AC012_multilink_root_lock_refuses_before_stop (__main__.InstallerFixtures.test_AC012_multilink_root_lock_refuses_before_stop) ... ok
test_AC012_native_dpkg_lock_refuses_before_stop (__main__.InstallerFixtures.test_AC012_native_dpkg_lock_refuses_before_stop) ... ok
test_AC012_old_sudo_preserves_binary_unit_policy_before_mutation (__main__.InstallerFixtures.test_AC012_old_sudo_preserves_binary_unit_policy_before_mutation) ... ok
test_AC012_real_sudo_lists_safe_hold_names_and_denies_injection (__main__.InstallerFixtures.test_AC012_real_sudo_lists_safe_hold_names_and_denies_injection) ... ok
test_AC012_root_lock_contention_preserves_running_install (__main__.InstallerFixtures.test_AC012_root_lock_contention_preserves_running_install) ... ok
test_AC012_symlink_helper_directory_refuses_before_stop (__main__.InstallerFixtures.test_AC012_symlink_helper_directory_refuses_before_stop) ... ok
test_AC012_unfinished_root_record_refuses_before_stop (__main__.InstallerFixtures.test_AC012_unfinished_root_record_refuses_before_stop) ... ok

----------------------------------------------------------------------
Ran 16 tests in 2.593s

OK
```

The earlier architecture-build evidence remains applicable: this correction changes installer policy generation/version checking and its fixture/evidence only, without changing Go binaries or release build commands. Systemd PID1 lifecycle limitations above remain unchanged.
