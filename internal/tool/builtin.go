package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/teatak/pudding-core/internal/app"
	"github.com/teatak/pudding-core/internal/browser"
	"github.com/teatak/pudding-core/internal/lsp"
	"github.com/teatak/pudding-core/internal/provider"
	"github.com/teatak/pudding-core/internal/skill"
	"github.com/teatak/pudding-core/internal/store"
)

const (
	TimeGetCurrent      = "builtin_time_get_current"
	WebSearch           = "builtin_web_search"
	WebFetch            = "builtin_web_fetch"
	HistorySearch       = "builtin_history_search"
	HistoryGetMessage   = "builtin_history_get_message"
	SkillRead           = "builtin_skill_read"
	ProjectInspect      = "builtin_project_inspect"
	ProjectInstructions = "builtin_project_instructions"
	CodeSymbols         = "builtin_code_symbols"
	CodeDefinition      = "builtin_code_definition"
	CodeReferences      = "builtin_code_references"
	CodeDiagnostics     = "builtin_code_diagnostics"
	FileList            = "builtin_file_list"
	FileRead            = "builtin_file_read"
	AttachmentReadImage = "builtin_attachment_read_image"
	FileStat            = "builtin_file_stat"
	FileSearch          = "builtin_file_search"
	FileSlice           = "builtin_file_slice"
	FileWrite           = "builtin_file_write"
	FilePatch           = "builtin_file_patch"
	FileDelete          = "builtin_file_delete"
	FileMove            = "builtin_file_move"
	FileCopy            = "builtin_file_copy"
	CommandRun          = "builtin_command_run"
	GitStatus           = "builtin_git_status"
	GitDiff             = "builtin_git_diff"
	GitLog              = "builtin_git_log"
	GitStage            = "builtin_git_stage"
	GitUnstage          = "builtin_git_unstage"
	GitCommit           = "builtin_git_commit"
	PatchPropose        = "builtin_patch_propose"
	PatchApply          = "builtin_patch_apply"
	SkillValidate       = "builtin_skill_validate"
	SkillSubmit         = "builtin_skill_submit"
	RESTRequest         = "builtin_rest_request"
	GraphQLRequest      = "builtin_graphql_request"
	GraphQLIntrospect   = "builtin_graphql_introspect"
	GraphQLSearch       = "builtin_graphql_search"
	WeatherGet          = "builtin_weather_get"
	CameraCapture       = "builtin_camera_capture"
	DesktopScreenshot   = "builtin_desktop_screenshot"
	BrowserStatus       = "builtin_browser_status"
	BrowserOpen         = "builtin_browser_open"
	BrowserObserve      = "builtin_browser_observe"
	BrowserScreenshot   = "builtin_browser_screenshot"
	BrowserBack         = "builtin_browser_back"
	BrowserForward      = "builtin_browser_forward"
	BrowserReload       = "builtin_browser_reload"
	BrowserClose        = "builtin_browser_close"
	BrowserClick        = "builtin_browser_click"
	BrowserType         = "builtin_browser_type"
	BrowserScroll       = "builtin_browser_scroll"
)

type WebConfigSource interface {
	TavilyAPIKey(ctx context.Context) (string, bool, error)
}

type AppEndpointSource interface {
	ResolveEndpoint(ctx context.Context, sessionID, endpointName, connection string) (*app.EndpointBinding, error)
}

type AppSkillReader interface {
	ReadSkill(ctx context.Context, appID, skillID string) (*app.SkillDetail, error)
}

type SkillReader interface {
	ReadSkill(ctx context.Context, id string) (*skill.Document, error)
}

type SkillDraftSource interface {
	ValidateDraft(ctx context.Context, id string) (*skill.Validation, error)
	DraftDetail(ctx context.Context, id string) (*skill.DraftDetail, error)
	ApplyDraft(ctx context.Context, id string) error
}

type HistorySearchSource interface {
	SearchMessages(ctx context.Context, in store.MessageSearchInput) ([]*store.Message, error)
}

type HistoryMessageSource interface {
	GetMessage(ctx context.Context, sessionID string, messageID string) (*store.Message, error)
}

type BrowserStateStore interface {
	GetBrowserState(ctx context.Context, sessionID string) (*store.BrowserState, error)
	GetBrowserTabState(ctx context.Context, sessionID, tabID string) (*store.BrowserState, error)
	ListBrowserStates(ctx context.Context, sessionID string) ([]*store.BrowserState, error)
	PutBrowserState(ctx context.Context, in store.BrowserStateInput) (*store.BrowserState, error)
	DeleteBrowserState(ctx context.Context, sessionID, tabID string) error
	ClearBrowserState(ctx context.Context, sessionID string) error
}

type BuiltinOption func(*BuiltinRunner)

type BuiltinRunner struct {
	webConfig        WebConfigSource
	appEndpoints     AppEndpointSource
	appSkills        AppSkillReader
	skillReader      SkillReader
	skillDrafts      SkillDraftSource
	history          HistorySearchSource
	historyMessages  HistoryMessageSource
	browserState     BrowserStateStore
	browser          browser.Service
	languageService  lsp.Service
	goServerResolver GoServerResolver
	camera           CameraCapturer
	screen           DesktopScreenCapturer
	homeDir          string
	webHTTPClient    *http.Client
	tavilySearch     string
	tavilyExtract    string
	weatherEndpoint  string
	weatherMu        sync.Mutex
	weatherCache     map[string]weatherCacheEntry
	graphqlSchemaMu  sync.Mutex
	graphqlSchemas   map[string]*graphqlSchemaCache
	patchMu          sync.Mutex
	patchProposals   map[string]*patchProposal
	gitApprovalMu    sync.Mutex
	gitApprovals     map[string]gitCommitApprovalSnapshot
}

