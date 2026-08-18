package app

// promote_service.go contains the service-level Promote flow: the Service.Promote method,
// the promoteHarnessDropKeys helper, the category prompt helper, the nextNumericID helper,
// and the promoteDestinationPath helper. All I/O for the promote capability belongs here;
// the pure generation core lives in promote.go (buildGenericAgent).

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"mosaic-common/docformat"
	"mosaic-deploy/internal/catalog"
	"mosaic-deploy/internal/catalog/catalogpaths"
	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/harness/descriptor"
)

// promoteHarnessDropSet is the unified result of the harness drop-key derivation and
// field classification. Produced by a single module.Frontmatter call so both products
// share the same plan.
type promoteHarnessDropSet struct {
	// DropKeys is the harness-specific frontmatter key drop set for PromoteInput.DropKeys.
	DropKeys map[string]bool
	// Classifier is the field classifier for the same harness, ready for unknown-field
	// detection and diverted-field recovery in the same Promote call.
	Classifier descriptor.FieldClassifier
}

// buildPromoteHarnessDropSet derives the harness-specific frontmatter drop set and the
// field classifier from a resolved harness module. Both products derive from a single
// module.Frontmatter call, so the classifier used for unknown-field detection and
// diverted-field recovery later in the Promote call is guaranteed to be built from the
// same plan that shaped the drop set.
//
// The drop set includes:
//   - Descriptor().Frontmatter.ModelKey and ToolsKey — always included, even when those
//     names happen to be MOSAIC generic vocabulary words (e.g. ModelKey="model").
//   - Every key from FrontmatterPlan.Set. FrontmatterSpec.Drop and FrontmatterPlan.Remove
//     name generic fields the harness removes at deploy time, which promote is trying to
//     restore, not remove again (AD-3). Only plan.Set keys enter the drop set.
//
// Keys in promoteProtectedGenericKeys are excluded (AD-4). On Frontmatter failure the
// classifier is built from descriptor keys alone; the error is returned so the caller
// can decide whether to proceed or abort.
//
// Pure with respect to the filesystem: it calls only module.Descriptor() and
// module.Frontmatter(...), both of which the HarnessModule contract requires to be
// deterministic and side-effect free.
func buildPromoteHarnessDropSet(
	module domain.HarnessModule,
	kind domain.ArtifactKind,
	agentKey string,
) (promoteHarnessDropSet, error) {
	desc := module.Descriptor()

	// Request the frontmatter plan for this specific agent kind and key. The plan is used
	// both to populate the field classifier and to derive the plan-Set key contribution to
	// the drop set.
	plan, err := module.Frontmatter(domain.FrontmatterRequest{
		Kind:     kind,
		AgentKey: agentKey,
	})

	// Build the field classifier. On plan failure, use a zero plan so the classifier still
	// contributes descriptor-declared keys. The error is returned alongside the partial set.
	var clfPlan domain.FrontmatterPlan
	if err == nil {
		clfPlan = plan
	}
	clf := descriptor.NewFieldClassifier(desc, clfPlan)

	result := make(map[string]bool)

	// Descriptor-level keys: the key the harness writes the model to and the key it writes
	// its tool entries to. Included even when they match a MOSAIC vocabulary name (e.g.
	// ModelKey="model"), because the harness explicitly uses them and promote must remove
	// them. AD-4 protected keys remain excluded.
	if k := desc.Frontmatter.ModelKey; k != "" && !promoteProtectedGenericKeys[k] {
		result[k] = true
	}
	if k := desc.Frontmatter.ToolsKey; k != "" && !promoteProtectedGenericKeys[k] {
		result[k] = true
	}

	// Plan.Set keys: the harness adds these to the deployed file; promote must remove them.
	// Plan.Remove and Spec.Drop name generic fields the harness removes at deploy time —
	// exactly what promote is trying to restore, not remove again (AD-3). Empty key names
	// and collisions with protected generic keys are silently ignored.
	for _, field := range clfPlan.Set {
		if field.Key == "" || promoteProtectedGenericKeys[field.Key] {
			continue
		}
		result[field.Key] = true
	}

	return promoteHarnessDropSet{DropKeys: result, Classifier: clf}, err
}

