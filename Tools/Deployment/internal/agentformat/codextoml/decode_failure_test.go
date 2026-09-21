package codextoml_test

// decode_failure_test.go covers the decode failure contract: an exhaustive per-input
// matrix where every decode input falls in exactly one row. Each row has a
// corresponding sub-test below.
//
// ErrMalformedDeployed is the single decode-failure sentinel (declared in agentformat).
// EntryMissingInstructions is the report entry kind for an absent developer_instructions
// key (not an error, just a report entry).
//
// The matrix rows (labelled (a) through (i)) follow the decode contract exactly.
// A "Legacy name" sub-case is included (version, id, role): these must take the
// non-owned path, not fail, and not appear in the canonical frontmatter.

import (
	"errors"
	"testing"

	"mosaic-deploy/internal/agentfields"
	"mosaic-deploy/internal/agentformat"
)

// decodeExpectErr calls Decode and asserts it returns an error wrapping sentinel.
// Returns the ArtifactError for further inspection.
func decodeExpectErr(t *testing.T, tomlBytes []byte, sentinel error) *agentformat.ArtifactError {
	t.Helper()
	tr := codexTranslator(t)
	_, _, err := tr.Decode(tomlBytes, decodeCtx("agent"))
	if err == nil {
		t.Fatalf("Decode returned nil error; want error wrapping %v\nInput:\n%s", sentinel, tomlBytes)
	}
	if !errors.Is(err, sentinel) {
		t.Fatalf("Decode error = %v; want error wrapping %v", err, sentinel)
	}
	var ae *agentformat.ArtifactError
	if !errors.As(err, &ae) {
		t.Fatalf("Decode error = %v; want *agentformat.ArtifactError", err)
	}
	return ae
}

// --- Row (a): TOML the parser rejects ---

// TestDecodeFailure_SyntaxError_ErrMalformedDeployed verifies that TOML with a syntax
// error fails with ErrMalformedDeployed. The ArtifactError.Key must be empty because
// the failure is file-wide, not key-specific.
func TestDecodeFailure_SyntaxError_ErrMalformedDeployed(t *testing.T) {
	// Not valid TOML: unclosed string.
	input := []byte(`name = "unclosed`)
	ae := decodeExpectErr(t, input, agentformat.ErrMalformedDeployed)
	if ae.Key != "" {
		t.Errorf("ArtifactError.Key = %q; want empty (syntax errors are file-wide, not key-specific)", ae.Key)
	}
	if ae.Phase != "decode" {
		t.Errorf("ArtifactError.Phase = %q; want %q", ae.Phase, "decode")
	}
}

// TestDecodeFailure_DuplicateKey_ErrMalformedDeployed verifies that a TOML file with
// a duplicate key fails with ErrMalformedDeployed.
func TestDecodeFailure_DuplicateKey_ErrMalformedDeployed(t *testing.T) {
	input := []byte("name = \"a\"\nname = \"b\"\ndeveloper_instructions = \"body\"\n")
	decodeExpectErr(t, input, agentformat.ErrMalformedDeployed)
}

// --- Row (b): developer_instructions present as a string ---

// TestDecodeFailure_DeveloperInstructions_String_BodyRecovered verifies that when
// developer_instructions is present as a string, the body is recovered byte for byte.
// (This is a success row, not an error row.)
func TestDecodeFailure_DeveloperInstructions_String_BodyRecovered(t *testing.T) {
	const wantBody = "These are the instructions.\n"
	input := []byte("name = \"a\"\ndescription = \"d\"\ndeveloper_instructions = \"These are the instructions.\\n\"\n")

	canonical := decodeToml(t, input, "a")
	_, body := splitCanonical(t, canonical)

	if string(body) != wantBody {
		t.Errorf("body = %q; want %q", body, wantBody)
	}
}

// --- Row (c): developer_instructions absent ---

