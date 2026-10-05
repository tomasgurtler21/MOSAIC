package tui

// nav_minter_test.go verifies the run-select screen's identity-minting path
// (Stage 2 / T2.1): calling RunIdentityMinter on "new run", passing the
// minted values to the session factory, and invoking the OnRunIDResolved
// callback with the resolved run_id for both the new-run and resume paths.
//
// Tests are in package tui (internal) because rootModel fields (selections,
// screen) are unexported.

import (
	"context"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	tuicommon "mosaic-common/tui"
	"mosaic-run/internal/domain"
	"mosaic-run/internal/runscan"
)

// ---------------------------------------------------------------------------
// Run selection screen: new-run identity minting (Stage 2 / T2.1)
// ---------------------------------------------------------------------------

// TestRunSelect_NewRun_WithMinter_PopulatesCanonicalRunID verifies that when
// MintRunIdentity is provided and the user picks "new run" on the run-select screen,
// the minted run ID is written to selections.runID in canonical format.
//
// In RED: updateRunSelect does not call the minter, so selections.runID stays "".
// In GREEN: the minter is called and selections.runID == fixedMintedRunID.
func TestRunSelect_NewRun_WithMinter_PopulatesCanonicalRunID(t *testing.T) {
	candidates := []runscan.RunCandidate{
		newTestCandidate("20260701T120000Z-a3f9", "/ws/Orchestration-20260701T120000Z-a3f9"),
		newTestCandidate("20260702T120000Z-b4e8", "/ws/Orchestration-20260702T120000Z-b4e8"),
	}
	calls := make([]factoryCall, 0)
	capSess := newCapturingSession()
	m := newModelWithScanMinterFactory(candidates, fixedMinter(), &calls, capSess)
	if m.screen != screenRunSelect {
		t.Fatalf("precondition: screen = %v, want screenRunSelect", m.screen)
	}

	// "Start a new run" is the first item on the run-select list. Enter selects it.
	sendKey(m, tea.KeyEnter)

	if m.selections.runID == "" {
		t.Error("selections.runID is empty after 'new run' with minter; want minted run ID")
	}
	if m.selections.runID != fixedMintedRunID {
		t.Errorf("selections.runID = %q, want minted value %q", m.selections.runID, fixedMintedRunID)
	}
	if !domain.IsValidRunID(m.selections.runID) {
		t.Errorf("selections.runID %q does not satisfy domain.IsValidRunID", m.selections.runID)
	}
}

// TestRunSelect_NewRun_WithMinter_PopulatesMintedRunFolder verifies that when
// MintRunIdentity is provided and the user picks "new run", the minted run folder
// is written to selections.runFolder.
//
// In RED: minter is not called; selections.runFolder stays "".
// In GREEN: selections.runFolder == fixedMintedRunFolder.
func TestRunSelect_NewRun_WithMinter_PopulatesMintedRunFolder(t *testing.T) {
	candidates := []runscan.RunCandidate{
		newTestCandidate("20260701T120000Z-a3f9", "/ws/Orchestration-20260701T120000Z-a3f9"),
		newTestCandidate("20260702T120000Z-b4e8", "/ws/Orchestration-20260702T120000Z-b4e8"),
	}
	calls := make([]factoryCall, 0)
	capSess := newCapturingSession()
	m := newModelWithScanMinterFactory(candidates, fixedMinter(), &calls, capSess)
	if m.screen != screenRunSelect {
		t.Fatalf("precondition: screen = %v, want screenRunSelect", m.screen)
	}

	sendKey(m, tea.KeyEnter)

	if m.selections.runFolder == "" {
		t.Error("selections.runFolder is empty after 'new run' with minter; want minted run folder")
	}
	if m.selections.runFolder != fixedMintedRunFolder {
		t.Errorf("selections.runFolder = %q, want minted value %q", m.selections.runFolder, fixedMintedRunFolder)
	}
}

