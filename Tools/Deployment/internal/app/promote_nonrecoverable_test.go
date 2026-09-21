package app_test

// promote_nonrecoverable_test.go covers T15.1.
//
// T15.1 -- Promote from Codex (non-recoverable source):
//   (a) In interactive mode the flow asks the user for tools through the question channel
//       promote already owns, with text stating that the source harness does not record tools,
//       and accepts the answer in the format fixed by the prompt contract (comma-separated
//       generic names via AskText).
//   (b) An answer naming a tool outside the generic vocabulary is rejected and re-asked, and
//       no such name reaches the promoted file.
//   (c) A blank answer yields an explicit empty tools list.
//   (d) In non-interactive mode the promoted agent gets an explicit empty tools list plus a
//       reported warning.
//   (e) The promoted agent is valid generic Markdown.
//   (f) The carriage container from the decoded Codex source is stripped before the canonical
//       document reaches the promote output, and every key it held is named in a report.
//   (g) sandbox_mode from the Codex source is stripped and named with its value in the report:
//       StrippedField{Key: "sandbox_mode", Reason: StripReasonUnknownField}.

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
// Codex non-recoverable harness module stub
// ---------------------------------------------------------------------------

// newCodexNonRecoverableModule returns a codexPromoteModule with ToolInfoUnrecoverable set
// to true. This models a Codex harness whose deployed artifact does not carry a recoverable
// tool list -- sandbox_mode records capability instead of a tools field.
func newCodexNonRecoverableModule() *codexPromoteModule {
	m := newCodexPromoteModule()
	m.descriptor.ToolInfoUnrecoverable = true
	return m
}

// newCodexNonRecoverablePromoteDeps builds Deps suitable for the non-recoverable promote tests.
// The registry resolves "codex-harness" to a module with ToolInfoUnrecoverable: true so the
// promote flow takes the non-recoverable branch rather than the ordinary reverse-mapping path.
func newCodexNonRecoverablePromoteDeps(t *testing.T, stub *interactiontest.Stub, mosaicRoot string) (app.Deps, string) {
	t.Helper()
	deps, workspace := newPromoteDeps(t, stub, mosaicRoot)
	deps.Registry = &stubRegistry{
		list: []domain.HarnessRef{
			{ID: "codex-harness", DisplayName: "Codex Harness"},
		},
		modules: map[string]domain.HarnessModule{
			"codex-harness": newCodexNonRecoverableModule(),
		},
	}
	return deps, workspace
}

// ---------------------------------------------------------------------------
// Fixtures
// ---------------------------------------------------------------------------

// eligibleCodexTomlWithUserKeyBytes returns a Codex TOML source file with a user-owned key
// (my_custom_setting) alongside the standard eligible fields. After decode the user key ends
// up in the carriage container (mosaic_carriage), making it suitable for T15.1(f) and T15.3.
func eligibleCodexTomlWithUserKeyBytes() []byte {
	return []byte(
		"# mosaic_transform_version: 2.1.0\n" +
			"# mosaic_version: 1.0.0\n" +
			"\n" +
			"name = \"my-codex-agent\"\n" +
			"my_custom_setting = \"user-value\"\n" +
			"sandbox_mode = \"read-only\"\n" +
			"developer_instructions = \"\"\"\n" +
			"<Identity type=\"core\">\n" +
			"You are My Codex Agent.\n" +
			"</Identity>\n" +
			"\"\"\"\n",
	)
}

// bodylessCodexTomlBytes returns bytes for a Codex TOML source file that has no
// developer_instructions key. When the Codex decoder processes this file, the decode report
// contains EntryMissingInstructions. Used for the AC15.11 promote source-read merge point test.
func bodylessCodexTomlBytes() []byte {
	return []byte(
		"# mosaic_transform_version: 2.1.0\n" +
			"# mosaic_version: 1.0.0\n" +
			"\n" +
			"name = \"my-codex-agent\"\n" +
			"sandbox_mode = \"read-only\"\n",
	)
}

// writeCodexNonRecoverableSource writes the given bytes to dir/<name> and returns the full path.
func writeCodexNonRecoverableSource(t *testing.T, dir string, name string, content []byte) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("writeCodexNonRecoverableSource: %v", err)
	}
	return path
}

// ---------------------------------------------------------------------------
// T15.1(a) -- interactive mode asks QPromoteNonRecoverableTools
// ---------------------------------------------------------------------------

