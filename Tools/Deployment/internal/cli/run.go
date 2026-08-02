package cli

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"mosaic-deploy/internal/app"
	"mosaic-deploy/internal/domain"
)

// Exit code contract (AC19.4):
const (
	ExitSuccess   = 0 // OutcomeSuccess
	ExitFailure   = 1 // OutcomeFailed or unexpected error
	ExitWithSkips = 2 // OutcomeCompletedWithGaps
	ExitUsage     = 3 // argument parsing or usage error
)

// Run parses args, selects the appropriate subcommand, and delegates to the app.Service.
// It writes human-readable output to out by default; --output json emits domain.RunSummary
// as a single JSON document. errOut receives error and usage messages. Run returns an exit
// code per the contract above. Dependency wiring is the caller's responsibility; Run never
// constructs its own dependencies.
func Run(ctx context.Context, args []string, svc app.Service, out, errOut io.Writer) int {
	// exitCode is set by a subcommand handler when one runs.
	// If no subcommand runs (unknown flag, unknown subcommand, or no subcommand given),
	// Execute() returns an error and we return ExitUsage directly.
	exitCode := ExitUsage

	// Global persistent flags, shared by all subcommands.
	var verbose bool
	var mosaicRoot string
	var allowExternal bool

	root := &cobra.Command{
		Use:           "mosaic-deploy",
		SilenceErrors: true,
		SilenceUsage:  true,
		// RunE on root handles the case where no subcommand is provided.
		RunE: func(cmd *cobra.Command, _ []string) error {
			return fmt.Errorf("a subcommand is required: deploy, update, or workflows")
		},
	}
	// Redirect cobra's own output (help, usage, errors) to errOut so it does not
	// intermix with the machine-readable output written to out.
	root.SetOut(errOut)
	root.SetErr(errOut)

	root.PersistentFlags().BoolVar(&verbose, "verbose", false, "Enable verbose output")
	root.PersistentFlags().StringVar(&mosaicRoot, "mosaic-root", "", "Override the MOSAIC root directory")
	root.PersistentFlags().BoolVar(&allowExternal, "allow-external", false, "Enable external harness modules")

	// ------------------------------------------------------------------
	// deploy subcommand
	// ------------------------------------------------------------------
	var (
		deployHarness     string
		deployWorkspace   string
		deploySelections  string
		deployOutput      string
		deployConflict    string
		deployDryRun      bool
		deployAutoConfirm bool
	)

	deployCmd := &cobra.Command{
		Use:           "deploy",
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			// Parse --conflict
			var conflictDefault domain.ConflictDecision
			switch strings.ToLower(deployConflict) {
			case "", "skip":
				conflictDefault = domain.DecisionSkip
			case "overwrite":
				conflictDefault = domain.DecisionOverwrite
			case "backup":
				conflictDefault = domain.DecisionBackupThenOverwrite
			default:
				fmt.Fprintf(errOut, "invalid --conflict value %q; valid values: skip, overwrite, backup\n", deployConflict)
				exitCode = ExitUsage
				return nil
			}

			req := app.DeployRequest{
				HarnessID:       deployHarness,
				WorkspacePath:   deployWorkspace,
				Scope:           domain.ScopeProject,
				DryRun:          deployDryRun,
				AutoConfirmPlan: deployAutoConfirm,
			}

			// Only set ConflictDefault when the flag was explicitly provided; an absent
			// flag leaves the field at the zero value so interactive conflict prompts fire.
			if deployConflict != "" {
				req.ConflictDefault = conflictDefault
			}

			// Apply selections file when provided.
			if deploySelections != "" {
				sf, code, err := parseSelectionsFile(deploySelections)
				if err != nil {
					fmt.Fprintf(errOut, "error: %v\n", err)
					exitCode = code
					return nil
				}
				req.WorkflowIDs = sf.Workflows
				req.UtilityAgentIDs = sf.UtilityAgents
				req.HookIDs = sf.Hooks
				if len(sf.TierModels) > 0 {
					req.TierModels = tierModelsFromFile(sf)
				}
			}

			summary, svcErr := svc.DeployNew(ctx, req)
			exitCode = renderOutput(out, errOut, deployOutput, summary, svcErr)
			return nil
		},
	}

	deployCmd.Flags().StringVar(&deployHarness, "harness", "", "Harness ID")
	deployCmd.Flags().StringVar(&deployWorkspace, "workspace", "", "Workspace path")
	deployCmd.Flags().StringVar(&deploySelections, "selections", "", "Path to selections YAML file")
	deployCmd.Flags().StringVar(&deployOutput, "output", "", "Output format (json)")
	deployCmd.Flags().StringVar(&deployConflict, "conflict", "", "How to handle locally-modified files (skip|overwrite|backup)")
	deployCmd.Flags().BoolVar(&deployDryRun, "dry-run", false, "Dry run mode; no files are written")
	deployCmd.Flags().BoolVar(&deployAutoConfirm, "auto-confirm", false, "Auto-confirm the deployment plan without prompting")

	// ------------------------------------------------------------------
	// update subcommand
	// ------------------------------------------------------------------
	var (
		updateHarness     string
		updateWorkspace   string
		updateConflict    string
		updateOutput      string
		updateDryRun      bool
		updateAutoConfirm bool
	)

	updateCmd := &cobra.Command{
		Use:           "update",
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			// Parse --conflict
			var conflictDefault domain.ConflictDecision
			switch strings.ToLower(updateConflict) {
			case "", "skip":
				conflictDefault = domain.DecisionSkip
			case "overwrite":
				conflictDefault = domain.DecisionOverwrite
			case "backup":
				conflictDefault = domain.DecisionBackupThenOverwrite
			default:
				fmt.Fprintf(errOut, "invalid --conflict value %q; valid values: skip, overwrite, backup\n", updateConflict)
				exitCode = ExitUsage
				return nil
			}

			req := app.UpdateRequest{
				HarnessID:       updateHarness,
				WorkspacePath:   updateWorkspace,
				ConflictDefault: conflictDefault,
				DryRun:          updateDryRun,
				AutoConfirmPlan: updateAutoConfirm,
			}

			summary, svcErr := svc.Update(ctx, req)
			exitCode = renderOutput(out, errOut, updateOutput, summary, svcErr)
			return nil
		},
	}

	updateCmd.Flags().StringVar(&updateHarness, "harness", "", "Harness ID")
	updateCmd.Flags().StringVar(&updateWorkspace, "workspace", "", "Workspace path")
	updateCmd.Flags().StringVar(&updateConflict, "conflict", "", "How to handle locally-modified files (skip|overwrite|backup)")
	updateCmd.Flags().StringVar(&updateOutput, "output", "", "Output format (json)")
	updateCmd.Flags().BoolVar(&updateDryRun, "dry-run", false, "Dry run mode; no files are written")
	updateCmd.Flags().BoolVar(&updateAutoConfirm, "auto-confirm", false, "Auto-confirm the deployment plan without prompting")

	// ------------------------------------------------------------------
	// workflows subcommand
	// ------------------------------------------------------------------
	var (
		workflowsHarness     string
		workflowsWorkspace   string
		workflowsWorkflows   string
		workflowsConflict    string
		workflowsOutput      string
		workflowsDryRun      bool
		workflowsAutoConfirm bool
	)

	workflowsCmd := &cobra.Command{
		Use:           "workflows",
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			// Parse --conflict (same valid values and "set only when explicitly provided" rule as update).
			var conflictDefault domain.ConflictDecision
			switch strings.ToLower(workflowsConflict) {
			case "", "skip":
				conflictDefault = domain.DecisionSkip
			case "overwrite":
				conflictDefault = domain.DecisionOverwrite
			case "backup":
				conflictDefault = domain.DecisionBackupThenOverwrite
			default:
				fmt.Fprintf(errOut, "invalid --conflict value %q; valid values: skip, overwrite, backup\n", workflowsConflict)
				exitCode = ExitUsage
				return nil
			}

			req := app.WorkflowUpdateRequest{
				HarnessID:       workflowsHarness,
				WorkspacePath:   workflowsWorkspace,
				DryRun:          workflowsDryRun,
				AutoConfirmPlan: workflowsAutoConfirm,
			}

			// Only set ConflictDefault when the flag was explicitly provided.
			if workflowsConflict != "" {
				req.ConflictDefault = conflictDefault
			}

			// Parse --workflows as a comma-separated list into WorkflowIDs. Nil when absent
			// so the interactive selection fires (CD-6 pre-answer convention).
			if workflowsWorkflows != "" {
				parts := strings.Split(workflowsWorkflows, ",")
				ids := make([]string, 0, len(parts))
				for _, p := range parts {
					if t := strings.TrimSpace(p); t != "" {
						ids = append(ids, t)
					}
				}
				req.WorkflowIDs = ids
			}

			summary, svcErr := svc.UpdateWorkflows(ctx, req)
			exitCode = renderOutput(out, errOut, workflowsOutput, summary, svcErr)
			return nil
		},
	}

	workflowsCmd.Flags().StringVar(&workflowsHarness, "harness", "", "Harness ID")
	workflowsCmd.Flags().StringVar(&workflowsWorkspace, "workspace", "", "Workspace path")
	workflowsCmd.Flags().StringVar(&workflowsWorkflows, "workflows", "", "Comma-separated workflow IDs to deploy (replaces the current set)")
	workflowsCmd.Flags().StringVar(&workflowsConflict, "conflict", "", "How to handle locally-modified files (skip|overwrite|backup)")
	workflowsCmd.Flags().StringVar(&workflowsOutput, "output", "", "Output format (json)")
	workflowsCmd.Flags().BoolVar(&workflowsDryRun, "dry-run", false, "Dry run mode; no files are written")
	workflowsCmd.Flags().BoolVar(&workflowsAutoConfirm, "auto-confirm", false, "Auto-confirm the deployment plan without prompting")

	root.AddCommand(deployCmd, updateCmd, workflowsCmd)
	root.SetArgs(args)

	if err := root.Execute(); err != nil {
		// cobra failed to parse flags or find a subcommand (including root.RunE error).
		return ExitUsage
	}
	return exitCode
}
