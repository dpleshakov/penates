package tui

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/charmbracelet/x/ansi"
	"golang.org/x/mod/semver"

	"github.com/dpleshakov/penates/internal/kit"
	"github.com/dpleshakov/penates/internal/lock"
	"github.com/dpleshakov/penates/internal/plan"
	"github.com/dpleshakov/penates/internal/state"
)

// The content the details pane and the details view render (tui-screens.md
// 5): the same lines for the row under the cursor on a wide terminal and on
// `enter`. The renderers are pure functions of the tree rows the model
// built plus the kit, the lock record, the plan verdict and the stored
// answers; they perform no I/O — the caller reads the changelog. Prose
// wraps at no more than 100 columns regardless of the width (2.2), and
// ScrollWindow cuts the window the view shows out of the full lines.

// ItemDetails carries the values one item's details render from (5.1): the
// tree row of 3.4 and the values the tree input holds besides it. A frozen
// row renders from the Row alone: Item, Plan and the lock record stay out.
type ItemDetails struct {
	// Row is the tree row the details follow.
	Row ItemRow
	// Item is the kit item behind the row: its variables and its
	// contributions. Nil for the rows without their kit: the frozen rows
	// (9.2) and the items of an unreadable kit.
	Item *kit.Item
	// Plan is the verdict of the current selection on the item; its
	// structured conflicts render as the reasons of the Status section.
	// Nil when nothing was planned for the item.
	Plan *plan.ItemPlan
	// Lock is the lock of the target: the stored answers and the owned
	// locations of an installed item. Nil means nothing is installed.
	Lock *lock.Lock
	// Answers are the variable answers given at selection time; they win
	// over the ones recorded in the lock, as they do at planning.
	Answers map[string]string
	// Changelog holds the release notes of the item's CHANGELOG.md, read
	// by the caller (5.1: Changelog when CHANGELOG.md is present).
	Changelog []ChangelogEntry
}

// ChangelogEntry is one release note of an item's changelog: the version
// the entry names and the change it reports (5.1).
type ChangelogEntry struct {
	Version string
	Note    string
}

// KitDetails carries the values one kit group's details render from (5.2).
type KitDetails struct {
	// Row is the tree row of the group.
	Row KitRow
	// Kit is the loaded kit behind the group; nil for the orphans group
	// and for a kit that did not load (6.6).
	Kit *kit.Kit
	// Registry is the state registry: it names the other targets the kit
	// is attached to. Nil behaves as an empty one.
	Registry *state.Registry
	// Target is the path of the current target, left out of the
	// also-attached list.
	Target string
}

// statusLabelPad is the width of the labeled field of the item details: the
// Status, Pending and Tags lines start their content at the tenth column
// (4.1, 5.1).
const statusLabelPad = 10

// statusNames spells the statuses the way the table of 3.1 names them.
var statusNames = [...]string{
	StatusAvailable:   "available",
	StatusInstalled:   "installed",
	StatusUpdate:      "update",
	StatusDowngrade:   "downgrade",
	StatusModified:    "modified",
	StatusConflict:    "conflict",
	StatusBroken:      "broken",
	StatusWarning:     "warning",
	StatusOrphan:      "orphan",
	StatusUnavailable: "unavailable",
}

// statusName is the name of one status.
func statusName(s Status) string { return statusNames[s] }

// RenderItemDetails renders the details content of one item (5.1): the
// sections of the spec in order, each left out when it holds nothing. The
// lines are full-length; the pane and the view clip or scroll them. Prose
// wraps at no more than ProseLimit columns regardless of width.
func RenderItemDetails(d ItemDetails, width int, set Set) []string {
	r := d.Row
	lines := []string{r.ID + " · " + r.KitName}
	lines = addSection(lines, WrapProse(r.Description, width))
	lines = addSection(lines, itemStatusSection(d, width, set))
	lines = addSection(lines, itemPendingSection(r, set))
	lines = addSection(lines, itemTagsSection(r))
	lines = addSection(lines, itemVariablesSection(d, width))
	lines = addSection(lines, itemContributesSection(d))
	if r.Major {
		lines = addSection(lines, majorNote(width, set))
	}
	lines = addSection(lines, itemChangelogSection(d, width))
	return lines
}

