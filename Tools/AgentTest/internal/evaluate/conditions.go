package evaluate

import (
	"fmt"

	"mosaic-agent-test/internal/domain"
)

// evaluateConditions surfaces every reportable condition in ev that is not
// itself the verdict — a cost-attribution miss, an unterminated invocation
// interval, a degraded protocol extraction, an unmatched invocation, or an
// unreadable orchestration document. None of these may be silently dropped.
func evaluateConditions(ev domain.RunEvidence) []domain.RunCondition {
	var out []domain.RunCondition

	if ev.Cost.Attribution == domain.AttributionUnknownBucket || ev.Cost.Attribution == domain.AttributionUnavailable {
		out = append(out, domain.RunCondition{
			Kind:   domain.ConditionCostUnattributed,
			Detail: ev.Cost.Detail,
		})
	}

	for _, seq := range ev.ConcurrencyProblems.UnterminatedSeqs {
		out = append(out, domain.RunCondition{
			Kind:   domain.ConditionUnterminatedInterval,
			Detail: fmt.Sprintf("invocation seq %d has no matching end record", seq),
		})
	}

	for _, r := range ev.Records {
		if r.Kind == domain.RecordStart && r.Message != nil && r.Message.Extraction == domain.ExtractionDegraded {
			out = append(out, domain.RunCondition{
				Kind:   domain.ConditionExtractionDegraded,
				Detail: fmt.Sprintf("invocation seq %d's message was only degraded-recovered, not cleanly parsed", r.Seq),
			})
		}
		if r.Kind == domain.RecordRun && r.Event == domain.RunEventUnmatchedInvocation {
			out = append(out, domain.RunCondition{
				Kind:   domain.ConditionUnmatchedInvocation,
				Detail: r.Detail,
			})
		}
	}

	if ev.OrchestrationProblem != "" {
		out = append(out, domain.RunCondition{
			Kind:   domain.ConditionOrchestrationUnreadable,
			Detail: ev.OrchestrationProblem,
		})
	}

	if !ev.LogsProduced {
		out = append(out, domain.RunCondition{
			Kind:   domain.ConditionNoLogsProduced,
			Detail: fmt.Sprintf("no logs found under %s", ev.LogRoot),
		})
	}

	return out
}
