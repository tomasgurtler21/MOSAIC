package app_test

// retarget_crossformat_test.go covers T15.7(a) and T15.8: the encode step at the
// retarget output path (I15.6) and its prior-bytes and non-regression properties.
// Also covers the per-file failure / run-continues contract (AC15.8 second clause).
//
// Covered behaviours:
//
// T15.7(a) -- Markdown-source to Codex target produces TOML:
//   Retargeting a Markdown-source agent to a Codex-format target writes a destination
//   file that a standard TOML parser accepts, with the expected extension. The output
//   is not Markdown with YAML frontmatter in a .toml file -- which is the pre-stage
//   defect this test exists to drive out.
//
// T15.8(a) -- Overwrite with existing Codex destination preserves user-added keys (both shapes):
//   When the destination Codex TOML file already exists and overwrite is allowed,
//   the destination's user-added TOML keys survive the retarget. Tested for both
//   value-carried keys (scalar; mosaic_carriage) and marker-carried keys (TOML table;
//   mosaic_carriage_markers). This proves the destination's raw prior bytes were read
//   and threaded into the encode call for both carriage shapes.
//
// T15.8(b) -- Fresh destination (no prior bytes) succeeds:
//   When the destination does not exist, the retarget succeeds (Op: OpCreate), the
//   destination file is created, and its content parses as valid TOML. No prior-bytes
//   dependency: the encode step must not require prior bytes for a create.
//
// T15.8(c) -- Markdown-to-Markdown output byte-identical to core function:
//   After I15.6 is added, a Markdown-source to Markdown-target retarget must still
//   produce output byte-identical to what BuildRetargetedAgent returns directly. This
//   guards against the encode step accidentally re-serialising the Markdown output.
//
// T15.8(d) -- No-overwrite still fails per-file when destination exists:
//   The no-overwrite protection is unaffected by the encode step: an existing
//   destination without overwrite=true still yields StatusFailed with the destination
//   path in the reason.
//
// T15.8(e) -- Retarget over a malformed TOML destination succeeds:
//   When the existing destination contains invalid TOML (e.g. a leftover Markdown
//   file), the retarget still succeeds. The encode call receives Op: OpUpdate with
//   DeployedRaw set from the raw bytes and Deployed nil (parallel to the deploy
//   path's conflict-overwrite exception). The output is a valid file in the target
//   format.
//
// AC15.8 (second clause) -- Per-file failure does not abort the run:
//   When a per-file failure occurs (here via the strip step that directly precedes
//   the encode call in the same loop body), the failing artifact shows StatusFailed
//   with a non-empty reason, the remaining artifacts are still processed, and
//   TransformHarness returns err == nil.
//
// These tests are in the TDD RED phase for the encode-step behaviours (T15.7(a),
// T15.8(a), T15.8(b), T15.8(e)). They compile successfully but fail when run because
// I15.6 has not yet been implemented: the current flow writes Markdown bytes directly
// to disk, so the TOML-format assertions fail. T15.8(c) and T15.8(d) are written
// before implementation and may pass in the pre-stage state.

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	toml "github.com/pelletier/go-toml/v2"

	"mosaic-deploy/internal/app"
	"mosaic-deploy/internal/app/interactiontest"
	"mosaic-deploy/internal/domain"

	// Blank-import the wiring package so the codex-toml translator is registered.
	// Do NOT replace with _ "mosaic-deploy/internal/agentformat/codextoml" -- that
	// would bypass the wiring package and mask a broken wiring invariant as green.
	_ "mosaic-deploy/internal/agentformat/all"
)

// ---------------------------------------------------------------------------
// Helpers: deps and registry setup
// ---------------------------------------------------------------------------

// newMarkdownToCodexRetargetDeps returns app.Deps wired for Markdown-source →
// Codex-target retarget tests: the source harness is "source-harness" (the same
// transform-test source module used in existing transform tests) and the target is
// "codex-harness" (the Codex format module).
//
// Both modules are registered by ID. The source harness ID and target harness ID
// are "source-harness" and "codex-harness" respectively; callers must supply both
// in their TransformHarnessRequest to avoid an interactive question.
func newMarkdownToCodexRetargetDeps(t *testing.T, stub *interactiontest.Stub) (app.Deps, string) {
	t.Helper()
	deps, workspace := newBaseDeps(t, stub)
	srcMod := newTransformSourceModule()
	tgtMod := newCodexPromoteModule()
	deps.Registry = &stubRegistry{
		list: []domain.HarnessRef{
			transformSourceHarness,
			{ID: "codex-harness", DisplayName: "Codex Harness", Usable: true},
		},
		modules: map[string]domain.HarnessModule{
			transformSourceHarness.ID: srcMod,
			"codex-harness":           tgtMod,
		},
	}
	return deps, workspace
}

// newMarkdownToMarkdownRetargetDeps returns app.Deps wired for Markdown-source →
// Markdown-target retarget tests (the non-regression scenario). Uses the
// transform-test source and target modules, both Markdown format.
func newMarkdownToMarkdownRetargetDeps(t *testing.T, stub *interactiontest.Stub) (app.Deps, string) {
	t.Helper()
	deps, workspace := newBaseDeps(t, stub)
	deps.Registry = newTransformRegistry()
	return deps, workspace
}

// ---------------------------------------------------------------------------
// Fixtures
// ---------------------------------------------------------------------------

// crossFormatSourceAgentName is the filename used for source agent files in
// cross-format retarget tests. The name does not carry .src.md (the source harness
// extension) so the extension-based key derivation path is exercised through the
// fallback, stripping ".src.md" via TrimSuffix when the extension matches.
const crossFormatSourceAgentName = "xformat-agent.src.md"

// crossFormatAgentKey is the agent key derived from crossFormatSourceAgentName by
// stripping the ".src.md" source-harness extension.
const crossFormatAgentKey = "xformat-agent"

