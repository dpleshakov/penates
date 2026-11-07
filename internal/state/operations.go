package state

import (
	"fmt"
	"strings"
)

// LockDeleter deletes the lock file of a target. *lock.Store satisfies it;
// the one-method interface keeps this package free of a dependency on
// internal/lock — the operations receive the lock facts they need, the
// item keys of a target's lock, as plain values.
type LockDeleter interface {
	// Delete removes the lock file of target. A missing lock file is not
	// an error.
	Delete(target string) error
}

// AttachKit attaches the registered kit to target, registering the target
// when it is new. Attaching a kit that is already attached is refused, so
// the caller shows the condition instead of repeating the attachment
// silently. The operation saves the registry.
func (s *Store) AttachKit(kit, target string) error {
	return s.Update(func(reg *Registry) error {
		if _, ok := reg.Kits[kit]; !ok {
			return fmt.Errorf("kit %q is not registered", kit)
		}
		if attached(reg, kit, target) {
			return fmt.Errorf("kit %q is already attached to %s", kit, target)
		}
		if _, ok := reg.Targets[target]; !ok {
			reg.Targets[target] = Target{}
		}
		reg.Attachments = append(reg.Attachments, Attachment{Kit: kit, Target: target})
		return nil
	})
}

// DetachKit removes the attachment of the registered kit from the target
// at once, whether or not the kit holds installed items there. The files
// in the target are not touched: installed items stay installed and
// owned, only no longer offered through this attachment. Pending selects
// of the kit's items in the target are dropped with their answers — a
// detached kit's items are not offered for installation any more — while
// reset and remove marks stay, working on installed content through the
// self-sufficient lock. The operation saves the registry.
func (s *Store) DetachKit(kit, target string) error {
	return s.Update(func(reg *Registry) error {
		if _, ok := reg.Kits[kit]; !ok {
			return fmt.Errorf("kit %q is not registered", kit)
		}
		if !attached(reg, kit, target) {
			return fmt.Errorf("kit %q is not attached to %s", kit, target)
		}
		detach(reg, kit, target)
		return nil
	})
}

// DeferDetachKit queues the detach of the registered kit from the target
// as a deferred operation: it waits for the apply that removes the kit's
// installed items in the target, and CompleteDeferred finishes it.
// Queueing an already queued detach changes nothing. The operation saves
// the registry.
func (s *Store) DeferDetachKit(kit, target string) error {
	return s.Update(func(reg *Registry) error {
		if _, ok := reg.Kits[kit]; !ok {
			return fmt.Errorf("kit %q is not registered", kit)
		}
		if !attached(reg, kit, target) {
			return fmt.Errorf("kit %q is not attached to %s", kit, target)
		}
		p := reg.pendingFor(target)
		for _, d := range p.Deferred {
			if d.Kind == DeferredDetachKit && d.Kit == kit {
				return nil
			}
		}
		p.Deferred = append(p.Deferred, Deferred{Kind: DeferredDetachKit, Kit: kit})
		reg.Pending[target] = p
		return nil
	})
}

// EditKitPath points the registered kit to dir. The edit is accepted only
// when dir holds a kit of the same name — dirKitName is the name the kit
// in dir declares, which the caller reads from dir before calling — and a
// mismatch is refused with the reason naming both kits. The kit's name is
// its identity, so its attachments and pending actions survive the move.
// The operation saves the registry.
func (s *Store) EditKitPath(kit, dir, dirKitName string) error {
	return s.Update(func(reg *Registry) error {
		if _, ok := reg.Kits[kit]; !ok {
			return fmt.Errorf("kit %q is not registered", kit)
		}
		if dirKitName != kit {
			return fmt.Errorf("the directory %s holds the kit %q, not %q", dir, dirKitName, kit)
		}
		k := reg.Kits[kit]
		k.Dir = dir
		reg.Kits[kit] = k
		return nil
	})
}

// ForgetTarget forgets the registered target at once: it deletes the
// target's lock, removes the target with its attachments and pending
// actions, and clears the last active target when it was the one
// forgotten. The files in the target are not touched; what the lock
// recorded is no longer owned. A forget that waits for the apply removing
// the target's installed items is queued with DeferForgetTarget instead.
// The lock goes before the registry: a save that fails after the deletion
// can only leave an empty lock file behind, never a forgotten target with
// a live lock. The operation saves the registry.
func (s *Store) ForgetTarget(target string, locks LockDeleter) error {
	return s.Update(func(reg *Registry) error {
		if _, ok := reg.Targets[target]; !ok {
			return fmt.Errorf("target %s is not registered", target)
		}
		if err := locks.Delete(target); err != nil {
			return fmt.Errorf("forget %s: %w", target, err)
		}
		forget(reg, target)
		return nil
	})
}

