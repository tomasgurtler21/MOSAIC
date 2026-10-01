package transform_test

// infrastructure_declarations_test.go covers ReadInfrastructureDeclarations: reading back the
// <InfrastructureAgent> sections of a deployed orchestrator's InfrastructureAgents region.

import (
	"reflect"
	"testing"

	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/transform"
)

func TestReadInfrastructureDeclarations_NoDeclarations_ReturnsNil(t *testing.T) {
	cases := map[string][]byte{
		"nil input":    nil,
		"empty input":  {},
		"empty region": deployedOrchestrator("", ""),
		"no region":    []byte(deployedFrontmatter + "\n<Identity type=\"core\">\nBody only.\n</Identity>\n"),
	}
	for name, deployed := range cases {
		t.Run(name, func(t *testing.T) {
			got := transform.ReadInfrastructureDeclarations(deployed)
			if len(got) != 0 {
				t.Errorf("got %d declarations, want none: %+v", len(got), got)
			}
		})
	}
}

func TestReadInfrastructureDeclarations_UnparseableInput_DoesNotPanicOrReturnDeclarations(t *testing.T) {
	cases := map[string][]byte{
		"unclosed region":  []byte("<InfrastructureAgents type=\"managed\">\n<InfrastructureAgent type=\"core\" name=\"x\" version=\"1\">\n"),
		"binary noise":     {0x00, 0xff, 0xfe, '<', '<', '>', 0x01},
		"mismatched close": []byte("<InfrastructureAgents type=\"managed\">\n</Identity>\n"),
	}
	for name, deployed := range cases {
		t.Run(name, func(t *testing.T) {
			got := transform.ReadInfrastructureDeclarations(deployed)
			if len(got) != 0 {
				t.Errorf("unparseable input returned declarations: %+v", got)
			}
		})
	}
}

func TestReadInfrastructureDeclarations_AssembledSection_RoundTripsFields(t *testing.T) {
	// Arrange
	block := transform.InfrastructureBlock{
		Key:         "orchestration-review",
		Version:     "1.4",
		Class:       "review",
		Description: "Advisory checks on run bookkeeping.",
		OnFailure:   "halt",
		Triggers:    []domain.InfrastructureTrigger{{Trigger: "STAGE_END", TriggerParam: ""}},
	}
	deployed := deployedOrchestrator(infraSection(block), "")

	// Act
	got := transform.ReadInfrastructureDeclarations(deployed)

	// Assert
	if len(got) != 1 {
		t.Fatalf("got %d declarations, want 1: %+v", len(got), got)
	}
	d := got[0]
	if d.Key != "orchestration-review" || d.Version != "1.4" {
		t.Errorf("key/version = %q/%q, want orchestration-review/1.4", d.Key, d.Version)
	}
	if !d.Parsed {
		t.Fatal("Parsed = false for a section written by AssembleInfrastructureBlocks")
	}
	if !reflect.DeepEqual(d.Block, block) {
		t.Errorf("block mismatch:\ngot:  %+v\nwant: %+v", d.Block, block)
	}
}

func TestReadInfrastructureDeclarations_MultiTrigger_ReadsEveryRowWithDashAsEmptyParam(t *testing.T) {
	block := newInfraBlock("progress-check", "2.0")
	block.Triggers = []domain.InfrastructureTrigger{
		{Trigger: "INVOCATION_INTERVAL", TriggerParam: "10"},
		{Trigger: "STAGE_END", TriggerParam: ""},
		{Trigger: "PHASE_END", TriggerParam: "implementation"},
	}

	got := transform.ReadInfrastructureDeclarations(deployedOrchestrator(infraSection(block), ""))

	if len(got) != 1 || !got[0].Parsed {
		t.Fatalf("want one parsed declaration, got %+v", got)
	}
	if !reflect.DeepEqual(got[0].Block.Triggers, block.Triggers) {
		t.Errorf("triggers = %+v, want %+v", got[0].Block.Triggers, block.Triggers)
	}
}

