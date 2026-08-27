package cli

import (
	"fmt"

	"github.com/spf13/pflag"

	"mosaic-run/internal/harness"
)

// FlagSpec describes one command-line flag's arity for pre-scan consumers that
// must interpret os.Args before cobra parses it.
type FlagSpec struct {
	// Name is the long flag name including its leading "--" (e.g. "--harness").
	Name string

	// TakesValue is true when the flag consumes a following argument in the
	// "--flag value" form. False for boolean flags, which never do.
	TakesValue bool
}

// RegisterRunFlags registers every flag the run subcommand accepts onto fs.
// Both cli.Run (with runCmd.Flags()) and RunFlagSpecs (with a throwaway set it
// then introspects) call this function, making it the single declaration that
// keeps registration and arity-publication in sync.
//
// The "--tui" flag is the documented exception: it is an entry-point-only flag
// that the run subcommand does not register, so it is not registered here.
// RunFlagSpecs appends it explicitly with TakesValue: false.
func RegisterRunFlags(fs *pflag.FlagSet) {
	fs.String("workflow", "", "Workflow identifier (required)")
	fs.String("task", "", "Task description (required)")
	fs.Bool("allow-version-drift", false, "Allow workflow version mismatch when resuming")
	fs.String("mode", "", "Execution mode (orchestrated|auto|auto-review) — required")
	fs.String("checkpoints", "disabled", "Checkpoint support (disabled|enabled)")
	fs.String("commits", "disabled", "Commit-class infrastructure dispatch (disabled|enabled)")
	fs.String("commit-branch", "", "Commit branch variant (mosaic-owned|user-own); default mosaic-owned")
	fs.Bool("pre-consult", true, "Enable one-shot run-start pre-consultation (auto and auto-review only); use --pre-consult=false to disable")
	fs.Bool("manual-resolution", false, "Put the user in the resolver's place on consultation failure")
	fs.String("run", "", "Resume a specific run by run_id")
	fs.Bool("new-run", false, "Force creation of a new run")
	fs.String("harness", harness.FakeHarnessID, fmt.Sprintf("Harness adapter to use (%s)", harness.FlagValues()))
	fs.String("timeout", "30m", "Invocation timeout for the harness adapter (e.g. 30m, 1h)")
	fs.String("claude-path", "", "Executable path override for the harness selected by --harness; when absent, each harness uses its own default (claude, opencode, copilot)")
	fs.String("infra-class", "", "Comma-separated class=agent mappings for non-interactive agent-per-class selection (e.g. checkpoint=checkpoint-manager-git,commit=commit-manager-git)")
	fs.StringArray("input", nil, "Path to a file or directory to copy into a new run's folder before the first dispatch; repeatable. Not permitted with --run.")
}

// RunFlagSpecs returns the arity of every flag mosaic-run accepts: every flag
// the run subcommand declares, plus the entry-point-only "--tui" flag. The
// returned slice is ordered by flag name (pflag's VisitAll order) and must
// never be mutated by callers.
//
// This is the authoritative arity declaration. The run subcommand's flag
// registration and this function derive from one declaration (RegisterRunFlags),
// so a flag cannot be added to one without appearing in the other.
//
// The "--tui" flag is the documented exception: it is an entry-point-only flag
// that the run subcommand does not register. RunFlagSpecs appends it explicitly
// with TakesValue: false.
func RunFlagSpecs() []FlagSpec {
	fs := pflag.NewFlagSet("run-specs", pflag.ContinueOnError)
	RegisterRunFlags(fs)

	var specs []FlagSpec
	fs.VisitAll(func(f *pflag.Flag) {
		specs = append(specs, FlagSpec{
			Name:       "--" + f.Name,
			TakesValue: f.Value.Type() != "bool",
		})
	})

	// --tui is the one entry-point-only flag not registered on the run subcommand.
	// It is appended explicitly so that pre-scan consumers have a complete arity map.
	specs = append(specs, FlagSpec{Name: "--tui", TakesValue: false})

	return specs
}

// ValueBearingFlagNames returns the names of every flag whose FlagSpec has
// TakesValue == true. It is the set a pre-scan must skip the following token for
// when scanning os.Args before cobra has parsed them.
func ValueBearingFlagNames() []string {
	specs := RunFlagSpecs()
	var names []string
	for _, s := range specs {
		if s.TakesValue {
			names = append(names, s.Name)
		}
	}
	return names
}
