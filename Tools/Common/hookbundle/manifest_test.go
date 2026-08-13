package hookbundle_test

// Tests for manifest parsing (Parse and Load) against fixture manifests and
// against the real mosaic-logger bundle.
//
// Fixture coverage (Tools/Common/testdata/hookbundle/):
//   - complete.yaml               — every field populated, multiple variants
//   - absent-optional-fields.yaml — placeholder/content_hash legitimately absent
//   - unsupported-variant.yaml    — a variant marked unsupported
//   - empty-registration.yaml     — a variant with an empty registration list (directory-drop case)
//   - reuses-variant.yaml         — a variant using the reuses indirection
//   - escaping-source.yaml        — a file entry whose source escapes the variant directory
//   - malformed.yaml              — structurally invalid input
//   - load-sample/hook.yaml       — a bundle directory, for Load

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mosaic-common/hookbundle"
)

// testdataDir is relative to this package's working directory (Common/hookbundle).
const testdataDir = "../testdata/hookbundle"

// repoRoot locates the repository root from this package's working directory
// (Tools/Common/hookbundle), so tests can exercise the real mosaic-logger bundle.
const repoRoot = "../../.."

func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(testdataDir, name))
	if err != nil {
		t.Fatalf("reading fixture %q: %v", name, err)
	}
	return data
}

// ---------------------------------------------------------------------------
// T3.1 — Complete manifest: whole-bundle fields
// ---------------------------------------------------------------------------

func TestParse_CompleteManifest_PopulatesWholeBundleFields(t *testing.T) {
	data := readFixture(t, "complete.yaml")

	m, err := hookbundle.Parse(data)
	if err != nil {
		t.Fatalf("Parse: unexpected error: %v", err)
	}

	if m.SchemaVersion != "1" {
		t.Errorf("SchemaVersion = %q, want %q", m.SchemaVersion, "1")
	}
	if m.ID != "sample-bundle" {
		t.Errorf("ID = %q, want %q", m.ID, "sample-bundle")
	}
	if m.Version != "2.0.0" {
		t.Errorf("Version = %q, want %q", m.Version, "2.0.0")
	}
	if m.Description == "" {
		t.Error("Description is empty; expected the fixture's description")
	}
	if !m.Placeholder {
		t.Error("Placeholder = false, want true (fixture declares placeholder: true)")
	}
	if m.ContentHash != "sha256:deadbeef" {
		t.Errorf("ContentHash = %q, want %q", m.ContentHash, "sha256:deadbeef")
	}
}

func TestParse_CompleteManifest_VariantsPresent(t *testing.T) {
	data := readFixture(t, "complete.yaml")

	m, err := hookbundle.Parse(data)
	if err != nil {
		t.Fatalf("Parse: unexpected error: %v", err)
	}

	if len(m.Variants) != 2 {
		t.Fatalf("len(Variants) = %d, want 2", len(m.Variants))
	}
	if _, ok := m.Variants["alpha"]; !ok {
		t.Error(`Variants["alpha"] not found`)
	}
	if _, ok := m.Variants["beta"]; !ok {
		t.Error(`Variants["beta"] not found`)
	}
}

func TestParse_CompleteManifest_VariantFiles(t *testing.T) {
	data := readFixture(t, "complete.yaml")

	m, err := hookbundle.Parse(data)
	if err != nil {
		t.Fatalf("Parse: unexpected error: %v", err)
	}

	v := m.Variants["alpha"]
	if !v.Supported {
		t.Error(`Variants["alpha"].Supported = false, want true`)
	}
	if len(v.Files) != 1 {
		t.Fatalf(`len(Variants["alpha"].Files) = %d, want 1`, len(v.Files))
	}
	if v.Files[0].Source != "alpha.py" || v.Files[0].Target != "alpha.py" {
		t.Errorf(`Variants["alpha"].Files[0] = %+v, want Source/Target "alpha.py"`, v.Files[0])
	}
}

