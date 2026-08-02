package app

import (
	"context"

	"mosaic-common/interaction"
	"mosaic-log-analyzer/internal/domain"
)

// outcomeContinue is a package-internal sentinel returned by resolveSource to
// signal that a usable source was found and the caller should proceed. It is
// never surfaced to callers of the public API.
const outcomeContinue = Outcome(255)

// resolveSource determines the log source following the binding order:
// explicit path → default OrchestrationLogs/ location → user prompt.
//
// Returns the resolved source and outcomeContinue when a usable source is
// available. Returns an early-exit Outcome (OutcomeSourceNotFound or
// OutcomeSourceUnusable) when no usable source could be found.
func (s *Service) resolveSource(ctx context.Context, req Request) (domain.Source, Outcome, error) {
	if req.ExplicitPath != "" {
		src := s.deps.Source.Classify(req.ExplicitPath)
		switch src.Kind {
		case domain.SourceLogsRoot, domain.SourceSingleRun:
			return src, outcomeContinue, nil
		case domain.SourceUnusable:
			return src, OutcomeSourceUnusable, nil
		default: // SourceNotFound
			return src, OutcomeSourceNotFound, nil
		}
	}

	// No explicit path: probe the default location beneath the working directory.
	src := s.deps.Source.Default(s.deps.WorkDir)
	if src.IsUsable() {
		return src, outcomeContinue, nil
	}

	// Neither explicit path nor default is available — ask the user.
	ans, err := s.deps.Interaction.AskText(ctx, interaction.TextQuestion{
		Question: interaction.Question{
			ID:     QuestionLogSourcePath,
			Title:  "Log source",
			Prompt: "Enter the path to your logs root or run folder:",
		},
	})
	if err != nil {
		return domain.Source{}, OutcomeSourceNotFound, err
	}
	if ans.Status != interaction.Answered {
		// Declined, skipped or cancelled — a clean not-found outcome, not an error.
		return domain.Source{Kind: domain.SourceNotFound}, OutcomeSourceNotFound, nil
	}

	// Classify the user-supplied path.
	prompted := s.deps.Source.Classify(ans.Text)
	if !prompted.IsUsable() {
		return prompted, OutcomeSourceNotFound, nil
	}
	return prompted, outcomeContinue, nil
}
