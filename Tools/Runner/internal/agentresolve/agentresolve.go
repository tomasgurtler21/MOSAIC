package agentresolve

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"mosaic-run/internal/domain"
)

// ResolveAll resolves every agent identifier in the given set to a definition
// file in the specified directory. The directory is the one containing the
// orchestrator agent file the user supplied (FR-28).
//
// Returns a map from agent identifier to AgentReference.
//
// Returns *domain.RefusalError naming the agent and directory if an identifier
// has no matching definition file. The match is by filename stem: identifier
// "foo-bar" matches file "foo-bar.md". The match is case-sensitive and exact.
func ResolveAll(dir string, identifiers []string) (map[string]domain.AgentReference, error) {
	if len(identifiers) == 0 {
		return map[string]domain.AgentReference{}, nil
	}

	// Scan the directory for .md files and build a stem -> path index.
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, &domain.RefusalError{
			Component: "agentresolve",
			Resource:  dir,
			Reason:    fmt.Sprintf("cannot read directory: %v", err),
		}
	}

	// Build map from filename stem to absolute path.
	// Guard against duplicate stems (safety net for future matching rule changes).
	stemToPath := make(map[string]string)
	stemCount := make(map[string]int)
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(strings.ToLower(name), ".md") {
			continue
		}
		stem := strings.TrimSuffix(name, filepath.Ext(name))
		absPath := filepath.Join(dir, name)
		stemToPath[stem] = absPath
		stemCount[stem]++
	}

	// Resolve each requested identifier.
	result := make(map[string]domain.AgentReference, len(identifiers))
	for _, id := range identifiers {
		// Check for ambiguous match (duplicate stems -- safety net).
		if stemCount[id] > 1 {
			return nil, &domain.RefusalError{
				Component: "agentresolve",
				Resource:  id,
				Reason:    fmt.Sprintf("ambiguous match: two files in %q claim identifier %q", dir, id),
			}
		}

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
