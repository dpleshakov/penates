# RFC: penates — a kit installer for agent harnesses

| | |
|---|---|
| Status | Accepted |
| Date | 2026-11-04 |
| Scope | v1 design |

## 1. Summary

penates is a small cross-platform application with a terminal user interface (TUI). It installs, updates and removes harness content from one or more kit directories into the configuration directory of an agent harness, for example `~/.config/opencode`. Harness content includes skills, agent instructions, plugins and configuration fragments.

The design rests on four properties:

- A kit is a plain directory bound to one harness. Its unit is the **item**: a directory that holds everything one installable capability needs.
- Variation between machines is expressed by separate items and by per-item variables, not by conditional logic.
- Every file, Markdown block and JSON value that penates writes is recorded in a lock. Anything not recorded belongs to the user and is never overwritten or removed.
- Ordering of versions comes from semantic versions declared in the kit. Content hashes are used only to detect local modifications.

## 2. Context and problem

Agent harnesses such as OpenCode and Claude Code are customized with skills, instruction files, plugins and configuration. A user who maintains such customization across several machines faces two independent streams of change:

- the evolution of the shared customization itself;
- machine-specific adjustments: local paths, tools that are unavailable on a given machine, different harness versions.

When both streams live in the same files, keeping machines in sync requires repeated manual merges.

### 2.1 Requirements

1. Group several files and fragments into one installable unit.
2. Select in a TUI which units to install, without handling individual files.
3. Install units that are not yet installed.
4. Update outdated units, with a basis for the decision that a person can understand.
5. Keep alternative variants of a unit side by side and install the appropriate one.
6. Parameterize individual units where needed.
7. Combine content from several kits into one target.
8. Never destroy or silently alter configuration that the user already has.

### 2.2 Motivating cases (OpenCode)

| Case | Expression in penates |
|---|---|
| A skill references the local path to OpenCode sources; the path differs per machine | One item with a variable |
| On one machine `git` is blocked but `gh` is available | A separate item, selected by the user on that machine |
| A plugin exists in variants for incompatible OpenCode v1 and v2 | Two items, distinguished by tags |
| An npm plugin must be registered in `opencode.json` | A `json` contribution to a shared file |
| A rule must appear in `AGENTS.md`, which the user also edits | A `section` contribution to a shared file |

## 3. Goals and non-goals

### 3.1 Goals

- Install, update and remove items from local kit directories into global harness configuration directories.
- Protect user content through recorded ownership.
- Support several kits per target.
- Run on Windows, Linux and macOS as a single static binary.

### 3.2 Non-goals

- Obtaining kits. penates performs no network access and no version-control operations. How a kit reaches the machine (clone, archive, copy) is the owner's concern.
- Discovery of or distribution from public registries.
- Translation of content between harnesses.
- Detection of the operating system, installed harnesses or harness versions.
- Project-scoped targets, such as a `.opencode/` directory inside a repository.
- A command-line interface in v1. The architecture permits adding one later (section 10).

## 4. Principles

1. **Knowledge about the environment belongs to the kit author.** penates does not reason about operating systems, harnesses or their versions. Tags and directory layout are the author's responsibility.
2. **Variation is expressed by files, not by logic.** Two variants are two items. Duplicated content is explicit and searchable; conditional logic is neither.
3. **The application does not know any harness.** Harness specifics live in kit data: destination paths and the list of shared files.
4. **A kit source is a plain directory.**
5. **User content is never overwritten or removed silently.** Whatever is not recorded as owned by penates belongs to the user.
6. **Simple deterministic checks are preferred over policies.**
7. **Windows is a first-class platform.** Files are copied; symbolic links are not used.

## 5. Terminology

| Term | Meaning |
|---|---|
| Harness | An agent environment that consumes the installed content, for example OpenCode or Claude Code. |
| Kit | A directory containing `kit.yaml` and items for one harness. The source of truth for its items. |
| Item | A directory inside a kit containing `item.yaml` and all files the item contributes. The unit of selection, installation, versioning and atomicity. |
| Contribution | One declared effect of an item on the target: a whole file, a Markdown section or a set of JSON values. |
| Shared file | A target file that several items and the user may write to. Declared by the kit. |
| Target | A harness configuration directory into which items are installed. |
| Lock | The per-target record of installed items and of everything penates owns in the target. |
| Tag | A free-form label on an item, used only for display and filtering. |
| Variable | A per-item parameter whose value is requested at installation and substituted into templated contributions. |

## 6. Model

### 6.1 Kit

```
opencode-kit/
├── kit.yaml
└── items/
    ├── writing/
    │   ├── item.yaml
    │   └── skill/SKILL.md
    ├── done-v1/
    ├── done-v2/
    └── git-via-gh/
```

