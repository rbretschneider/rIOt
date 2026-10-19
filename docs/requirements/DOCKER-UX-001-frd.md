# Formal Requirements Document

**Story ID:** DOCKER-UX-001
**Title:** Docker area: fleet-wide containers, first-class stacks, stored and live logs, activity, and inventory
**Author:** Business Developer Agent
**Date:** 2026-10-19
**Status:** DRAFT, revision 3 (clarifications resolved with conservative defaults, see Section 2a; settled and pending product decisions in Section 13)

## Revision history

| Rev | Date | Change | By |
|---|---|---|---|
| 3a | 2026-10-19 | Product-owner decisions recorded: D-21 (ii), D-24, D-30, D-44 to D-48 and D-53 to D-58 accepted with the recommended values (S-10 in §13.1; details in ADD §17.1). D-57 (critical-disk storage scope) and D-58 (device and registration cap) are now requirements. No other requirement changed. | Product owner (recorded) |
| 3 | 2026-10-19 | The product owner settled six decisions: D-1 read-only compose (S-4); D-21 part (i), audit retention of 180 days for the long class (S-5); D-31, no personal names in these documents (S-6); D-35 with D-37, PART-001 merged whole and renumbered as P0's first step (S-7); D-42, out-of-window log timestamps re-stamped and never dropped (S-8, which replaces the security review's drop recommendation); D-43, per-database operator sign-off before any migration reconciliation (S-9). Added FR-166..FR-175, NFR-028, AC-122..AC-133, A-15, R-18, R-19 and Section 8.13 (where out-of-window timestamps come from, and why no data is lost). Folded in these security addendum findings: SEC-021 (FR-166..FR-170, FR-175), SEC-022 (NFR-028), SEC-023 (FR-171, FR-172), SEC-027 (NFR-009), SEC-028 (FR-173) and SEC-029 (NFR-011). SEC-024 to SEC-026 are design-level and stay with the ADD. Changed §3 (P0, P1 and P2 scope and prerequisites), C-04, FR-126, NFR-009, NFR-011, NFR-020, NFR-022, NFR-024, NFR-025, BR-010, §8.1, §8.8, §8.12 rule 1, §10, §13 (D-1 and D-35 moved to the settled list), §14 and R-15. FRD bounds aligned to ADD rev 3 constants (clock-skew event, gap count, per-device ceiling). | Business Developer Agent |
| 2 | 2026-10-18 | Two product decisions settled by the product owner. (1) The fleet-wide Docker section replaces the per-device Docker page; legacy URLs redirect (former D-18 settled). (2) Stored container logs are kept on a rotating basis **and** live streaming is added, both in one Logs tab. Added phase P0 (stored-log rotation). Added FR-148..FR-165, NFR-024..NFR-027, BR-017..BR-019, AC-107..AC-121, and Section 8.12. Adopted AC-106 from the ADD (§10). Changed FR-006, FR-008, FR-028, FR-058, FR-074, FR-075, FR-078..FR-080, FR-083, FR-085, FR-121, NFR-001, NFR-003, NFR-020, BR-001, BR-005, and AC-004, AC-007, AC-040, AC-048, AC-051, AC-052, AC-056, AC-058, AC-085. Wording-only edits: FR-010, FR-054, FR-089, NFR-006, NFR-008, NFR-009, NFR-015, NFR-016, NFR-017, NFR-018, NFR-022, AC-037, AC-059, AC-101, AC-102, AC-103, and the AC-105 phase tag (now P0-P4). Removed decision D-18. Added decisions D-33..D-36 (numbered after the ADD's D-19..D-28 and the security review's D-29..D-31). Changed D-13. | Business Developer Agent |
| 1 | 2026-10-18 | Initial draft (commit 4d6dec1) | Business Developer Agent |

---

## 1. Executive Summary

Today an operator reaches Docker only from inside one device page, sees a tile
grid for that one host, and reads logs as periodically stored snapshots. Those
snapshots are purged by a row-by-row DELETE, which is the pattern that bloated
a production database. Tools such as Komodo, Portainer and Podman Desktop give a
fleet-wide table, stacks as first-class objects, a single obvious action per
resource, live logs, an action history, and image/volume/network cleanup. This
story makes Docker a top-level section of the rIOt dashboard with that layout
and ease of use. The section replaces the per-device Docker page. It keeps every
detail the current pages show, and it keeps stored container logs, now rotated
by whole days without bloat. Live streaming is added on top, in the same Logs
tab. The story is delivered in five phases:

- **P0, stored-log rotation:** daily partitions, a configurable retention, and per-container caps.
- **P1, find and act:** the section, fleet table, bulk actions, resource header, command palette, and the stored-log history view.
- **P2, diagnose:** a Live toggle on top of history, Inspect, Activity, and I/O stats.
- **P3, stacks.**
- **P4, inventory and cleanup:** images, volumes, networks, and prune.

P1 to P4 start only after CMD-LIFECYCLE-001 (command states, steps, per-target
locks) and UI-FOUNDATION-001 (design primitives) are merged to `main`. P0 is not
gated by them.

## 2. Background & Context

### 2.1 Problem statement

- **Docker is hard to find and scoped to one host.** There is no Docker entry
  in the main navigation. To answer "which containers are unhealthy or have
  updates anywhere in my fleet?", the operator has to open every device.
- **Layout does not scale.** The per-host page groups containers into tiles by
  stack. That is good for seeing topology, but it is not sortable, has no
  filters beyond a text box, has no multi-select, and cannot be deep-linked.
- **Actions are ambiguous.** A container shows up to five equal-weight
  buttons. The most relevant one (Update when an update exists, Start when it
  is stopped) is not emphasised, and destructive Stop sits next to harmless
  actions.
- **Logs are not live.** The Logs tab polls stored snapshots every 15 s. They
  come from the `container_logs` collector, which captures the last 50 lines
  per interval for running containers only. An operator who is debugging a
  crash loop cannot follow output as it happens.
- **Stored logs rotate in the bloat-prone way.** The stored log table
  (`container_logs`, migration 000015) is purged by a hard-coded 7-day
  row-by-row DELETE (`internal/server/server.go:490`,
  `ContainerLogRepo.Purge(now − 7d)`). On a production instance this table
  reached about 28 GB while holding less than 1 GB of live data. The
  retention cannot be configured, and one chatty container can dominate
  storage.
- **No action history.** After an update fails, the only trace is a 5 s
  banner, or a command row that the operator has to find on another page. No
  per-container history shows who did what and which step failed.
- **Stacks are not objects.** A compose project exists only as a tile group.
  It has no page, no service table, no stack-level pull, restart, down or up,
  and no way to see the compose file that defines it.
- **No inventory.** Images, volumes and networks are not collected, so unused
  disk space cannot be seen or reclaimed from rIOt.

### 2.2 Persona

| Persona | Description | Goals |
|---|---|---|
| Homelab operator | One person running 2 to 30 hosts with Docker, about 10 to 200 containers in total. Most are compose stacks under `/srv/stacks/<name>`, with some standalone containers. Signs in as the single admin, or through OIDC. Comfortable with the Docker CLI, but wants not to need SSH for routine work. | Spot problems across the fleet in seconds. Update or restart many containers at once, safely. Read recent log history and follow live logs while fixing a problem. Know what rIOt did and why it failed. Reclaim disk space without deleting data by accident. |

### 2.3 Current state (verified on `main@a7b9f9e`)

| Current element | What it does today | Fate in this story |
|---|---|---|
| Route `/devices/:id/containers` with `pages/DeviceContainers.tsx` | Tile grid for one host: header counts, a "Check for Updates" button, and search. Compose stacks shown with `ComposeStackSection`, standalone containers with `CompactContainerTile`. | **Replaced** by the fleet-wide Docker > Containers (settled by the product owner, S-1). No per-device Docker page remains. The URL **redirects** to `/docker/containers?host=<device-id>` (FR-006). The tile layout survives **only** as the optional Tiles view inside the fleet table (FR-028). "Check for Updates" is **kept** (FR-030). |
| Route `/devices/:id/containers/:cid` with `pages/ContainerDetailPage.tsx` | Breadcrumb (host / Containers / name), name, short ID. Falls back to the container name when the container is recreated. | **Replaced** by the canonical container page. The URL **redirects** (FR-007). The behaviour of surviving a recreate is **kept** and becomes the default (FR-009). |
| `components/ContainerDetail.tsx` (`ContainerDetailContent`) | Header actions (Update, Check Updates, Restart, Stop, Start). Tabs General / Network / Volumes / Logs / Terminal. General shows status, live gauges, metric history (CPU and memory), alert rules, the 200 most recent fleet events filtered to this container, source, description, environment, and labels. | Actions **move** to the resource header (FR-046..FR-052). General, Network and Volumes **merge** into Overview with all content kept (FR-056). Metric history **moves** to Stats (FR-057). Logs becomes **one tab** with stored history (P1) plus a Live toggle (P2). Terminal is **kept** (FR-059). The Environment section is **removed**, because the agent never fills in `env`. Environment viewing moves to Inspect, with redaction (FR-061, FR-086). |
| `components/ComposeStackSection.tsx` | Stack tile: running count, "Update Stack (n)", the auto-update toggle, network cluster groups, and update confirmation through `UpdateConfirmModal`. | **Kept** inside the Tiles view. In Phase 3 its heading links to the stack page, which becomes the primary place for stack actions (FR-115). |
| `UpdateConfirmModal`, `NetworkClusterGroup`, `CompactContainerTile`, `ContainerStatusBadge`, `ContainerMetricChart`, `Terminal` | Supporting components | **Kept** and reused. Each is migrated to UI-FOUNDATION primitives when it is touched. |
| `GET /api/v1/fleet/containers` (FLEET-DASH) | Slim fleet rows for the dashboard leaderboard | **Extended** with the fields the table needs (FR-064). |
| `container_logs` collector, `container_logs` table (migration 000015), and `GET .../containers/{cid}/logs` | Stored log snapshots. Hard-coded 7-day retention by row DELETE. No per-container storage cap beyond the collector's tail of 50 lines. | **Kept** and made the history half of the Logs tab (settled by the product owner, S-3). Rotation moves to whole-day partitions dropped by age (FR-148). Retention becomes a dedicated setting, default 7 days (FR-149). Per-container caps are added (FR-152, FR-153). |
| Agent `docker` collector | Container list and stats. No start time, exit code, running image ID, or network/block I/O. | **Extended** with additive fields (FR-062). No new collector name. |

Constraints carried in from other stories:

- Single tenant, single admin (JWT cookie, optional OIDC). There is no RBAC.
- On a production instance, Postgres bloat reached about 28 GB in
  `container_logs` while holding less than 1 GB of live data, because of
  DELETE-based retention (PART-001). PART-001 (unmerged branch
  `part-001-partitioning`) converts `container_logs` and five other tables to
  daily range partitions.
- Collectors run only when they are listed in the agent's
  `collector.enabled` whitelist.
- `RIOT_RETENTION_DAYS` (default 30) governs other retained data today. It does
  not govern container logs.

## 2a. Resolved Clarifications

No product owner was available for a live interview in revision 1. Every Stage 1
question was resolved with the conservative default recorded here. Defaults
that are genuine product choices are also listed in Section 13 for the product owner to
confirm or change. Rows C-13 and C-14 were settled by the product owner for revision 2.
Every entry here is binding for this draft.

| ID | Question | Resolution |
|---|---|---|
| C-01 | Is this one FRD or one per sub-story? | One FRD, sliced into phases P0 to P4 (Section 3). The architect may give each phase its own child story ID. |
| C-02 | How is a container identified in URLs? | By host plus container name, so links survive a recreate (BR-002). |
| C-03 | How is a stack identified? | By host plus compose project name. The same project name on two hosts is two stacks (BR-003, D-10). |
| C-04 | Is compose editing in scope? | No. Viewing is read-only. Settled by the product owner (S-4): rIOt never writes compose or `.env` files. |
| C-05 | Can secret values ever be revealed? | No. Redaction happens before data leaves the host (BR-004, D-2). |
| C-06 | Is container Remove in scope? | No (D-4). |
| C-07 | Is inventory a new opt-in collector or part of the existing one? | A new opt-in collector (D-6). |
| C-08 | Where is the network/block I/O history stored? | In the existing container metrics history, only after PART-001 is merged. Until then only current rates are shown (D-7). |
| C-09 | Does Activity include Docker events that rIOt did not trigger? | Yes, with the actor "Agent (observed)" (D-15). |
| C-10 | Are there permissions per role? | No. Any authenticated operator can do every action, as today (BR-013). |
| C-11 | Does the selection belong in the URL? | Yes, capped at 200 entries, and only for visible rows (FR-026, BR-012, D-8). |
| C-12 | Does live logging need an agent config change? | No. It is available on any agent version that supports it and has the `docker` collector enabled (NFR-016). |
| C-13 | Does the fleet-wide section replace the per-device Docker page? | **Yes (settled by the product owner, S-1).** The device page links into the fleet view filtered to that host. Old URLs redirect (BR-014, BR-018). |
| C-14 | Stored logs, live logs, or both? | **Both (settled by the product owner, S-3).** Stored logs on a rotating basis, plus on-demand live streaming, in one Logs tab (BR-005, BR-017, BR-019). |

## 3. Phased Delivery Plan

| Phase | Name | Scope (FR ranges) | Prerequisites | Business value |
|---|---|---|---|---|
| P0 | Stored-log rotation | FR-148..FR-156, FR-166..FR-173 | **Not** gated by CMD-LIFECYCLE-001 or UI-FOUNDATION-001. P0's first step merges PART-001 whole, with its migrations renumbered above `000023_command_lifecycle` (NFR-024, S-7), under the migration-safety rules of NFR-028. FR-166 to FR-173 must be in place before `container_logs` rows are routed to partitions. FR-152 and FR-153 have no prerequisite and may ship first. FR-154 and FR-155 must not be released before FR-173 (masking at ingest). | Removes the live bloat risk in stored container logs. Makes retention operator-controlled. Stops one chatty container, or one misbehaving agent, from dominating storage. Keeps every log line, even from devices with a wrong clock. |
| Gate | Prerequisites for P1 to P4 | none | CMD-LIFECYCLE-001 and UI-FOUNDATION-001 both merged to `main` (NFR-001) | Gives state tracking, steps, locks, and accessible primitives that P1 to P4 rely on |
| P1 | Find and act | FR-001..FR-073, FR-157..FR-161, FR-174 | Gate. An agent release with the additive collector fields; the UI degrades gracefully on older agents. The history view works on P0 or pre-P0 storage. | Fleet-wide visibility, deep links, safe bulk actions, one obvious action, fast navigation, and a usable stored-log history |
| P2 | Diagnose | FR-074..FR-107, FR-162..FR-165, FR-175 | P1. Agent support for live logs and on-demand inspect. Actor attribution and step output recording on commands. Security review of log streaming and redaction. FR-107 additionally needs PART-001 merged (D-7). | Live tail on top of history, a real action history, and inspect without SSH |
| P3 | Stacks | FR-108..FR-127 | P1. P2 for merged stack live logs (FR-121) and stack Activity (FR-122). Agent support for the new stack actions and compose file reads. | Stacks as first-class objects, with lifecycle actions and the compose definition visible |
| P4 | Inventory and cleanup | FR-128..FR-147 | P1. P2 Activity, for recording prune history. The AGENT-PRIV capability statement (NFR-014). Collector whitelist rollout (NFR-015). | Disk usage visibility and safe reclamation |

A sub-navigation entry and a tab appear only once the phase that delivers them
has shipped (FR-002, FR-054). Each phase can be released on its own. P0 can run
in parallel with the Gate and with P1.

## 4. Actors

| Actor | Description | Permissions |
|---|---|---|
| Operator | The authenticated dashboard user: the local admin, or an OIDC-linked identity. There is one role. | Views everything in the Docker area. Runs every container, stack and prune action. Reads stored log history, and opens live logs, Inspect, compose and env views, and Terminal (when the `docker_terminal` feature is on). Sets the container-log retention in the server configuration. |
| rIOt server | REST API, dashboard WebSocket hub, command service (CMD-LIFECYCLE-001), agent connection registry, retention worker | Checks that every requested resource belongs to the named host. Stores and rotates collected log lines. Relays live logs without storing them, and fans one agent stream out to several viewers. Records activity and audit entries. |
| rIOt agent (current release) | Runs on each host, with Docker access through the docker socket group and no extra sudo. Authenticates with `X-rIOt-Key` (plus mTLS when enabled). | Collects stored log lines within the per-container caps. Streams logs, returns redacted inspect data and redacted compose/env content for its own host only. Runs the actions it is sent, and collects inventory when that collector is enabled. |
| Legacy agent | An agent released before the phase in question | Sends only the fields and actions it already knows. The UI shows "Requires agent update" wherever a capability is missing. The server's stored-log ceiling (FR-153) still applies to it. |
| Auto-update / auto-patch dispatcher | Server-internal automation | Creates commands that appear in Activity with the actor "System". |
| Docker engine (on the host) | The source of truth for containers, images, volumes, networks and log output | Emits events that the agent forwards. These appear in Activity as "Agent (observed)". |
| Demo visitor | An anonymous visitor to the static demo build | Sees every shipped Docker page with simulated data. Every action is simulated. |

## 5. Functional Requirements

Each requirement carries its phase tag. Section 8 defines the terms it uses
(column semantics, chips, action rules, redaction rules, bounds, and the log
boundary rules).

### 5.1 Docker section and navigation [P1]

- **FR-001 [P1]:** The primary navigation must contain a top-level "Docker" entry on every authenticated page.
- **FR-002 [P1]:** The Docker section must show a sub-navigation with entries in this order: Containers, Stacks, Images, Volumes, Networks, Activity. An entry must be present only once its phase has shipped: Containers in P1, Activity in P2, Stacks in P3, and Images, Volumes and Networks in P4.
- **FR-003 [P1]:** Selecting "Docker" must open Containers. The current sub-navigation entry must be marked as the current page for assistive technology.
- **FR-004 [P1]:** When no host reports Docker as available, every Docker page must show an empty state that says no host reports Docker. It must not show an error or an empty table.
- **FR-005 [P1]:** A failed data fetch on any Docker page must show the UI-FOUNDATION error state, with the message and a Retry button. It must never show the empty state from FR-004.
- **FR-006 [P1]:** `/devices/:id/containers` must redirect, replacing the history entry, to the fleet view filtered to that host: `/docker/containers?host=<device-id>`. If the device does not exist, it must show "Device not found" with a link to Fleet. No page scoped to a single device may render the Docker container list (BR-018).
- **FR-007 [P1]:** `/devices/:id/containers/:cid` must redirect, replacing the history entry, to the canonical page of the container that currently has short ID `:cid` on that host. If no container has that short ID, it must show "Container not found" with a link to Docker > Containers filtered to that host.
- **FR-008 [P1]:** Every existing link to a host's Docker page, including the Docker link on the device page, must point to `/docker/containers?host=<device-id>`.
- **FR-009 [P1]:** The canonical container page address must identify the container by host and container name. After the container is recreated with a new ID and the same name, the same address must keep showing it.
- **FR-010 [P1]:** Every Docker page and tab state described in this FRD must be reachable through its own URL, and must render correctly when that URL is opened directly in a new browser tab. The one exception is the Live toggle of the Logs tab (FR-164).

### 5.2 Containers table [P1]

