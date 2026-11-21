package view

import (
	"github.com/dpleshakov/penates/internal/kit"
	"github.com/dpleshakov/penates/internal/lock"
	"github.com/dpleshakov/penates/internal/plan"
)

// The decision itself: kit item, lock record, reconciliation and verdict
// in; one ItemState out. Both clients call this — the tree builder of the
// TUI for its rows, the CLI for its item lines — so an item never holds
// different states on the two sides of the interface.

// BrokenReason is the fixed reason a broken item shows in its reason
// lines: the content it owns is missing from the target.
const BrokenReason = "owned content is missing from the target"

// RowID is the identity token an item is keyed and labeled by: the token
// the loader assigned — the item id, or the directory name of an item
// whose id is unusable — or the bare id for items built without the
// loader.
func RowID(it *kit.Item) string {
	if it.RowID != "" {
		return it.RowID
	}
	return it.ID
}

// Decide decides the state of one item a loaded kit provides, from the
// values the load path gathered: key is the item's identity ("kit name/item
// id"), it is the kit item, rec its lock record (nil when not installed),
// reconciled the lock-vs-target reconciliation (nil when it did not run),
// and ip the planner's verdict for the item (nil when nothing was planned).
//
// An item whose declaration is broken warns with its first problem and
// takes no further part. An installed item is broken or modified by the
// reconciliation, otherwise it carries its version marker; the verdict
// lifts a conflict or a warning over any of that. An item without a lock
// record is available, again under the verdict.
func Decide(key string, it *kit.Item, rec *lock.Item, reconciled *plan.Reconciliation, ip *plan.ItemPlan) ItemState {
	if len(it.Problems) > 0 {
		// An item whose declaration is broken: the warning names the first
		// problem, and the item takes no part in planning.
		return ItemState{State: StateWarning, Reason: it.Problems[0].Error()}
	}
	var st ItemState
	if rec == nil {
		st.State = StateAvailable
		st.applyVerdict(ip)
		return st
	}
	if kit.MajorVersion(it.Version) != kit.MajorVersion(rec.Version) {
		st.Major = true
	}
	switch {
	case reconciled != nil && reconciled.BrokenItem(key):
		st.State = StateBroken
		st.Reason = BrokenReason
	case reconciled != nil && reconciled.ModifiedItem(key):
		st.State = StateModified
	default:
		switch kit.CompareVersions(it.Version, rec.Version) {
		case 1:
			st.State = StateUpdate
		case -1:
			st.State = StateDowngrade
		default:
			st.State = StateInstalled
		}
	}
	st.applyVerdict(ip)
	return st
}

// DecideFrozen decides the state of one installed item no kit item backs:
// an orphan, or an item of a kit that did not load. base is the frozen
// state the line shows the item in; the reconciliation may still raise it
// — a broken or modified item outranks the frozen state it is shown in.
func DecideFrozen(key string, base State, reconciled *plan.Reconciliation) ItemState {
	st := ItemState{State: base}
	switch {
	case reconciled != nil && reconciled.BrokenItem(key):
		st.State = StateBroken
		st.Reason = BrokenReason
	case reconciled != nil && reconciled.ModifiedItem(key):
		st.State = StateModified
	}
	return st
}

// applyVerdict lifts the planner's verdict onto the state: a verdict with
// conflicts makes the item conflicting, a NoAction verdict with a warning
// makes it warning. An available item takes the verdict the way an
// installed one does — the planner speaks about the selection, not about
// the lock.
func (st *ItemState) applyVerdict(ip *plan.ItemPlan) {
	if ip == nil {
		return
	}
	if len(ip.Conflicts) > 0 {
		st.State = StateConflict
		st.Reason = ConflictLine(ip.Conflicts[0])
	} else if ip.Status == plan.StatusNoAction && ip.Warning {
		st.State = StateWarning
		st.Reason = ip.Reason
	}
}
