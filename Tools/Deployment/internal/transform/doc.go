// Package transform applies a harness transformation to a generic MOSAIC source file. Its
// exported entry point Apply is a pure function: bytes in, bytes out, with no filesystem access,
// no network calls, no clock reads, and no randomness. All document editing goes through
// docformat; no harness name appears in this package. The Report it returns is the audit trail
// consumed by logging, gap collection, and the TODO checklist.
package transform
