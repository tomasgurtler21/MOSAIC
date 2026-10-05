package domain_test

// Tests for the DecideHITLCompliance pure function.
//
// Coverage:
//
//   Short-circuit:
//   - EffectiveHITL=false -> HITLAccept regardless of Status and Approvals.
//
//   Every response status is gated (there is no non-SUCCESS shortcut):
//   - An unapproved written output triggers HITLRedispatch for SUCCESS,
//     COMPLETED_NEEDS_ACTION, PARTIALLY_DONE, NEEDS_CLARIFICATION,
//     CAPABILITY_EXCEEDED and BLOCKED; HITLEscalate once the re-dispatch is spent.
//   - Approved or empty approvals accept for non-SUCCESS statuses too.
//   - Read-failure values stay distinct in NonCompliant.
//
//   Empty Approvals list:
//   - EffectiveHITL=true, Status=SUCCESS, nil Approvals → HITLAccept (nothing
//     to verify).
//   - EffectiveHITL=true, Status=SUCCESS, empty (non-nil) Approvals →
//     HITLAccept.
//
//   All-approved cases:
//   - Single artifact, ApprovalTrue → HITLAccept, NonCompliant is empty.
//   - Multiple artifacts, all ApprovalTrue → HITLAccept, NonCompliant is empty.
//
//   Non-compliant cases — first redispatch available (RedispatchUsed=false):
//   - Single artifact, ApprovalFalse → HITLRedispatch.
//   - Single artifact, ApprovalAbsent → HITLRedispatch (absent is not approved).
//   - Single artifact, ApprovalNoFrontmatter → HITLRedispatch.
//   - Single artifact, ApprovalMalformed → HITLRedispatch.
//   - Single artifact, ApprovalFileMissing → HITLRedispatch (treated the same
//     as ApprovalFalse: a missing artifact has not closed its gate).
//   - Single artifact, ApprovalUnreadable → HITLRedispatch.
//
//   Non-compliant cases — redispatch already consumed (RedispatchUsed=true):
//   - Single artifact, ApprovalFalse → HITLEscalate.
//   - Single artifact, ApprovalFileMissing → HITLEscalate.
//
//   Mixed Approvals list:
//   - Some approved, some not → HITLRedispatch (first attempt), NonCompliant
//     lists only the non-approved artifacts in dispatch order.
//   - Same mixed list, RedispatchUsed=true → HITLEscalate, NonCompliant still
//     lists only the non-approved artifacts.
//
//   NonCompliant field population:
//   - When Outcome is HITLAccept, NonCompliant is nil or empty.
//   - When Outcome is HITLRedispatch, NonCompliant carries only the non-approved
//     entries in their original dispatch order.
//   - When Outcome is HITLEscalate, NonCompliant carries the same entries as
//     HITLRedispatch would.
//   - An approved artifact is never included in NonCompliant.
//   - NonCompliant paths match the ArtifactApproval.Path values as dispatched.

import (
	"testing"

	"mosaic-run/internal/domain"
)

func TestDecideHITLCompliance_HITLFalse_AlwaysAccept(t *testing.T) {
	// When HITL was not required, the decision is always accept regardless of
	// what the artifacts look like.
	cases := []struct {
		name      string
		approvals []domain.ArtifactApproval
	}{
		{"nil approvals", nil},
		{"single false", []domain.ArtifactApproval{{Path: "a.md", Approval: domain.ApprovalFalse}}},
		{"file missing", []domain.ArtifactApproval{{Path: "a.md", Approval: domain.ApprovalFileMissing}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := domain.HITLComplianceInput{
				EffectiveHITL:  false,
				Status:         domain.StatusSUCCESS,
				Approvals:      tc.approvals,
				RedispatchUsed: false,
			}
			got := domain.DecideHITLCompliance(in)
			if got.Outcome != domain.HITLAccept {
				t.Errorf("EffectiveHITL=false: got %v, want HITLAccept", got.Outcome)
			}
		})
	}
}

