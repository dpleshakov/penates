# penates TUI screens

| | |
|---|---|
| Status | Draft |
| Date | 2026-11-07 |
| Refines | concept.md sections 6.7 (targets) and 10 (user experience) |

This document defines the layout, visual language, screens and key bindings of the penates terminal user interface for v1.

## 1. Interface principles

1. **The layout adapts to the terminal.** penates uses the full width and height of the terminal and re-lays out on resize. There is a minimum size and there are breakpoints; there is no maximum width.
2. **State is shown by a glyph and a color together.** Color alone never carries meaning, so the interface remains complete with `NO_COLOR` and for users with impaired color perception.
3. **Details follow the cursor.** On a wide terminal the details of the row under the cursor are shown in a side pane without an explicit command.
4. **Input happens in place.** Paths, filters and variables are entered where they apply. Modal dialogs are reserved for destructive or irreversible decisions.
5. **Frequent actions have direct keys; rare actions live in the command palette.** The user needs to remember one key for everything else.
6. **One list, one focus.** Kits and items form a single tree. There is no focus switching between panes.

## 2. Layout

### 2.1 Regions

```
 header    application name, current target (opens the target list), pending count, help hint
 body      the tree of kits and items; on wide terminals, the details pane to its right
 input     optional line: filter input
 notice    optional line: transient message, cleared by the next key or after a few seconds
 footer    keys valid for the row under the cursor, then global keys
```

The header and footer are fixed. The tree scrolls; the cursor row is kept in view.

### 2.2 Breakpoints

| Terminal width | Layout |
|---|---|
| 110 columns and more | Tree and details pane side by side. The pane takes about 40% of the width, at least 44 columns. |
| 80 to 109 columns | Tree only, with a description column. `enter` opens details as a full-height view. |
| 60 to 79 columns | Tree only, without tags and descriptions. |
| Below 60 columns or 16 rows | A message stating the minimum size and the current size. |

Within a layout, columns that do not fit are dropped in this order: tags, description, then the version column shrinks to the kit version alone.

Prose in the details pane and in the details view is wrapped at no more than 100 columns, regardless of the available width. This limit concerns readability, not layout.

### 2.3 Mockups

Mockups are shown for terminals of 120, 90 and 64 columns. Colors are not reproduced. `▌` marks the cursor row, which is rendered as a highlighted background.

## 3. Visual language

### 3.1 Item status

| Glyph | Status | Color | ASCII fallback |
|---|---|---|---|
| `○` | Available (not installed) | dim | `o` |
| `●` | Installed, matches the kit | green | `*` |
| `↑` | Update available | cyan | `^` |
| `↓` | Downgrade available | dim | `v` |
| `~` | Modified by the user; kit content unchanged | yellow | `~` |
| `✗` | Conflict | red | `x` |
| `!` | Broken: owned content is missing from the target | red | `!` |
| `△` | Warning: kit content changed without a version change | yellow | `?` |
| `◌` | Orphan: removed from its kit | yellow | `.` |
| `⊘` | Unavailable: the kit cannot be read | dim | `#` |

### 3.2 Pending actions

Pending actions are shown in a column at the right edge of the tree, separate from the status.

| Glyph | Pending action | Color | ASCII fallback |
|---|---|---|---|
| `✓` | Install, update or downgrade | accent | `+` |
| `↺` | Reset to kit | accent | `r` |
| `−` | Remove | red | `-` |

The exact verb of a pending action is shown in the details pane and in the summary.

### 3.3 Other conventions

- Secondary information (paths, counts, tags) is dim.
- The theme follows the terminal background: light or dark.
- `NO_COLOR` is honored.
- The ASCII glyph set is used when `PENATES_ASCII=1` is set; automatic detection of a terminal that cannot render the Unicode set is deferred (backlog `ascii-detect`). The progress spinner of 10.2 falls back to `|` `/` `-` `\` in the ASCII set.
- Mouse support is a planned addition after v1 (backlog `mouse-support`): clicking a row moves the cursor, clicking the pending column toggles the default action, the wheel scrolls, and clicking the target in the header opens the target list. No interaction ever requires the mouse.

### 3.4 Rows

An item row contains: status glyph, item id, tags, version (installed version, or `installed → kit` when they differ), description, pending action. For an item in `conflict`, `broken` or `warning`, the description column shows the reason.

A kit row contains: expansion mark (`▾` expanded, `▸` collapsed), kit name, a summary of item states by glyph (for example `●2 ↑1 ✗1`), and the kit path. An unreadable kit shows `⊘ unreadable` in place of the summary and is collapsed.

The last row of the tree is `+ add kit`.

## 4. Main screen

### 4.1 Wide terminal

```
 penates  ~/.config/opencode ▾                                                                       3 pending · ? help

  ▾ personal-opencode     ●2 ↑1 ✗1      ~/kits/opencode       │ done-v2 · personal-opencode
    ● writing                     1.0.0                       │
