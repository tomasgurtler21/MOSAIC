// Package dispatchlog (in-package tests) covers the file-backed Logger and the
// domain.NopDispatchLogger. Tests are in-package so that unexported failure-injection
// seams are accessible when the implementation is added.
//
// Every test that touches the filesystem resolves its working directory from
// t.TempDir() so that no test can create RunnerLogs/ in the repository root.
package dispatchlog

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"mosaic-run/internal/domain"
)

// ============================================================
// Helpers
// ============================================================

// readLogLines reads the dispatch log file and returns its non-empty lines.
// The caller must have called logger.Close() before readLogLines so that any
// in-flight writes are complete.
func readLogLines(t *testing.T, logger *Logger) []string {
	t.Helper()
	p := logger.Path()
	if p == "" {
		t.Fatal("readLogLines: logger.Path() is empty; no log file was created")
	}
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("readLogLines: ReadFile(%q): %v", p, err)
	}
	var lines []string
	for _, line := range strings.Split(string(data), "\n") {
		if line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

// blockDispatchLogs places a regular file where RunnerLogs/ would be created,
// causing os.MkdirAll to fail on both Windows and POSIX (including for the
// nested RunnerLogs/{run_id}/ subfolder, since MkdirAll cannot create a
// directory under a path component that is already a regular file). Returns
// the path of the blocker file.
func blockDispatchLogs(t *testing.T, workDir string) string {
	t.Helper()
	blocker := filepath.Join(workDir, LogsFolderName)
	if err := os.WriteFile(blocker, []byte("blocker"), 0644); err != nil {
		t.Fatalf("blockDispatchLogs: WriteFile(%q): %v", blocker, err)
	}
	return blocker
}

// validRunID is a canonical run_id used across tests.
const validRunID = "20260824T162217Z-b7c1"

// sampleRequest builds a ProtocolRequest with all fields populated for tests
// that verify field completeness in JSONL output.
func sampleRequest() domain.ProtocolRequest {
	return domain.ProtocolRequest{
		AgentInstanceID:      "test-agent#1",
		RunID:                validRunID,
		TaskDescription:      "do something useful",
		InputArtifacts:       []string{"Orchestration-xyz/input.md"},
		OutputArtifacts:      []string{"Orchestration-xyz/output.md"},
		InputFiles:           []string{"src/main.go"},
		OutputFiles:          []string{"src/result.go"},
		Constraints:          "no side effects",
		IncludeResultSummary: true,
		HumanInTheLoop:       false,
	}
}

// sampleResponse builds a ProtocolResponse with all fields populated for tests
// that verify field completeness in JSONL output.
func sampleResponse() domain.ProtocolResponse {
	return domain.ProtocolResponse{
		AgentInstanceID: "test-agent#1",
		RunID:           validRunID,
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "completed successfully",
		ResultData:      "some result data here",
		ErrorCode:       domain.ErrorNone,
		ErrorReason:     "",
	}
}

// unmarshalLine parses one JSONL line into a generic map for field inspection.
func unmarshalLine(t *testing.T, line string) map[string]interface{} {
	t.Helper()
	var m map[string]interface{}
	if err := json.Unmarshal([]byte(line), &m); err != nil {
		t.Fatalf("unmarshalLine: JSON parse failed for line %q: %v", line, err)
	}
	return m
}

// ============================================================
// NopDispatchLogger (domain.NopDispatchLogger)
// ============================================================

func TestNopDispatchLogger_ImplementsDispatchLogger(t *testing.T) {
	// NopDispatchLogger must satisfy the domain.DispatchLogger interface.
	// This test fails at compile time if the type does not implement the interface.
	var _ domain.DispatchLogger = domain.NopDispatchLogger{}
}

func TestNopDispatchLogger_ZeroValueIsUsable(t *testing.T) {
	// The zero value of NopDispatchLogger must be usable without explicit initialisation.
	var nop domain.NopDispatchLogger
	nop.LogRequest(sampleRequest())
}

func TestNopDispatchLogger_LogRequestDoesNotPanic(t *testing.T) {
	nop := domain.NopDispatchLogger{}
	nop.LogRequest(domain.ProtocolRequest{})
}

func TestNopDispatchLogger_LogResponseDoesNotPanic(t *testing.T) {
	nop := domain.NopDispatchLogger{}
	nop.LogResponse(domain.ProtocolResponse{})
}

func TestNopDispatchLogger_LogErrorDoesNotPanic(t *testing.T) {
	nop := domain.NopDispatchLogger{}
	nop.LogError("agent#1", "harness failed")
}

func TestNopDispatchLogger_SetRunIDDoesNotPanic(t *testing.T) {
	nop := domain.NopDispatchLogger{}
	nop.SetRunID(validRunID)
}

func TestNopDispatchLogger_CloseDoesNotPanic(t *testing.T) {
	nop := domain.NopDispatchLogger{}
	nop.Close()
}

func TestNopDispatchLogger_PathReturnsEmpty(t *testing.T) {
	nop := domain.NopDispatchLogger{}
	if p := nop.Path(); p != "" {
		t.Errorf("NopDispatchLogger.Path() = %q, want empty string", p)
	}
}

func TestNopDispatchLogger_CopyIsSafe(t *testing.T) {
	// NopDispatchLogger may be copied freely; each copy must be independently usable.
	nop1 := domain.NopDispatchLogger{}
	nop2 := nop1 // value copy
	nop1.LogRequest(sampleRequest())
	nop2.LogResponse(sampleResponse())
}

func TestNopDispatchLogger_ConcurrentCallsAreSafe(t *testing.T) {
	// NopDispatchLogger must be safe for concurrent use.
	nop := domain.NopDispatchLogger{}
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			nop.LogRequest(sampleRequest())
			nop.LogResponse(sampleResponse())
			nop.LogError("agent#1", "err")
		}()
	}
	wg.Wait()
}

// ============================================================
// Logger implements domain.DispatchLogger
// ============================================================

func TestLogger_ImplementsDispatchLogger(t *testing.T) {
	// *Logger must satisfy the domain.DispatchLogger interface.
	// This is a compile-time-only check: the nil pointer assignment fails
	// to compile if the method set is wrong, but performs no method calls
	// so the stub does not panic in RED phase.
	var _ domain.DispatchLogger = (*Logger)(nil)
}

// ============================================================
// File creation and naming
// ============================================================

func TestLogger_FileCreatedUnderRunnerLogs_WithRunID(t *testing.T) {
	// When SetRunID is called before the first log call, the log file must be
	// at RunnerLogs/{run_id}/{run_id}-dispatch.log under the supplied working
	// directory.
	workDir := t.TempDir()
	logger := New(workDir)

	logger.SetRunID(validRunID)
	logger.LogRequest(sampleRequest())
	logger.Close()

	wantPath := filepath.Join(workDir, LogsFolderName, validRunID, validRunID+"-dispatch.log")
	if _, err := os.Stat(wantPath); err != nil {
		t.Errorf("expected log file at %q, but stat failed: %v", wantPath, err)
	}
}

func TestLogger_PathReturnsAbsoluteFilePath_WithRunID(t *testing.T) {
	// Path() must return the absolute path to the log file after the first write.
	workDir := t.TempDir()
	logger := New(workDir)

	logger.SetRunID(validRunID)
	logger.LogRequest(sampleRequest())
	logger.Close()

	wantPath := filepath.Join(workDir, LogsFolderName, validRunID, validRunID+"-dispatch.log")
	if gotPath := logger.Path(); gotPath != wantPath {
		t.Errorf("Path() = %q, want %q", gotPath, wantPath)
	}
}