```yaml
# kit.yaml
name: personal-opencode          # unique among kits registered on a machine
description: Personal OpenCode customization
target_hint: ~/.config/opencode  # suggested target on first registration
shared:                          # files that section and json contributions may write to
  - AGENTS.md
  - opencode.json
```

### 6.2 Item

```yaml
# items/git-via-gh/item.yaml
id: git-via-gh                   # slug, unique within the kit
version: 1.2.0                   # semantic version, required
description: Use gh instead of git on machines where git is blocked
tags: [gh]
vars: []
contributes:
  - file: skill/SKILL.md
    to: skills/git-via-gh/SKILL.md
  - section: agents-note.md
    to: AGENTS.md
  - json: permissions.json
    to: opencode.json
```

Rules:

- `id` matches `^[a-z0-9][a-z0-9-]*$` and is unique within the kit.
- The identity of an item is the pair (kit name, item id). Items with the same id in different kits are distinct items.
- `version` is a semantic version and is required.
- Source paths are relative to the item directory. `to` paths are relative to the target root, use forward slashes, and must not be absolute or contain `..`.
- An optional `CHANGELOG.md` in the item directory is displayed in the item details.

### 6.3 Contribution kinds

| Kind | Source | Effect on the target |
|---|---|---|
| `file` | A file or a directory | The file, or each file of the directory, is owned entirely by the item. A `file` contribution may not target a shared file. |
| `section` | A Markdown file | A block delimited by markers is placed into a shared Markdown file. |
| `json` | A JSON or JSONC file | The fragment's values are placed into a shared JSON or JSONC file. |

`section` and `json` contributions may target only files listed in `shared` of the item's kit.

### 6.4 Variables

```yaml
vars:
  - id: opencode_source_path
    prompt: Path to the local OpenCode source tree
    required: true
```

- A contribution opts into substitution with `template: true`. Files without this flag are copied verbatim, so content that legitimately contains `{{` is not affected.
- Substitution is plain replacement of `{{var_id}}`. There are no conditionals, loops or expressions.
- An unknown placeholder in a templated contribution is an error.
- A required variable without a value makes the item not installable, with an explanation. An optional variable without a value is substituted as an empty string.
- Answers are stored in the application state and reused. On update, a newly introduced variable is requested; the answer for a removed variable is discarded.
- Secrets are not supported as variables and are never stored. Secrets are supplied to the harness by its own means, such as environment variables.

### 6.5 Tags and variants

Tags have no semantics in penates, now or in future versions. They are displayed and can be used in the text filter. If automatic matching of items to machines is ever introduced, it will use a new field and will not reinterpret tags.

Variants of one capability are separate items, for example `done-v1` and `done-v2`. Mutual exclusion is not declared; it follows from ownership. If item Y conflicts only with contributions owned by installed item X, the TUI offers to replace X with Y.

### 6.6 Targets

v1 supports global harness configuration directories only. A target is registered on first use of a kit: the user supplies the kit path, penates proposes `target_hint`, and the user confirms or edits it.

### 6.7 Application state

All state is kept in the penates data directory (`os.UserConfigDir()/penates`). Nothing is written into a target other than the contributions themselves.

- **Configuration:** registered kits with their paths, registered targets, the mapping of kits to targets, and the most recent selection per target.
- **Lock:** one file per target, keyed by the target path.

The lock records, per installed item:

- kit name, item id and installed version;
- variable answers;
- for each `file` contribution: every installed file with its SHA-256, and every directory penates created;
- for each `section` contribution: the shared file, the block identifier and the SHA-256 of the block content;
- for each `json` contribution: the shared file and each owned location (RFC 6901 JSON Pointer for object members; pointer to the array plus the element value for array elements) with the SHA-256 of the written value;
- shared files that penates created.

```json
{
  "target": "~/.config/opencode",
  "items": {
    "personal-opencode/git-via-gh": {
      "version": "1.2.0",
      "answers": {},
      "files": {
        "skills/git-via-gh/SKILL.md": "sha256:9ab2…"
      },
      "dirs_created": ["skills/git-via-gh"],
      "sections": {
        "AGENTS.md": { "block": "personal-opencode/git-via-gh", "hash": "sha256:41c0…" }
      },
      "json": {
        "opencode.json": [
          { "pointer": "/permission/bash/gh *", "hash": "sha256:77de…" }
        ]
      }
    }
  },
  "created_shared_files": ["AGENTS.md"]
}
```

The lock is self-sufficient: removal and modification detection work when the source kit is unavailable.

## 7. Ownership and installation rules

### 7.1 Ownership

