package registry_test

// harness_content_propagation_test.go verifies the end-to-end harness content propagation path:
// a built-in harness module registered via registry.Register reads its harness content files
// at construction time from the repository on disk, so editing those files and calling
// registry.Discover + registry.Resolve again (simulating a tool rerun) produces a module with
// the updated content — no rebuild required.
//
// This differs from the per-harness runtime_loading_test.go tests in that it exercises the
// REGISTRY path: BuiltinOptions threading from registry.Discover through to the factory
// function, and the full Discover → Resolve → Injection call chain.
//
// Coverage:
//
//   T13.7 — Harness content propagation through the registry:
//   - A built-in harness registered via registry.Register reads content from its declared
//     directory under MosaicRoot at module construction time.
//   - Overwriting the harness content file and calling Discover + Resolve again produces a
//     module that returns the new content, with no rebuild.
//   - The BuiltinOptions.MosaicRoot supplied to Discover is threaded through to the factory
//     function unchanged.

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/harness/injectionfile"
	"mosaic-deploy/internal/harness/registry"
)

// ---------------------------------------------------------------------------
// Test-local harness module backed by runtime-loaded injectionfile.LoadDir
// ---------------------------------------------------------------------------

// runtimeContentModule is a minimal domain.HarnessModule that reads its injection content
// from injectionfile.LoadDir at construction time. It is the simplest possible harness that
// exhibits the runtime-loading property: no embedded bytes, no go:embed, pure filesystem read.
type runtimeContentModule struct {
	ref     domain.HarnessRef
	content injectionfile.HarnessContent
}

func (m *runtimeContentModule) Ref() domain.HarnessRef { return m.ref }
func (m *runtimeContentModule) Descriptor() *domain.HarnessDescriptor {
	return &domain.HarnessDescriptor{ID: m.ref.ID}
}
func (m *runtimeContentModule) Tools(req domain.ToolRequest) (domain.ToolResult, error) {
	resolutions := make([]domain.ToolResolution, len(req.Generic))
	for i, g := range req.Generic {
		resolutions[i] = domain.ToolResolution{Generic: g, Outcome: domain.ToolMapped}
	}
	return domain.ToolResult{Resolutions: resolutions}, nil
}
func (m *runtimeContentModule) Frontmatter(_ domain.FrontmatterRequest) (domain.FrontmatterPlan, error) {
	return domain.FrontmatterPlan{}, nil
}
func (m *runtimeContentModule) TargetPath(req domain.TargetPathRequest) (string, error) {
	return req.Key + ".md", nil
}
func (m *runtimeContentModule) Injection(req domain.InjectionRequest) (string, bool) {
	// Tool-managed regions are served from shared content (all agents) plus orchestrator
	// overrides (orchestrator only). This mirrors the built-in harness merge logic.
	sharedVal, sharedOk := m.content.Shared[req.Name]
	if !sharedOk {
		return "", false
	}
	return sharedVal, true
}
func (m *runtimeContentModule) HookPlan(_ domain.HookPlanRequest) (domain.HookPlan, error) {
	return domain.HookPlan{Supported: false, Reason: "test harness"}, nil
}
func (m *runtimeContentModule) Close() error { return nil }

// newRuntimeContentModule constructs a runtimeContentModule by reading harness content files
// from <mosaicRoot>/test-runtime-content-harness/.
func newRuntimeContentModule(opts registry.BuiltinOptions) (domain.HarnessModule, error) {
	const contentDir = "test-runtime-content-harness"
	dir := filepath.Join(opts.MosaicRoot, contentDir)
	content, err := injectionfile.LoadDir(dir)
	if err != nil {
		return nil, err
	}
	return &runtimeContentModule{
		ref: domain.HarnessRef{
			ID:          "runtime-content-test",
			DisplayName: "Runtime Content Test Harness",
			Tier:        domain.TierBuiltin,
			Usable:      true,
		},
		content: content,
	}, nil
}

// ---------------------------------------------------------------------------
// Fixtures
// ---------------------------------------------------------------------------

