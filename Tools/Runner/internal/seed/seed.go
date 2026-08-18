// Package seed builds and applies plans that copy user-supplied source files
// and directories into a target folder.
//
// Planning and application are deliberately separate. BuildPlan performs every
// validation and writes nothing, so a caller can reject an invalid seed set
// before creating the target folder. Apply performs every write and revalidates
// nothing. Neither function knows anything about runs, sessions, or the CLI.
//
// BuildPlan also enforces a naming rule: exactly one source across the whole
// seed set must be a "Requirement*" candidate (case-insensitive, matched
// against file sources and the top-level files of directory sources), and
// that entry's destination is renamed to Requirements.md. A seed set with
// zero or more than one candidate is refused; see BuildPlan for the precedence
// of this refusal relative to the others.
package seed

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"mosaic-run/internal/domain"
)

// requirementsDestName is the literal destination name that the single
// Requirement* candidate in a seed set is renamed to. Kept as a single named
// constant so a future caller-supplied override is a narrow change.
const requirementsDestName = "Requirements.md"

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

// HasDest reports whether the plan writes the given destination name,
// compared against Entry.Dest exactly as the plan holds it.
func (p Plan) HasDest(dest string) bool {
	for _, e := range p.Entries {
		if e.Dest == dest {
			return true
		}
	}
	return false
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
//     at the plan root, unless it is the sole Requirement* candidate, in which
//     case its destination becomes Requirements.md (see below).
//   - A directory source yields one entry per regular file beneath it, with
//     destinations preserving the path relative to the named directory. A
//     directory source contributes no entry for the directories themselves.
//
// Sources are processed in the order given, and Plan.Entries preserves that order.
// A nil or empty sources slice yields an empty plan and a nil error; this is the
// sole case in which the Requirement* naming rule does not apply.
//
// After entries are accumulated, exactly one entry across the whole seed set
// must be a "Requirement*" candidate: a case-insensitive prefix match on the
// base name, considered only for file sources and the top-level files of
// directory sources (nested directory files are never candidates, though they
// are still copied unrenamed). The single candidate's destination is renamed
// to Requirements.md; every other entry keeps its destination unchanged. Zero
// or more than one candidate refuses the whole seed set.
//
// BuildPlan returns a *domain.RefusalError with Component "seed" when any of the
// following hold; see "Refusal error shapes" for Resource/Reason contracts. They
// are checked in this order, which determines which refusal a caller sees when
// more than one would apply:
//  1. a source path does not exist on disk
//  2. a source path is itself a symlink
//  3. a symlink is encountered anywhere beneath a directory source
//  4. zero or more than one Requirement* candidate is found across the seed set
//  5. two entries resolve to the same destination
//  6. an entry's destination is a runner-managed path (see ReservedDestinations)
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

	// Requirements.md naming rule: exactly one entry across all sources
	// combined must be a "Requirement*" candidate (case-insensitive, prefix
	// match on the base name), considering only file sources and the
	// top-level files of directory sources. Nested directory files are
	// excluded from the candidate pool, though they are still copied. This
	// runs after Phase 1 accumulation and before Phase 2 validation, so
	// Phase 2 sees the renamed destination (Required Behaviour 7/8).
	var candidateIdxs []int
	for i, e := range entries {
		// A candidate's Dest has no "/" (it sits at the root of the plan
		// output): true for every file source, and for a directory source
		// only when the file is at the directory's top level.
		if strings.Contains(e.Dest, "/") {
			continue
		}
		if strings.HasPrefix(strings.ToLower(e.Dest), "requirement") {
			candidateIdxs = append(candidateIdxs, i)
		}
	}

	switch len(candidateIdxs) {
	case 1:
		entries[candidateIdxs[0]].Dest = requirementsDestName
	case 0:
		return Plan{}, &domain.RefusalError{
			Component: "seed",
			Resource:  "",
			Reason:    "no Requirement* candidate was found among the seeded sources; exactly one source must have a name starting with \"Requirement\" (case-insensitive) so it can be copied to Requirements.md",
		}
	default:
		matched := make([]string, 0, len(candidateIdxs))
		for _, idx := range candidateIdxs {
			matched = append(matched, entries[idx].Source)
		}
		return Plan{}, &domain.RefusalError{
			Component: "seed",
			Resource:  strings.Join(matched, ", "),
			Reason: fmt.Sprintf(
				"multiple Requirement* candidates were found among the seeded sources: %s; exactly one is required so it can be copied to Requirements.md",
				strings.Join(matched, ", "),
			),
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
