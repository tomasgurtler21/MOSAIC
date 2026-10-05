package analysis_test

// Tests that the harness recognition check in the aggregator flags unknown
// harness values but not the format-defined ghcp-cli value.

import (
	"testing"

	"mosaic-log-analyzer/internal/analysis"
	"mosaic-log-analyzer/internal/domain"
)

func usageEventWithHarness(h domain.Harness) domain.Event {
	ev := turnEvent("claude-sonnet", domain.TokenUsage{Input: domain.Tokens(10)})
	ev.Harness = h
	return ev
}

func TestAggregate_GHCPCLIHarness_RaisesNoUnrecognisedHarnessFinding(t *testing.T) {
	_, findings := analysis.Aggregate(orchestratorOnlyInput(usageEventWithHarness(domain.HarnessGHCPCLI)))

	if hasFindingKind(findings, domain.FindingUnrecognisedHarness) {
		t.Errorf("ghcp-cli must not raise FindingUnrecognisedHarness; got: %v", findings)
	}
}

func TestAggregate_UnknownHarness_IsToleratedAndFlagged(t *testing.T) {
	agg, findings := analysis.Aggregate(orchestratorOnlyInput(usageEventWithHarness("some-new-harness")))

	if !hasFindingKind(findings, domain.FindingUnrecognisedHarness) {
		t.Errorf("unknown harness must still raise FindingUnrecognisedHarness; got: %v", findings)
	}
	run := requireOneRun(t, agg)
	assertTokenPresent(t, "Orchestrator Input", run.Orchestrator.Totals().Input, 10)
}
