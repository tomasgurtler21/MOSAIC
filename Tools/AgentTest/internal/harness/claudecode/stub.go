package claudecode

import "fmt"

// errNotImplemented marks a contract surface whose behaviour belongs to the
// implementation pass (I10.x) and has not been written yet. Every test
// exercising the named function is expected to fail against this stub —
// that is the TDD RED phase this file exists to make visible. It is
// deliberately not a panic: a caller composing several stubbed calls (for
// example the conformance suite) must see a normal Go error, not a crash.
func errNotImplemented(what string) error {
	return fmt.Errorf("claudecode: %s not implemented", what)
}
