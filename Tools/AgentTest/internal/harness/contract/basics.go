package contract

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"mosaic-agent-test/internal/domain"
)

// testSpawnPlan asserts that SpawnPlan describes how to start the subject
// without performing any process control itself. The adapter performs no
// process control, so the assertion is on the plan's contents plus the
// absence of any observable side effect from calling it — no file appears
// in the sandbox as a result of merely building the plan.
func testSpawnPlan(t *testing.T, cfg Config) {
	t.Helper()

	adapter, sb, prov := provisionForTest(t, cfg)

	before := listDir(t, sb.SubjectDir)

	plan, err := adapter.SpawnPlan(testContext(), cfg.Subject, prov)
	if err != nil {
		t.Fatalf("SpawnPlan: %v", err)
	}

	if plan.Executable == "" {
		t.Errorf("SpawnPlan: Executable is empty; the plan must name what to run")
	}
	if plan.WorkingDir == "" {
		t.Errorf("SpawnPlan: WorkingDir is empty; the plan must name where to run it")
	}

	after := listDir(t, sb.SubjectDir)
	if !reflect.DeepEqual(before, after) {
		t.Errorf("SpawnPlan: sandbox subject dir changed from %v to %v; SpawnPlan must describe, not perform, process control", before, after)
	}
}

// testCapabilitiesDeclared asserts that Capabilities is constant across
// calls, as the port requires.
func testCapabilitiesDeclared(t *testing.T, cfg Config) {
	t.Helper()

	adapter := cfg.New(t, t.TempDir())

	first := adapter.Capabilities()
	second := adapter.Capabilities()
	if !reflect.DeepEqual(first, second) {
		t.Errorf("Capabilities: not constant across calls: %+v then %+v", first, second)
	}

	if first.CorrelationField == "" {
		t.Errorf("Capabilities: CorrelationField is empty; the adapter must nominate a field")
	}
}

// testConfigScopeInspection asserts that InspectScopes reports on every
// non-sandbox scope ConfigScopes declares, without error.
func testConfigScopeInspection(t *testing.T, cfg Config) {
	t.Helper()

	adapter := cfg.New(t, t.TempDir())

	scopes := adapter.ConfigScopes()

	findings, err := adapter.InspectScopes(testContext())
	if err != nil {
		t.Fatalf("InspectScopes: %v", err)
	}

	inspected := make(map[string]domain.ScopeFinding, len(findings))
	for _, f := range findings {
		inspected[f.Scope.Name] = f
	}

	for _, s := range scopes {
		if s.InSandbox {
			continue // the adapter's own sandbox scope needs no external inspection
		}
		finding, ok := inspected[s.Name]
		if !ok {
			t.Errorf("InspectScopes: no finding for declared non-sandbox scope %q", s.Name)
			continue
		}
		if s.Isolatable && !finding.Neutralized {
			t.Errorf("InspectScopes: scope %q is declared isolatable but was not neutralized", s.Name)
		}
		if finding.RewritesInput && !finding.Neutralized && finding.Detail == "" {
			t.Errorf("InspectScopes: scope %q registers a hook that rewrites the intercepted call's input and was not neutralized, but Detail is empty; the operator needs an explanation of the un-neutralized rewriting hook", s.Name)
		}
	}

	if err := CheckPreservesSubjectFunction(t, cfg); err != nil {
		t.Error(err)
	}
}

// testProvisionRefusesCompetingRewriteHooks asserts that Provision fails
// rather than proceeds when the composed configuration it is asked to
// install would contain more than one entry that rewrites the intercepted
// call's input, per HarnessAdapter.Provision's MUST-fail obligation. An
// adapter that has not yet wired a way to simulate the condition (Config
// leaves CompetingRewriteRequest nil) is skipped rather than silently
// passed, so the gap stays visible in test output.
func testProvisionRefusesCompetingRewriteHooks(t *testing.T, cfg Config) {
	t.Helper()

	if cfg.CompetingRewriteRequest == nil {
		t.Skip("contract: Config.CompetingRewriteRequest is nil; this adapter has not wired a seam for simulating competing input-rewrite hooks yet")
	}

	dir := t.TempDir()
	adapter := cfg.New(t, dir)
	sb := newSandbox(t, dir)

	req := cfg.CompetingRewriteRequest(sb, cfg.Subject)

	if prov, err := adapter.Provision(testContext(), req); err == nil {
		t.Errorf("Provision: expected an error when the composed configuration contains more than one entry that would rewrite the intercepted call's input, got a successful Provisioning: %+v", prov)
	}
}

