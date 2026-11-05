package jsonc

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"

	"github.com/tailscale/hujson"
)

// errNotFound marks a pointer that resolves to nothing: an absent object
// member or an out-of-range array index.
var errNotFound = errors.New("value not found")

// Doc is a parsed JSONC document open for surgical edits. Every operation
// mutates the parsed tree in place; untouched parts of the tree keep their
// exact source bytes, so Bytes reproduces the input until an edit touches it.
type Doc struct {
	root hujson.Value
}

// Parse reads a JSONC document — JSON with comments and trailing commas
// (JWCC). Syntax errors carry the position the parser reports.
func Parse(data []byte) (*Doc, error) {
	root, err := hujson.Parse(data)
	if err != nil {
		return nil, fmt.Errorf("parse: %w", err)
	}
	return &Doc{root: root}, nil
}

// Bytes serializes the document. The output is valid JWCC and, for the parts
// no edit touched, byte-identical to the input.
func (d *Doc) Bytes() []byte {
	return d.root.Pack()
}

// lookup resolves the pointer and reports whether it finds anything. op names
// the calling operation for error messages; anything other than not-found —
// a scalar in the middle of the path, a malformed pointer — is an error.
func (d *Doc) lookup(op, pointer string) (*hujson.Value, bool, error) {
	frags, err := splitPointer(pointer)
	if err != nil {
		return nil, false, fmt.Errorf("%s %q: %w", op, pointer, err)
	}
	v, err := d.resolve(frags)
	if err != nil {
		if errors.Is(err, errNotFound) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("%s %q: %w", op, pointer, err)
	}
	return v, true, nil
}

// Get returns the value at the pointer as minimal JSON: whitespace, comments,
// and trailing commas inside it are stripped, so the result is fit for byte
// comparison against other values returned by Get. found is false when the
// pointer resolves to nothing — an absent member, an out-of-range array
// index, or the "-" append token; malformed pointers are an error. If an
// object holds duplicate names, the first member wins.
func (d *Doc) Get(pointer string) (json.RawMessage, bool, error) {
	v, found, err := d.lookup("get", pointer)
	if err != nil || !found {
		return nil, found, err
	}
	min := v.Clone()
	min.Minimize()
	return json.RawMessage(min.Pack()), true, nil
}

// Raw returns the value at the pointer as its source spells it, comments
// inside it included; the whitespace and comments that surround the value —
// before it and after it — are not part of the result. found is false when
// the pointer resolves to nothing; malformed pointers are an error.
func (d *Doc) Raw(pointer string) ([]byte, bool, error) {
	v, found, err := d.lookup("raw", pointer)
	if err != nil || !found {
		return nil, found, err
	}
	sub := v.Clone()
	sub.BeforeExtra = nil
	sub.AfterExtra = nil
	return sub.Pack(), true, nil
}

// Members lists the names of the object at the pointer, in source order and
// unescaped. found is false when the pointer resolves to nothing; an error
// when it resolves to a value that is not an object.
func (d *Doc) Members(pointer string) ([]string, bool, error) {
	v, found, err := d.lookup("members", pointer)
	if err != nil || !found {
		return nil, found, err
	}
	obj, ok := v.Value.(*hujson.Object)
	if !ok {
		return nil, true, fmt.Errorf("members %q: not an object", pointer)
	}
	var names []string
	for _, m := range obj.Members {
		lit, ok := m.Name.Value.(hujson.Literal)
		if !ok {
			continue
		}
		var name string
		if json.Unmarshal(lit, &name) != nil {
			continue
		}
		names = append(names, name)
	}
	return names, true, nil
}

// Elements lists the elements of the array at the pointer as minimal JSON, in
// source order. found is false when the pointer resolves to nothing; an error
// when it resolves to a value that is not an array.
func (d *Doc) Elements(pointer string) ([][]byte, bool, error) {
	v, found, err := d.lookup("elements", pointer)
	if err != nil || !found {
		return nil, found, err
	}
	arr, ok := v.Value.(*hujson.Array)
	if !ok {
		return nil, true, fmt.Errorf("elements %q: not an array", pointer)
	}
	var out [][]byte
	for i := range arr.Elements {
		min := arr.Elements[i].Clone()
		min.Minimize()
		out = append(out, min.Pack())
	}
	return out, true, nil
}

