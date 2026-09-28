package dispatchlog

import (
	"encoding/json"
	"strconv"
	"sync"
	"testing"
)

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
