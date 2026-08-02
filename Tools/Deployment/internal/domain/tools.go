package domain

// ToolDestinationKind selects where a mapping destination writes its names.
// The string value is used verbatim in YAML and JSON wire formats, so any change
// is a breaking protocol change.
type ToolDestinationKind string

const (
	// DestMain appends the destination's names to the harness's own tools field, the key
	// named by FrontmatterSpec.ToolsKey, rendered in the harness's own tools format.
	DestMain ToolDestinationKind = "main"
	// DestField writes the destination's names to a separate frontmatter key named by
	// ToolDestination.Field, rendered in ToolDestination.Format.
	DestField ToolDestinationKind = "field"
)

// ToolValueFormat is the rendered shape of a DestField destination's value. It maps onto
// the existing FieldValue vocabulary; no new rendering concept is introduced.
// The string value is used verbatim in YAML and JSON wire formats.
type ToolValueFormat string

const (
	FormatListBlock ToolValueFormat = "list-block" // KindList, ListBlock style
	FormatListFlow  ToolValueFormat = "list-flow"  // KindList, ListFlow style
	FormatScalar    ToolValueFormat = "scalar"     // KindScalar, names joined by Separator
)

// DefaultToolValueFormat is applied when a DestField destination omits Format.
const DefaultToolValueFormat = FormatListBlock

// DefaultScalarSeparator is applied when a FormatScalar destination omits Separator.
const DefaultScalarSeparator = ", "

// ToolDestination is one output target of a generic tool mapping. A mapping may declare
// several; each is rendered independently.
type ToolDestination struct {
	Kind ToolDestinationKind
	// Field is the frontmatter key to emit. Required when Kind == DestField, must be
	// empty when Kind == DestMain.
	Field string
	// Format is the rendered value shape. Meaningful only when Kind == DestField; empty
	// means DefaultToolValueFormat.
	Format ToolValueFormat
	// Separator joins names when Format == FormatScalar; empty means DefaultScalarSeparator.
	Separator string
	// Names are the harness-side tool or server names this destination contributes.
	// Different destinations of the same mapping may contribute different names.
	Names []string
}

// ToolShape describes the frontmatter value structure a harness uses for its tool list.
type ToolShape string

const (
	ShapeList       ToolShape = "list"       // flat list value
	ShapePermission ToolShape = "permission" // nested allow/deny mapping
)

// Disposition is the default permission assigned to a tool that an agent does not explicitly use.
type Disposition string

const (
	Allow Disposition = "allow"
	Deny  Disposition = "deny"
)

// ToolSpec is the complete tool configuration for a harness, declared in its descriptor.
type ToolSpec struct {
	Shape    ToolShape
	Universe []HarnessTool // full harness tool set; declared order is the only ordering authority
	Mappings []ToolMapping
	// CustomToolTemplate is the minimal form a user-supplied MCP server name takes in this
	// harness's output. "%s" is replaced by the supplied name. Empty = the name is used as-is.
	CustomToolTemplate string
	// PlaceholderExpansion names the tools the orchestrator's {tool-permissions} placeholder
	// resolves to. Empty means "the whole Universe".
	PlaceholderExpansion []string
}

// HarnessTool is one entry in the harness's full tool universe.
type HarnessTool struct {
	Name         string      // harness-side name, possibly hierarchical: "read/readFile"
	Unused       Disposition // permission shape: disposition when the agent does not use it
	ByConvention bool        // emitted for every agent regardless of generic tools
}

// ToolMapping declares how one generic tool name maps to harness-specific output.
// An empty Destinations slice means "declared unsupported by this harness".
//
// Deprecated fields HarnessTools and Field are retained only while the implementation
// migrates to the Destinations model. New code must use Destinations exclusively.
type ToolMapping struct {
	Generic      string   // generic tool name
	HarnessTools []string // deprecated: use Destinations instead
	Field        string   // deprecated: use Destinations instead
	// Destinations is the ordered set of output targets for this generic tool.
	// An empty slice means "declared unsupported by this harness" — the same
	// meaning the empty HarnessTools slice carried before.
	Destinations []ToolDestination
}

// ToolOutcome is the resolution status of one generic tool entry.
type ToolOutcome string

const (
	ToolMapped   ToolOutcome = "mapped"
	ToolCustom   ToolOutcome = "custom"   // resolved by a user-supplied MCP server name
	ToolSkipped  ToolOutcome = "skipped"  // user declined to name it
	ToolUnmapped ToolOutcome = "unmapped" // no mapping and no user decision yet
)

// ToolResolution is the per-generic-tool audit record. Every generic tool an agent
// declares produces exactly one of these; none may be dropped.
type ToolResolution struct {
	Generic string
	Outcome ToolOutcome
	// HarnessTools is the flattened union of every destination's names, deduplicated,
	// first-seen order. It is the stable summary consumed by gap reporting, the transform
	// report, and the external-module JSON protocol.
	HarnessTools []string
	// Field is deprecated: the diversion key is now expressed per-destination in Destinations.
	// Retained only while the implementation migrates to the Destinations model.
	Field string
	// Destinations records every destination this generic tool resolved to, in declaration
	// order. Empty for ToolSkipped and ToolUnmapped outcomes.
	Destinations []ToolDestination
}

// ToolRequest is the input to HarnessModule.Tools.
type ToolRequest struct {
	AgentKey     string
	Generic      []string          // the agent's declared generic tools, source order
	Placeholder  string            // non-empty when the source used a placeholder instead of a list
	CustomNames  map[string]string // generic tool -> user-supplied MCP server name
	SkippedTools map[string]bool   // generic tool -> user chose to skip
}

// ToolResult is the module's answer: rendered frontmatter fields plus a full audit trail.
type ToolResult struct {
	Fields      []FrontmatterField // ready to place into frontmatter, in emission order
	Resolutions []ToolResolution   // one per entry in ToolRequest.Generic, same order
}
