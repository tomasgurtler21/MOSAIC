package evaluate

import (
	"fmt"
	"strconv"

	"mosaic-agent-test/internal/domain"
)

// evaluateAssertions evaluates every assertion class declared by ev's
// definition, plus echo fidelity — which is never declared, never
// suppressed, and always evaluated for every stubbed invocation. Echo
// fidelity results always appear in the returned slice regardless of the
// effective echo fidelity mode; whether a mismatch contributes to the
// verdict is decided in Evaluate based on the mode.
func evaluateAssertions(ev domain.RunEvidence) []domain.AssertionResult {
	var out []domain.AssertionResult
	a := ev.Definition.Assertions
	layer := ev.Definition.Layer

	if a.InvocationSequence != nil {
		out = append(out, evaluateSequence(observedSequence(ev.Records), *a.InvocationSequence))
	}

	if a.ExecutionLogAgentIDs != nil {
		out = append(out, evaluateExecutionLogAgentIDs(ev.Orchestration.ExecutionLog, *a.ExecutionLogAgentIDs))
	}

	if a.ExecutionLogAllStatus != nil {
		out = append(out, evaluateExecutionLogAllStatus(ev.Orchestration.ExecutionLog, *a.ExecutionLogAllStatus))
	}

	if a.FinalPhase != nil {
		out = append(out, evaluateEquals(domain.ClassFinalPhase, ev.Orchestration.Phase, *a.FinalPhase))
	}

	if a.FinalStatus != nil {
		out = append(out, evaluateEquals(domain.ClassFinalStatus, ev.Orchestration.LastStatus, *a.FinalStatus))
	}

	if a.ProtocolViolations != nil {
		out = append(out, evaluateProtocolViolations(ev, layer, *a.ProtocolViolations))
	}

	for _, name := range a.ArtifactCreated {
		out = append(out, evaluateArtifactCreated(name, ev.SnapshotFiles))
	}

	for _, name := range a.ArtifactNotCreated {
		out = append(out, evaluateArtifactNotCreated(name, ev.SnapshotFiles))
	}

	for _, group := range sortedKeys(a.MinConcurrency) {
		out = append(out, evaluateMinConcurrency(group, a.MinConcurrency[group], ev.PeakConcurrency[group]))
	}

	for _, tma := range a.TaskMessages {
		out = append(out, evaluateTaskMessage(ev.Records, tma)...)
	}

	out = append(out, evaluateEchoFidelity(ev.Records)...)

	return out
}

// assertionsDeclared reports whether a is not its zero value — i.e. whether
// the test definition declared at least one assertion class, however that
// declaration was spelled. A pointer field's nilness and a slice or map
// field's nilness both distinguish "not declared" from "declared", per
// domain.Assertions' own documented distinction between nil ("not
// evaluated") and an empty non-nil value ("assert the empty set").
func assertionsDeclared(a domain.Assertions) bool {
	return a.InvocationSequence != nil ||
		a.ExecutionLogAgentIDs != nil ||
		a.ExecutionLogAllStatus != nil ||
		a.FinalPhase != nil ||
		a.FinalStatus != nil ||
		a.ProtocolViolations != nil ||
		a.ArtifactCreated != nil ||
		a.ArtifactNotCreated != nil ||
		a.MinConcurrency != nil ||
		a.TaskMessages != nil
}

func evaluateEquals(class domain.AssertionClass, observed, want string) domain.AssertionResult {
	ar := domain.AssertionResult{Class: class, Expected: want, Actual: observed}
	if observed == want {
		ar.Outcome = domain.AssertionPass
	} else {
		ar.Outcome = domain.AssertionFail
		ar.Detail = fmt.Sprintf("observed %q, declared %q", observed, want)
	}
	return ar
}

func evaluateExecutionLogAgentIDs(rows []domain.ExecutionLogRow, want []string) domain.AssertionResult {
	observed := make([]string, len(rows))
	for i, row := range rows {
		observed[i] = row.AgentInstance
	}
	ar := domain.AssertionResult{
		Class:    domain.ClassExecutionLogAgentIDs,
		Expected: fmt.Sprintf("%v", want),
		Actual:   fmt.Sprintf("%v", observed),
	}
	if stringSliceEqual(observed, want) {
		ar.Outcome = domain.AssertionPass
	} else {
		ar.Outcome = domain.AssertionFail
		ar.Detail = "the observed execution-log agent-ID order does not match the declared order"
	}
	return ar
}

func evaluateExecutionLogAllStatus(rows []domain.ExecutionLogRow, want string) domain.AssertionResult {
	ar := domain.AssertionResult{Class: domain.ClassExecutionLogAllStatus, Expected: want}
	for _, row := range rows {
		if row.Status != want {
			ar.Outcome = domain.AssertionFail
			ar.Actual = row.Status
			ar.Detail = fmt.Sprintf("execution-log row seq %d has status %q, declared uniform status %q", row.Seq, row.Status, want)
			return ar
		}
	}
	ar.Outcome = domain.AssertionPass
	ar.Actual = want
	return ar
}