▌   ↑ done-v2       opencode-v2   1.2.0 → 1.3.0          ✓    │ Marks the session name with [DONE] on the /done
    ✗ git-via-gh    gh            1.2.0                       │ command.
    ○ done-v1       opencode-v1   1.0.0                       │
  ▾ team-opencode         ○1 ◌1         ~/kits/team           │ Status    ↑ update, 1.2.0 → 1.3.0 (minor)
    ○ review                      0.3.0                  ✓    │ Pending   ✓ update to 1.3.0
    ◌ old-lint                    0.2.0                  −    │ Tags      opencode-v2
  ▸ old-kit               ⊘ unreadable  ~/kits/old            │
  + add kit                                                   │ Contributes
                                                              │   file  plugin/done.ts
                                                              │   json  opencode.json  /plugin
                                                              │
                                                              │ Changelog
                                                              │   1.3.0  Supports custom marker text
                                                              │   1.2.0  Initial release for OpenCode v2

 space unselect · d remove · enter details · / filter · a apply · ctrl+p commands                                q quit
```

### 4.2 Medium terminal

```
 penates  ~/.config/opencode ▾                                         3 pending · ? help

  ▾ personal-opencode     ●2 ↑1 ✗1      ~/kits/opencode
    ● writing                   1.0.0          Text editing skill
▌   ↑ done-v2      opencode-v2  1.2.0 → 1.3.0  Marks finished sessions                  ✓
    ✗ git-via-gh   gh           1.2.0          AGENTS.md: heading "Git usage" exists
    ○ done-v1      opencode-v1  1.0.0          Marks finished sessions
  ▾ team-opencode         ○1 ◌1         ~/kits/team
    ○ review                    0.3.0          Review agent with team checklist         ✓
    ◌ old-lint                  0.2.0          Removed from kit                         −
  ▸ old-kit               ⊘ unreadable  ~/kits/old
  + add kit

 space unselect · d remove · enter details · / filter · a apply                    q quit
```

### 4.3 Narrow terminal

```
 penates  …/opencode ▾                                3 pending

  ▾ personal-opencode     ●2 ↑1 ✗1
    ● writing         1.0.0
▌   ↑ done-v2         1.2.0 → 1.3.0                           ✓
    ✗ git-via-gh      1.2.0
    ○ done-v1         1.0.0
  ▾ team-opencode         ○1 ◌1
    ○ review          0.3.0                                   ✓
    ◌ old-lint        0.2.0                                   −
  ▸ old-kit               ⊘ unreadable
  + add kit

 space · d · enter · / · a apply                         ? help
```

The target path in the header is shortened from the left when it does not fit.

### 4.4 Too small

```
  penates needs at least
  60 columns and 16 rows.

  Current size: 48 × 20.
```

## 5. Details

The details pane (wide terminals) and the details view (`enter`, any width) show the same content for the row under the cursor. `esc` closes the details view. Long content in the view scrolls with `↑` `↓`, `pgup` `pgdn`. In the details view of a major update under review (7.2), `space` selects the update.

### 5.1 Item

Content, in order:

- id and kit;
- description;
- `Status` with the reason where applicable;
- `Pending` when an action is set;
- `Tags`;
- `Variables` with stored answers;
- `Contributes`: kind, target path, and section block or JSON location;
- `Changelog` when `CHANGELOG.md` is present.

Reasons for non-installable or modified states are stated with the next step:

```
git-via-gh · personal-opencode

Use gh instead of git where git is blocked

Status    ✗ conflict
          AGENTS.md has the heading "Git usage"
          outside managed blocks. Remove or rename
          it, then rescan with R.

Contributes
  file     skills/git-via-gh/SKILL.md
  section  AGENTS.md
  json     opencode.json  /permission/bash/gh *
```

```
done-v2 · personal-opencode

Status    ~ modified
          plugin/done.ts was changed after installation.
          The kit content is unchanged; your version is
          kept on updates.

          r resets the file to the kit version.
