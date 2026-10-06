package tui

import (
	"fmt"
	"runtime/debug"

	tea "github.com/charmbracelet/bubbletea"

	"mosaic-run/internal/domain"
)

// maxPanicErrorStack bounds the stack text carried in the user-facing error.
// The debug log always receives the full stack.
const maxPanicErrorStack = 4096

// guardCmd returns a tea.Cmd that runs cmd and, if cmd panics, recovers,
// records the panic value and full stack in debug as a runner.error entry and
// returns onPanic(err) as the command's message instead of re-panicking.
// site is a short label naming the work ("session", "test run"). When cmd does
// not panic, its message is returned unchanged.
//
// Bubble Tea recovers panics in command goroutines too, but it tears the
// program down; this boundary turns the panic into a message the model handles.
func guardCmd(debugLog domain.DebugLogger, site string, onPanic func(err error) tea.Msg, cmd tea.Cmd) tea.Cmd {
	return func() (msg tea.Msg) {
		defer func() {
			p := recover()
			if p == nil {
				return
			}
			full := debug.Stack()
			if debugLog != nil {
				debugLog.Log(domain.EventRunnerError,
					fmt.Sprintf("panic in %s: %v\n%s", site, p, full),
					domain.F("site", site))
			}
			short := full
			if len(short) > maxPanicErrorStack {
				short = short[:maxPanicErrorStack]
			}
			msg = onPanic(fmt.Errorf("panic in %s: %v\n%s", site, p, short))
		}()
		return cmd()
	}
}
