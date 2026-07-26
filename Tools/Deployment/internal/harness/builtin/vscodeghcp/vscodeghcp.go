// Package vscodeghcp implements the VS Code GitHub Copilot built-in harness module. It embeds
// the vscode-ghcp.yaml descriptor and provides tool formatting as a flow-style YAML sequence
// with single-quoted hierarchical path items (e.g. ['read/readFile', 'edit/editFiles']).
//
// Exceptions requiring module code beyond the descriptor:
//
//  1. Flow-style single-quoted tool list: VS Code GHCP expects tools as a flow-style YAML
//     sequence with single-quoted items: ['read/readFile', 'edit/editFiles', 'search/listDirectory'].
//     The descriptor's FieldValue schema supports KindList with style options, but does not
//     allow specifying both ListFlow style AND QuoteSingle on items simultaneously at the
//     descriptor level. Module code post-processes the descriptor-driven list to apply both
//     the flow style and the single-quote style on every item.
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
// File extension decision (recorded here per AC13.6):
// The descriptor declares ".md" as the agent extension, matching the CodebaseAgnostic
// canonical reference set (Agents/VS code GHCP/CodebaseAgnostic/Agents/*.md). Files under
// Agents/VS code GHCP/UtilityAgents/ use ".agent.md" but were authored outside the deployment
// tool and represent a deviation from the canonical output. VS Code GHCP recognises both
// ".md" and ".agent.md" files in .github/agents/; the deployment tool emits ".md".
//
// Hook variant reuse: the vscode-ghcp hook variant in hook bundles declares reuses: claude-code.
// The catalog resolves reuse references before calling HookPlan, so the HookPlanRequest already
// contains the resolved file list. This module reads from the "vscode-ghcp" variant key without
// any special reuse logic — reuse is handled upstream by the catalog.
//
// User-level editor setting: enabling hooks in VS Code requires setting "chat.hooks.enabled": true
// in User Settings. This is a user-level VS Code setting that the deployment tool cannot write.
// The registration step with ID "enable-chat-hooks" always has Performable: false and is always
// reported as a TODO item, never attempted. This is not special-cased in module code; it is
// a property of the registration step data provided in the hook bundle variant.
//
// Registration: init() calls registry.Register so the module is available whenever this
// package is imported.
package vscodeghcp

import (
	_ "embed"
	"fmt"
	"path"

	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/harness/descriptor"
	"mosaic-deploy/internal/harness/registry"
)

//go:embed vscode-ghcp.yaml
var embeddedDescriptor []byte

func init() {
	registry.Register("vscode-ghcp", func() (domain.HarnessModule, error) {
		return New()
	})
}

// module is the VS Code GitHub Copilot built-in HarnessModule implementation.
type module struct {
	ref  domain.HarnessRef
	desc *domain.HarnessDescriptor
}

// New parses the embedded vscode-ghcp.yaml descriptor and returns the VS Code GHCP HarnessModule.
// The module formats tools as a flow-style single-quoted YAML sequence with hierarchical paths.
func New() (domain.HarnessModule, error) {
	desc, err := descriptor.Parse(embeddedDescriptor, "builtin:vscode-ghcp")
	if err != nil {
		return nil, fmt.Errorf("parse embedded vscode-ghcp descriptor: %w", err)
	}
	ref := domain.HarnessRef{
		ID:          desc.ID,
		DisplayName: desc.DisplayName,
		Tier:        domain.TierBuiltin,
		Usable:      true,
	}
	return &module{ref: ref, desc: desc}, nil
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

// Close releases resources. VS Code GHCP holds no external resources; always returns nil.
func (m *module) Close() error {
	return nil
}

// Tools maps generic tool names to VS Code GHCP tools and formats the result as a flow-style
// YAML sequence with single-quoted hierarchical path items:
// ['read/readFile', 'edit/editFiles', 'search/listDirectory'].
//
// One-to-many expansion (file_write → edit/createFile + edit/editFiles) and many-to-one
// deduplication (file_write + file_edit both map to edit/editFiles, deduplicated by
// descriptor.MapTools to appear once) are handled by descriptor.MapTools.
//
// search/listDirectory is emitted by convention (ByConvention: true in the descriptor's
// universe) for every agent regardless of which generic tools are declared.
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
// flow-style single-quoted list. This includes edit/createDirectory and search/listDirectory
// which appear in the full orchestrator tool set.
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
		Resolutions: nil,
	}
}

// convertFieldsToFlowList converts the KindList tools field returned by descriptor.MapTools
// into a flow-style list with single-quoted scalar items, producing the VS Code GHCP
// canonical format: ['read/readFile', 'edit/editFiles', 'search/listDirectory'].
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
					Kind:   domain.KindScalar,
					Scalar: item.Scalar,
					Quote:  domain.QuoteSingle,
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

// Frontmatter builds the FrontmatterPlan for VS Code GHCP agents. It returns the
// descriptor's static Add fields (containing disable-model-invocation: false), remove list,
// and key order. Model and version stamps are applied exclusively by the transform pipeline
// and must not appear here; including them would produce duplicate FieldChange entries in
// transform.Report.Fields.
func (m *module) Frontmatter(req domain.FrontmatterRequest) (domain.FrontmatterPlan, error) {
	return domain.FrontmatterPlan{
		Set:      m.desc.Frontmatter.Add,
		Remove:   m.desc.Frontmatter.Drop,
		KeyOrder: m.desc.Frontmatter.KeyOrder,
	}, nil
}

// TargetPath returns the deployment path for one artifact.
//
// Skills receive a key-named subdirectory to prevent filename collisions (all skill entry
// files are named SKILL.md by convention). All other artifact kinds are handled by the
// descriptor algorithm.
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

// Injection returns the harness-level content for a canonical injection name as declared
// in the embedded descriptor's injections list. VS Code GHCP declares HarnessConstraints
// (file-reading constraint), CustomConstraints (parallel tool calls), and LanguagePatterns
// (empty) as harness-level injections.
func (m *module) Injection(name string) (string, bool) {
	for _, inj := range m.desc.Injections {
		if inj.Name == name {
			return inj.Content, true
		}
	}
	return "", false
}

// HookPlan resolves a hook bundle for VS Code GHCP. The vscode-ghcp hook variant reuses
// the claude-code file set; the catalog resolves this reuse before calling HookPlan by
// populating the variant's Files from the claude-code variant. This module reads from the
// "vscode-ghcp" variant key; no reuse logic is required here.
//
// The hook target directory is .claude/hooks/ — the same location as Claude Code hooks,
// allowing a single deployment to serve both platforms.
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
		// No variant for this harness in the bundle (e.g. bundle not yet configured).
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
