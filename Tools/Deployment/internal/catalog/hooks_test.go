package catalog_test

// Tests for hook bundle enumeration (T7.4) and integrity validation (T7.5).
//
// Hook bundles live under Agents/Generic/Hooks/{bundle-id}/ and are described by a
// hook.yaml manifest. The repository currently contains one bundle: subagent-logger.
//
// T7.4 covers:
//   - Bundle id, version, description, and placeholder flag
//   - Per-harness variant support: supported vs unsupported
//   - File sets: each supported variant's files are enumerated
//   - Registration steps: parsed and exposed on each variant
//   - Variant reuse: a variant declaring "reuses: <other>" gets the other variant's
//     Files (already resolved by the catalog, not by the caller)
//
// T7.5 covers:
//   - Hook bundle integrity: if hook.yaml carries a content_hash field that does not
//     match the computed hash of all listed variant files, the catalog reports an Issue
//     with code "hook-hash-mismatch".
//   - The real subagent-logger bundle (which has no content_hash field) produces no
//     hash-mismatch issue — the absence of a stored hash means no check is performed.

import (
	"os"
	"path/filepath"
	"testing"

	"mosaic-deploy/internal/catalog"
)

// ---------------------------------------------------------------------------
// T7.4 — Bundle enumeration: real subagent-logger bundle
// ---------------------------------------------------------------------------

// TestHooks_AtLeastOneBundlePresent verifies that the catalog enumerates at least one
// hook bundle from the real repository tree.
func TestHooks_AtLeastOneBundlePresent(t *testing.T) {
	cat := loadRealCatalog(t)
	hooks := cat.Hooks()
	if len(hooks) == 0 {
		t.Fatal("Hooks() returned an empty list; expected at least the subagent-logger bundle")
	}
}

// TestHooks_SubagentLogger_IsPresent verifies that the subagent-logger bundle is found.
func TestHooks_SubagentLogger_IsPresent(t *testing.T) {
	cat := loadRealCatalog(t)
	_, ok := cat.Hook("subagent-logger")
	if !ok {
		t.Fatal("Hook(\"subagent-logger\"): returned not-found; expected this bundle to be present")
	}
}

// TestHook_SubagentLogger_Key verifies the subagent-logger bundle's key matches its id.
func TestHook_SubagentLogger_Key(t *testing.T) {
	cat := loadRealCatalog(t)
	h, ok := cat.Hook("subagent-logger")
	if !ok {
		t.Fatal("Hook(\"subagent-logger\"): not found")
	}
	if h.Key != "subagent-logger" {
		t.Errorf("subagent-logger Key = %q, want %q", h.Key, "subagent-logger")
	}
}

// TestHook_SubagentLogger_Version_NonEmpty verifies that the version field is populated.
func TestHook_SubagentLogger_Version_NonEmpty(t *testing.T) {
	cat := loadRealCatalog(t)
	h, _ := cat.Hook("subagent-logger")
	if h.Version == "" {
		t.Error("subagent-logger Version is empty; expected non-empty version from hook.yaml")
	}
}

// TestHook_SubagentLogger_Description_NonEmpty verifies that the description field is populated.
func TestHook_SubagentLogger_Description_NonEmpty(t *testing.T) {
	cat := loadRealCatalog(t)
	h, _ := cat.Hook("subagent-logger")
	if h.Description == "" {
		t.Error("subagent-logger Description is empty; expected non-empty description from hook.yaml")
	}
}

// TestHook_SubagentLogger_Placeholder_IsTrue verifies that the placeholder flag is set
// for the subagent-logger bundle, which carries placeholder variant content.
func TestHook_SubagentLogger_Placeholder_IsTrue(t *testing.T) {
	cat := loadRealCatalog(t)
	h, _ := cat.Hook("subagent-logger")
	if !h.Placeholder {
		t.Error("subagent-logger Placeholder = false; expected true (hook.yaml declares placeholder: true)")
	}
}

// TestHook_SubagentLogger_SourceDir_IsAbsolute verifies that the bundle's SourceDir is
// an absolute path.
func TestHook_SubagentLogger_SourceDir_IsAbsolute(t *testing.T) {
	cat := loadRealCatalog(t)
	h, _ := cat.Hook("subagent-logger")
	if !filepath.IsAbs(h.SourceDir) {
		t.Errorf("subagent-logger SourceDir is not absolute: %q", h.SourceDir)
	}
}

