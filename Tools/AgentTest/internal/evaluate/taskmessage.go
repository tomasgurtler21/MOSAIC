package evaluate

import (
	"fmt"
	"strconv"
	"strings"

	"mosaic-agent-test/internal/domain"
)

// evaluateTaskMessage evaluates one TaskMessageAssertion against the
// invocation at its declared global sequence number. Each declared sub-field
// produces its own separate AssertionResult, with Target "<seq>.<subfield>".
//
// Exception: when no start record exists for the declared sequence number, a
// single result is returned with the bare sequence number as Target (no
// per-field split is possible without an invocation to inspect).
func evaluateTaskMessage(records []domain.LogRecord, want domain.TaskMessageAssertion) []domain.AssertionResult {
	seqStr := strconv.Itoa(want.At)

	rec := findStartRecord(records, want.At)
	if rec == nil {
		return []domain.AssertionResult{{
			Class:    domain.ClassTaskMessage,
			Target:   seqStr,
			Outcome:  domain.AssertionFail,
			Expected: fmt.Sprintf("invocation at sequence %d", want.At),
			Actual:   fmt.Sprintf("no invocation observed at sequence %d", want.At),
			Detail:   fmt.Sprintf("no invocation with sequence %d was observed", want.At),
		}}
	}

	// When extraction is degraded or failed, the fields in the message cannot
	// be trusted. Produce one failing result per declared sub-field so the
	// caller can see which checks failed and why.
	if rec.Message != nil && (rec.Message.Extraction == domain.ExtractionDegraded || rec.Message.Extraction == domain.ExtractionFailed) {
		extractionStatus := string(rec.Message.Extraction)
		return degradedResults(want, seqStr, extractionStatus, rec.Identity)
	}

	var msg domain.TaskMessage
	if rec.Message != nil {
		msg = *rec.Message
	}

	var out []domain.AssertionResult

	// Identity check: always evaluable from the start record, regardless of
	// message extraction quality, since it comes from the record itself.
	if want.Identity != nil {
		ar := domain.AssertionResult{
			Class:    domain.ClassTaskMessage,
			Target:   seqStr + ".identity",
			Expected: want.Identity.Key(),
			Actual:   rec.Identity.Key(),
		}
		if rec.Identity == *want.Identity {
			ar.Outcome = domain.AssertionPass
		} else {
			ar.Outcome = domain.AssertionFail
			ar.Detail = fmt.Sprintf("invocation %d is %s, declared identity is %s — a sequence drift, not an ordinary message mismatch", want.At, rec.Identity.Key(), want.Identity.Key())
		}
		out = append(out, ar)
	}

	// Input artifacts check.
	if want.RequiredInputArtifacts != nil || want.OptionalInputArtifacts != nil {
		ar := domain.AssertionResult{
			Class:    domain.ClassTaskMessage,
			Target:   seqStr + ".input_artifacts",
			Expected: artifactSetDescription(want.RequiredInputArtifacts, want.OptionalInputArtifacts),
			Actual:   artifactListDescription(msg.InputArtifacts),
		}
		if _, ok := evaluateArtifactSet(msg.InputArtifacts, want.RequiredInputArtifacts, want.OptionalInputArtifacts, "input"); ok {
			ar.Outcome = domain.AssertionPass
		} else {
			detail, _ := evaluateArtifactSet(msg.InputArtifacts, want.RequiredInputArtifacts, want.OptionalInputArtifacts, "input")
			ar.Outcome = domain.AssertionFail
			ar.Detail = detail
		}
		out = append(out, ar)
	}

	// Output artifacts check.
	if want.RequiredOutputArtifacts != nil || want.OptionalOutputArtifacts != nil {
		ar := domain.AssertionResult{
			Class:    domain.ClassTaskMessage,
			Target:   seqStr + ".output_artifacts",
			Expected: artifactSetDescription(want.RequiredOutputArtifacts, want.OptionalOutputArtifacts),
			Actual:   artifactListDescription(msg.OutputArtifacts),
		}
		if _, ok := evaluateArtifactSet(msg.OutputArtifacts, want.RequiredOutputArtifacts, want.OptionalOutputArtifacts, "output"); ok {
			ar.Outcome = domain.AssertionPass
		} else {
			detail, _ := evaluateArtifactSet(msg.OutputArtifacts, want.RequiredOutputArtifacts, want.OptionalOutputArtifacts, "output")
			ar.Outcome = domain.AssertionFail
			ar.Detail = detail
		}
		out = append(out, ar)
	}

	// Human-in-the-loop check.
	if want.HumanInTheLoop != nil {
		ar := domain.AssertionResult{
			Class:    domain.ClassTaskMessage,
			Target:   seqStr + ".human_in_the_loop",
			Expected: strconv.FormatBool(*want.HumanInTheLoop),
			Actual:   strconv.FormatBool(msg.HumanInTheLoop),
		}
		if msg.HumanInTheLoop == *want.HumanInTheLoop {
			ar.Outcome = domain.AssertionPass
		} else {
			ar.Outcome = domain.AssertionFail
			ar.Detail = fmt.Sprintf("human_in_the_loop observed %v, declared %v", msg.HumanInTheLoop, *want.HumanInTheLoop)
		}
		out = append(out, ar)
	}

	// Task description substring check.
	if want.TaskDescriptionContains != nil {
		ar := domain.AssertionResult{
			Class:    domain.ClassTaskMessage,
			Target:   seqStr + ".task_description",
			Expected: fmt.Sprintf("contains %v", want.TaskDescriptionContains),
			Actual:   msg.TaskDescription,
		}
		var missingSubstrings []string
		for _, substr := range want.TaskDescriptionContains {
			if !strings.Contains(msg.TaskDescription, substr) {
				missingSubstrings = append(missingSubstrings, substr)
			}
		}
		if len(missingSubstrings) == 0 {
			ar.Outcome = domain.AssertionPass
		} else {
			ar.Outcome = domain.AssertionFail
			ar.Detail = fmt.Sprintf("task_description does not contain declared substring(s): %v", missingSubstrings)
		}
		out = append(out, ar)
	}

	return out
}

