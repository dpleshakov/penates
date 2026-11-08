package builtin

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/dpleshakov/penates/internal/state"
)

const (
	// kitDirName is the name of the managed kit directory inside the data
	// directory.
	kitDirName = "builtin-kit"
	// stampName is the name of the stamp file beside the kit directory. It
	// holds the embedded hash of the content the directory was filled with,
	// and it is written only after the directory is fully in place.
	stampName = "builtin-kit.sha256"
)

// Report is what one Sync run found and did. The interface shows the
// notices and the conflict on the kit row.
type Report struct {
	// Notices are human-readable lines about what Sync did or refused to do.
	Notices []string
	// Modified reports the managed directory as differing from the content
	// it was unpacked with: a local modification.
	Modified bool
	// Conflict reports a local modification while the embedded content
	// differs: the replacement is blocked until an explicit reset.
	Conflict bool
	// Registered reports that this run registered the built-in kit under
	// its reserved name.
	Registered bool
}

// KitDir returns the managed kit directory inside the penates data
// directory.
func KitDir(dataDir string) string { return filepath.Join(dataDir, kitDirName) }

// Sync brings the managed kit directory under dataDir in step with the kit
// embedded in src and the registry in store, and returns what it did.
//
// The embedded content is hashed at runtime — there is no committed hash a
// stale copy of would silently suppress replacements. The managed directory
// is governed by the three-state model the product uses for targets: the
// stamp is the base, the directory content is the current state, the
// embedded content is the incoming one. A directory present without a stamp
// is not owned and is never touched. A failure to write is reported and
// retried on the next start; only a registry that cannot be read is an
// error.
func Sync(dataDir string, store *state.Store, src fs.FS) (Report, error) {
	var rep Report
	kitDir := KitDir(dataDir)
	stampPath := filepath.Join(dataDir, stampName)

	// An interrupted run may have left its temporary artifacts behind; they
	// are swept before anything else, so the data directory holds no stray
	// directories (best effort — the next start tries again).
	sweepLeftovers(dataDir)

	reg, err := store.LoadRegistry()
	if err != nil {
		return rep, err
	}
	// The name held by another directory is a foreign or repointed kit:
	// penates stands down — no unpack, no replace, no registration.
	if k, ok := reg.Kits[state.BuiltinKitName]; ok && k.Dir != kitDir {
		rep.Notices = append(rep.Notices,
			fmt.Sprintf("built-in kit not registered: the name %q is taken by the kit at %s", state.BuiltinKitName, k.Dir))
		return rep, nil
	}

	incoming, err := hashTree(src)
	if err != nil {
		return rep, fmt.Errorf("built-in kit: hashing embedded content: %w", err)
	}

	dirExists := true
	if _, err := os.Stat(kitDir); err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			return rep, fmt.Errorf("built-in kit: %w", err)
		}
		dirExists = false
	}
	var dirHash string
	if dirExists {
		if dirHash, err = hashTree(os.DirFS(kitDir)); err != nil {
			return rep, fmt.Errorf("built-in kit: hashing %s: %w", kitDir, err)
		}
	}

	// ours records that the directory is penates's own content at the
	// embedded hash; only then may the kit be registered.
	ours := false
	switch {
	case dirExists && stampHash(stampPath) == "":
		// A directory penates never wrote is not owned — not by the state
		// it lacks a stamp for, but by rule: stand down entirely so a later
		// reset can never overwrite it.
		rep.Notices = append(rep.Notices,
			fmt.Sprintf("built-in kit not installed: %s exists and is not owned by penates (delete or rename it to let penates unpack the kit there)", kitDir))
		return rep, nil
	case !dirExists:
		// Absent — a fresh data directory, or a crash left a stamp over no
		// directory. Either way the directory is treated as absent.
		if err := unpack(dataDir, kitDir, src); err != nil {
			rep.Notices = append(rep.Notices, reportWriteErr("unpack", err))
			return rep, nil
		}
		if err := writeStamp(stampPath, incoming); err != nil {
			// No stamp over an existing directory is the not-owned state:
			// the next start will stand down, not retry. The notice names
			// the real way out.
			rep.Notices = append(rep.Notices,
				fmt.Sprintf("built-in kit: unpacked into %s but could not record the stamp (%v); delete the directory so the next start unpacks the kit again", kitDir, err))
			return rep, nil
		}
		rep.Notices = append(rep.Notices, fmt.Sprintf("built-in kit unpacked into %s", kitDir))
		ours = true
	case dirHash == incoming:
		// The directory already holds the embedded content — in place, or a
		// crash between the rename and the stamp left the stamp stale.
		// Write nothing but the stamp.
		if stampHash(stampPath) != incoming {
			if err := writeStamp(stampPath, incoming); err != nil {
				rep.Notices = append(rep.Notices, reportWriteErr("record the stamp of", err))
				return rep, nil
			}
		}
		ours = true
	case stampHash(stampPath) == incoming:
		// In place and current; a differing directory content is a local
		// modification the binary has nothing to say about.
		rep.Modified = true
		ours = true
	case dirHash == stampHash(stampPath):
		// Ours and untouched: replace with the embedded content.
		if err := replace(dataDir, kitDir, src); err != nil {
			rep.Notices = append(rep.Notices, reportWriteErr("replace", err))
			return rep, nil
		}
		if err := writeStamp(stampPath, incoming); err != nil {
			rep.Notices = append(rep.Notices, reportWriteErr("record the stamp of", err))
			return rep, nil
		}
		rep.Notices = append(rep.Notices, "built-in kit updated to the content of this penates")
		ours = true
	default:
		// Modified locally while the embedded content differs: a conflict.
		// Nothing is written; an explicit reset takes the embedded content.
		rep.Modified = true
		rep.Conflict = true
		rep.Notices = append(rep.Notices,
			"built-in kit update blocked: "+kitDir+" is modified locally; reset it to take the content of this penates")
		ours = true
	}

	if !ours {
		return rep, nil
	}
	added, err := store.RegisterBuiltinKit(kitDir)
	if err != nil {
		rep.Notices = append(rep.Notices, reportWriteErr("register", err))
		return rep, nil
	}
	rep.Registered = added
	return rep, nil
}

