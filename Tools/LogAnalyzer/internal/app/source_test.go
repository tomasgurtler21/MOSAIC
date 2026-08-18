package app_test

// Tests for source resolution order and declined-prompt behaviour.
//
// The service's source resolution must follow: explicit path → default location
// → user prompt via Interaction. A missing or declined source must never be a
// hard failure; the result must be OutcomeSourceNotFound with a nil error.

import (
	"context"
	"testing"

	"mosaic-common/interaction"
	"mosaic-log-analyzer/internal/app"
	"mosaic-log-analyzer/internal/domain"
)

// ---------------------------------------------------------------------------
// Source resolution: explicit path takes priority
// ---------------------------------------------------------------------------

func TestAnalyze_ExplicitPath_ClassifyCalledWithExplicitPath(t *testing.T) {
	// When ExplicitPath is set, the service must call Classify with that path —
	// not Default — to resolve the source.
	const explicitPath = "/my/logs"

	logSource := &fakeLogSource{
		classifyFunc: func(path string) domain.Source {
			if path == explicitPath {
				return domain.Source{Kind: domain.SourceLogsRoot, Path: explicitPath}
			}
			return domain.Source{Kind: domain.SourceNotFound}
		},
	}

	svc := app.New(app.Deps{
		Source:      logSource,
		Reader:      newFakeEventReader(),
		Pricing:     newFakePricingStore(),
		Clock:       newFakeClock(),
		Interaction: &alwaysSkippingInteraction{},
		WorkDir:     "/workdir",
	})

	_, _ = svc.Analyze(context.Background(), app.Request{ExplicitPath: explicitPath})

	found := false
	for _, c := range logSource.classifyCalls {
		if c == explicitPath {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Classify was not called with the explicit path %q; calls: %v",
			explicitPath, logSource.classifyCalls)
	}
}

func TestAnalyze_ExplicitPath_DefaultNotCalled(t *testing.T) {
	// When ExplicitPath is set, Default must not be called: the explicit path
	// short-circuits the default-location probe entirely.
	const explicitPath = "/my/logs"

	logSource := &fakeLogSource{
		classifyFunc: func(_ string) domain.Source {
			return domain.Source{Kind: domain.SourceLogsRoot, Path: explicitPath}
		},
	}

	svc := app.New(app.Deps{
		Source:      logSource,
		Reader:      newFakeEventReader(),
		Pricing:     newFakePricingStore(),
		Clock:       newFakeClock(),
		Interaction: &alwaysSkippingInteraction{},
		WorkDir:     "/workdir",
	})

	_, _ = svc.Analyze(context.Background(), app.Request{ExplicitPath: explicitPath})

	if len(logSource.defaultCalls) > 0 {
		t.Errorf("Default must not be called when an explicit path is supplied; got %d call(s): %v",
			len(logSource.defaultCalls), logSource.defaultCalls)
	}
}

func TestAnalyze_ExplicitPath_SourceUnusable_OutcomeSourceUnusable(t *testing.T) {
	// When ExplicitPath is set but Classify reports SourceUnusable, the outcome
	// must be OutcomeSourceUnusable, not an error and not OutcomeSourceNotFound.
	const explicitPath = "/not/a/logs/tree"

	logSource := &fakeLogSource{
		classifyFunc: func(_ string) domain.Source {
			return domain.Source{
				Kind:   domain.SourceUnusable,
				Path:   explicitPath,
				Reason: "no recognised structure found",
			}
		},
	}

	svc := app.New(app.Deps{
		Source:      logSource,
		Reader:      newFakeEventReader(),
		Pricing:     newFakePricingStore(),
		Clock:       newFakeClock(),
		Interaction: &alwaysSkippingInteraction{},
		WorkDir:     "/workdir",
	})

	result, err := svc.Analyze(context.Background(), app.Request{ExplicitPath: explicitPath})

	if err != nil {
		t.Errorf("Analyze must not return an error for an unusable source; got: %v", err)
	}
	if result.Outcome != app.OutcomeSourceUnusable {
		t.Errorf("Outcome = %v, want OutcomeSourceUnusable", result.Outcome)
	}
}

