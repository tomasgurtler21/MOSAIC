package main

// Integration coverage for the runner's provision-request completeness
// obligation (the request runner.setup builds must carry the interceptor
// subcommand and the resolved interpreter), driven end to end through this
// binary's own wiring seam (Deps.RunnerDeps, already proven field-faithful
// by wiring_test.go) and a real harness adapter — never the runner package's
// own stub.
//
// internal/runner's own tests (setup_test.go) prove the runner *populates*
// ProvisionRequest.InterceptorArgs and InterpreterCmd from Deps. These tests
// prove what a real harness adapter *does* with what the runner hands it:
// the registered hook argv a harness would actually invoke begins with the
// interceptor subcommand rather than falling through to the CLI frontend,
// and the resolved interpreter displaces the logger bundle's published
// placeholder in the generated configuration document. This is composition
// root territory rather than internal/runner's, because internal/runner may
// name no harness (AC13.9) — proving the fields reach a real adapter's
// generated output requires importing one, and this package is the one
// place a concrete adapter may be imported outside internal/harness itself.
//
// Every assertion below runs from inside the launcher callback: teardown
// (which removes everything Provision installed, via Deprovision) runs
// after Launch returns and before runner.Run itself returns, so a generated
// document can only be inspected from inside that callback — the same
// constraint internal/runner's own setup_test.go documents and follows.

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/fixtures"
	"mosaic-agent-test/internal/harness/claudecode"
	"mosaic-agent-test/internal/harness/opencode"
	"mosaic-agent-test/internal/preflight"
	"mosaic-agent-test/internal/runner"
	"mosaic-agent-test/internal/sideeffects"
	"mosaic-agent-test/internal/workspace"
)

// capturingLauncher is a domain.SubjectLauncher double whose Launch delegates
// to fn, letting a test inspect the sandbox setup has already populated
// before teardown removes it — the same pattern internal/runner's own tests
// use, reimplemented here since that package's unexported test helpers are
// not reachable from this one.
type capturingLauncher struct {
	fn func(ctx context.Context, plan domain.SpawnPlan) (domain.SubjectResult, error)
}

func (l capturingLauncher) Launch(ctx context.Context, plan domain.SpawnPlan) (domain.SubjectResult, error) {
	return l.fn(ctx, plan)
}

// realAdapterRunnerDeps wires a runner.Deps against a real harness adapter
// through this binary's own Deps.RunnerDeps seam, so these tests exercise
// exactly the projection production uses rather than a hand-assembled
// runner.Deps that happens to resemble it.
func realAdapterRunnerDeps(t *testing.T, adapter domain.HarnessAdapter, interpreterCmd string) runner.Deps {
	t.Helper()

	fixtureRoot := t.TempDir()
	resolver, err := fixtures.NewResolver(fixtureRoot)
	if err != nil {
		t.Fatalf("fixtures.NewResolver(%q): %v", fixtureRoot, err)
	}

	bundle := HarnessBundle{
		Adapter:        adapter,
		Decoder:        fakeDecoder,
		Launcher:       fakeLauncher{},
		Environment:    domain.EnvironmentReport{},
		InterpreterCmd: interpreterCmd,
	}

	d := Deps{
		HarnessFactory: fakeHarnessFactory{},
		Fixtures:       resolver,
		Effects:        sideeffects.NewApplier(resolver),
		Cost:           fakeCostProvider{},
		Clock:          fakeClock{},
		SelfPath:       "/abs/path/to/mosaic-agent-test",
	}

	ws := workspace.NewManager(t.TempDir(), d.Clock)
	return d.RunnerDeps(ws, fakeProgressSink{}, bundle)
}

// minimalRunnerRequest builds the smallest runner.Request a real adapter can
// provision and spawn a plan for.
func minimalRunnerRequest(testID string) runner.Request {
	return runner.Request{
		Key: domain.RunKey{RunID: "run-" + testID, TestName: testID, RunNumber: 1},
		Test: preflight.ResolvedTest{
			Definition: domain.TestDefinition{
				Name:  testID,
				Layer: domain.LayerSubagent,
				Subject: domain.SubjectUnderTest{
					Identity:       "researcher",
					DefinitionPath: "researcher.md",
					OpeningMessage: "start",
				},
			},
			Registry: domain.StubRegistry{TestID: testID},
		},
	}
}