func NewBuiltinRunner(opts ...BuiltinOption) *BuiltinRunner {
	r := &BuiltinRunner{
		webHTTPClient:   &http.Client{Timeout: webDefaultTimeout},
		tavilySearch:    tavilySearchEndpoint,
		tavilyExtract:   tavilyExtractEndpoint,
		weatherEndpoint: weatherDefaultEndpoint,
		weatherCache:    map[string]weatherCacheEntry{},
		graphqlSchemas:  map[string]*graphqlSchemaCache{},
		patchProposals:  map[string]*patchProposal{},
		gitApprovals:    map[string]gitCommitApprovalSnapshot{},
	}
	for _, opt := range opts {
		opt(r)
	}
	return r
}

func WithWebConfig(source WebConfigSource) BuiltinOption {
	return func(r *BuiltinRunner) {
		r.webConfig = source
	}
}

func WithAppEndpoints(source AppEndpointSource) BuiltinOption {
	return func(r *BuiltinRunner) {
		r.appEndpoints = source
		if skills, ok := source.(AppSkillReader); ok {
			r.appSkills = skills
		}
	}
}

func WithAppSkills(source AppSkillReader) BuiltinOption {
	return func(r *BuiltinRunner) {
		r.appSkills = source
	}
}

func WithSkills(source SkillReader) BuiltinOption {
	return func(r *BuiltinRunner) {
		r.skillReader = source
		if drafts, ok := source.(SkillDraftSource); ok {
			r.skillDrafts = drafts
		}
	}
}

func WithHistorySearch(source HistorySearchSource) BuiltinOption {
	return func(r *BuiltinRunner) {
		r.history = source
		if messages, ok := source.(HistoryMessageSource); ok {
			r.historyMessages = messages
		}
	}
}

func WithBrowser(service browser.Service) BuiltinOption {
	return func(r *BuiltinRunner) {
		r.browser = service
	}
}

func WithLanguageService(service lsp.Service) BuiltinOption {
	return func(r *BuiltinRunner) {
		r.languageService = service
	}
}

func WithGoServerResolver(resolver GoServerResolver) BuiltinOption {
	return func(r *BuiltinRunner) {
		r.goServerResolver = resolver
	}
}

func WithBrowserState(store BrowserStateStore) BuiltinOption {
	return func(r *BuiltinRunner) {
		r.browserState = store
	}
}

func WithCamera(capturer CameraCapturer) BuiltinOption {
	return func(r *BuiltinRunner) {
		r.camera = capturer
	}
}

func WithDesktopScreen(capturer DesktopScreenCapturer) BuiltinOption {
	return func(r *BuiltinRunner) {
		r.screen = capturer
	}
}

func WithHomeDir(dir string) BuiltinOption {
	return func(r *BuiltinRunner) {
		r.homeDir = dir
	}
}

func WithWebHTTPClient(client *http.Client) BuiltinOption {
	return func(r *BuiltinRunner) {
		if client != nil {
			r.webHTTPClient = client
		}
	}
}

func WithTavilyEndpoints(searchURL, extractURL string) BuiltinOption {
	return func(r *BuiltinRunner) {
		if strings.TrimSpace(searchURL) != "" {
			r.tavilySearch = searchURL
		}
		if strings.TrimSpace(extractURL) != "" {
			r.tavilyExtract = extractURL
		}
	}
}

func WithWeatherEndpoint(endpoint string) BuiltinOption {
	return func(r *BuiltinRunner) {
		if strings.TrimSpace(endpoint) != "" {
			r.weatherEndpoint = strings.TrimRight(strings.TrimSpace(endpoint), "/")
		}
	}
}