func TestDecideHITLCompliance_EveryStatus_UnapprovedWrittenOutput_Redispatches(t *testing.T) {
	// The gate runs for every response status: a written output that is not
	// approved triggers the single re-dispatch whatever the agent returned.
	statuses := []domain.StatusCode{
		domain.StatusSUCCESS,
		domain.StatusCOMPLETED_NEEDS_ACTION,
		domain.StatusPARTIALLY_DONE,
		domain.StatusNEEDS_CLARIFICATION,
		domain.StatusCAPABILITY_EXCEEDED,
		domain.StatusBLOCKED,
	}
	falseApproval := []domain.ArtifactApproval{
		{Path: "artifact.md", Approval: domain.ApprovalFalse},
	}
	for _, status := range statuses {
		t.Run(string(status), func(t *testing.T) {
			in := domain.HITLComplianceInput{
				EffectiveHITL:  true,
				Status:         status,
				Approvals:      falseApproval,
				RedispatchUsed: false,
			}
			got := domain.DecideHITLCompliance(in)
			if got.Outcome != domain.HITLRedispatch {
				t.Errorf("status=%s: got %v, want HITLRedispatch", status, got.Outcome)
			}
			if len(got.NonCompliant) != 1 || got.NonCompliant[0].Path != "artifact.md" {
				t.Errorf("status=%s: NonCompliant = %v, want the unapproved artifact", status, got.NonCompliant)
			}
		})
	}
}

func TestDecideHITLCompliance_EveryStatus_RedispatchSpent_Escalates(t *testing.T) {
	statuses := []domain.StatusCode{
		domain.StatusSUCCESS,
		domain.StatusCOMPLETED_NEEDS_ACTION,
		domain.StatusPARTIALLY_DONE,
		domain.StatusNEEDS_CLARIFICATION,
		domain.StatusCAPABILITY_EXCEEDED,
		domain.StatusBLOCKED,
	}
	for _, status := range statuses {
		t.Run(string(status), func(t *testing.T) {
			in := domain.HITLComplianceInput{
				EffectiveHITL:  true,
				Status:         status,
				Approvals:      []domain.ArtifactApproval{{Path: "artifact.md", Approval: domain.ApprovalFalse}},
				RedispatchUsed: true,
			}
			got := domain.DecideHITLCompliance(in)
			if got.Outcome != domain.HITLEscalate {
				t.Errorf("status=%s: got %v, want HITLEscalate", status, got.Outcome)
			}
		})
	}
}

func TestDecideHITLCompliance_NonSuccessStatus_ApprovedOrNothingWritten_Accepts(t *testing.T) {
	// A non-SUCCESS response passes the gate when every written output is
	// approved, and when nothing was written (empty approvals).
	statuses := []domain.StatusCode{
		domain.StatusCOMPLETED_NEEDS_ACTION,
		domain.StatusPARTIALLY_DONE,
		domain.StatusNEEDS_CLARIFICATION,
		domain.StatusCAPABILITY_EXCEEDED,
	}
	cases := []struct {
		name      string
		approvals []domain.ArtifactApproval
	}{
		{"all approved", []domain.ArtifactApproval{{Path: "a.md", Approval: domain.ApprovalTrue}}},
		{"nothing written", nil},
	}
	for _, status := range statuses {
		for _, tc := range cases {
			t.Run(string(status)+"/"+tc.name, func(t *testing.T) {
				in := domain.HITLComplianceInput{
					EffectiveHITL: true,
					Status:        status,
					Approvals:     tc.approvals,
				}
				got := domain.DecideHITLCompliance(in)
				if got.Outcome != domain.HITLAccept {
					t.Errorf("got %v, want HITLAccept", got.Outcome)
				}
			})
		}
	}
}

func TestDecideHITLCompliance_NonSuccessStatus_EveryReadFailure_StaysDistinctAndNonCompliant(t *testing.T) {
	// The read-failure values keep their identity in NonCompliant, so the
	// operator message can tell them apart, and each blocks acceptance.
	failures := []domain.HumanApproval{
		domain.ApprovalUnreadable,
		domain.ApprovalNoFrontmatter,
		domain.ApprovalMalformed,
		domain.ApprovalAbsent,
		domain.ApprovalFalse,
	}
	for _, f := range failures {
		in := domain.HITLComplianceInput{
			EffectiveHITL: true,
			Status:        domain.StatusPARTIALLY_DONE,
			Approvals:     []domain.ArtifactApproval{{Path: "a.md", Approval: f}},
		}
		got := domain.DecideHITLCompliance(in)
		if got.Outcome != domain.HITLRedispatch {
			t.Errorf("approval=%v: got %v, want HITLRedispatch", f, got.Outcome)
			continue
		}
		if len(got.NonCompliant) != 1 || got.NonCompliant[0].Approval != f {
			t.Errorf("approval=%v: NonCompliant = %v, want the read result preserved", f, got.NonCompliant)
		}
	}
}

