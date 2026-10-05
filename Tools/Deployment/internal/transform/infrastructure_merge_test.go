package transform_test

// infrastructure_merge_test.go covers MergeInfrastructureDeclarations: splicing catalog blocks
// into the inner bytes of a deployed InfrastructureAgents region.

import (
	"strings"
	"testing"

	"mosaic-deploy/internal/transform"
)

// merge runs the merge on string content and returns the merged text and the result.
func merge(region string, blocks []transform.InfrastructureBlock, mode transform.InfrastructureMergeMode) (string, transform.InfrastructureMergeResult) {
	merged, result := transform.MergeInfrastructureDeclarations([]byte(region), blocks, mode)
	return string(merged), result
}

func TestMergeInfrastructureDeclarations_EnsureAppendsNewKeyAfterExistingContent(t *testing.T) {
	existing := infraSection(newInfraBlock("alpha", "1.0"), newInfraBlock("beta", "1.0"))
	added := newInfraBlock("gamma", "1.0")

	merged, result := merge(existing, []transform.InfrastructureBlock{added}, transform.InfrastructureMergeEnsure)

	if want := existing + infraSection(added); merged != want {
		t.Errorf("merged region mismatch:\ngot:  %q\nwant: %q", merged, want)
	}
	if !sameStrings(result.Added, []string{"gamma"}) {
		t.Errorf("Added = %v, want [gamma]", result.Added)
	}
	if len(result.Replaced) != 0 || len(result.Ignored) != 0 {
		t.Errorf("unexpected Replaced/Ignored: %+v", result)
	}
}

func TestMergeInfrastructureDeclarations_EnsureIntoEmptyRegion_WritesBlocksInOrder(t *testing.T) {
	blocks := []transform.InfrastructureBlock{newInfraBlock("one", "1.0"), newInfraBlock("two", "1.0")}

	merged, result := merge("", blocks, transform.InfrastructureMergeEnsure)

	if want := infraSection(blocks...); merged != want {
		t.Errorf("merged = %q, want %q", merged, want)
	}
	if !sameStrings(result.Added, []string{"one", "two"}) {
		t.Errorf("Added = %v, want [one two]", result.Added)
	}
}

func TestMergeInfrastructureDeclarations_DeclaredKey_ReplacedInPlaceWithoutDuplicate(t *testing.T) {
	existing := infraSection(newInfraBlock("alpha", "1.0"), newInfraBlock("beta", "1.0"), newInfraBlock("gamma", "1.0"))
	refreshed := newInfraBlock("beta", "1.1")
	refreshed.OnFailure = "halt"

	merged, result := merge(existing, []transform.InfrastructureBlock{refreshed}, transform.InfrastructureMergeEnsure)

	want := infraSection(newInfraBlock("alpha", "1.0"), refreshed, newInfraBlock("gamma", "1.0"))
	if merged != want {
		t.Errorf("beta not replaced in place:\ngot:  %q\nwant: %q", merged, want)
	}
	if n := strings.Count(merged, `name="beta"`); n != 1 {
		t.Errorf("section for beta appears %d times, want exactly 1", n)
	}
	if !sameStrings(result.Replaced, []string{"beta"}) || len(result.Added) != 0 {
		t.Errorf("result = %+v, want Replaced [beta] and nothing added", result)
	}
}

func TestMergeInfrastructureDeclarations_HandAddedSections_StayByteIdentical(t *testing.T) {
	cases := map[string]string{
		"hand-added table section": handAddedTableSection,
		"unparsable section":       handWrittenSection,
	}
	for name, hand := range cases {
		t.Run(name, func(t *testing.T) {
			existing := infraSection(newInfraBlock("alpha", "1.0")) + hand + infraSection(newInfraBlock("omega", "1.0"))
			blocks := []transform.InfrastructureBlock{newInfraBlock("alpha", "1.1"), newInfraBlock("fresh", "1.0")}

			merged, _ := merge(existing, blocks, transform.InfrastructureMergeEnsure)

			want := infraSection(blocks[0]) + hand + infraSection(newInfraBlock("omega", "1.0")) + infraSection(blocks[1])
			if merged != want {
				t.Errorf("merge changed bytes outside the touched sections:\ngot:  %q\nwant: %q", merged, want)
			}
		})
	}
}

func TestMergeInfrastructureDeclarations_UnparsableDeclaredKey_ReplacedByMatchingBlock(t *testing.T) {
	existing := infraSection(newInfraBlock("alpha", "1.0")) + handWrittenSection
	replacement := newInfraBlock("hand-made", "1.0")

	merged, result := merge(existing, []transform.InfrastructureBlock{replacement}, transform.InfrastructureMergeEnsure)

	if want := infraSection(newInfraBlock("alpha", "1.0")) + infraSection(replacement); merged != want {
		t.Errorf("merged = %q, want %q", merged, want)
	}
	if n := strings.Count(merged, `name="hand-made"`); n != 1 {
		t.Errorf("section for hand-made appears %d times, want 1", n)
	}
	if !sameStrings(result.Replaced, []string{"hand-made"}) {
		t.Errorf("Replaced = %v, want [hand-made]", result.Replaced)
	}
}

