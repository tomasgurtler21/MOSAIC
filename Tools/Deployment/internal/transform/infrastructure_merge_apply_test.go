package transform_test

// infrastructure_merge_apply_test.go covers the InfrastructureAgents region dispatch of
// transform.Apply: the opt-in Request.InfrastructureMerge modes, and regression checks that
// the existing replace / preserve / clear behaviour is unchanged when merge is not requested.

import (
	"reflect"
	"strings"
	"testing"

	"mosaic-deploy/internal/transform"
)

const infraRegionName = "InfrastructureAgents"

func TestApply_InfrastructureEnsure_AppendsNewKeyAndKeepsExistingSections(t *testing.T) {
	existing := infraSection(newInfraBlock("alpha", "1.0")) + handWrittenSection
	added := newInfraBlock("beta", "1.0")

	result := applyOrchestrator(t, orchestratorWithInfrastructureAgents, func(r *transform.Request) {
		r.Deployed = deployedOrchestrator(existing, "")
		r.InfrastructureAgents = []transform.InfrastructureBlock{added}
		r.InfrastructureMerge = transform.InfrastructureMergeEnsure
	})

	if got, want := string(regionContent(t, result.Output, infraRegionName)), existing+infraSection(added); got != want {
		t.Errorf("region mismatch:\ngot:  %q\nwant: %q", got, want)
	}
	if a := regionAction(t, result, infraRegionName); a != transform.RegionMergedInfra {
		t.Errorf("action = %q, want %q", a, transform.RegionMergedInfra)
	}
	if !sameStrings(result.Report.InfrastructureAgents, []string{"alpha", "hand-made", "beta"}) {
		t.Errorf("Report.InfrastructureAgents = %v, want [alpha hand-made beta]", result.Report.InfrastructureAgents)
	}
	if !sameStrings(result.Report.InfrastructureMerge.Added, []string{"beta"}) {
		t.Errorf("Report.InfrastructureMerge.Added = %v, want [beta]", result.Report.InfrastructureMerge.Added)
	}
}

func TestApply_InfrastructureEnsure_ReplacesDeclaredKeyInPlaceWithoutDuplicate(t *testing.T) {
	existing := infraSection(newInfraBlock("alpha", "1.0"), newInfraBlock("beta", "1.0"))
	refreshed := newInfraBlock("alpha", "1.3")

	result := applyOrchestrator(t, orchestratorWithInfrastructureAgents, func(r *transform.Request) {
		r.Deployed = deployedOrchestrator(existing, "")
		r.InfrastructureAgents = []transform.InfrastructureBlock{refreshed}
		r.InfrastructureMerge = transform.InfrastructureMergeEnsure
	})

	got := string(regionContent(t, result.Output, infraRegionName))
	if want := infraSection(refreshed, newInfraBlock("beta", "1.0")); got != want {
		t.Errorf("region mismatch:\ngot:  %q\nwant: %q", got, want)
	}
	if !sameStrings(result.Report.InfrastructureMerge.Replaced, []string{"alpha"}) {
		t.Errorf("Replaced = %v, want [alpha]", result.Report.InfrastructureMerge.Replaced)
	}
}

func TestApply_InfrastructureRefresh_DoesNotAppendUndeclaredKeys(t *testing.T) {
	existing := infraSection(newInfraBlock("alpha", "1.0"))

	result := applyOrchestrator(t, orchestratorWithInfrastructureAgents, func(r *transform.Request) {
		r.Deployed = deployedOrchestrator(existing, "")
		r.InfrastructureAgents = []transform.InfrastructureBlock{newInfraBlock("alpha", "1.1"), newInfraBlock("new-key", "1.0")}
		r.InfrastructureMerge = transform.InfrastructureMergeRefresh
	})

	got := string(regionContent(t, result.Output, infraRegionName))
	if want := infraSection(newInfraBlock("alpha", "1.1")); got != want {
		t.Errorf("region mismatch:\ngot:  %q\nwant: %q", got, want)
	}
	if strings.Contains(got, "new-key") {
		t.Error("refresh appended an undeclared key")
	}
	if !sameStrings(result.Report.InfrastructureAgents, []string{"alpha"}) {
		t.Errorf("Report.InfrastructureAgents = %v, want [alpha]", result.Report.InfrastructureAgents)
	}
}

