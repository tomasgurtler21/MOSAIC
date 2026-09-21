package codextoml

import (
	"bytes"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/pelletier/go-toml/v2"

	"mosaic-common/docformat"
	"mosaic-common/mosaic"

	"mosaic-deploy/internal/agentfields"
	"mosaic-deploy/internal/agentformat"
)

// Decode converts Codex TOML bytes to a canonical MOSAIC Markdown document
// with YAML frontmatter and a body.
//
// The decoded key order is fixed and total:
//  1. Stamps in agentfields.All() registry order
//  2. name, description, model, sandbox_mode -- each only if present
//  3. mosaic_carriage (value-carried user keys) -- absent when empty
//  4. mosaic_carriage_markers (marker-carried user key names) -- absent when empty
//
// The quoting policy is applied per value (see applyQuotingPolicy). Non-owned
// keys are placed in the carriage container (mosaic_carriage or
// mosaic_carriage_markers) according to their TOML type and source spelling.
func (codexTranslator) Decode(deployed []byte, ctx agentformat.ArtifactContext) ([]byte, agentformat.Report, error) {
	// Read stamp block from the leading comment lines. This is tolerant and never
	// fails; a missing or malformed block yields an empty map.
	stamps, _ := readStampBlock(deployed)

	// Parse the TOML. A malformed document (syntax error, duplicate key) is the one
	// failure the tolerance rule does not cover.
	var rawValues map[string]interface{}
	if err := toml.Unmarshal(deployed, &rawValues); err != nil {
		return nil, agentformat.Report{}, &agentformat.ArtifactError{
			Phase: "decode",
			Key:   "",
			Err:   fmt.Errorf("%w: %s", agentformat.ErrMalformedDeployed, err.Error()),
		}
	}

	// --- Validate and extract owned string keys ---

	ownedVals := make(map[string]string)
	ownedPresent := make(map[string]bool)

	for _, key := range []string{"name", "description", "model", "sandbox_mode"} {
		rawVal, exists := rawValues[key]
		if !exists {
			continue
		}
		s, ok := rawVal.(string)
		if !ok {
			return nil, agentformat.Report{}, &agentformat.ArtifactError{
				Phase: "decode",
				Key:   key,
				Err:   fmt.Errorf("%w: key %q must be a string, got %T", agentformat.ErrMalformedDeployed, key, rawVal),
			}
		}
		ownedVals[key] = s
		ownedPresent[key] = true
	}

	// --- developer_instructions ---

	var body []byte
	var report agentformat.Report
	instructionsPresent := false

	if rawVal, exists := rawValues["developer_instructions"]; exists {
		instructionsPresent = true
		s, ok := rawVal.(string)
		if !ok {
			return nil, agentformat.Report{}, &agentformat.ArtifactError{
				Phase: "decode",
				Key:   "developer_instructions",
				Err:   fmt.Errorf("%w: key %q must be a string, got %T", agentformat.ErrMalformedDeployed, "developer_instructions", rawVal),
			}
		}
		body = []byte(s)
	}

	if !instructionsPresent {
		report.Entries = append(report.Entries, agentformat.ReportEntry{
			Kind:   agentformat.EntryMissingInstructions,
			Key:    "developer_instructions",
			Reason: "deployed file carries no developer_instructions key; canonical body is empty",
		})
	}

	// --- Build canonical frontmatter fields in fixed total order ---

	var fields []mosaic.FrontmatterField

	// 1. Stamps in agentfields.All() registry order.
	for _, f := range agentfields.All() {
		val, ok := stamps[f.Deployed]
		if !ok {
			continue
		}
		fv, err := applyQuotingPolicy(val, f.Deployed)
		if err != nil {
			return nil, agentformat.Report{}, err
		}
		fields = append(fields, mosaic.FrontmatterField{Key: f.Deployed, Value: fv})
	}

	// 2. name, description, model, sandbox_mode -- each only if present.
	for _, key := range []string{"name", "description", "model", "sandbox_mode"} {
		if !ownedPresent[key] {
			continue
		}
		fv, err := applyQuotingPolicy(ownedVals[key], key)
		if err != nil {
			return nil, agentformat.Report{}, err
		}
		fields = append(fields, mosaic.FrontmatterField{Key: key, Value: fv})
	}

	// 3 & 4. Build carriage containers for non-owned user keys.
	// Scan the deployed source to collect per-key metadata (attached comment flags
	// and raw scalar spellings). This is needed to classify keys correctly.
	deployedScan := scanTomlLines(deployed)
	deployedInfos := buildInfosFromScan(deployedScan)

	// Collect all non-owned, non-carriage top-level keys in ascending byte order.
	var nonOwnedKeys []string
	for key := range rawValues {
		if isMosaicOwned(key) || agentformat.IsCarriageKey(key) {
			continue
		}
		nonOwnedKeys = append(nonOwnedKeys, key)
	}
	sort.Strings(nonOwnedKeys)

	var valueCarriagePairs []mosaic.FieldPair // for mosaic_carriage
	var markerNames []string                  // for mosaic_carriage_markers
	markerSet := make(map[string]bool)        // de-duplication guard

	for _, key := range nonOwnedKeys {
		val := rawValues[key]
		info := deployedInfos[key]

		addMarker := func() {
			if !markerSet[key] {
				markerSet[key] = true
				markerNames = append(markerNames, key)
			}
		}

		// Bare-illegal keys are always marker-carried regardless of value shape.
		if !isBareKey(key) {
			addMarker()
			continue
		}

		// Keys with attached TOML comments are escalated to marker carriage so
		// the comment travels with the span through the prior-bytes channel.
		if info != nil && info.hasAttachedComment {
			addMarker()
			continue
		}

		// Classify by Go type produced by go-toml/v2.
		switch v := val.(type) {
		case string:
			if hasUnrepresentableControlChar(v) {
				// Control characters that cannot be pre-escaped → marker.
				addMarker()
			} else {
				// Value-carry as TOML string, using the pre-escape trick to
				// preserve the string type marker (QuoteDouble after roundtrip).
				valueCarriagePairs = append(valueCarriagePairs, mosaic.FieldPair{
					Key:   key,
					Value: makeStringCarriageFV(v),
				})
			}
		case bool:
			boolStr := "false"
			if v {
				boolStr = "true"
			}
			valueCarriagePairs = append(valueCarriagePairs, mosaic.FieldPair{
				Key:   key,
				Value: mosaic.ScalarValue(boolStr, mosaic.QuotePlain),
			})
		case int64:
			rawScalar := ""
			if info != nil {
				rawScalar = info.rawScalar
			}
			if isPlainDecimalInt(rawScalar) {
				// Plain-decimal integer: value-carry with the original text as
				// QuotePlain so encode re-emits it as a TOML integer.
				valueCarriagePairs = append(valueCarriagePairs, mosaic.FieldPair{
					Key:   key,
					Value: mosaic.ScalarValue(rawScalar, mosaic.QuotePlain),
				})
			} else {
				// Exotic spelling (0x1F, 1_000, +5): the library normalised the
				// value; escalate to marker to preserve the original text via the
				// prior-bytes channel.
				addMarker()
			}
		default:
			// float64, map[string]interface{} (table), []interface{} (array),
			// toml.LocalDate, time.Time, and any other type: marker-carry.
			// - Floats: always escalated to preserve source spelling (3.14, inf, nan).
			// - Maps/slices: non-scalar types that cannot be value-carried.
			addMarker()
		}
	}

	// 3. mosaic_carriage (value-carried pairs in ascending byte order by key).
	// Pairs are already sorted because nonOwnedKeys was sorted.
	if len(valueCarriagePairs) > 0 {
		fields = append(fields, mosaic.FrontmatterField{
			Key:   agentformat.CarriageValuesKey,
			Value: mosaic.MappingValue(valueCarriagePairs),
		})
	}

	// 4. mosaic_carriage_markers (marker names in ascending byte order).
	// Names are already sorted because nonOwnedKeys was sorted and we appended
	// from that sorted slice.
	if len(markerNames) > 0 {
		items := make([]mosaic.FieldValue, len(markerNames))
		for i, name := range markerNames {
			items[i] = mosaic.ScalarValue(name, mosaic.QuoteDouble)
		}
		fields = append(fields, mosaic.FrontmatterField{
			Key:   agentformat.CarriageMarkersKey,
			Value: mosaic.ListValue(items, mosaic.ListFlow),
		})
	}

	// --- Render canonical document ---

	fmText := docformat.RenderFrontmatter(fields, "\n")

	var buf bytes.Buffer
	buf.WriteString("---\n")
	buf.WriteString(fmText)
	buf.WriteString("---\n")
	buf.Write(body)

	return buf.Bytes(), report, nil
}

