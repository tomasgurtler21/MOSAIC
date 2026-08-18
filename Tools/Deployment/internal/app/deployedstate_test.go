package app

// deployedstate_test.go covers the deployed-artifact probe and supporting helpers:
//
//   probeDeployedArtifact — reads one file and returns its state
//   probeDeployedState    — probes a full set of planned paths
//   extractDeployedWorkflows — scans content for workflow section markers
//   discoverExistingWorkflows — returns IDs from probe state (new signature)
//
// All tests use t.TempDir() for filesystem fixtures, matching the existing app test style.
// These are white-box tests so they can call package-internal functions directly.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/manifest"
	"mosaic-deploy/internal/plan"
)

// ---------------------------------------------------------------------------
// probeDeployedArtifact — absent file
// ---------------------------------------------------------------------------

// TestProbeDeployedArtifact_AbsentFile_PresentIsFalse verifies that a file that does not
// exist at the target path is reported as not present.
func TestProbeDeployedArtifact_AbsentFile_PresentIsFalse(t *testing.T) {
	ws := t.TempDir()

	state := probeDeployedArtifact(ws, "nonexistent.md", "")

	if state.Present {
		t.Error("expected Present: false for absent file, got true")
	}
}

// TestProbeDeployedArtifact_AbsentFile_ContentHashIsEmpty verifies that a missing file does
// not yield a zero hash — it yields an empty ContentHash, distinguishing "absent" from
// "present but empty".
func TestProbeDeployedArtifact_AbsentFile_ContentHashIsEmpty(t *testing.T) {
	ws := t.TempDir()

	state := probeDeployedArtifact(ws, "nonexistent.md", "")

	if state.ContentHash != "" {
		t.Errorf("absent file must have empty ContentHash, got %q", state.ContentHash)
	}
	// Guard: a zero sha256 hex would be misleading as a hash value for a missing file.
	zeroHash := "sha256:0000000000000000000000000000000000000000000000000000000000000000"
	if state.ContentHash == zeroHash {
		t.Error("absent file must not carry a zero hash; ContentHash must be empty")
	}
}

// ---------------------------------------------------------------------------
// probeDeployedArtifact — present file, full frontmatter
// ---------------------------------------------------------------------------

// TestProbeDeployedArtifact_FullFrontmatter_PresentAndVersionsPopulated verifies that when a
// file is readable and carries all three version stamps, the state reflects each stamp exactly.
func TestProbeDeployedArtifact_FullFrontmatter_PresentAndVersionsPopulated(t *testing.T) {
	ws := t.TempDir()
	content := []byte("---\nversion: \"2.0\"\ntransform_version: \"1.5\"\ninjections_version: \"1.2\"\n---\n\nAgent body.\n")
	writeFile(t, ws, "agent.md", content)

	state := probeDeployedArtifact(ws, "agent.md", "")

	if !state.Present {
		t.Fatal("expected Present: true for a readable file, got false")
	}
	if state.Version != "2.0" {
		t.Errorf("Version = %q, want %q", state.Version, "2.0")
	}
	if state.TransformVersion != "1.5" {
		t.Errorf("TransformVersion = %q, want %q", state.TransformVersion, "1.5")
	}
	if state.InjectionsVersion != "1.2" {
		t.Errorf("InjectionsVersion = %q, want %q", state.InjectionsVersion, "1.2")
	}
}

// TestProbeDeployedArtifact_FullFrontmatter_ContentHashMatchesManifestHash verifies that the
// content hash produced by the probe is computed with the same algorithm that the manifest
// uses, so that probe hashes and recorded hashes are directly comparable.
func TestProbeDeployedArtifact_FullFrontmatter_ContentHashMatchesManifestHash(t *testing.T) {
	ws := t.TempDir()
	content := []byte("---\nversion: \"2.0\"\ntransform_version: \"1.5\"\ninjections_version: \"1.2\"\n---\n\nAgent body.\n")
	writeFile(t, ws, "agent.md", content)

	state := probeDeployedArtifact(ws, "agent.md", "")

	want := manifest.Hash(content)
	if state.ContentHash != want {
		t.Errorf("ContentHash = %q, want %q (manifest.Hash form)", state.ContentHash, want)
	}
	if !strings.HasPrefix(state.ContentHash, "sha256:") {
		t.Errorf("ContentHash %q must start with the 'sha256:' prefix", state.ContentHash)
	}
}

// ---------------------------------------------------------------------------
// probeDeployedArtifact — present file, partial frontmatter
// ---------------------------------------------------------------------------

// TestProbeDeployedArtifact_PartialFrontmatter_MissingFieldsAreEmpty verifies that when the
// deployed file only carries some version fields, missing fields are reported as empty strings
// rather than substituting any default.
func TestProbeDeployedArtifact_PartialFrontmatter_MissingFieldsAreEmpty(t *testing.T) {
	ws := t.TempDir()
	// Only the "version" field is present; transform_version and injections_version are absent.
	content := []byte("---\nversion: \"3.0\"\n---\n\nAgent without transform/injections stamps.\n")
	writeFile(t, ws, "agent.md", content)

	state := probeDeployedArtifact(ws, "agent.md", "")

	if !state.Present {
		t.Fatal("expected Present: true, got false")
	}
	if state.Version != "3.0" {
		t.Errorf("Version = %q, want %q", state.Version, "3.0")
	}
	if state.TransformVersion != "" {
		t.Errorf("TransformVersion = %q, want empty (key absent from frontmatter)", state.TransformVersion)
	}
	if state.InjectionsVersion != "" {
		t.Errorf("InjectionsVersion = %q, want empty (key absent from frontmatter)", state.InjectionsVersion)
	}
}

// ---------------------------------------------------------------------------
// probeDeployedArtifact — present file, malformed frontmatter
// ---------------------------------------------------------------------------

// TestProbeDeployedArtifact_MalformedFrontmatter_PresentTrueVersionsEmpty verifies that when
// a file is readable but its frontmatter cannot be parsed, the probe still reports Present:
// true and the computed hash, but all version fields are empty strings (graceful degradation).
func TestProbeDeployedArtifact_MalformedFrontmatter_PresentTrueVersionsEmpty(t *testing.T) {
	ws := t.TempDir()
	// Duplicate key causes the frontmatter parser to return an error.
	content := []byte("---\nversion: \"1.0\"\nversion: \"2.0\"\n---\n\nBody after malformed frontmatter.\n")
	writeFile(t, ws, "agent.md", content)

	state := probeDeployedArtifact(ws, "agent.md", "")

	if !state.Present {
		t.Fatal("malformed frontmatter must not make the file appear absent; got Present: false")
	}
	if state.ContentHash != manifest.Hash(content) {
		t.Errorf("ContentHash mismatch for malformed-frontmatter file")
	}
	if state.Version != "" || state.TransformVersion != "" || state.InjectionsVersion != "" {
		t.Errorf("all version fields must be empty when frontmatter is malformed; got Version=%q TransformVersion=%q InjectionsVersion=%q",
			state.Version, state.TransformVersion, state.InjectionsVersion)
	}
}

