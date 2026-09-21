package app_test

// codex_promote_source_test.go covers T8.3.
//
// T8.3 -- Promote and retarget locate and read a Codex source file:
//   - A Codex .toml source file that satisfies both harness-only eligibility signals is
//     successfully read, decoded, and promoted without a read or decode error.
//   - The agent key derived from a Codex source is the filename with the .toml extension
//     stripped (e.g. "my-codex-agent.toml" -> "my-codex-agent"), not the full filename.
//   - The promoted output is a Markdown generic-agent file at the expected destination path.
//   - The promoted output contains the name field sourced from the Codex file.
//   - A Markdown source file continues to promote successfully through the same flow,
//     confirming the Markdown path is not regressed by the Codex read changes.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mosaic-deploy/internal/app"
	"mosaic-deploy/internal/app/interactiontest"
	"mosaic-deploy/internal/domain"
	_ "mosaic-deploy/internal/agentformat/all"
)

// ---------------------------------------------------------------------------
// Codex harness module stub for promote tests
// ---------------------------------------------------------------------------

// codexPromoteModule is a HarnessModule whose TargetPath returns key+".toml" and whose
// Descriptor declares AgentFormatID "codex-toml" with ".toml" as the agent extension.
// It is the minimal Codex module stub needed by the promote service.
type codexPromoteModule struct {
	ref        domain.HarnessRef
	descriptor domain.HarnessDescriptor
}

func (m *codexPromoteModule) Ref() domain.HarnessRef                { return m.ref }
func (m *codexPromoteModule) Descriptor() *domain.HarnessDescriptor { return &m.descriptor }
func (m *codexPromoteModule) Tools(req domain.ToolRequest) (domain.ToolResult, error) {
	res := make([]domain.ToolResolution, len(req.Generic))
	for i, g := range req.Generic {
		res[i] = domain.ToolResolution{Generic: g, Outcome: domain.ToolMapped}
	}
	return domain.ToolResult{Resolutions: res}, nil
}
func (m *codexPromoteModule) Frontmatter(req domain.FrontmatterRequest) (domain.FrontmatterPlan, error) {
	return domain.FrontmatterPlan{}, nil
}
func (m *codexPromoteModule) TargetPath(req domain.TargetPathRequest) (string, error) {
	return req.Key + ".toml", nil
}
func (m *codexPromoteModule) Injection(_ domain.InjectionRequest) (string, bool) { return "", false }
func (m *codexPromoteModule) HookPlan(_ domain.HookPlanRequest) (domain.HookPlan, error) {
	return domain.HookPlan{Supported: false, Reason: "stub"}, nil
}
func (m *codexPromoteModule) Close() error { return nil }

// newCodexPromoteModule returns a minimal codexPromoteModule wired as "codex-harness".
//
// The descriptor declares sandbox_mode in Frontmatter.KeyOrder so that NewFieldClassifier
// adds it to harnessKeys. Without this declaration, DetectHarnessMatch classifies
// sandbox_mode as ClassUnknown and returns HarnessMatchNo for any source file that carries
// it -- causing the file to be skipped as a harness mismatch before any retarget logic runs.
// The production Codex descriptor lists sandbox_mode in its key_order for the same reason.
func newCodexPromoteModule() *codexPromoteModule {
	return &codexPromoteModule{
		ref: domain.HarnessRef{ID: "codex-harness", DisplayName: "Codex Harness"},
		descriptor: domain.HarnessDescriptor{
			AgentFormatID: "codex-toml",
			Extensions: map[domain.ArtifactKind]string{
				domain.ArtifactAgent: ".toml",
			},
			Frontmatter: domain.FrontmatterSpec{
				KeyOrder: []string{"sandbox_mode"},
			},
		},
	}
}

// ---------------------------------------------------------------------------
// Codex promote test fixtures
// ---------------------------------------------------------------------------

// eligibleCodexTomlBytes returns a minimal Codex TOML file that satisfies both harness-only
// eligibility signals after decode:
//
//   - Signal one: the stamp block carries # mosaic_transform_version: 2.1.0, which the Codex
//     decoder maps to mosaic_transform_version: "2.1.0" in the canonical frontmatter.
//   - Signal two: developer_instructions carries a canonical <Identity type="core"> section
//     tag that becomes the canonical body after decode.
//
// The file also carries name and description so the promoted generic file has a populated
// name field to assert against.
func eligibleCodexTomlBytes() []byte {
	return []byte(
		"# mosaic_transform_version: 2.1.0\n" +
			"# mosaic_version: 1.0.0\n" +
			"\n" +
			"name = \"my-codex-agent\"\n" +
			"description = \"A Codex agent for promote tests\"\n" +
			"sandbox_mode = \"read-only\"\n" +
			"developer_instructions = \"\"\"\n" +
			"<Identity type=\"core\">\n" +
			"You are My Codex Agent.\n" +
			"</Identity>\n" +
			"\"\"\"\n",
	)
}

