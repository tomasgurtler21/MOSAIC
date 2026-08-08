package runselect_test

// runselect_test.go drives the TDD RED phase for the runselect package: the
// single selection decision shared by the CLI, the CLI entry point, and the
// TUI entry point.
//
// These tests compile against the placeholder declarations in runselect.go
// and fail because those placeholders return zero values. They become GREEN
// when Resolve, Answer, and Announce are implemented per the contract in
// Development/Designs (ContractsDesign.md, "runselect.Resolve").

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mosaic-run/internal/domain"
	"mosaic-run/internal/runscan"
	"mosaic-run/internal/runselect"
)

// ---------------------------------------------------------------------------
// Test fixtures
// ---------------------------------------------------------------------------

func candidate(runID, folder string) runscan.RunCandidate {
	return runscan.RunCandidate{
		RunInfo: runscan.RunInfo{
			RunID:       runID,
			FolderPath:  folder,
			LastUpdated: time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC),
			Workflow:    "test-workflow",
			Task:        "test task",
			Phase:       "EXECUTION",
			Stage:       "Stage-1",
			LastAgent:   "impl#1",
		},
	}
}

func unresumable(runID, folder string) runscan.UnresumableRun {
	return runscan.UnresumableRun{
		RunInfo: runscan.RunInfo{
			RunID:       runID,
			FolderPath:  folder,
			LastUpdated: time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC),
			Phase:       "COMPLETED",
		},
		Reason: runscan.ReasonCompleted,
	}
}

// countingMinter returns a Minter that records how many times it was called
// and returns distinct, deterministic identities on each call.
func countingMinter() (m runselect.Minter, calls *int) {
	n := 0
	calls = &n
	m = func() (string, string) {
		n++
		id := "20260801T000000Z-000" + string(rune('0'+n))
		return id, "/work/" + domain.RunScopedFolder(id)
	}
	return m, calls
}

const testWorkDir = "/work"

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
// T2.2: Answer — converting a chosen Choice into a resolved Identity
// ---------------------------------------------------------------------------

// TestAnswer_NewRunChoiceID_MintsIdentity verifies that answering with
// NewRunChoiceID mints a fresh identity.
func TestAnswer_NewRunChoiceID_MintsIdentity(t *testing.T) {
	mint, calls := countingMinter()
	q := runselect.Question{Choices: []runselect.Choice{
		{ID: runselect.NewRunChoiceID, Kind: runselect.ChoiceNewRun, Selectable: true},
	}}
	id, err := runselect.Answer(q, runselect.NewRunChoiceID, mint)
	if err != nil {
		t.Fatalf("Answer(new-run) error = %v, want nil", err)
	}
	if !id.IsNewRun {
		t.Error("Identity.IsNewRun = false, want true for the new-run choice")
	}
	if id.RunID == "" {
		t.Error("Identity.RunID is empty; want the minted run_id")
	}
	if *calls != 1 {
		t.Errorf("mint was called %d time(s), want exactly 1", *calls)
	}
}

// TestAnswer_SelectableChoiceID_ResolvesToItsIdentity verifies that choosing
// a selectable resume choice resolves to that run's identity, not a minted one.
func TestAnswer_SelectableChoiceID_ResolvesToItsIdentity(t *testing.T) {
	const runID = "20260701T120000Z-a3f9"
	folder := filepath.Join(testWorkDir, domain.RunScopedFolder(runID))
	mint, calls := countingMinter()
	q := runselect.Question{Choices: []runselect.Choice{
		{ID: runselect.NewRunChoiceID, Kind: runselect.ChoiceNewRun, Selectable: true},
		{ID: runID, Kind: runselect.ChoiceResume, Selectable: true, Run: runscan.RunInfo{
			RunID: runID, FolderPath: folder,
		}},
	}}
	id, err := runselect.Answer(q, runID, mint)
	if err != nil {
		t.Fatalf("Answer(%q) error = %v, want nil", runID, err)
	}
	if id.IsNewRun {
		t.Error("Identity.IsNewRun = true, want false for a resume choice")
	}
	if id.RunID != runID {
		t.Errorf("Identity.RunID = %q, want %q", id.RunID, runID)
	}
	if id.RunFolder != folder {
		t.Errorf("Identity.RunFolder = %q, want %q", id.RunFolder, folder)
	}
	if *calls != 0 {
		t.Errorf("mint was called %d time(s); resolving an existing choice must not mint", *calls)
	}
}