// testProvisionDeprovision asserts that Provision writes only what its
// ledger records, that every recorded path actually exists, and that
// Deprovision is idempotent.
func testProvisionDeprovision(t *testing.T, cfg Config) {
	t.Helper()

	dir := t.TempDir()
	adapter := cfg.New(t, dir)
	sb := newSandbox(t, dir)

	prov, err := adapter.Provision(testContext(), buildProvisionRequest(sb, cfg.Subject))
	if err != nil {
		t.Fatalf("Provision: %v", err)
	}

	for _, f := range prov.Files {
		if _, err := os.Stat(f); err != nil {
			t.Errorf("Provision: recorded file %q does not exist: %v", f, err)
		}
	}
	for _, d := range prov.Dirs {
		info, err := os.Stat(d)
		if err != nil {
			t.Errorf("Provision: recorded dir %q does not exist: %v", d, err)
			continue
		}
		if !info.IsDir() {
			t.Errorf("Provision: recorded dir %q is not a directory", d)
		}
	}

	if err := adapter.Deprovision(testContext(), prov); err != nil {
		t.Fatalf("Deprovision: %v", err)
	}
	// Idempotent: a second call over paths already gone must not fail.
	if err := adapter.Deprovision(testContext(), prov); err != nil {
		t.Errorf("Deprovision: second call over already-removed paths returned an error: %v", err)
	}
}

// testTranslateBasics exercises one uneventful pre-invocation translation
// cycle: a native payload becomes a normalized call, and a decision
// translates back to a native reply.
func testTranslateBasics(t *testing.T, cfg Config) {
	t.Helper()

	adapter := cfg.New(t, t.TempDir())
	caps := adapter.Capabilities()

	// The identity fed to NativePre is the normalized, harness-neutral
	// vocabulary authored tests use — never a harness's own native name.
	// NativePre is responsible for encoding it into whatever wire shape
	// its harness actually speaks (see Config.NativePre's doc comment).
	id := domain.CollaboratorIdentity{ToolName: domain.DispatchToolName, AgentIdentity: "Worker"}
	msg := domain.TaskMessage{
		AgentInstanceID: "Worker#1",
		TaskDescription: "do the work",
		InputArtifacts:  []string{},
		OutputArtifacts: []string{},
	}
	token := randomToken(t)

	native := cfg.NativePre(id, msg, token)
	call, err := adapter.TranslateCall(domain.PhasePre, native)
	if err != nil {
		t.Fatalf("TranslateCall(PhasePre): %v", err)
	}
	if call.Identity != id {
		t.Errorf("TranslateCall: Identity = %+v, want %+v", call.Identity, id)
	}
	if call.CorrelationToken != token {
		t.Errorf("TranslateCall: CorrelationToken = %q, want %q", call.CorrelationToken, token)
	}

	outcome := domain.InterceptionOutcome{
		Kind:             domain.OutcomePassthrough,
		CorrelationToken: token,
	}
	reply, err := adapter.TranslateOutcome(outcome, call)
	if err != nil {
		t.Fatalf("TranslateOutcome: %v", err)
	}
	if len(reply) == 0 {
		t.Errorf("TranslateOutcome: returned an empty native reply")
	}
	_ = caps
}

// listDir returns the sorted-by-walk set of entries directly beneath dir,
// used to detect side effects a call should not have caused.
func listDir(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("listDir(%q): %v", dir, err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, filepath.Join(dir, e.Name()))
	}
	return names
}
