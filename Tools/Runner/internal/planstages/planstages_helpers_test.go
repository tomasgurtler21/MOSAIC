package planstages_test

// Shared test helpers and constants for the planstages test suite.

import (
	"errors"
	"path/filepath"
	"testing"

	"mosaic-run/internal/domain"
)

const planstagesTestdataDir = "../../testdata/planstages"

// planstagesFixture returns the absolute path to a named fixture file.
func planstagesFixture(name string) string {
	return filepath.Join(planstagesTestdataDir, name)
}

// asRefusalError asserts that err wraps a *domain.RefusalError and returns it.
// Calls t.Fatal on failure.
func asRefusalError(t *testing.T, err error) *domain.RefusalError {
	t.Helper()
	var re *domain.RefusalError
	if !errors.As(err, &re) {
		t.Fatalf("want *domain.RefusalError, got %T: %v", err, err)
	}
	return re
}
