package resultsummary

import (
	"sort"
	"strings"
)

// parseOpenMarker tries to interpret a trimmed line as an opening marker.
// It returns the region type, block name, and whether the line was a marker.
// The line must already have leading/trailing whitespace stripped.
func parseOpenMarker(line string) (RegionType, string, bool) {
	const genPrefix = "<!-- generated:"
	const anaPrefix = "<!-- analysis:"
	const suffix = " -->"

	if strings.HasPrefix(line, genPrefix) && strings.HasSuffix(line, suffix) {
		inner := line[len(genPrefix) : len(line)-len(suffix)]
		// Reject closing markers (start with "/") and names containing spaces.
		if inner != "" && !strings.HasPrefix(inner, "/") && !strings.ContainsAny(inner, " \t") {
			return RegionGenerated, inner, true
		}
	}
	if strings.HasPrefix(line, anaPrefix) && strings.HasSuffix(line, suffix) {
		inner := line[len(anaPrefix) : len(line)-len(suffix)]
		if inner != "" && !strings.HasPrefix(inner, "/") && !strings.ContainsAny(inner, " \t") {
			return RegionAnalysis, inner, true
		}
	}
	return "", "", false
}

// isCloseMarker returns true when trimmed line is the closing marker for the
// given block type and name.
func isCloseMarker(line string, blockType RegionType, blockName string) bool {
	switch blockType {
	case RegionGenerated:
		return line == "<!-- /generated:"+blockName+" -->"
	case RegionAnalysis:
		return line == "<!-- /analysis:"+blockName+" -->"
	}
	return false
}

// ParseMarkedDocument parses a Markdown document containing
// <!-- generated:name --> and <!-- analysis:name --> marker pairs.
// Returns a list of regions (plain text, generated blocks, analysis blocks)
// that the merge logic uses to selectively regenerate generated content while
// preserving analysis content.
//
// Parsing rules:
//   - Markers must appear on their own line (leading/trailing whitespace ok).
//   - No nesting of markers.
//   - An unclosed marker extends to EOF.
//   - Unknown marker types are treated as plain text.
func ParseMarkedDocument(content string) []DocumentRegion {
	if content == "" {
		return nil
	}

	lines := strings.Split(content, "\n")

	var regions []DocumentRegion
	var plainBuf strings.Builder
	var blockBuf strings.Builder
	var blockType RegionType
	var blockName string
	inBlock := false

	flushPlain := func() {
		if plainBuf.Len() > 0 {
			regions = append(regions, DocumentRegion{
				Type:    RegionPlain,
				Content: plainBuf.String(),
			})
			plainBuf.Reset()
		}
	}

	for i, line := range lines {
		// Reconstitute each line with its newline (strings.Split strips them).
		// The last element after splitting "a\nb\n" is "", which gets no newline.
		lineWithNL := line
		if i < len(lines)-1 {
			lineWithNL = line + "\n"
		}

		trimmed := strings.TrimSpace(line)

		if !inBlock {
			if rt, name, ok := parseOpenMarker(trimmed); ok {
				flushPlain()
				inBlock = true
				blockType = rt
				blockName = name
				blockBuf.Reset()
			} else {
				plainBuf.WriteString(lineWithNL)
			}
		} else {
			if isCloseMarker(trimmed, blockType, blockName) {
				regions = append(regions, DocumentRegion{
					Type:    blockType,
					Name:    blockName,
					Content: blockBuf.String(),
				})
				inBlock = false
				blockType = ""
				blockName = ""
			} else {
				blockBuf.WriteString(lineWithNL)
			}
		}
	}

	// An unclosed block extends to EOF.
	if inBlock {
		regions = append(regions, DocumentRegion{
			Type:    blockType,
			Name:    blockName,
			Content: blockBuf.String(),
		})
	}

	// Flush any trailing plain text.
	flushPlain()

	return regions
}

// MergeDocument takes the regions of an existing document and a map of new
// generated-block contents keyed by block name, and returns the merged
// document. Generated blocks are replaced; analysis blocks and plain text are
// preserved verbatim. New sections not present in the existing document are
// appended in sorted key order.
func MergeDocument(existing []DocumentRegion, generated map[string]string) string {
	var sb strings.Builder
	handled := make(map[string]bool)

	for _, region := range existing {
		switch region.Type {
		case RegionPlain:
			sb.WriteString(region.Content)

		case RegionGenerated:
			sb.WriteString("<!-- generated:" + region.Name + " -->\n")
			if newContent, ok := generated[region.Name]; ok {
				sb.WriteString(newContent)
				handled[region.Name] = true
			} else {
				// No replacement supplied; keep the existing content.
				sb.WriteString(region.Content)
			}
			sb.WriteString("<!-- /generated:" + region.Name + " -->\n")

		case RegionAnalysis:
			// Always preserve analysis blocks verbatim, including the markers.
			sb.WriteString("<!-- analysis:" + region.Name + " -->\n")
			sb.WriteString(region.Content)
			sb.WriteString("<!-- /analysis:" + region.Name + " -->\n")
		}
	}

	// Append newly generated sections that did not exist in the prior document.
	// Sort keys for deterministic output.
	var newKeys []string
	for k := range generated {
		if !handled[k] {
			newKeys = append(newKeys, k)
		}
	}
	sort.Strings(newKeys)
	for _, k := range newKeys {
		sb.WriteString("<!-- generated:" + k + " -->\n")
		sb.WriteString(generated[k])
		sb.WriteString("<!-- /generated:" + k + " -->\n")
	}

	return sb.String()
}
