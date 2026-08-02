package app

// resolve.go implements the shared resolvers used by both use cases: harness/workspace/
// workflow/utility-agent/hook selection, tier and per-agent model resolution, and custom
// tool name resolution. Every resolver either uses a pre-answered request field (CD-6) or
// consults domain.Interaction exactly once per decision point.

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/transform"
)

// ---------------------------------------------------------------------------
// Harness, workspace, workflow, utility-agent, and hook selection
// ---------------------------------------------------------------------------

// askHarness prompts for a harness selection from the registry's known harnesses.
func (s *service) askHarness(ctx context.Context) (string, error) {
	opts := make([]domain.Option, 0)
	for _, h := range s.deps.Registry.List() {
		opts = append(opts, domain.Option{
			ID: h.ID, Label: h.DisplayName, Disabled: !h.Usable, DisabledReason: h.UnusableReason,
		})
	}
	q := domain.ChoiceQuestion{
		Question: domain.Question{ID: domain.QHarness, Title: "Select a harness"},
		Options:  opts,
	}
	ans, err := s.deps.Interaction.SelectOne(ctx, q)
	if err != nil {
		return "", err
	}
	if ans.Status == domain.Cancelled {
		return "", errors.New("harness selection was cancelled")
	}
	id := ans.OptionID
	if id == "" {
		id = ans.Custom
	}
	if id == "" {
		return "", errors.New("no harness was selected")
	}
	return id, nil
}

// stripMatchedOuterQuotes removes a single surrounding pair of double-quote characters when
// both the first and last characters are double quotes. It strips exactly one outer pair; an
// unmatched quote or multiple nested pairs are left unchanged. The input must already have
// surrounding whitespace removed.
func stripMatchedOuterQuotes(s string) string {
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		return s[1 : len(s)-1]
	}
	return s
}

// askWorkspace prompts for the target workspace path.
func (s *service) askWorkspace(ctx context.Context) (string, error) {
	q := domain.TextQuestion{
		Question: domain.Question{ID: domain.QWorkspace, Title: "Target workspace path"},
	}
	ans, err := s.deps.Interaction.AskText(ctx, q)
	if err != nil {
		return "", err
	}
	if ans.Status != domain.Answered {
		return "", errors.New("no workspace path was provided")
	}
	text := stripMatchedOuterQuotes(strings.TrimSpace(ans.Text))
	if text == "" {
		return "", errors.New("no workspace path was provided")
	}
	return text, nil
}

// askWorkflows prompts for workflow selection, grouped by category for browsable display.
func (s *service) askWorkflows(ctx context.Context) []string {
	opts := make([]domain.Option, 0)
	for _, cat := range s.deps.Catalog.WorkflowCategories() {
		for _, wf := range cat.Workflows {
			opts = append(opts, domain.Option{
				ID: wf.ID, Label: wf.Name, Description: wf.Description, Hint: wf.Hint, Group: cat.Name,
			})
		}
	}
	q := domain.ChoiceQuestion{
		Question: domain.Question{ID: domain.QWorkflows, Title: "Select workflows", AllowSkip: true, AllowSkipAll: true},
		Options:  opts,
	}
	ans, err := s.deps.Interaction.SelectMany(ctx, q)
	if err != nil || ans.Status != domain.Answered {
		return []string{}
	}
	return ans.OptionIDs
}

// askUtilityAgents prompts for utility-agent selection, restricted to the tool config's
// allow-list (FR-8).
func (s *service) askUtilityAgents(ctx context.Context) []string {
	cfg, _ := s.deps.ToolConfig.Load()
	allowed := make(map[string]bool, len(cfg.UtilityAgentAllowList))
	for _, k := range cfg.UtilityAgentAllowList {
		allowed[k] = true
	}
	opts := make([]domain.Option, 0)
	for _, a := range s.deps.Catalog.UtilityAgents() {
		if !allowed[a.Key] {
			continue
		}
		opts = append(opts, domain.Option{ID: a.Key, Label: a.Name, Description: a.Description})
	}
	q := domain.ChoiceQuestion{
		Question: domain.Question{ID: domain.QUtilityAgents, Title: "Select utility agents", AllowSkip: true, AllowSkipAll: true},
		Options:  opts,
	}
	ans, err := s.deps.Interaction.SelectMany(ctx, q)
	if err != nil || ans.Status != domain.Answered {
		return []string{}
	}
	return ans.OptionIDs
}

