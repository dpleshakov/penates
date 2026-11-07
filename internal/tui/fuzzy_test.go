package tui

import "testing"

// The fuzzy matcher contract (fuzzy.go): which strings match, and the
// order the scores put them in. The table keeps the cases the ranking
// promises: a match at the start beats one inside a word, a consecutive
// run beats a gappy one, and an exact word beats one spread over
// boundaries.

func TestFuzzyMatch(t *testing.T) {
	tests := []struct {
		pattern, s string
		want       bool
	}{
		{"", "anything", true},
		{"", "", true},
		{"z", "opencode", false},
		{"cla", "clue", false},
		{"OC", "opencode", true},     // case-insensitive
		{"open", "xopencoder", true}, // a subsequence anywhere
		{"oe", "opencode", true},     // the pattern may skip
	}
	for _, tt := range tests {
		if _, ok := FuzzyMatch(tt.pattern, tt.s); ok != tt.want {
			t.Errorf("FuzzyMatch(%q, %q) match = %v, want %v", tt.pattern, tt.s, ok, tt.want)
		}
	}
}

func TestFuzzyMatchRanking(t *testing.T) {
	tests := []struct {
		pattern, better, worse string
	}{
		// A match at the start of the string beats one inside a word.
		{"cl", "claude", "my-claude"},
		// A consecutive run beats a gappy one.
		{"ab", "abc", "acb"},
		// An exact word beats the same letters spread over boundaries.
		{"ab", "ab", "a-b"},
		// A shorter gap outranks a longer one.
		{"bd", "abd", "abcd"},
	}
	for _, tt := range tests {
		better, ok := FuzzyMatch(tt.pattern, tt.better)
		if !ok {
			t.Errorf("FuzzyMatch(%q, %q) does not match", tt.pattern, tt.better)
			continue
		}
		worse, ok := FuzzyMatch(tt.pattern, tt.worse)
		if !ok {
			t.Errorf("FuzzyMatch(%q, %q) does not match", tt.pattern, tt.worse)
			continue
		}
		if better <= worse {
			t.Errorf("FuzzyMatch(%q): %q scores %d, want above %q at %d", tt.pattern, tt.better, better, tt.worse, worse)
		}
	}
}

func TestFuzzyMatchDeterministic(t *testing.T) {
	// The score is a pure function of the inputs: the same pair scores the
	// same twice, and the match does not depend on the order the caller
	// asks in.
	for _, tt := range []struct{ pattern, s string }{
		{"open", "~/.config/opencode"},
		{"tgt", "targets"},
		{"э", "данные"},
	} {
		first, _ := FuzzyMatch(tt.pattern, tt.s)
		second, _ := FuzzyMatch(tt.pattern, tt.s)
		if first != second {
			t.Errorf("FuzzyMatch(%q, %q) scored %d then %d", tt.pattern, tt.s, first, second)
		}
	}
}
