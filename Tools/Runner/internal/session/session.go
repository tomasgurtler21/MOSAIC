// Package session implements the use-case loop that connects the pure engine
// to the outside world. It drives the complete run lifecycle:
//
//  1. Run-start sequence (in fixed order):
//     a. Load orchestrator file and enumerate workflow regions (orchfile)
//     b. Parse the selected workflow routing table (workflow)
//     c. Read existing artifact or detect new run (artifact store)
//     d. Admit the workflow (compat)
//     e. Resolve every agent identifier (agentresolve)
//     f. Read stage set if a staged phase is present (planstages)
//     g. Settle checkpoint value (FR-9 refusal if no provider)
//     h. Create or resume the artifact
//     i. Commit setup (commits enabled and no commit_branch recorded): an
//        ordinary out-of-band invocation against the existing artifact. It
//        takes the next sequence and always gets an Execution Log row without
//        moving current_state; only SUCCESS with a [branch:{name}] marker
//        records commit_branch and lets the run continue
//     j. Pre-consultation (auto and auto-review modes, when enabled); it
//        never touches the artifact
//
//     A failed commit setup or pre-consultation ends the start with
//     RunStartFailed. The run folder, the artifact and any setup row are
//     kept, no workflow step is dispatched, and a resume retries the step.
//
//  2. Dispatch loop: ask engine -> dispatch via harness -> apply to artifact -> repeat.
//
//  3. Special cases in the dispatch loop:
//     - Mode-driven routing: in ExecutionModeOrchestrated, every routing choice
//       is delegated to the RoutingConsultant (engine returns Consult). In auto
//       and auto-review modes, the engine decides; the consultant is only invoked
//       on Deviation decisions or HITL escalations.
//     - On Findings auto-routing: engine returns Dispatch for COMPLETED_NEEDS_ACTION
//       with an unambiguous OnFindings hint; session treats it identically to any
//       other Dispatch (harness -> artifact update). No consultation is needed.
//       At the review loop limit (persisted in the artifact state the engine
//       receives) the engine returns a Deviation instead.
//     - Stage-* output re-derivation: after a completed row's output artifacts
//       include a Stage-* pattern, session re-reads the plan artifact via
//       planstages to obtain a refreshed stage set for the engine's next call.
//     - Deviation: engine returns Deviation; session routes through the
//       RoutingConsultant (consultRoute). If no consultant is wired, the run
//       terminates with RunDeviationUnresolved.
//     - Harness error: a harness-level failure (timeout, launch failure) is
//       recorded as a synthetic BLOCKED response with error code E501, whose
//       status message and error reason are the Runner's error description
//       ("never a crash" per port contract). It is persisted as an accepted
//       workflow row (current_state.error_code E501) and routed through the
//       RoutingConsultant as an ordinary BLOCKED/E501 deviation; the next
//       consultation request carries the description as last_error_reason.
//     - HITL verification: after every attempt of an HITL-dispatched step, whatever
//       its status, the outputs the attempt actually created or modified (found
//       by the OutputWriteDetector against a baseline taken before the first
//       attempt) are checked for human_approved. Declared outputs that were not
//       written are neither registered nor gated. A non-compliant result
//       triggers one HITL redispatch; if the redispatch is also non-compliant,
//       the deviation is escalated to the RoutingConsultant. Only written
//       outputs are registered in the Artifacts table.
//       A BLOCKED response with error code E503 (the agent could not reach the
//       user) is exempt: it is accepted without a redispatch and routed as
//       BLOCKED. When a gate-discharging redispatch returns SUCCESS, its row is
//       recorded as returned under the redispatch's agent instance, but
//       current_state, routing and trigger evaluation follow the original
//       attempt's status and error code.
//     - Consultation recording: a RoutingConsultant invocation (routing,
//       pre-consultation, stop or failure) writes nothing to the artifact: no
//       Execution Log row, no global_sequence advance, no current_state change.
//       It stays visible in the diagnostic, dispatch and MOSAIC logs. After a
//       successful routing consultation, and before any later state write, the
//       session re-reads the artifact so any Workflow Notes the script
//       orchestrator appended directly during its deliberation are preserved;
//       the session's local sequence then follows the re-read artifact's
//       global_sequence, so the agent the consultation dispatches takes the
//       next Seq directly, with no collision from a consultation row.
//     - Graceful stop: session records current state and returns RunStopped.
//     - Infrastructure-agent trigger: a named no-op hook is called after each
//       harness invocation (FR-40).
//
// Both frontends (TUI and CLI) drive the same Session surface.
package session

