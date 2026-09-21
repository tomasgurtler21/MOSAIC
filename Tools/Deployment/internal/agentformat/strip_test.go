package agentformat_test

// strip_test.go covers StripCarriage, the carriage container stripping helper.
//
// These tests constitute the complete T15.9 test suite, written before I15.4 delivers
// the implementation. They are in the RED phase: every test calls StripCarriage, which
// currently panics. The tests must compile successfully and fail when run.
//
// Verified behaviours:
//
// (a) Container removal: a document carrying both carriage key shapes (value-carried
//     and marker-carried) has both keys absent from the returned document, with no
//     residue of either key name.
//
// (b) Complete key inventory: every key the container held, marker-carried keys included,
//     appears in CarriedKeys. A result that lists only value-carried keys and omits
//     marker-carried ones is the failure mode this test exists to catch.
//
// (c) Unchanged remainder: owned keys, their values, and the body are byte-identical
//     between input and output. The input document the caller passed in is not mutated
//     in place.
//
// (d) No-container path: a document carrying no carriage container is returned unchanged
//     with nil CarriedKeys, producing no spurious report entry for Markdown-source agents.
//
// (e) Malformed container: a document whose mosaic_carriage key is not a mapping, or
//     whose mosaic_carriage_markers key is not a list, yields ErrMalformedCarriage
//     wrapped in an ArtifactError with Phase "strip" -- a distinct sentinel from
//     ErrMalformedDeployed, which is the deployed-file decode failure.

import (
	"bytes"
	"errors"
	"testing"

	"mosaic-common/docformat"
	"mosaic-common/mosaic"

	"mosaic-deploy/internal/agentformat"
)

// ---------------------------------------------------------------------------
// Fixture helpers
// ---------------------------------------------------------------------------

// stripTestOwnedKey is a distinctive frontmatter key that is not a carriage key and
// should survive stripping untouched.
const stripTestOwnedKey = "owned_setting"

// stripTestValueCarriedKey is the key carried via value carriage (mosaic_carriage).
const stripTestValueCarriedKey = "vc_user_key"

// stripTestMarkerCarriedKey is the key carried via marker carriage (mosaic_carriage_markers).
const stripTestMarkerCarriedKey = "mc_user_key"

// buildSyntheticDocWithBothKeyShapes constructs a canonical document that carries:
//   - An ordinary owned key (stripTestOwnedKey) with value "owned-value"
//   - A value-carried key in mosaic_carriage (stripTestValueCarriedKey)
//   - A marker-carried key name in mosaic_carriage_markers (stripTestMarkerCarriedKey)
//   - A non-trivial body
//
// The document is built through docformat.RenderFrontmatter to match the canonical
// render path used by the translator layer.
func buildSyntheticDocWithBothKeyShapes() []byte {
	valuePairs := []mosaic.FieldPair{
		{Key: stripTestValueCarriedKey, Value: mosaic.ScalarValue("vc-value", mosaic.QuoteDouble)},
	}
	markerItems := []mosaic.FieldValue{
		mosaic.ScalarValue(stripTestMarkerCarriedKey, mosaic.QuoteDouble),
	}
	fields := []mosaic.FrontmatterField{
		{Key: "name", Value: mosaic.ScalarValue("strip-test-agent", mosaic.QuotePlain)},
		{Key: "description", Value: mosaic.ScalarValue("StripCarriage test agent.", mosaic.QuotePlain)},
		{Key: stripTestOwnedKey, Value: mosaic.ScalarValue("owned-value", mosaic.QuotePlain)},
		{Key: agentformat.CarriageValuesKey, Value: mosaic.MappingValue(valuePairs)},
		{Key: agentformat.CarriageMarkersKey, Value: mosaic.ListValue(markerItems, mosaic.ListFlow)},
	}
	fmText := docformat.RenderFrontmatter(fields, "\n")
	return []byte("---\n" + fmText + "---\nBody text for StripCarriage test.\n")
}