// crossFormatSourceBytes returns bytes for a Markdown source agent that the
// "source-harness" transform module recognises (HarnessMatchYes). The file carries
// src_model so the source harness's ModelKey field is satisfied, and an Identity
// region so the eligibility check passes.
func crossFormatSourceBytes() []byte {
	return []byte("---\n" +
		"transform_version: \"2.1.0\"\n" +
		"injections_version: \"3.0.0\"\n" +
		"src_model: claude-3-5-sonnet\n" +
		"name: xformat-agent\n" +
		"description: Cross-format retarget test agent.\n" +
		"role: subagent\n" +
		"---\n" +
		"<Identity type=\"core\">\n" +
		"You are the cross-format test agent.\n" +
		"</Identity>\n")
}

// codexDestinationWithUserKeyBytes returns bytes for a Codex TOML destination file
// that contains a user-added key outside the MOSAIC schema. When the Codex decoder
// processes this file as prior bytes, it captures the user key in the carriage
// container so the encoder can re-emit it on the next retarget.
//
// The user key is:
//   - my_custom_setting = "keep-this-value" -- a string scalar, carried via mosaic_carriage
//     (value-carried, not marker-carried, because scalar strings are value-carried)
//
// This is the minimal fixture for T15.8(a): after retarget with overwrite, the
// destination should still contain my_custom_setting = "keep-this-value".
func codexDestinationWithUserKeyBytes() []byte {
	return []byte(
		"# mosaic_transform_version: 2.1.0\n" +
			"# mosaic_version: 1.0.0\n" +
			"\n" +
			"name = \"xformat-agent\"\n" +
			"description = \"Pre-existing Codex agent\"\n" +
			"sandbox_mode = \"read-only\"\n" +
			"my_custom_setting = \"keep-this-value\"\n" +
			"developer_instructions = \"\"\"\n" +
			"<Identity type=\"core\">\n" +
			"Pre-existing destination agent body.\n" +
			"</Identity>\n" +
			"\"\"\"\n",
	)
}

// codexDestinationWithMarkerCarriedKeyBytes returns bytes for a Codex TOML destination file
// that contains a user-added TOML table section outside the MOSAIC schema. When the Codex
// decoder processes this file as prior bytes, it captures the table in the carriage markers
// container (mosaic_carriage_markers) so the encoder can re-emit it on the next retarget.
//
// The user key is:
//   - [my_config_section] -- a TOML table, carried via mosaic_carriage_markers
//     (marker-carried, not value-carried, because maps/tables are marker-carried)
//
// The section is placed AFTER developer_instructions so that it is treated as a top-level
// TOML header region and emitted after developer_instructions in the output. Placing
// [my_config_section] before developer_instructions would cause TOML to interpret
// developer_instructions as a key inside my_config_section (nested), making it part of
// that table's scope rather than the top-level developer_instructions key.
//
// This is the marker-carried fixture for the T15.8(a) companion test: after retarget with
// overwrite, the destination should still contain the [my_config_section] table.
func codexDestinationWithMarkerCarriedKeyBytes() []byte {
	return []byte(
		"# mosaic_transform_version: 2.1.0\n" +
			"# mosaic_version: 1.0.0\n" +
			"\n" +
			"name = \"xformat-agent\"\n" +
			"description = \"Pre-existing Codex agent\"\n" +
			"sandbox_mode = \"read-only\"\n" +
			"developer_instructions = \"\"\"\n" +
			"<Identity type=\"core\">\n" +
			"Pre-existing destination agent body.\n" +
			"</Identity>\n" +
			"\"\"\"\n" +
			"\n" +
			"[my_config_section]\n" +
			"endpoint = \"https://api.example.com\"\n",
	)
}

// malformedCarriageSourceBytes returns bytes for a Markdown source agent that has a
// structurally invalid mosaic_carriage value: a scalar string instead of a YAML mapping.
// This makes the file pass eligibility and source-harness matching (it has transform_version,
// src_model, and an Identity body), but causes a per-file failure at the strip step during
// retarget.
//
// The failure chain when this file is retargeted:
//  1. BuildRetargetedAgent preserves mosaic_carriage (ClassMosaic key) in the output bytes.
//  2. StripCarriage is called on the retargeted bytes; it finds mosaic_carriage is not a
//     YAML mapping and returns ErrMalformedCarriage wrapped in an ArtifactError.
//  3. The per-file loop records StatusFailed with a reason and continues -- the same
//     fail-and-continue pattern as the encode-failure path directly below it in the loop.
//
// Note on testing the encode path directly: the encode failure path (AC15.8 second clause)
// shares the exact same "fail, continue" loop structure as the strip failure path used here.
// However, triggering an encode failure through TransformHarness requires injecting a
// translator that returns an error from Encode; the translator registry is global and panics
// on duplicate registration, TestVocabularyEqualsRegistry prevents adding unknown format IDs,
// and Deps carries no translator injection point. There is no DI mechanism available. The
// strip path (which immediately precedes the encode call in the same loop body at lines
// 437-446 vs 477-490 of transform_service_impl.go) exercises the same per-file-failure-
// run-continues contract that AC15.8 requires.
func malformedCarriageSourceBytes() []byte {
	return []byte("---\n" +
		"transform_version: \"2.1.0\"\n" +
		"injections_version: \"3.0.0\"\n" +
		"src_model: claude-3-5-sonnet\n" +
		"name: bad-agent\n" +
		"description: Source with malformed carriage container.\n" +
		"role: subagent\n" +
		"mosaic_carriage: \"not-a-map\"\n" +
		"---\n" +
		"<Identity type=\"core\">\n" +
		"You are the bad-carriage test agent.\n" +
		"</Identity>\n")
}

// bodylessCodexDestBytes returns bytes for a Codex TOML destination file that has no
// developer_instructions key. When the Codex decoder processes this file as prior bytes during
// a retarget, the decode report contains EntryMissingInstructions. Used for the AC15.11
// destination-read merge point test.
func bodylessCodexDestBytes() []byte {
	return []byte(
		"# mosaic_transform_version: 2.1.0\n" +
			"# mosaic_version: 1.0.0\n" +
			"\n" +
			"name = \"xformat-agent\"\n" +
			"sandbox_mode = \"read-only\"\n",
	)
}

