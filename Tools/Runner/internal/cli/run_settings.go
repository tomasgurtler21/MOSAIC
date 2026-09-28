package cli

import (
	"fmt"
	"io"
	"strings"

	"mosaic-run/internal/domain"
)

// runConfigInputs carries the values computed by the phases that run after
// run-identity resolution and before RunConfig construction: the resolved
// identity itself, orchestrator discovery, and every parsed setting.
type runConfigInputs struct {
	discoveredOrchPath   string
	resolvedRunID        string
	resolvedRunFolder    string
	resolvedIsNewRun     bool
	parsedMode           domain.ExecutionMode
	checkpointsEnabled   bool
	commitsEnabled       bool
	parsedCommitBranch   domain.CommitBranchVariant
	infraClassSelections map[string]string
	infrastructureFilter []string
}

// parseModeFlag validates and parses the required --mode flag. It is
// validated after run identity resolution so that run-not-found and scanner
// errors appear first, matching the original monolithic Run's order.
func parseModeFlag(raw string, errOut io.Writer) (mode domain.ExecutionMode, exitCode int, ok bool) {
	if raw == "" {
		fmt.Fprintf(errOut, "error: --mode is required; valid values: %s\n", strings.Join(executionModeStrings(), ", "))
		return "", ExitUsage, false
	}
	parsed, err := domain.ParseExecutionMode(raw)
	if err != nil {
		fmt.Fprintf(errOut, "error: invalid --mode value %q; valid values: %s\n", raw, strings.Join(executionModeStrings(), ", "))
		return "", ExitUsage, false
	}
	return parsed, ExitSuccess, true
}

// executionModeStrings renders every valid execution mode as a string, for
// --mode error messages.
func executionModeStrings() []string {
	modes := domain.ExecutionModes()
	out := make([]string, 0, len(modes))
	for _, m := range modes {
		out = append(out, string(m))
	}
	return out
}

// parseCommitsFlag parses --commits into its boolean form.
func parseCommitsFlag(raw string, errOut io.Writer) (enabled bool, exitCode int, ok bool) {
	switch raw {
	case "disabled":
		return false, ExitSuccess, true
	case "enabled":
		return true, ExitSuccess, true
	default:
		fmt.Fprintf(errOut, "error: invalid --commits value %q; valid values: disabled, enabled\n", raw)
		return false, ExitUsage, false
	}
}

// parseCommitBranchFlag parses --commit-branch. CommitBranchVariant is only
// meaningful when commits are enabled: when commits are disabled and the flag
// was not explicitly provided, the default is zeroed out so a disabled-commits
// run carries no spurious mosaic-owned default.
func parseCommitBranchFlag(raw string, commitsEnabled bool, errOut io.Writer) (variant domain.CommitBranchVariant, exitCode int, ok bool) {
	parsed, err := domain.ParseCommitBranchVariant(raw)
	if err != nil {
		validVariants := make([]string, 0, len(domain.CommitBranchVariants()))
		for _, v := range domain.CommitBranchVariants() {
			validVariants = append(validVariants, string(v))
		}
		fmt.Fprintf(errOut, "error: invalid --commit-branch value %q; valid values: %s\n", raw, strings.Join(validVariants, ", "))
		return "", ExitUsage, false
	}
	if !commitsEnabled && raw == "" {
		parsed = domain.CommitBranchVariant("")
	}
	return parsed, ExitSuccess, true
}

// parseInfraClassFlag parses --infra-class into a class-to-agent map.
func parseInfraClassFlag(raw string, errOut io.Writer) (selections map[string]string, exitCode int, ok bool) {
	if raw == "" {
		return nil, ExitSuccess, true
	}

	result := make(map[string]string)
	for _, pair := range strings.Split(raw, ",") {
		pair = strings.TrimSpace(pair)
		if pair == "" {
			continue
		}
		eqIdx := strings.Index(pair, "=")
		if eqIdx < 0 {
			fmt.Fprintf(errOut, "error: invalid --infra-class value %q: expected format class=agent\n", pair)
			return nil, ExitUsage, false
		}
		class := strings.TrimSpace(pair[:eqIdx])
		agent := strings.TrimSpace(pair[eqIdx+1:])
		result[class] = agent
	}
	if len(result) == 0 {
		return nil, ExitSuccess, true
	}
	return result, ExitSuccess, true
}

// parseInfrastructureFlag parses --infrastructure into an InfrastructureFilter.
// nil/empty/populated semantics: nil when not passed, []string{} when passed
// empty, populated slice when comma-separated keys are given. This flag has
// no invalid form; validity of --infrastructure without dev-test-mode is
// checked earlier by validateRunFlags.
func parseInfrastructureFlag(raw string, changed bool) []string {
	if !changed {
		return nil
	}
	if raw == "" {
		return []string{}
	}
	tokens := strings.Split(raw, ",")
	result := make([]string, 0, len(tokens))
	for _, tok := range tokens {
		tok = strings.TrimSpace(tok)
		if tok != "" {
			result = append(result, tok)
		}
	}
	return result
}

// buildRunConfig assembles the domain.RunConfig from the parsed flags and
// resolved run identity.
func buildRunConfig(f runFlags, in runConfigInputs) domain.RunConfig {
	return domain.RunConfig{
		OrchestratorFilePath: in.discoveredOrchPath,
		HarnessID:            f.harness,
		WorkflowID:           domain.WorkflowID(f.workflowID),
		Task:                 f.task,
		AllowVersionDrift:    f.allowVersionDrift,
		RunID:                in.resolvedRunID,
		RunFolder:            in.resolvedRunFolder,
		IsNewRun:             in.resolvedIsNewRun,
		RunSettings: domain.RunSettings{
			Mode:                in.parsedMode,
			Checkpoints:         in.checkpointsEnabled,
			Commits:             in.commitsEnabled,
			CommitBranchVariant: in.parsedCommitBranch,
			PreConsultation:     f.preConsult,
			ManualResolution:    f.manualResolution,
		},
		InfraClassSelections: in.infraClassSelections,
		SeedInputs:           f.inputFlags,
		InfrastructureFilter: in.infrastructureFilter,
	}
}
