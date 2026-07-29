package screens

import (
	"fmt"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"mosaic-common/tui/widgets"
	"mosaic-run/internal/domain"
)

// DeviationChoice classifies the user's response on the deviation screen.
type DeviationChoice string

const (
	DeviationChoiceDelegate DeviationChoice = "delegate"
	DeviationChoiceManual   DeviationChoice = "manual"
	DeviationChoiceStop     DeviationChoice = "stop"
)

// ManualResolution holds the user's choices for manual deviation resolution.
// When Agent is non-empty the session constructs a CustomDispatch invocation;
// otherwise a simple RejoinAtRow is used.
type ManualResolution struct {
	RejoinRowIndex int
	HITLOverride   *bool // nil means no override

	// Communication Protocol composition fields. Populated during manual resolution.
	// When Agent is non-empty, the caller should use a CustomDispatch; otherwise
	// a RejoinAtRow at RejoinRowIndex is used.
	Agent           string // custom agent identifier; empty = use routing-table agent
	Task            string // task description for the custom invocation
	InputArtifacts  string // comma-separated input artifact paths (may be empty)
	OutputArtifacts string // comma-separated output artifact paths (may be empty)
	Constraints     string // optional constraints text
}

// deviationStep tracks which phase of the deviation resolution UI is active.
type deviationStep int

const (
	deviationStepChoice          deviationStep = iota
	deviationStepManualRow                     // select rejoin-point row
	deviationStepManualAgent                   // enter agent identifier (optional)
	deviationStepManualTask                    // enter task description
	deviationStepManualInputs                  // enter input artifacts (comma-separated)
	deviationStepManualOutputs                 // enter output artifacts (comma-separated)
	deviationStepManualConstraints             // enter constraints text
	deviationStepManualHITL                    // HITL override: yes/no
	deviationStepManualHITLValue               // HITL override value: true/false
	deviationStepDone
)

// DeviationScreen presents the deviation details and resolution options.
//
// Two resolution paths are offered:
//  1. Delegate to orchestrator (handled externally; the screen just signals the choice).
//  2. Resolve manually (the screen collects the rejoin row, full Communication Protocol
//     composition fields — agent, task, artifacts, constraints — and HITL override).
//
// Navigation contract:
//   - After a choice is made and resolved -> Done() == true, Choice() and Resolution() return results.
//   - Esc on first step -> Back() == true (caller treats this as "stop the run").
type DeviationScreen struct {
	info   domain.DeviationInfo
	table  domain.RoutingTable
	step   deviationStep
	choice DeviationChoice
	manual ManualResolution

	choiceList       *widgets.List
	rowList          *widgets.List
	agentInput       *widgets.TextInput
	taskInput        *widgets.TextInput
	inputsInput      *widgets.TextInput
	outputsInput     *widgets.TextInput
	constraintsInput *widgets.TextInput

	hitlCursor int // 0=no override, 1=yes override
	hitlVal    bool
	hitlValCur int // 0=true, 1=false

	done   bool
	back   bool
	width  int
	height int
	styles Styles
}

// NewDeviationScreen creates a deviation resolution screen.
func NewDeviationScreen(info domain.DeviationInfo, table domain.RoutingTable, width, height int, styles Styles) *DeviationScreen {
	listStyles := widgets.ListStyles{
		Normal:   styles.Body,
		Selected: styles.Selected,
		Disabled: styles.Muted,
		Cursor:   "▶",
	}

	// Build choice list.
	choiceItems := []widgets.ListItem{
		{ID: string(DeviationChoiceDelegate), Label: "Delegate to orchestrator"},
		{ID: string(DeviationChoiceManual), Label: "Resolve manually"},
		{ID: string(DeviationChoiceStop), Label: "Stop the run"},
	}
	choiceList := widgets.NewList(choiceItems, 5, width-4, listStyles)

	// Build routing row list for manual resolution.
	rowItems := make([]widgets.ListItem, 0, len(table.Rows)+1)
	for _, row := range table.Rows {
		rowItems = append(rowItems, widgets.ListItem{
			ID:    strconv.Itoa(row.Index),
			Label: fmt.Sprintf("[%d] %s (%s)", row.Index, row.Agent, row.Phase),
		})
	}
	rowItems = append(rowItems, widgets.ListItem{
		ID:    "stop",
		Label: "Stop the run",
	})
	rowList := widgets.NewList(rowItems, 10, width-4, listStyles)

	// Text input widgets for Communication Protocol composition.
	inputStyles := widgets.TextInputStyles{
		Label:  styles.Subtitle,
		Input:  styles.Body,
		ErrMsg: styles.Error,
	}
	agentInput := widgets.NewTextInput(
		"Agent identifier (leave blank to rejoin without custom dispatch):",
		"e.g. Research#1",
		width-4, inputStyles,
	)
	taskInput := widgets.NewTextInput(
		"Task description:",
		"Describe the task for this invocation",
		width-4, inputStyles,
	)
	inputsInput := widgets.NewTextInput(
		"Input artifacts (comma-separated paths, or leave blank):",
		"e.g. Orchestration/Plan.md, Orchestration/Design.md",
		width-4, inputStyles,
	)
	outputsInput := widgets.NewTextInput(
		"Output artifacts (comma-separated paths, or leave blank):",
		"e.g. Orchestration/Stage-1/Output.md",
		width-4, inputStyles,
	)
	constraintsInput := widgets.NewTextInput(
		"Constraints (optional):",
		"Any additional constraints for this invocation",
		width-4, inputStyles,
	)

	return &DeviationScreen{
		info:             info,
		table:            table,
		choiceList:       choiceList,
		rowList:          rowList,
		agentInput:       agentInput,
		taskInput:        taskInput,
		inputsInput:      inputsInput,
		outputsInput:     outputsInput,
		constraintsInput: constraintsInput,
		width:            width,
		height:           height,
		styles:           styles,
	}
}

