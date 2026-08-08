package claudecode_test

// Tests for logger-bundle deployment driven by the manifest (T10.4).
//
// The adapter resolves the bundle's variant for this harness through the
// shared manifest package and copies exactly the files that variant lists
// into the workspace's hook directory, honouring source/target renames and
// a source path that reaches outside the variant directory (the real bundle
// uses exactly this form for its self-referencing "../hook.yaml" entry).
//
// The test that matters most here is negative: adding a file or an event
// registration to the bundle must change what gets deployed with no edit to
// this tool's source. Both fixture bundles below are deployed through the
// identical adapter code; only the bundle directory passed to Provision
// differs.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/harness/claudecode"
)

func provisionWithBundle(t *testing.T, bundleDirName string) (domain.Provisioning, domain.Sandbox, error) {
	t.Helper()

	dir := t.TempDir()
	sb := newSandbox(t, dir)
	adapter := claudecode.New(claudecode.Options{})

	req := domain.ProvisionRequest{
		Sandbox:         sb,
		LoggerBundleDir: fixtureBundleDir(t, bundleDirName),
		InterceptorPath: "interceptor",
		InterceptorArgs: []string{"intercept"},
	}
	prov, err := adapter.Provision(testContext(), req)
	return prov, sb, err
}

// TestProvision_DeploysExactlyTheManifestsVariantFiles asserts that
// Provision copies exactly the files the fixture bundle's claude-code
// variant lists into the workspace's hook directory, honouring the
// declared source/target renames and the source path that escapes the
// variant directory.
func TestProvision_DeploysExactlyTheManifestsVariantFiles(t *testing.T) {
	_, sb, err := provisionWithBundle(t, "fixture-bundle")
	if err != nil {
		t.Fatalf("Provision: %v", err)
	}

	hooksDir := filepath.Join(sb.SubjectDir, claudecode.HooksRelDir)

	wantFiles := map[string]string{
		"hook_a.py":         "hook_a",
		"renamed_hook_b.py": "hook_b",
		"shared.py":         "shared",
	}
	for target, marker := range wantFiles {
		full := filepath.Join(hooksDir, target)
		data, err := os.ReadFile(full)
		if err != nil {
			t.Errorf("Provision: expected deployed file %q not found: %v", target, err)
			continue
		}
		if !strings.Contains(string(data), marker) {
			t.Errorf("Provision: deployed file %q does not contain expected marker %q; got: %s", target, marker, data)
		}
	}

	// Nothing beyond what the manifest lists should be deployed.
	entries, err := os.ReadDir(hooksDir)
	if err != nil {
		t.Fatalf("reading hooks dir: %v", err)
	}
	if len(entries) != len(wantFiles) {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("Provision: hooks dir has %d entries %v, want exactly %d (%v)", len(entries), names, len(wantFiles), keysOf(wantFiles))
	}
}

// TestProvision_AddingFileOrEventToBundleChangesDeploymentWithNoSourceEdit
// is the negative test that matters most for this task: the same adapter
// code, driven against a bundle that adds one file and one event
// registration, deploys the added file and folds the added event into
// composition, with no change to this adapter's Go source.
func TestProvision_AddingFileOrEventToBundleChangesDeploymentWithNoSourceEdit(t *testing.T) {
	_, baseSandbox, baseErr := provisionWithBundle(t, "fixture-bundle")
	if baseErr != nil {
		t.Fatalf("Provision (base bundle): %v", baseErr)
	}
	_, augmentedSandbox, augErr := provisionWithBundle(t, "fixture-bundle-augmented")
	if augErr != nil {
		t.Fatalf("Provision (augmented bundle): %v", augErr)
	}

	baseHooks := filepath.Join(baseSandbox.SubjectDir, claudecode.HooksRelDir)
	augmentedHooks := filepath.Join(augmentedSandbox.SubjectDir, claudecode.HooksRelDir)

	if _, err := os.Stat(filepath.Join(baseHooks, "hook_c.py")); err == nil {
		t.Errorf("Provision: base bundle unexpectedly deployed hook_c.py, which only the augmented manifest lists")
	}
	if _, err := os.Stat(filepath.Join(augmentedHooks, "hook_c.py")); err != nil {
		t.Errorf("Provision: augmented bundle did not deploy hook_c.py, which its manifest lists: %v", err)
	}

	// The augmented bundle's extra SessionStart registration must be
	// present in the composed settings file; the base bundle's must not.
	baseSettings, err := os.ReadFile(filepath.Join(baseSandbox.SubjectDir, claudecode.SettingsRelPath))
	if err != nil {
		t.Fatalf("reading base settings.json: %v", err)
	}
	augmentedSettings, err := os.ReadFile(filepath.Join(augmentedSandbox.SubjectDir, claudecode.SettingsRelPath))
	if err != nil {
		t.Fatalf("reading augmented settings.json: %v", err)
	}
	if strings.Contains(string(baseSettings), "hook_c.py") {
		t.Errorf("Provision: base settings.json unexpectedly references hook_c.py")
	}
	if !strings.Contains(string(augmentedSettings), "hook_c.py") {
		t.Errorf("Provision: augmented settings.json does not reference hook_c.py's registration")
	}
}

// TestProvision_ProvisioningRecordsDeployedBundleFiles asserts that every
// file Provision deploys from the bundle is recorded in the returned
// ledger, so Deprovision can remove exactly what was created.
func TestProvision_ProvisioningRecordsDeployedBundleFiles(t *testing.T) {
	prov, sb, err := provisionWithBundle(t, "fixture-bundle")
	if err != nil {
		t.Fatalf("Provision: %v", err)
	}

	hooksDir := filepath.Join(sb.SubjectDir, claudecode.HooksRelDir)
	wantTargets := []string{"hook_a.py", "renamed_hook_b.py", "shared.py"}
	for _, target := range wantTargets {
		want := filepath.Join(hooksDir, target)
		found := false
		for _, f := range prov.Files {
			if f == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Provisioning.Files does not record deployed bundle file %q; got %v", want, prov.Files)
		}
	}
}

// TestProvision_RefusesBundleFileTargetEscapingHooksDir is the write-side
// companion to TestProvision_DeploysExactlyTheManifestsVariantFiles's
// source-escape case: a bundle-declared file whose target reaches outside
// the workspace's hooks directory must be refused, never written. AC10.7's
// "the tool never writes outside the workspace" is non-negotiable, and
// unlike the source path (which legitimately escapes the variant directory
// for the real bundle's self-referencing entry), there is no legitimate
// reason for a target to escape HooksRelDir.
func TestProvision_RefusesBundleFileTargetEscapingHooksDir(t *testing.T) {
	dir := t.TempDir()
	sb := newSandbox(t, dir)
	adapter := claudecode.New(claudecode.Options{})

	req := domain.ProvisionRequest{
		Sandbox:         sb,
		LoggerBundleDir: fixtureBundleDir(t, "fixture-bundle-escaping-target"),
		InterceptorPath: "interceptor",
		InterceptorArgs: []string{"intercept"},
	}
	_, err := adapter.Provision(testContext(), req)
	if err == nil {
		t.Fatalf("Provision: want error for a bundle file whose target escapes the hooks directory, got nil")
	}

	hooksDir := filepath.Join(sb.SubjectDir, claudecode.HooksRelDir)
	escaped := filepath.Clean(filepath.Join(hooksDir, "../../../../escaped-hook.py"))
	if _, statErr := os.Stat(escaped); statErr == nil {
		t.Errorf("Provision: escaping target was written to %q; the tool must never write outside the workspace", escaped)
	}
}

func keysOf(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
