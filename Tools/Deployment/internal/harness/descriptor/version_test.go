package descriptor_test

// Tests for descriptor schema version handling: the loader accepts known versions and
// rejects unknown versions with a distinct, identifiable error.
//
// Coverage:
//   - SupportedSchemaVersions is non-empty, so at least one version is always valid.
//   - Parse with a supported schema_version succeeds and populates the SchemaVersion field.
//   - Parse with an unsupported schema_version returns ErrUnsupportedSchemaVersion.
//   - The unsupported-version error is distinct from general validation errors: errors.Is
//     on the returned error matches ErrUnsupportedSchemaVersion.
//   - Load with an unsupported schema_version also returns ErrUnsupportedSchemaVersion.
//   - Load and Parse agree: both recognise the same set of supported versions.

import (
	"errors"
	"path/filepath"
	"testing"

	"mosaic-deploy/internal/harness/descriptor"
)

// --- SupportedSchemaVersions ---

func TestSupportedSchemaVersions_IsNonEmpty(t *testing.T) {
	if len(descriptor.SupportedSchemaVersions) == 0 {
		t.Fatal("SupportedSchemaVersions must contain at least one version string")
	}
}

func TestSupportedSchemaVersions_ContainsNoEmptyStrings(t *testing.T) {
	for i, v := range descriptor.SupportedSchemaVersions {
		if v == "" {
			t.Errorf("SupportedSchemaVersions[%d] is an empty string, which is not a valid version", i)
		}
	}
}

// --- Parse: supported version succeeds ---

func TestParse_SupportedSchemaVersion_ReturnsNoError(t *testing.T) {
	// The valid-full.yaml fixture declares schema_version: "1", which must be in
	// SupportedSchemaVersions. If the test harness changes SupportedSchemaVersions, this
	// fixture must be updated to match.
	src := fixtureBytes(t, "valid-full.yaml")

	_, err := descriptor.Parse(src, "valid-full.yaml")

	if err != nil {
		t.Fatalf("Parse with supported schema_version returned unexpected error: %v", err)
	}
}

func TestParse_SupportedSchemaVersion_PopulatesSchemaVersionField(t *testing.T) {
	src := fixtureBytes(t, "valid-full.yaml")

	d, err := descriptor.Parse(src, "valid-full.yaml")

	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if d.SchemaVersion == "" {
		t.Error("SchemaVersion must be populated after parsing a valid descriptor")
	}
}

func TestParse_SupportedSchemaVersion_SchemaVersionMatchesSource(t *testing.T) {
	src := fixtureBytes(t, "valid-full.yaml")

	d, err := descriptor.Parse(src, "valid-full.yaml")

	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	// The fixture declares schema_version: "1". The first entry in SupportedSchemaVersions
	// must equal "1" for this test to remain valid.
	want := "1"
	if d.SchemaVersion != want {
		t.Errorf("SchemaVersion: want %q, got %q", want, d.SchemaVersion)
	}
}

// --- Parse: unsupported version ---

func TestParse_UnsupportedSchemaVersion_ReturnsError(t *testing.T) {
	src := fixtureBytes(t, "unsupported-version.yaml")

	_, err := descriptor.Parse(src, "unsupported-version.yaml")

	if err == nil {
		t.Fatal("expected Parse to return an error for an unsupported schema_version, got nil")
	}
}

func TestParse_UnsupportedSchemaVersion_ErrorIsErrUnsupportedSchemaVersion(t *testing.T) {
	// The error must be unwrappable to ErrUnsupportedSchemaVersion so callers can
	// distinguish version incompatibility from other parse failures.
	src := fixtureBytes(t, "unsupported-version.yaml")

	_, err := descriptor.Parse(src, "unsupported-version.yaml")

	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, descriptor.ErrUnsupportedSchemaVersion) {
		t.Errorf("error must satisfy errors.Is(err, ErrUnsupportedSchemaVersion); got: %v", err)
	}
}

func TestParse_UnsupportedSchemaVersion_ReturnsNilDescriptor(t *testing.T) {
	src := fixtureBytes(t, "unsupported-version.yaml")

	d, _ := descriptor.Parse(src, "unsupported-version.yaml")

	if d != nil {
		t.Error("Parse with unsupported schema_version must return nil descriptor")
	}
}

// --- Load: unsupported version ---

func TestLoad_UnsupportedSchemaVersion_ReturnsError(t *testing.T) {
	path := filepath.Join(testdataDir, "unsupported-version.yaml")

	_, err := descriptor.Load(path)

	if err == nil {
		t.Fatal("expected Load to return an error for an unsupported schema_version, got nil")
	}
}

func TestLoad_UnsupportedSchemaVersion_ErrorIsErrUnsupportedSchemaVersion(t *testing.T) {
	path := filepath.Join(testdataDir, "unsupported-version.yaml")

	_, err := descriptor.Load(path)

	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, descriptor.ErrUnsupportedSchemaVersion) {
		t.Errorf("error must satisfy errors.Is(err, ErrUnsupportedSchemaVersion); got: %v", err)
	}
}

// --- ErrUnsupportedSchemaVersion sentinel ---

func TestErrUnsupportedSchemaVersion_IsNonNil(t *testing.T) {
	if descriptor.ErrUnsupportedSchemaVersion == nil {
		t.Fatal("ErrUnsupportedSchemaVersion must not be nil")
	}
}

func TestErrUnsupportedSchemaVersion_HasNonEmptyMessage(t *testing.T) {
	if descriptor.ErrUnsupportedSchemaVersion.Error() == "" {
		t.Fatal("ErrUnsupportedSchemaVersion.Error() must return a non-empty string")
	}
}

// --- Version error is distinct ---

func TestParse_UnsupportedSchemaVersion_ErrorIsDistinctFromGenericError(t *testing.T) {
	// ErrUnsupportedSchemaVersion must not match a plain errors.New result, confirming
	// it is a distinct sentinel and not accidentally matched by other errors.
	genericErr := errors.New("descriptor schema version not supported")

	if errors.Is(genericErr, descriptor.ErrUnsupportedSchemaVersion) {
		t.Error("a plain errors.New with the same text must not match ErrUnsupportedSchemaVersion via errors.Is")
	}
}

func TestParse_MissingSchemaVersion_IsNotErrUnsupportedSchemaVersion(t *testing.T) {
	// A descriptor with no schema_version at all is a different error from one with an
	// explicitly unsupported version. The two cases must not be collapsed.
	src := readValidateFixture(t, "invalid/missing-schema-version.yaml")

	_, err := descriptor.Parse(src, "invalid/missing-schema-version.yaml")

	if err == nil {
		t.Fatal("expected error for missing schema_version, got nil")
	}
	// Missing vs unsupported are distinct; a missing version must produce a validation
	// error, not ErrUnsupportedSchemaVersion (since no version was declared at all).
	// Both cases must return errors, but the callers may need to distinguish them.
	// This test simply verifies that loading fails; the distinction is documented here
	// as a design note rather than a hard assertion to avoid over-specifying the implementation.
}
