package app

// stripped.go declares the Stage 2 reporting types shared by the promote result and the
// Stage 5/6 transform result: StripReason and StrippedField. Both types are data-only;
// no logic lives here. Assembly logic belongs in the service layer.

// StripReason explains why a frontmatter field was removed from generated output.
type StripReason string

const (
	// StripReasonUnmappedDivertedValue — the field is a declared diverted tool destination
	// and Values are the entries that could not be reverse-mapped to a generic tool.
	StripReasonUnmappedDivertedValue StripReason = "unmapped-diverted-value"
	// StripReasonUnknownField — the field is unknown to MOSAIC and to the identified
	// harness (FR-3). Values are the field's rendered contents.
	StripReasonUnknownField StripReason = "unknown-field"
	// StripReasonCarriedKey — the field was held in the carriage container (mosaic_carriage
	// or mosaic_carriage_markers) at the source document and did not travel the format-change
	// boundary. It is reported because the value cannot reach the target format, not because
	// it was unknown to the source harness.
	StripReasonCarriedKey StripReason = "carried-key"
)

// StrippedField reports one frontmatter field removed from generated output, with enough
// detail for the user to re-add it by hand.
type StrippedField struct {
	// Key is the frontmatter key that was removed.
	Key string `json:"key"`
	// Values are the human-readable values lost with it, in document order. Empty when the
	// field carried no value.
	Values []string `json:"values,omitempty"`
	// Reason is why it was removed.
	Reason StripReason `json:"reason"`
}
