package cli

import (
	"context"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"mosaic-run/internal/domain"
	"mosaic-run/internal/runselect"
	"mosaic-run/internal/session"
)

// executeRun runs the phase pipeline behind the run subcommand: flag reading
// and validation, run-identity resolution, settings parsing, orchestrator
// discovery, session invocation, and outcome reporting. It returns the exit
// code the run subcommand ends with.
//
// Each phase helper reports (..., exitCode, ok): ok == false means the phase
// has already written its error message to errOut and the pipeline must stop
// immediately, returning that exact exit code. This keeps every early-return
// message and code from the original monolithic Run identical, and preserves
// the original phase order exactly:
//
//  1. flag reading
//  2. required-flag and mutual-exclusivity validation
//  3. --checkpoints parsing
//  4. --harness validation
//  5. --timeout validation
//  6. run-identity resolution (skipped when identity is pre-resolved)
//  7. run announcement
//  8. --mode parsing
//  9. --commits parsing
//  10. --commit-branch parsing
//  11. --infra-class parsing
//  12. --infrastructure parsing
//  13. orchestrator discovery
//  14. RunConfig construction
//  15. session invocation and outcome reporting
func executeRun(ctx context.Context, cmd *cobra.Command, store domain.ArtifactStore, identity *RunIdentity, sess session.Session, out, errOut io.Writer) int {
	f := readRunFlags(cmd)

	if code, ok := validateRunFlags(f, errOut); !ok {
		return code
	}

	checkpointsEnabled, code, ok := parseCheckpointsFlag(f.checkpoints, errOut)
	if !ok {
		return code
	}

	if code, ok := validateHarnessFlag(f.harness, errOut); !ok {
		return code
	}

	if code, ok := validateTimeoutFlag(f.timeout, errOut); !ok {
		return code
	}

	resolvedRunID, resolvedRunFolder, resolvedIsNewRun, resolvedPosition, code, ok := resolveRunIdentity(identity, f, errOut)
	if !ok {
		return code
	}

	// State the chosen run before any dispatch (AC2.7): which run, whether
	// new or resumed, and for a resumed run its recorded position.
	fmt.Fprintln(out, runselect.Announce(announceIdentity(resolvedRunID, resolvedRunFolder, resolvedIsNewRun, resolvedPosition)))

	parsedMode, code, ok := parseModeForRun(f, resolvedIsNewRun, needsRunnerAdoption(resolvedRunFolder, resolvedIsNewRun), errOut)
	if !ok {
		return code
	}

	reviewLoopLimit, code, ok := parseReviewLoopLimitFlag(f, resolvedIsNewRun, errOut)
	if !ok {
		return code
	}

	commitsEnabled, code, ok := parseCommitsFlag(f.commits, errOut)
	if !ok {
		return code
	}

	parsedCommitBranch, code, ok := parseCommitBranchFlag(f.commitBranch, commitsEnabled, errOut)
	if !ok {
		return code
	}

	if commitSetupPending(resolvedRunFolder, resolvedIsNewRun) && !f.commitBranchChanged {
		fmt.Fprintln(errOut, "error: --commit-branch is required to retry commit setup on this run (mosaic-owned|user-own)")
		return ExitUsage
	}

	infraClassSelections, code, ok := parseInfraClassFlag(f.infraClass, errOut)
	if !ok {
		return code
	}

	infrastructureFilter := parseInfrastructureFlag(f.infrastructure, f.infrastructureChanged)

	discoveredOrchPath, code, ok := discoverOrchestratorPath(f.harness, errOut)
	if !ok {
		return code
	}

	config := buildRunConfig(f, runConfigInputs{
		discoveredOrchPath:   discoveredOrchPath,
		resolvedRunID:        resolvedRunID,
		resolvedRunFolder:    resolvedRunFolder,
		resolvedIsNewRun:     resolvedIsNewRun,
		parsedMode:           parsedMode,
		checkpointsEnabled:   checkpointsEnabled,
		commitsEnabled:       commitsEnabled,
		parsedCommitBranch:   parsedCommitBranch,
		infraClassSelections: infraClassSelections,
		infrastructureFilter: infrastructureFilter,
		reviewLoopLimit:      reviewLoopLimit,
		supplied: domain.SuppliedSettings{
			Mode:                 f.modeChanged,
			PreConsultation:      f.preConsultChanged,
			ManualResolution:     f.manualResolutionChanged,
			ReviewLoopLimit:      f.reviewLoopLimitChanged,
			InfraClassSelections: f.infraClassChanged,
			CommitBranchVariant:  f.commitBranchChanged,
		},
	})

	return runSessionAndReport(ctx, config, store, sess, out, errOut, resolvedRunFolder)
}
