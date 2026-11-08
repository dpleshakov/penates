//go:build ignore

// Command check-builtin-kit validates the kit embedded into the binary: the
// directory must load through the kit reader with no problems — the reader
// already reports invalid item versions among them. It reads the source
// directory of internal/builtin/kit, the same files `go:embed` ships, so a
// broken kit fails `make check` before it fails a user's startup.
//
// Usage:
//
//	go run tools/check-builtin-kit.go
package main

import (
	"fmt"
	"os"

	"github.com/dpleshakov/penates/internal/kit"
)

func main() {
	if err := run("internal/builtin/kit"); err != nil {
		fmt.Fprintln(os.Stderr, "check-builtin-kit:", err)
		os.Exit(1)
	}
	fmt.Println("check-builtin-kit: the embedded kit loads clean")
}

func run(dir string) error {
	k, problems, err := kit.Read(dir)
	if err != nil {
		return err
	}
	for _, p := range problems {
		fmt.Fprintln(os.Stderr, "check-builtin-kit:", p)
	}
	if len(problems) > 0 {
		return fmt.Errorf("%d problem(s) in %s", len(problems), dir)
	}
	if k == nil || len(k.Items) == 0 {
		return fmt.Errorf("%s holds no items", dir)
	}
	return nil
}