// Insert places value at the pointer, which must not resolve to anything yet.
// An object member is appended last; an array element is appended when the
// index equals the length or is "-", and inserted before the element
// otherwise. The value is a JSON(C) document holding a single value: the
// whitespace and comments around it are dropped, the value itself — comments
// inside it included — is spliced in as written. The new member or element is
// laid out like its neighbor: same indentation on its own line in a
// multi-line composite, one space after the comma in a single-line one; a
// member or element added to an empty composite gets a new line and two
// spaces.
func (d *Doc) Insert(pointer string, value []byte) error {
	if pointer == "" {
		return fmt.Errorf("insert %q: the root is already occupied", pointer)
	}
	frags, err := splitPointer(pointer)
	if err != nil {
		return fmt.Errorf("insert %q: %w", pointer, err)
	}
	parent, err := d.resolve(frags[:len(frags)-1])
	if err != nil {
		return fmt.Errorf("insert %q: %w", pointer, err)
	}
	frag, err := parseValue(value)
	if err != nil {
		return fmt.Errorf("insert %q: %w", pointer, err)
	}
	last := frags[len(frags)-1]
	switch comp := parent.Value.(type) {
	case *hujson.Object:
		if _, ok := memberIndex(comp, last); ok {
			return fmt.Errorf("insert %q: member %q already exists", pointer, last)
		}
		i := len(comp.Members)
		nameLead, valueLead := []byte("\n  "), []byte(" ")
		if i > 0 {
			nameLead = separator(comp.Members[i-1].Name.BeforeExtra, false)
			valueLead = spaceLike(comp.Members[i-1].Value.BeforeExtra)
		}
		value := hujson.Value{
			BeforeExtra: valueLead,
			Value:       frag,
		}
		if i > 0 && comp.Members[i-1].Value.AfterExtra != nil {
			// The object ended with a trailing comma; the comma stays
			// trailing, so the marker moves onto the new last member.
			value.AfterExtra = []byte{}
		}
		comp.Members = append(comp.Members, hujson.ObjectMember{
			Name: hujson.Value{
				BeforeExtra: nameLead,
				Value:       hujson.String(last),
			},
			Value: value,
		})
	case *hujson.Array:
		idx, err := insertIndex(len(comp.Elements), last)
		if err != nil {
			return fmt.Errorf("insert %q: %w", pointer, err)
		}
		var lead []byte
		first := idx == len(comp.Elements)
		switch {
		case first && idx > 0:
			lead = comp.Elements[idx-1].BeforeExtra
		case !first:
			lead = comp.Elements[idx].BeforeExtra
		}
		elem := hujson.ArrayElement{
			BeforeExtra: separator(lead, first && idx == 0),
			Value:       frag,
		}
		if first && idx > 0 && comp.Elements[idx-1].AfterExtra != nil {
			// The array ended with a trailing comma; the comma stays
			// trailing, so the marker moves onto the new last element.
			elem.AfterExtra = []byte{}
		}
		if first {
			comp.Elements = append(comp.Elements, elem)
		} else {
			comp.Elements = slices.Insert(comp.Elements, idx, elem)
		}
	default:
		return fmt.Errorf("insert %q: cannot add into a scalar", pointer)
	}
	return nil
}

// Replace overwrites the value at the pointer with value, a JSON(C) document
// holding a single value. The whitespace and comments that surrounded the old
// value — its line, indentation, and trailing comment — stay where they were;
// comments around the new value are dropped, comments inside it are kept.
func (d *Doc) Replace(pointer string, value []byte) error {
	frags, err := splitPointer(pointer)
	if err != nil {
		return fmt.Errorf("replace %q: %w", pointer, err)
	}
	v, err := d.resolve(frags)
	if err != nil {
		return fmt.Errorf("replace %q: %w", pointer, err)
	}
	frag, err := parseValue(value)
	if err != nil {
		return fmt.Errorf("replace %q: %w", pointer, err)
	}
	v.Value = frag
	return nil
}

