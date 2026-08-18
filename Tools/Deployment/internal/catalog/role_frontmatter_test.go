package catalog_test

// role_frontmatter_test.go verifies the catalog integration path for frontmatter-declared
// roles using isolated t.TempDir() fixture trees.
//
// Behaviors exercised:
//
//   Frontmatter role wins over directory-implied role:
//     A file placed in the orchestrator path (which carries path-derived RoleOrchestrator)
//     receives RoleSubagent when its frontmatter declares `role: subagent`. This is the
//     primary property that guards against silent role changes on directory reorganisation.
//
//   Absent role field falls back to path-derived role:
//     A worker-agent file with no `role` field in its frontmatter is loaded with the
//     path-derived role (RoleSubagent). This test is explicit so the fallback-path coverage
//     does not depend on the absence of `role` in real source files.
//
//   Unrecognised role value — agent retained, Issue recorded:
//     A file with `role: bad-value` in frontmatter is loaded with the path-derived role
//     (not dropped), and catalog.Issues() contains exactly one entry with Code "invalid-role"
//     and Severity SeverityError.

import (
	"path/filepath"
	"strings"
	"testing"

	"mosaic-common/docformat"
	"mosaic-deploy/internal/catalog"
	"mosaic-deploy/internal/domain"
)

// writeAgentFixture writes a minimal agent markdown file at relPath inside root.
// frontmatter is the raw YAML content between the --- delimiters (must end with \n).
// The parent directory must already exist.
func writeAgentFixture(t *testing.T, root, relPath, frontmatter string) {
	t.Helper()
	content := []byte("---\n" + frontmatter + "---\n\n# Test Agent Fixture\n")
	mustWriteFile(t, root, relPath, content)
}

// ---------------------------------------------------------------------------
// Frontmatter role wins over directory-implied role
// ---------------------------------------------------------------------------

// TestParseAgentFile_FrontmatterRoleWins_SubagentInOrchestratorPath verifies that a file
// placed in the orchestrator directory receives RoleSubagent when its frontmatter declares
// `role: subagent`, overriding the path-derived RoleOrchestrator. This guards against
// silent contract-block changes caused by directory reorganisation.
func TestParseAgentFile_FrontmatterRoleWins_SubagentInOrchestratorPath(t *testing.T) {
	root := makeTempMosaicRoot(t)

	// Place a fixture in the orchestrator path declaring role: subagent.
	// Without frontmatter override the path-derived role would be RoleOrchestrator.
	mustMkdir(t, root, "Catalog", "Orchestrator")
	writeAgentFixture(t, root,
		filepath.Join("Catalog", "Orchestrator", "orchestrator.md"),
		"role: subagent\n")

	cat, err := catalog.Load(root, "")
	if err != nil {
		t.Fatalf("catalog.Load: %v", err)
	}

	a := cat.Orchestrator()
	if a.Key == "" {
		t.Fatal("orchestrator agent was not loaded from fixture")
	}
	if a.Role != domain.RoleSubagent {
		t.Errorf("orchestrator Role = %q, want RoleSubagent; frontmatter must win over path-derived role", a.Role)
	}

	// No invalid-role issue should be produced — "subagent" is a recognised value.
	for _, iss := range cat.Issues() {
		if iss.Code == "invalid-role" {
			t.Errorf("unexpected invalid-role issue for recognised role value: %s", iss.Message)
		}
	}
}

// ---------------------------------------------------------------------------
// Absent role field falls back to path-derived role
// ---------------------------------------------------------------------------

