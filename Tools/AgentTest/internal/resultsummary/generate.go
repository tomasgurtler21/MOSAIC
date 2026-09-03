package resultsummary

import (
	"errors"
	"os"
	"sort"
	"strings"
	"time"

	"mosaic-agent-test/internal/resultstore"
)

// statsAccumulator gathers metrics for one harness+model combination across
// one or more report files.
type statsAccumulator struct {
	testCount     int
	passCount     int
	excludedCount int // sum of Aggregate.Excluded across tests
	durationMS    int64
	runCount      int
	totalCost     float64
	costWarning   bool
	hasPartial    bool
}

// add integrates the metrics from one report wire into the accumulator.
func (a *statsAccumulator) add(raw resultstore.ReportWire) {
	// Report-level cost attribution determines the warning flag.
	attr := raw.TotalCost.Attribution
	if attr == "unknown_bucket" || attr == "unavailable" {
		a.costWarning = true
	}

	// Report-level infrastructure failures determine the partial flag.
	if raw.InfrastructureFailures > 0 {
		a.hasPartial = true
	}

	for _, t := range raw.Tests {
		if t.Aggregate.InfrastructureFailure {
			a.hasPartial = true
		}
		a.testCount += t.Aggregate.Counted
		a.passCount += t.Aggregate.Passed
		a.excludedCount += t.Aggregate.Excluded
		a.totalCost += t.Aggregate.TotalCost.TotalUSD
		for _, r := range t.Runs {
			a.durationMS += r.DurationMS
			a.runCount++
		}
	}
}

// toStats converts the accumulated values into a HarnessModelStats struct.
func (a *statsAccumulator) toStats(model, harness string) HarnessModelStats {
	var passRate float64
	if a.testCount > 0 {
		passRate = float64(a.passCount) / float64(a.testCount)
	}
	var avgDuration time.Duration
	if a.runCount > 0 {
		avgDuration = time.Duration(a.durationMS/int64(a.runCount)) * time.Millisecond
	}
	return HarnessModelStats{
		Harness:        harness,
		Model:          model,
		TestCount:      a.testCount,
		PassCount:      a.passCount,
		PassRate:       passRate,
		AvgDuration:    avgDuration,
		TotalCost:      a.totalCost,
		CostWarning:    a.costWarning,
		HasPartial:     a.hasPartial,
		ExcludedCount:  a.excludedCount,
		AttemptedCount: a.testCount + a.excludedCount,
	}
}

// Generate scans the OrchestrationTestResults tree, groups reports, and writes
// or updates user-summary.md and internal-summary.md files per version. It
// also writes/updates the cross-version summary.md (unchanged behavior). It
// returns a result describing which files were written or updated. It returns
// an error only for infrastructure failures (cannot read
// OrchestrationTestResults/, cannot write a summary file).
//
// An empty or missing OrchestrationTestResults tree is not an error; Generate
// returns a SummaryResult with zero files written.
//
// When req.VersionFilter is non-empty, only that version directory
// is scanned and only its per-version summaries are written (plus the
// cross-version summary.md, updated to reflect any changes).
func Generate(fs FileSystem, req SummaryRequest) (SummaryResult, error) {
	var result SummaryResult

	// The result store writes reports under {root}/Orchestrator/{version}/,
	// so we scan that subtree for version directories.
	orchestratorRoot := req.TestResultsRoot + "/Orchestrator"
	rootEntries, err := fs.ListDir(orchestratorRoot)
	if err != nil {
		if isNotExist(err) {
			return result, nil
		}
		return result, err
	}

	// Identify version directories (skip files and non-directory entries).
	var versionDirs []string
	for _, name := range rootEntries {
		info, statErr := fs.Stat(orchestratorRoot + "/" + name)
		if statErr != nil {
			continue
		}
		if !info.IsDir {
			continue
		}
		versionDirs = append(versionDirs, name)
	}
	sort.Strings(versionDirs)

	if len(versionDirs) == 0 {
		return result, nil
	}

	// Scan every version directory for its reports. Store VersionSummary for
	// each version that has at least one valid report.
	allVersionSummaries := make(map[string]VersionSummary)
	for _, ver := range versionDirs {
		versionPath := orchestratorRoot + "/" + ver
		reports, scanErr := scanVersionDir(fs, versionPath)
		if scanErr != nil {
			return result, scanErr
		}
		if len(reports) == 0 {
			continue
		}
		vs := buildVersionSummary(ver, reports)
		allVersionSummaries[ver] = vs

		// Write per-version summary only when the filter matches (or is absent).
		if req.VersionFilter != "" && req.VersionFilter != ver {
			continue
		}

		userPath := versionPath + "/user-summary.md"
		userOutcome, userErr := writeUserSummary(fs, userPath, vs)
		if userErr != nil {
			return result, userErr
		}
		if userOutcome.Created {
			result.FilesWritten = append(result.FilesWritten, userPath)
		} else {
			result.FilesUpdated = append(result.FilesUpdated, userPath)
		}

		internalPath := versionPath + "/internal-summary.md"
		internalOutcome, internalErr := writeInternalSummary(fs, internalPath, vs)
		if internalErr != nil {
			return result, internalErr
		}
		if internalOutcome.Created {
			result.FilesWritten = append(result.FilesWritten, internalPath)
		} else {
			result.FilesUpdated = append(result.FilesUpdated, internalPath)
		}
	}

	// Write the cross-version summary whenever there is any data to show.
	if len(allVersionSummaries) > 0 {
		cv := buildCrossVersionSummary(allVersionSummaries)
		crossPath := orchestratorRoot + "/summary.md"
		outcome, writeErr := writeCrossFileSummary(fs, crossPath, cv)
		if writeErr != nil {
			return result, writeErr
		}
		if outcome.Created {
			result.FilesWritten = append(result.FilesWritten, crossPath)
		} else {
			result.FilesUpdated = append(result.FilesUpdated, crossPath)
		}
	}

	return result, nil
}

