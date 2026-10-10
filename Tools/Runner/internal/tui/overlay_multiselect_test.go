package tui

// overlay_multiselect_test.go verifies the multi-select question overlay: it
// shows pre-checked defaults, toggles, takes free-form entries, confirms with
// Enter and answers Cancelled on Esc.

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"mosaic-common/interaction"
	"mosaic-common/tui/pastesafe"
)

func multiSelectQuestion(allowCustom bool, defaults ...string) interaction.ChoiceQuestion {
	return interaction.ChoiceQuestion{
		Question: interaction.Question{Title: "Pick inputs", AllowCustom: allowCustom, CustomPrompt: "Add a path"},
		Options: []interaction.Option{
			{ID: "a", Label: "alpha-label"},
			{ID: "b", Label: "beta-label"},
		},
		DefaultOptionIDs: defaults,
	}
}

// openMultiSelect shows q over the progress screen with keys treated as typed
// separately (Enter after text submits).
func openMultiSelect(t *testing.T, q interaction.ChoiceQuestion) (*rootModel, chan answerMsg) {
	t.Helper()
	restore := pastesafe.SetClock(pastesafe.SeparateKeysClock())
	t.Cleanup(restore)
	m := newTestModel()
	m.progressScreen = newProgressScreen(m)
	m.screen = screenProgress
	reply := make(chan answerMsg, 1)
	m.Update(questionMsg{kind: questionSelectMany, choiceQ: q, reply: reply})
	if m.screen != screenQuestion {
		t.Fatalf("precondition: question overlay not open, screen = %v", m.screen)
	}
	return m, reply
}

func sendKeys(m *rootModel, types ...tea.KeyType) {
	for _, kt := range types {
		sendKey(m, kt)
	}
}

// sendTyped delivers text as separately typed keys (spaces as KeySpace).
func sendTyped(m *rootModel, s string) {
	for _, r := range s {
		if r == ' ' {
			m.Update(tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{r}})
			continue
		}
		m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
}

func takeReply(t *testing.T, reply chan answerMsg) interaction.MultiChoiceAnswer {
	t.Helper()
	select {
	case ans := <-reply:
		return ans.multiChoiceAns
	default:
		t.Fatal("no reply was sent; the waiting session stays blocked")
		return interaction.MultiChoiceAnswer{}
	}
}

func assertNoReply(t *testing.T, reply chan answerMsg) {
	t.Helper()
	select {
	case <-reply:
		t.Fatal("a reply was sent although the user did not finish")
	default:
	}
}

func TestMultiSelectOverlayShowsOptionsAndPreCheckedDefaults(t *testing.T) {
	m, _ := openMultiSelect(t, multiSelectQuestion(false, "b"))

	view := m.View()

	if !strings.Contains(view, "alpha-label") || !strings.Contains(view, "beta-label") {
		t.Fatalf("view does not list the options:\n%s", view)
	}
	for _, line := range strings.Split(view, "\n") {
		if strings.Contains(line, "beta-label") && !strings.Contains(line, "[x]") {
			t.Errorf("default option is not shown checked: %q", line)
		}
		if strings.Contains(line, "alpha-label") && strings.Contains(line, "[x]") {
			t.Errorf("non-default option is shown checked: %q", line)
		}
	}
}

func TestMultiSelectOverlayEnterKeepsDefaultsChecked(t *testing.T) {
	m, reply := openMultiSelect(t, multiSelectQuestion(false, "a", "b"))

	sendKeys(m, tea.KeyEnter)

	ans := takeReply(t, reply)
	if ans.Status != interaction.Answered || !reflect.DeepEqual(ans.OptionIDs, []string{"a", "b"}) {
		t.Fatalf("answer = %+v, want Answered with [a b]", ans)
	}
	if m.screen != screenProgress {
		t.Errorf("screen = %v after confirming, want screenProgress (%v)", m.screen, screenProgress)
	}
}

func TestMultiSelectOverlayToggleChangesSelection(t *testing.T) {
	m, reply := openMultiSelect(t, multiSelectQuestion(false, "a"))

	sendKeys(m, tea.KeySpace, tea.KeyDown, tea.KeySpace, tea.KeyEnter)

	ans := takeReply(t, reply)
	if ans.Status != interaction.Answered || !reflect.DeepEqual(ans.OptionIDs, []string{"b"}) {
		t.Fatalf("answer = %+v, want Answered with [b]", ans)
	}
}

func TestMultiSelectOverlayUncheckingEverythingAnswersEmptyNotCancelled(t *testing.T) {
	m, reply := openMultiSelect(t, multiSelectQuestion(false, "a"))

	sendKeys(m, tea.KeySpace, tea.KeyEnter)

	ans := takeReply(t, reply)
	if ans.Status != interaction.Answered || len(ans.OptionIDs) != 0 || len(ans.Custom) != 0 {
		t.Fatalf("answer = %+v, want Answered with an empty selection", ans)
	}
}

func TestMultiSelectOverlayReturnsCustomEntriesAlongsideDefaults(t *testing.T) {
	m, reply := openMultiSelect(t, multiSelectQuestion(true, "a"))

	sendKeys(m, tea.KeyDown, tea.KeyDown, tea.KeySpace) // add-entry row, open field
	sendTyped(m, "docs/extra.md")
	sendKeys(m, tea.KeyEnter) // apply entry
	assertNoReply(t, reply)
	sendKeys(m, tea.KeyEnter) // confirm

	ans := takeReply(t, reply)
	if ans.Status != interaction.Answered {
		t.Fatalf("Status = %q, want Answered", ans.Status)
	}
	if !reflect.DeepEqual(ans.OptionIDs, []string{"a"}) {
		t.Errorf("OptionIDs = %v, want [a]", ans.OptionIDs)
	}
	if !reflect.DeepEqual(ans.Custom, []string{"docs/extra.md"}) {
		t.Errorf("Custom = %v, want [docs/extra.md]", ans.Custom)
	}
}

