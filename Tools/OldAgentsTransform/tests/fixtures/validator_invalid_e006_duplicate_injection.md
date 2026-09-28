---
id: test-validator-e006-injection
version: 1.0.0
name: test-agent
description: Invalid file - same INJECTION name appears more than once in the file (E006)
---

<Identity type="core">
# TestAgent Agent

<CodebaseContext type="project">
First occurrence of CodebaseContext injection.
</CodebaseContext>

<CodebaseContext type="project">
Second occurrence - duplicate injection boundary name.
</CodebaseContext>

</Identity>
