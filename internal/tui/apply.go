package tui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/dpleshakov/penates/internal/apply"
	"github.com/dpleshakov/penates/internal/kit"
	"github.com/dpleshakov/penates/internal/plan"
	"github.com/dpleshakov/penates/internal/state"
	"github.com/dpleshakov/penates/internal/target"
)

// The apply flow the main screen drives: the pending actions of the target
// collect into one apply, the summary confirms, and the perform plumbing
// applies with the same ordering as before — removals first, the installs
// planned from the target as the removals leave it. Nothing asks for
// variables here: the forms live at selection time (7.3), and a selection
// whose required answers went missing plans as excluded; the summary keeps
// its format until task 09.

// pendingApply is one apply waiting on the summary's confirmation: the
// pending actions it was built from, and the paths its planning touches.
type pendingApply struct {
	selection   []string
	removals    []string
	resets      map[string]bool
	paths       []string
	sharedPaths []string
}

// beginApply collects what the target's pending actions would apply and
// moves to the summary it waits on.
func (m *model) beginApply() {
	p := m.collectApply()
	if len(p.selection) == 0 && len(p.removals) == 0 {
		m.lastResults = "nothing selected\n"
		m.screen = screenResults
		return
	}
	m.buildSummary(p)
}

// collectApply gathers the target's pending actions into one pending apply.
// A removal wins over a held selection: the removed item does not join the
// installs of the same apply. The paths cover the selected items' owned
// files, the kits' shared files, and the removals' recorded files and
// shared files, so the planning sees everything it may touch.
func (m *model) collectApply() *pendingApply {
	p := &pendingApply{resets: map[string]bool{}}
	if pending := m.pendingActions(); pending != nil {
		for key, item := range pending.Items {
			switch {
			case item.Remove:
				p.removals = append(p.removals, key)
			case item.Select:
				p.selection = append(p.selection, key)
			}
			if item.Reset {
				p.resets[key] = true
			}
		}
	}
	sort.Strings(p.selection)
	sort.Strings(p.removals)

	var paths []string
	seen := map[string]bool{}
	sharedSeen := map[string]bool{}
	var sharedPaths []string
	for _, k := range m.kits {
		for _, s := range k.Shared {
			if !sharedSeen[s] {
				sharedSeen[s] = true
				sharedPaths = append(sharedPaths, s)
			}
		}
	}
	for _, key := range p.selection {
		kitName, itemID, _ := strings.Cut(key, "/")
		for _, k := range m.kits {
			if k.Name != kitName {
				continue
			}
			for _, it := range k.Items {
				if it.ID != itemID {
					continue
				}
				for _, c := range it.Contributes {
					if c.Kind == kit.KindFile && !seen[c.To] {
						seen[c.To] = true
						paths = append(paths, c.To)
					}
				}
			}
		}
	}

	// A removal is planned from the lock: its files and shared files join the
	// snapshot, for the items of kits that are gone as well.
	if m.lk != nil {
		for _, key := range p.removals {
			rec := m.lk.Items[key]
			if rec == nil {
				continue
			}
			for path := range rec.Files {
				if !seen[path] {
					seen[path] = true
					paths = append(paths, path)
				}
			}
			for file := range rec.Sections {
				if !sharedSeen[file] {
					sharedSeen[file] = true
					sharedPaths = append(sharedPaths, file)
				}
			}
			for file := range rec.JSON {
				if !sharedSeen[file] {
					sharedSeen[file] = true
					sharedPaths = append(sharedPaths, file)
				}
			}
		}
	}
	sort.Strings(paths)
	sort.Strings(sharedPaths)
	p.paths = paths
	p.sharedPaths = sharedPaths
	return p
}

// buildSummary plans the pending apply without writing anything and shows
// what it would do: every change per item with the reason of each action,
// the removals with the content they leave behind, the items kept, and every
// excluded item with its reasons. The apply runs only after the
// screen's confirmation, and plans again from the target of that moment.
func (m *model) buildSummary(p *pendingApply) {
	m.pending = p
	snap, err := target.Read(m.target, p.paths, p.sharedPaths)
	if err != nil {
		m.lastResults = "apply failed: " + err.Error()
		m.screen = screenResults
		return
	}
	var installs, removals []*plan.ItemPlan
	if len(p.removals) > 0 {
		remove := map[string]bool{}
		for _, key := range p.removals {
			remove[key] = true
		}
		removals = plan.BuildRemoval(plan.Input{Lock: m.lk, Snapshot: snap, Remove: remove}).Items
	}
	if len(p.selection) > 0 {
		sources, err := kit.ReadSources(m.kits, p.selection)
		if err != nil {
			m.lastResults = "apply failed: " + err.Error()
			m.screen = screenResults
			return
		}
		installs = plan.Build(plan.Input{
			Kits:       m.kits,
			Selection:  p.selection,
			Lock:       m.lk,
			Snapshot:   snap,
			Sources:    sources,
			Answers:    m.currentAnswers(),
			Reset:      p.resets,
			Reconciled: m.reconcil,
		}).Items
	}
	m.summary = renderSummary(installs, removals)
	m.screen = screenSummary
}

