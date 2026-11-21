package view

import (
	"sort"

	"github.com/dpleshakov/penates/internal/lock"
	"github.com/dpleshakov/penates/internal/plan"
	"github.com/dpleshakov/penates/internal/target"
)

// The verbs and the path lines of the apply summary — one decision both
// interfaces render: the CLI prints the lines, the TUI lays them out with
// its glyphs and colors. The verbs are part of the CLI's stable output
// skeleton; the past forms are what a finished step reports.

// The verbs of the summary, the progress, and the result lines, the same
// words both interfaces speak.
const (
	VerbInstall   = "install"
	VerbUpdate    = "update"
	VerbDowngrade = "downgrade"
	VerbRemove    = "remove"
	VerbReset     = "reset"
)

// PastVerb is the verb a finished step reports in the result.
func PastVerb(verb string) string {
	switch verb {
	case VerbInstall:
		return "installed"
	case VerbUpdate:
		return "updated"
	case VerbDowngrade:
		return "downgraded"
	case VerbReset:
		return "reset"
	case VerbRemove:
		return "removed"
	}
	return verb
}

// Mark is what an apply does to one path of an item: the writes —
// create, replace, delete — and the two no-write states, keep and
// release.
type Mark int

const (
	// MarkCreate is a write the target has not seen: a file, block or
	// JSON value is created.
	MarkCreate Mark = iota
	// MarkReplace is a write over owned content: the content is replaced.
	MarkReplace
	// MarkDelete is a write taking owned content away: the content is
	// deleted.
	MarkDelete
	// MarkKeep is no write: owned content is kept and remains owned.
	MarkKeep
	// MarkRelease is no write: content is kept and released from the
	// lock.
	MarkRelease
)

// glyphs are the plain glyphs of the marks: the ones the CLI prints, and
// the ones the TUI renders in their colors.
var glyphs = [...]string{
	MarkCreate:  "+",
	MarkReplace: "~",
	MarkDelete:  "-",
	MarkKeep:    "=",
	MarkRelease: "!",
}

// Glyph is the plain glyph of the mark: +, ~, -, =, !.
func (m Mark) Glyph() string { return glyphs[m] }

// PathLine is one path line under a summary item: the mark of what the
// apply does to the path, the target-relative path, and the optional
// note behind it — the block a section writes, the pointer of an owned
// JSON location, or the reason content is kept.
type PathLine struct {
	Mark Mark
	Path string
	Note string
}

// PlanVerb is the verb of a planned install: a reset stays a reset, an
// installed item updates or downgrades, a fresh one installs.
func PlanVerb(ip *plan.ItemPlan, lk *lock.Lock, resets map[string]bool) string {
	if resets[ip.Key] {
		return VerbReset
	}
	if lk.Items[ip.Key] != nil {
		if ip.Downgrade {
			return VerbDowngrade
		}
		return VerbUpdate
	}
	return VerbInstall
}

// VersionText is the version text of a summary item line: the installed
// and the kit version joined with an arrow when they differ; a fresh
// install and a removal name the one version they reach for and take
// away.
func VersionText(ip *plan.ItemPlan, lk *lock.Lock, verb string) string {
	if verb == VerbInstall || verb == VerbRemove {
		return ip.Version
	}
	if rec := lk.Items[ip.Key]; rec != nil && rec.Version != ip.Version {
		return rec.Version + " → " + ip.Version
	}
	return ip.Version
}

