# UPDATE-ASSET-004 implementation report

Stack: Go1.24, existing standard library and testify. No new dependency,
API, database, frontend, agent, privileged helper or release workflow change.

The server's platform substring matcher admitted maintenance helpers and
sidecars; whichever appeared last replaced the agent URL. The reported got
hashes match signed v2.74.0 maintenance binaries for amd64/arm64/armv7 exactly.
Read-only live update-check also advertised riot-maintenance-linux-amd64.

Changed only isAgentBinaryAsset to compare the exact riot-agent-<suffix>
basename, adding .exe for Windows. Removed obsolete verifier-only exclusion.
Existing suffix generation, version-derived availability and metadata stay.
Agent signature/checksum and root-helper verification are unchanged. No
production agent update, container recreation or release replacement performed.

## Acceptance and validation

AC001/002: all eight actual release targets, three mixed-asset orders each,
including maintenance after agent, and signature sidecar after helpers.
AC003: helper-only, decoy-only and mixed missing-agent feeds expose no agent URL;
exact matcher negatives cover helper kind, wrong arch, prefix/suffix, text,
signature, checksum, archive, path, case, whitespace and extension lookalikes.
AC004: current/latest/version-derived availability/release/time/checksum/signature
metadata preserved with and without a canonical agent. Existing unsigned,
no-release, dev, downgrade, verifier and backoff tests retained.
AC005: full Go suite includes existing signed download/root-helper verification;
no verification code changed or bypassed.

Full Go baseline passed before new tests. New regression tests failed against
old code (maintenance/sidecar and decoy selection), then passed after the fix.
Focused checker suite passes; full go test ./... and go vet ./... pass.
Full output retained in adjacent UPDATE-ASSET-004-validation.txt. No frontend
code changed, so frontend tests/build were not rerun. Linux race and PostgreSQL
integration checks run through normal PR CI; local Windows tests do not claim
those environment-dependent checks. git diff --check passes. Existing struct
alignment was retained after formatting the changed code to avoid unrelated diff.

Ready for independent QA on the committed clean implementation.
