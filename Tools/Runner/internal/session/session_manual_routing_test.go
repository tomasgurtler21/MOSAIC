package session_test

// Session-level tests of manual routing: a full manual dispatch is run and
// recorded exactly as chosen, and every way manual routing can end without a
// dispatch is a resumable stop that dispatches and records nothing.

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"mosaic-common/interaction"
	"mosaic-run/internal/domain"
)

// requireOnlyDispatchOf asserts the run invoked exactly the named agent once
// and returns its request.
func requireOnlyDispatchOf(t *testing.T, rig *manualRig, agent string) domain.ProtocolRequest {
	t.Helper()
	invs := rig.f.Invocations()
	if len(invs) != 1 || invs[0].Agent.Identifier != agent {
		t.Fatalf("want exactly one dispatch of %s, got %v", agent, invokedAgents(rig.f))
	}
	return invs[0].Request
}

func hasSuffixIn(paths []string, suffix string) bool {
	return slices.ContainsFunc(paths, func(p string) bool { return strings.HasSuffix(p, suffix) })
}

func TestSession_ManualRouting_BuildReviewAtRow12Stage3WithAnExtraInput_DispatchesAndRecordsExactlyThat(t *testing.T) {
	user := newManualUser(t).completeDialogue("3", "docs/extra.md")
	rig := newManualRig(t, user, nil)
	queueSuccess(rig.f, "build-review", "build-review#3")

	got, err := rig.ses.Start(context.Background(), rig.cfg)

	requireRunStatus(t, got, err, domain.RunStoppedByConsultant)
	req := requireOnlyDispatchOf(t, rig, "build-review")
	if req.TaskDescription != "review the build of the stage" {
		t.Errorf("want the task typed by the user, got %q", req.TaskDescription)
	}
	if len(req.InputArtifacts) != 2 || !hasSuffixIn(req.InputArtifacts, "Stage-3/Plan.md") || !hasSuffixIn(req.InputArtifacts, "docs/extra.md") {
		t.Errorf("want inputs: the row's stage 3 default plus the extra path, got %v", req.InputArtifacts)
	}
	if len(req.OutputArtifacts) != 1 || !hasSuffixIn(req.OutputArtifacts, "Stage-3/Build.md") {
		t.Errorf("want the row's stage 3 output default, got %v", req.OutputArtifacts)
	}
	steps := workflowSteps(rig.store)
	if len(steps) != 1 {
		t.Fatalf("want one recorded workflow step, got %+v", steps)
	}
	requireStepAt(t, steps[0], "EXECUTION", 12, "Implementation.3")
	if steps[0].AgentInstance != "build-review#3" {
		t.Errorf("want build-review#3 recorded, got %q", steps[0].AgentInstance)
	}
}

func TestSession_ManualRouting_InputsQuestion_IsPreCheckedWithTheRowDefaultsAndOffersTheRegistry(t *testing.T) {
	user := newManualUser(t).completeDialogue("3", "docs/extra.md")
	registry := []domain.ArtifactRegistryEntry{
		{Artifact: "Orchestration-" + testRunID + "/Stage-3/Design.md", CreatedIn: "PLANNING", CreatedBy: "spec-writer#2"},
	}
	rig := newManualRig(t, user, registry)
	queueSuccess(rig.f, "build-review", "build-review#3")

	got, err := rig.ses.Start(context.Background(), rig.cfg)

	requireRunStatus(t, got, err, domain.RunStoppedByConsultant)
	qs := user.manyQs[domain.QuestionManualInputs]
	if len(qs) == 0 {
		t.Fatal("want the inputs question asked")
	}
	if want := []string{"Stage-3/Plan.md"}; !slices.Equal(qs[0].DefaultOptionIDs, want) {
		t.Errorf("want the row's stage 3 inputs pre-checked %v, got %v", want, qs[0].DefaultOptionIDs)
	}
	var offered []string
	for _, o := range qs[0].Options {
		offered = append(offered, o.ID)
	}
	if !slices.Contains(offered, "Stage-3/Design.md") {
		t.Errorf("want the registry artifact offered in its recorded (unprefixed) form, got %v", offered)
	}
	outs := user.manyQs[domain.QuestionManualOutputs]
	if len(outs) == 0 || !slices.Equal(outs[0].DefaultOptionIDs, []string{"Stage-3/Build.md"}) {
		t.Errorf("want the row's stage 3 outputs pre-checked, got %+v", outs)
	}
}

