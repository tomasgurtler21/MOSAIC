package session_test

// Fixtures for the manual routing session tests: a scripted user answering by
// question ID, and a resumed run over the thirteen-row manual-routing workflow
// whose first consultation is the real ManualResolver.

import (
	"context"
	"slices"
	"testing"

	"mosaic-common/interaction"
	"mosaic-run/internal/deviation"
	"mosaic-run/internal/domain"
	"mosaic-run/internal/harness"
	"mosaic-run/internal/session"
)

const manualRoutingWorkflow = "manual-routing"

var manualRoutingAgents = []string{
	"planner", "plan-review", "spec-writer", "spec-review", "test-writer", "test-review",
	"implementer", "refactorer", "lint-check", "doc-writer", "impl-review", "build-review", "final-review",
}

// manualUser is a scripted interaction.Interaction. Questions without a script
// are answered with fallback (Cancelled when unset).
type manualUser struct {
	t        *testing.T
	one      map[interaction.QuestionID][]func(interaction.ChoiceQuestion) interaction.ChoiceAnswer
	many     map[interaction.QuestionID]func(interaction.ChoiceQuestion) interaction.MultiChoiceAnswer
	text     map[interaction.QuestionID]func(interaction.TextQuestion) interaction.TextAnswer
	fallback interaction.AnswerStatus

	asked  []interaction.QuestionID
	manyQs map[interaction.QuestionID][]interaction.ChoiceQuestion
}

func newManualUser(t *testing.T) *manualUser {
	return &manualUser{
		t:      t,
		one:    map[interaction.QuestionID][]func(interaction.ChoiceQuestion) interaction.ChoiceAnswer{},
		many:   map[interaction.QuestionID]func(interaction.ChoiceQuestion) interaction.MultiChoiceAnswer{},
		text:   map[interaction.QuestionID]func(interaction.TextQuestion) interaction.TextAnswer{},
		manyQs: map[interaction.QuestionID][]interaction.ChoiceQuestion{},
	}
}

func (u *manualUser) count(id interaction.QuestionID) {
	u.asked = append(u.asked, id)
	if len(u.asked) > 300 {
		u.t.Fatal("manual user was asked more than 300 questions; routing does not terminate")
	}
}

// pickOne scripts the answers of a SelectOne question; the last one repeats.
func (u *manualUser) pickOne(id interaction.QuestionID, options ...string) *manualUser {
	for _, opt := range options {
		u.one[id] = append(u.one[id], func(interaction.ChoiceQuestion) interaction.ChoiceAnswer {
			return interaction.ChoiceAnswer{Status: interaction.Answered, OptionID: opt}
		})
	}
	return u
}

func (u *manualUser) typeOne(id interaction.QuestionID, custom string) *manualUser {
	u.one[id] = append(u.one[id], func(interaction.ChoiceQuestion) interaction.ChoiceAnswer {
		return interaction.ChoiceAnswer{Status: interaction.Answered, Custom: custom}
	})
	return u
}

func (u *manualUser) say(id interaction.QuestionID, text string) *manualUser {
	u.text[id] = func(interaction.TextQuestion) interaction.TextAnswer {
		return interaction.TextAnswer{Status: interaction.Answered, Text: text}
	}
	return u
}

// acceptDefaultsPlus answers a SelectMany with the pre-checked options plus
// the free-form paths in extra, in the way a user adds an extra path.
func (u *manualUser) acceptDefaultsPlus(id interaction.QuestionID, extra ...string) *manualUser {
	u.many[id] = func(q interaction.ChoiceQuestion) interaction.MultiChoiceAnswer {
		return interaction.MultiChoiceAnswer{
			Status: interaction.Answered, OptionIDs: slices.Clone(q.DefaultOptionIDs), Custom: extra,
		}
	}
	return u
}

