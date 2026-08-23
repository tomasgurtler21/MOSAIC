// Package intercept holds the pure interception decision core: given a
// normalized intercepted call and the current run state, it decides what
// happens to that call and returns the answer as data.
//
// Imports domain and stubmatch only. Performs no file, network, process or
// clock access; time enters as a parameter (Input.Now).
package intercept

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"time"

	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/stubmatch"
)

// TokenSource names how a post- or completion-phase call's correlation token
// was recovered.
type TokenSource string

const (
	// TokenFromCall: the call carried a non-empty token directly.
	TokenFromCall TokenSource = "call"
	// TokenFromAgent: the call carried no token, and the token was recovered
	// through the dispatch-to-agent association the agent-start phase built.
	// This source applies when AgentDispatch is populated by prior agent-start
	// events, binding each agent identifier to its dispatch token.
	TokenFromAgent TokenSource = "agent"
	// TokenFromSoleOutstanding: the call carried no token and no agent
	// binding existed, but exactly one dispatch was outstanding, so the
	// correlation is unambiguous without any association at all.
	//
	// This is the degradation path, and it is deliberate: an agent-start
	// event that fires late or not at all still allows correlation to succeed
	// for the run shape in which the cutoff was observed failing.
	TokenFromSoleOutstanding TokenSource = "sole_outstanding"
	// TokenUnresolved: no token could be recovered. Whether this is a defect
	// or a legitimate un-stubbed completion is decided by
	// CorrelationExpected, never by the emptiness of the token alone.
	TokenUnresolved TokenSource = "unresolved"
)

// ResolveToken returns the correlation token a post- or completion-phase call
// belongs to.
//
// Resolution order, each step tried only when the one before it yielded
// nothing:
//
//  1. The call's own token. A call that carries one is authoritative and is
//     never overridden.
//  2. State.AgentDispatch, keyed by the call's AgentID. When an agent-start
//     event bound the agent identifier to a dispatch token, this step returns
//     that token without any inference about the run shape.
//  3. The sole outstanding dispatch, when the run state holds exactly one
//     in-flight entry and that entry has no agent binding. This is the
//     degradation path for an agent-start event that fired late or not at all.
//
// A call reaching the end of that order resolves to TokenUnresolved and an
// empty token. Never a default and never a guess: step 3 applies only where
// there is exactly one candidate, so it cannot attribute a completion to the
// wrong dispatch.
func ResolveToken(state domain.RunState, call domain.InterceptedCall) (token string, source TokenSource) {
	if call.CorrelationToken != "" {
		return call.CorrelationToken, TokenFromCall
	}
	// Step 2: recover through the agent-start association. When the completion
	// event carries an AgentID and the agent-start phase already bound it to a
	// dispatch token, use that token. This is the mechanism for harnesses whose
	// completion event carries no dispatch identifier.
	if call.AgentID != "" {
		if tok, ok := state.AgentDispatch[call.AgentID]; ok {
			return tok, TokenFromAgent
		}
	}
	// Step 3: sole-outstanding fallback — when exactly one dispatch is in
	// flight and that dispatch has no pending stub (i.e. it is an unmatched-
	// passthrough dispatch that fired before an agent-start event could bind
	// it), attribute the completion to it. This is the degradation path for
	// an agent-start event that fired late or not at all. A dispatch with a
	// pending stub requires exact agent matching and is never inferred; sole-
	// outstanding applies only on the passthrough path where the echo fidelity
	// check cannot produce a false mismatch against a registered stub.
	if len(state.InFlight) == 1 {
		for tok := range state.InFlight {
			if _, hasPending := state.PendingStubs[tok]; !hasPending {
				return tok, TokenFromSoleOutstanding
			}
		}
	}
	return "", TokenUnresolved
}

