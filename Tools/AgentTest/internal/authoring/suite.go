package authoring

// ParseSuite parses a test-suite document (`*.suite.yaml`) into
// domain.TestSuite.

import (
	"time"

	goyaml "github.com/goccy/go-yaml"
	"github.com/goccy/go-yaml/ast"

	"mosaic-agent-test/internal/domain"
)

var suiteKnownFields = map[string]bool{
	"schema_version": true,
	"id":             true,
	"description":    true,
	"defaults":       true,
	"tests":          true,
}

// WireSettings is the flattened set of run-settings fields the wire formats
// carry, shared by suite defaults, suite entry overrides and a test
// definition's own settings. Pointer fields mirror domain.RunSettings: a nil
// field is "not stated at this level", not "stated as zero".
//
// harness: is deliberately absent. A suite is harness-agnostic and carries
// no harness identity of its own — the harness is always selected
// externally, by the invocation. A document still declaring it is rejected
// (see reportRemovedHarnessKeyIfPresent), never silently ignored.
type WireSettings struct {
	Timeout              *string  `yaml:"timeout" json:"timeout"`
	TurnLimit            *int     `yaml:"turn_limit" json:"turn_limit"`
	Repetitions          *int     `yaml:"repetitions" json:"repetitions"`
	PassRate             *float64 `yaml:"pass_rate" json:"pass_rate"`
	StopAfterInvocations *int     `yaml:"stop_after_invocations" json:"stop_after_invocations"`

	// EchoFidelity controls whether echo fidelity mismatches affect the
	// test verdict. Valid values: "required" (default), "advisory".
	EchoFidelity *string `yaml:"echo_fidelity" json:"echo_fidelity"`
}

// toDomain converts w to domain.RunSettings. The only field needing real
// translation is Timeout, whose wire form is a duration string.
func (w WireSettings) toDomain(src Source, pointer string, report *Report) domain.RunSettings {
	settings := domain.RunSettings{
		TurnLimit:            w.TurnLimit,
		Repetitions:          w.Repetitions,
		PassRate:             w.PassRate,
		StopAfterInvocations: w.StopAfterInvocations,
		EchoFidelity:         w.EchoFidelity,
	}
	if w.Timeout != nil {
		d, err := time.ParseDuration(*w.Timeout)
		if err != nil {
			report.Add(Diagnostic{
				Severity: SeverityError,
				Code:     "malformed-field",
				Path:     src.Path,
				Pointer:  pointer,
				Message:  "timeout is not a valid duration: " + err.Error(),
			})
		} else {
			settings.Timeout = &d
		}
	}
	return settings
}

type wireSuiteEntry struct {
	Path         string `yaml:"path"`
	WireSettings `yaml:",inline"`
}

type wireSuite struct {
	SchemaVersion int              `yaml:"schema_version"`
	ID            string           `yaml:"id"`
	Description   string           `yaml:"description"`
	Defaults      WireSettings     `yaml:"defaults"`
	Tests         []wireSuiteEntry `yaml:"tests"`
}

// ParseSuite parses and schema-validates a test-suite document, accumulating
// every diagnostic found rather than stopping at the first.
func ParseSuite(src Source) (domain.TestSuite, Report) {
	var report Report

	root := parseYAMLRoot(src, &report)
	if root == nil {
		return domain.TestSuite{}, report
	}
	checkUnknownTopLevelFields(src, root, suiteKnownFields, &report)
	reportRemovedHarnessKeyIfPresent(src, mappingChild(root, "defaults"), "defaults.harness", &report)
	if testsNode, ok := mappingChild(root, "tests").(*ast.SequenceNode); ok {
		for i, item := range testsNode.Values {
			reportRemovedHarnessKeyIfPresent(src, item, sequencePointer("tests", i, "harness"), &report)
		}
	}

	var wire wireSuite
	if err := goyaml.Unmarshal(src.Data, &wire); err != nil {
		report.Add(Diagnostic{
			Severity: SeverityError,
			Code:     "malformed-document",
			Path:     src.Path,
			Message:  err.Error(),
		})
		return domain.TestSuite{}, report
	}

	if wire.ID == "" {
		report.Add(missingRequiredField(src, "id"))
	}

	suite := domain.TestSuite{
		SchemaVersion: wire.SchemaVersion,
		ID:            wire.ID,
		Description:   wire.Description,
		Defaults:      wire.Defaults.toDomain(src, "defaults.timeout", &report),
		SourcePath:    src.Path,
	}

	for i, e := range wire.Tests {
		suite.Entries = append(suite.Entries, domain.SuiteEntry{
			Path:     e.Path,
			Settings: e.WireSettings.toDomain(src, sequencePointer("tests", i, "timeout"), &report),
		})
	}

	return suite, report
}
