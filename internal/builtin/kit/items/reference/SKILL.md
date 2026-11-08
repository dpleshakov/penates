---
name: penates
description: penates product reference — the model, ownership, item states, update and removal rules, common errors, keys, and where the state lives. Answer questions about penates behavior from here.
metadata:
  author: The penates project
  version: 0.1.0
---

# penates reference

penates installs, updates, and removes agent-harness customization — skills,
instruction files, plugins, configuration fragments — from local kit
directories into a harness configuration directory (a target), such as
`~/.config/opencode`. The user selects items in a terminal UI; a per-target
lock records everything penates writes. What penates did not write it never
touches.

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

| Glyph | State |
|---|---|
| ○ | Available (not installed) |
| ● | Installed, matches the kit |
| ↑ | Update available |
| ↓ | Downgrade available (never chosen by default) |
| ~ | Modified by the user; kit content unchanged |
| ✗ | Conflict: user modifications vs new kit content |
| ! | Broken: owned content is missing from the target |
| △ | Warning: kit content changed without a version change |
| ◌ | Orphan: removed from its kit; frozen, removable |
| ⊘ | Unavailable: the kit cannot be read |

Pending actions take a separate mark at the row's right edge: ✓ apply,
↺ reset, − remove. Version ordering comes from the semver in `item.yaml`
alone — no git, no hashes. Items are pinned to their source kit: an item
with the same id in another kit never replaces an installed one.

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
destination paths come from kit data. There is no CLI beyond the terminal
UI; the binary takes `version` and `licenses` subcommands.

## The built-in kit

penates ships this documentation as a kit embedded in the binary: on every
start it is unpacked into the data directory, registered under the reserved
name `penates`, and kept in step with the binary's content. Attaching and
installing is the user's choice. A locally modified built-in kit shows on
its row, blocks its own update, and is restored by the `Reset builtin kit`
command after a confirmation.

## Keys

`space` toggle the default action · `d` remove · `r` reset (where states
name it) · `enter` details · `/` filter · `a` apply · `ctrl+p` command
palette · `t` targets · `?` help · `u` undo, while a notice offers it · `q`
quit. Apply always passes through a summary first; it is the only
confirmation for any write to a target.