import (
	"context"
	"errors"
	"fmt"
	"os"

	commonharness "mosaic-common/harness"
	"mosaic-run/internal/domain"
	"mosaic-run/internal/snapshot/lockprotocol"
)

// Session is the use-case layer that drives the execution loop.
// Both frontends (TUI and CLI) drive it through this surface.
type Session interface {
	// Start begins or resumes a run. It performs the full run-start sequence
	// and then enters the dispatch loop.
	//
	// Progress is reported through the Interaction port's Progress and Notify
	// methods.
	//
	// Returns the run outcome when the run completes, stops, or is refused.
	// Refusals (pre-invocation failures) are returned as RunOutcome{Status:
	// RunRefused} with a nil error. A commit setup or pre-consultation failure
	// after the artifact exists is RunOutcome{Status: RunStartFailed}, also
	// with a nil error; the artifact is kept and a resume retries the step. Unexpected infrastructure failures return
	// a non-nil error.
	Start(ctx context.Context, config domain.RunConfig) (domain.RunOutcome, error)
}

// Deps collects all port dependencies required by the session.
// Every field is a port (interface); no concrete types are used.
type Deps struct {
	// Harness dispatches subagent invocations. Required. A nil value makes
	// Start refuse the run (MissingPortError) before any artifact write or
	// dispatch.
	Harness domain.HarnessAdapter
	// Store reads and writes the Orchestration.md artifact. Required; see Harness.
	Store domain.ArtifactStore
	// Clock provides deterministic timestamps. Required; see Harness.
	Clock domain.Clock
	// Interact provides the user-interaction channel (progress events, etc.).
	// Required; see Harness.
	Interact domain.Interaction
	// PreConsult performs the one-shot run-start pre-consultation (auto and
	// auto-review modes only). Required when the run's effective settings enable
	// pre-consultation in auto or auto-review mode; checked after resume
	// reconciliation and before any artifact write. Otherwise optional and
	// never called.
	PreConsult domain.PreConsultant
	// OnInfrastructureTrigger is an optional hook called after each harness
	// invocation (FR-40). If nil, no action is taken. In production this is the
	// named no-op; tests inject a counter function to verify the hook is invoked
	// exactly once per dispatch cycle (AC8.7).
	OnInfrastructureTrigger func()

	// Debug records dispatch events to the run's debug log, including run-start
	// refusals and unresolved deviations that never reach Store.Apply and so
	// leave no trace in Orchestration.md.
	//
	// Optional: nil is normalised to domain.NopDebugLogger in New. Behaviour is
	// otherwise identical with and without a logger.
	Debug domain.DebugLogger

	// DispatchLog records the full ProtocolRequest/ProtocolResponse pairs for
	// every subagent invocation to a dedicated JSONL log file.
	//
	// Optional: nil is normalised to domain.NopDispatchLogger in New, so the
	// dispatch loop never nil-checks it.
	DispatchLog domain.DispatchLogger

	// Routing is the configured routing decision-maker consulted whenever the
	// engine yields a Consult or Deviation decision. In production this is the
	// OrchestratorConsultant; a run started with manual resolution still uses
	// it first and falls back to Manual on a consultation failure.
	//
	// Nil is permitted only when the run's mode does not require routing
	// consultations (i.e. auto/auto-review with no expected deviations in
	// tests). The session must not call it when nil.
	Routing domain.RoutingConsultant

	// Manual is the fallback resolver used when a consultation fails and the
	// run's ManualResolution setting is enabled. Required when the run's
	// effective settings enable manual resolution; checked at the same point as
	// PreConsult. When wired, it also serves a ManualDispatch restart.
	Manual domain.RoutingConsultant

	// Approvals reads human_approved from dispatched output artifacts for HITL
	// compliance verification. Nil is normalised in New to a reader that
	// reports ApprovalUnreadable, so the session never nil-checks it.
	Approvals domain.ApprovalReader

	// Outputs detects which declared outputs an invocation wrote. Nil is
	// normalised in New to a conservative detector that reports every
	// declared concrete path as written. Production wires
	// artifact.NewOutputWriteDetector().
	Outputs domain.OutputWriteDetector

	// StopRequested reports whether a graceful stop has been confirmed. The
	// dispatch loop polls it at safe boundaries (immediately before each
	// invokeAndLog call, never mid-invocation) and never inspects ctx for
	// this purpose -- ctx cancellation remains the separate, unchanged
	// hard-cancel (ctrl+c) path.
	//
	// Optional: nil is normalised in New to a function that always returns
	// false, so the dispatch loop never nil-checks it.
	StopRequested func() bool

	// BackupStateHook is an optional function called immediately after
	// SetupBackupAndTransform returns a non-nil *BackupState, before the
	// deferred Cleanup is registered. Tests use this to inject RestoreFunc
	// (an injectable error seam on BackupState) so that Cleanup can be forced
	// to fail without relying on filesystem tricks. Nil in production.
	BackupStateHook func(*lockprotocol.BackupState)
}

