package authoring

// ParseTestDefinition parses a test-definition document (`*.test.yaml`) into
// domain.TestDefinition.

import (
	goyaml "github.com/goccy/go-yaml"

	"mosaic-agent-test/internal/domain"
)

var definitionKnownFields = map[string]bool{
	"schema_version":         true,
	"id":                     true,
	"description":            true,
	"layer":                  true,
	"negative":               true,
	"subject":                true,
	"stub_registry":          true,
	"stub_agents":            true,
	"timeout":                true,
	"turn_limit":             true,
	"repetitions":            true,
	"pass_rate":              true,
	"stop_after_invocations": true,
	"seed_files":             true,
	"parallel_groups":        true,
	"assertions":             true,

	// "harness" is a known top-level key only in the sense that its presence
	// is recognised and rejected with a specific "removed-key-harness"
	// diagnostic (see ParseTestDefinition), rather than falling through to
	// the generic "unknown-field" check below.
	"harness": true,
}

type wireSubject struct {
	Identity       string   `yaml:"identity"`
	Agent          string   `yaml:"agent"`
	Workflows      []string `yaml:"workflows"`
	OpeningMessage string   `yaml:"opening_message"`
	InvocationKind string   `yaml:"invocation_kind"`
	Model          string   `yaml:"model"`
	AllowedTools   []string `yaml:"allowed_tools"`
}

type wireStubAgent struct {
	Identity wireCollaboratorIdentity `yaml:"identity"`
	Source   string                   `yaml:"source"`
}

type wireSeedFile struct {
	Path    string `yaml:"path"`
	Content string `yaml:"content"`
	Ref     string `yaml:"ref"`
}

type wireCollaboratorIdentity struct {
	Tool  string `yaml:"tool" json:"tool"`
	Agent string `yaml:"agent" json:"agent"`
}

func (w wireCollaboratorIdentity) toDomain() domain.CollaboratorIdentity {
	return domain.CollaboratorIdentity{ToolName: w.Tool, AgentIdentity: w.Agent}
}

type wireParallelGroup struct {
	Name    string                     `yaml:"name"`
	Members []wireCollaboratorIdentity `yaml:"members"`
}

type wireSequenceStep struct {
	Tool    string                     `yaml:"tool"`
	Agent   string                     `yaml:"agent"`
	Group   string                     `yaml:"group"`
	Members []wireCollaboratorIdentity `yaml:"members"`
}

type wireSequenceAssertion struct {
	Exact bool               `yaml:"exact"`
	Steps []wireSequenceStep `yaml:"steps"`
}

type wireExecutionLog struct {
	AgentIDs  []string `yaml:"agent_ids"`
	AllStatus *string  `yaml:"all_status"`
}

type wireFinalState struct {
	Phase      *string `yaml:"phase"`
	LastStatus *string `yaml:"last_status"`
}

type wireTaskMessage struct {
	At                      int                       `yaml:"at"`
	Identity                *wireCollaboratorIdentity `yaml:"identity"`
	RequiredInputArtifacts  []string                  `yaml:"required_input_artifacts"`
	OptionalInputArtifacts  []string                  `yaml:"optional_input_artifacts"`
	RequiredOutputArtifacts []string                  `yaml:"required_output_artifacts"`
	OptionalOutputArtifacts []string                  `yaml:"optional_output_artifacts"`
	HumanInTheLoop          *bool                     `yaml:"human_in_the_loop"`
	TaskDescriptionContains []string                  `yaml:"task_description_contains"`
}

type wireAssertions struct {
	InvocationSequence *wireSequenceAssertion `yaml:"invocation_sequence"`
	ExecutionLog       *wireExecutionLog      `yaml:"execution_log"`
	FinalState         *wireFinalState        `yaml:"final_state"`
	ProtocolViolations *int                   `yaml:"protocol_violations"`
	ArtifactCreated    []string               `yaml:"artifact_created"`
	ArtifactNotCreated []string               `yaml:"artifact_not_created"`
	MinConcurrency     map[string]int         `yaml:"min_concurrency"`
	TaskMessages       []wireTaskMessage      `yaml:"task_messages"`
}

