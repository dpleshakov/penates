// Package kit reads kit directories — kit.yaml, items, item.yaml, and contribution
// declarations — and validates everything the product concept (docs/concept.md)
// requires of kit data: id syntax, version, source and target path rules, and
// shared-file targets. Validation errors carry file positions.
package kit

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/goccy/go-yaml"
	"golang.org/x/mod/semver"
)

// idSyntax is the identifier syntax prescribed for items; kit names follow
// the same rule so that an item key is a pair of tokens.
var idSyntax = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

// Kit is one kit directory bound to one harness: the source of truth for its items.
type Kit struct {
	// Name is the kit name from kit.yaml, unique among registered kits.
	Name string
	// Description is the free-form summary from kit.yaml.
	Description string
	// TargetHint suggests a target directory on first registration.
	TargetHint string
	// Shared lists the target files that section and json contributions may write to.
	Shared []string
	// Dir is the absolute path of the kit directory.
	Dir string
	// Items are the kit's items, sorted by id.
	Items []*Item
}

// Item is one installable unit: everything a single capability needs.
type Item struct {
	// Dir is the absolute path of the item directory.
	Dir string
	// ID is the item slug, unique within the kit.
	ID string
	// Version is the semantic version declared in item.yaml.
	Version string
	// Description is the free-form summary from item.yaml.
	Description string
	// Tags are free-form labels used for display and filtering only.
	Tags []string
	// Vars are the per-item parameters requested at installation.
	Vars []Var
	// Contributes are the item's declared effects on the target.
	Contributes []Contribution
	// HasChangelog reports whether the item directory holds a CHANGELOG.md.
	HasChangelog bool
}

// Var is one per-item parameter whose value is substituted into templated
// contributions.
type Var struct {
	ID       string
	Prompt   string
	Required bool
}

// ContributionKind distinguishes the three effects an item may have on the target.
type ContributionKind string

const (
	// KindFile contributes a whole file or directory tree.
	KindFile ContributionKind = "file"
	// KindSection contributes a marked Markdown block into a shared file.
	KindSection ContributionKind = "section"
	// KindJSON contributes values into a shared JSON or JSONC file.
	KindJSON ContributionKind = "json"
)

// Contribution is one declared effect of an item on the target. Directory file
// sources are expanded at load time, so a file contribution is always one plain
// file.
type Contribution struct {
	// Kind selects the contribution kind.
	Kind ContributionKind
	// Src is the source path relative to the item directory, with forward slashes.
	Src string
	// To is the target path relative to the target root, with forward slashes.
	To string
	// Template opts the contribution into variable substitution.
	Template bool
}

// SourcePath returns the absolute path of the source file of one contribution.
// It is the key contributions' contents are passed to the planner under.
func (it *Item) SourcePath(c Contribution) string {
	return filepath.Join(it.Dir, filepath.FromSlash(c.Src))
}

// ItemKey returns the identity of an item: the kit name and the item id. Items with
// the same id in different kits are distinct items.
func ItemKey(kitName, itemID string) string {
	return kitName + "/" + itemID
}

// ValidationError describes one rejected field of a kit file.
type ValidationError struct {
	// File locates the error: the path of the kit file, relative to the kit root.
	File string
	// Field names the field, for example "contributes[0].to"; empty for file-level
	// errors such as a missing file.
	Field string
	// Line is the line the YAML parser named for the problem, 0 when it
	// named none.
	Line int
	// Msg says what is wrong.
	Msg string
}

func (e *ValidationError) Error() string {
	switch {
	case e.Line > 0:
		return fmt.Sprintf("%s:%d  %s", e.File, e.Line, e.Msg)
	case e.Field == "":
		return fmt.Sprintf("%s: %s", e.File, e.Msg)
	}
	return fmt.Sprintf("%s: %s: %s", e.File, e.Field, e.Msg)
}

// kitFile and itemFile mirror the YAML formats of kit.yaml and item.yaml.
type kitFile struct {
	Name        string   `yaml:"name"`
	Description string   `yaml:"description"`
	TargetHint  string   `yaml:"target_hint"`
	Shared      []string `yaml:"shared"`
}

