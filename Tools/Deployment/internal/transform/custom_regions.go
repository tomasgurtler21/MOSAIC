package transform

import (
	"fmt"
	"sort"

	"mosaic-common/docformat"
	"mosaic-deploy/internal/domain"
)

// nestedRegionRecord is one user-owned region that must survive regeneration of its
// enclosing managed region parent.
//
// The record deliberately holds no content — content is resolved from the deployed region
// map at re-emission time, so there is one source of truth for content and rename resolution
// applies uniformly.
type nestedRegionRecord struct {
	Kind  docformat.NodeKind // NodeInjection (source-declared) or NodeCustom (deployed-only)
	Name  string
	Order int // emission order; deployed-file-origin regions first, then source-only regions
}

// customRegionRecord is a custom region found in the deployed file along with the
// anchor pair that determines where it goes in the output document.
//
// The anchor pair (ParentName + PrevSibling) matches the placement rule in ContractsDesign.md
// AD-1: position is expressed as the name of the enclosing parent and the name of the nearest
// preceding sibling within that parent. Both are recomputed from the deployed file on every
// run (the tool is stateless), and placement is attempted in the output document by name.
type customRegionRecord struct {
	Name        string             // the tag name; the region map key
	Content     []byte             // inner bytes, byte-identical to the deployed file
	ParentKind  docformat.NodeKind // NodeSection, NodeDeployed, or "" when top level
	ParentName  string             // name of nearest enclosing SECTION or DEPLOYED ancestor; "" when top level
	PrevSibling string             // name of the nearest preceding sibling node; "" when first in parent
	Order       int                // index in deployed-document order, for deterministic ordering
}

// collectDeployedCustomRegions walks the deployed document and returns one record for every
// custom region in deployed-document order. Each record carries the full anchor pair
// (ParentName + PrevSibling) that determines where the region is placed in the output.
//
// The anchor pair is derived from the deployed file only and is recomputed on every run;
// the tool persists no placement state.
func collectDeployedCustomRegions(depDoc *docformat.Document) []customRegionRecord {
	body := depDoc.Body()
	customs := body.CustomRegions() // all custom regions at any depth, document order

	records := make([]customRegionRecord, 0, len(customs))
	for i, node := range customs {
		var parentKind docformat.NodeKind
		var parentName string
		var prevSibling string

		if parent := node.Parent(); parent != nil {
			parentKind = parent.Kind()
			parentName = parent.Name()
			// Find the preceding sibling node within the parent's children.
			siblings := parent.Children()
			for j, sib := range siblings {
				if sib == node && j > 0 {
					prevSibling = siblings[j-1].Name()
					break
				}
			}
		} else {
			// Top-level node: find the nearest preceding top-level sibling.
			topLevel := body.TopLevelNodes()
			for j, n := range topLevel {
				if n == node && j > 0 {
					prevSibling = topLevel[j-1].Name()
					break
				}
			}
		}

		records = append(records, customRegionRecord{
			Name:        node.Name(),
			Content:     node.Content(),
			ParentKind:  parentKind,
			ParentName:  parentName,
			PrevSibling: prevSibling,
			Order:       i,
		})
	}
	return records
}

