package jsonc

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func mustParse(t *testing.T, data string) *Doc {
	t.Helper()
	d, err := Parse([]byte(data))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	return d
}

func TestRoundTrip(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		doc  string
	}{
		{"compact", `{"a":1,"b":[1,2]}`},
		{"pretty", "{\n  \"a\": 1,\n  \"b\": [\n    1,\n    2\n  ]\n}\n"},
		{"line comments", "{\n  // about a\n  \"a\": 1, // trailing\n  \"b\": 2\n}\n"},
		{"block comments", "{ /* head */ \"a\": /* mid */ 1 /* tail */ }\n"},
		{"trailing commas", "{\n  \"a\": 1,\n  \"b\": [\n    1,\n    2,\n  ],\n}\n"},
		{"empty composites", "{\n  \"a\": {},\n  \"b\": [],\n  \"c\": {\n  },\n}\n"},
		{"leading comment", "// header\n{\"a\": 1}\n"},
		{"escapes", `{"a\b": "x\ny", "tab": "\t"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			d := mustParse(t, tt.doc)
			if diff := cmp.Diff(tt.doc, string(d.Bytes())); diff != "" {
				t.Errorf("round trip changed the document (-want +got):\n%s", diff)
			}
		})
	}
}

func TestGet(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		doc    string
		ptr    string
		want   string
		found  bool
		wanErr bool
	}{
		{"whole document standardized", "{ \"a\": 1 }", "", `{"a":1}`, true, false},
		{"member", "{\n  \"a\": 1,\n  \"b\": 2\n}", "/b", `2`, true, false},
		{"nested object standardized", "{\n  \"a\": /* c */ {\n    \"b\": [1, 2,],\n  },\n}", "/a", `{"b":[1,2]}`, true, false},
		{"array element", "[\n  \"x\",\n  \"y\"\n]", "/1", `"y"`, true, false},
		{"escaped slash in name", `{"a/b": 1, "plain": 2}`, "/a~1b", `1`, true, false},
		{"escaped tilde in name", `{"a~b": 2}`, "/a~0b", `2`, true, false},
		{"tilde and slash in one name", `{"a~1b": 3}`, "/a~01b", `3`, true, false},
		{"name written with unicode escape", `{"\u0061x": 7}`, "/ax", `7`, true, false},
		{"name with a space", `{"a b": 5}`, "/a b", `5`, true, false},
		{"absent member", `{"a": 1}`, "/z", "", false, false},
		{"array append token", "[1, 2]", "/-", "", false, false},
		{"array index at length", "[1, 2]", "/2", "", false, false},
		{"pointer without slash", `{"a": 1}`, "a", "", false, true},
		{"invalid escape", `{"a": 1}`, "/a~2", "", false, true},
		{"lone tilde escape", `{"a": 1}`, "/~", "", false, true},
		{"index into a scalar", `{"a": 1}`, "/a/x", "", false, true},
		{"array index not a number", "[1]", "/x", "", false, true},
		{"array index with leading zero", "[1]", "/01", "", false, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			d := mustParse(t, tt.doc)
			got, found, err := d.Get(tt.ptr)
			if tt.wanErr {
				if err == nil {
					t.Fatalf("Get(%q) = %q, %v; want error", tt.ptr, got, found)
				}
				return
			}
			if err != nil {
				t.Fatalf("Get(%q): %v", tt.ptr, err)
			}
			if found != tt.found {
				t.Fatalf("Get(%q) found = %v, want %v", tt.ptr, found, tt.found)
			}
			if diff := cmp.Diff(tt.want, string(got)); diff != "" {
				t.Errorf("Get(%q) (-want +got):\n%s", tt.ptr, diff)
			}
			if found && !json.Valid(got) {
				t.Errorf("Get(%q) = %q: not valid standard JSON", tt.ptr, got)
			}
		})
	}
}

func TestInsertObject(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		doc    string
		ptr    string
		value  string
		want   string
		wanErr string
	}{
		{
			name:  "append to a multi-line object",
			doc:   "{\n  \"a\": 1\n}\n",
			ptr:   "/b",
			value: "2",
			want:  "{\n  \"a\": 1,\n  \"b\": 2\n}\n",
		},
		{
			name:  "append to a single-line object",
			doc:   `{ "a": 1 }`,
			ptr:   "/b",
			value: "2",
			want:  `{ "a": 1, "b": 2 }`,
		},
		{
			name:  "append to a compact object",
			doc:   `{"a":1}`,
			ptr:   "/b",
			value: "2",
			want:  `{"a":1, "b":2}`,
		},
		{
			name:  "append to an empty object",
			doc:   "{}",
			ptr:   "/b",
			value: "2",
			want:  "{\n  \"b\": 2}",
		},
		{
			name:  "append keeps the trailing comma",
			doc:   "{\n  \"a\": 1,\n}\n",
			ptr:   "/b",
			value: "2",
			want:  "{\n  \"a\": 1,\n  \"b\": 2,\n}\n",
		},
		{
			name:  "append copies tab indentation",
			doc:   "{\n\t\"a\": 1\n}\n",
			ptr:   "/b",
			value: "2",
			want:  "{\n\t\"a\": 1,\n\t\"b\": 2\n}\n",
		},
		{
			name:  "append copies indentation from a commented neighbor",
			doc:   "{\n  \"a\": 1,\n  // about b\n  \"b\": 2\n}\n",
			ptr:   "/c",
			value: "3",
			want:  "{\n  \"a\": 1,\n  // about b\n  \"b\": 2,\n  \"c\": 3\n}\n",
		},
		{
			name:  "member name with a space",
			doc:   "{\n  \"a\": 1\n}\n",
			ptr:   "/a b",
			value: `"two words"`,
			want:  "{\n  \"a\": 1,\n  \"a b\": \"two words\"\n}\n",
		},
		{
			name:  "value keeps its inner comments",
			doc:   "{\n  \"a\": 1\n}\n",
			ptr:   "/b",
			value: "{\"x\": 1 /* inline */,}",
			want:  "{\n  \"a\": 1,\n  \"b\": {\"x\": 1 /* inline */,}\n}\n",
		},
		{
			name:  "insert into a nested empty object",
			doc:   `{"a": {}}`,
			ptr:   "/a/b",
			value: "2",
			want:  "{\"a\": {\n  \"b\": 2}}",
		},
		{
			name:   "member already exists",
			doc:    `{"a": 1}`,
			ptr:    "/a",
			value:  "2",
			wanErr: `member "a" already exists`,
		},
		{
			name:   "parent missing",
			doc:    `{"a": 1}`,
			ptr:    "/missing/b",
			value:  "2",
			wanErr: "not found",
		},
		{
			name:   "parent is a scalar",
			doc:    `{"a": 1}`,
			ptr:    "/a/b",
			value:  "2",
			wanErr: "cannot add into a scalar",
		},
		{
			name:   "value is not JSON",
			doc:    `{"a": 1}`,
			ptr:    "/b",
			value:  "{oops",
			wanErr: "parse value",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			d := mustParse(t, tt.doc)
			err := d.Insert(tt.ptr, []byte(tt.value))
			if tt.wanErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wanErr) {
					t.Fatalf("Insert(%q, %q) = %v; want error holding %q", tt.ptr, tt.value, err, tt.wanErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("Insert(%q, %q): %v", tt.ptr, tt.value, err)
			}
			if diff := cmp.Diff(tt.want, string(d.Bytes())); diff != "" {
				t.Errorf("document after insert (-want +got):\n%s", diff)
			}
		})
	}
}

func TestInsertArray(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		doc    string
		ptr    string
		value  string
		want   string
		wanErr string
	}{
		{
			name:  "append by index",
			doc:   "[\n  1\n]\n",
			ptr:   "/1",
			value: "2",
			want:  "[\n  1,\n  2\n]\n",
		},
		{
			name:  "append by dash",
			doc:   "[\n  1\n]\n",
			ptr:   "/-",
			value: "2",
			want:  "[\n  1,\n  2\n]\n",
		},
		{
			name:  "append to a single-line array",
			doc:   "[1, 2]",
			ptr:   "/-",
			value: "3",
			want:  "[1, 2, 3]",
		},
		{
			name:  "insert before an element",
			doc:   "[\n  1,\n  2\n]\n",
			ptr:   "/1",
			value: "0",
			want:  "[\n  1,\n  0,\n  2\n]\n",
		},
		{
			name:  "insert before an element in a single-line array",
			doc:   "[1, 2]",
			ptr:   "/1",
			value: "0",
			want:  "[1, 0, 2]",
		},
		{
			name:  "insert before a commented element",
			doc:   "[\n  1,\n  // about two\n  2\n]\n",
			ptr:   "/1",
			value: "0",
			want:  "[\n  1,\n  0,\n  // about two\n  2\n]\n",
		},
		{
			name:  "append to an empty array",
			doc:   "[]",
			ptr:   "/-",
			value: "0",
			want:  "[\n  0]",
		},
		{
			name:  "append keeps the trailing comma",
			doc:   "[\n  1,\n]\n",
			ptr:   "/-",
			value: "2",
			want:  "[\n  1,\n  2,\n]\n",
		},
		{
			name:   "index out of range",
			doc:    "[1]",
			ptr:    "/5",
			value:  "0",
			wanErr: "out of range",
		},
		{
			name:   "index not a number",
			doc:    "[1]",
			ptr:    "/x",
			value:  "0",
			wanErr: `invalid array index "x"`,
		},
		{
			name:   "index with leading zero",
			doc:    "[1]",
			ptr:    "/01",
			value:  "0",
			wanErr: `invalid array index "01"`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			d := mustParse(t, tt.doc)
			err := d.Insert(tt.ptr, []byte(tt.value))
			if tt.wanErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wanErr) {
					t.Fatalf("Insert(%q, %q) = %v; want error holding %q", tt.ptr, tt.value, err, tt.wanErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("Insert(%q, %q): %v", tt.ptr, tt.value, err)
			}
			if diff := cmp.Diff(tt.want, string(d.Bytes())); diff != "" {
				t.Errorf("document after insert (-want +got):\n%s", diff)
			}
		})
	}
}

func TestReplace(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		doc    string
		ptr    string
		value  string
		want   string
		wanErr string
	}{
		{
			name:  "scalar becomes an object",
			doc:   "{\n  \"a\": 1,\n  \"b\": 2\n}\n",
			ptr:   "/a",
			value: `{"x": [1]}`,
			want:  "{\n  \"a\": {\"x\": [1]},\n  \"b\": 2\n}\n",
		},
		{
			name:  "object becomes a scalar",
			doc:   "{\n  \"a\": {\n    \"b\": 1\n  },\n  \"c\": 2\n}\n",
			ptr:   "/a",
			value: "true",
			want:  "{\n  \"a\": true,\n  \"c\": 2\n}\n",
		},
		{
			name:  "trailing comment on the value survives",
			doc:   "{ \"mode\": 1 /* mode */ }",
			ptr:   "/mode",
			value: "2",
			want:  "{ \"mode\": 2 /* mode */ }",
		},
		{
			name:  "leading comments around the value survive",
			doc:   "{\n  // about a\n  \"a\": 1\n}\n",
			ptr:   "/a",
			value: "2",
			want:  "{\n  // about a\n  \"a\": 2\n}\n",
		},
		{
			name:  "whole document keeps its leading comment",
			doc:   "// header\n{\"a\": 1}\n",
			ptr:   "",
			value: "[9]",
			want:  "// header\n[9]\n",
		},
		{
			name:   "absent",
			doc:    `{"a": 1}`,
			ptr:    "/z",
			value:  "2",
			wanErr: "not found",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			d := mustParse(t, tt.doc)
			err := d.Replace(tt.ptr, []byte(tt.value))
			if tt.wanErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wanErr) {
					t.Fatalf("Replace(%q, %q) = %v; want error holding %q", tt.ptr, tt.value, err, tt.wanErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("Replace(%q, %q): %v", tt.ptr, tt.value, err)
			}
			if diff := cmp.Diff(tt.want, string(d.Bytes())); diff != "" {
				t.Errorf("document after replace (-want +got):\n%s", diff)
			}
		})
	}
}

func TestRemove(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		doc    string
		ptr    string
		want   string
		wanErr string
	}{
		{
			name: "middle member",
			doc:  "{\n  \"a\": 1,\n  \"b\": 2,\n  \"c\": 3\n}\n",
			ptr:  "/b",
			want: "{\n  \"a\": 1,\n  \"c\": 3\n}\n",
		},
		{
			name: "last member without trailing comma",
			doc:  "{\n  \"a\": 1,\n  \"b\": 2\n}\n",
			ptr:  "/b",
			want: "{\n  \"a\": 1\n}\n",
		},
		{
			name: "last member keeps the trailing comma",
			doc:  "{\n  \"a\": 1,\n  \"b\": 2,\n}\n",
			ptr:  "/b",
			want: "{\n  \"a\": 1,\n}\n",
		},
		{
			name: "only member",
			doc:  `{"a": 1}`,
			ptr:  "/a",
			want: "{}",
		},
		{
			name: "only member of a multi-line object",
			doc:  "{\n  \"a\": 1\n}\n",
			ptr:  "/a",
			want: "{\n}\n",
		},
		{
			name: "leading comment dies with the member",
			doc:  "{\n  // about b\n  \"b\": 2,\n  \"c\": 3\n}\n",
			ptr:  "/b",
			want: "{\n  \"c\": 3\n}\n",
		},
		{
			name: "comment before the removed member dies with it",
			doc:  "{\n  \"a\": 1, // about a\n  \"b\": 2\n}\n",
			ptr:  "/b",
			want: "{\n  \"a\": 1\n}\n",
		},
		{
			name: "middle array element",
			doc:  "[1, 2, 3]",
			ptr:  "/1",
			want: "[1, 3]",
		},
		{
			name: "last array element keeps the trailing comma",
			doc:  "[\n  1,\n  2,\n]\n",
			ptr:  "/1",
			want: "[\n  1,\n]\n",
		},
		{
			name: "only element of a nested array",
			doc:  `{"x": [1]}`,
			ptr:  "/x/0",
			want: `{"x": []}`,
		},
		{
			name:   "absent member",
			doc:    `{"a": 1}`,
			ptr:    "/z",
			wanErr: "not found",
		},
		{
			name:   "root",
			doc:    `{"a": 1}`,
			ptr:    "",
			wanErr: "root cannot be removed",
		},
		{
			name:   "array append token",
			doc:    "[1]",
			ptr:    "/-",
			wanErr: `invalid array index "-"`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			d := mustParse(t, tt.doc)
			err := d.Remove(tt.ptr)
			if tt.wanErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wanErr) {
					t.Fatalf("Remove(%q) = %v; want error holding %q", tt.ptr, err, tt.wanErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("Remove(%q): %v", tt.ptr, err)
			}
			if diff := cmp.Diff(tt.want, string(d.Bytes())); diff != "" {
				t.Errorf("document after remove (-want +got):\n%s", diff)
			}
		})
	}
}

// TestSequence walks a planner-shaped chain of edits over one realistic file:
// every untouched line must survive byte for byte while the owned values move.
func TestSequence(t *testing.T) {
	t.Parallel()
	doc := "// opencode configuration.\n" +
		"{\n" +
		"\t\"theme\": \"dark\", // chosen in the UI\n" +
		"\t\"autoupdate\": true,\n" +
		"\t\"keybinds\": {\n" +
		"\t\t\"leader\": \"<ctrl>x\",\n" +
		"\t},\n" +
		"}\n"
	want := "// opencode configuration.\n" +
		"{\n" +
		"\t\"theme\": \"dark\", // chosen in the UI\n" +
		"\t\"autoupdate\": false,\n" +
		"\t\"plugins\": [\"penates\"],\n" +
		"}\n"

	d := mustParse(t, doc)
	steps := []struct {
		name string
		run  func() error
	}{
		{"insert plugins", func() error { return d.Insert("/plugins", []byte(`["penates"]`)) }},
		{"replace autoupdate", func() error { return d.Replace("/autoupdate", []byte("false")) }},
		{"remove keybinds", func() error { return d.Remove("/keybinds") }},
	}
	for _, st := range steps {
		if err := st.run(); err != nil {
			t.Fatalf("%s: %v", st.name, err)
		}
	}
	if diff := cmp.Diff(want, string(d.Bytes())); diff != "" {
		t.Errorf("document after the sequence (-want +got):\n%s", diff)
	}

	std, found, err := d.Get("")
	if err != nil || !found {
		t.Fatalf("Get root: %q, %v, %v", std, found, err)
	}
	if !json.Valid(std) {
		t.Errorf("standardized document is not valid JSON: %s", std)
	}
	if want := `{"theme":"dark","autoupdate":false,"plugins":["penates"]}`; string(std) != want {
		t.Errorf("standardized document = %s, want %s", std, want)
	}
	for _, comment := range []string{"// opencode configuration.", "// chosen in the UI"} {
		if !strings.Contains(string(d.Bytes()), comment) {
			t.Errorf("comment %q did not survive", comment)
		}
	}
}
