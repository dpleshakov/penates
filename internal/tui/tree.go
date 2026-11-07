package tui

import (
	"sort"
	"strings"

	"github.com/dpleshakov/penates/internal/kit"
	"github.com/dpleshakov/penates/internal/lock"
	"github.com/dpleshakov/penates/internal/plan"
	"github.com/dpleshakov/penates/internal/state"
	"github.com/dpleshakov/penates/internal/target"
)

// The tree model behind the body (tui-screens.md 3.4): one tree of kit rows,
// item rows and the add-kit row, built from plain values. The model performs
// no I/O: the caller loads the kits, reads the lock, snapshots the target
// and plans the selection, and hands the results in as values; the output is
// structured rows that render through a Layout and a glyph Set (RenderTree).

// AddKitLabel is the label of the last row of the tree (3.4).
const AddKitLabel = "+ add kit"

// OrphansKey is the collapse-state key of the trailing orphans group. The
// key is reserved: a kit name is a slug and cannot hold a NUL byte, so no
// kit collapses by it.
const OrphansKey = "\x00orphans"

// orphansName is the name the trailing orphans group shows (6.7).
const orphansName = "orphans"

// brokenReason is the reason a broken item shows in the description column
// (3.1, 3.4): the content it owns is missing from the target.
const brokenReason = "owned content is missing from the target"

// RowKind tells the three kinds of tree rows apart.
type RowKind int

const (
	// KindKit is the header row of one group: a kit, or the trailing
	// orphans group (6.7).
	KindKit RowKind = iota
	// KindItem is one item line under its group.
	KindItem
	// KindAddKit is the last row of the tree (3.4).
	KindAddKit
)

// Row is one line of the tree. Kind says which payload holds.
type Row struct {
	Kind RowKind
	Kit  KitRow
	Item ItemRow
}

// GlyphCount is one entry of a kit summary: a status and the number of the
// group's item rows that hold it (3.4).
type GlyphCount struct {
	Status Status
	Count  int
}

// KitRow is the header row of one group: a kit, or the orphans group.
type KitRow struct {
	// Name is the kit name; the orphans group is named "orphans".
	Name string
	// Path is the kit directory; the orphans group carries none.
	Path string
	// Collapsed reports whether the group's item rows stay out of the
	// tree. A collapsed group keeps its summary (3.4).
	Collapsed bool
	// Unreadable reports a kit that is registered and attached but did not
	// load: it shows "unreadable" in place of the summary and is collapsed
	// by default (6.6).
	Unreadable bool
	// Detaching reports a kit that detaches once the apply removes its
	// installed items (6.7).
	Detaching bool
	// Orphans marks the trailing group of the items whose kit is not
	// attached to the target (6.7). It is not a kit.
	Orphans bool
	// Summary counts the group's item rows by status glyph, in the order
	// of the status table (3.1). An unreadable kit carries none.
	Summary []GlyphCount
	// Matches and Total are the counts of 7.5, valid while Filtering is
	// set: the group's item rows the filter lets through, of all item rows
	// the group holds.
	Matches int
	Total   int
	// Filtering reports an active filter: the summary slot shows
	// "Matches of Total" instead of the glyph counts.
	Filtering bool
}

// ItemRow is one item line under its group.
type ItemRow struct {
	// Key is the item key: "kit name/item id".
	Key string
	// KitName and ID name the item the row shows.
	KitName string
	ID      string
	// Status is the item's state (3.1): the most specific of the states it
	// holds. A planned conflict outranks all, broken and warning outrank
	// modified, modified outranks the version markers, the version
	// markers outrank the frozen orphan state.
	Status Status
	// Reason is the conflict, broken or warning reason the description
	// column shows in place of the description (3.4).
	Reason string
	// Tags are the item's labels (3.3). Rows without their kit carry none.
	Tags []string
	// Installed reports whether the target holds the item.
	Installed bool
	// InstalledVersion and KitVersion feed the version column: "installed
	// → kit" when they differ (3.4). A row without its kit carries only
	// the installed version.
	InstalledVersion string
	KitVersion       string
	// Description is the item's summary; rows without their kit carry
	// none.
	Description string
	// Major marks a version change across the major version: a behavior
	// change the user reviews before selecting it (7.2).
	Major bool
	// Pending is the action waiting for the apply (3.2), nil when none is
	// set. A removal outranks a reset, a reset outranks a selection.
	Pending *Pending
}

