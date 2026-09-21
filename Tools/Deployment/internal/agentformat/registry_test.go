package agentformat_test

// registry_test.go covers the translator registry: registration, lookup, unknown-ID
// error, duplicate-registration panic, LookupString entry point, and RegisteredIDs.

import (
	"errors"
	"testing"

	"mosaic-deploy/internal/agentformat"
	"mosaic-deploy/internal/agentformat/formatid"

	// Blank-import the wiring package so that the CodexTOML translator is registered.
	// Tests in this file that assert both format IDs are resolvable require this; the
	// Markdown translator self-registers from the agentformat package's own init().
	_ "mosaic-deploy/internal/agentformat/all"
)

// TestRegistry_LookupMarkdown_ReturnsTranslator verifies that the Markdown format ID
// resolves to a non-nil translator after the agentformat package is imported.
func TestRegistry_LookupMarkdown_ReturnsTranslator(t *testing.T) {
	tr, err := agentformat.Lookup(formatid.Markdown)
	if err != nil {
		t.Fatalf("Lookup(%q) returned unexpected error: %v", formatid.Markdown, err)
	}
	if tr == nil {
		t.Fatalf("Lookup(%q) returned nil translator with nil error", formatid.Markdown)
	}
}

// TestRegistry_LookupCodexTOML_ReturnsTranslator verifies that the CodexTOML format
// ID resolves to a non-nil translator after the wiring package is imported.
func TestRegistry_LookupCodexTOML_ReturnsTranslator(t *testing.T) {
	tr, err := agentformat.Lookup(formatid.CodexTOML)
	if err != nil {
		t.Fatalf("Lookup(%q) returned unexpected error: %v", formatid.CodexTOML, err)
	}
	if tr == nil {
		t.Fatalf("Lookup(%q) returned nil translator with nil error", formatid.CodexTOML)
	}
}

// TestRegistry_LookupUnknownID_ReturnsErrUnknownFormat verifies that looking up an
// ID that has never been registered returns a non-nil error wrapping ErrUnknownFormat.
func TestRegistry_LookupUnknownID_ReturnsErrUnknownFormat(t *testing.T) {
	unknown := formatid.ID("does-not-exist-format")
	_, err := agentformat.Lookup(unknown)
	if err == nil {
		t.Fatal("Lookup of unknown ID returned nil error; want non-nil error wrapping ErrUnknownFormat")
	}
	if !errors.Is(err, agentformat.ErrUnknownFormat) {
		t.Errorf("Lookup of unknown ID returned %v; want error wrapping ErrUnknownFormat", err)
	}
}

// TestRegistry_LookupUnknownID_NeverReturnsNilTranslatorWithNilError verifies the
// registry never returns (nil, nil) for any input.
func TestRegistry_LookupUnknownID_NeverReturnsNilTranslatorWithNilError(t *testing.T) {
	_, err := agentformat.Lookup(formatid.ID("some-unregistered-id"))
	if err == nil {
		t.Fatal("Lookup returned nil error for an unregistered ID; contract requires a non-nil error")
	}
}

// TestRegistry_RegisterDuplicate_Panics verifies that calling Register twice with
// the same format ID panics: two translators for one format is a programming error.
func TestRegistry_RegisterDuplicate_Panics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("Register with duplicate ID did not panic; expected panic for duplicate registration")
		}
	}()

	// Use a unique test-only ID so we do not collide with the production registry.
	testID := formatid.ID("test-duplicate-panic-format")
	stub := stubTranslator{}
	agentformat.Register(testID, stub)
	// Second registration with the same ID must panic.
	agentformat.Register(testID, stub)
}

// TestRegistry_LookupString_EmptyString_ResolvesToMarkdown verifies that LookupString("")
// resolves to the Markdown identity translator (CD-4: absent agent_format_id means Markdown).
func TestRegistry_LookupString_EmptyString_ResolvesToMarkdown(t *testing.T) {
	tr, err := agentformat.LookupString("")
	if err != nil {
		t.Fatalf("LookupString(\"\") returned unexpected error: %v", err)
	}
	if tr == nil {
		t.Fatal("LookupString(\"\") returned nil translator with nil error")
	}

	// Verify the returned translator is the same one Lookup(Markdown) returns.
	markdownTr, _ := agentformat.Lookup(formatid.Markdown)
	if tr != markdownTr {
		t.Error("LookupString(\"\") did not return the same translator as Lookup(Markdown)")
	}
}

// TestRegistry_LookupString_InvalidID_ReturnsError verifies that LookupString with an
// unrecognised format ID string returns a non-nil error.
func TestRegistry_LookupString_InvalidID_ReturnsError(t *testing.T) {
	_, err := agentformat.LookupString("no-such-format-ever")
	if err == nil {
		t.Fatal("LookupString with unrecognised ID returned nil error; want non-nil error")
	}
}

// TestRegistry_LookupString_CodexTOML_Resolves verifies that LookupString with the
// CodexTOML string value returns a non-nil translator.
func TestRegistry_LookupString_CodexTOML_Resolves(t *testing.T) {
	tr, err := agentformat.LookupString(string(formatid.CodexTOML))
	if err != nil {
		t.Fatalf("LookupString(%q) returned unexpected error: %v", formatid.CodexTOML, err)
	}
	if tr == nil {
		t.Fatal("LookupString returned nil translator with nil error")
	}
}

// TestRegistry_RegisteredIDs_ContainsBothKnownFormats verifies that RegisteredIDs
// returns a slice containing both the Markdown and CodexTOML format IDs.
func TestRegistry_RegisteredIDs_ContainsBothKnownFormats(t *testing.T) {
	ids := agentformat.RegisteredIDs()

	found := make(map[formatid.ID]bool, len(ids))
	for _, id := range ids {
		found[id] = true
	}

	if !found[formatid.Markdown] {
		t.Errorf("RegisteredIDs does not contain %q; Markdown translator must always be registered", formatid.Markdown)
	}
	if !found[formatid.CodexTOML] {
		t.Errorf("RegisteredIDs does not contain %q; CodexTOML translator must be registered after importing the wiring package", formatid.CodexTOML)
	}
}

// TestRegistry_RegisteredIDs_IsSorted verifies that RegisteredIDs returns IDs in
// sorted order, as required by RegisteredIDs' contract.
func TestRegistry_RegisteredIDs_IsSorted(t *testing.T) {
	ids := agentformat.RegisteredIDs()
	for i := 1; i < len(ids); i++ {
		if ids[i] < ids[i-1] {
			t.Errorf("RegisteredIDs is not sorted: %q appears before %q at index %d", ids[i-1], ids[i], i)
		}
	}
}

// stubTranslator is a minimal Translator implementation for the duplicate-registration
// panic test. Its Encode and Decode are not called in this test.
type stubTranslator struct{}

func (stubTranslator) Encode(canonical []byte, ctx agentformat.ArtifactContext) ([]byte, agentformat.Report, error) {
	return canonical, agentformat.Report{}, nil
}
func (stubTranslator) Decode(deployed []byte, ctx agentformat.ArtifactContext) ([]byte, agentformat.Report, error) {
	return deployed, agentformat.Report{}, nil
}
