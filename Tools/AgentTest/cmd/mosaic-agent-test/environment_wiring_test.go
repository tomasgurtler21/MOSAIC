package main

// Tests for the composition-root wiring seam T3.4 names explicitly: a
// problem-bearing CheckEnvironment report must flow into a rejected
// preflight before any subject is spawned. environmentBakedPreflight is the
// exact function that owns this seam ("sets in.Environment = env before
// delegating to preflight.Validate"), so it is exercised directly rather
// than only through preflight.Validate's own suite (which proves the
// diagnostic-emission half in isolation) or claudecode's CheckEnvironment
// suite (which proves the problem-detection half in isolation). Neither of
// those proves the wiring between them actually holds.
//
// A regression that silently dropped "in.Environment = env" inside
// environmentBakedPreflight — leaving in.Environment at its zero value no
// matter what report buildDeps resolved — would pass every other test in
// this stage yet defeat AC3.5's "before any subject is spawned" guarantee at
// the one place it is supposed to be enforced. These tests fail if that
// regression is introduced.
//
// This file also covers the deployer-baking half of environmentBakedPreflight
// (added in Stage 9): the deployer and scratch root passed as arguments must
// be the ones preflight.Validate receives, and a caller-supplied Deploy on the
// Input must not leak through in place of the baked-in one.

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/preflight"
)

// writeMinimalSuiteTree materializes the smallest suite tree preflight.Validate
// accepts without any authoring or fixture errors of its own, so a test can
// isolate the environment-diagnostic behaviour from every other diagnostic
// Validate can produce. Validate returns before running any check
// (including the environment one) when the suite file itself cannot be
// read, so a valid tree is required to reach environmentBakedPreflight's
// contribution at all.
func writeMinimalSuiteTree(t *testing.T) (suitePath, fixtureRoot string) {
	t.Helper()
	root := t.TempDir()

	const suite = `
schema_version: 1
id: s
tests:
  - path: happy-path.test.yaml
`
	const definition = `
schema_version: 1
id: happy-path
layer: orchestrator
subject:
  identity: orchestrator
  definition: .claude/agents/orchestrator.md
stub_registry: happy-path.stubs.json
assertions:
  final_state:
    phase: COMPLETED
    last_status: SUCCESS
`
	const registry = `{
  "schema_version": 1,
  "test_id": "happy-path",
  "on_unmatched": "halt",
  "stubs": [
    { "match": { "tool": "Task", "agent": "researcher", "invocation": 1 },
      "response": { "status_code": "SUCCESS" } }
  ]
}`

	files := map[string]string{
		"s.suite.yaml":          suite,
		"happy-path.test.yaml":  definition,
		"happy-path.stubs.json": registry,
	}
	for rel, content := range files {
		full := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("MkdirAll(%q): %v", filepath.Dir(full), err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatalf("WriteFile(%q): %v", full, err)
		}
	}

	return filepath.Join(root, "s.suite.yaml"), filepath.Join(root, "fixtures")
}

