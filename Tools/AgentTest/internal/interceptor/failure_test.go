package interceptor_test

// Coverage: T12.3/AC12.2/AC12.3 — failure-containment tests, one per failure
// class. Every one asserts the same two things: a valid native reply is
// still produced, and the subject's turn is not crashed or left hanging
// (exit code 0, decodable reply). The absent/corrupt/unreadable state
// classes additionally assert the diagnostics distinguish them, because
// conflating "setup never ran" with "setup ran and something broke" makes
// every later diagnosis wrong.

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/fixtures"
	"mosaic-agent-test/internal/harness/fake"
	"mosaic-agent-test/internal/interceptor"
	"mosaic-agent-test/internal/invlog"
	"mosaic-agent-test/internal/runstate"
	"mosaic-agent-test/internal/sideeffects"
)

// bareConfig assembles a Config against a controlDir that has not been
// through newHarness's store.Initialize, so a test can plant exactly the
// on-disk condition its failure class needs.
func bareConfig(t *testing.T, controlDir, subjectDir string, clock *fakeClock, registry domain.StubRegistry, adapter domain.HarnessAdapter) interceptor.Config {
	t.Helper()
	store := runstate.NewStore(controlDir, clock)
	log := invlog.NewLog(filepath.Join(controlDir, domain.InvocationLogName))
	resolver, err := fixtures.NewResolver(t.TempDir())
	if err != nil {
		t.Fatalf("NewResolver returned unexpected error: %v", err)
	}
	applier := sideeffects.NewApplier(resolver)

	return interceptor.Config{
		ControlDir: controlDir,
		SubjectDir: subjectDir,
		Adapter:    adapter,
		State:      store,
		Log:        log,
		Effects:    applier,
		Clock:      clock,
		Registry:   registry,
		TestID:     "stage12-example",
		RunNumber:  1,
	}
}

func assertContained(t *testing.T, exitCode int, reply, diag []byte) {
	t.Helper()
	if exitCode != 0 {
		t.Fatalf("Run returned exit code %d, want 0 — a contained failure must never crash or leave the subject's turn unanswered", exitCode)
	}
	got := decodeReply(t, reply)
	if got.Kind == "" {
		t.Fatal("expected a valid outcome kind in the neutral reply")
	}
	if len(diag) == 0 {
		t.Error("expected a diagnostic to be written to Config.Diag")
	}
}

func TestRun_FailureContainment_StateAbsent(t *testing.T) {
	subjectDir, controlDir := newSandboxDirs(t)
	clock := newFakeClock()
	adapter := fake.New(fake.Options{Capabilities: domain.HarnessCapabilities{SupportsDirectSubstitution: true}})
	cfg := bareConfig(t, controlDir, subjectDir, clock, domain.StubRegistry{OnUnmatched: domain.UnmatchedHalt}, adapter)
	// Deliberately never call store.Initialize: no state.json exists.

	id := domain.CollaboratorIdentity{ToolName: "Task", AgentIdentity: "researcher"}
	native := encodePre(t, id, "corr-1", "researcher#1", "do work")

	reply, exitCode, diag := run(t, cfg, domain.PhasePre, native)
	assertContained(t, exitCode, reply, diag)

	records := readLog(t, cfg.Log)
	if len(records) == 0 {
		t.Fatal("expected a diagnostic record to be appended to the invocation log")
	}
	last := records[len(records)-1]
	if !strings.Contains(strings.ToLower(last.Detail), "absent") {
		t.Errorf("record Detail = %q, want it to name the state as absent — distinctly from corrupt or unreadable", last.Detail)
	}
}

