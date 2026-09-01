package cli

import (
	"fmt"
	"io"
)

// FlagSpec describes one flag on the CLI's command surface: its name, the
// placeholder shown for its value (empty for a boolean flag), a one-line
// summary, and whether it consumes a following token. It is the single
// source three consumers currently duplicate: this package's own flag
// parser, the composition root's positional detection, and the usage text
// itself.
type FlagSpec struct {
	Name          string // "--harness"
	Placeholder   string // "<id>", empty for a boolean flag
	Summary       string // one line, shown in usage
	ConsumesValue bool   // false for boolean flags such as --tui
}

// CommandSpec describes one command on the usage surface.
type CommandSpec struct {
	Name    string // "run"
	Args    string // "<suite>"
	Summary string
	Group   string // non-empty groups commands under a section header in --help
}

// commandSpecs is the single source Commands returns. The interceptor route
// is deliberately absent and is never derived generically: the subject
// under test may read this binary's help text.
//
// Ungrouped commands (empty Group) appear first in usage output. Commands with
// a non-empty Group are rendered under a group header line after all ungrouped
// commands.
var commandSpecs = []CommandSpec{
	{Name: "run", Args: "<suite>", Summary: "Pre-flights the suite, then executes it"},
	{Name: "validate", Args: "<suite>", Summary: "Pre-flights the suite only; creates no sandbox"},
	{Name: "store", Args: "[<report.json>...]", Summary: "File reports into OrchestrationTestResults/", Group: "Process Test Reports"},
	{Name: "summary", Args: "", Summary: "Generate summary Markdown from stored reports", Group: "Process Test Reports"},
}

// flagSpecs is the single source Flags, ValueConsumingFlags and Usage all
// derive from — the shared list ContractsDesign.md's command-surface table
// describes, kept in display order.
var flagSpecs = []FlagSpec{
	{Name: "--tests", Placeholder: "<id,...>", Summary: "Restrict the run to named tests", ConsumesValue: true},
	{Name: "--harness", Placeholder: "<id>", Summary: "Select the adapter; value validated against the catalog", ConsumesValue: true},
	{Name: "--format", Placeholder: "<text|json>", Summary: "Report rendering", ConsumesValue: true},
	{Name: "--fixtures", Placeholder: "<dir>", Summary: "Fixture root for $ref resolution", ConsumesValue: true},
	{Name: "--workspace-root", Placeholder: "<dir>", Summary: "Where sandboxes are created", ConsumesValue: true},
	{Name: "--timeout", Placeholder: "<duration>", Summary: "Per-attempt timeout override", ConsumesValue: true},
	{Name: "--repetitions", Placeholder: "<n>", Summary: "Repetition count override", ConsumesValue: true},
	{Name: "--max-concurrent-runs", Placeholder: "<n>", Summary: "Max runs executing at once across the whole suite (>= 1)", ConsumesValue: true},
	{Name: "--tui", Summary: "Launch the interactive frontend", ConsumesValue: false},
	{Name: "--logger-bundle", Placeholder: "<dir>", Summary: "Logger bundle directory override", ConsumesValue: true},
	{Name: "--cost-tool", Placeholder: "<path>", Summary: "Log-analysis tool path override", ConsumesValue: true},
	{Name: "--deploy-tool", Placeholder: "<path>", Summary: "Deployment tool path override", ConsumesValue: true},
	{Name: "--mosaic-root", Placeholder: "<dir>", Summary: "MOSAIC repository root override for the deployment tool", ConsumesValue: true},
	{Name: "--catalog-folder", Placeholder: "<dir>", Summary: "Catalogue directory override for the deployment tool", ConsumesValue: true},
	{Name: "--suites", Placeholder: "<path>", Summary: "Suite root or suite file for interactive discovery; pre-scanned by the composition root", ConsumesValue: true},
	{Name: "--subject-model", Placeholder: "<id>", Summary: "Model the subject runs on; validated against the selected harness's catalog", ConsumesValue: true},
	{Name: "--stub-model", Placeholder: "<id>", Summary: "Model every stub runs on; absent means the subject's model", ConsumesValue: true},
	{Name: "--report-path", Placeholder: "<path>", Summary: "Override the JSON report file location", ConsumesValue: true},
	{Name: "--no-report", Summary: "Suppress the JSON report file entirely", ConsumesValue: false},
	{Name: "--keep-sandbox", Summary: "Retain every sandbox for diagnosis", ConsumesValue: false},
	{Name: "--keep-sandbox-on-failure", Summary: "Retain a sandbox when the attempt failed", ConsumesValue: false},
	{Name: "--dir", Placeholder: "<folder>", Summary: "Directory of reports to store (store only)", ConsumesValue: true},
	{Name: "--for-version", Placeholder: "<ver>", Summary: "Restrict summary to one orchestrator version (summary only)", ConsumesValue: true},
	{Name: "--help", Summary: "Show this usage surface and exit (-h)", ConsumesValue: false},
}

