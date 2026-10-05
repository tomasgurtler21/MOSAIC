package seed_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mosaic-run/internal/domain"
	"mosaic-run/internal/seed"
)

// writeFile creates a file at path with the given content, creating intermediate
// directories as needed. It calls t.Fatal on failure.
func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("writeFile: MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("writeFile: %v", err)
	}
}

// assertRefusalError asserts err is a *domain.RefusalError with Component "seed"
// and that every wantSubstr appears somewhere in the rendered error string.
func assertRefusalError(t *testing.T, err error, wantSubstrs ...string) {
	t.Helper()
	var re *domain.RefusalError
	if !errors.As(err, &re) {
		t.Fatalf("want *domain.RefusalError, got %T: %v", err, err)
	}
	if re.Component != "seed" {
		t.Errorf("RefusalError.Component = %q, want \"seed\"", re.Component)
	}
	msg := re.Error()
	for _, sub := range wantSubstrs {
		if !strings.Contains(msg, sub) {
			t.Errorf("error message %q does not contain %q", msg, sub)
		}
	}
}

// assertZeroPlan asserts p is the zero Plan (contains no entries).
func assertZeroPlan(t *testing.T, p seed.Plan) {
	t.Helper()
	if !p.IsEmpty() {
		t.Errorf("expected zero Plan (IsEmpty true), got %d entries", len(p.Entries))
	}
}
