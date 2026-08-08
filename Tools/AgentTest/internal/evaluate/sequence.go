package evaluate

import (
	"fmt"
	"sort"

	"mosaic-agent-test/internal/domain"
)

// observedStep is one invocation's identity and declared group, in the
// order it was logged.
type observedStep struct {
	Seq      int
	Identity domain.CollaboratorIdentity
	Group    string
}

// observedSequence builds the ordered list of invocations from start
// records, sorted by their global sequence number.
func observedSequence(records []domain.LogRecord) []observedStep {
	var out []observedStep
	for _, r := range records {
		if r.Kind != domain.RecordStart {
			continue
		}
		out = append(out, observedStep{Seq: r.Seq, Identity: r.Identity, Group: r.Group})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Seq < out[j].Seq })
	return out
}

// evaluateSequence matches the observed invocation sequence against a
// declared SequenceAssertion. Order between steps decides the match; order
// within a declared parallel group does not.
func evaluateSequence(observed []observedStep, want domain.SequenceAssertion) domain.AssertionResult {
	pointer := 0
	for _, step := range want.Steps {
		switch {
		case step.Identity != nil:
			idx := -1
			for i := pointer; i < len(observed); i++ {
				if observed[i].Identity == *step.Identity {
					idx = i
					break
				}
			}
			if idx == -1 {
				return sequenceFail(fmt.Sprintf("expected invocation %s not found at or after position %d", step.Identity.Key(), pointer))
			}
			if want.Exact && idx != pointer {
				return sequenceFail(fmt.Sprintf("exact match requires %s at position %d, but it first appears at position %d", step.Identity.Key(), pointer, idx))
			}
			pointer = idx + 1

		default:
			start := -1
			for i := pointer; i < len(observed); i++ {
				if observed[i].Group == step.Group {
					start = i
					break
				}
			}
			if start == -1 {
				return sequenceFail(fmt.Sprintf("declared parallel group %q not found at or after position %d", step.Group, pointer))
			}
			if want.Exact && start != pointer {
				return sequenceFail(fmt.Sprintf("exact match requires parallel group %q at position %d, but it first appears at position %d", step.Group, pointer, start))
			}
			end := start
			for end < len(observed) && observed[end].Group == step.Group {
				end++
			}
			runLen := end - start
			if runLen != len(step.Members) {
				return sequenceFail(fmt.Sprintf("parallel group %q observed with %d member(s), declared %d", step.Group, runLen, len(step.Members)))
			}
			if !identityMultisetEqual(observed[start:end], step.Members) {
				return sequenceFail(fmt.Sprintf("parallel group %q members do not match the declared set", step.Group))
			}
			pointer = end
		}
	}

	if want.Exact && pointer != len(observed) {
		return sequenceFail(fmt.Sprintf("exact match requires nothing beyond the declared steps, but %d further invocation(s) were observed", len(observed)-pointer))
	}

	return domain.AssertionResult{
		Class:   domain.ClassInvocationSequence,
		Outcome: domain.AssertionPass,
	}
}

func sequenceFail(detail string) domain.AssertionResult {
	return domain.AssertionResult{
		Class:   domain.ClassInvocationSequence,
		Outcome: domain.AssertionFail,
		Detail:  detail,
	}
}

// identityMultisetEqual reports whether observed's identities are the same
// multiset as members, regardless of order.
func identityMultisetEqual(observed []observedStep, members []domain.CollaboratorIdentity) bool {
	if len(observed) != len(members) {
		return false
	}
	counts := map[string]int{}
	for _, m := range members {
		counts[m.Key()]++
	}
	for _, o := range observed {
		key := o.Identity.Key()
		if counts[key] == 0 {
			return false
		}
		counts[key]--
	}
	for _, remaining := range counts {
		if remaining != 0 {
			return false
		}
	}
	return true
}
