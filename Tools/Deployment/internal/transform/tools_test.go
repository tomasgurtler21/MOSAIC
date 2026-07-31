package transform_test

// tools_test.go covers the tool mapping stage of the transformation pipeline (Stage 9).
// Tests are organised by the behavior they exercise at the full Apply level:
//
//   T9.1 – Many-to-one deduplication and Universe-order stability in the output field.
//   T9.3 – Field diversion: a diverted generic tool appears in a separate frontmatter
//           key in the output document, not in the main tools value.
//   T9.4 – Unmapped-tool gap: Apply emits a GapUnmappedTool for every generic tool in
//           the source that has no descriptor mapping and no user-supplied name.
//   T9.6 – Skipped-tool: a tool the user chose to skip is absent from the output tools
//           value and present as a gap in Report.Gaps.
//   T9.7 – Placeholder expansion: when ToolSpec.PlaceholderExpansion is empty, Apply
//           expands the {tool-permissions} placeholder to the full Universe.

import (
	"strings"
	"testing"

	"mosaic-common/docformat"
	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/harness/descriptor"
	"mosaic-deploy/internal/transform"
)

// ---------------------------------------------------------------------------
// descriptorModule — a domain.HarnessModule backed by an arbitrary inline descriptor.
//
// This is deliberately distinct from fixtureModule (which reads testdata/transform/
// fixture.yaml at fixed path) so that each test can supply its own descriptor.
// ---------------------------------------------------------------------------

type descriptorModule struct {
	desc *domain.HarnessDescriptor
}

func (m *descriptorModule) Ref() domain.HarnessRef {
	return domain.HarnessRef{
		ID:     m.desc.ID,
		Tier:   domain.TierDescriptor,
		Usable: true,
	}
}

func (m *descriptorModule) Descriptor() *domain.HarnessDescriptor { return m.desc }
func (m *descriptorModule) Close() error                          { return nil }

func (m *descriptorModule) Tools(req domain.ToolRequest) (domain.ToolResult, error) {
	return descriptor.MapTools(m.desc, req)
}

func (m *descriptorModule) Frontmatter(req domain.FrontmatterRequest) (domain.FrontmatterPlan, error) {
	return descriptor.ApplyFrontmatterSpec(m.desc, req)
}

func (m *descriptorModule) TargetPath(req domain.TargetPathRequest) (string, error) {
	return descriptor.ResolveTargetPath(m.desc, req)
}

func (m *descriptorModule) Injection(req domain.InjectionRequest) (string, bool) {
	for _, inj := range m.desc.Injections {
		if inj.Name == req.Name {
			return inj.Content, true
		}
	}
	return "", false
}

func (m *descriptorModule) HookPlan(_ domain.HookPlanRequest) (domain.HookPlan, error) {
	return domain.HookPlan{Supported: false, Reason: "descriptor module declares no hooks"}, nil
}

// newDescriptorModule parses src as a YAML harness descriptor and returns a HarnessModule
// backed by it. All harness-specific behaviour flows through the shared descriptor
// algorithms (CD-2): no test branch exists on a harness id.
func newDescriptorModule(t *testing.T, src, origin string) domain.HarnessModule {
	t.Helper()
	d, err := descriptor.Parse([]byte(src), origin)
	if err != nil {
		t.Fatalf("descriptor.Parse(%q): %v", origin, err)
	}
	return &descriptorModule{desc: d}
}

// ---------------------------------------------------------------------------
// Inline descriptor YAML used across the tests in this file.
// ---------------------------------------------------------------------------

