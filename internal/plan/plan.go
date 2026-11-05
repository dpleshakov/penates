// Package plan is the pure decision core: kits, lock, snapshot, and source
// contents in; a plan out — self-contained actions with final content bytes,
// reasons, errors per item, and the JSON locations each item owns. It
// implements the three-state conflict model of the RFC and the per-location
// table for json contributions (RFC 7.4), and performs no I/O.
package plan

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
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
	// Reasons holds the error reasons when Status is Error.
	Reasons []string
	// Reason explains a NoAction.
	Reason string
	// Actions are the planned writes, in declaration order, when Status is Install.
	Actions []Action
	// JSONLocations lists, per shared file, the JSON locations the item takes
	// ownership of when installed (RFC 7.4): the values it writes, and equal
	// values owned by another item, which make the item a co-owner. Locations
	// merely found satisfied are absent. The lock records them; a plan may
	// install an item whose locations are all satisfied or co-owned without
	// writing any byte.
	JSONLocations map[string][]Location
}

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
)

// Location is one JSON location an item owns inside a shared JSON file
// (RFC 6.7, 7.4): an object member by the RFC 6901 pointer of the member, an
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
}

// Build plans the installation of the selected items. Items that fail validation are
// excluded with their reasons; the others proceed (RFC 7.7).
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

// claimSet records which selected items write which target paths, so that
// cross-item conflicts reach every claimant (RFC 7.5).
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
	// each selected item claims there, to detect the conflicts of 7.5.
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
// fragment claims there (RFC 7.5). Arrays are sets: their elements are not
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

// sortedKeys lists the keys of a set map, sorted.
func sortedKeys(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
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

	if _, installed := lk.Items[key]; installed {
		ip.Status = StatusNoAction
		ip.Reason = "already installed"
		return ip
	}

	var errs []string
	sectionFiles := map[string]bool{}
	jsonLocs := newJSONRecord()
	for _, c := range it.Contributes {
		switch c.Kind {
		case kit.KindFile:
			if others := claims.fileRivals(c.To, key); len(others) > 0 {
				errs = append(errs, fmt.Sprintf("%s is also contributed by %s", c.To, strings.Join(others, ", ")))
				continue
			}
			state := in.Snapshot.File(c.To)
			if state.Exists {
				if owner, _, owned := lk.FileOwner(c.To); owned {
					errs = append(errs, fmt.Sprintf("%s is owned by installed item %s", c.To, owner))
				} else {
					errs = append(errs, fmt.Sprintf("%s exists in the target and is not recorded in the lock; refusing to touch a file that may belong to the user", c.To))
				}
				continue
			}
			content, ok := in.Sources[it.SourcePath(c)]
			if !ok {
				errs = append(errs, fmt.Sprintf("source %s was not provided to the planner", c.Src))
				continue
			}
			ip.Actions = append(ip.Actions, Action{
				Kind:    ActionFile,
				To:      c.To,
				Content: content,
				Src:     c.Src,
				Reason:  "target file is absent",
			})
		case kit.KindSection:
			if others := claims.sharedRivals(c.To, key); len(others) > 0 {
				errs = append(errs, fmt.Sprintf("%s is contributed as a file by %s", c.To, strings.Join(others, ", ")))
				continue
			}
			if others := claims.sectionRivals(c.To, key); len(others) > 0 {
				errs = append(errs, fmt.Sprintf("%s is also contributed as json by %s; one shared file is either Markdown or JSON", c.To, strings.Join(others, ", ")))
				continue
			}
			if owner, _, owned := lk.FileOwner(c.To); owned {
				errs = append(errs, fmt.Sprintf("%s is owned as a whole file by installed item %s", c.To, owner))
				continue
			}
			if sectionFiles[c.To] {
				errs = append(errs, fmt.Sprintf("two section contributions write %s; one item owns one block per shared file", c.To))
				continue
			}
			sectionFiles[c.To] = true
			a, err := planSection(key, c, it, in, shared)
			if err != nil {
				errs = append(errs, err.Error())
				continue
			}
			ip.Actions = append(ip.Actions, *a)
		case kit.KindJSON:
			if others := claims.jsonRivals(c.To, key); len(others) > 0 {
				errs = append(errs, fmt.Sprintf("%s is also contributed as a file or section by %s; one shared file is either Markdown or JSON", c.To, strings.Join(others, ", ")))
				continue
			}
			if owner, _, owned := lk.FileOwner(c.To); owned {
				errs = append(errs, fmt.Sprintf("%s is owned as a whole file by installed item %s", c.To, owner))
				continue
			}
			a, err := planJSON(key, c, it, in, claims, shared, jsonLocs)
			if err != nil {
				errs = append(errs, err.Error())
				continue
			}
			if a != nil {
				ip.Actions = append(ip.Actions, *a)
			}
		}
	}
	if locs := jsonLocs.sorted(); len(locs) > 0 {
		ip.JSONLocations = locs
	}
	if len(it.Vars) > 0 {
		errs = append(errs, "variables are not supported yet")
	}

	if len(errs) > 0 {
		ip.Status = StatusError
		ip.Reasons = errs
		ip.Actions = nil
		return ip
	}
	ip.Status = StatusInstall
	return ip
}

