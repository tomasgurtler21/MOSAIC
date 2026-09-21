package app

// harness_only_discovery_codex_test.go covers the extension-driven discovery behaviours
// introduced in Stage 9.
//
// All tested functions are package-internal, so this file belongs to package app (not
// package app_test), giving it direct access to unexported symbols -- the same convention
// established throughout the harness-only suite.
//
// This file imports _ "mosaic-deploy/internal/agentformat/all" so that the Codex TOML
// translator is registered for the scan-level tests that route through the decode funnel
// with a Codex HarnessDescriptor. Tests in this file must NOT blank-import
// internal/agentformat/codextoml directly -- that bypasses the wiring-package breakage
// guard. See the wiring contract in internal/agentformat/all/all.go.
//
// Verified behaviours:
//
// finalSuffix (CD-5, key derivation primitive):
//   - A single-component extension returns itself.
//   - A two-component extension returns only the final dotted part.
//
// agentKeyFromFileName -- Codex and GHCP CLI key derivation:
//   - A .toml file's key strips the .toml suffix.
//   - A .agent.toml file's key strips the full .agent.toml suffix.
//   - The same logical agent derives the same key under both .md and .toml extensions.
//   - A GHCP CLI plain .md file's key is identical to its .agent.md counterpart.
//
// isAgentFile -- extension-driven file acceptance:
//   - A .toml file is accepted by a Codex harness (agentExt ".toml").
//   - A .md file is rejected by a Codex harness.
//   - A plain .md file is accepted by a GHCP CLI harness (agentExt ".agent.md"),
//     because finalSuffix(".agent.md") == ".md".
//
// mosaicOrchestratorFileNames -- per-extension name set (T9.2):
//   - For a Markdown extension (.md) the set contains exactly the four existing names
//     including the .agent.md variants.
//   - For a GHCP CLI extension (.agent.md) the set is identical to the .md set
//     (finalSuffix(".agent.md") == ".md" and nothing is removed from any existing set).
//   - For a Codex extension (.toml) the set contains exactly the four .toml names.
//   - Every extension yields a set with exactly four members.
//
// isOrchestratorFileName -- Codex-extension orchestrator classification (T9.2):
//   - orchestrator.toml and its three variants are excluded for a Codex descriptor.
//   - Matching is case-insensitive (ORCHESTRATOR.TOML -> true).
//   - Whole-name rule: orchestrator-script-custom.toml is not excluded.
//
// scanHarnessOnlyAgents -- scan-level extension-driven behaviours (T9.1, T9.2):
//   - A Codex agents directory containing eligible .toml files yields the expected agents.
//   - .md files in a Codex agents directory are ignored by the scan.
//   - Key derivation for a .toml file strips the .toml suffix correctly.
//   - A GHCP CLI agents directory still discovers plain foo.md files as harness-only agents.
//   - orchestrator.toml is excluded for a Codex descriptor (T9.2).
//   - orchestrator-script.toml is excluded for a Codex descriptor (T9.2).
//   - A file whose name merely begins with "orchestrator-script" is not excluded;
//     only exact whole-name matches are rejected (T9.2 whole-name rule).

import (
	"os"
	"path/filepath"
	"testing"

	_ "mosaic-deploy/internal/agentformat/all"
	"mosaic-deploy/internal/domain"
)

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

// codexHarnessDescriptor returns a minimal HarnessDescriptor for the Codex harness:
// agent extension ".toml" and format ID "codex-toml". The format ID makes the decode
// funnel route through the Codex TOML translator.
func codexHarnessDescriptor() *domain.HarnessDescriptor {
	return &domain.HarnessDescriptor{
		AgentFormatID: "codex-toml",
		Extensions: map[domain.ArtifactKind]string{
			domain.ArtifactAgent: ".toml",
		},
	}
}

// ghcpCliHarnessDescriptor returns a minimal HarnessDescriptor for the GHCP CLI harness:
// agent extension ".agent.md". The Markdown identity translator handles decoding.
func ghcpCliHarnessDescriptor() *domain.HarnessDescriptor {
	return &domain.HarnessDescriptor{
		Extensions: map[domain.ArtifactKind]string{
			domain.ArtifactAgent: ".agent.md",
		},
	}
}

// minimalEligibleTomlAgentBytes returns the bytes of a minimal eligible Codex TOML agent
// file. The stamp block carries mosaic_transform_version (signal one of eligibility). The
// developer_instructions body carries a properly paired canonical <Identity type="core">
// section (signal two of eligibility). Decoding through the Codex translator yields
// canonical bytes that pass eligibleHarnessOnly.
//
// The file's TOML name field is set to "harness-only-agent" as a placeholder; the agent
// key is always derived from the file name by agentKeyFromFileName, not from the name field.
func minimalEligibleTomlAgentBytes() []byte {
	return []byte(
		"# mosaic_transform_version: 1.0.0\n" +
			"# mosaic_version: 2.1.0\n" +
			"\n" +
			"name = \"harness-only-agent\"\n" +
			"sandbox_mode = \"read-only\"\n" +
			"developer_instructions = \"\"\"\n" +
			"<Identity type=\"core\">\n" +
			"This agent provides identity content.\n" +
			"</Identity>\n" +
			"\"\"\"\n")
}

// ---------------------------------------------------------------------------
// finalSuffix -- the key derivation primitive (CD-5)
// ---------------------------------------------------------------------------

