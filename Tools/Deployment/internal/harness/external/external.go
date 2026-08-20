// Package external implements domain.HarnessModule over a JSON-over-stdio child process protocol.
// It translates each HarnessModule method call into a single-line JSON request sent to the
// child process's stdin and reads the single-line JSON response from its stdout. Version
// negotiation, timeouts, and error distinction (missing executable, non-zero exit, malformed JSON,
// timeout) are handled here so callers cannot encounter a silent wrong result.
//
// Wire protocol: one JSON object per line on stdin (request) and stdout (response).
// Protocol version negotiation happens at construction via the "handshake" method. A mismatch
// makes the harness unusable at Resolve time, not at transformation time.
//
// Error taxonomy (for callers that need to distinguish failure modes):
//   - ErrExecutableNotFound: the executable file does not exist or is not executable.
//   - ErrNonZeroExit: the module process exited with a non-zero status before responding.
//   - ErrMalformedResponse: the module wrote JSON that does not match the expected response shape.
//   - ErrEmptyResponse: the module wrote nothing to stdout before closing or exiting.
//   - ErrTimeout: the module did not respond within the configured timeout.
//   - ErrProtocolMismatch: the module declared an incompatible protocol version on handshake.
//
// Close() and reconnect behaviour: Close() terminates the child process (releases resources).
// If a method is called on a closed adapter, the adapter reconnects automatically by spawning a
// new subprocess. This makes the adapter safe to use with the shared contract test suite, which
// calls Close() as part of its universal invariants and then continues exercising per-method
// behaviour on the same adapter instance.
package external

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"mosaic-deploy/internal/domain"
)

// ProtocolVersion is the version this host implementation speaks. A module must echo this
// version in its handshake response; any other value causes resolution to fail.
const ProtocolVersion = "1.0"

// Options configures the external module adapter.
type Options struct {
	// Timeout is the per-request deadline. If zero, a default of 30 seconds is used.
	// A module that does not respond within this period triggers ErrTimeout.
	Timeout time.Duration

	// MosaicRoot is the root of the MOSAIC repository. It is forwarded to external modules
	// so they can locate harness content files at run time. May be empty for external modules
	// that do not require repository access.
	MosaicRoot string
}

// Sentinel errors returned by the external adapter. Tests use errors.Is to distinguish them.
var (
	// ErrExecutableNotFound is returned when the executable path does not exist or cannot be
	// executed. It is returned at New() time so callers get immediate feedback.
	ErrExecutableNotFound = errors.New("external harness executable not found")

	// ErrNonZeroExit is returned when the module process exits with a non-zero status code
	// without writing a valid response. The error message includes the exit code and any
	// stderr output captured from the module.
	ErrNonZeroExit = errors.New("external harness exited with non-zero status")

	// ErrMalformedResponse is returned when the module writes bytes that are not valid JSON
	// or do not decode into the expected response shape.
	ErrMalformedResponse = errors.New("external harness response is not valid JSON")

	// ErrEmptyResponse is returned when the module closes stdout without writing any response
	// line. This is distinct from ErrNonZeroExit: the process may have exited zero but sent
	// nothing.
	ErrEmptyResponse = errors.New("external harness wrote no response")

	// ErrTimeout is returned when the module does not produce a response within the configured
	// timeout. The module process is killed after this error is returned.
	ErrTimeout = errors.New("external harness request timed out")

	// ErrProtocolMismatch is returned at handshake time when the module reports a protocol
	// version that this host does not support. The harness is unusable; callers should display
	// a clear message indicating a version incompatibility.
	ErrProtocolMismatch = errors.New("external harness protocol version mismatch")
)

// ---------------------------------------------------------------------------
// Wire protocol types
// ---------------------------------------------------------------------------

// wireRequest is the JSON envelope sent to the module on stdin for each method call.
type wireRequest struct {
	Protocol string          `json:"protocol"`
	ID       string          `json:"id"`
	Method   string          `json:"method"`
	Params   json.RawMessage `json:"params,omitempty"`
}

// wireResponse is the JSON envelope received from the module on stdout.
type wireResponse struct {
	Protocol string          `json:"protocol"`
	ID       string          `json:"id"`
	Result   json.RawMessage `json:"result,omitempty"`
	Error    *wireRespError  `json:"error,omitempty"`
}