// applyQuotingPolicy selects the quote style for a MOSAIC-owned value and
// returns the mosaic.FieldValue to pass to docformat.RenderFrontmatter.
//
// The three rules:
//  1. Unquoted (QuotePlain) when the value needs no escaping and isReservedSpelling is false.
//  2. Double-quoted: pre-escape to the five sequences docformat understands, then
//     emit as a QuotePlain value that already contains the surrounding "...".
//  3. Unrepresentable: a control character other than LF, CR, TAB -- abort with
//     ErrUnrepresentableValue.
//
// Note on the "pre-escape as QuotePlain" approach: docformat's escapeDoubleQuoted
// only escapes '\' and '"', and does NOT escape newlines or tabs. To produce a
// double-quoted YAML scalar that the docformat parse side understands, the translator
// pre-escapes the value to the five sequences (\", \\, \n, \r, \t), then wraps the
// result in "..." and passes it as a QuotePlain scalar. docformat emits it verbatim,
// and the parse side's unescapeDoubleQuoted decodes it correctly.
func applyQuotingPolicy(value, key string) (mosaic.FieldValue, error) {
	// Rule 3: check for unrepresentable characters first.
	for _, r := range value {
		if r < 0x20 && r != '\n' && r != '\r' && r != '\t' {
			return mosaic.FieldValue{}, &agentformat.ArtifactError{
				Phase: "decode",
				Key:   key,
				Err:   agentformat.ErrUnrepresentableValue,
			}
		}
		if r == 0x7F {
			return mosaic.FieldValue{}, &agentformat.ArtifactError{
				Phase: "decode",
				Key:   key,
				Err:   agentformat.ErrUnrepresentableValue,
			}
		}
	}

	// Rule 1: unquoted only when no quoting is needed.
	if canEmitUnquoted(value) {
		return mosaic.ScalarValue(value, mosaic.QuotePlain), nil
	}

	// Rule 2: double-quoted with pre-escaping.
	escaped := preEscapeForDocformat(value)
	// Wrap in "..." and use QuotePlain so docformat emits it verbatim.
	return mosaic.ScalarValue(`"`+escaped+`"`, mosaic.QuotePlain), nil
}