func TestLogger_PathReturnsEmpty_BeforeFirstLog(t *testing.T) {
	// Path() must return "" before any log call (lazy file creation contract).
	workDir := t.TempDir()
	logger := New(workDir)
	defer logger.Close()

	if p := logger.Path(); p != "" {
		t.Errorf("Path() before first log call = %q, want empty string", p)
	}
}

func TestLogger_NoFileCreated_BeforeFirstLog(t *testing.T) {
	// RunnerLogs/ must not be created until the first log call.
	workDir := t.TempDir()
	logger := New(workDir)
	defer logger.Close()

	logDir := filepath.Join(workDir, LogsFolderName)
	if _, err := os.Stat(logDir); !os.IsNotExist(err) {
		t.Errorf("RunnerLogs/ must not exist before first log call; stat(%q) err = %v", logDir, err)
	}
}

func TestLogger_LazyCreation_TriggeredByLogRequest(t *testing.T) {
	// LogRequest must trigger lazy file creation.
	workDir := t.TempDir()
	logger := New(workDir)

	logger.SetRunID(validRunID)
	logger.LogRequest(sampleRequest())
	logger.Close()

	logDir := filepath.Join(workDir, LogsFolderName)
	if _, err := os.Stat(logDir); err != nil {
		t.Errorf("RunnerLogs/ must be created after LogRequest: %v", err)
	}
}

func TestLogger_LazyCreation_TriggeredByLogResponse(t *testing.T) {
	// LogResponse must trigger lazy file creation.
	workDir := t.TempDir()
	logger := New(workDir)

	logger.SetRunID(validRunID)
	logger.LogResponse(sampleResponse())
	logger.Close()

	logDir := filepath.Join(workDir, LogsFolderName)
	if _, err := os.Stat(logDir); err != nil {
		t.Errorf("RunnerLogs/ must be created after LogResponse: %v", err)
	}
}

func TestLogger_LazyCreation_TriggeredByLogError(t *testing.T) {
	// LogError must trigger lazy file creation.
	workDir := t.TempDir()
	logger := New(workDir)

	logger.SetRunID(validRunID)
	logger.LogError("agent#1", "something went wrong")
	logger.Close()

	logDir := filepath.Join(workDir, LogsFolderName)
	if _, err := os.Stat(logDir); err != nil {
		t.Errorf("RunnerLogs/ must be created after LogError: %v", err)
	}
}

func TestLogger_FallbackFileName_StartsWithStartup_WhenNoRunID(t *testing.T) {
	// When no valid SetRunID is called before the first log call, the file name must
	// start with "startup-" so it cannot collide with a valid run_id name.
	workDir := t.TempDir()
	logger := New(workDir)

	logger.LogRequest(sampleRequest())
	logger.Close()

	logDir := filepath.Join(workDir, LogsFolderName)
	entries, err := os.ReadDir(logDir)
	if err != nil {
		t.Fatalf("ReadDir(%q): %v", logDir, err)
	}
	if len(entries) == 0 {
		t.Fatal("no log file created in RunnerLogs/ after first log call")
	}

	name := entries[0].Name()
	if !strings.HasPrefix(name, "startup-") {
		t.Errorf("fallback filename = %q, want prefix \"startup-\"", name)
	}
}

func TestLogger_FallbackFileName_HasDispatchSuffix_WhenNoRunID(t *testing.T) {
	// The fallback filename must carry the "-dispatch" suffix so it is
	// distinguishable from debuglog's own out-of-run fallback filename, which
	// uses the plain "startup-{timestamp}.log" form.
	workDir := t.TempDir()
	logger := New(workDir)

	logger.LogRequest(sampleRequest())
	logger.Close()

	logDir := filepath.Join(workDir, LogsFolderName)
	entries, err := os.ReadDir(logDir)
	if err != nil {
		t.Fatalf("ReadDir(%q): %v", logDir, err)
	}
	if len(entries) == 0 {
		t.Fatal("no log file created in RunnerLogs/ after first log call")
	}

	name := entries[0].Name()
	if !strings.HasSuffix(name, "-dispatch.log") {
		t.Errorf("fallback filename = %q, want suffix \"-dispatch.log\"", name)
	}
}

func TestLogger_FallbackFileName_IsNotAValidRunID(t *testing.T) {
	// The fallback filename base (without the -dispatch.log suffix) must not
	// satisfy domain.IsValidRunID, keeping the two naming schemes distinguishable.
	workDir := t.TempDir()
	logger := New(workDir)

	logger.LogRequest(sampleRequest())
	logger.Close()

	logDir := filepath.Join(workDir, LogsFolderName)
	entries, err := os.ReadDir(logDir)
	if err != nil {
		t.Fatalf("ReadDir(%q): %v", logDir, err)
	}
	if len(entries) == 0 {
		t.Fatal("no log file created")
	}

	baseName := strings.TrimSuffix(entries[0].Name(), "-dispatch.log")
	if domain.IsValidRunID(baseName) {
		t.Errorf("fallback filename base %q must not satisfy IsValidRunID, but it does", baseName)
	}
}

func TestLogger_SetRunID_CalledAfterFirstLog_DoesNotRenameFile(t *testing.T) {
	// When SetRunID is called after the first log call has already written,
	// the file name must remain unchanged (fallback name).
	workDir := t.TempDir()
	logger := New(workDir)

	logger.LogRequest(sampleRequest())
	pathBefore := logger.Path()
	if pathBefore == "" {
		t.Fatal("Path() empty after first log call; expected a file to be created")
	}

	logger.SetRunID(validRunID)
	pathAfter := logger.Path()

	if pathBefore != pathAfter {
		t.Errorf("Path() changed after SetRunID: before=%q, after=%q", pathBefore, pathAfter)
	}
}

func TestLogger_SetRunID_CalledAfterFirstLog_WritesCorrelationEntry(t *testing.T) {
	// When SetRunID is called after the first entry, a correlation entry
	// containing the run_id must be appended so the file can be tied to its run.
	workDir := t.TempDir()
	logger := New(workDir)

	logger.LogRequest(sampleRequest())
	logger.SetRunID(validRunID)
	logger.Close()

	lines := readLogLines(t, logger)

	foundCorrelation := false
	for _, line := range lines {
		m := unmarshalLine(t, line)
		if m["type"] == "correlation" {
			if rid, ok := m["run_id"].(string); ok && rid == validRunID {
				foundCorrelation = true
			}
		}
	}
	if !foundCorrelation {
		t.Errorf("log file must contain a correlation entry with run_id %q\nlines: %v", validRunID, lines)
	}
}

func TestLogger_SetRunID_EmptyRunID_IsIgnored(t *testing.T) {
	// An empty run_id must be silently ignored; the next log call must use the fallback name.
	workDir := t.TempDir()
	logger := New(workDir)

	logger.SetRunID("")
	logger.LogRequest(sampleRequest())
	logger.Close()

	logDir := filepath.Join(workDir, LogsFolderName)
	entries, err := os.ReadDir(logDir)
	if err != nil {
		t.Fatalf("ReadDir(%q): %v", logDir, err)
	}
	if len(entries) == 0 {
		t.Fatal("no log file created")
	}
	if !strings.HasPrefix(entries[0].Name(), "startup-") {
		t.Errorf("after empty SetRunID, file name = %q, want startup- prefix", entries[0].Name())
	}
}