// TestFinalSuffix_SingleComponentExtension_ReturnsItself verifies that an extension with a
// single dot component is returned unchanged. ".toml" and ".md" are the two single-component
// extensions in use today; both must pass through as-is.
func TestFinalSuffix_SingleComponentExtension_ReturnsItself(t *testing.T) {
	for _, ext := range []string{".toml", ".md"} {
		got := finalSuffix(ext)
		if got != ext {
			t.Errorf("finalSuffix(%q) = %q; want %q (single-component extension must be returned unchanged)",
				ext, got, ext)
		}
	}
}

// TestFinalSuffix_TwoComponentExtension_ReturnsFinalPart verifies that a two-component
// extension returns only the final dotted component. Both ".agent.md" (GHCP CLI) and
// ".agent.toml" yield ".md" and ".toml" respectively.
func TestFinalSuffix_TwoComponentExtension_ReturnsFinalPart(t *testing.T) {
	cases := []struct {
		ext  string
		want string
	}{
		{".agent.md", ".md"},
		{".agent.toml", ".toml"},
	}
	for _, c := range cases {
		got := finalSuffix(c.ext)
		if got != c.want {
			t.Errorf("finalSuffix(%q) = %q; want %q (final dotted component only)",
				c.ext, got, c.want)
		}
	}
}

// ---------------------------------------------------------------------------
// agentKeyFromFileName -- key derivation for .toml and cross-extension equality
// ---------------------------------------------------------------------------

// TestAgentKeyFromFileName_TomlFile_StripsTomlSuffix verifies that a plain .toml file
// (e.g. "my-agent.toml") has its .toml suffix stripped to yield the agent key "my-agent".
func TestAgentKeyFromFileName_TomlFile_StripsTomlSuffix(t *testing.T) {
	got := agentKeyFromFileName("my-agent.toml", ".toml")
	if got != "my-agent" {
		t.Errorf("agentKeyFromFileName(%q, %q) = %q; want %q",
			"my-agent.toml", ".toml", got, "my-agent")
	}
}

// TestAgentKeyFromFileName_AgentTomlFile_StripsFullSuffix verifies that a file using the
// double-extension form "my-agent.agent.toml" has the entire ".agent.toml" suffix stripped
// to yield the agent key "my-agent". No suffix fragment must leak into the key.
func TestAgentKeyFromFileName_AgentTomlFile_StripsFullSuffix(t *testing.T) {
	got := agentKeyFromFileName("my-agent.agent.toml", ".toml")
	if got != "my-agent" {
		t.Errorf("agentKeyFromFileName(%q, %q) = %q; want %q; "+
			"the full .agent.toml suffix must be stripped, not just the .toml part",
			"my-agent.agent.toml", ".toml", got, "my-agent")
	}
}

// TestAgentKeyFromFileName_SameLogicalAgent_EqualKeyAcrossMdAndToml verifies that the same
// logical agent produces the same derived key regardless of which extension the harness
// uses. "my-agent.md" under a Markdown harness and "my-agent.toml" under a Codex harness
// must both yield the key "my-agent". This equality is required so that a generic catalog
// counterpart check (catalogKeys["my-agent"]) works consistently across harnesses.
func TestAgentKeyFromFileName_SameLogicalAgent_EqualKeyAcrossMdAndToml(t *testing.T) {
	mdKey := agentKeyFromFileName("my-agent.md", ".md")
	tomlKey := agentKeyFromFileName("my-agent.toml", ".toml")
	if mdKey != tomlKey {
		t.Errorf("same logical agent derives different keys under different extensions: "+
			"agentKeyFromFileName(%q, %q) = %q, "+
			"agentKeyFromFileName(%q, %q) = %q; "+
			"both must yield the same key",
			"my-agent.md", ".md", mdKey,
			"my-agent.toml", ".toml", tomlKey)
	}
	if mdKey != "my-agent" {
		t.Errorf("derived key = %q; want \"my-agent\"", mdKey)
	}
}

// TestAgentKeyFromFileName_GhcpCli_PlainMdSameKeyAsAgentMd verifies that GHCP CLI's plain
// "foo.md" form and its "foo.agent.md" form both derive the same agent key "foo". The
// finalSuffix of ".agent.md" is ".md", so both files pass isAgentFile and both strip the
// correct suffix in agentKeyFromFileName. This test locks the property that switching from
// one form to the other does not change the derived key.
func TestAgentKeyFromFileName_GhcpCli_PlainMdSameKeyAsAgentMd(t *testing.T) {
	plainKey := agentKeyFromFileName("foo.md", ".agent.md")
	agentKey := agentKeyFromFileName("foo.agent.md", ".agent.md")
	if plainKey != agentKey {
		t.Errorf("GHCP CLI key derivation differs between plain and double-extension forms: "+
			"agentKeyFromFileName(%q, %q) = %q, "+
			"agentKeyFromFileName(%q, %q) = %q; both must yield the same key",
			"foo.md", ".agent.md", plainKey,
			"foo.agent.md", ".agent.md", agentKey)
	}
	if plainKey != "foo" {
		t.Errorf("derived key = %q; want \"foo\"", plainKey)
	}
}

// ---------------------------------------------------------------------------
// isAgentFile -- extension-driven file acceptance
// ---------------------------------------------------------------------------

// TestIsAgentFile_CodexExtension_AcceptsTomlFiles verifies that a .toml file is accepted
// by the agent-file filter when the harness declares ".toml" as its agent extension.
func TestIsAgentFile_CodexExtension_AcceptsTomlFiles(t *testing.T) {
	if !isAgentFile("my-agent.toml", ".toml") {
		t.Error("isAgentFile(\"my-agent.toml\", \".toml\") returned false; want true -- " +
			"a .toml file must be accepted by a Codex (agentExt \".toml\") harness")
	}
}

