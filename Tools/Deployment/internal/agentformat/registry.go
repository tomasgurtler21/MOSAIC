// Package agentformat owns the format registry, the Translator contract, the
// artifact-context type, the report type, and the carriage-container vocabulary.
// It is pure: no I/O, no clock, no harness knowledge.
package agentformat

import (
	"errors"
	"fmt"
	"sort"
	"sync"

	"mosaic-deploy/internal/agentformat/formatid"
	"mosaic-deploy/internal/domain"
)

// Translator is the symmetric pair for one agent file format. Both directions are
// pure functions of their inputs: same inputs, same bytes, always.
//
// Encode takes a canonical MOSAIC document (Markdown with YAML frontmatter) and the
// artifact context, and returns the bytes to write to disk.
//
// Decode takes the bytes read from disk and the artifact context, and returns the
// canonical form. Decode(Encode(d)) == N(d) over MOSAIC-owned keys, user-owned keys,
// the body and the stamps, where N is the encode-side normalisation of the document
// and == is the VALUE-LEVEL relation defined in the ContractsDesign round-trip section.
type Translator interface {
	Encode(canonical []byte, ctx ArtifactContext) ([]byte, Report, error)
	Decode(deployed []byte, ctx ArtifactContext) ([]byte, Report, error)
}

// Operation is the create-vs-update distinction carried by ArtifactContext.Op.
// Its zero value (OpUnspecified) is invalid for formats that need the prior-bytes
// channel; those formats raise ErrUnspecifiedOperation on it.
type Operation string

const (
	// OpCreate signals that no prior deployed artifact exists for this key.
	// Formats that use the prior-bytes channel require ctx.PriorDeployed to be nil.
	// Providing non-nil PriorDeployed with OpCreate raises ErrUnspecifiedOperation.
	OpCreate Operation = "create"

	// OpUpdate signals that a prior deployed artifact exists for this key.
	// Formats that use the prior-bytes channel require ctx.PriorDeployed to be non-nil.
	// Providing nil PriorDeployed with OpUpdate raises ErrMissingPriorBytes.
	OpUpdate Operation = "update"

	// OpUnspecified is the zero value. It is invalid for formats that need the
	// prior-bytes channel and raises ErrUnspecifiedOperation there.
	// The Markdown identity translator accepts it without error; the four existing
	// harnesses continue to work without setting Op because Markdown ignores it.
	OpUnspecified Operation = ""
)

// ErrMissingPriorBytes is raised by a translator's Encode when Op == OpUpdate and
// ctx.PriorDeployed is nil. Declared here; the single raise site is the Codex
// translator's Encode, implemented in Stage 4.
var ErrMissingPriorBytes = errors.New("prior deployed bytes are required for update but were not provided")

// ErrUnspecifiedOperation is raised by a translator's Encode when Op == OpUnspecified
// or when Op == OpCreate with non-nil PriorDeployed. Declared here; the single raise
// site is the Codex translator's Encode, implemented in Stage 4.
var ErrUnspecifiedOperation = errors.New("operation must be explicitly OpCreate or OpUpdate")

// ArtifactContext carries the per-artifact metadata a translator needs. All five fields
// are declared now so that later stages extend behaviour, not the struct shape.
type ArtifactContext struct {
	// AgentKey is the sole authority for the emitted name in a Codex TOML file
	// (AD-14). Used by the Codex TOML encode path (this stage, T2.3).
	AgentKey string

	// Kind classifies the artifact: agent, skill or hook. The translator layer only
	// ever encodes agents today; the field threads the pipeline's vocabulary without
	// requiring the translator to know about artifact kinds.
	Kind domain.ArtifactKind

	// PriorDeployed is the raw on-disk bytes of the same artifact in this format on
	// an update; nil on create. Used by the carriage stage (Stage 4) for marker
	// recovery, tolerant stamp read, and user header-comment preservation.
	// This stage's encode neither reads nor requires it; no test here asserts about it.
	PriorDeployed []byte

	// RefreshMode, when true, suppresses deploy-path normalisations: the description
	// fallback, name forcing, sandbox_mode fallback, and foreign-key drop. The
	// harness-only refresh stage turns it on. This stage leaves it false everywhere
	// and implements no suppression behaviour; declaring the field here means the
	// refresh stage adds behaviour behind an existing field rather than widening a
	// struct already pinned by this stage's test suite.
	RefreshMode bool

	// Op carries the create-vs-update decision for formats that need the prior-bytes
	// channel. Its zero value (OpUnspecified) is invalid for those formats. The
	// carriage stage (Stage 4) is the first stage to enforce Op at the translator
	// level; Stage 7 threads it through the pipeline. This stage's encode neither
	// reads nor requires it; no test here asserts about it. Declaring it now prevents
	// the carriage stage from widening a struct already pinned by this stage's suite.
	Op Operation
}

// ArtifactError is a structured error wrapping a translator failure. Phase names the
// translation direction ("encode", "decode", "strip"); Key names the offending key
// when the failure is key-specific and is empty otherwise.
type ArtifactError struct {
	Phase string
	Key   string
	Err   error
}

// Error implements the error interface.
func (e *ArtifactError) Error() string {
	if e.Key != "" {
		return fmt.Sprintf("agentformat %s %q: %v", e.Phase, e.Key, e.Err)
	}
	return fmt.Sprintf("agentformat %s: %v", e.Phase, e.Err)
}

// Unwrap returns the wrapped error for errors.Is / errors.As.
func (e *ArtifactError) Unwrap() error { return e.Err }

