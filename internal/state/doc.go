// Package state holds the application state: the registry — kits, targets,
// attachments between them, the last active target — and the pending actions
// of each target with their deferred operations and variable answers. The
// operations over the registry — attach, detach, edit kit path, forget
// target — keep its invariants. It persists under ~/.config/penates on every
// OS, honoring XDG_CONFIG_HOME, and saves on every mutation. An old-format
// state file is refused with the path to delete. The state the current TUI
// reads is a compatibility view of the registry until the TUI rewrite
// replaces it.
package state