func TestMultiSelectOverlayTypedLettersWhileEditingStayInTheField(t *testing.T) {
	m, reply := openMultiSelect(t, multiSelectQuestion(true))

	sendKeys(m, tea.KeyDown, tea.KeyDown, tea.KeySpace)
	sendTyped(m, "stay")

	if m.screen != screenQuestion {
		t.Fatalf("screen = %v while typing, want screenQuestion (%v)", m.screen, screenQuestion)
	}
	sendKeys(m, tea.KeyEnter, tea.KeyEnter)
	ans := takeReply(t, reply)
	if !reflect.DeepEqual(ans.Custom, []string{"stay"}) {
		t.Errorf("Custom = %v, want [stay]", ans.Custom)
	}
}

func TestMultiSelectOverlayEscRepliesCancelledAndCloses(t *testing.T) {
	m, reply := openMultiSelect(t, multiSelectQuestion(true, "a"))

	sendKeys(m, tea.KeyEsc)

	ans := takeReply(t, reply)
	if ans.Status != interaction.Cancelled {
		t.Fatalf("Status = %q, want Cancelled", ans.Status)
	}
	if ans.OptionIDs != nil || ans.Custom != nil {
		t.Errorf("a cancelled answer must carry no selection, got %+v", ans)
	}
	if m.screen != screenProgress {
		t.Errorf("screen = %v after Esc, want screenProgress (%v)", m.screen, screenProgress)
	}
}

func TestMultiSelectOverlayEscWhileEditingOnlyClosesTheField(t *testing.T) {
	m, reply := openMultiSelect(t, multiSelectQuestion(true))
	sendKeys(m, tea.KeyDown, tea.KeyDown, tea.KeySpace)
	sendTyped(m, "abandoned")

	sendKeys(m, tea.KeyEsc)

	assertNoReply(t, reply)
	if m.screen != screenQuestion {
		t.Fatalf("screen = %v, want the overlay to stay open", m.screen)
	}
	sendKeys(m, tea.KeyEnter)
	ans := takeReply(t, reply)
	if ans.Status != interaction.Answered || len(ans.Custom) != 0 {
		t.Errorf("answer = %+v, want Answered without the abandoned entry", ans)
	}
}

func TestMultiSelectOverlayWithoutOptionsIsConfirmable(t *testing.T) {
	q := interaction.ChoiceQuestion{Question: interaction.Question{Title: "Pick", AllowCustom: true}}
	m, reply := openMultiSelect(t, q)

	sendKeys(m, tea.KeyEnter)

	ans := takeReply(t, reply)
	if ans.Status != interaction.Answered || len(ans.OptionIDs) != 0 || len(ans.Custom) != 0 {
		t.Fatalf("answer = %+v, want Answered and empty", ans)
	}
}

func TestMultiSelectOverlayHelpLineNamesTheKeys(t *testing.T) {
	m, _ := openMultiSelect(t, multiSelectQuestion(true))

	view := strings.ToLower(m.View())

	for _, want := range []string{"space", "enter", "esc"} {
		if !strings.Contains(view, want) {
			t.Errorf("help line does not mention %q:\n%s", want, view)
		}
	}
}

// signalModel reports when the wrapped model has processed a question message.
type signalModel struct {
	inner  *rootModel
	opened chan struct{}
}

func (s *signalModel) Init() tea.Cmd { return s.inner.Init() }

func (s *signalModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	_, cmd := s.inner.Update(msg)
	if _, ok := msg.(questionMsg); ok {
		select {
		case s.opened <- struct{}{}:
		default:
		}
	}
	return s, cmd
}

func (s *signalModel) View() string { return s.inner.View() }

func TestProgramRefSelectManyShowsOverlayAndReturnsConfirmedSelection(t *testing.T) {
	restore := pastesafe.SetClock(pastesafe.SeparateKeysClock())
	defer restore()
	inner := newTestModel()
	inner.progressScreen = newProgressScreen(inner)
	inner.screen = screenProgress
	model := &signalModel{inner: inner, opened: make(chan struct{}, 1)}
	p, done := runHeadless(model)
	defer func() { p.Quit(); <-done }()
	ref := NewProgramRef()
	ref.set(p)

	type result struct {
		ans interaction.MultiChoiceAnswer
		err error
	}
	got := make(chan result, 1)
	go func() {
		ans, err := ref.SelectMany(context.Background(), multiSelectQuestion(false, "a"))
		got <- result{ans, err}
	}()

	select {
	case <-model.opened:
	case r := <-got:
		t.Fatalf("SelectMany returned %+v without waiting for the user", r.ans)
	case <-time.After(5 * time.Second):
		t.Fatal("the question never reached the program")
	}
	p.Send(tea.KeyMsg{Type: tea.KeyEnter})

	select {
	case r := <-got:
		if r.err != nil {
			t.Fatalf("unexpected error: %v", r.err)
		}
		if r.ans.Status != interaction.Answered || !reflect.DeepEqual(r.ans.OptionIDs, []string{"a"}) {
			t.Fatalf("answer = %+v, want Answered with [a]", r.ans)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("SelectMany did not return after the user confirmed")
	}
}
