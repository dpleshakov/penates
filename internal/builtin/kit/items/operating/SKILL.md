---
name: penates-operating
description: Drive penates day to day through the CLI — read the state, change kit content in the kit rather than the target, answer for confirmations, and behave when penates refuses. For the behavior specification — the model, ownership, the state words, and the error categories — see the penates reference skill.
metadata:
  author: The penates project
  version: 0.3.0
---

# penates operating

You drive penates through its command line: every capability of the
terminal UI has a CLI twin, dialog-free. A bare `penates` opens the
terminal UI — that is the human's door; yours is `penates --help` and the
subcommands. Each command has exactly one form: no aliases, no
abbreviations to guess.

On a fresh machine you most likely appeared through
`penates setup --target <path>`: it attaches the built-in kit to your
harness's configuration directory and installs this very skill. The first
run is usually the human's, in the terminal UI; this command is how you
begin one alone.

This skill teaches driving. The `penates` reference skill, installed
beside it, is the behavior specification — the model, ownership, item
states, the state words, and the error categories. This skill never
repeats those tables: when you need to decode a state word or an error
category, look there.

## Read before you act

State is read with two commands:

```
penates status --target <path>  # the target, its kits and items with reasons, staged pending actions
penates show <kit/item> --target <path>  # one item: description, status with reason, variables, contributions
```

Every item line of `status` opens with its state word — `installed`,
`update-available`, `conflict`, and the rest; the words are a stable
contract, and the reference explains what each means. Both commands are
reading commands: they write nothing outside the penates data directory,
so one permission rule covers them all.

`--target <path>` is required wherever a command acts on a target — pass
it every time. You know the configuration directory of your harness; name
it. The active target is the human's terminal-UI state: the CLI neither
reads nor writes which one it opens, and there is no default to fall back
on.

Every failure prints one line, `Error [category]: message`, and the
message names the concrete next step — the command to run, the flag or
value to set, or the act only a person can take. Follow it. The exit
code is `0` only when the command fully succeeded, `2` for a wrong
command line; the full contract is in the reference.

## Change content in the kit, not in the target

The target holds the installed copy; the kit holds the source. An edit
made in the installed copy is a user modification, not a fix: penates
keeps it silently while the kit stands still and refuses as a conflict
once the kit moves. To change what is installed, change the kit:

```
penates kit path <kit>      # the registered directory of the kit — edit there
penates update [<kit/item>…]
```

Edit the kit content, raise the version in its `item.yaml` in the same
change (writing kits is the `penates-kit-authoring` skill's subject), and
run `update`; it applies every available update, or the named items, and
never downgrades.

The kit's directory may move — another checkout path, a renamed clone.
Point the registered kit at the place that now holds it with
`penates kit move <kit> <dir>`; the directory must hold a kit of the same
name, and the kit's attachments and pending actions survive the move.

Machine-specific variation is data, not edits: variables. Stored answers
are reused, so `--set id=value` is needed only for a variable that is new
or whose answer changes:

```
penates install <kit/item> --set opencode_source_path=~/dev/opencode --target <path>
```

`penates reset <kit/item>` returns an item to its kit content and
discards the user's modifications — that is the person's call, not yours.

## Confirmations

A mutating command — `install`, `update`, `remove`, `reset`, `apply`,
`pending clear`, `setup` — prints the summary and asks once. `--dry-run`
prints the summary and writes nothing of it, beside removing an
interrupted apply's staged temp files; reach for it when the outcome is
not obvious. `--yes` confirms in advance: use it when you have full right
to the action — and note that without a terminal there is no asking at
all, so a scripted run that would ask needs `--yes` anyway.

A major update is marked in the summary with its changelog printed; read
it before answering.

## When penates refuses

Two refusals carry a rule of conduct:

- `staged` — the target holds pending actions someone staged, a person
  most likely, in the terminal UI; one-shot commands refuse until they
  are applied or cleared. Do not decide: tell the person who staged
  them. `penates apply`, `penates pending clear`, or waiting — the
  choice is theirs.
- `busy` — another penates process holds the state: someone has the
  terminal UI open. Do not retry in a loop and do not look for
  workarounds; report and wait.

Any other refusal comes with its next step in the message; follow it.
The full table of error categories is in the reference.
