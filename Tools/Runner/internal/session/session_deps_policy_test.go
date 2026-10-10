package session_test

// Tests for the session dependency policy: which Deps ports are always
// required, which are required only by the run's effective settings (after
// resume reconciliation or adoption), and that a missing required port ends the
// run with a reported refusal naming the capability, before any artifact write
// or dispatch, and never with a panic.

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"mosaic-run/internal/domain"
	"mosaic-run/internal/harness"
	"mosaic-run/internal/session"
)

// ---- helpers ----

// collectMissingPorts returns the Port of every *MissingPortError in err, in
// order, descending into errors.Join results.
func collectMissingPorts(err error) []session.Port {
	if err == nil {
		return nil
	}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		var ports []session.Port
		for _, inner := range joined.Unwrap() {
			ports = append(ports, collectMissingPorts(inner)...)
		}
		return ports
	}
	var mpe *session.MissingPortError
	if errors.As(err, &mpe) {
		return []session.Port{mpe.Port}
	}
	return nil
}

// requireMissingPorts asserts err is a non-nil missing-port error naming
// exactly the wanted ports, in order, and matching the class sentinel.
func requireMissingPorts(t *testing.T, err error, want ...session.Port) {
	t.Helper()
	if len(want) == 0 {
		if err != nil {
			t.Fatalf("want nil error, got %v", err)
		}
		return
	}
	if err == nil {
		t.Fatalf("want a missing-port error for %v, got nil", want)
	}
	if !errors.Is(err, session.ErrMissingPort) {
		t.Errorf("want errors.Is(err, ErrMissingPort), got %v", err)
	}
	if got := collectMissingPorts(err); !reflect.DeepEqual(got, want) {
		t.Errorf("want missing ports %v, got %v (error: %v)", want, got, err)
	}
}

// startGuarded runs Start and converts a panic into a test failure, so a nil
// dereference reports as one failing test instead of aborting the package run.
func startGuarded(t *testing.T, ses session.Session, cfg domain.RunConfig) (out domain.RunOutcome, err error) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("Start panicked instead of reporting a failure: %v", r)
		}
	}()
	return ses.Start(context.Background(), cfg)
}

// requireRefusedNaming asserts a RunRefused outcome whose Cause is a
// missing-port error for exactly the wanted ports and whose message names the
// capability of each, and that the refusal is visible in the debug log.
func requireRefusedNaming(t *testing.T, got domain.RunOutcome, err error, logger *sessionRecordingLogger, want ...session.Port) {
	t.Helper()
	requireRefused(t, got, err)
	requireMissingPorts(t, got.Cause, want...)
	var mpe *session.MissingPortError
	if errors.As(got.Cause, &mpe) {
		if mpe.Capability == "" || !strings.Contains(got.Message, mpe.Capability) {
			t.Errorf("want outcome message to name the capability %q, got %q", mpe.Capability, got.Message)
		}
	}
	if logger != nil && !logger.eventLogged(domain.EventSessionRefusal) {
		t.Errorf("want a %s debug entry, got events %v", domain.EventSessionRefusal, logger.allEvents())
	}
}

// requireNothingWrittenOrDispatched asserts the refusal happened before any
// artifact write (create, apply, adopt) or harness dispatch.
func requireNothingWrittenOrDispatched(t *testing.T, f *harness.MockAdapter, store *memStore) {
	t.Helper()
	if store != nil {
		if len(store.Applied) != 0 {
			t.Errorf("want no Apply call before the refusal, got %d", len(store.Applied))
		}
		if len(store.AdoptCalls) != 0 {
			t.Errorf("want no AdoptRunnerSettings call before the refusal, got %d", len(store.AdoptCalls))
		}
	}
	if f != nil {
		if n := len(f.Invocations()); n != 0 {
			t.Errorf("want no harness dispatch before the refusal, got %d", n)
		}
	}
}

// policyFixture is a linear-workflow session setup whose Deps the test may
// alter before building the session.
type policyFixture struct {
	deps     session.Deps
	f        *harness.MockAdapter
	store    *memStore
	logger   *sessionRecordingLogger
	orchPath string
}

