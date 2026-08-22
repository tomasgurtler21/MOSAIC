package tui

// screens.go renders the four screens (suite selection, live progress,
// results, test detail) from the folded Model, on the shared scaffold
// (title, status, help) and the shared key set (AC16.4).
//
// Screen layout is deliberately an implementation decision (see Plan.md);
// what is fixed is the shared theme, key bindings and scaffold this file
// forwards to rather than hand-rolling. Every dynamic string that could run
// long (a suite path, a test ID, an assertion detail) is truncated to the
// reported width before being joined into the view, so a narrow terminal
// degrades legibly instead of wrapping into noise (AC16.4).

import (
	"fmt"
	"strings"

	tuicommon "mosaic-common/tui"

	"mosaic-agent-test/internal/report"
)

// contentWidth is the width every screen renders against, defaulting to the
// shared scaffold's default when the program has not yet received a
// tea.WindowSizeMsg.
func (m Model) contentWidth() int {
	w := m.width
	if w <= 0 {
		w = tuicommon.DefaultWidth
	}
	return w
}

// renderScreen assembles one screen from the shared title, body, status and
// help sections.
func (m Model) renderScreen(title, subtitle, body string, help []tuicommon.HelpEntry) string {
	width := m.contentWidth()
	parts := []string{
		tuicommon.RenderTitle(title, subtitle, width, m.theme),
		body,
		tuicommon.RenderStatus(m.statusMsg, m.statusError, width, m.theme),
		tuicommon.RenderHelp(help, width, m.theme),
	}
	return strings.Join(parts, "\n")
}

// ---------------------------------------------------------------------------
// Harness selection
// ---------------------------------------------------------------------------

// viewHarnessSelect renders the harness-selection affordance — the TUI's
// equivalent of the CLI's --harness flag — following the same
// cursor-and-select layout as viewSuiteSelect.
func (m Model) viewHarnessSelect() string {
	width := m.contentWidth()

	var b strings.Builder
	if len(m.opts.Harnesses) == 0 {
		b.WriteString(tuicommon.Truncate("no harness catalog offered", width))
	}
	for i, h := range m.opts.Harnesses {
		prefix := "  "
		if i == m.harnessCursor {
			prefix = "> "
		}
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString(tuicommon.Truncate(prefix+h.Label, width))
	}

	return m.renderScreen("Select a Harness", "", b.String(), tuicommon.EntryScreenHelp())
}

// ---------------------------------------------------------------------------
// Model selection
// ---------------------------------------------------------------------------

// viewModelSelect renders the model-selection screen's two sequential phases:
// subject phase (choosing which model the agent under test runs on) and stub
// phase (choosing which model every stub runs on, with "same as subject" as the
// leading default option).
func (m Model) viewModelSelect() string {
	width := m.contentWidth()
	ids := m.currentModelIDs()

	var b strings.Builder

	switch m.modelPhase {
	case modelPhaseSubject:
		if len(ids) == 0 {
			b.WriteString(tuicommon.Truncate("no models available for this harness", width))
		}
		for i, id := range ids {
			prefix := "  "
			if i == m.modelCursor {
				prefix = "> "
			}
			if i > 0 {
				b.WriteString("\n")
			}
			b.WriteString(tuicommon.Truncate(prefix+id, width))
		}
		return m.renderScreen("Select Subject Model", "Model the agent under test runs on", b.String(), tuicommon.EntryScreenHelp())

	case modelPhaseStub:
		// Stub list: "same as subject" at position 0, then the catalog models.
		const sameAsSubject = "same as subject"
		stubList := make([]string, 0, 1+len(ids))
		stubList = append(stubList, sameAsSubject)
		stubList = append(stubList, ids...)
		for i, entry := range stubList {
			prefix := "  "
			if i == m.modelCursor {
				prefix = "> "
			}
			if i > 0 {
				b.WriteString("\n")
			}
			b.WriteString(tuicommon.Truncate(prefix+entry, width))
		}
		return m.renderScreen("Select Stub Model", "Model every stub collaborator runs on", b.String(), tuicommon.EntryScreenHelp())
	}

	return ""
}

// ---------------------------------------------------------------------------
// Suite selection
// ---------------------------------------------------------------------------

// suiteSelectHelp is EntryScreenHelp with a Tab entry (to reach the settings
// screen) and the detail-pane scroll entry prepended, so all affordances on
// this screen are discoverable via the help bar.
func suiteSelectHelp() []tuicommon.HelpEntry {
	tabEntry := tuicommon.HelpEntry{
		Key:  "tab",
		Desc: "configure run",
	}
	scrollEntry := tuicommon.HelpEntry{
		Key:  "pgup/pgdn",
		Desc: "scroll detail",
	}
	return append([]tuicommon.HelpEntry{tabEntry, scrollEntry}, tuicommon.EntryScreenHelp()...)
}

