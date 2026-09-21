package descriptor

import (
	"errors"
	"strings"

	"mosaic-deploy/internal/domain"
)

// MinimalGenericToolSet is the fixed set of non-escalating generic tool names that
// constitute a minimal read-only grant. A minimal grant rendered through any target
// harness's Tools() produces exactly the read and search capabilities the agent needs
// without enabling any workspace-write or shell-execution capability.
//
// The set is disjoint from the escalating set (file_write, file_edit, terminal,
// subagent) by design: rendering the minimal set through a harness module can never
// upgrade a sandbox to workspace-write mode.
var MinimalGenericToolSet = []string{
	"file_read",
	"file_search",
	"content_search",
}

// MinimalGrant is the result of rendering the minimal generic tool set through a
// target harness module's own Tools() method. It carries the fields the module
// emits for a minimal read-only grant, the per-tool resolution audit trail, and
// the structural flag that indicates whether the target's descriptor declares a
// tools key.
type MinimalGrant struct {
	// Fields holds the frontmatter fields produced by the module's Tools() call
	// for the minimal generic set. For harnesses that declare a tools key this
	// always includes the tools field. For harnesses with no declared tools key
	// (e.g. Codex) it holds the capability field the module emits instead
	// (e.g. sandbox_mode).
	Fields []domain.FrontmatterField

	// Resolutions is the audit trail, one per member of MinimalGenericToolSet,
	// in the same order as MinimalGenericToolSet. Each record describes how the
	// corresponding generic tool was resolved by the module. Callers (promote,
	// retarget) use this to report per-tool resolution outcomes.
	Resolutions []domain.ToolResolution

	// ToolsKeyDeclared reports whether the harness descriptor declares a tools key.
	// When false the absence of a tools-shaped field in Fields is expected and correct;
	// capability is expressed through another field. When true, Fields must contain the
	// declared tools key or RenderMinimalToolGrant has already returned an error.
	ToolsKeyDeclared bool
}

// ErrMinimalGrantFieldAbsent is returned by RenderMinimalToolGrant when the target
// harness declares a tools key and the module's Tools() result does not carry a field
// with that key. An absent tools field on a harness that declares one means the
// deployed agent file will either have no tools constraint or a stale value from a
// prior deployment; neither is acceptable.
var ErrMinimalGrantFieldAbsent = errors.New("minimal grant: declared tools key absent from rendered result")

// ErrMinimalGrantValueEmpty is returned by RenderMinimalToolGrant when the target
// harness declares a tools key, the field is present, but its emitted value is empty
// in the target's own syntax:
//   - an empty or whitespace-only scalar (the Claude Code comma-separated form with
//     nothing in it is read by Claude Code as "inherit everything" -- more dangerous
//     than an absent field);
//   - a list with no entries;
//   - a permission mapping with no allow entry.
//
// A presence-only check would pass a present-but-empty value, so this value-level
// refusal is required alongside ErrMinimalGrantFieldAbsent.
var ErrMinimalGrantValueEmpty = errors.New("minimal grant: rendered value is empty in the target's syntax")

// RenderMinimalToolGrant renders the minimal generic tool set through the target
// harness module's own Tools() method and returns the resulting MinimalGrant.
//
// The entry point calls m.Tools() with MinimalGenericToolSet rather than calling
// descriptor.MapTools directly. This is required because Claude Code declares
// shape: list but emits a comma-separated scalar after its module-level
// post-processing. Calling the shared mapper directly would bypass that step and
// emit a YAML list where Claude Code reads a list as unrestricted -- an
// "inherit everything" grant disguised as a tools field.
//
// After the module call, RenderMinimalToolGrant enforces two guards for harnesses
// that declare a tools key (placement 1 of the always-emitted invariant):
//
//  1. Presence: if the declared tools key is absent from the result, the function
//     returns ErrMinimalGrantFieldAbsent rather than a quietly missing constraint.
//  2. Value: if the declared tools key is present but its value is empty in the
//     target's own syntax, the function returns ErrMinimalGrantValueEmpty.
//     An empty comma-separated scalar is the concrete "inherit everything" hazard,
//     so present-but-empty is more dangerous than absent, and a presence-only check
//     would pass it.
//
// Harnesses that declare no tools key (e.g. Codex) are not subject to either guard;
// for them, returning no tools-shaped field is the correct documented outcome.
// Capability is stated through another field (e.g. sandbox_mode), and the placement-2
// guard in contracttest catches a module that returns nothing at all.
func RenderMinimalToolGrant(m domain.HarnessModule, agentKey string) (MinimalGrant, error) {
	desc := m.Descriptor()
	toolsKey := desc.Frontmatter.ToolsKey

	// Call through the module's own Tools() so that module-level post-processing
	// (e.g. Claude Code's convertFieldsToScalar) is applied. Never call MapTools
	// directly from this entry point.
	result, err := m.Tools(domain.ToolRequest{
		AgentKey: agentKey,
		Generic:  MinimalGenericToolSet,
	})
	if err != nil {
		return MinimalGrant{}, err
	}

	grant := MinimalGrant{
		Fields:           result.Fields,
		Resolutions:      result.Resolutions,
		ToolsKeyDeclared: toolsKey != "",
	}

	// Apply the two-part guard only for harnesses that declare a tools key.
	// For harnesses with no tools key the absence of a tools-shaped field is
	// the correct outcome (e.g. Codex uses sandbox_mode), not an error.
	if toolsKey != "" {
		// Guard 1 (placement 1): the declared tools key must be present in Fields.
		var toolsField *domain.FrontmatterField
		for i := range result.Fields {
			if result.Fields[i].Key == toolsKey {
				toolsField = &result.Fields[i]
				break
			}
		}
		if toolsField == nil {
			return MinimalGrant{}, ErrMinimalGrantFieldAbsent
		}

		// Guard 2: the emitted value must be non-empty in the target's own syntax.
		// Route the emptiness test through the actual emitted shape, not the declared
		// descriptor shape, because Claude Code declares list and emits a scalar.
		if isEmptyInTargetSyntax(toolsField.Value) {
			return MinimalGrant{}, ErrMinimalGrantValueEmpty
		}
	}

	return grant, nil
}

// isEmptyInTargetSyntax reports whether a field value is empty in the syntax the
// target harness actually emits. The check routes through the emitted Kind rather
// than the descriptor's declared shape, because Claude Code declares shape: list
// and emits a KindScalar after module post-processing.
//
// Empty means:
//   - KindScalar: the scalar text is empty or consists solely of whitespace. A
//     whitespace-only comma-separated tools value is still read as unrestricted.
//   - KindList: the list carries no items.
//   - KindMapping: the mapping has no pair whose value is "allow". An all-deny
//     mapping grants nothing and is as dangerous as an empty list.
//
// A non-empty scalar, a list with at least one item, or a mapping with at least
// one allow pair is non-empty and must be accepted. The explicit "none" form for
// shapes that define one is also accepted here because it is non-empty (a deliberate
// deny rather than an accidental absence).
func isEmptyInTargetSyntax(v domain.FieldValue) bool {
	switch v.Kind {
	case domain.KindScalar:
		return strings.TrimSpace(v.Scalar) == ""
	case domain.KindList:
		return len(v.Items) == 0
	case domain.KindMapping:
		allowStr := string(domain.Allow)
		for _, pair := range v.Pairs {
			if pair.Value.Kind == domain.KindScalar && pair.Value.Scalar == allowStr {
				return false
			}
		}
		return true
	default:
		// Unknown kind: treat as empty to be conservative.
		return true
	}
}
