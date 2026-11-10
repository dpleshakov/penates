// Package relpath holds the one validity rule for the relative paths of the
// domain: a valid path is non-empty and consists of plain elements separated by
// forward slashes — no absolute paths, no "." or ".." elements, no backslashes,
// no drive letters. Kit-declared contribution paths and paths inside the target
// obey the same rule; conversion to platform paths happens only at the
// file-system boundary. The package is pure and knows no file system.
package relpath

import "strings"

// Valid reports whether p obeys the path rule the package defines.
func Valid(p string) bool {
	if p == "" || strings.ContainsAny(p, "\\:") {
		return false
	}
	for _, el := range strings.Split(p, "/") {
		if el == "" || el == "." || el == ".." {
			return false
		}
	}
	return true
}