func TestApply_InfrastructureMerge_NoChange_LeavesRegionByteIdentical(t *testing.T) {
	existing := infraSection(newInfraBlock("alpha", "1.0")) + handAddedTableSection

	result := applyOrchestrator(t, orchestratorWithInfrastructureAgents, func(r *transform.Request) {
		r.Deployed = deployedOrchestrator(existing, "")
		r.InfrastructureAgents = []transform.InfrastructureBlock{newInfraBlock("alpha", "1.0")}
		r.InfrastructureMerge = transform.InfrastructureMergeRefresh
	})

	if got := string(regionContent(t, result.Output, infraRegionName)); got != existing {
		t.Errorf("region changed with nothing to change:\ngot:  %q\nwant: %q", got, existing)
	}
	if !sameStrings(result.Report.InfrastructureMerge.Unchanged, []string{"alpha"}) {
		t.Errorf("Unchanged = %v, want [alpha]", result.Report.InfrastructureMerge.Unchanged)
	}
}

func TestApply_InfrastructureEnsure_NestedCustomRegion_SurvivesExactlyOnce(t *testing.T) {
	existing := infraSection(newInfraBlock("alpha", "1.0")) + customRegionInInfra

	result := applyOrchestrator(t, orchestratorWithInfrastructureAgents, func(r *transform.Request) {
		r.Deployed = deployedOrchestrator(existing, "")
		r.InfrastructureAgents = []transform.InfrastructureBlock{newInfraBlock("beta", "1.0")}
		r.InfrastructureMerge = transform.InfrastructureMergeEnsure
	})

	got := string(regionContent(t, result.Output, infraRegionName))
	if n := strings.Count(got, customRegionInInfra); n != 1 {
		t.Errorf("custom region appears %d times, want once byte-identical: %q", n, got)
	}
	if !strings.Contains(got, `name="beta"`) || !strings.Contains(got, `name="alpha"`) {
		t.Errorf("declarations missing after merge: %q", got)
	}
}

func TestApply_InfrastructureEnsure_NoDeployedFile_WritesBlocks(t *testing.T) {
	blocks := []transform.InfrastructureBlock{newInfraBlock("alpha", "1.0")}

	result := applyOrchestrator(t, orchestratorWithInfrastructureAgents, func(r *transform.Request) {
		r.InfrastructureAgents = blocks
		r.InfrastructureMerge = transform.InfrastructureMergeEnsure
	})

	if got, want := string(regionContent(t, result.Output, infraRegionName)), infraSection(blocks...); got != want {
		t.Errorf("region = %q, want %q", got, want)
	}
	if a := regionAction(t, result, infraRegionName); a != transform.RegionMergedInfra {
		t.Errorf("action = %q, want %q", a, transform.RegionMergedInfra)
	}
}

func TestApply_InfrastructureRefresh_NothingDeclared_RegionEmptied(t *testing.T) {
	result := applyOrchestrator(t, orchestratorWithInfrastructureAgents, func(r *transform.Request) {
		r.Deployed = deployedOrchestrator("", "")
		r.InfrastructureAgents = []transform.InfrastructureBlock{newInfraBlock("alpha", "1.0")}
		r.InfrastructureMerge = transform.InfrastructureMergeRefresh
	})

	if got := regionContent(t, result.Output, infraRegionName); len(strings.TrimSpace(string(got))) != 0 {
		t.Errorf("region not empty: %q", got)
	}
	if a := regionAction(t, result, infraRegionName); a != transform.RegionEmptied {
		t.Errorf("action = %q, want %q", a, transform.RegionEmptied)
	}
}

// ---------------------------------------------------------------------------
// Regression: merge not requested keeps the existing behaviour exactly.
// ---------------------------------------------------------------------------

