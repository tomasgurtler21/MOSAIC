package cli_test

// Tests for the two path-override flags this stage adds to the command
// surface: --logger-bundle and --cost-tool. Neither flag builds anything
// into preflight.Input — both are pre-scanned by the composition root ahead
// of either frontend's own parser, the same way --harness already is (see
// ContractsDesign.md's configuration resolution contract). What this
// package's own parser owes them is narrower: it must recognise both flags
// rather than reject them as unknown, in both accepted forms, on both
// commands.

import (
	"bytes"
	"context"
	"testing"

	"mosaic-agent-test/internal/cli"
	"mosaic-agent-test/internal/preflight"
)

// TestRunCommand_PathOverrideFlags_AreRecognised asserts --logger-bundle and
// --cost-tool never surface as an unknown flag on the run command, in either
// accepted form.
func TestRunCommand_PathOverrideFlags_AreRecognised(t *testing.T) {
	forms := map[string][]string{
		"space":  {"run", "suite.yaml", "--logger-bundle", "C:/bundles/logger", "--cost-tool", "C:/bin/mosaic-log-analyzer.exe"},
		"equals": {"run", "suite.yaml", "--logger-bundle=C:/bundles/logger", "--cost-tool=C:/bin/mosaic-log-analyzer.exe"},
	}

	for name, args := range forms {
		t.Run(name, func(t *testing.T) {
			stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
			runner := &fakeSuiteRunner{result: passingResult("orchestrator-routing")}
			opts := baseOptions(stdout, stderr, scriptedPreflight(preflight.Plan{}, cleanReport(), nil), runner)

			got := cli.Execute(context.Background(), args, opts)

			if got == cli.ExitUsage {
				t.Errorf("%s form: --logger-bundle/--cost-tool were rejected as unknown flags (exit = ExitUsage); stderr=%s", name, stderr.String())
			}
		})
	}
}

// TestValidateCommand_PathOverrideFlags_AreRecognised asserts the same for
// the validate command, since both commands share one flag parser.
func TestValidateCommand_PathOverrideFlags_AreRecognised(t *testing.T) {
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	runner := &fakeSuiteRunner{}
	opts := baseOptions(stdout, stderr, scriptedPreflight(preflight.Plan{}, cleanReport(), nil), runner)

	got := cli.Execute(context.Background(), []string{"validate", "suite.yaml", "--logger-bundle", "C:/bundles/logger", "--cost-tool", "C:/bin/mosaic-log-analyzer.exe"}, opts)

	if got == cli.ExitUsage {
		t.Errorf("validate: --logger-bundle/--cost-tool were rejected as unknown flags (exit = ExitUsage); stderr=%s", stderr.String())
	}
}
