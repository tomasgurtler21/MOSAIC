package tui

// multiselect_routing_test.go verifies two related concerns:
//
//  1. Routing: that the three SelectMany question IDs (QWorkflows, QUtilityAgents,
//     QHooks) each open their dedicated multi-select screen instead of the generic
//     inlineSelectOne fallback, and that unrecognised SelectMany IDs still reach
//     the generic fallback exactly as before.
//
//  2. Round-trip: that the multi-select screens deliver every selected ID through
//     the reply channel as a MultiChoiceAnswer, and that confirming with nothing
//     selected produces a skip answer while back/ESC produces a cancelled answer.
//
// Tests are in the internal tui package so they can inspect unexported rootModel
// fields and drive key events through m.Update.

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"mosaic-deploy/internal/domain"
)

// ---------------------------------------------------------------------------
// Helpers for SelectMany questions
// ---------------------------------------------------------------------------

// buildSelectManyMsg constructs a questionMsg for a SelectMany question with the
// given question ID, options, and a buffered reply channel.
func buildSelectManyMsg(id domain.QuestionID, options []domain.Option) questionMsg {
	return questionMsg{
		kind: questionSelectMany,
		choiceQ: domain.ChoiceQuestion{
			Question: domain.Question{
				ID:        id,
				Title:     "Select items",
				AllowSkip: true,
			},
			Options: options,
		},
		reply: make(chan answerMsg, 1),
	}
}

// workflowOptions returns a set of options whose Group fields produce two
// categories ("Build" with two workflows, "Test" with one) when passed through
// optionsToWorkflowCategories.
func workflowOptions() []domain.Option {
	return []domain.Option{
		{ID: "build-ci", Label: "CI Pipeline", Description: "Runs CI checks", Group: "Build"},
		{ID: "build-cd", Label: "CD Pipeline", Description: "Deploys to staging", Group: "Build"},
		{ID: "test-unit", Label: "Unit Tests", Description: "Runs unit tests", Group: "Test"},
	}
}

// agentOptions returns two utility-agent options.
func agentOptions() []domain.Option {
	return []domain.Option{
		{ID: "code-reviewer", Label: "Code Reviewer", Description: "Reviews code quality"},
		{ID: "doc-writer", Label: "Documentation Writer", Description: "Writes documentation"},
	}
}

// hookOptions returns two hook-bundle options.
func hookOptions() []domain.Option {
	return []domain.Option{
		{ID: "pre-commit", Label: "pre-commit", Description: "Pre-commit checks"},
		{ID: "post-merge", Label: "post-merge", Description: "Post-merge actions"},
	}
}

// ---------------------------------------------------------------------------
// Routing: QWorkflows
// ---------------------------------------------------------------------------

// TestMultiSelectRouting_QWorkflows_DoesNotUseGenericFallback verifies that a
// QWorkflows SelectMany question does NOT activate the generic inlineSelectOne
// overlay. The dedicated WorkflowBrowserScreen must be used instead.
func TestMultiSelectRouting_QWorkflows_DoesNotUseGenericFallback(t *testing.T) {
	m := newRoutingModel()
	qMsg := buildSelectManyMsg(domain.QWorkflows, workflowOptions())

	m.Update(qMsg)

	if m.selectOverlay != nil {
		t.Error("selectOverlay is non-nil after QWorkflows SelectMany; " +
			"the generic inlineSelectOne must not handle this question — use the dedicated WorkflowBrowserScreen")
	}
	if m.screen != screenQuestion {
		t.Errorf("screen = %v after QWorkflows; want screenQuestion", m.screen)
	}
}

// TestMultiSelectRouting_QWorkflows_ViewShowsWorkflowBrowserTitle verifies that
// after a QWorkflows question the View output comes from WorkflowBrowserScreen,
// which renders the title "Select Workflows" (not the generic question title
// "Select items").
func TestMultiSelectRouting_QWorkflows_ViewShowsWorkflowBrowserTitle(t *testing.T) {
	m := newRoutingModel()
	qMsg := buildSelectManyMsg(domain.QWorkflows, workflowOptions())
	m.Update(qMsg)

	view := m.View()

	if !strings.Contains(view, "Select Workflows") {
		t.Errorf("View() after QWorkflows does not contain 'Select Workflows'; "+
			"the WorkflowBrowserScreen renders that title but the generic overlay does not:\n%s", view)
	}
}

// ---------------------------------------------------------------------------
// Routing: QUtilityAgents
// ---------------------------------------------------------------------------