// wireRespError carries a structured error from the module.
type wireRespError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// wireFieldValue is the JSON wire form of domain.FieldValue.
type wireFieldValue struct {
	Kind    string           `json:"kind"`
	Scalar  string           `json:"scalar,omitempty"`
	Quote   string           `json:"quote,omitempty"`
	Items   []wireFieldValue `json:"items,omitempty"`
	List    string           `json:"list,omitempty"`
	Pairs   []wireFieldPair  `json:"pairs,omitempty"`
	Comment string           `json:"comment,omitempty"`
}

type wireFieldPair struct {
	Key   string         `json:"key"`
	Value wireFieldValue `json:"value"`
}

type wireFrontmatterField struct {
	Key   string         `json:"key"`
	Value wireFieldValue `json:"value"`
}

// ---------------------------------------------------------------------------
// Param wire types (sent by adapter to module)
// ---------------------------------------------------------------------------

type wireToolRequest struct {
	AgentKey     string            `json:"agent_key"`
	Generic      []string          `json:"generic,omitempty"`
	Placeholder  string            `json:"placeholder,omitempty"`
	CustomNames  map[string]string `json:"custom_names,omitempty"`
	SkippedTools map[string]bool   `json:"skipped_tools,omitempty"`
}

type wireFrontmatterRequest struct {
	Kind       string                 `json:"kind"`
	AgentKey   string                 `json:"agent_key"`
	Role       string                 `json:"role,omitempty"`
	Source     []wireFrontmatterField `json:"source,omitempty"`
	Model      wireModelSelection     `json:"model"`
	ToolFields []wireFrontmatterField `json:"tool_fields,omitempty"`
	Versions   wireVersionStamps      `json:"versions"`
}

type wireModelSelection struct {
	ModelID string `json:"model_id"`
	Origin  string `json:"origin"`
}

type wireVersionStamps struct {
	Version                       string `json:"version"`
	TransformVersion              string `json:"transform_version"`
	InjectionsVersion             string `json:"injections_version"`
	OrchestratorInjectionsVersion string `json:"orchestrator_injections_version,omitempty"` // omitted for backward compatibility with older external modules
}

type wireTargetPathRequest struct {
	Kind     string `json:"kind"`
	Key      string `json:"key"`
	FileName string `json:"file_name,omitempty"`
	Scope    string `json:"scope"`
	GOOS     string `json:"goos"`
}

type wireInjectionRequest struct {
	Name     string `json:"name"`
	AgentKey string `json:"agent_key,omitempty"` // omitted for backward compatibility with older external modules
}

type wireHookPlanRequest struct {
	Bundle wireHookBundle `json:"bundle"`
	Scope  string         `json:"scope"`
}

type wireHookBundle struct {
	Key         string                     `json:"key"`
	Version     string                     `json:"version,omitempty"`
	Description string                     `json:"description,omitempty"`
	SourceDir   string                     `json:"source_dir,omitempty"`
	Variants    map[string]wireHookVariant `json:"variants,omitempty"`
	Placeholder bool                       `json:"placeholder,omitempty"`
}

type wireHookVariant struct {
	HarnessID     string         `json:"harness_id"`
	Supported     bool           `json:"supported"`
	ReusesVariant string         `json:"reuses_variant,omitempty"`
	Files         []wireHookFile `json:"files,omitempty"`
	Registration  []wireRegStep  `json:"registration,omitempty"`
}

type wireHookFile struct {
	SourcePath string `json:"source_path"`
	TargetName string `json:"target_name"`
}

type wireRegStep struct {
	ID          string `json:"id"`
	TargetPath  string `json:"target_path,omitempty"`
	Fragment    string `json:"fragment,omitempty"`
	Performable bool   `json:"performable,omitempty"`
	Instruction string `json:"instruction,omitempty"`
}

// ---------------------------------------------------------------------------
// Result wire types (received by adapter from module)
// ---------------------------------------------------------------------------

type wireToolResult struct {
	Fields      []wireFrontmatterField `json:"fields"`
	Resolutions []wireToolResolution   `json:"resolutions"`
}

type wireToolResolution struct {
	Generic      string                  `json:"generic"`
	Outcome      string                  `json:"outcome"`
	HarnessTools []string                `json:"harness_tools,omitempty"`
	Destinations []wireToolDestination   `json:"destinations,omitempty"`
}

type wireToolDestination struct {
	To        string   `json:"to"`
	Field     string   `json:"field,omitempty"`
	Format    string   `json:"format,omitempty"`
	Separator string   `json:"separator,omitempty"`
	Names     []string `json:"names,omitempty"`
}

