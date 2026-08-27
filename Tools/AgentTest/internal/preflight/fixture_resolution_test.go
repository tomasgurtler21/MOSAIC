package preflight_test

// Tests for per-document resolver construction in preflight.Validate.
//
// These tests are in the TDD RED phase: they compile against the new
// preflight.Input.ResolverFactory field (declared in the contract stub), but
// every test here fails until the implementation agent (I1.2) wires
// ResolverFactory into checkSeedFileRefs and checkSideEffectRefs.
//
// Coverage:
//   - A test definition's seed_files[].ref resolves against a root derived
//     from that test definition's own directory, not from a process-wide
//     fixture root.
//   - A stub registry's side_effects[].ref resolves against the stub
//     registry's own directory (not the test definition's directory).
//   - Resolution succeeds regardless of the in.FixtureRoot value when the
//     fixture sits beside the referencing document.
//   - An explicit shared-root supplied via the factory still works as a
//     fallback when the fixture is not beside the referencing document.

import (
	"os"
	"path/filepath"
	"testing"

	"mosaic-agent-test/internal/fixtures"
	"mosaic-agent-test/internal/preflight"
)

// docRelativeFactory returns a ResolverFactory whose resolvers search only
// the supplied document directory. This is the document-relative-only case
// (no shared root), built from the existing fixtures.NewResolver so that the
// factory itself is not a stub and the test failures originate in preflight's
// failure to call the factory, not in the factory itself.
func docRelativeFactory() fixtures.ResolverFactory {
	return func(docDir string) (fixtures.Resolver, error) {
		return fixtures.NewResolver(docDir)
	}
}

// multiRootFactory returns a ResolverFactory whose resolvers search docDir
// first and sharedRoot as fallback, using fixtures.NewMultiRootResolver.
// Until I1.1 is implemented, NewMultiRootResolver is a stub, so tests using
// this factory will fail for two reasons (no real multi-root + no wiring).
func multiRootFactory(sharedRoot string) fixtures.ResolverFactory {
	return func(docDir string) (fixtures.Resolver, error) {
		return fixtures.NewMultiRootResolver(docDir, sharedRoot)
	}
}

// minimalDefinition returns a valid YAML test definition body that declares
// no seed_files and no assertions, so it passes all structural checks except
// whatever the test is adding.
const minimalDefinitionBody = `
schema_version: 1
name: my-test
id: 1
version: 1
changelog:
  - version: 1
    date: "2026-01-01"
    changes: "Initial."
layer: orchestrator
subject:
  identity: orchestrator
  agent: orchestrator
  infrastructure_agents: []
stub_registry: my-test.stubs.json
`

// minimalRegistryBody is a stub registry that declares no stubs and no side
// effects, so it passes all structural checks by itself.
const minimalRegistryBody = `{
  "schema_version": 1,
  "test_id": "my-test",
  "on_unmatched": "halt",
  "stubs": []
}`

// TestValidate_SeedFileRef_ResolvesAgainstTestDefinitionDirectory verifies
// that when ResolverFactory is set, a seed_files[].ref in a test definition
// is resolved against that definition's own directory, not against a shared
// fixture root. A ref present beside the test definition resolves without
// error even when the shared fixture root is empty or nonexistent.
func TestValidate_SeedFileRef_ResolvesAgainstTestDefinitionDirectory(t *testing.T) {
	// "nearby-fixture.md" lives beside the test definition (root/),
	// not inside a shared fixtures/ directory.
	root := writeTree(t, map[string]string{
		"s.suite.yaml": `
schema_version: 1
id: s
tests:
  - path: my-test.test.yaml
`,
		"my-test.test.yaml": `
schema_version: 1
name: my-test
id: 1
version: 1
changelog:
  - version: 1
    date: "2026-01-01"
    changes: "Initial."
layer: orchestrator
subject:
  identity: orchestrator
  agent: orchestrator
  infrastructure_agents: []
stub_registry: my-test.stubs.json
seed_files:
  - path: Orchestration/doc.md
    ref: nearby-fixture.md
`,
		"my-test.stubs.json": minimalRegistryBody,
		"nearby-fixture.md":  "# fixture beside test definition\n",
		// No "fixtures/nearby-fixture.md" — the file is only at the root level.
	})

	_, report := preflight.Validate(preflight.Input{
		SuitePath:       filepath.Join(root, "s.suite.yaml"),
		FixtureRoot:     filepath.Join(root, "nonexistent-fixtures"), // not beside the test def
		ResolverFactory: docRelativeFactory(),
		HarnessID:       "claude-code",
	})

	if report.HasErrors() {
		t.Fatalf("expected seed_files ref to resolve against the test definition's own "+
			"directory (root/), not against FixtureRoot, got errors: %v", report.Diagnostics)
	}
}

