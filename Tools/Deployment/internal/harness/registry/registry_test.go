package registry_test

// Tests for the harness registry: discovery, resolution precedence, external-module opt-in gate,
// provenance reporting, and the listing data consumed by the harness selection screen.
//
// Design notes:
//   - All on-disk harness structures are created in t.TempDir() so tests are fully isolated.
//   - Built-in harnesses are registered by calling registry.Register in test setup; unique IDs
//     based on the test name prevent cross-test interference in the shared package-level registry.
//   - External harness detection is based on the presence of a platform-executable file
//     alongside harness.yaml. A dummy executable is created in tests that exercise external
//     behaviour; it does not implement the JSON-over-stdio protocol and will fail if invoked.
//   - Tests verify behaviour (List contents, Resolve outcome, Ref fields) rather than internal
//     implementation details.
//
// Coverage:
//   T6.1  Discovery across built-in and on-disk sources, including absent/empty directory.
//   T6.2  Resolution precedence when the same harness id is offered by multiple tiers.
//   T6.4  External-module opt-in gate: listing, resolution, and reason text.
//   T6.5  Provenance: every resolved harness reports its tier and source path.
//   T6.6  Listing: the full set of data the harness selection screen consumes.

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/harness/registry"
)

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

// makeRoot creates an empty MosaicRoot temp directory with the harnesses sub-tree.
func makeRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	must(t, os.MkdirAll(filepath.Join(root, "MosaicDeploy", "harnesses"), 0o755))
	return root
}

// addDescriptorOnly adds a descriptor-only harness folder under root/MosaicDeploy/harnesses/<id>/.
func addDescriptorOnly(t *testing.T, root, id, displayName string) {
	t.Helper()
	dir := filepath.Join(root, "MosaicDeploy", "harnesses", id)
	must(t, os.MkdirAll(dir, 0o755))
	writeHarnessYAML(t, dir, id, displayName)
}

// addExternal adds an external harness folder: harness.yaml plus a dummy executable.
// The executable does not implement the JSON-over-stdio protocol; tests only verify the
// gate and provenance, not protocol correctness (that is Stage 23's scope).
//
// Convention: the registry detects an external harness by the presence of a file named
// "harness-exec" (Unix, mode 0o755) or "harness-exec.bat" / "harness-exec.exe" (Windows)
// placed alongside harness.yaml. On Windows, compiled Go programs produce .exe binaries;
// .bat is used here as a test stub for environments that cannot run native binaries.
// Registry detection on Windows must recognise BOTH .exe AND .bat so that a user shipping
// a compiled harness (harness-exec.exe) is treated identically to a scripted one.
// See TestDiscover_ExternalHarness_DetectedByExeOnWindows for the .exe-specific case.
func addExternal(t *testing.T, root, id, displayName string) {
	t.Helper()
	addDescriptorOnly(t, root, id, displayName)
	dir := filepath.Join(root, "MosaicDeploy", "harnesses", id)

	execName := "harness-exec"
	content := []byte("#!/bin/sh\nexit 1\n")
	if runtime.GOOS == "windows" {
		execName = "harness-exec.bat"
		content = []byte("@echo off\r\nexit /b 1\r\n")
	}
	execPath := filepath.Join(dir, execName)
	must(t, os.WriteFile(execPath, content, 0o755))
}

// writeHarnessYAML writes a minimal valid harness.yaml to dir.
func writeHarnessYAML(t *testing.T, dir, id, displayName string) {
	t.Helper()
	yaml := fmt.Sprintf("schema_version: \"1\"\nid: %q\ndisplay_name: %q\n", id, displayName)
	must(t, os.WriteFile(filepath.Join(dir, "harness.yaml"), []byte(yaml), 0o644))
}

// stubModule returns a minimal domain.HarnessModule for use as a built-in factory result.
// Its Ref.Tier is TierBuiltin and Ref.ID is set to id.
func stubModule(id string) domain.HarnessModule {
	return &minimalModule{
		ref: domain.HarnessRef{
			ID:          id,
			DisplayName: id + "-display",
			Tier:        domain.TierBuiltin,
			Usable:      true,
		},
		desc: &domain.HarnessDescriptor{
			SchemaVersion: "1",
			ID:            id,
			DisplayName:   id + "-display",
		},
	}
}

// uniqueID returns a harness id that is unique to the calling test and the given suffix.
// Uniqueness prevents cross-test interference in the shared package-level registry.
func uniqueID(t *testing.T, suffix string) string {
	t.Helper()
	// Keep only safe characters (letters, digits, hyphens) from the test name.
	name := strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' {
			return r
		}
		return '-'
	}, t.Name())
	// Trim leading/trailing hyphens and collapse runs.
	for strings.Contains(name, "--") {
		name = strings.ReplaceAll(name, "--", "-")
	}
	name = strings.Trim(name, "-")
	if suffix != "" {
		return name + "-" + suffix
	}
	return name
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// containsRef reports whether refs contains a HarnessRef with the given id.
func containsRef(refs []domain.HarnessRef, id string) bool {
	for _, r := range refs {
		if r.ID == id {
			return true
		}
	}
	return false
}

// findRef returns the first HarnessRef with the given id, or an empty HarnessRef if absent.
func findRef(refs []domain.HarnessRef, id string) (domain.HarnessRef, bool) {
	for _, r := range refs {
		if r.ID == id {
			return r, true
		}
	}
	return domain.HarnessRef{}, false
}

// minimalModule is a test stub implementing domain.HarnessModule.
type minimalModule struct {
	ref  domain.HarnessRef
	desc *domain.HarnessDescriptor
}

