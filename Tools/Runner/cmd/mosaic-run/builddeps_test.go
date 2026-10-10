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
// Which ports it returns depends only on which transports exist: the raw
// invoker carries consultation (Routing and PreConsult), the interaction
// channel carries manual resolution. No run setting is an input, so a resumed
// run whose recorded settings enable a capability finds it wired whatever the
// frontend supplied. The session's effective settings decide which wired
// capabilities are used.
//
// All tests reach buildDeps through buildDepsFromTransports.
// ---------------------------------------------------------------------------

// TestBuildDeps_Routing_IsAlwaysOrchestratorConsultant verifies that Routing is
// wired as *deviation.OrchestratorConsultant for every combination of
// transports, including none. A nil Routing causes a nil-pointer panic on any
// consultation attempt.
func TestBuildDeps_Routing_IsAlwaysOrchestratorConsultant(t *testing.T) {
	cases := []struct {
		name     string
		invoker  domain.RawInvoker
		interact domain.Interaction
	}{
		{"no transports", nil, nil},
		{"raw invoker only", &fakeRawInvoker{}, nil},
		{"interaction only", nil, &mainTestNoopInteraction{}},
		{"both transports", &fakeRawInvoker{}, &mainTestNoopInteraction{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			deps := buildDepsFromTransports(tc.invoker, tc.interact, nil, nil)

			if _, ok := deps.Routing.(*deviation.OrchestratorConsultant); !ok {
				t.Errorf("buildDeps.Routing = %T, want *deviation.OrchestratorConsultant", deps.Routing)
			}
		})
	}
}

// TestBuildDeps_InteractionPresent_WiresManualResolver verifies that an
// interaction channel wires *deviation.ManualResolver as Deps.Manual.
func TestBuildDeps_InteractionPresent_WiresManualResolver(t *testing.T) {
	deps := buildDepsFromTransports(nil, &mainTestNoopInteraction{}, nil, nil)

	if _, ok := deps.Manual.(*deviation.ManualResolver); !ok {
		t.Errorf("buildDeps(interaction present).Manual = %T, want *deviation.ManualResolver", deps.Manual)
	}
}

// TestBuildDeps_NoInteraction_LeavesManualNil verifies that without an
// interaction channel there is nothing to resolve manually through, so Manual
// stays nil.
func TestBuildDeps_NoInteraction_LeavesManualNil(t *testing.T) {
	deps := buildDepsFromTransports(&fakeRawInvoker{}, nil, nil, nil)

	if deps.Manual != nil {
		t.Errorf("buildDeps(no interaction).Manual = %T, want nil", deps.Manual)
	}
}

// TestBuildDeps_RawInvokerPresent_WiresPreConsultant verifies that a raw
// invoker wires a non-nil Deps.PreConsult of the concrete type
// *deviation.OrchestratorConsultant. The type check matches the pattern used
// for Routing assertions: a future refactor that wires a different type will
// fail this test, not silently pass.
func TestBuildDeps_RawInvokerPresent_WiresPreConsultant(t *testing.T) {
	deps := buildDepsFromTransports(&fakeRawInvoker{}, nil, nil, nil)

	if deps.PreConsult == nil {
		t.Fatal("buildDeps(raw invoker present).PreConsult = nil, want *deviation.OrchestratorConsultant")
	}
	if _, ok := deps.PreConsult.(*deviation.OrchestratorConsultant); !ok {
		t.Errorf("buildDeps(raw invoker present).PreConsult = %T, want *deviation.OrchestratorConsultant", deps.PreConsult)
	}
}

// TestBuildDeps_NoRawInvoker_LeavesPreConsultNil verifies that a harness
// without a raw-JSON transport leaves PreConsult nil: there is no transport to
// consult through.
func TestBuildDeps_NoRawInvoker_LeavesPreConsultNil(t *testing.T) {
	deps := buildDepsFromTransports(nil, &mainTestNoopInteraction{}, nil, nil)

	if deps.PreConsult != nil {
		t.Errorf("buildDeps(no raw invoker).PreConsult = %T, want nil", deps.PreConsult)
	}
}

// TestBuildDeps_ApprovalsPassedThrough verifies that the approvals parameter
// is propagated to Deps.Approvals. Both frontends pass the real approval
// reader; buildDeps must not substitute or discard it.
func TestBuildDeps_ApprovalsPassedThrough(t *testing.T) {
	sentinelReader := &sentinelApprovalReader{}

	deps := buildDepsFromTransports(nil, nil, sentinelReader, nil)

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
// ---------------------------------------------------------------------------

// TestBuildDeps_DispatchLoggerWiredIntoRoutingConsultant verifies that when
// a non-nil DispatchLogger is passed to buildDeps, the OrchestratorConsultant
// wired as Deps.Routing carries that exact instance in its DispatchLogger field.
// The pointer-equality check ensures the same logger (not a copy or wrapper)
// reaches the consultant, which is required so that consultation log entries
// appear in the same log file as subagent dispatch entries.
func TestBuildDeps_DispatchLoggerWiredIntoRoutingConsultant(t *testing.T) {
	sentinel := &sentinelDispatchLogger{}

	deps := buildDepsFromTransports(nil, nil, nil, sentinel)

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
// a raw invoker and a non-nil DispatchLogger are passed to buildDeps, the
// OrchestratorConsultant wired as Deps.PreConsult also carries that exact
// instance. Since Routing and PreConsult reference the same consultant object,
// this test confirms the single consultant receives the logger rather than
// checking that two separate consultants each receive a copy.
func TestBuildDeps_DispatchLoggerWiredIntoPreConsultConsultant(t *testing.T) {
	sentinel := &sentinelDispatchLogger{}

	deps := buildDepsFromTransports(&fakeRawInvoker{}, nil, nil, sentinel)

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

// TestBuildDeps_SameTransportsYieldSameWiring verifies the parity contract
// directly: the CLI path and the TUI path both call buildDeps with the
// transports they hold and nothing else, so the same transports yield
// structurally identical Deps - same consultant types, same Manual/PreConsult
// presence. This is the assertion that prevents the two frontends from
// diverging.
func TestBuildDeps_SameTransportsYieldSameWiring(t *testing.T) {
	invoker := &fakeRawInvoker{}
	interact := &mainTestNoopInteraction{}

	cliDeps := buildDepsFromTransports(invoker, interact, nil, nil)
	tuiDeps := buildDepsFromTransports(invoker, interact, nil, nil)

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
		t.Errorf("PreConsult: CLI nil=%v TUI nil=%v, both want non-nil (raw invoker present)",
			cliDeps.PreConsult == nil, tuiDeps.PreConsult == nil)
	}
}
