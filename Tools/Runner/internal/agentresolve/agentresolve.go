package agentresolve

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"mosaic-run/internal/domain"
)

// agentStem returns the identifier stem for the given agent filename.
// It recognizes two compound extensions (case-insensitive):
//   - ".agent.md" -- strips the full ".agent.md" suffix.
//   - ".md"        -- strips the ".md" suffix.
//
// Returns an empty string for filenames that match neither pattern, or whose
// stem after stripping is empty (e.g. ".md", ".agent.md").
// Callers must skip files whose stem is empty.
func agentStem(name string) string {
	lower := strings.ToLower(name)
	if strings.HasSuffix(lower, ".agent.md") {
		return name[:len(name)-len(".agent.md")]
	}
	if strings.HasSuffix(lower, ".md") {
		return name[:len(name)-len(".md")]
	}
	return ""
}

// ResolveAll resolves every agent identifier in the given set to a definition
// file in the specified directory. The directory is the one containing the
// orchestrator agent file the user supplied (FR-28).
//
// Returns a map from agent identifier to AgentReference.
//
// Recognizes both .md and .agent.md extensions (case-insensitive). Both
// "foo.md" and "foo.agent.md" resolve to identifier "foo". If both forms exist
// for the same identifier, resolution fails with a RefusalError (cross-format
// duplicate guard). This guard runs before the early-return path for empty
// identifiers, so it is always enforced as a directory-level invariant.
//
// Returns *domain.RefusalError naming the agent and directory if an identifier
// has no matching definition file. The match is by filename stem: identifier
// "foo-bar" matches "foo-bar.md" or "foo-bar.agent.md". The match is
// case-sensitive and exact.
func ResolveAll(dir string, identifiers []string) (map[string]domain.AgentReference, error) {
	// Always read the directory so the cross-format duplicate scan runs before
	// the early-return path for empty identifiers. This means a non-existent
	// directory returns a RefusalError even when no identifiers are requested.
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, &domain.RefusalError{
			Component: "agentresolve",
			Resource:  dir,
			Reason:    fmt.Sprintf("cannot read directory: %v", err),
		}
	}

	// Build map from filename stem to absolute path.
	// Track case-folded stems to detect cross-format and case-collision duplicates.
	stemToPath := make(map[string]string)    // original stem -> absPath
	foldedToFiles := make(map[string][]string) // lower(stem) -> []filename

	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		stem := agentStem(name)
		if stem == "" {
			// Skip files with empty stems (e.g. ".md", ".agent.md") and
			// non-agent files that agentStem does not recognize.
			continue
		}
		absPath := filepath.Join(dir, name)
		stemToPath[stem] = absPath
		folded := strings.ToLower(stem)
		foldedToFiles[folded] = append(foldedToFiles[folded], name)
	}

	// Cross-format duplicate guard: a stem claimed by more than one file is a
	// directory-level error regardless of what identifiers were requested.
	for _, names := range foldedToFiles {
		if len(names) > 1 {
			return nil, &domain.RefusalError{
				Component: "agentresolve",
				Resource:  dir,
				Reason:    fmt.Sprintf("duplicate agent stems: %s and %s", names[0], names[1]),
			}
		}
	}

	// After the duplicate scan, the empty-identifiers fast path is safe.
	if len(identifiers) == 0 {
		return map[string]domain.AgentReference{}, nil
	}

	// Resolve each requested identifier (case-sensitive exact match).
	result := make(map[string]domain.AgentReference, len(identifiers))
	for _, id := range identifiers {
		path, ok := stemToPath[id]
		if !ok {
			return nil, &domain.RefusalError{
				Component: "agentresolve",
				Resource:  id,
				Reason:    fmt.Sprintf("no definition file for agent %q found in directory %q", id, dir),
			}
		}

		result[id] = domain.AgentReference{
			Identifier:     id,
			DefinitionPath: path,
			InvocationKind: domain.InvocationOrdinary,
		}
	}

	return result, nil
}

// ResolveOne resolves a single agent identifier to a definition file in the
// given directory. Returns a single AgentReference for the matched file.
//
// Used by session.go for infrastructure agents (commit-class and declared
// infra agents) which are dispatched outside the main routing table.
//
// Differences from ResolveAll:
//   - Does NOT run the whole-directory cross-format duplicate scan.
//   - If exactly one file matches: returns the resolved reference.
//   - If two or more files match: returns *domain.RefusalError (ambiguous).
//   - If no file matches: returns *domain.RefusalError (no definition file).
//   - If id is empty: returns *domain.RefusalError.
//   - Uses case-sensitive exact match (consistent with ResolveAll).
//   - InvocationKind is set to "" (empty string).
//
// Returns *domain.RefusalError if the identifier has no matching definition
// file or is ambiguous, same error type as ResolveAll.
func ResolveOne(dir string, id string) (domain.AgentReference, error) {
	if id == "" {
		return domain.AgentReference{}, &domain.RefusalError{
			Component: "agentresolve",
			Resource:  dir,
			Reason:    "agent identifier must not be empty",
		}
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return domain.AgentReference{}, &domain.RefusalError{
			Component: "agentresolve",
			Resource:  dir,
			Reason:    fmt.Sprintf("cannot read directory: %v", err),
		}
	}

	var matches []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		stem := agentStem(name)
		if stem == id {
			matches = append(matches, name)
		}
	}

	switch len(matches) {
	case 0:
		return domain.AgentReference{}, &domain.RefusalError{
			Component: "agentresolve",
			Resource:  id,
			Reason:    fmt.Sprintf("no definition file for agent %q found in directory %q", id, dir),
		}
	case 1:
		absPath := filepath.Join(dir, matches[0])
		return domain.AgentReference{
			Identifier:     id,
			DefinitionPath: absPath,
			InvocationKind: "",
		}, nil
	default:
		return domain.AgentReference{}, &domain.RefusalError{
			Component: "agentresolve",
			Resource:  id,
			Reason:    fmt.Sprintf("ambiguous match: multiple files in %q claim identifier %q", dir, id),
		}
	}
}
