package harness

// ModelCatalog is one CLI-backed harness's model data: the identifiers it
// accepts, and how to describe their shape to a person.
type ModelCatalog struct {
	// HarnessID is the CLIHarness.ID this catalog belongs to.
	HarnessID string

	// IDs are the valid model identifiers, in stable declared order — the
	// order a selection UI presents and a usage text lists.
	IDs []string

	// FormatHint is human-readable guidance on the identifier shape, e.g.
	// "provider/model-id". Display only: nothing validates against it, and
	// nothing switches on it.
	FormatHint string
}

// cliModelCatalogs is the single declaration of every CLI-backed harness's
// model data, in the same stable order as cliHarnesses. Each entry is seeded
// verbatim from the Deploy tool's builtin harness descriptors.
//
// This must mirror cliHarnesses one-for-one. The coverage test
// TestModelCatalog_EveryIdentityCatalogEntryHasModelData enforces that
// invariant at test time.
var cliModelCatalogs = []ModelCatalog{
	{
		HarnessID:  HarnessIDClaudeCode,
		FormatHint: "claude-<model>-<version>",
		IDs: []string{
			"claude-opus-4-6",
			"claude-sonnet-4-6",
			"claude-haiku-4-5",
			"claude-fable-5",
			"claude-opus-5",
			"claude-sonnet-5",
			"claude-sonnet-4-5",
			"claude-opus-4-8",
			"claude-opus-4-7",
		},
	},
	{
		HarnessID:  HarnessIDOpenCode,
		FormatHint: "provider/model-id",
		IDs: []string{
			"github-copilot/claude-opus-4.6",
			"github-copilot/claude-sonnet-4.6",
			"openai/gpt-5.6-luna",
			"openai/gpt-5.6-terra",
			"openai/gpt-5.6-sol",
		},
	},
	{
		HarnessID:  HarnessIDGHCPCLI,
		FormatHint: "claude-<model>-<version> or gpt-<version>",
		IDs: []string{
			"claude-sonnet-5",
			"claude-sonnet-4.6",
			"claude-sonnet-4.5",
			"claude-opus-4.6",
			"claude-opus-4.7",
			"claude-opus-4.8",
			"claude-opus-4.8-fast",
			"claude-opus-5",
			"claude-haiku-4-5",
			"claude-fable-5",
			"gpt-5.4",
			"gpt-5.4-mini",
			"gpt-5.4-nano",
			"gpt-5.3-codex",
			"gpt-5.2-codex",
			"gpt-4.1",
			"gemini-3.5-flash",
			"gemini-3.6-flash",
			"kimi-k2.7-code",
			"gpt-5.6-sol",
			"gpt-5.6-terra",
			"gpt-5.6-luna",
			"mai-code-1-flash",
		},
	},
}

// copyIDs returns a fresh copy of ids so a caller mutating the returned slice
// cannot corrupt the catalog's own backing array.
func copyIDs(ids []string) []string {
	if ids == nil {
		return nil
	}
	out := make([]string, len(ids))
	copy(out, ids)
	return out
}

// ModelCatalogs returns every CLI-backed harness's model catalog, in the same
// stable order CLIHarnesses uses, as fresh values the caller may not corrupt
// for the next caller.
func ModelCatalogs() []ModelCatalog {
	out := make([]ModelCatalog, len(cliModelCatalogs))
	for i, e := range cliModelCatalogs {
		out[i] = ModelCatalog{
			HarnessID:  e.HarnessID,
			FormatHint: e.FormatHint,
			IDs:        copyIDs(e.IDs),
		}
	}
	return out
}

// LookupModelCatalog returns the model catalog for harnessID. The second
// result distinguishes "no such harness" from a harness with no models; it
// never panics. The returned IDs slice is a fresh copy.
func LookupModelCatalog(harnessID string) (ModelCatalog, bool) {
	for _, e := range cliModelCatalogs {
		if e.HarnessID == harnessID {
			return ModelCatalog{
				HarnessID:  e.HarnessID,
				FormatHint: e.FormatHint,
				IDs:        copyIDs(e.IDs),
			}, true
		}
	}
	return ModelCatalog{}, false
}

// ModelIDs returns harnessID's valid model identifiers as a fresh slice, or
// nil when harnessID names no known CLI-backed harness. It is the convenience
// form for a caller that only needs the list.
func ModelIDs(harnessID string) []string {
	cat, ok := LookupModelCatalog(harnessID)
	if !ok {
		return nil
	}
	return cat.IDs
}

// IsModelID reports whether modelID is valid for harnessID. It is false for
// an unknown harness, for an unknown model, and for a model that is valid for
// a different harness.
func IsModelID(harnessID, modelID string) bool {
	cat, ok := LookupModelCatalog(harnessID)
	if !ok {
		return false
	}
	for _, id := range cat.IDs {
		if id == modelID {
			return true
		}
	}
	return false
}
