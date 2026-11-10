# Backlog

Accepted work that has not started yet, in priority order.

### `ascii-detect`
**Goal:** Decide whether penates detects a terminal that cannot render the Unicode glyph set — Windows console code page, Unix locale — and switches to the ASCII set without `PENATES_ASCII=1`.
**Context:** v1 ships the variable alone (docs/tui-screens.md, 3.3). The sketched scheme, if detection is wanted: `PENATES_ASCII` overrides everything; on Windows, a console output code page other than 65001 selects the ASCII set; on Unix, an encoding other than UTF-8 in LC_ALL, LC_CTYPE, or LANG does. The open question is less feasibility than need: whether users run terminals that are both broken for Unicode and not known to them. Decide after the new TUI sees real use.
**Added:** 2026-11-07, in the TUI rework discussion

### `restore-stale-selection`
**Goal:** Restoring a saved selection tells the user when the kit moved on: a selection whose item version changed since it was saved is dropped and reported, as tui-screens 11.1 specifies.
**Context:** The pending object does not record the kit version the selection was made against, so restore validates structurally only — the item still exists in its kit; an update saved against 1.3.0 restores silently when the kit already holds 1.4.0 and applies at the new version. Fixing this needs the kit version stored in ItemPending at selection time and compared on restore.
**Added:** 2026-11-07, during the overlays and startup task of the TUI rework

### `mouse-support`
**Goal:** Additive mouse input for the TUI: a click moves the cursor, clicking the pending column toggles the default action, the wheel scrolls, and clicking the target in the header opens the target list (docs/tui-screens.md, 3.3).
**Context:** Postponed from the v1 TUI rework: the keyboard covers every action, so the mouse adds convenience only; the design does not depend on it.
**Blocked until:** the keyboard implementation of the new TUI (docs/2026-11-07-tasks-tui-rework.md) has landed; revisit the question then
**Added:** 2026-11-07, in the TUI rework discussion
