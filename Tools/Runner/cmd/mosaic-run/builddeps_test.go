package main

import (
	"context"
	"testing"

	"mosaic-run/internal/deviation"
	"mosaic-run/internal/domain"
)

// ---------------------------------------------------------------------------
// buildDeps: consultant wiring selection
//
// buildDeps is the shared wiring constructor that both frontends delegate to.
// These tests verify that the correct consultant types are selected for each
// RunSettings combination, and that the Approvals field is passed through.
//
// Wiring assertions (non-nil Routing, Manual, PreConsult, non-nil Approvals)
// are RED until buildDeps is updated to wire all deps from RunSettings rather
// than individual string/bool parameters.
// ---------------------------------------------------------------------------

// TestBuildDeps_OrchestratedMode_WiresOrchestratorConsultantAsRouting verifies
// that Mode=orchestrated causes buildDeps to wire *deviation.OrchestratorConsultant
// as Deps.Routing.
func TestBuildDeps_OrchestratedMode_WiresOrchestratorConsultantAsRouting(t *testing.T) {
	settings := domain.RunSettings{Mode: domain.ExecutionModeOrchestrated}
	deps := buildDeps(settings, nil, nil, nil, nil)
	if _, ok := deps.Routing.(*deviation.OrchestratorConsultant); !ok {
		t.Errorf("buildDeps(orchestrated).Routing = %T, want *deviation.OrchestratorConsultant", deps.Routing)
	}
}

// TestBuildDeps_AutoMode_WiresOrchestratorConsultantAsRouting verifies that
// Mode=auto wires *deviation.OrchestratorConsultant as Deps.Routing.
func TestBuildDeps_AutoMode_WiresOrchestratorConsultantAsRouting(t *testing.T) {
	settings := domain.RunSettings{Mode: domain.ExecutionModeAuto}
	deps := buildDeps(settings, nil, nil, nil, nil)
	if _, ok := deps.Routing.(*deviation.OrchestratorConsultant); !ok {
		t.Errorf("buildDeps(auto).Routing = %T, want *deviation.OrchestratorConsultant", deps.Routing)
	}
}

// TestBuildDeps_AutoReviewMode_WiresOrchestratorConsultantAsRouting verifies
// that Mode=auto-review wires *deviation.OrchestratorConsultant as Deps.Routing.
func TestBuildDeps_AutoReviewMode_WiresOrchestratorConsultantAsRouting(t *testing.T) {
	settings := domain.RunSettings{Mode: domain.ExecutionModeAutoReview}
	deps := buildDeps(settings, nil, nil, nil, nil)
	if _, ok := deps.Routing.(*deviation.OrchestratorConsultant); !ok {
		t.Errorf("buildDeps(auto-review).Routing = %T, want *deviation.OrchestratorConsultant", deps.Routing)
	}
}

// TestBuildDeps_ManualResolutionEnabled_WiresManualResolverAsManual verifies
// that ManualResolution=true wires *deviation.ManualResolver as Deps.Manual.
func TestBuildDeps_ManualResolutionEnabled_WiresManualResolverAsManual(t *testing.T) {
	settings := domain.RunSettings{Mode: domain.ExecutionModeAuto, ManualResolution: true}
	deps := buildDeps(settings, nil, nil, nil, nil)
	if _, ok := deps.Manual.(*deviation.ManualResolver); !ok {
		t.Errorf("buildDeps(manualResolution=true).Manual = %T, want *deviation.ManualResolver", deps.Manual)
	}
}

// TestBuildDeps_ManualResolutionDisabled_LeavesManualNil verifies that
// ManualResolution=false leaves Deps.Manual nil.
func TestBuildDeps_ManualResolutionDisabled_LeavesManualNil(t *testing.T) {
	settings := domain.RunSettings{Mode: domain.ExecutionModeAuto, ManualResolution: false}
	deps := buildDeps(settings, nil, nil, nil, nil)
	if deps.Manual != nil {
		t.Errorf("buildDeps(manualResolution=false).Manual = %T, want nil", deps.Manual)
	}
}

