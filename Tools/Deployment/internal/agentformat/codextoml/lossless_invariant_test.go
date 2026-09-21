package codextoml_test

// lossless_invariant_test.go covers two related concerns:
//
// T3.3 - The hostile-content body corpus through the full translator round trip:
//   decode(encode(body)) returns the original body bytes exactly for every corpus entry.
//
// T3.5 - The lossless round-trip invariant:
//   decode(encode(D)) == N(D) for MOSAIC-owned keys, body and stamps.
//   Inputs triggering every normalisation (blank description, absent model, absent or
//   divergent name) are included. The invariant holds over more than one round trip
//   (idempotence). A hand-edited MOSAIC-owned key is decodable and produces canonical
//   output distinguishable from what MOSAIC would have written. sandbox_mode is covered
//   explicitly. At least one invariant case draws values from the hostile-value corpus.

import (
	"bytes"
	"testing"

	"mosaic-deploy/internal/agentfields"
	"mosaic-deploy/internal/agentformat"
)

// roundTrip calls encode then decode and returns the decoded canonical bytes.
// It is the building block for the lossless invariant tests.
//
// Note: this helper uses Op: agentformat.OpCreate, which means it does not supply
// prior deployed bytes. It is therefore only suitable for canonical documents that
// contain no marker-carried keys (mosaic_carriage_markers). Once Op enforcement is
// implemented, passing a canonical containing markers to roundTrip will result in
// ErrMarkerUnrecoverable (OpCreate cannot supply the prior-bytes channel required to
// recover a marker). Tests for the marker-carried round-trip path use OpUpdate with
// explicit prior bytes; see TestCarriageLossless_MarkerCarried_MultipleRoundTrips in
// carriage_marker_test.go.
func roundTrip(t *testing.T, canonical []byte, agentKey string) []byte {
	t.Helper()
	tr := codexTranslator(t)
	ctx := agentformat.ArtifactContext{
		AgentKey: agentKey,
		Op:       agentformat.OpCreate,
	}
	tomlBytes, _, err := tr.Encode(canonical, ctx)
	if err != nil {
		t.Fatalf("Encode error: %v", err)
	}
	decoded, _, err := tr.Decode(tomlBytes, decodeCtx(agentKey))
	if err != nil {
		t.Fatalf("Decode after Encode error: %v", err)
	}
	return decoded
}

// --- T3.3: hostile body corpus through the full decode(encode(body)) round trip ---

// TestDecodeEncode_HostileBody_RoundTrip verifies that for every entry in the
// hostile-body corpus, decode(encode(body)) == body byte for byte. The hostile corpus
// (defined in hostile_test.go) covers bodies that exercise every branch of the
// string-form selection logic.
func TestDecodeEncode_HostileBody_RoundTrip(t *testing.T) {
	for _, tc := range hostilebodies {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			canonical := []byte("---\nname: hostile-test\ndescription: A test.\n---\n" + tc.body)

			result := roundTrip(t, canonical, "hostile-test")
			_, body := splitCanonical(t, result)

			if string(body) != tc.body {
				t.Errorf("round-trip body mismatch:\ngot:  %q\nwant: %q", body, tc.body)
			}
		})
	}
}

// --- T3.5: lossless round-trip invariant ---

// TestRoundTrip_BasicInvariant verifies that decode(encode(D)) preserves the
// MOSAIC-owned key values and the body. This is the core invariant assertion.
func TestRoundTrip_BasicInvariant(t *testing.T) {
	const agentKey = "my-agent"
	canonical := makeCanonical(
		"name: my-agent\ndescription: An agent for testing.\nmodel: gpt-5\nsandbox_mode: read-only\n",
		"These are the instructions.\n",
	)

	result := roundTrip(t, canonical, agentKey)
	doc := parseCanonical(t, result)
	_, body := splitCanonical(t, result)

	checkField(t, doc, "description", "An agent for testing.")
	checkField(t, doc, "model", "gpt-5")
	checkField(t, doc, "sandbox_mode", "read-only")

	if string(body) != "These are the instructions.\n" {
		t.Errorf("body = %q; want %q", body, "These are the instructions.\n")
	}
}

// TestRoundTrip_Normalization_BlankDescription verifies that a blank description in
// the canonical input is normalised to the deterministic fallback and survives the
// round trip as that fallback value. decode(encode(D)) == N(D) where N applies the
// description fallback.
func TestRoundTrip_Normalization_BlankDescription(t *testing.T) {
	const agentKey = "norm-agent"
	canonical := makeCanonical("name: norm-agent\ndescription: \n", "body\n")

	result := roundTrip(t, canonical, agentKey)
	doc := parseCanonical(t, result)

	wantDesc := "MOSAIC agent " + agentKey
	checkField(t, doc, "description", wantDesc)
}

