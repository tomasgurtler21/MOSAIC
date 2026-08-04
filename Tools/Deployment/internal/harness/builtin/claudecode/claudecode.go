// Package claudecode implements the Claude Code built-in harness module. It embeds the
// claude-code.yaml descriptor and provides tool formatting as a comma-separated scalar
// string (the format Claude Code expects), which cannot be expressed through the descriptor
// data alone.
//
// Exceptions requiring module code beyond the descriptor (documented per I11.6):
//
//  1. Comma-separated scalar tool format: Claude Code expects tools as a flat comma-
//     separated scalar (e.g., "Read, Write, Edit"), not a YAML list. The descriptor's
//     ToolSpec.Shape = "list" describes the semantic tool set; the rendering must be a
//     KindScalar. The FieldValue schema has no "comma-separated scalar" variant, so module
//     code post-processes the descriptor-driven list into the required scalar format.
//
//  2. Skill path key subdirectory: Skills must be deployed under
//     "<skills-dir>/<key>/SKILL.md" to avoid filename collisions (all entry files share
//     the name SKILL.md). The descriptor path template is a flat directory without
//     support for an intermediate key segment, so module code handles that composition.
//
//  3. Frontmatter version stamps (model, transform_version, injections_version): these are
//     runtime values applied exclusively by the transform pipeline (Steps 3 and 4 of
//     applyFrontmatter). The module does not include them in FrontmatterPlan.Set; doing so
//     would produce duplicate FieldChange entries in transform.Report.Fields because the
//     transform applies them independently of the module plan.
//
// Registration: init() calls registry.Register so the module is available whenever this
// package is imported.
package claudecode

import (
	_ "embed"
	"fmt"
	"path"
	"path/filepath"
	"strings"

	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/harness/descriptor"
	"mosaic-deploy/internal/harness/injectionfile"
	"mosaic-deploy/internal/harness/registry"
)

//go:embed claude-code.yaml
var embeddedDescriptor []byte

func init() {
	registry.Register("claude-code", func(opts registry.BuiltinOptions) (domain.HarnessModule, error) {
		return New(opts)
	})
}

// module is the Claude Code built-in HarnessModule implementation.
type module struct {
	ref             domain.HarnessRef
	desc            *domain.HarnessDescriptor
	injections      map[string]string // parsed from HarnessInjections.md at construction time
	orchInjections  map[string]string // parsed from HarnessInjectionsOrchestrator.md at construction time
}

// New parses the embedded claude-code.yaml descriptor and reads HarnessInjections.md and
// HarnessInjectionsOrchestrator.md from the declared repository directory at construction
// time. Editing those files and rerunning the tool changes the injected content with no
// rebuild required. The descriptor YAML remains embedded.
func New(opts registry.BuiltinOptions) (domain.HarnessModule, error) {
	desc, err := descriptor.Parse(embeddedDescriptor, "builtin:claude-code")
	if err != nil {
		return nil, fmt.Errorf("parse embedded claude-code descriptor: %w", err)
	}
	contentDir := filepath.Join(opts.MosaicRoot, RepoContentDir)
	content, err := injectionfile.LoadDir(contentDir)
	if err != nil {
		return nil, fmt.Errorf("claude-code: load harness content: %w", err)
	}
	desc.OrchestratorInjectionsVersion = content.OrchestratorVersion
	ref := domain.HarnessRef{
		ID:          desc.ID,
		DisplayName: desc.DisplayName,
		Tier:        domain.TierBuiltin,
		Usable:      true,
	}
	return &module{ref: ref, desc: desc, injections: content.Shared, orchInjections: content.Orchestrator}, nil
}

// Ref returns identity and provenance. Two calls return equal values.
func (m *module) Ref() domain.HarnessRef {
	return m.ref
}

// Descriptor returns the parsed descriptor. The same pointer is returned on every call;
// callers must treat it as read-only.
func (m *module) Descriptor() *domain.HarnessDescriptor {
	return m.desc
}

// Close releases resources. Claude Code holds no external resources; always returns nil.
func (m *module) Close() error {
	return nil
}