// TestProbeDeployedArtifact_NoFrontmatter_PresentTrueVersionsEmpty verifies that a file with
// no YAML frontmatter block at all is treated as Present but with no version information.
func TestProbeDeployedArtifact_NoFrontmatter_PresentTrueVersionsEmpty(t *testing.T) {
	ws := t.TempDir()
	content := []byte("This file has no frontmatter at all. Just raw content.\n")
	writeFile(t, ws, "agent.md", content)

	state := probeDeployedArtifact(ws, "agent.md", "")

	if !state.Present {
		t.Fatal("expected Present: true for a readable file with no frontmatter")
	}
	if state.Version != "" || state.TransformVersion != "" || state.InjectionsVersion != "" {
		t.Error("expected all version fields empty when no frontmatter is present")
	}
}

// ---------------------------------------------------------------------------
// probeDeployedArtifact — directory target
// ---------------------------------------------------------------------------

// TestProbeDeployedArtifact_TargetIsDirectory_PresentIsFalse verifies that when the target
// path is a directory (as hook bundle paths typically are), the probe reports Present: false
// rather than attempting to hash the directory contents.
func TestProbeDeployedArtifact_TargetIsDirectory_PresentIsFalse(t *testing.T) {
	ws := t.TempDir()
	dir := filepath.Join(ws, "hook-bundle")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir hook-bundle: %v", err)
	}

	state := probeDeployedArtifact(ws, "hook-bundle", "")

	if state.Present {
		t.Error("expected Present: false when target path is a directory, got true")
	}
	if state.ContentHash != "" {
		t.Errorf("expected empty ContentHash for directory target, got %q", state.ContentHash)
	}
}

// ---------------------------------------------------------------------------
// probeDeployedArtifact — orchestrator with workflow section blocks
// ---------------------------------------------------------------------------

// TestProbeDeployedArtifact_OrchestratorWithWorkflows_WorkflowsPopulated verifies that when
// the deployed file contains managed-form workflow blocks with version attributes, the
// Workflows field carries each workflow's ID and version in first-occurrence order.
// The production deployed shape is <Workflow type="managed"> nested inside a managed
// <AvailableWorkflows> region inside a core <Identity> section.
func TestProbeDeployedArtifact_OrchestratorWithWorkflows_WorkflowsPopulated(t *testing.T) {
	ws := t.TempDir()
	content := []byte("---\nversion: \"1.0\"\n---\n\n" +
		"<Identity type=\"core\">\n" +
		"<AvailableWorkflows type=\"managed\">\n" +
		"<Workflow type=\"managed\" name=\"quick-fix\" version=\"3.0\">\n" +
		"Quick fix workflow body.\n" +
		"</Workflow>\n" +
		"<Workflow type=\"managed\" name=\"code-review\" version=\"2.1\">\n" +
		"Code review workflow body.\n" +
		"</Workflow>\n" +
		"</AvailableWorkflows>\n" +
		"</Identity>\n")
	writeFile(t, ws, "orchestrator.md", content)

	state := probeDeployedArtifact(ws, "orchestrator.md", "")

	if !state.Present {
		t.Fatal("expected Present: true")
	}
	if len(state.Workflows) != 2 {
		t.Fatalf("expected 2 workflows in Workflows, got %d: %v", len(state.Workflows), state.Workflows)
	}
	if state.Workflows[0].ID != "quick-fix" {
		t.Errorf("Workflows[0].ID = %q, want %q", state.Workflows[0].ID, "quick-fix")
	}
	if state.Workflows[0].Version != "3.0" {
		t.Errorf("Workflows[0].Version = %q, want %q", state.Workflows[0].Version, "3.0")
	}
	if state.Workflows[1].ID != "code-review" {
		t.Errorf("Workflows[1].ID = %q, want %q", state.Workflows[1].ID, "code-review")
	}
	if state.Workflows[1].Version != "2.1" {
		t.Errorf("Workflows[1].Version = %q, want %q", state.Workflows[1].Version, "2.1")
	}
}

// TestProbeDeployedArtifact_OrchestratorWithDuplicateWorkflowBlock_Deduplicates verifies that
// when the same workflow ID appears more than once (which should not happen in practice),
// only the first occurrence is retained in the Workflows list.
func TestProbeDeployedArtifact_OrchestratorWithDuplicateWorkflowBlock_Deduplicates(t *testing.T) {
	ws := t.TempDir()
	content := []byte("<Identity type=\"core\">\n" +
		"<AvailableWorkflows type=\"managed\">\n" +
		"<Workflow type=\"managed\" name=\"quick-fix\" version=\"3.0\">\n" +
		"First occurrence.\n" +
		"</Workflow>\n" +
		"<Workflow type=\"managed\" name=\"quick-fix\" version=\"3.0\">\n" +
		"Second occurrence (duplicate).\n" +
		"</Workflow>\n" +
		"</AvailableWorkflows>\n" +
		"</Identity>\n")
	writeFile(t, ws, "orchestrator.md", content)

	state := probeDeployedArtifact(ws, "orchestrator.md", "")

	if len(state.Workflows) != 1 {
		t.Errorf("expected 1 deduplicated workflow, got %d: %v", len(state.Workflows), state.Workflows)
	}
}

// TestProbeDeployedArtifact_WorkflowBlockWithNoVersionAttribute_VersionIsEmpty verifies that a
// workflow section that carries no version attribute yields an empty Version field, not an error
// or a default value.
func TestProbeDeployedArtifact_WorkflowBlockWithNoVersionAttribute_VersionIsEmpty(t *testing.T) {
	ws := t.TempDir()
	content := []byte("<Identity type=\"core\">\n" +
		"<AvailableWorkflows type=\"managed\">\n" +
		"<Workflow type=\"managed\" name=\"old-workflow\">\n" +
		"This block has no version attribute.\n" +
		"</Workflow>\n" +
		"</AvailableWorkflows>\n" +
		"</Identity>\n")
	writeFile(t, ws, "orchestrator.md", content)

	state := probeDeployedArtifact(ws, "orchestrator.md", "")

	if len(state.Workflows) != 1 {
		t.Fatalf("expected 1 workflow, got %d", len(state.Workflows))
	}
	if state.Workflows[0].Version != "" {
		t.Errorf("Workflows[0].Version = %q, want empty string when no version comment present",
			state.Workflows[0].Version)
	}
}