// newPolicyFixture returns fully wired required ports (Harness, Store, Clock,
// Interact) plus a recording debug logger, and no optional capability ports.
func newPolicyFixture(t *testing.T) *policyFixture {
	t.Helper()
	dir := t.TempDir()
	orchPath := copyOrchestratorFile(t, dir, "linear-orch.md")
	writeAgentFile(t, dir, "agent-a")
	writeAgentFile(t, dir, "agent-b")
	fx := &policyFixture{
		f:        harness.NewMockAdapter(),
		store:    &memStore{},
		logger:   &sessionRecordingLogger{},
		orchPath: orchPath,
	}
	fx.deps = session.Deps{
		Harness:  fx.f,
		Store:    fx.store,
		Clock:    fixedClock{t: epoch},
		Interact: &noopInteraction{},
		Debug:    fx.logger,
	}
	queueLinearSuccess(fx.f)
	return fx
}

func (fx *policyFixture) start(t *testing.T, cfg domain.RunConfig) (domain.RunOutcome, error) {
	t.Helper()
	return startGuarded(t, session.New(fx.deps), cfg)
}

// ---- Deps.CheckRequired ----

func TestDeps_CheckRequired_AllWired_ReturnsNil(t *testing.T) {
	d := session.Deps{
		Harness: harness.NewMockAdapter(), Store: &memStore{}, Clock: fixedClock{t: epoch}, Interact: &noopInteraction{},
	}

	requireMissingPorts(t, d.CheckRequired())
}

func TestDeps_CheckRequired_ReportsEachMissingRequiredPort(t *testing.T) {
	full := func() session.Deps {
		return session.Deps{
			Harness: harness.NewMockAdapter(), Store: &memStore{}, Clock: fixedClock{t: epoch}, Interact: &noopInteraction{},
		}
	}
	cases := []struct {
		name string
		mut  func(*session.Deps)
		want []session.Port
	}{
		{"harness", func(d *session.Deps) { d.Harness = nil }, []session.Port{session.PortHarness}},
		{"store", func(d *session.Deps) { d.Store = nil }, []session.Port{session.PortStore}},
		{"clock", func(d *session.Deps) { d.Clock = nil }, []session.Port{session.PortClock}},
		{"interact", func(d *session.Deps) { d.Interact = nil }, []session.Port{session.PortInteract}},
		{"all four, in declaration order", func(d *session.Deps) { *d = session.Deps{} },
			[]session.Port{session.PortHarness, session.PortStore, session.PortClock, session.PortInteract}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := full()
			tc.mut(&d)

			requireMissingPorts(t, d.CheckRequired(), tc.want...)
		})
	}
}

func TestMissingPortError_MatchesSentinelAndNamesCapability(t *testing.T) {
	err := &session.MissingPortError{Port: session.PortManual, Capability: "manual resolution", Reason: "enabled"}

	if !errors.Is(err, session.ErrMissingPort) {
		t.Error("want errors.Is(MissingPortError, ErrMissingPort) true")
	}
	if !strings.Contains(err.Error(), "manual resolution") {
		t.Errorf("want the message to name the capability, got %q", err.Error())
	}
}

// ---- Deps.CheckForSettings ----

func TestDeps_CheckForSettings_RequiresPortsOnlyForEnabledSettings(t *testing.T) {
	pre := &scriptedPreConsultant{}
	manual := &scriptedRoutingConsultant{}
	auto := func(pre, man bool) domain.RunSettings {
		return domain.RunSettings{Mode: domain.ExecutionModeAuto, PreConsultation: pre, ManualResolution: man}
	}
	cases := []struct {
		name     string
		deps     session.Deps
		settings domain.RunSettings
		want     []session.Port
	}{
		{"auto with pre-consultation, PreConsult unwired", session.Deps{}, auto(true, false), []session.Port{session.PortPreConsult}},
		{"auto-review with pre-consultation, PreConsult unwired", session.Deps{},
			domain.RunSettings{Mode: domain.ExecutionModeAutoReview, PreConsultation: true}, []session.Port{session.PortPreConsult}},
		{"orchestrated with pre-consultation never needs PreConsult", session.Deps{},
			domain.RunSettings{Mode: domain.ExecutionModeOrchestrated, PreConsultation: true}, nil},
		{"auto without pre-consultation", session.Deps{}, auto(false, false), nil},
		{"manual resolution in auto, Manual unwired", session.Deps{}, auto(false, true), []session.Port{session.PortManual}},
		{"manual resolution in orchestrated, Manual unwired", session.Deps{},
			domain.RunSettings{Mode: domain.ExecutionModeOrchestrated, ManualResolution: true}, []session.Port{session.PortManual}},
		{"both enabled, neither wired", session.Deps{}, auto(true, true),
			[]session.Port{session.PortPreConsult, session.PortManual}},
		{"both enabled, both wired", session.Deps{PreConsult: pre, Manual: manual}, auto(true, true), nil},
		{"pre-consultation enabled, wired; manual unrequested", session.Deps{PreConsult: pre}, auto(true, false), nil},
		{"zero settings, nothing wired", session.Deps{}, domain.RunSettings{}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			requireMissingPorts(t, tc.deps.CheckForSettings(tc.settings), tc.want...)
		})
	}
}