- **FR-011 [P1]:** Docker > Containers must list, in one table, every container in the latest telemetry of every host that reports Docker as available.
- **FR-012 [P1]:** The table must have these columns, in this order: selection checkbox, status, name, stack, host, image, ports, CPU, memory, and uptime/exit. Section 8.2 defines each column.
- **FR-013 [P1]:** The status column must show state and health as an icon with a text alternative. It must not rely on colour alone.
- **FR-014 [P1]:** The name cell must link to the canonical container page. When a `riot.*` display name is set, the cell must show it as the primary text and the Docker container name as secondary text.
- **FR-015 [P1]:** The stack cell must show the compose project name, or "—" for a standalone container. In P1, activating it must filter the table to that stack. From P3 it must link to the stack page (FR-124).
- **FR-016 [P1]:** The host cell must show the hostname exactly as reported and must link to the device page.
- **FR-017 [P1]:** The image cell must show `repository:tag`. It must show an "Update available" badge only when the container's update status is `update_available`, and the badge's tooltip must state the reason. The statuses `pinned` and `check_failed` must not show that badge (CMD-LIFECYCLE-001 FR-053).
- **FR-018 [P1]:** The ports cell must render each published port according to the link rules in Section 8.4. When a container has more than 3 published ports, the cell must show the first 3 and a "+N" control that reveals the rest.
- **FR-019 [P1]:** The CPU and memory cells must show current usage for running containers, and "—" for any other state. When a memory limit is set, the memory cell must also show usage as a percentage of the limit.
- **FR-020 [P1]:** The uptime/exit cell must follow Section 8.2. Uptime is measured from the last start, not from creation. When the host's agent does not report start time or exit code, the cell must show Docker's status text instead (for example "Up 3 hours").
- **FR-021 [P1]:** A search box must filter rows with a case-insensitive substring match on display name, container name, image, stack, hostname, `riot` group, and `riot` tags.
- **FR-022 [P1]:** Filter chips Running, Stopped, Unhealthy and "Updates available" must be shown, each with a count. Chip semantics and count rules are in Section 8.3.
- **FR-023 [P1]:** A host filter and a stack filter must each allow more than one value. The stack filter must offer only stacks on the selected hosts, or on all hosts when no host is selected.
- **FR-024 [P1]:** Filters must combine as: search AND host AND stack AND (any selected chip). When no chip is selected, the chips must not filter at all.
- **FR-025 [P1]:** Every column except selection and ports must be sortable, ascending and descending. The default sort must be name, ascending. Rows that tie must be ordered by hostname, then container name.
- **FR-026 [P1]:** Search, chips, host filter, stack filter, sort, view mode and selection must be stored in the page URL. Opening that URL must restore them. Browser back and forward must step through filter changes. Selection must be stored only when 200 or fewer rows are selected. Above that, the URL must omit the selection and the page must keep working.
- **FR-027 [P1]:** Unrecognised or malformed URL values must be ignored without an error, and the valid remainder must still be applied. A selection entry that does not match a currently visible row must be dropped.
- **FR-028 [P1]:** A view toggle must switch between Table (the default, D-9) and Tiles. Tiles must render the existing stack, network-cluster and standalone tile layout, with the same search and filters applied. The Tiles view inside the fleet page is the only place the tile layout appears. A host-filtered Tiles view replaces the former per-device Docker page. Multi-select and the bulk bar must be available only in Table view.
- **FR-029 [P1]:** When the URL does not set a view mode, the page must use the last view mode chosen in this browser, and Table when none is stored.
- **FR-030 [P1]:** The page must provide "Check for updates". It must send the existing update re-check command to every host that matches the current host filter, or to every Docker host when no host filter is set. It must report per-host results in a toast.
- **FR-031 [P1]:** When filters match no rows, the table must show "No containers match these filters" with a "Clear filters" action that resets search, chips, host and stack.
- **FR-032 [P1]:** Rows from a host that the dashboard currently shows as offline must carry a "Host offline" indicator. The indicator must show the time the host was last seen, using `<RelativeTime>`.
- **FR-033 [P1]:** Live telemetry updates must update row values without losing selection, keyboard focus, scroll position, filters or sort.
- **FR-034 [P1]:** When the active sort column is CPU, memory or uptime, rows must be re-ordered no more than once every 5 seconds. Re-ordering must not happen while a row has keyboard focus or the pointer is over the table.
- **FR-035 [P1]:** Under the table, the page must show "Showing N of M containers on K hosts", where N is the number of visible rows.

### 5.3 Selection and bulk actions [P1]

- **FR-036 [P1]:** Each row must have a checkbox. The header checkbox must select or deselect every visible row, and must show a mixed state when only some visible rows are selected.
- **FR-037 [P1]:** Shift plus click on a row checkbox must select every visible row between the last toggled row and that row.
- **FR-038 [P1]:** When a filter change hides a selected row, that row must be deselected (BR-012).
- **FR-039 [P1]:** While one or more rows are selected, a bulk action bar must show the selection count, the actions Start, Stop, Restart and Update, and Clear selection. Each action label must show how many selected containers it applies to, for example "Restart (4)". An action that applies to none of them must be disabled. Applicability is defined in Section 8.5.
- **FR-040 [P1]:** Activating a bulk action must open a confirmation that shows:
  - the applicable targets, grouped by host;
  - the selected containers that will be skipped, each with its reason;
  - the network-dependent containers the action will also stop or restart.
- **FR-041 [P1]:** Bulk Stop must use the danger confirmation. When it has more than 5 targets or spans more than one host, the operator must type the target count before the confirm button becomes enabled (D-12).
- **FR-042 [P1]:** A confirmed bulk action must create one command per target through the command service. A target that already has a non-terminal command must be reported as "Already in progress", linked to the existing command, and the remaining targets must still be dispatched.
- **FR-043 [P1]:** After confirmation, a progress panel must list each target with its live lifecycle state and step, using the CMD-LIFECYCLE-001 labels (its FR-090 and FR-095), until every target is terminal. A summary toast must then show the counts of succeeded, failed, and already-in-progress targets.
- **FR-044 [P1]:** A single bulk action must accept at most 100 targets. Above that, the confirm button must be disabled and the confirmation must say to narrow the selection.
- **FR-045 [P1]:** When any target's host is offline, the confirmation must say that commands for that host will wait until the host reconnects, and expire if it does not reconnect within the command's queued TTL.

### 5.4 Resource header [P1, stack header P3]

- **FR-046 [P1]:** The container page header must show the breadcrumb Docker > Containers > `<host>` > `<container name>`. Each ancestor must be a link: Docker > Containers filtered to that host for the host crumb.
- **FR-047 [P1]:** The header must show the display name and container name, a state badge, a health badge (only when the container defines a health check), host (as a link), stack (as a link once P3 ships, plain text before that), `repository:tag` with the update badge from FR-017, and the uptime/exit text from Section 8.2.
- **FR-048 [P1]:** The header must show at most one primary action, chosen by the rules in Section 8.5 and styled as the primary Button variant.
- **FR-049 [P1]:** The header must show the secondary actions that Section 8.5 lists for the container's state. It must not repeat the action that is currently primary.
- **FR-050 [P1]:** A "More actions" (⋯) menu must contain the non-destructive items listed in Section 8.5. Any destructive item must appear after a visual divider, use danger styling, and be the last group in the menu.
- **FR-051 [P1]:** While the container's target has a non-terminal command, the header must show that command's lifecycle label and step in place of the primary action, and must disable every action for that target. Cancel must be offered only while the command is `queued` (CMD-LIFECYCLE-001 FR-091 and FR-098).
- **FR-052 [P1]:** Update, Restart and Stop must require confirmation. Update must use the existing update confirmation, including its list of network dependents. Stop must use the danger variant. Start and "Check for updates" must run without confirmation.
- **FR-053 [P3]:** The stack page header must show the breadcrumb Docker > Stacks > `<stack>` on `<host>`, the stack state (running services out of total), the update count, at most one primary action (Section 8.5), the secondary actions, and a ⋯ menu that places Down in a separated destructive group.

### 5.5 Container page tabs [P1 structure, later phases fill tabs]

- **FR-054 [P1]:** The container page must provide tabs in this order: Overview, Logs, Activity, Inspect, Terminal, Stats. Activity and Inspect must appear from P2. Terminal must appear only when the `docker_terminal` feature is enabled. There must be exactly one Logs tab: no separate Live tab or History tab.
- **FR-055 [P1]:** The selected tab must be stored in the URL. Opening a URL that names a tab must open that tab. With no tab named, Overview must open.
- **FR-056 [P1]:** Overview must keep everything the current General, Network and Volumes tabs show:
  - State, Docker status text, restart policy, restart count, health, and image freshness.
  - Live CPU and memory gauges.
  - Applicable alert rules, with "Add alert" and "Manage all rules".
  - Recent events for this container.
  - Source repository and description.
  - Labels, with values redacted per Section 8.6.
  - The ports table, the networks table, and the mounts table.
- **FR-057 [P1]:** The CPU and memory history charts, with ranges 1h, 6h, 24h and 7d and the remembered range choice, must move to the Stats tab. Overview must link to Stats.
- **FR-058 [P1]:** The Logs tab must open on the stored log history. In P1 it must provide:
  - the history time range, paging and empty states of FR-157 to FR-161;
  - a case-insensitive substring text filter that shows only matching lines, highlights each match, and shows the match count;
  - a stream toggle with the values both (the default), stdout and stderr. stderr lines must carry an "ERR" text marker and must not be distinguished by colour alone;
  - wrap (on by default);
  - timestamps (on by default), in the operator's preferred time zone through the UI-FOUNDATION formatter;
  - Download, which saves every line currently loaded in the view for the selected streams as a plain-text file named `<host>_<container>_<UTC timestamp>.log`. The text filter does not apply to the download.
  
  The fixed line-limit buttons (200, 500, 1000) of today's view are replaced by the time range. When the host does not run the `container_logs` collector, the tab must say so, naming the collector, as it does today.
- **FR-059 [P1]:** The Terminal tab must behave exactly as the current Terminal tab, including the message shown when the container is not running.
- **FR-060 [P1]:** In P1 the Stats tab must show the current CPU and memory values and the history charts from FR-057.
- **FR-061 [P1]:** The Environment section must be removed from Overview. The container's environment must be viewable only in Inspect (FR-086).

### 5.6 Agent and fleet data, additive [P1]

- **FR-062 [P1]:** The existing `docker` collector must additionally report these values for each container:
  - last start time;
  - for a container that is not running: last finish time and exit code;
  - the ID of the image the container is running;
  - for a running container: cumulative network bytes received and transmitted, and cumulative block bytes read and written.
- **FR-063 [P1]:** The additions in FR-062 must not introduce a new collector name, and must not require any agent configuration change.
- **FR-064 [P1]:** The fleet container data used by the table must include every value needed by Section 8.2 and by the chips:
  - state, Docker status text, health, start time, exit code, and published ports;
  - update status and reason;
  - compose project and service;
  - the `riot.*` display name, icon, group, and tags;
  - CPU, memory usage, and memory limit.

  It must continue to exclude environment, mounts, network attachments, and every label other than the compose project, the compose service, and the `riot.*` display labels.

### 5.7 Command palette [P1]

- **FR-065 [P1]:** Ctrl+K (Windows and Linux) and Cmd+K (macOS) must open the command palette from any authenticated page. A visible control in the page header must also open it.
- **FR-066 [P1]:** The shortcut must not open the palette while keyboard focus is inside a Terminal. The keystroke must reach the terminal instead.
- **FR-067 [P1]:** The palette must search hosts, containers, stacks, and the Docker sub-pages. Results must be grouped by type, and each result must show its type, its name, and (for containers and stacks) its host.
- **FR-068 [P1]:** Matching must be case-insensitive, and every whitespace-separated query token must appear in the result's name, host, stack or image. Ranking within a group must be:
  1. results whose name starts with the query;
  2. then results whose name contains the query;
  3. then matches on other fields.
- **FR-069 [P1]:** The palette must show at most 50 results. When more match, it must show "N more, refine your search".
- **FR-070 [P1]:** Up and Down must move the active result. Enter must navigate to it and close the palette. Escape must close the palette and return focus to the element that had focus before it opened.
- **FR-071 [P1]:** With an empty query, the palette must show up to 8 resources recently visited in this browser, newest first.
- **FR-072 [P1]:** The palette must include containers on offline hosts, marked "Host offline".
- **FR-073 [P1]:** The palette must not show a resource that no longer exists in the latest fleet data, even when that resource is in the recent list.

### 5.8 Live logs [P2]

- **FR-074 [P2]:** The Logs tab must provide a "Live" toggle in the same tab as the stored history (FR-058). The toggle must be enabled only when the container is running and its agent supports live logs. Otherwise it must be disabled, with a short reason: "Container is not running" or "Live logs require an agent update". The history stays visible in every case. There must be no separate Live mode that replaces the history view.
- **FR-075 [P2]:** Turning Live on must append the streamed tail directly below the history lines already shown, following the boundary rules in Section 8.12. The lines must come from the agent through the agent's existing connection to the server. The server must relay them to the browser and must not persist anything from the live stream. Stored history keeps coming only from the `container_logs` collector (BR-005).
- **FR-076 [P2]:** Turning Live on must turn Follow on, which keeps the newest line in view. Scrolling up must turn Follow off. A "Jump to latest" control must scroll to the newest line and turn Follow back on.
- **FR-077 [P2]:** While Live is on, Pause must freeze the visible lines while new lines keep buffering, and must show a count of lines received since the pause. Resume must show the buffered lines and turn Follow on.
- **FR-078 [P2]:** The text filter, stream toggle, wrap, timestamps and Download controls from FR-058 must apply to the whole view, meaning the history lines and the live lines together. Download must include both. Masking (FR-089) applies to everything shown and downloaded. The live tail has no separate "Since" control: its starting point is the boundary (Section 8.12), and the history range is chosen with FR-157.
- **FR-079 [P2]:** Turning Live off must stop the stream and keep every line already shown. It must then resume history refresh (FR-160), appending only stored lines that are newer than the last line shown, under the identity rule in Section 8.12.
- **FR-080 [P2]:** The whole view (history plus live lines) must hold at most the view buffer bound from Section 8.8. When the bound is reached, the oldest lines must be dropped and a notice must say "Older lines were dropped from view".
- **FR-081 [P2]:** While Live is on, a connection indicator must show one of these session states: Connecting, Live, Reconnecting, Ended, Stopped, or Unavailable (Section 8.9). After an unplanned disconnect the stream must reconnect automatically and insert a visible gap marker that records the time of the gap.
- **FR-082 [P2]:** When the container stops during a stream, the stream must end with the marker "Container exited (code N)". If the container starts again, or is recreated with the same name, while the page is open and Live is still on, the stream must resume automatically with a "Container restarted" marker.
- **FR-083 [P2]:** When the browser tab has been hidden for more than 5 minutes with Live on, the stream must stop (state Stopped). When the tab becomes visible again, the stream must resume from the boundary of the last line shown (Section 8.12), with a gap marker.
- **FR-084 [P2]:** When a container produces more than the per-session line-rate bound (Section 8.8), the excess lines must be dropped at the source and the stream must insert a "N lines dropped" marker. A line longer than the line-length bound must be truncated, with a "[truncated]" marker.
- **FR-085 [P2]:** When the per-host bound on distinct live streams is already reached, turning Live on for a container that has no live stream yet must show "Too many live log sessions on <host>. Close another log view and retry." It must not end any existing stream. A viewer that joins an existing shared stream (FR-165) does not count against this bound, but does count against the viewers-per-stream and server-wide viewer bounds (Section 8.8).

### 5.9 Inspect and masking [P2]

- **FR-086 [P2]:** The Inspect tab must show the container's complete inspect document as formatted, indented JSON. The document must be fetched from the agent when the tab opens and when the operator selects Refresh. It must not be stored on the server.
- **FR-087 [P2]:** Before the document leaves the host, every value covered by the redaction rules in Section 8.6 must be replaced with the marker `<redacted>`. The tab must show "N values redacted".
- **FR-088 [P2]:** The tab must provide copy to clipboard (of the redacted document), a text search that highlights matches inside the document, and collapsing and expanding of objects and arrays.
- **FR-089 [P2]:** Every log line shown, downloaded or relayed by the Docker area (stored history and live tail alike) and every Activity output text must pass through the log masking rules in Section 8.7 before it reaches the browser (D-3).
- **FR-090 [P2]:** When the host is offline or its agent does not support inspect, the tab must say which of the two applies. When the agent does not answer within 10 seconds, the tab must show an error with Retry.

### 5.10 Activity [P2]

- **FR-091 [P2]:** The container Activity tab must list every command whose target is this container (by host and name, across recreates), every stack command for this container's stack, every bulk command that included this container, and the Docker lifecycle events recorded for this container (D-15).
- **FR-092 [P2]:** Each entry must show:
  - the action label;
  - the actor (FR-093);
  - the lifecycle state, using canonical labels;
  - requested, started and finished times;
  - the duration;
  - for a failed entry, the failure step and message.
- **FR-093 [P2]:** The actor must be recorded at command creation and shown as one of:
  - the signed-in operator's identity, for user-initiated commands: the local admin username, or the OIDC preferred username or subject;
  - "System (auto-update)" or "System (auto-patch)", for dispatcher-created commands;
  - "Agent (observed)", for Docker events that rIOt did not trigger;
  - "Unknown (before activity tracking)", for commands created before this capability existed.
- **FR-094 [P2]:** Expanding an entry must show a timeline of its steps. Each step must show its humanised name, its start time, its duration, its outcome, and its output text, within the bounds in Section 8.8. When output was cut by a bound, the step must show "Output truncated".
- **FR-095 [P2]:** A non-terminal entry must update its state, step and output live, without a page reload.
- **FR-096 [P2]:** Entries must be ordered newest first, 25 per page, with filters for lifecycle state and entry type (Command or Docker event).
- **FR-097 [P2]:** Activity must include every matching entry within its retention period. It must not be limited to entries found among the most recent fleet-wide events, which is how the current "Recent Events" section works.
- **FR-098 [P2]:** Docker > Activity must list the same kinds of entries for every host. It must have filters for host, stack, container, actor, lifecycle state and time range, stored in the URL. Each entry must link to its resource page.
- **FR-099 [P2]:** Output text must be shown as plain text. Terminal control (ANSI) sequences must be stripped, and no output may ever be interpreted as markup.
- **FR-100 [P2]:** Activity entries for a container that no longer exists must stay visible on Docker > Activity, and must link to the host instead of the missing container.

### 5.11 Stats [P2]

- **FR-101 [P2]:** The Stats tab must show the current network receive and transmit rates and the current block read and write rates, in bytes per second, averaged over the most recent collection interval.
- **FR-102 [P2]:** When the host's agent does not report I/O counters, the I/O figures must show "—" with the tooltip "Requires agent update".
- **FR-103 [P2]:** Rates must never be shown as negative. When a counter goes down (for example after a container restart), that interval must show "—".
- **FR-104 [P2]:** All Stats charts must label their time axis in the preferred zone (UI-FOUNDATION-001 FR-105).
- **FR-105 [P2]:** When a CPU or memory limit is set for the container, the CPU and memory history charts must show that limit as a reference line, labelled with its value.
- **FR-106 [P2]:** Stats values must update as new telemetry arrives, without a page reload.
- **FR-107 [P2, needs PART-001]:** When D-7 is confirmed and PART-001 is merged, the Stats tab must also show network and block I/O history charts with the same 1h, 6h, 24h and 7d ranges as the CPU and memory charts. Until both conditions hold, these charts must not be shown.

