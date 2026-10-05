package seed_test

// Tests for NewPlan refusals: non-existent source, symlink detection,
// cross-source destination collisions, reserved destinations,
// RefusalError contract, and refusal precedence.

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"mosaic-run/internal/domain"
	"mosaic-run/internal/seed"
)

// ---------------------------------------------------------------------------
// NewPlan — refusal: non-existent source
// ---------------------------------------------------------------------------

func TestNewPlan_NonExistentSource_Refused(t *testing.T) {
	nonExistent := filepath.Join(t.TempDir(), "does-not-exist.md")

	plan, err := seed.NewPlan([]string{nonExistent})
	if err == nil {
		t.Fatal("expected an error, got nil")
	}
	assertZeroPlan(t, plan)
	assertRefusalError(t, err, nonExistent)
}

// ---------------------------------------------------------------------------
// NewPlan — refusal: symlink as source path
// ---------------------------------------------------------------------------

func TestNewPlan_SymlinkAsSource_Refused(t *testing.T) {
	if runtime.GOOS == "windows" {
		// Symlink creation requires elevated privileges on Windows; skip rather
		// than weakening the production check to accommodate the test environment.
		t.Skip("symlink creation not available without elevated privileges on Windows")
	}

	dir := t.TempDir()
	realFile := filepath.Join(dir, "real.md")
	writeFile(t, realFile, "content\n")
	linkPath := filepath.Join(dir, "link.md")
	if err := os.Symlink(realFile, linkPath); err != nil {
		t.Fatalf("os.Symlink: %v", err)
	}

	plan, err := seed.NewPlan([]string{linkPath})
	if err == nil {
		t.Fatal("expected an error for symlink source, got nil")
	}
	assertZeroPlan(t, plan)
	assertRefusalError(t, err, linkPath)
}

// ---------------------------------------------------------------------------
// NewPlan — refusal: symlink inside a directory source
// ---------------------------------------------------------------------------

func TestNewPlan_SymlinkInsideDirectory_Refused(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation not available without elevated privileges on Windows")
	}

	dir := t.TempDir()
	srcDir := filepath.Join(dir, "srcdir")
	writeFile(t, filepath.Join(srcDir, "regular.md"), "ok\n")

	// Create a symlink inside srcDir.
	realTarget := filepath.Join(dir, "external.md")
	writeFile(t, realTarget, "external\n")
	linkInside := filepath.Join(srcDir, "linked.md")
	if err := os.Symlink(realTarget, linkInside); err != nil {
		t.Fatalf("os.Symlink: %v", err)
	}

	plan, err := seed.NewPlan([]string{srcDir})
	if err == nil {
		t.Fatal("expected an error for symlink inside directory, got nil")
	}
	assertZeroPlan(t, plan)
	assertRefusalError(t, err, linkInside)
}

// ---------------------------------------------------------------------------
// NewPlan — refusal: cross-source destination collision (names BOTH sources)
// ---------------------------------------------------------------------------

func TestNewPlan_CrossSourceCollision_RefusedNamesBothSources(t *testing.T) {
	dir := t.TempDir()
	// Both files have the same base name → both map to dest "Plan.md".
	src1 := filepath.Join(dir, "dir1", "Plan.md")
	src2 := filepath.Join(dir, "dir2", "Plan.md")
	writeFile(t, src1, "plan from dir1\n")
	writeFile(t, src2, "plan from dir2\n")
	// A Requirement* candidate keeps the naming rule from masking the
	// collision refusal this test targets.
	reqSrc := filepath.Join(dir, "Requirement.md")
	writeFile(t, reqSrc, "req\n")

	plan, err := seed.NewPlan([]string{src1, src2, reqSrc})
	if err == nil {
		t.Fatal("expected an error for cross-source collision, got nil")
	}
	assertZeroPlan(t, plan)
	// Error must name both offending source paths.
	assertRefusalError(t, err, src1, src2)
}

func TestNewPlan_CrossSourceCollision_DirectoryAndFile(t *testing.T) {
	// A directory containing "Report.md" and a separate file source also named
	// "Report.md" → both map to dest "Report.md".
	dir := t.TempDir()
	srcDir := filepath.Join(dir, "srcdir")
	writeFile(t, filepath.Join(srcDir, "Report.md"), "from dir\n")
	srcFile := filepath.Join(dir, "Report.md")
	writeFile(t, srcFile, "from file\n")
	// A Requirement* candidate keeps the naming rule from masking the
	// collision refusal this test targets.
	reqSrc := filepath.Join(dir, "Requirement.md")
	writeFile(t, reqSrc, "req\n")

	plan, err := seed.NewPlan([]string{srcDir, srcFile, reqSrc})
	if err == nil {
		t.Fatal("expected cross-source collision error, got nil")
	}
	assertZeroPlan(t, plan)
	// Both source roots must appear.
	assertRefusalError(t, err, srcDir, srcFile)
}

