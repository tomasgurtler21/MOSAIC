// Package manifest reads and writes the deployment manifest file
// (<workspace>/.mosaic/manifest.yaml). It distinguishes one normal state (present and readable)
// from four abnormal states — absent, empty, corrupt, and future-schema — and never collapses them
// into an empty result. The Hash function computes content hashes in the canonical
// "sha256:<lowercase-hex>" form with no byte normalisation, so a line-ending change is a
// detectable modification.
package manifest
