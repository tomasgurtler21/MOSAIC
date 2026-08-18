package logging

import (
	"fmt"
	"os"
	"path/filepath"
)

// writeToFile writes content to path, either truncating or appending depending on the
// truncate flag. The directory is created if it does not exist. An error is returned
// if directory creation or file open/write fails.
func writeToFile(path, content string, truncate bool) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create log directory %q: %w", dir, err)
	}

	flags := os.O_CREATE | os.O_WRONLY
	if truncate {
		flags |= os.O_TRUNC
	} else {
		flags |= os.O_APPEND
	}

	f, err := os.OpenFile(path, flags, 0o644)
	if err != nil {
		return fmt.Errorf("open log file %q: %w", path, err)
	}
	defer f.Close()

	if _, err := fmt.Fprint(f, content); err != nil {
		return fmt.Errorf("write log file %q: %w", path, err)
	}
	return nil
}
