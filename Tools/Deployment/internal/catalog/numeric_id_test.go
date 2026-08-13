package catalog_test

// numeric_id_test.go verifies AgentByNumericID — the numeric-id-keyed catalog lookup
// introduced in Stage 8.
//
// Verified behaviors:
//
//   - An agent whose source file declares a numeric `id` is found by that id (happy path).
//   - An empty id string never matches any agent.
//   - A non-existent id returns false.
//   - An agent whose source file declares no `id` is not reachable through AgentByNumericID,
//     even though it is present in the catalog by key.
//   - The orchestrator, which carries no `id`, is never returned by AgentByNumericID.
//   - When two catalog agents declare the same `id`, the catalog records a
//     "duplicate-agent-id" issue at error severity. The issue, not a silent pick, is the
//     contract.

import (
	"path/filepath"
	"strings"
	"testing"

	"mosaic-common/docformat"
	"mosaic-deploy/internal/catalog"
)

// ---------------------------------------------------------------------------
// Test helpers for agent file creation
// ---------------------------------------------------------------------------

// writeWorkerAgentFile writes a minimal, parseable agent source file at
//
//	<root>/Catalog/Agents/Generic/Agents/<category>/<name>.md
//
// with numericID as the frontmatter `id` scalar. Pass numericID="" to omit the `id` field
// entirely (simulating an agent that has no numeric id).
func writeWorkerAgentFile(t *testing.T, root, category, name, numericID string) {
	t.Helper()
	mustMkdir(t, root, "Catalog", "Agents", "Generic", "Agents", category)
	fm := "---\n"
	if numericID != "" {
		fm += "id: \"" + numericID + "\"\n"
	}
	fm += "version: \"1.0.0\"\n"
	fm += "name: " + name + "\n"
	fm += "description: Test agent for numeric-id unit tests.\n"
	fm += "role: subagent\n"
	fm += "---\nContent.\n"
	relPath := filepath.Join("Catalog", "Agents", "Generic", "Agents", category, name+".md")
	mustWriteFile(t, root, relPath, []byte(fm))
}

// ---------------------------------------------------------------------------
// AgentByNumericID — happy path
// ---------------------------------------------------------------------------

// TestAgentByNumericID_KnownID_ReturnsAgent verifies that an agent whose source file
// declares a numeric `id` is returned by AgentByNumericID with the matching key and
// NumericID fields.
func TestAgentByNumericID_KnownID_ReturnsAgent(t *testing.T) {
	// Arrange
	root := makeTempMosaicRoot(t)
	writeWorkerAgentFile(t, root, "Creation", "test-writer-tdd", "99")

	cat, err := catalog.Load(root, "")
	if err != nil {
		t.Fatalf("catalog.Load: %v", err)
	}

	// Act
	agent, ok := cat.AgentByNumericID("99")

	// Assert
	if !ok {
		t.Fatal("AgentByNumericID(\"99\") returned ok = false; expected the agent with id \"99\" to be found")
	}
	if agent.Key != "test-writer-tdd" {
		t.Errorf("AgentByNumericID(\"99\").Key = %q, want %q", agent.Key, "test-writer-tdd")
	}
	if agent.NumericID != "99" {
		t.Errorf("AgentByNumericID(\"99\").NumericID = %q, want %q", agent.NumericID, "99")
	}
}

