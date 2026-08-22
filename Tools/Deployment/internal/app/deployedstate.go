package app

// deployedstate.go implements the deployed-artifact probe and supporting helpers.
// Every function here is package-internal; the service layer calls them during both the
// deploy-new and update flows to populate plan.Input.DeployedState before calling Build.

import (
	"os"
	"path/filepath"
	"strings"

	"mosaic-common/docformat"
	"mosaic-common/mosaic"
	"mosaic-deploy/internal/agentfields"
	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/manifest"
	"mosaic-deploy/internal/plan"
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
	// Each MOSAIC-only stamp is read via agentfields.ReadOrder, which tries the prefixed name first
	// and falls back to the legacy name. This accepts both already-migrated and legacy-named files.
	if doc, parseErr := docformat.Parse(data); parseErr == nil {
		fm := doc.Frontmatter()
		if fm.Present() {
			state.Version = readDeployedStamp(fm, "version")
			// Read harness version: try mosaic_harness_version first, fall back to
			// mosaic_transform_version (legacy name) for pre-migration files.
			state.HarnessVersion = readDeployedStamp(fm, "harness_version")
			if state.HarnessVersion == "" {
				state.HarnessVersion = readDeployedStamp(fm, "transform_version")
			}
			// OrchestratorInjectionsVersion is intentionally NOT read from frontmatter.
			// Stage 3 redesigns the read side to stop populating this field entirely.
			// Role-conditional comparison is handled in AgentStaleness using InjectionsVersion.
			state.ToolMappingsVersion = readDeployedStamp(fm, "tool_mappings_version")
			state.BundleVersion = readDeployedStamp(fm, "bundle_version")
			if modelKey != "" {
				if v, ok := fm.Get(modelKey); ok && v.Kind == domain.KindScalar {
					state.ModelID = v.Scalar
				}
			}
		}
	}

	// Read injection version from InjectionHarness-class region tag attributes.
	// Falls back to mosaic_injections_version frontmatter for pre-migration files.
	state.InjectionsVersion = extractDeployedInjectionVersion(data, "injections_version")

	// Extract workflow section markers; nil when none are present.
	state.Workflows = extractDeployedWorkflows(data)

	// Extract the protocol version from the deployed <CommunicationProtocol type="managed"> region.
	// Returns "" when the region is absent or carries no version attribute; both are treated as
	// stale by the planner. This call never fails the scan.
	state.ProtocolVersion = extractDeployedProtocolVersion(data)

	return state
}

// readDeployedStamp reads one MOSAIC-only stamp field from a deployed file's frontmatter,
// accepting both the prefixed (mosaic_-prefixed) and legacy (unprefixed) name forms.
// The prefixed name is tried first; if absent, the legacy name is tried. Returns an empty
// string when neither form is present or the value is not a scalar. The legacy name is
// used as the lookup key into the agentfields registry.
func readDeployedStamp(fm *docformat.Frontmatter, legacyKey string) string {
	f, ok := agentfields.ByDeployedName(legacyKey)
	if !ok {
		return ""
	}
	for _, key := range agentfields.ReadOrder(f) {
		if v, ok := fm.Get(key); ok && v.Kind == domain.KindScalar {
			return v.Scalar
		}
	}
	return ""
}

// extractDeployedInjectionVersion parses a deployed agent file and returns the version
// attribute from the first InjectionHarness-class region found. Returns "" when no such
// region exists or when the region carries no version attribute.
//
// Falls back to reading the specified frontmatter field for pre-migration files that still
// carry injection versions in frontmatter rather than tag attributes. The primary path
// (tag read) takes precedence over the fallback.
//
// legacyFrontmatterKey is the agentfields registry key used for the frontmatter fallback
// (e.g., "injections_version"). The primary path reads the tag version from whichever
// InjectionHarness-class region it finds first, since all harness regions in a file share
// the same version value (written by applyHarnessRegion with a single role-selected value).
func extractDeployedInjectionVersion(data []byte, legacyFrontmatterKey string) string {
	if len(data) == 0 {
		return ""
	}
	doc, err := docformat.Parse(data)
	if err != nil {
		return ""
	}

	// Primary path: enumerate all tool-managed regions and find the first InjectionHarness-class one.
	for _, node := range doc.Body().DeployedRegions() {
		class, classErr := docformat.ClassifyRegion(node.Kind(), node.Name())
		if classErr != nil {
			continue // skip unclassified or error regions
		}
		if class == mosaic.InjectionHarness {
			v := node.Version()
			if v != "" {
				return v
			}
			// Found an InjectionHarness region but it has no version attribute — stop primary path.
			break
		}
	}

	// Fallback path: pre-migration files carry the injection version in frontmatter.
	if doc.Frontmatter().Present() {
		return readDeployedStamp(doc.Frontmatter(), legacyFrontmatterKey)
	}
	return ""
}

