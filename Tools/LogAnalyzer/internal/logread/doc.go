// Package logread implements the domain.EventReader port. It streams
// JSONL event files line by line, decoding each line into a domain.Event.
// The reader is tolerant by design: malformed lines, truncated tails,
// and unrecognised event types all produce domain.Finding values and
// allow reading to continue, so a single bad line never aborts an entire
// file. File contents are never buffered whole into memory.
package logread
