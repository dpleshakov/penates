# Backlog

Accepted work that has not started yet, in priority order.

### `tui-deferred-only-apply`
**Goal:** Applying a target whose pending holds only deferred operations completes them in the TUI, the way the CLI's apply does, instead of showing an empty results screen.
**Context:** `beginApply` (internal/tui/apply.go) skips the summary and shows empty results when no item actions are staged, so a deferred detach or forget waits forever unless item actions are staged beside it. The CLI's apply completes or drops the deferred with a report (internal/cli/mutate.go, completeDeferredOnly); the open question is what the TUI summary shows for a deferred-only apply. Found while implementing the CLI's apply.
**Added:** 2026-11-21, during the CLI interface work

### `file-exec-bit`
**Goal:** A `file` contribution whose source file carries the executable bit installs with that bit preserved, so a kit can ship runnable scripts.
**Context:** apply.Item stages every contribution through stage() (internal/apply/apply.go), which chmods each staged file to 0o644 regardless of its source, and the kit loader reads file bytes only, so the mode is lost before planning. A skill whose helper scripts run as bare commands — symlinked onto PATH — installs unrunnable, and the symlink breaks. Found while publishing the peer-chat item in penates-opencode; that item's README carries a note pointing here until this lands. The fix threads the source mode through kit load → plan action → stage.
**Added:** 2026-11-18, while publishing the peer-chat kit item

### `mixed-run-shared-composition`
**Goal:** An apply that removes and installs items composing the same shared file in one run leaves the file as the run's removals and installs together intend it.
**Context:** runApply builds the removal and the install plans from the same pre-run snapshot and lock (internal/tui/apply.go), and the install planner composes shared files — sections and json values — from the snapshot alone; the removals the same run plans never reach it. An install into a shared file whose values the run removes writes them back with its whole-file rewrite, leaving the value in the target with no owner recorded. Reachable through the documented swap flow (concept 6.6: mark X for removal and select Y in the same apply). Found while verifying the boundary of the coowner-single-run-removal fix — the same single-run class. The fix direction: hand the removals' composed shared-file contents to plan.Build as the base its items compose from.
**Added:** 2026-11-10, in the backlog-fixes review

### `ascii-detect`
**Goal:** Decide whether penates detects a terminal that cannot render the Unicode glyph set — Windows console code page, Unix locale — and switches to the ASCII set without `PENATES_ASCII=1`.
**Context:** v1 ships the variable alone (docs/tui-screens.md, 3.3). The sketched scheme, if detection is wanted: `PENATES_ASCII` overrides everything; on Windows, a console output code page other than 65001 selects the ASCII set; on Unix, an encoding other than UTF-8 in LC_ALL, LC_CTYPE, or LANG does. The open question is less feasibility than need: whether users run terminals that are both broken for Unicode and not known to them. Decide after the new TUI sees real use.
**Added:** 2026-11-07, in the TUI rework discussion

### `mouse-support`
**Goal:** Additive mouse input for the TUI: a click moves the cursor, clicking the pending column toggles the default action, the wheel scrolls, and clicking the target in the header opens the target list (docs/tui-screens.md, 3.3).
**Context:** Postponed from the v1 TUI rework: the keyboard covers every action, so the mouse adds convenience only; the design does not depend on it.
**Blocked until:** the keyboard implementation of the new TUI (docs/2026-11-07-tasks-tui-rework.md) has landed; revisit the question then
**Added:** 2026-11-07, in the TUI rework discussion
