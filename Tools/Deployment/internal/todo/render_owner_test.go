package todo_test

// render_owner_test.go covers owner-grouped rendering in RenderMarkdown and the consistency of
// RenderSummary with that output (T8.5, T8.6).
//
// The rendering contract for the Project-specific injections category (domain.TodoInjections):
//
//   - Items with a non-empty Owner are rendered under a "### agent: <owner>" sub-heading.
//   - Items with an empty Owner are rendered under a "### unattributed" sub-heading, always last.
//   - The "### unattributed" heading is emitted only when at least one unowned item exists.
//   - Two agents carrying the same region name produce two distinct sub-headings, making every
//     item uniquely attributable.
//   - Output is deterministic: same input always yields byte-identical output.
//
// For every other category (Models, Tool mappings, Skipped files, Hook registration,
// Manual steps, Deployment location):
//   - Items render as a flat list of checkbox bullets with no owner sub-headings (T8.6).
//
// RenderSummary is unchanged: it counts items per category and does not mention owners.
// This file asserts that RenderSummary remains consistent with RenderMarkdown (T8.6).

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/todo"
)

// ownerMeta returns a stable Meta value for rendering tests in this file.
func ownerMeta() todo.Meta {
	return todo.Meta{
		Harness:       "claude-code",
		WorkspacePath: "/workspace",
		GeneratedAt:   time.Date(2026, 8, 14, 14, 24, 5, 0, time.UTC),
		Mode:          domain.ModeDeployWorkspace,
	}
}

// ---------------------------------------------------------------------------
// T8.5: injection items grouped by agent in RenderMarkdown
// ---------------------------------------------------------------------------

// TestRenderMarkdown_InjectionItems_OwnerHeadingPresent verifies that when injection items
// carry an Owner, a sub-heading naming that owner appears in the markdown output above the
// item's checkbox bullet. The exact heading form is "### agent: <owner>".
func TestRenderMarkdown_InjectionItems_OwnerHeadingPresent(t *testing.T) {
	c := todo.NewCollector()
	c.Add(domain.TodoItem{
		Category: domain.TodoInjections,
		Subject:  "SeverityThresholds",
		Owner:    "build-review",
	})

	out := todo.RenderMarkdown(c.Groups(), ownerMeta())
	content := string(out)

	if !strings.Contains(content, "### agent: build-review") {
		t.Errorf("RenderMarkdown output does not contain owner heading %q in the injection section;\noutput:\n%s",
			"### agent: build-review", content)
	}
	// The item itself must also be present.
	if !strings.Contains(content, "SeverityThresholds") {
		t.Errorf("RenderMarkdown output does not contain item subject %q;\noutput:\n%s",
			"SeverityThresholds", content)
	}
}

// TestRenderMarkdown_InjectionItems_TwoAgentsSameRegionDistinguishable verifies that when two
// agents both carry an injection region of the same name, each item is rendered under its
// own agent sub-heading so the two entries are distinguishable (AC8.7).
func TestRenderMarkdown_InjectionItems_TwoAgentsSameRegionDistinguishable(t *testing.T) {
	c := todo.NewCollector()
	c.Add(domain.TodoItem{
		Category: domain.TodoInjections,
		Subject:  "SeverityThresholds",
		Owner:    "build-review",
	})
	c.Add(domain.TodoItem{
		Category: domain.TodoInjections,
		Subject:  "SeverityThresholds",
		Owner:    "plan-review",
	})

	out := todo.RenderMarkdown(c.Groups(), ownerMeta())
	content := string(out)

	// Both agent sub-headings must appear separately in the output.
	if !strings.Contains(content, "### agent: build-review") {
		t.Errorf("RenderMarkdown output missing owner heading %q;\noutput:\n%s", "### agent: build-review", content)
	}
	if !strings.Contains(content, "### agent: plan-review") {
		t.Errorf("RenderMarkdown output missing owner heading %q;\noutput:\n%s", "### agent: plan-review", content)
	}
	// The subject appears twice (once per agent), confirming both items are present.
	count := strings.Count(content, "SeverityThresholds")
	if count < 2 {
		t.Errorf("RenderMarkdown: expected SeverityThresholds to appear at least twice (once per agent), got %d occurrences;\noutput:\n%s",
			count, content)
	}
}

