package screens_test

// summary_crossrenderer_test.go verifies AC22.7: the TUI SummaryScreen and the generated
// TODO checklist file (via todo.RenderMarkdown) are projections of one source and must
// not disagree. Both renderers operate on data drawn from the same todo.Collector; this
// test runs both over a shared set of known items and asserts that every subject and
// category that appears in the markdown file also appears in the screen view.

import (
	"strings"
	"testing"
	"time"

	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/todo"
	"mosaic-deploy/internal/tui/screens"
)

// TestSummaryScreen_CrossRenderer_SubjectsMatchMarkdownChecklist verifies that all subjects
// and categories in a todo.Collector appear in both the generated markdown checklist and the
// TUI SummaryScreen view. This is the direct test for AC22.7: neither renderer may drop an
// item that the other renders, because both are projections of the same source.
func TestSummaryScreen_CrossRenderer_SubjectsMatchMarkdownChecklist(t *testing.T) {
	// Arrange: populate a collector with items from two distinct categories so the
	// cross-renderer check covers both category heading output and per-item subject output.
	coll := todo.NewCollector()
	coll.Add(domain.TodoItem{
		Category: domain.TodoModels,
		Subject:  "fast-tier",
		Detail:   "No model configured for fast-tier; select one before running.",
	})
	coll.Add(domain.TodoItem{
		Category: domain.TodoSkippedFiles,
		Subject:  "review-agent",
		Detail:   "User skipped this file during deployment.",
	})

	groups := coll.Groups()
	items := coll.Items()

	// Render the markdown checklist (written to disk as MOSAIC-DEPLOYMENT-TODO.md).
	meta := todo.Meta{
		Harness:       "claude-code",
		WorkspacePath: "/workspace",
		Mode:          domain.ModeDeployNew,
		GeneratedAt:   time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	markdownBytes := todo.RenderMarkdown(groups, meta)
	markdown := string(markdownBytes)

	// Render the TUI summary screen. The RunSummary.Todos field is populated from the same
	// collector (via Items()), mirroring how the app layer feeds the screen after a run.
	summary := domain.RunSummary{
		Mode:           domain.ModeDeployNew,
		Harness:        domain.HarnessRef{ID: "claude-code", DisplayName: "Claude Code"},
		WorkspacePath:  "/workspace",
		DeploymentRoot: "/workspace/.ai",
		Todos:          items,
		TodoFilePath:   "/workspace/" + todo.FileName,
		Outcome:        domain.OutcomeCompletedWithGaps,
	}
	// Use a generous height so that all content is visible without scrolling.
	screen := screens.NewSummaryScreen(summary, 120, 80, plainStyles())
	screenView := screen.View()

	// Assert: every subject must appear in both outputs.
	for _, item := range items {
		if !strings.Contains(markdown, item.Subject) {
			t.Errorf("markdown checklist does not contain subject %q — todo.RenderMarkdown dropped an item that todo.Collector holds", item.Subject)
		}
		if !strings.Contains(screenView, item.Subject) {
			t.Errorf("SummaryScreen view does not contain subject %q — the screen renderer dropped an item that todo.Collector holds", item.Subject)
		}
	}

	// Assert: every category must appear in both outputs.
	for _, g := range groups {
		catStr := string(g.Category)
		if !strings.Contains(markdown, catStr) {
			t.Errorf("markdown checklist does not contain category %q — todo.RenderMarkdown dropped a category that todo.Collector holds", catStr)
		}
		if !strings.Contains(screenView, catStr) {
			t.Errorf("SummaryScreen view does not contain category %q — the screen renderer dropped a category that todo.Collector holds", catStr)
		}
	}
}

// TestSummaryScreen_CrossRenderer_EmptyCollectorProducesConsistentOutputs verifies that when
// the collector is empty (no gaps) both renderers still produce output and neither claims
// follow-up items exist. An empty collector is the clean-run case.
func TestSummaryScreen_CrossRenderer_EmptyCollectorProducesConsistentOutputs(t *testing.T) {
	coll := todo.NewCollector()

	groups := coll.Groups()
	items := coll.Items()

	// Markdown for an empty collector should state no manual actions are required.
	meta := todo.Meta{
		Harness:       "claude-code",
		WorkspacePath: "/workspace",
		Mode:          domain.ModeDeployNew,
		GeneratedAt:   time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	markdownBytes := todo.RenderMarkdown(groups, meta)
	markdown := string(markdownBytes)

	if !strings.Contains(markdown, "No manual actions required") {
		t.Errorf("markdown for empty collector does not contain 'No manual actions required':\n%s", markdown)
	}

	// Screen for an empty collector should not show a follow-up section.
	summary := domain.RunSummary{
		Mode:           domain.ModeDeployNew,
		Harness:        domain.HarnessRef{ID: "claude-code", DisplayName: "Claude Code"},
		WorkspacePath:  "/workspace",
		DeploymentRoot: "/workspace/.ai",
		Todos:          items,
		Outcome:        domain.OutcomeSuccess,
	}
	screen := screens.NewSummaryScreen(summary, 120, 80, plainStyles())
	screenView := screen.View()

	if strings.Contains(screenView, "Follow-up") {
		t.Errorf("SummaryScreen for empty collector shows 'Follow-up' section; want none:\n%s", screenView)
	}

	if coll.Empty() != true {
		t.Error("collector.Empty() = false for an empty collector; want true")
	}
}
