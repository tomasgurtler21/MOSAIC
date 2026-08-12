package app_test

// transform_service_target_harness_options_test.go tests the target-harness ChoiceQuestion
// contract and the surrounding behaviours that must remain unchanged after the fix.
//
// T1.1 — Options are populated from the registry:
//
//	The ChoiceQuestion emitted for QTransformTargetHarness must carry one option per
//	registry harness, in Registry.List() order. Each option must mirror the harness's
//	ID and DisplayName. The source harness must be included but disabled. An unusable
//	harness must be disabled with its UnusableReason. When a harness is both the source
//	and unusable, the source reason wins.
//
//	Tests in this group capture the ChoiceQuestion passed to SelectOne via
//	capturingInteractionSpy and assert on the Options slice directly — the only way to
//	verify the fix drives transformHarness itself rather than a hand-built message.
//
// T1.2 — Unchanged surrounding behaviour:
//
//	The pre-answered path skips the question entirely (no options built, no SelectOne
//	call). The pre-answer whole-run same-harness check fires before any question is
//	asked. The post-interactive whole-run same-harness check fires when the interactive
//	answer resolves to the source harness ID.

import (
	"context"
	"errors"
	"testing"

	"mosaic-deploy/internal/app"
	"mosaic-deploy/internal/app/interactiontest"
	"mosaic-deploy/internal/domain"
)

// ---------------------------------------------------------------------------
// capturingInteractionSpy — records ChoiceQuestions sent to SelectOne
// ---------------------------------------------------------------------------

// capturingInteractionSpy wraps a base domain.Interaction, intercepts SelectOne calls
// for QTransformTargetHarness, captures the full ChoiceQuestion (including Options),
// and returns a scripted answer. All other Interaction methods delegate to the base.
//
// Use it when a test needs to assert on the shape of the question — specifically the
// Options field — rather than just whether the question was asked.
type capturingInteractionSpy struct {
	base                   domain.Interaction
	capturedTargetHarnessQ *domain.ChoiceQuestion
	targetHarnessAnswer    domain.ChoiceAnswer
}

func (s *capturingInteractionSpy) SelectOne(ctx context.Context, q domain.ChoiceQuestion) (domain.ChoiceAnswer, error) {
	if q.ID == domain.QTransformTargetHarness {
		cp := q
		s.capturedTargetHarnessQ = &cp
		return s.targetHarnessAnswer, nil
	}
	return s.base.SelectOne(ctx, q)
}

func (s *capturingInteractionSpy) SelectMany(ctx context.Context, q domain.ChoiceQuestion) (domain.MultiChoiceAnswer, error) {
	return s.base.SelectMany(ctx, q)
}

func (s *capturingInteractionSpy) AskText(ctx context.Context, q domain.TextQuestion) (domain.TextAnswer, error) {
	return s.base.AskText(ctx, q)
}

func (s *capturingInteractionSpy) Confirm(ctx context.Context, q domain.Question) (domain.ConfirmAnswer, error) {
	return s.base.Confirm(ctx, q)
}

func (s *capturingInteractionSpy) Notify(ctx context.Context, n domain.Notice) {
	s.base.Notify(ctx, n)
}

func (s *capturingInteractionSpy) Progress(ctx context.Context, e domain.ProgressEvent) {
	s.base.Progress(ctx, e)
}

func (s *capturingInteractionSpy) Review(ctx context.Context, p domain.Plan) (domain.ConfirmAnswer, error) {
	return s.base.Review(ctx, p)
}

// newCapturingSpy returns a capturingInteractionSpy that answers QTransformTargetHarness
// with targetHarnessID and delegates all other questions to a default stub.
func newCapturingSpy(targetHarnessID string) *capturingInteractionSpy {
	return &capturingInteractionSpy{
		base:                interactiontest.NewBuilder().Build(),
		targetHarnessAnswer: domain.ChoiceAnswer{Status: domain.Answered, OptionID: targetHarnessID},
	}
}

// ---------------------------------------------------------------------------
// T1.1: Target-harness question carries registry options
// ---------------------------------------------------------------------------

