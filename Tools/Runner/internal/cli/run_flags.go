package cli

import (
	"fmt"
	"io"
	"time"

	"github.com/spf13/cobra"
)

// runFlags holds every run-subcommand flag value read directly from the
// parsed FlagSet, before any validation or type conversion.
type runFlags struct {
	workflowID            string
	task                  string
	allowVersionDrift     bool
	checkpoints           string
	mode                  string
	commits               string
	commitBranch          string
	preConsult            bool
	manualResolution      bool
	runID                 string
	isNewRun              bool
	harness               string
	timeout               string
	infraClass            string
	inputFlags            []string
	infrastructure        string
	infrastructureChanged bool
	devTestMode           bool
}

// readRunFlags reads all flag values from the parsed FlagSet. RegisterRunFlags
// registered every flag onto runCmd.Flags(), so cobra has already populated
// them before RunE is called. GetString/GetBool/GetStringArray errors are
// impossible here: every flag was registered with the correct type, so the
// only possible error is "flag does not exist", which would be a programming
// error caught immediately in testing.
func readRunFlags(cmd *cobra.Command) runFlags {
	var f runFlags

	f.workflowID, _ = cmd.Flags().GetString("workflow")
	f.task, _ = cmd.Flags().GetString("task")
	f.allowVersionDrift, _ = cmd.Flags().GetBool("allow-version-drift")
	f.checkpoints, _ = cmd.Flags().GetString("checkpoints")
	f.mode, _ = cmd.Flags().GetString("mode")
	f.commits, _ = cmd.Flags().GetString("commits")
	f.commitBranch, _ = cmd.Flags().GetString("commit-branch")
	f.preConsult, _ = cmd.Flags().GetBool("pre-consult")
	f.manualResolution, _ = cmd.Flags().GetBool("manual-resolution")
	f.runID, _ = cmd.Flags().GetString("run")
	f.isNewRun, _ = cmd.Flags().GetBool("new-run")
	f.harness, _ = cmd.Flags().GetString("harness")
	f.timeout, _ = cmd.Flags().GetString("timeout")
	f.infraClass, _ = cmd.Flags().GetString("infra-class")

	inputFlagsRaw, _ := cmd.Flags().GetStringArray("input")
	// Preserve nil semantics: when --input is not supplied, SeedInputs must be
	// nil (not an empty slice) so callers can distinguish "not set" from "set to
	// zero paths". pflag's GetStringArray returns []string{} for an unset flag
	// whose default is nil; convert that back to nil here.
	if len(inputFlagsRaw) > 0 {
		f.inputFlags = inputFlagsRaw
	}

	f.infrastructure, _ = cmd.Flags().GetString("infrastructure")
	f.infrastructureChanged = cmd.Flags().Changed("infrastructure")
	f.devTestMode, _ = cmd.Flags().GetBool("dev-test-mode")

	// --executable-path is accepted as-is; it is pre-scanned in main.go and
	// passed directly to buildAdapter. No validation is needed here, and its
	// value is not read into RunConfig by cli.Run.

	return f
}

// validateRunFlags applies the run subcommand's required-flag and
// mutual-exclusivity guards, in the same order the original monolithic Run
// applied them.
func validateRunFlags(f runFlags, errOut io.Writer) (exitCode int, ok bool) {
	// Validate required flags.
	if f.workflowID == "" {
		fmt.Fprintf(errOut, "error: --workflow is required\n")
		return ExitUsage, false
	}
	if f.task == "" {
		fmt.Fprintf(errOut, "error: --task is required\n")
		return ExitUsage, false
	}

	// Check --run and --new-run mutual exclusivity.
	if f.runID != "" && f.isNewRun {
		fmt.Fprintf(errOut, "error: --run and --new-run are mutually exclusive\n")
		return ExitUsage, false
	}

	// Check --input and --run mutual exclusivity. Seeding only applies when
	// creating a new run; silently ignoring --input on a resume would let a
	// user believe inputs were seeded when they were not.
	if len(f.inputFlags) > 0 && f.runID != "" {
		fmt.Fprintf(errOut, "error: --input and --run are mutually exclusive\n")
		return ExitUsage, false
	}

	// Dev-mode guard: --infrastructure is only available in dev test mode.
	// The hidden --dev-test-mode flag is passed automatically by the test
	// framework's buildRunArgs; production runs never pass it.
	if f.infrastructureChanged && !f.devTestMode {
		fmt.Fprintf(errOut, "error: --infrastructure is only available in dev test mode\n")
		return ExitUsage, false
	}

	return ExitSuccess, true
}

// parseCheckpointsFlag parses --checkpoints into its boolean form.
func parseCheckpointsFlag(raw string, errOut io.Writer) (enabled bool, exitCode int, ok bool) {
	switch raw {
	case "disabled":
		return false, ExitSuccess, true
	case "enabled":
		return true, ExitSuccess, true
	default:
		fmt.Fprintf(errOut, "error: invalid --checkpoints value %q; valid values: disabled, enabled\n", raw)
		return false, ExitUsage, false
	}
}

// validateTimeoutFlag verifies that --timeout is a parseable duration.
func validateTimeoutFlag(raw string, errOut io.Writer) (exitCode int, ok bool) {
	if _, err := time.ParseDuration(raw); err != nil {
		fmt.Fprintf(errOut, "error: invalid --timeout value %q: %v\n", raw, err)
		return ExitUsage, false
	}
	return ExitSuccess, true
}
