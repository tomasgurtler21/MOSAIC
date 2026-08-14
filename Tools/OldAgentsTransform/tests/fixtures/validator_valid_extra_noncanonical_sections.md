---
id: test-extra-sections
version: 1.0.0
name: test-agent
description: Document with canonical sections in correct order, interspersed with non-canonical sections.
---

<Identity type="core">
# TestAgent Agent
Identity section content. Canonical section at the correct position.
</Identity>

<PrologueSection type="core">
A non-canonical section appearing between Identity and Capabilities.
The E007 order check must skip this section entirely (subsequence rule).
</PrologueSection>

<Capabilities type="core">
## Capabilities
Capabilities section content. Canonical section in correct relative order.
</Capabilities>

<InternalNotes type="core">
Another non-canonical section between Capabilities and Constraints.
</InternalNotes>

<Constraints type="core">
## Constraints
Constraints section content.
</Constraints>
