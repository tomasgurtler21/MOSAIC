// Package preflight composes internal/authoring and internal/fixtures into
// one aggregated, deterministically ordered cross-format validation report.
// It performs no process spawning and no sandbox creation.
package preflight

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"mosaic-agent-test/internal/agentdeploy"
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

	// CatalogFolder is the process-wide default catalog folder resolved
	// by the composition root (WiringConfig.CatalogFolder). Preflight
	// resolves Overrides.CatalogFolder against this to produce
	// Plan.CatalogFolder.
	CatalogFolder string

	// Capabilities are the selected adapter's declared capabilities, supplied
	// by the composition root. Preflight decides on these values alone; no
	// harness name reaches a decision here.
	Capabilities domain.HarnessCapabilities

	// Environment is the selected adapter's environment report, supplied by
	// the composition root. A report carrying problems becomes preflight
	// errors, so an unusable environment is refused before any subject is
	// spawned and any cost is incurred.
	Environment domain.EnvironmentReport

	// Deploy renders declarations in dry-run mode to validate them against
	// the real catalogue, so a subject naming an agent that does not exist,
	// or a workflow id that does not exist, is refused before any sandbox is
	// created and any cost is incurred. Supplied by the composition root.
	//
	// A nil Deploy skips exactly the checks that need it and no others,
	// following the same "not supplied means not checked" convention as a
	// zero-valued Capabilities.
	Deploy domain.AgentDeployer

	// DeployScratchRoot is the workspace root the dry-run renders are pointed
	// at. Nothing is written there — DryRun suppresses both the write and the
	// parent-directory creation — so it need not exist; it exists as a field
	// rather than a constant so a test can assert the value the port received.
	// Supplied by the composition root alongside Deploy.
	DeployScratchRoot string

	// MosaicRootProvenance is the human-readable description of which
	// configuration tier produced the MosaicRoot value passed to the
	// deployment tool. It is embedded in delegate-tool failure diagnostics
	// so the user can identify which configuration to change.
	// Supplied by the composition root.
	MosaicRootProvenance string
}

// Overrides are CLI-flag-sourced values applied on top of a suite's
// defaults.
type Overrides struct {
	Timeout     *time.Duration
	TurnLimit   *int
	Repetitions *int
	TestIDs     []string // when non-empty, restrict the plan to these tests

	// SubjectModel and StubModel are the models chosen at run time by
	// whichever frontend is driving. Plain strings rather than pointers: the
	// empty string is unambiguously "not chosen", because no harness accepts
	// an empty model identifier. Validation against the harness's catalog is
	// the frontend's job and has already happened by the time these arrive.
	//
	// An empty StubModel with a non-empty SubjectModel is the ordinary case,
	// and means the stubs run on the subject's model.
	SubjectModel string
	StubModel    string

	// CatalogFolder, when non-nil, overrides the process-wide default
	// catalog folder for the run about to start. The pointed-to string
	// is an absolute path to the catalog directory. A nil pointer means
	// "use the process-wide default from WiringConfig.CatalogFolder".
	//
	// This follows the same pointer-means-override convention as
	// Repetitions, Timeout, and TurnLimit.
	CatalogFolder *string
}

// Plan is a fully resolved, cross-validated set of tests ready to run. It is
// usable only when the accompanying Report has no errors.
type Plan struct {
	Suite domain.TestSuite
	Tests []ResolvedTest

	// CatalogFolder is the resolved catalog folder for this run: the
	// override from Overrides.CatalogFolder if set, otherwise the
	// process-wide default from Input.CatalogFolder (the WiringConfig
	// value threaded through Input). The composition root uses this to
	// decide whether a per-run deployer is needed.
	CatalogFolder string
}

