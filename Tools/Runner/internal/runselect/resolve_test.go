package runselect_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mosaic-run/internal/domain"
	"mosaic-run/internal/runscan"
	"mosaic-run/internal/runselect"
)

// ---------------------------------------------------------------------------
// T2.1: Resolve — the decision never infers a selection from a count
// ---------------------------------------------------------------------------

// TestResolve_NoFlags_ZeroCandidates_ReturnsQuestion is the zero-candidate arm
// of AC2.6/AC2.3: with neither flag and no runs at all, Resolve must still
// return a Question (offering "start a new run"), not silently mint one.
func TestResolve_NoFlags_ZeroCandidates_ReturnsQuestion(t *testing.T) {
	mint, calls := countingMinter()
	dec, err := runselect.Resolve(runselect.Request{
		Scan:    runscan.ScanResult{},
		WorkDir: testWorkDir,
	}, mint)
	if err != nil {
		t.Fatalf("Resolve(zero candidates) error = %v, want nil", err)
	}
	if dec.Resolved != nil {
		t.Errorf("Decision.Resolved = %+v, want nil (zero candidates must not auto-resolve)", dec.Resolved)
	}
	if dec.Question == nil {
		t.Fatal("Decision.Question = nil, want non-nil for zero candidates with no explicit flags")
	}
	if *calls != 0 {
		t.Errorf("mint was called %d time(s); Resolve must not mint when returning a Question", *calls)
	}
}

// TestResolve_NoFlags_OneCandidate_ReturnsQuestion is the direct expression of
// the defect this stage removes: exactly one resumable run must NOT be
// silently auto-resolved.
func TestResolve_NoFlags_OneCandidate_ReturnsQuestion(t *testing.T) {
	mint, _ := countingMinter()
	dec, err := runselect.Resolve(runselect.Request{
		Scan: runscan.ScanResult{
			Candidates: []runscan.RunCandidate{
				candidate("20260701T120000Z-a3f9", "/work/Orchestration-20260701T120000Z-a3f9"),
			},
		},
		WorkDir: testWorkDir,
	}, mint)
	if err != nil {
		t.Fatalf("Resolve(one candidate) error = %v, want nil", err)
	}
	if dec.Resolved != nil {
		t.Errorf("Decision.Resolved = %+v, want nil; a single candidate must not be silently resumed", dec.Resolved)
	}
	if dec.Question == nil {
		t.Fatal("Decision.Question = nil, want non-nil for exactly one candidate with no explicit flags")
	}
}

// TestResolve_NoFlags_ManyCandidates_ReturnsQuestion verifies the many-candidate
// arm also yields a Question rather than a resolved identity.
func TestResolve_NoFlags_ManyCandidates_ReturnsQuestion(t *testing.T) {
	mint, _ := countingMinter()
	dec, err := runselect.Resolve(runselect.Request{
		Scan: runscan.ScanResult{
			Candidates: []runscan.RunCandidate{
				candidate("20260701T120000Z-a3f9", "/work/Orchestration-20260701T120000Z-a3f9"),
				candidate("20260702T120000Z-b4e8", "/work/Orchestration-20260702T120000Z-b4e8"),
			},
		},
		WorkDir: testWorkDir,
	}, mint)
	if err != nil {
		t.Fatalf("Resolve(many candidates) error = %v, want nil", err)
	}
	if dec.Resolved != nil {
		t.Errorf("Decision.Resolved = %+v, want nil for many candidates", dec.Resolved)
	}
	if dec.Question == nil {
		t.Fatal("Decision.Question = nil, want non-nil for many candidates")
	}
}

