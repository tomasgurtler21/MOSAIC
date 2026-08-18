// Package config loads and saves the two configuration files used by the deployment tool:
// tool-config.yaml (project-scoped, typically version-controlled) and user-config.yaml
// (user-scoped, stores tier-to-model mappings). An absent tool-config.yaml yields documented
// defaults rather than an error. Per-agent model selections have no representation here; only
// tier-level mappings are persisted to user config.
package config
