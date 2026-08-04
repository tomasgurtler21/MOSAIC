---
id: test-validator-empty-injections
version: 1.0.0
name: test-agent
description: Valid file where injection boundaries have open tag immediately followed by close tag
---

[[SECTION:Identity]]
# TestAgent Agent

Content in identity section.

[[INJECTION:IdentityExtension]]
[[/INJECTION:IdentityExtension]]

[[DEPLOYED:AvailableWorkflows]]
[[/DEPLOYED:AvailableWorkflows]]

[[/SECTION:Identity]]
---

[[DEPLOYED:CommunicationProtocol]]
[[/DEPLOYED:CommunicationProtocol]]
---

[[SECTION:ArtifactProvenance]]
## Artifact Provenance

Artifact provenance content.

[[INJECTION:ArtifactProvenanceExtension]]
[[/INJECTION:ArtifactProvenanceExtension]]

[[/SECTION:ArtifactProvenance]]
---

[[SECTION:Capabilities]]
## Capabilities

Capabilities content.

[[DEPLOYED:LanguagePatterns]]
[[/DEPLOYED:LanguagePatterns]]

[[INJECTION:CodebaseContext]]
[[/INJECTION:CodebaseContext]]

[[INJECTION:OutputArtifactTemplate]]
[[/INJECTION:OutputArtifactTemplate]]

[[/SECTION:Capabilities]]
---

[[SECTION:Constraints]]
## Constraints

Constraint content.

[[DEPLOYED:HarnessConstraints]]
[[/DEPLOYED:HarnessConstraints]]

[[DEPLOYED:CustomConstraints]]
[[/DEPLOYED:CustomConstraints]]

[[/SECTION:Constraints]]
---

[[SECTION:ErrorHandling]]
## Error Handling

Error handling content.

[[INJECTION:ErrorHandlingExtension]]
[[/INJECTION:ErrorHandlingExtension]]

[[/SECTION:ErrorHandling]]
---

[[SECTION:OutputFormat]]
## Output Format

Output format content.

[[/SECTION:OutputFormat]]
---

[[SECTION:ExecutionPhilosophy]]
## Execution Philosophy

Execution philosophy content.

[[INJECTION:ContextLimits]]
[[/INJECTION:ContextLimits]]

[[/SECTION:ExecutionPhilosophy]]