// renderSummary lays out the plans of one pending apply: every
// change per item with the reason of each action, the removals with the
// content they leave behind, the items kept, and every excluded item with
// its conflicts and reasons. An install that writes no byte still takes
// ownership of its JSON locations; the summary names them.
func renderSummary(installs, removals []*plan.ItemPlan) string {
	var changes, kept, excluded, removed []*plan.ItemPlan
	for _, ip := range installs {
		switch ip.Status {
		case plan.StatusInstall:
			changes = append(changes, ip)
		case plan.StatusNoAction:
			kept = append(kept, ip)
		case plan.StatusError:
			excluded = append(excluded, ip)
		}
	}
	for _, ip := range removals {
		if ip.Status == plan.StatusError {
			excluded = append(excluded, ip)
			continue
		}
		removed = append(removed, ip)
	}

	var b strings.Builder
	writeActions := func(items []*plan.ItemPlan) {
		for _, ip := range items {
			fmt.Fprintf(&b, "  %s %s\n", ip.Key, ip.Version)
			for _, a := range ip.Actions {
				fmt.Fprintf(&b, "    %-14s %-44s %s\n", string(a.Kind), a.To, a.Reason)
			}
			if len(ip.Actions) == 0 {
				files := make([]string, 0, len(ip.JSONLocations))
				for f := range ip.JSONLocations {
					files = append(files, f)
				}
				sort.Strings(files)
				for _, f := range files {
					for _, loc := range ip.JSONLocations[f] {
						fmt.Fprintf(&b, "    %-14s %-44s takes ownership of %s\n", string(plan.ActionJSON), f, loc.Pointer)
					}
				}
			}
			for _, report := range ip.Reports {
				fmt.Fprintf(&b, "    %s\n", report)
			}
		}
		b.WriteString("\n")
	}
	if len(changes) > 0 {
		b.WriteString("changes:\n")
		writeActions(changes)
	}
	if len(removed) > 0 {
		b.WriteString("removals:\n")
		writeActions(removed)
	}
	if len(kept) > 0 {
		b.WriteString("kept:\n")
		for _, ip := range kept {
			reason := ip.Reason
			if ip.Warning {
				reason = "warning: " + reason
			}
			fmt.Fprintf(&b, "  %s %s   %s\n", ip.Key, ip.Version, reason)
		}
		b.WriteString("\n")
	}
	if len(excluded) > 0 {
		b.WriteString("excluded:\n")
		for _, ip := range excluded {
			fmt.Fprintf(&b, "  %s\n", ip.Key)
			for _, line := range exclusionLines(ip) {
				fmt.Fprintf(&b, "    %s\n", line)
			}
		}
		b.WriteString("\n")
	}
	return b.String()
}

