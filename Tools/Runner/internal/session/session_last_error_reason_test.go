package session_test

// Tests for the error reason carried on routing consultations and for harness
// errors recorded as BLOCKED/E501 workflow outcomes on every path that can hit
// one (auto dispatch, HITL re-dispatch, consultant-routed dispatch).
//
// Consultation behavior is proven through the captured ConsultationRequest,
// never through the Execution Log rows a consultation may or may not write.

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	commonharness "mosaic-common/harness"
	"mosaic-run/internal/domain"
	"mosaic-run/internal/harness"
)

// ===== helpers =====

// blockedResponse builds a BLOCKED protocol response with the given error
// code and reason.
func blockedResponse(instance string, code domain.ErrorCode, message, reason string) *domain.ProtocolResponse {
	return &domain.ProtocolResponse{
		AgentInstanceID: instance,
		StatusCode:      domain.StatusBLOCKED,
		StatusMessage:   message,
		ErrorCode:       code,
		ErrorReason:     reason,
	}
}

// requireHarnessErrorRow asserts store.Applied holds an accepted workflow row
// for the failed attempt: BLOCKED/E501, neither infrastructure nor HITL
// rejected, naming agent and carrying the failure description. Returns it.
func requireHarnessErrorRow(t *testing.T, store *memStore, agent, wantInSummary string) domain.CompletedStep {
	t.Helper()
	for _, s := range store.Applied {
		if s.Status == domain.StatusBLOCKED && s.ErrorCode == domain.ErrorTOOL_UNAVAILABLE &&
			strings.HasPrefix(s.AgentInstance, agent+"#") {
			if s.IsInfrastructure {
				t.Errorf("want the harness-error row of %s to be a workflow row, got IsInfrastructure=true", agent)
			}
			if s.HITLRejected {
				t.Errorf("want the harness-error row of %s not HITL-gated, got HITLRejected=true", agent)
			}
			if !strings.Contains(s.Summary, wantInSummary) {
				t.Errorf("want harness-error row Summary to contain %q, got %q", wantInSummary, s.Summary)
			}
			return s
		}
	}
	t.Fatalf("want a BLOCKED/E501 row for %s in store.Applied, got %+v", agent, store.Applied)
	return domain.CompletedStep{}
}

// requireCurrentStateBlockedE501 asserts the persisted workflow position is
// the failed step.
func requireCurrentStateBlockedE501(t *testing.T, store *memStore, wantAgentInstance string) {
	t.Helper()
	cs := store.state.CurrentState
	if cs.LastStatus != domain.StatusBLOCKED {
		t.Errorf("want current_state.last_status BLOCKED, got %q", cs.LastStatus)
	}
	if cs.ErrorCode != domain.ErrorTOOL_UNAVAILABLE {
		t.Errorf("want current_state.error_code E501, got %q", cs.ErrorCode)
	}
	if cs.LastAgent != wantAgentInstance {
		t.Errorf("want current_state.last_agent %q, got %q", wantAgentInstance, cs.LastAgent)
	}
}

// requireErrorReasonCarries asserts a consultation request carries the harness
// failure description as both last_status_message and last_error_reason.
func requireErrorReasonCarries(t *testing.T, req domain.ConsultationRequest, fragment string) {
	t.Helper()
	if req.LastErrorReason == nil {
		t.Fatal("want last_error_reason set to the harness error description, got nil")
	}
	if !strings.Contains(*req.LastErrorReason, fragment) {
		t.Errorf("want last_error_reason to contain %q, got %q", fragment, *req.LastErrorReason)
	}
	if req.LastStatusMessage == nil || *req.LastStatusMessage != *req.LastErrorReason {
		t.Errorf("want last_status_message equal to last_error_reason (%q), got %v",
			*req.LastErrorReason, req.LastStatusMessage)
	}
}

