package agentresolve_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mosaic-run/internal/agentresolve"
	"mosaic-run/internal/domain"
)

// --- .agent.md recognition ---

// TestResolveAll_AgentMdFile_ResolvesToBaseStem verifies that foo.agent.md
// resolves to the identifier "foo" (the compound extension is stripped).
func TestResolveAll_AgentMdFile_ResolvesToBaseStem(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "foo.agent.md"), []byte("# Foo\n"), 0600); err != nil {
		t.Fatalf("write foo.agent.md: %v", err)
	}

	result, err := agentresolve.ResolveAll(dir, []string{"foo"})

	if err != nil {
		t.Fatalf("ResolveAll: unexpected error: %v", err)
	}
	ref, ok := result["foo"]
	if !ok {
		t.Fatal("want result map to contain key \"foo\" resolved from foo.agent.md")
	}
	if ref.Identifier != "foo" {
		t.Errorf("AgentReference.Identifier: want %q, got %q", "foo", ref.Identifier)
	}
}

// TestResolveAll_AgentMdFile_DefinitionPathEndsWithAgentMd verifies that the
// DefinitionPath for a .agent.md file ends with the original .agent.md filename.
func TestResolveAll_AgentMdFile_DefinitionPathEndsWithAgentMd(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "foo.agent.md"), []byte("# Foo\n"), 0600); err != nil {
		t.Fatalf("write foo.agent.md: %v", err)
	}

	result, err := agentresolve.ResolveAll(dir, []string{"foo"})

	if err != nil {
		t.Fatalf("ResolveAll: %v", err)
	}
	ref := result["foo"]
	if !strings.HasSuffix(ref.DefinitionPath, "foo.agent.md") {
		t.Errorf("DefinitionPath must end with %q; got %q", "foo.agent.md", ref.DefinitionPath)
	}
}

// TestResolveAll_AgentMdFile_InvocationKindIsOrdinary verifies that a
// .agent.md file resolves to a reference carrying InvocationOrdinary, the
// same as a .md file.
func TestResolveAll_AgentMdFile_InvocationKindIsOrdinary(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "foo.agent.md"), []byte("# Foo\n"), 0600); err != nil {
		t.Fatalf("write foo.agent.md: %v", err)
	}

	result, err := agentresolve.ResolveAll(dir, []string{"foo"})

	if err != nil {
		t.Fatalf("ResolveAll: %v", err)
	}
	ref := result["foo"]
	if ref.InvocationKind != domain.InvocationOrdinary {
		t.Errorf("InvocationKind: want %q, got %q", domain.InvocationOrdinary, ref.InvocationKind)
	}
}

// --- Cross-format duplicate guard ---

// TestResolveAll_CrossFormatDuplicate_RequestedIdentifier_ReturnsRefusalError
// verifies that foo.md and foo.agent.md coexisting in the same directory
// produces a RefusalError even when "foo" is in the requested identifiers.
func TestResolveAll_CrossFormatDuplicate_RequestedIdentifier_ReturnsRefusalError(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "foo.md"), []byte("# Foo\n"), 0600); err != nil {
		t.Fatalf("write foo.md: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "foo.agent.md"), []byte("# Foo Agent\n"), 0600); err != nil {
		t.Fatalf("write foo.agent.md: %v", err)
	}

	_, err := agentresolve.ResolveAll(dir, []string{"foo"})

	if err == nil {
		t.Fatal("want RefusalError for cross-format duplicate (foo.md + foo.agent.md), got nil")
	}
	asRefusalError(t, err)
}

// TestResolveAll_CrossFormatDuplicate_RequestedIdentifier_ComponentIsAgentresolve
// verifies the RefusalError component when a cross-format duplicate is detected.
func TestResolveAll_CrossFormatDuplicate_RequestedIdentifier_ComponentIsAgentresolve(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "foo.md"), []byte("# Foo\n"), 0600); err != nil {
		t.Fatalf("write foo.md: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "foo.agent.md"), []byte("# Foo Agent\n"), 0600); err != nil {
		t.Fatalf("write foo.agent.md: %v", err)
	}

	_, err := agentresolve.ResolveAll(dir, []string{"foo"})

	re := asRefusalError(t, err)
	if re.Component != "agentresolve" {
		t.Errorf("RefusalError.Component: want %q, got %q", "agentresolve", re.Component)
	}
}