func (m *minimalModule) Ref() domain.HarnessRef                   { return m.ref }
func (m *minimalModule) Descriptor() *domain.HarnessDescriptor     { return m.desc }
func (m *minimalModule) Close() error                              { return nil }
func (m *minimalModule) Tools(_ domain.ToolRequest) (domain.ToolResult, error) {
	return domain.ToolResult{}, nil
}
func (m *minimalModule) Frontmatter(_ domain.FrontmatterRequest) (domain.FrontmatterPlan, error) {
	return domain.FrontmatterPlan{}, nil
}
func (m *minimalModule) TargetPath(_ domain.TargetPathRequest) (string, error) {
	return "", domain.ErrArtifactUnsupported
}
func (m *minimalModule) Injection(_ domain.InjectionRequest) (string, bool) { return "", false }
func (m *minimalModule) HookPlan(_ domain.HookPlanRequest) (domain.HookPlan, error) {
	return domain.HookPlan{Supported: false, Reason: "stub"}, nil
}

// ---------------------------------------------------------------------------
// T6.1 — Discovery: built-ins and on-disk harnesses
// ---------------------------------------------------------------------------

// TestDiscover_NoHarnessesDirectory_SucceedsWithOnlyBuiltins verifies that Discover does not
// fail when the MosaicDeploy/harnesses directory is absent. Only the registered built-ins are
// available; no error is returned for the missing directory.
func TestDiscover_NoHarnessesDirectory_SucceedsWithOnlyBuiltins(t *testing.T) {
	root := t.TempDir() // no MosaicDeploy/harnesses sub-tree
	id := uniqueID(t, "builtin")
	registry.Register(id, func() (domain.HarnessModule, error) { return stubModule(id), nil })

	reg, err := registry.Discover(registry.Options{MosaicRoot: root})
	if err != nil {
		t.Fatalf("Discover returned error for absent harnesses directory: %v", err)
	}
	if reg == nil {
		t.Fatal("Discover returned nil Registry")
	}

	refs := reg.List()
	if !containsRef(refs, id) {
		t.Errorf("registered built-in %q not present in List(); got: %v", id, refIDs(refs))
	}
}

// TestDiscover_EmptyHarnessesDirectory_SucceedsWithOnlyBuiltins verifies that an empty
// MosaicDeploy/harnesses directory is treated identically to an absent one.
func TestDiscover_EmptyHarnessesDirectory_SucceedsWithOnlyBuiltins(t *testing.T) {
	root := makeRoot(t) // empty harnesses dir
	id := uniqueID(t, "builtin")
	registry.Register(id, func() (domain.HarnessModule, error) { return stubModule(id), nil })

	reg, err := registry.Discover(registry.Options{MosaicRoot: root})
	if err != nil {
		t.Fatalf("Discover returned error for empty harnesses directory: %v", err)
	}

	refs := reg.List()
	if !containsRef(refs, id) {
		t.Errorf("registered built-in %q not present in List(); got: %v", id, refIDs(refs))
	}
}

// TestDiscover_ValidDescriptorOnlyFolders_AreDiscoveredAndListed verifies that on-disk
// descriptor-only harness folders are included in the List() result after Discover.
func TestDiscover_ValidDescriptorOnlyFolders_AreDiscoveredAndListed(t *testing.T) {
	root := makeRoot(t)
	alphaID := uniqueID(t, "alpha")
	betaID := uniqueID(t, "beta")
	addDescriptorOnly(t, root, alphaID, "Alpha")
	addDescriptorOnly(t, root, betaID, "Beta")

	reg, err := registry.Discover(registry.Options{MosaicRoot: root})
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}

	refs := reg.List()
	if !containsRef(refs, alphaID) {
		t.Errorf("descriptor-only harness %q not present in List(); got: %v", alphaID, refIDs(refs))
	}
	if !containsRef(refs, betaID) {
		t.Errorf("descriptor-only harness %q not present in List(); got: %v", betaID, refIDs(refs))
	}
}

// TestDiscover_HarnessDirectoryWithInvalidDescriptor_DoesNotPanicOrHide verifies that a
// harness folder whose harness.yaml is malformed does not cause Discover to fail and does
// not silently omit the harness. Per the Error Handling Strategy in ContractsDesign.md, a
// harness with an invalid descriptor must be listed as Usable: false with a non-empty
// UnusableReason — it is never silently dropped, and Discover always returns nil error.
func TestDiscover_HarnessDirectoryWithInvalidDescriptor_DoesNotPanicOrHide(t *testing.T) {
	root := makeRoot(t)
	badDir := filepath.Join(root, "MosaicDeploy", "harnesses", "bad-harness")
	must(t, os.MkdirAll(badDir, 0o755))
	// Write a descriptor that fails validation (missing required id field).
	must(t, os.WriteFile(filepath.Join(badDir, "harness.yaml"), []byte("schema_version: \"1\"\n"), 0o644))

	// Discover must succeed: an invalid descriptor is a harness-level problem, not a
	// registry-level failure.
	reg, err := registry.Discover(registry.Options{MosaicRoot: root})
	if err != nil {
		t.Fatalf("Discover returned error for an invalid descriptor; the Error Handling Strategy "+
			"requires Discover to succeed and list the harness as Usable: false: %v", err)
	}

	refs := reg.List()
	ref, ok := findRef(refs, "bad-harness")
	if !ok {
		t.Fatalf("harness folder \"bad-harness\" absent from List() after Discover; "+
			"the Error Handling Strategy requires it to appear with Usable: false (got: %v)", refIDs(refs))
	}
	if ref.Usable {
		t.Errorf("harness \"bad-harness\" with invalid descriptor: Usable = true, want false")
	}
	if ref.UnusableReason == "" {
		t.Errorf("harness \"bad-harness\" with invalid descriptor: UnusableReason is empty; "+
			"a non-empty reason is required so the harness selection screen can explain the problem")
	}
}