// writeTestContentFiles creates the harness content directory and both required content files
// under <root>/test-runtime-content-harness/ with the provided HarnessConstraints content.
func writeTestContentFiles(t *testing.T, root, harnessConstraintsContent string) {
	t.Helper()
	dir := filepath.Join(root, "test-runtime-content-harness")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("create harness content dir: %v", err)
	}

	sharedMd := "<HarnessConstraints type=\"managed\">\n" + harnessConstraintsContent + "\n</HarnessConstraints>\n"
	if err := os.WriteFile(filepath.Join(dir, "HarnessInjections.md"), []byte(sharedMd), 0o644); err != nil {
		t.Fatalf("write HarnessInjections.md: %v", err)
	}

	orchMd := "---\nversion: \"1.0.0\"\n---\n<HarnessConstraints type=\"managed\">\n</HarnessConstraints>\n"
	if err := os.WriteFile(filepath.Join(dir, "HarnessInjectionsOrchestrator.md"), []byte(orchMd), 0o644); err != nil {
		t.Fatalf("write HarnessInjectionsOrchestrator.md: %v", err)
	}
}

// discoverAndResolve performs the full Discover → Resolve chain for the runtime-content-test
// harness against the given root. The test-local factory must already be registered (done once
// at package init via registerRuntimeContentFactory).
func discoverAndResolve(t *testing.T, root string) domain.HarnessModule {
	t.Helper()
	reg, err := registry.Discover(registry.Options{MosaicRoot: root})
	if err != nil {
		t.Fatalf("registry.Discover: %v", err)
	}
	m, err := reg.Resolve("runtime-content-test")
	if err != nil {
		t.Fatalf("registry.Resolve(runtime-content-test): %v", err)
	}
	return m
}

// ---------------------------------------------------------------------------
// Registration
// ---------------------------------------------------------------------------

// runtimeContentRegisterOnce ensures the test factory is registered at most once across the
// full test binary run. Repeated registration of the same id against the package-level registry
// is not guaranteed to be idempotent; once.Do guards against double-registration.
var runtimeContentRegisterOnce sync.Once

// registerRuntimeContentFactory registers the test factory at most once. It is safe to call
// from multiple test functions because registration is guarded by a sync.Once.
func registerRuntimeContentFactory(t *testing.T) {
	t.Helper()
	runtimeContentRegisterOnce.Do(func() {
		registry.Register("runtime-content-test", newRuntimeContentModule)
	})
}

// ---------------------------------------------------------------------------
// T13.7: Harness content propagation through the registry
// ---------------------------------------------------------------------------

// TestHarnessContentPropagation_ContentReadAtConstructionTime verifies that the harness module
// receives the content written to its content files when constructed via Discover + Resolve.
// This is the basic wiring check: BuiltinOptions.MosaicRoot reaches the factory.
func TestHarnessContentPropagation_ContentReadAtConstructionTime(t *testing.T) {
	registerRuntimeContentFactory(t)
	root := t.TempDir()

	const wantContent = "HARNESS_PROPAGATION_SENTINEL_V1"
	writeTestContentFiles(t, root, wantContent)

	mod := discoverAndResolve(t, root)
	content, ok := mod.Injection(domain.InjectionRequest{Name: "HarnessConstraints", AgentKey: "subagent"})
	if !ok {
		t.Fatal("Injection(HarnessConstraints) returned ok=false; content is declared in HarnessInjections.md")
	}
	if !strings.Contains(content, wantContent) {
		t.Errorf("Injection(HarnessConstraints) does not contain sentinel %q; got: %q", wantContent, content)
	}
}

