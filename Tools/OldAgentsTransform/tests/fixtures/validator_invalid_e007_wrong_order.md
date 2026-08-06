---
id: test-validator-e007
version: 1.0.0
name: test-agent
description: Invalid file - sections appear in wrong order (Capabilities before Identity) (E007)
---

[[SECTION:Capabilities]]
## Capabilities
Content here. Capabilities appears before Identity, violating canonical order.
[[/SECTION:Capabilities]]
---
[[SECTION:Identity]]
# TestAgent Agent
Content here. Identity appears after Capabilities, violating canonical order.
[[/SECTION:Identity]]