// sweepLeftovers removes the temporary artifacts an interrupted run may
// have left in the data directory: unpack directories, their .old copies,
// and stamp temp files. The patterns match only the dotted names the runs
// create; the managed kit directory itself never matches. Best effort —
// a failure to remove is ignored.
func sweepLeftovers(dataDir string) {
	matches, err := filepath.Glob(filepath.Join(dataDir, ".builtin-kit-*"))
	if err != nil {
		return
	}
	if stampTmp, err := filepath.Glob(filepath.Join(dataDir, ".builtin-stamp-*")); err == nil {
		matches = append(matches, stampTmp...)
	}
	for _, m := range matches {
		_ = os.RemoveAll(m)
	}
}

// Reset discards a modified managed directory and unpacks the embedded
// content in its place: the explicit way out of a local modification or a
// conflict (concept: the built-in kit). The directory must be owned — a
// stamp must name it — so a reset can never overwrite content penates did
// not write. On return the directory is owned and stamped at the embedded
// hash.
func Reset(dataDir string, src fs.FS) error {
	kitDir := KitDir(dataDir)
	stampPath := filepath.Join(dataDir, stampName)
	if stampHash(stampPath) == "" {
		return fmt.Errorf("built-in kit: %s carries no stamp; it is not owned and is not reset", kitDir)
	}
	if err := os.RemoveAll(kitDir); err != nil {
		return err
	}
	if err := unpack(dataDir, kitDir, src); err != nil {
		return err
	}
	incoming, err := hashTree(src)
	if err != nil {
		return fmt.Errorf("built-in kit: hashing embedded content: %w", err)
	}
	return writeStamp(stampPath, incoming)
}

// stampHash returns the hash the stamp file records, or "" when the stamp is
// missing or unreadable — a stamp that proves nothing proves no ownership.
func stampHash(path string) string {
	data, err := os.ReadFile(path) //nolint:gosec // the path is built from the data directory
	if err != nil || len(data) != sha256.Size*2 {
		return ""
	}
	return string(data)
}

// writeStamp records the embedded hash, atomically, so an interruption
// cannot leave a half-written stamp behind.
func writeStamp(path, hash string) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".builtin-stamp-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	if _, err := io.WriteString(tmp, hash); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// unpack writes src into a fresh temporary directory under dataDir and moves
// it into place. The stamp is written by the caller only after this
// returns, so an interrupted unpack can never look owned.
func unpack(dataDir, kitDir string, src fs.FS) error {
	tmp, err := os.MkdirTemp(dataDir, ".builtin-kit-*")
	if err != nil {
		return err
	}
	if err := copyTree(src, tmp); err != nil {
		_ = os.RemoveAll(tmp)
		return err
	}
	if err := os.Rename(tmp, kitDir); err != nil {
		_ = os.RemoveAll(tmp)
		return err
	}
	return nil
}

// replace moves the managed directory aside, moves a fresh unpack into its
// place, and removes the old content. A crash between the two renames
// leaves no directory — the next start reads that as absent and unpacks
// again.
func replace(dataDir, kitDir string, src fs.FS) error {
	tmp, err := os.MkdirTemp(dataDir, ".builtin-kit-*")
	if err != nil {
		return err
	}
	if err := copyTree(src, tmp); err != nil {
		_ = os.RemoveAll(tmp)
		return err
	}
	aside := tmp + ".old"
	if err := os.Rename(kitDir, aside); err != nil {
		_ = os.RemoveAll(tmp)
		return err
	}
	if err := os.Rename(tmp, kitDir); err != nil {
		// Put the owned content back; the retry happens on the next start.
		_ = os.Rename(aside, kitDir)
		_ = os.RemoveAll(tmp)
		return err
	}
	_ = os.RemoveAll(aside)
	return nil
}

// copyTree writes every file of src into dst, creating the directories on
// the way.
func copyTree(src fs.FS, dst string) error {
	return fs.WalkDir(src, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if p == "." {
				return nil
			}
			// The unpacked kit is read by penates alone; the store's own
			// directory mode is the matching permission.
			return os.MkdirAll(filepath.Join(dst, filepath.FromSlash(p)), 0o750)
		}
		data, err := fs.ReadFile(src, p)
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(dst, filepath.FromSlash(p)), data, 0o600)
	})
}

// hashTree returns the hash of every file of f: the file path and the file
// content, in walk order, fed to one SHA-256. The walk is lexical, so the
// same tree hashes the same on every platform.
func hashTree(f fs.FS) (string, error) {
	h := sha256.New()
	err := fs.WalkDir(f, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		data, err := fs.ReadFile(f, p)
		if err != nil {
			return err
		}
		if _, err := io.WriteString(h, p); err != nil {
			return err
		}
		if _, err := h.Write([]byte{0}); err != nil {
			return err
		}
		_, err = h.Write(data)
		return err
	})
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// reportWriteErr words a failed write as a notice: named, with the next
// step, never fatal.
func reportWriteErr(what string, err error) string {
	return fmt.Sprintf("built-in kit: could not %s the kit directory (%v); retried on the next start", what, err)
}
