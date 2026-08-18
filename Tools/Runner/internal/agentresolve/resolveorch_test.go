package agentresolve_test

// Tests for agentresolve.ResolveOrchestrator and the invariant that the
// orchestrator reference is separate from the dispatchable agent map.
//
// Coverage:
//
//   Happy path — orchestrator reference identity:
//   - Identifier is the file's name with its extension removed (filename stem).
//   - DefinitionPath is the absolute path to the file.
//   - InvocationKind is InvocationOrchestrator (not InvocationOrdinary).
//
//   Refusal — unresolvable orchestrator:
//   - A path to a file that does not exist returns *domain.RefusalError.
//   - RefusalError.Component is "agentresolve".
//   - The error names the given path (Resource or Error() text).
//   - A path that points to a directory (not a file) returns *domain.RefusalError.
//   - A path whose filename has an empty stem returns *domain.RefusalError.
//
//   Separation from dispatchable agents:
//   - Calling ResolveAll with only workflow agent identifiers does not include
//     the orchestrator in the result even when its file is in the same directory.
//   - The reference from ResolveOrchestrator carries InvocationOrchestrator,
//     not InvocationOrdinary, asserting it is not a regular agent dispatch.
//   - ResolveAll's refusal for a missing agent definition file is unchanged
//     regardless of whether ResolveOrchestrator is also called.

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mosaic-run/internal/agentresolve"
	"mosaic-run/internal/domain"
)

// minimalWorkflowContent is minimal valid orchestrator file content used in
// fixtures that need the file to be readable by orchfile (not needed for
// ResolveOrchestrator itself, which only cares about the file's existence and stem).
const minimalWorkflowContent = `<Workflow type="core" name="demo" version="1.0">
## Demo Workflow

| Phase | Subagent | HITL | Input | Output |
|-------|----------|:----:|-------|--------|
| PLANNING | agent-a | ❌ | - | plan.md |
</Workflow>
`

// writeOrchFile creates an orchestrator file in dir with the given name.
// Content is minimal valid workflow content so the file is readable.
func writeOrchFile(t *testing.T, dir, name string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(minimalWorkflowContent), 0600); err != nil {
		t.Fatalf("writeOrchFile(%q): %v", name, err)
	}
	return path
}

// asRefusalErrorOrch asserts that err wraps a *domain.RefusalError and returns it.
func asRefusalErrorOrch(t *testing.T, err error) *domain.RefusalError {
	t.Helper()
	var re *domain.RefusalError
	if !errors.As(err, &re) {
		t.Fatalf("want *domain.RefusalError, got %T: %v", err, err)
	}
	return re
}

// ===== Happy path: orchestrator reference identity =====

// TestResolveOrchestrator_IdentifierIsFileStem verifies that the returned
// AgentReference.Identifier is the file's name with its extension removed.
func TestResolveOrchestrator_IdentifierIsFileStem(t *testing.T) {
	dir := t.TempDir()
	path := writeOrchFile(t, dir, "my-orchestrator.md")

	ref, err := agentresolve.ResolveOrchestrator(path)

	if err != nil {
		t.Fatalf("ResolveOrchestrator: unexpected error: %v", err)
	}
	if ref.Identifier != "my-orchestrator" {
		t.Errorf("want Identifier %q (file stem), got %q", "my-orchestrator", ref.Identifier)
	}
}

// TestResolveOrchestrator_DefinitionPathMatchesSuppliedFile verifies that
// the returned AgentReference.DefinitionPath is the absolute path of the file
// supplied to ResolveOrchestrator.
func TestResolveOrchestrator_DefinitionPathMatchesSuppliedFile(t *testing.T) {
	dir := t.TempDir()
	path := writeOrchFile(t, dir, "my-orchestrator.md")

	ref, err := agentresolve.ResolveOrchestrator(path)

	if err != nil {
		t.Fatalf("ResolveOrchestrator: unexpected error: %v", err)
	}
	if ref.DefinitionPath != path {
		t.Errorf("want DefinitionPath %q, got %q", path, ref.DefinitionPath)
	}
}

