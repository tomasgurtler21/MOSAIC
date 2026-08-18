package app

// workspace_agent_scan.go declares the Stage 4 types for workspace-scan-driven agent set
// derivation and implements scanWorkspaceAgents, the single-pass classifier that decides
// Update's agent-set membership.
//
// Update uses the workspace scan — not workflow discovery — to determine which deployed
// agents to staleness-check. Every .md file found in the harness's deployed-agents directory
// is classified in one pass:
//
//   - Matched: the file resolves to a catalog agent (by numeric id first, filename key second).
//     These enter Update's artifact set and run the full staleness pipeline.
//   - HarnessOnly: the file resolves to no catalog agent but passes the two-signal eligibility
//     check. These enter the harness-only consent path.
//   - Neither: not recognisable as a MOSAIC-managed agent. Left byte-identical, no plan item,
//     no warning.
//
// Workflow discovery keeps its separate role: feeding orchestrator workflow-set drift
// reporting. It no longer gates which non-orchestrator agents are staleness-checked.

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"mosaic-common/docformat"
	"mosaic-deploy/internal/agentfields"
	"mosaic-deploy/internal/catalog"
	"mosaic-deploy/internal/domain"
)

// AgentMatchKind names the mechanism that resolved a scanned file to a catalog agent.
type AgentMatchKind string

const (
	// MatchByNumericID indicates the frontmatter `id` scalar resolved through
	// catalog.AgentByNumericID. This path survives a rename and is always tried first.
	MatchByNumericID AgentMatchKind = "numeric-id"
	// MatchByFileNameKey indicates the key derived from the file name resolved through
	// catalog.Agent. This is the fallback for agents with no numeric id, notably both
	// orchestrator-role files.
	MatchByFileNameKey AgentMatchKind = "filename-key"
)

// ScannedAgentMatch is one deployed file the workspace scan resolved to a catalog agent.
type ScannedAgentMatch struct {
	// TargetPath is the file's path relative to the deployment root, in the form plan items
	// and the executor use: filepath.Join(agentsDir, fileName).
	TargetPath string
	// FileName is the base file name including its extension.
	FileName string
	// AgentKey is the key of the matched catalog agent. It comes from the catalog record,
	// never from the file name, so a renamed deployed file carries its true catalog key.
	AgentKey string
	// NumericID is the frontmatter `id` scalar as written in the deployed file, or "" when absent.
	NumericID string
	// MatchedBy records which mechanism resolved the match. Diagnostic and test-facing; it
	// does not alter downstream behaviour.
	MatchedBy AgentMatchKind
}

// WorkspaceAgentScan is the result of one classification pass over the harness's
// deployed-agents directory. Matched and HarnessOnly are disjoint by construction; a file
// in neither is a file Update leaves entirely alone.
type WorkspaceAgentScan struct {
	// Matched is every scanned file that resolved to a catalog agent, sorted by TargetPath.
	Matched []ScannedAgentMatch
	// HarnessOnly is every scanned file that resolved to no catalog agent but passed the
	// two-signal eligibility check, sorted by TargetPath. Element shape and semantics are
	// exactly today's HarnessOnlyAgent — this field is the replacement source for what
	// scanHarnessOnlyAgents returns to Update.
	HarnessOnly []HarnessOnlyAgent
}

// MatchedKeys returns the catalog agent keys of every matched file, sorted and deduplicated.
// This is the value fed into artifact resolution. Deduplication matters: two deployed files
// can resolve to the same catalog key (a duplicate numeric id, or an id match and a filename
// match landing on one agent). The scan never picks between colliding files — it contributes
// the key once and leaves path resolution, and the ErrAmbiguousDeployedID collision report,
// to the existing resolveDeployedPath contract during probing.
func (s WorkspaceAgentScan) MatchedKeys() []string {
	seen := make(map[string]bool)
	var keys []string
	for _, m := range s.Matched {
		if !seen[m.AgentKey] {
			seen[m.AgentKey] = true
			keys = append(keys, m.AgentKey)
		}
	}
	sort.Strings(keys)
	return keys
}

