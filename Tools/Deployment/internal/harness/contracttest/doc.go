// Package contracttest provides the shared harness contract test suite. It drives any
// domain.HarnessModule implementation through a standard set of assertions covering universal
// invariants (determinism, idempotent Close, non-nil Descriptor, etc.) and per-method
// expectations supplied via Fixtures. Every built-in, the descriptor-only adapter, and the
// external adapter are verified by calling Run with appropriate fixtures, so no provision tier
// writes its own per-method tests.
package contracttest
