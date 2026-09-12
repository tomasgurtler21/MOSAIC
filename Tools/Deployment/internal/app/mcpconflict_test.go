package app

// mcpconflict_test.go verifies the conflict detection logic for deployed Claude Code
// agent files that contain both a tools: field and an mcpServers: field in their
// frontmatter. When both fields are present, Claude Code silently ignores mcpServers
// (CC-079). The detection function surfaces an advisory gap so the user knows to
// remove mcpServers and re-supply any MCP tools via the corrected mechanism.
//
// The function under test -- checkConflictingToolFields -- is package-private. This
// test file uses package app (not package app_test) so it can access the unexported symbol.
//
// Expected TDD RED state:
// This file FAILS TO COMPILE until checkConflictingToolFields is added to the app package
// (I2.1). Once the implementation adds the function, every test here must PASS for the
// RED phase to be complete. The function signature expected by these tests is:
//
//   func checkConflictingToolFields(fm *docformat.Frontmatter, harnessKey string, agentKey string) []domain.Gap

import (
	"strings"
	"testing"

	"mosaic-common/docformat"
	"mosaic-deploy/internal/domain"
)

// parseFrontmatterFixture wraps a YAML fragment in frontmatter delimiters and parses it,
// returning the *docformat.Frontmatter. It is a test helper only.
func parseFrontmatterFixture(t *testing.T, yamlBody string) *docformat.Frontmatter {
	t.Helper()
	src := "---\n" + yamlBody + "\n---\n\nBody content.\n"
	doc, err := docformat.Parse([]byte(src))
	if err != nil {
		t.Fatalf("parseFrontmatterFixture: docformat.Parse failed: %v", err)
	}
	return doc.Frontmatter()
}

// claudeCodeHarnessKey is the harness identifier used in production. Tests that exercise
// the Claude Code-specific detection branch use this constant so the expected harness key
// is stated once and never duplicated.
const claudeCodeHarnessKey = "claude-code"

// ---------------------------------------------------------------------------
// Detection triggers: Claude Code harness with both tools: and mcpServers:
// ---------------------------------------------------------------------------

// TestCheckConflictingToolFields_BothFieldsPresent_ClaudeCode_ReturnsGap verifies that a
// deployed Claude Code agent whose frontmatter carries both a tools: field and an
// mcpServers: field produces at least one gap of kind GapConflictingToolField.
//
// This is the core scenario: both fields present on Claude Code triggers the warning.
func TestCheckConflictingToolFields_BothFieldsPresent_ClaudeCode_ReturnsGap(t *testing.T) {
	fm := parseFrontmatterFixture(t,
		"tools: [bash, computer]\n"+
			"mcpServers:\n"+
			"  my-server:\n"+
			"    command: /usr/bin/my-server\n",
	)

	gaps := checkConflictingToolFields(fm, claudeCodeHarnessKey, "my-agent")

	if len(gaps) == 0 {
		t.Fatal("expected at least one gap for a Claude Code agent with both tools: and " +
			"mcpServers: in its deployed frontmatter; got none; the CC-079 conflict must " +
			"be surfaced as an advisory gap so the user knows mcpServers is being silently ignored")
	}

	if gaps[0].Kind != domain.GapConflictingToolField {
		t.Errorf("gap Kind = %q, want %q; the gap must use the dedicated GapConflictingToolField "+
			"kind so the todo system can categorize and render it correctly",
			gaps[0].Kind, domain.GapConflictingToolField)
	}
}

// TestCheckConflictingToolFields_ExactlyOneGap_BothFields_ClaudeCode verifies that exactly
// one gap is returned when both fields are present. Multiple gaps for the same conflict
// on a single agent would clutter the todo checklist.
func TestCheckConflictingToolFields_ExactlyOneGap_BothFields_ClaudeCode(t *testing.T) {
	fm := parseFrontmatterFixture(t,
		"tools: [bash]\n"+
			"mcpServers:\n"+
			"  server-a:\n"+
			"    command: /usr/bin/a\n",
	)

	gaps := checkConflictingToolFields(fm, claudeCodeHarnessKey, "my-agent")

	if len(gaps) != 1 {
		t.Errorf("got %d gaps, want exactly 1; a single agent with one tools+mcpServers "+
			"conflict should produce exactly one advisory gap -- zero means the warning "+
			"was lost, more than one means duplicate checklist entries",
			len(gaps))
	}
}