// TestValidate_SideEffectRef_ResolvesAgainstRegistryDirectory verifies that
// when ResolverFactory is set, a side_effects[].ref in a stub registry is
// resolved against that registry's own directory, not against the test
// definition's directory or a shared fixture root.
//
// The registry lives in a subdirectory ("registries/") and its side-effect
// fixture sits beside it ("registries/side-effect.md"). If resolution used
// the test definition's directory (root/) or FixtureRoot, the ref would not
// resolve and preflight would report an "unresolvable-ref" error.
func TestValidate_SideEffectRef_ResolvesAgainstRegistryDirectory(t *testing.T) {
	root := writeTree(t, map[string]string{
		"s.suite.yaml": `
schema_version: 1
id: s
tests:
  - path: my-test.test.yaml
`,
		// Test definition in root/; its stub_registry points to the registries/ subdir.
		// stub_agents declares the worker collaborator so the undeclared-stub-agent
		// check does not fire — we want only the side_effects ref check to be the
		// discriminating assertion in this test.
		"my-test.test.yaml": `
schema_version: 1
name: my-test
id: 1
version: 1
changelog:
  - version: 1
    date: "2026-01-01"
    changes: "Initial."
layer: orchestrator
subject:
  identity: orchestrator
  agent: orchestrator
  infrastructure_agents: []
stub_registry: registries/my-test.stubs.json
stub_agents:
  - identity:
      tool: Task
      agent: worker
    source: agents/worker.md
`,
		// Registry in registries/; it has a side_effect ref.
		"registries/my-test.stubs.json": `{
  "schema_version": 1,
  "test_id": "my-test",
  "on_unmatched": "halt",
  "stubs": [
    {
      "match": { "tool": "Task", "agent": "worker", "invocation": 1 },
      "response": { "status_code": "SUCCESS" },
      "side_effects": {
        "create_files": [
          { "path": "output.md", "ref": "side-effect.md" }
        ]
      }
    }
  ]
}`,
		// side-effect.md lives beside the registry, not at root/ or fixtures/.
		"registries/side-effect.md": "# side effect content\n",
		// Stub agent source file (required by the undeclared-stub-agent check).
		"agents/worker.md": "# worker stub agent\n",
	})

	_, report := preflight.Validate(preflight.Input{
		SuitePath:       filepath.Join(root, "s.suite.yaml"),
		FixtureRoot:     filepath.Join(root, "nonexistent-fixtures"),
		ResolverFactory: docRelativeFactory(),
		HarnessID:       "claude-code",
	})

	if report.HasErrors() {
		t.Fatalf("expected side_effects ref to resolve against the registry's own "+
			"directory (registries/), not against FixtureRoot or the test definition's "+
			"directory, got errors: %v", report.Diagnostics)
	}
}

