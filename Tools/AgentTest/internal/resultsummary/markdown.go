package resultsummary

import (
	"fmt"
	"sort"
	"strings"
	"time"
)


// formatPassRate formats a pass rate (0.0–1.0) as a percentage string like "100%".
func formatPassRate(rate float64) string {
	return fmt.Sprintf("%.0f%%", rate*100)
}

// formatCost formats a cost value normalized to per-100-tests. When warning is
// true the cost is unresolved and the marker "[cost?]" is returned instead of
// a computed value. testCount is the denominator; if zero the per-100-tests
// value is computed as zero.
func formatCost(cost float64, testCount int, warning bool) string {
	if warning {
		return "[cost?]"
	}
	var normalized float64
	if testCount > 0 {
		normalized = cost / float64(testCount) * 100
	}
	return fmt.Sprintf("$%.2f/100t", normalized)
}

// formatDuration formats a duration for display. Zero duration returns "-".
func formatDuration(d time.Duration) string {
	if d == 0 {
		return "-"
	}
	return d.String()
}

// RenderVersionSummary produces the complete Markdown content for one
// version's summary.md file. The output contains marked regions
// (<!-- generated:overview -->, <!-- generated:model-results -->, etc.)
// so that MergeDocument can selectively update them on subsequent runs.
//
// The returned string is deterministic: the same VersionSummary input always
// produces byte-identical output (maps are iterated in sorted key order,
// floating-point values use fixed-precision formatting).
//
// Sections rendered (each wrapped in a <!-- generated:name --> marker):
//   - overview: report count, suite/model/harness lists
//   - model-results: per-model pass rates and cost tables
//   - model-comparison: models ranked by pass rate
//   - harness-comparison: harnesses ranked by pass rate
//   - problem-areas: tests with lowest pass rates or highest spread
//
// Analysis placeholders (each wrapped in <!-- analysis:name --> markers):
//   - overall-analysis: space for qualitative commentary on the version
func RenderVersionSummary(v VersionSummary) string {
	var sb strings.Builder

	sb.WriteString("# Version Summary: " + v.Version + "\n\n")

	// generated:overview
	sb.WriteString("<!-- generated:overview -->\n")
	renderOverviewSection(&sb, v)
	sb.WriteString("<!-- /generated:overview -->\n\n")

	// generated:model-comparison
	sb.WriteString("<!-- generated:model-comparison -->\n")
	renderModelComparisonSection(&sb, v)
	sb.WriteString("<!-- /generated:model-comparison -->\n\n")

	// generated:harness-comparison
	sb.WriteString("<!-- generated:harness-comparison -->\n")
	renderHarnessComparisonSection(&sb, v)
	sb.WriteString("<!-- /generated:harness-comparison -->\n\n")

	// generated:model-results
	sb.WriteString("<!-- generated:model-results -->\n")
	renderModelResultsSection(&sb, v)
	sb.WriteString("<!-- /generated:model-results -->\n\n")

	// generated:problem-areas
	sb.WriteString("<!-- generated:problem-areas -->\n")
	renderProblemAreasSection(&sb, v)
	sb.WriteString("<!-- /generated:problem-areas -->\n\n")

	// analysis:overall-analysis (empty placeholder)
	sb.WriteString("<!-- analysis:overall-analysis -->\n")
	sb.WriteString("<!-- /analysis:overall-analysis -->\n")

	return sb.String()
}

func renderOverviewSection(sb *strings.Builder, v VersionSummary) {
	sb.WriteString("## Overview\n\n")
	sb.WriteString(fmt.Sprintf("- **Version:** %s\n", v.Version))
	sb.WriteString(fmt.Sprintf("- **Reports:** %d\n", v.ReportCount))
	sb.WriteString(fmt.Sprintf("- **Total Tests:** %d\n", v.TotalTests))
	sb.WriteString(fmt.Sprintf("- **Suites:** %s\n", strings.Join(v.Suites, ", ")))
	sb.WriteString(fmt.Sprintf("- **Models:** %s\n", strings.Join(v.Models, ", ")))
	sb.WriteString(fmt.Sprintf("- **Harnesses:** %s\n", strings.Join(v.Harnesses, ", ")))
	sb.WriteString("\n")
}

