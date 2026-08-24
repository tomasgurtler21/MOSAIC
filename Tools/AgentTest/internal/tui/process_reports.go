package tui

// process_reports.go implements the Update and View methods for the six new
// screens added for the "Process Test Reports" mode:
//
//   ScreenModeSelect    - "Run Tests" / "Process Test Reports" top-level choice
//   ScreenProcessSelect - "Store Reports" / "Generate Summary" sub-choice
//   ScreenStoreInput    - collect file/directory path for store operation
//   ScreenStoreResult   - display store outcome (counts, paths)
//   ScreenSummaryInput  - collect optional version filter for summary
//   ScreenSummaryResult - display summary outcome (paths written/updated)
//
// After any result screen, Enter or Esc returns to ScreenModeSelect.
// Selecting "Run Tests" from ScreenModeSelect enters the existing
// harness/suite-select flow unchanged.

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	tuicommon "mosaic-common/tui"

	"mosaic-agent-test/internal/resultstore"
	"mosaic-agent-test/internal/resultsummary"
	"mosaic-agent-test/internal/tui/screens"
)

// modeSelectItems are the two choices on the mode-select screen, in display
// order. Index 0 is "Run Tests"; index 1 is "Process Test Reports".
var modeSelectItems = []string{
	"Run Tests",
	"Process Test Reports",
}

// processSelectItems are the two choices on the process-select screen, in
// display order. Index 0 is "Store Reports"; index 1 is "Generate Summary".
var processSelectItems = []string{
	"Store Reports",
	"Generate Summary",
}

// ---------------------------------------------------------------------------
// ScreenModeSelect
// ---------------------------------------------------------------------------

// updateModeSelect handles cursor movement and selection on the mode-select
// screen. Enter selects the current item: "Run Tests" enters the existing
// harness/suite-select flow; "Process Test Reports" moves to process-select.
// Ctrl+C is handled by handleKey's global Cancel binding. "q" is handled here
// because GlobalKeys.Cancel only binds ctrl+c, not the "q" alias.
func (m Model) updateModeSelect(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case tuicommon.MatchesKey(msg, tuicommon.GlobalKeys.Down):
		if m.processReports.cursor < len(modeSelectItems)-1 {
			m.processReports.cursor++
		}
		return m, nil

	case tuicommon.MatchesKey(msg, tuicommon.GlobalKeys.Up):
		if m.processReports.cursor > 0 {
			m.processReports.cursor--
		}
		return m, nil

	case tuicommon.MatchesKey(msg, tuicommon.GlobalKeys.Select):
		switch m.processReports.cursor {
		case 0: // "Run Tests"
			// Enter the existing run flow: harness-select (if catalog present)
			// or suite-select (if no catalog), exactly as before Stage 5.
			if len(m.opts.Harnesses) > 0 {
				m.screen = ScreenHarnessSelect
			} else {
				m.screen = ScreenSuiteSelect
			}
		case 1: // "Process Test Reports"
			m.processReports.cursor = 0 // reset cursor for process-select screen
			m.screen = ScreenProcessSelect
		}
		return m, nil

	case msg.Type == tea.KeyRunes && len(msg.Runes) == 1 && msg.Runes[0] == 'q':
		// "q" is a conventional quit alias. GlobalKeys.Cancel only covers ctrl+c,
		// so we handle "q" explicitly here to satisfy the I5.4 requirement.
		cancel := m.cancel
		return m, func() tea.Msg {
			if cancel != nil {
				cancel()
			}
			return tea.Quit()
		}
	}
	return m, nil
}

// viewModeSelect renders the mode-selection screen.
func (m Model) viewModeSelect() string {
	width := m.contentWidth()

	var b strings.Builder
	for i, item := range modeSelectItems {
		prefix := "  "
		if i == m.processReports.cursor {
			prefix = "> "
		}
		if i > 0 {
			b.WriteString("\n")
		}
		// Disable "Process Test Reports" when neither Store nor Summary is wired.
		label := item
		if i == 1 && m.opts.Store == nil && m.opts.Summary == nil {
			label = item + " (unavailable)"
		}
		b.WriteString(tuicommon.Truncate(prefix+label, width))
	}

	return m.renderScreen("AgentTest", "Select mode", b.String(), tuicommon.EntryScreenHelp())
}

// ---------------------------------------------------------------------------
// ScreenProcessSelect
// ---------------------------------------------------------------------------

