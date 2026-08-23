// Package fixtures -- multi-root resolution implementation.
//
// This file provides NewMultiRootResolver, ResolverFactory, NewResolverFactory,
// and ResolutionError, enabling fixture refs to be resolved against a
// document-local root first and a shared fallback root second.
package fixtures

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ResolverFactory constructs a Resolver for a given document directory.
// When sharedRoot is non-empty, the returned Resolver searches docDir first
// and sharedRoot as fallback. When sharedRoot is empty, only docDir is
// searched.
type ResolverFactory func(docDir string) (Resolver, error)

// ResolutionError is returned when a ref cannot be resolved against any
// searched root. It satisfies errors.Is(err, ErrRefNotFound) via Unwrap.
type ResolutionError struct {
	// Ref is the reference that failed to resolve.
	Ref string
	// SearchedRoots lists the absolute paths of every root that was tried,
	// in the order they were searched. At least one entry is always present.
	SearchedRoots []string
}

// Error returns a human-readable description of the resolution failure,
// naming every location that was searched.
func (e *ResolutionError) Error() string {
	if len(e.SearchedRoots) == 0 {
		return fmt.Sprintf("fixtures: %s: not found (no roots searched)", e.Ref)
	}
	return fmt.Sprintf("fixtures: %s: not found in any of: %s",
		e.Ref, strings.Join(e.SearchedRoots, ", "))
}

// Unwrap returns ErrRefNotFound so that errors.Is(err, ErrRefNotFound) holds
// for all ResolutionError values, keeping compatibility with existing callers.
func (e *ResolutionError) Unwrap() error { return ErrRefNotFound }

// multiRootResolver resolves refs by trying each root in order, returning
// content from the first root that contains the file. All existing
// containment, cycle, and malformed-ref rules apply independently per root.
type multiRootResolver struct {
	roots     []*resolver // one resolver per root, in precedence order
	rootPaths []string   // absolute paths for error messages, parallel to roots
}

// NewMultiRootResolver constructs a Resolver that resolves refs by searching
// roots in order. The first root whose file satisfies the ref wins. All
// existing containment, cycle, and malformed-ref rules apply independently
// to each root.
//
// At least one root must be provided. Each root is made absolute at
// construction time (same as NewResolver). An error is returned only if a
// root path cannot be made absolute.
//
// When resolution fails against all roots, the returned error is of type
// *ResolutionError, which names every location searched.
func NewMultiRootResolver(roots ...string) (Resolver, error) {
	if len(roots) == 0 {
		return nil, fmt.Errorf("fixtures: NewMultiRootResolver requires at least one root")
	}
	mr := &multiRootResolver{}
	for _, root := range roots {
		abs, err := filepath.Abs(root)
		if err != nil {
			return nil, fmt.Errorf("fixtures: resolving root %q: %w", root, err)
		}
		mr.roots = append(mr.roots, &resolver{root: abs})
		mr.rootPaths = append(mr.rootPaths, abs)
	}
	return mr, nil
}

// NewResolverFactory returns a ResolverFactory that uses sharedRoot as a
// fallback for every resolver it produces. sharedRoot is made absolute once
// at factory-construction time. An empty sharedRoot means no fallback.
//
// The factory it returns inserts docDir/fixtures/ as a search root between
// docDir and sharedRoot when that subdirectory exists on disk. Precedence:
// (1) docDir, (2) docDir/fixtures/ (when present), (3) sharedRoot.
// The fixtures/ subdirectory is checked once per factory call (once per
// document), not once per Resolve call. When no fixtures/ subdirectory
// exists the resolver behaves exactly as before (two roots only).
func NewResolverFactory(sharedRoot string) (ResolverFactory, error) {
	if sharedRoot == "" {
		return func(docDir string) (Resolver, error) {
			fixturesSubdir := filepath.Join(docDir, "fixtures")
			if info, err := os.Stat(fixturesSubdir); err == nil && info.IsDir() {
				return NewMultiRootResolver(docDir, fixturesSubdir)
			}
			return NewMultiRootResolver(docDir)
		}, nil
	}
	absShared, err := filepath.Abs(sharedRoot)
	if err != nil {
		return nil, fmt.Errorf("fixtures: resolving shared root %q: %w", sharedRoot, err)
	}
	return func(docDir string) (Resolver, error) {
		fixturesSubdir := filepath.Join(docDir, "fixtures")
		if info, err := os.Stat(fixturesSubdir); err == nil && info.IsDir() {
			return NewMultiRootResolver(docDir, fixturesSubdir, absShared)
		}
		return NewMultiRootResolver(docDir, absShared)
	}, nil
}

