package view

// The state of an item, decided once and rendered twice: a glyph and a
// color in the TUI, a state word in the CLI. The state words are part of
// the CLI's stable output skeleton: they are the first token of every item
// line, and their set is a contract — existing words are never removed or
// renamed, and new ones may be added together with the states they name.

// State is the state of one item as both interfaces show it. The states
// form a priority, decided in Decide: a planned conflict outranks all, a
// planned warning outranks the rest, broken outranks modified, modified
// outranks the version markers, and the version markers outrank the frozen
// states an item is shown in when no kit item backs it.
type State int

const (
	// StateAvailable — not installed; a kit version exists, nothing stands
	// in the way.
	StateAvailable State = iota
	// StateInstalled — installed, the versions match, the content stands
	// as recorded.
	StateInstalled
	// StateUpdate — the kit version is higher than the installed one.
	StateUpdate
	// StateDowngrade — the kit version is lower than the installed one;
	// never chosen by default.
	StateDowngrade
	// StateModified — the user changed owned content; the kit content is
	// unchanged.
	StateModified
	// StateConflict — a planned verdict claims a taken place: user content
	// in the path, a place owned by another item, changed content against
	// changed kit content.
	StateConflict
	// StateBroken — owned content is missing from the target.
	StateBroken
	// StateWarning — kit content changed without a version change, or the
	// item's declaration is broken.
	StateWarning
	// StateOrphan — installed, the kit stopped providing it (or the kit is
	// not attached).
	StateOrphan
	// StateUnavailable — the kit cannot be read; installed items are
	// frozen.
	StateUnavailable
)

// word returns the CLI state word of the state.
var word = [...]string{
	StateAvailable:   "available",
	StateInstalled:   "installed",
	StateUpdate:      "update-available",
	StateDowngrade:   "downgrade-available",
	StateModified:    "modified",
	StateConflict:    "conflict",
	StateBroken:      "broken",
	StateWarning:     "warning",
	StateOrphan:      "orphan",
	StateUnavailable: "unavailable",
}

// String returns the CLI state word of the state: available, installed,
// update-available, downgrade-available, modified, conflict, broken,
// warning, orphan, unavailable. The set of words is a stable contract of
// the CLI's output skeleton.
func (s State) String() string { return word[s] }

// ItemState is the decided state of one item: the state itself, the reason
// prose the line shows in its place — a rendered conflict, the broken
// reason, the first problem of a broken declaration, or the warning reason
// — and whether the item's version change crosses the major version.
type ItemState struct {
	State  State
	Reason string
	Major  bool
}