func TestLogger_SetRunID_InvalidRunID_IsIgnored(t *testing.T) {
	// An invalid run_id must be silently ignored; the fallback name is used.
	workDir := t.TempDir()
	logger := New(workDir)

	logger.SetRunID("not-valid")
	logger.LogRequest(sampleRequest())
	logger.Close()

	logDir := filepath.Join(workDir, LogsFolderName)
	entries, err := os.ReadDir(logDir)
	if err != nil {
		t.Fatalf("ReadDir(%q): %v", logDir, err)
	}
	if len(entries) == 0 {
		t.Fatal("no log file created")
	}
	if !strings.HasPrefix(entries[0].Name(), "startup-") {
		t.Errorf("after invalid SetRunID, file name = %q, want startup- prefix", entries[0].Name())
	}
}

func TestLogger_SetRunID_OnlyFirstEffectiveCallNames_TheFile(t *testing.T) {
	// Only the first effective (valid) SetRunID call names the file.
	// A second valid call before the first log must be ignored.
	workDir := t.TempDir()
	logger := New(workDir)

	firstID := validRunID
	logger.SetRunID(firstID)
	logger.SetRunID("20260101T000000Z-ffff") // second call — must be ignored
	logger.LogRequest(sampleRequest())
	logger.Close()

	wantPath := filepath.Join(workDir, LogsFolderName, firstID, firstID+"-dispatch.log")
	if _, err := os.Stat(wantPath); err != nil {
		t.Errorf("expected file named after first run_id %q, stat failed: %v", wantPath, err)
	}
}

func TestLogger_RunnerLogsFolderCreatedDirectlyUnderWorkDir(t *testing.T) {
	// RunnerLogs/ must be a direct child of the supplied working directory
	// (the fallback-name case: no run subfolder is involved).
	workDir := t.TempDir()
	logger := New(workDir)

	logger.LogRequest(sampleRequest())
	logger.Close()

	logDir := filepath.Join(workDir, LogsFolderName)
	fi, err := os.Stat(logDir)
	if err != nil {
		t.Fatalf("RunnerLogs/ not created at %q: %v", logDir, err)
	}
	if !fi.IsDir() {
		t.Errorf("%q must be a directory", logDir)
	}
}

func TestLogger_NoDispatchLogsFolderEverCreated(t *testing.T) {
	// The retired DispatchLogs/ folder name must never be created by any code
	// path in this package, whether or not a run_id is known at first write.
	workDir := t.TempDir()
	logger := New(workDir)

	logger.SetRunID(validRunID)
	logger.LogRequest(sampleRequest())
	logger.Close()

	if _, err := os.Stat(filepath.Join(workDir, "DispatchLogs")); !os.IsNotExist(err) {
		t.Errorf("DispatchLogs/ must never be created; stat err = %v", err)
	}
}

func TestLogger_RunSubfolderNestsInsideRunnerLogs(t *testing.T) {
	// The run_id subfolder must nest one level inside RunnerLogs/, not sit
	// directly at the RunnerLogs/ root.
	workDir := t.TempDir()
	logger := New(workDir)

	logger.SetRunID(validRunID)
	logger.LogRequest(sampleRequest())
	logger.Close()

	runDir := filepath.Join(workDir, LogsFolderName, validRunID)
	fi, err := os.Stat(runDir)
	if err != nil {
		t.Fatalf("run subfolder not created at %q: %v", runDir, err)
	}
	if !fi.IsDir() {
		t.Errorf("%q must be a directory", runDir)
	}
}

// ============================================================
// JSONL format — request entries
// ============================================================

func TestLogger_LogRequest_WritesValidJSON(t *testing.T) {
	// LogRequest must write a line that parses as valid JSON.
	workDir := t.TempDir()
	logger := New(workDir)

	logger.SetRunID(validRunID)
	logger.LogRequest(sampleRequest())
	logger.Close()

	lines := readLogLines(t, logger)
	if len(lines) == 0 {
		t.Fatal("no JSONL lines written after LogRequest")
	}
	unmarshalLine(t, lines[0]) // fails the test if JSON is invalid
}

func TestLogger_LogRequest_TypeFieldIsRequest(t *testing.T) {
	// The "type" field in a request entry must be "request".
	workDir := t.TempDir()
	logger := New(workDir)

	logger.SetRunID(validRunID)
	logger.LogRequest(sampleRequest())
	logger.Close()

	lines := readLogLines(t, logger)
	m := unmarshalLine(t, lines[0])
	if got := m["type"]; got != "request" {
		t.Errorf("request entry type = %q, want \"request\"", got)
	}
}

func TestLogger_LogRequest_HasTimestamp(t *testing.T) {
	// The "timestamp" field must be present and non-empty in a request entry.
	workDir := t.TempDir()
	logger := New(workDir)

	logger.SetRunID(validRunID)
	logger.LogRequest(sampleRequest())
	logger.Close()

	lines := readLogLines(t, logger)
	m := unmarshalLine(t, lines[0])
	ts, ok := m["timestamp"].(string)
	if !ok || ts == "" {
		t.Errorf("request entry missing non-empty \"timestamp\" field; got %v", m["timestamp"])
	}
}

func TestLogger_LogRequest_TimestampIsUTC(t *testing.T) {
	// The "timestamp" field must end with 'Z' (UTC).
	workDir := t.TempDir()
	logger := New(workDir)

	logger.SetRunID(validRunID)
	logger.LogRequest(sampleRequest())
	logger.Close()

	lines := readLogLines(t, logger)
	m := unmarshalLine(t, lines[0])
	ts, _ := m["timestamp"].(string)
	if !strings.HasSuffix(ts, "Z") {
		t.Errorf("request entry timestamp %q must end with 'Z'", ts)
	}
}

func TestLogger_LogRequest_ContainsNestedRequestObject(t *testing.T) {
	// The request entry must contain a nested "request" object.
	workDir := t.TempDir()
	logger := New(workDir)

	logger.SetRunID(validRunID)
	logger.LogRequest(sampleRequest())
	logger.Close()

	lines := readLogLines(t, logger)
	m := unmarshalLine(t, lines[0])
	if _, ok := m["request"]; !ok {
		t.Errorf("request entry must have a nested \"request\" field; got keys: %v", mapKeys(m))
	}
}

func TestLogger_LogRequest_AgentInstanceID_IsPreserved(t *testing.T) {
	// The agent_instance_id in the request payload must be preserved verbatim.
	workDir := t.TempDir()
	logger := New(workDir)

	req := sampleRequest()
	req.AgentInstanceID = "specific-agent#42"
	logger.SetRunID(validRunID)
	logger.LogRequest(req)
	logger.Close()

	lines := readLogLines(t, logger)
	m := unmarshalLine(t, lines[0])
	reqObj, _ := m["request"].(map[string]interface{})
	if reqObj == nil {
		t.Fatal("request entry missing nested request object")
	}
	if got := reqObj["agent_instance_id"]; got != "specific-agent#42" {
		t.Errorf("request.agent_instance_id = %v, want \"specific-agent#42\"", got)
	}
}

