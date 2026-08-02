package app

import "mosaic-common/interaction"

// Question identifiers. Both frontends key off these exact values, so the TUI
// and the CLI answer the same questions by the same names.
const (
	// QuestionLogSourcePath asks for a logs-root or single-run-folder path.
	// Shape: AskText. Skipped/cancelled => OutcomeSourceNotFound.
	QuestionLogSourcePath interaction.QuestionID = "log-source-path"

	// QuestionPricingAction asks what to do about unpriced models.
	// Shape: SelectOne over PricingActionEnter / PricingActionShowPath /
	// PricingActionSkip. Skipped/cancelled => treated as PricingActionSkip.
	QuestionPricingAction interaction.QuestionID = "pricing-action"

	// QuestionPricingRate asks for one rate for one model.
	// Shape: AskText with Subject = "{model}:{field}" and a Validate that accepts
	// a non-negative decimal. Skipped/cancelled => that model stays unpriced.
	QuestionPricingRate interaction.QuestionID = "pricing-rate"

	// QuestionPricingThreshold asks for the optional context-length threshold.
	// Shape: AskText with Subject = model id. An empty answer or a skip means the
	// model has no long-context tier.
	QuestionPricingThreshold interaction.QuestionID = "pricing-threshold"

	// QuestionRunSelect asks which run to open when several are available.
	// Shape: SelectOne over run ids. Skipped/cancelled => stay on the run list.
	QuestionRunSelect interaction.QuestionID = "run-select"

	// QuestionPricingModel asks which model to set or overwrite a price for.
	// Shape: SelectOne whose OptionIDs are model id strings, one per model in
	// ReportModels order, each Label carrying the model id and its current
	// priced/unpriced state. Skipped/cancelled => no edit, report unchanged.
	QuestionPricingModel interaction.QuestionID = "pricing-model"
)

// Option identifiers for QuestionPricingAction.
const (
	PricingActionEnter    = "enter"
	PricingActionShowPath = "show-path"
	PricingActionSkip     = "skip"
)

// Rate field identifiers used as the Subject suffix of QuestionPricingRate.
const (
	RateFieldInput                = "input"
	RateFieldCachedInput          = "cached_input"
	RateFieldCacheWrite           = "cache_write"
	RateFieldOutputUnderThreshold = "output_under_threshold"
	RateFieldOutputAtThreshold    = "output_at_threshold"
)