// scanVersionDir reads and parses all valid report JSON files in the given
// version directory. Non-JSON files and JSON files that fail ParseAndValidate
// are silently skipped. Files are processed in sorted name order.
func scanVersionDir(fs FileSystem, versionDir string) ([]resultstore.ParsedReport, error) {
	names, err := fs.ListDir(versionDir)
	if err != nil {
		return nil, err
	}
	sort.Strings(names)

	var reports []resultstore.ParsedReport
	for _, name := range names {
		if !strings.HasSuffix(name, ".json") {
			continue
		}
		fullPath := versionDir + "/" + name
		data, readErr := fs.ReadFile(fullPath)
		if readErr != nil {
			continue
		}
		parsed, parseErr := resultstore.ParseAndValidate(data)
		if parseErr != nil {
			continue
		}
		reports = append(reports, parsed)
	}
	return reports, nil
}

// testComboRate holds a pass rate for one model+harness combination for a
// specific test, used when computing problem areas.
type testComboRate struct {
	model    string
	harness  string
	rate     float64
	counted  int // from Aggregate.Counted
	excluded int // from Aggregate.Excluded
}

// buildVersionSummary aggregates a slice of parsed reports into a VersionSummary
// ready for rendering. Maps are iterated in sorted key order where determinism
// matters.
func buildVersionSummary(version string, reports []resultstore.ParsedReport) VersionSummary {
	type comboKey struct{ model, harness string }
	type suiteComboKey struct{ suite, model, harness string }
	type testKey struct {
		suite     string
		numericID int
	}

	byCombo := make(map[comboKey]*statsAccumulator)
	bySuiteCombo := make(map[suiteComboKey]*statsAccumulator)
	testCombos := make(map[testKey][]testComboRate)
	// infraCombos parallels testCombos but holds only infra-flagged test entries.
	infraCombos := make(map[testKey][]testComboRate)
	// testNames tracks the first-seen display name for each numeric-ID-keyed test.
	testNames := make(map[testKey]string)

	suiteSet := make(map[string]bool)
	modelSet := make(map[string]bool)
	harnessSet := make(map[string]bool)
	totalTests := 0

	// exclusionDetails accumulates per-exclusion detail keyed by (suite, numericID)
	// for deterministic ordering.
	type exclusionEntry struct {
		suite     string
		numericID int
		detail    ExclusionDetail
	}
	var exclusionEntries []exclusionEntry

	for _, parsed := range reports {
		model := parsed.ModelShort
		harness := parsed.HarnessID
		suite := parsed.SuiteID
		raw := parsed.Raw

		suiteSet[suite] = true
		modelSet[model] = true
		harnessSet[harness] = true

		ck := comboKey{model, harness}
		if byCombo[ck] == nil {
			byCombo[ck] = &statsAccumulator{}
		}
		byCombo[ck].add(raw)

		sck := suiteComboKey{suite, model, harness}
		if bySuiteCombo[sck] == nil {
			bySuiteCombo[sck] = &statsAccumulator{}
		}
		bySuiteCombo[sck].add(raw)

		for _, t := range raw.Tests {
			totalTests += t.Aggregate.Counted
			// Key by numeric test ID so that renamed tests (same numeric ID,
			// different string name) are still recognized as the same test.
			tk := testKey{suite, t.TestID}
			if _, seen := testNames[tk]; !seen {
				testNames[tk] = t.TestName
			}
			var rate float64
			if t.Aggregate.Counted > 0 {
				rate = float64(t.Aggregate.Passed) / float64(t.Aggregate.Counted)
			}
			cr := testComboRate{
				model:    model,
				harness:  harness,
				rate:     rate,
				counted:  t.Aggregate.Counted,
				excluded: t.Aggregate.Excluded,
			}
			if t.Aggregate.InfrastructureFailure {
				infraCombos[tk] = append(infraCombos[tk], cr)
			} else {
				testCombos[tk] = append(testCombos[tk], cr)
			}

			// Collect per-exclusion detail from the wire field (nil for older reports).
			for _, ex := range t.Aggregate.Exclusions {
				exclusionEntries = append(exclusionEntries, exclusionEntry{
					suite:     suite,
					numericID: t.TestID,
					detail: ExclusionDetail{
						Suite:             suite,
						TestName:          t.TestName,
						Reason:            ex.Reason,
						TerminationReason: ex.TerminationReason,
						Detail:            ex.Detail,
					},
				})
			}
		}
	}

	// Sort exclusion entries by suite, then numeric test ID, to produce
	// deterministic output regardless of report scan order.
	sort.Slice(exclusionEntries, func(i, j int) bool {
		if exclusionEntries[i].suite != exclusionEntries[j].suite {
			return exclusionEntries[i].suite < exclusionEntries[j].suite
		}
		return exclusionEntries[i].numericID < exclusionEntries[j].numericID
	})
	var exclusionDetails []ExclusionDetail
	for _, e := range exclusionEntries {
		exclusionDetails = append(exclusionDetails, e.detail)
	}

	// Build ByModel map.
	byModel := make(map[string]map[string]HarnessModelStats)
	for ck, acc := range byCombo {
		if byModel[ck.model] == nil {
			byModel[ck.model] = make(map[string]HarnessModelStats)
		}
		byModel[ck.model][ck.harness] = acc.toStats(ck.model, ck.harness)
	}

	// Build BySuite map.
	bySuite := make(map[string]map[string]map[string]HarnessModelStats)
	for sck, acc := range bySuiteCombo {
		if bySuite[sck.suite] == nil {
			bySuite[sck.suite] = make(map[string]map[string]HarnessModelStats)
		}
		if bySuite[sck.suite][sck.model] == nil {
			bySuite[sck.suite][sck.model] = make(map[string]HarnessModelStats)
		}
		bySuite[sck.suite][sck.model][sck.harness] = acc.toStats(sck.model, sck.harness)
	}

	// Build ProblemTests. Iterate test keys in sorted order for determinism.
	type testKey2 = testKey
	var sortedTestKeys []testKey2
	for tk := range testCombos {
		sortedTestKeys = append(sortedTestKeys, tk)
	}
	sort.Slice(sortedTestKeys, func(i, j int) bool {
		if sortedTestKeys[i].suite != sortedTestKeys[j].suite {
			return sortedTestKeys[i].suite < sortedTestKeys[j].suite
		}
		return sortedTestKeys[i].numericID < sortedTestKeys[j].numericID
	})

	var problemTests []TestStats
	for _, tk := range sortedTestKeys {
		combos := testCombos[tk]
		if len(combos) < 2 {
			continue
		}
		// Sort combos by model+harness key for deterministic best/worst selection.
		sort.Slice(combos, func(i, j int) bool {
			ki := combos[i].model + "/" + combos[i].harness
			kj := combos[j].model + "/" + combos[j].harness
			return ki < kj
		})

		best := combos[0]
		worst := combos[0]
		for _, c := range combos[1:] {
			if c.rate > best.rate {
				best = c
			}
			if c.rate < worst.rate {
				worst = c
			}
		}
		spread := best.rate - worst.rate
		if spread <= 0 {
			continue
		}
		problemTests = append(problemTests, TestStats{
			SuiteID:       tk.suite,
			TestName:      testNames[tk],
			NumericID:     tk.numericID,
			BestRate:      best.rate,
			BestCombo:     best.model + "/" + best.harness,
			WorstRate:     worst.rate,
			WorstCombo:    worst.model + "/" + worst.harness,
			Spread:        spread,
			BestCounted:   best.counted,
			BestExcluded:  best.excluded,
			WorstCounted:  worst.counted,
			WorstExcluded: worst.excluded,
		})
	}

	// Build InfraTests from infraCombos. Include all entries (no spread filter).
	// Sort infra keys by suite then numeric ID for determinism.
	var sortedInfraKeys []testKey
	for tk := range infraCombos {
		sortedInfraKeys = append(sortedInfraKeys, tk)
	}
	sort.Slice(sortedInfraKeys, func(i, j int) bool {
		if sortedInfraKeys[i].suite != sortedInfraKeys[j].suite {
			return sortedInfraKeys[i].suite < sortedInfraKeys[j].suite
		}
		return sortedInfraKeys[i].numericID < sortedInfraKeys[j].numericID
	})

	var infraTests []TestStats
	for _, tk := range sortedInfraKeys {
		combos := infraCombos[tk]
		// Sort combos by model+harness key for deterministic best/worst selection.
		sort.Slice(combos, func(i, j int) bool {
			ki := combos[i].model + "/" + combos[i].harness
			kj := combos[j].model + "/" + combos[j].harness
			return ki < kj
		})
		best := combos[0]
		worst := combos[0]
		for _, c := range combos[1:] {
			if c.rate > best.rate {
				best = c
			}
			if c.rate < worst.rate {
				worst = c
			}
		}
		spread := best.rate - worst.rate
		infraTests = append(infraTests, TestStats{
			SuiteID:       tk.suite,
			TestName:      testNames[tk],
			NumericID:     tk.numericID,
			BestRate:      best.rate,
			BestCombo:     best.model + "/" + best.harness,
			WorstRate:     worst.rate,
			WorstCombo:    worst.model + "/" + worst.harness,
			Spread:        spread,
			BestCounted:   best.counted,
			BestExcluded:  best.excluded,
			WorstCounted:  worst.counted,
			WorstExcluded: worst.excluded,
		})
	}

	suites := sortedStringKeys(suiteSet)
	models := sortedStringKeys(modelSet)
	harnesses := sortedStringKeys(harnessSet)

	return VersionSummary{
		Version:          version,
		ReportCount:      len(reports),
		Suites:           suites,
		Models:           models,
		Harnesses:        harnesses,
		TotalTests:       totalTests,
		ByModel:          byModel,
		BySuite:          bySuite,
		ProblemTests:     problemTests,
		InfraTests:       infraTests,
		ExclusionDetails: exclusionDetails,
	}
}

