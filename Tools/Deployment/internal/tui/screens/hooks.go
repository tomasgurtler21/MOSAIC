package screens

// hooks.go implements the HookScreen: hook-bundle selection with three distinct presentation
// modes depending on what the harness and catalog provide:
//
//  1. Harness does not support hooks at all → clear "not supported" message (AC21.5).
//  2. Harness supports hooks; catalog has bundles → multi-select with placeholder bundles
//     visually distinguished from working ones (AC21.5).
//  3. No hook bundles in the catalog → empty-state message (AC21.6).
//
// Navigation contract (when the list is shown):
//   - ↑/↓ or k/j to navigate.
//   - Space to toggle selection.
//   - Enter to confirm the selection.
//   - Esc to go back.
//
// Placeholder bundles are selectable so the user can still include them; the detail pane
// shows a clear warning that the content is a placeholder and may not be functional (AC21.5).

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/tui/widgets"
)

// HookScreen presents hook bundles for multi-select. When the harness does not support
// hooks the list is replaced by an informational message. Placeholder bundles are shown
// with a [placeholder] suffix and a prominent warning in the detail pane.
type HookScreen struct {
	hooks              []domain.HookBundle
	harnessSupported   bool // false → show "not supported" message instead of list
	multisel           *widgets.MultiSelect
	detail             *widgets.DetailPane

	// Error state (catalog load failure etc.).
	hasError bool
	errorMsg string

	done   bool
	back   bool
	width  int
	height int
	styles Styles
}

// NewHookScreen creates the hook selection screen.
//
// harnessSupported controls whether the harness in question can use hooks at all. When
// false the screen renders an informative "not supported" message and users cannot select
// anything. hooks is the list of available bundles; empty hooks with harnessSupported true
// produces the empty-state message. Pass errorMsg to render a catalog-error state.
func NewHookScreen(
	hooks []domain.HookBundle,
	harnessSupported bool,
	width, height int,
	styles Styles,
	errorMsg string,
) *HookScreen {
	lh := hookListHeight(height)
	lw := hookListWidth(width)
	dw := width - lw - 1
	if dw < 10 {
		dw = 10
	}

	items := makeHookItems(hooks)
	msStyles := widgets.MultiSelectStyles{
		Normal:   styles.Body,
		Selected: styles.Selected,
		Checked:  styles.Checked,
		Disabled: styles.Muted,
		Cursor:   "▶",
		CheckOn:  "[x]",
		CheckOff: "[ ]",
	}
	detailStyles := widgets.DetailPaneStyles{
		Title:  styles.Subtitle,
		Body:   styles.Body,
		Border: styles.Border,
		Empty:  styles.Muted,
	}

	s := &HookScreen{
		hooks:            hooks,
		harnessSupported: harnessSupported,
		multisel:         widgets.NewMultiSelect(items, lh, lw, msStyles),
		detail:           widgets.NewDetailPane(lh, dw, detailStyles),
		hasError:         errorMsg != "",
		errorMsg:         errorMsg,
		width:            width,
		height:           height,
		styles:           styles,
	}
	s.updateDetail()
	return s
}

// makeHookItems converts a slice of hook bundles into multi-select list items. Placeholder
// bundles are annotated with [placeholder] in their label.
func makeHookItems(hooks []domain.HookBundle) []widgets.ListItem {
	items := make([]widgets.ListItem, len(hooks))
	for i, h := range hooks {
		label := h.Key
		if h.Placeholder {
			label = fmt.Sprintf("%s  [placeholder]", h.Key)
		}

		var detail strings.Builder
		if h.Description != "" {
			fmt.Fprintf(&detail, "%s\n\n", h.Description)
		}
		if h.Placeholder {
			fmt.Fprintf(&detail, "⚠ This bundle contains placeholder content and may not be functional.")
		}

		items[i] = widgets.ListItem{
			ID:     h.Key,
			Label:  label,
			Detail: detail.String(),
		}
	}
	return items
}

// Update processes key input.
func (s *HookScreen) Update(msg tea.Msg) tea.Cmd {
	// In not-supported, error, or empty states only Esc matters.
	if !s.harnessSupported || s.hasError || len(s.hooks) == 0 {
		if keyMsg, ok := msg.(tea.KeyMsg); ok {
			switch keyMsg.String() {
			case "esc", "enter":
				// Both esc and enter advance past the informational screen.
				s.done = true
			}
		}
		return nil
	}

	cmd := s.multisel.Update(msg)
	if s.multisel.Done() {
		s.done = true
	} else if s.multisel.Back() {
		s.back = true
	} else {
		s.updateDetail()
	}
	return cmd
}

