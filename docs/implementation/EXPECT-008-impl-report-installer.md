# Implementation Report: EXPECT-008 Part I (installer)

- **Story ID:** EXPECT-008
- **Part:** I (installer and its CI), ADD revision 2 (commit 31cdbc9)
- **Engineer:** Senior Dev Agent (Opus 5.5)
- **Date:** 2026-10-27
- **Detected stack:** Bash installer (`set -euo pipefail`), Python 3 `unittest` fixture run in disposable Debian containers, GitHub Actions. No Go, no web, no new dependency.

## Completed components

| File | Action | Notes |
|---|---|---|
| `scripts/install.sh` | MODIFY | `# BEGIN/END reinstall-notice` block after `download_file`: `notice_snapshot`, `notice_facts`, `notice_fact`, `notice_reason`, `notice_attempt` (worker), `reinstall_notice` (subshell wrapper). One call site at the stop: `reinstall_notice </dev/null 2>/dev/null 9>&- \|\| true`, guarded by `CONFIG_EXISTED`, no bootstrap key, and `systemctl is-active --quiet`. Nothing else changed |
| `scripts/test_install_maintenance.py` | MODIFY | 30 `test_EX8_*` tests; curl stub notice branch (real curl kept as `curl.fixture-real`); `systemctl` stub gains `is-active` and a stop timestamp; loopback listener (HTTP, HTTPS, mTLS, hang, endless chunked body); certificate helpers; system-store CA helper (removed on cleanup); piped runner; `prod_key()`; agent tripwires. Existing tests untouched |
| `.github/workflows/ci.yml` | MODIFY | Fixture step installs `curl gawk` and runs `update-alternatives --set awk /usr/bin/mawk` |

No new fixture file under `scripts/tests/` (the ADD names none).

ADD rev-2 items implemented: exact curl trust set (`--cacert` plus an empty `--capath`, under `env -i PATH=/usr/sbin:/usr/bin:/sbin:/bin LC_ALL=C`), `head -c 4096` response bound with the status code through curl's stderr write-out, no stdin reads (call site `</dev/null`, wrapper `exec </dev/null 2>/dev/null`), `9>&-`, `^riot_[0-9a-f]{64}$` and `_` in the awk plain-scalar set, open-then-verify snapshot, worker/wrapper one-line structure, `LC_ALL=C`.

## AC to test list

All tests are in `scripts/test_install_maintenance.py`.

| AC / condition | Tests |
|---|---|
| AC-001 | `test_EX8_AC001_one_notice_after_validation_before_stop`, `..._success_line_once_and_exit_status_unchanged`, `..._piped_script_runs_to_completion` (C-3) |
| AC-002 | `test_EX8_AC002_no_notice_without_identity_or_running_agent` (fresh, `--bootstrap-key`, no device_id, no api_key, inactive unit; each compared with the same run with the notice call removed) |
| AC-003 | `test_EX8_AC003_abort_before_stop_sends_no_notice` (invalid signature, unit validation, candidate policy, effective policy) |
| AC-004 | `..._server_failures_never_block_install`, `..._curl_runs_under_the_time_bound` (C-4), `..._real_hang_ends_within_10s`, `..._oversize_undeclared_body_is_bounded` (C-2), `..._internal_error_prints_one_line` (C-3), `..._every_reason_is_one_line_and_empty_stderr` (C-3; 27 emulated rows plus 5 skip rows) |
| AC-005 | `..._untrusted_certificate_no_key_sent`, `..._system_store_ca_not_trusted_with_trust_file` (C-1), `..._system_store_positive_control` (C-1), `..._trust_file_rows_pass_empty_capath` (C-1), `..._pinned_certificate_mismatch_no_key_sent`, `..._mtls_presents_client_certificate`, `..._tls_verify_off_skipped`, `..._pin_without_pinned_certificate_skipped`, `..._paths_outside_etc_riot_skipped` |
| AC-006 | `..._key_never_in_argv_output_or_files` (run with and without `bash -x`, accepting and closed port, argv recorders on env/awk/head/stat/wc/tr/timeout/mkdir/rm/cat), `..._key_reaches_only_the_printf_builtin`, `..._environment_cannot_widen_trust_or_log_keys` (C-4, with control) |
| AC-007 | `..._reader_agrees_on_every_awk` (23 cases on mawk and gawk), `..._full_runs_use_mawk` |
| AC-021 | `test_EX8_AC021_no_file_left_behind` |
| AC-024 | `test_EX8_AC024_new_installer_against_old_server` (400 and 404) |
| AC-026 | `test_EX8_AC026_no_power_package_or_agent_execution` plus `ex8_run()` failing any EX8 run whose trace holds `AGENT TRIPWIRE`, `POWER TRIPWIRE`, `PACKAGE` or `UNEXPECTED PACKAGE` |
| C-5 | `test_EX8_reader_rejects_values_that_could_inject_curl_config`; every EX8 key from `prod_key()` |
| C-6 | `test_EX8_snapshot_rejects_swapped_inode` (symlink, FIFO, two links, swap to symlink and swap to another file between open and lstat) |