// TestDiscover_RegisteredBuiltins_AreResolvable verifies that harnesses registered via
// Register can be resolved by id after Discover.
func TestDiscover_RegisteredBuiltins_AreResolvable(t *testing.T) {
	root := makeRoot(t)
	id := uniqueID(t, "")
	registry.Register(id, func() (domain.HarnessModule, error) { return stubModule(id), nil })

	reg, err := registry.Discover(registry.Options{MosaicRoot: root})
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}

	m, err := reg.Resolve(id)
	if err != nil {
		t.Fatalf("Resolve(%q): unexpected error: %v", id, err)
	}
	if m == nil {
		t.Fatalf("Resolve(%q): returned nil module with nil error", id)
	}
}

// ---------------------------------------------------------------------------
// T6.2 — Resolution precedence when the same id is offered by multiple tiers
// ---------------------------------------------------------------------------

// TestResolve_BuiltinOnly_ReturnsBuiltinTier verifies that a built-in registered by Register
// is resolvable and carries TierBuiltin provenance when no on-disk override exists.
func TestResolve_BuiltinOnly_ReturnsBuiltinTier(t *testing.T) {
	root := makeRoot(t)
	id := uniqueID(t, "")
	registry.Register(id, func() (domain.HarnessModule, error) { return stubModule(id), nil })

	reg, err := registry.Discover(registry.Options{MosaicRoot: root})
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}

	m, err := reg.Resolve(id)
	if err != nil {
		t.Fatalf("Resolve(%q): %v", id, err)
	}
	if m.Ref().Tier != domain.TierBuiltin {
		t.Errorf("Resolve(%q): Ref().Tier = %q, want %q", id, m.Ref().Tier, domain.TierBuiltin)
	}
}

// TestResolve_DescriptorOnlyOverridesBuiltin_WhenSameID verifies the documented precedence:
// a descriptor-only on-disk harness overrides a compiled-in built-in when they share an id.
func TestResolve_DescriptorOnlyOverridesBuiltin_WhenSameID(t *testing.T) {
	root := makeRoot(t)
	id := uniqueID(t, "conflict")

	// Register a built-in with the same id.
	registry.Register(id, func() (domain.HarnessModule, error) { return stubModule(id), nil })
	// Add a descriptor-only on-disk harness with the same id.
	addDescriptorOnly(t, root, id, "Descriptor Override")

	reg, err := registry.Discover(registry.Options{MosaicRoot: root})
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}

	m, err := reg.Resolve(id)
	if err != nil {
		t.Fatalf("Resolve(%q): %v", id, err)
	}
	// Descriptor-only must win over built-in.
	if m.Ref().Tier != domain.TierDescriptor {
		t.Errorf("Resolve(%q): Ref().Tier = %q, want %q (descriptor-only must override built-in)",
			id, m.Ref().Tier, domain.TierDescriptor)
	}
}

// TestResolve_ExternalOverridesDescriptorOnly_WhenOptedIn verifies that an opted-in external
// module wins over a descriptor-only harness with the same id.
func TestResolve_ExternalOverridesDescriptorOnly_WhenOptedIn(t *testing.T) {
	root := makeRoot(t)
	id := uniqueID(t, "conflict")

	// Add both a descriptor-only and an external harness with the same id.
	// addExternal calls addDescriptorOnly internally then adds the executable.
	addExternal(t, root, id, "External Override")

	reg, err := registry.Discover(registry.Options{MosaicRoot: root, AllowExternal: true})
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}

	m, err := reg.Resolve(id)
	if err != nil {
		t.Fatalf("Resolve(%q): %v", id, err)
	}
	// External must win when opted in.
	if m.Ref().Tier != domain.TierExternal {
		t.Errorf("Resolve(%q): Ref().Tier = %q, want %q (external must override descriptor-only when opted in)",
			id, m.Ref().Tier, domain.TierExternal)
	}
}

// TestResolve_ExternalOverridesBuiltin_WhenOptedIn verifies that an opted-in external module
// wins over a built-in with the same id (external > descriptor-only > built-in).
func TestResolve_ExternalOverridesBuiltin_WhenOptedIn(t *testing.T) {
	root := makeRoot(t)
	id := uniqueID(t, "conflict")

	// Register a built-in.
	registry.Register(id, func() (domain.HarnessModule, error) { return stubModule(id), nil })
	// Add an external module with the same id.
	addExternal(t, root, id, "External Override")

	reg, err := registry.Discover(registry.Options{MosaicRoot: root, AllowExternal: true})
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}

	m, err := reg.Resolve(id)
	if err != nil {
		t.Fatalf("Resolve(%q): %v", id, err)
	}
	if m.Ref().Tier != domain.TierExternal {
		t.Errorf("Resolve(%q): Ref().Tier = %q, want %q", id, m.Ref().Tier, domain.TierExternal)
	}
}

// TestResolve_PrecedenceIsDeterministic verifies that resolving the same id twice returns
// the same tier, confirming the precedence is not affected by map iteration order.
func TestResolve_PrecedenceIsDeterministic(t *testing.T) {
	root := makeRoot(t)
	id := uniqueID(t, "conflict")

	// Register a built-in and add a descriptor-only override.
	registry.Register(id, func() (domain.HarnessModule, error) { return stubModule(id), nil })
	addDescriptorOnly(t, root, id, "Descriptor Override")

	reg, err := registry.Discover(registry.Options{MosaicRoot: root})
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}

	first, err := reg.Resolve(id)
	if err != nil {
		t.Fatalf("first Resolve(%q): %v", id, err)
	}
	second, err := reg.Resolve(id)
	if err != nil {
		t.Fatalf("second Resolve(%q): %v", id, err)
	}

	if first.Ref().Tier != second.Ref().Tier {
		t.Errorf("Resolve(%q) returned different tiers across calls: %q vs %q — precedence is non-deterministic",
			id, first.Ref().Tier, second.Ref().Tier)
	}
}