type itemFile struct {
	ID          string             `yaml:"id"`
	Version     string             `yaml:"version"`
	Description string             `yaml:"description"`
	Tags        []string           `yaml:"tags"`
	Vars        []varFile          `yaml:"vars"`
	Contributes []contributionFile `yaml:"contributes"`
}

type varFile struct {
	ID       string `yaml:"id"`
	Prompt   string `yaml:"prompt"`
	Required bool   `yaml:"required"`
}

type contributionFile struct {
	File     *string `yaml:"file"`
	Section  *string `yaml:"section"`
	JSON     *string `yaml:"json"`
	To       string  `yaml:"to"`
	Template bool    `yaml:"template"`
}

// Load reads and validates the kit in dir. It either returns a fully valid kit or an
// error naming every problem it found.
func Load(dir string) (*Kit, error) {
	k, problems, err := Read(dir)
	if err != nil {
		if len(problems) == 0 {
			return nil, err
		}
		errs := make([]error, 0, len(problems)+1)
		errs = append(errs, err)
		errs = append(errs, problems...)
		return nil, errors.Join(errs...)
	}
	if len(problems) > 0 {
		return nil, errors.Join(problems...)
	}
	return k, nil
}

// Read reads the kit in dir tolerantly: kit.yaml and the items directory
// must read and validate — their problems are the returned error — while
// a broken item is only reported beside the kit and does not fail it. The
// returned kit holds the items that loaded; the interface shows the
// problems on the affected item rows and still lets the kit attach.
func Read(dir string) (*Kit, []error, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, nil, err
	}

	var errs []error
	fail := func(file, field, format string, args ...any) {
		errs = append(errs, &ValidationError{File: file, Field: field, Msg: fmt.Sprintf(format, args...)})
	}

	var kf kitFile
	if err := decodeFile(filepath.Join(abs, "kit.yaml"), "kit.yaml", &kf); err != nil {
		return nil, nil, err
	}
	if kf.Name == "" {
		fail("kit.yaml", "name", "is required")
	} else if !idSyntax.MatchString(kf.Name) {
		fail("kit.yaml", "name", "must match %s", idSyntax)
	}
	for i, s := range kf.Shared {
		if !validRelPath(s) {
			fail("kit.yaml", fmt.Sprintf("shared[%d]", i), "%q is not a relative slash-separated path", s)
		}
	}

	k := &Kit{
		Name:        kf.Name,
		Description: kf.Description,
		TargetHint:  kf.TargetHint,
		Shared:      kf.Shared,
		Dir:         abs,
	}

	itemsDir := filepath.Join(abs, "items")
	entries, err := os.ReadDir(itemsDir)
	if err != nil {
		return nil, nil, fmt.Errorf("kit %s: reading items: %w", abs, err)
	}
	var problems []error
	ids := map[string]bool{}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		it, itemProblems := loadItem(filepath.Join(itemsDir, entry.Name()), entry.Name(), kf.Shared, ids)
		if it != nil && len(itemProblems) == 0 {
			k.Items = append(k.Items, it)
		}
		problems = append(problems, itemProblems...)
	}
	if len(k.Items) == 0 {
		fail("kit.yaml", "", "no items found under items/")
	}
	if len(errs) > 0 {
		return nil, problems, errors.Join(errs...)
	}

	sort.Slice(k.Items, func(i, j int) bool { return k.Items[i].ID < k.Items[j].ID })
	return k, problems, nil
}