// ---------------------------------------------------------------------------
// Source resolution: default location
// ---------------------------------------------------------------------------

func TestAnalyze_NoExplicitPath_DefaultCalled(t *testing.T) {
	// When ExplicitPath is empty, the service must probe the default location
	// by calling Source.Default with the configured WorkDir.
	logSource := &fakeLogSource{}

	svc := app.New(app.Deps{
		Source:      logSource,
		Reader:      newFakeEventReader(),
		Pricing:     newFakePricingStore(),
		Clock:       newFakeClock(),
		Interaction: &alwaysSkippingInteraction{},
		WorkDir:     "/workdir",
	})

	_, _ = svc.Analyze(context.Background(), app.Request{})

	if len(logSource.defaultCalls) == 0 {
		t.Error("Default must be called when no explicit path is supplied")
	}
}

func TestAnalyze_NoExplicitPath_UsableDefault_ClassifyNotCalled(t *testing.T) {
	// When Default returns a usable source, Classify must not be called: the
	// default source is already fully resolved.
	logSource := &fakeLogSource{
		defaultFunc: func(_ string) domain.Source {
			return domain.Source{Kind: domain.SourceLogsRoot, Path: "/default/logs"}
		},
	}

	svc := app.New(app.Deps{
		Source:      logSource,
		Reader:      newFakeEventReader(),
		Pricing:     newFakePricingStore(),
		Clock:       newFakeClock(),
		Interaction: &alwaysSkippingInteraction{},
		WorkDir:     "/workdir",
	})

	_, _ = svc.Analyze(context.Background(), app.Request{})

	if len(logSource.classifyCalls) > 0 {
		t.Errorf("Classify must not be called when Default returns a usable source; got: %v",
			logSource.classifyCalls)
	}
}

// ---------------------------------------------------------------------------
// Source resolution: user prompt fallback
// ---------------------------------------------------------------------------

func TestAnalyze_BothUnavailable_InteractionPrompted(t *testing.T) {
	// When neither the explicit path nor the default location is available, the
	// service must ask the user for a path via Interaction with the question ID
	// QuestionLogSourcePath.
	logSource := &fakeLogSource{
		// Both Default and Classify return SourceNotFound.
	}

	ia := &scriptedInteraction{}

	svc := app.New(app.Deps{
		Source:      logSource,
		Reader:      newFakeEventReader(),
		Pricing:     newFakePricingStore(),
		Clock:       newFakeClock(),
		Interaction: ia,
		WorkDir:     "/workdir",
	})

	_, _ = svc.Analyze(context.Background(), app.Request{})

	if !ia.askTextCalledWith(app.QuestionLogSourcePath) {
		t.Errorf("AskText must be called with %q when neither explicit path nor default is available; calls: %v",
			app.QuestionLogSourcePath, ia.askTextCalls)
	}
}

func TestAnalyze_PromptAnswered_ClassifyCalledWithProvidedPath(t *testing.T) {
	// When the user supplies a path via the prompt, Classify must be called with
	// that path to resolve the source.
	const promptedPath = "/user/supplied/logs"

	logSource := &fakeLogSource{
		classifyFunc: func(path string) domain.Source {
			if path == promptedPath {
				return domain.Source{Kind: domain.SourceLogsRoot, Path: promptedPath}
			}
			return domain.Source{Kind: domain.SourceNotFound}
		},
	}

	ia := &scriptedInteraction{
		onAskText: func(q interaction.TextQuestion) interaction.TextAnswer {
			if q.ID == app.QuestionLogSourcePath {
				return interaction.TextAnswer{Status: interaction.Answered, Text: promptedPath}
			}
			return interaction.TextAnswer{Status: interaction.SkippedOne}
		},
	}

	svc := app.New(app.Deps{
		Source:      logSource,
		Reader:      newFakeEventReader(),
		Pricing:     newFakePricingStore(),
		Clock:       newFakeClock(),
		Interaction: ia,
		WorkDir:     "/workdir",
	})

	_, _ = svc.Analyze(context.Background(), app.Request{})

	found := false
	for _, c := range logSource.classifyCalls {
		if c == promptedPath {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Classify must be called with the user-supplied path %q; calls: %v",
			promptedPath, logSource.classifyCalls)
	}
}

