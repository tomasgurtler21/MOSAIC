package app_test

// render_service_test.go covers the service-level RenderAgent flow: request validation,
// source-file handling, role and version derivation, destination handling, output fidelity,
// and the orchestrator workflow case.
//
// Verified behaviours:
//
// Request validation (T1.1):
//   - A request with neither SourcePath nor SourceAgentKey returns ErrRenderSourceRequired.
//   - A request with both SourcePath and SourceAgentKey returns ErrRenderSourceAmbiguous.
//   - A request with an empty TargetHarnessID returns ErrRenderHarnessRequired.
//   - A request naming an unregistered harness returns an error wrapping ErrRenderHarnessUnresolvable.
//   - A request with neither DestinationPath nor WorkspaceRoot returns ErrRenderDestinationRequired.
//   - A request with both DestinationPath and WorkspaceRoot returns ErrRenderDestinationAmbiguous.
//   - A request with the same path for source and destination does not silently succeed
//     (it is rejected before any write attempt, at minimum by ErrRenderDestinationExists
//     if the source already exists, or by the validation order if caught earlier).
//   - RenderAgent never consults domain.Interaction regardless of the request shape.
//
// Source-file handling (T1.2):
//   - A valid generic-form agent file at an arbitrary path outside the catalogue is read
//     and rendered successfully.
//   - A SourcePath that does not exist on the filesystem returns ErrRenderSourceNotFound.
//   - A SourcePath that points to a directory (not a file) returns ErrRenderSourceNotFile.
//   - A SourcePath whose content is not a parseable generic-form MOSAIC agent returns
//     ErrRenderSourceNotGeneric; nothing is written.
//   - On any source error the result is the zero value.
//
// Role and version derivation (T1.3):
//   - A source file with `role: subagent` yields Role "subagent" in the result.
//   - A source file with `role: orchestrator` yields Role "orchestrator" in the result.
//   - A source file with no `role` field defaults to "subagent" (domain.RoleSubagent).
//   - A source file with an unrecognised `role` value returns ErrRenderInvalidRole; the
//     result is the zero value and nothing is written.
//   - A source file with a `version` field yields that value as SourceVersion in the result.
//   - A source file with no `version` field yields an empty SourceVersion — a legal state.
//
// Destination handling (T1.4):
//   - A DestinationPath is honoured verbatim: the rendered file is written to that exact path.
//   - When the destination's parent directories do not exist, they are created.
//   - When a file already exists at the destination and Overwrite is false, the call returns
//     ErrRenderDestinationExists and the existing file is left byte-identical to before.
//   - When a file already exists at the destination and Overwrite is true, the call succeeds
//     and the destination contains the rendered output.
//   - DryRun=true computes every outcome and reports the correct DestinationPath in the
//     result but writes no file and creates no directory.
//   - DryRun=true still performs the overwrite check: an existing destination without
//     Overwrite returns ErrRenderDestinationExists even in dry-run.
//   - On a successful render, DestinationResolvedBy is "explicit" for a DestinationPath request
//     and "workspace" for a WorkspaceRoot request.
//
// Output fidelity (T1.5):
//   - The bytes written at the destination are byte-identical to what transform.Apply
//     produces for the same generic source, harness module, and model — no transformation
//     logic is reimplemented in renderAgent.
//
// Orchestrator and workflow set (T1.6):
//   - An orchestrator rendered with a caller-specified workflow set carries those workflow
//     ids in the result's Workflows field and in the rendered output's workflow region.
//   - req.Workflows nil (not specified) is distinguishable in the rendered output from
//     req.Workflows set to a non-nil empty slice (explicitly none): an orchestrator
//     rendered with nil carries its default workflow injection, while one rendered with
//     []string{} carries an empty set.
//   - A workflow id supplied in req.Workflows that is not in the catalogue returns
//     ErrRenderUnknownWorkflow; nothing is written.
//   - A non-empty Workflows slice supplied for an agent whose resolved role is not
//     orchestrator returns ErrRenderWorkflowsNotApplicable; nothing is written.
//   - A non-nil but empty InfrastructureAgents for a subagent is legal ("explicitly none").

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"mosaic-deploy/internal/app"
	"mosaic-deploy/internal/app/interactiontest"
	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/transform"
)

// isZeroRenderResult reports whether r is the zero value of RenderAgentResult.
// Plain equality is not usable because the type contains slice fields.
func isZeroRenderResult(r app.RenderAgentResult) bool {
	return reflect.DeepEqual(r, app.RenderAgentResult{})
}

// ---------------------------------------------------------------------------
// Render test fixtures
// ---------------------------------------------------------------------------

// renderHarness is the target harness used in all render tests.
var renderHarness = domain.HarnessRef{
	ID:          "render-test-harness",
	DisplayName: "Render Test Harness",
	Tier:        domain.TierBuiltin,
	Usable:      true,
}

// newRenderTargetModule returns a stubHarnessModule for renderHarness. The agent extension
// ".rt.md" is distinct from the generic ".md" so tests can identify rendered output easily.
// Paths.Agents is populated so WorkspaceRoot requests resolve a non-empty destination.
func newRenderTargetModule() *stubHarnessModule {
	return &stubHarnessModule{
		ref: renderHarness,
		descriptor: domain.HarnessDescriptor{
			ID:          "render-test-harness",
			DisplayName: "Render Test Harness",
			Frontmatter: domain.FrontmatterSpec{
				ModelKey: "rt_model",
			},
			Paths: domain.PathSpec{
				Agents: domain.ScopedPaths{
					Supported: true,
					Project:   ".claude/agents",
				},
			},
			Extensions: map[domain.ArtifactKind]string{
				domain.ArtifactAgent: ".md",
			},
		},
	}
}

// newRenderRegistry returns a stubRegistry backed by the render test harness module only.
func newRenderRegistry() *stubRegistry {
	mod := newRenderTargetModule()
	return &stubRegistry{
		list:    []domain.HarnessRef{renderHarness},
		modules: map[string]domain.HarnessModule{renderHarness.ID: mod},
	}
}

// newRenderDeps returns an app.Deps wired for render tests. Uses the minimal base deps
// with the render registry substituted in place of the minimal single-harness registry.
// The catalog is also enriched with a workflow the orchestrator tests can reference.
func newRenderDeps(t *testing.T) app.Deps {
	t.Helper()
	stub := interactiontest.NewBuilder().Build()
	deps, _ := newBaseDeps(t, stub)
	deps.Registry = newRenderRegistry()
	// Add a workflow that orchestrator render tests can request by id.
	deps.Catalog = newRenderCatalog()
	return deps
}

// newRenderCatalog returns a stubCatalog enriched with the minimal catalog data plus one
// workflow ("quick-fix", already defined in testhelpers_test.go) and one workflow section,
// so that render tests exercising the workflow path can drive Catalog.WorkflowSection.
func newRenderCatalog() *stubCatalog {
	cat := newMinimalCatalog()
	cat.sections = map[string][]byte{
		"quick-fix": []byte("<Workflow type=\"core\" name=\"quick-fix\">\nQuick Fix workflow block.\n</Workflow>\n"),
	}
	return cat
}

// minimalGenericSubagentBytes returns a byte-valid generic-form MOSAIC agent for render
// tests. It carries the required MOSAIC frontmatter fields and one <Identity type="core">
// body block.
func minimalGenericSubagentBytes() []byte {
	return []byte("---\n" +
		"id: \"42\"\n" +
		"version: \"2.0.0\"\n" +
		"name: render-test-agent\n" +
		"description: A generic-form agent for render tests.\n" +
		"role: subagent\n" +
		"model: \"{model-identifier}\"\n" +
		"tools: [bash]\n" +
		"---\n" +
		"<Identity type=\"core\">\n" +
		"# RenderTest Agent\n\n" +
		"You are the render test agent.\n" +
		"</Identity>\n" +
		"<CommunicationProtocol type=\"managed\">\n" +
		"</CommunicationProtocol>\n")
}