func TestDecideHITLCompliance_HITLFalse_NonSuccessStatus_Accepts(t *testing.T) {
	in := domain.HITLComplianceInput{
		EffectiveHITL: false,
		Status:        domain.StatusCOMPLETED_NEEDS_ACTION,
		Approvals:     []domain.ArtifactApproval{{Path: "a.md", Approval: domain.ApprovalFalse}},
	}
	if got := domain.DecideHITLCompliance(in); got.Outcome != domain.HITLAccept {
		t.Errorf("EffectiveHITL=false: got %v, want HITLAccept", got.Outcome)
	}
}

func TestDecideHITLCompliance_EmptyApprovals_Accept(t *testing.T) {
	// When the output_artifacts list is empty, there is nothing to verify and
	// the step is compliant.
	cases := []struct {
		name      string
		approvals []domain.ArtifactApproval
	}{
		{"nil slice", nil},
		{"empty non-nil slice", []domain.ArtifactApproval{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := domain.HITLComplianceInput{
				EffectiveHITL:  true,
				Status:         domain.StatusSUCCESS,
				Approvals:      tc.approvals,
				RedispatchUsed: false,
			}
			got := domain.DecideHITLCompliance(in)
			if got.Outcome != domain.HITLAccept {
				t.Errorf("%s: got %v, want HITLAccept", tc.name, got.Outcome)
			}
		})
	}
}

func TestDecideHITLCompliance_AllApproved_Accept(t *testing.T) {
	// When every artifact in the list is approved, the step is compliant.
	cases := []struct {
		name      string
		approvals []domain.ArtifactApproval
	}{
		{
			"single approved",
			[]domain.ArtifactApproval{{Path: "out.md", Approval: domain.ApprovalTrue}},
		},
		{
			"multiple approved",
			[]domain.ArtifactApproval{
				{Path: "out1.md", Approval: domain.ApprovalTrue},
				{Path: "out2.md", Approval: domain.ApprovalTrue},
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := domain.HITLComplianceInput{
				EffectiveHITL:  true,
				Status:         domain.StatusSUCCESS,
				Approvals:      tc.approvals,
				RedispatchUsed: false,
			}
			got := domain.DecideHITLCompliance(in)
			if got.Outcome != domain.HITLAccept {
				t.Errorf("%s: got %v, want HITLAccept", tc.name, got.Outcome)
			}
			if len(got.NonCompliant) != 0 {
				t.Errorf("%s: NonCompliant should be empty on accept, got %v", tc.name, got.NonCompliant)
			}
		})
	}
}

func TestDecideHITLCompliance_NonCompliant_FirstAttempt_Redispatch(t *testing.T) {
	// When at least one artifact is not approved and the redispatch has not
	// been used, the outcome is HITLRedispatch.
	nonApprovedValues := []struct {
		name     string
		approval domain.HumanApproval
	}{
		{"ApprovalFalse", domain.ApprovalFalse},
		{"ApprovalAbsent", domain.ApprovalAbsent},
		{"ApprovalNoFrontmatter", domain.ApprovalNoFrontmatter},
		{"ApprovalMalformed", domain.ApprovalMalformed},
		{"ApprovalFileMissing", domain.ApprovalFileMissing},
		{"ApprovalUnreadable", domain.ApprovalUnreadable},
	}
	for _, tc := range nonApprovedValues {
		t.Run(tc.name, func(t *testing.T) {
			in := domain.HITLComplianceInput{
				EffectiveHITL: true,
				Status:        domain.StatusSUCCESS,
				Approvals: []domain.ArtifactApproval{
					{Path: "artifact.md", Approval: tc.approval},
				},
				RedispatchUsed: false,
			}
			got := domain.DecideHITLCompliance(in)
			if got.Outcome != domain.HITLRedispatch {
				t.Errorf("%s, RedispatchUsed=false: got %v, want HITLRedispatch", tc.name, got.Outcome)
			}
		})
	}
}

