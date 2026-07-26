package domain

import "context"

// QuestionID is a stable identifier for each kind of question the app flow can ask.
// The app uses these to latch SkippedAll and to look up pre-supplied answers (CD-4, CD-6).
type QuestionID string

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

// AnswerStatus records how a question was resolved.
type AnswerStatus string

const (
	Answered   AnswerStatus = "answered"
	SkippedOne AnswerStatus = "skipped"
	SkippedAll AnswerStatus = "skipped-all"
	Cancelled  AnswerStatus = "cancelled" // user aborted the run
)

// Question carries the display fields shared by all four question shapes.
type Question struct {
	ID           QuestionID
	Subject      string // tier name, agent key, tool name, file path; "" for run-level questions
	Title        string
	Prompt       string
	Detail       string // long-form context, e.g. a tier's rationale text
	AllowSkip    bool
	AllowSkipAll bool
	// AllowCustom offers free-text entry alongside the option list on a choice question.
	AllowCustom     bool
	CustomPrompt    string
	DefaultOptionID string
}

// Option is one selectable choice in a ChoiceQuestion.
type Option struct {
	ID             string
	Label          string
	Description    string
	Hint           string
	Group          string // e.g. workflow category, for grouped browsing
	Disabled       bool
	DisabledReason string // e.g. "no stored mapping for this tier"
}

// ChoiceQuestion is a select-one or select-many question with a fixed set of options.
type ChoiceQuestion struct {
	Question
	Options []Option
}

// ChoiceAnswer is the user's response to a SelectOne question.
type ChoiceAnswer struct {
	Status   AnswerStatus
	OptionID string // set when Status == Answered and Custom is empty
	Custom   string // set when the user chose free-text entry
}

// MultiChoiceAnswer is the user's response to a SelectMany question.
type MultiChoiceAnswer struct {
	Status    AnswerStatus
	OptionIDs []string
}

// TextQuestion is a free-text input question, optionally multiline.
type TextQuestion struct {
	Question
	Default   string
	Multiline bool
	// Validate is supplied by the caller so both frontends enforce identical validation rules.
	Validate func(string) error
}

// TextAnswer is the user's response to an AskText question.
type TextAnswer struct {
	Status AnswerStatus
	Text   string
}

// ConfirmAnswer is the user's response to a yes/no Confirm question.
type ConfirmAnswer struct {
	Status  AnswerStatus
	Confirm bool
}

// NoticeLevel classifies the severity of a notification shown to the user.
type NoticeLevel string

const (
	NoticeInfo    NoticeLevel = "info"
	NoticeWarning NoticeLevel = "warning"
	NoticeError   NoticeLevel = "error"
)

// Notice is a one-way message shown to the user that requires no response.
type Notice struct {
	Level   NoticeLevel
	Title   string
	Message string
}

// ProgressEvent reports incremental progress through a long-running phase.
type ProgressEvent struct {
	Phase   string // "planning", "transforming", "writing"
	Current int
	Total   int
	Subject string
}

// Interaction is the only channel through which a use case may consult a user.
// Implementations: tui (interactive prompts), cli (resolve from flags/config, else skip).
// No implementation may block indefinitely; the CLI implementation must never block at all.
// Notify and Progress never fail and never block.
type Interaction interface {
	SelectOne(ctx context.Context, q ChoiceQuestion) (ChoiceAnswer, error)
	SelectMany(ctx context.Context, q ChoiceQuestion) (MultiChoiceAnswer, error)
	AskText(ctx context.Context, q TextQuestion) (TextAnswer, error)
	Confirm(ctx context.Context, q Question) (ConfirmAnswer, error)
	Notify(ctx context.Context, n Notice)
	Progress(ctx context.Context, e ProgressEvent)
	// Review shows a full plan and returns the user's go/no-go decision.
	Review(ctx context.Context, p Plan) (ConfirmAnswer, error)
}
