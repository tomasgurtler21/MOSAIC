// Package fixtures resolves `$ref` references against a fixture root.
//
// A reference is a slash-separated relative path. Absolute paths, drive
// letters, UNC paths and any traversal escaping the root are refused — $ref
// is not a general file-read primitive.
package fixtures

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

var (
	ErrRefNotFound    = errors.New("fixtures: referenced file does not exist")
	ErrRefEscapesRoot = errors.New("fixtures: reference escapes the fixture root")
	ErrRefCycle       = errors.New("fixtures: reference cycle")
	ErrRefMalformed   = errors.New("fixtures: malformed reference")
)

// Resolver resolves $ref references against a fixture root.
type Resolver interface {
	// Resolve returns the referenced content, following nested $ref
	// occurrences inside resolved fixtures and refusing reference cycles.
	//
	// Every returned error names the offending reference and, for a cycle,
	// the full chain in the order it was traversed.
	Resolve(ref string) ([]byte, error)

	// Validate performs the same checks as Resolve without returning
	// content, so pre-flight can verify every reference without loading any
	// of them.
	Validate(ref string) error
}

// NewResolver constructs a Resolver rooted at root. root need not exist yet
// — resolution reports ErrRefNotFound the same way whether the miss is a
// missing root or a missing file beneath it.
func NewResolver(root string) (Resolver, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("fixtures: resolving root %q: %w", root, err)
	}
	return &resolver{root: abs}, nil
}

type resolver struct {
	root string
}

func (r *resolver) Resolve(ref string) ([]byte, error) {
	return r.resolve(ref, nil)
}

func (r *resolver) Validate(ref string) error {
	_, err := r.resolve(ref, nil)
	return err
}

// resolve reads the file ref names, and — when its content is itself a
// `{"$ref": "..."}` indirection — follows that reference recursively. chain
// records every reference already visited on this resolution, so a cycle is
// refused rather than looped forever.
func (r *resolver) resolve(ref string, chain []string) ([]byte, error) {
	for _, seen := range chain {
		if seen == ref {
			full := append(append([]string{}, chain...), ref)
			return nil, fmt.Errorf("%w: %s", ErrRefCycle, strings.Join(full, " -> "))
		}
	}

	full, err := r.safeJoin(ref)
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(full)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("%w: %s", ErrRefNotFound, ref)
		}
		return nil, fmt.Errorf("fixtures: reading %q: %w", ref, err)
	}

	if next, ok := indirection(data); ok {
		return r.resolve(next, append(chain, ref))
	}
	return data, nil
}

// safeJoin resolves ref against the fixture root and refuses anything that
// would escape it: an absolute path, a drive letter, a UNC path, or a
// relative path whose ".." segments walk outside the root.
func (r *resolver) safeJoin(ref string) (string, error) {
	if ref == "" {
		return "", fmt.Errorf("%w: empty reference", ErrRefMalformed)
	}
	if filepath.IsAbs(ref) {
		return "", fmt.Errorf("%w: %s", ErrRefEscapesRoot, ref)
	}
	if len(ref) >= 2 && ref[1] == ':' {
		// A drive-letter path (e.g. "C:\...") is not filepath.IsAbs on every
		// platform's interpretation of a slash-separated ref; refuse it
		// explicitly rather than relying on that alone.
		return "", fmt.Errorf("%w: %s", ErrRefEscapesRoot, ref)
	}
	if strings.HasPrefix(ref, "\\\\") || strings.HasPrefix(ref, "//") {
		return "", fmt.Errorf("%w: %s", ErrRefEscapesRoot, ref)
	}

	cleaned := filepath.Clean(filepath.FromSlash(ref))
	full := filepath.Join(r.root, cleaned)

	rel, err := filepath.Rel(r.root, full)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("%w: %s", ErrRefEscapesRoot, ref)
	}

	return full, nil
}

// indirection reports whether data is a JSON document containing exactly one
// key, "$ref", and returns the reference it names.
func indirection(data []byte) (string, bool) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return "", false
	}
	if len(raw) != 1 {
		return "", false
	}
	refRaw, ok := raw["$ref"]
	if !ok {
		return "", false
	}
	var ref string
	if err := json.Unmarshal(refRaw, &ref); err != nil {
		return "", false
	}
	return ref, true
}