func TestApply_InfrastructureMergeNone_BlocksReplaceWholeRegion(t *testing.T) {
	existing := infraSection(newInfraBlock("alpha", "1.0")) + handWrittenSection + customRegionInInfra
	blocks := []transform.InfrastructureBlock{newInfraBlock("beta", "2.0")}

	result := applyOrchestrator(t, orchestratorWithInfrastructureAgents, func(r *transform.Request) {
		r.Deployed = deployedOrchestrator(existing, "")
		r.InfrastructureAgents = blocks
	})

	got := string(regionContent(t, result.Output, infraRegionName))
	if !strings.HasPrefix(got, infraSection(blocks...)) {
		t.Errorf("region does not start with the assembled blocks: %q", got)
	}
	if strings.Contains(got, `name="alpha"`) || strings.Contains(got, "hand-made") {
		t.Errorf("replace mode kept previously deployed sections: %q", got)
	}
	if a := regionAction(t, result, infraRegionName); a != transform.RegionAssembledInfra {
		t.Errorf("action = %q, want %q", a, transform.RegionAssembledInfra)
	}
	if !sameStrings(result.Report.InfrastructureAgents, []string{"beta"}) {
		t.Errorf("Report.InfrastructureAgents = %v, want [beta]", result.Report.InfrastructureAgents)
	}
	if !reflect.DeepEqual(result.Report.InfrastructureMerge, transform.InfrastructureMergeResult{}) {
		t.Errorf("Report.InfrastructureMerge populated without merge: %+v", result.Report.InfrastructureMerge)
	}
}

func TestApply_InfrastructureMergeNone_NoBlocksWithDeployed_PreservesRegion(t *testing.T) {
	existing := infraSection(newInfraBlock("alpha", "1.0")) + handWrittenSection

	result := applyOrchestrator(t, orchestratorWithInfrastructureAgents, func(r *transform.Request) {
		r.Deployed = deployedOrchestrator(existing, "")
	})

	if got := string(regionContent(t, result.Output, infraRegionName)); got != existing {
		t.Errorf("region not preserved:\ngot:  %q\nwant: %q", got, existing)
	}
	if a := regionAction(t, result, infraRegionName); a != transform.RegionPreservedInfra {
		t.Errorf("action = %q, want %q", a, transform.RegionPreservedInfra)
	}
	if !reflect.DeepEqual(result.Report.InfrastructureMerge, transform.InfrastructureMergeResult{}) {
		t.Errorf("Report.InfrastructureMerge populated without merge: %+v", result.Report.InfrastructureMerge)
	}
}

func TestApply_InfrastructureMergeNone_NoBlocksNoDeployed_ClearsRegion(t *testing.T) {
	result := applyOrchestrator(t, orchestratorWithInfrastructureAgents, func(r *transform.Request) {})

	if got := regionContent(t, result.Output, infraRegionName); len(strings.TrimSpace(string(got))) != 0 {
		t.Errorf("region not empty: %q", got)
	}
	if a := regionAction(t, result, infraRegionName); a != transform.RegionEmptied {
		t.Errorf("action = %q, want %q", a, transform.RegionEmptied)
	}
}

func TestAssembleInfrastructureBlocks_ExactBytesForOneSection(t *testing.T) {
	block := newInfraBlock("alpha", "1.0")
	block.Name = "Alpha Agent"
	block.Triggers = append(block.Triggers, newInfraBlock("x", "1").Triggers[0])
	block.Triggers[1].TriggerParam = ""

	want := "<InfrastructureAgent type=\"core\" name=\"alpha\" version=\"1.0\">\n" +
		"Alpha Agent\n\n" +
		"\n" +
		"| Class | Trigger | Param | On Failure | Description |\n" +
		"|-------|---------|-------|------------|-------------|\n" +
		"| review | INVOCATION_INTERVAL | 30 | continue | Advisory checks for alpha. |\n" +
		"| review | INVOCATION_INTERVAL | - | continue | Advisory checks for alpha. |\n" +
		"\n" +
		"</InfrastructureAgent>\n"
	if got := infraSection(block); got != want {
		t.Errorf("assembled bytes changed:\ngot:  %q\nwant: %q", got, want)
	}
}