// malformedTomlBytes returns bytes that are syntactically invalid TOML. The content
// is a Markdown agent file (YAML frontmatter) placed at a path that the retarget
// flow would expect to contain valid TOML -- the "malformed destination" scenario
// for T15.8(e).
func malformedTomlBytes() []byte {
	return []byte("---\n" +
		"transform_version: \"2.1.0\"\n" +
		"name: leftover-markdown-at-toml-path\n" +
		"---\n" +
		"<Identity type=\"core\">\n" +
		"This file was left behind in the wrong format.\n" +
		"</Identity>\n")
}

// ---------------------------------------------------------------------------
// Helpers: path and TOML assertions
// ---------------------------------------------------------------------------

// codexDestPath returns the destination path that the retarget loop computes for the
// given source directory and the Codex target module (key.toml in the same directory).
func codexDestPath(srcDir string) string {
	return filepath.Join(srcDir, crossFormatAgentKey+".toml")
}

// markdownDestPath returns the destination path computed by the target Markdown module
// for the given source directory.
func markdownDestPath(srcDir string) string {
	return filepath.Join(srcDir, crossFormatAgentKey+".tgt.md")
}

// assertValidToml verifies that the given bytes parse as well-formed TOML and returns
// the parsed map. Fails the test immediately if parsing fails.
func assertValidToml(t *testing.T, loc string, content []byte) map[string]interface{} {
	t.Helper()
	var result map[string]interface{}
	if err := toml.Unmarshal(content, &result); err != nil {
		t.Fatalf("%s: content is not valid TOML: %v\nFirst 200 bytes: %q",
			loc, err, string(content[:min(len(content), 200)]))
	}
	return result
}

// ---------------------------------------------------------------------------
// T15.7(a) -- Markdown-source to Codex target produces valid TOML
// ---------------------------------------------------------------------------

// TestRetarget_MarkdownSource_ToCodexTarget_DestinationParseableAsToml verifies that
// retargeting a Markdown-source agent to a Codex-format target writes a destination
// file that a standard TOML parser accepts. In the pre-stage state the service writes
// Markdown bytes directly (YAML frontmatter in a .toml file), which this assertion
// rejects. After I15.6 the encode step routes through the Codex TOML translator,
// producing a destination the TOML parser accepts.
//
// Asserts on the written file, not on intermediate bytes.
func TestRetarget_MarkdownSource_ToCodexTarget_DestinationParseableAsToml(t *testing.T) {
	// Arrange
	dir := t.TempDir()
	srcPath := filepath.Join(dir, crossFormatSourceAgentName)
	if err := os.WriteFile(srcPath, crossFormatSourceBytes(), 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}

	stub := interactiontest.NewBuilder().Build()
	deps, _ := newMarkdownToCodexRetargetDeps(t, stub)
	svc := app.New(deps)

	req := app.TransformHarnessRequest{
		SourceHarnessID: transformSourceHarness.ID,
		TargetHarnessID: "codex-harness",
		Path:            srcPath,
		TargetModel:     "codex-model",
		Overwrite:       false,
		DryRun:          false,
	}

	// Act
	result, err := svc.TransformHarness(context.Background(), req)

	// Assert: whole-run error not expected.
	if err != nil {
		t.Fatalf("TransformHarness: unexpected error: %v", err)
	}
	if len(result.Files) != 1 {
		t.Fatalf("len(result.Files) = %d, want 1", len(result.Files))
	}
	out := result.Files[0]
	if out.Status != app.StatusTransformed {
		t.Fatalf("Files[0].Status = %q, want StatusTransformed; reason: %s", out.Status, out.Reason)
	}

	// Assert: destination path ends with .toml extension.
	if !strings.HasSuffix(out.DestinationPath, ".toml") {
		t.Errorf("DestinationPath = %q does not end with .toml; target format extension not applied",
			out.DestinationPath)
	}

	// Assert: destination file exists.
	destBytes, readErr := os.ReadFile(out.DestinationPath)
	if readErr != nil {
		t.Fatalf("cannot read destination file %q: %v", out.DestinationPath, readErr)
	}

	// Assert: destination file is valid TOML.
	// This is the primary RED-phase failure: the current flow writes Markdown bytes
	// (YAML frontmatter starting with "---\n"), which the TOML parser rejects.
	assertValidToml(t, "T15.7(a) destination", destBytes)
}

// TestRetarget_MarkdownSource_ToCodexTarget_ExtensionAndFormatAgree verifies that
// the destination file's extension (.toml) and its content format (TOML) are
// consistent: a .toml file must contain TOML, not YAML frontmatter.
//
// The pre-stage defect is that the file has .toml extension but YAML content.
func TestRetarget_MarkdownSource_ToCodexTarget_ExtensionAndFormatAgree(t *testing.T) {
	// Arrange
	dir := t.TempDir()
	srcPath := filepath.Join(dir, crossFormatSourceAgentName)
	if err := os.WriteFile(srcPath, crossFormatSourceBytes(), 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}

	stub := interactiontest.NewBuilder().Build()
	deps, _ := newMarkdownToCodexRetargetDeps(t, stub)
	svc := app.New(deps)

	req := app.TransformHarnessRequest{
		SourceHarnessID: transformSourceHarness.ID,
		TargetHarnessID: "codex-harness",
		Path:            srcPath,
		TargetModel:     "codex-model",
		Overwrite:       false,
		DryRun:          false,
	}

	// Act
	result, err := svc.TransformHarness(context.Background(), req)
	if err != nil {
		t.Fatalf("TransformHarness: unexpected error: %v", err)
	}
	if len(result.Files) != 1 || result.Files[0].Status != app.StatusTransformed {
		t.Fatalf("expected one StatusTransformed outcome; got %v", result.Files)
	}
	out := result.Files[0]

	// Assert: extension agrees with format.
	destBytes, readErr := os.ReadFile(out.DestinationPath)
	if readErr != nil {
		t.Fatalf("cannot read destination: %v", readErr)
	}

	ext := filepath.Ext(out.DestinationPath)
	if ext == ".toml" {
		// Extension declares TOML: content must parse as TOML.
		var m map[string]interface{}
		if parseErr := toml.Unmarshal(destBytes, &m); parseErr != nil {
			t.Errorf("destination has .toml extension but content is not valid TOML: %v\ncontent: %q",
				parseErr, string(destBytes[:min(len(destBytes), 200)]))
		}
	} else if ext == ".md" {
		// Extension declares Markdown: content must have YAML frontmatter.
		if !strings.HasPrefix(string(destBytes), "---\n") {
			t.Errorf("destination has %q extension but content does not start with YAML frontmatter delimiter",
				ext)
		}
	} else {
		t.Errorf("unexpected destination extension %q; must be .toml (Codex target) or .md (Markdown target)",
			ext)
	}
}