// ---- Start: always-required ports ----

func TestSession_Start_MissingRequiredPort_RefusesNamingCapability(t *testing.T) {
	cases := []struct {
		name string
		mut  func(*session.Deps)
		port session.Port
	}{
		{"harness", func(d *session.Deps) { d.Harness = nil }, session.PortHarness},
		{"store", func(d *session.Deps) { d.Store = nil }, session.PortStore},
		{"clock", func(d *session.Deps) { d.Clock = nil }, session.PortClock},
		{"interact", func(d *session.Deps) { d.Interact = nil }, session.PortInteract},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fx := newPolicyFixture(t)
			tc.mut(&fx.deps)

			got, err := fx.start(t, baseLinearConfig(fx.orchPath))

			requireRefusedNaming(t, got, err, fx.logger, tc.port)
			requireNothingWrittenOrDispatched(t, fx.f, fx.store)
		})
	}
}

func TestSession_Start_MissingRequiredPort_ResumedRun_RefusesWithoutTouchingArtifact(t *testing.T) {
	fx := newPolicyFixture(t)
	fx.deps.Interact = nil
	fx.store.state = resumedAutoState()
	fx.store.exists = true
	cfg := baseLinearConfig(fx.orchPath)
	markResume(&cfg)

	got, err := fx.start(t, cfg)

	requireRefusedNaming(t, got, err, fx.logger, session.PortInteract)
	requireNothingWrittenOrDispatched(t, fx.f, fx.store)
	if !reflect.DeepEqual(fx.store.state, resumedAutoState()) {
		t.Errorf("want the stored artifact unchanged by the refusal, got %+v", fx.store.state)
	}
}

// ---- Start: ports required by the effective settings ----

func TestSession_Start_NewRun_PreConsultationEnabledButUnwired_RefusesBeforeArtifactCreate(t *testing.T) {
	for _, mode := range []domain.ExecutionMode{domain.ExecutionModeAuto, domain.ExecutionModeAutoReview} {
		t.Run(string(mode), func(t *testing.T) {
			fx := newPolicyFixture(t)
			cfg := baseLinearConfig(fx.orchPath)
			cfg.Mode = mode
			cfg.PreConsultation = true

			got, err := fx.start(t, cfg)

			requireRefusedNaming(t, got, err, fx.logger, session.PortPreConsult)
			requireNothingWrittenOrDispatched(t, fx.f, fx.store)
			if fx.store.exists {
				t.Error("want the artifact not created: the check must run before the artifact is created")
			}
		})
	}
}

func TestSession_Start_NewRun_ManualResolutionEnabledButUnwired_RefusesBeforeArtifactCreate(t *testing.T) {
	for _, mode := range []domain.ExecutionMode{domain.ExecutionModeAuto, domain.ExecutionModeOrchestrated} {
		t.Run(string(mode), func(t *testing.T) {
			fx := newPolicyFixture(t)
			cfg := baseLinearConfig(fx.orchPath)
			cfg.Mode = mode
			cfg.ManualResolution = true

			got, err := fx.start(t, cfg)

			requireRefusedNaming(t, got, err, fx.logger, session.PortManual)
			requireNothingWrittenOrDispatched(t, fx.f, fx.store)
			if fx.store.exists {
				t.Error("want the artifact not created: the check must run before the artifact is created")
			}
		})
	}
}

func TestSession_Start_NewRun_BothSettingsEnabledNeitherWired_NamesBothCapabilities(t *testing.T) {
	fx := newPolicyFixture(t)
	cfg := baseLinearConfig(fx.orchPath)
	cfg.PreConsultation = true
	cfg.ManualResolution = true

	got, err := fx.start(t, cfg)

	requireRefusedNaming(t, got, err, fx.logger, session.PortPreConsult, session.PortManual)
}