// TestIsAgentFile_CodexExtension_RejectsMdFiles verifies that a .md file is rejected by the
// agent-file filter when the harness declares ".toml" as its agent extension. A .md file in
// a Codex agents directory is never a Codex agent and must be ignored.
func TestIsAgentFile_CodexExtension_RejectsMdFiles(t *testing.T) {
	if isAgentFile("my-agent.md", ".toml") {
		t.Error("isAgentFile(\"my-agent.md\", \".toml\") returned true; want false -- " +
			"a .md file must be rejected by a Codex (agentExt \".toml\") harness")
	}
}

// TestIsAgentFile_GhcpCliExtension_AcceptsPlainMdFiles verifies that the GHCP CLI harness
// (agentExt ".agent.md") still accepts plain ".md" files alongside ".agent.md" files. The
// filter uses finalSuffix(".agent.md") == ".md", so any file ending in ".md" is accepted.
// This preserves the behaviour of the pre-Stage-9 hard-coded ".md" filter.
func TestIsAgentFile_GhcpCliExtension_AcceptsPlainMdFiles(t *testing.T) {
	if !isAgentFile("foo.md", ".agent.md") {
		t.Error("isAgentFile(\"foo.md\", \".agent.md\") returned false; want true -- " +
			"a GHCP CLI harness (agentExt \".agent.md\") must continue to accept plain .md files; " +
			"finalSuffix(\".agent.md\") == \".md\" so the filter passes both forms")
	}
}

// ---------------------------------------------------------------------------
// mosaicOrchestratorFileNames -- per-extension name set assertions (T9.2)
// ---------------------------------------------------------------------------

// assertOrchestratorNameSet is a table-driven helper that verifies the four exact name set
// members returned by mosaicOrchestratorFileNames for a given agent extension. Using a
// helper keeps each caller focused on the expected names and eliminates repetition.
func assertOrchestratorNameSet(t *testing.T, agentExt string, wantNames []string) {
	t.Helper()
	got := mosaicOrchestratorFileNames(agentExt)

	// The set must have exactly four members.
	if len(got) != 4 {
		t.Errorf("mosaicOrchestratorFileNames(%q) returned a set with %d members; want exactly 4",
			agentExt, len(got))
	}

	// Every expected name must be present.
	for _, name := range wantNames {
		if !got[name] {
			t.Errorf("mosaicOrchestratorFileNames(%q) does not contain %q; "+
				"full set: %v", agentExt, name, got)
		}
	}

	// No extra members beyond what we expect.
	wantSet := make(map[string]bool, len(wantNames))
	for _, n := range wantNames {
		wantSet[n] = true
	}
	for name := range got {
		if !wantSet[name] {
			t.Errorf("mosaicOrchestratorFileNames(%q) contains unexpected member %q; "+
				"only the four derived names should be present", agentExt, name)
		}
	}
}

// TestMosaicOrchestratorFileNames_MarkdownExtension_ExactFourExistingNames verifies that for
// a Markdown-extension harness (agentExt ".md") the returned set is exactly the four existing
// names. The two .agent.md variants must still be present -- nothing is removed from any
// existing harness's set.
func TestMosaicOrchestratorFileNames_MarkdownExtension_ExactFourExistingNames(t *testing.T) {
	wantNames := []string{
		"orchestrator.md",
		"orchestrator.agent.md",
		"orchestrator-script.md",
		"orchestrator-script.agent.md",
	}
	assertOrchestratorNameSet(t, ".md", wantNames)
}

// TestMosaicOrchestratorFileNames_GhcpCliExtension_SameFourNamesAsMd verifies that for a
// GHCP CLI harness (agentExt ".agent.md") the returned set is identical to the Markdown set.
// finalSuffix(".agent.md") == ".md", so the derivation produces the four .md names, not the
// four .agent.agent.md names that a naive substitution would yield. This is the exact
// "nothing is removed from any existing harness's set" contract from CD-5.
func TestMosaicOrchestratorFileNames_GhcpCliExtension_SameFourNamesAsMd(t *testing.T) {
	wantNames := []string{
		"orchestrator.md",
		"orchestrator.agent.md",
		"orchestrator-script.md",
		"orchestrator-script.agent.md",
	}
	assertOrchestratorNameSet(t, ".agent.md", wantNames)
}

// TestMosaicOrchestratorFileNames_CodexExtension_ExactFourTomlNames verifies that for a
// Codex harness (agentExt ".toml") the returned set contains exactly the four .toml names.
// The .toml names are added by Stage 9; nothing is removed from any other harness's set.
func TestMosaicOrchestratorFileNames_CodexExtension_ExactFourTomlNames(t *testing.T) {
	wantNames := []string{
		"orchestrator.toml",
		"orchestrator.agent.toml",
		"orchestrator-script.toml",
		"orchestrator-script.agent.toml",
	}
	assertOrchestratorNameSet(t, ".toml", wantNames)
}

// TestMosaicOrchestratorFileNames_DerivedfromExtension_MatchesFinalSuffix verifies the
// general form: for any declared extension, the four members are exactly the names that
// can be derived from finalSuffix(agentExt). This test covers an additional extension
// (".ext") without adding a dedicated test for it, fulfilling the plan requirement that "a
// sixth harness is covered without a new test".
func TestMosaicOrchestratorFileNames_DerivedFromExtension_MatchesFinalSuffix(t *testing.T) {
	agentExt := ".ext"
	suffix := finalSuffix(agentExt)
	wantNames := []string{
		"orchestrator" + suffix,
		"orchestrator.agent" + suffix,
		"orchestrator-script" + suffix,
		"orchestrator-script.agent" + suffix,
	}
	assertOrchestratorNameSet(t, agentExt, wantNames)
}

