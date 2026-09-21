package codextoml

import (
	"bytes"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/goccy/go-yaml"
	"github.com/pelletier/go-toml/v2"

	"mosaic-common/docformat"

	"mosaic-deploy/internal/agentformat"
	"mosaic-deploy/internal/agentformat/formatid"
)

func init() {
	// Register under the CodexTOML format ID. Any package that imports codextoml
	// (directly or through the agentformat/all wiring package) makes Lookup resolve.
	agentformat.Register(formatid.CodexTOML, codexTranslator{})
}

// codexTranslator implements the Translator interface for the Codex TOML agent
// file format. It converts between a canonical MOSAIC Markdown document with YAML
// frontmatter and a Codex-compatible TOML file.
type codexTranslator struct{}

// Encode converts a canonical MOSAIC Markdown document to Codex TOML bytes.
//
// The three-row classification rule applied to every canonical frontmatter key:
//   - In the Codex-native emitted set (emittedKeys): emitted as a native TOML key.
//   - Deployed stamp name (isStampKey): written into the provenance comment block.
//   - Anything else (including generic vocabulary Codex never emits, and a stamp's
//     Legacy name): foreign to Codex, dropped and reported as EntryDroppedForeignKey.
//
// Deploy-path normalisations (all suppressed when ctx.RefreshMode is true):
//   - Name forcing: name is ctx.AgentKey verbatim; divergent name in canonical
//     frontmatter loses and is reported as EntryOverriddenName.
//   - Description fallback: blank or absent description becomes
//     "MOSAIC agent " + ctx.AgentKey, reported as EntryAppliedFallback.
//   - Model omission: empty or absent model omits the TOML key silently.
//   - sandbox_mode fallback: absent sandbox_mode becomes "read-only", reported as
//     EntryAppliedFallback. A Codex file with no sandbox_mode inherits the parent
//     session's mode; transform.resolveTools returns early without calling
//     Module.Tools when the source has no tools key, so this encoder is the only
//     place that can guarantee the key is present.
//   - Foreign-key drop (described above).
//
// ErrEmptyBody is returned (not suppressed by RefreshMode) when the body is empty.
func (codexTranslator) Encode(canonical []byte, ctx agentformat.ArtifactContext) ([]byte, agentformat.Report, error) {
	// --- Op enforcement (Codex-specific; Markdown ignores Op). ---
	//
	// Rule a: OpUnspecified is always invalid.
	// Rule b: OpCreate with non-nil PriorDeployed contradicts the operation.
	// Rule c: OpUpdate with nil PriorDeployed violates the prior-bytes contract.
	if ctx.Op == agentformat.OpUnspecified {
		return nil, agentformat.Report{}, &agentformat.ArtifactError{
			Phase: "encode",
			Err:   agentformat.ErrUnspecifiedOperation,
		}
	}
	if ctx.Op == agentformat.OpCreate && ctx.PriorDeployed != nil {
		return nil, agentformat.Report{}, &agentformat.ArtifactError{
			Phase: "encode",
			Err:   agentformat.ErrUnspecifiedOperation,
		}
	}
	if ctx.Op == agentformat.OpUpdate && ctx.PriorDeployed == nil {
		return nil, agentformat.Report{}, &agentformat.ArtifactError{
			Phase: "encode",
			Err:   agentformat.ErrMissingPriorBytes,
		}
	}

	// Split canonical document into frontmatter bytes and body bytes.
	fmBytes, body, err := docformat.SplitFrontmatter(canonical)
	if err != nil {
		return nil, agentformat.Report{}, &agentformat.ArtifactError{
			Phase: "encode",
			Err:   fmt.Errorf("split frontmatter: %w", err),
		}
	}

	// Empty body fails loudly regardless of RefreshMode. No instructions fallback
	// can be invented, and a Codex file without developer_instructions is not runnable.
	if len(body) == 0 {
		return nil, agentformat.Report{}, &agentformat.ArtifactError{
			Phase: "encode",
			Err:   agentformat.ErrEmptyBody,
		}
	}

	// Parse frontmatter YAML into a key-value map.
	fm := make(map[string]interface{})
	if len(fmBytes) > 0 {
		if yamlErr := yaml.Unmarshal(fmBytes, &fm); yamlErr != nil {
			return nil, agentformat.Report{}, &agentformat.ArtifactError{
				Phase: "encode",
				Err:   fmt.Errorf("parse frontmatter: %w", yamlErr),
			}
		}
	}

	var report agentformat.Report
	stamps := make(map[string]string)

	// In refresh mode, foreign keys are not dropped but emitted as native TOML pairs.
	// These slices collect them in iteration order; they are sorted before emission.
	var refreshForeignKeys []string
	refreshForeignValMap := make(map[string]interface{})

	// --- Classify every frontmatter key by the three-row table. ---
	//
	// Row 1: in emittedKeys  → handled below during normalisation.
	// Row 2: isStampKey      → stamp value collected for the comment block.
	// Row 3: carriage key    → consumed by the carriage path below; NOT foreign.
	// Row 4: everything else → foreign; drop and (in deploy mode) report;
	//                          in refresh mode, collect for native TOML emission.
	for key, val := range fm {
		if _, inEmitted := emittedKeys[key]; inEmitted {
			continue // emitted keys are handled in the normalisation block below
		}
		if isStampKey(key) {
			stamps[key] = fmValStr(val)
			continue
		}
		// Carriage container keys are consumed by the carriage path; they must
		// never be reported as foreign and must never appear in TOML output.
		if agentformat.IsCarriageKey(key) {
			continue
		}
		// Foreign key: in deploy mode, dropped and reported.
		// In refresh mode, collected for emission as a native TOML key-value pair.
		if !ctx.RefreshMode {
			report.Entries = append(report.Entries, agentformat.ReportEntry{
				Kind:   agentformat.EntryDroppedForeignKey,
				Key:    key,
				Detail: fmValStr(val),
				Reason: "key is not in the Codex-native emitted set and is not a MOSAIC stamp deployed name; Codex does not accept this key",
			})
		} else {
			refreshForeignKeys = append(refreshForeignKeys, key)
			refreshForeignValMap[key] = val
		}
	}

	// --- Extract carriage containers from the canonical frontmatter. ---

	// Value-carried keys (mosaic_carriage): a mapping of user key → YAML-decoded value.
	// goccy/go-yaml decodes the mapping as map[string]interface{}, where the value
	// type encodes the TOML type marker: string → TOML string; uint64/int64 → TOML
	// integer; float64 → TOML float; bool → TOML boolean.
	var carriageValueKeys []string
	var carriageValueMap map[string]interface{}
	if raw, ok := fm[agentformat.CarriageValuesKey]; ok {
		if m, ok2 := raw.(map[string]interface{}); ok2 && len(m) > 0 {
			carriageValueMap = m
			for k := range m {
				carriageValueKeys = append(carriageValueKeys, k)
			}
			sort.Strings(carriageValueKeys)
		}
	}

	// Marker-carried key names (mosaic_carriage_markers): a flow list of
	// double-quoted key names. goccy/go-yaml decodes the list as []interface{}.
	var markerNames []string
	if raw, ok := fm[agentformat.CarriageMarkersKey]; ok {
		if items, ok2 := raw.([]interface{}); ok2 {
			for _, item := range items {
				if s, ok3 := item.(string); ok3 {
					markerNames = append(markerNames, s)
				}
			}
		}
	}

	// Markers require the prior-bytes channel; OpCreate cannot supply it.
	if len(markerNames) > 0 && ctx.Op != agentformat.OpUpdate {
		return nil, agentformat.Report{}, &agentformat.ArtifactError{
			Phase: "encode",
			Err: fmt.Errorf("%w: canonical contains marker-carried keys but Op is not OpUpdate (prior bytes are required to recover markers)",
				agentformat.ErrMarkerUnrecoverable),
		}
	}

	// --- Deploy-path normalisations (all suppressed when RefreshMode is true). ---

	// Name: ctx.AgentKey is the sole authority.
	canonicalName := fmValStr(fm["name"])
	emitName := ctx.AgentKey
	if !ctx.RefreshMode {
		// In deploy mode: force the agent key; report if the frontmatter had a
		// divergent name that is being overridden.
		if canonicalName != "" && canonicalName != ctx.AgentKey {
			report.Entries = append(report.Entries, agentformat.ReportEntry{
				Kind:   agentformat.EntryOverriddenName,
				Key:    "name",
				Detail: canonicalName,
				Reason: "ctx.AgentKey is the sole authority for the emitted name (AD-14); canonical frontmatter name diverged from agent key",
			})
		}
	} else {
		// In refresh mode: use the canonical name if present; fall back to AgentKey.
		if canonicalName != "" {
			emitName = canonicalName
		}
	}

	// Description: absent or YAML-null becomes a deterministic fallback in deploy mode.
	// The condition checks the raw map value rather than the string form so that an
	// explicitly-present empty string ("description: """) is preserved and not replaced
	// by the fallback. A key that is absent or whose value is YAML null (description: )
	// gives nil here and triggers the fallback; anything else -- including "" and
	// whitespace-only strings -- is a deliberate value and is left untouched.
	description := fmValStr(fm["description"])
	if !ctx.RefreshMode {
		if fm["description"] == nil {
			description = "MOSAIC agent " + ctx.AgentKey
			report.Entries = append(report.Entries, agentformat.ReportEntry{
				Kind:   agentformat.EntryAppliedFallback,
				Key:    "description",
				Detail: description,
				Reason: "canonical frontmatter had no description; applied deterministic fallback",
			})
		}
	}

	// Model: emit only when non-empty; omission is silent (not reported).
	model := strings.TrimSpace(fmValStr(fm["model"]))

	// sandbox_mode: absent becomes "read-only" in deploy mode (fail-closed). A Codex
	// file with no sandbox_mode inherits the parent session's permissive mode, which
	// is the hazard this fallback closes. An explicitly supplied value is never
	// overridden.
	sandboxMode := fmValStr(fm["sandbox_mode"])
	if !ctx.RefreshMode {
		if sandboxMode == "" {
			sandboxMode = "read-only"
			report.Entries = append(report.Entries, agentformat.ReportEntry{
				Kind:   agentformat.EntryAppliedFallback,
				Key:    "sandbox_mode",
				Detail: sandboxMode,
				Reason: "canonical input carried no sandbox_mode; a Codex file with no sandbox_mode inherits the parent session's mode; emitting read-only as a fail-closed fallback",
			})
		}
	}

	// --- Build the TOML output. ---
	var buf bytes.Buffer

	// Stamp comment block at the top (in agentfields.All() order).
	block := writeStampBlock(stamps)
	buf.Write(block)

	// Foreign header comments (AC4.9a): re-emit non-stamp leading comments from
	// the prior bytes after the stamp block and before the first TOML key.
	// This preserves user annotations (e.g. TODO notes) across redeploys.
	if ctx.Op == agentformat.OpUpdate && len(ctx.PriorDeployed) > 0 {
		foreignComments := extractForeignHeaderComments(ctx.PriorDeployed)
		buf.Write(foreignComments)
	}

	// TOML key-value pairs (emitted set, fixed order).
	buf.WriteString("name = ")
	writeTomlBasicString(&buf, emitName)
	buf.WriteByte('\n')

	buf.WriteString("description = ")
	writeTomlBasicString(&buf, description)
	buf.WriteByte('\n')

	if model != "" {
		buf.WriteString("model = ")
		writeTomlBasicString(&buf, model)
		buf.WriteByte('\n')
	}

	if sandboxMode != "" {
		buf.WriteString("sandbox_mode = ")
		writeTomlBasicString(&buf, sandboxMode)
		buf.WriteByte('\n')
	}

	// Refresh-mode foreign keys: emit as native TOML key-value pairs.
	// In deploy mode these keys are dropped and reported; in refresh mode the drop
	// is suppressed and the keys must survive verbatim in the encoded output.
	if len(refreshForeignKeys) > 0 {
		sort.Strings(refreshForeignKeys)
		for _, k := range refreshForeignKeys {
			v := refreshForeignValMap[k]
			buf.WriteString(k)
			buf.WriteString(" = ")
			if err2 := writeForeignKeyValue(&buf, v); err2 != nil {
				return nil, agentformat.Report{}, &agentformat.ArtifactError{
					Phase: "encode",
					Key:   k,
					Err:   err2,
				}
			}
			buf.WriteByte('\n')
		}
	}

	// Value-carried user keys: emit each key as a native TOML key-value pair.
	// The type is recovered from the YAML-decoded value type: string → TOML string;
	// uint64/int64 → TOML integer; float64 → TOML float; bool → TOML boolean.
	// Scan prior bytes (OpUpdate) for attached comments so we can report them as
	// dropped (the comment cannot travel through value carriage).
	var priorScan []tomlLineRecord
	var priorInfos map[string]*tomlKeyInfo
	if ctx.Op == agentformat.OpUpdate && len(ctx.PriorDeployed) > 0 {
		priorScan = scanTomlLines(ctx.PriorDeployed)
		priorInfos = buildInfosFromScan(priorScan)
	}

	for _, k := range carriageValueKeys {
		v := carriageValueMap[k]
		buf.WriteString(k)
		buf.WriteString(" = ")
		if err2 := writeCarriageValue(&buf, v); err2 != nil {
			return nil, agentformat.Report{}, &agentformat.ArtifactError{
				Phase: "encode",
				Key:   k,
				Err:   err2,
			}
		}
		buf.WriteByte('\n')
		report.Entries = append(report.Entries, agentformat.ReportEntry{
			Kind: agentformat.EntryCarriedContainer,
			Key:  k,
		})
		// If the prior bytes had an attached comment for this value-carried key,
		// the comment cannot be preserved through value carriage (frontmatter
		// cannot carry TOML comments). Report the loss.
		if priorInfos != nil {
			if info, ok := priorInfos[k]; ok && info.hasAttachedComment {
				commentText := attachedCommentText(ctx.PriorDeployed, k, priorScan)
				report.Entries = append(report.Entries, agentformat.ReportEntry{
					Kind:   agentformat.EntryDroppedComment,
					Key:    k,
					Detail: commentText,
					Reason: "value-carried key had an attached TOML comment in prior bytes; the comment cannot travel through value carriage and was dropped",
				})
			}
		}
	}

	// Region-split emission order (AC4.11 / CD-3):
	// Marker spans are classified by whether their first non-comment line is a TOML
	// table or array-of-tables header (starts with '['):
	//   - Non-header regions: emitted before developer_instructions so that plain
	//     key assignments and dotted keys appear at TOML top level.
	//   - Header regions: emitted after developer_instructions because a table header
	//     opens a section context; any keys that follow it (including
	//     developer_instructions) would be placed inside that table.
	// Both groups are emitted in ascending marker name order; within each span,
	// lines are re-emitted in their original source order.
	type markerEntry struct {
		name string
		span []byte
	}
	var nonHeaderMarkers []markerEntry
	var headerMarkers []markerEntry

	if ctx.Op == agentformat.OpUpdate && len(markerNames) > 0 {
		if priorScan == nil {
			priorScan = scanTomlLines(ctx.PriorDeployed)
		}
		for _, markerName := range markerNames {
			// Control characters in marker key names cannot be re-emitted as valid
			// TOML quoted keys.
			if hasControlChar(markerName) {
				return nil, agentformat.Report{}, &agentformat.ArtifactError{
					Phase: "encode",
					Key:   markerName,
					Err: fmt.Errorf("%w: marker key contains a control character and cannot be re-emitted",
						agentformat.ErrMarkerUnrecoverable),
				}
			}
			// Resurrection guard: a marker naming a MOSAIC-owned key must never
			// restore that key's prior value. MOSAIC's current value already appears
			// in the output; emit EntryRefusedMarker and skip the span.
			if isMosaicOwned(markerName) {
				report.Entries = append(report.Entries, agentformat.ReportEntry{
					Kind:   agentformat.EntryRefusedMarker,
					Key:    markerName,
					Reason: "marker names a MOSAIC-owned key; the marker was refused to prevent restoring a prior value",
				})
				continue
			}
			// Extract the span from prior bytes.
			span, spanErr := extractSpanBytes(ctx.PriorDeployed, markerName, priorScan)
			if spanErr != nil {
				return nil, agentformat.Report{}, &agentformat.ArtifactError{
					Phase: "encode",
					Key:   markerName,
					Err:   spanErr,
				}
			}
			if spanIsHeaderRegion(span) {
				headerMarkers = append(headerMarkers, markerEntry{markerName, span})
			} else {
				nonHeaderMarkers = append(nonHeaderMarkers, markerEntry{markerName, span})
			}
		}
		sort.Slice(nonHeaderMarkers, func(i, j int) bool { return nonHeaderMarkers[i].name < nonHeaderMarkers[j].name })
		sort.Slice(headerMarkers, func(i, j int) bool { return headerMarkers[i].name < headerMarkers[j].name })
	}

	// Emit non-header marker spans before developer_instructions.
	for _, me := range nonHeaderMarkers {
		buf.Write(me.span)
		if len(me.span) > 0 && me.span[len(me.span)-1] != '\n' {
			buf.WriteByte('\n')
		}
	}

	// developer_instructions: use the body bytes, selecting the TOML string form
	// deterministically per the body's content.
	buf.WriteString("developer_instructions = ")
	writeTomlBodyString(&buf, body)
	buf.WriteByte('\n')

	// Emit header marker spans after developer_instructions.
	for _, me := range headerMarkers {
		buf.Write(me.span)
		if len(me.span) > 0 && me.span[len(me.span)-1] != '\n' {
			buf.WriteByte('\n')
		}
	}

	// Verification pass: re-parse the emitted TOML to detect duplicate keys that
	// the marker path may have introduced (e.g. a key appearing in both containers).
	// A duplicate key causes an ErrMarkerUnrecoverable rather than a corrupt file.
	output := buf.Bytes()
	var checkMap map[string]interface{}
	if tomlErr := toml.Unmarshal(output, &checkMap); tomlErr != nil {
		return nil, agentformat.Report{}, &agentformat.ArtifactError{
			Phase: "encode",
			Err: fmt.Errorf("%w: verification pass detected invalid TOML: %v",
				agentformat.ErrMarkerUnrecoverable, tomlErr),
		}
	}

	return output, report, nil
}