func TestSession_Start_NewRun_OrchestratedPreConsultationNeedsNoPreConsultPort(t *testing.T) {
	consultant := &scriptedRoutingConsultant{}
	consultant.queueStop("nothing to do")
	ses, _, _, orchPath := newOrchestratedSession(t, consultant)
	cfg := baseOrchestratedConfig(orchPath)
	cfg.PreConsultation = true

	got, err := startGuarded(t, ses, cfg)

	if errors.Is(got.Cause, session.ErrMissingPort) || errors.Is(err, session.ErrMissingPort) {
		t.Errorf("want no missing-port refusal: pre-consultation is not used in orchestrated mode, got cause %v", got.Cause)
	}
}

// ---- Start: the effective setting comes from the recorded artifact ----

// TestSession_Start_Resume_RecordedPreConsultationButUnwired_RefusesNamingIt covers
// the frontend that supplies no settings: RunConfig.RunSettings is empty, the
// artifact records auto + pre-consultation, and PreConsult is not wired.
func TestSession_Start_Resume_RecordedPreConsultationButUnwired_RefusesNamingIt(t *testing.T) {
	fx := newPolicyFixture(t)
	fx.store.state = resumedAutoState()
	fx.store.exists = true
	cfg := baseLinearConfig(fx.orchPath)
	markResume(&cfg)
	cfg.RunSettings = domain.RunSettings{}

	got, err := fx.start(t, cfg)

	requireRefusedNaming(t, got, err, fx.logger, session.PortPreConsult)
	requireNothingWrittenOrDispatched(t, fx.f, fx.store)
	if !reflect.DeepEqual(fx.store.state, resumedAutoState()) {
		t.Errorf("want the stored artifact unchanged by the refusal, got %+v", fx.store.state)
	}
}

func TestSession_Start_Resume_RecordedManualResolutionButUnwired_RefusesNamingIt(t *testing.T) {
	fx := newPolicyFixture(t)
	state := resumedAutoState()
	state.RunSettings.PreConsultation = false
	state.RunSettings.ManualResolution = true
	fx.store.state = state
	fx.store.exists = true
	cfg := baseLinearConfig(fx.orchPath)
	markResume(&cfg)
	cfg.RunSettings = domain.RunSettings{}

	got, err := fx.start(t, cfg)

	requireRefusedNaming(t, got, err, fx.logger, session.PortManual)
	requireNothingWrittenOrDispatched(t, fx.f, fx.store)
}

func TestSession_Start_Resume_RecordedPreConsultationWired_RunsItWithNothingSupplied(t *testing.T) {
	fx := newPolicyFixture(t)
	pc := &scriptedPreConsultant{}
	fx.deps.PreConsult = pc
	fx.store.state = resumedAutoState()
	fx.store.exists = true
	cfg := baseLinearConfig(fx.orchPath)
	markResume(&cfg)
	cfg.RunSettings = domain.RunSettings{}

	got, err := fx.start(t, cfg)

	requireRunStatus(t, got, err, domain.RunCompleted)
	if !pc.Called {
		t.Error("want PreConsult called: the recorded setting enables it although the frontend supplied none")
	}
}

// TestSession_Start_Adoption_SuppliedPreConsultationButUnwired_RefusesBeforeAdoptWrite
// covers an artifact that records no runner settings: the supplied values are
// adopted, and the check must see them before the adoption write.
func TestSession_Start_Adoption_SuppliedPreConsultationButUnwired_RefusesBeforeAdoptWrite(t *testing.T) {
	fx := newPolicyFixture(t)
	state := resumedAutoState()
	state.RunSettings = domain.RunSettings{}
	fx.store.state = state
	fx.store.exists = true
	cfg := baseLinearConfig(fx.orchPath)
	markResume(&cfg)
	cfg.RunSettings = domain.RunSettings{Mode: domain.ExecutionModeAuto, PreConsultation: true}
	cfg.Supplied = domain.SuppliedSettings{Mode: true, PreConsultation: true, ManualResolution: true}

	got, err := fx.start(t, cfg)

	requireRefusedNaming(t, got, err, fx.logger, session.PortPreConsult)
	requireNothingWrittenOrDispatched(t, fx.f, fx.store)
}
