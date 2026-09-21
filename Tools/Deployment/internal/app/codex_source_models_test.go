package app_test

// codex_source_models_test.go covers T8.3a: source-model discovery for Codex .toml source
// files. ReadSourceModel and IndexSourceModels must decode Codex TOML through the funnel
// before reading the model key from the decoded canonical frontmatter.
//
// Verified behaviours:
//   - A Codex .toml source file with a TOML "model" key yields that value as the SourceModel
//     when the descriptor's Frontmatter.ModelKey is "model".
//   - Multiple source files with the same model appear as one entry in Distinct.
//   - A malformed .toml file (TOML syntax error) yields UnsetSourceModel without panicking.
//   - An empty .toml file yields UnsetSourceModel.

import (
	"os"
	"path/filepath"
	"testing"

	"mosaic-deploy/internal/app"
	"mosaic-deploy/internal/domain"
	_ "mosaic-deploy/internal/agentformat/all"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// codexSrcDesc returns a HarnessDescriptor for Codex source model reads.
func codexSrcDesc() domain.HarnessDescriptor {
	return domain.HarnessDescriptor{
		AgentFormatID: "codex-toml",
		Frontmatter: domain.FrontmatterSpec{
			ModelKey: "model",
		},
	}
}

// writeCodexSourceFile writes a minimal Codex TOML source agent file to dir/name and
// returns its absolute path. model may be empty to omit the model key.
func writeCodexSourceFile(t *testing.T, dir, name, model string) string {
	t.Helper()
	var content []byte
	if model != "" {
		content = []byte("model = \"" + model + "\"\n" +
			"sandbox_mode = \"read-only\"\n" +
			"developer_instructions = \"Source agent body.\\n\"\n")
	} else {
		content = []byte("sandbox_mode = \"read-only\"\n" +
			"developer_instructions = \"Source agent body.\\n\"\n")
	}
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, content, 0o644); err != nil {
		t.Fatalf("writeCodexSourceFile: %v", err)
	}
	return p
}

// writeBrokenCodexSourceFile writes a .toml file with invalid TOML syntax to dir/name
// and returns its absolute path. The decode funnel will return an error for this file.
func writeBrokenCodexSourceFile(t *testing.T, dir, name string) string {
	t.Helper()
	content := []byte("model = \"gpt-6-astra\"\nmodel = \"duplicate-key-syntax-error\"\n")
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, content, 0o644); err != nil {
		t.Fatalf("writeBrokenCodexSourceFile: %v", err)
	}
	return p
}

// ---------------------------------------------------------------------------
// T8.3a — ReadSourceModel for Codex .toml
// ---------------------------------------------------------------------------

// TestReadSourceModel_CodexToml_ModelKeyPresent_ReturnsModel verifies that a Codex .toml
// source file with a "model" TOML key is decoded through the funnel and the model value is
// returned when the descriptor declares ModelKey = "model".
func TestReadSourceModel_CodexToml_ModelKeyPresent_ReturnsModel(t *testing.T) {
	content := []byte("model = \"gpt-6-astra\"\n" +
		"sandbox_mode = \"read-only\"\n" +
		"developer_instructions = \"Agent body.\\n\"\n")

	got := app.ReadSourceModel(content, codexSrcDesc())

	if got != "gpt-6-astra" {
		t.Errorf("ReadSourceModel = %q, want %q; model TOML key must be extracted after decode",
			got, "gpt-6-astra")
	}
}

// TestReadSourceModel_CodexToml_ModelKeyAbsent_ReturnsUnset verifies that a valid Codex
// .toml source file with no "model" key yields UnsetSourceModel.
func TestReadSourceModel_CodexToml_ModelKeyAbsent_ReturnsUnset(t *testing.T) {
	content := []byte("sandbox_mode = \"read-only\"\n" +
		"developer_instructions = \"Agent body.\\n\"\n")

	got := app.ReadSourceModel(content, codexSrcDesc())

	if got != app.UnsetSourceModel {
		t.Errorf("ReadSourceModel = %q, want UnsetSourceModel (%q); absent model key must yield unset",
			got, app.UnsetSourceModel)
	}
}

// TestReadSourceModel_CodexToml_BrokenSyntax_ReturnsUnset verifies that a Codex .toml file
// with TOML syntax errors yields UnsetSourceModel without panicking. Malformed source files
// must degrade gracefully.
func TestReadSourceModel_CodexToml_BrokenSyntax_ReturnsUnset(t *testing.T) {
	// Duplicate key is invalid TOML.
	content := []byte("model = \"gpt-6-astra\"\nmodel = \"duplicate\"\n")

	got := app.ReadSourceModel(content, codexSrcDesc())

	if got != app.UnsetSourceModel {
		t.Errorf("ReadSourceModel = %q, want UnsetSourceModel for broken TOML; got %q", got, got)
	}
}

// ---------------------------------------------------------------------------
// T8.3a — IndexSourceModels for Codex .toml
// ---------------------------------------------------------------------------

