// Package todo collects the manual steps a deployment run could not perform automatically and
// renders them for two consumers: the MOSAIC-DEPLOYMENT-TODO.md file (RenderMarkdown) and the
// on-screen summary shown by both frontends (RenderSummary). Both renderers operate on the same
// collected items so they cannot disagree. A run with no items still produces a file that clearly
// says so.
package todo