// TestEnvironmentBakedPreflight_ProblemsSurfaceAsPreflightDiagnostic asserts
// the composition-root seam end-to-end: a problem-bearing EnvironmentReport
// given to environmentBakedPreflight reaches preflight.Validate and comes
// back out as an "environment-unusable" error diagnostic, carrying the
// problem's own Detail verbatim — even though the caller's own
// preflight.Input never set Environment itself. This is only possible if
// environmentBakedPreflight actually assigns in.Environment = env, which is
// the exact statement this test pins.
func TestEnvironmentBakedPreflight_ProblemsSurfaceAsPreflightDiagnostic(t *testing.T) {
	suitePath, fixtureRoot := writeMinimalSuiteTree(t)

	const detail = `no usable interpreter found: tried python3, py, python; install one of these or set an interpreter explicitly`
	envReport := domain.EnvironmentReport{
		Problems: []domain.EnvironmentProblem{
			{Kind: domain.ProblemInterpreterUnresolved, Detail: detail},
		},
	}

	validate := environmentBakedPreflight(envReport, nil, "")

	// Deliberately leave Environment unset on the caller's own Input: proving
	// the diagnostic appears anyway is what distinguishes this test from one
	// that would pass even if environmentBakedPreflight forgot to bake the
	// report in, so long as the caller happened to set it too.
	_, report := validate(preflight.Input{
		SuitePath:   suitePath,
		FixtureRoot: fixtureRoot,
		HarnessID:   "claude-code",
	})

	var found bool
	for _, d := range report.Diagnostics {
		if d.Code != "environment-unusable" {
			continue
		}
		found = true
		if d.Severity != "error" {
			t.Errorf("environment-unusable diagnostic Severity = %q, want %q", d.Severity, "error")
		}
		if !strings.Contains(d.Message, detail) {
			t.Errorf("environment-unusable diagnostic Message = %q, does not preserve the problem's Detail (%q) verbatim", d.Message, detail)
		}
	}
	if !found {
		t.Fatalf("environmentBakedPreflight(envReport) with a Problems-bearing report produced no %q diagnostic; diagnostics=%+v (AC3.5: a failing environment check must surface as a preflight error before any subject is spawned)", "environment-unusable", report.Diagnostics)
	}
}

// TestEnvironmentBakedPreflight_HealthyReportProducesNoEnvironmentDiagnostic
// is the positive case TestEnvironmentBakedPreflight_ProblemsSurfaceAsPreflightDiagnostic
// is proven against: an OK() report must not manufacture a diagnostic of its
// own, so the negative-path test above is proven against a wiring that can
// actually pass through cleanly, not one that always refuses.
func TestEnvironmentBakedPreflight_HealthyReportProducesNoEnvironmentDiagnostic(t *testing.T) {
	suitePath, fixtureRoot := writeMinimalSuiteTree(t)

	validate := environmentBakedPreflight(domain.EnvironmentReport{}, nil, "")

	_, report := validate(preflight.Input{
		SuitePath:   suitePath,
		FixtureRoot: fixtureRoot,
		HarnessID:   "claude-code",
	})

	for _, d := range report.Diagnostics {
		if d.Code == "environment-unusable" {
			t.Errorf("environmentBakedPreflight(EnvironmentReport{}) produced an unexpected %q diagnostic %+v for a report with no problems", "environment-unusable", d)
		}
	}
}

// TestEnvironmentBakedPreflight_OverridesAnyCallerSuppliedEnvironment
// asserts environmentBakedPreflight's own report — not whatever the caller's
// Input already carried — is what preflight.Validate sees. A frontend never
// constructs its own EnvironmentReport (per this seam's doc comment), so a
// stray non-zero Input.Environment must never leak into the result in place
// of the baked-in one.
func TestEnvironmentBakedPreflight_OverridesAnyCallerSuppliedEnvironment(t *testing.T) {
	suitePath, fixtureRoot := writeMinimalSuiteTree(t)

	// The closure is baked with a healthy report...
	validate := environmentBakedPreflight(domain.EnvironmentReport{}, nil, "")

	// ...but the caller's own Input carries a problem the closure must
	// override rather than merge or defer to.
	const staleDetail = "stale caller-supplied problem that must not surface"
	_, report := validate(preflight.Input{
		SuitePath:   suitePath,
		FixtureRoot: fixtureRoot,
		HarnessID:   "claude-code",
		Environment: domain.EnvironmentReport{
			Problems: []domain.EnvironmentProblem{
				{Kind: domain.ProblemInterpreterUnresolved, Detail: staleDetail},
			},
		},
	})

	for _, d := range report.Diagnostics {
		if d.Code == "environment-unusable" {
			t.Errorf("environmentBakedPreflight(EnvironmentReport{}, nil, \"\") let the caller-supplied Input.Environment leak through as diagnostic %+v; the baked-in report must be the one used", d)
		}
	}
}

// ---------------------------------------------------------------------------
// Deployer baking — T9.1 extension + T9.4 identity assertion
// ---------------------------------------------------------------------------