// ---------------------------------------------------------------------------
// isOrchestratorFileName -- Codex-extension orchestrator classification (T9.2)
// ---------------------------------------------------------------------------

// TestIsOrchestratorFileName_CodexDescriptor_OrchestratorToml_ReturnsTrue verifies that
// "orchestrator.toml" is classified as an orchestrator file name when a Codex descriptor
// (agentExt ".toml") is supplied. MOSAIC's own Codex orchestrator must never be offered
// as a user-authored harness-only agent.
func TestIsOrchestratorFileName_CodexDescriptor_OrchestratorToml_ReturnsTrue(t *testing.T) {
	desc := codexHarnessDescriptor()
	if !isOrchestratorFileName("orchestrator.toml", desc) {
		t.Error("isOrchestratorFileName(\"orchestrator.toml\", codexDesc) returned false; " +
			"want true -- MOSAIC's Codex orchestrator must be recognised and excluded")
	}
}

// TestIsOrchestratorFileName_CodexDescriptor_OrchestratorAgentToml_ReturnsTrue verifies that
// the double-extension form "orchestrator.agent.toml" is also classified as the orchestrator
// file name for a Codex harness.
func TestIsOrchestratorFileName_CodexDescriptor_OrchestratorAgentToml_ReturnsTrue(t *testing.T) {
	desc := codexHarnessDescriptor()
	if !isOrchestratorFileName("orchestrator.agent.toml", desc) {
		t.Error("isOrchestratorFileName(\"orchestrator.agent.toml\", codexDesc) returned false; " +
			"want true")
	}
}

// TestIsOrchestratorFileName_CodexDescriptor_OrchestratorScriptToml_ReturnsTrue verifies
// that "orchestrator-script.toml" is classified as an orchestrator file name for a Codex
// harness.
func TestIsOrchestratorFileName_CodexDescriptor_OrchestratorScriptToml_ReturnsTrue(t *testing.T) {
	desc := codexHarnessDescriptor()
	if !isOrchestratorFileName("orchestrator-script.toml", desc) {
		t.Error("isOrchestratorFileName(\"orchestrator-script.toml\", codexDesc) returned false; " +
			"want true")
	}
}

// TestIsOrchestratorFileName_CodexDescriptor_OrchestratorScriptAgentToml_ReturnsTrue verifies
// that "orchestrator-script.agent.toml" is classified as an orchestrator file name for a
// Codex harness.
func TestIsOrchestratorFileName_CodexDescriptor_OrchestratorScriptAgentToml_ReturnsTrue(t *testing.T) {
	desc := codexHarnessDescriptor()
	if !isOrchestratorFileName("orchestrator-script.agent.toml", desc) {
		t.Error("isOrchestratorFileName(\"orchestrator-script.agent.toml\", codexDesc) returned false; " +
			"want true")
	}
}

// TestIsOrchestratorFileName_CodexDescriptor_UpperCase_ReturnsTrue verifies that the
// comparison is case-insensitive for Codex file names: "ORCHESTRATOR.TOML" must return true.
func TestIsOrchestratorFileName_CodexDescriptor_UpperCase_ReturnsTrue(t *testing.T) {
	desc := codexHarnessDescriptor()
	if !isOrchestratorFileName("ORCHESTRATOR.TOML", desc) {
		t.Error("isOrchestratorFileName(\"ORCHESTRATOR.TOML\", codexDesc) returned false; " +
			"want true -- classification must be case-insensitive for Codex files")
	}
}

// TestIsOrchestratorFileName_CodexDescriptor_OrchestratorScriptCustomToml_ReturnsFalse
// verifies that the whole-name rule is preserved for Codex files: a file whose name merely
// begins with "orchestrator-script" is not classified as an orchestrator. Only exact
// whole-name matches are excluded; substring or prefix matching is explicitly forbidden.
func TestIsOrchestratorFileName_CodexDescriptor_OrchestratorScriptCustomToml_ReturnsFalse(t *testing.T) {
	desc := codexHarnessDescriptor()
	if isOrchestratorFileName("orchestrator-script-custom.toml", desc) {
		t.Error("isOrchestratorFileName(\"orchestrator-script-custom.toml\", codexDesc) returned true; " +
			"want false -- only exact whole-name matches are excluded; " +
			"\"orchestrator-script-custom.toml\" must not be treated as an orchestrator file")
	}
}

// ---------------------------------------------------------------------------
// scanHarnessOnlyAgents -- Codex extension-driven discovery (T9.1)
// ---------------------------------------------------------------------------

// TestScanHarnessOnlyAgents_CodexDescriptor_FindsEligibleTomlFiles verifies that when a
// Codex HarnessDescriptor is passed, eligible .toml files in the agents directory are
// returned as harness-only agents. This is the primary T9.1 assertion: Codex discovery
// is driven by the declared .toml extension, and eligible agents are reported.
func TestScanHarnessOnlyAgents_CodexDescriptor_FindsEligibleTomlFiles(t *testing.T) {
	// Arrange
	workspace := t.TempDir()
	agentsDir := ".codex/agents"
	fullDir := filepath.Join(workspace, agentsDir)
	writeHarnessOnlyAgentFile(t, fullDir, "my-codex-agent.toml", minimalEligibleTomlAgentBytes())

	desc := codexHarnessDescriptor()

	// Act
	result, _ := scanHarnessOnlyAgents(workspace, agentsDir, map[string]bool{}, desc)

	// Assert
	if len(result) == 0 {
		t.Fatal("scanHarnessOnlyAgents with Codex descriptor returned empty result; " +
			"an eligible .toml file must be included in the harness-only set")
	}
	if len(result) != 1 {
		t.Errorf("scanHarnessOnlyAgents returned %d results, want 1", len(result))
	}
}