// TestResolve_Question_NewRunChoiceAlwaysFirst verifies that, for zero, one,
// and many candidates alike, the returned Question always offers starting a
// new run as its first choice (AC2.3).
func TestResolve_Question_NewRunChoiceAlwaysFirst(t *testing.T) {
	cases := []struct {
		name   string
		result runscan.ScanResult
	}{
		{"zero candidates", runscan.ScanResult{}},
		{"one candidate", runscan.ScanResult{Candidates: []runscan.RunCandidate{
			candidate("20260701T120000Z-a3f9", "/work/Orchestration-20260701T120000Z-a3f9"),
		}}},
		{"many candidates", runscan.ScanResult{Candidates: []runscan.RunCandidate{
			candidate("20260701T120000Z-a3f9", "/work/Orchestration-20260701T120000Z-a3f9"),
			candidate("20260702T120000Z-b4e8", "/work/Orchestration-20260702T120000Z-b4e8"),
		}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mint, _ := countingMinter()
			dec, err := runselect.Resolve(runselect.Request{Scan: tc.result, WorkDir: testWorkDir}, mint)
			if err != nil {
				t.Fatalf("Resolve error = %v, want nil", err)
			}
			if dec.Question == nil {
				t.Fatal("Decision.Question = nil, want non-nil")
			}
			if len(dec.Question.Choices) == 0 {
				t.Fatal("Question.Choices is empty; a new-run choice must always be present")
			}
			first := dec.Question.Choices[0]
			if first.ID != runselect.NewRunChoiceID {
				t.Errorf("Choices[0].ID = %q, want %q (new-run choice must be first)", first.ID, runselect.NewRunChoiceID)
			}
			if first.Kind != runselect.ChoiceNewRun {
				t.Errorf("Choices[0].Kind = %v, want ChoiceNewRun", first.Kind)
			}
			if !first.Selectable {
				t.Error("new-run choice must be Selectable")
			}
		})
	}
}

// TestResolve_Question_EveryResumableCandidateIsSelectable verifies AC2.4:
// every resumable run appears as a selectable choice, not only the most
// recent.
func TestResolve_Question_EveryResumableCandidateIsSelectable(t *testing.T) {
	mint, _ := countingMinter()
	dec, err := runselect.Resolve(runselect.Request{
		Scan: runscan.ScanResult{
			Candidates: []runscan.RunCandidate{
				candidate("20260701T120000Z-a3f9", "/work/Orchestration-20260701T120000Z-a3f9"),
				candidate("20260702T120000Z-b4e8", "/work/Orchestration-20260702T120000Z-b4e8"),
				candidate("20260703T120000Z-c5d7", "/work/Orchestration-20260703T120000Z-c5d7"),
			},
		},
		WorkDir: testWorkDir,
	}, mint)
	if err != nil {
		t.Fatalf("Resolve error = %v, want nil", err)
	}
	if dec.Question == nil {
		t.Fatal("Decision.Question = nil, want non-nil")
	}

	want := map[string]bool{
		"20260701T120000Z-a3f9": false,
		"20260702T120000Z-b4e8": false,
		"20260703T120000Z-c5d7": false,
	}
	for _, c := range dec.Question.Choices {
		if c.Kind != runselect.ChoiceResume {
			continue
		}
		if _, ok := want[c.ID]; ok {
			want[c.ID] = true
			if !c.Selectable {
				t.Errorf("choice %q must be Selectable (every resumable run is selectable)", c.ID)
			}
		}
	}
	for id, found := range want {
		if !found {
			t.Errorf("resumable candidate %q is missing from Question.Choices", id)
		}
	}
}

// TestResolve_Question_UnresumableRunsShownButNotSelectable verifies AC2.5:
// unresumable runs are offered with their reason, and are not selectable as a
// resume target.
func TestResolve_Question_UnresumableRunsShownButNotSelectable(t *testing.T) {
	mint, _ := countingMinter()
	dec, err := runselect.Resolve(runselect.Request{
		Scan: runscan.ScanResult{
			Candidates: []runscan.RunCandidate{
				candidate("20260701T120000Z-a3f9", "/work/Orchestration-20260701T120000Z-a3f9"),
			},
			Unresumable: []runscan.UnresumableRun{
				unresumable("20260601T090000Z-1234", "/work/Orchestration-20260601T090000Z-1234"),
			},
		},
		WorkDir: testWorkDir,
	}, mint)
	if err != nil {
		t.Fatalf("Resolve error = %v, want nil", err)
	}
	if dec.Question == nil {
		t.Fatal("Decision.Question = nil, want non-nil")
	}

	var found *runselect.Choice
	for i := range dec.Question.Choices {
		if dec.Question.Choices[i].ID == "20260601T090000Z-1234" {
			found = &dec.Question.Choices[i]
		}
	}
	if found == nil {
		t.Fatal("unresumable run is missing from Question.Choices; it must be shown, not hidden")
	}
	if found.Kind != runselect.ChoiceUnresumable {
		t.Errorf("Kind = %v, want ChoiceUnresumable", found.Kind)
	}
	if found.Selectable {
		t.Error("unresumable choice must not be Selectable")
	}
	if found.Reason != runscan.ReasonCompleted {
		t.Errorf("Reason = %q, want %q", found.Reason, runscan.ReasonCompleted)
	}
}

