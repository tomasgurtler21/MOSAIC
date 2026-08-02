package domain

import (
	"context"
	"time"

	"mosaic-common/interaction"
)

// Interaction re-exports the shared interaction port from mosaic-common. It is the
// only channel through which a use case may consult a user. No implementation may
// block indefinitely; the CLI implementation must never block.
type Interaction = interaction.Interaction

// LogSource discovers what log data exists, using directory structure only.
// It NEVER returns an error: a missing, unusable or empty source is a described
// outcome, and anything odd encountered on the way is a Finding.
// Implementations: logscan.Scanner (production), in-memory fake (tests).
type LogSource interface {
	// Default probes for OrchestrationLogs/ beneath workDir. An absent folder
	// yields Source{Kind: SourceNotFound}.
	Default(workDir string) Source

	// Classify decides whether path is a logs root, a single run folder, or
	// unusable-with-reason. A non-existent path yields SourceNotFound.
	Classify(path string) Source

	// Enumerate lists runs, the unattributable bucket, agent-instance folders and
	// their event files for a usable source. A non-usable source yields an empty
	// Inventory carrying that Source. No file contents are read.
	Enumerate(src Source) (Inventory, []Finding)
}

// EventReader streams one JSONL event file. Malformed lines are contained per
// line as Findings; they never abort the file.
// Implementations: logread.Reader (production), scripted fake (tests).
type EventReader interface {
	// Read decodes path line by line, calling visit for each decoded event in
	// file order. The whole file is never buffered in memory.
	//
	// Returns an error ONLY when visit returns one (returned unchanged) or ctx is
	// cancelled. An unopenable file, a malformed line, a truncated tail and an
	// unrecognised schema version all yield Findings and a nil error.
	Read(ctx context.Context, path string, visit func(Event) error) ([]Finding, error)
}

// PricingStore owns the external YAML pricing file.
// Implementations: pricing.Store (production), in-memory fake (tests).
type PricingStore interface {
	// Path returns the config file's location so a frontend can tell the user
	// exactly which file to edit or which file was written.
	Path() string

	// Load reads the table. An absent file yields an empty table and a nil error.
	// A comments-only file yields an empty table. Individually invalid entries are
	// skipped and reported as Findings while the remaining entries still load.
	// An error is returned only when the document itself cannot be parsed.
	Load(ctx context.Context) (PricingTable, []Finding, error)

	// Put persists one model entry, creating the file and its parent directory if
	// absent, and never discarding existing entries or (where practical) comments.
	Put(ctx context.Context, entry ModelPricing) error
}

// Clock provides the current time. Injected so output is deterministic in tests.
type Clock interface {
	Now() time.Time
}
