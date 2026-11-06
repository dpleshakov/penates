# RFC completeness checklist — 2026-11-06

The row-by-row walk of RFC sections 6–10 against the tests, closed at the end
of the full-RFC board (TASK-14 of `docs/archive/2026-11-04-tasks-full-rfc.md`).
Every rule of those sections names the test that covers it or the documented
deviation that takes its place. The walk found five gaps, closed in the same
pass: the json rules of 6.3 were tested only through their section siblings,
the update-introduced variable request of 6.4 had no test, the removal report
of 9.1 was never pinned through the results screen, RFC 8's major-update mark
was not implemented, and the `broken` status of 7.7 was missing from the
RFC 10.2 list.

This file is a snapshot of the shipped state. Later changes update the RFC and
the tests; this checklist records what the board shipped.

## Section 6 — Model

### 6.1 Kit

| Rule | Covered by |
|---|---|
| `kit.yaml` fields: name, description, target_hint, shared | `kit.TestLoadValid` |
| `items/` holds the item directories; a kit without items is rejected | `kit.TestLoadInvalid` ("missing items directory", "no items") |
| The kit name is unique among registered kits | structural: `state.State.Kits` is keyed by name (`state_test.TestRoundTrip`); re-registering a name replaces its path |

### 6.2 Item

| Rule | Covered by |
|---|---|
| `id` matches `^[a-z0-9][a-z0-9-]*$` | `kit.TestLoadInvalid` ("bad item id"); the kit name follows the same rule ("name must be a token") |
| `id` unique within the kit | `kit.TestLoadInvalid` ("duplicate item id") |
| Identity is the pair (kit name, item id) | `kit.TestLoadValid` (`ItemKey`); cross-kit claims conflict without precedence (`plan.TestPlanSections` "file and section claims on one path conflict both ways") |
| `version` is a required full semantic version | `kit.TestLoadInvalid` ("missing version", "version not semantic") |
| Source paths are relative to the item directory | `kit.TestLoadInvalid` ("source path with dotdot"), `kit.TestLoadSources` |
| `to` paths: relative, forward slashes, never absolute, no `..` | `kit.TestLoadInvalid` ("absolute to", "to with dotdot element", "to with backslash"); re-checked at the file-system boundary (`target.TestSnapshotRejectsBadPaths`) |
| An optional `CHANGELOG.md` shows in the item details | `tui.TestItemDetails` (with and without one) |

### 6.3 Contribution kinds

| Rule | Covered by |
|---|---|
| `file`: the file — or each file of a directory source — is owned entirely; directory sources expand per file | `kit.TestLoadSources` ("directory file source expands to one contribution per file", "empty directory source"), `plan.TestBuild` ("installs into absent files") |
| A `file` contribution may not target a shared file | `kit.TestLoadInvalid` ("file contribution targets shared file") |
| `section`: a marked block into a shared Markdown file | `plan.TestPlanSections`, `markdown.TestParse`, `apply.TestInstallSectionEndToEnd` |
| `json`: values into a shared JSON or JSONC file | `plan.TestPlanJSON`, `apply.TestInstallJSONEndToEnd` |
| `section` and `json` target only files listed in `shared` | `kit.TestLoadInvalid` ("section contribution outside shared", "json contribution outside shared") |
| Only `file` sources may be directories | `kit.TestLoadSources` ("section source may not be a directory", "json source may not be a directory") |

### 6.4 Variables