// writeEligibleCodexTomlFile writes eligibleCodexTomlBytes to dir/name and returns the path.
func writeEligibleCodexTomlFile(t *testing.T, dir, name string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("writeEligibleCodexTomlFile mkdir: %v", err)
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, eligibleCodexTomlBytes(), 0o644); err != nil {
		t.Fatalf("writeEligibleCodexTomlFile: %v", err)
	}
	return path
}

// newCodexPromoteDeps builds Deps suitable for Codex promote tests. It is identical to
// newPromoteDeps except the registry resolves "codex-harness" to a codexPromoteModule
// instead of the default Markdown stub module.
func newCodexPromoteDeps(t *testing.T, stub *interactiontest.Stub, mosaicRoot string) (app.Deps, string) {
	t.Helper()
	deps, workspace := newPromoteDeps(t, stub, mosaicRoot)
	deps.Registry = &stubRegistry{
		list: []domain.HarnessRef{
			{ID: "codex-harness", DisplayName: "Codex Harness"},
		},
		modules: map[string]domain.HarnessModule{
			"codex-harness": newCodexPromoteModule(),
		},
	}
	return deps, workspace
}

// ---------------------------------------------------------------------------
// T8.3 -- Promote reads a Codex .toml source without a read or decode error
// ---------------------------------------------------------------------------

// TestPromote_CodexSource_EligibleFile_WithCategory_NoError verifies that Promote
// successfully reads and decodes a Codex .toml source file: no read error, no decode
// error, and no eligibility rejection. The test confirms that the readDeployedArtifact
// funnel correctly dispatches to the Codex decoder for a .toml harness descriptor.
func TestPromote_CodexSource_EligibleFile_WithCategory_NoError(t *testing.T) {
	// Arrange
	mosaicRoot := writeMosaicRoot(t)
	srcPath := writeEligibleCodexTomlFile(t, t.TempDir(), "my-codex-agent.toml")

	stub := interactiontest.NewBuilder().Build()
	deps, _ := newCodexPromoteDeps(t, stub, mosaicRoot)
	svc := app.New(deps)

	req := app.PromoteRequest{
		FilePath:  srcPath,
		Category:  "TestCategory",
		HarnessID: "codex-harness",
	}

	// Act
	_, err := svc.Promote(context.Background(), req)

	// Assert: must not fail with a read error, decode error, or eligibility rejection.
	// Any of those would indicate the funnel did not correctly dispatch to the Codex decoder.
	if err != nil {
		t.Fatalf("Promote Codex source: unexpected error: %v", err)
	}
}

// TestPromote_CodexSource_KeyDerivedByStrippingTomlExtension verifies that the agent key is
// derived from the source filename by stripping the ".toml" extension, not by stripping the
// generic ".md" suffix. A Codex file "my-codex-agent.toml" must produce key "my-codex-agent",
// not "my-codex-agent.toml".
func TestPromote_CodexSource_KeyDerivedByStrippingTomlExtension(t *testing.T) {
	// Arrange
	mosaicRoot := writeMosaicRoot(t)
	srcPath := writeEligibleCodexTomlFile(t, t.TempDir(), "my-codex-agent.toml")

	stub := interactiontest.NewBuilder().Build()
	deps, _ := newCodexPromoteDeps(t, stub, mosaicRoot)
	svc := app.New(deps)

	req := app.PromoteRequest{
		FilePath:  srcPath,
		Category:  "TestCategory",
		HarnessID: "codex-harness",
	}

	// Act
	result, err := svc.Promote(context.Background(), req)

	// Assert
	if err != nil {
		t.Fatalf("Promote Codex source: unexpected error: %v", err)
	}
	wantKey := "my-codex-agent"
	if result.Key != wantKey {
		t.Errorf("Key = %q, want %q; .toml extension must be stripped, not retained", result.Key, wantKey)
	}
}