// requireBlockedE501Deviation asserts the engine routed the failure as an
// ordinary BLOCKED/E501 non-success deviation.
func requireBlockedE501Deviation(t *testing.T, req domain.ConsultationRequest) {
	t.Helper()
	if req.Deviation == nil {
		t.Fatal("want the consultation to carry a deviation, got nil")
	}
	if req.Deviation.Kind != domain.DeviationNonSuccess {
		t.Errorf("want deviation kind %q, got %q", domain.DeviationNonSuccess, req.Deviation.Kind)
	}
	if req.Deviation.Response.StatusCode != domain.StatusBLOCKED ||
		req.Deviation.Response.ErrorCode != domain.ErrorTOOL_UNAVAILABLE {
		t.Errorf("want deviating response BLOCKED/E501, got %s/%s",
			req.Deviation.Response.StatusCode, req.Deviation.Response.ErrorCode)
	}
}

// ===== last_error_reason population =====

// A BLOCKED response (here an E100 rejection) hands its error_reason to the
// consultation verbatim.
func TestSession_LastErrorReason_BlockedResponseReasonReachesConsultationVerbatim(t *testing.T) {
	const reason = "task_description is missing\nsecond line | with pipe"
	consultant := &scriptedRoutingConsultant{}
	consultant.queueStop("done")
	ses, f, _, orchPath := newAutoSessionWithConsultant(t, consultant)
	f.Queue("agent-a", harness.ScriptedEntry{Response: blockedResponse(
		"agent-a#1", domain.ErrorINVALID_INVOCATION, "invalid invocation", reason)})

	ses.Start(context.Background(), baseLinearConfig(orchPath)) //nolint:errcheck

	if len(consultant.Requests) == 0 {
		t.Fatal("want a consultation after the BLOCKED response, got none")
	}
	got := consultant.Requests[0].LastErrorReason
	if got == nil {
		t.Fatal("want last_error_reason set for a BLOCKED trigger, got nil")
	}
	if *got != reason {
		t.Errorf("want last_error_reason %q verbatim, got %q", reason, *got)
	}
}

// Statuses other than BLOCKED never send an error reason, even if the response
// carries a stray one.
func TestSession_LastErrorReason_NonBlockedStatusSendsNull(t *testing.T) {
	consultant := &scriptedRoutingConsultant{}
	consultant.queueStop("done")
	ses, f, _, orchPath := newAutoSessionWithConsultant(t, consultant)
	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#1",
		StatusCode:      domain.StatusPARTIALLY_DONE,
		StatusMessage:   "only half done",
		ErrorReason:     "stray reason that must not be forwarded",
	}})

	ses.Start(context.Background(), baseLinearConfig(orchPath)) //nolint:errcheck

	if len(consultant.Requests) == 0 {
		t.Fatal("want a consultation after the PARTIALLY_DONE response, got none")
	}
	req := consultant.Requests[0]
	if req.LastStatusMessage == nil || *req.LastStatusMessage != "only half done" {
		t.Errorf("want last_status_message %q, got %v", "only half done", req.LastStatusMessage)
	}
	if req.LastErrorReason != nil {
		t.Errorf("want last_error_reason null for PARTIALLY_DONE, got %q", *req.LastErrorReason)
	}
}

// The first consultation of a run has no triggering response: both fields null.
func TestSession_LastErrorReason_FirstStepSendsNull(t *testing.T) {
	consultant := &scriptedRoutingConsultant{}
	consultant.queueStop("stop immediately")
	ses, _, _, orchPath := newOrchestratedSession(t, consultant)

	ses.Start(context.Background(), baseOrchestratedConfig(orchPath)) //nolint:errcheck

	if len(consultant.Requests) == 0 {
		t.Fatal("want a first-step consultation, got none")
	}
	req := consultant.Requests[0]
	if req.LastStatusMessage != nil {
		t.Errorf("want last_status_message null on the first step, got %q", *req.LastStatusMessage)
	}
	if req.LastErrorReason != nil {
		t.Errorf("want last_error_reason null on the first step, got %q", *req.LastErrorReason)
	}
}

