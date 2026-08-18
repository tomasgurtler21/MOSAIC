---
name: test-agent
---

<Identity type="core">
Identity section content.
</Identity>

<CommunicationProtocol type="managed">
</CommunicationProtocol>

<Capabilities type="core">
Capabilities content — appears before ArtifactProvenance, which is out of canonical order.
</Capabilities>

<ArtifactProvenance type="managed">
ArtifactProvenance appears after Capabilities — out of canonical order (should be at slot 2, before Capabilities at slot 3).
</ArtifactProvenance>

<ArtifactProvenanceExtension type="project">
</ArtifactProvenanceExtension>
