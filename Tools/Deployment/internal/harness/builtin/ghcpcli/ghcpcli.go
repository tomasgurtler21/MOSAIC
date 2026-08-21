// Package ghcpcli implements the GitHub Copilot CLI built-in harness module. It embeds the
// ghcp-cli.yaml descriptor and provides tool formatting as a flow-style YAML sequence with
// single-quoted items (the format GHCP CLI expects), which cannot be expressed through the
// descriptor data alone.
//
// Exceptions requiring module code beyond the descriptor (documented per I11.6):
//
//  1. Flow-style single-quoted tool list: GHCP CLI expects tools as a flow-style YAML
//     sequence with single-quoted items: ['read', 'edit', 'search']. The descriptor's
//     FieldValue schema supports KindList with style options, but does not allow specifying
//     both ListFlow style AND QuoteSingle on items simultaneously at the descriptor level
//     (the descriptor Add field is unmarshalled from YAML without per-item quote control).
//     Module code post-processes the descriptor-driven list to apply both the flow style
//     and the single-quote style on every item.
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
// GHCP CLI supports hooks. HookPlan is descriptor-driven (same pattern as claudecode.go):
// it reads hooks.supported and hooks.variant_key from the descriptor and returns variant
// files from the bundle when the "ghcp-cli" variant is present. TargetPath for ArtifactHook
// resolves via descriptor.ResolveTargetPath using the paths.hooks.project setting.
//
// Registration: init() calls registry.Register so the module is available whenever this
// package is imported.
package ghcpcli

import (
	_ "embed"
	"fmt"
	"path"
	"path/filepath"

	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/harness/descriptor"
	"mosaic-deploy/internal/harness/injectionfile"
	"mosaic-deploy/internal/harness/registry"
)

//go:embed ghcp-cli.yaml
var embeddedDescriptor []byte

func init() {
	registry.Register("ghcp-cli", func(opts registry.BuiltinOptions) (domain.HarnessModule, error) {
		return New(opts)
	})
}

// module is the GitHub Copilot CLI built-in HarnessModule implementation.
type module struct {
	ref             domain.HarnessRef
	desc            *domain.HarnessDescriptor
	injections      map[string]string // parsed from HarnessInjections.md at construction time
	orchInjections  map[string]string // parsed from HarnessInjectionsOrchestrator.md at construction time
}

