package runner_test

// Tests for {run_id} placeholder expansion in seed file content (T3.1).
//
// The runner's seedFile function currently expands {run_id} in seed file
// paths but not in seed file content. The tests below assert that content
// (both inline and $ref-resolved) is expanded before writing, and that
// content without the placeholder is left unchanged.
//
// RED phase: these tests fail until I3.1 is implemented — seedFile applies
// strings.ReplaceAll to the content bytes before os.WriteFile.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/fixtures"
	"mosaic-agent-test/internal/runner"
	"mosaic-agent-test/internal/sideeffects"
)

const seedContentRunID = "20260823T210751Z-a854"

// TestRun_SeedFile_InlineContentWithRunIDPlaceholder_IsExpanded asserts
// that a seed file's inline Content field has every occurrence of {run_id}
// replaced by the actual run ID before the file is written to disk. The path
// is already expanded; the new requirement is that the content follows.
func TestRun_SeedFile_InlineContentWithRunIDPlaceholder_IsExpanded(t *testing.T) {
	h := newHarness(t)
	req := newRequest("seedfile-content-runid")
	req.Key.RunID = seedContentRunID
	req.Test.Definition.SeedFiles = []domain.SeedFile{
		{
			Path:    "Orchestration/Plan.md",
			Content: "This plan belongs to run " + domain.RunIDPlaceholder + ". Refer to Orchestration-" + domain.RunIDPlaceholder + "/",
		},
	}

	h.Launcher.launchFn = func(ctx context.Context, plan domain.SpawnPlan) (domain.SubjectResult, error) {
		sb := h.Adapter.lastProvisionReq.Sandbox
		got, err := os.ReadFile(filepath.Join(sb.SubjectDir, "Orchestration", "Plan.md"))
		if err != nil {
			t.Fatalf("seed file not found on disk: %v", err)
		}
		if strings.Contains(string(got), domain.RunIDPlaceholder) {
			t.Errorf("seed file content still contains literal placeholder %q — it must be expanded to %q before writing; got content: %q",
				domain.RunIDPlaceholder, seedContentRunID, got)
		}
		if !strings.Contains(string(got), seedContentRunID) {
			t.Errorf("seed file content does not contain the actual run ID %q; got content: %q",
				seedContentRunID, got)
		}
		return domain.SubjectResult{Disposition: domain.DispositionCompleted}, nil
	}

	if _, err := runner.Run(context.Background(), h.Deps, req, nil); err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}
}

// TestRun_SeedFile_InlineContentWithMultipleRunIDOccurrences_AllAreExpanded
// asserts that when the inline Content field contains the {run_id} placeholder
// more than once, every occurrence is replaced, not only the first.
func TestRun_SeedFile_InlineContentWithMultipleRunIDOccurrences_AllAreExpanded(t *testing.T) {
	h := newHarness(t)
	req := newRequest("seedfile-content-multi-runid")
	req.Key.RunID = seedContentRunID
	// Three occurrences of the placeholder in a realistic multi-line content body.
	req.Test.Definition.SeedFiles = []domain.SeedFile{
		{
			Path: "notes/summary.md",
			Content: "run_id: " + domain.RunIDPlaceholder + "\n" +
				"path: Orchestration-" + domain.RunIDPlaceholder + "/\n" +
				"log: OrchestrationLogs/" + domain.RunIDPlaceholder + "/\n",
		},
	}

	h.Launcher.launchFn = func(ctx context.Context, plan domain.SpawnPlan) (domain.SubjectResult, error) {
		sb := h.Adapter.lastProvisionReq.Sandbox
		got, err := os.ReadFile(filepath.Join(sb.SubjectDir, "notes", "summary.md"))
		if err != nil {
			t.Fatalf("seed file not found on disk: %v", err)
		}
		if strings.Contains(string(got), domain.RunIDPlaceholder) {
			t.Errorf("seed file content still contains the placeholder — all occurrences must be expanded; got: %q", got)
		}
		occurrences := strings.Count(string(got), seedContentRunID)
		if occurrences != 3 {
			t.Errorf("seed file content contains the run ID %d time(s), want 3 (one per occurrence of the placeholder); got: %q",
				occurrences, got)
		}
		return domain.SubjectResult{Disposition: domain.DispositionCompleted}, nil
	}

	if _, err := runner.Run(context.Background(), h.Deps, req, nil); err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}
}