// TestProbeDeployedArtifact_NonOrchestratorFile_WorkflowsNil verifies that a plain agent
// file without any workflow section markers has a nil Workflows field, not an empty slice.
func TestProbeDeployedArtifact_NonOrchestratorFile_WorkflowsNil(t *testing.T) {
	ws := t.TempDir()
	content := []byte("---\nversion: \"1.0\"\n---\n\nPlain worker agent with no workflow sections.\n")
	writeFile(t, ws, "worker.md", content)

	state := probeDeployedArtifact(ws, "worker.md", "")

	if state.Workflows != nil {
		t.Errorf("expected nil Workflows for a file with no workflow section markers, got %v", state.Workflows)
	}
}

// ---------------------------------------------------------------------------
// probeDeployedArtifact — model ID extraction (new behaviour)
// ---------------------------------------------------------------------------

// TestProbeDeployedArtifact_ModelKeyPresentInFrontmatter_ModelIDExtractedVerbatim verifies that
// when a deployed file's frontmatter carries the harness model key, the returned state's
// ModelID holds that value verbatim — no normalisation, formatting, or validation.
func TestProbeDeployedArtifact_ModelKeyPresentInFrontmatter_ModelIDExtractedVerbatim(t *testing.T) {
	ws := t.TempDir()
	content := []byte("---\nversion: \"1.0\"\nmodel: \"claude-opus-4-5\"\n---\n\nAgent body.\n")
	writeFile(t, ws, "agent.md", content)

	state := probeDeployedArtifact(ws, "agent.md", "model")

	if state.ModelID != "claude-opus-4-5" {
		t.Errorf("ModelID = %q, want %q; model key in frontmatter must be extracted verbatim",
			state.ModelID, "claude-opus-4-5")
	}
}

// TestProbeDeployedArtifact_ModelKeyAbsentFromFrontmatter_ModelIDEmpty verifies that when
// the frontmatter is valid but does not carry the specified model key, ModelID is empty.
func TestProbeDeployedArtifact_ModelKeyAbsentFromFrontmatter_ModelIDEmpty(t *testing.T) {
	ws := t.TempDir()
	// Frontmatter carries version stamps but no model key.
	content := []byte("---\nversion: \"1.0\"\ntransform_version: \"2.0\"\n---\n\nAgent without model.\n")
	writeFile(t, ws, "agent.md", content)

	state := probeDeployedArtifact(ws, "agent.md", "model")

	if state.ModelID != "" {
		t.Errorf("ModelID = %q, want empty; model key absent from frontmatter must yield empty ModelID",
			state.ModelID)
	}
}

// TestProbeDeployedArtifact_EmptyModelKey_ModelIDEmpty verifies that passing an empty
// modelKey causes no lookup to be attempted, yielding an empty ModelID even when the
// frontmatter carries a field that might match — an empty key is a legitimate harness
// configuration meaning "this harness does not emit a model".
func TestProbeDeployedArtifact_EmptyModelKey_ModelIDEmpty(t *testing.T) {
	ws := t.TempDir()
	// File carries a "model" key, but the caller says modelKey="" (harness emits no model).
	content := []byte("---\nversion: \"1.0\"\nmodel: \"claude-opus-4-5\"\n---\n\nAgent body.\n")
	writeFile(t, ws, "agent.md", content)

	state := probeDeployedArtifact(ws, "agent.md", "")

	if state.ModelID != "" {
		t.Errorf("ModelID = %q, want empty; empty modelKey must produce empty ModelID with no lookup",
			state.ModelID)
	}
}

// TestProbeDeployedArtifact_AbsentFile_ModelIDEmpty verifies that an absent file yields an
// empty ModelID, consistent with Present: false and the never-error contract.
func TestProbeDeployedArtifact_AbsentFile_ModelIDEmpty(t *testing.T) {
	ws := t.TempDir()

	state := probeDeployedArtifact(ws, "nonexistent.md", "model")

	if state.Present {
		t.Fatal("expected Present: false for absent file")
	}
	if state.ModelID != "" {
		t.Errorf("ModelID = %q, want empty for absent file", state.ModelID)
	}
}

// TestProbeDeployedArtifact_MalformedFrontmatter_ModelIDEmpty verifies that when frontmatter
// cannot be parsed, ModelID is empty — graceful degradation, not an error or panic.
func TestProbeDeployedArtifact_MalformedFrontmatter_ModelIDEmpty(t *testing.T) {
	ws := t.TempDir()
	// Duplicate key triggers a parse error.
	content := []byte("---\nversion: \"1.0\"\nversion: \"2.0\"\n---\n\nBody.\n")
	writeFile(t, ws, "agent.md", content)

	state := probeDeployedArtifact(ws, "agent.md", "model")

	if state.ModelID != "" {
		t.Errorf("ModelID = %q, want empty when frontmatter is malformed", state.ModelID)
	}
}

// TestProbeDeployedArtifact_NoFrontmatter_ModelIDEmpty verifies that a file with no YAML
// frontmatter block yields an empty ModelID.
func TestProbeDeployedArtifact_NoFrontmatter_ModelIDEmpty(t *testing.T) {
	ws := t.TempDir()
	content := []byte("This file has no frontmatter at all.\n")
	writeFile(t, ws, "agent.md", content)

	state := probeDeployedArtifact(ws, "agent.md", "model")

	if state.ModelID != "" {
		t.Errorf("ModelID = %q, want empty when file has no frontmatter", state.ModelID)
	}
}

// TestProbeDeployedArtifact_DirectoryTarget_ModelIDEmpty verifies that when the target path
// is a directory, ModelID is empty alongside Present: false.
func TestProbeDeployedArtifact_DirectoryTarget_ModelIDEmpty(t *testing.T) {
	ws := t.TempDir()
	dir := filepath.Join(ws, "hook-bundle")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir hook-bundle: %v", err)
	}

	state := probeDeployedArtifact(ws, "hook-bundle", "model")

	if state.Present {
		t.Fatal("expected Present: false when target path is a directory")
	}
	if state.ModelID != "" {
		t.Errorf("ModelID = %q, want empty for directory target", state.ModelID)
	}
}