### 5.12 Stacks [P3]

- **FR-108 [P3]:** Docker > Stacks must list one row per stack (host plus compose project) with:
  - a status icon (Running, Partial or Down; Section 8.10);
  - the name;
  - the host;
  - running services out of total;
  - the count of services with updates available;
  - the auto-update state.
- **FR-109 [P3]:** The Stacks list must provide search, the chips Running, Partial, Down and "Updates available" with counts, a host filter, sorting, and URL state. These must behave like FR-021 to FR-027.
- **FR-110 [P3]:** The stack page must provide the tabs Services, Logs, Activity and Compose, in that order. The selected tab must be stored in the URL.
- **FR-111 [P3]:** The Services tab must list each container of the stack with its service name, container name, status and health, image with update badge, ports, CPU, memory, and uptime/exit, using the column definitions in Section 8.2. Each row must link to the container page. A network-dependent service must show "Network via <parent>". The tab must also show the combined current CPU and memory of the stack's running containers.
- **FR-112 [P3]:** The stack actions Pull, Update, Redeploy, Restart, Down and Up must behave as defined in Section 8.11.
- **FR-113 [P3]:** Each stack action must create one command whose target is the stack, through the command service. On a conflict, the page must attach to the existing command and show its progress.
- **FR-114 [P3]:** Confirmation must follow Section 8.11:
  - Down: danger, and the operator must type the stack name.
  - Redeploy and Restart: confirm, listing affected services and network dependents.
  - Update: the existing update confirmation.
  - Pull and Up: no confirmation.
- **FR-115 [P3]:** The auto-update toggle for the stack must move from the stack tile to the stack page header. The Tiles view must keep its toggle, and its stack heading must link to the stack page.
- **FR-116 [P3]:** After Down, and whenever a known stack has no containers, the stack must stay listed with state Down and with Up as its primary action (BR-015).
- **FR-117 [P3]:** The ⋯ menu of a Down stack must offer "Forget stack". It must remove the stack from rIOt's list, must not change anything on the host, and must use a primary-variant confirmation. A forgotten stack must reappear automatically when its containers appear again.
- **FR-118 [P3]:** Every stack action and every compose or env read must name the stack by host and project. The server must resolve the working directory and file paths from what that host has reported for that project. A request that supplies a path must be rejected.
- **FR-119 [P3]:** The Compose tab must show each compose file declared for the stack, read-only, in a monospace view with line numbers. With more than one file, each file must have its own sub-tab. Content must be fetched from the agent on demand and must not be stored on the server.
- **FR-120 [P3]:** The Compose tab must show the stack's `.env` file from its working directory (if present) as a key list. Values must be redacted under the environment rule in Section 8.6. Values under `environment:` keys in the compose files that match the sensitive-key rule, and any URL credentials, must also be redacted before they leave the host.
- **FR-121 [P3]:** The stack Logs tab must show merged logs from the stack's containers, in both modes:
  - **History:** the stored lines of every container of the stack within the chosen range, merged in timestamp order. This includes containers that are not currently running, while their lines are retained.
  - **Live:** turning Live on appends the merged live tail of the stack's running containers, applying the boundary rules in Section 8.12 per container.

  Each line must carry its service name as a text prefix, in a colour assigned to that service. The colour must stay the same for the whole page session and in both modes. Per-service toggles must show or hide each service. All controls from FR-058 and FR-076 to FR-085 must apply. A merged view must include at most the merged-stream bound from Section 8.8; above it, the operator must pick which services to include.
- **FR-122 [P3]:** The stack Activity tab must list the stack's own commands, plus the commands and events of its member containers, with the same entry content as FR-092 to FR-096.
- **FR-123 [P3]:** A compose file must be returned only when all of these hold:
  - it is declared for that stack by that host;
  - it lies inside the stack's working directory once symbolic links are resolved;
  - it has a `.yml`, `.yaml` or `.json` extension;
  - it is no larger than the bound in Section 8.8;
  - it is not inside the agent's own configuration or state directories.

  Any other request must return a specific reason (for example "outside stack directory", "too large", "not readable by the agent", or "missing") and no content.
- **FR-124 [P3]:** The stack cell in the containers table, and the stack in the container header, must link to the stack page.
- **FR-125 [P3]:** On a host whose agent does not support a stack action, that action must be disabled with the tooltip "Requires agent update". Update must stay available wherever it works today.
- **FR-126 [P3]:** The Compose tab must show no edit control, and the read-only state must be stated in the tab (S-4).
- **FR-127 [P3]:** Overview of a compose container must show its stack and service, linked to the stack page.

### 5.13 Inventory: images, volumes, networks [P4]

- **FR-128 [P4]:** A new opt-in collector, provisionally named `docker_inventory` (the architect may rename it), must report the following for each host:

  | Item | Fields |
  |---|---|
  | Images | ID, repository tags, repository digests, size, created time, number of containers in any state that reference it, dangling flag |
  | Volumes | name, driver, mountpoint, created time, compose project label, anonymous flag, referencing containers, size when available |
  | Networks | ID, name, driver, scope, internal flag, subnets and gateways, attached containers, built-in flag |
  | Disk usage | total and reclaimable size for images, volumes and build cache |

- **FR-129 [P4]:** The collector must run only on hosts whose collector whitelist includes it. It must run no more often than the inventory interval bound in Section 8.8. Each inventory page must offer "Refresh inventory" for a host, which requests one immediate collection.
- **FR-130 [P4]:** For each host and item type, the collector must report at most the item-count bound in Section 8.8. When it hits the bound, it must set a truncation flag, and the page must say "Showing first N items on <host>".
- **FR-131 [P4]:** The server must keep only the latest inventory for each host, and no history (NFR-003).
- **FR-132 [P4]:** Docker > Images must show a per-host disk usage summary and a table with these columns: host, `repository:tag` (or "<none>"), short ID, size, created, "Used by N containers" (linked to the containers table filtered to that image), and a status of In use, Unused or Dangling. It must provide search, the chips In use, Unused and Dangling with counts, a host filter, sorting, and URL state.
- **FR-133 [P4]:** Docker > Volumes must show a table with these columns: name, host, driver, stack, Named or Anonymous, used by, size ("—" when unavailable), and created. It must provide search, the chips In use, Unused and Anonymous with counts, a host filter, sorting, and URL state.
- **FR-134 [P4]:** Docker > Networks must show a table with these columns: name, host, driver, scope, subnet, attached containers count, and a Built-in flag. It must provide search, the chips In use, Unused and Built-in with counts, a host filter, sorting, and URL state.
- **FR-135 [P4]:** Each inventory page must show "Inventory as of <time>" for each host, and must show a "Stale" marker when the inventory is older than twice the inventory interval.
- **FR-136 [P4]:** When a host does not have the inventory collector enabled, each inventory page must say so for that host, naming the collector that needs to be added to that host's agent configuration. It must not imply that the host has zero items.

### 5.14 Prune and remove [P4]

- **FR-137 [P4]:** For each host, the system must offer:
  - "Prune images": dangling only (the default), or all unused;
  - "Prune volumes";
  - "Prune networks";
  - removal of individually selected unused items from any inventory table.
- **FR-138 [P4]:** Every prune or remove must first show a preview that lists each item to be removed, the item count, the estimated reclaimable size, and the inventory age. When the inventory is older than 15 minutes, the operator must refresh it before the confirm button becomes enabled.
- **FR-139 [P4]:** In a volume preview, unused anonymous volumes must start selected and unused named volumes must start unselected (D-5). The operator may change any selection before confirming.
- **FR-140 [P4]:** Built-in networks (`bridge`, `host`, `none`, and any swarm ingress network) must never appear in a prune preview and must never be removable.
- **FR-141 [P4]:** Volume removal must always require the operator to type the host name. Image or network removal must require the operator to type the item count when the count is greater than 20.
- **FR-142 [P4]:** Execution must remove only the items that were listed and confirmed, and only those still unused at execution time. Items found in use must be skipped and reported as "Skipped: in use". No other item may be removed (BR-008).
- **FR-143 [P4]:** The result must list each item's outcome (Removed, Skipped with reason, or Failed with message) and the total space reclaimed. It must be recorded as an Activity entry with step output.
- **FR-144 [P4]:** On a given host, a prune or remove must not start while any Docker command for that host is non-terminal. A Docker command for that host must not start while a prune or remove is non-terminal. Either conflict must be reported as "Already in progress" with a link to the blocking command (BR-009).
- **FR-145 [P4]:** No prune or remove may delete a volume as a side effect of removing an image, network or container, or of a stack Down.
- **FR-146 [P4]:** When a host's agent does not support prune, the prune controls for that host must be disabled with the tooltip "Requires agent update".
- **FR-147 [P4]:** When the targets of a container Stop (single or bulk), a stack Down, or a stack Redeploy or Restart include the container or stack that runs the rIOt server or its database, as far as rIOt can determine, the confirmation must warn that the dashboard will become unavailable and that rIOt may not be able to reverse the action. The operator must type the host name to proceed (see OQ-01).

### 5.15 Stored-log rotation [P0]

- **FR-148 [P0]:** Stored container log lines must be kept in whole-day partitions (UTC days) by line timestamp. Retention must remove data only by removing whole days. It must never delete individual stored log lines. This reuses the PART-001 daily-partition pattern for `container_logs`; it must not introduce a second, different partitioning scheme for that table (NFR-024).
- **FR-149 [P0]:** The container-log retention period must be a dedicated server setting, in whole days, separate from the general retention setting (`RIOT_RETENTION_DAYS`). It must default to 7 days, so that behaviour does not change when the setting is absent (D-34). Accepted values are 1 to 90. When the value is missing, the default applies. When it is non-numeric or outside the range, the server must use the default and write a warning log entry at startup naming the setting and the value used.
- **FR-150 [P0]:** A day must be removed only when every line in it is older than the retention period. At any time the stored history must contain at least the last *N* full days plus the current day, where *N* is the retention period.
- **FR-151 [P0]:** After the server starts with a changed retention value, the next retention run must apply it. Lowering the value must remove the days now outside the period. Raising the value must not restore days that were already removed. The effective value must be readable by the dashboard (FR-161).
- **FR-152 [P0]:** For each container in each collection, the agent must store at most a line cap and a byte cap (defaults per D-33, configurable per agent). When a container produced more than the caps in an interval, the agent must keep the newest lines that fit and record a gap marker stating how many lines were not stored for that container and interval.
- **FR-153 [P0]:** The server must enforce a hard per-container ceiling on each ingest (Section 8.8), whatever the agent's configuration or version. It must drop the excess, store a gap marker with the count dropped, and still accept the rest of the telemetry without error.
- **FR-154 [P0]:** When a container stopped since the previous collection, the next collection must store the lines it wrote between the previous collection and its stop, within the caps of FR-152. Lines written by a crashing container just before it exits must therefore reach the stored history.
- **FR-155 [P0]:** For containers that run with a TTY, stored lines must contain the container's output text exactly, with no stream-header bytes, and must be recorded as stdout.
- **FR-156 [P0]:** The move to whole-day rotation must not lose any stored line younger than the retention period at conversion time, and the existing stored-log API must keep returning lines throughout the conversion. Ingest must continue during and after the conversion.
- **FR-166 [P0]:** At ingest, the server must check the agent-reported time of every stored log line and every gap report against the accepted window: from the receive time minus the effective retention period, to the receive time plus 5 minutes. A line or gap report outside that window must be stored with the server's receive time as its time. That time decides which day the line belongs to and when retention removes it (S-8, Section 8.13).
- **FR-167 [P0]:** A line re-stamped under FR-166 must keep its agent-reported time, and must carry a clock-skew flag.
- **FR-168 [P0]:** No stored log line may be discarded because of its timestamp. A line whose reported time is older than the retention period must be re-stamped and flagged under FR-166 and FR-167, not dropped. The caps and ceilings of FR-152, FR-153 and FR-172 still apply as usual.
- **FR-169 [P0]:** No stored log row may be placed where retention never removes it. If any stored log rows are ever found in such a place, the server must raise an operator-visible warning that states how many rows are affected. This is defence in depth: FR-166 means it should never happen.
- **FR-170 [P0]:** The server must count re-stamped lines per device. When a device sends re-stamped lines in 3 consecutive ingests, the server must raise one operator-visible "clock skew" event for that host, stating the approximate size of the skew. It must not raise a second event while the first is active, and must clear the event after 1 hour of ingests with no re-stamped lines from that host (Section 8.8).
- **FR-171 [P0]:** Only the server may create gap markers, and only from gap reports it has validated.
  - A stored log line must have the stream `stdout` or `stderr`. A line with any other stream value must be dropped and counted. It must never be stored or shown as a gap marker.
  - A gap report is accepted only when it names a container that meets the container-name rules, carries a whole count from 1 up to the gap-count bound (Section 8.8), and is the only gap report for that container in that ingest. Any other gap report must be dropped and counted.
  - The history view must tell agent-reported gaps from server-ceiling gaps, and only the server may produce a server-ceiling gap.
- **FR-172 [P0]:** In addition to the per-container ceiling of FR-153, the server must enforce a per-device ceiling over a rolling minute (Section 8.8), so that an agent cannot multiply its allowance by inventing container names. Lines for container names that are not in the device's latest data must count against a separate per-device unknown-name budget (Section 8.8). Excess lines must be dropped. Excess lines for known containers must be recorded in a server-ceiling gap marker; excess lines for unknown names have no container to mark, so they must be counted per device instead.
- **FR-173 [P0]:** From P0 onward, every stored log line must be masked under Section 8.7 at ingest, before it is stored, whatever the agent version (D-3). Masking must run after the caps and ceilings, so that dropped lines are never processed. FR-154 and FR-155, which newly store crash output and TTY output, must not be released before this requirement.

### 5.16 Stored-log history view [P1]

- **FR-157 [P1]:** The history view must provide a time range with the presets 15 minutes, 1 hour (the default), 6 hours, 24 hours and 7 days, and a custom range with a start and end. A range that starts before the oldest retained day must be limited to the oldest retained day, and the picker must say so.
- **FR-158 [P1]:** The history view must load the newest stored lines in the chosen range, up to the history page bound (Section 8.8). When more lines exist in the range, a "Load older lines" control must load the next older page, until the start of the range is reached. When the range holds no stored lines, the view must say "No stored lines in this range". That state must be distinct from the "collector not enabled" state of FR-058 and from a fetch error.
- **FR-159 [P1]:** Gap markers recorded under FR-152 and FR-153 must appear inline, at their position in time, as "N lines not stored (per-container limit)".
- **FR-160 [P1]:** While Live is off and the range ends at the present, the history view must refresh at least every 15 seconds. It must append only stored lines newer than the last line shown, under the identity rule in Section 8.12. The view must show "History up to <time of newest stored line>. New lines appear within about one collection interval." Once P2 ships, the view must add "or turn on Live".
- **FR-161 [P1]:** The history view must state the effective retention, for example "History is kept for 7 days". The value must come from the server's effective setting (FR-151), not from a value built into the dashboard.
- **FR-174 [P1]:** Each line re-stamped under FR-166 must show a clock-skew marker. The marker must state, as text, both the time the server received the line and the time the agent reported. The view must place and order the line by its receive time. The marker must not rely on colour alone.

### 5.17 Live and history together [P2]

- **FR-162 [P2]:** Turning Live on must start the live tail at the boundary: the timestamp of the newest line currently shown for each container (Section 8.12).
  - Lines at or before the boundary that are already shown must not be shown again.
  - Lines after the boundary that are not yet stored must be shown once, which closes the collection lag.
  - When more than the live backfill bound of lines lie between the boundary and the present, the oldest of them must be skipped, and a gap marker must state how many.
  - When no history line is shown (for example, an empty range), the live tail must start with the last 100 lines.
- **FR-163 [P2]:** While Live is on, the history refresh of FR-160 must pause. Lines that the collector stores while Live is on must not be added to the view, because they would repeat the live tail.
- **FR-164 [P2]:** The Live toggle must not be restored from a URL or from a previous visit. Opening the Logs tab, from a link or otherwise, must always show the history with Live off, and must not start any agent stream.
- **FR-165 [P2]:** When several viewers show the live tail of the same container on the same host at the same time, the agent must hold at most one live log read for that container. Every viewer must receive the shared stream.
  - Each viewer's own boundary backfill (FR-162) may be served separately.
  - A viewer leaving must not end the stream for the other viewers.
  - The agent's read for that container must stop within 5 seconds after the last viewer leaves.

  This is the default of D-36.
- **FR-175 [P2]:** A live line whose reported time lies outside the accepted window of FR-166, measured against the server's receive time, must be placed at its receive time and must show the clock-skew marker of FR-174. No line, stored or live, may cause later lines to be rejected or hidden because it carries a future time.

## 6. Non-Functional Requirements

- **NFR-001 [Dependency gate]:** P1 to P4 implementation must not start until CMD-LIFECYCLE-001 and UI-FOUNDATION-001 are both merged to `main`. P0 is exempt from this gate (BR-001). Every action and progress display in this story must use the CMD-LIFECYCLE-001 states, steps, 409 conflict behaviour and per-target locks. No phase may reimplement them.
- **NFR-002 [UI foundation]:** All new and touched Docker UI must use the UI-FOUNDATION-001 primitives and services: Button, Card, Badge with the shared enum-to-label map, Modal, ConfirmDialog, EmptyState, Skeleton, QueryState, toasts, the formatting module, and `<RelativeTime>`. It must use design tokens instead of raw palette classes or hex colours. It must not use native browser dialogs. Raw enum values must never be shown.
- **NFR-003 [Database growth]:** This story must not add any table that is high-churn and retained by row DELETE, and it must remove the row-DELETE retention of stored container logs.
  - Stored container logs must be rotated by whole days only (FR-148), within the per-container caps (FR-152, FR-153).
  - Live log lines must never be persisted.
  - Inspect, compose and env content must never be persisted.
  - Inventory must be kept as latest-only per host.
  - Known stacks must take at most one record per host and project.
  - Activity step output must be bounded per command (Section 8.8) and retained for a bounded period (D-14).
  - Any new per-interval value (FR-062) must live in storage that is already bounded or partitioned.

  The ADD must state, for each new or changed stored element, the expected growth per host per day and how it is bounded. For stored container logs it must state the worst case per host per day under the default caps.
- **NFR-004 [Performance: table]:** With 2,000 containers loaded in a production build on a mid-range laptop:
  - the Containers table must render within 1 s of its data arriving;
  - search, chip, filter and sort changes must be reflected within 200 ms;
  - a live telemetry update must not block input for more than 100 ms.
