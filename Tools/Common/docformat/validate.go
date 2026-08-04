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
	// canonicalOrderInDoc tracks canonical-order slot names encountered during the walk,
	// in document order. It includes canonical section names and top-level canonical
	// deployed names (CommunicationProtocol). This drives the out-of-order check.
	var canonicalOrderInDoc []string
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

			// Track canonical-order slots for out-of-order checking.
			// Sections: track any section name that appears in CanonicalOrder.
			// Deployed: track top-level deployed names that appear in CanonicalOrder.
			if kind == NodeSection {
				if canonicalOrderIndex(name) >= 0 {
					canonicalOrderInDoc = append(canonicalOrderInDoc, name)
				}
			} else if kind == NodeDeployed {
				// Only top-level deployed names count for canonical ordering (len(stack)==0
				// at the point we process the opening tag, before pushing it to the stack).
				if len(stack) == 0 && canonicalOrderIndex(name) >= 0 {
					canonicalOrderInDoc = append(canonicalOrderInDoc, name)
				}
			}

			// wrong-marker: a canonical name declared with the wrong marker kind.
			// This is reported in preference to unknown-injection/unknown-deployed so
			// that one mistake produces one actionable diagnostic.
			if kind == NodeInjection && isCanonicalDeployed(name) {
				issues = append(issues, Issue{
					Severity: SeverityError,
					Code:     "wrong-marker",
					Message: fmt.Sprintf(
						"boundary name %q is a tool-managed name and must be declared with [[DEPLOYED:]] but was declared with [[INJECTION:]] (line %d)",
						name, lineNum,
					),
					Line: lineNum,
				})
			} else if kind == NodeDeployed && isCanonicalInjection(name) {
				issues = append(issues, Issue{
					Severity: SeverityError,
					Code:     "wrong-marker",
					Message: fmt.Sprintf(
						"boundary name %q is a user-owned name and must be declared with [[INJECTION:]] but was declared with [[DEPLOYED:]] (line %d)",
						name, lineNum,
					),
					Line: lineNum,
				})
			}

			// wrong-parent: canonical injections and deployed regions must appear inside
			// their expected parent section.
			if opts.RequireInjectionParents {
				if kind == NodeInjection {
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
				} else if kind == NodeDeployed {
					if expectedParent, isCanonical := DeployedParent[name]; isCanonical {
						if expectedParent == "" {
							// CommunicationProtocol and any future top-level deployed names:
							// must appear at body top level (no enclosing nodes on the stack).
							if len(stack) != 0 {
								issues = append(issues, Issue{
									Severity: SeverityError,
									Code:     "wrong-parent",
									Message: fmt.Sprintf(
										"deployed region %q must be at body top level but appears inside %q (line %d)",
										name, stack[len(stack)-1].name, lineNum,
									),
									Line: lineNum,
								})
							}
						} else {
							current := enclosingSection()
							if current != expectedParent {
								var msg string
								if current == "" {
									msg = fmt.Sprintf(
										"deployed region %q must be inside section %q but appears at body top level (line %d)",
										name, expectedParent, lineNum,
									)
								} else {
									msg = fmt.Sprintf(
										"deployed region %q must be inside section %q but is inside %q (line %d)",
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
				}
			}

			// unknown-injection: flag non-canonical injection names when the option is set.
			// wrong-marker is raised in preference when the name is canonical-but-misplaced,
			// so a single mistake produces one actionable diagnostic.
			if kind == NodeInjection && !opts.AllowUnknownInjections {
				if isCanonicalDeployed(name) {
					// wrong-marker already raised; skip unknown-injection to avoid a second
					// diagnostic for the same mistake.
				} else if !isCanonicalInjection(name) {
					issues = append(issues, Issue{
						Severity: SeverityError,
						Code:     "unknown-injection",
						Message:  fmt.Sprintf("injection %q is not a recognised canonical injection name (line %d)", name, lineNum),
						Line:     lineNum,
					})
				}
			}

			// unknown-deployed: flag unrecognised [[DEPLOYED:]] names when the option is set.
			// wrong-marker is raised in preference when the name is canonical-but-misplaced.
			if kind == NodeDeployed && !opts.AllowUnknownInjections {
				if isCanonicalInjection(name) {
					// wrong-marker already raised; skip unknown-deployed.
				} else if !isCanonicalDeployed(name) {
					issues = append(issues, Issue{
						Severity: SeverityError,
						Code:     "unknown-deployed",
						Message:  fmt.Sprintf("deployed region %q is not a recognised canonical tool-managed name (line %d)", name, lineNum),
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

	// out-of-order-section: canonical-order slots must appear in their canonical relative order.
	// The check walks CanonicalOrder (which includes both sections and the CommunicationProtocol
	// deployed slot) and enforces relative ordering among the slots that are present.
	if opts.RequireCanonicalSections {
		issues = append(issues, checkSectionOrder(canonicalOrderInDoc)...)
	}

	return issues
}

// checkSectionOrder returns out-of-order-section issues for any canonical-order entry whose
// position in the document is before an earlier-appearing canonical-order entry.
// It enforces relative order among the entries that are present; it does NOT require all
// eight canonical slots to be present.
func checkSectionOrder(names []string) []Issue {
	var issues []Issue
	lastIdx := -1
	for _, name := range names {
		idx := canonicalOrderIndex(name)
		if idx < 0 {
			continue // not a canonical-order slot; skip
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

// canonicalOrderIndex returns the position of name in CanonicalOrder, or -1.
func canonicalOrderIndex(name string) int {
	for i, s := range CanonicalOrder {
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

// tagTypeName returns "SECTION", "INJECTION", or "DEPLOYED" for use in error messages.
func tagTypeName(kind NodeKind) string {
	switch kind {
	case NodeSection:
		return "SECTION"
	case NodeDeployed:
		return "DEPLOYED"
	default:
		return "INJECTION"
	}
}
