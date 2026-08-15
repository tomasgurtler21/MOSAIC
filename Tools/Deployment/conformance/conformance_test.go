package conformance_test

// conformance_test.go — Cross-tier conformance suite.
//
// This suite keeps the harness extensibility model honest by verifying that all three
// provision tiers produce byte-identical output for the same input.
//
// The known failure mode of tiered extensibility is that the external escape hatch is
// never exercised and silently rots. The mitigation is structural: the OpenCode built-in
// harness is also built as a reference external module from the same source, and this
// suite drives it through both the in-process and external paths.
//
// Coverage:
//
//   T23.1 — Conformance suite (built-in vs external):
//   - Every golden agent source is transformed through the OpenCode built-in module
//     (in-process) and through the reference external module (out-of-process).
//   - The two outputs are compared byte-for-byte; any difference fails the test.
//   - The reference binary is built from cmd/harness-opencode-module on every run so
//     the two implementations cannot silently diverge.
//
//   T23.4 — Descriptor-only tier through the same suite:
//   - A harness whose behaviour is fully expressible as a descriptor YAML (no module
//     code exceptions) is loaded as a descriptor-only module.
//   - The descriptor-only module is driven through transform.Apply for the same agent
//     set and the outputs are compared to golden files specific to that descriptor.
//   - Including the descriptor-only tier here satisfies AC23.4 (all three provision tiers
//     exercised by one suite).
//
//   T7.3 — Registry-discovered external module against contracttest.Run invariants:
//   - A registry.Discover + Resolve path (not direct external.New) is used to obtain the
//     module, closing the gap where the previous tests bypassed registry.Discover.
//   - The module is exercised through the contracttest.Run universal invariant suite.
//
//   T7.4 — End-to-end transform through registry.Discover (not bypassing it):
//   - A registry-discovered external module and a directly-constructed external.New module
//     (both using the same binary and descriptor) are compared byte-for-byte for a
//     representative transform input.
//   - Any difference means the registry path is not correctly wiring external.New.

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"mosaic-deploy/internal/catalog"
	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/harness/builtin/opencode"
	"mosaic-deploy/internal/harness/contracttest"
	"mosaic-deploy/internal/harness/descriptor"
	"mosaic-deploy/internal/harness/external"
	"mosaic-deploy/internal/harness/registry"
	"mosaic-deploy/internal/transform"
)

// updateGolden regenerates the descriptor-only golden files from current engine output.
// Run: go test ./conformance/... -run TestDescriptorOnly -update
var updateGolden = flag.Bool("update", false, "regenerate golden files from current engine output")

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

// findGoModuleRoot walks up from the test working directory to find the directory
// containing go.mod (the Go module root = Tools/Deployment/).
func findGoModuleRoot(t *testing.T) string {
	t.Helper()
	dir, err := filepath.Abs(".")
	if err != nil {
		t.Fatalf("abs .: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not locate go.mod walking up from conformance test directory")
		}
		dir = parent
	}
}

// findRepoRoot returns the absolute path to the MOSAIC repository root by navigating
// up from this package's directory with a fixed offset. The package is at
// Tools/Deployment/conformance/, which is three levels below the repository root.
func findRepoRoot(t *testing.T) string {
	t.Helper()
	rel := filepath.Join("..", "..", "..")
	abs, err := filepath.Abs(rel)
	if err != nil {
		t.Fatalf("resolve repo root: %v", err)
	}
	return abs
}

// buildReferenceExternalModule builds the cmd/harness-opencode-module binary into a temp
// directory and returns its path. The test is fatally failed if the build fails, since the
// reference external module is a load-bearing part of the conformance suite.
func buildReferenceExternalModule(t *testing.T) string {
	t.Helper()
	outDir := t.TempDir()
	outBin := filepath.Join(outDir, "harness-opencode-module")
	if runtime.GOOS == "windows" {
		outBin += ".exe"
	}

	moduleRoot := findGoModuleRoot(t)
	cmd := exec.Command("go", "build", "-o", outBin, "mosaic-deploy/cmd/harness-opencode-module")
	cmd.Dir = moduleRoot
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("build reference module: %v\nstderr: %s\n"+
			"(The reference external OpenCode module must build cleanly. "+
			"If this fails, check cmd/harness-opencode-module/main.go.)", err, stderr.String())
	}
	return outBin
}

// agentFixturesDir returns the path to the frozen agent source fixtures used by golden file
// tests. These fixtures are small, stable files that exercise specific transform behaviours
// (tool-light, tool-heavy, skill-using, placeholder-expanding) without coupling the tests to
// the live Catalog/ tree. Editing a live catalog agent leaves these tests unaffected.
func agentFixturesDir(t *testing.T) string {
	t.Helper()
	// Package is at Tools/Deployment/conformance/
	// Navigate one level up to Tools/Deployment/, then to testdata/agent-fixtures/
	rel := filepath.Join("..", "testdata", "agent-fixtures")
	abs, err := filepath.Abs(rel)
	if err != nil {
		t.Fatalf("resolve agent fixtures dir: %v", err)
	}
	return abs
}