// askInfrastructureAgents prompts for infrastructure-agent selection. All agents with a
// non-empty Infrastructure field from the catalog are presented. Unlike utility agents,
// infrastructure agents are not gated by an allow-list. Multiple agents of the same class
// may be selected without restriction.
func (s *service) askInfrastructureAgents(ctx context.Context) []string {
	opts := make([]domain.Option, 0)
	for _, a := range s.deps.Catalog.InfrastructureAgents() {
		label := a.Name
		if label == "" {
			label = a.Key
		}
		label = label + " [" + a.Infrastructure + "]"
		opts = append(opts, domain.Option{
			ID:          a.Key,
			Label:       label,
			Description: a.Description,
		})
	}
	q := domain.ChoiceQuestion{
		Question: domain.Question{
			ID:           domain.QInfrastructureAgents,
			Title:        "Select infrastructure agents",
			AllowSkip:    true,
			AllowSkipAll: true,
		},
		Options: opts,
	}
	ans, err := s.deps.Interaction.SelectMany(ctx, q)
	if err != nil || ans.Status != domain.Answered {
		return []string{}
	}
	return ans.OptionIDs
}

// askHooks prompts for hook bundle selection from every bundle the catalog knows about.
func (s *service) askHooks(ctx context.Context) []string {
	opts := make([]domain.Option, 0)
	for _, h := range s.deps.Catalog.Hooks() {
		opts = append(opts, domain.Option{ID: h.Key, Label: h.Key, Description: h.Description})
	}
	q := domain.ChoiceQuestion{
		Question: domain.Question{ID: domain.QHooks, Title: "Select hook bundles", AllowSkip: true, AllowSkipAll: true},
		Options:  opts,
	}
	ans, err := s.deps.Interaction.SelectMany(ctx, q)
	if err != nil || ans.Status != domain.Answered {
		return []string{}
	}
	return ans.OptionIDs
}

// ---------------------------------------------------------------------------
// Tier discovery and per-agent model resolution
// ---------------------------------------------------------------------------

// modelResolution is the outcome of resolveModels: the per-agent model map ready for
// plan.Input.Models, the tier-to-model mappings used this run, and accumulated state
// that can be threaded into a subsequent resolveModels invocation.
type modelResolution struct {
	models         map[string]domain.ModelSelection
	tierModelsUsed map[domain.Tier]string

	// accumulatedOptions holds every custom model ID that was entered during this
	// resolveModels invocation (at both tier and agent granularity), merged with any
	// custom IDs that were passed in via extraOptions. These are domain.Option values
	// ready to be appended to harnessOptions in a subsequent invocation.
	accumulatedOptions []domain.Option

	// tierSkippedAll is true when the user chose SkippedAll for QTierModel during
	// this invocation, or it was already true on entry.
	tierSkippedAll bool

	// agentSkippedAll is true when the user chose SkippedAll for QAgentModel during
	// this invocation, or it was already true on entry.
	agentSkippedAll bool
}

