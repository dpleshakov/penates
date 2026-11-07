package tui

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/dpleshakov/penates/internal/kit"
	"github.com/dpleshakov/penates/internal/plan"
)

// The prose the user sees for the planner's structured verdicts: conflicts
// render one line per cause, exclusions list their conflicts first. The
// screens, the tree reasons, and the summary all speak these lines.

// exclusionLines renders an excluded item's causes: the structured
// conflicts first, then the plain reasons.
func exclusionLines(ip *plan.ItemPlan) []string {
	lines := make([]string, 0, len(ip.Conflicts)+len(ip.Reasons))
	for _, c := range ip.Conflicts {
		lines = append(lines, renderConflict(c))
	}
	return append(lines, ip.Reasons...)
}

// renderConflict renders one structured planner conflict as the prose the
// user sees: every contribution kind and cause has its own line here.
func renderConflict(c plan.Conflict) string {
	rivals := strings.Join(c.Rivals, ", ")
	switch c.Cause {
	case plan.CauseClaimed:
		if c.Kind == kit.KindJSON {
			return fmt.Sprintf("%s is claimed at %s with a different value by %s", c.File, quotePointer(c.Pointer), rivals)
		}
		return fmt.Sprintf("%s is also contributed by %s", c.File, rivals)
	case plan.CauseClaimedAsFile:
		return fmt.Sprintf("%s is contributed as a file by %s", c.File, rivals)
	case plan.CauseClaimedAsJSON:
		return fmt.Sprintf("%s is also contributed as json by %s; one shared file is either Markdown or JSON", c.File, rivals)
	case plan.CauseClaimedAsFileOrSection:
		return fmt.Sprintf("%s is also contributed as a file or section by %s; one shared file is either Markdown or JSON", c.File, rivals)
	case plan.CauseUserContent:
		if c.Heading != "" {
			return fmt.Sprintf("%s already has a heading %q outside managed blocks; the section would duplicate it", c.File, c.Heading)
		}
		if c.Block != "" {
			return fmt.Sprintf("%s holds block %s that is not recorded in the lock; refusing to touch content that may belong to the user", c.File, c.Block)
		}
		return fmt.Sprintf("%s exists in the target and is not recorded in the lock; refusing to touch a file that may belong to the user", c.File)
	case plan.CauseOwned:
		switch {
		case c.Kind == kit.KindFile:
			return fmt.Sprintf("%s is owned by installed item %s", c.File, c.Owner)
		case c.Block != "":
			return fmt.Sprintf("block %s in %s is owned by installed item %s", c.Block, c.File, c.Owner)
		default:
			return fmt.Sprintf("%s is owned as a whole file by installed item %s", c.File, c.Owner)
		}
	case plan.CauseModified:
		switch c.Kind {
		case kit.KindJSON:
			return fmt.Sprintf("%s holds a value at %s that was modified after installation, and the kit provides different content", c.File, quotePointer(c.Pointer))
		case kit.KindSection:
			return fmt.Sprintf("block %s in %s was modified after installation, and the kit provides different content", c.Block, c.File)
		default:
			return fmt.Sprintf("%s was modified after installation, and the kit provides different content", c.File)
		}
	case plan.CauseDifferentValue:
		return fmt.Sprintf("%s holds a different value at %s", c.File, quotePointer(c.Pointer))
	case plan.CauseTwoSections:
		return fmt.Sprintf("two section contributions write %s; one item owns one block per shared file", c.File)
	}
	return ""
}

// quotePointer names a JSON location for a conflict line: the root by
// name, any other pointer quoted.
func quotePointer(ptr string) string {
	if ptr == "" {
		return "the root"
	}
	return strconv.Quote(ptr)
}
