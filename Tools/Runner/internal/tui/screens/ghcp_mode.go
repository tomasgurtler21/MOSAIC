package screens

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// GHCPCLIModeChoice classifies the user's GHCP CLI permission-mode selection.
type GHCPCLIModeChoice string

const (
	// GHCPCLIModeBlanket corresponds to --yolo: all tools, paths, and URLs are
	// permitted for the spawned process.
	GHCPCLIModeBlanket GHCPCLIModeChoice = "blanket"

	// GHCPCLIModeAllowlist corresponds to per-tool --allow-tool entries derived
	// from the agent's deployed tools frontmatter. Note: agent/skill tools are
	// passed through but have no gating effect on the installed GHCP CLI.
	GHCPCLIModeAllowlist GHCPCLIModeChoice = "allowlist"
)

// ghcpModeOptions lists the two choices in cursor order.
var ghcpModeOptions = []GHCPCLIModeChoice{
	GHCPCLIModeBlanket,
	GHCPCLIModeAllowlist,
}

// GHCPCLIModeScreen presents the two GHCP CLI permission-mode choices to the
// user before any GHCP CLI process is spawned. It is shown only when the
// selected harness is ghcp-cli; other harnesses skip it.
//
// Navigation contract:
//   - Enter on "Blanket"           -> Done() == true, Mode() == GHCPCLIModeBlanket.
//   - Enter on "Partial Allowlist" -> Done() == true, Mode() == GHCPCLIModeAllowlist.
//   - Esc                          -> Back() == true.
type GHCPCLIModeScreen struct {
	cursor int
	done   bool
	back   bool
	width  int
	height int
	styles Styles
}

// NewGHCPCLIModeScreen creates a GHCP CLI permission-mode selection screen.
func NewGHCPCLIModeScreen(width, height int, styles Styles) *GHCPCLIModeScreen {
	return &GHCPCLIModeScreen{
		cursor: 0, // Blanket is selected by default
		width:  width,
		height: height,
		styles: styles,
	}
}

// Update processes key messages for the GHCP CLI mode selection screen.
func (s *GHCPCLIModeScreen) Update(msg tea.Msg) tea.Cmd {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return nil
	}
	switch keyMsg.String() {
	case "up", "k":
		if s.cursor > 0 {
			s.cursor--
		}
	case "down", "j":
		if s.cursor < len(ghcpModeOptions)-1 {
			s.cursor++
		}
	case "enter":
		s.done = true
	case "esc":
		s.back = true
	}
	return nil
}

// View renders the GHCP CLI mode selection screen with warning text.
func (s *GHCPCLIModeScreen) View() string {
	title := s.styles.Title.Width(s.width).Render("GHCP CLI Permission Mode")
	border := s.styles.Border.Width(s.width).Render(strings.Repeat("─", s.width))

	subtitle := s.styles.Subtitle.Width(s.width).Render(
		"Select the permission strategy for this GHCP CLI run.",
	)

	optionLabels := map[GHCPCLIModeChoice]string{
		GHCPCLIModeBlanket:   "Blanket — grant all tool, path, and URL permissions (--yolo)",
		GHCPCLIModeAllowlist: "Partial Allowlist — restrict to per-tool --allow-tool entries from the agent's deployed tools frontmatter",
	}

	var optionsBuilder strings.Builder
	for i, choice := range ghcpModeOptions {
		label := optionLabels[choice]
		if i == s.cursor {
			optionsBuilder.WriteString("▶ " + s.styles.Selected.Render(label) + "\n")
		} else {
			optionsBuilder.WriteString("  " + s.styles.Body.Render(label) + "\n")
		}
	}

	warningText := strings.Join([]string{
		"Warnings:",
		"  • Partial Allowlist cannot gate agent/skill tools: they are passed through",
		"    to the installed GHCP CLI but have no gating effect on it.",
		"  • Agents declaring read/search tools are unaffected by mode choice: GHCP CLI",
		"    auto-allows read and search operations regardless of --allow-tool entries.",
	}, "\n")
	warning := s.styles.Warning.Width(s.width).Render(warningText)

	help := s.styles.Help.Width(s.width).Render("↑/k up  ↓/j down  enter select  esc back  ctrl+c quit")
	return strings.Join([]string{title, subtitle, border, optionsBuilder.String(), warning, border, help}, "\n")
}

// Done reports whether the user confirmed a mode choice.
func (s *GHCPCLIModeScreen) Done() bool { return s.done }

// Back reports whether the user pressed Esc.
func (s *GHCPCLIModeScreen) Back() bool { return s.back }

// Mode returns the selected GHCP CLI permission mode. Only valid when Done() is true.
func (s *GHCPCLIModeScreen) Mode() GHCPCLIModeChoice {
	if s.cursor >= 0 && s.cursor < len(ghcpModeOptions) {
		return ghcpModeOptions[s.cursor]
	}
	return GHCPCLIModeBlanket
}

// Reset clears the Done and Back flags and resets the cursor.
func (s *GHCPCLIModeScreen) Reset() {
	s.done = false
	s.back = false
	s.cursor = 0
}

// Resize updates the screen dimensions.
func (s *GHCPCLIModeScreen) Resize(width, height int) {
	s.width = width
	s.height = height
}