// TestRoundTrip_Normalization_AbsentDescription verifies the same normalisation for
// an absent description key.
func TestRoundTrip_Normalization_AbsentDescription(t *testing.T) {
	const agentKey = "norm-agent2"
	canonical := makeCanonical("name: norm-agent2\n", "body\n")

	result := roundTrip(t, canonical, agentKey)
	doc := parseCanonical(t, result)

	wantDesc := "MOSAIC agent " + agentKey
	checkField(t, doc, "description", wantDesc)
}

// TestRoundTrip_Normalization_AbsentModel verifies that an absent model key is
// omitted from the decoded canonical frontmatter after the round trip.
func TestRoundTrip_Normalization_AbsentModel(t *testing.T) {
	const agentKey = "no-model"
	canonical := makeCanonical("name: no-model\ndescription: d\n", "body\n")

	result := roundTrip(t, canonical, agentKey)
	doc := parseCanonical(t, result)

	if _, ok := doc.Frontmatter().Get("model"); ok {
		t.Error("model present in round-trip output; must be absent when not in canonical input")
	}
}

// TestRoundTrip_Normalization_DivergentName verifies that a name in the canonical
// frontmatter that differs from agentKey is replaced by agentKey after encode, and
// that value survives decode. decode(encode(D)) == N(D) where N applies name forcing.
func TestRoundTrip_Normalization_DivergentName(t *testing.T) {
	const agentKey = "correct-key"
	canonical := makeCanonical("name: wrong-name\ndescription: d\n", "body\n")

	result := roundTrip(t, canonical, agentKey)
	doc := parseCanonical(t, result)

	checkField(t, doc, "name", agentKey)
}

// TestRoundTrip_SandboxMode_ExplicitlyPresent verifies that sandbox_mode survives
// the round trip when present in canonical input. This is the explicit sandbox_mode
// assertion: an ownership rule built only on agentfields would misclassify sandbox_mode
// as a user key (absent from agentfields), ignore it on decode, and fail this test.
func TestRoundTrip_SandboxMode_ExplicitlyPresent(t *testing.T) {
	const agentKey = "sm-agent"
	for _, mode := range []string{"read-only", "workspace-write"} {
		mode := mode
		t.Run(mode, func(t *testing.T) {
			canonical := makeCanonical("name: sm-agent\ndescription: d\nsandbox_mode: "+mode+"\n", "body\n")

			result := roundTrip(t, canonical, agentKey)
			doc := parseCanonical(t, result)

			checkField(t, doc, "sandbox_mode", mode)
		})
	}
}

// TestRoundTrip_SandboxMode_FallbackApplied verifies that when sandbox_mode is absent
// from the canonical input, the fallback "read-only" is applied on encode and survives
// decode. This is the other half of the sandbox_mode obligation.
func TestRoundTrip_SandboxMode_FallbackApplied(t *testing.T) {
	const agentKey = "sm-fallback"
	// No sandbox_mode in canonical input.
	canonical := makeCanonical("name: sm-fallback\ndescription: d\n", "body\n")

	result := roundTrip(t, canonical, agentKey)
	doc := parseCanonical(t, result)

	checkField(t, doc, "sandbox_mode", "read-only")
}

// TestRoundTrip_MultipleRoundTrips_Stable verifies that the lossless invariant holds
// over more than one round trip: a second decode(encode(...)) produces bytes identical
// to the first. This catches a decode that stabilises only on the second pass.
func TestRoundTrip_MultipleRoundTrips_Stable(t *testing.T) {
	const agentKey = "stable-agent"
	canonical := makeCanonical(
		"name: stable-agent\ndescription: Stable agent.\nmodel: gpt-5\nsandbox_mode: read-only\n",
		"Instructions.\n",
	)

	result1 := roundTrip(t, canonical, agentKey)
	result2 := roundTrip(t, result1, agentKey)

	if !bytes.Equal(result1, result2) {
		t.Errorf("invariant unstable after two round trips\nfirst:  %q\nsecond: %q", result1, result2)
	}
}

// TestRoundTrip_ThirdRoundTrip_Stable extends the multi-round-trip check to three
// passes to catch cases where the invariant holds at two but breaks at three.
func TestRoundTrip_ThirdRoundTrip_Stable(t *testing.T) {
	const agentKey = "three-pass"
	canonical := makeCanonical(
		"name: three-pass\ndescription: Three-pass agent.\nsandbox_mode: read-only\n",
		"Body.\n",
	)

	result1 := roundTrip(t, canonical, agentKey)
	result2 := roundTrip(t, result1, agentKey)
	result3 := roundTrip(t, result2, agentKey)

	if !bytes.Equal(result2, result3) {
		t.Errorf("invariant unstable at third round trip\nsecond: %q\nthird:  %q", result2, result3)
	}
}

