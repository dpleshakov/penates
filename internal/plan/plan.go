// Package plan is the pure decision core: kits, lock, snapshot, and source
// contents in; a plan out — self-contained actions with final content bytes,
// plain reasons and structured conflicts per item, and the JSON locations
// each item owns. It implements the behavior rules of the product concept
// (docs/concept.md) — among them the three-state conflict model and the
// per-location table for json contributions — and performs no I/O.
package plan

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/dpleshakov/penates/internal/jsonc"
	"github.com/dpleshakov/penates/internal/kit"
	"github.com/dpleshakov/penates/internal/lock"
	"github.com/dpleshakov/penates/internal/markdown"
	"github.com/dpleshakov/penates/internal/target"
)

// ItemStatus says what the planner decided for one item.
type ItemStatus string

const (
	// StatusInstall means every contribution checks out and the item will be applied.
	StatusInstall ItemStatus = "install"
	// StatusNoAction means the item needs nothing: it is installed at the same
	// version already.
	StatusNoAction ItemStatus = "no-action"
	// StatusRemove means the item is removed from the target: its actions
	// delete files and rewrite shared files without the item's content, and
	// apply drops the item's lock record. Only BuildRemoval produces
	// it.
	StatusRemove ItemStatus = "remove"
	// StatusError means the item is excluded from the plan; its reasons say why.
	StatusError ItemStatus = "error"
)

// ItemPlan is the planner's verdict on one selected item.
type ItemPlan struct {
	// Key is the item key: "kit name/item id".
	Key string
	// KitName and ItemID name the item the plan is about.
	KitName string
	ItemID  string
	// Version is the version that would be installed.
	Version string
	// Status is the verdict.
	Status ItemStatus
	// Reasons holds the plain exclusion reasons when Status is Error: a
	// missing variable, an unknown placeholder, a source the planner did
	// not receive. Conflicts are not reasons; they are Conflicts.
	Reasons []string
	// Reason explains a NoAction.
	Reason string
	// Actions are the planned writes, in declaration order, when Status is Install.
	Actions []Action
	// JSONLocations lists, per shared file, the JSON locations the item takes
	// ownership of when installed: the values it writes, and equal
	// values owned by another item, which make the item a co-owner. Locations
	// merely found satisfied are absent. An update also carries the locations
	// of the earlier install that the new fragment stops declaring: ownership
	// never ends but with the removal of the item. The lock records
	// them; a plan may install an item whose locations are all satisfied,
	// co-owned, or carried without writing any byte.
	JSONLocations map[string][]Location
	// Answers are the values the plan substituted into the item's templated
	// contributions: the client's fresh answers over the recorded ones, for
	// the declared variables only. Apply records them in the lock;
	// an item with no variables carries none.
	Answers map[string]string
	// Downgrade marks an item whose kit version is lower than the installed
	// one. The plan is complete, but the client shows it as a
	// downgrade and never selects it by default.
	Downgrade bool
	// Conflicts holds the structured conflicts of an excluded item: an
	// encounter with user content or with another selected item's claim,
	// whichever contribution kind wrote it. The client shows an item with
	// conflicts as conflicting, not merely excluded, and renders the
	// user-facing prose from the structure. Set only when Status is
	// Error.
	Conflicts []Conflict
	// Warning marks a NoAction item the version table warns about:
	// the kit provides different content under the installed version, so
	// applying would keep everything and update nothing. Set only when
	// Status is NoAction.
	Warning bool
	// Kept holds the content a removal plan leaves behind: a modified
	// file, a modified block in a shared file, a modified or unreadable
	// JSON value. The screens mark this content with `!` (tui-screens.md
	// 10.1) and the record on exit names it (10.4).
	Kept []KeptNote
}

// KeptNote is one piece of content a removal plan leaves behind instead of
// taking it away.
type KeptNote struct {
	// Path is the target-relative path of the kept file, or of the shared
	// file that holds the kept block or value.
	Path string
	// Pointer is the RFC 6901 pointer of the kept value, when the content
	// is one JSON location of the shared file.
	Pointer string
	// Reason says why the content stayed.
	Reason string
}

// Conflict is one collision the planner found, as data: the kind of the
// item's contribution that refused to write, the location it wanted, what
// stands there and whose it is. The client renders the user-facing prose
// from it.
type Conflict struct {
	// Kind is the contribution kind whose planning found the collision.
	Kind kit.ContributionKind
	// File is the target-relative path of the file or shared file the
	// collision is about.
	File string
	// Block is the managed block identifier, when the collision is about a
	// block in the shared file.
	Block string
	// Pointer is the RFC 6901 pointer of the JSON value, when the collision
	// is about one location of the shared file; the root location is "".
	Pointer string
	// Heading is the duplicated heading, when the collision is the coarse
	// duplicate-heading check of a section contribution.
	Heading string
	// Cause names the situation the collision is.
	Cause ConflictCause
	// Owner is the installed item that owns the content standing in the
	// location, when the collision is with owned content: another item for
	// CauseOwned, the item itself for CauseModified, the single installed
	// owner of the standing value for CauseDifferentValue.
	Owner string
	// Rivals are the other selected items claiming the same location, for
	// the claimed causes.
	Rivals []string
}

// ConflictCause tells the situations of a collision apart: what stands in
// the location, and whose it is.
type ConflictCause string

const (
	// CauseClaimed is a rivalry between selected items over one location:
	// other items claim it as well; for a json location, with a different
	// value. Rivals names them.
	CauseClaimed ConflictCause = "claimed"
	// CauseClaimedAsFile is a shared file other selected items contribute
	// whole files to: a block or a json value cannot share it with them.
	CauseClaimedAsFile ConflictCause = "claimed-as-file"
	// CauseClaimedAsJSON is a shared file other selected items contribute
	// json values to: a block cannot share it with them.
	CauseClaimedAsJSON ConflictCause = "claimed-as-json"
	// CauseClaimedAsFileOrSection is a shared file other selected items
	// contribute whole files or blocks to: a json value cannot share it
	// with them.
	CauseClaimedAsFileOrSection ConflictCause = "claimed-as-file-or-section"
	// CauseUserContent is content standing in the location that the lock
	// does not record: it may belong to the user, and penates refuses to
	// touch it.
	CauseUserContent ConflictCause = "user-content"
	// CauseOwned is content an installed item owns standing in the
	// location; Owner names the item.
	CauseOwned ConflictCause = "owned"
	// CauseModified is the item's own owned content the user modified
	// after installation, with the kit providing different content.
	CauseModified ConflictCause = "modified"
	// CauseDifferentValue is a json location holding a value the fragment
	// does not provide and this item does not own: nothing is written.
	CauseDifferentValue ConflictCause = "different-value"
	// CauseTwoSections is the item itself contributing two blocks to one
	// shared file: one item owns one block per shared file.
	CauseTwoSections ConflictCause = "two-sections"
)

// ActionKind distinguishes what an action writes: a whole file or a managed
// Markdown block inside a shared file.
type ActionKind string

const (
	// ActionFile writes its content as the whole target file.
	ActionFile ActionKind = "file"
	// ActionSection writes its content as the whole shared file, with the block
	// placed by the planner.
	ActionSection ActionKind = "section"
	// ActionJSON writes its content as the whole shared file, with the
	// fragment's values inserted by RFC 6901 pointer.
	ActionJSON ActionKind = "json"
	// ActionRemove deletes the file at To: an owned file removed unchanged, or
	// a shared file penates created that is left without content.
	ActionRemove ActionKind = "remove"
	// ActionSectionRemove writes its content as the whole shared file with the
	// item's block deleted, or with its markers stripped when the user
	// modified the block. The item's section record dies with its
	// lock record.
	ActionSectionRemove ActionKind = "section-remove"
	// ActionJSONRemove writes its content as the whole shared file with the
	// values the item last-owned removed. Values another item
	// co-owns or the user modified stay.
	ActionJSONRemove ActionKind = "json-remove"
)