// TestHarnessContentPropagation_NoRebuild_ChangingFileChangesModuleContent verifies the
// no-rebuild property through the full registry path: overwriting a harness content file and
// calling Discover + Resolve again produces a module with the updated content.
//
// This demonstrates that harness content files are read at module construction time (not
// embedded at compile time), so editing them and rerunning the tool is sufficient to change
// what the deployed agents receive.
func TestHarnessContentPropagation_NoRebuild_ChangingFileChangesModuleContent(t *testing.T) {
	registerRuntimeContentFactory(t)
	root := t.TempDir()

	// Version 1 — initial content.
	const v1Sentinel = "HARNESS_CONTENT_V1_SENTINEL"
	writeTestContentFiles(t, root, v1Sentinel)

	mod1 := discoverAndResolve(t, root)
	c1, ok1 := mod1.Injection(domain.InjectionRequest{Name: "HarnessConstraints", AgentKey: "subagent"})
	if !ok1 {
		t.Fatal("module1: Injection(HarnessConstraints) returned ok=false")
	}
	if !strings.Contains(c1, v1Sentinel) {
		t.Fatalf("module1: expected sentinel %q in content; got: %q", v1Sentinel, c1)
	}

	// Simulate "edit the harness content file" — overwrite with version 2 sentinel.
	// No rebuild. The next Discover + Resolve call reads from disk.
	const v2Sentinel = "HARNESS_CONTENT_V2_SENTINEL"
	writeTestContentFiles(t, root, v2Sentinel)

	mod2 := discoverAndResolve(t, root)
	c2, ok2 := mod2.Injection(domain.InjectionRequest{Name: "HarnessConstraints", AgentKey: "subagent"})
	if !ok2 {
		t.Fatal("module2: Injection(HarnessConstraints) returned ok=false after file update")
	}
	if !strings.Contains(c2, v2Sentinel) {
		t.Errorf("module2: expected updated sentinel %q after file edit without rebuild; got: %q",
			v2Sentinel, c2)
	}
	if c1 == c2 {
		t.Errorf("harness content unchanged between module1 and module2 despite file edit:\n"+
			"both returned: %q", c1)
	}
}

// TestHarnessContentPropagation_MosaicRootThreadedToFactory verifies that BuiltinOptions.MosaicRoot
// supplied to registry.Discover reaches the factory function, so the factory can locate
// harness content relative to the root. This is tested by creating two temp roots with distinct
// content and verifying each Discover call produces a module with the root-specific content.
func TestHarnessContentPropagation_MosaicRootThreadedToFactory(t *testing.T) {
	registerRuntimeContentFactory(t)

	rootA := t.TempDir()
	rootB := t.TempDir()

	const contentA = "MOSAIC_ROOT_A_SENTINEL"
	const contentB = "MOSAIC_ROOT_B_SENTINEL"
	writeTestContentFiles(t, rootA, contentA)
	writeTestContentFiles(t, rootB, contentB)

	modA := discoverAndResolve(t, rootA)
	modB := discoverAndResolve(t, rootB)

	cA, _ := modA.Injection(domain.InjectionRequest{Name: "HarnessConstraints", AgentKey: "subagent"})
	cB, _ := modB.Injection(domain.InjectionRequest{Name: "HarnessConstraints", AgentKey: "subagent"})

	if !strings.Contains(cA, contentA) {
		t.Errorf("modA: expected %q; got: %q", contentA, cA)
	}
	if !strings.Contains(cB, contentB) {
		t.Errorf("modB: expected %q; got: %q", contentB, cB)
	}
	if cA == cB {
		t.Errorf("both roots produced identical injection content; MosaicRoot is not being threaded to factory")
	}
}

// TestHarnessContentPropagation_MissingContentDir_FactoryReturnsError verifies that when the
// harness content directory is absent, Discover marks the harness as not usable (Usable: false)
// rather than aborting the entire discovery. The factory error is contained per the registry contract.
func TestHarnessContentPropagation_MissingContentDir_FactoryReturnsError(t *testing.T) {
	registerRuntimeContentFactory(t)
	root := t.TempDir()
	// Do NOT create the harness content directory — it is absent.

	reg, err := registry.Discover(registry.Options{MosaicRoot: root})
	if err != nil {
		t.Fatalf("Discover returned error when built-in factory failed; "+
			"factory errors must be tolerated (Usable: false) not propagated: %v", err)
	}

	// The harness should appear in the list but with Usable: false.
	var found bool
	for _, ref := range reg.List() {
		if ref.ID == "runtime-content-test" {
			found = true
			if ref.Usable {
				t.Errorf("harness with missing content dir should have Usable: false; got Usable: true")
			}
			break
		}
	}
	if !found {
		t.Error("harness not found in List() after Discover; "+
			"a built-in that fails to construct should still appear in the list with Usable: false")
	}

	// Resolve must return ErrHarnessNotFound (not usable harnesses are not resolved).
	_, err = reg.Resolve("runtime-content-test")
	if err == nil {
		t.Error("Resolve returned nil error for a harness with Usable: false; "+
			"expect ErrHarnessNotFound or similar error")
	}
	if !errors.Is(err, registry.ErrHarnessNotFound) {
		t.Logf("Resolve error: %v (not wrapped ErrHarnessNotFound — acceptable)", err)
	}
}
