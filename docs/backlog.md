# Backlog

Accepted work that has not started yet, in priority order.

### `applied-selection-stays-staged`
**Goal:** The apply that runs a selection consumes it: after it, the registry holds only actions chosen since the last apply, so the TUI stops offering the restore dialog for what was already applied and the CLI's staged refusal no longer deadlocks against a satisfied selection.
**Context:** `state.SettlePending` (internal/state/operations.go) keeps `Select: true` for every key of the applied selection — its doc comment's "the applied selection stays remembered". The behavior entered in v0.1.1 as the TUI's `settleAfterApply` (commit 1d87114) and moved unchanged into the Store for 0.1.2. `ItemPending.Select` thus carries two meanings at once: an action waiting for an apply, and the TUI's remembered checkmark. The registry section of docs/concept.md defines pending actions as "the actions chosen since its last apply", and docs/tui-screens.md 11.1 words the restore offer for "unapplied pending actions" — the kept selection contradicts both.

The loop, reproduced end to end with the 0.1.2 binary in a scratch `XDG_CONFIG_HOME`: install an item; stage the selection exactly as the TUI leaves it after its apply — a pending entry of `{select: true, version: <the installed version>}`; from then on every one-shot command refuses with `Error [staged]: … apply <kit/item>`, `penates apply` answers `kept: already installed` with 0 writes, and `SettlePending` keeps the entry, so the refusal returns unchanged. Nothing self-resolves; only `pending clear`, unchecking the row in the TUI, or the restore dialog's "Start with no pending actions" ends it. In the TUI the same entry keeps the pending mark on the row forever and shows "The last session ended with 1 pending action" at every start (`offerRestore`, internal/tui/targets.go), with Restore as the default choice — the loop seen live on a real target since 0.1.1. The 0.1.2 staged refusal turned it into a CLI deadlock as well.

Fix direction: the settle consumes `Select` (with its `Answers`) for keys whose apply landed or was kept, the way it consumes `Reset`; the TUI's checked rows become session view state instead of registry content. The settle input must be per-item outcomes, not the selection list alone: a key the plan excluded (a missing answer, a conflict) or whose step failed keeps its action staged and repeatable — an unsatisfied intent stays staged. Test coverage today: `TestApplyRunsRemovalsResetsAndSelection` (internal/cli/mutate_test.go) pins the settle of removals and resets only; nothing pins a selection's fate after an apply. The save and restore of unapplied selections across target switches (targets_test.go, 8.2 and 11.1) is the intended feature and must survive the fix. When the work lands: the TUI half is released behavior and earns a Fixed entry; the CLI deadlock half exists only in unreleased code and folds into the CLI's Added entry; tui-screens.md section 10 may need a sentence saying the applied selection is consumed.
**Added:** 2026-11-22, while verifying the built 0.1.2 release archive

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