// loadItem reads and validates one item directory. Problems are reported
// through fail and leave a partially filled item behind; the caller
// rejects the kit on any error.
func loadItem(dir, dirName string, shared []string, ids map[string]bool) (*Item, []error) {
	file := filepath.ToSlash(filepath.Join("items", dirName, "item.yaml"))
	it := &Item{Dir: dir}
	var problems []error
	fail := func(file, field, format string, args ...any) {
		problems = append(problems, &ValidationError{File: file, Field: field, Msg: fmt.Sprintf(format, args...)})
	}

	var f itemFile
	if err := decodeFile(filepath.Join(dir, "item.yaml"), file, &f); err != nil {
		var ve *ValidationError
		if errors.As(err, &ve) {
			fail(ve.File, ve.Field, "%s", ve.Msg)
			return it, problems
		}
		fail(file, "", "%v", err)
		return it, problems
	}

	it.ID = f.ID
	it.Version = f.Version
	it.Description = f.Description
	it.Tags = f.Tags
	it.HasChangelog = fileExists(filepath.Join(dir, "CHANGELOG.md"))

	switch {
	case f.ID == "":
		fail(file, "id", "is required")
	case !idSyntax.MatchString(f.ID):
		fail(file, "id", "must match %s", idSyntax)
	case ids[f.ID]:
		fail(file, "id", "%q is already used by another item in this kit", f.ID)
	default:
		ids[f.ID] = true
	}

	if f.Version == "" {
		fail(file, "version", "is required")
	} else if !validVersion(f.Version) {
		fail(file, "version", "%q is not a semantic version", f.Version)
	}

	seenVar := map[string]bool{}
	for i, v := range f.Vars {
		field := fmt.Sprintf("vars[%d]", i)
		switch {
		case v.ID == "":
			fail(file, field+".id", "is required")
		case seenVar[v.ID]:
			fail(file, field+".id", "%q is declared twice", v.ID)
		default:
			seenVar[v.ID] = true
		}
		it.Vars = append(it.Vars, Var(v))
	}

	for i, c := range f.Contributes {
		field := fmt.Sprintf("contributes[%d]", i)
		src, kind, n := contributionSource(c)
		switch n {
		case 0:
			fail(file, field, "needs exactly one of file, section, or json")
			continue
		case 1:
			// One kind declared: valid.
		default:
			fail(file, field, "declares more than one of file, section, and json")
			continue
		}
		if src == "" {
			fail(file, field+"."+string(kind), "is required")
			continue
		}
		if !validRelPath(src) {
			fail(file, field+"."+string(kind), "%q is not a relative slash-separated path", src)
			continue
		}
		if !validRelPath(c.To) {
			fail(file, field+".to", "%q is not a relative slash-separated path", c.To)
			continue
		}
		if kind == KindFile && contains(shared, c.To) {
			fail(file, field+".to", "a file contribution may not target a shared file")
			continue
		}
		if kind != KindFile && !contains(shared, c.To) {
			fail(file, field+".to", "%s contributions may target only files listed in the kit's shared", kind)
			continue
		}
		it.Contributes = append(it.Contributes, resolveSource(dir, kind, src, c.To, c.Template, field, file, fail)...)
	}

	return it, problems
}

// resolveSource checks that a contribution's source exists and expands directory
// file sources into one contribution per file, so every contribution names one
// plain file. Problems are reported through fail and leave the contribution out.
func resolveSource(itemDir string, kind ContributionKind, src, to string, template bool, field, file string, fail func(file, field, format string, args ...any)) []Contribution {
	path := filepath.Join(itemDir, filepath.FromSlash(src))
	info, err := os.Stat(path)
	if err != nil {
		fail(file, field+"."+string(kind), "source %q cannot be read: %v", src, err)
		return nil
	}
	if info.IsDir() {
		if kind != KindFile {
			fail(file, field+"."+string(kind), "source %q is a directory, but only file contributions may use directories", src)
			return nil
		}
		var out []Contribution
		err = filepath.WalkDir(path, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			rel, err := filepath.Rel(path, p)
			if err != nil {
				return err
			}
			out = append(out, Contribution{
				Kind:     KindFile,
				Src:      filepath.ToSlash(filepath.Join(src, rel)),
				To:       to + "/" + filepath.ToSlash(rel),
				Template: template,
			})
			return nil
		})
		if err != nil {
			fail(file, field+".file", "source %q cannot be read: %v", src, err)
			return nil
		}
		if len(out) == 0 {
			fail(file, field+".file", "source directory %q holds no files", src)
			return nil
		}
		sort.Slice(out, func(i, j int) bool { return out[i].To < out[j].To })
		return out
	}
	return []Contribution{{Kind: kind, Src: src, To: to, Template: template}}
}