// ResolvedTest is one test definition plus its stub registry and the
// settings effective after suite defaults and overrides are applied.
type ResolvedTest struct {
	Definition domain.TestDefinition
	Registry   domain.StubRegistry
	Settings   domain.RunSettings

	// Models are the run-time model selections in force for this run. They are
	// run-level rather than per-test, and are therefore identical across every
	// ResolvedTest in one Plan; they live here so the value reaches the runner
	// through the existing per-attempt argument rather than through a second
	// channel the two frontends would have to keep in step separately.
	Models domain.ModelSelection
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

	// Resolve CatalogFolder unconditionally, before any early return.
	// Every returned Plan -- even one accompanying a non-empty Report
	// (e.g. the missing-suite early return) -- carries a resolved value.
	plan.CatalogFolder = in.CatalogFolder
	if in.Overrides.CatalogFolder != nil {
		plan.CatalogFolder = *in.Overrides.CatalogFolder
	}

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
			ids := registryIdentities(registry)
			checkCollaboratorsKnown(&report, defPath, def, ids)
			checkDispatchToolsProducible(&report, regPath, ids, in.Capabilities)
		}

		if resolver != nil {
			checkSeedFileRefs(&report, resolver, defPath, def)
			if haveRegistry {
				checkSideEffectRefs(&report, resolver, regPath, registry)
			}
		}

		checkParallelGroups(&report, defPath, def)
		checkSuppressedAssertions(&report, defPath, def)
		checkDeploymentDeclarations(&report, defPath, defDir, def, in)
		if haveRegistry {
			checkUndeclaredStubAgents(&report, defPath, def, registry, in)
		}

		effective := mergeSettings(mergeSettings(suite.Defaults, entry.Settings), def.Settings)
		effective = applyOverrides(effective, in.Overrides)
		addRangeDiagnostics(&report, defPath, effective)

		plan.Tests = append(plan.Tests, ResolvedTest{
			Definition: def,
			Registry:   registry,
			Settings:   effective,
			Models: domain.ModelSelection{
				Subject: in.Overrides.SubjectModel,
				Stub:    in.Overrides.StubModel,
			},
		})
	}

	plan.Tests = filterByTestIDs(plan.Tests, in.Overrides.TestIDs)

	addEnvironmentDiagnostics(&report, in.Environment)

	return plan, report
}