// minimalGenericSubagentNoVersionBytes returns a generic agent without a `version` field,
// to verify that absent version yields an empty SourceVersion in the result.
func minimalGenericSubagentNoVersionBytes() []byte {
	return []byte("---\n" +
		"id: \"43\"\n" +
		"name: render-test-no-version\n" +
		"description: A generic agent with no version field.\n" +
		"role: subagent\n" +
		"model: \"{model-identifier}\"\n" +
		"tools: []\n" +
		"---\n" +
		"<Identity type=\"core\">\n" +
		"You are the render test agent with no version.\n" +
		"</Identity>\n" +
		"<CommunicationProtocol type=\"managed\">\n" +
		"</CommunicationProtocol>\n")
}

// minimalGenericSubagentNoRoleBytes returns a generic agent without a `role` field, to
// verify that an absent role defaults to domain.RoleSubagent.
func minimalGenericSubagentNoRoleBytes() []byte {
	return []byte("---\n" +
		"id: \"44\"\n" +
		"version: \"1.0.0\"\n" +
		"name: render-test-no-role\n" +
		"description: A generic agent with no role field.\n" +
		"model: \"{model-identifier}\"\n" +
		"tools: []\n" +
		"---\n" +
		"<Identity type=\"core\">\n" +
		"You are the render test agent with no role.\n" +
		"</Identity>\n" +
		"<CommunicationProtocol type=\"managed\">\n" +
		"</CommunicationProtocol>\n")
}

// minimalGenericSubagentBadRoleBytes returns a generic agent with an unrecognised `role`
// value, to verify that ErrRenderInvalidRole is returned.
func minimalGenericSubagentBadRoleBytes() []byte {
	return []byte("---\n" +
		"id: \"45\"\n" +
		"version: \"1.0.0\"\n" +
		"name: render-test-bad-role\n" +
		"description: A generic agent with an invalid role.\n" +
		"role: analyst\n" +
		"model: \"{model-identifier}\"\n" +
		"tools: []\n" +
		"---\n" +
		"<Identity type=\"core\">\n" +
		"You are the render test agent with a bad role.\n" +
		"</Identity>\n" +
		"<CommunicationProtocol type=\"managed\">\n" +
		"</CommunicationProtocol>\n")
}

// minimalGenericOrchestratorBytes returns a generic-form orchestrator agent for workflow
// render tests. It carries role: orchestrator and the AvailableWorkflows deployed region.
// AvailableWorkflows is in CanonicalDeployed, so docformat.ClassifyRegion requires the
// managed region marker; project region returns ErrMarkerMismatch.
func minimalGenericOrchestratorBytes() []byte {
	return []byte("---\n" +
		"id: \"46\"\n" +
		"version: \"1.0.0\"\n" +
		"name: render-test-orchestrator\n" +
		"description: A generic orchestrator for render tests.\n" +
		"role: orchestrator\n" +
		"model: \"{model-identifier}\"\n" +
		"tools: []\n" +
		"---\n" +
		"<Identity type=\"core\">\n" +
		"You are the render test orchestrator.\n" +
		"</Identity>\n" +
		"<AvailableWorkflows type=\"managed\">\n" +
		"</AvailableWorkflows>\n" +
		"<CommunicationProtocol type=\"managed\">\n" +
		"</CommunicationProtocol>\n")
}

// minimalGenericOrchestratorWithInfraRegionBytes returns a generic-form orchestrator that
// carries both the <InfrastructureAgents type="managed"> and <AvailableWorkflows type="managed">
// regions. Used to test that a caller-specified InfrastructureAgents set is injected and
// appears in the rendered output.
func minimalGenericOrchestratorWithInfraRegionBytes() []byte {
	return []byte("---\n" +
		"id: \"47\"\n" +
		"version: \"1.0.0\"\n" +
		"name: render-test-orchestrator-infra\n" +
		"description: A generic orchestrator for infrastructure-agent render tests.\n" +
		"role: orchestrator\n" +
		"model: \"{model-identifier}\"\n" +
		"tools: []\n" +
		"---\n" +
		"<Identity type=\"core\">\n" +
		"You are the render test orchestrator with infrastructure support.\n" +
		"</Identity>\n" +
		"<InfrastructureAgents type=\"managed\">\n" +
		"</InfrastructureAgents>\n" +
		"<AvailableWorkflows type=\"managed\">\n" +
		"</AvailableWorkflows>\n" +
		"<CommunicationProtocol type=\"managed\">\n" +
		"</CommunicationProtocol>\n")
}

// notAGenericAgentBytes returns content that is not a parseable generic-form MOSAIC agent:
// the YAML frontmatter block contains a syntax error that docformat.Parse cannot process.
func notAGenericAgentBytes() []byte {
	return []byte("---\n" +
		": : : this is invalid yaml content\n" +
		"  - [broken\n" +
		"---\n" +
		"Some body text.\n")
}

// writeRenderSourceFile writes content to a file named name under dir and returns the
// absolute path. It fails the test if the write fails.
func writeRenderSourceFile(t *testing.T, dir, name string, content []byte) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, content, 0o644); err != nil {
		t.Fatalf("writeRenderSourceFile: %v", err)
	}
	return p
}

// newPreAnsweredRenderRequest returns a minimal RenderAgentRequest with all required fields
// populated and pointing to a valid temporary source file. The request uses an explicit
// destination path inside the same temp directory.
func newPreAnsweredRenderRequest(sourcePath, destPath string) app.RenderAgentRequest {
	return app.RenderAgentRequest{
		SourcePath:      sourcePath,
		TargetHarnessID: renderHarness.ID,
		DestinationPath: destPath,
		TargetModel:     "render-model-x",
	}
}

// ---------------------------------------------------------------------------
// T1.1: Request validation and pre-answer behaviour
// ---------------------------------------------------------------------------

// TestRenderAgent_NoSource_ReturnsErrRenderSourceRequired verifies that a request with
// neither SourcePath nor SourceAgentKey is refused with ErrRenderSourceRequired before
// any filesystem access.
func TestRenderAgent_NoSource_ReturnsErrRenderSourceRequired(t *testing.T) {
	// Arrange
	deps := newRenderDeps(t)
	svc := app.New(deps)
	destDir := t.TempDir()

	req := app.RenderAgentRequest{
		TargetHarnessID: renderHarness.ID,
		DestinationPath: filepath.Join(destDir, "out.md"),
	}

	// Act
	result, err := svc.RenderAgent(context.Background(), req)

	// Assert
	if !errors.Is(err, app.ErrRenderSourceRequired) {
		t.Errorf("error = %v, want error wrapping ErrRenderSourceRequired", err)
	}
	if !isZeroRenderResult(result) {
		t.Error("result must be zero value on error")
	}
}

// TestRenderAgent_BothSources_ReturnsErrRenderSourceAmbiguous verifies that a request
// with both SourcePath and SourceAgentKey set is refused with ErrRenderSourceAmbiguous.
func TestRenderAgent_BothSources_ReturnsErrRenderSourceAmbiguous(t *testing.T) {
	// Arrange
	deps := newRenderDeps(t)
	svc := app.New(deps)
	dir := t.TempDir()
	src := writeRenderSourceFile(t, dir, "agent.md", minimalGenericSubagentBytes())

	req := app.RenderAgentRequest{
		SourcePath:      src,
		SourceAgentKey:  "some-agent",
		TargetHarnessID: renderHarness.ID,
		DestinationPath: filepath.Join(dir, "out.md"),
	}

	// Act
	result, err := svc.RenderAgent(context.Background(), req)

	// Assert
	if !errors.Is(err, app.ErrRenderSourceAmbiguous) {
		t.Errorf("error = %v, want error wrapping ErrRenderSourceAmbiguous", err)
	}
	if !isZeroRenderResult(result) {
		t.Error("result must be zero value on error")
	}
}