```

### 5.2 Kit

```
▌ ▾ personal-opencode     ●2 ↑1 ✗1      ~/kits/opencode       │ personal-opencode
    ● writing                     1.0.0                       │
    ↑ done-v2       opencode-v2   1.2.0 → 1.3.0               │ Personal OpenCode customization
    ✗ git-via-gh    gh            1.2.0                       │
    ○ done-v1       opencode-v1   1.0.0                       │ Path           ~/kits/opencode
                                                              │ Target hint    ~/.config/opencode
                                                              │ Shared files   AGENTS.md, opencode.json
                                                              │ Items          4: 2 installed, 1 update, 1 conflict
                                                              │ Also attached  ~/wsl/opencode

 space collapse · e edit path · d detach · / filter · a apply · ctrl+p commands                                  q quit
```

## 6. Kits

### 6.1 First run

With no kit registered, the tree contains only `+ add kit`, and the body explains what a kit is.

```
 penates  no target                                                                         ? help




                              No kits are registered yet.

              A kit is a directory with kit.yaml. Add one to start managing
              items in a harness configuration directory.


 ▌+ add kit



 enter add kit · ctrl+p commands                                                            q quit
```

The first run opens on this screen; `enter` on `+ add kit` leads into 6.2.

### 6.2 Adding a kit

`enter` on `+ add kit` turns the row into a path input with directory completion. As soon as the input names a directory containing `kit.yaml`, the details pane shows the kit preview; the directory is read a short interval after the last keystroke, not on every key. On medium and narrow terminals the preview opens as the details view after `enter`.

```
 penates  ~/.config/opencode ▾                                                                                   ? help

  ▾ personal-opencode     ●2 ↑1 ✗1      ~/kits/opencode       │ New kit
    ● writing                     1.0.0                       │
    ↑ done-v2       opencode-v2   1.2.0 → 1.3.0               │ Path          ~/kits/claude
  ▸ team-opencode         ○1 ◌1         ~/kits/team           │ Name          personal-claude
▌ + add kit  ~/kits/cla█                                      │ Description   Personal Claude Code customization
             ╭──────────────────────╮                         │ Items         7
             │ ~/kits/claude/       │                         │ Shared files  CLAUDE.md, settings.json
             │ ~/kits/claude-work/  │                         │ Target hint   ~/.claude
             ╰──────────────────────╯                         │
                                                              │ △ The kit proposes ~/.claude.
                                                              │   The current target is ~/.config/opencode.

 tab complete · enter attach · esc cancel
```

- When `target_hint` equals the current target, `enter` attaches the kit. Attaching changes only the penates configuration and takes effect immediately; nothing is written to the target.
- When `target_hint` differs from the current target, the preview carries the warning shown above and `enter` opens 6.3. The comparison is between two paths; penates does not interpret either.
- A kit can be attached to several targets. Attaching always applies to the current target.

### 6.3 Target hint differs

```
╭ Attach personal-claude ──────────────────────────────────╮
│ The kit proposes ~/.claude.                              │
│ The current target is ~/.config/opencode.               │
│                                                          │
│ ▌Register ~/.claude as a target and attach there         │
│  Attach to ~/.config/opencode anyway                     │
│  Cancel                                                  │
│                                                          │
│ enter choose · esc cancel                                │
╰──────────────────────────────────────────────────────────╯
```

When `~/.claude` is already registered, the first option reads `Attach to ~/.claude and switch to it`.

### 6.4 First target

When no target is registered, the preview ends with a target input prefilled with `target_hint`. On medium and narrow terminals, where there is no details pane, the preview — and with it the input — opens as the details view after `enter` (6.2).

```
 penates  no target                                                                                              ? help

                                                              │ New kit
                                                              │
                                                              │ Path          ~/kits/opencode
▌ + add kit  ~/kits/opencode                                  │ Name          personal-opencode
                                                              │ Items         4
                                                              │ Shared files  AGENTS.md, opencode.json
                                                              │
                                                              │ No target is registered. Target directory:
                                                              │ ▌~/.config/opencode█
                                                              │   The directory exists.
                                                              │
                                                              │ enter register target and attach · esc cancel
```

When the directory does not exist, the line reads `The directory does not exist. It is created on the first apply.`

### 6.5 Errors

Errors are shown in the preview. The input remains editable.

```
▌ + add kit  ~/kits/opencode                                  │ New kit
                                                              │
                                                              │ Path          ~/kits/opencode
                                                              │
                                                              │ ✗ The kit cannot be added.
                                                              │
                                                              │   kit.yaml:4  shared: expected a list of file names