func TestParse_CompleteManifest_RegistrationStepFields(t *testing.T) {
	data := readFixture(t, "complete.yaml")

	m, err := hookbundle.Parse(data)
	if err != nil {
		t.Fatalf("Parse: unexpected error: %v", err)
	}

	v := m.Variants["alpha"]
	if len(v.Registration) != 1 {
		t.Fatalf(`len(Variants["alpha"].Registration) = %d, want 1`, len(v.Registration))
	}
	step := v.Registration[0]
	if step.ID != "settings-fragment" {
		t.Errorf("Registration[0].ID = %q, want %q", step.ID, "settings-fragment")
	}
	if step.TargetPath != ".alpha/settings.json" {
		t.Errorf("Registration[0].TargetPath = %q, want %q", step.TargetPath, ".alpha/settings.json")
	}
	if !step.Performable {
		t.Error("Registration[0].Performable = false, want true")
	}
	if step.Instruction == "" {
		t.Error("Registration[0].Instruction is empty")
	}
	if !strings.Contains(step.Fragment, `"hooks"`) {
		t.Errorf("Registration[0].Fragment = %q, want it to contain the authored JSON fragment", step.Fragment)
	}
}

// ---------------------------------------------------------------------------
// T3.1 — Absent optional whole-bundle fields
// ---------------------------------------------------------------------------

func TestParse_AbsentOptionalFields_PlaceholderIsZeroValue(t *testing.T) {
	data := readFixture(t, "absent-optional-fields.yaml")

	m, err := hookbundle.Parse(data)
	if err != nil {
		t.Fatalf("Parse: unexpected error: %v", err)
	}

	if m.Placeholder {
		t.Error("Placeholder = true, want false (key omitted from manifest)")
	}
}

func TestParse_AbsentOptionalFields_ContentHashIsEmpty(t *testing.T) {
	data := readFixture(t, "absent-optional-fields.yaml")

	m, err := hookbundle.Parse(data)
	if err != nil {
		t.Fatalf("Parse: unexpected error: %v", err)
	}

	if m.ContentHash != "" {
		t.Errorf("ContentHash = %q, want empty string (key omitted from manifest)", m.ContentHash)
	}
}

// ---------------------------------------------------------------------------
// T3.1 — A variant marked unsupported
// ---------------------------------------------------------------------------

func TestParse_UnsupportedVariant_SupportedIsFalse(t *testing.T) {
	data := readFixture(t, "unsupported-variant.yaml")

	m, err := hookbundle.Parse(data)
	if err != nil {
		t.Fatalf("Parse: unexpected error: %v", err)
	}

	v, ok := m.Variants["gamma"]
	if !ok {
		t.Fatal(`Variants["gamma"] not found`)
	}
	if v.Supported {
		t.Error(`Variants["gamma"].Supported = true, want false`)
	}
}

// ---------------------------------------------------------------------------
// T3.1 — A variant with an empty registration list (directory-drop case)
// ---------------------------------------------------------------------------

func TestParse_EmptyRegistrationVariant_RegistrationIsEmptyNotNilPanic(t *testing.T) {
	data := readFixture(t, "empty-registration.yaml")

	m, err := hookbundle.Parse(data)
	if err != nil {
		t.Fatalf("Parse: unexpected error: %v", err)
	}

	v, ok := m.Variants["dropvariant"]
	if !ok {
		t.Fatal(`Variants["dropvariant"] not found`)
	}
	if len(v.Registration) != 0 {
		t.Errorf(`Variants["dropvariant"].Registration has %d steps, want 0`, len(v.Registration))
	}
	if !v.Supported {
		t.Error(`Variants["dropvariant"].Supported = false, want true`)
	}
}

// ---------------------------------------------------------------------------
// T3.1 — A variant using the reuses indirection
// ---------------------------------------------------------------------------

func TestParse_ReusesVariant_RecordsReuseKey(t *testing.T) {
	data := readFixture(t, "reuses-variant.yaml")

	m, err := hookbundle.Parse(data)
	if err != nil {
		t.Fatalf("Parse: unexpected error: %v", err)
	}

	v, ok := m.Variants["secondary"]
	if !ok {
		t.Fatal(`Variants["secondary"] not found`)
	}
	if v.Reuses != "primary" {
		t.Errorf(`Variants["secondary"].Reuses = %q, want %q`, v.Reuses, "primary")
	}
}

func TestParse_ReusesVariant_ReferencedVariantUnaffected(t *testing.T) {
	data := readFixture(t, "reuses-variant.yaml")

	m, err := hookbundle.Parse(data)
	if err != nil {
		t.Fatalf("Parse: unexpected error: %v", err)
	}

	primary, ok := m.Variants["primary"]
	if !ok {
		t.Fatal(`Variants["primary"] not found`)
	}
	if primary.Reuses != "" {
		t.Errorf(`Variants["primary"].Reuses = %q, want empty (it is the reused variant, not the reuser)`, primary.Reuses)
	}
	if len(primary.Files) != 1 {
		t.Errorf(`Variants["primary"].Files has %d entries, want 1`, len(primary.Files))
	}
}

