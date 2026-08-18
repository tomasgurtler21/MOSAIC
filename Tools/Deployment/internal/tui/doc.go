// Package tui is the interactive terminal frontend. It owns the terminal for the lifetime of a
// run, presents question-shaped prompts through the Interaction port, and renders the plan review
// and run summary screens using the Bubble Tea framework. It contains no flow logic; every
// question that does not fit the four Interaction shapes is a port design defect, fixed in domain.
// It imports only app and domain.
package tui
