package domain

import "mosaic-common/mosaic"

// Type aliases — domain.X IS mosaic.X (same type, no conversion needed).
type ValueKind = mosaic.ValueKind
type QuoteStyle = mosaic.QuoteStyle
type ListStyle = mosaic.ListStyle
type FieldValue = mosaic.FieldValue
type FieldPair = mosaic.FieldPair
type FrontmatterField = mosaic.FrontmatterField

// Re-declared constants pointing at the mosaic-common originals.
const (
	KindScalar  = mosaic.KindScalar
	KindList    = mosaic.KindList
	KindMapping = mosaic.KindMapping

	QuotePlain  = mosaic.QuotePlain
	QuoteSingle = mosaic.QuoteSingle
	QuoteDouble = mosaic.QuoteDouble

	ListFlow  = mosaic.ListFlow
	ListBlock = mosaic.ListBlock
)

// Constructor functions delegating to mosaic-common — existing callers using domain.ScalarValue
// etc. continue to compile without changes.
func ScalarValue(text string, q QuoteStyle) FieldValue  { return mosaic.ScalarValue(text, q) }
func ListValue(items []FieldValue, style ListStyle) FieldValue { return mosaic.ListValue(items, style) }
func MappingValue(pairs []FieldPair) FieldValue          { return mosaic.MappingValue(pairs) }
