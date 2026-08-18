package descriptor_test

// Tests asserting that model identifiers in a harness descriptor are never validated
// against any fixed list: any non-empty string is a valid model ID.
//
// Coverage:
//   - A descriptor with no models block loads successfully without error.
//   - A descriptor with an explicitly empty model ID list loads successfully.
//   - When no models block is present, ModelCatalog.IDs is nil or empty.
//   - A descriptor with unconventional model identifier formats (provider-prefixed paths,
//     colon-separated identifiers, all-caps, arbitrary strings) loads successfully.
//   - Unusual model IDs are stored verbatim: no normalisation, case-folding, or
//     validation against a known list occurs.
//   - The ModelCatalog.FormatHint is also stored verbatim and never validated.

import (
	"os"
	"path/filepath"
	"testing"

	"mosaic-deploy/internal/harness/descriptor"
)

// --- Absent models block ---

func TestParse_AbsentModelsBlock_ReturnsNoError(t *testing.T) {
	// A descriptor without a "models:" key must load without error.
	// The schema must treat the models block as optional.
	src := loadModelsFixture(t, "valid-empty-models.yaml")

	_, err := descriptor.Parse(src, "valid-empty-models.yaml")

	if err != nil {
		t.Fatalf("Parse without models block returned unexpected error: %v", err)
	}
}

func TestParse_AbsentModelsBlock_ModelIDsIsEmpty(t *testing.T) {
	src := loadModelsFixture(t, "valid-empty-models.yaml")

	d, err := descriptor.Parse(src, "valid-empty-models.yaml")

	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(d.Models.IDs) != 0 {
		t.Errorf("Models.IDs should be empty when no models block is present, got: %v", d.Models.IDs)
	}
}

func TestParse_AbsentModelsBlock_FormatHintIsEmpty(t *testing.T) {
	src := loadModelsFixture(t, "valid-empty-models.yaml")

	d, err := descriptor.Parse(src, "valid-empty-models.yaml")

	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if d.Models.FormatHint != "" {
		t.Errorf("Models.FormatHint should be empty when no models block is present, got: %q", d.Models.FormatHint)
	}
}

func TestLoad_AbsentModelsBlock_LoadsSuccessfully(t *testing.T) {
	path := filepath.Join(testdataDir, "valid-empty-models.yaml")

	d, err := descriptor.Load(path)

	if err != nil {
		t.Fatalf("Load without models block returned unexpected error: %v", err)
	}
	if d == nil {
		t.Fatal("Load returned nil descriptor without error")
	}
}

// --- Explicitly empty model list ---

func TestParse_EmptyModelIDList_ReturnsNoError(t *testing.T) {
	const yaml = `schema_version: "1"
id: "empty-list-harness"
display_name: "Empty List Harness"
models:
  ids: []
  format_hint: "provider/model-id"
`
	_, err := descriptor.Parse([]byte(yaml), "inline:empty-list")

	if err != nil {
		t.Fatalf("Parse with empty model ids list returned unexpected error: %v", err)
	}
}

func TestParse_EmptyModelIDList_ModelIDsIsEmpty(t *testing.T) {
	const yaml = `schema_version: "1"
id: "empty-list-harness"
display_name: "Empty List Harness"
models:
  ids: []
`
	d, err := descriptor.Parse([]byte(yaml), "inline:empty-list")

	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(d.Models.IDs) != 0 {
		t.Errorf("Models.IDs should be empty for an explicit empty list, got: %v", d.Models.IDs)
	}
}

// --- Unusual model identifier formats ---

func TestParse_UnusualModelIdentifierFormats_ReturnsNoError(t *testing.T) {
	// The fixture contains IDs with provider-prefixed paths, colons, uppercase letters,
	// and other formats that might tempt an implementation to validate or reject them.
	src := loadModelsFixture(t, "valid-unusual-model-ids.yaml")

	_, err := descriptor.Parse(src, "valid-unusual-model-ids.yaml")

	if err != nil {
		t.Fatalf("Parse with unusual model IDs returned unexpected error: %v", err)
	}
}

func TestParse_UnusualModelIdentifierFormats_AllIDsLoaded(t *testing.T) {
	src := loadModelsFixture(t, "valid-unusual-model-ids.yaml")

	d, err := descriptor.Parse(src, "valid-unusual-model-ids.yaml")

	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	// The fixture declares 5 model IDs; all must be loaded.
	const wantCount = 5
	if len(d.Models.IDs) != wantCount {
		t.Fatalf("Models.IDs: want %d entries, got %d: %v", wantCount, len(d.Models.IDs), d.Models.IDs)
	}
}

func TestParse_UnusualModelIdentifierFormats_IDsStoredVerbatim(t *testing.T) {
	src := loadModelsFixture(t, "valid-unusual-model-ids.yaml")

	d, err := descriptor.Parse(src, "valid-unusual-model-ids.yaml")

	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	// Verify the most distinctive IDs are stored exactly as written.
	wantContains := map[string]bool{
		"accounts/fireworks/models/deepseek-r1": false,
		"provider:region:model-name":            false,
		"UPPERCASE-MODEL-ID":                    false,
	}
	for _, id := range d.Models.IDs {
		if _, ok := wantContains[id]; ok {
			wantContains[id] = true
		}
	}
	for id, found := range wantContains {
		if !found {
			t.Errorf("expected model ID %q to be present verbatim in Models.IDs", id)
		}
	}
}

func TestParse_UnusualModelIdentifierFormats_NoNormalisationOccurs(t *testing.T) {
	// An ID like "UPPERCASE-MODEL-ID" must not be lowercased. The implementation must
	// store every ID exactly as written.
	src := loadModelsFixture(t, "valid-unusual-model-ids.yaml")

	d, err := descriptor.Parse(src, "valid-unusual-model-ids.yaml")

	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	for _, id := range d.Models.IDs {
		if id == "uppercase-model-id" {
			t.Error("model ID was lowercased; IDs must be stored verbatim without case-folding")
		}
	}
}

// --- FormatHint verbatim storage ---

func TestParse_ModelFormatHintStoredVerbatim(t *testing.T) {
	// FormatHint is display-only and must be passed through without modification.
	const yaml = `schema_version: "1"
id: "hint-harness"
display_name: "Hint Harness"
models:
  ids:
    - "some-model"
  format_hint: "any format hint text is also accepted without validation"
`
	d, err := descriptor.Parse([]byte(yaml), "inline:hint")

	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	want := "any format hint text is also accepted without validation"
	if d.Models.FormatHint != want {
		t.Errorf("Models.FormatHint: want %q, got %q", want, d.Models.FormatHint)
	}
}

// --- No hardcoded list validation ---

func TestParse_ModelIDsNeverValidatedAgainstHardcodedList(t *testing.T) {
	// Sending in a completely made-up model identifier must not produce an error.
	const yaml = `schema_version: "1"
id: "made-up-models-harness"
display_name: "Made Up Models Harness"
models:
  ids:
    - "xyzzy-not-a-real-provider/not-a-real-model-xyz-99999"
`
	_, err := descriptor.Parse([]byte(yaml), "inline:made-up")

	if err != nil {
		t.Fatalf("Parse with a made-up model ID returned unexpected error: %v (model IDs must never be validated)", err)
	}
}

// --- helper ---

func loadModelsFixture(t *testing.T, name string) []byte {
	t.Helper()
	path := filepath.Join(testdataDir, name)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return data
}
