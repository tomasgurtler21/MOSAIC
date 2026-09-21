package agentresolve_test

// Tests for agentresolve.ResolveAll.
//
// Coverage:
//
//   Happy path - single agent:
//   - Directory with one .md file; requesting that agent returns a non-nil map.
//   - AgentReference.Identifier matches the requested identifier.
//   - AgentReference.DefinitionPath ends with the expected filename.
//   - Returned map is keyed by the agent identifier.
//
//   Happy path - multiple agents:
//   - Directory with three .md files; requesting all three returns a map with
//     three entries.
//   - Each entry carries the correct Identifier.
//   - Each entry's DefinitionPath names a file that exists in the directory.
//   - Requesting a subset of identifiers returns only the requested subset.
//
//   Happy path - empty identifiers:
//   - ResolveAll with an empty identifiers slice returns an empty map and no error.
//
//   Refusal - missing agent:
//   - Requesting an identifier that has no matching .md file returns an error.
//   - The error wraps *domain.RefusalError.
//   - RefusalError.Component is "agentresolve".
//   - RefusalError.Resource or message names the missing identifier.
//   - RefusalError.Resource or message names the directory searched.
//
//   Note on duplicate-identifier guard: the matching rule (exact filename stem)
//   makes it impossible to have two files with the same stem in the same
//   directory. The guard exists as a safety net for future changes to the
//   matching rule and cannot be triggered by fixture-based tests.

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mosaic-run/internal/agentresolve"
	"mosaic-run/internal/domain"
)

const agentresolveTestdataDir = "../../testdata/agentresolve"

// agentsDir returns the path to the fixture directory containing agent files.
func agentsDir() string {
	return filepath.Join(agentresolveTestdataDir, "agents")
}

// asRefusalError asserts that err wraps a *domain.RefusalError and returns it.
// Calls t.Fatal on failure.
func asRefusalError(t *testing.T, err error) *domain.RefusalError {
	t.Helper()
	var re *domain.RefusalError
	if !errors.As(err, &re) {
		t.Fatalf("want *domain.RefusalError, got %T: %v", err, err)
	}
	return re
}

// --- Happy path: single agent ---

