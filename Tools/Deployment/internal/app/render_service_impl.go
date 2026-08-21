package app

// render_service_impl.go implements Service.RenderAgent: the single-agent render mode
// that takes one generic-form MOSAIC agent file at an arbitrary path and renders it into
// one target harness's deployed form at an explicit destination path.
//
// Implementation tasks covered here:
//   I1.2 — Service method wired to renderAgent.
//   I1.3 — Source reading from an arbitrary path, frontmatter parsing, key/role/version.
//   I1.4 — Harness resolution, protocol/bundle loading, transform.Request assembly.
//   I1.5 — Caller-specified workflow and infrastructure-agent selection.
//   I1.6 — Destination writing with parent-directory creation, overwrite protection, dry-run.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"mosaic-common/docformat"
	"mosaic-deploy/internal/config"
	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/transform"
)

// renderAgent is the real implementation of Service.RenderAgent.
//
// Validation order (so the caller gets the most actionable message and nothing is read or
// written before the request is known to be coherent):
//   source arity → destination arity → harness required → harness resolvable →
//   source exists/resolvable → source parseable → role/version derived →
//   workflow applicability → workflow ids resolvable →
//   destination resolution → overwrite check → render → write.
func renderAgent(_ context.Context, s *service, req RenderAgentRequest) (RenderAgentResult, error) {
	// -----------------------------------------------------------------------
	// 1. Source arity
	// -----------------------------------------------------------------------
	if req.SourcePath == "" && req.SourceAgentKey == "" {
		return RenderAgentResult{}, ErrRenderSourceRequired
	}
	if req.SourcePath != "" && req.SourceAgentKey != "" {
		return RenderAgentResult{}, ErrRenderSourceAmbiguous
	}

	// -----------------------------------------------------------------------
	// 2. Destination arity
	// -----------------------------------------------------------------------
	if req.DestinationPath == "" && req.WorkspaceRoot == "" {
		return RenderAgentResult{}, ErrRenderDestinationRequired
	}
	if req.DestinationPath != "" && req.WorkspaceRoot != "" {
		return RenderAgentResult{}, ErrRenderDestinationAmbiguous
	}

	// -----------------------------------------------------------------------
	// 3. Harness required
	// -----------------------------------------------------------------------
	if req.TargetHarnessID == "" {
		return RenderAgentResult{}, ErrRenderHarnessRequired
	}

	// -----------------------------------------------------------------------
	// 4. Harness resolvable
	// -----------------------------------------------------------------------
	module, err := s.deps.Registry.Resolve(req.TargetHarnessID)
	if err != nil {
		return RenderAgentResult{}, fmt.Errorf("%w: %v", ErrRenderHarnessUnresolvable, err)
	}
	defer module.Close()

	// -----------------------------------------------------------------------
	// 5. Source exists / resolvable
	// -----------------------------------------------------------------------
	var srcBytes []byte
	var sourcePath string
	var sourceAgentKey string
	var agentKey string

	if req.SourcePath != "" {
		fi, statErr := os.Stat(req.SourcePath)
		if statErr != nil {
			return RenderAgentResult{}, fmt.Errorf("%w: %s", ErrRenderSourceNotFound, req.SourcePath)
		}
		if !fi.Mode().IsRegular() {
			return RenderAgentResult{}, fmt.Errorf("%w: %s", ErrRenderSourceNotFile, req.SourcePath)
		}

		srcBytes, err = os.ReadFile(req.SourcePath)
		if err != nil {
			return RenderAgentResult{}, fmt.Errorf("%w: %s: %v", ErrRenderSourceNotFound, req.SourcePath, err)
		}

		sourcePath = req.SourcePath
		// Agent key: base name without extension (strips .md or any other extension).
		base := filepath.Base(req.SourcePath)
		agentKey = strings.TrimSuffix(base, filepath.Ext(base))
	} else {
		// Source from catalogue.
		agent, ok := s.deps.Catalog.Agent(req.SourceAgentKey)
		if !ok {
			return RenderAgentResult{}, fmt.Errorf("%w: %q", ErrRenderAgentNotInCatalog, req.SourceAgentKey)
		}
		srcBytes, err = s.deps.Catalog.ReadSource(agent.SourcePath)
		if err != nil {
			return RenderAgentResult{}, fmt.Errorf("cannot read catalogue source for %q: %w", req.SourceAgentKey, err)
		}
		sourcePath = agent.SourcePath
		sourceAgentKey = req.SourceAgentKey
		agentKey = req.SourceAgentKey
	}

	// -----------------------------------------------------------------------
	// 6. Source parseable
	// -----------------------------------------------------------------------
	doc, parseErr := docformat.Parse(srcBytes)
	if parseErr != nil {
		return RenderAgentResult{}, fmt.Errorf("%w: %v", ErrRenderSourceNotGeneric, parseErr)
	}
	fm := doc.Frontmatter()

	// -----------------------------------------------------------------------
	// 7. Role and version from frontmatter
	// -----------------------------------------------------------------------
	role := domain.RoleSubagent // default when `role` is absent
	if v, ok := fm.Get("role"); ok && v.Kind == domain.KindScalar {
		parsed, valid := domain.ParseAgentRole(v.Scalar)
		if !valid {
			return RenderAgentResult{}, fmt.Errorf("%w: %q", ErrRenderInvalidRole, v.Scalar)
		}
		role = parsed
	}

	var sourceVersion string
	if v, ok := fm.Get("version"); ok && v.Kind == domain.KindScalar {
		sourceVersion = v.Scalar
	}

	// -----------------------------------------------------------------------
	// 8. Workflow applicability
	//
	// A non-nil non-empty Workflows or InfrastructureAgents slice for a non-orchestrator is
	// an error. A non-nil but empty slice ("explicitly none") is always legal.
	// -----------------------------------------------------------------------
	if role != domain.RoleOrchestrator {
		if len(req.Workflows) > 0 {
			return RenderAgentResult{}, ErrRenderWorkflowsNotApplicable
		}
		if len(req.InfrastructureAgents) > 0 {
			return RenderAgentResult{}, ErrRenderWorkflowsNotApplicable
		}
	}

	// -----------------------------------------------------------------------
	// 9. Workflow IDs resolvable
	// -----------------------------------------------------------------------
	var wfBlocks []transform.WorkflowBlock
	switch {
	case req.Workflows == nil && role == domain.RoleOrchestrator:
		// nil Workflows for an orchestrator: inject the full catalog workflow set as the
		// default. This produces output that is byte-distinguishable from []string{}
		// ("explicitly none"), which clears the AvailableWorkflows region via an empty
		// wfBlocks slice. Both are valid; neither silently collapses to the other.
		for _, wf := range s.deps.Catalog.Workflows() {
			section, sectErr := s.deps.Catalog.WorkflowSection(wf.ID)
			if sectErr != nil {
				// A missing section is unusual but not fatal here: skip rather than
				// refusing to render an otherwise-valid orchestrator.
				continue
			}
			wfBlocks = append(wfBlocks, transform.WorkflowBlock{ID: wf.ID, Block: section})
		}
	case req.Workflows != nil:
		// non-nil (even empty) = "explicitly specified" — validate and assemble exactly
		// those ids. An empty slice clears the AvailableWorkflows region.
		for _, id := range req.Workflows {
			if _, exists := s.deps.Catalog.Workflow(id); !exists {
				return RenderAgentResult{}, fmt.Errorf("%w: %q", ErrRenderUnknownWorkflow, id)
			}
			section, sectErr := s.deps.Catalog.WorkflowSection(id)
			if sectErr != nil {
				return RenderAgentResult{}, fmt.Errorf("cannot load workflow section %q: %w", id, sectErr)
			}
			wfBlocks = append(wfBlocks, transform.WorkflowBlock{ID: id, Block: section})
		}
	// else: req.Workflows == nil && role != RoleOrchestrator — wfBlocks stays nil;
	// the source has no AvailableWorkflows region so no workflow handling is needed.
	}

	// Assemble infrastructure blocks. nil InfrastructureAgents = not specified.
	var infraBlocks []transform.InfrastructureBlock
	if req.InfrastructureAgents != nil {
		infraBlocks = s.buildInfrastructureBlocks(req.InfrastructureAgents)
	}

	// -----------------------------------------------------------------------
	// 10. Destination resolution
	// -----------------------------------------------------------------------
	var destPath string
	var destResolvedBy string

	if req.DestinationPath != "" {
		destPath = req.DestinationPath
		destResolvedBy = "explicit"
	} else {
		// Ask the harness module to resolve a project-scope path for this agent key, then
		// join it onto the workspace root supplied by the caller.
		relPath, tgtErr := module.TargetPath(domain.TargetPathRequest{
			Kind:  domain.ArtifactAgent,
			Key:   agentKey,
			Scope: domain.ScopeProject,
		})
		if tgtErr != nil {
			return RenderAgentResult{}, fmt.Errorf("cannot resolve destination path for agent %q: %w", agentKey, tgtErr)
		}
		if filepath.IsAbs(relPath) {
			destPath = relPath
		} else {
			destPath = filepath.Join(req.WorkspaceRoot, relPath)
		}
		destResolvedBy = "workspace"
	}

	// -----------------------------------------------------------------------
	// 11. Overwrite check (runs under DryRun too, so a preview reports what a real run
	//     would refuse).
	// -----------------------------------------------------------------------
	if _, existErr := os.Stat(destPath); existErr == nil {
		if !req.Overwrite {
			return RenderAgentResult{}, fmt.Errorf("%w: %s", ErrRenderDestinationExists, destPath)
		}
	}

	// -----------------------------------------------------------------------
	// 12. Load protocol and bundle
	// -----------------------------------------------------------------------
	protocol, protoErr := s.loadProtocol()
	if protoErr != nil {
		return RenderAgentResult{}, protoErr
	}
	bundle, bundleErr := s.loadBundle()
	if bundleErr != nil {
		return RenderAgentResult{}, bundleErr
	}

	// -----------------------------------------------------------------------
	// 13. Tool-mappings version hash
	// -----------------------------------------------------------------------
	toolCfg, _ := s.deps.ToolConfig.Load()
	userCfg, userCfgErr := s.deps.UserConfig.Load()
	if userCfgErr != nil {
		return RenderAgentResult{}, userCfgErr
	}
	toolMappingsVersion := config.HashToolDestinations(toolCfg.ToolDestinations, userCfg.ToolDestinations)

	// -----------------------------------------------------------------------
	// 14. Render via transform.Apply (the sole transformation entry point)
	// -----------------------------------------------------------------------
	desc := module.Descriptor()
	transformResult, transformErr := transform.Apply(transform.Request{
		Source:                        srcBytes,
		Kind:                          domain.ArtifactAgent,
		Key:                           agentKey,
		Module:                        module,
		Model:                         domain.ModelSelection{ModelID: req.TargetModel},
		Scope:                         domain.ScopeProject,
		Role:                          role,
		Protocol:                      protocol,
		Bundle:                        bundle,
		Workflows:                     wfBlocks,
		InfrastructureAgents:          infraBlocks,
		ToolMappingsVersion:           toolMappingsVersion,
		InjectionsVersion:             desc.InjectionsVersion,
		OrchestratorInjectionsVersion: desc.OrchestratorInjectionsVersion,
		// Deployed is nil: there is no prior deployed file at a fresh render destination.
	})
	if transformErr != nil {
		return RenderAgentResult{}, fmt.Errorf("transform failed: %w", transformErr)
	}

	// -----------------------------------------------------------------------
	// 15. Write (or dry-run)
	// -----------------------------------------------------------------------
	var createdDirs []string
	if !req.DryRun {
		parentDir := filepath.Dir(destPath)
		createdDirs = newDirectoriesNeeded(parentDir)
		if mkErr := os.MkdirAll(parentDir, 0o755); mkErr != nil {
			return RenderAgentResult{}, fmt.Errorf("cannot create destination directory %q: %w", parentDir, mkErr)
		}
		if writeErr := os.WriteFile(destPath, transformResult.Output, 0o644); writeErr != nil {
			return RenderAgentResult{}, fmt.Errorf("cannot write destination %q: %w", destPath, writeErr)
		}
	}

	// Collect harness tool names from the transform report.
	var tools []string
	for _, res := range transformResult.Report.Tools {
		if res.Outcome == domain.ToolMapped {
			tools = append(tools, res.HarnessTools...)
		}
	}

	// Workflow and infrastructure-agent keys from the transform report.
	var resultWorkflows []string
	if len(transformResult.Report.Workflows) > 0 {
		resultWorkflows = transformResult.Report.Workflows
	}
	var resultInfraAgents []string
	if len(transformResult.Report.InfrastructureAgents) > 0 {
		resultInfraAgents = transformResult.Report.InfrastructureAgents
	}

	return RenderAgentResult{
		SourcePath:            sourcePath,
		SourceAgentKey:        sourceAgentKey,
		AgentKey:              agentKey,
		SourceVersion:         sourceVersion,
		Role:                  string(role),
		TargetHarnessID:       req.TargetHarnessID,
		TargetModel:           req.TargetModel,
		DestinationPath:       destPath,
		DestinationResolvedBy: destResolvedBy,
		CreatedDirectories:    createdDirs,
		Workflows:             resultWorkflows,
		InfrastructureAgents:  resultInfraAgents,
		Tools:                 tools,
		DryRun:                req.DryRun,
	}, nil
}

// newDirectoriesNeeded walks from parentDir upward and returns the directories that do not
// yet exist on the filesystem, outermost first. This list is recorded so a teardown ledger
// can remove exactly what this run created.
func newDirectoriesNeeded(parentDir string) []string {
	// Collect dirs that do not exist, innermost first, then reverse to outermost first.
	var missing []string
	dir := parentDir
	for {
		if _, statErr := os.Stat(dir); statErr == nil {
			break // this directory already exists; stop walking up
		}
		missing = append(missing, dir)
		parent := filepath.Dir(dir)
		if parent == dir {
			break // reached filesystem root
		}
		dir = parent
	}

	// Reverse so the result is outermost-first (the order callers expect for teardown).
	for i, j := 0, len(missing)-1; i < j; i, j = i+1, j-1 {
		missing[i], missing[j] = missing[j], missing[i]
	}
	return missing
}