// TestResolveOrchestrator_InvocationKindIsOrchestrator verifies that the
// returned reference carries InvocationOrchestrator so the orchestrator retains
// native subagent-spawning capability during a consultation.
func TestResolveOrchestrator_InvocationKindIsOrchestrator(t *testing.T) {
	dir := t.TempDir()
	path := writeOrchFile(t, dir, "my-orchestrator.md")

	ref, err := agentresolve.ResolveOrchestrator(path)

	if err != nil {
		t.Fatalf("ResolveOrchestrator: unexpected error: %v", err)
	}
	if ref.InvocationKind != domain.InvocationOrchestrator {
		t.Errorf("want InvocationKind %q, got %q",
			domain.InvocationOrchestrator, ref.InvocationKind)
	}
}

// ===== Refusal: file does not exist =====

// TestResolveOrchestrator_NonexistentFile_ReturnsError verifies that a path
// naming a file that does not exist produces a non-nil error.
func TestResolveOrchestrator_NonexistentFile_ReturnsError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "does-not-exist.md")

	_, err := agentresolve.ResolveOrchestrator(path)

	if err == nil {
		t.Fatal("want error for a path that does not exist, got nil")
	}
}

// TestResolveOrchestrator_NonexistentFile_ReturnsRefusalError verifies that
// the error for a non-existent path wraps *domain.RefusalError.
func TestResolveOrchestrator_NonexistentFile_ReturnsRefusalError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "does-not-exist.md")

	_, err := agentresolve.ResolveOrchestrator(path)

	asRefusalErrorOrch(t, err)
}

// TestResolveOrchestrator_NonexistentFile_ComponentIsAgentresolve verifies
// that the RefusalError names the agentresolve component.
func TestResolveOrchestrator_NonexistentFile_ComponentIsAgentresolve(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "does-not-exist.md")

	_, err := agentresolve.ResolveOrchestrator(path)

	re := asRefusalErrorOrch(t, err)
	if re.Component != "agentresolve" {
		t.Errorf("want RefusalError.Component %q, got %q", "agentresolve", re.Component)
	}
}

// TestResolveOrchestrator_NonexistentFile_NamesPath verifies that the
// RefusalError includes the given path so the caller can surface it to the user.
func TestResolveOrchestrator_NonexistentFile_NamesPath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "does-not-exist.md")

	_, err := agentresolve.ResolveOrchestrator(path)

	re := asRefusalErrorOrch(t, err)
	// Path should appear either in Resource or in the full error string.
	if re.Resource != path && !strings.Contains(re.Error(), path) {
		t.Errorf("want error to name the path %q; got RefusalError.Resource=%q, Error()=%q",
			path, re.Resource, re.Error())
	}
}

// ===== Refusal: path is a directory =====

// TestResolveOrchestrator_Directory_ReturnsRefusalError verifies that a path
// naming an existing directory (not a file) produces a *domain.RefusalError.
func TestResolveOrchestrator_Directory_ReturnsRefusalError(t *testing.T) {
	dir := t.TempDir()

	_, err := agentresolve.ResolveOrchestrator(dir)

	asRefusalErrorOrch(t, err)
}

// TestResolveOrchestrator_Directory_ComponentIsAgentresolve verifies the
// RefusalError component for a directory path.
func TestResolveOrchestrator_Directory_ComponentIsAgentresolve(t *testing.T) {
	dir := t.TempDir()

	_, err := agentresolve.ResolveOrchestrator(dir)

	re := asRefusalErrorOrch(t, err)
	if re.Component != "agentresolve" {
		t.Errorf("want RefusalError.Component %q, got %q", "agentresolve", re.Component)
	}
}

// ===== Refusal: empty filename stem =====

// TestResolveOrchestrator_EmptyStem_ReturnsRefusalError verifies that a file
// whose name has an empty stem (e.g. ".md") produces a *domain.RefusalError.
// filepath.Ext(".md") == ".md", so TrimSuffix leaves an empty string.
func TestResolveOrchestrator_EmptyStem_ReturnsRefusalError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".md")
	if err := os.WriteFile(path, []byte(minimalWorkflowContent), 0600); err != nil {
		t.Fatalf("write .md file: %v", err)
	}

	_, err := agentresolve.ResolveOrchestrator(path)

	asRefusalErrorOrch(t, err)
}

// TestResolveOrchestrator_EmptyStem_ComponentIsAgentresolve verifies the
// RefusalError component for an empty-stem file.
func TestResolveOrchestrator_EmptyStem_ComponentIsAgentresolve(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".md")
	if err := os.WriteFile(path, []byte(minimalWorkflowContent), 0600); err != nil {
		t.Fatalf("write .md file: %v", err)
	}

	_, err := agentresolve.ResolveOrchestrator(path)

	re := asRefusalErrorOrch(t, err)
	if re.Component != "agentresolve" {
		t.Errorf("want RefusalError.Component %q, got %q", "agentresolve", re.Component)
	}
}