// toDomain converts the assertion block. A nil receiver (the "assertions"
// key was never declared) yields the zero-valued domain.Assertions — every
// class nil, meaning "not evaluated".
func (w *wireAssertions) toDomain() domain.Assertions {
	var a domain.Assertions
	if w == nil {
		return a
	}

	if w.InvocationSequence != nil {
		steps := make([]domain.SequenceStep, 0, len(w.InvocationSequence.Steps))
		for _, s := range w.InvocationSequence.Steps {
			step := domain.SequenceStep{Group: s.Group}
			if s.Group == "" {
				id := domain.CollaboratorIdentity{ToolName: s.Tool, AgentIdentity: s.Agent}
				step.Identity = &id
			}
			for _, m := range s.Members {
				step.Members = append(step.Members, m.toDomain())
			}
			steps = append(steps, step)
		}
		a.InvocationSequence = &domain.SequenceAssertion{
			Steps: steps,
			Exact: w.InvocationSequence.Exact,
		}
	}

	if w.ExecutionLog != nil {
		if w.ExecutionLog.AgentIDs != nil {
			ids := w.ExecutionLog.AgentIDs
			a.ExecutionLogAgentIDs = &ids
		}
		a.ExecutionLogAllStatus = w.ExecutionLog.AllStatus
	}

	if w.FinalState != nil {
		a.FinalPhase = w.FinalState.Phase
		a.FinalStatus = w.FinalState.LastStatus
	}

	a.ProtocolViolations = w.ProtocolViolations
	a.ArtifactCreated = w.ArtifactCreated
	a.ArtifactNotCreated = w.ArtifactNotCreated
	a.MinConcurrency = w.MinConcurrency

	for _, tm := range w.TaskMessages {
		var identity *domain.CollaboratorIdentity
		if tm.Identity != nil {
			id := tm.Identity.toDomain()
			identity = &id
		}
		a.TaskMessages = append(a.TaskMessages, domain.TaskMessageAssertion{
			At:                      tm.At,
			Identity:                identity,
			RequiredInputArtifacts:  tm.RequiredInputArtifacts,
			OptionalInputArtifacts:  tm.OptionalInputArtifacts,
			RequiredOutputArtifacts: tm.RequiredOutputArtifacts,
			OptionalOutputArtifacts: tm.OptionalOutputArtifacts,
			HumanInTheLoop:          tm.HumanInTheLoop,
			TaskDescriptionContains: tm.TaskDescriptionContains,
		})
	}

	return a
}

type wireDefinition struct {
	SchemaVersion int    `yaml:"schema_version"`
	ID            string `yaml:"id"`
	Description   string `yaml:"description"`
	Layer         string `yaml:"layer"`
	Negative      bool   `yaml:"negative"`

	Subject      wireSubject    `yaml:"subject"`
	StubRegistry string         `yaml:"stub_registry"`
	StubAgents   []wireStubAgent `yaml:"stub_agents"`

	WireSettings `yaml:",inline"`

	SeedFiles      []wireSeedFile      `yaml:"seed_files"`
	ParallelGroups []wireParallelGroup `yaml:"parallel_groups"`
	Assertions     *wireAssertions     `yaml:"assertions"`
}

// ParseTestDefinition parses and schema-validates a test-definition
// document, accumulating every diagnostic found rather than stopping at the
// first.
func ParseTestDefinition(src Source) (domain.TestDefinition, Report) {
	var report Report

	root := parseYAMLRoot(src, &report)
	if root == nil {
		return domain.TestDefinition{}, report
	}
	checkUnknownTopLevelFields(src, root, definitionKnownFields, &report)
	reportRemovedHarnessKeyIfPresent(src, root, "harness", &report)
	reportRemovedSubjectDefinitionKeyIfPresent(src, root, &report)

	var wire wireDefinition
	if err := goyaml.Unmarshal(src.Data, &wire); err != nil {
		report.Add(Diagnostic{
			Severity: SeverityError,
			Code:     "malformed-document",
			Path:     src.Path,
			Message:  err.Error(),
		})
		return domain.TestDefinition{}, report
	}

	if wire.ID == "" {
		report.Add(missingRequiredField(src, "id"))
	}

	def := domain.TestDefinition{
		SchemaVersion: wire.SchemaVersion,
		ID:            wire.ID,
		Description:   wire.Description,
		Layer:         domain.TestLayer(wire.Layer),
		Negative:      wire.Negative,
		Subject: domain.SubjectUnderTest{
			Identity:        wire.Subject.Identity,
			CatalogAgentKey: wire.Subject.Agent,
			Workflows:       wire.Subject.Workflows,
			OpeningMessage:  wire.Subject.OpeningMessage,
			InvocationKind:  wire.Subject.InvocationKind,
			Model:           wire.Subject.Model,
			AllowedTools:    wire.Subject.AllowedTools,
		},
		StubRegistryPath: wire.StubRegistry,
		Settings:         wire.WireSettings.toDomain(src, "timeout", &report),
		SourcePath:       src.Path,
	}

	for _, sf := range wire.SeedFiles {
		def.SeedFiles = append(def.SeedFiles, domain.SeedFile{
			Path:    sf.Path,
			Content: sf.Content,
			Ref:     sf.Ref,
		})
	}

	for _, sa := range wire.StubAgents {
		def.StubAgents = append(def.StubAgents, domain.StubAgent{
			Identity:   sa.Identity.toDomain(),
			SourcePath: sa.Source,
		})
	}

	for _, pg := range wire.ParallelGroups {
		members := make([]domain.CollaboratorIdentity, 0, len(pg.Members))
		for _, m := range pg.Members {
			members = append(members, m.toDomain())
		}
		def.ParallelGroups = append(def.ParallelGroups, domain.ParallelGroup{
			Name:    pg.Name,
			Members: members,
		})
	}

	def.Assertions = wire.Assertions.toDomain()

	return def, report
}