// ---------------------------------------------------------------------------
// No-trigger cases: only tools:, only mcpServers:, or non-Claude Code harness
// ---------------------------------------------------------------------------

// TestCheckConflictingToolFields_OnlyToolsField_ClaudeCode_ReturnsNil verifies that a
// Claude Code agent whose frontmatter has only a tools: field (no mcpServers:) does NOT
// produce a conflict gap. This is the normal, correctly-configured case.
func TestCheckConflictingToolFields_OnlyToolsField_ClaudeCode_ReturnsNil(t *testing.T) {
	fm := parseFrontmatterFixture(t, "tools: [bash, computer]\n")

	gaps := checkConflictingToolFields(fm, claudeCodeHarnessKey, "my-agent")

	if len(gaps) != 0 {
		t.Errorf("got %d gap(s) for a Claude Code agent with only tools: in frontmatter, "+
			"want 0; an agent without mcpServers: must not trigger the CC-079 warning "+
			"(false positive)", len(gaps))
	}
}

// TestCheckConflictingToolFields_OnlyMcpServersField_ClaudeCode_ReturnsNil verifies that a
// Claude Code agent with only an mcpServers: field (no tools:) does not produce a conflict
// gap. Without tools: present, there is no shadowing conflict.
func TestCheckConflictingToolFields_OnlyMcpServersField_ClaudeCode_ReturnsNil(t *testing.T) {
	fm := parseFrontmatterFixture(t,
		"mcpServers:\n"+
			"  my-server:\n"+
			"    command: /usr/bin/my-server\n",
	)

	gaps := checkConflictingToolFields(fm, claudeCodeHarnessKey, "my-agent")

	if len(gaps) != 0 {
		t.Errorf("got %d gap(s) for a Claude Code agent with only mcpServers: in frontmatter, "+
			"want 0; mcpServers: alone is not a conflict -- tools: must also be present for "+
			"shadowing to occur", len(gaps))
	}
}

// TestCheckConflictingToolFields_BothFieldsPresent_NonClaudeCode_ReturnsNil verifies that
// a non-Claude Code agent whose frontmatter has both tools: and mcpServers: does NOT produce
// a conflict gap. The CC-079 warning is Claude Code-specific; other harnesses may legitimately
// use both fields with different semantics.
func TestCheckConflictingToolFields_BothFieldsPresent_NonClaudeCode_ReturnsNil(t *testing.T) {
	fm := parseFrontmatterFixture(t,
		"tools: [bash]\n"+
			"mcpServers:\n"+
			"  my-server:\n"+
			"    command: /usr/bin/my-server\n",
	)

	gaps := checkConflictingToolFields(fm, "other-harness", "my-agent")

	if len(gaps) != 0 {
		t.Errorf("got %d gap(s) for a non-Claude Code agent with both tools: and mcpServers:, "+
			"want 0; the conflict detection is Claude Code-specific (CC-079) and must not "+
			"flag agents deployed for other harnesses", len(gaps))
	}
}

// TestCheckConflictingToolFields_NoFrontmatter_ReturnsNil verifies that a file without a
// frontmatter block (Present() == false) does not produce a conflict gap. The detection
// function must gracefully handle the absence of frontmatter without panicking.
func TestCheckConflictingToolFields_NoFrontmatter_ReturnsNil(t *testing.T) {
	doc, err := docformat.Parse([]byte("No frontmatter here.\n\nJust body text.\n"))
	if err != nil {
		t.Fatalf("docformat.Parse: %v", err)
	}
	fm := doc.Frontmatter()

	gaps := checkConflictingToolFields(fm, claudeCodeHarnessKey, "my-agent")

	if len(gaps) != 0 {
		t.Errorf("got %d gap(s) for a file with no frontmatter, want 0; "+
			"the detection must be a no-op when the deployed file has no frontmatter block",
			len(gaps))
	}
}

// ---------------------------------------------------------------------------
// Gap content: Subject, Owner, and Detail assertions
// ---------------------------------------------------------------------------

