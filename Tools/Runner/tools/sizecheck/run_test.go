package main

// Integration tests for run(): exit codes, output format, flags, and error handling.

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRun_CompliantTree_ExitsZeroWithOKSummary(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	code := run([]string{"-root", root}, &stdout, &stderr)
	if code != 0 {
		t.Errorf("exit code = %d, want 0; stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "size limits OK") {
		t.Errorf("stdout = %q, want it to contain %q", stdout.String(), "size limits OK")
	}
}

func TestRun_FileSizeViolation_ExitsOne(t *testing.T) {
	root := t.TempDir()
	writeGoFileLines(t, root, "main.go", 501)
	var stdout, stderr bytes.Buffer
	code := run([]string{"-root", root}, &stdout, &stderr)
	if code != 1 {
		t.Errorf("exit code = %d, want 1; stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "FILE") {
		t.Errorf("stdout = %q does not contain FILE violation line", stdout.String())
	}
	if !strings.Contains(stdout.String(), "violation(s)") {
		t.Errorf("stdout = %q does not contain violation summary", stdout.String())
	}
}

func TestRun_FuncSizeViolation_ExitsOne(t *testing.T) {
	root := t.TempDir()
	writeGoFileWithFunc(t, root, "big.go", 151)
	var stdout, stderr bytes.Buffer
	code := run([]string{"-root", root}, &stdout, &stderr)
	if code != 1 {
		t.Errorf("exit code = %d, want 1; stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "FUNC") {
		t.Errorf("stdout = %q does not contain FUNC violation line", stdout.String())
	}
}

func TestRun_UnknownFlag_ExitsTwoWithStderrMessage(t *testing.T) {
	root := t.TempDir()
	var stdout, stderr bytes.Buffer
	code := run([]string{"-root", root, "-unknownflag123"}, &stdout, &stderr)
	if code != 2 {
		t.Errorf("exit code = %d, want 2 for unknown flag", code)
	}
	if stderr.Len() == 0 {
		t.Error("expected error message on stderr for unknown flag, got none")
	}
}

func TestRun_NonExistentPath_ExitsTwo(t *testing.T) {
	root := t.TempDir()
	var stdout, stderr bytes.Buffer
	code := run([]string{"-root", root, filepath.Join(root, "does-not-exist")}, &stdout, &stderr)
	if code != 2 {
		t.Errorf("exit code = %d, want 2 for non-existent path", code)
	}
}

func TestRun_NonGoFileArgument_ExitsTwo(t *testing.T) {
	root := t.TempDir()
	notGo := filepath.Join(root, "README.md")
	if err := os.WriteFile(notGo, []byte("# readme\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	code := run([]string{"-root", root, notGo}, &stdout, &stderr)
	if code != 2 {
		t.Errorf("exit code = %d, want 2 for non-.go file argument", code)
	}
}

func TestRun_ToolErrorGoesToStderrNotStdout(t *testing.T) {
	root := t.TempDir()
	var stdout, stderr bytes.Buffer
	run([]string{"-root", root, filepath.Join(root, "nonexistent")}, &stdout, &stderr)
	if stderr.Len() == 0 {
		t.Error("expected error message on stderr, got none")
	}
	// Error message must start with "sizecheck: "
	if !strings.Contains(stderr.String(), "sizecheck:") {
		t.Errorf("stderr = %q, want it to start with \"sizecheck:\"", stderr.String())
	}
}

func TestRun_ExportsFlag_ExitsZero(t *testing.T) {
	root := t.TempDir()
	pkgDir := filepath.Join(root, "pkg")
	if err := os.MkdirAll(pkgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pkgDir, "pkg.go"), []byte("package p\nfunc Foo() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	code := run([]string{"-root", root, "-exports"}, &stdout, &stderr)
	if code != 0 {
		t.Errorf("exit code = %d, want 0 for -exports; stderr=%q", code, stderr.String())
	}
}

func TestRun_ExportsFlag_PrintsExportLines(t *testing.T) {
	root := t.TempDir()
	pkgDir := filepath.Join(root, "pkg")
	if err := os.MkdirAll(pkgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pkgDir, "pkg.go"), []byte("package p\nfunc Foo() {}\nfunc Bar() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	run([]string{"-root", root, "-exports"}, &stdout, &stderr)
	if !strings.Contains(stdout.String(), "EXPORTS") {
		t.Errorf("stdout = %q, want EXPORTS lines", stdout.String())
	}
	if !strings.Contains(stdout.String(), "pkg") {
		t.Errorf("stdout = %q, want package directory name", stdout.String())
	}
}

func TestRun_ExportsFlag_NeverExitsOneForSizeViolations(t *testing.T) {
	// -exports is informational only; size violations must not cause exit 1.
	root := t.TempDir()
	writeGoFileLines(t, root, "main.go", 501)
	var stdout, stderr bytes.Buffer
	code := run([]string{"-root", root, "-exports"}, &stdout, &stderr)
	if code != 0 {
		t.Errorf("exit code = %d, want 0 (-exports is informational, no size checks)", code)
	}
}

func TestRun_ExportsFlag_SkipsTestFiles(t *testing.T) {
	root := t.TempDir()
	pkgDir := filepath.Join(root, "pkg")
	if err := os.MkdirAll(pkgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Production file: 1 exported func.
	if err := os.WriteFile(filepath.Join(pkgDir, "pkg.go"), []byte("package p\nfunc Exported() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Test file: 1 exported func that must NOT be counted.
	if err := os.WriteFile(filepath.Join(pkgDir, "pkg_test.go"), []byte("package p\nfunc TestFoo(t interface{}) {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	run([]string{"-root", root, "-exports"}, &stdout, &stderr)
	// The count should be 1, not 2.
	if strings.Contains(stdout.String(), ": 2") {
		t.Errorf("stdout = %q; test file exports must not be counted", stdout.String())
	}
}

func TestRun_RootFlag_OutputUsesRelativePaths(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "subpkg")
	writeGoFileLines(t, sub, "big.go", 501)
	var stdout, stderr bytes.Buffer
	run([]string{"-root", root, sub}, &stdout, &stderr)
	if strings.Contains(stdout.String(), root) {
		t.Errorf("output contains absolute root path; should use relative: stdout=%q", stdout.String())
	}
}

func TestRun_OutputSortedByPath(t *testing.T) {
	root := t.TempDir()
	writeGoFileLines(t, filepath.Join(root, "zzz"), "toolong.go", 501)
	writeGoFileLines(t, filepath.Join(root, "aaa"), "toolong.go", 501)
	var stdout, stderr bytes.Buffer
	run([]string{"-root", root}, &stdout, &stderr)
	var filelines []string
	for _, l := range strings.Split(stdout.String(), "\n") {
		if strings.HasPrefix(l, "FILE") {
			filelines = append(filelines, l)
		}
	}
	if len(filelines) < 2 {
		t.Fatalf("expected at least 2 FILE lines, got %d: %v", len(filelines), filelines)
	}
	for i := 1; i < len(filelines); i++ {
		if filelines[i] < filelines[i-1] {
			t.Errorf("output not sorted by path: %q before %q", filelines[i-1], filelines[i])
		}
	}
}

func TestRun_ViolationCountInSummary(t *testing.T) {
	root := t.TempDir()
	// Create two violating files.
	writeGoFileLines(t, filepath.Join(root, "a"), "big.go", 501)
	writeGoFileLines(t, filepath.Join(root, "b"), "big.go", 501)
	var stdout, stderr bytes.Buffer
	run([]string{"-root", root}, &stdout, &stderr)
	out := stdout.String()
	// Summary must be "2 size violation(s)" or similar.
	if !strings.Contains(out, "2 size violation") {
		t.Errorf("stdout = %q, want summary to mention 2 violations", out)
	}
}

func TestRun_ParseError_ExitsTwoWithNoPartialReport(t *testing.T) {
	// When a walked .go file contains invalid Go syntax, run must exit 2,
	// write a message to stderr (prefixed "sizecheck:"), and produce no
	// partial report on stdout. The contract states: "With a parse error,
	// the run stops with exit 2. It does not produce a partial report."
	root := t.TempDir()
	const badSrc = `package p

func Broken( {
`
	if err := os.WriteFile(filepath.Join(root, "bad.go"), []byte(badSrc), 0o644); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	code := run([]string{"-root", root}, &stdout, &stderr)
	if code != 2 {
		t.Errorf("exit code = %d, want 2 for parse error; stdout=%q stderr=%q",
			code, stdout.String(), stderr.String())
	}
	// No partial report: stdout must not contain FILE or FUNC violation lines.
	if strings.Contains(stdout.String(), "FILE") || strings.Contains(stdout.String(), "FUNC") {
		t.Errorf("stdout contains partial report on parse error: %q", stdout.String())
	}
	if stderr.Len() == 0 {
		t.Error("expected error message on stderr for parse error, got none")
	}
	if !strings.Contains(stderr.String(), "sizecheck:") {
		t.Errorf("stderr = %q, want it to contain \"sizecheck:\"", stderr.String())
	}
}
