package plan_test

// infrastructure_staleness_test.go covers plan.InfrastructureStaleness, the pure drift
// computation for the infrastructure agent declarations of one orchestrator-role file.
//
//   - Ensure intent: a key that is not declared is Missing; a declared key that is current
//     produces no drift.
//   - A declared catalog-backed key whose version, class, triggers or on-failure differ from the
//     catalog agent is Stale, under both the ensure and the refresh intent.
//   - A declared key with no catalog counterpart (or a catalog agent that is not infrastructure)
//     and a declaration whose section did not parse never produce drift.
//   - Refresh never reports an undeclared key.
//   - Deltas carry the infrastructure field prefix; Reason names the keys.

import (
	"strings"
	"testing"

	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/plan"
)

// infraCatalogAgent returns a catalog infrastructure agent with one INVOCATION_INTERVAL trigger.
func infraCatalogAgent(key, version string) domain.Agent {
	a := makeInfrastructureAgent(key, "review")
	a.Version = version
	a.OnFailure = "continue"
	a.Triggers = []domain.InfrastructureTrigger{{Trigger: "INVOCATION_INTERVAL", TriggerParam: "30"}}
	return a
}

// currentDeclaration returns a parsed declaration identical to infraCatalogAgent(key, version).
func currentDeclaration(key, version string) domain.DeployedInfrastructureDeclaration {
	return domain.DeployedInfrastructureDeclaration{
		Key:       key,
		Version:   version,
		Parsed:    true,
		Class:     "review",
		OnFailure: "continue",
		Triggers:  []domain.InfrastructureTrigger{{Trigger: "INVOCATION_INTERVAL", TriggerParam: "30"}},
	}
}

func infraCatalog(agents ...domain.Agent) *fakeCatalog {
	return &fakeCatalog{orchestrator: makeOrchestrator(), infraAgents: agents}
}

func ensureIntent(keys ...string) plan.InfrastructureDeclarationIntent {
	return plan.InfrastructureDeclarationIntent{EnsureKeys: keys}
}

var refreshIntent = plan.InfrastructureDeclarationIntent{RefreshDeclared: true}

func TestInfrastructureStaleness_EnsureKeyNotDeclared_Missing(t *testing.T) {
	cat := infraCatalog(infraCatalogAgent("code-review", "1.0"))

	drift := plan.InfrastructureStaleness(nil, cat, ensureIntent("code-review"))

	if !drift.IsStale() {
		t.Fatal("IsStale() = false, want true for an ensure key that is not declared")
	}
	if got := drift.Missing; len(got) != 1 || got[0] != "code-review" {
		t.Errorf("Missing = %v, want [code-review]", got)
	}
	if len(drift.Stale) != 0 {
		t.Errorf("Stale = %v, want none", drift.Stale)
	}
}

func TestInfrastructureStaleness_EnsureKeyDeclaredAndCurrent_NoDrift(t *testing.T) {
	cat := infraCatalog(infraCatalogAgent("code-review", "1.0"))
	declared := domain.DeployedInfrastructureDeclarations{currentDeclaration("code-review", "1.0")}

	drift := plan.InfrastructureStaleness(declared, cat, ensureIntent("code-review"))

	if drift.IsStale() {
		t.Errorf("IsStale() = true, want false; drift = %+v", drift)
	}
	if got := drift.Keys(); len(got) != 0 {
		t.Errorf("Keys() = %v, want none", got)
	}
}

func TestInfrastructureStaleness_ZeroIntent_NoDrift(t *testing.T) {
	cat := infraCatalog(infraCatalogAgent("code-review", "2.0"))
	declared := domain.DeployedInfrastructureDeclarations{currentDeclaration("code-review", "1.0")}

	drift := plan.InfrastructureStaleness(declared, cat, plan.InfrastructureDeclarationIntent{})

	if drift.IsStale() {
		t.Errorf("zero intent produced drift: %+v", drift)
	}
}