func TestRun_FailureContainment_StateCorrupt(t *testing.T) {
	subjectDir, controlDir := newSandboxDirs(t)
	clock := newFakeClock()
	adapter := fake.New(fake.Options{Capabilities: domain.HarnessCapabilities{SupportsDirectSubstitution: true}})
	cfg := bareConfig(t, controlDir, subjectDir, clock, domain.StubRegistry{OnUnmatched: domain.UnmatchedHalt}, adapter)
	if err := cfg.State.Initialize(baseState()); err != nil {
		t.Fatalf("Initialize returned unexpected error: %v", err)
	}
	if err := os.WriteFile(filepath.Join(controlDir, domain.StateFileName), []byte("{not valid json"), 0o644); err != nil {
		t.Fatalf("failed to corrupt the state document: %v", err)
	}

	id := domain.CollaboratorIdentity{ToolName: "Task", AgentIdentity: "researcher"}
	native := encodePre(t, id, "corr-1", "researcher#1", "do work")

	reply, exitCode, diag := run(t, cfg, domain.PhasePre, native)
	assertContained(t, exitCode, reply, diag)

	records := readLog(t, cfg.Log)
	if len(records) == 0 {
		t.Fatal("expected a diagnostic record to be appended to the invocation log")
	}
	last := records[len(records)-1]
	if !strings.Contains(strings.ToLower(last.Detail), "corrupt") {
		t.Errorf("record Detail = %q, want it to name the state as corrupt — distinctly from absent or unreadable", last.Detail)
	}
}

func TestRun_FailureContainment_StateUnreadable(t *testing.T) {
	subjectDir, controlDir := newSandboxDirs(t)
	clock := newFakeClock()
	adapter := fake.New(fake.Options{Capabilities: domain.HarnessCapabilities{SupportsDirectSubstitution: true}})
	cfg := bareConfig(t, controlDir, subjectDir, clock, domain.StubRegistry{OnUnmatched: domain.UnmatchedHalt}, adapter)
	// A directory where the state document must be makes it exist but be
	// unreadable as a file — a condition distinct from "absent" (os.IsNotExist
	// is false) and from "corrupt" (the document was never even parseable
	// bytes).
	if err := os.MkdirAll(filepath.Join(controlDir, domain.StateFileName), 0o755); err != nil {
		t.Fatalf("failed to plant an unreadable state path: %v", err)
	}

	id := domain.CollaboratorIdentity{ToolName: "Task", AgentIdentity: "researcher"}
	native := encodePre(t, id, "corr-1", "researcher#1", "do work")

	reply, exitCode, diag := run(t, cfg, domain.PhasePre, native)
	assertContained(t, exitCode, reply, diag)

	records := readLog(t, cfg.Log)
	if len(records) == 0 {
		t.Fatal("expected a diagnostic record to be appended to the invocation log")
	}
	last := records[len(records)-1]
	if !strings.Contains(strings.ToLower(last.Detail), "unreadable") {
		t.Errorf("record Detail = %q, want it to name the state as unreadable — distinctly from absent or corrupt", last.Detail)
	}
}

func TestRun_FailureContainment_LockAcquisitionTimesOut(t *testing.T) {
	h := newHarness(t, baseState(), domain.StubRegistry{OnUnmatched: domain.UnmatchedHalt}, domain.HarnessCapabilities{SupportsDirectSubstitution: true}, nil)

	// Hold the lock from a second store on the same control directory,
	// indefinitely, so the call under test must contend for the entire
	// acquisition window.
	holder := runstate.NewStore(h.ControlDir, h.Clock)
	release := make(chan struct{})
	holderStarted := make(chan struct{})
	holderDone := make(chan struct{})
	go func() {
		defer close(holderDone)
		_, _ = holder.Update(func(s domain.RunState) (domain.RunState, error) {
			close(holderStarted)
			<-release
			return s, nil
		})
	}()
	<-holderStarted
	defer func() {
		close(release)
		<-holderDone
	}()

	stopTicking := h.Clock.startTicking(500 * time.Millisecond) // fast-forward past the 5s acquisition timeout
	defer stopTicking()

	id := domain.CollaboratorIdentity{ToolName: "Task", AgentIdentity: "researcher"}
	native := encodePre(t, id, "corr-1", "researcher#1", "do work")

	reply, exitCode, diag := run(t, h.Config, domain.PhasePre, native)
	assertContained(t, exitCode, reply, diag)
}

