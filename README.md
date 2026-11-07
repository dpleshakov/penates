# penates

penates installs, updates, and removes agent-harness customization — skills,
instruction files, plugins, configuration fragments — from local kit
directories into a harness configuration directory such as
`~/.config/opencode`. You select the items in a terminal UI; a per-target lock
records everything penates writes, and what it did not write it never touches.

Customization kept in sync across machines faces two independent streams of
change: the shared content itself evolves, and every machine needs its own
paths, tools, and harness versions. When both live in the same files, each sync
is a manual merge. A kit holds the shared content as installable **items**;
each machine selects its own items and answers its own variables.

## The main screen

```
penates  target ~/.config/opencode  kits: personal-opencode, team-opencode

-- personal-opencode  ~/kits/opencode
 [installed]  writing                        1.0.0            Text editing skill
 [update]     done-v2  opencode-v2           1.2.0 > 1.3.0    Marks finished sessions
 [conflict]   git-via-gh  gh                 1.2.0            AGENTS.md: heading exists
 [ ]          done-v1  opencode-v1           1.0.0            Marks finished sessions

-- team-opencode  ~/kits/team
>[selected]   review                         0.3.0            Review agent with team checklist
 [orphan]     old-lint                       0.2.0            Removed from kit

/  filter    space  select    i  details    r  reset    d  remove    a  apply    q  quit
```

Bracket markers cover every state: not installed, installed, selected, update
and downgrade available, modified, broken, conflict, warning, orphan (no kit
provides the item anymore), and unavailable (the kit did not load).

## A minimal kit

A kit is a plain directory; an item directory holds everything one installable
capability needs.

```
my-opencode-kit/
├── kit.yaml
└── items/
    └── writing/
        ├── item.yaml
        ├── skill/SKILL.md
        └── agents-note.md
```

```yaml
# kit.yaml
name: my-opencode-kit            # unique among registered kits
description: Personal OpenCode customization
target_hint: ~/.config/opencode  # proposed target on first registration
shared:                          # files that section and json contributions may write to
  - AGENTS.md
```

```yaml
# items/writing/item.yaml
id: writing
version: 1.0.0
description: Text editing skill
contributes:
  - file: skill/SKILL.md
    to: skills/writing/SKILL.md
  - section: agents-note.md
    to: AGENTS.md
```

The `file` contribution installs the skill whole under `skills/writing/`. The
`section` contribution appends a marked block to the shared `AGENTS.md`,
creating the file if it is absent; the rest of the file stays untouched, and
removing the item takes the block with it. Items may also contribute `json`
fragments into shared configuration files and declare variables that are
requested at installation and substituted into templated contributions. The
complete format, with the update and removal rules, is specified in
[docs/rfc-penates.md](docs/rfc-penates.md).

## Ownership

- Everything penates writes is recorded in a per-target lock; what is not
  recorded belongs to the user and is never overwritten or removed.
- An update keeps a file the user modified when the kit content is unchanged,
  and reports a conflict instead of writing when it differs.
- A removal deletes what is unchanged, keeps the content the user reshaped,
  and reports it.

penates keeps its own state in its configuration directory; a target receives
only the declared contributions.

## Running

No releases yet — build from source and start with a kit path:

```
go run ./cmd/penates /path/to/my-opencode-kit
```

The first run proposes the kit's `target_hint` as the target; confirm or edit
it, and the kit registers. Without arguments, penates opens the registered
target, or offers a picker when several are registered.

## Development

```
go build ./...
go test ./...
golangci-lint run
```

The stack and modules are recorded in
[docs/architecture.md](docs/architecture.md).

License: [MIT](LICENSE). Releases follow
[Semantic Versioning](https://semver.org/spec/v2.0.0.html).
