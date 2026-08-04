// Package seed builds and applies plans that copy user-supplied source files
// and directories into a target folder.
//
// Planning and application are deliberately separate. BuildPlan performs every
// validation and writes nothing, so a caller can reject an invalid seed set
// before creating the target folder. Apply performs every write and revalidates
// nothing. Neither function knows anything about runs, sessions, or the CLI.
package seed

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"mosaic-run/internal/domain"
)

// Entry is one planned file copy.
type Entry struct {
	// Source is the path to read, as resolved from the user-supplied source
	// path, in OS-native form.
	Source string

	// Dest is the path to write, relative to the target folder, always
	// forward-slash separated and never absolute, never beginning with "./",
	// and never containing "..". Converting to OS-native separators is the
	// responsibility of the writer, at write time.
	Dest string

	// SourceRoot is the user-supplied source path, exactly as given, that
	// produced this entry. For a file source it equals Source. It exists so a
	// collision refusal can name the two offending *sources* rather than only
	// the two files.
	SourceRoot string
}

// Plan is a validated, ordered set of copies. The zero Plan is a valid empty
// plan. A Plan returned by BuildPlan has passed every planning refusal rule;
// a Plan constructed any other way has not, and Apply does not recheck.
type Plan struct {
	// Entries in application order: sources in the order given, and within a
	// directory source, a deterministic walk order.
	Entries []Entry
}

// IsEmpty reports whether the plan copies nothing.
func (p Plan) IsEmpty() bool {
	return len(p.Entries) == 0
}

// reservedDestinations is the internal, authoritative set of runner-managed
// destination paths. Extend this slice to grow the reserved set.
var reservedDestinations = []string{"Orchestration.md"}

// ReservedDestinations returns the destination paths, relative to the target
// folder and forward-slash separated, that the runner manages itself and that a
// seed source may therefore never write to. Planning refuses any entry whose
// destination matches one of these.
//
// Today the sole entry is "Orchestration.md", the artifact the runner writes to
// establish the run. This is the single source of truth for the set inside this
// package; extend it here rather than at any call site.
//
// The returned slice is a copy; mutating it does not affect planning.
func ReservedDestinations() []string {
	cp := make([]string, len(reservedDestinations))
	copy(cp, reservedDestinations)
	return cp
}

