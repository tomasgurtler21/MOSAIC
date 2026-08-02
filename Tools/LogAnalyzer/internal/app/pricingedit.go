package app

import (
	"context"
	"sort"

	"mosaic-common/interaction"
	"mosaic-log-analyzer/internal/domain"
)

// ModelPricingStatus pairs one model appearing in a report with its current
// state in the pricing table.
type ModelPricingStatus struct {
	// Model is the model id as it appears in the report.
	Model domain.ModelID
	// Priced reports whether the pricing table has an entry for Model.
	Priced bool
	// Pricing holds the current rates. Meaningful only when Priced is true;
	// it is the zero value otherwise.
	Pricing domain.ModelPricing
}

// PricingEditOutcome classifies how a pricing-edit flow ended.
type PricingEditOutcome uint8

const (
	// PricingEditCancelled: no model was selected (skipped, cancelled, empty model
	// id, nil session, or no models in the report). Nothing was asked beyond the
	// selection and nothing was persisted.
	PricingEditCancelled PricingEditOutcome = iota
	// PricingEditNoChange: a model was selected but no valid entry resulted (every
	// rate question skipped, or the collected entry failed validation). Nothing was
	// persisted; any pre-existing price is untouched.
	PricingEditNoChange
	// PricingEditSaved: an entry was persisted and the report was re-priced.
	PricingEditSaved
)

// PricingEditResult is what the pricing-editor entry points return.
type PricingEditResult struct {
	// Outcome classifies the end state.
	Outcome PricingEditOutcome
	// Model is the model the user chose. Empty when Outcome is PricingEditCancelled.
	Model domain.ModelID
	// Report is the re-priced report when Outcome is PricingEditSaved, and the
	// caller's original report otherwise. Never a zero Report.
	Report domain.Report
	// ConfigPath is the pricing config file the entry was written to.
	// Set only when Outcome is PricingEditSaved.
	ConfigPath string
}

// ReportModels returns every distinct model that appears anywhere in the
// report — named runs (orchestrator and agents) and the unattributable
// bucket — sorted by model id and de-duplicated.
//
// Returns an empty result for an empty report. A nil Unattributable is
// handled without panic. Priced and unpriced models are both included;
// this is a superset of Report.UnpricedModels.
func ReportModels(r domain.Report) []domain.ModelID {
	seen := make(map[domain.ModelID]struct{})

	collectModels := func(actor domain.ActorReport) {
		for _, m := range actor.Models {
			seen[m] = struct{}{}
		}
	}

	for _, run := range r.Runs {
		collectModels(run.Orchestrator)
		for _, agent := range run.Agents {
			collectModels(agent)
		}
	}

	if r.Unattributable != nil {
		collectModels(r.Unattributable.Orchestrator)
		for _, agent := range r.Unattributable.Agents {
			collectModels(agent)
		}
	}

	if len(seen) == 0 {
		return nil
	}

	models := make([]domain.ModelID, 0, len(seen))
	for m := range seen {
		models = append(models, m)
	}
	sort.Slice(models, func(i, j int) bool {
		return models[i] < models[j]
	})
	return models
}

// ModelPricingStates pairs each model in the report with its current state
// in the supplied pricing table, preserving ReportModels ordering, so a
// frontend can render the list without re-deriving priced/unpriced status.
func ModelPricingStates(r domain.Report, t domain.PricingTable) []ModelPricingStatus {
	models := ReportModels(r)
	if len(models) == 0 {
		return nil
	}

	states := make([]ModelPricingStatus, len(models))
	for i, m := range models {
		pricing, ok := t.Lookup(m)
		if ok {
			states[i] = ModelPricingStatus{Model: m, Priced: true, Pricing: pricing}
		} else {
			states[i] = ModelPricingStatus{Model: m, Priced: false}
		}
	}
	return states
}