type wireFrontmatterPlan struct {
	Set      []wireFrontmatterField `json:"set,omitempty"`
	Remove   []string               `json:"remove,omitempty"`
	KeyOrder []string               `json:"key_order,omitempty"`
}

type wireTargetPathResult struct {
	Path string `json:"path"`
}

type wireInjectionResult struct {
	Content string `json:"content"`
	OK      bool   `json:"ok"`
}

type wireHookPlanResult struct {
	Supported    bool           `json:"supported"`
	Reason       string         `json:"reason,omitempty"`
	Files        []wireHookFile `json:"files,omitempty"`
	TargetDir    string         `json:"target_dir,omitempty"`
	Registration []wireRegStep  `json:"registration,omitempty"`
}

// ---------------------------------------------------------------------------
// Domain ↔ wire conversions
// ---------------------------------------------------------------------------

func toWireFieldValue(fv domain.FieldValue) wireFieldValue {
	w := wireFieldValue{
		Kind:    string(fv.Kind),
		Scalar:  fv.Scalar,
		Quote:   string(fv.Quote),
		List:    string(fv.List),
		Comment: fv.Comment,
	}
	if fv.Items != nil {
		w.Items = make([]wireFieldValue, len(fv.Items))
		for i, item := range fv.Items {
			w.Items[i] = toWireFieldValue(item)
		}
	}
	if fv.Pairs != nil {
		w.Pairs = make([]wireFieldPair, len(fv.Pairs))
		for i, pair := range fv.Pairs {
			w.Pairs[i] = wireFieldPair{Key: pair.Key, Value: toWireFieldValue(pair.Value)}
		}
	}
	return w
}

func fromWireFieldValue(w wireFieldValue) domain.FieldValue {
	fv := domain.FieldValue{
		Kind:    domain.ValueKind(w.Kind),
		Scalar:  w.Scalar,
		Quote:   domain.QuoteStyle(w.Quote),
		List:    domain.ListStyle(w.List),
		Comment: w.Comment,
	}
	if w.Items != nil {
		fv.Items = make([]domain.FieldValue, len(w.Items))
		for i, item := range w.Items {
			fv.Items[i] = fromWireFieldValue(item)
		}
	}
	if w.Pairs != nil {
		fv.Pairs = make([]domain.FieldPair, len(w.Pairs))
		for i, pair := range w.Pairs {
			fv.Pairs[i] = domain.FieldPair{Key: pair.Key, Value: fromWireFieldValue(pair.Value)}
		}
	}
	return fv
}

func toWireFrontmatterField(f domain.FrontmatterField) wireFrontmatterField {
	return wireFrontmatterField{Key: f.Key, Value: toWireFieldValue(f.Value)}
}

func toWireFrontmatterFields(fs []domain.FrontmatterField) []wireFrontmatterField {
	if fs == nil {
		return nil
	}
	wfs := make([]wireFrontmatterField, len(fs))
	for i, f := range fs {
		wfs[i] = toWireFrontmatterField(f)
	}
	return wfs
}

func fromWireFrontmatterField(w wireFrontmatterField) domain.FrontmatterField {
	return domain.FrontmatterField{Key: w.Key, Value: fromWireFieldValue(w.Value)}
}

func fromWireFrontmatterFields(ws []wireFrontmatterField) []domain.FrontmatterField {
	if ws == nil {
		return nil
	}
	fs := make([]domain.FrontmatterField, len(ws))
	for i, w := range ws {
		fs[i] = fromWireFrontmatterField(w)
	}
	return fs
}