// buildCrossVersionSummary constructs a CrossVersionSummary from all scanned
// version summaries. Versions are sorted newest-first (reverse lexicographic).
// Regressions are computed by comparing the two most recent versions.
func buildCrossVersionSummary(allVersionSummaries map[string]VersionSummary) CrossVersionSummary {
	// Sort versions newest-first.
	versions := sortedStringKeys(mapFromVersionSummaries(allVersionSummaries))
	sort.Sort(sort.Reverse(sort.StringSlice(versions)))

	// Collect unique models and harnesses across all versions.
	modelSet := make(map[string]bool)
	harnessSet := make(map[string]bool)
	for _, vs := range allVersionSummaries {
		for _, m := range vs.Models {
			modelSet[m] = true
		}
		for _, h := range vs.Harnesses {
			harnessSet[h] = true
		}
	}
	models := sortedStringKeys(modelSet)
	harnesses := sortedStringKeys(harnessSet)

	// Detect regressions between the two most recent versions.
	var regressions []RegressionFlag
	if len(versions) >= 2 {
		newVer := versions[0]
		oldVer := versions[1]
		newVS := allVersionSummaries[newVer]
		oldVS := allVersionSummaries[oldVer]

		for _, model := range models {
			for _, harness := range harnesses {
				newStats, hasNew := getHarnessStats(newVS, model, harness)
				oldStats, hasOld := getHarnessStats(oldVS, model, harness)
				if !hasNew || !hasOld {
					continue
				}
				delta := newStats.PassRate - oldStats.PassRate
				if delta < 0 {
					regressions = append(regressions, RegressionFlag{
						Model:       model,
						Harness:     harness,
						OldVersion:  oldVer,
						NewVersion:  newVer,
						OldPassRate: oldStats.PassRate,
						NewPassRate: newStats.PassRate,
						Delta:       delta,
					})
				}
			}
		}
	}

	return CrossVersionSummary{
		Versions:    versions,
		Models:      models,
		Harnesses:   harnesses,
		ByVersion:   allVersionSummaries,
		Regressions: regressions,
	}
}

