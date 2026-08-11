package domain

import (
	"context"

	iport "mosaic-common/interaction"
)

// Type aliases for the shared interaction types. These aliases preserve the existing
// API surface of the domain package so all code referencing domain.ChoiceQuestion,
// domain.Answered, etc. continues to compile without modification.

type QuestionID = iport.QuestionID
type AnswerStatus = iport.AnswerStatus

const (
	Answered   = iport.Answered
	SkippedOne = iport.SkippedOne
	SkippedAll = iport.SkippedAll
	Cancelled  = iport.Cancelled
)

type Question = iport.Question
type Option = iport.Option
type ChoiceQuestion = iport.ChoiceQuestion
type ChoiceAnswer = iport.ChoiceAnswer
type MultiChoiceAnswer = iport.MultiChoiceAnswer
type TextQuestion = iport.TextQuestion
type TextAnswer = iport.TextAnswer
type ConfirmAnswer = iport.ConfirmAnswer

type NoticeLevel = iport.NoticeLevel

const (
	NoticeInfo    = iport.NoticeInfo
	NoticeWarning = iport.NoticeWarning
	NoticeError   = iport.NoticeError
)

type Notice = iport.Notice
type ProgressEvent = iport.ProgressEvent

// QuestionID constants specific to the deployment tool.
const (
	QHarness               QuestionID = "harness"
	QMode                  QuestionID = "mode"
	QWorkspace             QuestionID = "workspace"
	QWorkflows             QuestionID = "workflows"
	QUtilityAgents         QuestionID = "utility-agents"
	QInfrastructureAgents  QuestionID = "infrastructure-agents"
	QHooks                 QuestionID = "hooks"
	QTierModel             QuestionID = "tier-model"
	QAgentModel            QuestionID = "agent-model"
	QCustomModel           QuestionID = "custom-model"
	QCustomTool            QuestionID = "custom-tool"
	QLocalModification     QuestionID = "local-modification"
	QPlanConfirm           QuestionID = "plan-confirm"
	QExternalOptIn         QuestionID = "external-opt-in"
	// QHarnessOnlyRefreshScope is asked once per harness-only agent during an Update run
	// when at least one eligible agent with no generic counterpart is discovered. It
	// presents two scope options and their apply-to-all variants, mirroring the
	// askLocalModification convention.
	QHarnessOnlyRefreshScope QuestionID = "harness-only-refresh-scope"
	// QPromoteCategory is asked when Service.Promote is called without a pre-answered
	// category. The options are derived from the subdirectories present under
	// Agents/Generic/Agents/ plus the UtilityAgents sentinel.
	QPromoteCategory QuestionID = "promote-category"

	// QPromoteCustomTool is asked once per distinct harness-side tool entry that has no
	// reverse mapping to a generic tool. It is the inverse of QCustomTool: that question
	// asks for a harness-side name given a generic tool, this one asks for a generic name
	// given a harness-side tool. Subject is the harness-side tool name. It is a text
	// question and therefore reaches the existing TextPromptScreen overlay with no TUI
	// routing change.
	QPromoteCustomTool QuestionID = "promote-custom-tool"

	// QPromoteRecommendedTier is asked during promote when the deployed file carries no
	// recommended_tier. It is a choice question over the catalog's discovered tiers, with
	// a custom entry allowed. Subject is the source file path.
	QPromoteRecommendedTier QuestionID = "promote-recommended-tier"

	// QPromoteTierRationale is asked during promote when the deployed file carries no
	// tier_rationale. Text question; Subject is the source file path.
	QPromoteTierRationale QuestionID = "promote-tier-rationale"

	// QPromoteRequiredSkills is asked during promote when the deployed file carries no
	// required_skills. Text question taking a comma-separated list; Subject is the source
	// file path.
	QPromoteRequiredSkills QuestionID = "promote-required-skills"

	// QTransformTargetHarness asks which harness to transform the input agents into.
	// Answer: one harness id from ListHarnesses, excluding the selected source harness.
	QTransformTargetHarness QuestionID = "transform-target-harness"

	// QTransformTargetModel asks which target-harness model to write into the transformed
	// agents. Skippable: a skipped or empty answer leaves the model field empty in every
	// output file. Answer: one model id from the target descriptor's ModelCatalog, or a
	// free-text id when the harness declares none.
	QTransformTargetModel QuestionID = "transform-target-model"
)

// PlanReviewer is the deployment-specific interface for plan review. The shared Interaction
// port does not include Review; this interface captures the deployment-only Review method so
// that usage sites can declare their dependency precisely.
type PlanReviewer interface {
	Review(ctx context.Context, p Plan) (ConfirmAnswer, error)
}

// Interaction is the full interaction interface used by the deployment tool. It embeds the
// shared harness-neutral port and adds the deployment-specific PlanReviewer so that the
// app service can call Review without widening the shared port.
type Interaction interface {
	iport.Interaction
	PlanReviewer
}
