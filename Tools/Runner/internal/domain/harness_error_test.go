package domain_test

// Tests for the harness-error response constructor and the last_error_reason
// selector used when building routing consultations.

import (
	"errors"
	"strings"
	"testing"

	"mosaic-run/internal/domain"
)

func TestHarnessErrorResponse_IsBlockedE501WithDescription(t *testing.T) {
	const desc = "harness: subprocess timed out after 60s"

	got := domain.HarnessErrorResponse("agent-a#3", "20260928T103906Z-d408", errors.New(desc))

	if got.StatusCode != domain.StatusBLOCKED {
		t.Errorf("want StatusCode BLOCKED, got %q", got.StatusCode)
	}
	if got.ErrorCode != domain.ErrorTOOL_UNAVAILABLE {
		t.Errorf("want ErrorCode E501, got %q", got.ErrorCode)
	}
	if got.StatusMessage != desc {
		t.Errorf("want StatusMessage %q, got %q", desc, got.StatusMessage)
	}
	if got.ErrorReason != desc {
		t.Errorf("want ErrorReason %q, got %q", desc, got.ErrorReason)
	}
	if got.AgentInstanceID != "agent-a#3" {
		t.Errorf("want AgentInstanceID agent-a#3, got %q", got.AgentInstanceID)
	}
	if got.RunID != "20260928T103906Z-d408" {
		t.Errorf("want RunID passed through, got %q", got.RunID)
	}
}

func TestHarnessErrorResponse_MultiLineErrorReasonStaysVerbatim(t *testing.T) {
	desc := "line one\nline two | with pipe\n" + strings.Repeat("x", 300)

	got := domain.HarnessErrorResponse("agent-a#1", "r", errors.New(desc))

	if got.ErrorReason != desc {
		t.Errorf("want ErrorReason verbatim (no truncation or stripping), got %q", got.ErrorReason)
	}
}

func TestLastErrorReasonFor_BlockedReturnsErrorReasonVerbatim(t *testing.T) {
	resp := &domain.ProtocolResponse{
		StatusCode:  domain.StatusBLOCKED,
		ErrorCode:   domain.ErrorINVALID_INVOCATION,
		ErrorReason: "task_description is missing",
	}

	got := domain.LastErrorReasonFor(resp)

	if got == nil {
		t.Fatal("want non-nil pointer for a BLOCKED response, got nil")
	}
	if *got != "task_description is missing" {
		t.Errorf("want error_reason verbatim, got %q", *got)
	}
}

func TestLastErrorReasonFor_BlockedWithEmptyReasonReturnsPointerToEmptyString(t *testing.T) {
	resp := &domain.ProtocolResponse{StatusCode: domain.StatusBLOCKED}

	got := domain.LastErrorReasonFor(resp)

	if got == nil {
		t.Fatal("want pointer to empty string for BLOCKED with no error_reason, got nil")
	}
	if *got != "" {
		t.Errorf("want empty string, got %q", *got)
	}
}

func TestLastErrorReasonFor_NonBlockedStatusesReturnNil(t *testing.T) {
	for _, status := range []domain.StatusCode{
		domain.StatusSUCCESS,
		domain.StatusCOMPLETED_NEEDS_ACTION,
		domain.StatusPARTIALLY_DONE,
	} {
		t.Run(string(status), func(t *testing.T) {
			resp := &domain.ProtocolResponse{StatusCode: status, ErrorReason: "stray reason"}

			if got := domain.LastErrorReasonFor(resp); got != nil {
				t.Errorf("want nil for status %s, got %q", status, *got)
			}
		})
	}
}

func TestLastErrorReasonFor_NilResponseReturnsNil(t *testing.T) {
	if got := domain.LastErrorReasonFor(nil); got != nil {
		t.Errorf("want nil for nil response, got %q", *got)
	}
}