// TestScanHarnessOnlyAgents_CodexDescriptor_IgnoresMdFiles verifies that .md files in a
// Codex agents directory are ignored. A .md file is not a Codex agent file
// (isAgentFile("foo.md", ".toml") is false), so it must never be scanned or returned.
func TestScanHarnessOnlyAgents_CodexDescriptor_IgnoresMdFiles(t *testing.T) {
	// Arrange -- a Codex agents directory with an eligible .md file (wrong extension for Codex).
	workspace := t.TempDir()
	agentsDir := ".codex/agents"
	fullDir := filepath.Join(workspace, agentsDir)
	writeHarnessOnlyAgentFile(t, fullDir, "my-markdown-agent.md", minimalEligibleAgentBytes())

	desc := codexHarnessDescriptor()

	// Act
	result, _ := scanHarnessOnlyAgents(workspace, agentsDir, map[string]bool{}, desc)

	// Assert -- the .md file must be ignored; no result should be returned.
	if len(result) != 0 {
		t.Errorf("scanHarnessOnlyAgents with Codex descriptor returned %d result(s) for a .md file; "+
			"want 0 -- .md files must be ignored in a Codex agents directory", len(result))
	}
}

// TestScanHarnessOnlyAgents_CodexDescriptor_KeyStripsTomlSuffix verifies that when a Codex
// descriptor is used and a .toml file is discovered, the derived agent key has the .toml
// suffix stripped. No extension fragment must leak into the key.
func TestScanHarnessOnlyAgents_CodexDescriptor_KeyStripsTomlSuffix(t *testing.T) {
	// Arrange
	workspace := t.TempDir()
	agentsDir := ".codex/agents"
	fullDir := filepath.Join(workspace, agentsDir)
	writeHarnessOnlyAgentFile(t, fullDir, "specialist-agent.toml", minimalEligibleTomlAgentBytes())

	desc := codexHarnessDescriptor()

	// Act
	result, _ := scanHarnessOnlyAgents(workspace, agentsDir, map[string]bool{}, desc)

	// Assert
	if len(result) == 0 {
		t.Fatal("scanHarnessOnlyAgents with Codex descriptor returned empty result; " +
			"cannot verify key derivation")
	}
	if result[0].Key != "specialist-agent" {
		t.Errorf("result[0].Key = %q, want \"specialist-agent\"; "+
			"Key must be the base name with .toml stripped", result[0].Key)
	}
	if result[0].TransformVersion != "1.0.0" {
		t.Errorf("result[0].TransformVersion = %q, want \"1.0.0\"; "+
			"TransformVersion must be carried through from the Codex stamp block",
			result[0].TransformVersion)
	}
}

// TestScanHarnessOnlyAgents_CodexDescriptor_TargetPathIsRelative verifies that the
// TargetPath in the returned entry is relative to the workspace root, in the same form
// plan items use, for a Codex .toml agent.
func TestScanHarnessOnlyAgents_CodexDescriptor_TargetPathIsRelative(t *testing.T) {
	// Arrange
	workspace := t.TempDir()
	agentsDir := ".codex/agents"
	fullDir := filepath.Join(workspace, agentsDir)
	writeHarnessOnlyAgentFile(t, fullDir, "my-agent.toml", minimalEligibleTomlAgentBytes())

	desc := codexHarnessDescriptor()

	// Act
	result, _ := scanHarnessOnlyAgents(workspace, agentsDir, map[string]bool{}, desc)

	// Assert
	if len(result) == 0 {
		t.Fatal("scanHarnessOnlyAgents returned empty result; cannot verify TargetPath")
	}
	want := filepath.Join(agentsDir, "my-agent.toml")
	if result[0].TargetPath != want {
		t.Errorf("result[0].TargetPath = %q, want %q; "+
			"TargetPath must be relative to the workspace root",
			result[0].TargetPath, want)
	}
}

// TestScanHarnessOnlyAgents_CodexDescriptor_MdAndTomlBothPresent_OnlyTomlReturned
// verifies that when a Codex agents directory contains both a .md file and a .toml file
// for different agents, only the .toml file is returned. The .md file is ignored by the
// extension filter.
func TestScanHarnessOnlyAgents_CodexDescriptor_MdAndTomlBothPresent_OnlyTomlReturned(t *testing.T) {
	// Arrange
	workspace := t.TempDir()
	agentsDir := ".codex/agents"
	fullDir := filepath.Join(workspace, agentsDir)

	// .md file -- should be ignored by Codex harness
	writeHarnessOnlyAgentFile(t, fullDir, "markdown-agent.md", minimalEligibleAgentBytes())
	// .toml file -- should be discovered
	writeHarnessOnlyAgentFile(t, fullDir, "codex-agent.toml", minimalEligibleTomlAgentBytes())

	desc := codexHarnessDescriptor()

	// Act
	result, _ := scanHarnessOnlyAgents(workspace, agentsDir, map[string]bool{}, desc)

	// Assert -- exactly one result: the .toml file.
	if len(result) != 1 {
		t.Errorf("scanHarnessOnlyAgents with Codex descriptor returned %d result(s); "+
			"want 1 (only the .toml file; the .md file must be ignored)", len(result))
		return
	}
	if result[0].Key != "codex-agent" {
		t.Errorf("result[0].Key = %q, want \"codex-agent\"", result[0].Key)
	}
}