// scanWorkspaceAgents performs ONE non-recursive walk of the harness's deployed-agents
// directory (agentsDir, relative to workspace) and classifies every file it finds into
// exactly one of three mutually exclusive outcomes: Matched, HarnessOnly, or Neither.
//
// Catalog matching, in order, first hit wins:
//  1. The file's frontmatter `id` scalar (read via agentfields.ReadOrder, prefixed name
//     preferred over legacy), looked up through catalog.AgentByNumericID. This survives a
//     rename, so a renamed deployed file still matches its catalog source.
//  2. The key derived from the file name (trailing ".agent.md" or ".md" removed), looked
//     up through catalog.Agent. This is the necessary fallback for agents with no numeric
//     id, notably both orchestrator-role files.
//
// An unmatched file is then tested for harness-only eligibility (two-signal check).
// isOrchestratorFileName gates the harness-only path only — it must not gate the matched
// path: an orchestrator-role file that resolves via a catalog lookup is reported as Matched.
//
// Tolerance is the contract: a file that cannot be read, cannot be parsed, carries no
// frontmatter, is not a ".md" file, or is a directory is skipped silently.
//
// An empty agentsDir yields an empty scan without touching the workspace root, and an
// unreadable or absent directory yields an empty scan rather than an error.
//
// Determinism: both returned slices are sorted by TargetPath. MatchedKeys is sorted and
// deduplicated by key.
func scanWorkspaceAgents(workspace, agentsDir string, c catalog.Catalog) WorkspaceAgentScan {
	// Guard: an empty agentsDir must yield an empty result without scanning workspace root.
	if agentsDir == "" {
		return WorkspaceAgentScan{}
	}

	dir := filepath.Join(workspace, agentsDir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		// Directory does not exist or is unreadable — return empty result.
		return WorkspaceAgentScan{}
	}

	var matched []ScannedAgentMatch
	var harnessOnly []HarnessOnlyAgent

	idField, _ := agentfields.ByGeneric("id")

	for _, entry := range entries {
		// Non-recursive: skip directories.
		if entry.IsDir() {
			continue
		}

		name := entry.Name()

		// Only .md files.
		if !strings.HasSuffix(name, ".md") {
			continue
		}

		// Read file bytes tolerantly — skip silently on error.
		filePath := filepath.Join(dir, name)
		data, err := os.ReadFile(filePath)
		if err != nil {
			continue
		}

		// Parse the document. Unparseable bytes are skipped silently.
		doc, err := docformat.Parse(data)
		if err != nil {
			continue
		}

		fm := doc.Frontmatter()
		if !fm.Present() {
			// No frontmatter block — skip silently.
			continue
		}

		targetPath := filepath.Join(agentsDir, name)

		// Read the numeric ID from frontmatter, accepting both the prefixed and legacy forms.
		var numericID string
		for _, key := range agentfields.ReadOrder(idField) {
			if v, ok := fm.Get(key); ok && v.Kind == domain.KindScalar && v.Scalar != "" {
				numericID = v.Scalar
				break
			}
		}

		// Catalog matching step 1: try numeric ID lookup. This survives a rename.
		if numericID != "" {
			if agent, ok := c.AgentByNumericID(numericID); ok {
				matched = append(matched, ScannedAgentMatch{
					TargetPath: targetPath,
					FileName:   name,
					AgentKey:   agent.Key,
					NumericID:  numericID,
					MatchedBy:  MatchByNumericID,
				})
				continue
			}
		}

		// Catalog matching step 2: try filename key lookup. Fallback for agents with no
		// numeric id, notably both orchestrator-role files.
		fileKey := agentKeyFromFileName(name)
		if agent, ok := c.Agent(fileKey); ok {
			matched = append(matched, ScannedAgentMatch{
				TargetPath: targetPath,
				FileName:   name,
				AgentKey:   agent.Key,
				NumericID:  numericID,
				MatchedBy:  MatchByFileNameKey,
			})
			continue
		}

		// Neither catalog lookup succeeded. Check harness-only eligibility.
		// Orchestrator-named files are excluded from the harness-only path, matching the
		// existing scanHarnessOnlyAgents contract.
		if isOrchestratorFileName(name) {
			continue
		}

		verdict := eligibleHarnessOnly(data)
		if !verdict.Eligible {
			// Not eligible as harness-only — leave byte-identical, no plan item.
			continue
		}

		harnessOnly = append(harnessOnly, HarnessOnlyAgent{
			TargetPath:       targetPath,
			FileName:         name,
			Key:              fileKey,
			NumericID:        verdict.Meta.NumericID,
			Version:          verdict.Meta.Version,
			TransformVersion: verdict.Meta.TransformVersion,
			Role:             verdict.Meta.Role,
		})
	}

	// Sort both slices by TargetPath for determinism.
	sort.Slice(matched, func(i, j int) bool {
		return matched[i].TargetPath < matched[j].TargetPath
	})
	sort.Slice(harnessOnly, func(i, j int) bool {
		return harnessOnly[i].TargetPath < harnessOnly[j].TargetPath
	})

	return WorkspaceAgentScan{
		Matched:     matched,
		HarnessOnly: harnessOnly,
	}
}
