---
id: test-validator-e007
version: 1.0.0
name: test-agent
description: Invalid file - sections appear in wrong order (Capabilities before Identity) (E007)
---

<Capabilities type="core">
## Capabilities
Content here. Capabilities appears before Identity, violating canonical order.
</Capabilities>
---
<Identity type="core">
# TestAgent Agent
Content here. Identity appears after Capabilities, violating canonical order.
</Identity>
