package app

// promote.go declares the types, sentinel errors, and generation core for the promote
// capability. The core function buildGenericAgent produces a generic agent source file
// from an already boundary-tagged harness-only agent; all I/O belongs to Service.Promote
// and its helpers in promote_service.go.
//
// Field policy comment (restated here per design requirement):
//
// Generated / overwritten (written first, ahead of carried keys):
//   id        → PromoteInput.NumericID
//   version   → PromoteInput.Version, or "1.0.0" when empty
//   name      → source `name` when present, else PromoteInput.Key
//   role      → string(PromoteInput.Role), defaulting to domain.RoleSubagent
//
// Carried over verbatim — every other source key not in the drop set, in original order.
// This includes: description, recommended_tier, tier_rationale, tools, required_skills,
// and infrastructure fields.
//
// Dropped (union of promoteDroppedFrontmatterKeys and PromoteInput.DropKeys,
// minus promoteProtectedGenericKeys):
//   Cross-harness stamps: transform_version, injections_version,
//   orchestrator_injections_version, bundle_version, protocol_version,
//   tool_mappings_version, model.
//   Harness-specific keys: derived from the identified harness's descriptor keys
//   (ModelKey, ToolsKey) and its FrontmatterPlan.Set key names, passed in by the
//   service layer via PromoteInput.DropKeys (see promoteHarnessDropKeys).

import (
	"errors"
	"fmt"

	"mosaic-common/docformat"
	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/harness/descriptor"
)

// ErrPromoteNotTransformed reports a source file that does not satisfy the two-signal
// harness-only detection rule and therefore cannot be promoted. The wrapping error
// carries the verdict's Reason so the user learns which signal was missing.
var ErrPromoteNotTransformed = errors.New("file is not an already-transformed MOSAIC agent")

// ErrPromoteNumericIDRequired reports a PromoteInput with an empty NumericID.
var ErrPromoteNumericIDRequired = errors.New("promote requires a numeric agent id")

// promoteDroppedFrontmatterKeys names the frontmatter fields removed when generating a
// generic source from a deployed harness-only agent. Each is either a stamp the
// deployment pipeline writes at deploy time or a harness-specific artifact; carrying any
// of them into a generic source would make the file lie about its provenance.
var promoteDroppedFrontmatterKeys = map[string]bool{
	"transform_version":               true,
	"injections_version":              true,
	"orchestrator_injections_version": true,
	"bundle_version":                  true,
	"protocol_version":                true,
	"tool_mappings_version":           true,
	"model":                           true,
}

// PromoteInput carries everything buildGenericAgent needs. The frontmatter decisions the
// core cannot make alone — the numeric id in particular, which requires knowledge of the
// destination catalog — arrive here rather than being invented.
type PromoteInput struct {
	// Source is the harness-only agent file bytes, verbatim. Never mutated.
	Source []byte
	// NumericID is the `id` value to write. Required and non-empty.
	NumericID string
	// Key is the destination file's base name without ".md". It supplies the `name`
	// frontmatter field when the source carries none.
	Key string
	// Role is written to the `role` frontmatter field. Defaults to domain.RoleSubagent
	// when the zero value is supplied.
	Role domain.AgentRole
	// Version is the `version` value to write. Defaults to "1.0.0" when empty.
	Version string

	// DropKeys are the harness-specific frontmatter keys to remove, derived by the caller
	// from the identified harness's own contract (see promoteHarnessDropKeys). They are
	// unioned with promoteDroppedFrontmatterKeys. Keys in promoteProtectedGenericKeys are
	// ignored. A nil map means "no harness-specific drops".
	DropKeys map[string]bool

	// Tools is the authoritative generic tools list reconstructed by reverse mapping.
	// A nil slice means no reconstruction was attempted and any source `tools` key is
	// carried through unchanged. A non-nil slice (including empty) is authoritative and
	// replaces the key outright; a non-nil empty slice removes the key entirely. Entries
	// are written in slice order as a flow-style list (AD-7).
	Tools []string

	// RecommendedTier is the generic-only field recovered from the user during promote.
	// An empty string means the field was not supplied and must be omitted from the output
	// entirely — never written blank (AD-12).
	RecommendedTier string

	// TierRationale is the generic-only field recovered from the user during promote.
	// An empty string means the field was not supplied; it is omitted from the output.
	TierRationale string

	// RequiredSkills is the generic-only field recovered from the user during promote,
	// entered as a comma-separated list and split by the service layer. A nil or empty
	// slice means the field was not supplied and is omitted from the output.
	RequiredSkills []string
}

