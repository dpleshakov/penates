package view

import (
	"testing"

	"github.com/dpleshakov/penates/internal/kit"
	"github.com/dpleshakov/penates/internal/plan"
)

// The prose of the structured verdicts, pinned per cause and kind: this is
// the wording both the tree reasons and the CLI item lines speak.

func TestConflictLines(t *testing.T) {
	tests := []struct {
		name     string
		conflict plan.Conflict
		want     string
	}{
		{
			name: "a file claimed by rivals",
			conflict: plan.Conflict{
				Cause:  plan.CauseClaimed,
				Kind:   kit.KindFile,
				File:   "AGENTS.md",
				Rivals: []string{"a/one", "a/two"},
			},
			want: "AGENTS.md is also contributed by a/one, a/two",
		},
		{
			name: "a json pointer claimed with another value",
			conflict: plan.Conflict{
				Cause:   plan.CauseClaimed,
				Kind:    kit.KindJSON,
				File:    "settings.json",
				Pointer: "/model",
				Rivals:  []string{"a/one"},
			},
			want: `settings.json is claimed at "/model" with a different value by a/one`,
		},
		{
			name: "claimed as a file",
			conflict: plan.Conflict{
				Cause:  plan.CauseClaimedAsFile,
				File:   "settings.json",
				Rivals: []string{"a/one"},
			},
			want: "settings.json is contributed as a file by a/one",
		},
		{
			name: "claimed as json",
			conflict: plan.Conflict{
				Cause:  plan.CauseClaimedAsJSON,
				File:   "notes.md",
				Rivals: []string{"a/one"},
			},
			want: "notes.md is also contributed as json by a/one; one shared file is either Markdown or JSON",
		},
		{
			name: "claimed as a file or section",
			conflict: plan.Conflict{
				Cause:  plan.CauseClaimedAsFileOrSection,
				File:   "notes.md",
				Rivals: []string{"a/one"},
			},
			want: "notes.md is also contributed as a file or section by a/one; one shared file is either Markdown or JSON",
		},
		{
			name: "user content by a heading",
			conflict: plan.Conflict{
				Cause:   plan.CauseUserContent,
				File:    "AGENTS.md",
				Heading: "Git usage",
			},
			want: `AGENTS.md already has a heading "Git usage" outside managed blocks; the section would duplicate it`,
		},
		{
			name: "user content by a block",
			conflict: plan.Conflict{
				Cause: plan.CauseUserContent,
				File:  "AGENTS.md",
				Block: "release-notes",
			},
			want: "AGENTS.md holds block release-notes that is not recorded in the lock; refusing to touch content that may belong to the user",
		},
		{
			name: "user content by a whole file",
			conflict: plan.Conflict{
				Cause: plan.CauseUserContent,
				File:  "plugin.ts",
			},
			want: "plugin.ts exists in the target and is not recorded in the lock; refusing to touch a file that may belong to the user",
		},
		{
			name: "owned as a file",
			conflict: plan.Conflict{
				Cause: plan.CauseOwned,
				Kind:  kit.KindFile,
				File:  "plugin.ts",
				Owner: "other/thing",
			},
			want: "plugin.ts is owned by installed item other/thing",
		},
		{
			name: "owned as a block",
			conflict: plan.Conflict{
				Cause: plan.CauseOwned,
				Kind:  kit.KindSection,
				File:  "AGENTS.md",
				Block: "greeting",
				Owner: "other/greeting",
			},
			want: "block greeting in AGENTS.md is owned by installed item other/greeting",
		},
		{
			name: "owned as a whole file",
			conflict: plan.Conflict{
				Cause: plan.CauseOwned,
				Kind:  kit.KindSection,
				File:  "AGENTS.md",
				Owner: "other/greeting",
			},
			want: "AGENTS.md is owned as a whole file by installed item other/greeting",
		},
		{
			name: "modified file",
			conflict: plan.Conflict{
				Cause: plan.CauseModified,
				Kind:  kit.KindFile,
				File:  "plugin.ts",
			},
			want: "plugin.ts was modified after installation, and the kit provides different content",
		},
		{
			name: "modified block",
			conflict: plan.Conflict{
				Cause: plan.CauseModified,
				Kind:  kit.KindSection,
				File:  "AGENTS.md",
				Block: "greeting",
			},
			want: "block greeting in AGENTS.md was modified after installation, and the kit provides different content",
		},
		{
			name: "modified json value",
			conflict: plan.Conflict{
				Cause:   plan.CauseModified,
				Kind:    kit.KindJSON,
				File:    "settings.json",
				Pointer: "/model",
			},
			want: `settings.json holds a value at "/model" that was modified after installation, and the kit provides different content`,
		},
		{
			name: "a different value at the root",
			conflict: plan.Conflict{
				Cause:   plan.CauseDifferentValue,
				File:    "settings.json",
				Pointer: "",
			},
			want: "settings.json holds a different value at the root",
		},
		{
			name: "two sections in one file",
			conflict: plan.Conflict{
				Cause: plan.CauseTwoSections,
				File:  "AGENTS.md",
			},
			want: "two section contributions write AGENTS.md; one item owns one block per shared file",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ConflictLine(tt.conflict); got != tt.want {
				t.Errorf("ConflictLine = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestExclusionLines(t *testing.T) {
	ip := &plan.ItemPlan{
		Conflicts: []plan.Conflict{{Cause: plan.CauseUserContent, File: "plugin.ts"}},
		Reasons:   []string{"no source file"},
	}
	want := []string{
		"plugin.ts exists in the target and is not recorded in the lock; refusing to touch a file that may belong to the user",
		"no source file",
	}
	got := ExclusionLines(ip)
	if len(got) != len(want) {
		t.Fatalf("ExclusionLines = %q, want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("ExclusionLines[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}