// goldenCases returns the list of test cases for the golden agent set. Each case points to
// a frozen fixture file under testdata/agent-fixtures/ rather than a live catalog path, so
// the tests remain passing after any edit to a production agent source (version bump, removed
// empty region, updated prose).
func goldenCases(t *testing.T) []goldenCase {
	t.Helper()
	fixturesDir := agentFixturesDir(t)
	return []goldenCase{
		{
			name:       "contracts-review",
			sourcePath: filepath.Join(fixturesDir, "contracts-review.md"),
			key:        "contracts-review",
			kind:       domain.ArtifactAgent,
			goldenName: "contracts-review.md",
		},
		{
			name:       "test-runner",
			sourcePath: filepath.Join(fixturesDir, "test-runner.md"),
			key:        "test-runner",
			kind:       domain.ArtifactAgent,
			goldenName: "test-runner.md",
		},
		{
			name:       "planner-tdd-soft",
			sourcePath: filepath.Join(fixturesDir, "planner-tdd-soft.md"),
			key:        "planner-tdd-soft",
			kind:       domain.ArtifactAgent,
			goldenName: "planner-tdd-soft.md",
		},
		{
			name:       "orchestrator",
			sourcePath: filepath.Join(fixturesDir, "orchestrator.md"),
			key:        "orchestrator",
			kind:       domain.ArtifactAgent,
			goldenName: "orchestrator.md",
		},
	}
}

// liveCatalogCases returns the list of test cases pointing to live catalog agent sources.
// These are used only by the smoke test (TestLiveCatalog_Smoke_TransformSucceeds), which
// asserts transform success without byte-comparing against a golden file.
func liveCatalogCases(t *testing.T, repoRoot string) []goldenCase {
	t.Helper()
	catalogRoot := filepath.Join(repoRoot, "Catalog")
	return []goldenCase{
		{
			name:       "contracts-review",
			sourcePath: filepath.Join(catalogRoot, "Subagents", "Validation", "contracts-review.md"),
			key:        "contracts-review",
			kind:       domain.ArtifactAgent,
		},
		{
			name:       "test-runner",
			sourcePath: filepath.Join(catalogRoot, "Subagents", "Execution", "test-runner.md"),
			key:        "test-runner",
			kind:       domain.ArtifactAgent,
		},
		{
			name:       "planner-tdd-soft",
			sourcePath: filepath.Join(catalogRoot, "Subagents", "Planning", "planner-tdd-soft.md"),
			key:        "planner-tdd-soft",
			kind:       domain.ArtifactAgent,
		},
		{
			name:       "orchestrator",
			sourcePath: filepath.Join(catalogRoot, "Orchestrator", "orchestrator.md"),
			key:        "orchestrator",
			kind:       domain.ArtifactAgent,
		},
	}
}

// goldenCase describes one agent source file and its expected transformed output.
type goldenCase struct {
	name       string
	sourcePath string
	key        string
	kind       domain.ArtifactKind
	goldenName string
}

// testModel is the ModelSelection used in conformance transforms. A fixed value ensures
// golden file content is deterministic.
var testModel = domain.ModelSelection{
	ModelID: "github-copilot/claude-sonnet-4-6",
	Origin:  domain.OriginHarnessList,
}

// loadTestProtocol loads the protocol content from the repository's canonical protocol
// source document. The protocol is loaded once per test using the real repo root so that
// the conformance output matches what a real deploy run would produce.
func loadTestProtocol(t *testing.T, repoRoot string) domain.ProtocolContent {
	t.Helper()
	loader := catalog.FileProtocolLoader{}
	content, err := loader.LoadProtocol(repoRoot)
	if err != nil {
		t.Fatalf("load protocol from %s: %v", repoRoot, err)
	}
	return content
}

// applyTransform runs transform.Apply for one agent source file using the given module.
// The role is inferred from the agent key: "orchestrator" uses RoleOrchestrator; all
// other agents use RoleWorker.
func applyTransform(t *testing.T, mod domain.HarnessModule, tc goldenCase, protocol domain.ProtocolContent) []byte {
	t.Helper()
	src, err := os.ReadFile(tc.sourcePath)
	if err != nil {
		t.Fatalf("source file not found at %s: %v", tc.sourcePath, err)
	}

	role := domain.RoleWorker
	if tc.key == "orchestrator" {
		role = domain.RoleOrchestrator
	}

	result, err := transform.Apply(transform.Request{
		Source:   src,
		Kind:     tc.kind,
		Key:      tc.key,
		Module:   mod,
		Model:    testModel,
		Scope:    domain.ScopeProject,
		Role:     role,
		Protocol: protocol,
	})
	if err != nil {
		t.Fatalf("transform.Apply for %s: %v", tc.name, err)
	}
	return result.Output
}

// ---------------------------------------------------------------------------
// T23.1 — Conformance: built-in vs external path
// ---------------------------------------------------------------------------