- **NFR-005 [Performance: palette]:** The palette must open within 100 ms. With 5,000 indexed resources, it must update results within 100 ms of a keystroke.
- **NFR-006 [Performance: live logs]:** On a LAN, a line written by a container must appear in the browser within 2 s at p95 while Live is on.
- **NFR-007 [Agent impact]:** With live log sessions running at the line-rate bound, command and result messages on the same agent connection must not be delayed by more than 1 s at p95. One inventory collection must finish within 30 s, and two collections must never run at the same time on one agent.
- **NFR-008 [Session limits]:** Live log streams and viewers must be limited to the bounds in Section 8.8. Inspect, compose and env reads must be limited to 30 requests per minute per host. A request over the limit must get a clear "Try again shortly" message.
- **NFR-009 [Security: authorization]:** Every new endpoint and stream must require the same admin authentication as the existing terminal relay, and must enforce the same browser-origin check for WebSockets. Before opening a live log session, inspect request, or compose/env read, the server must confirm that the named container or stack exists on the named host in its latest data. An agent must serve these requests only for containers and stacks on its own host. A viewer's attachment to a live stream must be bound to the browser connection that opened it, and must end when that connection closes. A live log connection must close when the sign-in session that opened it expires, must refuse new attachments after that time, and must not stay open for more than 4 hours. The browser then reconnects through a fresh connection that checks the sign-in again. Stored history reads must use the same admin authentication.
- **NFR-010 [Security: redaction]:** Redaction of inspect data, environment variables, labels, compose files and `.env` files must happen on the host before transmission. No API, stream or download may return an unredacted value covered by Section 8.6. Browser-side masking may remain only as defence in depth.
- **NFR-011 [Security: audit]:** The system must record the actor, host, target, time and remote address for each of these:
  - each live log session (start and end), Inspect view, and compose/env view;
  - each container or stack action, prune and remove;
  - every operator-created command in the long class, including non-Docker commands (reboot, shutdown, OS update, agent update, agent uninstall, privilege sync);
  - each device deletion.

  Retention is by class (S-5):
  - **Long class:** terminal sessions (including the host shell), destructive or privileged commands, stack path changes, and device deletions. Kept for 180 days by default.
  - **Short class:** reads (live log sessions, Inspect and compose/env views) and other commands. Kept for 90 days by default, and never longer than the long class.
  - If the retention is made configurable, the short class must still never exceed the long class.

  Open sessions must never be purged. Audit records must survive device deletion. Terminal audit records are kept forever today, so the first retention run after the upgrade deletes those older than 180 days. The release notes must say so.
- **NFR-012 [Security: destructive actions]:** Every destructive action, as defined in BR-006, must have:
  - a danger confirmation that lists every target and every collateral effect (network dependents, the rIOt server itself);
  - typed confirmation, at the thresholds set in FR-041, FR-114, FR-141 and FR-147;
  - a per-target lock (NFR-001).

  A URL alone must never start an action: every action requires an explicit operator confirmation or activation inside the page.
- **NFR-013 [Security: file reads]:** The agent must refuse any compose or env read that falls outside FR-123 or FR-120. It must never return the agent's configuration, key, certificate or state files, whatever a container label declares.
- **NFR-014 [Privilege]:** New collection, actions and file reads must work with the agent's existing access: docker socket group membership and plain file reads as the agent user. No new sudoers rule may be added. If AGENT-PRIV is merged at implementation time, the ADD must state, for each new agent capability, whether it maps to an AGENT-PRIV manifest capability. The expected answer is none.
- **NFR-015 [Collector whitelist]:** The P4 collector must be opt-in through `collector.enabled`. Its name must be documented in the README, checked by the agent `doctor` command (whether it is enabled and whether the docker socket is reachable), and announced in the release notes with the exact line operators must add to each agent's configuration. Hosts without it must degrade as FR-136 describes. P0 adds no collector name; the existing `container_logs` collector keeps its name.
- **NFR-016 [Compatibility]:** Each combination must behave as follows:
  - **New server with an older agent:** each missing capability must degrade to an explicit "Requires agent update" state, never to an error or an empty view. Stored logs from older agents are still bounded by the server ceiling (FR-153).
  - **New agent with an older server:** the new fields must be ignored, without errors.
  - **Agent configuration:** P0 to P3 must require no agent configuration change. The P0 caps apply by default and may be tuned per agent.
- **NFR-017 [Accessibility]:** The new pages must meet these keyboard and screen-reader requirements:
  - **Containers and inventory tables:** reachable with Tab; arrow keys move between rows; Space toggles the row checkbox; Enter opens the row's resource.
  - **Tabs:** follow the WAI-ARIA tabs pattern (arrow keys, Home, End).
  - **⋯ menus:** follow the menu-button pattern (Enter, Space or Down opens; arrow keys move; Escape closes and returns focus).
  - **Palette:** follows the combobox and listbox pattern.
  - **Bulk bar, progress panel, and the Logs controls (range, Live toggle, Pause, filter):** reachable and operable by keyboard. The Live toggle exposes its on/off state to assistive technology.
  - **Status:** never conveyed by colour alone.
  - **Automated checks:** every new page must have zero serious or critical violations in an automated accessibility scan.
- **NFR-018 [Demo mode]:** The demo build must render every shipped Docker page with simulated data, including simulated stored history and a simulated live log stream. Simulated actions must not make any network call.
- **NFR-019 [Dependencies]:** No new Go or npm dependency may be added unless the ADD justifies it and the security review approves it.
- **NFR-020 [Testability]:** Automated tests must cover:
  - chip semantics and counts;
  - the URL state round trip;
  - the primary-action rules;
  - bulk applicability;
  - the redaction rules (on the agent);
  - the log masking rules;
  - the compose file-read guard;
  - the prune "only listed and still unused" guard;
  - the legacy URL redirects;
  - whole-day log rotation, including that no row-level delete runs against stored logs;
  - the per-container caps, the server ceiling and the per-device ceiling;
  - the history/live boundary and identity rules (Section 8.12);
  - the timestamp window, re-stamping and the clock-skew flag (FR-166 to FR-170);
  - stream and gap-report validation (FR-171);
  - masking at ingest after the caps (FR-173);
  - audit retention by class (NFR-011);
  - migration safety (NFR-028).

  Rules that depend on time must use an injected clock. The existing test counts must not go down.
- **NFR-021 [Public repository]:** Documentation, fixtures, demo data and test data must use placeholders only (for example `host-a`, `192.0.2.10`, `/srv/stacks/<name>`, `example.internal`). They must not contain real hostnames, LAN addresses, home paths, domains or email addresses.
- **NFR-022 [Observability]:** The server must write a structured log entry each time:
  - a live log session or viewer opens, closes, or hits a limit;
  - an on-demand read is rejected;
  - a prune runs;
  - a redaction step fails. In that case the request must fail closed and return no content;
  - a log retention run removes days (days removed, oldest day kept);
  - the server ceiling of FR-153 or the per-device ceiling of FR-172 drops lines (device, container count, lines dropped; never the line content);
  - an ingest re-stamps lines under FR-166 (device, count, largest skew; never the line content);
  - an ingest drops lines or gap reports under FR-171 (device, count, reason).
- **NFR-023 [Layout]:** At 1280 px viewport width and above, the default Containers columns must fit without horizontal scrolling. Below 768 px, the table must scroll horizontally and must not drop columns silently.
- **NFR-024 [Dependency: PART-001 for P0]:** FR-148, FR-150, FR-151 and FR-156, and the partition-dependent part of FR-149, must be delivered through PART-001. As settled by the product owner (S-7), PART-001 is merged **whole**, covering all six of its tables. Its migrations are renumbered above `000023_command_lifecycle`, keeping their internal order. This merge is P0's first step, under PART-001's own ADD and security-review conditions and the migration-safety rules of NFR-028. FR-166 to FR-173 must be in place before `container_logs` rows are routed to partitions. P0 must not ship a second, competing partitioning of `container_logs`. FR-152 and FR-153 do not depend on PART-001 and may ship first. FR-154 and FR-155 must not be released before FR-173. Until the partitioned rotation is in place, the existing 7-day purge stays unchanged, and no new row-DELETE path may be added.
- **NFR-025 [Database growth: stored logs]:** After P0, retention of stored container logs must create no dead rows. Storage must stay within retention days × the per-host daily maximum implied by the caps, plus any partitions created ahead of time. The per-device ceiling (FR-172) must hold that maximum whatever container names an agent reports. No stored log row may be placed where retention never removes it (FR-166, FR-169). The conversion must meet PART-001's own conversion NFRs, including no long blocking of ingest.
- **NFR-026 [Freshness: stored history]:** For a container within its caps, the newest stored line must lag the container's output by no more than one collection interval plus 10 seconds.
- **NFR-027 [Agent load: shared streams]:** Any number of viewers of the same container's live tail must cost the agent the same continuous Docker log reads as one viewer. The only extra cost is each viewer's one-time boundary backfill.
- **NFR-028 [Operational safety: database migrations]:** Merging PART-001 and renumbering its migrations must never put a deployed database at risk (S-7, S-9).
  - No step that rewrites a database's recorded migration version (`migrate force`) may run on a deployed database without the operator's sign-off for that specific database.
  - Before any such step, a verified backup of that database must exist: `pg_dump` output that has been restore-tested or, at minimum, for which `pg_restore --list` succeeds.
  - The database's state must be identified by inspecting its schema (which tables, columns and partitions exist), not by the version number alone.
  - A database whose migration state is dirty must stop the procedure for a human decision.
  - The procedure must never drop or truncate a table that holds telemetry, logs, commands or audit data.
  - The renumbered PART-001 migrations must do nothing, and change no data, on a database that PART-001 has already converted.
  - The state observed, the action taken and the version afterwards must be recorded for each database.

## 7. Business Rules

- **BR-001:** No P1 to P4 implementation starts before CMD-LIFECYCLE-001 and UI-FOUNDATION-001 are on `main`. P0 (stored-log rotation) is not gated by them, because it fixes an existing bloat risk and has no dashboard dependency.
- **BR-002:** A container is identified by host plus container name (consistent with CMD-LIFECYCLE-001 BR-001). IDs change on recreate. Names do not.
- **BR-003:** A stack is identified by host plus compose project name. The same project name on two hosts is two stacks.
- **BR-004:** rIOt never shows, streams or downloads an unredacted value covered by Section 8.6, and it offers no "reveal" (D-2).
- **BR-005:** Stored log history comes only from the `container_logs` collector, rotated per BR-017. Live logs are relayed and never stored; turning Live on adds nothing to storage.
- **BR-006:** These actions are destructive:
  - container Stop;
  - stack Down;
  - any prune or remove of images, volumes or networks;
  - container Remove, if D-4 is accepted.
- **BR-007:** Volumes hold data. Volume removal always requires typed confirmation, and no other action ever removes a volume.
- **BR-008:** A prune removes only items that were shown, confirmed, and still unused at execution time.
- **BR-009:** At most one non-terminal command per target (CMD-LIFECYCLE-001 BR-008). On a host, a prune or remove never overlaps any other Docker command.
- **BR-010:** Compose files are read-only in rIOt. rIOt never writes compose or `.env` files (S-4).
- **BR-011:** Every activity entry has exactly one actor category: Operator identity, System, Agent (observed), or Unknown (before activity tracking).
- **BR-012:** A selection never contains rows hidden by the current filters, so a bulk action only affects containers the operator can see.
- **BR-013:** There is a single role. This story adds and removes no permission.
- **BR-014:** Existing container URLs keep working, through redirects.
- **BR-015:** A stack rIOt has seen stays known while it is down, until the operator forgets it.
- **BR-016:** Every resource page shows at most one primary action.
- **BR-017:** Stored container logs rotate by whole days. A day is kept until all of it is older than the retention period (default 7 days), and is then removed in one piece.
- **BR-018:** The fleet-wide Docker section is the only Docker UI. A device page links into it filtered to that host and has no Docker page of its own.
- **BR-019:** In the Logs view, a log line is shown at most once, whether it arrived from stored history or from the live tail (Section 8.12).

## 8. Data Requirements

### 8.1 Entities

| Entity | New or changed | Key fields | Storage rule |
|---|---|---|---|
| Container (telemetry) | Changed, additive | Adds start time, finish time, exit code, running image ID, and cumulative network rx/tx and block read/write counters | Rides the existing telemetry. No new table. |
| Fleet container row | Changed, additive | Fields listed in FR-064 | Derived from latest telemetry |
| Stored container log line (`container_logs`) | Changed | Existing fields (device, container ID and name, timestamp, stream, line), plus gap markers (container, interval, count of lines not stored, agent or server origin), plus the agent-reported time and a clock-skew flag for re-stamped lines (FR-166, FR-167) | Whole-day partitions by the stored time, which is the receive time for re-stamped lines. Retention by removing whole days (default 7, range 1–90). Per-container caps per collection, a per-container and a per-device server ceiling (FR-152, FR-153, FR-172). Stream limited to `stdout` and `stderr` (FR-171). Masked at ingest (FR-173). |
| Command (CMD-LIFECYCLE-001) | Changed, additive | Adds the actor category and actor identity. Adds step records (name, start, end, outcome, output text) within the Section 8.8 bounds. Adds the new action names: stack Pull, Redeploy, Restart, Down and Up; image, volume and network prune and remove. | Existing command storage, bounded per NFR-003 |
| Known stack | New | host, compose project, working directory, declared compose file paths, last-seen time, forgotten flag | One record per host and project, updated in place |
| Live log stream | New, transient | stream ID, host, container, list of attached viewers (browser connection, boundary, streams, state) | Memory only, never persisted. One stream per container per host, shared by its viewers (FR-165). The audit record follows NFR-011. |
| Host inventory | New | Images, volumes, networks and disk usage as in FR-128; collected-at time; truncation flags | Latest per host only |
| Inspect document, compose file, env key list | New, transient | Redacted content, redaction count | Never persisted |
| UI state | New, client side | URL query (filters, sort, view, selection, tab, log time range); local preferences for view mode and recent resources. The Live toggle is never stored (FR-164). | URL and browser storage only |

### 8.2 Container column definitions

| Column | Content |
|---|---|
| Status | An icon for the state (running, paused, restarting, exited, created, dead, removing), overlaid with health when a health check exists (healthy, unhealthy, starting). The accessible text is, for example, "Running, unhealthy". |
| Name | The display name, with the container name as secondary text when it differs |
| Stack | The compose project, or "—" |
| Host | The hostname as reported |
| Image | `repository:tag`, plus the update badge (FR-017) |
| Ports | Section 8.4 |
| CPU | Percent with one decimal place, for running containers only |
| Memory | Bytes from the formatting module, plus the percentage of the limit when one is set. Running containers only. |
| Uptime / exit | running: "Up <duration since last start>"; exited or dead: "Exited (<code>) <relative time since finish>"; created: "Created"; paused: "Paused"; restarting: "Restarting". Fallback: Docker's status text. |

### 8.3 Filter chips

| Chip | A container matches when |
|---|---|
| Running | state is `running` |
| Stopped | state is `exited`, `created` or `dead` |
| Unhealthy | health is `unhealthy`, or state is `restarting` |
| Updates available | update status is `update_available` |

- Containers in state `paused` or `removing` match no chip. They are shown only when no chip is selected.
- A chip's count is the number of containers that match that chip **and** the current search, host and stack filters. The chip selection itself is ignored when counting.
- The same rule defines counts for the stack chips and the inventory chips.

### 8.4 Published port link rules

- A port binding becomes a link only when all of these hold:
  - the protocol is TCP;
  - a host port is published;
  - the host binding address is not a loopback address (`127.0.0.0/8` or `::1`).
- **Link host:** the binding address when it is a specific non-wildcard address. Otherwise it is the host's primary address as reported by its agent. IPv6 addresses are wrapped in brackets.
- **Scheme:** `https` when the container port is 443 or 8443, otherwise `http`.
- **Link text:** `<host port>→<container port>`.
- **Behaviour:** links open in a new browser tab, with no opener and no referrer.
- **Non-link ports:** loopback-bound and UDP ports are shown as plain text with the protocol suffix (for example `53/udp`).
- **Example:** `http://192.0.2.10:8080`.

### 8.5 Action rules

**Container primary action (the first matching row wins):**

| # | Condition | Primary action |
|---|---|---|
| 1 | The target has a non-terminal command | None. Shows the lifecycle label and step (FR-051). |
| 2 | State is `exited`, `created` or `dead` | Start |
| 3 | State is `running` and update status is `update_available` | Update |
| 4 | State is `running` and health is `unhealthy` | Restart |
| 5 | Anything else (running and healthy with no update, `paused`, `restarting`, `removing`) | None (D-16) |

**Container secondary actions and menu:**

| State | Secondary actions | ⋯ menu, normal group | ⋯ menu, destructive group |
|---|---|---|---|
| running | Restart, Check for updates, Logs, Terminal (when enabled) | Copy container ID, Copy image reference, Open source repository (when known), Auto-update on/off | Stop |
| exited, created, dead | Check for updates, Logs | The same as for running | none (Remove only if D-4 is accepted) |
| paused, restarting | Logs | The same as for running | Stop |

**Bulk applicability:**

| Action | Applies to containers in state |
|---|---|
| Start | `exited`, `created`, `dead` |
| Stop | `running`, `restarting`, `paused` |
| Restart | `running` |
| Update | Any state, with update status `update_available` |

A selected container that has a non-terminal command is skipped with the reason
"Already in progress".

**Stack primary action (the first matching row wins):**

| # | Condition | Primary action |
|---|---|---|
| 1 | The stack target has a non-terminal command | None. Shows progress. |
| 2 | No service is running, or the stack is known and Down | Up |
| 3 | Some services are running, but not all | Up |
| 4 | All services are running and at least one has an update available | Update |
| 5 | All services are running and at least one is unhealthy | Restart |
| 6 | Anything else | None |

Stack secondary actions are Pull, Redeploy, Restart, and Check for updates,
excluding whichever is currently primary. The ⋯ menu holds Auto-update on/off,
Copy working directory, and Forget stack (Down stacks only). Its destructive
group holds Down.

### 8.6 Redaction rules (inspect, labels, compose, env)

These rules are applied on the host (NFR-010). The security review may tighten
them. It must not loosen them without the product owner's decision (D-2).

1. **Environment variables, wherever they appear** (inspect `Config.Env`,
   `.env` files): every value is redacted except for keys on this safe list.
   `TZ`, `PUID`, `PGID`, `UID`, `GID`, `UMASK`, `LANG`, `LANGUAGE`, `LC_ALL`,
   `PATH`, `HOME`, `HOSTNAME`, `TERM`, `NODE_ENV`, `PYTHONUNBUFFERED`.
   **Exception:** a safe-list value that contains URL credentials is still
   redacted.
2. **Sensitive-key rule.** This applies to label values, compose
   `environment:` entries, and inspect fields other than `Config.Env`. A value
   is redacted when its key, compared case-insensitively, contains any of:
   `PASSWORD`, `PASSWD`, `PASS`, `PWD`, `SECRET`, `TOKEN`, `APIKEY`,
   `API_KEY`, `ACCESS_KEY`, `PRIVATE`, `CREDENTIAL`, `AUTH`, `COOKIE`,
   `SESSION`, `DSN`, `CONNECTION_STRING`, `SALT`, or `KEY` as a whole
   underscore- or dot-separated segment.
3. **URL credentials.** In any value, the password part of
   `scheme://user:password@host` is redacted. The username and host are kept.
4. **Command-line arguments** (`Config.Cmd`, `Config.Entrypoint`,
   `Args`): the value part of `--<flag>=<value>`, and the argument that follows
   `--<flag>`, are redacted when the flag name matches rule 2.
5. Each redacted value becomes `<redacted>`, and the key stays visible. The
   number of redactions is reported (FR-087).
6. If redaction cannot be applied (for example, the content cannot be
   parsed), no content is returned (NFR-022).

### 8.7 Log masking rules (stored history, live logs, Activity output)

Masking is best effort. The Logs tab must show a short notice saying that
known secret patterns are masked, but that logs may still contain sensitive
data. Matched spans are replaced with `<masked>`. Masking applies to:

- `Authorization: Bearer <token>` and `Authorization: Basic <value>`: the value is masked;
- URL credentials, per rule 3 in Section 8.6;
- `<key>=<value>`, `<key>: <value>` and `"<key>": "<value>"`, where the key matches rule 2 in Section 8.6: the value is masked;
- tokens made of three base64url segments, the first starting with `eyJ` (JWT form);
- PEM private key blocks, from `-----BEGIN ... PRIVATE KEY-----` to the matching END line.

### 8.8 Bounds

| Item | Bound |
|---|---|
| **Stored logs** | |
| Container-log retention | Default 7 days; accepted 1–90 days (FR-149, D-34) |
| Per-container cap per collection (agent) | Default 50 lines **and** 16 KiB, whichever is reached first; configurable per agent (FR-152, D-33) |
| Per-container ceiling per ingest (server) | 1,000 lines and 256 KiB, whatever the agent configuration (FR-153) |
| Per-device ceiling (server, rolling 60 s) | min(N, 20) × the per-container ceiling, where N is the number of containers in the device's latest data (minimum 1): at most 20,000 lines and 5 MiB per minute (FR-172) |
| Unknown-name budget (server, rolling 60 s) | 200 lines and 64 KiB per device, shared by all lines and gap reports for container names not in the device's latest data (FR-172) |
| Gap reports per ingest | At most one per container. Count is a whole number from 1 to 10,000,000; a count that is a lower bound is shown with "+" (FR-171) |
| Accepted timestamp window at ingest | From the receive time minus the effective retention, to the receive time plus 5 minutes. Lines outside it are re-stamped, never dropped (FR-166, FR-168) |
| Clock-skew event | Raised after re-stamped lines in 3 consecutive ingests; cleared after 1 hour of ingests without re-stamped lines (FR-170) |
| History page (one load or "Load older lines") | 5,000 lines |
| Stored-history freshness | One collection interval plus 10 s (NFR-026) |
| **Live logs and the view** | |
| Live backfill at the boundary | 5,000 lines |
| Lines held in one Logs view (history plus live) | 10,000 lines |
| Log line length | 16 KiB; longer lines are truncated |
| Line rate per live stream | 1,000 lines/s; excess is dropped at the source |
| Distinct live streams per host (one per container) | 8 |
| Viewers per shared stream | 8 |
| Live viewers server-wide | 64 |
| Containers per merged stack view | 25 |
| Hidden-tab stop delay | 5 minutes |
| Agent stops a shared stream after its last viewer leaves | Within 5 s |
| **Other** | |
| Inspect response | 1 MiB; 10 s timeout |
| Compose file size / files per stack | 1 MiB / 10 |
| `.env` file size | 256 KiB |
| On-demand reads (inspect, compose, env) | 30 per minute per host |
| Activity step output | 64 steps and 64 KiB in total per command |
| Activity step output retention | 30 days (D-14). The command summary follows the CMD-LIFECYCLE-001 retention. |
| Audit retention | Long class 180 days; short class 90 days, never longer than the long class (S-5, NFR-011) |
| Live log connection lifetime | Until the sign-in session expires, and 4 hours at most (NFR-009) |
| Inventory interval | At least 15 minutes between automatic collections |
| Inventory items per host | 2,000 images, 2,000 volumes, 500 networks |
| Bulk targets per action | 100 |
| Selection stored in the URL | 200 rows |
| Palette results / recent items | 50 / 8 |

### 8.9 State transitions: Logs view and live log session

**Logs view (one tab):**

| From | Event | To |
|---|---|---|
| (none) | Operator opens the Logs tab | History, Live off (refreshing per FR-160 when the range ends at the present) |
| History | Operator changes the range or loads older lines | History |
| History | Operator turns Live on (enabled per FR-074) | History + Live tail (the session table below starts at Connecting) |
| History + Live tail | Operator turns Live off | History, Live off. Every shown line is kept, and refresh resumes from the last shown line (FR-079). |
| History + Live tail | Operator changes the history range | History, Live off. The view reloads the new range. |
| Any | Operator leaves the tab or the page | Closed. The viewer detaches; the agent stream stops when it was the last viewer (FR-165). |

**Live session (while Live is on):**

| From | Event | To |
|---|---|---|
| (none) | Operator turns Live on | Connecting |
| Connecting | First line, or the agent confirms | Live |
| Connecting | Host offline, agent unsupported, or stream limit reached | Unavailable (the toggle returns to off with the reason) |
| Live | Connection drops unexpectedly | Reconnecting |
| Reconnecting | Reconnected (a gap marker is inserted; resumes from the boundary) | Live |
| Reconnecting | Host offline for more than 60 s | Unavailable |
| Live | Container stops | Ended |
| Ended | Container starts or is recreated with the same name | Live (a "Container restarted" marker is inserted) |
| Live | Tab hidden for more than 5 min | Stopped |
| Stopped | Tab becomes visible | Connecting (resumes from the boundary) |
| Any | Operator turns Live off, leaves the page, or the browser connection closes | Closed (this viewer detaches) |

Pause and Follow are view states only. They do not change the session state.

### 8.10 State transitions: stack and prune

**Stack state (derived):**

| State | Condition |
|---|---|
| Running | Every container of the stack is running |
| Partial | At least one container is running, and at least one is not |
| Down | The stack is known and has no running container, including when it has no containers at all |
| Forgotten | The operator chose Forget while the stack was Down. Hidden from the list. Returns to Running or Partial when its containers appear again. |

**Prune flow:**

| From | Event | To |
|---|---|---|
| Idle | Operator opens prune or remove | Preview loading |
| Preview loading | Inventory available | Preview ready, or Preview stale if older than 15 min |
| Preview stale | Refresh inventory completes | Preview ready |
| Preview ready | Operator changes the selection | Preview ready |
| Preview ready | Typed confirmation (if required) and Confirm | Submitted, as a lifecycle command |
| Submitted | Command reaches a terminal state | Results shown, recorded in Activity |
| Any before Submitted | Cancel | Idle (nothing sent) |

### 8.11 Stack actions

| Action | Effect on the host | Confirmation | Disruptive |
|---|---|---|---|
| Pull | Pulls the images for every service. Changes no container. | None | No |
| Update | Pulls, then recreates the services whose image changed. This is the existing stack update, with the CMD-LIFECYCLE-001 safety rules. | Existing update confirmation | Yes, for changed services |
| Redeploy | Recreates every service's container from the images already on the host, without pulling | Primary confirmation listing services and network dependents | Yes |
| Restart | Restarts the stack's running containers, without recreating them | Primary confirmation listing services and network dependents | Yes |
| Down | Stops and removes the stack's containers and the networks the stack created. Volumes and images are never removed. | Danger confirmation; the operator types the stack name | Yes (destructive) |
| Up | Creates and starts every service from the compose files currently on the host. Pulls only images that are missing. | None | No |

The CMD-LIFECYCLE-001 network-dependent restore rules (its FR-028 to FR-033)
apply to Update, Redeploy and Restart.

### 8.12 History/live boundary and line identity

1. **Line identity.** A line is identified by: container, the timestamp exactly
   as Docker reports it (full precision), stream, and the displayed text after
   masking (Section 8.7) and after line-length truncation. Stored and live lines
   both come with Docker's own timestamps, so the same output line has the same
   identity in both sources. For a re-stamped line (FR-166), the identity uses
   the agent-reported time kept with it (FR-167), while the line is placed and
   ordered by its receive time.
2. **Boundary.** For each container in the view, the boundary *B* is the
   timestamp of the newest line currently shown for that container.
3. **Turning Live on.** The live tail is requested from *B*.
   - A line older than *B* is dropped.
   - A line at *B* is dropped when a line with the same identity is already shown.
   - Every later line is appended once, in timestamp order.
   - When more than the live backfill bound (Section 8.8) lies after *B*, the oldest lines beyond the bound are skipped, and a gap marker gives their count.
   - With no line shown, the tail starts with the last 100 lines.
4. **While Live is on.** Newly stored lines are not merged into the view
   (FR-163).
5. **Turning Live off, and history refresh.** A stored line is appended only
   when it is newer than the newest shown line of its container, or at the same
   timestamp with an identity not already shown.
6. **Merged stack views.** Rules 2 to 5 apply per container, and the merged
   view is ordered by timestamp across containers.
7. **Known limitation.** Two genuinely different output lines with the same
   container, timestamp, stream and text are indistinguishable. Only one of
   them is shown at the boundary (R-13).

### 8.13 Out-of-window log timestamps: where they come from, and why nothing is lost

The product owner asked how a log line could carry a time outside the accepted
window (FR-166), and whether handling them could lose important log data.

**Where they come from.** These are the realistic sources:

| Source | What happens to the timestamp |
|---|---|
| Raspberry Pi and other single-board computers without a real-time clock | They boot at the epoch (1970), or at the time of the last shutdown, and stay there until NTP synchronises. Container output from early boot carries those times. |
| A dead CMOS/RTC battery on a PC or server | The same effect after every power loss: the clock starts at a default or stale date. |
| A buffered backlog | An agent that was offline buffers telemetry and sends it when it reconnects. Lines older than the oldest retained day arrive all at once. |
| A container with its own clock problem | A wrong time-zone setting, a faketime-style library, or a clock bug inside the container. Most of these only change the time printed inside the line text, which this rule leaves alone. They matter only when the reported time itself comes from the container. |
| A misbehaving or forged agent (the security case) | A compromised agent can send any time, for example far in the future, to keep rows from ever being rotated or to hide newer lines (SEC-021). |

**What happens to such lines.**

- They are kept. No line is ever dropped because of its time (FR-168).
- They are placed at the time the server received them, so they land in a day
  that retention rotates normally. They are kept for the full retention period
  counted from their arrival (FR-166).
- The time the device reported stays with the line, and the Logs view shows it
  beside the receive time, with a clock-skew marker (FR-167, FR-174, FR-175).
- The server counts them per device and raises one clock-skew event when the
  skew persists (FR-170).

**Answer: no log data is lost.** The only change is where the line sits on the
time axis: at the moment it arrived, with the device's claimed time shown next
to it. A line from a Raspberry Pi that booted believing it was 1970 appears at
the minute it was received, marked, with its reported time
(1970-01-01 00:00:12 UTC, for example) visible beside it. A backlog from a host
that was offline for two weeks is kept for a full retention period from its
arrival, instead of being thrown away as already expired.

**Why not keep the reported time, or drop the line?**

- Keeping the reported time would put the line where retention never removes it,
  which recreates the unbounded growth this story removes. A future-dated line
  could also hide every newer line of that container.
- Dropping the line would lose exactly the output operators need most: boot-time
  messages and output from after an outage.

The product owner chose re-stamping (S-8), in place of the security review's
recommendation to drop and count.

## 9. Acceptance Criteria

Unless an AC says otherwise, it is verified by an automated test (vitest for
web, Go tests for server and agent). Phase tags match the FRs.

### 9.1 Navigation and URLs [P1]

**AC-001 [P1] [FR-001, FR-003]**
Given: an authenticated operator on any page
When: they activate "Docker" in the main navigation
Then: Docker > Containers opens
And: the Containers sub-navigation entry has `aria-current="page"`.

**AC-002 [P1] [FR-002]**
Given: a build where only P1 has shipped
When: the Docker sub-navigation renders
Then: it contains Containers
And: it does not contain Stacks, Images, Volumes, Networks or Activity.

**AC-003 [P1] [FR-004, FR-005]**
Given: the fleet fetch succeeds with no Docker-capable host
When: Containers renders
Then: the empty state "no host reports Docker" is shown.
And Given: instead, the fleet fetch fails with HTTP 500
When: Containers renders
Then: the error state with Retry is shown and the empty state is not.

**AC-004 [P1] [FR-006, FR-028, BR-018]**
Given: device `<device-id>` exists
When: the browser opens `/devices/<device-id>/containers`
Then: the location becomes `/docker/containers?host=<device-id>`, showing the fleet view filtered to that host
And: pressing Back does not return to the legacy URL
And: switching to Tiles shows that host's stack, network-cluster and standalone groups inside the fleet page
And: no route renders a Docker container list scoped to a single device.

**AC-005 [P1] [FR-007]**
Given: container `web` with short ID `abc123` on `host-a`
When: the browser opens `/devices/<host-a-id>/containers/abc123`
Then: the canonical page for `web` on `host-a` is shown.
And Given: no container has short ID `zzz999`
When: the browser opens `/devices/<host-a-id>/containers/zzz999`
Then: "Container not found" is shown with a link to Containers filtered to `host-a`.

**AC-006 [P1] [FR-009, FR-010]**
Given: the canonical page for `web` on `host-a` is open
When: telemetry shows `web` recreated with a new ID
Then: the same URL keeps showing `web` without a "not found" state
And: opening that URL in a new browser tab shows the same page.

**AC-007 [P1] [FR-008]**
Given: a device page for `host-a` with ID `<host-a-id>`
When: the operator activates its Docker link
Then: the browser is at `/docker/containers?host=<host-a-id>`, filtered to `host-a`.

### 9.2 Containers table [P1]

**AC-008 [P1] [FR-011, FR-035]**
Given: `host-a` with 3 containers and `host-b` with 2, both Docker-capable
When: Containers renders with no filters
Then: 5 rows are shown
And: the footer reads "Showing 5 of 5 containers on 2 hosts".

**AC-009 [P1] [FR-012, FR-013, FR-014, FR-016]**
Given: a running container whose health is unhealthy, with display name "Web UI" and container name `web`, on `host-a`
When: its row renders
Then: the columns appear in the FR-012 order
And: the status cell has the accessible text "Running, unhealthy"
And: the name cell shows "Web UI" with `web` as secondary text and links to the canonical container page
And: the host cell shows `host-a` and links to its device page.

**AC-010 [P1] [FR-017]**
Given: three containers with update status `update_available` (reason `registry_newer`), `pinned`, and `check_failed`
When: their rows render
Then: only the first shows "Update available"
And: its tooltip states the reason.

**AC-011 [P1] [FR-018]**
Given: a container with ports `0.0.0.0:8080→80/tcp`, `127.0.0.1:9000→9000/tcp`, `53→53/udp`, and `8443→443/tcp`, on a host whose primary address is `192.0.2.10`
When: its ports cell renders
Then: `8080→80` links to `http://192.0.2.10:8080`
And: `9000→9000` is plain text
And: `53/udp` is plain text
And: only the first 3 ports are shown, with a "+1" control that reveals `8443→443`, which links to `https://192.0.2.10:8443`
And: every link has `target="_blank"` and `rel` containing `noopener` and `noreferrer`.

**AC-012 [P1] [FR-019, FR-020]**
Given: a running container last started 2 h ago and created 10 days ago, using 256 MiB of a 1 GiB memory limit
When: its row renders
Then: the uptime cell reads "Up 2h 0m"
And: the memory cell shows 256 MiB and 25%.
And Given: an exited container with exit code 137
Then: its CPU and memory cells show "—", and its uptime cell reads "Exited (137)" followed by the relative finish time.
And Given: an agent that reports neither start time nor exit code, with Docker status "Up 3 hours"
Then: the uptime cell reads "Up 3 hours".

**AC-013 [P1] [FR-021]**
Given: containers named `web`, `db`, and `cache` (with image `redis:7`)
When: the operator types "REDIS"
Then: only `cache` is shown.

**AC-014 [P1] [FR-022, FR-024, Section 8.3]**
Given: 4 running containers (1 of them unhealthy), 2 exited, 1 restarting, 1 paused, and 2 with updates available
When: the page renders with no filters
Then: the chip counts are Running 4, Stopped 2, Unhealthy 2, Updates available 2.
And When: Stopped and Unhealthy are both selected
Then: exactly the 4 containers that are stopped, unhealthy or restarting are shown
And: the paused container is not shown.

**AC-015 [P1] [FR-022, Section 8.3]**
Given: the host filter is set to `host-a`, which has 1 running container, while the fleet has 4 running
When: the chips render
Then: Running shows 1.

**AC-016 [P1] [FR-015, FR-023]**
Given: stacks `media` on `host-a` and `infra` on `host-b`
When: the host filter is `host-a`
Then: the stack filter offers `media` and not `infra`.
And When: in P1, the operator activates the stack cell `media`
Then: the table is filtered to stack `media`.

**AC-017 [P1] [FR-025]**
Given: an unsorted fleet
When: the page renders
Then: rows are sorted by name ascending, and ties are ordered by hostname
And When: the operator activates the CPU header twice
Then: rows are sorted by CPU descending.

**AC-018 [P1] [FR-026, FR-027]**
Given: search "web", chips Running and Updates available, host `host-a`, sort CPU descending, view Table, and 2 selected rows
When: the URL is copied and opened in a new tab
Then: every one of these settings is restored.
And Given: a URL with `status=bogus` and a selection entry for a container that does not exist
Then: the page renders without an error, with the valid settings applied and that entry dropped.

**AC-019 [P1] [FR-026]**
Given: 250 selected rows
When: the URL updates
Then: it contains no selection parameter
And: the 250 rows stay selected on the page.

**AC-020 [P1] [FR-028, FR-029]**
Given: the operator switched to Tiles earlier in this browser
When: Containers opens from a URL with no view mode
Then: Tiles is shown, with the stack, network-cluster and standalone groups
And: no row checkboxes or bulk bar are present.

**AC-021 [P1] [FR-030]**
Given: the host filter is `host-a`
When: the operator activates "Check for updates"
Then: exactly one update re-check command is sent, to `host-a`
And: a toast reports the result for `host-a`.

**AC-022 [P1] [FR-031]**
Given: filters that match no row
When: the table renders
Then: "No containers match these filters" is shown
And When: the operator activates "Clear filters"
Then: all rows are shown and the URL has no filter parameters.

**AC-023 [P1] [FR-032]**
Given: `host-b` is offline and was last seen 12 minutes ago
When: its rows render
Then: each carries "Host offline" and a relative time of "12m ago".

**AC-024 [P1] [FR-033, FR-034]**
Given: 3 rows selected, keyboard focus on row 2, and sort by CPU
When: two telemetry updates that change CPU order arrive within 5 s
Then: selection and focus stay on the same containers
And: the row order changes at most once in that 5 s.

### 9.3 Bulk actions [P1]

**AC-025 [P1] [FR-036, FR-037, FR-038]**
Given: 10 visible rows
When: the operator checks row 2, then Shift-clicks row 5
Then: rows 2 to 5 are selected and the header checkbox is in the mixed state.
And When: a search hides rows 4 and 5
Then: only rows 2 and 3 stay selected.

**AC-026 [P1] [FR-039, Section 8.5]**
Given: a selection of 2 running containers (1 with an update), 1 exited container, and 1 container with a non-terminal command
When: the bulk bar renders
Then: it shows "4 selected", "Start (1)", "Stop (2)", "Restart (2)", "Update (1)"
And: the counts exclude the container whose command is in progress.

**AC-027 [P1] [FR-040]**
Given: a bulk Restart that includes a network parent with 1 dependent
When: the confirmation opens
Then: it lists the targets grouped by host, the skipped containers with reasons, and the dependent.

**AC-028 [P1] [FR-041]**
Given: a bulk Stop of 6 containers on one host
When: the confirmation opens
Then: it is the danger variant, and Confirm stays disabled until the operator types "6".
And Given: a bulk Stop of 2 containers on 2 hosts
Then: typed confirmation is also required.
And Given: a bulk Stop of 2 containers on 1 host
Then: no typed confirmation is required.