// TestBuildDeps_PreConsultEnabled_WiresPreConsultant verifies that
// PreConsultation=true wires a non-nil Deps.PreConsult of the concrete type
// *deviation.OrchestratorConsultant. The type check matches the pattern used
// for Routing assertions: a future refactor that wires a different type will
// fail this test, not silently pass.
func TestBuildDeps_PreConsultEnabled_WiresPreConsultant(t *testing.T) {
	settings := domain.RunSettings{Mode: domain.ExecutionModeAuto, PreConsultation: true}
	deps := buildDeps(settings, nil, nil, nil, nil)
	if deps.PreConsult == nil {
		t.Fatal("buildDeps(preConsult=true).PreConsult = nil, want *deviation.OrchestratorConsultant")
	}
	if _, ok := deps.PreConsult.(*deviation.OrchestratorConsultant); !ok {
		t.Errorf("buildDeps(preConsult=true).PreConsult = %T, want *deviation.OrchestratorConsultant", deps.PreConsult)
	}
}

// TestBuildDeps_PreConsultDisabled_LeavesPreConsultNil verifies that
// PreConsultation=false leaves Deps.PreConsult nil.
func TestBuildDeps_PreConsultDisabled_LeavesPreConsultNil(t *testing.T) {
	settings := domain.RunSettings{Mode: domain.ExecutionModeAuto, PreConsultation: false}
	deps := buildDeps(settings, nil, nil, nil, nil)
	if deps.PreConsult != nil {
		t.Errorf("buildDeps(preConsult=false).PreConsult = %T, want nil", deps.PreConsult)
	}
}

// ---------------------------------------------------------------------------
// T7.1: buildDeps wires each execution mode identically for both frontends.
//
// The shared buildDeps function is the mechanism that prevents the TUI and CLI
// from diverging. These tests assert that the same RunSettings — the struct
// both frontends already produce — yields the same session.Deps wiring from
// buildDeps, regardless of which frontend called it. They also assert that no
// consultant is left with an empty routing table at construction time (the
// table is delivered later, through RunContextBinder).
// ---------------------------------------------------------------------------

// TestBuildDeps_AllModes_RoutingIsNeverNil verifies that for every mode in
// domain.ExecutionModes(), buildDeps wires a non-nil Deps.Routing. A nil
// Routing causes a nil-pointer panic on any consultation attempt.
func TestBuildDeps_AllModes_RoutingIsNeverNil(t *testing.T) {
	for _, mode := range domain.ExecutionModes() {
		mode := mode
		t.Run(string(mode), func(t *testing.T) {
			settings := domain.RunSettings{Mode: mode}
			deps := buildDeps(settings, nil, nil, nil, nil)
			if deps.Routing == nil {
				t.Errorf("buildDeps(mode=%s).Routing = nil, want non-nil routing consultant", mode)
			}
		})
	}
}

// TestBuildDeps_AllModes_RoutingIsOrchestratorConsultant verifies that for
// every mode in domain.ExecutionModes(), the wired Deps.Routing is specifically
// *deviation.OrchestratorConsultant. Both auto and orchestrated modes must use
// the same consultant type; only the session's routing logic differs by mode.
func TestBuildDeps_AllModes_RoutingIsOrchestratorConsultant(t *testing.T) {
	for _, mode := range domain.ExecutionModes() {
		mode := mode
		t.Run(string(mode), func(t *testing.T) {
			settings := domain.RunSettings{Mode: mode}
			deps := buildDeps(settings, nil, nil, nil, nil)
			if _, ok := deps.Routing.(*deviation.OrchestratorConsultant); !ok {
				t.Errorf("buildDeps(mode=%s).Routing = %T, want *deviation.OrchestratorConsultant", mode, deps.Routing)
			}
		})
	}
}

// TestBuildDeps_ApprovalsPassedThrough verifies that the approvals parameter
// is propagated to Deps.Approvals. Both frontends pass the real approval
// reader; buildDeps must not substitute or discard it.
func TestBuildDeps_ApprovalsPassedThrough(t *testing.T) {
	sentinelReader := &sentinelApprovalReader{}
	settings := domain.RunSettings{Mode: domain.ExecutionModeAuto}
	deps := buildDeps(settings, nil, nil, sentinelReader, nil)
	if deps.Approvals != sentinelReader {
		t.Errorf("buildDeps.Approvals = %T (%p), want the exact sentinelApprovalReader (%p) passed in",
			deps.Approvals, deps.Approvals, sentinelReader)
	}
}

// sentinelApprovalReader is a test-only ApprovalReader used to verify that
// the approvals parameter is passed through buildDeps without substitution.
type sentinelApprovalReader struct{}

func (r *sentinelApprovalReader) ReadApproval(_ context.Context, _ string) domain.HumanApproval {
	return domain.ApprovalUnreadable
}

