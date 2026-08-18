package main

// Tests for composition-root harness selection: the supported-harness set,
// adapter and decoder wiring, and the flag-validation consequence of the
// supported set.
//
// The adapter-coverage guard-rail drives both newAdapter and decoderFor from
// AgentTest's own supported set (not the whole shared catalog), so a catalog
// addition this tool does not yet wire breaks here — not silently as a late
// ErrUnknownHarness. Companion tests verify the supported set is a strict
// subset of the catalog (no phantom identity), and that catalog identities
// outside the supported set are actively rejected by both switches.

import (
	"errors"
	"io"
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

// TestCompositionRootSwitches_ResolveEverySupportedEntry is the adapter-
// coverage guard-rail: it drives both newAdapter and decoderFor from
// AgentTest's own supported set, so a supported identity this composition
// root forgets to wire fails here rather than reaching ErrUnknownHarness
// silently at runtime.
func TestCompositionRootSwitches_ResolveEverySupportedEntry(t *testing.T) {
	for _, entry := range supportedHarnesses() {
		t.Run(entry.ID, func(t *testing.T) {
			adapter, err := newAdapter(entry.ID, adapterOptions{})
			if err != nil {
				t.Errorf("newAdapter(%q, ...) returned an error: %v; every supported entry must resolve to an adapter", entry.ID, err)
			} else if adapter == nil {
				t.Errorf("newAdapter(%q, ...) returned a nil adapter with a nil error", entry.ID)
			} else if got := adapter.ID(); got != entry.ID {
				t.Errorf("newAdapter(%q, ...).ID() = %q, want %q", entry.ID, got, entry.ID)
			}

			dec, err := decoderFor(entry.ID)
			if err != nil {
				t.Errorf("decoderFor(%q) returned an error: %v; every supported entry must resolve to a decoder", entry.ID, err)
			} else if dec == nil {
				t.Errorf("decoderFor(%q) returned a nil decoder with a nil error", entry.ID)
			}
		})
	}
}

// TestSupportedHarnesses_IsSubsetOfSharedCatalog verifies the supported set
// contains no identity the shared catalog does not declare. A phantom identity
// in the supported set means a switch case that can never be exercised by a
// real catalog-sourced selection.
func TestSupportedHarnesses_IsSubsetOfSharedCatalog(t *testing.T) {
	for _, entry := range supportedHarnesses() {
		if _, ok := commonharness.LookupCLIHarness(entry.ID); !ok {
			t.Errorf("supported harness %q is not in the shared catalog: the supported set must be a subset of the catalog", entry.ID)
		}
	}
}

// TestUnsupportedCatalogIdentities_YieldErrUnknownHarness verifies that any
// catalog identity this tool has not yet wired is actively rejected by both
// newAdapter and decoderFor, rather than resolving to something unexpected.
// This test is a no-op while the supported set equals the full catalog; it
// becomes meaningful once a new catalog entry exists that this tool does not
// yet support.
func TestUnsupportedCatalogIdentities_YieldErrUnknownHarness(t *testing.T) {
	supported := supportedHarnesses()
	supportedIDs := make(map[string]bool, len(supported))
	for _, e := range supported {
		supportedIDs[e.ID] = true
	}

	for _, entry := range commonharness.CLIHarnesses() {
		if supportedIDs[entry.ID] {
			continue // covered by the resolve test above
		}
		t.Run(entry.ID, func(t *testing.T) {
			adapter, err := newAdapter(entry.ID, adapterOptions{})
			if err == nil {
				t.Errorf("newAdapter(%q, ...) succeeded for an unsupported catalog identity; want ErrUnknownHarness", entry.ID)
			} else if !errors.Is(err, ErrUnknownHarness) {
				t.Errorf("newAdapter(%q, ...) error = %v, want it to wrap ErrUnknownHarness", entry.ID, err)
			}
			if adapter != nil {
				t.Errorf("newAdapter(%q, ...) returned a non-nil adapter for an unsupported identity", entry.ID)
			}

			dec, err := decoderFor(entry.ID)
			if err == nil {
				t.Errorf("decoderFor(%q) succeeded for an unsupported catalog identity; want ErrUnknownHarness", entry.ID)
			} else if !errors.Is(err, ErrUnknownHarness) {
				t.Errorf("decoderFor(%q) error = %v, want it to wrap ErrUnknownHarness", entry.ID, err)
			}
			if dec != nil {
				t.Errorf("decoderFor(%q) returned a non-nil decoder for an unsupported identity", entry.ID)
			}
		})
	}
}

// TestCLIOptionsHarnesses_MatchSupportedSet verifies that the harness set
// offered to the CLI frontend comes from supportedHarnesses(), not the full
// shared catalog, so an identity the catalog declares but this tool does not
// support is never offered as a selectable value. If the supported set is
// narrower than the catalog (Stage 5 onward), this test ensures the gap is
// visible in flag validation rather than discovered as a late ErrUnknownHarness.
func TestCLIOptionsHarnesses_MatchSupportedSet(t *testing.T) {
	// cliOptions only reads d for non-Harnesses fields (WorkspaceRoot, FixtureRoot,
	// HarnessID, Preflight, Suite); a zero Deps is sufficient for checking
	// the Harnesses field without launching a frontend.
	opts := cliOptions(Deps{}, io.Discard, io.Discard)

	supported := supportedHarnesses()
	supportedIDs := make(map[string]bool, len(supported))
	for _, e := range supported {
		supportedIDs[e.ID] = true
	}

	// Every offered harness must be in the supported set.
	for _, h := range opts.Harnesses {
		if !supportedIDs[h.ID] {
			t.Errorf("cliOptions.Harnesses includes %q which is not in supportedHarnesses(): the frontend must not offer unsupported identities", h.ID)
		}
	}

	// Every supported identity must appear in the offered set.
	offeredIDs := make(map[string]bool, len(opts.Harnesses))
	for _, h := range opts.Harnesses {
		offeredIDs[h.ID] = true
	}
	for _, e := range supported {
		if !offeredIDs[e.ID] {
			t.Errorf("cliOptions.Harnesses is missing supported harness %q", e.ID)
		}
	}

	// Catalog identities not in the supported set must not appear in the
	// offered set — this is the assertion that protects against silently
	// offering ghcp-cli (or a future addition) before this tool can wire it.
	for _, entry := range commonharness.CLIHarnesses() {
		if !supportedIDs[entry.ID] && offeredIDs[entry.ID] {
			t.Errorf("cliOptions.Harnesses offers %q which is in the catalog but not in supportedHarnesses()", entry.ID)
		}
	}
}

// TestTUIOptionsHarnesses_MatchSupportedSet mirrors TestCLIOptionsHarnesses_MatchSupportedSet
// for the TUI frontend, ensuring both frontends offer an identical set sourced
// from supportedHarnesses(). An unsupported catalog identity must not be offered
// to either frontend; without this test an implementer who updates
// cliOptions.Harnesses but forgets tuiOptions.Harnesses would satisfy every
// other test while silently offering an unsupported catalog identity to TUI users.
func TestTUIOptionsHarnesses_MatchSupportedSet(t *testing.T) {
	// tuiOptions requires a non-empty WorkspaceRoot to pass its internal
	// newTUISuiteRunner check; workspace.NewManager performs no filesystem
	// validation at construction, so a placeholder string is sufficient.
	// We pass nil for suites — we are only inspecting the Harnesses field.
	opts, err := tuiOptions(Deps{WorkspaceRoot: "placeholder"}, nil)
	if err != nil {
		t.Fatalf("tuiOptions(Deps{WorkspaceRoot: \"placeholder\"}, nil) returned an error: %v", err)
	}

	supported := supportedHarnesses()
	supportedIDs := make(map[string]bool, len(supported))
	for _, e := range supported {
		supportedIDs[e.ID] = true
	}

	// Every offered harness must be in the supported set.
	for _, h := range opts.Harnesses {
		if !supportedIDs[h.ID] {
			t.Errorf("tuiOptions.Harnesses includes %q which is not in supportedHarnesses(): the frontend must not offer unsupported identities", h.ID)
		}
	}

	// Every supported identity must appear in the offered set.
	offeredIDs := make(map[string]bool, len(opts.Harnesses))
	for _, h := range opts.Harnesses {
		offeredIDs[h.ID] = true
	}
	for _, e := range supported {
		if !offeredIDs[e.ID] {
			t.Errorf("tuiOptions.Harnesses is missing supported harness %q", e.ID)
		}
	}

	// Catalog identities not in the supported set must not appear in the
	// offered set — this is the assertion that protects against silently
	// offering ghcp-cli (or a future addition) before this tool can wire it.
	for _, entry := range commonharness.CLIHarnesses() {
		if !supportedIDs[entry.ID] && offeredIDs[entry.ID] {
			t.Errorf("tuiOptions.Harnesses offers %q which is in the catalog but not in supportedHarnesses()", entry.ID)
		}
	}
}