func BuiltinDefinitions() []provider.ToolDef {
	return []provider.ToolDef{
		{
			Name:        TimeGetCurrent,
			Description: "Get the current time. Optionally accepts an IANA timezone name.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"timezone":{"type":"string","description":"IANA timezone name, for example Asia/Singapore or America/Los_Angeles. Defaults to the system local timezone."}},"additionalProperties":false}`),
			Capability:  store.ModeChat,
		},
		{
			Name:        WebSearch,
			Description: "Search the web for current or external information. Returns a short answer plus relevant results.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"query":{"type":"string","description":"Search query in natural language. Use one topic per call."},"max_results":{"type":"integer","description":"Optional result count, 1-10, default 5."},"depth":{"type":"string","enum":["basic","advanced"],"description":"Optional search depth. Defaults to basic."},"topic":{"type":"string","enum":["general","news"],"description":"Optional topic. Use news for recent/current events."},"include_answer":{"type":"boolean","description":"Optional, defaults to true."}},"required":["query"],"additionalProperties":false}`),
			Capability:  store.ModeChat,
		},
		{
			Name:        WebFetch,
			Description: "Fetch readable body text from one URL. Use for page reading and summarization, not API calls.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"url":{"type":"string","description":"Full URL starting with http:// or https://."},"depth":{"type":"string","enum":["basic","advanced"],"description":"Optional extract depth. Defaults to basic."},"max_chars":{"type":"integer","description":"Optional maximum body characters, default 4000 and cap 20000."}},"required":["url"],"additionalProperties":false}`),
			Capability:  store.ModeChat,
		},
		{
			Name:        HistorySearch,
			Description: "Full-text search canonical message history in one session. Defaults to the current session; pass session_id only when the user clearly refers to another session. Use when the answer is not in the current context or was compacted away.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"query":{"type":"string","description":"SQLite FTS5 query. Use keywords, quoted phrases, or prefix terms such as chan*. For Chinese, prefer at least three characters."},"session_id":{"type":"string","description":"Optional session id. Defaults to the current session; empty never means all sessions."},"limit":{"type":"integer","description":"Maximum hits, default 10 and hard cap 30."}},"required":["query"],"additionalProperties":false}`),
			Capability:  store.ModeChat,
		},
		{
			Name:        HistoryGetMessage,
			Description: "Read one full canonical history message by message_id. Use after builtin_history_search returns @message(id), or when context contains an attachment/local folder hint and original message details are needed. Defaults to the current session.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"message_id":{"type":"string","description":"Canonical message id, usually from an @message(message_id) reference."},"session_id":{"type":"string","description":"Optional session id. Defaults to the current session."}},"required":["message_id"],"additionalProperties":false}`),
			Capability:  store.ModeChat,
		},
		{
			Name:        SkillRead,
			Description: "Read the full SKILL.md body for one registered global skill or one installed app skill after the user's intent clearly matches the prompt index.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"skill_id":{"type":"string","description":"Skill id from Available Skills or Installed Apps. Do not pass the display path."},"app_id":{"type":"string","description":"Optional installed app id. Set this when reading an app-scoped skill."}},"required":["skill_id"],"additionalProperties":false}`),
			Capability:  store.ModeChat,
		},
		{
			Name:        ProjectInspect,
			Description: "Inspect one authorized project directory using a bounded read-only scan. Returns Git root, detected languages, manifests, project instruction files, and suggested verification commands without executing them.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"scope":{"type":"string","enum":["project"],"description":"Project inspection is limited to authorized project directories."},"path":{"type":"string","description":"Optional absolute or relative directory inside an authorized project root. Defaults to the first project root."}},"required":["scope"],"additionalProperties":false}`),
			Capability:  store.ModeProject,
		},
		{
			Name:        ProjectInstructions,
			Description: "Read the project instruction files that apply to one or more authorized target paths. Resolves AGENTS.md, CLAUDE.md, and CONTRIBUTING.md from the project root toward each target directory, returning deduplicated content in broad-to-specific order.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"scope":{"type":"string","enum":["project"],"description":"Project instructions can be read only from authorized project directories."},"paths":{"type":"array","items":{"type":"string"},"minItems":1,"maxItems":32,"description":"Target files or directories whose applicable instructions are needed. Missing future file paths are allowed when their parent remains inside the project."}},"required":["scope","paths"],"additionalProperties":false}`),
			Capability:  store.ModeProject,
		},
		{
			Name:        CodeSymbols,
			Description: "Search semantic workspace symbols through the language server. Uses one shared tool contract across languages; the current implementation supports Go through gopls.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"scope":{"type":"string","enum":["project"]},"path":{"type":"string","description":"Authorized project file or directory used to resolve the language root. Defaults to the first project root."},"language":{"type":"string","enum":["go"],"description":"Optional language override. Usually inferred from path."},"query":{"type":"string","description":"Non-empty symbol name query."},"max_results":{"type":"integer","minimum":1,"maximum":200,"description":"Default 100, maximum 200."}},"required":["scope","query"],"additionalProperties":false}`),
			Capability:  store.ModeProject,
		},
		{
			Name:        CodeDefinition,
			Description: "Find semantic definitions for a 1-based source position through the language server. Project-external locations are counted but not exposed.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"scope":{"type":"string","enum":["project"]},"path":{"type":"string","description":"Authorized source file."},"language":{"type":"string","enum":["go"],"description":"Optional language override."},"line":{"type":"integer","minimum":1,"description":"1-based line."},"column":{"type":"integer","minimum":1,"description":"1-based Unicode character column."}},"required":["scope","path","line","column"],"additionalProperties":false}`),
			Capability:  store.ModeProject,
		},
		{
			Name:        CodeReferences,
			Description: "Find semantic references for a 1-based source position through the language server. Results are sorted, deduplicated, bounded, and limited to authorized Project roots.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"scope":{"type":"string","enum":["project"]},"path":{"type":"string","description":"Authorized source file."},"language":{"type":"string","enum":["go"],"description":"Optional language override."},"line":{"type":"integer","minimum":1},"column":{"type":"integer","minimum":1},"include_declaration":{"type":"boolean","description":"Defaults to true."},"max_results":{"type":"integer","minimum":1,"maximum":500,"description":"Default 100, maximum 500."}},"required":["scope","path","line","column"],"additionalProperties":false}`),
			Capability:  store.ModeProject,
		},
		{
			Name:        CodeDiagnostics,
			Description: "Read current semantic language-server diagnostics for up to 32 source files sharing one language root. This is static analysis and does not claim tests or builds passed.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"scope":{"type":"string","enum":["project"]},"paths":{"type":"array","items":{"type":"string"},"minItems":1,"maxItems":32,"description":"Authorized source files in one language root."},"language":{"type":"string","enum":["go"],"description":"Optional language override."},"severity":{"type":"array","items":{"type":"string","enum":["error","warning","information","hint"]},"description":"Optional severity filter."}},"required":["scope","paths"],"additionalProperties":false}`),
			Capability:  store.ModeProject,
		},
		{
			Name:        FileList,
			Description: "List files in a Pudding-managed file area or an authorized project directory.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"scope":{"type":"string","enum":["skill_draft","skill_published","temp","project"],"description":"Target file area. Use project for authorized local project directories."},"path":{"type":"string","description":"Relative path inside a managed area, or an absolute/relative path inside authorized project directories. Use . to list the root."},"max_entries":{"type":"integer","description":"Optional maximum entries, 1-1000, default 200."}},"required":["scope","path"],"additionalProperties":false}`),
			Capability:  store.ModeProject,
		},
		{
			Name:        FileRead,
			Description: "Read one small UTF-8 text file, or save one supported image file as an attachment from a Pudding-managed file area or an authorized project directory. Image bytes are visible only to models with image input support; otherwise the model sees attachment metadata only. For large text files use builtin_file_slice or builtin_file_search first.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"scope":{"type":"string","enum":["skill_draft","skill_published","temp","project"],"description":"Target file area. Use project for authorized local project directories."},"path":{"type":"string","description":"Relative file path inside a managed area, or an absolute/relative path inside authorized project directories."},"max_chars":{"type":"integer","description":"Optional max characters, default 20000 and cap 100000."}},"required":["scope","path"],"additionalProperties":false}`),
			Capability:  store.ModeProject,
		},
		{
			Name:        AttachmentReadImage,
			Description: "Read one captured/uploaded image attachment by attachmentKey and route it as an image attachment. Image bytes are visible only to models with image input support; otherwise the model sees metadata only. Use only for existing Pudding attachments that are not already provided as image parts; do not pass local filesystem paths.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"attachmentKey":{"type":"string","description":"Attachment key returned by a capture/upload tool, for example sessions/{sessionID}/blobs/name.png. Prefer this field."},"url":{"type":"string","description":"Display URL fallback for older tool results. Prefer attachmentKey when available."}},"additionalProperties":false}`),
			Capability:  store.ModeChat,
		},
		{
			Name:        FileStat,
			Description: "Return metadata for one file or directory: exists, type, size, mtime, and MIME when available.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"scope":{"type":"string","enum":["skill_draft","skill_published","temp","project"],"description":"Target file area. Use project for authorized local project directories."},"path":{"type":"string","description":"Relative path inside a managed area, or an absolute/relative path inside authorized project directories."}},"required":["scope","path"],"additionalProperties":false}`),
			Capability:  store.ModeProject,
		},
		{
			Name:        FileSearch,
			Description: "Search UTF-8 text files by literal text or RE2-compatible regular expression. Supports case control, project-relative include/exclude globs, and bounded context lines. Skips binary files and common generated directories.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"scope":{"type":"string","enum":["skill_draft","skill_published","temp","project"],"description":"Target file area. Use project for authorized local project directories."},"path":{"type":"string","description":"Search root. Relative path inside a managed area, or an absolute/relative path inside authorized project directories. Use . for the root."},"query":{"type":"string","description":"Text or regular expression to search for."},"mode":{"type":"string","enum":["literal","regex"],"description":"Search mode. Defaults to literal."},"case_sensitive":{"type":"boolean","description":"Whether matching is case-sensitive. Defaults to true."},"include_globs":{"type":"array","items":{"type":"string"},"maxItems":32,"description":"Optional project-relative path globs to include. Supports ** directory segments."},"exclude_globs":{"type":"array","items":{"type":"string"},"maxItems":32,"description":"Optional project-relative path globs to exclude. Supports ** directory segments."},"context_lines":{"type":"integer","minimum":0,"maximum":5,"description":"Context lines before and after each matching line. Defaults to 0."},"max_results":{"type":"integer","minimum":1,"maximum":500,"description":"Optional maximum matching lines, default 100 and cap 500."}},"required":["scope","path","query"],"additionalProperties":false}`),
			Capability:  store.ModeProject,
		},
		{
			Name:        FileSlice,
			Description: "Read a focused line slice from a UTF-8 text file. Supports origin=start for line ranges and origin=end for tail-style reads, with optional reverse order.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"scope":{"type":"string","enum":["skill_draft","skill_published","temp","project"],"description":"Target file area. Use project for authorized local project directories."},"path":{"type":"string","description":"Relative file path inside a managed area, or an absolute/relative path inside authorized project directories."},"origin":{"type":"string","enum":["start","end"],"description":"start reads from a 1-based line range. end reads the last N lines after optional skip. Default start."},"start":{"type":"integer","description":"1-based start line for origin=start. Default 1."},"end":{"type":"integer","description":"Inclusive end line for origin=start. If omitted, lines controls the range length."},"lines":{"type":"integer","description":"Line count for origin=end or when end is omitted. Default 100 and cap 500."},"skip":{"type":"integer","description":"For origin=end, skip this many lines from the file end before taking lines. Default 0."},"order":{"type":"string","enum":["natural","reverse"],"description":"natural returns file order. reverse returns newest/end-most lines first. Default natural."}},"required":["scope","path"],"additionalProperties":false}`),
			Capability:  store.ModeProject,
		},
		{
			Name:        FileWrite,
			Description: "Overwrite one text file in a writable Pudding-managed file area or authorized project directory.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"scope":{"type":"string","enum":["skill_draft","temp","project"],"description":"Target writable file area. Use project for authorized local project directories."},"path":{"type":"string","description":"Relative file path inside a managed area, or an absolute/relative path inside authorized project directories."},"content":{"type":"string","description":"New file content."}},"required":["scope","path","content"],"additionalProperties":false}`),
			Capability:  store.ModeProject,
		},
		{
			Name:        FilePatch,
			Description: "Replace text in one file in a writable Pudding-managed file area or authorized project directory by exact string match.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"scope":{"type":"string","enum":["skill_draft","temp","project"],"description":"Target writable file area. Use project for authorized local project directories."},"path":{"type":"string","description":"Relative file path inside a managed area, or an absolute/relative path inside authorized project directories."},"old_string":{"type":"string","description":"Exact text to replace."},"new_string":{"type":"string","description":"Replacement text."},"replace_all":{"type":"boolean","description":"Replace all matches. Defaults false; without this, exactly one match is required."}},"required":["scope","path","old_string","new_string"],"additionalProperties":false}`),
			Capability:  store.ModeProject,
		},
		{
			Name:        FileDelete,
			Description: "Delete a file or, when recursive is true, a directory from a writable Pudding-managed file area or authorized project directory.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"scope":{"type":"string","enum":["skill_draft","temp","project"],"description":"Target writable file area. Use project for authorized local project directories."},"path":{"type":"string","description":"Relative path inside a managed area, or an absolute/relative path inside authorized project directories."},"recursive":{"type":"boolean","description":"Required to delete a directory."}},"required":["scope","path"],"additionalProperties":false}`),
			Capability:  store.ModeProject,
		},
		{
			Name:        FileMove,
			Description: "Move or rename a file or directory inside the same writable managed area or authorized project directories.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"scope":{"type":"string","enum":["skill_draft","temp","project"]},"from_path":{"type":"string"},"to_path":{"type":"string"}},"required":["scope","from_path","to_path"],"additionalProperties":false}`),
			Capability:  store.ModeProject,
		},
		{
			Name:        FileCopy,
			Description: "Copy a file or directory inside the same writable managed area or authorized project directory. Directories require recursive=true; existing destinations require overwrite=true.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"scope":{"type":"string","enum":["skill_draft","temp","project"],"description":"Target writable file area."},"from_path":{"type":"string","description":"Source path in the same scope."},"to_path":{"type":"string","description":"Destination path in the same scope."},"recursive":{"type":"boolean","description":"Required when copying a directory."},"overwrite":{"type":"boolean","description":"Replace an existing destination. Defaults false."}},"required":["scope","from_path","to_path"],"additionalProperties":false}`),
			Capability:  store.ModeProject,
		},
		{
			Name:        CommandRun,
			Description: "Run one local command directly from an argv array in an authorized project directory. This tool does not invoke a shell or interpret shell syntax. Returns exit code, bounded output, duration, timeout and cancellation metadata; recognized test, lint, build, and check commands also return verification status and project-scoped diagnostics.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"scope":{"type":"string","enum":["project"],"description":"Commands can run only with project access."},"argv":{"type":"array","items":{"type":"string"},"minItems":1,"maxItems":128,"description":"Executable followed by arguments. Do not pass a shell command string."},"cwd":{"type":"string","description":"Absolute or relative directory inside authorized project roots. Defaults to the first project root."},"env":{"type":"object","additionalProperties":{"type":"string"},"description":"Optional environment values for this command. Pudding otherwise inherits only a minimal safe environment."},"timeout_ms":{"type":"integer","minimum":100,"maximum":600000,"description":"Optional timeout in milliseconds. Defaults to 60000; maximum 600000."}},"required":["scope","argv"],"additionalProperties":false}`),
			Capability:  store.ModeProject,
		},
		{
			Name:        GitStatus,
			Description: "Read structured Git worktree status for the repository containing an authorized project directory. Returns branch, upstream divergence, file states, and summary counts.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"scope":{"type":"string","enum":["project"],"description":"Git tools can read only authorized project repositories."},"cwd":{"type":"string","description":"Absolute or relative directory inside an authorized project root. Defaults to the first project root."}},"required":["scope"],"additionalProperties":false}`),
			Capability:  store.ModeProject,
		},
		{
			Name:        GitDiff,
			Description: "Read the unstaged or staged Git diff for an authorized project repository. Returns a bounded patch plus structured per-file additions and deletions.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"scope":{"type":"string","enum":["project"],"description":"Git tools can read only authorized project repositories."},"cwd":{"type":"string","description":"Absolute or relative directory inside an authorized project root. Defaults to the first project root."},"staged":{"type":"boolean","description":"When true, read the staged diff. Defaults to false for unstaged changes."}},"required":["scope"],"additionalProperties":false}`),
			Capability:  store.ModeProject,
		},
		{
			Name:        GitLog,
			Description: "Read recent commits from an authorized project repository as structured metadata.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"scope":{"type":"string","enum":["project"],"description":"Git tools can read only authorized project repositories."},"cwd":{"type":"string","description":"Absolute or relative directory inside an authorized project root. Defaults to the first project root."},"limit":{"type":"integer","minimum":1,"maximum":100,"description":"Maximum commits to return. Defaults to 20."}},"required":["scope"],"additionalProperties":false}`),
			Capability:  store.ModeProject,
		},
		{
			Name:        GitStage,
			Description: "Stage explicit project file paths in Git after approval. Paths are literal repository-relative or absolute paths; arbitrary pathspecs and Git options are not accepted.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"scope":{"type":"string","enum":["project"],"description":"Git writes can target only authorized project repositories."},"cwd":{"type":"string","description":"Absolute or relative directory inside an authorized project root. Defaults to the first project root."},"paths":{"type":"array","items":{"type":"string"},"minItems":1,"maxItems":128,"description":"Explicit files to stage. Paths returned by builtin_git_status are repository-relative and can be passed directly."}},"required":["scope","paths"],"additionalProperties":false}`),
			Capability:  store.ModeProject,
		},
		{
			Name:        GitUnstage,
			Description: "Remove explicit project file paths from the Git index after approval without changing worktree files. Arbitrary pathspecs and Git options are not accepted.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"scope":{"type":"string","enum":["project"],"description":"Git writes can target only authorized project repositories."},"cwd":{"type":"string","description":"Absolute or relative directory inside an authorized project root. Defaults to the first project root."},"paths":{"type":"array","items":{"type":"string"},"minItems":1,"maxItems":128,"description":"Explicit repository files to unstage."}},"required":["scope","paths"],"additionalProperties":false}`),
			Capability:  store.ModeProject,
		},
		{
			Name:        GitCommit,
			Description: "Commit the currently staged Git changes after showing their status and diff for approval. The commit is rejected if HEAD or the index changes while approval is pending. Hooks and signing are disabled.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"scope":{"type":"string","enum":["project"],"description":"Git writes can target only authorized project repositories."},"cwd":{"type":"string","description":"Absolute or relative directory inside an authorized project root. Defaults to the first project root."},"message":{"type":"string","minLength":1,"maxLength":16384,"description":"Commit message. Amend, signing, hooks, and arbitrary commit options are not supported."}},"required":["scope","message"],"additionalProperties":false}`),
			Capability:  store.ModeProject,
		},
		{
			Name:        PatchPropose,
			Description: "Prepare a reviewable multi-file text patch without changing project files. Prefer ordered exact-match edits for existing files; use new_text for creates or intentional full replacement, and delete=true for deletion. The proposal records current file hashes and returns a unified diff for approval.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"scope":{"type":"string","enum":["project"],"description":"Patch proposals can target only authorized project files."},"files":{"type":"array","minItems":1,"maxItems":16,"items":{"type":"object","properties":{"path":{"type":"string","description":"Absolute or relative file path inside one authorized project root."},"new_text":{"type":"string","description":"Complete desired UTF-8 file content. Use for creating a file or an intentional full replacement."},"edits":{"type":"array","minItems":1,"maxItems":64,"items":{"type":"object","properties":{"old_text":{"type":"string","minLength":1,"description":"Exact existing text to replace in the current in-memory snapshot."},"new_text":{"type":"string","description":"Replacement UTF-8 text."},"replace_all":{"type":"boolean","description":"Replace every exact match. Defaults to false and requires old_text to be unique."}},"required":["old_text","new_text"],"additionalProperties":false},"description":"Ordered exact replacements for an existing file. Mutually exclusive with new_text and delete."},"delete":{"type":"boolean","description":"Set true to delete an existing regular text file. Mutually exclusive with new_text and edits."}},"required":["path"],"additionalProperties":false},"description":"All proposed file changes. Every file must resolve inside the same project root."}},"required":["scope","files"],"additionalProperties":false}`),
			Capability:  store.ModeProject,
		},
		{
			Name:        PatchApply,
			Description: "Apply one previously generated patch proposal after its unified diff is approved. The apply fails without changing files if any source file has drifted since proposal creation.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"proposal_id":{"type":"string","description":"Proposal id returned by builtin_patch_propose."}},"required":["proposal_id"],"additionalProperties":false}`),
			Capability:  store.ModeProject,
		},
		{
			Name:        SkillValidate,
			Description: "Validate one staged skill package before asking the user to review it.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"draft_id":{"type":"string","description":"Staged skill package id."}},"required":["draft_id"],"additionalProperties":false}`),
			Capability:  store.ModeProject,
		},
		{
			Name:        SkillSubmit,
			Description: "Submit one valid staged skill package for user review in Settings. This does not publish the skill.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"draft_id":{"type":"string","description":"Staged skill package id."}},"required":["draft_id"],"additionalProperties":false}`),
			Capability:  store.ModeProject,
		},
		{
			Name:        RESTRequest,
			Description: "Send one HTTP request to a configured REST endpoint. Pass an endpoint name plus a relative path; authorization and configured headers are injected by Pudding. Omit connection when there is one configured connection.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"endpoint":{"type":"string","description":"Configured endpoint name, for example github_rest. Required; there is no default endpoint."},"connection":{"type":"string","description":"Optional connection name or id. Only pass this when the endpoint reports multiple configured connections."},"method":{"type":"string","enum":["GET","POST","PUT","PATCH","DELETE"],"description":"HTTP method. Defaults to GET."},"path":{"type":"string","description":"Relative path under the endpoint base URL, for example /repos/owner/repo/issues. Must not be a full URL."},"query":{"type":"object","additionalProperties":{"type":["string","number","boolean"]},"description":"Optional query parameters."},"body_json":{"description":"Optional JSON body. Mutually exclusive with body_text."},"body_text":{"type":"string","description":"Optional text body. Mutually exclusive with body_json."}},"required":["endpoint","path"],"additionalProperties":false}`),
			Capability:  store.ModeChat,
		},
		{
			Name:        GraphQLRequest,
			Description: "Send one GraphQL query or mutation to a configured GraphQL endpoint. Authorization is injected by Pudding. Omit connection when there is one configured connection.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"endpoint":{"type":"string","description":"Configured GraphQL endpoint name. Required; there is no default endpoint."},"connection":{"type":"string","description":"Optional connection name or id. Only pass this when the endpoint reports multiple configured connections."},"query":{"type":"string","description":"GraphQL query or mutation text."},"variables":{"description":"Optional GraphQL variables object or JSON object string."}},"required":["endpoint","query"],"additionalProperties":false}`),
			Capability:  store.ModeChat,
		},
		{
			Name:        GraphQLIntrospect,
			Description: "Inspect a configured GraphQL endpoint schema. Without type_name returns Query/Mutation/Subscription fields; with type_name returns that type's one-level structure.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"endpoint":{"type":"string","description":"Configured GraphQL endpoint name."},"connection":{"type":"string","description":"Optional connection name or id."},"type_name":{"type":"string","description":"Optional GraphQL type name to inspect, for example Query, User, or UserInput."},"force_refresh":{"type":"boolean","description":"Ignore cached schema and fetch again."}},"required":["endpoint"],"additionalProperties":false}`),
			Capability:  store.ModeChat,
		},
		{
			Name:        GraphQLSearch,
			Description: "Search names and descriptions in a configured GraphQL endpoint schema. Use before writing GraphQL when field/type names are uncertain.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"endpoint":{"type":"string","description":"Configured GraphQL endpoint name."},"connection":{"type":"string","description":"Optional connection name or id."},"query":{"type":"string","description":"Case-insensitive keywords. Space-separated keywords are ANDed."},"max_results":{"type":"integer","description":"Optional result count, default 30 and cap 50."},"force_refresh":{"type":"boolean","description":"Ignore cached schema and fetch again."}},"required":["endpoint","query"],"additionalProperties":false}`),
			Capability:  store.ModeChat,
		},
		{
			Name:        WeatherGet,
			Description: "Get current weather and optional short forecast for a location using wttr.in JSON. Useful for weather answers without doing web search.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"location":{"type":"string","description":"City/location. Empty uses IP-based approximate location."},"lang":{"type":"string","description":"Optional language, for example en, zh, zh-cn, zh-tw. Defaults to en."},"days":{"type":"integer","description":"Forecast days to include, 0-3. Defaults to 0 current-only."}},"additionalProperties":false}`),
			Capability:  store.ModeChat,
		},
		{
			Name:        CameraCapture,
			Description: "Take one photo from the local camera and return a displayable attachment URL. The photo bytes are not routed to the model; call builtin_attachment_read_image only if the image content must be inspected.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`),
			Capability:  store.ModeChat,
		},
		{
			Name:        DesktopScreenshot,
			Description: "Capture the local desktop screen and route it as image attachment(s). Use only when the user asks you to look at the current screen or desktop.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"display":{"type":"integer","description":"Optional display index. Main display is 0. Omit to capture all active displays."}},"additionalProperties":false}`),
			Capability:  store.ModeChat,
		},
		{
			Name:        BrowserStatus,
			Description: "List the current session's managed browser tabs and their tab IDs. Use the returned tabID for deterministic browser actions when more than one tab exists.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`),
			Capability:  store.ModeChat,
		},
		{
			Name:        BrowserOpen,
			Description: "Open a URL in a managed browser tab. Pass tabID to reuse a tab, or newTab=true to create a separate tab. With multiple tabs, omitting both is an error.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"url":{"type":"string","description":"URL to open. http and https are supported; scheme defaults to https."},"tabID":{"type":"string","description":"Existing tab ID returned by builtin_browser_status."},"newTab":{"type":"boolean","description":"Create a new tab before opening the URL."}},"required":["url"],"additionalProperties":false}`),
			Capability:  store.ModeChat,
		},
		{
			Name:        BrowserObserve,
			Description: "Observe a managed browser tab: title, URL, visible page text, and interactive elements.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"tabID":{"type":"string","description":"Tab ID. Required when more than one tab exists."},"maxTextChars":{"type":"integer","description":"Optional max page text characters, default 6000, cap 20000."},"maxElements":{"type":"integer","description":"Optional max interactive elements, default 30, cap 100."}},"additionalProperties":false}`),
			Capability:  store.ModeChat,
		},
		{
			Name:        BrowserScreenshot,
			Description: "Capture a screenshot of a managed browser tab and route it as an image attachment.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"tabID":{"type":"string","description":"Tab ID. Required when more than one tab exists."},"fullPage":{"type":"boolean","description":"Capture beyond the viewport when supported. Defaults false."}},"additionalProperties":false}`),
			Capability:  store.ModeChat,
		},
		{
			Name:        BrowserBack,
			Description: "Navigate a managed browser tab back in history.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"tabID":{"type":"string","description":"Tab ID. Required when more than one tab exists."}},"additionalProperties":false}`),
			Capability:  store.ModeChat,
		},
		{
			Name:        BrowserForward,
			Description: "Navigate a managed browser tab forward in history.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"tabID":{"type":"string","description":"Tab ID. Required when more than one tab exists."}},"additionalProperties":false}`),
			Capability:  store.ModeChat,
		},
		{
			Name:        BrowserReload,
			Description: "Reload a managed browser tab.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"tabID":{"type":"string","description":"Tab ID. Required when more than one tab exists."}},"additionalProperties":false}`),
			Capability:  store.ModeChat,
		},
		{
			Name:        BrowserClose,
			Description: "Close one managed browser tab by tabID, or close all tabs in the current session when tabID is omitted.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"tabID":{"type":"string","description":"Optional tab ID to close. Omit to close all tabs in the session."}},"additionalProperties":false}`),
			Capability:  store.ModeChat,
		},
		{
			Name:        BrowserClick,
			Description: "Click an element in a managed browser tab by CSS selector or viewport coordinates. Use method=pointer for real mouse events, method=dom only as a fallback for simple elements.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"tabID":{"type":"string","description":"Tab ID. Required when more than one tab exists."},"selector":{"type":"string","description":"CSS selector to click."},"x":{"type":"number","description":"Viewport X coordinate, used when selector is omitted."},"y":{"type":"number","description":"Viewport Y coordinate, used when selector is omitted."},"method":{"type":"string","enum":["auto","pointer","dom"],"description":"Click implementation. auto defaults to real CDP mouse events and falls back to DOM click; pointer forces real mouse events; dom forces element.click()."}},"additionalProperties":false}`),
			Capability:  store.ModeChat,
		},
		{
			Name:        BrowserType,
			Description: "Type text into an editable element in a managed browser tab.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"tabID":{"type":"string","description":"Tab ID. Required when more than one tab exists."},"selector":{"type":"string","description":"CSS selector for the input. If omitted, uses the active element."},"text":{"type":"string","description":"Text to type."},"clear":{"type":"boolean","description":"Replace existing value before typing."}},"required":["text"],"additionalProperties":false}`),
			Capability:  store.ModeChat,
		},
		{
			Name:        BrowserScroll,
			Description: "Scroll a managed browser tab or one of its scrollable elements.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"tabID":{"type":"string","description":"Tab ID. Required when more than one tab exists."},"selector":{"type":"string","description":"Optional CSS selector for a scrollable element."},"deltaX":{"type":"number","description":"Horizontal scroll delta in pixels."},"deltaY":{"type":"number","description":"Vertical scroll delta in pixels. Defaults to 600 when both deltas are omitted."}},"additionalProperties":false}`),
			Capability:  store.ModeChat,
		},
	}
}

