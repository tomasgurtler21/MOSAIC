package widgets_test

import (
	"os"
	"testing"

	"mosaic-common/tui/pastesafe"
)

// TestMain makes every key event look like a separately typed key, so tests that type
// text and press Enter in the same instant are not read as a paste.
func TestMain(m *testing.M) {
	restore := pastesafe.SetClock(pastesafe.SeparateKeysClock())
	code := m.Run()
	restore()
	os.Exit(code)
}