// ---------------------------------------------------------------------------
// T2.1 / T2.2: Resolve — explicit flags settle the decision without asking
// ---------------------------------------------------------------------------

// TestResolve_RunIDFlag_WellFormedResumable_ReturnsResolvedIdentity verifies
// rule 3: a well-formed --run naming a resumable candidate resolves directly.
func TestResolve_RunIDFlag_WellFormedResumable_ReturnsResolvedIdentity(t *testing.T) {
	const runID = "20260701T120000Z-a3f9"
	mint, calls := countingMinter()
	dec, err := runselect.Resolve(runselect.Request{
		Scan: runscan.ScanResult{
			Candidates: []runscan.RunCandidate{
				candidate(runID, filepath.Join(testWorkDir, domain.RunScopedFolder(runID))),
			},
		},
		WorkDir:   testWorkDir,
		RunIDFlag: runID,
	}, mint)
	if err != nil {
		t.Fatalf("Resolve(--run) error = %v, want nil", err)
	}
	if dec.Question != nil {
		t.Errorf("Decision.Question = %+v, want nil when --run is explicit", dec.Question)
	}
	if dec.Resolved == nil {
		t.Fatal("Decision.Resolved = nil, want non-nil when --run is explicit")
	}
	if dec.Resolved.RunID != runID {
		t.Errorf("Resolved.RunID = %q, want %q", dec.Resolved.RunID, runID)
	}
	if dec.Resolved.IsNewRun {
		t.Error("Resolved.IsNewRun = true, want false for --run")
	}
	if *calls != 0 {
		t.Errorf("mint was called %d time(s); --run must not mint", *calls)
	}
}

// TestResolve_RunIDFlag_TargetingUnresumableRun_ReturnsUsageError verifies
// that --run naming a run the scan reported unresumable is refused, naming
// the reason, per the Resolve doc comment (rule 3).
func TestResolve_RunIDFlag_TargetingUnresumableRun_ReturnsUsageError(t *testing.T) {
	const runID = "20260601T090000Z-1234"
	mint, _ := countingMinter()
	_, err := runselect.Resolve(runselect.Request{
		Scan: runscan.ScanResult{
			Unresumable: []runscan.UnresumableRun{
				unresumable(runID, filepath.Join(testWorkDir, domain.RunScopedFolder(runID))),
			},
		},
		WorkDir:   testWorkDir,
		RunIDFlag: runID,
	}, mint)
	if err == nil {
		t.Fatal("Resolve(--run targeting unresumable run) returned nil error, want a usage error")
	}
	if !errors.Is(err, runselect.ErrUsage) {
		t.Errorf("error %v does not wrap runselect.ErrUsage", err)
	}
	if !strings.Contains(err.Error(), string(runscan.ReasonCompleted)) {
		t.Errorf("error %q does not name the unresumable reason %q", err.Error(), runscan.ReasonCompleted)
	}
}

// TestResolve_RunIDFlag_Malformed_ReturnsUsageError verifies rule 2: a
// malformed run_id is a usage error.
func TestResolve_RunIDFlag_Malformed_ReturnsUsageError(t *testing.T) {
	mint, _ := countingMinter()
	_, err := runselect.Resolve(runselect.Request{
		WorkDir:   testWorkDir,
		RunIDFlag: "not-a-valid-run-id",
	}, mint)
	if err == nil {
		t.Fatal("Resolve(malformed --run) returned nil error, want a usage error")
	}
	if !errors.Is(err, runselect.ErrUsage) {
		t.Errorf("error %v does not wrap runselect.ErrUsage", err)
	}
}

