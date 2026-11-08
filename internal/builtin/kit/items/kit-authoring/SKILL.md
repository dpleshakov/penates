---
name: penates-kit-authoring
description: Create, fill, and maintain a penates kit repository — the directory skeleton, kit.yaml and item.yaml schemas, the three contribution kinds, variables, tags and variants, versioning conventions, and how penates treats the content on install, update, and removal.
metadata:
  author: The penates project
  version: 0.1.0
---

# penates kit authoring

A kit is a plain directory bound to one harness; its unit is the item — a
directory holding everything one installable capability needs. Variation
between machines is expressed by separate items and per-item variables,
never by conditional logic. Duplicate content is explicit and searchable;
templates substitute variables only.

## Skeleton

```
opencode-kit/
├── kit.yaml
└── items/
    ├── writing/
    │   ├── item.yaml
    │   └── skill/SKILL.md
    ├── done-v1/
    └── done-v2/
```

## kit.yaml

```yaml
name: personal-opencode          # unique among registered kits; ^[a-z0-9][a-z0-9-]*$
description: Personal OpenCode customization
target_hint: ~/.config/opencode  # proposed as the target on first registration
shared:                          # files section and json contributions may write to
  - AGENTS.md
  - opencode.json
```

## item.yaml

```yaml
id: git-via-gh                   # slug, unique within the kit
version: 1.2.0                   # semver, required
description: Use gh instead of git where git is blocked
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

Source paths are relative to the item directory. `to` paths are relative
to the target root, forward-slashed, never absolute, never containing
`..`. An optional `CHANGELOG.md` in the item directory is shown in the
item details and on major updates.

## Contribution kinds

- **file** — a file or a directory; every file lands in the target owned
  entirely by the item. A `file` contribution may not target a shared
  file.
- **section** — a Markdown file; its content is placed into a shared
  Markdown file inside block markers:

  ```
  <!-- penates:begin kit-name/item-id -->
  ...fragment content...
  <!-- penates:end kit-name/item-id -->
  ```

  A missing block is appended
  at the end of the shared file, which is created when absent; text
  outside managed blocks is never modified. The fragment's first heading
  must not match a heading the user already has outside managed blocks —
  installation refuses with a `heading exists` error.

- **json** — a JSON or JSONC fragment; its leaf values and array elements
  are placed into a shared JSON or JSONC file at the same locations
  (object members by path, array elements as a set: missing elements are
  appended, equal ones count as satisfied). Comments and formatting of
  untouched parts are preserved.

`section` and `json` contributions may target only files listed in the
kit's `shared`.

## Variables

```yaml
vars:
  - id: opencode_source_path
    prompt: Path to the local OpenCode source tree
    required: true
```

A contribution opts into substitution with `template: true`; files without
it are copied verbatim, so content that legitimately contains `{{` is
safe. Substitution is plain replacement of `{{var_id}}` — no conditionals,
no loops; an unknown placeholder in a templated contribution is an error.
Answers are asked at selection, stored in the lock, and reused; a new
variable introduced by an update is asked, a removed one's answer is
discarded. Secrets are not variables: supply them to the harness by its
own means.

## Tags and variants

Tags carry no semantics — display and filtering only. Variants of one
capability are separate items (`done-v1`, `done-v2`); mutual exclusion
follows from ownership, not from declarations. Replacing variant X with Y
is the user's action: mark X for removal and select Y in the same apply.

## Versioning

`item.yaml`'s semver is the only ordering; conventions:

- **major** — behavior changes: removed steps, a rewritten workflow;
- **minor** — added capabilities or sections;
- **patch** — wording and corrections.

Content changed without a version change reads as a warning, and no update
is offered — bump the version in the same change as the content. Major
updates are flagged for review and show `CHANGELOG.md` when present; give
each entry one user-visible line.

## How penates treats the content

On install, everything the item declares is written and recorded in the
lock, item-atomically: fully or not at all. On update, content the user
modified while the kit stayed unchanged is kept; modified while the kit
changed is a conflict — nothing is written until the user resets the item
to the kit. On removal, unchanged content is deleted; user-modified
content is kept and released with a report. An item deleted from the kit
stays installed as an orphan — frozen, updatable never, removable always —
so renaming an item id strands its installed copies: keep ids stable and
raise the version instead.