// manyToOneDescriptorYAML is a list-shape harness where file_search and
// content_search both map to the same harness tool "search", creating a many-to-one
// collapse. The Universe order is: "read" (0), "search" (1).
const manyToOneDescriptorYAML = `schema_version: "1"
id: "many-to-one-harness"
display_name: "Many-to-One Harness"
tools:
  shape: list
  universe:
    - name: "read"
      unused: deny
      by_convention: false
    - name: "search"
      unused: deny
      by_convention: false
  mappings:
    - generic: "file_read"
      harness_tools:
        - "read"
    - generic: "file_search"
      harness_tools:
        - "search"
    - generic: "content_search"
      harness_tools:
        - "search"
frontmatter:
  tools_key: "tools"
`

// universeOrderDescriptorYAML is a list-shape harness whose Universe order (write, read)
// is the reverse of a typical request order (file_read, file_write). Tests use it to
// assert that output ordering follows the Universe, not the request.
const universeOrderDescriptorYAML = `schema_version: "1"
id: "universe-order-harness"
display_name: "Universe Order Harness"
tools:
  shape: list
  universe:
    - name: "write"
      unused: deny
      by_convention: false
    - name: "read"
      unused: deny
      by_convention: false
  mappings:
    - generic: "file_read"
      harness_tools:
        - "read"
    - generic: "file_write"
      harness_tools:
        - "write"
frontmatter:
  tools_key: "tools"
`

// fieldDiversionDescriptorYAML is a list-shape harness that routes the "skill" generic
// tool to a separate "mcp_servers" frontmatter field rather than the main tools list.
const fieldDiversionDescriptorYAML = `schema_version: "1"
id: "field-diversion-harness"
display_name: "Field Diversion Harness"
tools:
  shape: list
  universe:
    - name: "read-file"
      unused: deny
      by_convention: false
  mappings:
    - generic: "file_read"
      harness_tools:
        - "read-file"
    - generic: "skill"
      harness_tools:
        - "mcp-skill-tool"
      field: "mcp_servers"
frontmatter:
  tools_key: "tools"
`

// placeholderNoExpansionDescriptorYAML is a list-shape harness with no
// placeholder_expansion. Per the design, an empty placeholder_expansion means "the full
// Universe", so a {tool-permissions} source must expand to all Universe tools.
const placeholderNoExpansionDescriptorYAML = `schema_version: "1"
id: "no-expansion-harness"
display_name: "No Expansion Harness"
tools:
  shape: list
  universe:
    - name: "alpha"
      unused: deny
      by_convention: false
    - name: "beta"
      unused: deny
      by_convention: false
    - name: "gamma"
      unused: deny
      by_convention: false
frontmatter:
  tools_key: "tools"
`

// customToolDescriptorYAML is a list-shape harness with custom_tool_template set to
// "mcp:%s". The "terminal" generic tool has no explicit mapping; a CustomTools entry for
// "terminal" must therefore produce a ToolCustom resolution with the formatted name.
const customToolDescriptorYAML = `schema_version: "1"
id: "custom-tool-harness"
display_name: "Custom Tool Harness"
tools:
  shape: list
  universe:
    - name: "read-file"
      unused: deny
      by_convention: false
  mappings:
    - generic: "file_read"
      harness_tools:
        - "read-file"
  custom_tool_template: "mcp:%s"
frontmatter:
  tools_key: "tools"
`

// oneToManyDescriptorYAML is a list-shape harness where file_write maps to two
// harness tools (write/createFile and write/editFile), providing an end-to-end
// one-to-many expansion fixture at the transform layer.
const oneToManyDescriptorYAML = `schema_version: "1"
id: "one-to-many-harness"
display_name: "One-to-Many Harness"
tools:
  shape: list
  universe:
    - name: "write/createFile"
      unused: deny
      by_convention: false
    - name: "write/editFile"
      unused: deny
      by_convention: false
  mappings:
    - generic: "file_write"
      harness_tools:
        - "write/createFile"
        - "write/editFile"
frontmatter:
  tools_key: "tools"
`

// ---------------------------------------------------------------------------
// Source agent YAML skeletons used in tests.
// ---------------------------------------------------------------------------