## Test results (full fixture, run as CI does, in Docker Desktop)

Command, per image, repo scripts mounted read-only at `/src`:

`docker run --rm --mount type=bind,src=<LF copy of scripts>,dst=/src,readonly debian:<image> bash -ec 'apt-get update -qq; apt-get install -y -qq --no-install-recommends python3 openssl sudo systemd ca-certificates curl gawk; update-alternatives --set awk /usr/bin/mawk; RIOT_INSTALLER_FIXTURE=1 python3 /src/scripts/test_install_maintenance.py -v'`

The Windows checkout is CRLF, so the mounted copy was normalised to LF first (CI checks out LF). Throwaway containers only (`--rm`), no hawaii, no device.

| Image | Result | Notes |
|---|---|---|
| `debian:bookworm-slim` | `Ran 98 tests ... OK` (68 existing, 30 new) | Baseline before any change: 68 OK. mawk 1.3.4-20200120 |
| `debian:trixie-slim` | `Ran 98 tests ... OK` (68 existing, 30 new) | Baseline before any change: 68 OK. curl 8.14.1, OpenSSL 3.5 |

All 30 EX8 lines show `... ok` on both images (a second EX8-only run on each image also passed 30/30). `go test ./scripts/` passes (the Go tests that read `install.sh`).

Mutation checks (each run on bookworm, each made the intended tests fail, then discarded): removing `--capath` (C-1 tests fail), removing the `head -c 4096` bound (oversize test fails), key regex without `_` (AC-001 and others fail), removing the stdin isolation and adding a stdin reader (piped and AC-002 tests fail), removing `env -i` (time-bound, environment and static tests fail), removing the inode comparison (swap-to-another-file test fails).

Linter: `bash -n scripts/install.sh` clean. No other linter covers these files.

## Deviations from the ADD

1. **`--pass ''` replaced by `--pass riot-no-passphrase`** (AD-016 step 2 and the mTLS row). curl 8.14.1 on trixie rejects an empty `--pass` argument ("blank argument where content is expected", exit 2), which would have made every mTLS device print "connection failed" on trixie, the OS hawaii runs. Found by `test_EX8_AC005_mtls_presents_client_certificate` on trixie. The dummy word is a constant, not a secret; its only purpose, keeping curl from asking for a passphrase, is unchanged. Needs architect review for ADD wording.
2. Test-only: the curl stub also appends `NOTICE rc=<n>` in real mode (diagnostics, no secret); the awk-agreement test asserts gawk is present as well as mawk (CI now installs gawk). The AC-004 time-bound test accepts `timeout -k 1 [78]` and `--max-time [67]` because `SECONDS` can tick once before curl starts.
3. `notice_attempt` checks for `curl` before reading the config, as AD-016 step 1 says. A `wget`-only host with a registered device therefore prints "curl not available" even when no identity is configured. Kept as specified.

No other file was touched. Existing tests were not modified; no existing test failed because of the extra `systemctl is-active` trace line (ADD 12 note 6).

## Notes for QA

- Run the fixture only in the disposable container (`RIOT_INSTALLER_FIXTURE=1`). The EX8 tests install a test CA into the container's system store and restore it on cleanup, replace `env awk head stat wc tr timeout mkdir rm cat` with argv recorders for the duration of one test, and bind a listener on `127.0.0.1`.
- Probe: an encrypted client key (curl is given the dummy passphrase, so it fails fast and the line reads "connection failed" or "TLS verification failed"; the agent cannot load an encrypted key either). A config whose `server.url` is IPv6 in brackets (accepted by the URL pattern if quoted). `wget`-only hosts (curl check precedes identity check, deviation 3).
- Real-curl tests depend on `curl` being installed in the image; `ex8_real()` fails loudly if `curl.fixture-real` is missing.
