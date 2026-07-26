// Package descriptor loads, parses, and validates harness descriptor files (harness.yaml).
// It owns the YAML wire format and the mapping from that wire format onto domain.HarnessDescriptor
// (which carries no struct tags per CD-1). It also exports the shared, descriptor-driven
// algorithms MapTools and ApplyFrontmatterSpec that every provision tier delegates to, so the
// mapping logic has exactly one implementation.
package descriptor
