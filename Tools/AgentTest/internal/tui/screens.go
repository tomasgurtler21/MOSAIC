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

	"mosaic-agent-test/internal/domain"
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
// Suite selection
// ---------------------------------------------------------------------------

// suiteSelectHelp is EntryScreenHelp with the Space/toggle entry inserted,
// so the retention affordance (Stage 7) is discoverable the same way every
// other key binding on this screen is.
func suiteSelectHelp() []tuicommon.HelpEntry {
	entry := tuicommon.HelpEntry{
		Key:  tuicommon.GlobalKeys.Space.Help().Key,
		Desc: tuicommon.GlobalKeys.Space.Help().Desc,
	}
	return append([]tuicommon.HelpEntry{entry}, tuicommon.EntryScreenHelp()...)
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
	b.WriteString("\n")
	b.WriteString(tuicommon.Truncate(fmt.Sprintf("Retain sandbox: %s", m.retention), width))

	return m.renderScreen("Select a Suite", "", b.String(), suiteSelectHelp())
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

	return m.renderScreen("Suite Progress", "", strings.Join(lines, "\n"), tuicommon.EntryScreenHelp())
}

// ---------------------------------------------------------------------------
// Results
// ---------------------------------------------------------------------------

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

	return m.renderScreen("Results", "", b.String(), tuicommon.EntryScreenHelp())
}

// ---------------------------------------------------------------------------
// Test detail
// ---------------------------------------------------------------------------

// viewDetail renders the drill-down for the selected finished test. The
// outcome classes it names come from report.Classify — never re-derived
// here — so an infrastructure fault cannot read like a subject regression
// in this screen without also failing report.Classify's own contract
// (AC16.3).
func (m Model) viewDetail() string {
	width := m.contentWidth()
	tests := m.resultTests()
	if len(tests) == 0 || m.resultsCursor >= len(tests) {
		return m.renderScreen("Test Detail", "", "no detail available", tuicommon.EntryScreenHelp())
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

	lines := []string{
		tuicommon.Truncate("Test: "+test.TestID, width),
		fmt.Sprintf("Verdict: %s", run.Verdict),
		"Outcome: " + strings.Join(classNames, ", "),
		fmt.Sprintf("Duration: %s", run.Duration),
		fmt.Sprintf("Cost: %.2f USD (%s)", run.Cost.TotalUSD, run.Cost.Attribution),
	}
	for _, a := range run.Assertions {
		if a.Outcome == domain.AssertionFail {
			lines = append(lines, tuicommon.Truncate("Failed assertion: "+a.Detail, width))
		}
	}

	return m.renderScreen("Test Detail", "", strings.Join(lines, "\n"), tuicommon.EntryScreenHelp())
}