func TestDecideHITLCompliance_NonCompliant_SecondAttempt_Escalate(t *testing.T) {
	// When at least one artifact is not approved and the redispatch has already
	// been used, the outcome is HITLEscalate.
	cases := []struct {
		name     string
		approval domain.HumanApproval
	}{
		{"ApprovalFalse", domain.ApprovalFalse},
		{"ApprovalFileMissing", domain.ApprovalFileMissing},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := domain.HITLComplianceInput{
				EffectiveHITL: true,
				Status:        domain.StatusSUCCESS,
				Approvals: []domain.ArtifactApproval{
					{Path: "artifact.md", Approval: tc.approval},
				},
				RedispatchUsed: true,
			}
			got := domain.DecideHITLCompliance(in)
			if got.Outcome != domain.HITLEscalate {
				t.Errorf("%s, RedispatchUsed=true: got %v, want HITLEscalate", tc.name, got.Outcome)
			}
		})
	}
}

func TestDecideHITLCompliance_MixedApprovals_Redispatch(t *testing.T) {
	// When some artifacts are approved and some are not, the outcome is
	// HITLRedispatch (first attempt). Only the non-approved artifacts appear in
	// NonCompliant, in dispatch order.
	approvals := []domain.ArtifactApproval{
		{Path: "Orchestration/progress.md", Approval: domain.ApprovalTrue},
		{Path: "Orchestration/design.md", Approval: domain.ApprovalFalse},
		{Path: "Orchestration/notes.md", Approval: domain.ApprovalTrue},
		{Path: "Orchestration/output.md", Approval: domain.ApprovalAbsent},
	}
	in := domain.HITLComplianceInput{
		EffectiveHITL:  true,
		Status:         domain.StatusSUCCESS,
		Approvals:      approvals,
		RedispatchUsed: false,
	}
	got := domain.DecideHITLCompliance(in)
	if got.Outcome != domain.HITLRedispatch {
		t.Errorf("mixed approvals, first attempt: got %v, want HITLRedispatch", got.Outcome)
	}
	if len(got.NonCompliant) != 2 {
		t.Errorf("NonCompliant: got %d entries, want 2", len(got.NonCompliant))
	} else {
		if got.NonCompliant[0].Path != "Orchestration/design.md" {
			t.Errorf("NonCompliant[0].Path = %q, want %q", got.NonCompliant[0].Path, "Orchestration/design.md")
		}
		if got.NonCompliant[1].Path != "Orchestration/output.md" {
			t.Errorf("NonCompliant[1].Path = %q, want %q", got.NonCompliant[1].Path, "Orchestration/output.md")
		}
	}
}

func TestDecideHITLCompliance_MixedApprovals_Escalate(t *testing.T) {
	// When some artifacts are still non-approved after the redispatch has been
	// used, the outcome is HITLEscalate. NonCompliant is populated the same way.
	approvals := []domain.ArtifactApproval{
		{Path: "Orchestration/progress.md", Approval: domain.ApprovalTrue},
		{Path: "Orchestration/design.md", Approval: domain.ApprovalFalse},
	}
	in := domain.HITLComplianceInput{
		EffectiveHITL:  true,
		Status:         domain.StatusSUCCESS,
		Approvals:      approvals,
		RedispatchUsed: true,
	}
	got := domain.DecideHITLCompliance(in)
	if got.Outcome != domain.HITLEscalate {
		t.Errorf("mixed approvals, second attempt: got %v, want HITLEscalate", got.Outcome)
	}
	if len(got.NonCompliant) != 1 {
		t.Errorf("NonCompliant: got %d entries, want 1", len(got.NonCompliant))
	} else if got.NonCompliant[0].Path != "Orchestration/design.md" {
		t.Errorf("NonCompliant[0].Path = %q, want %q", got.NonCompliant[0].Path, "Orchestration/design.md")
	}
}

func TestDecideHITLCompliance_NonCompliant_EmptyOnAccept(t *testing.T) {
	// When the outcome is HITLAccept, NonCompliant must be nil or empty — no
	// spurious entries from the approved artifacts.
	in := domain.HITLComplianceInput{
		EffectiveHITL: true,
		Status:        domain.StatusSUCCESS,
		Approvals: []domain.ArtifactApproval{
			{Path: "out.md", Approval: domain.ApprovalTrue},
		},
		RedispatchUsed: false,
	}
	got := domain.DecideHITLCompliance(in)
	if got.Outcome != domain.HITLAccept {
		t.Fatalf("expected HITLAccept, got %v", got.Outcome)
	}
	if len(got.NonCompliant) != 0 {
		t.Errorf("NonCompliant on accept: got %v, want empty", got.NonCompliant)
	}
}