func TestInfrastructureStaleness_DeclaredKeyDiffers_StaleUnderBothIntents(t *testing.T) {
	cases := []struct {
		name       string
		mutate     func(*domain.DeployedInfrastructureDeclaration)
		wantFields []string
	}{
		{"version", func(d *domain.DeployedInfrastructureDeclaration) { d.Version = "0.9" }, []string{"version"}},
		{"empty version", func(d *domain.DeployedInfrastructureDeclaration) { d.Version = "" }, []string{"version"}},
		{"class", func(d *domain.DeployedInfrastructureDeclaration) { d.Class = "commit" }, []string{"class"}},
		{"on failure", func(d *domain.DeployedInfrastructureDeclaration) { d.OnFailure = "halt" }, []string{"on_failure"}},
		{"trigger", func(d *domain.DeployedInfrastructureDeclaration) {
			d.Triggers = []domain.InfrastructureTrigger{{Trigger: "STAGE_END"}}
		}, []string{"triggers"}},
		{"trigger param", func(d *domain.DeployedInfrastructureDeclaration) {
			d.Triggers = []domain.InfrastructureTrigger{{Trigger: "INVOCATION_INTERVAL", TriggerParam: "10"}}
		}, []string{"triggers"}},
		{"extra trigger", func(d *domain.DeployedInfrastructureDeclaration) {
			d.Triggers = append(d.Triggers, domain.InfrastructureTrigger{Trigger: "STAGE_END"})
		}, []string{"triggers"}},
		{"several", func(d *domain.DeployedInfrastructureDeclaration) {
			d.OnFailure = "halt"
			d.Version = "0.1"
			d.Class = "commit"
		}, []string{"version", "class", "on_failure"}},
	}
	intents := map[string]plan.InfrastructureDeclarationIntent{
		"ensure":  ensureIntent("code-review"),
		"refresh": refreshIntent,
	}
	for _, tc := range cases {
		for intentName, intent := range intents {
			t.Run(tc.name+"/"+intentName, func(t *testing.T) {
				cat := infraCatalog(infraCatalogAgent("code-review", "1.0"))
				d := currentDeclaration("code-review", "1.0")
				tc.mutate(&d)

				drift := plan.InfrastructureStaleness(domain.DeployedInfrastructureDeclarations{d}, cat, intent)

				if len(drift.Stale) != 1 {
					t.Fatalf("Stale = %+v, want one change", drift.Stale)
				}
				got := drift.Stale[0]
				if got.Key != "code-review" || got.SourceVersion != "1.0" || got.DeployedVersion != d.Version {
					t.Errorf("change = %+v, want key code-review, source 1.0, deployed %q", got, d.Version)
				}
				if strings.Join(got.Fields, ",") != strings.Join(tc.wantFields, ",") {
					t.Errorf("Fields = %v, want %v", got.Fields, tc.wantFields)
				}
				if len(drift.Missing) != 0 {
					t.Errorf("Missing = %v, want none", drift.Missing)
				}
			})
		}
	}
}

func TestInfrastructureStaleness_DeclaredKeyNotInCatalog_NoDrift(t *testing.T) {
	cat := infraCatalog(infraCatalogAgent("code-review", "1.0"))
	declared := domain.DeployedInfrastructureDeclarations{currentDeclaration("team-extra", "9.9")}

	for name, intent := range map[string]plan.InfrastructureDeclarationIntent{
		"refresh": refreshIntent,
		"ensure":  ensureIntent("code-review"),
	} {
		drift := plan.InfrastructureStaleness(declared, cat, intent)
		if len(drift.Stale) != 0 {
			t.Errorf("%s: Stale = %+v, want none for a key with no catalog counterpart", name, drift.Stale)
		}
	}
}

func TestInfrastructureStaleness_CatalogAgentNotInfrastructure_NoDrift(t *testing.T) {
	cat := &fakeCatalog{
		orchestrator: makeOrchestrator(),
		workers:      []domain.Agent{makeAgent("plain-worker", "2.0")},
	}
	declared := domain.DeployedInfrastructureDeclarations{currentDeclaration("plain-worker", "1.0")}

	if drift := plan.InfrastructureStaleness(declared, cat, refreshIntent); drift.IsStale() {
		t.Errorf("a catalog agent without an infrastructure class produced drift: %+v", drift)
	}
	if drift := plan.InfrastructureStaleness(nil, cat, ensureIntent("plain-worker")); drift.IsStale() {
		t.Errorf("an ensure key that is not infrastructure-backed produced drift: %+v", drift)
	}
}

func TestInfrastructureStaleness_UnparsedDeclaration_NoDrift(t *testing.T) {
	cat := infraCatalog(infraCatalogAgent("code-review", "2.0"))
	declared := domain.DeployedInfrastructureDeclarations{{Key: "code-review", Version: "1.0", Parsed: false}}

	for name, intent := range map[string]plan.InfrastructureDeclarationIntent{
		"refresh": refreshIntent,
		"ensure":  ensureIntent("code-review"),
	} {
		if drift := plan.InfrastructureStaleness(declared, cat, intent); drift.IsStale() {
			t.Errorf("%s: unparsed declaration produced drift: %+v", name, drift)
		}
	}
}

func TestInfrastructureStaleness_RefreshNeverReportsMissing(t *testing.T) {
	cat := infraCatalog(infraCatalogAgent("code-review", "1.0"), infraCatalogAgent("commit-agent", "1.0"))
	declared := domain.DeployedInfrastructureDeclarations{currentDeclaration("code-review", "1.0")}

	drift := plan.InfrastructureStaleness(declared, cat, refreshIntent)
	if len(drift.Missing) != 0 || drift.IsStale() {
		t.Errorf("refresh with current declarations produced drift: %+v", drift)
	}
	if drift := plan.InfrastructureStaleness(nil, cat, refreshIntent); drift.IsStale() {
		t.Errorf("refresh of a file with no declarations produced drift: %+v", drift)
	}
}