// TestScanHarnessOnlyAgents_MarkdownDescriptor_BehavesAsBeforeStage9 verifies that the
// Markdown harness discovery behaviour is unchanged after Stage 9. Passing nil (or no
// descriptor) must produce the same result as passing a Markdown descriptor. This test
// uses the same fixture and assertions as the pre-Stage-9 suite to confirm the conversion
// introduced no regression on the Markdown path.
func TestScanHarnessOnlyAgents_MarkdownDescriptor_BehavesAsBeforeStage9(t *testing.T) {
	// Arrange
	workspace := t.TempDir()
	agentsDir := ".claude/agents"
	fullDir := filepath.Join(workspace, agentsDir)
	writeHarnessOnlyAgentFile(t, fullDir, "hand-authored.md", minimalEligibleAgentBytes())

	// Act -- no descriptor: the nil path uses .md as the default extension.
	result, _ := scanHarnessOnlyAgents(workspace, agentsDir, map[string]bool{})

	// Assert -- the .md file must be discovered, key derived, and TransformVersion carried.
	if len(result) == 0 {
		t.Fatal("scanHarnessOnlyAgents without descriptor returned empty result; " +
			"the Markdown discovery path must be unchanged after Stage 9")
	}
	if result[0].Key != "hand-authored" {
		t.Errorf("result[0].Key = %q, want \"hand-authored\"", result[0].Key)
	}
	if result[0].TransformVersion != "1.0.0" {
		t.Errorf("result[0].TransformVersion = %q, want \"1.0.0\"", result[0].TransformVersion)
	}
}

// TestScanHarnessOnlyAgents_GhcpCliDescriptor_AcceptsPlainMdFiles verifies that the GHCP
// CLI harness (agentExt ".agent.md") still accepts plain foo.md files. A GHCP CLI agents
// directory may contain files named "foo.md" without the ".agent.md" double extension; the
// generalised key derivation must not reject or skip them.
func TestScanHarnessOnlyAgents_GhcpCliDescriptor_AcceptsPlainMdFiles(t *testing.T) {
	// Arrange -- a plain foo.md file in a GHCP CLI agents directory.
	workspace := t.TempDir()
	agentsDir := ".ghcp-cli/agents"
	fullDir := filepath.Join(workspace, agentsDir)
	writeHarnessOnlyAgentFile(t, fullDir, "foo.md", minimalEligibleAgentBytes())

	desc := ghcpCliHarnessDescriptor()

	// Act
	result, _ := scanHarnessOnlyAgents(workspace, agentsDir, map[string]bool{}, desc)

	// Assert -- the plain .md file must be discovered and returned.
	if len(result) == 0 {
		t.Fatal("scanHarnessOnlyAgents with GHCP CLI descriptor returned empty result for foo.md; " +
			"a GHCP CLI harness must continue to accept plain .md files -- " +
			"isAgentFile(\"foo.md\", \".agent.md\") must be true because " +
			"finalSuffix(\".agent.md\") == \".md\"")
	}
	if len(result) != 1 {
		t.Errorf("scanHarnessOnlyAgents returned %d results, want 1", len(result))
	}
	if result[0].Key != "foo" {
		t.Errorf("result[0].Key = %q, want \"foo\"; "+
			"key derivation for a GHCP CLI plain .md file must strip the .md suffix",
			result[0].Key)
	}
}

// ---------------------------------------------------------------------------
// scanHarnessOnlyAgents -- Codex orchestrator exclusion (T9.2)
// ---------------------------------------------------------------------------

// TestScanHarnessOnlyAgents_CodexDescriptor_ExcludesOrchestratorToml verifies that a file
// named "orchestrator.toml" is never returned as a harness-only agent when a Codex
// descriptor is in use, regardless of the file's eligibility signals. MOSAIC's own Codex
// orchestrator must not be classified as a user-authored harness-only agent.
func TestScanHarnessOnlyAgents_CodexDescriptor_ExcludesOrchestratorToml(t *testing.T) {
	// Arrange -- "orchestrator.toml" satisfies both eligibility signals.
	workspace := t.TempDir()
	agentsDir := ".codex/agents"
	fullDir := filepath.Join(workspace, agentsDir)
	writeHarnessOnlyAgentFile(t, fullDir, "orchestrator.toml", minimalEligibleTomlAgentBytes())

	desc := codexHarnessDescriptor()

	// Act
	result, _ := scanHarnessOnlyAgents(workspace, agentsDir, map[string]bool{}, desc)

	// Assert -- orchestrator.toml must always be excluded by isOrchestratorFileName.
	if len(result) != 0 {
		t.Errorf("scanHarnessOnlyAgents returned %d result(s) for \"orchestrator.toml\" with a "+
			"Codex descriptor; want 0 -- MOSAIC's orchestrator file must always be excluded",
			len(result))
	}
}