// TestHook_SubagentLogger_SourceDir_Exists verifies that the SourceDir exists on disk.
func TestHook_SubagentLogger_SourceDir_Exists(t *testing.T) {
	cat := loadRealCatalog(t)
	h, _ := cat.Hook("subagent-logger")
	if _, err := os.Stat(h.SourceDir); os.IsNotExist(err) {
		t.Errorf("subagent-logger SourceDir %q does not exist on disk", h.SourceDir)
	}
}

// ---------------------------------------------------------------------------
// T7.4 — Variant support: supported vs unsupported
// ---------------------------------------------------------------------------

// TestHook_SubagentLogger_Variants_NonEmpty verifies that the Variants map is non-nil
// and contains at least one entry.
func TestHook_SubagentLogger_Variants_NonEmpty(t *testing.T) {
	cat := loadRealCatalog(t)
	h, _ := cat.Hook("subagent-logger")
	if len(h.Variants) == 0 {
		t.Error("subagent-logger Variants is empty; expected variant entries from hook.yaml")
	}
}

// TestHook_Variant_ClaudeCode_IsSupported verifies that the claude-code variant is marked
// as supported.
func TestHook_Variant_ClaudeCode_IsSupported(t *testing.T) {
	cat := loadRealCatalog(t)
	h, _ := cat.Hook("subagent-logger")

	v, ok := h.Variants["claude-code"]
	if !ok {
		t.Fatal("subagent-logger Variants[\"claude-code\"]: not found")
	}
	if !v.Supported {
		t.Error("claude-code variant Supported = false; expected true")
	}
}

// TestHook_Variant_OpenCode_IsSupported verifies that the opencode variant is marked
// as supported.
func TestHook_Variant_OpenCode_IsSupported(t *testing.T) {
	cat := loadRealCatalog(t)
	h, _ := cat.Hook("subagent-logger")

	v, ok := h.Variants["opencode"]
	if !ok {
		t.Fatal("subagent-logger Variants[\"opencode\"]: not found")
	}
	if !v.Supported {
		t.Error("opencode variant Supported = false; expected true")
	}
}

// TestHook_Variant_VscodeGhcp_IsSupported verifies that the vscode-ghcp variant is marked
// as supported.
func TestHook_Variant_VscodeGhcp_IsSupported(t *testing.T) {
	cat := loadRealCatalog(t)
	h, _ := cat.Hook("subagent-logger")

	v, ok := h.Variants["vscode-ghcp"]
	if !ok {
		t.Fatal("subagent-logger Variants[\"vscode-ghcp\"]: not found")
	}
	if !v.Supported {
		t.Error("vscode-ghcp variant Supported = false; expected true")
	}
}

// TestHook_Variant_GhcpCli_IsNotSupported verifies that the ghcp-cli variant is marked
// as not supported.
func TestHook_Variant_GhcpCli_IsNotSupported(t *testing.T) {
	cat := loadRealCatalog(t)
	h, _ := cat.Hook("subagent-logger")

	v, ok := h.Variants["ghcp-cli"]
	if !ok {
		t.Fatal("subagent-logger Variants[\"ghcp-cli\"]: not found")
	}
	if v.Supported {
		t.Error("ghcp-cli variant Supported = true; expected false (hook.yaml declares supported: false)")
	}
}

// ---------------------------------------------------------------------------
// T7.4 — File sets
// ---------------------------------------------------------------------------

// TestHook_Variant_ClaudeCode_Files_TwoFiles verifies that the claude-code variant has
// exactly 2 files as declared in hook.yaml: subagent-logger.ps1 and config.json.
func TestHook_Variant_ClaudeCode_Files_TwoFiles(t *testing.T) {
	const wantCount = 2
	cat := loadRealCatalog(t)
	h, _ := cat.Hook("subagent-logger")

	v, ok := h.Variants["claude-code"]
	if !ok {
		t.Fatal("subagent-logger Variants[\"claude-code\"]: not found")
	}
	if len(v.Files) != wantCount {
		var names []string
		for _, f := range v.Files {
			names = append(names, f.TargetName)
		}
		t.Errorf("claude-code Files count = %d, want %d: %v", len(v.Files), wantCount, names)
	}
}