// placeCustomRegion inserts rec into the output document using the AD-1 anchor rule:
//
//  1. Resolve the parent. If rec.ParentName is non-empty, find the named section or
//     managed region node in the output body (by name, any depth). If rec.ParentName is empty,
//     the parent is the body.
//  2. Within the resolved parent, find a node named rec.PrevSibling. If found, insert the
//     custom region immediately after it via Body.InsertRegionAfter. If not found (or
//     rec.PrevSibling is empty), append at the parent's end.
//  3. When the parent named by rec.ParentName cannot be resolved in the output (e.g. the
//     source removed the parent section), fall through to a body-level placement so content
//     is never silently lost.
//
// Ordering contract: append-only was the previous behaviour and caused silent mis-positioning
// when other content followed the custom region inside its parent. InsertRegionAfter is the
// correct primitive and must be used whenever the preceding sibling is present and resolved.
func placeCustomRegion(body *docformat.Body, rec customRegionRecord, content []byte) (*docformat.Node, error) {
	if rec.ParentName != "" {
		// Resolve the parent as a section (any depth).
		if section, ok := body.SectionDeep(rec.ParentName); ok {
			return insertOrAppendInNode(body, section, rec, content)
		}
		// Resolve the parent as a deployed region (any depth).
		if deployed, ok := body.Deployed(rec.ParentName); ok {
			return insertOrAppendInNode(body, deployed, rec, content)
		}
		// Parent not found in the output — fall through to body-level placement so the
		// custom region content is never dropped. This covers the case where the source
		// removed the parent section (a structural change, not a normal update).
	}

	// Body-level placement: after the preceding top-level sibling, or append at the end.
	if rec.PrevSibling != "" {
		if sib := findTopLevelNodeByName(body, rec.PrevSibling); sib != nil {
			return body.InsertRegionAfter(sib, docformat.NodeCustom, rec.Name, content)
		}
	}
	return body.AppendRegion(docformat.NodeCustom, rec.Name, content)
}

// insertOrAppendInNode places the custom region described by rec inside parentNode.
// If rec.PrevSibling names a direct child of parentNode, the region is inserted immediately
// after that child. Otherwise the region is appended at the end of the parent's content.
func insertOrAppendInNode(body *docformat.Body, parentNode *docformat.Node, rec customRegionRecord, content []byte) (*docformat.Node, error) {
	if rec.PrevSibling != "" {
		if sib := findChildByName(parentNode, rec.PrevSibling); sib != nil {
			return body.InsertRegionAfter(sib, docformat.NodeCustom, rec.Name, content)
		}
	}
	return parentNode.AppendRegion(docformat.NodeCustom, rec.Name, content)
}

// findChildByName returns the first direct child node of n whose name matches, or nil.
func findChildByName(n *docformat.Node, name string) *docformat.Node {
	for _, child := range n.Children() {
		if child.Name() == name {
			return child
		}
	}
	return nil
}

// findTopLevelNodeByName returns the first top-level node in the body with the given name,
// or nil when no top-level node has that name.
func findTopLevelNodeByName(body *docformat.Body, name string) *docformat.Node {
	for _, n := range body.TopLevelNodes() {
		if n.Name() == name {
			return n
		}
	}
	return nil
}

// sourceAnchorNames returns the set of names that can anchor a custom region in the source
// document: every section name and every managed region name, at any nesting
// depth. Matching is by name only; position is never consulted.
//
// A custom region is considered anchored when its ParentName appears in this set. An empty
// ParentName (top-level custom region) is never anchored by definition.
func sourceAnchorNames(sourceBody *docformat.Body) map[string]bool {
	names := make(map[string]bool)
	for _, node := range sourceBody.SectionsDeep() {
		names[node.Name()] = true
	}
	for _, node := range sourceBody.DeployedRegions() {
		names[node.Name()] = true
	}
	return names
}

// parkCustomRegions appends the given custom region records at the end of the body as
// custom regions, content byte-identical, names in ascending sorted order. It returns
// the sorted parked names and one RegionOutcome per parked region with Action RegionParked.
//
// Records are sorted by Name before appending so the output document is deterministic
// regardless of the order in which records arrive.
func parkCustomRegions(body *docformat.Body, records []customRegionRecord) ([]string, []RegionOutcome, error) {
	// Sort by name for deterministic output.
	sort.Slice(records, func(i, j int) bool {
		return records[i].Name < records[j].Name
	})

	names := make([]string, 0, len(records))
	outcomes := make([]RegionOutcome, 0, len(records))
	for _, rec := range records {
		if _, err := body.AppendRegion(docformat.NodeCustom, rec.Name, rec.Content); err != nil {
			return nil, nil, fmt.Errorf("park custom region %q: %w", rec.Name, err)
		}
		names = append(names, rec.Name)
		outcomes = append(outcomes, RegionOutcome{
			Name:   rec.Name,
			Marker: docformat.NodeCustom,
			Class:  domain.InjectionProject,
			Action: RegionParked,
			Bytes:  len(rec.Content),
		})
	}
	return names, outcomes, nil
}