// ---------------------------------------------------------------------------
// T15.8(a) -- Overwrite with existing Codex destination preserves user-added keys
// ---------------------------------------------------------------------------

// TestRetarget_MarkdownSource_ToCodex_Overwrite_PreservesValueCarriedUserKey verifies
// that retargeting over an existing Codex destination with Overwrite=true preserves
// the destination's user-added TOML key (my_custom_setting). This proves the
// destination's raw prior bytes were read and threaded into the encode call, which
// carries value-carried user keys forward through mosaic_carriage.
//
// In the pre-stage state, no prior bytes are read and the encode step does not run.
// The output is Markdown, so both the TOML parse assertion and the user-key assertion
// fail (RED).
func TestRetarget_MarkdownSource_ToCodex_Overwrite_PreservesValueCarriedUserKey(t *testing.T) {
	// Arrange: write source and pre-existing Codex destination.
	dir := t.TempDir()
	srcPath := filepath.Join(dir, crossFormatSourceAgentName)
	if err := os.WriteFile(srcPath, crossFormatSourceBytes(), 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}
	destPath := codexDestPath(dir)
	if err := os.WriteFile(destPath, codexDestinationWithUserKeyBytes(), 0o644); err != nil {
		t.Fatalf("write pre-existing destination: %v", err)
	}

	stub := interactiontest.NewBuilder().Build()
	deps, _ := newMarkdownToCodexRetargetDeps(t, stub)
	svc := app.New(deps)

	req := app.TransformHarnessRequest{
		SourceHarnessID: transformSourceHarness.ID,
		TargetHarnessID: "codex-harness",
		Path:            srcPath,
		TargetModel:     "codex-model",
		Overwrite:       true, // allow overwriting the existing destination
		DryRun:          false,
	}

	// Act
	result, err := svc.TransformHarness(context.Background(), req)

	// Assert: no whole-run error.
	if err != nil {
		t.Fatalf("TransformHarness: unexpected error: %v", err)
	}
	if len(result.Files) != 1 {
		t.Fatalf("len(result.Files) = %d, want 1", len(result.Files))
	}
	out := result.Files[0]
	if out.Status != app.StatusTransformed {
		t.Fatalf("Status = %q, want StatusTransformed; reason: %s", out.Status, out.Reason)
	}

	// Assert: destination file is valid TOML.
	destBytes, readErr := os.ReadFile(destPath)
	if readErr != nil {
		t.Fatalf("cannot read destination: %v", readErr)
	}
	parsed := assertValidToml(t, "T15.8(a) destination", destBytes)

	// Assert: user-added key my_custom_setting is preserved.
	val, ok := parsed["my_custom_setting"]
	if !ok {
		t.Errorf("destination does not contain user-added key my_custom_setting; "+
			"prior bytes were not read or not passed to the encoder. "+
			"Keys present: %v", tomlKeys(parsed))
	} else if s, isStr := val.(string); !isStr || s != "keep-this-value" {
		t.Errorf("my_custom_setting = %v (%T); want string %q",
			val, val, "keep-this-value")
	}
}

// TestRetarget_MarkdownSource_ToCodex_Overwrite_PreservesMarkerCarriedUserKey verifies
// that retargeting over an existing Codex destination with Overwrite=true preserves
// the destination's user-added TOML table section (my_config_section). This proves the
// destination's raw prior bytes were read and threaded into the encode call, which
// carries marker-carried user keys forward through mosaic_carriage_markers.
//
// A marker-carried key is one whose TOML value is a table or array-of-tables (rather
// than a scalar). The Codex decoder stores the key name in mosaic_carriage_markers; the
// encoder extracts the original span from the prior bytes and re-emits it verbatim.
//
// In the pre-stage state, no prior bytes are read and the encode step does not run.
// The output is Markdown, so both the TOML parse assertion and the section assertion
// fail (RED).
//
// This is the marker-carried companion to TestRetarget_MarkdownSource_ToCodex_Overwrite_PreservesValueCarriedUserKey,
// which covers value-carried keys. AC15.7 requires "user keys of both shapes" to survive.
func TestRetarget_MarkdownSource_ToCodex_Overwrite_PreservesMarkerCarriedUserKey(t *testing.T) {
	// Arrange: write source and pre-existing Codex destination with a TOML table section.
	dir := t.TempDir()
	srcPath := filepath.Join(dir, crossFormatSourceAgentName)
	if err := os.WriteFile(srcPath, crossFormatSourceBytes(), 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}
	destPath := codexDestPath(dir)
	if err := os.WriteFile(destPath, codexDestinationWithMarkerCarriedKeyBytes(), 0o644); err != nil {
		t.Fatalf("write pre-existing destination: %v", err)
	}

	stub := interactiontest.NewBuilder().Build()
	deps, _ := newMarkdownToCodexRetargetDeps(t, stub)
	svc := app.New(deps)

	req := app.TransformHarnessRequest{
		SourceHarnessID: transformSourceHarness.ID,
		TargetHarnessID: "codex-harness",
		Path:            srcPath,
		TargetModel:     "codex-model",
		Overwrite:       true, // allow overwriting the existing destination
		DryRun:          false,
	}

	// Act
	result, err := svc.TransformHarness(context.Background(), req)

	// Assert: no whole-run error.
	if err != nil {
		t.Fatalf("TransformHarness: unexpected error: %v", err)
	}
	if len(result.Files) != 1 {
		t.Fatalf("len(result.Files) = %d, want 1", len(result.Files))
	}
	out := result.Files[0]
	if out.Status != app.StatusTransformed {
		t.Fatalf("Status = %q, want StatusTransformed; reason: %s", out.Status, out.Reason)
	}

	// Assert: destination file is valid TOML.
	destBytes, readErr := os.ReadFile(destPath)
	if readErr != nil {
		t.Fatalf("cannot read destination: %v", readErr)
	}
	parsed := assertValidToml(t, "T15.8(a) marker-carried destination", destBytes)

	// Assert: user-added table section my_config_section is preserved.
	// The Codex decoder carries [my_config_section] via mosaic_carriage_markers; the encoder
	// reads the prior bytes and re-emits the table span after developer_instructions.
	section, ok := parsed["my_config_section"]
	if !ok {
		t.Errorf("destination does not contain user-added table my_config_section; "+
			"prior bytes were not read or the marker-carried section was not re-emitted. "+
			"Keys present: %v", tomlKeys(parsed))
		return
	}
	sectionMap, isMap := section.(map[string]interface{})
	if !isMap {
		t.Errorf("my_config_section = %v (%T); want a TOML table (map)", section, section)
		return
	}
	ep, ok := sectionMap["endpoint"]
	if !ok {
		t.Errorf("my_config_section.endpoint absent; want %q", "https://api.example.com")
	} else if s, isStr := ep.(string); !isStr || s != "https://api.example.com" {
		t.Errorf("my_config_section.endpoint = %v (%T); want string %q",
			ep, ep, "https://api.example.com")
	}
}