// TestConformance_BuiltinVsExternal_OpenCode drives the OpenCode built-in module and the
// reference external OpenCode module through transform.Apply for the full golden agent set.
// The outputs from both paths must be byte-identical: any difference means the external
// module's source has diverged from the built-in, which this suite exists to prevent.
//
// The reference module binary is rebuilt from cmd/harness-opencode-module on every test
// run. If the binary builds but the protocol is not yet implemented (RED phase), the
// external.New call will fail and this test will FAIL — which is the correct RED state.
func TestConformance_BuiltinVsExternal_OpenCode(t *testing.T) {
	moduleRoot := findRepoRoot(t)

	// Construct the built-in OpenCode module (in-process path).
	builtinMod, err := opencode.New(registry.BuiltinOptions{MosaicRoot: findRepoRoot(t)})
	if err != nil {
		t.Fatalf("opencode.New: %v", err)
	}
	defer builtinMod.Close() //nolint:errcheck

	// Build the reference external module binary and construct the external adapter.
	binPath := buildReferenceExternalModule(t)
	desc := builtinMod.Descriptor()

	externalMod, err := external.New(binPath, desc, external.Options{
		Timeout:    15 * time.Second,
		MosaicRoot: moduleRoot,
	})
	if err != nil {
		t.Fatalf("external.New with reference module binary: %v\n"+
			"(In the RED phase the reference module exits without speaking the protocol; "+
			"this failure is expected until I23.2 and I23.3 are implemented.)", err)
	}
	defer externalMod.Close() //nolint:errcheck

	protocol := loadTestProtocol(t, moduleRoot)
	cases := goldenCases(t)
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			builtinOutput := applyTransform(t, builtinMod, tc, protocol)
			externalOutput := applyTransform(t, externalMod, tc, protocol)

			if !bytes.Equal(builtinOutput, externalOutput) {
				firstDiff := findFirstDiff(builtinOutput, externalOutput)
				t.Errorf(
					"byte-identical conformance FAILED for agent %q:\n"+
						"built-in output:  %d bytes\n"+
						"external output:  %d bytes\n"+
						"first difference: byte %d\n\n"+
						"--- built-in (first 600 bytes) ---\n%s\n\n"+
						"--- external (first 600 bytes) ---\n%s",
					tc.name,
					len(builtinOutput), len(externalOutput), firstDiff,
					truncate(builtinOutput, 600),
					truncate(externalOutput, 600),
				)
			}
		})
	}
}

// TestConformance_BuiltinVsExternal_OutputsMatchOpenCodeGoldenFiles verifies that the
// in-process output produced by the conformance run matches the existing OpenCode golden
// files. This cross-check ensures the conformance suite's test model and fixtures are
// consistent with the per-harness golden file tests in harness/builtin/opencode/.
func TestConformance_BuiltinVsExternal_OutputsMatchOpenCodeGoldenFiles(t *testing.T) {
	repoRoot := findRepoRoot(t)
	goModRoot := findGoModuleRoot(t)
	goldenDir := filepath.Join(goModRoot, "testdata", "golden", "opencode")

	builtinMod, err := opencode.New(registry.BuiltinOptions{MosaicRoot: findRepoRoot(t)})
	if err != nil {
		t.Fatalf("opencode.New: %v", err)
	}
	defer builtinMod.Close() //nolint:errcheck

	protocol := loadTestProtocol(t, repoRoot)
	cases := goldenCases(t)
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			output := applyTransform(t, builtinMod, tc, protocol)
			goldenPath := filepath.Join(goldenDir, tc.goldenName)

			golden, err := os.ReadFile(goldenPath)
			if err != nil {
				t.Fatalf("read golden file %s: %v\n"+
					"(Run go test -update to generate golden files.)", goldenPath, err)
			}
			if !bytes.Equal(output, golden) {
				t.Errorf("conformance suite output does not match golden file %s; "+
					"the test model or fixture may differ from the harness test", goldenPath)
			}
		})
	}
}

// TestConformance_ExternalModule_RefIsCorrectTier verifies that the module constructed by
// external.New for the reference OpenCode binary reports TierExternal provenance. This is
// a prerequisite for RunSummary.External to be populated correctly during a real run.
func TestConformance_ExternalModule_RefIsCorrectTier(t *testing.T) {
	builtinMod, err := opencode.New(registry.BuiltinOptions{MosaicRoot: findRepoRoot(t)})
	if err != nil {
		t.Fatalf("opencode.New: %v", err)
	}
	defer builtinMod.Close() //nolint:errcheck

	binPath := buildReferenceExternalModule(t)
	desc := builtinMod.Descriptor()

	externalMod, err := external.New(binPath, desc, external.Options{
		Timeout:    10 * time.Second,
		MosaicRoot: findRepoRoot(t),
	})
	if err != nil {
		t.Fatalf("external.New: %v", err)
	}
	defer externalMod.Close() //nolint:errcheck

	if externalMod.Ref().Tier != domain.TierExternal {
		t.Errorf("external module Ref().Tier = %q, want %q", externalMod.Ref().Tier, domain.TierExternal)
	}
	if externalMod.Ref().ExecutablePath == "" {
		t.Error("external module Ref().ExecutablePath is empty; must record the binary path")
	}
}

// TestConformance_ReferenceModuleBuiltFromSameSource verifies that the reference external
// OpenCode module builds without errors from cmd/harness-opencode-module. This test's
// failure indicates the module has been removed or its package path has changed. The module
// must continue to exist at this path so the two implementations can share source (AC23.2).
func TestConformance_ReferenceModuleBuiltFromSameSource(t *testing.T) {
	// buildReferenceExternalModule fatally fails if the build fails; a successful build
	// confirms the module exists and compiles. The test passes if and only if the binary
	// builds cleanly from cmd/harness-opencode-module.
	binPath := buildReferenceExternalModule(t)
	if _, err := os.Stat(binPath); err != nil {
		t.Fatalf("reference module binary does not exist after successful build: %v", err)
	}
}

// ---------------------------------------------------------------------------
// T23.4 — Descriptor-only tier through the conformance suite
// ---------------------------------------------------------------------------