// Location is one JSON location an item owns inside a shared JSON file:
// an object member by the RFC 6901 pointer of the member, an
// array element by the pointer of its array, because arrays are owned as
// sets. Hash is "sha256:<hex>" over the minimal JSON of the value, so
// comparisons of installation base, current target, and incoming kit content
// survive reformatting and comments.
type Location struct {
	Pointer string
	Hash    string
}

// Action is one planned write. Actions are self-contained: Content is the final
// content of To, so apply writes and never reads kit files.
type Action struct {
	// Kind says what the action writes.
	Kind ActionKind
	// To is the target-relative path, with forward slashes.
	To string
	// Content is the complete final content of To.
	Content []byte
	// Block is the managed block identifier a section action writes.
	Block string
	// BlockHash is "sha256:<hex>" of the content between the markers of a section
	// action; the lock records it as the installation base.
	BlockHash string
	// Src names the contribution source, relative to the item directory, for
	// diagnostics.
	Src string
	// Reason names the rule that produced the action.
	Reason string
}

// Plan is the outcome of planning a selection: one entry per selected item, in
// selection order, without duplicates.
type Plan struct {
	Items []*ItemPlan
}

// Installable returns the item plans that will be applied, in plan order.
func (p *Plan) Installable() []*ItemPlan {
	var out []*ItemPlan
	for _, ip := range p.Items {
		if ip.Status == StatusInstall {
			out = append(out, ip)
		}
	}
	return out
}

// Removals returns the item plans that remove their items, in plan order.
func (p *Plan) Removals() []*ItemPlan {
	var out []*ItemPlan
	for _, ip := range p.Items {
		if ip.Status == StatusRemove {
			out = append(out, ip)
		}
	}
	return out
}

// Input carries everything planning needs. None of it is read further here: the
// planner is pure and touches no file system.
type Input struct {
	// Kits are the loaded kits of the target being planned for.
	Kits []*kit.Kit
	// Selection lists item keys, most significant first; duplicates keep the first
	// occurrence.
	Selection []string
	// Lock is the current lock of the target.
	Lock *lock.Lock
	// Snapshot is the observed state of the target's files. The caller snapshots
	// every file path and shared file the planning may look at.
	Snapshot *target.Snapshot
	// Sources holds the content of every contribution source of the selected
	// items, keyed by kit.Item.SourcePath. The caller reads the files; the
	// planner hashes and composes them.
	Sources map[string][]byte
	// Answers holds the variable values the client collected in its forms,
	// keyed by item key and variable id. They win over the answers recorded in
	// the lock; a required variable neither knows about is a missing value.
	Answers map[string]map[string]string
	// Reset names the installed items the user asked to reset to the kit
	// content: their owned content is replaced with the incoming
	// content even where the user modified it. A reset is never part of a
	// regular update plan.
	Reset map[string]bool
	// Remove names the installed items the user asked to remove. Only
	// BuildRemoval reads it; the removal is planned from the lock and the
	// snapshot alone, so orphans and items of unavailable kits remove like
	// any other item.
	Remove map[string]bool
	// Reconciled carries the outcome of reconciling the lock with the target
	// by recorded hashes, as the client produced it before
	// planning. A broken item plans again at its installed version: its lost
	// content is installed. A modified one keeps its content, and the
	// no-action reason names the modification. Nil when the client did not
	// reconcile.
	Reconciled *Reconciliation
}

// MissingVars returns the required variables of the selected items that
// neither the lock nor fresh answers give a value, keyed by item key in
// declaration order. Installed items take part: an update may introduce a
// variable the installation never answered. The client requests
// them before planning and passes the answers back in Input.Answers.
func MissingVars(kits []*kit.Kit, selection []string, lk *lock.Lock, fresh map[string]map[string]string) map[string][]kit.Var {
	index := map[string]*kit.Item{}
	for _, k := range kits {
		for _, it := range k.Items {
			index[kit.ItemKey(k.Name, it.ID)] = it
		}
	}
	out := map[string][]kit.Var{}
	seen := map[string]bool{}
	for _, key := range selection {
		if seen[key] {
			continue
		}
		seen[key] = true
		it, ok := index[key]
		if !ok {
			continue
		}
		for _, v := range it.Vars {
			if !v.Required {
				continue
			}
			if a, answered := answerOf(key, v.ID, lk, fresh); !answered || a == "" {
				out[key] = append(out[key], v)
			}
		}
	}
	return out
}

// answerOf returns the value of one variable: a fresh answer the client
// collected wins over the answer recorded in the lock. answered is
// false when neither holds one.
func answerOf(key, varID string, lk *lock.Lock, fresh map[string]map[string]string) (value string, answered bool) {
	if a, ok := fresh[key][varID]; ok {
		return a, true
	}
	if rec, installed := lk.Items[key]; installed {
		a, ok := rec.Answers[varID]
		return a, ok
	}
	return "", false
}