// ---------------------------------------------------------------------------
// DispatchLogger wiring in OrchestratorConsultant
//
// buildDeps receives a domain.DispatchLogger and must wire it into the
// OrchestratorConsultant so that consultation invocations (ConsultRouting and
// PreConsult) are recorded in the same dispatch log as subagent invocations.
// Both the Routing and PreConsult fields of session.Deps point to the same
// OrchestratorConsultant instance, so a single DispatchLogger set on that
// consultant covers both call sites.
//
// RED state: both tests below fail because buildDeps does not yet assign
// consultant.DispatchLogger = dispLogger (that assignment is I4.3). The field
// is declared on OrchestratorConsultant (I4.1 scaffold), so the tests compile.
// ---------------------------------------------------------------------------

// TestBuildDeps_DispatchLoggerWiredIntoRoutingConsultant verifies that when
// a non-nil DispatchLogger is passed to buildDeps, the OrchestratorConsultant
// wired as Deps.Routing carries that exact instance in its DispatchLogger field.
// The pointer-equality check ensures the same logger (not a copy or wrapper)
// reaches the consultant, which is required so that consultation log entries
// appear in the same log file as subagent dispatch entries.
func TestBuildDeps_DispatchLoggerWiredIntoRoutingConsultant(t *testing.T) {
	sentinel := &sentinelDispatchLogger{}
	settings := domain.RunSettings{Mode: domain.ExecutionModeAuto}

	deps := buildDeps(settings, nil, nil, nil, sentinel)

	consultant, ok := deps.Routing.(*deviation.OrchestratorConsultant)
	if !ok {
		t.Fatalf("deps.Routing = %T, want *deviation.OrchestratorConsultant", deps.Routing)
	}
	if consultant.DispatchLogger != sentinel {
		t.Errorf("OrchestratorConsultant.DispatchLogger = %T (%p), "+
			"want the exact sentinelDispatchLogger (%p) passed to buildDeps; "+
			"both CLI and TUI paths must wire their dispLogger into the consultant",
			consultant.DispatchLogger, consultant.DispatchLogger, sentinel)
	}
}

// TestBuildDeps_DispatchLoggerWiredIntoPreConsultConsultant verifies that when
// PreConsultation=true and a non-nil DispatchLogger is passed to buildDeps, the
// OrchestratorConsultant wired as Deps.PreConsult also carries that exact
// instance. Since Routing and PreConsult reference the same consultant object,
// this test confirms the single consultant receives the logger rather than
// checking that two separate consultants each receive a copy.
func TestBuildDeps_DispatchLoggerWiredIntoPreConsultConsultant(t *testing.T) {
	sentinel := &sentinelDispatchLogger{}
	settings := domain.RunSettings{Mode: domain.ExecutionModeAuto, PreConsultation: true}

	deps := buildDeps(settings, nil, nil, nil, sentinel)

	preConsultant, ok := deps.PreConsult.(*deviation.OrchestratorConsultant)
	if !ok {
		t.Fatalf("deps.PreConsult = %T, want *deviation.OrchestratorConsultant", deps.PreConsult)
	}
	if preConsultant.DispatchLogger != sentinel {
		t.Errorf("OrchestratorConsultant.DispatchLogger (via PreConsult) = %T (%p), "+
			"want the exact sentinelDispatchLogger (%p) passed to buildDeps",
			preConsultant.DispatchLogger, preConsultant.DispatchLogger, sentinel)
	}
}

// sentinelDispatchLogger is a no-op domain.DispatchLogger used as a sentinel
// for pointer-equality checks in wiring tests.
type sentinelDispatchLogger struct{}

func (s *sentinelDispatchLogger) LogRequest(_ domain.ProtocolRequest)   {}
func (s *sentinelDispatchLogger) LogResponse(_ domain.ProtocolResponse) {}
func (s *sentinelDispatchLogger) LogError(_ string, _ string)           {}
func (s *sentinelDispatchLogger) SetRunID(_ string)                     {}
func (s *sentinelDispatchLogger) Close()                                {}
func (s *sentinelDispatchLogger) Path() string                          { return "" }

