package session_test

// Test doubles for session tests: in-memory ArtifactStore, fixed-time Clock,
// no-op Interaction, callback harness wrapper, and orchestrator-reference
// capture consultant.

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	"mosaic-common/interaction"
	"mosaic-run/internal/domain"
)

// ---- Stage-6 test doubles ----

// scriptedRoutingConsultant implements domain.RoutingConsultant with a scripted
// sequence of RoutingInstruction/error pairs consumed in FIFO order.
// After the queue is exhausted, ConsultRouting returns a ConsultFailTransport
// error so tests that over-consult fail rather than panic.
type scriptedRoutingConsultant struct {
	instructions []domain.RoutingInstruction
	errors       []error // parallel slice; nil element = no error for that call
	idx          int
	CallCount    int
	Requests     []domain.ConsultationRequest
}

func (s *scriptedRoutingConsultant) ConsultRouting(_ context.Context, req domain.ConsultationRequest) (domain.RoutingInstruction, error) {
	s.CallCount++
	s.Requests = append(s.Requests, req)
	if s.idx >= len(s.instructions) {
		return domain.RoutingInstruction{}, &domain.ConsultationError{
			Failure: domain.ConsultFailTransport,
			Detail:  fmt.Sprintf("scriptedRoutingConsultant: queue exhausted (call #%d)", s.CallCount),
		}
	}
	instr := s.instructions[s.idx]
	var err error
	if s.idx < len(s.errors) {
		err = s.errors[s.idx]
	}
	s.idx++
	return instr, err
}

func (s *scriptedRoutingConsultant) queueDispatch(agent, taskDesc string, rowIndex int) {
	s.instructions = append(s.instructions, domain.RoutingInstruction{
		Dispatch: &domain.DispatchInstruction{
			Agent:           agent,
			RowIndex:        rowIndex,
			TaskDescription: taskDesc,
		},
	})
	s.errors = append(s.errors, nil)
}

func (s *scriptedRoutingConsultant) queueDispatchWithConstraints(agent, taskDesc string, rowIndex int, constraints *string) {
	s.instructions = append(s.instructions, domain.RoutingInstruction{
		Dispatch: &domain.DispatchInstruction{
			Agent:           agent,
			RowIndex:        rowIndex,
			TaskDescription: taskDesc,
			Constraints:     constraints,
		},
	})
	s.errors = append(s.errors, nil)
}

func (s *scriptedRoutingConsultant) queueDispatchWithInputs(agent, taskDesc string, rowIndex int, inputs *[]string) {
	s.instructions = append(s.instructions, domain.RoutingInstruction{
		Dispatch: &domain.DispatchInstruction{
			Agent:           agent,
			RowIndex:        rowIndex,
			TaskDescription: taskDesc,
			InputArtifacts:  inputs,
		},
	})
	s.errors = append(s.errors, nil)
}

func (s *scriptedRoutingConsultant) queueDispatchWithOutputs(agent, taskDesc string, rowIndex int, outputs *[]string) {
	s.instructions = append(s.instructions, domain.RoutingInstruction{
		Dispatch: &domain.DispatchInstruction{
			Agent:           agent,
			RowIndex:        rowIndex,
			TaskDescription: taskDesc,
			OutputArtifacts: outputs,
		},
	})
	s.errors = append(s.errors, nil)
}

func (s *scriptedRoutingConsultant) queueDispatchWithHITL(agent, taskDesc string, rowIndex int, hitlOverride *bool) {
	s.instructions = append(s.instructions, domain.RoutingInstruction{
		Dispatch: &domain.DispatchInstruction{
			Agent:           agent,
			RowIndex:        rowIndex,
			TaskDescription: taskDesc,
			HITLOverride:    hitlOverride,
		},
	})
	s.errors = append(s.errors, nil)
}

func (s *scriptedRoutingConsultant) queueDispatchWithHITLAndOutputs(agent, taskDesc string, rowIndex int, hitlOverride *bool, outputs *[]string) {
	s.instructions = append(s.instructions, domain.RoutingInstruction{
		Dispatch: &domain.DispatchInstruction{
			Agent:           agent,
			RowIndex:        rowIndex,
			TaskDescription: taskDesc,
			HITLOverride:    hitlOverride,
			OutputArtifacts: outputs,
		},
	})
	s.errors = append(s.errors, nil)
}

