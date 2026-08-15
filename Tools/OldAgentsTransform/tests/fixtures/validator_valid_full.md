---
id: test-validator-full
version: 1.0.0
name: test-agent
description: Valid file with 6 sections, one top-level DEPLOYED slot (CommunicationProtocol), and multiple non-empty regions
---

<Identity type="core">
# TestAgent Agent

You are the TestAgent agent in a multi-agent orchestration system.

**Goal:** Serve as a test fixture.

<IdentityExtension type="project">
Additional identity content injected here.
</IdentityExtension>

<AvailableWorkflows type="managed">
Available workflows list injected here.
</AvailableWorkflows>

</Identity>
---

<CommunicationProtocol type="managed" version="1.9">
## Communication Protocol

Standard protocol content delivered by the deploy tool.

</CommunicationProtocol>
---

<Capabilities type="core">
## Capabilities

### Core Capabilities
- Test valid boundary structures
- Verify canonical names are accepted

<LanguagePatterns type="project">
Language pattern content injected here.
</LanguagePatterns>

<CodebaseContext type="project">
Codebase context content injected here.
</CodebaseContext>

</Capabilities>
---

<Constraints type="core">
## Constraints

- Stay within scope
- Do not modify fixtures

<CustomConstraints type="custom">
Custom constraint content injected here.
</CustomConstraints>

</Constraints>
---

<ErrorHandling type="core">
## Error Handling

- Handle errors gracefully
- Return structured results

<ErrorHandlingExtension type="project">
Error handling extension content injected here.
</ErrorHandlingExtension>

</ErrorHandling>
---

<ExecutionPhilosophy type="core">
## Execution Philosophy

- Quality over speed
- Context management

<ContextLimits type="project">
Context limit content injected here.
</ContextLimits>

</ExecutionPhilosophy>