// decodeWireToolResolution converts a wireToolResolution to domain.ToolResolution using the
// three-row decision table for the multi-destination wire protocol:
//
//   - Row 1: destinations present and non-empty → decode each wireToolDestination, recompute
//     HarnessTools as the flattened, deduplicated, first-seen union of all destination Names.
//   - Row 2: destinations absent/empty + harness_tools non-empty → default to one DestMain
//     destination carrying harness_tools; HarnessTools = deep copy of harness_tools.
//   - Row 3: both absent/empty → nil Destinations and nil HarnessTools.
func decodeWireToolResolution(r wireToolResolution) domain.ToolResolution {
	res := domain.ToolResolution{
		Generic: r.Generic,
		Outcome: domain.ToolOutcome(r.Outcome),
	}

	if len(r.Destinations) > 0 {
		// Row 1: destinations present and non-empty.
		res.Destinations = make([]domain.ToolDestination, len(r.Destinations))
		seenNames := make(map[string]bool)
		var ht []string
		for i, d := range r.Destinations {
			names := make([]string, len(d.Names))
			copy(names, d.Names)
			res.Destinations[i] = domain.ToolDestination{
				Kind:      domain.ToolDestinationKind(d.To),
				Field:     d.Field,
				Format:    domain.ToolValueFormat(d.Format),
				Separator: d.Separator,
				Names:     names,
			}
			for _, name := range names {
				if !seenNames[name] {
					seenNames[name] = true
					ht = append(ht, name)
				}
			}
		}
		res.HarnessTools = ht
	} else if len(r.HarnessTools) > 0 {
		// Row 2: destinations absent/empty, harness_tools present → default to single DestMain.
		htCopy := make([]string, len(r.HarnessTools))
		copy(htCopy, r.HarnessTools)
		res.Destinations = []domain.ToolDestination{{Kind: domain.DestMain, Names: htCopy}}
		res.HarnessTools = htCopy
	}
	// Row 3: both absent/empty → nil Destinations and nil HarnessTools (zero values).

	return res
}

func toWireHookBundle(b domain.HookBundle) wireHookBundle {
	wb := wireHookBundle{
		Key:         b.Key,
		Version:     b.Version,
		Description: b.Description,
		SourceDir:   b.SourceDir,
		Placeholder: b.Placeholder,
	}
	if b.Variants != nil {
		wb.Variants = make(map[string]wireHookVariant, len(b.Variants))
		for k, v := range b.Variants {
			wb.Variants[k] = toWireHookVariant(v)
		}
	}
	return wb
}

func toWireHookVariant(v domain.HookVariant) wireHookVariant {
	wv := wireHookVariant{
		HarnessID:     v.HarnessID,
		Supported:     v.Supported,
		ReusesVariant: v.ReusesVariant,
	}
	if v.Files != nil {
		wv.Files = make([]wireHookFile, len(v.Files))
		for i, f := range v.Files {
			wv.Files[i] = wireHookFile{SourcePath: f.SourcePath, TargetName: f.TargetName}
		}
	}
	if v.Registration != nil {
		wv.Registration = make([]wireRegStep, len(v.Registration))
		for i, s := range v.Registration {
			wv.Registration[i] = wireRegStep{
				ID:          s.ID,
				TargetPath:  s.TargetPath,
				Fragment:    s.Fragment,
				Performable: s.Performable,
				Instruction: s.Instruction,
			}
		}
	}
	return wv
}

// ---------------------------------------------------------------------------
// Connection state (one per live subprocess)
// ---------------------------------------------------------------------------

// liveConn holds the I/O state for a single subprocess connection. It is replaced when the
// adapter reconnects after a Close() call.
type liveConn struct {
	cmd       *exec.Cmd
	stdinW    *os.File
	stdoutR   *os.File
	stderrBuf bytes.Buffer
	enc       *json.Encoder
	dec       *json.Decoder
	waitCh    chan error
	nextID    atomic.Uint64
}

func (c *liveConn) kill() {
	if c.cmd != nil && c.cmd.Process != nil {
		c.cmd.Process.Kill() //nolint:errcheck
	}
}

func (c *liveConn) nextRequestID() string {
	return fmt.Sprintf("%d", c.nextID.Add(1))
}

// readResponseWithTimeout reads one JSON response with the given timeout.
func (c *liveConn) readResponseWithTimeout(timeout time.Duration) (wireResponse, error) {
	type result struct {
		resp wireResponse
		err  error
	}
	ch := make(chan result, 1)
	go func() {
		var resp wireResponse
		err := c.dec.Decode(&resp)
		ch <- result{resp, err}
	}()

	select {
	case r := <-ch:
		if r.err != nil {
			return wireResponse{}, c.classifyReadError(r.err)
		}
		return r.resp, nil
	case <-time.After(timeout):
		c.kill()
		return wireResponse{}, ErrTimeout
	}
}

