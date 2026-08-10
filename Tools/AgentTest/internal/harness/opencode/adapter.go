package opencode

import (
	"mosaic-agent-test/internal/domain"
)

// Options configures the adapter. Fields are added as later passes need
// them (scope inspection, provisioning, correlation).
type Options struct {
	// ScopeProbe reads configuration-scope documents. Defaults to a real
	// filesystem probe when nil (I6.1); overridden in tests with a fixture
	// probe so inspection is testable without the real filesystem.
	ScopeProbe ScopeProbe
}

// Adapter implements domain.HarnessAdapter for the OpenCode harness.
type Adapter struct {
	opts Options
}

var _ domain.HarnessAdapter = (*Adapter)(nil)

// New constructs the adapter. Construction alone performs no I/O.
func New(opts Options) *Adapter {
	return &Adapter{opts: opts}
}

// ID implements domain.HarnessAdapter.
func (a *Adapter) ID() string {
	return HarnessID
}

// Capabilities is a package-level function as well as a method, so the
// declared values can be asserted without constructing an adapter.
//
// SupportsDirectSubstitution is false: the pre-invocation hook
// (tool.execute.before) can block a call by throwing and can rewrite the
// call's arguments before it runs, but it cannot return a fabricated
// successful result — throwing surfaces to the subject as a refusal, not a
// response. The post-invocation hook's (tool.execute.after) output
// mutations are confirmed not to be honoured downstream, and its runtime
// output shape is inconsistent between two undocumented forms, so it is not
// a substitution mechanism either. Result substitution through OpenCode's
// custom-tool-shadowing mechanism is a structurally different approach,
// deferred to a future task and not implemented here.
//
// SupportsPostInterception is true: a post-invocation hook exists and
// observes the real result, which is what makes in-flight echo-fidelity
// comparison possible. It is an observation point only.
//
// CorrelationField is "args.prompt": the dispatch tool's prompt argument is
// echoed to the post-invocation hook, so it is the field a planted
// correlation token can survive in.
//
// RegistrationModel is domain.RegistrationDirectoryDrop: OpenCode discovers
// plugins by their presence in the plugins directory; there is no shared
// configuration document to declare them in.
//
// BridgeKind is domain.BridgeInProcess: the plugin loads inside the
// harness's own runtime and forwards to the interceptor binary as a child
// process.
func Capabilities() domain.HarnessCapabilities {
	return domain.HarnessCapabilities{
		SupportsDirectSubstitution: false,
		SupportsPostInterception:   true,
		CorrelationField:           "args.prompt",
		RegistrationModel:          domain.RegistrationDirectoryDrop,
		BridgeKind:                 domain.BridgeInProcess,
		// This adapter performs no normalization (see
		// ContractsDesign.md's dispatch-tool vocabulary contract): its
		// TranslateCall propagates the native name verbatim, so the one
		// name it can actually produce is its own un-normalized native
		// name, not domain.DispatchToolName.
		ProducibleToolNames: []string{InterceptedToolName},
	}
}

// Capabilities implements domain.HarnessAdapter.
func (a *Adapter) Capabilities() domain.HarnessCapabilities {
	return Capabilities()
}
