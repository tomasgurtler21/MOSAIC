// Package preflight composes internal/authoring and internal/fixtures into
// one aggregated, deterministically ordered cross-format validation report.
// It performs no process spawning and no sandbox creation.
package preflight

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"mosaic-agent-test/internal/authoring"
	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/fixtures"
)

// Input is everything Validate needs to resolve a runnable plan.
type Input struct {
	SuitePath   string
	FixtureRoot string
	// HarnessID selects which adapter's capabilities the plan targets.
	HarnessID string
	// Overrides applied on top of suite defaults, from CLI flags.
	Overrides Overrides
}

// Overrides are CLI-flag-sourced values applied on top of a suite's
// defaults.
type Overrides struct {
	Timeout     *time.Duration
	TurnLimit   *int
	Repetitions *int
	TestIDs     []string // when non-empty, restrict the plan to these tests
}

// Plan is a fully resolved, cross-validated set of tests ready to run. It is
// usable only when the accompanying Report has no errors.
type Plan struct {
	Suite domain.TestSuite
	Tests []ResolvedTest
}

// ResolvedTest is one test definition plus its stub registry and the
// settings effective after suite defaults and overrides are applied.
type ResolvedTest struct {
	Definition domain.TestDefinition
	Registry   domain.StubRegistry
	Settings   domain.RunSettings
}

// Validate parses and cross-validates everything an execution needs and
// returns a fully resolved plan, together with every schema and
// cross-reference error found in one pass. Cross-checks performed:
//   - every suite entry resolves to an existing test definition
//   - every test definition resolves to an existing stub registry
//   - every collaborator named in an assertion appears in that test's
//     registry
//   - every $ref in a definition, a stub side effect or a seed file resolves
//   - every parallel group names only members that appear in the expected
//     invocation sequence
//   - the registry's test_id matches the definition's id
//   - repetitions/pass rate, timeout and turn limit are within legal ranges
func Validate(in Input) (Plan, authoring.Report) {
	var report authoring.Report
	var plan Plan

	suiteData, err := os.ReadFile(in.SuitePath)
	if err != nil {
		report.Add(authoring.Diagnostic{
			Severity: authoring.SeverityError,
			Code:     "missing-suite",
			Path:     in.SuitePath,
			Message:  fmt.Sprintf("suite file not found: %v", err),
		})
		return plan, report
	}

	suite, suiteReport := authoring.ParseSuite(authoring.Source{Path: in.SuitePath, Data: suiteData})
	report.Merge(suiteReport)
	plan.Suite = suite

	resolver, err := fixtures.NewResolver(in.FixtureRoot)
	if err != nil {
		report.Add(authoring.Diagnostic{
			Severity: authoring.SeverityError,
			Code:     "invalid-fixture-root",
			Path:     in.FixtureRoot,
			Message:  err.Error(),
		})
		resolver = nil
	}

	suiteDir := filepath.Dir(in.SuitePath)

	for i, entry := range suite.Entries {
		defPath := filepath.Join(suiteDir, filepath.FromSlash(entry.Path))
		defData, err := os.ReadFile(defPath)
		if err != nil {
			report.Add(authoring.Diagnostic{
				Severity: authoring.SeverityError,
				Code:     "missing-test-definition",
				Path:     in.SuitePath,
				Pointer:  fmt.Sprintf("tests[%d].path", i),
				Message:  fmt.Sprintf("test definition %q not found", entry.Path),
			})
			continue
		}

		def, defReport := authoring.ParseTestDefinition(authoring.Source{Path: defPath, Data: defData})
		report.Merge(defReport)
		defDir := filepath.Dir(defPath)

		var registry domain.StubRegistry
		var regPath string
		haveRegistry := false

		if def.StubRegistryPath == "" {
			report.Add(authoring.Diagnostic{
				Severity: authoring.SeverityError,
				Code:     "missing-stub-registry",
				Path:     defPath,
				Pointer:  "stub_registry",
				Message:  "test definition declares no stub_registry",
			})
		} else {
			regPath = filepath.Join(defDir, filepath.FromSlash(def.StubRegistryPath))
			regData, err := os.ReadFile(regPath)
			if err != nil {
				report.Add(authoring.Diagnostic{
					Severity: authoring.SeverityError,
					Code:     "missing-stub-registry",
					Path:     defPath,
					Pointer:  "stub_registry",
					Message:  fmt.Sprintf("stub registry %q not found", def.StubRegistryPath),
				})
			} else {
				var regReport authoring.Report
				registry, regReport = authoring.ParseStubRegistry(authoring.Source{Path: regPath, Data: regData})
				report.Merge(regReport)
				haveRegistry = true

				if registry.TestID != "" && registry.TestID != def.ID {
					report.Add(authoring.Diagnostic{
						Severity: authoring.SeverityError,
						Code:     "test-id-mismatch",
						Path:     regPath,
						Pointer:  "test_id",
						Message:  fmt.Sprintf("stub registry test_id %q does not match test definition id %q", registry.TestID, def.ID),
					})
				}
			}
		}

		if haveRegistry {
			checkCollaboratorsKnown(&report, defPath, def, registryIdentities(registry))
		}

		if resolver != nil {
			checkSeedFileRefs(&report, resolver, defPath, def)
			if haveRegistry {
				checkSideEffectRefs(&report, resolver, regPath, registry)
			}
		}

		checkParallelGroups(&report, defPath, def)

		effective := mergeSettings(mergeSettings(suite.Defaults, entry.Settings), def.Settings)
		effective = applyOverrides(effective, in.Overrides)
		addRangeDiagnostics(&report, defPath, effective)

		plan.Tests = append(plan.Tests, ResolvedTest{
			Definition: def,
			Registry:   registry,
			Settings:   effective,
		})
	}

	plan.Tests = filterByTestIDs(plan.Tests, in.Overrides.TestIDs)

	return plan, report
}

