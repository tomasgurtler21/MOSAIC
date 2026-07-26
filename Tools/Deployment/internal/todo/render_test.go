package todo_test

// Tests for markdown checklist rendering (T15.7) and the shared-source guarantee (T15.8).
//
// RenderMarkdown and RenderSummary are both projections of the same []Group slice obtained
// from Collector.Groups(). This structural arrangement (CD-9, AC15.6) is what prevents the
// interactive summary from saying one thing and the checklist file saying another.
//
// A run with no gaps must produce a checklist file that clearly states nothing is required,
// not a blank or empty document that would confuse the reader (AC15.7).

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/todo"
)

// sampleMeta returns a Meta struct with distinctive values for use in rendering tests.
func sampleMeta() todo.Meta {
	return todo.Meta{
		Harness:        "claude-code",
		WorkspacePath:  "/workspace/my-project",
		DeploymentRoot: "/workspace/my-project/.claude",
		GeneratedAt:    time.Date(2024, 1, 15, 9, 0, 0, 0, time.UTC),
		Mode:           domain.ModeDeployNew,
	}
}

// ---------------------------------------------------------------------------
// T15.7: Markdown checklist rendering — structure and content
// ---------------------------------------------------------------------------

// TestRenderMarkdown_EmptyCollectorProducesClearContent verifies that a Collector with no items
// produces a non-empty markdown file that clearly states no manual actions are required,
// rather than a blank or confusing document (AC15.7).
func TestRenderMarkdown_EmptyCollectorProducesClearContent(t *testing.T) {
	c := todo.NewCollector()
	out := todo.RenderMarkdown(c.Groups(), sampleMeta())

	if len(out) == 0 {
		t.Fatal("RenderMarkdown with no items returned empty bytes; want a file that says nothing is required")
	}

	content := strings.ToLower(string(out))
	signals := []string{"no ", "none", "complete", "nothing", "all set", "0 item", "no action"}
	found := false
	for _, s := range signals {
		if strings.Contains(content, s) {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("RenderMarkdown empty-run output does not clearly indicate no items; got:\n%s", string(out))
	}
}

// TestRenderMarkdown_ContainsMarkdownCheckboxSyntax verifies that when items are present the
// output uses markdown checkbox syntax (- [ ]) for each item.
func TestRenderMarkdown_ContainsMarkdownCheckboxSyntax(t *testing.T) {
	c := todo.NewCollector()
	c.AddGap(domain.Gap{Kind: domain.GapNoModel, Subject: "my-agent", Detail: "No model assigned"})
	out := todo.RenderMarkdown(c.Groups(), sampleMeta())

	if !bytes.Contains(out, []byte("[ ]")) {
		t.Errorf("RenderMarkdown output missing checkbox syntax '[ ]'; full output:\n%s", out)
	}
}

// TestRenderMarkdown_ContainsItemSubject verifies that each item's Subject appears verbatim
// in the markdown output.
func TestRenderMarkdown_ContainsItemSubject(t *testing.T) {
	c := todo.NewCollector()
	c.AddGap(domain.Gap{Kind: domain.GapNoModel, Subject: "distinctive-agent-subject-xyz"})
	out := todo.RenderMarkdown(c.Groups(), sampleMeta())

	if !bytes.Contains(out, []byte("distinctive-agent-subject-xyz")) {
		t.Errorf("RenderMarkdown output missing item subject 'distinctive-agent-subject-xyz'")
	}
}

// TestRenderMarkdown_ContainsItemDetail verifies that each item's Detail text appears in the
// markdown output so the user knows what action is needed.
func TestRenderMarkdown_ContainsItemDetail(t *testing.T) {
	c := todo.NewCollector()
	c.AddGap(domain.Gap{
		Kind:   domain.GapNoModel,
		Subject: "detail-agent",
		Detail:  "Assign a model in the harness configuration before next deployment",
	})
	out := todo.RenderMarkdown(c.Groups(), sampleMeta())

	if !bytes.Contains(out, []byte("Assign a model in the harness configuration before next deployment")) {
		t.Errorf("RenderMarkdown output missing item detail text")
	}
}

// TestRenderMarkdown_ContainsCategoryHeadings verifies that the markdown output contains a
// section heading for each category that has items.
func TestRenderMarkdown_ContainsCategoryHeadings(t *testing.T) {
	c := todo.NewCollector()
	c.AddGap(domain.Gap{Kind: domain.GapNoModel, Subject: "agent-a"})
	c.AddGap(domain.Gap{Kind: domain.GapUnmappedTool, Subject: "file_edit"})
	out := todo.RenderMarkdown(c.Groups(), sampleMeta())

	content := string(out)
	if !strings.Contains(content, string(domain.TodoModels)) {
		t.Errorf("RenderMarkdown missing heading text for category %q", domain.TodoModels)
	}
	if !strings.Contains(content, string(domain.TodoToolMappings)) {
		t.Errorf("RenderMarkdown missing heading text for category %q", domain.TodoToolMappings)
	}
}

// TestRenderMarkdown_ContainsHarnessMetadata verifies that the generated file includes the
// harness name from the Meta, giving readers context about which harness produced the run.
func TestRenderMarkdown_ContainsHarnessMetadata(t *testing.T) {
	c := todo.NewCollector()
	meta := sampleMeta()
	out := todo.RenderMarkdown(c.Groups(), meta)

	if !bytes.Contains(out, []byte(meta.Harness)) {
		t.Errorf("RenderMarkdown missing harness %q from metadata", meta.Harness)
	}
}

// TestRenderMarkdown_FragmentAppearsInOutput verifies that when a gap has a Fragment (literal
// content the user must paste), it appears verbatim in the markdown output.
func TestRenderMarkdown_FragmentAppearsInOutput(t *testing.T) {
	c := todo.NewCollector()
	c.AddGap(domain.Gap{
		Kind:     domain.GapHookRegistration,
		Subject:  "pre-commit",
		Fragment: "#!/bin/sh\nmosaic-deploy hook run\n",
	})
	out := todo.RenderMarkdown(c.Groups(), sampleMeta())

	if !bytes.Contains(out, []byte("mosaic-deploy hook run")) {
		t.Errorf("RenderMarkdown output missing fragment content 'mosaic-deploy hook run'")
	}
}

// TestRenderMarkdown_MultipleItemsAllPresent verifies that when a run has many gaps across
// multiple categories, all items appear in the output.
func TestRenderMarkdown_MultipleItemsAllPresent(t *testing.T) {
	c := todo.NewCollector()
	c.AddGap(domain.Gap{Kind: domain.GapNoModel, Subject: "agent-multi-a"})
	c.AddGap(domain.Gap{Kind: domain.GapNoModel, Subject: "agent-multi-b"})
	c.AddGap(domain.Gap{Kind: domain.GapUnmappedTool, Subject: "search_web"})
	c.AddGap(domain.Gap{Kind: domain.GapSkippedFile, Subject: "agents/conflict.md"})
	out := todo.RenderMarkdown(c.Groups(), sampleMeta())

	for _, subject := range []string{"agent-multi-a", "agent-multi-b", "search_web", "agents/conflict.md"} {
		if !bytes.Contains(out, []byte(subject)) {
			t.Errorf("RenderMarkdown output missing item subject %q", subject)
		}
	}
}

// TestRenderMarkdown_IsDeterministic verifies that RenderMarkdown is pure: the same groups and
// meta always produce byte-identical output.
func TestRenderMarkdown_IsDeterministic(t *testing.T) {
	c := todo.NewCollector()
	c.AddGap(domain.Gap{Kind: domain.GapNoModel, Subject: "agent-a"})
	c.AddGap(domain.Gap{Kind: domain.GapUnmappedTool, Subject: "file_edit"})

	groups := c.Groups()
	meta := sampleMeta()
	out1 := todo.RenderMarkdown(groups, meta)
	out2 := todo.RenderMarkdown(groups, meta)

	if !bytes.Equal(out1, out2) {
		t.Error("RenderMarkdown is not deterministic: same input produced different output on two calls")
	}
}

// ---------------------------------------------------------------------------
// T15.8: Single-source guarantee — summary and checklist cannot disagree
// ---------------------------------------------------------------------------

// TestSharedSource_SummaryAndMarkdownDeriveFromSameGroups verifies the structural guarantee
// of CD-9 and AC15.6: both renderers receive the same Groups() value from the Collector,
// which is the only way they can be prevented from disagreeing.
func TestSharedSource_SummaryAndMarkdownDeriveFromSameGroups(t *testing.T) {
	c := todo.NewCollector()
	c.AddGap(domain.Gap{Kind: domain.GapNoModel, Subject: "agent-a"})
	c.AddGap(domain.Gap{Kind: domain.GapUnmappedTool, Subject: "file_edit"})
	c.AddGap(domain.Gap{Kind: domain.GapSkippedFile, Subject: "agents/skipped.md"})

	// Both renderers are called with the SAME groups slice from one Collector.Groups() call.
	groups := c.Groups()
	markdownOut := todo.RenderMarkdown(groups, sampleMeta())
	summaryLines := todo.RenderSummary(groups)

	// Every category present in the summary must also appear in the markdown.
	markdownContent := string(markdownOut)
	for _, line := range summaryLines {
		if !strings.Contains(markdownContent, string(line.Category)) {
			t.Errorf("summary has category %q that is absent from markdown; the two outputs disagree", line.Category)
		}
	}
}

// TestSharedSource_NumberOfGroupsMatchesSummaryLines verifies that RenderSummary produces
// exactly one line per non-empty category group, matching the structure of RenderMarkdown.
func TestSharedSource_NumberOfGroupsMatchesSummaryLines(t *testing.T) {
	c := todo.NewCollector()
	c.AddGap(domain.Gap{Kind: domain.GapNoModel, Subject: "agent-b"})
	c.AddGap(domain.Gap{Kind: domain.GapHookRegistration, Subject: "git-hook"})

	groups := c.Groups()
	summaryLines := todo.RenderSummary(groups)

	if len(summaryLines) != len(groups) {
		t.Errorf("RenderSummary returned %d lines for %d groups; want 1 line per group",
			len(summaryLines), len(groups))
	}
}

// TestSharedSource_SummaryLinesHaveNonEmptyText verifies that every SummaryLine produced by
// RenderSummary has a non-empty Text field — an empty line would be useless in the TUI.
func TestSharedSource_SummaryLinesHaveNonEmptyText(t *testing.T) {
	c := todo.NewCollector()
	c.AddGap(domain.Gap{Kind: domain.GapNoModel, Subject: "agent-c"})
	c.AddGap(domain.Gap{Kind: domain.GapSkippedFile, Subject: "agents/file.md"})

	groups := c.Groups()
	lines := todo.RenderSummary(groups)

	for i, line := range lines {
		if line.Text == "" {
			t.Errorf("RenderSummary line[%d] (category %q) has empty Text", i, line.Category)
		}
	}
}

// TestSharedSource_SummaryLinesCategoryMatchesGroups verifies that each SummaryLine carries
// the same category as the corresponding group, preserving the group order.
func TestSharedSource_SummaryLinesCategoryMatchesGroups(t *testing.T) {
	c := todo.NewCollector()
	c.AddGap(domain.Gap{Kind: domain.GapNoModel, Subject: "agent-d"})
	c.AddGap(domain.Gap{Kind: domain.GapManualStep, Subject: "editor-setting"})

	groups := c.Groups()
	lines := todo.RenderSummary(groups)

	if len(lines) != len(groups) {
		t.Fatalf("len(lines) = %d; len(groups) = %d: cannot compare position-by-position",
			len(lines), len(groups))
	}
	for i := range groups {
		if lines[i].Category != groups[i].Category {
			t.Errorf("line[%d].Category = %q; group[%d].Category = %q: order mismatch",
				i, lines[i].Category, i, groups[i].Category)
		}
	}
}

// TestSharedSource_EmptyCollectorSummaryIsCoherent verifies that an empty Collector produces
// either zero summary lines or lines that clearly indicate no actions are needed (AC15.7),
// consistent with what RenderMarkdown says about the same empty state.
func TestSharedSource_EmptyCollectorSummaryIsCoherent(t *testing.T) {
	c := todo.NewCollector()
	groups := c.Groups()
	lines := todo.RenderSummary(groups)

	// Acceptable: empty slice (the TUI shows nothing, which is clear enough).
	if len(lines) == 0 {
		return
	}
	// Also acceptable: lines with text indicating no items.
	for _, line := range lines {
		lower := strings.ToLower(line.Text)
		signals := []string{"no ", "none", "complete", "nothing", "all set", "0 item", "no action"}
		found := false
		for _, s := range signals {
			if strings.Contains(lower, s) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("RenderSummary empty-run line does not indicate no items; Text = %q", line.Text)
		}
	}
}

// TestSharedSource_MarkdownAndSummaryAgreeOnAllCategories verifies that the set of categories
// present in the summary exactly matches the set appearing in the markdown, across a run with
// all documented gap kinds.
func TestSharedSource_MarkdownAndSummaryAgreeOnAllCategories(t *testing.T) {
	c := todo.NewCollector()
	c.AddGap(domain.Gap{Kind: domain.GapNoModel, Subject: "a1"})
	c.AddGap(domain.Gap{Kind: domain.GapUnmappedTool, Subject: "a2"})
	c.AddGap(domain.Gap{Kind: domain.GapEmptyInjection, Subject: "a3"})
	c.AddGap(domain.Gap{Kind: domain.GapSkippedFile, Subject: "a4"})
	c.AddGap(domain.Gap{Kind: domain.GapHookRegistration, Subject: "a5"})
	c.AddGap(domain.Gap{Kind: domain.GapManualStep, Subject: "a6"})
	c.AddGap(domain.Gap{Kind: domain.GapFallbackLocation, Subject: "a7"})

	groups := c.Groups()
	markdownOut := todo.RenderMarkdown(groups, sampleMeta())
	summaryLines := todo.RenderSummary(groups)

	markdownContent := string(markdownOut)

	// Build sets of categories from each renderer for symmetric comparison.
	markdownCategories := make(map[domain.TodoCategory]bool)
	for _, g := range groups {
		if strings.Contains(markdownContent, string(g.Category)) {
			markdownCategories[g.Category] = true
		}
	}
	summaryCategories := make(map[domain.TodoCategory]bool)
	for _, line := range summaryLines {
		summaryCategories[line.Category] = true
	}

	for cat := range summaryCategories {
		if !markdownCategories[cat] {
			t.Errorf("category %q is in summary but not found in markdown", cat)
		}
	}
	for cat := range markdownCategories {
		if !summaryCategories[cat] {
			t.Errorf("category %q is in markdown but not found in summary", cat)
		}
	}
}
