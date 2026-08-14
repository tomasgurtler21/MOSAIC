package app_test

// transform_source_models_test.go covers the source-model discovery pre-pass (T7.1):
//
// ReadSourceModel (T7.1 — extraction logic):
//   - When Frontmatter.ModelKey is declared on the descriptor and that key is present
//     as a non-empty scalar in the frontmatter, its value is returned.
//   - When ModelKey is absent from the frontmatter (or the descriptor does not declare
//     one), the generic "model" key is tried next.
//   - When both ModelKey and "model" are present, ModelKey takes precedence.
//   - When neither is present (or both are empty), UnsetSourceModel ("") is returned.
//   - Unparseable or empty content returns UnsetSourceModel without panicking.
//
// IndexSourceModels (T7.1 — batch pre-pass):
//   - Files is one entry per supplied path, in input order; its length equals len(filePaths).
//   - Distinct contains each unique SourceModel value exactly once.
//   - Distinct is sorted lexicographically ascending, with UnsetSourceModel last.
//   - A path that cannot be read yields a SourceFile with ReadErr != nil and
//     SourceModel = UnsetSourceModel.
//   - An empty filePaths slice returns an index with empty Files and Distinct slices.
//
// All tests call the exported app.ReadSourceModel / app.IndexSourceModels functions,
// which are stubs returning UnsetSourceModel / SourceModelIndex{} until I7.2 delivers
// the real implementations. Tests expecting a specific non-empty value fail immediately
// (TDD RED phase). Tests covering the unset/empty case pass trivially because the stub
// returns the zero value for those cases; they are marked NOT RED-VERIFIED where this applies.

import (
	"os"
	"path/filepath"
	"testing"

	"mosaic-deploy/internal/app"
	"mosaic-deploy/internal/domain"
)

// ---------------------------------------------------------------------------
// Helpers for building test content and descriptors
// ---------------------------------------------------------------------------

// srcDescWithModelKey returns a minimal HarnessDescriptor whose Frontmatter.ModelKey
// is set to modelKey. All other fields are zero.
func srcDescWithModelKey(modelKey string) domain.HarnessDescriptor {
	return domain.HarnessDescriptor{
		Frontmatter: domain.FrontmatterSpec{ModelKey: modelKey},
	}
}

// fmBytes builds a minimal YAML-frontmatter document from a flat list of key/value pairs
// (alternating: key0, val0, key1, val1, …). The document body is empty.
func fmBytes(keyvals ...string) []byte {
	b := []byte("---\n")
	for i := 0; i+1 < len(keyvals); i += 2 {
		b = append(b, (keyvals[i] + ": " + keyvals[i+1] + "\n")...)
	}
	b = append(b, "---\n"...)
	return b
}

// writeSrcModelFile writes a source-harness agent file with the given src_model value
// (or omits the field when model is empty) to dir/name and returns its absolute path.
func writeSrcModelFile(t *testing.T, dir, name, model string) string {
	t.Helper()
	var content []byte
	if model != "" {
		content = []byte("---\n" +
			"transform_version: \"2.1.0\"\n" +
			"injections_version: \"3.0.0\"\n" +
			"src_model: " + model + "\n" +
			"---\n" +
			"<Identity type=\"core\">\nYou are the agent.\n</Identity>\n")
	} else {
		content = []byte("---\n" +
			"transform_version: \"2.1.0\"\n" +
			"injections_version: \"3.0.0\"\n" +
			"---\n" +
			"<Identity type=\"core\">\nYou are the agent.\n</Identity>\n")
	}
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, content, 0o644); err != nil {
		t.Fatalf("writeSrcModelFile: %v", err)
	}
	return p
}

// ---------------------------------------------------------------------------
// ReadSourceModel — ModelKey resolution (happy path)
// ---------------------------------------------------------------------------

// TestReadSourceModel_ModelKeyFieldPresent_ReturnsItsValue verifies that when the
// descriptor's Frontmatter.ModelKey is declared and present as a non-empty scalar in
// the frontmatter, ReadSourceModel returns that scalar value.
func TestReadSourceModel_ModelKeyFieldPresent_ReturnsItsValue(t *testing.T) {
	// Arrange
	content := fmBytes("src_model", "claude-3-sonnet")
	srcDesc := srcDescWithModelKey("src_model")

	// Act
	got := app.ReadSourceModel(content, srcDesc)

	// Assert
	// RED: stub returns UnsetSourceModel ("") regardless of content.
	if got != "claude-3-sonnet" {
		t.Errorf("ReadSourceModel = %q, want %q; "+
			"(RED: stub returns empty string until I7.2 extracts the value from frontmatter)",
			got, "claude-3-sonnet")
	}
}