// performApply runs the apply the summary confirmed: removals first, then
// the installs. Both plan again from a fresh snapshot: the removals rewrite
// and delete what the summary's snapshot observed, so a removed item's
// content cannot rise again in a shared file. The apply runs with a nil
// progress callback — the progress view of 10.2 is task 09.
func (m *model) performApply(p *pendingApply) {
	m.pending = nil
	snap, err := target.Read(m.target, p.paths, p.sharedPaths)
	if err != nil {
		m.lastResults = "apply failed: " + err.Error()
		m.screen = screenResults
		return
	}

	var b strings.Builder
	removed := map[string]bool{}

	// The removals go first: they shrink what the installs write into.
	if len(p.removals) > 0 {
		remove := map[string]bool{}
		for _, key := range p.removals {
			remove[key] = true
		}
		rp := plan.BuildRemoval(plan.Input{Lock: m.lk, Snapshot: snap, Remove: remove})
		results := apply.Remove(m.target, rp, m.lk, m.app.Locks, nil)
		errors := map[string]string{}
		for _, r := range results {
			if r.Err != nil {
				errors[r.Key] = r.Err.Error()
				continue
			}
			// A removed item drops its pending entry: the next apply
			// collects no removal for it again, and does not reinstall it
			// through a held selection.
			removed[r.Key] = true
		}
		for _, ip := range rp.Items {
			switch ip.Status {
			case plan.StatusRemove:
				if err := errors[ip.Key]; err != "" {
					fmt.Fprintf(&b, "failed   %s: %s\n", ip.Key, err)
				} else {
					fmt.Fprintf(&b, "removed  %s\n", ip.Key)
				}
			case plan.StatusError:
				fmt.Fprintf(&b, "excluded %s:\n", ip.Key)
				for _, line := range exclusionLines(ip) {
					fmt.Fprintf(&b, "         %s\n", line)
				}
			}
			for _, report := range ip.Reports {
				fmt.Fprintf(&b, "         %s\n", report)
			}
		}
		if lk, err := m.app.Locks.Load(m.target); err == nil {
			m.lk = lk
		}
		// The removals rewrote and deleted what the snapshot observed: the
		// installs plan from the target as the removals leave it, so a
		// removed item's content cannot rise again in a shared file.
		if len(p.selection) > 0 {
			snap, err = target.Read(m.target, p.paths, p.sharedPaths)
			if err != nil {
				m.lastResults = "apply failed: " + err.Error()
				m.screen = screenResults
				return
			}
		}
	}

	if len(p.selection) > 0 {
		sources, err := kit.ReadSources(m.kits, p.selection)
		if err != nil {
			m.lastResults = "apply failed: " + err.Error()
			m.screen = screenResults
			return
		}
		planned := plan.Build(plan.Input{
			Kits:       m.kits,
			Selection:  p.selection,
			Lock:       m.lk,
			Snapshot:   snap,
			Sources:    sources,
			Answers:    m.currentAnswers(),
			Reset:      p.resets,
			Reconciled: m.reconcil,
		})
		results := apply.Apply(m.target, planned, m.lk, m.app.Locks, nil)
		errors := map[string]string{}
		for _, r := range results {
			if r.Err != nil {
				errors[r.Key] = r.Err.Error()
			}
		}

		for _, ip := range planned.Items {
			switch ip.Status {
			case plan.StatusInstall:
				if err := errors[ip.Key]; err != "" {
					fmt.Fprintf(&b, "failed   %s: %s\n", ip.Key, err)
				} else {
					fmt.Fprintf(&b, "applied  %s %s\n", ip.Key, ip.Version)
				}
			case plan.StatusNoAction:
				fmt.Fprintf(&b, "kept     %s: %s\n", ip.Key, ip.Reason)
			case plan.StatusError:
				fmt.Fprintf(&b, "excluded %s:\n", ip.Key)
				for _, line := range exclusionLines(ip) {
					fmt.Fprintf(&b, "         %s\n", line)
				}
			}
		}
	}

	if b.Len() == 0 {
		b.WriteString("nothing selected\n")
	}
	m.lastResults = b.String()

	// The selection as applied — the removed items dropped out of it —
	// replaces the remembered one, so a removed item is not offered selected
	// again; the resets and removals that ran are consumed.
	m.settleAfterApply(removed, p.selection)

	// The deferred operations the apply covered complete here: a kit whose
	// removals landed detaches from the target, one it did not cover is
	// dropped with the reason in the report (6.7).
	m.completeDeferred()

	if lk, err := m.app.Locks.Load(m.target); err == nil {
		m.lk = lk
	}
	m.screen = screenResults
}

// completeDeferred finishes the deferred operations of the target after
// the apply (6.7): the lock as the apply left it decides — a deferred
// detach whose kit lost its last installed item completes, one the apply
// did not finish is dropped with its reason. The report joins the results,
// the completed operations as well as the dropped ones.
func (m *model) completeDeferred() {
	lk, err := m.app.Locks.Load(m.target)
	if err != nil {
		m.lastResults += "\ncould not complete the deferred operations: " + err.Error()
		return
	}
	m.lk = lk
	installed := make([]string, 0, len(lk.Items))
	for key := range lk.Items {
		installed = append(installed, key)
	}
	sort.Strings(installed)
	report, err := m.app.State.CompleteDeferred(m.target, installed, m.app.Locks)
	if err != nil {
		m.lastResults += "\ncould not complete the deferred operations: " + err.Error()
		return
	}
	for _, line := range report {
		m.lastResults += line + "\n"
	}
	if len(report) > 0 {
		if err := m.reloadRegistry(); err != nil {
			m.lastResults += "\ncould not reload the state: " + err.Error()
		}
	}
}

// settleAfterApply folds the apply into the target's pending actions: the
// applied selection stays remembered as the active selection, a reset and a
// completed removal are consumed, and the answers of an item that no longer
// holds a selection go with it.
func (m *model) settleAfterApply(removed map[string]bool, selection []string) {
	selected := map[string]bool{}
	for _, key := range selection {
		selected[key] = true
	}
	err := m.app.State.Update(func(reg *state.Registry) error {
		p, ok := reg.Pending[m.target]
		if !ok {
			return nil
		}
		for key, item := range p.Items {
			if removed[key] {
				delete(p.Items, key)
				continue
			}
			item.Reset = false
			if !selected[key] {
				item.Select = false
				item.Answers = nil
			}
			if !item.Select && !item.Reset && !item.Remove && len(item.Answers) == 0 {
				delete(p.Items, key)
			}
		}
		if len(p.Items) == 0 && len(p.Deferred) == 0 {
			delete(reg.Pending, m.target)
		}
		return nil
	})
	if err != nil {
		m.lastResults += "\ncould not save the state: " + err.Error()
		return
	}
	if err := m.reloadRegistry(); err != nil {
		m.lastResults += "\ncould not reload the state: " + err.Error()
	}
}