// TestResolve_UnknownID_ReturnsErrHarnessNotFound verifies that requesting an id not present
// in any tier returns ErrHarnessNotFound.
func TestResolve_UnknownID_ReturnsErrHarnessNotFound(t *testing.T) {
	root := makeRoot(t)

	reg, err := registry.Discover(registry.Options{MosaicRoot: root})
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}

	_, err = reg.Resolve("this-id-does-not-exist")
	if !errors.Is(err, registry.ErrHarnessNotFound) {
		t.Errorf("Resolve unknown id: got error %v, want ErrHarnessNotFound", err)
	}
}

// ---------------------------------------------------------------------------
// T6.4 — External-module opt-in gate
// ---------------------------------------------------------------------------

// TestList_ExternalHarness_WithoutOptIn_IncludedInList verifies that an external module
// appears in List() even without AllowExternal, so the harness selection screen can show it
// with an explanation of why it is unavailable.
func TestList_ExternalHarness_WithoutOptIn_IncludedInList(t *testing.T) {
	root := makeRoot(t)
	id := uniqueID(t, "ext")
	addExternal(t, root, id, "External Harness")

	reg, err := registry.Discover(registry.Options{MosaicRoot: root, AllowExternal: false})
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}

	refs := reg.List()
	if !containsRef(refs, id) {
		t.Errorf("external harness %q absent from List() without opt-in; got: %v", id, refIDs(refs))
	}
}

// TestList_ExternalHarness_WithoutOptIn_UsableIsFalse verifies that an external module's
// HarnessRef.Usable is false when AllowExternal is not set.
func TestList_ExternalHarness_WithoutOptIn_UsableIsFalse(t *testing.T) {
	root := makeRoot(t)
	id := uniqueID(t, "ext")
	addExternal(t, root, id, "External Harness")

	reg, err := registry.Discover(registry.Options{MosaicRoot: root, AllowExternal: false})
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}

	ref, ok := findRef(reg.List(), id)
	if !ok {
		t.Fatalf("external harness %q absent from List()", id)
	}
	if ref.Usable {
		t.Errorf("external harness %q: Usable = true without opt-in, want false", id)
	}
}

// TestList_ExternalHarness_WithoutOptIn_RequiresOptInIsTrue verifies that the HarnessRef
// for an external module carries RequiresOptIn == true when AllowExternal is false.
func TestList_ExternalHarness_WithoutOptIn_RequiresOptInIsTrue(t *testing.T) {
	root := makeRoot(t)
	id := uniqueID(t, "ext")
	addExternal(t, root, id, "External Harness")

	reg, err := registry.Discover(registry.Options{MosaicRoot: root, AllowExternal: false})
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}

	ref, ok := findRef(reg.List(), id)
	if !ok {
		t.Fatalf("external harness %q absent from List()", id)
	}
	if !ref.RequiresOptIn {
		t.Errorf("external harness %q: RequiresOptIn = false, want true", id)
	}
}

// TestList_ExternalHarness_WithoutOptIn_UnusableReasonIsNonEmpty verifies that the
// HarnessRef.UnusableReason field explains why the module is unavailable. This text is
// shown to the user in the harness selection screen.
func TestList_ExternalHarness_WithoutOptIn_UnusableReasonIsNonEmpty(t *testing.T) {
	root := makeRoot(t)
	id := uniqueID(t, "ext")
	addExternal(t, root, id, "External Harness")

	reg, err := registry.Discover(registry.Options{MosaicRoot: root, AllowExternal: false})
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}

	ref, ok := findRef(reg.List(), id)
	if !ok {
		t.Fatalf("external harness %q absent from List()", id)
	}
	if !ref.Usable && ref.UnusableReason == "" {
		t.Errorf("external harness %q: Usable is false but UnusableReason is empty", id)
	}
}

// TestResolve_ExternalHarness_WithoutOptIn_ReturnsErrExternalNotPermitted verifies that
// Resolve returns ErrExternalNotPermitted for an external module when AllowExternal is false.
// The gate is at Resolve, not at execution, so no consumer can obtain the module object.
func TestResolve_ExternalHarness_WithoutOptIn_ReturnsErrExternalNotPermitted(t *testing.T) {
	root := makeRoot(t)
	id := uniqueID(t, "ext")
	addExternal(t, root, id, "External Harness")

	reg, err := registry.Discover(registry.Options{MosaicRoot: root, AllowExternal: false})
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}

	_, err = reg.Resolve(id)
	if !errors.Is(err, registry.ErrExternalNotPermitted) {
		t.Errorf("Resolve external harness without opt-in: got %v, want ErrExternalNotPermitted", err)
	}
}

// TestResolve_ExternalHarness_WithoutOptIn_ReturnsNilModule verifies that when
// ErrExternalNotPermitted is returned by Resolve, the module value is nil. No caller should
// receive an external module object without having passed the gate.
func TestResolve_ExternalHarness_WithoutOptIn_ReturnsNilModule(t *testing.T) {
	root := makeRoot(t)
	id := uniqueID(t, "ext")
	addExternal(t, root, id, "External Harness")

	reg, err := registry.Discover(registry.Options{MosaicRoot: root, AllowExternal: false})
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}

	m, err := reg.Resolve(id)
	if !errors.Is(err, registry.ErrExternalNotPermitted) {
		t.Fatalf("expected ErrExternalNotPermitted, got %v", err)
	}
	if m != nil {
		t.Errorf("Resolve returned a non-nil module alongside ErrExternalNotPermitted; module must be nil when gated")
	}
}

