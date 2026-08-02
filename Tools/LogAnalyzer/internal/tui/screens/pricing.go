package screens

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"mosaic-log-analyzer/internal/domain"
)

// PricingScreen shows the list of models that have no pricing entry and
// displays the config file path. It serves as the background context while
// pricing questions arrive through the Interaction port.
//
// Navigation contract:
//   - Esc -> Back() == true.
type PricingScreen struct {
	models     []domain.ModelID
	configPath string
	statusMsg  string
	width      int
	height     int
	styles     Styles
	back       bool
}

// NewPricingScreen creates the pricing-entry background screen.
func NewPricingScreen(models []domain.ModelID, configPath string, width, height int, styles Styles) *PricingScreen {
	return &PricingScreen{
		models:     models,
		configPath: configPath,
		width:      width,
		height:     height,
		styles:     styles,
	}
}

// ID implements Screen.
func (s *PricingScreen) ID() ScreenID { return ScreenPricing }

// Init implements Screen.
func (s *PricingScreen) Init() tea.Cmd { return nil }

// SetStatus updates the status line shown below the model list.
func (s *PricingScreen) SetStatus(msg string) { s.statusMsg = msg }

// Update processes key messages.
func (s *PricingScreen) Update(msg tea.Msg) (Screen, tea.Cmd) {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return s, nil
	}
	switch key.String() {
	case "esc", "q":
		s.back = true
	}
	return s, nil
}

// View renders the pricing-entry background screen.
func (s *PricingScreen) View(width, height int) string {
	s.width = width
	s.height = height

	var sb strings.Builder
	sep := s.styles.Border.Width(width).Render(strings.Repeat("─", width))

	sb.WriteString(s.styles.Title.Width(width).Render("Log Analyzer — Pricing Entry"))
	sb.WriteByte('\n')
	sb.WriteString(s.styles.Subtitle.Width(width).Render(
		fmt.Sprintf("%d model(s) have no pricing entry", len(s.models)),
	))
	sb.WriteByte('\n')
	sb.WriteString(sep)
	sb.WriteByte('\n')
	sb.WriteByte('\n')

	// List of unpriced models.
	for _, m := range s.models {
		sb.WriteString(s.styles.Body.Width(width).Render("  • " + string(m)))
		sb.WriteByte('\n')
	}

	if s.configPath != "" {
		sb.WriteByte('\n')
		sb.WriteString(s.styles.Muted.Width(width).Render("Config file: " + s.configPath))
		sb.WriteByte('\n')
	}

	if s.statusMsg != "" {
		sb.WriteByte('\n')
		sb.WriteString(s.styles.Success.Width(width).Render(s.statusMsg))
		sb.WriteByte('\n')
	}

	sb.WriteString(sep)
	sb.WriteByte('\n')
	sb.WriteString(s.styles.Help.Width(width).Render("Responding to pricing prompts…  esc skip  ctrl+c quit"))

	return sb.String()
}

// Help returns the key hints for this screen.
func (s *PricingScreen) Help() string {
	return "Responding to pricing prompts…  esc skip  ctrl+c quit"
}

// Back reports whether the user pressed Esc.
func (s *PricingScreen) Back() bool { return s.back }

// Reset clears the back flag.
func (s *PricingScreen) Reset() { s.back = false }
