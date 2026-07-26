package todo

import (
	"fmt"
	"strings"
)

// RenderMarkdown produces MOSAIC-DEPLOYMENT-TODO.md as a byte slice. When the groups slice is
// empty (no gaps) the output still contains a header and a clear statement that no manual
// actions are required, rather than a blank or confusing file (AC15.7).
func RenderMarkdown(groups []Group, meta Meta) []byte {
	var sb strings.Builder

	fmt.Fprintf(&sb, "# MOSAIC Deployment TODO\n\n")
	fmt.Fprintf(&sb, "Generated: %s  \n", meta.GeneratedAt.Format("2006-01-02 15:04:05 UTC"))
	fmt.Fprintf(&sb, "Harness: %s  \n", meta.Harness)
	fmt.Fprintf(&sb, "Workspace: %s  \n", meta.WorkspacePath)
	fmt.Fprintf(&sb, "Mode: %s  \n\n", string(meta.Mode))

	if len(groups) == 0 {
		sb.WriteString("## No manual actions required\n\n")
		sb.WriteString("All items were deployed successfully. No gaps were detected during this run.\n")
		return []byte(sb.String())
	}

	for _, g := range groups {
		fmt.Fprintf(&sb, "## %s\n\n", string(g.Category))
		for _, item := range g.Items {
			fmt.Fprintf(&sb, "- [ ] **%s**", item.Subject)
			if item.Detail != "" {
				fmt.Fprintf(&sb, " — %s", item.Detail)
			}
			sb.WriteString("\n")
			if item.Fragment != "" {
				fmt.Fprintf(&sb, "\n```\n%s\n```\n", item.Fragment)
			}
		}
		sb.WriteString("\n")
	}

	return []byte(sb.String())
}

// RenderSummary produces the lines displayed by both the TUI and the CLI summary view. It
// operates on the same []Group as RenderMarkdown so the two outputs cannot disagree (AC15.6).
func RenderSummary(groups []Group) []SummaryLine {
	lines := make([]SummaryLine, 0, len(groups))
	for _, g := range groups {
		count := len(g.Items)
		noun := "item"
		if count != 1 {
			noun = "items"
		}
		lines = append(lines, SummaryLine{
			Category: g.Category,
			Text:     fmt.Sprintf("%s: %d %s require manual action", string(g.Category), count, noun),
		})
	}
	return lines
}
