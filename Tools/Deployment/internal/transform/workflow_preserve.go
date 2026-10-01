package transform

import (
	"strings"

	"mosaic-common/docformat"
	"mosaic-deploy/internal/domain"
)

const workflowSectionPrefix = "Workflow:"

// preserveDeployedWorkflows lifts the deployed AvailableWorkflows region content into node.
// It reports false, leaving node untouched, when the deployed file has no non-empty region.
// Nested custom regions are left out of the lifted content; the region machinery re-emits
// them after generation. The returned IDs are the workflow names in document order.
func preserveDeployedWorkflows(node *docformat.Node, name string, class domain.InjectionClass, req Request) (RegionOutcome, []string, bool) {
	if len(req.Deployed) == 0 {
		return RegionOutcome{}, nil, false
	}
	doc, err := docformat.Parse(req.Deployed)
	if err != nil {
		return RegionOutcome{}, nil, false
	}
	region, ok := doc.Body().Deployed("AvailableWorkflows")
	if !ok || len(region.Content()) == 0 {
		return RegionOutcome{}, nil, false
	}

	preserved := stripCustomChildren(region.Content())
	node.SetContent(preserved) //nolint:errcheck // Node.SetContent always returns nil; forward-compatible error return.

	var ids []string
	for _, child := range node.Children() {
		if strings.HasPrefix(child.Name(), workflowSectionPrefix) {
			ids = append(ids, strings.TrimPrefix(child.Name(), workflowSectionPrefix))
		}
	}
	return RegionOutcome{
		Name:   name,
		Marker: node.Kind(),
		Class:  class,
		Action: RegionPreservedWorkflows,
		Bytes:  len(preserved),
	}, ids, true
}