// promoteHarnessDropKeys derives the harness-specific frontmatter keys to drop from a
// promoted generic file. It is a thin wrapper over buildPromoteHarnessDropSet for callers
// that only need the drop-key map and not the field classifier.
//
// The plan is requested for the promoted agent's own kind and key so a harness that shapes
// its plan per artifact contributes the keys it would actually have added to this file.
// A module whose Frontmatter call fails contributes only its descriptor keys, and the error
// is returned so the caller can decide; it never panics or silently drops the whole set.
func promoteHarnessDropKeys(
	module domain.HarnessModule,
	kind domain.ArtifactKind,
	agentKey string,
) (map[string]bool, error) {
	set, err := buildPromoteHarnessDropSet(module, kind, agentKey)
	return set.DropKeys, err
}

// promoteRecoveredFields carries the generic-only frontmatter values recovered from the user
// during a promote run. A zero-valued member means "not supplied" and is omitted from the
// promoted file entirely — never written as an empty value.
type promoteRecoveredFields struct {
	RecommendedTier string
	TierRationale   string
	RequiredSkills  []string
	// Supplied names the fields the user actually answered, in ask order, for
	// PromoteResult.RecoveredFields.
	Supplied []string
}

// resolvePromoteTools turns the harness-side tool entries of a deployed agent into the
// authoritative generic tools list.
//
// Entries that reverse-map are resolved silently and never prompt. Each distinct unmapped
// entry is asked once through QPromoteCustomTool, in extraction order, mirroring
// resolveCustomTools. Every non-answered outcome — SkippedOne, SkippedAll, Cancelled, or a
// transport error — carries the harness name into the generic list verbatim and unmodified;
// SkippedAll additionally latches so no further question is asked this run. This is what
// makes the non-interactive path complete with zero user interaction.
//
// The returned tools slice is deduplicated first-seen in extraction order and is always
// non-nil, so the caller may pass it straight into PromoteInput.Tools as an authoritative
// replacement. verbatim lists the entries that were carried through unchanged, in the same
// order, for PromoteResult.VerbatimTools.
func (s *service) resolvePromoteTools(
	ctx context.Context,
	module domain.HarnessModule,
	entries []string,
	subject string,
) (tools []string, verbatim []string) {
	tools = make([]string, 0, len(entries))
	seen := make(map[string]bool, len(entries))

	resolutions := descriptor.ReverseMapTools(module.Descriptor(), entries)

	skippedAll := false

	addTool := func(name string) {
		if !seen[name] {
			seen[name] = true
			tools = append(tools, name)
		}
	}

	for _, res := range resolutions {
		if res.Outcome == domain.ToolMapped {
			// Resolved via descriptor mapping — add silently, no question asked.
			addTool(res.Generic)
			continue
		}

		// Unmapped entry — ask the user for a generic name, or carry verbatim.
		harnessName := res.Harness

		if skippedAll {
			// SkippedAll was already latched; carry verbatim without asking.
			addTool(harnessName)
			verbatim = append(verbatim, harnessName)
			continue
		}

		q := domain.TextQuestion{
			Question: domain.Question{
				ID:           domain.QPromoteCustomTool,
				Subject:      harnessName,
				Title:        "Generic tool name for harness tool " + harnessName,
				AllowSkip:    true,
				AllowSkipAll: true,
			},
		}
		ans, err := s.deps.Interaction.AskText(ctx, q)
		if err != nil {
			// Transport error — carry verbatim (AD-8).
			addTool(harnessName)
			verbatim = append(verbatim, harnessName)
			continue
		}

		switch ans.Status {
		case domain.Answered:
			if ans.Text != "" {
				addTool(ans.Text)
			} else {
				// Empty answer treated as non-answered — carry verbatim.
				addTool(harnessName)
				verbatim = append(verbatim, harnessName)
			}
		case domain.SkippedAll:
			skippedAll = true
			addTool(harnessName)
			verbatim = append(verbatim, harnessName)
		default:
			// SkippedOne, Cancelled, or any other non-answered status — carry verbatim (AD-8).
			addTool(harnessName)
			verbatim = append(verbatim, harnessName)
		}
	}

	return tools, verbatim
}

