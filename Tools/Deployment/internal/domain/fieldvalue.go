package domain

// ValueKind classifies the structural shape of a FieldValue.
type ValueKind string

const (
	KindScalar  ValueKind = "scalar"
	KindList    ValueKind = "list"
	KindMapping ValueKind = "mapping"
)

// QuoteStyle describes how a scalar value is quoted in YAML output.
type QuoteStyle string

const (
	QuotePlain  QuoteStyle = ""       // unquoted
	QuoteSingle QuoteStyle = "single"
	QuoteDouble QuoteStyle = "double"
)

// ListStyle describes the YAML serialisation style of a list value.
type ListStyle string

const (
	ListFlow  ListStyle = "flow"  // [a, b, c]
	ListBlock ListStyle = "block" // - a\n- b
)

// FieldValue is a style-preserving YAML value shared between docformat (parse/serialise) and
// HarnessModule (render). Style fields are honoured on serialisation; a zero-value style means
// "choose the style the source used, or the harness default for new keys".
type FieldValue struct {
	Kind    ValueKind
	Scalar  string       // Kind == KindScalar; the value text without quotes
	Quote   QuoteStyle   // Kind == KindScalar
	Items   []FieldValue // Kind == KindList
	List    ListStyle    // Kind == KindList
	Pairs   []FieldPair  // Kind == KindMapping, order-significant
	Comment string       // trailing inline comment including the leading '#', or empty
}

// FieldPair is one key-value entry in a mapping value, preserving source order.
type FieldPair struct {
	Key   string
	Value FieldValue
}

// FrontmatterField is a named key-value pair in an agent frontmatter block.
type FrontmatterField struct {
	Key   string
	Value FieldValue
}

// ScalarValue constructs a scalar FieldValue with the given text and quote style.
func ScalarValue(text string, q QuoteStyle) FieldValue {
	return FieldValue{Kind: KindScalar, Scalar: text, Quote: q}
}

// ListValue constructs a list FieldValue with the given items and serialisation style.
func ListValue(items []FieldValue, style ListStyle) FieldValue {
	return FieldValue{Kind: KindList, Items: items, List: style}
}

// MappingValue constructs a mapping FieldValue with the given ordered pairs.
func MappingValue(pairs []FieldPair) FieldValue {
	return FieldValue{Kind: KindMapping, Pairs: pairs}
}
