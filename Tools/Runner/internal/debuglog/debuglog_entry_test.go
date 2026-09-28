package debuglog

import (
	"os"
	"strconv"
	"strings"
	"testing"

	"mosaic-run/internal/domain"
)

// ============================================================
// Entry writing (T4.3)
// ============================================================

func TestLogger_SingleLineEntry_HasTimestampBracket(t *testing.T) {
	// Each entry must begin with a '[' opening the timestamp bracket.
	workDir := t.TempDir()
	logger := New(workDir)

	logger.SetRunID(validRunID)
	logger.Log(domain.EventRunnerStart, "message")
	logger.Close()

	content := readLogFile(t, logger)
	firstLine := strings.SplitN(content, "\n", 2)[0]
	if !strings.HasPrefix(firstLine, "[") {
		t.Errorf("entry must start with '[', got: %q", firstLine)
	}
}

func TestLogger_SingleLineEntry_HasUTCTimestamp(t *testing.T) {
	// The timestamp must be UTC (ends with 'Z' inside the bracket).
	workDir := t.TempDir()
	logger := New(workDir)

	logger.SetRunID(validRunID)
	logger.Log(domain.EventRunnerStart, "message")
	logger.Close()

	content := readLogFile(t, logger)
	if !strings.Contains(content, "Z]") {
		t.Errorf("entry must contain UTC timestamp ending 'Z]'\ncontent: %s", content)
	}
}

func TestLogger_SingleLineEntry_ContainsEventName(t *testing.T) {
	// The entry must contain the event name.
	workDir := t.TempDir()
	logger := New(workDir)

	logger.SetRunID(validRunID)
	logger.Log(domain.EventHarnessInvokeStart, "dispatching")
	logger.Close()

	content := readLogFile(t, logger)
	if !strings.Contains(content, domain.EventHarnessInvokeStart) {
		t.Errorf("entry must contain event name %q\ncontent: %s", domain.EventHarnessInvokeStart, content)
	}
}

func TestLogger_SingleLineEntry_ContainsField(t *testing.T) {
	// A single-line entry must render each field as key=value.
	workDir := t.TempDir()
	logger := New(workDir)

	logger.SetRunID(validRunID)
	logger.Log(domain.EventHarnessInvokeStart, "dispatching",
		domain.F("agent", "researcher#1"),
		domain.F("kind", "ordinary"),
	)
	logger.Close()

	content := readLogFile(t, logger)
	if !strings.Contains(content, "agent=researcher#1") {
		t.Errorf("entry must contain field 'agent=researcher#1'\ncontent: %s", content)
	}
	if !strings.Contains(content, "kind=ordinary") {
		t.Errorf("entry must contain field 'kind=ordinary'\ncontent: %s", content)
	}
}

func TestLogger_SingleLineEntry_ContainsPipeSeparatorAndMessage(t *testing.T) {
	// The message must follow a '|' separator.
	workDir := t.TempDir()
	logger := New(workDir)

	logger.SetRunID(validRunID)
	logger.Log(domain.EventRunnerStart, "hello world")
	logger.Close()

	content := readLogFile(t, logger)
	if !strings.Contains(content, "| hello world") {
		t.Errorf("entry must contain '| hello world'\ncontent: %s", content)
	}
}

func TestLogger_MultiLineEntry_UsesBeginEndDelimiters(t *testing.T) {
	// A multi-line message must use begin/end block delimiters so the payload
	// is unambiguously attributable to its entry.
	workDir := t.TempDir()
	logger := New(workDir)

	logger.SetRunID(validRunID)
	logger.Log(domain.EventHarnessStdout, "line1\nline2\nline3")
	logger.Close()

	content := readLogFile(t, logger)

	beginMark := "--- begin " + domain.EventHarnessStdout + " ---"
	endMark := "--- end " + domain.EventHarnessStdout + " ---"

	if !strings.Contains(content, beginMark) {
		t.Errorf("multi-line entry must contain %q\ncontent:\n%s", beginMark, content)
	}
	if !strings.Contains(content, endMark) {
		t.Errorf("multi-line entry must contain %q\ncontent:\n%s", endMark, content)
	}
}

func TestLogger_MultiLineEntry_PayloadWrittenVerbatim(t *testing.T) {
	// The multi-line payload must appear verbatim inside the begin/end block.
	workDir := t.TempDir()
	logger := New(workDir)

	payload := "first line\nsecond line\n{\"type\":\"result\"}\n"
	logger.SetRunID(validRunID)
	logger.Log(domain.EventHarnessStdout, payload)
	logger.Close()

	content := readLogFile(t, logger)
	if !strings.Contains(content, payload) {
		t.Errorf("payload must appear verbatim in the log file\ncontent:\n%s", content)
	}
}

