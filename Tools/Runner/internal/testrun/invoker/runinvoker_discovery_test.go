// Tests for snapshotOrchestrationDirs and discoverNewestOrchestrationDir.
package invoker

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestSnapshotOrchestrationDirs_ReturnsOrchestrationDirs verifies that
// snapshotOrchestrationDirs returns only Orchestration-* directories from the
// workspace (not files, not other directories).
func TestSnapshotOrchestrationDirs_ReturnsOrchestrationDirs(t *testing.T) {
	ws := t.TempDir()

	// Create directories and files.
	orchDir1 := "Orchestration-abc123"
	orchDir2 := "Orchestration-def456"
	otherDir := "SomeOtherDir"
	orchFile := "Orchestration-not-a-dir.txt"

	for _, name := range []string{orchDir1, orchDir2, otherDir} {
		if err := os.Mkdir(filepath.Join(ws, name), 0755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(ws, orchFile), []byte(""), 0644); err != nil {
		t.Fatal(err)
	}

	snapshot := snapshotOrchestrationDirs(ws)

	if _, ok := snapshot[orchDir1]; !ok {
		t.Errorf("snapshot missing %q", orchDir1)
	}
	if _, ok := snapshot[orchDir2]; !ok {
		t.Errorf("snapshot missing %q", orchDir2)
	}
	if _, ok := snapshot[otherDir]; ok {
		t.Errorf("snapshot includes non-Orchestration dir %q", otherDir)
	}
	if _, ok := snapshot[orchFile]; ok {
		t.Errorf("snapshot includes file (not a directory) %q", orchFile)
	}
}

// TestSnapshotOrchestrationDirs_EmptyWorkspace_ReturnsEmptyMap verifies that
// when the workspace contains no Orchestration-* dirs, an empty (non-nil) map
// is returned.
func TestSnapshotOrchestrationDirs_EmptyWorkspace_ReturnsEmptyMap(t *testing.T) {
	ws := t.TempDir()
	// Create a non-Orchestration dir to ensure the workspace is not empty.
	if err := os.Mkdir(filepath.Join(ws, "SomeOtherDir"), 0755); err != nil {
		t.Fatal(err)
	}

	snapshot := snapshotOrchestrationDirs(ws)

	if snapshot == nil {
		t.Error("snapshotOrchestrationDirs returned nil, want non-nil empty map")
	}
	if len(snapshot) != 0 {
		t.Errorf("snapshot len = %d, want 0; snapshot = %v", len(snapshot), snapshot)
	}
}

// TestSnapshotOrchestrationDirs_NonExistentWorkspace_ReturnsEmptyMap verifies
// that when the workspace does not exist, snapshotOrchestrationDirs returns a
// non-nil empty map (no error is returned; the pre-invoke snapshot failure is
// silent and non-fatal).
func TestSnapshotOrchestrationDirs_NonExistentWorkspace_ReturnsEmptyMap(t *testing.T) {
	nonExistent := filepath.Join(t.TempDir(), "does-not-exist")

	snapshot := snapshotOrchestrationDirs(nonExistent)

	if snapshot == nil {
		t.Error("snapshotOrchestrationDirs returned nil for non-existent workspace, want non-nil empty map")
	}
	if len(snapshot) != 0 {
		t.Errorf("snapshot len = %d, want 0 for non-existent workspace", len(snapshot))
	}
}

// TestSnapshotOrchestrationDirs_OnlyOrchestrationPrefixDirectories verifies
// that directories named "Orchestration-" exactly (zero suffix) and directories
// named differently are treated correctly.
func TestSnapshotOrchestrationDirs_OnlyOrchestrationPrefixDirectories(t *testing.T) {
	ws := t.TempDir()

	// Has the prefix and is a directory.
	orchWithSuffix := "Orchestration-run-abc"
	// Exact prefix only (no suffix after the dash).
	orchExact := "Orchestration-"
	// Does NOT have the prefix.
	notOrch := "NotOrchestration-abc"

	for _, name := range []string{orchWithSuffix, orchExact, notOrch} {
		if err := os.Mkdir(filepath.Join(ws, name), 0755); err != nil {
			t.Fatal(err)
		}
	}

	snapshot := snapshotOrchestrationDirs(ws)

	if _, ok := snapshot[orchWithSuffix]; !ok {
		t.Errorf("snapshot missing %q", orchWithSuffix)
	}
	if _, ok := snapshot[orchExact]; !ok {
		t.Errorf("snapshot missing %q (dir named exactly 'Orchestration-' is valid)", orchExact)
	}
	if _, ok := snapshot[notOrch]; ok {
		t.Errorf("snapshot includes %q which does not start with 'Orchestration-'", notOrch)
	}
}

// TestDiscoverNewestOrchestrationDir_IgnoresPreExistingDirs verifies that
// directories in the preExisting set are excluded from consideration even when
// they have a newer mtime than any new directory.
func TestDiscoverNewestOrchestrationDir_IgnoresPreExistingDirs(t *testing.T) {
	ws := t.TempDir()

	// Create "new" folder first so it will have an older mtime.
	newPath := filepath.Join(ws, "Orchestration-new-run")
	if err := os.Mkdir(newPath, 0755); err != nil {
		t.Fatal(err)
	}

	// Create pre-existing folder.
	preExistingName := "Orchestration-old-run"
	prePath := filepath.Join(ws, preExistingName)
	if err := os.Mkdir(prePath, 0755); err != nil {
		t.Fatal(err)
	}

	// Give the pre-existing folder a future mtime to ensure a naive
	// "newest by mtime" approach (without filtering) would select it.
	futureTime := time.Now().Add(time.Hour)
	if err := os.Chtimes(prePath, futureTime, futureTime); err != nil {
		t.Fatal(err)
	}

	// Build the preExisting snapshot containing the old-run folder.
	preExisting := map[string]struct{}{
		preExistingName: {},
	}

	got, err := discoverNewestOrchestrationDir(ws, preExisting)
	if err != nil {
		t.Fatalf("discoverNewestOrchestrationDir returned error: %v", err)
	}

	if got != newPath {
		t.Errorf("discoverNewestOrchestrationDir = %q, want %q\n(pre-existing dir %q must be ignored even though it has a newer mtime)",
			got, newPath, preExistingName)
	}
}

// TestDiscoverNewestOrchestrationDir_NoNewFolder_ErrorHasListing verifies that
// when no new Orchestration-* directories appear (all are pre-existing), the
// returned error is a *discoveryError with Kind == discoveryNoNewFolder and
// Listing populated with the workspace directory names.
func TestDiscoverNewestOrchestrationDir_NoNewFolder_ErrorHasListing(t *testing.T) {
	ws := t.TempDir()

	// Create an Orchestration dir that will be marked as pre-existing.
	existingName := "Orchestration-already-exists"
	if err := os.Mkdir(filepath.Join(ws, existingName), 0755); err != nil {
		t.Fatal(err)
	}
	// Also create a non-matching file to verify listing completeness.
	if err := os.WriteFile(filepath.Join(ws, "some-file.txt"), []byte(""), 0644); err != nil {
		t.Fatal(err)
	}

	// All Orchestration-* dirs are pre-existing.
	preExisting := map[string]struct{}{existingName: {}}

	_, err := discoverNewestOrchestrationDir(ws, preExisting)
	if err == nil {
		t.Fatal("expected error when all Orchestration-* dirs are pre-existing, got nil")
	}

	var de *discoveryError
	if !errors.As(err, &de) {
		t.Fatalf("error is not *discoveryError: %T: %v", err, err)
	}

	if de.Kind != discoveryNoNewFolder {
		t.Errorf("discoveryError.Kind = %v, want discoveryNoNewFolder", de.Kind)
	}

	// Workspace field must record the path that was scanned.
	if de.Workspace != ws {
		t.Errorf("discoveryError.Workspace = %q, want %q", de.Workspace, ws)
	}

	// Cause must be non-nil: the no-new-folder path sets a descriptive error.
	if de.Cause == nil {
		t.Errorf("discoveryError.Cause is nil on no-new-folder path, want descriptive error")
	}

	if len(de.Listing) == 0 {
		t.Errorf("discoveryError.Listing is empty, want workspace entries listed")
	}

	// The listing must contain the pre-existing dir name.
	foundExisting := false
	for _, name := range de.Listing {
		if name == existingName {
			foundExisting = true
			break
		}
	}
	if !foundExisting {
		t.Errorf("discoveryError.Listing = %v, does not contain %q", de.Listing, existingName)
	}

	// The listing must also contain the non-Orchestration file. Listing must
	// include ALL workspace entries, not only Orchestration-* dirs -- the
	// testrun.discovery.fail message body is strings.Join(e.Listing, "\n") and
	// a partial listing misleads the operator.
	foundFile := false
	for _, name := range de.Listing {
		if name == "some-file.txt" {
			foundFile = true
			break
		}
	}
	if !foundFile {
		t.Errorf("discoveryError.Listing = %v, does not contain %q (Listing must include all workspace entries, not only Orchestration-* dirs)",
			de.Listing, "some-file.txt")
	}
}

// TestDiscoverNewestOrchestrationDir_NoNewFolder_EmptyWorkspace_ErrorHasEmptyListing
// verifies that when the workspace exists but is empty, the error is a
// *discoveryError with Kind == discoveryNoNewFolder and an empty Listing.
func TestDiscoverNewestOrchestrationDir_NoNewFolder_EmptyWorkspace_ErrorHasEmptyListing(t *testing.T) {
	ws := t.TempDir() // empty workspace

	_, err := discoverNewestOrchestrationDir(ws, nil)
	if err == nil {
		t.Fatal("expected error for empty workspace, got nil")
	}

	var de *discoveryError
	if !errors.As(err, &de) {
		t.Fatalf("error is not *discoveryError: %T: %v", err, err)
	}

	if de.Kind != discoveryNoNewFolder {
		t.Errorf("discoveryError.Kind = %v, want discoveryNoNewFolder", de.Kind)
	}
}

// TestDiscoverNewestOrchestrationDir_ReadDirFailure_ReturnsInfraError verifies
// that when the workspace directory itself cannot be read (e.g., removed after
// the subprocess ran), the returned error is a *discoveryError with Kind ==
// discoveryReadDirFailed. This is distinct from "no new folder" so callers can
// emit the appropriate diagnostic.
func TestDiscoverNewestOrchestrationDir_ReadDirFailure_ReturnsInfraError(t *testing.T) {
	nonExistent := filepath.Join(t.TempDir(), "workspace-was-removed")

	_, err := discoverNewestOrchestrationDir(nonExistent, nil)
	if err == nil {
		t.Fatal("expected error for non-existent workspace, got nil")
	}

	var de *discoveryError
	if !errors.As(err, &de) {
		t.Fatalf("error is not *discoveryError: %T: %v\n(post-invoke ReadDir failure must return a typed *discoveryError)", err, err)
	}

	if de.Kind != discoveryReadDirFailed {
		t.Errorf("discoveryError.Kind = %v, want discoveryReadDirFailed\n(ReadDir failure must be distinct from no-new-folder)", de.Kind)
	}

	// Workspace field must record the path that was scanned.
	if de.Workspace != nonExistent {
		t.Errorf("discoveryError.Workspace = %q, want %q", de.Workspace, nonExistent)
	}

	if de.Cause == nil {
		t.Errorf("discoveryError.Cause is nil, want the underlying ReadDir error")
	}
}

// TestDiscoverNewestOrchestrationDir_LargeWorkspace_ListingTruncatedAt200
// verifies that when the workspace contains more than maxListingEntries (200)
// entries, discoveryError.Listing is capped at 200 entries and Truncated
// carries the overflow count.
func TestDiscoverNewestOrchestrationDir_LargeWorkspace_ListingTruncatedAt200(t *testing.T) {
	ws := t.TempDir()

	// Create 201 files (no Orchestration-* dirs) so that discovery returns a
	// discoveryNoNewFolder error containing all 201 workspace entries in its
	// listing -- which must then be capped at 200 with Truncated == 1.
	for i := 0; i < 201; i++ {
		name := fmt.Sprintf("file-%03d.txt", i)
		if err := os.WriteFile(filepath.Join(ws, name), []byte(""), 0644); err != nil {
			t.Fatal(err)
		}
	}

	_, err := discoverNewestOrchestrationDir(ws, nil)
	if err == nil {
		t.Fatal("expected error for workspace with no Orchestration-* dirs, got nil")
	}

	var de *discoveryError
	if !errors.As(err, &de) {
		t.Fatalf("error is not *discoveryError: %T: %v", err, err)
	}

	if de.Kind != discoveryNoNewFolder {
		t.Errorf("discoveryError.Kind = %v, want discoveryNoNewFolder", de.Kind)
	}

	if len(de.Listing) != 200 {
		t.Errorf("discoveryError.Listing len = %d, want 200 (must be capped at maxListingEntries)",
			len(de.Listing))
	}

	if de.Truncated != 1 {
		t.Errorf("discoveryError.Truncated = %d, want 1 (201 entries - 200 cap = 1 overflow)",
			de.Truncated)
	}
}

// TestDiscoverNewestOrchestrationDir_MultipleNewFolders_NewestSelected verifies
// that when multiple new Orchestration-* directories appear, the one with the
// most recent mtime is returned.
func TestDiscoverNewestOrchestrationDir_MultipleNewFolders_NewestSelected(t *testing.T) {
	ws := t.TempDir()

	// Create three new Orchestration-* dirs with distinct, controlled mtimes.
	older := filepath.Join(ws, "Orchestration-older")
	middle := filepath.Join(ws, "Orchestration-middle")
	newest := filepath.Join(ws, "Orchestration-newest")

	for _, p := range []string{older, middle, newest} {
		if err := os.Mkdir(p, 0755); err != nil {
			t.Fatal(err)
		}
	}

	now := time.Now()
	if err := os.Chtimes(older, now.Add(-2*time.Minute), now.Add(-2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(middle, now.Add(-1*time.Minute), now.Add(-1*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(newest, now, now); err != nil {
		t.Fatal(err)
	}

	got, err := discoverNewestOrchestrationDir(ws, nil)
	if err != nil {
		t.Fatalf("discoverNewestOrchestrationDir returned error: %v", err)
	}

	if got != newest {
		t.Errorf("discoverNewestOrchestrationDir = %q, want %q (must select newest by mtime)", got, newest)
	}
}

// TestDiscoverNewestOrchestrationDir_EqualMtimes_LexicographicTieBreak verifies
// that when two new Orchestration-* directories have identical mtimes, the one
// that appears first in lexicographic (ReadDir) order wins.
func TestDiscoverNewestOrchestrationDir_EqualMtimes_LexicographicTieBreak(t *testing.T) {
	ws := t.TempDir()

	// "aaa" sorts before "zzz" in ReadDir lexicographic order.
	pathAAA := filepath.Join(ws, "Orchestration-aaa")
	pathZZZ := filepath.Join(ws, "Orchestration-zzz")

	for _, p := range []string{pathAAA, pathZZZ} {
		if err := os.Mkdir(p, 0755); err != nil {
			t.Fatal(err)
		}
	}

	// Set both directories to the exact same mtime.
	sameTime := time.Now().Add(-30 * time.Second)
	for _, p := range []string{pathAAA, pathZZZ} {
		if err := os.Chtimes(p, sameTime, sameTime); err != nil {
			t.Fatal(err)
		}
	}

	got, err := discoverNewestOrchestrationDir(ws, nil)
	if err != nil {
		t.Fatalf("discoverNewestOrchestrationDir returned error: %v", err)
	}

	// With equal mtimes the first in ReadDir order (Orchestration-aaa) must win.
	if got != pathAAA {
		t.Errorf("discoverNewestOrchestrationDir = %q, want %q\n(equal-mtime tie-break must select first in lexicographic/ReadDir order)",
			got, pathAAA)
	}
}

// TestDiscoverNewestOrchestrationDir_SingleNewFolder_Returned verifies the
// basic case: exactly one Orchestration-* dir exists and is returned.
func TestDiscoverNewestOrchestrationDir_SingleNewFolder_Returned(t *testing.T) {
	ws := t.TempDir()
	want := newTestOrchestrationDir(t, ws, "Orchestration-only-one")

	got, err := discoverNewestOrchestrationDir(ws, nil)
	if err != nil {
		t.Fatalf("discoverNewestOrchestrationDir returned error: %v", err)
	}
	if got != want {
		t.Errorf("discoverNewestOrchestrationDir = %q, want %q", got, want)
	}
}

// TestDiscoverNewestOrchestrationDir_PreExisting_NilTreatedAsEmpty verifies
// that passing nil as the preExisting set is treated as an empty set (no
// directories are excluded by default).
func TestDiscoverNewestOrchestrationDir_PreExisting_NilTreatedAsEmpty(t *testing.T) {
	ws := t.TempDir()
	want := newTestOrchestrationDir(t, ws, "Orchestration-run-1")

	got, err := discoverNewestOrchestrationDir(ws, nil)
	if err != nil {
		t.Fatalf("discoverNewestOrchestrationDir returned error with nil preExisting: %v", err)
	}
	if got != want {
		t.Errorf("discoverNewestOrchestrationDir = %q, want %q", got, want)
	}
}
