// Package docformat is the single authority on MOSAIC file structure. It parses and serialises
// MOSAIC documents (YAML frontmatter + markdown body) with byte-exact fidelity: an unmodified
// document round-trips to identical bytes, and every untouched region of a modified document is
// preserved exactly as written in the source. It recognises [[SECTION:...]] and [[INJECTION:...]]
// boundary tags and treats all other content as opaque spans.
package docformat
