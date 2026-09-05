package debuglog

// SetToolVersion configures the tool version string to write as the first
// entry in the log file (a "tool-version" header line). If v is empty, no
// version header is written. Safe to call multiple times; only the first
// non-empty value takes effect.
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