// New creates a new Session with the given port dependencies.
//
// A nil Deps.Debug is replaced with domain.NopDebugLogger before the session
// is returned, so the dispatch loop never nil-checks the logger.
//
// The returned session uses the following fixed-path dependencies from the
// runner's package set: orchfile, workflow, compat, agentresolve, planstages,
// and engine. These are not behind ports because they are pure functions or
// read-only loaders that impose no testability burden of their own.
func New(deps Deps) Session {
	if deps.Debug == nil {
		deps.Debug = domain.NopDebugLogger{}
	}
	if deps.DispatchLog == nil {
		deps.DispatchLog = domain.NopDispatchLogger{}
	}
	if deps.Approvals == nil {
		deps.Approvals = unreadableApprovalReader{}
	}
	if deps.StopRequested == nil {
		deps.StopRequested = func() bool { return false }
	}
	return &sessionImpl{deps: deps}
}

// unreadableApprovalReader is the zero-value ApprovalReader used when
// Deps.Approvals is nil. It reports every artifact as ApprovalUnreadable, so
// the session never nil-checks the reader and a missing reader never silently
// passes the HITL gate.
//
// It also implements domain.ApprovalCapability, returning false, so the
// run-start refusal check can distinguish this stand-in from a real reader.
type unreadableApprovalReader struct{}

func (unreadableApprovalReader) ReadApproval(_ context.Context, _ string) domain.HumanApproval {
	return domain.ApprovalUnreadable
}

func (unreadableApprovalReader) ApprovalsReadable() bool { return false }