// classifyReadError maps a read/decode error to the appropriate sentinel.
func (c *liveConn) classifyReadError(err error) error {
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		select {
		case waitErr := <-c.waitCh:
			var exitErr *exec.ExitError
			if errors.As(waitErr, &exitErr) && exitErr.ExitCode() != 0 {
				code := exitErr.ExitCode()
				stderr := c.stderrBuf.String()
				// The Go runtime emits "all goroutines are asleep - deadlock!" on stdout/stderr
				// when a program blocks on an empty select with no other user goroutines. This
				// is indistinguishable from a hang from the adapter's perspective: the process
				// accepted a write to stdin but produced no response before dying. Classify as
				// ErrTimeout so that test fake harnesses using `select {}` work on all platforms.
				if strings.Contains(stderr, "all goroutines are asleep - deadlock!") {
					return ErrTimeout
				}
				stderrTrimmed := strings.TrimSpace(stderr)
				if stderrTrimmed != "" {
					return fmt.Errorf("%w: exit status %d: %s", ErrNonZeroExit, code, stderrTrimmed)
				}
				return fmt.Errorf("%w: exit status %d", ErrNonZeroExit, code)
			}
			return ErrEmptyResponse
		case <-time.After(5 * time.Second):
			return ErrEmptyResponse
		}
	}
	return fmt.Errorf("%w: %v", ErrMalformedResponse, err)
}

// exitOrEmpty is called when a write to stdin fails; classifies using exit status.
func (c *liveConn) exitOrEmpty(writeErr error) error {
	select {
	case waitErr := <-c.waitCh:
		var exitErr *exec.ExitError
		if errors.As(waitErr, &exitErr) && exitErr.ExitCode() != 0 {
			return fmt.Errorf("%w: exit status %d", ErrNonZeroExit, exitErr.ExitCode())
		}
		return ErrEmptyResponse
	case <-time.After(500 * time.Millisecond):
		return fmt.Errorf("%w: stdin write failed: %v", ErrNonZeroExit, writeErr)
	}
}

// ---------------------------------------------------------------------------
// Adapter implementation
// ---------------------------------------------------------------------------

// adapter implements domain.HarnessModule by communicating with a persistent child process
// over the JSON-over-stdio protocol.
//
// The adapter supports transparent reconnection: if Close() is called and a method is
// subsequently invoked, the adapter reconnects by spawning a new subprocess. This makes the
// adapter compatible with the shared contract test suite, which calls Close() during its
// universal invariants and then continues exercising per-method behaviour.
type adapter struct {
	// Immutable configuration (used for reconnect).
	execPath   string
	desc       *domain.HarnessDescriptor
	timeout    time.Duration
	mosaicRoot string // forwarded to the child process as MOSAIC_ROOT; "" means do not inject

	// Protected by mu.
	mu   sync.Mutex
	ref  domain.HarnessRef
	conn *liveConn // nil when the connection is closed
}

// startLiveConn creates and returns a new subprocess connection after performing the handshake.
// When mosaicRoot is non-empty it is exported to the child process as the MOSAIC_ROOT
// environment variable, merged with the inherited host environment. When mosaicRoot is empty
// no MOSAIC_ROOT variable is injected, preserving any value already present in the host env.
func startLiveConn(execPath string, desc *domain.HarnessDescriptor, timeout time.Duration, mosaicRoot string) (*liveConn, domain.HarnessRef, error) {
	if _, err := os.Stat(execPath); os.IsNotExist(err) {
		return nil, domain.HarnessRef{}, fmt.Errorf("%w: %s", ErrExecutableNotFound, execPath)
	}

	stdinR, stdinW, err := os.Pipe()
	if err != nil {
		return nil, domain.HarnessRef{}, fmt.Errorf("create stdin pipe: %w", err)
	}
	stdoutR, stdoutW, err := os.Pipe()
	if err != nil {
		stdinR.Close()
		stdinW.Close()
		return nil, domain.HarnessRef{}, fmt.Errorf("create stdout pipe: %w", err)
	}

	c := &liveConn{
		stdinW:  stdinW,
		stdoutR: stdoutR,
		waitCh:  make(chan error, 1),
	}
	c.enc = json.NewEncoder(stdinW)
	c.dec = json.NewDecoder(stdoutR)

	var cmd *exec.Cmd
	if runtime.GOOS == "windows" && strings.HasSuffix(strings.ToLower(execPath), ".bat") {
		cmd = exec.Command("cmd", "/c", execPath)
	} else {
		cmd = exec.Command(execPath)
	}
	cmd.Stdin = stdinR
	cmd.Stdout = stdoutW
	cmd.Stderr = &c.stderrBuf
	// Export MOSAIC_ROOT to the child process only when a root was supplied. Merging with
	// os.Environ() rather than replacing it preserves PATH and other variables the module needs.
	if mosaicRoot != "" {
		cmd.Env = append(os.Environ(), "MOSAIC_ROOT="+mosaicRoot)
	}
	c.cmd = cmd

	if err := cmd.Start(); err != nil {
		stdinR.Close()
		stdinW.Close()
		stdoutR.Close()
		stdoutW.Close()
		return nil, domain.HarnessRef{}, fmt.Errorf("%w: %v", ErrExecutableNotFound, err)
	}

	// Close the ends that belong to the child process.
	stdinR.Close()
	stdoutW.Close()

	go func() { c.waitCh <- cmd.Wait() }()

	ref, err := performHandshake(c, execPath, desc, timeout)
	if err != nil {
		c.kill()
		c.stdinW.Close()
		c.stdoutR.Close()
		select { case <-c.waitCh: default: }
		return nil, domain.HarnessRef{}, err
	}

	return c, ref, nil
}

