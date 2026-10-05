package agentresolve_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mosaic-run/internal/agentresolve"
)

// ===== ResolveOne =====
//
// Coverage:
//
//   Refusal - empty identifier:
//   - An empty id string returns *domain.RefusalError.
//   - RefusalError.Component is "agentresolve".
//
//   Refusal - no matching file:
//   - An id with no matching file in the directory returns *domain.RefusalError.
//   - RefusalError.Component is "agentresolve".
//
//   Happy path - single .md file:
//   - Exactly one matching .md file returns the AgentReference with the correct
//     Identifier, a DefinitionPath ending with the .md filename, and
//     InvocationKind set to "" (empty string).
//
//   Happy path - single .agent.md file:
//   - Exactly one matching .agent.md file returns the AgentReference with the
//     correct Identifier, a DefinitionPath ending with the .agent.md filename,
//     and InvocationKind set to "" (empty string).
//
//   Refusal - ambiguous (both .md and .agent.md present):
//   - A directory containing both foo.md and foo.agent.md returns
//     *domain.RefusalError because two files match the same identifier.
//   - RefusalError.Component is "agentresolve".
//
//   Refusal - directory not found:
//   - A non-existent directory returns *domain.RefusalError.
//   - RefusalError.Component is "agentresolve".

// --- ResolveOne: empty identifier ---

// TestResolveOne_EmptyID_ReturnsRefusalError verifies that passing an empty
// string as the agent identifier returns *domain.RefusalError immediately,
// without attempting to read the directory.
func TestResolveOne_EmptyID_ReturnsRefusalError(t *testing.T) {
	dir := t.TempDir()

	_, err := agentresolve.ResolveOne(dir, "")

	if err == nil {
		t.Fatal("want RefusalError for empty id, got nil")
	}
	asRefusalError(t, err)
}

// TestResolveOne_EmptyID_ComponentIsAgentresolve verifies the RefusalError
// component when an empty identifier is given.
func TestResolveOne_EmptyID_ComponentIsAgentresolve(t *testing.T) {
	dir := t.TempDir()

	_, err := agentresolve.ResolveOne(dir, "")

	re := asRefusalError(t, err)
	if re.Component != "agentresolve" {
		t.Errorf("RefusalError.Component: want %q, got %q", "agentresolve", re.Component)
	}
}

// --- ResolveOne: no matching file ---

// TestResolveOne_NoMatch_ReturnsRefusalError verifies that an identifier with
// no corresponding file in the directory returns *domain.RefusalError.
func TestResolveOne_NoMatch_ReturnsRefusalError(t *testing.T) {
	dir := t.TempDir()
	// Directory is empty; no file will match.

	_, err := agentresolve.ResolveOne(dir, "nonexistent-agent")

	if err == nil {
		t.Fatal("want RefusalError when no file matches the identifier, got nil")
	}
	asRefusalError(t, err)
}

// TestResolveOne_NoMatch_ComponentIsAgentresolve verifies the RefusalError
// component when no file matches the requested identifier.
func TestResolveOne_NoMatch_ComponentIsAgentresolve(t *testing.T) {
	dir := t.TempDir()

	_, err := agentresolve.ResolveOne(dir, "nonexistent-agent")

	re := asRefusalError(t, err)
	if re.Component != "agentresolve" {
		t.Errorf("RefusalError.Component: want %q, got %q", "agentresolve", re.Component)
	}
}

// --- ResolveOne: exactly one .md file ---

// TestResolveOne_SingleMdFile_ReturnsReference verifies that exactly one
// matching .md file causes ResolveOne to return a non-error AgentReference.
func TestResolveOne_SingleMdFile_ReturnsReference(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "my-agent.md"), []byte("# My Agent\n"), 0600); err != nil {
		t.Fatalf("write my-agent.md: %v", err)
	}

	ref, err := agentresolve.ResolveOne(dir, "my-agent")

	if err != nil {
		t.Fatalf("ResolveOne: unexpected error: %v", err)
	}
	if ref.Identifier != "my-agent" {
		t.Errorf("Identifier: want %q, got %q", "my-agent", ref.Identifier)
	}
}

// TestResolveOne_SingleMdFile_DefinitionPathEndsWithFilename verifies that the
// returned DefinitionPath ends with the matched .md filename.
func TestResolveOne_SingleMdFile_DefinitionPathEndsWithFilename(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "my-agent.md"), []byte("# My Agent\n"), 0600); err != nil {
		t.Fatalf("write my-agent.md: %v", err)
	}

	ref, err := agentresolve.ResolveOne(dir, "my-agent")

	if err != nil {
		t.Fatalf("ResolveOne: %v", err)
	}
	if !strings.HasSuffix(ref.DefinitionPath, "my-agent.md") {
		t.Errorf("DefinitionPath must end with %q; got %q", "my-agent.md", ref.DefinitionPath)
	}
}

// TestResolveOne_SingleMdFile_InvocationKindIsEmpty verifies that the
// InvocationKind field in the returned reference is "" (empty string), as
// specified in the ResolveOne contract.
func TestResolveOne_SingleMdFile_InvocationKindIsEmpty(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "my-agent.md"), []byte("# My Agent\n"), 0600); err != nil {
		t.Fatalf("write my-agent.md: %v", err)
	}

	ref, err := agentresolve.ResolveOne(dir, "my-agent")

	if err != nil {
		t.Fatalf("ResolveOne: %v", err)
	}
	if ref.InvocationKind != "" {
		t.Errorf("InvocationKind: want %q (empty), got %q", "", ref.InvocationKind)
	}
}

