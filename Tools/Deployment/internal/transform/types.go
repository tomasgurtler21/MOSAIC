package transform

import (
	"mosaic-deploy/internal/agentformat"
	"mosaic-deploy/internal/domain"
)

// WorkflowBlock is one workflow's <Workflow type="core" name="{id}"> block, assembled verbatim
// into the orchestrator's <AvailableWorkflows type="project"> region.
type WorkflowBlock struct {
	ID    string
	Block []byte // the section block including its boundary tags, in source order
}

// OriginConfidence records what the plan layer could establish about a deployed
// artifact's provenance. It says nothing about whether the file was hand-edited:
// an agent hand edit is not detected today for any harness, and Stage 17 does not
// add detection. The zero value is OriginConfirmed so that the ~470 existing
// transform.Request literals that omit the field default to today's behaviour and
// produce no owned-key entries.
type OriginConfidence string

const (
	// OriginConfirmed is the zero value. The plan layer confirmed this run wrote the
	// file (manifest-backed, no conflict). Zero owned-key entries are emitted.
	OriginConfirmed OriginConfidence = ""

	// OriginUnconfirmed means the file is conflict-classified and its deployed bytes
	// could be decoded. Entries are emitted for differing non-stamp owned keys.
	OriginUnconfirmed OriginConfidence = "unconfirmed"

	// OriginUnconfirmedUnparseable means the file is conflict-classified and its
	// deployed bytes could NOT be decoded. Zero entries are emitted: there is no
	// canonical deployed form to compare against.
	OriginUnconfirmedUnparseable OriginConfidence = "unconfirmed_unparseable"
)

// OwnedKeyDifference names one MOSAIC-owned key whose deployed value differs from
// the value this run writes, on an artifact whose origin the plan layer could not
// confirm.
//
// "Unconfirmed origin" is weaker than "hand-edited": an agent conflict means only
// that the tool cannot confirm it wrote this file (unparseable, no usable manifest,
// or no manifest record at this target path). It asserts nothing about who changed
// what and changes no classification.
type OwnedKeyDifference struct {
	Key      string // the owned key
	Deployed string // value in the decoded canonical deployed form; "" when DeployedPresent is false
	Incoming string // value this run would write; "" when IncomingPresent is false
	Reason   string // human-readable; e.g. "origin could not be confirmed; deployed value differs"

	// DeployedPresent and IncomingPresent distinguish absent from empty. A renderer
	// must consult these: an absent side renders as "(absent)", never as empty.
	DeployedPresent bool
	IncomingPresent bool
}