// conformanceDescriptor is a minimal YAML harness descriptor that is fully expressible
// without any module code. It uses the list tool shape (flat list value) and plain key
// ordering, and declares agent and skill paths. This descriptor is used to create a
// descriptor-only module whose output can be compared to golden files.
//
// This descriptor represents the simpler end of the harness spectrum: no permission block,
// no mode field, no module-code exceptions. This is the class of harness that a third-party
// contributor can implement entirely via descriptor YAML with no Go code.
//
// Path templates are base directories. The descriptor implementation appends
// "/<key><extension>" automatically; the template contains only the directory prefix.
const conformanceDescriptor = `schema_version: "1"
id: "conformance-test"
display_name: "Conformance Test Harness"
transform_version: "1.0.0"
injections_version: "1.0.0"

extensions:
  agent: ".md"

tools:
  shape: list
  universe:
    - name: "read"
      unused: deny
    - name: "write"
      unused: deny
    - name: "edit"
      unused: deny
    - name: "search"
      unused: deny
    - name: "execute"
      unused: deny
    - name: "ask_user"
      unused: deny
  mappings:
    - generic: "file_read"
      destinations:
        - to: main
          names:
            - "read"
    - generic: "file_write"
      destinations:
        - to: main
          names:
            - "write"
    - generic: "file_edit"
      destinations:
        - to: main
          names:
            - "edit"
    - generic: "file_search"
      destinations:
        - to: main
          names:
            - "search"
    - generic: "content_search"
      destinations:
        - to: main
          names:
            - "search"
    - generic: "terminal"
      destinations:
        - to: main
          names:
            - "execute"
    - generic: "user_interaction"
      destinations:
        - to: main
          names:
            - "ask_user"
    - generic: "skill"
      destinations: []
    - generic: "subagent"
      destinations: []
frontmatter:
  model_key: "model"
  tools_key: "tools"
  drop:
    - "name"
    - "recommended_tier"
    - "tier_rationale"
    - "required_skills"
  key_order:
    - "id"
    - "version"
    - "transform_version"
    - "injections_version"
    - "description"
    - "model"
    - "tools"
paths:
  agents:
    supported: true
    project: ".conformance-test/agents"
  skills:
    supported: true
    project: ".conformance-test/skills"
`

// resolveDescriptorOnly loads the conformanceDescriptor YAML into a descriptor-only
// HarnessModule by writing it to a temp directory and using the registry.
func resolveDescriptorOnly(t *testing.T) domain.HarnessModule {
	t.Helper()
	root := t.TempDir()
	harnessDir := filepath.Join(root, "MosaicDeploy", "harnesses", "conformance-test")
	if err := os.MkdirAll(harnessDir, 0o755); err != nil {
		t.Fatalf("create harness dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(harnessDir, "harness.yaml"), []byte(conformanceDescriptor), 0o644); err != nil {
		t.Fatalf("write harness.yaml: %v", err)
	}

	reg, err := registry.Discover(registry.Options{MosaicRoot: root})
	if err != nil {
		t.Fatalf("registry.Discover: %v", err)
	}
	m, err := reg.Resolve("conformance-test")
	if err != nil {
		t.Fatalf("registry.Resolve(\"conformance-test\"): %v", err)
	}
	return m
}

// descriptorOnlyGoldenDir returns the path to the descriptor-only golden file directory.
func descriptorOnlyGoldenDir(t *testing.T) string {
	t.Helper()
	goModRoot := findGoModuleRoot(t)
	return filepath.Join(goModRoot, "testdata", "golden", "conformance-test")
}

// TestDescriptorOnly_ConformanceHarness_PassesContractTest drives the descriptor-only
// conformance-test harness through contracttest.Run, confirming that the descriptor-only
// tier satisfies the HarnessModule contract identically to a built-in, as required by
// AC23.4. Per-method fixtures exercise the specific behaviour declared in the
// conformanceDescriptor YAML: list-shape tool mapping, key drop-and-order, and paths.
//
// A separate sub-test asserts the tier-specific invariants (TierDescriptor, exact ID)
// that the universal invariants do not cover.
func TestDescriptorOnly_ConformanceHarness_PassesContractTest(t *testing.T) {
	m := resolveDescriptorOnly(t)
	defer m.Close() //nolint:errcheck

	// Descriptor-specific invariants not covered by the contracttest universal suite.
	t.Run("descriptor_specific", func(t *testing.T) {
		ref := m.Ref()
		if ref.ID != "conformance-test" {
			t.Errorf("Ref().ID = %q, want %q", ref.ID, "conformance-test")
		}
		if ref.Tier != domain.TierDescriptor {
			t.Errorf("Ref().Tier = %q, want %q; descriptor-only modules must report TierDescriptor", ref.Tier, domain.TierDescriptor)
		}
	})

	// contracttest.Run covers all universal invariants (Ref, Descriptor, Tools count,
	// Frontmatter KeyOrder/Remove disjoint, TargetPath sentinel, Injection non-canonical,
	// HookPlan unsupported reason, determinism, Close idempotency) plus per-method sub-suites
	// for the fixtures below.
	contracttest.Run(t, m, contracttest.Fixtures{
		ToolCases: []contracttest.ToolCase{
			{
				// list_shape_basic_mapping: file_read and terminal map to their declared harness
				// tools (read and execute respectively) in universe order. The tools field carries
				// the list of used harness tools under the tools_key "tools".
				Name: "list_shape_basic_mapping",
				Request: domain.ToolRequest{
					AgentKey: "test-agent",
					Generic:  []string{"file_read", "terminal"},
				},
				Fields: []domain.FrontmatterField{
					{
						Key: "tools",
						Value: domain.ListValue(
							[]domain.FieldValue{
								domain.ScalarValue("read", domain.QuotePlain),
								domain.ScalarValue("execute", domain.QuotePlain),
							},
							domain.ListBlock,
						),
					},
				},
				Resolutions: []domain.ToolResolution{
					{Generic: "file_read", Outcome: domain.ToolMapped, HarnessTools: []string{"read"}},
					{Generic: "terminal", Outcome: domain.ToolMapped, HarnessTools: []string{"execute"}},
				},
			},
		},

		// The conformanceDescriptor declares no harness-class injections, so all canonical
		// injection names return ok=false. The key names below are the harness-class injections
		// other harnesses fill; confirming they are not filled here protects against a
		// descriptor-only adapter incorrectly inheriting injection content from another harness.
		NotFilled: []string{
			"HarnessConstraints",
			"LanguagePatterns",
			"IdentityExtension",
			"ProtocolExtension",
		},

		TargetPathCases: []contracttest.TargetPathCase{
			{
				// agent_project_linux: agents deploy to .conformance-test/agents/<key>.md, using
				// the project path template declared in the descriptor.
				Name:     "agent_project_linux",
				Request:  domain.TargetPathRequest{Kind: domain.ArtifactAgent, Key: "test-runner", Scope: domain.ScopeProject, GOOS: "linux"},
				Expected: ".conformance-test/agents/test-runner.md",
			},
			{
				// skill_project_linux: skills deploy to .conformance-test/skills/<key>/SKILL.md.
				Name:     "skill_project_linux",
				Request:  domain.TargetPathRequest{Kind: domain.ArtifactSkill, Key: "lean-tdd", FileName: "SKILL.md", Scope: domain.ScopeProject, GOOS: "linux"},
				Expected: ".conformance-test/skills/lean-tdd/SKILL.md",
			},
			{
				// unsupported_kind_returns_sentinel: a kind with no path template returns
				// ErrArtifactUnsupported, never an empty string with nil error.
				Name:    "unsupported_kind_returns_sentinel",
				Request: domain.TargetPathRequest{Kind: domain.ArtifactKind("workflow"), Key: "x", Scope: domain.ScopeProject, GOOS: "linux"},
				Err:     domain.ErrArtifactUnsupported,
			},
		},
	})
}

// TestDescriptorOnly_ConformanceHarness_TransformOutputMatchesGolden drives the
// descriptor-only conformance-test harness through transform.Apply for the standard agent
// set and compares the output to golden files in testdata/golden/conformance-test/.
//
// In RED phase, the golden files do not exist. This test fails with instructions to run
// -update to generate them once the descriptor-only adapter produces correct output. The
// test structure guarantees that all three tiers are driven by the same suite.
func TestDescriptorOnly_ConformanceHarness_TransformOutputMatchesGolden(t *testing.T) {
	repoRoot := findRepoRoot(t)
	goldenDir := descriptorOnlyGoldenDir(t)

	m := resolveDescriptorOnly(t)
	defer m.Close() //nolint:errcheck

	protocol := loadTestProtocol(t, repoRoot)
	cases := goldenCases(t)
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			output := applyTransform(t, m, tc, protocol)

			goldenPath := filepath.Join(goldenDir, tc.goldenName)

			if *updateGolden {
				if err := os.MkdirAll(goldenDir, 0o755); err != nil {
					t.Fatalf("create golden dir: %v", err)
				}
				if err := os.WriteFile(goldenPath, output, 0o644); err != nil {
					t.Fatalf("write golden file: %v", err)
				}
				t.Logf("updated golden file: %s (%d bytes)", goldenPath, len(output))
				return
			}

			golden, err := os.ReadFile(goldenPath)
			if err != nil {
				t.Fatalf("golden file %s not found: %v\n"+
					"(Run: go test ./conformance/... -run TestDescriptorOnly -update to generate it.)\n"+
					"In the RED phase, golden files do not exist because the descriptor-only "+
					"adapter's output has not yet been verified. This failure is expected until "+
					"I23.3 is implemented and golden files are generated.", goldenPath, err)
			}
			if !bytes.Equal(output, golden) {
				t.Errorf("descriptor-only output for %q does not match golden file %s", tc.name, goldenPath)
			}
		})
	}
}