func TestLogger_LogRequest_TaskDescription_IsPreserved(t *testing.T) {
	// The task_description field must be preserved verbatim (no truncation).
	workDir := t.TempDir()
	logger := New(workDir)

	req := sampleRequest()
	req.TaskDescription = "do something very specific and important"
	logger.SetRunID(validRunID)
	logger.LogRequest(req)
	logger.Close()

	lines := readLogLines(t, logger)
	m := unmarshalLine(t, lines[0])
	reqObj, _ := m["request"].(map[string]interface{})
	if got := reqObj["task_description"]; got != req.TaskDescription {
		t.Errorf("request.task_description = %q, want %q", got, req.TaskDescription)
	}
}

func TestLogger_LogRequest_RunID_IsPreserved(t *testing.T) {
	// The run_id in the nested request object must be preserved verbatim.
	workDir := t.TempDir()
	logger := New(workDir)

	req := sampleRequest()
	req.RunID = validRunID
	logger.SetRunID(validRunID)
	logger.LogRequest(req)
	logger.Close()

	lines := readLogLines(t, logger)
	m := unmarshalLine(t, lines[0])
	reqObj, _ := m["request"].(map[string]interface{})
	if reqObj == nil {
		t.Fatal("request entry missing nested request object")
	}
	if got := reqObj["run_id"]; got != validRunID {
		t.Errorf("request.run_id = %v, want %q", got, validRunID)
	}
}

func TestLogger_LogRequest_Constraints_IsPreserved(t *testing.T) {
	// The constraints field in the nested request object must be preserved verbatim.
	workDir := t.TempDir()
	logger := New(workDir)

	req := sampleRequest()
	req.Constraints = "no side effects"
	logger.SetRunID(validRunID)
	logger.LogRequest(req)
	logger.Close()

	lines := readLogLines(t, logger)
	m := unmarshalLine(t, lines[0])
	reqObj, _ := m["request"].(map[string]interface{})
	if reqObj == nil {
		t.Fatal("request entry missing nested request object")
	}
	if got := reqObj["constraints"]; got != "no side effects" {
		t.Errorf("request.constraints = %v, want \"no side effects\"", got)
	}
}

func TestLogger_LogRequest_LargeTaskDescription_NotTruncated(t *testing.T) {
	// Large task descriptions must be written in full — no truncation.
	workDir := t.TempDir()
	logger := New(workDir)

	var sb strings.Builder
	for i := 0; i < 500; i++ {
		sb.WriteString("This is line " + strconv.Itoa(i) + " of a very long task description.\n")
	}
	largeDesc := sb.String()

	req := sampleRequest()
	req.TaskDescription = largeDesc
	logger.SetRunID(validRunID)
	logger.LogRequest(req)
	logger.Close()

	// Read the raw file to confirm the large payload is present.
	data, err := os.ReadFile(logger.Path())
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !strings.Contains(string(data), "line 499") {
		t.Errorf("large task description appears truncated; 'line 499' not found in log")
	}
}

func TestLogger_LogRequest_AllSliceFields_Serialised(t *testing.T) {
	// InputArtifacts, OutputArtifacts, InputFiles, OutputFiles must all appear in the entry.
	workDir := t.TempDir()
	logger := New(workDir)

	req := sampleRequest()
	req.InputArtifacts = []string{"Orchestration-xyz/in.md"}
	req.OutputArtifacts = []string{"Orchestration-xyz/out.md"}
	req.InputFiles = []string{"src/a.go"}
	req.OutputFiles = []string{"src/b.go"}
	logger.SetRunID(validRunID)
	logger.LogRequest(req)
	logger.Close()

	lines := readLogLines(t, logger)
	m := unmarshalLine(t, lines[0])
	reqObj, _ := m["request"].(map[string]interface{})

	checkStringSlice(t, reqObj, "input_artifacts", "Orchestration-xyz/in.md")
	checkStringSlice(t, reqObj, "output_artifacts", "Orchestration-xyz/out.md")
	checkStringSlice(t, reqObj, "input_files", "src/a.go")
	checkStringSlice(t, reqObj, "output_files", "src/b.go")
}

func TestLogger_LogRequest_BoolFields_Serialised(t *testing.T) {
	// include_result_summary and human_in_the_loop must appear in the request entry.
	workDir := t.TempDir()
	logger := New(workDir)

	req := sampleRequest()
	req.IncludeResultSummary = true
	req.HumanInTheLoop = true
	logger.SetRunID(validRunID)
	logger.LogRequest(req)
	logger.Close()

	lines := readLogLines(t, logger)
	m := unmarshalLine(t, lines[0])
	reqObj, _ := m["request"].(map[string]interface{})

	if got := reqObj["include_result_summary"]; got != true {
		t.Errorf("request.include_result_summary = %v, want true", got)
	}
	if got := reqObj["human_in_the_loop"]; got != true {
		t.Errorf("request.human_in_the_loop = %v, want true", got)
	}
}

func TestLogger_LogRequest_EntryIsOnSingleLine(t *testing.T) {
	// Each LogRequest entry must be a single JSON line (JSONL contract).
	workDir := t.TempDir()
	logger := New(workDir)

	logger.SetRunID(validRunID)
	logger.LogRequest(sampleRequest())
	logger.Close()

	data, err := os.ReadFile(logger.Path())
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	// The file must end with exactly one newline after the JSON object.
	// No internal newlines must be present within the JSON entry.
	rawLines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(rawLines) != 1 {
		t.Errorf("LogRequest must produce exactly 1 line, got %d lines:\n%s", len(rawLines), string(data))
	}
}

// ============================================================
// JSONL format — response entries
// ============================================================

func TestLogger_LogResponse_WritesValidJSON(t *testing.T) {
	// LogResponse must write a line that parses as valid JSON.
	workDir := t.TempDir()
	logger := New(workDir)

	logger.SetRunID(validRunID)
	logger.LogResponse(sampleResponse())
	logger.Close()

	lines := readLogLines(t, logger)
	if len(lines) == 0 {
		t.Fatal("no JSONL lines written after LogResponse")
	}
	unmarshalLine(t, lines[0])
}

func TestLogger_LogResponse_TypeFieldIsResponse(t *testing.T) {
	// The "type" field in a response entry must be "response".
	workDir := t.TempDir()
	logger := New(workDir)

	logger.SetRunID(validRunID)
	logger.LogResponse(sampleResponse())
	logger.Close()

	lines := readLogLines(t, logger)
	m := unmarshalLine(t, lines[0])
	if got := m["type"]; got != "response" {
		t.Errorf("response entry type = %q, want \"response\"", got)
	}
}

func TestLogger_LogResponse_HasTimestamp(t *testing.T) {
	// The "timestamp" field must be present and non-empty in a response entry.
	workDir := t.TempDir()
	logger := New(workDir)

	logger.SetRunID(validRunID)
	logger.LogResponse(sampleResponse())
	logger.Close()

	lines := readLogLines(t, logger)
	m := unmarshalLine(t, lines[0])
	ts, ok := m["timestamp"].(string)
	if !ok || ts == "" {
		t.Errorf("response entry missing non-empty \"timestamp\" field; got %v", m["timestamp"])
	}
}

func TestLogger_LogResponse_TopLevelAgentInstanceID(t *testing.T) {
	// The response entry must carry a top-level "agent_instance_id" field for
	// easy consumer correlation without parsing the nested response object.
	workDir := t.TempDir()
	logger := New(workDir)

	resp := sampleResponse()
	resp.AgentInstanceID = "researcher#5"
	logger.SetRunID(validRunID)
	logger.LogResponse(resp)
	logger.Close()

	lines := readLogLines(t, logger)
	m := unmarshalLine(t, lines[0])
	if got := m["agent_instance_id"]; got != "researcher#5" {
		t.Errorf("response entry top-level agent_instance_id = %v, want \"researcher#5\"", got)
	}
}