**AC-029 [P1] [FR-042, FR-043]**
Given: a bulk Update of `web` (which already has a command in flight) and `db`
When: the operator confirms
Then: only `db` gets a new command
And: `web` is shown as "Already in progress" with a link to the existing command
And: the progress panel follows `db` through its states until it is terminal
And: the summary toast reports 1 succeeded and 1 already in progress.

**AC-030 [P1] [FR-044]**
Given: 101 selected applicable containers
When: a bulk action's confirmation opens
Then: Confirm is disabled and the confirmation asks the operator to narrow the selection.

**AC-031 [P1] [FR-045]**
Given: a bulk Restart that includes a container on offline `host-b`
When: the confirmation opens
Then: it states that `host-b` commands will wait for reconnection and expire if the host does not reconnect in time.

### 9.4 Resource header and tabs [P1]

**AC-032 [P1] [FR-046, FR-047]**
Given: compose container `web` in stack `media` on `host-a`, with a health check
When: its page renders
Then: the breadcrumb reads Docker > Containers > host-a > web, and each ancestor is a link
And: the header shows a state badge, a health badge, the host link, the stack, the image with its tag, and the uptime.

**AC-033 [P1] [FR-048, FR-049, Section 8.5]**
Given a container in each condition below, its header shows this primary action, and the primary action does not appear again among the secondary actions:

| Condition | Primary action |
|---|---|
| A non-terminal command | none |
| exited | Start |
| running with an update | Update |
| running and unhealthy with no update | Restart |
| running, healthy, no update | none |
| paused | none |

This is a table-driven test.

**AC-034 [P1] [FR-050]**
Given: a running container
When: the ⋯ menu opens
Then: Stop is the last item, in a group after a divider, with danger styling.

**AC-035 [P1] [FR-051]**
Given: the container's target has a `queued` command
When: the header renders
Then: it shows "Queued" in place of the primary action, every action is disabled, and Cancel is visible.
And When: the command becomes `running` at step `pull`
Then: Cancel is hidden and the label shows the running state and the step.

**AC-036 [P1] [FR-052]**
Given: a running container
When: the operator activates Stop
Then: a danger ConfirmDialog opens, and no command is sent until it is confirmed.
And When: the operator activates Start on an exited container
Then: the command is sent with no dialog.

**AC-037 [P1] [FR-054, FR-055]**
Given: the `docker_terminal` feature is disabled and only P1 has shipped
When: the container page renders
Then: the tabs are Overview, Logs and Stats, in that order, with exactly one Logs tab.
And When: a URL that names the Stats tab is opened
Then: Stats is the selected tab.

**AC-038 [P1] [FR-056, FR-061]**
Given: a container with ports, networks, mounts, labels, alert rules, events, a repository URL and a description
When: Overview renders
Then: each of those appears, together with state, status, restart policy, restart count, health and image freshness
And: no Environment section is present.

**AC-039 [P1] [FR-057, FR-060]**
Given: 24 h of CPU and memory history, with 7d as the previously chosen range
When: Stats renders
Then: CPU and memory charts are shown with the 1h, 6h, 24h and 7d range options, and 7d is selected.

**AC-040 [P1] [FR-058, FR-059]**
Given: stored logs exist for the last 3 hours, including 4 stderr lines, and the container is exited
When: the Logs tab is opened
Then: the history for the last 1 hour is shown, with the range picker, text filter, stream toggle (both, stdout, stderr), wrap and timestamps
And: no 200, 500 or 1000 line-limit buttons are present
And When: the stream toggle is set to stderr
Then: only stderr lines are shown, each marked "ERR"
And When: the operator activates Download
Then: a file named `<host>_<container>_<UTC timestamp>.log` contains every loaded line for the selected streams.
And When: the Terminal tab is opened
Then: it shows "Container must be running to open a terminal."
And Given: a host without the `container_logs` collector
Then: the Logs tab says the collector is not enabled and names it.

### 9.5 Agent and fleet data [P1]

**AC-041 [P1] [FR-062, FR-063]**
Given: an exited container with exit code 1, and a running container with network and block I/O
When: the `docker` collector runs with the existing whitelist
Then: the payload includes start time, finish time, exit code and running image ID for the first, and the four I/O counters for the second
And: no new collector name is required.

**AC-042 [P1] [FR-064]**
Given: a container with environment values, mounts and a non-compose label `secret.label=x`
When: the fleet container data is fetched
Then: it contains state, health, ports, start time, update status and the compose project and service
And: it contains no environment, mounts, network attachments, or `secret.label`.

### 9.6 Command palette [P1]

**AC-043 [P1] [FR-065, FR-070]**
Given: focus on any page element
When: the operator presses Ctrl+K
Then: the palette opens with focus in its input.
And When: the operator presses Escape
Then: it closes and focus returns to the original element.

**AC-044 [P1] [FR-066]**
Given: focus inside a running Terminal
When: the operator presses Ctrl+K
Then: the palette does not open and the terminal receives the keystroke.

**AC-045 [P1] [FR-067, FR-068, FR-069]**
Given: a host `host-a`, containers `web` and `webhook` on `host-a`, and a stack `web-stack`
When: the operator types "web"
Then: results are grouped by type (hosts, containers, stacks)
And: within containers, `web` ranks before `webhook`.
And When: the operator types "host-a hook"
Then: `webhook` is the only container result.
And Given: 60 containers match
Then: 50 are shown with "10 more, refine your search".

**AC-046 [P1] [FR-070, FR-071, FR-073]**
Given: the operator visited container `db` and then stack `media`
When: the palette opens with an empty query
Then: `media` is listed before `db`.
And When: `db` disappears from the fleet data
Then: `db` is not listed.
And When: the operator presses Down and then Enter
Then: the browser navigates to the highlighted resource.

**AC-047 [P1] [FR-072]**
Given: container `nas-sync` on offline `host-b`
When: the operator searches "nas"
Then: `nas-sync` is listed with "Host offline".

### 9.7 Live logs [P2]

**AC-048 [P2] [FR-074]**
Given: a running container on an agent without live-log support, and a stopped container on a supporting agent
When: the Logs tab opens for each
Then: the stored history is shown in both cases
And: the Live toggle is disabled, with the reason "Live logs require an agent update" for the first and "Container is not running" for the second.

**AC-049 [P2] [FR-075, NFR-003]**
Given: a live session that relays 1,000 lines
When: the session ends
Then: the server's stored container log count is unchanged by the session.

**AC-050 [P2] [FR-076, FR-077]**
Given: Live turned on, with Follow on
When: the operator scrolls up
Then: Follow turns off.
And When: the operator activates Pause and 30 lines arrive
Then: the view is unchanged and "30 new lines" is shown.
And When: the operator activates Resume
Then: the 30 lines are shown and Follow is on.

**AC-051 [P2] [FR-078]**
Given: a view holding 60 history lines and 50 live lines (100 stdout and 10 stderr in total), 5 of which contain "timeout"
When: the text filter "TIMEOUT" is applied
Then: 5 lines are shown, highlighted, with a count of 5, drawn from both history and live lines.
And When: the stream toggle is set to stderr
Then: only stderr lines are shown, each marked "ERR".
And When: the stream toggle is set back to both and the operator activates Download
Then: the file is named `<host>_<container>_<UTC timestamp>.log` and contains all 110 masked lines, history first and then live.

**AC-052 [P2] [FR-162]**
Given: history whose newest shown line is at time *B*, and a container that wrote 8,000 lines after *B* that are not shown
When: Live is turned on
Then: the 5,000 newest of those lines are appended after a gap marker stating that 3,000 lines were skipped.

**AC-053 [P2] [FR-080]**
Given: 10,000 lines in the view
When: line 10,001 arrives
Then: the oldest line is removed and "Older lines were dropped from view" is shown.

**AC-054 [P2] [FR-081, Section 8.9]**
Given: Live on
When: the browser's connection to the server drops and comes back after 4 s
Then: the indicator shows Reconnecting and then Live
And: a gap marker with the gap time is inserted.

**AC-055 [P2] [FR-082]**
Given: Live on
When: the container exits with code 137 and then starts again
Then: "Container exited (code 137)" is shown, then "Container restarted", and the stream continues.

**AC-056 [P2] [FR-083]**
Given: Live on, and an injected clock
When: the tab is hidden for 5 min 1 s
Then: the state is Stopped and this viewer is detached from the agent stream.
And When: the tab becomes visible
Then: the stream resumes from the boundary of the last shown line, with a gap marker, and no shown line is repeated.

**AC-057 [P2] [FR-084]**
Given: a container emitting 3,000 lines in one second, including one 20 KiB line
When: they are streamed
Then: at most 1,000 lines are delivered for that second, followed by a "2,000 lines dropped" marker
And: the 20 KiB line is cut to 16 KiB with "[truncated]".

**AC-058 [P2] [FR-085, NFR-008]**
Given: live streams open for 8 different containers on `host-a`
When: Live is turned on for a ninth container on `host-a`
Then: the "Too many live log sessions on host-a" message is shown, the toggle returns to off, and the 8 existing streams continue.
And When: a second viewer turns Live on for one of the 8 containers already streaming
Then: it joins that stream without the message.

**AC-059 [P2] [FR-089, Section 8.7]**
Given: log lines containing `Authorization: Bearer abc.def`, `postgres://app:hunter2@db:5432`, `api_key=XYZ`, a JWT-form token, and a PEM private key block
When: they are shown from stored history, shown from the live tail, and included in a Download
Then: each secret part is replaced with `<masked>`
And: `app`, `db:5432` and the key names stay visible.

**AC-060 [P2] [NFR-009]**
Given: an authenticated browser
When: it requests a live session for a container ID that does not exist on the named host
Then: the server refuses it and no request is sent to any agent.
And Given: a session request with no admin authentication
Then: it is refused.
And Given: a session request from a disallowed origin
Then: it is refused.

**AC-061 [P2] [NFR-007]**
Given: 8 live sessions at 1,000 lines/s each on a test agent
When: a command is sent to that agent
Then: the agent acknowledges it within 1 s at p95, measured over 100 commands.

### 9.8 Inspect [P2]

**AC-062 [P2] [FR-086, FR-087, Section 8.6]**
Given: a container with environment `TZ=UTC`, `DB_PASSWORD=x`, `APP_MODE=prod`; label `traefik.http.middlewares.auth.basicauth.users=u:hash`; and command `--token=abc`
When: Inspect opens
Then: `TZ` shows `UTC`
And: `DB_PASSWORD`, `APP_MODE`, the label value and `abc` show `<redacted>`
And: "4 values redacted" is shown.

**AC-063 [P2] [NFR-010]**
Given: the agent's inspect response for the container in AC-062
When: it is captured before reaching the server
Then: it contains none of the values `x`, `prod`, `u:hash` or `abc`.

**AC-064 [P2] [FR-086, FR-088]**
Given: Inspect is open
When: the operator activates Refresh, Copy, and a search for "Mounts"
Then: a new agent request is made, the clipboard receives the redacted JSON, and "Mounts" is highlighted
And: no inspect content is stored on the server.

**AC-065 [P2] [FR-090, NFR-022]**
Given: an agent that does not answer
When: 10 s pass, measured with an injected clock
Then: an error with Retry is shown.
And Given: content that redaction cannot parse
Then: no content is returned, and a structured log entry is written.

### 9.9 Activity [P2]

**AC-066 [P2] [FR-091, FR-093]**
Given: container `web`, recreated twice, with:
- a Restart, a stack Update of its stack, and a bulk Stop that included it, all by OIDC user `operator-1`;
- an auto-update;
- a Docker `die` event that rIOt did not trigger;
- a command from before this feature

When: its Activity tab opens
Then: all six entries are listed
And: the three operator entries show `operator-1`, the auto-update shows "System (auto-update)", the `die` event shows "Agent (observed)", and the old command shows "Unknown (before activity tracking)".

**AC-067 [P2] [FR-092, FR-094]**
Given: a failed Update with the steps precheck, pull, and recreate (failed: "port already allocated")
When: the entry is expanded
Then: each step shows its name, start, duration and outcome
And: recreate shows the failure output.

**AC-068 [P2] [FR-094, Section 8.8]**
Given: a command whose step output totals 100 KiB
When: it is shown
Then: at most 64 KiB is stored and shown, and "Output truncated" appears.

**AC-069 [P2] [FR-095]**
Given: an expanded running entry
When: the step changes from pull to recreate
Then: the entry updates without a reload.

**AC-070 [P2] [FR-096, FR-097]**
Given: 500 fleet events newer than `web`'s only `die` event, and 30 command entries for `web`
When: `web`'s Activity opens
Then: the first page shows 25 entries, newest first
And: the `die` event can be reached on a later page, or by filtering to type Docker event.

**AC-071 [P2] [FR-098, FR-100]**
Given: entries on `host-a` and `host-b`, one of them for a container that has since been deleted
When: Docker > Activity is filtered to `host-a` and to actor "System"
Then: only `host-a` System entries are shown, the filters are stored in the URL, and the deleted container's entry links to `host-a`.

**AC-072 [P2] [FR-099]**
Given: step output containing `<img src=x onerror=alert(1)>` and ANSI colour codes
When: it is shown
Then: it renders as literal text with no element created, and the ANSI codes are removed.

### 9.10 Stats [P2]

**AC-073 [P2] [FR-101, FR-103, FR-106]**
Given: counter samples of rx 1,000 then 61,000 bytes, 60 s apart
When: Stats renders after the second sample arrives, without a reload
Then: network receive shows 1000 B/s.
And Given: counters that went down between samples
Then: that interval shows "—".

**AC-074 [P2] [FR-102]**
Given: an agent that does not report I/O counters
When: Stats renders
Then: the I/O figures show "—" with the tooltip "Requires agent update".

**AC-075 [P2] [FR-104, FR-105, FR-107]**
Given: a container with a 1 GiB memory limit, and PART-001 not merged
When: Stats renders
Then: the memory chart shows a reference line labelled 1 GiB
And: the time axis carries the preferred zone label
And: no network or block I/O history chart is present.

### 9.11 Stacks [P3]

**AC-076 [P3] [FR-108, FR-109, Section 8.10]**
Given: stacks `media` (3 of 3 running), `infra` (1 of 2 running), and a known `old` stack with no containers
When: Stacks renders
Then: their states are Running, Partial and Down, and the chip counts are Running 1, Partial 1, Down 1.

**AC-077 [P3] [BR-003]**
Given: project `media` on both `host-a` and `host-b`
When: Stacks renders
Then: two rows are shown, one per host.

**AC-078 [P3] [FR-110, FR-111]**
Given: stack `media`, where service `vpn` is the network parent of `torrent`
When: the Services tab renders
Then: one row is shown per container, with the Section 8.2 columns
And: `torrent` shows "Network via vpn"
And: each row links to its container page
And: the combined CPU and memory of the running containers is shown.

**AC-079 [P3] [FR-112, FR-114, Section 8.11]**
Given: stack `media` on `host-a`
When: the operator activates Pull
Then: a command is sent with no dialog, and no container ID changes.
And When: the operator activates Down
Then: a danger dialog requires the operator to type "media"
And: after the command completes, no volume and no image of the stack has been removed.

**AC-080 [P3] [FR-113]**
Given: stack `media` has an in-flight Update
When: the operator activates Restart
Then: no new command is created, and the page shows the existing Update's progress.

**AC-081 [P3] [FR-116, FR-117]**
Given: stack `media` after Down
When: Stacks renders
Then: `media` is listed as Down, with Up as its primary action.
And When: the operator chooses "Forget stack" and confirms
Then: `media` is no longer listed and nothing on `host-a` changes.
And When: containers of `media` appear again
Then: it is listed again.

**AC-082 [P3] [FR-118]**
Given: a stack action request that includes a working-directory path supplied by the client
When: the server receives it
Then: it is rejected and nothing is sent to the agent.

**AC-083 [P3] [FR-119, FR-120, FR-126]**
Given: stack `media` with `compose.yaml` containing `environment: { DB_PASSWORD: s3cret, LOG_LEVEL: info }`, and `.env` containing `TZ=UTC` and `API_TOKEN=t`
When: the Compose tab opens
Then: the file is shown with line numbers, `s3cret` is `<redacted>`, and `info` is visible
And: the `.env` list shows `TZ=UTC` and `API_TOKEN=<redacted>`
And: no edit control is present, and the read-only state is stated.

**AC-084 [P3] [FR-123, NFR-013]**
Given: a container whose labels declare a working directory of `/srv/stacks/media` and these config files:
- `/srv/stacks/media/../../etc/riot/agent.yaml`;
- a 2 MiB `/srv/stacks/media/big.yaml`;
- `/srv/stacks/media/notes.txt`;
- `/srv/stacks/media/link.yaml`, a symbolic link to a file outside the working directory

When: each is requested
Then: each is refused with a specific reason, and no content is returned.

**AC-085 [P3] [FR-121]**
Given: stack `media` with services `web`, `db` and `cache`, where `cache` stopped 30 minutes ago and has stored lines from before it stopped
When: its Logs tab opens with the 1-hour range
Then: stored lines from all three services are merged in timestamp order, each with its service name prefix and colour.
And When: Live is turned on
Then: the live tails of `web` and `db` are appended under the boundary rules, each service keeping the same colour it had in the history
And When: the `db` toggle is turned off
Then: no `db` lines are shown, in either the history or the live part.
And Given: a stack with 30 containers
Then: the operator is asked to choose at most 25.

**AC-086 [P3] [FR-125]**
Given: a host whose agent supports Update but not Redeploy
When: the stack header renders
Then: Update is enabled, and Redeploy is disabled with the tooltip "Requires agent update".

**AC-087 [P3] [FR-053, Section 8.5]**
Given a stack in each condition below, its header shows this primary action (table-driven):

| Condition | Primary action |
|---|---|
| 0 of 3 running | Up |
| 2 of 3 running | Up |
| 3 of 3 running with an update | Update |
| 3 of 3 running, one unhealthy | Restart |
| 3 of 3 running, healthy | none |

And: Down appears in the destructive group of the ⋯ menu.

**AC-088 [P3] [FR-115, FR-124, FR-127]**
Given: compose container `web` in stack `media`
When: its row in the containers table, its page header and its Overview render
Then: each shows `media` as a link to the stack page
And: the Tiles view heading for `media` links to the stack page and still shows the auto-update toggle.

**AC-106 [P3] [FR-122] (adopted from ADD §10)**
Given: stack `media` on `host-a` with members `web` and `db`, and these entries:
- a stack Restart of `media`;
- a container Restart of `web`;
- a bulk Update that included `db`;
- a Docker `die` event for `web` that rIOt did not trigger;
- a container Restart of `cache` in stack `infra`

When: the `media` stack's Activity tab opens
Then: exactly the first four entries are listed, newest first, each with the FR-092 content
And: the `cache` entry is not listed
And: expanding the stack Restart shows its step timeline (FR-094).

### 9.12 Inventory and prune [P4]

**AC-089 [P4] [FR-128, FR-129, FR-136, NFR-015]**
Given: `host-a` with `docker_inventory` in its whitelist and `host-b` without it
When: Images renders
Then: `host-a` images are listed with their disk usage summary
And: `host-b` shows that the collector is not enabled, naming the collector
And: running the agent's doctor command on `host-b` reports the collector as not enabled.

**AC-090 [P4] [FR-130, FR-131]**
Given: a host with 2,500 images
When: inventory is collected twice
Then: 2,000 images are reported, with "Showing first 2,000 items on host-a"
And: only the latest inventory for that host is stored.