// resolvePromoteGenericFields asks for the generic-only frontmatter fields the harness drops
// at deploy time and that cannot be derived from the deployed file: recommended_tier,
// tier_rationale, and required_skills.
//
// A field the source already carries is not asked about — a harness that does not drop it
// carried it through, and re-asking would be noise. Each remaining field is asked at most
// once per run. A field the user does not supply (skipped, cancelled, unresolvable, or
// answered with empty text) is returned as its zero value, which the generation core writes
// with its empty form (recommended_tier: "", tier_rationale: "", required_skills: []) rather
// than omitting it — a deliberate, scoped override of AD-12 for the promote direction.
func (s *service) resolvePromoteGenericFields(
	ctx context.Context,
	facts promoteSourceFacts,
	subject string,
) promoteRecoveredFields {
	var result promoteRecoveredFields

	// recommended_tier — only asked when the source does not already carry it.
	if !facts.Keys["recommended_tier"] {
		opts := s.buildPromoteTierOptions()
		ans, err := s.deps.Interaction.SelectOne(ctx, domain.ChoiceQuestion{
			Question: domain.Question{
				ID:          domain.QPromoteRecommendedTier,
				Subject:     subject,
				Title:       "Recommended tier for the promoted agent",
				AllowSkip:   true,
				AllowCustom: true,
			},
			Options: opts,
		})
		if err == nil && ans.Status == domain.Answered {
			tier := ans.OptionID
			if tier == "" {
				tier = ans.Custom
			}
			if tier != "" {
				result.RecommendedTier = tier
				result.Supplied = append(result.Supplied, "recommended_tier")
			}
		}
	}

	// tier_rationale — only asked when the source does not already carry it.
	if !facts.Keys["tier_rationale"] {
		ans, err := s.deps.Interaction.AskText(ctx, domain.TextQuestion{
			Question: domain.Question{
				ID:        domain.QPromoteTierRationale,
				Subject:   subject,
				Title:     "Tier rationale for the promoted agent",
				AllowSkip: true,
			},
		})
		if err == nil && ans.Status == domain.Answered && ans.Text != "" {
			result.TierRationale = ans.Text
			result.Supplied = append(result.Supplied, "tier_rationale")
		}
	}

	// required_skills — only asked when the source does not already carry it.
	if !facts.Keys["required_skills"] {
		ans, err := s.deps.Interaction.AskText(ctx, domain.TextQuestion{
			Question: domain.Question{
				ID:        domain.QPromoteRequiredSkills,
				Subject:   subject,
				Title:     "Required skills (comma-separated)",
				AllowSkip: true,
			},
		})
		if err == nil && ans.Status == domain.Answered && ans.Text != "" {
			// Split on commas, trim whitespace, discard empty entries (AD-11).
			parts := strings.Split(ans.Text, ",")
			skills := make([]string, 0, len(parts))
			for _, p := range parts {
				p = strings.TrimSpace(p)
				if p != "" {
					skills = append(skills, p)
				}
			}
			if len(skills) > 0 {
				result.RequiredSkills = skills
				result.Supplied = append(result.Supplied, "required_skills")
			}
		}
	}

	return result
}

// buildPromoteTierOptions returns the SelectOne options for QPromoteRecommendedTier, one per
// tier the catalog has discovered, sorted by tier string for determinism.
//
// The source is Deps.Catalog.Tiers() []domain.TierInfo, whose Tier field is the only one used:
// each entry becomes domain.Option{ID: string(ti.Tier), Label: string(ti.Tier)}. TierInfo's
// Rationale and AgentKeys are deliberately not surfaced — Rationale is the existing rationale
// of other agents and must not be presented as a suggestion for this one. A nil or empty
// Tiers() yields an empty option slice, which is legitimate: the question sets AllowCustom so
// a tier no catalog agent declares yet can still be entered.
func (s *service) buildPromoteTierOptions() []domain.Option {
	tiers := s.deps.Catalog.Tiers()
	opts := make([]domain.Option, 0, len(tiers))
	for _, ti := range tiers {
		opts = append(opts, domain.Option{
			ID:    string(ti.Tier),
			Label: string(ti.Tier),
		})
	}
	sort.Slice(opts, func(i, j int) bool {
		return opts[i].ID < opts[j].ID
	})
	return opts
}

