package planstages

import (
	"bytes"
	"fmt"
	"os"
	"strconv"
	"strings"

	"mosaic-common/mdtable"
	"mosaic-run/internal/domain"
)

// ReadStages reads the stage table from the Plan.md file at the given path
// and returns a validated StageSet.
//
// The needsApproach parameter indicates whether the caller requires the Approach
// column (true for two-group workflows). When true and the column is missing or
// contains an unrecognised value, an error is returned.
//
// Validates:
//   - Stage numbers are consecutive starting at 1 (no gaps, no sub-numbering)
//   - Dependencies reference only existing, lower-numbered stages (FR-16a)
//   - Approach values are in the recognised set when needsApproach is true (FR-17a)
//
// Returns *domain.RefusalError with a specific message naming what could not be
// read if the file is missing, the ## Stages heading is not found, required
// columns are absent, or Approach is required but absent or contains invalid
// values.
func ReadStages(path string, needsApproach bool) (domain.StageSet, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return domain.StageSet{}, &domain.RefusalError{
			Component: "planstages",
			Resource:  path,
			Reason:    fmt.Sprintf("cannot read file: %v", err),
		}
	}

	// Find the "## Stages" heading.
	offset, found := findStagesHeading(data)
	if !found {
		return domain.StageSet{}, &domain.RefusalError{
			Component: "planstages",
			Resource:  path,
			Reason:    "no ## Stages heading found",
		}
	}

	// Parse the first table at or after the heading.
	tbl, _, _, err := mdtable.ParseAt(data, offset)
	if err != nil {
		return domain.StageSet{}, &domain.RefusalError{
			Component: "planstages",
			Resource:  path,
			Reason:    fmt.Sprintf("no stage table found after ## Stages heading: %v", err),
		}
	}

	// Validate required columns.
	stageCol := tbl.Column("Stage")
	hitlCol := tbl.Column("HITL")
	dependsCol := tbl.Column("Depends On")

	if stageCol == -1 {
		return domain.StageSet{}, &domain.RefusalError{
			Component: "planstages",
			Resource:  path,
			Reason:    `required column "Stage" is absent from stage table`,
		}
	}
	if hitlCol == -1 {
		return domain.StageSet{}, &domain.RefusalError{
			Component: "planstages",
			Resource:  path,
			Reason:    `required column "HITL" is absent from stage table`,
		}
	}
	if dependsCol == -1 {
		return domain.StageSet{}, &domain.RefusalError{
			Component: "planstages",
			Resource:  path,
			Reason:    `required column "Depends On" is absent from stage table`,
		}
	}

	// Check Approach column when required.
	approachCol := tbl.Column("Approach")
	if needsApproach && approachCol == -1 {
		return domain.StageSet{}, &domain.RefusalError{
			Component: "planstages",
			Resource:  path,
			Reason:    `required column "Approach" is absent from stage table (two-group workflow)`,
		}
	}

	// Parse rows.
	entries := make([]domain.StageEntry, 0, len(tbl.Rows))
	for rowIdx, row := range tbl.Rows {
		_ = rowIdx

		// Parse stage number.
		stageStr := strings.TrimSpace(row[stageCol])
		stageNum, err := strconv.Atoi(stageStr)
		if err != nil {
			return domain.StageSet{}, &domain.RefusalError{
				Component: "planstages",
				Resource:  path,
				Reason:    fmt.Sprintf("stage number %q is not an integer", stageStr),
			}
		}

		// Parse HITL.
		hitlStr := strings.TrimSpace(row[hitlCol])
		hitl := hitlStr == "✅"

		// Parse Depends On.
		dependsStr := strings.TrimSpace(row[dependsCol])
		var dependsOn []domain.StageNumber
		if dependsStr != "-" && dependsStr != "" {
			parts := strings.Split(dependsStr, ",")
			for _, p := range parts {
				p = strings.TrimSpace(p)
				n, err := strconv.Atoi(p)
				if err != nil {
					return domain.StageSet{}, &domain.RefusalError{
						Component: "planstages",
						Resource:  path,
						Reason:    fmt.Sprintf("dependency %q in stage %d is not an integer", p, stageNum),
					}
				}
				dependsOn = append(dependsOn, domain.StageNumber(n))
			}
		}

		// Parse Approach (only when column is present and needsApproach is true).
		var approach domain.Approach
		if approachCol != -1 && needsApproach {
			approachStr := strings.TrimSpace(row[approachCol])
			switch approachStr {
			case string(domain.ApproachTDD):
				approach = domain.ApproachTDD
			case string(domain.ApproachImplementationFirst):
				approach = domain.ApproachImplementationFirst
			case string(domain.ApproachImplementationOnly):
				approach = domain.ApproachImplementationOnly
			case string(domain.ApproachTestsOnly):
				approach = domain.ApproachTestsOnly
			default:
				return domain.StageSet{}, &domain.RefusalError{
					Component: "planstages",
					Resource:  path,
					Reason:    fmt.Sprintf("unrecognised Approach value %q in stage %d", approachStr, stageNum),
				}
			}
		}

		entries = append(entries, domain.StageEntry{
			Number:    domain.StageNumber(stageNum),
			HITL:      hitl,
			DependsOn: dependsOn,
			Approach:  approach,
		})
	}

	// Validate: stages must start at 1 and be consecutive.
	for i, e := range entries {
		want := domain.StageNumber(i + 1)
		if e.Number != want {
			return domain.StageSet{}, &domain.RefusalError{
				Component: "planstages",
				Resource:  path,
				Reason:    fmt.Sprintf("stage numbers must be consecutive starting at 1: found %d at position %d (expected %d)", e.Number, i+1, want),
			}
		}
	}

	// Build a set of valid stage numbers for dependency validation.
	validNums := make(map[domain.StageNumber]bool, len(entries))
	for _, e := range entries {
		validNums[e.Number] = true
	}

	// Validate dependencies (FR-16a): must reference only existing, lower-numbered stages.
	for _, e := range entries {
		for _, dep := range e.DependsOn {
			if !validNums[dep] {
				return domain.StageSet{}, &domain.RefusalError{
					Component: "planstages",
					Resource:  path,
					Reason:    fmt.Sprintf("stage %d depends on nonexistent stage %d", e.Number, dep),
				}
			}
			if dep >= e.Number {
				return domain.StageSet{}, &domain.RefusalError{
					Component: "planstages",
					Resource:  path,
					Reason:    fmt.Sprintf("stage %d has a forward dependency on stage %d", e.Number, dep),
				}
			}
		}
	}

	return domain.StageSet{Entries: entries}, nil
}

// findStagesHeading scans data for a "## Stages" heading line and returns
// the byte offset immediately after that line (where a table might follow).
// Returns (0, false) if not found.
func findStagesHeading(data []byte) (int, bool) {
	lines := bytes.Split(data, []byte("\n"))
	offset := 0
	for _, line := range lines {
		trimmed := strings.TrimSpace(string(line))
		if trimmed == "## Stages" {
			// Return the offset after this line (start of next line).
			return offset + len(line) + 1, true
		}
		offset += len(line) + 1
	}
	return 0, false
}
