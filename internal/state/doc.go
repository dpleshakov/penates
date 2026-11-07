// Package state holds the application state: registered kits and targets, the
// kit-to-target mapping, the last selection, and variable answers. It persists under
// ~/.config/penates on every OS, honoring XDG_CONFIG_HOME, and writes nothing
// anywhere else.
package state