// claudecodeFixtureBundleDir resolves the claudecode adapter package's own
// test fixture bundle, whose "claude-code" variant registers a PreToolUse
// entry invoking the published interpreter placeholder ("python3") — the
// exact document the interpreter-substitution test needs to observe
// displacement against.
func claudecodeFixtureBundleDir(t *testing.T) string {
	t.Helper()
	abs, err := filepath.Abs(filepath.Join("..", "..", "testdata", "claudecode", "fixture-bundle"))
	if err != nil {
		t.Fatalf("resolving claudecode fixture bundle dir: %v", err)
	}
	return abs
}

// extractJSONArrayLiteral finds "const <name> = [...]" in generated and
// returns the bracketed literal, so a test can decode it as JSON without
// parsing the surrounding generated TypeScript module.
func extractJSONArrayLiteral(generated, name string) (string, bool) {
	marker := "const " + name + " = "
	idx := strings.Index(generated, marker)
	if idx < 0 {
		return "", false
	}
	rest := generated[idx+len(marker):]
	end := strings.IndexByte(rest, '\n')
	if end < 0 {
		return "", false
	}
	line := strings.TrimSuffix(strings.TrimSpace(rest[:end]), ";")
	return line, true
}

// TestRun_ClaudeCodeRegisteredHookArgvBeginsWithInterceptorSubcommand
// asserts that, once the runner's provision request is complete, the
// claudecode adapter's generated settings.json registers the interceptor
// with an argv beginning with domain.InterceptorSubcommand — the harness
// invokes that command and, per selectFrontend's own routing rule, must
// reach the interceptor rather than falling through to the CLI frontend.
func TestRun_ClaudeCodeRegisteredHookArgvBeginsWithInterceptorSubcommand(t *testing.T) {
	deps := realAdapterRunnerDeps(t, claudecode.New(claudecode.Options{}), "")
	req := minimalRunnerRequest("claudecode-argv-routing")

	deps.Launcher = capturingLauncher{fn: func(ctx context.Context, plan domain.SpawnPlan) (domain.SubjectResult, error) {
		raw, err := os.ReadFile(filepath.Join(plan.WorkingDir, claudecode.SettingsRelPath))
		if err != nil {
			t.Errorf("reading generated settings.json: %v", err)
			return domain.SubjectResult{Disposition: domain.DispositionCompleted}, nil
		}
		settings, err := claudecode.ParseFragment(string(raw))
		if err != nil {
			t.Errorf("parsing generated settings.json: %v", err)
			return domain.SubjectResult{Disposition: domain.DispositionCompleted}, nil
		}

		entries := settings.Hooks["PreToolUse"]
		if len(entries) == 0 || len(entries[0].Hooks) == 0 {
			t.Errorf("generated settings.json has no PreToolUse entry; want the interceptor's own registration, got hooks=%+v", settings.Hooks)
			return domain.SubjectResult{Disposition: domain.DispositionCompleted}, nil
		}
		args := entries[0].Hooks[0].Args
		if len(args) == 0 || args[0] != domain.InterceptorSubcommand {
			t.Errorf("registered interceptor hook Args = %v, want it to begin with %q so the harness routes to the interceptor rather than falling through to the CLI frontend", args, domain.InterceptorSubcommand)
		}
		return domain.SubjectResult{Disposition: domain.DispositionCompleted}, nil
	}}

	if _, err := runner.Run(context.Background(), deps, req, nil); err != nil {
		t.Fatalf("runner.Run returned unexpected error: %v", err)
	}
}