// resolveModels asks QTierModel once per discovered tier and QAgentModel once per agent
// whose model could not be resolved from a tier mapping, honouring pre-answers, skip, and
// skip-all. Custom model IDs entered during the invocation are appended to harnessOptions
// in-place so subsequent agents in the same call see them. All accumulated state is returned
// in modelResolution for optional downstream threading.
//
// extraOptions, if non-nil, contains custom model options accumulated from a prior invocation.
// They are appended to harnessOptions (after deduplication) so custom entries from a previous
// batch appear as selectable options.
//
// tierSkipOverride and agentSkipOverride, when true, pre-set the skip-all latches for
// QTierModel and QAgentModel respectively. This lets a caller suppress interactive model
// questions without setting SkipAll in the request.
func (s *service) resolveModels(
	ctx context.Context,
	tierModels map[domain.Tier]string,
	agentModels map[string]string,
	skipAll map[domain.QuestionID]bool,
	harnessID string,
	module domain.HarnessModule,
	agents []domain.Agent,
	extraOptions []domain.Option,
	tierSkipOverride bool,
	agentSkipOverride bool,
) modelResolution {
	cfg, _ := s.deps.UserConfig.Load()
	tierModelsUsed := make(map[domain.Tier]string)
	for k, v := range cfg.TierModels[harnessID] {
		tierModelsUsed[k] = v
	}
	for k, v := range tierModels {
		tierModelsUsed[k] = v
	}

	harnessOptions := buildModelOptions(module.Descriptor().Models.IDs)

	// Track which option IDs are already in harnessOptions to enable deduplication.
	optionSeen := make(map[string]bool, len(harnessOptions))
	for _, opt := range harnessOptions {
		optionSeen[opt.ID] = true
	}

	// Load persisted custom model IDs for this harness and merge into harnessOptions.
	for _, id := range cfg.CustomModelIDs[harnessID] {
		if !optionSeen[id] {
			optionSeen[id] = true
			harnessOptions = append(harnessOptions, domain.Option{ID: id, Label: id})
		}
	}

	// Merge extraOptions from a prior invocation (deduplicated).
	for _, opt := range extraOptions {
		if !optionSeen[opt.ID] {
			optionSeen[opt.ID] = true
			harnessOptions = append(harnessOptions, opt)
		}
	}

	// accumulatedCustomIDs tracks the custom IDs entered in this invocation for deduplication.
	accumulatedCustomIDs := make(map[string]bool)
	var accumulatedOptions []domain.Option

	// appendCustomID adds a custom model ID to harnessOptions and accumulatedOptions if
	// not already present, so subsequent questions within this invocation see it.
	appendCustomID := func(id string) {
		if !optionSeen[id] {
			optionSeen[id] = true
			harnessOptions = append(harnessOptions, domain.Option{ID: id, Label: id})
		}
		if !accumulatedCustomIDs[id] {
			accumulatedCustomIDs[id] = true
			accumulatedOptions = append(accumulatedOptions, domain.Option{ID: id, Label: id})
		}
	}

	tierSkippedAll := skipAll[domain.QTierModel] || tierSkipOverride
	for _, ti := range s.deps.Catalog.Tiers() {
		if _, ok := tierModelsUsed[ti.Tier]; ok {
			continue // pre-answered, either from this request or a prior persisted run
		}
		if tierSkippedAll {
			continue
		}
		q := domain.ChoiceQuestion{
			Question: domain.Question{
				ID: domain.QTierModel, Subject: string(ti.Tier),
				Title: "Select a model for tier " + string(ti.Tier), Detail: ti.Rationale,
				AllowSkip: true, AllowSkipAll: true, AllowCustom: true,
			},
			Options: harnessOptions,
		}
		ans, err := s.deps.Interaction.SelectOne(ctx, q)
		if err != nil {
			continue
		}
		switch ans.Status {
		case domain.SkippedAll:
			tierSkippedAll = true
		case domain.Answered:
			modelID := ans.OptionID
			if ans.Custom != "" {
				modelID = ans.Custom
				appendCustomID(modelID)
			}
			if modelID != "" {
				tierModelsUsed[ti.Tier] = modelID
			}
		}
	}

	models := make(map[string]domain.ModelSelection)
	agentSkippedAll := skipAll[domain.QAgentModel] || agentSkipOverride
	for _, agent := range agents {
		if modelID, ok := agentModels[agent.Key]; ok && modelID != "" {
			origin := domain.OriginCustom
			for _, opt := range harnessOptions {
				if opt.ID == modelID {
					origin = domain.OriginHarnessList
					break
				}
			}
			models[agent.Key] = domain.ModelSelection{ModelID: modelID, Origin: origin}
		} else if tierModel, ok := tierModelsUsed[agent.RecommendedTier]; ok && tierModel != "" {
			models[agent.Key] = domain.ModelSelection{ModelID: tierModel, Origin: domain.OriginTierMapping}
		} else if !agentSkippedAll {
			options := make([]domain.Option, 0, len(harnessOptions)+1)
			options = append(options, domain.Option{
				ID: "apply-tier-mapping", Label: "Apply stored tier mapping",
				Disabled:       true,
				DisabledReason: fmt.Sprintf("no stored mapping for tier %q", agent.RecommendedTier),
			})
			options = append(options, harnessOptions...)
			q := domain.ChoiceQuestion{
				Question: domain.Question{
					ID: domain.QAgentModel, Subject: agent.Key,
					Title: "Select a model for " + agent.Key,
					AllowSkip: true, AllowSkipAll: true, AllowCustom: true,
				},
				Options: options,
			}
			ans, err := s.deps.Interaction.SelectOne(ctx, q)
			if err == nil {
				switch ans.Status {
				case domain.SkippedAll:
					agentSkippedAll = true
				case domain.Answered:
					modelID := ans.OptionID
					origin := domain.OriginHarnessList
					if ans.Custom != "" {
						modelID = ans.Custom
						origin = domain.OriginCustom
						appendCustomID(modelID)
					}
					if modelID != "" {
						models[agent.Key] = domain.ModelSelection{ModelID: modelID, Origin: origin}
					}
				}
			}
		}
		// GapNoModel gaps for unresolved agents are emitted solely by plan.Build, which
		// also populates Plan.Gaps (rendered by the Review screen). Emitting them here
		// would produce duplicates — one from plan.Gaps copied into the collector and a
		// second one from this loop.
	}

	return modelResolution{
		models:             models,
		tierModelsUsed:     tierModelsUsed,
		accumulatedOptions: accumulatedOptions,
		tierSkippedAll:     tierSkippedAll,
		agentSkippedAll:    agentSkippedAll,
	}
}