func TestLogger_LogResponse_ContainsNestedResponseObject(t *testing.T) {
	// The response entry must contain a nested "response" object.
	workDir := t.TempDir()
	logger := New(workDir)

	logger.SetRunID(validRunID)
	logger.LogResponse(sampleResponse())
	logger.Close()

	lines := readLogLines(t, logger)
	m := unmarshalLine(t, lines[0])
	if _, ok := m["response"]; !ok {
		t.Errorf("response entry must have a nested \"response\" field; got keys: %v", mapKeys(m))
	}
}

func TestLogger_LogResponse_NestedStatusCode_IsPreserved(t *testing.T) {
	// The status_code in the nested response object must be preserved verbatim.
	workDir := t.TempDir()
	logger := New(workDir)

	resp := sampleResponse()
	resp.StatusCode = domain.StatusBLOCKED
	logger.SetRunID(validRunID)
	logger.LogResponse(resp)
	logger.Close()

	lines := readLogLines(t, logger)
	m := unmarshalLine(t, lines[0])
	respObj, _ := m["response"].(map[string]interface{})
	if respObj == nil {
		t.Fatal("response entry missing nested response object")
	}
	if got := respObj["status_code"]; got != string(domain.StatusBLOCKED) {
		t.Errorf("response.status_code = %v, want %q", got, domain.StatusBLOCKED)
	}
}

func TestLogger_LogResponse_NestedStatusMessage_IsPreserved(t *testing.T) {
	// The status_message in the nested response object must be preserved verbatim.
	workDir := t.TempDir()
	logger := New(workDir)

	resp := sampleResponse()
	resp.StatusMessage = "something happened with details"
	logger.SetRunID(validRunID)
	logger.LogResponse(resp)
	logger.Close()

	lines := readLogLines(t, logger)
	m := unmarshalLine(t, lines[0])
	respObj, _ := m["response"].(map[string]interface{})
	if got := respObj["status_message"]; got != resp.StatusMessage {
		t.Errorf("response.status_message = %v, want %q", got, resp.StatusMessage)
	}
}

func TestLogger_LogResponse_LargeResultData_NotTruncated(t *testing.T) {
	// Large result_data must be written in full — no truncation.
	workDir := t.TempDir()
	logger := New(workDir)

	var sb strings.Builder
	for i := 0; i < 500; i++ {
		sb.WriteString("Result line " + strconv.Itoa(i) + ": some data that must not be truncated.\n")
	}
	largeResult := sb.String()

	resp := sampleResponse()
	resp.ResultData = largeResult
	logger.SetRunID(validRunID)
	logger.LogResponse(resp)
	logger.Close()

	data, err := os.ReadFile(logger.Path())
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !strings.Contains(string(data), "Result line 499") {
		t.Errorf("large result_data appears truncated; 'Result line 499' not found in log")
	}
}

func TestLogger_LogResponse_NestedRunID_IsPreserved(t *testing.T) {
	// The run_id in the nested response object must be preserved verbatim.
	workDir := t.TempDir()
	logger := New(workDir)

	resp := sampleResponse()
	resp.RunID = validRunID
	logger.SetRunID(validRunID)
	logger.LogResponse(resp)
	logger.Close()

	lines := readLogLines(t, logger)
	m := unmarshalLine(t, lines[0])
	respObj, _ := m["response"].(map[string]interface{})
	if respObj == nil {
		t.Fatal("response entry missing nested response object")
	}
	if got := respObj["run_id"]; got != validRunID {
		t.Errorf("response.run_id = %v, want %q", got, validRunID)
	}
}

func TestLogger_LogResponse_NestedAgentInstanceID_IsPreserved(t *testing.T) {
	// The agent_instance_id in the nested response object must be preserved verbatim,
	// independently of the top-level copy verified by TestLogger_LogResponse_TopLevelAgentInstanceID.
	workDir := t.TempDir()
	logger := New(workDir)

	resp := sampleResponse()
	resp.AgentInstanceID = "nested-check-agent#3"
	logger.SetRunID(validRunID)
	logger.LogResponse(resp)
	logger.Close()

	lines := readLogLines(t, logger)
	m := unmarshalLine(t, lines[0])
	respObj, _ := m["response"].(map[string]interface{})
	if respObj == nil {
		t.Fatal("response entry missing nested response object")
	}
	if got := respObj["agent_instance_id"]; got != "nested-check-agent#3" {
		t.Errorf("response (nested).agent_instance_id = %v, want \"nested-check-agent#3\"", got)
	}
}

func TestLogger_LogResponse_NestedErrorFields_WhenBlocked(t *testing.T) {
	// When the response has StatusCode BLOCKED with non-empty error_code and
	// error_reason, both fields must appear in the nested response object.
	// This is the only scenario in which the omitempty fields are serialised.
	workDir := t.TempDir()
	logger := New(workDir)

	resp := sampleResponse()
	resp.StatusCode = domain.StatusBLOCKED
	resp.ErrorCode = domain.ErrorTOOL_UNAVAILABLE
	resp.ErrorReason = "tool unavailable"
	logger.SetRunID(validRunID)
	logger.LogResponse(resp)
	logger.Close()

	lines := readLogLines(t, logger)
	m := unmarshalLine(t, lines[0])
	respObj, _ := m["response"].(map[string]interface{})
	if respObj == nil {
		t.Fatal("response entry missing nested response object")
	}
	if got := respObj["error_code"]; got != string(domain.ErrorTOOL_UNAVAILABLE) {
		t.Errorf("response.error_code = %v, want %q", got, domain.ErrorTOOL_UNAVAILABLE)
	}
	if got := respObj["error_reason"]; got != "tool unavailable" {
		t.Errorf("response.error_reason = %v, want \"tool unavailable\"", got)
	}
}

func TestLogger_LogResponse_EntryIsOnSingleLine(t *testing.T) {
	// Each LogResponse entry must be a single JSON line (JSONL contract).
	workDir := t.TempDir()
	logger := New(workDir)

	logger.SetRunID(validRunID)
	logger.LogResponse(sampleResponse())
	logger.Close()

	data, err := os.ReadFile(logger.Path())
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	rawLines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(rawLines) != 1 {
		t.Errorf("LogResponse must produce exactly 1 line, got %d lines:\n%s", len(rawLines), string(data))
	}
}

// ============================================================
// JSONL format — error entries
// ============================================================

func TestLogger_LogError_WritesValidJSON(t *testing.T) {
	// LogError must write a line that parses as valid JSON.
	workDir := t.TempDir()
	logger := New(workDir)

	logger.SetRunID(validRunID)
	logger.LogError("agent#1", "harness timed out")
	logger.Close()

	lines := readLogLines(t, logger)
	if len(lines) == 0 {
		t.Fatal("no JSONL lines written after LogError")
	}
	unmarshalLine(t, lines[0])
}

func TestLogger_LogError_TypeFieldIsError(t *testing.T) {
	// The "type" field in an error entry must be "error".
	workDir := t.TempDir()
	logger := New(workDir)

	logger.SetRunID(validRunID)
	logger.LogError("agent#1", "harness timed out")
	logger.Close()

	lines := readLogLines(t, logger)
	m := unmarshalLine(t, lines[0])
	if got := m["type"]; got != "error" {
		t.Errorf("error entry type = %q, want \"error\"", got)
	}
}