// manyToOneSource uses both file_search and content_search, which both map to "search"
// in manyToOneDescriptorYAML.
const manyToOneSource = `---
id: 10
version: 1.0.0
description: Many-to-one dedup test agent
tools: [file_read, file_search, content_search]
---
Body.
`

// universeOrderSource uses file_read and file_write in request order; the Universe
// declares write before read, so the output must emit write first.
const universeOrderSource = `---
id: 11
version: 1.0.0
description: Universe-order test agent
tools: [file_read, file_write]
---
Body.
`

// diversionSource uses file_read (normal) and skill (diverted to mcp_servers).
const diversionSource = `---
id: 12
version: 1.0.0
description: Field diversion test agent
tools: [file_read, skill]
---
Body.
`

// unmappedToolSource has "skill" as a generic tool. The fixture harness declares no
// mapping for "skill", so it produces a ToolUnmapped resolution.
const unmappedToolSource = `---
id: 20
version: 1.0.0
description: Unmapped tool gap test agent
model: {model-identifier}
tools: [file_read, skill]
required_skills: []
---
Body.
`

// placeholderSource uses the {tool-permissions} scalar placeholder instead of a list.
const placeholderSource = `---
id: 30
version: 1.0.0
description: Placeholder expansion test agent
tools: {tool-permissions}
---
Body.
`

// customToolSource uses "terminal" which has no explicit mapping in customToolDescriptorYAML;
// with a CustomTools entry it resolves to ToolCustom with the formatted MCP server name.
const customToolSource = `---
id: 40
version: 1.0.0
description: Custom tool name test agent
tools: [terminal]
---
Body.
`

// oneToManySource uses "file_write" which maps to two harness tools in oneToManyDescriptorYAML.
const oneToManySource = `---
id: 50
version: 1.0.0
description: One-to-many expansion test agent
tools: [file_write]
---
Body.
`

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// listFieldScalars returns the scalar items of a KindList FrontmatterField with the
// given key from the document's frontmatter. Returns nil when the key is absent or the
// value is not a list.
func listFieldScalars(t *testing.T, doc *docformat.Document, key string) []string {
	t.Helper()
	v, ok := doc.Frontmatter().Get(key)
	if !ok {
		return nil
	}
	if v.Kind != domain.KindList {
		return nil
	}
	result := make([]string, len(v.Items))
	for i, item := range v.Items {
		result[i] = item.Scalar
	}
	return result
}

// countOccurrences returns how many times target appears in items.
func countOccurrences(items []string, target string) int {
	count := 0
	for _, s := range items {
		if s == target {
			count++
		}
	}
	return count
}

// findGapByKind returns the first Gap with the given Kind from gaps, or nil.
func findGapByKind(gaps []domain.Gap, kind domain.GapKind) *domain.Gap {
	for i := range gaps {
		if gaps[i].Kind == kind {
			return &gaps[i]
		}
	}
	return nil
}

