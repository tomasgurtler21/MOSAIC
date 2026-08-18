# stub-agent-placeholder
# Shared placeholder generic-form stub agent definition used by e2e test
# definitions. All stub_agents entries in e2e test fixtures point here so
# preflight's os.Stat existence check passes. The fake adapter used by e2e
# tests does not invoke the deployment tool (Deploy is nil), so only the file
# existence check runs; the render step is skipped.