// updateDetail refreshes the detail pane to show the focused bundle's description.
func (s *HookScreen) updateDetail() {
	idx := s.multisel.CursorIndex()
	if idx < 0 || idx >= len(s.hooks) {
		s.detail.SetContent("", "")
		return
	}
	h := s.hooks[idx]

	title := h.Key
	if h.Placeholder {
		title = fmt.Sprintf("%s  [placeholder]", h.Key)
	}

	var body strings.Builder
	if h.Description != "" {
		fmt.Fprintf(&body, "%s\n\n", h.Description)
	}
	if h.Placeholder {
		fmt.Fprintf(&body,
			"Warning: this bundle's content is a placeholder.\n"+
				"It may not function as described until its variant files are provided.",
		)
	} else {
		fmt.Fprintf(&body, "Version: %s", h.Version)
	}
	s.detail.SetContent(title, body.String())
}

// View renders the hook selection screen.
func (s *HookScreen) View() string {
	title := s.styles.Title.Width(s.width).Render("Select Hook Bundles")
	border := s.styles.Border.Width(s.width).Render(strings.Repeat("─", s.width))

	var content string
	var help string

	switch {
	case s.hasError:
		content = s.styles.Error.Width(s.width).Render("Error: " + s.errorMsg)
		help = s.styles.Help.Width(s.width).Render("esc/enter continue  ctrl+c quit")

	case !s.harnessSupported:
		content = s.styles.Warning.Width(s.width).Render(
			"The selected harness does not support hooks.\n" +
				"No hooks can be deployed for this target.",
		)
		help = s.styles.Help.Width(s.width).Render("esc/enter continue  ctrl+c quit")

	case len(s.hooks) == 0:
		content = s.styles.Muted.Width(s.width).Render(
			"No hook bundles are available in the catalog.",
		)
		help = s.styles.Help.Width(s.width).Render("esc/enter continue  ctrl+c quit")

	default:
		lw := hookListWidth(s.width)
		sep := s.styles.Border.Render("│")
		content = lipgloss.JoinHorizontal(
			lipgloss.Top,
			lipgloss.NewStyle().Width(lw).Render(s.multisel.View()),
			sep,
			s.detail.View(),
		)
		help = s.styles.Help.Width(s.width).Render(
			"↑/k up  ↓/j down  space toggle  enter confirm  esc back  ctrl+c quit",
		)
	}

	return strings.Join([]string{title, border, content, border, help}, "\n")
}

// Done reports whether the user confirmed the selection (or advanced past an informational screen).
func (s *HookScreen) Done() bool { return s.done }

// Back reports whether the user cancelled on a selectable screen.
func (s *HookScreen) Back() bool { return s.back }

// SelectedIDs returns the keys of the selected hook bundles in declaration order.
// Returns an empty slice when no bundles are available or the harness does not support hooks.
func (s *HookScreen) SelectedIDs() []string {
	if !s.harnessSupported || len(s.hooks) == 0 || s.hasError {
		return []string{}
	}
	return s.multisel.SelectedIDs()
}

// Reset clears Done and Back flags.
func (s *HookScreen) Reset() {
	s.done = false
	s.back = false
	if s.harnessSupported && len(s.hooks) > 0 && !s.hasError {
		s.multisel.Reset()
	}
}

// Resize updates the screen dimensions.
func (s *HookScreen) Resize(width, height int) {
	s.width = width
	s.height = height
	lh := hookListHeight(height)
	lw := hookListWidth(width)
	dw := width - lw - 1
	if dw < 10 {
		dw = 10
	}
	s.multisel.Resize(lh, lw)
	s.detail.Resize(lh, dw)
}

// hookListHeight calculates the available content height.
func hookListHeight(totalHeight int) int {
	// title + border + border + help = 4 rows reserved
	h := totalHeight - 5
	if h < 3 {
		return 3
	}
	return h
}

// hookListWidth returns the list column width (left two-thirds).
func hookListWidth(totalWidth int) int {
	w := totalWidth * 2 / 3
	if w < 30 {
		return totalWidth
	}
	return w
}