// canEmitUnquoted reports whether value can safely be emitted as a YAML
// unquoted (plain) scalar. Returns false when the value:
//   - is the empty string
//   - has a leading or trailing space
//   - has a leading character that is unsafe for YAML plain scalars:
//     '#' (inline comment), '"' (double-quoted scalar -- docformat.Parse
//     would strip the outer quotes causing silent data corruption), or any
//     YAML block/flow indicator or sigil: ' | > & * ! ` @ %
//   - contains ": " (the YAML mapping separator)
//   - contains " #" (would start a YAML inline comment)
//   - contains any control character (including newline, CR, tab)
//   - has a spelling that isReservedSpelling considers reserved
func canEmitUnquoted(s string) bool {
	if len(s) == 0 {
		return false
	}
	if isReservedSpelling(s) {
		return false
	}
	// Leading characters that are unsafe for YAML plain scalars.
	// '#' starts an inline comment. '"' is especially dangerous: docformat.Parse
	// treats a token starting with '"' as a double-quoted scalar and strips the
	// surrounding quotes, causing silent data corruption on re-parse. The
	// remaining characters are YAML block/flow indicators and sigils that YAML
	// parsers handle specially: single-quote begins a single-quoted scalar, '|'
	// and '>' are block-scalar indicators, '&' is an anchor sigil, '*' is an
	// alias sigil, '!' is a tag indicator, and '%', '@', '`' are reserved.
	switch s[0] {
	case '#', '"', '\'', '|', '>', '&', '*', '!', '`', '@', '%':
		return false
	}
	if s[0] == ' ' || s[len(s)-1] == ' ' {
		return false
	}
	if strings.Contains(s, ": ") {
		return false
	}
	if strings.Contains(s, " #") {
		return false
	}
	for i := 0; i < len(s); i++ {
		b := s[i]
		if b < 0x20 || b == 0x7F {
			return false
		}
	}
	return true
}

// preEscapeForDocformat escapes a string to the five sequences that the
// docformat parse side's unescapeDoubleQuoted understands: \", \\, \n, \r, \t.
// The result is the content to place inside "..." in the YAML scalar.
func preEscapeForDocformat(s string) string {
	var sb strings.Builder
	sb.Grow(len(s) + 8)
	for _, r := range s {
		switch r {
		case '\\':
			sb.WriteString(`\\`)
		case '"':
			sb.WriteString(`\"`)
		case '\n':
			sb.WriteString(`\n`)
		case '\r':
			sb.WriteString(`\r`)
		case '\t':
			sb.WriteString(`\t`)
		default:
			sb.WriteRune(r)
		}
	}
	return sb.String()
}

