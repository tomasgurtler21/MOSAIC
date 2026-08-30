package harness_test

// Tests for ExtractClaudeCodeTools and ExtractGHCPCLITools: frontmatter-based
// tool extraction from deployed agent definition files.
//
// All tests write temporary agent definition files with known frontmatter and
// assert on the returned tool name slices. No mocking is needed: the functions
// accept a file path and return ([]string, error), making them directly testable.
//
// TDD RED phase: these tests are written before the implementation. They will
// panic at runtime until toolextract.go is implemented (Stage 1, I1.1-I1.3).

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"mosaic-common/harness"
)

// ---------------------------------------------------------------------------
// Testdata helpers
// ---------------------------------------------------------------------------

// writeAgentFile writes content to a new file inside t.TempDir() and
// returns the absolute file path. The file is removed automatically when the
// test ends via t.TempDir's cleanup.
func writeAgentFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "agent.md")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("writeAgentFile: %v", err)
	}
	return path
}

// ---------------------------------------------------------------------------
// ExtractClaudeCodeTools: Claude Code shape (comma-separated scalar)
// ---------------------------------------------------------------------------

// TestExtractClaudeCodeTools_ParsesCommaSeparatedScalar verifies that a
// deployed Claude Code agent definition with a comma-separated tools scalar
// is split into individual tool name strings.
func TestExtractClaudeCodeTools_ParsesCommaSeparatedScalar(t *testing.T) {
	content := "---\nname: test-agent\ntools: Read, Write, Edit, Bash\n---\n\nBody.\n"
	path := writeAgentFile(t, content)

	got, err := harness.ExtractClaudeCodeTools(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := []string{"Read", "Write", "Edit", "Bash"}
	if len(got) != len(want) {
		t.Fatalf("want %d tools %v, got %d: %v", len(want), want, len(got), got)
	}
	for i, name := range want {
		if got[i] != name {
			t.Errorf("tools[%d]: want %q, got %q", i, name, got[i])
		}
	}
}

// TestExtractClaudeCodeTools_ParsesFullOrchestratorToolSet verifies the
// realistic full tool set matching the deployed Claude Code golden fixture:
// Read, Write, Edit, Bash, Glob, Grep, Task, TaskStop, AskUserQuestion.
func TestExtractClaudeCodeTools_ParsesFullOrchestratorToolSet(t *testing.T) {
	content := "---\nname: orchestrator\ndescription: Test fixture\nmodel: claude-sonnet-4-6\ntools: Read, Write, Edit, Bash, Glob, Grep, Task, TaskStop, AskUserQuestion\n---\n\nBody.\n"
	path := writeAgentFile(t, content)

	got, err := harness.ExtractClaudeCodeTools(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := []string{"Read", "Write", "Edit", "Bash", "Glob", "Grep", "Task", "TaskStop", "AskUserQuestion"}
	if len(got) != len(want) {
		t.Fatalf("want %d tools, got %d: %v", len(want), len(got), got)
	}
	for i, name := range want {
		if got[i] != name {
			t.Errorf("tools[%d]: want %q, got %q", i, name, got[i])
		}
	}
}

// TestExtractClaudeCodeTools_TrimsWhitespace verifies that leading and trailing
// whitespace is trimmed from each tool name after splitting on commas.
func TestExtractClaudeCodeTools_TrimsWhitespace(t *testing.T) {
	content := "---\nname: test-agent\ntools:  Read ,  Write ,  Edit \n---\n\nBody.\n"
	path := writeAgentFile(t, content)

	got, err := harness.ExtractClaudeCodeTools(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := []string{"Read", "Write", "Edit"}
	if len(got) != len(want) {
		t.Fatalf("want %d tools after whitespace trim, got %d: %v", len(want), len(got), got)
	}
	for i, name := range want {
		if got[i] != name {
			t.Errorf("tools[%d]: want %q (whitespace trimmed), got %q", i, name, got[i])
		}
	}
}

// TestExtractClaudeCodeTools_SingleTool verifies parsing when there is exactly
// one tool in the comma-separated scalar (no commas present).
func TestExtractClaudeCodeTools_SingleTool(t *testing.T) {
	content := "---\nname: test-agent\ntools: Read\n---\n\nBody.\n"
	path := writeAgentFile(t, content)

	got, err := harness.ExtractClaudeCodeTools(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 || got[0] != "Read" {
		t.Errorf("want [\"Read\"], got %v", got)
	}
}

// TestExtractClaudeCodeTools_ReadsFromFilePath verifies that the function
// accepts and reads from a file path (not pre-parsed data), with a realistic
// deployed frontmatter shape including non-tools keys.
func TestExtractClaudeCodeTools_ReadsFromFilePath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "subagent.md")
	content := "---\nname: subagent\ndescription: A test subagent\nmodel: claude-sonnet-4-6\ntools: Read, Glob, Grep\nmosaic_role: subagent\n---\n\nAgent body.\n"
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("writing agent file: %v", err)
	}

	got, err := harness.ExtractClaudeCodeTools(path)
	if err != nil {
		t.Fatalf("unexpected error reading from file path %q: %v", path, err)
	}

	want := []string{"Read", "Glob", "Grep"}
	if len(got) != len(want) {
		t.Fatalf("want %v, got %v", want, got)
	}
	for i, name := range want {
		if got[i] != name {
			t.Errorf("tools[%d]: want %q, got %q", i, name, got[i])
		}
	}
}

// TestExtractClaudeCodeTools_MissingToolsKey verifies that ErrToolsMissing is
// returned when the frontmatter has no tools key at all.
func TestExtractClaudeCodeTools_MissingToolsKey(t *testing.T) {
	content := "---\nname: test-agent\nmodel: claude-sonnet-4-6\n---\n\nBody.\n"
	path := writeAgentFile(t, content)

	_, err := harness.ExtractClaudeCodeTools(path)
	if !errors.Is(err, harness.ErrToolsMissing) {
		t.Errorf("want errors.Is(err, ErrToolsMissing), got %v", err)
	}
}

// TestExtractClaudeCodeTools_EmptyToolsValue verifies that ErrToolsEmpty is
// returned when the tools key is present but has an empty scalar value.
func TestExtractClaudeCodeTools_EmptyToolsValue(t *testing.T) {
	content := "---\nname: test-agent\ntools:\n---\n\nBody.\n"
	path := writeAgentFile(t, content)

	_, err := harness.ExtractClaudeCodeTools(path)
	if !errors.Is(err, harness.ErrToolsEmpty) {
		t.Errorf("want errors.Is(err, ErrToolsEmpty) for empty tools value, got %v", err)
	}
}

// TestExtractClaudeCodeTools_WrongKindReturnsErrToolsEmpty verifies that when
// the tools field has a list shape (not a scalar), ErrToolsEmpty is returned
// because the shape does not match the Claude Code expected format.
func TestExtractClaudeCodeTools_WrongKindReturnsErrToolsEmpty(t *testing.T) {
	// GHCP CLI flow-list style is the wrong shape for Claude Code extraction.
	content := "---\nname: test-agent\ntools: ['read', 'edit']\n---\n\nBody.\n"
	path := writeAgentFile(t, content)

	_, err := harness.ExtractClaudeCodeTools(path)
	if !errors.Is(err, harness.ErrToolsEmpty) {
		t.Errorf("want errors.Is(err, ErrToolsEmpty) for list-shaped tools field, got %v", err)
	}
}

// TestExtractClaudeCodeTools_NonexistentFileReturnsError verifies that a file
// I/O error is returned when the path does not exist (not a sentinel, just
// non-nil).
func TestExtractClaudeCodeTools_NonexistentFileReturnsError(t *testing.T) {
	_, err := harness.ExtractClaudeCodeTools("/nonexistent/path/that/does/not/exist/agent.md")
	if err == nil {
		t.Error("want non-nil error for nonexistent file, got nil")
	}
}

// ---------------------------------------------------------------------------
// ExtractGHCPCLITools: GHCP CLI shape (flow-style YAML list with translation)
// ---------------------------------------------------------------------------

// TestExtractGHCPCLITools_TranslatesEditToWrite verifies that the deployed
// GHCP CLI name "edit" is translated to the copilot CLI --allow-tool kind "write".
func TestExtractGHCPCLITools_TranslatesEditToWrite(t *testing.T) {
	content := "---\nname: test-agent\ntools: ['edit']\n---\n\nBody.\n"
	path := writeAgentFile(t, content)

	got, err := harness.ExtractGHCPCLITools(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !containsArg(got, "write") {
		t.Errorf("want \"write\" in result (translated from \"edit\"), got %v", got)
	}
	if containsArg(got, "edit") {
		t.Errorf("want \"edit\" absent (must be translated to \"write\"), got %v", got)
	}
}

// TestExtractGHCPCLITools_TranslatesExecuteToShell verifies that the deployed
// GHCP CLI name "execute" is translated to the copilot CLI --allow-tool kind "shell".
func TestExtractGHCPCLITools_TranslatesExecuteToShell(t *testing.T) {
	content := "---\nname: test-agent\ntools: ['execute']\n---\n\nBody.\n"
	path := writeAgentFile(t, content)

	got, err := harness.ExtractGHCPCLITools(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !containsArg(got, "shell") {
		t.Errorf("want \"shell\" in result (translated from \"execute\"), got %v", got)
	}
	if containsArg(got, "execute") {
		t.Errorf("want \"execute\" absent (must be translated to \"shell\"), got %v", got)
	}
}

// TestExtractGHCPCLITools_PassesAgentVerbatim verifies that the deployed GHCP
// CLI name "agent" passes through to the result unchanged.
func TestExtractGHCPCLITools_PassesAgentVerbatim(t *testing.T) {
	content := "---\nname: test-agent\ntools: ['agent']\n---\n\nBody.\n"
	path := writeAgentFile(t, content)

	got, err := harness.ExtractGHCPCLITools(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !containsArg(got, "agent") {
		t.Errorf("want \"agent\" in result (verbatim pass-through), got %v", got)
	}
}

// TestExtractGHCPCLITools_PassesSkillVerbatim verifies that the deployed GHCP
// CLI name "skill" passes through to the result unchanged.
func TestExtractGHCPCLITools_PassesSkillVerbatim(t *testing.T) {
	content := "---\nname: test-agent\ntools: ['skill']\n---\n\nBody.\n"
	path := writeAgentFile(t, content)

	got, err := harness.ExtractGHCPCLITools(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !containsArg(got, "skill") {
		t.Errorf("want \"skill\" in result (verbatim pass-through), got %v", got)
	}
}

// TestExtractGHCPCLITools_ExcludesRead verifies that "read" is excluded from
// the result because GHCP CLI has no read kind and reads are ungated.
func TestExtractGHCPCLITools_ExcludesRead(t *testing.T) {
	content := "---\nname: test-agent\ntools: ['read', 'edit']\n---\n\nBody.\n"
	path := writeAgentFile(t, content)

	got, err := harness.ExtractGHCPCLITools(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if containsArg(got, "read") {
		t.Errorf("want \"read\" excluded from result, got %v", got)
	}
}

// TestExtractGHCPCLITools_ExcludesSearch verifies that "search" is excluded
// from the result because GHCP CLI auto-allows search operations.
func TestExtractGHCPCLITools_ExcludesSearch(t *testing.T) {
	content := "---\nname: test-agent\ntools: ['read', 'search', 'edit']\n---\n\nBody.\n"
	path := writeAgentFile(t, content)

	got, err := harness.ExtractGHCPCLITools(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if containsArg(got, "search") {
		t.Errorf("want \"search\" excluded from result, got %v", got)
	}
}

// TestExtractGHCPCLITools_ExcludesAskUser verifies that "ask_user" is excluded
// from the result because it is handled by --no-ask-user flag separately and
// does not map to any --allow-tool entry.
func TestExtractGHCPCLITools_ExcludesAskUser(t *testing.T) {
	content := "---\nname: test-agent\ntools: ['ask_user', 'edit']\n---\n\nBody.\n"
	path := writeAgentFile(t, content)

	got, err := harness.ExtractGHCPCLITools(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if containsArg(got, "ask_user") {
		t.Errorf("want \"ask_user\" excluded from result, got %v", got)
	}
}

// TestExtractGHCPCLITools_FullOrchestratorShape verifies the complete realistic
// GHCP CLI deployed tool set matching the golden fixture shape:
// ['read', 'edit', 'search', 'execute', 'ask_user', 'agent']
// Expected result: ["write", "shell", "agent"] (read/search/ask_user excluded;
// edit translated to write; execute translated to shell; agent verbatim).
func TestExtractGHCPCLITools_FullOrchestratorShape(t *testing.T) {
	content := "---\nname: orchestrator\ndescription: Test fixture\nmodel: claude-sonnet-4-6\ntools: ['read', 'edit', 'search', 'execute', 'ask_user', 'agent']\n---\n\nBody.\n"
	path := writeAgentFile(t, content)

	got, err := harness.ExtractGHCPCLITools(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	wantPresent := []string{"write", "shell", "agent"}
	wantAbsent := []string{"read", "search", "ask_user", "edit", "execute"}

	for _, name := range wantPresent {
		if !containsArg(got, name) {
			t.Errorf("want %q in result for full orchestrator shape, got %v", name, got)
		}
	}
	for _, name := range wantAbsent {
		if containsArg(got, name) {
			t.Errorf("want %q absent from result for full orchestrator shape, got %v", name, got)
		}
	}
	if len(got) != len(wantPresent) {
		t.Errorf("want exactly %d tool entries %v, got %d: %v", len(wantPresent), wantPresent, len(got), got)
	}
}

// TestExtractGHCPCLITools_ReadsFromFilePath verifies that the function accepts
// and reads from a file path (not pre-parsed data), using a temp file with a
// realistic deployed frontmatter shape including non-tools keys.
func TestExtractGHCPCLITools_ReadsFromFilePath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ghcp-subagent.md")
	content := "---\nname: ghcp-subagent\ndescription: A GHCP CLI test subagent\nmodel: gpt-5\ntools: ['read', 'edit', 'execute']\nmosaic_role: subagent\n---\n\nAgent body.\n"
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("writing agent file: %v", err)
	}

	got, err := harness.ExtractGHCPCLITools(path)
	if err != nil {
		t.Fatalf("unexpected error reading from file path %q: %v", path, err)
	}

	// read is excluded; edit -> write; execute -> shell.
	wantPresent := []string{"write", "shell"}
	wantAbsent := []string{"read", "edit", "execute"}

	for _, name := range wantPresent {
		if !containsArg(got, name) {
			t.Errorf("want %q in result, got %v", name, got)
		}
	}
	for _, name := range wantAbsent {
		if containsArg(got, name) {
			t.Errorf("want %q absent from result, got %v", name, got)
		}
	}
}

// TestExtractGHCPCLITools_MissingToolsKey verifies that ErrToolsMissing is
// returned when the frontmatter has no tools key at all.
func TestExtractGHCPCLITools_MissingToolsKey(t *testing.T) {
	content := "---\nname: test-agent\nmodel: claude-sonnet-4-6\n---\n\nBody.\n"
	path := writeAgentFile(t, content)

	_, err := harness.ExtractGHCPCLITools(path)
	if !errors.Is(err, harness.ErrToolsMissing) {
		t.Errorf("want errors.Is(err, ErrToolsMissing), got %v", err)
	}
}

// TestExtractGHCPCLITools_EmptyToolsList verifies that ErrToolsEmpty is
// returned when the tools list is present but contains zero entries.
func TestExtractGHCPCLITools_EmptyToolsList(t *testing.T) {
	content := "---\nname: test-agent\ntools: []\n---\n\nBody.\n"
	path := writeAgentFile(t, content)

	_, err := harness.ExtractGHCPCLITools(path)
	if !errors.Is(err, harness.ErrToolsEmpty) {
		t.Errorf("want errors.Is(err, ErrToolsEmpty) for empty tools list, got %v", err)
	}
}

// TestExtractGHCPCLITools_AllExcludedToolsReturnsErrToolsEmpty verifies that
// ErrToolsEmpty is returned when the tools list contains only entries that are
// excluded from --allow-tool (read, search, ask_user), producing zero entries
// after translation.
func TestExtractGHCPCLITools_AllExcludedToolsReturnsErrToolsEmpty(t *testing.T) {
	content := "---\nname: test-agent\ntools: ['read', 'search', 'ask_user']\n---\n\nBody.\n"
	path := writeAgentFile(t, content)

	_, err := harness.ExtractGHCPCLITools(path)
	if !errors.Is(err, harness.ErrToolsEmpty) {
		t.Errorf("want errors.Is(err, ErrToolsEmpty) when all tools are excluded from --allow-tool, got %v", err)
	}
}

// TestExtractGHCPCLITools_WrongKindReturnsErrToolsEmpty verifies that when
// the tools field has a scalar shape (not a list), ErrToolsEmpty is returned
// because the shape does not match the GHCP CLI expected format.
func TestExtractGHCPCLITools_WrongKindReturnsErrToolsEmpty(t *testing.T) {
	// Claude Code comma-separated scalar is the wrong shape for GHCP CLI extraction.
	content := "---\nname: test-agent\ntools: read, edit, execute\n---\n\nBody.\n"
	path := writeAgentFile(t, content)

	_, err := harness.ExtractGHCPCLITools(path)
	if !errors.Is(err, harness.ErrToolsEmpty) {
		t.Errorf("want errors.Is(err, ErrToolsEmpty) for scalar-shaped tools field, got %v", err)
	}
}

// TestExtractGHCPCLITools_NonexistentFileReturnsError verifies that a file I/O
// error is returned when the path does not exist.
func TestExtractGHCPCLITools_NonexistentFileReturnsError(t *testing.T) {
	_, err := harness.ExtractGHCPCLITools("/nonexistent/path/that/does/not/exist/agent.md")
	if err == nil {
		t.Error("want non-nil error for nonexistent file, got nil")
	}
}

// ---------------------------------------------------------------------------
// Non-collision: extract functions do not touch SpawnRequest.AllowedTools
// ---------------------------------------------------------------------------

// TestExtractClaudeCodeTools_DoesNotTouchSpawnRequestAllowedTools verifies the
// non-collision constraint: ExtractClaudeCodeTools accepts only a file path and
// returns ([]string, error). It has no SpawnRequest parameter, so it cannot
// read from or write to SpawnRequest.AllowedTools by design. A caller who
// constructs a SpawnRequest after calling the extract function must explicitly
// choose which field to write the returned tool names to; they cannot
// accidentally land in AllowedTools.
func TestExtractClaudeCodeTools_DoesNotTouchSpawnRequestAllowedTools(t *testing.T) {
	content := "---\nname: test-agent\ntools: Read, Write\n---\n\nBody.\n"
	path := writeAgentFile(t, content)

	// Set up a SpawnRequest with a known AllowedTools value representing
	// AgentTest's test-authored allowlist.
	req := harness.SpawnRequest{
		Agent:        harness.AgentRef{Identifier: "test", DefinitionPath: path},
		AllowedTools: []string{"AgentTestTool", "AnotherAgentTestTool"},
	}

	// Call the extraction function. It returns its own []string and has no
	// SpawnRequest parameter -- the function interface prevents any collision.
	extracted, err := harness.ExtractClaudeCodeTools(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(extracted) == 0 {
		t.Fatal("want non-empty extracted tools")
	}

	// The extracted tool names must differ from AllowedTools: they come from
	// frontmatter, not from the AgentTest-authored allowlist.
	// AllowedTools on the SpawnRequest must be completely unaffected.
	if len(req.AllowedTools) != 2 {
		t.Errorf("want AllowedTools unchanged (2 entries), got %v", req.AllowedTools)
	}
	if req.AllowedTools[0] != "AgentTestTool" {
		t.Errorf("AllowedTools[0]: want \"AgentTestTool\" unchanged, got %q", req.AllowedTools[0])
	}
	if req.AllowedTools[1] != "AnotherAgentTestTool" {
		t.Errorf("AllowedTools[1]: want \"AnotherAgentTestTool\" unchanged, got %q", req.AllowedTools[1])
	}
}

// TestExtractGHCPCLITools_DoesNotTouchSpawnRequestAllowedTools verifies the
// same non-collision constraint for ExtractGHCPCLITools.
func TestExtractGHCPCLITools_DoesNotTouchSpawnRequestAllowedTools(t *testing.T) {
	content := "---\nname: test-agent\ntools: ['edit', 'execute']\n---\n\nBody.\n"
	path := writeAgentFile(t, content)

	req := harness.SpawnRequest{
		Agent:        harness.AgentRef{Identifier: "test", DefinitionPath: path},
		AllowedTools: []string{"AgentTestTool"},
	}

	extracted, err := harness.ExtractGHCPCLITools(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(extracted) == 0 {
		t.Fatal("want non-empty extracted tools")
	}

	// AllowedTools on the SpawnRequest must be unaffected.
	if len(req.AllowedTools) != 1 || req.AllowedTools[0] != "AgentTestTool" {
		t.Errorf("want AllowedTools unchanged [\"AgentTestTool\"], got %v", req.AllowedTools)
	}
}

// TestExtractFunctions_SignaturesPreventAllowedToolsCollision verifies the
// structural guarantee that both extract functions accept (path string) and
// return ([]string, error), with no SpawnRequest in their signature. This
// means a caller placing extracted tools into SpawnRequest must do so
// explicitly via field assignment, making it impossible to accidentally write
// to AllowedTools when the intent is to populate a different field.
//
// This is a compile-time check: if this file compiles, the signatures are
// confirmed correct. The test body demonstrates the required explicit
// assignment pattern.
func TestExtractFunctions_SignaturesPreventAllowedToolsCollision(t *testing.T) {
	content := "---\nname: test-agent\ntools: Read\n---\n\nBody.\n"
	ccPath := writeAgentFile(t, content)

	ghcpContent := "---\nname: test-agent\ntools: ['edit']\n---\n\nBody.\n"
	ghcpPath := writeAgentFile(t, ghcpContent)

	// Both functions have the same signature: (string) -> ([]string, error).
	// They accept a path, not a SpawnRequest.
	ccResult, err := harness.ExtractClaudeCodeTools(ccPath)
	if err != nil {
		t.Fatalf("ExtractClaudeCodeTools: %v", err)
	}
	ghcpResult, err := harness.ExtractGHCPCLITools(ghcpPath)
	if err != nil {
		t.Fatalf("ExtractGHCPCLITools: %v", err)
	}

	// A caller must explicitly assign extracted tools to the intended field.
	// AllowedTools (AgentTest's field) and DerivedTools (Runner's field, added
	// in Stage 2) are separate; this separation is enforced by the extract
	// functions having no SpawnRequest parameter.
	req := harness.SpawnRequest{
		Agent:        harness.AgentRef{Identifier: "test"},
		AllowedTools: []string{"ExistingAgentTestTool"}, // AgentTest's field: unchanged
		// DerivedTools: ccResult or ghcpResult            // Stage 2 wires this
	}
	_ = ccResult
	_ = ghcpResult

	// AllowedTools is unchanged; no extract function can modify it.
	if len(req.AllowedTools) != 1 || req.AllowedTools[0] != "ExistingAgentTestTool" {
		t.Errorf("AllowedTools must remain unchanged, got %v", req.AllowedTools)
	}
}
