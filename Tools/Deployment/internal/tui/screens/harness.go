package screens

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/tui/widgets"
)

// harnessListHeight is the default number of harness rows visible at once.
const harnessListHeight = 10

// HarnessScreen presents the list of available harnesses, each annotated with its provision
// tier and a prominent external-module indicator when an external module is active.
//
// Navigation contract:
//   - Enter on a usable harness -> Done() == true, SelectedID() returns the harness ID.
//   - Esc -> Back() == true (caller navigates to the previous screen or exits).
//   - ctrl+c is handled by the root model before reaching this screen.
type HarnessScreen struct {
	harnesses   []domain.HarnessRef
	list        *widgets.List
	detail      *widgets.DetailPane
	width       int
	height      int
	styles      Styles
	helpText    string
	toolVersion string
}

// NewHarnessScreen creates the harness selection screen. harnesses is the live registry list;
// it is shown in the order it is provided.
func NewHarnessScreen(harnesses []domain.HarnessRef, width, height int, styles Styles) *HarnessScreen {
	items := makeHarnessItems(harnesses, styles)
	listStyles := widgets.ListStyles{
		Normal:   styles.Body,
		Selected: styles.Selected,
		Disabled: styles.Muted,
		Cursor:   "▶",
	}
	listHeight := height - 8 // reserve space for title, help, detail-border
	if listHeight < 3 {
		listHeight = 3
	}
	listWidth := width * 2 / 3
	if listWidth < 30 {
		listWidth = width
	}
	detailWidth := width - listWidth - 1
	if detailWidth < 10 {
		detailWidth = 10
	}
	detailStyles := widgets.DetailPaneStyles{
		Title:  styles.Subtitle,
		Body:   styles.Body,
		Border: styles.Border,
		Empty:  styles.Muted,
	}

	s := &HarnessScreen{
		harnesses: harnesses,
		list:      widgets.NewList(items, listHeight, listWidth, listStyles),
		detail:    widgets.NewDetailPane(listHeight, detailWidth, detailStyles),
		width:     width,
		height:    height,
		styles:    styles,
		helpText:  "↑/k up  ↓/j down  enter select  esc back  ctrl+c quit",
	}
	s.updateDetail()
	return s
}

// makeHarnessItems converts the registry list into widget ListItems, annotating each with its
// provision tier and flagging unusable harnesses.
func makeHarnessItems(harnesses []domain.HarnessRef, styles Styles) []widgets.ListItem {
	items := make([]widgets.ListItem, len(harnesses))
	for i, h := range harnesses {
		tier := tierLabel(h.Tier)
		label := h.DisplayName
		if label == "" {
			label = h.ID
		}

		var detail strings.Builder
		fmt.Fprintf(&detail, "ID: %s\n", h.ID)
		fmt.Fprintf(&detail, "Tier: %s\n", tier)
		if h.SourcePath != "" {
			fmt.Fprintf(&detail, "Source: %s\n", h.SourcePath)
		}
		if h.Tier == domain.TierExternal {
			if h.ExecutablePath != "" {
				fmt.Fprintf(&detail, "Module: %s\n", h.ExecutablePath)
			}
		}
		if !h.Usable && h.UnusableReason != "" {
			fmt.Fprintf(&detail, "\nUnavailable: %s\n", h.UnusableReason)
		}

		// The label shown in the list includes the tier badge and an external-module indicator.
		// The indicator is only shown when the module is opted-in and usable; an external
		// harness that failed to load or is not opted-in is not active.
		displayLabel := fmt.Sprintf("%-30s  %s", label, tier)
		if h.Tier == domain.TierExternal && h.Usable {
			displayLabel = fmt.Sprintf("%-30s  %s  [active external module]", label, tier)
		}

		items[i] = widgets.ListItem{
			ID:             h.ID,
			Label:          displayLabel,
			Description:    tier,
			Detail:         detail.String(),
			Disabled:       !h.Usable,
			DisabledReason: h.UnusableReason,
		}
	}
	return items
}

// tierLabel returns a short display string for the provision tier.
func tierLabel(tier domain.ProvisionTier) string {
	switch tier {
	case domain.TierBuiltin:
		return "[built-in]"
	case domain.TierDescriptor:
		return "[descriptor]"
	case domain.TierExternal:
		return "[external]"
	default:
		return fmt.Sprintf("[%s]", tier)
	}
}

// SetToolVersion sets the tool version string displayed in the screen title.
// Call before the first View() for the version to appear on entry.
func (s *HarnessScreen) SetToolVersion(v string) {
	s.toolVersion = v
}

// Update processes a key message and delegates to the list widget.
func (s *HarnessScreen) Update(msg tea.Msg) tea.Cmd {
	cmd := s.list.Update(msg)
	if !s.list.Done() && !s.list.Back() {
		s.updateDetail()
	}
	return cmd
}

// updateDetail refreshes the detail pane to show the currently highlighted harness.
func (s *HarnessScreen) updateDetail() {
	item, ok := s.list.SelectedItem()
	if !ok {
		s.detail.SetContent("", "")
		return
	}
	s.detail.SetContent(item.Label, item.Detail)
}

// View renders the full screen: title, list+detail side by side, status, and help bar.
func (s *HarnessScreen) View() string {
	titleText := "Select Harness"
	if s.toolVersion != "" {
		titleText = "MOSAIC Deploy v" + s.toolVersion + " -- Select Harness"
	}
	title := s.styles.Title.Width(s.width).Render(titleText)
	border := s.styles.Border.Width(s.width).Render(strings.Repeat("─", s.width))

	listView := s.list.View()
	detailView := s.detail.View()

	// Side-by-side layout.
	listWidth := s.width * 2 / 3
	if listWidth < 30 {
		listWidth = s.width
	}
	sep := s.styles.Border.Render("│")
	content := lipgloss.JoinHorizontal(
		lipgloss.Top,
		lipgloss.NewStyle().Width(listWidth).Render(listView),
		sep,
		detailView,
	)

	help := s.styles.Help.Width(s.width).Render(s.helpText)

	return strings.Join([]string{title, border, content, border, help}, "\n")
}

// Done reports whether the user selected a harness.
func (s *HarnessScreen) Done() bool { return s.list.Done() }

// Back reports whether the user pressed back.
func (s *HarnessScreen) Back() bool { return s.list.Back() }

// SelectedID returns the ID of the selected harness.
func (s *HarnessScreen) SelectedID() string { return s.list.SelectedID() }

// Resize updates the screen dimensions.
func (s *HarnessScreen) Resize(width, height int) {
	s.width = width
	s.height = height
	listHeight := height - 8
	if listHeight < 3 {
		listHeight = 3
	}
	listWidth := width * 2 / 3
	if listWidth < 30 {
		listWidth = width
	}
	detailWidth := width - listWidth - 1
	if detailWidth < 10 {
		detailWidth = 10
	}
	s.list.Resize(listHeight, listWidth)
	s.detail.Resize(listHeight, detailWidth)
}

// Reset clears the done and back flags.
func (s *HarnessScreen) Reset() { s.list.Reset() }