// itemStatusSection is the Status section (5.1): the status glyph and name,
// then the reason of a non-installable or modified state with the next step
// the user takes. A conflict flows the step into its reason paragraph; the
// other states give it a paragraph of its own.
func itemStatusSection(d ItemDetails, width int, set Set) []string {
	r := d.Row
	head := r.Status.Mark(set) + " " + statusName(r.Status)
	if versioned(r.Status) && r.InstalledVersion != "" && r.KitVersion != "" && r.InstalledVersion != r.KitVersion {
		head += ", " + r.InstalledVersion + " → " + r.KitVersion
		if r.Status == StatusUpdate {
			head += " (" + versionChange(r.InstalledVersion, r.KitVersion) + ")"
		}
	}
	label := fmt.Sprintf("%-*s", statusLabelPad, "Status")
	lines := []string{label + head}

	paras, step := itemReason(d)
	if len(paras) == 0 {
		return lines
	}
	indent := strings.Repeat(" ", statusLabelPad)
	if step != "" && r.Status == StatusConflict {
		paras[len(paras)-1] = joinSentence(paras[len(paras)-1], step)
		step = ""
	}
	for _, para := range paras {
		lines = append(lines, wrapIndent(para, indent, indent, width)...)
	}
	if step != "" {
		lines = append(lines, "")
		lines = append(lines, wrapIndent(step, indent, indent, width)...)
	}
	return lines
}

// versioned reports whether a status carries the version pair in its line:
// an update or a downgrade between the installed version and the kit's
// (4.1: update, 1.2.0 → 1.3.0 (minor)).
func versioned(s Status) bool { return s == StatusUpdate || s == StatusDowngrade }

// itemReason is the reason content of the Status section (5.1): the
// paragraphs that state the situation, and the next step the user takes. A
// conflict renders from the structured causes of the plan, one paragraph
// per conflict; the step comes from the first cause that names one.
func itemReason(d ItemDetails) (paras []string, step string) {
	r := d.Row
	switch r.Status {
	case StatusConflict:
		if d.Plan != nil {
			for _, c := range d.Plan.Conflicts {
				if s := renderConflict(c); s != "" {
					paras = append(paras, s)
					if step == "" {
						step = conflictNextStep(c)
					}
				}
			}
		}
		if len(paras) == 0 && r.Reason != "" {
			paras = append(paras, r.Reason)
		}
	case StatusModified:
		paras = append(paras, "The installed content was changed after installation. "+
			"The kit content is unchanged; your version is kept on updates.")
		step = statusNextStep(r.Status)
	case StatusBroken:
		paras = append(paras, brokenReason)
		step = statusNextStep(r.Status)
	case StatusWarning:
		if r.Reason != "" {
			paras = append(paras, r.Reason)
		}
	}
	return paras, step
}

// statusNextStep is the advice a non-installable or modified state gives
// after its reason (5.1): the key that acts on the state. A state with no
// acting key names none — a warning leaves the content installed as
// recorded, and its reason stands without advice (7.1: no reset, no
// default action).
func statusNextStep(s Status) string {
	switch s {
	case StatusModified:
		return "r resets the file to the kit version."
	case StatusBroken:
		return "space selects the item; the apply installs its content again."
	}
	return ""
}

// conflictNextStep is the advice a conflict's cause gives (5.1): what to do
// about the content standing in the way. An unknown cause names none.
func conflictNextStep(c plan.Conflict) string {
	switch c.Cause {
	case plan.CauseUserContent:
		return "Remove or rename it, then rescan with R."
	case plan.CauseOwned:
		return "Select it to offer replacing the installed owner."
	case plan.CauseModified:
		return "r resets the file to the kit version."
	case plan.CauseClaimed, plan.CauseClaimedAsFile, plan.CauseClaimedAsJSON, plan.CauseClaimedAsFileOrSection:
		return "Select only one of the rival items."
	}
	return ""
}