// TestHook_Variant_ClaudeCode_Files_TargetNames verifies the exact target file names
// for the claude-code variant.
func TestHook_Variant_ClaudeCode_Files_TargetNames(t *testing.T) {
	cat := loadRealCatalog(t)
	h, _ := cat.Hook("subagent-logger")

	v, ok := h.Variants["claude-code"]
	if !ok {
		t.Fatal("subagent-logger Variants[\"claude-code\"]: not found")
	}

	wantTargets := map[string]bool{
		"subagent-logger.ps1":  true,
		"subagent-logger.json": true,
	}
	for _, f := range v.Files {
		if !wantTargets[f.TargetName] {
			t.Errorf("claude-code variant has unexpected target file %q", f.TargetName)
		}
		delete(wantTargets, f.TargetName)
	}
	for remaining := range wantTargets {
		t.Errorf("claude-code variant is missing expected target file %q", remaining)
	}
}

// TestHook_Variant_ClaudeCode_Files_SourcePathsExist verifies that each file listed in
// the claude-code variant has a SourcePath that exists on disk.
func TestHook_Variant_ClaudeCode_Files_SourcePathsExist(t *testing.T) {
	cat := loadRealCatalog(t)
	h, _ := cat.Hook("subagent-logger")

	v, ok := h.Variants["claude-code"]
	if !ok {
		t.Fatal("subagent-logger Variants[\"claude-code\"]: not found")
	}
	for _, f := range v.Files {
		if !filepath.IsAbs(f.SourcePath) {
			t.Errorf("claude-code file %q SourcePath is not absolute: %q", f.TargetName, f.SourcePath)
		}
		if _, err := os.Stat(f.SourcePath); os.IsNotExist(err) {
			t.Errorf("claude-code file %q SourcePath %q does not exist on disk", f.TargetName, f.SourcePath)
		}
	}
}

// ---------------------------------------------------------------------------
// T7.4 — Registration steps
// ---------------------------------------------------------------------------

// TestHook_Variant_ClaudeCode_RegistrationSteps_Present verifies that the claude-code
// variant has at least one registration step.
func TestHook_Variant_ClaudeCode_RegistrationSteps_Present(t *testing.T) {
	cat := loadRealCatalog(t)
	h, _ := cat.Hook("subagent-logger")

	v, ok := h.Variants["claude-code"]
	if !ok {
		t.Fatal("subagent-logger Variants[\"claude-code\"]: not found")
	}
	if len(v.Registration) == 0 {
		t.Error("claude-code variant has no registration steps; expected at least one")
	}
}

// TestHook_Variant_ClaudeCode_RegistrationSteps_HaveIDs verifies that each registration
// step has a non-empty ID.
func TestHook_Variant_ClaudeCode_RegistrationSteps_HaveIDs(t *testing.T) {
	cat := loadRealCatalog(t)
	h, _ := cat.Hook("subagent-logger")

	v, ok := h.Variants["claude-code"]
	if !ok {
		t.Fatal("subagent-logger Variants[\"claude-code\"]: not found")
	}
	for i, step := range v.Registration {
		if step.ID == "" {
			t.Errorf("claude-code registration step[%d] has empty ID", i)
		}
	}
}

// TestHook_Variant_ClaudeCode_RegistrationSteps_HasSettingsFragment verifies that the
// settings-fragment step is present and is marked as performable.
func TestHook_Variant_ClaudeCode_RegistrationSteps_HasSettingsFragment(t *testing.T) {
	cat := loadRealCatalog(t)
	h, _ := cat.Hook("subagent-logger")

	v, ok := h.Variants["claude-code"]
	if !ok {
		t.Fatal("subagent-logger Variants[\"claude-code\"]: not found")
	}
	for _, step := range v.Registration {
		if step.ID == "settings-fragment" {
			if !step.Performable {
				t.Error("claude-code settings-fragment step Performable = false; expected true")
			}
			if step.Fragment == "" {
				t.Error("claude-code settings-fragment step Fragment is empty; expected hook JSON")
			}
			return
		}
	}
	t.Error("claude-code variant missing expected registration step with id \"settings-fragment\"")
}

// TestHook_Variant_OpenCode_RegistrationSteps_Empty verifies that the opencode variant
// has no registration steps (declared as registration: [] in hook.yaml).
func TestHook_Variant_OpenCode_RegistrationSteps_Empty(t *testing.T) {
	cat := loadRealCatalog(t)
	h, _ := cat.Hook("subagent-logger")

	v, ok := h.Variants["opencode"]
	if !ok {
		t.Fatal("subagent-logger Variants[\"opencode\"]: not found")
	}
	if len(v.Registration) != 0 {
		t.Errorf("opencode variant Registration has %d steps, want 0", len(v.Registration))
	}
}

// ---------------------------------------------------------------------------
// T7.4 — Variant reuse resolution
// ---------------------------------------------------------------------------