// CorrelationExpected reports whether a dispatch existed that this call could
// have correlated to.
//
// True when the run state holds at least one pending stub or at least one
// in-flight entry. This is the distinction between the defect and the benign
// case: a completion event arriving when nothing is outstanding is a
// legitimate un-stubbed dispatch and raises no condition; a completion event
// arriving while a dispatch is outstanding and resolving to nothing is a
// correlation failure and must be named. The predicate keys on a dispatch
// having existed, never on the token being empty.
func CorrelationExpected(state domain.RunState) bool {
	return len(state.PendingStubs) > 0 || len(state.InFlight) > 0
}

// SeqSource names where a resolved sequence number came from.
type SeqSource string

const (
	// SeqFromPending: the correlation token resolved to a pending stub.
	SeqFromPending SeqSource = "pending"
	// SeqFromInFlight: the token resolved to an in-flight entry but no
	// pending stub — the dispatch was unmatched under a passthrough policy,
	// so no stub was ever registered for it.
	SeqFromInFlight SeqSource = "in_flight"
	// SeqFromCounter: the token resolved to nothing, so the sequence number
	// is the global invocation counter — the count of dispatches issued so
	// far. This is an inference about the run, not a fact about this call.
	SeqFromCounter SeqSource = "counter"
)

// Input is everything the decision needs. Nothing is read from ambient
// state.
type Input struct {
	Call     domain.InterceptedCall
	State    domain.RunState
	Registry domain.StubRegistry
	Groups   []domain.ParallelGroup // for tagging records with their group
	Now      time.Time              // injected; the core never reads a clock
}

// Decision is the whole answer: what to tell the harness, how the state
// must change, what to log, and what files to materialise. The core
// returns these as data and mutates nothing, so a test asserts on them
// without a filesystem.
type Decision struct {
	Outcome     domain.InterceptionOutcome
	Delta       domain.StateDelta
	Records     []domain.LogRecord
	SideEffects []domain.FileEffect
	// TerminateSubject, when true, instructs the interceptor shell to write
	// the early-exit sentinel after delivering the current reply. It is set
	// by the post/completion handler when the just-completed dispatch is the
	// Nth (where N == EarlyExitThreshold). The outcome on the same decision
	// is always OutcomePassthrough — the reply must be delivered first.
	TerminateSubject bool
}

// Decide handles all three interception phases, selected by Input.Call.Phase.
//
// Pre-invocation:
//   - a stubbed call yields OutcomeSubstitute when
//     Call.Capabilities.SupportsDirectSubstitution is true, and
//     OutcomeRewritePrompt otherwise. Nothing else influences that choice
//     — no harness name appears in this package.
//   - a call with no stub resolves through the registry's unmatched
//     policy.
//   - any call once the early-exit threshold has been reached yields
//     OutcomeHalt with HaltEarlyExit.
//
// Post-invocation routing:
//   - PhasePost with SupportsReplyRecovery=true: bare passthrough — no
//     echo comparison, no state mutation, no end record. The pending
//     dispatch and in-flight entry survive intact for the completion event.
//     On this kind of harness the post point fires at launch and carries
//     only a launch acknowledgement, so echo comparison here would produce
//     a false mismatch.
//   - PhasePost with SupportsReplyRecovery=false: full post-invocation
//     handling — echo comparison, ResolvePending, ClearInFlight, one
//     RecordEnd. The post point carries the real reply on this kind of
//     harness.
//   - PhaseCompletion (any capability): full post-invocation handling —
//     echo comparison against the collaborator's real reply, ResolvePending,
//     ClearInFlight, one RecordEnd. This is the phase that carries the real
//     reply on a harness whose post point fires at launch (see
//     domain.PhaseCompletion's doc comment).
//
// Neither post nor completion ever halts or denies: both fire after the
// collaborator has already run, so refusing either can only damage the
// subject's run.
func Decide(in Input) (Decision, error) {
	if in.Call.Phase == domain.PhaseAgentStart {
		return decideAgentStart(in), nil
	}
	if in.Call.Phase == domain.PhasePost {
		if in.Call.Capabilities.SupportsReplyRecovery {
			// Bare passthrough: the post event fires at launch on a
			// reply-recovery harness and carries only a launch
			// acknowledgement. Leave pending and in-flight intact so the
			// completion event can compare the real reply.
			return Decision{
				Outcome: domain.InterceptionOutcome{
					Kind:             domain.OutcomePassthrough,
					CorrelationToken: in.Call.CorrelationToken,
				},
			}, nil
		}
		return decidePost(in), nil
	}
	if in.Call.Phase == domain.PhaseCompletion {
		return decidePost(in), nil
	}
	return decidePre(in), nil
}