// probeDeployedHookBundle reports the on-disk state of a hook bundle, whose target path is a
// directory rather than a file. Present is true when <workspace>/<targetPath> exists, is a
// directory, and contains at least one regular file directly within it; false in every other
// case, including an unreadable path, a file at that path, and an empty directory.
//
// ContentHash is manifest.Hash(nil) whenever Present is true, matching the executor's hook
// manifest convention (hook bundles are recorded with a nil content hash), so an untouched
// bundle does not classify as a local modification. Every version field is left empty: a hook
// bundle carries no in-file version marker.
//
// It never returns an error; any I/O failure yields Present: false.
func probeDeployedHookBundle(workspace, targetPath string) domain.DeployedArtifactState {
	fullPath := filepath.Join(workspace, targetPath)

	entries, err := os.ReadDir(fullPath)
	if err != nil {
		// Path absent, unreadable, or is not a directory.
		return domain.DeployedArtifactState{Present: false}
	}

	// A hook bundle is present only when at least one regular file exists directly inside
	// the directory. Subdirectories and other non-regular entries do not count.
	for _, entry := range entries {
		if !entry.IsDir() && entry.Type().IsRegular() {
			return domain.DeployedArtifactState{
				Present:     true,
				ContentHash: manifest.Hash(nil),
				// All version fields left empty: a hook bundle directory carries no in-file
				// version marker. The manifest entry's recorded Version is the only version
				// signal for hooks; classifyHookItem reads it via HookStalenessFromRecorded.
			}
		}
	}

	return domain.DeployedArtifactState{Present: false}
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

// probeDeployedStateWithIndex probes every path in paths, using id-based agent resolution
// when index is non-nil. It is the id-aware successor to probeDeployedState.
//
// When index is nil (the harness does not declare an agents directory), behavior is identical
// to probeDeployedState: every artifact is probed at its harness-computed planned path.
//
// When index is non-nil, agent artifacts whose catalog entry carries a non-empty NumericID
// are located via resolveDeployedPath. The id-resolved path may differ from the planned path
// when the deployed file was renamed after initial deployment. In all cases the state is stored
// under the planned path key so the planner can look it up by its expected path.
//
// Seed entries are always reused verbatim and bypass both name-based and id-based probing;
// this preserves the orchestrator's single-read invariant in the update and workflow-update flows.
//
// Returns an error if resolveDeployedPath encounters two or more deployed files with the same id.
// buildParseFailedPaths extracts the target paths of scan matches that have ParseFailed=true.
// Returns nil when no entries are parse-failed, so the map is only allocated when needed.
// The implementation agent must call this at the Update flow call site.
func buildParseFailedPaths(matched []ScannedAgentMatch) map[string]bool {
	var result map[string]bool
	for _, m := range matched {
		if m.ParseFailed {
			if result == nil {
				result = make(map[string]bool)
			}
			result[m.TargetPath] = true
		}
	}
	return result
}

func probeDeployedStateWithIndex(
	workspace string,
	paths plan.PlannedPaths,
	modelKey string,
	seed map[string]domain.DeployedArtifactState,
	index DeployedAgentIndex,
	agentByKey map[string]domain.Agent,
	parseFailedPaths map[string]bool,
) (map[string]domain.DeployedArtifactState, error) {
	result := make(map[string]domain.DeployedArtifactState, len(paths))

	for _, pp := range paths {
		// Seeded entries are reused without re-reading or re-resolving.
		if seeded, ok := seed[pp.TargetPath]; ok {
			result[pp.TargetPath] = seeded
			continue
		}

		// Hook artifacts deploy to a directory, not a file. Use the directory-oriented probe
		// so that a non-empty hook bundle directory is observed as present. probeDeployedArtifact
		// always yields Present: false for directories and must not be used for hooks.
		if pp.Ref.Kind == domain.ArtifactHook {
			result[pp.TargetPath] = probeDeployedHookBundle(workspace, pp.TargetPath)
			continue
		}

		// Id-based resolution: only for agent artifacts when the index is active.
		if index != nil && pp.Ref.Kind == domain.ArtifactAgent {
			if agent, ok := agentByKey[pp.Ref.Key]; ok && agent.NumericID != "" {
				resolved, err := resolveDeployedPath(agent, index, pp.TargetPath)
				if err != nil {
					return nil, err
				}
				if resolved == "" {
					// Agent has a numeric id but no deployed file matches in the index.
					// Fallback: probe at the planned path only when the scan flagged this path
					// as parse-failed. This catches files that exist on disk but could not be
					// indexed because their frontmatter was unparseable. Files that simply have
					// a different id (non-matching id-based resolution) still yield Present: false.
					if parseFailedPaths[pp.TargetPath] {
						fallback := probeDeployedArtifact(workspace, pp.TargetPath, modelKey)
						if fallback.Present {
							fallback.ParseFailed = true
						}
						result[pp.TargetPath] = fallback
					} else {
						result[pp.TargetPath] = domain.DeployedArtifactState{Present: false}
					}
				} else {
					// Probe at the id-resolved path (may differ from planned path if renamed).
					result[pp.TargetPath] = probeDeployedArtifact(workspace, resolved, modelKey)
				}
				continue
			}
		}

		// Non-agent artifacts, id-less agents, and the no-index case: probe at planned path.
		result[pp.TargetPath] = probeDeployedArtifact(workspace, pp.TargetPath, modelKey)
	}

	return result, nil
}

// extractDeployedWorkflows parses a DEPLOYED agent file for `Workflow:<id>` managed regions
// and returns each distinct workflow in first-occurrence (document) order, paired with the
// version attribute on that region's opening tag.
//
// Deployed workflow blocks are managed regions, never core sections. The deploy transform
// retypes every workflow block to type="managed" before writing (transform.assembleWorkflowBlocks
// via docformat.RetypeOpenTagLine), and the Stage 4 managed-block vocabulary makes
// <Workflow type="managed"> nested in <AvailableWorkflows type="managed"> the canonical
// deployed shape. This function therefore enumerates Body().DeployedRegions() and NOT
// Body().SectionsDeep().
//
// The core form is deliberately NOT recognised in a deployed file: a <Workflow type="core">
// block yields no discovered workflow. Production cannot write that shape, and accepting it
// would let a fixture drift back to a shape no deployed file has. Catalog-side parsing of
// <Workflow type="core"> is a separate concern in internal/catalog and is unaffected.
//
// A region with no version attribute yields an entry with an empty Version. Returns nil when
// no workflow regions are found, when the input is empty, or when the file cannot be parsed.
func extractDeployedWorkflows(data []byte) domain.DeployedWorkflows {
	if len(data) == 0 {
		return nil
	}
	doc, err := docformat.Parse(data)
	if err != nil {
		return nil
	}

	var result domain.DeployedWorkflows
	seen := make(map[string]bool)

	for _, node := range doc.Body().DeployedRegions() {
		name := node.Name()
		if !strings.HasPrefix(name, "Workflow:") {
			continue
		}
		id := name[len("Workflow:"):]
		if !seen[id] {
			seen[id] = true
			result = append(result, domain.DeployedWorkflow{ID: id, Version: node.Version()})
		}
	}

	if len(result) == 0 {
		return nil
	}
	return result
}

// discoverExistingWorkflows returns the workflow IDs embedded in the deployed orchestrator,
// sourced from the probe result. It performs no file access and no planner call.
// Returns nil when the orchestrator file is absent or carries no workflow markers.
func discoverExistingWorkflows(orchestratorState domain.DeployedArtifactState) []string {
	return orchestratorState.Workflows.IDs()
}