// ---------------------------------------------------------------------------
// isReservedSpelling
// ---------------------------------------------------------------------------

// isReservedSpelling reports whether s is a YAML-reserved or TOML-reserved
// scalar spelling. A plain-emitted scalar is re-read by two different parsers
// on the way back out, so membership in either language's reserved set forces
// double quoting.
//
// The corpus is CLOSED: it is an enumerated set of patterns, not delegated to a
// library. The YAML half is matched case-insensitively; the TOML half is
// case-sensitive where stated.
func isReservedSpelling(s string) bool {
	if s == "" {
		return true
	}

	lower := strings.ToLower(s)

	// YAML keywords (case-insensitive).
	switch lower {
	case "true", "false", "null", "~",
		"on", "off", "yes", "no", "y", "n",
		".inf", "-.inf", "+.inf", ".nan":
		return true
	}

	// TOML-specific float/infinity/NaN keywords (case-sensitive).
	switch s {
	case "inf", "+inf", "-inf", "nan", "+nan", "-nan":
		return true
	}

	// Numeric patterns shared by YAML and TOML.
	if isReservedNumeric(s) {
		return true
	}

	// TOML date, time and date-time patterns.
	if isReservedDateTime(s) {
		return true
	}

	return false
}

// Compiled regular expressions for reserved numeric and date/time spellings.
// These are package-level to avoid recompilation on every call.
var (
	// Decimal integer: optional leading sign, one or more digits with optional
	// underscore separators (TOML style). The first character after the sign must
	// be a digit; each underscore must be followed by at least one digit, so
	// trailing underscores and double-underscores are not matched.
	reDecimalInt = regexp.MustCompile(`^[+-]?[0-9](_?[0-9]+)*$`)

	// Hex integer: optional leading sign, 0x/0X prefix, one or more hex digits
	// with optional underscore separators.
	reHexInt = regexp.MustCompile(`^[+-]?0[xX][0-9a-fA-F][0-9a-fA-F_]*$`)

	// Octal integer: optional leading sign, 0o/0O prefix.
	reOctalInt = regexp.MustCompile(`^[+-]?0[oO][0-7][0-7_]*$`)

	// Binary integer: optional leading sign, 0b/0B prefix.
	reBinaryInt = regexp.MustCompile(`^[+-]?0[bB][01][01_]*$`)

	// Float: covers the forms that appear in the corpus.
	// Alternatives:
	//   1. digits.digits with optional exponent  (1.5, 1.0, 1_000.5)
	//   2. .digits with optional exponent        (.5)
	//   3. digits. (trailing dot)                (1.)
	//   4. digits with exponent, no dot          (1e3, 1E3, 1_000e3)
	reFloat = regexp.MustCompile(
		`^[+-]?` +
			`(` +
			`[0-9][0-9_]*\.[0-9_]*([eE][+-]?[0-9]+)?` + // 1.x or 1.xe3
			`|` +
			`\.[0-9][0-9_]*([eE][+-]?[0-9]+)?` + // .5
			`|` +
			`[0-9][0-9_]*\.` + // 1.
			`|` +
			`[0-9][0-9_]*[eE][+-]?[0-9]+` + // 1e3
			`)$`,
	)

	// TOML local date: YYYY-MM-DD
	reDate = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

	// TOML local time: HH:MM:SS with optional fractional seconds
	reTime = regexp.MustCompile(`^\d{2}:\d{2}:\d{2}(\.\d+)?$`)

	// TOML date-time (local or offset): YYYY-MM-DDxHH:MM:SS... where x is T or space
	reDateTime = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}[T ]\d{2}:\d{2}:\d{2}`)
)

// isReservedNumeric reports whether s is a numeric spelling reserved by YAML or TOML.
func isReservedNumeric(s string) bool {
	return reDecimalInt.MatchString(s) ||
		reHexInt.MatchString(s) ||
		reOctalInt.MatchString(s) ||
		reBinaryInt.MatchString(s) ||
		reFloat.MatchString(s)
}

// isReservedDateTime reports whether s is a TOML date, time or date-time spelling.
func isReservedDateTime(s string) bool {
	return reDate.MatchString(s) ||
		reTime.MatchString(s) ||
		reDateTime.MatchString(s)
}
