package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/dpleshakov/penates/internal/kit"
	"github.com/dpleshakov/penates/internal/lock"
	"github.com/dpleshakov/penates/internal/plan"
	"github.com/dpleshakov/penates/internal/target"
	"github.com/dpleshakov/penates/internal/view"
)

// The summary and the result, the two layouts a mutating command prints
// (Output layouts, round 1): the summary before the confirmation — one
// verb group per item with a mark per path, the excluded with their
// reasons, the write count, and the changelog of every major update —
// and the result after the run — one past-verb line per item with the
// kept content and the failures, the counts, and the deferred report.
// The skeleton is stable: item names kit/item, the verbs, one mark or
// reason per line; the prose — reasons, notes, changelogs, paths — is
// not. Sections separate by blank lines; a note follows its line after
// two spaces; a reason indents by four.

// The verbs of the summary and the result, the same words the terminal
// UI's summary speaks.
const (
	verbInstall   = "install"
	verbUpdate    = "update"
	verbDowngrade = "downgrade"
	verbRemove    = "remove"
	verbReset     = "reset"
)

// The marks of the summary path lines: a write the target has not seen,
// owned content replaced, owned content taken away, content that stands
// as recorded, content a removal leaves behind as the user's own.
const (
	markCreate  = "+"
	markReplace = "~"
	markDelete  = "-"
	markKeep    = "="
	markRelease = "!"
)

// summaryLine is one path line under a summary item: the mark of what
// the apply does to the path, the target-relative path, and the optional
// note behind it — the block a section writes, the pointer of an owned
// JSON location, or the reason content is kept.
type summaryLine struct {
	mark string
	path string
	note string
}

// summaryItem is one verb group of the summary: the item line with its
// verb, key, version text and major mark, and the path lines under it.
type summaryItem struct {
	verb    string
	key     string
	version string
	major   bool
	lines   []summaryLine
}

// summaryExcluded is one excluded item of the summary: the reasons and
// the category its stderr line carries.
type summaryExcluded struct {
	key      string
	reasons  []string
	category Category
}

// summary is the content a mutating command prints before it asks.
type summary struct {
	// target is the path the target line names first.
	target string
	items  []summaryItem
	excl   []summaryExcluded
}

// writes counts the path lines that write the target: the created,
// replaced and deleted content.
func (s *summary) writes() int {
	n := 0
	for _, it := range s.items {
		for _, ln := range it.lines {
			switch ln.mark {
			case markCreate, markReplace, markDelete:
				n++
			}
		}
	}
	return n
}

// clean reports whether the plan holds no excluded item: a clean dry-run
// fully succeeds, a plan with exclusions does not.
func (s *summary) clean() bool { return len(s.excl) == 0 }

// buildSummary lays the planner's verdicts out into the summary, the way
// the terminal UI builds its summary screen from the same verdicts: the
// installs first — a fresh install, then the updates and the downgrades
// — then the removals, then the resets, and the excluded items last.
// lk and snap are the lock and the snapshot the plans were built
// against; resets are the items the run resets to kit.
func buildSummary(s *session, installs, removals []*plan.ItemPlan, resets map[string]bool) *summary {
	summ := &summary{target: s.target}
	var installItems, updateItems, removeItems, resetItems []summaryItem
	for _, ip := range installs {
		if ip.Status == plan.StatusError {
			summ.excl = append(summ.excl, excludedSummaryItem(ip))
			continue
		}
		verb, lines := installSummaryLines(ip, s.lock, s.snap, resets)
		it := summaryItem{
			verb:    verb,
			key:     ip.Key,
			version: summaryVersionText(ip, s.lock, verb),
			major:   summaryMajor(ip, s.lock),
			lines:   lines,
		}
		switch verb {
		case verbReset:
			resetItems = append(resetItems, it)
		case verbUpdate, verbDowngrade:
			updateItems = append(updateItems, it)
		default:
			installItems = append(installItems, it)
		}
	}
	for _, ip := range removals {
		if ip.Status == plan.StatusError {
			summ.excl = append(summ.excl, excludedSummaryItem(ip))
			continue
		}
		removeItems = append(removeItems, summaryItem{
			verb:    verbRemove,
			key:     ip.Key,
			version: ip.Version,
			lines:   removalSummaryLines(ip),
		})
	}
	summ.items = append(summ.items, installItems...)
	summ.items = append(summ.items, updateItems...)
	summ.items = append(summ.items, removeItems...)
	summ.items = append(summ.items, resetItems...)
	return summ
}