func TestDecideHITLCompliance_ApprovedArtifactNotInNonCompliant(t *testing.T) {
	// An artifact whose Approval is ApprovalTrue must never appear in the
	// NonCompliant list even when other artifacts are non-compliant.
	approvals := []domain.ArtifactApproval{
		{Path: "approved.md", Approval: domain.ApprovalTrue},
		{Path: "not-approved.md", Approval: domain.ApprovalFalse},
	}
	in := domain.HITLComplianceInput{
		EffectiveHITL:  true,
		Status:         domain.StatusSUCCESS,
		Approvals:      approvals,
		RedispatchUsed: false,
	}
	got := domain.DecideHITLCompliance(in)
	for _, nc := range got.NonCompliant {
		if nc.Path == "approved.md" {
			t.Errorf("approved.md must not appear in NonCompliant, but it does: %v", got.NonCompliant)
		}
	}
}

func TestDecideHITLCompliance_FileMissingTreatedAsNonCompliant(t *testing.T) {
	// ApprovalFileMissing must not be treated as compliant. A SUCCESS result
	// whose declared output artifact does not exist on disk has not closed its
	// HITL gate.
	in := domain.HITLComplianceInput{
		EffectiveHITL: true,
		Status:        domain.StatusSUCCESS,
		Approvals: []domain.ArtifactApproval{
			{Path: "missing-artifact.md", Approval: domain.ApprovalFileMissing},
		},
		RedispatchUsed: false,
	}
	got := domain.DecideHITLCompliance(in)
	if got.Outcome == domain.HITLAccept {
		t.Errorf("ApprovalFileMissing must produce a non-accept outcome, got HITLAccept")
	}
}

func TestDecideHITLCompliance_RedispatchUsed_TransitionToEscalate(t *testing.T) {
	// The transition from HITLRedispatch (first attempt) to HITLEscalate
	// (second attempt) for the same non-compliant artifact, asserting that
	// exactly one redispatch is allowed per step.
	artifact := domain.ArtifactApproval{Path: "stage/output.md", Approval: domain.ApprovalFalse}

	firstAttempt := domain.HITLComplianceInput{
		EffectiveHITL:  true,
		Status:         domain.StatusSUCCESS,
		Approvals:      []domain.ArtifactApproval{artifact},
		RedispatchUsed: false,
	}
	firstDecision := domain.DecideHITLCompliance(firstAttempt)
	if firstDecision.Outcome != domain.HITLRedispatch {
		t.Errorf("first attempt: got %v, want HITLRedispatch", firstDecision.Outcome)
	}

	secondAttempt := domain.HITLComplianceInput{
		EffectiveHITL:  true,
		Status:         domain.StatusSUCCESS,
		Approvals:      []domain.ArtifactApproval{artifact},
		RedispatchUsed: true,
	}
	secondDecision := domain.DecideHITLCompliance(secondAttempt)
	if secondDecision.Outcome != domain.HITLEscalate {
		t.Errorf("second attempt: got %v, want HITLEscalate", secondDecision.Outcome)
	}
}

func TestDecideHITLCompliance_NonCompliantPaths_InDispatchOrder(t *testing.T) {
	// The NonCompliant slice must preserve the original dispatch order of the
	// non-approved entries, not skip or reorder them.
	approvals := []domain.ArtifactApproval{
		{Path: "z-artifact.md", Approval: domain.ApprovalFalse},
		{Path: "a-artifact.md", Approval: domain.ApprovalFalse},
		{Path: "m-artifact.md", Approval: domain.ApprovalFalse},
	}
	in := domain.HITLComplianceInput{
		EffectiveHITL:  true,
		Status:         domain.StatusSUCCESS,
		Approvals:      approvals,
		RedispatchUsed: false,
	}
	got := domain.DecideHITLCompliance(in)
	if len(got.NonCompliant) != 3 {
		t.Fatalf("expected 3 NonCompliant entries, got %d", len(got.NonCompliant))
	}
	expectedOrder := []string{"z-artifact.md", "a-artifact.md", "m-artifact.md"}
	for i, expected := range expectedOrder {
		if got.NonCompliant[i].Path != expected {
			t.Errorf("NonCompliant[%d].Path = %q, want %q", i, got.NonCompliant[i].Path, expected)
		}
	}
}