func TestLogger_LogError_HasTimestamp(t *testing.T) {
	// The "timestamp" field must be present and non-empty in an error entry.
	workDir := t.TempDir()
	logger := New(workDir)

	logger.SetRunID(validRunID)
	logger.LogError("agent#1", "something failed")
	logger.Close()

	lines := readLogLines(t, logger)
	m := unmarshalLine(t, lines[0])
	ts, ok := m["timestamp"].(string)
	if !ok || ts == "" {
		t.Errorf("error entry missing non-empty \"timestamp\" field; got %v", m["timestamp"])
	}
}

func TestLogger_LogError_AgentInstanceID_IsPreserved(t *testing.T) {
	// The agent_instance_id in the error entry must match the argument passed to LogError.
	workDir := t.TempDir()
	logger := New(workDir)

	logger.SetRunID(validRunID)
	logger.LogError("specific-agent#7", "harness timed out")
	logger.Close()

	lines := readLogLines(t, logger)
	m := unmarshalLine(t, lines[0])
	if got := m["agent_instance_id"]; got != "specific-agent#7" {
		t.Errorf("error entry agent_instance_id = %v, want \"specific-agent#7\"", got)
	}
}

func TestLogger_LogError_ErrorText_IsPreserved(t *testing.T) {
	// The "error" field in the error entry must match the errText argument verbatim.
	workDir := t.TempDir()
	logger := New(workDir)

	errText := "exit status 1: harness process terminated unexpectedly"
	logger.SetRunID(validRunID)
	logger.LogError("agent#1", errText)
	logger.Close()

	lines := readLogLines(t, logger)
	m := unmarshalLine(t, lines[0])
	if got := m["error"]; got != errText {
		t.Errorf("error entry error = %v, want %q", got, errText)
	}
}

func TestLogger_LogError_EntryIsOnSingleLine(t *testing.T) {
	// Each LogError entry must be a single JSON line (JSONL contract).
	workDir := t.TempDir()
	logger := New(workDir)

	logger.SetRunID(validRunID)
	logger.LogError("agent#1", "some error text")
	logger.Close()

	data, err := os.ReadFile(logger.Path())
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	rawLines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(rawLines) != 1 {
		t.Errorf("LogError must produce exactly 1 line, got %d lines:\n%s", len(rawLines), string(data))
	}
}

func TestLogger_LogError_EntryIsOnSingleLine_WithNewlinesInErrText(t *testing.T) {
	// When errText contains embedded newlines, json.Marshal must escape them so
	// the entry remains a single JSONL line. An implementation that used string
	// concatenation instead of proper JSON marshaling would produce multiple lines.
	workDir := t.TempDir()
	logger := New(workDir)

	logger.SetRunID(validRunID)
	logger.LogError("agent#1", "line one\nline two\nline three")
	logger.Close()

	data, err := os.ReadFile(logger.Path())
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	rawLines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(rawLines) != 1 {
		t.Errorf("LogError with embedded newlines in errText must produce exactly 1 JSONL line, got %d lines:\n%s", len(rawLines), string(data))
	}
}

// ============================================================
// Multiple entries and ordering
// ============================================================

func TestLogger_MultipleEntries_AllPresentInFile(t *testing.T) {
	// Sequential log calls must all produce entries in the file.
	workDir := t.TempDir()
	logger := New(workDir)

	logger.SetRunID(validRunID)
	logger.LogRequest(sampleRequest())
	logger.LogResponse(sampleResponse())
	logger.LogError("agent#1", "late harness error")
	logger.Close()

	lines := readLogLines(t, logger)
	if len(lines) != 3 {
		t.Errorf("expected 3 JSONL lines, got %d\nlines: %v", len(lines), lines)
	}
}

func TestLogger_EntryTypes_InOrder(t *testing.T) {
	// When LogRequest, LogResponse, and LogError are called in sequence,
	// the resulting entries must appear in the same order in the file.
	workDir := t.TempDir()
	logger := New(workDir)

	logger.SetRunID(validRunID)
	logger.LogRequest(sampleRequest())
	logger.LogResponse(sampleResponse())
	logger.LogError("agent#1", "error")
	logger.Close()

	lines := readLogLines(t, logger)
	if len(lines) < 3 {
		t.Fatalf("expected at least 3 lines, got %d", len(lines))
	}

	types := []string{"request", "response", "error"}
	for i, wantType := range types {
		m := unmarshalLine(t, lines[i])
		if got := m["type"]; got != wantType {
			t.Errorf("line %d type = %q, want %q", i, got, wantType)
		}
	}
}

// ============================================================
// Close and Path behavior
// ============================================================

func TestLogger_Path_ReturnsSamePathAfterClose(t *testing.T) {
	// Path() must return the same path before and after Close.
	// Close disables future writes but must not clear the path.
	workDir := t.TempDir()
	logger := New(workDir)

	logger.SetRunID(validRunID)
	logger.LogRequest(sampleRequest())
	pathBeforeClose := logger.Path()

	logger.Close()
	pathAfterClose := logger.Path()

	if pathBeforeClose != pathAfterClose {
		t.Errorf("Path() changed after Close: before=%q, after=%q", pathBeforeClose, pathAfterClose)
	}
}

func TestLogger_Close_PreventsSubsequentWrites(t *testing.T) {
	// After Close, LogRequest/LogResponse/LogError must produce no new entries.
	workDir := t.TempDir()
	logger := New(workDir)

	logger.SetRunID(validRunID)
	logger.LogRequest(sampleRequest())
	logger.Close()

	// These calls must be no-ops after Close.
	logger.LogRequest(sampleRequest())
	logger.LogResponse(sampleResponse())
	logger.LogError("agent#1", "after close")

	lines := readLogLines(t, logger)
	if len(lines) != 1 {
		t.Errorf("after Close, subsequent log calls must be no-ops; got %d lines, want 1", len(lines))
	}
}

func TestLogger_Close_IsIdempotent_OnNormalLogger(t *testing.T) {
	// Close is idempotent even when the logger wrote entries normally.
	workDir := t.TempDir()
	logger := New(workDir)
	logger.SetRunID(validRunID)
	logger.LogRequest(sampleRequest())
	logger.Close()
	logger.Close() // second close must not panic
}

func TestLogger_Close_IsIdempotent_OnNeverUsedLogger(t *testing.T) {
	// Close must be safe to call multiple times on a logger that was never used.
	workDir := t.TempDir()
	logger := New(workDir)
	logger.Close()
	logger.Close() // must not panic
}

func TestLogger_FlushPerEntry_EntryPresentBeforeClose(t *testing.T) {
	// Each entry must be flushed to disk before Close is called. This ensures
	// entries survive an os.Exit that bypasses Close.
	workDir := t.TempDir()
	logger := New(workDir)

	logger.SetRunID(validRunID)
	logger.LogRequest(sampleRequest())

	// Intentionally do NOT call Close before reading.
	p := logger.Path()
	if p == "" {
		t.Fatal("Path() is empty after LogRequest; log file was not created")
	}

	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("os.ReadFile(%q) before Close: %v", p, err)
	}
	if !strings.Contains(string(data), "request") {
		t.Errorf("entry must be flushed to disk before Close is called; 'request' not found\ncontent:\n%s", string(data))
	}

	logger.Close() // cleanup
}

// ============================================================
// Fail-silent behavior
// ============================================================

