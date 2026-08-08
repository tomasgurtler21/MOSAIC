package evaluate

import (
	"fmt"
	"strconv"
	"strings"

	"mosaic-agent-test/internal/domain"
)

// evaluateTaskMessage evaluates one TaskMessageAssertion against the
// invocation at its declared global sequence number.
func evaluateTaskMessage(records []domain.LogRecord, want domain.TaskMessageAssertion) domain.AssertionResult {
	target := strconv.Itoa(want.At)
	ar := domain.AssertionResult{Class: domain.ClassTaskMessage, Target: target}

	rec := findStartRecord(records, want.At)
	if rec == nil {
		ar.Outcome = domain.AssertionFail
		ar.Detail = fmt.Sprintf("no invocation with sequence %d was observed", want.At)
		return ar
	}

	if want.Identity != nil && rec.Identity != *want.Identity {
		ar.Outcome = domain.AssertionFail
		ar.Expected = want.Identity.Key()
		ar.Actual = rec.Identity.Key()
		ar.Detail = fmt.Sprintf("invocation %d is %s, declared identity is %s — a sequence drift, not an ordinary message mismatch", want.At, rec.Identity.Key(), want.Identity.Key())
		return ar
	}

	var msg domain.TaskMessage
	if rec.Message != nil {
		msg = *rec.Message
	}

	// A degraded or failed extraction never carries fields worth trusting:
	// a task-message assertion against it must fail, never accidentally
	// pass against an effectively-empty message.
	if rec.Message != nil && (rec.Message.Extraction == domain.ExtractionDegraded || rec.Message.Extraction == domain.ExtractionFailed) {
		ar.Outcome = domain.AssertionFail
		ar.Detail = fmt.Sprintf("invocation %d's message extraction was %q; task-message assertions never pass against it", want.At, rec.Message.Extraction)
		return ar
	}

	var failures []string

	if want.RequiredInputArtifacts != nil || want.OptionalInputArtifacts != nil {
		if detail, ok := evaluateArtifactSet(msg.InputArtifacts, want.RequiredInputArtifacts, want.OptionalInputArtifacts, "input"); !ok {
			failures = append(failures, detail)
		}
	}

	if want.RequiredOutputArtifacts != nil || want.OptionalOutputArtifacts != nil {
		if detail, ok := evaluateArtifactSet(msg.OutputArtifacts, want.RequiredOutputArtifacts, want.OptionalOutputArtifacts, "output"); !ok {
			failures = append(failures, detail)
		}
	}

	if want.HumanInTheLoop != nil && msg.HumanInTheLoop != *want.HumanInTheLoop {
		failures = append(failures, fmt.Sprintf("human_in_the_loop observed %v, declared %v", msg.HumanInTheLoop, *want.HumanInTheLoop))
	}

	for _, substr := range want.TaskDescriptionContains {
		if !strings.Contains(msg.TaskDescription, substr) {
			failures = append(failures, fmt.Sprintf("task_description %q does not contain declared substring %q", msg.TaskDescription, substr))
		}
	}

	if len(failures) > 0 {
		ar.Outcome = domain.AssertionFail
		ar.Detail = strings.Join(failures, "; ")
		return ar
	}

	ar.Outcome = domain.AssertionPass
	return ar
}

// evaluateArtifactSet checks the required-versus-optional set semantics: a
// required entry must all be present, an optional entry may be, and
// anything observed that is in neither set fails the assertion.
func evaluateArtifactSet(observed, required, optional []string, direction string) (detail string, ok bool) {
	var missing []string
	for _, r := range required {
		if !contains(observed, r) {
			missing = append(missing, r)
		}
	}

	var extra []string
	for _, o := range observed {
		if !contains(required, o) && !contains(optional, o) {
			extra = append(extra, o)
		}
	}

	if len(missing) == 0 && len(extra) == 0 {
		return "", true
	}

	var parts []string
	if len(missing) > 0 {
		parts = append(parts, fmt.Sprintf("required %s artifact(s) missing: %s", direction, strings.Join(missing, ", ")))
	}
	if len(extra) > 0 {
		parts = append(parts, fmt.Sprintf("%s artifact(s) in neither the required nor the optional set: %s", direction, strings.Join(extra, ", ")))
	}
	return strings.Join(parts, "; "), false
}

func findStartRecord(records []domain.LogRecord, seq int) *domain.LogRecord {
	for i := range records {
		if records[i].Kind == domain.RecordStart && records[i].Seq == seq {
			return &records[i]
		}
	}
	return nil
}
