# 2026-11-04-tasks-full-rfc.md

**Status:** Active

## Context

Completing the RFC after the walking skeleton: sections, json contributions,
variables, updates, removal, reconciliation, and the remaining TUI. Design
decisions spanning the slices:

- Plan actions become self-contained: the planner receives source contents
  (`Input.Sources`, read by the caller) and emits actions with final content
  bytes; apply stops reading kit files and only writes. Validation before the
  first write (RFC 7.7) then covers fragments and templates too.
- The target snapshot gains the content of shared files; parsing (Markdown
  block layout, JSONC trees) is pure and lives with the planner-side packages.
- Variable substitution happens in the planner, so hashes recorded in the lock
  and content comparisons on update use the substituted content.
- Statuses grow beyond install/no-action/error as the slices need them
  (update, downgrade, warning, modified, conflict, orphan).

Spec: docs/rfc-penates.md; module rules: docs/architecture.md.

---

### TASK-01 `register-input-focus`
**Description:** Fix the register form: `textinput.Focus()` is never called, so
the form ignores typing and Enter silently accepts the prefilled `target_hint`.
Found by the tmux smoke test.
**Definition of done:** a headless test types a path into the form and confirms
Enter registers that exact target; the tmux flow also works.
**Status:** Done

### TASK-02 `section-model`
**Description:** `internal/target`: read shared files into a layout — managed
blocks (id between `penates:begin/end` markers) with content hashes, headings
outside managed blocks, existence.
**Definition of done:** parsing fixture shared files answers block ids, block
content hashes, outside headings; table tests pass.
**Status:** Done

### TASK-03 `section-planning`
**Description:** Planner implements RFC 7.3 for `section` contributions: append
when absent (create the shared file when missing), three-state evaluation when
present, the coarse case-insensitive duplicate-heading check against headings
outside managed blocks, block-id conflicts between selected items (7.5). Actions
carry final content; `Input.Sources` is introduced here.
**Definition of done:** table tests cover every 7.3 rule and the conflict; the
planner performs no I/O.
**Status:** Pending

### TASK-04 `section-apply`
**Description:** Apply writes section actions: append with a blank-line
separator at the end of the shared file, replace the content between markers
without touching anything else; the lock records `sections` and
`created_shared_files`.
**Definition of done:** applying a fixture kit writes the marked blocks into a
shared file and round-trips through the lock; tests pass.
**Status:** Pending

### TASK-05 `jsonc-package`
**Description:** `internal/jsonc`: a wrapper over hujson that reads a JSONC
file into a tree and edits it surgically — get, insert, replace, and remove
object members and array elements by RFC 6901 pointer — preserving comments and
formatting of untouched parts.
**Definition of done:** table tests cover edit-in-place with comments
preserved, pointers with special characters, and array-element operations.
**Status:** Pending

### TASK-06 `json-planning`
**Description:** Planner implements RFC 7.4: fragments decompose into leaf
values and array elements; per-location table (absent write, equal-not-owned
satisfied, equal-owned co-owner, different error, owned three-state); conflicts
between selected items on the same location (7.5).
**Definition of done:** table tests cover every 7.4 row and co-ownership; the
planner performs no I/O.
**Status:** Pending

### TASK-07 `json-apply`
**Description:** Apply merges json actions into shared files through
`internal/jsonc`, preserving comments and formatting; the lock records each
owned location with pointer and value hash; shared files created by penates are
recorded.
**Definition of done:** applying a fixture kit writes the values into a JSONC
file with comments of untouched parts intact and round-trips through the lock;
tests pass.
**Status:** Pending

### TASK-08 `variables`
**Description:** RFC 6.4 end to end: answers stored in the application state
and reused; the TUI requests missing required variables in a form before
planning; the planner substitutes `{{var}}` in `template: true` contributions,
errors on unknown placeholders, required-without-value makes the item
not installable; optional empty substitutes as empty; the lock records answers.
**Definition of done:** a headless TUI test answers a form and the installed
content carries the substitution; unknown-placeholder and missing-required
tests pass; answers reuse on a second apply without a form.
**Status:** Pending

### TASK-09 `updates`
**Description:** RFC 8 with the update rows of the 7.2 table: version
comparison against the lock (higher update, equal-content-equal no-action,
equal-content-differs warning, lower downgrade); update planning for owned
files, sections, and json locations under the three-state rules (user-modified
kept, modified-plus-changed-incoming conflict); explicit reset-to-kit (7.6)
planned only on user request; the TUI shows `1.2.0 > 1.3.0` style versions.
**Definition of done:** table tests cover the version table and every update
row for all three contribution kinds, plus reset; tests pass.
**Status:** Pending

### TASK-10 `removal`
**Description:** RFC 9: planning and applying removal of installed items —
owned files deleted when unchanged and kept-plus-reported when user-modified,
created directories removed when empty, sections deleted with their markers or
kept without markers when modified, json locations removed when this item is
the last owner, shared files created by penates deleted when left empty;
orphans and items of unavailable kits stay frozen but removable (9.2); the TUI
marks items for removal and the results report kept content.
**Definition of done:** table tests cover every 9.1 row for all three
contribution kinds plus the orphan flow; a headless TUI test removes an
installed item.
**Status:** Pending

### TASK-11 `reconciliation`
**Description:** RFC 7.7: before planning, reconcile the lock with the target
using recorded hashes — owned files whose content differs or that are missing
mark their item modified or broken, leftover `.penates-*` staged files from an
interrupted apply are removed, and the reconciliation feeds the planner and the
TUI statuses.
**Definition of done:** a test simulating an interrupted apply (staged temp
files, moved-but-unrecorded files) reconciles to the expected statuses and
clean target; tests pass.
**Status:** Pending

### TASK-12 `tui-list-polish`
**Description:** The RFC 10.2 list: kit group headers, the full status set
(update, downgrade, modified, conflict, orphan, kit unavailable, warning) with
selection and installed marks, `/` text filter over id, tags, and description,
and `i` details — description, contributions, changelog when present, source
kit, variable answers.
**Definition of done:** headless tests drive the filter and the details
screen; every status renders a distinct marker.
**Status:** Pending

### TASK-13 `apply-summary`
**Description:** RFC 10.2: before applying, a summary screen lists every change
per item and every excluded item with its reason; apply happens only after
confirmation.
**Definition of done:** a headless test sees the summary before the results and
can cancel out of it; tests pass.
**Status:** Pending

### TASK-14 `rfc-completeness-pass`
**Description:** Final pass over the RFC: walk sections 6–10 row by row against
the tests, close the gaps found, update `docs/architecture.md` (new packages)
and `CHANGELOG.md`, and archive this file.
**Definition of done:** a written checklist of every RFC rule with its covering
test or documented deviation; all tests, vet, and gofmt green.
**Status:** Pending