// buildDocWithNoContainer constructs a canonical document with no carriage container:
// only ordinary frontmatter keys and a body.
func buildDocWithNoContainer() []byte {
	fields := []mosaic.FrontmatterField{
		{Key: "name", Value: mosaic.ScalarValue("no-container-agent", mosaic.QuotePlain)},
		{Key: "description", Value: mosaic.ScalarValue("No container.", mosaic.QuotePlain)},
		{Key: stripTestOwnedKey, Value: mosaic.ScalarValue("owned-value", mosaic.QuotePlain)},
	}
	fmText := docformat.RenderFrontmatter(fields, "\n")
	return []byte("---\n" + fmText + "---\nBody text.\n")
}

// buildDocWithMalformedCarriageValues constructs a canonical document where
// mosaic_carriage is a scalar (not a mapping), which is the malformed-container case.
func buildDocWithMalformedCarriageValues() []byte {
	fields := []mosaic.FrontmatterField{
		{Key: "name", Value: mosaic.ScalarValue("malformed-agent", mosaic.QuotePlain)},
		// mosaic_carriage present but as a scalar, not a mapping -- malformed.
		{Key: agentformat.CarriageValuesKey, Value: mosaic.ScalarValue("not-a-mapping", mosaic.QuotePlain)},
	}
	fmText := docformat.RenderFrontmatter(fields, "\n")
	return []byte("---\n" + fmText + "---\nBody.\n")
}

// buildDocWithMalformedCarriageMarkers constructs a canonical document where
// mosaic_carriage_markers is a scalar (not a list), which is the malformed-container case.
func buildDocWithMalformedCarriageMarkers() []byte {
	fields := []mosaic.FrontmatterField{
		{Key: "name", Value: mosaic.ScalarValue("malformed-markers-agent", mosaic.QuotePlain)},
		// mosaic_carriage_markers present but as a scalar, not a list -- malformed.
		{Key: agentformat.CarriageMarkersKey, Value: mosaic.ScalarValue("not-a-list", mosaic.QuotePlain)},
	}
	fmText := docformat.RenderFrontmatter(fields, "\n")
	return []byte("---\n" + fmText + "---\nBody.\n")
}