// TestProbeDeployedArtifact_ModelIDDoesNotAffectVersionFields verifies that extracting a
// model ID from frontmatter leaves the existing version fields (Version, TransformVersion,
// InjectionsVersion) unaffected — the model is extracted in the same parse pass without
// perturbing the other scalars.
func TestProbeDeployedArtifact_ModelIDDoesNotAffectVersionFields(t *testing.T) {
	ws := t.TempDir()
	content := []byte("---\nversion: \"2.0\"\ntransform_version: \"1.5\"\ninjections_version: \"1.2\"\nmodel: \"claude-sonnet-4\"\n---\n\nAgent body.\n")
	writeFile(t, ws, "agent.md", content)

	state := probeDeployedArtifact(ws, "agent.md", "model")

	if state.ModelID != "claude-sonnet-4" {
		t.Errorf("ModelID = %q, want %q", state.ModelID, "claude-sonnet-4")
	}
	if state.Version != "2.0" {
		t.Errorf("Version = %q, want %q; version field must be unaffected by model extraction", state.Version, "2.0")
	}
	if state.TransformVersion != "1.5" {
		t.Errorf("TransformVersion = %q, want %q; transform_version field must be unaffected", state.TransformVersion, "1.5")
	}
	if state.InjectionsVersion != "1.2" {
		t.Errorf("InjectionsVersion = %q, want %q; injections_version field must be unaffected", state.InjectionsVersion, "1.2")
	}
}

// TestProbeDeployedArtifact_ModelIDDoesNotAffectWorkflows verifies that model extraction does
// not interfere with workflow section marker detection, so orchestrator files carrying both
// a model key and workflow sections are handled correctly.
func TestProbeDeployedArtifact_ModelIDDoesNotAffectWorkflows(t *testing.T) {
	ws := t.TempDir()
	content := []byte("---\nversion: \"1.0\"\nmodel: \"claude-opus-4-5\"\n---\n\n" +
		"<Identity type=\"core\">\n" +
		"<AvailableWorkflows type=\"managed\">\n" +
		"<Workflow type=\"managed\" name=\"quick-fix\" version=\"3.0\">\n" +
		"Quick fix workflow body.\n" +
		"</Workflow>\n" +
		"</AvailableWorkflows>\n" +
		"</Identity>\n")
	writeFile(t, ws, "orchestrator.md", content)

	state := probeDeployedArtifact(ws, "orchestrator.md", "model")

	if state.ModelID != "claude-opus-4-5" {
		t.Errorf("ModelID = %q, want %q", state.ModelID, "claude-opus-4-5")
	}
	if len(state.Workflows) != 1 {
		t.Fatalf("expected 1 workflow, got %d; workflow extraction must be unaffected by model extraction", len(state.Workflows))
	}
	if state.Workflows[0].ID != "quick-fix" {
		t.Errorf("Workflows[0].ID = %q, want %q", state.Workflows[0].ID, "quick-fix")
	}
}

// TestProbeDeployedArtifact_ModelIDNotIncludedInHasVersionInfo verifies that a file carrying
// only a model key (and no version stamps) reports HasVersionInfo() == false. ModelID is not
// a version stamp and must not affect staleness classification.
func TestProbeDeployedArtifact_ModelIDNotIncludedInHasVersionInfo(t *testing.T) {
	ws := t.TempDir()
	// File has a model key but no version, transform_version, or injections_version.
	content := []byte("---\nmodel: \"claude-opus-4-5\"\n---\n\nAgent body.\n")
	writeFile(t, ws, "agent.md", content)

	state := probeDeployedArtifact(ws, "agent.md", "model")

	if state.ModelID != "claude-opus-4-5" {
		t.Errorf("ModelID = %q, want %q", state.ModelID, "claude-opus-4-5")
	}
	if state.HasVersionInfo() {
		t.Error("HasVersionInfo() must return false when only ModelID is present; ModelID is not a version stamp")
	}
}

// TestProbeDeployedArtifact_CustomModelKey_ExtractsFromCorrectKey verifies that the probe
// uses whatever key the caller specifies, not a hardcoded string. This confirms that harnesses
// using a non-default model frontmatter key (e.g. "active_model") are correctly supported.
func TestProbeDeployedArtifact_CustomModelKey_ExtractsFromCorrectKey(t *testing.T) {
	ws := t.TempDir()
	// Frontmatter carries "active_model" (not "model") as the model key.
	content := []byte("---\nversion: \"1.0\"\nactive_model: \"gpt-4o\"\n---\n\nAgent body.\n")
	writeFile(t, ws, "agent.md", content)

	// Probe with the correct key.
	stateCorrect := probeDeployedArtifact(ws, "agent.md", "active_model")
	if stateCorrect.ModelID != "gpt-4o" {
		t.Errorf("ModelID = %q, want %q when probing with the correct key", stateCorrect.ModelID, "gpt-4o")
	}

	// Probe with a wrong key — must yield empty ModelID.
	stateWrong := probeDeployedArtifact(ws, "agent.md", "model")
	if stateWrong.ModelID != "" {
		t.Errorf("ModelID = %q, want empty when probing with a key that is absent from frontmatter", stateWrong.ModelID)
	}
}

// TestProbeDeployedArtifact_ModelKeyIsNonScalar_ModelIDEmpty verifies that when the frontmatter
// key named by modelKey maps to a non-scalar value (such as a YAML mapping or sequence), the
// probe returns an empty ModelID rather than panicking or producing a stringified representation.
// Non-scalar values are not valid model IDs and must degrade gracefully to an empty string,
// consistent with the never-error / graceful-degradation contract.
func TestProbeDeployedArtifact_ModelKeyIsNonScalar_ModelIDEmpty(t *testing.T) {
	ws := t.TempDir()
	// "model" maps to a YAML mapping (non-scalar value) — not a valid model ID string.
	content := []byte("---\nversion: \"1.0\"\nmodel:\n  provider: anthropic\n  name: claude\n---\n\nAgent body.\n")
	writeFile(t, ws, "agent.md", content)

	state := probeDeployedArtifact(ws, "agent.md", "model")

	if state.ModelID != "" {
		t.Errorf("ModelID = %q, want empty when model key maps to a non-scalar YAML value; "+
			"non-scalar values must not be coerced to a model ID string", state.ModelID)
	}
}

// TestProbeDeployedArtifact_ModelIDWithLeadingTrailingSpaces_PreservedVerbatim verifies that
// when the model frontmatter value is a quoted YAML string containing leading and trailing
// spaces, those spaces are preserved verbatim in ModelID. The probe does not normalise or trim
// model values — consistent with the verbatim extraction contract shared by the other scalar
// fields (Version, TransformVersion, InjectionsVersion).
func TestProbeDeployedArtifact_ModelIDWithLeadingTrailingSpaces_PreservedVerbatim(t *testing.T) {
	ws := t.TempDir()
	// Quoted YAML string with intentional leading and trailing spaces.
	// YAML preserves spaces inside quoted scalars, so the parsed value is "  claude-opus-4-5  ".
	content := []byte("---\nversion: \"1.0\"\nmodel: \"  claude-opus-4-5  \"\n---\n\nAgent body.\n")
	writeFile(t, ws, "agent.md", content)

	state := probeDeployedArtifact(ws, "agent.md", "model")

	want := "  claude-opus-4-5  "
	if state.ModelID != want {
		t.Errorf("ModelID = %q, want %q; model ID value must be preserved verbatim including whitespace",
			state.ModelID, want)
	}
}

