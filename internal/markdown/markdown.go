// Package markdown works with managed blocks in shared Markdown files: blocks
// delimited by penates markers, headings outside them, and the append and replace
// operations section contributions are planned with. It is pure text manipulation
// and knows no file system.
package markdown

import (
	"regexp"
	"strings"
)

var (
	beginRe   = regexp.MustCompile(`^\s*<!--\s*penates:begin\s+(\S+)\s*-->\s*$`)
	endRe     = regexp.MustCompile(`^\s*<!--\s*penates:end\s+(\S+)\s*-->\s*$`)
	headingRe = regexp.MustCompile(`^\s{0,3}#{1,6}\s+(.+?)\s*#*\s*$`)
)

// Block is one managed block: the id named by the markers and the content between
// them, without the surrounding newlines.
type Block struct {
	ID      string
	Content string
}

// Layout is the parsed structure of a shared Markdown file.
type Layout struct {
	// Blocks are the managed blocks, in file order.
	Blocks []Block
	// Headings are the lowercased texts of ATX headings outside managed blocks.
	Headings []string
}

// Parse splits a shared file into managed blocks and outside headings. A begin
// marker without its end is user content: no block is managed from it, and its
// lines take part in the heading scan like any other user line.
func Parse(s string) Layout {
	var lay Layout
	lines := strings.Split(s, "\n")
	for i := 0; i < len(lines); i++ {
		if m := beginRe.FindStringSubmatch(lines[i]); m != nil {
			var content []string
			j := i + 1
			for ; j < len(lines); j++ {
				if e := endRe.FindStringSubmatch(lines[j]); e != nil && e[1] == m[1] {
					break
				}
				content = append(content, lines[j])
			}
			if j < len(lines) {
				lay.Blocks = append(lay.Blocks, Block{ID: m[1], Content: strings.Join(content, "\n")})
				i = j
				continue
			}
		}
		if h := headingRe.FindStringSubmatch(lines[i]); h != nil {
			lay.Headings = append(lay.Headings, strings.ToLower(h[1]))
		}
	}
	return lay
}

// BlockText renders one managed block.
func BlockText(id, content string) string {
	return "<!-- penates:begin " + id + " -->\n" + content + "\n<!-- penates:end " + id + " -->"
}

// Append adds a managed block at the end of s, separated by a blank line. A file
// with nothing but whitespace takes the block alone. The result always ends with
// a newline after the end marker.
func Append(s, id, content string) string {
	block := BlockText(id, content)
	if strings.TrimSpace(s) == "" {
		return block + "\n"
	}
	return strings.TrimRight(s, "\n") + "\n\n" + block + "\n"
}

// Replace swaps the content of the managed block id in s, keeping everything
// outside the markers byte for byte. It reports whether the block was found.
func Replace(s, id, content string) (string, bool) {
	lines := strings.Split(s, "\n")
	for i := 0; i < len(lines); i++ {
		if m := beginRe.FindStringSubmatch(lines[i]); m != nil && m[1] == id {
			for j := i + 1; j < len(lines); j++ {
				if e := endRe.FindStringSubmatch(lines[j]); e != nil && e[1] == id {
					out := make([]string, 0, len(lines))
					out = append(out, lines[:i+1]...)
					if content != "" {
						out = append(out, strings.Split(content, "\n")...)
					}
					out = append(out, lines[j:]...)
					return strings.Join(out, "\n"), true
				}
			}
			return s, false
		}
	}
	return s, false
}

// Remove deletes the managed block id together with its markers, keeping
// everything else byte for byte. It reports whether the block was found.
func Remove(s, id string) (string, bool) {
	lines := strings.Split(s, "\n")
	for i := 0; i < len(lines); i++ {
		if m := beginRe.FindStringSubmatch(lines[i]); m != nil && m[1] == id {
			for j := i + 1; j < len(lines); j++ {
				if e := endRe.FindStringSubmatch(lines[j]); e != nil && e[1] == id {
					out := make([]string, 0, len(lines))
					out = append(out, lines[:i]...)
					// A blank line kept the block separated from the text
					// before it; it goes with the block.
					if i > 0 && strings.TrimSpace(lines[i-1]) == "" {
						out = out[:len(out)-1]
					}
					out = append(out, lines[j+1:]...)
					return strings.Join(out, "\n"), true
				}
			}
			return s, false
		}
	}
	return s, false
}

// Strip deletes the marker lines of the managed block id and keeps the
// content between them, for a block the user modified: the content stays as
// ordinary text of the file (RFC 9.1). It reports whether the block was
// found.
func Strip(s, id string) (string, bool) {
	lines := strings.Split(s, "\n")
	for i := 0; i < len(lines); i++ {
		if m := beginRe.FindStringSubmatch(lines[i]); m != nil && m[1] == id {
			for j := i + 1; j < len(lines); j++ {
				if e := endRe.FindStringSubmatch(lines[j]); e != nil && e[1] == id {
					out := make([]string, 0, len(lines))
					out = append(out, lines[:i]...)
					out = append(out, lines[i+1:j]...)
					out = append(out, lines[j+1:]...)
					return strings.Join(out, "\n"), true
				}
			}
			return s, false
		}
	}
	return s, false
}

// FirstHeading returns the lowercased text of the first ATX heading of a
// fragment; empty when the fragment has none.
func FirstHeading(fragment string) string {
	for _, line := range strings.Split(fragment, "\n") {
		if h := headingRe.FindStringSubmatch(line); h != nil {
			return strings.ToLower(h[1])
		}
	}
	return ""
}