// writeCarriageValue writes the TOML value representation for a value recovered
// from the mosaic_carriage YAML mapping. The Go type (from goccy/go-yaml) encodes
// the TOML type: string → basic string; uint64/int64 → integer; float64 → float;
// bool → boolean.
func writeCarriageValue(buf *bytes.Buffer, v interface{}) error {
	switch vv := v.(type) {
	case string:
		writeTomlBasicString(buf, vv)
	case uint64:
		buf.WriteString(strconv.FormatUint(vv, 10))
	case int64:
		buf.WriteString(strconv.FormatInt(vv, 10))
	case float64:
		// Note: decode.go's default branch escalates all float64 values to the
		// mosaic_carriage_markers channel, so this branch is not reachable from
		// the normal decode→encode path. It is retained as a safety net for callers
		// that construct a carriage mapping directly (e.g. tests or future paths).
		buf.WriteString(strconv.FormatFloat(vv, 'g', -1, 64))
	case bool:
		if vv {
			buf.WriteString("true")
		} else {
			buf.WriteString("false")
		}
	default:
		return fmt.Errorf("unsupported carriage value type %T", v)
	}
	return nil
}

// writeForeignKeyValue writes the TOML value representation for a foreign key recovered
// from canonical YAML frontmatter in refresh mode. It handles the Go types that
// goccy/go-yaml produces when decoding a YAML scalar: string, bool, int, int64,
// uint64, float64. Arrays and maps fall back to a quoted string representation.
func writeForeignKeyValue(buf *bytes.Buffer, v interface{}) error {
	switch vv := v.(type) {
	case string:
		writeTomlBasicString(buf, vv)
	case bool:
		if vv {
			buf.WriteString("true")
		} else {
			buf.WriteString("false")
		}
	case int:
		buf.WriteString(strconv.Itoa(vv))
	case int64:
		buf.WriteString(strconv.FormatInt(vv, 10))
	case uint64:
		buf.WriteString(strconv.FormatUint(vv, 10))
	case float64:
		buf.WriteString(strconv.FormatFloat(vv, 'g', -1, 64))
	default:
		// Complex type (map, slice, etc.): fall back to a quoted string rendering.
		writeTomlBasicString(buf, fmValStr(v))
	}
	return nil
}

