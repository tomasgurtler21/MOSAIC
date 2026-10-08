// Package pastesafe provides a single-line text entry that a paste can never
// submit: all pasted text is kept and each pasted line break becomes one space.
//
// Design decision: timing heuristic. A strict mechanism is not easily
// achievable. On Windows, bubbletea delivers a paste as individual key events
// with the Paste flag never set: a pasted CR arrives as KeyEnter,
// indistinguishable from a typed Enter, and a pasted LF arrives as KeyCtrlJ.
// Only ANSI terminals with bracketed paste mark keys with Paste.
//
// Detection rule: a key with the Paste flag is always pasted text, independent
// of timing. For unmarked keys, a KeyEnter that arrives within PasteWindow
// (inclusive) after the previous key event seen by the same Field is pasted: it
// is replaced by a space and the field stays open; the user presses Enter again
// to submit. An Enter after the window submits. A multi-rune KeyRunes message
// containing CR or LF is pasted text as well.
//
// Window: PasteWindow is chosen with margin for render latency. The Field sees
// key events when Update runs, not when they arrived, and the program may
// render between two pasted keys, so a slow terminal or a long paste stretches
// the observed gap. The window is therefore clearly larger than one render of a
// typical screen, while still shorter than a human pause before pressing Enter.
//
// Line break rule: one line break is one space, whatever its encoding. A CR
// followed by LF (KeyEnter then KeyCtrlJ within the window, or CR+LF inside the
// Runes of a message) yields one space. A lone CR or lone LF yields one space.
// KeyCtrlJ never submits: right after a pasted CR it completes that CR+LF and
// adds nothing, otherwise (also outside the window) it adds one space.
//
// Known edge cases (accepted cost): an Enter right after SetValue or WithValue
// has no preceding key event and submits; a paste whose very first key event is
// a line break has no preceding key in the window and submits.
//
// Clock contract: the Field never reads wall time directly. It reads a Clock
// once per key message and decides synchronously inside Update; it starts no
// timers and returns no tea.Cmd for paste detection, so parent models forward
// nothing for it. The default clock is the process-wide clock (the wall clock
// unless replaced with SetClock); WithClock or Field.SetClock overrides it per
// instance.
package pastesafe

