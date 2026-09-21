---
version: "1.0.0"
harness: codex
---

# Harness Injections -- Codex (module-local test fixture)

This file is a FROZEN test fixture for the codex package's own unit tests (T5.6).
It is NOT the live catalog content and must NOT be updated in step with the real
Catalog/HarnessInjections/Codex/HarnessInjections.md. If you need to change the
test assertions in codex_test.go, update both this file and the matching constant.

<HarnessConstraints type="managed">
Codex operates in a sandboxed environment. Tool access is controlled by the sandbox_mode field set during deployment.
</HarnessConstraints>
