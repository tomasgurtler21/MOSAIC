package cli_test

// Stage 7 (Sandbox Retention): the CLI's --keep-sandbox and
// --keep-sandbox-on-failure flags, resolving into cli.RunConfig.Retention —
// the field newSuiteRunner (composition root) threads into
// suite.Options.Retention and, through it, into every attempt's
// runner.Request.Retention (see ContractsDesign.md's "runner.Deps and
// runner.Request (extended)" and "suite.Options (extended)" sections). This
// package's own obligation, mirroring how --logger-bundle/--cost-tool and
// --harness are each tested in their own file, is narrower: recognise both
// flags rather than reject them as unknown, and resolve them into
// RunConfig.Retention exactly as the command-surface table in
// ContractsDesign.md specifies.

import (
	"bytes"
	"context"
	"testing"

	"mosaic-agent-test/internal/cli"
	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/preflight"
)

// TestRunCommand_KeepSandboxFlags_AreRecognised asserts neither
// --keep-sandbox nor --keep-sandbox-on-failure, alone or together, ever
// surfaces as an unknown flag.
func TestRunCommand_KeepSandboxFlags_AreRecognised(t *testing.T) {
	forms := map[string][]string{
		"keep-sandbox":            {"run", "suite.yaml", "--keep-sandbox"},
		"keep-sandbox-on-failure": {"run", "suite.yaml", "--keep-sandbox-on-failure"},
		"both":                    {"run", "suite.yaml", "--keep-sandbox", "--keep-sandbox-on-failure"},
	}

	for name, args := range forms {
		t.Run(name, func(t *testing.T) {
			stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
			runner := &fakeSuiteRunner{result: passingResult("orchestrator-routing")}
			opts := baseOptions(stdout, stderr, scriptedPreflight(preflight.Plan{}, cleanReport(), nil), runner)

			got := cli.Execute(context.Background(), args, opts)

			if got == cli.ExitUsage {
				t.Errorf("%s form: retention flag(s) were rejected as unknown (exit = ExitUsage); stderr=%s", name, stderr.String())
			}
		})
	}
}

// TestRunCommand_RetentionFlags_ResolveToTheDocumentedPolicy drives every
// combination the command-surface table names — none, either flag alone,
// and both together — and asserts the SuiteFactory's RunConfig carries the
// resolved domain.RetentionPolicy, never a value derived a second way.
func TestRunCommand_RetentionFlags_ResolveToTheDocumentedPolicy(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want domain.RetentionPolicy
	}{
		{name: "neither flag", args: []string{"run", "suite.yaml"}, want: domain.RetainNever},
		{name: "--keep-sandbox-on-failure alone", args: []string{"run", "suite.yaml", "--keep-sandbox-on-failure"}, want: domain.RetainOnFailure},
		{name: "--keep-sandbox alone", args: []string{"run", "suite.yaml", "--keep-sandbox"}, want: domain.RetainAlways},
		{name: "both flags together", args: []string{"run", "suite.yaml", "--keep-sandbox", "--keep-sandbox-on-failure"}, want: domain.RetainAlways},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
			factory := &fakeSuiteFactory{runner: &fakeSuiteRunner{result: passingResult("orchestrator-routing")}}
			opts := baseOptionsWithFactory(stdout, stderr, scriptedPreflight(preflight.Plan{}, cleanReport(), nil), factory.build)

			cli.Execute(context.Background(), tc.args, opts)

			if got := factory.lastConfig().Retention; got != tc.want {
				t.Errorf("RunConfig.Retention = %q, want %q (stderr=%s)", got, tc.want, stderr.String())
			}
		})
	}
}

// TestFlags_IncludesTheRetentionFlags asserts the shared flag specification
// — the single source the parser, the composition root's positional
// detection and the usage text all derive from — documents both flags, so a
// user running --help ever learns they exist.
func TestFlags_IncludesTheRetentionFlags(t *testing.T) {
	names := map[string]bool{}
	for _, f := range cli.Flags() {
		names[f.Name] = true
	}
	for _, want := range []string{"--keep-sandbox", "--keep-sandbox-on-failure"} {
		if !names[want] {
			t.Errorf("cli.Flags() does not include %q", want)
		}
	}
}