| Rule | Covered by |
|---|---|
| Substitution only where `template: true`; plain files keep `{{` verbatim | `plan.TestPlanVars` ("a plain contribution keeps its braces") |
| Substitution is plain `{{var_id}}` replacement; an answer is never rescanned | `plan.TestPlanVars` ("an answer is substituted once, not rescanned") |
| An unknown placeholder is an error | `plan.TestPlanVars` ("an unknown placeholder is an error") |
| Required without a value: not installable, with an explanation | `plan.TestPlanVars` ("a required variable without a value is not installable"), `tui.TestApplySummaryExcluded`, `apply.TestInstallEndToEnd` (first pass) |
| Optional without a value substitutes as empty | `plan.TestPlanVars` ("an optional variable without a value substitutes as empty") |
| Answers are stored and reused; no second form on the next run | `tui.TestRegisterListApplyFlow`, `apply.TestInstallEndToEnd`. They are stored in the lock, per RFC 6.7 ("variable answers" among the lock's per-item records); RFC 6.4's "application state" reads through 6.7 |
| An update may introduce a variable; it is requested | `plan.TestMissingVarsOnUpdate`; the TUI asks before the summary (`tui.beginApply` → `plan.MissingVars`, driven by `tui.TestRegisterListApplyFlow`) |
| The answer of a removed variable is discarded | `plan.TestResolveVars` |
| Secrets are not supported and never stored | documented (RFC 6.4): no mechanism exists to test; nothing in the state or lock format holds one |

### 6.5 Tags and variants

| Rule | Covered by |
|---|---|
| Tags are display and filter only, no semantics | `tui.TestListFilter` (tag match), `tui.TestItemDetails` (tags line); no code reads them for anything else |
| Variants are separate items; no declared exclusion | structural; ownership decides — the replacement flow below |
| If Y conflicts only with content owned by installed X, the TUI offers to replace X with Y | **deviation:** there is no dedicated offer; the user marks X for removal (`d`) and selects Y in the same apply — pinned by `tui.TestRemoveThenInstallSameFile` |

### 6.6 Targets

| Rule | Covered by |
|---|---|
| Registered on first use; `target_hint` proposed; the user confirms or edits | `tui.TestRegisterListApplyFlow` (confirm), `tui.TestRegisterFormTyping` (edit) |
| v1 supports global configuration directories only | **deviation:** penates never verifies that a target is a global harness directory; the confirmed path is used as given (principle 3: no harness knowledge) |

### 6.7 Application state

| Rule | Covered by |
|---|---|
| State lives under `os.UserConfigDir()/penates`; nothing else is written into a target beyond the contributions | `state.DefaultDir` wires it (`cmd/penates`); the stores take an injected directory (`state_test`, `lock_test`); apply writes only action paths (`apply.TestApplyFailureLeavesNothing`, `apply.TestRemoveExecutesThePlan`) |
| Configuration: kits, targets, kit-to-target mapping, last selection | `state_test.TestRoundTrip`, `state_test.TestQueries` |
| One lock per target, keyed by the target path | `lock.TestRoundTrip`, `lock.TestLoadMissing` |
| The lock records version, answers, files with SHA-256, created directories, sections, json locations, created shared files | `lock.TestRoundTrip` (whole shape), recording per kind: `apply.TestApplyInstalls`, `apply.TestApplyInstallsJSON`, `apply.TestInstallSectionEndToEnd` |
| The lock is self-sufficient: removal and modification detection without the kit | `plan.TestBuildRemoval` ("an orphan of a kit that is gone removes from the lock alone"), `tui.TestOrphanRemoval`, `plan.TestReconcile` (pure lock + snapshot) |

## Section 7 — Ownership and installation rules

### 7.1 Ownership

| Rule | Covered by |
|---|---|
| Owned iff the lock records it; everything else is never overwritten or removed | `plan.TestBuild` ("present and unowned file is never written"), `plan.TestPlanSections` ("unowned block in the target is never touched"), `plan.TestPlanJSON` ("a different unowned value is an error"), `plan.TestBuildRemoval` ("a file the user added beside the owned ones is never planned", "a shared file the user wrote survives empty") |
| Three states: base, current, incoming | `plan.TestOwnedAction` (all four rows); per kind: `plan.TestPlanFileUpdates`, `plan.TestPlanSectionUpdates`, `plan.TestPlanJSONUpdates` |

### 7.2 `file` contributions

| Row | Covered by |
|---|---|
| Absent → write, record the hash | `plan.TestBuild` ("installs into absent files"), `apply.TestApplyInstalls` |
| Owned, unchanged; update → replace | `plan.TestPlanFileUpdates` ("unchanged file is replaced by the update"), `tui.TestUpdateAndResetFlow` |
| Owned, modified; incoming equals base → keep without notice | `plan.TestPlanFileUpdates` ("user modification with equal incoming is kept"), `tui.TestUpdateAndResetFlow` (user change survives an apply) |
| Owned, modified; incoming differs → no write, conflict | `plan.TestPlanFileUpdates` ("user modification with different incoming is a conflict") |
| Present, not recorded → error, never written | `plan.TestBuild` ("present and unowned file is never written") |
| Owned by another item → error naming the owner | `plan.TestBuild` ("file owned by another installed item") |
| Directory sources run file by file under the same rules | `kit.TestLoadSources`, `plan.TestBuild` (two-file item) |

### 7.3 `section` contributions

| Rule | Covered by |
|---|---|
| Block form: `penates:begin/end` markers with the block id | `markdown.TestParse`, `markdown.TestAppend` |
| Absent → appended at the end, separated by a blank line | `plan.TestPlanSections` ("existing shared file keeps its content and gains the block"), `markdown.TestAppend` |
| Shared file absent → created and recorded as created by penates | `plan.TestPlanSections` ("absent shared file is created with the block"), `apply.TestInstallSectionEndToEnd` (lock records the created file) |
| Present → three-state on the hash between the markers | `plan.TestPlanSectionUpdates` (all rows); drift: `plan.TestReconcile` ("a changed block marks the item modified", "a removed block breaks the item") |
| Duplicate first heading, case-insensitive, outside managed blocks → installation fails | `plan.TestPlanSections` ("duplicate heading outside managed blocks"), `markdown.TestFirstHeading`; inside blocks it does not fire ("a heading inside a planned block does not block the next item") |
| Text outside managed blocks is never modified | `markdown.TestReplace`, `markdown.TestRemove`, `markdown.TestStrip` (byte-exact outside the block) |
| One item owns at most one block per shared file | `plan.TestPlanSections` ("one item may not write two sections into one file"); the block id is the item key, so this follows from the marker scheme |

### 7.4 `json` contributions

| Rule | Covered by |
|---|---|
| The fragment decomposes into member leaves and array elements | `plan.walkJSON`; observed through the location sets of `plan.TestPlanJSON` ("a nested subtree is inserted at the highest missing ancestor") |
| Absent → write; the location is owned | `plan.TestPlanJSON` ("absent member is inserted and owned, untouched bytes survive", "missing array is inserted whole and its elements owned", "an empty shared file is filled with the fragment") |
| Present, equal, not owned → satisfied, never owned, never removed | `plan.TestPlanJSON` ("equal unowned value is satisfied without owning it") |
| Present, equal, owned by another → co-owner; removed only when the last owner is removed | `plan.TestPlanJSON` ("equal value owned by another item makes the item a co-owner", "array element owned by another item is co-owned"), `apply.TestApplyRecordsCoOwnershipWithoutWrites`, `plan.TestBuildRemoval` ("a co-owned value stays while the record dies alone") |
| Present, different, not owned by this item → error, nothing written | `plan.TestPlanJSON` ("a different unowned value is an error", "an object cannot be placed where a scalar sits", "root of a different kind is a different value") |
| Owned by this item → three-state of 7.2 | `plan.TestPlanJSONUpdates` (all rows + reset) |
| Arrays are sets: a missing element is appended and owned; an existing equal element is satisfied | `plan.TestPlanJSON` ("array elements are a set: missing appended and owned, equal satisfied") |
| The set rule governs array locations; the three-state table applies to member locations | documented reading (RFC 7.4's set sentences): an element is atomic — the user's reshaping of an array reads as removal and addition, and the next apply re-appends the missing element (`plan.TestReconcile` "a gone array element breaks the item") |
| JSONC: comments and formatting of untouched parts survive; only affected values change | `jsonc.TestRoundTrip`, `jsonc.TestInsertObject`, `jsonc.TestInsertArray`, `jsonc.TestReplace`, `jsonc.TestRemove`, `jsonc.TestSequence`; through the planner: `plan.TestPlanJSON` ("untouched bytes survive", "comments inside an inserted subtree survive"), `apply.TestInstallJSONEndToEnd` |

### 7.5 Conflicts between items and kits

| Rule | Covered by |
|---|---|
| The same file claimed twice → conflict, both sides | `plan.TestBuild` ("two items claiming the same file conflict") |
| A file and a section (or json) claiming one path → conflict both ways | `plan.TestPlanSections` ("file and section claims on one path conflict both ways"), `plan.TestPlanJSON` ("json and section contributions cannot share one file") |
| The same JSON location with different values → conflict; equal values → shared | `plan.TestPlanJSON` ("two items claiming one location with different values conflict both ways", "two items claiming one location with equal values share it") |
| No precedence between kits | the two-kit conflict cases above (`k1`/`k2`) |
| Two items claiming the same block identifier | unreachable: the block identifier is the item key, so two items cannot claim one block; the foreign-owned block errors instead (`plan.TestPlanSections` "block owned by another installed item") |

### 7.6 Explicit reset

| Rule | Covered by |
|---|---|
| Reset replaces owned content with incoming, on user request only, never part of a regular update | `plan.TestPlanFileUpdates` / `TestPlanSectionUpdates` / `TestPlanJSONUpdates` ("reset overwrites the user's modification"); the TUI toggles it per row with `r` and clears it after the apply (`tui.TestUpdateAndResetFlow`) |

### 7.7 Atomicity

| Rule | Covered by |
|---|---|
| Every contribution validates before the first write | the planner excludes with reasons before apply runs (`plan.TestBuild` "a failing item does not block another"); apply stages everything before the first rename (`apply.TestApplyFailureLeavesNothing`) |
| The item is the unit of atomicity | `apply.TestApplyFailureLeavesNothing`, `apply.TestFailureOfOneItemDoesNotBlockTheNext` |
| A failing item is excluded with its reason; others proceed | the tests above; the reason reaches the user (`tui.TestApplySummaryExcluded`) |
| The lock records only completely applied items; after an interruption the lock is reconciled by recorded hashes | `apply.TestApplyFailureLeavesNothing` (no lock entry), `apply.TestReconcileAfterInterruption`, `tui.TestReconciliationFlow`, `plan.TestReconcile`, `plan.TestPlanRepairsBrokenItems`, `plan.TestPlanModifiedNoAction` |

## Section 8 — Versions and updates

| Rule | Covered by |
|---|---|
| The version in `item.yaml` is the only ordering | `kit.TestLoadInvalid` ("version not semantic"), `kit.CompareVersions`, `plan.TestPlanUpdateVersions` |
| Higher → update available | `plan.TestPlanUpdateVersions` ("higher version updates"), `tui.TestListStatuses` (update marker and `1.0.0 > 1.1.0`) |
| Equal, content equal → no action | `plan.TestPlanUpdateVersions` ("equal version with equal content needs no action") |
| Equal, content differs → warning, no update offered | `plan.TestPlanUpdateVersions` ("equal version with changed content warns"), `tui.TestListStatuses` (warning marker) |
| Lower → shown as a downgrade, never selected by default | `plan.TestPlanUpdateVersions` ("lower version plans a downgrade"), `tui.TestListStatuses` (downgrade marker and arrow); nothing is ever auto-selected — a downgrade plans only when the user selects it |
| Major updates are marked for review | `tui.TestListStatuses` (major leg: `row.major` with the update marker and the arrow; the marker renders in the error style) — closed by this pass; the gap was that every update rendered alike |
| `CHANGELOG.md` displayed when present | `tui.TestItemDetails` |
| Updates come only from the pinned kit; same id in another kit never replaces | structural: planning indexes items by (kit, id) and the lock keys records the same way; `plan.TestBuild` ("file owned by another installed item") refuses the foreign owner |
| Ownership never ends but with removal: content the plan keeps standing — kept modifications and undeclared contributions — stays recorded | `apply.TestApplyKeepsRecordedContent` (files, blocks, directories), `plan.TestPlanJSONUpdates` ("a location the update drops stays owned with its value standing", "an array element the update drops stays owned"), `plan.TestPlanJSONContributionDropped`; documented in RFC 8 since this pass (before it, json released what files and sections kept) |

## Section 9 — Removal and orphans

### 9.1 Removal

| Row | Covered by |
|---|---|
| Owned file, unchanged → deleted | `plan.TestBuildRemoval` ("unchanged owned file is deleted"), `apply.TestRemoveExecutesThePlan`, `tui.TestRemoveFlow` |
| Owned file, modified → kept, removed from the lock, reported | `plan.TestBuildRemoval` ("modified owned file is kept and reported"), the report reaches the user (`tui.TestRemoveReportsKeptContent`) |
| Directory created by penates → deleted when empty, kept while it holds files | `apply.TestRemoveExecutesThePlan` (emptied directory gone, shared parent kept, the user's file stays) |
| Owned section → deleted with its markers / content kept without markers, reported | `plan.TestBuildRemoval` ("unchanged owned block is deleted with its markers", "modified block keeps its content without markers and is reported"), `markdown.TestStrip` |
| Owned json location → removed if the last owner; otherwise kept, removed from the lock, reported | `plan.TestBuildRemoval` ("json value of the last owner is removed", "an array element is removed by the value it holds", "a co-owned value stays while the record dies alone", "a modified json value is kept and reported") |
| A location that no longer holds the recorded value is kept and reported, whatever stands there | `plan.TestBuildRemoval` ("an array element the user replaced is kept and reported", "an array element the user removed is kept and reported") — closed by this pass; before it, reshaped arrays passed silently |
| The (pointer, hash) record cannot distinguish a member from an array element: an array holding the recorded value reads as the element standing, and that element is removed | `plan.TestBuildRemoval` ("a scalar the user replaced with an array holding it loses the value") — pinned reading of the RFC 6.7 record shape |
| Files not in the lock are never touched | `plan.TestBuildRemoval` ("a file the user added beside the owned ones is never planned", "owned file that is gone already dies silently", "a block the user deleted already plans nothing", "a json value that is gone already plans nothing") |
| A shared file penates created is deleted when the removal leaves it without content | `plan.TestBuildRemoval` ("a created shared file left without content is deleted", "a shared file the user wrote survives empty"), `apply.TestRemoveEndToEnd` |
| Removals run before the installs of one apply; installs plan from the target as removals leave it | `tui.TestRemoveThenInstallSameFile`, `tui.TestRemoveWithFormResume` |

### 9.2 Orphans and unavailable kits

| Rule | Covered by |
|---|---|
| An orphan stays managed and frozen: marked, no updates, removable | `tui.TestOrphanRemoval`, `tui.TestListStatuses` (orphan leg), `plan.TestBuildRemoval` ("an orphan of a kit that is gone removes from the lock alone") |
| All items of an unavailable kit get the same frozen status | `tui.TestListStatuses` (kit unavailable leg) |

## Section 10 — User experience

### 10.1 First run

| Rule | Covered by |
|---|---|
| Started with a kit path; `kit.yaml` read; `target_hint` proposed | `tui.TestRegisterListApplyFlow` |
| The user confirms or edits; kit and target register | the tests above (`tui.TestRegisterFormTyping` for the edit) |

### 10.2 Main screen

| Rule | Covered by |
|---|---|
| Header with target and kit names; kit group headers with directories | `tui.TestKitGroups` |
| Statuses: not installed, selected, installed, update, downgrade, modified, broken, conflict, orphan, kit unavailable, warning — each its own marker | `tui.TestListStatuses` (every one); `broken` joined the RFC list this pass — RFC 7.7 already spoke of it, 10.2 did not |
| `/` filters by id, tags, description | `tui.TestListFilter` |
| `i` shows description, contributions, changelog, source kit, answers | `tui.TestItemDetails` |
| Forms request variables; answers are remembered | `tui.TestRegisterListApplyFlow` |
| On a later run the previous selection is offered for restoration | **deviation:** the previous selection is restored as the active selection on entry, not offered as a proposal; deselecting is one key (`tui.TestUpdateAndResetFlow`, `tui.TestListStatuses` "held selection") |
| Version column `1.2.0 > 1.3.0` | `tui.TestUpdateAndResetFlow`, `tui.TestListStatuses` |
| Before applying, a summary lists every change per item and every excluded item with its reason; removals join the same summary; apply runs only after confirmation | `tui.TestApplySummary`, `tui.TestApplySummaryExcluded`, `tui.TestRemoveWithFormResume` |
| An install that writes no byte still names the locations it takes ownership of | `tui.renderSummary`; the underlying co-ownership install is pinned by `apply.TestApplyRecordsCoOwnershipWithoutWrites` |