// ===== Separation from dispatchable agents =====

// TestResolveAll_OrchestratorFilePresent_NotIncludedWhenNotRequested verifies
// that calling ResolveAll with only workflow agent identifiers does not include
// the orchestrator identifier in the result, even when its file is in the same
// directory as the agent files. The orchestrator is a separate reference, not
// part of the dispatchable agent map.
func TestResolveAll_OrchestratorFilePresent_NotIncludedWhenNotRequested(t *testing.T) {
	dir := t.TempDir()
	writeOrchFile(t, dir, "orchestrator.md")
	// Write a workflow agent file.
	agentPath := filepath.Join(dir, "agent-a.md")
	if err := os.WriteFile(agentPath, []byte("# Agent-A\n"), 0600); err != nil {
		t.Fatalf("write agent-a.md: %v", err)
	}

	result, err := agentresolve.ResolveAll(dir, []string{"agent-a"})

	if err != nil {
		t.Fatalf("ResolveAll: %v", err)
	}
	if _, ok := result["orchestrator"]; ok {
		t.Error("want orchestrator absent from ResolveAll result when not in requested identifiers")
	}
	if len(result) != 1 {
		t.Errorf("want exactly 1 entry in result (only agent-a), got %d", len(result))
	}
}

// TestResolveOrchestrator_InvocationKindDistinguishesOrchestratorFromAgents
// verifies that the reference from ResolveOrchestrator carries a different
// InvocationKind than the references from ResolveAll. This is the observable
// property that prevents the orchestrator from being treated as an ordinary
// dispatchable agent.
func TestResolveOrchestrator_InvocationKindDistinguishesOrchestratorFromAgents(t *testing.T) {
	dir := t.TempDir()
	orchPath := writeOrchFile(t, dir, "orchestrator.md")
	agentPath := filepath.Join(dir, "agent-a.md")
	if err := os.WriteFile(agentPath, []byte("# Agent-A\n"), 0600); err != nil {
		t.Fatalf("write agent-a.md: %v", err)
	}

	orchRef, orchErr := agentresolve.ResolveOrchestrator(orchPath)
	agentMap, agentErr := agentresolve.ResolveAll(dir, []string{"agent-a"})

	if orchErr != nil {
		t.Fatalf("ResolveOrchestrator: %v", orchErr)
	}
	if agentErr != nil {
		t.Fatalf("ResolveAll: %v", agentErr)
	}

	agentRef := agentMap["agent-a"]
	// The orchestrator must not carry InvocationOrdinary (what agents carry).
	if orchRef.InvocationKind == domain.InvocationOrdinary {
		t.Error("want orchestrator reference to carry InvocationOrchestrator, not InvocationOrdinary")
	}
	// A workflow agent must carry InvocationOrdinary.
	if agentRef.InvocationKind != domain.InvocationOrdinary {
		t.Errorf("want agent reference to carry InvocationOrdinary, got %q", agentRef.InvocationKind)
	}
}

// TestResolveAll_MissingAgentRefusal_UnchangedByOrchestratorResolution
// verifies that calling ResolveAll for a missing agent returns *domain.RefusalError
// regardless of whether ResolveOrchestrator has been called. Ordinary agent
// resolution behaviour is unchanged by the introduction of orchestrator resolution.
func TestResolveAll_MissingAgentRefusal_UnchangedByOrchestratorResolution(t *testing.T) {
	dir := t.TempDir()
	orchPath := writeOrchFile(t, dir, "orchestrator.md")
	// "missing-agent" has no .md file in dir.

	// Call ResolveOrchestrator first (as the session will, after Stage 5).
	_, orchErr := agentresolve.ResolveOrchestrator(orchPath)
	// We do not assert on orchErr here; we are testing ResolveAll, not ResolveOrchestrator.
	_ = orchErr

	_, err := agentresolve.ResolveAll(dir, []string{"missing-agent"})

	if err == nil {
		t.Fatal("want error from ResolveAll for a missing agent, got nil")
	}
	var re *domain.RefusalError
	if !errors.As(err, &re) {
		t.Fatalf("want *domain.RefusalError from ResolveAll, got %T: %v", err, err)
	}
	if re.Component != "agentresolve" {
		t.Errorf("want RefusalError.Component %q, got %q", "agentresolve", re.Component)
	}
}