**AC-091 [P4] [FR-132, FR-133, FR-134, FR-135]**
Given: an image used by 2 stopped containers, an untagged unused image, a named unused volume, the `bridge` network, and an inventory collected 40 minutes ago with a 15-minute interval
When: the three inventory pages render
Then: the first image is In use, with "Used by 2 containers"
And: the second image is Dangling and Unused
And: the volume is Named and Unused
And: `bridge` is flagged Built-in
And: each page shows "Inventory as of" with the collection time and a "Stale" marker.

**AC-092 [P4] [FR-137, FR-138, FR-139, FR-140]**
Given: an inventory 20 minutes old, with 2 unused anonymous volumes, 1 unused named volume, and the networks `bridge` and `old-net` (unused)
When: Prune volumes opens
Then: Confirm is disabled until the operator refreshes the inventory
And: after refresh, the 2 anonymous volumes are selected and the named volume is not.
And When: Prune networks opens
Then: `old-net` is listed and `bridge` is not.

**AC-093 [P4] [FR-141]**
Given: a volume prune of 1 volume
When: the dialog opens
Then: Confirm stays disabled until the operator types the host name.
And Given: an image prune of 21 images
Then: the operator must type "21".

**AC-094 [P4] [FR-142, FR-143]**
Given: a confirmed image prune of images A and B, where B became in use after the preview, and C is unused but was not listed
When: the prune runs
Then: A is Removed, B is "Skipped: in use", and C still exists
And: an Activity entry shows the per-item outcomes and the space reclaimed.

**AC-095 [P4] [FR-144]**
Given: a `docker_update` in flight on `host-a`
When: a prune on `host-a` is confirmed
Then: it is reported as "Already in progress" with a link to the update, and no prune runs.
And Given: a prune is in flight on `host-a`
When: a container Restart is sent on `host-a`
Then: it is also rejected.

**AC-096 [P4] [FR-145]**
Given: a stack with a named volume
When: Down runs, and an image prune of all unused images runs
Then: the volume still exists.

**AC-097 [P4] [FR-146]**
Given: a host whose agent does not support prune
When: the inventory pages render for that host
Then: its prune controls are disabled with the tooltip "Requires agent update".

**AC-098 [P4] [FR-147]**
Given: the stack that rIOt identifies as running its own server
When: Down is chosen for that stack
Then: the dialog warns that the dashboard will become unavailable, and requires the operator to type the host name.

### 9.13 Stored-log rotation [P0]

**AC-107 [P0] [FR-148, FR-150, NFR-025]**
Given: stored lines spanning the last 10 UTC days, a retention of 7 days, and an injected clock at 00:30 UTC
When: the retention run executes
Then: the days entirely older than 7 days are removed as whole days
And: every line from the last 7 full days and from the current day remains readable
And: no row-level delete statement was executed against stored container logs (asserted through the database statement log, or a repository spy in unit tests)
And: the remaining days show no dead rows caused by the run.

**AC-108 [P0] [FR-149]**
Given: the container-log retention setting is unset and the general retention setting is 30
When: the server starts
Then: the effective container-log retention is 7 days.
And Given: the setting is 14
Then: the effective retention is 14.
And Given: the setting is `0`, `91` or `abc`
Then: the effective retention is 7, and a startup warning names the setting and the value used.

**AC-109 [P0] [FR-151]**
Given: 14 days of stored lines under a retention of 14
When: the server restarts with a retention of 3 and the retention run executes
Then: only the last 3 full days and the current day remain.
And When: the server restarts with a retention of 14 again
Then: the removed days are not restored, and new days accumulate up to 14.

**AC-110 [P0] [FR-152]**
Given: the default caps (50 lines and 16 KiB), and a container that writes 500 short lines in one collection interval
When: the collection runs
Then: the newest 50 lines are stored
And: a gap marker records 450 lines not stored for that container and interval.
And Given: a container that writes 30 lines of 1 KiB each in one interval
Then: only the newest lines fitting within 16 KiB are stored, and the marker records the rest.

**AC-111 [P0] [FR-153]**
Given: an agent configured with a 5,000-line cap, and a separate legacy agent without caps, each sending 5,000 lines for one container in one ingest
When: the server ingests them
Then: at most 1,000 lines and 256 KiB are stored for that container from each ingest
And: a gap marker records the lines dropped
And: the rest of the telemetry is accepted without error.

**AC-112 [P0] [FR-154]**
Given: a collection at time T, then a container that writes 10 lines and exits at T + 20 s
When: the next collection runs
Then: those 10 lines are stored.

**AC-113 [P0] [FR-155]**
Given: a container running with a TTY that prints "hello"
When: its logs are collected
Then: the stored line is exactly "hello", with stream stdout and no header bytes.

**AC-114 [P0] [FR-156]**
Given: 7 days of stored lines before the move to whole-day rotation, and an agent sending lines throughout
When: the conversion runs
Then: every pre-existing line younger than 7 days is still returned by the stored-log API afterwards
And: lines sent during the conversion are stored
And: the stored-log API returns lines throughout the conversion.

**AC-115 [P0] [NFR-026]**
Given: a container writing 1 line per second within its caps, and a 60 s collection interval
When: the stored history is read at any moment
Then: the newest stored line is no more than 70 s older than the newest line the container wrote.

**AC-122 [P0] [FR-166, FR-167, FR-169]**
Given: an effective retention of 7 days, a receive time R, and lines reported at R + 1 hour, R + 4 minutes and R − 8 days
When: they are ingested
Then: the R + 1 hour and R − 8 days lines are stored at time R, each keeping its reported time and carrying the clock-skew flag
And: the R + 4 minutes line is stored at its own time, with no flag
And: no row is placed where retention never removes it
And: the next day's partition is still created normally (PG test).

**AC-123 [P0] [FR-168]**
Given: an agent that was offline for 10 days, a retention of 7 days, and a buffered backlog of 500 lines for one container, all reported more than 7 days ago
When: the backlog is flushed and ingested
Then: all 500 lines are stored. None is dropped because of its time
And: each is re-stamped to its receive time and flagged
And: they are still readable 6 days after receipt.

**AC-124 [P0] [FR-169]**
Given: a test that places 3 stored log rows where retention never removes them
When: the health check runs
Then: an operator-visible warning states that 3 rows are affected.

**AC-125 [P0] [FR-170, NFR-022]**
Given: `host-b` sending re-stamped lines in every ingest, and an injected clock
When: 3 consecutive ingests from `host-b` contain re-stamped lines
Then: exactly one clock-skew event is raised for `host-b`, stating the approximate skew
And: no second event is raised while it is active, however many further ingests are re-stamped
And When: 1 hour passes with ingests from `host-b` that contain no re-stamped lines
Then: the event is cleared
And: the structured log entries for those ingests carry counts and the largest skew, never line content.

**AC-126 [P0] [FR-171]**
Given: one ingest containing an ordinary line with stream `gap_server` and text `999999+`, a line with stream `foo`, gap reports with a negative count, with an invalid container name, and a duplicate for the same container, and one valid gap report
When: it is ingested
Then: the two lines with invalid streams are dropped and counted, and neither is stored or shown as a gap marker
And: the three invalid gap reports are dropped and counted
And: only the valid gap report is stored, as an agent-reported gap.

**AC-127 [P0] [FR-172]**
Given: a device whose latest data lists 2 containers, an injected clock, and lines of 100 bytes
When: within one minute it sends 500 lines for each of the 2 listed containers and 1,000 lines under each of 20 invented container names
Then: all 1,000 lines of the 2 listed containers are stored
And: at most 200 lines in total are stored for the invented names (the unknown-name budget), and the other 19,800 or more are dropped and counted for the device, with no gap marker
And Given: a device whose latest data lists 25 containers, each sending 1,000 lines of 100 bytes within one minute
When: the lines are ingested
Then: at most 20,000 lines are stored for that device in that minute (min(25, 20) × 1,000)
And: server-ceiling gap markers for its containers record the 5,000 or more lines dropped.

**AC-128 [P0] [FR-173, FR-154, FR-155]**
Given: a P0 server with no P1 component deployed, a container that prints `DATABASE_URL=postgres://app:hunter2@db/app` just before it crashes, and a TTY container that prints `Authorization: Bearer abc123`
When: their lines are collected and ingested
Then: the stored rows contain `<masked>` in place of `hunter2` and `abc123`
And: the number of masking calls equals the number of lines stored (lines dropped by the caps are never masked).

**AC-129 [P0] [NFR-028]**
Given: three test databases: one at main's `000023_command_lifecycle`, one converted under PART-001's old branch numbers, and one in a dirty migration state
When: the reconciliation procedure runs against each (PG tests)
Then: the procedure identifies each state from its schema, not from the version number alone
And: on the PART-001-converted database, the renumbered PART-001 migrations do nothing and change no data
And: on the dirty database, the procedure stops without forcing a version
And: no table holding telemetry, logs, commands or audit data is dropped or truncated in any case
And: a review of the runbook confirms it requires the operator's sign-off per database and a verified backup (`pg_restore --list` succeeds) before any `migrate force`.

### 9.14 Stored-log history view [P1]

**AC-116 [P1] [FR-157, FR-158, FR-161]**
Given: a retention of 7 days and 12,000 stored lines in the last 24 hours for container `web`
When: the Logs tab opens
Then: the 1-hour range is selected, and "History is kept for 7 days" is shown.
And When: the operator selects 24 hours
Then: the newest 5,000 lines are shown, with "Load older lines"
And When: "Load older lines" is activated twice
Then: all 12,000 lines are shown and the control is no longer offered.
And When: a custom range starting 10 days ago is chosen
Then: the range is limited to the oldest retained day, and the picker says so.
And Given: a range with no stored lines
Then: "No stored lines in this range" is shown, not the collector-not-enabled message and not an error.

**AC-117 [P1] [FR-159, FR-160]**
Given: a history containing a gap marker for 450 lines not stored, with the range ending at the present, and an injected clock
When: the view renders
Then: "450 lines not stored (per-container limit)" appears at its position in time
And: "History up to <time of newest stored line>" is shown.
And When: 15 s pass and 3 new lines have been stored, one of which has the same identity as a line already shown
Then: exactly 2 lines are appended and no shown line is repeated.

**AC-130 [P1] [FR-174]**
Given: a stored line received at 10:00:05 UTC whose agent reported 1970-01-01 00:00:12 UTC
When: the history view renders
Then: the line is placed at 10:00:05 UTC, between the lines received just before and just after it
And: a clock-skew marker shows both times as text.

### 9.15 Live and history together [P2]

**AC-118 [P2] [FR-162, BR-019, Section 8.12]**
Given: history whose newest shown line L1 is at time *B*, and lines L2, L3 and L4 written after *B* but not yet stored
When: Live is turned on
Then: L2, L3 and L4 appear once, below L1, in order, and L1 is not shown again.
And When: the collector later stores L2 to L4
Then: they are not added to the view again.
And Given: an empty history range
When: Live is turned on
Then: the tail starts with the last 100 lines.

**AC-119 [P2] [FR-079, FR-163]**
Given: Live on for 2 minutes, during which the collector stored 40 lines
When: the view is inspected
Then: those 40 lines appear only once, from the live tail.
And When: Live is turned off and 15 s pass, during which 5 lines newer than the last shown line are stored
Then: every line already shown remains, and exactly those 5 lines are appended.

**AC-120 [P2] [FR-164]**
Given: a URL to the Logs tab of `web`, copied while Live was on
When: it is opened in a new browser tab
Then: the history is shown with Live off
And: no live stream request is sent to the server or the agent.

**AC-121 [P2] [FR-165, NFR-027]**
Given: three browser tabs with Live on for `web` on `host-a`
When: the agent's log reads are counted
Then: exactly one continuous Docker log read exists for `web`, plus at most one boundary backfill per viewer.
And When: one tab turns Live off
Then: the other two keep receiving lines.
And When: the last tab turns Live off
Then: the agent's read for `web` stops within 5 s, measured with an injected clock.

**AC-131 [P2] [FR-175]**
Given: Live on for `web`, and a live line whose reported time is 1 day in the future
When: it arrives, followed by 5 normal lines
Then: the future-dated line is placed at its receive time with the clock-skew marker
And: all 5 normal lines are appended after it. None is rejected or hidden.

### 9.16 Cross-cutting

**AC-099 [P1-P4] [NFR-002]**
Given: every new or touched Docker UI source file
When: it is scanned
Then: it contains no native `confirm(`, `alert(` or `prompt(` call, no hex colour literal, and no raw `gray-<N>` class
And: no rendered Docker page shows a raw command-status or state enum value.

**AC-100 [P1-P4] [NFR-004]**
Given: 2,000 demo containers in a production build
When: the operator types a search term and toggles a chip
Then: the table reflects each change within 200 ms (performance test).

**AC-101 [P1-P4] [NFR-017]**
Given: each shipped Docker page
When: an automated accessibility scan runs
Then: there are zero serious or critical violations
And: keyboard-only tests show the following:
- the table: arrow keys move between rows, Space toggles a row, Enter opens a row;
- the tabs: arrow keys, Home and End move between tabs;
- the ⋯ menu: opens with Enter, closes with Escape, and returns focus;
- the Logs Live toggle: operable with Space or Enter, and exposes its on/off state.

**AC-102 [P1-P4] [NFR-016]**
Given: the new server with a P1-era agent
When: Logs (Live toggle), Inspect, the stack actions and Images are opened for that host
Then: each shows "Requires agent update" or the collector-not-enabled state, and none shows an error or an empty table
And: the stored history still renders in the Logs tab.

**AC-103 [P1-P4] [NFR-018]**
Given: the demo build
When: each shipped Docker page is opened, and Live is turned on in a Logs tab
Then: populated content, simulated stored history and a simulated stream are shown, with no network request to a real API.

**AC-104 [P1-P4] [NFR-011]**
Given: a live log session, an Inspect view, a Compose view, a stack Down and a prune by the operator
When: the audit record is queried
Then: each has the actor, host, target, time and remote address.

**AC-132 [P2] [NFR-011]**
Given: audit rows on an injected clock:
- long-class rows that are 179 and 181 days old;
- short-class rows that are 89 and 91 days old;
- an open terminal session that started 200 days ago;
- a pre-upgrade terminal row that is 400 days old

When: the retention run executes
Then: the 181-day long-class row, the 91-day short-class row and the 400-day terminal row are removed
And: the 179-day and 89-day rows and the open session remain.
And Given: an operator reboots `host-a` and then deletes the device
Then: an audit row for the reboot and one for the device deletion both exist after the deletion.

**AC-133 [P2] [NFR-009]**
Given: a live log connection opened with a sign-in session that expires in 10 minutes, and an injected clock
When: 10 minutes pass
Then: the server closes the connection and refuses any attachment on it.
And Given: a connection whose session lasts longer than 4 hours
When: 4 hours pass
Then: the connection is closed, and the browser reconnects through a fresh connection that checks the sign-in again.

**AC-105 [P0-P4] [NFR-021]**
Given: the repository's docs, fixtures and demo data added by this story
When: they are scanned for private IPv4 ranges (10/8, 172.16/12, 192.168/16) and email addresses
Then: none is found.

## 10. Out of Scope

- Compose file editing, and creating new stacks or containers from the UI (templates, git-backed deploy). Settled by the product owner (S-4): rIOt never writes compose or `.env` files.
- Container Remove, Kill, Pause and Unpause. Remove is pending D-4.
- Revealing redacted secret values (pending D-2).
- Registry management and registry credentials, image build, and build-cache prune.
- Docker Swarm services, Kubernetes, Podman-specific features, and Windows container specifics beyond what the Docker API reports.
- A container file browser, and exec beyond the existing Terminal.
- Persisting live logs. Fleet-wide log search across containers. Full-text indexing of stored logs.
- Changes to the `container_logs` collector or its storage beyond FR-148 to FR-156: for example collecting stopped containers continuously, multiline grouping, or log-driver integrations.
- Editing the container-log retention from the dashboard. It is a server setting, and the dashboard only displays the effective value (FR-161).
- Changing the rotation of the other five PART-001 tables. That remains PART-001's scope.
- A per-device Docker page of any kind (BR-018).
- New alert rule metrics based on I/O, inventory, or log content.
- Inventory history and disk-usage trends.
- RBAC, multi-user roles, and per-host permissions.
- The app-shell and navigation redesign beyond adding the Docker entry (a separate later story), a light theme, and layouts optimised for widths below 768 px.
- Exporting tables (CSV and similar).
- Changes to auto-update policy semantics beyond moving the existing toggle.
- Releasing, tagging or pushing, and rolling the new collector out to each host's configuration.

## 11. Assumptions

| ID | Assumption |
|---|---|
| A-01 | CMD-LIFECYCLE-001 merges with its FRD intact: canonical states, steps, `stderr_tail`, `failure_step`, `origin`, target keys, the 409 conflict body, and dashboard command tracking. Activity, bulk and stack actions build on it. |
| A-02 | CMD-LIFECYCLE-001 does not record an operator identity. This story adds the actor identity (FR-093). |
| A-03 | Command rows already have, or will be given, a bounded retention. If they have none, the ADD must add one (NFR-003). *(The ADD found none and adds it, AD-025.)* |
| A-04 | The signed-in identity (local admin username, or OIDC identity from the existing external identities) is available to the server when a command is created. *(The ADD found this false and adds session claims, AD-022.)* |
| A-05 | Each host's agent reports a primary address that the operator's browser can reach. If not, port links can be wrong, and the operator can still read the plain text. |
| A-06 | The agent user can read compose files, which are typically world-readable, but often not `.env` files, which are often mode 0600 and owned by root. FR-123 then returns "not readable by the agent". Granting more access is out of scope (NFR-014). |
| A-07 | The Docker Engine API version on hosts supports the log follow, inspect, disk-usage and prune operations. On older engines the operations degrade per NFR-016. |
| A-08 | The default telemetry interval stays around 60 s, so the I/O rates in FR-101 are 60-second averages. The `container_logs` collector runs on the same interval by default, so stored history lags by up to about 60 s (NFR-026). |
| A-09 | Containers on offline hosts are shown from the last received telemetry. |
| A-10 | The terminal relay pattern (an admin-authenticated browser WebSocket, relayed through the agent connection registry after CMD-LIFECYCLE-001) is the model for live logs and on-demand reads. The architect designs against the post-CMD-LIFECYCLE-001 registry. |
| A-11 | Container labels can be set by image authors, not only by compose. Any label-derived path is therefore untrusted (FR-123, NFR-013). |
| A-12 | Docker reports the same timestamp for a log line whether it is read by the collector or by the live stream, at the same precision. The identity rule (Section 8.12) depends on this. |
| A-13 | PART-001's `container_logs` design (daily range partitions on `timestamp`, a legacy partition for existing rows, drop-by-age) is accepted as the rotation design. Any revision PART-001 makes after its own security review applies here too. |
| A-14 | The existing agent setting for the `container_logs` tail (default 50 lines) becomes, or maps onto, the line cap of FR-152, so that hosts with no configuration keep today's behaviour. |
| A-15 | On the production deployment, the v2.67.0 release (which shipped CMD-LIFECYCLE-001) moved the database schema from version 22 to 23 without leaving it dirty, so `000023_command_lifecycle` ran there. Other operators' databases are covered by the NFR-028 procedure. |

## 12. Open Questions (not product decisions; for the architect and the security review)