func TestMergeInfrastructureDeclarations_NestedCustomRegion_StaysIntact(t *testing.T) {
	existing := infraSection(newInfraBlock("alpha", "1.0")) + customRegionInInfra
	blocks := []transform.InfrastructureBlock{newInfraBlock("alpha", "1.1"), newInfraBlock("beta", "1.0")}

	merged, _ := merge(existing, blocks, transform.InfrastructureMergeEnsure)

	if n := strings.Count(merged, customRegionInInfra); n != 1 {
		t.Fatalf("custom region appears %d times, want exactly once, byte-identical: %q", n, merged)
	}
	if !strings.HasPrefix(merged, infraSection(blocks[0])+customRegionInInfra) {
		t.Errorf("alpha was not replaced in place ahead of the custom region: %q", merged)
	}
	if !strings.HasSuffix(merged, infraSection(blocks[1])) {
		t.Errorf("beta not appended after the existing content: %q", merged)
	}
}

func TestMergeInfrastructureDeclarations_InterstitialProse_StaysInPlace(t *testing.T) {
	existing := "Team note before.\n\n" + infraSection(newInfraBlock("alpha", "1.0")) +
		"\nProse between sections.\n\n" + infraSection(newInfraBlock("beta", "1.0")) + "Trailing prose.\n"
	refreshed := newInfraBlock("alpha", "2.0")

	merged, _ := merge(existing, []transform.InfrastructureBlock{refreshed}, transform.InfrastructureMergeEnsure)

	want := "Team note before.\n\n" + infraSection(refreshed) +
		"\nProse between sections.\n\n" + infraSection(newInfraBlock("beta", "1.0")) + "Trailing prose.\n"
	if merged != want {
		t.Errorf("prose around sections changed:\ngot:  %q\nwant: %q", merged, want)
	}
}

func TestMergeInfrastructureDeclarations_RefreshReplacesDeclaredKeysButNeverAppends(t *testing.T) {
	existing := infraSection(newInfraBlock("alpha", "1.0"), newInfraBlock("beta", "1.0"))
	blocks := []transform.InfrastructureBlock{newInfraBlock("beta", "1.5"), newInfraBlock("brand-new", "1.0")}

	merged, result := merge(existing, blocks, transform.InfrastructureMergeRefresh)

	want := infraSection(newInfraBlock("alpha", "1.0"), newInfraBlock("beta", "1.5"))
	if merged != want {
		t.Errorf("refresh result mismatch:\ngot:  %q\nwant: %q", merged, want)
	}
	if strings.Contains(merged, "brand-new") {
		t.Error("refresh appended an undeclared key")
	}
	if len(result.Added) != 0 {
		t.Errorf("Added = %v, want none in refresh mode", result.Added)
	}
	if !sameStrings(result.Ignored, []string{"brand-new"}) {
		t.Errorf("Ignored = %v, want [brand-new]", result.Ignored)
	}
	if !sameStrings(result.Replaced, []string{"beta"}) {
		t.Errorf("Replaced = %v, want [beta]", result.Replaced)
	}
}

func TestMergeInfrastructureDeclarations_RefreshIntoEmptyRegion_StaysEmpty(t *testing.T) {
	merged, result := merge("", []transform.InfrastructureBlock{newInfraBlock("alpha", "1.0")}, transform.InfrastructureMergeRefresh)

	if merged != "" {
		t.Errorf("refresh wrote %q into an empty region", merged)
	}
	if !sameStrings(result.Ignored, []string{"alpha"}) {
		t.Errorf("Ignored = %v, want [alpha]", result.Ignored)
	}
}

func TestMergeInfrastructureDeclarations_NoActualChange_ReturnsRegionByteIdentical(t *testing.T) {
	existing := infraSection(newInfraBlock("alpha", "1.0"), newInfraBlock("beta", "1.0")) + handWrittenSection
	same := []transform.InfrastructureBlock{newInfraBlock("alpha", "1.0"), newInfraBlock("beta", "1.0")}

	for _, mode := range []transform.InfrastructureMergeMode{transform.InfrastructureMergeEnsure, transform.InfrastructureMergeRefresh} {
		merged, result := merge(existing, same, mode)

		if merged != existing {
			t.Errorf("mode %q: region changed although every block matches:\ngot:  %q\nwant: %q", mode, merged, existing)
		}
		if !sameStrings(result.Unchanged, []string{"alpha", "beta"}) {
			t.Errorf("mode %q: Unchanged = %v, want [alpha beta]", mode, result.Unchanged)
		}
		if len(result.Replaced) != 0 || len(result.Added) != 0 {
			t.Errorf("mode %q: reported changes for an identical merge: %+v", mode, result)
		}
	}
}

