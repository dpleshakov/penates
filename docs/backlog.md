# Backlog

Accepted work that has not started yet, in priority order.

### `failed-kit-note`
**Goal:** A registered kit that fails to load and holds no installed items still shows its group header with the "kit did not load" note; today such a kit renders no group at all and the note is lost from the list.
**Context:** Found in the TASK-12 review of the full-RFC board; the note renders under the header, but the list skips groups whose rows the filter does not show — and a failed kit with no installed items has none.
**Added:** 2026-11-06, in the full-RFC plan

### `release-pipeline`
**Goal:** One-command releases: archives for every platform, checksums, and a release workflow that turns the unreleased changelog section into a draft release.
**Context:** Blocked until the distribution channels are decided (concept.md, open questions); model the setup on a proven goreleaser and release-workflow pattern.
**Blocked until:** the distribution decision
**Added:** 2026-11-04, in the walking-skeleton plan