// TestRunSelect_NewRun_WithMinter_SessionFactoryCalledWithMintedFolder verifies that
// when MintRunIdentity is provided and the user picks "new run", the session factory is
// called with the minted run folder — not with an empty string.
//
// In RED: factory is called with "" (current code passes "" for new runs).
// In GREEN: factory is called with fixedMintedRunFolder.
func TestRunSelect_NewRun_WithMinter_SessionFactoryCalledWithMintedFolder(t *testing.T) {
	candidates := []runscan.RunCandidate{
		newTestCandidate("20260701T120000Z-a3f9", "/ws/Orchestration-20260701T120000Z-a3f9"),
		newTestCandidate("20260702T120000Z-b4e8", "/ws/Orchestration-20260702T120000Z-b4e8"),
	}
	calls := make([]factoryCall, 0)
	capSess := newCapturingSession()
	m := newModelWithScanMinterFactory(candidates, fixedMinter(), &calls, capSess)
	if m.screen != screenRunSelect {
		t.Fatalf("precondition: screen = %v, want screenRunSelect", m.screen)
	}

	sendKey(m, tea.KeyEnter)

	// The session factory must have been called at least once for the "new run" selection.
	if len(calls) == 0 {
		t.Fatal("session factory was not called after 'new run' selection; want at least one call")
	}
	// Find the new-run factory call and verify its run folder argument.
	var found bool
	for _, c := range calls {
		if c.isNewRun {
			found = true
			if c.runFolder != fixedMintedRunFolder {
				t.Errorf("session factory (isNewRun=true) received runFolder = %q, want minted %q; "+
					"an empty folder must never reach the factory when a minter is provided",
					c.runFolder, fixedMintedRunFolder)
			}
		}
	}
	if !found {
		t.Error("no session factory call with isNewRun=true found; " +
			"factory must be called when 'new run' is selected on the run-select screen")
	}
}

// TestRunSelect_NewRun_NilMinter_PreservesEmptySelections verifies that when
// MintRunIdentity is nil (the legacy/test path), selecting "new run" on the
// run-select screen leaves selections.runID and selections.runFolder empty,
// preserving backward-compatible behaviour and not panicking.
//
// This test passes in both RED and GREEN: it guards against accidental regression
// where a nil-minter check is removed and the code panics on nil dereference.
func TestRunSelect_NewRun_NilMinter_PreservesEmptySelections(t *testing.T) {
	candidates := []runscan.RunCandidate{
		newTestCandidate("20260701T120000Z-a3f9", "/ws/Orchestration-20260701T120000Z-a3f9"),
		newTestCandidate("20260702T120000Z-b4e8", "/ws/Orchestration-20260702T120000Z-b4e8"),
	}
	// newModelWithScan does not set MintRunIdentity; field is nil.
	m := newModelWithScan(candidates)
	if m.screen != screenRunSelect {
		t.Fatalf("precondition: screen = %v, want screenRunSelect", m.screen)
	}

	sendKey(m, tea.KeyEnter)

	if m.selections.runID != "" {
		t.Errorf("selections.runID = %q, want empty string (nil minter must preserve legacy empty-selection behaviour)",
			m.selections.runID)
	}
	if m.selections.runFolder != "" {
		t.Errorf("selections.runFolder = %q, want empty string (nil minter must preserve legacy empty-selection behaviour)",
			m.selections.runFolder)
	}
	if !m.selections.isNewRun {
		t.Error("selections.isNewRun = false after 'new run' selection; want true")
	}
}

// TestRunSelect_Resume_WithMinter_MinterNotCalled verifies that when the user selects
// an existing candidate on the run-select screen (the resume path), the MintRunIdentity
// function is never invoked — minting is exclusive to the "new run" branch.
func TestRunSelect_Resume_WithMinter_MinterNotCalled(t *testing.T) {
	candidates := []runscan.RunCandidate{
		newTestCandidate("20260701T120000Z-a3f9", "/ws/Orchestration-20260701T120000Z-a3f9"),
		newTestCandidate("20260702T120000Z-b4e8", "/ws/Orchestration-20260702T120000Z-b4e8"),
	}
	mintCalled := false
	countingMinter := RunIdentityMinter(func() (string, string) {
		mintCalled = true
		return fixedMintedRunID, fixedMintedRunFolder
	})
	calls := make([]factoryCall, 0)
	capSess := newCapturingSession()
	m := newModelWithScanMinterFactory(candidates, countingMinter, &calls, capSess)
	if m.screen != screenRunSelect {
		t.Fatalf("precondition: screen = %v, want screenRunSelect", m.screen)
	}

	// Navigate down once to move past "Start a new run" to the first candidate, then select.
	sendKey(m, tea.KeyDown)
	sendKey(m, tea.KeyEnter)

	if mintCalled {
		t.Error("minter was called when a candidate was selected (resume path); " +
			"minting must only occur in the 'new run' branch")
	}
	if m.selections.runID != "20260701T120000Z-a3f9" {
		t.Errorf("selections.runID = %q, want candidate run ID %q (resume path must use candidate identity)",
			m.selections.runID, "20260701T120000Z-a3f9")
	}
}