// TestMultiSelectRouting_QUtilityAgents_DoesNotUseGenericFallback verifies that
// a QUtilityAgents SelectMany question routes to the dedicated UtilityAgentScreen,
// not the generic inlineSelectOne.
func TestMultiSelectRouting_QUtilityAgents_DoesNotUseGenericFallback(t *testing.T) {
	m := newRoutingModel()
	qMsg := buildSelectManyMsg(domain.QUtilityAgents, agentOptions())

	m.Update(qMsg)

	if m.selectOverlay != nil {
		t.Error("selectOverlay is non-nil after QUtilityAgents SelectMany; " +
			"the generic inlineSelectOne must not handle this question — use the dedicated UtilityAgentScreen")
	}
	if m.screen != screenQuestion {
		t.Errorf("screen = %v after QUtilityAgents; want screenQuestion", m.screen)
	}
}

// TestMultiSelectRouting_QUtilityAgents_ViewShowsUtilityAgentTitle verifies that
// the View output after a QUtilityAgents question contains "Select Utility Agents",
// which is the title rendered by UtilityAgentScreen.
func TestMultiSelectRouting_QUtilityAgents_ViewShowsUtilityAgentTitle(t *testing.T) {
	m := newRoutingModel()
	qMsg := buildSelectManyMsg(domain.QUtilityAgents, agentOptions())
	m.Update(qMsg)

	view := m.View()

	if !strings.Contains(view, "Select Utility Agents") {
		t.Errorf("View() after QUtilityAgents does not contain 'Select Utility Agents':\n%s", view)
	}
}

// ---------------------------------------------------------------------------
// Routing: QHooks
// ---------------------------------------------------------------------------

// TestMultiSelectRouting_QHooks_DoesNotUseGenericFallback verifies that a QHooks
// SelectMany question routes to the dedicated HookScreen, not the generic
// inlineSelectOne.
func TestMultiSelectRouting_QHooks_DoesNotUseGenericFallback(t *testing.T) {
	m := newRoutingModel()
	qMsg := buildSelectManyMsg(domain.QHooks, hookOptions())

	m.Update(qMsg)

	if m.selectOverlay != nil {
		t.Error("selectOverlay is non-nil after QHooks SelectMany; " +
			"the generic inlineSelectOne must not handle this question — use the dedicated HookScreen")
	}
	if m.screen != screenQuestion {
		t.Errorf("screen = %v after QHooks; want screenQuestion", m.screen)
	}
}

// TestMultiSelectRouting_QHooks_ViewShowsHookScreenTitle verifies that the View
// output after a QHooks question contains "Select Hook Bundles", the title
// rendered by HookScreen.
func TestMultiSelectRouting_QHooks_ViewShowsHookScreenTitle(t *testing.T) {
	m := newRoutingModel()
	qMsg := buildSelectManyMsg(domain.QHooks, hookOptions())
	m.Update(qMsg)

	view := m.View()

	if !strings.Contains(view, "Select Hook Bundles") {
		t.Errorf("View() after QHooks does not contain 'Select Hook Bundles':\n%s", view)
	}
}

// ---------------------------------------------------------------------------
// Routing: unrecognised SelectMany ID falls back to generic overlay
// ---------------------------------------------------------------------------

// TestMultiSelectRouting_UnrecognisedID_UsesGenericFallback verifies that a
// SelectMany question whose ID is not one of the three specialised IDs still
// falls through to the generic inlineSelectOne overlay. This preserves backward
// compatibility for any future or third-party question IDs.
func TestMultiSelectRouting_UnrecognisedID_UsesGenericFallback(t *testing.T) {
	m := newRoutingModel()
	// QHarness is a SelectOne question in the app layer, but its ID is not in
	// the specialised SelectMany routing table, making it a good stand-in for an
	// unrecognised ID in a SelectMany context.
	qMsg := buildSelectManyMsg(domain.QHarness, []domain.Option{
		{ID: "some-option", Label: "Some Option"},
	})

	m.Update(qMsg)

	if m.selectOverlay == nil {
		t.Error("selectOverlay is nil for an unrecognised SelectMany ID; " +
			"unrecognised IDs must fall back to the generic inlineSelectOne overlay")
	}
	if m.screen != screenQuestion {
		t.Errorf("screen = %v for unrecognised SelectMany; want screenQuestion", m.screen)
	}
}

