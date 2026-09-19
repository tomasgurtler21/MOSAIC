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
	fs.String("executable-path", "", "Executable path override for the harness selected by --harness; when absent, each harness uses its own default (claude, opencode, copilot)")
	fs.String("infra-class", "", "Comma-separated class=agent mappings for non-interactive agent-per-class selection (e.g. checkpoint=checkpoint-manager-git,commit=commit-manager-git)")
	fs.StringArray("input", nil, "Path to a file or directory to copy into a new run's folder before the first dispatch; repeatable. Not permitted with --run.")
	fs.String("ghcp-permission-mode", "", "GHCP CLI permission strategy: blanket (--yolo, grants all permissions) or allowlist (per-tool --allow-tool entries from agent frontmatter). Required when --harness=ghcp-cli.")
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

// RegisterTestFlags registers every flag the test subcommand accepts onto fs.
// Both cli.RunTestCommand (with testCmd.Flags()) and TestFlagSpecs (with a
// throwaway set it then introspects) call this function, making it the single
// declaration that keeps registration and arity-publication in sync.
func RegisterTestFlags(fs *pflag.FlagSet) {
	fs.String("catalog", "", "Path to the MOSAIC repo root (required)")
	fs.String("suite", "", "Test suite to run (smoke|full)")
	fs.StringArray("workflow", nil, "Workflow ID to test (repeatable)")
	fs.String("mode", "", "Execution mode for single-workflow scope")
	fs.StringArray("harness", nil, "Harness ID to test (repeatable; default: all CLI harnesses)")
	fs.String("ghcp-permission-mode", "", "GHCP CLI permission strategy (blanket|allowlist)")
}

// TestFlagSpecs returns the arity of every flag the test subcommand accepts,
// for pre-scan compatibility in main.go. This is the authoritative arity
// declaration for test flags, derived from RegisterTestFlags.
func TestFlagSpecs() []FlagSpec {
	fs := pflag.NewFlagSet("test-specs", pflag.ContinueOnError)
	RegisterTestFlags(fs)

	var specs []FlagSpec
	fs.VisitAll(func(f *pflag.Flag) {
		specs = append(specs, FlagSpec{
			Name:       "--" + f.Name,
			TakesValue: f.Value.Type() != "bool",
		})
	})
	return specs
}

// ValueBearingFlagNames returns the names of every flag whose FlagSpec has
// TakesValue == true, for the run subcommand. It is the set a pre-scan must
// skip the following token for when scanning os.Args before cobra has parsed
// them. This function covers the run subcommand only; callers that also need
// to recognise test subcommand flags (e.g. main.go's pre-scan) should
// additionally call TestFlagSpecs.
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

// DefaultTestHarnesses returns the IDs of all CLI harnesses that the test
// subcommand runs against by default when no --harness flags are supplied.
// The list is derived from harness.CLISelections(), which is the same source
// used by the TUI harness-selection screen and the --harness flag validation.
func DefaultTestHarnesses() []string {
	sels := harness.CLISelections()
	ids := make([]string, len(sels))
	for i, s := range sels {
		ids[i] = s.ID
	}
	return ids
}

// AllValueBearingFlagNames returns the names of every value-bearing flag for
// both the run and test subcommands, deduplicated. This is the set that
// main.go's pre-scan functions use so that test subcommand flags (e.g.
// --catalog) are not misidentified as positional arguments.
func AllValueBearingFlagNames() []string {
	seen := make(map[string]bool)
	var names []string
	for _, s := range RunFlagSpecs() {
		if s.TakesValue && !seen[s.Name] {
			seen[s.Name] = true
			names = append(names, s.Name)
		}
	}
	for _, s := range TestFlagSpecs() {
		if s.TakesValue && !seen[s.Name] {
			seen[s.Name] = true
			names = append(names, s.Name)
		}
	}
	return names
}
