---
id: test-validator-e006-injection
version: 1.0.0
name: test-agent
description: Invalid file - same INJECTION name appears more than once in the file (E006)
---

[[SECTION:Identity]]
# TestAgent Agent

[[INJECTION:IdentityExtension]]
First occurrence of IdentityExtension injection.
[[/INJECTION:IdentityExtension]]

[[INJECTION:IdentityExtension]]
Second occurrence - duplicate injection boundary name.
[[/INJECTION:IdentityExtension]]

[[/SECTION:Identity]]