// TestResolve_NewRunFlag_ReturnsResolvedIdentityFromMint verifies rule 4:
// --new-run resolves directly via the injected Minter, called exactly once.
func TestResolve_NewRunFlag_ReturnsResolvedIdentityFromMint(t *testing.T) {
	mint, calls := countingMinter()
	dec, err := runselect.Resolve(runselect.Request{
		WorkDir: testWorkDir,
		NewRun:  true,
	}, mint)
	if err != nil {
		t.Fatalf("Resolve(--new-run) error = %v, want nil", err)
	}
	if dec.Question != nil {
		t.Errorf("Decision.Question = %+v, want nil when --new-run is explicit", dec.Question)
	}
	if dec.Resolved == nil {
		t.Fatal("Decision.Resolved = nil, want non-nil when --new-run is explicit")
	}
	if !dec.Resolved.IsNewRun {
		t.Error("Resolved.IsNewRun = false, want true for --new-run")
	}
	if dec.Resolved.RunID == "" {
		t.Error("Resolved.RunID is empty; want the minted run_id")
	}
	if *calls != 1 {
		t.Errorf("mint was called %d time(s), want exactly 1", *calls)
	}
}

// TestResolve_BothFlagsSet_ReturnsUsageError verifies rule 1: --run and
// --new-run together are always a usage error, regardless of workspace state.
func TestResolve_BothFlagsSet_ReturnsUsageError(t *testing.T) {
	mint, calls := countingMinter()
	_, err := runselect.Resolve(runselect.Request{
		WorkDir:   testWorkDir,
		RunIDFlag: "20260701T120000Z-a3f9",
		NewRun:    true,
	}, mint)
	if err == nil {
		t.Fatal("Resolve(--run + --new-run) returned nil error, want a usage error")
	}
	if !errors.Is(err, runselect.ErrUsage) {
		t.Errorf("error %v does not wrap runselect.ErrUsage", err)
	}
	if *calls != 0 {
		t.Errorf("mint was called %d time(s); a usage error must not mint", *calls)
	}
}

// ---------------------------------------------------------------------------
// T2.5: starting a new run leaves every existing run untouched
// ---------------------------------------------------------------------------

// TestResolve_NewRun_LeavesExistingRunFoldersUntouched verifies AC2.8 at the
// unit level: resolving a new-run decision performs no filesystem I/O beyond
// what the injected Minter does, so pre-existing run folders and their
// artifacts are byte-for-byte unmodified.
func TestResolve_NewRun_LeavesExistingRunFoldersUntouched(t *testing.T) {
	workDir := t.TempDir()
	const existingID = "20260701T120000Z-a3f9"
	existingFolder := filepath.Join(workDir, domain.RunScopedFolder(existingID))
	if err := os.MkdirAll(existingFolder, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	artifactPath := filepath.Join(existingFolder, "Orchestration.md")
	const artifactContent = "---\nphase: EXECUTION\n---\nunchanged content\n"
	if err := os.WriteFile(artifactPath, []byte(artifactContent), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	before, err := os.ReadFile(artifactPath)
	if err != nil {
		t.Fatalf("ReadFile (before): %v", err)
	}
	infoBefore, err := os.Stat(artifactPath)
	if err != nil {
		t.Fatalf("Stat (before): %v", err)
	}

	mint, _ := countingMinter()
	_, err = runselect.Resolve(runselect.Request{
		Scan: runscan.ScanResult{
			Candidates: []runscan.RunCandidate{candidate(existingID, existingFolder)},
		},
		WorkDir: workDir,
		NewRun:  true,
	}, mint)
	if err != nil {
		t.Fatalf("Resolve(--new-run) error = %v, want nil", err)
	}

	after, err := os.ReadFile(artifactPath)
	if err != nil {
		t.Fatalf("ReadFile (after): %v", err)
	}
	if string(before) != string(after) {
		t.Errorf("existing artifact content changed:\nbefore: %q\nafter:  %q", before, after)
	}
	infoAfter, err := os.Stat(artifactPath)
	if err != nil {
		t.Fatalf("Stat (after): %v", err)
	}
	if !infoBefore.ModTime().Equal(infoAfter.ModTime()) {
		t.Errorf("existing artifact mtime changed: before=%v after=%v", infoBefore.ModTime(), infoAfter.ModTime())
	}
}