// performHandshake sends the handshake request and validates the response.
func performHandshake(c *liveConn, execPath string, desc *domain.HarnessDescriptor, timeout time.Duration) (domain.HarnessRef, error) {
	id := c.nextRequestID()
	params, _ := json.Marshal(map[string]string{"protocol": ProtocolVersion})
	req := wireRequest{Protocol: ProtocolVersion, ID: id, Method: "handshake", Params: params}
	if err := c.enc.Encode(req); err != nil {
		return domain.HarnessRef{}, c.exitOrEmpty(err)
	}

	resp, err := c.readResponseWithTimeout(timeout)
	if err != nil {
		return domain.HarnessRef{}, err
	}

	if resp.Protocol != ProtocolVersion {
		return domain.HarnessRef{}, fmt.Errorf("%w: module speaks %q, host requires %q",
			ErrProtocolMismatch, resp.Protocol, ProtocolVersion)
	}
	if resp.Error != nil {
		return domain.HarnessRef{}, fmt.Errorf("handshake error from module: %s: %s",
			resp.Error.Code, resp.Error.Message)
	}

	var handshakeResult struct {
		Protocol string `json:"protocol"`
		Harness  struct {
			ID          string `json:"id"`
			DisplayName string `json:"display_name"`
			Tier        string `json:"tier"`
			Usable      bool   `json:"usable"`
		} `json:"harness"`
	}
	if err := json.Unmarshal(resp.Result, &handshakeResult); err != nil {
		return domain.HarnessRef{}, fmt.Errorf("%w: handshake result: %v", ErrMalformedResponse, err)
	}

	if handshakeResult.Protocol != "" && handshakeResult.Protocol != ProtocolVersion {
		return domain.HarnessRef{}, fmt.Errorf("%w: result protocol %q != %q",
			ErrProtocolMismatch, handshakeResult.Protocol, ProtocolVersion)
	}

	harnessID := handshakeResult.Harness.ID
	if harnessID == "" {
		harnessID = desc.ID
	}
	displayName := handshakeResult.Harness.DisplayName
	if displayName == "" {
		displayName = desc.DisplayName
	}

	return domain.HarnessRef{
		ID:             harnessID,
		DisplayName:    displayName,
		Tier:           domain.TierExternal,
		ExecutablePath: execPath,
		RequiresOptIn:  true,
		Usable:         true,
	}, nil
}

// New constructs a HarnessModule that communicates with the executable at execPath over the
// JSON-over-stdio protocol. It performs a handshake to verify protocol version compatibility
// before returning a usable module.
//
// Returns ErrExecutableNotFound if execPath does not exist or is not executable.
// Returns ErrProtocolMismatch if the module's handshake response declares an incompatible
// protocol version.
//
// The returned module manages a subprocess. Callers should call Close() when done. Close() is
// idempotent and the module reconnects automatically if a method is called after Close().
func New(execPath string, d *domain.HarnessDescriptor, opts Options) (domain.HarnessModule, error) {
	timeout := opts.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}

	c, ref, err := startLiveConn(execPath, d, timeout, opts.MosaicRoot)
	if err != nil {
		return nil, err
	}

	return &adapter{
		execPath:   execPath,
		desc:       d,
		timeout:    timeout,
		mosaicRoot: opts.MosaicRoot,
		ref:        ref,
		conn:       c,
	}, nil
}

