package artifact_test

// Tests for the ApprovalReader implementation.
//
// Coverage:
//
//   ReadApproval — file exists with a parseable frontmatter block:
//   - human_approved: true  →  ApprovalTrue
//   - human_approved: false →  ApprovalFalse
//   - frontmatter present, human_approved key absent →  ApprovalAbsent
//   - human_approved: maybe (value is neither "true" nor "false") →  ApprovalMalformed
//
//   ReadApproval — file structure issues:
//   - no frontmatter block (no leading --- delimiter) →  ApprovalNoFrontmatter
//   - file does not exist at the given path →  ApprovalFileMissing
//
//   IsApproved — gate semantics for every HumanApproval value:
//   - ApprovalTrue reports true (the only value that passes the gate).
//   - ApprovalFalse reports false.
//   - ApprovalAbsent reports false (absence is not the same as approved).
//   - ApprovalNoFrontmatter reports false.
//   - ApprovalMalformed reports false.
//   - ApprovalFileMissing reports false.
//   - ApprovalUnreadable reports false.

import (
	"context"
	"path/filepath"
	"testing"

	"mosaic-run/internal/artifact"
	"mosaic-run/internal/domain"
)

// hitlFixturePath returns the absolute path to a named HITL fixture file.
func hitlFixturePath(name string) string {
	return filepath.Join("..", "..", "testdata", "hitl", name)
}

// TestReadApproval_True verifies that a file with human_approved: true maps to
// ApprovalTrue.
func TestReadApproval_True(t *testing.T) {
	reader := artifact.NewApprovalReader()
	got := reader.ReadApproval(context.Background(), hitlFixturePath("approved.md"))
	if got != domain.ApprovalTrue {
		t.Errorf("approved.md: got %v, want ApprovalTrue (%v)", got, domain.ApprovalTrue)
	}
}

// TestReadApproval_False verifies that a file with human_approved: false maps
// to ApprovalFalse.
func TestReadApproval_False(t *testing.T) {
	reader := artifact.NewApprovalReader()
	got := reader.ReadApproval(context.Background(), hitlFixturePath("not-approved.md"))
	if got != domain.ApprovalFalse {
		t.Errorf("not-approved.md: got %v, want ApprovalFalse (%v)", got, domain.ApprovalFalse)
	}
}

// TestReadApproval_FieldAbsent verifies that a frontmatter block that contains
// no human_approved key maps to ApprovalAbsent, not ApprovalTrue.
func TestReadApproval_FieldAbsent(t *testing.T) {
	reader := artifact.NewApprovalReader()
	got := reader.ReadApproval(context.Background(), hitlFixturePath("field-absent.md"))
	if got != domain.ApprovalAbsent {
		t.Errorf("field-absent.md: got %v, want ApprovalAbsent (%v)", got, domain.ApprovalAbsent)
	}
}

// TestReadApproval_FieldAbsent_NotEquivalentToTrue is an explicit guard
// asserting that ApprovalAbsent is not treated as the gate being open.
func TestReadApproval_FieldAbsent_NotEquivalentToTrue(t *testing.T) {
	reader := artifact.NewApprovalReader()
	got := reader.ReadApproval(context.Background(), hitlFixturePath("field-absent.md"))
	if got == domain.ApprovalTrue {
		t.Errorf("field-absent.md: got ApprovalTrue; an absent field must not pass the gate")
	}
}

// TestReadApproval_MalformedValue verifies that a human_approved value that is
// neither "true" nor "false" maps to ApprovalMalformed.
func TestReadApproval_MalformedValue(t *testing.T) {
	reader := artifact.NewApprovalReader()
	got := reader.ReadApproval(context.Background(), hitlFixturePath("malformed-value.md"))
	if got != domain.ApprovalMalformed {
		t.Errorf("malformed-value.md: got %v, want ApprovalMalformed (%v)", got, domain.ApprovalMalformed)
	}
}

// TestReadApproval_NoFrontmatter verifies that a file with no --- delimiters
// maps to ApprovalNoFrontmatter.
func TestReadApproval_NoFrontmatter(t *testing.T) {
	reader := artifact.NewApprovalReader()
	got := reader.ReadApproval(context.Background(), hitlFixturePath("no-frontmatter.md"))
	if got != domain.ApprovalNoFrontmatter {
		t.Errorf("no-frontmatter.md: got %v, want ApprovalNoFrontmatter (%v)", got, domain.ApprovalNoFrontmatter)
	}
}

// TestReadApproval_FileMissing verifies that a path that names no existing file
// maps to ApprovalFileMissing.
func TestReadApproval_FileMissing(t *testing.T) {
	reader := artifact.NewApprovalReader()
	got := reader.ReadApproval(context.Background(), hitlFixturePath("does-not-exist.md"))
	if got != domain.ApprovalFileMissing {
		t.Errorf("missing file: got %v, want ApprovalFileMissing (%v)", got, domain.ApprovalFileMissing)
	}
}

// TestReadApproval_NoPanic_MalformedValue verifies that a malformed value does
// not cause a panic; the reader must degrade gracefully.
func TestReadApproval_NoPanic_MalformedValue(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("malformed-value.md: ReadApproval panicked: %v", r)
		}
	}()
	reader := artifact.NewApprovalReader()
	_ = reader.ReadApproval(context.Background(), hitlFixturePath("malformed-value.md"))
}

// TestReadApproval_NoPanic_FileMissing verifies that a missing file does not
// cause a panic.
func TestReadApproval_NoPanic_FileMissing(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("missing file: ReadApproval panicked: %v", r)
		}
	}()
	reader := artifact.NewApprovalReader()
	_ = reader.ReadApproval(context.Background(), hitlFixturePath("does-not-exist.md"))
}

// TestIsApproved_OnlyTruePassesGate verifies that IsApproved returns true only
// for ApprovalTrue and false for every other value.
func TestIsApproved_OnlyTruePassesGate(t *testing.T) {
	cases := []struct {
		value domain.HumanApproval
		name  string
		want  bool
	}{
		{domain.ApprovalTrue, "ApprovalTrue", true},
		{domain.ApprovalFalse, "ApprovalFalse", false},
		{domain.ApprovalAbsent, "ApprovalAbsent", false},
		{domain.ApprovalNoFrontmatter, "ApprovalNoFrontmatter", false},
		{domain.ApprovalMalformed, "ApprovalMalformed", false},
		{domain.ApprovalFileMissing, "ApprovalFileMissing", false},
		{domain.ApprovalUnreadable, "ApprovalUnreadable", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.value.IsApproved(); got != tc.want {
				t.Errorf("%s.IsApproved() = %v, want %v", tc.name, got, tc.want)
			}
		})
	}
}