// TestPromote_NonRecoverableSource_Interactive_AsksQPromoteNonRecoverableTools verifies that
// when the source harness has ToolInfoUnrecoverable=true, the promote flow asks
// QPromoteNonRecoverableTools with the source file path as its subject. The flow must enter
// the non-recoverable branch and not rely on the reverse-mapping path which would silently
// produce an empty tool set.
func TestPromote_NonRecoverableSource_Interactive_AsksQPromoteNonRecoverableTools(t *testing.T) {
	// Arrange
	mosaicRoot := writeMosaicRoot(t)
	srcPath := writeCodexNonRecoverableSource(t, t.TempDir(), "my-codex-agent.toml", eligibleCodexTomlBytes())

	stub := interactiontest.NewBuilder().
		AnswerText(domain.QPromoteNonRecoverableTools, srcPath, "file_read").
		Build()
	deps, _ := newCodexNonRecoverablePromoteDeps(t, stub, mosaicRoot)
	svc := app.New(deps)

	req := app.PromoteRequest{
		FilePath:  srcPath,
		Category:  "TestCategory",
		HarnessID: "codex-harness",
	}

	// Act
	_, _ = svc.Promote(context.Background(), req)

	// Assert
	if !stub.WasAsked(domain.QPromoteNonRecoverableTools, srcPath) {
		t.Errorf("QPromoteNonRecoverableTools was not asked with subject %q; "+
			"the non-recoverable branch must prompt for tools instead of relying on reverse mapping", srcPath)
	}
}

// TestPromote_NonRecoverableSource_Interactive_AnsweredTool_AppearsInPromotedFile verifies that
// a tool name provided through QPromoteNonRecoverableTools appears in the promoted file's tools
// list. The user answered "file_read" and the output must contain exactly that tool.
func TestPromote_NonRecoverableSource_Interactive_AnsweredTool_AppearsInPromotedFile(t *testing.T) {
	// Arrange
	mosaicRoot := writeMosaicRoot(t)
	srcPath := writeCodexNonRecoverableSource(t, t.TempDir(), "my-codex-agent.toml", eligibleCodexTomlBytes())

	stub := interactiontest.NewBuilder().
		AnswerText(domain.QPromoteNonRecoverableTools, srcPath, "file_read").
		Build()
	deps, _ := newCodexNonRecoverablePromoteDeps(t, stub, mosaicRoot)
	svc := app.New(deps)

	req := app.PromoteRequest{
		FilePath:  srcPath,
		Category:  "TestCategory",
		HarnessID: "codex-harness",
	}

	// Act
	result, err := svc.Promote(context.Background(), req)
	if err != nil {
		t.Fatalf("Promote: unexpected error: %v", err)
	}

	// Assert
	found := false
	for _, tool := range result.Tools {
		if tool == "file_read" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("result.Tools = %v; want \"file_read\" to appear after the user answered that generic tool name", result.Tools)
	}
}

// ---------------------------------------------------------------------------
// T15.1(b) -- unknown tool name rejected, not in promoted file
// ---------------------------------------------------------------------------

// TestPromote_NonRecoverableSource_Interactive_UnknownToolName_NotInPromotedFile verifies that
// a tool name outside the generic vocabulary does not appear in the promoted file. The
// implementation must reject it (re-ask or discard) rather than accepting it silently.
//
// The stub returns the same answer on every call, so the flow either loops until it gives up
// (falling back to blank-answer behaviour) or rejects it; either way, the name must not reach
// the output. The primary assertion is that result.Tools does not contain the bogus name.
//
// The re-ask obligation (the question is asked more than once) and the vocabulary-named-back
// obligation are not explicitly verified here: verifying re-ask would require a stub that
// returns different answers on subsequent calls, which the current interactiontest.Builder
// does not support (it always returns the scripted answer). The primary behavioral protection
// -- the bad tool name is absent from result.Tools -- is sufficient coverage for the output
// contract. A future test could use an answer sequence stub if re-ask behavior is introduced
// as a testable invariant.
func TestPromote_NonRecoverableSource_Interactive_UnknownToolName_NotInPromotedFile(t *testing.T) {
	// Arrange
	mosaicRoot := writeMosaicRoot(t)
	srcPath := writeCodexNonRecoverableSource(t, t.TempDir(), "my-codex-agent.toml", eligibleCodexTomlBytes())

	stub := interactiontest.NewBuilder().
		AnswerText(domain.QPromoteNonRecoverableTools, srcPath, "completely_unknown_tool_xyz").
		Build()
	deps, _ := newCodexNonRecoverablePromoteDeps(t, stub, mosaicRoot)
	svc := app.New(deps)

	req := app.PromoteRequest{
		FilePath:  srcPath,
		Category:  "TestCategory",
		HarnessID: "codex-harness",
	}

	// Act
	result, err := svc.Promote(context.Background(), req)
	if err != nil {
		t.Fatalf("Promote: unexpected error: %v", err)
	}

	// Assert
	for _, tool := range result.Tools {
		if tool == "completely_unknown_tool_xyz" {
			t.Errorf("result.Tools = %v; unknown tool name %q must never appear in the promoted file",
				result.Tools, "completely_unknown_tool_xyz")
			break
		}
	}
}