// completeDialogue scripts a full valid dialogue for build-review at row 12 and
// the given stage, with one extra input path.
func (u *manualUser) completeDialogue(stage, extraInput string) *manualUser {
	u.pickOne(domain.QuestionManualRow, "12").pickOne(domain.QuestionManualStage, stage)
	u.say(domain.QuestionManualTask, "review the build of the stage")
	u.acceptDefaultsPlus(domain.QuestionManualInputs, extraInput)
	u.acceptDefaultsPlus(domain.QuestionManualOutputs)
	u.say(domain.QuestionManualConstraints, "")
	return u.pickOne(domain.QuestionManualHITL, domain.ManualHITLDefault)
}

func (u *manualUser) SelectOne(_ context.Context, q interaction.ChoiceQuestion) (interaction.ChoiceAnswer, error) {
	u.count(q.ID)
	queue := u.one[q.ID]
	if len(queue) == 0 {
		return interaction.ChoiceAnswer{Status: u.unscripted()}, nil
	}
	fn := queue[0]
	if len(queue) > 1 {
		u.one[q.ID] = queue[1:]
	}
	return fn(q), nil
}

func (u *manualUser) SelectMany(_ context.Context, q interaction.ChoiceQuestion) (interaction.MultiChoiceAnswer, error) {
	u.count(q.ID)
	u.manyQs[q.ID] = append(u.manyQs[q.ID], q)
	if fn, ok := u.many[q.ID]; ok {
		return fn(q), nil
	}
	return interaction.MultiChoiceAnswer{Status: u.unscripted()}, nil
}

func (u *manualUser) AskText(_ context.Context, q interaction.TextQuestion) (interaction.TextAnswer, error) {
	u.count(q.ID)
	if fn, ok := u.text[q.ID]; ok {
		return fn(q), nil
	}
	return interaction.TextAnswer{Status: u.unscripted()}, nil
}

func (u *manualUser) unscripted() interaction.AnswerStatus {
	if u.fallback == "" {
		return interaction.Cancelled
	}
	return u.fallback
}

func (u *manualUser) Confirm(context.Context, interaction.Question) (interaction.ConfirmAnswer, error) {
	return interaction.ConfirmAnswer{Status: interaction.Unsupported}, nil
}
func (u *manualUser) Notify(context.Context, interaction.Notice)          {}
func (u *manualUser) Progress(context.Context, interaction.ProgressEvent) {}

// manualRig is a resumed orchestrated run, last step plan-review at row 2,
// whose first consultation is a manual dispatch (the stop screen's "Manual
// dispatch"). Later consultations go to consultant.
type manualRig struct {
	ses        session.Session
	f          *harness.MockAdapter
	store      *memStore
	cfg        domain.RunConfig
	consultant *scriptedRoutingConsultant
}

func newManualRig(t *testing.T, user *manualUser, registry []domain.ArtifactRegistryEntry) *manualRig {
	t.Helper()
	dir := scopedTempDir(t)
	orchPath := copyOrchestratorFile(t, dir, "manual-routing-orch.md")
	for _, agent := range manualRoutingAgents {
		writeAgentFile(t, dir, agent)
	}
	writeConsultRowStagePlan(t, dir)

	seed := rowStageSeed("plan-review", 2, "PLANNING", "")
	seed.Workflow = manualRoutingWorkflow
	seed.RunID = testRunID
	seed.ArtifactRegistry = registry
	seed.RunSettings = domain.RunSettings{Mode: domain.ExecutionModeOrchestrated}

	consultant := &scriptedRoutingConsultant{}
	consultant.queueStop("done")
	f := harness.NewMockAdapter()
	store := &memStore{state: seed, exists: true}
	ses := session.New(session.Deps{
		Harness:  f,
		Store:    store,
		Routing:  consultant,
		Manual:   &deviation.ManualResolver{Interact: user},
		Clock:    fixedClock{t: epoch},
		Interact: &noopInteraction{},
	})
	return &manualRig{ses: ses, f: f, store: store, consultant: consultant, cfg: domain.RunConfig{
		OrchestratorFilePath: orchPath,
		WorkflowID:           manualRoutingWorkflow,
		Task:                 "test task",
		IsNewRun:             false,
		RunID:                testRunID,
		RunFolder:            dir,
		ManualDispatch:       true,
		RunSettings:          domain.RunSettings{Mode: domain.ExecutionModeOrchestrated},
	}}
}
