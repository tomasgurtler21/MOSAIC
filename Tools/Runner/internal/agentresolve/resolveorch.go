package agentresolve

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"mosaic-run/internal/domain"
)

// ResolveOrchestrator resolves the orchestrator agent file the run was given
// into an AgentReference carrying the orchestrator invocation kind.
//
// path is domain.RunConfig.OrchestratorFilePath — the same file whose
// workflow regions orchfile enumerated. The returned reference's Identifier
// is the file's name with its extension removed, matching the filename-stem
// identity rule ResolveAll uses for ordinary agents. DefinitionPath is the
// absolute path to that file.
//
// The returned reference always carries InvocationKind ==
// domain.InvocationOrchestrator, so the orchestrator retains native
// subagent-spawning capability during a consultation.
//
// Returns *domain.RefusalError{Component: "agentresolve", Resource: path}
// when the file does not exist, cannot be read, is a directory, or has an
// empty filename stem.
//
// This function never contributes to the dispatchable agent map: it is a
// separate call with a separate return value, and ResolveAll is unchanged.
func ResolveOrchestrator(path string) (domain.AgentReference, error) {
	info, err := os.Stat(path)
	if err != nil {
		return domain.AgentReference{}, &domain.RefusalError{
			Component: "agentresolve",
			Resource:  path,
			Reason:    fmt.Sprintf("orchestrator file not accessible: %v", err),
		}
	}
	if info.IsDir() {
		return domain.AgentReference{}, &domain.RefusalError{
			Component: "agentresolve",
			Resource:  path,
			Reason:    "orchestrator path is a directory, not a file",
		}
	}

	base := filepath.Base(path)
	ext := filepath.Ext(base)
	stem := strings.TrimSuffix(base, ext)
	if stem == "" {
		return domain.AgentReference{}, &domain.RefusalError{
			Component: "agentresolve",
			Resource:  path,
			Reason:    "orchestrator file has an empty filename stem",
		}
	}

	absPath, err := filepath.Abs(path)
	if err != nil {
		return domain.AgentReference{}, &domain.RefusalError{
			Component: "agentresolve",
			Resource:  path,
			Reason:    fmt.Sprintf("failed to resolve absolute path: %v", err),
		}
	}

	return domain.AgentReference{
		Identifier:     stem,
		DefinitionPath: absPath,
		InvocationKind: domain.InvocationOrchestrator,
	}, nil
}
