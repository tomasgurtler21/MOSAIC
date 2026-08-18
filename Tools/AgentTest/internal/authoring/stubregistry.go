package authoring

// ParseStubRegistry parses a stub-registry document (`*.stubs.json`) into
// domain.StubRegistry. Unlike the two YAML formats, this one is JSON — both
// machine-written and machine-read, where byte comparison matters.

import (
	"encoding/json"
	"fmt"
	"sort"

	"mosaic-agent-test/internal/domain"
)

var stubRegistryKnownFields = map[string]bool{
	"schema_version":   true,
	"test_id":          true,
	"on_unmatched":     true,
	"generic_response": true,
	"stubs":            true,
}

type wireStubMatch struct {
	Tool       string `json:"tool"`
	Agent      string `json:"agent"`
	Invocation int    `json:"invocation"`
}

type wireFileEffect struct {
	Path    string `json:"path"`
	Content string `json:"content"`
	Ref     string `json:"ref"`
}

type wireSideEffects struct {
	CreateFiles []wireFileEffect `json:"create_files"`
}

type wireStub struct {
	Match       wireStubMatch    `json:"match"`
	Response    json.RawMessage  `json:"response"`
	SideEffects *wireSideEffects `json:"side_effects"`
}

type wireStubRegistry struct {
	SchemaVersion   int             `json:"schema_version"`
	TestID          string          `json:"test_id"`
	OnUnmatched     string          `json:"on_unmatched"`
	GenericResponse json.RawMessage `json:"generic_response"`
	Stubs           []wireStub      `json:"stubs"`
}

// ParseStubRegistry parses and schema-validates a stub-registry document,
// accumulating every diagnostic found rather than stopping at the first.
func ParseStubRegistry(src Source) (domain.StubRegistry, Report) {
	var report Report

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(src.Data, &raw); err != nil {
		report.Add(Diagnostic{
			Severity: SeverityError,
			Code:     "malformed-document",
			Path:     src.Path,
			Message:  err.Error(),
		})
		return domain.StubRegistry{}, report
	}
	checkUnknownJSONFields(src, raw, stubRegistryKnownFields, &report)

	var wire wireStubRegistry
	if err := json.Unmarshal(src.Data, &wire); err != nil {
		report.Add(Diagnostic{
			Severity: SeverityError,
			Code:     "malformed-document",
			Path:     src.Path,
			Message:  err.Error(),
		})
		return domain.StubRegistry{}, report
	}

	if wire.TestID == "" {
		report.Add(missingRequiredField(src, "test_id"))
	}

	policy := domain.UnmatchedPolicy(wire.OnUnmatched)
	if policy == domain.UnmatchedGenericResponse && len(wire.GenericResponse) == 0 {
		report.Add(Diagnostic{
			Severity: SeverityError,
			Code:     "missing-generic-response",
			Path:     src.Path,
			Pointer:  "generic_response",
			Message:  `on_unmatched is "generic-response" but no generic_response payload was declared`,
		})
	}

	reg := domain.StubRegistry{
		SchemaVersion:   wire.SchemaVersion,
		TestID:          wire.TestID,
		OnUnmatched:     policy,
		GenericResponse: wire.GenericResponse,
		SourcePath:      src.Path,
	}

	for _, s := range wire.Stubs {
		stub := domain.Stub{
			Match: domain.StubMatch{
				Identity: domain.CollaboratorIdentity{
					ToolName:      s.Match.Tool,
					AgentIdentity: s.Match.Agent,
				},
				Invocation: s.Match.Invocation,
			},
			Response: s.Response,
		}
		if s.SideEffects != nil {
			for _, fe := range s.SideEffects.CreateFiles {
				stub.SideEffects = append(stub.SideEffects, domain.FileEffect{
					Path:    fe.Path,
					Content: fe.Content,
					Ref:     fe.Ref,
				})
			}
		}
		reg.Stubs = append(reg.Stubs, stub)
	}

	return reg, report
}

// checkUnknownJSONFields reports an "unknown-field" diagnostic for every key
// in raw not present in known, in a deterministic (sorted) order. JSON gives
// no line number, so Line stays 0.
func checkUnknownJSONFields(src Source, raw map[string]json.RawMessage, known map[string]bool, report *Report) {
	keys := make([]string, 0, len(raw))
	for k := range raw {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if known[k] {
			continue
		}
		report.Add(Diagnostic{
			Severity: SeverityError,
			Code:     "unknown-field",
			Path:     src.Path,
			Pointer:  k,
			Message:  fmt.Sprintf("unknown field %q", k),
		})
	}
}