// Orchestrated mode: a SUCCESS step is followed by a consultation with a null
// error reason; a BLOCKED step is followed by one carrying its reason.
func TestSession_LastErrorReason_OrchestratedFollowsTriggeringResponse(t *testing.T) {
	const reason = "cannot read input plan.md"
	consultant := &scriptedRoutingConsultant{}
	consultant.queueDispatch("agent-a", "step one", 0)
	consultant.queueDispatch("agent-b", "step two", 1)
	consultant.queueStop("done")
	ses, f, _, orchPath := newOrchestratedSession(t, consultant)
	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#2", StatusCode: domain.StatusSUCCESS, StatusMessage: "planned"}})
	f.Queue("agent-b", harness.ScriptedEntry{Response: blockedResponse(
		"agent-b#4", domain.ErrorINPUT_NOT_FOUND, "input missing", reason)})

	ses.Start(context.Background(), baseOrchestratedConfig(orchPath)) //nolint:errcheck

	if len(consultant.Requests) != 3 {
		t.Fatalf("want 3 consultations (first step, after SUCCESS, after BLOCKED), got %d", len(consultant.Requests))
	}
	if r := consultant.Requests[1].LastErrorReason; r != nil {
		t.Errorf("want last_error_reason null after a SUCCESS response, got %q", *r)
	}
	r := consultant.Requests[2].LastErrorReason
	if r == nil || *r != reason {
		t.Errorf("want last_error_reason %q after the BLOCKED response, got %v", reason, r)
	}
}

// ===== harness errors: auto path =====

func TestSession_HarnessError_AutoPath_RecordedAsBlockedE501AndRoutedAsDeviation(t *testing.T) {
	modes := []domain.ExecutionMode{domain.ExecutionModeAuto, domain.ExecutionModeAutoReview}
	failures := []string{
		"harness: subprocess timed out after 60s",
		"harness: launch failed: executable not found",
	}
	for _, mode := range modes {
		for _, failure := range failures {
			t.Run(fmt.Sprintf("%s/%s", mode, failure), func(t *testing.T) {
				consultant := &scriptedRoutingConsultant{}
				consultant.queueStop("done")
				ses, f, store, orchPath := newAutoSessionWithConsultant(t, consultant)
				f.Queue("agent-a", harness.ScriptedEntry{Err: errors.New(failure)})
				cfg := baseLinearConfig(orchPath)
				cfg.RunSettings.Mode = mode

				ses.Start(context.Background(), cfg) //nolint:errcheck

				row := requireHarnessErrorRow(t, store, "agent-a", failure)
				requireCurrentStateBlockedE501(t, store, row.AgentInstance)
				if len(consultant.Requests) == 0 {
					t.Fatal("want the engine deviation to reach the consultant, got no consultation")
				}
				requireBlockedE501Deviation(t, consultant.Requests[0])
				requireErrorReasonCarries(t, consultant.Requests[0], failure)
			})
		}
	}
}

