package claudecode

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// ConfigHomeRelDir is where this harness's relocated user-scope
// configuration lives inside the sandbox's control directory — the same
// location SpawnPlan points ConfigHomeEnvVar at. Provisioning seeds
// credential material here so the subject can still authenticate once its
// user scope has been relocated (see spawn.go's ConfigHomeEnvVar wiring).
const ConfigHomeRelDir = "claude-home"

// CredentialSource supplies the harness credential material seeded into the
// relocated config home. Naming which files are credentials is adapter
// knowledge and lives nowhere else.
type CredentialSource interface {
	// Credentials returns the credential files to seed, or an empty slice
	// when the harness is not logged in. Not being logged in is not an
	// error here: the resulting authentication failure is diagnosed from
	// the subject's exit code and stderr, not pre-empted.
	Credentials() ([]CredentialFile, error)
}

// CredentialFile is one credential document to seed into the sandbox.
type CredentialFile struct {
	// RelPath is relative to the relocated config home. It must not escape
	// it.
	RelPath string
	Content []byte
	Mode    fs.FileMode
}

// credentialRelPath is where this harness keeps its own login credentials
// beneath its (possibly relocated) config home — the same document
// realCredentialSource reads and the same name every fixture in this
// package's tests seeds under.
const credentialRelPath = ".credentials.json"

// realCredentialSource is the real, user-home-based CredentialSource
// Options.Credentials == nil selects. It never reads a relocated config
// home (ConfigHomeEnvVar), only the real one: it exists to seed the
// sandbox's relocated copy from the developer's actual, un-relocated
// credentials, so seeding this scope back is meaningful.
type realCredentialSource struct{}

// Credentials implements CredentialSource. A missing credential file reports
// no credentials with a nil error — the harness is simply not logged in, and
// that is not this seam's failure to report; every other read error is
// returned so Provision fails loudly rather than seeding partial or
// unreadable material.
func (realCredentialSource) Credentials() ([]CredentialFile, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("claudecode: resolving user home directory: %w", err)
	}

	path := filepath.Join(home, ".claude", credentialRelPath)
	info, statErr := os.Stat(path)
	if statErr != nil {
		if os.IsNotExist(statErr) {
			return nil, nil
		}
		return nil, fmt.Errorf("claudecode: reading credentials %q: %w", path, statErr)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("claudecode: reading credentials %q: %w", path, err)
	}

	return []CredentialFile{{RelPath: credentialRelPath, Content: data, Mode: info.Mode()}}, nil
}