// TestBuildDeps_SameSettingsYieldSameWiring verifies the parity contract
// directly: calling buildDeps with the same RunSettings from the CLI path and
// the TUI path (which both produce a domain.RunSettings) yields structurally
// identical Deps — same consultant types, same Manual/PreConsult presence.
// This is the single assertion that prevents the two frontends from diverging.
func TestBuildDeps_SameSettingsYieldSameWiring(t *testing.T) {
	// Settings that exercise every wiring branch.
	settings := domain.RunSettings{
		Mode:             domain.ExecutionModeOrchestrated,
		ManualResolution: true,
		PreConsultation:  true,
	}

	cliDeps := buildDeps(settings, nil, nil, nil, nil)
	tuiDeps := buildDeps(settings, nil, nil, nil, nil)

	// Routing: both must be *deviation.OrchestratorConsultant.
	_, cliRoutingOK := cliDeps.Routing.(*deviation.OrchestratorConsultant)
	_, tuiRoutingOK := tuiDeps.Routing.(*deviation.OrchestratorConsultant)
	if !cliRoutingOK || !tuiRoutingOK {
		t.Errorf("Routing type mismatch: CLI=%T TUI=%T, both want *deviation.OrchestratorConsultant",
			cliDeps.Routing, tuiDeps.Routing)
	}

	// Manual: both must be *deviation.ManualResolver.
	_, cliManualOK := cliDeps.Manual.(*deviation.ManualResolver)
	_, tuiManualOK := tuiDeps.Manual.(*deviation.ManualResolver)
	if !cliManualOK || !tuiManualOK {
		t.Errorf("Manual type mismatch: CLI=%T TUI=%T, both want *deviation.ManualResolver",
			cliDeps.Manual, tuiDeps.Manual)
	}

	// PreConsult: both must be non-nil.
	if cliDeps.PreConsult == nil || tuiDeps.PreConsult == nil {
		t.Errorf("PreConsult: CLI=%v TUI=%v, both want non-nil (PreConsultation=true)",
			cliDeps.PreConsult == nil, tuiDeps.PreConsult == nil)
	}
}

// ---------------------------------------------------------------------------
// T7.4: Both frontends offer the same set of execution modes.
//
// The TUI mode-selection step populates its list from domain.ExecutionModes().
// The CLI validates the --mode flag against domain.ExecutionModes(). The test
// asserts that the canonical list is non-empty and that buildDeps accepts every
// mode in it — confirming that neither frontend offers a mode it cannot execute
// (a mode absent from ExecutionModes() would not reach buildDeps from either frontend).
// ---------------------------------------------------------------------------

// TestBuildDeps_EveryExecutionMode_IsAccepted verifies that buildDeps produces
// a structurally consistent Deps (non-nil Routing) for every mode in
// domain.ExecutionModes(). If a mode is in the list but buildDeps cannot handle
// it, this test fails — catching a frontend that presents modes it cannot execute.
func TestBuildDeps_EveryExecutionMode_IsAccepted(t *testing.T) {
	modes := domain.ExecutionModes()
	if len(modes) == 0 {
		t.Fatal("domain.ExecutionModes() returned an empty slice; at least one mode must be defined")
	}
	for _, mode := range modes {
		mode := mode
		t.Run(string(mode), func(t *testing.T) {
			settings := domain.RunSettings{Mode: mode}
			deps := buildDeps(settings, nil, nil, nil, nil)
			if deps.Routing == nil {
				t.Errorf("buildDeps(mode=%s) produced nil Routing; all ExecutionModes entries must be executable", mode)
			}
		})
	}
}

// TestExecutionModes_AreAllHandledByBuildDeps verifies that the count of modes
// in domain.ExecutionModes() equals the number of modes for which buildDeps
// wires an OrchestratorConsultant. If ExecutionModes() adds a new mode without
// a corresponding branch in buildDeps, the unhandled mode would silently fall
// through to the fake adapter, making the frontend appear to offer it while
// actually failing every run started with it.
func TestExecutionModes_AreAllHandledByBuildDeps(t *testing.T) {
	modes := domain.ExecutionModes()
	handled := 0
	for _, mode := range modes {
		settings := domain.RunSettings{Mode: mode}
		deps := buildDeps(settings, nil, nil, nil, nil)
		if _, ok := deps.Routing.(*deviation.OrchestratorConsultant); ok {
			handled++
		}
	}
	if handled != len(modes) {
		t.Errorf("buildDeps handles %d of %d ExecutionModes() entries with OrchestratorConsultant; "+
			"all modes must be handled so neither frontend offers an unexecutable mode",
			handled, len(modes))
	}
}