// The first attempt's E501 row is recorded before the raw-text bypass retry,
// even when the retry then succeeds; the run routes on the retry.
func TestSession_HarnessError_BypassFirstAttempt_E501RowRecordedBeforeRetry(t *testing.T) {
	consultant := &scriptedRoutingConsultant{}
	ses, f, store, orchPath := newAutoSessionWithConsultant(t, consultant)
	f.Queue("agent-a",
		harness.ScriptedEntry{Err: wrapSentinel(commonharness.ErrProtocolNotExtractable)},
		harness.ScriptedEntry{Response: &domain.ProtocolResponse{
			AgentInstanceID: "agent-a#2", StatusCode: domain.StatusSUCCESS, StatusMessage: "done on retry"}},
	)
	f.Queue("agent-b", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-b#3", StatusCode: domain.StatusSUCCESS, StatusMessage: "done"}})

	got, err := ses.Start(context.Background(), baseLinearConfig(orchPath))

	if err != nil {
		t.Fatalf("want nil error, got %v", err)
	}
	if got.Status != domain.RunCompleted {
		t.Errorf("want RunCompleted (routed on the retry), got %q (%s)", got.Status, got.Message)
	}
	if consultant.CallCount != 0 {
		t.Errorf("want no consultation when the retry succeeds, got %d", consultant.CallCount)
	}
	var aRows []domain.CompletedStep
	for _, s := range store.Applied {
		if strings.HasPrefix(s.AgentInstance, "agent-a#") && !s.IsInfrastructure {
			aRows = append(aRows, s)
		}
	}
	if len(aRows) != 2 {
		t.Fatalf("want 2 agent-a workflow rows (E501 attempt, then retry), got %d: %+v", len(aRows), aRows)
	}
	if aRows[0].Status != domain.StatusBLOCKED || aRows[0].ErrorCode != domain.ErrorTOOL_UNAVAILABLE {
		t.Errorf("want first agent-a row BLOCKED/E501, got %s/%s", aRows[0].Status, aRows[0].ErrorCode)
	}
	if aRows[1].Status != domain.StatusSUCCESS {
		t.Errorf("want second agent-a row SUCCESS, got %s", aRows[1].Status)
	}
	if aRows[1].Seq <= aRows[0].Seq {
		t.Errorf("want strictly increasing Seq across the attempts, got %d then %d", aRows[0].Seq, aRows[1].Seq)
	}
	last := 0
	for _, s := range store.Applied {
		if s.Seq <= last {
			t.Errorf("want Seq strictly increasing across all rows, got %d after %d", s.Seq, last)
		}
		last = s.Seq
	}
}

// ===== harness errors: HITL re-dispatch path =====

func TestSession_HarnessError_HITLRedispatch_RecordedAsBlockedE501NotGated(t *testing.T) {
	const failure = "harness: subprocess timed out during HITL re-dispatch"
	consultant := &scriptedRoutingConsultant{}
	consultant.queueStop("done")
	ses, f, store, orchPath := newAutoHITLSession(t, &fixedApprovalReader{domain.ApprovalFalse}, consultant)
	f.Queue("agent-a",
		harness.ScriptedEntry{Response: &domain.ProtocolResponse{
			AgentInstanceID: "agent-a#1", StatusCode: domain.StatusSUCCESS, StatusMessage: "initial"}},
		harness.ScriptedEntry{Err: errors.New(failure)},
	)

	ses.Start(context.Background(), baseLinearConfig(orchPath)) //nolint:errcheck

	var rejected int
	for _, s := range store.Applied {
		if s.HITLRejected {
			rejected++
		}
	}
	if rejected != 1 {
		t.Errorf("want exactly the initial attempt HITL-rejected, got %d rejected rows", rejected)
	}
	row := requireHarnessErrorRow(t, store, "agent-a", failure)
	requireCurrentStateBlockedE501(t, store, row.AgentInstance)
	if len(consultant.Requests) == 0 {
		t.Fatal("want the engine deviation to reach the consultant, got no consultation")
	}
	requireBlockedE501Deviation(t, consultant.Requests[0])
	requireErrorReasonCarries(t, consultant.Requests[0], failure)
}

// ===== harness errors: consultant-routed path =====

func TestSession_HarnessError_ConsultPath_RecordedAsBlockedE501AndNextConsultationCarriesDescription(t *testing.T) {
	const failure = "harness: launch failed: permission denied"
	consultant := &scriptedRoutingConsultant{}
	consultant.queueDispatch("agent-a", "try the work", 0)
	consultant.queueStop("done")
	ses, f, store, orchPath := newOrchestratedSession(t, consultant)
	f.Queue("agent-a", harness.ScriptedEntry{Err: errors.New(failure)})

	ses.Start(context.Background(), baseOrchestratedConfig(orchPath)) //nolint:errcheck

	row := requireHarnessErrorRow(t, store, "agent-a", failure)
	requireCurrentStateBlockedE501(t, store, row.AgentInstance)
	if len(consultant.Requests) < 2 {
		t.Fatalf("want a consultation after the harness error, got %d requests", len(consultant.Requests))
	}
	requireErrorReasonCarries(t, consultant.Requests[1], failure)
}
