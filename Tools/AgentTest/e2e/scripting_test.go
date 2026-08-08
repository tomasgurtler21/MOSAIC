package e2e

// Small builders for internal/harness/fake.Options.SubjectTurns, so a
// scenario test's script reads as the sequence of steps it declares rather
// than as struct-literal boilerplate repeated across every test in this
// suite.

import (
	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/harness/fake"
)

// subjectStep is one step of a scripted subject's turn sequence.
type subjectStep fake.SubjectTurn

// invoke declares that the scripted subject dispatches one collaborator.
func invoke(tool, agent string) subjectStep {
	id := domain.CollaboratorIdentity{ToolName: tool, AgentIdentity: agent}
	return subjectStep{Invoke: &id}
}

// finish declares the scripted subject's final protocol message, with a
// completed disposition and a zero exit code — the common case every
// scenario that does not itself test a non-completed disposition uses.
func finish(protocolMessage string) subjectStep {
	return subjectStep{Result: &domain.SubjectResult{
		ProtocolMessage: protocolMessage,
		Disposition:     domain.DispositionCompleted,
		ExitCode:        0,
	}}
}

// finishAs is finish with an explicit disposition and exit code, for a
// scenario scripting a non-ordinary ending.
func finishAs(protocolMessage string, disposition domain.RunDisposition, exitCode int) subjectStep {
	return subjectStep{Result: &domain.SubjectResult{
		ProtocolMessage: protocolMessage,
		Disposition:     disposition,
		ExitCode:        exitCode,
	}}
}

// fakeOptionsFor assembles a fake.Options from capabilities and a step
// sequence.
func fakeOptionsFor(caps domain.HarnessCapabilities, steps []subjectStep) fake.Options {
	turns := make([]fake.SubjectTurn, 0, len(steps))
	for _, s := range steps {
		turns = append(turns, fake.SubjectTurn(s))
	}
	return fake.Options{Capabilities: caps, SubjectTurns: turns}
}