// TreeInput carries the values one tree is built from. Everything is a
// value or a pointer the model only reads: loading the kits, reading the
// lock, snapshotting the target and planning the selection happen before,
// in the caller.
type TreeInput struct {
	// Target is the path of the target the tree shows; the registry keys
	// the pending actions of the target by it.
	Target string
	// Registry is the state registry. Nil behaves as an empty one.
	Registry *state.Registry
	// Lock is the lock of the target. Nil means nothing is installed.
	Lock *lock.Lock
	// Snapshot covers the paths the lock records and the loaded kits
	// contribute to; the model reconciles it with the lock for the
	// modified and broken statuses. Nil skips the reconciliation.
	Snapshot *target.Snapshot
	// Kits are the loaded kits of the target. A kit registered and
	// attached to the target that is missing here is unreadable (6.6).
	Kits []*kit.Kit
	// Plans holds the planner verdicts of the current selection, keyed by
	// item key. A verdict with conflicts makes its item conflicting; a
	// NoAction verdict with a warning makes it warning (3.1). A verdict
	// excluded for plain reasons keeps its base status; the reason shows
	// in the details.
	Plans map[string]*plan.ItemPlan
	// Collapsed holds the group collapse state, kits by name and the
	// orphans group by OrphansKey. A group absent from the map is expanded
	// when readable and collapsed when unreadable (3.4, 6.6).
	Collapsed map[string]bool
	// Filter is the filter text of 7.5; empty shows everything.
	Filter string
}

// BuildTree builds the rows of the body (3.4): one row per kit attached to
// the target, in attachment order, with the kit's item rows under it. The
// installed items a loaded kit no longer provides stay in its group as
// orphans, the items of a kit that did not load show as unavailable under
// the kit's row, and the items of kits not attached to the target form the
// orphans group at the end (6.7); the last row is always the add-kit row. A
// collapsed group keeps its row and its summary; an unreadable kit is
// collapsed by default and shows "unreadable" in place of the summary. With
// an active filter, a group stays visible when any of its items match and
// carries the match counts (7.5).
func BuildTree(in TreeInput) []Row {
	reg := in.Registry
	var names []string
	if reg != nil {
		names = reg.KitsFor(in.Target)
	}
	loaded := make(map[string]*kit.Kit, len(in.Kits))
	for _, k := range in.Kits {
		loaded[k.Name] = k
	}
	var pending *state.Pending
	detaching := map[string]bool{}
	if reg != nil {
		pending = reg.Pending[in.Target]
	}
	if pending != nil {
		for _, d := range pending.Deferred {
			if d.Kind == state.DeferredDetachKit {
				detaching[d.Kit] = true
			}
		}
	}
	rec := &plan.Reconciliation{}
	if in.Lock != nil && in.Snapshot != nil {
		rec = plan.Reconcile(in.Lock, in.Snapshot)
	}
	filter := strings.ToLower(in.Filter)
	filtering := filter != ""

	rows := make([]Row, 0, len(names)+2)
	covered := map[string]bool{}

	// One group per kit attached to the target, in the registry's
	// attachment order: ascending by name, the order KitsFor lists.
	for _, name := range names {
		k := loaded[name]
		g := KitRow{Name: name, Detaching: detaching[name]}
		if kk, ok := reg.Kits[name]; ok {
			g.Path = kk.Dir
		}
		if k == nil {
			g.Unreadable = true
		}
		g.Collapsed = collapseOf(in.Collapsed, name, g.Unreadable)

		var items []ItemRow
		if k != nil {
			for _, it := range k.Items {
				key := kit.ItemKey(name, it.ID)
				covered[key] = true
				items = append(items, kitItemRow(name, it, key, in, rec, pending))
			}
		}
		items = append(items, heldRows(name, in, rec, pending, covered, g.Unreadable)...)

		finishGroup(&g, items, filtering, filter)
		if filtering && g.Matches == 0 {
			continue
		}
		rows = append(rows, Row{Kind: KindKit, Kit: g})
		if g.Collapsed {
			continue
		}
		rows = appendItemRows(rows, items, filtering, filter)
	}

	// The orphans group at the end (6.7): the installed items whose kit is
	// not attached to the target. It shows only when it holds items.
	var orphans []string
	if in.Lock != nil {
		for key := range in.Lock.Items {
			if !covered[key] {
				orphans = append(orphans, key)
			}
		}
	}
	if len(orphans) > 0 {
		sort.Strings(orphans)
		g := KitRow{Name: orphansName, Orphans: true}
		g.Collapsed = collapseOf(in.Collapsed, OrphansKey, false)
		items := make([]ItemRow, 0, len(orphans))
		for _, key := range orphans {
			items = append(items, frozenRow(key, StatusOrphan, "", in, rec, pending))
		}
		finishGroup(&g, items, filtering, filter)
		if !filtering || g.Matches > 0 {
			rows = append(rows, Row{Kind: KindKit, Kit: g})
			if !g.Collapsed {
				rows = appendItemRows(rows, items, filtering, filter)
			}
		}
	}

	return append(rows, Row{Kind: KindAddKit})
}

