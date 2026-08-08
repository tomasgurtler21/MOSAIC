package claudecode_test

// Tests for agent-definition deployment and teardown (T10.6).
//
// The stub collaborator definitions are written into the workspace's agent
// directory at setup and recorded in the teardown ledger; uninstall removes
// exactly what the ledger recorded and leaves no adapter-authored file
// behind. Includes the case where setup fails partway: whatever was created
// up to that point is still recorded and still removed.

import (
	"os"
	"path/filepath"
	"testing"

	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/harness/claudecode"
)

func stubCollaborator(agentType, fileName, content string) domain.StubCollaborator {
	return domain.StubCollaborator{
		Identity:   domain.CollaboratorIdentity{ToolName: "Task", AgentIdentity: agentType},
		Definition: []byte(content),
		TargetPath: filepath.ToSlash(filepath.Join(claudecode.AgentsRelDir, fileName)),
	}
}

// TestProvision_DeploysStubCollaboratorDefinitions asserts that every
// collaborator the request declares is written into the workspace's agent
// directory at its declared target path, and recorded in the returned
// ledger.
func TestProvision_DeploysStubCollaboratorDefinitions(t *testing.T) {
	dir := t.TempDir()
	sb := newSandbox(t, dir)
	adapter := claudecode.New(claudecode.Options{})

	collaborators := []domain.StubCollaborator{
		stubCollaborator("Worker", "worker.md", "# Worker stub\n"),
		stubCollaborator("Reviewer", "reviewer.md", "# Reviewer stub\n"),
	}

	req := domain.ProvisionRequest{
		Sandbox:         sb,
		Collaborators:   collaborators,
		InterceptorPath: "interceptor",
		InterceptorArgs: []string{"intercept"},
	}
	prov, err := adapter.Provision(testContext(), req)
	if err != nil {
		t.Fatalf("Provision: %v", err)
	}

	agentsDir := filepath.Join(sb.SubjectDir, claudecode.AgentsRelDir)
	for _, c := range collaborators {
		full := filepath.Join(sb.SubjectDir, filepath.FromSlash(c.TargetPath))
		data, err := os.ReadFile(full)
		if err != nil {
			t.Errorf("Provision: stub collaborator definition %q not written: %v", c.TargetPath, err)
			continue
		}
		if string(data) != string(c.Definition) {
			t.Errorf("Provision: stub collaborator definition %q content = %q, want %q", c.TargetPath, data, c.Definition)
		}

		found := false
		for _, f := range prov.Files {
			if f == full {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Provisioning.Files does not record collaborator definition %q; got %v", full, prov.Files)
		}
	}

	entries, err := os.ReadDir(agentsDir)
	if err != nil {
		t.Fatalf("reading agents dir: %v", err)
	}
	if len(entries) != len(collaborators) {
		t.Errorf("Provision: agents dir has %d entries, want exactly %d (only the declared collaborators)", len(entries), len(collaborators))
	}
}

// TestDeprovision_RemovesExactlyWhatWasRecorded asserts that uninstall
// removes exactly the ledger's contents and leaves no adapter-authored file
// behind — including the parent agent directory it created, once empty.
func TestDeprovision_RemovesExactlyWhatWasRecorded(t *testing.T) {
	dir := t.TempDir()
	sb := newSandbox(t, dir)
	adapter := claudecode.New(claudecode.Options{})

	req := domain.ProvisionRequest{
		Sandbox:         sb,
		Collaborators:   []domain.StubCollaborator{stubCollaborator("Worker", "worker.md", "# Worker stub\n")},
		InterceptorPath: "interceptor",
		InterceptorArgs: []string{"intercept"},
	}
	prov, err := adapter.Provision(testContext(), req)
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

	// Nothing untracked by the ledger under the subject dir should have
	// been touched; the subject dir itself must still exist.
	if _, err := os.Stat(sb.SubjectDir); err != nil {
		t.Errorf("Deprovision: subject dir itself was removed, which the ledger never recorded as its own entry: %v", err)
	}
}

// TestProvision_PartialFailureStillRecordsWhatWasCreated is the
// partial-failure case: when a later step of setup fails (here, a stub
// collaborator whose target path escapes the subject directory), whatever
// Provision created before the failure is still present in the returned
// ledger so a caller can tear it down, per Deprovision's obligation to
// remove exactly what Provisioning records.
func TestProvision_PartialFailureStillRecordsWhatWasCreated(t *testing.T) {
	dir := t.TempDir()
	sb := newSandbox(t, dir)
	adapter := claudecode.New(claudecode.Options{})

	goodCollaborator := stubCollaborator("Worker", "worker.md", "# Worker stub\n")
	escaping := domain.StubCollaborator{
		Identity:   domain.CollaboratorIdentity{ToolName: "Task", AgentIdentity: "Escaper"},
		Definition: []byte("# should never be written"),
		TargetPath: "../../outside-the-sandbox.md",
	}

	req := domain.ProvisionRequest{
		Sandbox:         sb,
		Collaborators:   []domain.StubCollaborator{goodCollaborator, escaping},
		InterceptorPath: "interceptor",
		InterceptorArgs: []string{"intercept"},
	}
	prov, err := adapter.Provision(testContext(), req)
	if err == nil {
		t.Fatalf("Provision: want error for a stub collaborator target path escaping the subject directory, got success")
	}

	// The tool must never write outside the sandbox on any path, including
	// this failure path.
	escapedPath := filepath.Join(dir, "outside-the-sandbox.md")
	if _, statErr := os.Stat(escapedPath); statErr == nil {
		t.Errorf("Provision: wrote %q outside the sandbox despite refusing the request", escapedPath)
	}

	// What was created before the failure (the good collaborator's
	// definition) must still be recorded, so it can be torn down.
	goodPath := filepath.Join(sb.SubjectDir, filepath.FromSlash(goodCollaborator.TargetPath))
	recorded := false
	for _, f := range prov.Files {
		if f == goodPath {
			recorded = true
			break
		}
	}
	if !recorded {
		t.Errorf("Provision: partial failure did not record the file created before the failure (%q); ledger=%v", goodPath, prov.Files)
	}

	if err := adapter.Deprovision(testContext(), prov); err != nil {
		t.Errorf("Deprovision: tearing down a partial ledger returned an error: %v", err)
	}
	if _, statErr := os.Stat(goodPath); !os.IsNotExist(statErr) {
		t.Errorf("Deprovision: file recorded from the partial ledger was not removed: %q", goodPath)
	}
}
