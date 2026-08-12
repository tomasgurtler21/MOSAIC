package docformat

// StructureNames returns the document's ordered structural slot names: top-level
// [[SECTION:]] names and top-level [[DEPLOYED:]] region names, interleaved in document
// order. Nested sections and nested deployed regions are not included. Top-level
// [[INJECTION:]] and [[CUSTOM:]] regions are not structural slots and are excluded.
//
// The decision to include top-level [[DEPLOYED:]] regions is documented in AD-2 of the
// design: a deployed region is a valid anchor for custom regions, and the existing
// validator's canonical-order check already treats it as an ordered slot alongside
// sections. Only top-level deployed regions participate; nested ones are content of their
// enclosing section.
func StructureNames(b *Body) []string {
	b.ensureParsed()
	var names []string
	for _, item := range b.items {
		if n, ok := item.(*Node); ok {
			if n.kind == NodeSection || n.kind == NodeDeployed {
				names = append(names, n.name)
			}
		}
	}
	return names
}

// OrderDiffers reports whether the relative order of the names common to both lists
// differs between them. Names present in only one list are additions or removals and
// never, by themselves, produce a true result. A duplicate name in either list is
// compared at its first occurrence; later occurrences are ignored.
//
// Both lists empty, either list empty, or a single common name: always false.
func OrderDiffers(a, b []string) bool {
	// Build position map for a: first occurrence of each name.
	aIdx := make(map[string]int, len(a))
	for i, name := range a {
		if _, exists := aIdx[name]; !exists {
			aIdx[name] = i
		}
	}

	// Build position map for b: first occurrence of each name.
	bIdx := make(map[string]int, len(b))
	for i, name := range b {
		if _, exists := bIdx[name]; !exists {
			bIdx[name] = i
		}
	}

	// Collect common elements as (aPosition, bPosition) pairs, in a-order.
	// Iterating a in order gives us pairs sorted by a-position; we then
	// check whether the b-positions are also in strictly ascending order.
	type pair struct{ bPos int }
	var pairs []pair
	seen := make(map[string]bool, len(a))
	for _, name := range a {
		if seen[name] {
			// Skip duplicate names: first occurrence was already recorded.
			continue
		}
		seen[name] = true
		if bPos, inB := bIdx[name]; inB {
			pairs = append(pairs, pair{bPos})
		}
	}

	// If fewer than two common elements, relative order cannot differ.
	if len(pairs) < 2 {
		return false
	}

	// Verify that b-positions are strictly ascending (same relative order as in a).
	for i := 1; i < len(pairs); i++ {
		if pairs[i].bPos < pairs[i-1].bPos {
			return true
		}
	}
	return false
}

// ReorderDetected reports whether the source document's structural slot order differs
// from the deployed document's structural slot order. It is a convenience wrapper over
// StructureNames and OrderDiffers.
//
// A pure addition or removal of a section in the source does not by itself trigger a
// reorder: only the relative order of the sections common to both documents matters.
func ReorderDetected(deployed, source *Document) bool {
	deployedNames := StructureNames(deployed.Body())
	sourceNames := StructureNames(source.Body())
	return OrderDiffers(deployedNames, sourceNames)
}
