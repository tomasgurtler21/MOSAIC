package widgets_test

import (
	"reflect"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"mosaic-common/tui/pastesafe"
	"mosaic-common/tui/widgets"
)

// newCustomSelect builds a MultiSelect over items a and b with custom entry
// enabled. Keys are treated as separately typed, so Enter always submits.
func newCustomSelect(t *testing.T) *widgets.MultiSelect {
	t.Helper()
	restore := pastesafe.SetClock(pastesafe.SeparateKeysClock())
	t.Cleanup(restore)
	items := []widgets.ListItem{{ID: "a", Label: "A"}, {ID: "b", Label: "B"}}
	m := widgets.NewMultiSelect(items, 10, 40, widgets.DefaultMultiSelectStyles())
	m.EnableCustomEntry("Add an entry")
	return m
}

func press(m *widgets.MultiSelect, types ...tea.KeyType) {
	for _, kt := range types {
		m.Update(tea.KeyMsg{Type: kt})
	}
}

func typeText(m *widgets.MultiSelect, s string) {
	for _, r := range s {
		if r == ' ' {
			m.Update(tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{r}})
			continue
		}
		m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
}

// addEntry moves to the add-entry row, opens the field, types text and applies it.
func addEntry(m *widgets.MultiSelect, text string) {
	for i := 0; i < 6; i++ { // the cursor stops at the last row, the add-entry row
		press(m, tea.KeyDown)
	}
	press(m, tea.KeySpace)
	typeText(m, text)
	press(m, tea.KeyEnter)
}

func TestMultiSelectPreCheckedDefaultsStartCheckedAndToggleOff(t *testing.T) {
	m := newCustomSelect(t)
	m.SetChecked("a", true)

	if got := m.SelectedIDs(); !reflect.DeepEqual(got, []string{"a"}) {
		t.Fatalf("SelectedIDs = %v, want [a]", got)
	}
	press(m, tea.KeySpace) // cursor starts on the first item
	if m.IsChecked("a") {
		t.Fatal("toggling a pre-checked default must uncheck it")
	}
}

func TestMultiSelectAddEntryRowIsLastAndSpaceOpensField(t *testing.T) {
	m := newCustomSelect(t)

	press(m, tea.KeyDown, tea.KeyDown)
	if m.CursorIndex() != 2 {
		t.Fatalf("CursorIndex = %d, want 2 (the add-entry row after two items)", m.CursorIndex())
	}
	press(m, tea.KeySpace)

	if !m.Editing() {
		t.Fatal("Space on the add-entry row must open the entry field")
	}
}

func TestMultiSelectEnteredTextBecomesCheckedCustomEntry(t *testing.T) {
	m := newCustomSelect(t)

	addEntry(m, "docs/extra.md")

	if m.Editing() {
		t.Error("Enter outside the paste window must close the field")
	}
	if m.Done() {
		t.Error("applying an entry must not confirm the selection")
	}
	if got := m.CustomEntries(); !reflect.DeepEqual(got, []string{"docs/extra.md"}) {
		t.Fatalf("CustomEntries = %v, want [docs/extra.md]", got)
	}
	if got := m.SelectedIDs(); len(got) != 0 {
		t.Errorf("SelectedIDs = %v, must never contain custom entries", got)
	}
}

func TestMultiSelectKeysWhileEditingGoToTheField(t *testing.T) {
	m := newCustomSelect(t)

	addEntry(m, "jk q")

	if got := m.CustomEntries(); !reflect.DeepEqual(got, []string{"jk q"}) {
		t.Fatalf("CustomEntries = %v, want [jk q]", got)
	}
	if m.IsChecked("a") || m.IsChecked("b") {
		t.Error("typed space or navigation letters must not toggle items while editing")
	}
}

func TestMultiSelectEmptyEntryAddsNothing(t *testing.T) {
	m := newCustomSelect(t)

	press(m, tea.KeyDown, tea.KeyDown, tea.KeySpace)
	if !m.Editing() {
		t.Fatal("precondition: Space on the add-entry row must open the field")
	}
	typeText(m, "   ")
	press(m, tea.KeyEnter)

	if m.Editing() {
		t.Error("Enter must close the field")
	}
	if got := m.CustomEntries(); len(got) != 0 {
		t.Fatalf("CustomEntries = %v, want none", got)
	}
}

func TestMultiSelectEntryEqualToItemIDChecksItemWithoutCustomRow(t *testing.T) {
	m := newCustomSelect(t)

	addEntry(m, "a")

	if !m.IsChecked("a") {
		t.Error("an entry equal to an item ID must check that item")
	}
	if got := m.CustomEntries(); len(got) != 0 {
		t.Errorf("CustomEntries = %v, want none", got)
	}
}

func TestMultiSelectDuplicateEntryIsKeptOnce(t *testing.T) {
	m := newCustomSelect(t)

	addEntry(m, "x.md")
	addEntry(m, "x.md")

	if got := m.CustomEntries(); !reflect.DeepEqual(got, []string{"x.md"}) {
		t.Fatalf("CustomEntries = %v, want [x.md] once", got)
	}
}

