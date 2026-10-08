package deviation_test

// Shared doubles and builders for the manual routing dialogue tests: a
// scripted Interaction that answers by question ID (not call order), records
// every question, and a request carrying row defaults and a registry.

import (
	"context"
	"fmt"
	"slices"
	"testing"
	"time"

	"mosaic-common/interaction"
	"mosaic-run/internal/deviation"
	"mosaic-run/internal/domain"
)

type (
	oneFn  func(q interaction.ChoiceQuestion) interaction.ChoiceAnswer
	manyFn func(q interaction.ChoiceQuestion) interaction.MultiChoiceAnswer
	textFn func(q interaction.TextQuestion) interaction.TextAnswer
)

// dialogue is a scripted interaction.Interaction. Each question ID owns a queue
// of answers: they are consumed in order and the last one repeats. A question
// with no queue fails the test and is answered Cancelled, so a dialogue that
// asks something unexpected still terminates.
type dialogue struct {
	t       *testing.T
	one     map[interaction.QuestionID][]oneFn
	many    map[interaction.QuestionID][]manyFn
	text    map[interaction.QuestionID][]textFn
	calls   []interaction.QuestionID
	oneQs   map[interaction.QuestionID][]interaction.ChoiceQuestion
	manyQs  map[interaction.QuestionID][]interaction.ChoiceQuestion
	textQs  map[interaction.QuestionID][]interaction.TextQuestion
	notices []interaction.Notice
}

func newDialogue(t *testing.T) *dialogue {
	return &dialogue{
		t:      t,
		one:    map[interaction.QuestionID][]oneFn{},
		many:   map[interaction.QuestionID][]manyFn{},
		text:   map[interaction.QuestionID][]textFn{},
		oneQs:  map[interaction.QuestionID][]interaction.ChoiceQuestion{},
		manyQs: map[interaction.QuestionID][]interaction.ChoiceQuestion{},
		textQs: map[interaction.QuestionID][]interaction.TextQuestion{},
	}
}

// standard scripts a complete valid dialogue for row (and stage, when not "").
func (d *dialogue) standard(row, stage string) *dialogue {
	d.one[domain.QuestionManualRow] = []oneFn{pick(row)}
	if stage != "" {
		d.one[domain.QuestionManualStage] = []oneFn{pick(stage)}
	}
	d.text[domain.QuestionManualTask] = []textFn{say("do the work")}
	d.many[domain.QuestionManualInputs] = []manyFn{acceptDefaults()}
	d.many[domain.QuestionManualOutputs] = []manyFn{acceptDefaults()}
	d.text[domain.QuestionManualConstraints] = []textFn{say("")}
	d.one[domain.QuestionManualHITL] = []oneFn{pick(domain.ManualHITLDefault)}
	return d
}

// cancelFirst makes the first answer to question id a Cancelled one; the
// scripted answers follow.
func (d *dialogue) cancelFirst(id interaction.QuestionID) *dialogue {
	if q, ok := d.one[id]; ok {
		d.one[id] = append([]oneFn{cancelOne}, q...)
	}
	if q, ok := d.many[id]; ok {
		d.many[id] = append([]manyFn{cancelMany}, q...)
	}
	if q, ok := d.text[id]; ok {
		d.text[id] = append([]textFn{cancelText}, q...)
	}
	return d
}

func nextOf[F any](queue []F) (F, []F) {
	if len(queue) > 1 {
		return queue[0], queue[1:]
	}
	return queue[0], queue
}

// bump records the call and enforces a hard cap so a looping implementation
// fails the test instead of spinning.
func (d *dialogue) bump(id interaction.QuestionID) {
	d.calls = append(d.calls, id)
	if len(d.calls) > 4*deviation.ManualPromptLimit {
		d.t.Fatalf("dialogue asked more than %d questions; routing does not terminate", 4*deviation.ManualPromptLimit)
	}
}

func (d *dialogue) SelectOne(_ context.Context, q interaction.ChoiceQuestion) (interaction.ChoiceAnswer, error) {
	d.bump(q.ID)
	d.oneQs[q.ID] = append(d.oneQs[q.ID], q)
	queue := d.one[q.ID]
	if len(queue) == 0 {
		d.t.Errorf("unexpected SelectOne question %q", q.ID)
		return interaction.ChoiceAnswer{Status: interaction.Cancelled}, nil
	}
	fn, rest := nextOf(queue)
	d.one[q.ID] = rest
	return fn(q), nil
}

func (d *dialogue) SelectMany(_ context.Context, q interaction.ChoiceQuestion) (interaction.MultiChoiceAnswer, error) {
	d.bump(q.ID)
	d.manyQs[q.ID] = append(d.manyQs[q.ID], q)
	queue := d.many[q.ID]
	if len(queue) == 0 {
		d.t.Errorf("unexpected SelectMany question %q", q.ID)
		return interaction.MultiChoiceAnswer{Status: interaction.Cancelled}, nil
	}
	fn, rest := nextOf(queue)
	d.many[q.ID] = rest
	return fn(q), nil
}