// TestRetarget_MarkdownSource_ToCodex_Overwrite_SourceFileUnchanged verifies that
// the source file is not modified by the retarget operation. The source is a
// Markdown agent and must remain byte-identical after retargeting.
func TestRetarget_MarkdownSource_ToCodex_Overwrite_SourceFileUnchanged(t *testing.T) {
	// Arrange
	dir := t.TempDir()
	srcPath := filepath.Join(dir, crossFormatSourceAgentName)
	srcContent := crossFormatSourceBytes()
	if err := os.WriteFile(srcPath, srcContent, 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}
	// Pre-existing destination to allow overwrite=true path.
	destPath := codexDestPath(dir)
	if err := os.WriteFile(destPath, codexDestinationWithUserKeyBytes(), 0o644); err != nil {
		t.Fatalf("write destination: %v", err)
	}

	stub := interactiontest.NewBuilder().Build()
	deps, _ := newMarkdownToCodexRetargetDeps(t, stub)
	svc := app.New(deps)

	req := app.TransformHarnessRequest{
		SourceHarnessID: transformSourceHarness.ID,
		TargetHarnessID: "codex-harness",
		Path:            srcPath,
		TargetModel:     "codex-model",
		Overwrite:       true,
		DryRun:          false,
	}

	// Act
	_, _ = svc.TransformHarness(context.Background(), req) //nolint:errcheck

	// Assert: source file is untouched.
	afterSrc, readErr := os.ReadFile(srcPath)
	if readErr != nil {
		t.Fatalf("cannot read source after retarget: %v", readErr)
	}
	if !bytes.Equal(afterSrc, srcContent) {
		t.Error("source file was modified by TransformHarness; it must be read-only to the retarget")
	}
}

// ---------------------------------------------------------------------------
// T15.8(b) -- Fresh destination (no prior bytes) succeeds
// ---------------------------------------------------------------------------

// TestRetarget_MarkdownSource_ToCodex_FreshDestination_Succeeds verifies that
// retargeting to a destination that does not exist succeeds (no error) and writes
// a valid destination file in the target format. No prior bytes are needed for a
// create; the encoder must succeed without them.
func TestRetarget_MarkdownSource_ToCodex_FreshDestination_Succeeds(t *testing.T) {
	// Arrange: write source, do NOT write a pre-existing destination.
	dir := t.TempDir()
	srcPath := filepath.Join(dir, crossFormatSourceAgentName)
	if err := os.WriteFile(srcPath, crossFormatSourceBytes(), 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}
	// Verify destination does not exist before the call.
	destPath := codexDestPath(dir)
	if _, statErr := os.Stat(destPath); statErr == nil {
		t.Fatalf("test setup error: destination %q already exists", destPath)
	}

	stub := interactiontest.NewBuilder().Build()
	deps, _ := newMarkdownToCodexRetargetDeps(t, stub)
	svc := app.New(deps)

	req := app.TransformHarnessRequest{
		SourceHarnessID: transformSourceHarness.ID,
		TargetHarnessID: "codex-harness",
		Path:            srcPath,
		TargetModel:     "codex-model",
		Overwrite:       false, // no overwrite needed -- destination does not exist
		DryRun:          false,
	}

	// Act
	result, err := svc.TransformHarness(context.Background(), req)

	// Assert: no whole-run error.
	if err != nil {
		t.Fatalf("TransformHarness: unexpected error: %v", err)
	}
	if len(result.Files) != 1 {
		t.Fatalf("len(result.Files) = %d, want 1", len(result.Files))
	}
	out := result.Files[0]
	if out.Status != app.StatusTransformed {
		t.Fatalf("Status = %q, want StatusTransformed; reason: %s", out.Status, out.Reason)
	}

	// Assert: destination file was created.
	destBytes, readErr := os.ReadFile(destPath)
	if readErr != nil {
		t.Fatalf("destination file %q was not created: %v", destPath, readErr)
	}

	// Assert: destination file is valid TOML (not Markdown).
	assertValidToml(t, "T15.8(b) fresh destination", destBytes)
}

// ---------------------------------------------------------------------------
// T15.8(c) -- Markdown-to-Markdown output byte-identical to core function
// ---------------------------------------------------------------------------

