package harness

import (
	"errors"
	"os"
	"strings"

	"mosaic-common/docformat"
	"mosaic-common/mosaic"
)

// ErrToolsMissing is returned when the invoked agent's definition file
// has no tools key in its frontmatter.
var ErrToolsMissing = errors.New("harness: agent definition has no tools field in frontmatter")

// ErrToolsEmpty is returned when the tools field is present but has a
// wrong shape (not the expected kind for the target harness), or when
// all items are unrecognised names (misconfiguration detection).
//
// For GHCP CLI: returned when tools is not a list, or when every scalar
// item in the list is an unrecognised name.
// For Claude Code: returned when tools is a non-empty list (wrong kind;
// Claude Code tools are comma-separated scalars).
var ErrToolsEmpty = errors.New("harness: agent definition tools field has wrong shape or all unrecognised names")

// ExtractClaudeCodeTools reads the deployed agent definition at path,
// parses its frontmatter, and returns the Claude Code tool names from
// the tools field as individual strings suitable for --allowedTools.
//
// Return values:
//   - (tools, nil) when the tools field is a non-empty comma-separated
//     scalar with at least one tool name.
//   - (empty, nil) when the tools field is present but empty:
//     an empty/blank scalar, an all-whitespace/comma scalar, or a
//     zero-item list (tools: []).
//     NOTE: "empty" means len(tools) == 0. Callers and tests MUST use
//     len(tools) == 0 to detect this case, not nil checks.
//   - (nil, ErrToolsMissing) when the tools key is absent.
//   - (nil, ErrToolsEmpty) when the tools field is a non-empty list
//     (wrong kind -- Claude Code tools are comma-separated scalars;
//     a non-empty list is misconfiguration, not a valid empty representation).
//
// The deployed Claude Code tools field is a comma-separated scalar
// string (e.g. "Read, Write, Edit, Bash"). This function splits on
// commas and trims whitespace from each entry.
func ExtractClaudeCodeTools(path string) ([]string, error) {
	fv, err := readToolsField(path)
	if err != nil {
		return nil, err
	}

	// Claude Code expects a scalar (comma-separated string).
	// A zero-item list is accepted as an alternative empty representation.
	if fv.Kind != mosaic.KindScalar {
		if fv.Kind == mosaic.KindList && len(fv.Items) == 0 {
			return []string{}, nil
		}
		return nil, ErrToolsEmpty
	}

	raw := strings.TrimSpace(fv.Scalar)
	if raw == "" {
		return []string{}, nil
	}

	parts := strings.Split(raw, ",")
	var tools []string
	for _, p := range parts {
		name := strings.TrimSpace(p)
		if name != "" {
			tools = append(tools, name)
		}
	}
	if len(tools) == 0 {
		return []string{}, nil
	}
	return tools, nil
}

// ExtractGHCPCLITools reads the deployed agent definition at path,
// parses its frontmatter, and returns GHCP CLI --allow-tool arguments
// with the MOSAIC-to-copilot-CLI translation applied.
//
// Translation table (MOSAIC deployed name -> copilot CLI --allow-tool kind):
//
//	edit     -> write
//	execute  -> shell
//	agent    -> agent   (verbatim pass-through; confirmed safe no-op)
//	skill    -> skill   (verbatim pass-through; confirmed safe no-op)
//	read     -> (excluded: GHCP CLI has no read kind; reads are ungated)
//	search   -> (excluded: GHCP CLI auto-allows search operations)
//	ask_user -> (excluded: handled by --no-ask-user flag separately)
//
// Return values:
//   - (empty, nil) when the tools field is an empty list (tools: []),
//     or when all items translate to ungated/excluded kinds and at least
//     one item is a recognised tool name (ungated or gated).
//     NOTE: "empty" means len(tools) == 0. The returned slice may be
//     nil or non-nil empty depending on the code path. Callers and tests
//     MUST use len(tools) == 0 to detect this case.
//   - (tools, nil) when at least one item translates to a gated kind.
//   - (nil, ErrToolsMissing) when the tools key is absent from frontmatter.
//   - (nil, ErrToolsEmpty) when the tools field is present but has a
//     wrong shape (not a list), or when all scalar items are unrecognised
//     names (misconfiguration detection).
//
// Non-scalar list items are silently skipped and do not count as
// recognised names.
//
// The deployed GHCP CLI tools field is a flow-style YAML list
// (e.g. ['read', 'edit', 'search', 'execute', 'ask_user', 'agent']).
func ExtractGHCPCLITools(path string) ([]string, error) {
	fv, err := readToolsField(path)
	if err != nil {
		return nil, err
	}

	// GHCP CLI expects a list (flow-style YAML list).
	if fv.Kind != mosaic.KindList {
		return nil, ErrToolsEmpty
	}

	// An explicit empty list (tools: []) is a valid empty representation.
	if len(fv.Items) == 0 {
		return []string{}, nil
	}

	var tools []string
	var anyRecognised bool
	for _, item := range fv.Items {
		if item.Kind != mosaic.KindScalar {
			// Non-scalar items are silently skipped; do not set anyRecognised.
			continue
		}
		name := strings.TrimSpace(item.Scalar)
		translated, include := translateGHCPTool(name)
		if include {
			tools = append(tools, translated)
			anyRecognised = true
		} else if isUngatedGHCPTool(name) {
			anyRecognised = true
		}
	}

	if len(tools) == 0 && !anyRecognised {
		// All items were unrecognised names: likely misconfiguration.
		return nil, ErrToolsEmpty
	}
	// When all items are ungated, tools is nil (no appends).
	// Callers use len(tools) == 0, not nil checks.
	return tools, nil
}

// isUngatedGHCPTool reports whether name is a known GHCP CLI tool name
// that is ungated (excluded from --allow-tool but still available without
// explicit permission). This avoids duplicating the name set already
// present in translateGHCPTool's switch.
//
// Returns true for: "read", "search", "ask_user".
// Returns false for: everything else (gated tools, unknown names).
func isUngatedGHCPTool(name string) bool {
	switch name {
	case "read", "search", "ask_user":
		return true
	default:
		return false
	}
}

// translateGHCPTool maps a MOSAIC GHCP CLI deployed tool name to the
// copilot CLI --allow-tool kind name. Returns ("", false) for tools that
// should be excluded from --allow-tool entries.
func translateGHCPTool(name string) (string, bool) {
	switch name {
	case "edit":
		return "write", true
	case "execute":
		return "shell", true
	case "agent":
		return "agent", true
	case "skill":
		return "skill", true
	case "read", "search", "ask_user":
		// Excluded: read/search are ungated or auto-allowed; ask_user is
		// handled by --no-ask-user separately.
		return "", false
	default:
		// Unknown tool kinds are excluded rather than forwarded blindly.
		return "", false
	}
}

// readToolsField reads the agent definition file at path, parses its
// frontmatter, and returns the FieldValue for the "tools" key.
// Returns ErrToolsMissing when the key is absent.
func readToolsField(path string) (mosaic.FieldValue, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return mosaic.FieldValue{}, err
	}

	doc, err := docformat.Parse(data)
	if err != nil {
		return mosaic.FieldValue{}, err
	}

	fv, ok := doc.Frontmatter().Get("tools")
	if !ok {
		return mosaic.FieldValue{}, ErrToolsMissing
	}
	return fv, nil
}
