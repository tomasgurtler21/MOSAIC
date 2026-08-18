---
id: test-validator-e006
version: 1.0.0
name: test-agent
description: Invalid file - same boundary name appears more than once (E006)
---

<Identity type="core">
# TestAgent Agent
First occurrence of Identity section.
</Identity>
---
<Identity type="core">
# TestAgent Agent
Second occurrence of Identity - duplicate boundary name.
</Identity>