// InstallLines builds the path lines of one install-plan item: the
// actions mark create where the target has not seen the content and
// replace where owned content is replaced; the content the lock records
// and no action rewrites marks keep — it stays as installed and remains
// owned. A NoAction item writes nothing: its verdict reason explains its
// first kept line.
func InstallLines(ip *plan.ItemPlan, lk *lock.Lock, snap *target.Snapshot, resets map[string]bool) (string, []PathLine) {
	verb := PlanVerb(ip, lk, resets)
	rec := lk.Items[ip.Key]
	var lines []PathLine
	writtenFiles := map[string]bool{}
	writtenShared := map[string]bool{}
	for _, a := range ip.Actions {
		switch a.Kind {
		case plan.ActionFile:
			writtenFiles[a.To] = true
			m := MarkReplace
			if !snap.File(a.To).Exists {
				m = MarkCreate
			}
			lines = append(lines, PathLine{Mark: m, Path: a.To})
		case plan.ActionSection:
			writtenShared[a.To] = true
			m := MarkReplace
			if _, present := snap.SharedFile(a.To).BlockHash(a.Block); !present {
				m = MarkCreate
			}
			lines = append(lines, PathLine{Mark: m, Path: a.To, Note: "section " + a.Block})
		case plan.ActionJSON:
			writtenShared[a.To] = true
			m := MarkCreate
			if rec != nil && len(rec.JSON[a.To]) > 0 {
				m = MarkReplace
			}
			lines = append(lines, PathLine{Mark: m, Path: a.To})
		}
	}
	lines = append(lines, keptLines(rec, writtenFiles, writtenShared)...)
	if ip.Status == plan.StatusNoAction && ip.Reason != "" && len(lines) > 0 {
		lines[0].Note = "kept: " + ip.Reason
	}
	// JSON locations the item takes ownership of without a write — values
	// found satisfied, or carried from the earlier install — mark keep
	// with their pointer: the lock records them, nothing is written.
	for _, file := range SortedKeys(ip.JSONLocations) {
		if writtenShared[file] || (rec != nil && len(rec.JSON[file]) > 0) {
			continue
		}
		for _, loc := range ip.JSONLocations[file] {
			lines = append(lines, PathLine{Mark: MarkKeep, Path: file, Note: loc.Pointer})
		}
	}
	return verb, lines
}

// keptLines marks the content the lock records that no action rewrites:
// it stays as installed and remains owned.
func keptLines(rec *lock.Item, writtenFiles, writtenShared map[string]bool) []PathLine {
	if rec == nil {
		return nil
	}
	var lines []PathLine
	for _, path := range SortedKeys(rec.Files) {
		if writtenFiles[path] {
			continue
		}
		lines = append(lines, PathLine{Mark: MarkKeep, Path: path})
	}
	for _, file := range SortedKeys(rec.Sections) {
		if writtenShared[file] {
			continue
		}
		lines = append(lines, PathLine{Mark: MarkKeep, Path: file, Note: "section " + rec.Sections[file].Block})
	}
	for _, file := range SortedKeys(rec.JSON) {
		if writtenShared[file] {
			continue
		}
		lines = append(lines, PathLine{Mark: MarkKeep, Path: file})
	}
	return lines
}

// RemovalLines builds the path lines of one removal plan: the owned
// content taken away marks delete, the content the user modified — kept
// and released from the lock — marks release.
func RemovalLines(ip *plan.ItemPlan) []PathLine {
	var lines []PathLine
	for _, a := range ip.Actions {
		switch a.Kind {
		case plan.ActionRemove:
			lines = append(lines, PathLine{Mark: MarkDelete, Path: a.To})
		case plan.ActionSectionRemove:
			lines = append(lines, PathLine{Mark: MarkDelete, Path: a.To, Note: "section " + a.Block})
		case plan.ActionJSONRemove:
			lines = append(lines, PathLine{Mark: MarkDelete, Path: a.To})
		}
	}
	seen := map[string]bool{}
	for _, k := range ip.Kept {
		id := k.Path + "\x00" + k.Reason
		if seen[id] {
			continue
		}
		seen[id] = true
		lines = append(lines, PathLine{Mark: MarkRelease, Path: k.Path, Note: "kept and released: " + k.Reason})
	}
	return lines
}

// SortedKeys lists a string-keyed map's keys, sorted.
func SortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