func TestLogger_FailSilent_LogRequest_WhenFolderBlocked(t *testing.T) {
	// When RunnerLogs/ cannot be created, LogRequest must return without panicking.
	workDir := t.TempDir()
	blockDispatchLogs(t, workDir)

	logger := New(workDir)
	defer logger.Close()

	logger.LogRequest(sampleRequest()) // must not panic
}

func TestLogger_FailSilent_LogResponse_WhenFolderBlocked(t *testing.T) {
	// When RunnerLogs/ cannot be created, LogResponse must return without panicking.
	workDir := t.TempDir()
	blockDispatchLogs(t, workDir)

	logger := New(workDir)
	defer logger.Close()

	logger.LogResponse(sampleResponse()) // must not panic
}

func TestLogger_FailSilent_LogError_WhenFolderBlocked(t *testing.T) {
	// When RunnerLogs/ cannot be created, LogError must return without panicking.
	workDir := t.TempDir()
	blockDispatchLogs(t, workDir)

	logger := New(workDir)
	defer logger.Close()

	logger.LogError("agent#1", "error text") // must not panic
}

func TestLogger_FailSilent_PathIsEmpty_WhenFolderBlocked(t *testing.T) {
	// Path() must return "" when the logger is disabled (no file was created).
	workDir := t.TempDir()
	blockDispatchLogs(t, workDir)

	logger := New(workDir)
	logger.LogRequest(sampleRequest())
	logger.Close()

	if p := logger.Path(); p != "" {
		t.Errorf("Path() on disabled logger = %q, want empty string", p)
	}
}

func TestLogger_FailSilent_PermanentDisable_SubsequentCallsNeverPanic(t *testing.T) {
	// After a logging failure, all subsequent log calls must also not panic.
	workDir := t.TempDir()
	blockDispatchLogs(t, workDir)

	logger := New(workDir)
	defer logger.Close()

	logger.LogRequest(sampleRequest()) // trigger failure
	for i := 0; i < 5; i++ {
		logger.LogRequest(sampleRequest())
		logger.LogResponse(sampleResponse())
		logger.LogError("agent#"+strconv.Itoa(i), "repeated error")
	}
}

func TestLogger_FailSilent_CloseIsIdempotent_OnDisabledLogger(t *testing.T) {
	// Close must be safe to call multiple times on a disabled logger.
	workDir := t.TempDir()
	blockDispatchLogs(t, workDir)

	logger := New(workDir)
	logger.LogRequest(sampleRequest()) // trigger failure
	logger.Close()
	logger.Close() // must not panic
}

func TestLogger_FailSilent_SetRunID_OnDisabledLogger_NoPanic(t *testing.T) {
	// SetRunID on a disabled logger must not panic.
	workDir := t.TempDir()
	blockDispatchLogs(t, workDir)

	logger := New(workDir)
	logger.LogRequest(sampleRequest()) // trigger failure
	logger.SetRunID(validRunID)        // must not panic
	logger.Close()
}

// ============================================================
// Concurrent safety
// ============================================================

func TestLogger_ConcurrentLogRequest_AllEntriesPresent(t *testing.T) {
	// LogRequest entries written from multiple goroutines must all appear in the log.
	workDir := t.TempDir()
	logger := New(workDir)
	logger.SetRunID(validRunID)

	const goroutines = 10
	const entriesEach = 20

	var wg sync.WaitGroup
	for g := 0; g < goroutines; g++ {
		g := g
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < entriesEach; i++ {
				req := sampleRequest()
				req.TaskDescription = "goroutine-" + strconv.Itoa(g) + "-entry-" + strconv.Itoa(i)
				logger.LogRequest(req)
			}
		}()
	}
	wg.Wait()
	logger.Close()

	lines := readLogLines(t, logger)
	if len(lines) != goroutines*entriesEach {
		t.Errorf("concurrent LogRequest: got %d lines, want %d", len(lines), goroutines*entriesEach)
	}
}

func TestLogger_ConcurrentLogResponse_AllEntriesPresent(t *testing.T) {
	// LogResponse entries written from multiple goroutines must all appear in the log.
	workDir := t.TempDir()
	logger := New(workDir)
	logger.SetRunID(validRunID)

	const goroutines = 10
	const entriesEach = 20

	var wg sync.WaitGroup
	for g := 0; g < goroutines; g++ {
		g := g
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < entriesEach; i++ {
				resp := sampleResponse()
				resp.StatusMessage = "goroutine-" + strconv.Itoa(g) + "-entry-" + strconv.Itoa(i)
				logger.LogResponse(resp)
			}
		}()
	}
	wg.Wait()
	logger.Close()

	lines := readLogLines(t, logger)
	if len(lines) != goroutines*entriesEach {
		t.Errorf("concurrent LogResponse: got %d lines, want %d", len(lines), goroutines*entriesEach)
	}
}

func TestLogger_ConcurrentLogError_AllEntriesPresent(t *testing.T) {
	// LogError entries written from multiple goroutines must all appear in the log.
	workDir := t.TempDir()
	logger := New(workDir)
	logger.SetRunID(validRunID)

	const goroutines = 10
	const entriesEach = 20

	var wg sync.WaitGroup
	for g := 0; g < goroutines; g++ {
		g := g
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < entriesEach; i++ {
				errText := "goroutine-" + strconv.Itoa(g) + "-error-" + strconv.Itoa(i)
				logger.LogError("agent#"+strconv.Itoa(g), errText)
			}
		}()
	}
	wg.Wait()
	logger.Close()

	lines := readLogLines(t, logger)
	if len(lines) != goroutines*entriesEach {
		t.Errorf("concurrent LogError: got %d lines, want %d", len(lines), goroutines*entriesEach)
	}
}

func TestLogger_ConcurrentMixed_AllMethodsAreSafe(t *testing.T) {
	// Mixing LogRequest, LogResponse, and LogError from multiple goroutines
	// must not cause data races, panics, or corrupted JSON lines.
	workDir := t.TempDir()
	logger := New(workDir)
	logger.SetRunID(validRunID)

	const goroutines = 10
	const callsEach = 15

	var wg sync.WaitGroup
	for g := 0; g < goroutines; g++ {
		g := g
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < callsEach; i++ {
				switch i % 3 {
				case 0:
					logger.LogRequest(sampleRequest())
				case 1:
					logger.LogResponse(sampleResponse())
				case 2:
					logger.LogError("agent#"+strconv.Itoa(g), "concurrent error "+strconv.Itoa(i))
				}
			}
		}()
	}
	wg.Wait()
	logger.Close()

	// Verify every line is valid JSON and has a "type" field.
	lines := readLogLines(t, logger)
	for idx, line := range lines {
		m := unmarshalLine(t, line)
		if _, ok := m["type"]; !ok {
			t.Errorf("concurrent line %d missing 'type' field: %s", idx, line)
		}
	}
}

func TestLogger_ConcurrentWrites_EachLineIsValidJSON(t *testing.T) {
	// No concurrent write must produce a corrupted (partially overwritten) JSON line.
	// Every line in the file must be independently parseable.
	workDir := t.TempDir()
	logger := New(workDir)
	logger.SetRunID(validRunID)

	const goroutines = 8
	const entriesEach = 25

	var wg sync.WaitGroup
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < entriesEach; i++ {
				logger.LogRequest(sampleRequest())
			}
		}()
	}
	wg.Wait()
	logger.Close()

	lines := readLogLines(t, logger)
	for idx, line := range lines {
		if err := json.Unmarshal([]byte(line), &map[string]interface{}{}); err != nil {
			t.Errorf("line %d is invalid JSON: %v\nline content: %s", idx, err, line)
		}
	}
}

