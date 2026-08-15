package runner_test

// Tests for the subject version field in RunEvidence.
//
// On the catalogue path, subject version is always recorded as empty because
// the Deploy subcommand reports no per-agent version. On the stub-agents path,
// the subject is not rendered (it uses a pre-existing DefinitionPath), so no
// version is captured from a Render call either. Stub renders' SourceVersion
// values belong to stubs, not to the subject, and must not flow into
// RunEvidence.SubjectVersion.
//
// These tests drive runner.Run with a configured stubDeployer and assert the
// observable state of domain.RunEvidence.SubjectVersion. They say nothing
// about the intermediate storage the implementation uses.

import (
	"context"
	"path/filepath"
	"testing"

	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/runner"
)

// TestRun_StubRenderVersionDoesNotFlowIntoSubjectVersionEvidence asserts that
// on the stub-agents path, a SourceVersion returned by Deploy.Render for a
// stub agent does not flow into RunEvidence.SubjectVersion. The subject uses a
// pre-existing DefinitionPath and is not rendered; only stubs are rendered.
// Stub versions belong to the stub, not to the subject under test.
func TestRun_StubRenderVersionDoesNotFlowIntoSubjectVersionEvidence(t *testing.T) {
	// Arrange
	h := newHarness(t)
	req := stubAgentsRenderRequest("evidence-stub-version-not-subject")

	// Configure the stub render to return a non-empty SourceVersion. This must
	// not appear in evidence.SubjectVersion — that field is for the subject only.
	h.Deployer.renderFn = func(r domain.RenderAgentRequest) (domain.RenderAgentResult, error) {
		return domain.RenderAgentResult{
			DestinationPath: filepath.Join(r.WorkspaceRoot, "researcher.md"),
			SourceVersion:   "v2.5.0",
		}, nil
	}

	// Act
	evidence, err := runner.Run(context.Background(), h.Deps, req)

	// Assert
	if err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}
	if evidence.SubjectVersion != "" {
		t.Errorf("evidence.SubjectVersion = %q, want empty string; "+
			"a stub agent's SourceVersion must not flow into SubjectVersion — "+
			"the subject is not rendered on the stub-agents path and its version "+
			"is not available from stub renders",
			evidence.SubjectVersion)
	}
}

// TestRun_AbsentSubjectVersionCarriedAsEmptyInEvidence asserts that when the
// deployment port returns an empty SourceVersion — a legal state meaning the
// source file declared no version — RunEvidence.SubjectVersion is also empty.
// The "" → "unknown" mapping is the report layer's responsibility, not the
// runner's; the runner carries what the port reported without transforming it.
func TestRun_AbsentSubjectVersionCarriedAsEmptyInEvidence(t *testing.T) {
	// Arrange
	h := newHarness(t)
	req := renderingRequest("evidence-carries-absent-subject-version")

	h.Deployer.renderFn = func(r domain.RenderAgentRequest) (domain.RenderAgentResult, error) {
		return domain.RenderAgentResult{
			DestinationPath: filepath.Join(r.WorkspaceRoot, r.CatalogAgentKey+".md"),
			AgentKey:        r.CatalogAgentKey,
			SourceVersion:   "", // source declared no version — a legal state
		}, nil
	}

	// Act
	evidence, err := runner.Run(context.Background(), h.Deps, req)

	// Assert
	if err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}
	if evidence.SubjectVersion != "" {
		t.Errorf("evidence.SubjectVersion = %q, want empty string; "+
			"an absent source version must be carried as empty in RunEvidence — "+
			"the report layer renders empty as 'unknown', not the runner",
			evidence.SubjectVersion)
	}
}

// TestRun_StubAgentsPathSubjectVersionIsAlwaysEmpty asserts that on the
// stub-agents path, RunEvidence.SubjectVersion is always empty regardless of
// any SourceVersion values returned by stub renders. The subject is not
// rendered on this path (it uses a pre-existing DefinitionPath), so no version
// is captured. The report layer renders an empty value as "unknown".
func TestRun_StubAgentsPathSubjectVersionIsAlwaysEmpty(t *testing.T) {
	// Arrange
	h := newHarness(t)
	req := stubAgentsRenderRequest("evidence-stub-agents-path-version-empty")

	// Configure the stub render to return a non-trivial SourceVersion string
	// with special characters to prove no transformation or pass-through happens.
	h.Deployer.renderFn = func(r domain.RenderAgentRequest) (domain.RenderAgentResult, error) {
		return domain.RenderAgentResult{
			DestinationPath: filepath.Join(r.WorkspaceRoot, "researcher.md"),
			SourceVersion:   "v3.14.159-alpha+build.42",
		}, nil
	}

	// Act
	evidence, err := runner.Run(context.Background(), h.Deps, req)

	// Assert
	if err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}
	if evidence.SubjectVersion != "" {
		t.Errorf("evidence.SubjectVersion = %q, want empty string; "+
			"on the stub-agents path the subject is not rendered and its version "+
			"is not available — SubjectVersion must be empty (reported as 'unknown' "+
			"by the report layer), never populated from a stub render result",
			evidence.SubjectVersion)
	}
}
