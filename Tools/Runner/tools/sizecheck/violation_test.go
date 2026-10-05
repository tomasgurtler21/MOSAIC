package main

// Tests for violation.String() output formatting.

import (
	"testing"
)

func TestViolationString_FileViolation(t *testing.T) {
	v := violation{
		Kind:  fileTooLong,
		Path:  "internal/engine/engine.go",
		Lines: 1311,
	}
	const want = "FILE internal/engine/engine.go: 1311 lines (limit 500)"
	if got := v.String(); got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}
}

func TestViolationString_FuncViolation_NoReceiver(t *testing.T) {
	v := violation{
		Kind:  funcTooLong,
		Path:  "internal/engine/engine.go",
		Lines: 200,
		Func:  "DoSomething",
		Start: 10,
		End:   209,
	}
	const want = "FUNC internal/engine/engine.go:10-209 DoSomething: 200 lines (limit 150)"
	if got := v.String(); got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}
}

func TestViolationString_FuncViolation_ValueReceiver(t *testing.T) {
	v := violation{
		Kind:  funcTooLong,
		Path:  "internal/session/session.go",
		Lines: 155,
		Func:  "Session.Start",
		Start: 50,
		End:   204,
	}
	const want = "FUNC internal/session/session.go:50-204 Session.Start: 155 lines (limit 150)"
	if got := v.String(); got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}
}

func TestViolationString_FuncViolation_PointerReceiver(t *testing.T) {
	v := violation{
		Kind:  funcTooLong,
		Path:  "internal/session/session.go",
		Lines: 160,
		Func:  "(*Session).Start",
		Start: 50,
		End:   209,
	}
	const want = "FUNC internal/session/session.go:50-209 (*Session).Start: 160 lines (limit 150)"
	if got := v.String(); got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}
}

// LinesEqualsEndMinusStartPlusOne checks the invariant Lines == End-Start+1.
func TestViolationLines_EqualsEndMinusStartPlusOne(t *testing.T) {
	v := violation{
		Kind:  funcTooLong,
		Path:  "pkg/foo.go",
		Lines: 160,
		Func:  "Foo",
		Start: 50,
		End:   209,
	}
	if want := v.End - v.Start + 1; v.Lines != want {
		t.Errorf("Lines = %d, want End-Start+1 = %d", v.Lines, want)
	}
}