func TestRun_FailureContainment_PayloadTheAdapterRejects(t *testing.T) {
	h := newHarness(t, baseState(), domain.StubRegistry{OnUnmatched: domain.UnmatchedHalt}, domain.HarnessCapabilities{SupportsDirectSubstitution: true}, nil)
	malformed := []byte(`{"phase":"pre"}`) // no tool, no token: the fake adapter's TranslateCall rejects this

	reply, exitCode, diag := run(t, h.Config, domain.PhasePre, malformed)
	assertContained(t, exitCode, reply, diag)

	state := readState(t, h.Store)
	if state.SequenceCounter != 0 {
		t.Errorf("a payload the adapter rejects must never reach state: SequenceCounter = %d, want 0", state.SequenceCounter)
	}
}

func TestRun_FailureContainment_SideEffectWriteFailing(t *testing.T) {
	id := domain.CollaboratorIdentity{ToolName: "Task", AgentIdentity: "researcher"}
	registry := domain.StubRegistry{
		TestID:      "stage12-example",
		OnUnmatched: domain.UnmatchedHalt,
		Stubs: []domain.Stub{{
			Match:       domain.StubMatch{Identity: id, Invocation: 1},
			Response:    json.RawMessage(`{"status_code":"SUCCESS"}`),
			SideEffects: []domain.FileEffect{{Path: "../escape.txt", Content: "must never be written"}},
		}},
	}
	h := newHarness(t, baseState(), registry, domain.HarnessCapabilities{SupportsDirectSubstitution: true}, nil)
	native := encodePre(t, id, "corr-1", "researcher#1", "do work")

	reply, exitCode, diag := run(t, h.Config, domain.PhasePre, native)
	assertContained(t, exitCode, reply, diag)
}

// panickingAdapter wraps a working adapter but panics inside translation, so
// a test can exercise the "unexpected panic anywhere inside" failure class
// without needing to reach into the shell's own internals.
type panickingAdapter struct {
	inner domain.HarnessAdapter
}

var _ domain.HarnessAdapter = panickingAdapter{}

func (p panickingAdapter) ID() string                               { return p.inner.ID() }
func (p panickingAdapter) Capabilities() domain.HarnessCapabilities { return p.inner.Capabilities() }
func (p panickingAdapter) ConfigScopes() []domain.ConfigScope       { return p.inner.ConfigScopes() }
func (p panickingAdapter) InspectScopes(ctx context.Context) ([]domain.ScopeFinding, error) {
	return p.inner.InspectScopes(ctx)
}
func (p panickingAdapter) Provision(ctx context.Context, req domain.ProvisionRequest) (domain.Provisioning, error) {
	return p.inner.Provision(ctx, req)
}
func (p panickingAdapter) Deprovision(ctx context.Context, prov domain.Provisioning) error {
	return p.inner.Deprovision(ctx, prov)
}
func (p panickingAdapter) SpawnPlan(ctx context.Context, subject domain.SubjectUnderTest, prov domain.Provisioning) (domain.SpawnPlan, error) {
	return p.inner.SpawnPlan(ctx, subject, prov)
}
func (p panickingAdapter) TranslateCall(phase domain.InterceptionPhase, native []byte) (domain.InterceptedCall, error) {
	panic("simulated unexpected panic inside translation")
}
func (p panickingAdapter) TranslateOutcome(outcome domain.InterceptionOutcome, call domain.InterceptedCall) ([]byte, error) {
	return p.inner.TranslateOutcome(outcome, call)
}
func (p panickingAdapter) CheckEnvironment(ctx context.Context) (domain.EnvironmentReport, error) {
	return p.inner.CheckEnvironment(ctx)
}

func TestRun_FailureContainment_UnexpectedPanic(t *testing.T) {
	h := newHarness(t, baseState(), domain.StubRegistry{OnUnmatched: domain.UnmatchedHalt}, domain.HarnessCapabilities{SupportsDirectSubstitution: true}, nil)
	cfg := h.Config
	cfg.Adapter = panickingAdapter{inner: h.Adapter}

	id := domain.CollaboratorIdentity{ToolName: "Task", AgentIdentity: "researcher"}
	native := encodePre(t, id, "corr-1", "researcher#1", "do work")

	// If Run does not recover the panic, this call itself crashes the test
	// binary rather than merely failing this test — that failure mode is
	// exactly what this test exists to catch.
	reply, exitCode, diag := run(t, cfg, domain.PhasePre, native)
	assertContained(t, exitCode, reply, diag)
}