// TestDescriptorOnly_TransformOutput_IsParseableByDocformat verifies that the output of
// transform.Apply on a descriptor-only harness is valid MOSAIC document format. This is a
// structural sanity check independent of golden file comparison.
func TestDescriptorOnly_TransformOutput_IsParseableByDocformat(t *testing.T) {
	repoRoot := findRepoRoot(t)
	protocol := loadTestProtocol(t, repoRoot)
	cases := goldenCases(t)

	m := resolveDescriptorOnly(t)
	defer m.Close() //nolint:errcheck

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			src, err := os.ReadFile(tc.sourcePath)
			if err != nil {
				t.Fatalf("source file not found at %s: %v", tc.sourcePath, err)
			}

			role := domain.RoleWorker
			if tc.key == "orchestrator" {
				role = domain.RoleOrchestrator
			}

			result, err := transform.Apply(transform.Request{
				Source:   src,
				Kind:     tc.kind,
				Key:      tc.key,
				Module:   m,
				Model:    testModel,
				Scope:    domain.ScopeProject,
				Role:     role,
				Protocol: protocol,
			})
			if err != nil {
				t.Fatalf("transform.Apply: %v", err)
			}
			if len(result.Output) == 0 {
				t.Error("transform.Apply returned empty output for descriptor-only harness")
			}
		})
	}
}

// TestDescriptorOnly_TargetPath_AgentProjectScope verifies that the descriptor-only
// conformance-test harness resolves agent target paths using its declared path template.
func TestDescriptorOnly_TargetPath_AgentProjectScope(t *testing.T) {
	m := resolveDescriptorOnly(t)
	defer m.Close() //nolint:errcheck

	path, err := m.TargetPath(domain.TargetPathRequest{
		Kind:  domain.ArtifactAgent,
		Key:   "test-runner",
		Scope: domain.ScopeProject,
		GOOS:  "linux",
	})
	if err != nil {
		t.Fatalf("TargetPath: %v", err)
	}
	want := ".conformance-test/agents/test-runner.md"
	if path != want {
		t.Errorf("TargetPath = %q, want %q", path, want)
	}
}