// TestHook_Variant_VscodeGhcp_ReusesClaudeCode verifies that vscode-ghcp declares
// ReusesVariant = "claude-code" as written in hook.yaml.
func TestHook_Variant_VscodeGhcp_ReusesClaudeCode(t *testing.T) {
	cat := loadRealCatalog(t)
	h, _ := cat.Hook("subagent-logger")

	v, ok := h.Variants["vscode-ghcp"]
	if !ok {
		t.Fatal("subagent-logger Variants[\"vscode-ghcp\"]: not found")
	}
	if v.ReusesVariant != "claude-code" {
		t.Errorf("vscode-ghcp ReusesVariant = %q, want %q", v.ReusesVariant, "claude-code")
	}
}

// TestHook_Variant_VscodeGhcp_FilesResolvedFromClaudeCode verifies that when a variant
// declares "reuses: <other>", the catalog resolves and exposes the OTHER variant's file
// list. The caller must not need to follow the reuse chain manually.
func TestHook_Variant_VscodeGhcp_FilesResolvedFromClaudeCode(t *testing.T) {
	cat := loadRealCatalog(t)
	h, _ := cat.Hook("subagent-logger")

	claudeCode, ok := h.Variants["claude-code"]
	if !ok {
		t.Fatal("subagent-logger Variants[\"claude-code\"]: not found")
	}
	vscode, ok := h.Variants["vscode-ghcp"]
	if !ok {
		t.Fatal("subagent-logger Variants[\"vscode-ghcp\"]: not found")
	}

	// The vscode-ghcp variant reuses claude-code's files; after resolution the Files
	// lists must be equivalent (same target names, since source paths may differ).
	if len(vscode.Files) != len(claudeCode.Files) {
		t.Errorf("vscode-ghcp Files count = %d after reuse resolution, want %d (same as claude-code)",
			len(vscode.Files), len(claudeCode.Files))
		return
	}

	// Compare target names (source path prefix may differ but the deployed names must match).
	claudeTargets := make(map[string]bool)
	for _, f := range claudeCode.Files {
		claudeTargets[f.TargetName] = true
	}
	for _, f := range vscode.Files {
		if !claudeTargets[f.TargetName] {
			t.Errorf("vscode-ghcp resolved file %q not present in claude-code Files", f.TargetName)
		}
	}
}

// TestHook_Variant_GhcpCli_Files_Empty verifies that the unsupported ghcp-cli variant
// has no files (an unsupported variant has no deployable content).
func TestHook_Variant_GhcpCli_Files_Empty(t *testing.T) {
	cat := loadRealCatalog(t)
	h, _ := cat.Hook("subagent-logger")

	v, ok := h.Variants["ghcp-cli"]
	if !ok {
		t.Fatal("subagent-logger Variants[\"ghcp-cli\"]: not found")
	}
	if len(v.Files) != 0 {
		t.Errorf("unsupported ghcp-cli variant has %d files; expected 0", len(v.Files))
	}
}

// ---------------------------------------------------------------------------
// T7.4 — Lookup
// ---------------------------------------------------------------------------

// TestHookLookup_AllBundles_FoundByKey verifies that every bundle returned by Hooks()
// can be looked up by key via Hook(key).
func TestHookLookup_AllBundles_FoundByKey(t *testing.T) {
	cat := loadRealCatalog(t)
	for _, h := range cat.Hooks() {
		found, ok := cat.Hook(h.Key)
		if !ok {
			t.Errorf("Hook(%q): returned not-found, but key was returned by Hooks()", h.Key)
			continue
		}
		if found.Key != h.Key {
			t.Errorf("Hook(%q).Key = %q, want %q", h.Key, found.Key, h.Key)
		}
	}
}

// TestHookLookup_UnknownKey_ReturnsFalse verifies that Hook(key) returns false for an
// unknown bundle id.
func TestHookLookup_UnknownKey_ReturnsFalse(t *testing.T) {
	cat := loadRealCatalog(t)
	_, ok := cat.Hook("this-bundle-does-not-exist")
	if ok {
		t.Error("Hook(\"this-bundle-does-not-exist\"): returned true, want false")
	}
}

// ---------------------------------------------------------------------------
// T7.5 — Integrity validation: content_hash mismatch
// ---------------------------------------------------------------------------

