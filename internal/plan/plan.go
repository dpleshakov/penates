// Package plan is the pure decision core: kits, lock, snapshot, and selection in; a
// plan out — actions per item with reasons, errors per item. It implements the
// three-state conflict model of the RFC and performs no I/O.
package plan

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/dpleshakov/penates/internal/kit"
	"github.com/dpleshakov/penates/internal/lock"
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

// Action is one planned write of a file contribution.
type Action struct {
	// Src is the absolute source path in the item directory.
	Src string
	// To is the target-relative path, with forward slashes.
	To string
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
	// every file path any selected item contributes.
	Snapshot *target.Snapshot
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

	p := &Plan{}
	seen := map[string]bool{}
	for _, key := range in.Selection {
		if seen[key] {
			continue
		}
		seen[key] = true
		p.Items = append(p.Items, planItem(key, index, in.Lock, in.Snapshot, claims))
	}
	return p
}

// collectClaims maps every target path contributed as a file by any selected item to
// the set of items claiming it, so that cross-item conflicts can be reported to every
// claimant (RFC 7.5).
func collectClaims(in Input, index map[string]*kit.Item) map[string]map[string]bool {
	claims := map[string]map[string]bool{}
	for _, key := range in.Selection {
		it, ok := index[key]
		if !ok {
			continue
		}
		for _, c := range it.Contributes {
			if c.Kind != kit.KindFile {
				continue
			}
			if claims[c.To] == nil {
				claims[c.To] = map[string]bool{}
			}
			claims[c.To][key] = true
		}
	}
	return claims
}

func planItem(key string, index map[string]*kit.Item, lk *lock.Lock, snap *target.Snapshot, claims map[string]map[string]bool) *ItemPlan {
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

	if _, installed := lk.Items[key]; installed {
		ip.Status = StatusNoAction
		ip.Reason = "already installed"
		return ip
	}

	var errs []string
	for _, c := range it.Contributes {
		switch c.Kind {
		case kit.KindFile:
			if others := otherClaimants(claims[c.To], key); len(others) > 0 {
				errs = append(errs, fmt.Sprintf("%s is also contributed by %s", c.To, strings.Join(others, ", ")))
				continue
			}
			state := snap.File(c.To)
			switch {
			case !state.Exists:
				ip.Actions = append(ip.Actions, Action{
					Src:    filepath.Join(it.Dir, filepath.FromSlash(c.Src)),
					To:     c.To,
					Reason: "target file is absent",
				})
			default:
				if owner, _, owned := lk.FileOwner(c.To); owned {
					errs = append(errs, fmt.Sprintf("%s is owned by installed item %s", c.To, owner))
				} else {
					errs = append(errs, fmt.Sprintf("%s exists in the target and is not recorded in the lock; refusing to touch a file that may belong to the user", c.To))
				}
			}
		case kit.KindSection, kit.KindJSON:
			errs = append(errs, fmt.Sprintf("%s contributions are not supported yet", c.Kind))
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

// otherClaimants lists the items claiming a path besides key, in stable order.
func otherClaimants(claimants map[string]bool, key string) []string {
	var others []string
	for other := range claimants {
		if other != key {
			others = append(others, other)
		}
	}
	sort.Strings(others)
	return others
}