// ---------------------------------------------------------------------------
// T15.1(c) -- blank answer yields explicit empty tools list
// ---------------------------------------------------------------------------

// TestPromote_NonRecoverableSource_Interactive_BlankAnswer_ExplicitEmptyToolsList verifies that
// when the user answers QPromoteNonRecoverableTools with an empty string, the promoted file
// contains an explicit empty tools list (tools: []), not an absent tools field. An absent tools
// field in generic Markdown means "inherit all" for some consumers, so the explicit form is
// required.
func TestPromote_NonRecoverableSource_Interactive_BlankAnswer_ExplicitEmptyToolsList(t *testing.T) {
	// Arrange
	mosaicRoot := writeMosaicRoot(t)
	srcPath := writeCodexNonRecoverableSource(t, t.TempDir(), "my-codex-agent.toml", eligibleCodexTomlBytes())

	stub := interactiontest.NewBuilder().
		AnswerText(domain.QPromoteNonRecoverableTools, srcPath, "").
		Build()
	deps, _ := newCodexNonRecoverablePromoteDeps(t, stub, mosaicRoot)
	svc := app.New(deps)

	req := app.PromoteRequest{
		FilePath:  srcPath,
		Category:  "TestCategory",
		HarnessID: "codex-harness",
	}

	// Act
	result, err := svc.Promote(context.Background(), req)
	if err != nil {
		t.Fatalf("Promote: unexpected error: %v", err)
	}

	destBytes, readErr := os.ReadFile(result.DestinationPath)
	if readErr != nil {
		t.Fatalf("cannot read destination file %q: %v", result.DestinationPath, readErr)
	}

	// Assert: destination must carry an explicit empty tools list, not an absent field.
	if !strings.Contains(string(destBytes), "tools: []") {
		t.Errorf("promoted file does not contain \"tools: []\" for blank answer; "+
			"a blank answer must produce an explicit empty list, not an absent field; got:\n%s",
			string(destBytes))
	}
}

// ---------------------------------------------------------------------------
// T15.1(d) -- non-interactive: empty tools list and warning notice
// ---------------------------------------------------------------------------

// TestPromote_NonRecoverableSource_NonInteractive_EmptyToolsList verifies that when the question
// stub returns SkippedOne for QPromoteNonRecoverableTools (the non-interactive path), the
// promoted file contains an explicit empty tools list.
func TestPromote_NonRecoverableSource_NonInteractive_EmptyToolsList(t *testing.T) {
	// Arrange: no answer scripted -- stub returns SkippedOne by default.
	mosaicRoot := writeMosaicRoot(t)
	srcPath := writeCodexNonRecoverableSource(t, t.TempDir(), "my-codex-agent.toml", eligibleCodexTomlBytes())

	stub := interactiontest.NewBuilder().Build()
	deps, _ := newCodexNonRecoverablePromoteDeps(t, stub, mosaicRoot)
	svc := app.New(deps)

	req := app.PromoteRequest{
		FilePath:  srcPath,
		Category:  "TestCategory",
		HarnessID: "codex-harness",
	}

	// Act
	result, err := svc.Promote(context.Background(), req)
	if err != nil {
		t.Fatalf("Promote (non-interactive): unexpected error: %v", err)
	}

	destBytes, readErr := os.ReadFile(result.DestinationPath)
	if readErr != nil {
		t.Fatalf("cannot read destination file %q: %v", result.DestinationPath, readErr)
	}

	// Assert
	if !strings.Contains(string(destBytes), "tools: []") {
		t.Errorf("non-interactive promote does not contain \"tools: []\"; "+
			"the non-interactive path must write an explicit empty tools list; got:\n%s",
			string(destBytes))
	}
}

