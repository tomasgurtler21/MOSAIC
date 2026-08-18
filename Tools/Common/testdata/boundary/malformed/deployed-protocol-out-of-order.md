---
name: test-agent
---

<Identity type="core">
Identity content.
</Identity>

<Capabilities type="core">
Capabilities content — at canonical index 2, before CommunicationProtocol below.
</Capabilities>

<CommunicationProtocol type="managed">
Protocol content appearing after Capabilities — out of canonical order (CommunicationProtocol belongs at slot 1, before Capabilities at slot 2).
</CommunicationProtocol>
