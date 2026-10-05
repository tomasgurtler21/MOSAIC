package transform

import (
	"bytes"
	"strings"

	"mosaic-common/docformat"
	"mosaic-deploy/internal/domain"
)

// InfrastructureMergeMode selects how infrastructure blocks are merged into a deployed region.
type InfrastructureMergeMode string

const (
	InfrastructureMergeNone    InfrastructureMergeMode = ""        // existing replace/preserve/clear
	InfrastructureMergeEnsure  InfrastructureMergeMode = "ensure"  // replace declared in place, append undeclared
	InfrastructureMergeRefresh InfrastructureMergeMode = "refresh" // replace declared in place, never append
)

// InfrastructureMergeResult reports what a merge did, by key.
type InfrastructureMergeResult struct {
	Added     []string // appended keys, in block order
	Replaced  []string // declared keys whose section bytes changed, in block order
	Unchanged []string // declared keys whose replacement was byte-identical
	Ignored   []string // refresh-mode undeclared keys and duplicate input keys
}

// MergeInfrastructureDeclarations splices blocks into regionContent (the inner bytes of a
// deployed <InfrastructureAgents> region) and returns the new inner bytes.
//
// A block whose key is already declared replaces the first section for that key in place; a
// block whose key is not declared is appended after the existing content (ensure mode) or
// ignored (refresh mode). Every other byte, including hand-added sections, prose and nested
// custom regions, is left as it was. A repeated key in blocks uses its first occurrence.
// Mode InfrastructureMergeNone or an empty block list returns regionContent unchanged.
func MergeInfrastructureDeclarations(regionContent []byte, blocks []InfrastructureBlock, mode InfrastructureMergeMode) (merged []byte, result InfrastructureMergeResult) {
	if mode == InfrastructureMergeNone || len(blocks) == 0 {
		return regionContent, InfrastructureMergeResult{}
	}
	children, padded, ok := scanRegionChildren(regionContent)
	if !ok {
		// A region that cannot be read reliably is left alone rather than risk damaging it.
		for _, b := range blocks {
			result.Ignored = append(result.Ignored, b.Key)
		}
		return regionContent, result
	}

	eol := DetectLineEnding(regionContent)
	declared := firstDeclaredSections(children)
	replacements := make(map[int][]byte) // child index -> new section bytes
	var appended bytes.Buffer
	seen := make(map[string]bool, len(blocks))

	for _, block := range blocks {
		if seen[block.Key] {
			result.Ignored = append(result.Ignored, block.Key)
			continue
		}
		seen[block.Key] = true
		rendered := renderInfrastructureSection(block, eol)

		idx, isDeclared := declared[block.Key]
		switch {
		case isDeclared && bytes.Equal(padded[children[idx].start:children[idx].end], rendered):
			result.Unchanged = append(result.Unchanged, block.Key)
		case isDeclared:
			replacements[idx] = rendered
			result.Replaced = append(result.Replaced, block.Key)
		case mode == InfrastructureMergeEnsure:
			appended.Write(rendered)
			result.Added = append(result.Added, block.Key)
		default:
			result.Ignored = append(result.Ignored, block.Key)
		}
	}

	if len(replacements) == 0 && appended.Len() == 0 {
		return regionContent, result
	}

	var out bytes.Buffer
	cursor := 0
	for i, child := range children {
		replacement, replace := replacements[i]
		if !replace {
			continue
		}
		out.Write(padded[cursor:child.start])
		out.Write(replacement)
		cursor = child.end
	}
	out.Write(padded[cursor:])
	out.Write(appended.Bytes())
	return out.Bytes(), result
}

// firstDeclaredSections maps each declared key to the index of its first section among children.
func firstDeclaredSections(children []regionChild) map[string]int {
	declared := make(map[string]int)
	for i, child := range children {
		node := child.node
		if node.Kind() != docformat.NodeSection || !strings.HasPrefix(node.Name(), infrastructureSectionPrefix) {
			continue
		}
		key := strings.TrimPrefix(node.Name(), infrastructureSectionPrefix)
		if _, dup := declared[key]; !dup {
			declared[key] = i
		}
	}
	return declared
}

// renderInfrastructureSection returns the section bytes for block using the given line terminator.
func renderInfrastructureSection(block InfrastructureBlock, eol string) []byte {
	var buf bytes.Buffer
	writeInfrastructureBlock(&buf, block)
	if eol == "\n" {
		return buf.Bytes()
	}
	return bytes.ReplaceAll(buf.Bytes(), []byte("\n"), []byte(eol))
}

// planInfrastructureMerge computes the merged inner bytes of the InfrastructureAgents region
// for an opt-in merge request. Nested custom regions of the deployed region are left out of
// the merged bytes because the region machinery re-emits them after generation.
func planInfrastructureMerge(req Request) (merged []byte, result InfrastructureMergeResult) {
	var deployedContent []byte
	if len(req.Deployed) > 0 {
		if doc, err := docformat.Parse(req.Deployed); err == nil {
			if region, ok := doc.Body().Deployed("InfrastructureAgents"); ok {
				deployedContent = stripCustomChildren(region.Content())
			}
		}
	}
	merged, result = MergeInfrastructureDeclarations(deployedContent, req.InfrastructureAgents, req.InfrastructureMerge)
	return ConvertLineEndings(merged, sourceLineEnding(req)), result
}

// applyInfrastructureMerge writes the merged region content. An empty outcome is reported as
// the region being left empty; otherwise the keys returned are every declaration now present.
func applyInfrastructureMerge(node *docformat.Node, name string, class domain.InjectionClass, req Request) (RegionOutcome, []string) {
	merged, _ := planInfrastructureMerge(req)
	if len(merged) == 0 {
		node.Clear() //nolint:errcheck // Node.Clear always returns nil; forward-compatible error return.
		return RegionOutcome{Name: name, Marker: node.Kind(), Class: class, Action: RegionEmptied}, nil
	}
	node.SetContent(merged) //nolint:errcheck // Node.SetContent always returns nil; forward-compatible error return.

	var keys []string
	for _, decl := range declarationsFromNodes(node.Children()) {
		keys = append(keys, decl.Key)
	}
	return RegionOutcome{
		Name:   name,
		Marker: node.Kind(),
		Class:  class,
		Action: RegionMergedInfra,
		Bytes:  len(merged),
	}, keys
}

// infrastructureMergeReport returns the merge result for the report when the
// InfrastructureAgents region was written by a merge, and the zero result otherwise.
func infrastructureMergeReport(req Request, outcomes []RegionOutcome) InfrastructureMergeResult {
	for _, o := range outcomes {
		if o.Action == RegionMergedInfra {
			_, result := planInfrastructureMerge(req)
			return result
		}
	}
	return InfrastructureMergeResult{}
}
