package tui

// model_select_test.go covers the TUI's model-selection screen:
//
//   T12.1 - The model-selection screen lists the selected harness's catalog
//            models, and changing the harness changes the offered list.
//   T12.2 - A chosen subject model and stub model reach the started run's
//            runtime model values (preflight.Input.Overrides.SubjectModel /
//            StubModel), matching what the equivalent command-line invocation
//            produces.
//   T12.3 - The new screen is reachable and exitable within the existing
//            state machine, and quitting and cancelling still work from it.
//
// The model-selection screen (ScreenModelSelect) has two sequential phases
// on one screen constant:
//
//   - Subject phase: the user picks which model the agent under test runs on.
//   - Stub phase: the user picks which model every stub collaborator runs on.
//     The stub phase's list is the harness's catalog preceded by one entry
//     meaning "same as the subject model", which resolves to an empty
//     StubModel override.
//
// Navigation: harness_select → model_select (subject) → model_select (stub)
// → suite_select → progress → results → detail.
//
// Escape from the subject phase returns to harness_select; Escape from the
// stub phase returns to the subject phase. ctrl+c quits from either phase.
//
// When a harness has no catalog entry the screen offers an empty list and
// the user can press Enter through both phases without selecting anything,
// leaving both model overrides empty. An unwired catalog must not block the
// frontend.
//
// All tests are in package tui (not tui_test) so they can use the shared
// test helpers from testhelpers_test.go and construct Models without
// needing Update to have reached a given screen first.

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	commonharness "mosaic-common/harness"

	"mosaic-agent-test/internal/authoring"
	"mosaic-agent-test/internal/preflight"
)

// ---------------------------------------------------------------------------
// Fixture helpers
// ---------------------------------------------------------------------------

// twoModelCatalog returns a small, stable model catalog covering two of the
// CLI-backed harnesses, so tests do not depend on the real catalog's
// membership or ordering. The harness IDs match twoHarnessCatalog().
func twoModelCatalog() []commonharness.ModelCatalog {
	return []commonharness.ModelCatalog{
		{
			HarnessID:  "claude-code",
			IDs:        []string{"claude-opus-4-5", "claude-sonnet-4-5"},
			FormatHint: "claude-<model>-<version>",
		},
		{
			HarnessID:  "opencode",
			IDs:        []string{"openai/gpt-4o", "openai/gpt-4-turbo"},
			FormatHint: "provider/model-id",
		},
	}
}

// newModelSelectOptions extends newHarnessFixtureOptions with a model catalog,
// for tests that drive the full harness-select → model-select → suite-select
// flow.
func newModelSelectOptions(suites []string, runner *fakeSuiteRunner) Options {
	o := newHarnessFixtureOptions(suites, runner)
	o.Models = twoModelCatalog()
	return o
}

// advanceToModelSelect drives m from ScreenHarnessSelect to ScreenModelSelect
// (subject phase) by selecting the first offered harness. The test is failed
// and the helper returns early if the screen does not reach ScreenModelSelect.
func advanceToModelSelect(t *testing.T, m Model) Model {
	t.Helper()
	if m.Screen() != ScreenHarnessSelect {
		t.Fatalf("advanceToModelSelect: Screen() = %q, want %q (must start at harness-select)", m.Screen(), ScreenHarnessSelect)
	}
	m, _ = safeUpdate(t, m, keyMsg("\r")) // select the first harness
	if m.Screen() != ScreenModelSelect {
		t.Fatalf("advanceToModelSelect: Screen() after harness selection = %q, want %q", m.Screen(), ScreenModelSelect)
	}
	return m
}

// advanceThroughModelSelect drives m through both model-select phases by
// pressing Enter twice (accepting the default at each phase), so tests that
// care about suite-select or run behaviour can reach those screens without
// duplicating the model-select navigation.
func advanceThroughModelSelect(t *testing.T, m Model) Model {
	t.Helper()
	m, _ = safeUpdate(t, m, keyMsg("\r")) // subject phase: accept default (cursor position 0)
	m, _ = safeUpdate(t, m, keyMsg("\r")) // stub phase: accept default ("same as subject")
	return m
}

// ---------------------------------------------------------------------------
// T12.1: catalog listing
// ---------------------------------------------------------------------------

// TestModelSelect_SubjectPhase_ShowsHarnessCatalogModels asserts that the
// model-selection screen's subject phase renders the currently selected
// harness's catalog model IDs so the user can see what is available to pick.
func TestModelSelect_SubjectPhase_ShowsHarnessCatalogModels(t *testing.T) {
	m := NewModel(newModelSelectOptions([]string{"suite-a.yaml"}, newFakeSuiteRunner()))
	m = advanceToModelSelect(t, m)

	view := safeView(t, m)
	for _, id := range twoModelCatalog()[0].IDs { // first harness is "claude-code"
		if !strings.Contains(view, id) {
			t.Errorf("model-select subject-phase view does not contain catalog model %q for the selected harness:\n%s", id, view)
		}
	}
}