// resolveVars merges fresh answers with the recorded ones for the item's
// declared variables: an answer of a variable the item no longer declares is
// discarded. It reports the required variables left without a
// value, sorted.
func resolveVars(key string, it *kit.Item, lk *lock.Lock, fresh map[string]map[string]string) (map[string]string, []string) {
	if len(it.Vars) == 0 {
		return nil, nil
	}
	answers := make(map[string]string, len(it.Vars))
	var missing []string
	for _, v := range it.Vars {
		a, _ := answerOf(key, v.ID, lk, fresh)
		answers[v.ID] = a
		if v.Required && a == "" {
			missing = append(missing, v.ID)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
	}
	return answers, missing
}

// placeholder matches one {{var_id}} substitution site.
var placeholder = regexp.MustCompile(`\{\{[^{}]*\}\}`)

// substitute replaces every {{var_id}} with the answer of the declared
// variable and returns the first placeholder no declared variable covers.
// The content of an answer is never rescanned: a value that
// itself contains braces lands verbatim.
func substitute(src []byte, answers map[string]string) (out []byte, unknown string) {
	out = placeholder.ReplaceAllFunc(src, func(m []byte) []byte {
		name := string(m[2 : len(m)-2])
		a, ok := answers[name]
		if !ok {
			if unknown == "" {
				unknown = name
			}
			return m
		}
		return []byte(a)
	})
	if unknown != "" {
		return src, unknown
	}
	return out, ""
}

// Build plans the installation of the selected items. Items that fail validation are
// excluded with their reasons; the others proceed.
func Build(in Input) *Plan {
	index := map[string]*kit.Item{}
	for _, k := range in.Kits {
		for _, it := range k.Items {
			index[kit.ItemKey(k.Name, it.ID)] = it
		}
	}

	claims := collectClaims(in, index)
	shared := &sharedFiles{
		pending:    map[string]string{},
		jsonOwners: map[string]map[string]map[string]bool{},
	}

	p := &Plan{}
	seen := map[string]bool{}
	for _, key := range in.Selection {
		if seen[key] {
			continue
		}
		seen[key] = true
		p.Items = append(p.Items, planItem(key, index, in, claims, shared))
	}
	return p
}

// BuildRemoval plans the removal of the items named in Input.Remove.
// It reads the lock and the snapshot only: an item is removed by what the
// lock records, so orphans and items of unavailable kits remove exactly like
// the rest. Content the user modified stays in the target and reaches
// the client as a report; unchanged owned content goes away, and a shared
// file penates created that is left without content is deleted. The items
// compose their shared files across the run: every removal action rewrites
// a whole shared file, so a later item decides and removes its content in
// what the earlier items' removals leave, as the install planner builds its
// items on theirs.
func BuildRemoval(in Input) *Plan {
	keys := make([]string, 0, len(in.Remove))
	for key, remove := range in.Remove {
		if remove {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)

	p := &Plan{}
	// Shared-file contents as the items planned so far leave them: each
	// removal action rewrites the whole file, and a later item composing
	// from the pre-run snapshot would write an earlier item's removed
	// content back.
	pending := map[string]string{}
	for _, key := range keys {
		p.Items = append(p.Items, planRemoval(key, in, pending))
	}
	return p
}

// planRemoval plans one item's removal from its lock record. Files, sections,
// and json values that stand as installed are taken away; the ones the user
// modified stay and are reported. A created shared file the removal leaves
// without content is deleted with it. pending carries the shared-file
// contents the items planned earlier leave behind; the compositions of this
// item are recorded there for the ones planned after it.
func planRemoval(key string, in Input, pending map[string]string) *ItemPlan {
	ip := &ItemPlan{Key: key}
	kitName, itemID, split := strings.Cut(key, "/")
	if split {
		ip.KitName, ip.ItemID = kitName, itemID
	}
	rec, installed := in.Lock.Items[key]
	if !installed {
		ip.Status = StatusError
		ip.Reasons = []string{"the item is not installed"}
		return ip
	}
	ip.Version = rec.Version
	ip.Status = StatusRemove

	for _, path := range sortedKeys(rec.Files) {
		st := in.Snapshot.File(path)
		if !st.Exists {
			continue // the file is gone already; the record dies with the item
		}
		if st.Hash == rec.Files[path] {
			ip.Actions = append(ip.Actions, Action{
				Kind:   ActionRemove,
				To:     path,
				Reason: "the owned file is removed unchanged",
			})
			continue
		}
		ip.Kept = append(ip.Kept, KeptNote{Path: path, Reason: "modified by the user"})
	}

	for _, file := range sortedKeys(rec.Sections) {
		sec := rec.Sections[file]
		composed, action := removalSection(ip, file, sec, in, pending)
		if action != nil {
			ip.Actions = append(ip.Actions, *action)
		}
		if composed != nil {
			pending[file] = string(composed)
		}
	}

	for _, file := range sortedKeys(rec.JSON) {
		removalJSON(ip, key, file, rec.JSON[file], in, pending)
	}

	return ip
}

// removalSection composes the shared file without the item's block: a block
// that stands as installed loses its markers with the content, a modified one
// keeps its content as ordinary text and is reported. The file is decided as
// the items planned earlier leave it, and a created shared file left without
// content is deleted instead of rewritten. composed is the new file content
// when an action rewrites it.
func removalSection(ip *ItemPlan, file string, sec lock.Section, in Input, pending map[string]string) (composed []byte, action *Action) {
	st := in.Snapshot.SharedFile(file)
	if !st.Exists || st.IsDir {
		return nil, nil
	}
	base, ok := pending[file]
	if !ok {
		base = st.Content
	}
	present := false
	installed := false
	for _, b := range markdown.Parse(base).Blocks {
		if b.ID == sec.Block {
			present = true
			installed = hashOf([]byte(b.Content)) == sec.Hash
			break
		}
	}
	if !present {
		return nil, nil // the user deleted the block already
	}

	var text string
	if installed {
		text, _ = markdown.Remove(base, sec.Block)
	} else {
		text, _ = markdown.Strip(base, sec.Block)
		ip.Kept = append(ip.Kept, KeptNote{Path: file, Reason: "modified by the user"})
	}
	if in.Lock.CreatedShared(file) && strings.TrimSpace(text) == "" {
		return nil, &Action{
			Kind:   ActionRemove,
			To:     file,
			Reason: "the shared file penates created is left without content",
		}
	}
	return []byte(text), &Action{
		Kind:    ActionSectionRemove,
		To:      file,
		Content: []byte(text),
		Block:   sec.Block,
		Reason:  "the item's block is removed from the shared file",
	}
}

// removalJSON composes the shared file without the values the item
// last-owns: a value another item co-owns stays, a modified one stays and is
// reported. The document is read from the file as the removal actions
// planned earlier leave it, and the item's composition is recorded for the
// items planned after it.
func removalJSON(ip *ItemPlan, key, file string, locs []lock.Location, in Input, pending map[string]string) {
	st := in.Snapshot.SharedFile(file)
	if !st.Exists || st.IsDir {
		return
	}
	base, ok := pending[file]
	if !ok {
		base = st.Content
	}
	doc, err := jsonc.Parse([]byte(base))
	if err != nil {
		ip.Kept = append(ip.Kept, KeptNote{Path: file, Reason: "it no longer holds a JSON document"})
		return
	}

	sorted := append([]lock.Location(nil), locs...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Pointer < sorted[j].Pointer })
	changed := false
	for _, loc := range sorted {
		min, found, err := doc.Get(loc.Pointer)
		if err != nil || !found {
			continue // the value is gone already; the record dies with the item
		}
		// The lock records array elements under the pointer of their array:
		// such a value stands while the array holds an element
		// with the recorded hash. A member's value stands at the pointer
		// itself and is compared by its hash.
		present := hashOf(min) == loc.Hash
		if !present && min[0] == '[' {
			elems, _, err := doc.Elements(loc.Pointer)
			if err == nil {
				for _, e := range elems {
					if hashOf(e) == loc.Hash {
						present = true
						break
					}
				}
			}
		}
		if !present {
			// The location no longer holds the recorded value: whatever the
			// user put there — another element, another value, a whole array
			// in place of a scalar — it stays, and the removal says so.
			ip.Kept = append(ip.Kept, KeptNote{Path: file, Pointer: loc.Pointer, Reason: "modified by the user"})
			continue
		}
		if owners := in.Lock.JSONOwners(file, loc.Pointer, loc.Hash); len(owners) > 1 {
			continue // another item still owns the value; the record dies alone
		}
		if err := removeJSONValue(doc, loc); err != nil {
			ip.Kept = append(ip.Kept, KeptNote{Path: file, Pointer: loc.Pointer, Reason: err.Error()})
			continue
		}
		changed = true
	}
	if !changed {
		return
	}
	composed := doc.Bytes()
	if in.Lock.CreatedShared(file) && jsonWithoutContent(composed) {
		ip.Actions = append(ip.Actions, Action{
			Kind:   ActionRemove,
			To:     file,
			Reason: "the shared file penates created is left without content",
		})
		return
	}
	pending[file] = string(composed)
	ip.Actions = append(ip.Actions, Action{
		Kind:    ActionJSONRemove,
		To:      file,
		Content: composed,
		Reason:  "the item's values are removed from the shared file",
	})
}

// removeJSONValue deletes one owned location: a member at its own pointer, an
// array element at the index its value holds in the array now, since the lock
// records elements as (array pointer, value hash) pairs.
func removeJSONValue(doc *jsonc.Doc, loc lock.Location) error {
	if min, _, err := doc.Get(loc.Pointer); err == nil && hashOf(min) == loc.Hash {
		return doc.Remove(loc.Pointer)
	}
	elems, found, err := doc.Elements(loc.Pointer)
	if err != nil || !found {
		return fmt.Errorf("the value's array is not in the file")
	}
	for i, e := range elems {
		if hashOf(e) == loc.Hash {
			return doc.Remove(loc.Pointer + "/" + strconv.Itoa(i))
		}
	}
	return fmt.Errorf("the value is not in the file's array")
}

// jsonWithoutContent reports whether the composed document holds no values at
// all: an empty object or array, or nothing.
func jsonWithoutContent(data []byte) bool {
	doc, err := jsonc.Parse(data)
	if err != nil {
		return false
	}
	min, found, err := doc.Get("")
	if err != nil || !found {
		return true
	}
	switch string(min) {
	case "{}", "[]":
		return true
	}
	return false
}

// sortedKeys lists a string-keyed map's keys, sorted.
func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// claimSet records which selected items write which target paths, so that
// cross-item conflicts reach every claimant.
type claimSet struct {
	// files maps a target path to the items contributing a whole file to it.
	files map[string]map[string]bool
	// shared maps a target path to the items writing into it as a shared file
	// (section or json contributions).
	shared map[string]map[string]bool
	// sections and jsons split the shared-file claims by kind: one shared file
	// is either Markdown or JSON, and a path claimed by both kinds is refused
	// on both sides.
	sections map[string]map[string]bool
	jsons    map[string]map[string]bool
	// jsonMembers records, per shared file and member pointer, the value hash
	// each selected item claims there, to detect the conflicts between them.
	jsonMembers map[string]map[string]map[string]string
}

// collectClaims indexes every contribution of every selected item by target path.
func collectClaims(in Input, index map[string]*kit.Item) *claimSet {
	c := &claimSet{
		files:       map[string]map[string]bool{},
		shared:      map[string]map[string]bool{},
		sections:    map[string]map[string]bool{},
		jsons:       map[string]map[string]bool{},
		jsonMembers: map[string]map[string]map[string]string{},
	}
	add := func(m map[string]map[string]bool, path, key string) {
		if m[path] == nil {
			m[path] = map[string]bool{}
		}
		m[path][key] = true
	}
	for _, key := range in.Selection {
		it, ok := index[key]
		if !ok {
			continue
		}
		for _, contr := range it.Contributes {
			switch contr.Kind {
			case kit.KindFile:
				add(c.files, contr.To, key)
			case kit.KindSection:
				add(c.shared, contr.To, key)
				add(c.sections, contr.To, key)
			case kit.KindJSON:
				add(c.shared, contr.To, key)
				add(c.jsons, contr.To, key)
				c.collectJSONClaims(in, key, it, contr)
			}
		}
	}
	return c
}

// collectJSONClaims records the member locations one json fragment claims, so
// that two items claiming the same location with different values are caught
// wherever they plan. An unparseable fragment claims nothing; its own item
// reports the parse error.
func (c *claimSet) collectJSONClaims(in Input, key string, it *kit.Item, contr kit.Contribution) {
	src, ok := in.Sources[it.SourcePath(contr)]
	if !ok {
		return
	}
	frag, err := jsonc.Parse(src)
	if err != nil {
		return
	}
	_ = walkJSON(frag, func(pointer string, element bool, hash string) error {
		if element {
			return nil
		}
		if c.jsonMembers[contr.To] == nil {
			c.jsonMembers[contr.To] = map[string]map[string]string{}
		}
		if c.jsonMembers[contr.To][pointer] == nil {
			c.jsonMembers[contr.To][pointer] = map[string]string{}
		}
		c.jsonMembers[contr.To][pointer][key] = hash
		return nil
	})
}

// fileRivals lists the items, other than key, that claim a path as a whole file or
// as a shared file; a file contribution excludes both.
func (c *claimSet) fileRivals(path, key string) []string {
	var others []string
	for other := range c.files[path] {
		if other != key {
			others = append(others, other)
		}
	}
	for other := range c.shared[path] {
		if other != key {
			others = append(others, other)
		}
	}
	sort.Strings(others)
	return others
}

// sharedRivals lists the items, other than key, that claim a shared path as a
// whole file; several shared writers coexist in one file.
func (c *claimSet) sharedRivals(path, key string) []string {
	var others []string
	for other := range c.files[path] {
		if other != key {
			others = append(others, other)
		}
	}
	sort.Strings(others)
	return others
}

// jsonRivals lists the items, other than key, that claim a path as a whole
// file or as a section: json values need a JSON document, and a file or a
// Markdown block cannot share it.
func (c *claimSet) jsonRivals(path, key string) []string {
	set := map[string]bool{}
	for other := range c.files[path] {
		if other != key {
			set[other] = true
		}
	}
	for other := range c.sections[path] {
		if other != key {
			set[other] = true
		}
	}
	return sortedKeys(set)
}

// sectionRivals lists the items, other than key, that claim a path as a whole
// file or as json; a Markdown block needs a Markdown document.
func (c *claimSet) sectionRivals(path, key string) []string {
	set := map[string]bool{}
	for other := range c.files[path] {
		if other != key {
			set[other] = true
		}
	}
	for other := range c.jsons[path] {
		if other != key {
			set[other] = true
		}
	}
	return sortedKeys(set)
}

// jsonConflict reports the first member location, in pointer order, that
// another selected item claims with a value different from the hash this
// fragment claims there. Arrays are sets: their elements are not
// member locations and never conflict.
func (c *claimSet) jsonConflict(file string, key string, leaves map[string]string) (string, []string) {
	ptrs := make([]string, 0, len(leaves))
	for ptr := range leaves {
		ptrs = append(ptrs, ptr)
	}
	sort.Strings(ptrs)
	for _, ptr := range ptrs {
		var rivals []string
		for other, hash := range c.jsonMembers[file][ptr] {
			if other != key && hash != leaves[ptr] {
				rivals = append(rivals, other)
			}
		}
		if len(rivals) > 0 {
			sort.Strings(rivals)
			return ptr, rivals
		}
	}
	return "", nil
}

// sharedFiles carries the shared-file contents as the planner composes them:
// items are planned in order, and each action rewrites the whole file, so a
// later item must build on what the earlier ones leave behind.
type sharedFiles struct {
	pending map[string]string
	// jsonOwners records the JSON locations the items planned so far took
	// ownership of, including co-ownership: file → "pointer\x00hash" → item.
	// The document a later item evaluates against already holds the values the
	// earlier ones wrote; this map says who owns them.
	jsonOwners map[string]map[string]map[string]bool
}

// base returns the content of a shared file before the next item's actions:
// the result of earlier planned actions, or the observed target content.
func (s *sharedFiles) base(snap *target.Snapshot, path string) string {
	if c, ok := s.pending[path]; ok {
		return c
	}
	return snap.SharedFile(path).Content
}

// record notes the content a planned action leaves in a shared file.
func (s *sharedFiles) record(path, content string) {
	s.pending[path] = content
}

// ownJSON notes that key took ownership of one JSON location while planning.
func (s *sharedFiles) ownJSON(file, pointer, hash, key string) {
	if s.jsonOwners[file] == nil {
		s.jsonOwners[file] = map[string]map[string]bool{}
	}
	loc := pointer + "\x00" + hash
	if s.jsonOwners[file][loc] == nil {
		s.jsonOwners[file][loc] = map[string]bool{}
	}
	s.jsonOwners[file][loc][key] = true
}

// jsonOwnerItems lists the items planned so far that own the location.
func (s *sharedFiles) jsonOwnerItems(file, pointer, hash string) map[string]bool {
	return s.jsonOwners[file][pointer+"\x00"+hash]
}

func planItem(key string, index map[string]*kit.Item, in Input, claims *claimSet, shared *sharedFiles) *ItemPlan {
	ip := &ItemPlan{Key: key}
	kitName, itemID, split := strings.Cut(key, "/")
	if split {
		ip.KitName, ip.ItemID = kitName, itemID
	}

	it, ok := index[key]
	if !ok {
		ip.Status = StatusError
		ip.Reasons = []string{"no registered kit provides this item"}
		return ip
	}
	ip.Version = it.Version
	lk := in.Lock

	var reasons []string

	// A required variable without a value stops the item here: the content of
	// its templated contributions is undefined without one.
	answers, missing := resolveVars(key, it, lk, in.Answers)
	if len(missing) > 0 {
		for _, id := range missing {
			reasons = append(reasons, fmt.Sprintf("required variable %q has no value", id))
		}
		ip.Status = StatusError
		ip.Reasons = reasons
		return ip
	}
	if answers != nil {
		ip.Answers = answers
	}

	resolved, failed := substituteSources(it, answers, in, &reasons)

	// An installed item is re-decided by the version table: a higher
	// kit version updates, a lower one plans a downgrade the client never
	// selects by default, an equal version needs nothing or warns, and an
	// explicit reset bypasses the table entirely. A broken item bypasses it
	// too: reconciliation found its recorded content missing from the target,
	// and the next apply installs it again.
	reset := false
	if rec, installed := lk.Items[key]; installed {
		reset = in.Reset[key]
		if !reset && !in.Reconciled.BrokenItem(key) {
			switch kit.CompareVersions(it.Version, rec.Version) {
			case 0:
				if contentUnchanged(it, rec, in, resolved) {
					ip.Status = StatusNoAction
					ip.Reason = "already installed"
					if in.Reconciled.ModifiedItem(key) {
						ip.Reason = "already installed; the content was modified after installation"
					}
				} else {
					ip.Status = StatusNoAction
					ip.Reason = "content changed without a version change; no update is offered"
					ip.Warning = true
				}
				return ip
			case -1:
				ip.Downgrade = true
			}
		}
	}

	return planContributions(key, it, in, claims, shared, ip, resolved, failed, reset, reasons)
}

// substituteSources resolves the substituted content of the item's templated
// contributions, keyed by source path. A source that holds an unknown
// placeholder is not in the result: its reason is recorded and the
// contribution is skipped.
func substituteSources(it *kit.Item, answers map[string]string, in Input, reasons *[]string) (resolved map[string][]byte, failed map[string]bool) {
	resolved = map[string][]byte{}
	failed = map[string]bool{}
	for _, c := range it.Contributes {
		if !c.Template {
			continue
		}
		src, ok := in.Sources[it.SourcePath(c)]
		if !ok {
			continue // the missing-source reason fires where the content is read
		}
		out, unknown := substitute(src, answers)
		if unknown != "" {
			*reasons = append(*reasons, fmt.Sprintf("%s holds the unknown placeholder {{%s}}", c.Src, unknown))
			failed[it.SourcePath(c)] = true
			continue
		}
		resolved[it.SourcePath(c)] = out
	}
	return resolved, failed
}

// contentUnchanged reports whether every contribution of the installed item
// would install exactly the content the lock recorded at installation:
// the kit changed nothing under an unchanged version.
func contentUnchanged(it *kit.Item, rec *lock.Item, in Input, resolved map[string][]byte) bool {
	for _, c := range it.Contributes {
		src, ok := in.Sources[it.SourcePath(c)]
		if !ok {
			return false
		}
		if sub, ok := resolved[it.SourcePath(c)]; ok {
			src = sub
		}
		switch c.Kind {
		case kit.KindFile:
			if rec.Files[c.To] != hashOf(src) {
				return false
			}
		case kit.KindSection:
			if rec.Sections[c.To].Hash != hashOf(src) {
				return false
			}
		case kit.KindJSON:
			if !jsonUnchanged(c, rec, in, src) {
				return false
			}
		}
	}
	return true
}

// jsonUnchanged reports whether the fragment would change nothing in the
// shared file: every location recorded in the lock comes back with its
// recorded value, every location the fragment drops counts as a change, and
// every unrecorded one is already satisfied by the target, so planning would
// write nothing.
func jsonUnchanged(c kit.Contribution, rec *lock.Item, in Input, src []byte) bool {
	frag, err := jsonc.Parse(src)
	if err != nil {
		return false
	}
	members := map[string]map[string]bool{}
	elements := map[string]map[string]bool{}
	collect := func(ptr string, element bool, hash string) error {
		set := members
		if element {
			set = elements
		}
		if set[ptr] == nil {
			set[ptr] = map[string]bool{}
		}
		set[ptr][hash] = true
		return nil
	}
	if walkJSON(frag, collect) != nil {
		return false
	}

	// Every recorded location must come back with its recorded value.
	recorded := map[string]map[string]bool{}
	for _, loc := range rec.JSON[c.To] {
		if recorded[loc.Pointer] == nil {
			recorded[loc.Pointer] = map[string]bool{}
		}
		recorded[loc.Pointer][loc.Hash] = true
		if !members[loc.Pointer][loc.Hash] && !elements[loc.Pointer][loc.Hash] {
			return false // dropped from the fragment, or changed
		}
	}

	// Unrecorded locations: satisfied only when the target holds the same
	// value. A file the fragment would create is a change by definition.
	state := in.Snapshot.SharedFile(c.To)
	if !state.Exists || strings.TrimSpace(state.Content) == "" {
		return false
	}
	doc, err := jsonc.Parse([]byte(state.Content))
	if err != nil {
		return false
	}
	for ptr, hashes := range members {
		for hash := range hashes {
			if recorded[ptr][hash] {
				continue
			}
			tmin, found, err := doc.Get(ptr)
			if err != nil || !found || hashOf(tmin) != hash {
				return false
			}
		}
	}
	for ptr, hashes := range elements {
		for hash := range hashes {
			if recorded[ptr][hash] {
				continue
			}
			elems, found, err := doc.Elements(ptr)
			if err != nil || !found {
				return false
			}
			present := false
			for _, e := range elems {
				if hashOf(e) == hash {
					present = true
					break
				}
			}
			if !present {
				return false
			}
		}
	}
	return true
}

// exclusion is one cause of an item's exclusion from the plan: a structured
// conflict the client renders for the user, or a plain reason the client
// prints as it is. Exactly one of the two is set.
type exclusion struct {
	conflict *Conflict
	reason   string
}

// conflictExclusion marks an exclusion as a conflict.
func conflictExclusion(c Conflict) exclusion { return exclusion{conflict: &c} }

// reasonf builds a plain exclusion reason.
func reasonf(format string, args ...any) exclusion {
	return exclusion{reason: fmt.Sprintf(format, args...)}
}

// reasonOf turns an error into a plain exclusion reason.
func reasonOf(err error) exclusion { return exclusion{reason: err.Error()} }

// empty reports whether the exclusion holds nothing: the contribution
// passed.
func (e exclusion) empty() bool { return e.conflict == nil && e.reason == "" }

// planContributions evaluates every contribution of the item into ip: the
// planned writes, the owned JSON locations, and the exclusions. reset
// marks an explicit reset to kit, which overwrites the item's owned
// content even where the user modified it. reasons are the plain reasons
// gathered before the contributions run; conflicts gather here.
func planContributions(key string, it *kit.Item, in Input, claims *claimSet, shared *sharedFiles, ip *ItemPlan, resolved map[string][]byte, failed map[string]bool, reset bool, reasons []string) *ItemPlan {
	lk := in.Lock

	sectionFiles := map[string]bool{}
	jsonLocs := newJSONRecord()
	var confs []Conflict
	for _, c := range it.Contributes {
		switch c.Kind {
		case kit.KindFile:
			if others := claims.fileRivals(c.To, key); len(others) > 0 {
				confs = append(confs, Conflict{
					Kind:   kit.KindFile,
					File:   c.To,
					Cause:  CauseClaimed,
					Rivals: others,
				})
				continue
			}
			content, ok := in.Sources[it.SourcePath(c)]
			if !ok {
				reasons = append(reasons, fmt.Sprintf("source %s was not provided to the planner", c.Src))
				continue
			}
			if c.Template {
				if failed[it.SourcePath(c)] {
					continue // the unknown-placeholder reason is already recorded
				}
				content = resolved[it.SourcePath(c)]
			}
			state := in.Snapshot.File(c.To)
			if !state.Exists {
				ip.Actions = append(ip.Actions, Action{
					Kind:    ActionFile,
					To:      c.To,
					Content: content,
					Src:     c.Src,
					Reason:  "target file is absent",
				})
				continue
			}
			if state.Hash == "" {
				// A directory occupies the path: the snapshot gives it no hash.
				reasons = append(reasons, fmt.Sprintf("%s is a directory", c.To))
				continue
			}
			owner, base, owned := lk.FileOwner(c.To)
			if !owned {
				confs = append(confs, Conflict{
					Kind:  kit.KindFile,
					File:  c.To,
					Cause: CauseUserContent,
				})
				continue
			}
			if owner != key {
				confs = append(confs, Conflict{
					Kind:  kit.KindFile,
					File:  c.To,
					Owner: owner,
					Cause: CauseOwned,
				})
				continue
			}
			// Owned by this item: the three-state rules decide,
			// and a reset overwrites whatever the user changed.
			if reset {
				ip.Actions = append(ip.Actions, Action{
					Kind:    ActionFile,
					To:      c.To,
					Content: content,
					Src:     c.Src,
					Reason:  "reset to kit replaces the owned file",
				})
				continue
			}
			if write, conflict := ownedAction(base, state.Hash, hashOf(content)); conflict != "" {
				confs = append(confs, Conflict{
					Kind:  kit.KindFile,
					File:  c.To,
					Owner: key,
					Cause: CauseModified,
				})
			} else if write {
				ip.Actions = append(ip.Actions, Action{
					Kind:    ActionFile,
					To:      c.To,
					Content: content,
					Src:     c.Src,
					Reason:  "update replaces the owned file",
				})
			}
		case kit.KindSection:
			if others := claims.sharedRivals(c.To, key); len(others) > 0 {
				confs = append(confs, Conflict{
					Kind:   kit.KindSection,
					File:   c.To,
					Cause:  CauseClaimedAsFile,
					Rivals: others,
				})
				continue
			}
			if others := claims.sectionRivals(c.To, key); len(others) > 0 {
				confs = append(confs, Conflict{
					Kind:   kit.KindSection,
					File:   c.To,
					Cause:  CauseClaimedAsJSON,
					Rivals: others,
				})
				continue
			}
			if owner, _, owned := lk.FileOwner(c.To); owned {
				confs = append(confs, Conflict{
					Kind:  kit.KindSection,
					File:  c.To,
					Owner: owner,
					Cause: CauseOwned,
				})
				continue
			}
			if sectionFiles[c.To] {
				confs = append(confs, Conflict{
					Kind:  kit.KindSection,
					File:  c.To,
					Cause: CauseTwoSections,
				})
				continue
			}
			sectionFiles[c.To] = true
			if c.Template && failed[it.SourcePath(c)] {
				continue // the unknown-placeholder reason is already recorded
			}
			a, exc := planSection(key, c, it, in, resolved, shared, reset)
			if exc.conflict != nil {
				confs = append(confs, *exc.conflict)
				continue
			}
			if exc.reason != "" {
				reasons = append(reasons, exc.reason)
				continue
			}
			if a != nil {
				ip.Actions = append(ip.Actions, *a)
			}
		case kit.KindJSON:
			if others := claims.jsonRivals(c.To, key); len(others) > 0 {
				confs = append(confs, Conflict{
					Kind:   kit.KindJSON,
					File:   c.To,
					Cause:  CauseClaimedAsFileOrSection,
					Rivals: others,
				})
				continue
			}
			if owner, _, owned := lk.FileOwner(c.To); owned {
				confs = append(confs, Conflict{
					Kind:  kit.KindJSON,
					File:  c.To,
					Owner: owner,
					Cause: CauseOwned,
				})
				continue
			}
			if c.Template && failed[it.SourcePath(c)] {
				continue // the unknown-placeholder reason is already recorded
			}
			a, exc := planJSON(key, c, it, in, resolved, claims, shared, jsonLocs, reset)
			if exc.conflict != nil {
				confs = append(confs, *exc.conflict)
				continue
			}
			if exc.reason != "" {
				reasons = append(reasons, exc.reason)
				continue
			}
			if a != nil {
				ip.Actions = append(ip.Actions, *a)
			}
		}
	}
	// A json file the item no longer contributes to keeps its recorded
	// locations: no fragment re-declares them, and apply replaces the whole
	// lock record, so the plan carries them.
	if old := lk.Items[key]; old != nil {
		declared := map[string]bool{}
		for _, c := range it.Contributes {
			if c.Kind == kit.KindJSON {
				declared[c.To] = true
			}
		}
		for file, locs := range old.JSON {
			if declared[file] {
				continue // planJSON carries what the fragment leaves out
			}
			for _, loc := range locs {
				jsonLocs.add(file, loc.Pointer, loc.Hash)
			}
		}
	}
	if locs := jsonLocs.sorted(); len(locs) > 0 {
		ip.JSONLocations = locs
	}

	if len(reasons) > 0 || len(confs) > 0 {
		ip.Status = StatusError
		ip.Reasons = reasons
		ip.Conflicts = confs
		ip.Actions = nil
		return ip
	}
	ip.Status = StatusInstall
	return ip
}

// planSection evaluates one section contribution. The
// managed block identifier is the item key: one item owns at most one block per
// shared file, and the lock records the block under the item. resolved carries
// the substituted content of the item's templated contributions. The action is
// nil when the item's own block is up to date or the user's modification is
// kept, and on an explicit reset the owned block is replaced
// regardless.
func planSection(key string, c kit.Contribution, it *kit.Item, in Input, resolved map[string][]byte, shared *sharedFiles, reset bool) (*Action, exclusion) {
	state := in.Snapshot.SharedFile(c.To)
	if state.IsDir {
		return nil, reasonf("%s is a directory", c.To)
	}

	fragment, ok := in.Sources[it.SourcePath(c)]
	if !ok {
		return nil, reasonf("source %s was not provided to the planner", c.Src)
	}
	if sub, ok := resolved[it.SourcePath(c)]; ok {
		fragment = sub
	}

	base := shared.base(in.Snapshot, c.To)
	layout := markdown.Parse(base)

	// The coarse duplicate-heading check: the user may already have written the
	// fragment's instructions by hand, outside any managed block.
	if h := markdown.FirstHeading(string(fragment)); h != "" {
		for _, existing := range layout.Headings {
			if existing == h {
				return nil, conflictExclusion(Conflict{
					Kind:    kit.KindSection,
					File:    c.To,
					Heading: h,
					Cause:   CauseUserContent,
				})
			}
		}
	}

	present := false
	for _, b := range layout.Blocks {
		if b.ID == key {
			present = true
			break
		}
	}
	if present {
		owner, installedBase, owned := in.Lock.SectionOwner(c.To, key)
		if owned && owner == key {
			// Owned by this item: the three-state rules decide,
			// and a reset overwrites whatever the user changed.
			if !reset {
				if write, conflict := ownedAction(installedBase, state.Blocks[key], hashOf(fragment)); conflict != "" {
					return nil, conflictExclusion(Conflict{
						Kind:  kit.KindSection,
						File:  c.To,
						Block: key,
						Owner: key,
						Cause: CauseModified,
					})
				} else if !write {
					return nil, exclusion{}
				}
			}
			content, _ := markdown.Replace(base, key, string(fragment))
			shared.record(c.To, content)
			reason := "update replaces the owned block"
			if reset {
				reason = "reset to kit replaces the owned block"
			}
			return &Action{
				Kind:      ActionSection,
				To:        c.To,
				Content:   []byte(content),
				Block:     key,
				BlockHash: hashOf(fragment),
				Src:       c.Src,
				Reason:    reason,
			}, exclusion{}
		}
		if owned {
			return nil, conflictExclusion(Conflict{
				Kind:  kit.KindSection,
				File:  c.To,
				Block: key,
				Owner: owner,
				Cause: CauseOwned,
			})
		}
		return nil, conflictExclusion(Conflict{
			Kind:  kit.KindSection,
			File:  c.To,
			Block: key,
			Cause: CauseUserContent,
		})
	}

	reason := "block is absent; it will be appended"
	if !state.Exists {
		reason = "shared file is absent; it will be created with the block"
	}
	content := markdown.Append(base, key, string(fragment))
	shared.record(c.To, content)
	return &Action{
		Kind:      ActionSection,
		To:        c.To,
		Content:   []byte(content),
		Block:     key,
		BlockHash: hashOf(fragment),
		Src:       c.Src,
		Reason:    reason,
	}, exclusion{}
}

func hashOf(data []byte) string {
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// jsonRecord accumulates the JSON locations one item takes ownership of across
// its json contributions, deduplicating.
type jsonRecord struct {
	locs map[string][]Location
	seen map[string]map[string]bool
}

func newJSONRecord() *jsonRecord {
	return &jsonRecord{
		locs: map[string][]Location{},
		seen: map[string]map[string]bool{},
	}
}

// add notes one location the item owns.
func (r *jsonRecord) add(file, pointer, hash string) {
	if r.seen[file] == nil {
		r.seen[file] = map[string]bool{}
	}
	id := pointer + "\x00" + hash
	if r.seen[file][id] {
		return
	}
	r.seen[file][id] = true
	r.locs[file] = append(r.locs[file], Location{Pointer: pointer, Hash: hash})
}

// sorted returns the recorded locations per file, sorted by pointer and hash.
func (r *jsonRecord) sorted() map[string][]Location {
	if len(r.locs) == 0 {
		return nil
	}
	out := make(map[string][]Location, len(r.locs))
	for file, locs := range r.locs {
		s := append([]Location(nil), locs...)
		sort.Slice(s, func(i, j int) bool {
			if s[i].Pointer != s[j].Pointer {
				return s[i].Pointer < s[j].Pointer
			}
			return s[i].Hash < s[j].Hash
		})
		out[file] = s
	}
	return out
}

// planJSON evaluates one json contribution under the json location rules: the
// fragment is decomposed into member leaves and array elements, every location
// is decided against the file as the items planned earlier leave it, and the
// action carries the composed file. The action is nil when every location is
// satisfied or co-owned and no byte changes; the item still takes ownership of
// the co-owned locations through rec. resolved carries the substituted
// content of the item's templated contributions; reset marks an explicit
// reset to kit.
func planJSON(key string, c kit.Contribution, it *kit.Item, in Input, resolved map[string][]byte, claims *claimSet, shared *sharedFiles, rec *jsonRecord, reset bool) (*Action, exclusion) {
	state := in.Snapshot.SharedFile(c.To)
	if state.IsDir {
		return nil, reasonf("%s is a directory", c.To)
	}
	src, ok := in.Sources[it.SourcePath(c)]
	if !ok {
		return nil, reasonf("source %s was not provided to the planner", c.Src)
	}
	if sub, ok := resolved[it.SourcePath(c)]; ok {
		src = sub
	}
	frag, err := jsonc.Parse(src)
	if err != nil {
		return nil, reasonOf(fmt.Errorf("source %s does not hold a JSON document: %w", c.Src, err))
	}

	// The member locations the fragment claims, for the conflicts between
	// selected items, and the element pairs it declares, for the carry of the earlier
	// install's locations below.
	leaves := map[string]string{}
	elements := map[string]map[string]bool{}
	err = walkJSON(frag, func(pointer string, element bool, hash string) error {
		if element {
			if elements[pointer] == nil {
				elements[pointer] = map[string]bool{}
			}
			elements[pointer][hash] = true
			return nil
		}
		leaves[pointer] = hash
		return nil
	})
	if err != nil {
		return nil, reasonOf(err)
	}
	if ptr, rivals := claims.jsonConflict(c.To, key, leaves); len(rivals) > 0 {
		return nil, conflictExclusion(Conflict{
			Kind:    kit.KindJSON,
			File:    c.To,
			Pointer: ptr,
			Cause:   CauseClaimed,
			Rivals:  rivals,
		})
	}

	// Locations of the earlier install that this fragment no longer declares
	// stay owned: apply replaces the item's whole lock record, so whatever
	// the record must keep has to be in the plan. A member the
	// fragment re-declares is superseded by the value the plan writes there;
	// the elements of an array stand one by one, so an undeclared pair stays
	// as recorded.
	if old := in.Lock.Items[key]; old != nil {
		for _, loc := range old.JSON[c.To] {
			if _, member := leaves[loc.Pointer]; member {
				continue
			}
			if elements[loc.Pointer][loc.Hash] {
				continue
			}
			rec.add(c.To, loc.Pointer, loc.Hash)
		}
	}

	w := &jsonPlanner{key: key, file: c.To, frag: frag, in: in, shared: shared, rec: rec, reset: reset}

	baseContent, pending := shared.pending[c.To]
	if !pending {
		baseContent = state.Content
	}
	if (!state.Exists && !pending) || strings.TrimSpace(baseContent) == "" {
		// The shared file is created from the fragment as it is written.
		err = walkJSON(frag, func(pointer string, element bool, hash string) error {
			rec.add(c.To, pointer, hash)
			shared.ownJSON(c.To, pointer, hash, key)
			return nil
		})
		if err != nil {
			return nil, reasonOf(err)
		}
		shared.record(c.To, string(src))
		reason := "shared file is absent; it will be created with the fragment"
		if state.Exists {
			reason = "shared file is empty; the fragment becomes its content"
		}
		return &Action{Kind: ActionJSON, To: c.To, Content: src, Src: c.Src, Reason: reason}, exclusion{}
	}

	doc, err := jsonc.Parse([]byte(baseContent))
	if err != nil {
		return nil, reasonOf(fmt.Errorf("%s does not hold a JSON document: %w", c.To, err))
	}
	w.doc = doc
	if exc := w.place(""); !exc.empty() {
		return nil, exc
	}
	if w.wrote == 0 {
		return nil, exclusion{}
	}
	shared.record(c.To, string(doc.Bytes()))
	return &Action{
		Kind:    ActionJSON,
		To:      c.To,
		Content: doc.Bytes(),
		Src:     c.Src,
		Reason:  "fragment values are written into the shared file",
	}, exclusion{}
}

// jsonPlanner places one json contribution into the working document of its
// shared file, deciding every location by the json location table and composing
// the file bytes apply will write. reset marks an explicit reset to kit.
type jsonPlanner struct {
	key    string
	file   string
	doc    *jsonc.Doc // the shared file as the items planned earlier leave it
	frag   *jsonc.Doc // the contribution fragment
	in     Input
	shared *sharedFiles
	rec    *jsonRecord
	reset  bool
	wrote  int
}

// place walks the fragment against the document: a location that is absent
// inserts the missing subtree whole, a location that is present is decided by
// the json location table.
func (w *jsonPlanner) place(ptr string) exclusion {
	fmin, _, err := w.frag.Get(ptr)
	if err != nil {
		return reasonOf(err)
	}
	tmin, tfound, err := w.doc.Get(ptr)
	if err != nil {
		return reasonOf(err)
	}
	switch fmin[0] {
	case '{':
		if !tfound {
			return w.insertSubtree(ptr)
		}
		if tmin[0] != '{' {
			return w.different(ptr)
		}
		names, _, err := w.frag.Members(ptr)
		if err != nil {
			return reasonOf(err)
		}
		for _, name := range names {
			if exc := w.place(pointerJoin(ptr, name)); !exc.empty() {
				return exc
			}
		}
	case '[':
		if !tfound {
			return w.insertSubtree(ptr)
		}
		if tmin[0] != '[' {
			return w.different(ptr)
		}
		return w.placeArray(ptr)
	default:
		if !tfound {
			return w.insertSubtree(ptr)
		}
		return w.placeLeaf(ptr, string(fmin), string(tmin))
	}
	return exclusion{}
}

// placeLeaf decides one member location by the json location table.
func (w *jsonPlanner) placeLeaf(ptr, fmin, tmin string) exclusion {
	incoming := hashOf([]byte(fmin))
	current := hashOf([]byte(tmin))
	if w.in.Lock.OwnsJSONPointer(w.key, w.file, ptr) {
		// Owned by this item: the three-state rules decide,
		// and a reset overwrites whatever the user changed.
		if w.reset {
			return w.overwrite(ptr, incoming)
		}
		write, conflict := ownedAction(w.baseHash(ptr), current, incoming)
		if conflict != "" {
			return conflictExclusion(Conflict{
				Kind:    kit.KindJSON,
				File:    w.file,
				Pointer: ptr,
				Owner:   w.key,
				Cause:   CauseModified,
			})
		}
		if write {
			return w.overwrite(ptr, incoming)
		}
		// The value stays as it stands and stays owned: the update replaces
		// the whole lock record, so the kept location must be in it.
		w.rec.add(w.file, ptr, incoming)
		w.shared.ownJSON(w.file, ptr, incoming, w.key)
		return exclusion{}
	}
	if string(tmin) == string(fmin) {
		if owners := w.owners(ptr, incoming); len(owners) > 0 {
			// Present with an equal value, owned by another item: this item
			// becomes a co-owner; the value stays as it is.
			w.rec.add(w.file, ptr, incoming)
			w.shared.ownJSON(w.file, ptr, incoming, w.key)
		}
		// Present with an equal value, not owned: satisfied, and never owned.
		return exclusion{}
	}
	return w.different(ptr)
}

// placeArray decides the elements of one array location: arrays are treated as
// sets, so an existing equal element is satisfied and a missing one is
// appended and owned.
func (w *jsonPlanner) placeArray(ptr string) exclusion {
	elems, _, err := w.frag.Elements(ptr)
	if err != nil {
		return reasonOf(err)
	}
	existing, _, err := w.doc.Elements(ptr)
	if err != nil {
		return reasonOf(err)
	}
	exset := make(map[string]bool, len(existing))
	for _, e := range existing {
		exset[string(e)] = true
	}
	for i, emin := range elems {
		incoming := hashOf(emin)
		if exset[string(emin)] {
			if owners := w.owners(ptr, incoming); len(owners) > 0 {
				w.rec.add(w.file, ptr, incoming)
				w.shared.ownJSON(w.file, ptr, incoming, w.key)
			}
			continue
		}
		raw, _, err := w.frag.Raw(pointerJoin(ptr, strconv.Itoa(i)))
		if err != nil {
			return reasonOf(err)
		}
		if err := w.doc.Insert(ptr+"/-", raw); err != nil {
			return reasonOf(err)
		}
		w.rec.add(w.file, ptr, incoming)
		w.shared.ownJSON(w.file, ptr, incoming, w.key)
		w.wrote++
	}
	return exclusion{}
}

// insertSubtree writes the fragment subtree at an absent location whole: the
// highest missing ancestor is inserted as the fragment spells it, and every
// location under it is owned by the item.
func (w *jsonPlanner) insertSubtree(ptr string) exclusion {
	raw, _, err := w.frag.Raw(ptr)
	if err != nil {
		return reasonOf(err)
	}
	// Insert takes the pointer of the location itself: it resolves the parent,
	// which exists by the invariant of place, and appends there.
	if err := w.doc.Insert(ptr, raw); err != nil {
		return reasonOf(err)
	}
	w.wrote++
	if err := walkJSONFrom(w.frag, ptr, func(p string, element bool, hash string) error {
		w.rec.add(w.file, p, hash)
		w.shared.ownJSON(w.file, p, hash, w.key)
		return nil
	}); err != nil {
		return reasonOf(err)
	}
	return exclusion{}
}

// overwrite replaces an owned location's value with the fragment's.
func (w *jsonPlanner) overwrite(ptr string, incoming string) exclusion {
	raw, _, err := w.frag.Raw(ptr)
	if err != nil {
		return reasonOf(err)
	}
	if err := w.doc.Replace(ptr, raw); err != nil {
		return reasonOf(err)
	}
	w.rec.add(w.file, ptr, incoming)
	w.shared.ownJSON(w.file, ptr, incoming, w.key)
	w.wrote++
	return exclusion{}
}

// owners lists the items owning the exact location: the installed ones from
// the lock, plus the ones planned before this item.
func (w *jsonPlanner) owners(ptr, hash string) []string {
	owners := w.in.Lock.JSONOwners(w.file, ptr, hash)
	for other := range w.shared.jsonOwnerItems(w.file, ptr, hash) {
		owners = append(owners, other)
	}
	sort.Strings(owners)
	return owners
}

// baseHash returns the hash this item recorded at installation for one member
// location, or "" when it holds none; the three-state rules compare it with
// the current target value.
func (w *jsonPlanner) baseHash(ptr string) string {
	item, ok := w.in.Lock.Items[w.key]
	if !ok {
		return ""
	}
	for _, loc := range item.JSON[w.file] {
		if loc.Pointer == ptr {
			return loc.Hash
		}
	}
	return ""
}

// different reports the json location table's refusal row: the location
// holds a value the fragment does not provide, and this item does not own
// it. When exactly one installed item owns the standing value, the
// conflict names it.
func (w *jsonPlanner) different(ptr string) exclusion {
	c := Conflict{
		Kind:    kit.KindJSON,
		File:    w.file,
		Pointer: ptr,
		Cause:   CauseDifferentValue,
	}
	if tmin, found, err := w.doc.Get(ptr); err == nil && found {
		if owners := w.in.Lock.JSONOwners(w.file, ptr, hashOf(tmin)); len(owners) == 1 {
			c.Owner = owners[0]
		}
	}
	return conflictExclusion(c)
}

// ownedAction resolves the table row "owned by this item" by the three-state
// rules: base is the hash recorded at installation, current the
// hash in the target now, incoming the hash the kit provides. It reports
// whether the incoming value is written, and names the conflict when the item
// is in conflict. Every contribution kind runs its owned content through it.
func ownedAction(base, current, incoming string) (write bool, conflict string) {
	if current == base {
		// Unchanged since installation: an update replaces the content, and
		// an equal incoming value needs no write.
		return incoming != current, ""
	}
	if incoming == base {
		// Modified by the user, and the kit provides the installed content:
		// the user's content is kept without notice.
		return false, ""
	}
	// Modified by the user, and the kit provides different content: in
	// conflict, nothing is written.
	return false, "modified after installation"
}

// walkJSON visits every location a fragment holds: object members holding
// leaves, each with the RFC 6901 pointer of the member, and array elements,
// each with the pointer of its array, because arrays are owned as sets and an
// element is atomic. Composite members are descended; an empty composite holds
// no locations. The hash is over the minimal JSON of the value.
func walkJSON(frag *jsonc.Doc, visit func(pointer string, element bool, hash string) error) error {
	return walkJSONFrom(frag, "", visit)
}

// walkJSONFrom walks the fragment from one pointer, for the locations under an
// inserted subtree.
func walkJSONFrom(frag *jsonc.Doc, start string, visit func(pointer string, element bool, hash string) error) error {
	min, found, err := frag.Get(start)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("fragment value at %q is missing", start)
	}
	var walk func(ptr string, min []byte) error
	walk = func(ptr string, min []byte) error {
		switch min[0] {
		case '{':
			names, _, err := frag.Members(ptr)
			if err != nil {
				return err
			}
			for _, name := range names {
				child := pointerJoin(ptr, name)
				cmin, _, err := frag.Get(child)
				if err != nil {
					return err
				}
				if err := walk(child, cmin); err != nil {
					return err
				}
			}
		case '[':
			elems, _, err := frag.Elements(ptr)
			if err != nil {
				return err
			}
			for _, emin := range elems {
				if err := visit(ptr, true, hashOf(emin)); err != nil {
					return err
				}
			}
		default:
			if err := visit(ptr, false, hashOf(min)); err != nil {
				return err
			}
		}
		return nil
	}
	return walk(start, min)
}

// pointerJoin extends a pointer with one escaped reference token.
func pointerJoin(ptr, token string) string {
	return ptr + "/" + jsonc.EscapeToken(token)
}
