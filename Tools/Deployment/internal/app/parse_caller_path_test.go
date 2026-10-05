package app_test

// parse_caller_path_test.go verifies that user-visible errors caused by a source file that
// cannot be parsed name that file, for the entry points whose error previously carried only the
// parse problem.
//
// Verified behaviours:
//   - Promote of a source file that is invalid UTF-8 returns an error that still matches
//     ErrPromoteNotTransformed and contains the file path.
//   - RenderAgent of a source file that is invalid UTF-8 returns an error that still matches
//     ErrRenderSourceNotGeneric and contains the file path.

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mosaic-deploy/internal/app"
	"mosaic-deploy/internal/app/interactiontest"
)

// invalidUTF8Agent is a frontmatter-bearing file whose body contains bytes that are not valid UTF-8.
var invalidUTF8Agent = []byte("---\ntransform_version: \"1.0\"\nversion: \"1.0\"\n---\nbody \xff\xfe broken\n")

// writeInvalidUTF8File writes invalidUTF8Agent as name in a fresh temp dir and returns its path.
func writeInvalidUTF8File(t *testing.T, name string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, invalidUTF8Agent, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return path
}

func TestPromote_UnparseableSource_ErrorNamesTheFile(t *testing.T) {
	// Arrange
	mosaicRoot := writeMosaicRoot(t)
	srcPath := writeInvalidUTF8File(t, "broken-agent.md")
	deps, _ := newPromoteDeps(t, interactiontest.NewBuilder().Build(), mosaicRoot)
	svc := app.New(deps)

	// Act
	_, err := svc.Promote(context.Background(), app.PromoteRequest{
		FilePath:  srcPath,
		Category:  "TestCategory",
		HarnessID: "stub-harness",
	})

	// Assert
	if err == nil {
		t.Fatal("Promote returned nil error for an unparseable source")
	}
	if !errors.Is(err, app.ErrPromoteNotTransformed) {
		t.Errorf("error = %v; want it to keep matching ErrPromoteNotTransformed", err)
	}
	if !strings.Contains(err.Error(), srcPath) {
		t.Errorf("error %q does not contain the source file path %q", err.Error(), srcPath)
	}
}

func TestRenderAgent_UnparseableSource_ErrorNamesTheFile(t *testing.T) {
	// Arrange
	srcPath := writeInvalidUTF8File(t, "broken-agent.md")
	svc := app.New(newRenderDeps(t))

	// Act
	_, err := svc.RenderAgent(context.Background(), app.RenderAgentRequest{
		SourcePath:      srcPath,
		TargetHarnessID: renderHarness.ID,
		DestinationPath: filepath.Join(t.TempDir(), "out.md"),
	})

	// Assert
	if err == nil {
		t.Fatal("RenderAgent returned nil error for an unparseable source")
	}
	if !errors.Is(err, app.ErrRenderSourceNotGeneric) {
		t.Errorf("error = %v; want it to keep matching ErrRenderSourceNotGeneric", err)
	}
	if !strings.Contains(err.Error(), srcPath) {
		t.Errorf("error %q does not contain the source file path %q", err.Error(), srcPath)
	}
}
