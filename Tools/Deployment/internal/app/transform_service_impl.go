package app

// transform_service_impl.go implements the harness-to-harness transform service flow:
// path resolution, non-recursive directory enumeration filtered by the source harness
// extension, a pre-pass that reads every source file once and discovers the distinct
// source-model set, per-source-model model questioning, per-file detection/transform,
// overwrite protection, interactive question routing per the CD-6 pre-answer convention,
// and whole-run vs per-file error handling.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"mosaic-deploy/internal/config"
	"mosaic-deploy/internal/domain"
)

// titleForSourceModel returns a user-facing Title for the per-source-model QTransformTargetModel
// question. The title identifies which source model is being mapped so the user knows which group
// they are answering for.
func titleForSourceModel(sourceModel string) string {
	if sourceModel == UnsetSourceModel {
		return "Select target model for agents with no model set (optional, enter to skip)"
	}
	return fmt.Sprintf("Select target model for source model %q (optional, enter to skip)", sourceModel)
}

// transformHarness is the real implementation of Service.TransformHarness.
func transformHarness(ctx context.Context, s *service, req TransformHarnessRequest) (TransformHarnessResult, error) {
	// Whole-run error: same source and target harness (pre-answered).
	if req.SourceHarnessID != "" && req.TargetHarnessID != "" && req.SourceHarnessID == req.TargetHarnessID {
		return TransformHarnessResult{}, ErrTransformSameHarness
	}

	// Resolve source harness module. Whole-run error if it cannot be resolved.
	srcModule, err := s.deps.Registry.Resolve(req.SourceHarnessID)
	if err != nil {
		return TransformHarnessResult{}, fmt.Errorf("cannot resolve source harness %q: %w", req.SourceHarnessID, err)
	}
	defer srcModule.Close()

	// Ask for target harness when not pre-answered (CD-6 pre-answer convention).
	targetHarnessID := req.TargetHarnessID
	if targetHarnessID == "" {
		// Build one option per registry harness, mirroring askHarness in resolve.go.
		// The source harness is included but disabled so the overlay is never empty even
		// on a single-harness registry; source-role reason takes precedence over the
		// harness's own unusable reason when both apply.
		opts := make([]domain.Option, 0, len(s.deps.Registry.List()))
		for _, h := range s.deps.Registry.List() {
			var disabled bool
			var disabledReason string
			if h.ID == req.SourceHarnessID {
				disabled = true
				disabledReason = "already the transform source"
			} else if !h.Usable {
				disabled = true
				disabledReason = h.UnusableReason
			}
			opts = append(opts, domain.Option{
				ID:             h.ID,
				Label:          h.DisplayName,
				Disabled:       disabled,
				DisabledReason: disabledReason,
			})
		}
		ans, askErr := s.deps.Interaction.SelectOne(ctx, domain.ChoiceQuestion{
			Question: domain.Question{
				ID:    domain.QTransformTargetHarness,
				Title: "Select target harness",
			},
			Options: opts,
		})
		if askErr != nil {
			return TransformHarnessResult{}, fmt.Errorf("target harness question failed: %w", askErr)
		}
		if ans.Status != domain.Answered {
			return TransformHarnessResult{}, fmt.Errorf("target harness selection cancelled")
		}
		targetHarnessID = ans.OptionID
	}

	// Whole-run error: same source and target harness (one answered interactively).
	if targetHarnessID == req.SourceHarnessID {
		return TransformHarnessResult{}, ErrTransformSameHarness
	}

	// Resolve target harness module. Whole-run error if it cannot be resolved.
	tgtModule, err := s.deps.Registry.Resolve(targetHarnessID)
	if err != nil {
		return TransformHarnessResult{}, fmt.Errorf("cannot resolve target harness %q: %w", targetHarnessID, err)
	}
	defer tgtModule.Close()

	// Compute the tool-mappings version hash so the written stamp can be used by a later
	// update run to detect when the target harness's tool-destination configuration changed.
	// A malformed or unreadable user config is a whole-run failure: the hash would be silently
	// wrong, defeating the staleness detection it is meant to support. ToolConfig errors are
	// ignored by convention (consistent with deploy.go and update.go).
	toolCfg, _ := s.deps.ToolConfig.Load()
	userCfg, err := s.deps.UserConfig.Load()
	if err != nil {
		return TransformHarnessResult{}, err
	}
	toolMappingsVersion := config.HashToolDestinations(toolCfg.ToolDestinations, userCfg.ToolDestinations)

	// Whole-run stat of the input path.
	pathInfo, statErr := os.Stat(req.Path)
	if statErr != nil {
		return TransformHarnessResult{}, fmt.Errorf("cannot access input path %q: %w", req.Path, statErr)
	}
	inputIsDir := pathInfo.IsDir()

	// Source harness agent extension (e.g. ".src.md") used for directory enumeration filtering.
	srcDesc := srcModule.Descriptor()
	srcExt := srcDesc.Extensions[domain.ArtifactAgent]

	// Collect file paths to process (non-recursive for directory input).
	var filePaths []string
	if inputIsDir {
		entries, readErr := os.ReadDir(req.Path)
		if readErr != nil {
			return TransformHarnessResult{}, fmt.Errorf("cannot enumerate input directory %q: %w", req.Path, readErr)
		}
		for _, entry := range entries {
			if entry.IsDir() {
				continue // non-recursive: skip subdirectories
			}
			if srcExt != "" && !strings.HasSuffix(entry.Name(), srcExt) {
				continue // extension filter: only files matching source harness extension
			}
			filePaths = append(filePaths, filepath.Join(req.Path, entry.Name()))
		}
		sort.Strings(filePaths) // lexicographic order for deterministic results
	} else {
		filePaths = []string{req.Path}
	}

	// Pre-pass: read every source file once and discover the distinct source-model set.
	// The per-file loop below consumes the already-read bytes from the index so no file
	// is read twice.
	idx := IndexSourceModels(filePaths, *srcDesc)

	// Resolve the target model for each distinct source model via the four-rule table.
	// The resolved map is keyed by source model identifier (UnsetSourceModel for the
	// unset group). It is built before the per-file loop so the loop does a simple lookup.
	resolved := make(TransformModelMapping, len(idx.Distinct))
	for _, k := range idx.Distinct {
		// Rule 1: ModelMap entry present (even with an empty value — a present key is a
		// deliberate answer, not an absent one).
		if req.ModelMap != nil {
			if target, ok := req.ModelMap[k]; ok {
				resolved[k] = target
				continue
			}
		}
		// Rule 2: TargetModel non-empty — use it as the fallback for all unmapped groups.
		if req.TargetModel != "" {
			resolved[k] = req.TargetModel
			continue
		}
		// Rule 3: SkipAll suppresses every model question; leave the group's target empty.
		if req.SkipAll[domain.QTransformTargetModel] {
			resolved[k] = ""
			continue
		}
		// Rule 4: ask the user once for this source model.
		ans, _ := s.deps.Interaction.SelectOne(ctx, domain.ChoiceQuestion{
			Question: domain.Question{
				ID:        domain.QTransformTargetModel,
				Subject:   k,
				Title:     titleForSourceModel(k),
				AllowSkip: true,
			},
			Options: buildModelOptions(tgtModule.Descriptor().Models.IDs),
		})
		// Only domain.Answered sets a value; any other status (skip, cancel) leaves empty.
		if ans.Status == domain.Answered {
			target := ans.OptionID
			if ans.Custom != "" {
				target = ans.Custom
			}
			resolved[k] = target
		} else {
			resolved[k] = ""
		}
	}

	// Compute result.TargetModel: the batch-wide value when exactly one non-empty target
	// model was applied across all groups, empty when multiple distinct targets were used.
	// This preserves backward compatibility for the summary screen and CLI report.
	var resultTargetModel string
	{
		nonEmpty := make(map[string]struct{})
		for _, v := range resolved {
			if v != "" {
				nonEmpty[v] = struct{}{}
			}
		}
		if len(nonEmpty) == 1 {
			for v := range nonEmpty {
				resultTargetModel = v
			}
		}
	}

	// Process each file independently, using the pre-read content from the index.
	var files []TransformFileOutcome
	var transformed, skippedMismatch, skippedNotAgent, failed int

	for _, sf := range idx.Files {
		srcPath := sf.Path

		// Handle files that could not be read in the pre-pass.
		if sf.ReadErr != nil {
			files = append(files, TransformFileOutcome{
				SourcePath: srcPath,
				Status:     StatusFailed,
				Reason:     fmt.Sprintf("cannot read file: %v", sf.ReadErr),
			})
			failed++
			continue
		}

		srcBytes := sf.Content

		// Derive agent key from source filename (strips the source harness extension).
		name := filepath.Base(srcPath)
		var agentKey string
		if srcExt != "" && strings.HasSuffix(name, srcExt) {
			agentKey = strings.TrimSuffix(name, srcExt)
		} else {
			agentKey = agentKeyFromFileName(name)
		}

		// Detect harness match.
		verdict := DetectHarnessMatch(srcBytes, srcModule, domain.ArtifactAgent, agentKey)

		switch verdict.Status {
		case HarnessMatchNotAgent:
			files = append(files, TransformFileOutcome{
				SourcePath: srcPath,
				Status:     StatusSkippedNotAgent,
				Reason:     verdict.Reason,
			})
			skippedNotAgent++
			continue
		case HarnessMatchNo:
			files = append(files, TransformFileOutcome{
				SourcePath: srcPath,
				Status:     StatusSkippedMismatch,
				Reason:     verdict.Reason,
			})
			skippedMismatch++
			continue
		}

		// HarnessMatchYes or HarnessMatchIndeterminate: proceed with transform.
		// For Indeterminate, carry the reason as a per-file warning.
		var warning string
		if verdict.Status == HarnessMatchIndeterminate {
			warning = verdict.Reason
		}

		// Resolve destination path via target harness module.
		tgtRelPath, tgtErr := tgtModule.TargetPath(domain.TargetPathRequest{
			Kind:  domain.ArtifactAgent,
			Key:   agentKey,
			Scope: domain.ScopeProject,
		})
		if tgtErr != nil {
			files = append(files, TransformFileOutcome{
				SourcePath: srcPath,
				Status:     StatusFailed,
				Reason:     fmt.Sprintf("cannot resolve destination path: %v", tgtErr),
			})
			failed++
			continue
		}

		// Make destination path absolute. tgtRelPath is relative to the deployment root
		// (project/workspace root), so we must join it with that root — not with the
		// source file's own directory. Derive the workspace root by stripping the source
		// harness's declared agents directory suffix from the source file's directory.
		// If the source directory does not end with that declared suffix (e.g. a flat
		// ad-hoc layout), fall back to using the source directory itself as the base.
		var destPath string
		if filepath.IsAbs(tgtRelPath) {
			destPath = tgtRelPath
		} else {
			srcDir := filepath.Dir(srcPath)
			base := srcDir // fallback for flat / ad-hoc layouts
			if srcAgentsRel := filepath.FromSlash(srcDesc.Paths.Agents.Project); srcAgentsRel != "" {
				// Check whether srcDir ends with the source harness's declared agents path.
				cleanSrcDir := filepath.Clean(srcDir)
				cleanAgents := filepath.Clean(srcAgentsRel)
				separator := string(filepath.Separator)
				if cleanSrcDir == cleanAgents {
					// srcDir IS the agents dir relative to CWD — workspace root is ".".
					base = "."
				} else if strings.HasSuffix(cleanSrcDir, separator+cleanAgents) {
					// Strip the declared agents suffix to recover the workspace root.
					base = cleanSrcDir[:len(cleanSrcDir)-len(separator)-len(cleanAgents)]
				}
				// Otherwise: srcDir does not end with the declared agents path (flat or
				// ad-hoc layout); keep base = srcDir so existing callers are unaffected.
			}
			destPath = filepath.Join(base, tgtRelPath)
		}

		// Overwrite protection: check for existing destination. This check runs even in
		// dry-run mode so that a preview accurately reports what would fail in a real run
		// (per the doc comment on DryRun: "computes and reports every outcome"). Only the
		// actual write is suppressed under DryRun, not the existence check.
		if !req.Overwrite {
			if _, existErr := os.Stat(destPath); existErr == nil {
				// Destination exists and overwrite not requested: per-file failure, not whole-run.
				files = append(files, TransformFileOutcome{
					SourcePath: srcPath,
					Status:     StatusFailed,
					Reason:     fmt.Sprintf("destination file already exists: %s", destPath),
				})
				failed++
				continue
			}
		}

		// Look up the target model for this file's own source model from the resolved map.
		fileTargetModel := resolved[sf.SourceModel]

		// Build the retargeted agent bytes (pure core function).
		retargetIn := RetargetInput{
			Source:              srcBytes,
			SourceModule:        srcModule,
			TargetModule:        tgtModule,
			Kind:                domain.ArtifactAgent,
			AgentKey:            agentKey,
			TargetModel:         fileTargetModel,
			ToolMappingsVersion: toolMappingsVersion,
		}
		tgtBytes, report, retargetErr := BuildRetargetedAgent(retargetIn)
		if retargetErr != nil {
			files = append(files, TransformFileOutcome{
				SourcePath: srcPath,
				Status:     StatusFailed,
				Reason:     retargetErr.Error(),
			})
			failed++
			continue
		}

		// Write the output unless dry-run.
		if !req.DryRun {
			if mkErr := os.MkdirAll(filepath.Dir(destPath), 0o755); mkErr != nil {
				files = append(files, TransformFileOutcome{
					SourcePath: srcPath,
					Status:     StatusFailed,
					Reason:     fmt.Sprintf("cannot create destination directory: %v", mkErr),
				})
				failed++
				continue
			}
			if writeErr := os.WriteFile(destPath, tgtBytes, 0o644); writeErr != nil {
				files = append(files, TransformFileOutcome{
					SourcePath: srcPath,
					Status:     StatusFailed,
					Reason:     fmt.Sprintf("cannot write destination: %v", writeErr),
				})
				failed++
				continue
			}
		}

		// Collect target-harness tool names from the retarget report.
		var tools []string
		for _, res := range report.Tools {
			if res.Outcome == domain.ToolMapped {
				tools = append(tools, res.HarnessTools...)
			}
		}

		files = append(files, TransformFileOutcome{
			SourcePath:           srcPath,
			DestinationPath:      destPath,
			Status:               StatusTransformed,
			Warning:              warning,
			AgentKey:             agentKey,
			Tools:                tools,
			CarriedVerbatimTools: report.CarriedVerbatimTools,
			StrippedFields:       report.StrippedFields,
		})
		transformed++
	}

	// Populate AppliedModelMap from the resolved map (nil when the batch is empty, matching
	// the design invariant that AppliedModelMap is nil when the run produced no files).
	var appliedModelMap TransformModelMapping
	if len(resolved) > 0 {
		appliedModelMap = resolved
	}

	return TransformHarnessResult{
		SourceHarnessID:  req.SourceHarnessID,
		TargetHarnessID:  targetHarnessID,
		TargetModel:      resultTargetModel,
		AppliedModelMap:  appliedModelMap,
		InputPath:        req.Path,
		InputIsDirectory: inputIsDir,
		DryRun:           req.DryRun,
		Files:            files,
		Transformed:      transformed,
		SkippedMismatch:  skippedMismatch,
		SkippedNotAgent:  skippedNotAgent,
		Failed:           failed,
	}, nil
}
