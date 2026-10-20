# Formal Requirements Document

**Story:** APP-WORKSPACE-001 — Unified application workspace  
**Date:** 2026-10-20  
**Status:** Ready for security review and implementation against the paired ADD  
**Baseline:** The Docker workspace and portrait fix are deployed through v2.71.1. This story extends their accepted presentation across the application.

## 1. Outcome and scope

The operator should move between machines, Docker workloads, monitoring and settings without moving between unrelated interface styles. Replace the crowded horizontal application navigation with a persistent grouped sidebar and a small utility topbar. Apply a common resource header and neutral surfaces throughout. Devices remains the canonical list of every registered machine; Docker contains only machines known to support Docker.

This is a frontend integration using existing APIs, routes, capabilities, tokens, components and dependencies. It does not change the authentication model, telemetry, deployment model, retention, database schema or API contracts. It does not add Komodo deployment editing, Git integrations, resource creation, new permission models or new backend features.

## 2. Binding constraints

- Preserve all current public URLs, query parameters, legacy redirects, feature toggles and demo routing. The canonical Devices collection remains `/`; device resources remain `/devices/:id`.
- Preserve the fix that enables protected feature settings only after authentication is confirmed. Loading, login, OIDC discovery and initial setup must remain outside the authenticated shell.
- Preserve stored and live container logs, service selection, history boundaries, retention indicators, gaps, exports, terminal behavior, action locks, progress and confirmations.
- Preserve device administration, probes, alerts, security findings, settings forms, secrets masking, timestamps and current loading/error/empty distinctions.
- Use the UI-FOUNDATION primitives, semantic tokens, focus behavior and thin scrollbars already established. The neutral surface change intentionally supersedes Docker-only surface scoping; severity and chart colors retain their meanings.
- Follow root `CLAUDE.md`: upstream FRD → ADD → security → development → QA → technical writing. No dependency additions/upgrades, backend edits, migrations or production deployment are required by this story.

## 3. Functional requirements

| ID | Requirement |
|---|---|
| FR-001 | Authenticated pages share one application shell, one grouped application navigation and one utility topbar. Desktop navigation remains visible while viewing long resources. |
| FR-002 | Navigation groups are Infrastructure (Overview, Devices), Docker (Containers, Stacks, Hosts, Images, Volumes, Networks), Observe (Probes, Alerts, Security, Activity), Administration (Alert Rules, Agents, Settings). Docker is hidden when its existing feature is disabled. |
| FR-003 | A detail route marks its owning collection active. Devices owns `/devices/:id`, device probes and terminal; Probes owns `/probes/:id`; Security owns `/learn/:findingId`. Exactly one application navigation item is current for a supported route. |
| FR-004 | Preserve existing settings subsection URLs and redirect `/settings/alert-rules` to `/alert-rules`. Settings may retain a compact subsection navigation, but DockerLayout is only its feature gate and outlet; it must not render a second competing application sidebar. Activity is Docker-gated even though it appears under Observe. Agents selects its dedicated item rather than Settings. |
| FR-005 | On narrow and portrait screens navigation collapses to an accessible toggle with visible expanded state. It must not consume permanent horizontal space needed by tables or hide account/search controls. Opening, closing and route selection must be keyboard usable. |
| FR-006 | The topbar exposes the existing command palette search, alerts/unread badge, account actions and clock/version information without repeating all primary links. Search keeps Ctrl/Cmd+K and terminal exclusions. No new search service is introduced. |
| FR-007 | Pages use a shared resource header: title, optional short description or resource type, actions, and an optional metadata row. Resource names wrap, controls wrap, and primary page content has one meaningful h1 rather than stacked shell/page/resource headings. |
| FR-008 | Devices, Dashboard, device detail, device probes and terminal receive the shared header and neutral presentation while retaining existing data, tabs, links, controls and operational workflows. |
| FR-009 | Probes, probe detail, Alerts, Alert Rules, Security and security guidance receive consistent headers and presentation; preserve creation, editing, acknowledgement, filtering, explanations and existing feature gating. |
| FR-010 | Settings and each subsection use the same workspace surfaces and header hierarchy. Preserve validation, dirty/pending state, saved values, confirmation steps and masking of credentials. |
| FR-011 | Neutral surfaces apply to shell, content and inherited gray surface classes globally. Accent remains available for interaction; success/warning/danger remain semantic. Preserve chart contrast and established focus indicators. |
| FR-012 | Docker host lists, counts and host filter options include only records with `docker_available === true`. Offline records with that flag remain visible; availability is not equivalent to online status. |
| FR-013 | Devices continues to show every registered machine, including machines without Docker. Docker host empty states distinguish no known Docker hosts from a text/filter mismatch and from a failed query. |
| FR-014 | Docker containers/stacks/inventory retain host scoping, direct links, resource switching and canonical resource identities. Existing URLs to unavailable/offline hosts must not be rewritten to a different resource. No data is deleted by frontend filtering. |
| FR-015 | Existing responsive tables, optional columns and view preferences, action menus, selection behavior and saved collection return state remain intact. No application-wide horizontal page overflow is introduced. |