// decideAgentStart handles the PhaseAgentStart interception point. It is
// observation only: the outcome is always OutcomePassthrough and the subject
// is never halted. The state delta pops the oldest unclaimed dispatch token
// and binds the call's AgentID to that token in AgentDispatch, establishing
// the association the completion phase uses for correlation.
func decideAgentStart(in Input) Decision {
	delta := domain.StateDelta{}

	if in.Call.AgentID != "" && len(in.State.UnclaimedDispatches) > 0 {
		claimedToken := in.State.UnclaimedDispatches[0]
		delta.DequeueUnclaimed = 1
		delta.BindAgent = map[string]string{in.Call.AgentID: claimedToken}
	}

	return Decision{
		Outcome: domain.InterceptionOutcome{Kind: domain.OutcomePassthrough},
		Delta:   delta,
	}
}

func decidePre(in Input) Decision {
	id := in.Call.Identity
	seq := in.State.SequenceCounter + 1
	ordinal := in.State.CollaboratorCounters[id.Key()] + 1

	// Protocol-extraction-failure escalation ladder, final step: the
	// collaborator identity is not determinable at all. Never guess it and
	// never pass the call through — halt explicitly.
	if in.Call.Message.Extraction == domain.ExtractionFailed || id.IsZero() {
		outcome := domain.InterceptionOutcome{
			Kind:             domain.OutcomeHalt,
			HaltReason:       domain.HaltExtractionFailed,
			CorrelationToken: in.Call.CorrelationToken,
			Message:          "This operation could not be understood and cannot proceed.",
		}
		delta := domain.StateDelta{SequenceIncrement: 1}
		records := []domain.LogRecord{
			startRecord(in, seq, ordinal, outcome.Kind),
			{
				Kind:             domain.RecordError,
				TestID:           in.State.TestID,
				RunNumber:        in.State.RunNumber,
				Timestamp:        in.Now,
				Seq:              seq,
				CorrelationToken: in.Call.CorrelationToken,
				Event:            domain.RunEventExtractionFailed,
			},
		}
		return Decision{Outcome: outcome, Delta: delta, Records: records}
	}

	var extraRecords []domain.LogRecord
	switch in.Call.Message.Extraction {
	case domain.ExtractionRecovered:
		extraRecords = append(extraRecords, domain.LogRecord{
			Kind:      domain.RecordRun,
			TestID:    in.State.TestID,
			RunNumber: in.State.RunNumber,
			Timestamp: in.Now,
			Event:     domain.RunEventExtractionRecovered,
		})
	case domain.ExtractionDegraded:
		extraRecords = append(extraRecords, domain.LogRecord{
			Kind:             domain.RecordError,
			TestID:           in.State.TestID,
			RunNumber:        in.State.RunNumber,
			Timestamp:        in.Now,
			Seq:              seq,
			Identity:         id,
			CorrelationToken: in.Call.CorrelationToken,
		})
	}

	delta := domain.StateDelta{
		SequenceIncrement:      1,
		CollaboratorIncrements: map[string]int{id.Key(): 1},
	}

	var outcome domain.InterceptionOutcome
	var sideEffects []domain.FileEffect
	token := in.Call.CorrelationToken

	result := stubmatch.Match(in.Registry, id, ordinal)

	switch {
	case result.Matched:
		outcome = substitutionOutcome(in.Call.Capabilities, token, result.Stub.Response)
		sideEffects = result.Stub.SideEffects
		delta.MarkInFlight = map[string]domain.InFlight{
			token: {Seq: seq, Identity: id, StartedAt: in.Now},
		}
		delta.AddPending = map[string]domain.PendingStub{
			token: {Seq: seq, Identity: id, Expected: result.Stub.Response},
		}

	case result.Policy == domain.UnmatchedGenericResponse:
		outcome = substitutionOutcome(in.Call.Capabilities, token, result.GenericResponse)
		delta.MarkInFlight = map[string]domain.InFlight{
			token: {Seq: seq, Identity: id, StartedAt: in.Now},
		}
		delta.AddPending = map[string]domain.PendingStub{
			token: {Seq: seq, Identity: id, Expected: result.GenericResponse},
		}

	case result.Policy == domain.UnmatchedPassthrough:
		outcome = domain.InterceptionOutcome{
			Kind:             domain.OutcomePassthrough,
			CorrelationToken: token,
		}
		delta.MarkInFlight = map[string]domain.InFlight{
			token: {Seq: seq, Identity: id, StartedAt: in.Now},
		}

	default: // domain.UnmatchedHalt
		outcome = domain.InterceptionOutcome{
			Kind:             domain.OutcomeHalt,
			HaltReason:       domain.HaltUnmatched,
			CorrelationToken: token,
			Message:          "This operation was not expected in this run.",
		}
		extraRecords = append(extraRecords, domain.LogRecord{
			Kind:      domain.RecordRun,
			TestID:    in.State.TestID,
			RunNumber: in.State.RunNumber,
			Timestamp: in.Now,
			Event:     domain.RunEventUnmatchedInvocation,
		})
	}

	// Enqueue the correlation token for the agent-start phase to claim when
	// the dispatch results in an agent that will actually run. Terminating
	// outcomes (substitute, halt) are excluded: on those paths the response is
	// delivered at this point and the agent never runs, so no agent-start or
	// completion event will fire. Only rewrite-prompt and passthrough outcomes
	// proceed to agent-start and completion.
	if len(delta.MarkInFlight) > 0 && !TerminatesAtPre(outcome.Kind) {
		delta.EnqueueUnclaimed = []string{token}
	}

	records := append([]domain.LogRecord{startRecord(in, seq, ordinal, outcome.Kind)}, extraRecords...)

	d := Decision{
		Outcome:     outcome,
		Delta:       delta,
		Records:     records,
		SideEffects: sideEffects,
	}

	// Cutoff detection for outcomes that terminate at the pre-invocation point:
	// when TerminatesAtPre is true (OutcomeSubstitute or OutcomeHalt), the reply
	// IS delivered at this point and no post/completion event fires for it.
	// Detect the cutoff here so the interceptor shell writes the sentinel after
	// the reply leaves cfg.Out.
	//
	// TerminatesAtPre(OutcomeSubstitute): the harness supports direct substitution
	// — the substitute reply is the Nth reply delivered to the subject.
	//
	// TerminatesAtPre(OutcomeHalt): the call was refused — the refusal IS the Nth
	// answer and no post/completion fires for it. Under an unmatched-halt policy,
	// this is how the cutoff fires when the Nth dispatch matched no registered stub.
	//
	// Equality (==) rather than >= is used here: for dispatches beyond the threshold,
	// the state should have EarlyExitTriggered=true from the Nth dispatch, and the
	// !EarlyExitTriggered guard handles that case. A strict == here ensures the
	// pre-invocation path does not write the sentinel for any dispatch beyond the
	// Nth when state was not yet updated (e.g. asynchronous completion lag).
	if in.State.EarlyExitThreshold > 0 && !in.State.EarlyExitTriggered &&
		seq == in.State.EarlyExitThreshold && TerminatesAtPre(outcome.Kind) {
		d.TerminateSubject = true
		d.Delta.SetEarlyExitTriggered = true
		d.Records = append(d.Records, domain.LogRecord{
			Kind:      domain.RecordRun,
			TestID:    in.State.TestID,
			RunNumber: in.State.RunNumber,
			Timestamp: in.Now,
			Event:     domain.RunEventEarlyExitTriggered,
		})
	}

	return d
}