// TestValidate_SeedFileRef_SucceedsRegardlessOfFixtureRootValue verifies that
// resolution is independent of in.FixtureRoot when fixtures sit beside the
// referencing document and ResolverFactory is supplied. The same invocation
// succeeds whether FixtureRoot is empty, nonexistent, or points elsewhere.
func TestValidate_SeedFileRef_SucceedsRegardlessOfFixtureRootValue(t *testing.T) {
	root := writeTree(t, map[string]string{
		"s.suite.yaml": `
schema_version: 1
id: s
tests:
  - path: my-test.test.yaml
`,
		"my-test.test.yaml": `
schema_version: 1
name: my-test
id: 1
version: 1
changelog:
  - version: 1
    date: "2026-01-01"
    changes: "Initial."
layer: orchestrator
subject:
  identity: orchestrator
  agent: orchestrator
  infrastructure_agents: []
stub_registry: my-test.stubs.json
seed_files:
  - path: doc.md
    ref: local-fixture.md
`,
		"my-test.stubs.json": minimalRegistryBody,
		"local-fixture.md":   "# local\n",
	})

	cases := []struct {
		name        string
		fixtureRoot string
	}{
		{"empty FixtureRoot", ""},
		{"nonexistent FixtureRoot", filepath.Join(root, "does-not-exist")},
		{"FixtureRoot pointing elsewhere", os.TempDir()},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, report := preflight.Validate(preflight.Input{
				SuitePath:       filepath.Join(root, "s.suite.yaml"),
				FixtureRoot:     tc.fixtureRoot,
				ResolverFactory: docRelativeFactory(),
				HarnessID:       "claude-code",
			})
			if report.HasErrors() {
				t.Fatalf("FixtureRoot=%q: expected no errors when fixture is beside the "+
					"test definition and ResolverFactory is set, got: %v",
					tc.fixtureRoot, report.Diagnostics)
			}
		})
	}
}

// TestValidate_SharedFixtureRoot_Works_AsFactoryFallback verifies that an
// explicit shared fixture root supplied via the factory still serves as a
// fallback for refs that are not present beside the referencing document. The
// factory is built by the caller to use docDir first and sharedRoot second;
// preflight must call the factory with the correct docDir and then let the
// factory's multi-root resolver handle the fallback.
//
// This test also exercises the "AC1.4: explicit --fixtures override still
// works as fallback" contract.
func TestValidate_SharedFixtureRoot_Works_AsFactoryFallback(t *testing.T) {
	sharedRoot := filepath.Join(t.TempDir(), "shared-fixtures")
	if err := os.MkdirAll(sharedRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll(%q): %v", sharedRoot, err)
	}

	root := writeTree(t, map[string]string{
		"s.suite.yaml": `
schema_version: 1
id: s
tests:
  - path: my-test.test.yaml
`,
		"my-test.test.yaml": `
schema_version: 1
name: my-test
id: 1
version: 1
changelog:
  - version: 1
    date: "2026-01-01"
    changes: "Initial."
layer: orchestrator
subject:
  identity: orchestrator
  agent: orchestrator
  infrastructure_agents: []
stub_registry: my-test.stubs.json
seed_files:
  - path: doc.md
    ref: shared-only.md
`,
		"my-test.stubs.json": minimalRegistryBody,
		// "shared-only.md" is NOT beside the test definition.
	})

	// Write the fixture ONLY in the shared root.
	sharedFixture := filepath.Join(sharedRoot, "shared-only.md")
	if err := os.WriteFile(sharedFixture, []byte("# shared only\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(%q): %v", sharedFixture, err)
	}

	// FixtureRoot is intentionally set to a path that does NOT equal sharedRoot
	// and does not contain the fixture. This ensures the old single-root path
	// (which uses FixtureRoot directly) cannot find "shared-only.md". The only
	// way this test can pass is if preflight calls ResolverFactory and the
	// factory's multi-root resolver searches sharedRoot as a fallback.
	//
	// Until I1.1 is implemented, NewMultiRootResolver is a stub, so this test
	// fails for two compound reasons — which is the correct TDD RED state: both
	// the fixture package and preflight need to be updated before this test
	// turns green.
	_, report := preflight.Validate(preflight.Input{
		SuitePath:       filepath.Join(root, "s.suite.yaml"),
		FixtureRoot:     filepath.Join(root, "nonexistent-fixtures"),
		ResolverFactory: multiRootFactory(sharedRoot),
		HarnessID:       "claude-code",
	})

	if report.HasErrors() {
		t.Fatalf("expected shared fixture root to serve as fallback via ResolverFactory, "+
			"got errors: %v", report.Diagnostics)
	}
}