// TestReadSourceModel_ModelKeyAbsentFromFrontmatter_GenericModelPresent_FallsBack verifies
// that when the descriptor's ModelKey is not present in the frontmatter but the generic
// "model" key is, ReadSourceModel returns the generic key's value.
func TestReadSourceModel_ModelKeyAbsentFromFrontmatter_GenericModelPresent_FallsBack(t *testing.T) {
	// Arrange: descriptor says look for "src_model", but only "model" is in the frontmatter.
	content := fmBytes("model", "gpt-4")
	srcDesc := srcDescWithModelKey("src_model") // "src_model" absent from content

	// Act
	got := app.ReadSourceModel(content, srcDesc)

	// Assert
	// RED: stub returns "".
	if got != "gpt-4" {
		t.Errorf("ReadSourceModel = %q, want %q; "+
			"(RED: stub does not inspect frontmatter; deliver I7.2 to read the generic model key)",
			got, "gpt-4")
	}
}

// TestReadSourceModel_BothModelKeyAndGenericPresent_ModelKeyTakesPrecedence verifies that
// when both the descriptor's declared ModelKey field and the generic "model" key are present,
// the descriptor's ModelKey value is used. This is consistent with how retarget.go strips the
// source model: the declared key is the authoritative signal.
func TestReadSourceModel_BothModelKeyAndGenericPresent_ModelKeyTakesPrecedence(t *testing.T) {
	// Arrange: both keys present; ModelKey should win.
	content := fmBytes("src_model", "claude-3-sonnet", "model", "gpt-4")
	srcDesc := srcDescWithModelKey("src_model")

	// Act
	got := app.ReadSourceModel(content, srcDesc)

	// Assert: must return the ModelKey value, not the generic one.
	// RED: stub returns "".
	if got != "claude-3-sonnet" {
		t.Errorf("ReadSourceModel = %q, want %q (ModelKey takes precedence over generic \"model\"); "+
			"(RED: stub does not inspect frontmatter)", got, "claude-3-sonnet")
	}
}

// TestReadSourceModel_EmptyModelKeyValue_FallsBackToGenericModel verifies that when the
// descriptor's ModelKey is present in the frontmatter but its value is an empty scalar,
// ReadSourceModel falls back to the generic "model" key. An empty value is treated the same
// as absent: neither triggers an interactive question.
func TestReadSourceModel_EmptyModelKeyValue_FallsBackToGenericModel(t *testing.T) {
	// Arrange: "src_model" has an empty value; "model" has a real value.
	content := fmBytes("src_model", "", "model", "gpt-4-turbo")
	srcDesc := srcDescWithModelKey("src_model")

	// Act
	got := app.ReadSourceModel(content, srcDesc)

	// Assert: must return the generic "model" value because ModelKey value is empty.
	// RED: stub returns "".
	if got != "gpt-4-turbo" {
		t.Errorf("ReadSourceModel = %q, want %q; "+
			"(RED: stub does not inspect frontmatter; an empty ModelKey value must fall back to \"model\")",
			got, "gpt-4-turbo")
	}
}

// TestReadSourceModel_EmptyDescriptorModelKey_UsesGenericModel verifies that when the
// descriptor's Frontmatter.ModelKey is empty (the harness does not declare a model key),
// ReadSourceModel uses the generic "model" key as the sole lookup path.
func TestReadSourceModel_EmptyDescriptorModelKey_UsesGenericModel(t *testing.T) {
	// Arrange: descriptor has no declared model key; content has generic "model" only.
	content := fmBytes("model", "claude-opus")
	srcDesc := srcDescWithModelKey("") // no declared model key

	// Act
	got := app.ReadSourceModel(content, srcDesc)

	// Assert
	// RED: stub returns "".
	if got != "claude-opus" {
		t.Errorf("ReadSourceModel = %q, want %q; "+
			"(RED: stub does not inspect frontmatter; with empty ModelKey, generic \"model\" must be tried)",
			got, "claude-opus")
	}
}

