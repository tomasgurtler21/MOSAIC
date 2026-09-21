package app

// deployed_agent_index.go defines the workspace-scan types and the per-agent resolution
// logic introduced by ID-Based Agent Identification.
//
// buildDeployedAgentIndex scans the deployed-agents directory once per run and
// returns an id-keyed index. resolveDeployedPath uses that index to locate an
// existing deployed artifact for a given catalog agent.

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"mosaic-deploy/internal/agentfields"
	"mosaic-deploy/internal/domain"
)

// ErrAmbiguousDeployedID reports two or more deployed files declaring the same agent id.
// The wrapping error names the id and every colliding path. The run must not silently pick
// one; it must surface the collision and leave the workspace unchanged.
var ErrAmbiguousDeployedID = errors.New("two deployed files declare the same agent id")

// DeployedAgentEntry is one deployed file the workspace scan matched to a numeric id.
type DeployedAgentEntry struct {
	// NumericID is the frontmatter `id` scalar, verbatim as written in the deployed file.
	NumericID string
	// TargetPath is the file's path relative to the deployment root, in the same format that
	// probeDeployedArtifact expects — i.e. filepath.Join(agentsDir, filename).
	TargetPath string
}

// DeployedAgentIndex maps a numeric agent id to every deployed file declaring it. A slice
// rather than a single path so that a collision is reportable rather than silently resolved.
type DeployedAgentIndex map[string][]DeployedAgentEntry

// Lookup returns the entries for id, in walk order.
//   - Zero entries: the agent is not deployed in this workspace.
//   - One entry:    the agent is deployed at entries[0].TargetPath.
//   - Two or more:  the workspace is ambiguous for this id; the caller must report ErrAmbiguousDeployedID.
func (i DeployedAgentIndex) Lookup(id string) []DeployedAgentEntry {
	return i[id]
}

// buildDeployedAgentIndex walks the workspace's deployed-agent directory (agentsDir, relative
// to workspace), decodes each file through the funnel, and indexes files by their `id` scalar.
//
// Tolerance is the contract: a file that cannot be read, cannot be decoded, cannot be parsed,
// carries no frontmatter, or carries no `id` is skipped silently. This is target-finding, not
// validation — a file that cannot be matched is simply not one of ours.
//
// src is the source harness descriptor used to identify the agent file extension
// (src.Extensions[domain.ArtifactAgent]) and to select the decoder. A nil or zero-value
// src is treated as a Markdown identity harness. Only files whose names end with the
// declared extension are indexed; when no extension is declared, only ".md" files are indexed.
//
// The walk is non-recursive over the single agents directory. Scope limitation: harness
// extension directories (additional locations a harness may declare for deployed agents
// beyond its primary agents directory) are not walked; the harness extension mechanism
// is out of scope for this implementation.
//
// It is called once per run and its result is threaded through the flows so the directory
// is not re-walked per agent.
// buildDeployedAgentIndex walks the workspace's deployed-agent directory (agentsDir, relative
// to workspace), decodes each file through the funnel, and indexes files by their `id` scalar.
// The second return value carries any non-fatal decode notices accumulated across all indexed
// files (via decodeReportNotices), ready for callers to emit through the run log.
func buildDeployedAgentIndex(workspace, agentsDir string, src *domain.HarnessDescriptor) (DeployedAgentIndex, []string) {
	idx := make(DeployedAgentIndex)
	var notices []string

	// Determine the file extension to filter by. Default to ".md" when the harness declares none.
	agentExt := ".md"
	if src != nil {
		if ext, ok := src.Extensions[domain.ArtifactAgent]; ok && ext != "" {
			agentExt = ext
		}
	}

	dir := filepath.Join(workspace, agentsDir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		// Directory does not exist or is unreadable — return empty index.
		return idx, notices
	}

	idField, _ := agentfields.ByGeneric("id")

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, agentExt) {
			continue
		}

		filePath := filepath.Join(dir, name)
		data, err := os.ReadFile(filePath)
		if err != nil {
			// Unreadable — skip silently.
			continue
		}

		// Decode through the funnel so Codex TOML and other non-Markdown formats are read
		// in canonical form. A decode failure is treated as unparseable — skip silently.
		dr := decodeDeployedBytes(data, domain.ArtifactAgent, src)
		if dr.DecodeErr != nil || dr.Canonical == nil {
			continue
		}
		// Surface the decode report at the index-build boundary so that entries such as
		// EntryMissingInstructions from body-less Codex agents reach the caller for logging.
		notices = append(notices, decodeReportNotices(name, dr.Report)...)

		// Parse frontmatter from canonical (Markdown) bytes.
		doc, parseErr := ParseGenericSource(dr.Canonical)
		if parseErr != nil {
			// Unparseable — skip silently.
			continue
		}

		fm := doc.Frontmatter()
		if !fm.Present() {
			// No frontmatter — skip silently.
			continue
		}

		// Read the numeric ID from the decoded frontmatter, accepting both the prefixed
		// (mosaic_-prefixed) and legacy (unprefixed) forms via agentfields.ReadOrder.
		var numericID string
		for _, key := range agentfields.ReadOrder(idField) {
			if v, ok := fm.Get(key); ok && v.Kind == domain.KindScalar && v.Scalar != "" {
				numericID = v.Scalar
				break
			}
		}
		if numericID == "" {
			// No id field or empty value — skip silently.
			continue
		}
		targetPath := filepath.Join(agentsDir, name)
		idx[numericID] = append(idx[numericID], DeployedAgentEntry{
			NumericID:  numericID,
			TargetPath: targetPath,
		})
	}

	return idx, notices
}

// resolveDeployedPath returns the target path at which to look for an existing deployed
// artifact. It implements the resolution order for locating an existing deployed file:
//
//  1. If agent has a non-empty NumericID, the id index is consulted via index.Lookup(id):
//     - exactly one entry → return that entry's TargetPath and nil error.
//     - zero entries      → return "", nil (agent is not deployed; no name-based fallback).
//     - two or more       → return "", ErrAmbiguousDeployedID (never pick silently).
//
//  2. If agent has an empty NumericID (orchestrator, utility, third-party), fallbackPath is
//     returned unconditionally. The harness-computed path is authoritative for these agents.
//
// Writing a new file always uses the harness-computed TargetPath; ID matching governs
// finding an existing artifact, never placement.
func resolveDeployedPath(agent domain.Agent, index DeployedAgentIndex, fallbackPath string) (string, error) {
	if agent.NumericID == "" {
		// No numeric id — use the harness-computed fallback path unconditionally.
		return fallbackPath, nil
	}

	entries := index.Lookup(agent.NumericID)
	switch len(entries) {
	case 0:
		// Not deployed — return empty path, no error, no name-based fallback.
		return "", nil
	case 1:
		return entries[0].TargetPath, nil
	default:
		// Ambiguous — two or more deployed files declare this id.
		paths := make([]string, len(entries))
		for i, e := range entries {
			paths[i] = e.TargetPath
		}
		return "", fmt.Errorf("agent id %q: %w (paths: %v)", agent.NumericID, ErrAmbiguousDeployedID, paths)
	}
}