// buildModelOptions converts a harness's declared model list into ChoiceQuestion options.
func buildModelOptions(ids []string) []domain.Option {
	opts := make([]domain.Option, 0, len(ids))
	for _, id := range ids {
		opts = append(opts, domain.Option{ID: id, Label: id})
	}
	return opts
}

// persistCustomModelIDs merges the custom model IDs accumulated during this run into
// the persisted user config's CustomModelIDs list for the given harness, then saves.
// Deduplication is by exact string match. Existing persisted IDs that are not in the
// current run's set are preserved (the list is append-only across runs).
func (s *service) persistCustomModelIDs(harnessID string, customIDs []string) error {
	cfg, err := s.deps.UserConfig.Load()
	if err != nil {
		return err
	}
	if cfg.CustomModelIDs == nil {
		cfg.CustomModelIDs = make(map[string][]string)
	}
	existing := cfg.CustomModelIDs[harnessID]
	seen := make(map[string]bool, len(existing)+len(customIDs))
	merged := make([]string, 0, len(existing)+len(customIDs))
	for _, id := range existing {
		if !seen[id] {
			seen[id] = true
			merged = append(merged, id)
		}
	}
	for _, id := range customIDs {
		if !seen[id] {
			seen[id] = true
			merged = append(merged, id)
		}
	}
	cfg.CustomModelIDs[harnessID] = merged
	return s.deps.UserConfig.Save(cfg)
}

// persistTierModels merges the tier mappings used this run into the persisted user config
// and saves it, keyed by harness id (AC18.3). Per-agent selections are never passed here.
func (s *service) persistTierModels(harnessID string, tierModelsUsed map[domain.Tier]string) error {
	cfg, err := s.deps.UserConfig.Load()
	if err != nil {
		return err
	}
	if cfg.TierModels == nil {
		cfg.TierModels = make(map[string]map[domain.Tier]string)
	}
	merged := make(map[domain.Tier]string, len(tierModelsUsed))
	for k, v := range cfg.TierModels[harnessID] {
		merged[k] = v
	}
	for k, v := range tierModelsUsed {
		merged[k] = v
	}
	cfg.TierModels[harnessID] = merged
	return s.deps.UserConfig.Save(cfg)
}