// DeferForgetTarget queues the forget of the registered target as a
// deferred operation: it waits for the apply that removes the target's
// installed items, and CompleteDeferred finishes it. Queueing an already
// queued forget changes nothing. The operation saves the registry.
func (s *Store) DeferForgetTarget(target string) error {
	return s.Update(func(reg *Registry) error {
		if _, ok := reg.Targets[target]; !ok {
			return fmt.Errorf("target %s is not registered", target)
		}
		p := reg.pendingFor(target)
		for _, d := range p.Deferred {
			if d.Kind == DeferredForgetTarget {
				return nil
			}
		}
		p.Deferred = append(p.Deferred, Deferred{Kind: DeferredForgetTarget})
		reg.Pending[target] = p
		return nil
	})
}

// CompleteDeferred finishes the deferred operations of the registered
// target after an apply. installed holds the item keys of the target's
// lock after the apply; locks deletes the lock of a completed forget. A
// deferred detach completes when the lock holds no installed items of its
// kit any more, a deferred forget when it holds none at all; an operation
// the apply did not cover completely is dropped, with the reason in the
// returned report for the interface to show. Every queued operation is
// consumed either way. The operation saves the registry.
func (s *Store) CompleteDeferred(target string, installed []string, locks LockDeleter) ([]string, error) {
	var report []string
	err := s.Update(func(reg *Registry) error {
		if _, ok := reg.Targets[target]; !ok {
			return fmt.Errorf("target %s is not registered", target)
		}
		p, ok := reg.Pending[target]
		if !ok || len(p.Deferred) == 0 {
			return nil
		}
		// The forget goes first: when it completes, the target dies and
		// takes the rest of the pending actions with it.
		for _, d := range p.Deferred {
			if d.Kind != DeferredForgetTarget {
				continue
			}
			if len(installed) == 0 {
				if err := locks.Delete(target); err != nil {
					return fmt.Errorf("forget %s: %w", target, err)
				}
				forget(reg, target)
				report = append(report, fmt.Sprintf("completed the deferred forget of %s", target))
				return nil
			}
			report = append(report, fmt.Sprintf("dropped the deferred forget of %s: installed items remain: %s", target, strings.Join(installed, ", ")))
			break
		}
		for _, d := range p.Deferred {
			if d.Kind != DeferredDetachKit {
				continue
			}
			remaining := installedOf(d.Kit, installed)
			if len(remaining) > 0 {
				report = append(report, fmt.Sprintf("dropped the deferred detach of %s from %s: installed items remain: %s", d.Kit, target, strings.Join(remaining, ", ")))
				continue
			}
			detach(reg, d.Kit, target)
			report = append(report, fmt.Sprintf("completed the deferred detach of %s from %s", d.Kit, target))
		}
		p.Deferred = nil
		if len(p.Items) == 0 {
			delete(reg.Pending, target)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return report, nil
}

// attached reports whether the attachment of kit to target is recorded.
func attached(reg *Registry, kit, target string) bool {
	for _, a := range reg.Attachments {
		if a.Kit == kit && a.Target == target {
			return true
		}
	}
	return false
}

// detach removes the attachment of kit from target and drops the select
// marks and answers of the kit's items in the target's pending actions,
// collapsing the pending object when nothing is left in it.
func detach(reg *Registry, kit, target string) {
	kept := reg.Attachments[:0]
	for _, a := range reg.Attachments {
		if a.Kit == kit && a.Target == target {
			continue
		}
		kept = append(kept, a)
	}
	reg.Attachments = kept
	p, ok := reg.Pending[target]
	if !ok {
		return
	}
	prefix := kit + "/"
	for key, item := range p.Items {
		if !strings.HasPrefix(key, prefix) {
			continue
		}
		item.Select = false
		item.Answers = nil
		if !item.Reset && !item.Remove {
			delete(p.Items, key)
		}
	}
	if len(p.Items) == 0 && len(p.Deferred) == 0 {
		delete(reg.Pending, target)
	}
}

// forget removes the target with its attachments and pending actions and
// clears the last active target when it pointed at the forgotten one.
func forget(reg *Registry, target string) {
	delete(reg.Targets, target)
	kept := reg.Attachments[:0]
	for _, a := range reg.Attachments {
		if a.Target != target {
			kept = append(kept, a)
		}
	}
	reg.Attachments = kept
	delete(reg.Pending, target)
	if reg.LastTarget == target {
		reg.LastTarget = ""
	}
}

// installedOf lists the item keys of kit among installed, the item keys
// of a target's lock.
func installedOf(kit string, installed []string) []string {
	var keys []string
	prefix := kit + "/"
	for _, key := range installed {
		if strings.HasPrefix(key, prefix) {
			keys = append(keys, key)
		}
	}
	return keys
}