// getHarnessStats looks up the HarnessModelStats for a given model+harness
// combination within a VersionSummary.
func getHarnessStats(vs VersionSummary, model, harness string) (HarnessModelStats, bool) {
	byHarness, ok := vs.ByModel[model]
	if !ok {
		return HarnessModelStats{}, false
	}
	stats, ok := byHarness[harness]
	return stats, ok
}

// writeUserSummary renders the user-facing per-version summary via
// RenderUserSummary, merges it with any existing content (preserving analysis
// blocks), and writes the result to path.
func writeUserSummary(fs FileSystem, path string, vs VersionSummary) (SummaryFileOutcome, error) {
	newDoc := RenderUserSummary(vs)
	return writeMergedDoc(fs, path, newDoc)
}

// writeInternalSummary renders the internal-facing per-version summary via
// RenderInternalSummary, merges it with any existing content (preserving
// analysis blocks), and writes the result to path.
func writeInternalSummary(fs FileSystem, path string, vs VersionSummary) (SummaryFileOutcome, error) {
	newDoc := RenderInternalSummary(vs)
	return writeMergedDoc(fs, path, newDoc)
}

// writeCrossFileSummary renders the cross-version summary, merges it with any
// existing content, and writes the result to path.
func writeCrossFileSummary(fs FileSystem, path string, cv CrossVersionSummary) (SummaryFileOutcome, error) {
	newDoc := RenderCrossVersionSummary(cv)
	return writeMergedDoc(fs, path, newDoc)
}