func TestMultiSelectCustomEntryRowToggles(t *testing.T) {
	m := newCustomSelect(t)
	addEntry(m, "x.md")
	press(m, tea.KeyUp) // from the add-entry row to the custom row

	press(m, tea.KeySpace)
	if got := m.CustomEntries(); len(got) != 0 {
		t.Fatalf("unchecked custom entry must be omitted, got %v", got)
	}
	press(m, tea.KeySpace)
	if got := m.CustomEntries(); !reflect.DeepEqual(got, []string{"x.md"}) {
		t.Fatalf("re-checked custom entry missing, got %v", got)
	}
}

func TestMultiSelectCustomEntriesKeepOrderAdded(t *testing.T) {
	m := newCustomSelect(t)

	addEntry(m, "first")
	addEntry(m, "second")

	if got := m.CustomEntries(); !reflect.DeepEqual(got, []string{"first", "second"}) {
		t.Fatalf("CustomEntries = %v, want [first second]", got)
	}
}

func TestMultiSelectEscWhileEditingClosesFieldWithoutBackOrEntry(t *testing.T) {
	m := newCustomSelect(t)
	press(m, tea.KeyDown, tea.KeyDown, tea.KeySpace)
	typeText(m, "abandoned")

	press(m, tea.KeyEsc)

	if m.Editing() {
		t.Error("Esc must close the entry field")
	}
	if m.Back() {
		t.Error("Esc while editing must not set Back")
	}
	if got := m.CustomEntries(); len(got) != 0 {
		t.Errorf("CustomEntries = %v, want none", got)
	}
}

func TestMultiSelectPastedLineBreaksBecomeSpacesAndDoNotApply(t *testing.T) {
	m := newCustomSelect(t)
	press(m, tea.KeyDown, tea.KeyDown, tea.KeySpace)

	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("one\r\ntwo"), Paste: true})

	if !m.Editing() {
		t.Fatal("a paste must not close the field")
	}
	press(m, tea.KeyEnter)
	if got := m.CustomEntries(); !reflect.DeepEqual(got, []string{"one two"}) {
		t.Fatalf("CustomEntries = %v, want [one two]", got)
	}
}

func TestMultiSelectEnterInsidePasteWindowDoesNotApplyEntry(t *testing.T) {
	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	restore := pastesafe.SetClock(func() time.Time { return at })
	defer restore()
	items := []widgets.ListItem{{ID: "a", Label: "A"}}
	m := widgets.NewMultiSelect(items, 10, 40, widgets.DefaultMultiSelectStyles())
	m.EnableCustomEntry("Add")
	press(m, tea.KeyDown, tea.KeySpace)
	typeText(m, "x")

	press(m, tea.KeyEnter)

	if !m.Editing() {
		t.Fatal("an Enter inside the paste window must not close the field")
	}
	if len(m.CustomEntries()) != 0 {
		t.Fatal("an Enter inside the paste window must not add an entry")
	}
}

func TestMultiSelectEscCancelIsDistinguishableFromEmptyConfirm(t *testing.T) {
	cancelled := newCustomSelect(t)
	confirmed := newCustomSelect(t)

	press(cancelled, tea.KeyEsc)
	press(confirmed, tea.KeyEnter)

	if !cancelled.Back() || cancelled.Done() {
		t.Errorf("Esc: Back=%v Done=%v, want Back only", cancelled.Back(), cancelled.Done())
	}
	if !confirmed.Done() || confirmed.Back() {
		t.Errorf("Enter on an empty selection: Done=%v Back=%v, want Done only", confirmed.Done(), confirmed.Back())
	}
	if len(confirmed.SelectedIDs()) != 0 || len(confirmed.CustomEntries()) != 0 {
		t.Error("an empty confirmed selection must have no ids and no custom entries")
	}
}

func TestMultiSelectEnterConfirmsOnCustomAndAddEntryRows(t *testing.T) {
	m := newCustomSelect(t)
	addEntry(m, "x.md")

	press(m, tea.KeyEnter) // cursor is on the add-entry row

	if !m.Done() {
		t.Fatal("Enter on the add-entry row must confirm")
	}
	if got := m.CustomEntries(); !reflect.DeepEqual(got, []string{"x.md"}) {
		t.Errorf("CustomEntries = %v, want [x.md]", got)
	}
}

func TestMultiSelectWithoutItemsStartsOnAddEntryRow(t *testing.T) {
	restore := pastesafe.SetClock(pastesafe.SeparateKeysClock())
	defer restore()
	m := widgets.NewMultiSelect(nil, 10, 40, widgets.DefaultMultiSelectStyles())
	m.EnableCustomEntry("Add")

	press(m, tea.KeySpace)

	if !m.Editing() {
		t.Fatal("with no items the cursor starts on the add-entry row, so Space opens the field")
	}
}

func TestMultiSelectResetClosesOpenFieldAndKeepsEntries(t *testing.T) {
	m := newCustomSelect(t)
	addEntry(m, "kept")
	press(m, tea.KeySpace) // reopen on the add-entry row
	typeText(m, "dropped")

	m.Reset()

	if m.Editing() {
		t.Error("Reset must close an open field")
	}
	if got := m.CustomEntries(); !reflect.DeepEqual(got, []string{"kept"}) {
		t.Errorf("CustomEntries = %v, want [kept]", got)
	}
}