// Update processes a key message for the current deviation resolution step.
// Returns a tea.Cmd when text-input widgets need initialisation commands (e.g. cursor blink).
func (s *DeviationScreen) Update(msg tea.Msg) tea.Cmd {
	_, isKey := msg.(tea.KeyMsg)

	switch s.step {
	case deviationStepChoice:
		if !isKey {
			return nil
		}
		s.choiceList.Update(msg)
		if s.choiceList.Back() {
			s.back = true
			return nil
		}
		if s.choiceList.Done() {
			id := s.choiceList.SelectedID()
			s.choice = DeviationChoice(id)
			switch s.choice {
			case DeviationChoiceDelegate, DeviationChoiceStop:
				s.step = deviationStepDone
				s.done = true
			case DeviationChoiceManual:
				s.step = deviationStepManualRow
				s.rowList.Reset()
			}
		}

	case deviationStepManualRow:
		if !isKey {
			return nil
		}
		s.rowList.Update(msg)
		if s.rowList.Back() {
			s.step = deviationStepChoice
			s.choiceList.Reset()
			return nil
		}
		if s.rowList.Done() {
			id := s.rowList.SelectedID()
			if id == "stop" {
				s.choice = DeviationChoiceStop
				s.step = deviationStepDone
				s.done = true
			} else {
				rowIdx, _ := strconv.Atoi(id)
				s.manual.RejoinRowIndex = rowIdx
				s.step = deviationStepManualAgent
				return s.agentInput.Init()
			}
		}

	case deviationStepManualAgent:
		cmd := s.agentInput.Update(msg)
		if s.agentInput.Back() {
			s.agentInput.Reset()
			s.step = deviationStepManualRow
			s.rowList.Reset()
			return nil
		}
		if s.agentInput.Done() {
			s.manual.Agent = strings.TrimSpace(s.agentInput.Value())
			s.agentInput.Reset()
			s.step = deviationStepManualTask
			return s.taskInput.Init()
		}
		return cmd

	case deviationStepManualTask:
		cmd := s.taskInput.Update(msg)
		if s.taskInput.Back() {
			s.taskInput.Reset()
			s.step = deviationStepManualAgent
			return s.agentInput.Init()
		}
		if s.taskInput.Done() {
			s.manual.Task = strings.TrimSpace(s.taskInput.Value())
			s.taskInput.Reset()
			s.step = deviationStepManualInputs
			return s.inputsInput.Init()
		}
		return cmd

	case deviationStepManualInputs:
		cmd := s.inputsInput.Update(msg)
		if s.inputsInput.Back() {
			s.inputsInput.Reset()
			s.step = deviationStepManualTask
			return s.taskInput.Init()
		}
		if s.inputsInput.Done() {
			s.manual.InputArtifacts = strings.TrimSpace(s.inputsInput.Value())
			s.inputsInput.Reset()
			s.step = deviationStepManualOutputs
			return s.outputsInput.Init()
		}
		return cmd

	case deviationStepManualOutputs:
		cmd := s.outputsInput.Update(msg)
		if s.outputsInput.Back() {
			s.outputsInput.Reset()
			s.step = deviationStepManualInputs
			return s.inputsInput.Init()
		}
		if s.outputsInput.Done() {
			s.manual.OutputArtifacts = strings.TrimSpace(s.outputsInput.Value())
			s.outputsInput.Reset()
			s.step = deviationStepManualConstraints
			return s.constraintsInput.Init()
		}
		return cmd

	case deviationStepManualConstraints:
		cmd := s.constraintsInput.Update(msg)
		if s.constraintsInput.Back() {
			s.constraintsInput.Reset()
			s.step = deviationStepManualOutputs
			return s.outputsInput.Init()
		}
		if s.constraintsInput.Done() {
			s.manual.Constraints = strings.TrimSpace(s.constraintsInput.Value())
			s.constraintsInput.Reset()
			s.step = deviationStepManualHITL
			s.hitlCursor = 0
		}
		return cmd

	case deviationStepManualHITL:
		keyMsg, ok := msg.(tea.KeyMsg)
		if !ok {
			return nil
		}
		switch keyMsg.String() {
		case "up", "k":
			if s.hitlCursor > 0 {
				s.hitlCursor--
			}
		case "down", "j":
			if s.hitlCursor < 1 {
				s.hitlCursor++
			}
		case "enter":
			if s.hitlCursor == 0 {
				s.manual.HITLOverride = nil
				s.step = deviationStepDone
				s.done = true
			} else {
				s.step = deviationStepManualHITLValue
				s.hitlValCur = 1 // default false
			}
		case "esc":
			s.step = deviationStepManualConstraints
			return s.constraintsInput.Init()
		}

	case deviationStepManualHITLValue:
		keyMsg, ok := msg.(tea.KeyMsg)
		if !ok {
			return nil
		}
		switch keyMsg.String() {
		case "up", "k":
			if s.hitlValCur > 0 {
				s.hitlValCur--
			}
		case "down", "j":
			if s.hitlValCur < 1 {
				s.hitlValCur++
			}
		case "enter":
			val := s.hitlValCur == 0 // 0=true, 1=false
			s.manual.HITLOverride = &val
			s.step = deviationStepDone
			s.done = true
		case "esc":
			s.step = deviationStepManualHITL
			s.hitlCursor = 0
		}
	}

	return nil
}