// ---------------------------------------------------------------------------
// ReadSourceModel — unset / error cases (NOT RED-VERIFIED)
// ---------------------------------------------------------------------------

// TestReadSourceModel_NoModelFields_ReturnsUnsetSourceModel verifies that when neither
// the descriptor's ModelKey nor the generic "model" key is present in the frontmatter,
// ReadSourceModel returns UnsetSourceModel (the empty string).
//
// NOT RED-VERIFIED: the stub always returns UnsetSourceModel, so this test passes
// trivially in the RED phase regardless of content. Its value is in pinning the GREEN
// behaviour when the real implementation lands.
func TestReadSourceModel_NoModelFields_ReturnsUnsetSourceModel(t *testing.T) {
	// Arrange: frontmatter with no model fields at all.
	content := fmBytes("name", "my-agent", "role", "subagent")
	srcDesc := srcDescWithModelKey("src_model")

	// Act
	got := app.ReadSourceModel(content, srcDesc)

	// Assert: must return the empty-string sentinel.
	if got != app.UnsetSourceModel {
		t.Errorf("ReadSourceModel = %q, want UnsetSourceModel (%q); "+
			"a file with no model fields must contribute to the unset group",
			got, app.UnsetSourceModel)
	}
}

// TestReadSourceModel_UnparseableContent_ReturnsUnsetSourceModel verifies that when the
// content bytes cannot be parsed as a document, ReadSourceModel returns UnsetSourceModel
// without panicking or returning an error. The file is classified by the existing per-file
// detection path; ReadSourceModel must not surface a parse failure.
//
// NOT RED-VERIFIED: same trivial pass as NoModelFields above.
func TestReadSourceModel_UnparseableContent_ReturnsUnsetSourceModel(t *testing.T) {
	// Arrange: clearly non-YAML content.
	content := []byte("\x00\x01\x02 not valid yaml at all \xff\xfe")
	srcDesc := srcDescWithModelKey("src_model")

	// Act — must not panic.
	got := app.ReadSourceModel(content, srcDesc)

	// Assert
	if got != app.UnsetSourceModel {
		t.Errorf("ReadSourceModel = %q for unparseable content, want UnsetSourceModel; "+
			"parse failures must be silently treated as unset",
			got)
	}
}

// TestReadSourceModel_NilContent_ReturnsUnsetSourceModel verifies that nil bytes are
// handled gracefully, returning UnsetSourceModel.
//
// NOT RED-VERIFIED: same trivial pass.
func TestReadSourceModel_NilContent_ReturnsUnsetSourceModel(t *testing.T) {
	srcDesc := srcDescWithModelKey("src_model")
	got := app.ReadSourceModel(nil, srcDesc)
	if got != app.UnsetSourceModel {
		t.Errorf("ReadSourceModel(nil) = %q, want UnsetSourceModel", got)
	}
}

// ---------------------------------------------------------------------------
// IndexSourceModels — Files length and content
// ---------------------------------------------------------------------------

// TestIndexSourceModels_FilesLengthMatchesInputPaths verifies the core invariant that
// Files contains exactly one entry per supplied path.
func TestIndexSourceModels_FilesLengthMatchesInputPaths(t *testing.T) {
	// Arrange: three files.
	dir := t.TempDir()
	p1 := writeSrcModelFile(t, dir, "a.src.md", "model-a")
	p2 := writeSrcModelFile(t, dir, "b.src.md", "model-b")
	p3 := writeSrcModelFile(t, dir, "c.src.md", "model-a")
	srcDesc := srcDescWithModelKey("src_model")

	// Act
	// RED: stub returns SourceModelIndex{} with nil Files.
	idx := app.IndexSourceModels([]string{p1, p2, p3}, srcDesc)

	// Assert: exactly one entry per path.
	if len(idx.Files) != 3 {
		t.Errorf("len(Files) = %d, want 3; IndexSourceModels must produce one entry per path "+
			"(RED: stub returns empty SourceModelIndex; deliver I7.2)", len(idx.Files))
	}
}

