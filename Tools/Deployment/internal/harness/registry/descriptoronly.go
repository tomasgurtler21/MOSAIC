package registry

import (
	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/harness/descriptor"
)

// runtimeModule implements domain.HarnessModule for harnesses provided by an on-disk
// harness.yaml folder. The module's entire behaviour is driven by the descriptor through
// shared algorithms in harness/descriptor (CD-2):
//   - descriptor.MapTools handles tool mapping
//   - descriptor.ApplyFrontmatterSpec handles frontmatter shaping
//   - descriptor.ResolveTargetPath handles deployment paths
//
// This is the zero-code runtime harness tier (AC6.2): placing a folder with a valid
// harness.yaml under MosaicDeploy/harnesses/ makes it immediately available with no rebuild.
//
// The same struct is used as a placeholder for the external tier (TierExternal) until
// Stage 23 adds the JSON-over-stdio protocol. At that point, the external tier gains its
// own implementation that forwards calls across the process boundary.
type runtimeModule struct {
	ref  domain.HarnessRef
	desc *domain.HarnessDescriptor
}

// newRuntimeModule constructs a descriptor-driven module for the given ref and descriptor.
// Both the descriptor-only and (for now) external tiers use this implementation.
func newRuntimeModule(ref domain.HarnessRef, desc *domain.HarnessDescriptor) *runtimeModule {
	return &runtimeModule{ref: ref, desc: desc}
}

// Ref returns the identity and provenance of this harness. The value is stable; calling Ref
// twice returns equal values.
func (m *runtimeModule) Ref() domain.HarnessRef {
	return m.ref
}

// Descriptor returns the parsed descriptor. The same pointer is returned on every call;
// callers must treat it as read-only.
func (m *runtimeModule) Descriptor() *domain.HarnessDescriptor {
	return m.desc
}

// Close releases resources held by this module. The descriptor-only tier holds no resources
// (no child processes, no open files), so this always returns nil.
func (m *runtimeModule) Close() error {
	return nil
}

// Tools maps an agent's generic tool list to harness-specific fields using the shared
// descriptor-driven algorithm. Every entry in req.Generic appears exactly once in the
// returned Resolutions, in the same order.
func (m *runtimeModule) Tools(req domain.ToolRequest) (domain.ToolResult, error) {
	return descriptor.MapTools(m.desc, req)
}

// Frontmatter returns the ordered field operations the harness applies to a generic agent's
// frontmatter. The plan is derived from the descriptor's frontmatter.add / frontmatter.drop /
// frontmatter.key_order declarations.
func (m *runtimeModule) Frontmatter(req domain.FrontmatterRequest) (domain.FrontmatterPlan, error) {
	return descriptor.ApplyFrontmatterSpec(m.desc, req)
}

// TargetPath returns the deployment path for one artifact, delegating to the shared
// descriptor path-resolution algorithm. Returns domain.ErrArtifactUnsupported when the
// descriptor declares no path for the requested artifact kind.
func (m *runtimeModule) TargetPath(req domain.TargetPathRequest) (string, error) {
	return descriptor.ResolveTargetPath(m.desc, req)
}

// Injection returns the harness-level content for a canonical injection name, as declared in
// the descriptor's injections list. ok is false when the name is not in that list.
func (m *runtimeModule) Injection(name string) (string, bool) {
	for _, inj := range m.desc.Injections {
		if inj.Name == name {
			return inj.Content, true
		}
	}
	return "", false
}

// HookPlan returns a plan for deploying one hook bundle with this harness. When the
// descriptor declares hooks.supported: false, the returned plan carries Supported: false
// and a non-empty Reason. Full hook variant file resolution for the descriptor-only tier
// is out of Stage 6's scope.
func (m *runtimeModule) HookPlan(req domain.HookPlanRequest) (domain.HookPlan, error) {
	if !m.desc.Hooks.Supported {
		return domain.HookPlan{
			Supported: false,
			Reason:    "harness descriptor declares hooks not supported",
		}, nil
	}
	// Descriptor-only harnesses do not ship hook variant files. Hook deployment for
	// runtime-provisioned harnesses is out of scope for this stage.
	return domain.HookPlan{
		Supported: false,
		Reason:    "hook variant files are not available for runtime-provisioned harnesses",
	}, nil
}
