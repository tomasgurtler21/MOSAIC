package debuglog

import (
	"strconv"
	"strings"
	"testing"

	"mosaic-run/internal/domain"
)

// ============================================================
// SetToolVersion -- version header
// ============================================================

func TestLogger_SetToolVersion_WritesVersionHeaderAsFirstLine(t *testing.T) {
	// When SetToolVersion is called before the first Log, the very first line
	// of the log file must be a version-header entry containing both the
	// "tool-version" event name and the supplied version string.
	workDir := t.TempDir()
	logger := New(workDir)
	logger.SetToolVersion("1.0.0")
	logger.SetRunID(validRunID)
	logger.Log(domain.EventRunnerStart, "first real entry")
	logger.Close()

	content := readLogFile(t, logger)
	firstLine := strings.SplitN(content, "\n", 2)[0]
	if !strings.Contains(firstLine, "tool-version") {
		t.Errorf("first line must contain %q; got: %q", "tool-version", firstLine)
	}
	if !strings.Contains(firstLine, "1.0.0") {
		t.Errorf("first line must contain version string %q; got: %q", "1.0.0", firstLine)
	}
}

func TestLogger_SetToolVersion_VersionHeaderNotRepeated(t *testing.T) {
	// The version header must appear exactly once in the log file regardless of
	// how many Log calls follow.
	workDir := t.TempDir()
	logger := New(workDir)
	logger.SetToolVersion("1.0.0")
	logger.SetRunID(validRunID)
	for i := 0; i < 5; i++ {
		logger.Log(domain.EventRunnerStart, "entry "+strconv.Itoa(i))
	}
	logger.Close()

	content := readLogFile(t, logger)
	count := strings.Count(content, "tool-version")
	if count != 1 {
		t.Errorf("version header must appear exactly once; found %d occurrences\ncontent:\n%s", count, content)
	}
}

func TestLogger_SetToolVersion_VersionHeaderSurvivesReplay(t *testing.T) {
	// When SetRunID is called after the first Log (triggering a replay into the
	// run folder), the replayed log file must still start with the version header
	// as its very first line.
	workDir := t.TempDir()
	logger := New(workDir)
	logger.SetToolVersion("1.0.0")
	logger.Log(domain.EventRunnerStart, "pre-identity entry")
	logger.SetRunID(validRunID)
	logger.Close()

	content := readLogFile(t, logger)
	firstLine := strings.SplitN(content, "\n", 2)[0]
	if !strings.Contains(firstLine, "tool-version") {
		t.Errorf("replayed file: first line must contain %q; got: %q", "tool-version", firstLine)
	}
	if !strings.Contains(firstLine, "1.0.0") {
		t.Errorf("replayed file: first line must contain version string %q; got: %q", "1.0.0", firstLine)
	}
}

func TestLogger_SetToolVersion_ReplayedFileContainsOriginalEntriesAfterHeader(t *testing.T) {
	// After replay, the pre-identity entries must follow the version header in
	// the run-folder file.
	workDir := t.TempDir()
	logger := New(workDir)
	logger.SetToolVersion("1.0.0")
	logger.Log(domain.EventRunnerStart, "pre-identity entry")
	logger.SetRunID(validRunID)
	logger.Close()

	content := readLogFile(t, logger)
	if !strings.Contains(content, "pre-identity entry") {
		t.Errorf("replayed file must still contain original entries\ncontent:\n%s", content)
	}
}

func TestLogger_SetToolVersion_VersionHeaderAbsentWhenNotCalled(t *testing.T) {
	// When SetToolVersion is never called, no version header must appear in the log.
	workDir := t.TempDir()
	logger := New(workDir)
	logger.SetRunID(validRunID)
	logger.Log(domain.EventRunnerStart, "normal entry")
	logger.Close()

	content := readLogFile(t, logger)
	if strings.Contains(content, "tool-version") {
		t.Errorf("version header must not appear when SetToolVersion was not called\ncontent:\n%s", content)
	}
}

func TestLogger_SetToolVersion_EmptyVersionNoHeader(t *testing.T) {
	// When SetToolVersion is called with an empty string, no version header
	// must be written -- graceful degradation.
	workDir := t.TempDir()
	logger := New(workDir)
	logger.SetToolVersion("")
	logger.SetRunID(validRunID)
	logger.Log(domain.EventRunnerStart, "entry after empty SetToolVersion")
	logger.Close()

	content := readLogFile(t, logger)
	if strings.Contains(content, "tool-version") {
		t.Errorf("version header must not appear when SetToolVersion was called with empty string\ncontent:\n%s", content)
	}
}

func TestLogger_SetToolVersion_VersionHeaderBeforeFirstRealEntry(t *testing.T) {
	// The version header must appear before the first real log entry in the file.
	workDir := t.TempDir()
	logger := New(workDir)
	logger.SetToolVersion("2.3.4")
	logger.SetRunID(validRunID)
	logger.Log(domain.EventRunnerStart, "real entry marker")
	logger.Close()

	content := readLogFile(t, logger)
	versionIdx := strings.Index(content, "tool-version")
	realEntryIdx := strings.Index(content, "real entry marker")
	if versionIdx == -1 {
		t.Fatal("version header not found in log file")
	}
	if realEntryIdx == -1 {
		t.Fatal("real entry not found in log file")
	}
	if versionIdx > realEntryIdx {
		t.Errorf("version header (byte offset %d) must appear before real entry (byte offset %d)\ncontent:\n%s",
			versionIdx, realEntryIdx, content)
	}
}

func TestLogger_SetToolVersion_OnlyFirstNonEmptyValueTakesEffect(t *testing.T) {
	// If SetToolVersion is called multiple times with different non-empty values,
	// only the first non-empty value must appear in the log.
	workDir := t.TempDir()
	logger := New(workDir)
	logger.SetToolVersion("1.0.0")
	logger.SetToolVersion("9.9.9") // second call must be ignored
	logger.SetRunID(validRunID)
	logger.Log(domain.EventRunnerStart, "entry")
	logger.Close()

	content := readLogFile(t, logger)
	if !strings.Contains(content, "1.0.0") {
		t.Errorf("first version %q must appear in the log\ncontent:\n%s", "1.0.0", content)
	}
	if strings.Contains(content, "9.9.9") {
		t.Errorf("second version %q must NOT appear in the log\ncontent:\n%s", "9.9.9", content)
	}
}