// TestTransformHarness_TargetHarnessQuestion_OptionsListNonEmpty verifies that when
// TargetHarnessID is empty the ChoiceQuestion emitted for QTransformTargetHarness
// carries at least one option. This is the core assertion for the bug being fixed:
// the TUI selection overlay renders nothing to pick when Options is nil, so the fix
// must populate Options from the registry.
func TestTransformHarness_TargetHarnessQuestion_OptionsListNonEmpty(t *testing.T) {
	// Arrange
	spy := newCapturingSpy(transformTargetHarness.ID)
	dummyStub := interactiontest.NewBuilder().Build()
	deps, _ := newTransformDeps(t, dummyStub)
	deps.Interaction = spy
	svc := app.New(deps)

	dir := t.TempDir()
	agentPath := writeTransformFile(t, dir, "my-agent.src.md", matchingAgentBytes())

	req := newPreAnsweredTransformRequest(agentPath)
	req.TargetHarnessID = "" // unset to trigger the interactive question

	// Act
	_, _ = svc.TransformHarness(context.Background(), req)

	// Assert: the question was captured and Options is non-empty.
	if spy.capturedTargetHarnessQ == nil {
		t.Fatal("QTransformTargetHarness was not asked; want the service to call SelectOne " +
			"when TargetHarnessID is empty")
	}
	opts := spy.capturedTargetHarnessQ.Options
	if len(opts) == 0 {
		t.Error("QTransformTargetHarness.Options is empty; want at least one option per registry " +
			"harness — this is the exact symptom of the bug: the selection overlay renders with " +
			"nothing to pick when Options is nil")
	}
}

// TestTransformHarness_TargetHarnessQuestion_OneOptionPerRegistryHarness verifies that
// the Options list contains exactly one entry per harness in the registry, preserving
// Registry.List() cardinality. Every harness must appear so the overlay is never empty
// even when all remaining harnesses are disabled.
func TestTransformHarness_TargetHarnessQuestion_OneOptionPerRegistryHarness(t *testing.T) {
	// Arrange: two-harness registry (source + target).
	spy := newCapturingSpy(transformTargetHarness.ID)
	dummyStub := interactiontest.NewBuilder().Build()
	deps, _ := newTransformDeps(t, dummyStub)
	deps.Interaction = spy
	svc := app.New(deps)

	dir := t.TempDir()
	agentPath := writeTransformFile(t, dir, "my-agent.src.md", matchingAgentBytes())

	req := newPreAnsweredTransformRequest(agentPath)
	req.TargetHarnessID = ""

	// Act
	_, _ = svc.TransformHarness(context.Background(), req)

	if spy.capturedTargetHarnessQ == nil {
		t.Fatal("QTransformTargetHarness was not asked")
	}

	// Assert: exactly one option per registry harness (2 in the transform registry).
	wantLen := len(newTransformRegistry().List())
	opts := spy.capturedTargetHarnessQ.Options
	if len(opts) != wantLen {
		t.Errorf("len(Options) = %d, want %d (one per registry harness in List() order); "+
			"every harness must appear so the selection overlay always has entries to display",
			len(opts), wantLen)
	}
}

// TestTransformHarness_TargetHarnessQuestion_OptionIDsMatchRegistryHarnessIDs verifies
// that each option's ID matches the corresponding registry harness's ID, in the order
// Registry.List() returns them.
func TestTransformHarness_TargetHarnessQuestion_OptionIDsMatchRegistryHarnessIDs(t *testing.T) {
	// Arrange
	spy := newCapturingSpy(transformTargetHarness.ID)
	dummyStub := interactiontest.NewBuilder().Build()
	deps, _ := newTransformDeps(t, dummyStub)
	deps.Interaction = spy
	svc := app.New(deps)

	dir := t.TempDir()
	agentPath := writeTransformFile(t, dir, "my-agent.src.md", matchingAgentBytes())

	req := newPreAnsweredTransformRequest(agentPath)
	req.TargetHarnessID = ""

	// Act
	_, _ = svc.TransformHarness(context.Background(), req)

	if spy.capturedTargetHarnessQ == nil {
		t.Fatal("QTransformTargetHarness was not asked")
	}

	refs := newTransformRegistry().List()
	opts := spy.capturedTargetHarnessQ.Options
	if len(opts) < len(refs) {
		t.Fatalf("len(Options) = %d, want %d; cannot verify IDs — options list is too short",
			len(opts), len(refs))
	}

	// Assert: each option ID mirrors the harness ID from Registry.List(), in order.
	for i, ref := range refs {
		if opts[i].ID != ref.ID {
			t.Errorf("Options[%d].ID = %q, want %q (Registry.List() order must be preserved)",
				i, opts[i].ID, ref.ID)
		}
	}
}

