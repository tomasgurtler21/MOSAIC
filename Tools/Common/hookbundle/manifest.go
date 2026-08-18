// Package hookbundle owns the hook.yaml manifest model — the schema, its
// variants, file lists and registration steps — and the "which variant does
// harness X consume" resolution rule. It is shared between the deployment
// tool and the agent test tool.
//
// This package owns the manifest's meaning and nothing else: content-hash
// validation, catalog directory scanning, hook directory paths, fragment
// merging, conflict handling and non-performable-step checklists all stay in
// the deployment tool.
package hookbundle

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/goccy/go-yaml"
)

// Manifest is the decoded hook.yaml model.
type Manifest struct {
	SchemaVersion string
	ID            string
	Version       string
	Description   string
	Placeholder   bool   // legitimately absent in real bundles; zero value is correct
	ContentHash   string // legitimately absent; empty means "not declared"
	Variants      map[string]Variant
}

// Variant is one harness's view of the bundle.
type Variant struct {
	Key          string
	Supported    bool
	Reuses       string // another variant key this one defers to
	Files        []File
	Registration []RegistrationStep
}

// File is one source/target pair to deploy for a variant.
type File struct {
	// Source is relative to the variant directory and may escape it
	// (e.g. "../hook.yaml"); that form appears in real bundles and is valid.
	Source string
	Target string
}

// RegistrationStep is one step required to register a variant's hooks with
// its harness.
type RegistrationStep struct {
	ID          string
	TargetPath  string
	Performable bool
	Instruction string
	// Fragment is the registration payload exactly as authored — an opaque
	// string, never round-tripped through a parsed structure. Whitespace and
	// key order are preserved byte-for-byte so downstream byte comparison and
	// golden-file testing stay valid.
	Fragment string
}

// Sentinel errors. ErrVariantUnsupported and ErrVariantMissing are distinct
// because "this bundle does not support that harness" and "no such variant
// key" call for different messages to the user.
var (
	ErrMalformedManifest  = errors.New("hookbundle: malformed manifest")
	ErrVariantMissing     = errors.New("hookbundle: no such variant")
	ErrVariantUnsupported = errors.New("hookbundle: variant not supported by this bundle")
	ErrReuseCycle         = errors.New("hookbundle: variant reuse cycle")
)

// wire types for hook.yaml, decoded by go-yaml and then converted into the
// exported model above. Kept unexported: the manifest's wire shape is this
// package's implementation detail, not part of its API.
type manifestYAML struct {
	SchemaVersion string                  `yaml:"schema_version"`
	ID            string                  `yaml:"id"`
	Version       string                  `yaml:"version"`
	Description   string                  `yaml:"description"`
	Placeholder   bool                    `yaml:"placeholder"`
	ContentHash   string                  `yaml:"content_hash"`
	Variants      map[string]*variantYAML `yaml:"variants"`
}

type variantYAML struct {
	Supported    bool          `yaml:"supported"`
	Reuses       string        `yaml:"reuses"`
	Files        []fileYAML    `yaml:"files"`
	Registration []regStepYAML `yaml:"registration"`
}

type fileYAML struct {
	Source string `yaml:"source"`
	Target string `yaml:"target"`
}

type regStepYAML struct {
	ID          string `yaml:"id"`
	TargetPath  string `yaml:"target_path"`
	Performable bool   `yaml:"performable"`
	Instruction string `yaml:"instruction"`
	Fragment    string `yaml:"fragment"`
}

// Parse decodes manifest bytes. Malformed input yields a diagnosable error
// naming what failed — never a panic, never a silently zero-valued manifest.
func Parse(data []byte) (Manifest, error) {
	var mw manifestYAML
	if err := yaml.Unmarshal(data, &mw); err != nil {
		return Manifest{}, fmt.Errorf("%w: %v", ErrMalformedManifest, err)
	}

	m := Manifest{
		SchemaVersion: mw.SchemaVersion,
		ID:            mw.ID,
		Version:       mw.Version,
		Description:   mw.Description,
		Placeholder:   mw.Placeholder,
		ContentHash:   mw.ContentHash,
		Variants:      make(map[string]Variant, len(mw.Variants)),
	}

	for key, vw := range mw.Variants {
		if vw == nil {
			continue
		}
		v := Variant{
			Key:       key,
			Supported: vw.Supported,
			Reuses:    vw.Reuses,
		}
		for _, fw := range vw.Files {
			v.Files = append(v.Files, File{
				Source: fw.Source,
				Target: fw.Target,
			})
		}
		for _, rw := range vw.Registration {
			v.Registration = append(v.Registration, RegistrationStep{
				ID:          rw.ID,
				TargetPath:  rw.TargetPath,
				Performable: rw.Performable,
				Instruction: rw.Instruction,
				Fragment:    rw.Fragment,
			})
		}
		m.Variants[key] = v
	}

	return m, nil
}

// Load reads and parses hook.yaml from a bundle directory.
func Load(bundleDir string) (Manifest, error) {
	path := filepath.Join(bundleDir, "hook.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		return Manifest{}, fmt.Errorf("hookbundle: reading %s: %w", path, err)
	}
	m, err := Parse(data)
	if err != nil {
		return Manifest{}, fmt.Errorf("hookbundle: parsing %s: %w", path, err)
	}
	return m, nil
}