// TestRenderMarkdown_InjectionItems_UnownedRendersUnattributed verifies that an injection item
// with no Owner is rendered under an "unattributed" sub-heading so the reader knows it could
// not be attributed, rather than being silently dropped or merged with a named owner's items.
func TestRenderMarkdown_InjectionItems_UnownedRendersUnattributed(t *testing.T) {
	c := todo.NewCollector()
	c.Add(domain.TodoItem{
		Category: domain.TodoInjections,
		Subject:  "LegacyRegion",
		Owner:    "", // no owner
	})

	out := todo.RenderMarkdown(c.Groups(), ownerMeta())
	content := string(out)

	if !strings.Contains(content, "### unattributed") {
		t.Errorf("RenderMarkdown output does not contain %q heading for unowned injection item;\noutput:\n%s",
			"### unattributed", content)
	}
	if !strings.Contains(content, "LegacyRegion") {
		t.Errorf("RenderMarkdown output does not contain item subject %q;\noutput:\n%s",
			"LegacyRegion", content)
	}
}

// TestRenderMarkdown_InjectionItems_UnattributedGroupIsLast verifies that the "unattributed"
// group appears after all named owner groups in the rendered output, matching GroupByOwner's
// ordering contract.
func TestRenderMarkdown_InjectionItems_UnattributedGroupIsLast(t *testing.T) {
	c := todo.NewCollector()
	c.Add(domain.TodoItem{
		Category: domain.TodoInjections,
		Subject:  "UnownedRegion",
		Owner:    "",
	})
	c.Add(domain.TodoItem{
		Category: domain.TodoInjections,
		Subject:  "OwnedRegion",
		Owner:    "agent-a",
	})

	out := todo.RenderMarkdown(c.Groups(), ownerMeta())
	content := string(out)

	posAgent := strings.Index(content, "### agent: agent-a")
	posUnattrib := strings.Index(content, "### unattributed")

	if posAgent < 0 {
		t.Fatalf("RenderMarkdown output missing %q heading;\noutput:\n%s", "### agent: agent-a", content)
	}
	if posUnattrib < 0 {
		t.Fatalf("RenderMarkdown output missing %q heading;\noutput:\n%s", "### unattributed", content)
	}
	if posUnattrib < posAgent {
		t.Errorf("'unattributed' heading appears before 'agent-a' heading in output; want unattributed last;\noutput:\n%s",
			content)
	}
}

// TestRenderMarkdown_InjectionItems_UnattributedHeadingAbsentWhenAllOwned verifies that the
// "unattributed" sub-heading is not emitted when every item in the injection category has
// a non-empty Owner — no phantom heading for a non-existent group.
func TestRenderMarkdown_InjectionItems_UnattributedHeadingAbsentWhenAllOwned(t *testing.T) {
	c := todo.NewCollector()
	c.Add(domain.TodoItem{
		Category: domain.TodoInjections,
		Subject:  "SomeName",
		Owner:    "agent-a",
	})

	out := todo.RenderMarkdown(c.Groups(), ownerMeta())
	content := string(out)

	if strings.Contains(content, "unattributed") {
		t.Errorf("RenderMarkdown emitted 'unattributed' heading when all items have an owner;\noutput:\n%s",
			content)
	}
}

