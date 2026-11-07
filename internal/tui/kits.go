package tui

import (
	"fmt"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/dpleshakov/penates/internal/state"
)

// The kit management flows of the main screen (tui-screens.md 6): detaching
// (6.7) first; adding a kit and the relocation of 6.6 arrive with the path
// input. The flows drive the state operations of the core — DetachKit,
// DeferDetachKit — and hold no state logic of their own.

// detachUnderCursor opens the detach flow of the kit row under the cursor
// (6.7): a kit with installed items in the target asks what happens to
// them, a kit without any detaches at once with a notice. The orphans
// group is not a kit, and a kit already detaching waits for its apply.
func (m *model) detachUnderCursor() tea.Cmd {
	if len(m.rows) == 0 {
		return nil
	}
	r := m.rows[m.cursor]
	if r.Kind != KindKit || r.Kit.Orphans || r.Kit.Detaching {
		return nil
	}
	name := r.Kit.Name
	installed := m.installedItems(name)
	if len(installed) == 0 {
		return m.detachKit(name)
	}
	m.detachName = name
	m.dialog = &dialog{
		kind:  dialogDetach,
		title: "Detach " + name,
		body:  detachDialogBody(installed, m.target),
		options: []string{
			"Remove its items from the target",
			"Keep its items installed as orphans",
			"Cancel",
		},
		// The default is Cancel: the question is destructive, and the
		// answer must never happen by enter alone (6.7).
		chosen: 2,
	}
	return nil
}

// installedItem is one installed item of a kit in the current target: the
// item key and the version the lock records.
type installedItem struct {
	key     string
	id      string
	version string
}

// installedItems lists the kit's installed items in the target, in
// ascending key order, the way the lock records them.
func (m *model) installedItems(kitName string) []installedItem {
	if m.lk == nil {
		return nil
	}
	prefix := kitName + "/"
	var out []installedItem
	for key, rec := range m.lk.Items {
		if !strings.HasPrefix(key, prefix) {
			continue
		}
		id, _, _ := strings.Cut(strings.TrimPrefix(key, prefix), "/")
		v := ""
		if rec != nil {
			v = rec.Version
		}
		out = append(out, installedItem{key: key, id: id, version: v})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].key < out[j].key })
	return out
}

// detachDialogBody names what the detach dialog asks about (6.7): how
// many items of the kit are installed in the target, then the items with
// their versions.
func detachDialogBody(items []installedItem, target string) []string {
	noun, verb := "items", "are"
	if len(items) == 1 {
		noun, verb = "item", "is"
	}
	var list []string
	for _, it := range items {
		list = append(list, strings.TrimSpace(it.id+" "+it.version))
	}
	return append(
		[]string{fmt.Sprintf("%d %s of this kit %s installed in %s:", len(items), noun, verb, target)},
		wrapIndent(strings.Join(list, ", "), "  ", "  ", ProseLimit)...,
	)
}

// detachWithRemovals marks the kit's installed items for removal and
// queues the detach behind the apply that removes them (6.7): the row
// shows "detaching" until then, and CompleteDeferred finishes the detach
// after the apply.
func (m *model) detachWithRemovals(kitName string) tea.Cmd {
	for _, it := range m.installedItems(kitName) {
		if err := m.editPending(it.key, func(ip *state.ItemPending) { ip.Remove = true }); err != nil {
			return m.showNotice(err.Error())
		}
	}
	if err := m.app.State.DeferDetachKit(kitName, m.target); err != nil {
		return m.showNotice(err.Error())
	}
	if err := m.reloadRegistry(); err != nil {
		return m.showNotice(err.Error())
	}
	m.refresh()
	return nil
}

// detachKit detaches the kit from the target at once (6.7): the files in
// the target are not touched, its installed items move to the orphans
// group, and the notice confirms the change (11.2). The undo offer rides
// on the same notice once the u key works (task 10).
func (m *model) detachKit(kitName string) tea.Cmd {
	if err := m.app.State.DetachKit(kitName, m.target); err != nil {
		return m.showNotice(err.Error())
	}
	if err := m.reloadRegistry(); err != nil {
		return m.showNotice(err.Error())
	}
	m.refresh()
	return m.showNotice(fmt.Sprintf("Detached %s.", kitName))
}