func TestInfrastructureStaleness_BothIntents_KeyReportedOnce(t *testing.T) {
	cat := infraCatalog(infraCatalogAgent("code-review", "2.0"))
	declared := domain.DeployedInfrastructureDeclarations{currentDeclaration("code-review", "1.0")}
	intent := plan.InfrastructureDeclarationIntent{EnsureKeys: []string{"code-review"}, RefreshDeclared: true}

	drift := plan.InfrastructureStaleness(declared, cat, intent)

	if len(drift.Stale) != 1 {
		t.Errorf("Stale = %+v, want the key once", drift.Stale)
	}
	if got := drift.Keys(); len(got) != 1 || got[0] != "code-review" {
		t.Errorf("Keys() = %v, want [code-review]", got)
	}
}

func TestInfrastructureStaleness_Keys_MissingThenStale(t *testing.T) {
	cat := infraCatalog(
		infraCatalogAgent("a-agent", "1.0"), infraCatalogAgent("b-agent", "2.0"), infraCatalogAgent("c-agent", "1.0"))
	declared := domain.DeployedInfrastructureDeclarations{
		currentDeclaration("b-agent", "1.0"), currentDeclaration("c-agent", "1.0")}

	drift := plan.InfrastructureStaleness(declared, cat, ensureIntent("a-agent", "b-agent", "c-agent"))

	if got := strings.Join(drift.Keys(), ","); got != "a-agent,b-agent" {
		t.Errorf("Keys() = %q, want %q (missing first, then stale)", got, "a-agent,b-agent")
	}
}

func TestInfrastructureDrift_Deltas_PrefixedAndOrdered(t *testing.T) {
	cat := infraCatalog(infraCatalogAgent("a-agent", "1.5"), infraCatalogAgent("b-agent", "2.0"))
	declared := domain.DeployedInfrastructureDeclarations{currentDeclaration("b-agent", "1.0")}

	deltas := plan.InfrastructureStaleness(declared, cat, ensureIntent("a-agent", "b-agent")).Deltas()

	want := []domain.VersionDelta{
		{Field: plan.InfrastructureDeltaFieldPrefix + "a-agent", Deployed: "", Source: "1.5"},
		{Field: plan.InfrastructureDeltaFieldPrefix + "b-agent", Deployed: "1.0", Source: "2.0"},
	}
	if len(deltas) != len(want) {
		t.Fatalf("Deltas() = %+v, want %+v", deltas, want)
	}
	for i := range want {
		if deltas[i] != want[i] {
			t.Errorf("Deltas()[%d] = %+v, want %+v", i, deltas[i], want[i])
		}
	}
	if plan.InfrastructureDeltaFieldPrefix != "infrastructure:" {
		t.Errorf("InfrastructureDeltaFieldPrefix = %q, want %q", plan.InfrastructureDeltaFieldPrefix, "infrastructure:")
	}
}

func TestInfrastructureDrift_Reason_NamesKeys(t *testing.T) {
	cat := infraCatalog(infraCatalogAgent("a-agent", "1.5"), infraCatalogAgent("b-agent", "2.0"),
		infraCatalogAgent("c-agent", "1.0"))
	c := currentDeclaration("c-agent", "1.0")
	c.Class = "commit"
	c.Triggers = nil
	declared := domain.DeployedInfrastructureDeclarations{currentDeclaration("b-agent", "1.0"), c}

	reason := plan.InfrastructureStaleness(declared, cat, ensureIntent("a-agent", "b-agent", "c-agent")).Reason()

	for _, want := range []string{
		"infrastructure declarations missing: a-agent",
		"infrastructure declarations stale:",
		"b-agent (version 1.0 -> 2.0)",
		"c-agent (class, triggers)",
	} {
		if !strings.Contains(reason, want) {
			t.Errorf("Reason() = %q, want it to contain %q", reason, want)
		}
	}
}

func TestInfrastructureDrift_Reason_EmptyWithoutDrift(t *testing.T) {
	cat := infraCatalog(infraCatalogAgent("code-review", "1.0"))
	declared := domain.DeployedInfrastructureDeclarations{currentDeclaration("code-review", "1.0")}

	drift := plan.InfrastructureStaleness(declared, cat, ensureIntent("code-review"))

	if got := drift.Reason(); got != "" {
		t.Errorf("Reason() = %q, want empty", got)
	}
	if got := drift.Deltas(); len(got) != 0 {
		t.Errorf("Deltas() = %+v, want none", got)
	}
}