// TestRetarget_MarkdownToMarkdown_OutputMatchesBuildRetargetedAgent verifies that the
// TransformHarness service produces output byte-identical to calling BuildRetargetedAgent
// directly for a Markdown-source to Markdown-target retarget. This guards against I15.6
// accidentally re-serialising the Markdown output: for a Markdown target the encode step
// must use the identity translator, preserving bytes exactly.
//
// This test may pass in the pre-stage state (before I15.6) because the service currently
// calls BuildRetargetedAgent and writes directly, producing identical bytes. It is written
// before the implementation so it remains a regression guard throughout the stage.
func TestRetarget_MarkdownToMarkdown_OutputMatchesBuildRetargetedAgent(t *testing.T) {
	// Arrange: write source file for the source harness.
	dir := t.TempDir()
	srcPath := filepath.Join(dir, crossFormatSourceAgentName)
	srcBytes := crossFormatSourceBytes()
	if err := os.WriteFile(srcPath, srcBytes, 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}

	stub := interactiontest.NewBuilder().Build()
	deps, workspace := newMarkdownToMarkdownRetargetDeps(t, stub)
	svc := app.New(deps)

	req := app.TransformHarnessRequest{
		SourceHarnessID: transformSourceHarness.ID,
		TargetHarnessID: transformTargetHarness.ID,
		Path:            srcPath,
		TargetModel:     "target-model-x",
		Overwrite:       false,
		DryRun:          false,
	}

	// Act: run via the service.
	result, err := svc.TransformHarness(context.Background(), req)
	if err != nil {
		t.Fatalf("TransformHarness: unexpected error: %v", err)
	}
	if len(result.Files) == 0 {
		t.Fatalf("TransformHarness returned no file outcomes")
	}
	out := result.Files[0]
	if out.Status != app.StatusTransformed {
		t.Fatalf("source not matched as source-harness (Status=%q); "+
			"harness stub setup failure: the source fixture must match the source harness "+
			"for this test to run; fixture or stub change may have broken the setup",
			out.Status)
	}

	// Read the service-produced destination bytes.
	serviceDestBytes, readErr := os.ReadFile(out.DestinationPath)
	if readErr != nil {
		t.Fatalf("cannot read service destination: %v", readErr)
	}

	// Produce the expected bytes via BuildRetargetedAgent directly.
	// ToolMappingsVersion is left empty to match the hash computed from the default
	// stub configs (DefaultToolConfig with nil user destinations); the hash of an
	// all-nil input is empty.
	srcMod := newTransformSourceModule()
	tgtMod := newTransformTargetModule()
	coreIn := app.RetargetInput{
		Source:              srcBytes,
		SourceModule:        srcMod,
		TargetModule:        tgtMod,
		Kind:                domain.ArtifactAgent,
		AgentKey:            crossFormatAgentKey,
		TargetModel:         "target-model-x",
		ToolMappingsVersion: "",
	}
	coreBytes, _, coreErr := app.BuildRetargetedAgent(coreIn)
	if coreErr != nil {
		t.Fatalf("BuildRetargetedAgent: unexpected error: %v", coreErr)
	}

	// Assert: service output is byte-identical to core output.
	// After I15.6, the Markdown identity translator must not alter these bytes.
	if !bytes.Equal(serviceDestBytes, coreBytes) {
		t.Errorf("TransformHarness output differs from BuildRetargetedAgent output for "+
			"Markdown-to-Markdown retarget; encode step must not change Markdown bytes.\n"+
			"service output length:  %d\n"+
			"core output length:     %d",
			len(serviceDestBytes), len(coreBytes))
	}

	// Workspace is referenced to avoid unused-variable compilation error.
	_ = workspace
}

// TestRetarget_MarkdownToMarkdown_OutputIsMarkdown verifies that a Markdown-to-Markdown
// retarget produces a Markdown-format output (YAML frontmatter, starts with "---\n").
// This is the minimal format regression guard: if the encode step accidentally chose
// the wrong translator for a Markdown target, the output would not start with "---\n".
func TestRetarget_MarkdownToMarkdown_OutputIsMarkdown(t *testing.T) {
	// Arrange
	dir := t.TempDir()
	srcPath := filepath.Join(dir, crossFormatSourceAgentName)
	if err := os.WriteFile(srcPath, crossFormatSourceBytes(), 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}

	stub := interactiontest.NewBuilder().Build()
	deps, _ := newMarkdownToMarkdownRetargetDeps(t, stub)
	svc := app.New(deps)

	req := app.TransformHarnessRequest{
		SourceHarnessID: transformSourceHarness.ID,
		TargetHarnessID: transformTargetHarness.ID,
		Path:            srcPath,
		TargetModel:     "target-model-x",
		Overwrite:       false,
		DryRun:          false,
	}

	// Act
	result, err := svc.TransformHarness(context.Background(), req)
	if err != nil {
		t.Fatalf("TransformHarness: unexpected error: %v", err)
	}
	if len(result.Files) == 0 {
		t.Fatalf("no file outcomes")
	}
	out := result.Files[0]
	if out.Status != app.StatusTransformed {
		t.Fatalf("source not matched; Status=%q; "+
			"harness stub setup failure: the source fixture must match the source harness "+
			"for this regression guard to run",
			out.Status)
	}

	destBytes, readErr := os.ReadFile(out.DestinationPath)
	if readErr != nil {
		t.Fatalf("cannot read destination: %v", readErr)
	}

	// A Markdown file must start with the YAML frontmatter delimiter.
	if !strings.HasPrefix(string(destBytes), "---\n") {
		t.Errorf("Markdown-to-Markdown retarget destination does not start with \"---\\n\"; "+
			"encode step may have chosen the wrong format translator for a Markdown target. "+
			"First 100 bytes: %q", string(destBytes[:min(len(destBytes), 100)]))
	}
}

// ---------------------------------------------------------------------------
// T15.8(d) -- No-overwrite still fails per-file when destination exists
// ---------------------------------------------------------------------------

