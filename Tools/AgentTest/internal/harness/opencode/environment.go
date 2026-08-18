package opencode

import (
	"context"

	"mosaic-agent-test/internal/domain"
)

// CheckEnvironment implements domain.HarnessAdapter. This harness runs no
// interpreter of its own — the logger bundle's registration fragment is
// invoked by OpenCode's own plugin runtime, not by an interpreter this
// adapter resolves — so InterpreterApplicable stays false and InterpreterCmd
// stays empty rather than inventing a resolved value this adapter never
// uses. It reports honestly on the one thing it does check: whether any
// non-sandbox configuration scope registers something that could rewrite the
// intercepted call's input.
func (a *Adapter) CheckEnvironment(ctx context.Context) (domain.EnvironmentReport, error) {
	var report domain.EnvironmentReport

	findings, err := a.InspectScopes(ctx)
	report.Scopes = findings
	if err == nil {
		for _, f := range findings {
			if f.RewritesInput && !f.Neutralized {
				report.Problems = append(report.Problems, domain.EnvironmentProblem{
					Kind:   domain.ProblemCompetingRewriter,
					Detail: f.Detail,
				})
			}
		}
	}

	return report, nil
}
