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
 penates  ~/.config/opencode                                     3 pending · ? help

  ▾ personal-opencode     ●2 ↑1 ✗1      ~/kits/opencode
    ● writing                           1.0.0          Text editing skill
    ↑ done-v2             opencode-v2   1.2.0 → 1.3.0  Marks finished sessions   ✓
    ✗ git-via-gh          gh            1.2.0          AGENTS.md: heading exists
    ○ done-v1             opencode-v1   1.0.0          Marks finished sessions
  ▸ old-kit               ⊘ unreadable  ~/kits/old
  + add kit

 space unselect · d remove · enter details · / filter · a apply          q quit
```

Kits and items form one tree. Every state shows itself as a glyph and a color
together — available, installed, update or downgrade available, modified,
conflict, broken, warning, orphan (no kit provides the item anymore), and
unavailable (the kit did not load); a pending action takes a separate mark at
the right edge. The footer names the keys the row under the cursor answers:
`space` toggles the default action of the status, `d` removes an installed
item, `r` resets it to the kit content where the key applies, `enter` opens
the details — beside the tree on wide terminals, as a scrolling view
everywhere — and `/` filters by id, tags and description. `a` collects the
pending actions into a summary that nothing is applied before: the items
group by verb with a mark per path, and confirming runs the apply step by
step, each item ending in a check or a cross. On exit penates prints what
was applied during the session to the terminal.

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
[docs/concept.md](docs/concept.md).

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

Prebuilt binaries for Linux, Windows, and macOS — amd64 and arm64 — are on
the [releases](https://github.com/dpleshakov/penates/releases) page; with Go
installed, `go run ./cmd/penates` runs the same interface from source.

With no arguments penates opens the terminal UI. The first run explains what
a kit is and offers `+ add kit`: type the kit's directory — with completion
and a live preview — confirm or edit the target directory the kit's
`target_hint` proposes, and the kit attaches there. On later runs penates
opens the target that was active when it last exited.

`penates version` prints the version and the commit the binary was built
from — `penates dev (unknown)` on a plain `go build`. `penates licenses`
prints the components included in the binary — penates, the Go standard
library and runtime, and the third-party modules — with the license text of
each; the third-party texts are embedded only in the release builds, and a
build from source says so and points at the releases page. A first argument
that names no command is rejected with one `Error:` line naming what to run.

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