// Tools maps generic tool names to Claude Code tools and formats the result as a
// comma-separated scalar string (e.g., "Read, Write, Edit, Bash").
//
// See package documentation for why module code is required for this format conversion.
func (m *module) Tools(req domain.ToolRequest) (domain.ToolResult, error) {
	if req.Placeholder != "" {
		return m.expandPlaceholder(), nil
	}
	result, err := descriptor.MapTools(m.desc, req)
	if err != nil {
		return domain.ToolResult{}, err
	}
	return m.convertFieldsToScalar(result), nil
}

// expandPlaceholder handles the {tool-permissions} placeholder for orchestrator agents.
// It expands the descriptor's PlaceholderExpansion (or full Universe if empty) into a
// comma-separated scalar, maintaining Claude Code's required format.
func (m *module) expandPlaceholder() domain.ToolResult {
	expansion := m.desc.Tools.PlaceholderExpansion
	if len(expansion) == 0 {
		expansion = make([]string, len(m.desc.Tools.Universe))
		for i, t := range m.desc.Tools.Universe {
			expansion[i] = t.Name
		}
	}
	scalar := strings.Join(expansion, ", ")
	return domain.ToolResult{
		Fields: []domain.FrontmatterField{{
			Key:   m.desc.Frontmatter.ToolsKey,
			Value: domain.ScalarValue(scalar, domain.QuotePlain),
		}},
		Resolutions: []domain.ToolResolution{},
	}
}

// convertFieldsToScalar converts the KindList tools field returned by descriptor.MapTools
// into a comma-separated KindScalar value.
func (m *module) convertFieldsToScalar(result domain.ToolResult) domain.ToolResult {
	if len(result.Fields) == 0 {
		return result
	}
	toolsKey := m.desc.Frontmatter.ToolsKey
	newFields := make([]domain.FrontmatterField, len(result.Fields))
	for i, f := range result.Fields {
		if f.Key == toolsKey && f.Value.Kind == domain.KindList {
			names := make([]string, len(f.Value.Items))
			for j, item := range f.Value.Items {
				names[j] = item.Scalar
			}
			newFields[i] = domain.FrontmatterField{
				Key:   f.Key,
				Value: domain.ScalarValue(strings.Join(names, ", "), domain.QuotePlain),
			}
		} else {
			newFields[i] = f
		}
	}
	return domain.ToolResult{
		Fields:      newFields,
		Resolutions: result.Resolutions,
	}
}

// Frontmatter builds the FrontmatterPlan for Claude Code agents. It returns the
// descriptor's static Add fields (empty for Claude Code), remove list, and key order.
// Model and version stamps are applied exclusively by the transform pipeline and must
// not appear here; including them would produce duplicate FieldChange entries in
// transform.Report.Fields.
//
// Exception: orchestrator_injections_version is stamped here (not by the transform
// pipeline) because it is conditional on AgentKey — only the orchestrator agent
// receives this field. The transform pipeline applies version stamps uniformly;
// it cannot express this per-agent-key conditional. For non-orchestrator agents,
// orchestrator_injections_version is also excluded from the returned KeyOrder so it
// does not appear in the deployed subagent frontmatter at all.
func (m *module) Frontmatter(req domain.FrontmatterRequest) (domain.FrontmatterPlan, error) {
	var set []domain.FrontmatterField
	if len(m.desc.Frontmatter.Add) > 0 {
		set = append(set, m.desc.Frontmatter.Add...)
	}

	keyOrder := m.desc.Frontmatter.KeyOrder
	if req.AgentKey == "orchestrator" && req.Versions.OrchestratorInjectionsVersion != "" {
		set = append(set, domain.FrontmatterField{
			Key:   "orchestrator_injections_version",
			Value: domain.ScalarValue(req.Versions.OrchestratorInjectionsVersion, domain.QuotePlain),
		})
	} else {
		// For non-orchestrator agents, strip orchestrator_injections_version from the key
		// order so the field is not positioned (and thus not emitted) in subagent output.
		keyOrder = filterKeyOrder(keyOrder, "orchestrator_injections_version")
	}

	return domain.FrontmatterPlan{
		Set:      set,
		Remove:   m.desc.Frontmatter.Drop,
		KeyOrder: keyOrder,
	}, nil
}

