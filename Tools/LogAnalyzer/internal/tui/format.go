package tui

import (
	"mosaic-log-analyzer/internal/domain"
	"mosaic-log-analyzer/internal/tui/screens"
)

// Re-export the rendering constants and functions from the screens package so
// callers (including tests) in the tui package can use them without an extra
// import. The canonical definitions live in screens to avoid a circular import.

// AbsentMarker is rendered for a token category with no data. It must never be
// "0" and must never be confused with a zero figure.
const AbsentMarker = screens.AbsentMarker

// UnpricedMarker is rendered for a money cell whose model has no pricing entry.
// It must never be "$0.00".
const UnpricedMarker = screens.UnpricedMarker

// ProvisionalLabel marks a run with no run_end/session_end so its cost is not
// read as final.
const ProvisionalLabel = screens.ProvisionalLabel

// UnattributableLabel titles the separate unknown-run section.
const UnattributableLabel = screens.UnattributableLabel

// FormatTokens renders a TokenCount: AbsentMarker when absent, the grouped
// number otherwise (including "0" for a present zero).
func FormatTokens(t domain.TokenCount) string {
	return screens.FormatTokens(t)
}

// FormatMoney renders a MoneyValue: AbsentMarker for MoneyNoData,
// UnpricedMarker for MoneyUnpriced, and a currency-formatted amount for
// MoneyKnown (including "$0.00").
func FormatMoney(v domain.MoneyValue) string {
	return screens.FormatMoney(v)
}

// FormatTotal renders a CategoryMoney total, appending a partial-total marker
// when Complete is false.
func FormatTotal(c domain.CategoryMoney) string {
	return screens.FormatTotal(c)
}
