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

// ErrToolsEmpty is returned when the tools key is present but resolves
// to zero tool entries.
var ErrToolsEmpty = errors.New("harness: agent definition has an empty tools field")

// ExtractClaudeCodeTools reads the deployed agent definition at path,
// parses its frontmatter, and returns the Claude Code tool names from
// the tools field as individual strings suitable for --allowedTools.
//
// Returns ErrToolsMissing when the tools key is absent from the
// frontmatter, and ErrToolsEmpty when the key is present but resolves
// to zero tool names.
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
	if fv.Kind != mosaic.KindScalar {
		return nil, ErrToolsEmpty
	}

	raw := strings.TrimSpace(fv.Scalar)
	if raw == "" {
		return nil, ErrToolsEmpty
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
		return nil, ErrToolsEmpty
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
// Returns ErrToolsMissing when the tools key is absent, ErrToolsEmpty
// when it resolves to zero tool names.
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

	if len(fv.Items) == 0 {
		return nil, ErrToolsEmpty
	}

	var tools []string
	for _, item := range fv.Items {
		if item.Kind != mosaic.KindScalar {
			continue
		}
		name := strings.TrimSpace(item.Scalar)
		translated, include := translateGHCPTool(name)
		if include {
			tools = append(tools, translated)
		}
	}

	if len(tools) == 0 {
		return nil, ErrToolsEmpty
	}
	return tools, nil
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