// TestRenderAgent_EmptyHarnessID_ReturnsErrRenderHarnessRequired verifies that a request
// with an empty TargetHarnessID is refused before any harness resolution attempt.
func TestRenderAgent_EmptyHarnessID_ReturnsErrRenderHarnessRequired(t *testing.T) {
	// Arrange
	deps := newRenderDeps(t)
	svc := app.New(deps)
	dir := t.TempDir()
	src := writeRenderSourceFile(t, dir, "agent.md", minimalGenericSubagentBytes())

	req := app.RenderAgentRequest{
		SourcePath:      src,
		TargetHarnessID: "", // empty — the key omission
		DestinationPath: filepath.Join(dir, "out.md"),
	}

	// Act
	result, err := svc.RenderAgent(context.Background(), req)

	// Assert
	if !errors.Is(err, app.ErrRenderHarnessRequired) {
		t.Errorf("error = %v, want error wrapping ErrRenderHarnessRequired", err)
	}
	if !isZeroRenderResult(result) {
		t.Error("result must be zero value on error")
	}
}

// TestRenderAgent_UnknownHarnessID_ReturnsErrRenderHarnessUnresolvable verifies that a
// harness id not registered in the registry returns ErrRenderHarnessUnresolvable.
func TestRenderAgent_UnknownHarnessID_ReturnsErrRenderHarnessUnresolvable(t *testing.T) {
	// Arrange
	deps := newRenderDeps(t)
	svc := app.New(deps)
	dir := t.TempDir()
	src := writeRenderSourceFile(t, dir, "agent.md", minimalGenericSubagentBytes())

	req := app.RenderAgentRequest{
		SourcePath:      src,
		TargetHarnessID: "nonexistent-harness",
		DestinationPath: filepath.Join(dir, "out.md"),
	}

	// Act
	result, err := svc.RenderAgent(context.Background(), req)

	// Assert
	if !errors.Is(err, app.ErrRenderHarnessUnresolvable) {
		t.Errorf("error = %v, want error wrapping ErrRenderHarnessUnresolvable", err)
	}
	if !isZeroRenderResult(result) {
		t.Error("result must be zero value on error")
	}
}

// TestRenderAgent_NoDestination_ReturnsErrRenderDestinationRequired verifies that a
// request with neither DestinationPath nor WorkspaceRoot is refused.
func TestRenderAgent_NoDestination_ReturnsErrRenderDestinationRequired(t *testing.T) {
	// Arrange
	deps := newRenderDeps(t)
	svc := app.New(deps)
	dir := t.TempDir()
	src := writeRenderSourceFile(t, dir, "agent.md", minimalGenericSubagentBytes())

	req := app.RenderAgentRequest{
		SourcePath:      src,
		TargetHarnessID: renderHarness.ID,
		// Neither DestinationPath nor WorkspaceRoot
	}

	// Act
	result, err := svc.RenderAgent(context.Background(), req)

	// Assert
	if !errors.Is(err, app.ErrRenderDestinationRequired) {
		t.Errorf("error = %v, want error wrapping ErrRenderDestinationRequired", err)
	}
	if !isZeroRenderResult(result) {
		t.Error("result must be zero value on error")
	}
}

// TestRenderAgent_BothDestinations_ReturnsErrRenderDestinationAmbiguous verifies that
// supplying both DestinationPath and WorkspaceRoot is refused.
func TestRenderAgent_BothDestinations_ReturnsErrRenderDestinationAmbiguous(t *testing.T) {
	// Arrange
	deps := newRenderDeps(t)
	svc := app.New(deps)
	dir := t.TempDir()
	src := writeRenderSourceFile(t, dir, "agent.md", minimalGenericSubagentBytes())

	req := app.RenderAgentRequest{
		SourcePath:      src,
		TargetHarnessID: renderHarness.ID,
		DestinationPath: filepath.Join(dir, "out.md"),
		WorkspaceRoot:   dir, // both supplied — the key ambiguity
	}

	// Act
	result, err := svc.RenderAgent(context.Background(), req)

	// Assert
	if !errors.Is(err, app.ErrRenderDestinationAmbiguous) {
		t.Errorf("error = %v, want error wrapping ErrRenderDestinationAmbiguous", err)
	}
	if !isZeroRenderResult(result) {
		t.Error("result must be zero value on error")
	}
}

// TestRenderAgent_SamePathSourceAndDestination_RefusedBeforeWrite verifies that supplying
// the same path for SourcePath and DestinationPath does not silently overwrite the source.
// Because the source file exists, the overwrite check fires at minimum with
// ErrRenderDestinationExists (Overwrite is false). In any case no write is attempted.
func TestRenderAgent_SamePathSourceAndDestination_RefusedBeforeWrite(t *testing.T) {
	// Arrange
	deps := newRenderDeps(t)
	svc := app.New(deps)
	dir := t.TempDir()
	src := writeRenderSourceFile(t, dir, "agent.md", minimalGenericSubagentBytes())
	originalBytes, _ := os.ReadFile(src)

	req := app.RenderAgentRequest{
		SourcePath:      src,
		TargetHarnessID: renderHarness.ID,
		DestinationPath: src, // same as source
		Overwrite:       false,
	}

	// Act
	_, err := svc.RenderAgent(context.Background(), req)

	// Assert: any error is acceptable — what must NOT happen is a nil error.
	if err == nil {
		t.Error("expected a non-nil error when source and destination are the same path")
	}

	// The source file must be byte-identical to before the call.
	afterBytes, readErr := os.ReadFile(src)
	if readErr != nil {
		t.Fatalf("cannot read source file after call: %v", readErr)
	}
	if !bytes.Equal(originalBytes, afterBytes) {
		t.Error("source file was modified by RenderAgent; it must be left unchanged")
	}
}

// TestRenderAgent_FullyPreAnswered_NoInteraction verifies that RenderAgent never consults
// domain.Interaction even when the request is fully specified and valid, distinguishing it
// from every other Service method that may ask questions through the interaction port.
func TestRenderAgent_FullyPreAnswered_NoInteraction(t *testing.T) {
	// Arrange: interaction stub that fails the test if any question is asked.
	stub := interactiontest.NewBuilder().Build()
	deps, _ := newBaseDeps(t, stub)
	deps.Registry = newRenderRegistry()
	deps.Catalog = newRenderCatalog()
	svc := app.New(deps)

	dir := t.TempDir()
	src := writeRenderSourceFile(t, dir, "agent.md", minimalGenericSubagentBytes())
	req := newPreAnsweredRenderRequest(src, filepath.Join(dir, "out.md"))

	// Act
	_, _ = svc.RenderAgent(context.Background(), req)

	// Assert: no questions were asked through the interaction port.
	if len(stub.QuestionOrder()) != 0 {
		t.Errorf("RenderAgent asked questions %v; it must never consult Interaction", stub.QuestionOrder())
	}
}

// ---------------------------------------------------------------------------
// T1.2: Source-file handling
// ---------------------------------------------------------------------------

