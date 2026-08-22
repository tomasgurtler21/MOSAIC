package evaluate

import (
	"fmt"
	"sort"
	"strings"

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

// formatObservedKeys returns a bracketed, comma-separated list of identity
// keys for observed steps at/after fromPos. Returns "(none)" when no steps
// exist at or after that position.
func formatObservedKeys(observed []observedStep, fromPos int) string {
	var keys []string
	for i := fromPos; i < len(observed); i++ {
		keys = append(keys, observed[i].Identity.Key())
	}
	if len(keys) == 0 {
		return "(none)"
	}
	return "[" + strings.Join(keys, ", ") + "]"
}

// formatDeclaredSteps returns a bracketed, comma-separated summary of the
// declared sequence steps for use in the Expected diagnostic field.
func formatDeclaredSteps(steps []domain.SequenceStep) string {
	var parts []string
	for _, s := range steps {
		if s.Identity != nil {
			parts = append(parts, s.Identity.Key())
		} else {
			parts = append(parts, fmt.Sprintf("group:%s", s.Group))
		}
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

// evaluateSequence matches the observed invocation sequence against a
// declared SequenceAssertion. Order between steps decides the match; order
// within a declared parallel group does not.
func evaluateSequence(observed []observedStep, want domain.SequenceAssertion) domain.AssertionResult {
	expected := formatDeclaredSteps(want.Steps)
	actual := formatObservedKeys(observed, 0)

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
				detail := fmt.Sprintf(
					"expected invocation %s not found at or after position %d; observed at/after position %d: %s",
					step.Identity.Key(), pointer, pointer, formatObservedKeys(observed, pointer),
				)
				return sequenceFail(expected, actual, detail)
			}
			if want.Exact && idx != pointer {
				detail := fmt.Sprintf(
					"exact match requires %s at position %d, but it first appears at position %d; observed at/after position %d: %s",
					step.Identity.Key(), pointer, idx, pointer, formatObservedKeys(observed, pointer),
				)
				return sequenceFail(expected, actual, detail)
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
				detail := fmt.Sprintf(
					"declared parallel group %q not found at or after position %d; observed at/after position %d: %s",
					step.Group, pointer, pointer, formatObservedKeys(observed, pointer),
				)
				return sequenceFail(expected, actual, detail)
			}
			if want.Exact && start != pointer {
				detail := fmt.Sprintf(
					"exact match requires parallel group %q at position %d, but it first appears at position %d; observed at/after position %d: %s",
					step.Group, pointer, start, pointer, formatObservedKeys(observed, pointer),
				)
				return sequenceFail(expected, actual, detail)
			}
			end := start
			for end < len(observed) && observed[end].Group == step.Group {
				end++
			}
			runLen := end - start
			if runLen != len(step.Members) {
				detail := fmt.Sprintf(
					"parallel group %q observed with %d member(s), declared %d; observed at/after position %d: %s",
					step.Group, runLen, len(step.Members), pointer, formatObservedKeys(observed, pointer),
				)
				return sequenceFail(expected, actual, detail)
			}
			if !identityMultisetEqual(observed[start:end], step.Members) {
				detail := fmt.Sprintf(
					"parallel group %q members do not match the declared set; observed at/after position %d: %s",
					step.Group, pointer, formatObservedKeys(observed, pointer),
				)
				return sequenceFail(expected, actual, detail)
			}
			pointer = end
		}
	}

	if want.Exact && pointer != len(observed) {
		detail := fmt.Sprintf(
			"exact match requires nothing beyond the declared steps, but %d further invocation(s) were observed; observed at/after position %d: %s",
			len(observed)-pointer, pointer, formatObservedKeys(observed, pointer),
		)
		return sequenceFail(expected, actual, detail)
	}

	return domain.AssertionResult{
		Class:    domain.ClassInvocationSequence,
		Outcome:  domain.AssertionPass,
		Expected: expected,
		Actual:   actual,
	}
}

func sequenceFail(expected, actual, detail string) domain.AssertionResult {
	return domain.AssertionResult{
		Class:    domain.ClassInvocationSequence,
		Outcome:  domain.AssertionFail,
		Expected: expected,
		Actual:   actual,
		Detail:   detail,
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
