package screens

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"mosaic-log-analyzer/internal/app"
	"mosaic-log-analyzer/internal/domain"
)

// PricingScreen lists every model in the current report with its priced /
// unpriced state, and lets the user select one to set or overwrite its price.
// It also still serves as the background context while pricing questions
// arrive through the Interaction port.
//
// Navigation contract:
//   - ↑/↓ (and k/j) move the cursor.
//   - Enter -> Done() == true; SelectedModel() returns the chosen model.
//   - Esc/q -> Back() == true.
type PricingScreen struct {
	models     []app.ModelPricingStatus
	configPath string
	statusMsg  string
	cursor     int
	width      int
	height     int
	styles     Styles
	done       bool
	back       bool
}

// NewPricingScreen creates the pricing screen over the given per-model pricing
// states. An empty or nil slice is valid and renders without panic.
func NewPricingScreen(models []app.ModelPricingStatus, configPath string, width, height int, styles Styles) *PricingScreen {
	return &PricingScreen{
		models:     models,
		configPath: configPath,
		width:      width,
		height:     height,
		styles:     styles,
	}
}

// SetModels replaces the model list (used after a reprice without re-navigating).
// The cursor is clamped into range.
func (s *PricingScreen) SetModels(models []app.ModelPricingStatus) {
	s.models = models
	if len(models) == 0 {
		s.cursor = 0
	} else if s.cursor >= len(models) {
		s.cursor = len(models) - 1
	}
}

// ID implements Screen.
func (s *PricingScreen) ID() ScreenID { return ScreenPricing }

// Init implements Screen.
func (s *PricingScreen) Init() tea.Cmd { return nil }

// SetStatus updates the status line shown below the model list. Used to
// confirm that a price was written.
func (s *PricingScreen) SetStatus(msg string) { s.statusMsg = msg }

// Done reports whether the user chose a model with Enter.
func (s *PricingScreen) Done() bool { return s.done }

// SelectedModel returns the highlighted model. Only meaningful when Done() is
// true; returns "" when the list is empty or the cursor is out of range.
func (s *PricingScreen) SelectedModel() domain.ModelID {
	if len(s.models) == 0 || s.cursor < 0 || s.cursor >= len(s.models) {
		return ""
	}
	return s.models[s.cursor].Model
}

// Back reports whether the user pressed Esc.
func (s *PricingScreen) Back() bool { return s.back }

// Reset clears the done and back flags.
func (s *PricingScreen) Reset() {
	s.done = false
	s.back = false
}

// Update processes key messages.
func (s *PricingScreen) Update(msg tea.Msg) (Screen, tea.Cmd) {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return s, nil
	}
	switch key.String() {
	case "esc", "q":
		s.back = true
	case "up", "k":
		if s.cursor > 0 {
			s.cursor--
		}
	case "down", "j":
		if s.cursor < len(s.models)-1 {
			s.cursor++
		}
	case "enter":
		if len(s.models) > 0 {
			s.done = true
		}
	}
	return s, nil
}

// View renders the pricing screen. Stub: shows model names; priced/unpriced
// distinction and full selectable-list rendering are added in I5.1.
func (s *PricingScreen) View(width, height int) string {
	s.width = width
	s.height = height

	var sb strings.Builder
	sep := s.styles.Border.Width(width).Render(strings.Repeat("─", width))

	sb.WriteString(s.styles.Title.Width(width).Render("Log Analyzer — Pricing"))
	sb.WriteByte('\n')
	sb.WriteString(s.styles.Subtitle.Width(width).Render(
		fmt.Sprintf("%d model(s) in report", len(s.models)),
	))
	sb.WriteByte('\n')
	sb.WriteString(sep)
	sb.WriteByte('\n')
	sb.WriteByte('\n')

	// List models with priced/unpriced distinction.
	for i, m := range s.models {
		prefix := "  "
		if i == s.cursor {
			prefix = "> "
		}
		var status string
		if m.Priced {
			status = "  (priced)"
		} else {
			status = "  " + AbsentMarker + " unpriced"
		}
		sb.WriteString(s.styles.Body.Width(width).Render(prefix + string(m.Model) + status))
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
	sb.WriteString(s.styles.Help.Width(width).Render("↑/↓ select  enter price  esc back  ctrl+c quit"))

	return sb.String()
}

// Help returns the key hints for this screen.
func (s *PricingScreen) Help() string {
	return "↑/↓ select  enter price  esc back  ctrl+c quit"
}