// TestResolve_ExternalHarness_WithOptIn_ReturnsNonNilModule verifies that when
// AllowExternal is true, Resolve returns a non-nil module and no error for an external
// harness. The module may fail when methods are called (the dummy executable does not
// implement the protocol); that is beyond this test's scope.
func TestResolve_ExternalHarness_WithOptIn_ReturnsNonNilModule(t *testing.T) {
	root := makeRoot(t)
	id := uniqueID(t, "ext")
	addExternal(t, root, id, "External Harness")

	reg, err := registry.Discover(registry.Options{MosaicRoot: root, AllowExternal: true})
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}

	m, err := reg.Resolve(id)
	if err != nil {
		t.Fatalf("Resolve(%q) with opt-in: unexpected error: %v", id, err)
	}
	if m == nil {
		t.Fatalf("Resolve(%q) with opt-in: returned nil module with nil error", id)
	}
}

// TestList_ExternalHarness_WithOptIn_UsableIsTrue verifies that when AllowExternal is true,
// the HarnessRef for an external module carries Usable == true.
func TestList_ExternalHarness_WithOptIn_UsableIsTrue(t *testing.T) {
	root := makeRoot(t)
	id := uniqueID(t, "ext")
	addExternal(t, root, id, "External Harness")

	reg, err := registry.Discover(registry.Options{MosaicRoot: root, AllowExternal: true})
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}

	ref, ok := findRef(reg.List(), id)
	if !ok {
		t.Fatalf("external harness %q absent from List()", id)
	}
	if !ref.Usable {
		t.Errorf("external harness %q with opt-in: Usable = false, want true", id)
	}
}

// ---------------------------------------------------------------------------
// T6.5 — Provenance: tier and source path on every resolved harness
// ---------------------------------------------------------------------------

// TestProvenance_BuiltinHarness_TierIsBuiltin verifies that a harness resolved from a
// compiled-in built-in reports TierBuiltin provenance.
func TestProvenance_BuiltinHarness_TierIsBuiltin(t *testing.T) {
	root := makeRoot(t)
	id := uniqueID(t, "")
	registry.Register(id, func() (domain.HarnessModule, error) { return stubModule(id), nil })

	reg, err := registry.Discover(registry.Options{MosaicRoot: root})
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}

	m, err := reg.Resolve(id)
	if err != nil {
		t.Fatalf("Resolve(%q): %v", id, err)
	}
	if m.Ref().Tier != domain.TierBuiltin {
		t.Errorf("built-in harness Ref().Tier = %q, want %q", m.Ref().Tier, domain.TierBuiltin)
	}
}

// TestProvenance_BuiltinHarness_IDMatchesRegisteredID verifies that Ref().ID matches the
// id that was passed to Register.
func TestProvenance_BuiltinHarness_IDMatchesRegisteredID(t *testing.T) {
	root := makeRoot(t)
	id := uniqueID(t, "")
	registry.Register(id, func() (domain.HarnessModule, error) { return stubModule(id), nil })

	reg, err := registry.Discover(registry.Options{MosaicRoot: root})
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}

	m, err := reg.Resolve(id)
	if err != nil {
		t.Fatalf("Resolve(%q): %v", id, err)
	}
	if m.Ref().ID != id {
		t.Errorf("built-in Ref().ID = %q, want %q", m.Ref().ID, id)
	}
}

// TestProvenance_DescriptorOnlyHarness_TierIsDescriptor verifies that a harness resolved
// from an on-disk descriptor-only folder reports TierDescriptor provenance.
func TestProvenance_DescriptorOnlyHarness_TierIsDescriptor(t *testing.T) {
	root := makeRoot(t)
	id := uniqueID(t, "")
	addDescriptorOnly(t, root, id, "Alpha")

	reg, err := registry.Discover(registry.Options{MosaicRoot: root})
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}

	m, err := reg.Resolve(id)
	if err != nil {
		t.Fatalf("Resolve(%q): %v", id, err)
	}
	if m.Ref().Tier != domain.TierDescriptor {
		t.Errorf("descriptor-only Ref().Tier = %q, want %q", m.Ref().Tier, domain.TierDescriptor)
	}
}

// TestProvenance_DescriptorOnlyHarness_SourcePathIsNonEmpty verifies that a descriptor-only
// harness carries a non-empty SourcePath pointing to the harness.yaml on disk.
func TestProvenance_DescriptorOnlyHarness_SourcePathIsNonEmpty(t *testing.T) {
	root := makeRoot(t)
	id := uniqueID(t, "")
	addDescriptorOnly(t, root, id, "Alpha")

	reg, err := registry.Discover(registry.Options{MosaicRoot: root})
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}

	m, err := reg.Resolve(id)
	if err != nil {
		t.Fatalf("Resolve(%q): %v", id, err)
	}
	if m.Ref().SourcePath == "" {
		t.Errorf("descriptor-only Ref().SourcePath is empty; expected path to harness.yaml on disk")
	}
}

// TestProvenance_DescriptorOnlyHarness_SourcePathPointsInsideRoot verifies that the
// SourcePath reported by a descriptor-only harness is within the on-disk harnesses tree.
func TestProvenance_DescriptorOnlyHarness_SourcePathPointsInsideRoot(t *testing.T) {
	root := makeRoot(t)
	id := uniqueID(t, "")
	addDescriptorOnly(t, root, id, "Alpha")

	reg, err := registry.Discover(registry.Options{MosaicRoot: root})
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}

	m, err := reg.Resolve(id)
	if err != nil {
		t.Fatalf("Resolve(%q): %v", id, err)
	}

	srcPath := m.Ref().SourcePath
	harnessesRoot := filepath.Join(root, "MosaicDeploy", "harnesses")
	if !strings.HasPrefix(srcPath, harnessesRoot) {
		t.Errorf("descriptor-only Ref().SourcePath = %q is not inside harnesses root %q", srcPath, harnessesRoot)
	}
}

