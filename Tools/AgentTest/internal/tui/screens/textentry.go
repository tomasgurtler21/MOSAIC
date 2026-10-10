package screens

import (
	"unicode"

	tea "github.com/charmbracelet/bubbletea"

	"mosaic-common/tui/pastesafe"
)

// draftEntry adapts the shared paste-safe field to the draft/confirmed model
// the text screens share: the field holds the in-progress draft, and a
// submitted non-empty draft is handed to the owning screen to become its
// confirmed value. All Enter, Esc and paste handling lives in the field.
type draftEntry struct {
	field  *pastesafe.Field
	edited bool // a character was accepted or Backspace pressed since the last reset
}

// newDraftEntry returns an entry accepting every rune.
func newDraftEntry() *draftEntry {
	return &draftEntry{field: pastesafe.NewField()}
}

// newDigitEntry returns an entry accepting only digit runes.
func newDigitEntry() *draftEntry {
	return &draftEntry{field: pastesafe.NewField(pastesafe.WithRuneFilter(unicode.IsDigit))}
}

// entryResult reports what a key message did to the entry.
type entryResult struct {
	submitted bool   // Enter confirmed the entry
	draft     string // the draft at submission; empty when nothing was typed
	back      bool   // Esc was pressed
}

// update feeds a message to the field. A submission or Esc clears the draft.
func (d *draftEntry) update(msg tea.Msg) entryResult {
	before := d.field.Value()
	d.field.Update(msg)
	if key, ok := msg.(tea.KeyMsg); ok && key.Type == tea.KeyBackspace {
		d.edited = true
	}
	if d.field.Value() != before {
		d.edited = true
	}
	var res entryResult
	switch {
	case d.field.Done():
		res = entryResult{submitted: true, draft: d.field.Value()}
		d.clear()
	case d.field.Back():
		res = entryResult{back: true}
		d.clear()
	}
	return res
}

// draft returns the in-progress text.
func (d *draftEntry) draft() string { return d.field.Value() }

// clear discards the draft and the Done/Back flags of the field.
func (d *draftEntry) clear() {
	d.field.SetValue("")
	d.field.Reset()
}

// reset discards the draft and the edited flag.
func (d *draftEntry) reset() {
	d.clear()
	d.edited = false
}

// newKeyGate returns a field that accepts no text and only provides the
// paste-safe Enter (Done) and Esc (Back) handling for screens that take key
// commands instead of typed text.
func newKeyGate() *pastesafe.Field {
	return pastesafe.NewField(pastesafe.WithRuneFilter(func(rune) bool { return false }))
}
