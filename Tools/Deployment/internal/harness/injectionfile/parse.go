// Package injectionfile provides ParseInjections and ParseInjectionsWithVersion, the shared
// extraction mechanisms used by all harness provision tiers (built-in, descriptor-only, and
// external) to resolve injection content from MOSAIC-formatted Markdown source files
// (HarnessInjections.md and HarnessInjectionsOrchestrator.md).
package injectionfile

import (
	"fmt"
	"strings"

	"mosaic-common/docformat"
	"mosaic-common/mosaic"
)

// ParseInjections extracts all [[INJECTION:Name]] regions from a MOSAIC-formatted
// Markdown file (such as HarnessInjections.md) and returns a map from canonical
// injection name to its content string.
//
// The function calls docformat.Parse on src, then iterates Body().Injections() to
// build the map. Each injection node's Name() becomes the map key; its Content()
// (converted to string, with leading/trailing whitespace trimmed) becomes the value.
//
// An injection region with empty content between its tags produces a map entry with
// an empty-string value — the caller can distinguish "declared but empty" from
// "not declared" (absent key).
//
// Returns an error if src cannot be parsed by docformat.Parse (malformed boundary
// tags, unclosed frontmatter, etc.). A file with no injection regions is valid and
// returns an empty non-nil map with a nil error.
func ParseInjections(src []byte) (map[string]string, error) {
	injections, _, err := ParseInjectionsWithVersion(src)
	return injections, err
}

// ParseInjectionsWithVersion is like ParseInjections but also returns the `version`
// scalar from the file's YAML frontmatter. When the frontmatter is absent or has no
// `version` scalar, version is returned as "". This is the authoritative source of the
// orchestrator_injections_version value stamped into deployed orchestrator frontmatter.
func ParseInjectionsWithVersion(src []byte) (injections map[string]string, version string, err error) {
	doc, parseErr := docformat.Parse(src)
	if parseErr != nil {
		return nil, "", fmt.Errorf("injectionfile: parse failed: %w", parseErr)
	}

	if fm := doc.Frontmatter(); fm.Present() {
		if v, ok := fm.Get("version"); ok && v.Kind == mosaic.KindScalar {
			version = v.Scalar
		}
	}

	nodes := doc.Body().Injections()
	result := make(map[string]string, len(nodes))
	for _, node := range nodes {
		result[node.Name()] = strings.TrimSpace(string(node.Content()))
	}
	return result, version, nil
}