```

Other errors:

- `kit.yaml not found in ~/kits/opencode`
- `A kit named personal-opencode is already registered at ~/kits/opencode-old`
- `The kit is already attached to this target`

Item-level errors, such as an invalid `item.yaml` or duplicate ids, do not prevent attaching the kit. They are shown on the affected item rows.

### 6.6 Unreadable kit and relocation

An unreadable kit is collapsed and marked `⊘ unreadable`. Its installed items are frozen (RFC 9.2). `e` on the kit row turns the row into a path input:

```
  ▸ team-opencode         ○1 ◌1         ~/kits/team           │ old-kit
▌ ▾ old-kit  ~/archive/old-k█                                 │
     ◌ legacy-rules                0.4.0                      │ ✗ The kit directory cannot be read:
                                                              │   ~/kits/old: no such directory
                                                              │
                                                              │ Its 1 installed item is frozen: no updates, removal
                                                              │ only. Point the kit to its new directory to resume.

 tab complete · enter apply path · esc cancel
```

The new directory must contain a kit with the same name. Otherwise the path is rejected:

```
▌ ▾ old-kit  ~/archive/old-kit                                │ old-kit
                                                              │
                                                              │ ✗ ~/archive/old-kit contains the kit team-tools,
                                                              │   not old-kit. Add it as a new kit instead.
```

`e` is also available on readable kits, for a kit directory that has been moved deliberately.

### 6.7 Detaching a kit

`d` on a kit row with installed items opens a dialog. The default option is `Cancel`.

```
╭ Detach personal-opencode ────────────────────────────────────╮
│ 2 items of this kit are installed in ~/.config/opencode:     │
│   writing 1.0.0, done-v2 1.2.0                               │
│                                                              │
│  Remove its items from the target                            │
│  Keep its items installed as orphans                         │
│ ▌Cancel                                                      │
│                                                              │
│ enter choose · esc cancel                                    │
╰──────────────────────────────────────────────────────────────╯
```

- **Remove its items from the target:** the items receive the pending action `−`. The kit is detached after the apply that removes them completes; until then its row shows `detaching`.
- **Keep its items installed as orphans:** the kit is detached immediately. Its installed items move to an `orphans` group at the end of the tree.

A kit without installed items is detached immediately. The notice line confirms it and offers `u` to undo while the notice is shown.

## 7. Items

### 7.1 Selecting actions

| Key | Effect |
|---|---|
| `space` | Toggles the default action for the item's status. |
| `d` | Toggles removal. Available on any installed item, including orphans and items of unreadable kits. |
| `r` | Toggles reset. Available on items in the states modified, conflict and broken. |

| Status | Default action of `space` |
|---|---|
| Available | Install |
| Update available | Update |
| Downgrade available | Downgrade |
| Broken | Reset |
| Other states | None; the footer names the applicable key, if any |

### 7.2 Major update

A major update cannot be selected without reviewing it. The first `space` on such a row shows the details with the changelog and the following note; the second `space` — in the details view or on the row itself — selects the update.

```
review · team-opencode

Status    ↑ update, 0.3.0 → 1.0.0 (major)

△ A major version changes behavior. Review the
  changelog below, then press space again to select.

Changelog
  1.0.0  Checklist rewritten; steps 4–6 removed
  0.4.0  Adds security section
```

### 7.3 Variables

When an item with variables is selected for installation, or an update introduces a new variable, the details pane (or the details view) turns into a form.

```
opencode-source · personal-opencode

Variables

Path to the local OpenCode source tree   required
▌D:\src\opencode█

Name of the default branch
 dev

tab next · enter confirm · esc cancel selection
```

- Fields are prefilled with stored answers.
- `enter` is accepted only when every required field has a value.
- `esc` clears the pending action of the item.

### 7.4 Replacing a variant

When the selected item collides only with content owned by another installed item, a dialog offers the replacement.

```
╭ Replace done-v1 ───────────────────────────────────────────╮
│ done-v2 writes to plugin/done.ts, which belongs to the     │
│ installed item done-v1 (personal-opencode).                │
│                                                            │
│ ▌Remove done-v1 and install done-v2                        │
│  Cancel                                                    │
│                                                            │
│ enter choose · esc cancel                                  │
╰────────────────────────────────────────────────────────────╯
```

Replacement sets `−` on the installed item and `✓` on the selected one. The summary shows both as one replacement.

### 7.5 Filter

`/` opens the filter input above the footer. The tree is filtered as the user types, by id, tags and description. Kit rows remain visible when any of their items match and show the number of matches.

```
 penates  ~/.config/opencode ▾                                                                                   ? help

  ▾ personal-opencode     2 of 4        ~/kits/opencode       │ done-v2 · personal-opencode