// Remove deletes the member or element at the pointer. The whitespace that
// separated it moves onto the composite's closing brace or bracket: removing
// a member leaves its line layout exactly as it was. A trailing comma is
// retained when the file had one and members remain. A comment between the
// removed member and its neighbor counts as the neighbor's leading comment
// and is removed together with it.
func (d *Doc) Remove(pointer string) error {
	if pointer == "" {
		return fmt.Errorf("remove %q: the root cannot be removed", pointer)
	}
	frags, err := splitPointer(pointer)
	if err != nil {
		return fmt.Errorf("remove %q: %w", pointer, err)
	}
	parent, err := d.resolve(frags[:len(frags)-1])
	if err != nil {
		return fmt.Errorf("remove %q: %w", pointer, err)
	}
	last := frags[len(frags)-1]
	switch comp := parent.Value.(type) {
	case *hujson.Object:
		i, ok := memberIndex(comp, last)
		if !ok {
			return fmt.Errorf("remove %q: %w", pointer, errNotFound)
		}
		hadComma := i == len(comp.Members)-1 && comp.Members[i].Value.AfterExtra != nil
		comp.Members = slices.Delete(comp.Members, i, i+1)
		if len(comp.Members) > 0 {
			retainTrailingComma(&comp.Members[len(comp.Members)-1].Value, hadComma)
		}
	case *hujson.Array:
		idx, err := parseIndex(last)
		if err != nil {
			return fmt.Errorf("remove %q: %w", pointer, err)
		}
		if idx >= len(comp.Elements) {
			return fmt.Errorf("remove %q: %w", pointer, errNotFound)
		}
		hadComma := idx == len(comp.Elements)-1 && comp.Elements[idx].AfterExtra != nil
		comp.Elements = slices.Delete(comp.Elements, idx, idx+1)
		if len(comp.Elements) > 0 {
			retainTrailingComma(&comp.Elements[len(comp.Elements)-1], hadComma)
		}
	default:
		return fmt.Errorf("remove %q: cannot remove from a scalar", pointer)
	}
	return nil
}

// retainTrailingComma keeps the composite's trailing comma after its last
// member or element was removed: the parser marks a trailing comma as a
// non-nil AfterExtra on the last member, so the mark moves onto the member
// that is last now. Nothing to mark when the composite never had the comma.
func retainTrailingComma(v *hujson.Value, hadComma bool) {
	if !hadComma {
		return
	}
	if v.AfterExtra == nil {
		v.AfterExtra = []byte{}
	}
}

// resolve walks the pointer fragments from the root and returns the value
// they end at. A fragment that names nothing yields errNotFound; a fragment
// that cannot be evaluated — a scalar in the middle of the path, a malformed
// array index — yields a plain error.
func (d *Doc) resolve(frags []string) (*hujson.Value, error) {
	cur := &d.root
	for _, frag := range frags {
		switch comp := cur.Value.(type) {
		case *hujson.Object:
			i, ok := memberIndex(comp, frag)
			if !ok {
				return nil, errNotFound
			}
			cur = &comp.Members[i].Value
		case *hujson.Array:
			if frag == "-" {
				return nil, errNotFound
			}
			i, err := parseIndex(frag)
			if err != nil {
				return nil, err
			}
			if i >= len(comp.Elements) {
				return nil, errNotFound
			}
			cur = &comp.Elements[i]
		default:
			return nil, errors.New("cannot index into a scalar")
		}
	}
	return cur, nil
}

// EscapeToken returns the RFC 6901 encoding of one reference token: "~"
// becomes "~0" and "/" becomes "~1". Pointers built from unescaped tokens with
// it are read back by every Doc operation.
func EscapeToken(token string) string {
	token = strings.ReplaceAll(token, "~", "~0")
	return strings.ReplaceAll(token, "/", "~1")
}

