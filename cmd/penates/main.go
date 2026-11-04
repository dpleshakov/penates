// Command penates installs kit items into agent harness configuration directories.
//
// The product design is specified in docs/rfc-penates.md; the modules and their rules
// in docs/architecture.md.
package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintln(os.Stderr, "penates: not yet functional; see docs/rfc-penates.md")
	os.Exit(1)
}