// TestValidate_SeedFileRef_NestedFixturesSubdirectory verifies that preflight
// resolves bare-filename refs in seed_files against a fixtures/ subdirectory
// of the test definition's directory when fixtures.NewResolverFactory is used.
// The fixture files exist only inside fixtures/, not at the document root.
//
// This test is in the TDD RED phase: it fails until NewResolverFactory is
// updated (I1.1) to include docDir/fixtures/ as a search root between docDir
// and sharedRoot. Before I1.1, the factory only searches [docDir, sharedRoot],
// cannot find the files that live in fixtures/, and preflight reports
// unresolvable-ref diagnostics -- the expected RED state.
func TestValidate_SeedFileRef_NestedFixturesSubdirectory(t *testing.T) {
	root := writeTree(t, map[string]string{
		"s.suite.yaml": `
schema_version: 1
id: s
tests:
  - path: ref-check.test.yaml
`,
		"ref-check.test.yaml": `
schema_version: 1
name: ref-check
id: 1
version: 1
changelog:
  - version: 1
    date: "2026-01-01"
    changes: "Initial."
layer: orchestrator
subject:
  identity: orchestrator
  agent: orchestrator
  infrastructure_agents: []
stub_registry: ref-check.stubs.json
seed_files:
  - path: Orchestration/sample-fixture.md
    ref: sample-fixture.md
  - path: Orchestration/second-fixture.md
    ref: second-fixture.md
`,
		"ref-check.stubs.json": `{
  "schema_version": 1,
  "test_id": "ref-check",
  "on_unmatched": "halt",
  "stubs": []
}`,
		// Fixture files live ONLY in fixtures/, not at the suite root.
		// Bare-filename refs ("sample-fixture.md", "second-fixture.md") must
		// resolve against docDir/fixtures/ after I1.1 is in place.
		"fixtures/sample-fixture.md": "# sample fixture\n",
		"fixtures/second-fixture.md": "# second fixture\n",
	})

	// Use the real NewResolverFactory with no shared root. After I1.1, the
	// factory includes docDir/fixtures/ as a middle search root and both refs
	// resolve. Before I1.1, the refs are not found and preflight reports
	// unresolvable-ref diagnostics -- this test is RED until I1.1 lands.
	factory, err := fixtures.NewResolverFactory("")
	if err != nil {
		t.Fatalf("fixtures.NewResolverFactory error = %v, want nil", err)
	}

	_, report := preflight.Validate(preflight.Input{
		SuitePath:       filepath.Join(root, "s.suite.yaml"),
		FixtureRoot:     "",
		ResolverFactory: factory,
		HarnessID:       "claude-code",
	})

	if report.HasErrors() {
		t.Fatalf("expected zero unresolvable-ref diagnostics when fixture files are "+
			"in docDir/fixtures/ and NewResolverFactory includes that directory as a "+
			"search root (requires I1.1 to pass); got: %v", report.Diagnostics)
	}
}

// TestValidate_NilResolverFactory_FallsBackToFixtureRoot verifies backward
// compatibility: when ResolverFactory is nil, preflight continues to use a
// single resolver built from FixtureRoot, exactly as before. Callers that do
// not set ResolverFactory must continue to work without change.
func TestValidate_NilResolverFactory_FallsBackToFixtureRoot(t *testing.T) {
	root := writeTree(t, map[string]string{
		"s.suite.yaml": `
schema_version: 1
id: s
tests:
  - path: my-test.test.yaml
`,
		"my-test.test.yaml": `
schema_version: 1
name: my-test
id: 1
version: 1
changelog:
  - version: 1
    date: "2026-01-01"
    changes: "Initial."
layer: orchestrator
subject:
  identity: orchestrator
  agent: orchestrator
  infrastructure_agents: []
stub_registry: my-test.stubs.json
seed_files:
  - path: doc.md
    ref: in-fixtures.md
`,
		"my-test.stubs.json":      minimalRegistryBody,
		"fixtures/in-fixtures.md": "# in shared fixtures dir\n",
	})

	// ResolverFactory is nil — must fall back to FixtureRoot.
	_, report := preflight.Validate(preflight.Input{
		SuitePath:       filepath.Join(root, "s.suite.yaml"),
		FixtureRoot:     filepath.Join(root, "fixtures"),
		ResolverFactory: nil, // explicit nil — tests backward-compat path
		HarnessID:       "claude-code",
	})

	if report.HasErrors() {
		t.Fatalf("expected nil ResolverFactory to fall back to FixtureRoot, got errors: %v",
			report.Diagnostics)
	}
}