// TestProvenance_ExternalHarness_TierIsExternal verifies that a resolved external module
// reports TierExternal provenance when AllowExternal is true.
func TestProvenance_ExternalHarness_TierIsExternal(t *testing.T) {
	root := makeRoot(t)
	id := uniqueID(t, "")
	addExternal(t, root, id, "External")

	reg, err := registry.Discover(registry.Options{MosaicRoot: root, AllowExternal: true})
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}

	m, err := reg.Resolve(id)
	if err != nil {
		t.Fatalf("Resolve(%q): %v", id, err)
	}
	if m.Ref().Tier != domain.TierExternal {
		t.Errorf("external Ref().Tier = %q, want %q", m.Ref().Tier, domain.TierExternal)
	}
}

// TestProvenance_ExternalHarness_SourcePathIsNonEmpty verifies that an external module's
// HarnessRef.SourcePath is non-empty (points to the harness.yaml or the harness folder).
func TestProvenance_ExternalHarness_SourcePathIsNonEmpty(t *testing.T) {
	root := makeRoot(t)
	id := uniqueID(t, "")
	addExternal(t, root, id, "External")

	reg, err := registry.Discover(registry.Options{MosaicRoot: root, AllowExternal: true})
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}

	m, err := reg.Resolve(id)
	if err != nil {
		t.Fatalf("Resolve(%q): %v", id, err)
	}
	if m.Ref().SourcePath == "" {
		t.Errorf("external Ref().SourcePath is empty; expected path to descriptor or harness folder")
	}
}

// TestProvenance_ExternalHarness_ExecutablePathIsNonEmpty verifies that an external module's
// HarnessRef.ExecutablePath is non-empty and points to the discovered executable file.
func TestProvenance_ExternalHarness_ExecutablePathIsNonEmpty(t *testing.T) {
	root := makeRoot(t)
	id := uniqueID(t, "")
	addExternal(t, root, id, "External")

	reg, err := registry.Discover(registry.Options{MosaicRoot: root, AllowExternal: true})
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}

	m, err := reg.Resolve(id)
	if err != nil {
		t.Fatalf("Resolve(%q): %v", id, err)
	}
	if m.Ref().ExecutablePath == "" {
		t.Errorf("external Ref().ExecutablePath is empty; expected path to the harness executable")
	}
}

// TestProvenance_RefIDIsNonEmptyForAllTiers verifies that every resolved module carries a
// non-empty id in its Ref(), regardless of provision tier.
func TestProvenance_RefIDIsNonEmptyForAllTiers(t *testing.T) {
	root := makeRoot(t)
	builtinID := uniqueID(t, "builtin")
	descriptorID := uniqueID(t, "descriptor")
	externalID := uniqueID(t, "external")

	registry.Register(builtinID, func() (domain.HarnessModule, error) { return stubModule(builtinID), nil })
	addDescriptorOnly(t, root, descriptorID, "Descriptor")
	addExternal(t, root, externalID, "External")

	reg, err := registry.Discover(registry.Options{MosaicRoot: root, AllowExternal: true})
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}

	for _, id := range []string{builtinID, descriptorID, externalID} {
		m, err := reg.Resolve(id)
		if err != nil {
			t.Errorf("Resolve(%q): %v", id, err)
			continue
		}
		if m.Ref().ID == "" {
			t.Errorf("Resolve(%q): Ref().ID is empty", id)
		}
	}
}

// TestProvenance_RefIsStable verifies that calling Ref() twice on the same module returns
// the same value (determinism requirement; same call, same result).
func TestProvenance_RefIsStable(t *testing.T) {
	root := makeRoot(t)
	id := uniqueID(t, "")
	addDescriptorOnly(t, root, id, "Stable")

	reg, err := registry.Discover(registry.Options{MosaicRoot: root})
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}

	m, err := reg.Resolve(id)
	if err != nil {
		t.Fatalf("Resolve(%q): %v", id, err)
	}

	first := m.Ref()
	second := m.Ref()
	if first.ID != second.ID || first.Tier != second.Tier || first.SourcePath != second.SourcePath {
		t.Errorf("Ref() is not stable across calls:\nfirst:  %+v\nsecond: %+v", first, second)
	}
}

// ---------------------------------------------------------------------------
// T6.6 — Listing: the data consumed by the harness selection screen
// ---------------------------------------------------------------------------

// TestList_BuiltinHarness_AppearsInList verifies that a registered built-in is present in
// the List() output of the Registry returned by Discover.
func TestList_BuiltinHarness_AppearsInList(t *testing.T) {
	root := makeRoot(t)
	id := uniqueID(t, "")
	registry.Register(id, func() (domain.HarnessModule, error) { return stubModule(id), nil })

	reg, err := registry.Discover(registry.Options{MosaicRoot: root})
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}

	if !containsRef(reg.List(), id) {
		t.Errorf("built-in %q absent from List(); got: %v", id, refIDs(reg.List()))
	}
}

// TestList_BuiltinHarness_UsableIsTrue verifies that a built-in harness is listed as usable.
func TestList_BuiltinHarness_UsableIsTrue(t *testing.T) {
	root := makeRoot(t)
	id := uniqueID(t, "")
	registry.Register(id, func() (domain.HarnessModule, error) { return stubModule(id), nil })

	reg, err := registry.Discover(registry.Options{MosaicRoot: root})
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}

	ref, ok := findRef(reg.List(), id)
	if !ok {
		t.Fatalf("built-in %q absent from List()", id)
	}
	if !ref.Usable {
		t.Errorf("built-in %q: Usable = false, want true", id)
	}
}

// TestList_DescriptorOnlyHarness_AppearsInList verifies that an on-disk descriptor-only
// harness appears in List().
func TestList_DescriptorOnlyHarness_AppearsInList(t *testing.T) {
	root := makeRoot(t)
	id := uniqueID(t, "")
	addDescriptorOnly(t, root, id, "Descriptor Harness")

	reg, err := registry.Discover(registry.Options{MosaicRoot: root})
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}

	if !containsRef(reg.List(), id) {
		t.Errorf("descriptor-only %q absent from List(); got: %v", id, refIDs(reg.List()))
	}
}