A file, Markdown block or JSON location is owned by penates if and only if the lock records it for an item. Everything else belongs to the user. Ownership is the sole basis for any write or deletion.

Each owned element is evaluated against three states: the content recorded at installation (base), the current content in the target, and the content the kit now provides (incoming). This is the conflict model used by Debian packaging for configuration files.

### 7.2 `file` contributions

| Target state | Action |
|---|---|
| Absent | Write; record the hash. |
| Owned by this item, unchanged since installation | Replace with incoming content when the item is updated. |
| Owned by this item, modified by the user; incoming equals base | Keep the user's content without notice. |
| Owned by this item, modified by the user; incoming differs from base | Do not write. The item is reported as in conflict. |
| Present and not recorded in the lock | Error. The file is never written. |
| Owned by another item | Error naming the owner. |

Directory sources are processed file by file under the same rules.

### 7.3 `section` contributions

A block has the form:

```markdown
<!-- penates:begin personal-opencode/git-via-gh -->
...fragment content...
<!-- penates:end personal-opencode/git-via-gh -->
```

- If the block is absent, it is appended at the end of the shared file, separated by a blank line. If the shared file is absent, it is created and recorded as created by penates.
- If the block is present, it is evaluated by the three-state rules of 7.2, using the hash of the content between the markers.
- If the first heading of the fragment matches, case-insensitively, a heading outside managed blocks, installation fails with an error. The check is intentionally coarse: it prevents duplicate instructions that the user has already written by hand.
- Text outside managed blocks is never modified.

### 7.4 `json` contributions

The fragment is decomposed into leaf values and array elements.

| Target state at a location | Action |
|---|---|
| Absent | Write; the location is owned by the item. |
| Present with an equal value, not owned | Treat as satisfied. The location is not owned and is never removed by penates. |
| Present with an equal value, owned by another item | Add this item as a co-owner. The value is removed only when the last owner is removed. |
| Present with a different value, not owned by this item | Error. Nothing is written. |
| Owned by this item | Evaluated by the three-state rules of 7.2. |

- Arrays are treated as sets. A missing element is appended and owned; an existing equal element is satisfied.
- JSONC is supported. Comments and formatting of untouched parts of the file are preserved; only the affected values are inserted, changed or removed.

### 7.5 Conflicts between items and kits

Two items that claim the same file, the same block identifier, or the same JSON location with different values are in conflict, regardless of whether they come from the same kit. Conflicts are reported as errors during planning. There is no precedence between kits.

### 7.6 Explicit reset

For an item in conflict or with modified content, the user may request **reset to kit**. Reset replaces the item's owned content with the incoming content. It is never performed as part of a regular update.

### 7.7 Atomicity

- A plan validates every contribution of every selected item before the first write.
- The item is the unit of atomicity: it is applied completely or not at all.
- An item that fails validation is excluded from the plan and reported with its reason; other items proceed.
- The lock records only completely applied items. After an interruption, penates reconciles the lock with the target using recorded hashes before planning.

## 8. Versions and updates

The version declared in `item.yaml` is the only source of ordering. Version-control metadata is not used: a kit may arrive as an archive without such metadata, and a stale copy must not be mistaken for an update.

| Kit version relative to installed | Behavior |
|---|---|
| Higher | Update available. |
| Equal, content equal | No action. |
| Equal, content differs | Warning: content changed without a version change. No update is offered. |
| Lower | Shown as a downgrade. Never selected by default. |

Version conventions for authors:

- **major:** behavior changes, for example removed steps or a rewritten workflow;
- **minor:** added capabilities or sections;
- **patch:** wording and corrections.

The TUI marks major updates for review and displays `CHANGELOG.md` when present.

Each installed item is pinned to its source kit. Updates come only from that kit. An item with the same id in another kit is a different item and never replaces the installed one.

## 9. Removal and orphans

### 9.1 Removal

| Element | Unchanged | Modified by the user |
|---|---|---|
| Owned file | Deleted | Kept; removed from the lock; reported |
| Directory created by penates | Deleted when empty | Kept while it contains files |
| Owned section | Deleted together with its markers | Content kept without markers; reported |
| Owned JSON location | Removed if this item is the last owner | Kept; removed from the lock; reported |

Files that are not recorded in the lock, including files the user added inside an item's directory, are never touched. A shared file created by penates is deleted when it contains no content after removal.

### 9.2 Orphans and unavailable kits

An installed item that no longer exists in its kit is an orphan. An orphan remains managed and frozen: it is marked as removed from the kit, receives no updates, and can be removed by the user. All items of a kit that has become unavailable receive the same frozen status.

## 10. User experience

### 10.1 First run

