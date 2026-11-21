package view

import (
	"testing"

	"github.com/dpleshakov/penates/internal/kit"
	"github.com/dpleshakov/penates/internal/lock"
	"github.com/dpleshakov/penates/internal/plan"
)

// The state decision, pinned per state: the word, the reason, the major
// mark, and the priority the verdicts and the reconciliation decide with.

// The fixture identities.
const (
	key    = "personal/writing"
	keyAlt = "personal/greeting"
)

// healthyItem is a kit item the loader accepted.
func healthyItem(version string) *kit.Item {
	return &kit.Item{ID: "writing", RowID: "writing", Version: version}
}

func TestDecideAvailableAndInstalled(t *testing.T) {
	it := healthyItem("1.0.0")
	st := Decide(key, it, nil, nil, nil)
	if st.State != StateAvailable || st.Reason != "" || st.Major {
		t.Errorf("Decide without a record = %+v, want available with nothing attached", st)
	}

	rec := &lock.Item{Version: "1.0.0"}
	st = Decide(key, it, rec, &plan.Reconciliation{}, nil)
	if st.State != StateInstalled || st.Reason != "" || st.Major {
		t.Errorf("Decide at the kit version = %+v, want installed", st)
	}
}

func TestDecideVersionMarkers(t *testing.T) {
	it := healthyItem("1.3.0")
	rec := &lock.Item{Version: "1.2.0"}
	st := Decide(key, it, rec, &plan.Reconciliation{}, nil)
	if st.State != StateUpdate || st.Major {
		t.Errorf("Decide behind the kit = %+v, want update, no major", st)
	}

	rec = &lock.Item{Version: "1.4.0"}
	st = Decide(key, it, rec, &plan.Reconciliation{}, nil)
	if st.State != StateDowngrade || st.Major {
		t.Errorf("Decide ahead of the kit = %+v, want downgrade, no major", st)
	}

	// A downgrade across the major version crosses it just the same.
	rec = &lock.Item{Version: "2.0.0"}
	st = Decide(key, it, rec, &plan.Reconciliation{}, nil)
	if st.State != StateDowngrade || !st.Major {
		t.Errorf("Decide ahead across the major = %+v, want downgrade with the major mark", st)
	}
}

// A version change across the major version carries the major mark.
func TestDecideMarksMajor(t *testing.T) {
	it := healthyItem("2.0.0")
	rec := &lock.Item{Version: "1.9.0"}
	st := Decide(key, it, rec, &plan.Reconciliation{}, nil)
	if st.State != StateUpdate || !st.Major {
		t.Errorf("Decide across the major = %+v, want update with the major mark", st)
	}
}

func TestDecideModifiedAndBroken(t *testing.T) {
	it := healthyItem("1.0.0")
	rec := &lock.Item{Version: "1.0.0"}
	mod := &plan.Reconciliation{Modified: map[string]bool{key: true}}
	st := Decide(key, it, rec, mod, nil)
	if st.State != StateModified || st.Reason != "" {
		t.Errorf("Decide modified = %+v, want modified without prose", st)
	}

	brk := &plan.Reconciliation{Broken: map[string]bool{key: true}}
	st = Decide(key, it, rec, brk, nil)
	if st.State != StateBroken || st.Reason != BrokenReason {
		t.Errorf("Decide broken = %+v, want broken with the fixed reason", st)
	}
}

// A warning from a broken declaration: the first problem is the reason,
// and nothing else takes part.
func TestDecideProblemsFirst(t *testing.T) {
	it := &kit.Item{ID: "writing", RowID: "writing~", Version: "1.0.0", Problems: []error{errProblem}}
	rec := &lock.Item{Version: "0.5.0"}
	mod := &plan.Reconciliation{Modified: map[string]bool{key: true}}
	conflict := &plan.ItemPlan{Status: plan.StatusError, Conflicts: []plan.Conflict{{Cause: plan.CauseUserContent, File: "AGENTS.md"}}}
	st := Decide(key, it, rec, mod, conflict)
	if st.State != StateWarning || st.Reason != errProblem.Error() || st.Major {
		t.Errorf("Decide with problems = %+v, want warning with the first problem, nothing else", st)
	}
}

