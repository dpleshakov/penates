# Backlog

Accepted work that has not started yet, in priority order.

### `release-pipeline`
**Goal:** One-command releases: archives for every platform, checksums, and a release workflow that turns the unreleased changelog section into a draft release.
**Context:** Blocked until the distribution channels are decided (concept.md, open questions); model the setup on a proven goreleaser and release-workflow pattern.
**Blocked until:** the distribution decision
**Added:** 2026-11-04, in the walking-skeleton plan

### `ascii-detect`
**Goal:** Decide whether penates detects a terminal that cannot render the Unicode glyph set — Windows console code page, Unix locale — and switches to the ASCII set without `PENATES_ASCII=1`.
**Context:** v1 ships the variable alone (docs/tui-screens.md, 3.3). The sketched scheme, if detection is wanted: `PENATES_ASCII` overrides everything; on Windows, a console output code page other than 65001 selects the ASCII set; on Unix, an encoding other than UTF-8 in LC_ALL, LC_CTYPE, or LANG does. The open question is less feasibility than need: whether users run terminals that are both broken for Unicode and not known to them. Decide after the new TUI sees real use.
**Added:** 2026-11-07, in the TUI rework discussion

### `coowner-single-run-removal`
**Goal:** A co-owned json value leaves when one removal run takes both of its owners away.
**Context:** `removalJSON` decides co-ownership by the lock, which planning never mutates. Removing both co-owners in one run, each item's plan still sees the other as a remaining owner and keeps the value — after the run no owner remains recorded, and the value stays in the target with nobody owning it; two sequential runs take it away on the second. Found while composing shared-file removals across a run (the same single-run class); the fix is to make the ownership check see the removals the run has already planned.
**Added:** 2026-11-07, during the removal-shared-file-loss fix

### `kit-item-errors-on-rows`
**Goal:** Item-level problems of a kit — an invalid item.yaml, duplicate ids — show on the affected item rows of the tree, as tui-screens 6.5 specifies.
**Context:** kit.Read loads tolerantly and the add-kit preview lists the problems, but the tree loads kits with the strict kit.Load: an attached kit holding broken items renders unreadable as a whole instead of showing its healthy items with the problems on their rows. enterTarget needs to read through kit.Read and carry the per-item problems into the rows.
**Added:** 2026-11-07, during the add-kit flow of the TUI rework

### `tui-tempdir-flake`
**Goal:** Track the intermittent `internal/tui` test failure on this machine and fix it if it ever turns out to be ours.
**Context:** The exact failure is `t.TempDir` RemoveAll cleanup: `unlinkat …\locks: The directory is not empty` — always after the test body passed, in old apply and tui tests alike. Not a busy handle (Windows would refuse with "being used by another process"): directory-entry deletion lags behind file deletion for the test process. The frequency swings by day — from about one run in twenty to nearly every second run — with no change on the code side; the mechanism (antivirus scanning fresh files, WSL interop overhead) is not pinned down. The temp directory itself is native NTFS. Nothing in the code holds handles across the cleanup. Expected to be unreachable on the CI runners native file systems; close after CI confirms, or if it ever shows there, treat as a real defect.
**Added:** 2026-11-07, during the TUI core tasks

### `mouse-support`
**Goal:** Additive mouse input for the TUI: a click moves the cursor, clicking the pending column toggles the default action, the wheel scrolls, and clicking the target in the header opens the target list (docs/tui-screens.md, 3.3).
**Context:** Postponed from the v1 TUI rework: the keyboard covers every action, so the mouse adds convenience only; the design does not depend on it.
**Blocked until:** the keyboard implementation of the new TUI (docs/2026-11-07-tasks-tui-rework.md) has landed; revisit the question then
**Added:** 2026-11-07, in the TUI rework discussion
