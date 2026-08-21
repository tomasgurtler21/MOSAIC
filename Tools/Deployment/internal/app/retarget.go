package app

// retarget.go declares and implements the Stage 5 harness-to-harness transform:
//   - HarnessMatchStatus / HarnessMatchVerdict / DetectHarnessMatch
//   - RetargetInput / RetargetReport / BuildRetargetedAgent
//   - Five sentinel errors
//
// Both DetectHarnessMatch and BuildRetargetedAgent are pure (no I/O) and deterministic.
// They must not read from or write to the filesystem, and must not call any function
// that does. Harness modules arrive as arguments; the registry is the caller's concern.

import (
	"errors"
	"fmt"
	"strings"

	"mosaic-common/docformat"
	"mosaic-deploy/internal/agentfields"
	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/harness/descriptor"
)

// ---------------------------------------------------------------------------
// Sentinel errors
// ---------------------------------------------------------------------------

// ErrRetargetNotTransformed reports a source file that fails the shared eligibility
// check and therefore cannot be transformed. The wrapping error carries the verdict's
// Reason.
var ErrRetargetNotTransformed = errors.New("file is not an already-transformed MOSAIC agent")

// ErrRetargetHarnessMismatch reports a file whose detected harness differs from the
// declared source harness. The wrapping error carries the match verdict's Reason.
var ErrRetargetHarnessMismatch = errors.New("file's detected harness does not match the selected source harness")

// ErrTransformDestinationExists reports a destination file that already exists when
// Overwrite was not requested.
var ErrTransformDestinationExists = errors.New("destination file already exists")

// ErrTransformSameHarness reports a request whose source and target harness are identical.
var ErrTransformSameHarness = errors.New("source and target harness must differ")

// ErrRetargetMissingAgentKey reports a RetargetInput with an empty AgentKey. The core
// never derives a key from file content; the caller must supply it.
var ErrRetargetMissingAgentKey = errors.New("retarget input requires an agent key")

// ---------------------------------------------------------------------------
// Harness match detection
// ---------------------------------------------------------------------------

// HarnessMatchStatus is the outcome of comparing a deployed file's shape against one
// harness's contract.
type HarnessMatchStatus string

const (
	// HarnessMatchYes — positive distinguishing evidence for this harness and none against.
	HarnessMatchYes HarnessMatchStatus = "match"
	// HarnessMatchNo — positive evidence the file belongs to a different harness.
	HarnessMatchNo HarnessMatchStatus = "mismatch"
	// HarnessMatchIndeterminate — the file is a transformed MOSAIC agent but carries no
	// signal distinguishing this harness from another. Never reported as a mismatch.
	HarnessMatchIndeterminate HarnessMatchStatus = "indeterminate"
	// HarnessMatchNotAgent — the file is not a transformed MOSAIC agent at all. Reason
	// carries the EligibilityVerdict's Reason verbatim.
	HarnessMatchNotAgent HarnessMatchStatus = "not-an-agent"
)

// HarnessMatchVerdict mirrors EligibilityVerdict's shape: a status plus a human-readable
// reason, never an error return.
type HarnessMatchVerdict struct {
	// Status is the detection outcome.
	Status HarnessMatchStatus
	// Reason explains any status other than HarnessMatchYes, in user-facing language.
	// Empty when Status is HarnessMatchYes.
	Reason string
	// Evidence lists the distinguishing signals found, in detection order, for diagnostics
	// and test assertions. e.g. "tools key \"tools\"", "field \"mcpServers\"",
	// "tool name \"Bash\"".
	Evidence []string
}

