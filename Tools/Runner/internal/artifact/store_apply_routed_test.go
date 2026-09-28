package artifact_test

// Tests for the current_state that Apply records when a step carries a routed
// outcome: after a gate-discharging re-dispatch that returned SUCCESS, the
// Execution Log row shows what the invocation returned, while current_state
// records the status and error code the step is routed on, together with the
// re-dispatch's agent instance.

import (
	"context"
	"testing"
	"time"

	"mosaic-run/internal/domain"
)

func TestApply_RoutedOutcome_CurrentStateRecordsRoutedStatusUnderRedispatchAgent(t *testing.T) {
	store, state := mustCreateStore(t)
	ctx := context.Background()
	step := newTestStep(2, "reviewer#2", "PLANNING", "", domain.StatusSUCCESS, time.Now(), []string{"review.md"})
	step.Routed = &domain.RoutedOutcome{Status: domain.StatusCOMPLETED_NEEDS_ACTION}

	after, err := store.Apply(ctx, state, step)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	cs := after.CurrentState
	if cs.LastStatus != domain.StatusCOMPLETED_NEEDS_ACTION {
		t.Errorf("current_state.last_status: want COMPLETED_NEEDS_ACTION, got %s", cs.LastStatus)
	}
	if cs.LastAgent != "reviewer#2" {
		t.Errorf("current_state.last_agent: want reviewer#2, got %q", cs.LastAgent)
	}
	if len(after.ExecutionLog) != 1 || after.ExecutionLog[0].Status != domain.StatusSUCCESS {
		t.Errorf("Execution Log row must show the status as returned (SUCCESS), got %+v", after.ExecutionLog)
	}
	if len(after.ArtifactRegistry) != 1 {
		t.Errorf("Artifacts must be upserted from the re-dispatch, got %v", after.ArtifactRegistry)
	}
}

func TestApply_RoutedOutcome_ErrorCodeFollowsRoutedOutcome(t *testing.T) {
	store, state := mustCreateStore(t)
	step := newTestStep(2, "reviewer#2", "PLANNING", "", domain.StatusSUCCESS, time.Now(), nil)
	step.Routed = &domain.RoutedOutcome{Status: domain.StatusBLOCKED, ErrorCode: domain.ErrorPERMISSION_DENIED}

	after, err := store.Apply(context.Background(), state, step)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	if cs := after.CurrentState; cs.LastStatus != domain.StatusBLOCKED || cs.ErrorCode != domain.ErrorPERMISSION_DENIED {
		t.Errorf("current_state: want BLOCKED/E502, got %s/%q", cs.LastStatus, cs.ErrorCode)
	}
}

func TestApply_BlockedE503_AcceptedRowRecordsCurrentStateAndArtifacts(t *testing.T) {
	store, state := mustCreateStore(t)
	step := newTestStep(2, "reviewer#2", "PLANNING", "", domain.StatusBLOCKED, time.Now(), []string{"review.md"})
	step.ErrorCode = domain.ErrorUSER_CONTACT

	after, err := store.Apply(context.Background(), state, step)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	if cs := after.CurrentState; cs.LastStatus != domain.StatusBLOCKED || cs.ErrorCode != domain.ErrorUSER_CONTACT || cs.LastAgent != "reviewer#2" {
		t.Errorf("current_state: want BLOCKED/E503 by reviewer#2, got %+v", cs)
	}
	if len(after.ArtifactRegistry) != 1 {
		t.Errorf("Artifacts must be upserted, got %v", after.ArtifactRegistry)
	}
}