// substitutionOutcome applies the capability-driven outcome selection: a
// harness that supports direct substitution gets the payload back directly,
// otherwise its input is rewritten into an echo instruction. Nothing else
// influences this choice — no harness name appears in this package.
func substitutionOutcome(caps domain.HarnessCapabilities, token string, response json.RawMessage) domain.InterceptionOutcome {
	if caps.SupportsDirectSubstitution {
		return domain.InterceptionOutcome{
			Kind:             domain.OutcomeSubstitute,
			StubResponse:     response,
			CorrelationToken: token,
		}
	}
	return domain.InterceptionOutcome{
		Kind:             domain.OutcomeRewritePrompt,
		RewrittenPrompt:  EchoInstruction(response),
		CorrelationToken: token,
	}
}

// startRecord describes one intercepted call at the pre-invocation point.
func startRecord(in Input, seq, ordinal int, kind domain.OutcomeKind) domain.LogRecord {
	msg := in.Call.Message
	return domain.LogRecord{
		Kind:             domain.RecordStart,
		TestID:           in.State.TestID,
		RunNumber:        in.State.RunNumber,
		Timestamp:        in.Now,
		Seq:              seq,
		Ordinal:          ordinal,
		Identity:         in.Call.Identity,
		Group:            groupFor(in.Call.Identity, in.Groups),
		CorrelationToken: in.Call.CorrelationToken,
		Outcome:          kind,
		Message:          &msg,
	}
}

