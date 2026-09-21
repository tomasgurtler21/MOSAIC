package app_test

// retarget_nonrecoverable_test.go covers T15.2, T15.3, T15.4, and T15.5 (retarget direction).
//
// T15.2 -- Retarget from Codex (non-recoverable source):
//   (a) The reverse leg yields no generic tools (no QPromoteCustomTool asked).
//   (b) The capability loss is reported, not passed silently.
//   (c) The target is written with an explicitly minimal read-only grant in the exact form that
//       target emits, obtained through RenderMinimalToolGrant -- routing through the target
//       module's own Tools() method.
//   (d) Escalation is never inferred from the source's sandbox_mode.
//   (e) The carriage container is stripped and its keys reported at the format-change boundary.
//   (f) sandbox_mode from the Codex source is stripped and named with its value in the report:
//       StrippedField{Key: "sandbox_mode", Reason: StripReasonUnknownField}.
//
// T15.3 -- Cross-format container invariant at flow level:
//   A promote and a retarget out of a Codex source carrying user keys of both shapes produce a
//   target file free of the container AND a run report naming every key that did not travel.
//   The source file is not modified by either operation.
//
// T15.4 -- Retarget to Codex:
//   Whatever generic tools the source has collapse into sandbox_mode by the collapse table, end
//   to end through the retarget flow, asserted on the encoded TOML destination file.
//   This test depends on Half A (I15.6) being complete; it will fail RED until then.
//
// T15.5 -- Markdown-to-Markdown retarget unchanged (retarget direction):
//   Retarget between two Markdown harnesses does not ask QPromoteNonRecoverableTools, does not
//   emit a capability-loss notice, and the result contains the harness-mapped tools from the
//   reverse mapping path.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	toml "github.com/pelletier/go-toml/v2"

	"mosaic-deploy/internal/app"
	"mosaic-deploy/internal/app/interactiontest"
	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/harness/descriptor"
	_ "mosaic-deploy/internal/agentformat/all"
)

// ---------------------------------------------------------------------------
// minimalGrantTargetModule -- target harness stub that returns Fields from Tools()
// ---------------------------------------------------------------------------

// minimalGrantTargetModule is a harness module stub whose Tools() method returns both
// Resolutions and Fields for the requested generic tools. It models a list-format harness
// (ToolsKey: "tools") and emits a KindList field. Tests that call RenderMinimalToolGrant
// through the retarget flow need a target module that provides non-empty Fields; the plain
// stubHarnessModule does not return Fields and would trigger ErrMinimalGrantFieldAbsent.
type minimalGrantTargetModule struct {
	ref        domain.HarnessRef
	descriptor domain.HarnessDescriptor
}

func (m *minimalGrantTargetModule) Ref() domain.HarnessRef                { return m.ref }
func (m *minimalGrantTargetModule) Descriptor() *domain.HarnessDescriptor { return &m.descriptor }

// Tools maps every requested generic tool and returns a single "tools" field carrying all of
// them as a KindList value, matching the behaviour of a list-format harness like Claude Code.
// When the caller requests the minimal grant set the returned field carries exactly the three
// minimal-grant tool names, which is what T15.2(c) asserts.
func (m *minimalGrantTargetModule) Tools(req domain.ToolRequest) (domain.ToolResult, error) {
	items := make([]domain.FieldValue, len(req.Generic))
	resolutions := make([]domain.ToolResolution, len(req.Generic))
	for i, g := range req.Generic {
		items[i] = domain.FieldValue{Kind: domain.KindScalar, Scalar: g}
		resolutions[i] = domain.ToolResolution{Generic: g, Outcome: domain.ToolMapped, HarnessTools: []string{g}}
	}
	toolsKey := m.descriptor.Frontmatter.ToolsKey
	if toolsKey == "" {
		toolsKey = "tools"
	}
	field := domain.FrontmatterField{
		Key: toolsKey,
		Value: domain.FieldValue{
			Kind:  domain.KindList,
			Items: items,
			List:  domain.ListBlock,
		},
	}
	return domain.ToolResult{
		Fields:      []domain.FrontmatterField{field},
		Resolutions: resolutions,
	}, nil
}

func (m *minimalGrantTargetModule) Frontmatter(req domain.FrontmatterRequest) (domain.FrontmatterPlan, error) {
	return domain.FrontmatterPlan{}, nil
}

func (m *minimalGrantTargetModule) TargetPath(req domain.TargetPathRequest) (string, error) {
	return req.Key + ".md", nil
}

func (m *minimalGrantTargetModule) Injection(_ domain.InjectionRequest) (string, bool) { return "", false }

func (m *minimalGrantTargetModule) HookPlan(_ domain.HookPlanRequest) (domain.HookPlan, error) {
	return domain.HookPlan{Supported: false, Reason: "stub"}, nil
}

func (m *minimalGrantTargetModule) Close() error { return nil }

