package codextoml

// export_test.go exposes package-internal vocabulary symbols to the external
// test package (package codextoml_test) for white-box classification testing.
// These names are only visible when the package is built in test mode.

// IsStampKey wraps the unexported isStampKey function for testing.
// It reports whether key is the Deployed name of some entry in agentfields.All().
var IsStampKey = isStampKey

// IsMosaicOwned wraps the unexported isMosaicOwned function for testing.
// It reports whether key is MOSAIC-owned: in the Codex-native emitted set, or
// the Deployed name of a stamp.
var IsMosaicOwned = isMosaicOwned

// EmittedKeys exposes the closed Codex-native emitted set for testing.
// The test iterates it to assert each key is emitted as a native TOML key.
var EmittedKeys = emittedKeys

// ReadStampBlock wraps the unexported readStampBlock function for testing.
// It reads the leading stamp comment lines from a Codex TOML byte slice and
// returns the stamp name-to-value map (Deployed names only) and the byte offset
// of the first non-header line.
var ReadStampBlock = readStampBlock