import (
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

// PasteWindow is the paste-detection window. An unmarked Enter that arrives
// within PasteWindow after the previous key event seen by the same Field is
// pasted.
const PasteWindow time.Duration = 80 * time.Millisecond

// Clock returns the current time as seen by paste detection.
type Clock func() time.Time

// Option configures a Field at construction.
type Option func(*Field)

// Field is a single-line, paste-safe text entry.
type Field struct {
	model      textinput.Model
	validateFn func(string) error
	filter     func(rune) bool
	clock      Clock
	errMsg     string
	done       bool
	back       bool

	hasLast   bool      // a key event precedes the current one
	lastKey   time.Time // time of the previous key event
	pendingCR bool      // the previous key event was a pasted CR
}

// NewField returns a focused Field.
func NewField(opts ...Option) *Field {
	f := &Field{model: textinput.New()}
	f.model.Focus()
	for _, o := range opts {
		o(f)
	}
	return f
}

// WithPlaceholder sets the placeholder text.
func WithPlaceholder(p string) Option { return func(f *Field) { f.model.Placeholder = p } }

// WithValue pre-fills the value.
func WithValue(v string) Option { return func(f *Field) { f.SetValue(v) } }

// WithValidate sets the submit validator.
func WithValidate(fn func(string) error) Option { return func(f *Field) { f.validateFn = fn } }

// WithClock sets a per-instance clock.
func WithClock(c Clock) Option { return func(f *Field) { f.clock = c } }

// WithRuneFilter keeps only runes for which accept returns true.
func WithRuneFilter(accept func(rune) bool) Option { return func(f *Field) { f.filter = accept } }

// Init returns the blink command.
func (f *Field) Init() tea.Cmd { return textinput.Blink }

// Update processes a message. It never returns a paste-detection command.
func (f *Field) Update(msg tea.Msg) tea.Cmd {
	km, ok := msg.(tea.KeyMsg)
	if !ok {
		var cmd tea.Cmd
		f.model, cmd = f.model.Update(msg)
		return cmd
	}

	inBurst := f.observe()
	afterCR := f.pendingCR
	f.pendingCR = false

	switch {
	case km.Type == tea.KeyEsc:
		f.back = true
		return nil
	case km.Type == tea.KeyEnter:
		if km.Paste || inBurst {
			f.pendingCR = true
			f.insert(" ")
			return nil
		}
		f.submit()
		return nil
	case km.Type == tea.KeyCtrlJ:
		if !(afterCR && inBurst) {
			f.insert(" ")
		}
		return nil
	case km.Type == tea.KeyRunes && (km.Paste || len(km.Runes) > 1 || hasLineBreak(km.Runes)):
		f.insert(string(km.Runes))
		return nil
	case km.Type == tea.KeyRunes:
		if !f.accepts(km.Runes) {
			return nil
		}
	}

	var cmd tea.Cmd
	f.model, cmd = f.model.Update(msg)
	return cmd
}

// observe reads the clock once, reports whether the key arrived within the
// paste window of the previous key event, and records the key time.
func (f *Field) observe() bool {
	clock := f.clock
	if clock == nil {
		clock = currentProcessClock()
	}
	now := clock()
	inBurst := f.hasLast && now.Sub(f.lastKey) <= PasteWindow
	f.lastKey = now
	f.hasLast = true
	return inBurst
}

func (f *Field) submit() {
	if f.validateFn != nil {
		if err := f.validateFn(f.model.Value()); err != nil {
			f.errMsg = err.Error()
			return
		}
	}
	f.errMsg = ""
	f.done = true
}

// insert adds text at the cursor, replacing each line break (CR+LF counted as
// one) with a space and dropping runes rejected by the rune filter.
func (f *Field) insert(s string) {
	in := []rune(s)
	out := make([]rune, 0, len(in))
	for i := 0; i < len(in); i++ {
		r := in[i]
		if r == '\r' && i+1 < len(in) && in[i+1] == '\n' {
			i++
			r = ' '
		} else if r == '\r' || r == '\n' {
			r = ' '
		}
		if f.filter == nil || f.filter(r) {
			out = append(out, r)
		}
	}
	if len(out) == 0 {
		return
	}
	f.model, _ = f.model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: out})
}

func (f *Field) accepts(rs []rune) bool {
	if f.filter == nil {
		return true
	}
	for _, r := range rs {
		if !f.filter(r) {
			return false
		}
	}
	return true
}

func hasLineBreak(rs []rune) bool {
	for _, r := range rs {
		if r == '\r' || r == '\n' {
			return true
		}
	}
	return false
}

// View renders the input line.
func (f *Field) View() string { return f.model.View() }

// Value returns the current text.
func (f *Field) Value() string { return f.model.Value() }

// SetValue replaces the value. The next Enter has no preceding key event.
func (f *Field) SetValue(v string) {
	f.model.SetValue(v)
	f.hasLast = false
	f.pendingCR = false
}

// SetValidate sets the submit validator.
func (f *Field) SetValidate(fn func(string) error) { f.validateFn = fn }

// SetClock sets a per-instance clock (nil = process-wide clock).
func (f *Field) SetClock(c Clock) { f.clock = c }

// Done reports whether a submit passed validation.
func (f *Field) Done() bool { return f.done }

// Back reports whether Esc was pressed.
func (f *Field) Back() bool { return f.back }

// ErrMsg returns the last validation error.
func (f *Field) ErrMsg() string { return f.errMsg }

// Reset clears Done, Back and ErrMsg; keeps the value.
func (f *Field) Reset() {
	f.done = false
	f.back = false
	f.errMsg = ""
}