// DetectHarnessMatch decides whether src is a deployed agent of the harness that module
// describes.
//
// It first defers to the shared eligibility check for "is this a transformed MOSAIC agent
// at all"; a negative verdict is returned as HarnessMatchNotAgent with the reason preserved.
// It then compares the file's frontmatter key vocabulary and tool names against the harness's
// model key, tools key, frontmatter-plan Set keys, spec Drop keys, KeyOrder entries, diverted
// tool-destination keys, and Tools.Universe names.
//
// Conservative by contract: HarnessMatchNo requires positive evidence of a different harness.
// Absence of this harness's signals alone yields HarnessMatchIndeterminate. Mixed evidence —
// positive harness signals alongside fields unknown to this harness — also yields
// HarnessMatchIndeterminate rather than a hard mismatch, because an unknown field could be a
// hand-added annotation on a file that genuinely belongs to this harness. HarnessMatchNo is
// reserved for files that carry unknown fields and no positive identification for this harness.
//
// Pure: no filesystem or network access, no mutation of src. Calls only module.Descriptor()
// and module.Frontmatter(...), both contractually deterministic. A module.Frontmatter error
// degrades the check to descriptor-declared keys only; it is never surfaced as an error.
func DetectHarnessMatch(src []byte, module domain.HarnessModule, kind domain.ArtifactKind, agentKey string) HarnessMatchVerdict {
	// Step 1: defer to the shared eligibility check.
	ev := eligibleHarnessOnly(src)
	if !ev.Eligible {
		return HarnessMatchVerdict{
			Status: HarnessMatchNotAgent,
			Reason: ev.Reason,
		}
	}

	// Parse the document. eligibleHarnessOnly already succeeded, but guard defensively.
	doc, err := docformat.Parse(src)
	if err != nil {
		return HarnessMatchVerdict{
			Status: HarnessMatchNotAgent,
			Reason: "document could not be parsed: " + err.Error(),
		}
	}

	d := module.Descriptor()

	// Get the frontmatter plan; degrade gracefully on any module error.
	var plan domain.FrontmatterPlan
	if p, ferr := module.Frontmatter(domain.FrontmatterRequest{Kind: kind, AgentKey: agentKey}); ferr == nil {
		plan = p
	}

	// Build the field classifier for this harness.
	clf := descriptor.NewFieldClassifier(d, plan)

	// Build Universe tool-name lookup for positive evidence from tool names.
	universeNames := make(map[string]bool, len(d.Tools.Universe))
	for _, t := range d.Tools.Universe {
		universeNames[t.Name] = true
	}

	fm := doc.Frontmatter()
	var evidence []string
	hasPositive := false
	hasNegative := false
	var negativeReasons []string

	// Scan frontmatter keys for classification signals.
	//   ClassHarness / ClassDivertedTool  → positive evidence (this harness owns this key)
	//   ClassUnknown                       → negative evidence (key unknown to this harness)
	//   ClassMosaic                        → neutral; no evidence recorded
	for _, key := range fm.Keys() {
		class := clf.Classify(key)
		switch class {
		case descriptor.ClassHarness, descriptor.ClassDivertedTool:
			hasPositive = true
			evidence = append(evidence, fmt.Sprintf("field %q", key))
		case descriptor.ClassUnknown:
			hasNegative = true
			negativeReasons = append(negativeReasons, fmt.Sprintf("unknown field %q", key))
			evidence = append(evidence, fmt.Sprintf("unknown field %q", key))
		}
	}

	// Scan tool names under the harness's own tools key for Universe membership.
	if d.Frontmatter.ToolsKey != "" {
		if v, ok := fm.Get(d.Frontmatter.ToolsKey); ok {
			for _, name := range descriptor.ExtractToolEntries(d, v) {
				if universeNames[name] {
					hasPositive = true
					evidence = append(evidence, fmt.Sprintf("tool name %q", name))
				}
			}
		}
	}

	// Scan diverted field tool names for Universe membership.
	for _, df := range descriptor.DivertedToolFields(d) {
		if v, ok := fm.Get(df.Key); ok {
			for _, name := range descriptor.ExtractDivertedToolEntries(df, v) {
				if universeNames[name] {
					hasPositive = true
					evidence = append(evidence, fmt.Sprintf("diverted tool name %q", name))
				}
			}
		}
	}

	// Determine verdict.
	//
	// Conservative by contract: HarnessMatchNo requires positive evidence of a different harness.
	// An unknown field alone is not such evidence — it could be a hand-added annotation on a file
	// that genuinely belongs to this harness. The verdict rules are:
	//
	//   hasNegative && !hasPositive → HarnessMatchNo: the file has fields unknown to this harness
	//     and no fields that positively identify it as this harness's. The unknown fields are the
	//     only distinguishing signal and suggest the file belongs to a different harness.
	//
	//   hasPositive && hasNegative → HarnessMatchIndeterminate: mixed evidence. The file carries
	//     harness-identifying signals alongside fields unknown to this harness. Ownership is
	//     ambiguous; unknown fields in the presence of positive same-harness evidence are not
	//     sufficient for a hard mismatch verdict.
	//
	//   hasPositive && !hasNegative → HarnessMatchYes: unambiguous positive identification.
	//
	//   !hasPositive && !hasNegative → HarnessMatchIndeterminate: no distinguishing signal.
	if hasNegative && !hasPositive {
		return HarnessMatchVerdict{
			Status:   HarnessMatchNo,
			Reason:   "file carries fields unknown to this harness and no fields identifying it as this harness: " + strings.Join(negativeReasons, ", "),
			Evidence: evidence,
		}
	}
	if hasPositive && hasNegative {
		return HarnessMatchVerdict{
			Status:   HarnessMatchIndeterminate,
			Reason:   "file carries harness-positive fields alongside fields unknown to this harness; ownership is ambiguous",
			Evidence: evidence,
		}
	}
	if hasPositive {
		return HarnessMatchVerdict{
			Status:   HarnessMatchYes,
			Evidence: evidence,
		}
	}
	// No distinguishing signal in either direction.
	return HarnessMatchVerdict{
		Status:   HarnessMatchIndeterminate,
		Reason:   "no signal distinguishing this harness from another was found; the transform proceeded normally",
		Evidence: evidence,
	}
}