func TestReadInfrastructureDeclarations_DisplayNameLine_ReadAsName(t *testing.T) {
	block := newInfraBlock("orch-review", "1.0")
	block.Name = "Orchestration Review"

	got := transform.ReadInfrastructureDeclarations(deployedOrchestrator(infraSection(block), ""))

	if len(got) != 1 || !got[0].Parsed {
		t.Fatalf("want one parsed declaration, got %+v", got)
	}
	if got[0].Block.Name != "Orchestration Review" {
		t.Errorf("Name = %q, want %q", got[0].Block.Name, "Orchestration Review")
	}
	if got[0].Key != "orch-review" {
		t.Errorf("Key = %q, want orch-review", got[0].Key)
	}
}

func TestReadInfrastructureDeclarations_SeveralDeclarations_ReturnedInDocumentOrder(t *testing.T) {
	region := infraSection(newInfraBlock("zeta", "1.0"), newInfraBlock("alpha", "1.1"), newInfraBlock("mid", "1.2"))

	got := transform.ReadInfrastructureDeclarations(deployedOrchestrator(region, ""))

	var keys, versions []string
	for _, d := range got {
		keys = append(keys, d.Key)
		versions = append(versions, d.Version)
		if !d.Parsed {
			t.Errorf("declaration %q not parsed", d.Key)
		}
	}
	if !sameStrings(keys, []string{"zeta", "alpha", "mid"}) {
		t.Errorf("keys = %v, want document order [zeta alpha mid]", keys)
	}
	if !sameStrings(versions, []string{"1.0", "1.1", "1.2"}) {
		t.Errorf("versions = %v, want [1.0 1.1 1.2]", versions)
	}
}

func TestReadInfrastructureDeclarations_HandWrittenSection_ListedByKeyButUnparsed(t *testing.T) {
	region := infraSection(newInfraBlock("first", "1.0")) + handWrittenSection

	got := transform.ReadInfrastructureDeclarations(deployedOrchestrator(region, ""))

	if len(got) != 2 {
		t.Fatalf("got %d declarations, want 2: %+v", len(got), got)
	}
	hand := got[1]
	if hand.Key != "hand-made" || hand.Version != "0.1" {
		t.Errorf("key/version = %q/%q, want hand-made/0.1", hand.Key, hand.Version)
	}
	if hand.Parsed {
		t.Error("hand-written prose section reported as Parsed")
	}
	if hand.Block.Class != "" || len(hand.Block.Triggers) != 0 {
		t.Errorf("unparsed declaration carries parsed fields: %+v", hand.Block)
	}
	if !got[0].Parsed {
		t.Error("the assembler-written section next to it must still parse")
	}
}

func TestReadInfrastructureDeclarations_InconsistentClassAcrossRows_Unparsed(t *testing.T) {
	section := `<InfrastructureAgent type="core" name="mixed" version="1.0">

| Class | Trigger | Param | On Failure | Description |
|-------|---------|-------|------------|-------------|
| review | STAGE_END | - | halt | First row. |
| audit | PHASE_END | - | halt | Second row. |

</InfrastructureAgent>
`
	got := transform.ReadInfrastructureDeclarations(deployedOrchestrator(section, ""))

	if len(got) != 1 {
		t.Fatalf("got %d declarations, want 1", len(got))
	}
	if got[0].Parsed {
		t.Error("section with differing Class values across rows reported as Parsed")
	}
	if got[0].Key != "mixed" {
		t.Errorf("Key = %q, want mixed", got[0].Key)
	}
}

func TestReadInfrastructureDeclarations_TableWithoutDataRows_Unparsed(t *testing.T) {
	section := `<InfrastructureAgent type="core" name="empty-table" version="1.0">

| Class | Trigger | Param | On Failure | Description |
|-------|---------|-------|------------|-------------|

</InfrastructureAgent>
`
	got := transform.ReadInfrastructureDeclarations(deployedOrchestrator(section, ""))

	if len(got) != 1 || got[0].Parsed {
		t.Errorf("want one unparsed declaration, got %+v", got)
	}
}