// writeMergedDoc merges newDoc with any existing file at path (preserving
// analysis blocks) and writes the result. Returns the outcome and any write error.
func writeMergedDoc(fs FileSystem, path string, newDoc string) (SummaryFileOutcome, error) {
	// Extract generated blocks from the newly rendered document.
	newRegions := ParseMarkedDocument(newDoc)
	generatedBlocks := make(map[string]string)
	for _, r := range newRegions {
		if r.Type == RegionGenerated {
			generatedBlocks[r.Name] = r.Content
		}
	}

	// Merge with existing file if present; otherwise use the new document as-is.
	var merged string
	created := false
	if existingData, readErr := fs.ReadFile(path); readErr == nil {
		existing := ParseMarkedDocument(string(existingData))
		merged = MergeDocument(existing, generatedBlocks)
	} else {
		merged = newDoc
		created = true
	}

	if writeErr := fs.WriteFile(path, []byte(merged)); writeErr != nil {
		return SummaryFileOutcome{}, writeErr
	}
	return SummaryFileOutcome{Path: path, Created: created}, nil
}

// sortedStringKeys returns the keys of a map[string]bool in sorted order.
func sortedStringKeys(m map[string]bool) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// mapFromVersionSummaries builds a map[string]bool from the keys of a
// map[string]VersionSummary, so sortedStringKeys can consume it.
func mapFromVersionSummaries(m map[string]VersionSummary) map[string]bool {
	out := make(map[string]bool, len(m))
	for k := range m {
		out[k] = true
	}
	return out
}

// isNotExist returns true when err indicates the file or directory does not
// exist (wraps os.ErrNotExist).
func isNotExist(err error) bool {
	return errors.Is(err, os.ErrNotExist)
}
