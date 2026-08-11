package runner_test

// Tests for the subject version being captured from the deployment port and
// carried into RunEvidence (TDD RED phase).
//
// The subject's source version is captured at render time from the deployment
// port's own result, not re-derived later. This is the only moment the value
// is available: the rendered file in the sandbox is gone by report time, and
// re-deriving it from the rendered file could disagree with what was actually
// deployed.
//
// These tests drive runner.Run with a configured stubDeployer and assert that
// domain.RunEvidence.SubjectVersion equals what the port reported. They say
// nothing about the intermediate storage the implementation uses — only the
// observable outcome matters.

import (
	"context"
	"path/filepath"
	"testing"

	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/runner"
)

// TestRun_SubjectVersionFromDeployPortCarriedIntoEvidence asserts that when
// the deployment port returns a non-empty SourceVersion for the subject
// render, that version appears in the resulting RunEvidence.SubjectVersion.
func TestRun_SubjectVersionFromDeployPortCarriedIntoEvidence(t *testing.T) {
	// Arrange
	h := newHarness(t)
	req := renderingRequest("evidence-carries-subject-version")

	const wantVersion = "v2.5.0"
	h.Deployer.renderFn = func(r domain.RenderAgentRequest) (domain.RenderAgentResult, error) {
		return domain.RenderAgentResult{
			DestinationPath: filepath.Join(r.WorkspaceRoot, r.CatalogAgentKey+".md"),
			AgentKey:        r.CatalogAgentKey,
			SourceVersion:   wantVersion,
		}, nil
	}

	// Act
	evidence, err := runner.Run(context.Background(), h.Deps, req)

	// Assert
	if err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}
	if evidence.SubjectVersion != wantVersion {
		t.Errorf("evidence.SubjectVersion = %q, want %q; "+
			"the version returned by Deploy.Render for the subject must be "+
			"captured at render time and carried into RunEvidence, so a report "+
			"can attribute a result to a specific version of the agent under test",
			evidence.SubjectVersion, wantVersion)
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

// TestRun_SubjectVersionPreservesExactStringFromPort asserts that the version
// string stored in RunEvidence is byte-identical to what the port returned.
// The runner must not normalise, trim, or transform the value in any way.
func TestRun_SubjectVersionPreservesExactStringFromPort(t *testing.T) {
	// Arrange
	h := newHarness(t)
	req := renderingRequest("evidence-preserves-exact-subject-version")

	// Use a version string with non-trivial characters to prove no
	// transformation happens.
	const wantVersion = "v3.14.159-alpha+build.42"
	h.Deployer.renderFn = func(r domain.RenderAgentRequest) (domain.RenderAgentResult, error) {
		return domain.RenderAgentResult{
			DestinationPath: filepath.Join(r.WorkspaceRoot, r.CatalogAgentKey+".md"),
			AgentKey:        r.CatalogAgentKey,
			SourceVersion:   wantVersion,
		}, nil
	}

	// Act
	evidence, err := runner.Run(context.Background(), h.Deps, req)

	// Assert
	if err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}
	if evidence.SubjectVersion != wantVersion {
		t.Errorf("evidence.SubjectVersion = %q, want exactly %q; "+
			"the runner must carry the version string byte-identical from the "+
			"port's result — no normalisation, trimming, or transformation",
			evidence.SubjectVersion, wantVersion)
	}
}