// nextNumericID returns the numeric agent id to assign to a newly promoted agent: one
// greater than the largest numeric id currently declared by any agent in the catalog,
// rendered in decimal. When the catalog declares no parseable numeric id, it returns "1".
//
// Non-numeric and empty id values are ignored rather than treated as errors. The result
// is deterministic for a given catalog state, which is what makes the write assertable in
// a test.
func nextNumericID(c catalog.Catalog) string {
	max := 0
	all := make([]domain.Agent, 0)
	all = append(all, c.Agents()...)
	all = append(all, c.UtilityAgents()...)
	orch := c.Orchestrator()
	if orch.Key != "" {
		all = append(all, orch)
	}
	for _, a := range all {
		if a.NumericID == "" {
			continue
		}
		n, err := strconv.Atoi(a.NumericID)
		if err != nil {
			continue
		}
		if n > max {
			max = n
		}
	}
	return strconv.Itoa(max + 1)
}

// promoteDestinationPath computes where the generated generic file is written, relative
// to the active catalogue root:
//
//	category == PromoteCategoryUtility → <catalogRoot>/UtilityAgents/{key}.md
//	otherwise                          → <catalogRoot>/Subagents/{category}/{key}.md
//
// No Catalog/ segment appears in these sub-paths: the catalogue root already is the catalogue.
// With no --catalog-folder supplied the resulting absolute path is byte-identical to the
// pre-feature default under the MOSAIC root.
//
// key is the source file's base name with a trailing ".agent.md" or ".md" removed — the
// same derivation the catalog uses to key an agent, so the promoted file lands under the
// key the user expects.
func promoteDestinationPath(catalogRoot, category, key string) string {
	if category == PromoteCategoryUtility {
		return catalogpaths.UtilityAgentFile(catalogRoot, key)
	}
	return catalogpaths.SubagentFile(catalogRoot, category, key)
}