// ---------------------------------------------------------------------------
// Round-trip: QWorkflows — multiple selections deliver all IDs
// ---------------------------------------------------------------------------

// TestMultiSelectRoundTrip_QWorkflows_MultipleSelectionsDeliverAllIDs verifies
// the core regression: when a user opens a workflow category, selects two
// workflows, returns to the category list, and confirms with Enter, the reply
// channel receives a MultiChoiceAnswer containing every selected ID — not just
// the first one.
//
// Key sequence for WorkflowBrowserScreen (Stage 6 scheme):
//   - Right: open the first category (Build)
//   - Space: select build-ci (cursor at 0)
//   - Down: move cursor to build-cd
//   - Space: select build-cd
//   - Esc: return to category list
//   - Enter: confirm the selection
func TestMultiSelectRoundTrip_QWorkflows_MultipleSelectionsDeliverAllIDs(t *testing.T) {
	m := newRoutingModel()
	qMsg := buildSelectManyMsg(domain.QWorkflows, workflowOptions())
	m.Update(qMsg)

	// Navigate: open Build category, select two workflows, confirm.
	m.Update(tea.KeyMsg{Type: tea.KeyRight})                               // open first category (Right opens folder)
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}})           // select build-ci
	m.Update(tea.KeyMsg{Type: tea.KeyDown})                                // move to build-cd
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}})           // select build-cd
	m.Update(tea.KeyMsg{Type: tea.KeyEsc})                                 // back to category list
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})                               // confirm (Enter confirms)

	if m.screen != screenRunning {
		t.Errorf("screen = %v after confirming workflow selection; want screenRunning", m.screen)
	}

	select {
	case ans := <-qMsg.reply:
		if ans.multiChoiceAns.Status != domain.Answered {
			t.Errorf("reply Status = %q; want Answered", ans.multiChoiceAns.Status)
		}
		ids := ans.multiChoiceAns.OptionIDs
		if len(ids) != 2 {
			t.Errorf("reply contains %d IDs %v; want 2 ([build-ci build-cd]) — "+
				"the multi-select round trip must deliver every selected ID, not truncate to one",
				len(ids), ids)
			return
		}
		if ids[0] != "build-ci" || ids[1] != "build-cd" {
			t.Errorf("reply IDs = %v; want [build-ci build-cd]", ids)
		}
	default:
		t.Error("reply channel has no message after confirming workflow selection; " +
			"the multi-select answer must be sent to the reply channel")
	}
}

// ---------------------------------------------------------------------------
// Round-trip: QUtilityAgents — multiple selections deliver all IDs
// ---------------------------------------------------------------------------

// TestMultiSelectRoundTrip_QUtilityAgents_MultipleSelectionsDeliverAllIDs
// verifies that selecting two utility agents and confirming delivers both agent
// keys in the MultiChoiceAnswer.
//
// Key sequence for UtilityAgentScreen:
//   - Space: select code-reviewer (cursor at 0)
//   - Down: move cursor to doc-writer
//   - Space: select doc-writer
//   - Enter: confirm
func TestMultiSelectRoundTrip_QUtilityAgents_MultipleSelectionsDeliverAllIDs(t *testing.T) {
	m := newRoutingModel()
	qMsg := buildSelectManyMsg(domain.QUtilityAgents, agentOptions())
	m.Update(qMsg)

	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}}) // select code-reviewer
	m.Update(tea.KeyMsg{Type: tea.KeyDown})                      // move to doc-writer
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}}) // select doc-writer
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})                     // confirm

	if m.screen != screenRunning {
		t.Errorf("screen = %v after confirming agent selection; want screenRunning", m.screen)
	}

	select {
	case ans := <-qMsg.reply:
		if ans.multiChoiceAns.Status != domain.Answered {
			t.Errorf("reply Status = %q; want Answered", ans.multiChoiceAns.Status)
		}
		ids := ans.multiChoiceAns.OptionIDs
		if len(ids) != 2 {
			t.Errorf("reply contains %d IDs %v; want 2 ([code-reviewer doc-writer]) — "+
				"both selected agents must appear in the answer",
				len(ids), ids)
			return
		}
		if ids[0] != "code-reviewer" || ids[1] != "doc-writer" {
			t.Errorf("reply IDs = %v; want [code-reviewer doc-writer]", ids)
		}
	default:
		t.Error("reply channel has no message after confirming agent selection")
	}
}

// ---------------------------------------------------------------------------
// Round-trip: QHooks — multiple selections deliver all IDs
// ---------------------------------------------------------------------------

