package domain

import (
	"fmt"
	"maps"
)

// ReconcileResumeSettings computes the effective settings of a resumed run.
//
// recorded holds the settings parsed from the artifact; supplied holds the
// values the frontend passed and which marks the ones actually supplied.
// Only supplied values are compared; the rest are ignored.
//
//   - Mode, PreConsultation, ManualResolution: when the artifact records a
//     mode, the recorded values are effective and a supplied value that
//     differs is a conflict. When it records none, the supplied values are
//     adopted (adopt is true) and a mode must have been supplied.
//   - ReviewLoopLimit: the recorded value is always effective, also when
//     adopting. A supplied value that differs is a conflict, including a
//     positive value against a recorded 0.
//   - InfraClassSelections: the recorded map is effective. When supplied, it
//     is compared to the recorded map (nil and empty are equal).
//   - Checkpoints, Commits, CommitBranch: the recorded values are effective.
//   - CommitBranchVariant: "" when commits are off (a supplied value is
//     ignored). With a recorded commit branch the derived variant is effective
//     and a supplied variant that differs is a conflict. With commits on and no
//     branch recorded, a variant must be supplied to retry commit setup.
//
// A conflict or a missing adoption mode is a *RefusalError naming the
// frontmatter key and both values.
func ReconcileResumeSettings(recorded, supplied RunSettings, which SuppliedSettings) (effective RunSettings, adopt bool, err error) {
	conflict := func(key, suppliedText, storedText string) error {
		return &RefusalError{
			Component: "domain",
			Resource:  key,
			Reason: fmt.Sprintf("%s is fixed for this run: the artifact records %s but %s was supplied; "+
				"run settings cannot change on resume", key, storedText, suppliedText),
		}
	}

	effective = recorded

	if recorded.Mode == ExecutionModeUnset {
		if !which.Mode || supplied.Mode == ExecutionModeUnset {
			return recorded, false, &RefusalError{
				Component: "domain",
				Resource:  "runner_mode",
				Reason: "runner_mode is not recorded in this artifact and none was supplied " +
					"(runner_mode must be supplied to adopt this run); valid values: orchestrated, auto, auto-review",
			}
		}
		adopt = true
		effective.Mode = supplied.Mode
		effective.PreConsultation = supplied.PreConsultation
		effective.ManualResolution = supplied.ManualResolution
	} else {
		if which.Mode && supplied.Mode != recorded.Mode {
			return recorded, false, conflict("runner_mode", string(supplied.Mode), string(recorded.Mode))
		}
		if which.PreConsultation && supplied.PreConsultation != recorded.PreConsultation {
			return recorded, false, conflict("runner_pre_consultation", fmt.Sprint(supplied.PreConsultation), fmt.Sprint(recorded.PreConsultation))
		}
		if which.ManualResolution && supplied.ManualResolution != recorded.ManualResolution {
			return recorded, false, conflict("runner_manual_resolution", fmt.Sprint(supplied.ManualResolution), fmt.Sprint(recorded.ManualResolution))
		}
	}

	if which.ReviewLoopLimit && supplied.ReviewLoopLimit != recorded.ReviewLoopLimit {
		return recorded, false, conflict("review_loop_limit", reviewLimitText(supplied.ReviewLoopLimit), reviewLimitText(recorded.ReviewLoopLimit))
	}
	if which.InfraClassSelections && !maps.Equal(supplied.InfraClassSelections, recorded.InfraClassSelections) {
		return recorded, false, conflict("infrastructure_selections", fmt.Sprint(supplied.InfraClassSelections), fmt.Sprint(recorded.InfraClassSelections))
	}

	switch {
	case !recorded.Commits:
		effective.CommitBranchVariant = ""
	case recorded.CommitBranch != "":
		effective.CommitBranchVariant = recorded.CommitBranchVariant
		if which.CommitBranchVariant && supplied.CommitBranchVariant != recorded.CommitBranchVariant {
			return recorded, false, conflict("commit_branch", string(supplied.CommitBranchVariant), string(recorded.CommitBranchVariant))
		}
	default:
		if !which.CommitBranchVariant || supplied.CommitBranchVariant == "" {
			return recorded, false, &RefusalError{
				Component: "domain",
				Resource:  "commit_branch",
				Reason:    "commit branch variant must be supplied to retry commit setup",
			}
		}
		effective.CommitBranchVariant = supplied.CommitBranchVariant
	}
	return effective, adopt, nil
}

func reviewLimitText(n int) string {
	if n == 0 {
		return "no limit"
	}
	return fmt.Sprint(n)
}
