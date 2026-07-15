package agenteval

import "time"

type Options struct {
	SourceHome string
	CasesDir   string
	Provider   string
	Model      string
	CaseNames  []string
	Runs       int
	Keep       bool
	Mock       bool
	Now        func() time.Time
}

type Report struct {
	GeneratedAt time.Time `json:"generatedAt"`
	Provider    string    `json:"provider"`
	Model       string    `json:"model"`
	Runs        int       `json:"runs"`
	Cases       int       `json:"cases"`
	Passed      int       `json:"passed"`
	Failed      int       `json:"failed"`
	Results     []Result  `json:"results"`
}

type Result struct {
	Case              string              `json:"case"`
	Run               int                 `json:"run"`
	Description       string              `json:"description,omitempty"`
	SessionID         string              `json:"sessionID,omitempty"`
	TurnID            string              `json:"turnID,omitempty"`
	TurnStatus        string              `json:"turnStatus,omitempty"`
	Passed            bool                `json:"passed"`
	Failure           string              `json:"failure,omitempty"`
	DurationMS        int64               `json:"durationMs"`
	BaselineFailed    bool                `json:"baselineFailed"`
	VerifyPassed      bool                `json:"verifyPassed"`
	VerifyExitCode    int                 `json:"verifyExitCode"`
	VerifyOutput      string              `json:"verifyOutput,omitempty"`
	ChangedPaths      []string            `json:"changedPaths,omitempty"`
	OutOfScopePaths   []string            `json:"outOfScopePaths,omitempty"`
	UnexpectedCommit  bool                `json:"unexpectedCommit,omitempty"`
	ToolCalls         int                 `json:"toolCalls"`
	ToolFailures      int                 `json:"toolFailures"`
	RepeatedToolCalls int                 `json:"repeatedToolCalls"`
	ToolCallSequence  []string            `json:"toolCallSequence,omitempty"`
	ToolFailureDetail []ToolFailureDetail `json:"toolFailureDetails,omitempty"`
	CommandAttempts   []CommandAttempt    `json:"commandAttempts,omitempty"`
	Approval          *ApprovalDetail     `json:"approval,omitempty"`
	AgentRanVerify    bool                `json:"agentRanVerify"`
	FalsePassClaim    bool                `json:"falsePassClaim,omitempty"`
	ProviderRequests  int                 `json:"providerRequests,omitempty"`
	InputTokens       int                 `json:"inputTokens,omitempty"`
	InputUncached     int                 `json:"inputUncachedTokens,omitempty"`
	InputCached       int                 `json:"inputCachedTokens,omitempty"`
	CacheCreation     int                 `json:"cacheCreationTokens,omitempty"`
	OutputTokens      int                 `json:"outputTokens,omitempty"`
	OutputContent     int                 `json:"outputContentTokens,omitempty"`
	OutputReasoning   int                 `json:"outputReasoningTokens,omitempty"`
	FinalResponse     string              `json:"finalResponse,omitempty"`
	FixtureDir        string              `json:"fixtureDir,omitempty"`
}

type ApprovalDetail struct {
	Kind            string   `json:"approvalKind,omitempty"`
	TargetMode      string   `json:"targetMode,omitempty"`
	Title           string   `json:"title,omitempty"`
	Reason          string   `json:"reason,omitempty"`
	Risk            string   `json:"risk,omitempty"`
	ProjectDirs     []string `json:"projectDirs,omitempty"`
	NeedsProjectDir bool     `json:"needsProjectDir,omitempty"`
}

type ToolFailureDetail struct {
	Name     string `json:"name"`
	Reason   string `json:"reason,omitempty"`
	Detail   string `json:"detail,omitempty"`
	Scope    string `json:"scope,omitempty"`
	Path     string `json:"path,omitempty"`
	PathKind string `json:"pathKind,omitempty"`
}

type CommandAttempt struct {
	Command  string `json:"command,omitempty"`
	ExitCode int    `json:"exitCode"`
	Output   string `json:"output,omitempty"`
}
