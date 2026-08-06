---
id: test-extra-sections
version: 1.0.0
name: test-agent
description: Document with canonical sections in correct order, interspersed with non-canonical sections.
---

[[SECTION:Identity]]
# TestAgent Agent
Identity section content. Canonical section at the correct position.
[[/SECTION:Identity]]

[[SECTION:PrologueSection]]
A non-canonical section appearing between Identity and Capabilities.
The E007 order check must skip this section entirely (subsequence rule).
[[/SECTION:PrologueSection]]

[[SECTION:Capabilities]]
## Capabilities
Capabilities section content. Canonical section in correct relative order.
[[/SECTION:Capabilities]]

[[SECTION:InternalNotes]]
Another non-canonical section between Capabilities and Constraints.
[[/SECTION:InternalNotes]]

[[SECTION:Constraints]]
## Constraints
Constraints section content.
[[/SECTION:Constraints]]