- **OQ-01:** How can rIOt reliably identify the container or stack that runs the rIOt server and its database (FR-147)? If it cannot, FR-147's warning applies only to containers explicitly marked by the operator, and the ADD must say so. *(Resolved in ADD AD-009, D-25.)*
- **OQ-02:** Is Activity step output stored inside the command record or in a bounded companion structure? Either way it must meet NFR-003 and the 30-day bound. *(Resolved in ADD AD-023.)*
- **OQ-03:** Does the latest inventory ride the telemetry snapshot or a separate latest-only record? Riding every snapshot would grow a high-churn table. NFR-003 asks the ADD to quantify that growth. *(Resolved in ADD AD-028.)*
- **OQ-04:** Which agent version boundary marks each capability (live logs, inspect, stack actions, prune), and how does the server learn it? The answer drives the "Requires agent update" states. *(Resolved in ADD AD-001.)*
- **OQ-05:** Do audit records for live log sessions reuse the terminal session audit store, and what is that store's retention today? *(Resolved in ADD AD-024.)*
- **OQ-06:** Does the security review want to extend the Section 8.6 safe list or sensitive-key list?
- **OQ-07 (rev 2):** How are gap markers (FR-152, FR-153) stored? They could be marker rows in the same partitioned table, or a field on the next stored line. Either way they must be bounded by the same rotation.
- **OQ-08 (rev 2):** How does the server expose the effective container-log retention (FR-161)? It could be a field on an existing settings or features response, or a new read.
- **OQ-09 (rev 2):** For a shared live stream (FR-165), is the per-viewer boundary backfill served by a separate short agent read, or by a short-lived buffer the agent keeps per stream? Either way it must stay within NFR-007 and the backfill bound.
- **OQ-10 (rev 2):** The ADD (AD-016, AD-017) currently models one agent session per browser socket, with its own Docker follow per session. It must be amended for FR-165 (D-36) and for the one-tab model (FR-074, FR-162 to FR-164), including the removal of the `mode` URL parameter and of the separate History component.

## 13. Decisions for the product owner

Decision IDs are shared across the story's documents: D-1 to D-17 and D-33 to
D-36 are defined here, D-19 to D-28 and D-37 to D-41 in the ADD (§17), and D-29
to D-31, D-42 and D-43 in the security review. Settled decisions are listed in
§13.1 with an S-number, and no longer appear among the open decisions:
- D-18 (S-2);
- D-1 (S-4);
- D-21 part (i) (S-5); part (ii) was resolved on 2026-10-19 (S-10);
- D-31 (S-6);
- D-35 together with D-37 (S-7);
- D-42 (S-8);
- D-43 (S-9);
- D-21 part (ii), D-24, D-30, D-44 to D-48 and D-53 to D-58, all defined in the ADD or the security review (S-10).

### 13.1 Settled by the product owner (revisions 2, 3 and 3a)

| ID | Decision | Outcome | Requirements |
|---|---|---|---|
| S-1 | Fleet-wide Docker section | **Yes.** Docker is a top-level section with a fleet-wide view, and it **replaces** the per-device Docker page. The device page links into the fleet view filtered to that host (`/docker/containers?host=<device-id>`). The tile grid survives only as the optional Tiles view inside the fleet table. | FR-006, FR-008, FR-028, BR-018, AC-004, AC-007 |
| S-2 | Legacy device Docker URLs (formerly D-18) | **Redirect** to the new pages. | FR-006, FR-007, BR-014 |
| S-3 | Stored and live logs | **Both, together.** Stored logs are kept on a rotating basis. Live streaming is added on demand. Both appear in one Logs tab: it opens on stored history, and a Live toggle appends the streamed tail. Stack pages merge logs in both modes. | FR-058, FR-074..FR-085, FR-121, FR-148..FR-165, BR-005, BR-017, BR-019 |
| S-4 (rev 3, formerly D-1) | Compose file editing | **(a) Read-only.** rIOt never writes compose or `.env` files. | C-04, FR-126, BR-010, §10 |
| S-5 (rev 3, D-21 part (i), defined in the ADD) | Audit retention | **180 days** by default for the long class: terminal sessions including the host shell, destructive or privileged commands, stack path changes, and device deletions. The short class (reads and other commands) stays at 90 days, never longer than the long class. If the retention is made configurable, that ordering holds. Terminal audit rows are kept forever today, so the upgrade deletes those older than 180 days. D-21 part (ii), the extra live-log bounds, was accepted on 2026-10-19 (S-10). | NFR-011, §8.8, AC-132 |
| S-6 (rev 3, formerly D-31, defined in the security review) | Personal names in public documents | **No personal names** in the DOCKER-UX-001 documents. Use "the product owner" for the decision-maker and "the operator" for the person running rIOt. Older documents on `main` are not changed. | All DOCKER-UX-001 documents |
| S-7 (rev 3, formerly D-35, with D-37 from the ADD) | PART-001 when P0 starts | **(a), merged whole.** PART-001 is merged with all six tables, its migrations renumbered above `000023_command_lifecycle` and kept in their internal order, as P0's first step. | §3, NFR-024, NFR-028, §14, R-15 |
| S-8 (rev 3, formerly D-42, defined in the security review) | Container-log timestamps outside the accepted window | **Re-stamp, never drop.** The stored time becomes the server receive time. The agent-reported time is kept with a clock-skew flag, and the UI shows a skew marker with both times. Lines older than the retention period are re-stamped and flagged too. No row ever lands where retention never removes it. A warning if one does is kept as defence in depth. Re-stamped lines are counted per device, with an event when the skew persists. This replaces the security review's drop recommendation. | FR-166..FR-170, FR-174, FR-175, §8.12, §8.13, AC-122..AC-125, AC-130, AC-131 |
| S-9 (rev 3, formerly D-43, defined in the security review) | Migration reconciliation on deployed databases | **Per-database operator sign-off.** No `migrate force` on any deployed database without the operator's sign-off for that database and a verified `pg_dump` taken first. The state is identified by schema inspection, the procedure stops on a dirty database, and data is never dropped. | NFR-028, AC-129 |
| S-10 (2026-10-19; defined in the ADD and the security review) | The ten remaining design decisions | **All accepted with the recommended values** (recorded in ADD §17.1): D-21 (ii) live-log byte caps (512 KiB/s per stream, 2 MiB/s per agent, 64 viewers, 4 h); D-24 `axe-core` approved as a devDependency; D-30 origin control on **every** state-changing route, including pre-existing ones; D-44 container-writable stack files refused from P1; D-45 the terminal-audit export before the P2 purge stays documented and optional; D-46, D-47 and D-48 ceiling, event and command limits; D-53 keep the catch-all (DEFAULT) partition, and drop partitions under a 1 s lock timeout, stopping at the first timeout; D-54, D-55 and D-56 quota, disk-threshold and fairness values, to be tuned once real fleets run; **D-57** (requirement): under critical free disk, store only priority (stderr and error) lines plus one snapshot per device per hour; **D-58** (requirement): at most 500 registered devices (configurable), plus a daily registration cap (value set at implementation, configurable). | FR-083, FR-165, NFR-009, NFR-011, NFR-025, FR-172, AC-101; ADD §17.1 |

### 13.2 Open decisions

Each default below is applied in this draft. The product owner confirms the default or
picks another option. Changing a default changes the FRs it names.

| ID | Decision | Options | Recommended default | Consequence of the default |
|---|---|---|---|---|
| D-2 | Secret values in env, inspect and compose | (a) Redact every environment value except a safe list, with no reveal. (b) Redact by sensitive-key pattern only. (c) Allow audited "reveal" on demand. | **(a)** | The safest option. The cost is that harmless values such as `APP_MODE` are hidden too. Option (c) means secrets would leave the host, so redaction could no longer happen at the source. |
| D-3 | Mask secret patterns in logs | (a) Mask known patterns in stored history, live logs and Activity output. (b) Show logs raw. | **(a)** | Lowers the chance of exposing secrets. Masking is best effort, and it can hide text that looks like a secret while debugging. |
| D-4 | Container Remove action | (a) Out of scope. (b) Stopped containers only, never removing volumes. (c) Any container, with force. | **(a) Out** | The ⋯ destructive group holds only Stop. Removing a container still needs the CLI. |
| D-5 | Volume removal scope | (a) Named and anonymous volumes, named ones unselected by default, typed confirmation. (b) Anonymous only. (c) No volume removal. | **(a)** | Gives full cleanup capability, with friction where data loss is most likely. |
| D-6 | How inventory collection is enabled | (a) A new opt-in collector name. (b) Folded into the existing `docker` collector, so it is on automatically. | **(a) Opt-in** | Each operator must add the collector name to every host's `collector.enabled`, plus README and doctor support. Hosts without it show "not enabled". Option (b) needs no configuration step, but it adds disk-usage scans to every Docker host and needs a security review of an always-on scan. |
| D-7 | Network and block I/O history | (a) Store it with the existing container metrics, after PART-001 merges. (b) Show current rates only, permanently. | **(a)** | FR-107 waits for PART-001. Until then only current rates are shown. Option (a) widens a high-churn table, which is acceptable only once that table is partitioned. |
| D-8 | Selection in the URL | (a) Store it, capped at 200, visible rows only. (b) Never store it. | **(a)** | A shared link restores the selection. A link can never start an action by itself (NFR-012). |
| D-9 | Default Containers view | (a) Table, with the last choice remembered per browser. (b) Tiles. | **(a) Table** | Matches the Komodo and Portainer layout. The existing tiles stay one click away, inside the fleet view (S-1). |
| D-10 | Same project name on several hosts | (a) A separate stack per host. (b) Grouped into one logical stack. | **(a)** | Simple and accurate. Fleet-wide "same stack" actions are not offered. |
| D-11 | Retention of Down stacks | (a) Listed until the operator forgets them. (b) Forgotten automatically after N days. | **(a)** | Stacks taken down outside rIOt stay listed until they are forgotten. |
| D-12 | Typed-confirmation threshold for bulk Stop | (a) More than 5 targets, or more than 1 host. (b) Always. (c) Never; the dialog alone. | **(a)** | Small single-host stops stay fast. Fleet-wide stops need a deliberate step. |
| D-13 | Live log limits | (a) The Section 8.8 values: 1,000 lines/s per stream, 8 distinct streams per host, 8 viewers per shared stream, 64 viewers server-wide, 5-minute hidden-tab stop, 10,000 lines per view. (b) Different values. | **(a)** | Protects the agent connection that carries commands. Very chatty containers show "lines dropped" markers. Several viewers of one container cost the agent one stream (D-36). |
| D-14 | Retention of Activity step output | (a) 30 days, with the summary kept for as long as commands are. (b) 7 days. (c) 90 days. | **(a)** | A month of detailed failure output is kept, and storage stays bounded. |
| D-15 | Externally observed Docker events in Activity | (a) Included, with the actor "Agent (observed)". (b) Commands only. | **(a)** | Activity also explains changes made outside rIOt, such as crashes or someone running the CLI. |
| D-16 | Primary action for a healthy running container | (a) None. (b) Restart. (c) Logs. | **(a) None** | No disruptive action is emphasised when nothing is wrong. |
| D-17 | Palette scope | (a) App-wide: hosts, containers, stacks, and Docker pages. (b) Docker area only. | **(a)** | Ctrl+K becomes a global way to navigate. Later stories can add more resource types. |
| **D-33 (new)** | Per-container stored-log cap per collection | (a) 50 lines and 16 KiB. (b) 200 lines and 64 KiB. (c) 20 lines and 8 KiB. In every option the server ceiling stays at 1,000 lines and 256 KiB (FR-153). | **(a) 50 lines and 16 KiB** | The line cap matches today's tail of 50, so history completeness does not change silently. The new byte cap bounds hosts with very long lines. Worst case with (a), for 50 chatty containers on a 60 s interval: 16 KiB × 50 × 1,440 ≈ 1.1 GB per host per day before compression; real loads are far lower. (b) keeps more of a chatty container's output for about 4 times the worst-case storage. (c) halves the worst case, but drops more lines (gap markers make every loss visible). |
| **D-34 (new)** | Container-log retention setting | (a) Its own server setting, default 7 days, accepted 1–90. (b) Share the general `RIOT_RETENTION_DAYS` (default 30). (c) Own setting, with a default of 30. | **(a)** | Today's 7-day behaviour is kept when nothing is configured. Operators can raise it deliberately, with storage growing about linearly. (b) silently raises container-log storage about 4 times on every existing install. |
| **D-36 (new)** | Several viewers of the same container's live logs | (a) One agent stream per container, fanned out to all viewers. (b) One agent stream per viewer, bounded only by the per-host cap. | **(a) Fan-out** | Agent load stays flat however many tabs are open (NFR-027). The ADD's live-log design (AD-016, AD-017) needs amending (OQ-10). (b) is simpler, but each extra tab multiplies Docker log reads and masking work on the host. |

## 14. Dependencies

| Dependency | Needed for | Nature |
|---|---|---|
| CMD-LIFECYCLE-001 (branch not merged yet) | P1 to P4: lifecycle states, steps, per-target locks, 409 conflicts, dashboard tracking, and the agent connection registry changes | **Hard gate** for P1 to P4: it must be merged to `main` first (NFR-001). Not needed by P0. |
| UI-FOUNDATION-001 (branch not merged yet) | P1 to P4: primitives, toasts, the formatting module, `<RelativeTime>`, `ApiError`, and the demo provider | **Hard gate** for P1 to P4: it must be merged to `main` first (NFR-001). Not needed by P0. |
| PART-001 (branch `part-001-partitioning`, not merged) | **P0** whole-day rotation of `container_logs` (FR-148, FR-150, FR-151, FR-156), and FR-107 I/O history. It also bounds the growth of the container metrics that FR-062 widens. | **Hard dependency for P0's partition FRs.** Merged whole, with its migrations renumbered above `000023_command_lifecycle`, as P0's first step (NFR-024, S-7), under the migration-safety rules of NFR-028 (S-9). Conditional for FR-107 (D-7). |
| AGENT-PRIV (not merged) | The capability mapping statement for new agent actions and collection (NFR-014) | The ADD must address it. No new privilege is expected. |
| AGENT-HARDEN-001 (shipped) | Signed agent updates, to roll out the new agent versions | Existing |
| FLEET-DASH fleet containers endpoint | The table data, extended per FR-064 | Existing; extended |
| Terminal relay and agent WebSocket | The pattern for live logs and on-demand reads | Existing; the post-CMD-LIFECYCLE-001 registry |
| `container_logs` collector and stored-log API | The history half of the Logs tab (FR-058, FR-157..FR-161), caps (FR-152..FR-155) | Existing; changed in P0 |
| OIDC external identities (migration 000022) | Actor identity (FR-093) | Existing |
| Collector whitelist, `internal/agent/doctor.go`, README, release notes | The P4 collector (NFR-015) | Operator action on each host |
| Pipeline | The architect's ADD and the security review must exist before engineering starts on any phase. The security review must cover live-log authorization, redaction, file reads, prune and bulk destructive actions, and agent privilege. **Revision 2 requires an ADD amendment for P0, the one-tab Logs model and shared streams (OQ-07 to OQ-10), and a security-review check of those amendments.** | Process |

## 15. Risks

| ID | Risk | Impact | Mitigation in this FRD |
|---|---|---|---|
| R-01 | The scope is large (an epic in five phases) | Long delivery; partial value is late | Phases ship independently, in value order (Section 3). P0 runs in parallel. |
| R-02 | Live logs saturate the agent connection that also carries commands | Commands are delayed during incidents, exactly when they matter | Line-rate, stream, viewer and backfill bounds; shared streams (FR-165); NFR-007 latency guard |
| R-03 | Redaction misses a secret with an unusual key name, in a label, or in a log | Secret exposed in the browser or in downloads | Environment variables are redacted by default with a safe list (D-2); redaction runs on the host; logs are masked in both history and live (D-3); the security review may tighten the lists |
| R-04 | Prune or Down removes data the operator still needed | Data loss | Volumes are never removed as a side effect; preview of exact items; only listed and still unused items are removed; typed confirmation; named volumes unselected by default |
| R-05 | The operator stops or takes down rIOt's own stack from rIOt | Dashboard lost; recovery needs SSH | FR-147 warning and typed confirmation; OQ-01 |
| R-06 | Malicious image labels steer compose file reads or stack actions toward sensitive paths | Agent key or config shown in the browser; compose run in an unexpected directory | FR-118, FR-123, NFR-013 confinement and deny rules; A-11 |
| R-07 | Inventory, step output or stored logs grow high-churn storage | A repeat of the PART-001 bloat | NFR-003 and NFR-025 bounds; whole-day rotation of stored logs (FR-148); per-container caps and a server ceiling (FR-152, FR-153); latest-only inventory |
| R-08 | A mixed agent fleet gives an inconsistent experience | Confusing blank views | NFR-016 explicit "Requires agent update" states; AC-102 |
| R-09 | The prerequisite branches change before they merge, or their migration numbers collide | Rework | Hard gate (NFR-001); the ADD uses the "next free migration number" at implementation time |
| R-10 | Operators do not enable the new collector | Images, Volumes and Networks look empty | FR-136 "not enabled" state, doctor check, and release notes (NFR-015) |
| R-11 | The Ctrl+K shortcut conflicts with terminal and browser shortcuts | Lost keystrokes in shells | FR-066 terminal exemption; a header button as an alternative |
| R-12 (rev 2) | Stored history lags the container by up to one collection interval | An operator reading history during an incident misses the newest lines | Freshness notice (FR-160) and NFR-026; the Live toggle closes the gap from the boundary (FR-162) |
| R-13 (rev 2) | The history/live boundary shows duplicates or loses lines | A misleading log view during debugging | Identity rule and boundary rules (Section 8.12), A-12, AC-117 to AC-119. Accepted limitation: identical lines with the same timestamp collapse into one. The TTY parse fix (FR-155) keeps stored and live text comparable. |
| R-14 (rev 2) | Several viewers stream the same container | Agent CPU and Docker log reads multiply | One agent stream per container (FR-165, NFR-027, D-36); viewer bounds |
| R-15 (rev 2, updated rev 3) | PART-001 is not merged, so the rotation fix is delayed or done twice | The bloat risk persists, or two competing partition designs appear | NFR-024 and S-7: PART-001 is merged whole and renumbered, as P0's first step; P0 never ships a second design; FR-152 and FR-153 ship independently |
| R-16 (rev 2) | The per-container cap hides output from chatty containers | Missing lines in history | Gap markers make every loss visible (FR-159). The cap is configurable per agent (FR-152). The Live tail is not capped by it. |
| R-17 (rev 2) | Lowering the retention removes history irreversibly | Lost evidence for an investigation | FR-151 states the behaviour. The effective value is visible in the Logs tab (FR-161). Release notes must say that lowering the value deletes whole days on the next run. |
| R-18 (rev 3) | Devices with a wrong clock produce lines whose stored time differs from the time they report | The history order differs from what the device believes, which can confuse an investigation | Lines are kept and re-stamped, never dropped (FR-166 to FR-168). Both times are shown with a skew marker (FR-174, FR-175). A clock-skew event points at the device to fix (FR-170). Section 8.13 explains the behaviour. |
| R-19 (rev 3) | Reconciling migration numbers on a deployed database goes wrong | A database is left dirty, a migration runs twice, or data is lost | NFR-028 and S-9: per-database operator sign-off, a verified backup first, identification by schema inspection, stop on dirty, never drop data, and a tested no-op re-run of the renumbered migrations. A-15 records the one deployment already confirmed clean. |