// TestRun_OpenCodeRegisteredHookArgvBeginsWithInterceptorSubcommand is
// claudecode's sibling test above, driven against the opencode adapter's
// generated plugin instead of a settings document.
func TestRun_OpenCodeRegisteredHookArgvBeginsWithInterceptorSubcommand(t *testing.T) {
	deps := realAdapterRunnerDeps(t, opencode.New(opencode.Options{}), "")
	req := minimalRunnerRequest("opencode-argv-routing")

	deps.Launcher = capturingLauncher{fn: func(ctx context.Context, plan domain.SpawnPlan) (domain.SubjectResult, error) {
		raw, err := os.ReadFile(filepath.Join(plan.WorkingDir, opencode.PluginsRelDir, opencode.PluginFileName))
		if err != nil {
			t.Errorf("reading generated interceptor plugin: %v", err)
			return domain.SubjectResult{Disposition: domain.DispositionCompleted}, nil
		}

		preArgsJSON, ok := extractJSONArrayLiteral(string(raw), "PRE_ARGS")
		if !ok {
			t.Errorf("generated plugin does not declare a PRE_ARGS literal; got:\n%s", raw)
			return domain.SubjectResult{Disposition: domain.DispositionCompleted}, nil
		}
		var preArgs []string
		if err := json.Unmarshal([]byte(preArgsJSON), &preArgs); err != nil {
			t.Errorf("decoding PRE_ARGS literal %q: %v", preArgsJSON, err)
			return domain.SubjectResult{Disposition: domain.DispositionCompleted}, nil
		}
		if len(preArgs) == 0 || preArgs[0] != domain.InterceptorSubcommand {
			t.Errorf("generated plugin's PRE_ARGS = %v, want it to begin with %q so the harness routes to the interceptor rather than falling through to the CLI frontend", preArgs, domain.InterceptorSubcommand)
		}
		return domain.SubjectResult{Disposition: domain.DispositionCompleted}, nil
	}}

	if _, err := runner.Run(context.Background(), deps, req, nil); err != nil {
		t.Fatalf("runner.Run returned unexpected error: %v", err)
	}
}

// TestRun_ClaudeCodeProvisionedSettingsUseTheResolvedInterpreterNotThePublishedPlaceholder
// asserts that once the runner's provision request carries the resolved
// interpreter, the claudecode adapter's generated settings.json names it in
// place of the logger bundle's published placeholder ("python3") — the
// document the logger hooks actually run under.
func TestRun_ClaudeCodeProvisionedSettingsUseTheResolvedInterpreterNotThePublishedPlaceholder(t *testing.T) {
	const resolvedInterpreter = "py"
	deps := realAdapterRunnerDeps(t, claudecode.New(claudecode.Options{}), resolvedInterpreter)
	deps.LoggerBundleDir = claudecodeFixtureBundleDir(t)
	req := minimalRunnerRequest("claudecode-interpreter-substitution")

	deps.Launcher = capturingLauncher{fn: func(ctx context.Context, plan domain.SpawnPlan) (domain.SubjectResult, error) {
		raw, err := os.ReadFile(filepath.Join(plan.WorkingDir, claudecode.SettingsRelPath))
		if err != nil {
			t.Errorf("reading generated settings.json: %v", err)
			return domain.SubjectResult{Disposition: domain.DispositionCompleted}, nil
		}

		if strings.Contains(string(raw), claudecode.PublishedInterpreter) {
			t.Errorf("generated settings.json still references the bundle's published interpreter placeholder %q; want it displaced by the resolved interpreter %q:\n%s", claudecode.PublishedInterpreter, resolvedInterpreter, raw)
		}

		settings, err := claudecode.ParseFragment(string(raw))
		if err != nil {
			t.Errorf("parsing generated settings.json: %v", err)
			return domain.SubjectResult{Disposition: domain.DispositionCompleted}, nil
		}
		// Compose orders rewriting contributions (the interceptor) before
		// observational ones (the bundle) within an event key, so the
		// bundle's own PreToolUse entry is the second block.
		entries := settings.Hooks["PreToolUse"]
		if len(entries) < 2 || len(entries[1].Hooks) == 0 {
			t.Errorf("generated settings.json does not have the bundle's PreToolUse entry after the interceptor's own; got hooks=%+v", settings.Hooks)
			return domain.SubjectResult{Disposition: domain.DispositionCompleted}, nil
		}
		if got := entries[1].Hooks[0].Command; got != resolvedInterpreter {
			t.Errorf("bundle's registered hook command = %q, want the resolved interpreter %q", got, resolvedInterpreter)
		}
		return domain.SubjectResult{Disposition: domain.DispositionCompleted}, nil
	}}

	if _, err := runner.Run(context.Background(), deps, req, nil); err != nil {
		t.Fatalf("runner.Run returned unexpected error: %v", err)
	}
}