// TestIndexSourceModels_FilesPreservedInInputOrder verifies that Files entries appear in
// the same order as the supplied filePaths slice, regardless of the models they carry.
func TestIndexSourceModels_FilesPreservedInInputOrder(t *testing.T) {
	// Arrange: three files with known paths in a specific order.
	dir := t.TempDir()
	p1 := writeSrcModelFile(t, dir, "z-first.src.md", "model-z")
	p2 := writeSrcModelFile(t, dir, "a-second.src.md", "model-a")
	p3 := writeSrcModelFile(t, dir, "m-third.src.md", "model-m")
	paths := []string{p1, p2, p3}
	srcDesc := srcDescWithModelKey("src_model")

	// Act
	// RED: stub returns SourceModelIndex{} with nil Files.
	idx := app.IndexSourceModels(paths, srcDesc)

	if len(idx.Files) != 3 {
		t.Fatalf("len(Files) = %d, want 3 (RED: deliver I7.2)", len(idx.Files))
	}

	// Assert: Files[i].Path matches paths[i].
	for i, want := range paths {
		if idx.Files[i].Path != want {
			t.Errorf("Files[%d].Path = %q, want %q; input order must be preserved", i, idx.Files[i].Path, want)
		}
	}
}

// TestIndexSourceModels_SingleFile_ExtractsSourceModel verifies that for a single-file
// input the SourceModel is extracted correctly and Distinct contains exactly that model.
func TestIndexSourceModels_SingleFile_ExtractsSourceModel(t *testing.T) {
	// Arrange
	dir := t.TempDir()
	p := writeSrcModelFile(t, dir, "agent.src.md", "claude-3-sonnet")
	srcDesc := srcDescWithModelKey("src_model")

	// Act
	// RED: stub returns SourceModelIndex{}.
	idx := app.IndexSourceModels([]string{p}, srcDesc)

	if len(idx.Files) != 1 {
		t.Fatalf("len(Files) = %d, want 1 (RED: deliver I7.2)", len(idx.Files))
	}

	// Assert: source model extracted.
	if idx.Files[0].SourceModel != "claude-3-sonnet" {
		t.Errorf("Files[0].SourceModel = %q, want %q; "+
			"(RED: stub sets SourceModel to UnsetSourceModel; deliver I7.2 to extract from frontmatter)",
			idx.Files[0].SourceModel, "claude-3-sonnet")
	}

	// Assert: Distinct contains exactly the one model.
	if len(idx.Distinct) != 1 || idx.Distinct[0] != "claude-3-sonnet" {
		t.Errorf("Distinct = %v, want [\"claude-3-sonnet\"]", idx.Distinct)
	}
}

// TestIndexSourceModels_MultipleFilesSameModel_CollapseToOneDistinctEntry verifies that
// agents sharing a source model produce a single entry in Distinct — the pre-pass collapses
// duplicates so the question is asked exactly once for that model.
func TestIndexSourceModels_MultipleFilesSameModel_CollapseToOneDistinctEntry(t *testing.T) {
	// Arrange: three files all with the same source model.
	dir := t.TempDir()
	p1 := writeSrcModelFile(t, dir, "agent-a.src.md", "claude-3-sonnet")
	p2 := writeSrcModelFile(t, dir, "agent-b.src.md", "claude-3-sonnet")
	p3 := writeSrcModelFile(t, dir, "agent-c.src.md", "claude-3-sonnet")
	srcDesc := srcDescWithModelKey("src_model")

	// Act
	// RED: stub returns SourceModelIndex{}.
	idx := app.IndexSourceModels([]string{p1, p2, p3}, srcDesc)

	// Assert: Distinct has exactly one entry.
	if len(idx.Distinct) != 1 {
		t.Errorf("len(Distinct) = %d, want 1; three files with the same source model must "+
			"collapse to one distinct entry so the question is asked once "+
			"(RED: deliver I7.2)", len(idx.Distinct))
	}

	if len(idx.Distinct) == 1 && idx.Distinct[0] != "claude-3-sonnet" {
		t.Errorf("Distinct[0] = %q, want %q", idx.Distinct[0], "claude-3-sonnet")
	}
}