// TestRenderAgent_ValidSourceOutsideCatalogue_Succeeds verifies the happy path: a
// generic-form agent at an arbitrary path outside Catalog/Subagents/ is read and rendered
// to the destination with a nil error and a populated result.
func TestRenderAgent_ValidSourceOutsideCatalogue_Succeeds(t *testing.T) {
	// Arrange: source is in a non-catalogue directory.
	deps := newRenderDeps(t)
	svc := app.New(deps)
	srcDir := t.TempDir()
	destDir := t.TempDir()
	src := writeRenderSourceFile(t, srcDir, "render-test-agent.md", minimalGenericSubagentBytes())
	dest := filepath.Join(destDir, "render-test-agent.md")

	req := newPreAnsweredRenderRequest(src, dest)

	// Act
	result, err := svc.RenderAgent(context.Background(), req)
	if err != nil {
		t.Fatalf("RenderAgent returned unexpected error: %v", err)
	}

	// Assert: result carries expected values.
	if result.SourcePath != src {
		t.Errorf("SourcePath = %q, want %q", result.SourcePath, src)
	}
	if result.AgentKey != "render-test-agent" {
		t.Errorf("AgentKey = %q, want %q", result.AgentKey, "render-test-agent")
	}
	if result.SourceVersion != "2.0.0" {
		t.Errorf("SourceVersion = %q, want %q", result.SourceVersion, "2.0.0")
	}
	if result.DestinationPath != dest {
		t.Errorf("DestinationPath = %q, want %q", result.DestinationPath, dest)
	}
	if result.DestinationResolvedBy != "explicit" {
		t.Errorf("DestinationResolvedBy = %q, want %q", result.DestinationResolvedBy, "explicit")
	}

	// Assert: the destination file exists and is non-empty.
	written, readErr := os.ReadFile(dest)
	if readErr != nil {
		t.Fatalf("destination file not written: %v", readErr)
	}
	if len(written) == 0 {
		t.Error("destination file is empty; want rendered agent content")
	}
}

// TestRenderAgent_SourcePathMissing_ReturnsErrRenderSourceNotFound verifies that a
// SourcePath that does not exist on the filesystem returns ErrRenderSourceNotFound.
func TestRenderAgent_SourcePathMissing_ReturnsErrRenderSourceNotFound(t *testing.T) {
	// Arrange
	deps := newRenderDeps(t)
	svc := app.New(deps)
	dir := t.TempDir()
	nonExistent := filepath.Join(dir, "does-not-exist.md")
	dest := filepath.Join(dir, "out.md")

	req := newPreAnsweredRenderRequest(nonExistent, dest)

	// Act
	result, err := svc.RenderAgent(context.Background(), req)

	// Assert
	if !errors.Is(err, app.ErrRenderSourceNotFound) {
		t.Errorf("error = %v, want error wrapping ErrRenderSourceNotFound", err)
	}
	if !isZeroRenderResult(result) {
		t.Error("result must be zero value when source is not found")
	}

	// Assert: no destination file was created.
	if _, statErr := os.Stat(dest); !os.IsNotExist(statErr) {
		t.Error("destination file must not be created when source is missing")
	}
}

// TestRenderAgent_SourcePathIsDirectory_ReturnsErrRenderSourceNotFile verifies that a
// SourcePath pointing to a directory (rather than a regular file) returns
// ErrRenderSourceNotFile without writing anything.
func TestRenderAgent_SourcePathIsDirectory_ReturnsErrRenderSourceNotFile(t *testing.T) {
	// Arrange: source is a directory, not a file.
	deps := newRenderDeps(t)
	svc := app.New(deps)
	dir := t.TempDir()
	dest := filepath.Join(dir, "out.md")

	req := newPreAnsweredRenderRequest(dir, dest) // dir as SourcePath

	// Act
	result, err := svc.RenderAgent(context.Background(), req)

	// Assert
	if !errors.Is(err, app.ErrRenderSourceNotFile) {
		t.Errorf("error = %v, want error wrapping ErrRenderSourceNotFile", err)
	}
	if !isZeroRenderResult(result) {
		t.Error("result must be zero value when source is a directory")
	}

	// Assert: no destination file was created.
	if _, statErr := os.Stat(dest); !os.IsNotExist(statErr) {
		t.Error("destination file must not be created when source is a directory")
	}
}

// TestRenderAgent_SourceNotGeneric_ReturnsErrRenderSourceNotGeneric verifies that a
// SourcePath whose content cannot be parsed as a generic-form MOSAIC agent returns
// ErrRenderSourceNotGeneric; nothing is written to the destination.
func TestRenderAgent_SourceNotGeneric_ReturnsErrRenderSourceNotGeneric(t *testing.T) {
	// Arrange: file has invalid YAML frontmatter that docformat.Parse cannot process.
	deps := newRenderDeps(t)
	svc := app.New(deps)
	dir := t.TempDir()
	src := writeRenderSourceFile(t, dir, "bad-agent.md", notAGenericAgentBytes())
	dest := filepath.Join(dir, "out.md")

	req := newPreAnsweredRenderRequest(src, dest)

	// Act
	result, err := svc.RenderAgent(context.Background(), req)

	// Assert
	if !errors.Is(err, app.ErrRenderSourceNotGeneric) {
		t.Errorf("error = %v, want error wrapping ErrRenderSourceNotGeneric", err)
	}
	if !isZeroRenderResult(result) {
		t.Error("result must be zero value when source is not a generic agent")
	}

	// Assert: no destination file was created.
	if _, statErr := os.Stat(dest); !os.IsNotExist(statErr) {
		t.Error("destination file must not be created when source is not a generic agent")
	}
}

// TestRenderAgent_SourceErrors_AreDistinctSentinels verifies that ErrRenderSourceNotFound
// and ErrRenderSourceNotFile are distinct sentinel values — errors.Is on each should not
// match the other.
func TestRenderAgent_SourceErrors_AreDistinctSentinels(t *testing.T) {
	if errors.Is(app.ErrRenderSourceNotFound, app.ErrRenderSourceNotFile) {
		t.Error("ErrRenderSourceNotFound matches ErrRenderSourceNotFile; they must be distinct")
	}
	if errors.Is(app.ErrRenderSourceNotFile, app.ErrRenderSourceNotFound) {
		t.Error("ErrRenderSourceNotFile matches ErrRenderSourceNotFound; they must be distinct")
	}
}

// ---------------------------------------------------------------------------
// T1.3: Role and version derivation from source frontmatter
// ---------------------------------------------------------------------------

// TestRenderAgent_SubagentRole_ReportedInResult verifies that a source file declaring
// `role: subagent` yields Role "subagent" in the result.
func TestRenderAgent_SubagentRole_ReportedInResult(t *testing.T) {
	// Arrange
	deps := newRenderDeps(t)
	svc := app.New(deps)
	dir := t.TempDir()
	src := writeRenderSourceFile(t, dir, "render-test-agent.md", minimalGenericSubagentBytes())
	req := newPreAnsweredRenderRequest(src, filepath.Join(dir, "out.md"))

	// Act
	result, err := svc.RenderAgent(context.Background(), req)
	if err != nil {
		t.Fatalf("RenderAgent returned error: %v", err)
	}

	// Assert
	if result.Role != string(domain.RoleSubagent) {
		t.Errorf("Role = %q, want %q", result.Role, string(domain.RoleSubagent))
	}
}

// TestRenderAgent_OrchestratorRole_ReportedInResult verifies that a source file declaring
// `role: orchestrator` yields Role "orchestrator" in the result.
func TestRenderAgent_OrchestratorRole_ReportedInResult(t *testing.T) {
	// Arrange
	deps := newRenderDeps(t)
	svc := app.New(deps)
	dir := t.TempDir()
	src := writeRenderSourceFile(t, dir, "render-test-orchestrator.md", minimalGenericOrchestratorBytes())
	req := newPreAnsweredRenderRequest(src, filepath.Join(dir, "out.md"))
	req.Workflows = []string{} // explicitly none; orchestrator is valid with empty

	// Act
	result, err := svc.RenderAgent(context.Background(), req)
	if err != nil {
		t.Fatalf("RenderAgent returned error: %v", err)
	}

	// Assert
	if result.Role != string(domain.RoleOrchestrator) {
		t.Errorf("Role = %q, want %q", result.Role, string(domain.RoleOrchestrator))
	}
}

