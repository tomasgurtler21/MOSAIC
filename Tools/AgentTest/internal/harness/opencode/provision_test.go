package opencode_test

// Tests for provisioning and deprovisioning, and for the single-rewriter
// invariant as it applies to a directory-drop registration model.
//
// OpenCode discovers plugins by their presence in the plugins directory —
// there is no shared configuration document to compose into. The
// single-rewriter invariant is therefore expressed as two checks:
// ComposePlugins refuses two rewriting contributions among the files this
// provisioning will write (unit-testable independently of the filesystem),
// and Provision's foreign-plugin scan refuses a plugins directory that
// already holds someone else's plugin (testable by planting a file).
//
// Note: the collaborator-writing tests that previously appeared here
// (TestProvision_DeploysStubCollaboratorDefinitions,
// TestProvision_RefusesCollaboratorTargetEscapingSandbox,
// TestProvision_PartialFailureStillRecordsWhatWasCreated) have been removed
// together with the mechanism they tested. Stub collaborator definitions are
// now placed by the deployment port (domain.AgentDeployer) before the adapter
// runs, not by the adapter's Provision loop. The new placement contract is
// exercised in internal/runner/render_test.go.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/harness/opencode"
)

func minimalProvisionRequest(sb domain.Sandbox) domain.ProvisionRequest {
	return domain.ProvisionRequest{
		Sandbox:         sb,
		InterceptorPath: "interceptor",
		InterceptorArgs: []string{"intercept"},
	}
}

// --- ComposePlugins: the compose-time half of the single-rewriter check ---

// TestComposePlugins_SingleRewriterSucceeds asserts that composing one
// rewriting contribution alongside non-rewriting ones succeeds and returns
// every declared file.
func TestComposePlugins_SingleRewriterSucceeds(t *testing.T) {
	interceptor := opencode.PluginContribution{
		Source:        "interceptor",
		RewritesInput: true,
		Files:         []opencode.PluginFile{{Target: "mosaic-interceptor.ts", Content: []byte("interceptor")}},
	}
	logger := opencode.PluginContribution{
		Source:        "mosaic-logger",
		RewritesInput: false,
		Files:         []opencode.PluginFile{{Target: "mosaic-logger.ts", Content: []byte("logger")}},
	}

	files, err := opencode.ComposePlugins(interceptor, logger)
	if err != nil {
		t.Fatalf("ComposePlugins: %v", err)
	}
	if len(files) != 2 {
		t.Fatalf("ComposePlugins: got %d files, want 2: %+v", len(files), files)
	}
}

// TestComposePlugins_RefusesTwoRewritingContributions asserts that more
// than one contribution declaring RewritesInput is refused before anything
// would be written, naming both competing sources.
func TestComposePlugins_RefusesTwoRewritingContributions(t *testing.T) {
	first := opencode.PluginContribution{
		Source:        "interceptor",
		RewritesInput: true,
		Files:         []opencode.PluginFile{{Target: "mosaic-interceptor.ts", Content: []byte("a")}},
	}
	second := opencode.PluginContribution{
		Source:        "another-rewriter",
		RewritesInput: true,
		Files:         []opencode.PluginFile{{Target: "another-rewriter.ts", Content: []byte("b")}},
	}

	_, err := opencode.ComposePlugins(first, second)
	if err == nil {
		t.Fatalf("ComposePlugins: want an error for two contributions declaring RewritesInput, got nil")
	}
	if !strings.Contains(err.Error(), "interceptor") || !strings.Contains(err.Error(), "another-rewriter") {
		t.Errorf("ComposePlugins: error %q does not name both competing sources", err.Error())
	}
}

// TestComposePlugins_RefusesTwoContributionsTargetingSameFileName asserts
// that two contributions, even if neither rewrites input, cannot both
// declare a file at the same target — the directory-drop model has only one
// slot per file name.
func TestComposePlugins_RefusesTwoContributionsTargetingSameFileName(t *testing.T) {
	first := opencode.PluginContribution{
		Source: "bundle-a",
		Files:  []opencode.PluginFile{{Target: "shared-name.ts", Content: []byte("a")}},
	}
	second := opencode.PluginContribution{
		Source: "bundle-b",
		Files:  []opencode.PluginFile{{Target: "shared-name.ts", Content: []byte("b")}},
	}

	_, err := opencode.ComposePlugins(first, second)
	if err == nil {
		t.Fatalf("ComposePlugins: want an error for two contributions targeting the same file name, got nil")
	}
}

