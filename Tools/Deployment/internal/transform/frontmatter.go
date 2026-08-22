package transform

import (
	"strings"

	"mosaic-common/docformat"
	"mosaic-deploy/internal/agentfields"
	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/harness/descriptor"
)

// resolveTools determines the tool frontmatter output for the transformation. It handles
// two distinct cases:
//
//   - Placeholder case: the source declares tools as a scalar placeholder (e.g.
//     `tools: {tool-permissions}`). The placeholder is expanded using the descriptor's
//     PlaceholderExpansion list. Report.Tools is an empty, non-nil slice because the
//     expansion is not decomposed into per-tool resolutions at this stage.
//
//   - List case: the source declares a concrete list of generic tool names. These are
//     passed to Module.Tools, which maps them to harness-specific names and builds the
//     frontmatter field(s). Report.Tools mirrors the resolution list from the module.
//
// If the source has no "tools" field, or if the descriptor declares no tools_key, a safe
// empty result is returned. All harness-specific tool knowledge flows through Module.Tools
// or through the descriptor's PlaceholderExpansion; no harness name appears here (AC8.4).
func resolveTools(req Request, fm *docformat.Frontmatter, desc *domain.HarnessDescriptor) (domain.ToolResult, error) {
	v, ok := fm.Get("tools")
	if !ok {
		// Source has no tools field — return empty, non-nil Resolutions for a safe report.
		return domain.ToolResult{Resolutions: make([]domain.ToolResolution, 0)}, nil
	}

	if v.Kind == domain.KindScalar {
		// Placeholder case: route through module.Tools() with Placeholder set so the module
		// controls the output format. Built-in modules may render the placeholder expansion
		// differently (e.g., Claude Code uses a comma-separated scalar while GHCP CLI uses a
		// flow-style single-quoted list). The Placeholder field carries the verbatim source
		// placeholder text (e.g., "{tool-permissions}").
		toolReq := domain.ToolRequest{
			AgentKey:     req.Key,
			Placeholder:  v.Scalar,
			CustomNames:  req.CustomTools,
			SkippedTools: req.SkippedTools,
		}
		result, err := req.Module.Tools(toolReq)
		if err != nil {
			return domain.ToolResult{}, err
		}
		if result.Resolutions == nil {
			result.Resolutions = make([]domain.ToolResolution, 0)
		}
		return result, nil
	}

	// List case: extract generic tool names and call Module.Tools.
	var genericTools []string
	if v.Kind == domain.KindList {
		genericTools = make([]string, len(v.Items))
		for i, item := range v.Items {
			genericTools[i] = item.Scalar
		}
	}

	toolReq := domain.ToolRequest{
		AgentKey:     req.Key,
		Generic:      genericTools,
		CustomNames:  req.CustomTools,
		SkippedTools: req.SkippedTools,
	}
	result, err := req.Module.Tools(toolReq)
	if err != nil {
		return domain.ToolResult{}, err
	}
	if result.Resolutions == nil {
		result.Resolutions = make([]domain.ToolResolution, 0)
	}
	return result, nil
}

