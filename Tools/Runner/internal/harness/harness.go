// Package harness provides the HarnessAdapter port and a fake implementation
// for use in tests. The fake adapter returns scripted protocol responses for
// given agent invocations, surfaces malformed responses as handleable errors
// (not panics), and handles context cancellation.
//
// Protocol serialisation helpers (MarshalRequest / UnmarshalResponse) encode
// and decode Communication Protocol v1.12 JSON messages.
package harness

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"mosaic-run/internal/domain"
)

// ScriptedEntry is one queued response for the MockAdapter.
//
// Exactly one of Response, Err, or RawJSON should be set per entry:
//   - Response: returned as the protocol response (happy-path entry).
//   - Err: returned as the harness-level error (error-path entry).
//   - RawJSON: passed through json.Unmarshal; an invalid JSON payload
//     produces an error return rather than a panic, exercising the
//     malformed-response handling path.
//
// An entry with none of the three fields set returns a sentinel error.
type ScriptedEntry struct {
	Response *domain.ProtocolResponse
	Err      error
	RawJSON  []byte
	Writes   []ScriptedWrite // optional; applied in order, also for Err entries
}

// ScriptedWrite is a file the MockAdapter writes during Invoke, before
// returning the scripted response or error.
type ScriptedWrite struct {
	Path    string // slash-separated, relative to the adapter's write root (dispatched form)
	Content string // written verbatim; parent directories are created
}

// Invocation records one call to MockAdapter.Invoke.
type Invocation struct {
	Agent   domain.AgentReference
	Request domain.ProtocolRequest
}

// MockAdapter implements domain.HarnessAdapter with scripted responses.
//
// Scripted entries are queued per agent identifier and consumed in FIFO order.
// When the queue for a given agent is exhausted, Invoke returns an error
// rather than blocking or panicking. All invocations are recorded so tests
// can assert call order and argument values.
type MockAdapter struct {
	queue       map[string][]ScriptedEntry
	invocations []Invocation
	writeRoot   string
}

// SetWriteRoot sets the directory ScriptedWrite paths are resolved against.
// Invoke with a non-empty Writes and no root set returns an error.
func (f *MockAdapter) SetWriteRoot(root string) {
	f.writeRoot = root
}

// NewMockAdapter returns a MockAdapter with an empty queue.
func NewMockAdapter() *MockAdapter {
	return &MockAdapter{queue: make(map[string][]ScriptedEntry)}
}

// Queue appends scripted entries for the given agent identifier.
// Entries are consumed in the order they were added (FIFO).
func (f *MockAdapter) Queue(agentID string, entries ...ScriptedEntry) {
	f.queue[agentID] = append(f.queue[agentID], entries...)
}

// Invocations returns all recorded invocations in call order.
func (f *MockAdapter) Invocations() []Invocation {
	return f.invocations
}

// RemainingQueueSize returns the total number of unconsumed scripted entries
// across all queued agents. A non-zero value after a run indicates that fewer
// agents were dispatched than expected, helping detect over-queued responses
// where the session dispatched fewer agents than the test author intended.
func (f *MockAdapter) RemainingQueueSize() int {
	total := 0
	for _, entries := range f.queue {
		total += len(entries)
	}
	return total
}

// Invoke implements domain.HarnessAdapter.
//
// It consumes the next scripted entry for the agent identifier and returns
// the scripted response or error. If the context is already cancelled, it
// returns ctx.Err() without consuming an entry. If the queue is exhausted,
// it returns a descriptive error.
func (f *MockAdapter) Invoke(ctx context.Context, agent domain.AgentReference, request domain.ProtocolRequest) (domain.ProtocolResponse, error) {
	// Honour context cancellation before consuming a scripted entry.
	select {
	case <-ctx.Done():
		return domain.ProtocolResponse{}, ctx.Err()
	default:
	}

	f.invocations = append(f.invocations, Invocation{Agent: agent, Request: request})

	entries := f.queue[agent.Identifier]
	if len(entries) == 0 {
		return domain.ProtocolResponse{}, fmt.Errorf("harness: no scripted response queued for agent %q", agent.Identifier)
	}

	entry := entries[0]
	f.queue[agent.Identifier] = entries[1:]

	if len(entry.Writes) > 0 {
		if f.writeRoot == "" {
			return domain.ProtocolResponse{}, fmt.Errorf("harness: scripted writes for agent %q but no write root set", agent.Identifier)
		}
		for _, w := range entry.Writes {
			dst := filepath.Join(f.writeRoot, filepath.FromSlash(w.Path))
			if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
				return domain.ProtocolResponse{}, fmt.Errorf("harness: scripted write %q: %w", w.Path, err)
			}
			if err := os.WriteFile(dst, []byte(w.Content), 0o644); err != nil {
				return domain.ProtocolResponse{}, fmt.Errorf("harness: scripted write %q: %w", w.Path, err)
			}
		}
	}

	if entry.Err != nil {
		return domain.ProtocolResponse{}, entry.Err
	}

	if entry.RawJSON != nil {
		var resp domain.ProtocolResponse
		if err := json.Unmarshal(entry.RawJSON, &resp); err != nil {
			return domain.ProtocolResponse{}, fmt.Errorf("harness: malformed protocol response from agent %q: %w", agent.Identifier, err)
		}
		return resp, nil
	}

	if entry.Response != nil {
		return *entry.Response, nil
	}

	return domain.ProtocolResponse{}, errors.New("harness: scripted entry has none of Response, Err, or RawJSON set")
}

// MarshalRequest serialises a ProtocolRequest to Communication Protocol v1.12
// JSON. Field names follow the json struct tags on ProtocolRequest.
func MarshalRequest(req domain.ProtocolRequest) ([]byte, error) {
	return json.Marshal(req)
}

// UnmarshalResponse parses Communication Protocol v1.12 JSON bytes into a
// ProtocolResponse. Returns an error if the bytes are not valid JSON.
func UnmarshalResponse(data []byte) (domain.ProtocolResponse, error) {
	var r domain.ProtocolResponse
	if err := json.Unmarshal(data, &r); err != nil {
		return domain.ProtocolResponse{}, err
	}
	return r, nil
}
