package app

// workflow_managed_eligibility_test.go covers the Stage 4 requirement that an agent file
// carrying managed workflow blocks inside its AvailableWorkflows region remains eligible
// for harness-only discovery. Before Stage 4, such files were ineligible because the
// unknown-deployed validation code fired for the managed Workflow block names.
//
// Coverage:
//   - An agent file with <Workflow type="managed" name="..." version="..."> blocks inside
//     <AvailableWorkflows type="managed"> is eligible (Eligible=true) after Stage 4.
//   - An agent file with multiple managed Workflow blocks remains eligible.
//   - The eligibility verdict for a valid agent with managed workflow blocks does not carry
//     a Reason mentioning "unknown-deployed".
//   - An agent file whose AvailableWorkflows region carries type="core" Workflow blocks
//     (pre-Stage-4 format) is also eligible — those are type="core" which is not managed
//     and therefore not flagged by unknown-deployed. (Regression guard.)
//   - An agent file with an unrecognised managed name (not Workflow) is still ineligible.

import (
	"strings"
	"testing"
)

// agentWithManagedWorkflowBlocks returns eligible agent bytes containing one
// managed Workflow block inside an AvailableWorkflows region.
func agentWithManagedWorkflowBlocks() []byte {
	return []byte("---\n" +
		"transform_version: \"3.0.0\"\n" +
		"version: \"6.0.0\"\n" +
		"id: \"1\"\n" +
		"---\n" +
		"<Identity type=\"core\">\n" +
		"# Orchestrator\n" +
		"\n" +
		"<AvailableWorkflows type=\"managed\">\n" +
		"<Workflow type=\"managed\" name=\"quick-fix\" version=\"3.0\">\n" +
		"## Quick Fix Workflow\n" +
		"\n" +
		"Use for small changes.\n" +
		"\n" +
		"</Workflow>\n" +
		"</AvailableWorkflows>\n" +
		"</Identity>\n")
}

// agentWithMultipleManagedWorkflowBlocks returns eligible agent bytes containing two
// managed Workflow blocks inside an AvailableWorkflows region.
func agentWithMultipleManagedWorkflowBlocks() []byte {
	return []byte("---\n" +
		"transform_version: \"3.0.0\"\n" +
		"version: \"6.0.0\"\n" +
		"id: \"2\"\n" +
		"---\n" +
		"<Identity type=\"core\">\n" +
		"# Orchestrator\n" +
		"\n" +
		"<AvailableWorkflows type=\"managed\">\n" +
		"<Workflow type=\"managed\" name=\"quick-fix\" version=\"3.0\">\n" +
		"Quick fix content.\n" +
		"</Workflow>\n" +
		"<Workflow type=\"managed\" name=\"greenfield-tdd\" version=\"3.3\">\n" +
		"Greenfield TDD content.\n" +
		"</Workflow>\n" +
		"</AvailableWorkflows>\n" +
		"</Identity>\n")
}

// agentWithUnrecognisedManagedName returns agent bytes with an unrecognised managed name,
// which must remain ineligible even after Stage 4.
func agentWithUnrecognisedManagedName() []byte {
	return []byte("---\n" +
		"transform_version: \"3.0.0\"\n" +
		"version: \"6.0.0\"\n" +
		"id: \"3\"\n" +
		"---\n" +
		"<Identity type=\"core\">\n" +
		"Content.\n" +
		"<UnrecognisedManagedBlock type=\"managed\">\n" +
		"Unknown.\n" +
		"</UnrecognisedManagedBlock>\n" +
		"</Identity>\n")
}

// ---------------------------------------------------------------------------
// Stage 4 eligibility with managed Workflow blocks
// ---------------------------------------------------------------------------

func TestEligibleHarnessOnly_Stage4_AgentWithOneManagedWorkflowBlock_IsEligible(t *testing.T) {
	// After Stage 4, a file with one managed Workflow block inside AvailableWorkflows must
	// be eligible. Before Stage 4, unknown-deployed blocks this — this test is RED until
	// the Stage 4 docformat vocabulary and validate amendments are in place.
	src := agentWithManagedWorkflowBlocks()
	verdict := eligibleHarnessOnly(src)

	if !verdict.Eligible {
		t.Errorf("eligibleHarnessOnly: Eligible=false for agent with managed Workflow block; "+
			"after Stage 4, managed Workflow blocks must not trigger unknown-deployed.\nReason: %s",
			verdict.Reason)
	}
}