// ---------------------------------------------------------------------------
// extractDeployedWorkflows
// ---------------------------------------------------------------------------

// TestExtractDeployedWorkflows_MultipleBlocksWithVersionAttributes_ReturnsAllInOrder verifies
// that each managed workflow block is paired with the version from its version attribute
// in first-occurrence (document) order. The production deployed shape is
// <Workflow type="managed"> blocks nested inside <AvailableWorkflows type="managed"> inside
// a core <Identity> section.
func TestExtractDeployedWorkflows_MultipleBlocksWithVersionAttributes_ReturnsAllInOrder(t *testing.T) {
	data := []byte(
		"<Identity type=\"core\">\n" +
			"<AvailableWorkflows type=\"managed\">\n" +
			"<Workflow type=\"managed\" name=\"quick-fix\" version=\"3.0\">\n" +
			"Content.\n" +
			"</Workflow>\n" +
			"<Workflow type=\"managed\" name=\"code-review\" version=\"2.1\">\n" +
			"Content.\n" +
			"</Workflow>\n" +
			"</AvailableWorkflows>\n" +
			"</Identity>\n")

	wfs := extractDeployedWorkflows(data)

	if len(wfs) != 2 {
		t.Fatalf("expected 2 workflows, got %d: %v", len(wfs), wfs)
	}
	if wfs[0].ID != "quick-fix" || wfs[0].Version != "3.0" {
		t.Errorf("wfs[0] = %+v, want {ID:quick-fix Version:3.0}", wfs[0])
	}
	if wfs[1].ID != "code-review" || wfs[1].Version != "2.1" {
		t.Errorf("wfs[1] = %+v, want {ID:code-review Version:2.1}", wfs[1])
	}
}

// TestExtractDeployedWorkflows_DuplicateBlock_OnlyFirstOccurrenceKept verifies that when the
// same workflow ID appears more than once in managed form, only the first occurrence is included.
func TestExtractDeployedWorkflows_DuplicateBlock_OnlyFirstOccurrenceKept(t *testing.T) {
	data := []byte(
		"<Identity type=\"core\">\n" +
			"<AvailableWorkflows type=\"managed\">\n" +
			"<Workflow type=\"managed\" name=\"quick-fix\" version=\"3.0\">\n" +
			"First occurrence.\n" +
			"</Workflow>\n" +
			"<Workflow type=\"managed\" name=\"quick-fix\" version=\"3.0\">\n" +
			"Duplicate — must be ignored.\n" +
			"</Workflow>\n" +
			"</AvailableWorkflows>\n" +
			"</Identity>\n")

	wfs := extractDeployedWorkflows(data)

	if len(wfs) != 1 {
		t.Errorf("expected 1 workflow after deduplication, got %d: %v", len(wfs), wfs)
	}
}

// TestExtractDeployedWorkflows_BlockWithNoVersionAttribute_VersionIsEmpty verifies that a
// managed-form block without a version attribute yields an entry with an empty Version.
func TestExtractDeployedWorkflows_BlockWithNoVersionAttribute_VersionIsEmpty(t *testing.T) {
	data := []byte(
		"<Identity type=\"core\">\n" +
			"<AvailableWorkflows type=\"managed\">\n" +
			"<Workflow type=\"managed\" name=\"legacy\">\n" +
			"Content without a version attribute.\n" +
			"</Workflow>\n" +
			"</AvailableWorkflows>\n" +
			"</Identity>\n")

	wfs := extractDeployedWorkflows(data)

	if len(wfs) != 1 {
		t.Fatalf("expected 1 workflow, got %d", len(wfs))
	}
	if wfs[0].Version != "" {
		t.Errorf("Version = %q, want empty string when no workflow-version comment present", wfs[0].Version)
	}
}

// TestExtractDeployedWorkflows_NoWorkflowBlocks_ReturnsNil verifies that content with no
// workflow section markers returns nil (not an empty slice).
func TestExtractDeployedWorkflows_NoWorkflowBlocks_ReturnsNil(t *testing.T) {
	data := []byte("Just regular agent content with no workflow sections.\n")

	wfs := extractDeployedWorkflows(data)

	if wfs != nil {
		t.Errorf("expected nil for content without workflow blocks, got %v", wfs)
	}
}

// TestExtractDeployedWorkflows_VersionAttributeWithExtraWhitespace_VersionTrimmed verifies
// that leading and trailing whitespace around the version attribute value is stripped for
// a managed-form workflow block.
func TestExtractDeployedWorkflows_VersionAttributeWithExtraWhitespace_VersionTrimmed(t *testing.T) {
	data := []byte(
		"<Identity type=\"core\">\n" +
			"<AvailableWorkflows type=\"managed\">\n" +
			"<Workflow type=\"managed\" name=\"padded\" version=\"  4.2  \">\n" +
			"Content.\n" +
			"</Workflow>\n" +
			"</AvailableWorkflows>\n" +
			"</Identity>\n")

	wfs := extractDeployedWorkflows(data)

	if len(wfs) != 1 {
		t.Fatalf("expected 1 workflow, got %d", len(wfs))
	}
	if wfs[0].Version != "4.2" {
		t.Errorf("Version = %q, want %q (whitespace must be trimmed)", wfs[0].Version, "4.2")
	}
}

// TestExtractDeployedWorkflows_UnclosedBlock_WorkflowCapturedWithVersion verifies that a
// managed-form workflow block with an opening tag but no closing tag (e.g. a truncated
// deployed file) is still captured: the ID and version come from the opening tag's attributes.
// Absent closing tags must not silently discard discovered workflow IDs.
func TestExtractDeployedWorkflows_UnclosedBlock_WorkflowCapturedWithVersion(t *testing.T) {
	// No </Workflow>, </AvailableWorkflows>, or </Identity> closing tags — simulates truncation.
	data := []byte(
		"<Identity type=\"core\">\n" +
			"<AvailableWorkflows type=\"managed\">\n" +
			"<Workflow type=\"managed\" name=\"quick-fix\" version=\"1.0\">\n" +
			"Content that is never closed.\n")

	wfs := extractDeployedWorkflows(data)

	if len(wfs) != 1 {
		t.Fatalf("expected 1 workflow from unclosed block, got %d: %v", len(wfs), wfs)
	}
	if wfs[0].ID != "quick-fix" {
		t.Errorf("wfs[0].ID = %q, want %q", wfs[0].ID, "quick-fix")
	}
	if wfs[0].Version != "1.0" {
		t.Errorf("wfs[0].Version = %q, want %q; version comment inside unclosed block must still be captured",
			wfs[0].Version, "1.0")
	}
}

