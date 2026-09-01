package main

import (
	"os"
	"path/filepath"

	"mosaic-agent-test/internal/resultstore"
)

// osFileSystem is the real FileSystem implementation used in production. It
// delegates every method to the corresponding os or filepath function. A zero
// value is ready to use; no construction is needed.
//
// osFileSystem satisfies both resultstore.FileSystem and
// resultsummary.FileSystem (both interfaces share identical method signatures,
// with Stat returning resultstore.FileInfo), so the composition root supplies
// one instance to both collaborator closures.
type osFileSystem struct{}

func (osFileSystem) ReadFile(path string) ([]byte, error) {
	return os.ReadFile(path)
}

func (osFileSystem) WriteFile(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func (osFileSystem) Stat(path string) (resultstore.FileInfo, error) {
	info, err := os.Stat(path)
	if err != nil {
		return resultstore.FileInfo{}, err
	}
	return resultstore.FileInfo{
		Name:  info.Name(),
		IsDir: info.IsDir(),
	}, nil
}

func (osFileSystem) MkdirAll(path string) error {
	return os.MkdirAll(path, 0o755)
}

func (osFileSystem) ListDir(path string) ([]string, error) {
	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names, nil
}