func (d *dialogue) AskText(_ context.Context, q interaction.TextQuestion) (interaction.TextAnswer, error) {
	d.bump(q.ID)
	d.textQs[q.ID] = append(d.textQs[q.ID], q)
	queue := d.text[q.ID]
	if len(queue) == 0 {
		d.t.Errorf("unexpected AskText question %q", q.ID)
		return interaction.TextAnswer{Status: interaction.Cancelled}, nil
	}
	fn, rest := nextOf(queue)
	d.text[q.ID] = rest
	return fn(q), nil
}

func (d *dialogue) Confirm(_ context.Context, _ interaction.Question) (interaction.ConfirmAnswer, error) {
	d.t.Error("unexpected Confirm question")
	return interaction.ConfirmAnswer{Status: interaction.Cancelled}, nil
}

func (d *dialogue) Notify(_ context.Context, n interaction.Notice)         { d.notices = append(d.notices, n) }
func (d *dialogue) Progress(_ context.Context, _ interaction.ProgressEvent) {}

// ---- answer constructors ----

func pick(id string) oneFn {
	return func(interaction.ChoiceQuestion) interaction.ChoiceAnswer {
		return interaction.ChoiceAnswer{Status: interaction.Answered, OptionID: id}
	}
}

func typed(s string) oneFn {
	return func(interaction.ChoiceQuestion) interaction.ChoiceAnswer {
		return interaction.ChoiceAnswer{Status: interaction.Answered, Custom: s}
	}
}

func cancelOne(interaction.ChoiceQuestion) interaction.ChoiceAnswer {
	return interaction.ChoiceAnswer{Status: interaction.Cancelled}
}

func chooseMany(ids ...string) manyFn {
	return func(interaction.ChoiceQuestion) interaction.MultiChoiceAnswer {
		return interaction.MultiChoiceAnswer{Status: interaction.Answered, OptionIDs: ids}
	}
}

// acceptDefaults answers with exactly the pre-checked options, as a user who
// presses Enter without changing anything.
func acceptDefaults() manyFn { return acceptDefaultsPlus() }

// acceptDefaultsPlus answers with the pre-checked options plus free-form paths.
func acceptDefaultsPlus(custom ...string) manyFn {
	return func(q interaction.ChoiceQuestion) interaction.MultiChoiceAnswer {
		return interaction.MultiChoiceAnswer{
			Status: interaction.Answered, OptionIDs: slices.Clone(q.DefaultOptionIDs), Custom: custom,
		}
	}
}

func cancelMany(interaction.ChoiceQuestion) interaction.MultiChoiceAnswer {
	return interaction.MultiChoiceAnswer{Status: interaction.Cancelled}
}

func say(s string) textFn {
	return func(interaction.TextQuestion) interaction.TextAnswer {
		return interaction.TextAnswer{Status: interaction.Answered, Text: s}
	}
}

// keepDefault answers with the question's pre-filled text.
func keepDefault(q interaction.TextQuestion) interaction.TextAnswer {
	return interaction.TextAnswer{Status: interaction.Answered, Text: q.Default}
}

func cancelText(interaction.TextQuestion) interaction.TextAnswer {
	return interaction.TextAnswer{Status: interaction.Cancelled}
}

// ---- request and run helpers ----

// testRowDefaults reproduces engine.ResolveRowDefaults' observable stage
// argument behaviour over a fixed payload: a staged row needs a stage in the
// set (read at call time), a non-staged row takes none.
func testRowDefaults(stages *domain.StageSet) domain.RowDefaultsFunc {
	return func(row domain.RoutingRow, stage domain.StageNumber) (domain.DispatchDefaults, error) {
		if !row.PhaseParsed.IsStaged {
			if stage != 0 {
				return domain.DispatchDefaults{}, fmt.Errorf("row %d: non-staged row cannot take a stage", row.Index+1)
			}
			return domain.DispatchDefaults{
				InputArtifacts:  []string{"Plan.md"},
				OutputArtifacts: []string{fmt.Sprintf("Row%d/Out.md", row.Index+1)},
			}, nil
		}
		if stage == 0 {
			return domain.DispatchDefaults{}, fmt.Errorf("row %d: staged row requires a stage", row.Index+1)
		}
		if stages == nil {
			return domain.DispatchDefaults{}, fmt.Errorf("row %d: stage %d is not in the stage set", row.Index+1, stage)
		}
		if _, ok := stages.Entry(stage); !ok {
			return domain.DispatchDefaults{}, fmt.Errorf("row %d: stage %d is not in the stage set", row.Index+1, stage)
		}
		return domain.DispatchDefaults{
			InputArtifacts:  []string{"Plan.md", fmt.Sprintf("Stage-%d/Spec.md", stage)},
			OutputArtifacts: []string{fmt.Sprintf("Stage-%d/Out.md", stage)},
			HITL:            row.Index == 2,
		}, nil
	}
}