// TestModelSelect_StubPhase_ShowsHarnessCatalogModels asserts that the stub
// phase also lists the selected harness's models (in addition to the leading
// "same as subject" entry) so the user can choose an explicit stub model.
func TestModelSelect_StubPhase_ShowsHarnessCatalogModels(t *testing.T) {
	m := NewModel(newModelSelectOptions([]string{"suite-a.yaml"}, newFakeSuiteRunner()))
	m = advanceToModelSelect(t, m)

	// Advance to stub phase by pressing Enter on the subject phase.
	m, _ = safeUpdate(t, m, keyMsg("\r"))
	if m.Screen() != ScreenModelSelect {
		t.Fatalf("Screen() after subject-phase Enter = %q, want still on %q (stub phase)", m.Screen(), ScreenModelSelect)
	}

	view := safeView(t, m)
	for _, id := range twoModelCatalog()[0].IDs {
		if !strings.Contains(view, id) {
			t.Errorf("model-select stub-phase view does not contain catalog model %q:\n%s", id, view)
		}
	}
}

// TestModelSelect_StubPhase_IncludesSameAsSubjectEntry asserts that the stub
// phase's rendered list includes an entry meaning "same as the subject model"
// (position 0, resolving to an empty StubModel override), preceding the
// harness's own catalog models.
func TestModelSelect_StubPhase_IncludesSameAsSubjectEntry(t *testing.T) {
	m := NewModel(newModelSelectOptions([]string{"suite-a.yaml"}, newFakeSuiteRunner()))
	m = advanceToModelSelect(t, m)

	// Advance to stub phase.
	m, _ = safeUpdate(t, m, keyMsg("\r"))
	if m.Screen() != ScreenModelSelect {
		t.Fatalf("Screen() after subject-phase Enter = %q, want %q", m.Screen(), ScreenModelSelect)
	}

	view := safeView(t, m)
	viewLower := strings.ToLower(view)
	// The entry must indicate it resolves to the subject's choice. The exact
	// wording is the implementer's to choose; requiring at least one of these
	// words covers "same as subject", "use subject model", etc.
	if !strings.Contains(viewLower, "same") && !strings.Contains(viewLower, "subject") {
		t.Errorf("stub-phase view does not indicate a 'same as subject' entry (looked for 'same' or 'subject' in lower-cased view):\n%s", view)
	}
}

// TestModelSelect_ChangeHarness_OfferedListChanges asserts that when the user
// returns to harness-select and picks a different harness, the model-select
// screen offers that harness's catalog instead of the previously selected
// harness's catalog. A selection valid for harness A must not appear for
// harness B.
func TestModelSelect_ChangeHarness_OfferedListChanges(t *testing.T) {
	catalog := twoModelCatalog()
	m := NewModel(newModelSelectOptions([]string{"suite-a.yaml"}, newFakeSuiteRunner()))

	// Select the first harness ("claude-code") and reach model-select.
	m = advanceToModelSelect(t, m)
	// Note the first harness's models; they should not appear for harness B.

	// Escape from the subject phase to return to harness-select.
	m, _ = safeUpdate(t, m, keyType(tea.KeyEsc))
	if m.Screen() != ScreenHarnessSelect {
		t.Fatalf("Screen() after Escape from subject phase = %q, want %q", m.Screen(), ScreenHarnessSelect)
	}

	// Select the second harness ("opencode").
	m, _ = safeUpdate(t, m, keyType(tea.KeyDown))
	m, _ = safeUpdate(t, m, keyMsg("\r"))
	if m.Screen() != ScreenModelSelect {
		t.Fatalf("Screen() after selecting second harness = %q, want %q", m.Screen(), ScreenModelSelect)
	}

	secondView := safeView(t, m)

	// The first harness's models must NOT appear in the second harness's list.
	for _, id := range catalog[0].IDs {
		if strings.Contains(secondView, id) {
			t.Errorf("model-select view after switching to second harness still shows first harness's model %q; harness change must update the offered list:\n%s", id, secondView)
		}
	}
	// The second harness's models MUST appear.
	for _, id := range catalog[1].IDs {
		if !strings.Contains(secondView, id) {
			t.Errorf("model-select view after switching to second harness does not show second harness's model %q:\n%s", id, secondView)
		}
	}
}

