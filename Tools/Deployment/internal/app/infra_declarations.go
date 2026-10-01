package app

// infra_declarations.go holds the shared app-layer plumbing for declaring infrastructure
// agents in orchestrator-role files: the per-file content inputs and the buildContent
// options that deliver them. Each orchestrator-role file is rendered against its own
// deployed bytes, so these inputs are keyed by target path rather than being one list
// shared by every orchestrator-role item.

import (
	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/transform"
)

// infraContentInput is what the content step needs to declare infrastructure agents in one
// orchestrator-role file: the catalog-built blocks and how they merge into the deployed region.
type infraContentInput struct {
	Blocks []transform.InfrastructureBlock
	Mode   transform.InfrastructureMergeMode
}

// buildContentOptions carries the per-target-path adjustments applied to orchestrator-role
// items by buildContent. The zero value changes nothing.
type buildContentOptions struct {
	infraByPath       map[string]infraContentInput
	preserveWorkflows map[string]bool
}

// buildContentOption adjusts buildContentOptions.
type buildContentOption func(*buildContentOptions)

// withInfrastructureDeclarations makes buildContent merge the given blocks into the
// InfrastructureAgents region of the orchestrator-role file at each target path.
func withInfrastructureDeclarations(byPath map[string]infraContentInput) buildContentOption {
	return func(o *buildContentOptions) { o.infraByPath = byPath }
}

// newBuildContentOptions folds opts into a buildContentOptions.
func newBuildContentOptions(opts []buildContentOption) buildContentOptions {
	var o buildContentOptions
	for _, opt := range opts {
		opt(&o)
	}
	return o
}

// applyTo sets the per-file infrastructure and workflow fields of req for the file at
// targetPath. Paths without options leave req untouched.
func (o buildContentOptions) applyTo(targetPath string, req *transform.Request) {
	if in, ok := o.infraByPath[targetPath]; ok {
		req.InfrastructureAgents = in.Blocks
		req.InfrastructureMerge = in.Mode
	}
	if o.preserveWorkflows[targetPath] {
		req.Workflows = nil
		req.PreserveDeployedWorkflows = true
	}
}

// infraContentForPaths returns the same catalog-built blocks and merge mode for every path.
// Keys the catalog does not know are skipped.
func (s *service) infraContentForPaths(paths []string, keys []string, mode transform.InfrastructureMergeMode) map[string]infraContentInput {
	blocks := s.buildInfrastructureBlocks(keys)
	byPath := make(map[string]infraContentInput, len(paths))
	for _, p := range paths {
		byPath[p] = infraContentInput{Blocks: blocks, Mode: mode}
	}
	return byPath
}

// infraKeysOf returns the keys of agents, in order.
func infraKeysOf(agents []domain.Agent) []string {
	keys := make([]string, 0, len(agents))
	for _, a := range agents {
		keys = append(keys, a.Key)
	}
	return keys
}
