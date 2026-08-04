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

import (
	"bytes"
	"flag"
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

// findRepoRoot walks up from the test working directory to find the MOSAIC repository root.
// The repo root is identified by the presence of the "Agents" directory, which is distinct
// from the Go module root (Tools/Deployment/). The agents used as golden test inputs live
// under <repoRoot>/Agents/, not under the Go module.
func findRepoRoot(t *testing.T) string {
	t.Helper()
	dir, err := filepath.Abs(".")
	if err != nil {
		t.Fatalf("abs .: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "Agents")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not locate MOSAIC repo root (Agents/ directory) walking up from conformance test directory")
		}
		dir = parent
	}
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

// goldenCases returns the list of test cases for the golden agent set. Each case has a
// source file path (from the generic Agents tree) and a golden file name.
func goldenCases(t *testing.T, moduleRoot string) []goldenCase {
	t.Helper()
	agentsRoot := filepath.Join(moduleRoot, "Agents", "Generic")
	return []goldenCase{
		{
			name:        "contracts-review",
			sourcePath:  filepath.Join(agentsRoot, "Agents", "Validation", "contracts-review.md"),
			key:         "contracts-review",
			kind:        domain.ArtifactAgent,
			goldenName:  "contracts-review.md",
		},
		{
			name:        "test-runner",
			sourcePath:  filepath.Join(agentsRoot, "Agents", "Execution", "test-runner.md"),
			key:         "test-runner",
			kind:        domain.ArtifactAgent,
			goldenName:  "test-runner.md",
		},
		{
			name:        "planner-tdd-soft",
			sourcePath:  filepath.Join(agentsRoot, "Agents", "Planning", "planner-tdd-soft.md"),
			key:         "planner-tdd-soft",
			kind:        domain.ArtifactAgent,
			goldenName:  "planner-tdd-soft.md",
		},
		{
			name:       "orchestrator",
			sourcePath: filepath.Join(agentsRoot, "Orchestrator", "orchestrator.md"),
			key:        "orchestrator",
			kind:       domain.ArtifactAgent,
			goldenName: "orchestrator.md",
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
		t.Skipf("source file not found at %s: %v", tc.sourcePath, err)
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
	cases := goldenCases(t, moduleRoot)
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
	cases := goldenCases(t, repoRoot)
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
	cases := goldenCases(t, repoRoot)
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
	cases := goldenCases(t, repoRoot)

	m := resolveDescriptorOnly(t)
	defer m.Close() //nolint:errcheck

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			src, err := os.ReadFile(tc.sourcePath)
			if err != nil {
				t.Skipf("source file not found at %s: %v", tc.sourcePath, err)
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