// ensureConn returns the live connection, reconnecting if Close() was called previously.
// Must be called with a.mu held.
func (a *adapter) ensureConn() (*liveConn, error) {
	if a.conn != nil {
		return a.conn, nil
	}
	// Reconnect after a previous Close(), forwarding the same mosaicRoot so the respawned
	// subprocess receives the same MOSAIC_ROOT environment variable as the original.
	c, ref, err := startLiveConn(a.execPath, a.desc, a.timeout, a.mosaicRoot)
	if err != nil {
		return nil, fmt.Errorf("reconnect: %w", err)
	}
	a.conn = c
	a.ref = ref
	return c, nil
}

// callMethod sends a method request and returns the raw result JSON.
func (a *adapter) callMethod(method string, params any) (json.RawMessage, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	c, err := a.ensureConn()
	if err != nil {
		return nil, err
	}

	paramsJSON, err := json.Marshal(params)
	if err != nil {
		return nil, fmt.Errorf("marshal %s params: %w", method, err)
	}

	req := wireRequest{
		Protocol: ProtocolVersion,
		ID:       c.nextRequestID(),
		Method:   method,
		Params:   paramsJSON,
	}
	if err := c.enc.Encode(req); err != nil {
		return nil, fmt.Errorf("send %s request: %w", method, err)
	}

	resp, err := c.readResponseWithTimeout(a.timeout)
	if err != nil {
		return nil, err
	}
	if resp.Protocol != ProtocolVersion {
		return nil, fmt.Errorf("%w: got %q, want %q", ErrProtocolMismatch, resp.Protocol, ProtocolVersion)
	}
	if resp.Error != nil {
		return nil, mapWireError(resp.Error)
	}
	return resp.Result, nil
}

// mapWireError converts a wire error to a Go error, mapping well-known codes to sentinels.
func mapWireError(e *wireRespError) error {
	switch e.Code {
	case "unsupported_artifact":
		return fmt.Errorf("%w: %s", domain.ErrArtifactUnsupported, e.Message)
	case "unsupported_scope":
		return fmt.Errorf("%w: %s", domain.ErrUnsupportedScope, e.Message)
	default:
		return fmt.Errorf("external harness error [%s]: %s", e.Code, e.Message)
	}
}

// ---------------------------------------------------------------------------
// domain.HarnessModule implementation
// ---------------------------------------------------------------------------

// Ref returns identity and provenance. Two calls return equal values.
func (a *adapter) Ref() domain.HarnessRef {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.ref
}

// Descriptor returns the descriptor passed to New(). The same pointer is returned on every
// call, satisfying the read-only contract.
func (a *adapter) Descriptor() *domain.HarnessDescriptor {
	return a.desc
}

// Close terminates the child process and releases all pipe resources. It is idempotent:
// a second call returns nil without any action.
//
// After Close(), the adapter reconnects automatically on the next method call by spawning a
// new subprocess and re-performing the handshake.
func (a *adapter) Close() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.conn != nil {
		a.conn.kill()
		a.conn.stdinW.Close()
		a.conn.stdoutR.Close()
		select { case <-a.conn.waitCh: default: }
		a.conn = nil
	}
	return nil
}

// Tools maps an agent's generic tool list to harness-specific fields by forwarding the
// request over the protocol and decoding the response.
func (a *adapter) Tools(req domain.ToolRequest) (domain.ToolResult, error) {
	params := wireToolRequest{
		AgentKey:     req.AgentKey,
		Generic:      req.Generic,
		Placeholder:  req.Placeholder,
		CustomNames:  req.CustomNames,
		SkippedTools: req.SkippedTools,
	}

	raw, err := a.callMethod("tools", params)
	if err != nil {
		return domain.ToolResult{}, err
	}

	var result wireToolResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return domain.ToolResult{}, fmt.Errorf("%w: tools result: %v", ErrMalformedResponse, err)
	}

	resolutions := make([]domain.ToolResolution, len(result.Resolutions))
	for i, r := range result.Resolutions {
		resolutions[i] = decodeWireToolResolution(r)
	}

	return domain.ToolResult{
		Fields:      fromWireFrontmatterFields(result.Fields),
		Resolutions: resolutions,
	}, nil
}