// TestParseAgentFile_AbsentRole_FallsBackToPathDerivedRole verifies that a worker-agent
// file with no `role` frontmatter field receives the path-derived role (RoleSubagent).
// This test is explicit so the fallback-path coverage survives once migration batches add
// `role` to real source files.
func TestParseAgentFile_AbsentRole_FallsBackToPathDerivedRole(t *testing.T) {
	root := makeTempMosaicRoot(t)

	// Worker-agent path: Catalog/Subagents/{category}/*.md → path-derived RoleSubagent.
	mustMkdir(t, root, "Catalog", "Subagents", "Validation")
	writeAgentFixture(t, root,
		filepath.Join("Catalog", "Subagents", "Validation", "no-role-agent.md"),
		"version: \"1.0\"\nname: No Role Agent\n")

	cat, err := catalog.Load(root, "")
	if err != nil {
		t.Fatalf("catalog.Load: %v", err)
	}

	a, ok := cat.Agent("no-role-agent")
	if !ok {
		t.Fatal("Agent(\"no-role-agent\") not found; expected agent to be present in catalog")
	}
	if a.Role != domain.RoleSubagent {
		t.Errorf("no-role-agent Role = %q, want RoleSubagent (path-derived fallback)", a.Role)
	}
}

// ---------------------------------------------------------------------------
// New first-class frontmatter role values: standalone and utility
// ---------------------------------------------------------------------------

// TestParseAgentFile_StandaloneRole_LoadsWithRoleStandaloneNoIssue verifies that a file
// declaring `role: standalone` in its frontmatter is loaded with RoleStandalone and
// produces no invalid-role issue. After the vocabulary expansion, "standalone" is a
// first-class accepted value — parseAgentFile must not treat it as unrecognised.
func TestParseAgentFile_StandaloneRole_LoadsWithRoleStandaloneNoIssue(t *testing.T) {
	root := makeTempMosaicRoot(t)

	mustMkdir(t, root, "Catalog", "Subagents", "Standalone")
	writeAgentFixture(t, root,
		filepath.Join("Catalog", "Subagents", "Standalone", "my-standalone-agent.md"),
		"role: standalone\nversion: \"1.0\"\nname: My Standalone Agent\n")

	cat, err := catalog.Load(root, "")
	if err != nil {
		t.Fatalf("catalog.Load: %v", err)
	}

	// The agent must be present in the catalog.
	a, ok := cat.Agent("my-standalone-agent")
	if !ok {
		t.Fatal("Agent(\"my-standalone-agent\") not found; expected agent to be present in catalog")
	}

	// Frontmatter `role: standalone` must win over the path-derived subagent role.
	if a.Role != domain.RoleStandalone {
		t.Errorf("my-standalone-agent Role = %q, want RoleStandalone; frontmatter must win over path-derived role", a.Role)
	}

	// No invalid-role issue must be produced — "standalone" is a recognised value.
	for _, iss := range cat.Issues() {
		if iss.Code == "invalid-role" {
			t.Errorf("unexpected invalid-role issue for \"standalone\" (a valid frontmatter role): %s", iss.Message)
		}
	}
}

// TestParseAgentFile_UtilityRoleInFrontmatter_LoadsWithRoleUtilityNoIssue verifies that a
// file explicitly declaring `role: utility` in its frontmatter is loaded with RoleUtility and
// produces no invalid-role issue. After the vocabulary expansion, "utility" is a valid
// frontmatter role — parseAgentFile must not treat it as unrecognised.
func TestParseAgentFile_UtilityRoleInFrontmatter_LoadsWithRoleUtilityNoIssue(t *testing.T) {
	root := makeTempMosaicRoot(t)

	// Place the fixture in the subagent path so the path-derived role (RoleSubagent)
	// differs from the declared role (RoleUtility), making frontmatter-wins detectable.
	mustMkdir(t, root, "Catalog", "Subagents", "Utility")
	writeAgentFixture(t, root,
		filepath.Join("Catalog", "Subagents", "Utility", "my-utility-agent.md"),
		"role: utility\nversion: \"1.0\"\nname: My Utility Agent\n")

	cat, err := catalog.Load(root, "")
	if err != nil {
		t.Fatalf("catalog.Load: %v", err)
	}

	// The agent must be present in the catalog.
	a, ok := cat.Agent("my-utility-agent")
	if !ok {
		t.Fatal("Agent(\"my-utility-agent\") not found; expected agent to be present in catalog")
	}

	// Frontmatter `role: utility` must win over the path-derived subagent role.
	if a.Role != domain.RoleUtility {
		t.Errorf("my-utility-agent Role = %q, want RoleUtility; frontmatter must win over path-derived role", a.Role)
	}

	// No invalid-role issue must be produced — "utility" is a recognised value.
	for _, iss := range cat.Issues() {
		if iss.Code == "invalid-role" {
			t.Errorf("unexpected invalid-role issue for \"utility\" (a valid frontmatter role): %s", iss.Message)
		}
	}
}

