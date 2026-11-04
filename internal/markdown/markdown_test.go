package markdown

import (
	"strings"
	"testing"
)

func TestParse(t *testing.T) {
	tests := map[string]struct {
		in           string
		wantBlocks   []Block
		wantHeadings []string
	}{
		"empty file": {in: "", wantHeadings: nil},
		"user file without blocks": {
			in:           "# User notes\n\ntext\n\n## More\n",
			wantHeadings: []string{"user notes", "more"},
		},
		"one block": {
			in:           "intro\n\n<!-- penates:begin kit/one -->\n## House rules\n\nbody\n<!-- penates:end kit/one -->\n",
			wantBlocks:   []Block{{ID: "kit/one", Content: "## House rules\n\nbody"}},
			wantHeadings: nil,
		},
		"two blocks in order": {
			in: "<!-- penates:begin a/1 -->\none\n<!-- penates:end a/1 -->\n\n# Title\n\n<!-- penates:begin b/2 -->\ntwo\n<!-- penates:end b/2 -->\n",
			wantBlocks: []Block{
				{ID: "a/1", Content: "one"},
				{ID: "b/2", Content: "two"},
			},
			wantHeadings: []string{"title"},
		},
		"headings inside a block are not outside headings": {
			in:         "<!-- penates:begin a/1 -->\n## Inside\n<!-- penates:end a/1 -->\n",
			wantBlocks: []Block{{ID: "a/1", Content: "## Inside"}},
		},
		"empty block": {
			in:         "<!-- penates:begin a/1 -->\n<!-- penates:end a/1 -->\n",
			wantBlocks: []Block{{ID: "a/1", Content: ""}},
		},
		"unbalanced begin is user content": {
			in:           "<!-- penates:begin a/1 -->\n## Heading inside nothing\n",
			wantHeadings: []string{"heading inside nothing"},
		},
		"mismatched end id keeps block open": {
			in:         "<!-- penates:begin a/1 -->\ncontent\n<!-- penates:end other -->\n<!-- penates:end a/1 -->\n",
			wantBlocks: []Block{{ID: "a/1", Content: "content\n<!-- penates:end other -->"}},
		},
		"setext heading is not detected": {
			in:           "Title\n=====\n",
			wantHeadings: nil,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			got := Parse(tc.in)
			if len(got.Blocks) != len(tc.wantBlocks) {
				t.Fatalf("blocks = %+v, want %+v", got.Blocks, tc.wantBlocks)
			}
			for i, want := range tc.wantBlocks {
				if got.Blocks[i] != want {
					t.Errorf("block %d = %+v, want %+v", i, got.Blocks[i], want)
				}
			}
			if len(got.Headings) != len(tc.wantHeadings) {
				t.Fatalf("headings = %q, want %q", got.Headings, tc.wantHeadings)
			}
			for i, want := range tc.wantHeadings {
				if got.Headings[i] != want {
					t.Errorf("heading %d = %q, want %q", i, got.Headings[i], want)
				}
			}
		})
	}
}

func TestAppend(t *testing.T) {
	tests := map[string]struct {
		in   string
		want string
	}{
		"empty file": {
			in:   "",
			want: "<!-- penates:begin k/i -->\ncontent\n<!-- penates:end k/i -->\n",
		},
		"whitespace-only file": {
			in:   "\n  \n",
			want: "<!-- penates:begin k/i -->\ncontent\n<!-- penates:end k/i -->\n",
		},
		"file without trailing newline": {
			in:   "# Notes",
			want: "# Notes\n\n<!-- penates:begin k/i -->\ncontent\n<!-- penates:end k/i -->\n",
		},
		"file with trailing newline": {
			in:   "# Notes\n",
			want: "# Notes\n\n<!-- penates:begin k/i -->\ncontent\n<!-- penates:end k/i -->\n",
		},
		"after an existing block": {
			in:   "<!-- penates:begin a/1 -->\none\n<!-- penates:end a/1 -->\n",
			want: "<!-- penates:begin a/1 -->\none\n<!-- penates:end a/1 -->\n\n<!-- penates:begin k/i -->\ncontent\n<!-- penates:end k/i -->\n",
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			if got := Append(tc.in, "k/i", "content"); got != tc.want {
				t.Errorf("Append =\n%q\nwant\n%q", got, tc.want)
			}
		})
	}
}

func TestReplace(t *testing.T) {
	in := "# Notes\n\n<!-- penates:begin k/i -->\nold line\n\nmore\n<!-- penates:end k/i -->\n\ntail\n"
	want := "# Notes\n\n<!-- penates:begin k/i -->\nnew\n<!-- penates:end k/i -->\n\ntail\n"
	got, ok := Replace(in, "k/i", "new")
	if !ok || got != want {
		t.Errorf("Replace = %q, %v; want %q", got, ok, want)
	}

	if _, ok := Replace(in, "k/other", "new"); ok {
		t.Error("Replace reported success for a missing block")
	}

	// Everything outside the markers stays byte for byte.
	if !strings.HasPrefix(got, "# Notes\n\n<!-- penates:begin k/i -->\n") {
		t.Errorf("Replace touched the head of the file: %q", got)
	}
	if !strings.HasSuffix(got, "<!-- penates:end k/i -->\n\ntail\n") {
		t.Errorf("Replace touched the tail of the file: %q", got)
	}
}

func TestFirstHeading(t *testing.T) {
	tests := map[string]struct {
		in   string
		want string
	}{
		"none":                {in: "plain text\n", want: ""},
		"atx":                 {in: "text\n\n## House Rules for gh\n\nbody", want: "house rules for gh"},
		"skips closed hashes": {in: "## Heading ##\n", want: "heading"},
		"first of several":    {in: "# A\n\n# B\n", want: "a"},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			if got := FirstHeading(tc.in); got != tc.want {
				t.Errorf("FirstHeading = %q, want %q", got, tc.want)
			}
		})
	}
}
