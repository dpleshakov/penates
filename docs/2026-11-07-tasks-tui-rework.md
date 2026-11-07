# 2026-11-07-tasks-tui-rework.md

**Status:** Active

## Context

The TUI of docs/tui-screens.md, replacing the screens that grew during the
RFC implementation. Starts when docs/2026-11-07-tasks-tui-core.md is
archived — the state model, its operations, the structured conflicts, and
the stepping apply it builds are prerequisites. Decisions spanning the
tasks:

- Tasks 01–04 build components with the old TUI untouched; task 05
  replaces the old screens in one commit; tasks 06–10 complete the flows
  on the new model. Intermediate states are working releases, partial
  against the spec: until 06, forms interrupt at apply time; until 07,
  adding a kit is a bare path input; until 09, the summary keeps the old
  format.
- Mouse input is out of scope (backlog `mouse-support`); the ASCII set
  switches on `PENATES_ASCII=1` alone (backlog `ascii-detect`).
- The fate of backlog `failed-kit-note` is decided in task 03: the tree
  model either renders a failed kit's group note, closing the entry, or
  the entry is re-scoped.
- Documentation and CHANGELOG ride in the same commits as the behaviour
  they describe (repository rule).

Spec: docs/tui-screens.md; model rules: docs/concept.md.

---

### TASK-01 `layout-framework`
**Description:** The layout engine the screens render through: fixed
header and footer, scrolling body that keeps the cursor row in view,
optional input and notice lines; the width breakpoints of 2.2 — the
details pane at 110 columns and above (about 40% of the width, at least
44 columns), the description column at 80–109, tags and description
dropped at 60–79, the minimum-size message below 60 columns or 16 rows;
columns drop in the order tags, description, then the version column
shrinks to the kit version; prose wraps at 100 columns; the target path
shortens from the left; resize re-lays out; a notice clears on the next
key or after a few seconds.
**Definition of done:** golden tests render fixture screens at 120, 90,
64, and below-minimum sizes and assert regions, dropped columns,
shortening, and the minimum-size message; a resize test re-lays out
without losing the cursor; the notice clears by key and by timeout.
**Status:** Done

