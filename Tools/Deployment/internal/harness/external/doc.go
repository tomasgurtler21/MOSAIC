// Package external implements domain.HarnessModule over a JSON-over-stdio child process protocol.
// It translates each HarnessModule method call into a single-line JSON request sent to the
// child process's stdin and reads the single-line JSON response from its stdout. Version
// negotiation, timeouts, and error distinction (missing executable, non-zero exit, malformed JSON,
// timeout) are handled here so callers cannot encounter a silent wrong result.
package external