// TestTransformHarness_TargetHarnessQuestion_OptionLabelsMatchDisplayNames verifies that
// each option's Label matches the corresponding registry harness's DisplayName.
func TestTransformHarness_TargetHarnessQuestion_OptionLabelsMatchDisplayNames(t *testing.T) {
	// Arrange
	spy := newCapturingSpy(transformTargetHarness.ID)
	dummyStub := interactiontest.NewBuilder().Build()
	deps, _ := newTransformDeps(t, dummyStub)
	deps.Interaction = spy
	svc := app.New(deps)

	dir := t.TempDir()
	agentPath := writeTransformFile(t, dir, "my-agent.src.md", matchingAgentBytes())

	req := newPreAnsweredTransformRequest(agentPath)
	req.TargetHarnessID = ""

	// Act
	_, _ = svc.TransformHarness(context.Background(), req)

	if spy.capturedTargetHarnessQ == nil {
		t.Fatal("QTransformTargetHarness was not asked")
	}

	refs := newTransformRegistry().List()
	opts := spy.capturedTargetHarnessQ.Options
	if len(opts) < len(refs) {
		t.Fatalf("len(Options) = %d, want %d; cannot verify labels — options list is too short",
			len(opts), len(refs))
	}

	// Assert: each option Label mirrors the harness DisplayName from Registry.List(), in order.
	for i, ref := range refs {
		if opts[i].Label != ref.DisplayName {
			t.Errorf("Options[%d].Label = %q, want %q (harness DisplayName)",
				i, opts[i].Label, ref.DisplayName)
		}
	}
}

// TestTransformHarness_TargetHarnessQuestion_SourceHarnessOptionIsDisabled verifies that
// the option for the source harness has Disabled=true and a non-empty DisabledReason.
// The source harness is included (not excluded) so the option list is never empty on a
// single-harness registry — but it must be unselectable so the user cannot pick it.
func TestTransformHarness_TargetHarnessQuestion_SourceHarnessOptionIsDisabled(t *testing.T) {
	// Arrange
	spy := newCapturingSpy(transformTargetHarness.ID)
	dummyStub := interactiontest.NewBuilder().Build()
	deps, _ := newTransformDeps(t, dummyStub)
	deps.Interaction = spy
	svc := app.New(deps)

	dir := t.TempDir()
	agentPath := writeTransformFile(t, dir, "my-agent.src.md", matchingAgentBytes())

	req := newPreAnsweredTransformRequest(agentPath)
	req.TargetHarnessID = ""

	// Act
	_, _ = svc.TransformHarness(context.Background(), req)

	if spy.capturedTargetHarnessQ == nil {
		t.Fatal("QTransformTargetHarness was not asked")
	}

	// Find the source harness option by ID.
	var srcOpt *domain.Option
	for i := range spy.capturedTargetHarnessQ.Options {
		if spy.capturedTargetHarnessQ.Options[i].ID == transformSourceHarness.ID {
			cp := spy.capturedTargetHarnessQ.Options[i]
			srcOpt = &cp
			break
		}
	}

	if srcOpt == nil {
		t.Fatalf("source harness option (ID=%q) not found in Options; "+
			"the source harness must be included so the list is never empty on a single-harness registry",
			transformSourceHarness.ID)
	}

	// Assert: source harness option is disabled with a stated reason.
	if !srcOpt.Disabled {
		t.Error("source harness option Disabled = false; want true — the source harness " +
			"must not be selectable as its own transform target")
	}
	if srcOpt.DisabledReason == "" {
		t.Error("source harness option DisabledReason is empty; want a non-empty reason " +
			"explaining why the source harness cannot be chosen as the target")
	}
}

// TestTransformHarness_TargetHarnessQuestion_UsableNonSourceHarnessNotDisabled verifies
// that a registry harness that is usable and is not the transform source appears with
// Disabled=false in the option list, making it selectable.
func TestTransformHarness_TargetHarnessQuestion_UsableNonSourceHarnessNotDisabled(t *testing.T) {
	// Arrange: two-harness registry; target harness is usable and not the source.
	spy := newCapturingSpy(transformTargetHarness.ID)
	dummyStub := interactiontest.NewBuilder().Build()
	deps, _ := newTransformDeps(t, dummyStub)
	deps.Interaction = spy
	svc := app.New(deps)

	dir := t.TempDir()
	agentPath := writeTransformFile(t, dir, "my-agent.src.md", matchingAgentBytes())

	req := newPreAnsweredTransformRequest(agentPath)
	req.TargetHarnessID = ""

	// Act
	_, _ = svc.TransformHarness(context.Background(), req)

	if spy.capturedTargetHarnessQ == nil {
		t.Fatal("QTransformTargetHarness was not asked")
	}

	// Find the target harness option.
	var tgtOpt *domain.Option
	for i := range spy.capturedTargetHarnessQ.Options {
		if spy.capturedTargetHarnessQ.Options[i].ID == transformTargetHarness.ID {
			cp := spy.capturedTargetHarnessQ.Options[i]
			tgtOpt = &cp
			break
		}
	}

	if tgtOpt == nil {
		t.Fatalf("target harness option (ID=%q) not found in Options",
			transformTargetHarness.ID)
	}

	// Assert: the usable non-source harness is not disabled.
	if tgtOpt.Disabled {
		t.Errorf("target harness option Disabled = true; want false — a usable harness that "+
			"is not the transform source must be selectable; DisabledReason: %q",
			tgtOpt.DisabledReason)
	}
}