func evaluateProtocolViolations(ev domain.RunEvidence, layer domain.TestLayer, want int) domain.AssertionResult {
	if domain.LayerSuppresses(layer, domain.ClassProtocolViolations) {
		return domain.AssertionResult{
			Class:    domain.ClassProtocolViolations,
			Outcome:  domain.AssertionNotEvaluated,
			Expected: strconv.Itoa(want),
			Actual:   "not evaluated (layer suppressed)",
			Detail:   domain.SuppressionReason(layer, domain.ClassProtocolViolations),
		}
	}

	total := sumViolations(ev.ProtocolViolations) + sumViolations(ev.SubjectProtocolViolations)
	ar := domain.AssertionResult{
		Class:    domain.ClassProtocolViolations,
		Expected: strconv.Itoa(want),
		Actual:   strconv.Itoa(total),
	}
	if total == want {
		ar.Outcome = domain.AssertionPass
	} else {
		ar.Outcome = domain.AssertionFail
		ar.Detail = fmt.Sprintf("observed %d protocol violation(s), declared %d", total, want)
	}
	return ar
}

func sumViolations(m map[domain.ViolationClassKey]int) int {
	total := 0
	for _, n := range m {
		total += n
	}
	return total
}

func evaluateArtifactCreated(name string, snapshot []string) domain.AssertionResult {
	ar := domain.AssertionResult{
		Class:    domain.ClassArtifactCreated,
		Target:   name,
		Expected: "created",
	}
	if contains(snapshot, name) {
		ar.Outcome = domain.AssertionPass
		ar.Actual = "found"
	} else {
		ar.Outcome = domain.AssertionFail
		ar.Actual = "not found"
		ar.Detail = fmt.Sprintf("%s was declared created but is absent from the snapshot", name)
	}
	return ar
}

func evaluateArtifactNotCreated(name string, snapshot []string) domain.AssertionResult {
	ar := domain.AssertionResult{
		Class:    domain.ClassArtifactNotCreated,
		Target:   name,
		Expected: "not created",
	}
	if !contains(snapshot, name) {
		ar.Outcome = domain.AssertionPass
		ar.Actual = "absent"
	} else {
		ar.Outcome = domain.AssertionFail
		ar.Actual = "found"
		ar.Detail = fmt.Sprintf("%s was declared not created but is present in the snapshot", name)
	}
	return ar
}

func evaluateMinConcurrency(group string, want, observed int) domain.AssertionResult {
	ar := domain.AssertionResult{
		Class:    domain.ClassMinConcurrency,
		Target:   group,
		Expected: strconv.Itoa(want),
		Actual:   strconv.Itoa(observed),
	}
	if observed >= want {
		ar.Outcome = domain.AssertionPass
	} else {
		ar.Outcome = domain.AssertionFail
		ar.Detail = fmt.Sprintf("reconstructed peak concurrency for %q is %d, declared minimum %d", group, observed, want)
	}
	return ar
}

func evaluateEchoFidelity(records []domain.LogRecord) []domain.AssertionResult {
	var out []domain.AssertionResult
	for _, r := range records {
		if r.Kind != domain.RecordEnd || r.Echo == nil {
			continue
		}
		ar := domain.AssertionResult{
			Class:  domain.ClassEchoFidelity,
			Target: strconv.Itoa(r.Seq),
			Actual: r.Echo.Observed,
		}
		if r.Echo.Expected != nil {
			ar.Expected = string(r.Echo.Expected)
		}
		if r.Echo.Match {
			ar.Outcome = domain.AssertionPass
		} else {
			ar.Outcome = domain.AssertionFail
			ar.Detail = r.Echo.Diff
		}
		out = append(out, ar)
	}
	// For each uncorrelated completion event, report echo fidelity as
	// not evaluated with a named reason. Without this, the evaluation
	// is silently absent — the same invisible failure the run event exists to name.
	for _, r := range records {
		if r.Kind == domain.RecordRun && r.Event == domain.RunEventUncorrelatedCompletion {
			out = append(out, domain.AssertionResult{
				Class:    domain.ClassEchoFidelity,
				Target:   strconv.Itoa(r.Seq),
				Outcome:  domain.AssertionNotEvaluated,
				Expected: "echo match",
				Actual:   "could not evaluate -- uncorrelated completion",
				Detail:   "echo fidelity could not be evaluated: completion could not be correlated to its dispatch",
			})
		}
	}
	return out
}

func contains(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}

func stringSliceEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
