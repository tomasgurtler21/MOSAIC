package app_test

// deployed_duplicate_id_bom_test.go verifies the duplicate-id abort when one of the colliding
// deployed files is BOM-prefixed (and so was previously skipped by the deployed-agent index).
//
// Verified behaviours:
//   - A BOM'd copy declaring the same id as another deployed agent in the agents directory aborts
//     the run with ErrAmbiguousDeployedID before any planning happens.
//   - The error names the id and both file paths.
//   - No workspace file is created, changed or removed.
//
// Both Service.DeployAgents and Service.Deploy are covered.

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mosaic-deploy/internal/app"
	"mosaic-deploy/internal/app/interactiontest"
)

// duplicateIDFiles returns two deployed files declaring id "50": a plain one at the agent's
// planned name and a BOM-prefixed copy under another name.
func duplicateIDFiles() (plainName string, plain []byte, bomName string, bom []byte) {
	body := []byte("---\nid: \"50\"\nversion: \"1.0\"\n---\nbody\n")
	return "plan-review.md", body, "plan-review-copy.md", append(append([]byte{}, classificationBOM...), body...)
}

// snapshotDir returns the relative path -> content of every file under root.
func snapshotDir(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		data, rerr := os.ReadFile(p)
		if rerr != nil {
			return rerr
		}
		rel, _ := filepath.Rel(root, p)
		out[rel] = string(data)
		return nil
	})
	if err != nil {
		t.Fatalf("snapshotDir(%q): %v", root, err)
	}
	return out
}

// assertUnchanged fails when the file set or any content differs from before.
func assertUnchanged(t *testing.T, before, after map[string]string) {
	t.Helper()
	if len(before) != len(after) {
		t.Errorf("workspace file count changed: before %d, after %d", len(before), len(after))
	}
	for name, content := range before {
		if got, ok := after[name]; !ok || got != content {
			t.Errorf("workspace file %q was removed or modified", name)
		}
	}
}

// assertAmbiguousIDError checks the error is ErrAmbiguousDeployedID and names the id and both paths.
func assertAmbiguousIDError(t *testing.T, err error, id string, names ...string) {
	t.Helper()
	if err == nil {
		t.Fatal("run returned nil error; a duplicate id must abort the run")
	}
	if !errors.Is(err, app.ErrAmbiguousDeployedID) {
		t.Fatalf("error = %v; want it to match ErrAmbiguousDeployedID", err)
	}
	if !strings.Contains(err.Error(), id) {
		t.Errorf("error %q does not name the id %q", err.Error(), id)
	}
	for _, n := range names {
		if !strings.Contains(err.Error(), n) {
			t.Errorf("error %q does not name the conflicting path %q", err.Error(), n)
		}
	}
}

func TestDeployAgents_BOMCopyDeclaringSameIDAsAnotherFile_AbortsWithAmbiguousID(t *testing.T) {
	// Arrange
	deps, capExec, workspace := newClassificationDeps(t)
	plainName, plain, bomName, bom := duplicateIDFiles()
	dir := filepath.Join(workspace, classificationAgentsDir)
	writeTempFileMkdir(t, dir, plainName, plain)
	writeTempFileMkdir(t, dir, bomName, bom)
	before := snapshotDir(t, workspace)
	svc := app.New(deps)

	// Act
	_, err := svc.DeployAgents(context.Background(),
		classificationRequest(workspace, []string{classificationIDAgent.Key}, []string{}, []string{}))

	// Assert
	assertAmbiguousIDError(t, err, "50", plainName, bomName)
	if capExec.capturedPlan != nil {
		t.Error("executor was reached; the run must abort before planning and writing")
	}
	assertUnchanged(t, before, snapshotDir(t, workspace))
}

func TestDeployNew_BOMCopyDeclaringSameIDAsAnotherFile_AbortsWithAmbiguousID(t *testing.T) {
	// Arrange - the minimal catalog's workflow references agent "test-runner" (id "1").
	stub := interactiontest.NewBuilder().Build()
	deps, workspace := newBaseDeps(t, stub)
	spy := &spyPlanner{response: newMinimalPlan(workspace)}
	deps.Planner = spy
	deps.Registry = newIDAwareRegistry(newIDAwareHarnessModule(classificationAgentsDir))
	dir := filepath.Join(workspace, classificationAgentsDir)
	body := []byte("---\nid: \"1\"\nversion: \"1.0\"\n---\nbody\n")
	writeTempFileMkdir(t, dir, "test-runner.md", body)
	writeTempFileMkdir(t, dir, "test-runner-copy.md", append(append([]byte{}, classificationBOM...), body...))
	before := snapshotDir(t, workspace)
	svc := app.New(deps)

	// Act
	_, err := svc.DeployNew(context.Background(), newDeployRequest(workspace, "stub-harness", []string{"quick-fix"}))

	// Assert
	assertAmbiguousIDError(t, err, "1", "test-runner.md", "test-runner-copy.md")
	if len(spy.capturedInputs) != 0 {
		t.Error("planner was called; the run must abort before planning")
	}
	assertUnchanged(t, before, snapshotDir(t, workspace))
}