// TestTransformHarness_TargetHarnessQuestion_UnusableHarnessIsDisabled verifies that a
// registry harness that is not Usable (and is not the transform source) appears disabled
// in the option list with its UnusableReason carried through as the DisabledReason.
func TestTransformHarness_TargetHarnessQuestion_UnusableHarnessIsDisabled(t *testing.T) {
	// Arrange: three-harness registry — source, a usable target, and an unusable third harness.
	unusableHarness := domain.HarnessRef{
		ID:             "unusable-harness",
		DisplayName:    "Unusable Harness",
		Tier:           domain.TierBuiltin,
		Usable:         false,
		UnusableReason: "requires external opt-in",
	}
	unusableModule := &stubHarnessModule{
		ref: unusableHarness,
		descriptor: domain.HarnessDescriptor{
			ID:          "unusable-harness",
			DisplayName: "Unusable Harness",
		},
	}

	dummyStub := interactiontest.NewBuilder().Build()
	deps, _ := newTransformDeps(t, dummyStub)
	deps.Registry = &stubRegistry{
		list: []domain.HarnessRef{transformSourceHarness, transformTargetHarness, unusableHarness},
		modules: map[string]domain.HarnessModule{
			"source-harness":   newTransformSourceModule(),
			"target-harness":   newTransformTargetModule(),
			"unusable-harness": unusableModule,
		},
	}

	spy := newCapturingSpy(transformTargetHarness.ID)
	deps.Interaction = spy
	svc := app.New(deps)

	dir := t.TempDir()
	agentPath := writeTransformFile(t, dir, "my-agent.src.md", matchingAgentBytes())

	req := newPreAnsweredTransformRequest(agentPath)
	req.TargetHarnessID = ""

	// Act
	_, _ = svc.TransformHarness(context.Background(), req)

	if spy.capturedTargetHarnessQ == nil {
		t.Fatal("QTransformTargetHarness was not asked")
	}

	// Find the unusable harness option.
	var unusableOpt *domain.Option
	for i := range spy.capturedTargetHarnessQ.Options {
		if spy.capturedTargetHarnessQ.Options[i].ID == unusableHarness.ID {
			cp := spy.capturedTargetHarnessQ.Options[i]
			unusableOpt = &cp
			break
		}
	}

	if unusableOpt == nil {
		t.Fatalf("unusable harness option (ID=%q) not found in Options; "+
			"every registry harness must appear in the list regardless of Usable state",
			unusableHarness.ID)
	}

	// Assert: the unusable harness is disabled with its UnusableReason.
	if !unusableOpt.Disabled {
		t.Error("unusable harness option Disabled = false; want true when the harness is not Usable")
	}
	if unusableOpt.DisabledReason != unusableHarness.UnusableReason {
		t.Errorf("unusable harness option DisabledReason = %q, want %q (the harness UnusableReason); "+
			"the reason must be carried through so the user sees why the harness is unavailable",
			unusableOpt.DisabledReason, unusableHarness.UnusableReason)
	}
}

