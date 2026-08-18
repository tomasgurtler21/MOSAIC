package screens

import (
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// ArtifactScreen shows the raw content of the Orchestration.md artifact.
// It is navigable with arrow keys and dismissed with Esc or 'q'.
type ArtifactScreen struct {
	content  string
	lines    []string
	offset   int  // first visible line
	done     bool // user pressed Esc or 'q'
	width    int
	height   int
	styles   Styles
}

// NewArtifactScreen creates a read-only artifact inspection screen.
func NewArtifactScreen(content string, width, height int, styles Styles) *ArtifactScreen {
	lines := strings.Split(content, "\n")
	return &ArtifactScreen{
		content: content,
		lines:   lines,
		width:   width,
		height:  height,
		styles:  styles,
	}
}

// SetContent updates the displayed content.
func (s *ArtifactScreen) SetContent(content string) {
	s.content = content
	s.lines = strings.Split(content, "\n")
	if s.offset >= len(s.lines) {
		s.offset = 0
	}
}

// Update processes a key message.
func (s *ArtifactScreen) Update(msg tea.Msg) tea.Cmd {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return nil
	}
	switch keyMsg.String() {
	case "up", "k":
		if s.offset > 0 {
			s.offset--
		}
	case "down", "j":
		maxOffset := len(s.lines) - s.contentHeight()
		if maxOffset < 0 {
			maxOffset = 0
		}
		if s.offset < maxOffset {
			s.offset++
		}
	case "pgup":
		s.offset -= s.contentHeight()
		if s.offset < 0 {
			s.offset = 0
		}
	case "pgdown":
		s.offset += s.contentHeight()
		maxOffset := len(s.lines) - s.contentHeight()
		if maxOffset < 0 {
			maxOffset = 0
		}
		if s.offset > maxOffset {
			s.offset = maxOffset
		}
	case "esc", "q":
		s.done = true
	}
	return nil
}

// contentHeight returns the number of lines visible in the content area.
func (s *ArtifactScreen) contentHeight() int {
	h := s.height - 5 // title + border + scrollbar line + border + help
	if h < 1 {
		return 1
	}
	return h
}

// View renders the artifact inspection screen.
func (s *ArtifactScreen) View() string {
	title := s.styles.Title.Width(s.width).Render("Artifact Inspection (read-only)")
	border := s.styles.Border.Width(s.width).Render(strings.Repeat("─", s.width))

	contentH := s.contentHeight()
	end := s.offset + contentH
	if end > len(s.lines) {
		end = len(s.lines)
	}

	var sb strings.Builder
	for i := s.offset; i < end; i++ {
		if i > s.offset {
			sb.WriteByte('\n')
		}
		sb.WriteString(s.styles.Body.Width(s.width).Render(s.lines[i]))
	}

	total := len(s.lines)

	// Status bar showing scroll position.
	statusText := strings.Repeat(" ", s.width)
	if total > 0 {
		pos := s.offset + 1
		statusText = s.styles.Muted.Width(s.width).Render(
			"lines " + strconv.Itoa(pos) + "-" + strconv.Itoa(end) + " of " + strconv.Itoa(total))
	}

	help := s.styles.Help.Width(s.width).Render("↑/k up  ↓/j down  pgup/pgdown  esc/q close")
	return strings.Join([]string{title, border, sb.String(), border, statusText, help}, "\n")
}

// Done reports whether the user closed the artifact view.
func (s *ArtifactScreen) Done() bool { return s.done }

// Reset clears the done flag and resets scroll position.
func (s *ArtifactScreen) Reset() {
	s.done = false
	s.offset = 0
}

// Resize updates the screen dimensions.
func (s *ArtifactScreen) Resize(width, height int) {
	s.width = width
	s.height = height
}
