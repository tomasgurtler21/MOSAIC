package claudecode_test

// Tests for credential seeding into the relocated config home (T8.1).
//
// spawn.go relocates this harness's user-scope configuration into
// p.Sandbox.ControlDir/claude-home so the competing-rewriter protection in
// scopes.go can report that scope isolated. That directory is also where
// this harness keeps its login credentials, so relocating it without
// seeding credentials back breaks the subject's ability to authenticate —
// the defect Stage-8/Plan.md describes. Provision must seed the harness's
// credential file(s) into the same relocated directory, driven entirely
// through the injectable claudecode.CredentialSource seam so no test run
// ever reads or copies the developer's real credentials (ContractsDesign.md
// "Testability Notes").

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/harness/claudecode"
)

// fixtureCredentialSource is a testable claudecode.CredentialSource
// standing in for the real user-home-based source.
type fixtureCredentialSource struct {
	files []claudecode.CredentialFile
	err   error
}

func (f fixtureCredentialSource) Credentials() ([]claudecode.CredentialFile, error) {
	return f.files, f.err
}

func minimalProvisionRequest(sb domain.Sandbox) domain.ProvisionRequest {
	return domain.ProvisionRequest{
		Sandbox:         sb,
		InterceptorPath: "interceptor",
		InterceptorArgs: []string{"intercept"},
	}
}

// TestProvision_SeedsCredentialsIntoRelocatedConfigHome asserts that a
// credential file the source declares is written under the same relocated
// config-home directory SpawnPlan points ConfigHomeEnvVar at, so the
// subject can still authenticate (AC8.1).
func TestProvision_SeedsCredentialsIntoRelocatedConfigHome(t *testing.T) {
	dir := t.TempDir()
	sb := newSandbox(t, dir)

	src := fixtureCredentialSource{files: []claudecode.CredentialFile{
		{RelPath: ".credentials.json", Content: []byte(`{"token":"fixture-token"}`), Mode: 0o600},
	}}
	adapter := claudecode.New(claudecode.Options{Credentials: src})

	prov, err := adapter.Provision(testContext(), minimalProvisionRequest(sb))
	if err != nil {
		t.Fatalf("Provision: %v", err)
	}

	wantPath := filepath.Join(sb.ControlDir, claudecode.ConfigHomeRelDir, ".credentials.json")
	data, statErr := os.ReadFile(wantPath)
	if statErr != nil {
		t.Fatalf("Provision: credential file not seeded at %q: %v", wantPath, statErr)
	}
	if string(data) != `{"token":"fixture-token"}` {
		t.Errorf("Provision: seeded credential content = %q, want %q", data, `{"token":"fixture-token"}`)
	}

	found := false
	for _, f := range prov.Files {
		if f == wantPath {
			found = true
		}
	}
	if !found {
		t.Errorf("Provisioning.Files does not record the seeded credential path %q; Deprovision must be able to remove it", wantPath)
	}
}

// TestProvision_SeededCredentialIsRecordedSensitive asserts that every
// seeded credential path is recorded in Provisioning.Sensitive, which is
// what makes harness-neutral retention scrubbing possible — a retained
// sandbox must never contain live harness credential material (AC8.3).
func TestProvision_SeededCredentialIsRecordedSensitive(t *testing.T) {
	dir := t.TempDir()
	sb := newSandbox(t, dir)

	src := fixtureCredentialSource{files: []claudecode.CredentialFile{
		{RelPath: ".credentials.json", Content: []byte(`{"token":"fixture-token"}`), Mode: 0o600},
	}}
	adapter := claudecode.New(claudecode.Options{Credentials: src})

	prov, err := adapter.Provision(testContext(), minimalProvisionRequest(sb))
	if err != nil {
		t.Fatalf("Provision: %v", err)
	}

	wantPath := filepath.Join(sb.ControlDir, claudecode.ConfigHomeRelDir, ".credentials.json")
	found := false
	for _, p := range prov.Sensitive {
		if p == wantPath {
			found = true
		}
	}
	if !found {
		t.Errorf("Provisioning.Sensitive = %v, want it to list the seeded credential path %q", prov.Sensitive, wantPath)
	}
}