// TestPromote_NonRecoverableSource_NonInteractive_WarningNoticeEmitted verifies that the
// non-interactive path (SkippedOne answer) emits at least one NoticeWarning, informing the
// operator that tools could not be recovered and an empty list was written.
func TestPromote_NonRecoverableSource_NonInteractive_WarningNoticeEmitted(t *testing.T) {
	// Arrange
	mosaicRoot := writeMosaicRoot(t)
	srcPath := writeCodexNonRecoverableSource(t, t.TempDir(), "my-codex-agent.toml", eligibleCodexTomlBytes())

	stub := interactiontest.NewBuilder().Build()
	deps, _ := newCodexNonRecoverablePromoteDeps(t, stub, mosaicRoot)
	svc := app.New(deps)

	req := app.PromoteRequest{
		FilePath:  srcPath,
		Category:  "TestCategory",
		HarnessID: "codex-harness",
	}

	// Act
	_, err := svc.Promote(context.Background(), req)
	if err != nil {
		t.Fatalf("Promote (non-interactive): unexpected error: %v", err)
	}

	// Assert
	notices := stub.Notices()
	hasWarning := false
	for _, n := range notices {
		if n.Level == domain.NoticeWarning {
			hasWarning = true
			break
		}
	}
	if !hasWarning {
		t.Errorf("no NoticeWarning emitted for non-interactive promote from non-recoverable source; "+
			"the operator must be told that tools could not be recovered; got notices: %v", notices)
	}
}

// ---------------------------------------------------------------------------
// T15.1(e) -- promoted agent is valid generic Markdown
// ---------------------------------------------------------------------------

// TestPromote_NonRecoverableSource_PromotedAgentIsValidMarkdown verifies that the destination
// file produced for a non-recoverable Codex source is a valid generic Markdown agent (starts
// with "---\n"), regardless of the tool resolution outcome.
func TestPromote_NonRecoverableSource_PromotedAgentIsValidMarkdown(t *testing.T) {
	// Arrange
	mosaicRoot := writeMosaicRoot(t)
	srcPath := writeCodexNonRecoverableSource(t, t.TempDir(), "my-codex-agent.toml", eligibleCodexTomlBytes())

	stub := interactiontest.NewBuilder().
		AnswerText(domain.QPromoteNonRecoverableTools, srcPath, "file_read").
		Build()
	deps, _ := newCodexNonRecoverablePromoteDeps(t, stub, mosaicRoot)
	svc := app.New(deps)

	req := app.PromoteRequest{
		FilePath:  srcPath,
		Category:  "TestCategory",
		HarnessID: "codex-harness",
	}

	// Act
	result, err := svc.Promote(context.Background(), req)
	if err != nil {
		t.Fatalf("Promote: unexpected error: %v", err)
	}

	destBytes, readErr := os.ReadFile(result.DestinationPath)
	if readErr != nil {
		t.Fatalf("cannot read destination file %q: %v", result.DestinationPath, readErr)
	}

	// Assert
	if !strings.HasPrefix(string(destBytes), "---\n") {
		t.Errorf("promoted agent does not start with Markdown frontmatter delimiter \"---\\n\"; "+
			"got: %q", string(destBytes[:min(len(destBytes), 40)]))
	}
}

// ---------------------------------------------------------------------------
// T15.1(f) -- carriage container stripped and keys named in report
// ---------------------------------------------------------------------------

// TestPromote_NonRecoverableSource_CarriageContainer_AbsentFromOutputFile verifies that the
// carriage container keys (mosaic_carriage and mosaic_carriage_markers) are not present in the
// promoted generic Markdown file. When a Codex source carries user keys they end up in the
// carriage container after decode; that container must be stripped at the format-change boundary
// before the canonical document reaches the promote output.
func TestPromote_NonRecoverableSource_CarriageContainer_AbsentFromOutputFile(t *testing.T) {
	// Arrange: source carries my_custom_setting, which ends up in mosaic_carriage after decode.
	mosaicRoot := writeMosaicRoot(t)
	srcPath := writeCodexNonRecoverableSource(t, t.TempDir(), "my-codex-agent.toml",
		eligibleCodexTomlWithUserKeyBytes())

	stub := interactiontest.NewBuilder().
		AnswerText(domain.QPromoteNonRecoverableTools, srcPath, "file_read").
		Build()
	deps, _ := newCodexNonRecoverablePromoteDeps(t, stub, mosaicRoot)
	svc := app.New(deps)

	req := app.PromoteRequest{
		FilePath:  srcPath,
		Category:  "TestCategory",
		HarnessID: "codex-harness",
	}

	// Act
	result, err := svc.Promote(context.Background(), req)
	if err != nil {
		t.Fatalf("Promote: unexpected error: %v", err)
	}

	destBytes, readErr := os.ReadFile(result.DestinationPath)
	if readErr != nil {
		t.Fatalf("cannot read destination file %q: %v", result.DestinationPath, readErr)
	}

	// Assert: neither carriage key must appear in the promoted file.
	if strings.Contains(string(destBytes), "mosaic_carriage:") {
		t.Errorf("promoted file contains \"mosaic_carriage:\"; "+
			"the carriage container must be stripped at the format-change boundary before the canonical "+
			"document reaches the promote output; got:\n%s", string(destBytes))
	}
	if strings.Contains(string(destBytes), "mosaic_carriage_markers:") {
		t.Errorf("promoted file contains \"mosaic_carriage_markers:\"; "+
			"it must be stripped at the format-change boundary; got:\n%s", string(destBytes))
	}
}