func (m Model) viewSuiteSelect() string {
	width := m.contentWidth()

	var b strings.Builder
	if len(m.opts.Suites) == 0 {
		b.WriteString(tuicommon.Truncate("no suites discovered", width))
	}
	for i, s := range m.opts.Suites {
		prefix := "  "
		if i == m.suiteCursor {
			prefix = "> "
		}
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString(tuicommon.Truncate(prefix+s, width))
	}

	body := b.String()
	if m.showFailureDetail && m.detailPane != nil {
		body = body + "\n" + m.detailPane.View()
	}

	return m.renderScreen("Select a Suite", "", body, suiteSelectHelp())
}

// ---------------------------------------------------------------------------
// Run configuration settings
// ---------------------------------------------------------------------------

// settingsHelp returns the help entries for the settings screen. All
// functional keys are listed so none is undiscoverable.
func settingsHelp() []tuicommon.HelpEntry {
	return []tuicommon.HelpEntry{
		{Key: "up/down", Desc: "move"},
		{Key: "enter", Desc: "change"},
		{Key: "esc", Desc: "back"},
		{Key: tuicommon.GlobalKeys.Cancel.Help().Key, Desc: tuicommon.GlobalKeys.Cancel.Help().Desc},
	}
}

// viewSettings renders the run-configuration settings screen. Each
// SettingsEntry is shown as a labelled row; the focused row is marked with a
// cursor prefix. When an inline or numeric editor is open for an entry, that
// entry shows the draft value in place of its current value.
func (m Model) viewSettings() string {
	width := m.contentWidth()
	entries := m.SettingsEntries()

	var b strings.Builder
	for i, e := range entries {
		if i > 0 {
			b.WriteString("\n")
		}
		prefix := "  "
		if i == m.settingsCursor {
			prefix = "> "
		}

		// When this entry's editor is open, show the draft instead of the
		// committed value.
		display := e.Display
		if m.settingEditing == e.Kind {
			display = m.settingDraft + "_"
		}

		b.WriteString(tuicommon.Truncate(fmt.Sprintf("%s%s: %s", prefix, e.Label, display), width))
	}

	return m.renderScreen("Run Configuration", "", b.String(), settingsHelp())
}

// ---------------------------------------------------------------------------
// Live progress
// ---------------------------------------------------------------------------

func (m Model) viewProgress() string {
	width := m.contentWidth()

	lines := []string{fmt.Sprintf("Total tests: %d", m.totalTests)}
	if testID, rep, reps, ok := m.Running(); ok {
		lines = append(lines, tuicommon.Truncate(
			fmt.Sprintf("Running: %s (repetition %d/%d)", testID, rep, reps), width))
		lines = append(lines, fmt.Sprintf("Invocations observed: %d", m.observedInvocations))
	} else {
		lines = append(lines, "Waiting for the next test to start...")
	}
	lines = append(lines, fmt.Sprintf("Finished: %d", len(m.finished)))

	body := strings.Join(lines, "\n")
	if m.showFailureDetail && m.detailPane != nil {
		body = body + "\n" + m.detailPane.View()
	}

	return m.renderScreen("Suite Progress", "", body, tuicommon.EntryScreenHelp())
}

// ---------------------------------------------------------------------------
// Results
// ---------------------------------------------------------------------------

// resultsHelp is EntryScreenHelp augmented with the pane-scroll entry, so the
// scroll affordance is discoverable on the results screen whenever the detail
// pane may be live.
func resultsHelp() []tuicommon.HelpEntry {
	scrollEntry := tuicommon.HelpEntry{
		Key:  "pgup/pgdn",
		Desc: "scroll detail",
	}
	return append([]tuicommon.HelpEntry{scrollEntry}, tuicommon.EntryScreenHelp()...)
}

func (m Model) viewResults() string {
	width := m.contentWidth()
	tests := m.resultTests()

	var b strings.Builder
	if len(tests) == 0 {
		b.WriteString(tuicommon.Truncate("no test results available", width))
	}
	for i, t := range tests {
		verdict := "?"
		if len(t.Runs) > 0 {
			verdict = string(t.Runs[len(t.Runs)-1].Verdict)
		}
		prefix := "  "
		if i == m.resultsCursor {
			prefix = "> "
		}
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString(tuicommon.Truncate(fmt.Sprintf("%s%-8s %s", prefix, verdict, t.TestID), width))
	}

	body := b.String()
	if m.showFailureDetail && m.detailPane != nil {
		body = body + "\n" + m.detailPane.View()
	}

	return m.renderScreen("Results", "", body, resultsHelp())
}

// ---------------------------------------------------------------------------
// Test detail
// ---------------------------------------------------------------------------