// joinSentence appends a next step to a reason sentence: a period joins the
// two, flowing as one paragraph (5.1: the conflict example).
func joinSentence(reason, step string) string {
	return strings.TrimSuffix(reason, ".") + ". " + step
}

// itemPendingSection is the Pending section (5.1): the action's glyph and
// the exact verb of what the apply will do (3.2). Absent without a pending
// action.
func itemPendingSection(r ItemRow, set Set) []string {
	if r.Pending == nil {
		return nil
	}
	label := fmt.Sprintf("%-*s", statusLabelPad, "Pending")
	return []string{label + r.Pending.Mark(set) + " " + pendingVerb(r)}
}

// pendingVerb names what the pending action does to this item: install,
// update to, downgrade to, reset to kit, remove (4.1, 3.2).
func pendingVerb(r ItemRow) string {
	switch *r.Pending {
	case PendingReset:
		return "reset to kit"
	case PendingRemove:
		return "remove"
	}
	switch r.Status {
	case StatusAvailable:
		return "install " + r.KitVersion
	case StatusUpdate:
		return "update to " + r.KitVersion
	case StatusDowngrade:
		return "downgrade to " + r.KitVersion
	case StatusBroken:
		return "install " + r.InstalledVersion
	}
	return "apply"
}

// itemTagsSection is the Tags section (5.1). Absent without tags.
func itemTagsSection(r ItemRow) []string {
	if len(r.Tags) == 0 {
		return nil
	}
	label := fmt.Sprintf("%-*s", statusLabelPad, "Tags")
	return []string{label + Dim(strings.Join(r.Tags, ", "))}
}

// itemVariablesSection is the Variables section (5.1): the declared
// variables with their stored answers, the answers of the selection over
// the ones recorded in the lock. Absent for an item without variables.
func itemVariablesSection(d ItemDetails, width int) []string {
	if d.Item == nil || len(d.Item.Vars) == 0 {
		return nil
	}
	rec := d.record()
	lines := []string{"Variables"}
	for _, v := range d.Item.Vars {
		answer, ok := d.Answers[v.ID]
		if !ok && rec != nil {
			answer, ok = rec.Answers[v.ID]
		}
		var text string
		switch {
		case ok && answer != "":
			text = v.ID + " = " + answer
		case v.Required:
			text = v.ID + " (required, no answer)"
		default:
			text = v.ID + " (optional)"
		}
		lines = append(lines, wrapIndent(text, "  ", "  ", width)...)
	}
	return lines
}

// itemContributesSection is the Contributes section (5.1): every
// contribution with its kind and its destination, a section contribution
// with its managed block, a json one with the pointers it owns. The lock
// records the locations of an installed item; the plan carries them for a
// selected one. Absent for a row without its kit.
func itemContributesSection(d ItemDetails) []string {
	if d.Item == nil || len(d.Item.Contributes) == 0 {
		return nil
	}
	rec := d.record()
	kindW := 0
	for _, c := range d.Item.Contributes {
		if w := len(string(c.Kind)); w > kindW {
			kindW = w
		}
	}
	entry := func(kind kit.ContributionKind, to, where string) string {
		line := "  " + fmt.Sprintf("%-*s", kindW, string(kind)) + "  " + to
		if where != "" {
			line += "  " + where
		}
		return line
	}
	lines := []string{"Contributes"}
	for _, c := range d.Item.Contributes {
		switch c.Kind {
		case kit.KindSection:
			lines = append(lines, entry(c.Kind, c.To, sectionBlock(rec, d.Row.Key, c.To)))
		case kit.KindJSON:
			if ptrs := jsonPointers(rec, d.Plan, c.To); len(ptrs) > 0 {
				for _, ptr := range ptrs {
					lines = append(lines, entry(c.Kind, c.To, ptr))
				}
				continue
			}
			lines = append(lines, entry(c.Kind, c.To, ""))
		default:
			lines = append(lines, entry(c.Kind, c.To, ""))
		}
	}
	return lines
}