// TestMultiSelectRoundTrip_QHooks_MultipleSelectionsDeliverAllIDs verifies that
// selecting two hook bundles and confirming delivers both bundle keys in the
// MultiChoiceAnswer.
//
// Key sequence for HookScreen:
//   - Space: select pre-commit (cursor at 0)
//   - Down: move cursor to post-merge
//   - Space: select post-merge
//   - Enter: confirm
func TestMultiSelectRoundTrip_QHooks_MultipleSelectionsDeliverAllIDs(t *testing.T) {
	m := newRoutingModel()
	qMsg := buildSelectManyMsg(domain.QHooks, hookOptions())
	m.Update(qMsg)

	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}}) // select pre-commit
	m.Update(tea.KeyMsg{Type: tea.KeyDown})                      // move to post-merge
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}}) // select post-merge
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})                     // confirm

	if m.screen != screenRunning {
		t.Errorf("screen = %v after confirming hook selection; want screenRunning", m.screen)
	}

	select {
	case ans := <-qMsg.reply:
		if ans.multiChoiceAns.Status != domain.Answered {
			t.Errorf("reply Status = %q; want Answered", ans.multiChoiceAns.Status)
		}
		ids := ans.multiChoiceAns.OptionIDs
		if len(ids) != 2 {
			t.Errorf("reply contains %d IDs %v; want 2 ([pre-commit post-merge])",
				len(ids), ids)
			return
		}
		if ids[0] != "pre-commit" || ids[1] != "post-merge" {
			t.Errorf("reply IDs = %v; want [pre-commit post-merge]", ids)
		}
	default:
		t.Error("reply channel has no message after confirming hook selection")
	}
}

// ---------------------------------------------------------------------------
// Round-trip: empty confirmation yields a skip answer
// ---------------------------------------------------------------------------

// TestMultiSelectRoundTrip_QUtilityAgents_ConfirmWithNoSelection_ProducesSkip
// verifies that confirming the UtilityAgentScreen without selecting any agent
// produces a MultiChoiceAnswer with Status=SkippedOne rather than Answered with
// an empty OptionIDs slice. The app layer checks Status == Answered to decide
// whether to apply selections, so an empty-slice Answered would silently deliver
// no agents while pretending the question was answered.
func TestMultiSelectRoundTrip_QUtilityAgents_ConfirmWithNoSelection_ProducesSkip(t *testing.T) {
	m := newRoutingModel()
	qMsg := buildSelectManyMsg(domain.QUtilityAgents, agentOptions())
	m.Update(qMsg)

	// Confirm without selecting anything.
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})

	if m.screen != screenRunning {
		t.Errorf("screen = %v after empty confirmation; want screenRunning", m.screen)
	}

	select {
	case ans := <-qMsg.reply:
		if ans.multiChoiceAns.Status != domain.SkippedOne {
			t.Errorf("reply Status = %q after confirming with no selection; want SkippedOne "+
				"(empty confirmation must not produce Answered with an empty slice — the app layer "+
				"distinguishes Answered from non-Answered to decide whether to use selections)",
				ans.multiChoiceAns.Status)
		}
	default:
		t.Error("reply channel has no message after empty confirmation on UtilityAgentScreen")
	}
}

// TestMultiSelectRoundTrip_QWorkflows_EnterWithNoSelection_ShowsValidationAndDoesNotAdvance
// verifies that pressing Enter at the category level of WorkflowBrowserScreen
// without selecting any workflow does not advance to screenRunning and does not
// send a reply. Instead the screen shows a transient validation message and stays
// on screenQuestion, giving the user a chance to select workflows or press Esc to
// cancel. This differs from UtilityAgentScreen and HookScreen, where Enter with
// no selection produces SkippedOne; WorkflowBrowserScreen enforces a non-empty
// selection guard at the screen level.
func TestMultiSelectRoundTrip_QWorkflows_EnterWithNoSelection_ShowsValidationAndDoesNotAdvance(t *testing.T) {
	m := newRoutingModel()
	qMsg := buildSelectManyMsg(domain.QWorkflows, workflowOptions())
	m.Update(qMsg)

	// Attempt to confirm at the category level with Enter, without opening any
	// folder or selecting any workflow.
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})

	// The screen must stay at screenQuestion — the validation guard blocks advancement.
	if m.screen != screenQuestion {
		t.Errorf("screen = %v after Enter with no workflow selection; want screenQuestion "+
			"(the empty-selection guard must keep the browser on screen)", m.screen)
	}

	// No reply must have been sent to the channel.
	select {
	case ans := <-qMsg.reply:
		t.Errorf("unexpected reply %+v after Enter with no workflow selection; "+
			"the screen must not dismiss when no workflows are selected", ans)
	default:
		// Correct: no reply sent.
	}
}