// filterByTestIDs restricts tests to those whose Definition.ID is named in
// ids, preserving order. An empty ids leaves tests untouched: every test
// runs unless a subset was explicitly declared. Cross-file validation above
// still covers every entry regardless of this filter, so an authoring error
// in a test excluded from the run is still reported.
func filterByTestIDs(tests []ResolvedTest, ids []string) []ResolvedTest {
	if len(ids) == 0 {
		return tests
	}
	wanted := make(map[string]bool, len(ids))
	for _, id := range ids {
		wanted[id] = true
	}
	filtered := make([]ResolvedTest, 0, len(tests))
	for _, t := range tests {
		if wanted[t.Definition.ID] {
			filtered = append(filtered, t)
		}
	}
	return filtered
}

// registryIdentities is the set of collaborator identities a stub registry
// declares.
func registryIdentities(reg domain.StubRegistry) map[domain.CollaboratorIdentity]bool {
	set := make(map[domain.CollaboratorIdentity]bool, len(reg.Stubs))
	for _, s := range reg.Stubs {
		set[s.Match.Identity] = true
	}
	return set
}

// sequenceIdentities is the set of collaborator identities that appear
// anywhere in a sequence assertion, ordered or grouped.
func sequenceIdentities(seq *domain.SequenceAssertion) map[domain.CollaboratorIdentity]bool {
	set := map[domain.CollaboratorIdentity]bool{}
	if seq == nil {
		return set
	}
	for _, step := range seq.Steps {
		if step.Identity != nil {
			set[*step.Identity] = true
		}
		for _, m := range step.Members {
			set[m] = true
		}
	}
	return set
}

// checkCollaboratorsKnown reports "unknown-collaborator" for every
// collaborator an assertion names that the test's stub registry does not
// declare.
func checkCollaboratorsKnown(report *authoring.Report, defPath string, def domain.TestDefinition, known map[domain.CollaboratorIdentity]bool) {
	for ti, tm := range def.Assertions.TaskMessages {
		if tm.Identity != nil && !known[*tm.Identity] {
			report.Add(authoring.Diagnostic{
				Severity: authoring.SeverityError,
				Code:     "unknown-collaborator",
				Path:     defPath,
				Pointer:  fmt.Sprintf("assertions.task_messages[%d].identity", ti),
				Message:  fmt.Sprintf("collaborator %+v is not declared in the stub registry", *tm.Identity),
			})
		}
	}

	seq := def.Assertions.InvocationSequence
	if seq == nil {
		return
	}
	for si, step := range seq.Steps {
		if step.Identity != nil && !known[*step.Identity] {
			report.Add(authoring.Diagnostic{
				Severity: authoring.SeverityError,
				Code:     "unknown-collaborator",
				Path:     defPath,
				Pointer:  fmt.Sprintf("assertions.invocation_sequence.steps[%d]", si),
				Message:  fmt.Sprintf("collaborator %+v is not declared in the stub registry", *step.Identity),
			})
		}
		for mi, m := range step.Members {
			if !known[m] {
				report.Add(authoring.Diagnostic{
					Severity: authoring.SeverityError,
					Code:     "unknown-collaborator",
					Path:     defPath,
					Pointer:  fmt.Sprintf("assertions.invocation_sequence.steps[%d].members[%d]", si, mi),
					Message:  fmt.Sprintf("collaborator %+v is not declared in the stub registry", m),
				})
			}
		}
	}
}

