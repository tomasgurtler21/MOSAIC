package tui

// helpers_test.go covers the internal helper functions of the tui package.
//
// Covered:
//   - extractField: key=value parser used to decode session notice messages
//   - extractStatus: convenience wrapper over extractField for the "status" key
//   - ProgramRef nil-program fallbacks: SelectOne, AskText, and Confirm behaviour
//     when no tea.Program has been started

import (
	"context"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"mosaic-common/interaction"
)

// ---------------------------------------------------------------------------
// extractField
// ---------------------------------------------------------------------------

func TestExtractField_SimpleValue(t *testing.T) {
	msg := `phase=PLANNING stage="" status=running`
	if got := extractField(msg, "phase"); got != "PLANNING" {
		t.Errorf("extractField(phase) = %q, want %q", got, "PLANNING")
	}
}

func TestExtractField_QuotedValue(t *testing.T) {
	msg := `phase=EXECUTION stage="Stage-2" status=SUCCESS`
	if got := extractField(msg, "stage"); got != "Stage-2" {
		t.Errorf("extractField(stage) = %q, want %q", got, "Stage-2")
	}
}

func TestExtractField_QuotedEmptyValue(t *testing.T) {
	msg := `phase=PLANNING stage="" status=running`
	if got := extractField(msg, "stage"); got != "" {
		t.Errorf("extractField(empty-stage) = %q, want empty string", got)
	}
}

func TestExtractField_StatusRunning(t *testing.T) {
	msg := `phase=PLANNING stage="" status=running`
	if got := extractField(msg, "status"); got != "running" {
		t.Errorf("extractField(status) = %q, want %q", got, "running")
	}
}

func TestExtractField_StatusSuccess(t *testing.T) {
	msg := `phase=PLANNING stage="" status=SUCCESS`
	if got := extractField(msg, "status"); got != "SUCCESS" {
		t.Errorf("extractField(status) = %q, want %q", got, "SUCCESS")
	}
}

func TestExtractField_KeyNotPresent(t *testing.T) {
	msg := `phase=PLANNING stage="" status=running`
	if got := extractField(msg, "missing"); got != "" {
		t.Errorf("extractField(missing key) = %q, want empty string", got)
	}
}

func TestExtractField_EmptyMessage(t *testing.T) {
	if got := extractField("", "status"); got != "" {
		t.Errorf("extractField(empty message) = %q, want empty string", got)
	}
}

func TestExtractField_PrefixMatchOnly(t *testing.T) {
	// "statussomething=value" must NOT match the "status" key.
	msg := `statussomething=extra status=real`
	if got := extractField(msg, "status"); got != "real" {
		t.Errorf("extractField must match exact key: got %q, want %q", got, "real")
	}
}

// ---------------------------------------------------------------------------
// extractStatus
// ---------------------------------------------------------------------------

func TestExtractStatus_RunningMessage(t *testing.T) {
	msg := `phase=PLANNING stage="" status=running`
	if got := extractStatus(msg); got != "running" {
		t.Errorf("extractStatus() = %q, want %q", got, "running")
	}
}

func TestExtractStatus_SuccessMessage(t *testing.T) {
	msg := `phase=DESIGN stage="" status=SUCCESS`
	if got := extractStatus(msg); got != "SUCCESS" {
		t.Errorf("extractStatus() = %q, want %q", got, "SUCCESS")
	}
}

func TestExtractStatus_BlockedMessage(t *testing.T) {
	msg := `phase=REVIEW stage="" status=BLOCKED`
	if got := extractStatus(msg); got != "BLOCKED" {
		t.Errorf("extractStatus() = %q, want %q", got, "BLOCKED")
	}
}

func TestExtractStatus_NoStatusKey_ReturnsEmpty(t *testing.T) {
	if got := extractStatus("phase=PLANNING"); got != "" {
		t.Errorf("extractStatus(no status key) = %q, want empty string", got)
	}
}

// ---------------------------------------------------------------------------
// ProgramRef helper — runHeadless for nil-program fallback tests
// ---------------------------------------------------------------------------

// runHeadless creates and starts a headless Bubble Tea program with no renderer and no
// input reader, suitable for unit-testing Send-based interactions. It returns the
// program and a channel that is closed when p.Run() returns.
func runHeadless(model tea.Model) (*tea.Program, chan struct{}) {
	p := tea.NewProgram(model,
		tea.WithoutRenderer(),
		tea.WithInput(nil),
		tea.WithoutSignals(),
	)
	done := make(chan struct{})
	go func() {
		p.Run() //nolint:errcheck
		close(done)
	}()
	return p, done
}

// TestProgramRef_SelectOne_NilProgram_ReturnsSkippedOne verifies that SelectOne called on
// a ProgramRef with no program set (nil *tea.Program) returns SkippedOne immediately,
// rather than blocking or panicking.
func TestProgramRef_SelectOne_NilProgram_ReturnsSkippedOne(t *testing.T) {
	ref := NewProgramRef()
	q := interaction.ChoiceQuestion{Question: interaction.Question{Title: "Pick one"}}

	ans, err := ref.SelectOne(context.Background(), q)
	if err != nil {
		t.Fatalf("SelectOne with nil program returned unexpected error: %v", err)
	}
	if ans.Status != interaction.SkippedOne {
		t.Errorf("SelectOne status = %q, want %q", ans.Status, interaction.SkippedOne)
	}
}

// TestProgramRef_AskText_NilProgram_ReturnsSkippedOne verifies that AskText called on a
// ProgramRef with no program set returns SkippedOne immediately, rather than blocking.
func TestProgramRef_AskText_NilProgram_ReturnsSkippedOne(t *testing.T) {
	ref := NewProgramRef()
	q := interaction.TextQuestion{Question: interaction.Question{Title: "Enter text"}}

	ans, err := ref.AskText(context.Background(), q)
	if err != nil {
		t.Fatalf("AskText with nil program returned unexpected error: %v", err)
	}
	if ans.Status != interaction.SkippedOne {
		t.Errorf("AskText status = %q, want %q", ans.Status, interaction.SkippedOne)
	}
}

// TestProgramRef_Confirm_NilProgram_ReturnsFalse verifies that Confirm called on a
// ProgramRef with no program set returns Answered/false (the safe default), rather than
// blocking or panicking.
func TestProgramRef_Confirm_NilProgram_ReturnsFalse(t *testing.T) {
	ref := NewProgramRef()
	q := interaction.Question{Title: "Confirm?"}

	ans, err := ref.Confirm(context.Background(), q)
	if err != nil {
		t.Fatalf("Confirm with nil program returned unexpected error: %v", err)
	}
	if ans.Status != interaction.Answered {
		t.Errorf("Confirm status = %q, want %q", ans.Status, interaction.Answered)
	}
	if ans.Confirm {
		t.Errorf("Confirm.Confirm = true, want false (nil-program fallback must be safe/no-op)")
	}
}
