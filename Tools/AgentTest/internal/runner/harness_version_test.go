package runner_test

// Tests for the harness version being carried from the harness adapter's
// optional version capability into run evidence.

import (
	"context"
	"testing"

	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/runner"
)

// versionedAdapter is a stub adapter that also reports a harness version.
type versionedAdapter struct {
	*stubAdapter
	version string
}

func (a *versionedAdapter) HarnessVersion(ctx context.Context) string { return a.version }

var _ domain.HarnessVersionReporter = (*versionedAdapter)(nil)

func TestRun_HarnessVersionFromReporterAdapterCarriedIntoEvidence(t *testing.T) {
	// Arrange
	h := newHarness(t)
	h.Deps.Adapter = &versionedAdapter{stubAdapter: h.Adapter, version: "2.1.284"}
	req := newRequest("harness-version-carried")

	// Act
	result, err := runner.Run(context.Background(), h.Deps, req, nil)

	// Assert
	if err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}
	if result.HarnessVersion != "2.1.284" {
		t.Errorf("RunEvidence.HarnessVersion = %q, want %q", result.HarnessVersion, "2.1.284")
	}
}

func TestRun_AdapterWithoutVersionCapabilityYieldsEmptyHarnessVersion(t *testing.T) {
	// Arrange
	h := newHarness(t)
	req := newRequest("harness-version-absent")

	// Act
	result, err := runner.Run(context.Background(), h.Deps, req, nil)

	// Assert
	if err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}
	if result.HarnessVersion != "" {
		t.Errorf("RunEvidence.HarnessVersion = %q, want empty for an adapter without the capability", result.HarnessVersion)
	}
}

func TestRun_ReporterReturningEmptyVersionDoesNotFailRun(t *testing.T) {
	// Arrange
	h := newHarness(t)
	h.Deps.Adapter = &versionedAdapter{stubAdapter: h.Adapter, version: ""}
	req := newRequest("harness-version-unknown")

	// Act
	result, err := runner.Run(context.Background(), h.Deps, req, nil)

	// Assert
	if err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}
	if result.HarnessVersion != "" {
		t.Errorf("RunEvidence.HarnessVersion = %q, want empty", result.HarnessVersion)
	}
}

func TestBuildEvidence_MapsSnapshotHarnessVersion(t *testing.T) {
	// Arrange
	req := newRequest("build-evidence-harness-version")
	snap := runner.Snapshot{
		HarnessID:      "stub-harness",
		HarnessVersion: "2.1.284",
		SubjectResult:  domain.SubjectResult{Disposition: domain.DispositionCompleted},
	}

	// Act
	evidence := runner.BuildEvidence(req, snap, domain.CostReport{}, 0)

	// Assert
	if evidence.HarnessVersion != "2.1.284" {
		t.Errorf("RunEvidence.HarnessVersion = %q, want %q", evidence.HarnessVersion, "2.1.284")
	}
}
