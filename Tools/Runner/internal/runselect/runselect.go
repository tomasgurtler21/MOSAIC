// Package runselect owns the single run-selection decision shared by the
// CLI, the CLI entry point, and the TUI entry point. It turns a workspace
// scan plus explicit caller input into either a resolved run identity or a
// question that must be answered before work can begin — never an inference
// from how many runs exist.
package runselect

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"mosaic-run/internal/domain"
	"mosaic-run/internal/runscan"
)

// ErrUsage marks a malformed or contradictory selection request (for example
// both RunIDFlag and NewRun set, or a malformed RunIDFlag). Callers detect it
// with errors.Is(err, ErrUsage) so the CLI can choose its usage exit code and
// the TUI can print and exit before launching.
var ErrUsage = errors.New("runselect: usage error")

// NewRunChoiceID is the reserved Choice.ID for starting a new run. It is
// never a valid run_id, so it can never collide with a real run.
const NewRunChoiceID = "__new_run__"

// Minter mints identity for a new run: a fresh run_id and the matching
// run-scoped folder path, absolute, whose final element is
// Orchestration-{runID}. Minting cannot fail.
type Minter func() (runID string, runFolder string)

// Request is the explicit input to a selection decision.
type Request struct {
	// Scan is the workspace scan result.
	Scan runscan.ScanResult

	// WorkDir roots run-scoped folders. Absolute.
	WorkDir string

	// RunIDFlag is the --run value, or "" when absent.
	RunIDFlag string

	// NewRun is true when --new-run was given.
	NewRun bool
}

// Position is enough of a resumed run's recorded state to recognise it as
// the intended one. Values are as recorded; nothing here is interpreted.
type Position struct {
	Phase       string
	Stage       string // "" when the run records no stage
	LastAgent   string
	LastUpdated time.Time

	// Workflow is the workflow ID the run recorded when it was created, as
	// read from its artifact. "" when the run records no workflow.
	Workflow string
}

// Identity is a settled run selection.
type Identity struct {
	RunID     string
	RunFolder string // absolute; final element is Orchestration-{RunID}
	IsNewRun  bool

	// Position is the resumed run's recorded position, for the
	// announcement. Always nil when IsNewRun is true. May be nil for a
	// resumed run whose artifact could not be parsed.
	Position *Position

	// Workflow is the workflow ID a resumed run recorded when it was
	// created, carried out of the selection decision so the caller resumes
	// the run as what it already is rather than asking again. Always "" when
	// IsNewRun is true.
	Workflow string
}

// Decision is the outcome of Resolve. Exactly one field is non-nil.
type Decision struct {
	// Resolved is set when explicit input determined the run.
	Resolved *Identity

	// Question is set when the selection is not settled and must be asked
	// (TUI) or refused (CLI).
	Question *Question
}

// ChoiceKind is the shape of a Choice.
type ChoiceKind int

const (
	ChoiceNewRun ChoiceKind = iota
	ChoiceResume
	ChoiceUnresumable
)

// Choice is one available outcome.
type Choice struct {
	// ID identifies the choice. NewRunChoiceID for the new-run choice, the
	// run_id otherwise.
	ID string

	// Kind distinguishes the three shapes.
	Kind ChoiceKind

	// Run is the run's metadata. Zero value for ChoiceNewRun.
	Run runscan.RunInfo

	// Selectable is false for ChoiceUnresumable and true otherwise. A
	// non-selectable choice is shown, never chosen.
	Selectable bool

	// Reason is why the choice is not selectable. Empty when Selectable.
	Reason runscan.UnresumableReason
}

// Question is an unsettled selection, carrying every outcome available.
// A Question always offers starting a new run, whatever the workspace
// contains, and Choices is therefore never empty.
type Question struct {
	// Choices are: the new-run choice first, then resumable runs in
	// recency order, then unresumable runs in recency order.
	Choices []Choice
}

// Resolve turns a scan result plus explicit caller input into either a
// resolved run identity or a question that must be answered before work
// can begin. It never infers a selection from how many runs exist.
//
// Resolution rules, in order:
//  1. req.RunIDFlag and req.NewRun both set        -> UsageError.
//  2. req.RunIDFlag set, malformed run_id          -> UsageError.
//  3. req.RunIDFlag set, well-formed               -> Decision.Resolved,
//     IsNewRun false, folder rooted at req.WorkDir. A run_id naming a
//     folder that the scan reported unresumable is a UsageError naming
//     the reason.
//  4. req.NewRun set                               -> Decision.Resolved,
//     IsNewRun true, identity from mint.
//  5. neither set                                  -> Decision.Question,
//     for any workspace contents including zero candidates.
//
// mint is called at most once and only in case 4. It must be non-nil.
//
// Resolve performs no I/O beyond what mint does, and does not create,
// modify, or read any run folder.
func Resolve(req Request, mint Minter) (Decision, error) {
	if req.RunIDFlag != "" && req.NewRun {
		return Decision{}, fmt.Errorf("%w: --run and --new-run are mutually exclusive", ErrUsage)
	}

	if req.RunIDFlag != "" {
		if !domain.IsValidRunID(req.RunIDFlag) {
			return Decision{}, fmt.Errorf("%w: invalid run_id format %q; expected {YYYYMMDD}T{HHMMSS}Z-{4-hex}", ErrUsage, req.RunIDFlag)
		}
		for _, u := range req.Scan.Unresumable {
			if u.RunID == req.RunIDFlag {
				return Decision{}, fmt.Errorf("%w: run %s is %s and cannot be resumed", ErrUsage, req.RunIDFlag, u.Reason)
			}
		}
		id := Identity{
			RunID:     req.RunIDFlag,
			RunFolder: filepath.Join(req.WorkDir, domain.RunScopedFolder(req.RunIDFlag)),
			IsNewRun:  false,
		}
		for _, c := range req.Scan.Candidates {
			if c.RunID == req.RunIDFlag {
				id.Position = positionFromRunInfo(c.RunInfo)
				id.Workflow = workflowFromPosition(id.Position)
				break
			}
		}
		return Decision{Resolved: &id}, nil
	}

	if req.NewRun {
		runID, runFolder := mint()
		return Decision{Resolved: &Identity{RunID: runID, RunFolder: runFolder, IsNewRun: true}}, nil
	}

	return Decision{Question: buildQuestion(req.Scan)}, nil
}

