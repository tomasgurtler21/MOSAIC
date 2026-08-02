package screens

import (
	"fmt"

	"mosaic-log-analyzer/internal/domain"
)

// AbsentMarker is rendered for a token category with no data. It must never be
// "0" and must never be confused with a zero figure.
const AbsentMarker = "—"

// UnpricedMarker is rendered for a money cell whose model has no pricing entry.
// It must never be "$0.00".
const UnpricedMarker = "unpriced"

// ProvisionalLabel marks a run with no run_end/session_end so its cost is not
// read as final.
const ProvisionalLabel = "in progress"

// UnattributableLabel titles the separate unknown-run section.
const UnattributableLabel = "Unattributable"

// FormatTokens renders a TokenCount: AbsentMarker when absent, the grouped
// number otherwise (including "0" for a present zero).
func FormatTokens(t domain.TokenCount) string {
	v, ok := t.Value()
	if !ok {
		return AbsentMarker
	}
	return formatGrouped(v)
}

// FormatMoney renders a MoneyValue: AbsentMarker for MoneyNoData,
// UnpricedMarker for MoneyUnpriced, and a currency-formatted amount for
// MoneyKnown (including "$0.00").
func FormatMoney(v domain.MoneyValue) string {
	switch v.State {
	case domain.MoneyNoData:
		return AbsentMarker
	case domain.MoneyUnpriced:
		return UnpricedMarker
	case domain.MoneyKnown:
		return "$" + v.Amount.StringFixed(2)
	default:
		return AbsentMarker
	}
}

// FormatTotal renders a CategoryMoney total, appending a partial-total marker
// when Complete is false.
func FormatTotal(c domain.CategoryMoney) string {
	s := FormatMoney(c.Total)
	if !c.Complete {
		s += " (partial)"
	}
	return s
}

// FormatTotalsLine renders a compact single-line summary of a Totals value
// showing token categories and money side by side. Category names come from
// domain.TokenCategory.Label().
func FormatTotalsLine(t domain.Totals) string {
	parts := make([]string, 0, len(domain.BillableCategories())+1)
	for _, cat := range domain.BillableCategories() {
		count, _ := t.Tokens.Get(cat)
		parts = append(parts, fmt.Sprintf("%s: %s", cat.Label(), FormatTokens(count)))
	}
	parts = append(parts, FormatTotal(t.Money))
	result := ""
	for i, p := range parts {
		if i > 0 {
			result += "  "
		}
		result += p
	}
	return result
}

// formatGrouped formats an int64 with comma grouping for readability.
func formatGrouped(n int64) string {
	if n < 0 {
		return "-" + formatGrouped(-n)
	}
	if n < 1000 {
		return fmt.Sprintf("%d", n)
	}
	return formatGrouped(n/1000) + "," + fmt.Sprintf("%03d", n%1000)
}
