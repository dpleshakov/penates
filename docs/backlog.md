# Backlog

Accepted work that has not started yet, in priority order.

### `section-contributions`
**Goal:** Items contribute a marked Markdown block to a shared file, per the RFC (7.3): markers with the item key, append at the end of the file, three-state evaluation of the block content, and the coarse duplicate-heading check.
**Context:** Needs the target snapshot extended with the block layout of shared Markdown files; the lock already carries the section fields.
**Added:** 2026-11-04, in the walking-skeleton plan

### `json-contributions`
**Goal:** Items contribute values into a shared JSON or JSONC file, per the RFC (7.4): leaf values and array elements, co-ownership between items, comments and formatting of untouched parts preserved.
**Context:** hujson is the chosen parser; the risk and the fallback are recorded in docs/architecture.md. Hardest slice; scope after section-contributions to reuse its shared-file plumbing.
**Added:** 2026-11-04, in the walking-skeleton plan

### `variables`
**Goal:** Per-item variables work end to end, per the RFC (6.4): forms in the TUI, answers stored in the state and the lock, plain `{{var}}` substitution in contributions that opt in, unknown placeholders as errors.
**Added:** 2026-11-04, in the walking-skeleton plan

### `updates`
**Goal:** Updates arrive from the pinned source kit, per the RFC (8): the version table with content-changed warnings, the update rows of the 7.2 state table for owned files, downgrade display, and the item CHANGELOG.md shown for review.
**Context:** The update rows are unreachable during a first install, which is why they were deferred from the walking skeleton.
**Added:** 2026-11-04, in the walking-skeleton plan

### `removal`
**Goal:** Removing an item deletes what it owns, per the RFC (9): owned files and directories, sections with their markers, JSON locations with last-owner counting, user-modified content kept and reported; orphans and unavailable kits freeze.
**Added:** 2026-11-04, in the walking-skeleton plan

### `reconciliation`
**Goal:** After an interrupted apply, penates reconciles the lock with the target using recorded hashes before planning, per the RFC (7.7).
**Added:** 2026-11-04, in the walking-skeleton plan

### `tui-polish`
**Goal:** The remaining screens of the RFC (10): text filter, item details with contributions and changelog, the full status set including conflict and warning markers, and the change summary before applying.
**Added:** 2026-11-04, in the walking-skeleton plan

### `release-pipeline`
**Goal:** One-command releases: archives for every platform, checksums, and a release workflow that turns the unreleased changelog section into a draft release.
**Context:** Blocked until the distribution channels are decided (RFC, open question 5); model the setup on a proven goreleaser and release-workflow pattern.
**Blocked until:** the distribution decision
**Added:** 2026-11-04, in the walking-skeleton plan
