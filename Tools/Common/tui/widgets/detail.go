package widgets

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// DetailPaneStyles holds the resolved styles for a DetailPane.
type DetailPaneStyles struct {
	Title  lipgloss.Style
	Body   lipgloss.Style
	Border lipgloss.Style
	Empty  lipgloss.Style
}

// DefaultDetailPaneStyles returns a plain style set usable in tests.
func DefaultDetailPaneStyles() DetailPaneStyles {
	return DetailPaneStyles{
		Title:  lipgloss.NewStyle().Bold(true),
		Body:   lipgloss.NewStyle(),
		Border: lipgloss.NewStyle(),
		Empty:  lipgloss.NewStyle(),
	}
}

// DetailPane renders a scrollable text pane alongside a list. The content is set via
// SetContent; the pane does not own navigation state.
type DetailPane struct {
	title   string
	content string
	offset  int // first visible line (scroll position)
	height  int
	width   int
	styles  DetailPaneStyles
}

// NewDetailPane creates a DetailPane with the given dimensions.
func NewDetailPane(height, width int, styles DetailPaneStyles) *DetailPane {
	return &DetailPane{
		height: height,
		width:  width,
		styles: styles,
	}
}

// SetContent updates the title and body text. Lines exceeding the width are wrapped by
// lipgloss when rendered.
func (d *DetailPane) SetContent(title, body string) {
	d.title = title
	d.content = body
	d.offset = 0 // reset scroll when content changes
}

// View renders the visible slice of the pane content.
func (d *DetailPane) View() string {
	if d.content == "" && d.title == "" {
		return d.styles.Empty.Width(d.width).Height(d.height).Render("(nothing selected)")
	}

	var lines []string
	if d.title != "" {
		titleLines := strings.Split(d.styles.Title.Width(d.width).Render(d.title), "\n")
		lines = append(lines, titleLines...)
	}
	if d.content != "" {
		bodyLines := strings.Split(d.styles.Body.Width(d.width).Render(d.content), "\n")
		lines = append(lines, bodyLines...)
	}

	// Apply scroll offset.
	if d.offset >= len(lines) {
		d.offset = 0
	}
	visible := lines[d.offset:]
	if len(visible) > d.height {
		visible = visible[:d.height]
	}

	// Pad to full height.
	for len(visible) < d.height {
		visible = append(visible, strings.Repeat(" ", d.width))
	}

	return strings.Join(visible, "\n")
}

// ScrollDown scrolls the content down by one line.
func (d *DetailPane) ScrollDown() {
	lines := d.totalLines()
	if d.offset < lines-d.height {
		d.offset++
	}
}

// ScrollUp scrolls the content up by one line.
func (d *DetailPane) ScrollUp() {
	if d.offset > 0 {
		d.offset--
	}
}

// totalLines returns the number of visual lines the pane's content occupies
// after lipgloss rendering. When the pane has a positive width, lipgloss
// word-wraps the text, so the visual line count can exceed the raw newline
// count. Width-zero mode (no-wrap) counts raw newlines, matching the
// historical behaviour.
func (d *DetailPane) totalLines() int {
	if d.width <= 0 {
		count := 0
		if d.title != "" {
			count += len(strings.Split(d.title, "\n"))
		}
		if d.content != "" {
			count += len(strings.Split(d.content, "\n"))
		}
		return count
	}
	// Positive width: count wrapped visual lines produced by the same render
	// path View() uses, so scroll bounds are derived from the rendered output.
	count := 0
	if d.title != "" {
		rendered := d.styles.Title.Width(d.width).Render(d.title)
		count += len(strings.Split(rendered, "\n"))
	}
	if d.content != "" {
		rendered := d.styles.Body.Width(d.width).Render(d.content)
		count += len(strings.Split(rendered, "\n"))
	}
	return count
}

// Resize updates the visible height and render width. The scroll offset is
// clamped to the last valid position for the new layout rather than being
// reset to zero: content that was visible before a resize remains visible
// when the new layout still has room for it.
func (d *DetailPane) Resize(height, width int) {
	d.height = height
	d.width = width
	maxOffset := d.totalLines() - height
	if maxOffset < 0 {
		maxOffset = 0
	}
	if d.offset > maxOffset {
		d.offset = maxOffset
	}
}
