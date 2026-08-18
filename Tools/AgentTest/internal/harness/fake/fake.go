// Package fake provides a scripted domain.HarnessAdapter with no LLM
// dependency. It is subject to the same conformance suite
// (internal/harness/contract) as every real adapter, so a whole-pipeline
// run can be driven through it with no LLM and no special case in the code
// under test.
//
// The adapter's own native wire shape (pre/post interception payloads and
// outcome replies) is private to tests in this package
// (internal/harness/fake/fake_test.go and conformance_test.go); it is never
// shared with a real harness.
package fake

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"mosaic-agent-test/internal/domain"
)

// Turn is one scripted collaborator response, consumed in order from the
// Script queue for its collaborator identity.
//
// Exactly one of Body, Raw or Err should be set:
//   - Body: the collaborator's response text, used to drive echo fidelity.
//   - Raw: a deliberately malformed scripted payload, to exercise the
//     malformed-payload error path without touching the native wire.
//   - Err: an error to return directly, to exercise the harness-level
//     error path.
type Turn struct {
	Body string
	Raw  []byte
	Err  error
}

// SubjectTurn is one scripted step of what the fake subject does: either it
// invokes a collaborator, or it produces its final result. Exactly one of
// Invoke or Result should be set.
type SubjectTurn struct {
	Invoke *domain.CollaboratorIdentity
	Result *domain.SubjectResult
}

// Options configures the scripted adapter.
type Options struct {
	// Capabilities is returned verbatim by the adapter, so both the
	// substitute and the rewrite paths are reachable without a real
	// harness.
	Capabilities domain.HarnessCapabilities

	// Script maps a collaborator identity key (domain.CollaboratorIdentity.Key)
	// to the turns that identity will produce, consumed in order. An
	// exhausted script surfaces as an error, never as blocking or a panic.
	Script map[string][]Turn

	// SubjectTurns are what the scripted subject emits, in order.
	SubjectTurns []SubjectTurn
}

// Recorded is one full decision cycle the adapter translated: the call it
// decoded and the outcome it was asked to carry back.
type Recorded struct {
	Phase    domain.InterceptionPhase
	Identity domain.CollaboratorIdentity
	Message  domain.TaskMessage
	Outcome  domain.InterceptionOutcome
}

// Adapter is a scripted domain.HarnessAdapter with no LLM dependency.
type Adapter struct {
	opts        Options
	invocations []Recorded
	scriptIdx   map[string]int
}

var _ domain.HarnessAdapter = (*Adapter)(nil)

// New constructs a scripted adapter from opts. Script queues are consumed
// as TranslateCall(PhasePost, ...) is called for a given collaborator
// identity; nothing is consumed by New itself.
func New(opts Options) *Adapter {
	return &Adapter{opts: opts}
}

// Invocations returns every call the adapter translated, in order, so
// tests can assert call sequence and argument values.
func (a *Adapter) Invocations() []Recorded {
	return a.invocations
}

// RemainingScript reports unconsumed scripted turns across every
// collaborator identity. A non-zero value after a run means fewer
// collaborators were invoked than the test author intended.
func (a *Adapter) RemainingScript() int {
	total := 0
	for key, turns := range a.opts.Script {
		consumed := 0
		if a.scriptIdx != nil {
			consumed = a.scriptIdx[key]
		}
		if remaining := len(turns) - consumed; remaining > 0 {
			total += remaining
		}
	}
	return total
}

// ID implements domain.HarnessAdapter.
func (a *Adapter) ID() string {
	return "fake"
}

// Capabilities implements domain.HarnessAdapter. It returns exactly what
// Options.Capabilities declared, so the fake adapter is honest by
// construction — its whole purpose is to let tests choose an effect
// deliberately.
func (a *Adapter) Capabilities() domain.HarnessCapabilities {
	return a.opts.Capabilities
}

// ConfigScopes implements domain.HarnessAdapter. The fake adapter merges no
// configuration scope beyond the sandbox it is given, so there is nothing
// to enumerate.
func (a *Adapter) ConfigScopes() []domain.ConfigScope {
	return nil
}

// InspectScopes implements domain.HarnessAdapter. There are no non-sandbox
// scopes to inspect (see ConfigScopes), so InspectScopes always succeeds
// with no findings.
func (a *Adapter) InspectScopes(ctx context.Context) ([]domain.ScopeFinding, error) {
	return nil, nil
}