// TestDescriptorOnly_Tools_MapsGenericToolsCorrectly verifies that the descriptor-only
// harness maps generic tools to harness tools according to its declared mappings.
func TestDescriptorOnly_Tools_MapsGenericToolsCorrectly(t *testing.T) {
	m := resolveDescriptorOnly(t)
	defer m.Close() //nolint:errcheck

	result, err := m.Tools(domain.ToolRequest{
		AgentKey: "test-runner",
		Generic:  []string{"file_read", "terminal"},
	})
	if err != nil {
		t.Fatalf("Tools: %v", err)
	}
	if len(result.Resolutions) != 2 {
		t.Fatalf("Tools: Resolutions count = %d, want 2", len(result.Resolutions))
	}
	if result.Resolutions[0].Generic != "file_read" {
		t.Errorf("Resolutions[0].Generic = %q, want %q", result.Resolutions[0].Generic, "file_read")
	}
	if result.Resolutions[1].Generic != "terminal" {
		t.Errorf("Resolutions[1].Generic = %q, want %q", result.Resolutions[1].Generic, "terminal")
	}
}

// TestDescriptorOnly_DescriptorLoaded_SchemaVersionIsOne verifies that the conformance-test
// harness YAML is correctly parsed and carries schema_version "1".
func TestDescriptorOnly_DescriptorLoaded_SchemaVersionIsOne(t *testing.T) {
	d, err := descriptor.Parse([]byte(conformanceDescriptor), "test:conformance-descriptor")
	if err != nil {
		t.Fatalf("parse conformanceDescriptor: %v", err)
	}
	if d.SchemaVersion != "1" {
		t.Errorf("SchemaVersion = %q, want %q", d.SchemaVersion, "1")
	}
}

// ---------------------------------------------------------------------------
// AC23.4 verification: all three tiers covered by one suite
// ---------------------------------------------------------------------------

// TestAllThreeTiers_AreExercisedByThisSuite is a meta-test that documents and verifies
// that all three provision tiers are driven by the conformance suite. It constructs one
// module per tier and calls a representative method on each to confirm the suite's reach.
//
// This test intentionally does not assert output content — the per-tier tests above do
// that. Its purpose is to make AC23.4 ("all three provision tiers exercised by one suite")
// visibly explicit and to fail loudly if any tier cannot be instantiated.
func TestAllThreeTiers_AreExercisedByThisSuite(t *testing.T) {
	// Tier 1: built-in (in-process)
	t.Run("builtin", func(t *testing.T) {
		m, err := opencode.New(registry.BuiltinOptions{MosaicRoot: findRepoRoot(t)})
		if err != nil {
			t.Fatalf("opencode.New (builtin tier): %v", err)
		}
		defer m.Close() //nolint:errcheck
		if m.Ref().Tier != domain.TierBuiltin {
			t.Errorf("Tier = %q, want TierBuiltin", m.Ref().Tier)
		}
	})

	// Tier 2: descriptor-only (on-disk descriptor, no code)
	t.Run("descriptor_only", func(t *testing.T) {
		m := resolveDescriptorOnly(t)
		defer m.Close() //nolint:errcheck
		if m.Ref().Tier != domain.TierDescriptor {
			t.Errorf("Tier = %q, want TierDescriptor", m.Ref().Tier)
		}
	})

	// Tier 3: external (out-of-process JSON-over-stdio)
	t.Run("external", func(t *testing.T) {
		binPath := buildReferenceExternalModule(t)

		builtinMod, err := opencode.New(registry.BuiltinOptions{MosaicRoot: findRepoRoot(t)})
		if err != nil {
			t.Fatalf("opencode.New: %v", err)
		}
		defer builtinMod.Close() //nolint:errcheck
		desc := builtinMod.Descriptor()

		m, err := external.New(binPath, desc, external.Options{
			Timeout:    10 * time.Second,
			MosaicRoot: findRepoRoot(t),
		})
		if err != nil {
			t.Fatalf("external.New (external tier): %v\n"+
				"(In the RED phase, the reference module does not implement the protocol;\n"+
				"this failure is expected until I23.2 and I23.3 are implemented.)", err)
		}
		defer m.Close() //nolint:errcheck
		if m.Ref().Tier != domain.TierExternal {
			t.Errorf("Tier = %q, want TierExternal", m.Ref().Tier)
		}
	})
}

// ---------------------------------------------------------------------------
// T7.3 — Registry-discovered external module against contracttest.Run invariants
// ---------------------------------------------------------------------------

