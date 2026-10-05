// Package roleenv defines the MOSAIC role environment the Runner adds to every
// harness process it spawns, and the run-scoped construction data adapters need
// to build it.
package roleenv

// Variable names and role values (pinned contract).
const (
	EnvRunID           = "MOSAIC_RUN_ID"
	EnvRole            = "MOSAIC_ROLE"
	EnvAgentInstanceID = "MOSAIC_AGENT_INSTANCE_ID"

	RoleSubagent     = "subagent"
	RoleOrchestrator = "orchestrator"
)

// SubagentEnv returns the "KEY=value" entries for a protocol invocation
// (HarnessAdapter.Invoke): MOSAIC_RUN_ID, MOSAIC_ROLE=subagent and
// MOSAIC_AGENT_INSTANCE_ID, in that order. An entry whose value is empty is
// omitted; MOSAIC_ROLE is always present. Never returns nil.
func SubagentEnv(runID, agentInstanceID string) []string {
	env := make([]string, 0, 3)
	env = appendIfSet(env, EnvRunID, runID)
	env = append(env, EnvRole+"="+RoleSubagent)
	return appendIfSet(env, EnvAgentInstanceID, agentInstanceID)
}

// OrchestratorEnv returns the entries for a script-orchestrator call
// (RawInvoker.InvokeRaw): MOSAIC_RUN_ID and MOSAIC_ROLE=orchestrator.
// MOSAIC_AGENT_INSTANCE_ID is never included. Empty runID omits MOSAIC_RUN_ID.
func OrchestratorEnv(runID string) []string {
	env := make([]string, 0, 2)
	env = appendIfSet(env, EnvRunID, runID)
	return append(env, EnvRole+"="+RoleOrchestrator)
}

// appendIfSet appends "key=value" unless value is empty, so an empty value is
// never written as "KEY=".
func appendIfSet(env []string, key, value string) []string {
	if value == "" {
		return env
	}
	return append(env, key+"="+value)
}

// Config is the run-scoped construction data an adapter needs to build the
// role environment.
type Config struct {
	RunID string // run id for InvokeRaw spawns and the Invoke fallback; "" when unknown
}

// Option configures Config (options pattern).
type Option func(*Config)

// WithRunID sets Config.RunID. An empty value leaves RunID empty.
func WithRunID(runID string) Option {
	return func(c *Config) { c.RunID = runID }
}

// NewConfig applies opts in order to a zero Config. Nil options are ignored.
func NewConfig(opts ...Option) Config {
	var c Config
	for _, opt := range opts {
		if opt != nil {
			opt(&c)
		}
	}
	return c
}