// promoteProtectedGenericKeys names frontmatter keys a harness-derived drop-set may never
// remove. A harness whose model key, tools key, or plan-added key collides with one of
// these loses the collision: the generic field survives (AD-4).
var promoteProtectedGenericKeys = map[string]bool{
	"id": true, "version": true, "name": true, "role": true, "description": true,
}

// promoteSourceFacts is the read-only view of a deployed agent's frontmatter that the
// promote service needs before it can ask its questions. It is produced by the pure
// inspectPromoteSource and carries no document reference, so it can be constructed
// directly in a test.
type promoteSourceFacts struct {
	// Keys is the set of frontmatter keys the deployed file carries. It answers "does the
	// source already have this field?" without re-parsing.
	Keys map[string]bool
	// HarnessTools are the harness-side tool entries extracted from the file under the
	// harness's own tools key, in document order, deduplicated first-seen. Empty when the
	// file carries no tools key or the harness declares none.
	HarnessTools []string
}

// inspectPromoteSource reads the facts service.Promote needs from a deployed agent's
// frontmatter before it can ask its questions: which keys the file already carries, and
// which harness-side tool entries it declares under the harness's own tools key.
//
// Pure: it parses src and returns; it performs no I/O and asks nothing. A source that
// fails to parse returns an error wrapping ErrPromoteNotTransformed.
func inspectPromoteSource(src []byte, d *domain.HarnessDescriptor) (promoteSourceFacts, error) {
	doc, err := docformat.Parse(src)
	if err != nil {
		return promoteSourceFacts{}, fmt.Errorf("%w: %s", ErrPromoteNotTransformed, err.Error())
	}

	fm := doc.Frontmatter()

	// Collect the presence set of all frontmatter keys.
	keys := make(map[string]bool)
	for _, k := range fm.Keys() {
		keys[k] = true
	}

	// Extract harness-side tool entries from the value stored under the harness's tools key.
	var toolsValue domain.FieldValue
	if d.Frontmatter.ToolsKey != "" {
		if v, ok := fm.Get(d.Frontmatter.ToolsKey); ok {
			toolsValue = v
		}
	}
	harnessTools := descriptor.ExtractToolEntries(d, toolsValue)

	return promoteSourceFacts{
		Keys:         keys,
		HarnessTools: harnessTools,
	}, nil
}

