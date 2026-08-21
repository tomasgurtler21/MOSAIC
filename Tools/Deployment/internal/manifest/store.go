package manifest

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/goccy/go-yaml"
	"mosaic-deploy/internal/domain"
)

// State describes the result of reading the manifest file.
// Each value is distinct so callers can tell "file missing" from "file empty" from
// "file corrupt" from "file written by a newer tool" (CD-8, AC14.2).
type State string

const (
	// StatePresent means the manifest file was found, read, and parsed without error.
	StatePresent State = "present"
	// StateAbsent means no manifest file exists at the expected path.
	StateAbsent State = "absent"
	// StateEmpty means the file exists but contains no content (zero bytes or whitespace only).
	StateEmpty State = "empty"
	// StateCorrupt means the file exists but cannot be parsed as a valid manifest.
	StateCorrupt State = "corrupt"
	// StateFuture means the manifest was written by a newer schema version that this build
	// does not know how to interpret.
	StateFuture State = "future-schema"
)

// Snapshot is the result of Store.Load. The five state values are always distinct;
// a missing or unreadable manifest is never silently returned as an empty record set
// (CD-8, AC14.2).
type Snapshot struct {
	// State is one of the five State constants and is always set.
	State State
	// Manifest is populated only when State == StatePresent.
	// For all other states it is the zero value of domain.Manifest.
	Manifest domain.Manifest
	// Path is the absolute path of the manifest file (or where it would be created if absent).
	Path string
	// Err carries the underlying error when State is StateCorrupt or StateFuture.
	// It is nil for all other states.
	Err error
}

// Store reads and writes the deployment manifest stored at
// <workspaceRoot>/.mosaic/manifest.yaml.
type Store interface {
	// Load reads the manifest for the given workspace root. It always returns a Snapshot;
	// only unexpected I/O errors (e.g. permission denied on an existing file) result in a
	// returned error. Missing, empty, corrupt, and future-schema manifests all produce a
	// Snapshot with a distinct State and a nil error.
	Load(workspaceRoot string) (Snapshot, error)

	// Save writes m to <workspaceRoot>/.mosaic/manifest.yaml, creating the .mosaic
	// directory if it does not exist.
	Save(workspaceRoot string, m domain.Manifest) error
}

// SchemaVersion is the manifest schema version written by this build into every manifest file.
const SchemaVersion = "1"

// Dir, FileName, and BackupDir are the fixed relative locations of the manifest and its
// backup folder within a workspace root.
const (
	Dir       = ".mosaic"
	FileName  = "manifest.yaml"
	BackupDir = "backups"
)

// ---------------------------------------------------------------------------
// YAML wire types
// ---------------------------------------------------------------------------

// yamlManifest is the YAML-serializable form of domain.Manifest. Timestamps are
// stored as RFC3339 strings for portability and human-readability.
type yamlManifest struct {
	SchemaVersion string              `yaml:"schema_version"`
	HarnessID     string              `yaml:"harness_id"`
	UpdatedAt     string              `yaml:"updated_at"`
	Entries       []yamlManifestEntry `yaml:"entries"`
}

type yamlManifestEntry struct {
	Ref                           yamlArtifactRef `yaml:"ref"`
	TargetPath                    string          `yaml:"target_path"`
	Version                       string          `yaml:"version"`
	HarnessVersion                string          `yaml:"harness_version"`
	InjectionsVersion             string          `yaml:"injections_version"`
	OrchestratorInjectionsVersion string          `yaml:"orchestrator_injections_version,omitempty"`
	ContentHash                   string          `yaml:"content_hash"`
	DeployedAt                    string          `yaml:"deployed_at"`
}

type yamlArtifactRef struct {
	Kind string `yaml:"kind"`
	Key  string `yaml:"key"`
}

// ---------------------------------------------------------------------------
// store implementation
// ---------------------------------------------------------------------------

type store struct{}

// NewStore returns the production Store implementation.
func NewStore() Store {
	return &store{}
}

func (s *store) manifestPath(workspaceRoot string) string {
	return filepath.Join(workspaceRoot, Dir, FileName)
}