// Request carries every input that Apply needs. Apply performs no filesystem, network,
// clock, or randomness operations; all inputs arrive here (CD-7, AC8.2).
type Request struct {
	Source       []byte              // the generic source file bytes, verbatim
	Kind         domain.ArtifactKind
	Key          string              // artifact slug (agent key, skill key, …)
	Module       domain.HarnessModule
	Model        domain.ModelSelection
	CustomTools  map[string]string   // generic tool name → user-supplied MCP server name
	SkippedTools map[string]bool
	Scope        domain.Scope
	// Deployed is the currently-deployed file bytes in CANONICAL form (Markdown with
	// YAML frontmatter), or nil on create. Decode happens in the application layer
	// before Apply is called. A caller holding raw on-disk bytes that has not decoded
	// them is violating this precondition.
	//
	// For Markdown harnesses the canonical and raw forms are identical, which is why
	// a missing decode step is invisible until Codex runs.
	//
	// Injection content for InjectionProject class is lifted from here and reinstated
	// in the output.
	Deployed []byte

	// DeployedRaw is the currently-deployed file bytes exactly as they sit on disk, in
	// the harness's own format, or nil on create. It is the translator's private
	// preservation channel for user-owned content the canonical value model cannot
	// carry (FR-9c). For a Markdown harness it equals Deployed, which is why a missing
	// thread here is invisible until Codex runs.
	//
	// The strictness (ErrMissingPriorBytes on OpUpdate with nil DeployedRaw) is
	// format-conditional and belongs to the translator, not to this field. The Markdown
	// identity translator ignores it entirely, so all existing call sites that omit this
	// field continue to work unchanged.
	DeployedRaw []byte

	// Origin records what the plan layer could establish about the deployed file's
	// provenance. The zero value is OriginConfirmed, which is today's behaviour:
	// no owned-key entries are emitted. Transform never recomputes this classification;
	// the plan layer is the single source of truth.
	//
	// This field does NOT mean the file was hand-edited. A hand edit to a deployed agent
	// changes no version field and classifies as unchanged today for all harnesses; this
	// work does not add hash-mismatch detection.
	Origin OriginConfidence

	// Op is create versus update, carried straight through to ArtifactContext.Op.
	// The application layer sets it from PlanItem.Action; transform never infers it.
	//
	// Its zero value is agentformat.OpUnspecified, which is what all ~470 existing call
	// sites supply. That is safe for Markdown harnesses: the Markdown identity translator
	// accepts any Op value including OpUnspecified. Only formats that need the
	// prior-bytes channel (Codex) raise ErrUnspecifiedOperation on OpUnspecified.
	//
	// Apply threads Op through to ArtifactContext.Op verbatim. It specifically does NOT
	// fall back to "if Deployed == nil then OpCreate else OpUpdate", because that
	// inference makes a dropped DeployedRaw thread invisible. The inference is forbidden
	// at every layer.
	Op agentformat.Operation
	Workflows            []WorkflowBlock        // non-empty only for the orchestrator agent
	InfrastructureAgents []InfrastructureBlock  // non-empty only for the orchestrator agent
	// ToolMappingsVersion is the hash of the effective tool-destination mapping set for this
	// run, computed by config.HashToolDestinations. It is written to the deployed file as the
	// `tool_mappings_version` frontmatter stamp so the planner can detect staleness on
	// subsequent runs when the config-declared mapping set changes. An empty string means no
	// config mappings are active and no stamp is written to the deployed file.
	ToolMappingsVersion string

	// Role is the deploying agent's role. It selects the protocol variant for
	// <CommunicationProtocol type="managed"> regions.
	Role domain.AgentRole

	// Protocol carries the role-keyed protocol blocks and the protocol source version,
	// loaded once per run by the app layer. Required for agents whose source declares a
	// <CommunicationProtocol type="managed"> region.
	Protocol domain.ProtocolContent

	// Bundle carries the canonical blocks and the bundle version, loaded once per run by
	// the app layer. Required for agents whose source declares any bundle-sourced managed
	// region; ignored for agents that declare none.
	Bundle domain.BundleContent

	// InjectionRenames overrides the package-level InjectionRenames table for this call.
	// Nil means use transform.InjectionRenames. Intended for tests so that conflict,
	// chain, precedence, and no-op cases are exercisable without mutating package state.
	InjectionRenames []RenameEntry

	// Timestamp is an RFC3339 UTC instant supplied by the caller that owns the clock.
	// The transform never reads a clock (enforced by the build-time purity check). It is
	// stamped onto parking TODO entries so a notice remains meaningful after the wholesale
	// regeneration of the TODO report. Empty is legal and omits the timestamp clause from
	// the parking gap detail message.
	Timestamp string

	// InjectionsVersion is the harness descriptor's injection version for non-orchestrator
	// agents. Used by applyHarnessRegion to stamp version attributes on InjectionHarness-class
	// regions. Populated by the app layer from desc.InjectionsVersion. Empty string means no
	// version attribute is written.
	InjectionsVersion string

	// OrchestratorInjectionsVersion is the injection version for orchestrator agents.
	// Used by applyHarnessRegion when req.Key == "orchestrator" to select the orchestrator-
	// specific value. Populated by the app layer from desc.OrchestratorInjectionsVersion.
	// Empty string means no version attribute is written for orchestrators.
	OrchestratorInjectionsVersion string
}

// Result is the output of a successful Apply call.
type Result struct {
	Output []byte
	Report Report
}

// Report is the audit trail of one transformation. It is the input to per-item update
// logging, gap collection, and the TODO checklist (AC8.5, AC15.2).
type Report struct {
	Fields               []FieldChange
	Tools                []domain.ToolResolution
	Regions              []RegionOutcome    // covers both injection and managed regions
	Gaps                 []domain.Gap
	Workflows            []string // workflow IDs present in the assembled injection, in emitted order
	InfrastructureAgents []string // agent keys present in the assembled InfrastructureAgents injection, in emitted order
	OutputBytes          int

	// OwnedKeyDifferences names MOSAIC-owned keys whose deployed value differs from
	// the value this run writes, on an artifact whose origin the plan could not confirm.
	// Empty on every ordinary run and on every artifact with Origin != OriginUnconfirmed.
	OwnedKeyDifferences []OwnedKeyDifference
}

// FieldChange records what happened to one frontmatter field: whether it was added,
// overwritten, or removed, and why. Before is empty when the field was added; After is
// empty when the field was dropped.
type FieldChange struct {
	Key    string
	Before string // rendered form of the value before the transform; empty when added
	After  string // rendered form of the value after the transform; empty when dropped
	Reason string // human-readable rationale, e.g. "descriptor add", "model selection", "version stamp"
}