// The verdict lifts conflict and warning over every decided state.
func TestDecideVerdictPriority(t *testing.T) {
	it := healthyItem("1.0.0")
	rec := &lock.Item{Version: "1.0.0"}
	mod := &plan.Reconciliation{Modified: map[string]bool{key: true}}

	warn := &plan.ItemPlan{Status: plan.StatusNoAction, Warning: true, Reason: "content changed without a version change; no update is offered"}
	st := Decide(key, it, rec, mod, warn)
	if st.State != StateWarning || st.Reason != warn.Reason {
		t.Errorf("Decide warning over modified = %+v, want warning with the verdict reason", st)
	}

	conflicted := &plan.ItemPlan{Status: plan.StatusError, Conflicts: []plan.Conflict{{Cause: plan.CauseUserContent, File: "AGENTS.md"}}}
	st = Decide(key, it, rec, mod, conflicted)
	if st.State != StateConflict {
		t.Errorf("Decide conflict over modified = %+v, want conflict", st)
	}

	// A conflict of the first cause is the reason; the others stay out of
	// the one-line reason.
	if st.Reason != ConflictLine(conflicted.Conflicts[0]) {
		t.Errorf("reason = %q, want the first conflict's line", st.Reason)
	}

	// An available item takes the verdict the same way.
	st = Decide(keyAlt, healthyItem2, nil, nil, conflicted)
	if st.State != StateConflict {
		t.Errorf("Decide conflict over available = %+v, want conflict", st)
	}
}

var healthyItem2 = &kit.Item{ID: "greeting", RowID: "greeting", Version: "1.0.0"}

// errProblem is a declaration problem for the fixtures.
var errProblem = &kit.ValidationError{File: "item.yaml", Field: "id", Line: 3, Msg: "the id is unusable"}

// DecideFrozen: the frozen state stands until the reconciliation raises it.
func TestDecideFrozen(t *testing.T) {
	st := DecideFrozen(key, StateOrphan, nil)
	if st.State != StateOrphan || st.Reason != "" || st.Major {
		t.Errorf("DecideFrozen orphan = %+v, want orphan with nothing attached", st)
	}

	mod := &plan.Reconciliation{Modified: map[string]bool{key: true}}
	st = DecideFrozen(key, StateOrphan, mod)
	if st.State != StateModified {
		t.Errorf("DecideFrozen modified = %+v, want modified", st)
	}

	brk := &plan.Reconciliation{Broken: map[string]bool{key: true}}
	st = DecideFrozen(key, StateUnavailable, brk)
	if st.State != StateBroken || st.Reason != BrokenReason {
		t.Errorf("DecideFrozen broken = %+v, want broken with the fixed reason", st)
	}
}

// RowID: the loader's token, or the bare id without one.
func TestRowID(t *testing.T) {
	if got := RowID(&kit.Item{ID: "a", RowID: "b"}); got != "b" {
		t.Errorf("RowID = %q, want the loader's token", got)
	}
	if got := RowID(&kit.Item{ID: "a"}); got != "a" {
		t.Errorf("RowID = %q, want the bare id", got)
	}
}

// The words are a contract: one per state, in the table's order, no
// additions.
func TestStateWords(t *testing.T) {
	want := []string{
		"available", "installed", "update-available", "downgrade-available",
		"modified", "conflict", "broken", "warning", "orphan", "unavailable",
	}
	for i, w := range want {
		if got := State(i).String(); got != w {
			t.Errorf("State(%d).String() = %q, want %q", i, got, w)
		}
	}
	if len(want) != int(StateUnavailable)+1 {
		t.Errorf("the word table holds %d words, want one per state", len(want))
	}
	if StateUpdate.String() == StateDowngrade.String() {
		t.Errorf("update and downgrade share a word")
	}
}
