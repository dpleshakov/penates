package tui

import (
	"strings"
	"unicode"
)

// The fuzzy matcher the search inputs share: the target list (8.1) filters
// its rows through it, and the command palette (9) will do the same over
// its commands. The match is a case-insensitive subsequence of runes; the
// score ranks better matches higher and is a deterministic function of its
// inputs, so the same pattern orders the same entries the same way on
// every run.

// FuzzyMatch reports whether s matches pattern as a subsequence of runes,
// case-insensitively, with a score that ranks better matches higher:
// a rune matched scores its base, matching at the start of s or after a
// word boundary scores a bonus, extending a consecutive run scores a
// bonus, and every rune skipped between matches costs. An empty pattern
// matches everything with a zero score.
func FuzzyMatch(pattern, s string) (int, bool) {
	p := []rune(strings.ToLower(pattern))
	if len(p) == 0 {
		return 0, true
	}
	t := []rune(strings.ToLower(s))
	if len(p) > len(t) {
		return 0, false
	}
	score := 0
	prev := -1 // the index of the previously matched rune in t
	from := 0
	for _, r := range p {
		found := -1
		for j := from; j < len(t); j++ {
			if t[j] == r {
				found = j
				break
			}
		}
		if found < 0 {
			return 0, false
		}
		score += 2
		switch {
		case prev < 0:
			// The first match: at the start of the string, or after a
			// word boundary.
			if found == 0 {
				score += 4
			} else if fuzzBoundary(t, found) {
				score += 2
			}
		case found == prev+1:
			// Extending a consecutive run.
			score += 2
		default:
			// Every skipped rune costs; landing after a word boundary
			// wins the cost back once.
			score -= found - prev - 1
			if fuzzBoundary(t, found) {
				score += 2
			}
		}
		prev = found
		from = found + 1
	}
	return score, true
}

// fuzzBoundary reports whether the rune at i starts a word: the rune
// before it is not a letter or a digit, so a separator, a space or the
// start of the string opens one.
func fuzzBoundary(t []rune, i int) bool {
	if i == 0 {
		return true
	}
	r := t[i-1]
	return !unicode.IsLetter(r) && !unicode.IsDigit(r)
}