// TestRenderMarkdown_InjectionItems_DeterministicOutput verifies that the owner-grouped
// injection output is deterministic: the same collector state always produces byte-identical
// markdown across multiple calls (AC8.8).
func TestRenderMarkdown_InjectionItems_DeterministicOutput(t *testing.T) {
	c := todo.NewCollector()
	c.Add(domain.TodoItem{Category: domain.TodoInjections, Subject: "RegionA", Owner: "plan-review"})
	c.Add(domain.TodoItem{Category: domain.TodoInjections, Subject: "RegionB", Owner: "build-review"})
	c.Add(domain.TodoItem{Category: domain.TodoInjections, Subject: "RegionC", Owner: ""})

	groups := c.Groups()
	meta := ownerMeta()

	out1 := todo.RenderMarkdown(groups, meta)
	out2 := todo.RenderMarkdown(groups, meta)

	if !bytes.Equal(out1, out2) {
		t.Error("RenderMarkdown with owned injection items is not deterministic: same input produced different output")
	}
}

// ---------------------------------------------------------------------------
// T8.6: other categories keep flat rendering; summary stays consistent
// ---------------------------------------------------------------------------

// TestRenderMarkdown_NonInjectionCategories_RemainsFlat verifies that categories other than
// Project-specific injections still render as a flat list of checkbox bullets with no owner
// sub-headings, even when TodoItem.Owner is populated. This pins the flat-rendering contract
// for Models, Tool mappings, Skipped files, Hook registration, Manual steps, and Deployment
// location categories (AC8.8).
func TestRenderMarkdown_NonInjectionCategories_RemainsFlat(t *testing.T) {
	c := todo.NewCollector()
	// Add items to non-injection categories with Owner set to a non-empty value.
	// The Owner must be ignored in those categories — no sub-heading should appear.
	c.Add(domain.TodoItem{
		Category: domain.TodoModels,
		Subject:  "my-agent",
		Owner:    "some-owner",
	})
	c.Add(domain.TodoItem{
		Category: domain.TodoSkippedFiles,
		Subject:  "agents/conflict.md",
		Owner:    "another-owner",
	})

	out := todo.RenderMarkdown(c.Groups(), ownerMeta())
	content := string(out)

	// The Models and Skipped files category headings must appear as ## headings.
	if !strings.Contains(content, "## "+string(domain.TodoModels)) {
		t.Errorf("RenderMarkdown missing ## heading for %q", domain.TodoModels)
	}
	if !strings.Contains(content, "## "+string(domain.TodoSkippedFiles)) {
		t.Errorf("RenderMarkdown missing ## heading for %q", domain.TodoSkippedFiles)
	}

	// No "### agent:" sub-headings should appear in the non-injection sections.
	// We verify by checking that "some-owner" and "another-owner" do not appear as headings —
	// they should not appear at all in flat-rendered categories.
	for _, ownerName := range []string{"some-owner", "another-owner"} {
		if strings.Contains(content, ownerName) {
			t.Errorf("RenderMarkdown emitted owner name %q in a non-injection category; "+
				"non-injection categories must render flat without owner sub-headings;\noutput:\n%s",
				ownerName, content)
		}
	}

	// The item subjects must still appear.
	if !strings.Contains(content, "my-agent") {
		t.Errorf("RenderMarkdown missing item subject %q in Models category", "my-agent")
	}
	if !strings.Contains(content, "agents/conflict.md") {
		t.Errorf("RenderMarkdown missing item subject %q in Skipped files category", "agents/conflict.md")
	}
}