func (s *scriptedRoutingConsultant) queueStop(reason string) {
	s.instructions = append(s.instructions, domain.RoutingInstruction{
		Stop: &domain.StopInstruction{Reason: reason},
	})
	s.errors = append(s.errors, nil)
}

func (s *scriptedRoutingConsultant) queueError(failure domain.ConsultationFailure) {
	s.instructions = append(s.instructions, domain.RoutingInstruction{})
	s.errors = append(s.errors, &domain.ConsultationError{
		Failure: failure,
		Detail:  "scripted consultation error",
	})
}

// fixedApprovalReader implements domain.ApprovalReader, always returning the
// same HumanApproval value for every path.
type fixedApprovalReader struct {
	approval domain.HumanApproval
}

func (r *fixedApprovalReader) ReadApproval(_ context.Context, _ string) domain.HumanApproval {
	return r.approval
}

// perPathApprovalReader implements domain.ApprovalReader, returning a
// specific approval value for named paths and a fallback for all others.
type perPathApprovalReader struct {
	specific map[string]domain.HumanApproval
	fallback domain.HumanApproval
}

func (r *perPathApprovalReader) ReadApproval(_ context.Context, path string) domain.HumanApproval {
	if v, ok := r.specific[path]; ok {
		return v
	}
	return r.fallback
}

// switchingApprovalReader returns firstApproval on the first ReadApproval call
// and restApproval for all subsequent calls. Used in HITL tests to simulate an
// initially non-compliant artifact that becomes compliant after one redispatch,
// so the session exercises exactly one rejection before accepting.
type switchingApprovalReader struct {
	mu            sync.Mutex
	calls         int
	firstApproval domain.HumanApproval
	restApproval  domain.HumanApproval
}

func (r *switchingApprovalReader) ReadApproval(_ context.Context, _ string) domain.HumanApproval {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls++
	if r.calls == 1 {
		return r.firstApproval
	}
	return r.restApproval
}

// applyBeforeConsultConsultant wraps a scriptedRoutingConsultant and records,
// at each ConsultRouting call, the number of Apply calls already made and the
// current Read count. Used by write-discipline tests.
type applyBeforeConsultConsultant struct {
	inner       *scriptedRoutingConsultant
	store       *memStore
	ApplyAtCall []int // len(store.Applied) at the moment of each ConsultRouting call
	ReadAtCall  []int // store.ReadCount at the moment of each ConsultRouting call
}

func (c *applyBeforeConsultConsultant) ConsultRouting(ctx context.Context, req domain.ConsultationRequest) (domain.RoutingInstruction, error) {
	c.ApplyAtCall = append(c.ApplyAtCall, len(c.store.Applied))
	c.ReadAtCall = append(c.ReadAtCall, c.store.ReadCount)
	return c.inner.ConsultRouting(ctx, req)
}

// ---- in-memory ArtifactStore ----

// memStore is an in-memory ArtifactStore for use in session tests.
// It starts empty (ErrNotExist on Read) and records every Apply call.
type memStore struct {
	state          domain.ArtifactState
	exists         bool
	readErr        error
	Applied        []domain.CompletedStep
	ReadCount      int    // counts every call to Read; used to verify re-read after deviation
	CreatedRunID   string // records the runID argument passed to Create; used to verify AC7.7

	// applyErrOnFirst, when true, causes Apply to return applyFirstErr on the
	// very first Apply call (when Applied is still empty). Used to simulate
	// storage failures immediately after artifact creation, e.g. for the
	// commit setup row recording failure path (Plan Risks §1).
	applyErrOnFirst bool
	applyFirstErr   error

	// BranchCalls records every branch name passed to SetCommitBranch.
	BranchCalls []string

	// AdoptCalls records every AdoptRunnerSettings call; AdoptWrites counts the
	// ones that changed the stored artifact.
	AdoptCalls  []adoptCall
	AdoptWrites int

	// keepRecordedIdentity, when true, makes Read report state.RunID exactly
	// as recorded, including an absent one. By default a recorded state that
	// names no run_id is reported under testRunID, standing in for a
	// well-formed artifact.
	keepRecordedIdentity bool
}

func (m *memStore) Read(_ context.Context) (domain.ArtifactState, error) {
	m.ReadCount++
	if m.readErr != nil {
		return domain.ArtifactState{}, m.readErr
	}
	if !m.exists {
		return domain.ArtifactState{}, os.ErrNotExist
	}
	st := m.state
	if st.RunID == "" && !m.keepRecordedIdentity {
		st.RunID = testRunID
	}
	return st, nil
}

