package deviation

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"mosaic-common/interaction"
	"mosaic-run/internal/domain"
)

// stepInputs asks for the input artifacts.
func (d *manualDialogue) stepInputs(ctx context.Context) error {
	return d.listStep(ctx, stepInputs, stepTask, stepOutputs)
}

// stepOutputs asks for the output artifacts.
func (d *manualDialogue) stepOutputs(ctx context.Context) error {
	return d.listStep(ctx, stepOutputs, stepInputs, stepConstraints)
}

// listStep asks one artifact list question (inputs or outputs): a multi-select
// pre-checked with the row's defaults for the chosen stage, or with the user's
// earlier selection, that also takes free-form paths. Esc goes to back.
func (d *manualDialogue) listStep(ctx context.Context, which, back, next manualStep) error {
	defaults, err := d.rowDefaults()
	if err != nil {
		return d.invalidResult(ctx, "the row's defaults cannot be resolved: "+err.Error(), d.stageOrRow())
	}
	state := d.listFor(which)
	q := d.listQuestion(which, defaults, *state)
	ans, cancelled, askErr := d.askMany(ctx, q)
	if askErr != nil {
		return askErr
	}
	if cancelled {
		d.step = back
		return nil
	}
	*state = listState{set: true, paths: selectedPaths(ans)}
	d.step = next
	return nil
}

func (d *manualDialogue) listFor(which manualStep) *listState {
	if which == stepInputs {
		return &d.inputs
	}
	return &d.outputs
}

func (d *manualDialogue) listQuestion(which manualStep, defaults domain.DispatchDefaults, state listState) interaction.ChoiceQuestion {
	id, prompt := domain.QuestionManualInputs, "Choose the input artifacts (add other paths as free-form entries):"
	own, other, ownKind, otherKind := defaults.InputArtifacts, defaults.OutputArtifacts, "input", "output"
	if which == stepOutputs {
		id, prompt = domain.QuestionManualOutputs, "Choose the output artifacts (add other paths as free-form entries):"
		own, other, ownKind, otherKind = defaults.OutputArtifacts, defaults.InputArtifacts, "output", "input"
	}
	options := newOptionList()
	options.add(own, "row default "+ownKind)
	options.add(other, "row default "+otherKind)
	for _, e := range d.req.ArtifactRegistry {
		options.add([]string{e.Artifact}, fmt.Sprintf("created by %s in %s", e.CreatedBy, e.CreatedIn))
	}
	checked := own
	if state.set {
		checked = state.paths
		options.add(state.paths, "added by you")
	}
	return interaction.ChoiceQuestion{
		Question: interaction.Question{
			ID: id, Prompt: prompt, AllowCustom: true, CustomPrompt: "Artifact path",
		},
		Options:          options.options,
		DefaultOptionIDs: slices.Clone(checked),
	}
}

// optionList collects artifact path options, each path once.
type optionList struct {
	options []interaction.Option
	seen    map[string]bool
}

func newOptionList() *optionList { return &optionList{seen: map[string]bool{}} }

func (l *optionList) add(paths []string, description string) {
	for _, p := range paths {
		p = strings.TrimSpace(p)
		if p == "" || l.seen[p] {
			continue
		}
		l.seen[p] = true
		l.options = append(l.options, interaction.Option{ID: p, Label: p, Description: description})
	}
}

// selectedPaths is the answer's checked options followed by its free-form
// entries, trimmed, without empty entries or repeats. Never nil.
func selectedPaths(ans interaction.MultiChoiceAnswer) []string {
	out := []string{}
	for _, p := range slices.Concat(ans.OptionIDs, ans.Custom) {
		p = strings.TrimSpace(p)
		if p != "" && !slices.Contains(out, p) {
			out = append(out, p)
		}
	}
	return out
}

// listOverride is the dispatch override for a list: nil (use the row's
// defaults) when the selection is exactly the defaults, else the selection.
func listOverride(state listState, defaults []string) *[]string {
	if !state.set || slices.Equal(state.paths, defaults) {
		return nil
	}
	paths := slices.Clone(state.paths)
	return &paths
}