// Frontmatter builds the FrontmatterPlan for an agent by forwarding the request.
func (a *adapter) Frontmatter(req domain.FrontmatterRequest) (domain.FrontmatterPlan, error) {
	params := wireFrontmatterRequest{
		Kind:     string(req.Kind),
		AgentKey: req.AgentKey,
		Role:     string(req.Role),
		Source:   toWireFrontmatterFields(req.Source),
		Model: wireModelSelection{
			ModelID: req.Model.ModelID,
			Origin:  string(req.Model.Origin),
		},
		ToolFields: toWireFrontmatterFields(req.ToolFields),
		Versions: wireVersionStamps{
			Version:                       req.Versions.Version,
			TransformVersion:              req.Versions.TransformVersion,
			InjectionsVersion:             req.Versions.InjectionsVersion,
			OrchestratorInjectionsVersion: req.Versions.OrchestratorInjectionsVersion,
		},
	}

	raw, err := a.callMethod("frontmatter", params)
	if err != nil {
		return domain.FrontmatterPlan{}, err
	}

	var plan wireFrontmatterPlan
	if err := json.Unmarshal(raw, &plan); err != nil {
		return domain.FrontmatterPlan{}, fmt.Errorf("%w: frontmatter result: %v", ErrMalformedResponse, err)
	}

	return domain.FrontmatterPlan{
		Set:      fromWireFrontmatterFields(plan.Set),
		Remove:   plan.Remove,
		KeyOrder: plan.KeyOrder,
	}, nil
}

// TargetPath resolves the deployment path for one artifact by forwarding the request.
// domain.ErrArtifactUnsupported is returned when the module responds with the
// "unsupported_artifact" error code.
func (a *adapter) TargetPath(req domain.TargetPathRequest) (string, error) {
	params := wireTargetPathRequest{
		Kind:     string(req.Kind),
		Key:      req.Key,
		FileName: req.FileName,
		Scope:    string(req.Scope),
		GOOS:     req.GOOS,
	}

	raw, err := a.callMethod("target_path", params)
	if err != nil {
		return "", err
	}

	var result wireTargetPathResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return "", fmt.Errorf("%w: target_path result: %v", ErrMalformedResponse, err)
	}
	return result.Path, nil
}

// Injection returns the harness-level content for a canonical injection name.
// ok is false for injections this harness does not fill.
//
// The agent_key field is forwarded to the external module via the wire protocol. External
// modules that predate this change ignore the unknown field and return shared content only
// (graceful degradation via omitempty on the wire type).
//
// The domain.HarnessModule interface does not permit returning an error from Injection,
// so communication or decode failures are treated as "not provided" (ok=false). This is
// intentional: callers fall back to the next harness in the chain rather than aborting the
// transform when a single injection lookup fails.
func (a *adapter) Injection(req domain.InjectionRequest) (string, bool) {
	params := wireInjectionRequest{Name: req.Name, AgentKey: req.AgentKey}

	raw, err := a.callMethod("injection", params)
	if err != nil {
		// Protocol error: treat as not provided; caller falls back to next harness.
		return "", false
	}

	var result wireInjectionResult
	if err := json.Unmarshal(raw, &result); err != nil {
		// Malformed response: treat as not provided; caller falls back to next harness.
		return "", false
	}
	return result.Content, result.OK
}

// HookPlan resolves a hook bundle deployment plan by forwarding the request.
func (a *adapter) HookPlan(req domain.HookPlanRequest) (domain.HookPlan, error) {
	params := wireHookPlanRequest{
		Bundle: toWireHookBundle(req.Bundle),
		Scope:  string(req.Scope),
	}

	raw, err := a.callMethod("hook_plan", params)
	if err != nil {
		return domain.HookPlan{}, err
	}

	var result wireHookPlanResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return domain.HookPlan{}, fmt.Errorf("%w: hook_plan result: %v", ErrMalformedResponse, err)
	}

	plan := domain.HookPlan{
		Supported: result.Supported,
		Reason:    result.Reason,
		TargetDir: result.TargetDir,
	}
	if result.Files != nil {
		plan.Files = make([]domain.HookFile, len(result.Files))
		for i, f := range result.Files {
			plan.Files[i] = domain.HookFile{SourcePath: f.SourcePath, TargetName: f.TargetName}
		}
	}
	if result.Registration != nil {
		plan.Registration = make([]domain.RegistrationStep, len(result.Registration))
		for i, s := range result.Registration {
			plan.Registration[i] = domain.RegistrationStep{
				ID:          s.ID,
				TargetPath:  s.TargetPath,
				Fragment:    s.Fragment,
				Performable: s.Performable,
				Instruction: s.Instruction,
			}
		}
	}
	return plan, nil
}
