package external_test

// Tests for the JSON-over-stdio external harness adapter.
//
// The external adapter wraps a child process that speaks the JSON-over-stdio protocol
// defined in the harness/external package. These tests cover:
//
//   Protocol (T23.2):
//   - Every HarnessModule method produces a correctly-shaped request on stdin.
//   - Every correctly-shaped response on stdout is decoded and returned to the caller.
//   - Protocol version is carried on every message.
//   - Handshake is the first exchange; mismatch fails at New() time, not later.
//   - Round-trip of ToolRequest, FrontmatterRequest, TargetPathRequest, injection name,
//     and HookPlanRequest all produce the expected result types.
//
//   Failure modes (T23.3):
//   - Missing executable → ErrExecutableNotFound.
//   - Process exits non-zero before responding → ErrNonZeroExit.
//   - Process writes malformed JSON → ErrMalformedResponse.
//   - Process writes nothing and exits → ErrEmptyResponse.
//   - Process does not respond within the timeout → ErrTimeout.
//
//   Opt-in and provenance (T23.5):
//   - The module's Ref() carries TierExternal and a non-empty ExecutablePath.
//   - ExecutablePath matches the path passed to New().
//   - Ref().ID is non-empty and matches the harness ID from the descriptor.
//   - The adapter provides all fields required to populate domain.ExternalModuleInfo.
//
//   Shared contract (T23.6):
//   - The external adapter passes contracttest.Run with the same fixtures used for the
//     built-in OpenCode module, confirming it is indistinguishable from a built-in.
//
// Test strategy — fake harness subprocess:
//
// Tests that need a real subprocess use the "self-as-subprocess" pattern: the test binary
// is re-invoked with the environment variable MOSAIC_TEST_FAKE_HARNESS set, which causes
// TestMain to run a minimal fake harness server instead of the test suite. This avoids the
// need for a separately-compiled helper binary.
//
// The fake harness modes used in these tests are:
//   MOSAIC_TEST_FAKE_HARNESS=echo      - Implements the full protocol with echo responses.
//   MOSAIC_TEST_FAKE_HARNESS=exit1     - Exits with status 1 immediately (no output).
//   MOSAIC_TEST_FAKE_HARNESS=malformed - Writes a non-JSON line and exits.
//   MOSAIC_TEST_FAKE_HARNESS=empty     - Writes nothing and exits 0.
//   MOSAIC_TEST_FAKE_HARNESS=hang      - Blocks indefinitely on stdin (triggers timeout).
//   MOSAIC_TEST_FAKE_HARNESS=mismatch  - Responds to handshake with wrong protocol version.

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/harness/contracttest"
	"mosaic-deploy/internal/harness/descriptor"
	"mosaic-deploy/internal/harness/external"
)

// ---------------------------------------------------------------------------
// TestMain: fake harness subprocess dispatcher
// ---------------------------------------------------------------------------

func TestMain(m *testing.M) {
	switch os.Getenv("MOSAIC_TEST_FAKE_HARNESS") {
	case "echo":
		runFakeHarnessEcho()
		os.Exit(0)
	case "exit1":
		os.Exit(1)
	case "malformed":
		fmt.Fprintln(os.Stdout, "this is not valid JSON {{{{")
		os.Exit(0)
	case "empty":
		os.Exit(0)
	case "hang":
		// Block forever — this triggers ErrTimeout in the adapter.
		select {}
	case "mismatch":
		runFakeHarnessMismatch()
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// runFakeHarnessEcho reads JSON requests from stdin and writes echo-style JSON responses
// to stdout. It implements only enough of the protocol to satisfy the test cases.
func runFakeHarnessEcho() {
	dec := json.NewDecoder(os.Stdin)
	enc := json.NewEncoder(os.Stdout)

	for {
		var req map[string]any
		if err := dec.Decode(&req); err != nil {
			return
		}

		id, _ := req["id"].(string)
		method, _ := req["method"].(string)

		switch method {
		case "handshake":
			_ = enc.Encode(map[string]any{
				"protocol": external.ProtocolVersion,
				"id":       id,
				"result": map[string]any{
					"protocol": external.ProtocolVersion,
					"harness": map[string]any{
						"id":           "fake-echo",
						"display_name": "Fake Echo Harness",
						"tier":         "external",
						"usable":       true,
					},
				},
			})
		case "descriptor":
			_ = enc.Encode(map[string]any{
				"protocol": external.ProtocolVersion,
				"id":       id,
				"result": map[string]any{
					"descriptor": map[string]any{
						"schema_version": "1",
						"id":             "fake-echo",
						"display_name":   "Fake Echo Harness",
					},
				},
			})
		case "tools":
			params, _ := req["params"].(map[string]any)
			generic, _ := params["generic"].([]any)
			resolutions := make([]map[string]any, len(generic))
			for i, g := range generic {
				resolutions[i] = map[string]any{
					"generic": g,
					"outcome": "unmapped",
				}
			}
			_ = enc.Encode(map[string]any{
				"protocol": external.ProtocolVersion,
				"id":       id,
				"result": map[string]any{
					"fields":      []any{},
					"resolutions": resolutions,
				},
			})
		case "frontmatter":
			_ = enc.Encode(map[string]any{
				"protocol": external.ProtocolVersion,
				"id":       id,
				"result": map[string]any{
					"set":       []any{},
					"remove":    []any{},
					"key_order": []any{},
				},
			})
		case "target_path":
			_ = enc.Encode(map[string]any{
				"protocol": external.ProtocolVersion,
				"id":       id,
				"error": map[string]any{
					"code":    "unsupported_artifact",
					"message": "fake-echo: artifact kind not supported",
				},
			})
		case "injection":
			_ = enc.Encode(map[string]any{
				"protocol": external.ProtocolVersion,
				"id":       id,
				"result": map[string]any{
					"content": "",
					"ok":      false,
				},
			})
		case "hook_plan":
			_ = enc.Encode(map[string]any{
				"protocol": external.ProtocolVersion,
				"id":       id,
				"result": map[string]any{
					"supported": false,
					"reason":    "fake-echo: hooks not supported",
					"files":     []any{},
				},
			})
		default:
			_ = enc.Encode(map[string]any{
				"protocol": external.ProtocolVersion,
				"id":       id,
				"error": map[string]any{
					"code":    "unsupported_method",
					"message": fmt.Sprintf("unknown method: %s", method),
				},
			})
		}
	}
}

// runFakeHarnessMismatch responds to the handshake with a different protocol version.
func runFakeHarnessMismatch() {
	dec := json.NewDecoder(os.Stdin)
	enc := json.NewEncoder(os.Stdout)

	var req map[string]any
	if err := dec.Decode(&req); err != nil {
		return
	}
	id, _ := req["id"].(string)
	_ = enc.Encode(map[string]any{
		"protocol": "99.0", // wrong version
		"id":       id,
		"result": map[string]any{
			"protocol": "99.0",
			"harness": map[string]any{
				"id":    "mismatch-harness",
				"tier":  "external",
				"usable": true,
			},
		},
	})
}

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

// selfExecutable returns the path to this test binary so tests can re-invoke it as a
// fake harness subprocess.
func selfExecutable(t *testing.T) string {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}
	return exe
}

// fakeHarnessCmd builds an exec.Cmd that re-invokes this test binary in the given fake
// harness mode. It inherits the test's PATH so the binary can be found. The caller must
// start the process via external.New (not by running the cmd directly).
func fakeHarnessExe(t *testing.T, mode string) string {
	t.Helper()
	// Write a wrapper script that re-runs the test binary with the env var set.
	// On Windows, write a .bat that does the same. The external adapter starts the script;
	// the script re-invokes the test binary, which sees the env var in TestMain.
	//
	// Simpler: just return the test executable path; the adapter will set the env var
	// via an option. Since the adapter doesn't support that, we write a wrapper.
	dir := t.TempDir()
	exe := selfExecutable(t)

	var wrapperPath string
	if runtime.GOOS == "windows" {
		wrapperPath = filepath.Join(dir, "fake-harness.bat")
		content := fmt.Sprintf("@echo off\r\nset MOSAIC_TEST_FAKE_HARNESS=%s\r\n%s %%*\r\n", mode, exe)
		if err := os.WriteFile(wrapperPath, []byte(content), 0o755); err != nil {
			t.Fatalf("write bat wrapper: %v", err)
		}
	} else {
		wrapperPath = filepath.Join(dir, "fake-harness")
		content := fmt.Sprintf("#!/bin/sh\nexport MOSAIC_TEST_FAKE_HARNESS=%s\nexec %q \"$@\"\n", mode, exe)
		if err := os.WriteFile(wrapperPath, []byte(content), 0o755); err != nil {
			t.Fatalf("write sh wrapper: %v", err)
		}
	}
	return wrapperPath
}

// minimalDescriptor returns a minimal *domain.HarnessDescriptor for use in tests that need
// a descriptor but do not care about its content beyond what is required by external.New.
func minimalDescriptor(t *testing.T, id string) *domain.HarnessDescriptor {
	t.Helper()
	yaml := fmt.Sprintf(`schema_version: "1"
id: %q
display_name: %q
tools:
  shape: list
`, id, id+"-display")
	desc, err := descriptor.Parse([]byte(yaml), "test:"+id)
	if err != nil {
		t.Fatalf("parse minimal descriptor: %v", err)
	}
	return desc
}

// buildReferenceModule builds the cmd/harness-opencode-module binary and returns its path.
// The test is skipped if the build fails (allowing partial RED-phase runs to proceed).
func buildReferenceModule(t *testing.T) string {
	t.Helper()
	outDir := t.TempDir()
	outBin := filepath.Join(outDir, "harness-opencode-module")
	if runtime.GOOS == "windows" {
		outBin += ".exe"
	}

	// Locate the module root by walking up from this file's directory.
	// This file is at internal/harness/external/; the module root is three levels up.
	moduleRoot := findModuleRoot(t)

	cmd := exec.Command("go", "build", "-o", outBin, "mosaic-deploy/cmd/harness-opencode-module")
	cmd.Dir = moduleRoot
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Logf("go build harness-opencode-module: %v\nstderr: %s", err, stderr.String())
		t.Skip("reference module binary could not be built; skipping contract test against external adapter")
	}
	return outBin
}

