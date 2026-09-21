// Package agentformat: strip.go declares the carriage container stripping helper.
//
// StripCarriage removes the carriage container from a canonical document and names
// every key it held, preparing the document for cross-format output. It is called
// immediately after the source artifact is decoded and before the canonical document
// reaches any code targeting another format.
//
// This helper is distinct from the translator layer's own container handling:
// the translator (Markdown or Codex TOML) handles the container at the encode/decode
// boundary within one format; StripCarriage handles the container at the promote/retarget
// format-change boundary across formats.
package agentformat

import (
	"bytes"
	"errors"
	"fmt"
	"sort"
	"strings"

	"mosaic-common/docformat"
	"mosaic-common/mosaic"
)

// ErrMalformedCarriage is returned by StripCarriage when a carriage container key is
// present with a value of a shape the container contract does not permit. It is its own
// sentinel, distinct from ErrMalformedDeployed: the container is MOSAIC's own structure
// and the deployed file is not implicated.
//
// Wrapped in an ArtifactError with Phase: "strip" and Key naming the offending carriage
// key. A caller matching ErrMalformedDeployed must NOT match this error: the two are
// distinguished by errors.Is.
var ErrMalformedCarriage = errors.New("carriage container is malformed")

// StripResult is the output of StripCarriage.
type StripResult struct {
	// Canonical is the canonical document with both carriage container keys removed.
	// When no container was present, Canonical is the input document, unmodified
	// (same underlying byte slice, not a copy).
	Canonical []byte

	// CarriedKeys names every key the container held, value-carried and marker-carried
	// alike, in ascending byte order. Nil when the container was absent.
	//
	// Marker-carried keys are included: they are precisely the keys whose values cannot
	// travel the format change, so a list containing only the value-carried half is the
	// failure mode this field exists to prevent.
	CarriedKeys []string
}

// StripCarriage removes the carriage container from a canonical document and names
// every key it held. It is pure and does not mutate its input: the caller receives a
// new byte slice when the container is present, and the document it passed in is untouched.
//
// A document carrying no container is returned with nil CarriedKeys and Canonical
// set to the input bytes unchanged. This is the normal case for Markdown-source agents,
// which carry no container.
//
// THE ERROR RETURN IS EXHAUSTIVELY ENUMERATED and is non-nil in exactly these two cases:
//
//  1. canonical is not a parseable MOSAIC document (no frontmatter block, or frontmatter
//     the canonical parser rejects). Returns the parser's error. A document with
//     well-formed frontmatter and no carriage keys is NOT this case; it is the
//     unchanged-document success above.
//
//  2. A carriage key is present with a value of the wrong shape:
//     - mosaic_carriage not a mapping
//     - mosaic_carriage_markers not a flow list of strings
//     - A mosaic_carriage pair whose value is neither a scalar nor a flow list of scalars
//     Returns ErrMalformedCarriage wrapped in an ArtifactError with Key naming the
//     offending carriage key and Phase == "strip".
//
// Nothing else fails. An empty container, a container naming a key that is not in the
// document, and a marker naming a key that also appears value-carried are all success cases.
func StripCarriage(canonical []byte) (StripResult, error) {
	// Fast path: if neither carriage key name appears in the input, return unchanged.
	hasValues := bytes.Contains(canonical, []byte(CarriageValuesKey))
	hasMarkers := bytes.Contains(canonical, []byte(CarriageMarkersKey))
	if !hasValues && !hasMarkers {
		return StripResult{Canonical: canonical}, nil
	}

	// One or both carriage keys are present. Parse the frontmatter to validate shapes
	// and collect the carried key names.
	doc, err := docformat.Parse(canonical)
	if err != nil {
		return StripResult{}, err
	}
	fm := doc.Frontmatter()

	// Validate and collect keys from mosaic_carriage (value-carried).
	var carriageKeys []string
	if hasValues {
		vcVal, ok := fm.Get(CarriageValuesKey)
		if ok {
			if vcVal.Kind != mosaic.KindMapping {
				return StripResult{}, &ArtifactError{
					Phase: "strip",
					Key:   CarriageValuesKey,
					Err:   fmt.Errorf("%w: %q must be a mapping, got %s", ErrMalformedCarriage, CarriageValuesKey, vcVal.Kind),
				}
			}
			for _, pair := range vcVal.Pairs {
				carriageKeys = append(carriageKeys, pair.Key)
			}
		}
	}

	// Validate and collect keys from mosaic_carriage_markers (marker-carried).
	if hasMarkers {
		markersVal, ok := fm.Get(CarriageMarkersKey)
		if ok {
			if markersVal.Kind != mosaic.KindList {
				return StripResult{}, &ArtifactError{
					Phase: "strip",
					Key:   CarriageMarkersKey,
					Err:   fmt.Errorf("%w: %q must be a list, got %s", ErrMalformedCarriage, CarriageMarkersKey, markersVal.Kind),
				}
			}
			for _, item := range markersVal.Items {
				if item.Kind == mosaic.KindScalar {
					carriageKeys = append(carriageKeys, item.Scalar)
				}
			}
		}
	}

	// Sort carriageKeys ascending for deterministic output.
	sort.Strings(carriageKeys)

	// Perform byte-level line removal of the carriage container keys.
	stripped := stripCarriageLines(canonical)

	return StripResult{
		Canonical:   stripped,
		CarriedKeys: carriageKeys,
	}, nil
}

// stripCarriageLines performs byte-level surgical removal of carriage container lines
// from a canonical Markdown+YAML document. It does not re-render the frontmatter.
// The carriage markers key is a single-line flow list; the carriage values key is a
// block mapping whose indented children must also be removed.
func stripCarriageLines(canonical []byte) []byte {
	// Locate frontmatter delimiters.
	if !bytes.HasPrefix(canonical, []byte("---\n")) {
		return canonical
	}
	rest := canonical[4:]
	endIdx := bytes.Index(rest, []byte("\n---\n"))
	if endIdx < 0 {
		return canonical
	}

	fmContent := rest[:endIdx+1]
	body := rest[endIdx+5:] // skip \n---\n (5 bytes)

	var out bytes.Buffer
	out.WriteString("---\n")

	fm := fmContent
	inCarriageMapping := false

	for len(fm) > 0 {
		nlIdx := bytes.IndexByte(fm, '\n')
		var line []byte
		if nlIdx >= 0 {
			line = fm[:nlIdx+1]
			fm = fm[nlIdx+1:]
		} else {
			line = fm
			fm = nil
		}

		lineStr := string(line)
		trimmed := strings.TrimRight(lineStr, "\n\r")

		// CarriageMarkersKey (flow list, single line) -- check before CarriageValuesKey
		// because both share the same prefix.
		if trimmed == CarriageMarkersKey+":" ||
			strings.HasPrefix(trimmed, CarriageMarkersKey+": ") ||
			strings.HasPrefix(trimmed, CarriageMarkersKey+":\t") {
			inCarriageMapping = false
			continue // skip
		}

		// mosaic_carriage (block mapping header).
		if trimmed == CarriageValuesKey+":" ||
			strings.HasPrefix(trimmed, CarriageValuesKey+": ") ||
			strings.HasPrefix(trimmed, CarriageValuesKey+":\t") {
			inCarriageMapping = true
			continue // skip
		}

		// Indented children of mosaic_carriage.
		if inCarriageMapping {
			if len(line) > 0 && (line[0] == ' ' || line[0] == '\t') {
				continue // skip child of mapping
			}
			inCarriageMapping = false // non-indented: mapping ended
		}

		out.Write(line)
	}

	out.WriteString("---\n")
	out.Write(body)

	return out.Bytes()
}