// ---------------------------------------------------------------------------
// Custom tool name resolution
// ---------------------------------------------------------------------------

// resolveCustomTools discovers every generic tool with no harness mapping across the given
// agents, asks QCustomTool once per distinct unmapped tool (honouring pre-answers, skip, and
// skip-all), and returns the resolved custom names plus the set of tools the user skipped.
// Every skipped tool produces a GapUnmappedTool entry (AC18.5).
func (s *service) resolveCustomTools(
	ctx context.Context,
	preAnswered map[string]string,
	skipAll map[domain.QuestionID]bool,
	module domain.HarnessModule,
	agents []domain.Agent,
) (map[string]string, map[string]bool) {
	customNames := make(map[string]string, len(preAnswered))
	for k, v := range preAnswered {
		customNames[k] = v
	}
	skipped := make(map[string]bool)

	var unmappedOrder []string
	seen := make(map[string]bool)
	for _, agent := range agents {
		if len(agent.Tools) == 0 {
			continue
		}
		result, err := module.Tools(domain.ToolRequest{
			AgentKey: agent.Key, Generic: agent.Tools, Placeholder: agent.ToolsPlaceholder,
		})
		if err != nil {
			continue
		}
		for _, res := range result.Resolutions {
			if res.Outcome == domain.ToolUnmapped && !seen[res.Generic] {
				seen[res.Generic] = true
				unmappedOrder = append(unmappedOrder, res.Generic)
			}
		}
	}

	skippedAll := skipAll[domain.QCustomTool]
	for _, toolName := range unmappedOrder {
		if _, ok := customNames[toolName]; ok {
			continue // pre-answered
		}
		if skippedAll {
			skipped[toolName] = true
			continue
		}
		q := domain.TextQuestion{
			Question: domain.Question{
				ID: domain.QCustomTool, Subject: toolName,
				Title: "Custom MCP server name for " + toolName,
				AllowSkip: true, AllowSkipAll: true,
			},
		}
		ans, err := s.deps.Interaction.AskText(ctx, q)
		if err != nil {
			skipped[toolName] = true
			continue
		}
		switch ans.Status {
		case domain.SkippedAll:
			skippedAll = true
			skipped[toolName] = true
		case domain.Answered:
			if ans.Text != "" {
				customNames[toolName] = ans.Text
			} else {
				skipped[toolName] = true
			}
		default:
			skipped[toolName] = true
		}
	}

	// Re-resolve with the final custom-name and skip maps so the harness module records the
	// propagated custom names (used by content generation during deployment).
	for _, agent := range agents {
		if len(agent.Tools) == 0 {
			continue
		}
		_, _ = module.Tools(domain.ToolRequest{
			AgentKey: agent.Key, Generic: agent.Tools, Placeholder: agent.ToolsPlaceholder,
			CustomNames: customNames, SkippedTools: skipped,
		})
	}

	// Unmapped-tool gaps are emitted solely by plan.Build, which names the owning agent and
	// also fires in the update flow. Emitting them here would produce duplicates.

	return customNames, skipped
}

// ---------------------------------------------------------------------------
// Locally-modified file prompting (update flow)
// ---------------------------------------------------------------------------