// TestIndexSourceModels_MultipleDistinctModels_SortedLexicographically verifies that
// Distinct is sorted lexicographically ascending so the prompt order is deterministic and
// assertable across runs.
func TestIndexSourceModels_MultipleDistinctModels_SortedLexicographically(t *testing.T) {
	// Arrange: files written in reverse lexicographic order to verify sorting.
	dir := t.TempDir()
	p1 := writeSrcModelFile(t, dir, "agent-z.src.md", "model-z")
	p2 := writeSrcModelFile(t, dir, "agent-a.src.md", "model-a")
	p3 := writeSrcModelFile(t, dir, "agent-m.src.md", "model-m")
	srcDesc := srcDescWithModelKey("src_model")

	// Act
	// RED: stub returns SourceModelIndex{}.
	idx := app.IndexSourceModels([]string{p1, p2, p3}, srcDesc)

	// Assert: Distinct is sorted lexicographically.
	want := []string{"model-a", "model-m", "model-z"}
	if len(idx.Distinct) != len(want) {
		t.Fatalf("len(Distinct) = %d, want %d (RED: deliver I7.2)", len(idx.Distinct), len(want))
	}
	for i, wantVal := range want {
		if idx.Distinct[i] != wantVal {
			t.Errorf("Distinct[%d] = %q, want %q; Distinct must be sorted lexicographically ascending",
				i, idx.Distinct[i], wantVal)
		}
	}
}

// TestIndexSourceModels_UnsetModelGroup_LastInDistinct verifies that agents with no model
// field (SourceModel = UnsetSourceModel = "") form a single group whose entry appears last
// in Distinct, after all named models, regardless of lexicographic position.
func TestIndexSourceModels_UnsetModelGroup_LastInDistinct(t *testing.T) {
	// Arrange: one named model ("model-a") and one with no model field.
	dir := t.TempDir()
	p1 := writeSrcModelFile(t, dir, "a-named.src.md", "model-a")
	p2 := writeSrcModelFile(t, dir, "b-unset.src.md", "") // no src_model field
	srcDesc := srcDescWithModelKey("src_model")

	// Act
	// RED: stub returns SourceModelIndex{}.
	idx := app.IndexSourceModels([]string{p1, p2}, srcDesc)

	// Assert: two entries, UnsetSourceModel last.
	if len(idx.Distinct) != 2 {
		t.Fatalf("len(Distinct) = %d, want 2 (RED: deliver I7.2)", len(idx.Distinct))
	}

	if idx.Distinct[0] != "model-a" {
		t.Errorf("Distinct[0] = %q, want %q (named models before unset)", idx.Distinct[0], "model-a")
	}
	if idx.Distinct[1] != app.UnsetSourceModel {
		t.Errorf("Distinct[1] = %q, want UnsetSourceModel (%q); unset group must be last",
			idx.Distinct[1], app.UnsetSourceModel)
	}
}

// TestIndexSourceModels_MixedNamedAndUnset_UnsetAlwaysLast verifies the ordering rule
// "UnsetSourceModel last" against a batch where the unset group would appear before named
// models if sorted lexicographically (since "" sorts before any printable character).
func TestIndexSourceModels_MixedNamedAndUnset_UnsetAlwaysLast(t *testing.T) {
	// Arrange: unset file first in the paths list; named model second.
	dir := t.TempDir()
	p1 := writeSrcModelFile(t, dir, "a-unset.src.md", "")
	p2 := writeSrcModelFile(t, dir, "b-named.src.md", "model-b")
	p3 := writeSrcModelFile(t, dir, "c-named.src.md", "model-a")
	srcDesc := srcDescWithModelKey("src_model")

	// Act
	// RED: stub returns SourceModelIndex{}.
	idx := app.IndexSourceModels([]string{p1, p2, p3}, srcDesc)

	// Assert: named models first, unset last.
	if len(idx.Distinct) != 3 {
		t.Fatalf("len(Distinct) = %d, want 3 (RED: deliver I7.2)", len(idx.Distinct))
	}

	last := idx.Distinct[len(idx.Distinct)-1]
	if last != app.UnsetSourceModel {
		t.Errorf("Distinct last = %q, want UnsetSourceModel (%q); "+
			"unset group must be last regardless of lexicographic sort position",
			last, app.UnsetSourceModel)
	}
}

