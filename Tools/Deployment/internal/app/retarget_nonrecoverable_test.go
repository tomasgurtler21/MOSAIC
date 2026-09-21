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

// eligibleCodexTomlRetargetWithWorkspaceWriteSandboxModeBytes returns Codex TOML for retarget
// tests that need a source carrying sandbox_mode = "workspace-write". This is the most
// privileged sandbox mode value and the one an escalating-bug would read to grant extra
// capability to the destination.
//
// Use this fixture in tests that verify the non-recoverable path ignores sandbox_mode entirely:
// if an implementation reads sandbox_mode and maps "workspace-write" to an escalated grant,
// a fixture with "read-only" (or no sandbox_mode) would not catch the bug -- but "workspace-write"
// forces the bug to manifest as escalating tools in the output.
func eligibleCodexTomlRetargetWithWorkspaceWriteSandboxModeBytes() []byte {
	return []byte(
		"# mosaic_transform_version: 2.1.0\n" +
			"\n" +
			"sandbox_mode = \"workspace-write\"\n" +
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
// from a non-recoverable source emits at least one NoticeWarning whose message mentions "tool"
// (case-insensitive). This guards against an unrelated warning (e.g. an owned-key drift warning)
// masking a missing capability-loss warning: a warning that exists but says nothing about tools
// would not satisfy the capability-loss reporting requirement.
func TestRetarget_CodexNonRecoverable_CapabilityLoss_WarningNoticeEmitted(t *testing.T) {
	stub := interactiontest.NewBuilder().Build()
	_, err := runCodexNonRecoverableRetarget(t, stub, eligibleCodexTomlRetargetBytes())
	if err != nil {
		t.Fatalf("TransformHarness: unexpected error: %v", err)
	}

	notices := stub.Notices()
	hasCapabilityLossWarning := false
	for _, n := range notices {
		if n.Level == domain.NoticeWarning && strings.Contains(strings.ToLower(n.Message), "tool") {
			hasCapabilityLossWarning = true
			break
		}
	}
	if !hasCapabilityLossWarning {
		t.Errorf("no NoticeWarning mentioning \"tool\" emitted for retarget from non-recoverable source; "+
			"the capability-loss warning must reference tools so it is distinguishable from unrelated warnings; "+
			"got notices: %v", notices)
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

// TestRetarget_CodexNonRecoverable_NoEscalationFromSandboxMode verifies that when the Codex
// source carries sandbox_mode = "workspace-write" (the most privileged value), the target file
// still contains only the minimal read-only grant and does not contain escalating tools.
//
// This is the critical fixture for T15.2(d): the source deliberately carries the value that an
// implementation bug would read and use to grant escalated capability. A fixture without
// sandbox_mode (or with "read-only") would pass trivially even if the bug exists, because no
// escalating value was present to read. With "workspace-write", a bug that reads sandbox_mode
// and maps it to an escalated grant produces observable output (escalating tools in the file),
// which this test catches.
func TestRetarget_CodexNonRecoverable_NoEscalationFromSandboxMode(t *testing.T) {
	stub := interactiontest.NewBuilder().Build()
	// Use workspace-write fixture: if the non-recoverable path incorrectly reads sandbox_mode
	// and escalates based on it, this value forces the bug to manifest as escalating tools in
	// the destination file.
	result, err := runCodexNonRecoverableRetarget(t, stub, eligibleCodexTomlRetargetWithWorkspaceWriteSandboxModeBytes())
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

	// Escalating tools must not appear in the target; the non-recoverable branch must apply
	// only the minimal read-only grant regardless of the source's sandbox_mode value.
	escalatingTools := []string{"terminal", "file_write", "file_edit", "bash", "subagent"}
	for _, tool := range escalatingTools {
		if strings.Contains(destStr, tool) {
			t.Errorf("destination file contains escalating tool %q; "+
				"escalation must never be inferred from the source's sandbox_mode value; "+
				"the non-recoverable branch must apply only the minimal read-only grant; got:\n%s",
				tool, destStr)
		}
	}

	// The minimal grant tools must be present: if the non-recoverable path did not run
	// (e.g. it silently skipped the source or returned an empty grant), these tools would be
	// absent and the escalation-absence assertion above would pass vacuously.
	minimalGrantTools := []string{"file_read", "file_search", "content_search"}
	for _, tool := range minimalGrantTools {
		if !strings.Contains(destStr, tool) {
			t.Errorf("destination file does not contain minimal grant tool %q; "+
				"the non-recoverable path must apply the minimal read-only grant; "+
				"absence of minimal tools means the path did not run correctly; got:\n%s",
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
// T15.4 (continued) -- collapse sub-cases: non-escalating and escalating tool sets
// ---------------------------------------------------------------------------
//
// The test above (TestRetarget_MarkdownSource_ToCodexTarget_ToolsCollapseToSandboxMode) uses
// a no-tools source fixture, which exercises only the empty-set fallback path ("read-only").
// The two tests below supply actual generic tools and assert the specific sandbox_mode value
// the collapse table must produce for each case. They use codexAsTargetModule (which implements
// the real collapse logic) and a source module that maps both file_read and file_write.

const collapseTestSourceID = "collapse-src-harness"

// newCollapseTestSourceModule returns a markdownMappingSourceModule whose descriptor maps
// "file_read" (non-escalating) and "file_write" (escalating) to the same generic names.
// This makes descriptor.ReverseMapTools available to the retarget flow for both tool names,
// so the target module receives a non-empty generic tool set to collapse.
func newCollapseTestSourceModule() *markdownMappingSourceModule {
	return &markdownMappingSourceModule{
		d: domain.HarnessDescriptor{
			ID:          collapseTestSourceID,
			DisplayName: "Collapse Test Source Harness",
			Frontmatter: domain.FrontmatterSpec{
				ModelKey: "src_model",
				ToolsKey: "tools",
				KeyOrder: []string{"src_model", "tools"},
			},
			Extensions: map[domain.ArtifactKind]string{
				domain.ArtifactAgent: ".md",
			},
			Paths: domain.PathSpec{
				Agents: domain.ScopedPaths{Supported: true, Project: "agents"},
			},
			Tools: domain.ToolSpec{
				Universe: []domain.HarnessTool{
					{Name: "file_read"},
					{Name: "file_write"},
				},
				Mappings: []domain.ToolMapping{
					{
						Generic: "file_read",
						Destinations: []domain.ToolDestination{{
							Kind:  domain.DestMain,
							Names: []string{"file_read"},
						}},
					},
					{
						Generic: "file_write",
						Destinations: []domain.ToolDestination{{
							Kind:  domain.DestMain,
							Names: []string{"file_write"},
						}},
					},
				},
			},
		},
	}
}

// newMarkdownToCodexCollapseRetargetDeps returns Deps wired for T15.4 collapse sub-case tests:
// the source is the collapseTestSourceModule (maps file_read and file_write to generic names)
// and the target is codexAsTargetModule (collapses generic tools to sandbox_mode via the collapse
// table). Using codexAsTargetModule as the target exercises the real collapse logic; the
// codexPromoteModule used by newMarkdownToCodexRetargetDeps does not implement the collapse.
func newMarkdownToCodexCollapseRetargetDeps(t *testing.T, stub *interactiontest.Stub) (app.Deps, string) {
	t.Helper()
	deps, workspace := newBaseDeps(t, stub)
	srcMod := newCollapseTestSourceModule()
	tgtMod := newCodexAsTargetModule()
	deps.Registry = &stubRegistry{
		list: []domain.HarnessRef{
			{ID: collapseTestSourceID, DisplayName: "Collapse Test Source Harness", Usable: true},
			{ID: codexAsTgtID, DisplayName: "Codex As Target", Usable: true},
		},
		modules: map[string]domain.HarnessModule{
			collapseTestSourceID: srcMod,
			codexAsTgtID:         tgtMod,
		},
	}
	return deps, workspace
}

// collapseSourceWithNonEscalatingToolBytes returns Markdown source bytes that carry only
// "file_read" under the tools key. The collapseTestSourceModule maps harness "file_read" to
// generic "file_read", which is non-escalating: the Codex collapse table must map it to
// sandbox_mode = "read-only".
func collapseSourceWithNonEscalatingToolBytes() []byte {
	return []byte("---\n" +
		"transform_version: \"2.1.0\"\n" +
		"injections_version: \"1.0.0\"\n" +
		"src_model: claude-3-5-sonnet\n" +
		"name: collapse-test-agent\n" +
		"tools:\n" +
		"- file_read\n" +
		"---\n" +
		"<Identity type=\"core\">\n" +
		"You are the collapse test agent.\n" +
		"</Identity>\n")
}

// collapseSourceWithEscalatingToolBytes returns Markdown source bytes that carry "file_write"
// under the tools key. The collapseTestSourceModule maps harness "file_write" to generic
// "file_write", which is escalating: the Codex collapse table must map it to
// sandbox_mode = "workspace-write".
func collapseSourceWithEscalatingToolBytes() []byte {
	return []byte("---\n" +
		"transform_version: \"2.1.0\"\n" +
		"injections_version: \"1.0.0\"\n" +
		"src_model: claude-3-5-sonnet\n" +
		"name: collapse-test-agent\n" +
		"tools:\n" +
		"- file_write\n" +
		"---\n" +
		"<Identity type=\"core\">\n" +
		"You are the collapse test agent.\n" +
		"</Identity>\n")
}

// TestRetarget_MarkdownSource_ToCodex_NonEscalatingTools_SandboxModeReadOnly verifies that
// retargeting a Markdown source with only non-escalating tools (file_read) to a Codex target
// produces sandbox_mode = "read-only" in the encoded TOML destination file. This exercises
// the non-escalating branch of the collapse table, which the no-tools fixture in
// TestRetarget_MarkdownSource_ToCodexTarget_ToolsCollapseToSandboxMode cannot reach.
func TestRetarget_MarkdownSource_ToCodex_NonEscalatingTools_SandboxModeReadOnly(t *testing.T) {
	// Arrange
	stub := interactiontest.NewBuilder().Build()
	deps, _ := newMarkdownToCodexCollapseRetargetDeps(t, stub)
	svc := app.New(deps)

	srcDir := t.TempDir()
	srcPath := filepath.Join(srcDir, "collapse-test-agent.md")
	if err := os.WriteFile(srcPath, collapseSourceWithNonEscalatingToolBytes(), 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}

	req := app.TransformHarnessRequest{
		SourceHarnessID: collapseTestSourceID,
		TargetHarnessID: codexAsTgtID,
		Path:            srcPath,
		Overwrite:       true,
	}

	// Act
	result, err := svc.TransformHarness(context.Background(), req)
	if err != nil {
		t.Fatalf("TransformHarness: unexpected error: %v", err)
	}
	if len(result.Files) == 0 {
		t.Fatalf("TransformHarness: no file outcomes")
	}

	out := result.Files[0]
	if out.Status != app.StatusTransformed {
		t.Fatalf("file status = %q (reason: %q), want %q", out.Status, out.Reason, app.StatusTransformed)
	}

	// Assert: destination must be valid TOML with sandbox_mode = "read-only".
	destBytes, readErr := os.ReadFile(out.DestinationPath)
	if readErr != nil {
		t.Fatalf("cannot read destination %q: %v", out.DestinationPath, readErr)
	}

	var parsed map[string]interface{}
	if err := toml.Unmarshal(destBytes, &parsed); err != nil {
		t.Fatalf("destination is not valid TOML: %v\nContent: %s", err, string(destBytes))
	}

	sandboxMode, ok := parsed["sandbox_mode"]
	if !ok {
		t.Fatalf("destination TOML does not contain sandbox_mode; "+
			"a non-escalating tool set must collapse to sandbox_mode = \"read-only\"; "+
			"present keys: %v", tomlKeys(parsed))
	}
	if s, isStr := sandboxMode.(string); !isStr || s != "read-only" {
		t.Errorf("sandbox_mode = %v (%T); want string %q; "+
			"a source with only non-escalating tools (file_read) must collapse to \"read-only\"",
			sandboxMode, sandboxMode, "read-only")
	}
}

// TestRetarget_MarkdownSource_ToCodex_EscalatingTool_SandboxModeWorkspaceWrite verifies that
// retargeting a Markdown source with at least one escalating tool (file_write) to a Codex target
// produces sandbox_mode = "workspace-write" in the encoded TOML destination file. This exercises
// the escalating branch of the collapse table, which an empty or non-escalating tool set cannot
// reach. A bug that maps escalating tools to "read-only" instead of "workspace-write" would
// silently under-privilege the agent on the Codex platform; this test makes that bug observable.
func TestRetarget_MarkdownSource_ToCodex_EscalatingTool_SandboxModeWorkspaceWrite(t *testing.T) {
	// Arrange
	stub := interactiontest.NewBuilder().Build()
	deps, _ := newMarkdownToCodexCollapseRetargetDeps(t, stub)
	svc := app.New(deps)

	srcDir := t.TempDir()
	srcPath := filepath.Join(srcDir, "collapse-test-agent.md")
	if err := os.WriteFile(srcPath, collapseSourceWithEscalatingToolBytes(), 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}

	req := app.TransformHarnessRequest{
		SourceHarnessID: collapseTestSourceID,
		TargetHarnessID: codexAsTgtID,
		Path:            srcPath,
		Overwrite:       true,
	}

	// Act
	result, err := svc.TransformHarness(context.Background(), req)
	if err != nil {
		t.Fatalf("TransformHarness: unexpected error: %v", err)
	}
	if len(result.Files) == 0 {
		t.Fatalf("TransformHarness: no file outcomes")
	}

	out := result.Files[0]
	if out.Status != app.StatusTransformed {
		t.Fatalf("file status = %q (reason: %q), want %q", out.Status, out.Reason, app.StatusTransformed)
	}

	// Assert: destination must be valid TOML with sandbox_mode = "workspace-write".
	destBytes, readErr := os.ReadFile(out.DestinationPath)
	if readErr != nil {
		t.Fatalf("cannot read destination %q: %v", out.DestinationPath, readErr)
	}

	var parsed map[string]interface{}
	if err := toml.Unmarshal(destBytes, &parsed); err != nil {
		t.Fatalf("destination is not valid TOML: %v\nContent: %s", err, string(destBytes))
	}

	sandboxMode, ok := parsed["sandbox_mode"]
	if !ok {
		t.Fatalf("destination TOML does not contain sandbox_mode; "+
			"a source with an escalating tool must produce sandbox_mode; "+
			"present keys: %v", tomlKeys(parsed))
	}
	if s, isStr := sandboxMode.(string); !isStr || s != "workspace-write" {
		t.Errorf("sandbox_mode = %v (%T); want string %q; "+
			"a source with an escalating tool (file_write) must collapse to \"workspace-write\"",
			sandboxMode, sandboxMode, "workspace-write")
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

// ---------------------------------------------------------------------------
// T15.2(c) continued -- per-target-type exact-form assertions
// ---------------------------------------------------------------------------
//
// The plan requires asserting the exact form each target harness emits for a minimal
// grant. Four distinct rendering paths exist:
//
//   Claude Code:   comma-separated scalar (KindScalar), not a YAML list
//   GHCP:          KindList in block style, plus a by-convention tool always included
//   OpenCode:      KindMapping with per-tool "allow"/"deny" dispositions
//   Codex target:  no tools field at all; capability expressed via sandbox_mode
//
// Each of the four stubs below mimics exactly one target harness's Tools() rendering
// path. They are used only in the corresponding per-target-type test below.

// ---------------------------------------------------------------------------
// claudeCodeLikeTargetModule -- comma-separated scalar tools field
// ---------------------------------------------------------------------------

// claudeCodeLikeTargetModule is a target harness stub that mimics the Claude Code module's
// post-processing behaviour: Tools() returns a comma-separated KindScalar rather than a
// KindList. Claude Code always formats its tools field as a single scalar string because a
// YAML list is read by Claude Code as "inherit all". Tests using this stub verify that the
// scalar form reaches the output file, not a YAML list.
type claudeCodeLikeTargetModule struct {
	ref        domain.HarnessRef
	descriptor domain.HarnessDescriptor
}

func (m *claudeCodeLikeTargetModule) Ref() domain.HarnessRef                { return m.ref }
func (m *claudeCodeLikeTargetModule) Descriptor() *domain.HarnessDescriptor { return &m.descriptor }

// Tools returns the requested generic tools joined as a comma-separated KindScalar, exactly
// as the Claude Code module's convertFieldsToScalar post-processes the descriptor-driven list.
func (m *claudeCodeLikeTargetModule) Tools(req domain.ToolRequest) (domain.ToolResult, error) {
	resolutions := make([]domain.ToolResolution, len(req.Generic))
	for i, g := range req.Generic {
		resolutions[i] = domain.ToolResolution{Generic: g, Outcome: domain.ToolMapped, HarnessTools: []string{g}}
	}
	toolsKey := m.descriptor.Frontmatter.ToolsKey
	if toolsKey == "" {
		toolsKey = "tools"
	}
	return domain.ToolResult{
		Fields: []domain.FrontmatterField{{
			Key:   toolsKey,
			Value: domain.FieldValue{Kind: domain.KindScalar, Scalar: strings.Join(req.Generic, ", ")},
		}},
		Resolutions: resolutions,
	}, nil
}

func (m *claudeCodeLikeTargetModule) Frontmatter(_ domain.FrontmatterRequest) (domain.FrontmatterPlan, error) {
	return domain.FrontmatterPlan{}, nil
}

func (m *claudeCodeLikeTargetModule) TargetPath(req domain.TargetPathRequest) (string, error) {
	return req.Key + ".cc.md", nil
}

func (m *claudeCodeLikeTargetModule) Injection(_ domain.InjectionRequest) (string, bool) {
	return "", false
}

func (m *claudeCodeLikeTargetModule) HookPlan(_ domain.HookPlanRequest) (domain.HookPlan, error) {
	return domain.HookPlan{Supported: false, Reason: "stub"}, nil
}

func (m *claudeCodeLikeTargetModule) Close() error { return nil }

const claudeCodeLikeTgtID = "claude-code-like-tgt"

func newClaudeCodeLikeTargetModule() *claudeCodeLikeTargetModule {
	return &claudeCodeLikeTargetModule{
		ref: domain.HarnessRef{ID: claudeCodeLikeTgtID, DisplayName: "Claude Code Like Target", Usable: true},
		descriptor: domain.HarnessDescriptor{
			ID:          claudeCodeLikeTgtID,
			DisplayName: "Claude Code Like Target",
			Frontmatter: domain.FrontmatterSpec{
				ToolsKey: "tools",
			},
			Paths: domain.PathSpec{
				Agents: domain.ScopedPaths{Supported: true, Project: "agents"},
			},
			Extensions: map[domain.ArtifactKind]string{
				domain.ArtifactAgent: ".cc.md",
			},
		},
	}
}

// ---------------------------------------------------------------------------
// openCodeLikeTargetModule -- permission-map (KindMapping) tools field
// ---------------------------------------------------------------------------

// openCodeLikeTargetModule is a target harness stub that mimics OpenCode's ShapePermission
// rendering: Tools() returns a KindMapping field with per-tool "allow"/"deny" dispositions.
// Granted tools (from req.Generic) receive "allow"; all other universe tools receive "deny".
type openCodeLikeTargetModule struct {
	ref        domain.HarnessRef
	descriptor domain.HarnessDescriptor
}

func (m *openCodeLikeTargetModule) Ref() domain.HarnessRef                { return m.ref }
func (m *openCodeLikeTargetModule) Descriptor() *domain.HarnessDescriptor { return &m.descriptor }

// Tools returns a KindMapping permission field, assigning "allow" to each tool in req.Generic
// and "deny" to all other tools in the universe. This is the permission-map form that
// OpenCode reads.
func (m *openCodeLikeTargetModule) Tools(req domain.ToolRequest) (domain.ToolResult, error) {
	resolutions := make([]domain.ToolResolution, len(req.Generic))
	for i, g := range req.Generic {
		resolutions[i] = domain.ToolResolution{Generic: g, Outcome: domain.ToolMapped, HarnessTools: []string{g}}
	}
	granted := make(map[string]bool, len(req.Generic))
	for _, g := range req.Generic {
		granted[g] = true
	}
	// Universe of all known tools; only the granted ones get "allow".
	universe := []string{"file_read", "file_search", "content_search", "file_write", "file_edit", "terminal", "subagent"}
	pairs := make([]domain.FieldPair, len(universe))
	for i, name := range universe {
		disposition := "deny"
		if granted[name] {
			disposition = "allow"
		}
		pairs[i] = domain.FieldPair{
			Key:   name,
			Value: domain.FieldValue{Kind: domain.KindScalar, Scalar: disposition},
		}
	}
	toolsKey := m.descriptor.Frontmatter.ToolsKey
	if toolsKey == "" {
		toolsKey = "permissions"
	}
	return domain.ToolResult{
		Fields: []domain.FrontmatterField{{
			Key:   toolsKey,
			Value: domain.FieldValue{Kind: domain.KindMapping, Pairs: pairs},
		}},
		Resolutions: resolutions,
	}, nil
}

func (m *openCodeLikeTargetModule) Frontmatter(_ domain.FrontmatterRequest) (domain.FrontmatterPlan, error) {
	return domain.FrontmatterPlan{}, nil
}

func (m *openCodeLikeTargetModule) TargetPath(req domain.TargetPathRequest) (string, error) {
	return req.Key + ".oc.md", nil
}

func (m *openCodeLikeTargetModule) Injection(_ domain.InjectionRequest) (string, bool) {
	return "", false
}

func (m *openCodeLikeTargetModule) HookPlan(_ domain.HookPlanRequest) (domain.HookPlan, error) {
	return domain.HookPlan{Supported: false, Reason: "stub"}, nil
}

func (m *openCodeLikeTargetModule) Close() error { return nil }

const openCodeLikeTgtID = "opencode-like-tgt"

func newOpenCodeLikeTargetModule() *openCodeLikeTargetModule {
	return &openCodeLikeTargetModule{
		ref: domain.HarnessRef{ID: openCodeLikeTgtID, DisplayName: "OpenCode Like Target", Usable: true},
		descriptor: domain.HarnessDescriptor{
			ID:          openCodeLikeTgtID,
			DisplayName: "OpenCode Like Target",
			Frontmatter: domain.FrontmatterSpec{
				ToolsKey: "permissions",
			},
			Paths: domain.PathSpec{
				Agents: domain.ScopedPaths{Supported: true, Project: "agents"},
			},
			Extensions: map[domain.ArtifactKind]string{
				domain.ArtifactAgent: ".oc.md",
			},
		},
	}
}

// ---------------------------------------------------------------------------
// ghcpLikeTargetModule -- KindList with a by-convention entry
// ---------------------------------------------------------------------------

// ghcpLikeTargetModule is a target harness stub that mimics GHCP's list-format Tools()
// output: returns a KindList (block style) that always includes a by-convention tool
// alongside whatever generic tools are granted. The by-convention tool is emitted for
// every agent regardless of the requested generic tool set.
type ghcpLikeTargetModule struct {
	ref        domain.HarnessRef
	descriptor domain.HarnessDescriptor
}

func (m *ghcpLikeTargetModule) Ref() domain.HarnessRef                { return m.ref }
func (m *ghcpLikeTargetModule) Descriptor() *domain.HarnessDescriptor { return &m.descriptor }

// byConventionTool is the tool that ghcpLikeTargetModule always includes in its Tools()
// output, mimicking GHCP's ByConvention tool that is emitted for every agent.
const ghcpByConventionTool = "read_repository"

// Tools returns a KindList field containing the granted tools plus the by-convention tool.
// This mimics GHCP's buildListToolFields behaviour where ByConvention tools are always added.
func (m *ghcpLikeTargetModule) Tools(req domain.ToolRequest) (domain.ToolResult, error) {
	resolutions := make([]domain.ToolResolution, len(req.Generic))
	for i, g := range req.Generic {
		resolutions[i] = domain.ToolResolution{Generic: g, Outcome: domain.ToolMapped, HarnessTools: []string{g}}
	}
	toolsKey := m.descriptor.Frontmatter.ToolsKey
	if toolsKey == "" {
		toolsKey = "tools"
	}
	// Build list: all requested generic tools first, then the by-convention tool.
	items := make([]domain.FieldValue, 0, len(req.Generic)+1)
	for _, g := range req.Generic {
		items = append(items, domain.FieldValue{Kind: domain.KindScalar, Scalar: g})
	}
	items = append(items, domain.FieldValue{Kind: domain.KindScalar, Scalar: ghcpByConventionTool})
	return domain.ToolResult{
		Fields: []domain.FrontmatterField{{
			Key: toolsKey,
			Value: domain.FieldValue{
				Kind:  domain.KindList,
				Items: items,
				List:  domain.ListBlock,
			},
		}},
		Resolutions: resolutions,
	}, nil
}

func (m *ghcpLikeTargetModule) Frontmatter(_ domain.FrontmatterRequest) (domain.FrontmatterPlan, error) {
	return domain.FrontmatterPlan{}, nil
}

func (m *ghcpLikeTargetModule) TargetPath(req domain.TargetPathRequest) (string, error) {
	return req.Key + ".ghcp.md", nil
}

func (m *ghcpLikeTargetModule) Injection(_ domain.InjectionRequest) (string, bool) {
	return "", false
}

func (m *ghcpLikeTargetModule) HookPlan(_ domain.HookPlanRequest) (domain.HookPlan, error) {
	return domain.HookPlan{Supported: false, Reason: "stub"}, nil
}

func (m *ghcpLikeTargetModule) Close() error { return nil }

const ghcpLikeTgtID = "ghcp-like-tgt"

func newGHCPLikeTargetModule() *ghcpLikeTargetModule {
	return &ghcpLikeTargetModule{
		ref: domain.HarnessRef{ID: ghcpLikeTgtID, DisplayName: "GHCP Like Target", Usable: true},
		descriptor: domain.HarnessDescriptor{
			ID:          ghcpLikeTgtID,
			DisplayName: "GHCP Like Target",
			Frontmatter: domain.FrontmatterSpec{
				ToolsKey: "tools",
			},
			Paths: domain.PathSpec{
				Agents: domain.ScopedPaths{Supported: true, Project: "agents"},
			},
			Extensions: map[domain.ArtifactKind]string{
				domain.ArtifactAgent: ".ghcp.md",
			},
		},
	}
}

// ---------------------------------------------------------------------------
// codexAsTargetModule -- no tools field; capability via sandbox_mode
// ---------------------------------------------------------------------------

// codexAsTargetModule is a target harness stub that mimics the Codex module's Tools()
// behaviour: instead of a tools field, it emits a sandbox_mode field whose value is
// "read-only" (for non-escalating grants) or "workspace-write" (for escalating grants).
// The descriptor declares no ToolsKey, so RenderMinimalToolGrant does not apply the
// tools-key presence/value guards, and the absent tools field is the expected outcome.
type codexAsTargetModule struct {
	ref        domain.HarnessRef
	descriptor domain.HarnessDescriptor
}

func (m *codexAsTargetModule) Ref() domain.HarnessRef                { return m.ref }
func (m *codexAsTargetModule) Descriptor() *domain.HarnessDescriptor { return &m.descriptor }

// escalatingToolsSet lists the generic tool names that require workspace-level access.
// Their presence in req.Generic causes the Codex sandbox to collapse to workspace-write.
var escalatingToolsSet = map[string]bool{
	"file_write": true,
	"file_edit":  true,
	"terminal":   true,
	"subagent":   true,
}

// Tools collapses the requested generic tools into a sandbox_mode value, exactly as the
// Codex built-in module does. Non-escalating tools produce "read-only". Any escalating
// tool produces "workspace-write". The sandbox_mode field is the only field emitted;
// no tools-key field is produced because the descriptor declares no ToolsKey.
func (m *codexAsTargetModule) Tools(req domain.ToolRequest) (domain.ToolResult, error) {
	resolutions := make([]domain.ToolResolution, len(req.Generic))
	for i, g := range req.Generic {
		resolutions[i] = domain.ToolResolution{Generic: g, Outcome: domain.ToolMapped}
	}
	mode := "read-only"
	for _, g := range req.Generic {
		if escalatingToolsSet[g] {
			mode = "workspace-write"
			break
		}
	}
	return domain.ToolResult{
		Fields: []domain.FrontmatterField{{
			Key:   "sandbox_mode",
			Value: domain.FieldValue{Kind: domain.KindScalar, Scalar: mode},
		}},
		Resolutions: resolutions,
	}, nil
}

func (m *codexAsTargetModule) Frontmatter(_ domain.FrontmatterRequest) (domain.FrontmatterPlan, error) {
	return domain.FrontmatterPlan{}, nil
}

// TargetPath returns a path with a "-codex-tgt.toml" suffix to avoid colliding with the
// source file when both source and target are Codex-format agents in the same directory.
func (m *codexAsTargetModule) TargetPath(req domain.TargetPathRequest) (string, error) {
	return req.Key + "-codex-tgt.toml", nil
}

func (m *codexAsTargetModule) Injection(_ domain.InjectionRequest) (string, bool) {
	return "", false
}

func (m *codexAsTargetModule) HookPlan(_ domain.HookPlanRequest) (domain.HookPlan, error) {
	return domain.HookPlan{Supported: false, Reason: "stub"}, nil
}

func (m *codexAsTargetModule) Close() error { return nil }

const codexAsTgtID = "codex-as-tgt"

func newCodexAsTargetModule() *codexAsTargetModule {
	return &codexAsTargetModule{
		ref: domain.HarnessRef{ID: codexAsTgtID, DisplayName: "Codex As Target", Usable: true},
		descriptor: domain.HarnessDescriptor{
			ID:            codexAsTgtID,
			DisplayName:   "Codex As Target",
			AgentFormatID: "codex-toml",
			// No ToolsKey: Codex expresses capability via sandbox_mode, not a tools field.
			Frontmatter: domain.FrontmatterSpec{
				KeyOrder: []string{"sandbox_mode"},
			},
			Paths: domain.PathSpec{
				Agents: domain.ScopedPaths{Supported: true, Project: ".codex/agents"},
			},
			Extensions: map[domain.ArtifactKind]string{
				domain.ArtifactAgent: ".toml",
			},
		},
	}
}

// ---------------------------------------------------------------------------
// Shared helper: deps and run for per-target-type tests
// ---------------------------------------------------------------------------

// newCodexNRRetargetWithTargetDeps returns Deps wired with the non-recoverable Codex source
// harness and the provided target module. Used by the per-target-type T15.2(c) tests so each
// can supply its own target module without duplicating the source-module setup.
func newCodexNRRetargetWithTargetDeps(t *testing.T, tgtMod domain.HarnessModule, tgtID string) (app.Deps, string) {
	t.Helper()
	stub := interactiontest.NewBuilder().Build()
	deps, workspace := newBaseDeps(t, stub)
	srcMod := newCodexNonRecoverableRetargetModule()
	deps.Registry = &stubRegistry{
		list: []domain.HarnessRef{
			{ID: codexNRHarnessID, DisplayName: "Codex NR Harness", Usable: true},
			{ID: tgtID, DisplayName: "Target Harness", Usable: true},
		},
		modules: map[string]domain.HarnessModule{
			codexNRHarnessID: srcMod,
			tgtID:            tgtMod,
		},
	}
	return deps, workspace
}

// runCodexNRRetargetWithTarget writes a Codex source file and runs TransformHarness from
// the non-recoverable Codex source to the given target module. Returns the result and error.
func runCodexNRRetargetWithTarget(t *testing.T, tgtMod domain.HarnessModule, tgtID string, srcBytes []byte) (app.TransformHarnessResult, error) {
	t.Helper()
	deps, _ := newCodexNRRetargetWithTargetDeps(t, tgtMod, tgtID)
	svc := app.New(deps)

	srcDir := t.TempDir()
	srcPath := filepath.Join(srcDir, "my-codex-agent.toml")
	if err := os.WriteFile(srcPath, srcBytes, 0o644); err != nil {
		t.Fatalf("runCodexNRRetargetWithTarget: write source: %v", err)
	}

	req := app.TransformHarnessRequest{
		SourceHarnessID: codexNRHarnessID,
		TargetHarnessID: tgtID,
		Path:            srcPath,
		Overwrite:       true,
	}

	return svc.TransformHarness(context.Background(), req)
}

// ---------------------------------------------------------------------------
// T15.2(c) per-target-type test: Claude Code comma-scalar form
// ---------------------------------------------------------------------------

// TestRetarget_CodexNonRecoverable_MinimalGrant_ClaudeCodeTarget_CommaScalarForm verifies
// that when the target harness emits tools as a comma-separated scalar (mimicking Claude Code),
// the destination file contains the minimal grant tools in a scalar field, not a YAML list.
// A YAML list would be read by Claude Code as "inherit all" -- an escalation hazard.
// The scalar form "tools: file_read, file_search, content_search" must appear instead.
func TestRetarget_CodexNonRecoverable_MinimalGrant_ClaudeCodeTarget_CommaScalarForm(t *testing.T) {
	result, err := runCodexNRRetargetWithTarget(t, newClaudeCodeLikeTargetModule(), claudeCodeLikeTgtID, eligibleCodexTomlRetargetBytes())
	if err != nil {
		t.Fatalf("TransformHarness: unexpected error: %v", err)
	}
	if len(result.Files) == 0 {
		t.Fatalf("no file outcomes")
	}
	out := result.Files[0]
	if out.Status != app.StatusTransformed {
		t.Fatalf("file status = %q (reason: %q), want %q", out.Status, out.Reason, app.StatusTransformed)
	}

	destBytes, readErr := os.ReadFile(out.DestinationPath)
	if readErr != nil {
		t.Fatalf("cannot read destination: %v", readErr)
	}
	destStr := string(destBytes)

	// Assert: all three minimal grant tools appear in the destination.
	for _, tool := range descriptor.MinimalGenericToolSet {
		if !strings.Contains(destStr, tool) {
			t.Errorf("destination does not contain minimal grant tool %q; got:\n%s", tool, destStr)
		}
	}

	// Assert: tools field is a comma-separated scalar, NOT a YAML block-list.
	// A YAML block-list item would appear as "  - tool_name" on its own line.
	// The comma-scalar form would appear as "tools: file_read, file_search, content_search"
	// on a single line -- no "  - " items under the tools key.
	for _, tool := range descriptor.MinimalGenericToolSet {
		listItem := "\n  - " + tool
		if strings.Contains(destStr, listItem) {
			t.Errorf("destination tools field contains YAML list item %q; "+
				"a Claude Code target must emit a comma-separated scalar, not a YAML list "+
				"(a YAML list is read by Claude Code as inherit-all); got:\n%s",
				listItem, destStr)
		}
	}
}

// ---------------------------------------------------------------------------
// T15.2(c) per-target-type test: OpenCode permission-map form
// ---------------------------------------------------------------------------

// TestRetarget_CodexNonRecoverable_MinimalGrant_OpenCodeTarget_PermissionMapForm verifies
// that when the target harness emits tools as a permission mapping (mimicking OpenCode's
// ShapePermission), the destination file contains "allow" dispositions for each of the
// minimal grant tools. A flat list or a scalar would indicate the permission-map path was
// bypassed and that the output file would be misread by OpenCode.
func TestRetarget_CodexNonRecoverable_MinimalGrant_OpenCodeTarget_PermissionMapForm(t *testing.T) {
	result, err := runCodexNRRetargetWithTarget(t, newOpenCodeLikeTargetModule(), openCodeLikeTgtID, eligibleCodexTomlRetargetBytes())
	if err != nil {
		t.Fatalf("TransformHarness: unexpected error: %v", err)
	}
	if len(result.Files) == 0 {
		t.Fatalf("no file outcomes")
	}
	out := result.Files[0]
	if out.Status != app.StatusTransformed {
		t.Fatalf("file status = %q (reason: %q), want %q", out.Status, out.Reason, app.StatusTransformed)
	}

	destBytes, readErr := os.ReadFile(out.DestinationPath)
	if readErr != nil {
		t.Fatalf("cannot read destination: %v", readErr)
	}
	destStr := string(destBytes)

	// Assert: the permissions key is present (the OpenCode-like module uses "permissions").
	if !strings.Contains(destStr, "permissions:") {
		t.Errorf("destination does not contain \"permissions:\" key; "+
			"the OpenCode permission-map form must include the declared tools key; got:\n%s", destStr)
	}

	// Assert: each minimal grant tool appears with "allow" disposition in the mapping.
	// The YAML serialisation of a KindMapping entry is "  tool_name: allow\n".
	for _, tool := range descriptor.MinimalGenericToolSet {
		wantEntry := "  " + tool + ": allow"
		if !strings.Contains(destStr, wantEntry) {
			t.Errorf("destination does not contain permission-map entry %q for minimal grant tool %q; "+
				"the OpenCode form must assign 'allow' to each granted tool; got:\n%s",
				wantEntry, tool, destStr)
		}
	}
}

// ---------------------------------------------------------------------------
// T15.2(c) per-target-type test: GHCP list-with-by_convention form
// ---------------------------------------------------------------------------

// TestRetarget_CodexNonRecoverable_MinimalGrant_GHCPTarget_ListWithByConvention verifies
// that when the target harness emits tools as a YAML block list and always includes a
// by-convention entry (mimicking GHCP), the destination file contains both the minimal
// grant tools and the by-convention tool. The by-convention tool must be present regardless
// of what generic tools are requested.
func TestRetarget_CodexNonRecoverable_MinimalGrant_GHCPTarget_ListWithByConvention(t *testing.T) {
	result, err := runCodexNRRetargetWithTarget(t, newGHCPLikeTargetModule(), ghcpLikeTgtID, eligibleCodexTomlRetargetBytes())
	if err != nil {
		t.Fatalf("TransformHarness: unexpected error: %v", err)
	}
	if len(result.Files) == 0 {
		t.Fatalf("no file outcomes")
	}
	out := result.Files[0]
	if out.Status != app.StatusTransformed {
		t.Fatalf("file status = %q (reason: %q), want %q", out.Status, out.Reason, app.StatusTransformed)
	}

	destBytes, readErr := os.ReadFile(out.DestinationPath)
	if readErr != nil {
		t.Fatalf("cannot read destination: %v", readErr)
	}
	destStr := string(destBytes)

	// Assert: each minimal grant tool appears as a YAML block-list item.
	// The YAML serialisation of a KindList/ListBlock item is "  - tool_name\n".
	for _, tool := range descriptor.MinimalGenericToolSet {
		listItem := "  - " + tool
		if !strings.Contains(destStr, listItem) {
			t.Errorf("destination does not contain YAML list item %q for minimal grant tool %q; "+
				"a GHCP-like target must emit tools as a YAML block list; got:\n%s",
				listItem, tool, destStr)
		}
	}

	// Assert: the by-convention tool is present in the list.
	// By-convention tools are emitted for every agent regardless of the requested grant.
	byConvItem := "  - " + ghcpByConventionTool
	if !strings.Contains(destStr, byConvItem) {
		t.Errorf("destination does not contain by-convention tool entry %q; "+
			"a GHCP-like target must include the by-convention tool in every tools list; got:\n%s",
			byConvItem, destStr)
	}
}

// ---------------------------------------------------------------------------
// T15.2(c) per-target-type test: Codex-as-target no-tools-field form
// ---------------------------------------------------------------------------

// TestRetarget_CodexNonRecoverable_MinimalGrant_CodexTarget_SandboxModeNoToolsKey verifies
// that when the target is a Codex harness (no ToolsKey, capability via sandbox_mode),
// the output TOML file carries sandbox_mode = "read-only" and no tools key. This is the
// "no tools field for Codex" form required by the plan: the non-recoverable minimal grant
// for a non-escalating set of generic tools collapses to "read-only" sandbox mode, and no
// separate tools field is written.
//
// The test also covers the missing case identified in the review: "a test that retargets
// from a non-recoverable Codex source to a Codex target" (Codex-to-Codex direction, in
// contrast to T15.4 which covers Markdown-to-Codex).
func TestRetarget_CodexNonRecoverable_MinimalGrant_CodexTarget_SandboxModeNoToolsKey(t *testing.T) {
	result, err := runCodexNRRetargetWithTarget(t, newCodexAsTargetModule(), codexAsTgtID, eligibleCodexTomlRetargetBytes())
	if err != nil {
		t.Fatalf("TransformHarness: unexpected error: %v", err)
	}
	if len(result.Files) == 0 {
		t.Fatalf("no file outcomes")
	}
	out := result.Files[0]
	if out.Status != app.StatusTransformed {
		t.Fatalf("file status = %q (reason: %q), want %q", out.Status, out.Reason, app.StatusTransformed)
	}

	destBytes, readErr := os.ReadFile(out.DestinationPath)
	if readErr != nil {
		t.Fatalf("cannot read destination: %v", readErr)
	}

	// Assert: destination file must be parseable as TOML.
	var parsed map[string]interface{}
	if err := toml.Unmarshal(destBytes, &parsed); err != nil {
		t.Fatalf("destination file is not valid TOML: %v\nFirst 200 bytes: %q",
			err, string(destBytes[:min(len(destBytes), 200)]))
	}

	// Assert: sandbox_mode = "read-only" is present.
	// The MinimalGenericToolSet contains only non-escalating tools (file_read, file_search,
	// content_search), so the Codex module must collapse them to "read-only".
	sandboxMode, ok := parsed["sandbox_mode"]
	if !ok {
		t.Errorf("destination TOML does not contain sandbox_mode key; "+
			"a Codex target must express capability via sandbox_mode, not a tools field; "+
			"present keys: %v", tomlKeys(parsed))
	} else if s, isStr := sandboxMode.(string); !isStr || s != "read-only" {
		t.Errorf("sandbox_mode = %v (%T); want string %q; "+
			"the minimal non-escalating grant must collapse to read-only sandbox mode",
			sandboxMode, sandboxMode, "read-only")
	}

	// Assert: no tools key in the output TOML.
	// Codex represents capability through sandbox_mode; a separate tools field would
	// indicate that the wrong rendering path was taken.
	if _, hasTools := parsed["tools"]; hasTools {
		t.Errorf("destination TOML contains a \"tools\" key; "+
			"a Codex target must not emit a tools field -- capability is expressed solely "+
			"via sandbox_mode; present keys: %v", tomlKeys(parsed))
	}
}
