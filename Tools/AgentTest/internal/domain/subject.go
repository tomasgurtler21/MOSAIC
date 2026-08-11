// Package domain holds the subject-neutral value types and port interfaces
// that every other package of this module depends on.
//
// This package imports nothing else internal to the module: it is the base
// layer the import-boundary checker (tools/importcheck) enforces.
//
// Vocabulary note: the thing being exercised is always a "subject", never an
// "orchestrator" — a future mode exercises a single agent rather than a
// multi-agent workflow, and the model must already accommodate that. The
// thing being intercepted is always a "collaborator", never a "subagent" —
// a future mode stubs a subagent's tools rather than its dispatch, and the
// composite CollaboratorIdentity is what avoids recarving the matcher when
// that mode arrives.
package domain

import (
	"fmt"
	"strings"
	"time"
)

// SubjectUnderTest is the thing being exercised.
type SubjectUnderTest struct {
	Identity string // agent identifier, e.g. "orchestrator"

	// CatalogAgentKey names the agent in the product's real generic catalogue
	// that this subject is rendered from. Authored in the test definition;
	// harness-neutral by construction, because nothing about a catalogue key
	// names a harness. Resolved from the declaration's subject.agent field
	// during authoring-layer parsing.
	CatalogAgentKey string

	// Workflows pins the workflow set the subject is rendered with, for an
	// orchestrator subject. nil means "not specified"; non-nil empty means
	// "explicitly none". An orchestrator rendered with a different workflow set
	// is a materially different agent, which is why a test can pin it rather
	// than inherit a default.
	Workflows []string

	// DefinitionPath is the definition file's path relative to the sandbox
	// subject dir. Its meaning is unchanged, but its source is not: it is
	// resolved during setup from what the deployment port reported it wrote,
	// and is never authored in a test definition. Do not author this field.
	DefinitionPath string

	OpeningMessage string // the first message the subject receives
	InvocationKind string // maps to mosaic-common/harness InvocationKind
	Model          string
	AllowedTools   []string
}

// DispatchToolName is the normalized, harness-neutral name of the tool that
// dispatches a collaborator. Authored tests, stub registries and fixtures use
// this name and no other.
//
// Adapters translate their harness's native name to it: a vendor renaming a
// tool must not break a test suite, because a suite that breaks on a rename
// is measuring the wrong thing.
//
// It is deliberately not any harness's own term.
const DispatchToolName = "dispatch"

// CollaboratorIdentity is a composite, not an agent type.
//
// In the current mode the intercepted thing is an agent dispatch, so ToolName
// is the dispatch tool and AgentIdentity is the agent type. In a future mode
// the intercepted thing is a plain tool call, and AgentIdentity is empty.
// Keying the matcher on the composite from the start is what avoids recarving
// it later.
type CollaboratorIdentity struct {
	ToolName      string `json:"tool"`
	AgentIdentity string `json:"agent,omitempty"`
}

// Key returns the canonical map key: "tool" when AgentIdentity is empty,
// otherwise "tool/agent". Stable across processes and safe as a JSON object
// key.
func (c CollaboratorIdentity) Key() string {
	if c.AgentIdentity == "" {
		return c.ToolName
	}
	return c.ToolName + "/" + c.AgentIdentity
}

// IsZero reports whether c is the zero-valued identity.
func (c CollaboratorIdentity) IsZero() bool {
	return c.ToolName == "" && c.AgentIdentity == ""
}

// ParseIdentityKey parses the canonical form Key produces back into a
// CollaboratorIdentity.
func ParseIdentityKey(s string) (CollaboratorIdentity, error) {
	if s == "" {
		return CollaboratorIdentity{}, fmt.Errorf("domain: empty identity key")
	}
	tool, agent, found := strings.Cut(s, "/")
	if !found {
		return CollaboratorIdentity{ToolName: tool}, nil
	}
	if tool == "" || agent == "" {
		return CollaboratorIdentity{}, fmt.Errorf("domain: malformed identity key %q", s)
	}
	return CollaboratorIdentity{ToolName: tool, AgentIdentity: agent}, nil
}

// CollaboratorInvocation is one intercepted call as recorded.
type CollaboratorInvocation struct {
	Seq      int // global 1-based sequence across the run; never used for matching
	Ordinal  int // 1-based per-identity occurrence; the matching key
	Identity CollaboratorIdentity
	Message  TaskMessage
	Group    string // declared parallel group, empty when ungrouped
}

// TaskMessage is the parsed Communication Protocol invocation, carrying
// exactly the fields per-invocation assertions are written against, plus the
// raw text so protocol validation can inspect what was actually sent.
type TaskMessage struct {
	AgentInstanceID      string   `json:"agent_instance_id"`
	RunID                string   `json:"run_id,omitempty"`
	TaskDescription      string   `json:"task_description"`
	InputArtifacts       []string `json:"input_artifacts"`
	OutputArtifacts      []string `json:"output_artifacts"`
	InputFiles           []string `json:"input_files,omitempty"`
	OutputFiles          []string `json:"output_files,omitempty"`
	Constraints          string   `json:"constraints,omitempty"`
	IncludeResultSummary bool     `json:"include_result_summary"`
	HumanInTheLoop       bool     `json:"human_in_the_loop"`

	// Raw and Extraction cross the interceptor-process-to-runner-process
	// boundary through invocations.jsonl. They were tagged json:"-", so both
	// arrived zero-valued and the degraded-extraction condition was dead code.
	Raw        string            `json:"raw,omitempty"`
	Extraction ExtractionQuality `json:"extraction,omitempty"`
}

// ExtractionQuality records how confidently a TaskMessage was recovered from
// a native interception payload.
type ExtractionQuality string

const (
	ExtractionParsed    ExtractionQuality = "parsed"
	ExtractionRecovered ExtractionQuality = "recovered"
	ExtractionDegraded  ExtractionQuality = "degraded"
	ExtractionFailed    ExtractionQuality = "failed"
)

// SubjectResult is the subject's own final protocol message plus its exit
// disposition. Run evidence carries this from the start: the subagent
// layer's protocol-validation rule is unimplementable without it.
type SubjectResult struct {
	ProtocolMessage string
	RawOutput       string
	Disposition     RunDisposition
	ExitCode        int

	// Stderr is the subject's standard error, carried through from the
	// launcher's response. An authentication failure is diagnosable from
	// this alone; without it a non-zero exit is indistinguishable from a
	// subject regression.
	Stderr string

	Duration time.Duration
}

// RunDisposition is how the subject's run ended.
type RunDisposition string

const (
	DispositionCompleted   RunDisposition = "completed"
	DispositionTimedOut    RunDisposition = "timed_out"
	DispositionEarlyExit   RunDisposition = "early_exit"
	DispositionTurnLimit   RunDisposition = "turn_limit"
	DispositionSpawnFailed RunDisposition = "spawn_failed"
)

// TestLayer names which of the three test layers a definition belongs to.
type TestLayer string

const (
	LayerSubagent     TestLayer = "subagent"
	LayerOrchestrator TestLayer = "orchestrator"
	LayerIntegration  TestLayer = "integration"
)