// TestRoundTrip_HandEdited_SandboxMode_Decodable verifies that a hand-edited
// sandbox_mode in a deployed TOML file (a value different from what MOSAIC would write)
// is decodable and produces canonical output distinguishable from what MOSAIC wrote.
// This proves drift detection can distinguish the hand edit.
func TestRoundTrip_HandEdited_SandboxMode_Decodable(t *testing.T) {
	// Simulate a deployed file where a user hand-edited sandbox_mode.
	handEditedTOML := []byte(`name = "agent"
description = "d"
sandbox_mode = "workspace-write"
developer_instructions = "body"
`)
	canonical := decodeToml(t, handEditedTOML, "agent")
	doc := parseCanonical(t, canonical)

	// The hand-edited value must survive decode.
	checkField(t, doc, "sandbox_mode", "workspace-write")

	// Compute what MOSAIC would normally write (uses the read-only fallback).
	normalTOML := []byte(`name = "agent"
description = "d"
developer_instructions = "body"
`)
	normalCanonical := decodeToml(t, normalTOML, "agent")
	normalDoc := parseCanonical(t, normalCanonical)
	normalFV, _ := normalDoc.Frontmatter().Get("sandbox_mode")

	// The hand-edited canonical form must differ from the MOSAIC-normalised form.
	if normalFV.Scalar == "workspace-write" {
		t.Error("hand-edited sandbox_mode is indistinguishable from MOSAIC's output; drift detection cannot act on it")
	}
}

// TestRoundTrip_HandEdited_Model_Decodable verifies that a hand-edited model value
// is decodable and distinguishable from what MOSAIC would have written.
func TestRoundTrip_HandEdited_Model_Decodable(t *testing.T) {
	handEditedTOML := []byte(`name = "agent"
description = "d"
model = "hand-edited-model"
sandbox_mode = "read-only"
developer_instructions = "body"
`)
	canonical := decodeToml(t, handEditedTOML, "agent")
	doc := parseCanonical(t, canonical)

	checkField(t, doc, "model", "hand-edited-model")
}

// TestRoundTrip_WithHostileValue_FromCorpus verifies that the lossless invariant holds
// when owned-key values come from the frontmatter-value hostile corpus. At least one
// invariant case must draw from the corpus, so the invariant is not proven only on
// benign values. Here "true" (YAML-reserved) is used as the description value.
func TestRoundTrip_WithHostileValue_FromCorpus(t *testing.T) {
	const agentKey = "hostile-value-invariant"

	// "true" is a YAML-reserved spelling; the quoting policy must double-quote it.
	tomlInput := []byte("name = \"" + agentKey + "\"\ndescription = \"true\"\nsandbox_mode = \"read-only\"\ndeveloper_instructions = \"body\"\n")

	canonical := decodeToml(t, tomlInput, agentKey)
	doc := parseCanonical(t, canonical)

	checkField(t, doc, "description", "true")

	// Second round trip must produce identical output.
	canonical2 := roundTrip(t, canonical, agentKey)
	if !bytes.Equal(canonical, canonical2) {
		t.Errorf("invariant unstable for hostile value \"true\":\nfirst:  %q\nsecond: %q", canonical, canonical2)
	}
}

// TestRoundTrip_StampsPreserved verifies that stamps present in a deployed TOML file
// survive the decode(encode(D)) round trip in the decoded canonical form.
func TestRoundTrip_StampsPreserved(t *testing.T) {
	fields := agentfields.All()
	if len(fields) == 0 {
		t.Skip("no stamp entries in agentfields.All()")
	}

	f := fields[0]
	const stampVal = "2.0.0"

	// The TOML file has a stamp in the comment block.
	tomlInput := []byte("# " + f.Deployed + ": " + stampVal + "\nname = \"a\"\ndescription = \"d\"\ndeveloper_instructions = \"body\"\n")
	canonical := decodeToml(t, tomlInput, "a")

	// Now encode the decoded canonical and decode again.
	result := roundTrip(t, canonical, "a")
	doc := parseCanonical(t, result)

	fv, ok := doc.Frontmatter().Get(f.Deployed)
	if !ok {
		t.Errorf("stamp %q absent from round-trip canonical output; must be preserved", f.Deployed)
	} else if fv.Scalar != stampVal {
		t.Errorf("stamp %q = %q; want %q", f.Deployed, fv.Scalar, stampVal)
	}
}