// New parses the embedded ghcp-cli.yaml descriptor and reads HarnessInjections.md and
// HarnessInjectionsOrchestrator.md from the declared repository directory at construction
// time. Editing those files and rerunning the tool changes the injected content with no
// rebuild required. The descriptor YAML remains embedded.
func New(opts registry.BuiltinOptions) (domain.HarnessModule, error) {
	desc, err := descriptor.Parse(embeddedDescriptor, "builtin:ghcp-cli")
	if err != nil {
		return nil, fmt.Errorf("parse embedded ghcp-cli descriptor: %w", err)
	}
	contentDir := filepath.Join(opts.MosaicRoot, RepoContentDir)
	content, err := injectionfile.LoadDir(contentDir)
	if err != nil {
		return nil, fmt.Errorf("ghcp-cli: load harness content: %w", err)
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

// Close releases resources. GHCP CLI holds no external resources; always returns nil.
func (m *module) Close() error {
	return nil
}

// Tools maps generic tool names to GHCP CLI tools and formats the result as a flow-style
// YAML sequence with single-quoted items: ['read', 'edit', 'search'].
//
// Many-to-one aliasing is handled by descriptor.MapTools: multiple generic tools that
// share a harness tool name produce one entry in the output list.
//
// See package documentation for why module code is required for this format.
func (m *module) Tools(req domain.ToolRequest) (domain.ToolResult, error) {
	if req.Placeholder != "" {
		return m.expandPlaceholder(), nil
	}
	result, err := descriptor.MapTools(m.desc, req)
	if err != nil {
		return domain.ToolResult{}, err
	}
	return m.convertFieldsToFlowList(result), nil
}

// expandPlaceholder handles the {tool-permissions} placeholder for orchestrator agents.
// It expands the descriptor's PlaceholderExpansion (or full Universe if empty) into a
// flow-style single-quoted list.
func (m *module) expandPlaceholder() domain.ToolResult {
	expansion := m.desc.Tools.PlaceholderExpansion
	if len(expansion) == 0 {
		expansion = make([]string, len(m.desc.Tools.Universe))
		for i, t := range m.desc.Tools.Universe {
			expansion[i] = t.Name
		}
	}
	items := make([]domain.FieldValue, len(expansion))
	for i, name := range expansion {
		items[i] = domain.FieldValue{Kind: domain.KindScalar, Scalar: name, Quote: domain.QuoteSingle}
	}
	return domain.ToolResult{
		Fields: []domain.FrontmatterField{{
			Key: m.desc.Frontmatter.ToolsKey,
			Value: domain.FieldValue{
				Kind:  domain.KindList,
				List:  domain.ListFlow,
				Items: items,
			},
		}},
		Resolutions: []domain.ToolResolution{},
	}
}

// convertFieldsToFlowList converts the KindList tools field returned by descriptor.MapTools
// into a flow-style list with single-quoted scalar items.
func (m *module) convertFieldsToFlowList(result domain.ToolResult) domain.ToolResult {
	if len(result.Fields) == 0 {
		return result
	}
	toolsKey := m.desc.Frontmatter.ToolsKey
	newFields := make([]domain.FrontmatterField, len(result.Fields))
	for i, f := range result.Fields {
		if f.Key == toolsKey && f.Value.Kind == domain.KindList {
			items := make([]domain.FieldValue, len(f.Value.Items))
			for j, item := range f.Value.Items {
				items[j] = domain.FieldValue{
					Kind:  domain.KindScalar,
					Scalar: item.Scalar,
					Quote: domain.QuoteSingle,
				}
			}
			newFields[i] = domain.FrontmatterField{
				Key: f.Key,
				Value: domain.FieldValue{
					Kind:  domain.KindList,
					List:  domain.ListFlow,
					Items: items,
				},
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

// Frontmatter builds the FrontmatterPlan for GHCP CLI agents. It returns the
// descriptor's static Add fields, resolved role-conditional fields, remove list,
// and key order. Model and version stamps are applied exclusively by the transform
// pipeline and must not appear here; including them would produce duplicate FieldChange
// entries in transform.Report.Fields.
//
// The orchestrator_injections_version field has been relocated from frontmatter to the
// version attribute on InjectionHarness-class region tags (written by applyHarnessRegion).
// This method no longer stamps it as a frontmatter field for any agent.
func (m *module) Frontmatter(req domain.FrontmatterRequest) (domain.FrontmatterPlan, error) {
	// Copy static add fields from descriptor.
	resolved := descriptor.ResolveRoleConditionalFields(m.desc.Frontmatter.RoleConditionalAdd, req.Role)
	set := make([]domain.FrontmatterField, 0, len(m.desc.Frontmatter.Add)+len(resolved))
	set = append(set, m.desc.Frontmatter.Add...)

	// Append resolved role-conditional fields (e.g. user-invocable for the agent's role).
	set = append(set, resolved...)

	return domain.FrontmatterPlan{
		Set:      set,
		Remove:   m.desc.Frontmatter.Drop,
		KeyOrder: m.desc.Frontmatter.KeyOrder,
	}, nil
}

// TargetPath returns the deployment path for one artifact.
//
// Skills receive a key-named subdirectory to prevent filename collisions (see package
// documentation). All other kinds, including hooks, are handled by the descriptor algorithm.
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
// with orchestrator-only content (from HarnessInjectionsOrchestrator.md). For all other
// agent keys, only shared content is returned.
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
	return "", true
}

// HookPlan resolves a hook bundle for GHCP CLI. GHCP CLI supports hooks via the descriptor;
// the plan depends on whether the bundle carries a "ghcp-cli" variant.
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