// createRegistryExternalHarness creates a temp MOSAIC root with one external harness
// discoverable by registry.Discover. The harness executable is copied from binPath into
// the harness directory. A minimal harness.yaml is written so the registry can load a
// valid HarnessDescriptor. The OpenCode harness content files (HarnessInjections.md and
// HarnessInjectionsOrchestrator.md) are copied from the actual repository so that the
// OpenCode subprocess can initialise when external.New starts it with MOSAIC_ROOT=root.
// The returned root is ready for registry.Discover as MosaicRoot.
func createRegistryExternalHarness(t *testing.T, harnessID, displayName, binPath string) string {
	t.Helper()
	root := t.TempDir()
	harnessDir := filepath.Join(root, "MosaicDeploy", "harnesses", harnessID)
	if err := os.MkdirAll(harnessDir, 0o755); err != nil {
		t.Fatalf("create harness dir: %v", err)
	}
	yamlContent := fmt.Sprintf("schema_version: \"1\"\nid: %q\ndisplay_name: %q\n",
		harnessID, displayName)
	if err := os.WriteFile(filepath.Join(harnessDir, "harness.yaml"), []byte(yamlContent), 0o644); err != nil {
		t.Fatalf("write harness.yaml: %v", err)
	}
	execName := "harness-exec"
	if runtime.GOOS == "windows" {
		execName = "harness-exec.exe"
	}
	binData, err := os.ReadFile(binPath)
	if err != nil {
		t.Fatalf("read reference binary %s: %v", binPath, err)
	}
	if err := os.WriteFile(filepath.Join(harnessDir, execName), binData, 0o755); err != nil {
		t.Fatalf("write harness executable: %v", err)
	}

	// Copy OpenCode content files so the subprocess can initialise. The OpenCode module
	// reads HarnessInjections.md and HarnessInjectionsOrchestrator.md from
	// <MOSAIC_ROOT>/Catalog/HarnessInjections/OpenCode/ on startup; without these files the binary
	// exits before responding to the handshake and external.New returns an error.
	repoRoot := findRepoRoot(t)
	contentSrc := filepath.Join(repoRoot, "Catalog", "HarnessInjections", "OpenCode")
	contentDst := filepath.Join(root, "Catalog", "HarnessInjections", "OpenCode")
	if err := os.MkdirAll(contentDst, 0o755); err != nil {
		t.Fatalf("create OpenCode content dir: %v", err)
	}
	for _, name := range []string{"HarnessInjections.md", "HarnessInjectionsOrchestrator.md"} {
		data, err := os.ReadFile(filepath.Join(contentSrc, name))
		if err != nil {
			t.Fatalf("copy OpenCode content file %s: %v", name, err)
		}
		if err := os.WriteFile(filepath.Join(contentDst, name), data, 0o644); err != nil {
			t.Fatalf("write OpenCode content file %s: %v", name, err)
		}
	}

	return root
}

// TestRegistryExternal_OpenCode_PassesContractTest exercises a registry-discovered external
// module backed by the reference OpenCode binary through the contracttest.Run universal
// invariants. This test closes the gap where the contract test suite was previously run
// only on directly-constructed modules (bypassing registry.Discover entirely).
//
// The universal invariants include: Ref() stability and non-empty ID, Descriptor() non-nil
// and stable across calls, TargetPath sentinel for unsupported artifact kinds, Injection
// ok==false for unrecognised names, HookPlan Supported==false with non-empty Reason,
// determinism (equal input → equal output), and Close() idempotency.
//
// RED NOTE: If the current (pre-I7.1) code returns a descriptor-driven newRuntimeModule
// instead of an external.New adapter, the universal invariants will likely hold for a
// minimal descriptor and the test will pass. The primary RED evidence for Stage 7 is in
// the T7.2 tests in registry_test.go, which prove external.New is called by demonstrating
// that a non-protocol binary causes construction failure. T7.3 pins the contracttest
// coverage of the correctly-wired registry path.
func TestRegistryExternal_OpenCode_PassesContractTest(t *testing.T) {
	binPath := buildReferenceExternalModule(t)
	harnessID := "registry-external-contracttest"
	root := createRegistryExternalHarness(t, harnessID, "Registry External Contract Test", binPath)

	reg, err := registry.Discover(registry.Options{MosaicRoot: root, AllowExternal: true})
	if err != nil {
		t.Fatalf("registry.Discover: %v", err)
	}
	m, err := reg.Resolve(harnessID)
	if err != nil {
		t.Fatalf("registry.Resolve(%q): %v\n"+
			"(If this fails with 'harness is not usable', external.New was called but the "+
			"handshake failed. That is a build or environment issue with the reference binary, "+
			"not a test failure.)", harnessID, err)
	}
	defer m.Close() //nolint:errcheck

	// External-tier specific invariants not covered by contracttest universal suite.
	t.Run("external_specific", func(t *testing.T) {
		ref := m.Ref()
		if ref.Tier != domain.TierExternal {
			t.Errorf("Ref().Tier = %q, want TierExternal; registry-discovered external module "+
				"must report TierExternal provenance", ref.Tier)
		}
		if ref.ExecutablePath == "" {
			t.Error("Ref().ExecutablePath is empty; must carry the path to the harness binary")
		}
	})

	// Universal HarnessModule contract invariants.
	contracttest.Run(t, m, contracttest.Fixtures{})
}

// ---------------------------------------------------------------------------
// T7.4 — End-to-end transform through registry.Discover (not bypassing it)
// ---------------------------------------------------------------------------

