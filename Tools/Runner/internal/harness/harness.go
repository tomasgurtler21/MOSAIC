// Package harness provides the HarnessAdapter port and a fake implementation
// for use in tests. The fake adapter returns scripted protocol responses for
// given agent invocations, surfaces malformed responses as handleable errors
// (not panics), and handles context cancellation.
//
// Protocol serialisation helpers (MarshalRequest / UnmarshalResponse) encode
// and decode Communication Protocol v1.7 JSON messages.
package harness

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"mosaic-run/internal/domain"
)

// ScriptedEntry is one queued response for the FakeAdapter.
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
}

// Invocation records one call to FakeAdapter.Invoke.
type Invocation struct {
	Agent   domain.AgentReference
	Request domain.ProtocolRequest
}

// FakeAdapter implements domain.HarnessAdapter with scripted responses.
//
// Scripted entries are queued per agent identifier and consumed in FIFO order.
// When the queue for a given agent is exhausted, Invoke returns an error
// rather than blocking or panicking. All invocations are recorded so tests
// can assert call order and argument values.
type FakeAdapter struct {
	queue       map[string][]ScriptedEntry
	invocations []Invocation
}

// NewFakeAdapter returns a FakeAdapter with an empty queue.
func NewFakeAdapter() *FakeAdapter {
	return &FakeAdapter{queue: make(map[string][]ScriptedEntry)}
}

// Queue appends scripted entries for the given agent identifier.
// Entries are consumed in the order they were added (FIFO).
func (f *FakeAdapter) Queue(agentID string, entries ...ScriptedEntry) {
	f.queue[agentID] = append(f.queue[agentID], entries...)
}

// Invocations returns all recorded invocations in call order.
func (f *FakeAdapter) Invocations() []Invocation {
	return f.invocations
}

// RemainingQueueSize returns the total number of unconsumed scripted entries
// across all queued agents. A non-zero value after a run indicates that fewer
// agents were dispatched than expected, helping detect over-queued responses
// where the session dispatched fewer agents than the test author intended.
func (f *FakeAdapter) RemainingQueueSize() int {
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
func (f *FakeAdapter) Invoke(ctx context.Context, agent domain.AgentReference, request domain.ProtocolRequest) (domain.ProtocolResponse, error) {
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

// MarshalRequest serialises a ProtocolRequest to Communication Protocol v1.7
// JSON. Field names follow the json struct tags on ProtocolRequest.
func MarshalRequest(req domain.ProtocolRequest) ([]byte, error) {
	return json.Marshal(req)
}

// UnmarshalResponse parses Communication Protocol v1.7 JSON bytes into a
// ProtocolResponse. Returns an error if the bytes are not valid JSON.
func UnmarshalResponse(data []byte) (domain.ProtocolResponse, error) {
	var r domain.ProtocolResponse
	if err := json.Unmarshal(data, &r); err != nil {
		return domain.ProtocolResponse{}, err
	}
	return r, nil
}