// degradedResults produces one failing AssertionResult per declared sub-field
// when extraction quality prevents trusting the message. Evidence in each
// result describes what was expected vs. the extraction status observed.
func degradedResults(want domain.TaskMessageAssertion, seqStr, extractionStatus string, recordIdentity domain.CollaboratorIdentity) []domain.AssertionResult {
	var out []domain.AssertionResult
	actualMsg := fmt.Sprintf("message extraction was %q; fields cannot be trusted", extractionStatus)

	if want.Identity != nil {
		out = append(out, domain.AssertionResult{
			Class:    domain.ClassTaskMessage,
			Target:   seqStr + ".identity",
			Outcome:  domain.AssertionFail,
			Expected: want.Identity.Key(),
			Actual:   actualMsg,
			Detail:   fmt.Sprintf("invocation %s's message extraction was %q; task-message assertions never pass against it", seqStr, extractionStatus),
		})
	}

	if want.RequiredInputArtifacts != nil || want.OptionalInputArtifacts != nil {
		out = append(out, domain.AssertionResult{
			Class:    domain.ClassTaskMessage,
			Target:   seqStr + ".input_artifacts",
			Outcome:  domain.AssertionFail,
			Expected: artifactSetDescription(want.RequiredInputArtifacts, want.OptionalInputArtifacts),
			Actual:   actualMsg,
			Detail:   fmt.Sprintf("invocation %s's message extraction was %q; task-message assertions never pass against it", seqStr, extractionStatus),
		})
	}

	if want.RequiredOutputArtifacts != nil || want.OptionalOutputArtifacts != nil {
		out = append(out, domain.AssertionResult{
			Class:    domain.ClassTaskMessage,
			Target:   seqStr + ".output_artifacts",
			Outcome:  domain.AssertionFail,
			Expected: artifactSetDescription(want.RequiredOutputArtifacts, want.OptionalOutputArtifacts),
			Actual:   actualMsg,
			Detail:   fmt.Sprintf("invocation %s's message extraction was %q; task-message assertions never pass against it", seqStr, extractionStatus),
		})
	}

	if want.HumanInTheLoop != nil {
		out = append(out, domain.AssertionResult{
			Class:    domain.ClassTaskMessage,
			Target:   seqStr + ".human_in_the_loop",
			Outcome:  domain.AssertionFail,
			Expected: strconv.FormatBool(*want.HumanInTheLoop),
			Actual:   actualMsg,
			Detail:   fmt.Sprintf("invocation %s's message extraction was %q; task-message assertions never pass against it", seqStr, extractionStatus),
		})
	}

	if want.TaskDescriptionContains != nil {
		out = append(out, domain.AssertionResult{
			Class:    domain.ClassTaskMessage,
			Target:   seqStr + ".task_description",
			Outcome:  domain.AssertionFail,
			Expected: fmt.Sprintf("contains %v", want.TaskDescriptionContains),
			Actual:   actualMsg,
			Detail:   fmt.Sprintf("invocation %s's message extraction was %q; task-message assertions never pass against it", seqStr, extractionStatus),
		})
	}

	return out
}

// artifactSetDescription returns a human-readable description of a required +
// optional artifact set for use as the Expected field in an AssertionResult.
func artifactSetDescription(required, optional []string) string {
	var parts []string
	if len(required) > 0 {
		parts = append(parts, fmt.Sprintf("required:%v", required))
	}
	if len(optional) > 0 {
		parts = append(parts, fmt.Sprintf("optional:%v", optional))
	}
	if len(parts) == 0 {
		return "[]"
	}
	return strings.Join(parts, " ")
}

// artifactListDescription returns a human-readable description of an observed
// artifact list for use as the Actual field in an AssertionResult.
func artifactListDescription(artifacts []string) string {
	if len(artifacts) == 0 {
		return "[]"
	}
	return fmt.Sprintf("%v", artifacts)
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
