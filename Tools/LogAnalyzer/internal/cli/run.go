package cli

import (
	"context"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"mosaic-log-analyzer/internal/app"
)

// Options is the fully-wired dependency set the composition root hands to the
// CLI. The CLI constructs no infrastructure of its own.
type Options struct {
	Service *app.Service
	Stdout  io.Writer
	Stderr  io.Writer
	Version string
}

// NewRootCommand builds the cobra command tree.
//
// Command surface:
//
//	mosaic-log-analyzer report [--path P] [--run ID] [--format json|text]
//	mosaic-log-analyzer total  --run ID [--path P]          (stable FR-19 JSON)
//	mosaic-log-analyzer pricing path                        (prints the config path)
//
// Persistent flags:
//
//	--path string   logs root OR a single run folder
//	--run  string   restrict to one run id
//	--format string json | text (default: json for `total`, text for `report`)
//	--tui           recognised and ignored here; consumed by the composition root
func NewRootCommand(opts Options) *cobra.Command {
	exitCodeHolder := new(int)
	*exitCodeHolder = ExitUsage
	return newRootCommandWithExitCode(opts, exitCodeHolder)
}

// Execute runs the command tree with the given args and returns the process
// exit code. It never panics and never blocks on user input.
func Execute(opts Options, args []string) int {
	exitCode := ExitUsage
	root := newRootCommandWithExitCode(opts, &exitCode)
	root.SetArgs(args)
	if err := root.Execute(); err != nil {
		fmt.Fprintf(opts.Stderr, "%v\n", err)
		return ExitUsage
	}
	return exitCode
}

// newRootCommandWithExitCode constructs the full cobra command tree. The
// exitCode pointer is written by each subcommand's RunE closure so that the
// caller can read the derived exit code after root.Execute() returns.
func newRootCommandWithExitCode(opts Options, exitCode *int) *cobra.Command {
	var (
		pathFlag   string
		runFlag    string
		formatFlag string
	)

	root := &cobra.Command{
		Use:           "mosaic-log-analyzer",
		Short:         "Analyse MOSAIC orchestration logs and report token usage and cost",
		SilenceErrors: true,
		SilenceUsage:  true,
	}
	root.SetOut(opts.Stderr)
	root.SetErr(opts.Stderr)

	root.PersistentFlags().StringVar(&pathFlag, "path", "", "logs root or single run folder")
	root.PersistentFlags().StringVar(&runFlag, "run", "", "restrict analysis to one run id")
	root.PersistentFlags().StringVar(&formatFlag, "format", "", "output format: json or text")
	root.PersistentFlags().Bool("tui", false, "launch interactive TUI (consumed by composition root)")

	// ------------------------------------------------------------------
	// report subcommand
	// ------------------------------------------------------------------
	reportCmd := &cobra.Command{
		Use:           "report",
		Short:         "Produce a full cost report",
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			req := app.Request{
				ExplicitPath: pathFlag,
				RunID:        runFlag,
			}
			result, err := opts.Service.Analyze(context.Background(), req)
			if err != nil {
				fmt.Fprintf(opts.Stderr, "error: %v\n", err)
				*exitCode = ExitFailure
				return nil
			}
			switch result.Outcome {
			case app.OutcomeNoData:
				*exitCode = ExitNoData
				return nil
			case app.OutcomeSourceNotFound:
				*exitCode = ExitUsage
				return nil
			case app.OutcomeSourceUnusable:
				*exitCode = ExitUnusable
				return nil
			}

			// Determine output format; default for report is text.
			format := formatFlag
			if format == "" {
				format = "text"
			}

			var encErr error
			switch format {
			case "json":
				encErr = EncodeReport(opts.Stdout, result.Report)
			default: // "text" or unrecognised → text
				encErr = EncodeText(opts.Stdout, result.Report)
			}
			if encErr != nil {
				fmt.Fprintf(opts.Stderr, "error: encoding output: %v\n", encErr)
				*exitCode = ExitFailure
				return nil
			}
			*exitCode = ExitSuccess
			return nil
		},
	}

	// ------------------------------------------------------------------
	// total subcommand (stable FR-19 JSON contract)
	// ------------------------------------------------------------------
	totalCmd := &cobra.Command{
		Use:           "total",
		Short:         "Emit the basic total-cost-per-run JSON (stable FR-19 contract)",
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if runFlag == "" {
				fmt.Fprintf(opts.Stderr, "error: --run is required for the total command\n")
				*exitCode = ExitUsage
				return nil
			}
			req := app.Request{
				ExplicitPath: pathFlag,
				RunID:        runFlag,
			}
			total, outcome, err := opts.Service.TotalForRun(context.Background(), req, runFlag)
			if err != nil {
				fmt.Fprintf(opts.Stderr, "error: %v\n", err)
				*exitCode = ExitFailure
				return nil
			}
			switch outcome {
			case app.OutcomeNoData:
				*exitCode = ExitNoData
				return nil
			case app.OutcomeSourceNotFound:
				*exitCode = ExitUsage
				return nil
			case app.OutcomeSourceUnusable:
				*exitCode = ExitUnusable
				return nil
			}

			if err := EncodeRunTotal(opts.Stdout, total); err != nil {
				fmt.Fprintf(opts.Stderr, "error: encoding output: %v\n", err)
				*exitCode = ExitFailure
				return nil
			}
			*exitCode = ExitSuccess
			return nil
		},
	}

	// ------------------------------------------------------------------
	// pricing subcommand group
	// ------------------------------------------------------------------
	pricingCmd := &cobra.Command{
		Use:           "pricing",
		Short:         "Pricing configuration commands",
		SilenceErrors: true,
		SilenceUsage:  true,
	}

	pricingPathCmd := &cobra.Command{
		Use:           "path",
		Short:         "Print the pricing config file path",
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			fmt.Fprintln(opts.Stdout, opts.Service.PricingPath())
			*exitCode = ExitSuccess
			return nil
		},
	}
	pricingCmd.AddCommand(pricingPathCmd)

	root.AddCommand(reportCmd, totalCmd, pricingCmd)
	return root
}