## 4. Acceptance criteria

Acceptance tests use the prefix `[APP-WORKSPACE AC-xxx]` in new test names.

| ID | Observable acceptance |
|---|---|
| AC-001 | Authenticated routes show the grouped navigation and small utility topbar; Docker does not add a second sidebar. |
| AC-002 | `/`, `/devices/x`, `/devices/x/probes` and `/devices/x/terminal` select Devices; `/dashboard` selects Overview; each Docker detail selects its collection; `/probes/x` selects Probes; `/learn/x` selects Security; Agents selects Agents and other settings children select Settings. No prefix collision marks two links current. |
| AC-003 | Mobile navigation exposes aria-expanded, can open and close with keyboard, closes after navigation, and leaves the selected content usable at 390px portrait and desktop widths. |
| AC-004 | Login, pending authentication and setup mount no authenticated shell queries or protected features request; logged-out OIDC remains stable. Authenticated mounting still enables features and WebSocket behavior. |
| AC-005 | Search button opens the existing palette; account logout/password links and alert badge remain reachable. Ctrl/Cmd+K continues to exclude terminal input. |
| AC-006 | PageHeader renders title as h1 and optional description, eyebrow, metadata and actions; absent slots leave no empty wrappers. Long titles/actions wrap without forced page overflow. |
| AC-007 | Fleet/device/dashboard/probe/alert/security/settings representative pages use the shared header and preserve their existing interactions under existing regression tests. |
| AC-008 | Docker host listings/counts/options include an online Docker host and an offline Docker host, exclude a non-Docker machine, and never remove that machine from Devices. A list containing only non-Docker hosts shows the Docker-specific empty state. |
| AC-009 | Container and stack logs still render stored history and expose capability-gated live mode; action confirmation/locking regressions stay green; direct resource URLs and collection filters remain valid. |
| AC-010 | Global neutral surfaces and thin scrollbar rules apply outside Docker; status/chart colors and focus tokens remain available. Verify actual appearance in desktop and portrait browser views because unit tests do not compute Tailwind styles. |
| AC-011 | Full frontend tests, TypeScript checking, production build and demo build pass without new dependencies; frontend API contracts and database migrations are unchanged. |

## 5. Delivery and evidence

Security reviews navigation destinations, auth query timing, feature boundaries, keyboard/focus behavior and preservation of secret/action safeguards before development. Development reports list changed components and AC coverage. QA checks representative populated, loading, error, empty, disabled-feature and portrait states; a screenshot alone is not evidence of preserved actions. Technical writing updates the shared UI guidance, navigation terminology and changelog after QA. Production rollout remains a separate operator-authorized step.

