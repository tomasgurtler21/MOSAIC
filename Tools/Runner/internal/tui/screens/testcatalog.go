package screens

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"mosaic-common/tui/widgets"
)

// TestCatalogScreen prompts the user to enter the MOSAIC repo root path for
// test mode. It validates that Tools/Runner/TestCatalog/ exists under the
// entered path before confirming.
//
// Navigation contract:
//   - Enter on a valid path -> Done() == true, Value() returns the normalized path.
//   - Esc -> Back() == true.
type TestCatalogScreen struct {
	input  *widgets.TextInput
	width  int
	height int
	styles Styles
}

// NewTestCatalogScreen creates the catalog source path input screen.
// Follows the OrchestratorFileScreen pattern from setup.go.
func NewTestCatalogScreen(width, height int, styles Styles) *TestCatalogScreen {
	inputStyles := widgets.TextInputStyles{
		Label:  styles.Subtitle,
		Input:  styles.Body,
		ErrMsg: styles.Error,
	}
	input := widgets.NewTextInput(
		"MOSAIC repo root path:",
		"/path/to/MOSAIC",
		width,
		inputStyles,
	)
	input.SetValidate(validateTestCatalogPath)
	return &TestCatalogScreen{
		input:  input,
		width:  width,
		height: height,
		styles: styles,
	}
}

// validateTestCatalogPath checks that Tools/Runner/TestCatalog/ exists under
// the entered path. The same normalization rules as validateOrchestratorFile
// apply: whitespace is trimmed and a matched outer pair of double quotes is
// stripped.
func validateTestCatalogPath(raw string) error {
	path := normalizePath(raw)
	if path == "" {
		return fmt.Errorf("path cannot be empty")
	}
	catalogDir := filepath.Join(path, "Tools", "Runner", "TestCatalog")
	if _, err := os.Stat(catalogDir); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("Tools/Runner/TestCatalog/ not found under %s", path)
		}
		return fmt.Errorf("cannot access Tools/Runner/TestCatalog/: %v", err)
	}
	return nil
}

// Update processes a key message and delegates to the text input widget.
func (s *TestCatalogScreen) Update(msg tea.Msg) tea.Cmd {
	return s.input.Update(msg)
}

// View renders the catalog path entry screen.
func (s *TestCatalogScreen) View() string {
	title := s.styles.Title.Width(s.width).Render("Test Catalog Path")
	subtitle := s.styles.Subtitle.Width(s.width).Render("Enter the path to the MOSAIC repo root.")
	border := s.styles.Border.Width(s.width).Render(strings.Repeat("─", s.width))
	guidance := s.styles.Muted.Width(s.width).Render("The directory must contain Tools/Runner/TestCatalog/.\n")
	inputView := s.input.View()
	help := s.styles.Help.Width(s.width).Render("enter confirm  esc back  ctrl+c quit")
	return strings.Join([]string{title, subtitle, border, guidance, inputView, border, help}, "\n")
}

// Done reports whether the user confirmed a valid path.
func (s *TestCatalogScreen) Done() bool { return s.input.Done() }

// Back reports whether the user pressed Esc.
func (s *TestCatalogScreen) Back() bool { return s.input.Back() }

// Value returns the validated MOSAIC repo root path. Only valid when Done() is true.
func (s *TestCatalogScreen) Value() string {
	return normalizePath(s.input.Value())
}

// Reset clears the done and back flags.
func (s *TestCatalogScreen) Reset() { s.input.Reset() }

// InputInit returns the command required to start cursor blinking.
func (s *TestCatalogScreen) InputInit() tea.Cmd { return s.input.Init() }

// Resize updates the screen dimensions.
func (s *TestCatalogScreen) Resize(width, height int) {
	s.width = width
	s.height = height
	s.input.Resize(width)
}