// buildQuestion renders every outcome available for the given scan result:
// the new-run choice first, then resumable runs in recency order, then
// unresumable runs in recency order, matching runscan.ScanResult's ordering.
func buildQuestion(scan runscan.ScanResult) *Question {
	choices := make([]Choice, 0, len(scan.Candidates)+len(scan.Unresumable)+1)
	choices = append(choices, Choice{ID: NewRunChoiceID, Kind: ChoiceNewRun, Selectable: true})
	for _, c := range scan.Candidates {
		choices = append(choices, Choice{
			ID:         c.RunID,
			Kind:       ChoiceResume,
			Run:        c.RunInfo,
			Selectable: true,
		})
	}
	for _, u := range scan.Unresumable {
		choices = append(choices, Choice{
			ID:         u.RunID,
			Kind:       ChoiceUnresumable,
			Run:        u.RunInfo,
			Selectable: false,
			Reason:     u.Reason,
		})
	}
	return &Question{Choices: choices}
}

// positionFromRunInfo converts a run's recorded metadata into a Position.
// Returns nil when the metadata carries nothing recognisable (an
// unparseable artifact), matching Identity.Position's documented contract.
//
// A recorded workflow counts as something recognisable in its own right: a run
// created but not yet executed records its workflow and nothing else, and that
// workflow is the one thing the caller needs to resume it.
func positionFromRunInfo(info runscan.RunInfo) *Position {
	if info.Workflow == "" && info.Phase == "" && info.LastAgent == "" && info.LastUpdated.IsZero() {
		return nil
	}
	return &Position{
		Phase:       info.Phase,
		Stage:       info.Stage,
		LastAgent:   info.LastAgent,
		LastUpdated: info.LastUpdated,
		Workflow:    info.Workflow,
	}
}

// workflowFromPosition reads the recorded workflow out of a position, treating
// an absent position as a run that recorded no workflow. An absent workflow
// travels as absent: it is never substituted from elsewhere, so the caller can
// refuse the resume rather than run the user's work through a process nothing
// ever chose.
func workflowFromPosition(pos *Position) string {
	if pos == nil {
		return ""
	}
	return pos.Workflow
}

// Answer converts a user's or caller's choice of one Choice from a
// Decision.Question into a resolved Identity.
//
// choiceID is Choice.ID. NewRunChoiceID yields a newly minted identity via
// mint; any other ID must name a selectable Choice. A choiceID that names a
// non-selectable Choice, or no Choice at all, returns an error.
func Answer(q Question, choiceID string, mint Minter) (Identity, error) {
	if choiceID == NewRunChoiceID {
		runID, runFolder := mint()
		return Identity{RunID: runID, RunFolder: runFolder, IsNewRun: true}, nil
	}
	for _, c := range q.Choices {
		if c.ID != choiceID {
			continue
		}
		if !c.Selectable {
			return Identity{}, fmt.Errorf("choice %q is not selectable (%s)", choiceID, c.Reason)
		}
		pos := positionFromRunInfo(c.Run)
		return Identity{
			RunID:     c.Run.RunID,
			RunFolder: c.Run.FolderPath,
			IsNewRun:  false,
			Position:  pos,
			Workflow:  workflowFromPosition(pos),
		}, nil
	}
	return Identity{}, fmt.Errorf("no such choice %q", choiceID)
}

// Announce renders the one-line statement of the run about to be performed:
// which run, whether it is new or resumed, and for a resumed run its
// recorded position. It is the single source of that wording for both
// frontends.
//
// Contract of the rendered text:
//   - always contains id.RunID
//   - always states either that the run is new or that it is being resumed
//   - when id.IsNewRun is false and id.Position is non-nil, states the
//     recorded phase, the stage when one is recorded, and the last agent
func Announce(id Identity) string {
	var sb strings.Builder
	if id.IsNewRun {
		fmt.Fprintf(&sb, "Starting new run %s", id.RunID)
		return sb.String()
	}
	fmt.Fprintf(&sb, "Resuming run %s", id.RunID)
	if id.Position != nil {
		fmt.Fprintf(&sb, " (phase=%s", id.Position.Phase)
		if id.Position.Stage != "" {
			fmt.Fprintf(&sb, " stage=%s", id.Position.Stage)
		}
		fmt.Fprintf(&sb, " last_agent=%s)", id.Position.LastAgent)
	}
	return sb.String()
}