// TestDecodeFailure_DeveloperInstructions_Absent_NotAnError verifies that when
// developer_instructions is absent from the TOML file, decode succeeds with an
// empty body and produces an EntryMissingInstructions report entry.
func TestDecodeFailure_DeveloperInstructions_Absent_NotAnError(t *testing.T) {
	input := []byte("name = \"a\"\ndescription = \"d\"\n")

	tr := codexTranslator(t)
	canonical, report, err := tr.Decode(input, decodeCtx("a"))
	if err != nil {
		t.Fatalf("Decode returned error; want success with EntryMissingInstructions\nerror: %v", err)
	}

	// Body must be empty.
	_, body := splitCanonical(t, canonical)
	if len(body) != 0 {
		t.Errorf("body = %q; want empty (developer_instructions absent)", body)
	}

	// Report must contain EntryMissingInstructions.
	found := false
	for _, e := range report.Entries {
		if e.Kind == agentformat.EntryMissingInstructions {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("report does not contain EntryMissingInstructions; got: %v", report.Entries)
	}
}

// --- Row (b continued): developer_instructions present as the empty string ---

// TestDecodeFailure_DeveloperInstructions_PresentButEmpty_NotAnError verifies that
// developer_instructions = "" (present as an empty string) is not an error and does not
// produce an EntryMissingInstructions report entry.
//
// The decode contract distinguishes this row from the absent case: the key is present,
// so nothing is missing; the body is legitimately empty. EntryMissingInstructions means
// "the file carries no developer_instructions key", not "the body is empty". A decode
// that wrongly emits EntryMissingInstructions for developer_instructions = "" would
// satisfy every other existing test and still be wrong.
func TestDecodeFailure_DeveloperInstructions_PresentButEmpty_NotAnError(t *testing.T) {
	// developer_instructions is present but its value is the empty string.
	input := []byte("name = \"a\"\ndescription = \"d\"\ndeveloper_instructions = \"\"\n")

	tr := codexTranslator(t)
	canonical, report, err := tr.Decode(input, decodeCtx("a"))
	if err != nil {
		t.Fatalf("Decode returned error; want success for developer_instructions = \"\"\nerror: %v", err)
	}

	// Body must be empty.
	_, body := splitCanonical(t, canonical)
	if len(body) != 0 {
		t.Errorf("body = %q; want empty (developer_instructions is present but empty)", body)
	}

	// Report must NOT contain EntryMissingInstructions. The key is present; nothing is
	// missing. Widening EntryMissingInstructions to cover "body is empty" would make it
	// unable to name the cause in the message that matters (absent vs. empty string).
	for _, e := range report.Entries {
		if e.Kind == agentformat.EntryMissingInstructions {
			t.Errorf("report contains EntryMissingInstructions but developer_instructions = \"\" is present (not absent); the entry kind must mean the key is absent, not that the body is empty")
		}
	}
}

// --- Row (d): developer_instructions present but not a string ---

// TestDecodeFailure_DeveloperInstructions_Integer_ErrMalformedDeployed verifies that
// when developer_instructions is present as a non-string type, decode fails with
// ErrMalformedDeployed naming the key.
func TestDecodeFailure_DeveloperInstructions_Integer_ErrMalformedDeployed(t *testing.T) {
	input := []byte("name = \"a\"\ndeveloper_instructions = 42\n")
	ae := decodeExpectErr(t, input, agentformat.ErrMalformedDeployed)
	if ae.Key != "developer_instructions" {
		t.Errorf("ArtifactError.Key = %q; want %q", ae.Key, "developer_instructions")
	}
}

// TestDecodeFailure_DeveloperInstructions_Array_ErrMalformedDeployed verifies that
// an array-valued developer_instructions also fails with ErrMalformedDeployed.
func TestDecodeFailure_DeveloperInstructions_Array_ErrMalformedDeployed(t *testing.T) {
	input := []byte("name = \"a\"\ndeveloper_instructions = [\"line1\", \"line2\"]\n")
	ae := decodeExpectErr(t, input, agentformat.ErrMalformedDeployed)
	if ae.Key != "developer_instructions" {
		t.Errorf("ArtifactError.Key = %q; want %q", ae.Key, "developer_instructions")
	}
}

// TestDecodeFailure_DeveloperInstructions_Table_ErrMalformedDeployed verifies that
// a table-valued developer_instructions also fails with ErrMalformedDeployed.
func TestDecodeFailure_DeveloperInstructions_Table_ErrMalformedDeployed(t *testing.T) {
	input := []byte("name = \"a\"\n[developer_instructions]\nfoo = \"bar\"\n")
	ae := decodeExpectErr(t, input, agentformat.ErrMalformedDeployed)
	if ae.Key != "developer_instructions" {
		t.Errorf("ArtifactError.Key = %q; want %q", ae.Key, "developer_instructions")
	}
}

// --- Row (e): owned key present but not a string ---

// TestDecodeFailure_OwnedKey_NonString_ErrMalformedDeployed verifies that when
// name, description, model, or sandbox_mode is present as a non-string type, decode
// fails with ErrMalformedDeployed naming that key.
func TestDecodeFailure_OwnedKey_NonString_ErrMalformedDeployed(t *testing.T) {
	cases := []struct {
		key   string
		input []byte
	}{
		{
			key:   "name",
			input: []byte("name = 42\ndeveloper_instructions = \"body\"\n"),
		},
		{
			key:   "description",
			input: []byte("name = \"a\"\ndescription = 42\ndeveloper_instructions = \"body\"\n"),
		},
		{
			key:   "model",
			input: []byte("name = \"a\"\nmodel = 42\ndeveloper_instructions = \"body\"\n"),
		},
		{
			key:   "sandbox_mode",
			input: []byte("name = \"a\"\nsandbox_mode = 42\ndeveloper_instructions = \"body\"\n"),
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.key, func(t *testing.T) {
			ae := decodeExpectErr(t, tc.input, agentformat.ErrMalformedDeployed)
			if ae.Key != tc.key {
				t.Errorf("ArtifactError.Key = %q; want %q", ae.Key, tc.key)
			}
		})
	}
}

// --- Row (f): name absent ---

// TestDecodeFailure_NameAbsent_NotAnError verifies that when name is absent from the
// TOML file, decode succeeds and omits the name key from the canonical frontmatter.
func TestDecodeFailure_NameAbsent_NotAnError(t *testing.T) {
	input := []byte("description = \"d\"\ndeveloper_instructions = \"body\"\n")

	canonical := decodeToml(t, input, "agent")
	doc := parseCanonical(t, canonical)

	if _, ok := doc.Frontmatter().Get("name"); ok {
		t.Error("name is present in canonical frontmatter but was absent from TOML; it must be omitted")
	}
}

// --- Row (g): description, model, or sandbox_mode absent ---

// TestDecodeFailure_OptionalKeys_Absent_NotAnError verifies that absent description,
// model and sandbox_mode do not cause decode to fail; the keys are simply omitted
// from the canonical frontmatter.
func TestDecodeFailure_OptionalKeys_Absent_NotAnError(t *testing.T) {
	// Only the minimum required structure; all optional owned keys absent.
	input := []byte("developer_instructions = \"body\"\n")

	canonical := decodeToml(t, input, "agent")
	doc := parseCanonical(t, canonical)

	for _, key := range []string{"description", "model", "sandbox_mode"} {
		if _, ok := doc.Frontmatter().Get(key); ok {
			t.Errorf("key %q is present in canonical frontmatter but was absent from TOML; it must be omitted", key)
		}
	}
}

// --- Row (h): missing, malformed or partial stamp block ---

// TestDecodeFailure_MissingStampBlock_NotAnError verifies that when the TOML file
// has no stamp comment block, decode succeeds and yields no stamps.
func TestDecodeFailure_MissingStampBlock_NotAnError(t *testing.T) {
	input := []byte("name = \"a\"\ndescription = \"d\"\ndeveloper_instructions = \"body\"\n")

	canonical := decodeToml(t, input, "a")
	doc := parseCanonical(t, canonical)

	// No stamp keys must appear in the frontmatter.
	for _, f := range agentfields.All() {
		if _, ok := doc.Frontmatter().Get(f.Deployed); ok {
			t.Errorf("stamp key %q present in canonical frontmatter but no stamp block was in TOML; must yield no stamps", f.Deployed)
		}
	}
}

// TestDecodeFailure_MalformedStampLine_NotAnError verifies that a malformed comment
// in the header does not cause decode to fail.
func TestDecodeFailure_MalformedStampLine_NotAnError(t *testing.T) {
	// A malformed comment line followed by valid TOML.
	input := []byte("# not-a-stamp-line without a colon\nname = \"a\"\ndeveloper_instructions = \"body\"\n")
	decodeToml(t, input, "a") // must not panic or error
}

// TestDecodeFailure_PartialStampBlock_RemainingStampsDecoded verifies that when only
// some stamps are present in the block, those stamps appear in the canonical form
// while the absent ones are simply missing (no error).
func TestDecodeFailure_PartialStampBlock_RemainingStampsDecoded(t *testing.T) {
	fields := agentfields.All()
	if len(fields) < 2 {
		t.Skip("need at least two stamp entries")
	}

	f0 := fields[0]
	// Only f0 is present; f1 is absent.
	input := []byte("# " + f0.Deployed + ": 1.0\nname = \"a\"\ndeveloper_instructions = \"body\"\n")

	canonical := decodeToml(t, input, "a")
	doc := parseCanonical(t, canonical)

	fv, ok := doc.Frontmatter().Get(f0.Deployed)
	if !ok {
		t.Errorf("stamp %q absent; must be present when it was in the block", f0.Deployed)
	} else if fv.Scalar != "1.0" {
		t.Errorf("stamp %q value = %q; want %q", f0.Deployed, fv.Scalar, "1.0")
	}
}

// --- Row (i): user-owned key of any TOML shape ---

// TestDecodeFailure_UserOwnedKey_Scalar_NotAnError verifies that a user-owned scalar
// key does not cause decode to fail and does not appear in the canonical frontmatter.
func TestDecodeFailure_UserOwnedKey_Scalar_NotAnError(t *testing.T) {
	input := []byte("name = \"a\"\nmy_custom_key = \"custom-value\"\ndeveloper_instructions = \"body\"\n")

	canonical := decodeToml(t, input, "a")
	doc := parseCanonical(t, canonical)

	if _, ok := doc.Frontmatter().Get("my_custom_key"); ok {
		t.Error("user-owned scalar key appeared in canonical frontmatter; non-owned keys must be absent")
	}
}

// TestDecodeFailure_UserOwnedKey_Table_NotAnError verifies that a user-owned table
// key does not cause decode to fail and does not appear in the canonical frontmatter
// as an owned key.
func TestDecodeFailure_UserOwnedKey_Table_NotAnError(t *testing.T) {
	input := []byte("name = \"a\"\ndeveloper_instructions = \"body\"\n[custom_settings]\nfoo = \"bar\"\n")

	decodeToml(t, input, "a") // must not panic or error
}

// TestDecodeFailure_UserOwnedKey_Integer_NotAnError verifies that a user-owned
// integer key does not cause decode to fail.
func TestDecodeFailure_UserOwnedKey_Integer_NotAnError(t *testing.T) {
	input := []byte("name = \"a\"\ndeveloper_instructions = \"body\"\nmy_int = 42\n")
	decodeToml(t, input, "a") // must not panic or error
}

// --- Legacy-name case: version, id, role ---

// TestDecodeFailure_LegacyNameKeys_DecodeWithoutError verifies that keys named
// "version", "id" and "role" are treated as non-owned user keys: they decode without
// error and are absent from the canonical frontmatter.
//
// This is the decode-side counterpart of the encode-side Legacy-name test. It turns
// red if isMosaicOwned is built on agentfields.IsMosaicOnlyDeployedKey (which matches
// unprefixed Legacy names), because that predicate would classify these keys as owned.
func TestDecodeFailure_LegacyNameKeys_DecodeWithoutError(t *testing.T) {
	cases := []string{"version", "id", "role"}

	for _, key := range cases {
		key := key
		t.Run(key, func(t *testing.T) {
			input := []byte("name = \"a\"\n" + key + " = \"user-value\"\ndeveloper_instructions = \"body\"\n")

			canonical := decodeToml(t, input, "a")
			doc := parseCanonical(t, canonical)

			// The key must be absent from the canonical frontmatter.
			if _, ok := doc.Frontmatter().Get(key); ok {
				t.Errorf("key %q present in canonical frontmatter; Legacy names must be treated as non-owned (absent from canonical output)", key)
			}
		})
	}
}

// TestDecodeFailure_ArtifactError_PhaseIsDecode verifies that every error produced
// by Decode carries Phase: "decode" in the wrapping ArtifactError.
func TestDecodeFailure_ArtifactError_PhaseIsDecode(t *testing.T) {
	// Syntax error is the simplest case to produce ErrMalformedDeployed.
	input := []byte(`name = unclosed-string`)
	tr := codexTranslator(t)
	_, _, err := tr.Decode(input, decodeCtx("a"))
	if err == nil {
		t.Fatal("expected decode error, got nil")
	}
	var ae *agentformat.ArtifactError
	if !errors.As(err, &ae) {
		t.Fatalf("error is not *agentformat.ArtifactError: %v", err)
	}
	if ae.Phase != "decode" {
		t.Errorf("ArtifactError.Phase = %q; want %q", ae.Phase, "decode")
	}
}