// TestExtractDeployedWorkflows_TwoBlocksWithAttributes_BothVersionsIndependent
// verifies that two closed managed-form workflow blocks each carry their own version
// independently from their opening tag attributes. Each block's version comes only from
// its own opening tag.
func TestExtractDeployedWorkflows_TwoBlocksWithAttributes_BothVersionsIndependent(t *testing.T) {
	data := []byte(
		"<Identity type=\"core\">\n" +
			"<AvailableWorkflows type=\"managed\">\n" +
			"<Workflow type=\"managed\" name=\"wf1\" version=\"1.0\">\n" +
			"Content of wf1.\n" +
			"</Workflow>\n" +
			"<Workflow type=\"managed\" name=\"wf2\" version=\"2.0\">\n" +
			"Content of wf2.\n" +
			"</Workflow>\n" +
			"</AvailableWorkflows>\n" +
			"</Identity>\n")

	wfs := extractDeployedWorkflows(data)

	if len(wfs) != 2 {
		t.Fatalf("expected 2 workflows, got %d: %v", len(wfs), wfs)
	}
	if wfs[0].ID != "wf1" {
		t.Errorf("wfs[0].ID = %q, want %q", wfs[0].ID, "wf1")
	}
	if wfs[0].Version != "1.0" {
		t.Errorf("wfs[0].Version = %q, want %q; orphan comment between blocks must not affect wf1's version",
			wfs[0].Version, "1.0")
	}
	if wfs[1].ID != "wf2" {
		t.Errorf("wfs[1].ID = %q, want %q", wfs[1].ID, "wf2")
	}
	if wfs[1].Version != "2.0" {
		t.Errorf("wfs[1].Version = %q, want %q; orphan comment between blocks must not affect wf2's version",
			wfs[1].Version, "2.0")
	}
}

// TestExtractDeployedWorkflows_CoreFormWorkflowBlock_NotRecognisedInDeployedFile verifies
// that a <Workflow type="core"> block in a deployed file yields no discovered workflow.
// Deployed workflow blocks are always managed regions (type="managed"): the deploy transform
// retypes every workflow block on write. Accepting the core form would let fixtures drift back
// to a shape no deployed file can produce and hide the extractor bug this stage fixes.
func TestExtractDeployedWorkflows_CoreFormWorkflowBlock_NotRecognisedInDeployedFile(t *testing.T) {
	// A core-form workflow block is the catalog-side shape, never written to a deployed file.
	data := []byte(
		"<Workflow type=\"core\" name=\"quick-fix\" version=\"1.0\">\n" +
			"Catalog-side content.\n" +
			"</Workflow>\n")

	wfs := extractDeployedWorkflows(data)

	if wfs != nil {
		t.Errorf("extractDeployedWorkflows: got %v, want nil; "+
			"a <Workflow type=\"core\"> block must not be recognised in a deployed file — "+
			"only <Workflow type=\"managed\"> regions (the production deployed shape) are valid",
			wfs)
	}
}

// ---------------------------------------------------------------------------
// extractDeployedWorkflows → WorkflowStaleness (end-to-end drift path)
// ---------------------------------------------------------------------------

// TestDeployedWorkflowDrift_IdenticalSets_NoDriftReported verifies that when the deployed
// orchestrator contains managed-form workflow blocks and the desired workflow set is identical,
// WorkflowStaleness reports no drift. This pins the end-to-end path: if extractDeployedWorkflows
// returns nil (broken extractor), WorkflowStaleness incorrectly treats all selected workflows
// as Added and reports false drift even when the sets are identical.
func TestDeployedWorkflowDrift_IdenticalSets_NoDriftReported(t *testing.T) {
	data := []byte(
		"---\nversion: \"1.0\"\n---\n\n" +
			"<Identity type=\"core\">\n" +
			"<AvailableWorkflows type=\"managed\">\n" +
			"<Workflow type=\"managed\" name=\"quick-fix\" version=\"1.0\">\n" +
			"Content.\n" +
			"</Workflow>\n" +
			"</AvailableWorkflows>\n" +
			"</Identity>\n")

	deployed := extractDeployedWorkflows(data)

	// Desired set is identical to the deployed set.
	selected := []domain.Workflow{
		{ID: "quick-fix", Version: "1.0"},
	}

	drift := plan.WorkflowStaleness(deployed, selected)

	if drift.Stale() {
		t.Errorf("WorkflowStaleness: Stale() = true, want false when deployed and desired sets are identical; "+
			"Added=%v Removed=%v Changed=%v; "+
			"a broken extractDeployedWorkflows (returning nil) causes WorkflowStaleness to treat all "+
			"selected workflows as Added, falsely reporting drift",
			drift.Added, drift.Removed, drift.Changed)
	}
}

// TestDeployedWorkflowDrift_DifferingSets_DriftReportedWithBothSidesPopulated verifies that
// when the deployed orchestrator has one workflow set and the desired set differs, drift is
// correctly reported with both Added and Removed populated. Specifically, a workflow present
// only in the deployed set must appear in Removed — which requires extractDeployedWorkflows
// to return the actual deployed set, not nil.
func TestDeployedWorkflowDrift_DifferingSets_DriftReportedWithBothSidesPopulated(t *testing.T) {
	// Deployed orchestrator carries quick-fix only.
	data := []byte(
		"---\nversion: \"1.0\"\n---\n\n" +
			"<Identity type=\"core\">\n" +
			"<AvailableWorkflows type=\"managed\">\n" +
			"<Workflow type=\"managed\" name=\"quick-fix\" version=\"1.0\">\n" +
			"Content.\n" +
			"</Workflow>\n" +
			"</AvailableWorkflows>\n" +
			"</Identity>\n")

	deployed := extractDeployedWorkflows(data)

	// Desired set has code-review only: quick-fix must be Removed, code-review must be Added.
	selected := []domain.Workflow{
		{ID: "code-review", Version: "2.0"},
	}

	drift := plan.WorkflowStaleness(deployed, selected)

	if !drift.Stale() {
		t.Error("WorkflowStaleness: Stale() = false, want true when deployed and desired sets differ")
	}

	// code-review must be Added (in selected but not in deployed).
	foundAdded := false
	for _, id := range drift.Added {
		if id == "code-review" {
			foundAdded = true
		}
	}
	if !foundAdded {
		t.Errorf("WorkflowDrift.Added = %v; want to contain \"code-review\"", drift.Added)
	}

	// quick-fix must be Removed (in deployed but not in selected).
	// If extractDeployedWorkflows returns nil (broken), Removed is always empty and this fails.
	foundRemoved := false
	for _, id := range drift.Removed {
		if id == "quick-fix" {
			foundRemoved = true
		}
	}
	if !foundRemoved {
		t.Errorf("WorkflowDrift.Removed = %v; want to contain \"quick-fix\"; "+
			"a broken extractDeployedWorkflows (returning nil) leaves the deployed set empty, "+
			"so Removed is never populated even when quick-fix is only in the deployed file",
			drift.Removed)
	}
}

