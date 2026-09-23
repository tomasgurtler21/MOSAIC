package snapshot

import (
	"bytes"
	"strings"
)

// TransformRule describes a single YAML-frontmatter field rewrite applied to
// snapshot copies. Each rule targets one field name and replaces one value
// with another.
type TransformRule struct {
	Field    string // YAML frontmatter field name (e.g. "mode")
	OldValue string // value to match (e.g. "subagent")
	NewValue string // replacement value (e.g. "primary")
}

// TransformationsFor returns the set of frontmatter transformation rules
// for the given harness. Returns nil for harnesses with no transformations.
//
// Known transformation sets:
//   - opencode: [{Field: "mode", OldValue: "subagent", NewValue: "primary"}]
//   - claude-code: nil (no transformations needed)
//   - ghcp-cli: nil (no transformations needed)
//   - fake: nil (test double, no transformations)
//   - unknown: nil (unknown harnesses have no transformations)
func TransformationsFor(harnessID string) []TransformRule {
	switch harnessID {
	case "opencode":
		return []TransformRule{
			{Field: "mode", OldValue: "subagent", NewValue: "primary"},
		}
	default:
		return nil
	}
}

// TransformFile applies the given transformation rules to the YAML
// frontmatter of a single file's content bytes. Returns the transformed
// content. If the content has no YAML frontmatter (no leading "---" line),
// the content is returned unchanged.
//
// The transformation is line-based: for each rule, any line within the
// frontmatter block (between the first "---" and the next "---") that
// matches "{rule.Field}: {rule.OldValue}" (with optional surrounding
// whitespace) is replaced with "{rule.Field}: {rule.NewValue}".
func TransformFile(content []byte, rules []TransformRule) []byte {
	if len(content) == 0 || len(rules) == 0 {
		return content
	}

	lines := bytes.Split(content, []byte("\n"))

	// First line must be "---" to indicate YAML frontmatter.
	if len(lines) == 0 || strings.TrimSpace(string(lines[0])) != "---" {
		return content
	}

	// Find the closing "---" delimiter.
	closingIdx := -1
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(string(lines[i])) == "---" {
			closingIdx = i
			break
		}
	}
	if closingIdx == -1 {
		// Unclosed frontmatter block: do not transform.
		return content
	}

	// Apply rules to lines within the frontmatter block (exclusive of delimiters).
	modified := false
	for i := 1; i < closingIdx; i++ {
		rewritten := applyRulesToLine(lines[i], rules)
		if !bytes.Equal(rewritten, lines[i]) {
			lines[i] = rewritten
			modified = true
		}
	}

	if !modified {
		return content
	}
	return bytes.Join(lines, []byte("\n"))
}

// applyRulesToLine checks the given line against each rule and returns the
// rewritten line if a match is found. Leading whitespace is preserved. Only
// the first matching rule is applied per line.
//
// CRLF handling: bytes.Split on \n leaves \r at the end of lines from CRLF
// files. The \r is stripped before matching and re-appended to the replacement
// so that each line's original line ending is preserved independently.
// Trailing spaces or tabs before \r do not prevent matching; they are dropped
// in the replacement (existing LF behavior), but \r is preserved.
func applyRulesToLine(line []byte, rules []TransformRule) []byte {
	lineStr := string(line)
	trimmed := strings.TrimLeft(lineStr, " \t")
	leading := lineStr[:len(lineStr)-len(trimmed)]

	// Detect and strip trailing \r before matching.
	hasCR := strings.HasSuffix(trimmed, "\r")
	if hasCR {
		trimmed = trimmed[:len(trimmed)-1]
	}

	for _, rule := range rules {
		target := rule.Field + ": " + rule.OldValue
		if strings.TrimRight(trimmed, " \t") == target {
			replacement := leading + rule.Field + ": " + rule.NewValue
			if hasCR {
				replacement += "\r"
			}
			return []byte(replacement)
		}
	}
	return line
}
