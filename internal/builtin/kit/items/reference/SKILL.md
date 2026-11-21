---
name: penates
description: penates product reference — the model, ownership, item states, update and removal rules, common errors, keys, the CLI contract, and where the state lives. Answer questions about penates behavior from here.
metadata:
  author: The penates project
  version: 0.3.0
---

# penates reference

penates installs, updates, and removes agent-harness customization — skills,
instruction files, plugins, configuration fragments — from local kit
directories into a harness configuration directory (a target), such as
`~/.config/opencode`. People work in the terminal UI; agents and automation
work through the command line (`penates --help`). A per-target lock records
everything penates writes. What penates did not write it never touches.

## Terminology

| Term | Meaning |
|---|---|
| Harness | An agent environment that consumes the installed content, e.g. OpenCode or Claude Code. |
| Kit | A directory with `kit.yaml` and items, bound to one harness. The source of truth for its items. |
| Item | A directory inside a kit with `item.yaml`. The unit of selection, installation, versioning, and atomicity. |
| Contribution | One declared effect of an item on the target: a whole file, a Markdown section, or a set of JSON values. |
| Shared file | A target file several items and the user may write to; declared by the kit. |
| Target | A harness configuration directory items install into. |
| Lock | The per-target record of installed items and everything penates owns there. |
| Tag | A free-form item label for display and filtering; no semantics. |
| Variable | A per-item parameter asked at selection and substituted into templated contributions. |

An item's identity is the pair (kit name, item id). The same id in another
kit is a different item.

## Ownership

A file, Markdown block, or JSON location is owned by penates if and only if
the lock records it for an item. Everything else belongs to the user and is
never overwritten or removed. Each owned element is judged by three states:
the content recorded at installation (base), the current target content,
and the content the kit now provides (incoming).

- Current equals base, incoming differs → replaced on update.
- Current differs (user edited), incoming equals base → the user's content
  stays, silently.
- Current differs, incoming differs → conflict; nothing is written; the
  item is reported in conflict.
- Present but not recorded → error; never written.

An update never releases ownership on its own; ownership ends only when the
item is removed. On removal, user-modified content is kept and released
from the lock with a report; unchanged content is deleted.

## Item states

The state of an item is decided once and rendered twice: a glyph in the
terminal UI, a state word in the CLI. The words are the first token of
every item line the CLI prints.

| Glyph | Word | State |
|---|---|---|
| ○ | `available` | Not installed; nothing stands in the way |
| ● | `installed` | Matches the kit |
| ↑ | `update-available` | The kit version is higher |
| ↓ | `downgrade-available` | The kit version is lower (never chosen by default) |
| ~ | `modified` | Modified by the user; kit content unchanged |
| ✗ | `conflict` | A planned verdict claims a taken place |
| ! | `broken` | Owned content is missing from the target |
| △ | `warning` | Kit content changed without a version change, or a broken item declaration |
| ◌ | `orphan` | Removed from its kit; frozen, removable |
| ⊘ | `unavailable` | The kit cannot be read; installed items are frozen |

The set of glyphs and the set of words are stable contracts. Pending
actions take a separate mark at the row's right edge: ✓ apply, ↺ reset,
− remove. Version ordering comes from the semver in `item.yaml` alone — no
git, no hashes. Items are pinned to their source kit: an item with the same
id in another kit never replaces an installed one.

## Common errors

- `heading exists` / a heading matches outside managed blocks — a section
  contribution whose first heading already exists in the shared file in the
  user's own text. Installation is refused; rename the heading or the item.
- `not owned` / file present and not recorded — the target already holds a
  file a `file` contribution wants to write. penates never overwrites it;
  the user decides.
- Owned by another item — two items claim the same file, block, or JSON
  location. Reported during planning; no precedence between kits.
- Kit unreadable (⊘) — the registered directory failed to load; its items
  are frozen.
- Orphan (◌) — the item vanished from its kit. It stays installed and
  managed, receives no updates, and can be removed.

## State and files

Everything penates keeps lives in its data directory — `~/.config/penates`
(or `$XDG_CONFIG_HOME/penates`): the registry (kits, targets, attachments,
pending actions), one lock per target, and the unpacked built-in kit with
its stamp. Nothing else is written anywhere except the contributions
themselves in targets.

penates performs no network access and no version-control operations: how a
kit reaches the machine is the owner's concern. It knows no harness —
destination paths come from kit data.

## The built-in kit

penates ships this documentation as a kit embedded in the binary: on every
start it is unpacked into the data directory, registered under the reserved
name `penates`, and kept in step with the binary's content. Its skills
divide the addressing: `reference` (this document) specifies the behavior,
`operating` teaches the agents to drive penates day to day through the CLI,
and `kit-authoring` addresses kit authors. In the terminal UI attaching and
installing is the user's choice; the CLI's first run is
`penates setup --target <path>`, which attaches the kit to a target and
installs the skills. A locally modified built-in kit shows on
its row, blocks its own update, and is restored by the `Reset builtin kit`
command after a confirmation.

## The CLI contract

The command line is for agents and automation; the stable skeleton of its
output and failures is the contract below. `penates --help` names the
commands; `--target <path>` names the target the command acts on and is
required wherever a command acts on one — the CLI neither reads nor writes
which target the terminal UI opens, switching targets is the TUI's own
state — and flags always come after the command path, so permission rules
can match the command text by its prefix.

Exit codes, a contract of exactly three values:

| Code | When |
|---|---|
| `0` | The command fully did what it was asked: success, including the empty ones (nothing staged, nothing to update), a clean `--dry-run` plan, `--help` |
| `1` | Any other failure: operational, refusal, confirmation the command could not ask for, a partially failed command |
| `2` | Usage only: the command line is wrong, and no repeat of it can help |

Error categories, one stderr line per failure in `Error [category]:
message` form, the message naming the next step. The set is a stable
contract: never removed or renamed, new ones may be added.

| Category | Covers |
|---|---|
| `usage` | A wrong or incomplete command line: unknown command or flag, misplaced flag, a required flag without a value, malformed `--set`, unknown variable id, required variable without a value |
| `not_found` | A named thing does not exist: item, kit, target |
| `conflict` | The planner refused a write under the ownership rules, and a registration that collides with the state — a kit already attached, a kit or target name already registered; the message carries the conflict's prose |
| `kit` | The kit content is at fault: unreadable `kit.yaml`, broken declaration, unknown placeholder |
| `staged` | A one-shot command refused: the target holds staged pending actions; apply, clear, or ask who staged them |
| `busy` | Another penates process holds the state; close it or retry |
| `confirmation` | An asking command without `--yes` and without a terminal |
| `state` | penates' own state unreadable or of another format version; the message names the file to delete |
| `io` | A file-system failure outside the state, and the fallback for anything else |

## Keys

`space` toggle the default action · `d` remove · `r` reset (where states
name it) · `enter` details · `/` filter · `a` apply · `ctrl+p` command
palette · `t` targets · `?` help · `u` undo, while a notice offers it · `q`
quit. Apply always passes through a summary first; it is the only
confirmation for any write to a target.