// addEnvironmentDiagnostics reports "environment-unusable" for every
// domain.EnvironmentProblem the selected adapter's environment check found,
// preserving each problem's own Detail text verbatim, so an unusable
// environment is refused before a run rather than discovered mid-run.
func addEnvironmentDiagnostics(report *authoring.Report, env domain.EnvironmentReport) {
	for _, p := range env.Problems {
		report.Add(authoring.Diagnostic{
			Severity: authoring.SeverityError,
			Code:     "environment-unusable",
			Message:  fmt.Sprintf("environment check failed (%s): %s", p.Kind, p.Detail),
		})
	}
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

// checkDispatchToolsProducible reports "unproducible-dispatch-tool" for
// every declared collaborator identity whose tool name is absent from the
// selected harness's Capabilities.ProducibleToolNames — the identity's
// dispatch tool would go unmatched at run time, so it is rejected before a
// run instead. Preflight decides on Capabilities alone; no harness name
// enters this decision.
func checkDispatchToolsProducible(report *authoring.Report, regPath string, ids map[domain.CollaboratorIdentity]bool, caps domain.HarnessCapabilities) {
	if len(caps.ProducibleToolNames) == 0 {
		// No producible set was supplied: the caller has not wired the
		// harness-capability route for this check. Every real adapter
		// declares at least one producible name, so this only happens when
		// Capabilities was left at its zero value, and skipping is the
		// honest choice — an empty set is "not supplied", not "this
		// harness can dispatch nothing".
		return
	}

	producible := make(map[string]bool, len(caps.ProducibleToolNames))
	for _, name := range caps.ProducibleToolNames {
		producible[name] = true
	}

	reported := make(map[string]bool, len(ids))
	for id := range ids {
		if producible[id.ToolName] || reported[id.ToolName] {
			continue
		}
		reported[id.ToolName] = true
		report.Add(authoring.Diagnostic{
			Severity: authoring.SeverityError,
			Code:     "unproducible-dispatch-tool",
			Path:     regPath,
			Message: fmt.Sprintf(
				"declared dispatch-tool identity %q is not producible by the selected harness (producible: %v)",
				id.ToolName, caps.ProducibleToolNames,
			),
		})
	}
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

// checkSuppressedAssertions reports "suppressed-assertion" (a warning, not an
// error) for every assertion class def declares that def.Layer suppresses
// entirely — the source of truth is domain.LayerSuppresses, so this check and
// internal/evaluate's behaviour can never drift apart. A warning, rather than
// an error, keeps the shipped layer-rule fixtures — which declare suppression
// deliberately, to exercise it — validating cleanly.
func checkSuppressedAssertions(report *authoring.Report, defPath string, def domain.TestDefinition) {
	for _, class := range declaredAssertionClasses(def.Assertions) {
		if !domain.LayerSuppresses(def.Layer, class) {
			continue
		}
		report.Add(authoring.Diagnostic{
			Severity: authoring.SeverityWarning,
			Code:     "suppressed-assertion",
			Path:     defPath,
			Message:  fmt.Sprintf("declared assertion class %q is suppressed: %s", class, domain.SuppressionReason(def.Layer, class)),
		})
	}
}

// declaredAssertionClasses reports every assertion class a declares, using
// the nil-means-not-declared convention domain.Assertions documents.
func declaredAssertionClasses(a domain.Assertions) []domain.AssertionClass {
	var out []domain.AssertionClass
	if a.InvocationSequence != nil {
		out = append(out, domain.ClassInvocationSequence)
	}
	if a.ExecutionLogAgentIDs != nil {
		out = append(out, domain.ClassExecutionLogAgentIDs)
	}
	if a.ExecutionLogAllStatus != nil {
		out = append(out, domain.ClassExecutionLogAllStatus)
	}
	if a.FinalPhase != nil {
		out = append(out, domain.ClassFinalPhase)
	}
	if a.FinalStatus != nil {
		out = append(out, domain.ClassFinalStatus)
	}
	if a.ProtocolViolations != nil {
		out = append(out, domain.ClassProtocolViolations)
	}
	if a.ArtifactCreated != nil {
		out = append(out, domain.ClassArtifactCreated)
	}
	if a.ArtifactNotCreated != nil {
		out = append(out, domain.ClassArtifactNotCreated)
	}
	if a.MinConcurrency != nil {
		out = append(out, domain.ClassMinConcurrency)
	}
	if len(a.TaskMessages) > 0 {
		out = append(out, domain.ClassTaskMessage)
	}
	return out
}

// mergeSettings applies override on top of base: only override's non-nil /
// non-empty fields take effect, so a level that states nothing leaves the
// level beneath it untouched.
func mergeSettings(base, override domain.RunSettings) domain.RunSettings {
	result := base
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

// checkDeploymentDeclarations validates the subject's agent declaration and
// each stub agent's source path against the real catalogue, reporting every
// error found rather than stopping at the first.
//
// The check branches on the definition's provisioning path:
//
//   - ProvisioningConflicted: when Deploy is non-nil, adds a hard
//     conflicting-provisioning-paths diagnostic before any dry-run, then
//     continues with stub file-existence checks (os.Stat only — no Render)
//     to surface all authoring errors in one pass. When Deploy is nil, the
//     conflict is not flagged: nil Deploy means no deployment checks at all,
//     and conflicting declarations are only meaningful when a deployment tool
//     is present.
//
//   - ProvisioningCatalog: first validates the catalogue agent key and
//     workflow ids via a dry-run Render (for unknown-subject-agent,
//     unknown-workflow, and environment-problem diagnostics); if Render
//     produces no diagnostic, validates the full deploy via a dry-run Deploy.
//     When Deploy is nil, both checks are skipped.
//
//   - ProvisioningStubAgents (default): the original field-inspection check
//     (missing-subject-agent) and the per-stub os.Stat + dry-run Render loop,
//     both unchanged.
//
// Render-based and Deploy-based checks run only when in.Deploy is non-nil,
// following the "not supplied means not checked" convention.
func checkDeploymentDeclarations(report *authoring.Report, defPath, defDir string, def domain.TestDefinition, in Input) {
	switch def.ProvisioningPath() {
	case domain.ProvisioningConflicted:
		// The conflict diagnostic is only meaningful when a deployment tool is
		// present. Tests that supply no Deploy (nil) may validly declare both
		// fields and still pass because no deployment check runs anyway.
		if in.Deploy == nil {
			return
		}
		// Hard error before any dry-run: the two paths are mutually exclusive.
		// Naming both conflicting fields in the message so the author knows
		// exactly what to fix.
		report.Add(authoring.Diagnostic{
			Severity: authoring.SeverityError,
			Code:     "conflicting-provisioning-paths",
			Path:     defPath,
			Pointer:  "stub_agents",
			Message: fmt.Sprintf(
				"test declares both a catalogue agent key (%q via subject.agent) and stub_agents; "+
					"these provisioning paths are mutually exclusive — "+
					"remove stub_agents to use the catalogue path, or remove subject.agent to use the stub-agents path",
				def.Subject.CatalogAgentKey,
			),
		})
		// After the hard diagnostic, still check stub file existence (os.Stat)
		// to surface all authoring errors in one pass. Dry-run renders are
		// skipped: a conflicted definition is invalid regardless, so no render
		// cost should be incurred.
		for si, sa := range def.StubAgents {
			resolvedPath := filepath.Join(defDir, filepath.FromSlash(sa.SourcePath))
			if _, err := os.Stat(resolvedPath); err != nil {
				report.Add(authoring.Diagnostic{
					Severity: authoring.SeverityError,
					Code:     "missing-stub-definition",
					Path:     defPath,
					Pointer:  fmt.Sprintf("stub_agents[%d].source", si),
					Message:  fmt.Sprintf("stub definition source file %q not found", sa.SourcePath),
				})
			}
		}
		return

	case domain.ProvisioningCatalog:
		if in.Deploy == nil {
			return
		}
		// Validate the catalogue agent key and workflow ids via Render first.
		// This surfaces unknown-subject-agent, unknown-workflow, and
		// environment-problem diagnostics using the structured Render reason
		// codes. If Render produces a diagnostic, skip the Deploy check:
		// at most one subject diagnostic per test.
		preCount := len(report.Diagnostics)
		checkSubjectRender(report, defPath, def, in)
		if len(report.Diagnostics) > preCount {
			return
		}
		// Render passed. Now validate the full deploy in dry-run mode.
		checkSubjectDeploy(report, defPath, def, in)
		return

	default:
		// Stub-agents path (or neither declared): original field-inspection
		// check and per-stub Render loop, unchanged.

		// missing-subject-agent: field inspection, no render needed.
		if def.Subject.CatalogAgentKey == "" {
			report.Add(authoring.Diagnostic{
				Severity: authoring.SeverityError,
				Code:     "missing-subject-agent",
				Path:     defPath,
				Pointer:  "subject.agent",
				Message:  "subject declares no catalogue agent key; use subject.agent to name the agent to render",
			})
		} else if in.Deploy != nil {
			// Dry-run the subject to validate the catalogue agent and workflow ids.
			checkSubjectRender(report, defPath, def, in)
		}

		// Stub agent checks: os.Stat for file existence, then dry-run render if
		// the file exists and Deploy is non-nil.
		for si, sa := range def.StubAgents {
			resolvedPath := filepath.Join(defDir, filepath.FromSlash(sa.SourcePath))
			if _, err := os.Stat(resolvedPath); err != nil {
				report.Add(authoring.Diagnostic{
					Severity: authoring.SeverityError,
					Code:     "missing-stub-definition",
					Path:     defPath,
					Pointer:  fmt.Sprintf("stub_agents[%d].source", si),
					Message:  fmt.Sprintf("stub definition source file %q not found", sa.SourcePath),
				})
				continue
			}
			if in.Deploy == nil {
				continue
			}
			_, renderErr := in.Deploy.Render(context.Background(), domain.RenderAgentRequest{
				SourcePath:    resolvedPath,
				HarnessID:     in.HarnessID,
				WorkspaceRoot: in.DeployScratchRoot,
				DryRun:        true,
			})
			if renderErr != nil {
				if isToolFailure(renderErr) {
					addDeployToolProblem(report, renderErr)
					return
				}
				var re *agentdeploy.RenderError
				if errors.As(renderErr, &re) {
					report.Add(DelegateToolDiagnostic(FailureContext{
						Operation:      "stub definition dry-run render",
						Provenance:     in.MosaicRootProvenance,
						DiagnosticCode: "unrenderable-stub-definition",
						Path:           defPath,
						Pointer:        fmt.Sprintf("stub_agents[%d].source", si),
					}, re))
				} else {
					report.Add(authoring.Diagnostic{
						Severity: authoring.SeverityError,
						Code:     "unrenderable-stub-definition",
						Path:     defPath,
						Pointer:  fmt.Sprintf("stub_agents[%d].source", si),
						Message:  fmt.Sprintf("stub definition %q failed dry-run render: %v", sa.SourcePath, renderErr),
					})
				}
			}
		}
	}
}

// checkSubjectDeploy performs a dry-run Deploy for a catalogue-path test and
// maps the outcome to a preflight diagnostic. It reports at most one subject
// diagnostic per test — a single dry run reveals only the first failure in
// the tool's validation order.
//
// The deploy subcommand produces no structured reason codes, so diagnostics
// carry the tool's own message verbatim. The known Render-path codes
// (unknown-subject-agent, unknown-workflow) are NOT emitted here: they map
// reason codes that Deploy never produces.
func checkSubjectDeploy(report *authoring.Report, defPath string, def domain.TestDefinition, in Input) {
	_, err := in.Deploy.Deploy(context.Background(), domain.DeployRequest{
		HarnessID:     in.HarnessID,
		WorkspaceRoot: in.DeployScratchRoot,
		Workflows:     def.Subject.Workflows,
		TierModels:    preflightTierModelMap(def.Subject),
		DryRun:        true,
	})
	if err == nil {
		return
	}

	if isToolFailure(err) {
		addDeployToolProblem(report, err)
		return
	}

	var de *agentdeploy.DeployError
	if errors.As(err, &de) {
		report.Add(DelegateToolDiagnostic(FailureContext{
			Operation:      "subject declaration dry-run deploy",
			Provenance:     in.MosaicRootProvenance,
			DiagnosticCode: "undeployable-subject-declaration",
			Path:           defPath,
			Pointer:        "subject",
		}, de))
		return
	}
	// Fallback for unexpected error types without structured failure fields.
	report.Add(authoring.Diagnostic{
		Severity: authoring.SeverityError,
		Code:     "undeployable-subject-declaration",
		Path:     defPath,
		Pointer:  "subject",
		Message:  fmt.Sprintf("subject declaration failed dry-run deploy: %s", err.Error()),
	})
}

// preflightTierModelMap builds the two-entry tier-model map for preflight's
// dry-run Deploy, using the same derivation as the runner's setup Deploy so
// the preflight dry run validates the exact model selection the real run will
// use.
func preflightTierModelMap(subject domain.SubjectUnderTest) map[string]string {
	stubModel := subject.StubModel
	if stubModel == "" {
		stubModel = subject.Model
	}
	return map[string]string{
		domain.TierTestSubject: subject.Model,
		domain.TierTestStub:    stubModel,
	}
}

// checkSubjectRender performs a dry-run render of the subject declaration and
// maps the deployment tool's reason code to a preflight diagnostic. It reports
// at most one subject diagnostic per test: a single dry-run render can only
// reveal the first failure in the tool's validation order, and that is the
// honest maximum it can report.
func checkSubjectRender(report *authoring.Report, defPath string, def domain.TestDefinition, in Input) {
	_, err := in.Deploy.Render(context.Background(), domain.RenderAgentRequest{
		CatalogAgentKey: def.Subject.CatalogAgentKey,
		HarnessID:       in.HarnessID,
		WorkspaceRoot:   in.DeployScratchRoot,
		Workflows:       def.Subject.Workflows,
		DryRun:          true,
	})
	if err == nil {
		return
	}

	if isToolFailure(err) {
		addDeployToolProblem(report, err)
		return
	}

	var re *agentdeploy.RenderError
	if errors.As(err, &re) {
		switch re.Reason {
		case "agent-not-in-catalog":
			report.Add(authoring.Diagnostic{
				Severity: authoring.SeverityError,
				Code:     "unknown-subject-agent",
				Path:     defPath,
				Pointer:  "subject.agent",
				Message: fmt.Sprintf("catalogue agent %q not found: %s",
					def.Subject.CatalogAgentKey, re.ToolMessage),
			})
			return
		case "unknown-workflow":
			report.Add(authoring.Diagnostic{
				Severity: authoring.SeverityError,
				Code:     "unknown-workflow",
				Path:     defPath,
				Pointer:  "subject.workflows",
				Message: fmt.Sprintf("workflow %q is not in the catalogue: %s",
					re.Detail, re.ToolMessage),
			})
			return
		}
		// Any other reason code — including future codes not modelled here —
		// is treated as unrenderable-subject-declaration. This is the
		// forward-compatibility rule: an unrecognised reason degrades to a less
		// specific diagnostic rather than to a wrong one.
		report.Add(DelegateToolDiagnostic(FailureContext{
			Operation:      "subject declaration dry-run render",
			Provenance:     in.MosaicRootProvenance,
			DiagnosticCode: "unrenderable-subject-declaration",
			Path:           defPath,
			Pointer:        "subject",
		}, re))
		return
	}

	// Non-RenderError: something unexpected (e.g. ErrUnparseableOutput). Treat
	// as unrenderable-subject-declaration since it is not a tool-availability
	// failure.
	report.Add(authoring.Diagnostic{
		Severity: authoring.SeverityError,
		Code:     "unrenderable-subject-declaration",
		Path:     defPath,
		Pointer:  "subject",
		Message:  fmt.Sprintf("subject declaration failed dry-run render: %v", err),
	})
}

// isToolFailure reports whether err represents a deployment-tool availability
// problem (the tool could not be invoked or timed out) rather than a
// declaration problem. These must be reported through the environment-problem
// channel rather than as declaration diagnostics.
func isToolFailure(err error) bool {
	return errors.Is(err, agentdeploy.ErrToolUnavailable) || errors.Is(err, agentdeploy.ErrTimedOut)
}

// addDeployToolProblem reports a tool-availability failure as an
// environment-unusable diagnostic so it is not mistaken for a declaration error.
func addDeployToolProblem(report *authoring.Report, err error) {
	report.Add(authoring.Diagnostic{
		Severity: authoring.SeverityError,
		Code:     "environment-unusable",
		Message:  fmt.Sprintf("deployment tool unavailable: %v", err),
	})
}

// checkUndeclaredStubAgents reports "undeclared-stub-agent" for every
// collaborator identity present in the stub registry that has no corresponding
// stub_agents entry. A dispatch to a collaborator with no deployed definition
// file is a hard-to-diagnose run-time failure for a declaration error that is
// fully knowable before the run.
//
// The diagnostic is suppressed when both signals indicate the catalogue
// provisioning path with a deployment tool present: in that case the workflow
// itself provisions every collaborator, and stub_agents is forbidden by
// conflicting-provisioning-paths. Gating on either signal alone would
// silently disable a genuine authoring check.
func checkUndeclaredStubAgents(report *authoring.Report, defPath string, def domain.TestDefinition, registry domain.StubRegistry, in Input) {
	// On the catalogue path with a deploy tool present, the workflow provisions
	// every collaborator. stub_agents is forbidden there (conflicting-provisioning-paths),
	// so requiring it would create an unresolvable deadlock. Suppress the check.
	if def.Subject.CatalogAgentKey != "" && in.Deploy != nil {
		return
	}

	// Build a set of declared stub identities.
	declared := make(map[domain.CollaboratorIdentity]bool, len(def.StubAgents))
	for _, sa := range def.StubAgents {
		declared[sa.Identity] = true
	}

	// Report any registry collaborator identity that has no declared stub agent.
	reported := make(map[domain.CollaboratorIdentity]bool)
	for si, stub := range registry.Stubs {
		id := stub.Match.Identity
		if declared[id] || reported[id] {
			continue
		}
		reported[id] = true
		report.Add(authoring.Diagnostic{
			Severity: authoring.SeverityError,
			Code:     "undeclared-stub-agent",
			Path:     defPath,
			Pointer:  fmt.Sprintf("stubs[%d].match", si),
			Message: fmt.Sprintf(
				"collaborator %+v is in the stub registry but has no stub_agents definition; "+
					"add a stub_agents entry with its source file so the deployment tool can render it",
				id,
			),
		})
	}
}
