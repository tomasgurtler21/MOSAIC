package engine_test

// Statuses and error codes outside the mechanical retries keep deviating to
// the orchestrator in the auto and auto-review modes, with no re-dispatch.

import (
	"testing"

	"mosaic-run/internal/domain"
)

func TestNext_NonRetryStatuses_Deviate(t *testing.T) {
	f := newRetryFixture(t)
	cases := []struct {
		name   string
		status domain.StatusCode
		code   domain.ErrorCode
	}{
		{"BLOCKED E100", domain.StatusBLOCKED, domain.ErrorINVALID_INVOCATION},
		{"BLOCKED E101", domain.StatusBLOCKED, domain.ErrorINPUT_NOT_FOUND},
		{"BLOCKED E401", domain.StatusBLOCKED, domain.ErrorDEPENDENCY_MISSING},
		{"BLOCKED E502", domain.StatusBLOCKED, domain.ErrorPERMISSION_DENIED},
		{"BLOCKED E503", domain.StatusBLOCKED, domain.ErrorUSER_CONTACT},
		{"BLOCKED without code", domain.StatusBLOCKED, domain.ErrorNone},
		{"NEEDS_CLARIFICATION", domain.StatusNEEDS_CLARIFICATION, domain.ErrorNone},
		{"CAPABILITY_EXCEEDED", domain.StatusCAPABILITY_EXCEEDED, domain.ErrorNone},
	}
	for _, tc := range cases {
		for _, mode := range autoModes {
			for _, resume := range []bool{false, true} {
				name := tc.name + "/" + modeName(mode)
				if resume {
					name += "/resume"
				}
				t.Run(name, func(t *testing.T) {
					log := (&runLog{}).stepWith(rtPlanner, "", tc.status, tc.code, rtRowPlanner, "stuck").
						inPhase(rtPlanPhase)

					dec := f.next(mode, log, resume)

					requireNonSuccessDeviation(t, dec)
					if dec.Dispatch != nil {
						t.Errorf("no re-dispatch expected, got %d step(s)", len(dec.Dispatch.Steps))
					}
				})
			}
		}
	}
}

// An earlier E501 in the window does not turn another code into a retry.
func TestNext_OtherBlockedCode_AfterE501Rows_StillDeviates(t *testing.T) {
	f := newRetryFixture(t)
	log := (&runLog{}).
		blocked(rtPlanner, "", e501, rtRowPlanner).
		blocked(rtPlanner, "", e502, rtRowPlanner).inPhase(rtPlanPhase)

	requireNonSuccessDeviation(t, f.next(domain.ExecutionModeAuto, log, false))
}
