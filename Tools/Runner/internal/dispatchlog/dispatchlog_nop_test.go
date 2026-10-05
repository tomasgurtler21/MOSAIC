package dispatchlog

import (
	"sync"
	"testing"

	"mosaic-run/internal/domain"
)

// ============================================================
// NopDispatchLogger (domain.NopDispatchLogger)
// ============================================================

func TestNopDispatchLogger_ImplementsDispatchLogger(t *testing.T) {
	// NopDispatchLogger must satisfy the domain.DispatchLogger interface.
	// This test fails at compile time if the type does not implement the interface.
	var _ domain.DispatchLogger = domain.NopDispatchLogger{}
}

func TestNopDispatchLogger_ZeroValueIsUsable(t *testing.T) {
	// The zero value of NopDispatchLogger must be usable without explicit initialisation.
	var nop domain.NopDispatchLogger
	nop.LogRequest(sampleRequest())
}

func TestNopDispatchLogger_LogRequestDoesNotPanic(t *testing.T) {
	nop := domain.NopDispatchLogger{}
	nop.LogRequest(domain.ProtocolRequest{})
}

func TestNopDispatchLogger_LogResponseDoesNotPanic(t *testing.T) {
	nop := domain.NopDispatchLogger{}
	nop.LogResponse(domain.ProtocolResponse{})
}

func TestNopDispatchLogger_LogErrorDoesNotPanic(t *testing.T) {
	nop := domain.NopDispatchLogger{}
	nop.LogError("agent#1", "harness failed")
}

func TestNopDispatchLogger_SetRunIDDoesNotPanic(t *testing.T) {
	nop := domain.NopDispatchLogger{}
	nop.SetRunID(validRunID)
}

func TestNopDispatchLogger_CloseDoesNotPanic(t *testing.T) {
	nop := domain.NopDispatchLogger{}
	nop.Close()
}

func TestNopDispatchLogger_PathReturnsEmpty(t *testing.T) {
	nop := domain.NopDispatchLogger{}
	if p := nop.Path(); p != "" {
		t.Errorf("NopDispatchLogger.Path() = %q, want empty string", p)
	}
}

func TestNopDispatchLogger_CopyIsSafe(t *testing.T) {
	// NopDispatchLogger may be copied freely; each copy must be independently usable.
	nop1 := domain.NopDispatchLogger{}
	nop2 := nop1 // value copy
	nop1.LogRequest(sampleRequest())
	nop2.LogResponse(sampleResponse())
}

func TestNopDispatchLogger_ConcurrentCallsAreSafe(t *testing.T) {
	// NopDispatchLogger must be safe for concurrent use.
	nop := domain.NopDispatchLogger{}
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			nop.LogRequest(sampleRequest())
			nop.LogResponse(sampleResponse())
			nop.LogError("agent#1", "err")
		}()
	}
	wg.Wait()
}