// sliceContains reports whether slice contains s.
func sliceContains(slice []string, s string) bool {
	for _, v := range slice {
		if v == s {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// T15.9(a) -- Container is gone from the returned document
// ---------------------------------------------------------------------------

// TestStripCarriage_ContainerRemoved_NoResidueOfEitherKeyShape verifies that
// StripCarriage removes both carriage keys from the returned document when the input
// carries both mosaic_carriage (value-carried) and mosaic_carriage_markers
// (marker-carried). Neither key name may appear in the returned document in any form.
func TestStripCarriage_ContainerRemoved_NoResidueOfEitherKeyShape(t *testing.T) {
	input := buildSyntheticDocWithBothKeyShapes()

	result, err := agentformat.StripCarriage(input)
	if err != nil {
		t.Fatalf("StripCarriage returned unexpected error: %v", err)
	}

	doc, parseErr := docformat.Parse(result.Canonical)
	if parseErr != nil {
		t.Fatalf("result.Canonical does not parse: %v", parseErr)
	}
	fm := doc.Frontmatter()

	if _, ok := fm.Get(agentformat.CarriageValuesKey); ok {
		t.Errorf("result.Canonical still contains %q; the container must be fully removed", agentformat.CarriageValuesKey)
	}
	if _, ok := fm.Get(agentformat.CarriageMarkersKey); ok {
		t.Errorf("result.Canonical still contains %q; the container must be fully removed", agentformat.CarriageMarkersKey)
	}

	// The raw bytes must not contain either carriage key as a YAML key pattern either.
	for _, carriageKey := range agentformat.CarriageKeys() {
		if bytes.Contains(result.Canonical, []byte(carriageKey+":")) {
			t.Errorf("result.Canonical contains raw bytes %q: full removal is required", carriageKey+":")
		}
	}
}

// ---------------------------------------------------------------------------
// T15.9(b) -- Every key named in CarriedKeys, marker-carried included
// ---------------------------------------------------------------------------

// TestStripCarriage_CarriedKeys_IncludesValueCarriedKey verifies that the value-carried
// key from mosaic_carriage appears in StripResult.CarriedKeys.
func TestStripCarriage_CarriedKeys_IncludesValueCarriedKey(t *testing.T) {
	input := buildSyntheticDocWithBothKeyShapes()

	result, err := agentformat.StripCarriage(input)
	if err != nil {
		t.Fatalf("StripCarriage returned unexpected error: %v", err)
	}

	if !sliceContains(result.CarriedKeys, stripTestValueCarriedKey) {
		t.Errorf("CarriedKeys does not contain value-carried key %q; got: %v",
			stripTestValueCarriedKey, result.CarriedKeys)
	}
}

// TestStripCarriage_CarriedKeys_IncludesMarkerCarriedKey verifies that the marker-carried
// key from mosaic_carriage_markers appears in StripResult.CarriedKeys.
//
// This is the failure-mode assertion: a StripResult that lists only value-carried keys
// and omits marker-carried keys is the defect this test exists to catch.
func TestStripCarriage_CarriedKeys_IncludesMarkerCarriedKey(t *testing.T) {
	input := buildSyntheticDocWithBothKeyShapes()

	result, err := agentformat.StripCarriage(input)
	if err != nil {
		t.Fatalf("StripCarriage returned unexpected error: %v", err)
	}

	if !sliceContains(result.CarriedKeys, stripTestMarkerCarriedKey) {
		t.Errorf("CarriedKeys does not contain marker-carried key %q; got: %v -- "+
			"marker-carried keys must be named in CarriedKeys because they are precisely "+
			"the keys whose values cannot travel the format change",
			stripTestMarkerCarriedKey, result.CarriedKeys)
	}
}

// TestStripCarriage_CarriedKeys_ContainsBothKeyShapes verifies that when a document
// carries both value-carried and marker-carried keys, CarriedKeys names them all.
func TestStripCarriage_CarriedKeys_ContainsBothKeyShapes(t *testing.T) {
	input := buildSyntheticDocWithBothKeyShapes()

	result, err := agentformat.StripCarriage(input)
	if err != nil {
		t.Fatalf("StripCarriage returned unexpected error: %v", err)
	}

	for _, wantKey := range []string{stripTestValueCarriedKey, stripTestMarkerCarriedKey} {
		if !sliceContains(result.CarriedKeys, wantKey) {
			t.Errorf("CarriedKeys is missing key %q; got %v -- both value-carried "+
				"and marker-carried keys must be listed", wantKey, result.CarriedKeys)
		}
	}
}

// ---------------------------------------------------------------------------
// T15.9(c) -- Owned keys, values and body untouched; input not mutated
// ---------------------------------------------------------------------------

// TestStripCarriage_OwnedKeys_Untouched verifies that frontmatter keys that are not
// part of the carriage container survive stripping with their values intact.
func TestStripCarriage_OwnedKeys_Untouched(t *testing.T) {
	input := buildSyntheticDocWithBothKeyShapes()

	result, err := agentformat.StripCarriage(input)
	if err != nil {
		t.Fatalf("StripCarriage returned unexpected error: %v", err)
	}

	doc, parseErr := docformat.Parse(result.Canonical)
	if parseErr != nil {
		t.Fatalf("result.Canonical does not parse: %v", parseErr)
	}
	fm := doc.Frontmatter()

	// Verify that the ordinary owned key and its value are intact.
	v, ok := fm.Get(stripTestOwnedKey)
	if !ok {
		t.Errorf("owned key %q is absent from result.Canonical; it must not be modified by stripping", stripTestOwnedKey)
	} else if v.Scalar != "owned-value" {
		t.Errorf("owned key %q has value %q; want %q", stripTestOwnedKey, v.Scalar, "owned-value")
	}

	// Verify name and description also survive.
	for _, wantKey := range []string{"name", "description"} {
		if _, ok := fm.Get(wantKey); !ok {
			t.Errorf("key %q absent from result.Canonical; stripping must not remove non-carriage keys", wantKey)
		}
	}
}

// TestStripCarriage_Body_Untouched verifies that the document body is preserved
// byte-for-byte by StripCarriage.
func TestStripCarriage_Body_Untouched(t *testing.T) {
	input := buildSyntheticDocWithBothKeyShapes()
	wantBody := "Body text for StripCarriage test.\n"

	result, err := agentformat.StripCarriage(input)
	if err != nil {
		t.Fatalf("StripCarriage returned unexpected error: %v", err)
	}

	if !bytes.Contains(result.Canonical, []byte(wantBody)) {
		t.Errorf("body %q not found in result.Canonical; the body must survive stripping unchanged", wantBody)
	}
}

// TestStripCarriage_DoesNotMutateInput verifies that StripCarriage does not mutate
// the input byte slice. The caller must be able to pass the same document to multiple
// consumers without defensive copying.
func TestStripCarriage_DoesNotMutateInput(t *testing.T) {
	input := buildSyntheticDocWithBothKeyShapes()
	snapshot := make([]byte, len(input))
	copy(snapshot, input)

	_, _ = agentformat.StripCarriage(input) //nolint:errcheck

	if !bytes.Equal(input, snapshot) {
		t.Error("StripCarriage mutated the input bytes; it must be pure")
	}
}

// ---------------------------------------------------------------------------
// T15.9(d) -- No-container path: unchanged document, nil CarriedKeys
// ---------------------------------------------------------------------------

// TestStripCarriage_NoContainer_ReturnsDocumentUnchanged verifies that a document
// carrying no carriage container is returned with Canonical byte-identical to the
// input and nil CarriedKeys. This is the ordinary Markdown-source case and must not
// produce a spurious capability-loss report.
func TestStripCarriage_NoContainer_ReturnsDocumentUnchanged(t *testing.T) {
	input := buildDocWithNoContainer()

	result, err := agentformat.StripCarriage(input)
	if err != nil {
		t.Fatalf("StripCarriage on no-container document returned unexpected error: %v", err)
	}

	if !bytes.Equal(result.Canonical, input) {
		t.Errorf("no-container result.Canonical differs from input:\ngot:  %q\nwant: %q",
			result.Canonical, input)
	}
	if result.CarriedKeys != nil {
		t.Errorf("no-container result.CarriedKeys = %v; want nil (must not produce a spurious report)", result.CarriedKeys)
	}
}

// TestStripCarriage_NoContainer_EmptyInputRoundTrips verifies that an empty canonical
// document (if parseable) also produces a success with nil CarriedKeys.
func TestStripCarriage_NoContainer_MinimalDocumentRoundTrips(t *testing.T) {
	// Minimal canonical document: frontmatter with only a name, no container.
	input := []byte("---\nname: minimal\n---\nMinimal body.\n")

	result, err := agentformat.StripCarriage(input)
	if err != nil {
		t.Fatalf("StripCarriage on minimal document returned unexpected error: %v", err)
	}

	if !bytes.Equal(result.Canonical, input) {
		t.Errorf("minimal-document result.Canonical differs from input: got %q, want %q",
			result.Canonical, input)
	}
	if result.CarriedKeys != nil {
		t.Errorf("minimal-document result.CarriedKeys = %v; want nil", result.CarriedKeys)
	}
}

// ---------------------------------------------------------------------------
// T15.9(e) -- Malformed container yields ErrMalformedCarriage with Phase "strip"
// ---------------------------------------------------------------------------

// TestStripCarriage_MalformedCarriageValues_ReturnsErrMalformedCarriage verifies that
// a document where mosaic_carriage is a scalar (not a mapping) returns ErrMalformedCarriage
// wrapped in an ArtifactError with Phase "strip". This is distinct from ErrMalformedDeployed,
// which signals a failure to decode a deployed file.
func TestStripCarriage_MalformedCarriageValues_ReturnsErrMalformedCarriage(t *testing.T) {
	input := buildDocWithMalformedCarriageValues()

	_, err := agentformat.StripCarriage(input)
	if err == nil {
		t.Fatal("expected error for malformed mosaic_carriage, got nil")
	}

	if !errors.Is(err, agentformat.ErrMalformedCarriage) {
		t.Errorf("error does not wrap ErrMalformedCarriage: %v", err)
	}

	// Must NOT match ErrMalformedDeployed -- the two sentinels must be distinguishable.
	if errors.Is(err, agentformat.ErrMalformedDeployed) {
		t.Error("error wraps ErrMalformedDeployed; ErrMalformedCarriage must be a distinct sentinel")
	}

	// The ArtifactError must have Phase "strip".
	var ae *agentformat.ArtifactError
	if !errors.As(err, &ae) {
		t.Fatalf("error is not an *ArtifactError; got %T: %v", err, err)
	}
	if ae.Phase != "strip" {
		t.Errorf("ArtifactError.Phase = %q; want %q", ae.Phase, "strip")
	}
}

// TestStripCarriage_MalformedCarriageMarkers_ReturnsErrMalformedCarriage verifies that
// a document where mosaic_carriage_markers is a scalar (not a list) also returns
// ErrMalformedCarriage with Phase "strip".
func TestStripCarriage_MalformedCarriageMarkers_ReturnsErrMalformedCarriage(t *testing.T) {
	input := buildDocWithMalformedCarriageMarkers()

	_, err := agentformat.StripCarriage(input)
	if err == nil {
		t.Fatal("expected error for malformed mosaic_carriage_markers, got nil")
	}

	if !errors.Is(err, agentformat.ErrMalformedCarriage) {
		t.Errorf("error does not wrap ErrMalformedCarriage: %v", err)
	}

	if errors.Is(err, agentformat.ErrMalformedDeployed) {
		t.Error("error wraps ErrMalformedDeployed; ErrMalformedCarriage must be a distinct sentinel")
	}

	var ae *agentformat.ArtifactError
	if !errors.As(err, &ae) {
		t.Fatalf("error is not an *ArtifactError; got %T: %v", err, err)
	}
	if ae.Phase != "strip" {
		t.Errorf("ArtifactError.Phase = %q; want %q", ae.Phase, "strip")
	}
	if ae.Key != agentformat.CarriageMarkersKey {
		t.Errorf("ArtifactError.Key = %q; want %q (the offending carriage key)", ae.Key, agentformat.CarriageMarkersKey)
	}
}

// TestStripCarriage_MalformedContainer_PhaseMustBeStrip_NotDecodeOrEncode verifies
// that the Phase on the ArtifactError is "strip" (the strip direction), not "decode"
// or "encode". This distinguishes it from both translator phases.
func TestStripCarriage_MalformedContainer_PhaseMustBeStrip_NotDecodeOrEncode(t *testing.T) {
	input := buildDocWithMalformedCarriageValues()

	_, err := agentformat.StripCarriage(input)
	if err == nil {
		t.Fatal("expected error for malformed mosaic_carriage, got nil")
	}

	var ae *agentformat.ArtifactError
	if !errors.As(err, &ae) {
		t.Fatalf("error is not *ArtifactError: %T", err)
	}

	if ae.Phase == "decode" || ae.Phase == "encode" {
		t.Errorf("ArtifactError.Phase = %q; strip errors must use Phase %q, not a translator phase", ae.Phase, "strip")
	}
	if ae.Phase != "strip" {
		t.Errorf("ArtifactError.Phase = %q; want %q", ae.Phase, "strip")
	}
}

// TestStripCarriage_MalformedCarriageValues_NamesOffendingKey verifies that the
// ArtifactError.Key names the offending carriage key when mosaic_carriage is malformed.
func TestStripCarriage_MalformedCarriageValues_NamesOffendingKey(t *testing.T) {
	input := buildDocWithMalformedCarriageValues()

	_, err := agentformat.StripCarriage(input)
	if err == nil {
		t.Fatal("expected error for malformed mosaic_carriage, got nil")
	}

	var ae *agentformat.ArtifactError
	if !errors.As(err, &ae) {
		t.Fatalf("error is not *ArtifactError: %T", err)
	}

	if ae.Key != agentformat.CarriageValuesKey {
		t.Errorf("ArtifactError.Key = %q; want %q (the offending carriage key)", ae.Key, agentformat.CarriageValuesKey)
	}
}