// record is the lock record of the item; nil when the item is not
// installed or the lock is absent.
func (d ItemDetails) record() *lock.Item {
	if d.Lock == nil {
		return nil
	}
	return d.Lock.Items[d.Row.Key]
}

// sectionBlock names the managed block of one section contribution: the
// lock records it for an installed item; otherwise the planner writes the
// item key (plan: the managed block identifier is the item key).
func sectionBlock(rec *lock.Item, key, file string) string {
	if rec != nil {
		if sec := rec.Sections[file]; sec.Block != "" {
			return sec.Block
		}
	}
	return key
}

// jsonPointers lists the pointers of the item's owned locations in one
// shared file, sorted and without duplicates. The lock records them for an
// installed item; the plan carries them, or their new shape, for a selected
// one.
func jsonPointers(rec *lock.Item, p *plan.ItemPlan, file string) []string {
	seen := map[string]bool{}
	var out []string
	add := func(ptr string) {
		if ptr != "" && !seen[ptr] {
			seen[ptr] = true
			out = append(out, ptr)
		}
	}
	if rec != nil {
		for _, loc := range rec.JSON[file] {
			add(loc.Pointer)
		}
	}
	if len(out) == 0 && p != nil {
		for _, loc := range p.JSONLocations[file] {
			add(loc.Pointer)
		}
	}
	sort.Strings(out)
	return out
}

// majorNote is the review note of 7.2, rendered above the changelog of a
// major update: the second space selects the update.
func majorNote(width int, set Set) []string {
	return wrapIndent(
		"A major version changes behavior. Review the changelog below, then press space again to select.",
		StatusWarning.Mark(set)+" ", "  ", width,
	)
}

// itemChangelogSection is the Changelog section (5.1): every release note
// under its version. Absent without notes.
func itemChangelogSection(d ItemDetails, width int) []string {
	if len(d.Changelog) == 0 {
		return nil
	}
	vw := 0
	for _, e := range d.Changelog {
		if len(e.Version) > vw {
			vw = len(e.Version)
		}
	}
	lines := []string{"Changelog"}
	for _, e := range d.Changelog {
		label := "  " + fmt.Sprintf("%-*s", vw, e.Version) + "  "
		lines = append(lines, wrapIndent(e.Note, label, "  "+strings.Repeat(" ", vw+2), width)...)
	}
	return lines
}

// RenderKitDetails renders the details content of one kit group (5.2): the
// description and the fact rows. The rows the group holds no facts for drop
// out; a kit that did not load shows its path alone.
func RenderKitDetails(k KitDetails, width int, set Set) []string {
	lines := []string{k.Row.Name}
	if k.Kit != nil {
		lines = addSection(lines, WrapProse(k.Kit.Description, width))
	}
	var fields []detailField
	if k.Row.Path != "" {
		fields = append(fields, detailField{"Path", []string{k.Row.Path}})
	}
	if k.Kit != nil {
		if k.Kit.TargetHint != "" {
			fields = append(fields, detailField{"Target hint", []string{k.Kit.TargetHint}})
		}
		if len(k.Kit.Shared) > 0 {
			fields = append(fields, detailField{"Shared files", []string{strings.Join(k.Kit.Shared, ", ")}})
		}
	}
	if s := itemsLine(k.Row); s != "" {
		fields = append(fields, detailField{"Items", []string{s}})
	}
	if t := alsoAttached(k.Registry, k.Row.Name, k.Target); len(t) > 0 {
		fields = append(fields, detailField{"Also attached", t})
	}
	lines = addSection(lines, renderFields(fields))
	return lines
}

// detailField is one label of the kit details and its values: a value with
// several lines continues under the first (5.2: Also attached).
type detailField struct {
	label  string
	values []string
}

