// Package app is the use-case layer. It owns flow sequencing for the two top-level operations
// (DeployNew, Update), decides when to consult the user through the Interaction port, latches
// SkippedAll per QuestionID, persists tier-to-model mappings to user config, and assembles the
// domain.RunSummary returned to both frontends. It imports every core package but never imports
// tui or cli; the frontends depend on app, not the reverse.
package app
