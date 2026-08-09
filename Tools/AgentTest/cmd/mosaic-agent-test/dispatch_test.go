package main

// Tests for composition-root harness selection covering the "opencode"
// addition: "opencode" must resolve to the opencode adapter and to its own
// exported decoder, exactly the way "claude-code" already resolves to the
// claudecode package's adapter and decoder, and an unrecognised identity
// must keep failing the same way it always has.
//
// TestCompositionRootSwitches_ResolveEveryCatalogEntry is the coverage test
// AC5.6 requires: it drives both switches from the shared catalog itself,
// so a future catalog addition that neither switch is updated for fails
// here rather than silently falling through to ErrUnknownHarness.

import (
	"errors"
	"reflect"
	"runtime"
	"testing"

	commonharness "mosaic-common/harness"

	"mosaic-agent-test/internal/harness/opencode"
)

func TestNewAdapter_OpenCode_ResolvesToOpenCodeAdapter(t *testing.T) {
	adapter, err := newAdapter(opencode.HarnessID, adapterOptions{})
	if err != nil {
		t.Fatalf("newAdapter(%q, ...) returned an error: %v", opencode.HarnessID, err)
	}
	if adapter == nil {
		t.Fatal("newAdapter returned a nil adapter with a nil error")
	}
	if got := adapter.ID(); got != opencode.HarnessID {
		t.Errorf("adapter.ID() = %q, want %q", got, opencode.HarnessID)
	}
}

func TestDecoderFor_OpenCode_MatchesOpenCodePackagesDecoder(t *testing.T) {
	dec, err := decoderFor(opencode.HarnessID)
	if err != nil {
		t.Fatalf("decoderFor(%q) returned an error: %v", opencode.HarnessID, err)
	}
	if dec == nil {
		t.Fatal("decoderFor returned a nil decoder with a nil error")
	}

	gotName := runtime.FuncForPC(reflect.ValueOf(dec).Pointer()).Name()
	wantName := runtime.FuncForPC(reflect.ValueOf(opencode.DecodeEnvelope).Pointer()).Name()
	if gotName != wantName {
		t.Errorf("decoderFor(%q) = %s, want the adapter's own opencode.DecodeEnvelope (%s)", opencode.HarnessID, gotName, wantName)
	}
}

// TestNewAdapter_UnrecognisedHarness_StillFailsAfterOpenCodeAddition
// verifies that adding "opencode" as a recognised value did not loosen the
// switch: an unrecognised identity is still ErrUnknownHarness, never a
// silent fallback to a default harness.
func TestNewAdapter_UnrecognisedHarness_StillFailsAfterOpenCodeAddition(t *testing.T) {
	adapter, err := newAdapter("not-a-real-harness", adapterOptions{})

	if err == nil {
		t.Fatal("newAdapter with an unrecognised harness returned a nil error")
	}
	if !errors.Is(err, ErrUnknownHarness) {
		t.Errorf("newAdapter error = %v, want it to wrap %v", err, ErrUnknownHarness)
	}
	if adapter != nil {
		t.Errorf("newAdapter with an unrecognised harness returned a non-nil adapter %v", adapter)
	}
}

// TestCompositionRootSwitches_ResolveEveryCatalogEntry verifies both
// newAdapter and decoderFor resolve every identity the shared CLI-harness
// catalog declares — driven from the catalog itself, so a catalog addition
// this composition root forgets to wire fails here rather than passing
// silently.
func TestCompositionRootSwitches_ResolveEveryCatalogEntry(t *testing.T) {
	for _, entry := range commonharness.CLIHarnesses() {
		t.Run(entry.ID, func(t *testing.T) {
			adapter, err := newAdapter(entry.ID, adapterOptions{})
			if err != nil {
				t.Errorf("newAdapter(%q, ...) returned an error: %v; every catalog entry must resolve to an adapter", entry.ID, err)
			} else if adapter == nil {
				t.Errorf("newAdapter(%q, ...) returned a nil adapter with a nil error", entry.ID)
			} else if got := adapter.ID(); got != entry.ID {
				t.Errorf("newAdapter(%q, ...).ID() = %q, want %q", entry.ID, got, entry.ID)
			}

			dec, err := decoderFor(entry.ID)
			if err != nil {
				t.Errorf("decoderFor(%q) returned an error: %v; every catalog entry must resolve to a decoder", entry.ID, err)
			} else if dec == nil {
				t.Errorf("decoderFor(%q) returned a nil decoder with a nil error", entry.ID)
			}
		})
	}
}
