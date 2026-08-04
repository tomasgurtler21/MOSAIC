---
id: test-validator-full
version: 1.0.0
name: test-agent
description: Valid file with all 7 sections, the CommunicationProtocol deployed slot, and multiple non-empty regions
---

[[SECTION:Identity]]
# TestAgent Agent

You are the TestAgent agent in a multi-agent orchestration system.

**Goal:** Serve as a test fixture.

[[INJECTION:IdentityExtension]]
Additional identity content injected here.
[[/INJECTION:IdentityExtension]]

[[DEPLOYED:AvailableWorkflows]]
Available workflows list injected here.
[[/DEPLOYED:AvailableWorkflows]]

[[/SECTION:Identity]]
---

[[DEPLOYED:CommunicationProtocol]]
<!-- protocol-version: 1.9 -->
## Communication Protocol

Standard protocol content delivered by the deploy tool.

[[/DEPLOYED:CommunicationProtocol]]
---

[[SECTION:ArtifactProvenance]]
## Artifact Provenance

Every output artifact carries run_id and created_by frontmatter fields.

[[INJECTION:ArtifactProvenanceExtension]]
Artifact provenance extension content injected here.
[[/INJECTION:ArtifactProvenanceExtension]]

[[/SECTION:ArtifactProvenance]]
---

[[SECTION:Capabilities]]
## Capabilities

### Core Capabilities
- Test valid boundary structures
- Verify canonical names are accepted

[[DEPLOYED:LanguagePatterns]]
Language pattern content injected here.
[[/DEPLOYED:LanguagePatterns]]

[[INJECTION:CodebaseContext]]
Codebase context content injected here.
[[/INJECTION:CodebaseContext]]

[[/SECTION:Capabilities]]
---

[[SECTION:Constraints]]
## Constraints

- Stay within scope
- Do not modify fixtures

[[DEPLOYED:CustomConstraints]]
Custom constraint content injected here.
[[/DEPLOYED:CustomConstraints]]

[[/SECTION:Constraints]]
---

[[SECTION:ErrorHandling]]
## Error Handling

- Handle errors gracefully
- Return structured results

[[INJECTION:ErrorHandlingExtension]]
Error handling extension content injected here.
[[/INJECTION:ErrorHandlingExtension]]

[[/SECTION:ErrorHandling]]
---

[[SECTION:OutputFormat]]
## Output Format

Always return structured output.

[[/SECTION:OutputFormat]]
---

[[SECTION:ExecutionPhilosophy]]
## Execution Philosophy

- Quality over speed
- Context management

[[INJECTION:ContextLimits]]
Context limit content injected here.
[[/INJECTION:ContextLimits]]

[[/SECTION:ExecutionPhilosophy]]