// TestTransformHarness_TargetHarnessQuestion_SourceAlsoUnusable_SourceReasonWins verifies
// that when the source harness is also not Usable, the source-harness DisabledReason takes
// precedence over the harness's own UnusableReason. The source role is the more specific
// explanation for why the harness cannot be picked here.
func TestTransformHarness_TargetHarnessQuestion_SourceAlsoUnusable_SourceReasonWins(t *testing.T) {
	// Arrange: source harness is both the transform source AND not Usable.
	unusableSourceRef := domain.HarnessRef{
		ID:             "source-harness",
		DisplayName:    "Source Harness",
		Tier:           domain.TierBuiltin,
		Usable:         false,
		UnusableReason: "load error: plugin not found",
	}
	unusableSourceModule := &stubHarnessModule{
		ref: unusableSourceRef,
		descriptor: domain.HarnessDescriptor{
			ID:          "source-harness",
			DisplayName: "Source Harness",
			Frontmatter: domain.FrontmatterSpec{ModelKey: "src_model"},
			Extensions:  map[domain.ArtifactKind]string{domain.ArtifactAgent: ".src.md"},
		},
	}

	dummyStub := interactiontest.NewBuilder().Build()
	deps, _ := newTransformDeps(t, dummyStub)
	deps.Registry = &stubRegistry{
		list: []domain.HarnessRef{unusableSourceRef, transformTargetHarness},
		modules: map[string]domain.HarnessModule{
			"source-harness": unusableSourceModule,
			"target-harness": newTransformTargetModule(),
		},
	}

	spy := newCapturingSpy(transformTargetHarness.ID)
	deps.Interaction = spy
	svc := app.New(deps)

	dir := t.TempDir()
	agentPath := writeTransformFile(t, dir, "my-agent.src.md", matchingAgentBytes())

	req := newPreAnsweredTransformRequest(agentPath)
	req.TargetHarnessID = ""

	// Act
	_, _ = svc.TransformHarness(context.Background(), req)

	if spy.capturedTargetHarnessQ == nil {
		t.Fatal("QTransformTargetHarness was not asked")
	}

	// Find the source harness option (which is also unusable in this scenario).
	var srcOpt *domain.Option
	for i := range spy.capturedTargetHarnessQ.Options {
		if spy.capturedTargetHarnessQ.Options[i].ID == unusableSourceRef.ID {
			cp := spy.capturedTargetHarnessQ.Options[i]
			srcOpt = &cp
			break
		}
	}

	if srcOpt == nil {
		t.Fatal("source harness option not found in Options")
	}

	if !srcOpt.Disabled {
		t.Error("source-and-unusable harness option Disabled = false; want true")
	}

	// The DisabledReason must be the source-harness reason, NOT the UnusableReason.
	// If it equals UnusableReason, the implementation is using the wrong priority.
	if srcOpt.DisabledReason == unusableSourceRef.UnusableReason {
		t.Errorf("source harness DisabledReason = %q (equals UnusableReason); "+
			"want the source-harness reason to take precedence when the harness is both "+
			"the transform source and not Usable — the source role is the more specific explanation",
			srcOpt.DisabledReason)
	}
	if srcOpt.DisabledReason == "" {
		t.Error("source harness DisabledReason is empty; want a non-empty reason identifying " +
			"the harness as the transform source")
	}
}

// ---------------------------------------------------------------------------
// T1.2: Unchanged surrounding behaviour
// ---------------------------------------------------------------------------

// TestTransformHarness_PreAnsweredTargetHarness_QuestionNotAsked verifies that when
// TargetHarnessID is pre-answered, SelectOne is never called for QTransformTargetHarness
// and therefore no options are built. This is the pre-answer short-circuit path (CD-6).
//
// NOT RED-VERIFIED: this test asserts a purely negative condition ("question was not asked").
// The current implementation already short-circuits correctly when TargetHarnessID is set,
// so the test passes in the RED phase for the right reason. Verify after implementation
// that it stays green once the option-building code is added to the interactive path.
func TestTransformHarness_PreAnsweredTargetHarness_QuestionNotAsked(t *testing.T) {
	// Arrange: spy that records any SelectOne call for QTransformTargetHarness.
	spy := &capturingInteractionSpy{
		base:                interactiontest.NewBuilder().Build(),
		targetHarnessAnswer: domain.ChoiceAnswer{Status: domain.Answered, OptionID: transformTargetHarness.ID},
	}
	dummyStub := interactiontest.NewBuilder().Build()
	deps, _ := newTransformDeps(t, dummyStub)
	deps.Interaction = spy
	svc := app.New(deps)

	dir := t.TempDir()
	agentPath := writeTransformFile(t, dir, "my-agent.src.md", matchingAgentBytes())

	req := newPreAnsweredTransformRequest(agentPath)
	// TargetHarnessID is non-empty in newPreAnsweredTransformRequest — the pre-answered path.

	// Act
	_, _ = svc.TransformHarness(context.Background(), req)

	// Assert: spy must not have captured a question.
	if spy.capturedTargetHarnessQ != nil {
		t.Error("QTransformTargetHarness was asked despite TargetHarnessID being pre-answered; " +
			"the pre-answered path must skip SelectOne entirely — no options are built")
	}
}

