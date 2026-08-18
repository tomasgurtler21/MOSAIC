package screens

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"mosaic-log-analyzer/internal/domain"
)

// NoDataScreen renders the explicit no-data state: a usable source that contains
// no run data. It distinguishes this from an error and from an unusable path.
//
// Navigation contract:
//   - 's' or Enter -> Done() == true (user wants to choose a different source).
//   - Esc or 'q'   -> Back() == true (user wants to quit).
type NoDataScreen struct {
	source domain.Source
	width  int
	height int
	styles Styles
	done   bool
	back   bool
}

// NewNoDataScreen creates the no-data state screen for the given source.
func NewNoDataScreen(source domain.Source, width, height int, styles Styles) *NoDataScreen {
	return &NoDataScreen{
		source: source,
		width:  width,
		height: height,
		styles: styles,
	}
}

// ID implements Screen.
func (s *NoDataScreen) ID() ScreenID { return ScreenNoData }

// Init implements Screen (no async work required).
func (s *NoDataScreen) Init() tea.Cmd { return nil }

// Update processes key messages for navigation.
func (s *NoDataScreen) Update(msg tea.Msg) (Screen, tea.Cmd) {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return s, nil
	}
	switch key.String() {
	case "s", "enter":
		s.done = true
	case "esc", "q":
		s.back = true
	}
	return s, nil
}

// View renders the no-data state.
func (s *NoDataScreen) View(width, height int) string {
	s.width = width
	s.height = height

	sep := s.styles.Border.Width(width).Render(strings.Repeat("─", width))
	title := s.styles.Title.Width(width).Render("Log Analyzer — No Data Found")
	subtitle := s.styles.Subtitle.Width(width).Render("The source exists but contains no run data.")

	pathLine := s.styles.Body.Width(width).Render("Source: " + s.source.Path)
	message := s.styles.Muted.Width(width).Render(
		"The logs directory is empty — no run folders or unattributable bucket were found.\n" +
			"This is a normal state when the tool has not yet been run against this directory.",
	)

	help := s.styles.Help.Width(width).Render("s choose a different source  esc quit  ctrl+c quit")

	return strings.Join([]string{
		title,
		subtitle,
		sep,
		"",
		pathLine,
		"",
		message,
		"",
		sep,
		help,
	}, "\n")
}

// Help returns the key hints for this screen.
func (s *NoDataScreen) Help() string {
	return "s choose a different source  esc quit  ctrl+c quit"
}

// Done reports whether the user wants to choose a different source.
func (s *NoDataScreen) Done() bool { return s.done }

// Back reports whether the user wants to quit.
func (s *NoDataScreen) Back() bool { return s.back }

// Reset clears the done and back flags.
func (s *NoDataScreen) Reset() {
	s.done = false
	s.back = false
}
