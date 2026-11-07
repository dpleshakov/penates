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

### `ascii-detect`
**Goal:** Decide whether penates detects a terminal that cannot render the Unicode glyph set — Windows console code page, Unix locale — and switches to the ASCII set without `PENATES_ASCII=1`.
**Context:** v1 ships the variable alone (docs/tui-screens.md, 3.3). The sketched scheme, if detection is wanted: `PENATES_ASCII` overrides everything; on Windows, a console output code page other than 65001 selects the ASCII set; on Unix, an encoding other than UTF-8 in LC_ALL, LC_CTYPE, or LANG does. The open question is less feasibility than need: whether users run terminals that are both broken for Unicode and not known to them. Decide after the new TUI sees real use.
**Added:** 2026-11-07, in the TUI rework discussion

### `tui-tempdir-flake`
**Goal:** Track the intermittent `internal/tui` test failure on this machine and fix it if it ever turns out to be ours.
**Context:** The exact failure is `t.TempDir` RemoveAll cleanup: `unlinkat …\TestListStatuses…\locks: The directory is not empty` — seen roughly once per twenty full runs, always after the test body passed. Not a busy handle (Windows would refuse with "being used by another process"): the deletion of files inside `locks` lags behind the directory removal on the WSL drvfs mount, likely widened by antivirus scanning fresh files. The tui tests hit the window most because they save locks most. Nothing in the code holds handles across the cleanup, and the apply-stepping change did not touch the step bodies. Expected to be unreachable on the CI runners' native file systems; close after CI confirms, or if it ever shows there, treat as a real defect.
**Added:** 2026-11-07, during the TUI core tasks

### `mouse-support`
**Goal:** Additive mouse input for the TUI: a click moves the cursor, clicking the pending column toggles the default action, the wheel scrolls, and clicking the target in the header opens the target list (docs/tui-screens.md, 3.3).
**Context:** Postponed from the v1 TUI rework: the keyboard covers every action, so the mouse adds convenience only; the design does not depend on it.
**Blocked until:** the keyboard implementation of the new TUI (docs/2026-11-07-tasks-tui-rework.md) has landed; revisit the question then
**Added:** 2026-11-07, in the TUI rework discussion