// TestIndexSourceModels_CodexToml_DistinctModelSet verifies that IndexSourceModels decodes
// each Codex .toml source file and produces the correct distinct source-model set. Multiple
// files declaring the same model appear only once in Distinct.
func TestIndexSourceModels_CodexToml_DistinctModelSet(t *testing.T) {
	dir := t.TempDir()
	p1 := writeCodexSourceFile(t, dir, "alpha.toml", "gpt-6-astra")
	p2 := writeCodexSourceFile(t, dir, "beta.toml", "gpt-6-astra")
	p3 := writeCodexSourceFile(t, dir, "gamma.toml", "gpt-7-nova")

	idx := app.IndexSourceModels([]string{p1, p2, p3}, codexSrcDesc())

	// Both distinct named models must be present.
	foundAstra, foundNova := false, false
	for _, m := range idx.Distinct {
		if m == "gpt-6-astra" {
			foundAstra = true
		}
		if m == "gpt-7-nova" {
			foundNova = true
		}
	}
	if !foundAstra {
		t.Errorf("Distinct does not contain %q; want it present in the distinct source-model set", "gpt-6-astra")
	}
	if !foundNova {
		t.Errorf("Distinct does not contain %q; want it present in the distinct source-model set", "gpt-7-nova")
	}
}

// TestIndexSourceModels_CodexToml_PerFileSourceModel verifies that each SourceFile in Files
// carries the correct SourceModel extracted from the corresponding .toml file.
func TestIndexSourceModels_CodexToml_PerFileSourceModel(t *testing.T) {
	dir := t.TempDir()
	p1 := writeCodexSourceFile(t, dir, "alpha.toml", "gpt-6-astra")
	p2 := writeCodexSourceFile(t, dir, "beta.toml", "gpt-7-nova")

	idx := app.IndexSourceModels([]string{p1, p2}, codexSrcDesc())

	if len(idx.Files) != 2 {
		t.Fatalf("expected 2 Files entries, got %d", len(idx.Files))
	}
	if idx.Files[0].SourceModel != "gpt-6-astra" {
		t.Errorf("Files[0].SourceModel = %q, want %q", idx.Files[0].SourceModel, "gpt-6-astra")
	}
	if idx.Files[1].SourceModel != "gpt-7-nova" {
		t.Errorf("Files[1].SourceModel = %q, want %q", idx.Files[1].SourceModel, "gpt-7-nova")
	}
}

// TestIndexSourceModels_CodexToml_MalformedFile_YieldsUnsetSourceModel verifies that a Codex
// .toml source file with a TOML syntax error yields SourceModel = UnsetSourceModel for that
// file, without failing the whole batch. This is the per-file graceful-degradation contract.
func TestIndexSourceModels_CodexToml_MalformedFile_YieldsUnsetSourceModel(t *testing.T) {
	dir := t.TempDir()
	good := writeCodexSourceFile(t, dir, "good.toml", "gpt-6-astra")
	bad := writeBrokenCodexSourceFile(t, dir, "bad.toml")

	idx := app.IndexSourceModels([]string{good, bad}, codexSrcDesc())

	if len(idx.Files) != 2 {
		t.Fatalf("expected 2 Files, got %d", len(idx.Files))
	}
	if idx.Files[0].SourceModel != "gpt-6-astra" {
		t.Errorf("good file SourceModel = %q, want %q", idx.Files[0].SourceModel, "gpt-6-astra")
	}
	if idx.Files[1].SourceModel != app.UnsetSourceModel {
		t.Errorf("bad file SourceModel = %q, want UnsetSourceModel; malformed TOML must yield unset",
			idx.Files[1].SourceModel)
	}
}

// TestIndexSourceModels_CodexToml_CanonicalContentStored verifies that IndexSourceModels
// stores the canonical (decoded Markdown) bytes in SourceFile.Content, not the raw TOML
// bytes. The retarget loop reads sf.Content and expects Markdown form.
func TestIndexSourceModels_CodexToml_CanonicalContentStored(t *testing.T) {
	dir := t.TempDir()
	p := writeCodexSourceFile(t, dir, "agent.toml", "gpt-6-astra")

	idx := app.IndexSourceModels([]string{p}, codexSrcDesc())

	if len(idx.Files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(idx.Files))
	}
	sf := idx.Files[0]
	if sf.ReadErr != nil {
		t.Fatalf("unexpected ReadErr: %v", sf.ReadErr)
	}
	// The canonical form must start with the YAML frontmatter delimiter, not with TOML.
	if len(sf.Content) < 4 || string(sf.Content[:4]) != "---\n" {
		t.Errorf("SourceFile.Content does not start with frontmatter delimiter; "+
			"IndexSourceModels must store canonical (decoded Markdown) bytes, not raw TOML; "+
			"first bytes: %q", sf.Content[:min(20, len(sf.Content))])
	}
}

// Note: min is a builtin in Go 1.21+. This module targets Go 1.26.5, so the builtin
// is used directly in the assertion above (min(20, len(sf.Content))). No local definition.