// TestProvision_NotLoggedIn_EmptyCredentialsIsNotAnError asserts that a
// credential source reporting no credentials (the harness is not logged in)
// does not fail provisioning: the resulting authentication failure is
// diagnosed later from the subject's exit code and stderr, not pre-empted
// here.
func TestProvision_NotLoggedIn_EmptyCredentialsIsNotAnError(t *testing.T) {
	dir := t.TempDir()
	sb := newSandbox(t, dir)

	adapter := claudecode.New(claudecode.Options{Credentials: fixtureCredentialSource{}})

	prov, err := adapter.Provision(testContext(), minimalProvisionRequest(sb))
	if err != nil {
		t.Fatalf("Provision: not being logged in must not be an error, got: %v", err)
	}
	if len(prov.Sensitive) != 0 {
		t.Errorf("Provisioning.Sensitive = %v, want empty when the credential source seeds nothing", prov.Sensitive)
	}
}

// TestProvision_CredentialSourceError_FailsProvision asserts that a genuine
// error from the credential source (as opposed to "not logged in", which
// reports an empty slice with a nil error) fails Provision rather than
// silently proceeding without credentials.
func TestProvision_CredentialSourceError_FailsProvision(t *testing.T) {
	dir := t.TempDir()
	sb := newSandbox(t, dir)

	wantErr := errors.New("fixture: credential source unavailable")
	adapter := claudecode.New(claudecode.Options{Credentials: fixtureCredentialSource{err: wantErr}})

	_, err := adapter.Provision(testContext(), minimalProvisionRequest(sb))
	if err == nil {
		t.Fatalf("Provision: want an error when the credential source fails, got nil")
	}
}

// TestProvision_RefusesCredentialRelPathEscapingConfigHome is the
// credential-seeding companion to the bundle-file escape test: a credential
// file whose RelPath would resolve outside the relocated config home must
// be refused, never written outside the sandbox.
func TestProvision_RefusesCredentialRelPathEscapingConfigHome(t *testing.T) {
	dir := t.TempDir()
	sb := newSandbox(t, dir)

	src := fixtureCredentialSource{files: []claudecode.CredentialFile{
		{RelPath: "../../../../escaped-credentials.json", Content: []byte("secret"), Mode: 0o600},
	}}
	adapter := claudecode.New(claudecode.Options{Credentials: src})

	_, err := adapter.Provision(testContext(), minimalProvisionRequest(sb))
	if err == nil {
		t.Fatalf("Provision: want an error for a credential RelPath escaping the relocated config home, got nil")
	}

	escaped := filepath.Clean(filepath.Join(sb.ControlDir, claudecode.ConfigHomeRelDir, "../../../../escaped-credentials.json"))
	if _, statErr := os.Stat(escaped); statErr == nil {
		t.Errorf("Provision: escaping credential RelPath was written to %q; the tool must never write outside the sandbox", escaped)
	}
}

// TestProvision_CredentialSeeding_SettingsAndHooksRemainIsolated asserts
// AC8.2: seeding credential material into the relocated config home must
// not weaken the user scope's competing-rewriter protection — the composed
// sandbox settings and the seeded credential material are two different
// things, and seeding one must never touch the other.
func TestProvision_CredentialSeeding_SettingsAndHooksRemainIsolated(t *testing.T) {
	dir := t.TempDir()
	sb := newSandbox(t, dir)

	src := fixtureCredentialSource{files: []claudecode.CredentialFile{
		{RelPath: ".credentials.json", Content: []byte(`{"token":"fixture-token"}`), Mode: 0o600},
	}}
	adapter := claudecode.New(claudecode.Options{Credentials: src})

	prov, err := adapter.Provision(testContext(), minimalProvisionRequest(sb))
	if err != nil {
		t.Fatalf("Provision: %v", err)
	}

	settingsPath := filepath.Join(sb.SubjectDir, claudecode.SettingsRelPath)
	settingsData, statErr := os.ReadFile(settingsPath)
	if statErr != nil {
		t.Fatalf("Provision: composed sandbox settings not found at %q: %v", settingsPath, statErr)
	}
	if string(settingsData) == `{"token":"fixture-token"}` {
		t.Errorf("Provision: credential seeding overwrote the composed sandbox settings document")
	}

	// The credential path is recorded Sensitive; the composed settings path
	// must never be, since it is not credential material and Deprovision
	// (not the retention scrubber) is responsible for removing it.
	for _, p := range prov.Sensitive {
		if p == settingsPath {
			t.Errorf("Provisioning.Sensitive unexpectedly lists the composed settings path %q; only credential material belongs there", settingsPath)
		}
	}
}