// Load reads the manifest for the given workspace root. Abnormal manifest states
// (absent, empty, corrupt, future-schema) are communicated via Snapshot.State and
// never result in a returned error. Only genuine I/O failures (e.g. permission denied
// on an existing file) return a non-nil error.
func (s *store) Load(workspaceRoot string) (Snapshot, error) {
	path := s.manifestPath(workspaceRoot)
	snap := Snapshot{Path: path}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			snap.State = StateAbsent
			return snap, nil
		}
		// Unexpected I/O error — propagate as a real error.
		return Snapshot{}, err
	}

	if strings.TrimSpace(string(data)) == "" {
		snap.State = StateEmpty
		return snap, nil
	}

	var raw yamlManifest
	if err := yaml.Unmarshal(data, &raw); err != nil {
		snap.State = StateCorrupt
		snap.Err = fmt.Errorf("parse error: %w", err)
		return snap, nil
	}

	if raw.SchemaVersion != SchemaVersion {
		snap.State = StateFuture
		snap.Err = fmt.Errorf("manifest schema_version %q is not recognised by this build (supports %q)", raw.SchemaVersion, SchemaVersion)
		return snap, nil
	}

	updatedAt, err := time.Parse(time.RFC3339, raw.UpdatedAt)
	if err != nil {
		snap.State = StateCorrupt
		snap.Err = fmt.Errorf("invalid updated_at %q: %w", raw.UpdatedAt, err)
		return snap, nil
	}

	m := domain.Manifest{
		SchemaVersion: raw.SchemaVersion,
		HarnessID:     raw.HarnessID,
		UpdatedAt:     updatedAt,
	}

	for _, e := range raw.Entries {
		deployedAt, err := time.Parse(time.RFC3339, e.DeployedAt)
		if err != nil {
			snap.State = StateCorrupt
			snap.Err = fmt.Errorf("entry %q: invalid deployed_at %q: %w", e.TargetPath, e.DeployedAt, err)
			return snap, nil
		}
		m.Entries = append(m.Entries, domain.ManifestEntry{
			Ref:                           domain.ArtifactRef{Kind: domain.ArtifactKind(e.Ref.Kind), Key: e.Ref.Key},
			TargetPath:                    e.TargetPath,
			Version:                       e.Version,
			HarnessVersion:                e.HarnessVersion,
			InjectionsVersion:             e.InjectionsVersion,
			OrchestratorInjectionsVersion: e.OrchestratorInjectionsVersion,
			ContentHash:                   e.ContentHash,
			DeployedAt:                    deployedAt,
		})
	}

	// Enforce sorted-by-TargetPath invariant regardless of the on-disk order.
	sort.Slice(m.Entries, func(i, j int) bool {
		return m.Entries[i].TargetPath < m.Entries[j].TargetPath
	})

	snap.State = StatePresent
	snap.Manifest = m
	return snap, nil
}

// Save writes m to <workspaceRoot>/.mosaic/manifest.yaml, creating the .mosaic
// directory if it does not exist. A second Save call on the same workspace replaces
// the previous manifest file entirely.
func (s *store) Save(workspaceRoot string, m domain.Manifest) error {
	dir := filepath.Join(workspaceRoot, Dir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create manifest directory: %w", err)
	}

	raw := yamlManifest{
		SchemaVersion: m.SchemaVersion,
		HarnessID:     m.HarnessID,
		UpdatedAt:     m.UpdatedAt.UTC().Format(time.RFC3339),
	}

	for _, e := range m.Entries {
		raw.Entries = append(raw.Entries, yamlManifestEntry{
			Ref:                           yamlArtifactRef{Kind: string(e.Ref.Kind), Key: e.Ref.Key},
			TargetPath:                    e.TargetPath,
			Version:                       e.Version,
			HarnessVersion:                e.HarnessVersion,
			InjectionsVersion:             e.InjectionsVersion,
			OrchestratorInjectionsVersion: e.OrchestratorInjectionsVersion,
			ContentHash:                   e.ContentHash,
			DeployedAt:                    e.DeployedAt.UTC().Format(time.RFC3339),
		})
	}

	data, err := yaml.Marshal(&raw)
	if err != nil {
		return fmt.Errorf("marshal manifest: %w", err)
	}

	path := filepath.Join(dir, FileName)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write manifest: %w", err)
	}
	return nil
}
