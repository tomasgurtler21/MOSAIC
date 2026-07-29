package tui

// helpers_test.go covers the internal helper functions and the TUIDeviationResolver
// type that were added or fixed during the review-fix phase of Stage-10.
//
// Covered:
//   - extractField: key=value parser used to decode session notice messages
//   - extractStatus: convenience wrapper over extractField for the "status" key
//   - TUIDeviationResolver.Resolve: nil-program fallback to Stop instruction

import (
	"context"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"mosaic-common/interaction"
	"mosaic-run/internal/domain"
	"mosaic-run/internal/tui/screens"
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
// TUIDeviationResolver — nil-program fallback
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// TUIDeviationResolver — Resolve branch coverage helpers
// ---------------------------------------------------------------------------

// autoReplyDeviationModel is a minimal tea.Model used in headless programs. When it
// receives a deviationRequestMsg it sends a pre-configured canned reply on the message's
// reply channel and signals tea.Quit to shut the program down.
type autoReplyDeviationModel struct {
	canned deviationReplyMsg
}

func (m *autoReplyDeviationModel) Init() tea.Cmd { return nil }

func (m *autoReplyDeviationModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if req, ok := msg.(deviationRequestMsg); ok {
		req.reply <- m.canned
		return m, tea.Quit
	}
	return m, nil
}

func (m *autoReplyDeviationModel) View() string { return "" }

// noReplyDeviationModel is a minimal tea.Model used in headless programs. When it
// receives a deviationRequestMsg it signals the received channel but never sends a
// reply and never quits. This lets tests control when to cancel the context.
type noReplyDeviationModel struct {
	received chan struct{}
}

func (m *noReplyDeviationModel) Init() tea.Cmd { return nil }

func (m *noReplyDeviationModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if _, ok := msg.(deviationRequestMsg); ok {
		select {
		case m.received <- struct{}{}:
		default:
		}
	}
	return m, nil
}

func (m *noReplyDeviationModel) View() string { return "" }

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

// ---------------------------------------------------------------------------
// TUIDeviationResolver — Resolve branch tests (issue 2)
// ---------------------------------------------------------------------------

// TestTUIDeviationResolver_ManualNoAgent_ReturnsRejoinAtRow verifies that when the TUI
// sends back DeviationChoiceManual with an empty Agent field, Resolve returns a
// RejoinInstruction with a non-nil Rejoin (RejoinAtRow) and a nil Custom field.
func TestTUIDeviationResolver_ManualNoAgent_ReturnsRejoinAtRow(t *testing.T) {
	canned := deviationReplyMsg{
		choice:     screens.DeviationChoiceManual,
		resolution: screens.ManualResolution{Agent: "", RejoinRowIndex: 2},
	}
	p, progDone := runHeadless(&autoReplyDeviationModel{canned: canned})

	ref := NewProgramRef()
	ref.set(p)
	resolver := &TUIDeviationResolver{Program: ref}

	instr, err := resolver.Resolve(context.Background(), domain.DeviationInfo{})
	<-progDone

	if err != nil {
		t.Fatalf("Resolve returned unexpected error: %v", err)
	}
	if instr.Rejoin == nil {
		t.Fatalf("instr.Rejoin = nil, want non-nil (RejoinAtRow path for empty Agent)")
	}
	if instr.Custom != nil {
		t.Errorf("instr.Custom = non-nil, want nil (empty Agent must NOT produce CustomDispatch)")
	}
	if instr.Rejoin.RowIndex != 2 {
		t.Errorf("instr.Rejoin.RowIndex = %d, want 2", instr.Rejoin.RowIndex)
	}
}

// TestTUIDeviationResolver_ManualWithAgent_ReturnsCustomDispatch verifies that when the
// TUI sends back DeviationChoiceManual with a non-empty Agent field, Resolve returns a
// RejoinInstruction with a non-nil Custom (CustomDispatch) and a nil Rejoin field.
func TestTUIDeviationResolver_ManualWithAgent_ReturnsCustomDispatch(t *testing.T) {
	canned := deviationReplyMsg{
		choice:     screens.DeviationChoiceManual,
		resolution: screens.ManualResolution{Agent: "custom-agent", RejoinRowIndex: 0},
	}
	p, progDone := runHeadless(&autoReplyDeviationModel{canned: canned})

	ref := NewProgramRef()
	ref.set(p)
	resolver := &TUIDeviationResolver{Program: ref}

	instr, err := resolver.Resolve(context.Background(), domain.DeviationInfo{})
	<-progDone

	if err != nil {
		t.Fatalf("Resolve returned unexpected error: %v", err)
	}
	if instr.Custom == nil {
		t.Fatalf("instr.Custom = nil, want non-nil (CustomDispatch path for non-empty Agent)")
	}
	if instr.Rejoin != nil {
		t.Errorf("instr.Rejoin = non-nil, want nil (non-empty Agent must NOT produce RejoinAtRow)")
	}
	if instr.Custom.Agent.Identifier != "custom-agent" {
		t.Errorf("Custom.Agent.Identifier = %q, want %q", instr.Custom.Agent.Identifier, "custom-agent")
	}
}

// TestTUIDeviationResolver_DelegateNilDelegate_ReturnsStop verifies that when the TUI
// sends back DeviationChoiceDelegate but the resolver has no Delegate wired, Resolve
// falls back to a Stop instruction.
func TestTUIDeviationResolver_DelegateNilDelegate_ReturnsStop(t *testing.T) {
	canned := deviationReplyMsg{choice: screens.DeviationChoiceDelegate}
	p, progDone := runHeadless(&autoReplyDeviationModel{canned: canned})

	ref := NewProgramRef()
	ref.set(p)
	resolver := &TUIDeviationResolver{
		Program:  ref,
		Delegate: nil, // no delegate configured
	}

	instr, err := resolver.Resolve(context.Background(), domain.DeviationInfo{})
	<-progDone

	if err != nil {
		t.Fatalf("Resolve returned unexpected error: %v", err)
	}
	if instr.Stop == nil {
		t.Errorf("instr.Stop = nil, want non-nil (nil Delegate should produce Stop fallback)")
	}
}

// TestTUIDeviationResolver_ContextCancelled_ReturnsStop verifies that when the calling
// context is cancelled while Resolve is waiting for a reply, Resolve returns immediately
// with a Stop instruction rather than blocking indefinitely.
func TestTUIDeviationResolver_ContextCancelled_ReturnsStop(t *testing.T) {
	received := make(chan struct{}, 1)
	p, progDone := runHeadless(&noReplyDeviationModel{received: received})

	ref := NewProgramRef()
	ref.set(p)
	resolver := &TUIDeviationResolver{Program: ref}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Run Resolve in a goroutine: p.Send blocks until the event loop reads the message,
	// then Resolve enters the select waiting for a reply or ctx.Done.
	resolveDone := make(chan domain.RejoinInstruction, 1)
	go func() {
		instr, _ := resolver.Resolve(ctx, domain.DeviationInfo{})
		resolveDone <- instr
	}()

	// Wait until the model has received the deviation request.
	select {
	case <-received:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout: deviation request was not delivered to the headless program")
	}

	// Cancel the context so the select in Resolve picks ctx.Done().
	cancel()

	// Resolve should return promptly.
	var instr domain.RejoinInstruction
	select {
	case instr = <-resolveDone:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout: Resolve did not return after context cancellation")
	}

	// Shut the headless program down (it never received tea.Quit from the model).
	p.Kill()
	<-progDone

	if instr.Stop == nil {
		t.Errorf("instr.Stop = nil after context cancellation, want non-nil Stop instruction")
	}
}

// ---------------------------------------------------------------------------
// ProgramRef — nil-program fallbacks (issue 8)
// ---------------------------------------------------------------------------

// TestTUIDeviationResolver_NilProgram_ReturnsStop verifies that when no Bubble Tea
// program has been started (ProgramRef holds a nil *tea.Program), Resolve returns
// a Stop instruction rather than blocking or panicking. This guards the case where
// a session goroutine calls Resolve before the TUI's Run() has started the program.
func TestTUIDeviationResolver_NilProgram_ReturnsStop(t *testing.T) {
	resolver := &TUIDeviationResolver{
		Program: NewProgramRef(), // no tea.Program set — program() returns nil
	}

	info := domain.DeviationInfo{
		Kind: domain.DeviationNonSuccess,
		Response: domain.ProtocolResponse{
			AgentInstanceID: "test-agent#1",
			StatusCode:      domain.StatusBLOCKED,
			StatusMessage:   "blocked",
		},
		CurrentPhase: "PLANNING",
	}

	instr, err := resolver.Resolve(context.Background(), info)
	if err != nil {
		t.Fatalf("Resolve returned unexpected error: %v", err)
	}
	if instr.Stop == nil {
		t.Errorf("Resolve() with nil program returned instr.Stop = nil; want a Stop instruction")
	}
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
