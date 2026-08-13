#!/bin/sh
# Test hook script — content for integrity validation fixture.
# This file's actual SHA-256 does NOT match the content_hash stored in ../hook.yaml.
# The mismatch is intentional: it simulates a developer editing a hook file without
# bumping the bundle version, which the catalog must report as a hook-hash-mismatch.
echo "placeholder hook"