func TestMergeInfrastructureDeclarations_Twice_IsIdempotent(t *testing.T) {
	existing := infraSection(newInfraBlock("alpha", "1.0")) + handWrittenSection
	blocks := []transform.InfrastructureBlock{newInfraBlock("alpha", "1.1"), newInfraBlock("beta", "1.0")}

	once, _ := merge(existing, blocks, transform.InfrastructureMergeEnsure)
	twice, result := merge(once, blocks, transform.InfrastructureMergeEnsure)

	if once == existing {
		t.Fatal("first merge changed nothing")
	}
	if twice != once {
		t.Errorf("second merge changed the region:\ngot:  %q\nwant: %q", twice, once)
	}
	if len(result.Added) != 0 || len(result.Replaced) != 0 {
		t.Errorf("second merge reported changes: %+v", result)
	}
}

func TestMergeInfrastructureDeclarations_NoneModeOrNoBlocks_ReturnsRegionUnchangedWithZeroResult(t *testing.T) {
	existing := infraSection(newInfraBlock("alpha", "1.0"))
	blocks := []transform.InfrastructureBlock{newInfraBlock("alpha", "9.9"), newInfraBlock("beta", "1.0")}

	noneMerged, noneResult := merge(existing, blocks, transform.InfrastructureMergeNone)
	emptyMerged, emptyResult := merge(existing, nil, transform.InfrastructureMergeEnsure)

	if noneMerged != existing || emptyMerged != existing {
		t.Errorf("region changed: none-mode=%q no-blocks=%q", noneMerged, emptyMerged)
	}
	for name, r := range map[string]transform.InfrastructureMergeResult{"none-mode": noneResult, "no-blocks": emptyResult} {
		if len(r.Added)+len(r.Replaced)+len(r.Unchanged)+len(r.Ignored) != 0 {
			t.Errorf("%s result not zero: %+v", name, r)
		}
	}
}

func TestMergeInfrastructureDeclarations_DuplicateInputKeys_FirstUsedLaterIgnored(t *testing.T) {
	first := newInfraBlock("beta", "1.0")
	later := newInfraBlock("beta", "7.7")

	merged, result := merge("", []transform.InfrastructureBlock{first, later}, transform.InfrastructureMergeEnsure)

	if want := infraSection(first); merged != want {
		t.Errorf("merged = %q, want only the first occurrence %q", merged, want)
	}
	if !sameStrings(result.Added, []string{"beta"}) || !sameStrings(result.Ignored, []string{"beta"}) {
		t.Errorf("result = %+v, want Added [beta] and Ignored [beta]", result)
	}
}

func TestMergeInfrastructureDeclarations_DuplicateDeclaredKey_OnlyFirstReplaced(t *testing.T) {
	existing := infraSection(newInfraBlock("dup", "1.0"), newInfraBlock("dup", "1.1"))
	refreshed := newInfraBlock("dup", "2.0")

	merged, _ := merge(existing, []transform.InfrastructureBlock{refreshed}, transform.InfrastructureMergeEnsure)

	if want := infraSection(refreshed, newInfraBlock("dup", "1.1")); merged != want {
		t.Errorf("merged = %q, want %q", merged, want)
	}
}

func TestMergeInfrastructureDeclarations_CRLFRegion_NewSectionsUseCRLF(t *testing.T) {
	existing := toCRLF(infraSection(newInfraBlock("alpha", "1.0")))

	merged, _ := merge(existing, []transform.InfrastructureBlock{newInfraBlock("beta", "1.0")}, transform.InfrastructureMergeEnsure)

	if !strings.HasPrefix(merged, existing) {
		t.Fatalf("existing CRLF content changed: %q", merged)
	}
	appended := merged[len(existing):]
	if appended == "" {
		t.Fatal("nothing appended")
	}
	if strings.Contains(strings.ReplaceAll(appended, "\r\n", ""), "\n") {
		t.Errorf("appended section contains a bare LF in a CRLF region: %q", appended)
	}
}

func TestMergeInfrastructureDeclarations_MergedRegion_ReadsBackEveryKey(t *testing.T) {
	existing := infraSection(newInfraBlock("alpha", "1.0")) + handWrittenSection
	blocks := []transform.InfrastructureBlock{newInfraBlock("alpha", "1.2"), newInfraBlock("beta", "1.0")}

	merged, _ := merge(existing, blocks, transform.InfrastructureMergeEnsure)

	var keys, versions []string
	for _, d := range transform.ReadInfrastructureDeclarations(deployedOrchestrator(merged, "")) {
		keys = append(keys, d.Key)
		versions = append(versions, d.Version)
	}
	if !sameStrings(keys, []string{"alpha", "hand-made", "beta"}) {
		t.Errorf("keys = %v, want [alpha hand-made beta]", keys)
	}
	if !sameStrings(versions, []string{"1.2", "0.1", "1.0"}) {
		t.Errorf("versions = %v, want [1.2 0.1 1.0]", versions)
	}
}
