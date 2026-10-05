package debuglog

import (
	"sync"
	"testing"

	"mosaic-run/internal/domain"
)

// ============================================================
// NopDebugLogger (domain.NopDebugLogger)
// ============================================================

func TestNopDebugLogger_ImplementsDebugLogger(t *testing.T) {
	// NopDebugLogger must satisfy the domain.DebugLogger interface.
	// This test fails at compile time if the type does not implement the interface.
	var _ domain.DebugLogger = domain.NopDebugLogger{}
}

func TestNopDebugLogger_ZeroValueIsUsable(t *testing.T) {
	// The zero value of NopDebugLogger must be usable without explicit initialisation.
	var nop domain.NopDebugLogger
	nop.Log("test.event", "message")
}

func TestNopDebugLogger_LogDoesNotPanic_NoFields(t *testing.T) {
	nop := domain.NopDebugLogger{}
	nop.Log("test.event", "message")
}

func TestNopDebugLogger_LogDoesNotPanic_WithFields(t *testing.T) {
	nop := domain.NopDebugLogger{}
	nop.Log("test.event", "message", domain.F("agent", "researcher#1"), domain.F("kind", "ordinary"))
}

func TestNopDebugLogger_LogDoesNotPanic_EmptyArgs(t *testing.T) {
	nop := domain.NopDebugLogger{}
	nop.Log("", "")
}

func TestNopDebugLogger_LogDoesNotPanic_MultiLineMessage(t *testing.T) {
	nop := domain.NopDebugLogger{}
	nop.Log("test.event", "line1\nline2\nline3\n")
}

func TestNopDebugLogger_CopyIsSafe(t *testing.T) {
	// NopDebugLogger may be copied freely and shared; each copy must be independently usable.
	nop1 := domain.NopDebugLogger{}
	nop2 := nop1 // value copy
	nop1.Log("test.event", "from copy 1")
	nop2.Log("test.event", "from copy 2")
}

func TestNopDebugLogger_ConcurrentLogIsSafe(t *testing.T) {
	// NopDebugLogger must be safe for concurrent use.
	nop := domain.NopDebugLogger{}
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			nop.Log("test.event", "concurrent call")
		}()
	}
	wg.Wait()
}

// ============================================================
// DebugField and F helper (domain)
// ============================================================

func TestDebugField_F_SetsKey(t *testing.T) {
	f := domain.F("mykey", "myvalue")
	if f.Key != "mykey" {
		t.Errorf("F() Key = %q, want %q", f.Key, "mykey")
	}
}

func TestDebugField_F_SetsValue(t *testing.T) {
	f := domain.F("mykey", "myvalue")
	if f.Value != "myvalue" {
		t.Errorf("F() Value = %q, want %q", f.Value, "myvalue")
	}
}

func TestDebugField_F_EmptyKeyAndValue(t *testing.T) {
	f := domain.F("", "")
	if f.Key != "" || f.Value != "" {
		t.Errorf("F(\"\", \"\") = {Key:%q, Value:%q}, want empty", f.Key, f.Value)
	}
}
