package main

// Tests for checkFile: file-size violations, function-size violations,
// receiver formatting, line counting, and error handling.
//
// writeGoFileLines and writeGoFileWithFunc are defined here and shared with
// run_test.go (same package).

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeGoFileLines writes a valid Go file with exactly totalLines newline
// characters, matching what wc -l counts. The file starts with a package
// declaration and fills remaining lines with comment lines.
func writeGoFileLines(t *testing.T, dir, name string, totalLines int) string {
	t.Helper()
	var sb strings.Builder
	sb.WriteString("package p\n")
	for i := 2; i <= totalLines; i++ {
		sb.WriteString(fmt.Sprintf("// line %d\n", i))
	}
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(sb.String()), 0o644); err != nil {
		t.Fatalf("writeGoFileLines: %v", err)
	}
	return path
}

// writeGoFileWithFunc writes a valid Go file containing exactly one function
// (BigFunc) that spans funcLines lines from the func keyword to the closing
// brace, inclusive. The file has a package declaration and blank line before
// the func so the func starts on line 3.
func writeGoFileWithFunc(t *testing.T, dir, name string, funcLines int) string {
	t.Helper()
	var sb strings.Builder
	sb.WriteString("package p\n\n") // lines 1-2
	// func keyword on line 3; closing brace on line (2 + funcLines)
	sb.WriteString("func BigFunc() {\n")
	for i := 0; i < funcLines-2; i++ {
		sb.WriteString(fmt.Sprintf("\t_ = %d\n", i))
	}
	sb.WriteString("}\n")
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(sb.String()), 0o644); err != nil {
		t.Fatalf("writeGoFileWithFunc: %v", err)
	}
	return path
}

