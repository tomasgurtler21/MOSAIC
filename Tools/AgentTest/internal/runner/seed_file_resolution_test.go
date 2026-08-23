package runner_test

// Tests for runtime seed-file resolution (AC1.8).
//
// The runner must resolve seed_files[].ref against the test definition's own
// directory rather than a process-wide fixture root, so a suite that passes
// preflight does not fail at runtime due to fixture resolution.
//
// RED phase: the test below fails because the current runner calls
// d.Fixtures.Resolve(sf.Ref) using the process-wide resolver (Deps.Fixtures
// is fixtures.Resolver rooted at a process-wide shared root). The fixture
// in the test is placed beside the test definition, which the process-wide
// resolver cannot reach because it is in a separate directory.
//
// GREEN phase (after I1.4): the runner must change Deps.Fixtures from
// fixtures.Resolver to fixtures.ResolverFactory, and seedFile() must call the
// factory with the test definition's directory — derived from
// req.Test.Definition.SourcePath (the domain.TestDefinition.SourcePath field
// already exists) — to construct a per-document resolver.
//
// NOTE: This test file is written against the current runner API
// (Deps.Fixtures fixtures.Resolver). When I1.4 changes Deps.Fixtures to
// fixtures.ResolverFactory, this file must be updated by the test writer
// agent to supply a ResolverFactory and wire testDefDir into it.

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/fixtures"
	"mosaic-agent-test/internal/runner"
	"mosaic-agent-test/internal/sideeffects"
)

// TestRun_SeedFile_ResolvesRefAgainstTestDefinitionDirectory verifies that
// the runner resolves a seed_files[].ref against the directory containing the
// test definition, not against the process-wide fixture root.
//
// The fixture "nearby-fixture.md" is placed in testDefDir, beside the
// conceptual test definition file. The process-wide resolver is rooted at
// sharedRoot, which is an empty directory that does NOT contain the fixture.
//
// With correct per-document resolution the runner builds a resolver for
// testDefDir (derived from req.Test.Definition.SourcePath) and finds the
// file there. Without per-document resolution, resolution would fail because
// the sharedRoot resolver cannot find the file.
func TestRun_SeedFile_ResolvesRefAgainstTestDefinitionDirectory(t *testing.T) {
	// testDefDir represents the directory where the test definition file lives.
	// It contains a fixture file "nearby-fixture.md" that is conceptually
	// "beside" the definition.
	testDefDir := t.TempDir()
	defPath := filepath.Join(testDefDir, "my-test.test.yaml")
	if err := os.WriteFile(filepath.Join(testDefDir, "nearby-fixture.md"), []byte("# fixture beside test definition\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(nearby-fixture.md): %v", err)
	}

	// sharedRoot is an EMPTY directory. It does not contain "nearby-fixture.md".
	// The process-wide resolver is rooted here, so it cannot find the fixture.
	// Only per-document resolution (using testDefDir as docDir) can find it.
	sharedRoot := t.TempDir()
	resolver, err := fixtures.NewResolver(sharedRoot)
	if err != nil {
		t.Fatalf("fixtures.NewResolver(%q): %v", sharedRoot, err)
	}
	effects := sideeffects.NewApplier(resolver)

	// Build a ResolverFactory with sharedRoot as the fallback. When the
	// factory is called with testDefDir, it produces a resolver that searches
	// testDefDir first (finding "nearby-fixture.md") and sharedRoot second.
	factory, err := fixtures.NewResolverFactory(sharedRoot)
	if err != nil {
		t.Fatalf("fixtures.NewResolverFactory(%q): %v", sharedRoot, err)
	}

	h := newHarness(t)
	// Override the harness's default resolver with one rooted at sharedRoot
	// (empty, does not contain the fixture). This simulates the process-wide
	// resolver that cannot reach testDefDir on its own.
	h.Deps.Fixtures = resolver
	h.Deps.Effects = effects
	// FixtureFactory enables per-document resolution: seedFile() will call
	// factory(testDefDir) to get a resolver that can find "nearby-fixture.md".
	h.Deps.FixtureFactory = factory

	req := newRequest("seed-file-doc-relative")
	// SourcePath is the key input: the runner derives docDir from
	// filepath.Dir(req.Test.Definition.SourcePath) and passes it to seedFile.
	req.Test.Definition.SourcePath = defPath
	req.Test.Definition.SeedFiles = []domain.SeedFile{
		{Path: "doc.md", Ref: "nearby-fixture.md"},
	}

	// Expect success: the runner resolves "nearby-fixture.md" against
	// testDefDir (the test definition's own directory) via FixtureFactory.
	if _, err := runner.Run(context.Background(), h.Deps, req, nil); err != nil {
		t.Fatalf("expected seed_files[].ref %q to resolve against the test definition's "+
			"directory (%q), but runner.Run returned an error: %v",
			"nearby-fixture.md", testDefDir, err)
	}
}
