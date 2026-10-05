package catalog

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"sort"
	"strings"

	"mosaic-common/docformat"
	"mosaic-deploy/internal/domain"
)

// ErrCatalogSourceInvalid matches (errors.Is) the error Load returns when one or more
// catalog source files cannot be interpreted.
var ErrCatalogSourceInvalid = errors.New("catalog source file is invalid")

// SourceKind identifies which kind of catalog source a failure concerns.
type SourceKind string

const (
	SourceKindAgent    SourceKind = "agent"
	SourceKindSkill    SourceKind = "skill"
	SourceKindWorkflow SourceKind = "workflow"
)

// SourceProblem identifies why a catalog source file cannot be interpreted.
type SourceProblem string

const (
	SourceProblemUnparseable   SourceProblem = "unparseable"
	SourceProblemNoFrontmatter SourceProblem = "no-frontmatter"
	SourceProblemMissingField  SourceProblem = "missing-field"
	SourceProblemUnreadable    SourceProblem = "unreadable"
)

// SourceFailure is one catalog source file that cannot be interpreted.
type SourceFailure struct {
	Path          string // absolute path of the offending file
	Kind          SourceKind
	Problem       SourceProblem
	MissingFields []string // SourceProblemMissingField only; subset of id, name, version in that order
	Err           error    // raw parse error (Unparseable) or read error (Unreadable); nil otherwise
}

// SourceValidationError aggregates every SourceFailure found by one Load.
type SourceValidationError struct {
	Failures []SourceFailure // sorted by Path ascending; never empty when returned
}

// Error lists the failure count, then every offending file with its problem, one per line.
func (e *SourceValidationError) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s: %d file(s) cannot be interpreted", ErrCatalogSourceInvalid, len(e.Failures))
	for _, f := range e.Failures {
		fmt.Fprintf(&b, "\n  %s: %s", f.Path, f.detail())
	}
	return b.String()
}

// Unwrap returns ErrCatalogSourceInvalid followed by every non-nil SourceFailure.Err.
func (e *SourceValidationError) Unwrap() []error {
	errs := []error{ErrCatalogSourceInvalid}
	for _, f := range e.Failures {
		if f.Err != nil {
			errs = append(errs, f.Err)
		}
	}
	return errs
}

// detail describes the problem in a human-readable form.
func (f SourceFailure) detail() string {
	switch f.Problem {
	case SourceProblemNoFrontmatter:
		return "no frontmatter"
	case SourceProblemMissingField:
		return "frontmatter is missing required field(s): " + strings.Join(f.MissingFields, ", ")
	case SourceProblemUnreadable:
		return "cannot read file: " + f.Err.Error()
	default:
		return f.Err.Error()
	}
}

// sourceIssue is the typed error produced while reading and checking one source file.
type sourceIssue struct {
	problem SourceProblem
	missing []string
	err     error
}

func (s *sourceIssue) Error() string {
	if s.err == nil {
		return string(s.problem)
	}
	return fmt.Sprintf("%s: %v", s.problem, s.err)
}

func (s *sourceIssue) Unwrap() error { return s.err }

// readAndParse reads path and parses it as a docformat document. A read error is
// returned as an unreadable sourceIssue and a parse error as an unparseable one.
// A not-exist read error stays recognisable through errors.Is(err, fs.ErrNotExist).
func readAndParse(path string) (*docformat.Document, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, &sourceIssue{problem: SourceProblemUnreadable, err: err}
	}
	doc, err := docformat.Parse(data)
	if err != nil {
		return nil, &sourceIssue{problem: SourceProblemUnparseable, err: err}
	}
	return doc, nil
}

// sourceFailures accumulates every failure found during one catalog load.
type sourceFailures []SourceFailure

// addFailure records err for path. A file that does not exist is not a failure.
// Errors that are not sourceIssues are treated as unparseable.
func (f *sourceFailures) addFailure(path string, kind SourceKind, err error) {
	if err == nil || errors.Is(err, fs.ErrNotExist) {
		return
	}
	failure := SourceFailure{Path: path, Kind: kind, Problem: SourceProblemUnparseable, Err: err}
	var issue *sourceIssue
	if errors.As(err, &issue) {
		failure.Problem = issue.problem
		failure.MissingFields = issue.missing
		failure.Err = issue.err
		if issue.problem == SourceProblemNoFrontmatter || issue.problem == SourceProblemMissingField {
			failure.Err = nil
		}
	}
	*f = append(*f, failure)
}

// asError returns nil when no failure was recorded, otherwise a *SourceValidationError
// with failures sorted by path.
func (f sourceFailures) asError() error {
	if len(f) == 0 {
		return nil
	}
	sorted := append([]SourceFailure(nil), f...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Path < sorted[j].Path })
	return &SourceValidationError{Failures: sorted}
}

// checkAgentSource validates a parsed agent: every agent needs frontmatter, and a
// subagent additionally needs a non-empty id, name, and version.
func checkAgentSource(agent domain.Agent, hasFrontmatter bool) error {
	if !hasFrontmatter {
		return &sourceIssue{problem: SourceProblemNoFrontmatter}
	}
	if agent.Role != domain.RoleSubagent {
		return nil
	}
	var missing []string
	if agent.NumericID == "" {
		missing = append(missing, "id")
	}
	if agent.Name == "" {
		missing = append(missing, "name")
	}
	if agent.Version == "" {
		missing = append(missing, "version")
	}
	if len(missing) > 0 {
		return &sourceIssue{problem: SourceProblemMissingField, missing: missing}
	}
	return nil
}
