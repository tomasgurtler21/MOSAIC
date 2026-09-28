package domain_test

// Tests for the commit branch variant rules of ReconcileResumeSettings.

import (
	"errors"
	"strings"
	"testing"

	"mosaic-run/internal/domain"
)

func recordedRunnerSettings() domain.RunSettings {
	return domain.RunSettings{Mode: domain.ExecutionModeAuto}
}

func TestReconcileResumeSettings_CommitsOff_VariantIsEmptyAndSuppliedIgnored(t *testing.T) {
	recorded := recordedRunnerSettings()
	supplied := domain.RunSettings{CommitBranchVariant: domain.CommitBranchUserOwn}

	got, _, err := domain.ReconcileResumeSettings(recorded, supplied, domain.SuppliedSettings{CommitBranchVariant: true})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.CommitBranchVariant != "" {
		t.Errorf("CommitBranchVariant = %q, want empty when commits are off", got.CommitBranchVariant)
	}
}

func TestReconcileResumeSettings_BranchRecorded_RecordedVariantIsEffective(t *testing.T) {
	recorded := recordedRunnerSettings()
	recorded.Commits = true
	recorded.CommitBranch = "mosaic/run/x"
	recorded.CommitBranchVariant = domain.CommitBranchMOSAICOwned

	got, _, err := domain.ReconcileResumeSettings(recorded, domain.RunSettings{}, domain.SuppliedSettings{})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.CommitBranchVariant != domain.CommitBranchMOSAICOwned {
		t.Errorf("CommitBranchVariant = %q, want %q", got.CommitBranchVariant, domain.CommitBranchMOSAICOwned)
	}
}

func TestReconcileResumeSettings_BranchRecorded_SameSuppliedVariantAccepted(t *testing.T) {
	recorded := recordedRunnerSettings()
	recorded.Commits = true
	recorded.CommitBranch = "feature/mine"
	recorded.CommitBranchVariant = domain.CommitBranchUserOwn
	supplied := domain.RunSettings{CommitBranchVariant: domain.CommitBranchUserOwn}

	got, _, err := domain.ReconcileResumeSettings(recorded, supplied, domain.SuppliedSettings{CommitBranchVariant: true})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.CommitBranchVariant != domain.CommitBranchUserOwn {
		t.Errorf("CommitBranchVariant = %q, want %q", got.CommitBranchVariant, domain.CommitBranchUserOwn)
	}
}

func TestReconcileResumeSettings_BranchRecorded_DifferingSuppliedVariantIsConflict(t *testing.T) {
	recorded := recordedRunnerSettings()
	recorded.Commits = true
	recorded.CommitBranch = "mosaic/run/x"
	recorded.CommitBranchVariant = domain.CommitBranchMOSAICOwned
	supplied := domain.RunSettings{CommitBranchVariant: domain.CommitBranchUserOwn}

	_, _, err := domain.ReconcileResumeSettings(recorded, supplied, domain.SuppliedSettings{CommitBranchVariant: true})

	var refusal *domain.RefusalError
	if !errors.As(err, &refusal) {
		t.Fatalf("want *RefusalError, got %T (%v)", err, err)
	}
}

func TestReconcileResumeSettings_SetupPending_SuppliedVariantIsEffective(t *testing.T) {
	recorded := recordedRunnerSettings()
	recorded.Commits = true
	supplied := domain.RunSettings{CommitBranchVariant: domain.CommitBranchUserOwn}

	got, _, err := domain.ReconcileResumeSettings(recorded, supplied, domain.SuppliedSettings{CommitBranchVariant: true})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.CommitBranchVariant != domain.CommitBranchUserOwn {
		t.Errorf("CommitBranchVariant = %q, want %q", got.CommitBranchVariant, domain.CommitBranchUserOwn)
	}
}

func TestReconcileResumeSettings_SetupPending_MissingVariantIsRefused(t *testing.T) {
	recorded := recordedRunnerSettings()
	recorded.Commits = true
	tests := []struct {
		name     string
		supplied domain.RunSettings
		which    domain.SuppliedSettings
	}{
		{"not supplied", domain.RunSettings{}, domain.SuppliedSettings{}},
		{"flagged but empty", domain.RunSettings{}, domain.SuppliedSettings{CommitBranchVariant: true}},
		{"value without supplied flag", domain.RunSettings{CommitBranchVariant: domain.CommitBranchMOSAICOwned}, domain.SuppliedSettings{}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := domain.ReconcileResumeSettings(recorded, tc.supplied, tc.which)

			var refusal *domain.RefusalError
			if !errors.As(err, &refusal) {
				t.Fatalf("want *RefusalError, got %T (%v)", err, err)
			}
			if !strings.Contains(err.Error(), "commit") {
				t.Errorf("refusal should name the commit branch variant, got %q", err.Error())
			}
		})
	}
}