// TestComposePlugins_NoContributionsIsEmptyNotError asserts that composing
// zero contributions is not itself an error condition.
func TestComposePlugins_NoContributionsIsEmptyNotError(t *testing.T) {
	files, err := opencode.ComposePlugins()
	if err != nil {
		t.Fatalf("ComposePlugins(): %v", err)
	}
	if len(files) != 0 {
		t.Errorf("ComposePlugins(): got %d files, want 0", len(files))
	}
}

// --- Provision / Deprovision integration ---

// TestProvision_WritesPluginUnderSandboxPluginsDirectory asserts that
// Provision writes the generated interceptor plugin into the sandbox's
// plugins directory and records it in the ledger.
func TestProvision_WritesPluginUnderSandboxPluginsDirectory(t *testing.T) {
	dir := t.TempDir()
	sb := newSandbox(t, dir)
	adapter := opencode.New(opencode.Options{})

	prov, err := adapter.Provision(testContext(), minimalProvisionRequest(sb))
	if err != nil {
		t.Fatalf("Provision: %v", err)
	}

	pluginPath := filepath.Join(sb.SubjectDir, opencode.PluginsRelDir, opencode.PluginFileName)
	if _, err := os.Stat(pluginPath); err != nil {
		t.Fatalf("Provision: generated plugin not written at %q: %v", pluginPath, err)
	}

	found := false
	for _, f := range prov.Files {
		if f == pluginPath {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Provisioning.Files does not record the generated plugin %q; got %v", pluginPath, prov.Files)
	}
}

// TestProvision_NeverWritesOutsideTheSandbox asserts that nothing Provision
// creates lands outside req.Sandbox.SubjectDir or req.Sandbox.ControlDir.
func TestProvision_NeverWritesOutsideTheSandbox(t *testing.T) {
	dir := t.TempDir()
	sb := newSandbox(t, dir)
	adapter := opencode.New(opencode.Options{})

	prov, err := adapter.Provision(testContext(), minimalProvisionRequest(sb))
	if err != nil {
		t.Fatalf("Provision: %v", err)
	}

	for _, f := range prov.Files {
		if !strings.HasPrefix(f, sb.SubjectDir) && !strings.HasPrefix(f, sb.ControlDir) {
			t.Errorf("Provisioning.Files contains %q, outside both SubjectDir %q and ControlDir %q", f, sb.SubjectDir, sb.ControlDir)
		}
	}
}

// TestProvision_DeploysLoggerBundleOpenCodeVariantFiles asserts that
// Provision copies exactly the files the fixture bundle's opencode variant
// lists, read through the shared manifest package as data.
func TestProvision_DeploysLoggerBundleOpenCodeVariantFiles(t *testing.T) {
	dir := t.TempDir()
	sb := newSandbox(t, dir)
	adapter := opencode.New(opencode.Options{})

	req := minimalProvisionRequest(sb)
	req.LoggerBundleDir = fixtureBundleDir(t, "fixture-bundle")
	prov, err := adapter.Provision(testContext(), req)
	if err != nil {
		t.Fatalf("Provision: %v", err)
	}

	pluginsDir := filepath.Join(sb.SubjectDir, opencode.PluginsRelDir)
	wantFiles := map[string]string{
		"hook_a.ts":             "hook_a",
		"lib/renamed_hook_b.ts": "hook_b",
		"shared.ts":             "shared",
	}
	for target, marker := range wantFiles {
		full := filepath.Join(pluginsDir, filepath.FromSlash(target))
		data, err := os.ReadFile(full)
		if err != nil {
			t.Errorf("Provision: expected deployed bundle file %q not found: %v", target, err)
			continue
		}
		if !strings.Contains(string(data), marker) {
			t.Errorf("Provision: deployed file %q does not contain expected marker %q; got: %s", target, marker, data)
		}

		found := false
		for _, f := range prov.Files {
			if f == full {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Provisioning.Files does not record deployed bundle file %q; got %v", full, prov.Files)
		}
	}
}

// TestProvision_RefusesBundleFileTargetEscapingPluginsDirectory asserts
// that a bundle-declared file whose target reaches outside the sandbox's
// plugins directory is refused, never written.
func TestProvision_RefusesBundleFileTargetEscapingPluginsDirectory(t *testing.T) {
	dir := t.TempDir()
	sb := newSandbox(t, dir)
	adapter := opencode.New(opencode.Options{})

	req := minimalProvisionRequest(sb)
	req.LoggerBundleDir = fixtureBundleDir(t, "fixture-bundle-escaping-target")
	_, err := adapter.Provision(testContext(), req)
	if err == nil {
		t.Fatalf("Provision: want an error for a bundle file whose target escapes the plugins directory, got nil")
	}

	pluginsDir := filepath.Join(sb.SubjectDir, opencode.PluginsRelDir)
	escaped := filepath.Clean(filepath.Join(pluginsDir, "../../../../escaped-hook.ts"))
	if _, statErr := os.Stat(escaped); statErr == nil {
		t.Errorf("Provision: escaping target was written to %q; the tool must never write outside the sandbox", escaped)
	}
}

// TestProvision_RefusesForeignPluginAlreadyPresent asserts the
// directory-drop model's second single-rewriter check: a plugins directory
// that already holds an entry this provisioning did not create is refused,
// because presence alone activates a plugin and the adapter has no way to
// tell whether it competes.
func TestProvision_RefusesForeignPluginAlreadyPresent(t *testing.T) {
	dir := t.TempDir()
	sb := newSandbox(t, dir)
	adapter := opencode.New(opencode.Options{})

	pluginsDir := filepath.Join(sb.SubjectDir, opencode.PluginsRelDir)
	if err := os.MkdirAll(pluginsDir, 0o755); err != nil {
		t.Fatalf("creating plugins dir: %v", err)
	}
	foreignPath := filepath.Join(pluginsDir, "someone-elses-plugin.ts")
	if err := os.WriteFile(foreignPath, []byte("// not ours"), 0o644); err != nil {
		t.Fatalf("writing foreign plugin: %v", err)
	}

	_, err := adapter.Provision(testContext(), minimalProvisionRequest(sb))
	if err == nil {
		t.Fatalf("Provision: want an error when the plugins directory already holds a foreign plugin, got nil")
	}
	if !strings.Contains(err.Error(), "someone-elses-plugin.ts") {
		t.Errorf("Provision: error %q does not name the foreign plugin entry", err.Error())
	}

	// The foreign file itself must be untouched.
	data, statErr := os.ReadFile(foreignPath)
	if statErr != nil {
		t.Fatalf("foreign plugin file disappeared: %v", statErr)
	}
	if string(data) != "// not ours" {
		t.Errorf("Provision: foreign plugin file content changed unexpectedly")
	}
}

// TestProvision_AttachesScopeFindingsToLedger asserts that Provision's
// ledger carries the same scope findings InspectScopes would independently
// report for the same probe, so a caller inspecting the ledger sees the
// full pre-flight picture rather than some other, merely non-nil, content.
func TestProvision_AttachesScopeFindingsToLedger(t *testing.T) {
	dir := t.TempDir()
	sb := newSandbox(t, dir)
	probe := fixtureScopeProbe{docs: map[string][]byte{}} // every non-sandbox scope absent
	adapter := opencode.New(opencode.Options{ScopeProbe: probe})

	prov, err := adapter.Provision(testContext(), minimalProvisionRequest(sb))
	if err != nil {
		t.Fatalf("Provision: %v", err)
	}
	if prov.ScopeFindings == nil {
		t.Fatalf("Provisioning.ScopeFindings is nil, want the scope findings InspectScopes would report")
	}

	want, err := adapter.InspectScopes(testContext())
	if err != nil {
		t.Fatalf("InspectScopes: %v", err)
	}
	if len(prov.ScopeFindings) != len(want) {
		t.Fatalf("Provisioning.ScopeFindings has %d findings, want %d (matching an independent InspectScopes call): got=%+v want=%+v", len(prov.ScopeFindings), len(want), prov.ScopeFindings, want)
	}
	for i, f := range prov.ScopeFindings {
		if f.Scope.Name != want[i].Scope.Name {
			t.Errorf("Provisioning.ScopeFindings[%d].Scope.Name = %q, want %q (matching an independent InspectScopes call)", i, f.Scope.Name, want[i].Scope.Name)
		}
	}
}

// TestProvision_RefusesLoggerBundleNonEmptyRegistrationForOpenCodeVariant
// asserts the design's explicit safety assertion: the logger bundle's
// opencode variant is documented as declaring an empty registration list
// because plugins are auto-discovered, and Provision must not silently
// proceed if a bundle's opencode variant ever declares a non-empty one — it
// must fail and name the surprise, rather than treating the unexpected
// registration steps as safe to ignore.
func TestProvision_RefusesLoggerBundleNonEmptyRegistrationForOpenCodeVariant(t *testing.T) {
	dir := t.TempDir()
	sb := newSandbox(t, dir)
	adapter := opencode.New(opencode.Options{})

	req := minimalProvisionRequest(sb)
	req.LoggerBundleDir = fixtureBundleDir(t, "fixture-bundle-nonempty-registration")
	_, err := adapter.Provision(testContext(), req)
	if err == nil {
		t.Fatalf("Provision: want an error for a logger bundle opencode variant with a non-empty registration list, got nil")
	}
	if !strings.Contains(err.Error(), "registration") {
		t.Errorf("Provision: error %q does not name the unexpected registration list", err.Error())
	}
}

// TestDeprovision_RemovesExactlyWhatWasRecorded asserts that Deprovision
// removes exactly the ledger's contents and leaves no adapter-authored file
// behind.
func TestDeprovision_RemovesExactlyWhatWasRecorded(t *testing.T) {
	dir := t.TempDir()
	sb := newSandbox(t, dir)
	adapter := opencode.New(opencode.Options{})

	prov, err := adapter.Provision(testContext(), minimalProvisionRequest(sb))
	if err != nil {
		t.Fatalf("Provision: %v", err)
	}

	if err := adapter.Deprovision(testContext(), prov); err != nil {
		t.Fatalf("Deprovision: %v", err)
	}

	for _, f := range prov.Files {
		if _, err := os.Stat(f); !os.IsNotExist(err) {
			t.Errorf("Deprovision: recorded file %q still exists (or stat failed unexpectedly): %v", f, err)
		}
	}
	for _, d := range prov.Dirs {
		if _, err := os.Stat(d); !os.IsNotExist(err) {
			t.Errorf("Deprovision: recorded directory %q still exists (or stat failed unexpectedly): %v", d, err)
		}
	}

	if _, err := os.Stat(sb.SubjectDir); err != nil {
		t.Errorf("Deprovision: subject dir itself was removed, which the ledger never recorded as its own entry: %v", err)
	}
}

// TestDeprovision_IsIdempotent asserts that calling Deprovision a second
// time on the same ledger, once everything is already gone, is not an
// error.
func TestDeprovision_IsIdempotent(t *testing.T) {
	dir := t.TempDir()
	sb := newSandbox(t, dir)
	adapter := opencode.New(opencode.Options{})

	prov, err := adapter.Provision(testContext(), minimalProvisionRequest(sb))
	if err != nil {
		t.Fatalf("Provision: %v", err)
	}

	if err := adapter.Deprovision(testContext(), prov); err != nil {
		t.Fatalf("Deprovision (first call): %v", err)
	}
	if err := adapter.Deprovision(testContext(), prov); err != nil {
		t.Errorf("Deprovision (second call, everything already gone): %v, want nil (idempotent)", err)
	}
}

// TestDeprovision_RemovesChildrenBeforeParentDirectories asserts that
// directory removal order lets Deprovision succeed even though os.Remove
// refuses a non-empty directory: children must be recorded, and removed,
// before their parents.
func TestDeprovision_RemovesChildrenBeforeParentDirectories(t *testing.T) {
	dir := t.TempDir()
	sb := newSandbox(t, dir)
	adapter := opencode.New(opencode.Options{})

	// Use a logger bundle to populate the plugins directory with nested files,
	// which causes Provision to record both files and their parent directories.
	req := minimalProvisionRequest(sb)
	req.LoggerBundleDir = fixtureBundleDir(t, "fixture-bundle")
	prov, err := adapter.Provision(testContext(), req)
	if err != nil {
		t.Fatalf("Provision: %v", err)
	}
	if len(prov.Dirs) == 0 {
		t.Skip("provisioning created no new directories to order; nothing to assert here")
	}

	if err := adapter.Deprovision(testContext(), prov); err != nil {
		t.Fatalf("Deprovision: %v (children-before-parents ordering likely violated)", err)
	}
}
