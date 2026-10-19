# DOCKER-UX-001 documentation report

Updated README feature descriptions, Docker navigation, retained history/live behavior, agent capability and inventory notes, storage settings and upgrade references. Added the Unreleased changelog entry. Imported the PART-001 documentation with its dependency merge.

Added [Docker API reference](../ops/docker-api.md) and [database upgrade procedure](../ops/docker-upgrade.md), covering the new routes, explicit live sessions, masked reads, exact cleanup previews, migration numbering, read-only fingerprinting and production backup/sign-off requirements. Added implementation and QA reports with actual validation limits.

Checked route names against server registration and configuration names against the implementation. History `after` takes precedence over `from`; pagination uses an opaque `before` cursor. Documentation distinguishes stored history from ephemeral live logs and makes inventory collection opt-in. No deployment was performed.

Stale content found outside scope: historical PART-001 reports retain their original branch migration numbers and original validation dates. They describe the dependency's history; the Docker implementation report and upgrade procedure describe the renumbered integrated sequence. The existing frontend bundle-size warning is a performance follow-up, not a new dependency or documentation error.