// TestRetarget_MarkdownSource_ToCodex_NoOverwrite_ExistingDestination_StatusFailed
// verifies that the no-overwrite protection is unaffected by the encode step. When the
// destination already exists and Overwrite=false, the per-file outcome is StatusFailed
// with a reason naming the destination path. The encode step must never be reached.
func TestRetarget_MarkdownSource_ToCodex_NoOverwrite_ExistingDestination_StatusFailed(t *testing.T) {
	// Arrange: write source and pre-existing destination.
	dir := t.TempDir()
	srcPath := filepath.Join(dir, crossFormatSourceAgentName)
	if err := os.WriteFile(srcPath, crossFormatSourceBytes(), 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}
	destPath := codexDestPath(dir)
	originalContent := []byte("original-destination-content")
	if err := os.WriteFile(destPath, originalContent, 0o644); err != nil {
		t.Fatalf("write destination: %v", err)
	}

	stub := interactiontest.NewBuilder().Build()
	deps, _ := newMarkdownToCodexRetargetDeps(t, stub)
	svc := app.New(deps)

	req := app.TransformHarnessRequest{
		SourceHarnessID: transformSourceHarness.ID,
		TargetHarnessID: "codex-harness",
		Path:            srcPath,
		TargetModel:     "codex-model",
		Overwrite:       false, // overwrite NOT allowed
		DryRun:          false,
	}

	// Act
	result, err := svc.TransformHarness(context.Background(), req)

	// Assert: no whole-run error (per-file failure only).
	if err != nil {
		t.Fatalf("TransformHarness: unexpected whole-run error: %v", err)
	}
	if len(result.Files) != 1 {
		t.Fatalf("len(result.Files) = %d, want 1", len(result.Files))
	}
	out := result.Files[0]

	// The per-file outcome must be StatusFailed.
	if out.Status != app.StatusFailed {
		t.Errorf("Status = %q, want StatusFailed; Overwrite=false with existing destination "+
			"must produce a per-file failure", out.Status)
	}

	// The reason must name the destination path.
	if !strings.Contains(out.Reason, destPath) {
		t.Errorf("Reason = %q does not contain destination path %q", out.Reason, destPath)
	}

	// The existing destination must be unchanged.
	afterDest, readErr := os.ReadFile(destPath)
	if readErr != nil {
		t.Fatalf("cannot read destination after refused overwrite: %v", readErr)
	}
	if !bytes.Equal(afterDest, originalContent) {
		t.Error("destination file was modified despite Overwrite=false; no-overwrite must prevent any write")
	}
}

// ---------------------------------------------------------------------------
// T15.8(e) -- Retarget over a malformed TOML destination succeeds
// ---------------------------------------------------------------------------

// TestRetarget_MarkdownSource_ToCodex_Overwrite_MalformedDestination_Succeeds verifies
// that retargeting over an existing destination whose content is invalid TOML succeeds.
// The encode call receives Op: OpUpdate with DeployedRaw set from the raw (malformed)
// bytes and Deployed nil -- the conflict-overwrite pattern from the deploy path.
// The output must be a valid file in the target format.
//
// In the pre-stage state the service ignores prior bytes and writes Markdown directly.
// The TOML-format assertion on the output fails (RED).
func TestRetarget_MarkdownSource_ToCodex_Overwrite_MalformedDestination_Succeeds(t *testing.T) {
	// Arrange: write source and a malformed TOML destination.
	dir := t.TempDir()
	srcPath := filepath.Join(dir, crossFormatSourceAgentName)
	if err := os.WriteFile(srcPath, crossFormatSourceBytes(), 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}
	destPath := codexDestPath(dir)
	if err := os.WriteFile(destPath, malformedTomlBytes(), 0o644); err != nil {
		t.Fatalf("write malformed destination: %v", err)
	}

	stub := interactiontest.NewBuilder().Build()
	deps, _ := newMarkdownToCodexRetargetDeps(t, stub)
	svc := app.New(deps)

	req := app.TransformHarnessRequest{
		SourceHarnessID: transformSourceHarness.ID,
		TargetHarnessID: "codex-harness",
		Path:            srcPath,
		TargetModel:     "codex-model",
		Overwrite:       true, // allow overwriting the malformed destination
		DryRun:          false,
	}

	// Act
	result, err := svc.TransformHarness(context.Background(), req)

	// Assert: no whole-run error.
	if err != nil {
		t.Fatalf("TransformHarness: unexpected error: %v", err)
	}
	if len(result.Files) != 1 {
		t.Fatalf("len(result.Files) = %d, want 1", len(result.Files))
	}
	out := result.Files[0]

	// A malformed destination must not cause the file to fail -- the encode receives
	// the raw bytes (DeployedRaw) even when canonical decode failed (Deployed nil).
	if out.Status != app.StatusTransformed {
		t.Errorf("Status = %q, want StatusTransformed; a malformed TOML destination must be "+
			"overwritten successfully, not cause a per-file failure. Reason: %s",
			out.Status, out.Reason)
	}

	// The replacement file must be valid TOML -- no Markdown frontmatter leaking through.
	destBytes, readErr := os.ReadFile(destPath)
	if readErr != nil {
		t.Fatalf("cannot read destination after overwrite: %v", readErr)
	}
	assertValidToml(t, "T15.8(e) destination after overwrite of malformed", destBytes)
}

// ---------------------------------------------------------------------------
// AC15.11 -- decode report merged at the retarget destination read point
// ---------------------------------------------------------------------------