func renderModelResultsSection(sb *strings.Builder, v VersionSummary) {
	sb.WriteString("## Model Results\n\n")

	for _, model := range v.Models { // v.Models is already sorted
		sb.WriteString(fmt.Sprintf("### %s\n\n", model))

		sb.WriteString("| Suite | Harness | Tests | Pass Rate | Cost |\n")
		sb.WriteString("|-------|---------|-------|-----------|------|\n")

		// Iterate suites in sorted order for determinism.
		for _, suite := range v.Suites { // v.Suites is already sorted
			byModel, ok := v.BySuite[suite]
			if !ok {
				continue
			}
			byHarness, ok := byModel[model]
			if !ok {
				continue
			}

			// Sort harnesses within this suite+model for determinism.
			var harnesses []string
			for h := range byHarness {
				harnesses = append(harnesses, h)
			}
			sort.Strings(harnesses)

			for _, harness := range harnesses {
				stats := byHarness[harness]
				sb.WriteString(fmt.Sprintf("| %s | %s | %d | %s | %s |\n",
					suite, harness,
					stats.TestCount,
					formatPassRate(stats.PassRate),
					formatCost(stats.TotalCost, stats.TestCount, stats.CostWarning),
				))
			}
		}
		sb.WriteString("\n")
	}
}

func renderModelComparisonSection(sb *strings.Builder, v VersionSummary) {
	sb.WriteString("## Model Comparison\n\n")
	sb.WriteString("| Model | Tests | Pass Rate | Cost |\n")
	sb.WriteString("|-------|-------|-----------|------|\n")

	for _, model := range v.Models { // already sorted
		byHarness := v.ByModel[model]

		// Aggregate across harnesses, sorted for determinism.
		var harnesses []string
		for h := range byHarness {
			harnesses = append(harnesses, h)
		}
		sort.Strings(harnesses)

		var testCount, passCount int
		var totalCost float64
		var costWarning bool

		for _, harness := range harnesses {
			s := byHarness[harness]
			testCount += s.TestCount
			passCount += s.PassCount
			totalCost += s.TotalCost
			if s.CostWarning {
				costWarning = true
			}
		}

		var passRate float64
		if testCount > 0 {
			passRate = float64(passCount) / float64(testCount)
		}

		sb.WriteString(fmt.Sprintf("| %s | %d | %s | %s |\n",
			model, testCount,
			formatPassRate(passRate),
			formatCost(totalCost, testCount, costWarning),
		))
	}
	sb.WriteString("\n")
}

func renderHarnessComparisonSection(sb *strings.Builder, v VersionSummary) {
	sb.WriteString("## Harness Comparison\n\n")
	sb.WriteString("| Harness | Tests | Pass Rate | Cost |\n")
	sb.WriteString("|---------|-------|-----------|------|\n")

	// Aggregate per-harness across all models.
	type hAgg struct {
		testCount   int
		passCount   int
		totalCost   float64
		costWarning bool
	}
	aggMap := make(map[string]*hAgg)

	for _, model := range v.Models {
		byHarness := v.ByModel[model]
		for _, harness := range v.Harnesses {
			s, ok := byHarness[harness]
			if !ok {
				continue
			}
			if aggMap[harness] == nil {
				aggMap[harness] = &hAgg{}
			}
			a := aggMap[harness]
			a.testCount += s.TestCount
			a.passCount += s.PassCount
			a.totalCost += s.TotalCost
			if s.CostWarning {
				a.costWarning = true
			}
		}
	}

	for _, harness := range v.Harnesses { // already sorted
		a, ok := aggMap[harness]
		if !ok {
			continue
		}
		var passRate float64
		if a.testCount > 0 {
			passRate = float64(a.passCount) / float64(a.testCount)
		}
		sb.WriteString(fmt.Sprintf("| %s | %d | %s | %s |\n",
			harness, a.testCount,
			formatPassRate(passRate),
			formatCost(a.totalCost, a.testCount, a.costWarning),
		))
	}
	sb.WriteString("\n")
}

func renderProblemAreasSection(sb *strings.Builder, v VersionSummary) {
	sb.WriteString("## Problem Areas\n\n")
	if len(v.ProblemTests) == 0 {
		sb.WriteString("No problem areas identified.\n\n")
		return
	}

	sb.WriteString("| Suite | ID | Test | Best Rate | Best Combo | Worst Rate | Worst Combo | Spread |\n")
	sb.WriteString("|-------|----|------|-----------|------------|------------|-------------|--------|\n")

	for _, pt := range v.ProblemTests {
		sb.WriteString(fmt.Sprintf("| %s | %d | %s | %s | %s | %s | %s | %s |\n",
			pt.SuiteID, pt.NumericID, pt.TestName,
			formatPassRate(pt.BestRate), pt.BestCombo,
			formatPassRate(pt.WorstRate), pt.WorstCombo,
			formatPassRate(pt.Spread),
		))
	}
	sb.WriteString("\n")
}