// askLocalModification prompts for the per-file decision on a locally-modified artifact.
// When applyToAll is true in the returned values, the caller must use the returned decision
// for all remaining conflict items without asking again.
//
// The question presents six options: three per-file decisions (Overwrite, Skip,
// Backup-then-overwrite) and three apply-to-all variants whose Option.ID carries the
// prefix "all:" (e.g., "all:overwrite"). When the user selects a compound option the answer
// arrives as Answered with the compound OptionID; the prefix is stripped to extract the
// ConflictDecision and applyToAll is returned as true.
//
// Any non-Answered outcome (SkippedOne, Cancelled, error) defaults to DecisionSkip with
// applyToAll=false, the safest choice: it never discards local edits without explicit consent.
func (s *service) askLocalModification(ctx context.Context, item domain.PlanItem) (decision domain.ConflictDecision, applyToAll bool) {
	opts := []domain.Option{
		{ID: string(domain.DecisionOverwrite), Label: "Overwrite"},
		{ID: string(domain.DecisionSkip), Label: "Skip"},
		{ID: string(domain.DecisionBackupThenOverwrite), Label: "Back up then overwrite"},
		{ID: "all:overwrite", Label: "Overwrite all remaining", Group: "Apply to all"},
		{ID: "all:skip", Label: "Skip all remaining", Group: "Apply to all"},
		{ID: "all:backup-then-overwrite", Label: "Back up then overwrite all remaining", Group: "Apply to all"},
	}
	q := domain.ChoiceQuestion{
		Question: domain.Question{
			ID: domain.QLocalModification, Subject: item.TargetPath,
			Title: "Locally modified file: " + item.TargetPath, AllowSkip: true,
		},
		Options: opts,
	}
	ans, err := s.deps.Interaction.SelectOne(ctx, q)
	if err != nil || ans.Status != domain.Answered {
		return domain.DecisionSkip, false
	}
	optID := ans.OptionID
	if strings.HasPrefix(optID, "all:") {
		remainder := strings.TrimPrefix(optID, "all:")
		d := domain.ConflictDecision(remainder)
		if d == "" {
			d = domain.DecisionSkip
		}
		return d, true
	}
	if d := domain.ConflictDecision(optID); d != "" {
		return d, false
	}
	return domain.DecisionSkip, false
}

// ---------------------------------------------------------------------------
// Content generation and supporting helpers
// ---------------------------------------------------------------------------

// buildWorkflowBlocks assembles the WorkflowBlock list for the orchestrator's
// AvailableWorkflows injection from the catalog's per-workflow section content.
func (s *service) buildWorkflowBlocks(workflowIDs []string) []transform.WorkflowBlock {
	blocks := make([]transform.WorkflowBlock, 0, len(workflowIDs))
	for _, id := range workflowIDs {
		b, err := s.deps.Catalog.WorkflowSection(id)
		if err != nil {
			continue
		}
		blocks = append(blocks, transform.WorkflowBlock{ID: id, Block: b})
	}
	return blocks
}

// buildInfrastructureBlocks assembles the InfrastructureBlock list for the orchestrator's
// InfrastructureAgents injection from the catalog's infrastructure agent metadata. Agents
// that are not found in the catalog (e.g. unknown keys) are silently skipped.
func (s *service) buildInfrastructureBlocks(infraAgentIDs []string) []transform.InfrastructureBlock {
	blocks := make([]transform.InfrastructureBlock, 0, len(infraAgentIDs))
	for _, id := range infraAgentIDs {
		a, ok := s.deps.Catalog.Agent(id)
		if !ok {
			continue
		}
		triggers := make([]domain.InfrastructureTrigger, len(a.Triggers))
		copy(triggers, a.Triggers)
		blocks = append(blocks, transform.InfrastructureBlock{
			Key:         a.Key,
			Version:     a.Version,
			Class:       a.Infrastructure,
			Description: a.Description,
			OnFailure:   a.OnFailure,
			Triggers:    triggers,
		})
	}
	return blocks
}