// appendItemRows appends the group's item rows the filter lets through.
func appendItemRows(rows []Row, items []ItemRow, filtering bool, filter string) []Row {
	for _, r := range items {
		if !filtering || matchRow(r, filter) {
			rows = append(rows, Row{Kind: KindItem, Item: r})
		}
	}
	return rows
}

// heldRows builds the rows of the kit's installed items the kit's own item
// list does not cover: the items of a kit that did not load are
// unavailable, the items of a loaded kit that stopped providing them are
// orphans (9.2). Both stay in the kit's group, frozen: no updates, removal
// only (6.6). An orphan's description says what happened to it; the items
// of an unreadable kit let the kit row speak.
func heldRows(name string, in TreeInput, rec *plan.Reconciliation, pending *state.Pending, covered map[string]bool, unreadable bool) []ItemRow {
	if in.Lock == nil {
		return nil
	}
	prefix := name + "/"
	var keys []string
	for key := range in.Lock.Items {
		if strings.HasPrefix(key, prefix) && !covered[key] {
			keys = append(keys, key)
		}
	}
	if len(keys) == 0 {
		return nil
	}
	sort.Strings(keys)
	base := StatusOrphan
	desc := "removed from the kit"
	if unreadable {
		base = StatusUnavailable
		desc = ""
	}
	rows := make([]ItemRow, 0, len(keys))
	for _, key := range keys {
		covered[key] = true
		rows = append(rows, frozenRow(key, base, desc, in, rec, pending))
	}
	return rows
}

// kitItemRow builds the row of one item a loaded kit provides: the kit's
// facts, the lock record, and the reconciliation statuses decide the state
// (docs/concept.md 8), the planner's verdict adds conflict and warning
// (3.1).
func kitItemRow(kitName string, it *kit.Item, key string, in TreeInput, rec *plan.Reconciliation, pending *state.Pending) ItemRow {
	r := ItemRow{
		Key:         key,
		KitName:     kitName,
		ID:          it.ID,
		Status:      StatusAvailable,
		Tags:        it.Tags,
		KitVersion:  it.Version,
		Description: it.Description,
	}
	var item *lock.Item
	if in.Lock != nil {
		item = in.Lock.Items[key]
	}
	if item == nil {
		r.applyVerdict(in.Plans[key])
		r.Pending = pendingOf(pending, key)
		return r
	}
	r.Installed = true
	r.InstalledVersion = item.Version
	if kit.MajorVersion(it.Version) != kit.MajorVersion(item.Version) {
		r.Major = true
	}
	switch {
	case rec.BrokenItem(key):
		r.Status = StatusBroken
		r.Reason = brokenReason
	case rec.ModifiedItem(key):
		r.Status = StatusModified
	default:
		switch kit.CompareVersions(it.Version, item.Version) {
		case 1:
			r.Status = StatusUpdate
		case -1:
			r.Status = StatusDowngrade
		default:
			r.Status = StatusInstalled
		}
	}
	if ip := in.Plans[key]; ip != nil {
		r.applyVerdict(ip)
	}
	r.Pending = pendingOf(pending, key)
	return r
}

// applyVerdict lifts the planner's verdict for the item onto the row: a
// verdict with conflicts makes the item conflicting, a NoAction verdict
// with a warning makes it warning (3.1). An available item takes the
// verdict the same way an installed one does — the planner speaks about
// the selection, not about the lock.
func (r *ItemRow) applyVerdict(ip *plan.ItemPlan) {
	if ip == nil {
		return
	}
	if len(ip.Conflicts) > 0 {
		r.Status = StatusConflict
		r.Reason = renderConflict(ip.Conflicts[0])
	} else if ip.Status == plan.StatusNoAction && ip.Warning {
		r.Status = StatusWarning
		r.Reason = ip.Reason
	}
}