// TestParseAgentFile_UnrecognisedRole_IssueMessageNamesAllFourValues verifies that the
// invalid-role issue message names all four accepted values — "subagent", "orchestrator",
// "utility", and "standalone" — so that users know the complete valid vocabulary.
// This test is separate from TestParseAgentFile_UnrecognisedRole_AgentRetainedAndIssueAppended,
// which verifies the issue is appended; this one verifies the message content.
func TestParseAgentFile_UnrecognisedRole_IssueMessageNamesAllFourValues(t *testing.T) {
	root := makeTempMosaicRoot(t)

	mustMkdir(t, root, "Catalog", "Subagents", "Validation")
	writeAgentFixture(t, root,
		filepath.Join("Catalog", "Subagents", "Validation", "four-values-check.md"),
		"role: completely-unknown\n")

	cat, err := catalog.Load(root, "")
	if err != nil {
		t.Fatalf("catalog.Load: %v", err)
	}

	var invalidRoleIssues []catalog.Issue
	for _, iss := range cat.Issues() {
		if iss.Code == "invalid-role" {
			invalidRoleIssues = append(invalidRoleIssues, iss)
		}
	}
	if len(invalidRoleIssues) == 0 {
		t.Fatal("no invalid-role issue found; expected one for unrecognised role")
	}

	msg := invalidRoleIssues[0].Message
	for _, expected := range []string{"subagent", "orchestrator", "utility", "standalone"} {
		if !strings.Contains(msg, expected) {
			t.Errorf("invalid-role issue message %q does not mention %q; "+
				"message must name all four accepted values", msg, expected)
		}
	}
}

// ---------------------------------------------------------------------------
// Unrecognised role value — agent retained, Issue recorded
// ---------------------------------------------------------------------------

// TestParseAgentFile_UnrecognisedRole_AgentRetainedAndIssueAppended verifies two properties
// of the invalid-role path in parseAgentFile:
//  1. The agent is added to the catalog with its path-derived role (not silently dropped).
//  2. Exactly one Issue with Code "invalid-role" and Severity SeverityError is present in
//     Catalog.Issues().
func TestParseAgentFile_UnrecognisedRole_AgentRetainedAndIssueAppended(t *testing.T) {
	root := makeTempMosaicRoot(t)

	// Write a fixture with an unrecognised role value.
	mustMkdir(t, root, "Catalog", "Subagents", "Validation")
	writeAgentFixture(t, root,
		filepath.Join("Catalog", "Subagents", "Validation", "bad-role-agent.md"),
		"role: bad-value\n")

	cat, err := catalog.Load(root, "")
	if err != nil {
		t.Fatalf("catalog.Load: %v", err)
	}

	// The agent must be retained in the catalog.
	if _, ok := cat.Agent("bad-role-agent"); !ok {
		t.Error("Agent(\"bad-role-agent\") not found; unrecognised role must not cause the agent to be dropped")
	}

	// Exactly one invalid-role issue must be recorded.
	var invalidRoleIssues []catalog.Issue
	for _, iss := range cat.Issues() {
		if iss.Code == "invalid-role" {
			invalidRoleIssues = append(invalidRoleIssues, iss)
		}
	}
	if len(invalidRoleIssues) != 1 {
		t.Fatalf("got %d invalid-role issues, want exactly 1; all issues: %v",
			len(invalidRoleIssues), cat.Issues())
	}

	iss := invalidRoleIssues[0]
	if iss.Severity != docformat.SeverityError {
		t.Errorf("invalid-role issue Severity = %q, want SeverityError", iss.Severity)
	}
	if iss.Subject != "bad-role-agent" {
		t.Errorf("invalid-role issue Subject = %q, want %q", iss.Subject, "bad-role-agent")
	}
}
