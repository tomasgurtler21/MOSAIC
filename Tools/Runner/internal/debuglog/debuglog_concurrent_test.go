package debuglog

import (
	"strconv"
	"strings"
	"sync"
	"testing"

	"mosaic-run/internal/domain"
)

// ============================================================
// Concurrency (T4.6)
// ============================================================

func TestLogger_ConcurrentWrites_AllEntriesPresent(t *testing.T) {
	// Entries written from multiple goroutines must all appear in the log file.
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
				msg := "goroutine-" + strconv.Itoa(g) + "-entry-" + strconv.Itoa(i)
				logger.Log(domain.EventSessionDispatchStart, msg)
			}
		}()
	}
	wg.Wait()
	logger.Close()

	content := readLogFile(t, logger)

	// Every message must appear exactly once.
	for g := 0; g < goroutines; g++ {
		for i := 0; i < entriesEach; i++ {
			want := "goroutine-" + strconv.Itoa(g) + "-entry-" + strconv.Itoa(i)
			if !strings.Contains(content, want) {
				t.Errorf("concurrent log: missing entry %q", want)
			}
		}
	}
}

func TestLogger_ConcurrentWrites_EntriesNotInterleaved(t *testing.T) {
	// Multi-line entries written concurrently must not be interleaved mid-entry.
	// Every begin-block must be followed by its matching end-block before any
	// other entry's header appears.
	workDir := t.TempDir()
	logger := New(workDir)
	logger.SetRunID(validRunID)

	const goroutines = 5
	const entriesEach = 15

	var wg sync.WaitGroup
	for g := 0; g < goroutines; g++ {
		g := g
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < entriesEach; i++ {
				payload := "goroutine=" + strconv.Itoa(g) + "\nentry=" + strconv.Itoa(i) + "\npayload-end"
				logger.Log(domain.EventHarnessStdout, payload, domain.F("bytes", strconv.Itoa(len(payload))))
			}
		}()
	}
	wg.Wait()
	logger.Close()

	content := readLogFile(t, logger)
	assertNoInterleavedBlocks(t, content)
}

func TestLogger_ConcurrentWrites_EntryCountMatchesExpected(t *testing.T) {
	// The total number of written entries must equal goroutines × entriesEach.
	// This guards against dropped writes under concurrency.
	workDir := t.TempDir()
	logger := New(workDir)
	logger.SetRunID(validRunID)

	const goroutines = 8
	const entriesEach = 10
	const total = goroutines * entriesEach

	var wg sync.WaitGroup
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < entriesEach; i++ {
				logger.Log(domain.EventSessionStepDone, "concurrent single-line entry")
			}
		}()
	}
	wg.Wait()
	logger.Close()

	content := readLogFile(t, logger)
	// Count occurrences of the event name in the file — one per entry.
	count := strings.Count(content, domain.EventSessionStepDone)
	if count != total {
		t.Errorf("concurrent writes: got %d entries, want %d", count, total)
	}
}