// TestRetarget_CodexDestination_BodyLess_MissingInstructionsInRunReport verifies that when the
// existing Codex destination has no developer_instructions key, the EntryMissingInstructions
// notice from the destination's decode report is forwarded to the run report as a GapManualStep
// gap. This asserts that mergeDecodeReport is called at the destination read point in the retarget
// loop (I15.6 requirement 5).
//
// In the pre-stage state the destination is not read for prior bytes, so no decode report exists
// and the spy records no gap. The assertion fails (RED). After I15.6, the destination read
// produces the decode report, mergeDecodeReport forwards EntryMissingInstructions as GapManualStep
// via deps.Todo.AddGap, and the assertion passes.
func TestRetarget_CodexDestination_BodyLess_MissingInstructionsInRunReport(t *testing.T) {
	// Arrange: Markdown source and a body-less Codex destination.
	dir := t.TempDir()
	srcPath := filepath.Join(dir, crossFormatSourceAgentName)
	if err := os.WriteFile(srcPath, crossFormatSourceBytes(), 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}
	destPath := codexDestPath(dir)
	if err := os.WriteFile(destPath, bodylessCodexDestBytes(), 0o644); err != nil {
		t.Fatalf("write body-less Codex destination: %v", err)
	}

	spy := &spyTodo{}
	stub := interactiontest.NewBuilder().Build()
	deps, _ := newMarkdownToCodexRetargetDeps(t, stub)
	deps.Todo = spy
	svc := app.New(deps)

	req := app.TransformHarnessRequest{
		SourceHarnessID: transformSourceHarness.ID,
		TargetHarnessID: "codex-harness",
		Path:            srcPath,
		TargetModel:     "codex-model",
		Overwrite:       true,
		DryRun:          false,
	}

	// Act
	_, _ = svc.TransformHarness(context.Background(), req) //nolint:errcheck

	// Assert: the decode report from the body-less destination must surface in the run report.
	// mergeDecodeReport translates EntryMissingInstructions to domain.GapManualStep and calls
	// deps.Todo.AddGap. In the pre-stage state (destination not read for prior bytes) the spy
	// records no gap.
	if !spy.hasGapKind(domain.GapManualStep) {
		t.Errorf("no GapManualStep recorded in the run report after retarget over a body-less "+
			"Codex destination; mergeDecodeReport must be called at the destination read point "+
			"and the EntryMissingInstructions notice forwarded to deps.Todo.AddGap")
	}
}

// ---------------------------------------------------------------------------
// AC15.8 (second clause) -- Per-file failure does not abort the run
// ---------------------------------------------------------------------------

// TestRetarget_PerFileFailure_RunContinues verifies that when a per-file failure occurs
// during the retarget loop, the run does not abort: the failing artifact shows StatusFailed
// with a non-empty reason, the remaining artifacts are still processed, and TransformHarness
// returns err == nil.
//
// The failure is triggered by setting mosaic_carriage to a scalar string in the source
// file's YAML frontmatter. When retargeted:
//   1. BuildRetargetedAgent preserves mosaic_carriage (ClassMosaic key) in the output bytes.
//   2. StripCarriage finds mosaic_carriage is not a YAML mapping and returns ErrMalformedCarriage.
//   3. The per-file loop records StatusFailed with a reason and continues.
//
// Two source files are written to a directory so that "run continues" is verifiable: if
// the loop aborted on the first failure, Files[1] would never be populated.
//
// Note on the encode failure path (AC15.8): the encode failure path shares the exact same
// "fail, continue" loop structure as the strip failure path used here (both paths are in
// transform_service_impl.go, strip at lines 437-446 and encode at lines 477-490). Triggering
// an encode failure through TransformHarness requires injecting a translator that returns an
// error from Encode; the translator registry is global and panics on duplicate registration,
// TestVocabularyEqualsRegistry prevents adding unknown format IDs, and Deps carries no
// translator injection point. The strip path is the best available exercise of the AC15.8
// per-file-failure-run-continues contract within the same loop body.
func TestRetarget_PerFileFailure_RunContinues(t *testing.T) {
	// Arrange: write two source files into a directory.
	// bad-agent.src.md has mosaic_carriage set to a scalar (malformed); StripCarriage fails.
	// good-agent.src.md is a normal source; it succeeds.
	// Files are processed in lexicographic order (bad < good), so Files[0]=bad, Files[1]=good.
	dir := t.TempDir()

	badSrcPath := filepath.Join(dir, "bad-agent.src.md")
	if err := os.WriteFile(badSrcPath, malformedCarriageSourceBytes(), 0o644); err != nil {
		t.Fatalf("write bad source: %v", err)
	}
	goodSrcPath := filepath.Join(dir, "good-agent.src.md")
	if err := os.WriteFile(goodSrcPath, crossFormatSourceBytes(), 0o644); err != nil {
		t.Fatalf("write good source: %v", err)
	}
	_ = goodSrcPath // path used only for setup; outcome is checked via result.Files

	stub := interactiontest.NewBuilder().Build()
	deps, _ := newMarkdownToCodexRetargetDeps(t, stub)
	svc := app.New(deps)

	req := app.TransformHarnessRequest{
		SourceHarnessID: transformSourceHarness.ID,
		TargetHarnessID: "codex-harness",
		Path:            dir,          // directory: service enumerates all .src.md files
		TargetModel:     "codex-model",
		Overwrite:       false,        // no existing destinations; fresh creates only
		DryRun:          false,
	}

	// Act
	result, err := svc.TransformHarness(context.Background(), req)

	// Assert: no whole-run error.
	// A per-file failure (strip or encode) must not propagate as a run-level error.
	if err != nil {
		t.Fatalf("TransformHarness: unexpected whole-run error: %v; "+
			"a per-file failure must never abort the run", err)
	}

	// Assert: both files were processed (run did not abort after the first failure).
	if len(result.Files) != 2 {
		t.Fatalf("len(result.Files) = %d, want 2; "+
			"the run may have aborted after the per-file failure instead of continuing",
			len(result.Files))
	}

	// Assert: Files[0] (bad-agent, lexicographically first) shows StatusFailed with a reason.
	bad := result.Files[0]
	if bad.Status != app.StatusFailed {
		t.Errorf("Files[0].Status = %q, want StatusFailed; "+
			"a source with malformed mosaic_carriage must produce a per-file failure",
			bad.Status)
	}
	if bad.Reason == "" {
		t.Errorf("Files[0].Reason is empty; a per-file failure must carry a non-empty reason")
	}

	// Assert: Files[1] (good-agent) was processed successfully despite the earlier failure.
	good := result.Files[1]
	if good.Status != app.StatusTransformed {
		t.Errorf("Files[1].Status = %q, want StatusTransformed; "+
			"the run must continue past a per-file failure and process remaining files. "+
			"Reason: %s", good.Status, good.Reason)
	}
}

// ---------------------------------------------------------------------------
// Auxiliary helpers
// ---------------------------------------------------------------------------

// tomlKeys returns the keys present in a parsed TOML map, for use in error messages.
func tomlKeys(m map[string]interface{}) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}