// splitPointer splits an RFC 6901 pointer into its unescaped reference tokens.
// The empty pointer splits into no tokens and denotes the value itself.
func splitPointer(pointer string) ([]string, error) {
	if pointer == "" {
		return nil, nil
	}
	if pointer[0] != '/' {
		return nil, errors.New("must be empty or begin with \"/\"")
	}
	toks := strings.Split(pointer[1:], "/")
	for i, tok := range toks {
		if !strings.Contains(tok, "~") {
			continue
		}
		for j := 0; j < len(tok); j++ {
			if tok[j] != '~' {
				continue
			}
			if j+1 == len(tok) || (tok[j+1] != '0' && tok[j+1] != '1') {
				bad := "~"
				if j+1 < len(tok) {
					bad = tok[j : j+2]
				}
				return nil, fmt.Errorf("invalid escape %q", bad)
			}
		}
		tok = strings.ReplaceAll(tok, "~1", "/")
		tok = strings.ReplaceAll(tok, "~0", "~")
		toks[i] = tok
	}
	return toks, nil
}

// memberIndex finds the first member of obj whose name equals name; object
// names are matched by their exact string value, whatever escaping the source
// used.
func memberIndex(obj *hujson.Object, name string) (int, bool) {
	for i, m := range obj.Members {
		lit, ok := m.Name.Value.(hujson.Literal)
		if ok && equalName(lit, name) {
			return i, true
		}
	}
	return 0, false
}

// equalName reports whether the literal spells the name, unescaping it when
// the source used escape sequences.
func equalName(lit hujson.Literal, name string) bool {
	if len(lit) >= 2 && lit[0] == '"' && lit[len(lit)-1] == '"' && !bytes.ContainsRune(lit, '\\') {
		return string(lit[1:len(lit)-1]) == name
	}
	var s string
	return json.Unmarshal(lit, &s) == nil && s == name
}

// insertIndex resolves the last pointer fragment against an array of the
// given length: "-" or the length itself appends, a smaller index inserts
// before that element.
func insertIndex(length int, tok string) (int, error) {
	if tok == "-" {
		return length, nil
	}
	i, err := parseIndex(tok)
	if err != nil {
		return 0, err
	}
	if i > length {
		return 0, fmt.Errorf("index %s is out of range", tok)
	}
	return i, nil
}

// parseIndex parses an RFC 6901 array-index token: digits with no leading
// zero.
func parseIndex(tok string) (int, error) {
	if len(tok) > 1 && tok[0] == '0' {
		return 0, fmt.Errorf("invalid array index %q", tok)
	}
	u, err := strconv.ParseUint(tok, 10, 64)
	if err != nil || u > uint64(math.MaxInt) {
		return 0, fmt.Errorf("invalid array index %q", tok)
	}
	return int(u), nil
}

// separator returns the whitespace that precedes a member or element next to
// the one whose leading whitespace is lead. A composite written across lines
// takes the neighbor on a new line at the same indentation; a single-line
// composite takes one space after the comma. An empty composite has no
// neighbor: the default is a new line and two spaces.
func separator(lead []byte, empty bool) []byte {
	if empty {
		return []byte("\n  ")
	}
	if i := bytes.LastIndexByte(lead, '\n'); i >= 0 {
		if tail := lead[i+1:]; isSpace(tail) {
			return append([]byte{'\n'}, tail...)
		}
	} else if isSpace(lead) && len(lead) > 0 {
		return lead
	}
	return []byte(" ")
}

// spaceLike returns the whitespace to place between the colon and an inserted
// member's value: whatever the preceding member used, when that is plain
// whitespace, or one space.
func spaceLike(lead []byte) []byte {
	if isSpace(lead) {
		return lead
	}
	return []byte(" ")
}

// isSpace reports whether b is empty or entirely whitespace.
func isSpace(b []byte) bool {
	for _, c := range b {
		switch c {
		case ' ', '\t', '\r', '\n':
		default:
			return false
		}
	}
	return true
}

// parseValue parses a JSON(C) document holding a single value and returns the
// value without its surrounding whitespace and comments.
func parseValue(data []byte) (hujson.ValueTrimmed, error) {
	v, err := hujson.Parse(data)
	if err != nil {
		return nil, fmt.Errorf("parse value: %w", err)
	}
	return v.Value, nil
}
