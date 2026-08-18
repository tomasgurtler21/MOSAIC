package domain

// StageSource describes where a run's stage table was looked for, so a stop
// caused by its absence can name the cause rather than the symptom.
//
// The zero value means "not stated"; consumers must degrade to a generic
// message rather than rendering empty fields.
type StageSource struct {
	// Path is the absolute path the stage table was read from or looked
	// for at.
	Path string

	// Seeded is true when the run's seed inputs declared a file at Path.
	// It distinguishes "a stage table was supposed to be seeded here and
	// is missing" from "this run never had one".
	Seeded bool
}
