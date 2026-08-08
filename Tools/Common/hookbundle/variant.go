package hookbundle

import "fmt"

// ResolveVariant answers "which variant does harness X consume".
//
// Resolution defaults to the harness ID and honours an explicit override.
// A variant whose Reuses field is set resolves to the reused variant,
// transitively; a cycle is refused.
//
// "This bundle does not support that harness" and "no such variant key" are
// distinct outcomes, because they call for different messages to the user.
func (m Manifest) ResolveVariant(harnessID string, variantOverride string) (Variant, error) {
	key := harnessID
	if variantOverride != "" {
		key = variantOverride
	}

	visited := make(map[string]bool)
	for {
		if visited[key] {
			return Variant{}, fmt.Errorf("%w: %s", ErrReuseCycle, key)
		}
		visited[key] = true

		v, ok := m.Variants[key]
		if !ok {
			return Variant{}, fmt.Errorf("%w: %q", ErrVariantMissing, key)
		}
		if !v.Supported {
			return Variant{}, fmt.Errorf("%w: %q", ErrVariantUnsupported, key)
		}
		if v.Reuses == "" {
			return v, nil
		}
		key = v.Reuses
	}
}
