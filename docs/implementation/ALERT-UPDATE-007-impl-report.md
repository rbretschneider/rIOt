# ALERT-UPDATE-007 — overflow and agent update readiness

Stack: Go agent/server, React 19 + TypeScript + Tailwind 4 dashboard.

## Problem and change

Long update failure messages and resource names expanded the Events & Alerts table beyond the viewport. The table now uses fixed columns, wraps messages and resource names (including unbroken paths), and keeps narrow-screen horizontal scrolling inside the bounded table container. Messages remain complete and acknowledgement remains reachable.

Linux update attempts previously downloaded/staged a signed release before discovering that root installation prerequisites were missing. A read-only, bounded preflight now checks the root-owned signed apply helper and probes the exact fixed sudo rule using sudo -n -l. It does not execute privileged work, grant permissions, or install helpers. Missing prerequisites produce an explicit one-time installer-migration error before any staging/download side effects. Existing signed verification and digest-bound completion remain in place. Non-Linux behavior is unchanged.

## Production diagnosis (2026-10-21)

- Hawaii: /usr/local/lib/riot/riot-agent-apply is absent; the agent service still has the legacy shell copy-on-boot hook. The sudoers file is dated March 30. Repeated app/agent releases cannot install the missing root helper/policy.
- Vermont: signed apply helper and fixed rules are present. Journal confirms v2.74.2 installed successfully at 09:06 New York time on October 21, with the original update command completed successfully. This is evidence that the installed modern path works; do not migrate or restart Vermont for Hawaii's failure.
- PiVPN/PiCUPS: screenshot shows the same unavailable-verifier error; their installed files/policy have not been inspected and the exact prerequisite state is UNVERIFIED.

## Validation

- Before changes: all Go tests pass; 86 frontend files / 1,584 tests pass.
- After changes: go test ./... passes; 86 frontend files / 1,585 tests pass; npm run build passes.
- Linux cross-compiled test binary run in Debian container: all four UpdateInstall regression tests pass. Cases cover absent helper, exact read-only sudo probe, denied policy, and preserving staged bytes without downloading on refusal.
- Browser demo checked at 1200 px: table and container 1140 px, acknowledgement right edge 1149 px (inside viewport). At 390 px: document width 380 px, table container 346 px with internal overflow:auto and table width 720 px. No page-wide horizontal overflow.
- Existing frontend test warnings and large-bundle build warning remain. npm run build:demo uses POSIX environment assignment and does not execute on Windows; equivalent VITE_DEMO environment + vite build passed.
- This is not live remediation: the running server/agents were not modified. Root migration and any service restart require separate operator approval under Hawaii rules.

## One-time migration preparation

The existing v2.74.2 install.sh was authenticated locally against checksums.txt.sig with the repository's pinned Ed25519 public key. Its content matches the reviewed scripts/install.sh ignoring CRLF/LF line endings.

Verified installer SHA-256:

```
6d36081d6e1306bdabf12e3231f9b00413ee3f02dfb007ebdf7b9ea43591d236
```

On each confirmed legacy host, use the existing server URL/config and matching signed release installer as root, with --version 2.74.2. Do not use --bootstrap-key or -y: existing enrollment and feature choices must be preserved. The installer backs up installed files, verifies signed release assets before publication, installs fixed policy/helpers, and stops/starts riot-agent as part of migration. This is a one-time installation repair, not a step to repeat for every release. Operator must explicitly authorize the managed file replacements and agent restart; Hawaii's SSH account cannot run this installer passwordlessly.