func TestEligibleHarnessOnly_Stage4_AgentWithMultipleManagedWorkflowBlocks_IsEligible(t *testing.T) {
	// Multiple managed Workflow blocks must all be recognised — the file must remain eligible.
	src := agentWithMultipleManagedWorkflowBlocks()
	verdict := eligibleHarnessOnly(src)

	if !verdict.Eligible {
		t.Errorf("eligibleHarnessOnly: Eligible=false for agent with multiple managed Workflow blocks; "+
			"all recognised managed block names must be exempt from unknown-deployed.\nReason: %s",
			verdict.Reason)
	}
}

func TestEligibleHarnessOnly_Stage4_ManagedWorkflowBlock_ReasonDoesNotMentionUnknownDeployed(t *testing.T) {
	// When eligibility fails for a different reason, it must not be the unknown-deployed code.
	// This test verifies the specific failure mode of pre-Stage-4 behaviour is gone.
	src := agentWithManagedWorkflowBlocks()
	verdict := eligibleHarnessOnly(src)

	// If the verdict is ineligible AND mentions unknown-deployed, that is the pre-Stage-4 bug.
	if !verdict.Eligible && strings.Contains(verdict.Reason, "unknown-deployed") {
		t.Errorf("eligibleHarnessOnly Reason mentions \"unknown-deployed\" for agent with managed Workflow block; "+
			"after Stage 4, managed Workflow blocks must not produce unknown-deployed.\nReason: %s",
			verdict.Reason)
	}
}

func TestEligibleHarnessOnly_Stage4_UnrecognisedManagedName_StillIneligible(t *testing.T) {
	// Stage 4 must not widen eligibility to files with truly unrecognised managed names.
	// Only "Workflow" (and its compound forms) are now recognised; others remain ineligible.
	src := agentWithUnrecognisedManagedName()
	verdict := eligibleHarnessOnly(src)

	if verdict.Eligible {
		t.Errorf("eligibleHarnessOnly: Eligible=true for agent with unrecognised managed name; "+
			"Stage 4 must not suppress unknown-deployed for names outside CanonicalManagedBlocks")
	}
	if !strings.Contains(verdict.Reason, "unknown-deployed") {
		t.Errorf("eligibleHarnessOnly Reason does not mention \"unknown-deployed\" for agent with unrecognised managed name; "+
			"want the reason to identify the blocking issue.\nReason: %s", verdict.Reason)
	}
}

func TestEligibleHarnessOnly_Stage4_AgentWithCoreTypeWorkflowBlocks_IsEligible(t *testing.T) {
	// A pre-Stage-4 deployed file whose AvailableWorkflows contains type="core" Workflow blocks
	// must also be eligible. type="core" is NodeSection, not NodeDeployed, so it does not
	// trigger unknown-deployed. This is a regression guard against inadvertent breakage.
	src := []byte("---\n" +
		"transform_version: \"2.0.0\"\n" +
		"version: \"5.0.0\"\n" +
		"id: \"4\"\n" +
		"---\n" +
		"<Identity type=\"core\">\n" +
		"# Orchestrator\n" +
		"\n" +
		"<AvailableWorkflows type=\"managed\">\n" +
		"<Workflow type=\"core\" name=\"quick-fix\" version=\"3.0\">\n" +
		"Quick fix content.\n" +
		"</Workflow>\n" +
		"</AvailableWorkflows>\n" +
		"</Identity>\n")

	verdict := eligibleHarnessOnly(src)

	if !verdict.Eligible {
		t.Errorf("eligibleHarnessOnly: Eligible=false for agent with type=\"core\" Workflow blocks "+
			"(pre-Stage-4 format); these are NodeSection nodes and must not trigger unknown-deployed.\nReason: %s",
			verdict.Reason)
	}
}