▌   ↑ done-v2       opencode-v2   1.2.0 → 1.3.0               │
    ○ done-v1       opencode-v1   1.0.0                       │ Marks the session name with [DONE] on the /done
                                                              │ command.

 / done█

 enter keep filter · esc clear
```

## 8. Targets

### 8.1 Target list

`t` opens the target list under the header. The list supports fuzzy search.

```
 penates  ~/.config/opencode ▴                                                                                   ? help
          ╭──────────────────────────────────────────────────────────╮
          │ › █                                                      │
          │▌● ~/.config/opencode        current   2 kits · 5 items   │
          │  ○ opencode wsl  ~/wsl/opencode       1 kit  · 3 items   │
          │  ○ ~/.claude                          1 kit  · 0 items   │
          │  + add target                                            │
          ├──────────────────────────────────────────────────────────┤
          │ enter switch · ctrl+r rename · ctrl+d remove · esc close │
          ╰──────────────────────────────────────────────────────────╯
```

- A target may carry an optional display name. The header shows the name, and the path in the details of the target list.
- `+ add target` turns into a path input with completion and the existence line of 6.4.

### 8.2 Switching with pending actions

Pending actions belong to one target, because each target has its own lock. Switching saves them for the target being left; opening that target again — at startup or by switching back — offers them for restoration (11.1). Switching never discards pending actions and asks nothing.

### 8.3 Removing a target

```
╭ Remove target ~/wsl/opencode ──────────────────────────────╮
│ 3 items are installed in this target.                      │
│                                                            │
│  Remove the items, then forget the target                  │
│  Forget the target, leave the files unmanaged              │
│ ▌Cancel                                                    │
│                                                            │
│ enter choose · esc cancel                                  │
╰────────────────────────────────────────────────────────────╯
```

- **Remove the items, then forget the target:** switches to the target, sets `−` on all installed items and opens the summary. The target is forgotten after the apply completes.
- **Forget the target, leave the files unmanaged:** the lock of the target is deleted. Files in the target are not touched and are no longer owned by penates.

## 9. Command palette

`ctrl+p` opens the command palette with fuzzy search. Commands that apply to a kit act on the kit under the cursor; when the cursor is not inside a kit, they ask for one. Commands that have a direct key show it.

```
                  ╭──────────────────────────────────────────────────╮
                  │ › ta█                                            │
                  │▌Switch target                                  t │
                  │ Add target                                       │
                  │ Rename target                                    │
                  │ Remove target                                    │
                  │ Detach kit                                       │
                  ╰──────────────────────────────────────────────────╯
```

Commands: Add kit, Detach kit, Edit kit path, Switch target, Add target, Rename target, Remove target, Rescan kits, Apply, Clear pending actions, Help, Quit.

## 10. Apply

### 10.1 Summary

`a` replaces the body with the summary. It is the only confirmation for any write to a target.

```
 penates  Apply to ~/.config/opencode                               3 items · 4 writes · 1 excluded

 install  review 0.3.0                                                                team-opencode
   + agents/review.md
   + AGENTS.md                     section team-opencode/review

 update   done-v2 1.2.0 → 1.3.0                                                   personal-opencode
   ~ plugin/done.ts
   = plugin/README.md              kept: changed by you, unchanged in kit

 remove   old-lint 0.2.0                                                              team-opencode
   - agents/old-lint.md
   ! agents/old-lint-notes.md      kept and released: changed by you

 excluded
   ✗ git-via-gh                    AGENTS.md has the heading "Git usage" outside managed blocks

 enter apply · ↑↓ scroll · esc back
```

| Mark | Color | Meaning |
|---|---|---|
| `+` | green | Create a file, block or JSON value |
| `~` | yellow | Replace owned content |
| `-` | red | Delete owned content |
| `=` | dim | No write; owned content is kept and remains owned |
| `!` | yellow | No write; content is kept and released from the lock |

A reset is listed as `reset` with `~` lines. A replacement of a variant (7.4) is listed as one `replace` group. When every pending item is excluded, `enter` is unavailable and the footer reads `nothing to apply`.

### 10.2 Progress

```
 penates  Applying to ~/.config/opencode                                                     2 of 3

 ✓ install  review 0.3.0
 ✓ update   done-v2 1.3.0
 ⠋ remove   old-lint 0.2.0