// RenderCrossVersionSummary produces the complete Markdown content for the
// cross-version summary.md file at TestResults/summary.md. It renders a
// comparison table showing how each model+harness combination performs across
// versions, enabling regression detection.
//
// The returned string is deterministic: the same CrossVersionSummary input
// always produces byte-identical output.
//
// Sections rendered (each wrapped in a <!-- generated:name --> marker):
//   - version-overview: list of versions with report counts
//   - version-comparison: per-model pass rates across versions
//   - regression-flags: models/harnesses whose pass rate dropped between
//     the two most recent versions
//
// Analysis placeholders (each wrapped in <!-- analysis:name --> markers):
//   - version-trends: space for qualitative cross-version observations
func RenderCrossVersionSummary(cv CrossVersionSummary) string {
	var sb strings.Builder

	sb.WriteString("# Cross-Version Summary\n\n")

	// generated:version-overview
	sb.WriteString("<!-- generated:version-overview -->\n")
	renderVersionOverviewSection(&sb, cv)
	sb.WriteString("<!-- /generated:version-overview -->\n\n")

	// generated:version-comparison
	sb.WriteString("<!-- generated:version-comparison -->\n")
	renderVersionComparisonSection(&sb, cv)
	sb.WriteString("<!-- /generated:version-comparison -->\n\n")

	// generated:regression-flags
	sb.WriteString("<!-- generated:regression-flags -->\n")
	renderRegressionFlagsSection(&sb, cv)
	sb.WriteString("<!-- /generated:regression-flags -->\n\n")

	// analysis:version-trends (empty placeholder)
	sb.WriteString("<!-- analysis:version-trends -->\n")
	sb.WriteString("<!-- /analysis:version-trends -->\n")

	return sb.String()
}

func renderVersionOverviewSection(sb *strings.Builder, cv CrossVersionSummary) {
	sb.WriteString("## Version Overview\n\n")
	sb.WriteString("| Version | Reports |\n")
	sb.WriteString("|---------|----------|\n")

	// cv.Versions is already ordered (newest first).
	for _, ver := range cv.Versions {
		vs, ok := cv.ByVersion[ver]
		if !ok {
			continue
		}
		sb.WriteString(fmt.Sprintf("| %s | %d |\n", ver, vs.ReportCount))
	}
	sb.WriteString("\n")
}

func renderVersionComparisonSection(sb *strings.Builder, cv CrossVersionSummary) {
	sb.WriteString("## Version Comparison\n\n")
	if len(cv.Versions) == 0 {
		sb.WriteString("No versions available.\n\n")
		return
	}

	// Build header row: "| Model | Harness | v2.0.0 | v1.0.0 |"
	sb.WriteString("| Model | Harness")
	for _, ver := range cv.Versions {
		sb.WriteString(" | " + ver)
	}
	sb.WriteString(" |\n")

	sb.WriteString("|-------|--------")
	for range cv.Versions {
		sb.WriteString("|--------")
	}
	sb.WriteString("|\n")

	// Iterate model+harness combos in sorted order.
	for _, model := range cv.Models {
		for _, harness := range cv.Harnesses {
			// Check if this combo exists in any version.
			hasData := false
			for _, ver := range cv.Versions {
				vs := cv.ByVersion[ver]
				if byHarness, ok := vs.ByModel[model]; ok {
					if _, ok2 := byHarness[harness]; ok2 {
						hasData = true
						break
					}
				}
			}
			if !hasData {
				continue
			}

			sb.WriteString(fmt.Sprintf("| %s | %s", model, harness))
			for _, ver := range cv.Versions {
				vs := cv.ByVersion[ver]
				if byHarness, ok := vs.ByModel[model]; ok {
					if stats, ok2 := byHarness[harness]; ok2 {
						sb.WriteString(" | " + formatPassRate(stats.PassRate))
						continue
					}
				}
				sb.WriteString(" | -")
			}
			sb.WriteString(" |\n")
		}
	}
	sb.WriteString("\n")
}

func renderRegressionFlagsSection(sb *strings.Builder, cv CrossVersionSummary) {
	sb.WriteString("## Regression Flags\n\n")
	if len(cv.Regressions) == 0 {
		sb.WriteString("No regressions detected.\n\n")
		return
	}

	sb.WriteString("| Model | Harness | Old Version | New Version | Old Rate | New Rate | Delta |\n")
	sb.WriteString("|-------|---------|-------------|-------------|----------|----------|-------|\n")

	for _, r := range cv.Regressions {
		sb.WriteString(fmt.Sprintf("| %s | %s | %s | %s | %s | %s | %s |\n",
			r.Model, r.Harness,
			r.OldVersion, r.NewVersion,
			formatPassRate(r.OldPassRate), formatPassRate(r.NewPassRate),
			formatPassRate(r.Delta),
		))
	}
	sb.WriteString("\n")
}