// TestScanHarnessOnlyAgents_CodexDescriptor_ExcludesOrchestratorScriptToml verifies that
// "orchestrator-script.toml" is excluded by the filename check when a Codex descriptor is
// used, exactly as "orchestrator-script.md" is excluded for Markdown harnesses.
func TestScanHarnessOnlyAgents_CodexDescriptor_ExcludesOrchestratorScriptToml(t *testing.T) {
	// Arrange
	workspace := t.TempDir()
	agentsDir := ".codex/agents"
	fullDir := filepath.Join(workspace, agentsDir)
	writeHarnessOnlyAgentFile(t, fullDir, "orchestrator-script.toml", minimalEligibleTomlAgentBytes())

	desc := codexHarnessDescriptor()

	// Act
	result, _ := scanHarnessOnlyAgents(workspace, agentsDir, map[string]bool{}, desc)

	// Assert
	if len(result) != 0 {
		t.Errorf("scanHarnessOnlyAgents returned %d result(s) for \"orchestrator-script.toml\" "+
			"with a Codex descriptor; want 0", len(result))
	}
}

// TestScanHarnessOnlyAgents_CodexDescriptor_OrchestratorScriptCustomToml_NotExcluded
// verifies the whole-name rule for the Codex path: a file named
// "orchestrator-script-custom.toml" is NOT excluded by isOrchestratorFileName, so an
// eligible file with that name must appear in the harness-only result. Only exact
// whole-name matches are rejected; a prefix match is not sufficient.
func TestScanHarnessOnlyAgents_CodexDescriptor_OrchestratorScriptCustomToml_NotExcluded(t *testing.T) {
	// Arrange -- a file whose name begins with "orchestrator-script" but is not an exact match.
	workspace := t.TempDir()
	agentsDir := ".codex/agents"
	fullDir := filepath.Join(workspace, agentsDir)
	writeHarnessOnlyAgentFile(t, fullDir, "orchestrator-script-custom.toml",
		minimalEligibleTomlAgentBytes())

	desc := codexHarnessDescriptor()

	// Act
	result, _ := scanHarnessOnlyAgents(workspace, agentsDir, map[string]bool{}, desc)

	// Assert -- the file must be discovered; its name is not in the exclusion set.
	if len(result) == 0 {
		t.Error("scanHarnessOnlyAgents returned no results for \"orchestrator-script-custom.toml\"; " +
			"want 1 -- the whole-name rule must not exclude a file whose name merely begins with " +
			"\"orchestrator-script\"; only exact matches (orchestrator-script.toml etc.) are excluded")
	}
}

// TestScanHarnessOnlyAgents_CodexDescriptor_OrchestratorExcluded_GenuineAgentStillDetected
// verifies that when MOSAIC's orchestrator.toml is present alongside a genuine harness-only
// .toml agent, only the genuine agent is returned. The orchestrator exclusion does not
// prevent other eligible files from being discovered.
func TestScanHarnessOnlyAgents_CodexDescriptor_OrchestratorExcluded_GenuineAgentStillDetected(t *testing.T) {
	// Arrange
	workspace := t.TempDir()
	agentsDir := ".codex/agents"
	fullDir := filepath.Join(workspace, agentsDir)

	// The MOSAIC orchestrator -- must be excluded.
	writeHarnessOnlyAgentFile(t, fullDir, "orchestrator.toml", minimalEligibleTomlAgentBytes())
	// A genuine user-authored harness-only agent -- must be returned.
	writeHarnessOnlyAgentFile(t, fullDir, "user-custom-agent.toml", minimalEligibleTomlAgentBytes())

	desc := codexHarnessDescriptor()

	// Act
	result, _ := scanHarnessOnlyAgents(workspace, agentsDir, map[string]bool{}, desc)

	// Assert -- exactly one result: the genuine user agent.
	if len(result) != 1 {
		t.Errorf("scanHarnessOnlyAgents returned %d result(s); want 1 "+
			"(orchestrator.toml excluded, user-custom-agent.toml returned)", len(result))
		for _, r := range result {
			t.Logf("  result: TargetPath=%q Key=%q", r.TargetPath, r.Key)
		}
		return
	}
	if result[0].Key != "user-custom-agent" {
		t.Errorf("result[0].Key = %q, want \"user-custom-agent\"", result[0].Key)
	}
}

// TestScanHarnessOnlyAgents_CodexDescriptor_CatalogCounterpartToml_Excluded verifies that
// a .toml file whose derived key appears in the catalogKeys map is excluded. Catalog-backed
// agents are not harness-only regardless of extension.
func TestScanHarnessOnlyAgents_CodexDescriptor_CatalogCounterpartToml_Excluded(t *testing.T) {
	// Arrange -- "my-codex-agent" appears in the catalog.
	workspace := t.TempDir()
	agentsDir := ".codex/agents"
	fullDir := filepath.Join(workspace, agentsDir)
	writeHarnessOnlyAgentFile(t, fullDir, "my-codex-agent.toml", minimalEligibleTomlAgentBytes())
	catalogKeys := map[string]bool{"my-codex-agent": true}

	desc := codexHarnessDescriptor()

	// Act
	result, _ := scanHarnessOnlyAgents(workspace, agentsDir, catalogKeys, desc)

	// Assert
	if len(result) != 0 {
		t.Errorf("scanHarnessOnlyAgents returned %d result(s) for a .toml file whose key "+
			"\"my-codex-agent\" is in the catalog; want 0 -- catalog-backed agents are not harness-only",
			len(result))
	}
}