func TestResolveAll_SingleAgent_ReturnsNonNilMap(t *testing.T) {
	result, err := agentresolve.ResolveAll(agentsDir(), []string{"test-writer-tdd"})

	if err != nil {
		t.Fatalf("ResolveAll returned unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("ResolveAll returned nil map")
	}
}

func TestResolveAll_SingleAgent_MapContainsRequestedKey(t *testing.T) {
	result, err := agentresolve.ResolveAll(agentsDir(), []string{"test-writer-tdd"})
	if err != nil {
		t.Fatalf("ResolveAll: %v", err)
	}

	if _, ok := result["test-writer-tdd"]; !ok {
		t.Errorf("result map must contain key %q", "test-writer-tdd")
	}
}

func TestResolveAll_SingleAgent_AgentReference_IdentifierMatches(t *testing.T) {
	result, err := agentresolve.ResolveAll(agentsDir(), []string{"test-writer-tdd"})
	if err != nil {
		t.Fatalf("ResolveAll: %v", err)
	}

	ref := result["test-writer-tdd"]
	if ref.Identifier != "test-writer-tdd" {
		t.Errorf("AgentReference.Identifier: want %q, got %q", "test-writer-tdd", ref.Identifier)
	}
}

func TestResolveAll_SingleAgent_AgentReference_DefinitionPath_EndsWithFilename(t *testing.T) {
	// The resolved path must end with "test-writer-tdd.md" so that the harness
	// adapter can locate the agent definition file.
	result, err := agentresolve.ResolveAll(agentsDir(), []string{"test-writer-tdd"})
	if err != nil {
		t.Fatalf("ResolveAll: %v", err)
	}

	ref := result["test-writer-tdd"]
	if !strings.HasSuffix(ref.DefinitionPath, "test-writer-tdd.md") {
		t.Errorf("DefinitionPath must end with %q; got %q", "test-writer-tdd.md", ref.DefinitionPath)
	}
}

// --- Happy path: multiple agents ---

func TestResolveAll_MultipleAgents_ReturnsAllRequested(t *testing.T) {
	// The agents directory contains test-writer-tdd.md, implementation-tdd.md,
	// and build-review.md.
	ids := []string{"test-writer-tdd", "implementation-tdd", "build-review"}
	result, err := agentresolve.ResolveAll(agentsDir(), ids)

	if err != nil {
		t.Fatalf("ResolveAll returned unexpected error: %v", err)
	}
	if len(result) != 3 {
		t.Errorf("want 3 entries in result map, got %d", len(result))
	}
}

func TestResolveAll_MultipleAgents_EachIdentifierCorrect(t *testing.T) {
	ids := []string{"test-writer-tdd", "implementation-tdd", "build-review"}
	result, err := agentresolve.ResolveAll(agentsDir(), ids)
	if err != nil {
		t.Fatalf("ResolveAll: %v", err)
	}

	for _, id := range ids {
		ref, ok := result[id]
		if !ok {
			t.Errorf("result map missing key %q", id)
			continue
		}
		if ref.Identifier != id {
			t.Errorf("result[%q].Identifier: want %q, got %q", id, id, ref.Identifier)
		}
	}
}

func TestResolveAll_MultipleAgents_EachDefinitionPathNamesCorrectFile(t *testing.T) {
	ids := []string{"test-writer-tdd", "implementation-tdd", "build-review"}
	result, err := agentresolve.ResolveAll(agentsDir(), ids)
	if err != nil {
		t.Fatalf("ResolveAll: %v", err)
	}

	for _, id := range ids {
		ref := result[id]
		expectedSuffix := id + ".md"
		if !strings.HasSuffix(ref.DefinitionPath, expectedSuffix) {
			t.Errorf("result[%q].DefinitionPath must end with %q; got %q", id, expectedSuffix, ref.DefinitionPath)
		}
	}
}

func TestResolveAll_Subset_ReturnsOnlyRequestedEntries(t *testing.T) {
	// Requesting only one of the three available agents must return a map
	// with exactly one entry, not all agents in the directory.
	result, err := agentresolve.ResolveAll(agentsDir(), []string{"build-review"})
	if err != nil {
		t.Fatalf("ResolveAll: %v", err)
	}

	if len(result) != 1 {
		t.Errorf("requesting 1 identifier must return 1-entry map, got %d", len(result))
	}
	if _, ok := result["build-review"]; !ok {
		t.Error("result map must contain the requested key \"build-review\"")
	}
}

// --- Happy path: empty identifiers ---

func TestResolveAll_EmptyIdentifiers_ReturnsEmptyMap(t *testing.T) {
	result, err := agentresolve.ResolveAll(agentsDir(), []string{})

	if err != nil {
		t.Fatalf("ResolveAll with empty identifiers must not return an error; got: %v", err)
	}
	if len(result) != 0 {
		t.Errorf("want empty map, got %d entries", len(result))
	}
}

// --- Refusal: missing agent ---

func TestResolveAll_MissingAgent_ReturnsError(t *testing.T) {
	// "planner" does not exist in the agents directory.
	_, err := agentresolve.ResolveAll(agentsDir(), []string{"planner"})

	if err == nil {
		t.Fatal("requesting a missing agent must return an error")
	}
}

func TestResolveAll_MissingAgent_ReturnsRefusalError(t *testing.T) {
	_, err := agentresolve.ResolveAll(agentsDir(), []string{"planner"})

	asRefusalError(t, err)
}

func TestResolveAll_MissingAgent_RefusalError_ComponentIsAgentresolve(t *testing.T) {
	_, err := agentresolve.ResolveAll(agentsDir(), []string{"planner"})

	re := asRefusalError(t, err)
	if re.Component != "agentresolve" {
		t.Errorf("RefusalError.Component: want %q, got %q", "agentresolve", re.Component)
	}
}

func TestResolveAll_MissingAgent_RefusalError_NamesIdentifier(t *testing.T) {
	// The error must name "planner" so the user knows which agent definition
	// is missing, without having to inspect the directory manually.
	_, err := agentresolve.ResolveAll(agentsDir(), []string{"planner"})

	re := asRefusalError(t, err)
	if !strings.Contains(re.Error(), "planner") {
		t.Errorf("RefusalError must name the missing identifier %q; got %q", "planner", re.Error())
	}
}

func TestResolveAll_MissingAgent_RefusalError_NamesDirectory(t *testing.T) {
	// The error must name the directory that was searched so the user knows
	// where to place the missing agent definition file.
	dir := agentsDir()
	_, err := agentresolve.ResolveAll(dir, []string{"planner"})

	re := asRefusalError(t, err)
	// The directory name or a recognisable part of it must appear in the error.
	if !strings.Contains(re.Error(), "agents") {
		t.Errorf("RefusalError must mention the searched directory; got %q", re.Error())
	}
}

func TestResolveAll_OneMissingOnePresent_ReturnsRefusalError(t *testing.T) {
	// Even when some identifiers resolve successfully, a single missing one
	// must produce a refusal error rather than silently returning a partial map.
	_, err := agentresolve.ResolveAll(agentsDir(), []string{"test-writer-tdd", "planner"})

	if err == nil {
		t.Fatal("a single missing agent among multiple requested must return an error")
	}
	asRefusalError(t, err)
}

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
