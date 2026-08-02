package app

import (
	"context"
	"strconv"

	"mosaic-common/interaction"
	"mosaic-log-analyzer/internal/domain"
)

// PricingGap describes one model the report could not price.
type PricingGap struct {
	Model domain.ModelID
	// Occurrences is how many actor reports listed this model as unpriced,
	// useful for display ordering.
	Occurrences int
}

// PricingGaps lists the unpriced models in a report, in the order they appear
// in Report.UnpricedModels (sorted by model id). An empty slice is returned
// when the report has no unpriced models.
func PricingGaps(r domain.Report) []PricingGap {
	if len(r.UnpricedModels) == 0 {
		return nil
	}

	// Count how many actor reports list each model as unpriced.
	occurrences := make(map[domain.ModelID]int)
	for _, run := range r.Runs {
		for _, m := range run.Orchestrator.UnpricedModels {
			occurrences[m]++
		}
		for _, agent := range run.Agents {
			for _, m := range agent.UnpricedModels {
				occurrences[m]++
			}
		}
	}
	if r.Unattributable != nil {
		for _, m := range r.Unattributable.Orchestrator.UnpricedModels {
			occurrences[m]++
		}
		for _, agent := range r.Unattributable.Agents {
			for _, m := range agent.UnpricedModels {
				occurrences[m]++
			}
		}
	}

	gaps := make([]PricingGap, len(r.UnpricedModels))
	for i, model := range r.UnpricedModels {
		gaps[i] = PricingGap{
			Model:       model,
			Occurrences: occurrences[model],
		}
	}
	return gaps
}

// ResolvePricingGaps offers the user, through the Interaction port, either to
// enter the missing prices or to be shown the config file path.
//
// Binding rules:
//   - Entered prices are persisted AUTOMATICALLY via PricingStore.Put, with no
//     separate confirmation step, and the written path is reported back through
//     Interaction.Notify.
//   - Declining, skipping or cancelling is a normal outcome: token totals remain
//     complete and money stays explicitly MoneyUnpriced.
//   - The report is re-priced from the Session, never by re-reading logs.
//   - This function must complete without blocking when given a skipping
//     Interaction implementation.
func (s *Service) ResolvePricingGaps(ctx context.Context, sess *Session, r domain.Report) (domain.Report, error) {
	gaps := PricingGaps(r)
	if len(gaps) == 0 {
		return r, nil
	}

	// Ask the user what to do about unpriced models.
	actionAns, err := s.deps.Interaction.SelectOne(ctx, interaction.ChoiceQuestion{
		Question: interaction.Question{
			ID:     QuestionPricingAction,
			Title:  "Missing prices",
			Prompt: "Some models have no pricing entry. What would you like to do?",
		},
		Options: []interaction.Option{
			{ID: PricingActionEnter, Label: "Enter prices now"},
			{ID: PricingActionShowPath, Label: "Show config file path"},
			{ID: PricingActionSkip, Label: "Skip (leave unpriced)"},
		},
	})
	if err != nil {
		return r, err
	}

	// Skipped or cancelled — return the original report unchanged (complete tokens,
	// explicit MoneyUnpriced money cells).
	if actionAns.Status != interaction.Answered {
		return r, nil
	}

	switch actionAns.OptionID {
	case PricingActionShowPath:
		s.deps.Interaction.Notify(ctx, interaction.Notice{
			Level:   interaction.NoticeInfo,
			Title:   "Pricing config",
			Message: "Edit this file to add model prices: " + s.deps.Pricing.Path(),
		})
		return r, nil

	case PricingActionEnter:
		anyPersisted := false
		for _, gap := range gaps {
			entry, ok, err := s.collectPricingEntry(ctx, gap.Model)
			if err != nil {
				return r, err
			}
			if !ok {
				continue
			}
			if err := s.SavePricing(ctx, sess, entry); err != nil {
				return r, err
			}
			s.deps.Interaction.Notify(ctx, interaction.Notice{
				Level:   interaction.NoticeInfo,
				Title:   "Saved",
				Message: "Pricing for " + string(gap.Model) + " saved to " + s.deps.Pricing.Path(),
			})
			anyPersisted = true
		}
		if !anyPersisted {
			return r, nil
		}
		// Re-price from the cached aggregate in the session — logs are not re-read.
		repriced := sess.Reprice()
		repriced.Quality = r.Quality // preserve the quality summary from the original analysis
		return repriced, nil

	default: // PricingActionSkip or any unrecognised option
		return r, nil
	}
}

