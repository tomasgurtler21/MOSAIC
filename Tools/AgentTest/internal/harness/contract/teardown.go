package contract

import (
	"os"
	"testing"
)

// testTeardown asserts that whatever an adapter installs into a workspace,
// it removes: uninstall leaves no adapter-authored file behind. This goes
// beyond testProvisionDeprovision's ledger-vs-disk check by also confirming
// the sandbox subject and control directories are left exactly as they
// were found — empty — rather than merely checking the recorded paths
// individually.
func testTeardown(t *testing.T, cfg Config) {
	t.Helper()

	dir := t.TempDir()
	adapter := cfg.New(t, dir)
	sb := newSandbox(t, dir)

	subjectBefore := listDir(t, sb.SubjectDir)
	controlBefore := listDir(t, sb.ControlDir)

	prov, err := adapter.Provision(testContext(), buildProvisionRequest(sb, cfg.Subject))
	if err != nil {
		t.Fatalf("Provision: %v", err)
	}

	if len(prov.Files) == 0 && len(prov.Dirs) == 0 {
		t.Skip("teardown: adapter installed nothing under the sandbox; nothing to verify removal of")
	}

	if err := adapter.Deprovision(testContext(), prov); err != nil {
		t.Fatalf("Deprovision: %v", err)
	}

	for _, f := range prov.Files {
		if _, err := os.Stat(f); err == nil {
			t.Errorf("teardown: file %q still exists after Deprovision", f)
		} else if !os.IsNotExist(err) {
			t.Errorf("teardown: stat %q after Deprovision: %v", f, err)
		}
	}
	for _, d := range prov.Dirs {
		if _, err := os.Stat(d); err == nil {
			t.Errorf("teardown: directory %q still exists after Deprovision", d)
		} else if !os.IsNotExist(err) {
			t.Errorf("teardown: stat %q after Deprovision: %v", d, err)
		}
	}

	subjectAfter := listDir(t, sb.SubjectDir)
	controlAfter := listDir(t, sb.ControlDir)
	if len(subjectAfter) != len(subjectBefore) {
		t.Errorf("teardown: subject dir has %d entries after Deprovision, had %d before provisioning: %v", len(subjectAfter), len(subjectBefore), subjectAfter)
	}
	if len(controlAfter) != len(controlBefore) {
		t.Errorf("teardown: control dir has %d entries after Deprovision, had %d before provisioning: %v", len(controlAfter), len(controlBefore), controlAfter)
	}
}