// TestTransformHarness_SameHarnessPreAnswered_RejectsBeforeQuestion verifies that the
// whole-run pre-answer same-harness check fires before any question is asked. The new
// option-building code must not bypass or interfere with this check.
//
// NOT RED-VERIFIED: the pre-answer same-harness guard already exists and fires
// correctly today. The test passes in the RED phase for the right reason. Verify after
// implementation that it stays green once the option-building code is in place.
func TestTransformHarness_SameHarnessPreAnswered_RejectsBeforeQuestion(t *testing.T) {
	// Arrange: source and target pre-answered to the same harness ID.
	spy := &capturingInteractionSpy{
		base:                interactiontest.NewBuilder().Build(),
		targetHarnessAnswer: domain.ChoiceAnswer{Status: domain.Answered, OptionID: transformSourceHarness.ID},
	}
	dummyStub := interactiontest.NewBuilder().Build()
	deps, _ := newTransformDeps(t, dummyStub)
	deps.Interaction = spy
	svc := app.New(deps)

	dir := t.TempDir()
	agentPath := writeTransformFile(t, dir, "my-agent.src.md", matchingAgentBytes())

	req := newPreAnsweredTransformRequest(agentPath)
	req.TargetHarnessID = req.SourceHarnessID // same → pre-answer guard must reject immediately

	// Act
	_, err := svc.TransformHarness(context.Background(), req)

	// Assert: ErrTransformSameHarness is returned.
	if err == nil {
		t.Fatal("TransformHarness returned nil error when source == target (pre-answered); " +
			"want error wrapping ErrTransformSameHarness")
	}
	if !errors.Is(err, app.ErrTransformSameHarness) {
		t.Errorf("error = %v; want errors.Is(err, ErrTransformSameHarness) = true", err)
	}

	// No question was asked — the pre-answer check fires before SelectOne is called.
	if spy.capturedTargetHarnessQ != nil {
		t.Error("QTransformTargetHarness was asked despite the pre-answer same-harness check; " +
			"the check must fire before any SelectOne call so no options are built unnecessarily")
	}
}

// TestTransformHarness_InteractiveAnswer_SameAsSource_ReturnsErrTransformSameHarness verifies
// that the post-interactive same-harness check fires when the interactive answer to
// QTransformTargetHarness resolves to the source harness ID. This is distinct from the
// pre-answer check: the question IS asked (and its options should be populated by the fix),
// and then the answer is validated.
//
// This guard is a belt-and-suspenders check that must survive the fix. Although the source
// harness should be disabled in the option list, the post-answer check catches the case
// where the ID arrives through a CLI pre-answers path that bypasses TUI disabled-option
// enforcement.
func TestTransformHarness_InteractiveAnswer_SameAsSource_ReturnsErrTransformSameHarness(t *testing.T) {
	// Arrange: TargetHarnessID empty → interactive path; spy returns the source harness ID.
	spy := &capturingInteractionSpy{
		base: interactiontest.NewBuilder().Build(),
		targetHarnessAnswer: domain.ChoiceAnswer{
			Status:   domain.Answered,
			OptionID: transformSourceHarness.ID, // same as source → post-answer guard must reject
		},
	}
	dummyStub := interactiontest.NewBuilder().Build()
	deps, _ := newTransformDeps(t, dummyStub)
	deps.Interaction = spy
	svc := app.New(deps)

	dir := t.TempDir()
	agentPath := writeTransformFile(t, dir, "my-agent.src.md", matchingAgentBytes())

	req := newPreAnsweredTransformRequest(agentPath)
	req.TargetHarnessID = "" // unset → triggers interactive path → spy returns source harness ID

	// Act
	_, err := svc.TransformHarness(context.Background(), req)

	// Assert: the question was asked (spy captured it).
	if spy.capturedTargetHarnessQ == nil {
		t.Fatal("QTransformTargetHarness was not asked; want the service to call SelectOne " +
			"when TargetHarnessID is empty, so the post-answer same-harness check has a chance to fire")
	}

	// Assert: ErrTransformSameHarness returned after the interactive answer was validated.
	if err == nil {
		t.Fatal("TransformHarness returned nil error after interactive same-harness answer; " +
			"want error wrapping ErrTransformSameHarness — the post-interactive check must still fire")
	}
	if !errors.Is(err, app.ErrTransformSameHarness) {
		t.Errorf("error = %v; want errors.Is(err, ErrTransformSameHarness) = true", err)
	}
}