// TestPromote_CodexSource_DestinationFileExistsAndIsMarkdown verifies that a successful
// Codex-source promote writes a file at the computed destination path and that the file is
// a well-formed Markdown document (starts with "---\n"), confirming the generic output format
// is always Markdown regardless of the source format.
func TestPromote_CodexSource_DestinationFileExistsAndIsMarkdown(t *testing.T) {
	// Arrange
	mosaicRoot := writeMosaicRoot(t)
	srcPath := writeEligibleCodexTomlFile(t, t.TempDir(), "my-codex-agent.toml")

	stub := interactiontest.NewBuilder().Build()
	deps, _ := newCodexPromoteDeps(t, stub, mosaicRoot)
	svc := app.New(deps)

	req := app.PromoteRequest{
		FilePath:  srcPath,
		Category:  "TestCategory",
		HarnessID: "codex-harness",
	}

	// Act
	result, err := svc.Promote(context.Background(), req)
	if err != nil {
		t.Fatalf("Promote Codex source: unexpected error: %v", err)
	}

	// Assert: destination file exists.
	destBytes, readErr := os.ReadFile(result.DestinationPath)
	if readErr != nil {
		t.Fatalf("destination file %q could not be read: %v", result.DestinationPath, readErr)
	}

	// Assert: destination is Markdown generic frontmatter (starts with "---\n").
	if !strings.HasPrefix(string(destBytes), "---\n") {
		t.Errorf("destination file does not start with Markdown frontmatter delimiter \"---\\n\"; got: %q",
			string(destBytes[:min(len(destBytes), 40)]))
	}

	// Assert: destination carries the name sourced from the Codex file.
	if !strings.Contains(string(destBytes), "name: my-codex-agent") {
		t.Errorf("destination file does not contain \"name: my-codex-agent\"; got:\n%s", string(destBytes))
	}
}

// ---------------------------------------------------------------------------
// T8.3 -- Markdown source symmetry: existing Markdown path not regressed
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// T8.3 -- Retarget loop: Codex source agent key derivation and canonical bytes
// ---------------------------------------------------------------------------

// eligibleCodexTomlRetargetBytes returns the minimal Codex TOML bytes for a retarget test.
// The file carries only a stamp block (mosaic_transform_version, eligibility signal one) and
// developer_instructions with an Identity region (eligibility signal two). No harness-specific
// TOML keys (sandbox_mode, name, etc.) are present so that DetectHarnessMatch finds no
// ClassUnknown fields and returns HarnessMatchIndeterminate -- allowing the retarget to proceed.
func eligibleCodexTomlRetargetBytes() []byte {
	return []byte(
		"# mosaic_transform_version: 2.1.0\n" +
			"\n" +
			"developer_instructions = \"\"\"\n" +
			"<Identity type=\"core\">\n" +
			"You are a Codex retarget test agent.\n" +
			"</Identity>\n" +
			"\"\"\"\n",
	)
}

// newCodexRetargetDeps builds Deps for a Codex-source retarget test. The registry has two
// harnesses: "codex-harness" as the source (Codex .toml format) and "stub-target-harness"
// as the target (stub Markdown module). Both are Usable so the TransformHarness flow does
// not reject them when building the target-harness option list.
func newCodexRetargetDeps(t *testing.T, stub *interactiontest.Stub) (app.Deps, string) {
	t.Helper()
	deps, workspace := newBaseDeps(t, stub)
	srcMod := newCodexPromoteModule()
	tgtMod := &stubHarnessModule{
		ref: domain.HarnessRef{
			ID:          "stub-target-harness",
			DisplayName: "Stub Target Harness",
			Usable:      true,
		},
		descriptor: domain.HarnessDescriptor{
			ID:          "stub-target-harness",
			DisplayName: "Stub Target Harness",
			Paths: domain.PathSpec{
				Agents: domain.ScopedPaths{
					Supported: true,
					Project:   "agents",
				},
			},
			Extensions: map[domain.ArtifactKind]string{
				domain.ArtifactAgent: ".md",
			},
		},
	}
	deps.Registry = &stubRegistry{
		list: []domain.HarnessRef{
			{ID: "codex-harness", DisplayName: "Codex Harness", Usable: true},
			{ID: "stub-target-harness", DisplayName: "Stub Target Harness", Usable: true},
		},
		modules: map[string]domain.HarnessModule{
			"codex-harness":       srcMod,
			"stub-target-harness": tgtMod,
		},
	}
	return deps, workspace
}