// ============================================================
// Helpers (not tests)
// ============================================================

// mapKeys returns the keys of a map for use in error messages.
func mapKeys(m map[string]interface{}) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

// checkStringSlice asserts that the given field in m is a JSON array containing
// the expected string element.
func checkStringSlice(t *testing.T, m map[string]interface{}, field, wantElem string) {
	t.Helper()
	raw, ok := m[field]
	if !ok {
		t.Errorf("field %q missing from entry; available: %v", field, mapKeys(m))
		return
	}
	arr, ok := raw.([]interface{})
	if !ok {
		t.Errorf("field %q is not an array; got %T", field, raw)
		return
	}
	for _, elem := range arr {
		if s, ok := elem.(string); ok && s == wantElem {
			return
		}
	}
	t.Errorf("field %q does not contain %q; got %v", field, wantElem, arr)
}

// ============================================================
// SetToolVersion -- version entry
// ============================================================

func TestLogger_SetToolVersion_WritesVersionEntryAsFirstLine(t *testing.T) {
	// When SetToolVersion is called before the first log call, the very first
	// JSONL line must be a version entry with type "version" and the correct
	// tool_version value.
	workDir := t.TempDir()
	logger := New(workDir)
	logger.SetToolVersion("1.0.0")
	logger.SetRunID(validRunID)
	logger.LogRequest(sampleRequest())
	logger.Close()

	lines := readLogLines(t, logger)
	if len(lines) == 0 {
		t.Fatal("log file is empty")
	}
	first := unmarshalLine(t, lines[0])
	if got, ok := first["type"].(string); !ok || got != "version" {
		t.Errorf("first line type = %q, want %q; entry: %s", got, "version", lines[0])
	}
	if got, ok := first["tool_version"].(string); !ok || got != "1.0.0" {
		t.Errorf("first line tool_version = %q, want %q; entry: %s", got, "1.0.0", lines[0])
	}
}

func TestLogger_SetToolVersion_VersionEntryHasTimestamp(t *testing.T) {
	// The version entry must include a non-empty timestamp field in RFC 3339 format.
	workDir := t.TempDir()
	logger := New(workDir)
	logger.SetToolVersion("1.0.0")
	logger.SetRunID(validRunID)
	logger.LogRequest(sampleRequest())
	logger.Close()

	lines := readLogLines(t, logger)
	if len(lines) == 0 {
		t.Fatal("log file is empty")
	}
	first := unmarshalLine(t, lines[0])
	// The first line must be the version entry; if it is not, the timestamp
	// check below is against the wrong entry and the test would be invalid.
	if typ, _ := first["type"].(string); typ != "version" {
		t.Fatalf("first line type = %q, want %q (cannot check timestamp on wrong entry type)", typ, "version")
	}
	ts, ok := first["timestamp"].(string)
	if !ok || ts == "" {
		t.Errorf("version entry must have a non-empty timestamp field; entry: %s", lines[0])
	}
}

func TestLogger_SetToolVersion_VersionEntryNotRepeated(t *testing.T) {
	// The version entry must appear exactly once regardless of how many log
	// calls follow.
	workDir := t.TempDir()
	logger := New(workDir)
	logger.SetToolVersion("1.0.0")
	logger.SetRunID(validRunID)
	logger.LogRequest(sampleRequest())
	logger.LogResponse(sampleResponse())
	logger.LogError("agent#1", "some error")
	logger.Close()

	lines := readLogLines(t, logger)
	count := 0
	for _, line := range lines {
		m := unmarshalLine(t, line)
		if m["type"] == "version" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("version entry must appear exactly once; found %d occurrences\nlines: %v", count, lines)
	}
}

func TestLogger_SetToolVersion_VersionEntryAbsentWhenNotCalled(t *testing.T) {
	// When SetToolVersion is never called, no version entry must appear in the log.
	workDir := t.TempDir()
	logger := New(workDir)
	logger.SetRunID(validRunID)
	logger.LogRequest(sampleRequest())
	logger.Close()

	lines := readLogLines(t, logger)
	for _, line := range lines {
		m := unmarshalLine(t, line)
		if m["type"] == "version" {
			t.Errorf("version entry must not appear when SetToolVersion was not called; line: %s", line)
		}
	}
}

func TestLogger_SetToolVersion_EmptyVersionNoEntry(t *testing.T) {
	// When SetToolVersion is called with an empty string, no version entry
	// must be written -- graceful degradation.
	workDir := t.TempDir()
	logger := New(workDir)
	logger.SetToolVersion("")
	logger.SetRunID(validRunID)
	logger.LogRequest(sampleRequest())
	logger.Close()

	lines := readLogLines(t, logger)
	for _, line := range lines {
		m := unmarshalLine(t, line)
		if m["type"] == "version" {
			t.Errorf("version entry must not appear when SetToolVersion called with empty string; line: %s", line)
		}
	}
}

func TestLogger_SetToolVersion_VersionEntryBeforeLogRequest(t *testing.T) {
	// The version entry must appear as the first line, before any request entry.
	workDir := t.TempDir()
	logger := New(workDir)
	logger.SetToolVersion("1.0.0")
	logger.SetRunID(validRunID)
	logger.LogRequest(sampleRequest())
	logger.Close()

	lines := readLogLines(t, logger)
	if len(lines) < 2 {
		t.Fatalf("expected at least 2 lines (version + request); got %d: %v", len(lines), lines)
	}
	first := unmarshalLine(t, lines[0])
	if first["type"] != "version" {
		t.Errorf("first line type = %q, want %q", first["type"], "version")
	}
	second := unmarshalLine(t, lines[1])
	if second["type"] != "request" {
		t.Errorf("second line type = %q, want %q (first request should follow version entry)", second["type"], "request")
	}
}

func TestLogger_SetToolVersion_VersionEntryFirstWhenSetRunIDCalledAfterLog(t *testing.T) {
	// When SetRunID is called after the first log (which appends a correlation
	// entry), the version entry must still be the first line in the file.
	workDir := t.TempDir()
	logger := New(workDir)
	logger.SetToolVersion("1.0.0")
	logger.LogRequest(sampleRequest())
	logger.SetRunID(validRunID)
	logger.Close()

	lines := readLogLines(t, logger)
	if len(lines) == 0 {
		t.Fatal("log file is empty")
	}
	first := unmarshalLine(t, lines[0])
	if got, ok := first["type"].(string); !ok || got != "version" {
		t.Errorf("first line type = %q, want %q (version entry must be first even when SetRunID called post-log)", got, "version")
	}
}

func TestLogger_SetToolVersion_OnlyFirstNonEmptyValueTakesEffect(t *testing.T) {
	// If SetToolVersion is called multiple times, only the first non-empty value
	// must appear in the version entry.
	workDir := t.TempDir()
	logger := New(workDir)
	logger.SetToolVersion("1.0.0")
	logger.SetToolVersion("9.9.9") // second call must be ignored
	logger.SetRunID(validRunID)
	logger.LogRequest(sampleRequest())
	logger.Close()

	lines := readLogLines(t, logger)
	if len(lines) == 0 {
		t.Fatal("log file is empty")
	}
	first := unmarshalLine(t, lines[0])
	if got, _ := first["tool_version"].(string); got != "1.0.0" {
		t.Errorf("tool_version = %q, want %q (second SetToolVersion call must be ignored)", got, "1.0.0")
	}
}