// planVerb is the verb of a planned install: a reset stays a reset, an
// installed item updates or downgrades, a fresh one installs.
func planVerb(ip *plan.ItemPlan, lk *lock.Lock, resets map[string]bool) string {
	if resets[ip.Key] {
		return verbReset
	}
	if lk.Items[ip.Key] != nil {
		if ip.Downgrade {
			return verbDowngrade
		}
		return verbUpdate
	}
	return verbInstall
}

// summaryVersionText is the version text of a summary item line: the
// installed and the kit version joined with an arrow when they differ.
func summaryVersionText(ip *plan.ItemPlan, lk *lock.Lock, verb string) string {
	if verb == verbInstall || verb == verbRemove {
		return ip.Version
	}
	if rec := lk.Items[ip.Key]; rec != nil && rec.Version != ip.Version {
		return rec.Version + " → " + ip.Version
	}
	return ip.Version
}

// summaryMajor reports whether an item's version change crosses the
// major version: the summary marks it and the changelog follows.
func summaryMajor(ip *plan.ItemPlan, lk *lock.Lock) bool {
	rec := lk.Items[ip.Key]
	if rec == nil {
		return false
	}
	return kit.MajorVersion(ip.Version) != kit.MajorVersion(rec.Version)
}

// installSummaryLines builds the path lines of one install-plan item:
// the actions mark `+` where the target has not seen the content and
// `~` where owned content is replaced; the content the lock records and
// no action rewrites marks `=` — it stays as installed and remains
// owned. A NoAction item writes nothing: its verdict reason explains its
// first kept line.
func installSummaryLines(ip *plan.ItemPlan, lk *lock.Lock, snap *target.Snapshot, resets map[string]bool) (string, []summaryLine) {
	verb := planVerb(ip, lk, resets)
	rec := lk.Items[ip.Key]
	var lines []summaryLine
	writtenFiles := map[string]bool{}
	writtenShared := map[string]bool{}
	for _, a := range ip.Actions {
		switch a.Kind {
		case plan.ActionFile:
			writtenFiles[a.To] = true
			m := markReplace
			if !snap.File(a.To).Exists {
				m = markCreate
			}
			lines = append(lines, summaryLine{mark: m, path: a.To})
		case plan.ActionSection:
			writtenShared[a.To] = true
			m := markReplace
			if _, present := snap.SharedFile(a.To).BlockHash(a.Block); !present {
				m = markCreate
			}
			lines = append(lines, summaryLine{mark: m, path: a.To, note: "section " + a.Block})
		case plan.ActionJSON:
			writtenShared[a.To] = true
			m := markCreate
			if rec != nil && len(rec.JSON[a.To]) > 0 {
				m = markReplace
			}
			lines = append(lines, summaryLine{mark: m, path: a.To})
		}
	}
	lines = append(lines, keptSummaryLines(rec, writtenFiles, writtenShared)...)
	if ip.Status == plan.StatusNoAction && ip.Reason != "" && len(lines) > 0 {
		lines[0].note = "kept: " + ip.Reason
	}
	// JSON locations the item takes ownership of without a write — values
	// found satisfied, or carried from the earlier install — mark `=` with
	// their pointer: the lock records them, nothing is written.
	for _, file := range sortedMapKeys(ip.JSONLocations) {
		if writtenShared[file] || (rec != nil && len(rec.JSON[file]) > 0) {
			continue
		}
		for _, loc := range ip.JSONLocations[file] {
			lines = append(lines, summaryLine{mark: markKeep, path: file, note: loc.Pointer})
		}
	}
	return verb, lines
}

// keptSummaryLines marks the content the lock records that no action
// rewrites: it stays as installed and remains owned.
func keptSummaryLines(rec *lock.Item, writtenFiles, writtenShared map[string]bool) []summaryLine {
	if rec == nil {
		return nil
	}
	var lines []summaryLine
	for _, path := range sortedMapKeys(rec.Files) {
		if writtenFiles[path] {
			continue
		}
		lines = append(lines, summaryLine{mark: markKeep, path: path})
	}
	for _, file := range sortedMapKeys(rec.Sections) {
		if writtenShared[file] {
			continue
		}
		lines = append(lines, summaryLine{mark: markKeep, path: file, note: "section " + rec.Sections[file].Block})
	}
	for _, file := range sortedMapKeys(rec.JSON) {
		if writtenShared[file] {
			continue
		}
		lines = append(lines, summaryLine{mark: markKeep, path: file})
	}
	return lines
}