// Report is what a translator tells the run about content it did not carry through
// verbatim, in either direction. The translator itself has no reporting channel to
// the user; a Report is always merged additively by its caller.
type Report struct {
	Entries []ReportEntry
}

// ReportEntry is one item in a Report.
type ReportEntry struct {
	Kind   EntryKind
	Key    string // the frontmatter or TOML key concerned
	Detail string // the discarded, overridden or invented value; see per-kind table
	Reason string // human-readable rationale
}

// EntryKind classifies what a report entry describes. Each kind has a fixed
// expectation on the Detail and Key fields; see the ContractsDesign per-kind table.
type EntryKind string

const (
	// EntryDroppedForeignKey - a key in row three of the classification table was
	// dropped. Detail required: the dropped value in canonical frontmatter form.
	EntryDroppedForeignKey EntryKind = "dropped_foreign_key"

	// EntryOverriddenName - the canonical frontmatter carried a name that differed
	// from ctx.AgentKey; the agent key won. Detail required: the divergent name lost.
	EntryOverriddenName EntryKind = "overridden_name"

	// EntryAppliedFallback - a deploy-path normalisation invented a value (description
	// fallback, sandbox_mode fallback). Detail required: the invented value.
	EntryAppliedFallback EntryKind = "applied_fallback"

	// EntryStrippedContainer - a carriage-carried key was consumed on encode and NOT
	// re-emitted: the target format has no place for it. This is the lossy outcome.
	EntryStrippedContainer EntryKind = "stripped_container"

	// EntryCarriedContainer - a carriage-carried key was consumed on encode and
	// RE-EMITTED in the target's own syntax. Nothing was lost. It has no user-facing
	// rendering; it exists so the container invariant's accounting is assertable per
	// format. Detail required to be empty.
	EntryCarriedContainer EntryKind = "carried_container"

	// EntryDroppedComment - a free-standing comment in the prior file's body that no
	// region claimed and that did not survive the encode. One entry per contiguous run.
	EntryDroppedComment EntryKind = "dropped_comment"

	// EntryRefusedMarker - a carriage marker named a key that isMosaicOwned reports as
	// MOSAIC's; the marker was refused and MOSAIC's current value emitted instead.
	EntryRefusedMarker EntryKind = "refused_marker"

	// EntryMissingInstructions - a decoded Codex file carried no developer_instructions;
	// the canonical body is empty. Not a failure. Detail required to be empty.
	EntryMissingInstructions EntryKind = "missing_instructions"
)

// Error sentinels for the translator layer. Each is raised at exactly one site.

// ErrUnknownFormat is returned by Lookup for an id with no registered translator.
var ErrUnknownFormat = errors.New("unknown agent format")

// ErrEmptyBody is returned by the Codex TOML encoder when the agent body is empty
// and no instructions fallback can be invented. Fail-closed: an empty body cannot be
// encoded as a runnable Codex agent.
var ErrEmptyBody = errors.New("agent body is empty; no developer_instructions fallback can be invented")

// ErrUnrepresentableValue is returned by the Codex TOML decoder when a MOSAIC-owned
// value contains a control character that the YAML output cannot represent.
var ErrUnrepresentableValue = errors.New("value contains a control character that cannot be represented")

// ErrMalformedDeployed is the single decode-failure sentinel. It is returned by a
// translator's Decode when the deployed file has syntax errors, duplicate keys, or
// when an owned key carries a value of a type the translator cannot accept (for example
// a TOML integer where a string is required). The wrapping ArtifactError carries
// Phase: "decode" and, where the failure is key-specific, Key naming the offending key.
// Callers classify with errors.Is; the ArtifactError.Key field names the key when
// the failure is key-specific and is empty for format-wide failures (syntax errors).
var ErrMalformedDeployed = errors.New("deployed file is malformed")

// registry internals.
var (
	mu          sync.RWMutex
	translators = map[formatid.ID]Translator{}
)

// Register makes t resolvable under id. It is called from an init() in the package
// that implements the translator. Registering an id twice panics: two translators for
// one format is a programming error, not a runtime condition.
func Register(id formatid.ID, t Translator) {
	mu.Lock()
	defer mu.Unlock()
	if _, exists := translators[id]; exists {
		panic(fmt.Sprintf("agentformat: translator already registered for %q", id))
	}
	translators[id] = t
}

// Lookup returns the translator for id. An unregistered id yields ErrUnknownFormat
// wrapping the id, never a nil translator with a nil error.
func Lookup(id formatid.ID) (Translator, error) {
	mu.RLock()
	defer mu.RUnlock()
	t, ok := translators[id]
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrUnknownFormat, id)
	}
	return t, nil
}

// LookupString is the single runtime entry point from a descriptor's AgentFormatID
// string to a translator: it parses with formatid.Parse and resolves with Lookup.
// The empty string resolves to the Markdown identity translator (CD-4: an absent
// agent_format_id means Markdown). No runtime site calls formatid.Parse itself.
func LookupString(id string) (Translator, error) {
	fid, err := formatid.Parse(id)
	if err != nil {
		return nil, err
	}
	return Lookup(fid)
}

// RegisteredIDs returns every registered id, sorted. Used by the per-format
// invariant tests and by the vocabulary-equality test (T2.7).
func RegisteredIDs() []formatid.ID {
	mu.RLock()
	defer mu.RUnlock()
	ids := make([]formatid.ID, 0, len(translators))
	for id := range translators {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids
}