// Promote generates a generic agent source file from a single already boundary-tagged
// harness-only agent file, so that a one-off harness-specific agent becomes reusable
// across harnesses through the normal Deploy/Update flow.
//
// Collision policy: when a file already exists at the destination and req.Overwrite is
// false, Promote returns ErrPromoteDestinationExists and writes nothing. With req.Overwrite
// true it replaces the file. There is no silent overwrite and no automatic renaming.
//
// Registration is automatic: writing a well-formed file into the chosen generic agent
// directory IS the registration — the catalog discovers agents by scanning those directories.
func (s *service) Promote(ctx context.Context, req PromoteRequest) (PromoteResult, error) {
	// Validate FilePath — there is no interactive fallback for a missing file path.
	if req.FilePath == "" {
		return PromoteResult{}, ErrPromoteFileRequired
	}

	// Stat the source path (following symlinks) before reading it. A missing or non-file
	// path produces a domain error with the offending path in the message, so the user knows
	// what to fix without seeing a localized OS error. Any other stat failure is wrapped with
	// path information but left as an access error rather than a domain sentinel.
	if info, statErr := os.Stat(req.FilePath); statErr != nil {
		if os.IsNotExist(statErr) {
			return PromoteResult{}, fmt.Errorf("promote source %s: %w", req.FilePath, ErrPromoteSourceNotFound)
		}
		return PromoteResult{}, fmt.Errorf("cannot access source file %s: %w", req.FilePath, statErr)
	} else if info.IsDir() {
		return PromoteResult{}, fmt.Errorf("promote source %s: %w", req.FilePath, ErrPromoteSourceNotFile)
	}

	// Validate and resolve the harness — runs after source-path validation, before reading.
	// HarnessID has no interactive fallback; the caller must supply the harness it already knows.
	if req.HarnessID == "" {
		return PromoteResult{}, ErrPromoteHarnessRequired
	}
	module, resolveErr := s.deps.Registry.Resolve(req.HarnessID)
	if resolveErr != nil {
		return PromoteResult{}, fmt.Errorf("%w: %w", ErrPromoteHarnessUnresolvable, resolveErr)
	}
	defer module.Close() //nolint:errcheck // close error must not fail an otherwise complete run

	// Read the source file bytes. The source is never modified or deleted by this function.
	src, err := os.ReadFile(req.FilePath)
	if err != nil {
		return PromoteResult{}, fmt.Errorf("reading source file %s: %w", req.FilePath, err)
	}

	// Apply the shared two-signal eligibility rule. Reject with ErrPromoteNotTransformed
	// when either signal is absent, so the caller learns which signal failed.
	verdict := eligibleHarnessOnly(src)
	if !verdict.Eligible {
		return PromoteResult{}, fmt.Errorf("%w: %s", ErrPromoteNotTransformed, verdict.Reason)
	}

	// Derive the agent key from the source filename — the same derivation the catalog uses
	// so the promoted file is reachable under the key the user expects.
	key := agentKeyFromFileName(filepath.Base(req.FilePath))

	// Inspect the deployed source for harness-side facts (tool entries and key presence set)
	// before asking any questions. This is a pure parse — no I/O, no questions.
	facts, factsErr := inspectPromoteSource(src, module.Descriptor())
	if factsErr != nil {
		return PromoteResult{}, factsErr
	}

	// Resolve category: use the pre-answered value (pre-answer convention — a non-empty
	// value suppresses the interactive question entirely) or ask through Interaction.
	category := req.Category
	if category == "" {
		category, err = s.askPromoteCategory(ctx, req.FilePath)
		if err != nil {
			return PromoteResult{}, err
		}
	}

	// Containment guard: reject category values that contain ".." traversal segments or
	// path separators. These must be rejected before any directory is created or path
	// resolution occurs, so a crafted value cannot escape the catalogue root.
	if strings.Contains(category, "..") || strings.ContainsAny(category, "/\\") {
		return PromoteResult{}, fmt.Errorf("invalid category %q: category must be a single directory name without path traversal or separators", category)
	}

	// Compute the destination path before checking for collisions. All promote writes
	// target the active catalogue root, not the MOSAIC root, so a custom --catalog-folder
	// receives the promoted file where the catalogue read side will also look.
	catalogRoot := s.deps.Catalog.CatalogRoot()
	destPath := promoteDestinationPath(catalogRoot, category, key)

	// Collision policy: refuse to overwrite an existing file without explicit consent.
	// Promote never silently overwrites a generic agent; the caller must set Overwrite: true.
	if !req.Overwrite {
		if _, statErr := os.Stat(destPath); statErr == nil {
			return PromoteResult{}, ErrPromoteDestinationExists
		}
	}

	// Derive the artifact kind for the harness drop-key computation. All promoted agents
	// use ArtifactAgent regardless of their role; the orchestrator-vs-subagent distinction
	// reaches the harness's FrontmatterPlan through key (AD-15), not through kind.
	kind := domain.ArtifactAgent

	// Derive the harness-specific frontmatter drop-set and the field classifier from the
	// resolved module's own contract. Both products share a single module.Frontmatter call
	// so the two derivations are guaranteed to agree. A plan failure is fatal: proceeding
	// without the full drop-set would leak the harness's own added fields into the generic
	// file — the exact defect this flow was designed to fix.
	harnessDropSet, dropErr := buildPromoteHarnessDropSet(module, kind, key)
	if dropErr != nil {
		return PromoteResult{}, fmt.Errorf("deriving harness frontmatter drop-set: %w", dropErr)
	}
	dropKeys := harnessDropSet.DropKeys
	clf := harnessDropSet.Classifier

	// Always add every diverted tool-destination field key to the drop set. These fields are
	// harness artifacts whose content is recovered into the generic tools list; the key itself
	// must never appear in the generic output (AC2.2, AC2.7).
	for _, df := range descriptor.DivertedToolFields(module.Descriptor()) {
		if !promoteProtectedGenericKeys[df.Key] {
			dropKeys[df.Key] = true
		}
	}

	// Parse the source frontmatter to classify each key and handle unknown fields (FR-3) and
	// diverted fields (FR-2). We parse the source once here; buildGenericAgent parses it again,
	// but promote is never a batch operation so this is acceptable.
	var strippedFields []StrippedField
	var divertedResolvedGenericNames []string // generic names recovered from diverted fields

	srcDoc, srcParseErr := docformat.Parse(src)
	if srcParseErr == nil {
		srcFm := srcDoc.Frontmatter()

		// Unknown-field detection (FR-3): classify every source key; keys that are ClassUnknown
		// are added to the drop set and reported as stripped.
		for _, k := range srcFm.Keys() {
			if clf.Classify(k) == descriptor.ClassUnknown {
				if !promoteProtectedGenericKeys[k] {
					dropKeys[k] = true
				}
				val, _ := srcFm.Get(k)
				rendered := descriptor.RenderFieldValues(val)
				strippedFields = append(strippedFields, StrippedField{
					Key:    k,
					Values: rendered,
					Reason: StripReasonUnknownField,
				})
			}
		}

		// Diverted-field recovery (FR-2, AC2.2, AC2.7): for each declared DestField destination,
		// extract its entries from the source, reverse-map them, and either recover them into the
		// generic tools list or report them as stripped — never prompt for diverted values.
		seenDivertedGeneric := make(map[string]bool)
		addDivertedGeneric := func(name string) {
			if !seenDivertedGeneric[name] {
				seenDivertedGeneric[name] = true
				divertedResolvedGenericNames = append(divertedResolvedGenericNames, name)
			}
		}

		for _, df := range descriptor.DivertedToolFields(module.Descriptor()) {
			var fieldValue domain.FieldValue
			if v, ok := srcFm.Get(df.Key); ok {
				fieldValue = v
			}
			entries := descriptor.ExtractDivertedToolEntries(df, fieldValue)

			var unmapped []string
			for _, entry := range entries {
				if genericName, ok := descriptor.ReverseMapTool(module.Descriptor(), entry); ok {
					addDivertedGeneric(genericName)
				} else {
					unmapped = append(unmapped, entry)
				}
			}

			// Report unmapped diverted values as stripped, per field (AC2.3, AC2.7).
			// The field key is always dropped (handled above), regardless of how many values mapped.
			if len(unmapped) > 0 {
				strippedFields = append(strippedFields, StrippedField{
					Key:    df.Key,
					Values: unmapped,
					Reason: StripReasonUnmappedDivertedValue,
				})
			}
		}
	}

	// Reverse-map the harness tool entries to the authoritative generic tools list.
	// resolvePromoteTools returns a non-nil slice (AD-7) so it is always authoritative.
	resolvedTools, verbatimTools := s.resolvePromoteTools(ctx, module, facts.HarnessTools, req.FilePath)

	// Append diverted-field recoveries to the generic tools list, deduplicating first-seen
	// against entries already resolved from the main tools key.
	seenMain := make(map[string]bool, len(resolvedTools))
	for _, t := range resolvedTools {
		seenMain[t] = true
	}
	for _, t := range divertedResolvedGenericNames {
		if !seenMain[t] {
			seenMain[t] = true
			resolvedTools = append(resolvedTools, t)
		}
	}

	// Recover the generic-only fields the harness dropped at deploy time.
	recovered := s.resolvePromoteGenericFields(ctx, facts, req.FilePath)

	// Assign a unique numeric id: one greater than the largest id in the loaded catalog.
	// This is deterministic for a given catalog state and the test asserts no collision.
	numericID := nextNumericID(s.deps.Catalog)

	// Generate the generic agent bytes from the harness-only source. The generation core
	// is pure: src is not modified and no file is written here.
	genericBytes, err := buildGenericAgent(PromoteInput{
		Source:          src,
		NumericID:       numericID,
		Key:             key,
		Role:            verdict.Meta.Role,
		Version:         verdict.Meta.Version,
		DropKeys:        dropKeys,
		Tools:           resolvedTools,
		RecommendedTier: recovered.RecommendedTier,
		TierRationale:   recovered.TierRationale,
		RequiredSkills:  recovered.RequiredSkills,
	})
	if err != nil {
		return PromoteResult{}, err
	}

	// Write the generated file to the destination, unless this is a dry run. A dry run
	// validates and computes everything but writes no file. DestinationPath is always
	// populated — for dry-run it reflects the path that would have been written.
	if req.DryRun {
		return PromoteResult{
			SourcePath:      req.FilePath,
			DestinationPath: destPath,
			Key:             key,
			NumericID:       numericID,
			Category:        category,
			DryRun:          true,
			HarnessID:       req.HarnessID,
			Tools:           resolvedTools,
			VerbatimTools:   verbatimTools,
			RecoveredFields: recovered.Supplied,
			StrippedFields:  strippedFields,
			DivertedTools:   facts.DivertedTools,
		}, nil
	}

	if mkErr := os.MkdirAll(filepath.Dir(destPath), 0o755); mkErr != nil {
		return PromoteResult{}, fmt.Errorf("creating destination directory: %w", mkErr)
	}
	if writeErr := os.WriteFile(destPath, genericBytes, 0o644); writeErr != nil {
		return PromoteResult{}, fmt.Errorf("writing promoted agent file: %w", writeErr)
	}

	return PromoteResult{
		SourcePath:      req.FilePath,
		DestinationPath: destPath,
		Key:             key,
		NumericID:       numericID,
		Category:        category,
		DryRun:          false,
		HarnessID:       req.HarnessID,
		Tools:           resolvedTools,
		VerbatimTools:   verbatimTools,
		RecoveredFields: recovered.Supplied,
		StrippedFields:  strippedFields,
		DivertedTools:   facts.DivertedTools,
	}, nil
}