// filterKeyOrder returns a copy of keys with the given key removed.
func filterKeyOrder(keys []string, exclude string) []string {
	filtered := make([]string, 0, len(keys))
	for _, k := range keys {
		if k != exclude {
			filtered = append(filtered, k)
		}
	}
	return filtered
}

// TargetPath returns the deployment path for one artifact.
//
// Skills receive a key-named subdirectory to prevent filename collisions (see package
// documentation). All other artifact kinds are handled by the descriptor algorithm.
func (m *module) TargetPath(req domain.TargetPathRequest) (string, error) {
	if req.Kind == domain.ArtifactSkill {
		return m.skillTargetPath(req)
	}
	return descriptor.ResolveTargetPath(m.desc, req)
}

// skillTargetPath resolves a skill deployment path with an intermediate key subdirectory:
// <skills-dir>/<key>/<filename>. This prevents filename collisions when multiple skills
// (all named SKILL.md by convention) are deployed to the same harness.
func (m *module) skillTargetPath(req domain.TargetPathRequest) (string, error) {
	if req.Scope != domain.ScopeProject {
		return "", fmt.Errorf("%w: scope %q is not supported; only %q is accepted", domain.ErrUnsupportedScope, req.Scope, domain.ScopeProject)
	}
	sp := m.desc.Paths.Skills
	if !sp.Supported {
		return "", fmt.Errorf("%w: skill", domain.ErrArtifactUnsupported)
	}
	filename := req.FileName
	if filename == "" {
		filename = req.Key
	}
	return path.Join(sp.Project, req.Key, filename), nil
}

// Injection returns the harness-level content for a canonical injection name.
//
// For the "orchestrator" agent key, shared content (from HarnessInjections.md) is merged
// with orchestrator-only content (from HarnessInjectionsOrchestrator.md):
//   - Both non-empty: returns sharedContent + "\n\n" + orchContent, ok=true
//   - Only shared non-empty: returns sharedContent, ok=true
//   - Only orchestrator-only non-empty: returns orchContent, ok=true
//   - Both declared-but-empty: returns "", ok=true
//   - Neither declared: returns "", ok=false
//
// For all other agent keys, only shared content is returned (existing behavior).
func (m *module) Injection(req domain.InjectionRequest) (string, bool) {
	sharedContent, sharedOk := m.injections[req.Name]
	if req.AgentKey != "orchestrator" {
		return sharedContent, sharedOk
	}
	orchContent, orchOk := m.orchInjections[req.Name]
	if !sharedOk && !orchOk {
		return "", false
	}
	if sharedContent != "" && orchContent != "" {
		return sharedContent + "\n\n" + orchContent, true
	}
	if sharedContent != "" {
		return sharedContent, true
	}
	if orchContent != "" {
		return orchContent, true
	}
	// Both declared but empty.
	return "", true
}

// HookPlan resolves a hook bundle for Claude Code. Claude Code supports hooks; the plan
// depends on whether the bundle carries a variant for this harness.
func (m *module) HookPlan(req domain.HookPlanRequest) (domain.HookPlan, error) {
	if !m.desc.Hooks.Supported {
		return domain.HookPlan{
			Supported: false,
			Reason:    "harness descriptor declares hooks not supported",
		}, nil
	}
	variantKey := m.desc.Hooks.VariantKey
	if variantKey == "" {
		variantKey = m.desc.ID
	}
	targetDir := m.desc.Paths.Hooks.Project
	variant, ok := req.Bundle.Variants[variantKey]
	if !ok {
		// No variant in this bundle (e.g. empty bundle in tests).
		return domain.HookPlan{
			Supported: true,
			TargetDir: targetDir,
		}, nil
	}
	return domain.HookPlan{
		Supported:    true,
		TargetDir:    targetDir,
		Files:        variant.Files,
		Registration: variant.Registration,
	}, nil
}
