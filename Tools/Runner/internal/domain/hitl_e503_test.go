package domain_test

// Tests for the BLOCKED/E503 exemption of the HITL gate and for the routed
// outcome of a completed step.
//
// An E503 response means the agent could not reach the user: the gate could
// not run, so it is accepted whatever the written outputs are stamped. Any
// other BLOCKED code is gated like every other status.

import (
	"testing"

	"mosaic-run/internal/domain"
)

func TestDecideHITLCompliance_BlockedE503_AcceptedDespiteUnapprovedOutputs(t *testing.T) {
	for _, redispatchUsed := range []bool{false, true} {
		in := domain.HITLComplianceInput{
			EffectiveHITL:  true,
			Status:         domain.StatusBLOCKED,
			ErrorCode:      domain.ErrorUSER_CONTACT,
			Approvals:      []domain.ArtifactApproval{{Path: "a.md", Approval: domain.ApprovalFalse}},
			RedispatchUsed: redispatchUsed,
		}
		got := domain.DecideHITLCompliance(in)
		if got.Outcome != domain.HITLAccept {
			t.Errorf("RedispatchUsed=%v: got %v, want HITLAccept", redispatchUsed, got.Outcome)
		}
		if len(got.NonCompliant) != 0 {
			t.Errorf("RedispatchUsed=%v: NonCompliant must be empty for an exempt result, got %v", redispatchUsed, got.NonCompliant)
		}
	}
}

func TestDecideHITLCompliance_BlockedOtherCodes_StillGated(t *testing.T) {
	codes := []domain.ErrorCode{
		domain.ErrorNone,
		domain.ErrorINVALID_INVOCATION,
		domain.ErrorINPUT_NOT_FOUND,
		domain.ErrorDEPENDENCY_MISSING,
		domain.ErrorTOOL_UNAVAILABLE,
		domain.ErrorPERMISSION_DENIED,
	}
	approvals := []domain.ArtifactApproval{{Path: "a.md", Approval: domain.ApprovalFalse}}
	for _, code := range codes {
		t.Run("code="+string(code), func(t *testing.T) {
			first := domain.DecideHITLCompliance(domain.HITLComplianceInput{
				EffectiveHITL: true, Status: domain.StatusBLOCKED, ErrorCode: code, Approvals: approvals,
			})
			if first.Outcome != domain.HITLRedispatch {
				t.Errorf("first attempt: got %v, want HITLRedispatch", first.Outcome)
			}
			spent := domain.DecideHITLCompliance(domain.HITLComplianceInput{
				EffectiveHITL: true, Status: domain.StatusBLOCKED, ErrorCode: code, Approvals: approvals,
				RedispatchUsed: true,
			})
			if spent.Outcome != domain.HITLEscalate {
				t.Errorf("re-dispatch spent: got %v, want HITLEscalate", spent.Outcome)
			}
		})
	}
}

func TestDecideHITLCompliance_E503CodeOnNonBlockedStatus_StillGated(t *testing.T) {
	// The exemption is the pair BLOCKED + E503; the code alone does not exempt.
	got := domain.DecideHITLCompliance(domain.HITLComplianceInput{
		EffectiveHITL: true,
		Status:        domain.StatusSUCCESS,
		ErrorCode:     domain.ErrorUSER_CONTACT,
		Approvals:     []domain.ArtifactApproval{{Path: "a.md", Approval: domain.ApprovalFalse}},
	})
	if got.Outcome != domain.HITLRedispatch {
		t.Errorf("got %v, want HITLRedispatch", got.Outcome)
	}
}

func TestCompletedStep_Routed_OverridesStatusAndErrorCode(t *testing.T) {
	step := domain.CompletedStep{
		Status:    domain.StatusSUCCESS,
		ErrorCode: domain.ErrorNone,
		Routed: &domain.RoutedOutcome{
			Status:    domain.StatusBLOCKED,
			ErrorCode: domain.ErrorPERMISSION_DENIED,
		},
	}
	if got := step.RoutedStatus(); got != domain.StatusBLOCKED {
		t.Errorf("RoutedStatus: got %s, want BLOCKED", got)
	}
	if got := step.RoutedErrorCode(); got != domain.ErrorPERMISSION_DENIED {
		t.Errorf("RoutedErrorCode: got %q, want E502", got)
	}
}

func TestCompletedStep_NoRouted_UsesRecordedStatusAndErrorCode(t *testing.T) {
	step := domain.CompletedStep{
		Status:    domain.StatusBLOCKED,
		ErrorCode: domain.ErrorUSER_CONTACT,
	}
	if got := step.RoutedStatus(); got != domain.StatusBLOCKED {
		t.Errorf("RoutedStatus: got %s, want BLOCKED", got)
	}
	if got := step.RoutedErrorCode(); got != domain.ErrorUSER_CONTACT {
		t.Errorf("RoutedErrorCode: got %q, want E503", got)
	}
}