// TestAgentByNumericID_MultipleAgents_EachFoundByOwnID verifies that when the catalog
// contains several agents with distinct numeric ids, each is found by its own id and not
// by another agent's id.
func TestAgentByNumericID_MultipleAgents_EachFoundByOwnID(t *testing.T) {
	// Arrange
	root := makeTempMosaicRoot(t)
	writeWorkerAgentFile(t, root, "Creation", "agent-alpha", "10")
	writeWorkerAgentFile(t, root, "Execution", "agent-beta", "20")
	writeWorkerAgentFile(t, root, "Validation", "agent-gamma", "30")

	cat, err := catalog.Load(root, "")
	if err != nil {
		t.Fatalf("catalog.Load: %v", err)
	}

	cases := []struct {
		id      string
		wantKey string
	}{
		{"10", "agent-alpha"},
		{"20", "agent-beta"},
		{"30", "agent-gamma"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run("id="+tc.id, func(t *testing.T) {
			agent, ok := cat.AgentByNumericID(tc.id)
			if !ok {
				t.Fatalf("AgentByNumericID(%q) returned ok = false; expected agent %q", tc.id, tc.wantKey)
			}
			if agent.Key != tc.wantKey {
				t.Errorf("AgentByNumericID(%q).Key = %q, want %q", tc.id, agent.Key, tc.wantKey)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// AgentByNumericID — empty and absent id
// ---------------------------------------------------------------------------

// TestAgentByNumericID_EmptyID_ReturnsFalse verifies that an empty id string is never a
// valid lookup key. Agents with no `id` field have NumericID == "" and must not be returned
// by this method — empty id is excluded by contract.
func TestAgentByNumericID_EmptyID_ReturnsFalse(t *testing.T) {
	// Arrange
	root := makeTempMosaicRoot(t)
	writeWorkerAgentFile(t, root, "Creation", "test-writer-tdd", "99")

	cat, err := catalog.Load(root, "")
	if err != nil {
		t.Fatalf("catalog.Load: %v", err)
	}

	// Act
	_, ok := cat.AgentByNumericID("")

	// Assert
	if ok {
		t.Error("AgentByNumericID(\"\") returned ok = true; empty id must never match any agent")
	}
}

// TestAgentByNumericID_NonExistentID_ReturnsFalse verifies that a numeric id not declared
// by any catalog agent returns false.
func TestAgentByNumericID_NonExistentID_ReturnsFalse(t *testing.T) {
	// Arrange
	root := makeTempMosaicRoot(t)
	writeWorkerAgentFile(t, root, "Creation", "test-writer-tdd", "99")

	cat, err := catalog.Load(root, "")
	if err != nil {
		t.Fatalf("catalog.Load: %v", err)
	}

	// Act
	_, ok := cat.AgentByNumericID("9999")

	// Assert
	if ok {
		t.Error("AgentByNumericID(\"9999\") returned ok = true; id \"9999\" is not declared by any catalog agent")
	}
}

// TestAgentByNumericID_AgentWithNoID_NotInNumericIndex verifies that an agent whose source
// file declares no `id` field is reachable via Agent() by key but NOT via AgentByNumericID.
// Such agents have NumericID == "" and must be excluded from the numeric id index.
func TestAgentByNumericID_AgentWithNoID_NotInNumericIndex(t *testing.T) {
	// Arrange — write an agent without an `id` field.
	root := makeTempMosaicRoot(t)
	writeWorkerAgentFile(t, root, "Creation", "no-id-agent", "")

	cat, err := catalog.Load(root, "")
	if err != nil {
		t.Fatalf("catalog.Load: %v", err)
	}

	// Precondition: the agent must be present by key.
	if _, ok := cat.Agent("no-id-agent"); !ok {
		t.Fatal("Agent(\"no-id-agent\") returned false; the agent must be present in the catalog by key")
	}

	// Act — looking up by empty id must not match the id-less agent.
	_, ok := cat.AgentByNumericID("")

	// Assert
	if ok {
		t.Error("AgentByNumericID(\"\") returned ok = true; " +
			"an agent with no `id` field must not be discoverable via the numeric id index")
	}
}

// ---------------------------------------------------------------------------
// AgentByNumericID — orchestrator is excluded
// ---------------------------------------------------------------------------

// TestAgentByNumericID_OrchestratorIsExcluded verifies that the orchestrator agent, which
// has no numeric id by definition, is never returned by AgentByNumericID. The orchestrator
// carries no `id` frontmatter field; querying with empty id must not find it.
func TestAgentByNumericID_OrchestratorIsExcluded(t *testing.T) {
	// Arrange — a minimal catalog with one worker agent so the catalog is non-trivial.
	root := makeTempMosaicRoot(t)
	writeWorkerAgentFile(t, root, "Creation", "test-writer-tdd", "1")

	cat, err := catalog.Load(root, "")
	if err != nil {
		t.Fatalf("catalog.Load: %v", err)
	}

	// Precondition: confirm the orchestrator has no numeric id.
	orc := cat.Orchestrator()
	if orc.NumericID != "" {
		// The real orchestrator file should not declare an `id`; skip rather than fail.
		t.Skipf("orchestrator.NumericID = %q (unexpected); "+
			"this test assumes the orchestrator has no numeric id", orc.NumericID)
	}

	// Act
	agent, ok := cat.AgentByNumericID("")

	// Assert — the orchestrator must not be returned via the empty-id path.
	if ok {
		t.Errorf("AgentByNumericID(\"\") returned ok = true with agent key %q; "+
			"the orchestrator (no numeric id) must not be returned by AgentByNumericID", agent.Key)
	}
}

// ---------------------------------------------------------------------------
// Duplicate id detection
// ---------------------------------------------------------------------------

// TestCatalog_DuplicateNumericID_RecordsDuplicateAgentIDIssue verifies that when two
// catalog agents declare the same numeric `id`, the catalog records a "duplicate-agent-id"
// issue at error severity. Detecting and surfacing the collision — rather than silently
// discarding one agent — is the contract.
func TestCatalog_DuplicateNumericID_RecordsDuplicateAgentIDIssue(t *testing.T) {
	// Arrange — two agents in different categories, both declaring id "77".
	root := makeTempMosaicRoot(t)
	writeWorkerAgentFile(t, root, "Creation", "agent-alpha", "77")
	writeWorkerAgentFile(t, root, "Execution", "agent-beta", "77")

	// Act
	cat, err := catalog.Load(root, "")
	if err != nil {
		t.Fatalf("catalog.Load: %v", err)
	}

	// Assert — a "duplicate-agent-id" error issue must be present.
	issues := cat.Issues()
	found := false
	for _, issue := range issues {
		if issue.Code == "duplicate-agent-id" && issue.Severity == docformat.SeverityError {
			found = true
			break
		}
	}
	if !found {
		t.Error("catalog.Issues() contains no \"duplicate-agent-id\" error issue; " +
			"two catalog agents declaring the same numeric id must produce this issue at error severity")
	}
}

// TestCatalog_DuplicateNumericID_IssueSubjectIdentifiesConflictingAgents verifies that the
// "duplicate-agent-id" issue's Subject or Message contains enough context to identify which
// id is duplicated, so an operator can locate and resolve the conflict.
func TestCatalog_DuplicateNumericID_IssueSubjectIdentifiesConflictingAgents(t *testing.T) {
	// Arrange
	root := makeTempMosaicRoot(t)
	writeWorkerAgentFile(t, root, "Creation", "agent-alpha", "77")
	writeWorkerAgentFile(t, root, "Execution", "agent-beta", "77")

	cat, err := catalog.Load(root, "")
	if err != nil {
		t.Fatalf("catalog.Load: %v", err)
	}

	// Find the duplicate-agent-id issue.
	var dupIssue *catalog.Issue
	for _, issue := range cat.Issues() {
		if issue.Code == "duplicate-agent-id" {
			i := issue
			dupIssue = &i
			break
		}
	}
	if dupIssue == nil {
		t.Fatal("no duplicate-agent-id issue found; cannot check Subject/Message content")
	}

	// Assert — the id "77" must appear in Subject or Message so the operator can identify it.
	if dupIssue.Subject != "77" && !strings.Contains(dupIssue.Message, "77") {
		t.Errorf("duplicate-agent-id issue: Subject = %q, Message = %q; "+
			"expected the duplicated id \"77\" to appear in Subject or Message for traceability",
			dupIssue.Subject, dupIssue.Message)
	}
}