// removalSummaryLines builds the path lines of one removal plan: the
// owned content taken away marks `-`, the content the user modified —
// kept and released from the lock — marks `!`.
func removalSummaryLines(ip *plan.ItemPlan) []summaryLine {
	var lines []summaryLine
	for _, a := range ip.Actions {
		switch a.Kind {
		case plan.ActionRemove:
			lines = append(lines, summaryLine{mark: markDelete, path: a.To})
		case plan.ActionSectionRemove:
			lines = append(lines, summaryLine{mark: markDelete, path: a.To, note: "section " + a.Block})
		case plan.ActionJSONRemove:
			lines = append(lines, summaryLine{mark: markDelete, path: a.To})
		}
	}
	seen := map[string]bool{}
	for _, k := range ip.Kept {
		id := k.Path + "\x00" + k.Reason
		if seen[id] {
			continue
		}
		seen[id] = true
		lines = append(lines, summaryLine{mark: markRelease, path: k.Path, note: "kept and released: " + k.Reason})
	}
	return lines
}

// exclusionCategoryOf maps an excluded item's verdict to the category its
// stderr line carries: a structured conflict is the conflict category —
// the ownership rules refused the write — a thing that does not exist is
// not_found, and the rest is kit content at fault. The exit code stays 1
// either way: the command line was right; the exclusion happened while
// the command ran.
func exclusionCategoryOf(ip *plan.ItemPlan) Category {
	if len(ip.Conflicts) > 0 {
		return CategoryConflict
	}
	for _, r := range ip.Reasons {
		switch {
		case strings.Contains(r, "is not installed"), strings.Contains(r, "no registered kit provides"):
			return CategoryNotFound
		}
	}
	return CategoryKit
}

// excludedSummaryItem is one excluded item's summary entry.
func excludedSummaryItem(ip *plan.ItemPlan) summaryExcluded {
	return summaryExcluded{key: ip.Key, reasons: view.ExclusionLines(ip), category: exclusionCategoryOf(ip)}
}

// changelogBlocks reads the CHANGELOG.md of every major update, under
// the `changelog <kit/item> <version>` heading the contract fixes; the
// content is kit data and unstable, the heading is not. Also printed on
// a dry-run, where the summary is the whole preview. An item without a
// changelog file brings no block.
func changelogBlocks(s *session, installs []*plan.ItemPlan) [][]string {
	var blocks [][]string
	for _, ip := range installs {
		if ip.Status == plan.StatusError || !summaryMajor(ip, s.lock) {
			continue
		}
		it := kitItemByKey(s, ip.Key)
		if it == nil || !it.HasChangelog {
			continue
		}
		data, err := os.ReadFile(filepath.Join(it.Dir, "CHANGELOG.md")) //nolint:gosec // the kit item built the path
		if err != nil {
			continue
		}
		text := strings.TrimRight(string(data), "\n")
		if text == "" {
			continue
		}
		block := append([]string{fmt.Sprintf("changelog %s %s", ip.Key, ip.Version)}, strings.Split(text, "\n")...)
		blocks = append(blocks, block)
	}
	return blocks
}

// printSummary writes the summary out: the target line first — the CLI
// neither reads nor writes which target the terminal UI opens, so every
// mutating run names the one it acts on — then the verb groups with the
// excluded, the write count, and the changelog of every major update.
func printSummary(w io.Writer, summ *summary) error {
	sections := [][]string{{"target " + summ.target}}
	var body []string
	for _, it := range summ.items {
		line := it.verb + "  " + it.key
		if it.version != "" {
			line += "  " + it.version
		}
		if it.major {
			line += "  major"
		}
		body = append(body, line)
		for _, ln := range it.lines {
			row := "  " + ln.mark + " " + ln.path
			if ln.note != "" {
				row += "  " + ln.note
			}
			body = append(body, row)
		}
	}
	for _, e := range summ.excl {
		body = append(body, "excluded  "+e.key)
		for _, r := range e.reasons {
			body = append(body, "    "+r)
		}
	}
	sections = append(sections, body)
	sections = append(sections, []string{fmt.Sprintf("%d write%s", summ.writes(), plural(summ.writes()))})
	return printSections(w, sections)
}