// TestMultiSelectRoundTrip_QHooks_ConfirmWithNoSelection_ProducesSkip verifies
// that confirming the HookScreen without selecting any bundle produces a skip
// answer. This covers the path where the harness supports hooks but the user
// chooses none.
func TestMultiSelectRoundTrip_QHooks_ConfirmWithNoSelection_ProducesSkip(t *testing.T) {
	m := newRoutingModel()
	qMsg := buildSelectManyMsg(domain.QHooks, hookOptions())
	m.Update(qMsg)

	// Confirm without selecting anything.
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})

	if m.screen != screenRunning {
		t.Errorf("screen = %v after empty hook confirmation; want screenRunning", m.screen)
	}

	select {
	case ans := <-qMsg.reply:
		if ans.multiChoiceAns.Status != domain.SkippedOne {
			t.Errorf("reply Status = %q after confirming hooks with no selection; want SkippedOne",
				ans.multiChoiceAns.Status)
		}
	default:
		t.Error("reply channel has no message after empty confirmation on HookScreen")
	}
}

// TestMultiSelectRoundTrip_QHooks_EmptyOptionList_SkipAnswerDelivered verifies
// that when no hook options are provided (harness does not support hooks or the
// catalog is empty), the HookScreen can be dismissed and produces a skip answer.
// The user must not be stuck on the screen.
func TestMultiSelectRoundTrip_QHooks_EmptyOptionList_SkipAnswerDelivered(t *testing.T) {
	m := newRoutingModel()
	qMsg := buildSelectManyMsg(domain.QHooks, nil) // no hooks at all
	m.Update(qMsg)

	// On the "not supported" or empty HookScreen, Enter advances past the screen.
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})

	if m.screen != screenRunning {
		t.Errorf("screen = %v after dismissing empty-hook screen; want screenRunning", m.screen)
	}

	select {
	case ans := <-qMsg.reply:
		// Require an explicitly non-Answered status. The app layer uses
		// Status==Answered as the sole signal that selections should be applied, so
		// any Answered reply — even with an empty or sentinel OptionIDs slice —
		// would be ambiguous. Only SkippedOne or Cancelled is correct here.
		if ans.multiChoiceAns.Status == domain.Answered {
			t.Errorf("reply Status = Answered (OptionIDs=%v) after dismissing empty-hook screen; "+
				"want SkippedOne or Cancelled — Answered is incorrect because the screen was dismissed "+
				"without a real selection and the app layer uses Status==Answered to decide whether "+
				"to apply selections",
				ans.multiChoiceAns.OptionIDs)
		}
	default:
		t.Error("reply channel has no message after dismissing empty-hook screen; " +
			"the user must not be stuck when no hooks are available")
	}
}

// ---------------------------------------------------------------------------
// Round-trip: back / ESC yields a cancelled answer
// ---------------------------------------------------------------------------

// TestMultiSelectRoundTrip_QUtilityAgents_Esc_ProducesCancelled verifies that
// pressing Esc on the UtilityAgentScreen sends a MultiChoiceAnswer with
// Status=Cancelled to the reply channel. The app layer resolves any non-Answered
// status to no selections, so Cancelled must not silently deliver partial
// selections made before pressing Esc.
func TestMultiSelectRoundTrip_QUtilityAgents_Esc_ProducesCancelled(t *testing.T) {
	m := newRoutingModel()
	qMsg := buildSelectManyMsg(domain.QUtilityAgents, agentOptions())
	m.Update(qMsg)

	// Select one agent, then cancel — partial selection must not be delivered.
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}}) // select code-reviewer
	m.Update(tea.KeyMsg{Type: tea.KeyEsc})                       // cancel

	if m.screen != screenRunning {
		t.Errorf("screen = %v after Esc on utility-agent screen; want screenRunning", m.screen)
	}

	select {
	case ans := <-qMsg.reply:
		if ans.multiChoiceAns.Status != domain.Cancelled {
			t.Errorf("reply Status = %q after Esc on utility-agent screen; want Cancelled "+
				"(Esc must not deliver partial selections)",
				ans.multiChoiceAns.Status)
		}
	default:
		t.Error("reply channel has no message after Esc on UtilityAgentScreen")
	}
}

