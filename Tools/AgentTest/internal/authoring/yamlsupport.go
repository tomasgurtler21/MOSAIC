package authoring

// Shared helpers for the two YAML-authored formats (test suite, test
// definition): parsing the document to a mapping node so top-level unknown
// fields can be reported with a line number, plus the missing-required-field
// diagnostic shape both formats use.

import (
	"fmt"

	"github.com/goccy/go-yaml/ast"
	"github.com/goccy/go-yaml/parser"
)

// parseYAMLRoot parses src as a YAML document and returns its root mapping
// node. A parse failure or a non-mapping root is reported as a
// "malformed-document" diagnostic and returns a nil node.
func parseYAMLRoot(src Source, report *Report) *ast.MappingNode {
	file, err := parser.ParseBytes(src.Data, 0)
	if err != nil {
		report.Add(Diagnostic{
			Severity: SeverityError,
			Code:     "malformed-document",
			Path:     src.Path,
			Message:  err.Error(),
		})
		return nil
	}
	if len(file.Docs) == 0 || file.Docs[0].Body == nil {
		// An empty document has no fields to check; callers still run their
		// own required-field checks against the zero-valued decode.
		return &ast.MappingNode{}
	}
	root, ok := file.Docs[0].Body.(*ast.MappingNode)
	if !ok {
		report.Add(Diagnostic{
			Severity: SeverityError,
			Code:     "malformed-document",
			Path:     src.Path,
			Message:  "expected a mapping at the document root",
		})
		return nil
	}
	return root
}

// checkUnknownTopLevelFields reports a "unknown-field" diagnostic, with line
// and structural pointer, for every root-level key not present in known.
func checkUnknownTopLevelFields(src Source, root *ast.MappingNode, known map[string]bool, report *Report) {
	if root == nil {
		return
	}
	for _, v := range root.Values {
		key := v.Key.String()
		if known[key] {
			continue
		}
		report.Add(Diagnostic{
			Severity: SeverityError,
			Code:     "unknown-field",
			Path:     src.Path,
			Line:     v.Key.GetToken().Position.Line,
			Pointer:  key,
			Message:  fmt.Sprintf("unknown field %q", key),
		})
	}
}

// sequencePointer renders a structural pointer into the ith element of a
// named list, e.g. sequencePointer("tests", 2, "timeout") -> "tests[2].timeout".
func sequencePointer(list string, i int, field string) string {
	return fmt.Sprintf("%s[%d].%s", list, i, field)
}

// removedHarnessKeyMessage is the diagnostic text every "removed-key-harness"
// occurrence uses, naming both the removal and where a harness is actually
// selected instead.
const removedHarnessKeyMessage = "harness: is no longer part of the suite/test schema; " +
	"the harness is selected by the invocation (the --harness flag or the TUI's " +
	"harness-select screen), never declared in an authored file"

// mappingChild returns the value of root's top-level key, or nil when root
// is nil or declares no such key.
func mappingChild(root *ast.MappingNode, key string) ast.Node {
	if root == nil {
		return nil
	}
	for _, v := range root.Values {
		if v.Key.String() == key {
			return v.Value
		}
	}
	return nil
}

// reportRemovedHarnessKeyIfPresent reports a "removed-key-harness" diagnostic
// for node's own "harness" key, if node is a mapping that declares one. node
// is nil-safe: a section that was never authored (e.g. no "defaults:") has
// nothing to check.
func reportRemovedHarnessKeyIfPresent(src Source, node ast.Node, pointer string, report *Report) {
	m, ok := node.(*ast.MappingNode)
	if !ok {
		return
	}
	for _, v := range m.Values {
		if v.Key.String() != "harness" {
			continue
		}
		report.Add(Diagnostic{
			Severity: SeverityError,
			Code:     "removed-key-harness",
			Path:     src.Path,
			Line:     v.Key.GetToken().Position.Line,
			Pointer:  pointer,
			Message:  removedHarnessKeyMessage,
		})
	}
}

// reportRemovedSubjectDefinitionKeyIfPresent reports a
// "removed-key-subject-definition" diagnostic when the subject mapping in root
// contains a "definition" key. The sandbox-relative definition path is now
// produced by the deployment port at render time and must never be authored.
func reportRemovedSubjectDefinitionKeyIfPresent(src Source, root *ast.MappingNode, report *Report) {
	subjectNode := mappingChild(root, "subject")
	if subjectNode == nil {
		return
	}
	subjectMapping, ok := subjectNode.(*ast.MappingNode)
	if !ok {
		return
	}
	for _, v := range subjectMapping.Values {
		if v.Key.String() != "definition" {
			continue
		}
		report.Add(Diagnostic{
			Severity: SeverityError,
			Code:     "removed-key-subject-definition",
			Path:     src.Path,
			Line:     v.Key.GetToken().Position.Line,
			Pointer:  "subject.definition",
			Message: "subject.definition is removed; use subject.agent to name the catalogue agent " +
				"key instead; the sandbox-relative definition path is now produced by rendering " +
				"from the deployment port's own result and must not be authored",
		})
	}
}

// missingRequiredField builds the standard "missing-required-field"
// diagnostic every authored format reports the same way.
func missingRequiredField(src Source, field string) Diagnostic {
	return Diagnostic{
		Severity: SeverityError,
		Code:     "missing-required-field",
		Path:     src.Path,
		Pointer:  field,
		Message:  fmt.Sprintf("missing required field %q", field),
	}
}