// checkSeedFileRefs reports "unresolvable-ref" for every seed file $ref that
// does not resolve against the fixture root.
func checkSeedFileRefs(report *authoring.Report, resolver fixtures.Resolver, defPath string, def domain.TestDefinition) {
	for si, sf := range def.SeedFiles {
		if sf.Ref == "" {
			continue
		}
		if err := resolver.Validate(sf.Ref); err != nil {
			report.Add(authoring.Diagnostic{
				Severity: authoring.SeverityError,
				Code:     "unresolvable-ref",
				Path:     defPath,
				Pointer:  fmt.Sprintf("seed_files[%d].ref", si),
				Message:  err.Error(),
			})
		}
	}
}

// checkSideEffectRefs reports "unresolvable-ref" for every stub side-effect
// $ref that does not resolve against the fixture root.
func checkSideEffectRefs(report *authoring.Report, resolver fixtures.Resolver, regPath string, reg domain.StubRegistry) {
	for sti, stub := range reg.Stubs {
		for ei, effect := range stub.SideEffects {
			if effect.Ref == "" {
				continue
			}
			if err := resolver.Validate(effect.Ref); err != nil {
				report.Add(authoring.Diagnostic{
					Severity: authoring.SeverityError,
					Code:     "unresolvable-ref",
					Path:     regPath,
					Pointer:  fmt.Sprintf("stubs[%d].side_effects[%d].ref", sti, ei),
					Message:  err.Error(),
				})
			}
		}
	}
}

// checkParallelGroups reports "unknown-parallel-group-member" for every
// parallel-group member that never appears in the test's invocation-sequence
// assertion.
func checkParallelGroups(report *authoring.Report, defPath string, def domain.TestDefinition) {
	if def.Assertions.InvocationSequence == nil {
		return
	}
	inSequence := sequenceIdentities(def.Assertions.InvocationSequence)
	for gi, group := range def.ParallelGroups {
		for mi, m := range group.Members {
			if !inSequence[m] {
				report.Add(authoring.Diagnostic{
					Severity: authoring.SeverityError,
					Code:     "unknown-parallel-group-member",
					Path:     defPath,
					Pointer:  fmt.Sprintf("parallel_groups[%d].members[%d]", gi, mi),
					Message:  fmt.Sprintf("parallel group %q member %+v never appears in the expected invocation sequence", group.Name, m),
				})
			}
		}
	}
}

// mergeSettings applies override on top of base: only override's non-nil /
// non-empty fields take effect, so a level that states nothing leaves the
// level beneath it untouched.
func mergeSettings(base, override domain.RunSettings) domain.RunSettings {
	result := base
	if override.HarnessID != "" {
		result.HarnessID = override.HarnessID
	}
	if override.Timeout != nil {
		result.Timeout = override.Timeout
	}
	if override.TurnLimit != nil {
		result.TurnLimit = override.TurnLimit
	}
	if override.Repetitions != nil {
		result.Repetitions = override.Repetitions
	}
	if override.PassRate != nil {
		result.PassRate = override.PassRate
	}
	if override.StopAfterInvocations != nil {
		result.StopAfterInvocations = override.StopAfterInvocations
	}
	return result
}

// applyOverrides folds CLI-flag-sourced overrides on top of settings already
// resolved from the suite and definition.
func applyOverrides(settings domain.RunSettings, o Overrides) domain.RunSettings {
	if o.Timeout != nil {
		settings.Timeout = o.Timeout
	}
	if o.TurnLimit != nil {
		settings.TurnLimit = o.TurnLimit
	}
	if o.Repetitions != nil {
		settings.Repetitions = o.Repetitions
	}
	return settings
}

// addRangeDiagnostics reports "setting-out-of-range" for every stated
// setting outside its legal range.
func addRangeDiagnostics(report *authoring.Report, path string, s domain.RunSettings) {
	if s.Repetitions != nil && *s.Repetitions < 0 {
		report.Add(rangeDiagnostic(path, "repetitions", "must not be negative"))
	}
	if s.PassRate != nil && (*s.PassRate < 0 || *s.PassRate > 1) {
		report.Add(rangeDiagnostic(path, "pass_rate", "must be between 0.0 and 1.0"))
	}
	if s.Timeout != nil && *s.Timeout < 0 {
		report.Add(rangeDiagnostic(path, "timeout", "must not be negative"))
	}
	if s.TurnLimit != nil && *s.TurnLimit < 0 {
		report.Add(rangeDiagnostic(path, "turn_limit", "must not be negative"))
	}
}

func rangeDiagnostic(path, field, reason string) authoring.Diagnostic {
	return authoring.Diagnostic{
		Severity: authoring.SeverityError,
		Code:     "setting-out-of-range",
		Path:     path,
		Pointer:  field,
		Message:  fmt.Sprintf("%s %s", field, reason),
	}
}