// writeMinimalSuiteTreeWithAgentDeclaration is writeMinimalSuiteTree's
// counterpart for tests that need preflight to reach the deploy-dependent
// checks. It writes a test definition using subject.agent (CatalogAgentKey)
// instead of the removed subject.definition, so checkDeploymentDeclarations
// proceeds past the field-inspection gate and calls Deploy.Render — making
// the deployer observable through a recording fake.
func writeMinimalSuiteTreeWithAgentDeclaration(t *testing.T) (suitePath, fixtureRoot string) {
	t.Helper()
	root := t.TempDir()

	const suite = `
schema_version: 1
id: s
tests:
  - path: happy-path.test.yaml
`
	const definition = `
schema_version: 1
id: happy-path
layer: orchestrator
subject:
  identity: orchestrator
  agent: some-agent
stub_registry: happy-path.stubs.json
assertions:
  final_state:
    phase: COMPLETED
    last_status: SUCCESS
`
	const registry = `{
  "schema_version": 1,
  "test_id": "happy-path",
  "on_unmatched": "halt",
  "stubs": [
    { "match": { "tool": "Task", "agent": "researcher", "invocation": 1 },
      "response": { "status_code": "SUCCESS" } }
  ]
}`

	files := map[string]string{
		"s.suite.yaml":          suite,
		"happy-path.test.yaml":  definition,
		"happy-path.stubs.json": registry,
	}
	for rel, content := range files {
		full := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("MkdirAll(%q): %v", filepath.Dir(full), err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatalf("WriteFile(%q): %v", full, err)
		}
	}

	return filepath.Join(root, "s.suite.yaml"), filepath.Join(root, "fixtures")
}

// fakeRecordingDeployer implements domain.AgentDeployer and records every
// Render call it receives. Tests pass it to environmentBakedPreflight and
// later inspect its calls to verify the deployer baked into the closure is the
// same object that preflight.Validate actually calls. The error it returns is
// a plain error (not ErrToolUnavailable or ErrTimedOut), so preflight turns it
// into an unrenderable-subject-declaration diagnostic rather than an
// environment-unusable one — making the call observable without triggering the
// tool-failure path that would short-circuit the check loop.
type fakeRecordingDeployer struct {
	calls []domain.RenderAgentRequest
}

func (f *fakeRecordingDeployer) Render(_ context.Context, req domain.RenderAgentRequest) (domain.RenderAgentResult, error) {
	f.calls = append(f.calls, req)
	return domain.RenderAgentResult{}, errors.New("fakeRecordingDeployer: intentional failure for wiring assertion")
}

// Deploy is a stub that satisfies domain.AgentDeployer. Tests in this package
// drive the deployer only through Render; Deploy support is added here so the
// interface remains satisfied while Deploy's implementation is pending.
func (f *fakeRecordingDeployer) Deploy(_ context.Context, _ domain.DeployRequest) (domain.DeployResult, error) {
	return domain.DeployResult{}, errors.New("fakeRecordingDeployer: Deploy not implemented in this test double")
}

// TestEnvironmentBakedPreflight_DeployerIsCalledWhenSubjectHasAgentKey asserts
// that the deployer passed to environmentBakedPreflight is the one
// preflight.Validate actually invokes when a test definition carries a
// catalogue agent key. This is the load-bearing wiring assertion
// ContractsDesign.md requires: a nil Deploy silently skips every new
// declaration check, so the only way to confirm the deployer is non-nil and
// the correct one is to observe the call. A regression that dropped the dep
// argument inside the closure — leaving in.Deploy nil — would fail this test
// with "fakeRecordingDeployer was not called".
func TestEnvironmentBakedPreflight_DeployerIsCalledWhenSubjectHasAgentKey(t *testing.T) {
	suitePath, fixtureRoot := writeMinimalSuiteTreeWithAgentDeclaration(t)

	dep := &fakeRecordingDeployer{}
	validate := environmentBakedPreflight(domain.EnvironmentReport{}, dep, "C:/scratch")

	// Deliberately leave Deploy unset on the caller's own Input: if the
	// deployer is called, it must have come from the baked-in dep argument,
	// not from the caller setting Input.Deploy themselves.
	validate(preflight.Input{
		SuitePath:   suitePath,
		FixtureRoot: fixtureRoot,
		HarnessID:   "claude-code",
	})

	if len(dep.calls) == 0 {
		t.Fatal("environmentBakedPreflight(env, dep, scratchRoot) with a subject declaring agent key \"some-agent\": " +
			"fakeRecordingDeployer.Render was not called; the deployer must be baked as Input.Deploy and invoked by preflight.Validate")
	}
}