func (r *BuiltinRunner) Definitions(context.Context, string) ([]provider.ToolDef, error) {
	return BuiltinDefinitions(), nil
}

func (r *BuiltinRunner) Call(ctx context.Context, call Call) Result {
	out := Result{CallID: call.CallID, Name: call.Name}
	switch call.Name {
	case TimeGetCurrent:
		return currentTime(call)
	case WebSearch:
		return r.webSearch(ctx, call)
	case WebFetch:
		return r.webFetch(ctx, call)
	case HistorySearch:
		return r.historySearch(ctx, call)
	case HistoryGetMessage:
		return r.historyGetMessage(ctx, call)
	case SkillRead:
		return r.skillRead(ctx, call)
	case ProjectInspect:
		return r.projectInspect(ctx, call)
	case ProjectInstructions:
		return r.projectInstructions(call)
	case CodeSymbols:
		return r.codeSymbols(ctx, call)
	case CodeDefinition:
		return r.codeDefinition(ctx, call)
	case CodeReferences:
		return r.codeReferences(ctx, call)
	case CodeDiagnostics:
		return r.codeDiagnostics(ctx, call)
	case FileList:
		return r.fileList(call)
	case FileRead:
		return r.fileRead(call)
	case AttachmentReadImage:
		return r.attachmentReadImage(call)
	case FileStat:
		return r.fileStat(call)
	case FileSearch:
		return r.fileSearch(call)
	case FileSlice:
		return r.fileSlice(call)
	case FileWrite:
		return r.fileWrite(call)
	case FilePatch:
		return r.filePatch(call)
	case FileDelete:
		return r.fileDelete(call)
	case FileMove:
		return r.fileMove(call)
	case FileCopy:
		return r.fileCopy(call)
	case CommandRun:
		return r.commandRun(ctx, call)
	case GitStatus:
		return r.gitStatus(ctx, call)
	case GitDiff:
		return r.gitDiff(ctx, call)
	case GitLog:
		return r.gitLog(ctx, call)
	case GitStage:
		return r.gitStage(ctx, call)
	case GitUnstage:
		return r.gitUnstage(ctx, call)
	case GitCommit:
		return r.gitCommit(ctx, call)
	case PatchPropose:
		return r.patchPropose(call)
	case PatchApply:
		return r.patchApply(call)
	case SkillValidate:
		return r.skillValidate(ctx, call)
	case SkillSubmit:
		return r.skillSubmit(ctx, call)
	case RESTRequest:
		return r.restRequest(ctx, call)
	case GraphQLRequest:
		return r.graphqlRequest(ctx, call)
	case GraphQLIntrospect:
		return r.graphqlIntrospect(ctx, call)
	case GraphQLSearch:
		return r.graphqlSearch(ctx, call)
	case WeatherGet:
		return r.weatherGet(ctx, call)
	case CameraCapture:
		return r.cameraCapture(ctx, call)
	case DesktopScreenshot:
		return r.desktopScreenshot(ctx, call)
	case BrowserStatus:
		return r.browserStatus(ctx, call)
	case BrowserOpen:
		return r.browserOpen(ctx, call)
	case BrowserObserve:
		return r.browserObserve(ctx, call)
	case BrowserScreenshot:
		return r.browserScreenshot(ctx, call)
	case BrowserBack:
		return r.browserNavigate(ctx, call, "back")
	case BrowserForward:
		return r.browserNavigate(ctx, call, "forward")
	case BrowserReload:
		return r.browserNavigate(ctx, call, "reload")
	case BrowserClose:
		return r.browserClose(ctx, call)
	case BrowserClick:
		return r.browserClick(ctx, call)
	case BrowserType:
		return r.browserType(ctx, call)
	case BrowserScroll:
		return r.browserScroll(ctx, call)
	default:
		out.Ok = false
		out.Content = fmt.Sprintf("unknown tool: %s", call.Name)
		return out
	}
}

func currentTime(call Call) Result {
	out := Result{CallID: call.CallID, Name: call.Name}
	var args struct {
		Timezone string `json:"timezone"`
	}
	if len(call.Args) > 0 {
		if err := json.Unmarshal(call.Args, &args); err != nil {
			out.Ok = false
			out.Content = "invalid arguments: " + err.Error()
			return out
		}
	}
	loc := time.Local
	if tz := strings.TrimSpace(args.Timezone); tz != "" {
		loaded, err := time.LoadLocation(tz)
		if err != nil {
			out.Ok = false
			out.Content = "invalid timezone: " + tz
			return out
		}
		loc = loaded
	}
	now := time.Now().In(loc)
	payload := map[string]any{
		"iso":      now.Format(time.RFC3339),
		"timezone": loc.String(),
		"unixMs":   now.UnixMilli(),
	}
	b, err := json.Marshal(payload)
	if err != nil {
		out.Ok = false
		out.Content = err.Error()
		return out
	}
	out.Ok = true
	out.Content = string(b)
	out.SummaryKind = SummaryReturnedFields
	out.SummaryCount = len(payload)
	return out
}