// fmValStr converts a YAML-decoded value to its string representation.
// For scalar strings it returns the string directly. For nil it returns "".
// For other types (lists, mappings, booleans, etc.) it returns a fmt.Sprintf
// rendering, which is always non-empty and suitable for report Detail fields.
func fmValStr(val interface{}) string {
	if val == nil {
		return ""
	}
	switch v := val.(type) {
	case string:
		return v
	default:
		return fmt.Sprintf("%v", v)
	}
}

// writeTomlBasicString writes s as a TOML basic string (double-quoted) to buf,
// escaping characters that require escaping: backslash, double quote, newline,
// carriage return, tab, and other control characters.
func writeTomlBasicString(buf *bytes.Buffer, s string) {
	buf.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			buf.WriteString(`\"`)
		case '\\':
			buf.WriteString(`\\`)
		case '\n':
			buf.WriteString(`\n`)
		case '\r':
			buf.WriteString(`\r`)
		case '\t':
			buf.WriteString(`\t`)
		default:
			if r < 0x20 || r == 0x7F {
				fmt.Fprintf(buf, `\u%04X`, r)
			} else {
				buf.WriteRune(r)
			}
		}
	}
	buf.WriteByte('"')
}

// writeTomlBodyString writes the body bytes as a TOML string for developer_instructions.
// It selects between two forms deterministically based on body content:
//
//  1. Multi-line basic string (""") when:
//     - body contains LF
//     - no CR in body
//     - no """ sequence in body
//     - no control chars other than LF and TAB
//     - body does not end with " or \
//     - body does not begin with LF
//
//  2. Single-line basic string with full escaping, in all other cases.
//
// The multi-line form is: """\n<escaped-body>""" where:
//   - The leading \n is stripped by TOML parsers (preserving the body)
//   - Backslashes in the body are escaped as \\ (TOML still processes escapes in
//     multi-line basic strings)
//   - Literal LF and TAB are left as-is (allowed in multi-line basic strings)
//   - The canUseMultilineBasic conditions ensure no other escaping is needed
//
// The single-line form escapes all special characters.
func writeTomlBodyString(buf *bytes.Buffer, body []byte) {
	if canUseMultilineBasic(body) {
		buf.WriteString(`"""`)
		buf.WriteByte('\n') // TOML trims exactly one leading newline after """
		writeMultilineBodyContent(buf, body)
		buf.WriteString(`"""`)
	} else {
		buf.WriteByte('"')
		writeEscapedBodyBytes(buf, body)
		buf.WriteByte('"')
	}
}