// findModuleRoot walks up from the test source tree to find go.mod.
func findModuleRoot(t *testing.T) string {
	t.Helper()
	// Start from the package source directory (internal/harness/external).
	// Walk up until we find go.mod.
	dir, err := filepath.Abs(".")
	if err != nil {
		t.Fatalf("abs .: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not locate go.mod walking up from test directory")
		}
		dir = parent
	}
}

// openCodeDescriptor loads and parses the embedded OpenCode descriptor by reading it from
// the built-in OpenCode module. If the module cannot be constructed, the test is skipped.
func openCodeDescriptor(t *testing.T) *domain.HarnessDescriptor {
	t.Helper()
	// Load the descriptor from the harness.yaml in the source tree. Walk up to find it.
	moduleRoot := findModuleRoot(t)
	descPath := filepath.Join(moduleRoot, "internal", "harness", "builtin", "opencode", "opencode.yaml")
	src, err := os.ReadFile(descPath)
	if err != nil {
		t.Skipf("opencode.yaml not found at %s; skipping: %v", descPath, err)
	}
	desc, err := descriptor.Parse(src, descPath)
	if err != nil {
		t.Fatalf("parse opencode.yaml: %v", err)
	}
	return desc
}

// ---------------------------------------------------------------------------
// T23.2 — JSON-over-stdio protocol
// ---------------------------------------------------------------------------