// ---------------------------------------------------------------------------
// probeDeployedState
// ---------------------------------------------------------------------------

// TestProbeDeployedState_AllPathsAbsent_HasEntryPerPathWithPresentFalse verifies that when
// none of the planned paths exist on disk, the result map contains one entry per path, all
// with Present: false.
func TestProbeDeployedState_AllPathsAbsent_HasEntryPerPathWithPresentFalse(t *testing.T) {
	ws := t.TempDir()
	paths := plan.PlannedPaths{
		{Ref: domain.ArtifactRef{Kind: domain.ArtifactAgent, Key: "agent-a"}, TargetPath: "agent-a.md"},
		{Ref: domain.ArtifactRef{Kind: domain.ArtifactAgent, Key: "agent-b"}, TargetPath: "agent-b.md"},
	}

	result := probeDeployedState(ws, paths, "", nil)

	if len(result) != 2 {
		t.Fatalf("expected 2 entries in result (one per planned path), got %d", len(result))
	}
	for _, pp := range paths {
		s, ok := result[pp.TargetPath]
		if !ok {
			t.Errorf("missing entry for path %q", pp.TargetPath)
			continue
		}
		if s.Present {
			t.Errorf("path %q: expected Present: false for absent file", pp.TargetPath)
		}
	}
}

// TestProbeDeployedState_MixedPresenceAbsence_EntryPerPathIncludingAbsent verifies the core
// invariant: the result contains one entry per planned path regardless of whether the file
// exists. Present files have Present: true; absent files have Present: false.
func TestProbeDeployedState_MixedPresenceAbsence_EntryPerPathIncludingAbsent(t *testing.T) {
	ws := t.TempDir()
	presentContent := []byte("---\nversion: \"1.0\"\n---\nPresent agent.\n")
	writeFile(t, ws, "present-agent.md", presentContent)

	paths := plan.PlannedPaths{
		{Ref: domain.ArtifactRef{Kind: domain.ArtifactAgent, Key: "present-agent"}, TargetPath: "present-agent.md"},
		{Ref: domain.ArtifactRef{Kind: domain.ArtifactAgent, Key: "absent-agent"}, TargetPath: "absent-agent.md"},
	}

	result := probeDeployedState(ws, paths, "", nil)

	if len(result) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(result))
	}

	present := result["present-agent.md"]
	if !present.Present {
		t.Error("present-agent.md: expected Present: true")
	}
	if present.ContentHash != manifest.Hash(presentContent) {
		t.Errorf("present-agent.md: ContentHash mismatch")
	}

	absent := result["absent-agent.md"]
	if absent.Present {
		t.Error("absent-agent.md: expected Present: false for missing file")
	}
	if absent.ContentHash != "" {
		t.Errorf("absent-agent.md: expected empty ContentHash, got %q", absent.ContentHash)
	}
}

// TestProbeDeployedState_SeedEntryReused_FileNotReRead verifies that a path present in the
// seed map is returned verbatim without issuing another file read. This guarantees the
// orchestrator probed early in the update flow is not read a second time during the full probe.
func TestProbeDeployedState_SeedEntryReused_FileNotReRead(t *testing.T) {
	ws := t.TempDir()
	// Write a file with version "1.0" on disk.
	writeFile(t, ws, "orchestrator.md", []byte("---\nversion: \"1.0\"\n---\nBody.\n"))

	// Provide a seed that claims a different version ("seeded-value").
	seeded := domain.DeployedArtifactState{
		Present: true,
		Version: "seeded-value",
	}
	seed := map[string]domain.DeployedArtifactState{
		"orchestrator.md": seeded,
	}

	paths := plan.PlannedPaths{
		{Ref: domain.ArtifactRef{Kind: domain.ArtifactAgent, Key: "orchestrator"}, TargetPath: "orchestrator.md"},
	}

	result := probeDeployedState(ws, paths, "", seed)

	s := result["orchestrator.md"]
	// The seeded value must be used as-is; the on-disk version "1.0" must not overwrite it.
	if s.Version != "seeded-value" {
		t.Errorf("seeded entry was re-read from disk; Version = %q, want %q (from seed)",
			s.Version, "seeded-value")
	}
}

// TestProbeDeployedState_EmptyPaths_ReturnsEmptyMap verifies that probing an empty path list
// returns an empty (non-nil) map.
func TestProbeDeployedState_EmptyPaths_ReturnsEmptyMap(t *testing.T) {
	ws := t.TempDir()

	result := probeDeployedState(ws, plan.PlannedPaths{}, "", nil)

	if result == nil {
		t.Error("expected non-nil empty map for empty path list, got nil")
	}
	if len(result) != 0 {
		t.Errorf("expected empty map for empty path list, got %d entries", len(result))
	}
}

// TestProbeDeployedState_ModelKeyForwarded_ModelIDPopulatedInResults verifies that the
// modelKey parameter is forwarded to each per-artifact probe call, so artifacts whose
// frontmatter carries that key have their ModelID populated in the returned state map.
func TestProbeDeployedState_ModelKeyForwarded_ModelIDPopulatedInResults(t *testing.T) {
	ws := t.TempDir()
	contentA := []byte("---\nversion: \"1.0\"\nmodel: \"claude-opus-4-5\"\n---\nAgent A.\n")
	contentB := []byte("---\nversion: \"1.0\"\n---\nAgent B (no model key).\n")
	writeFile(t, ws, "agent-a.md", contentA)
	writeFile(t, ws, "agent-b.md", contentB)

	paths := plan.PlannedPaths{
		{Ref: domain.ArtifactRef{Kind: domain.ArtifactAgent, Key: "agent-a"}, TargetPath: "agent-a.md"},
		{Ref: domain.ArtifactRef{Kind: domain.ArtifactAgent, Key: "agent-b"}, TargetPath: "agent-b.md"},
	}

	result := probeDeployedState(ws, paths, "model", nil)

	stateA := result["agent-a.md"]
	if stateA.ModelID != "claude-opus-4-5" {
		t.Errorf("agent-a.md: ModelID = %q, want %q; modelKey must be forwarded to per-artifact probe",
			stateA.ModelID, "claude-opus-4-5")
	}

	stateB := result["agent-b.md"]
	if stateB.ModelID != "" {
		t.Errorf("agent-b.md: ModelID = %q, want empty; model key absent from frontmatter",
			stateB.ModelID)
	}
}

