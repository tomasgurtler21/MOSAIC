---
id: test-validator-e005-near-miss
version: 1.0.0
name: test-agent
description: Invalid file - a near-miss tag (space after colon) is treated as content outside boundary (E005)
---

[[SECTION: Identity]]
# TestAgent Agent
This near-miss tag (space after colon) does not match TAG_PATTERN.
It is treated as ordinary content, and since it falls outside any real boundary, it triggers E005.