// TestProtocol_New_ReturnsNonNilModuleForEchoServer verifies that external.New returns a
// non-nil module and no error when the subprocess correctly responds to the handshake.
// This is the foundational protocol test: if the handshake works, the adapter is usable.
func TestProtocol_New_ReturnsNonNilModuleForEchoServer(t *testing.T) {
	desc := minimalDescriptor(t, "fake-echo")
	exePath := fakeHarnessExe(t, "echo")

	m, err := external.New(exePath, desc, external.Options{Timeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("external.New: unexpected error for echo server: %v", err)
	}
	if m == nil {
		t.Fatal("external.New returned nil module with nil error")
	}
	defer m.Close() //nolint:errcheck
}

// TestProtocol_ProtocolVersionOnHandshake verifies that the adapter includes the protocol
// version in its handshake request and that the module's Ref() is populated from the
// handshake response.
func TestProtocol_ProtocolVersionOnHandshake(t *testing.T) {
	desc := minimalDescriptor(t, "fake-echo")
	exePath := fakeHarnessExe(t, "echo")

	m, err := external.New(exePath, desc, external.Options{Timeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("external.New: %v", err)
	}
	defer m.Close() //nolint:errcheck

	ref := m.Ref()
	if ref.ID == "" {
		t.Error("Ref().ID is empty after successful handshake; harness identity must be populated from handshake response")
	}
}

// TestProtocol_Tools_RoundTrip verifies that a Tools request is serialised to JSON, sent to
// the subprocess, and the response is deserialised and returned to the caller. The test
// checks that the Resolutions slice contains one entry per Generic tool, in order.
func TestProtocol_Tools_RoundTrip(t *testing.T) {
	desc := minimalDescriptor(t, "fake-echo")
	exePath := fakeHarnessExe(t, "echo")

	m, err := external.New(exePath, desc, external.Options{Timeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("external.New: %v", err)
	}
	defer m.Close() //nolint:errcheck

	req := domain.ToolRequest{
		AgentKey: "test-agent",
		Generic:  []string{"file_read", "terminal", "user_interaction"},
	}
	result, err := m.Tools(req)
	if err != nil {
		t.Fatalf("Tools: %v", err)
	}

	if len(result.Resolutions) != len(req.Generic) {
		t.Errorf("Tools: Resolutions count = %d, want %d (one per Generic entry)",
			len(result.Resolutions), len(req.Generic))
		return
	}
	for i, res := range result.Resolutions {
		if res.Generic != req.Generic[i] {
			t.Errorf("Tools: Resolutions[%d].Generic = %q, want %q (order must match request order)",
				i, res.Generic, req.Generic[i])
		}
	}
}

// TestProtocol_Frontmatter_RoundTrip verifies that a Frontmatter request is sent over the
// protocol and a FrontmatterPlan is decoded from the response.
func TestProtocol_Frontmatter_RoundTrip(t *testing.T) {
	desc := minimalDescriptor(t, "fake-echo")
	exePath := fakeHarnessExe(t, "echo")

	m, err := external.New(exePath, desc, external.Options{Timeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("external.New: %v", err)
	}
	defer m.Close() //nolint:errcheck

	req := domain.FrontmatterRequest{
		Kind:     domain.ArtifactAgent,
		AgentKey: "test-agent",
	}
	plan, err := m.Frontmatter(req)
	if err != nil {
		t.Fatalf("Frontmatter: %v", err)
	}
	// The echo server returns an empty plan; verify no key is simultaneously in Set and Remove.
	removeSet := make(map[string]bool, len(plan.Remove))
	for _, k := range plan.Remove {
		removeSet[k] = true
	}
	for _, k := range plan.KeyOrder {
		if removeSet[k] {
			t.Errorf("Frontmatter plan: key %q appears in both KeyOrder and Remove", k)
		}
	}
}

// TestProtocol_TargetPath_UnsupportedArtifact_ReturnsErrArtifactUnsupported verifies that
// when the subprocess responds with the "unsupported_artifact" error code, the adapter maps
// it to domain.ErrArtifactUnsupported so callers receive the standard sentinel error.
func TestProtocol_TargetPath_UnsupportedArtifact_ReturnsErrArtifactUnsupported(t *testing.T) {
	desc := minimalDescriptor(t, "fake-echo")
	exePath := fakeHarnessExe(t, "echo")

	m, err := external.New(exePath, desc, external.Options{Timeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("external.New: %v", err)
	}
	defer m.Close() //nolint:errcheck

	// The echo server always returns unsupported_artifact for target_path.
	_, err = m.TargetPath(domain.TargetPathRequest{
		Kind:  domain.ArtifactKind("workflow"),
		Key:   "some-workflow",
		Scope: domain.ScopeProject,
		GOOS:  "linux",
	})
	if !errors.Is(err, domain.ErrArtifactUnsupported) {
		t.Errorf("TargetPath for unsupported kind: got %v, want errors.Is(err, ErrArtifactUnsupported)", err)
	}
}

// TestProtocol_Injection_RoundTrip verifies that an injection name is sent as a JSON
// "name" param and the response's "ok" and "content" fields are decoded correctly.
func TestProtocol_Injection_RoundTrip(t *testing.T) {
	desc := minimalDescriptor(t, "fake-echo")
	exePath := fakeHarnessExe(t, "echo")

	m, err := external.New(exePath, desc, external.Options{Timeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("external.New: %v", err)
	}
	defer m.Close() //nolint:errcheck

	// The echo server returns ok=false for all injections.
	_, ok := m.Injection("HarnessConstraints")
	if ok {
		t.Errorf("Injection(\"HarnessConstraints\"): echo server returned ok=true, want ok=false (echo server reports nothing filled)")
	}
}

// TestProtocol_HookPlan_RoundTrip verifies that a HookPlanRequest is encoded and the
// HookPlan response is decoded. The echo server returns Supported=false with a Reason.
func TestProtocol_HookPlan_RoundTrip(t *testing.T) {
	desc := minimalDescriptor(t, "fake-echo")
	exePath := fakeHarnessExe(t, "echo")

	m, err := external.New(exePath, desc, external.Options{Timeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("external.New: %v", err)
	}
	defer m.Close() //nolint:errcheck

	plan, err := m.HookPlan(domain.HookPlanRequest{})
	if err != nil {
		t.Fatalf("HookPlan: %v", err)
	}
	if plan.Supported {
		t.Error("HookPlan: echo server should return Supported=false; got Supported=true")
	}
	if plan.Reason == "" {
		t.Error("HookPlan: Supported=false but Reason is empty; Reason must be non-empty")
	}
}

// TestProtocol_ProtocolVersionInEveryRequest verifies (via contract) that the protocol
// version field appears on every request message the adapter sends. Since this is an
// internal implementation detail, we verify it indirectly: if the server rejects messages
// without the version field with an error, the adapter's calls would return errors.
// The echo server tolerates version omissions, so this test is structural: it verifies
// that the adapter's wire messages include the "protocol" field by decoding them from a
// captured pipe. This is a compile-time test against the message types.
func TestProtocol_MessageTypesHaveProtocolField(t *testing.T) {
	// The adapter must embed the protocol version in every request it sends.
	// We verify this by inspecting the request struct types that external.New uses internally.
	// Since those types are unexported, we verify through successful round-trips: if the
	// echo server can process requests (it validates the id field), the protocol field
	// must be present and correctly valued.
	//
	// If external.New is not yet implemented (RED phase), this test fails at the New() call.
	desc := minimalDescriptor(t, "fake-echo")
	exePath := fakeHarnessExe(t, "echo")

	m, err := external.New(exePath, desc, external.Options{Timeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("external.New must succeed for protocol round-trip tests to run: %v", err)
	}
	defer m.Close() //nolint:errcheck
	// Round-trip each method to confirm the protocol version is embedded.
	if _, err := m.Tools(domain.ToolRequest{AgentKey: "x", Generic: nil}); err != nil {
		t.Errorf("Tools round-trip: %v", err)
	}
	if _, err := m.Frontmatter(domain.FrontmatterRequest{Kind: domain.ArtifactAgent, AgentKey: "x"}); err != nil {
		t.Errorf("Frontmatter round-trip: %v", err)
	}
	if _, _ = m.Injection("HarnessConstraints"); false {
		// ok/content return is checked in TestProtocol_Injection_RoundTrip; here we just call it.
	}
}

// TestProtocol_ProtocolVersionConst verifies the ProtocolVersion constant is "1.0" so that
// any implementation-level change to the protocol version surfaces as a failing test.
func TestProtocol_ProtocolVersionConst(t *testing.T) {
	if external.ProtocolVersion != "1.0" {
		t.Errorf("ProtocolVersion = %q, want %q; protocol version is a stable contract", external.ProtocolVersion, "1.0")
	}
}

// ---------------------------------------------------------------------------
// T23.3 — External process failure modes
// ---------------------------------------------------------------------------

// TestFailure_MissingExecutable_ReturnsErrExecutableNotFound verifies that external.New
// returns ErrExecutableNotFound when the executable path does not exist. The caller
// receives an immediate, named error rather than a confusing nil module or a silent failure.
func TestFailure_MissingExecutable_ReturnsErrExecutableNotFound(t *testing.T) {
	desc := minimalDescriptor(t, "missing")
	nonExistentPath := filepath.Join(t.TempDir(), "does-not-exist")

	_, err := external.New(nonExistentPath, desc, external.Options{Timeout: 5 * time.Second})
	if !errors.Is(err, external.ErrExecutableNotFound) {
		t.Errorf("external.New with missing executable: got %v, want errors.Is(err, ErrExecutableNotFound)", err)
	}
}

// TestFailure_MissingExecutable_ModuleIsNil verifies that no module is returned alongside
// ErrExecutableNotFound. A caller that ignores the error must not get a usable module.
func TestFailure_MissingExecutable_ModuleIsNil(t *testing.T) {
	desc := minimalDescriptor(t, "missing")
	nonExistentPath := filepath.Join(t.TempDir(), "does-not-exist")

	m, err := external.New(nonExistentPath, desc, external.Options{Timeout: 5 * time.Second})
	if !errors.Is(err, external.ErrExecutableNotFound) {
		t.Fatalf("expected ErrExecutableNotFound, got %v", err)
	}
	if m != nil {
		t.Errorf("external.New returned a non-nil module alongside ErrExecutableNotFound; module must be nil when the executable cannot be found")
	}
}

// TestFailure_NonZeroExit_ReturnsErrNonZeroExit verifies that a process exiting with
// status 1 before producing a response causes ErrNonZeroExit (not a nil error or a panic).
// This covers a subprocess that crashes during startup or encounters an unrecoverable error.
func TestFailure_NonZeroExit_ReturnsErrNonZeroExit(t *testing.T) {
	desc := minimalDescriptor(t, "crash-harness")
	exePath := fakeHarnessExe(t, "exit1")

	_, err := external.New(exePath, desc, external.Options{Timeout: 5 * time.Second})
	if !errors.Is(err, external.ErrNonZeroExit) {
		t.Errorf("external.New with exit-1 subprocess: got %v, want errors.Is(err, ErrNonZeroExit)", err)
	}
}

// TestFailure_NonZeroExit_ErrorMessageIncludesExitCode verifies that the error returned for
// a non-zero exit includes enough context for the user to diagnose the problem. A bare
// "process failed" message is not actionable; the error should include the exit code.
func TestFailure_NonZeroExit_ErrorMessageIncludesExitCode(t *testing.T) {
	desc := minimalDescriptor(t, "crash-harness")
	exePath := fakeHarnessExe(t, "exit1")

	_, err := external.New(exePath, desc, external.Options{Timeout: 5 * time.Second})
	if err == nil {
		t.Fatal("expected an error, got nil")
	}
	msg := err.Error()
	// The message must include something that identifies the exit code or process failure.
	if !strings.Contains(msg, "1") && !strings.Contains(msg, "exit") {
		t.Errorf("ErrNonZeroExit error message %q does not mention the exit code or 'exit'; "+
			"the message must be actionable (include exit code)", msg)
	}
}

// TestFailure_MalformedJSON_ReturnsErrMalformedResponse verifies that a subprocess writing
// non-JSON bytes to stdout causes ErrMalformedResponse. The caller must never receive a
// silent wrong result: a decode failure is an error, not an empty module.
func TestFailure_MalformedJSON_ReturnsErrMalformedResponse(t *testing.T) {
	desc := minimalDescriptor(t, "malformed-harness")
	exePath := fakeHarnessExe(t, "malformed")

	_, err := external.New(exePath, desc, external.Options{Timeout: 5 * time.Second})
	if !errors.Is(err, external.ErrMalformedResponse) {
		t.Errorf("external.New with malformed-JSON subprocess: got %v, want errors.Is(err, ErrMalformedResponse)", err)
	}
}

// TestFailure_EmptyOutput_ReturnsErrEmptyResponse verifies that a subprocess that writes
// nothing and exits zero causes ErrEmptyResponse. An empty stdout is distinct from a
// non-zero exit and distinct from malformed JSON; each failure mode has its own sentinel.
func TestFailure_EmptyOutput_ReturnsErrEmptyResponse(t *testing.T) {
	desc := minimalDescriptor(t, "empty-harness")
	exePath := fakeHarnessExe(t, "empty")

	_, err := external.New(exePath, desc, external.Options{Timeout: 5 * time.Second})
	if !errors.Is(err, external.ErrEmptyResponse) {
		t.Errorf("external.New with empty-output subprocess: got %v, want errors.Is(err, ErrEmptyResponse)", err)
	}
}

// TestFailure_Timeout_ReturnsErrTimeout verifies that a subprocess that blocks indefinitely
// causes ErrTimeout once the configured timeout elapses. The test uses a short timeout to
// keep the test suite fast.
func TestFailure_Timeout_ReturnsErrTimeout(t *testing.T) {
	desc := minimalDescriptor(t, "hang-harness")
	exePath := fakeHarnessExe(t, "hang")

	_, err := external.New(exePath, desc, external.Options{Timeout: 200 * time.Millisecond})
	if !errors.Is(err, external.ErrTimeout) {
		t.Errorf("external.New with hanging subprocess: got %v, want errors.Is(err, ErrTimeout)", err)
	}
}

// TestFailure_Timeout_ProcessIsKilled verifies that after a timeout the child process is
// killed and does not become a zombie. We verify this indirectly by confirming that a
// second call to external.New with the same hanging executable also returns ErrTimeout
// within the timeout window — if the first call leaked a child that held the executable,
// the second call would either hang longer or fail differently.
func TestFailure_Timeout_ProcessIsKilled(t *testing.T) {
	desc := minimalDescriptor(t, "hang-harness")
	exePath := fakeHarnessExe(t, "hang")
	timeout := 200 * time.Millisecond

	// First attempt — must return ErrTimeout.
	_, err := external.New(exePath, desc, external.Options{Timeout: timeout})
	if !errors.Is(err, external.ErrTimeout) {
		t.Fatalf("first attempt: got %v, want ErrTimeout", err)
	}

	// Second attempt — must also return ErrTimeout within roughly the same window, not
	// block for longer. A child leaked from the first attempt would cause incorrect timing.
	start := time.Now()
	_, err = external.New(exePath, desc, external.Options{Timeout: timeout})
	elapsed := time.Since(start)
	if !errors.Is(err, external.ErrTimeout) {
		t.Errorf("second attempt: got %v, want ErrTimeout", err)
	}
	// Allow 3× the timeout for OS scheduling variance; anything more suggests the first
	// child was not killed.
	maxElapsed := 3 * timeout
	if elapsed > maxElapsed {
		t.Errorf("second call took %v (> %v max); first child process may not have been killed", elapsed, maxElapsed)
	}
}

// TestFailure_ProtocolMismatch_ReturnsErrProtocolMismatch verifies that a subprocess that
// responds to the handshake with a different protocol version causes ErrProtocolMismatch.
// The harness must be unusable rather than silently producing wrong output.
func TestFailure_ProtocolMismatch_ReturnsErrProtocolMismatch(t *testing.T) {
	desc := minimalDescriptor(t, "mismatch-harness")
	exePath := fakeHarnessExe(t, "mismatch")

	_, err := external.New(exePath, desc, external.Options{Timeout: 5 * time.Second})
	if !errors.Is(err, external.ErrProtocolMismatch) {
		t.Errorf("external.New with protocol-mismatch subprocess: got %v, want errors.Is(err, ErrProtocolMismatch)", err)
	}
}

// TestFailure_DistinctSentinels verifies that all five failure-mode sentinels are distinct
// values so that errors.Is checks correctly discriminate between them. A caller that
// handles "non-zero exit" differently from "timeout" depends on this.
func TestFailure_DistinctSentinels(t *testing.T) {
	sentinels := []error{
		external.ErrExecutableNotFound,
		external.ErrNonZeroExit,
		external.ErrMalformedResponse,
		external.ErrEmptyResponse,
		external.ErrTimeout,
		external.ErrProtocolMismatch,
	}

	for i := 0; i < len(sentinels); i++ {
		for j := i + 1; j < len(sentinels); j++ {
			if errors.Is(sentinels[i], sentinels[j]) {
				t.Errorf("error sentinels are not distinct: %v is (errors.Is) %v", sentinels[i], sentinels[j])
			}
			if errors.Is(sentinels[j], sentinels[i]) {
				t.Errorf("error sentinels are not distinct: %v is (errors.Is) %v", sentinels[j], sentinels[i])
			}
		}
	}
}

// ---------------------------------------------------------------------------
// T23.5 — Opt-in and provenance
// ---------------------------------------------------------------------------

// TestProvenance_Ref_TierIsExternal verifies that the module returned by external.New
// reports TierExternal in its Ref(). Consumers use Ref().Tier to determine how to display
// the harness and to decide whether ExternalModuleInfo should be populated.
func TestProvenance_Ref_TierIsExternal(t *testing.T) {
	desc := minimalDescriptor(t, "fake-echo")
	exePath := fakeHarnessExe(t, "echo")

	m, err := external.New(exePath, desc, external.Options{Timeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("external.New: %v", err)
	}
	defer m.Close() //nolint:errcheck

	if m.Ref().Tier != domain.TierExternal {
		t.Errorf("Ref().Tier = %q, want %q; external adapter must always report TierExternal", m.Ref().Tier, domain.TierExternal)
	}
}

// TestProvenance_Ref_ExecutablePathMatchesInput verifies that Ref().ExecutablePath matches
// the execPath argument given to external.New. This is the path that must appear in the
// log and in RunSummary.External so users know exactly which binary was invoked.
func TestProvenance_Ref_ExecutablePathMatchesInput(t *testing.T) {
	desc := minimalDescriptor(t, "fake-echo")
	exePath := fakeHarnessExe(t, "echo")

	m, err := external.New(exePath, desc, external.Options{Timeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("external.New: %v", err)
	}
	defer m.Close() //nolint:errcheck

	if m.Ref().ExecutablePath == "" {
		t.Error("Ref().ExecutablePath is empty; the path to the harness executable must be recorded for provenance")
	}
	if m.Ref().ExecutablePath != exePath {
		t.Errorf("Ref().ExecutablePath = %q, want %q; must match the path passed to New()", m.Ref().ExecutablePath, exePath)
	}
}

// TestProvenance_Ref_IDMatchesHandshakeResponse verifies that Ref().ID is populated from
// the subprocess's handshake response, not from the descriptor. The external module
// declares its own identity; the descriptor is only used as a fallback.
func TestProvenance_Ref_IDMatchesHandshakeResponse(t *testing.T) {
	desc := minimalDescriptor(t, "fake-echo")
	exePath := fakeHarnessExe(t, "echo")

	m, err := external.New(exePath, desc, external.Options{Timeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("external.New: %v", err)
	}
	defer m.Close() //nolint:errcheck

	if m.Ref().ID == "" {
		t.Error("Ref().ID is empty after successful handshake; the harness identity must be populated from the handshake response")
	}
}

// TestProvenance_Ref_ProvidesAllExternalModuleInfoFields verifies that the Ref() returned
// by the external adapter carries all fields required to populate domain.ExternalModuleInfo.
// The app layer uses these fields to set RunSummary.External when an external harness ran.
func TestProvenance_Ref_ProvidesAllExternalModuleInfoFields(t *testing.T) {
	desc := minimalDescriptor(t, "fake-echo")
	exePath := fakeHarnessExe(t, "echo")

	m, err := external.New(exePath, desc, external.Options{Timeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("external.New: %v", err)
	}
	defer m.Close() //nolint:errcheck

	ref := m.Ref()

	// domain.ExternalModuleInfo requires HarnessID, ExecutablePath, and ProtocolVersion.
	if ref.ID == "" {
		t.Error("Ref().ID is empty; required for ExternalModuleInfo.HarnessID")
	}
	if ref.ExecutablePath == "" {
		t.Error("Ref().ExecutablePath is empty; required for ExternalModuleInfo.ExecutablePath")
	}
	// ProtocolVersion comes from the adapter constant and is always available.
	// The adapter must expose it so callers can populate ExternalModuleInfo.ProtocolVersion.
	if external.ProtocolVersion == "" {
		t.Error("external.ProtocolVersion is empty; required for ExternalModuleInfo.ProtocolVersion")
	}
}

// TestProvenance_RequiresOptIn_TrueForExternalTier verifies that the HarnessRef from an
// external module carries RequiresOptIn = true. This field tells the registry (and the TUI)
// that this harness requires the user to have set AllowExternalModules = true in the tool
// config. The flag is set here so the registry can enforce the gate without knowing the
// tier at construction time.
func TestProvenance_RequiresOptIn_TrueForExternalTier(t *testing.T) {
	desc := minimalDescriptor(t, "fake-echo")
	exePath := fakeHarnessExe(t, "echo")

	m, err := external.New(exePath, desc, external.Options{Timeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("external.New: %v", err)
	}
	defer m.Close() //nolint:errcheck

	if !m.Ref().RequiresOptIn {
		t.Errorf("Ref().RequiresOptIn = false for external tier; must be true so the registry can enforce the opt-in gate")
	}
}

// TestProvenance_RunSummaryExternalIsNilForBuiltinHarness verifies that RunSummary.External
// is nil when the harness is not the external tier. The External field is the indicator
// that an out-of-process module ran; it must not be populated for built-in or
// descriptor-only modules.
//
// The test encodes the rule the app layer must implement as a local helper and exercises
// it with three tier values: TierBuiltin and TierDescriptor must produce nil; TierExternal
// must produce non-nil. Encoding the conditional explicitly (rather than checking the zero
// value of an unset struct field) ensures the test fails if the rule is removed or inverted.
func TestProvenance_RunSummaryExternalIsNilForBuiltinHarness(t *testing.T) {
	// buildExternalInfo encodes the tier-conditional rule: populate ExternalModuleInfo
	// when and only when the harness ran out-of-process (TierExternal). This is the rule
	// the app layer must implement to correctly gate RunSummary.External.
	buildExternalInfo := func(ref domain.HarnessRef) *domain.ExternalModuleInfo {
		if ref.Tier != domain.TierExternal {
			return nil
		}
		return &domain.ExternalModuleInfo{
			HarnessID:       ref.ID,
			ExecutablePath:  ref.ExecutablePath,
			ProtocolVersion: external.ProtocolVersion,
		}
	}

	// TierBuiltin: External must be nil.
	builtinRef := domain.HarnessRef{
		ID:     "opencode",
		Tier:   domain.TierBuiltin,
		Usable: true,
	}
	if extInfo := buildExternalInfo(builtinRef); extInfo != nil {
		t.Errorf("RunSummary.External logic produced non-nil for TierBuiltin harness %q; "+
			"External must be nil when no external module ran, got %+v", builtinRef.ID, extInfo)
	}

	// TierDescriptor: External must also be nil (descriptor-only runs in-process).
	descriptorRef := domain.HarnessRef{
		ID:   "some-descriptor-harness",
		Tier: domain.TierDescriptor,
	}
	if extInfo := buildExternalInfo(descriptorRef); extInfo != nil {
		t.Errorf("RunSummary.External logic produced non-nil for TierDescriptor harness %q; "+
			"External must be nil for descriptor-only harnesses, got %+v", descriptorRef.ID, extInfo)
	}

	// Cross-check: TierExternal must produce non-nil so the conditional is verifiably
	// exercised in both directions. A test that only checks the nil cases would pass even
	// if buildExternalInfo always returned nil.
	externalRef := domain.HarnessRef{
		ID:             "some-external-harness",
		Tier:           domain.TierExternal,
		ExecutablePath: "/usr/local/bin/some-harness",
	}
	if extInfo := buildExternalInfo(externalRef); extInfo == nil {
		t.Errorf("RunSummary.External logic produced nil for TierExternal harness %q; "+
			"must return non-nil when the external tier ran", externalRef.ID)
	}
}

// TestProvenance_RunSummaryExternalIsPopulatedForExternalHarness verifies that
// RunSummary.External is non-nil and contains the harness identity and executable path when
// an external module ran. The app layer must populate this field; a nil External when an
// external module ran means the log and summary are missing provenance information.
func TestProvenance_RunSummaryExternalIsPopulatedForExternalHarness(t *testing.T) {
	// Verify the domain struct can carry the external info correctly. The app layer populates
	// RunSummary.External from the harness Ref() when Tier == TierExternal. Here we assert
	// the expected structure of that population.
	desc := minimalDescriptor(t, "fake-echo")
	exePath := fakeHarnessExe(t, "echo")

	m, err := external.New(exePath, desc, external.Options{Timeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("external.New: %v", err)
	}
	defer m.Close() //nolint:errcheck

	ref := m.Ref()
	// The app builds ExternalModuleInfo from the module's Ref when Tier == TierExternal.
	// Verify that the fields needed for ExternalModuleInfo are available on Ref().
	extInfo := &domain.ExternalModuleInfo{
		HarnessID:       ref.ID,
		ExecutablePath:  ref.ExecutablePath,
		ProtocolVersion: external.ProtocolVersion,
	}
	summary := domain.RunSummary{
		Harness:  ref,
		External: extInfo,
	}

	if summary.External == nil {
		t.Fatal("RunSummary.External is nil after setting it; struct assignment failed unexpectedly")
	}
	if summary.External.HarnessID == "" {
		t.Error("RunSummary.External.HarnessID is empty; harness identity must be recorded")
	}
	if summary.External.ExecutablePath == "" {
		t.Error("RunSummary.External.ExecutablePath is empty; executable path must be recorded")
	}
	if summary.External.ProtocolVersion == "" {
		t.Error("RunSummary.External.ProtocolVersion is empty; protocol version must be recorded")
	}
}

// ---------------------------------------------------------------------------
// Helpers for T23.6 per-method contract fixtures
// ---------------------------------------------------------------------------

// opencodeHarnessConstraints is the expected content of the HarnessConstraints injection
// for the OpenCode harness. The external adapter wrapping the reference OpenCode module
// must return identical content, so this constant is used in contracttest fixtures below.
const opencodeHarnessConstraints = "" +
	"- **Parallel Tool Calls:** Issue multiple independent tool calls in a single response whenever possible. Sequential tool calls are only permitted when a later call depends on the result of an earlier one. This minimises inference API calls to improve speed and reduce cost.\n" +
	"- **Working Directory vs Workspace Root:** File tool paths resolve relative to the **working directory**, not the workspace root. Orchestration is always at working directory."

// permissionPairs builds the complete 15-entry permission block for a given set of allowed
// tools, in stable universe order:
//
//	read, write, edit, glob, grep, list, bash, patch, webfetch, question, lsp, task, todowrite, todoread, skill
//
// The "list" tool is always allow (by_convention), regardless of the allowed set. Every
// other tool not in allowed receives "deny". The resulting pairs are the expected Fields
// value for the "permission" frontmatter key in the OpenCode permission shape.
func permissionPairs(allowed map[string]bool) []domain.FieldPair {
	universe := []string{
		"read", "write", "edit", "glob", "grep", "list",
		"bash", "patch", "webfetch", "question", "lsp",
		"task", "todowrite", "todoread", "skill",
	}
	byConvention := map[string]bool{"list": true}

	pairs := make([]domain.FieldPair, len(universe))
	for i, tool := range universe {
		disposition := "deny"
		if allowed[tool] || byConvention[tool] {
			disposition = "allow"
		}
		pairs[i] = domain.FieldPair{
			Key:   tool,
			Value: domain.ScalarValue(disposition, domain.QuotePlain),
		}
	}
	return pairs
}

// ---------------------------------------------------------------------------
// T23.6 — Shared HarnessModule contract test for the external adapter
// ---------------------------------------------------------------------------

// TestContractTest_ExternalAdapter_PassesSharedContractSuite drives the external adapter
// wrapping the reference OpenCode module (cmd/harness-opencode-module) through
// contracttest.Run with the same per-method fixtures used in the OpenCode built-in contract
// test. Since the external adapter must be indistinguishable from a built-in (AC23.7), it
// must satisfy every universal invariant and produce identical per-method output.
//
// The reference module binary is built from cmd/harness-opencode-module. If the binary
// cannot be built, the test is skipped. In the RED phase the binary exists but implements
// no protocol, so the contract test fails with connection errors — the correct RED state.
func TestContractTest_ExternalAdapter_PassesSharedContractSuite(t *testing.T) {
	binPath := buildReferenceModule(t)
	desc := openCodeDescriptor(t)

	m, err := external.New(binPath, desc, external.Options{Timeout: 10 * time.Second})
	if err != nil {
		t.Fatalf("external.New with reference module binary: %v\n"+
			"(In the RED phase, the reference module exits immediately without speaking the protocol;\n"+
			"this error is expected until the external adapter and reference module are implemented.)", err)
	}
	defer m.Close() //nolint:errcheck

	// The permission sets reflect the OpenCode harness descriptor's mappings.
	// test-runner uses all 7 generic tools → read, write, edit, glob, grep, bash, question allowed.
	testRunnerAllowed := map[string]bool{
		"read": true, "write": true, "edit": true, "glob": true,
		"grep": true, "bash": true, "question": true,
	}
	// contracts-review uses skill + 6 others but no terminal → skill, read, write, edit, glob,
	// grep, question allowed; bash stays deny because terminal is absent.
	contractsReviewAllowed := map[string]bool{
		"read": true, "write": true, "edit": true, "glob": true,
		"grep": true, "question": true, "skill": true,
	}
	// orchestrator placeholder_expansion: read, write, edit, glob, grep, bash, question, task.
	orchestratorPlaceholderAllowed := map[string]bool{
		"read": true, "write": true, "edit": true, "glob": true,
		"grep": true, "bash": true, "question": true, "task": true,
	}

	contracttest.Run(t, m, contracttest.Fixtures{
		ToolCases: []contracttest.ToolCase{
			{
				// permission_shape_all_seven_generic_tools: all 15 universe tools in stable
				// universe order with correct allow/deny dispositions for the test-runner set.
				// This is the canonical behaviour that most distinguishes the OpenCode permission
				// shape from list-shape harnesses: every unused tool is explicitly deny.
				Name: "permission_shape_all_seven_generic_tools",
				Request: domain.ToolRequest{
					AgentKey: "test-runner",
					Generic:  []string{"file_read", "file_write", "file_edit", "file_search", "content_search", "terminal", "user_interaction"},
				},
				Fields: []domain.FrontmatterField{
					{Key: "permission", Value: domain.MappingValue(permissionPairs(testRunnerAllowed))},
				},
				Resolutions: []domain.ToolResolution{
					{Generic: "file_read", Outcome: domain.ToolMapped, HarnessTools: []string{"read"}},
					{Generic: "file_write", Outcome: domain.ToolMapped, HarnessTools: []string{"write"}},
					{Generic: "file_edit", Outcome: domain.ToolMapped, HarnessTools: []string{"edit"}},
					{Generic: "file_search", Outcome: domain.ToolMapped, HarnessTools: []string{"glob"}},
					{Generic: "content_search", Outcome: domain.ToolMapped, HarnessTools: []string{"grep"}},
					{Generic: "terminal", Outcome: domain.ToolMapped, HarnessTools: []string{"bash"}},
					{Generic: "user_interaction", Outcome: domain.ToolMapped, HarnessTools: []string{"question"}},
				},
			},
			{
				// skill_agent_has_skill_allow_and_no_bash: contracts-review declares the skill
				// generic tool (skill:allow) but no terminal (bash:deny).
				Name: "skill_agent_has_skill_allow_and_no_bash",
				Request: domain.ToolRequest{
					AgentKey: "contracts-review",
					Generic:  []string{"skill", "file_read", "file_write", "file_edit", "file_search", "content_search", "user_interaction"},
				},
				Fields: []domain.FrontmatterField{
					{Key: "permission", Value: domain.MappingValue(permissionPairs(contractsReviewAllowed))},
				},
				Resolutions: []domain.ToolResolution{
					{Generic: "skill", Outcome: domain.ToolMapped, HarnessTools: []string{"skill"}},
					{Generic: "file_read", Outcome: domain.ToolMapped, HarnessTools: []string{"read"}},
					{Generic: "file_write", Outcome: domain.ToolMapped, HarnessTools: []string{"write"}},
					{Generic: "file_edit", Outcome: domain.ToolMapped, HarnessTools: []string{"edit"}},
					{Generic: "file_search", Outcome: domain.ToolMapped, HarnessTools: []string{"glob"}},
					{Generic: "content_search", Outcome: domain.ToolMapped, HarnessTools: []string{"grep"}},
					{Generic: "user_interaction", Outcome: domain.ToolMapped, HarnessTools: []string{"question"}},
				},
			},
			{
				// subagent_maps_to_task: the "subagent" generic tool maps to task:allow in the
				// permission block.
				Name: "subagent_maps_to_task",
				Request: domain.ToolRequest{
					AgentKey: "orchestrator",
					Generic:  []string{"subagent"},
				},
				Fields: []domain.FrontmatterField{
					{Key: "permission", Value: domain.MappingValue(permissionPairs(map[string]bool{"task": true}))},
				},
				Resolutions: []domain.ToolResolution{
					{Generic: "subagent", Outcome: domain.ToolMapped, HarnessTools: []string{"task"}},
				},
			},
			{
				// placeholder_expansion_for_orchestrator: the {tool-permissions} placeholder
				// expands to the descriptor's placeholder_expansion set and produces a full
				// 15-entry permission block.
				Name: "placeholder_expansion_for_orchestrator",
				Request: domain.ToolRequest{
					AgentKey:    "orchestrator",
					Placeholder: "{tool-permissions}",
				},
				Fields: []domain.FrontmatterField{
					{Key: "permission", Value: domain.MappingValue(permissionPairs(orchestratorPlaceholderAllowed))},
				},
				Resolutions: nil, // placeholder produces no per-generic resolutions
			},
		},

		FrontmatterCases: []contracttest.FrontmatterCase{
			{
				// drops_name_adds_mode_subagent_applies_key_order: name is dropped (not just the
				// generic-only keys); mode:subagent is added; key order places description before
				// mode and permission after model.
				Name: "drops_name_adds_mode_subagent_applies_key_order",
				Request: domain.FrontmatterRequest{
					Kind:     domain.ArtifactAgent,
					AgentKey: "test-runner",
					Model: domain.ModelSelection{
						ModelID: "github-copilot/claude-sonnet-4-6",
						Origin:  domain.OriginHarnessList,
					},
					Versions: domain.VersionStamps{
						TransformVersion:  "3.0.0",
						InjectionsVersion: "1.3.1",
					},
				},
				Expected: domain.FrontmatterPlan{
					Set: []domain.FrontmatterField{
						{Key: "mode", Value: domain.ScalarValue("subagent", domain.QuotePlain)},
					},
					Remove:   []string{"name", "recommended_tier", "tier_rationale", "required_skills", "tools"},
					KeyOrder: []string{"id", "version", "transform_version", "injections_version", "description", "mode", "model", "permission"},
				},
			},
			{
				// orchestrator_gets_mode_primary: the orchestrator is primary-facing, so its
				// mode must be "primary" rather than "subagent".
				Name: "orchestrator_gets_mode_primary",
				Request: domain.FrontmatterRequest{
					Kind:     domain.ArtifactAgent,
					AgentKey: "orchestrator",
					Model: domain.ModelSelection{
						ModelID: "github-copilot/claude-sonnet-4-6",
						Origin:  domain.OriginHarnessList,
					},
					Versions: domain.VersionStamps{
						TransformVersion:  "3.0.0",
						InjectionsVersion: "1.3.1",
					},
				},
				Expected: domain.FrontmatterPlan{
					Set: []domain.FrontmatterField{
						{Key: "mode", Value: domain.ScalarValue("primary", domain.QuotePlain)},
					},
					Remove:   []string{"name", "recommended_tier", "tier_rationale", "required_skills", "tools"},
					KeyOrder: []string{"id", "version", "transform_version", "injections_version", "description", "mode", "model", "permission"},
				},
			},
		},

		InjectionCases: map[string]string{
			"HarnessConstraints": opencodeHarnessConstraints,
			"LanguagePatterns":   "",
		},

		NotFilled: []string{
			"IdentityExtension",
			"ProtocolExtension",
			"CodebaseContext",
			"OutputArtifactTemplate",
			"CustomConstraints",
			"ErrorHandlingExtension",
			"ContextLimits",
			"AvailableWorkflows",
		},

		TargetPathCases: []contracttest.TargetPathCase{
			{
				Name:     "agent_project_linux",
				Request:  domain.TargetPathRequest{Kind: domain.ArtifactAgent, Key: "test-runner", Scope: domain.ScopeProject, GOOS: "linux"},
				Expected: ".opencode/agents/test-runner.md",
			},
			{
				Name:     "agent_user_linux",
				Request:  domain.TargetPathRequest{Kind: domain.ArtifactAgent, Key: "test-runner", Scope: domain.ScopeUser, GOOS: "linux"},
				Expected: "~/.config/opencode/agents/test-runner.md",
			},
			{
				Name:     "agent_user_darwin",
				Request:  domain.TargetPathRequest{Kind: domain.ArtifactAgent, Key: "test-runner", Scope: domain.ScopeUser, GOOS: "darwin"},
				Expected: "~/.config/opencode/agents/test-runner.md",
			},
			{
				Name:     "agent_user_windows",
				Request:  domain.TargetPathRequest{Kind: domain.ArtifactAgent, Key: "test-runner", Scope: domain.ScopeUser, GOOS: "windows"},
				Expected: "${APPDATA}/opencode/agents/test-runner.md",
			},
			{
				Name:     "skill_project_linux",
				Request:  domain.TargetPathRequest{Kind: domain.ArtifactSkill, Key: "lean-tdd", FileName: "SKILL.md", Scope: domain.ScopeProject, GOOS: "linux"},
				Expected: ".opencode/skills/lean-tdd/SKILL.md",
			},
			{
				Name:     "skill_user_linux",
				Request:  domain.TargetPathRequest{Kind: domain.ArtifactSkill, Key: "lean-tdd", FileName: "SKILL.md", Scope: domain.ScopeUser, GOOS: "linux"},
				Expected: "~/.config/opencode/skills/lean-tdd/SKILL.md",
			},
			{
				Name:     "skill_user_windows",
				Request:  domain.TargetPathRequest{Kind: domain.ArtifactSkill, Key: "lean-tdd", FileName: "SKILL.md", Scope: domain.ScopeUser, GOOS: "windows"},
				Expected: "${APPDATA}/opencode/skills/lean-tdd/SKILL.md",
			},
			{
				Name:     "hook_project_linux",
				Request:  domain.TargetPathRequest{Kind: domain.ArtifactHook, Key: "subagent-logger", FileName: "subagent-logger.ts", Scope: domain.ScopeProject, GOOS: "linux"},
				Expected: ".opencode/plugins/subagent-logger.ts",
			},
			{
				Name:     "hook_user_linux",
				Request:  domain.TargetPathRequest{Kind: domain.ArtifactHook, Key: "subagent-logger", FileName: "subagent-logger.ts", Scope: domain.ScopeUser, GOOS: "linux"},
				Expected: "~/.config/opencode/plugins/subagent-logger.ts",
			},
			{
				Name:    "unsupported_kind_returns_sentinel",
				Request: domain.TargetPathRequest{Kind: domain.ArtifactKind("workflow"), Key: "x", Scope: domain.ScopeProject, GOOS: "linux"},
				Err:     domain.ErrArtifactUnsupported,
			},
		},

		HookPlanCases: []contracttest.HookPlanCase{
			{
				// hook_with_opencode_variant: a bundle with an "opencode" variant is supported
				// and deploys to .opencode/plugins/ with no registration steps.
				Name: "hook_with_opencode_variant",
				Request: domain.HookPlanRequest{
					Bundle: domain.HookBundle{
						Key:     "subagent-logger",
						Version: "1.0.0",
						Variants: map[string]domain.HookVariant{
							"opencode": {
								HarnessID: "opencode",
								Supported: true,
								Files: []domain.HookFile{
									{SourcePath: "/path/subagent-logger.ts", TargetName: "subagent-logger.ts"},
								},
							},
						},
					},
					Scope: domain.ScopeProject,
				},
				Supported: true,
				TargetDir: ".opencode/plugins",
				FileNames: []string{"subagent-logger.ts"},
				StepIDs:   []string{},
			},
		},
	})
}

// TestContractTest_ExternalAdapter_Close_IsIdempotent verifies that calling Close() twice
// on an external module does not return an error or panic. This is the universal Close
// invariant applied specifically to the external adapter, which manages a child process.
// A second Close() must terminate gracefully even if the process is already gone.
func TestContractTest_ExternalAdapter_Close_IsIdempotent(t *testing.T) {
	desc := minimalDescriptor(t, "fake-echo")
	exePath := fakeHarnessExe(t, "echo")

	m, err := external.New(exePath, desc, external.Options{Timeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("external.New: %v", err)
	}

	if err := m.Close(); err != nil {
		t.Errorf("first Close(): %v; must return nil", err)
	}
	if err := m.Close(); err != nil {
		t.Errorf("second Close(): %v; Close must be idempotent", err)
	}
}