// renderFields renders the fact rows of the kit details (5.2): the labels
// left-justified to the widest one, the values dim (3.3).
func renderFields(fields []detailField) []string {
	if len(fields) == 0 {
		return nil
	}
	labelW := 0
	for _, f := range fields {
		if w := len(f.label); w > labelW {
			labelW = w
		}
	}
	out := make([]string, 0, len(fields))
	for _, f := range fields {
		blank := strings.Repeat(" ", labelW+2)
		out = append(out, fmt.Sprintf("%-*s", labelW, f.label)+"  "+Dim(f.values[0]))
		for _, v := range f.values[1:] {
			out = append(out, blank+Dim(v))
		}
	}
	return out
}

// itemsLine is the Items row of the kit details (5.2): the group's item
// rows counted, then by status — "4: 2 installed, 1 update, 1 conflict".
func itemsLine(g KitRow) string {
	if len(g.Summary) == 0 {
		return ""
	}
	parts := make([]string, 0, len(g.Summary))
	for _, gc := range g.Summary {
		parts = append(parts, strconv.Itoa(gc.Count)+" "+statusName(gc.Status))
	}
	return strconv.Itoa(g.Total) + ": " + strings.Join(parts, ", ")
}

// alsoAttached lists the other targets the kit is attached to, sorted and
// without duplicates (5.2).
func alsoAttached(reg *state.Registry, kitName, target string) []string {
	if reg == nil {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	for _, a := range reg.Attachments {
		if a.Kit != kitName || a.Target == target || seen[a.Target] {
			continue
		}
		seen[a.Target] = true
		out = append(out, a.Target)
	}
	sort.Strings(out)
	return out
}

// versionChange names the increment between the installed version and the
// kit version: the parenthesized kind of the update status (4.1, 7.2).
func versionChange(from, to string) string {
	a, b := "v"+from, "v"+to
	switch {
	case semver.Major(a) != semver.Major(b):
		return "major"
	case semver.MajorMinor(a) != semver.MajorMinor(b):
		return "minor"
	}
	return "patch"
}

// addSection appends one section, separated from the previous one by a
// blank line; an empty section drops out (5.1: empty sections are omitted).
func addSection(lines, section []string) []string {
	if len(section) == 0 {
		return lines
	}
	if len(lines) == 0 {
		return section
	}
	return append(append(lines, ""), section...)
}

// wrapIndent wraps one paragraph of prose and prefixes the lines: the first
// line gets first, the rest rest — the continuation of a labeled or
// indented paragraph stays under its start. The prose never grows past
// ProseLimit columns of content, whatever the width (2.2).
func wrapIndent(text, first, rest string, width int) []string {
	if text == "" {
		return nil
	}
	pad := ansi.StringWidth(first)
	w := ProseWidth(width - pad)
	if lim := ProseLimit - pad; lim < w {
		w = lim
	}
	wrapped := WrapProse(text, w)
	out := make([]string, 0, len(wrapped))
	for i, line := range wrapped {
		p := rest
		if i == 0 {
			p = first
		}
		out = append(out, p+line)
	}
	return out
}

// ScrollOffset clamps a scroll offset into the range a window of height
// lines over total lines may take: the window never starts past the last
// full page, and content shorter than the window does not scroll.
func ScrollOffset(offset, total, height int) int {
	if height < 1 {
		return 0
	}
	limit := total - height
	if limit < 0 {
		limit = 0
	}
	if offset > limit {
		offset = limit
	}
	if offset < 0 {
		offset = 0
	}
	return offset
}

// ScrollWindow returns the window the details view shows for a scroll
// offset: height lines out of the full lines, clamped to the content
// (5: long content in the view scrolls).
func ScrollWindow(lines []string, offset, height int) []string {
	if height < 1 || len(lines) == 0 {
		return nil
	}
	start := ScrollOffset(offset, len(lines), height)
	end := start + height
	if end > len(lines) {
		end = len(lines)
	}
	return lines[start:end]
}