// TestList_DescriptorOnlyHarness_UsableIsTrue verifies that a descriptor-only harness is
// listed as usable (no opt-in required, no errors).
func TestList_DescriptorOnlyHarness_UsableIsTrue(t *testing.T) {
	root := makeRoot(t)
	id := uniqueID(t, "")
	addDescriptorOnly(t, root, id, "Descriptor Harness")

	reg, err := registry.Discover(registry.Options{MosaicRoot: root})
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}

	ref, ok := findRef(reg.List(), id)
	if !ok {
		t.Fatalf("descriptor-only %q absent from List()", id)
	}
	if !ref.Usable {
		t.Errorf("descriptor-only %q: Usable = false, want true", id)
	}
}

// TestList_DescriptorOnlyHarness_DisplayNameMatches verifies that the DisplayName in the
// listed HarnessRef matches what was declared in the harness descriptor.
func TestList_DescriptorOnlyHarness_DisplayNameMatches(t *testing.T) {
	root := makeRoot(t)
	id := uniqueID(t, "")
	displayName := "My Display Name"
	addDescriptorOnly(t, root, id, displayName)

	reg, err := registry.Discover(registry.Options{MosaicRoot: root})
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}

	ref, ok := findRef(reg.List(), id)
	if !ok {
		t.Fatalf("descriptor-only %q absent from List()", id)
	}
	if ref.DisplayName != displayName {
		t.Errorf("descriptor-only %q: DisplayName = %q, want %q", id, ref.DisplayName, displayName)
	}
}

// TestList_AllHarnessesHaveNonEmptyID verifies that every entry returned by List() carries
// a non-empty ID field. An entry with an empty ID is a data integrity error.
func TestList_AllHarnessesHaveNonEmptyID(t *testing.T) {
	root := makeRoot(t)
	builtinID := uniqueID(t, "builtin")
	descriptorID := uniqueID(t, "descriptor")

	registry.Register(builtinID, func() (domain.HarnessModule, error) { return stubModule(builtinID), nil })
	addDescriptorOnly(t, root, descriptorID, "Descriptor")

	reg, err := registry.Discover(registry.Options{MosaicRoot: root})
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}

	refs := reg.List()
	// Positive precondition: at least the known descriptor-only harness must be present so the
	// property check below is not vacuous. A broken implementation that returns an empty list
	// would otherwise pass this test without ever verifying the non-empty-ID invariant.
	if !containsRef(refs, descriptorID) {
		t.Fatalf("known descriptor-only harness %q absent from List(); "+
			"the non-empty-ID check below would be vacuous without at least one harness present", descriptorID)
	}
	for _, ref := range refs {
		if ref.ID == "" {
			t.Errorf("List() returned a HarnessRef with empty ID: %+v", ref)
		}
	}
}

// TestList_UsableHarnessesHaveEmptyUnusableReason verifies that harnesses listed as Usable
// carry an empty UnusableReason. An explanation is only meaningful when Usable is false.
func TestList_UsableHarnessesHaveEmptyUnusableReason(t *testing.T) {
	root := makeRoot(t)
	id := uniqueID(t, "")
	addDescriptorOnly(t, root, id, "Good Harness")

	reg, err := registry.Discover(registry.Options{MosaicRoot: root})
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}

	refs := reg.List()
	// Positive precondition: the known harness must be present and usable so the invariant
	// check below is not vacuous. A permanently-empty list would pass the loop without ever
	// testing the Usable→empty-reason invariant.
	knownRef, ok := findRef(refs, id)
	if !ok {
		t.Fatalf("known harness %q absent from List(); the UnusableReason invariant check would be vacuous", id)
	}
	if !knownRef.Usable {
		t.Fatalf("known harness %q is listed as Usable: false; cannot verify the usable→empty-reason invariant", id)
	}
	for _, ref := range refs {
		if ref.Usable && ref.UnusableReason != "" {
			t.Errorf("usable harness %q has non-empty UnusableReason: %q", ref.ID, ref.UnusableReason)
		}
	}
}

// TestList_ReturnsConsistentResultsAcrossCalls verifies that calling List() twice on the
// same Registry returns the same set of harness IDs.
func TestList_ReturnsConsistentResultsAcrossCalls(t *testing.T) {
	root := makeRoot(t)
	id := uniqueID(t, "")
	addDescriptorOnly(t, root, id, "Stable")

	reg, err := registry.Discover(registry.Options{MosaicRoot: root})
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}

	first := refIDs(reg.List())
	second := refIDs(reg.List())

	// Positive precondition: the known harness must appear in the first call so that the
	// consistency check below is not vacuous. A permanently-empty implementation would pass
	// every loop and length check below without ever exercising List() stability.
	firstContains := false
	for _, listID := range first {
		if listID == id {
			firstContains = true
			break
		}
	}
	if !firstContains {
		t.Fatalf("known harness %q absent from first List() call; the consistency check would be vacuous", id)
	}

	if len(first) != len(second) {
		t.Errorf("List() returned different lengths across calls: %d vs %d", len(first), len(second))
		return
	}
	firstSet := make(map[string]bool, len(first))
	for _, listID := range first {
		firstSet[listID] = true
	}
	for _, listID := range second {
		if !firstSet[listID] {
			t.Errorf("List() second call returned id %q not present in first call", listID)
		}
	}
}

// ---------------------------------------------------------------------------
// Windows .exe external-harness detection
// ---------------------------------------------------------------------------