// contributionSource returns the source path, its kind, and how many kind keys are
// set on the entry.
func contributionSource(c contributionFile) (src string, kind ContributionKind, n int) {
	if c.File != nil {
		src, kind, n = *c.File, KindFile, n+1
	}
	if c.Section != nil {
		src, kind, n = *c.Section, KindSection, n+1
	}
	if c.JSON != nil {
		src, kind, n = *c.JSON, KindJSON, n+1
	}
	return src, kind, n
}

// validVersion reports whether v is a full semantic version — major, minor, and
// patch present, with optional prerelease and build metadata. The x/mod
// implementation alone would accept shortened forms such as 1.2, which the
// version ordering needs in full.
func validVersion(v string) bool {
	x := "v" + v
	return semver.IsValid(x) && semver.Canonical(x) == x
}

// CompareVersions orders two validated semantic versions: -1 when a is lower,
// 0 when they are equal, 1 when a is higher. It compares the version declared
// in item.yaml against the one recorded in the lock.
func CompareVersions(a, b string) int {
	return semver.Compare("v"+a, "v"+b)
}

// MajorVersion returns the major component of a validated semantic version
// with the prefix semver.Major prints, for example "v2". Two versions with
// different majors mark a behavior change, which the TUI flags for review.
func MajorVersion(v string) string {
	return semver.Major("v" + v)
}

// validRelPath reports whether p is a non-empty relative path of forward-slash-
// separated elements, each a plain name: no absolute paths, no "." or ".." elements,
// no backslashes, no drive letters.
func validRelPath(p string) bool {
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

func contains(list []string, s string) bool {
	for _, el := range list {
		if el == s {
			return true
		}
	}
	return false
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

// ReadSources reads the source file of every contribution of the selected items,
// keyed by the path SourcePath produces. The planner receives this map instead of
// reading kit files itself, and apply writes action content only, so the content is
// validated once, before the first write.
func ReadSources(kits []*Kit, selection []string) (map[string][]byte, error) {
	index := map[string]*Item{}
	for _, k := range kits {
		for _, it := range k.Items {
			index[ItemKey(k.Name, it.ID)] = it
		}
	}
	out := map[string][]byte{}
	for _, key := range selection {
		it, ok := index[key]
		if !ok {
			continue
		}
		for _, c := range it.Contributes {
			path := it.SourcePath(c)
			if _, done := out[path]; done {
				continue
			}
			data, err := os.ReadFile(path) //nolint:gosec // src passed validRelPath at load; the path stays inside the kit
			if err != nil {
				return nil, fmt.Errorf("%s: read %s: %w", key, c.Src, err)
			}
			out[path] = data
		}
	}
	return out, nil
}

// decodeFile reads one YAML file into out. A file that cannot be read or parsed
// becomes a ValidationError carrying the given display path; parser errors carry
// their own position.
func decodeFile(path, display string, out any) error {
	data, err := os.ReadFile(path) //nolint:gosec // the loader builds the path from the registered kit directory
	if err != nil {
		return &ValidationError{File: display, Msg: fmt.Sprintf("cannot be read: %v", err)}
	}
	if err := yaml.Unmarshal(data, out); err != nil {
		line, msg := parserError(err)
		return &ValidationError{File: display, Line: line, Msg: msg}
	}
	return nil
}

// parserError reduces a go-yaml parser error to its position and its first
// message line: the errors open with "[line:column]" and repeat the source
// snippet below, which the one-line interface errors leave out.
func parserError(err error) (line int, msg string) {
	text := err.Error()
	if end := strings.IndexByte(text, '\n'); end >= 0 {
		text = text[:end]
	}
	if !strings.HasPrefix(text, "[") {
		return 0, text
	}
	end := strings.IndexByte(text, ']')
	if end < 0 {
		return 0, text
	}
	num, _, found := strings.Cut(text[1:end], ":")
	n, cErr := strconv.Atoi(num)
	if !found || cErr != nil {
		return 0, text
	}
	return n, strings.TrimSpace(text[end+1:])
}