// TestModelSelect_EmptyCatalog_AdvancesBothPhases asserts that when the
// selected harness has no model catalog entry (Options.Models is empty or
// contains no entry for the selected harness), the model-select screen offers
// an empty list and pressing Enter through both phases lands on
// ScreenSuiteSelect, so an unwired catalog does not block the frontend.
func TestModelSelect_EmptyCatalog_AdvancesBothPhases(t *testing.T) {
	// Build options with a harness catalog but no model catalog so the
	// selected harness has no known models.
	o := newHarnessFixtureOptions([]string{"suite-a.yaml"}, newFakeSuiteRunner())
	// o.Models is intentionally left empty.
	m := NewModel(o)

	m = advanceToModelSelect(t, m) // harness-select → model-select

	// Subject phase: Enter with no models → advances to stub phase.
	m, _ = safeUpdate(t, m, keyMsg("\r"))
	// Stub phase: Enter with no models → advances to suite-select.
	m, _ = safeUpdate(t, m, keyMsg("\r"))

	if m.Screen() != ScreenSuiteSelect {
		t.Errorf("Screen() after pressing Enter through both empty-catalog phases = %q, want %q (frontend must not block when catalog is absent)", m.Screen(), ScreenSuiteSelect)
	}
}

// TestModelSelect_SubjectPhase_DistinctFromStubPhase asserts that the subject
// phase renders differently from the stub phase, so the user can tell which
// selection they are making.
func TestModelSelect_SubjectPhase_DistinctFromStubPhase(t *testing.T) {
	m := NewModel(newModelSelectOptions([]string{"suite-a.yaml"}, newFakeSuiteRunner()))
	m = advanceToModelSelect(t, m)
	subjectView := safeView(t, m)

	m, _ = safeUpdate(t, m, keyMsg("\r")) // advance to stub phase
	stubView := safeView(t, m)

	if subjectView == stubView {
		t.Errorf("subject-phase view and stub-phase view are identical; the user must be able to distinguish which selection they are making:\n%s", subjectView)
	}
}

// ---------------------------------------------------------------------------
// T12.2: selections reach preflight.Input.Overrides
// ---------------------------------------------------------------------------

// TestModelSelect_ChosenSubjectModel_ReachesPreflightOverrides asserts that
// the model chosen on the subject phase reaches
// preflight.Input.Overrides.SubjectModel when a suite starts. This is the
// value the runner uses to configure the subject's model.
func TestModelSelect_ChosenSubjectModel_ReachesPreflightOverrides(t *testing.T) {
	catalog := twoModelCatalog()
	wantModel := catalog[0].IDs[1] // "claude-sonnet-4-5" — the second model, reachable via Down

	var captured preflight.Input
	runner := newFakeSuiteRunner()
	o := newModelSelectOptions([]string{"suite-a.yaml"}, runner)
	o.Preflight = func(in preflight.Input) (preflight.Plan, authoring.Report) {
		captured = in
		return fixturePlan("suite-under-test"), authoring.Report{}
	}

	m := NewModel(o)
	m = advanceToModelSelect(t, m)

	// Subject phase: move cursor down to pick the second catalog model.
	m, _ = safeUpdate(t, m, keyType(tea.KeyDown))
	m, _ = safeUpdate(t, m, keyMsg("\r")) // select subject model

	// Stub phase: accept the default ("same as subject", cursor position 0).
	m, _ = safeUpdate(t, m, keyMsg("\r"))

	if m.Screen() != ScreenSuiteSelect {
		t.Fatalf("Screen() after completing model-select = %q, want %q", m.Screen(), ScreenSuiteSelect)
	}

	// Start the suite through the full settings flow to trigger the preflight call.
	m, cmd := startSuiteFromSuiteSelect(t, m)
	if cmd != nil {
		_ = runCmd(t, cmd)
	}

	if captured.Overrides.SubjectModel != wantModel {
		t.Errorf("preflight.Input.Overrides.SubjectModel = %q, want %q (the model chosen on the subject phase)", captured.Overrides.SubjectModel, wantModel)
	}
}

// TestModelSelect_ChosenStubModel_ReachesPreflightOverrides asserts that
// selecting an explicit stub model (not the "same as subject" default) reaches
// preflight.Input.Overrides.StubModel.
func TestModelSelect_ChosenStubModel_ReachesPreflightOverrides(t *testing.T) {
	catalog := twoModelCatalog()
	// Moving down once past the "same as subject" entry reaches the first
	// catalog model.
	wantStub := catalog[0].IDs[0] // "claude-opus-4-5"

	var captured preflight.Input
	runner := newFakeSuiteRunner()
	o := newModelSelectOptions([]string{"suite-a.yaml"}, runner)
	o.Preflight = func(in preflight.Input) (preflight.Plan, authoring.Report) {
		captured = in
		return fixturePlan("suite-under-test"), authoring.Report{}
	}

	m := NewModel(o)
	m = advanceToModelSelect(t, m)

	// Subject phase: accept the first model (no cursor move needed).
	m, _ = safeUpdate(t, m, keyMsg("\r"))

	// Stub phase: move down once (past "same as subject") to select the first
	// catalog model as an explicit stub.
	m, _ = safeUpdate(t, m, keyType(tea.KeyDown))
	m, _ = safeUpdate(t, m, keyMsg("\r"))

	if m.Screen() != ScreenSuiteSelect {
		t.Fatalf("Screen() after completing model-select = %q, want %q", m.Screen(), ScreenSuiteSelect)
	}

	m, cmd := startSuiteFromSuiteSelect(t, m)
	if cmd != nil {
		_ = runCmd(t, cmd)
	}

	if captured.Overrides.StubModel != wantStub {
		t.Errorf("preflight.Input.Overrides.StubModel = %q, want %q (explicit stub model chosen in stub phase)", captured.Overrides.StubModel, wantStub)
	}
}