// detailHelp returns the key hints for the test-detail screen. It keeps only
// the essential entries so the help bar stays on one line and does not consume
// an extra line of screen height (which would push content off the bottom on
// short terminals). The scroll affordance is included per the spec.
func detailHelp() []tuicommon.HelpEntry {
	return []tuicommon.HelpEntry{
		{Key: "pgup/pgdn", Desc: "scroll"},
		{Key: tuicommon.GlobalKeys.Back.Help().Key, Desc: tuicommon.GlobalKeys.Back.Help().Desc},
		{Key: tuicommon.GlobalKeys.Cancel.Help().Key, Desc: tuicommon.GlobalKeys.Cancel.Help().Desc},
	}
}

// viewDetail renders the drill-down for the selected finished test. The
// outcome classes it names come from report.Classify — never re-derived
// here — so an infrastructure fault cannot read like a subject regression
// in this screen without also failing report.Classify's own contract
// (AC16.3).
//
// Diagnostic-bearing lines (Test ID, Outcome, Failed assertions) are wrapped
// to the terminal width so long values are shown in full across multiple lines
// rather than being cut with an ellipsis. Navigational single-line fields
// (Verdict, Duration, Cost) are left as-is because they are short by design.
// The rendered body is clamped to the available content height and the scroll
// offset (Model.detailScrollOffset) controls the visible window.
func (m Model) viewDetail() string {
	width := m.contentWidth()
	tests := m.resultTests()
	if len(tests) == 0 || m.resultsCursor >= len(tests) {
		return m.renderScreen("Test Detail", "", "no detail available", detailHelp())
	}

	test := tests[m.resultsCursor]
	var run report.RunReport
	if len(test.Runs) > 0 {
		run = test.Runs[len(test.Runs)-1]
	}

	classes := report.Classify(run)
	classNames := make([]string, len(classes))
	for i, c := range classes {
		classNames[i] = string(c)
	}

	// Build logical content lines. Diagnostic-bearing fields use Wrap so that
	// long values are presented in full. Navigational fields use plain
	// formatting because they are short and one line per field keeps the layout
	// readable.
	logicalLines := []string{
		tuicommon.Wrap("Test: "+test.TestID, width),
		fmt.Sprintf("Verdict: %s", run.Verdict),
		tuicommon.Wrap("Outcome: "+strings.Join(classNames, ", "), width),
		fmt.Sprintf("Duration: %s", run.Duration),
		fmt.Sprintf("Cost: %.2f USD (%s)", run.Cost.TotalUSD, run.Cost.Attribution),
	}

	// Catalog folder (suite-level, from the terminal result model).
	if m.result != nil && m.result.CatalogFolder != "" {
		logicalLines = append(logicalLines, tuicommon.Wrap("Catalog: "+m.result.CatalogFolder, width))
	}

	// Retained sandbox path -- show explicitly even when absent so the user
	// knows whether retention was active for this run.
	if run.RetainedSandboxPath != "" {
		logicalLines = append(logicalLines, tuicommon.Wrap("Sandbox: "+run.RetainedSandboxPath, width))
	} else {
		logicalLines = append(logicalLines, "Sandbox: no sandbox retained")
	}

	// All assertions with their full diagnostic fields, for both passing and
	// failing outcomes. Data comes directly from report.RunReport -- no
	// re-derivation here.
	for _, a := range run.Assertions {
		line := fmt.Sprintf("Assertion %s %s", a.Outcome, string(a.Class))
		if a.Target != "" {
			line += " [" + a.Target + "]"
		}
		if a.Expected != "" || a.Actual != "" {
			line += fmt.Sprintf(": expected %q, got %q", a.Expected, a.Actual)
		}
		if a.Detail != "" {
			line += " (" + a.Detail + ")"
		}
		logicalLines = append(logicalLines, tuicommon.Wrap(line, width))
	}

	// Failure reasons when present.
	for _, reason := range run.Reasons {
		logicalLines = append(logicalLines, fmt.Sprintf("Reason: %s", string(reason)))
	}

	// Run conditions when present.
	for _, c := range run.Conditions {
		logicalLines = append(logicalLines, tuicommon.Wrap(fmt.Sprintf("Condition %s: %s", c.Kind, c.Detail), width))
	}

	// Expand logical lines into physical lines: Wrap may introduce embedded
	// newlines when a logical line overflows the terminal width.
	var allLines []string
	for _, ll := range logicalLines {
		allLines = append(allLines, strings.Split(ll, "\n")...)
	}

	// Compute the available body height and apply the scroll offset. The
	// offset is supplied by the model and clamped here so the view is always
	// self-consistent regardless of how large the offset field has grown.
	contentH := tuicommon.ContentHeight(m.height, false)
	maxOffset := len(allLines) - contentH
	if maxOffset < 0 {
		maxOffset = 0
	}
	offset := m.detailScrollOffset
	if offset > maxOffset {
		offset = maxOffset
	}
	if offset < 0 {
		offset = 0
	}
	visible := allLines[offset:]
	if len(visible) > contentH {
		visible = visible[:contentH]
	}

	return m.renderScreen("Test Detail", "", strings.Join(visible, "\n"), detailHelp())
}