func (m *memStore) Create(_ context.Context, info domain.WorkflowInfo, task string, settings domain.RunSettings, now time.Time, runID string) (domain.ArtifactState, error) {
	m.CreatedRunID = runID // record for AC7.7 assertion
	if runID == "" {
		runID = testRunID // frontends always supply an identity; the double stands in for one
	}
	m.state = domain.ArtifactState{
		Type:            "orchestration-artifact",
		RunID:           runID,
		Workflow:        info.ID,
		WorkflowVersion: info.Version,
		Task:            task,
		Started:         now,
		LastUpdated:     now,
		GlobalSequence:  0,
		RunSettings:     settings,
	}
	m.exists = true
	return m.state, nil
}

func (m *memStore) Apply(_ context.Context, state domain.ArtifactState, step domain.CompletedStep) (domain.ArtifactState, error) {
	if m.applyErrOnFirst && len(m.Applied) == 0 {
		return domain.ArtifactState{}, m.applyFirstErr
	}
	m.Applied = append(m.Applied, step)
	state.GlobalSequence = step.Seq
	// current_state is updated only for workflow steps, matching the real
	// fileStore's contract (ContractsDesign.md, domain.ArtifactStore.Apply):
	// an infrastructure step must not move the recorded workflow position.
	if !step.IsInfrastructure && !step.HITLRejected {
		state.CurrentState = domain.CurrentState{
			Phase:      step.Phase,
			Stage:      step.Stage,
			LastStatus: step.RoutedStatus(),
			LastAgent:  step.AgentInstance,
			ErrorCode:  step.RoutedErrorCode(),
		}
	}
	entry := domain.ExecutionLogEntry{
		Seq:         step.Seq,
		Agent:       step.AgentInstance,
		Phase:       step.Phase,
		Stage:       step.Stage,
		WorkflowRow: step.WorkflowRow,
		Status:      step.Status,
	}
	// Like the file store, only a BLOCKED row carries its error code in the log.
	if step.Status == domain.StatusBLOCKED {
		entry.ErrorCode = step.ErrorCode
	}
	state.ExecutionLog = append(state.ExecutionLog, entry)
	m.state = state
	return state, nil
}

func (m *memStore) SetPhase(_ context.Context, _ domain.ArtifactState, _ string, _ time.Time) (domain.ArtifactState, error) {
	return domain.ArtifactState{}, fmt.Errorf("memStore.SetPhase: not implemented (session tests do not exercise SetPhase)")
}

// SetCommitBranch mirrors the store contract: set-once, refused when commits
// are disabled in the artifact or when the branch is empty.
func (m *memStore) SetCommitBranch(_ context.Context, branch string, now time.Time) (domain.ArtifactState, error) {
	m.BranchCalls = append(m.BranchCalls, branch)
	if !m.state.RunSettings.Commits || m.state.CommitBranch != "" || branch == "" {
		return domain.ArtifactState{}, &domain.RefusalError{
			Component: "artifact",
			Resource:  "commit_branch",
			Reason:    "commit_branch cannot be set",
		}
	}
	m.state.CommitBranch = branch
	m.state.LastUpdated = now
	return m.state, nil
}

// AdoptRunnerSettings mirrors the store contract: it records the three
// runner-owned settings once and refuses when they are already recorded or when
// mode is unset. It never touches review_loop_limit, infrastructure_selections
// or commit_branch. Every call is recorded in AdoptCalls (also refused ones),
// and the artifact write count is tracked in AdoptWrites.
func (m *memStore) AdoptRunnerSettings(_ context.Context, mode domain.ExecutionMode, pre, manual bool, now time.Time) (domain.ArtifactState, error) {
	m.AdoptCalls = append(m.AdoptCalls, adoptCall{Mode: mode, PreConsultation: pre, ManualResolution: manual})
	if m.state.Mode != domain.ExecutionModeUnset || mode == domain.ExecutionModeUnset {
		return domain.ArtifactState{}, &domain.RefusalError{
			Component: "artifact",
			Resource:  "runner_mode",
			Reason:    "runner settings cannot be adopted",
		}
	}
	m.state.Mode = mode
	m.state.PreConsultation = pre
	m.state.ManualResolution = manual
	m.state.LastUpdated = now
	m.AdoptWrites++
	return m.state, nil
}

// adoptCall is one recorded AdoptRunnerSettings invocation.
type adoptCall struct {
	Mode             domain.ExecutionMode
	PreConsultation  bool
	ManualResolution bool
}