// TestScanHarnessOnlyAgents_NoDescriptor_OrchestratorAgentMdExcluded verifies that the
// Markdown path (nil descriptor) still excludes orchestrator.agent.md and
// orchestrator-script.agent.md. This is the regression guard for the claim in the plan
// that nothing is removed from any existing harness's exclusion set.
func TestScanHarnessOnlyAgents_NoDescriptor_OrchestratorAgentMdExcluded(t *testing.T) {
	for _, name := range []string{"orchestrator.agent.md", "orchestrator-script.agent.md"} {
		t.Run(name, func(t *testing.T) {
			workspace := t.TempDir()
			agentsDir := ".claude/agents"
			fullDir := filepath.Join(workspace, agentsDir)
			writeHarnessOnlyAgentFile(t, fullDir, name, minimalEligibleAgentBytes())

			result, _ := scanHarnessOnlyAgents(workspace, agentsDir, map[string]bool{})
			if len(result) != 0 {
				t.Errorf("scanHarnessOnlyAgents (nil descriptor) returned %d result(s) for %q; "+
					"want 0 -- the Markdown exclusion set still includes the .agent.md variants; "+
					"nothing must be removed from any existing harness's set",
					len(result), name)
			}
		})
	}
}

// TestScanHarnessOnlyAgents_CodexDescriptor_NoArtifactsWritten verifies the no-write
// guarantee for the Codex path: after scanHarnessOnlyAgents returns, every .toml file
// the scan examined is byte-identical to its pre-scan bytes.
func TestScanHarnessOnlyAgents_CodexDescriptor_NoArtifactsWritten(t *testing.T) {
	// Arrange -- two eligible .toml files.
	workspace := t.TempDir()
	agentsDir := ".codex/agents"
	fullDir := filepath.Join(workspace, agentsDir)

	names := []string{"agent-alpha.toml", "agent-beta.toml"}
	for _, name := range names {
		writeHarnessOnlyAgentFile(t, fullDir, name, minimalEligibleTomlAgentBytes())
	}

	// Read original bytes before the scan.
	origBytes := make(map[string][]byte, len(names))
	for _, name := range names {
		b, err := os.ReadFile(filepath.Join(fullDir, name))
		if err != nil {
			t.Fatalf("pre-scan ReadFile(%q): %v", name, err)
		}
		origBytes[name] = b
	}

	desc := codexHarnessDescriptor()

	// Act -- run the scan.
	_, _ = scanHarnessOnlyAgents(workspace, agentsDir, map[string]bool{}, desc)

	// Assert -- every file must be byte-identical after the scan.
	for _, name := range names {
		after, err := os.ReadFile(filepath.Join(fullDir, name))
		if err != nil {
			t.Fatalf("post-scan ReadFile(%q): %v", name, err)
		}
		if string(origBytes[name]) != string(after) {
			t.Errorf("file %q was modified by scanHarnessOnlyAgents with Codex descriptor; "+
				"the scan must perform no writes", name)
		}
	}
}

// ---------------------------------------------------------------------------
// scanHarnessOnlyAgents -- AC9.6 observability: decode report surfaced for body-less file
// ---------------------------------------------------------------------------

// bodylessTomlAgentBytes returns the bytes of a Codex TOML file that carries the stamp
// block (satisfying eligibility signal one) but has no developer_instructions key (so
// the decoded body is empty and eligibility signal two fails). The file is ineligible but
// must still produce a decode-report entry that surfaces through the notices return value
// of scanHarnessOnlyAgents before the eligibility gate is applied.
func bodylessTomlAgentBytes() []byte {
	return []byte(
		"# mosaic_transform_version: 1.0.0\n" +
			"# mosaic_version: 2.1.0\n" +
			"\n" +
			"name = \"bodyless-agent\"\n" +
			"sandbox_mode = \"read-only\"\n")
	// No developer_instructions key: body is empty after decode.
}

// TestScanHarnessOnlyAgents_CodexDescriptor_BodylessFile_NoticesNonEmpty verifies that
// when a Codex TOML file contains the stamp block but no developer_instructions key, the
// scan does not include the file in the result (it is ineligible) but the decode report
// produced during decoding is surfaced in the second return value (the notices slice)
// before the eligibility gate is applied. This is the AC9.6 observability requirement:
// the decode report from a body-less Codex file must be observable at the discovery
// consumer.
func TestScanHarnessOnlyAgents_CodexDescriptor_BodylessFile_NoticesNonEmpty(t *testing.T) {
	// Arrange -- a TOML file with stamp block (signal 1) but no developer_instructions (signal 2 absent).
	workspace := t.TempDir()
	agentsDir := ".codex/agents"
	fullDir := filepath.Join(workspace, agentsDir)
	writeHarnessOnlyAgentFile(t, fullDir, "bodyless-agent.toml", bodylessTomlAgentBytes())

	desc := codexHarnessDescriptor()

	// Act
	result, notices := scanHarnessOnlyAgents(workspace, agentsDir, map[string]bool{}, desc)

	// Assert -- the file is ineligible (no body), so it must not appear in the result.
	if len(result) != 0 {
		t.Errorf("scanHarnessOnlyAgents returned %d result(s) for a body-less Codex file; "+
			"want 0 -- a file without developer_instructions is ineligible and must be excluded",
			len(result))
	}

	// Assert -- the decode report must be observable: the notices slice must be non-empty.
	// The report entry for the missing instructions must be surfaced before the eligibility
	// gate is applied, so it reaches the discovery consumer through the second return value.
	if len(notices) == 0 {
		t.Error("scanHarnessOnlyAgents returned an empty notices slice for a body-less Codex file; " +
			"want at least one entry -- the decode report (EntryMissingInstructions) must be " +
			"surfaced before the eligibility gate so the observability contract of AC9.6 is satisfied")
	}
}