// buildContent returns the deploy.ExecRequest.Content callback for one run. Agent items are
// rendered through transform.Apply; skill items are copied verbatim from source. Hook items
// bypass this callback entirely and are handled by deployHooks.
func (s *service) buildContent(
	module domain.HarnessModule,
	agentByKey map[string]domain.Agent,
	models map[string]domain.ModelSelection,
	customTools map[string]string,
	skippedTools map[string]bool,
	workflowBlocks []transform.WorkflowBlock,
	infrastructureBlocks []transform.InfrastructureBlock,
	scope domain.Scope,
	deployedReader func(domain.PlanItem) []byte,
) func(domain.PlanItem) ([]byte, error) {
	return func(item domain.PlanItem) ([]byte, error) {
		if item.Ref.Kind != domain.ArtifactAgent {
			return s.deps.Catalog.ReadSource(item.SourcePath)
		}
		agent, ok := agentByKey[item.Ref.Key]
		if !ok {
			return nil, fmt.Errorf("unknown agent %q", item.Ref.Key)
		}
		src, err := s.deps.Catalog.ReadSource(agent.SourcePath)
		if err != nil {
			return nil, err
		}
		var deployed []byte
		if deployedReader != nil {
			deployed = deployedReader(item)
		}
		var wfBlocks []transform.WorkflowBlock
		var infraBlocks []transform.InfrastructureBlock
		if agent.Role == domain.RoleOrchestrator {
			wfBlocks = workflowBlocks
			infraBlocks = infrastructureBlocks
		}
		res, err := transform.Apply(transform.Request{
			Source: src, Kind: domain.ArtifactAgent, Key: agent.Key, Module: module,
			Model: models[agent.Key], CustomTools: customTools, SkippedTools: skippedTools,
			Scope: scope, Deployed: deployed, Workflows: wfBlocks,
			InfrastructureAgents: infraBlocks,
		})
		if err != nil {
			return nil, err
		}
		return res.Output, nil
	}
}

// buildVersionStamps derives the version stamps for every plan item from the resolved
// artifact set and harness descriptor, keyed by target path (as deploy.ExecRequest requires).
func buildVersionStamps(
	agents []domain.Agent, skills []domain.Skill, hooks []domain.HookBundle,
	items []domain.PlanItem, desc *domain.HarnessDescriptor,
) map[string]domain.VersionStamp {
	agentByKey := make(map[string]domain.Agent, len(agents))
	for _, a := range agents {
		agentByKey[a.Key] = a
	}
	skillByKey := make(map[string]domain.Skill, len(skills))
	for _, sk := range skills {
		skillByKey[sk.Key] = sk
	}
	hookByKey := make(map[string]domain.HookBundle, len(hooks))
	for _, h := range hooks {
		hookByKey[h.Key] = h
	}

	stamps := make(map[string]domain.VersionStamp, len(items))
	for _, item := range items {
		switch item.Ref.Kind {
		case domain.ArtifactAgent:
			if a, ok := agentByKey[item.Ref.Key]; ok {
				stamp := domain.VersionStamp{
					Version: a.Version, TransformVersion: desc.TransformVersion, InjectionsVersion: desc.InjectionsVersion,
				}
				if a.Role == domain.RoleOrchestrator {
					stamp.OrchestratorInjectionsVersion = desc.OrchestratorInjectionsVersion
				}
				stamps[item.TargetPath] = stamp
			}
		case domain.ArtifactSkill:
			if sk, ok := skillByKey[item.Ref.Key]; ok {
				stamps[item.TargetPath] = domain.VersionStamp{Version: sk.Version}
			}
		case domain.ArtifactHook:
			if h, ok := hookByKey[item.Ref.Key]; ok {
				stamps[item.TargetPath] = domain.VersionStamp{Version: h.Version}
			}
		}
	}
	return stamps
}

// buildHookPlans resolves every selected hook bundle against the harness module.
func buildHookPlans(module domain.HarnessModule, hooks []domain.HookBundle, scope domain.Scope) []domain.HookPlan {
	plans := make([]domain.HookPlan, 0, len(hooks))
	for _, h := range hooks {
		p, err := module.HookPlan(domain.HookPlanRequest{Bundle: h, Scope: scope})
		if err == nil {
			plans = append(plans, p)
		}
	}
	return plans
}

// ---------------------------------------------------------------------------
// Update-flow workflow discovery
// ---------------------------------------------------------------------------

// unionPreserveOrder returns the deduplicated union of a and b, preserving first-occurrence
// order with a's elements first.
func unionPreserveOrder(a, b []string) []string {
	seen := make(map[string]bool, len(a)+len(b))
	out := make([]string, 0, len(a)+len(b))
	for _, id := range a {
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	for _, id := range b {
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
}
