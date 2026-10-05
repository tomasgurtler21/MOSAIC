package ghcpcli_test

// Tests for the MOSAIC role environment the GHCP CLI adapter adds to every
// process it spawns: protocol invocations carry the subagent role, script
// orchestrator calls carry the orchestrator role, and both keep the inherited
// environment. Uses the fake-CLI helper process in helperprocess_test.go, which
// records its environment to GO_HELPER_ENV_FILE.

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mosaic-run/internal/domain"
	"mosaic-run/internal/harness/ghcpcli"
	"mosaic-run/internal/harness/roleenv"

	commonharness "mosaic-common/harness"
)

const (
	roleEnvRunIDConstructed = "20261003T185044Z-bbbb"
	roleEnvRunIDRequest     = "20261003T185044Z-aaaa"
)

// newRoleEnvAdapter builds the adapter under test, bound to the given run id
// ("" builds it without any run-id option).
func newRoleEnvAdapter(t *testing.T, runID string) *ghcpcli.GHCPCLIAdapter {
	t.Helper()
	var opts []roleenv.Option
	if runID != "" {
		opts = append(opts, roleenv.WithRunID(runID))
	}
	return ghcpcli.NewGHCPCLIAdapterWithMode(helperExe(t), 5*time.Second, nil, commonharness.GHCPCLIModeBlanket, opts...)
}

// captureSpawnEnv makes the fake CLI record its environment and removes any
// MOSAIC role variables from the test process, so what the spawned process
// sees comes from the adapter alone. Returns the path of the recorded
// environment file.
func captureSpawnEnv(t *testing.T, scenario string) (envFile string) {
	t.Helper()
	setHelperEnv(t, scenario)
	envFile = filepath.Join(t.TempDir(), "env.json")
	t.Setenv("GO_HELPER_ENV_FILE", envFile)
	for _, key := range []string{roleenv.EnvRunID, roleenv.EnvRole, roleenv.EnvAgentInstanceID} {
		t.Setenv(key, "") // registers restoration of the original value
		os.Unsetenv(key)  //nolint:errcheck
	}
	return envFile
}

// readSpawnEnv returns the environment recorded by the fake CLI as a map.
func readSpawnEnv(t *testing.T, envFile string) map[string]string {
	t.Helper()
	data, err := os.ReadFile(envFile)
	if err != nil {
		t.Fatalf("spawned process did not record its environment: %v", err)
	}
	var entries []string
	if err := json.Unmarshal(data, &entries); err != nil {
		t.Fatalf("could not decode recorded environment: %v", err)
	}
	env := make(map[string]string, len(entries))
	for _, e := range entries {
		if i := strings.Index(e, "="); i > 0 {
			env[e[:i]] = e[i+1:]
		}
	}
	return env
}

func assertEnvValue(t *testing.T, env map[string]string, key, want string) {
	t.Helper()
	got, ok := env[key]
	if !ok {
		t.Errorf("%s is absent from the spawned environment, want %q", key, want)
		return
	}
	if got != want {
		t.Errorf("%s = %q, want %q", key, got, want)
	}
}

func assertEnvAbsent(t *testing.T, env map[string]string, key string) {
	t.Helper()
	if got, ok := env[key]; ok {
		t.Errorf("%s = %q is present in the spawned environment, want it absent", key, got)
	}
}

// roleEnvRequest is a protocol request for the given instance id and run id.
func roleEnvRequest(instanceID, runID string) domain.ProtocolRequest {
	req := minimalClaudeRequest(instanceID)
	req.RunID = runID
	return req
}

func invokeForEnv(t *testing.T, adapter *ghcpcli.GHCPCLIAdapter, instanceID, runID string) {
	t.Helper()
	if _, err := adapter.Invoke(context.Background(), ordinaryAgentRef(), roleEnvRequest(instanceID, runID)); err != nil {
		t.Fatalf("unexpected Invoke error: %v", err)
	}
}

func invokeRawForEnv(t *testing.T, adapter *ghcpcli.GHCPCLIAdapter) {
	t.Helper()
	if _, err := adapter.InvokeRaw(context.Background(), orchestratorRef(), rawPayload); err != nil {
		t.Fatalf("unexpected InvokeRaw error: %v", err)
	}
}

func TestGHCPCLIAdapter_Invoke_SpawnCarriesSubagentRoleEnvironment(t *testing.T) {
	envFile := captureSpawnEnv(t, "ghcpcli-success")
	adapter := newRoleEnvAdapter(t, roleEnvRunIDConstructed)

	invokeForEnv(t, adapter, "Research#3", roleEnvRunIDRequest)

	env := readSpawnEnv(t, envFile)
	assertEnvValue(t, env, "MOSAIC_RUN_ID", roleEnvRunIDRequest)
	assertEnvValue(t, env, "MOSAIC_ROLE", "subagent")
	assertEnvValue(t, env, "MOSAIC_AGENT_INSTANCE_ID", "Research#3")
}