// frozenRow builds the row of one installed item no kit item backs: the
// version and the pending action come from the lock and the registry, the
// status from the reconciliation — a broken or modified item outranks the
// frozen state it is shown in. desc is the description the row carries;
// frozen rows have no kit item behind them, so it says what the row is.
func frozenRow(key string, base Status, desc string, in TreeInput, rec *plan.Reconciliation, pending *state.Pending) ItemRow {
	kitName, id, _ := strings.Cut(key, "/")
	r := ItemRow{
		Key:         key,
		KitName:     kitName,
		ID:          id,
		Status:      base,
		Installed:   true,
		Description: desc,
	}
	if in.Lock != nil {
		if item := in.Lock.Items[key]; item != nil {
			r.InstalledVersion = item.Version
		}
	}
	switch {
	case rec.BrokenItem(key):
		r.Status = StatusBroken
		r.Reason = brokenReason
	case rec.ModifiedItem(key):
		r.Status = StatusModified
	}
	r.Pending = pendingOf(pending, key)
	return r
}

// pendingOf reads the pending action of one item out of the target's
// pending object: a removal outranks a reset, a reset outranks a selection
// — the same precedence the apply collects them in.
func pendingOf(pending *state.Pending, key string) *Pending {
	if pending == nil {
		return nil
	}
	item := pending.Items[key]
	if item == nil {
		return nil
	}
	var p Pending
	switch {
	case item.Remove:
		p = PendingRemove
	case item.Reset:
		p = PendingReset
	case item.Select:
		p = PendingApply
	default:
		return nil
	}
	return &p
}

// finishGroup fills one group's summary and filter counts from its item
// rows: the summary counts every row of the group by status glyph, in the
// order of the status table (3.1); the counts of 7.5 cover the rows the
// filter lets through, of all rows the group holds.
func finishGroup(g *KitRow, items []ItemRow, filtering bool, filter string) {
	counts := map[Status]int{}
	for _, r := range items {
		counts[r.Status]++
		g.Total++
		if filtering && matchRow(r, filter) {
			g.Matches++
		}
	}
	if !g.Unreadable {
		for s := StatusAvailable; s <= StatusUnavailable; s++ {
			if counts[s] > 0 {
				g.Summary = append(g.Summary, GlyphCount{Status: s, Count: counts[s]})
			}
		}
	}
	g.Filtering = filtering
}

// matchRow reports whether the filter of 7.5 lets the row through: a
// case-insensitive substring over the id, the tags and the description.
func matchRow(r ItemRow, filter string) bool {
	if filter == "" {
		return true
	}
	if strings.Contains(strings.ToLower(r.ID), filter) || strings.Contains(strings.ToLower(r.Description), filter) {
		return true
	}
	for _, tag := range r.Tags {
		if strings.Contains(strings.ToLower(tag), filter) {
			return true
		}
	}
	return false
}

// collapseOf resolves the collapse state of one group: the state the caller
// holds wins; absent state collapses an unreadable kit and expands
// everything else (3.4, 6.6).
func collapseOf(states map[string]bool, name string, unreadable bool) bool {
	if v, ok := states[name]; ok {
		return v
	}
	return unreadable
}

// DefaultAction names the action the space key toggles for a status (7.1):
// install for an available item, update for an update available, downgrade
// for a downgrade available, reset for a broken one. The other states have
// none; the footer names the applicable key instead. The bool reports
// whether the status has a default action at all.
func DefaultAction(s Status) (Pending, bool) {
	switch s {
	case StatusAvailable, StatusUpdate, StatusDowngrade:
		return PendingApply, true
	case StatusBroken:
		return PendingReset, true
	}
	return 0, false
}

// Removable reports whether the d key applies to the row (7.1): any
// installed item, including orphans and the items of unreadable kits.
func Removable(r ItemRow) bool { return r.Installed }

// Resettable reports whether the r key applies to the row (7.1): the states
// modified, conflict and broken.
func Resettable(r ItemRow) bool {
	return r.Status == StatusModified || r.Status == StatusConflict || r.Status == StatusBroken
}