// ---------------------------------------------------------------------------
// Harness-to-harness transform core
// ---------------------------------------------------------------------------

// RetargetInput carries everything BuildRetargetedAgent needs. Every decision the core
// cannot make alone arrives here rather than being invented or read from disk.
type RetargetInput struct {
	// Source is the deployed agent file bytes for the source harness, verbatim. Never mutated.
	Source []byte
	// SourceModule describes the harness the file was deployed for. Obtained via registry.Resolve.
	SourceModule domain.HarnessModule
	// TargetModule describes the harness to produce. Obtained via registry.Resolve.
	TargetModule domain.HarnessModule
	// Kind is the artifact kind, for the target module's Frontmatter and TargetPath calls.
	// Zero value is treated as domain.ArtifactAgent.
	Kind domain.ArtifactKind
	// AgentKey is the artifact slug, used for the target module's per-artifact plan shaping
	// and its TargetPath call. Required and always caller-supplied — there is no fallback and
	// the core never derives it. Empty yields ErrRetargetMissingAgentKey.
	AgentKey string
	// TargetModel is the target harness's model identifier. Empty leaves the target's
	// model field present and empty (FR-15).
	TargetModel string
	// ToolMappingsVersion is the effective tool-destination mapping hash for the TARGET
	// harness, computed by the service layer from project + user config. Empty when the
	// target has no config mappings.
	ToolMappingsVersion string
}

// RetargetReport is the audit trail of one file's transform.
type RetargetReport struct {
	// AgentKey is the artifact slug the transform resolved.
	AgentKey string
	// SourceHarnessID and TargetHarnessID are the two harness ids involved.
	SourceHarnessID string
	TargetHarnessID string
	// Tools is one entry per generic tool the reverse leg recovered, in recovery order,
	// recording how the target harness resolved it.
	Tools []domain.ToolResolution
	// CarriedVerbatimTools lists source tool names with no known mapping, kept under the
	// same name in the target output (FR-14).
	CarriedVerbatimTools []string
	// StrippedFields lists fields removed from the output and why.
	StrippedFields []StrippedField
	// InjectionsPreserved names every injection region carried through, in
	// document order. Present so a test can assert preservation without diffing bytes.
	InjectionsPreserved []string
}