func TestSession_ManualRouting_StageOutsideTheSetIsCorrected_ThenDispatchesAtTheCorrectedStage(t *testing.T) {
	user := newManualUser(t).completeDialogue("3", "docs/extra.md")
	user.one[domain.QuestionManualStage] = nil
	user.typeOne(domain.QuestionManualStage, "9").pickOne(domain.QuestionManualStage, "3")
	rig := newManualRig(t, user, nil)
	queueSuccess(rig.f, "build-review", "build-review#3")

	got, err := rig.ses.Start(context.Background(), rig.cfg)

	requireRunStatus(t, got, err, domain.RunStoppedByConsultant)
	requireOnlyDispatchOf(t, rig, "build-review")
	steps := workflowSteps(rig.store)
	if len(steps) != 1 {
		t.Fatalf("want one recorded workflow step, got %+v", steps)
	}
	requireStepAt(t, steps[0], "EXECUTION", 12, "Implementation.3")
}

func TestSession_ManualRouting_StopOption_StopsResumablyWithItsReason(t *testing.T) {
	user := newManualUser(t).pickOne(domain.QuestionManualRow, domain.ManualStopOptionID)
	rig := newManualRig(t, user, nil)

	got, err := rig.ses.Start(context.Background(), rig.cfg)

	requireRunStatus(t, got, err, domain.RunStoppedByConsultant)
	if got.StopReason != "user requested stop" {
		t.Errorf("want stop reason %q, got %q", "user requested stop", got.StopReason)
	}
	if n := len(rig.f.Invocations()); n != 0 {
		t.Errorf("want nothing dispatched, got %d invocations", n)
	}
}

func TestSession_ManualRouting_EndingWithoutDispatch_IsAResumableStopThatRecordsNothing(t *testing.T) {
	cases := []struct {
		name   string
		user   func(t *testing.T) *manualUser
		failed domain.ConsultationFailure
	}{
		{"user cancel at the first step", func(t *testing.T) *manualUser { return newManualUser(t) },
			domain.ConsultFailUserAbandoned},
		{"interaction unavailable", func(t *testing.T) *manualUser {
			u := newManualUser(t)
			u.fallback = interaction.Unsupported
			return u
		}, domain.ConsultFailInteractionUnavailable},
		{"invalid results beyond the bound", func(t *testing.T) *manualUser {
			return newManualUser(t).pickOne(domain.QuestionManualRow, "99")
		}, domain.ConsultFailManualBoundExceeded},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rig := newManualRig(t, tc.user(t), nil)
			logRows := len(rig.store.state.ExecutionLog)

			got, err := rig.ses.Start(context.Background(), rig.cfg)

			requireRunStatus(t, got, err, domain.RunStoppedByConsultant)
			var ce *domain.ConsultationError
			if !errors.As(got.Cause, &ce) || ce.Failure != tc.failed {
				t.Fatalf("want cause failure %q, got %v", tc.failed, got.Cause)
			}
			if got.StopReason == "" || got.StopReason != ce.Detail {
				t.Errorf("want the stop reason to name the case (%q), got %q", ce.Detail, got.StopReason)
			}
			if n := len(rig.f.Invocations()); n != 0 {
				t.Errorf("want nothing dispatched, got %d invocations", n)
			}
			if steps := workflowSteps(rig.store); len(steps) != 0 {
				t.Errorf("want nothing recorded, got %+v", steps)
			}
			if got := len(rig.store.state.ExecutionLog); got != logRows {
				t.Errorf("want no new Execution Log row, had %d and now %d", logRows, got)
			}
		})
	}
}
