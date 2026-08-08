package hookbundle_test

// Tests for variant resolution: which variant a given harness ID consumes.
//
// Fixtures:
//   - resolution.yaml — default resolution, explicit override, reuses, unsupported, missing
//   - reuse-cycle.yaml — a reuses cycle, which must be refused rather than looping forever

import (
	"errors"
	"testing"

	"mosaic-common/hookbundle"
)

func parseResolutionFixture(t *testing.T) hookbundle.Manifest {
	t.Helper()
	data := readFixture(t, "resolution.yaml")
	m, err := hookbundle.Parse(data)
	if err != nil {
		t.Fatalf("Parse(resolution.yaml): unexpected error: %v", err)
	}
	return m
}

func TestResolveVariant_DefaultsToHarnessID(t *testing.T) {
	m := parseResolutionFixture(t)

	v, err := m.ResolveVariant("claude-code", "")
	if err != nil {
		t.Fatalf("ResolveVariant: unexpected error: %v", err)
	}
	if v.Key != "claude-code" {
		t.Errorf("resolved Variant.Key = %q, want %q", v.Key, "claude-code")
	}
	if len(v.Files) != 1 || v.Files[0].Source != "adapter.py" {
		t.Errorf("resolved Variant.Files = %+v, want the claude-code variant's own files", v.Files)
	}
}

func TestResolveVariant_ExplicitOverride(t *testing.T) {
	m := parseResolutionFixture(t)

	// Overriding to "claude-code" while asking for a harness ID that has no
	// variant of its own must resolve to the overridden key, not the harness ID.
	v, err := m.ResolveVariant("some-other-harness", "claude-code")
	if err != nil {
		t.Fatalf("ResolveVariant: unexpected error: %v", err)
	}
	if v.Key != "claude-code" {
		t.Errorf("resolved Variant.Key = %q, want %q (the override)", v.Key, "claude-code")
	}
}

func TestResolveVariant_FollowsReuses(t *testing.T) {
	m := parseResolutionFixture(t)

	// legacy-harness declares reuses: claude-code, so resolving it must yield
	// the effective (reused) variant's files.
	v, err := m.ResolveVariant("legacy-harness", "")
	if err != nil {
		t.Fatalf("ResolveVariant: unexpected error: %v", err)
	}
	if len(v.Files) != 1 || v.Files[0].Source != "adapter.py" {
		t.Errorf("resolved Variant.Files = %+v, want the reused claude-code variant's files", v.Files)
	}
}

func TestResolveVariant_Unsupported_ReturnsError(t *testing.T) {
	m := parseResolutionFixture(t)

	_, err := m.ResolveVariant("disabled-harness", "")
	if err == nil {
		t.Fatal("ResolveVariant: expected an error for an unsupported variant, got nil")
	}
}

func TestResolveVariant_Unsupported_WrapsUnsupportedSentinel(t *testing.T) {
	m := parseResolutionFixture(t)

	_, err := m.ResolveVariant("disabled-harness", "")
	if err == nil {
		t.Fatal("ResolveVariant: expected an error, got nil")
	}
	if !errors.Is(err, hookbundle.ErrVariantUnsupported) {
		t.Errorf("error does not wrap ErrVariantUnsupported via errors.Is: %v", err)
	}
}

func TestResolveVariant_Missing_ReturnsError(t *testing.T) {
	m := parseResolutionFixture(t)

	_, err := m.ResolveVariant("no-such-harness", "")
	if err == nil {
		t.Fatal("ResolveVariant: expected an error for a missing variant, got nil")
	}
}

func TestResolveVariant_Missing_WrapsMissingSentinel(t *testing.T) {
	m := parseResolutionFixture(t)

	_, err := m.ResolveVariant("no-such-harness", "")
	if err == nil {
		t.Fatal("ResolveVariant: expected an error, got nil")
	}
	if !errors.Is(err, hookbundle.ErrVariantMissing) {
		t.Errorf("error does not wrap ErrVariantMissing via errors.Is: %v", err)
	}
}

// TestResolveVariant_UnsupportedAndMissing_AreDistinct verifies that "not
// supported by this bundle" and "no such variant" are reported as different,
// non-equal sentinel errors, because they call for different messages to the
// user.
func TestResolveVariant_UnsupportedAndMissing_AreDistinct(t *testing.T) {
	m := parseResolutionFixture(t)

	_, unsupportedErr := m.ResolveVariant("disabled-harness", "")
	_, missingErr := m.ResolveVariant("no-such-harness", "")

	if unsupportedErr == nil || missingErr == nil {
		t.Fatal("expected both resolutions to return errors")
	}
	if errors.Is(unsupportedErr, hookbundle.ErrVariantMissing) {
		t.Error("the unsupported-variant error also matches ErrVariantMissing; these outcomes must be distinguishable")
	}
	if errors.Is(missingErr, hookbundle.ErrVariantUnsupported) {
		t.Error("the missing-variant error also matches ErrVariantUnsupported; these outcomes must be distinguishable")
	}
}

func TestResolveVariant_ReuseCycle_ReturnsError(t *testing.T) {
	data := readFixture(t, "reuse-cycle.yaml")
	m, err := hookbundle.Parse(data)
	if err != nil {
		t.Fatalf("Parse(reuse-cycle.yaml): unexpected error: %v", err)
	}

	_, err = m.ResolveVariant("a", "")
	if err == nil {
		t.Fatal("ResolveVariant: expected an error for a reuse cycle, got nil")
	}
	if !errors.Is(err, hookbundle.ErrReuseCycle) {
		t.Errorf("error does not wrap ErrReuseCycle via errors.Is: %v", err)
	}
}