// applyFrontmatter applies the descriptor-driven frontmatter plan (removes, adds, and
// key order) together with the model selection, version stamps, and resolved tool fields.
// It mutates fm in place and returns:
//   - fieldChanges: one FieldChange per frontmatter key that was added, overwritten, or
//     removed (satisfying AC8.5).
//   - gaps: domain.Gap entries produced during the transformation (e.g. GapNoModel).
//
// Step order is significant: removes happen before adds so that a key that is dropped and
// re-added with a different value under the same name works correctly. Version stamps and
// tool fields are applied after the descriptor plan so that the plan's key-order list can
// include them in the desired position.
func applyFrontmatter(
	fm *docformat.Frontmatter,
	plan domain.FrontmatterPlan,
	toolResult domain.ToolResult,
	req Request,
	desc *domain.HarnessDescriptor,
) ([]FieldChange, []domain.Gap) {
	var changes []FieldChange
	var gaps []domain.Gap

	// Step 1: Remove fields declared in the descriptor drop list.
	for _, key := range plan.Remove {
		if v, ok := fm.Get(key); ok {
			changes = append(changes, FieldChange{
				Key:    key,
				Before: renderValue(v),
				After:  "",
				Reason: "descriptor drop",
			})
			fm.Remove(key)
		}
	}

	// Step 2: Add or overwrite fields from the descriptor add list.
	for _, field := range plan.Set {
		before := ""
		if v, ok := fm.Get(field.Key); ok {
			before = renderValue(v)
		}
		changes = append(changes, FieldChange{
			Key:    field.Key,
			Before: before,
			After:  renderValue(field.Value),
			Reason: "descriptor add",
		})
		fm.Set(field.Key, field.Value)
	}

	// Step 3: Set the model field from the resolved model selection.
	modelKey := desc.Frontmatter.ModelKey
	if modelKey != "" {
		if req.Model.Resolved() {
			before := ""
			if v, ok := fm.Get(modelKey); ok {
				before = renderValue(v)
			}
			changes = append(changes, FieldChange{
				Key:    modelKey,
				Before: before,
				After:  req.Model.ModelID,
				Reason: "model selection",
			})
			fm.Set(modelKey, domain.ScalarValue(req.Model.ModelID, domain.QuotePlain))
		} else {
			// No model resolved: remove the placeholder field and record a gap so the
			// TODO checklist can surface the missing model for the user.
			if v, ok := fm.Get(modelKey); ok {
				changes = append(changes, FieldChange{
					Key:    modelKey,
					Before: renderValue(v),
					After:  "",
					Reason: "model unresolved",
				})
				fm.Remove(modelKey)
			}
			gaps = append(gaps, domain.Gap{
				Kind:    domain.GapNoModel,
				Subject: req.Key,
				Detail:  "no model selected for this agent",
			})
		}
	}

	// Step 4: Stamp version fields under their mosaic_-prefixed deployed names, derived from
	// the agentfields registry. No literal prefixed key names appear here; all names come from
	// agentfields.ByDeployedName (which looks up by the legacy name) and uses field.Deployed.
	// The harness version stamp is written as mosaic_harness_version (via the new registry
	// entry with Legacy="harness_version"). The old mosaic_transform_version key is no longer
	// written here; it is stripped by the migration step below if present from a legacy file.
	tvField, _ := agentfields.ByDeployedName("harness_version")
	if c := applyVersionStamp(fm, tvField.Deployed, desc.TransformVersion); c != nil {
		changes = append(changes, *c)
	}
	// injections_version is no longer stamped as a frontmatter field; it is written as a
	// version attribute on InjectionHarness-class region tags by applyHarnessRegion instead.
	// The ivField variable is retained here for the migration strip below, which removes
	// legacy mosaic_injections_version fields from pre-migration deployed files.
	ivField, _ := agentfields.ByDeployedName("injections_version")
	// tool_mappings_version is a content hash of the effective tool_destinations
	// mappings for this harness (combined project + user config). It lets `update`
	// detect stale tool mappings without re-diffing tool lists. See "The
	// tool_mappings_version stamp" in Tools/Deployment/docs/configuration.md.
	tmvField, _ := agentfields.ByDeployedName("tool_mappings_version")
	if c := applyVersionStamp(fm, tmvField.Deployed, req.ToolMappingsVersion); c != nil {
		changes = append(changes, *c)
	}

	// Step 4b: Stamp bundle_version for roles that receive bundle blocks. The stamp enables
	// staleness detection on subsequent runs. Written only when AppliesToRole is true, so the
	// orchestrator (which today receives no bundle blocks) is never stamped.
	bvField, _ := agentfields.ByDeployedName("bundle_version")
	if req.Bundle.AppliesToRole(req.Role) && req.Bundle.Version != "" {
		before := ""
		if v, ok := fm.Get(bvField.Deployed); ok {
			before = renderValue(v)
		}
		fm.Set(bvField.Deployed, domain.ScalarValue(req.Bundle.Version, domain.QuotePlain))
		changes = append(changes, FieldChange{
			Key:    bvField.Deployed,
			Before: before,
			After:  req.Bundle.Version,
			Reason: "bundle version stamp",
		})
	}

	// Step 4c: Rename the generic "id" field to its mosaic_-prefixed deployed name. The
	// generic source carries "id"; deployed files must carry the prefixed form. The
	// agentfields registry supplies both names; no prefixed literal appears here.
	idField, _ := agentfields.ByGeneric("id")
	if v, ok := fm.Get(idField.Legacy); ok {
		fm.Remove(idField.Legacy)
		fm.Set(idField.Deployed, v)
		changes = append(changes, FieldChange{
			Key:    idField.Deployed,
			Before: "",
			After:  renderValue(v),
			Reason: "id field rename to deployed form",
		})
	}

	// Step 4d: Rename the generic "role" field to its mosaic_-prefixed deployed name. The
	// generic source carries "role"; deployed files must carry the prefixed form. This is
	// parallel to the id rename above; the agentfields registry supplies both names.
	roleField, _ := agentfields.ByGeneric("role")
	if v, ok := fm.Get(roleField.Legacy); ok {
		fm.Remove(roleField.Legacy)
		fm.Set(roleField.Deployed, v)
		changes = append(changes, FieldChange{
			Key:    roleField.Deployed,
			Before: "",
			After:  renderValue(v),
			Reason: "role field rename to deployed form",
		})
	}

	// Step 4e: Rename the generic "version" field to its mosaic_-prefixed deployed name.
	// The generic source carries "version"; deployed files must carry the prefixed form
	// (mosaic_version). This is parallel to the id and role renames above; the agentfields
	// registry supplies both names so no prefixed literal appears here.
	versionField, _ := agentfields.ByGeneric("version")
	if v, ok := fm.Get(versionField.Legacy); ok {
		fm.Remove(versionField.Legacy)
		fm.Set(versionField.Deployed, v)
		changes = append(changes, FieldChange{
			Key:    versionField.Deployed,
			Before: "",
			After:  renderValue(v),
			Reason: "version field rename to deployed form",
		})
	}

	// Migration: strip legacy fields that have been renamed or relocated.
	// All lookups go through the registry — no literal mosaic_* strings.
	// mosaic_transform_version: the harness version stamp is now written as mosaic_harness_version.
	// mosaic_injections_version: will be relocated to region tag attributes in Stage 2; Stage 1
	// pre-strips it so that deployed files are already free of it when Stage 2 takes effect.
	// mosaic_orchestrator_injections_version: same — stripped here to prepare for Stage 2.
	oldTVField, _ := agentfields.ByDeployedName("transform_version")
	if fm.Remove(oldTVField.Deployed) {
		changes = append(changes, FieldChange{
			Key:    oldTVField.Deployed,
			Before: "(present)",
			After:  "",
			Reason: "migration strip",
		})
	}
	if fm.Remove(ivField.Deployed) {
		changes = append(changes, FieldChange{
			Key:    ivField.Deployed,
			Before: "(present)",
			After:  "",
			Reason: "migration strip",
		})
	}
	oivField, _ := agentfields.ByDeployedName("orchestrator_injections_version")
	if fm.Remove(oivField.Deployed) {
		changes = append(changes, FieldChange{
			Key:    oivField.Deployed,
			Before: "(present)",
			After:  "",
			Reason: "migration strip",
		})
	}

	// Steps 5 and 5b: Tool field handling.
	// On Update (req.Deployed != nil) when the deployed file has the tools/permission key:
	// preserve that field verbatim rather than re-generating it from source. This makes
	// the tools/permission field user-owned after initial Deploy — subsequent Updates never
	// overwrite user customizations (including per-command sub-permission flow mappings).
	// On Create (req.Deployed == nil), or when the deployed file cannot be parsed, or when
	// the deployed file has no tools key: fall through to the Create path (Steps 5 and 5b).
	toolsPreservedVerbatim := false
	if req.Deployed != nil && desc.Frontmatter.ToolsKey != "" {
		deployedDoc, deployedParseErr := docformat.Parse(req.Deployed)
		if deployedParseErr == nil {
			deployedFM := deployedDoc.Frontmatter()
			if deployedToolsValue, ok := deployedFM.Get(desc.Frontmatter.ToolsKey); ok {
				before := ""
				if v, ok2 := fm.Get(desc.Frontmatter.ToolsKey); ok2 {
					before = renderValue(v)
				}
				fm.Set(desc.Frontmatter.ToolsKey, deployedToolsValue)
				changes = append(changes, FieldChange{
					Key:    desc.Frontmatter.ToolsKey,
					Before: before,
					After:  renderValue(deployedToolsValue),
					Reason: "preserved from deployed file",
				})
				toolsPreservedVerbatim = true
			}
		}
		// When deployedParseErr != nil: fall through to the Create path below.
		// buildDeployedRegionMap (called earlier in the pipeline) normally catches parse
		// failures before reaching this point. If it does not, the safe fallback is to
		// compute tools from source — identical to what a first-time Deploy would produce.
		// When the deployed file parses but has no tools key: also fall through, so the
		// output always has the tools/permission key set (never left absent).
	}

	if !toolsPreservedVerbatim {
		// Step 5: Set the resolved tool fields produced by Module.Tools (or PlaceholderExpansion).
		for _, field := range toolResult.Fields {
			before := ""
			if v, ok := fm.Get(field.Key); ok {
				before = renderValue(v)
			}
			changes = append(changes, FieldChange{
				Key:    field.Key,
				Before: before,
				After:  renderValue(field.Value),
				Reason: "tool mapping",
			})
			fm.Set(field.Key, field.Value)
		}

		// Step 5b: Merge user-added tools from the deployed file. This is skipped when
		// toolsPreservedVerbatim is true because the deployed value is already set verbatim
		// and there is nothing to merge.
		if req.Deployed != nil {
			mergeChanges := mergeDeployedTools(fm, req.Deployed, desc)
			changes = append(changes, mergeChanges...)
		}
	}

	// Step 6: Accumulate gaps for unresolved tool mappings. ToolUnmapped and ToolSkipped
	// resolutions are never emitted into the output; each produces a GapUnmappedTool so the
	// TODO checklist can surface the missing tool mapping. ToolCustom resolutions are resolved
	// and must not produce a gap.
	for _, res := range toolResult.Resolutions {
		if res.Outcome == domain.ToolUnmapped || res.Outcome == domain.ToolSkipped {
			gaps = append(gaps, domain.Gap{
				Kind:    domain.GapUnmappedTool,
				Subject: req.Key,
				Detail:  "unmapped tool: " + res.Generic,
			})
		}
	}

	// Step 7: Reorder the frontmatter keys according to the descriptor's key_order.
	// Unlisted keys keep their current relative order and are appended after listed keys.
	if len(plan.KeyOrder) > 0 {
		fm.Reorder(plan.KeyOrder)
	}

	return changes, gaps
}