// ---------------------------------------------------------------------------
// T3.1 — File entries whose source escapes the variant directory
// ---------------------------------------------------------------------------

func TestParse_EscapingFileSource_PreservedVerbatim(t *testing.T) {
	data := readFixture(t, "escaping-source.yaml")

	m, err := hookbundle.Parse(data)
	if err != nil {
		t.Fatalf("Parse: unexpected error: %v", err)
	}

	v, ok := m.Variants["alpha"]
	if !ok {
		t.Fatal(`Variants["alpha"] not found`)
	}

	var found bool
	for _, f := range v.Files {
		if f.Source == "../hook.yaml" {
			found = true
			if f.Target != "hook.yaml" {
				t.Errorf("escaping file entry Target = %q, want %q", f.Target, "hook.yaml")
			}
		}
	}
	if !found {
		t.Errorf(`Variants["alpha"].Files does not contain a "../hook.yaml" source entry: %+v`, v.Files)
	}
}

// ---------------------------------------------------------------------------
// T3.1 — Malformed input
// ---------------------------------------------------------------------------

func TestParse_MalformedManifest_ReturnsError(t *testing.T) {
	data := readFixture(t, "malformed.yaml")

	_, err := hookbundle.Parse(data)
	if err == nil {
		t.Fatal("Parse: expected an error for malformed input, got nil")
	}
}

func TestParse_MalformedManifest_ErrorNamesTheProblem(t *testing.T) {
	data := readFixture(t, "malformed.yaml")

	_, err := hookbundle.Parse(data)
	if err == nil {
		t.Fatal("Parse: expected an error for malformed input, got nil")
	}
	if err.Error() == "" {
		t.Error("Parse error message is empty; expected a diagnosable message naming what failed")
	}
}

func TestParse_MalformedManifest_DoesNotYieldZeroValuedSuccess(t *testing.T) {
	data := readFixture(t, "malformed.yaml")

	m, err := hookbundle.Parse(data)
	if err == nil && m.ID == "" {
		t.Error("Parse returned a zero-valued manifest with no error for malformed input; " +
			"malformed input must produce a diagnosable error, never a silently empty result")
	}
}

func TestParse_MalformedManifest_WrapsMalformedManifestSentinel(t *testing.T) {
	data := readFixture(t, "malformed.yaml")

	_, err := hookbundle.Parse(data)
	if err == nil {
		t.Fatal("Parse: expected an error for malformed input, got nil")
	}
	if !errors.Is(err, hookbundle.ErrMalformedManifest) {
		t.Errorf("Parse error does not wrap ErrMalformedManifest via errors.Is: %v", err)
	}
}

// ---------------------------------------------------------------------------
// T3.1 — Load reads hook.yaml from a bundle directory
// ---------------------------------------------------------------------------

func TestLoad_BundleDirectory_ParsesHookYAML(t *testing.T) {
	bundleDir := filepath.Join(testdataDir, "load-sample")

	m, err := hookbundle.Load(bundleDir)
	if err != nil {
		t.Fatalf("Load(%q): unexpected error: %v", bundleDir, err)
	}
	if m.ID != "load-sample-bundle" {
		t.Errorf("ID = %q, want %q", m.ID, "load-sample-bundle")
	}
}

func TestLoad_MissingBundleDirectory_ReturnsError(t *testing.T) {
	bundleDir := filepath.Join(testdataDir, "this-bundle-does-not-exist")

	_, err := hookbundle.Load(bundleDir)
	if err == nil {
		t.Fatal("Load: expected an error for a missing bundle directory, got nil")
	}
}

// ---------------------------------------------------------------------------
// AC3.3 — The real mosaic-logger manifest parses through the shared package
// with all four variants, their file lists and registration fragments intact.
// ---------------------------------------------------------------------------

func realLoggerBundleDir(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(repoRoot, "Catalog", "Hooks", "mosaic-logger")
	if _, err := os.Stat(filepath.Join(dir, "hook.yaml")); err != nil {
		t.Skipf("real mosaic-logger bundle not found at %q: %v", dir, err)
	}
	return dir
}

