package claudecode_test

// Tests that a retained sandbox holds no live credential material (T8.2),
// for both retention modes that leave a sandbox on disk. The generic
// scrub-before-retain mechanism (runner.Teardown scrubbing
// Provisioning.Sensitive) is already covered exhaustively at the runner
// level (internal/runner/lifecycle_test.go); what this file pins is the
// claudecode-specific half of AC8.3: that this adapter's real Provision
// actually seeds credentials and records them Sensitive, so the generic
// scrub mechanism has something correct to act on end to end.

import (
	"os"
	"path/filepath"
	"testing"

	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/harness/claudecode"
	"mosaic-agent-test/internal/runner"
)

// TestRetainedSandbox_NoLiveCredentialMaterial drives the real adapter's
// Provision (seeding a fixture credential) through runner.Teardown under
// each retention mode that leaves a sandbox on disk, and asserts the seeded
// credential file is gone afterward while the sandbox root itself survives.
func TestRetainedSandbox_NoLiveCredentialMaterial(t *testing.T) {
	for _, policy := range []domain.RetentionPolicy{domain.RetainAlways, domain.RetainOnFailure} {
		t.Run(string(policy), func(t *testing.T) {
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

			credPath := filepath.Join(sb.ControlDir, claudecode.ConfigHomeRelDir, ".credentials.json")
			if _, statErr := os.Stat(credPath); statErr != nil {
				t.Fatalf("credential file not present before teardown, nothing to prove scrubbed: %v", statErr)
			}

			ledger := runner.SetupLedger{SandboxRoot: dir, Provisioning: prov}
			outcome, err := runner.Teardown(runner.Deps{}, ledger, runner.AttemptOutcome{Policy: policy, Failed: true})
			if err != nil {
				t.Fatalf("Teardown: %v", err)
			}
			if !outcome.Retained {
				t.Fatalf("RetentionOutcome.Retained = false, want true under %s", policy)
			}
			if _, statErr := os.Stat(dir); statErr != nil {
				t.Errorf("sandbox root is gone after a retained Teardown: %v", statErr)
			}

			if _, statErr := os.Stat(credPath); !os.IsNotExist(statErr) {
				t.Errorf("credential file %q still exists in a retained sandbox under %s (err=%v), want it scrubbed — a retained sandbox must never contain live harness credential material", credPath, policy, statErr)
			}
		})
	}
}