### TASK-02 `visual-language`
**Description:** The glyph and color tables of 3.1–3.3: status and
pending glyphs with colors and ASCII fallbacks, dim secondary
information, the light and dark theme, NO_COLOR, the ASCII set selected
by `PENATES_ASCII=1`, and the ASCII spinner fallback of 10.2.
**Definition of done:** table tests cover every status and pending mark
in the Unicode and ASCII sets, with and without NO_COLOR; the spinner
falls back to `|` `/` `-` `\`.
**Status:** Done

### TASK-03 `tree-model`
**Description:** The pure model behind the body (3.4): one tree of kit
rows (expansion mark, item-state summary by glyph, path), item rows
(status, id, tags, version column, description or conflict reason,
pending mark), unreadable kits collapsed, the orphans group at the end,
the `+ add kit` row. Built from state, lock, snapshot, and plan
statuses; kit collapse state; the filter of 7.5 with match counts on kit
rows; pending actions per row following the 7.1 table.
**Definition of done:** table tests build trees from fixture state,
lock, snapshot, and plans — summaries count by glyph, frozen rows keep
their groups, filter counts are right, default actions follow statuses;
the model performs no I/O.
**Status:** Done

### TASK-04 `details-renderer`
**Description:** The shared content of the details pane and the details
view (5.1, 5.2): item details — status with the reason and the next
step, the pending verb, tags, stored variables, contributes with section
block and JSON pointers, changelog — and kit details — description,
path, target hint, shared files, item counts, also-attached targets.
Conflicts render from the structured causes of the core work. Content
wraps at 100 columns; the view scrolls.
**Definition of done:** fixture items and kits render the sections of
5.1 and 5.2 in order with reasons and next steps; wrapping and
scrolling are tested at fixed sizes.
**Status:** Done

### TASK-05 `main-screen-flip`
**Description:** Assemble tasks 01–04 into the new model and delete the
old screens: the main screen with the header (target, pending count,
help hint) and the footer naming the keys of the row under the cursor;
navigation of 12.1–12.4; `space`/`d`/`r` with the major-update gate
(7.2); the filter input (7.5); the first-run state (6.1) with `+ add
kit` accepting a path and attaching at `target_hint` — the full flow
with completion and preview is task 07. Startup opens the last active
target (11); a kit path argument opens the add-kit input prefilled. The
list-planning plumbing of the old model carries over. concept.md 10.2
and the README screen are rewritten in the same commit.
The concept reset rule 7.6 is reconciled with the interface table 7.1 in
the same rewrite: the model keeps reset for any installed item, the
interface names where the key exists.
**Definition of done:** the main screen renders at every breakpoint;
every capability of the deleted screens exists in the new UI — list,
statuses, select, reset, remove, filter, details, summary, apply,
results; the TUI tests port to the new model and pass; concept.md and
the README are updated.
**Status:** Done

### TASK-06 `selection-forms`
**Description:** Move the variable forms to selection time (7.3):
selecting an item with variables, or an update introducing a new one,
turns the details pane or view into a form; fields prefilled with stored
answers, `tab` between fields, `enter` accepted only when every required
field has a value, `esc` clears the pending action. Answers go to the
pending object of the state and reach the lock through the apply; the
apply-time form interruption is removed.
**Definition of done:** tests select an item with required and optional
variables and check prefill, gating, and `esc`; answers land in the
state pending and the summary shows substituted content; nothing asks
for variables at apply time.
**Status:** Done

### TASK-07 `add-kit-and-management`
**Description:** The kit flows of section 6: the inline input with
directory completion and the debounced live preview (6.2); the
target-hint dialog and the first-target input (6.3, 6.4); preview errors
with the input staying editable (6.5); `e` relocation with the
same-name rule (6.6); the `d` detach dialog and the detaching state
(6.7). Built on the state operations of the core file, not on new state
logic.
**Definition of done:** headless tests drive each flow — completion
lists directories, the preview appears for a valid path and shows every
error of 6.5, attach follows the three outcomes of 6.3, relocation
rejects a renamed kit, both detach paths behave per 6.7.
**Status:** Done

### TASK-08 `targets`
**Description:** The target list of 8.1: fuzzy search over path and
name, the current marker, kit and item counts, `+ add target` with the
existence line, renaming the display name, removal with the 8.3 dialog —
remove items then forget versus forget leaving files — and switching
that saves the pending actions of the target being left (8.2) with the
restore offer of 11.1. The fuzzy matcher is shared with the palette of
task 10.
**Definition of done:** tests switch between targets preserving
per-target pending actions and offering restoration; rename changes the
header; both removal paths end in the right state — items removed and
the target forgotten, or the lock deleted and the files untouched.
**Status:** Done

### TASK-09 `apply-screens`
**Description:** The apply screens of section 10: the summary in the
format of 10.1 — verb groups (install, update, remove, reset, replace)
with per-path marks `+` `~` `-` `=` `!`, excluded items with reasons,
`nothing to apply` when everything is excluded; progress driven through
the stepping apply of the core file — per-item spinner, counts, the
view not leavable by navigation; the result screen with kept-content
notes; the session record printed to the terminal on exit (10.4).
**Definition of done:** a mixed plan renders the summary of 10.1 as a
golden test; stepping drives the progress view item by item; results
and the exit record list the applied changes; an interruption between
steps is repaired on the next start.
**Status:** Pending

### TASK-10 `overlays-and-startup`
**Description:** The remaining overlays and startup flows: the command
palette (9) with fuzzy search over the fixed command set, acting on the
row context and asking for a kit when the cursor is outside one; the
help screen (12.5); notices with `u` undo for immediate configuration
changes (11.2); the restore dialog on entering a target with saved
pending actions, dropping invalid ones with a notice (11.1); the quit
dialog (11.3); the double `ctrl+c` force quit without dialogs (12.1).
**Definition of done:** the palette runs every command of section 9;
help lists the bindings and the glyph legends; attach and detach notices
offer a working undo; restore drops invalid actions with a notice; both
quit paths behave per 11.3 and 12.1.
**Status:** Pending