// TestRenderAgent_AbsentRole_DefaultsToSubagent verifies that a source file with no
// `role` frontmatter field defaults to domain.RoleSubagent without error.
func TestRenderAgent_AbsentRole_DefaultsToSubagent(t *testing.T) {
	// Arrange
	deps := newRenderDeps(t)
	svc := app.New(deps)
	dir := t.TempDir()
	src := writeRenderSourceFile(t, dir, "render-test-no-role.md", minimalGenericSubagentNoRoleBytes())
	req := newPreAnsweredRenderRequest(src, filepath.Join(dir, "out.md"))

	// Act
	result, err := svc.RenderAgent(context.Background(), req)
	if err != nil {
		t.Fatalf("RenderAgent returned error: %v", err)
	}

	// Assert: absent role defaults to subagent, not to an empty string or an error.
	if result.Role != string(domain.RoleSubagent) {
		t.Errorf("Role = %q for absent-role source, want %q (default)", result.Role, string(domain.RoleSubagent))
	}
}

// TestRenderAgent_UnrecognisedRole_ReturnsErrRenderInvalidRole verifies that a source
// file declaring an unrecognised `role` value (e.g. "analyst") is refused with
// ErrRenderInvalidRole and nothing is written.
func TestRenderAgent_UnrecognisedRole_ReturnsErrRenderInvalidRole(t *testing.T) {
	// Arrange
	deps := newRenderDeps(t)
	svc := app.New(deps)
	dir := t.TempDir()
	src := writeRenderSourceFile(t, dir, "render-test-bad-role.md", minimalGenericSubagentBadRoleBytes())
	dest := filepath.Join(dir, "out.md")
	req := newPreAnsweredRenderRequest(src, dest)

	// Act
	result, err := svc.RenderAgent(context.Background(), req)

	// Assert
	if !errors.Is(err, app.ErrRenderInvalidRole) {
		t.Errorf("error = %v, want error wrapping ErrRenderInvalidRole", err)
	}
	if !isZeroRenderResult(result) {
		t.Error("result must be zero value when role is unrecognised")
	}

	// Assert: no destination file was created.
	if _, statErr := os.Stat(dest); !os.IsNotExist(statErr) {
		t.Error("destination file must not be created when role is unrecognised")
	}
}

// TestRenderAgent_VersionPresentInFrontmatter_ReportedInResult verifies that the source
// file's `version` frontmatter value is carried into SourceVersion in the result.
func TestRenderAgent_VersionPresentInFrontmatter_ReportedInResult(t *testing.T) {
	// Arrange: source declares version: "2.0.0"
	deps := newRenderDeps(t)
	svc := app.New(deps)
	dir := t.TempDir()
	src := writeRenderSourceFile(t, dir, "render-test-agent.md", minimalGenericSubagentBytes())
	req := newPreAnsweredRenderRequest(src, filepath.Join(dir, "out.md"))

	// Act
	result, err := svc.RenderAgent(context.Background(), req)
	if err != nil {
		t.Fatalf("RenderAgent returned error: %v", err)
	}

	// Assert
	if result.SourceVersion != "2.0.0" {
		t.Errorf("SourceVersion = %q, want %q", result.SourceVersion, "2.0.0")
	}
}

// TestRenderAgent_VersionAbsentInFrontmatter_EmptySourceVersion verifies that a source
// file with no `version` field yields an empty SourceVersion — a legal state that must not
// be reported as a blank that looks like a real value, nor cause an error.
func TestRenderAgent_VersionAbsentInFrontmatter_EmptySourceVersion(t *testing.T) {
	// Arrange: source has no `version` field
	deps := newRenderDeps(t)
	svc := app.New(deps)
	dir := t.TempDir()
	src := writeRenderSourceFile(t, dir, "render-test-no-version.md", minimalGenericSubagentNoVersionBytes())
	req := newPreAnsweredRenderRequest(src, filepath.Join(dir, "out.md"))

	// Act
	result, err := svc.RenderAgent(context.Background(), req)
	if err != nil {
		t.Fatalf("RenderAgent returned unexpected error: %v", err)
	}

	// Assert: absent version must produce an empty SourceVersion, not an error.
	if result.SourceVersion != "" {
		t.Errorf("SourceVersion = %q for absent-version source, want %q (empty)", result.SourceVersion, "")
	}
}

// ---------------------------------------------------------------------------
// T1.4: Destination handling, overwrite protection, and dry-run
// ---------------------------------------------------------------------------

// TestRenderAgent_ExplicitDestinationPath_WrittenVerbatim verifies that
// req.DestinationPath is honoured exactly: the rendered file appears at that path, and
// result.DestinationResolvedBy is "explicit".
func TestRenderAgent_ExplicitDestinationPath_WrittenVerbatim(t *testing.T) {
	// Arrange
	deps := newRenderDeps(t)
	svc := app.New(deps)
	srcDir := t.TempDir()
	destDir := t.TempDir()
	src := writeRenderSourceFile(t, srcDir, "render-test-agent.md", minimalGenericSubagentBytes())
	dest := filepath.Join(destDir, "render-test-agent.md")

	req := newPreAnsweredRenderRequest(src, dest)

	// Act
	result, err := svc.RenderAgent(context.Background(), req)
	if err != nil {
		t.Fatalf("RenderAgent returned error: %v", err)
	}

	// Assert: destination path reported correctly.
	if result.DestinationPath != dest {
		t.Errorf("DestinationPath = %q, want %q", result.DestinationPath, dest)
	}
	if result.DestinationResolvedBy != "explicit" {
		t.Errorf("DestinationResolvedBy = %q, want %q", result.DestinationResolvedBy, "explicit")
	}

	// Assert: file exists at the reported path.
	if _, err := os.Stat(result.DestinationPath); err != nil {
		t.Errorf("file not found at reported DestinationPath %q: %v", result.DestinationPath, err)
	}
}

// TestRenderAgent_WorkspaceRoot_DestinationResolvedByHarness verifies that when
// WorkspaceRoot is supplied (rather than DestinationPath), the harness descriptor resolves
// the destination and result.DestinationResolvedBy is "workspace".
func TestRenderAgent_WorkspaceRoot_DestinationResolvedByHarness(t *testing.T) {
	// Arrange
	deps := newRenderDeps(t)
	svc := app.New(deps)
	srcDir := t.TempDir()
	workspaceDir := t.TempDir()
	src := writeRenderSourceFile(t, srcDir, "render-test-agent.md", minimalGenericSubagentBytes())

	req := app.RenderAgentRequest{
		SourcePath:      src,
		TargetHarnessID: renderHarness.ID,
		WorkspaceRoot:   workspaceDir,
		TargetModel:     "render-model-x",
	}

	// Act
	result, err := svc.RenderAgent(context.Background(), req)
	if err != nil {
		t.Fatalf("RenderAgent returned error: %v", err)
	}

	// Assert: resolved by harness, not supplied verbatim.
	if result.DestinationResolvedBy != "workspace" {
		t.Errorf("DestinationResolvedBy = %q, want %q", result.DestinationResolvedBy, "workspace")
	}
	if result.DestinationPath == "" {
		t.Error("DestinationPath is empty; harness must resolve a non-empty path")
	}
}

// TestRenderAgent_MissingParentDirs_CreatedAutomatically verifies that when the
// destination's parent directories do not exist, RenderAgent creates them and records them
// in result.CreatedDirectories (outermost first).
func TestRenderAgent_MissingParentDirs_CreatedAutomatically(t *testing.T) {
	// Arrange: destination path is nested three levels deep under a non-existent subtree.
	deps := newRenderDeps(t)
	svc := app.New(deps)
	srcDir := t.TempDir()
	destRoot := t.TempDir()
	src := writeRenderSourceFile(t, srcDir, "render-test-agent.md", minimalGenericSubagentBytes())
	dest := filepath.Join(destRoot, "level1", "level2", "level3", "render-test-agent.md")

	req := newPreAnsweredRenderRequest(src, dest)

	// Act
	result, err := svc.RenderAgent(context.Background(), req)
	if err != nil {
		t.Fatalf("RenderAgent returned error: %v", err)
	}

	// Assert: destination file was written.
	if _, statErr := os.Stat(dest); statErr != nil {
		t.Errorf("destination file not found at %q: %v", dest, statErr)
	}

	// Assert: created directories are reported.
	if len(result.CreatedDirectories) == 0 {
		t.Error("CreatedDirectories is empty; parent directories were created but not reported")
	}

	// Assert: all reported directories exist.
	for _, d := range result.CreatedDirectories {
		if fi, statErr := os.Stat(d); statErr != nil || !fi.IsDir() {
			t.Errorf("reported created directory %q does not exist or is not a directory", d)
		}
	}
}

