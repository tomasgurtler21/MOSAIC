// Package cli is the non-interactive frontend. It parses command-line arguments, constructs a
// pre-answered Interaction that resolves questions from flags and configuration (never blocking),
// calls the app.Service, and returns a process exit code. Its NewInteraction implementation
// records any unresolvable question as a TODO item rather than prompting the user. The --output
// json flag emits domain.RunSummary as a single JSON document.
package cli