func TestCheckFile_CompliantFile_NoViolations(t *testing.T) {
	root := t.TempDir()
	path := writeGoFileLines(t, root, "small.go", 10)
	violations, err := checkFile(root, path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(violations) != 0 {
		t.Errorf("got %d violations for a 10-line file, want 0: %v", len(violations), violations)
	}
}

func TestCheckFile_FileAtExactLimit_NoViolation(t *testing.T) {
	// Exactly 500 lines must NOT be flagged (rule is > 500).
	root := t.TempDir()
	path := writeGoFileLines(t, root, "atlimit.go", 500)
	violations, err := checkFile(root, path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, v := range violations {
		if v.Kind == fileTooLong {
			t.Errorf("file with exactly 500 lines flagged as too long (limit is > 500)")
		}
	}
}

func TestCheckFile_FileOverLimit_ReturnsFileViolation(t *testing.T) {
	// 501 lines must be flagged.
	root := t.TempDir()
	path := writeGoFileLines(t, root, "toolong.go", 501)
	violations, err := checkFile(root, path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var found bool
	for _, v := range violations {
		if v.Kind == fileTooLong {
			found = true
			if v.Lines != 501 {
				t.Errorf("file violation Lines = %d, want 501", v.Lines)
			}
		}
	}
	if !found {
		t.Error("expected fileTooLong violation for 501-line file, got none")
	}
}

func TestCheckFile_FuncAtExactLimit_NoViolation(t *testing.T) {
	// A function spanning exactly 150 lines must NOT be flagged (rule is > 150).
	root := t.TempDir()
	path := writeGoFileWithFunc(t, root, "atlimit.go", 150)
	violations, err := checkFile(root, path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, v := range violations {
		if v.Kind == funcTooLong {
			t.Errorf("function spanning exactly 150 lines flagged as too long (limit is > 150)")
		}
	}
}

func TestCheckFile_FuncOverLimit_ReturnsFuncViolation(t *testing.T) {
	// A function spanning 151 lines must be flagged.
	root := t.TempDir()
	path := writeGoFileWithFunc(t, root, "bigfunc.go", 151)
	violations, err := checkFile(root, path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var found bool
	for _, v := range violations {
		if v.Kind == funcTooLong {
			found = true
			if v.Lines != 151 {
				t.Errorf("func violation Lines = %d, want 151", v.Lines)
			}
			if v.Func != "BigFunc" {
				t.Errorf("func violation Func = %q, want %q", v.Func, "BigFunc")
			}
		}
	}
	if !found {
		t.Error("expected funcTooLong violation for 151-line function, got none")
	}
}

func TestCheckFile_FuncViolation_StartAndEndLines(t *testing.T) {
	// Verify that Start and End are set correctly. With our helper, func starts
	// on line 3 and the closing brace is on line 3 + funcLines - 1.
	root := t.TempDir()
	const funcLines = 151
	path := writeGoFileWithFunc(t, root, "lines.go", funcLines)
	violations, err := checkFile(root, path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, v := range violations {
		if v.Kind == funcTooLong {
			wantEnd := v.Start + funcLines - 1
			if v.End != wantEnd {
				t.Errorf("End = %d, want Start(%d) + %d - 1 = %d", v.End, v.Start, funcLines, wantEnd)
			}
			if v.Lines != funcLines {
				t.Errorf("Lines = %d, want %d", v.Lines, funcLines)
			}
		}
	}
}

func TestCheckFile_FuncViolation_ValueReceiver(t *testing.T) {
	root := t.TempDir()
	var sb strings.Builder
	sb.WriteString("package p\n\ntype T struct{}\n\n")
	// func keyword on line 5; 151-line span -> closing brace on line 155
	sb.WriteString("func (t T) Method() {\n")
	for i := 0; i < 149; i++ {
		sb.WriteString(fmt.Sprintf("\t_ = %d\n", i))
	}
	sb.WriteString("}\n")
	path := filepath.Join(root, "recv.go")
	if err := os.WriteFile(path, []byte(sb.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	violations, err := checkFile(root, path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var found bool
	for _, v := range violations {
		if v.Kind == funcTooLong {
			found = true
			if v.Func != "T.Method" {
				t.Errorf("value receiver func name = %q, want %q", v.Func, "T.Method")
			}
		}
	}
	if !found {
		t.Error("expected funcTooLong violation for value receiver method, got none")
	}
}

func TestCheckFile_FuncViolation_PointerReceiver(t *testing.T) {
	root := t.TempDir()
	var sb strings.Builder
	sb.WriteString("package p\n\ntype T struct{}\n\n")
	sb.WriteString("func (t *T) Method() {\n")
	for i := 0; i < 149; i++ {
		sb.WriteString(fmt.Sprintf("\t_ = %d\n", i))
	}
	sb.WriteString("}\n")
	path := filepath.Join(root, "ptrrecv.go")
	if err := os.WriteFile(path, []byte(sb.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	violations, err := checkFile(root, path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var found bool
	for _, v := range violations {
		if v.Kind == funcTooLong {
			found = true
			if v.Func != "(*T).Method" {
				t.Errorf("pointer receiver func name = %q, want %q", v.Func, "(*T).Method")
			}
		}
	}
	if !found {
		t.Error("expected funcTooLong violation for pointer receiver method, got none")
	}
}

func TestCheckFile_DocCommentExcludedFromFuncSpan(t *testing.T) {
	// A function with a 10-line doc comment must not have the doc lines counted.
	// The func keyword is on line 13 (package + blank + 10 doc lines + func).
	// If doc lines were counted, the span would be inflated by 10.
	root := t.TempDir()
	var sb strings.Builder
	sb.WriteString("package p\n\n")
	for i := 0; i < 10; i++ {
		sb.WriteString("// doc line\n")
	}
	// func keyword on line 13
	sb.WriteString("func BigFunc() {\n")
	// 149 body lines -> closing brace on line 163 -> span = 163 - 13 + 1 = 151 lines
	for i := 0; i < 149; i++ {
		sb.WriteString(fmt.Sprintf("\t_ = %d\n", i))
	}
	sb.WriteString("}\n")
	path := filepath.Join(root, "withdoc.go")
	if err := os.WriteFile(path, []byte(sb.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	violations, err := checkFile(root, path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var found bool
	for _, v := range violations {
		if v.Kind == funcTooLong && v.Func == "BigFunc" {
			found = true
			if v.Lines != 151 {
				t.Errorf("Lines = %d, want 151 (doc comment must not be counted)", v.Lines)
			}
		}
	}
	if !found {
		t.Error("expected funcTooLong for BigFunc with 151-line span, got none")
	}
}

func TestCheckFile_FuncWithoutBody_NotFlagged(t *testing.T) {
	// Interface methods and extern declarations have no body and must be ignored.
	root := t.TempDir()
	const src = `package p

type I interface {
	Foo()
	Bar(x int) string
}
`
	path := filepath.Join(root, "iface.go")
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	violations, err := checkFile(root, path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(violations) != 0 {
		t.Errorf("got %d violations for interface file, want 0: %v", len(violations), violations)
	}
}

func TestCheckFile_ViolationPathIsSlashSeparatedAndRelative(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "pkg")
	path := writeGoFileLines(t, sub, "toolong.go", 501)
	violations, err := checkFile(root, path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(violations) == 0 {
		t.Fatal("expected at least one violation, got none")
	}
	for _, v := range violations {
		if filepath.IsAbs(v.Path) {
			t.Errorf("violation Path is absolute: %s", v.Path)
		}
		if strings.Contains(v.Path, "\\") {
			t.Errorf("violation Path uses backslashes: %s", v.Path)
		}
		if v.Path != "pkg/toolong.go" {
			t.Errorf("violation Path = %q, want %q", v.Path, "pkg/toolong.go")
		}
	}
}

func TestCheckFile_FileViolationBeforeFuncViolation(t *testing.T) {
	// Within one path the file violation must come first in the returned slice.
	root := t.TempDir()
	// Build a file that is both >500 lines and contains a >150-line function.
	var sb strings.Builder
	sb.WriteString("package p\n\n")
	sb.WriteString("func BigFunc() {\n")
	for i := 0; i < 200; i++ {
		sb.WriteString(fmt.Sprintf("\t_ = %d\n", i))
	}
	sb.WriteString("}\n")
	// Pad to 501 lines total.
	content := sb.String()
	for i := strings.Count(content, "\n"); i < 501; i++ {
		content += fmt.Sprintf("// pad %d\n", i)
	}
	path := filepath.Join(root, "both.go")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	violations, err := checkFile(root, path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(violations) < 2 {
		t.Fatalf("expected at least 2 violations (file + func), got %d", len(violations))
	}
	if violations[0].Kind != fileTooLong {
		t.Errorf("first violation Kind = %v, want fileTooLong", violations[0].Kind)
	}
}

// TestCheckFile_ReturnsErrorForNonExistentFile verifies that checkFile returns a
// non-nil error when the given path does not exist. A genuinely unreadable
// (permission-denied) file is not exercised here because creating one portably
// on Windows is impractical.
func TestCheckFile_ReturnsErrorForNonExistentFile(t *testing.T) {
	root := t.TempDir()
	_, err := checkFile(root, filepath.Join(root, "nonexistent.go"))
	if err == nil {
		t.Error("expected error for nonexistent file, got nil")
	}
}

func TestCheckFile_ReturnsErrorForMalformedSyntax(t *testing.T) {
	// checkFile must return a non-nil error for a file with invalid Go syntax.
	// This exercises the "parse error" branch of the error handling contract.
	root := t.TempDir()
	const badSrc = `package p

func Broken( {
	// missing closing paren — syntax error
`
	path := filepath.Join(root, "bad.go")
	if err := os.WriteFile(path, []byte(badSrc), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := checkFile(root, path)
	if err == nil {
		t.Error("expected error for file with Go syntax error, got nil")
	}
}

func TestCheckFile_MultipleFuncViolations_SortedByStartLine(t *testing.T) {
	// When a file contains multiple over-limit functions, the returned function
	// violations must be ordered by start line.
	root := t.TempDir()
	var sb strings.Builder
	sb.WriteString("package p\n\n")
	// FirstFunc: func keyword on line 3, 149 body lines, closing brace on line 153.
	// Span = 153 - 3 + 1 = 151 (> 150, violates).
	sb.WriteString("func FirstFunc() {\n") // line 3
	for i := 0; i < 149; i++ {
		sb.WriteString(fmt.Sprintf("\t_ = %d\n", i))
	}
	sb.WriteString("}\n") // line 153
	sb.WriteString("\n")  // line 154 (blank separator)
	// SecondFunc: func keyword on line 155, 149 body lines, closing brace on line 305.
	// Span = 305 - 155 + 1 = 151 (> 150, violates).
	sb.WriteString("func SecondFunc() {\n") // line 155
	for i := 0; i < 149; i++ {
		sb.WriteString(fmt.Sprintf("\t_ = %d\n", i))
	}
	sb.WriteString("}\n") // line 305
	path := filepath.Join(root, "twofuncs.go")
	if err := os.WriteFile(path, []byte(sb.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	violations, err := checkFile(root, path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var funcViolations []violation
	for _, v := range violations {
		if v.Kind == funcTooLong {
			funcViolations = append(funcViolations, v)
		}
	}
	if len(funcViolations) < 2 {
		t.Fatalf("expected at least 2 funcTooLong violations, got %d: %v", len(funcViolations), funcViolations)
	}
	for i := 1; i < len(funcViolations); i++ {
		if funcViolations[i].Start <= funcViolations[i-1].Start {
			t.Errorf("funcViolations not sorted by Start: [%d].Start=%d >= [%d].Start=%d",
				i-1, funcViolations[i-1].Start, i, funcViolations[i].Start)
		}
	}
	if funcViolations[0].Func != "FirstFunc" {
		t.Errorf("first func violation Func = %q, want %q", funcViolations[0].Func, "FirstFunc")
	}
	if funcViolations[1].Func != "SecondFunc" {
		t.Errorf("second func violation Func = %q, want %q", funcViolations[1].Func, "SecondFunc")
	}
}

func TestCheckFile_FunctionLiteralCountsTowardEnclosingDecl(t *testing.T) {
	// Function literals (closures) must count toward their enclosing FuncDecl
	// rather than being ignored or double-counted. Build a function whose only
	// long body is inside a nested function literal.
	//
	// Layout (all lines are in Outer's span):
	//   line 3:  func Outer() {
	//   line 4:      go func() {
	//   lines 5 to 152:  body (148 lines)
	//   line 153:    }()
	//   line 154: }
	// Span of Outer = 154 - 3 + 1 = 152 lines (> 150, must violate).
	root := t.TempDir()
	var sb strings.Builder
	sb.WriteString("package p\n\n")
	sb.WriteString("func Outer() {\n")    // line 3
	sb.WriteString("\tgo func() {\n")     // line 4
	for i := 0; i < 148; i++ {           // lines 5-152
		sb.WriteString(fmt.Sprintf("\t\t_ = %d\n", i))
	}
	sb.WriteString("\t}()\n") // line 153
	sb.WriteString("}\n")     // line 154
	path := filepath.Join(root, "closure.go")
	if err := os.WriteFile(path, []byte(sb.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	violations, err := checkFile(root, path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var found bool
	for _, v := range violations {
		if v.Kind == funcTooLong && v.Func == "Outer" {
			found = true
			if v.Lines <= 150 {
				t.Errorf("Outer Lines = %d, want > 150 (closure body must count toward enclosing decl)", v.Lines)
			}
		}
	}
	if !found {
		t.Error("expected funcTooLong violation for Outer (closure body must count toward enclosing decl), got none")
	}
}
