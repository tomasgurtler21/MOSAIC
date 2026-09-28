package runselect_test

import (
	"path/filepath"
	"strings"
	"testing"

	"mosaic-run/internal/domain"
	"mosaic-run/internal/runscan"
	"mosaic-run/internal/runselect"
)

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
