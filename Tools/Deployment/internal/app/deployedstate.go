package app

// deployedstate.go implements the deployed-artifact probe and supporting helpers.
// Every function here is package-internal; the service layer calls them during both the
// deploy-new and update flows to populate plan.Input.DeployedState before calling Build.

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"

	"mosaic-common/docformat"
	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/manifest"
	"mosaic-deploy/internal/plan"
)

var (
	// workflowOpeningPattern matches the opening tag of a workflow section block.
	workflowOpeningPattern = regexp.MustCompile(`\[\[SECTION:Workflow:([^\]]+)\]\]`)
	// workflowVersionPattern matches a workflow-version HTML comment inside a block.
	workflowVersionPattern = regexp.MustCompile(`<!--\s*workflow-version:\s*(\S+)\s*-->`)
)

// probeDeployedArtifact reads a single deployed artifact at <workspace>/<targetPath> and
// reports its state. modelKey is the harness's model frontmatter key
// (Descriptor().Frontmatter.ModelKey); an empty modelKey means the harness emits no model
// and yields an empty ModelID. It never returns an error: any failure to read or parse yields
// a state whose Present is false (unreadable/absent) or whose scalar fields are empty (present
// but unparseable). Directories at the target path also yield Present: false.
func probeDeployedArtifact(workspace, targetPath, modelKey string) domain.DeployedArtifactState {
	fullPath := filepath.Join(workspace, targetPath)

	data, err := os.ReadFile(fullPath)
	if err != nil {
		// File absent, unreadable, or is a directory (ReadFile fails for directories).
		return domain.DeployedArtifactState{Present: false}
	}

	state := domain.DeployedArtifactState{
		Present:     true,
		ContentHash: manifest.Hash(data),
	}

	// Parse frontmatter for version stamps and model ID. Failure leaves scalar fields empty (graceful degradation).
	if doc, parseErr := docformat.Parse(data); parseErr == nil {
		fm := doc.Frontmatter()
		if fm.Present() {
			if v, ok := fm.Get("version"); ok && v.Kind == domain.KindScalar {
				state.Version = v.Scalar
			}
			if v, ok := fm.Get("transform_version"); ok && v.Kind == domain.KindScalar {
				state.TransformVersion = v.Scalar
			}
			if v, ok := fm.Get("injections_version"); ok && v.Kind == domain.KindScalar {
				state.InjectionsVersion = v.Scalar
			}
			if v, ok := fm.Get("orchestrator_injections_version"); ok && v.Kind == domain.KindScalar {
				state.OrchestratorInjectionsVersion = v.Scalar
			}
			if v, ok := fm.Get("tool_mappings_version"); ok && v.Kind == domain.KindScalar {
				state.ToolMappingsVersion = v.Scalar
			}
			if modelKey != "" {
				if v, ok := fm.Get(modelKey); ok && v.Kind == domain.KindScalar {
					state.ModelID = v.Scalar
				}
			}
		}
	}

	// Extract workflow section markers; nil when none are present.
	state.Workflows = extractDeployedWorkflows(data)

	// Extract the protocol version from the deployed [[DEPLOYED:CommunicationProtocol]] region.
	// Returns "" when the region is absent or carries no version comment; both are treated as
	// stale by the planner. This call never fails the scan.
	state.ProtocolVersion = extractDeployedProtocolVersion(data)

	return state
}

// probeDeployedState probes every path in paths, passing modelKey to each per-artifact probe.
// Entries already present in seed are reused verbatim and not re-read, so a path probed
// earlier in the flow (the orchestrator, for workflow discovery) is read exactly once per run.
// seed may be nil. The returned map contains one entry per path in paths, including absent ones.
func probeDeployedState(
	workspace string,
	paths plan.PlannedPaths,
	modelKey string,
	seed map[string]domain.DeployedArtifactState,
) map[string]domain.DeployedArtifactState {
	result := make(map[string]domain.DeployedArtifactState, len(paths))

	for _, pp := range paths {
		if seeded, ok := seed[pp.TargetPath]; ok {
			result[pp.TargetPath] = seeded
			continue
		}
		result[pp.TargetPath] = probeDeployedArtifact(workspace, pp.TargetPath, modelKey)
	}

	return result
}

// extractDeployedWorkflows scans deployed content for [[SECTION:Workflow:<id>]] blocks and
// returns each distinct workflow in first-occurrence order, paired with the version found in
// that block's <!-- workflow-version: X --> comment. A block with no version comment yields
// an entry with an empty Version field. Returns nil when no workflow section markers are found.
func extractDeployedWorkflows(data []byte) domain.DeployedWorkflows {
	openings := workflowOpeningPattern.FindAllSubmatchIndex(data, -1)
	if len(openings) == 0 {
		return nil
	}

	var result domain.DeployedWorkflows
	seen := make(map[string]bool, len(openings))

	for i, match := range openings {
		id := string(data[match[2]:match[3]])

		// Determine this block's content boundary: from just after the opening tag to either
		// the matching closing tag, the next opening tag, or end of content.
		blockStart := match[1] // byte offset just after the opening tag

		closingMarker := []byte("[[/SECTION:Workflow:" + id + "]]")
		var blockEnd int
		if idx := bytes.Index(data[blockStart:], closingMarker); idx >= 0 {
			blockEnd = blockStart + idx
		} else if i+1 < len(openings) {
			// No closing tag found; use the start of the next opening marker.
			blockEnd = openings[i+1][0]
		} else {
			// No closing tag and no next block; use end of content.
			blockEnd = len(data)
		}

		// Find the first workflow-version comment inside this block's content.
		version := ""
		if vm := workflowVersionPattern.FindSubmatch(data[blockStart:blockEnd]); vm != nil {
			version = string(vm[1])
		}

		if !seen[id] {
			seen[id] = true
			result = append(result, domain.DeployedWorkflow{ID: id, Version: version})
		}
	}

	return result
}

// discoverExistingWorkflows returns the workflow IDs embedded in the deployed orchestrator,
// sourced from the probe result. It performs no file access and no planner call.
// Returns nil when the orchestrator file is absent or carries no workflow markers.
func discoverExistingWorkflows(orchestratorState domain.DeployedArtifactState) []string {
	return orchestratorState.Workflows.IDs()
}
