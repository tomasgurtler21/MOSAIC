---
id: test-validator-e006-injection
version: 1.0.0
name: test-agent
description: Invalid file - same INJECTION name appears more than once in the file (E006)
---

<Identity type="core">
# TestAgent Agent

<IdentityExtension type="project">
First occurrence of IdentityExtension injection.
</IdentityExtension>

<IdentityExtension type="project">
Second occurrence - duplicate injection boundary name.
</IdentityExtension>

</Identity>