// manualRequest is a routing request with the stage set, row defaults and an
// (empty) registry the dialogue consumes.
func manualRequest(stages *domain.StageSet) domain.ConsultationRequest {
	req := rowStageRequest(stages)
	req.RowDefaults = testRowDefaults(stages)
	return req
}

// consultManual runs ConsultRouting over table under a deadline.
func consultManual(t *testing.T, d interaction.Interaction, table domain.RoutingTable, req domain.ConsultationRequest) (domain.RoutingInstruction, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	m := &deviation.ManualResolver{Interact: d, Table: table}
	instr, err := m.ConsultRouting(ctx, req)
	if ctx.Err() != nil {
		t.Fatalf("manual routing did not finish before the deadline: %v", ctx.Err())
	}
	return instr, err
}

// consultStaged runs the dialogue over the four-row table with stages 1..n.
func consultStaged(t *testing.T, d *dialogue, stageCount int) (domain.RoutingInstruction, error) {
	t.Helper()
	return consultManual(t, d, stagedRoutingTable(), manualRequest(stagesUpTo(stageCount)))
}

// requireDispatch fails unless the call returned a dispatch and no error.
func requireDispatch(t *testing.T, instr domain.RoutingInstruction, err error) *domain.DispatchInstruction {
	t.Helper()
	if err != nil {
		t.Fatalf("want a dispatch, got error: %v", err)
	}
	if instr.Dispatch == nil || instr.Stop != nil {
		t.Fatalf("want exactly a dispatch instruction, got %+v", instr)
	}
	return instr.Dispatch
}

// requireEnding fails unless the call ended with the given failure class and
// returned neither a dispatch nor a stop instruction.
func requireEnding(t *testing.T, instr domain.RoutingInstruction, err error, want domain.ConsultationFailure) {
	t.Helper()
	assertConsultationError(t, err, want)
	if instr.Dispatch != nil || instr.Stop != nil {
		t.Errorf("want no instruction on an ending outcome, got %+v", instr)
	}
}

// requireSequence compares the asked question IDs with want.
func requireSequence(t *testing.T, d *dialogue, want ...interaction.QuestionID) {
	t.Helper()
	if !slices.Equal(d.calls, want) {
		t.Errorf("question sequence:\n got %v\nwant %v", d.calls, want)
	}
}

func optionIDs(q interaction.ChoiceQuestion) []string {
	ids := make([]string, 0, len(q.Options))
	for _, o := range q.Options {
		ids = append(ids, o.ID)
	}
	return ids
}

// twelveRowTable has build-review at row 12, a staged Implementation row; rows
// 1..11 are non-staged fillers so the 1-based numbering is visible at row 12.
func twelveRowTable() domain.RoutingTable {
	var rows []domain.RoutingRow
	for i := 0; i < 11; i++ {
		rows = append(rows, domain.RoutingRow{
			Index: i, Phase: "PLANNING", Agent: fmt.Sprintf("filler-%d", i+1),
			PhaseParsed: domain.PhaseParsed{Name: "PLANNING"},
		})
	}
	rows = append(rows, domain.RoutingRow{
		Index: 11, Phase: "EXECUTION.Implementation.[StageNumber]", Agent: "build-review",
		PhaseParsed: domain.PhaseParsed{Name: "EXECUTION", IsStaged: true, Group: "Implementation"},
	})
	return domain.RoutingTable{Rows: rows}
}

// oneQ returns the i-th recorded SelectOne question with the given ID, failing
// the test (instead of panicking) when it was not asked that often.
func (d *dialogue) oneQ(id interaction.QuestionID, i int) interaction.ChoiceQuestion {
	d.t.Helper()
	if len(d.oneQs[id]) <= i {
		d.t.Fatalf("want question %s asked at least %d times, got %d (sequence %v)", id, i+1, len(d.oneQs[id]), d.calls)
	}
	return d.oneQs[id][i]
}

// manyQ is oneQ for SelectMany questions.
func (d *dialogue) manyQ(id interaction.QuestionID, i int) interaction.ChoiceQuestion {
	d.t.Helper()
	if len(d.manyQs[id]) <= i {
		d.t.Fatalf("want question %s asked at least %d times, got %d (sequence %v)", id, i+1, len(d.manyQs[id]), d.calls)
	}
	return d.manyQs[id][i]
}

// textQ is oneQ for AskText questions.
func (d *dialogue) textQ(id interaction.QuestionID, i int) interaction.TextQuestion {
	d.t.Helper()
	if len(d.textQs[id]) <= i {
		d.t.Fatalf("want question %s asked at least %d times, got %d (sequence %v)", id, i+1, len(d.textQs[id]), d.calls)
	}
	return d.textQs[id][i]
}

// requireOptions fails the test unless the question offers at least n options.
func requireOptions(t *testing.T, q interaction.ChoiceQuestion, n int) {
	t.Helper()
	if len(q.Options) < n {
		t.Fatalf("want at least %d options, got %d: %v", n, len(q.Options), optionIDs(q))
	}
}