// ---------------------------------------------------------------------------
// NewPlan — refusal: runner-managed destination
// ---------------------------------------------------------------------------

func TestNewPlan_RunnerManagedDestination_Refused(t *testing.T) {
	dir := t.TempDir()
	// A file named "Orchestration.md" maps to dest "Orchestration.md", which is
	// reserved.
	src := filepath.Join(dir, "Orchestration.md")
	writeFile(t, src, "# fake orchestration\n")
	// A Requirement* candidate keeps the naming rule from masking the
	// reserved-destination refusal this test targets.
	reqSrc := filepath.Join(dir, "Requirement.md")
	writeFile(t, reqSrc, "req\n")

	plan, err := seed.NewPlan([]string{src, reqSrc})
	if err == nil {
		t.Fatal("expected an error for runner-managed destination, got nil")
	}
	assertZeroPlan(t, plan)
	assertRefusalError(t, err, "Orchestration.md")
}

func TestNewPlan_RunnerManagedDestinationInSubdir_NotRefused(t *testing.T) {
	// "Sub/Orchestration.md" is NOT reserved — only the exact root-level path is.
	// A top-level Requirement.md candidate keeps the naming rule satisfied so
	// this test's subject (nested reserved-name tolerance) is reachable.
	dir := t.TempDir()
	srcDir := filepath.Join(dir, "srcdir")
	writeFile(t, filepath.Join(srcDir, "Sub", "Orchestration.md"), "nested\n")
	writeFile(t, filepath.Join(srcDir, "Requirement.md"), "req\n")

	plan, err := seed.NewPlan([]string{srcDir})
	if err != nil {
		t.Fatalf("unexpected error for Sub/Orchestration.md (not reserved): %v", err)
	}
	if plan.IsEmpty() {
		t.Error("expected a non-empty plan, got empty")
	}
}

// ---------------------------------------------------------------------------
// RefusalError contract
// ---------------------------------------------------------------------------

func TestNewPlan_AllRefusals_ReturnRefusalErrorType(t *testing.T) {
	// Non-existent source produces a RefusalError with Component "seed".
	nonExistent := filepath.Join(t.TempDir(), "no-such-file.md")
	_, err := seed.NewPlan([]string{nonExistent})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var re *domain.RefusalError
	if !errors.As(err, &re) {
		t.Fatalf("want *domain.RefusalError, got %T: %v", err, err)
	}
	if re.Component != "seed" {
		t.Errorf("Component = %q, want \"seed\"", re.Component)
	}
}

func TestNewPlan_AllRefusals_ReturnZeroPlan(t *testing.T) {
	// On any refusal the returned Plan must be zero (IsEmpty() == true).
	nonExistent := filepath.Join(t.TempDir(), "no-such.md")
	plan, err := seed.NewPlan([]string{nonExistent})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	assertZeroPlan(t, plan)
}

// ---------------------------------------------------------------------------
// ReservedDestinations
// ---------------------------------------------------------------------------

func TestReservedDestinations_ContainsOrchestrationMd(t *testing.T) {
	reserved := seed.ReservedDestinations()
	found := false
	for _, r := range reserved {
		if r == "Orchestration.md" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("ReservedDestinations() = %v; want slice containing \"Orchestration.md\"", reserved)
	}
}

func TestReservedDestinations_MutatingReturnedSliceDoesNotAffectSubsequentCalls(t *testing.T) {
	r1 := seed.ReservedDestinations()
	// Clobber the first element.
	if len(r1) > 0 {
		r1[0] = "tampered"
	}
	r2 := seed.ReservedDestinations()
	for _, r := range r2 {
		if r == "tampered" {
			t.Error("mutating returned slice affected subsequent ReservedDestinations() call")
		}
	}
}

// ---------------------------------------------------------------------------
// Refusal precedence
// ---------------------------------------------------------------------------

// TestNewPlan_RefusalPrecedence_ZeroMatch_BeforeCollision pins that a seed
// set with no Requirement* candidate that would also collide on destination
// produces the zero-match refusal, not the collision refusal.
func TestNewPlan_RefusalPrecedence_ZeroMatch_BeforeCollision(t *testing.T) {
	dir := t.TempDir()
	src1 := filepath.Join(dir, "dir1", "Plan.md")
	src2 := filepath.Join(dir, "dir2", "Plan.md")
	writeFile(t, src1, "plan from dir1\n")
	writeFile(t, src2, "plan from dir2\n")

	plan, err := seed.NewPlan([]string{src1, src2})
	if err == nil {
		t.Fatal("expected the zero-match refusal, got nil error")
	}
	assertZeroPlan(t, plan)

	var re *domain.RefusalError
	if !errors.As(err, &re) {
		t.Fatalf("want *domain.RefusalError, got %T: %v", err, err)
	}
	msg := re.Error()
	if strings.Contains(msg, "collision") {
		t.Errorf("want the zero-match refusal to take precedence, but got a collision-shaped message: %q", msg)
	}
}