// ---------------------------------------------------------------------------
// Run selection screen: OnRunIDResolved callback
// ---------------------------------------------------------------------------

// TestRunSelect_NewRun_WithMinter_OnRunIDResolved_CalledWithMintedRunID verifies that
// when OnRunIDResolved is set and the user picks "new run" on the run-select screen with
// a minter provided, the callback receives exactly the minted run_id.
//
// In RED (compile failure): OnRunIDResolved field does not exist on Options (I1.1 not done).
// After I1.1 (compile OK): callback is never invoked because updateRunSelect does not call
// it yet (I1.2 not done) — test still fails at runtime.
// In GREEN (after I1.1+I1.2): callback receives fixedMintedRunID.
func TestRunSelect_NewRun_WithMinter_OnRunIDResolved_CalledWithMintedRunID(t *testing.T) {
	candidates := []runscan.RunCandidate{
		newTestCandidate("20260701T120000Z-a3f9", "/ws/Orchestration-20260701T120000Z-a3f9"),
		newTestCandidate("20260702T120000Z-b4e8", "/ws/Orchestration-20260702T120000Z-b4e8"),
	}
	calls := make([]factoryCall, 0)
	capSess := newCapturingSession()

	var callbackRunIDs []string
	onResolved := func(runID string) {
		callbackRunIDs = append(callbackRunIDs, runID)
	}

	m := newModelWithScanMinterFactoryCallback(candidates, fixedMinter(), &calls, capSess, onResolved)
	if m.screen != screenRunSelect {
		t.Fatalf("precondition: screen = %v, want screenRunSelect", m.screen)
	}

	// "Start a new run" is the first item in the run-select list. Enter selects it.
	sendKey(m, tea.KeyEnter)

	if len(callbackRunIDs) == 0 {
		t.Fatal("OnRunIDResolved was not called after 'new run' with minter; want exactly one call with the minted run_id")
	}
	if len(callbackRunIDs) > 1 {
		t.Errorf("OnRunIDResolved was called %d times; want exactly 1", len(callbackRunIDs))
	}
	if callbackRunIDs[0] != fixedMintedRunID {
		t.Errorf("OnRunIDResolved received %q, want minted value %q", callbackRunIDs[0], fixedMintedRunID)
	}
}

// TestRunSelect_NewRun_NilMinter_OnRunIDResolved_NotCalled verifies that when the minter
// is nil (the legacy/backward-compat path), OnRunIDResolved is NOT called after "new run"
// is selected, because the resolved run_id is empty and the guard must prevent the callback
// from firing with an empty string.
//
// In RED (compile failure): OnRunIDResolved field does not exist on Options (I1.1 not done).
// In GREEN (after I1.1+I1.2): callback is never invoked because selections.runID stays "".
func TestRunSelect_NewRun_NilMinter_OnRunIDResolved_NotCalled(t *testing.T) {
	candidates := []runscan.RunCandidate{
		newTestCandidate("20260701T120000Z-a3f9", "/ws/Orchestration-20260701T120000Z-a3f9"),
		newTestCandidate("20260702T120000Z-b4e8", "/ws/Orchestration-20260702T120000Z-b4e8"),
	}
	sess := &stubNavSession{outcome: domain.RunOutcome{Status: domain.RunCompleted, Message: "ok"}}
	scanResult := &runscan.ScanResult{Candidates: candidates}

	callbackInvoked := false
	onResolved := func(runID string) {
		callbackInvoked = true
	}

	// MintRunIdentity is intentionally nil so selections.runID will be "" after "new run" selection.
	m := newRootModel(context.Background(), sess, Options{
		Theme:           tuicommon.DefaultTheme(),
		ScanResult:      scanResult,
		OnRunIDResolved: onResolved,
	})
	if m.screen != screenRunSelect {
		t.Fatalf("precondition: screen = %v, want screenRunSelect", m.screen)
	}

	// Select "Start a new run" — the first item; Enter confirms.
	sendKey(m, tea.KeyEnter)

	if callbackInvoked {
		t.Error("OnRunIDResolved was called when run_id is empty (nil-minter path); " +
			"callback must not fire when the resolved run_id is empty")
	}
}

