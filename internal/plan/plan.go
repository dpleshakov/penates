// Package plan is the pure decision core: kits, lock, snapshot, and source
// contents in; a plan out — self-contained actions with final content bytes,
// reasons, and errors per item. It implements the three-state conflict model of
// the RFC and performs no I/O.
package plan

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"

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
)

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
	shared := &sharedFiles{pending: map[string]string{}}

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
}

// collectClaims indexes every contribution of every selected item by target path.
func collectClaims(in Input, index map[string]*kit.Item) *claimSet {
	c := &claimSet{files: map[string]map[string]bool{}, shared: map[string]map[string]bool{}}
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
			case kit.KindSection, kit.KindJSON:
				add(c.shared, contr.To, key)
			}
		}
	}
	return c
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

// sharedFiles carries the shared-file contents as the planner composes them:
// items are planned in order, and each section action rewrites the whole file,
// so a later item must build on what the earlier ones leave behind.
type sharedFiles struct {
	pending map[string]string
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
			errs = append(errs, "json contributions are not supported yet")
		}
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