// ---------------------------------------------------------------------------
// Source resolution: declined or cancelled prompt
// ---------------------------------------------------------------------------

func TestAnalyze_DeclinedPrompt_OutcomeSourceNotFound(t *testing.T) {
	// Declining the log-source prompt (SkippedOne) must yield
	// OutcomeSourceNotFound — not an error, not a hang.
	logSource := &fakeLogSource{} // Default returns SourceNotFound

	ia := &scriptedInteraction{
		// AskText returns SkippedOne (default scriptedInteraction behaviour).
	}

	svc := app.New(app.Deps{
		Source:      logSource,
		Reader:      newFakeEventReader(),
		Pricing:     newFakePricingStore(),
		Clock:       newFakeClock(),
		Interaction: ia,
		WorkDir:     "/workdir",
	})

	result, err := svc.Analyze(context.Background(), app.Request{})

	if err != nil {
		t.Errorf("Analyze must not return an error when the prompt is declined; got: %v", err)
	}
	if result.Outcome != app.OutcomeSourceNotFound {
		t.Errorf("Outcome = %v, want OutcomeSourceNotFound when prompt is declined", result.Outcome)
	}
}

func TestAnalyze_CancelledPrompt_OutcomeSourceNotFound(t *testing.T) {
	// Cancelling the log-source prompt (Cancelled status) must also yield
	// OutcomeSourceNotFound with no error.
	logSource := &fakeLogSource{}

	ia := &scriptedInteraction{
		onAskText: func(_ interaction.TextQuestion) interaction.TextAnswer {
			return interaction.TextAnswer{Status: interaction.Cancelled}
		},
	}

	svc := app.New(app.Deps{
		Source:      logSource,
		Reader:      newFakeEventReader(),
		Pricing:     newFakePricingStore(),
		Clock:       newFakeClock(),
		Interaction: ia,
		WorkDir:     "/workdir",
	})

	result, err := svc.Analyze(context.Background(), app.Request{})

	if err != nil {
		t.Errorf("Analyze must not return an error when the prompt is cancelled; got: %v", err)
	}
	if result.Outcome != app.OutcomeSourceNotFound {
		t.Errorf("Outcome = %v, want OutcomeSourceNotFound when prompt is cancelled", result.Outcome)
	}
}

func TestAnalyze_SkippedAllPrompt_OutcomeSourceNotFound(t *testing.T) {
	// SkippedAll (the user dismissed all prompts) must also yield
	// OutcomeSourceNotFound, not an error.
	logSource := &fakeLogSource{}

	ia := &scriptedInteraction{
		onAskText: func(_ interaction.TextQuestion) interaction.TextAnswer {
			return interaction.TextAnswer{Status: interaction.SkippedAll}
		},
	}

	svc := app.New(app.Deps{
		Source:      logSource,
		Reader:      newFakeEventReader(),
		Pricing:     newFakePricingStore(),
		Clock:       newFakeClock(),
		Interaction: ia,
		WorkDir:     "/workdir",
	})

	result, err := svc.Analyze(context.Background(), app.Request{})

	if err != nil {
		t.Errorf("Analyze must not return an error when the prompt is skipped-all; got: %v", err)
	}
	if result.Outcome != app.OutcomeSourceNotFound {
		t.Errorf("Outcome = %v, want OutcomeSourceNotFound when prompt is skipped-all", result.Outcome)
	}
}
