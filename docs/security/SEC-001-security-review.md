# Security Review Report
**Story ID:** SEC-001
**Reviewer:** Security Researcher Agent
**Date:** 2026-03-22
**Verdict:** APPROVED

---

## Threat Model Summary

### Assets
- **Security telemetry data**: per-device security posture information (firewall status, failed logins, open ports, SELinux/AppArmor status) -- read-only, already exposed via existing endpoints
- **Update telemetry data**: pending security update counts, unattended upgrades status -- read-only, already collected by agents and stored in JSONB snapshots
- **Certificate metadata**: SSL/TLS certificate expiry information from web server telemetry -- read-only, no private key material involved
- **Security scores**: computed server-side from existing telemetry, no persistent storage of scores

### Threat Actors
- **Unauthenticated external attacker**: relevant if the dashboard is network-exposed without VPN; all modified endpoints sit behind JWT admin auth middleware
- **Authenticated dashboard user**: the only actor defined for this story; has read access to all security data; this is a single-user/homelab system with one admin account

### Attack Surface Introduced
This story introduces **no new attack surface**:
- No new API endpoints are created; two existing GET endpoints are extended with additional response fields
- No new user inputs are accepted; all modified endpoints are read-only GETs with no query parameters, path parameters, or request bodies
- No new authentication or authorization paths
- No database schema changes or migrations
- No new dependencies
- No new tokens, secrets, or credentials
- The data being surfaced (update counts, auto-updates status, cert expiry counts) is already present in the JSONB telemetry snapshots that the handlers already load into memory

---

## Findings

### CRITICAL

None.

---

### HIGH

None.

---

### MEDIUM

None.

---

### LOW / INFORMATIONAL

#### SEC-001-L01: Ambiguous `pending_security_count` semantics when no update telemetry exists
**Severity:** LOW
**Domain:** Data Exposure / API Contract
**Location:** ADD Section 6 (API Interface Contract), `internal/server/handlers/security.go` -- `SecurityDevices` handler
**Description:**
The ADD specifies that `pending_security_count` defaults to `0` when no update telemetry exists, while `unattended_upgrades` defaults to `null`. The frontend must use the presence of `unattended_upgrades` (non-null vs null) as a proxy to determine whether the `0` in `pending_security_count` means "zero pending security updates" or "no update data available." This coupling is fragile -- a future developer may not realize the semantic dependency between the two fields.
**Attack Scenario:**
No exploitable attack scenario. This is a data interpretation concern. A dashboard user could misread a device as having zero pending security updates when in reality the device has no update telemetry at all. This is a display accuracy issue, not a security vulnerability.
**Recommendation:**
Consider making `pending_security_count` a nullable type as well (`*int` / `number | null`), mirroring the `unattended_upgrades` pattern, so both fields independently signal "no data." This is a quality-of-life improvement, not a security requirement.
**Blocks:** Nothing

---

#### SEC-001-L02: N+2 parallel requests pattern for fleet score fetching
**Severity:** LOW
**Domain:** Rate Limiting / Abuse Prevention
**Location:** ADD Section 3 (AD-003), ADD Section 11 (Performance)
**Description:**
The Security page will issue N+2 HTTP requests on load (1 overview + 1 device list + N individual score fetches). For 50 devices, this is 52 concurrent requests from a single page load. While React Query caching (5-minute stale time) mitigates repeated loads, this could cause noticeable latency on the server for large fleets.
**Attack Scenario:**
No exploitable attack scenario. This is the same pattern already used on the Fleet Overview page with `MiniScore`, so it does not represent a regression. The dashboard is behind admin auth, so unauthenticated request flooding is not possible. An authenticated user repeatedly force-refreshing could generate load, but this is a self-hosted tool where the admin is the only user.
**Recommendation:**
Informational only. The ADD correctly identifies this tradeoff and the FRD (BR-002) mandates client-side computation. A future optimization could introduce a batch score endpoint, but that is out of scope for this story.
**Blocks:** Nothing

---

## Positive Observations

1. **No new endpoints**: The design correctly extends existing endpoints rather than creating new ones, keeping the API surface unchanged. This is the right call for adding read-only fields derived from data already in memory.

2. **Authentication coverage**: The security endpoints (`/api/v1/security/overview` and `/api/v1/security/devices`) are within the `r.Group` block at line 152 of `internal/server/router.go` that applies `adminAuth` middleware via `r.Use(adminAuth)`. All modified endpoints remain protected by JWT cookie authentication.

3. **No database changes**: All new data is derived from existing JSONB telemetry snapshots at query time. No migrations, no schema changes, no new indexes -- zero risk of data integrity issues.

4. **Nullable `*bool` for `unattended_upgrades`**: Using a pointer type to distinguish "disabled" (false) from "no data" (nil/null) is the correct Go pattern and avoids the common zero-value ambiguity bug.

5. **Read-only data flow**: The entire story is about surfacing data that already exists in a different view. No write operations, no state mutations, no side effects. The blast radius of any bug is limited to incorrect display.

6. **No new dependencies**: The story explicitly avoids adding new packages, eliminating supply chain risk.

7. **Existing error handling preserved**: The handlers already return structured error JSON with appropriate HTTP status codes. The ADD does not change this pattern.

---

## Verdict Rationale

**APPROVED**: No CRITICAL or HIGH findings exist. The two LOW/INFORMATIONAL findings are minor quality-of-life observations with no exploitable attack scenarios.

This story has an exceptionally small security footprint: it extends two existing read-only GET endpoints with additional fields extracted from data already loaded into memory, moves a UI component from one page to another, and adds client-side sorting. No new authentication paths, no new inputs, no new endpoints, no database changes, no new dependencies. The attack surface is unchanged.

Implementation may proceed. The LOW findings are documented for engineering awareness but do not require resolution before or during implementation.