// writeMultilineBodyContent writes body content for a TOML multi-line basic string.
// Because canUseMultilineBasic guarantees: no CR, no control chars except LF/TAB,
// no triple-quote sequence, does not end with " or \, and does not begin with LF,
// the only escaping needed is backslash → \\ (TOML processes escapes in multi-line
// basic strings, so a bare \ would start an escape sequence).
// Literal LF and TAB are written as-is (both are allowed in multi-line basic strings).
func writeMultilineBodyContent(buf *bytes.Buffer, body []byte) {
	for i := 0; i < len(body); i++ {
		b := body[i]
		if b == '\\' {
			buf.WriteString(`\\`)
		} else {
			buf.WriteByte(b)
		}
	}
}

// canUseMultilineBasic reports whether body satisfies all conditions for encoding
// as a TOML multi-line basic string. The conditions are from the encode contract.
func canUseMultilineBasic(body []byte) bool {
	if len(body) == 0 {
		return false
	}
	// Must contain at least one LF.
	if !bytes.Contains(body, []byte{'\n'}) {
		return false
	}
	// Must not begin with LF (TOML strips exactly one newline after the opening """).
	if body[0] == '\n' {
		return false
	}
	// Must not end with " (would create """" at close, ambiguous in some parsers).
	// Must not end with \ (would be an escape sequence touching the closing """).
	last := body[len(body)-1]
	if last == '"' || last == '\\' {
		return false
	}
	// Scan body bytes for disqualifying characters.
	for i := 0; i < len(body); i++ {
		b := body[i]
		// CR is forbidden in multi-line basic strings.
		if b == '\r' {
			return false
		}
		// Triple-quote terminates the string early.
		if b == '"' && i+2 < len(body) && body[i+1] == '"' && body[i+2] == '"' {
			return false
		}
		// Control characters other than LF (0x0A) and TAB (0x09) are forbidden.
		if b < 0x20 && b != '\n' && b != '\t' {
			return false
		}
		if b == 0x7F {
			return false
		}
	}
	return true
}