// groupFor reports the declared parallel group id belongs to, or "" when
// ungrouped.
func groupFor(id domain.CollaboratorIdentity, groups []domain.ParallelGroup) string {
	for _, g := range groups {
		for _, member := range g.Members {
			if member == id {
				return g.Name
			}
		}
	}
	return ""
}

// decidePost handles the post-invocation interception point: it recovers
// the pending stub by correlation token, compares echo fidelity, produces
// an end record, and clears the in-flight entry. It always resolves to
// OutcomePassthrough — the collaborator has already run by this point, so
// refusing it can only damage the subject's run.
func decidePost(in Input) Decision {
	// Resolve the correlation token. For PhaseCompletion, the call may carry
	// no token directly; ResolveToken checks the call's own token first, then
	// the agent-dispatch binding (when an agent-start event established one),
	// then the sole-outstanding fallback path. TokenUnresolved means none of
	// those succeeded.
	token, tokenSource := ResolveToken(in.State, in.Call)
	pending, hasPending := in.State.PendingStubs[token]

	// Resolve the sequence number for this dispatch. The resolution order is:
	// (1) pending stub — present for matched and generic-response dispatches;
	// (2) in-flight entry — present for unmatched-passthrough dispatches, which
	//     create an in-flight entry but no pending stub;
	// (3) global sequence counter — fallback when the token cannot be correlated
	//     to any dispatch at all.
	// This makes the cutoff count-based on every path, including paths where the
	// Nth dispatch matched no registered stub.
	resolvedSeq, resolvedSource := ResolveSeq(in.State, token)

	var echo *domain.EchoOutcome
	if hasPending {
		compared := CompareEcho(pending.Expected, in.Call.ObservedResponse)
		echo = &compared
	}

	delta := domain.StateDelta{
		ClearInFlight: []string{token},
	}
	if hasPending {
		delta.ResolvePending = []string{token}
	}
	// Release the agent binding when completion was correlated through it, so
	// the agent identifier does not match a later dispatch erroneously.
	if tokenSource == TokenFromAgent && in.Call.AgentID != "" {
		delta.ReleaseAgents = []string{in.Call.AgentID}
	}

	// Identity for the end record: prefer the call's own identity; fall
	// back to the correlated pending dispatch's identity when the call's
	// identity is zero and a pending dispatch was found. Never a default or
	// a guess — the fallback reaches only the dispatch the correlation token
	// resolved to.
	identity := in.Call.Identity
	if identity == (domain.CollaboratorIdentity{}) && hasPending {
		identity = pending.Identity
	}

	rec := domain.LogRecord{
		Kind:             domain.RecordEnd,
		TestID:           in.State.TestID,
		RunNumber:        in.State.RunNumber,
		Timestamp:        in.Now,
		Identity:         identity,
		CorrelationToken: token,
		Echo:             echo,
	}
	// Set the Seq field when we resolved a concrete dispatch (pending or in-flight).
	// When the token was unresolved (SeqFromCounter), the Seq remains zero.
	if resolvedSource == SeqFromPending || resolvedSource == SeqFromInFlight {
		rec.Seq = resolvedSeq
	}

	outcome := domain.InterceptionOutcome{
		Kind:             domain.OutcomePassthrough,
		CorrelationToken: token,
	}

	d := Decision{
		Outcome: outcome,
		Delta:   delta,
		Records: []domain.LogRecord{rec},
	}

	// Emit RunEventUncorrelatedCompletion when correlation did not succeed
	// through an established agent binding (TokenFromCall or TokenFromAgent)
	// and a dispatch was outstanding. This covers both the complete failure
	// case (TokenUnresolved) and the degraded sole-outstanding case
	// (TokenFromSoleOutstanding), which succeeds by proximity rather than by
	// an actual agent binding established at the agent-start phase. Both are
	// named here so the operator can see that correlation was not clean.
	//
	// TokenFromCall and TokenFromAgent never produce this event: a direct
	// token or an established binding are authoritative correlations.
	if (tokenSource == TokenUnresolved || tokenSource == TokenFromSoleOutstanding) && CorrelationExpected(in.State) {
		outstanding := len(in.State.InFlight)
		d.Records = append(d.Records, domain.LogRecord{
			Kind:      domain.RecordRun,
			TestID:    in.State.TestID,
			RunNumber: in.State.RunNumber,
			Timestamp: in.Now,
			Event:     domain.RunEventUncorrelatedCompletion,
			Detail:    fmt.Sprintf("%d dispatch(es) outstanding when completion arrived with no resolvable token", outstanding),
		})
	}

	// Cutoff detection: the cutoff fires when the sequence number resolved for
	// this dispatch is at or beyond the threshold, and the threshold has not
	// already been triggered. The outcome remains OutcomePassthrough — the reply
	// is delivered first, and the interceptor shell writes the sentinel only after
	// the reply leaves cfg.Out. The >= (not ==) comparison ensures a missed Nth
	// observation (e.g. due to a concurrency gap) is still caught by the next
	// post or completion event.
	//
	// The !EarlyExitTriggered guard ensures the RunEventEarlyExitTriggered record
	// appears exactly once in the invocation log, even if multiple post events
	// arrive with seq >= threshold before the updated state is read.
	//
	// No conjunct short-circuits before the threshold comparison: the cutoff is
	// count-based and fires regardless of whether the dispatch was stubbed,
	// answered with a generic response, or unmatched.
	if in.State.EarlyExitThreshold > 0 && !in.State.EarlyExitTriggered && resolvedSeq >= in.State.EarlyExitThreshold {
		d.TerminateSubject = true
		d.Delta.SetEarlyExitTriggered = true
		d.Records = append(d.Records, domain.LogRecord{
			Kind:      domain.RecordRun,
			TestID:    in.State.TestID,
			RunNumber: in.State.RunNumber,
			Timestamp: in.Now,
			Event:     domain.RunEventEarlyExitTriggered,
		})
	}

	return d
}

