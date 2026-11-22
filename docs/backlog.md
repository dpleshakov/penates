# Backlog

Accepted work that has not started yet, in priority order.

### `file-exec-bit`
**Goal:** A `file` contribution whose source file carries the executable bit installs with that bit preserved, so a kit can ship runnable scripts.
**Context:** apply.Item stages every contribution through stage() (internal/apply/apply.go), which chmods each staged file to 0o644 regardless of its source, and the kit loader reads file bytes only, so the mode is lost before planning. A skill whose helper scripts run as bare commands — symlinked onto PATH — installs unrunnable, and the symlink breaks. Found while publishing the peer-chat item in penates-opencode; that item's README carries a note pointing here until this lands. The fix threads the source mode through kit load → plan action → stage.
**Added:** 2026-11-18, while publishing the peer-chat kit item

### `mixed-run-shared-composition`
**Goal:** An apply that removes and installs items composing the same shared file in one run leaves the file as the run's removals and installs together intend it.
**Context:** runApply builds the removal and the install plans from the same pre-run snapshot and lock (internal/tui/apply.go), and the install planner composes shared files — sections and json values — from the snapshot alone; the removals the same run plans never reach it. An install into a shared file whose values the run removes writes them back with its whole-file rewrite, leaving the value in the target with no owner recorded. Reachable through the documented swap flow (concept 6.6: mark X for removal and select Y in the same apply). Found while verifying the boundary of the coowner-single-run-removal fix — the same single-run class. The fix direction: hand the removals' composed shared-file contents to plan.Build as the base its items compose from.
**Added:** 2026-11-10, in the backlog-fixes review

### `failed-step-leaves-unowned-files`
**Goal:** A step that fails after writing its files but before saving the lock leaves the target clean of them, so the retry applies without meeting penates's own output as user content.
**Context:** apply.Item writes an item's files and saves the lock as its last act; a failure at the lock save (a read-only lock file on Windows, a full disk) leaves the files renamed into the target with no lock record, and the reconciliation sees them as user content — the next apply refuses them with a conflict until the user removes them by hand. Found while writing the io-injection test of the settle fix (`TestApplyKeepsFailedSelectionStaged`, internal/cli/mutate_test.go): its Windows leg had to clear the files the refused step had landed. Item-level atomicity (concept 7.7) promises a failed item's changes are not recorded — whether the fix is undoing the renames on a lock-save failure or recording and reconciling them is the open question.
**Added:** 2026-11-22, while testing the settle fix

### `apply-reports-remaining-pending`
**Goal:** An apply that leaves pending actions staged — an excluded item, a failed step — says so in its own output: a note after the result names what stays staged and the two ways out.
**Context:** The staying behavior is the design (an unsatisfied intent stays staged and repeatable) but it is invisible: the reader learns of it only from the next command's staged refusal. The mechanics exist — in `mutation.run` (internal/cli/mutate.go), after `after` (settleAndComplete) a `hasPending` check arms the note; its lines are `pendingActionLines`, the vocabulary `status` and `pending clear` already share; notes are not printed on a dry-run, where nothing settles — consistent. The note names two ways out, not the staged refusal's three: its reader has just run the apply, so "ask the person who staged them" does not apply — apply again, or `penates pending clear`. The form is apply-only by construction: setup and the one-shot commands refuse at `stagedRefusal` before this point. The TUI carries the same phrase on its notice line after the results screen (concept 10.2, transient results) — consistency; the CLI note is the core. Concept 10.3 gains the sentence about the mutating commands' output, 10.2 the TUI notice; CHANGELOG under [Unreleased].
**Added:** 2026-11-22, in the peer-room UX discussion after the agent-driven 0.1.3 upgrade

### `pending-list`
**Goal:** `penates pending list` names every staged pending action across the registered targets — the cross-target audit of where something waits — read-only, a sibling of `pending clear` in the spirit of `kit list` and `target list`.
**Context:** The symmetric medicine for the group's show half: `kit` and `target` are honest because `list` exists, and the gate is the concept's "the command set grows as scenarios demand". The scenario uncovered today is the cross-target audit; the single-target case is already carried by the staged refusal, which names the target and its actions in full, and the apply note (`apply-reports-remaining-pending`) closes the anticipation from the apply side. The command reads the registry's pending map over all targets; no `--target`, consistent with 10.3's rule that the flag marks commands that act on one target; read-only, covered by the reading commands' allow rule. Demand datapoint: an agent driving the 0.1.3 upgrade reached for bare `penates pending` to inspect — present, not pressing.
**Added:** 2026-11-22, in the peer-room UX discussion after the agent-driven 0.1.3 upgrade

### `ascii-detect`
**Goal:** Decide whether penates detects a terminal that cannot render the Unicode glyph set — Windows console code page, Unix locale — and switches to the ASCII set without `PENATES_ASCII=1`.
**Context:** v1 ships the variable alone (docs/tui-screens.md, 3.3). The sketched scheme, if detection is wanted: `PENATES_ASCII` overrides everything; on Windows, a console output code page other than 65001 selects the ASCII set; on Unix, an encoding other than UTF-8 in LC_ALL, LC_CTYPE, or LANG does. The open question is less feasibility than need: whether users run terminals that are both broken for Unicode and not known to them. Decide after the new TUI sees real use.
**Added:** 2026-11-07, in the TUI rework discussion

### `mouse-support`
**Goal:** Additive mouse input for the TUI: a click moves the cursor, clicking the pending column toggles the default action, the wheel scrolls, and clicking the target in the header opens the target list (docs/tui-screens.md, 3.3).
**Context:** Postponed from the v1 TUI rework: the keyboard covers every action, so the mouse adds convenience only; the design does not depend on it.
**Blocked until:** the keyboard implementation of the new TUI (docs/2026-11-07-tasks-tui-rework.md) has landed; revisit the question then
**Added:** 2026-11-07, in the TUI rework discussion