// Commands returns the commands the usage surface documents, in display
// order. The interceptor route is deliberately absent and is never derived
// generically: the subject under test may read this binary's help text.
func Commands() []CommandSpec {
	out := make([]CommandSpec, len(commandSpecs))
	copy(out, commandSpecs)
	return out
}

// Flags returns every flag the usage surface documents, in display order.
func Flags() []FlagSpec {
	out := make([]FlagSpec, len(flagSpecs))
	copy(out, flagSpecs)
	return out
}

// ValueConsumingFlags returns the names of flags that consume a following
// token, derived from Flags. The composition root's positional detection
// consumes this instead of restating the list.
func ValueConsumingFlags() []string {
	var out []string
	for _, f := range flagSpecs {
		if f.ConsumesValue {
			out = append(out, f.Name)
		}
	}
	return out
}

// Usage writes the usage surface to w. Called for --help, -h, and a bare
// invocation with no arguments. The interceptor route is never written
// here — see Commands and flagSpecs above.
//
// Ungrouped commands appear first. Then for each distinct Group value (in
// order of first appearance), a blank line, a group-header line, and the
// grouped commands.
func Usage(w io.Writer, o Options) error {
	if _, err := fmt.Fprintln(w, "mosaic-agent-test — exercise a MOSAIC agent under an agent harness"); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, "\nUsage:\n  mosaic-agent-test <command> [flags]"); err != nil {
		return err
	}

	if _, err := fmt.Fprintln(w, "\nCommands:"); err != nil {
		return err
	}

	// Render ungrouped commands first.
	for _, c := range commandSpecs {
		if c.Group != "" {
			continue
		}
		if _, err := fmt.Fprintf(w, "  %s %s\n      %s\n", c.Name, c.Args, c.Summary); err != nil {
			return err
		}
	}

	// Render grouped commands under their group headers, in order of first
	// appearance. Collect group names preserving order.
	var groupOrder []string
	seenGroups := map[string]bool{}
	for _, c := range commandSpecs {
		if c.Group != "" && !seenGroups[c.Group] {
			groupOrder = append(groupOrder, c.Group)
			seenGroups[c.Group] = true
		}
	}
	for _, group := range groupOrder {
		if _, err := fmt.Fprintf(w, "\n  %s:\n", group); err != nil {
			return err
		}
		for _, c := range commandSpecs {
			if c.Group != group {
				continue
			}
			if _, err := fmt.Fprintf(w, "  %s %s\n      %s\n", c.Name, c.Args, c.Summary); err != nil {
				return err
			}
		}
	}

	if _, err := fmt.Fprintln(w, "\nFlags:"); err != nil {
		return err
	}
	for _, f := range flagSpecs {
		if f.Placeholder != "" {
			if _, err := fmt.Fprintf(w, "  %s %s\n      %s\n", f.Name, f.Placeholder, f.Summary); err != nil {
				return err
			}
			continue
		}
		if _, err := fmt.Fprintf(w, "  %s\n      %s\n", f.Name, f.Summary); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintln(w, "  -h\n      Shorthand for --help"); err != nil {
		return err
	}

	return nil
}