// TestPromote_NonRecoverableSource_CarriedKey_NamedInStrippedFields verifies that the key held
// in the carriage container (my_custom_setting) appears in PromoteResult.StrippedFields with
// Reason StripReasonCarriedKey. This is the run-report entry that tells the operator which keys
// did not travel the format change.
func TestPromote_NonRecoverableSource_CarriedKey_NamedInStrippedFields(t *testing.T) {
	// Arrange
	mosaicRoot := writeMosaicRoot(t)
	srcPath := writeCodexNonRecoverableSource(t, t.TempDir(), "my-codex-agent.toml",
		eligibleCodexTomlWithUserKeyBytes())

	stub := interactiontest.NewBuilder().
		AnswerText(domain.QPromoteNonRecoverableTools, srcPath, "file_read").
		Build()
	deps, _ := newCodexNonRecoverablePromoteDeps(t, stub, mosaicRoot)
	svc := app.New(deps)

	req := app.PromoteRequest{
		FilePath:  srcPath,
		Category:  "TestCategory",
		HarnessID: "codex-harness",
	}

	// Act
	result, err := svc.Promote(context.Background(), req)
	if err != nil {
		t.Fatalf("Promote: unexpected error: %v", err)
	}

	// Assert
	found := false
	for _, sf := range result.StrippedFields {
		if sf.Key == "my_custom_setting" && sf.Reason == app.StripReasonCarriedKey {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("result.StrippedFields does not contain {Key: \"my_custom_setting\", Reason: StripReasonCarriedKey}; "+
			"every key held in the carriage container must be named in the report; got: %v", result.StrippedFields)
	}
}

// TestPromote_NonRecoverableSource_SourceFile_NotModifiedByPromote verifies that the source
// Codex TOML file is not modified when promote strips its carriage container.
// StripCarriage is pure and must not mutate the source artifact.
func TestPromote_NonRecoverableSource_SourceFile_NotModifiedByPromote(t *testing.T) {
	// Arrange
	mosaicRoot := writeMosaicRoot(t)
	srcBytes := eligibleCodexTomlWithUserKeyBytes()
	srcPath := writeCodexNonRecoverableSource(t, t.TempDir(), "my-codex-agent.toml", srcBytes)

	stub := interactiontest.NewBuilder().
		AnswerText(domain.QPromoteNonRecoverableTools, srcPath, "file_read").
		Build()
	deps, _ := newCodexNonRecoverablePromoteDeps(t, stub, mosaicRoot)
	svc := app.New(deps)

	req := app.PromoteRequest{
		FilePath:  srcPath,
		Category:  "TestCategory",
		HarnessID: "codex-harness",
	}

	// Act
	_, err := svc.Promote(context.Background(), req)
	if err != nil {
		t.Fatalf("Promote: unexpected error: %v", err)
	}

	// Assert
	afterBytes, readErr := os.ReadFile(srcPath)
	if readErr != nil {
		t.Fatalf("cannot re-read source file: %v", readErr)
	}
	if string(afterBytes) != string(srcBytes) {
		t.Errorf("source file was modified by promote; promote must never modify the source artifact")
	}
}

// ---------------------------------------------------------------------------
// T15.1(g) -- sandbox_mode stripped and named with StripReasonUnknownField
// ---------------------------------------------------------------------------

// TestPromote_NonRecoverableSource_SandboxMode_StrippedAndNamedInReport verifies that
// sandbox_mode from the Codex source is stripped from the generic Markdown output and
// reported in PromoteResult.StrippedFields as:
//
//	StrippedField{Key: "sandbox_mode", Reason: StripReasonUnknownField}
//
// sandbox_mode is Codex-native vocabulary that has no meaning in generic Markdown. The
// classifier sees it as ClassUnknown and strips it with StripReasonUnknownField.
func TestPromote_NonRecoverableSource_SandboxMode_StrippedAndNamedInReport(t *testing.T) {
	// Arrange: eligibleCodexTomlBytes carries sandbox_mode = "read-only".
	mosaicRoot := writeMosaicRoot(t)
	srcPath := writeCodexNonRecoverableSource(t, t.TempDir(), "my-codex-agent.toml", eligibleCodexTomlBytes())

	stub := interactiontest.NewBuilder().
		AnswerText(domain.QPromoteNonRecoverableTools, srcPath, "file_read").
		Build()
	deps, _ := newCodexNonRecoverablePromoteDeps(t, stub, mosaicRoot)
	svc := app.New(deps)

	req := app.PromoteRequest{
		FilePath:  srcPath,
		Category:  "TestCategory",
		HarnessID: "codex-harness",
	}

	// Act
	result, err := svc.Promote(context.Background(), req)
	if err != nil {
		t.Fatalf("Promote: unexpected error: %v", err)
	}

	// Assert: sandbox_mode must appear in StrippedFields with StripReasonUnknownField.
	found := false
	for _, sf := range result.StrippedFields {
		if sf.Key == "sandbox_mode" && sf.Reason == app.StripReasonUnknownField {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("result.StrippedFields does not contain {Key: \"sandbox_mode\", Reason: StripReasonUnknownField}; "+
			"sandbox_mode is Codex-native and must be stripped and reported when promoting to generic Markdown; got: %v",
			result.StrippedFields)
	}
}

// TestPromote_NonRecoverableSource_SandboxMode_AbsentFromPromotedFile verifies that the promoted
// generic Markdown file does not contain a sandbox_mode key; it is Codex-specific and has no
// meaning in generic Markdown.
func TestPromote_NonRecoverableSource_SandboxMode_AbsentFromPromotedFile(t *testing.T) {
	// Arrange
	mosaicRoot := writeMosaicRoot(t)
	srcPath := writeCodexNonRecoverableSource(t, t.TempDir(), "my-codex-agent.toml", eligibleCodexTomlBytes())

	stub := interactiontest.NewBuilder().
		AnswerText(domain.QPromoteNonRecoverableTools, srcPath, "file_read").
		Build()
	deps, _ := newCodexNonRecoverablePromoteDeps(t, stub, mosaicRoot)
	svc := app.New(deps)

	req := app.PromoteRequest{
		FilePath:  srcPath,
		Category:  "TestCategory",
		HarnessID: "codex-harness",
	}

	// Act
	result, err := svc.Promote(context.Background(), req)
	if err != nil {
		t.Fatalf("Promote: unexpected error: %v", err)
	}

	destBytes, readErr := os.ReadFile(result.DestinationPath)
	if readErr != nil {
		t.Fatalf("cannot read destination file %q: %v", result.DestinationPath, readErr)
	}

	// Assert
	if strings.Contains(string(destBytes), "sandbox_mode:") {
		t.Errorf("promoted file contains \"sandbox_mode:\"; "+
			"sandbox_mode is Codex-native vocabulary and must not appear in the generic Markdown output; got:\n%s",
			string(destBytes))
	}
}

// ---------------------------------------------------------------------------
// T15.3 -- cross-format container invariant at flow level: both carriage shapes (promote)
// ---------------------------------------------------------------------------

// eligibleCodexTomlWithBothCarriageShapesBytes returns a Codex TOML source file for flow-level
// T15.3 both-shapes promote tests. It carries:
//
//   - my_value_key = "user-value": a plain string scalar -- after decode this lands in
//     mosaic_carriage (value-carried), so StripCarriage must report Key "my_value_key".
//   - [my_table] with setting = "config-value": a TOML table section -- after decode the key
//     name "my_table" lands in mosaic_carriage_markers (marker-carried), so StripCarriage must
//     report Key "my_table".
//
// The file includes sandbox_mode alongside these user keys. sandbox_mode is intentional: the
// promote descriptor (newCodexNonRecoverableModule) lists it in Frontmatter.KeyOrder so that
// DetectHarnessMatch classifies it as ClassHarness rather than ClassUnknown. This fixture is
// distinct from eligibleCodexTomlWithUserKeyBytes(), which carries only a value-carried key.
func eligibleCodexTomlWithBothCarriageShapesBytes() []byte {
	return []byte(
		"# mosaic_transform_version: 2.1.0\n" +
			"# mosaic_version: 1.0.0\n" +
			"\n" +
			"name = \"my-codex-agent\"\n" +
			"my_value_key = \"user-value\"\n" +
			"sandbox_mode = \"read-only\"\n" +
			"developer_instructions = \"\"\"\n" +
			"<Identity type=\"core\">\n" +
			"You are My Codex Agent.\n" +
			"</Identity>\n" +
			"\"\"\"\n" +
			"\n" +
			"[my_table]\n" +
			"setting = \"config-value\"\n",
	)
}

// TestPromote_CodexNonRecoverable_BothCarriageShapes_ValueCarriedKeyInStrippedFields verifies
// that when the Codex source carries a plain scalar user key (value-carried, lands in
// mosaic_carriage after decode), the key name appears in PromoteResult.StrippedFields with
// Reason StripReasonCarriedKey at flow level. This test is the flow-level complement to the
// T15.9 unit test for the promote direction: it proves that the full promote pipeline feeds
// the canonical document through StripCarriage and surfaces value-carried keys in the report.
func TestPromote_CodexNonRecoverable_BothCarriageShapes_ValueCarriedKeyInStrippedFields(t *testing.T) {
	// Arrange
	mosaicRoot := writeMosaicRoot(t)
	srcPath := writeCodexNonRecoverableSource(t, t.TempDir(), "my-codex-agent.toml",
		eligibleCodexTomlWithBothCarriageShapesBytes())

	stub := interactiontest.NewBuilder().
		AnswerText(domain.QPromoteNonRecoverableTools, srcPath, "file_read").
		Build()
	deps, _ := newCodexNonRecoverablePromoteDeps(t, stub, mosaicRoot)
	svc := app.New(deps)

	req := app.PromoteRequest{
		FilePath:  srcPath,
		Category:  "TestCategory",
		HarnessID: "codex-harness",
	}

	// Act
	result, err := svc.Promote(context.Background(), req)
	if err != nil {
		t.Fatalf("Promote: unexpected error: %v", err)
	}

	// Assert: value-carried key must appear in StrippedFields.
	found := false
	for _, sf := range result.StrippedFields {
		if sf.Key == "my_value_key" && sf.Reason == app.StripReasonCarriedKey {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("result.StrippedFields does not contain {Key: \"my_value_key\", Reason: StripReasonCarriedKey}; "+
			"the value-carried user key must be named in the run report after flow-level stripping; "+
			"got: %v", result.StrippedFields)
	}
}

// TestPromote_CodexNonRecoverable_BothCarriageShapes_MarkerCarriedKeyInStrippedFields verifies
// that when the Codex source carries a TOML table user key (marker-carried, lands in
// mosaic_carriage_markers after decode), the key name appears in PromoteResult.StrippedFields
// with Reason StripReasonCarriedKey at flow level. This is the critical half of the T15.3
// both-shapes requirement for the promote direction: a bug that silently dropped marker-carried
// keys from StrippedFields while correctly reporting value-carried keys would not be caught
// without this test.
func TestPromote_CodexNonRecoverable_BothCarriageShapes_MarkerCarriedKeyInStrippedFields(t *testing.T) {
	// Arrange
	mosaicRoot := writeMosaicRoot(t)
	srcPath := writeCodexNonRecoverableSource(t, t.TempDir(), "my-codex-agent.toml",
		eligibleCodexTomlWithBothCarriageShapesBytes())

	stub := interactiontest.NewBuilder().
		AnswerText(domain.QPromoteNonRecoverableTools, srcPath, "file_read").
		Build()
	deps, _ := newCodexNonRecoverablePromoteDeps(t, stub, mosaicRoot)
	svc := app.New(deps)

	req := app.PromoteRequest{
		FilePath:  srcPath,
		Category:  "TestCategory",
		HarnessID: "codex-harness",
	}

	// Act
	result, err := svc.Promote(context.Background(), req)
	if err != nil {
		t.Fatalf("Promote: unexpected error: %v", err)
	}

	// Assert: marker-carried key must appear in StrippedFields.
	found := false
	for _, sf := range result.StrippedFields {
		if sf.Key == "my_table" && sf.Reason == app.StripReasonCarriedKey {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("result.StrippedFields does not contain {Key: \"my_table\", Reason: StripReasonCarriedKey}; "+
			"the marker-carried user key (TOML table) must be named in the run report after flow-level "+
			"stripping; this verifies that StripCarriage enumerates both carriage sub-containers at "+
			"promote flow level; got: %v", result.StrippedFields)
	}
}

// TestPromote_CodexNonRecoverable_BothCarriageShapes_CarriageContainerAbsentFromOutput verifies
// that neither carriage container key (mosaic_carriage or mosaic_carriage_markers) appears in the
// promoted generic Markdown file when the Codex source carries both value-carried and
// marker-carried user keys. This asserts the stripping call removes both sub-containers from
// the canonical document before it reaches the promote output.
func TestPromote_CodexNonRecoverable_BothCarriageShapes_CarriageContainerAbsentFromOutput(t *testing.T) {
	// Arrange
	mosaicRoot := writeMosaicRoot(t)
	srcPath := writeCodexNonRecoverableSource(t, t.TempDir(), "my-codex-agent.toml",
		eligibleCodexTomlWithBothCarriageShapesBytes())

	stub := interactiontest.NewBuilder().
		AnswerText(domain.QPromoteNonRecoverableTools, srcPath, "file_read").
		Build()
	deps, _ := newCodexNonRecoverablePromoteDeps(t, stub, mosaicRoot)
	svc := app.New(deps)

	req := app.PromoteRequest{
		FilePath:  srcPath,
		Category:  "TestCategory",
		HarnessID: "codex-harness",
	}

	// Act
	result, err := svc.Promote(context.Background(), req)
	if err != nil {
		t.Fatalf("Promote: unexpected error: %v", err)
	}

	destBytes, readErr := os.ReadFile(result.DestinationPath)
	if readErr != nil {
		t.Fatalf("cannot read destination file %q: %v", result.DestinationPath, readErr)
	}
	destStr := string(destBytes)

	// Assert: neither carriage container must appear in the promoted file.
	if strings.Contains(destStr, "mosaic_carriage:") {
		t.Errorf("promoted file contains \"mosaic_carriage:\"; "+
			"the carriage container must be fully stripped before the promote output is written; "+
			"got:\n%s", destStr)
	}
	if strings.Contains(destStr, "mosaic_carriage_markers:") {
		t.Errorf("promoted file contains \"mosaic_carriage_markers:\"; "+
			"both carriage sub-containers must be stripped at the format-change boundary; "+
			"got:\n%s", destStr)
	}
}

// ---------------------------------------------------------------------------
// AC15.11 -- decode report merged at the promote source read point
// ---------------------------------------------------------------------------

// TestPromote_NonRecoverableSource_BodyLess_MissingInstructionsInRunReport verifies that when
// the Codex source has no developer_instructions key, the EntryMissingInstructions notice from
// the source's decode report is forwarded to the run report as a GapManualStep gap. This asserts
// that mergeDecodeReport is called at the promote source read point, before any eligibility check
// or downstream processing that may fail for a body-less source.
//
// In the pre-stage state mergeDecodeReport is not called at the source read point, so the spy
// records no gap. The assertion fails (RED). After I15.2, the source read produces the decode
// report, mergeDecodeReport forwards EntryMissingInstructions as GapManualStep via
// deps.Todo.AddGap, and the assertion passes.
func TestPromote_NonRecoverableSource_BodyLess_MissingInstructionsInRunReport(t *testing.T) {
	// Arrange: body-less Codex source (no developer_instructions).
	mosaicRoot := writeMosaicRoot(t)
	srcPath := writeCodexNonRecoverableSource(t, t.TempDir(), "my-codex-agent.toml", bodylessCodexTomlBytes())

	spy := &spyTodo{}
	stub := interactiontest.NewBuilder().Build()
	deps, _ := newCodexNonRecoverablePromoteDeps(t, stub, mosaicRoot)
	deps.Todo = spy
	svc := app.New(deps)

	req := app.PromoteRequest{
		FilePath:  srcPath,
		Category:  "TestCategory",
		HarnessID: "codex-harness",
	}

	// Act: promote may fail at the eligibility check because the body-less source has no
	// canonical body; the test asserts on the spy, not on the promote outcome.
	_, _ = svc.Promote(context.Background(), req) //nolint:errcheck

	// Assert: the decode report from the body-less source must surface in the run report.
	// mergeDecodeReport translates EntryMissingInstructions to domain.GapManualStep and calls
	// deps.Todo.AddGap. In the pre-stage state (no decode report merge at source read) the spy
	// records no gap.
	if !spy.hasGapKind(domain.GapManualStep) {
		t.Errorf("no GapManualStep recorded in the run report after promote from a body-less "+
			"Codex source; mergeDecodeReport must be called at the source read point and the "+
			"EntryMissingInstructions notice forwarded to deps.Todo.AddGap")
	}
}

// ---------------------------------------------------------------------------
// T15.5 -- Markdown-to-Markdown promote: non-recoverable branch not taken
// ---------------------------------------------------------------------------

// TestPromote_MarkdownSource_NonRecoverableBranchNotTaken verifies that promoting a Markdown
// source through a Markdown harness (ToolInfoUnrecoverable=false, the zero value) does NOT ask
// QPromoteNonRecoverableTools. The non-recoverable branch is gated by ToolInfoUnrecoverable and
// must not activate for harnesses that have a recoverable tool list.
func TestPromote_MarkdownSource_NonRecoverableBranchNotTaken(t *testing.T) {
	// Arrange: use the standard promote deps (Markdown module, ToolInfoUnrecoverable=false).
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
	_, err := svc.Promote(context.Background(), req)
	if err != nil {
		t.Fatalf("Promote (Markdown source): unexpected error: %v", err)
	}

	// Assert: QPromoteNonRecoverableTools must not have been asked for a Markdown source.
	for _, call := range stub.Calls() {
		if call.ID == domain.QPromoteNonRecoverableTools {
			t.Errorf("QPromoteNonRecoverableTools was asked for a Markdown-source promote; "+
				"it must only be asked when ToolInfoUnrecoverable=true on the source harness descriptor")
			break
		}
	}
}
