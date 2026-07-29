package docformat

import (
	"bytes"
	"fmt"
)

// Validate checks a parsed document for structural issues and returns all findings.
// The document's current body bytes (which may reflect mutations) are used for validation.
// Structural errors (unbalanced tags, wrong nesting, etc.) are always returned.
// Optional checks are gated behind ValidateOptions fields.
func Validate(d *Document, opts ValidateOptions) []Issue {
	raw := d.Body().bytes()
	return validateBytes(raw, opts)
}

// validateBytes runs the full structural validation pipeline over raw body bytes.
// It is separated from Validate so that tests can drive it directly if needed.
func validateBytes(raw []byte, opts ValidateOptions) []Issue {
	var issues []Issue
	lines := splitLinesPreserving(raw)

	type stackEntry struct {
		kind    NodeKind
		name    string
		lineNum int
	}

	var stack []stackEntry
	seenNames := map[string]bool{}
	var canonicalSectionsInDoc []string // canonical section names in the order they appear
	lineNum := 0

	// enclosingSection returns the name of the innermost enclosing section from the stack,
	// or "" when the current position is not inside any section.
	enclosingSection := func() string {
		for i := len(stack) - 1; i >= 0; i-- {
			if stack[i].kind == NodeSection {
				return stack[i].name
			}
		}
		return ""
	}

	for _, line := range lines {
		lineNum++
		kind, isClose, name, matched := parseBoundaryTag(line)

		if !matched {
			// Plain text content: flag non-blank content outside all boundaries.
			if len(stack) == 0 && len(bytes.TrimSpace(line)) > 0 {
				issues = append(issues, Issue{
					Severity: SeverityError,
					Code:     "content-outside-boundary",
					Message:  fmt.Sprintf("non-blank content at line %d appears outside all boundary tags", lineNum),
					Line:     lineNum,
				})
			}
			continue
		}

		if !isClose {
			// --- Opening tag ---

			// wrong-nesting: a section may not be nested inside another section.
			if kind == NodeSection && len(stack) > 0 {
				parentEntry := stack[len(stack)-1]
				if parentEntry.kind == NodeSection {
					issues = append(issues, Issue{
						Severity: SeverityError,
						Code:     "wrong-nesting",
						Message: fmt.Sprintf(
							"section %q is nested inside section %q at line %d — sections must not nest inside other sections",
							name, parentEntry.name, lineNum,
						),
						Line: lineNum,
					})
				}
			}

			// duplicate-name: each boundary name must appear at most once per document.
			if seenNames[name] {
				issues = append(issues, Issue{
					Severity: SeverityError,
					Code:     "duplicate-name",
					Message:  fmt.Sprintf("boundary name %q is used more than once at line %d", name, lineNum),
					Line:     lineNum,
				})
			}
			seenNames[name] = true

			// Track canonical sections for out-of-order checking.
			if kind == NodeSection {
				canonicalSectionsInDoc = append(canonicalSectionsInDoc, name)
			}

			// wrong-parent: canonical injections must appear inside their expected section.
			if kind == NodeInjection && opts.RequireInjectionParents {
				if expectedParent, isCanonical := InjectionParent[name]; isCanonical {
					current := enclosingSection()
					if current != expectedParent {
						var msg string
						if current == "" {
							msg = fmt.Sprintf(
								"injection %q must be inside section %q but appears at body top level (line %d)",
								name, expectedParent, lineNum,
							)
						} else {
							msg = fmt.Sprintf(
								"injection %q must be inside section %q but is inside %q (line %d)",
								name, expectedParent, current, lineNum,
							)
						}
						issues = append(issues, Issue{
							Severity: SeverityError,
							Code:     "wrong-parent",
							Message:  msg,
							Line:     lineNum,
						})
					}
				}
			}

			// unknown-injection: flag non-canonical injection names when the option is set.
			if kind == NodeInjection && !opts.AllowUnknownInjections {
				if !isCanonicalInjection(name) {
					issues = append(issues, Issue{
						Severity: SeverityError,
						Code:     "unknown-injection",
						Message:  fmt.Sprintf("injection %q is not a recognised canonical injection name (line %d)", name, lineNum),
						Line:     lineNum,
					})
				}
			}

			stack = append(stack, stackEntry{kind: kind, name: name, lineNum: lineNum})

		} else {
			// --- Closing tag ---

			if len(stack) == 0 {
				// No matching open tag on the stack.
				issues = append(issues, Issue{
					Severity: SeverityError,
					Code:     "unbalanced-tag",
					Message: fmt.Sprintf(
						"closing tag [[/%s:%s]] at line %d has no matching opening tag",
						tagTypeName(kind), name, lineNum,
					),
					Line: lineNum,
				})
			} else {
				top := stack[len(stack)-1]
				stack = stack[:len(stack)-1]

				// mismatched-tag: opening and closing names must match.
				if top.name != name {
					issues = append(issues, Issue{
						Severity: SeverityError,
						Code:     "mismatched-tag",
						Message: fmt.Sprintf(
							"opening tag %q (line %d) does not match closing tag %q (line %d)",
							top.name, top.lineNum, name, lineNum,
						),
						Line: lineNum,
						Node: top.name,
					})
				}
			}
		}
	}

	// Any tags remaining on the stack are unclosed.
	for _, entry := range stack {
		issues = append(issues, Issue{
			Severity: SeverityError,
			Code:     "unbalanced-tag",
			Message: fmt.Sprintf(
				"boundary tag %q opened at line %d is never closed",
				entry.name, entry.lineNum,
			),
			Line: entry.lineNum,
		})
	}

	// out-of-order-section: canonical sections must appear in their canonical relative order.
	if opts.RequireCanonicalSections {
		issues = append(issues, checkSectionOrder(canonicalSectionsInDoc)...)
	}

	return issues
}

// checkSectionOrder returns out-of-order-section issues for any canonical section whose
// position in the document is before an earlier-appearing canonical section.
// It enforces relative order among the sections that are present; it does NOT require all
// seven canonical sections to be present.
func checkSectionOrder(sectionNames []string) []Issue {
	var issues []Issue
	lastIdx := -1
	for _, name := range sectionNames {
		idx := canonicalSectionIndex(name)
		if idx < 0 {
			continue // not a canonical section; skip
		}
		if idx < lastIdx {
			issues = append(issues, Issue{
				Severity: SeverityError,
				Code:     "out-of-order-section",
				Message:  fmt.Sprintf("section %q appears out of canonical order", name),
			})
		} else {
			lastIdx = idx
		}
	}
	return issues
}

// canonicalSectionIndex returns the position of name in CanonicalSections, or -1.
func canonicalSectionIndex(name string) int {
	for i, s := range CanonicalSections {
		if s == name {
			return i
		}
	}
	return -1
}

// isCanonicalInjection reports whether name appears in CanonicalInjections.
func isCanonicalInjection(name string) bool {
	for _, ci := range CanonicalInjections {
		if ci == name {
			return true
		}
	}
	return false
}

// tagTypeName returns "SECTION" or "INJECTION" for use in error messages.
func tagTypeName(kind NodeKind) string {
	if kind == NodeSection {
		return "SECTION"
	}
	return "INJECTION"
}
