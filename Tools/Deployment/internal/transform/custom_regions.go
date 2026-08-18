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

// customRegionRecord is a custom region found in the deployed file along with
// the parent identity that determines where it goes in the output document.
//
// Placement rule: the custom region is always appended at the end of the
// resolved parent's content. No sibling-relative positioning is attempted.
type customRegionRecord struct {
	Name       string             // the tag name; the region map key
	Content    []byte             // inner bytes, byte-identical to the deployed file
	ParentKind docformat.NodeKind // NodeSection, NodeDeployed, or "" when top level
	ParentName string             // name of nearest enclosing SECTION or DEPLOYED ancestor; "" when top level
	Order      int                // index in deployed-document order, for deterministic ordering
}

// collectDeployedCustomRegions walks the deployed document and returns one
// record for every custom region in deployed-document order. Each record
// carries the parent identity (ParentKind + ParentName) that determines where
// the region is placed in the output.
//
// No sibling information is collected; placement is always at the end of the
// resolved parent.
func collectDeployedCustomRegions(depDoc *docformat.Document) []customRegionRecord {
	body := depDoc.Body()
	customs := body.CustomRegions() // all custom regions at any depth, document order

	records := make([]customRegionRecord, 0, len(customs))
	for i, node := range customs {
		var parentKind docformat.NodeKind
		var parentName string

		if parent := node.Parent(); parent != nil {
			parentKind = parent.Kind()
			parentName = parent.Name()
		}

		records = append(records, customRegionRecord{
			Name:       node.Name(),
			Content:    node.Content(),
			ParentKind: parentKind,
			ParentName: parentName,
			Order:      i,
		})
	}
	return records
}

// placeCustomRegion appends rec into the output document at the end of its
// resolved parent's content:
//
//  1. If rec.ParentName is non-empty, resolve as a section (Body.SectionDeep)
//     then as a managed region (Body.Deployed). If found, append the custom
//     region at the end of the resolved parent via parentNode.AppendRegion.
//  2. If rec.ParentName is empty or cannot be resolved, append at body level
//     via body.AppendRegion. This fallthrough ensures content is never lost.
//
// No sibling-relative positioning is attempted. The custom region always lands
// at the end of the parent's content, regardless of its original position in
// the deployed file.
func placeCustomRegion(body *docformat.Body, rec customRegionRecord, content []byte) (*docformat.Node, error) {
	if rec.ParentName != "" {
		// Resolve the parent as a section (any depth).
		if section, ok := body.SectionDeep(rec.ParentName); ok {
			return section.AppendRegion(docformat.NodeCustom, rec.Name, content)
		}
		// Resolve the parent as a deployed region (any depth).
		if deployed, ok := body.Deployed(rec.ParentName); ok {
			return deployed.AppendRegion(docformat.NodeCustom, rec.Name, content)
		}
		// Parent not found in the output — fall through to body-level placement so the
		// custom region content is never dropped. This covers the case where the source
		// removed the parent section (a structural change, not a normal update).
	}

	// Body-level placement: append at body end.
	return body.AppendRegion(docformat.NodeCustom, rec.Name, content)
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