// TestRunSelect_Resume_OnRunIDResolved_CalledWithCandidateRunID verifies that when
// OnRunIDResolved is set and the user selects an existing candidate on the run-select
// screen, the callback receives that candidate's run_id.
//
// In RED (compile failure): OnRunIDResolved field does not exist on Options (I1.1 not done).
// After I1.1 (compile OK): callback is never invoked because updateRunSelect does not call
// it yet (I1.2 not done) — test still fails at runtime.
// In GREEN (after I1.1+I1.2): callback receives candidateRunID.
func TestRunSelect_Resume_OnRunIDResolved_CalledWithCandidateRunID(t *testing.T) {
	const candidateRunID = "20260701T120000Z-a3f9"
	candidates := []runscan.RunCandidate{
		newTestCandidate(candidateRunID, "/ws/Orchestration-"+candidateRunID),
		newTestCandidate("20260702T120000Z-b4e8", "/ws/Orchestration-20260702T120000Z-b4e8"),
	}
	calls := make([]factoryCall, 0)
	capSess := newCapturingSession()

	var callbackRunIDs []string
	onResolved := func(runID string) {
		callbackRunIDs = append(callbackRunIDs, runID)
	}

	m := newModelWithScanMinterFactoryCallback(candidates, fixedMinter(), &calls, capSess, onResolved)
	if m.screen != screenRunSelect {
		t.Fatalf("precondition: screen = %v, want screenRunSelect", m.screen)
	}

	// Navigate down once past "Start a new run" to reach the first candidate, then select.
	sendKey(m, tea.KeyDown)
	sendKey(m, tea.KeyEnter)

	if len(callbackRunIDs) == 0 {
		t.Fatal("OnRunIDResolved was not called after selecting an existing candidate; " +
			"want exactly one call with the candidate's run_id")
	}
	if len(callbackRunIDs) > 1 {
		t.Errorf("OnRunIDResolved was called %d times; want exactly 1", len(callbackRunIDs))
	}
	if callbackRunIDs[0] != candidateRunID {
		t.Errorf("OnRunIDResolved received %q, want candidate run_id %q", callbackRunIDs[0], candidateRunID)
	}
}

// TestRunSelect_OnRunIDResolved_NilCallback_NewRun_NoopNoPanic verifies that when
// OnRunIDResolved is nil (not provided in Options), selecting "new run" with a minter
// does not panic and still populates selections.runID normally.
//
// This test is expected to pass in both RED and GREEN states — it guards against
// regressions where a nil-check is omitted and the code panics on nil dereference.
func TestRunSelect_OnRunIDResolved_NilCallback_NewRun_NoopNoPanic(t *testing.T) {
	candidates := []runscan.RunCandidate{
		newTestCandidate("20260701T120000Z-a3f9", "/ws/Orchestration-20260701T120000Z-a3f9"),
		newTestCandidate("20260702T120000Z-b4e8", "/ws/Orchestration-20260702T120000Z-b4e8"),
	}
	calls := make([]factoryCall, 0)
	capSess := newCapturingSession()

	// OnRunIDResolved is nil — the old-path / no-callback behaviour.
	m := newModelWithScanMinterFactory(candidates, fixedMinter(), &calls, capSess)
	if m.screen != screenRunSelect {
		t.Fatalf("precondition: screen = %v, want screenRunSelect", m.screen)
	}

	defer func() {
		if r := recover(); r != nil {
			t.Errorf("selecting 'new run' with nil OnRunIDResolved caused a panic: %v", r)
		}
	}()

	sendKey(m, tea.KeyEnter)

	// selections.runID must still be populated by the minter regardless of nil callback.
	if m.selections.runID != fixedMintedRunID {
		t.Errorf("selections.runID = %q, want %q (minting must work even with nil OnRunIDResolved)",
			m.selections.runID, fixedMintedRunID)
	}
}