// View renders the deviation screen.
func (s *DeviationScreen) View() string {
	title := s.styles.Title.Width(s.width).Render("Deviation Detected")
	border := s.styles.Border.Width(s.width).Render(strings.Repeat("─", s.width))

	// Deviation context.
	contextLines := []string{
		s.styles.Warning.Width(s.width).Render(fmt.Sprintf(
			"Agent %s returned %s at row %d (phase %s)",
			s.info.Response.AgentInstanceID,
			s.info.Response.StatusCode,
			s.info.CurrentRow,
			s.info.CurrentPhase,
		)),
	}
	if s.info.Response.StatusMessage != "" {
		msg := s.info.Response.StatusMessage
		if len(msg) > 120 {
			msg = msg[:117] + "…"
		}
		contextLines = append(contextLines, s.styles.Body.Width(s.width).Render(msg))
	}

	context := strings.Join(contextLines, "\n")

	var body string
	switch s.step {
	case deviationStepChoice:
		body = context + "\n\n" + s.styles.Subtitle.Width(s.width).Render("Choose how to proceed:") + "\n" + s.choiceList.View()
	case deviationStepManualRow:
		body = context + "\n\n" + s.styles.Subtitle.Width(s.width).Render("Select row to rejoin at:") + "\n" + s.rowList.View()
	case deviationStepManualAgent:
		body = context + "\n\n" + s.agentInput.View()
	case deviationStepManualTask:
		body = context + "\n\n" + s.taskInput.View()
	case deviationStepManualInputs:
		body = context + "\n\n" + s.inputsInput.View()
	case deviationStepManualOutputs:
		body = context + "\n\n" + s.outputsInput.View()
	case deviationStepManualConstraints:
		body = context + "\n\n" + s.constraintsInput.View()
	case deviationStepManualHITL:
		body = context + "\n\n" + s.styles.Subtitle.Width(s.width).Render("Override HITL for next invocation?") + "\n" +
			s.renderTwoChoice(s.hitlCursor, "No (keep as-is)", "Yes (override)")
	case deviationStepManualHITLValue:
		body = context + "\n\n" + s.styles.Subtitle.Width(s.width).Render("Set HITL override value:") + "\n" +
			s.renderTwoChoice(s.hitlValCur, "true", "false")
	}

	help := s.styles.Help.Width(s.width).Render("↑/k up  ↓/j down  enter select  esc back  ctrl+c quit")
	return strings.Join([]string{title, border, body, border, help}, "\n")
}

// renderTwoChoice renders two options with the cursor on the active one.
func (s *DeviationScreen) renderTwoChoice(cursor int, opt0, opt1 string) string {
	prefix0, prefix1 := "  ", "  "
	if cursor == 0 {
		prefix0 = "▶ "
	} else {
		prefix1 = "▶ "
	}
	line0 := prefix0 + s.styles.Body.Render(opt0)
	line1 := prefix1 + s.styles.Body.Render(opt1)
	if cursor == 0 {
		line0 = prefix0 + s.styles.Selected.Render(opt0)
	} else {
		line1 = prefix1 + s.styles.Selected.Render(opt1)
	}
	return line0 + "\n" + line1
}

// Done reports whether the user has completed the deviation resolution.
func (s *DeviationScreen) Done() bool { return s.done }

// Back reports whether the user wants to abort (back from the first choice screen).
func (s *DeviationScreen) Back() bool { return s.back }

// Choice returns the user's resolution choice. Only valid when Done() is true.
func (s *DeviationScreen) Choice() DeviationChoice { return s.choice }

// Resolution returns the manual resolution details. Only valid when Done() is true and
// Choice() == DeviationChoiceManual.
func (s *DeviationScreen) Resolution() ManualResolution { return s.manual }

// Resize updates the screen dimensions.
func (s *DeviationScreen) Resize(width, height int) {
	s.width = width
	s.height = height
}