// applyVersionStamp sets a single version stamp field in the frontmatter and returns the
// corresponding FieldChange. Returns nil when value is empty (nothing to stamp).
func applyVersionStamp(fm *docformat.Frontmatter, key, value string) *FieldChange {
	if value == "" {
		return nil
	}
	before := ""
	if v, ok := fm.Get(key); ok {
		before = renderValue(v)
	}
	fm.Set(key, domain.ScalarValue(value, domain.QuotePlain))
	return &FieldChange{
		Key:    key,
		Before: before,
		After:  value,
		Reason: "version stamp",
	}
}

// mergeDeployedTools augments the freshly-computed tool field(s) in fm with any
// tools present in the deployed file but absent from the current computed output.
// It uses descriptor.ExtractToolEntries to parse both the deployed file's tool
// field and the current output's tool field, then appends any entries from the
// deployed set that are missing from the output set.
//
// Parameters:
//   - fm: the frontmatter being built (already has Step 5 tool fields set)
//   - deployed: the previously-deployed file bytes (req.Deployed); nil on create
//   - desc: the harness descriptor (provides ToolsKey, Tools.Shape, Tools.Universe)
//
// When deployed is nil (new deployment), this is a no-op.
// When the deployed file has no parseable tool field, this is a no-op.
// Tool names are compared at the harness-name level (the rendered form in the deployed file).
// Appended tools preserve the harness-specific rendering format.
func mergeDeployedTools(fm *docformat.Frontmatter, deployed []byte, desc *domain.HarnessDescriptor) []FieldChange {
	if desc.Frontmatter.ToolsKey == "" {
		return nil
	}

	// Parse the deployed file to extract its tool field value.
	deployedDoc, err := docformat.Parse(deployed)
	if err != nil {
		// Graceful no-op on parse failure.
		return nil
	}

	deployedToolsValue, ok := deployedDoc.Frontmatter().Get(desc.Frontmatter.ToolsKey)
	if !ok {
		// Deployed file has no tools field; nothing to merge.
		return nil
	}

	// Get the names present in the deployed file.
	deployedNames := descriptor.ExtractToolEntries(desc, deployedToolsValue)
	if len(deployedNames) == 0 {
		return nil
	}

	// Get the names already in the current output.
	outputToolsValue, _ := fm.Get(desc.Frontmatter.ToolsKey)
	outputNames := descriptor.ExtractToolEntries(desc, outputToolsValue)

	// Build a set of names already in the output.
	inOutput := make(map[string]bool, len(outputNames))
	for _, name := range outputNames {
		inOutput[name] = true
	}

	// Collect names from deployed that are absent from the output.
	var toAppend []string
	for _, name := range deployedNames {
		if !inOutput[name] {
			toAppend = append(toAppend, name)
		}
	}
	if len(toAppend) == 0 {
		return nil
	}

	// Append missing tools to the output field, preserving the harness-specific format.
	var changes []FieldChange

	if desc.Tools.Shape == domain.ShapePermission {
		// Permission shape: KindMapping — append new pairs with allow disposition.
		currentValue, _ := fm.Get(desc.Frontmatter.ToolsKey)
		allowVal := domain.ScalarValue(string(domain.Allow), domain.QuotePlain)
		var pairs []domain.FieldPair
		if currentValue.Kind == domain.KindMapping {
			pairs = append(pairs, currentValue.Pairs...)
		}
		for _, name := range toAppend {
			pairs = append(pairs, domain.FieldPair{Key: name, Value: allowVal})
		}
		newValue := domain.MappingValue(pairs)
		before := renderValue(currentValue)
		fm.Set(desc.Frontmatter.ToolsKey, newValue)
		changes = append(changes, FieldChange{
			Key:    desc.Frontmatter.ToolsKey,
			Before: before,
			After:  renderValue(newValue),
			Reason: "tool preservation",
		})
	} else {
		// List or scalar shape.
		currentValue, _ := fm.Get(desc.Frontmatter.ToolsKey)
		before := renderValue(currentValue)

		switch currentValue.Kind {
		case domain.KindList:
			// Append new scalar items to the existing list, preserving the list style.
			items := make([]domain.FieldValue, len(currentValue.Items))
			copy(items, currentValue.Items)
			for _, name := range toAppend {
				items = append(items, domain.ScalarValue(name, domain.QuotePlain))
			}
			newValue := domain.ListValue(items, currentValue.List)
			fm.Set(desc.Frontmatter.ToolsKey, newValue)
			changes = append(changes, FieldChange{
				Key:    desc.Frontmatter.ToolsKey,
				Before: before,
				After:  renderValue(newValue),
				Reason: "tool preservation",
			})
		case domain.KindScalar:
			// Comma-separated scalar — append to the existing string.
			existing := strings.TrimSpace(currentValue.Scalar)
			added := strings.Join(toAppend, ", ")
			var newScalar string
			if existing == "" {
				newScalar = added
			} else {
				newScalar = existing + ", " + added
			}
			newValue := domain.ScalarValue(newScalar, currentValue.Quote)
			fm.Set(desc.Frontmatter.ToolsKey, newValue)
			changes = append(changes, FieldChange{
				Key:    desc.Frontmatter.ToolsKey,
				Before: before,
				After:  renderValue(newValue),
				Reason: "tool preservation",
			})
		default:
			// No parseable current value — build a new list from the deployed names.
			// This handles the case where the output tools field was cleared or is absent.
			// First include the computed output names, then the preserved ones.
			allNames := append(outputNames, toAppend...)
			items := make([]domain.FieldValue, len(allNames))
			for i, name := range allNames {
				items[i] = domain.ScalarValue(name, domain.QuotePlain)
			}
			newValue := domain.ListValue(items, domain.ListFlow)
			fm.Set(desc.Frontmatter.ToolsKey, newValue)
			changes = append(changes, FieldChange{
				Key:    desc.Frontmatter.ToolsKey,
				Before: before,
				After:  renderValue(newValue),
				Reason: "tool preservation",
			})
		}
	}

	return changes
}

// renderValue converts a domain.FieldValue to a concise string for use in FieldChange
// Before/After fields. The rendering is for human-readable audit purposes only; it is
// not a YAML round-trip serialisation. Callers must not parse the returned string.
func renderValue(v domain.FieldValue) string {
	switch v.Kind {
	case domain.KindScalar:
		return v.Scalar
	case domain.KindList:
		items := make([]string, len(v.Items))
		for i, item := range v.Items {
			items[i] = renderValue(item)
		}
		return "[" + strings.Join(items, ", ") + "]"
	case domain.KindMapping:
		pairs := make([]string, len(v.Pairs))
		for i, p := range v.Pairs {
			pairs[i] = p.Key + ": " + renderValue(p.Value)
		}
		return "{" + strings.Join(pairs, ", ") + "}"
	default:
		return ""
	}
}
