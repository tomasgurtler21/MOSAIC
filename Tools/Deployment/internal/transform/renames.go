package transform

import (
	"errors"
	"fmt"
)

// RenameEntry is one old-name-to-new-name mapping for a renamed source [[INJECTION:]].
// Entries never expire. Adding one here is the whole cost of renaming an injection.
// Do not remove or repurpose an entry once it is added — deployed files may still carry
// the old name indefinitely.
type RenameEntry struct {
	Old string // the [[INJECTION:]] name as it appears in already-deployed files
	New string // the [[INJECTION:]] name as declared in the source today
}

// InjectionRenames is the canonical rename table for source [[INJECTION:]] names that
// have been renamed since their last deployment. It is a compile-time literal: no file
// is read, so the transform stays a pure function. Add an entry when a source
// [[INJECTION:]] name changes; never remove or repurpose one.
var InjectionRenames = []RenameEntry{
	// (initially empty)
}

// ErrRenameMapConflict reports an ambiguous injection rename table. Ambiguity means the
// table cannot be applied deterministically without guessing the user's intent. The user
// must resolve the conflict; the tool never picks an arbitrary destination for their
// content.
var ErrRenameMapConflict = errors.New("injection rename map is ambiguous")

// ValidateRenames reports the first ambiguity in the rename table. Ambiguity is:
//   - an empty Old or New field in any entry,
//   - Old == New in any entry (a self-mapping; no migration would occur but the table is misleading),
//   - the same Old value appearing in more than one entry (one source name, two destinations),
//   - a name appearing as both an Old and a New in the table (a mapping chain — rejected
//     outright rather than resolved transitively, because resolution would depend on
//     iteration order and the chain cannot be applied in a single pass).
//
// The returned error wraps ErrRenameMapConflict and names the conflicting entry values
// so the user can locate the problem without inspecting the full table.
func ValidateRenames(entries []RenameEntry) error {
	seenOld := make(map[string]bool, len(entries))
	newNames := make(map[string]bool, len(entries))

	for _, e := range entries {
		if e.Old == "" {
			return fmt.Errorf("entry has empty Old field: %w", ErrRenameMapConflict)
		}
		if e.New == "" {
			return fmt.Errorf("entry %q has empty New field: %w", e.Old, ErrRenameMapConflict)
		}
		if e.Old == e.New {
			return fmt.Errorf("self-mapping entry %q maps to itself: %w", e.Old, ErrRenameMapConflict)
		}
		if seenOld[e.Old] {
			return fmt.Errorf("duplicate Old value %q appears in more than one entry: %w", e.Old, ErrRenameMapConflict)
		}
		seenOld[e.Old] = true
		newNames[e.New] = true
	}

	// Check for chains: a name that appears as both an Old key and a New destination.
	// Chains are rejected outright rather than resolved transitively, because the resolution
	// order would depend on iteration order and produce non-deterministic results.
	for _, e := range entries {
		if newNames[e.Old] {
			return fmt.Errorf("mapping chain: %q appears as both an Old and a New value: %w", e.Old, ErrRenameMapConflict)
		}
	}

	return nil
}

// resolveRenamed returns the current name for a deployed-file [[INJECTION:]] name and
// whether a rename was applied. Resolution is a single hop by construction: chains are
// rejected by ValidateRenames before this function is called. Custom names are never
// passed to this function — the rename table applies to NodeInjection only.
func resolveRenamed(name string, entries []RenameEntry) (string, bool) {
	for _, e := range entries {
		if e.Old == name {
			return e.New, true
		}
	}
	return name, false
}