func TestLogger_MultiLineEntry_LargePayloadNotTruncated(t *testing.T) {
	// Multi-line payloads of any size must be written in full — no truncation.
	workDir := t.TempDir()
	logger := New(workDir)

	var sb strings.Builder
	const lineCount = 200
	for i := 0; i < lineCount; i++ {
		sb.WriteString("payload line " + strconv.Itoa(i) + " — should appear verbatim in the debug log\n")
	}
	largePayload := sb.String()

	logger.SetRunID(validRunID)
	logger.Log(domain.EventHarnessStdout, largePayload, domain.F("bytes", strconv.Itoa(len(largePayload))))
	logger.Close()

	content := readLogFile(t, logger)
	if !strings.Contains(content, largePayload) {
		t.Errorf("large payload (len=%d) must appear verbatim; log appears truncated", len(largePayload))
	}
}

func TestLogger_MultipleEntries_AllPresentInFile(t *testing.T) {
	// Sequential Log calls must all produce entries in the file.
	workDir := t.TempDir()
	logger := New(workDir)

	logger.SetRunID(validRunID)
	messages := []string{"first entry", "second entry", "third entry"}
	for _, msg := range messages {
		logger.Log(domain.EventRunnerStart, msg)
	}
	logger.Close()

	content := readLogFile(t, logger)
	for _, want := range messages {
		if !strings.Contains(content, want) {
			t.Errorf("log must contain %q\ncontent:\n%s", want, content)
		}
	}
}

func TestLogger_FieldsInMultiLineEntry_AppearsInHeader(t *testing.T) {
	// Fields must appear on the header line of a multi-line entry (before the begin block).
	workDir := t.TempDir()
	logger := New(workDir)

	logger.SetRunID(validRunID)
	logger.Log(domain.EventHarnessStdout, "line1\nline2", domain.F("bytes", "10"))
	logger.Close()

	content := readLogFile(t, logger)
	lines := strings.Split(content, "\n")

	// Find the header line (starts with '[' and contains the event name).
	found := false
	for _, line := range lines {
		if strings.Contains(line, domain.EventHarnessStdout) && strings.HasPrefix(line, "[") {
			if strings.Contains(line, "bytes=10") {
				found = true
			}
			break
		}
	}
	if !found {
		t.Errorf("field 'bytes=10' must appear on the multi-line entry header line\ncontent:\n%s", content)
	}
}

func TestLogger_FieldValue_WithSpace_IsQuoted(t *testing.T) {
	// A field value that contains a space must be rendered with %q quoting so
	// it cannot be mistaken for the boundary between two fields or the message
	// separator. Unquoted values with spaces would make the wire format ambiguous.
	workDir := t.TempDir()
	logger := New(workDir)

	logger.SetRunID(validRunID)
	logger.Log(domain.EventHarnessInvokeStart, "dispatching",
		domain.F("path", "C:/a b/file.md"),
	)
	logger.Close()

	content := readLogFile(t, logger)
	// The value must appear with surrounding double-quote characters (Go %q output).
	if !strings.Contains(content, `path="C:/a b/file.md"`) {
		t.Errorf("field value with space must be quoted; want path=\"C:/a b/file.md\" in log\ncontent:\n%s", content)
	}
}

func TestLogger_FieldValue_WithPipe_IsQuoted(t *testing.T) {
	// A field value that contains '|' must be rendered with %q quoting so it
	// cannot be mistaken for the '|' separator that divides the header from the
	// message body. An unquoted '|' in a field value would corrupt the wire format.
	workDir := t.TempDir()
	logger := New(workDir)

	logger.SetRunID(validRunID)
	logger.Log(domain.EventHarnessInvokeStart, "dispatching",
		domain.F("tag", "foo|bar"),
	)
	logger.Close()

	content := readLogFile(t, logger)
	// The value must appear with surrounding double-quote characters (Go %q output).
	if !strings.Contains(content, `tag="foo|bar"`) {
		t.Errorf("field value with '|' must be quoted; want tag=\"foo|bar\" in log\ncontent:\n%s", content)
	}
}

func TestLogger_FlushPerEntry_EntryPresentBeforeClose(t *testing.T) {
	// Each entry must be flushed to the operating system immediately after Log
	// returns, before Close is called. This is the crash-survival guarantee: if
	// the process is killed by os.Exit (bypassing Close), the entry must already
	// be on disk.
	//
	// The file is read via an independent os.ReadFile — not through the Logger —
	// so we confirm durable flush rather than an in-memory buffer that Close would flush.
	workDir := t.TempDir()
	logger := New(workDir)

	logger.SetRunID(validRunID)
	const marker = "flush-survival-marker"
	logger.Log(domain.EventRunnerStart, marker)

	// Intentionally do NOT call Close before reading.
	p := logger.Path()
	if p == "" {
		t.Fatal("Path() is empty after Log; log file was not created")
	}

	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("os.ReadFile(%q) before Close: %v", p, err)
	}
	if !strings.Contains(string(data), marker) {
		t.Errorf("entry must be flushed to disk before Close is called; marker %q not found\ncontent:\n%s",
			marker, string(data))
	}

	logger.Close() // cleanup
}