// writeEscapedBodyBytes writes body as the content of a TOML single-line basic
// string, escaping all characters that require escaping. Non-ASCII UTF-8 sequences
// that are not control characters are emitted verbatim (TOML supports UTF-8).
func writeEscapedBodyBytes(buf *bytes.Buffer, body []byte) {
	for i := 0; i < len(body); {
		b := body[i]
		if b < 0x80 {
			// ASCII byte: handle escape cases.
			i++
			switch b {
			case '"':
				buf.WriteString(`\"`)
			case '\\':
				buf.WriteString(`\\`)
			case '\n':
				buf.WriteString(`\n`)
			case '\r':
				buf.WriteString(`\r`)
			case '\t':
				buf.WriteString(`\t`)
			default:
				if b < 0x20 || b == 0x7F {
					// Control character: use \uXXXX (4-digit Unicode escape).
					fmt.Fprintf(buf, `\u%04X`, b)
				} else {
					buf.WriteByte(b)
				}
			}
		} else {
			// Multi-byte UTF-8 sequence: emit verbatim if valid, else escape.
			r, size := utf8.DecodeRune(body[i:])
			if r == utf8.RuneError && size == 1 {
				// Invalid UTF-8 byte: encode as \uXXXX.
				fmt.Fprintf(buf, `\u%04X`, b)
				i++
			} else {
				// Valid non-ASCII rune: emit as UTF-8 bytes.
				buf.Write(body[i : i+size])
				i += size
			}
		}
	}
}