// TestResolveAll_CrossFormatDuplicate_UnrequestedIdentifier_ReturnsRefusalError
// verifies that the cross-format duplicate guard fires even when the conflicting
// identifier is absent from the requested set. The guard is a directory-level
// invariant, not filtered by the requested identifiers.
func TestResolveAll_CrossFormatDuplicate_UnrequestedIdentifier_ReturnsRefusalError(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "foo.md"), []byte("# Foo\n"), 0600); err != nil {
		t.Fatalf("write foo.md: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "foo.agent.md"), []byte("# Foo Agent\n"), 0600); err != nil {
		t.Fatalf("write foo.agent.md: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "bar.md"), []byte("# Bar\n"), 0600); err != nil {
		t.Fatalf("write bar.md: %v", err)
	}

	// Request only "bar" -- "foo" (which has the conflict) is not in the requested set.
	_, err := agentresolve.ResolveAll(dir, []string{"bar"})

	if err == nil {
		t.Fatal("want RefusalError for cross-format duplicate even when conflicting identifier is not requested, got nil")
	}
	asRefusalError(t, err)
}

// --- Mixed .md and .agent.md in same directory ---

// TestResolveAll_MixedFormats_ResolvesBothCorrectly verifies that a directory
// may contain some agents as .md and others as .agent.md. Both forms must
// resolve to their respective identifiers without conflict.
func TestResolveAll_MixedFormats_ResolvesBothCorrectly(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "bar.md"), []byte("# Bar\n"), 0600); err != nil {
		t.Fatalf("write bar.md: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "baz.agent.md"), []byte("# Baz\n"), 0600); err != nil {
		t.Fatalf("write baz.agent.md: %v", err)
	}

	result, err := agentresolve.ResolveAll(dir, []string{"bar", "baz"})

	if err != nil {
		t.Fatalf("ResolveAll: unexpected error: %v", err)
	}
	if len(result) != 2 {
		t.Errorf("want 2 entries in result map, got %d", len(result))
	}
	barRef, ok := result["bar"]
	if !ok {
		t.Error("result map must contain key \"bar\"")
	} else if !strings.HasSuffix(barRef.DefinitionPath, "bar.md") {
		t.Errorf("bar DefinitionPath must end with \"bar.md\"; got %q", barRef.DefinitionPath)
	}
	bazRef, ok := result["baz"]
	if !ok {
		t.Error("result map must contain key \"baz\"")
	} else if !strings.HasSuffix(bazRef.DefinitionPath, "baz.agent.md") {
		t.Errorf("baz DefinitionPath must end with \"baz.agent.md\"; got %q", bazRef.DefinitionPath)
	}
}

// --- Case-insensitive extension matching ---

// TestResolveAll_CaseInsensitiveAgentMdSuffix_Resolves verifies that .AGENT.MD
// (all uppercase) is recognized as the .agent.md compound extension and the
// file resolves to its base identifier.
func TestResolveAll_CaseInsensitiveAgentMdSuffix_Resolves(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "foo.AGENT.MD"), []byte("# Foo\n"), 0600); err != nil {
		t.Fatalf("write foo.AGENT.MD: %v", err)
	}

	result, err := agentresolve.ResolveAll(dir, []string{"foo"})

	if err != nil {
		t.Fatalf("ResolveAll: unexpected error: %v", err)
	}
	if _, ok := result["foo"]; !ok {
		t.Fatal("want result map to contain key \"foo\" resolved from foo.AGENT.MD")
	}
}

// --- Empty stem: .agent.md with no base name ---

// TestResolveAll_EmptyStemAgentMd_IsIgnored verifies that a file named exactly
// ".agent.md" (empty base stem) is ignored and produces no resolution entry.
// Requesting the synthetic identifier ".agent" must fail with a RefusalError.
func TestResolveAll_EmptyStemAgentMd_IsIgnored(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".agent.md"), []byte("# Not an agent\n"), 0600); err != nil {
		t.Fatalf("write .agent.md: %v", err)
	}

	// Requesting ".agent" should fail: the file has an empty stem and is ignored.
	_, err := agentresolve.ResolveAll(dir, []string{".agent"})

	if err == nil {
		t.Fatal("want RefusalError because .agent.md (empty stem) must be ignored and produce no resolution entry")
	}
	asRefusalError(t, err)
}

// --- Empty identifiers with cross-format duplicate ---

// TestResolveAll_EmptyIdentifiers_CrossFormatDuplicate_ReturnsRefusalError
// verifies that the duplicate scan runs before the early-return path for empty
// identifiers. A directory containing foo.md and foo.agent.md must produce a
// RefusalError even when the requested identifiers slice is empty.
func TestResolveAll_EmptyIdentifiers_CrossFormatDuplicate_ReturnsRefusalError(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "foo.md"), []byte("# Foo\n"), 0600); err != nil {
		t.Fatalf("write foo.md: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "foo.agent.md"), []byte("# Foo Agent\n"), 0600); err != nil {
		t.Fatalf("write foo.agent.md: %v", err)
	}

	_, err := agentresolve.ResolveAll(dir, []string{})

	if err == nil {
		t.Fatal("want RefusalError for cross-format duplicate even with empty identifiers, got nil")
	}
	asRefusalError(t, err)
}

// TestResolveAll_EmptyIdentifiers_NonexistentDir_ReturnsRefusalError verifies
// that a non-existent directory produces a RefusalError even when no
// identifiers are requested. The directory must be read for the duplicate scan.
func TestResolveAll_EmptyIdentifiers_NonexistentDir_ReturnsRefusalError(t *testing.T) {
	nonExistentDir := filepath.Join(t.TempDir(), "does-not-exist")

	_, err := agentresolve.ResolveAll(nonExistentDir, []string{})

	if err == nil {
		t.Fatal("want RefusalError for non-existent directory even with empty identifiers, got nil")
	}
	re := asRefusalError(t, err)
	if re.Component != "agentresolve" {
		t.Errorf("RefusalError.Component: want %q, got %q", "agentresolve", re.Component)
	}
}
