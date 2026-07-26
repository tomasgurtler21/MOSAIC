package domain

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

// ToolMapping declares how one generic tool name maps to harness-specific tool names.
type ToolMapping struct {
	Generic      string   // generic tool name
	HarnessTools []string // one-to-many; empty slice = declared unsupported by this harness
	Field        string   // non-empty = diverted to this frontmatter key instead of the tools value
}

// ToolOutcome is the resolution status of one generic tool entry.
type ToolOutcome string

const (
	ToolMapped   ToolOutcome = "mapped"
	ToolCustom   ToolOutcome = "custom"   // resolved by a user-supplied MCP server name
	ToolSkipped  ToolOutcome = "skipped"  // user declined to name it
	ToolUnmapped ToolOutcome = "unmapped" // no mapping and no user decision yet
)

// ToolResolution is the per-generic-tool record. Every generic tool an agent declares produces
// exactly one of these; none may be dropped.
type ToolResolution struct {
	Generic      string
	Outcome      ToolOutcome
	HarnessTools []string
	Field        string
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