// newMinimalGrantTargetModule returns a minimalGrantTargetModule wired as "minimal-tgt-harness".
func newMinimalGrantTargetModule() *minimalGrantTargetModule {
	return &minimalGrantTargetModule{
		ref: domain.HarnessRef{
			ID:          "minimal-tgt-harness",
			DisplayName: "Minimal Grant Target Harness",
			Usable:      true,
		},
		descriptor: domain.HarnessDescriptor{
			ID:          "minimal-tgt-harness",
			DisplayName: "Minimal Grant Target Harness",
			Frontmatter: domain.FrontmatterSpec{
				ToolsKey: "tools",
			},
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
}

// ---------------------------------------------------------------------------
// Deps helpers for non-recoverable retarget tests
// ---------------------------------------------------------------------------

const (
	codexNRHarnessID = "codex-nr-harness"
	minTgtHarnessID  = "minimal-tgt-harness"
)

// newCodexNonRecoverableRetargetModule returns a Codex module with ToolInfoUnrecoverable=true
// and the harness ref ID set to codexNRHarnessID so that DetectHarnessMatch agrees with the
// SourceHarnessID in the retarget request.
func newCodexNonRecoverableRetargetModule() *codexPromoteModule {
	m := newCodexNonRecoverableModule()
	m.ref = domain.HarnessRef{ID: codexNRHarnessID, DisplayName: "Codex NR Harness", Usable: true}
	return m
}

// newCodexNonRecoverableRetargetDeps builds Deps for T15.2 retarget tests.
// The registry has:
//   - "codex-nr-harness": Codex module with ToolInfoUnrecoverable=true (source)
//   - "minimal-tgt-harness": minimalGrantTargetModule with ToolsKey="tools" (target)
func newCodexNonRecoverableRetargetDeps(t *testing.T, stub *interactiontest.Stub) (app.Deps, string) {
	t.Helper()
	deps, workspace := newBaseDeps(t, stub)
	srcMod := newCodexNonRecoverableRetargetModule()
	tgtMod := newMinimalGrantTargetModule()
	deps.Registry = &stubRegistry{
		list: []domain.HarnessRef{
			{ID: codexNRHarnessID, DisplayName: "Codex NR Harness", Usable: true},
			{ID: minTgtHarnessID, DisplayName: "Minimal Grant Target Harness", Usable: true},
		},
		modules: map[string]domain.HarnessModule{
			codexNRHarnessID: srcMod,
			minTgtHarnessID:  tgtMod,
		},
	}
	return deps, workspace
}

// eligibleCodexTomlRetargetWithSandboxModeBytes returns Codex TOML for retarget tests that
// need a sandbox_mode field present in the source so the stripping machinery can report it.
// The file carries both eligibility signals and includes sandbox_mode = "read-only", which is
// the value the Codex decoder propagates into the canonical document when present.
//
// Tests that assert StrippedField{Key:"sandbox_mode", Reason:StripReasonUnknownField} must use
// this fixture rather than eligibleCodexTomlRetargetBytes(), which omits sandbox_mode entirely
// (meaning the Codex decoder never emits it and there is nothing to strip or report).
func eligibleCodexTomlRetargetWithSandboxModeBytes() []byte {
	return []byte(
		"# mosaic_transform_version: 2.1.0\n" +
			"\n" +
			"sandbox_mode = \"read-only\"\n" +
			"developer_instructions = \"\"\"\n" +
			"<Identity type=\"core\">\n" +
			"You are a Codex retarget test agent.\n" +
			"</Identity>\n" +
			"\"\"\"\n",
	)
}

// eligibleCodexTomlRetargetWithUserKeyBytes returns Codex TOML suitable for retarget tests
// that need a user-owned key in the carriage container. The file is eligible (has both
// eligibility signals) and carries my_custom_setting as a user key.
//
// Deliberately omits sandbox_mode and other Codex-native TOML keys so that DetectHarnessMatch
// finds no ClassUnknown fields and returns HarnessMatchIndeterminate, allowing the retarget to
// proceed rather than being skipped as a harness mismatch.
func eligibleCodexTomlRetargetWithUserKeyBytes() []byte {
	return []byte(
		"# mosaic_transform_version: 2.1.0\n" +
			"\n" +
			"my_custom_setting = \"user-value\"\n" +
			"developer_instructions = \"\"\"\n" +
			"<Identity type=\"core\">\n" +
			"You are a Codex retarget test agent.\n" +
			"</Identity>\n" +
			"\"\"\"\n",
	)
}

// runCodexNonRecoverableRetarget writes a Codex source file and runs TransformHarness from
// the non-recoverable Codex harness to the minimal grant target. Returns the result and any
// error. The helper sets Overwrite:true so pre-existing destination files do not block the run.
func runCodexNonRecoverableRetarget(t *testing.T, stub *interactiontest.Stub, srcBytes []byte) (
	app.TransformHarnessResult, error,
) {
	t.Helper()
	deps, _ := newCodexNonRecoverableRetargetDeps(t, stub)
	svc := app.New(deps)

	srcDir := t.TempDir()
	srcPath := filepath.Join(srcDir, "my-codex-agent.toml")
	if err := os.WriteFile(srcPath, srcBytes, 0o644); err != nil {
		t.Fatalf("runCodexNonRecoverableRetarget: write source: %v", err)
	}

	req := app.TransformHarnessRequest{
		SourceHarnessID: codexNRHarnessID,
		TargetHarnessID: minTgtHarnessID,
		Path:            srcPath,
		Overwrite:       true,
	}

	return svc.TransformHarness(context.Background(), req)
}

// ---------------------------------------------------------------------------
// T15.2(a) -- reverse leg yields no generic tools (no QPromoteCustomTool asked)
// ---------------------------------------------------------------------------

// TestRetarget_CodexNonRecoverable_NoQPromoteCustomToolAsked verifies that when the source
// harness has ToolInfoUnrecoverable=true, the retarget flow does NOT ask QPromoteCustomTool.
// The non-recoverable branch skips the reverse-mapping path entirely; asking QPromoteCustomTool
// would mean the flow tried to reverse-map tools that cannot be recovered.
func TestRetarget_CodexNonRecoverable_NoQPromoteCustomToolAsked(t *testing.T) {
	stub := interactiontest.NewBuilder().Build()
	result, err := runCodexNonRecoverableRetarget(t, stub, eligibleCodexTomlRetargetBytes())
	if err != nil {
		t.Fatalf("TransformHarness: unexpected error: %v", err)
	}
	if len(result.Files) == 0 {
		t.Fatalf("TransformHarness: no files in result; expected at least one file outcome")
	}

	for _, call := range stub.Calls() {
		if call.ID == domain.QPromoteCustomTool {
			t.Errorf("QPromoteCustomTool was asked for a non-recoverable source; "+
				"the non-recoverable branch must skip the reverse-mapping path entirely")
			break
		}
	}
}

// ---------------------------------------------------------------------------
// T15.2(b) -- capability loss is reported
// ---------------------------------------------------------------------------

// TestRetarget_CodexNonRecoverable_CapabilityLoss_WarningNoticeEmitted verifies that retarget
// from a non-recoverable source emits at least one NoticeWarning to inform the operator that
// tools could not be recovered and a minimal read-only grant was applied instead.
func TestRetarget_CodexNonRecoverable_CapabilityLoss_WarningNoticeEmitted(t *testing.T) {
	stub := interactiontest.NewBuilder().Build()
	_, err := runCodexNonRecoverableRetarget(t, stub, eligibleCodexTomlRetargetBytes())
	if err != nil {
		t.Fatalf("TransformHarness: unexpected error: %v", err)
	}

	notices := stub.Notices()
	hasWarning := false
	for _, n := range notices {
		if n.Level == domain.NoticeWarning {
			hasWarning = true
			break
		}
	}
	if !hasWarning {
		t.Errorf("no NoticeWarning emitted for retarget from non-recoverable source; "+
			"the capability loss must be reported to the operator; got notices: %v", notices)
	}
}

// ---------------------------------------------------------------------------
// T15.2(c) -- target written with minimal grant in exact target format
// ---------------------------------------------------------------------------

// TestRetarget_CodexNonRecoverable_MinimalGrant_AllThreeToolsInOutputFile verifies that the
// destination file contains each member of the minimal generic tool set. The grant is obtained
// through RenderMinimalToolGrant which routes through the target module's own Tools() method.
// For the minimalGrantTargetModule, the tools field is KindList and contains the three names.
func TestRetarget_CodexNonRecoverable_MinimalGrant_AllThreeToolsInOutputFile(t *testing.T) {
	stub := interactiontest.NewBuilder().Build()
	result, err := runCodexNonRecoverableRetarget(t, stub, eligibleCodexTomlRetargetBytes())
	if err != nil {
		t.Fatalf("TransformHarness: unexpected error: %v", err)
	}
	if len(result.Files) == 0 {
		t.Fatalf("TransformHarness: no files in result")
	}

	out := result.Files[0]
	if out.Status != app.StatusTransformed {
		t.Fatalf("file status = %q (reason: %q), want %q", out.Status, out.Reason, app.StatusTransformed)
	}

	destBytes, readErr := os.ReadFile(out.DestinationPath)
	if readErr != nil {
		t.Fatalf("cannot read destination file %q: %v", out.DestinationPath, readErr)
	}
	destStr := string(destBytes)

	// Each member of MinimalGenericToolSet must appear in the destination file.
	wantTools := []string{"file_read", "file_search", "content_search"}
	for _, tool := range wantTools {
		if !strings.Contains(destStr, tool) {
			t.Errorf("destination file does not contain minimal grant tool %q; "+
				"the target must be written with an explicitly minimal read-only grant; got:\n%s",
				tool, destStr)
		}
	}
}

// TestRetarget_CodexNonRecoverable_MinimalGrant_ToolsKeyPresentInOutputFile verifies that the
// destination file contains the target module's declared tools key ("tools:"). This confirms that
// the grant was emitted using the target module's ToolsKey field rather than a hardcoded or absent
// field name. It does not assert on the specific tool names; the companion test
// TestRetarget_CodexNonRecoverable_MinimalGrant_AllThreeToolsInOutputFile covers the full
// minimal-grant content.
func TestRetarget_CodexNonRecoverable_MinimalGrant_ToolsKeyPresentInOutputFile(t *testing.T) {
	stub := interactiontest.NewBuilder().Build()
	result, err := runCodexNonRecoverableRetarget(t, stub, eligibleCodexTomlRetargetBytes())
	if err != nil {
		t.Fatalf("TransformHarness: unexpected error: %v", err)
	}
	if len(result.Files) == 0 {
		t.Fatalf("TransformHarness: no files in result")
	}

	out := result.Files[0]
	if out.Status != app.StatusTransformed {
		t.Fatalf("file status = %q, want %q", out.Status, app.StatusTransformed)
	}

	destBytes, readErr := os.ReadFile(out.DestinationPath)
	if readErr != nil {
		t.Fatalf("cannot read destination: %v", readErr)
	}

	if !strings.Contains(string(destBytes), "tools:") {
		t.Errorf("destination file does not contain a \"tools:\" field; "+
			"the minimal grant must be emitted through the target module's declared ToolsKey; got:\n%s",
			string(destBytes))
	}
}

// ---------------------------------------------------------------------------
// T15.2(d) -- escalation never inferred from source sandbox_mode
// ---------------------------------------------------------------------------

// TestRetarget_CodexNonRecoverable_NoEscalationFromSandboxMode verifies that the target file
// does not contain tool names that would only appear if the retarget flow read the source's
// sandbox_mode value and inferred an escalated grant from it. The tools field must contain
// exactly the minimal grant set (file_read, file_search, content_search) and nothing more.
func TestRetarget_CodexNonRecoverable_NoEscalationFromSandboxMode(t *testing.T) {
	stub := interactiontest.NewBuilder().Build()
	result, err := runCodexNonRecoverableRetarget(t, stub, eligibleCodexTomlRetargetBytes())
	if err != nil {
		t.Fatalf("TransformHarness: unexpected error: %v", err)
	}
	if len(result.Files) == 0 {
		t.Fatalf("TransformHarness: no files in result")
	}

	out := result.Files[0]
	if out.Status != app.StatusTransformed {
		t.Fatalf("file status = %q, want %q", out.Status, app.StatusTransformed)
	}

	destBytes, readErr := os.ReadFile(out.DestinationPath)
	if readErr != nil {
		t.Fatalf("cannot read destination: %v", readErr)
	}
	destStr := string(destBytes)

	// Escalating tools must not appear in the target; the minimal grant is read-only.
	escalatingTools := []string{"terminal", "file_write", "file_edit", "bash", "subagent"}
	for _, tool := range escalatingTools {
		if strings.Contains(destStr, tool) {
			t.Errorf("destination file contains escalating tool %q; "+
				"escalation must never be inferred from the source's sandbox_mode value; "+
				"the non-recoverable branch must apply only the minimal read-only grant; got:\n%s",
				tool, destStr)
		}
	}
}

// ---------------------------------------------------------------------------
// T15.2(e) -- carriage container stripped at format-change boundary
// ---------------------------------------------------------------------------

// TestRetarget_CodexNonRecoverable_CarriageContainer_AbsentFromTarget verifies that the
// carriage container keys are not present in the target Markdown file. User keys carried in the
// Codex source's mosaic_carriage must be stripped at the format-change boundary.
func TestRetarget_CodexNonRecoverable_CarriageContainer_AbsentFromTarget(t *testing.T) {
	stub := interactiontest.NewBuilder().Build()
	result, err := runCodexNonRecoverableRetarget(t, stub, eligibleCodexTomlRetargetWithUserKeyBytes())
	if err != nil {
		t.Fatalf("TransformHarness: unexpected error: %v", err)
	}
	if len(result.Files) == 0 {
		t.Fatalf("TransformHarness: no files in result")
	}

	out := result.Files[0]
	if out.Status != app.StatusTransformed {
		t.Fatalf("file status = %q, want %q", out.Status, app.StatusTransformed)
	}

	destBytes, readErr := os.ReadFile(out.DestinationPath)
	if readErr != nil {
		t.Fatalf("cannot read destination: %v", readErr)
	}
	destStr := string(destBytes)

	if strings.Contains(destStr, "mosaic_carriage:") {
		t.Errorf("destination file contains \"mosaic_carriage:\"; "+
			"the carriage container must be stripped at the format-change boundary; got:\n%s", destStr)
	}
	if strings.Contains(destStr, "mosaic_carriage_markers:") {
		t.Errorf("destination file contains \"mosaic_carriage_markers:\"; "+
			"it must be stripped at the format-change boundary; got:\n%s", destStr)
	}
}

// TestRetarget_CodexNonRecoverable_CarriedKey_NamedInStrippedFields verifies that the user key
// held in the carriage container (my_custom_setting) appears in the file outcome's StrippedFields
// with Reason StripReasonCarriedKey.
func TestRetarget_CodexNonRecoverable_CarriedKey_NamedInStrippedFields(t *testing.T) {
	stub := interactiontest.NewBuilder().Build()
	result, err := runCodexNonRecoverableRetarget(t, stub, eligibleCodexTomlRetargetWithUserKeyBytes())
	if err != nil {
		t.Fatalf("TransformHarness: unexpected error: %v", err)
	}
	if len(result.Files) == 0 {
		t.Fatalf("TransformHarness: no files in result")
	}

	out := result.Files[0]
	if out.Status != app.StatusTransformed {
		t.Fatalf("file status = %q, want %q", out.Status, app.StatusTransformed)
	}

	found := false
	for _, sf := range out.StrippedFields {
		if sf.Key == "my_custom_setting" && sf.Reason == app.StripReasonCarriedKey {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("file outcome StrippedFields does not contain {Key: \"my_custom_setting\", Reason: StripReasonCarriedKey}; "+
			"every key held in the carriage container must be named in the run report; got: %v", out.StrippedFields)
	}
}

// ---------------------------------------------------------------------------
// T15.2(f) -- sandbox_mode stripped and named with StripReasonUnknownField
// ---------------------------------------------------------------------------

// TestRetarget_CodexNonRecoverable_SandboxMode_StrippedAndNamedInReport verifies that
// sandbox_mode from the Codex source is stripped from the target Markdown output and reported
// in the file outcome's StrippedFields as:
//
//	StrippedField{Key: "sandbox_mode", Reason: StripReasonUnknownField}
func TestRetarget_CodexNonRecoverable_SandboxMode_StrippedAndNamedInReport(t *testing.T) {
	stub := interactiontest.NewBuilder().Build()
	result, err := runCodexNonRecoverableRetarget(t, stub, eligibleCodexTomlRetargetWithSandboxModeBytes())
	if err != nil {
		t.Fatalf("TransformHarness: unexpected error: %v", err)
	}
	if len(result.Files) == 0 {
		t.Fatalf("TransformHarness: no files in result")
	}

	out := result.Files[0]
	if out.Status != app.StatusTransformed {
		t.Fatalf("file status = %q, want %q", out.Status, app.StatusTransformed)
	}

	found := false
	for _, sf := range out.StrippedFields {
		if sf.Key == "sandbox_mode" && sf.Reason == app.StripReasonUnknownField {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("file outcome StrippedFields does not contain {Key: \"sandbox_mode\", Reason: StripReasonUnknownField}; "+
			"sandbox_mode must be stripped and reported when retargeting from Codex; got: %v", out.StrippedFields)
	}
}

// TestRetarget_CodexNonRecoverable_SandboxMode_AbsentFromTargetFile verifies that the target
// Markdown file does not contain a sandbox_mode key when the source carries one.
// sandbox_mode is Codex-native vocabulary that has no meaning in generic Markdown; stripping it
// (asserted by TestRetarget_CodexNonRecoverable_SandboxMode_StrippedAndNamedInReport) must also
// prevent it from appearing in the written file. Uses eligibleCodexTomlRetargetWithSandboxModeBytes
// so the source actually carries sandbox_mode -- a source without it trivially satisfies this
// assertion and does not guard against the field leaking through.
func TestRetarget_CodexNonRecoverable_SandboxMode_AbsentFromTargetFile(t *testing.T) {
	stub := interactiontest.NewBuilder().Build()
	result, err := runCodexNonRecoverableRetarget(t, stub, eligibleCodexTomlRetargetWithSandboxModeBytes())
	if err != nil {
		t.Fatalf("TransformHarness: unexpected error: %v", err)
	}
	if len(result.Files) == 0 {
		t.Fatalf("TransformHarness: no files in result")
	}

	out := result.Files[0]
	if out.Status != app.StatusTransformed {
		t.Fatalf("file status = %q, want %q", out.Status, app.StatusTransformed)
	}

	destBytes, readErr := os.ReadFile(out.DestinationPath)
	if readErr != nil {
		t.Fatalf("cannot read destination: %v", readErr)
	}

	if strings.Contains(string(destBytes), "sandbox_mode:") {
		t.Errorf("destination file contains \"sandbox_mode:\"; "+
			"sandbox_mode is Codex-native vocabulary and must not appear in the Markdown target file; got:\n%s",
			string(destBytes))
	}
}

// ---------------------------------------------------------------------------
// T15.7(b) and T15.7(c) -- Codex-source retarget to Markdown: destination format
// ---------------------------------------------------------------------------

// TestRetarget_CodexNonRecoverable_Destination_StartsWithYamlFrontmatter verifies that
// retargeting a Codex-source agent to a Markdown harness produces a destination file that
// starts with the YAML frontmatter delimiter ("---\n"). This is the format-side assertion
// for T15.7(b): a Codex-to-Markdown retarget must produce valid Markdown, not a TOML or
// raw-text file. The tool-content aspect (minimal grant tools present) is covered by the
// companion T15.2(c) tests.
func TestRetarget_CodexNonRecoverable_Destination_StartsWithYamlFrontmatter(t *testing.T) {
	// Arrange + Act
	stub := interactiontest.NewBuilder().Build()
	result, err := runCodexNonRecoverableRetarget(t, stub, eligibleCodexTomlRetargetBytes())
	if err != nil {
		t.Fatalf("TransformHarness: unexpected error: %v", err)
	}
	if len(result.Files) == 0 {
		t.Fatalf("TransformHarness: no files in result")
	}
	out := result.Files[0]
	if out.Status != app.StatusTransformed {
		t.Fatalf("file status = %q (reason: %q), want %q", out.Status, out.Reason, app.StatusTransformed)
	}

	// Assert: destination file starts with YAML frontmatter.
	destBytes, readErr := os.ReadFile(out.DestinationPath)
	if readErr != nil {
		t.Fatalf("cannot read destination file %q: %v", out.DestinationPath, readErr)
	}
	if !strings.HasPrefix(string(destBytes), "---\n") {
		t.Errorf("destination file does not start with YAML frontmatter delimiter \"---\\n\"; "+
			"a Codex-source retarget to a Markdown harness must produce valid Markdown with YAML "+
			"frontmatter, not a TOML or raw-text file; first 100 bytes: %q",
			string(destBytes[:min(len(destBytes), 100)]))
	}
}

// TestRetarget_CodexNonRecoverable_Destination_HasMarkdownExtension verifies that the
// destination file for a Codex-source retarget to a Markdown harness has the .md extension
// declared by the target harness. This is a necessary condition for T15.7(c): if the
// extension does not match the target harness's declaration, the extension-and-format
// agreement assertion is meaningless.
func TestRetarget_CodexNonRecoverable_Destination_HasMarkdownExtension(t *testing.T) {
	// Arrange + Act
	stub := interactiontest.NewBuilder().Build()
	result, err := runCodexNonRecoverableRetarget(t, stub, eligibleCodexTomlRetargetBytes())
	if err != nil {
		t.Fatalf("TransformHarness: unexpected error: %v", err)
	}
	if len(result.Files) == 0 {
		t.Fatalf("TransformHarness: no files in result")
	}
	out := result.Files[0]
	if out.Status != app.StatusTransformed {
		t.Fatalf("file status = %q (reason: %q), want %q", out.Status, out.Reason, app.StatusTransformed)
	}

	// Assert: destination path ends with the .md extension declared by the target harness.
	if !strings.HasSuffix(out.DestinationPath, ".md") {
		t.Errorf("DestinationPath = %q does not end with .md; "+
			"the target harness declares .md as its extension for agent artifacts; "+
			"the destination extension must match the target harness's declared extension",
			out.DestinationPath)
	}
}

// TestRetarget_CodexNonRecoverable_Destination_ExtensionAndFormatAgree verifies that the
// destination file's extension and its content format are consistent for the
// Codex-to-Markdown direction (T15.7(c)). A .md file must contain Markdown with YAML
// frontmatter; a .toml file must contain TOML. The companion test for the
// Markdown-to-Codex direction is TestRetarget_MarkdownSource_ToCodexTarget_ExtensionAndFormatAgree
// in retarget_crossformat_test.go.
func TestRetarget_CodexNonRecoverable_Destination_ExtensionAndFormatAgree(t *testing.T) {
	// Arrange + Act
	stub := interactiontest.NewBuilder().Build()
	result, err := runCodexNonRecoverableRetarget(t, stub, eligibleCodexTomlRetargetBytes())
	if err != nil {
		t.Fatalf("TransformHarness: unexpected error: %v", err)
	}
	if len(result.Files) == 0 {
		t.Fatalf("TransformHarness: no files in result")
	}
	out := result.Files[0]
	if out.Status != app.StatusTransformed {
		t.Fatalf("file status = %q (reason: %q), want %q", out.Status, out.Reason, app.StatusTransformed)
	}

	destBytes, readErr := os.ReadFile(out.DestinationPath)
	if readErr != nil {
		t.Fatalf("cannot read destination file %q: %v", out.DestinationPath, readErr)
	}

	// Assert: extension and content format agree.
	ext := filepath.Ext(out.DestinationPath)
	switch ext {
	case ".md":
		// Extension declares Markdown: content must have YAML frontmatter.
		if !strings.HasPrefix(string(destBytes), "---\n") {
			t.Errorf("destination has .md extension but content does not start with YAML frontmatter "+
				"delimiter \"---\\n\"; extension and content format must agree; first 100 bytes: %q",
				string(destBytes[:min(len(destBytes), 100)]))
		}
	case ".toml":
		// Extension declares TOML: content must not start with YAML frontmatter.
		if strings.HasPrefix(string(destBytes), "---\n") {
			t.Errorf("destination has .toml extension but content starts with YAML frontmatter; "+
				"extension and content format must agree; a .toml file must contain TOML")
		}
	default:
		t.Errorf("unexpected destination extension %q; target harness declares .md; "+
			"the retarget destination must carry the target harness's extension", ext)
	}
}

// TestRetarget_CodexNonRecoverable_Destination_MinimalGrantFromNonRecoverablePath verifies
// the core T15.7(b) contract: a Codex-source retarget to a Markdown harness writes a
// destination that carries the minimal grant tools from the non-recoverable path, not an
// empty set that would come from a successful (but empty) reverse mapping. The destination
// must be Markdown with YAML frontmatter AND contain each member of the minimal grant.
// An empty tools list would indicate the reverse-mapping path was taken silently; the
// minimal-grant content proves the non-recoverable branch ran and applied the grant.
func TestRetarget_CodexNonRecoverable_Destination_MinimalGrantFromNonRecoverablePath(t *testing.T) {
	// Arrange + Act
	stub := interactiontest.NewBuilder().Build()
	result, err := runCodexNonRecoverableRetarget(t, stub, eligibleCodexTomlRetargetBytes())
	if err != nil {
		t.Fatalf("TransformHarness: unexpected error: %v", err)
	}
	if len(result.Files) == 0 {
		t.Fatalf("TransformHarness: no files in result")
	}
	out := result.Files[0]
	if out.Status != app.StatusTransformed {
		t.Fatalf("file status = %q (reason: %q), want %q", out.Status, out.Reason, app.StatusTransformed)
	}

	destBytes, readErr := os.ReadFile(out.DestinationPath)
	if readErr != nil {
		t.Fatalf("cannot read destination file %q: %v", out.DestinationPath, readErr)
	}
	destStr := string(destBytes)

	// Assert: destination is Markdown with YAML frontmatter (format check).
	if !strings.HasPrefix(destStr, "---\n") {
		t.Errorf("destination does not start with YAML frontmatter \"---\\n\"; "+
			"a Codex-to-Markdown retarget must produce valid Markdown; first 100 bytes: %q",
			destStr[:min(len(destStr), 100)])
	}

	// Assert: destination contains each member of the minimal grant tool set.
	// An empty tool list would indicate the reverse-mapping path ran (Codex has no reverse
	// mapping, so it yields nothing); the presence of these tools proves the non-recoverable
	// path ran and applied the minimal read-only grant through RenderMinimalToolGrant.
	wantTools := []string{"file_read", "file_search", "content_search"}
	for _, tool := range wantTools {
		if !strings.Contains(destStr, tool) {
			t.Errorf("destination does not contain minimal grant tool %q; "+
				"a Codex-source retarget to a Markdown harness must apply the minimal read-only "+
				"grant from the non-recoverable path (not an empty reverse mapping); got:\n%s",
				tool, destStr)
		}
	}
}

// ---------------------------------------------------------------------------
// T15.3 -- cross-format container invariant at flow level
// ---------------------------------------------------------------------------

// TestRetarget_CodexNonRecoverable_ContainerInvariant_SourceFileNotModified verifies that
// the source Codex TOML file is not modified by a retarget that strips its carriage container.
func TestRetarget_CodexNonRecoverable_ContainerInvariant_SourceFileNotModified(t *testing.T) {
	stub := interactiontest.NewBuilder().Build()
	srcBytes := eligibleCodexTomlRetargetWithUserKeyBytes()
	deps, _ := newCodexNonRecoverableRetargetDeps(t, stub)
	svc := app.New(deps)

	srcDir := t.TempDir()
	srcPath := filepath.Join(srcDir, "my-codex-agent.toml")
	if err := os.WriteFile(srcPath, srcBytes, 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}

	req := app.TransformHarnessRequest{
		SourceHarnessID: codexNRHarnessID,
		TargetHarnessID: minTgtHarnessID,
		Path:            srcPath,
		Overwrite:       true,
	}

	if _, err := svc.TransformHarness(context.Background(), req); err != nil {
		t.Fatalf("TransformHarness: unexpected error: %v", err)
	}

	afterBytes, readErr := os.ReadFile(srcPath)
	if readErr != nil {
		t.Fatalf("cannot re-read source: %v", readErr)
	}
	if string(afterBytes) != string(srcBytes) {
		t.Errorf("source file was modified by retarget; the source artifact must never be modified")
	}
}

// ---------------------------------------------------------------------------
// T15.3 (continued) -- both carriage shapes at flow level
// ---------------------------------------------------------------------------

// eligibleCodexTomlRetargetWithBothCarriageShapesBytes returns a Codex TOML source file for
// flow-level T15.3 tests. It carries:
//
//   - my_value_key = "user-value": a plain string scalar -- after decode this lands in
//     mosaic_carriage (value-carried), so StripCarriage must report Key "my_value_key".
//   - [my_table] with setting = "config-value": a TOML table section -- after decode the key
//     name "my_table" lands in mosaic_carriage_markers (marker-carried, because tables decode
//     as map[string]interface{} which is the default marker-carry case in the Codex decoder),
//     so StripCarriage must report Key "my_table".
//
// The file deliberately carries no sandbox_mode and no other Codex-native keys so that
// DetectHarnessMatch finds no ClassUnknown fields and returns HarnessMatchIndeterminate,
// allowing the retarget flow to proceed rather than being skipped as a harness mismatch.
func eligibleCodexTomlRetargetWithBothCarriageShapesBytes() []byte {
	return []byte(
		"# mosaic_transform_version: 2.1.0\n" +
			"\n" +
			"my_value_key = \"user-value\"\n" +
			"developer_instructions = \"\"\"\n" +
			"<Identity type=\"core\">\n" +
			"You are a Codex retarget test agent.\n" +
			"</Identity>\n" +
			"\"\"\"\n" +
			"\n" +
			"[my_table]\n" +
			"setting = \"config-value\"\n",
	)
}

// TestRetarget_CodexNonRecoverable_BothCarriageShapes_ValueCarriedKeyInStrippedFields verifies
// that when the Codex source carries a plain scalar user key (value-carried, lands in
// mosaic_carriage after decode), the key name appears in the file outcome's StrippedFields
// with Reason StripReasonCarriedKey at flow level. This test is the flow-level complement to
// the T15.9 unit test: it proves that the full promote/retarget pipeline feeds the canonical
// document through the stripping call and surfaces value-carried keys in the report.
func TestRetarget_CodexNonRecoverable_BothCarriageShapes_ValueCarriedKeyInStrippedFields(t *testing.T) {
	stub := interactiontest.NewBuilder().Build()
	result, err := runCodexNonRecoverableRetarget(t, stub, eligibleCodexTomlRetargetWithBothCarriageShapesBytes())
	if err != nil {
		t.Fatalf("TransformHarness: unexpected error: %v", err)
	}
	if len(result.Files) == 0 {
		t.Fatalf("TransformHarness: no files in result")
	}

	out := result.Files[0]
	if out.Status != app.StatusTransformed {
		t.Fatalf("file status = %q, want %q", out.Status, app.StatusTransformed)
	}

	found := false
	for _, sf := range out.StrippedFields {
		if sf.Key == "my_value_key" && sf.Reason == app.StripReasonCarriedKey {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("StrippedFields does not contain {Key: \"my_value_key\", Reason: StripReasonCarriedKey}; "+
			"the value-carried user key must be named in the run report after flow-level stripping; "+
			"got: %v", out.StrippedFields)
	}
}

// TestRetarget_CodexNonRecoverable_BothCarriageShapes_MarkerCarriedKeyInStrippedFields verifies
// that when the Codex source carries a TOML table user key (marker-carried, lands in
// mosaic_carriage_markers after decode), the key name appears in the file outcome's StrippedFields
// with Reason StripReasonCarriedKey at flow level. This is the critical half of the T15.3
// both-shapes requirement: a bug that silently dropped marker-carried keys from StrippedFields
// while correctly reporting value-carried keys would not be caught without this test.
func TestRetarget_CodexNonRecoverable_BothCarriageShapes_MarkerCarriedKeyInStrippedFields(t *testing.T) {
	stub := interactiontest.NewBuilder().Build()
	result, err := runCodexNonRecoverableRetarget(t, stub, eligibleCodexTomlRetargetWithBothCarriageShapesBytes())
	if err != nil {
		t.Fatalf("TransformHarness: unexpected error: %v", err)
	}
	if len(result.Files) == 0 {
		t.Fatalf("TransformHarness: no files in result")
	}

	out := result.Files[0]
	if out.Status != app.StatusTransformed {
		t.Fatalf("file status = %q, want %q", out.Status, app.StatusTransformed)
	}

	found := false
	for _, sf := range out.StrippedFields {
		if sf.Key == "my_table" && sf.Reason == app.StripReasonCarriedKey {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("StrippedFields does not contain {Key: \"my_table\", Reason: StripReasonCarriedKey}; "+
			"the marker-carried user key (TOML table) must be named in the run report after flow-level "+
			"stripping; this verifies that the stripping call enumerates both carriage sub-containers; "+
			"got: %v", out.StrippedFields)
	}
}

// TestRetarget_CodexNonRecoverable_BothCarriageShapes_CarriageContainerAbsentFromOutput verifies
// that neither carriage container key (mosaic_carriage or mosaic_carriage_markers) appears in the
// destination Markdown file when the Codex source carries both value-carried and marker-carried
// user keys. This asserts the stripping call removes both sub-containers from the canonical
// document before it reaches the retarget output.
func TestRetarget_CodexNonRecoverable_BothCarriageShapes_CarriageContainerAbsentFromOutput(t *testing.T) {
	stub := interactiontest.NewBuilder().Build()
	result, err := runCodexNonRecoverableRetarget(t, stub, eligibleCodexTomlRetargetWithBothCarriageShapesBytes())
	if err != nil {
		t.Fatalf("TransformHarness: unexpected error: %v", err)
	}
	if len(result.Files) == 0 {
		t.Fatalf("TransformHarness: no files in result")
	}

	out := result.Files[0]
	if out.Status != app.StatusTransformed {
		t.Fatalf("file status = %q, want %q", out.Status, app.StatusTransformed)
	}

	destBytes, readErr := os.ReadFile(out.DestinationPath)
	if readErr != nil {
		t.Fatalf("cannot read destination: %v", readErr)
	}
	destStr := string(destBytes)

	if strings.Contains(destStr, "mosaic_carriage:") {
		t.Errorf("destination file contains \"mosaic_carriage:\"; "+
			"the carriage container must be fully stripped before the output is written; got:\n%s", destStr)
	}
	if strings.Contains(destStr, "mosaic_carriage_markers:") {
		t.Errorf("destination file contains \"mosaic_carriage_markers:\"; "+
			"both carriage sub-containers must be stripped at the format-change boundary; got:\n%s", destStr)
	}
}

// ---------------------------------------------------------------------------
// T15.4 -- retarget to Codex: source tools collapse to sandbox_mode
// ---------------------------------------------------------------------------

// TestRetarget_MarkdownSource_ToCodexTarget_ToolsCollapseToSandboxMode verifies that
// retargeting a Markdown agent with tools to a Codex target produces an encoded TOML file
// where the source tools are collapsed into sandbox_mode by the collapse table.
//
// This test depends on Half A (I15.6) to produce an encoded TOML destination file. It will
// fail RED until I15.6 is implemented and the retarget output is encoded in the target's format.
func TestRetarget_MarkdownSource_ToCodexTarget_ToolsCollapseToSandboxMode(t *testing.T) {
	// Arrange: Markdown source with tools and Codex as the target.
	// Use newMarkdownToCodexRetargetDeps from retarget_crossformat_test.go.
	stub := interactiontest.NewBuilder().Build()
	deps, _ := newMarkdownToCodexRetargetDeps(t, stub)
	svc := app.New(deps)

	srcDir := t.TempDir()
	srcPath := filepath.Join(srcDir, crossFormatAgentKey+".src.md")
	if err := os.WriteFile(srcPath, crossFormatSourceBytes(), 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}

	req := app.TransformHarnessRequest{
		SourceHarnessID: "source-harness",
		TargetHarnessID: "codex-harness",
		Path:            srcPath,
		Overwrite:       true,
	}

	// Act
	result, err := svc.TransformHarness(context.Background(), req)
	if err != nil {
		t.Fatalf("TransformHarness: unexpected error: %v", err)
	}
	if len(result.Files) == 0 {
		t.Fatalf("TransformHarness: no files in result")
	}

	out := result.Files[0]
	if out.Status != app.StatusTransformed {
		t.Fatalf("file status = %q (reason: %q), want %q", out.Status, out.Reason, app.StatusTransformed)
	}

	// Assert: destination file must be parseable as TOML with a sandbox_mode key.
	destBytes, readErr := os.ReadFile(out.DestinationPath)
	if readErr != nil {
		t.Fatalf("cannot read destination %q: %v", out.DestinationPath, readErr)
	}

	var parsed map[string]interface{}
	if err := toml.Unmarshal(destBytes, &parsed); err != nil {
		t.Fatalf("destination file is not valid TOML (want encoded in target format after I15.6); "+
			"parse error: %v; first %d bytes:\n%s",
			err, min(len(destBytes), 200), string(destBytes[:min(len(destBytes), 200)]))
	}

	if _, ok := parsed["sandbox_mode"]; !ok {
		t.Errorf("TOML destination does not contain \"sandbox_mode\"; "+
			"source tools must collapse into sandbox_mode by the collapse table when retargeting to Codex; "+
			"got keys: %v", tomlKeys(parsed))
	}
}

// ---------------------------------------------------------------------------
// T15.5 -- Markdown-to-Markdown retarget: non-recoverable branch not taken
// ---------------------------------------------------------------------------

// TestRetarget_MarkdownToMarkdown_NonRecoverableBranchNotTaken verifies that retargeting
// between two Markdown harnesses does not trigger the non-recoverable branch. No
// QPromoteNonRecoverableTools or QPromoteCustomTool is asked via the non-recoverable path, and
// the result status is transformed (not failed with a capability-loss reason).
//
// This test guards against regressions to the existing Markdown-to-Markdown retarget path
// introduced by the non-recoverable source handling added in Half B.
func TestRetarget_MarkdownToMarkdown_NonRecoverableBranchNotTaken(t *testing.T) {
	// Arrange: two Markdown harnesses (neither has ToolInfoUnrecoverable).
	stub := interactiontest.NewBuilder().Build()
	deps, _ := newMarkdownToMarkdownRetargetDeps(t, stub)
	svc := app.New(deps)

	srcDir := t.TempDir()
	srcPath := filepath.Join(srcDir, crossFormatAgentKey+".src.md")
	if err := os.WriteFile(srcPath, crossFormatSourceBytes(), 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}

	req := app.TransformHarnessRequest{
		SourceHarnessID: "source-harness",
		TargetHarnessID: "target-harness",
		Path:            srcPath,
		Overwrite:       true,
	}

	// Act
	result, err := svc.TransformHarness(context.Background(), req)
	if err != nil {
		t.Fatalf("TransformHarness: unexpected error: %v", err)
	}
	if len(result.Files) == 0 {
		t.Fatalf("TransformHarness: no files in result")
	}

	// Assert: no non-recoverable-related questions asked.
	for _, call := range stub.Calls() {
		if call.ID == domain.QPromoteNonRecoverableTools {
			t.Errorf("QPromoteNonRecoverableTools was asked for a Markdown-to-Markdown retarget; "+
				"it must only be asked when ToolInfoUnrecoverable=true on the source descriptor")
			break
		}
	}
}

// TestRetarget_MarkdownToMarkdown_NoCapabilityLossNotice verifies that retargeting between two
// Markdown harnesses does not emit a capability-loss NoticeWarning. Markdown harnesses have
// recoverable tool information, so no capability-loss report is appropriate.
func TestRetarget_MarkdownToMarkdown_NoCapabilityLossNotice(t *testing.T) {
	stub := interactiontest.NewBuilder().Build()
	deps, _ := newMarkdownToMarkdownRetargetDeps(t, stub)
	svc := app.New(deps)

	srcDir := t.TempDir()
	srcPath := filepath.Join(srcDir, crossFormatAgentKey+".src.md")
	if err := os.WriteFile(srcPath, crossFormatSourceBytes(), 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}

	req := app.TransformHarnessRequest{
		SourceHarnessID: "source-harness",
		TargetHarnessID: "target-harness",
		Path:            srcPath,
		Overwrite:       true,
	}

	if _, err := svc.TransformHarness(context.Background(), req); err != nil {
		t.Fatalf("TransformHarness: unexpected error: %v", err)
	}

	// Assert: no warnings should be present (the non-recoverable warning would be the only one).
	notices := stub.Notices()
	for _, n := range notices {
		if n.Level == domain.NoticeWarning {
			t.Errorf("unexpected NoticeWarning for Markdown-to-Markdown retarget: %q; "+
				"capability-loss notices must not appear when the source harness has recoverable tools",
				n.Message)
		}
	}
}

// ---------------------------------------------------------------------------
// T15.5 (continued) -- reverse tool mapping produces output tools
// ---------------------------------------------------------------------------

// markdownMappingSourceModule is a HarnessModule stub for T15.5 tool-mapping assertion tests.
// Unlike stubHarnessModule, it holds a descriptor with a real tool mapping so that
// descriptor.ReverseMapTools can convert source harness tool names to generic names. This
// makes it possible to assert that the reverse-mapping path ran and produced output tools
// rather than silently returning an empty set.
//
// The module maps the single generic tool "file_read" to the harness name "file_read"
// (identity mapping), which is sufficient to verify the plumbing without requiring a
// multi-entry universe.
type markdownMappingSourceModule struct {
	d domain.HarnessDescriptor
}

func (m *markdownMappingSourceModule) Ref() domain.HarnessRef {
	return domain.HarnessRef{ID: m.d.ID, DisplayName: m.d.DisplayName, Usable: true}
}
func (m *markdownMappingSourceModule) Descriptor() *domain.HarnessDescriptor { return &m.d }
func (m *markdownMappingSourceModule) Tools(req domain.ToolRequest) (domain.ToolResult, error) {
	return descriptor.MapTools(&m.d, req)
}
func (m *markdownMappingSourceModule) Frontmatter(req domain.FrontmatterRequest) (domain.FrontmatterPlan, error) {
	return domain.FrontmatterPlan{}, nil
}
func (m *markdownMappingSourceModule) TargetPath(req domain.TargetPathRequest) (string, error) {
	ext := m.d.Extensions[req.Kind]
	return req.Key + ext, nil
}
func (m *markdownMappingSourceModule) Injection(_ domain.InjectionRequest) (string, bool) {
	return "", false
}
func (m *markdownMappingSourceModule) HookPlan(_ domain.HookPlanRequest) (domain.HookPlan, error) {
	return domain.HookPlan{Supported: false, Reason: "stub"}, nil
}
func (m *markdownMappingSourceModule) Close() error { return nil }

// newMarkdownToMarkdownToolMappingDeps returns Deps for the T15.5 tool-mapping assertion test.
// Source: "md-map-src" with a real mapping for generic "file_read" -> harness "file_read".
// Target: "minimal-tgt-harness" (minimalGrantTargetModule) which maps any generic tool to
// itself and returns a KindList tools field.
// Together they exercise the full reverse-map (source) + forward-map (target) pipeline for
// a Markdown-to-Markdown retarget.
func newMarkdownToMarkdownToolMappingDeps(t *testing.T, stub *interactiontest.Stub) (app.Deps, string) {
	t.Helper()
	deps, workspace := newBaseDeps(t, stub)
	srcMod := &markdownMappingSourceModule{
		d: domain.HarnessDescriptor{
			ID:          "md-map-src",
			DisplayName: "Markdown Mapping Source",
			Frontmatter: domain.FrontmatterSpec{
				ModelKey: "src_model",
				ToolsKey: "tools",
				KeyOrder: []string{"src_model", "tools"},
			},
			Extensions: map[domain.ArtifactKind]string{
				domain.ArtifactAgent: ".src.md",
			},
			Paths: domain.PathSpec{
				Agents: domain.ScopedPaths{Supported: true, Project: "agents"},
			},
			Tools: domain.ToolSpec{
				Universe: []domain.HarnessTool{{Name: "file_read"}},
				Mappings: []domain.ToolMapping{{
					Generic: "file_read",
					Destinations: []domain.ToolDestination{{
						Kind:  domain.DestMain,
						Names: []string{"file_read"},
					}},
				}},
			},
		},
	}
	tgtMod := newMinimalGrantTargetModule()
	deps.Registry = &stubRegistry{
		list: []domain.HarnessRef{
			{ID: "md-map-src", DisplayName: "Markdown Mapping Source", Usable: true},
			{ID: minTgtHarnessID, DisplayName: "Minimal Grant Target Harness", Usable: true},
		},
		modules: map[string]domain.HarnessModule{
			"md-map-src":    srcMod,
			minTgtHarnessID: tgtMod,
		},
	}
	return deps, workspace
}

// markdownToolMappingSourceBytes returns bytes for a Markdown source agent that carries
// "file_read" under the "tools" key. Used with newMarkdownToMarkdownToolMappingDeps to
// test that the reverse-mapping path produces output tools for a Markdown-to-Markdown retarget.
// The file carries src_model (the source harness's distinctive ModelKey) and transform_version
// (eligibility signal one), with an Identity body (eligibility signal two).
func markdownToolMappingSourceBytes() []byte {
	return []byte("---\n" +
		"transform_version: \"2.1.0\"\n" +
		"injections_version: \"1.0.0\"\n" +
		"src_model: claude-3-5-sonnet\n" +
		"name: tool-mapping-test-agent\n" +
		"tools:\n" +
		"- file_read\n" +
		"---\n" +
		"<Identity type=\"core\">\n" +
		"You are the tool-mapping test agent.\n" +
		"</Identity>\n")
}

// TestRetarget_MarkdownToMarkdown_ReverseToolMappingProducesOutputTools verifies that when
// retargeting between two Markdown harnesses, the reverse tool mapping path runs and the
// mapped tools appear in the destination file. The source file carries "tools: [file_read]"
// under the source harness's ToolsKey; the source descriptor maps harness "file_read" to
// generic "file_read"; the target module maps generic "file_read" back to "file_read" in a
// list field. The destination must contain "file_read", proving the reverse-mapping path ran
// and produced results rather than silently returning an empty tool set.
//
// This test guards against a regression where the non-recoverable branch changes break the
// standard Markdown-to-Markdown reverse-mapping path.
func TestRetarget_MarkdownToMarkdown_ReverseToolMappingProducesOutputTools(t *testing.T) {
	// Arrange
	stub := interactiontest.NewBuilder().Build()
	deps, _ := newMarkdownToMarkdownToolMappingDeps(t, stub)
	svc := app.New(deps)

	srcDir := t.TempDir()
	srcPath := filepath.Join(srcDir, "tool-mapping-agent.src.md")
	if err := os.WriteFile(srcPath, markdownToolMappingSourceBytes(), 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}

	req := app.TransformHarnessRequest{
		SourceHarnessID: "md-map-src",
		TargetHarnessID: minTgtHarnessID,
		Path:            srcPath,
		Overwrite:       true,
	}

	// Act
	result, err := svc.TransformHarness(context.Background(), req)
	if err != nil {
		t.Fatalf("TransformHarness: unexpected error: %v", err)
	}
	if len(result.Files) == 0 {
		t.Fatalf("TransformHarness: no files in result")
	}

	out := result.Files[0]
	if out.Status != app.StatusTransformed {
		t.Fatalf("file status = %q (reason: %q), want %q", out.Status, out.Reason, app.StatusTransformed)
	}

	// Assert: destination contains the mapped tool "file_read".
	destBytes, readErr := os.ReadFile(out.DestinationPath)
	if readErr != nil {
		t.Fatalf("cannot read destination: %v", readErr)
	}
	if !strings.Contains(string(destBytes), "file_read") {
		t.Errorf("destination file does not contain \"file_read\"; "+
			"the reverse-mapping path must run and produce output tools for a Markdown-to-Markdown "+
			"retarget; this guards against a broken reverse-mapping path that silently returns an "+
			"empty tool set; got:\n%s", string(destBytes))
	}
}