// TestCheckConflictingToolFields_GapSubjectIsAgentKey verifies that the returned gap's
// Subject field is set to the agent key, so the todo system can display which agent is
// affected by the conflict.
func TestCheckConflictingToolFields_GapSubjectIsAgentKey(t *testing.T) {
	fm := parseFrontmatterFixture(t,
		"tools: [bash]\n"+
			"mcpServers:\n"+
			"  my-server:\n"+
			"    command: /usr/bin/my-server\n",
	)
	agentKey := "my-specific-agent"

	gaps := checkConflictingToolFields(fm, claudeCodeHarnessKey, agentKey)

	if len(gaps) == 0 {
		t.Fatal("expected at least one gap, got none")
	}
	if gaps[0].Subject != agentKey {
		t.Errorf("gap Subject = %q, want %q; Subject must be the agent key so the checklist "+
			"entry identifies the affected agent",
			gaps[0].Subject, agentKey)
	}
}

// TestCheckConflictingToolFields_GapOwnerIsAgentKey verifies that the returned gap's Owner
// field is set to the agent key. Owner answers "which file is this about" and is used by the
// todo renderer when Subject alone is ambiguous.
func TestCheckConflictingToolFields_GapOwnerIsAgentKey(t *testing.T) {
	fm := parseFrontmatterFixture(t,
		"tools: [bash]\n"+
			"mcpServers:\n"+
			"  my-server:\n"+
			"    command: /usr/bin/my-server\n",
	)
	agentKey := "my-specific-agent"

	gaps := checkConflictingToolFields(fm, claudeCodeHarnessKey, agentKey)

	if len(gaps) == 0 {
		t.Fatal("expected at least one gap, got none")
	}
	if gaps[0].Owner != agentKey {
		t.Errorf("gap Owner = %q, want %q; Owner must be the agent key to provide "+
			"unambiguous file attribution in the checklist output",
			gaps[0].Owner, agentKey)
	}
}

// TestCheckConflictingToolFields_GapDetailMentionsMcpServers verifies that the gap's Detail
// field mentions "mcpServers" so the user knows which frontmatter field is causing the
// problem without having to look up the warning code.
func TestCheckConflictingToolFields_GapDetailMentionsMcpServers(t *testing.T) {
	fm := parseFrontmatterFixture(t,
		"tools: [bash]\n"+
			"mcpServers:\n"+
			"  my-server:\n"+
			"    command: /usr/bin/my-server\n",
	)

	gaps := checkConflictingToolFields(fm, claudeCodeHarnessKey, "my-agent")

	if len(gaps) == 0 {
		t.Fatal("expected at least one gap, got none")
	}
	if !strings.Contains(gaps[0].Detail, "mcpServers") {
		t.Errorf("gap Detail = %q; want it to mention %q so the user can identify "+
			"which deployed frontmatter field needs to be removed",
			gaps[0].Detail, "mcpServers")
	}
}

// TestCheckConflictingToolFields_GapDetailContainsRemediation verifies that the gap's Detail
// field includes a remediation instruction so the user knows what action to take to resolve
// the conflict. Acceptable terms include remove/remov*, migrat*, re-supply, or similar.
func TestCheckConflictingToolFields_GapDetailContainsRemediation(t *testing.T) {
	fm := parseFrontmatterFixture(t,
		"tools: [bash]\n"+
			"mcpServers:\n"+
			"  my-server:\n"+
			"    command: /usr/bin/my-server\n",
	)

	gaps := checkConflictingToolFields(fm, claudeCodeHarnessKey, "my-agent")

	if len(gaps) == 0 {
		t.Fatal("expected at least one gap, got none")
	}

	detail := strings.ToLower(gaps[0].Detail)
	remediationTerms := []string{"remov", "migrat", "re-supply", "resupply", "instead", "correct"}
	found := false
	for _, term := range remediationTerms {
		if strings.Contains(detail, term) {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("gap Detail = %q; want it to contain a remediation hint (one of: %v) "+
			"so the user knows what action to take; a warning without a remediation "+
			"leaves the user unable to fix the problem",
			gaps[0].Detail, remediationTerms)
	}
}