// TestRenderMarkdown_MixedCategories_InjectionGroupedOthersFlat verifies the combined case:
// injection items render grouped by owner while non-injection items in the same run remain
// flat. This is the realistic production scenario.
func TestRenderMarkdown_MixedCategories_InjectionGroupedOthersFlat(t *testing.T) {
	c := todo.NewCollector()
	c.Add(domain.TodoItem{Category: domain.TodoModels, Subject: "model-agent", Owner: ""})
	c.Add(domain.TodoItem{Category: domain.TodoInjections, Subject: "CodebaseContext", Owner: "build-review"})
	c.Add(domain.TodoItem{Category: domain.TodoInjections, Subject: "CodebaseContext", Owner: "plan-review"})

	out := todo.RenderMarkdown(c.Groups(), ownerMeta())
	content := string(out)

	// Injection section must have agent sub-headings in the exact "### agent: <owner>" form.
	if !strings.Contains(content, "### agent: build-review") {
		t.Errorf("RenderMarkdown: injection section missing owner heading %q;\noutput:\n%s", "### agent: build-review", content)
	}
	if !strings.Contains(content, "### agent: plan-review") {
		t.Errorf("RenderMarkdown: injection section missing owner heading %q;\noutput:\n%s", "### agent: plan-review", content)
	}

	// Models section must still be a flat ## section.
	if !strings.Contains(content, "## "+string(domain.TodoModels)) {
		t.Errorf("RenderMarkdown: Models category lost its ## heading;\noutput:\n%s", content)
	}
	if !strings.Contains(content, "model-agent") {
		t.Errorf("RenderMarkdown: Models item subject 'model-agent' missing;\noutput:\n%s", content)
	}
}

// TestRenderSummary_ConsistentWithOwnerGroupedMarkdown verifies that RenderSummary counts
// are consistent with RenderMarkdown when injection items are grouped by owner. The summary
// counts items per category (not per owner), so the injection category count equals the total
// number of injection items regardless of how many agent groups they split across.
func TestRenderSummary_ConsistentWithOwnerGroupedMarkdown(t *testing.T) {
	c := todo.NewCollector()
	c.Add(domain.TodoItem{Category: domain.TodoInjections, Subject: "RegionA", Owner: "agent-a"})
	c.Add(domain.TodoItem{Category: domain.TodoInjections, Subject: "RegionB", Owner: "agent-b"})
	c.Add(domain.TodoItem{Category: domain.TodoInjections, Subject: "RegionC", Owner: ""})
	c.Add(domain.TodoItem{Category: domain.TodoModels, Subject: "my-agent", Owner: ""})

	groups := c.Groups()
	markdownOut := todo.RenderMarkdown(groups, ownerMeta())
	summaryLines := todo.RenderSummary(groups)

	markdownContent := string(markdownOut)

	// Every category present in the summary must also appear in the markdown.
	for _, line := range summaryLines {
		if !strings.Contains(markdownContent, string(line.Category)) {
			t.Errorf("summary has category %q that is absent from markdown", line.Category)
		}
		if line.Text == "" {
			t.Errorf("summary line for category %q has empty Text", line.Category)
		}
	}

	// The number of summary lines must equal the number of non-empty category groups.
	if len(summaryLines) != len(groups) {
		t.Errorf("len(summaryLines) = %d; len(groups) = %d; want 1 summary line per non-empty group",
			len(summaryLines), len(groups))
	}
}

// TestRenderSummary_InjectionCategoryCountsAllItems verifies that RenderSummary counts
// injection items per category (not per owner), so the total reported equals the full count
// across all agent groups — the summary is category-level, not owner-level.
func TestRenderSummary_InjectionCategoryCountsAllItems(t *testing.T) {
	c := todo.NewCollector()
	c.Add(domain.TodoItem{Category: domain.TodoInjections, Subject: "RegionA", Owner: "agent-a"})
	c.Add(domain.TodoItem{Category: domain.TodoInjections, Subject: "RegionB", Owner: "agent-b"})
	c.Add(domain.TodoItem{Category: domain.TodoInjections, Subject: "RegionC", Owner: ""})

	groups := c.Groups()
	lines := todo.RenderSummary(groups)

	if len(lines) != 1 {
		t.Fatalf("RenderSummary len = %d; want 1 (only injections category)", len(lines))
	}
	// The summary line for injections must mention 3 items (all of them), not 1 or 2.
	if !strings.Contains(lines[0].Text, "3") {
		t.Errorf("RenderSummary for injections = %q; want it to mention 3 items (all injection items across all owners)",
			lines[0].Text)
	}
}