// loadFixtureCatalog loads a catalog from a testdata fixture tree. It fails the test if
// Load itself returns an error (as opposed to accumulating issues). The caller is
// responsible for examining the returned catalog's Issues() list.
func loadFixtureCatalog(t *testing.T, fixtureSubdir string) catalog.Catalog {
	t.Helper()
	fixtureRoot := filepath.Join("..", "..", "testdata", "catalog", fixtureSubdir)
	if _, err := os.Stat(fixtureRoot); os.IsNotExist(err) {
		t.Skipf("Skipping: fixture directory %s not found", fixtureRoot)
	}
	cat, err := catalog.Load(fixtureRoot)
	if err != nil {
		t.Fatalf("catalog.Load(%q): %v", fixtureRoot, err)
	}
	return cat
}

// findIssue returns the first Issue with the given code from the list, and whether one
// was found.
func findIssue(issues []catalog.Issue, code string) (catalog.Issue, bool) {
	for _, iss := range issues {
		if iss.Code == code {
			return iss, true
		}
	}
	return catalog.Issue{}, false
}

// TestIssues_TamperedHookBundle_ReportsHashMismatch verifies that when a hook bundle's
// content_hash field does not match the computed hash of the variant files, the catalog
// reports an Issue with code "hook-hash-mismatch".
//
// The tampered fixture (testdata/catalog/hook-integrity/tampered/) has a hook.yaml that
// stores an intentionally wrong content_hash (all zeroes). The actual variant file content
// does not hash to all zeroes, so the catalog must detect the mismatch.
func TestIssues_TamperedHookBundle_ReportsHashMismatch(t *testing.T) {
	cat := loadFixtureCatalog(t, "hook-integrity/tampered")

	issues := cat.Issues()
	iss, found := findIssue(issues, "hook-hash-mismatch")
	if !found {
		t.Errorf("Issues() does not contain a hook-hash-mismatch issue for the tampered bundle; got: %v", issues)
		return
	}
	if iss.Subject == "" {
		t.Error("hook-hash-mismatch Issue.Subject is empty; expected the bundle id")
	}
	if iss.Message == "" {
		t.Error("hook-hash-mismatch Issue.Message is empty; expected an explanatory message")
	}
}

// TestIssues_TamperedHookBundle_SubjectIsBundle verifies that the hook-hash-mismatch
// issue's Subject identifies the affected bundle by its id.
func TestIssues_TamperedHookBundle_SubjectIsBundle(t *testing.T) {
	cat := loadFixtureCatalog(t, "hook-integrity/tampered")

	iss, found := findIssue(cat.Issues(), "hook-hash-mismatch")
	if !found {
		t.Skip("No hook-hash-mismatch issue found; skipping Subject check")
	}
	if iss.Subject != "test-bundle" {
		t.Errorf("hook-hash-mismatch Issue.Subject = %q, want %q (the bundle id)", iss.Subject, "test-bundle")
	}
}

// TestIssues_RealSubagentLoggerBundle_NoHashMismatch verifies that the real
// subagent-logger bundle does not produce a hook-hash-mismatch issue. The bundle has no
// content_hash field in its hook.yaml, so no integrity check can be performed.
func TestIssues_RealSubagentLoggerBundle_NoHashMismatch(t *testing.T) {
	cat := loadRealCatalog(t)

	for _, iss := range cat.Issues() {
		if iss.Code == "hook-hash-mismatch" {
			t.Errorf("real subagent-logger bundle produced unexpected hook-hash-mismatch issue: %+v", iss)
		}
	}
}

// TestIssues_ValidHashBundle_NoHashMismatch verifies that a hook bundle whose
// content_hash field stores the correct SHA-256 of its variant files produces no
// hook-hash-mismatch issue. This guards against a buggy implementation that rejects
// bundles even when the stored hash is correct.
//
// The fixture at testdata/catalog/hook-integrity/valid-hash/ has a single-file variant
// (test-harness/test-hook.sh, 140 bytes, LF endings). The content_hash in hook.yaml
// is the SHA-256 of that file's raw bytes:
//
//	sha256:1b30980a2aaf3e12da7efa86d67b6005209ece0e80abb23415a2a906a95dab5e
//
// If the implementation's hash algorithm for multi-file bundles differs from a simple
// per-file hash, this fixture may need to be updated alongside that algorithm.
func TestIssues_ValidHashBundle_NoHashMismatch(t *testing.T) {
	cat := loadFixtureCatalog(t, "hook-integrity/valid-hash")

	for _, iss := range cat.Issues() {
		if iss.Code == "hook-hash-mismatch" {
			t.Errorf("valid-hash bundle produced unexpected hook-hash-mismatch issue; "+
				"the stored hash should match the file content: %+v", iss)
		}
	}
}