// TestIndexSourceModels_NonExistentPath_RecordsReadError verifies that a path that does
// not exist produces a SourceFile entry with ReadErr != nil and SourceModel = UnsetSourceModel.
// A read error is per-file, never a whole-run failure: IndexSourceModels must not panic or
// propagate the error upward.
func TestIndexSourceModels_NonExistentPath_RecordsReadError(t *testing.T) {
	// Arrange: a path guaranteed not to exist.
	dir := t.TempDir()
	nonexistent := filepath.Join(dir, "does-not-exist.src.md")
	srcDesc := srcDescWithModelKey("src_model")

	// Act
	// RED: stub returns SourceModelIndex{} with nil Files.
	idx := app.IndexSourceModels([]string{nonexistent}, srcDesc)

	if len(idx.Files) != 1 {
		t.Fatalf("len(Files) = %d, want 1; a read error must produce a Files entry, not be silently skipped "+
			"(RED: deliver I7.2)", len(idx.Files))
	}

	// Assert: read error recorded.
	if idx.Files[0].ReadErr == nil {
		t.Errorf("Files[0].ReadErr = nil, want non-nil; a non-existent path must record the read error; "+
			"(RED: stub sets ReadErr to nil)")
	}

	// Assert: SourceModel falls back to UnsetSourceModel when content cannot be read.
	if idx.Files[0].SourceModel != app.UnsetSourceModel {
		t.Errorf("Files[0].SourceModel = %q, want UnsetSourceModel when ReadErr is non-nil",
			idx.Files[0].SourceModel)
	}

	// Assert: Content is nil when ReadErr is non-nil.
	// The design specifies "Content is the file's bytes. Nil when ReadErr is non-nil."
	// A non-nil Content alongside a ReadErr would let a downstream loop silently process
	// bad bytes from a partially-read or garbage-initialized buffer.
	// RED: stub returns SourceModelIndex{} with nil Files, so this assertion is not exercised
	// until I7.2 delivers the real implementation; it pins the invariant for GREEN phase.
	if idx.Files[0].ReadErr != nil && idx.Files[0].Content != nil {
		t.Error("Files[0].Content is non-nil when ReadErr is non-nil; "+
			"Content must be nil when the file could not be read "+
			"(design invariant: Content is Nil when ReadErr is non-nil)")
	}
}

// TestIndexSourceModels_ReadErrorContributesUnsetModelOnlyOnce verifies that when multiple
// files fail to read, they all contribute UnsetSourceModel but Distinct contains that key
// at most once (the unset group is deduplicated like any other group).
func TestIndexSourceModels_ReadErrorContributesUnsetModelOnlyOnce(t *testing.T) {
	// Arrange: two non-existent paths.
	dir := t.TempDir()
	p1 := filepath.Join(dir, "missing-a.src.md")
	p2 := filepath.Join(dir, "missing-b.src.md")
	srcDesc := srcDescWithModelKey("src_model")

	// Act
	// RED: stub returns SourceModelIndex{}.
	idx := app.IndexSourceModels([]string{p1, p2}, srcDesc)

	if len(idx.Files) != 2 {
		t.Fatalf("len(Files) = %d, want 2 (RED: deliver I7.2)", len(idx.Files))
	}

	// Assert: Distinct has exactly one UnsetSourceModel entry (deduplicated).
	unsetCount := 0
	for _, d := range idx.Distinct {
		if d == app.UnsetSourceModel {
			unsetCount++
		}
	}
	if unsetCount != 1 {
		t.Errorf("UnsetSourceModel appears %d time(s) in Distinct, want exactly 1; "+
			"multiple read-error files must contribute only one unset-group entry "+
			"(RED: deliver I7.2)", unsetCount)
	}
}

// ---------------------------------------------------------------------------
// IndexSourceModels — empty input (NOT RED-VERIFIED for trivial pass cases)
// ---------------------------------------------------------------------------

// TestIndexSourceModels_EmptyFilePaths_ReturnsEmptyIndex verifies that an empty path list
// produces an index with empty (not nil-panicking) Files and Distinct slices.
//
// NOT RED-VERIFIED: the stub returns SourceModelIndex{} which satisfies len==0 for both
// slices. This test pins the GREEN contract without adding RED signal.
func TestIndexSourceModels_EmptyFilePaths_ReturnsEmptyIndex(t *testing.T) {
	srcDesc := srcDescWithModelKey("src_model")
	idx := app.IndexSourceModels(nil, srcDesc)

	if len(idx.Files) != 0 {
		t.Errorf("len(Files) = %d, want 0 for empty input", len(idx.Files))
	}
	if len(idx.Distinct) != 0 {
		t.Errorf("len(Distinct) = %d, want 0 for empty input", len(idx.Distinct))
	}
}