// plural suffixes the write count.
func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// resultRow is one item's line of the result: the past verb, the key,
// the version it reached, the note behind it — the reason a kept item
// keeps or a failure failed — the content a removal left behind, and the
// reasons and category of an excluded item.
type resultRow struct {
	verb      string // installed, updated, downgraded, reset, removed, kept, failed, excluded
	key       string
	version   string
	note      string
	keptNotes []string
	reasons   []string
	category  Category // the stderr line of a failed or excluded row
}

// runOutcome is what one executed run produced: the row per planned
// item, the counts, the deferred report of the apply, and the failure
// that reached the command after the run — a state the run could not
// save, a deferred completion that failed.
type runOutcome struct {
	rows     []resultRow
	applied  int
	kept     int
	failed   int // the failed and the excluded together
	deferred []string
	stateErr *Error
}

// pastVerb is the verb a finished step reports in the result.
func pastVerb(verb string) string {
	switch verb {
	case verbInstall:
		return "installed"
	case verbUpdate:
		return "updated"
	case verbDowngrade:
		return "downgraded"
	case verbReset:
		return "reset"
	case verbRemove:
		return "removed"
	}
	return verb
}

// resultVersion is the version text of a result row: the installed and
// the reached version joined with an arrow for an update, the reached
// version alone otherwise; a removal names none — what it took away is
// gone.
func resultVersion(ip *plan.ItemPlan, lk *lock.Lock, verb string) string {
	if verb == verbRemove {
		return ""
	}
	if rec := lk.Items[ip.Key]; rec != nil && rec.Version != ip.Version {
		return rec.Version + " → " + ip.Version
	}
	return ip.Version
}

// exclusionError is the one stderr line an excluded item earns: the key
// and the first reason; the stdout result carries the rest.
func exclusionError(row resultRow) *Error {
	msg := row.key
	if len(row.reasons) > 0 {
		msg += ": " + row.reasons[0]
	}
	return &Error{Category: row.category, Message: msg}
}

// printResult writes the result out: the row per item with the kept
// content and the reasons under their lines, the counts, and the
// deferred report. Every failed and excluded item also earns one Error
// line on stderr — the reason the exit code is 1, where an agent that
// reads only stderr finds it.
func printResult(w, errW io.Writer, oc *runOutcome) error {
	var body []string
	for _, row := range oc.rows {
		line := row.verb + "  " + row.key
		if row.version != "" {
			line += "  " + row.version
		}
		if row.note != "" {
			line += "  " + row.note
		}
		body = append(body, line)
		for _, note := range row.keptNotes {
			body = append(body, "  "+note)
		}
		for _, r := range row.reasons {
			body = append(body, "    "+r)
		}
	}
	sections := [][]string{body}
	var counts []string
	if oc.applied > 0 {
		counts = append(counts, fmt.Sprintf("%d applied", oc.applied))
	}
	if oc.kept > 0 {
		counts = append(counts, fmt.Sprintf("%d kept", oc.kept))
	}
	if oc.failed > 0 {
		counts = append(counts, fmt.Sprintf("%d failed", oc.failed))
	}
	if len(counts) > 0 {
		sections = append(sections, []string{strings.Join(counts, " · ")})
	}
	if len(oc.deferred) > 0 {
		sections = append(sections, oc.deferred)
	}
	if err := printSections(w, sections); err != nil {
		return err
	}
	for _, row := range oc.rows {
		switch row.verb {
		case "failed":
			Print(&Error{Category: row.category, Message: row.key + ": " + row.note}, errW)
		case "excluded":
			Print(exclusionError(row), errW)
		}
	}
	return nil
}

// printNotes writes the plain note sections — the staged temp files an
// interrupted apply had left, the staged actions the apply dropped — as
// `note` lines, the way status prints the registry's own notes.
func printNotes(w io.Writer, notes []string) error {
	if len(notes) == 0 {
		return nil
	}
	lines := make([]string, len(notes))
	for i, n := range notes {
		lines[i] = "note  " + n
	}
	return printSections(w, [][]string{lines})
}

// sortedMapKeys lists a string-keyed map's keys, sorted.
func sortedMapKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