// askPromoteCategory asks the user to select a target category for the promoted agent.
// It returns ErrPromoteCategoryRequired when the question is cancelled, skipped, or unanswered.
// Placement is never guessed: any non-Answered outcome becomes a hard error.
func (s *service) askPromoteCategory(ctx context.Context, srcPath string) (string, error) {
	opts, err := s.buildPromoteCategoryOptions()
	if err != nil {
		return "", fmt.Errorf("reading promote category options: %w", err)
	}

	ans, err := s.deps.Interaction.SelectOne(ctx, domain.ChoiceQuestion{
		Question: domain.Question{
			ID:        domain.QPromoteCategory,
			Subject:   srcPath,
			Title:     "Select a target category for the promoted agent",
			AllowSkip: true,
		},
		Options: opts,
	})
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrPromoteCategoryRequired, err)
	}
	if ans.Status != domain.Answered {
		return "", ErrPromoteCategoryRequired
	}
	return ans.OptionID, nil
}

// buildPromoteCategoryOptions returns the SelectOne options for QPromoteCategory.
// Options are derived from the subdirectories present under Subagents/ in the active
// catalogue root, each with Option.ID equal to the directory name, plus one option with
// Option.ID equal to PromoteCategoryUtility. Options are sorted by ID so the presented
// list is deterministic regardless of filesystem ordering.
func (s *service) buildPromoteCategoryOptions() ([]domain.Option, error) {
	agentsDir := catalogpaths.SubagentsDir(s.deps.Catalog.CatalogRoot())
	entries, err := os.ReadDir(agentsDir)
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("reading Subagents/: %w", err)
	}

	var opts []domain.Option
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		opts = append(opts, domain.Option{
			ID:    name,
			Label: name,
		})
	}

	// Add the utility-agents placement option.
	opts = append(opts, domain.Option{
		ID:    PromoteCategoryUtility,
		Label: "Utility Agents (UtilityAgents/)",
	})

	// Sort by ID for deterministic ordering regardless of filesystem readdir order.
	sort.Slice(opts, func(i, j int) bool {
		return opts[i].ID < opts[j].ID
	})

	return opts, nil
}