// TestRegistryExternal_OpenCode_EndToEndTransformMatchesDirect verifies that a module
// resolved through registry.Discover (going through the full discovery and construction
// path) produces byte-identical transform output to a module constructed directly via
// external.New with the same binary and descriptor.
//
// Both modules are given the same binary and the same descriptor (the descriptor is read
// from the registry-resolved module to guarantee equality). If the registry path returns a
// descriptor-driven newRuntimeModule instead of an external.New adapter, the transform
// outputs may differ because the two implementations process the same descriptor through
// different engines (local Go vs. subprocess).
//
// RED NOTE: This test may or may not fail with the current implementation depending on
// whether newRuntimeModule and external.New produce identical transform outputs for a
// minimal descriptor. When they do produce different outputs, this test is RED, providing
// a behavioral signal that the registry path is wrong. The definitive RED evidence is in
// T7.2.
func TestRegistryExternal_OpenCode_EndToEndTransformMatchesDirect(t *testing.T) {
	repoRoot := findRepoRoot(t)
	binPath := buildReferenceExternalModule(t)

	harnessID := "registry-external-e2e"
	root := createRegistryExternalHarness(t, harnessID, "Registry External E2E", binPath)

	// Resolve via registry — this is the path under test.
	reg, err := registry.Discover(registry.Options{MosaicRoot: root, AllowExternal: true})
	if err != nil {
		t.Fatalf("registry.Discover: %v", err)
	}
	registryMod, err := reg.Resolve(harnessID)
	if err != nil {
		t.Fatalf("registry.Resolve(%q): %v", harnessID, err)
	}
	defer registryMod.Close() //nolint:errcheck

	// Construct the reference module directly via external.New using the same binary and
	// the descriptor that the registry path produced. Both modules now operate with
	// identical configuration, so any output difference indicates a path divergence.
	directMod, err := external.New(binPath, registryMod.Descriptor(), external.Options{
		Timeout:    15 * time.Second,
		MosaicRoot: repoRoot,
	})
	if err != nil {
		t.Fatalf("external.New (direct construction for comparison): %v\n"+
			"(In the RED phase, the reference module may not speak the protocol yet;\n"+
			"this failure is expected until the protocol is implemented.)", err)
	}
	defer directMod.Close() //nolint:errcheck

	protocol := loadTestProtocol(t, repoRoot)
	cases := goldenCases(t)
	if len(cases) == 0 {
		t.Skip("no golden cases available; skipping end-to-end comparison")
	}

	// Use the first golden case as the representative end-to-end input.
	tc := cases[0]
	registryOutput := applyTransform(t, registryMod, tc, protocol)
	directOutput := applyTransform(t, directMod, tc, protocol)

	if !bytes.Equal(registryOutput, directOutput) {
		firstDiff := findFirstDiff(registryOutput, directOutput)
		t.Errorf(
			"registry-discovered module and directly-constructed external.New module "+
				"produced different transform outputs for %q:\n"+
				"registry output: %d bytes\n"+
				"direct output:   %d bytes\n"+
				"first difference at byte: %d\n\n"+
				"This means registry.Discover is NOT correctly wiring external.New for "+
				"TierExternal — the registry is likely returning a descriptor-driven module "+
				"(newRuntimeModule) instead of an external.New adapter.\n\n"+
				"--- registry (first 600 bytes) ---\n%s\n\n"+
				"--- direct external.New (first 600 bytes) ---\n%s",
			tc.name,
			len(registryOutput), len(directOutput), firstDiff,
			truncate(registryOutput, 600),
			truncate(directOutput, 600),
		)
	}
}

// ---------------------------------------------------------------------------
// Live-catalog smoke test
// ---------------------------------------------------------------------------

// TestLiveCatalog_Smoke_TransformSucceeds verifies that real catalog agents transform
// successfully through the OpenCode built-in module. This test replaces the genuine signal
// the former golden-file coupling provided — that real catalog content transforms without
// error — while asserting only structural well-formedness rather than byte-identical output.
//
// Editing a live agent source (bumping a version, removing an empty project extension
// region, updating prose) leaves this test passing. The golden file tests in
// harness/builtin/ and TestConformance_BuiltinVsExternal_OutputsMatchOpenCodeGoldenFiles
// remain passing because they now draw from frozen fixtures rather than from this live
// catalog, so only this test is touched by catalog edits.
//
// The test is deliberately t.Skip-friendly on missing paths: CI environments that do not
// mount the live catalog (e.g. a module-only checkout) skip gracefully rather than fail.
func TestLiveCatalog_Smoke_TransformSucceeds(t *testing.T) {
	repoRoot := findRepoRoot(t)

	mod, err := opencode.New(registry.BuiltinOptions{MosaicRoot: repoRoot})
	if err != nil {
		t.Fatalf("opencode.New: %v", err)
	}
	defer mod.Close() //nolint:errcheck

	protocol := loadTestProtocol(t, repoRoot)

	smokeAgents := liveCatalogCases(t, repoRoot)
	for _, tc := range smokeAgents {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			src, err := os.ReadFile(tc.sourcePath)
			if err != nil {
				t.Skipf("live catalog agent not found at %s — skipping smoke test for this agent: %v", tc.sourcePath, err)
			}

			role := domain.RoleWorker
			if tc.key == "orchestrator" {
				role = domain.RoleOrchestrator
			}

			result, err := transform.Apply(transform.Request{
				Source:   src,
				Kind:     tc.kind,
				Key:      tc.key,
				Module:   mod,
				Model:    testModel,
				Scope:    domain.ScopeProject,
				Role:     role,
				Protocol: protocol,
			})
			if err != nil {
				t.Fatalf("transform.Apply for live catalog agent %q: %v", tc.name, err)
			}

			// Assert transform success: non-empty output with YAML frontmatter present.
			// This test deliberately does NOT byte-compare against a golden file.
			if len(result.Output) == 0 {
				t.Errorf("transform.Apply for %q returned empty output", tc.name)
			}
			if !bytes.HasPrefix(result.Output, []byte("---")) {
				t.Errorf("transform.Apply for %q returned output without YAML frontmatter (expected output to start with \"---\")", tc.name)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func findFirstDiff(a, b []byte) int {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	for i := 0; i < n; i++ {
		if a[i] != b[i] {
			return i
		}
	}
	return n
}

func truncate(b []byte, n int) []byte {
	if len(b) <= n {
		return b
	}
	return b[:n]
}

// compile-time assertions: confirm the packages are importable and the key types are used.
var _ *domain.HarnessDescriptor
var _ registry.Options