// planSection evaluates one section contribution under the rules of RFC 7.3. The
// managed block identifier is the item key: one item owns at most one block per
// shared file, and the lock records the block under the item.
func planSection(key string, c kit.Contribution, it *kit.Item, in Input, shared *sharedFiles) (*Action, error) {
	state := in.Snapshot.SharedFile(c.To)
	if state.IsDir {
		return nil, fmt.Errorf("%s is a directory", c.To)
	}

	fragment, ok := in.Sources[it.SourcePath(c)]
	if !ok {
		return nil, fmt.Errorf("source %s was not provided to the planner", c.Src)
	}

	base := shared.base(in.Snapshot, c.To)
	layout := markdown.Parse(base)

	// The coarse duplicate-heading check: the user may already have written the
	// fragment's instructions by hand, outside any managed block.
	if h := markdown.FirstHeading(string(fragment)); h != "" {
		for _, existing := range layout.Headings {
			if existing == h {
				return nil, fmt.Errorf("%s already has a heading %q outside managed blocks; the section would duplicate it", c.To, h)
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
		if owner, _, owned := in.Lock.SectionOwner(c.To, key); owned {
			return nil, fmt.Errorf("block %s in %s is owned by installed item %s", key, c.To, owner)
		}
		return nil, fmt.Errorf("%s holds block %s that is not recorded in the lock; refusing to touch content that may belong to the user", c.To, key)
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
	}, nil
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

// planJSON evaluates one json contribution under the rules of RFC 7.4: the
// fragment is decomposed into member leaves and array elements, every location
// is decided against the file as the items planned earlier leave it, and the
// action carries the composed file. The action is nil when every location is
// satisfied or co-owned and no byte changes; the item still takes ownership of
// the co-owned locations through rec.
func planJSON(key string, c kit.Contribution, it *kit.Item, in Input, claims *claimSet, shared *sharedFiles, rec *jsonRecord) (*Action, error) {
	state := in.Snapshot.SharedFile(c.To)
	if state.IsDir {
		return nil, fmt.Errorf("%s is a directory", c.To)
	}
	src, ok := in.Sources[it.SourcePath(c)]
	if !ok {
		return nil, fmt.Errorf("source %s was not provided to the planner", c.Src)
	}
	frag, err := jsonc.Parse(src)
	if err != nil {
		return nil, fmt.Errorf("source %s does not hold a JSON document: %w", c.Src, err)
	}

	// The member locations the fragment claims, for the conflicts of 7.5.
	leaves := map[string]string{}
	err = walkJSON(frag, func(pointer string, element bool, hash string) error {
		if !element {
			leaves[pointer] = hash
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if ptr, rivals := claims.jsonConflict(c.To, key, leaves); len(rivals) > 0 {
		return nil, fmt.Errorf("%s is claimed at %s with a different value by %s", c.To, ptr, strings.Join(rivals, ", "))
	}

	w := &jsonPlanner{key: key, file: c.To, frag: frag, in: in, shared: shared, rec: rec}

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
			return nil, err
		}
		shared.record(c.To, string(src))
		reason := "shared file is absent; it will be created with the fragment"
		if state.Exists {
			reason = "shared file is empty; the fragment becomes its content"
		}
		return &Action{Kind: ActionJSON, To: c.To, Content: src, Src: c.Src, Reason: reason}, nil
	}

	doc, err := jsonc.Parse([]byte(baseContent))
	if err != nil {
		return nil, fmt.Errorf("%s does not hold a JSON document: %w", c.To, err)
	}
	w.doc = doc
	if err := w.place(""); err != nil {
		return nil, err
	}
	if w.wrote == 0 {
		return nil, nil
	}
	shared.record(c.To, string(doc.Bytes()))
	return &Action{
		Kind:    ActionJSON,
		To:      c.To,
		Content: doc.Bytes(),
		Src:     c.Src,
		Reason:  "fragment values are written into the shared file",
	}, nil
}

// jsonPlanner places one json contribution into the working document of its
// shared file, deciding every location by the table of RFC 7.4 and composing
// the file bytes apply will write.
type jsonPlanner struct {
	key    string
	file   string
	doc    *jsonc.Doc // the shared file as the items planned earlier leave it
	frag   *jsonc.Doc // the contribution fragment
	in     Input
	shared *sharedFiles
	rec    *jsonRecord
	wrote  int
}

// place walks the fragment against the document: a location that is absent
// inserts the missing subtree whole, a location that is present is decided by
// the table of RFC 7.4.
func (w *jsonPlanner) place(ptr string) error {
	fmin, _, err := w.frag.Get(ptr)
	if err != nil {
		return err
	}
	tmin, tfound, err := w.doc.Get(ptr)
	if err != nil {
		return err
	}
	switch fmin[0] {
	case '{':
		if !tfound {
			return w.insertSubtree(ptr)
		}
		if tmin[0] != '{' {
			return w.differentError(ptr)
		}
		names, _, err := w.frag.Members(ptr)
		if err != nil {
			return err
		}
		for _, name := range names {
			if err := w.place(pointerJoin(ptr, name)); err != nil {
				return err
			}
		}
	case '[':
		if !tfound {
			return w.insertSubtree(ptr)
		}
		if tmin[0] != '[' {
			return w.differentError(ptr)
		}
		return w.placeArray(ptr)
	default:
		if !tfound {
			return w.insertSubtree(ptr)
		}
		return w.placeLeaf(ptr, string(fmin), string(tmin))
	}
	return nil
}

// placeLeaf decides one member location by the table of RFC 7.4.
func (w *jsonPlanner) placeLeaf(ptr, fmin, tmin string) error {
	incoming := hashOf([]byte(fmin))
	current := hashOf([]byte(tmin))
	if w.in.Lock.OwnsJSONPointer(w.key, w.file, ptr) {
		// Owned by this item: the three-state rules of RFC 7.2 decide.
		write, conflict := jsonOwnedAction(w.baseHash(ptr), current, incoming)
		if conflict != "" {
			return fmt.Errorf("%s holds a value at %s that was modified after installation, and the kit provides different content", w.file, quoteLoc(ptr))
		}
		if write {
			return w.overwrite(ptr, incoming)
		}
		return nil
	}
	if string(tmin) == string(fmin) {
		if owners := w.owners(ptr, incoming); len(owners) > 0 {
			// Present with an equal value, owned by another item: this item
			// becomes a co-owner; the value stays as it is.
			w.rec.add(w.file, ptr, incoming)
			w.shared.ownJSON(w.file, ptr, incoming, w.key)
		}
		// Present with an equal value, not owned: satisfied, and never owned.
		return nil
	}
	return w.differentError(ptr)
}

// placeArray decides the elements of one array location: arrays are treated as
// sets, so an existing equal element is satisfied and a missing one is
// appended and owned.
func (w *jsonPlanner) placeArray(ptr string) error {
	elems, _, err := w.frag.Elements(ptr)
	if err != nil {
		return err
	}
	existing, _, err := w.doc.Elements(ptr)
	if err != nil {
		return err
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
			return err
		}
		if err := w.doc.Insert(ptr+"/-", raw); err != nil {
			return err
		}
		w.rec.add(w.file, ptr, incoming)
		w.shared.ownJSON(w.file, ptr, incoming, w.key)
		w.wrote++
	}
	return nil
}

// insertSubtree writes the fragment subtree at an absent location whole: the
// highest missing ancestor is inserted as the fragment spells it, and every
// location under it is owned by the item.
func (w *jsonPlanner) insertSubtree(ptr string) error {
	raw, _, err := w.frag.Raw(ptr)
	if err != nil {
		return err
	}
	// Insert takes the pointer of the location itself: it resolves the parent,
	// which exists by the invariant of place, and appends there.
	if err := w.doc.Insert(ptr, raw); err != nil {
		return err
	}
	w.wrote++
	return walkJSONFrom(w.frag, ptr, func(p string, element bool, hash string) error {
		w.rec.add(w.file, p, hash)
		w.shared.ownJSON(w.file, p, hash, w.key)
		return nil
	})
}

// overwrite replaces an owned location's value with the fragment's.
func (w *jsonPlanner) overwrite(ptr string, incoming string) error {
	raw, _, err := w.frag.Raw(ptr)
	if err != nil {
		return err
	}
	if err := w.doc.Replace(ptr, raw); err != nil {
		return err
	}
	w.rec.add(w.file, ptr, incoming)
	w.shared.ownJSON(w.file, ptr, incoming, w.key)
	w.wrote++
	return nil
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

// differentError reports the table row that refuses to write: the location
// holds a value the fragment does not provide, and penates does not own it.
func (w *jsonPlanner) differentError(ptr string) error {
	return fmt.Errorf("%s holds a different value at %s", w.file, quoteLoc(ptr))
}

// jsonOwnedAction resolves the table row "owned by this item" by the
// three-state rules of RFC 7.2: base is the hash recorded at installation,
// current the hash in the target now, incoming the hash the kit provides. It
// reports whether the incoming value is written, and names the conflict when
// the item is in conflict.
func jsonOwnedAction(base, current, incoming string) (write bool, conflict string) {
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

// quoteLoc names a location for diagnostics.
func quoteLoc(ptr string) string {
	if ptr == "" {
		return "the root"
	}
	return strconv.Quote(ptr)
}