// BuildPlan resolves sources into a validated copy plan. It performs no writes
// and does not require targetFolder (or any folder) to exist.
//
// Each source names either a file or a directory:
//   - A file source yields one entry whose destination is the source's base name
//     at the plan root.
//   - A directory source yields one entry per regular file beneath it, with
//     destinations preserving the path relative to the named directory. A
//     directory source contributes no entry for the directories themselves.
//
// Sources are processed in the order given, and Plan.Entries preserves that order.
// A nil or empty sources slice yields an empty plan and a nil error.
//
// BuildPlan returns a *domain.RefusalError with Component "seed" when any of the
// following hold; see "Refusal error shapes" for Resource/Reason contracts:
//   - a source path does not exist on disk
//   - a source path is itself a symlink
//   - a symlink is encountered anywhere beneath a directory source
//   - two entries resolve to the same destination
//   - an entry's destination is a runner-managed path (see ReservedDestinations)
//
// Symlink detection uses os.Lstat and fs.DirEntry.Type()&fs.ModeSymlink. It must
// never use os.Stat, which follows links and would let a symlink through.
//
// A non-nil error is always accompanied by the zero Plan.
func BuildPlan(sources []string) (Plan, error) {
	if len(sources) == 0 {
		return Plan{}, nil
	}

	// Phase 1: accumulate all entries, refusing per-source errors immediately
	// (non-existent source, symlink as source, symlink inside directory source).
	var entries []Entry

	for _, src := range sources {
		// Use Lstat so we detect symlinks without following them.
		info, err := os.Lstat(src)
		if err != nil {
			if os.IsNotExist(err) {
				return Plan{}, &domain.RefusalError{
					Component: "seed",
					Resource:  src,
					Reason:    fmt.Sprintf("source path %s does not exist", src),
				}
			}
			return Plan{}, &domain.RefusalError{
				Component: "seed",
				Resource:  src,
				Reason:    fmt.Sprintf("cannot stat source %s: %v", src, err),
			}
		}

		// Refuse source paths that are themselves symlinks.
		if info.Mode()&fs.ModeSymlink != 0 {
			return Plan{}, &domain.RefusalError{
				Component: "seed",
				Resource:  src,
				Reason:    fmt.Sprintf("source %s is a symlink; symlinks are not seeded", src),
			}
		}

		if info.IsDir() {
			// Walk the directory, accumulating one entry per regular file.
			// filepath.WalkDir visits entries in lexical order, which ensures
			// deterministic output for a given filesystem state.
			walkErr := filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
				if err != nil {
					return err
				}
				// Detect symlinks via the DirEntry type bits (never via Stat).
				if d.Type()&fs.ModeSymlink != 0 {
					return &domain.RefusalError{
						Component: "seed",
						Resource:  p,
						Reason:    fmt.Sprintf("%s is a symlink inside a directory source; symlinks are not seeded", p),
					}
				}
				// Skip the directory entries themselves; only regular files become entries.
				if d.IsDir() {
					return nil
				}
				// Compute the path relative to the named directory root, then
				// normalise to forward slashes so the representation is identical
				// on Windows and POSIX.
				rel, relErr := filepath.Rel(src, p)
				if relErr != nil {
					return fmt.Errorf("seed: computing relative path for %q: %w", p, relErr)
				}
				dest := filepath.ToSlash(rel)
				entries = append(entries, Entry{
					Source:     p,
					Dest:       dest,
					SourceRoot: src,
				})
				return nil
			})
			if walkErr != nil {
				return Plan{}, walkErr
			}
		} else {
			// Regular file: destination is its base name at the plan root.
			// filepath.Base returns a name without any separator, so no further
			// normalisation is needed for forward-slash compliance.
			dest := filepath.Base(src)
			entries = append(entries, Entry{
				Source:     src,
				Dest:       dest,
				SourceRoot: src,
			})
		}
	}

	// Phase 2: validate cross-entry constraints (collision, reserved paths).
	// All destinations have been accumulated, so cross-source collisions are
	// detectable even when the two sources are far apart in the source list.
	reservedSet := make(map[string]bool, len(reservedDestinations))
	for _, r := range reservedDestinations {
		reservedSet[r] = true
	}

	// destToFirstEntry maps each destination to the first entry that claimed it.
	destToFirstEntry := make(map[string]Entry, len(entries))
	for _, e := range entries {
		if first, collision := destToFirstEntry[e.Dest]; collision {
			return Plan{}, &domain.RefusalError{
				Component: "seed",
				Resource:  e.Dest,
				Reason: fmt.Sprintf(
					"destination collision: source %s and source %s both resolve to %s",
					first.SourceRoot, e.SourceRoot, e.Dest,
				),
			}
		}
		destToFirstEntry[e.Dest] = e

		if reservedSet[e.Dest] {
			return Plan{}, &domain.RefusalError{
				Component: "seed",
				Resource:  e.Dest,
				Reason:    "destination is managed by the runner and cannot be seeded",
			}
		}
	}

	return Plan{Entries: entries}, nil
}

// Apply copies every entry in plan into targetFolder. targetFolder must already
// exist; Apply does not create it. Intermediate destination directories are
// created as needed.
//
// Files are copied verbatim: byte-for-byte, with no templating, no variable
// substitution, and no injected YAML frontmatter of any kind. Entry.Dest is
// converted from its forward-slash form to OS-native separators at the point of
// the write and nowhere earlier.
//
// Entries are applied in Plan.Entries order. Apply stops at the first failure and
// returns a *domain.RefusalError naming the entry that failed. Apply performs no
// cleanup of entries already written: rollback is the caller's responsibility and
// is broader in scope than this package (the caller may need to remove files this
// package never wrote).
//
// Applying an empty plan is a no-op that returns nil.
func Apply(plan Plan, targetFolder string) error {
	for _, e := range plan.Entries {
		// Convert the forward-slash destination to OS-native separators only at
		// write time, as required by the design contract.
		nativeDest := filepath.FromSlash(e.Dest)
		destPath := filepath.Join(targetFolder, nativeDest)

		// Create any intermediate directories that the destination requires.
		if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
			return &domain.RefusalError{
				Component: "seed",
				Resource:  e.Source,
				Reason:    fmt.Sprintf("failed to create destination directory for %q: %v", e.Dest, err),
			}
		}

		// Copy the source file verbatim to the destination.
		if err := copyFile(e.Source, destPath); err != nil {
			return &domain.RefusalError{
				Component: "seed",
				Resource:  e.Source,
				Reason:    fmt.Sprintf("source %s: failed to copy to %s: %v", e.Source, e.Dest, err),
			}
		}
	}
	return nil
}

// copyFile copies src to dst byte-for-byte. It opens src for reading and
// creates (or truncates) dst for writing. It performs no content transformation.
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}

	if _, err = io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// Ensure the domain package is referenced even when no RefusalError is
// constructed in the visible function bodies (import-cycle guard).
var _ *domain.RefusalError