// sessionImpl is the concrete implementation of Session.
type sessionImpl struct {
	deps Deps
	// orchRef is the resolved orchestrator agent reference. It is assigned at
	// step 2a of Start and used when recording each consultation log row so
	// that the row's AgentInstance carries the real orchestrator identifier
	// (derived from the orchestrator file stem) rather than any hardcoded value.
	orchRef domain.AgentReference
	// manualDispatchPending tracks the one-shot ManualDispatch signal from the
	// stop-screen recovery action. Set to true at the start of each Start call
	// when config.ManualDispatch is true; cleared after the first consultRoute
	// call so that subsequent routing decisions use the configured consultant.
	manualDispatchPending bool
	// outputBaseline is the pre-invocation output snapshot of the step in
	// flight. It is set before the first attempt and reused by re-dispatches.
	outputBaseline domain.OutputBaseline
	// snapshotDir is the absolute path to the run-scoped agent snapshot
	// directory created by the copy-and-invoke strategy at step 5b. Empty
	// until step 5b succeeds for path-based harnesses. Used by cleanup to
	// know what to delete on terminal completion.
	snapshotDir string
	// backupState is the handle returned by SetupBackupAndTransform for
	// name-based harnesses (backup-and-transform strategy). Nil for path-based
	// harnesses and non-CLI harnesses. Its Cleanup method is registered in a
	// defer immediately after setup succeeds.
	backupState *lockprotocol.BackupState
}

// isRawTextHarnessError reports whether err is a raw-text protocol failure:
// the harness received output from the subprocess but could not extract a
// valid protocol JSON response. These errors are worth a single direct retry
// because they are often caused by the model writing a plain-text reply
// instead of the expected JSON structure, and a retry typically succeeds.
// Transport-level failures (timeouts, non-zero exits) do not qualify.
func isRawTextHarnessError(err error) bool {
	return errors.Is(err, commonharness.ErrProtocolNotExtractable) ||
		errors.Is(err, commonharness.ErrMalformedJSON) ||
		errors.Is(err, commonharness.ErrEmptyResponse)
}

// invokeAndLog wraps s.deps.Harness.Invoke with dispatch logging. It logs the
// full request immediately before the invocation and the full response (or a
// harness-level error) immediately after. The response and error are returned
// unchanged so callers need only change the method name.
func (s *sessionImpl) invokeAndLog(ctx context.Context, agentRef domain.AgentReference, request domain.ProtocolRequest) (domain.ProtocolResponse, error) {
	s.deps.DispatchLog.LogRequest(request)
	resp, err := s.deps.Harness.Invoke(ctx, agentRef, request)
	if err != nil {
		s.deps.DispatchLog.LogError(request.AgentInstanceID, err.Error())
	} else {
		s.deps.DispatchLog.LogResponse(resp)
	}
	return resp, err
}

// Start implements Session.
func (s *sessionImpl) Start(ctx context.Context, config domain.RunConfig) (outcome domain.RunOutcome, err error) {
	if reqErr := s.deps.CheckRequired(); reqErr != nil {
		return s.refusalCaused(reqErr.Error(), reqErr), nil
	}
	rs, outcome, done, err := s.prepareRouting(ctx, config)
	if done {
		return outcome, err
	}
	if outcome, done, err = s.readArtifact(ctx, rs); done {
		return outcome, err
	}
	if outcome, done, err = s.admitAndResolveAgents(ctx, rs); done {
		return outcome, err
	}
	defer func() {
		if s.snapshotDir == "" {
			return
		}
		if rmErr := os.RemoveAll(s.snapshotDir); rmErr != nil {
			s.deps.Debug.Log(domain.EventSnapshotCleanupFailed,
				fmt.Sprintf("failed to remove snapshot directory %s: %v", s.snapshotDir, rmErr))
		}
	}()
	defer func() {
		if s.backupState == nil {
			return
		}
		if cleanupErr := s.backupState.Cleanup(); cleanupErr != nil {
			s.deps.Debug.Log(domain.EventSnapshotCleanupFailed, cleanupErr.Error())
		}
	}()
	if outcome, done, err = s.setupSnapshotOrBackup(ctx, rs); done {
		return outcome, err
	}
	if outcome, done, err = s.setupInfraAndStages(ctx, rs); done {
		return outcome, err
	}
	if outcome, done, err = s.createOrResumeArtifact(ctx, rs); done {
		return outcome, err
	}
	if outcome, done, err = s.applyOverridesAndPreConsult(ctx, rs); done {
		return outcome, err
	}
	return s.runDispatchLoop(ctx, rs)
}