// TestModelSelect_DefaultStub_LeavesStubModelEmpty asserts that selecting the
// "same as subject" stub entry (position 0 in the stub phase, the default)
// leaves preflight.Input.Overrides.StubModel empty. This matches the CLI's
// "omit --stub-model" case and the tier map's documented fallback: the subject
// model is used for stubs when no explicit stub is requested.
func TestModelSelect_DefaultStub_LeavesStubModelEmpty(t *testing.T) {
	var captured preflight.Input
	runner := newFakeSuiteRunner()
	o := newModelSelectOptions([]string{"suite-a.yaml"}, runner)
	o.Preflight = func(in preflight.Input) (preflight.Plan, authoring.Report) {
		captured = in
		return fixturePlan("suite-under-test"), authoring.Report{}
	}

	m := NewModel(o)
	m = advanceToModelSelect(t, m)

	// Subject phase: accept first model (cursor at 0).
	m, _ = safeUpdate(t, m, keyMsg("\r"))
	// Stub phase: accept "same as subject" without moving the cursor.
	m, _ = safeUpdate(t, m, keyMsg("\r"))

	if m.Screen() != ScreenSuiteSelect {
		t.Fatalf("Screen() after completing model-select = %q, want %q", m.Screen(), ScreenSuiteSelect)
	}

	m, cmd := startSuiteFromSuiteSelect(t, m)
	if cmd != nil {
		_ = runCmd(t, cmd)
	}

	if captured.Overrides.StubModel != "" {
		t.Errorf("preflight.Input.Overrides.StubModel = %q, want empty string (same-as-subject entry resolves to absent --stub-model)", captured.Overrides.StubModel)
	}
}

// TestModelSelect_BothModels_ReachDistinctOverrideFields asserts that the
// subject model and stub model each reach a distinct field in
// preflight.Input.Overrides — SubjectModel and StubModel respectively — so
// both frontends name one field per value and cannot swap or conflate them.
func TestModelSelect_BothModels_ReachDistinctOverrideFields(t *testing.T) {
	catalog := twoModelCatalog()
	wantSubject := catalog[0].IDs[1] // second model (Down once on subject phase)
	wantStub := catalog[0].IDs[0]    // first catalog model (Down once on stub phase, past "same as subject")

	var captured preflight.Input
	runner := newFakeSuiteRunner()
	o := newModelSelectOptions([]string{"suite-a.yaml"}, runner)
	o.Preflight = func(in preflight.Input) (preflight.Plan, authoring.Report) {
		captured = in
		return fixturePlan("suite-under-test"), authoring.Report{}
	}

	m := NewModel(o)
	m = advanceToModelSelect(t, m)

	// Subject phase: pick the second model.
	m, _ = safeUpdate(t, m, keyType(tea.KeyDown))
	m, _ = safeUpdate(t, m, keyMsg("\r"))

	// Stub phase: skip "same as subject" and pick the first catalog model.
	m, _ = safeUpdate(t, m, keyType(tea.KeyDown))
	m, _ = safeUpdate(t, m, keyMsg("\r"))

	if m.Screen() != ScreenSuiteSelect {
		t.Fatalf("Screen() after completing model-select = %q, want %q", m.Screen(), ScreenSuiteSelect)
	}

	m, cmd := startSuiteFromSuiteSelect(t, m)
	if cmd != nil {
		_ = runCmd(t, cmd)
	}

	if captured.Overrides.SubjectModel != wantSubject {
		t.Errorf("preflight.Input.Overrides.SubjectModel = %q, want %q", captured.Overrides.SubjectModel, wantSubject)
	}
	if captured.Overrides.StubModel != wantStub {
		t.Errorf("preflight.Input.Overrides.StubModel = %q, want %q", captured.Overrides.StubModel, wantStub)
	}
}