// updateProcessSelect handles cursor movement and selection on the
// process-select screen. Enter selects the current item; Esc returns to
// mode-select.
func (m Model) updateProcessSelect(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case tuicommon.MatchesKey(msg, tuicommon.GlobalKeys.Down):
		if m.processReports.cursor < len(processSelectItems)-1 {
			m.processReports.cursor++
		}
		return m, nil

	case tuicommon.MatchesKey(msg, tuicommon.GlobalKeys.Up):
		if m.processReports.cursor > 0 {
			m.processReports.cursor--
		}
		return m, nil

	case tuicommon.MatchesKey(msg, tuicommon.GlobalKeys.Select):
		switch m.processReports.cursor {
		case 0: // "Store Reports"
			m.processReports.mode = "store"
			var styles screens.Styles
			m.processReports.storeInput = screens.NewStoreInputScreen("", m.width, styles)
			m.screen = ScreenStoreInput
		case 1: // "Generate Summary"
			m.processReports.mode = "summary"
			var styles screens.Styles
			m.processReports.summaryInput = screens.NewSummaryInputScreen("", m.width, styles)
			m.screen = ScreenSummaryInput
		}
		return m, nil

	case tuicommon.MatchesKey(msg, tuicommon.GlobalKeys.Back):
		m.processReports.cursor = 0
		m.screen = ScreenModeSelect
		return m, nil

	case msg.Type == tea.KeyBackspace:
		// Backspace is a conventional back alias. GlobalKeys.Back only covers esc,
		// so we handle Backspace explicitly here to satisfy the I5.5 requirement.
		m.processReports.cursor = 0
		m.screen = ScreenModeSelect
		return m, nil
	}
	return m, nil
}

// viewProcessSelect renders the process-select screen.
func (m Model) viewProcessSelect() string {
	width := m.contentWidth()

	var b strings.Builder
	for i, item := range processSelectItems {
		prefix := "  "
		if i == m.processReports.cursor {
			prefix = "> "
		}
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString(tuicommon.Truncate(prefix+item, width))
	}

	return m.renderScreen("Process Test Reports", "", b.String(), tuicommon.EntryScreenHelp())
}

// ---------------------------------------------------------------------------
// ScreenStoreInput
// ---------------------------------------------------------------------------

// updateStoreInput forwards key messages to the StoreInputScreen. When Done,
// it launches the store operation in a goroutine (via tea.Cmd) and transitions
// to ScreenStoreResult. When Back, it returns to ScreenProcessSelect.
func (m Model) updateStoreInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.processReports.storeInput == nil {
		return m, nil
	}
	cmd := m.processReports.storeInput.Update(msg)
	if m.processReports.storeInput.Done() {
		path := m.processReports.storeInput.Path()
		m.processReports.storeInput.Reset()
		m.screen = ScreenStoreResult
		m.processReports.storeResult = nil
		m.processReports.storeErr = nil

		// Determine whether the path is a directory or a file by the trailing
		// indicator the user may have provided. In the TUI we treat a non-empty
		// path as either Dir (when explicitly constructed for a directory) or a
		// single file. We use Dir when the path does not look like a .json file.
		var req resultstore.StoreFromPathsRequest
		req.TestResultsRoot = m.opts.TestResultsRoot
		if strings.HasSuffix(path, ".json") {
			req.Files = []string{path}
		} else {
			req.Dir = path
		}

		storeFn := m.opts.Store
		return m, func() tea.Msg {
			if storeFn == nil {
				return StoreFinishedMsg{Err: fmt.Errorf("store operation is not available")}
			}
			result, err := storeFn(req)
			return StoreFinishedMsg{Result: result, Err: err}
		}
	}
	if m.processReports.storeInput.Back() {
		m.processReports.storeInput.Reset()
		m.processReports.cursor = 0
		m.screen = ScreenProcessSelect
	}
	return m, cmd
}

// viewStoreInput delegates to the StoreInputScreen's own View.
func (m Model) viewStoreInput() string {
	if m.processReports.storeInput == nil {
		return ""
	}
	return m.processReports.storeInput.View()
}

// handleStoreFinished carries the store operation's result into the model.
func (m Model) handleStoreFinished(msg StoreFinishedMsg) Model {
	if msg.Err != nil {
		m.processReports.storeErr = msg.Err
	} else {
		r := msg.Result
		m.processReports.storeResult = &r
	}
	return m
}

// ---------------------------------------------------------------------------
// ScreenStoreResult
// ---------------------------------------------------------------------------

// updateStoreResult handles key input on the store-result screen. Enter or
// Esc returns to mode-select.
func (m Model) updateStoreResult(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case tuicommon.MatchesKey(msg, tuicommon.GlobalKeys.Select),
		tuicommon.MatchesKey(msg, tuicommon.GlobalKeys.Back):
		m.processReports.storeResult = nil
		m.processReports.storeErr = nil
		m.processReports.cursor = 0
		m.screen = ScreenModeSelect
	}
	return m, nil
}