// --- ResolveOne: exactly one .agent.md file ---

// TestResolveOne_SingleAgentMdFile_ReturnsReference verifies that exactly one
// matching .agent.md file causes ResolveOne to return a non-error AgentReference
// with the correct identifier (base stem, not full compound extension).
func TestResolveOne_SingleAgentMdFile_ReturnsReference(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "my-agent.agent.md"), []byte("# My Agent\n"), 0600); err != nil {
		t.Fatalf("write my-agent.agent.md: %v", err)
	}

	ref, err := agentresolve.ResolveOne(dir, "my-agent")

	if err != nil {
		t.Fatalf("ResolveOne: unexpected error: %v", err)
	}
	if ref.Identifier != "my-agent" {
		t.Errorf("Identifier: want %q, got %q", "my-agent", ref.Identifier)
	}
}

// TestResolveOne_SingleAgentMdFile_DefinitionPathEndsWithAgentMd verifies that
// the returned DefinitionPath ends with the .agent.md filename, preserving the
// original file extension.
func TestResolveOne_SingleAgentMdFile_DefinitionPathEndsWithAgentMd(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "my-agent.agent.md"), []byte("# My Agent\n"), 0600); err != nil {
		t.Fatalf("write my-agent.agent.md: %v", err)
	}

	ref, err := agentresolve.ResolveOne(dir, "my-agent")

	if err != nil {
		t.Fatalf("ResolveOne: %v", err)
	}
	if !strings.HasSuffix(ref.DefinitionPath, "my-agent.agent.md") {
		t.Errorf("DefinitionPath must end with %q; got %q", "my-agent.agent.md", ref.DefinitionPath)
	}
}

// TestResolveOne_SingleAgentMdFile_InvocationKindIsEmpty verifies that the
// InvocationKind field is "" when a .agent.md file is matched.
func TestResolveOne_SingleAgentMdFile_InvocationKindIsEmpty(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "my-agent.agent.md"), []byte("# My Agent\n"), 0600); err != nil {
		t.Fatalf("write my-agent.agent.md: %v", err)
	}

	ref, err := agentresolve.ResolveOne(dir, "my-agent")

	if err != nil {
		t.Fatalf("ResolveOne: %v", err)
	}
	if ref.InvocationKind != "" {
		t.Errorf("InvocationKind: want %q (empty), got %q", "", ref.InvocationKind)
	}
}

// --- ResolveOne: ambiguous (both .md and .agent.md present) ---

// TestResolveOne_BothMdAndAgentMd_ReturnsRefusalError verifies that a directory
// containing both foo.md and foo.agent.md returns *domain.RefusalError because
// two files match the same identifier and the result is ambiguous.
func TestResolveOne_BothMdAndAgentMd_ReturnsRefusalError(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "my-agent.md"), []byte("# My Agent\n"), 0600); err != nil {
		t.Fatalf("write my-agent.md: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "my-agent.agent.md"), []byte("# My Agent\n"), 0600); err != nil {
		t.Fatalf("write my-agent.agent.md: %v", err)
	}

	_, err := agentresolve.ResolveOne(dir, "my-agent")

	if err == nil {
		t.Fatal("want RefusalError when both .md and .agent.md match the same identifier, got nil")
	}
	asRefusalError(t, err)
}

// TestResolveOne_BothMdAndAgentMd_ComponentIsAgentresolve verifies the
// RefusalError component when an ambiguous match is detected.
func TestResolveOne_BothMdAndAgentMd_ComponentIsAgentresolve(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "my-agent.md"), []byte("# My Agent\n"), 0600); err != nil {
		t.Fatalf("write my-agent.md: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "my-agent.agent.md"), []byte("# My Agent\n"), 0600); err != nil {
		t.Fatalf("write my-agent.agent.md: %v", err)
	}

	_, err := agentresolve.ResolveOne(dir, "my-agent")

	re := asRefusalError(t, err)
	if re.Component != "agentresolve" {
		t.Errorf("RefusalError.Component: want %q, got %q", "agentresolve", re.Component)
	}
}

// --- ResolveOne: directory not found ---

// TestResolveOne_NonexistentDir_ReturnsRefusalError verifies that ResolveOne
// returns *domain.RefusalError when the given directory does not exist.
func TestResolveOne_NonexistentDir_ReturnsRefusalError(t *testing.T) {
	nonExistentDir := filepath.Join(t.TempDir(), "does-not-exist")

	_, err := agentresolve.ResolveOne(nonExistentDir, "some-agent")

	if err == nil {
		t.Fatal("want RefusalError for non-existent directory, got nil")
	}
	asRefusalError(t, err)
}

// TestResolveOne_NonexistentDir_ComponentIsAgentresolve verifies the
// RefusalError component when the directory does not exist.
func TestResolveOne_NonexistentDir_ComponentIsAgentresolve(t *testing.T) {
	nonExistentDir := filepath.Join(t.TempDir(), "does-not-exist")

	_, err := agentresolve.ResolveOne(nonExistentDir, "some-agent")

	re := asRefusalError(t, err)
	if re.Component != "agentresolve" {
		t.Errorf("RefusalError.Component: want %q, got %q", "agentresolve", re.Component)
	}
}
