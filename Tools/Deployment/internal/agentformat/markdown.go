package agentformat

import (
	"bytes"
	"strings"

	"mosaic-deploy/internal/agentformat/formatid"
)

func init() {
	// The Markdown identity translator lives in this package and self-registers here,
	// exactly as the built-in harnesses do. Any import of agentformat brings it in.
	Register(formatid.Markdown, markdownTranslator{})
}

// markdownTranslator is the identity translator for the Markdown format.
// Decode is the identity on deployed bytes. Encode strips the carriage container
// from the canonical frontmatter using surgical byte-level line deletion (no
// re-rendering), reports each held user key as EntryStrippedContainer, and
// returns all other bytes byte-for-byte unchanged.
type markdownTranslator struct{}

// Encode strips the carriage container from the canonical YAML frontmatter and
// returns the result. All bytes outside the container lines are preserved exactly
// (byte-identical to the input, including non-standard YAML formatting). Each
// user key held in the container is reported as EntryStrippedContainer.
//
// Markdown cannot carry user TOML keys, so the container is always stripped on
// encode. Decode is the identity (deployed Markdown files carry no container).
func (markdownTranslator) Encode(canonical []byte, ctx ArtifactContext) ([]byte, Report, error) {
	var report Report

	// Fast path: if neither carriage key name appears in the input, return unchanged.
	hasValues := bytes.Contains(canonical, []byte(CarriageValuesKey))
	hasMarkers := bytes.Contains(canonical, []byte(CarriageMarkersKey))
	if !hasValues && !hasMarkers {
		return canonical, report, nil
	}

	// Locate frontmatter delimiters. The document must start with "---\n" and
	// contain a closing "\n---\n".
	if !bytes.HasPrefix(canonical, []byte("---\n")) {
		return canonical, report, nil
	}
	rest := canonical[4:]
	endIdx := bytes.Index(rest, []byte("\n---\n"))
	if endIdx < 0 {
		return canonical, report, nil
	}

	// fmContent holds the YAML frontmatter lines (including the final \n).
	// body holds everything after the closing "---\n".
	fmContent := rest[:endIdx+1]
	body := rest[endIdx+5:] // skip \n---\n (5 bytes)

	// Process frontmatter lines, removing carriage container lines.
	var out bytes.Buffer
	out.WriteString("---\n")

	fm := fmContent
	inCarriageMapping := false // true while consuming indented children of mosaic_carriage

	for len(fm) > 0 {
		// Extract one line (including its terminating \n).
		nlIdx := bytes.IndexByte(fm, '\n')
		var line []byte
		if nlIdx >= 0 {
			line = fm[:nlIdx+1]
			fm = fm[nlIdx+1:]
		} else {
			line = fm
			fm = nil
		}

		// lineStr and trimmed are used only for key detection; all output uses
		// the original line bytes to preserve non-standard formatting.
		lineStr := string(line)
		trimmed := strings.TrimRight(lineStr, "\n\r")

		// --- mosaic_carriage_markers (flow list, single line). ---
		// Must be checked before mosaic_carriage because both start with the
		// same characters up to the underscore before "markers".
		if trimmed == CarriageMarkersKey+":" ||
			strings.HasPrefix(trimmed, CarriageMarkersKey+": ") ||
			strings.HasPrefix(trimmed, CarriageMarkersKey+":\t") {
			// Exit any active value mapping block.
			inCarriageMapping = false
			// Parse the flow list value to extract key names for reporting.
			afterColon := strings.TrimPrefix(trimmed, CarriageMarkersKey+":")
			afterColon = strings.TrimSpace(afterColon)
			for _, k := range parseYAMLFlowStringList(afterColon) {
				report.Entries = append(report.Entries, ReportEntry{
					Kind:   EntryStrippedContainer,
					Key:    k,
					Reason: "Markdown format cannot carry user TOML keys; the carriage container was stripped",
				})
			}
			continue // skip this line
		}

		// --- mosaic_carriage (block mapping header). ---
		if trimmed == CarriageValuesKey+":" ||
			strings.HasPrefix(trimmed, CarriageValuesKey+": ") ||
			strings.HasPrefix(trimmed, CarriageValuesKey+":\t") {
			inCarriageMapping = true
			continue // skip this line
		}

		// --- Indented children of mosaic_carriage. ---
		if inCarriageMapping {
			if len(line) > 0 && (line[0] == ' ' || line[0] == '\t') {
				// An indented line is a child of the mapping: parse and report its key.
				key := extractYAMLKeyFromMappingLine(trimmed)
				if key != "" {
					report.Entries = append(report.Entries, ReportEntry{
						Kind:   EntryStrippedContainer,
						Key:    key,
						Reason: "Markdown format cannot carry user TOML keys; the carriage container was stripped",
					})
				}
				continue // skip this line
			}
			// Non-indented line: the mapping block has ended.
			inCarriageMapping = false
		}

		out.Write(line)
	}

	out.WriteString("---\n")
	out.Write(body)

	return out.Bytes(), report, nil
}

// Decode is the identity on deployed bytes and returns an empty Report.
// Deployed Markdown files do not contain the carriage container.
func (markdownTranslator) Decode(deployed []byte, ctx ArtifactContext) ([]byte, Report, error) {
	return deployed, Report{}, nil
}

// extractYAMLKeyFromMappingLine extracts the YAML mapping key from an indented
// line such as "  carried_key: \"value\"". It trims leading whitespace then
// returns everything before the first colon.
func extractYAMLKeyFromMappingLine(line string) string {
	line = strings.TrimLeft(line, " \t")
	idx := strings.IndexByte(line, ':')
	if idx < 0 {
		return ""
	}
	return strings.TrimSpace(line[:idx])
}

// parseYAMLFlowStringList parses a YAML flow sequence of double-quoted strings
// such as `["key1", "key2"]` and returns the unquoted values. It handles the
// specific format produced by docformat.RenderFrontmatter for flow lists.
// Entries that are not double-quoted strings are silently skipped.
//
// A quote-aware state machine is used to split items so that commas inside
// double-quoted strings (e.g. ["a,b", "c"]) do not produce split artefacts.
func parseYAMLFlowStringList(s string) []string {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "[") || !strings.HasSuffix(s, "]") {
		return nil
	}
	inner := s[1 : len(s)-1]
	if strings.TrimSpace(inner) == "" {
		return nil
	}
	var result []string
	inQuote := false
	escaped := false
	start := 0
	for i := 0; i < len(inner); i++ {
		c := inner[i]
		if escaped {
			escaped = false
			continue
		}
		if c == '\\' && inQuote {
			escaped = true
			continue
		}
		if c == '"' {
			inQuote = !inQuote
			continue
		}
		if c == ',' && !inQuote {
			item := strings.TrimSpace(inner[start:i])
			if strings.HasPrefix(item, `"`) && strings.HasSuffix(item, `"`) {
				result = append(result, item[1:len(item)-1])
			}
			start = i + 1
		}
	}
	// Process the final item after the last comma (or the only item if no commas).
	item := strings.TrimSpace(inner[start:])
	if strings.HasPrefix(item, `"`) && strings.HasSuffix(item, `"`) {
		result = append(result, item[1:len(item)-1])
	}
	return result
}