// TestMultiSelectRoundTrip_QWorkflows_Esc_AtCategoryLevel_ProducesCancelled
// verifies that pressing Esc at the category-list level of WorkflowBrowserScreen
// cancels the question and sends Cancelled to the reply channel.
func TestMultiSelectRoundTrip_QWorkflows_Esc_AtCategoryLevel_ProducesCancelled(t *testing.T) {
	m := newRoutingModel()
	qMsg := buildSelectManyMsg(domain.QWorkflows, workflowOptions())
	m.Update(qMsg)

	// Press Esc while at the category level (before opening any folder).
	m.Update(tea.KeyMsg{Type: tea.KeyEsc})

	if m.screen != screenRunning {
		t.Errorf("screen = %v after Esc at workflow category level; want screenRunning", m.screen)
	}

	select {
	case ans := <-qMsg.reply:
		if ans.multiChoiceAns.Status != domain.Cancelled {
			t.Errorf("reply Status = %q after Esc at category level; want Cancelled",
				ans.multiChoiceAns.Status)
		}
	default:
		t.Error("reply channel has no message after Esc at workflow category level")
	}
}

// TestMultiSelectRoundTrip_QHooks_Esc_ProducesCancelled verifies that pressing
// Esc on the HookScreen sends a MultiChoiceAnswer with Status=Cancelled to the
// reply channel, even when one bundle was selected before pressing Esc. The
// same risk that Done()/Back() shape differences could silently prevent
// completion applies to the Back path — partial selections made before Esc must
// not be delivered.
func TestMultiSelectRoundTrip_QHooks_Esc_ProducesCancelled(t *testing.T) {
	m := newRoutingModel()
	qMsg := buildSelectManyMsg(domain.QHooks, hookOptions())
	m.Update(qMsg)

	// Select one hook bundle, then cancel — partial selection must not be delivered.
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}}) // select pre-commit
	m.Update(tea.KeyMsg{Type: tea.KeyEsc})                       // cancel

	if m.screen != screenRunning {
		t.Errorf("screen = %v after Esc on hook screen; want screenRunning", m.screen)
	}

	select {
	case ans := <-qMsg.reply:
		if ans.multiChoiceAns.Status != domain.Cancelled {
			t.Errorf("reply Status = %q after Esc on hook screen; want Cancelled "+
				"(Esc must not deliver the partial selection — the app layer resolves any "+
				"non-Answered status to no selections)",
				ans.multiChoiceAns.Status)
		}
	default:
		t.Error("reply channel has no message after Esc on HookScreen")
	}
}

// ---------------------------------------------------------------------------
// Resize propagation to new multi-select overlays
// ---------------------------------------------------------------------------

// TestMultiSelectRouting_QWorkflows_ResizeDoesNotPanic verifies that a
// WindowSizeMsg does not panic when the workflow browser overlay is active.
func TestMultiSelectRouting_QWorkflows_ResizeDoesNotPanic(t *testing.T) {
	m := newRoutingModel()
	m.Update(buildSelectManyMsg(domain.QWorkflows, workflowOptions()))

	defer func() {
		if r := recover(); r != nil {
			t.Errorf("WindowSizeMsg panicked with active workflow overlay: %v", r)
		}
	}()

	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
}

// TestMultiSelectRouting_QUtilityAgents_ResizeDoesNotPanic verifies that a
// WindowSizeMsg does not panic when the utility-agent overlay is active.
func TestMultiSelectRouting_QUtilityAgents_ResizeDoesNotPanic(t *testing.T) {
	m := newRoutingModel()
	m.Update(buildSelectManyMsg(domain.QUtilityAgents, agentOptions()))

	defer func() {
		if r := recover(); r != nil {
			t.Errorf("WindowSizeMsg panicked with active utility-agent overlay: %v", r)
		}
	}()

	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
}

// TestMultiSelectRouting_QHooks_ResizeDoesNotPanic verifies that a WindowSizeMsg
// does not panic when the hook overlay is active.
func TestMultiSelectRouting_QHooks_ResizeDoesNotPanic(t *testing.T) {
	m := newRoutingModel()
	m.Update(buildSelectManyMsg(domain.QHooks, hookOptions()))

	defer func() {
		if r := recover(); r != nil {
			t.Errorf("WindowSizeMsg panicked with active hook overlay: %v", r)
		}
	}()

	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
}