// TestModelSelect_SubjectModel_ReachesHarnessID asserts that the harness
// selection made on harness-select (and carried through model-select) still
// reaches preflight.Input.HarnessID, so model selection does not silently
// discard the harness identity chosen before it.
func TestModelSelect_SubjectModel_ReachesHarnessID(t *testing.T) {
	catalog := twoHarnessCatalog()

	var captured preflight.Input
	runner := newFakeSuiteRunner()
	o := newModelSelectOptions([]string{"suite-a.yaml"}, runner)
	o.Preflight = func(in preflight.Input) (preflight.Plan, authoring.Report) {
		captured = in
		return fixturePlan("suite-under-test"), authoring.Report{}
	}

	m := NewModel(o)
	if m.Screen() != ScreenHarnessSelect {
		t.Fatalf("initial Screen() = %q, want %q", m.Screen(), ScreenHarnessSelect)
	}

	// Select the second harness.
	m, _ = safeUpdate(t, m, keyType(tea.KeyDown))
	m, _ = safeUpdate(t, m, keyMsg("\r")) // harness chosen → model-select

	// Proceed through both model-select phases with defaults.
	m, _ = safeUpdate(t, m, keyMsg("\r")) // subject phase
	m, _ = safeUpdate(t, m, keyMsg("\r")) // stub phase

	// Start the suite through the full settings flow.
	m, cmd := startSuiteFromSuiteSelect(t, m)
	if cmd != nil {
		_ = runCmd(t, cmd)
	}

	if captured.HarnessID != catalog[1].ID {
		t.Errorf("preflight.Input.HarnessID = %q, want %q — harness selection must survive through model-select", captured.HarnessID, catalog[1].ID)
	}
}

// ---------------------------------------------------------------------------
// T12.3: navigation
// ---------------------------------------------------------------------------

// TestNavigation_HarnessSelect_AdvancesToModelSelect asserts that selecting a
// harness on the harness-select screen advances to ScreenModelSelect (subject
// phase), not directly to ScreenSuiteSelect. The model-select screen is the
// new stage between harness selection and suite selection.
func TestNavigation_HarnessSelect_AdvancesToModelSelect(t *testing.T) {
	m := NewModel(newModelSelectOptions([]string{"suite-a.yaml"}, newFakeSuiteRunner()))
	if m.Screen() != ScreenHarnessSelect {
		t.Fatalf("initial Screen() = %q, want %q", m.Screen(), ScreenHarnessSelect)
	}

	m, _ = safeUpdate(t, m, keyMsg("\r")) // select the first harness

	if m.Screen() != ScreenModelSelect {
		t.Errorf("Screen() after harness selection = %q, want %q (model-select must be presented before suite-select)", m.Screen(), ScreenModelSelect)
	}
}

// TestNavigation_ModelSelect_SubjectEnter_AdvancesToStubPhase asserts that
// pressing Enter on the subject phase keeps the screen on ScreenModelSelect
// (now in stub phase) rather than moving straight to ScreenSuiteSelect.
// A second Enter then advances to ScreenSuiteSelect, confirming the two-phase
// structure.
func TestNavigation_ModelSelect_SubjectEnter_AdvancesToStubPhase(t *testing.T) {
	m := NewModel(newModelSelectOptions([]string{"suite-a.yaml"}, newFakeSuiteRunner()))
	m = advanceToModelSelect(t, m)

	// Subject phase Enter → should stay on ScreenModelSelect (stub phase).
	m, _ = safeUpdate(t, m, keyMsg("\r"))
	if m.Screen() != ScreenModelSelect {
		t.Errorf("Screen() after subject-phase Enter = %q, want still on %q (stub phase is second and must stay on the same screen)", m.Screen(), ScreenModelSelect)
	}

	// Stub phase Enter → should advance to ScreenSuiteSelect.
	m, _ = safeUpdate(t, m, keyMsg("\r"))
	if m.Screen() != ScreenSuiteSelect {
		t.Errorf("Screen() after stub-phase Enter = %q, want %q", m.Screen(), ScreenSuiteSelect)
	}
}

// TestNavigation_ModelSelect_Stub_Enter_AdvancesToSuiteSelect asserts the
// complete harness → subject → stub → suite-select navigation path.
func TestNavigation_ModelSelect_Stub_Enter_AdvancesToSuiteSelect(t *testing.T) {
	m := NewModel(newModelSelectOptions([]string{"suite-a.yaml"}, newFakeSuiteRunner()))
	m = advanceToModelSelect(t, m)
	m = advanceThroughModelSelect(t, m) // two Enters

	if m.Screen() != ScreenSuiteSelect {
		t.Errorf("Screen() after completing both model-select phases = %q, want %q", m.Screen(), ScreenSuiteSelect)
	}
}

// TestNavigation_ModelSelect_SubjectPhase_Escape_ReturnsToHarnessSelect
// asserts that Escape on the subject phase returns to ScreenHarnessSelect so
// the user can change their harness choice before committing to a model list.
func TestNavigation_ModelSelect_SubjectPhase_Escape_ReturnsToHarnessSelect(t *testing.T) {
	m := NewModel(newModelSelectOptions([]string{"suite-a.yaml"}, newFakeSuiteRunner()))
	m = advanceToModelSelect(t, m)

	m, _ = safeUpdate(t, m, keyType(tea.KeyEsc))

	if m.Screen() != ScreenHarnessSelect {
		t.Errorf("Screen() after Escape from subject phase = %q, want %q", m.Screen(), ScreenHarnessSelect)
	}
}