// collectPricingEntry asks the user for pricing rates for a single model via
// the Interaction port. Returns (entry, true, nil) when enough rates were
// supplied to form a valid ModelPricing; returns (zero, false, nil) when the
// user skipped all rate questions or the resulting entry fails validation.
func (s *Service) collectPricingEntry(ctx context.Context, model domain.ModelID) (domain.ModelPricing, bool, error) {
	entry := domain.ModelPricing{Model: model}

	type rateField struct {
		name string
		set  func(r domain.Rate)
	}

	fields := []rateField{
		{RateFieldInput, func(r domain.Rate) { entry.Input = r }},
		{RateFieldCachedInput, func(r domain.Rate) { entry.CachedInput = r }},
		{RateFieldCacheWrite, func(r domain.Rate) { entry.CacheWrite = r }},
		{RateFieldOutputUnderThreshold, func(r domain.Rate) { entry.OutputUnderThreshold = r }},
		{RateFieldOutputAtThreshold, func(r domain.Rate) { entry.OutputAtThreshold = r }},
	}

	answeredCount := 0
	for _, f := range fields {
		ans, err := s.deps.Interaction.AskText(ctx, interaction.TextQuestion{
			Question: interaction.Question{
				ID:      QuestionPricingRate,
				Subject: string(model) + ":" + f.name,
				Title:   f.name,
				Prompt:  "USD per million tokens for " + string(model) + " " + f.name + ":",
			},
			Validate: func(v string) error {
				_, verr := domain.ParseRate(v)
				return verr
			},
		})
		if err != nil {
			return domain.ModelPricing{}, false, err
		}
		if ans.Status != interaction.Answered {
			continue
		}
		r, err := domain.ParseRate(ans.Text)
		if err != nil {
			continue
		}
		f.set(r)
		answeredCount++
	}

	// Ask for the optional context-length threshold.
	thAns, err := s.deps.Interaction.AskText(ctx, interaction.TextQuestion{
		Question: interaction.Question{
			ID:      QuestionPricingThreshold,
			Subject: string(model),
			Title:   "Context length threshold",
			Prompt:  "Enter context token count for long-context tier (empty = no tier):",
		},
	})
	if err != nil {
		return domain.ModelPricing{}, false, err
	}
	if thAns.Status == interaction.Answered && thAns.Text != "" {
		threshold, parseErr := strconv.ParseInt(thAns.Text, 10, 64)
		if parseErr == nil && threshold > 0 {
			entry.ContextLengthThreshold = threshold
			entry.HasThreshold = true
		}
	}

	if answeredCount == 0 {
		return domain.ModelPricing{}, false, nil
	}

	// Only persist entries that satisfy the domain validation rules.
	if err := entry.Validate(); err != nil {
		return domain.ModelPricing{}, false, nil
	}

	return entry, true, nil
}

// SavePricing persists one entry via the PricingStore and folds it into the
// session's pricing table so a subsequent Reprice call reflects the new entry
// without reloading the file.
func (s *Service) SavePricing(ctx context.Context, sess *Session, entry domain.ModelPricing) error {
	if err := s.deps.Pricing.Put(ctx, entry); err != nil {
		return err
	}
	sess.table = sess.table.With(entry)
	return nil
}