// buildGenericAgent produces the bytes of a generic agent source file from an already
// boundary-tagged harness-only agent.
//
// Eligibility is decided by eligibleHarnessOnly — the same function the Update discovery
// scan uses, never a reimplementation. An ineligible source returns an error wrapping
// ErrPromoteNotTransformed and no output.
//
// The generated file:
//   - has every [[INJECTION:...]] region stripped to an empty tag pair — no harness-
//     specific injection content; the deploy pipeline fills injections per-harness;
//   - has every [[DEPLOYED:...]] region stripped to an empty tag pair — this matches
//     real generic source files and satisfies the transform pipeline's constraint that
//     deployed regions in a source must be empty (ErrSourceDeployedRegionNotEmpty);
//   - carries over the [[SECTION:...]] tag structure and section body prose as-is;
//   - carries frontmatter per the field policy documented at the top of promote.go.
//
// The function is pure: in.Source is not modified and no file is written or read.
func buildGenericAgent(in PromoteInput) ([]byte, error) {
	// NumericID is required before doing any other work; the catalog enforces uniqueness
	// and the caller must supply this from the loaded catalog (see nextNumericID).
	if in.NumericID == "" {
		return nil, ErrPromoteNumericIDRequired
	}

	// Eligibility is the shared two-signal decision — never reimplemented here.
	verdict := eligibleHarnessOnly(in.Source)
	if !verdict.Eligible {
		return nil, fmt.Errorf("%w: %s", ErrPromoteNotTransformed, verdict.Reason)
	}

	// Parse the source. eligibleHarnessOnly already parsed successfully, but guard
	// defensively in case an edge case reaches here.
	doc, err := docformat.Parse(in.Source)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrPromoteNotTransformed, err.Error())
	}

	// Clone so that all mutations apply to a fresh document; in.Source is never modified.
	out := doc.Clone()
	fm := out.Frontmatter()

	// Drop every harness-specific stamp (see promoteDroppedFrontmatterKeys). These are
	// deployment-time or transform-time stamps that have no place in a generic source.
	for key := range promoteDroppedFrontmatterKeys {
		fm.Remove(key)
	}

	// Drop harness-specific keys derived by the caller from the identified harness's own
	// contract (see promoteHarnessDropKeys). This is unioned with the cross-harness stamp
	// set above. Keys in promoteProtectedGenericKeys are never removed — a harness whose
	// model key, tools key, or plan key collides with a protected field loses the collision
	// and the generic field survives (AD-4).
	for key := range in.DropKeys {
		if key == "" || promoteProtectedGenericKeys[key] {
			continue
		}
		fm.Remove(key)
	}

	// Determine the four generated field values per the field policy above.

	// version: use the supplied value; default to "1.0.0" because hand-authored
	// harness-only agents commonly carry no version field at all.
	version := in.Version
	if version == "" {
		version = "1.0.0"
	}

	// role: use the supplied value; default to domain.RoleSubagent.
	role := in.Role
	if role == "" {
		role = domain.RoleSubagent
	}

	// name: prefer the source's own name scalar so the promoted agent keeps the name the
	// author intended; fall back to in.Key (the destination file's base name).
	name := in.Key
	if v, ok := fm.Get("name"); ok && v.Kind == domain.KindScalar && v.Scalar != "" {
		name = v.Scalar
	}

	// Set (or overwrite) the four generated keys. Set updates an existing entry in-place
	// and marks it dirty (so it re-serialises from the new value rather than rawBytes);
	// absent keys are appended. Reorder below moves all four to the front.
	fm.Set("id", domain.ScalarValue(in.NumericID, domain.QuotePlain))
	fm.Set("version", domain.ScalarValue(version, domain.QuotePlain))
	fm.Set("name", domain.ScalarValue(name, domain.QuotePlain))
	fm.Set("role", domain.ScalarValue(string(role), domain.QuotePlain))

	// Apply the tools reconstruction contract (AD-7).
	//   nil  → carry source tools key unchanged (no-op here; existing carry logic handles it)
	//   []   → remove the tools key (non-nil empty is an authoritative removal)
	//   [...] → replace or add the tools key with the supplied list in flow-style order (AD-6)
	if in.Tools != nil {
		if len(in.Tools) == 0 {
			fm.Remove("tools")
		} else {
			items := make([]domain.FieldValue, len(in.Tools))
			for i, t := range in.Tools {
				items[i] = domain.ScalarValue(t, domain.QuotePlain)
			}
			fm.Set("tools", domain.ListValue(items, domain.ListFlow))
		}
	}

	// Apply recovered generic-only fields per AD-12: a non-empty value is written, an
	// empty value is omitted entirely (never written blank). Keys already carried from the
	// source are overwritten in place; absent keys are appended after the reorder step
	// so they follow the carried keys in the fixed order tools, recommended_tier,
	// tier_rationale, required_skills (AD-13).
	if in.RecommendedTier != "" {
		fm.Set("recommended_tier", domain.ScalarValue(in.RecommendedTier, domain.QuotePlain))
	}
	if in.TierRationale != "" {
		fm.Set("tier_rationale", domain.ScalarValue(in.TierRationale, domain.QuotePlain))
	}
	if len(in.RequiredSkills) > 0 {
		items := make([]domain.FieldValue, len(in.RequiredSkills))
		for i, s := range in.RequiredSkills {
			items[i] = domain.ScalarValue(s, domain.QuotePlain)
		}
		fm.Set("required_skills", domain.ListValue(items, domain.ListFlow))
	}

	// Reorder: generated keys first (in policy order), then every other surviving key in
	// its original relative order. Reorder appends unlisted keys after the listed ones.
	fm.Reorder([]string{"id", "version", "name", "role"})

	// Strip every injection region to an empty tag pair. The deploy pipeline injects
	// harness-specific content at deploy time; no harness content belongs in the generic
	// source. Clear removes only the items slice; the open and close tags are preserved.
	for _, node := range out.Body().Injections() {
		node.Clear()
	}

	// Strip every deployed region to an empty tag pair. Generic source files carry empty
	// deployed placeholders; the deploy pipeline fills them with canonical content.
	// transform.ErrSourceDeployedRegionNotEmpty rejects a source whose deployed regions
	// carry content, so this step is required for the generated file to be usable.
	for _, node := range out.Body().DeployedRegions() {
		node.Clear()
	}

	return out.Bytes(), nil
}