// ResolveSeq reports the invocation sequence number the dispatch behind a
// post- or completion-phase call carries, and where that number came from.
//
// The resolution order is pending stub, then in-flight entry, then the global
// sequence counter. The fallback to the counter is what makes the cutoff
// count-based: a dispatch that matched no registered stub creates no pending
// entry, and a completion event that cannot be correlated resolves to no
// entry at all, yet the tool still knows how many dispatches have been
// issued. The count is the only thing the cutoff may depend on.
//
// A zero return is possible only for a state whose SequenceCounter is zero,
// i.e. before any dispatch — a caller must treat 0 as "no dispatch".
func ResolveSeq(state domain.RunState, token string) (seq int, source SeqSource) {
	if pending, ok := state.PendingStubs[token]; ok {
		return pending.Seq, SeqFromPending
	}
	if inFlight, ok := state.InFlight[token]; ok {
		return inFlight.Seq, SeqFromInFlight
	}
	return state.SequenceCounter, SeqFromCounter
}

// TerminatesAtPre reports whether an outcome answers the dispatch at the
// pre-invocation point, so no later phase will observe it.
//
// True for OutcomeSubstitute (the tool supplied the reply itself) and
// OutcomeHalt (the call was refused and will never run). False for
// OutcomeRewritePrompt and OutcomePassthrough, on which the collaborator
// still runs and the reply is observed at the post or completion point —
// termination is deferred to whichever of those observes it, because the
// reply must reach the subject before the subject is stopped.
//
// This generalises the previous pre-invocation cutoff condition, which tested
// OutcomeSubstitute alone and therefore never fired on a harness that can
// only rewrite a prompt. The substitution path's behaviour is unchanged.
func TerminatesAtPre(kind domain.OutcomeKind) bool {
	return kind == domain.OutcomeSubstitute || kind == domain.OutcomeHalt
}

