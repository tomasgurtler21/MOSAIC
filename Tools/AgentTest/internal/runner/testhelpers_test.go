package runner_test

// Shared test doubles for the runner package's tests.
//
// The workspace manager, fixture resolver and side-effect applier are the
// real implementations from earlier stages — there is nothing harness- or
// process-shaped about them, so stubbing them would only hide bugs. Only the
// two spawning-related ports (domain.HarnessAdapter, domain.SubjectLauncher)
// and the cost port are stubbed here, exactly as T13.2 and T13.6 call for:
// the whole lifecycle is driven through a stub adapter and a stub launcher
// so no real process and no real harness is ever needed.

import (
	"context"
	"sync"
	"time"

	"mosaic-agent-test/internal/domain"
)

// fakeClock is a manually advanced domain.Clock, so time-dependent
// assertions never depend on real sleeping.
type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func newFakeClock() *fakeClock {
	return &fakeClock{now: time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)}
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

var _ domain.Clock = (*fakeClock)(nil)

// callRecorder records the order in which the stub collaborators below are
// invoked, so a test can assert on sequencing without inspecting internal
// runner state.
type callRecorder struct {
	mu    sync.Mutex
	calls []string
}

func (r *callRecorder) record(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, name)
}

func (r *callRecorder) all() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.calls...)
}

// stubAdapter is a minimal domain.HarnessAdapter double. It performs no
// harness-specific work and names no harness: it exists only to let a test
// drive the runner's setup/spawn sequence deterministically. Provision
// records the sandbox it was given, so a test can inspect setup content
// (registry, groups, seed files, state) from within a launcher callback
// that runs after setup and before teardown.
type stubAdapter struct {
	rec *callRecorder

	// provisioning is returned by Provision on success; its Sandbox field is
	// overwritten with the request's sandbox before returning.
	provisioning domain.Provisioning
	provisionErr error

	plan    domain.SpawnPlan
	planErr error

	deprovisionErr error

	// lastProvisionReq captures the most recent Provision call, so a test can
	// read back the sandbox setup actually created.
	lastProvisionReq domain.ProvisionRequest

	// lastSpawnPlanSubject captures the subject SpawnPlan was actually
	// handed, so a test can assert on run-id prompt injection without
	// inspecting internal runner state.
	lastSpawnPlanSubject domain.SubjectUnderTest
}

var _ domain.HarnessAdapter = (*stubAdapter)(nil)

func (a *stubAdapter) ID() string { return "stub-harness" }

func (a *stubAdapter) Capabilities() domain.HarnessCapabilities {
	return domain.HarnessCapabilities{}
}

func (a *stubAdapter) ConfigScopes() []domain.ConfigScope { return nil }

func (a *stubAdapter) InspectScopes(ctx context.Context) ([]domain.ScopeFinding, error) {
	return nil, nil
}

func (a *stubAdapter) Provision(ctx context.Context, req domain.ProvisionRequest) (domain.Provisioning, error) {
	a.rec.record("adapter.Provision")
	a.lastProvisionReq = req
	if a.provisionErr != nil {
		return domain.Provisioning{}, a.provisionErr
	}
	p := a.provisioning
	p.Sandbox = req.Sandbox
	return p, nil
}

func (a *stubAdapter) Deprovision(ctx context.Context, p domain.Provisioning) error {
	a.rec.record("adapter.Deprovision")
	return a.deprovisionErr
}

func (a *stubAdapter) SpawnPlan(ctx context.Context, subject domain.SubjectUnderTest, p domain.Provisioning) (domain.SpawnPlan, error) {
	a.rec.record("adapter.SpawnPlan")
	a.lastSpawnPlanSubject = subject
	if a.planErr != nil {
		return domain.SpawnPlan{}, a.planErr
	}
	return a.plan, nil
}

func (a *stubAdapter) TranslateCall(phase domain.InterceptionPhase, native []byte) (domain.InterceptedCall, error) {
	return domain.InterceptedCall{}, errNotUsedByRunnerTests
}

func (a *stubAdapter) TranslateOutcome(outcome domain.InterceptionOutcome, call domain.InterceptedCall) ([]byte, error) {
	return nil, errNotUsedByRunnerTests
}

// CheckEnvironment implements domain.HarnessAdapter. Not exercised by the
// runner's own tests, which drive setup/spawn directly rather than through a
// pre-flight check.
func (a *stubAdapter) CheckEnvironment(ctx context.Context) (domain.EnvironmentReport, error) {
	return domain.EnvironmentReport{}, nil
}

// stubLauncher is a minimal domain.SubjectLauncher double. It performs no
// process control: launchFn, when set, decides what Launch reports, so a
// test can script every terminating condition without a real subject.
type stubLauncher struct {
	rec *callRecorder

	launchFn func(ctx context.Context, plan domain.SpawnPlan) (domain.SubjectResult, error)

	// receivedPlan captures the plan Launch was actually handed, so a test
	// can assert it reached the launcher unmodified from what the adapter
	// returned.
	receivedPlan domain.SpawnPlan
}

var _ domain.SubjectLauncher = (*stubLauncher)(nil)

func (l *stubLauncher) Launch(ctx context.Context, plan domain.SpawnPlan) (domain.SubjectResult, error) {
	l.rec.record("launcher.Launch")
	l.receivedPlan = plan
	if l.launchFn != nil {
		return l.launchFn(ctx, plan)
	}
	return domain.SubjectResult{Disposition: domain.DispositionCompleted}, nil
}

// stubCostProvider is a minimal domain.CostProvider double.
type stubCostProvider struct {
	rec *callRecorder

	report domain.CostReport
	err    error

	lastQuery domain.CostQuery
}

var _ domain.CostProvider = (*stubCostProvider)(nil)

func (c *stubCostProvider) Cost(ctx context.Context, q domain.CostQuery) (domain.CostReport, error) {
	if c.rec != nil {
		c.rec.record("cost.Cost")
	}
	c.lastQuery = q
	return c.report, c.err
}

// recordingSink is a domain.ProgressSink double that captures every emitted
// event, so a test can assert on the progress stream without a real
// frontend.
type recordingSink struct {
	mu     sync.Mutex
	events []domain.ProgressEvent
}

var _ domain.ProgressSink = (*recordingSink)(nil)

func (s *recordingSink) Emit(ev domain.ProgressEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, ev)
}

func (s *recordingSink) all() []domain.ProgressEvent {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]domain.ProgressEvent(nil), s.events...)
}

var errNotUsedByRunnerTests = errNotUsedByRunnerTestsError{}

type errNotUsedByRunnerTestsError struct{}

func (errNotUsedByRunnerTestsError) Error() string {
	return "runner tests: this stub method is not exercised by the runner, which never calls TranslateCall/TranslateOutcome directly"
}