// BuildRetargetedAgent produces the bytes of a deployed agent in the target harness's form
// from the bytes of that agent deployed for the source harness.
//
// Pipeline shape: deployed(source) -> generic-shaped intermediate -> deployed(target).
// The reverse leg reuses the Stage 2 shared classification and diverted-field recovery
// against the source descriptor. The forward leg applies the target module's Tools(...) and
// Frontmatter(...) output, exactly as the deploy path does.
//
// Differences from buildGenericAgent, and the only permitted divergence from it:
//   - every injection region's content is carried through byte-for-byte;
//   - every managed region's content is carried through byte-for-byte;
//   - the output is a deployed file, so MOSAIC-only bookkeeping fields carry their
//     agentfields Deployed (mosaic_-prefixed) names.
//
// The function is pure: in.Source is not modified and no file is written or read. Identical
// inputs produce byte-identical output.
func BuildRetargetedAgent(in RetargetInput) ([]byte, RetargetReport, error) {
	// Validate: AgentKey is required; the core never derives it from file content.
	if in.AgentKey == "" {
		return nil, RetargetReport{}, ErrRetargetMissingAgentKey
	}

	// Normalize Kind: zero value is treated as domain.ArtifactAgent.
	kind := in.Kind
	if kind == "" {
		kind = domain.ArtifactAgent
	}

	// Eligibility check — the same shared two-signal rule as buildGenericAgent and the
	// Update discovery scan. An ineligible source cannot be transformed.
	ev := eligibleHarnessOnly(in.Source)
	if !ev.Eligible {
		return nil, RetargetReport{}, fmt.Errorf("%w: %s", ErrRetargetNotTransformed, ev.Reason)
	}

	// Parse source. eligibleHarnessOnly already succeeded, but guard defensively.
	doc, err := docformat.Parse(in.Source)
	if err != nil {
		return nil, RetargetReport{}, fmt.Errorf("%w: %s", ErrRetargetNotTransformed, err.Error())
	}

	// Collect injection names before cloning (for the InjectionsPreserved report field).
	// The content itself is preserved by not calling node.Clear() — unlike buildGenericAgent.
	var injectionsPreserved []string
	for _, node := range doc.Body().Injections() {
		injectionsPreserved = append(injectionsPreserved, node.Name())
	}

	// Clone the source so in.Source is never mutated.
	out := doc.Clone()
	fm := out.Frontmatter()

	// --- Source module setup ---

	srcDesc := in.SourceModule.Descriptor()
	var srcPlan domain.FrontmatterPlan
	if p, ferr := in.SourceModule.Frontmatter(domain.FrontmatterRequest{Kind: kind, AgentKey: in.AgentKey}); ferr == nil {
		srcPlan = p
	}
	srcClassifier := descriptor.NewFieldClassifier(srcDesc, srcPlan)

	// --- Target module setup ---

	tgtDesc := in.TargetModule.Descriptor()
	var tgtPlan domain.FrontmatterPlan
	if p, ferr := in.TargetModule.Frontmatter(domain.FrontmatterRequest{Kind: kind, AgentKey: in.AgentKey}); ferr == nil {
		tgtPlan = p
	}

	// =========================================================================
	// REVERSE LEG: extract generic tool list from source harness representation
	// =========================================================================

	// Extract harness tool names from the source's main tools field.
	var mainToolsValue domain.FieldValue
	if srcDesc.Frontmatter.ToolsKey != "" {
		if v, ok := fm.Get(srcDesc.Frontmatter.ToolsKey); ok {
			mainToolsValue = v
		}
	}
	mainHarnessTools := descriptor.ExtractToolEntries(srcDesc, mainToolsValue)

	// Reverse-map main harness tools to their generic names.
	// Tools that cannot be reverse-mapped are carried verbatim (FR-14).
	var genericTools []string   // generic names for the forward leg
	var customTools []string    // source names with no mapping — carry verbatim
	var strippedFields []StrippedField
	seenGeneric := make(map[string]bool)

	for _, harnessName := range mainHarnessTools {
		genericName, ok := descriptor.ReverseMapTool(srcDesc, harnessName)
		if ok {
			if !seenGeneric[genericName] {
				seenGeneric[genericName] = true
				genericTools = append(genericTools, genericName)
			}
		} else {
			// No mapping for this source tool → carry verbatim to target main tools field.
			customTools = append(customTools, harnessName)
		}
	}

	// Process diverted tool fields from the source descriptor.
	// Entries that reverse-map contribute to genericTools;
	// entries that do not are stripped and reported.
	srcDivertedFields := descriptor.DivertedToolFields(srcDesc)
	for _, df := range srcDivertedFields {
		var fieldValue domain.FieldValue
		if v, ok := fm.Get(df.Key); ok {
			fieldValue = v
		}
		entries := descriptor.ExtractDivertedToolEntries(df, fieldValue)
		var unmapped []string
		for _, entry := range entries {
			genericName, ok := descriptor.ReverseMapTool(srcDesc, entry)
			if ok {
				if !seenGeneric[genericName] {
					seenGeneric[genericName] = true
					genericTools = append(genericTools, genericName)
				}
			} else {
				unmapped = append(unmapped, entry)
			}
		}
		// Any unmapped entries from a diverted field are reported as stripped.
		if len(unmapped) > 0 {
			strippedFields = append(strippedFields, StrippedField{
				Key:    df.Key,
				Values: unmapped,
				Reason: StripReasonUnmappedDivertedValue,
			})
		}
	}

	// =========================================================================
	// FRONTMATTER CLEANUP: build the generic-shaped intermediate
	// =========================================================================

	// Build the set of keys to remove unconditionally regardless of their class.
	// This covers:
	//   1. MOSAIC stamps (both prefixed and legacy forms) — re-stamped with target values.
	//   2. The generic "model" key and the source's own ModelKey — replaced by target's.
	//   3. The generic "tools" key and the source's own ToolsKey — replaced by target's.
	removeKeys := make(map[string]bool)

	// Read bundle_version before adding its names to removeKeys, so the value is captured
	// before removal. Bundle content is harness-independent and is carried through as
	// mosaic_bundle_version in the target output.
	bvField, _ := agentfields.ByDeployedName("bundle_version")
	var bundleVersion string
	for _, key := range agentfields.ReadOrder(bvField) {
		if v, ok := fm.Get(key); ok && v.Kind == domain.KindScalar {
			bundleVersion = v.Scalar
			break
		}
	}
	// Both prefixed and legacy bundle_version forms are removed; the value is re-written
	// as mosaic_bundle_version after the forward leg, preserving the original value.
	removeKeys[bvField.Deployed] = true
	removeKeys[bvField.Legacy] = true

	// Read role before adding its names to removeKeys, so the value is captured before
	// removal. The role is harness-independent and is carried through as mosaic_role in
	// the target output, whichever key form the source used.
	roleField, _ := agentfields.ByGeneric("role")
	var roleValue string
	for _, key := range agentfields.ReadOrder(roleField) {
		if v, ok := fm.Get(key); ok && v.Kind == domain.KindScalar {
			roleValue = v.Scalar
			break
		}
	}
	// Both prefixed and legacy role forms are removed; the value is re-written as
	// mosaic_role after the forward leg, preserving the original value.
	removeKeys[roleField.Deployed] = true
	removeKeys[roleField.Legacy] = true

	// All other MOSAIC stamp fields (both prefixed and legacy forms), except id.
	// The id field is the agent's identity and is kept as-is.
	for _, f := range agentfields.All() {
		if f.Generic == "id" {
			continue // id is kept; promote restores it as generic "id" but retarget keeps it
		}
		if f.Legacy == "bundle_version" {
			continue // handled above
		}
		if f.Generic == "role" {
			continue // handled above
		}
		if f.Deployed != "" {
			removeKeys[f.Deployed] = true
		}
		if f.Legacy != "" && f.Legacy != f.Deployed {
			removeKeys[f.Legacy] = true
		}
	}

	// Generic "model" key (carries the source harness's model; replaced by target's ModelKey).
	removeKeys["model"] = true
	// Source harness's own ModelKey (may differ from "model", e.g. "model_id").
	if srcDesc.Frontmatter.ModelKey != "" {
		removeKeys[srcDesc.Frontmatter.ModelKey] = true
	}

	// Generic "tools" key (carries source harness tool names; replaced by target's tool result).
	removeKeys["tools"] = true
	// Source harness's own ToolsKey (may differ from "tools", e.g. "allowed_tools").
	if srcDesc.Frontmatter.ToolsKey != "" {
		removeKeys[srcDesc.Frontmatter.ToolsKey] = true
	}

	// Iterate over a snapshot of the frontmatter keys to avoid modifying the slice
	// while iterating. For each key: remove it if it is in removeKeys; otherwise classify
	// it with the source classifier and act on the class.
	keySnapshot := fm.Keys()
	allKeys := make([]string, len(keySnapshot))
	copy(allKeys, keySnapshot)

	for _, key := range allKeys {
		if removeKeys[key] {
			fm.Remove(key)
			continue
		}
		class := srcClassifier.Classify(key)
		switch class {
		case descriptor.ClassHarness:
			// Source-harness-specific field (e.g. alpha_stamp): drop it.
			fm.Remove(key)
		case descriptor.ClassDivertedTool:
			// Source-harness diverted tool field: already processed above; drop it.
			// Any unmapped values were recorded in strippedFields during diverted processing.
			fm.Remove(key)
		case descriptor.ClassUnknown:
			// Unknown to both MOSAIC and the source harness: strip and report.
			v, _ := fm.Get(key)
			strippedFields = append(strippedFields, StrippedField{
				Key:    key,
				Values: descriptor.RenderFieldValues(v),
				Reason: StripReasonUnknownField,
			})
			fm.Remove(key)
			// ClassMosaic: keep as-is (version, name, description, role, id, etc.).
		}
	}

	// =========================================================================
	// FORWARD LEG: apply the target harness's deployed form
	// =========================================================================

	// Apply target plan: Remove keys the target drops at deploy time.
	for _, key := range tgtPlan.Remove {
		fm.Remove(key)
	}
	// Apply target plan: Set keys the target adds at deploy time (e.g. stamps, harness keys).
	for _, field := range tgtPlan.Set {
		fm.Set(field.Key, field.Value)
	}

	// Set target model field. Always written — even when TargetModel is empty, the field is
	// written with an empty scalar so the key is present (FR-15).
	if tgtDesc.Frontmatter.ModelKey != "" {
		fm.Set(tgtDesc.Frontmatter.ModelKey, domain.ScalarValue(in.TargetModel, domain.QuotePlain))
	}

	// Stamp target version fields under their mosaic_-prefixed deployed names.
	// No literal prefixed key names appear here; all names come from agentfields.ByDeployedName.
	// The harness version stamp uses the new entry (harness_version), writing as
	// mosaic_harness_version. The old mosaic_transform_version key is not written.
	tvField, _ := agentfields.ByDeployedName("harness_version")
	if tgtDesc.TransformVersion != "" {
		fm.Set(tvField.Deployed, domain.ScalarValue(tgtDesc.TransformVersion, domain.QuotePlain))
	}
	// mosaic_injections_version is written here and then stripped by the migration step below,
	// consistent with applyFrontmatter. Stage 2 will remove the write entirely.
	ivField, _ := agentfields.ByDeployedName("injections_version")
	if tgtDesc.InjectionsVersion != "" {
		fm.Set(ivField.Deployed, domain.ScalarValue(tgtDesc.InjectionsVersion, domain.QuotePlain))
	}
	// Migration: strip legacy fields that have been renamed or relocated.
	// mosaic_injections_version is stripped here (pre-stripping for Stage 2 relocation to tags).
	// mosaic_orchestrator_injections_version is also stripped for the same reason.
	fm.Remove(ivField.Deployed)
	oivField, _ := agentfields.ByDeployedName("orchestrator_injections_version")
	fm.Remove(oivField.Deployed)
	tmvField, _ := agentfields.ByDeployedName("tool_mappings_version")
	if in.ToolMappingsVersion != "" {
		fm.Set(tmvField.Deployed, domain.ScalarValue(in.ToolMappingsVersion, domain.QuotePlain))
	}
	// mosaic_orchestrator_injections_version is intentionally NOT stamped here from the
	// descriptor. The target module's own Frontmatter() method is the sole authority for this
	// field, because only the module knows which agents should carry it (e.g. orchestrator-only).
	// The target plan's Set entries (applied above) already include the field when appropriate.

	// Carry bundle_version through as mosaic_bundle_version (harness-independent content).
	if bundleVersion != "" {
		fm.Set(bvField.Deployed, domain.ScalarValue(bundleVersion, domain.QuotePlain))
	}

	// Carry role through as mosaic_role (harness-independent field). The value was captured
	// from either the prefixed or legacy form of the source; it is always re-written under
	// the prefixed deployed key so the retargeted file uses the canonical deployed form.
	if roleValue != "" {
		fm.Set(roleField.Deployed, domain.ScalarValue(roleValue, domain.QuotePlain))
	}

	// Forward-map generic tools to target harness tool names.
	toolReq := domain.ToolRequest{
		AgentKey: in.AgentKey,
		Generic:  genericTools,
	}
	toolResult, err := in.TargetModule.Tools(toolReq)
	if err != nil {
		return nil, RetargetReport{}, fmt.Errorf("target module Tools failed: %w", err)
	}

	// Append custom (verbatim-carried) tools to the target's main tools field.
	// Custom tools are source harness names that have no generic reverse mapping and are
	// kept under their original name in the target output's main tools key (FR-14).
	if len(customTools) > 0 && tgtDesc.Frontmatter.ToolsKey != "" {
		mainKey := tgtDesc.Frontmatter.ToolsKey
		mainIdx := -1
		for i, f := range toolResult.Fields {
			if f.Key == mainKey {
				mainIdx = i
				break
			}
		}
		if mainIdx >= 0 {
			// Append custom tools to the existing main tools field.
			v := toolResult.Fields[mainIdx].Value
			for _, ct := range customTools {
				v.Items = append(v.Items, domain.FieldValue{Kind: domain.KindScalar, Scalar: ct})
			}
			toolResult.Fields[mainIdx] = domain.FrontmatterField{Key: mainKey, Value: v}
		} else {
			// No main tools field was created (all generic tools routed to DestField or no
			// generic tools at all). Create one with the custom tools.
			items := make([]domain.FieldValue, len(customTools))
			for i, ct := range customTools {
				items[i] = domain.FieldValue{Kind: domain.KindScalar, Scalar: ct}
			}
			prepended := make([]domain.FrontmatterField, 0, len(toolResult.Fields)+1)
			prepended = append(prepended, domain.FrontmatterField{
				Key:   mainKey,
				Value: domain.ListValue(items, domain.ListBlock),
			})
			prepended = append(prepended, toolResult.Fields...)
			toolResult.Fields = prepended
		}
	}

	// Apply all tool result fields (main tools key and any diverted fields).
	for _, field := range toolResult.Fields {
		fm.Set(field.Key, field.Value)
	}

	// Reorder frontmatter keys per the target harness's declared key order.
	// Unlisted keys keep their current relative order and are appended after listed keys.
	if len(tgtPlan.KeyOrder) > 0 {
		fm.Reorder(tgtPlan.KeyOrder)
	}

	// NOTE: injection and deployed region content is NOT stripped here. Unlike
	// buildGenericAgent (which calls node.Clear() on both injection and deployed regions),
	// BuildRetargetedAgent carries region content through verbatim — this is the only
	// permitted divergence from the promote generation core.

	// Build the report.
	report := RetargetReport{
		AgentKey:             in.AgentKey,
		SourceHarnessID:      srcDesc.ID,
		TargetHarnessID:      tgtDesc.ID,
		Tools:                toolResult.Resolutions,
		CarriedVerbatimTools: customTools,
		StrippedFields:       strippedFields,
		InjectionsPreserved:  injectionsPreserved,
	}

	return out.Bytes(), report, nil
}