func TestReadInfrastructureDeclarations_PipeInDescription_StillReadsWithoutFailing(t *testing.T) {
	block := newInfraBlock("piped", "1.0")
	block.Description = "Checks a | b before the stage ends."

	got := transform.ReadInfrastructureDeclarations(deployedOrchestrator(infraSection(block), ""))

	if len(got) != 1 {
		t.Fatalf("got %d declarations, want 1", len(got))
	}
	if got[0].Key != "piped" || got[0].Version != "1.0" {
		t.Errorf("key/version = %q/%q, want piped/1.0", got[0].Key, got[0].Version)
	}
	if got[0].Parsed && got[0].Block.Description != block.Description {
		t.Errorf("Description = %q, want the cells after the fourth joined back: %q",
			got[0].Block.Description, block.Description)
	}
}

func TestReadInfrastructureDeclarations_CRLFDocument_RoundTripsFields(t *testing.T) {
	block := newInfraBlock("crlf-agent", "3.1")
	block.Triggers = []domain.InfrastructureTrigger{
		{Trigger: "STAGE_END", TriggerParam: ""},
		{Trigger: "INVOCATION_INTERVAL", TriggerParam: "5"},
	}
	deployed := []byte(toCRLF(string(deployedOrchestrator(infraSection(block), ""))))

	got := transform.ReadInfrastructureDeclarations(deployed)

	if len(got) != 1 || !got[0].Parsed {
		t.Fatalf("want one parsed declaration from CRLF input, got %+v", got)
	}
	if !reflect.DeepEqual(got[0].Block, block) {
		t.Errorf("block mismatch:\ngot:  %+v\nwant: %+v", got[0].Block, block)
	}
}

func TestReadInfrastructureDeclarations_DuplicateKeys_AllReturned(t *testing.T) {
	region := infraSection(newInfraBlock("dup", "1.0"), newInfraBlock("dup", "1.1"))

	got := transform.ReadInfrastructureDeclarations(deployedOrchestrator(region, ""))

	if len(got) != 2 {
		t.Fatalf("got %d declarations, want both duplicates: %+v", len(got), got)
	}
	if got[0].Version != "1.0" || got[1].Version != "1.1" {
		t.Errorf("versions = %q, %q; want document order 1.0, 1.1", got[0].Version, got[1].Version)
	}
}

func TestReadInfrastructureDeclarations_SectionInsideCustomRegion_NotReturned(t *testing.T) {
	nested := "<TeamNotes type=\"custom\">\n" + infraSection(newInfraBlock("hidden", "1.0")) + "</TeamNotes>\n"
	region := infraSection(newInfraBlock("visible", "1.0")) + nested

	got := transform.ReadInfrastructureDeclarations(deployedOrchestrator(region, ""))

	if len(got) != 1 || got[0].Key != "visible" {
		t.Errorf("want only the direct child 'visible', got %+v", got)
	}
}

func TestReadInfrastructureDeclarations_SectionOutsideInfrastructureRegion_NotReturned(t *testing.T) {
	deployed := deployedOrchestrator("", infraSection(newInfraBlock("elsewhere", "1.0")))

	got := transform.ReadInfrastructureDeclarations(deployed)

	if len(got) != 0 {
		t.Errorf("declaration outside the InfrastructureAgents region returned: %+v", got)
	}
}

func TestReadInfrastructureDeclarations_SectionWithoutVersionAttribute_KeyReadVersionEmpty(t *testing.T) {
	region := "<InfrastructureAgent type=\"core\" name=\"no-version\">\nSome prose.\n</InfrastructureAgent>\n"

	got := transform.ReadInfrastructureDeclarations(deployedOrchestrator(region, ""))

	if len(got) != 1 {
		t.Fatalf("got %d declarations, want 1: %+v", len(got), got)
	}
	if got[0].Key != "no-version" || got[0].Version != "" {
		t.Errorf("key/version = %q/%q, want no-version/empty", got[0].Key, got[0].Version)
	}
}