// EchoInstruction renders the prompt that replaces a stubbed call's input
// on the rewrite path.
//
// Two properties are contractual:
//   - it carries the stub response verbatim, so a faithful echo compares
//     equal under CompareEcho;
//   - it contains no test-revealing vocabulary of any kind. The subject
//     must not be able to tell it is being exercised, and that obligation
//     reaches prompts, stub responses and seeded files alike.
func EchoInstruction(stubResponse json.RawMessage) string {
	return fmt.Sprintf(
		"Respond with exactly the following JSON object as your entire reply, unchanged and with no additional commentary:\n\n%s",
		string(stubResponse),
	)
}

// CompareEcho decides echo fidelity: the observed response is normalized
// (JSON object parsed, key order and insignificant whitespace ignored) and
// compared against the expected stub. A narrow fence tolerance is applied
// before parsing: if the observed text, after trimming surrounding
// whitespace, is enclosed in a single ```json or bare ``` fence with no
// additional prose, the fence markers are stripped before parsing.
// Surrounding prose, partial fences, wrong info strings, and fenced
// non-JSON content remain mismatches.
func CompareEcho(expected json.RawMessage, observed string) domain.EchoOutcome {
	var expectedVal interface{}
	_ = json.Unmarshal(expected, &expectedVal)

	trimmed := strings.TrimSpace(observed)

	// Bounded fence stripping, applied at most once. The conditions are:
	// at least two lines, first line is exactly "```json" or "```" (after
	// trimming its own trailing whitespace), last line is exactly "```"
	// (after trimming its own trailing whitespace). Only these two info
	// strings are tolerated; the check is case-sensitive.
	if stripped, ok := stripCodeFence(trimmed); ok {
		trimmed = strings.TrimSpace(stripped)
	}

	var observedVal interface{}
	if err := json.Unmarshal([]byte(trimmed), &observedVal); err != nil {
		return domain.EchoOutcome{
			Match:    false,
			Expected: expected,
			Observed: observed,
			Diff:     "observed text is not a bare JSON value: " + err.Error(),
		}
	}

	if reflect.DeepEqual(expectedVal, observedVal) {
		return domain.EchoOutcome{
			Match:    true,
			Expected: expected,
			Observed: observed,
		}
	}
	return domain.EchoOutcome{
		Match:    false,
		Expected: expected,
		Observed: observed,
		Diff:     "observed value differs from the expected stub response",
	}
}

// stripCodeFence strips the opening and closing markers of a ```json or ```
// code fence from s, returning the body and true when exactly one such fence
// wraps the entire string with no surrounding prose. Returns ("", false)
// when the conditions are not met.
func stripCodeFence(s string) (string, bool) {
	lines := strings.Split(s, "\n")
	if len(lines) < 2 {
		return "", false
	}
	first := strings.TrimRight(lines[0], " \t\r")
	last := strings.TrimRight(lines[len(lines)-1], " \t\r")
	if last != "```" {
		return "", false
	}
	if first != "```json" && first != "```" {
		return "", false
	}
	return strings.Join(lines[1:len(lines)-1], "\n"), true
}