// TestProbeDeployedState_EmptyModelKey_NoModelIDInResults verifies that passing an empty
// modelKey to probeDeployedState produces results with empty ModelID for all entries, even
// when the files carry a "model" frontmatter field.
func TestProbeDeployedState_EmptyModelKey_NoModelIDInResults(t *testing.T) {
	ws := t.TempDir()
	content := []byte("---\nversion: \"1.0\"\nmodel: \"claude-opus-4-5\"\n---\nAgent body.\n")
	writeFile(t, ws, "agent.md", content)

	paths := plan.PlannedPaths{
		{Ref: domain.ArtifactRef{Kind: domain.ArtifactAgent, Key: "agent"}, TargetPath: "agent.md"},
	}

	result := probeDeployedState(ws, paths, "", nil)

	state := result["agent.md"]
	if state.ModelID != "" {
		t.Errorf("agent.md: ModelID = %q, want empty when modelKey is empty", state.ModelID)
	}
}

// TestProbeDeployedState_SeededEntryWithEmptyModelID_NotReProbed verifies that a seeded entry
// whose ModelID is empty is returned verbatim and is NOT silently re-probed to acquire a
// non-empty ModelID from the on-disk file. This is a real hazard in the Update flow: the
// orchestrator early-probe seeds state before the harness modelKey is threaded in, so that
// seeded entry legitimately carries an empty ModelID. Re-probing it would diverge from the
// seeded state the caller deliberately provided and violate the seed-reuse contract.
func TestProbeDeployedState_SeededEntryWithEmptyModelID_NotReProbed(t *testing.T) {
	ws := t.TempDir()
	// Write a file on disk that carries a model key — re-probing it with modelKey="model"
	// would yield a non-empty ModelID. The seeded entry must prevent this re-probe.
	writeFile(t, ws, "orchestrator.md", []byte("---\nversion: \"1.0\"\nmodel: \"claude-opus-4-5\"\n---\nBody.\n"))

	// Seed an entry with ModelID explicitly empty, as would happen when the orchestrator
	// was probed before the harness modelKey was available (empty modelKey probe).
	seeded := domain.DeployedArtifactState{
		Present: true,
		Version: "1.0",
		ModelID: "", // deliberately empty — must be preserved as-is
	}
	seed := map[string]domain.DeployedArtifactState{
		"orchestrator.md": seeded,
	}

	paths := plan.PlannedPaths{
		{Ref: domain.ArtifactRef{Kind: domain.ArtifactAgent, Key: "orchestrator"}, TargetPath: "orchestrator.md"},
	}

	result := probeDeployedState(ws, paths, "model", seed)

	s := result["orchestrator.md"]
	// The seeded entry must be returned verbatim: ModelID stays empty even though the
	// on-disk file has a model key that would yield "claude-opus-4-5" if re-probed.
	if s.ModelID != "" {
		t.Errorf("seeded entry with empty ModelID was re-probed: ModelID = %q, want empty; "+
			"seeded entries must be returned verbatim regardless of what the on-disk file carries",
			s.ModelID)
	}
	// Confirm the full entry is returned verbatim, not partially re-probed.
	if s.Version != "1.0" {
		t.Errorf("seeded entry Version = %q, want %q; seeded entry was not returned verbatim",
			s.Version, "1.0")
	}
	if !s.Present {
		t.Error("seeded entry Present must be true (as seeded); entry was not returned verbatim")
	}
}

// ---------------------------------------------------------------------------
// discoverExistingWorkflows (new package-level function, sourced from probe)
// ---------------------------------------------------------------------------

// TestDiscoverExistingWorkflows_StateWithWorkflows_ReturnsIDsInOrder verifies that when the
// probe state carries workflow entries, discoverExistingWorkflows returns their IDs in the
// order they were found in the deployed file.
func TestDiscoverExistingWorkflows_StateWithWorkflows_ReturnsIDsInOrder(t *testing.T) {
	state := domain.DeployedArtifactState{
		Present: true,
		Workflows: domain.DeployedWorkflows{
			{ID: "quick-fix", Version: "3.0"},
			{ID: "code-review", Version: "2.1"},
		},
	}

	ids := discoverExistingWorkflows(state)

	if len(ids) != 2 {
		t.Fatalf("expected 2 IDs, got %d: %v", len(ids), ids)
	}
	if ids[0] != "quick-fix" {
		t.Errorf("ids[0] = %q, want %q", ids[0], "quick-fix")
	}
	if ids[1] != "code-review" {
		t.Errorf("ids[1] = %q, want %q", ids[1], "code-review")
	}
}

// TestDiscoverExistingWorkflows_AbsentOrchestrator_ReturnsNil verifies that when the
// orchestrator file is absent (Present: false), nil is returned, not an empty slice.
func TestDiscoverExistingWorkflows_AbsentOrchestrator_ReturnsNil(t *testing.T) {
	state := domain.DeployedArtifactState{Present: false}

	ids := discoverExistingWorkflows(state)

	if ids != nil {
		t.Errorf("expected nil for absent orchestrator state, got %v", ids)
	}
}

// TestDiscoverExistingWorkflows_PresentButNoWorkflows_ReturnsNil verifies that when the
// orchestrator is present but carries no workflow section markers, nil is returned.
func TestDiscoverExistingWorkflows_PresentButNoWorkflows_ReturnsNil(t *testing.T) {
	state := domain.DeployedArtifactState{
		Present:   true,
		Workflows: nil, // no workflow markers found by probe
	}

	ids := discoverExistingWorkflows(state)

	if ids != nil {
		t.Errorf("expected nil when no workflow markers are present, got %v", ids)
	}
}

// ---------------------------------------------------------------------------
// Helpers (local to this test file)
// ---------------------------------------------------------------------------

// writeFile writes content to name inside dir. Fails the test fatally if the write fails.
func writeFile(t *testing.T, dir, name string, content []byte) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), content, 0o644); err != nil {
		t.Fatalf("writeFile %q: %v", name, err)
	}
}