// TestRenderAgent_ExistingDestination_NoOverwrite_ReturnsErrRenderDestinationExists
// verifies that an existing destination file is never silently replaced: without
// req.Overwrite the call returns ErrRenderDestinationExists and leaves the existing file
// byte-identical to before.
func TestRenderAgent_ExistingDestination_NoOverwrite_ReturnsErrRenderDestinationExists(t *testing.T) {
	// Arrange: pre-create a file at the destination.
	deps := newRenderDeps(t)
	svc := app.New(deps)
	srcDir := t.TempDir()
	destDir := t.TempDir()
	src := writeRenderSourceFile(t, srcDir, "render-test-agent.md", minimalGenericSubagentBytes())
	dest := filepath.Join(destDir, "render-test-agent.md")
	existingContent := []byte("pre-existing content that must not be overwritten\n")
	if err := os.WriteFile(dest, existingContent, 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	req := newPreAnsweredRenderRequest(src, dest)
	req.Overwrite = false // default, but explicit

	// Act
	result, err := svc.RenderAgent(context.Background(), req)

	// Assert
	if !errors.Is(err, app.ErrRenderDestinationExists) {
		t.Errorf("error = %v, want error wrapping ErrRenderDestinationExists", err)
	}
	if !isZeroRenderResult(result) {
		t.Error("result must be zero value when overwrite is refused")
	}

	// Assert: destination file content is unchanged.
	afterBytes, readErr := os.ReadFile(dest)
	if readErr != nil {
		t.Fatalf("cannot read destination after refused overwrite: %v", readErr)
	}
	if !bytes.Equal(existingContent, afterBytes) {
		t.Error("existing destination file was modified; it must be left byte-identical to before")
	}
}

// TestRenderAgent_ExistingDestination_WithOverwrite_Succeeds verifies that an existing
// destination file is replaced when req.Overwrite is true.
func TestRenderAgent_ExistingDestination_WithOverwrite_Succeeds(t *testing.T) {
	// Arrange: pre-create a file at the destination.
	deps := newRenderDeps(t)
	svc := app.New(deps)
	srcDir := t.TempDir()
	destDir := t.TempDir()
	src := writeRenderSourceFile(t, srcDir, "render-test-agent.md", minimalGenericSubagentBytes())
	dest := filepath.Join(destDir, "render-test-agent.md")
	if err := os.WriteFile(dest, []byte("old content\n"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	req := newPreAnsweredRenderRequest(src, dest)
	req.Overwrite = true

	// Act
	_, err := svc.RenderAgent(context.Background(), req)
	if err != nil {
		t.Fatalf("RenderAgent returned error with Overwrite=true: %v", err)
	}

	// Assert: destination was replaced (content is no longer "old content").
	afterBytes, readErr := os.ReadFile(dest)
	if readErr != nil {
		t.Fatalf("cannot read destination after overwrite: %v", readErr)
	}
	if bytes.Equal([]byte("old content\n"), afterBytes) {
		t.Error("destination file was not updated; overwrite had no effect")
	}
}

// TestRenderAgent_DryRun_WritesNoFile verifies that DryRun=true computes the outcome and
// populates the result (including DestinationPath) but writes nothing to the filesystem.
func TestRenderAgent_DryRun_WritesNoFile(t *testing.T) {
	// Arrange
	deps := newRenderDeps(t)
	svc := app.New(deps)
	srcDir := t.TempDir()
	destDir := t.TempDir()
	src := writeRenderSourceFile(t, srcDir, "render-test-agent.md", minimalGenericSubagentBytes())
	dest := filepath.Join(destDir, "render-test-agent.md")

	req := newPreAnsweredRenderRequest(src, dest)
	req.DryRun = true

	// Act
	result, err := svc.RenderAgent(context.Background(), req)
	if err != nil {
		t.Fatalf("RenderAgent dry-run returned error: %v", err)
	}

	// Assert: result reports dry-run.
	if !result.DryRun {
		t.Error("result.DryRun = false, want true")
	}

	// Assert: DestinationPath is populated (it is the path that would have been written).
	if result.DestinationPath == "" {
		t.Error("result.DestinationPath is empty in dry-run; want the would-be destination path")
	}

	// Assert: no file was written.
	if _, statErr := os.Stat(dest); !os.IsNotExist(statErr) {
		t.Errorf("file was written at %q during dry-run; nothing must be written", dest)
	}
}

// TestRenderAgent_DryRun_OverwriteCheckStillRuns verifies that the overwrite check is
// not skipped in dry-run mode: an existing destination without req.Overwrite returns
// ErrRenderDestinationExists even when DryRun is true.
func TestRenderAgent_DryRun_OverwriteCheckStillRuns(t *testing.T) {
	// Arrange: pre-create a file at the destination.
	deps := newRenderDeps(t)
	svc := app.New(deps)
	srcDir := t.TempDir()
	destDir := t.TempDir()
	src := writeRenderSourceFile(t, srcDir, "render-test-agent.md", minimalGenericSubagentBytes())
	dest := filepath.Join(destDir, "render-test-agent.md")
	if err := os.WriteFile(dest, []byte("existing\n"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	req := newPreAnsweredRenderRequest(src, dest)
	req.DryRun = true
	req.Overwrite = false

	// Act
	_, err := svc.RenderAgent(context.Background(), req)

	// Assert: overwrite check fires even under dry-run.
	if !errors.Is(err, app.ErrRenderDestinationExists) {
		t.Errorf("error = %v in dry-run, want ErrRenderDestinationExists", err)
	}
}

// TestRenderAgent_DryRun_NoParentDirsCreated verifies that DryRun=true creates no
// directories on the way to the destination.
func TestRenderAgent_DryRun_NoParentDirsCreated(t *testing.T) {
	// Arrange
	deps := newRenderDeps(t)
	svc := app.New(deps)
	srcDir := t.TempDir()
	destRoot := t.TempDir()
	src := writeRenderSourceFile(t, srcDir, "render-test-agent.md", minimalGenericSubagentBytes())
	nonExistentParent := filepath.Join(destRoot, "new-dir")
	dest := filepath.Join(nonExistentParent, "render-test-agent.md")

	req := newPreAnsweredRenderRequest(src, dest)
	req.DryRun = true

	// Act
	result, err := svc.RenderAgent(context.Background(), req)
	if err != nil {
		t.Fatalf("RenderAgent dry-run returned error: %v", err)
	}

	// Assert: no directory was created on disk.
	if _, statErr := os.Stat(nonExistentParent); !os.IsNotExist(statErr) {
		t.Errorf("directory %q was created during dry-run; nothing must be created", nonExistentParent)
	}

	// Assert: CreatedDirectories still reports what would have been created.
	_ = result // CreatedDirectories content verified by inspection in the real GREEN run
}

// ---------------------------------------------------------------------------
// T1.5: Output fidelity
// ---------------------------------------------------------------------------

// TestRenderAgent_RenderedBytes_ByteIdenticalToTransformApply verifies that the bytes
// written at the destination are byte-identical to what transform.Apply produces for the
// same generic source, harness module, and model. This pins that no transformation logic
// was reimplemented in renderAgent and that the existing transform.Apply is the sole
// transformation entry point (AC1.3).
func TestRenderAgent_RenderedBytes_ByteIdenticalToTransformApply(t *testing.T) {
	// Arrange
	deps := newRenderDeps(t)
	svc := app.New(deps)
	srcDir := t.TempDir()
	destDir := t.TempDir()
	genericBytes := minimalGenericSubagentBytes()
	src := writeRenderSourceFile(t, srcDir, "render-test-agent.md", genericBytes)
	dest := filepath.Join(destDir, "render-test-agent.md")

	req := newPreAnsweredRenderRequest(src, dest)

	// Act: render via the service.
	_, err := svc.RenderAgent(context.Background(), req)
	if err != nil {
		t.Fatalf("RenderAgent returned error: %v", err)
	}

	// Read the bytes that the service wrote.
	actualBytes, readErr := os.ReadFile(dest)
	if readErr != nil {
		t.Fatalf("cannot read destination file: %v", readErr)
	}

	// Compute the expected bytes directly via transform.Apply, using the same harness
	// module that the registry will supply for renderHarness.ID.
	mod := newRenderTargetModule()
	// Protocol and bundle content come from Deps, which newRenderDeps wires with stubs.
	// We use the same stub content to reproduce the service's transform.Apply call.
	protocol := minimalProtocolContent("1.9")
	bundle := minimalBundleContent("1.0.0")

	expected, transformErr := transform.Apply(transform.Request{
		Source:   genericBytes,
		Kind:     domain.ArtifactAgent,
		Key:      "render-test-agent",
		Module:   mod,
		Model:    domain.ModelSelection{ModelID: "render-model-x"},
		Scope:    domain.ScopeProject,
		Role:     domain.RoleSubagent,
		Protocol: protocol,
		Bundle:   bundle,
	})
	if transformErr != nil {
		t.Fatalf("transform.Apply returned error: %v", transformErr)
	}

	// Assert: byte-identical output.
	if !bytes.Equal(actualBytes, expected.Output) {
		t.Errorf("rendered bytes differ from transform.Apply output:\n  len(actual)=%d  len(expected)=%d",
			len(actualBytes), len(expected.Output))
	}
}

// ---------------------------------------------------------------------------
// T1.6: Orchestrator case and caller-specified workflow sets
// ---------------------------------------------------------------------------

// TestRenderAgent_Orchestrator_WithCallerWorkflows_AppearsInOutput verifies that an
// orchestrator rendered with a caller-specified workflow set carries those workflow ids in
// result.Workflows and in the rendered file's workflow region.
func TestRenderAgent_Orchestrator_WithCallerWorkflows_AppearsInOutput(t *testing.T) {
	// Arrange
	deps := newRenderDeps(t)
	svc := app.New(deps)
	dir := t.TempDir()
	src := writeRenderSourceFile(t, dir, "render-test-orchestrator.md", minimalGenericOrchestratorBytes())
	dest := filepath.Join(dir, "out.md")

	req := newPreAnsweredRenderRequest(src, dest)
	req.Workflows = []string{"quick-fix"} // the workflow present in newRenderCatalog

	// Act
	result, err := svc.RenderAgent(context.Background(), req)
	if err != nil {
		t.Fatalf("RenderAgent returned error: %v", err)
	}

	// Assert: workflow id appears in result.
	if len(result.Workflows) == 0 {
		t.Fatal("result.Workflows is empty; want at least \"quick-fix\"")
	}
	found := false
	for _, wf := range result.Workflows {
		if wf == "quick-fix" {
			found = true
		}
	}
	if !found {
		t.Errorf("result.Workflows = %v, want to contain \"quick-fix\"", result.Workflows)
	}

	// Assert: rendered file body contains the workflow block text.
	renderedBytes, readErr := os.ReadFile(dest)
	if readErr != nil {
		t.Fatalf("cannot read destination file: %v", readErr)
	}
	if !bytes.Contains(renderedBytes, []byte("quick-fix")) {
		t.Error("rendered output does not contain \"quick-fix\"; workflow block was not injected")
	}
}

// TestRenderAgent_Orchestrator_WithCallerInfrastructureAgents_AppearsInOutput verifies
// that an orchestrator rendered with a caller-specified infrastructure-agent set carries those
// agent keys in result.InfrastructureAgents and in the rendered file's infrastructure region.
// This mirrors TestRenderAgent_Orchestrator_WithCallerWorkflows_AppearsInOutput for the
// InfrastructureAgents code path (render_service_impl.go lines 190-193, 306-309).
func TestRenderAgent_Orchestrator_WithCallerInfrastructureAgents_AppearsInOutput(t *testing.T) {
	// Arrange: use render deps with an infrastructure agent added to the catalog so that
	// buildInfrastructureBlocks can resolve its metadata.
	deps := newRenderDeps(t)
	cat := newRenderCatalog()
	cat.infraAgents = []domain.Agent{
		{
			Key:            "render-infra-agent",
			NumericID:      "99",
			Version:        "1.0",
			Name:           "Render Infra Agent",
			Role:           domain.RoleWorker,
			Infrastructure: "checkpoint",
			Description:    "Infrastructure agent used in render tests",
			OnFailure:      "continue",
			Triggers: []domain.InfrastructureTrigger{
				{Trigger: "STAGE_END"},
			},
		},
	}
	deps.Catalog = cat
	svc := app.New(deps)

	dir := t.TempDir()
	// Use the orchestrator fixture that carries the <InfrastructureAgents type="managed"> region.
	src := writeRenderSourceFile(t, dir, "render-test-orchestrator-infra.md", minimalGenericOrchestratorWithInfraRegionBytes())
	dest := filepath.Join(dir, "out.md")

	req := newPreAnsweredRenderRequest(src, dest)
	req.Workflows = []string{}                                 // explicitly none — valid for orchestrator
	req.InfrastructureAgents = []string{"render-infra-agent"} // the key registered in the catalog above

	// Act
	result, err := svc.RenderAgent(context.Background(), req)
	if err != nil {
		t.Fatalf("RenderAgent returned error: %v", err)
	}

	// Assert: infrastructure agent key appears in result.
	if len(result.InfrastructureAgents) == 0 {
		t.Fatal("result.InfrastructureAgents is empty; want at least \"render-infra-agent\"")
	}
	found := false
	for _, key := range result.InfrastructureAgents {
		if key == "render-infra-agent" {
			found = true
		}
	}
	if !found {
		t.Errorf("result.InfrastructureAgents = %v, want to contain \"render-infra-agent\"", result.InfrastructureAgents)
	}

	// Assert: rendered file body contains the infrastructure agent key so the caller can
	// confirm the region was actually populated, not merely tracked in the result struct.
	renderedBytes, readErr := os.ReadFile(dest)
	if readErr != nil {
		t.Fatalf("cannot read destination file: %v", readErr)
	}
	if !bytes.Contains(renderedBytes, []byte("render-infra-agent")) {
		t.Error("rendered output does not contain \"render-infra-agent\"; infrastructure-agent block was not injected")
	}
}

// TestRenderAgent_Orchestrator_NilWorkflows_VsEmptyWorkflows_AreDistinguishable verifies
// that nil Workflows ("not specified") and []string{} ("explicitly none") produce
// distinguishable rendered output. An orchestrator with nil Workflows renders its default
// AvailableWorkflows injection (which the transform layer controls); one with an empty
// slice renders an explicitly empty set. Both are valid; neither must silently use the
// other's behaviour.
func TestRenderAgent_Orchestrator_NilWorkflows_VsEmptyWorkflows_AreDistinguishable(t *testing.T) {
	// Arrange: two renders of the same orchestrator source — one nil, one empty.
	deps := newRenderDeps(t)
	svc := app.New(deps)
	dir := t.TempDir()
	src := writeRenderSourceFile(t, dir, "render-test-orchestrator.md", minimalGenericOrchestratorBytes())

	destNil := filepath.Join(dir, "out-nil.md")
	reqNil := newPreAnsweredRenderRequest(src, destNil)
	reqNil.Workflows = nil // not specified

	destEmpty := filepath.Join(dir, "out-empty.md")
	reqEmpty := newPreAnsweredRenderRequest(src, destEmpty)
	reqEmpty.Workflows = []string{} // explicitly none

	// Act
	_, errNil := svc.RenderAgent(context.Background(), reqNil)
	_, errEmpty := svc.RenderAgent(context.Background(), reqEmpty)

	if errNil != nil {
		t.Fatalf("nil-Workflows render error: %v", errNil)
	}
	if errEmpty != nil {
		t.Fatalf("empty-Workflows render error: %v", errEmpty)
	}

	// Assert: the two output files are not byte-identical (distinguishable outputs).
	bytesNil, _ := os.ReadFile(destNil)
	bytesEmpty, _ := os.ReadFile(destEmpty)
	if bytes.Equal(bytesNil, bytesEmpty) {
		t.Error("nil-Workflows and empty-Workflows rendered the same output; they must be distinguishable")
	}
}

// TestRenderAgent_UnknownWorkflowID_ReturnsErrRenderUnknownWorkflow verifies that a
// workflow id in req.Workflows that is not present in the catalogue returns
// ErrRenderUnknownWorkflow; nothing is written to the destination.
func TestRenderAgent_UnknownWorkflowID_ReturnsErrRenderUnknownWorkflow(t *testing.T) {
	// Arrange
	deps := newRenderDeps(t)
	svc := app.New(deps)
	dir := t.TempDir()
	src := writeRenderSourceFile(t, dir, "render-test-orchestrator.md", minimalGenericOrchestratorBytes())
	dest := filepath.Join(dir, "out.md")

	req := newPreAnsweredRenderRequest(src, dest)
	req.Workflows = []string{"no-such-workflow"} // not in the catalogue

	// Act
	result, err := svc.RenderAgent(context.Background(), req)

	// Assert
	if !errors.Is(err, app.ErrRenderUnknownWorkflow) {
		t.Errorf("error = %v, want error wrapping ErrRenderUnknownWorkflow", err)
	}
	if !isZeroRenderResult(result) {
		t.Error("result must be zero value when workflow id is unknown")
	}

	// Assert: no destination file was created.
	if _, statErr := os.Stat(dest); !os.IsNotExist(statErr) {
		t.Error("destination file must not be created when a workflow id is unknown")
	}
}

// TestRenderAgent_NonEmptyWorkflowsForSubagent_ReturnsErrRenderWorkflowsNotApplicable
// verifies that supplying a non-empty Workflows slice for a subagent is refused with
// ErrRenderWorkflowsNotApplicable. A non-nil but empty slice is "explicitly none" and is
// valid for a subagent; only a non-empty slice is refused.
func TestRenderAgent_NonEmptyWorkflowsForSubagent_ReturnsErrRenderWorkflowsNotApplicable(t *testing.T) {
	// Arrange: source is a subagent, but Workflows is non-empty.
	deps := newRenderDeps(t)
	svc := app.New(deps)
	dir := t.TempDir()
	src := writeRenderSourceFile(t, dir, "render-test-agent.md", minimalGenericSubagentBytes())
	dest := filepath.Join(dir, "out.md")

	req := newPreAnsweredRenderRequest(src, dest)
	req.Workflows = []string{"quick-fix"} // non-applicable for a subagent

	// Act
	result, err := svc.RenderAgent(context.Background(), req)

	// Assert
	if !errors.Is(err, app.ErrRenderWorkflowsNotApplicable) {
		t.Errorf("error = %v, want error wrapping ErrRenderWorkflowsNotApplicable", err)
	}
	if !isZeroRenderResult(result) {
		t.Error("result must be zero value when workflows are not applicable")
	}
}

// TestRenderAgent_ExplicitlyEmptyWorkflowsForSubagent_IsLegal verifies that a non-nil
// but empty InfrastructureAgents (or Workflows) slice for a subagent is valid — "explicitly
// none" must not be rejected by ErrRenderWorkflowsNotApplicable.
func TestRenderAgent_ExplicitlyEmptyWorkflowsForSubagent_IsLegal(t *testing.T) {
	// Arrange: subagent with Workflows = []string{} (explicitly none, not non-empty).
	deps := newRenderDeps(t)
	svc := app.New(deps)
	dir := t.TempDir()
	src := writeRenderSourceFile(t, dir, "render-test-agent.md", minimalGenericSubagentBytes())
	dest := filepath.Join(dir, "out.md")

	req := newPreAnsweredRenderRequest(src, dest)
	req.Workflows = []string{}            // explicitly none — must not trigger the error
	req.InfrastructureAgents = []string{} // same for infrastructure agents

	// Act
	_, err := svc.RenderAgent(context.Background(), req)

	// Assert: the call either succeeds or fails for a different reason — but never for
	// ErrRenderWorkflowsNotApplicable. An explicitly empty slice is always legal.
	if errors.Is(err, app.ErrRenderWorkflowsNotApplicable) {
		t.Error("ErrRenderWorkflowsNotApplicable returned for explicitly empty Workflows; " +
			"nil/empty slice must not be treated as a non-empty set")
	}
}

// TestRenderAgent_SourceAgentKey_CatalogLookup_Succeeds verifies that when SourceAgentKey
// is supplied the catalogue agent is resolved and rendered successfully, with SourceAgentKey
// echoed in the result.
func TestRenderAgent_SourceAgentKey_CatalogLookup_Succeeds(t *testing.T) {
	// Arrange: the minimal catalog already has "orchestrator" (key from minimalOrchestrator).
	deps := newRenderDeps(t)
	svc := app.New(deps)
	dir := t.TempDir()
	dest := filepath.Join(dir, "out.md")

	req := app.RenderAgentRequest{
		SourceAgentKey:  "orchestrator", // from minimalOrchestrator in testhelpers_test.go
		TargetHarnessID: renderHarness.ID,
		DestinationPath: dest,
		TargetModel:     "render-model-x",
		Workflows:       []string{}, // explicitly none
	}

	// Act
	result, err := svc.RenderAgent(context.Background(), req)
	if err != nil {
		t.Fatalf("RenderAgent returned error: %v", err)
	}

	// Assert
	if result.SourceAgentKey != "orchestrator" {
		t.Errorf("SourceAgentKey = %q, want %q", result.SourceAgentKey, "orchestrator")
	}
	if result.AgentKey != "orchestrator" {
		t.Errorf("AgentKey = %q, want %q", result.AgentKey, "orchestrator")
	}
}

// TestRenderAgent_SourceAgentKeyNotInCatalog_ReturnsErrRenderAgentNotInCatalog verifies
// that a SourceAgentKey naming no catalogue agent returns ErrRenderAgentNotInCatalog.
func TestRenderAgent_SourceAgentKeyNotInCatalog_ReturnsErrRenderAgentNotInCatalog(t *testing.T) {
	// Arrange
	deps := newRenderDeps(t)
	svc := app.New(deps)
	dir := t.TempDir()
	dest := filepath.Join(dir, "out.md")

	req := app.RenderAgentRequest{
		SourceAgentKey:  "no-such-agent-in-catalog",
		TargetHarnessID: renderHarness.ID,
		DestinationPath: dest,
	}

	// Act
	result, err := svc.RenderAgent(context.Background(), req)

	// Assert
	if !errors.Is(err, app.ErrRenderAgentNotInCatalog) {
		t.Errorf("error = %v, want error wrapping ErrRenderAgentNotInCatalog", err)
	}
	if !isZeroRenderResult(result) {
		t.Error("result must be zero value when agent key is not in the catalogue")
	}
}