// TestNavigation_ModelSelect_StubPhase_Escape_ReturnsToSubjectPhase asserts
// that Escape on the stub phase returns to the subject phase (still on
// ScreenModelSelect) so the user can reconsider the subject model before
// locking in the stub. A subsequent Enter from the re-entered subject phase
// must advance to the stub phase again, not to suite-select.
func TestNavigation_ModelSelect_StubPhase_Escape_ReturnsToSubjectPhase(t *testing.T) {
	m := NewModel(newModelSelectOptions([]string{"suite-a.yaml"}, newFakeSuiteRunner()))
	m = advanceToModelSelect(t, m)

	// Advance to stub phase.
	m, _ = safeUpdate(t, m, keyMsg("\r"))
	if m.Screen() != ScreenModelSelect {
		t.Fatalf("Screen() after subject-phase Enter = %q, want %q — prerequisite for Escape test", m.Screen(), ScreenModelSelect)
	}

	// Escape from stub phase → back to subject phase.
	m, _ = safeUpdate(t, m, keyType(tea.KeyEsc))
	if m.Screen() != ScreenModelSelect {
		t.Fatalf("Screen() after Escape from stub phase = %q, want still on %q (subject phase)", m.Screen(), ScreenModelSelect)
	}

	// From the re-entered subject phase, Enter must go to stub phase, not to
	// suite-select. If it goes to suite-select, we skipped the stub phase.
	m, _ = safeUpdate(t, m, keyMsg("\r"))
	if m.Screen() != ScreenModelSelect {
		t.Errorf("Screen() after Enter from re-entered subject phase = %q, want still on %q (stub phase should be presented again)", m.Screen(), ScreenModelSelect)
	}

	// Now in stub phase, Enter must go to suite-select.
	m, _ = safeUpdate(t, m, keyMsg("\r"))
	if m.Screen() != ScreenSuiteSelect {
		t.Errorf("Screen() after Enter from stub phase (second time) = %q, want %q", m.Screen(), ScreenSuiteSelect)
	}
}

// TestNavigation_ModelSelect_SubjectEscape_ThenReselect_CanAdvance asserts
// that after escaping from subject phase to harness-select and then
// reselecting a harness, the user can proceed through model-select again
// without the screen being stuck.
func TestNavigation_ModelSelect_SubjectEscape_ThenReselect_CanAdvance(t *testing.T) {
	m := NewModel(newModelSelectOptions([]string{"suite-a.yaml"}, newFakeSuiteRunner()))
	m = advanceToModelSelect(t, m)

	// Escape to harness-select.
	m, _ = safeUpdate(t, m, keyType(tea.KeyEsc))
	if m.Screen() != ScreenHarnessSelect {
		t.Fatalf("Screen() after Escape = %q, want %q", m.Screen(), ScreenHarnessSelect)
	}

	// Reselect the same harness.
	m, _ = safeUpdate(t, m, keyMsg("\r"))
	if m.Screen() != ScreenModelSelect {
		t.Fatalf("Screen() after re-selecting harness = %q, want %q", m.Screen(), ScreenModelSelect)
	}

	// Must still be able to advance through both phases.
	m = advanceThroughModelSelect(t, m)
	if m.Screen() != ScreenSuiteSelect {
		t.Errorf("Screen() after advancing through model-select a second time = %q, want %q", m.Screen(), ScreenSuiteSelect)
	}
}

// TestNavigation_ModelSelect_SubjectPhase_CtrlC_Quits asserts that the shared
// Cancel binding (ctrl+c) produces a quit command from the model-select
// screen's subject phase, exactly as from every other screen.
func TestNavigation_ModelSelect_SubjectPhase_CtrlC_Quits(t *testing.T) {
	m := NewModel(newModelSelectOptions([]string{"suite-a.yaml"}, newFakeSuiteRunner()))
	m = advanceToModelSelect(t, m)

	_, cmd := safeUpdate(t, m, keyType(tea.KeyCtrlC))
	if cmd == nil {
		t.Error("ctrl+c from model-select subject phase produced no command; expected a quit command matching the shared Cancel binding")
	}
}

// TestNavigation_ModelSelect_StubPhase_CtrlC_Quits asserts that the shared
// Cancel binding produces a quit command from the stub phase as well.
func TestNavigation_ModelSelect_StubPhase_CtrlC_Quits(t *testing.T) {
	m := NewModel(newModelSelectOptions([]string{"suite-a.yaml"}, newFakeSuiteRunner()))
	m = advanceToModelSelect(t, m)
	m, _ = safeUpdate(t, m, keyMsg("\r")) // advance to stub phase

	_, cmd := safeUpdate(t, m, keyType(tea.KeyCtrlC))
	if cmd == nil {
		t.Error("ctrl+c from model-select stub phase produced no command; expected a quit command")
	}
}