```

The view cannot be left by navigation while items are being applied; the double `ctrl+c` quit (12.1) remains available, and the next start reconciles the interruption and offers the pending actions for restoration.

### 10.3 Result

```
 penates  Applied to ~/.config/opencode                                           3 done · 0 failed

 ✓ installed  review 0.3.0
 ✓ updated    done-v2 1.3.0
 ✓ removed    old-lint
              agents/old-lint-notes.md kept: changed by you

 enter back to the list
```

An item that failed during application is listed with `✗` and the reason. Its changes are not recorded, consistent with item-level atomicity (RFC 7.7).

### 10.4 Record on exit

When penates exits, it prints the changes applied during the session to the regular terminal output, so that a record remains in the terminal history:

```
penates: applied to ~/.config/opencode
  installed  team-opencode/review 0.3.0
  updated    personal-opencode/done-v2 1.2.0 → 1.3.0
  removed    team-opencode/old-lint (kept: agents/old-lint-notes.md)
```

Nothing is printed when no changes were applied.

## 11. Startup and notices

Startup opens the target that was current when penates last exited.

### 11.1 Restoring pending actions

When a target with unapplied pending actions is opened — at startup, or by switching to it (8.2) — the dialog offers restoration:

```
╭ Previous selection ────────────────────────────────────╮
│ The last session ended with 2 pending actions in       │
│ ~/.config/opencode: install review, update done-v2.    │
│                                                        │
│ ▌Restore                                               │
│  Start with no pending actions                         │
│                                                        │
│ enter choose · esc start with none                     │
╰────────────────────────────────────────────────────────╯
```

Restored actions that are no longer valid, for example an update whose kit version has changed, are dropped and reported on the notice line.

### 11.2 Notices

Transient results are shown on the notice line:

- after a rescan (`R`; kits are also read at every start): `Rescanned 2 kits: 1 new item, 1 update, 1 orphan.` Pending actions invalidated by the rescan are dropped and counted in the same message;
- after reconciliation of an interrupted apply (RFC 7.7): `Reconciled after an interrupted apply: done-v2 recorded as 1.2.0.`;
- after an immediate configuration change, such as attaching or detaching a kit without installed items, with `u` to undo.

### 11.3 Quitting with pending actions

```
╭ Quit ──────────────────────────────────────────────────╮
│ 2 pending actions have not been applied. They will     │
│ be offered for restoration on the next start.          │
│                                                        │
│ ▌Quit                                                  │
│  Back                                                  │
│                                                        │
│ enter choose · esc back                                │
╰────────────────────────────────────────────────────────╯
```

The force quit — `ctrl+c` twice (12.1) — skips this dialog; pending actions are saved either way and offered on the next start.

## 12. Key bindings

### 12.1 Global

| Key | Action |
|---|---|
| `↑` `↓`, `j` `k` | Move the cursor |
| `pgup` `pgdn`, `home` `end` | Move by page, to the first or last row |
| `/` | Filter (7.5) |
| `a` | Summary (10.1) |
| `t` | Target list (8.1) |
| `R` | Rescan kits |
| `ctrl+p` | Command palette (9) |
| `?` | Full help |
| `q` | Quit |
| `ctrl+c` twice | Force quit without dialogs |
| `esc` | Close the overlay or view, cancel the input, or clear the filter |

`ctrl+c` alone does not quit: it arms the quit and shows the notice `press ctrl+c again to quit`. Any other key disarms it; the second `ctrl+c` quits immediately, without dialogs — pending actions are saved on every mutation, and an interrupted apply is repaired by reconciliation (11.2). The double press also protects a `ctrl+c` meant to copy selected text.

### 12.2 Kit row

| Key | Action |
|---|---|
| `space`, `→` `←` | Expand or collapse |
| `enter` | Details view |
| `e` | Edit path (6.6) |
| `d` | Detach (6.7) |

### 12.3 Item row

| Key | Action |
|---|---|
| `space` | Toggle the default action (7.1) |
| `d` | Toggle removal |
| `r` | Toggle reset |
| `enter` | Details view |
| `←` | Move to the kit row |

### 12.4 `+ add kit` row

| Key | Action |
|---|---|
| `enter` | Add a kit (6.2) |

### 12.5 Help

`?` and the palette command Help open the full help: the bindings of 12.1–12.4 grouped by context, and the status and pending glyph legends (3.1, 3.2). `esc` closes it.