// TestRun_SeedFile_InlineContentWithoutRunIDPlaceholder_IsWrittenUnchanged
// asserts that a seed file whose inline content does not contain {run_id}
// is written byte-identical to what was declared — expansion is a no-op
// when the placeholder is absent.
func TestRun_SeedFile_InlineContentWithoutRunIDPlaceholder_IsWrittenUnchanged(t *testing.T) {
	h := newHarness(t)
	req := newRequest("seedfile-content-no-placeholder")
	req.Key.RunID = seedContentRunID
	const declaredContent = "# Static Plan\n\nThis content has no placeholders.\n"
	req.Test.Definition.SeedFiles = []domain.SeedFile{
		{Path: "static/plan.md", Content: declaredContent},
	}

	h.Launcher.launchFn = func(ctx context.Context, plan domain.SpawnPlan) (domain.SubjectResult, error) {
		sb := h.Adapter.lastProvisionReq.Sandbox
		got, err := os.ReadFile(filepath.Join(sb.SubjectDir, "static", "plan.md"))
		if err != nil {
			t.Fatalf("seed file not found on disk: %v", err)
		}
		if string(got) != declaredContent {
			t.Errorf("seed file content = %q, want it byte-identical to the declared content %q — placeholder-free content must be left unchanged",
				got, declaredContent)
		}
		return domain.SubjectResult{Disposition: domain.DispositionCompleted}, nil
	}

	if _, err := runner.Run(context.Background(), h.Deps, req, nil); err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}
}

// TestRun_SeedFile_RefResolvedContentWithRunIDPlaceholder_IsExpanded asserts
// that when a seed file uses a $ref to source its content from a fixture
// file, and that fixture file contains {run_id}, the placeholder is expanded
// before the file is written to disk. Expansion happens after $ref resolution,
// so the content the runner writes is fully substituted.
func TestRun_SeedFile_RefResolvedContentWithRunIDPlaceholder_IsExpanded(t *testing.T) {
	// testDefDir represents the directory of the test definition. The fixture
	// file lives beside the definition so per-document resolution finds it.
	testDefDir := t.TempDir()
	defPath := filepath.Join(testDefDir, "my-test.test.yaml")

	// The fixture content contains {run_id} in two places — both must be
	// expanded to the actual run ID by the time the file is written.
	fixtureContent := "# Plan for run " + domain.RunIDPlaceholder + "\n\nPath: Orchestration-" + domain.RunIDPlaceholder + "/Plan.md\n"
	if err := os.WriteFile(filepath.Join(testDefDir, "plan-template.md"), []byte(fixtureContent), 0o644); err != nil {
		t.Fatalf("WriteFile(plan-template.md): %v", err)
	}

	// sharedRoot is an empty directory that does not contain the fixture,
	// confirming that per-document resolution (not the shared root) found it.
	sharedRoot := t.TempDir()
	resolver, err := fixtures.NewResolver(sharedRoot)
	if err != nil {
		t.Fatalf("fixtures.NewResolver(%q): %v", sharedRoot, err)
	}
	effects := sideeffects.NewApplier(resolver)
	factory, err := fixtures.NewResolverFactory(sharedRoot)
	if err != nil {
		t.Fatalf("fixtures.NewResolverFactory(%q): %v", sharedRoot, err)
	}

	h := newHarness(t)
	h.Deps.Fixtures = resolver
	h.Deps.Effects = effects
	h.Deps.FixtureFactory = factory

	req := newRequest("seedfile-ref-content-runid")
	req.Key.RunID = seedContentRunID
	req.Test.Definition.SourcePath = defPath
	req.Test.Definition.SeedFiles = []domain.SeedFile{
		{Path: "Orchestration/Plan.md", Ref: "plan-template.md"},
	}

	h.Launcher.launchFn = func(ctx context.Context, plan domain.SpawnPlan) (domain.SubjectResult, error) {
		sb := h.Adapter.lastProvisionReq.Sandbox
		got, err := os.ReadFile(filepath.Join(sb.SubjectDir, "Orchestration", "Plan.md"))
		if err != nil {
			t.Fatalf("seed file not found on disk — $ref resolution or writing failed: %v", err)
		}
		if strings.Contains(string(got), domain.RunIDPlaceholder) {
			t.Errorf("$ref-resolved seed file content still contains literal placeholder %q — it must be expanded to %q before writing; got: %q",
				domain.RunIDPlaceholder, seedContentRunID, got)
		}
		if !strings.Contains(string(got), seedContentRunID) {
			t.Errorf("$ref-resolved seed file content does not contain the actual run ID %q; got: %q",
				seedContentRunID, got)
		}
		return domain.SubjectResult{Disposition: domain.DispositionCompleted}, nil
	}

	if _, err := runner.Run(context.Background(), h.Deps, req, nil); err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}
}