1. The user starts penates with a kit path.
2. penates reads `kit.yaml` and proposes `target_hint` as the target.
3. The user confirms or edits the target; the kit and target are registered.

### 10.2 Main screen

```
 penates   target ~/.config/opencode   kits: personal-opencode, team-opencode

 -- personal-opencode   ~/kits/opencode
 [installed]   writing                   1.0.0          Text editing skill
 [update]      done-v2     opencode-v2   1.2.0 > 1.3.0  Marks finished sessions
 [conflict]    git-via-gh  gh            1.2.0          AGENTS.md: heading exists
 [ ]           done-v1     opencode-v1   1.0.0          Marks finished sessions

 -- team-opencode   ~/kits/team
 [selected]    review                    0.3.0          Review agent with team checklist
 [orphan]      old-lint                  0.2.0          Removed from kit

 /  filter   space  select   i  details   a  apply   q  quit
```

- Statuses: not installed, selected, installed, update available, downgrade available, modified, conflict, orphan, kit unavailable, warning.
- `/` filters by id, tags and description.
- `i` shows the description, contributions, changelog, source kit and variable answers.
- Forms request variables; answers are remembered.
- On a later run, the previous selection is offered for restoration.
- Before applying, a summary lists every change per item and every excluded item with its reason.

## 11. Architecture

- **Core.** Given registered kits, the lock and a snapshot of the target, the core produces a plan: a list of actions per item, each with a reason, and a list of errors. The core applies a plan under the rules of section 7.7. The core has no knowledge of any harness.
- **Clients.** The TUI is the only client in v1. A command-line client may be added later on the same core without changes to it.
- **Distribution form.** A single static binary written in Go.

## 12. Alternatives

| Tool or class | Primary axis of variation | Reason it does not satisfy the requirements |
|---|---|---|
| chezmoi | Machine | Variation is expressed through template conditions in file names and contents, contrary to principle 2. No interactive selection of subsets and no versioned updates of units. |
| Microsoft APM | Project and team | A dependency manager with a lockfile and drift detection. Deploys recognized primitive types only (no arbitrary files such as OpenCode plugins) and has no per-machine variables at installation. |
| OpenPackage, neoskills, tackroom | Harness | Distribute one source to many harnesses with format translation. Machine variation is not the primary concern. |
| skills CLI, skpm, skills-installer | Source | Install skills from public repositories. Configuration, plugins and per-machine parameters are outside their scope. |
| File synchronization (Syncthing and similar) | None | Mirror files as they are; no layer for variation or ownership. |

penates addresses the machine axis: one kit, several machines, each with its own selection and parameters. Kits may contain content obtained through the tools above.

## 13. Rejected decisions

| Decision | Reason for rejection |
|---|---|
| Machine-matched labels with automatic variant resolution | Introduces resolution logic. When the kit author and the user are the same person, tags interpreted by the user are sufficient. |
| Built-in vocabulary and automatic detection of harnesses and their versions | Version detection is unreliable; detection code per harness reintroduces harness knowledge into the application; the kit format would depend on application releases; a target is not equivalent to an installed harness. |
| Translation of content between harnesses | Plugins, hooks and configuration are not portable; translation concentrates complexity where it cannot succeed. Portable content is duplicated across kits by the author. |
| Ordering by content hash or version-control commit | Neither provides order. A stale kit copy would replace newer content; kits may lack version-control metadata. |
| Owning entire shared configuration files | Destroys configuration the user already has. |
| Last-writer-wins merge of configuration fragments | Arrays are replaced and values are lost without notice. |
| Shadowing of items across kits by higher version | Another kit could replace an installed item without notice. Source pinning applies instead. |
| UUID item identifiers | Copying an item directory to create a variant duplicates the identifier. A slug qualified by kit name is sufficient. |
| Built-in retrieval of kits (git, archives) | Delivery of the kit is the owner's concern. |
| Template logic (conditionals, loops) | Contradicts principle 2. |
| Storing state inside the target directory | The target directory holds only installed content. |
| Project-scoped targets in v1 | Project configuration is shared through the project repository and belongs to tooling for that axis. |

## 14. Open questions

1. **Name availability.** Manual verification on GitHub, npm, Homebrew and pkg.go.dev before the repository is created.
2. **Shared file name variants.** A harness may accept several names for the same file, for example `opencode.json` and `opencode.jsonc`. The kit currently declares a single name.
3. **Section duplicate check.** The heading comparison in 7.3 may require refinement after practical use.
4. **Command-line client.** The command set is defined when a concrete need arises.
5. **Distribution channels and license.**

## Appendix A. Name

Penates are the Roman deities of the household storeroom; each household has its own. The name is independent of any particular harness.
