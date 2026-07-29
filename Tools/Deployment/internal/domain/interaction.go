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
	QHarness           QuestionID = "harness"
	QMode              QuestionID = "mode"
	QWorkspace         QuestionID = "workspace"
	QWorkflows         QuestionID = "workflows"
	QUtilityAgents     QuestionID = "utility-agents"
	QHooks             QuestionID = "hooks"
	QTierModel         QuestionID = "tier-model"
	QAgentModel        QuestionID = "agent-model"
	QCustomModel       QuestionID = "custom-model"
	QCustomTool        QuestionID = "custom-tool"
	QLocalModification QuestionID = "local-modification"
	QPlanConfirm       QuestionID = "plan-confirm"
	QExternalOptIn     QuestionID = "external-opt-in"
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