func (mr *multiRootResolver) Resolve(ref string) ([]byte, error) {
	return mr.resolve(ref, nil)
}

func (mr *multiRootResolver) Validate(ref string) error {
	_, err := mr.resolve(ref, nil)
	return err
}

// resolve implements the multi-root resolution logic with cycle detection.
func (mr *multiRootResolver) resolve(ref string, chain []string) ([]byte, error) {
	// Cycle detection: if ref already appears in the current traversal chain,
	// refuse it rather than looping forever.
	for _, seen := range chain {
		if seen == ref {
			full := append(append([]string{}, chain...), ref)
			return nil, fmt.Errorf("%w: %s", ErrRefCycle, strings.Join(full, " -> "))
		}
	}

	// Root-independent structural checks. These apply before any root is
	// consulted: a malformed or clearly-escaping ref is rejected immediately,
	// without testing it against each individual root.
	if ref == "" {
		return nil, fmt.Errorf("%w: empty reference", ErrRefMalformed)
	}
	if filepath.IsAbs(ref) {
		return nil, fmt.Errorf("%w: %s", ErrRefEscapesRoot, ref)
	}
	if len(ref) >= 2 && ref[1] == ':' {
		// Drive-letter path (e.g. "C:\..."): refuse explicitly as in the
		// single-root resolver.
		return nil, fmt.Errorf("%w: %s", ErrRefEscapesRoot, ref)
	}
	if strings.HasPrefix(ref, "\\\\") || strings.HasPrefix(ref, "//") {
		return nil, fmt.Errorf("%w: %s", ErrRefEscapesRoot, ref)
	}

	// Try each root in precedence order. Track which roots were actually
	// searched (safeJoin succeeded), so the ResolutionError can name them.
	var searchedRoots []string
	for i, r := range mr.roots {
		full, err := r.safeJoin(ref)
		if err != nil {
			// safeJoin failed — only the root-dependent ".." escape check
			// can trigger here, since root-independent checks already ran
			// above. Skip this root.
			continue
		}
		searchedRoots = append(searchedRoots, mr.rootPaths[i])

		data, readErr := os.ReadFile(full)
		if readErr != nil {
			if os.IsNotExist(readErr) {
				// File is not in this root; try the next one.
				continue
			}
			return nil, fmt.Errorf("fixtures: reading %q: %w", ref, readErr)
		}

		// File found in this root. Follow any $ref indirection using the
		// same multi-root resolver so fallback roots are available for
		// nested refs too.
		if next, ok := indirection(data); ok {
			return mr.resolve(next, append(chain, ref))
		}
		return data, nil
	}

	if len(searchedRoots) == 0 {
		// Every root rejected the ref via the root-dependent escape check.
		// Report as ErrRefEscapesRoot, matching the single-root behaviour.
		return nil, fmt.Errorf("%w: %s", ErrRefEscapesRoot, ref)
	}

	// Ref passed safeJoin for at least one root but was not found in any
	// of them: return a ResolutionError naming every root searched.
	return nil, &ResolutionError{
		Ref:           ref,
		SearchedRoots: searchedRoots,
	}
}