// TestTransformHarness_CodexSource_AgentKeyStripsTomlExtension verifies that the retarget
// batch loop in transform_service_impl.go correctly handles a Codex .toml source file on
// both the key-derivation and the bytes-dispatch dimensions:
//
//  1. Agent-key derivation: the agent key produced from "my-codex-agent.toml" must be
//     "my-codex-agent" -- the ".toml" extension is stripped using the source harness's
//     declared extension, not the generic ".md" suffix.
//
//  2. Canonical bytes dispatched to DetectHarnessMatch: IndexSourceModels stores canonical
//     (decoded Markdown) bytes in sf.Content. If raw TOML bytes were dispatched instead,
//     eligibleHarnessOnly would find no transform_version in Markdown frontmatter and
//     return ineligible, so DetectHarnessMatch would return HarnessMatchNotAgent and the
//     outcome would be StatusSkippedNotAgent rather than a successful transform.
func TestTransformHarness_CodexSource_AgentKeyStripsTomlExtension(t *testing.T) {
	// Arrange: write a minimal eligible Codex TOML file.
	dir := t.TempDir()
	srcFile := writeTransformFile(t, dir, "my-codex-agent.toml", eligibleCodexTomlRetargetBytes())

	stub := interactiontest.NewBuilder().Build()
	deps, _ := newCodexRetargetDeps(t, stub)
	svc := app.New(deps)

	req := app.TransformHarnessRequest{
		SourceHarnessID: "codex-harness",
		TargetHarnessID: "stub-target-harness",
		Path:            srcFile,
		TargetModel:     "target-model",
		Overwrite:       true, // overwrite if destination exists
		DryRun:          true, // do not write any files to disk
	}

	// Act
	result, err := svc.TransformHarness(context.Background(), req)

	// Assert
	if err != nil {
		t.Fatalf("TransformHarness: unexpected error: %v", err)
	}
	if len(result.Files) != 1 {
		t.Fatalf("len(result.Files) = %d, want 1; expected one outcome for the single source file",
			len(result.Files))
	}
	out := result.Files[0]

	// StatusSkippedNotAgent is evidence that DetectHarnessMatch received raw TOML bytes:
	// raw TOML has no Markdown frontmatter with transform_version and no canonical section
	// tags, so eligibleHarnessOnly would return ineligible. A non-skipped status proves
	// that canonical Markdown bytes (which carry both signals) were dispatched.
	if out.Status == app.StatusSkippedNotAgent {
		t.Errorf("Status == StatusSkippedNotAgent; DetectHarnessMatch received raw TOML bytes "+
			"instead of canonical Markdown bytes from IndexSourceModels; "+
			"reason: %s", out.Reason)
	}

	// The agent key must have the ".toml" suffix stripped by the retarget loop.
	if strings.HasSuffix(out.AgentKey, ".toml") {
		t.Errorf("AgentKey = %q; .toml extension must be stripped by the retarget batch loop "+
			"using the source harness's declared extension, not carried into the agent key",
			out.AgentKey)
	}
	wantKey := "my-codex-agent"
	if out.AgentKey != wantKey {
		t.Errorf("AgentKey = %q, want %q; filename %q with .toml stripped should yield %q",
			out.AgentKey, wantKey, filepath.Base(srcFile), wantKey)
	}
}

// TestPromote_MarkdownSource_EligibleFile_WithCategory_NoError verifies that the standard
// Markdown promote path continues to work after the Codex read changes: a Markdown source
// passes eligibility and produces a successful PromoteResult. This is a symmetry test that
// guards against regressions introduced by the Codex funnel dispatch.
func TestPromote_MarkdownSource_EligibleFile_WithCategory_NoError(t *testing.T) {
	// Arrange
	mosaicRoot := writeMosaicRoot(t)
	srcPath := writeEligibleSourceFile(t, t.TempDir(), "my-harness-agent.md")

	stub := interactiontest.NewBuilder().Build()
	deps, _ := newPromoteDeps(t, stub, mosaicRoot)
	svc := app.New(deps)

	req := app.PromoteRequest{
		FilePath:  srcPath,
		Category:  "TestCategory",
		HarnessID: "stub-harness",
	}

	// Act
	result, err := svc.Promote(context.Background(), req)

	// Assert
	if err != nil {
		t.Fatalf("Promote Markdown source: unexpected error: %v", err)
	}
	wantKey := "my-harness-agent"
	if result.Key != wantKey {
		t.Errorf("Key = %q, want %q; .md extension must be stripped for Markdown source", result.Key, wantKey)
	}
}