// TestAnswer_NonSelectableChoiceID_ReturnsError verifies that answering with
// the ID of a non-selectable (unresumable) choice is rejected.
func TestAnswer_NonSelectableChoiceID_ReturnsError(t *testing.T) {
	const runID = "20260601T090000Z-1234"
	mint, _ := countingMinter()
	q := runselect.Question{Choices: []runselect.Choice{
		{ID: runselect.NewRunChoiceID, Kind: runselect.ChoiceNewRun, Selectable: true},
		{ID: runID, Kind: runselect.ChoiceUnresumable, Selectable: false, Reason: runscan.ReasonCompleted},
	}}
	_, err := runselect.Answer(q, runID, mint)
	if err == nil {
		t.Fatal("Answer(non-selectable choice) returned nil error, want a non-nil error")
	}
}

// TestAnswer_UnknownChoiceID_ReturnsError verifies that an ID naming no
// Choice at all is rejected.
func TestAnswer_UnknownChoiceID_ReturnsError(t *testing.T) {
	mint, _ := countingMinter()
	q := runselect.Question{Choices: []runselect.Choice{
		{ID: runselect.NewRunChoiceID, Kind: runselect.ChoiceNewRun, Selectable: true},
	}}
	_, err := runselect.Answer(q, "not-a-known-choice-id", mint)
	if err == nil {
		t.Fatal("Answer(unknown choice id) returned nil error, want a non-nil error")
	}
}

// ---------------------------------------------------------------------------
// T2.3: Announce — the chosen-run statement
// ---------------------------------------------------------------------------

// TestAnnounce_NewRun_ContainsRunIDAndStatesNew verifies the new-run
// announcement contract: the run_id is present, and the text states the run
// is new.
func TestAnnounce_NewRun_ContainsRunIDAndStatesNew(t *testing.T) {
	id := runselect.Identity{RunID: "20260801T000000Z-0001", IsNewRun: true}
	text := runselect.Announce(id)
	if !strings.Contains(text, id.RunID) {
		t.Errorf("Announce text %q does not contain the run_id %q", text, id.RunID)
	}
	if !strings.Contains(strings.ToLower(text), "new") {
		t.Errorf("Announce text %q does not state that the run is new", text)
	}
}

// TestAnnounce_ResumedRun_ContainsRunIDAndPosition verifies the resumed-run
// announcement contract: the run_id is present, the text states resumption,
// and the recorded phase, stage, and last agent are all present.
func TestAnnounce_ResumedRun_ContainsRunIDAndPosition(t *testing.T) {
	id := runselect.Identity{
		RunID:    "20260701T120000Z-a3f9",
		IsNewRun: false,
		Position: &runselect.Position{
			Phase:     "EXECUTION",
			Stage:     "Stage-1",
			LastAgent: "implementation-tdd#2",
		},
	}
	text := runselect.Announce(id)
	if !strings.Contains(text, id.RunID) {
		t.Errorf("Announce text %q does not contain the run_id %q", text, id.RunID)
	}
	if !strings.Contains(strings.ToLower(text), "resum") {
		t.Errorf("Announce text %q does not state that the run is resumed", text)
	}
	if !strings.Contains(text, "EXECUTION") {
		t.Errorf("Announce text %q does not contain the recorded phase %q", text, "EXECUTION")
	}
	if !strings.Contains(text, "Stage-1") {
		t.Errorf("Announce text %q does not contain the recorded stage %q", text, "Stage-1")
	}
	if !strings.Contains(text, "implementation-tdd#2") {
		t.Errorf("Announce text %q does not contain the last agent %q", text, "implementation-tdd#2")
	}
}

// TestAnnounce_ResumedRun_NilPosition_DoesNotPanic verifies that a resumed
// run whose artifact could not be parsed (nil Position) still produces a
// non-empty announcement rather than panicking.
func TestAnnounce_ResumedRun_NilPosition_DoesNotPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("Announce panicked with nil Position: %v", r)
		}
	}()
	id := runselect.Identity{RunID: "20260701T120000Z-a3f9", IsNewRun: false, Position: nil}
	text := runselect.Announce(id)
	if !strings.Contains(text, id.RunID) {
		t.Errorf("Announce text %q does not contain the run_id %q", text, id.RunID)
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
