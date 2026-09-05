package dispatchlog

// SetToolVersion configures the tool version string to write as the first
// JSONL entry (type "version") in the log file. If v is empty, no version
// entry is written. Safe to call multiple times; only the first non-empty
// value takes effect.
func (l *Logger) SetToolVersion(v string) {
	if v == "" {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.toolVersion == "" {
		l.toolVersion = v
	}
}