// findGapNaming returns the first Gap whose Subject or Detail contains name, or nil.
func findGapNaming(gaps []domain.Gap, name string) *domain.Gap {
	for i := range gaps {
		if strings.Contains(gaps[i].Subject, name) || strings.Contains(gaps[i].Detail, name) {
			return &gaps[i]
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// T9.1: Deduplication and stable ordering
// ---------------------------------------------------------------------------

// TestToolMapping_ManyToOne_HarnessToolDeduplicatedInOutputField asserts that when two
// generic tools both map to the same harness tool name, that name appears exactly once
// in the output frontmatter tools field. Allowing duplicates would produce an invalid
// tool list that a harness parser may reject.
func TestToolMapping_ManyToOne_HarnessToolDeduplicatedInOutputField(t *testing.T) {
	mod := newDescriptorModule(t, manyToOneDescriptorYAML, "inline:many-to-one")
	req := transform.Request{
		Source: []byte(manyToOneSource),
		Kind:   domain.ArtifactAgent,
		Key:    "dedup-test-agent",
		Module: mod,
		Model:  domain.ModelSelection{Origin: domain.OriginUnresolved},
		Scope:  domain.ScopeProject,
	}

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	doc, err := docformat.Parse(result.Output)
	if err != nil {
		t.Fatalf("Parse output: %v", err)
	}

	items := listFieldScalars(t, doc, "tools")
	if items == nil {
		t.Fatal("tools field absent from output frontmatter or not a list")
	}
	// "search" appears in both file_search and content_search mappings; it must be
	// deduplicated to exactly one occurrence in the output.
	count := countOccurrences(items, "search")
	if count != 1 {
		t.Errorf("harness tool %q appears %d times in output tools field; want exactly 1 (deduplication required); output: %v",
			"search", count, items)
	}
}

// TestToolMapping_Ordering_FollowsUniverseDeclarationOrder asserts that when multiple
// generic tools are mapped, the output tools field lists harness tools in the order
// declared by ToolSpec.Universe, regardless of the order the generic tools appear in
// the source agent. Stable ordering is required by AC9.2.
func TestToolMapping_Ordering_FollowsUniverseDeclarationOrder(t *testing.T) {
	// Universe order is: "write" (index 0), "read" (index 1).
	// Source order is: file_read (maps to "read"), file_write (maps to "write") —
	// the reverse of Universe order.
	// Output must be: "write", "read" (Universe order wins).
	mod := newDescriptorModule(t, universeOrderDescriptorYAML, "inline:universe-order")
	req := transform.Request{
		Source: []byte(universeOrderSource),
		Kind:   domain.ArtifactAgent,
		Key:    "ordering-test-agent",
		Module: mod,
		Model:  domain.ModelSelection{Origin: domain.OriginUnresolved},
		Scope:  domain.ScopeProject,
	}

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	doc, err := docformat.Parse(result.Output)
	if err != nil {
		t.Fatalf("Parse output: %v", err)
	}

	items := listFieldScalars(t, doc, "tools")
	if len(items) < 2 {
		t.Fatalf("expected at least 2 tools in output, got %d: %v", len(items), items)
	}
	if items[0] != "write" {
		t.Errorf("tools[0]: want %q (first in Universe), got %q; output order must follow Universe, not source order; items: %v",
			"write", items[0], items)
	}
	if items[1] != "read" {
		t.Errorf("tools[1]: want %q (second in Universe), got %q; items: %v",
			"read", items[1], items)
	}
}

// ---------------------------------------------------------------------------
// T9.3: Field diversion in output frontmatter
// ---------------------------------------------------------------------------

// TestToolMapping_FieldDiversion_DiversionKeyPresentInOutputFrontmatter asserts that
// when a generic tool's descriptor mapping declares a non-empty Field, the harness tool
// for that mapping appears in the named frontmatter key in the Apply output, not in the
// main tools value. This is the end-to-end assertion for field diversion.
//
// This test will be RED until MapTools produces a FrontmatterField for diversion keys
// and applyFrontmatter writes that field to the document.
func TestToolMapping_FieldDiversion_DiversionKeyPresentInOutputFrontmatter(t *testing.T) {
	mod := newDescriptorModule(t, fieldDiversionDescriptorYAML, "inline:field-diversion")
	req := transform.Request{
		Source: []byte(diversionSource),
		Kind:   domain.ArtifactAgent,
		Key:    "diversion-test-agent",
		Module: mod,
		Model:  domain.ModelSelection{Origin: domain.OriginUnresolved},
		Scope:  domain.ScopeProject,
	}

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	doc, err := docformat.Parse(result.Output)
	if err != nil {
		t.Fatalf("Parse output: %v", err)
	}

	// "mcp_servers" must appear in the output frontmatter, containing "mcp-skill-tool".
	v, ok := doc.Frontmatter().Get("mcp_servers")
	if !ok {
		t.Fatalf("mcp_servers key absent from output frontmatter; a field-diverted tool must write its harness name to the declared diversion field; "+
			"frontmatter keys: %v", doc.Frontmatter().Keys())
	}
	if v.Kind != domain.KindList {
		t.Fatalf("mcp_servers field value kind: want %q, got %q", domain.KindList, v.Kind)
	}
	var found bool
	for _, item := range v.Items {
		if item.Scalar == "mcp-skill-tool" {
			found = true
			break
		}
	}
	if !found {
		scalars := make([]string, len(v.Items))
		for i, item := range v.Items {
			scalars[i] = item.Scalar
		}
		t.Errorf("harness tool %q not found in mcp_servers field; got: %v", "mcp-skill-tool", scalars)
	}
}

// ---------------------------------------------------------------------------
// T9.4: Unmapped-tool gap reporting
// ---------------------------------------------------------------------------

// TestToolMapping_UnmappedTool_GapKindIsGapUnmappedTool asserts that Apply emits at
// least one Gap with Kind GapUnmappedTool when the source agent declares a generic tool
// that has no entry in the descriptor's mappings. The gap is the signal to the TODO
// checklist that the user must resolve this tool manually.
//
// This test will be RED until applyFrontmatter (or an equivalent gap-accumulation step)
// converts ToolUnmapped resolutions into GapUnmappedTool gaps in Report.Gaps.
func TestToolMapping_UnmappedTool_GapKindIsGapUnmappedTool(t *testing.T) {
	// The fixture descriptor has no mapping for "skill". With source tools: [file_read, skill],
	// "skill" will resolve to ToolUnmapped.
	req := transform.Request{
		Source: []byte(unmappedToolSource),
		Kind:   domain.ArtifactAgent,
		Key:    "gap-test-agent",
		Module: newFixtureModule(t),
		Model:  fixtureModel(),
		Scope:  domain.ScopeProject,
	}

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	gap := findGapByKind(result.Report.Gaps, domain.GapUnmappedTool)
	if gap == nil {
		t.Errorf("expected Report.Gaps to contain a Gap with Kind=%q for unmapped tool %q; got %d gaps with kinds: %v",
			domain.GapUnmappedTool, "skill", len(result.Report.Gaps), gapKinds(result.Report.Gaps))
	}
}

// TestToolMapping_UnmappedTool_GapNamesTheUnmappedTool asserts that the GapUnmappedTool
// gap produced for an unmapped tool carries the generic tool name so that the TODO
// checklist can tell the user which specific tool needs a mapping.
//
// This test will be RED until gap-accumulation is implemented.
func TestToolMapping_UnmappedTool_GapNamesTheUnmappedTool(t *testing.T) {
	req := transform.Request{
		Source: []byte(unmappedToolSource),
		Kind:   domain.ArtifactAgent,
		Key:    "gap-test-agent",
		Module: newFixtureModule(t),
		Model:  fixtureModel(),
		Scope:  domain.ScopeProject,
	}

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	// The gap must carry "skill" somewhere so the user knows which tool to resolve.
	gap := findGapNaming(result.Report.Gaps, "skill")
	if gap == nil {
		subjects := make([]string, len(result.Report.Gaps))
		for i, g := range result.Report.Gaps {
			subjects[i] = g.Subject
		}
		t.Errorf("expected a gap naming %q in Report.Gaps; got %d gaps with subjects: %v",
			"skill", len(result.Report.Gaps), subjects)
	}
}

// TestToolMapping_UnmappedTool_GapNamesTheAgent asserts that the GapUnmappedTool gap
// for an unmapped tool carries enough context to identify which agent the gap belongs to.
// The subject or detail must include the agent key so the checklist entry can guide the
// user to the right deployment item.
//
// This test will be RED until gap-accumulation is implemented.
func TestToolMapping_UnmappedTool_GapNamesTheAgent(t *testing.T) {
	const agentKey = "gap-agent-key-test"
	req := transform.Request{
		Source: []byte(unmappedToolSource),
		Kind:   domain.ArtifactAgent,
		Key:    agentKey,
		Module: newFixtureModule(t),
		Model:  fixtureModel(),
		Scope:  domain.ScopeProject,
	}

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	// There must be at least one gap that carries the agent key in Subject or Detail.
	gap := findGapNaming(result.Report.Gaps, agentKey)
	if gap == nil {
		subjects := make([]string, len(result.Report.Gaps))
		details := make([]string, len(result.Report.Gaps))
		for i, g := range result.Report.Gaps {
			subjects[i] = g.Subject
			details[i] = g.Detail
		}
		t.Errorf("expected a gap carrying agent key %q in Subject or Detail; got subjects: %v, details: %v",
			agentKey, subjects, details)
	}
}

// TestToolMapping_UnmappedTool_NeverAppearsInOutputToolsField asserts that a generic
// tool with no harness mapping does not appear—in its original generic name or in any
// pass-through form—in the output tools field. Only mapped, custom, or by-convention
// harness names may appear.
func TestToolMapping_UnmappedTool_NeverAppearsInOutputToolsField(t *testing.T) {
	req := transform.Request{
		Source: []byte(unmappedToolSource),
		Kind:   domain.ArtifactAgent,
		Key:    "passthrough-test-agent",
		Module: newFixtureModule(t),
		Model:  fixtureModel(),
		Scope:  domain.ScopeProject,
	}

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	doc, err := docformat.Parse(result.Output)
	if err != nil {
		t.Fatalf("Parse output: %v", err)
	}

	items := listFieldScalars(t, doc, "tools")
	for _, item := range items {
		if item == "skill" {
			t.Errorf("generic tool name %q appeared verbatim in output tools field; unmapped tools must never be passed through to the output", "skill")
		}
	}
}

// ---------------------------------------------------------------------------
// T9.6: Skipped-tool handling
// ---------------------------------------------------------------------------

// TestToolMapping_SkippedTool_AbsentFromOutputToolsField asserts that a generic tool
// the user chose to skip (SkippedTools entry) does not appear in the output tools field.
// A skipped tool produces a ToolSkipped resolution with no harness tools, so nothing
// from it should reach the tools list.
func TestToolMapping_SkippedTool_AbsentFromOutputToolsField(t *testing.T) {
	req := transform.Request{
		Source:       []byte(unmappedToolSource),
		Kind:         domain.ArtifactAgent,
		Key:          "skip-test-agent",
		Module:       newFixtureModule(t),
		Model:        fixtureModel(),
		SkippedTools: map[string]bool{"skill": true},
		Scope:        domain.ScopeProject,
	}

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	doc, err := docformat.Parse(result.Output)
	if err != nil {
		t.Fatalf("Parse output: %v", err)
	}

	items := listFieldScalars(t, doc, "tools")
	for _, item := range items {
		if item == "skill" {
			t.Errorf("skipped tool %q appeared in output tools field; a skipped tool must be absent from the output", "skill")
		}
	}
}

// TestToolMapping_SkippedTool_PresentAsGapInReport asserts that a skipped unmapped
// tool is present as a gap in the transformation report. The gap allows the TODO
// checklist to remind the user that the tool was skipped and may need attention later.
//
// This test will be RED until gap-accumulation converts ToolSkipped resolutions into
// gap entries in Report.Gaps.
func TestToolMapping_SkippedTool_PresentAsGapInReport(t *testing.T) {
	req := transform.Request{
		Source:       []byte(unmappedToolSource),
		Kind:         domain.ArtifactAgent,
		Key:          "skip-gap-test-agent",
		Module:       newFixtureModule(t),
		Model:        fixtureModel(),
		SkippedTools: map[string]bool{"skill": true},
		Scope:        domain.ScopeProject,
	}

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	// The skipped tool must surface as a gap so the checklist can report it.
	gap := findGapNaming(result.Report.Gaps, "skill")
	if gap == nil {
		t.Errorf("expected a gap naming %q in Report.Gaps for a skipped unmapped tool; got %d gaps with kinds: %v",
			"skill", len(result.Report.Gaps), gapKinds(result.Report.Gaps))
	} else if gap.Kind != domain.GapUnmappedTool {
		t.Errorf("gap kind for skipped tool: want %q, got %q; a skipped unmapped tool must produce a GapUnmappedTool gap",
			domain.GapUnmappedTool, gap.Kind)
	}
}

// ---------------------------------------------------------------------------
// T9.7: Placeholder expansion when PlaceholderExpansion is empty
// ---------------------------------------------------------------------------

// TestToolMapping_PlaceholderWithEmptyExpansion_ExpandsToFullUniverse asserts that when
// the descriptor's PlaceholderExpansion slice is empty, a {tool-permissions} placeholder
// in the source agent expands to every tool in the full Universe. An empty
// PlaceholderExpansion is the descriptor author's way of saying "all Universe tools".
func TestToolMapping_PlaceholderWithEmptyExpansion_ExpandsToFullUniverse(t *testing.T) {
	// placeholderNoExpansionDescriptorYAML has no placeholder_expansion but declares
	// three Universe tools: "alpha", "beta", "gamma". The placeholder must expand to all three.
	mod := newDescriptorModule(t, placeholderNoExpansionDescriptorYAML, "inline:no-expansion")
	req := transform.Request{
		Source: []byte(placeholderSource),
		Kind:   domain.ArtifactAgent,
		Key:    "placeholder-universe-agent",
		Module: mod,
		Model:  domain.ModelSelection{Origin: domain.OriginUnresolved},
		Scope:  domain.ScopeProject,
	}

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	doc, err := docformat.Parse(result.Output)
	if err != nil {
		t.Fatalf("Parse output: %v", err)
	}

	items := listFieldScalars(t, doc, "tools")
	if items == nil {
		t.Fatal("tools field absent from output frontmatter or not a list; placeholder must be expanded")
	}

	wantTools := []string{"alpha", "beta", "gamma"}
	emitted := make(map[string]bool, len(items))
	for _, item := range items {
		emitted[item] = true
	}
	for _, want := range wantTools {
		if !emitted[want] {
			t.Errorf("Universe tool %q absent from output tools after placeholder expansion; empty PlaceholderExpansion means all Universe tools must appear; got: %v",
				want, items)
		}
	}
}

// ---------------------------------------------------------------------------
// T9.5: Custom tool name in output frontmatter and gap absence
// ---------------------------------------------------------------------------

// TestToolMapping_CustomTool_AppearsInOutputFrontmatter asserts that when a user
// supplies a custom MCP server name for an unmapped generic tool, Apply produces the
// formatted name (via custom_tool_template) in the output frontmatter tools field.
// Without this, a harness that requires every tool to be listed would receive a
// deployment that silently omits the MCP server entry.
//
// This test will be RED until buildListToolFields includes ToolCustom resolutions in
// the list output alongside ToolMapped resolutions.
func TestToolMapping_CustomTool_AppearsInOutputFrontmatter(t *testing.T) {
	mod := newDescriptorModule(t, customToolDescriptorYAML, "inline:custom-tool")
	req := transform.Request{
		Source:      []byte(customToolSource),
		Kind:        domain.ArtifactAgent,
		Key:         "custom-tool-test-agent",
		Module:      mod,
		Model:       domain.ModelSelection{Origin: domain.OriginUnresolved},
		Scope:       domain.ScopeProject,
		CustomTools: map[string]string{"terminal": "my-server"},
	}

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	doc, err := docformat.Parse(result.Output)
	if err != nil {
		t.Fatalf("Parse output: %v", err)
	}

	// custom_tool_template is "mcp:%s", so "my-server" must become "mcp:my-server".
	const wantName = "mcp:my-server"
	items := listFieldScalars(t, doc, "tools")
	if items == nil {
		t.Fatal("tools field absent from output frontmatter or not a list; custom tool must appear in the tools field")
	}
	var found bool
	for _, item := range items {
		if item == wantName {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("formatted custom tool name %q not found in output frontmatter tools field; "+
			"ToolCustom resolutions must appear in the list output; got: %v", wantName, items)
	}
}

// TestToolMapping_CustomTool_NoGapUnmappedToolInReport asserts that a generic tool
// resolved via a user-supplied custom name (ToolCustom outcome) does NOT produce a
// GapUnmappedTool in Report.Gaps. A custom-named tool is resolved — it must not be
// treated as still-unresolved and appear in the gap report.
//
// This test will be RED until gap-accumulation distinguishes ToolCustom from ToolUnmapped
// and does not emit a gap for ToolCustom resolutions.
func TestToolMapping_CustomTool_NoGapUnmappedToolInReport(t *testing.T) {
	mod := newDescriptorModule(t, customToolDescriptorYAML, "inline:custom-tool")
	req := transform.Request{
		Source:      []byte(customToolSource),
		Kind:        domain.ArtifactAgent,
		Key:         "custom-gap-test-agent",
		Module:      mod,
		Model:       domain.ModelSelection{Origin: domain.OriginUnresolved},
		Scope:       domain.ScopeProject,
		CustomTools: map[string]string{"terminal": "my-server"},
	}

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	// A custom-named tool is resolved: no GapUnmappedTool must appear for it.
	for _, gap := range result.Report.Gaps {
		if gap.Kind == domain.GapUnmappedTool {
			t.Errorf("found unexpected GapUnmappedTool in Report.Gaps for custom-named tool %q; "+
				"ToolCustom resolutions must not produce an unmapped-tool gap; "+
				"gap: {Kind:%q Subject:%q Detail:%q}",
				"terminal", gap.Kind, gap.Subject, gap.Detail)
		}
	}
}

// ---------------------------------------------------------------------------
// T9.1 (transform layer): One-to-many expansion in output frontmatter
// ---------------------------------------------------------------------------

// TestToolMapping_OneToMany_BothHarnessToolsPresentInOutput asserts that when a
// single generic tool maps to multiple harness tools, all of them appear in the output
// frontmatter tools field. This provides end-to-end Apply-level confirmation that
// one-to-many expansion works through the full pipeline, complementing the descriptor-
// layer coverage in the harness/descriptor package.
func TestToolMapping_OneToMany_BothHarnessToolsPresentInOutput(t *testing.T) {
	// file_write maps to "write/createFile" and "write/editFile" in oneToManyDescriptorYAML.
	mod := newDescriptorModule(t, oneToManyDescriptorYAML, "inline:one-to-many")
	req := transform.Request{
		Source: []byte(oneToManySource),
		Kind:   domain.ArtifactAgent,
		Key:    "one-to-many-test-agent",
		Module: mod,
		Model:  domain.ModelSelection{Origin: domain.OriginUnresolved},
		Scope:  domain.ScopeProject,
	}

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	doc, err := docformat.Parse(result.Output)
	if err != nil {
		t.Fatalf("Parse output: %v", err)
	}

	items := listFieldScalars(t, doc, "tools")
	if items == nil {
		t.Fatal("tools field absent from output frontmatter or not a list")
	}

	wantTools := []string{"write/createFile", "write/editFile"}
	emitted := make(map[string]bool, len(items))
	for _, item := range items {
		emitted[item] = true
	}
	for _, want := range wantTools {
		if !emitted[want] {
			t.Errorf("harness tool %q absent from output tools field; one-to-many expansion must include "+
				"all mapped harness tools in the output; got: %v", want, items)
		}
	}
}
