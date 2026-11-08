// Package builtin ships the penates documentation kit inside the binary and
// keeps the unpacked copy in the penates data directory in step with the
// embedded content. The kit itself is ordinary kit data: the package knows
// no harness — the target hint and the install destinations live in the
// kit's own files.
package builtin

import (
	"embed"
	"io/fs"
)

// embedded holds the kit under kit/.
var embedded embed.FS

// Content is the built-in kit as a filesystem rooted at the kit directory.
var Content fs.FS = mustSub(embedded, "kit")

// mustSub subs dir out of f. The embedded tree is fixed at build time, so
// the failure it guards against cannot happen in a correct build; a panic
// here is a build error, not a runtime condition.
func mustSub(f embed.FS, dir string) fs.FS {
	sub, err := fs.Sub(f, dir)
	if err != nil {
		panic(err)
	}
	return sub
}