func TestGHCPCLIAdapter_Invoke_EmptyRequestRunID_FallsBackToConstructionRunID(t *testing.T) {
	envFile := captureSpawnEnv(t, "ghcpcli-success")
	adapter := newRoleEnvAdapter(t, roleEnvRunIDConstructed)

	invokeForEnv(t, adapter, "Research#3", "")

	env := readSpawnEnv(t, envFile)
	assertEnvValue(t, env, "MOSAIC_RUN_ID", roleEnvRunIDConstructed)
	assertEnvValue(t, env, "MOSAIC_ROLE", "subagent")
}

func TestGHCPCLIAdapter_Invoke_WithoutAnyRunID_OmitsRunIDButKeepsRole(t *testing.T) {
	envFile := captureSpawnEnv(t, "ghcpcli-success")
	adapter := newRoleEnvAdapter(t, "")

	invokeForEnv(t, adapter, "Research#3", "")

	env := readSpawnEnv(t, envFile)
	assertEnvAbsent(t, env, "MOSAIC_RUN_ID")
	assertEnvValue(t, env, "MOSAIC_ROLE", "subagent")
	assertEnvValue(t, env, "MOSAIC_AGENT_INSTANCE_ID", "Research#3")
}

func TestGHCPCLIAdapter_Invoke_KeepsInheritedEnvironment(t *testing.T) {
	envFile := captureSpawnEnv(t, "ghcpcli-success")
	t.Setenv("MOSAIC_TEST_INHERITED_MARKER", "kept-value")
	adapter := newRoleEnvAdapter(t, roleEnvRunIDConstructed)

	invokeForEnv(t, adapter, "Research#3", roleEnvRunIDRequest)

	env := readSpawnEnv(t, envFile)
	assertEnvValue(t, env, "MOSAIC_TEST_INHERITED_MARKER", "kept-value")
	assertEnvValue(t, env, "MOSAIC_ROLE", "subagent")
}

func TestGHCPCLIAdapter_InvokeRaw_SpawnCarriesOrchestratorRoleEnvironment(t *testing.T) {
	envFile := captureSpawnEnv(t, "ghcpcli-success")
	adapter := newRoleEnvAdapter(t, roleEnvRunIDConstructed)

	invokeRawForEnv(t, adapter)

	env := readSpawnEnv(t, envFile)
	assertEnvValue(t, env, "MOSAIC_RUN_ID", roleEnvRunIDConstructed)
	assertEnvValue(t, env, "MOSAIC_ROLE", "orchestrator")
	assertEnvAbsent(t, env, "MOSAIC_AGENT_INSTANCE_ID")
}

func TestGHCPCLIAdapter_InvokeRaw_WithoutRunID_OmitsRunIDButKeepsRole(t *testing.T) {
	envFile := captureSpawnEnv(t, "ghcpcli-success")
	adapter := newRoleEnvAdapter(t, "")

	invokeRawForEnv(t, adapter)

	env := readSpawnEnv(t, envFile)
	assertEnvAbsent(t, env, "MOSAIC_RUN_ID")
	assertEnvValue(t, env, "MOSAIC_ROLE", "orchestrator")
	assertEnvAbsent(t, env, "MOSAIC_AGENT_INSTANCE_ID")
}

func TestGHCPCLIAdapter_InvokeRaw_KeepsInheritedEnvironment(t *testing.T) {
	envFile := captureSpawnEnv(t, "ghcpcli-success")
	t.Setenv("MOSAIC_TEST_INHERITED_MARKER", "kept-value")
	adapter := newRoleEnvAdapter(t, roleEnvRunIDConstructed)

	invokeRawForEnv(t, adapter)

	env := readSpawnEnv(t, envFile)
	assertEnvValue(t, env, "MOSAIC_TEST_INHERITED_MARKER", "kept-value")
	assertEnvValue(t, env, "MOSAIC_ROLE", "orchestrator")
}

func TestGHCPCLIAdapter_RunID_ReturnsTheConstructionRunID(t *testing.T) {
	if got := newRoleEnvAdapter(t, roleEnvRunIDConstructed).RunID(); got != roleEnvRunIDConstructed {
		t.Errorf("RunID() = %q, want %q", got, roleEnvRunIDConstructed)
	}
	if got := newRoleEnvAdapter(t, "").RunID(); got != "" {
		t.Errorf("RunID() without option = %q, want empty", got)
	}
}