// ---- fixed-time Clock ----

type fixedClock struct{ t time.Time }

func (c fixedClock) Now() time.Time { return c.t }

var epoch = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

// ---- no-op Interaction ----

type noopInteraction struct{}

func (n *noopInteraction) SelectOne(_ context.Context, _ interaction.ChoiceQuestion) (interaction.ChoiceAnswer, error) {
	return interaction.ChoiceAnswer{Status: interaction.Answered}, nil
}
func (n *noopInteraction) SelectMany(_ context.Context, _ interaction.ChoiceQuestion) (interaction.MultiChoiceAnswer, error) {
	return interaction.MultiChoiceAnswer{Status: interaction.Answered}, nil
}
func (n *noopInteraction) AskText(_ context.Context, _ interaction.TextQuestion) (interaction.TextAnswer, error) {
	return interaction.TextAnswer{Status: interaction.Answered}, nil
}
func (n *noopInteraction) Confirm(_ context.Context, _ interaction.Question) (interaction.ConfirmAnswer, error) {
	return interaction.ConfirmAnswer{Status: interaction.Answered}, nil
}
func (n *noopInteraction) Notify(_ context.Context, _ interaction.Notice)          {}
func (n *noopInteraction) Progress(_ context.Context, _ interaction.ProgressEvent) {}

// ---- callbackHarness ----

// callbackHarness wraps a HarnessAdapter and calls onInvoke after each
// successful invocation. It is used in graceful-stop tests to synchronise a
// context cancellation with the completion of a specific dispatch (so the
// cancellation happens genuinely mid-loop, not before the session starts).
type callbackHarness struct {
	delegate domain.HarnessAdapter
	onInvoke func(agentID string)
}

func (h *callbackHarness) Invoke(ctx context.Context, agent domain.AgentReference, request domain.ProtocolRequest) (domain.ProtocolResponse, error) {
	resp, err := h.delegate.Invoke(ctx, agent, request)
	if err == nil && h.onInvoke != nil {
		h.onInvoke(agent.Identifier)
	}
	return resp, err
}

// ---- beforeInvokeHarness ----

// beforeInvokeHarness wraps a HarnessAdapter and calls before immediately
// before delegating each Invoke, so a test can observe the durable state at
// the moment a given agent is dispatched.
type beforeInvokeHarness struct {
	delegate domain.HarnessAdapter
	before   func(agentID string)
}

func (h *beforeInvokeHarness) Invoke(ctx context.Context, agent domain.AgentReference, request domain.ProtocolRequest) (domain.ProtocolResponse, error) {
	if h.before != nil {
		h.before(agent.Identifier)
	}
	return h.delegate.Invoke(ctx, agent, request)
}

// ---- orchRefCaptureConsultant ----

// orchRefCaptureConsultant is a test-only domain.RoutingConsultant that also
// implements domain.RunContextBinder. Every time the session calls
// BindRunContext, the consultant records the supplied orchestrator reference.
// After session.Start returns, lastBoundOrchRef returns the most-recently
// recorded reference, which should be the snapshot-resolved one if step 5a
// ran and re-bound consultants correctly.
type orchRefCaptureConsultant struct {
	mu    sync.Mutex
	bound []domain.RunContext
}

// BindRunContext implements domain.RunContextBinder.
func (c *orchRefCaptureConsultant) BindRunContext(rc domain.RunContext) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.bound = append(c.bound, rc)
}

// ConsultRouting implements domain.RoutingConsultant. It returns an error so
// that any unexpected consultation call surfaces immediately as a test failure
// rather than hanging or panicking.
func (c *orchRefCaptureConsultant) ConsultRouting(_ context.Context, _ domain.ConsultationRequest) (domain.RoutingInstruction, error) {
	return domain.RoutingInstruction{}, &domain.ConsultationError{
		Failure: domain.ConsultFailTransport,
		Detail:  "orchRefCaptureConsultant: ConsultRouting called unexpectedly in this test",
	}
}

// lastBoundOrchRef returns the orchestrator reference from the most recent
// BindRunContext call, or a zero AgentReference if BindRunContext was never
// called.
func (c *orchRefCaptureConsultant) lastBoundOrchRef() domain.AgentReference {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.bound) == 0 {
		return domain.AgentReference{}
	}
	return c.bound[len(c.bound)-1].Orchestrator
}

// bindCallCount returns how many times BindRunContext was called.
func (c *orchRefCaptureConsultant) bindCallCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.bound)
}
