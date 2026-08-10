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
// Post-invocation and completion:
//   - the pending stub is recovered by correlation token, echo fidelity is
//     compared, an end record is produced, and the outcome is always
//     OutcomePassthrough. Neither phase ever halts or denies: both fire
//     after the collaborator has already run, so refusing either can only
//     damage the subject's run. PhaseCompletion is handled identically to
//     PhasePost: it is the event that carries a real reply on a harness
//     whose post-invocation point fires at launch (see
//     domain.PhaseCompletion's doc comment).
func Decide(in Input) (Decision, error) {
	if in.Call.Phase == domain.PhasePost || in.Call.Phase == domain.PhaseCompletion {
		return decidePost(in), nil
	}
	return decidePre(in), nil
}

func decidePre(in Input) Decision {
	id := in.Call.Identity
	seq := in.State.SequenceCounter + 1
	ordinal := in.State.CollaboratorCounters[id.Key()] + 1

	// Early-exit threshold takes priority over everything else: any call
	// halts once the threshold has been reached.
	if in.State.EarlyExitThreshold > 0 && in.State.SequenceCounter >= in.State.EarlyExitThreshold {
		outcome := domain.InterceptionOutcome{
			Kind:             domain.OutcomeHalt,
			HaltReason:       domain.HaltEarlyExit,
			CorrelationToken: in.Call.CorrelationToken,
			Message:          "This operation cannot proceed further in this run.",
		}
		delta := domain.StateDelta{
			SequenceIncrement:      1,
			CollaboratorIncrements: map[string]int{id.Key(): 1},
			SetEarlyExitTriggered:  true,
		}
		records := []domain.LogRecord{
			startRecord(in, seq, ordinal, outcome.Kind),
			{
				Kind:      domain.RecordRun,
				TestID:    in.State.TestID,
				RunNumber: in.State.RunNumber,
				Timestamp: in.Now,
				Event:     domain.RunEventEarlyExitTriggered,
			},
		}
		return Decision{Outcome: outcome, Delta: delta, Records: records}
	}

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

	records := append([]domain.LogRecord{startRecord(in, seq, ordinal, outcome.Kind)}, extraRecords...)

	return Decision{
		Outcome:     outcome,
		Delta:       delta,
		Records:     records,
		SideEffects: sideEffects,
	}
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
	token := in.Call.CorrelationToken
	pending, hasPending := in.State.PendingStubs[token]

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

	rec := domain.LogRecord{
		Kind:             domain.RecordEnd,
		TestID:           in.State.TestID,
		RunNumber:        in.State.RunNumber,
		Timestamp:        in.Now,
		Identity:         in.Call.Identity,
		CorrelationToken: token,
		Echo:             echo,
	}
	if hasPending {
		rec.Seq = pending.Seq
	}

	outcome := domain.InterceptionOutcome{
		Kind:             domain.OutcomePassthrough,
		CorrelationToken: token,
	}

	return Decision{
		Outcome: outcome,
		Delta:   delta,
		Records: []domain.LogRecord{rec},
	}
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
// compared against the expected stub. Surrounding prose in the observed
// text is a mismatch, not a tolerated wrapper.
func CompareEcho(expected json.RawMessage, observed string) domain.EchoOutcome {
	var expectedVal interface{}
	_ = json.Unmarshal(expected, &expectedVal)

	trimmed := strings.TrimSpace(observed)
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