// viewStoreResult renders the store-result screen, showing counts and per-file
// detail from StoreResult.Reports (SourcePath, TargetPath, SkipReason).
func (m Model) viewStoreResult() string {
	width := m.contentWidth()
	var b strings.Builder

	if m.processReports.storeErr != nil {
		b.WriteString(tuicommon.Truncate("Error: "+m.processReports.storeErr.Error(), width))
	} else if m.processReports.storeResult != nil {
		r := m.processReports.storeResult
		b.WriteString(tuicommon.Truncate(r.SummaryLine(), width))

		for _, rep := range r.Reports {
			b.WriteString("\n")
			if rep.Skipped {
				b.WriteString(tuicommon.Truncate(
					fmt.Sprintf("  skipped  %s (%s: %s)", rep.SourcePath, rep.SkipReason, rep.Message),
					width))
			} else {
				b.WriteString(tuicommon.Truncate(
					fmt.Sprintf("  stored   %s -> %s", rep.SourcePath, rep.TargetPath),
					width))
			}
		}
	} else {
		b.WriteString("Storing reports...")
	}

	help := []tuicommon.HelpEntry{
		{Key: tuicommon.GlobalKeys.Select.Help().Key, Desc: "return to menu"},
		{Key: tuicommon.GlobalKeys.Cancel.Help().Key, Desc: tuicommon.GlobalKeys.Cancel.Help().Desc},
	}
	return m.renderScreen("Store Reports", "Result", b.String(), help)
}

// ---------------------------------------------------------------------------
// ScreenSummaryInput
// ---------------------------------------------------------------------------

// updateSummaryInput forwards key messages to the SummaryInputScreen. When
// Done, it launches the summary generation in a goroutine (via tea.Cmd) and
// transitions to ScreenSummaryResult. When Back, it returns to
// ScreenProcessSelect.
func (m Model) updateSummaryInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.processReports.summaryInput == nil {
		return m, nil
	}
	cmd := m.processReports.summaryInput.Update(msg)
	if m.processReports.summaryInput.Done() {
		filter := m.processReports.summaryInput.VersionFilter()
		m.processReports.summaryInput.Reset()
		m.screen = ScreenSummaryResult
		m.processReports.summaryResult = nil
		m.processReports.summaryErr = nil

		req := resultsummary.SummaryRequest{
			TestResultsRoot: m.opts.TestResultsRoot,
			VersionFilter:   filter,
		}

		summaryFn := m.opts.Summary
		return m, func() tea.Msg {
			if summaryFn == nil {
				return SummaryFinishedMsg{Err: fmt.Errorf("summary operation is not available")}
			}
			result, err := summaryFn(req)
			return SummaryFinishedMsg{Result: result, Err: err}
		}
	}
	if m.processReports.summaryInput.Back() {
		m.processReports.summaryInput.Reset()
		m.processReports.cursor = 0
		m.screen = ScreenProcessSelect
	}
	return m, cmd
}

// viewSummaryInput delegates to the SummaryInputScreen's own View.
func (m Model) viewSummaryInput() string {
	if m.processReports.summaryInput == nil {
		return ""
	}
	return m.processReports.summaryInput.View()
}

// handleSummaryFinished carries the summary generation's result into the model.
func (m Model) handleSummaryFinished(msg SummaryFinishedMsg) Model {
	if msg.Err != nil {
		m.processReports.summaryErr = msg.Err
	} else {
		r := msg.Result
		m.processReports.summaryResult = &r
	}
	return m
}

// ---------------------------------------------------------------------------
// ScreenSummaryResult
// ---------------------------------------------------------------------------

// updateSummaryResult handles key input on the summary-result screen. Enter
// or Esc returns to mode-select.
func (m Model) updateSummaryResult(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case tuicommon.MatchesKey(msg, tuicommon.GlobalKeys.Select),
		tuicommon.MatchesKey(msg, tuicommon.GlobalKeys.Back):
		m.processReports.summaryResult = nil
		m.processReports.summaryErr = nil
		m.processReports.cursor = 0
		m.screen = ScreenModeSelect
	}
	return m, nil
}

// viewSummaryResult renders the summary-result screen, showing which
// summary.md files were written or updated.
func (m Model) viewSummaryResult() string {
	width := m.contentWidth()
	var b strings.Builder

	if m.processReports.summaryErr != nil {
		b.WriteString(tuicommon.Truncate("Error: "+m.processReports.summaryErr.Error(), width))
	} else if m.processReports.summaryResult != nil {
		r := m.processReports.summaryResult
		total := len(r.FilesWritten) + len(r.FilesUpdated)
		if total == 0 {
			b.WriteString("No summary files written (TestResults/ may be empty).")
		} else {
			b.WriteString(fmt.Sprintf("%d summary file(s) written or updated.", total))
			for _, f := range r.FilesWritten {
				b.WriteString("\n")
				b.WriteString(tuicommon.Truncate("  wrote   "+f, width))
			}
			for _, f := range r.FilesUpdated {
				b.WriteString("\n")
				b.WriteString(tuicommon.Truncate("  updated "+f, width))
			}
		}
	} else {
		b.WriteString("Generating summary...")
	}

	help := []tuicommon.HelpEntry{
		{Key: tuicommon.GlobalKeys.Select.Help().Key, Desc: "return to menu"},
		{Key: tuicommon.GlobalKeys.Cancel.Help().Key, Desc: tuicommon.GlobalKeys.Cancel.Help().Desc},
	}
	return m.renderScreen("Generate Summary", "Result", b.String(), help)
}
