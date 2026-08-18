package domain

// InfrastructureTrigger represents one trigger entry from an infrastructure agent's
// frontmatter. A single agent may declare multiple triggers.
type InfrastructureTrigger struct {
	Trigger      string // e.g. "STAGE_END", "INVOCATION_INTERVAL", "PHASE_END", "MANUAL"
	TriggerParam string // e.g. "10"; empty string when the trigger takes no parameter
}

// Agent is the parsed metadata of one generic source agent. It never carries file content;
// content is read on demand through Catalog.ReadSource.
type Agent struct {
	Key              string    // slug; file base name; the value referenced_agents uses
	NumericID        string    // frontmatter `id`, verbatim as written
	Version          string    // frontmatter `version`
	Name             string
	Description      string
	Role             AgentRole
	Category         string // source folder, e.g. "Execution"; empty for orchestrator/utility
	RecommendedTier  Tier   // open string, never validated
	TierRationale    string // explanatory text shown to the user during model selection
	Tools            []string // generic tool vocabulary, in source order; nil when ToolsPlaceholder is set
	ToolsPlaceholder string   // e.g. "{tool-permissions}"; empty when Tools is a real list
	RequiredSkills   []string // skill keys
	SourcePath       string   // absolute

	// Infrastructure fields — non-empty only for infrastructure agents.

	// Infrastructure is the agent's class from the infrastructure class vocabulary
	// ("checkpoint", "commit", "review"). Empty for non-infrastructure agents.
	Infrastructure string

	// Triggers is the list of declared trigger conditions. Nil for non-infrastructure agents.
	Triggers []InfrastructureTrigger

	// OnFailure is the failure policy: "halt" or "continue". Empty for non-infrastructure agents.
	OnFailure string
}

// Skill is a versioned capability bundle that agents can require.
type Skill struct {
	Key         string   // folder name, e.g. "lean-tdd"
	Name        string
	Description string
	Version     string
	SourceDir   string   // absolute
	EntryFile   string   // relative to SourceDir, normally "SKILL.md"
	Files       []string // every file in the skill, relative to SourceDir, including EntryFile
}

// HookBundle is one versioned capability whose file set differs per harness.
type HookBundle struct {
	Key         string
	Version     string               // covers the whole bundle; bumped on any change inside it
	Description string
	SourceDir   string
	Variants    map[string]HookVariant // key: harness id
	Placeholder bool                   // true while variant content is a marked placeholder
}

// HookVariant is the harness-specific realisation of a HookBundle.
type HookVariant struct {
	HarnessID     string
	Supported     bool
	ReusesVariant string          // harness id whose Files this variant adopts; empty if own files
	Files         []HookFile      // already resolved through ReusesVariant by the catalog
	Registration  []RegistrationStep
}

// HookFile describes one file in a hook bundle: its source path and the name it gets at the
// deployment target.
type HookFile struct {
	SourcePath string // absolute
	TargetName string // filename at the deployment target
}

// Workflow describes one deployable workflow document.
type Workflow struct {
	ID               string
	Name             string
	Description      string
	Hint             string
	Version          string
	Category         string   // source folder, e.g. "Build"
	SourcePath       string
	ReferencedAgents []string // agent slugs
}

// WorkflowCategory groups workflows from the same source folder for browsable display.
type WorkflowCategory struct {
	Name      string
	Workflows []Workflow // in index order
}