// EditModelPricing sets or overwrites the price of one already-chosen model.
//
// Flow: collect the rate answers for model through the Interaction port
// (reusing the same question vocabulary as the pricing-gap flow), persist a
// valid entry via the pricing store, and return a report re-priced from the
// Session. Logs are never re-read.
//
// Binding rules:
//   - An empty model id yields PricingEditCancelled with the report unchanged
//     and no questions asked.
//   - A nil sess yields PricingEditCancelled with the report unchanged and no
//     questions asked.
//   - When the user skips every rate question, or the collected rates do not
//     form a valid domain.ModelPricing, NOTHING is persisted and the returned
//     result carries Outcome == PricingEditNoChange with Report == r unchanged.
//   - The returned Report preserves r.Quality (the re-price does not recompute
//     the quality summary).
//   - Must complete without blocking under a skipping Interaction.
func (s *Service) EditModelPricing(ctx context.Context, sess *Session, r domain.Report, model domain.ModelID) (PricingEditResult, error) {
	if model == "" || sess == nil {
		return PricingEditResult{Outcome: PricingEditCancelled, Report: r}, nil
	}

	entry, ok, err := s.collectPricingEntry(ctx, model)
	if err != nil {
		return PricingEditResult{}, err
	}
	if !ok {
		return PricingEditResult{Outcome: PricingEditNoChange, Report: r}, nil
	}

	if err := s.SavePricing(ctx, sess, entry); err != nil {
		return PricingEditResult{}, err
	}

	s.deps.Interaction.Notify(ctx, interaction.Notice{
		Level:   interaction.NoticeInfo,
		Title:   "Saved",
		Message: "Pricing for " + string(model) + " saved to " + s.deps.Pricing.Path(),
	})

	repriced := sess.Reprice()
	repriced.Quality = r.Quality

	return PricingEditResult{
		Outcome:    PricingEditSaved,
		Model:      model,
		Report:     repriced,
		ConfigPath: s.deps.Pricing.Path(),
	}, nil
}

// EditPricing is the frontend-agnostic entry point: it asks which model to
// price through the Interaction port (QuestionPricingModel, a SelectOne over
// every model returned by ReportModels), then delegates to EditModelPricing.
//
// Binding rules:
//   - A skipped or cancelled selection returns PricingEditCancelled with the
//     report unchanged and nothing persisted.
//   - An unrecognised OptionID is treated as a cancel.
//   - A report with no models asks nothing and returns PricingEditCancelled.
//   - Must complete without blocking under a skipping Interaction.
func (s *Service) EditPricing(ctx context.Context, sess *Session, r domain.Report) (PricingEditResult, error) {
	models := ReportModels(r)
	if len(models) == 0 {
		return PricingEditResult{Outcome: PricingEditCancelled, Report: r}, nil
	}

	// Determine the pricing table to use for labelling options (priced/unpriced).
	var table domain.PricingTable
	if sess != nil {
		table = sess.Table()
	} else {
		table = domain.EmptyPricingTable()
	}

	// Build one SelectOne option per model, with a label that distinguishes
	// priced from unpriced so a user can tell them apart.
	options := make([]interaction.Option, len(models))
	for i, m := range models {
		_, priced := table.Lookup(m)
		label := string(m)
		if priced {
			label += " (priced)"
		} else {
			label += " (unpriced)"
		}
		options[i] = interaction.Option{
			ID:    string(m),
			Label: label,
		}
	}

	ans, err := s.deps.Interaction.SelectOne(ctx, interaction.ChoiceQuestion{
		Question: interaction.Question{
			ID:     QuestionPricingModel,
			Title:  "Select model to price",
			Prompt: "Which model would you like to set or overwrite a price for?",
		},
		Options: options,
	})
	if err != nil {
		return PricingEditResult{}, err
	}

	if ans.Status != interaction.Answered {
		return PricingEditResult{Outcome: PricingEditCancelled, Report: r}, nil
	}

	// Reject unrecognised option ids — treat them as a cancel.
	chosen := domain.ModelID(ans.OptionID)
	valid := false
	for _, m := range models {
		if m == chosen {
			valid = true
			break
		}
	}
	if !valid {
		return PricingEditResult{Outcome: PricingEditCancelled, Report: r}, nil
	}

	return s.EditModelPricing(ctx, sess, r, chosen)
}
