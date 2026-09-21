package app

// workspace_agent_scan.go declares the Stage 4 types for workspace-scan-driven agent set
// derivation and implements scanWorkspaceAgents, the single-pass classifier that decides
// Update's agent-set membership.
//
// Update uses the workspace scan — not workflow discovery — to determine which deployed
// agents to staleness-check. Every file matching the harness's declared agent extension
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
	// MatchByFileNameKeyParseFailed indicates the file's frontmatter could not be parsed,
	// but the filename matched a catalog agent key. The file is identifiable as MOSAIC-managed
	// despite the parse failure. NumericID is empty when this is the match kind.
	MatchByFileNameKeyParseFailed AgentMatchKind = "filename-key-parse-failed"
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
	// NumericID is always "" when ParseFailed is true (the id could not be extracted).
	NumericID string
	// MatchedBy records which mechanism resolved the match. Diagnostic and test-facing; it
	// does not alter downstream behaviour.
	MatchedBy AgentMatchKind
	// ParseFailed is true when the file was matched by filename-key after a parse failure.
	// When true, MatchedBy is MatchByFileNameKeyParseFailed and NumericID is empty.
	ParseFailed bool
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
//  2. The key derived from the file name (harness-declared extension stripped, then
//     ".agent.md" or ".md" stripped as fallback), looked up through catalog.Agent. This is
//     the necessary fallback for agents with no numeric id, notably both orchestrator-role
//     files.
//
// An unmatched file is then tested for harness-only eligibility (two-signal check) using
// the decoded canonical bytes. isOrchestratorFileName gates the harness-only path only —
// it must not gate the matched path: an orchestrator-role file that resolves via a catalog
// lookup is reported as Matched.
//
// src is the source harness descriptor. It supplies the declared agent file extension
// (src.Extensions[domain.ArtifactAgent]) for the file filter and the decoder for
// producing canonical bytes. A nil or zero-value src is treated as a Markdown harness
// (extension ".md", identity decode).
//
// Tolerance is the contract: a file that cannot be read, cannot be decoded, carries no
// frontmatter, does not match the declared extension, or is a directory is skipped silently.
//
// An empty agentsDir yields an empty scan without touching the workspace root, and an
// unreadable or absent directory yields an empty scan rather than an error.
//
// Determinism: both returned slices are sorted by TargetPath. MatchedKeys is sorted and
// deduplicated by key.
// scanWorkspaceAgents performs ONE non-recursive walk of the harness's deployed-agents
// directory and classifies every file matching the declared extension. The second return
// value carries any non-fatal decode notices accumulated across all decoded files (via
// decodeReportNotices), ready for callers to emit through the run log.
func scanWorkspaceAgents(workspace, agentsDir string, c catalog.Catalog, src *domain.HarnessDescriptor) (WorkspaceAgentScan, []string) {
	// Guard: an empty agentsDir must yield an empty result without scanning workspace root.
	if agentsDir == "" {
		return WorkspaceAgentScan{}, nil
	}
	var notices []string

	// Determine the file extension to filter and strip. Default to ".md" when undeclared.
	agentExt := ".md"
	if src != nil {
		if ext, ok := src.Extensions[domain.ArtifactAgent]; ok && ext != "" {
			agentExt = ext
		}
	}

	dir := filepath.Join(workspace, agentsDir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		// Directory does not exist or is unreadable — return empty result.
		return WorkspaceAgentScan{}, notices
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

		// Only files with the declared extension.
		if !strings.HasSuffix(name, agentExt) {
			continue
		}

		// Read file bytes tolerantly — skip silently on error.
		filePath := filepath.Join(dir, name)
		rawData, err := os.ReadFile(filePath)
		if err != nil {
			continue
		}

		// Derive the agent key from the file name using the generalised agentKeyFromFileName,
		// which strips the declared extension and its ".agent.<suffix>" variant.
		fileKey := agentKeyFromFileName(name, agentExt)

		// Decode through the funnel. Undecodable files trigger a filename-key fallback
		// before skipping, mirroring the parse-failure path for Markdown files.
		dr := decodeDeployedBytes(rawData, domain.ArtifactAgent, src)
		// Surface the decode report at the scan boundary so that entries such as
		// EntryMissingInstructions from body-less Codex agents reach the caller for logging.
		notices = append(notices, decodeReportNotices(name, dr.Report)...)
		if dr.DecodeErr != nil || dr.Canonical == nil {
			// Decode failed — attempt filename-key fallback before skipping.
			if agent, ok := c.Agent(fileKey); ok {
				targetPath := filepath.Join(agentsDir, name)
				matched = append(matched, ScannedAgentMatch{
					TargetPath:  targetPath,
					FileName:    name,
					AgentKey:    agent.Key,
					NumericID:   "",
					MatchedBy:   MatchByFileNameKeyParseFailed,
					ParseFailed: true,
				})
			}
			continue
		}

		canonical := dr.Canonical

		// Parse frontmatter from canonical bytes. Parse failure triggers filename-key fallback.
		doc, parseErr := ParseGenericSource(canonical)
		if parseErr != nil {
			// Parse failed — attempt filename-key fallback before skipping.
			if agent, ok := c.Agent(fileKey); ok {
				targetPath := filepath.Join(agentsDir, name)
				matched = append(matched, ScannedAgentMatch{
					TargetPath:  targetPath,
					FileName:    name,
					AgentKey:    agent.Key,
					NumericID:   "",
					MatchedBy:   MatchByFileNameKeyParseFailed,
					ParseFailed: true,
				})
			}
			continue
		}

		fm := doc.Frontmatter()
		if !fm.Present() {
			// No frontmatter block — skip silently.
			continue
		}

		targetPath := filepath.Join(agentsDir, name)

		// Read the numeric ID from canonical frontmatter, accepting prefixed and legacy forms.
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
		// existing scanHarnessOnlyAgents contract. For non-Markdown harnesses, orchestrator
		// detection uses the catalog-matched path (step 1 and 2 above), so this guard
		// covers only the rare case where a file's name matches an orchestrator pattern.
		if isOrchestratorFileName(name, src) {
			continue
		}

		// Eligibility check on canonical bytes: the two-signal check works on Markdown form.
		verdict := eligibleHarnessOnly(canonical)
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
	}, notices
}