// TestNavigation_ModelSelect_CursorMovement_DoesNotAdvanceScreen asserts that
// pressing Up/Down on the model-select screen moves the cursor without leaving
// the screen, so navigation keys are not accidentally treated as selection.
func TestNavigation_ModelSelect_CursorMovement_DoesNotAdvanceScreen(t *testing.T) {
	m := NewModel(newModelSelectOptions([]string{"suite-a.yaml"}, newFakeSuiteRunner()))
	m = advanceToModelSelect(t, m)

	m, _ = safeUpdate(t, m, keyType(tea.KeyDown))
	if m.Screen() != ScreenModelSelect {
		t.Errorf("Screen() after Down on subject phase = %q, want still on %q", m.Screen(), ScreenModelSelect)
	}

	m, _ = safeUpdate(t, m, keyType(tea.KeyUp))
	if m.Screen() != ScreenModelSelect {
		t.Errorf("Screen() after Up on subject phase = %q, want still on %q", m.Screen(), ScreenModelSelect)
	}
}

// TestNavigation_ModelSelect_RendersWithoutPanic is the safest structural
// check for a screen that has no implementation yet: View() must not panic
// in either phase, so a partial implementation cannot crash the test binary.
func TestNavigation_ModelSelect_RendersWithoutPanic(t *testing.T) {
	m := NewModel(newModelSelectOptions([]string{"suite-a.yaml"}, newFakeSuiteRunner()))
	m = advanceToModelSelect(t, m)
	_ = safeView(t, m) // subject phase

	m, _ = safeUpdate(t, m, keyMsg("\r")) // advance to stub phase
	_ = safeView(t, m)                    // stub phase
}

// TestModelSelect_ChangeHarness_PreviousSubjectModelSelectionCleared asserts
// that a subject model confirmed for harness A does not reach
// preflight.Input.Overrides.SubjectModel when the user subsequently changes to
// harness B and proceeds through model-select without making a new explicit
// selection. The design states: "Changing the harness re-derives the offered
// model list and clears any model already chosen, so a selection valid for the
// previous harness cannot survive into the run."
func TestModelSelect_ChangeHarness_PreviousSubjectModelSelectionCleared(t *testing.T) {
	catalog := twoModelCatalog()
	// Second model of harness A ("claude-code") — chosen explicitly before escape.
	harnessAModel := catalog[0].IDs[1] // "claude-sonnet-4-5"
	// First model of harness B ("opencode") — what cursor-0 on that harness's
	// subject phase would select when no cursor movement is made.
	harnessBFirstModel := catalog[1].IDs[0] // "anthropic/claude-opus-4-5"

	var captured preflight.Input
	runner := newFakeSuiteRunner()
	o := newModelSelectOptions([]string{"suite-a.yaml"}, runner)
	o.Preflight = func(in preflight.Input) (preflight.Plan, authoring.Report) {
		captured = in
		return fixturePlan("suite-under-test"), authoring.Report{}
	}

	m := NewModel(o)

	// Select harness A (first entry = "claude-code") and enter model-select.
	m, _ = safeUpdate(t, m, keyMsg("\r")) // harness-select: select first harness
	if m.Screen() != ScreenModelSelect {
		t.Fatalf("Screen() after harness A selection = %q, want %q", m.Screen(), ScreenModelSelect)
	}

	// Subject phase for harness A: move cursor to the second model and confirm.
	m, _ = safeUpdate(t, m, keyType(tea.KeyDown))
	m, _ = safeUpdate(t, m, keyMsg("\r")) // confirms harnessAModel → stub phase

	// Escape from stub phase back to subject phase, then from subject phase to
	// harness-select, so the harness can be changed before any model has been
	// definitively committed to a run.
	m, _ = safeUpdate(t, m, keyType(tea.KeyEsc)) // stub → subject phase
	m, _ = safeUpdate(t, m, keyType(tea.KeyEsc)) // subject phase → harness-select
	if m.Screen() != ScreenHarnessSelect {
		t.Fatalf("Screen() after double-Escape = %q, want %q", m.Screen(), ScreenHarnessSelect)
	}

	// Select harness B (second entry = "opencode").
	m, _ = safeUpdate(t, m, keyType(tea.KeyDown))
	m, _ = safeUpdate(t, m, keyMsg("\r")) // harness-select: select second harness
	if m.Screen() != ScreenModelSelect {
		t.Fatalf("Screen() after harness B selection = %q, want %q", m.Screen(), ScreenModelSelect)
	}

	// Proceed through both model-select phases with the default (no cursor
	// movement), accepting whatever cursor position 0 offers for harness B.
	m, _ = safeUpdate(t, m, keyMsg("\r")) // subject phase: accept default
	m, _ = safeUpdate(t, m, keyMsg("\r")) // stub phase: accept "same as subject"
	if m.Screen() != ScreenSuiteSelect {
		t.Fatalf("Screen() after completing model-select for harness B = %q, want %q", m.Screen(), ScreenSuiteSelect)
	}

	// Start the suite through the full settings flow to trigger the preflight call.
	m, cmd := startSuiteFromSuiteSelect(t, m)
	if cmd != nil {
		_ = runCmd(t, cmd)
	}

	// The captured subject model must not be harness A's selection.
	if captured.Overrides.SubjectModel == harnessAModel {
		t.Errorf("preflight.Input.Overrides.SubjectModel = %q — harness A's model selection survived a harness change and reached the run; the selection must be cleared when a new harness is chosen",
			harnessAModel)
	}
	// The captured subject model must be harness B's first model (cursor-0 default).
	if captured.Overrides.SubjectModel != harnessBFirstModel {
		t.Errorf("preflight.Input.Overrides.SubjectModel = %q, want %q (harness B's first model, the default at cursor position 0 after the harness change reset the selection)",
			captured.Overrides.SubjectModel, harnessBFirstModel)
	}
}