// TestDiscover_ExternalHarness_DetectedByExeOnWindows verifies that the registry recognises
// a compiled Windows executable (harness-exec.exe) as the external harness marker, not just
// the .bat stub used by addExternal. Compiled Go harnesses on Windows produce .exe files;
// the detection convention must include both .bat and .exe so that real compiled harnesses
// are not missed. This test is skipped on non-Windows platforms.
func TestDiscover_ExternalHarness_DetectedByExeOnWindows(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows-only: verifies that harness-exec.exe is recognised as an external marker")
	}
	root := makeRoot(t)
	id := uniqueID(t, "ext-exe")

	// Create a descriptor-only folder, then place harness-exec.exe (not .bat) alongside it.
	addDescriptorOnly(t, root, id, "External EXE Harness")
	dir := filepath.Join(root, "MosaicDeploy", "harnesses", id)
	must(t, os.WriteFile(filepath.Join(dir, "harness-exec.exe"), []byte{}, 0o755))

	reg, err := registry.Discover(registry.Options{MosaicRoot: root, AllowExternal: true})
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}

	ref, ok := findRef(reg.List(), id)
	if !ok {
		t.Fatalf("harness %q absent from List(); harness-exec.exe was not recognised as an external marker", id)
	}
	if ref.Tier != domain.TierExternal {
		t.Errorf("harness %q: Tier = %q, want TierExternal (harness-exec.exe must be recognised as the external marker on Windows)",
			id, ref.Tier)
	}
}

// ---------------------------------------------------------------------------
// Minor gap tests: provenance, missing descriptor file, id precedence
// ---------------------------------------------------------------------------

// TestProvenance_BuiltinHarness_SourcePathIsEmpty verifies the design contract that
// built-in harnesses carry an empty SourcePath. Built-ins are compiled into the binary;
// there is no on-disk descriptor/module path to report (domain.HarnessRef doc comment).
func TestProvenance_BuiltinHarness_SourcePathIsEmpty(t *testing.T) {
	root := makeRoot(t)
	id := uniqueID(t, "srcpath")
	registry.Register(id, func() (domain.HarnessModule, error) { return stubModule(id), nil })

	reg, err := registry.Discover(registry.Options{MosaicRoot: root})
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}

	m, err := reg.Resolve(id)
	if err != nil {
		t.Fatalf("Resolve(%q): %v", id, err)
	}
	if m.Ref().SourcePath != "" {
		t.Errorf("built-in Ref().SourcePath = %q, want empty string "+
			"(built-ins have no on-disk descriptor path per domain.HarnessRef comment)", m.Ref().SourcePath)
	}
}

// TestDiscover_HarnessDirectoryWithNoDescriptorFile_IsHandledGracefully pins the registry's
// behaviour when a subdirectory exists under MosaicDeploy/harnesses/ but contains no
// harness.yaml. Discover must not panic and must not return an error. The folder must either
// be absent from List() (silently skipped) or listed with Usable: false — it must never be
// listed as usable when it has no descriptor.
func TestDiscover_HarnessDirectoryWithNoDescriptorFile_IsHandledGracefully(t *testing.T) {
	root := makeRoot(t)
	// Create an empty directory under harnesses/ — no harness.yaml.
	emptyDir := filepath.Join(root, "MosaicDeploy", "harnesses", "no-descriptor-folder")
	must(t, os.MkdirAll(emptyDir, 0o755))

	reg, err := registry.Discover(registry.Options{MosaicRoot: root})
	if err != nil {
		t.Fatalf("Discover returned error for a harness folder with no harness.yaml; "+
			"expected graceful handling (skip or list as unusable): %v", err)
	}
	if reg == nil {
		t.Fatal("Discover returned nil Registry")
	}

	// If the folder appears in List() it must be unusable, not usable.
	ref, present := findRef(reg.List(), "no-descriptor-folder")
	if present && ref.Usable {
		t.Errorf("harness folder with no harness.yaml is listed as Usable: true; "+
			"expected Usable: false or absent from List() entirely")
	}
	// Both "silently skipped" and "listed as Usable: false" are valid behaviours.
	// This test pins whichever the implementation chooses and prevents future regressions.
}

// TestDiscover_IDFromYAMLField_TakesPreferenceOverFolderName pins which identifier is
// authoritative when harness.yaml declares an id that differs from the folder name.
// The YAML id field is the public contract used by Resolve(id); the folder name is a
// storage detail. The harness must be resolvable by its declared YAML id, not by folder name.
func TestDiscover_IDFromYAMLField_TakesPreferenceOverFolderName(t *testing.T) {
	root := makeRoot(t)
	folderName := uniqueID(t, "folder")
	yamlID := uniqueID(t, "yaml-id") // intentionally different from folderName

	// Write a harness.yaml whose id field differs from the containing folder name.
	dir := filepath.Join(root, "MosaicDeploy", "harnesses", folderName)
	must(t, os.MkdirAll(dir, 0o755))
	yaml := fmt.Sprintf("schema_version: \"1\"\nid: %q\ndisplay_name: \"ID Mismatch Test\"\n", yamlID)
	must(t, os.WriteFile(filepath.Join(dir, "harness.yaml"), []byte(yaml), 0o644))

	reg, err := registry.Discover(registry.Options{MosaicRoot: root})
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}

	refs := reg.List()
	// The YAML id field must be the authoritative identifier: the harness is findable by
	// its declared id, not by the folder name.
	if !containsRef(refs, yamlID) {
		t.Errorf("harness not found by YAML id %q; the YAML id field must take precedence "+
			"over the folder name %q for resolution (got: %v)", yamlID, folderName, refIDs(refs))
	}
	if _, err := reg.Resolve(yamlID); err != nil {
		t.Errorf("Resolve(%q) by YAML id failed: %v; the YAML id must be the authoritative key", yamlID, err)
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// refIDs extracts the ID from each HarnessRef for use in diagnostic messages.
func refIDs(refs []domain.HarnessRef) []string {
	ids := make([]string, len(refs))
	for i, r := range refs {
		ids[i] = r.ID
	}
	return ids
}