// CheckEnvironment implements domain.HarnessAdapter. It is trivially
// successful: the scripted adapter exists so a whole-pipeline run can be
// driven with no LLM and no special case, and a pre-flight refusal from the
// scripted adapter itself would defeat that purpose. It runs no interpreter,
// so InterpreterApplicable stays false.
func (a *Adapter) CheckEnvironment(ctx context.Context) (domain.EnvironmentReport, error) {
	return domain.EnvironmentReport{}, nil
}

// fakeMarkerName is the file Provision writes into the sandbox's control
// directory to record that this adapter is installed. Deprovision removes
// exactly this file and any directory it created to hold it.
const fakeMarkerName = "fake-adapter-installed.json"

// Provision implements domain.HarnessAdapter. It writes only under
// req.Sandbox: a marker file in the control directory recording that the
// scripted adapter is installed, creating the subject and control
// directories first when they do not already exist.
func (a *Adapter) Provision(ctx context.Context, req domain.ProvisionRequest) (domain.Provisioning, error) {
	sb := req.Sandbox

	var dirs []string
	for _, d := range []string{sb.SubjectDir, sb.ControlDir} {
		if d == "" {
			continue
		}
		if _, err := os.Stat(d); err == nil {
			continue
		} else if !os.IsNotExist(err) {
			return domain.Provisioning{}, fmt.Errorf("fake: stat %q: %w", d, err)
		}
		if err := os.MkdirAll(d, 0o755); err != nil {
			return domain.Provisioning{}, fmt.Errorf("fake: mkdir %q: %w", d, err)
		}
		dirs = append(dirs, d)
	}

	markerPath := filepath.Join(sb.ControlDir, fakeMarkerName)
	if err := os.WriteFile(markerPath, []byte(`{"adapter":"fake"}`), 0o644); err != nil {
		return domain.Provisioning{}, fmt.Errorf("fake: write marker: %w", err)
	}

	return domain.Provisioning{
		Sandbox: sb,
		Files:   []string{markerPath},
		Dirs:    dirs,
	}, nil
}

// Deprovision implements domain.HarnessAdapter. It removes exactly what
// Provisioning records and nothing else, and is idempotent: a recorded
// path already gone is not an error.
func (a *Adapter) Deprovision(ctx context.Context, p domain.Provisioning) error {
	for _, f := range p.Files {
		if err := os.Remove(f); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("fake: remove %q: %w", f, err)
		}
	}
	for i := len(p.Dirs) - 1; i >= 0; i-- {
		d := p.Dirs[i]
		if err := os.RemoveAll(d); err != nil {
			return fmt.Errorf("fake: remove dir %q: %w", d, err)
		}
	}
	return nil
}

// subjectTurnWire is this package's own private wire shape for the script a
// scripted stand-in subject replays, carried in the SpawnPlan's Stdin.
// Exactly one of Invoke or Result is set per entry, mirroring SubjectTurn.
type subjectTurnWire struct {
	Invoke *domain.CollaboratorIdentity `json:"invoke,omitempty"`
	Result *subjectResultWire           `json:"result,omitempty"`
}

type subjectResultWire struct {
	ProtocolMessage string `json:"protocol_message"`
	Disposition     string `json:"disposition"`
	ExitCode        int    `json:"exit_code"`
}

// SpawnPlan implements domain.HarnessAdapter. The returned plan names a
// scripted stand-in rather than a real harness CLI, and carries
// Options.SubjectTurns encoded into Stdin so the stand-in can replay them
// with no LLM and no special case in the code under test. It performs no
// process control itself.
func (a *Adapter) SpawnPlan(ctx context.Context, subject domain.SubjectUnderTest, p domain.Provisioning) (domain.SpawnPlan, error) {
	wire := make([]subjectTurnWire, 0, len(a.opts.SubjectTurns))
	for _, st := range a.opts.SubjectTurns {
		w := subjectTurnWire{}
		if st.Invoke != nil {
			id := *st.Invoke
			w.Invoke = &id
		}
		if st.Result != nil {
			w.Result = &subjectResultWire{
				ProtocolMessage: st.Result.ProtocolMessage,
				Disposition:     string(st.Result.Disposition),
				ExitCode:        st.Result.ExitCode,
			}
		}
		wire = append(wire, w)
	}

	stdin, err := json.Marshal(wire)
	if err != nil {
		return domain.SpawnPlan{}, fmt.Errorf("fake: encode subject-turn script: %w", err)
	}

	workingDir := p.Sandbox.SubjectDir
	if workingDir == "" {
		workingDir = subject.DefinitionPath
	}

	return domain.SpawnPlan{
		Executable: "fake-subject",
		Args:       []string{"--sandbox", p.Sandbox.ControlDir},
		WorkingDir: workingDir,
		Stdin:      stdin,
	}, nil
}