// TestNavigation_ModelSelect_SubjectEscape_ThenReselect_CursorResets asserts
// that after escaping from the model-select subject phase back to harness-select
// and then reselecting a harness, the model-select screen's cursor resets to
// position 0 rather than retaining the position from the previous entry. A
// sticky cursor would cause the wrong model to be selected silently when the
// user accepts the default without moving the cursor after re-entry.
func TestNavigation_ModelSelect_SubjectEscape_ThenReselect_CursorResets(t *testing.T) {
	catalog := twoModelCatalog()
	// The model at cursor position 0 for "claude-code" — what an immediate
	// Enter (no cursor movement) must select after re-entry if the cursor reset.
	firstModel := catalog[0].IDs[0] // "claude-opus-4-5"

	var captured preflight.Input
	runner := newFakeSuiteRunner()
	o := newModelSelectOptions([]string{"suite-a.yaml"}, runner)
	o.Preflight = func(in preflight.Input) (preflight.Plan, authoring.Report) {
		captured = in
		return fixturePlan("suite-under-test"), authoring.Report{}
	}

	m := NewModel(o)
	m = advanceToModelSelect(t, m)

	// Move the cursor to position 1 (second model) without confirming, then
	// escape — so the cursor was non-zero when the phase was abandoned.
	m, _ = safeUpdate(t, m, keyType(tea.KeyDown))
	m, _ = safeUpdate(t, m, keyType(tea.KeyEsc))
	if m.Screen() != ScreenHarnessSelect {
		t.Fatalf("Screen() after Escape = %q, want %q", m.Screen(), ScreenHarnessSelect)
	}

	// Reselect the same harness.
	m, _ = safeUpdate(t, m, keyMsg("\r"))
	if m.Screen() != ScreenModelSelect {
		t.Fatalf("Screen() after reselecting harness = %q, want %q", m.Screen(), ScreenModelSelect)
	}

	// Accept immediately with no cursor movement. If the cursor reset to 0, this
	// selects the first model. If the cursor is sticky (position 1), this selects
	// the second model — and the assertion below catches that.
	m, _ = safeUpdate(t, m, keyMsg("\r")) // subject phase: accept cursor position
	m, _ = safeUpdate(t, m, keyMsg("\r")) // stub phase: accept "same as subject"
	if m.Screen() != ScreenSuiteSelect {
		t.Fatalf("Screen() after model-select = %q, want %q", m.Screen(), ScreenSuiteSelect)
	}

	// Start the suite through the full settings flow.
	m, cmd := startSuiteFromSuiteSelect(t, m)
	if cmd != nil {
		_ = runCmd(t, cmd)
	}

	if captured.Overrides.SubjectModel != firstModel {
		t.Errorf("SubjectModel = %q after re-entry with no cursor movement, want %q (cursor position 0); a sticky cursor from the previous entry would select a different model silently",
			captured.Overrides.SubjectModel, firstModel)
	}
}

// TestNavigation_FullFlow_HarnessToResultsViaModelSelect asserts that the
// complete end-to-end flow — harness-select → model-select → suite-select →
// progress → results — reaches ScreenResults, so the inserted model-select
// screen does not break the rest of the navigation chain.
func TestNavigation_FullFlow_HarnessToResultsViaModelSelect(t *testing.T) {
	runner := newFakeSuiteRunner()
	m := NewModel(newModelSelectOptions([]string{"suite-a.yaml"}, runner))

	// Harness-select: choose first harness.
	m, _ = safeUpdate(t, m, keyMsg("\r"))
	if m.Screen() != ScreenModelSelect {
		t.Fatalf("Screen() after harness selection = %q, want %q", m.Screen(), ScreenModelSelect)
	}

	// Model-select: accept defaults through both phases.
	m = advanceThroughModelSelect(t, m)
	if m.Screen() != ScreenSuiteSelect {
		t.Fatalf("Screen() after model-select = %q, want %q", m.Screen(), ScreenSuiteSelect)
	}

	// Suite-select: select the first suite and run to completion.
	m = runSuiteToCompletion(t, m, runner)
	if m.Screen() != ScreenResults {
		t.Errorf("Screen() after suite completion = %q, want %q", m.Screen(), ScreenResults)
	}
}
