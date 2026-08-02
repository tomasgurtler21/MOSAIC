package cli

import (
	"fmt"
	"io"
	"strconv"

	"mosaic-log-analyzer/internal/domain"
)

// EncodeText writes a human-readable rendering of the report to w.
//
// The output includes:
//   - All-runs totals (named runs only; unattributable is separate)
//   - Per-run rows with per-actor breakdown
//   - Provisional label for in-progress runs (no run_end/session_end observed)
//   - Unpriced marker for money cells whose model has no pricing entry
//   - The unattributable section (if present), separate from named-run totals
//   - Data-quality summary (findings count and individual findings)
func EncodeText(w io.Writer, r domain.Report) error {
	// All-runs total (named runs only)
	fmt.Fprintf(w, "=== All Runs Total ===\n")
	writeTextTotals(w, r.AllRuns, "  ")

	// Named runs
	if len(r.Runs) > 0 {
		fmt.Fprintf(w, "\n=== Runs ===\n")
		for _, run := range r.Runs {
			writeTextRun(w, run)
		}
	}

	// Unattributable bucket (distinct section, not folded into named-run totals)
	if r.Unattributable != nil {
		fmt.Fprintf(w, "\n=== Unattributable ===\n")
		writeTextTotals(w, r.Unattributable.Totals, "  ")
		for _, agent := range r.Unattributable.Agents {
			fmt.Fprintf(w, "  Agent: %s\n", agent.Actor.Label())
			writeTextTotals(w, agent.Totals, "    ")
		}
	}

	// Data-quality summary
	if !r.Quality.IsClean() {
		fmt.Fprintf(w, "\n=== Data Quality Findings ===\n")
		for kind, count := range r.Quality.Counts {
			fmt.Fprintf(w, "  %-30s  %d\n", string(kind), count)
		}
		if len(r.Quality.Findings) > 0 {
			fmt.Fprintf(w, "\nIndividual findings:\n")
			for _, f := range r.Quality.Findings {
				severity := "info"
				if f.Severity == domain.SeverityWarning {
					severity = "warning"
				}
				if f.Path != "" && f.Line > 0 {
					fmt.Fprintf(w, "  [%s] %s at %s:%d  %s\n", severity, string(f.Kind), f.Path, f.Line, f.Detail)
				} else if f.Path != "" {
					fmt.Fprintf(w, "  [%s] %s at %s  %s\n", severity, string(f.Kind), f.Path, f.Detail)
				} else {
					fmt.Fprintf(w, "  [%s] %s  %s\n", severity, string(f.Kind), f.Detail)
				}
			}
		}
	} else if r.Quality.Incomplete() {
		fmt.Fprintf(w, "\nData quality: incomplete (some data could not be read)\n")
	}

	// Unpriced model list
	if len(r.UnpricedModels) > 0 {
		fmt.Fprintf(w, "\nUnpriced models (no pricing entry found):\n")
		for _, m := range r.UnpricedModels {
			fmt.Fprintf(w, "  %s\n", string(m))
		}
	}

	return nil
}

// writeTextRun writes one run's section to w.
func writeTextRun(w io.Writer, run domain.RunReport) {
	label := run.Run.Label()
	if run.Provisional {
		fmt.Fprintf(w, "\nRun: %s  [in progress]\n", label)
	} else {
		fmt.Fprintf(w, "\nRun: %s\n", label)
	}
	writeTextTotals(w, run.Totals, "  ")

	// Orchestrator actor
	fmt.Fprintf(w, "  Orchestrator:\n")
	writeTextTotals(w, run.Orchestrator.Totals, "    ")

	// Agent-instance actors
	for _, agent := range run.Agents {
		fmt.Fprintf(w, "  Agent: %s\n", agent.Actor.Label())
		writeTextTotals(w, agent.Totals, "    ")
	}
}

// writeTextTotals writes a compact token + money summary at the given indent level.
func writeTextTotals(w io.Writer, t domain.Totals, indent string) {
	// Token figures
	writeTextTokens(w, t.Tokens, indent)
	// Money summary (total)
	fmt.Fprintf(w, "%sMoney: %s\n", indent, formatTextMoney(t.Money.Total))
}

// writeTextTokens writes per-category token counts with thousand-separator grouping.
// Labels come from domain.TokenCategory.Label() so the CLI and TUI vocabularies
// cannot drift apart.
func writeTextTokens(w io.Writer, u domain.TokenUsage, indent string) {
	for _, cat := range domain.BillableCategories() {
		count, _ := u.Get(cat)
		v, ok := count.Value()
		if ok {
			fmt.Fprintf(w, "%s%s: %s tokens\n", indent, cat.Label(), formatGrouped(v))
		} else {
			fmt.Fprintf(w, "%s%s: -\n", indent, cat.Label())
		}
	}
}

// formatGrouped formats n with comma thousand-separators (e.g. 1234567 → "1,234,567").
func formatGrouped(n int64) string {
	if n < 0 {
		return "-" + formatGrouped(-n)
	}
	s := strconv.FormatInt(n, 10)
	// Insert commas every three digits from the right.
	out := make([]byte, 0, len(s)+(len(s)-1)/3)
	offset := len(s) % 3
	if offset == 0 {
		offset = 3
	}
	out = append(out, s[:offset]...)
	for i := offset; i < len(s); i += 3 {
		out = append(out, ',')
		out = append(out, s[i:i+3]...)
	}
	return string(out)
}

// formatTextMoney returns a human-readable money string.
// Unpriced is rendered as "unpriced" (not "$0.00") to distinguish it from zero.
// No-data is rendered as "-".
func formatTextMoney(mv domain.MoneyValue) string {
	switch mv.State {
	case domain.MoneyKnown:
		return "$" + mv.Amount.StringFixed(2)
	case domain.MoneyUnpriced:
		return "unpriced"
	default: // MoneyNoData
		return "-"
	}
}