func TestLoad_RealMosaicLoggerBundle_AllFourVariantsPresent(t *testing.T) {
	m, err := hookbundle.Load(realLoggerBundleDir(t))
	if err != nil {
		t.Fatalf("Load: unexpected error: %v", err)
	}

	for _, key := range []string{"claude-code", "opencode", "vscode-ghcp", "ghcp-cli"} {
		v, ok := m.Variants[key]
		if !ok {
			t.Errorf("Variants[%q] not found", key)
			continue
		}
		if !v.Supported {
			t.Errorf("Variants[%q].Supported = false, want true", key)
		}
	}
}

func TestLoad_RealMosaicLoggerBundle_PlaceholderAndContentHashAbsent(t *testing.T) {
	m, err := hookbundle.Load(realLoggerBundleDir(t))
	if err != nil {
		t.Fatalf("Load: unexpected error: %v", err)
	}

	if m.Placeholder {
		t.Error("Placeholder = true, want false; the real bundle's comments explain both variants are fully authored")
	}
	if m.ContentHash != "" {
		t.Errorf("ContentHash = %q, want empty; the real bundle's comments explain hashing would be unstable "+
			"because claude-code lists ../hook.yaml in its own file set", m.ContentHash)
	}
}

func TestLoad_RealMosaicLoggerBundle_OpenCodeRegistrationEmpty(t *testing.T) {
	m, err := hookbundle.Load(realLoggerBundleDir(t))
	if err != nil {
		t.Fatalf("Load: unexpected error: %v", err)
	}

	v, ok := m.Variants["opencode"]
	if !ok {
		t.Fatal(`Variants["opencode"] not found`)
	}
	if len(v.Registration) != 0 {
		t.Errorf(`Variants["opencode"].Registration has %d steps, want 0 (opencode registers by directory drop)`, len(v.Registration))
	}
}

func TestLoad_RealMosaicLoggerBundle_ClaudeCodeFileEscapesVariantDirectory(t *testing.T) {
	m, err := hookbundle.Load(realLoggerBundleDir(t))
	if err != nil {
		t.Fatalf("Load: unexpected error: %v", err)
	}

	v, ok := m.Variants["claude-code"]
	if !ok {
		t.Fatal(`Variants["claude-code"] not found`)
	}
	var found bool
	for _, f := range v.Files {
		if f.Source == "../hook.yaml" && f.Target == "hook.yaml" {
			found = true
		}
	}
	if !found {
		t.Errorf(`Variants["claude-code"].Files does not contain the self-referencing "../hook.yaml" entry: %+v`, v.Files)
	}
}

func TestLoad_RealMosaicLoggerBundle_ClaudeCodeFragmentPreservesStructureVerbatim(t *testing.T) {
	m, err := hookbundle.Load(realLoggerBundleDir(t))
	if err != nil {
		t.Fatalf("Load: unexpected error: %v", err)
	}

	v, ok := m.Variants["claude-code"]
	if !ok {
		t.Fatal(`Variants["claude-code"] not found`)
	}

	var step *hookbundle.RegistrationStep
	for i := range v.Registration {
		if v.Registration[i].ID == "settings-fragment" {
			step = &v.Registration[i]
			break
		}
	}
	if step == nil {
		t.Fatal(`Variants["claude-code"].Registration has no "settings-fragment" step`)
	}

	// The fragment is copied verbatim, never round-tripped through a parsed
	// structure: whitespace and key nesting from the authored JSON survive.
	wantSnippet := "  \"hooks\": {\n    \"SessionStart\": ["
	if !strings.Contains(step.Fragment, wantSnippet) {
		t.Errorf("Fragment does not preserve the authored JSON's exact structure and indentation.\n"+
			"want it to contain:\n%s\ngot:\n%s", wantSnippet, step.Fragment)
	}
}

func TestLoad_RealMosaicLoggerBundle_GhcpCliFileCount(t *testing.T) {
	m, err := hookbundle.Load(realLoggerBundleDir(t))
	if err != nil {
		t.Fatalf("Load: unexpected error: %v", err)
	}

	v, ok := m.Variants["ghcp-cli"]
	if !ok {
		t.Fatal(`Variants["ghcp-cli"] not found`)
	}
	const want = 10 // 9 Python adapter modules plus the self-deployed hook.yaml
	if len(v.Files) != want {
		t.Errorf(`Variants["ghcp-cli"].Files has %d entries, want %d`, len(v.Files), want)
	}
}