// TestEnvironmentBakedPreflight_ScratchRootIsBakedAsWorkspaceRoot asserts
// that the scratchRoot argument to environmentBakedPreflight reaches
// preflight.Validate as Input.DeployScratchRoot and appears as the
// WorkspaceRoot in every dry-run render call. The doc comment on
// preflight.Input.DeployScratchRoot states "it exists as a field rather than
// a constant so a test can assert the value the port received" — this is that
// assertion.
func TestEnvironmentBakedPreflight_ScratchRootIsBakedAsWorkspaceRoot(t *testing.T) {
	suitePath, fixtureRoot := writeMinimalSuiteTreeWithAgentDeclaration(t)

	const wantScratchRoot = "C:/test-scratch-root"
	dep := &fakeRecordingDeployer{}
	validate := environmentBakedPreflight(domain.EnvironmentReport{}, dep, wantScratchRoot)

	validate(preflight.Input{
		SuitePath:   suitePath,
		FixtureRoot: fixtureRoot,
		HarnessID:   "claude-code",
	})

	if len(dep.calls) == 0 {
		t.Fatal("environmentBakedPreflight: deployer was not called; cannot assert that the scratchRoot was baked as WorkspaceRoot")
	}
	for i, call := range dep.calls {
		if call.WorkspaceRoot != wantScratchRoot {
			t.Errorf("call[%d].WorkspaceRoot = %q, want the baked scratchRoot %q (the scratchRoot argument must reach preflight.Validate as Input.DeployScratchRoot)", i, call.WorkspaceRoot, wantScratchRoot)
		}
	}
}

// TestEnvironmentBakedPreflight_DeployerOverridesAnyCallerSuppliedDeploy
// mirrors TestEnvironmentBakedPreflight_OverridesAnyCallerSuppliedEnvironment
// for the Deploy field: a deployer already set on the caller's Input must not
// leak through in place of the one baked into the closure. A frontend that
// inadvertently set Input.Deploy must still be overridden, so the composition
// root remains the sole site for this dependency — matching the guarantee
// already established for the environment report.
func TestEnvironmentBakedPreflight_DeployerOverridesAnyCallerSuppliedDeploy(t *testing.T) {
	suitePath, fixtureRoot := writeMinimalSuiteTreeWithAgentDeclaration(t)

	// bakedDep is the deployer the closure is built with — the one that must be called.
	bakedDep := &fakeRecordingDeployer{}
	// callerDep is what the caller places on its own Input — it must be overridden.
	callerDep := &fakeRecordingDeployer{}

	validate := environmentBakedPreflight(domain.EnvironmentReport{}, bakedDep, "C:/scratch")

	validate(preflight.Input{
		SuitePath:   suitePath,
		FixtureRoot: fixtureRoot,
		HarnessID:   "claude-code",
		Deploy:      callerDep,
	})

	if len(bakedDep.calls) == 0 {
		t.Error("environmentBakedPreflight: the baked deployer was not called; " +
			"the closure must always assign in.Deploy = dep regardless of what Input.Deploy already holds")
	}
	if len(callerDep.calls) > 0 {
		t.Errorf("environmentBakedPreflight: the caller-supplied Input.Deploy was called %d time(s); "+
			"the baked deployer must override any caller-supplied value rather than letting it leak through", len(callerDep.calls))
	}
}
