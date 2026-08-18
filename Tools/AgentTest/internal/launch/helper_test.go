package launch_test

// Shared fake-subprocess infrastructure for this package's tests, mirroring
// mosaic-common/harness's own helper-process pattern: this test binary
// doubles as a fake CLI when GO_WANT_HELPER_PROCESS=1 is set, so Launch can
// be exercised against a real (if trivial) subprocess without depending on
// any real harness CLI being installed.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") == "1" {
		runHelperProcess()
	}
	os.Exit(m.Run())
}

func runHelperProcess() {
	if argsFile := os.Getenv("GO_HELPER_ARGS_FILE"); argsFile != "" {
		data, _ := json.Marshal(os.Args[1:])
		os.WriteFile(argsFile, data, 0644) //nolint:errcheck
	}

	switch os.Getenv("GO_HELPER_CMD") {
	case "success":
		os.Stdout.WriteString(`[{"type":"result","subtype":"success","is_error":false,"result":"{\"agent_instance_id\":\"orchestrator#1\",\"status_code\":\"SUCCESS\",\"status_message\":\"ok\"}"}]`) //nolint:errcheck
	case "hang":
		time.Sleep(24 * time.Hour)
	default:
		os.Stderr.WriteString("unknown GO_HELPER_CMD: " + os.Getenv("GO_HELPER_CMD")) //nolint:errcheck
		os.Exit(2)
	}
	os.Exit(0)
}

// helperExe returns the path to this test binary, which acts as the fake
// CLI executable when GO_WANT_HELPER_PROCESS=1 is set.
func helperExe(t *testing.T) string {
	t.Helper()
	return os.Args[0]
}

// setHelperEnv configures the current process environment so a subprocess
// spawned by the code under test enters helper-process mode.
func setHelperEnv(t *testing.T, cmd string) {
	t.Helper()
	t.Setenv("GO_WANT_HELPER_PROCESS", "1")
	t.Setenv("GO_HELPER_CMD", cmd)
}

// setHelperEnvCapturingArgs is setHelperEnv plus a temp file the helper
// process writes its received arguments into, so a test can assert exactly
// what argv the code under test passed through — without this package
// constructing any of it itself.
func setHelperEnvCapturingArgs(t *testing.T, cmd string) (argsFile string) {
	t.Helper()
	setHelperEnv(t, cmd)
	argsFile = filepath.Join(t.TempDir(), "args.json")
	t.Setenv("GO_HELPER_ARGS_FILE", argsFile)
	return argsFile
}

func readHelperArgs(t *testing.T, argsFile string) []string {
	t.Helper()
	data, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatalf("readHelperArgs: reading %q: %v", argsFile, err)
	}
	var args []string
	if err := json.Unmarshal(data, &args); err != nil {
		t.Fatalf("readHelperArgs: unmarshalling %q: %v", data, err)
	}
	return args
}