// wireCall is this package's own private native pre/post payload shape,
// mirrored by the test helpers in fake_test.go and conformance_test.go.
type wireCall struct {
	Phase           string `json:"phase"`
	Tool            string `json:"tool"`
	Agent           string `json:"agent"`
	AgentInstanceID string `json:"agent_instance_id"`
	TaskDescription string `json:"task_description"`
	Token           string `json:"token"`
}

// TranslateCall implements domain.HarnessAdapter.
//
// On PhasePre it decodes the fake's own native wire shape into a normalized
// call. On PhasePost it additionally consumes the next Turn scripted for
// the decoded collaborator identity: an exhausted script, a scripted Err,
// or a malformed scripted Raw payload must each surface as a handleable
// error here, never a panic and never a zero-valued call on success.
func (a *Adapter) TranslateCall(phase domain.InterceptionPhase, native []byte) (domain.InterceptedCall, error) {
	var w wireCall
	if err := json.Unmarshal(native, &w); err != nil {
		return domain.InterceptedCall{}, fmt.Errorf("fake: malformed native payload: %w", err)
	}
	if w.Tool == "" || w.Token == "" {
		return domain.InterceptedCall{}, fmt.Errorf("fake: unrecognised native payload: %+v", w)
	}

	id := domain.CollaboratorIdentity{ToolName: w.Tool, AgentIdentity: w.Agent}
	call := domain.InterceptedCall{
		Phase:            phase,
		Identity:         id,
		Message:          domain.TaskMessage{AgentInstanceID: w.AgentInstanceID, TaskDescription: w.TaskDescription},
		CorrelationToken: w.Token,
		RawPayload:       native,
		Capabilities:     a.Capabilities(),
	}

	if phase != domain.PhasePost {
		return call, nil
	}

	turn, err := a.nextTurn(id)
	if err != nil {
		return domain.InterceptedCall{}, err
	}
	if turn.Err != nil {
		return domain.InterceptedCall{}, turn.Err
	}
	if turn.Raw != nil {
		var probe json.RawMessage
		if err := json.Unmarshal(turn.Raw, &probe); err != nil {
			return domain.InterceptedCall{}, fmt.Errorf("fake: malformed scripted payload for %q: %w", id.Key(), err)
		}
		call.ObservedResponse = string(turn.Raw)
		return call, nil
	}

	call.ObservedResponse = turn.Body
	return call, nil
}

// nextTurn returns the next scripted Turn for id, advancing its per-identity
// index. An identity with no remaining scripted turns — including one with
// no script at all — is reported as an error, never a panic.
func (a *Adapter) nextTurn(id domain.CollaboratorIdentity) (Turn, error) {
	key := id.Key()
	turns := a.opts.Script[key]

	if a.scriptIdx == nil {
		a.scriptIdx = make(map[string]int)
	}
	idx := a.scriptIdx[key]
	if idx >= len(turns) {
		return Turn{}, fmt.Errorf("fake: script exhausted for collaborator %q", key)
	}
	a.scriptIdx[key] = idx + 1
	return turns[idx], nil
}

// wireReply is this package's own private native outcome-reply shape.
type wireReply struct {
	Kind   string          `json:"kind"`
	Body   json.RawMessage `json:"body,omitempty"`
	Prompt string          `json:"prompt,omitempty"`
	Token  string          `json:"token"`
}

// TranslateOutcome implements domain.HarnessAdapter. It also appends the
// completed decision to Invocations.
func (a *Adapter) TranslateOutcome(outcome domain.InterceptionOutcome, call domain.InterceptedCall) ([]byte, error) {
	reply := wireReply{
		Kind:   string(outcome.Kind),
		Body:   outcome.StubResponse,
		Prompt: outcome.RewrittenPrompt,
		Token:  outcome.CorrelationToken,
	}
	b, err := json.Marshal(reply)
	if err != nil {
		return nil, fmt.Errorf("fake: encode outcome reply: %w", err)
	}

	a.invocations = append(a.invocations, Recorded{
		Phase:    call.Phase,
		Identity: call.Identity,
		Message:  call.Message,
		Outcome:  outcome,
	})

	return b, nil
}
