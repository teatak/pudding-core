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
	"github.com/teatak/pudding-core/internal/computer"
	"github.com/teatak/pudding-core/internal/lsp"
	"github.com/teatak/pudding-core/internal/provider"
	"github.com/teatak/pudding-core/internal/skill"
	"github.com/teatak/pudding-core/internal/store"
)

const (
	TimeGetCurrent    = "builtin_time_get_current"
	WebSearch         = "builtin_web_search"
	WebFetch          = "builtin_web_fetch"
	HistorySearch     = "builtin_history_search"
	HistoryGetMessage = "builtin_history_get_message"
	SkillRead         = "builtin_skill_read"
	PlanUpdate        = "builtin_plan_update"
	CodeSymbols       = "builtin_code_symbols"
	CodeDefinition    = "builtin_code_definition"
	CodeReferences    = "builtin_code_references"
	CodeDiagnostics   = "builtin_code_diagnostics"
	CodeRename        = "builtin_code_rename"
	FileList          = "builtin_file_list"
	FileRead          = "builtin_file_read"
	MediaRead         = "builtin_media_read"
	AttachmentExport  = "builtin_attachment_export"
	FileStat          = "builtin_file_stat"
	FileSearch        = "builtin_file_search"
	FileSlice         = "builtin_file_slice"
	FileWrite         = "builtin_file_write"
	FilePatch         = "builtin_file_patch"
	FileDelete        = "builtin_file_delete"
	FileMove          = "builtin_file_move"
	FileCopy          = "builtin_file_copy"
	CommandRun        = "builtin_command_run"
	CommandSession    = "builtin_command_session"
	GitStatus         = "builtin_git_status"
	GitDiff           = "builtin_git_diff"
	GitLog            = "builtin_git_log"
	GitStage          = "builtin_git_stage"
	GitUnstage        = "builtin_git_unstage"
	GitCommit         = "builtin_git_commit"
	SkillValidate     = "builtin_skill_validate"
	AppSave           = "builtin_app_save"
	RESTRequest       = "builtin_rest_request"
	GraphQLRequest    = "builtin_graphql_request"
	GraphQLIntrospect = "builtin_graphql_introspect"
	GraphQLSearch     = "builtin_graphql_search"
	WeatherGet        = "builtin_weather_get"
	CameraCapture     = "builtin_camera_capture"
	DesktopScreenshot = "builtin_desktop_screenshot"
	BrowserStatus     = "builtin_browser_status"
	BrowserOpen       = "builtin_browser_open"
	BrowserObserve    = "builtin_browser_observe"
	BrowserScreenshot = "builtin_browser_screenshot"
	BrowserBack       = "builtin_browser_back"
	BrowserForward    = "builtin_browser_forward"
	BrowserReload     = "builtin_browser_reload"
	BrowserClose      = "builtin_browser_close"
	BrowserClick      = "builtin_browser_click"
	BrowserType       = "builtin_browser_type"
	BrowserScroll     = "builtin_browser_scroll"
	ComputerListApps  = "builtin_computer_list_apps"
	ComputerUseApp    = "builtin_computer_use_app"
	ComputerQuitApp   = "builtin_computer_quit_app"
	ComputerObserve   = "builtin_computer_observe"
	ComputerAct       = "builtin_computer_act"
	RequestUserInput  = "builtin_request_user_input"
)

type WebConfigSource interface {
	TavilyAPIKey(ctx context.Context) (string, bool, error)
}

type AppEndpointSource interface {
	ResolveEndpoint(ctx context.Context, sessionID, endpointName, connection string) (*app.EndpointBinding, error)
}

type AppAuthoringSource interface {
	ListDefinitions(ctx context.Context) ([]*app.Definition, error)
	SaveAuthoredPackage(ctx context.Context, packageJSON []byte, update bool) (*app.Definition, error)
}

type SkillReader interface {
	ReadSkill(ctx context.Context, id string) (*skill.Document, error)
}

type SkillValidator interface {
	ValidateSkill(ctx context.Context, id string) (*skill.Validation, error)
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
	webConfig                WebConfigSource
	appEndpoints             AppEndpointSource
	appAuthoring             AppAuthoringSource
	skillReader              SkillReader
	skillValidator           SkillValidator
	history                  HistorySearchSource
	historyMessages          HistoryMessageSource
	browserState             BrowserStateStore
	browser                  browser.Service
	computer                 computer.Controller
	languageService          lsp.Service
	goServerResolver         GoServerResolver
	typeScriptServerResolver TypeScriptServerResolver
	camera                   CameraCapturer
	screen                   DesktopScreenCapturer
	homeDir                  string
	webHTTPClient            *http.Client
	tavilySearch             string
	tavilyExtract            string
	weatherEndpoint          string
	weatherMu                sync.Mutex
	weatherCache             map[string]weatherCacheEntry
	graphqlSchemaMu          sync.Mutex
	graphqlSchemas           map[string]*graphqlSchemaCache
	appTokenMu               sync.Mutex
	appTokens                map[string]endpointAuthTokenCacheEntry
	patchMu                  sync.Mutex
	preparedPatches          map[string]*preparedPatch
	gitApprovalMu            sync.Mutex
	gitApprovals             map[string]gitCommitApprovalSnapshot
	commands                 commandRunner
	processes                *backgroundProcessManager
}

func NewBuiltinRunner(opts ...BuiltinOption) *BuiltinRunner {
	commands := newDirectCommandRunner()
	r := &BuiltinRunner{
		webHTTPClient:   &http.Client{Timeout: webDefaultTimeout},
		tavilySearch:    tavilySearchEndpoint,
		tavilyExtract:   tavilyExtractEndpoint,
		weatherEndpoint: weatherDefaultEndpoint,
		weatherCache:    map[string]weatherCacheEntry{},
		graphqlSchemas:  map[string]*graphqlSchemaCache{},
		appTokens:       map[string]endpointAuthTokenCacheEntry{},
		preparedPatches: map[string]*preparedPatch{},
		gitApprovals:    map[string]gitCommitApprovalSnapshot{},
		commands:        commands,
		processes:       newBackgroundProcessManager(backgroundProcessRetentionTTL, commands),
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
	}
}

func WithAppAuthoring(source AppAuthoringSource) BuiltinOption {
	return func(r *BuiltinRunner) {
		r.appAuthoring = source
	}
}

func WithSkills(source SkillReader) BuiltinOption {
	return func(r *BuiltinRunner) {
		r.skillReader = source
		if validator, ok := source.(SkillValidator); ok {
			r.skillValidator = validator
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

func WithComputer(controller computer.Controller) BuiltinOption {
	return func(r *BuiltinRunner) {
		r.computer = controller
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

func WithTypeScriptServerResolver(resolver TypeScriptServerResolver) BuiltinOption {
	return func(r *BuiltinRunner) {
		r.typeScriptServerResolver = resolver
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

func WithCommandSandbox(dir string) BuiltinOption {
	return func(r *BuiltinRunner) {
		r.setCommandRunner(newPlatformCommandRunner(dir))
	}
}

func WithBackgroundProcessEvents(events func(BackgroundProcessEvent)) BuiltinOption {
	return func(r *BuiltinRunner) {
		r.processes.events = events
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
			Description: "Read the full SKILL.md body for one registered global skill after the user's intent clearly matches Available Skills. This does not load Apps; use builtin_app_load for an App.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"skill_id":{"type":"string","description":"Skill id from Available Skills. Do not pass the display path."}},"required":["skill_id"],"additionalProperties":false}`),
			Capability:  store.ModeChat,
		},
		{
			Name:        PlanUpdate,
			Description: "Create or replace the progress plan for a substantial multi-step turn. Call before starting long work, then call again only when a major step changes status. Keep exactly one step in_progress until every step is completed. Skip this tool for short or single-step work.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"plan":{"type":"array","minItems":2,"maxItems":12,"items":{"type":"object","properties":{"step":{"type":"string","minLength":1,"maxLength":200,"description":"Short task description shown beside the progress segments."},"status":{"type":"string","enum":["pending","in_progress","completed"]}},"required":["step","status"],"additionalProperties":false}}},"required":["plan"],"additionalProperties":false}`),
			Capability:  store.ModeWork,
		},
		{
			Name:        CodeSymbols,
			Description: "Search semantic workspace symbols through the language server. Uses one shared tool contract for Go and TypeScript/JavaScript.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"scope":{"type":"string","enum":["project"]},"path":{"type":"string","description":"Authorized project file or directory used to resolve the language root. Defaults to the first project root."},"language":{"type":"string","enum":["go","typescript"],"description":"Optional language override. Use typescript for TypeScript and JavaScript; usually inferred from path."},"query":{"type":"string","maxLength":500,"description":"Non-empty symbol name query."},"max_results":{"type":"integer","minimum":1,"maximum":200,"description":"Default 100, maximum 200."}},"required":["scope","query"],"additionalProperties":false}`),
			Capability:  store.ModeCode,
		},
		{
			Name:        CodeDefinition,
			Description: "Find semantic definitions for a 1-based source position through the language server. Project-external locations are counted but not exposed.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"scope":{"type":"string","enum":["project"]},"path":{"type":"string","description":"Authorized source file."},"language":{"type":"string","enum":["go","typescript"],"description":"Optional language override. Use typescript for TypeScript and JavaScript."},"line":{"type":"integer","minimum":1,"description":"1-based line."},"column":{"type":"integer","minimum":1,"description":"1-based Unicode character column."}},"required":["scope","path","line","column"],"additionalProperties":false}`),
			Capability:  store.ModeCode,
		},
		{
			Name:        CodeReferences,
			Description: "Find semantic references for a 1-based source position through the language server. Results are sorted, deduplicated, bounded, and limited to authorized Project roots.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"scope":{"type":"string","enum":["project"]},"path":{"type":"string","description":"Authorized source file."},"language":{"type":"string","enum":["go","typescript"],"description":"Optional language override. Use typescript for TypeScript and JavaScript."},"line":{"type":"integer","minimum":1},"column":{"type":"integer","minimum":1},"include_declaration":{"type":"boolean","description":"Defaults to true."},"max_results":{"type":"integer","minimum":1,"maximum":500,"description":"Default 100, maximum 500."}},"required":["scope","path","line","column"],"additionalProperties":false}`),
			Capability:  store.ModeCode,
		},
		{
			Name:        CodeDiagnostics,
			Description: "Read current semantic language-server diagnostics for up to 32 source files sharing one language root. This is static analysis and does not claim tests or builds passed.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"scope":{"type":"string","enum":["project"]},"paths":{"type":"array","items":{"type":"string"},"minItems":1,"maxItems":32,"description":"Authorized source files in one language root."},"language":{"type":"string","enum":["go","typescript"],"description":"Optional language override. Use typescript for TypeScript and JavaScript."},"severity":{"type":"array","items":{"type":"string","enum":["error","warning","information","hint"]},"description":"Optional severity filter."}},"required":["scope","paths"],"additionalProperties":false}`),
			Capability:  store.ModeCode,
		},
		{
			Name:        CodeRename,
			Description: "Rename one project symbol through the language server and atomically apply all resulting reference edits. The operation fails without partial writes when any target is invalid or changes during application.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"scope":{"type":"string","enum":["project"]},"path":{"type":"string","description":"Authorized source file containing the symbol."},"language":{"type":"string","enum":["go","typescript"],"description":"Optional language override. Use typescript for TypeScript and JavaScript."},"line":{"type":"integer","minimum":1,"description":"1-based line."},"column":{"type":"integer","minimum":1,"description":"1-based Unicode character column."},"new_name":{"type":"string","minLength":1,"maxLength":256,"description":"New symbol name. Language-specific validity is checked by the language server."}},"required":["scope","path","line","column","new_name"],"additionalProperties":false}`),
			Capability:  store.ModeCode,
		},
		{
			Name:        FileList,
			Description: "List files in a Pudding-managed file area or an authorized project directory. For a multi-root Project, path=. returns every authorized root so each directory is visible and can be selected explicitly.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"scope":{"type":"string","enum":["app","skill","temp","project"],"description":"Target file area. Use app to inspect installed App package files, skill for global user Skills, and project for authorized local project directories."},"path":{"type":"string","description":"Relative path inside a managed area, or an absolute/relative path inside authorized project directories. Use . to list the root; when multiple Project roots are authorized, . lists those roots instead of silently selecting the first one."},"max_entries":{"type":"integer","description":"Optional maximum entries, 1-1000, default 200."}},"required":["scope","path"],"additionalProperties":false}`),
			Capability:  store.ModeCode,
		},
		{
			Name:        FileRead,
			Description: "Read one small UTF-8 text file from a Pudding-managed file area or an authorized project directory. This tool is text-only; use builtin_media_read for supported images or audio. For large text files use builtin_file_slice or builtin_file_search first.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"scope":{"type":"string","enum":["app","skill","temp","project"],"description":"Target file area. Use app to inspect installed App package files, skill for global user Skills, and project for authorized local project directories."},"path":{"type":"string","description":"Relative file path inside a managed area, or an absolute/relative path inside authorized project directories."},"max_chars":{"type":"integer","description":"Optional max characters, default 20000 and cap 100000."}},"required":["scope","path"],"additionalProperties":false}`),
			Capability:  store.ModeCode,
		},
		{
			Name:        MediaRead,
			Description: "Route one supported raster image or audio file up to 20 MiB to the model as a media attachment. Set source=attachment for an existing session attachment. In Code mode, source=file reads a Pudding-managed or explicitly authorized project file. Media bytes are visible only when the current model supports that input type; otherwise only metadata is available. This tool does not transcribe audio. SVG is text source and should be read with builtin_file_read.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"source":{"type":"string","enum":["attachment","file"],"description":"Where the media comes from. source=file requires Code capability."},"attachmentKey":{"type":"string","description":"For source=attachment, the exact session attachment key returned by an upload or capture tool. Prefer this field."},"url":{"type":"string","description":"For source=attachment, a session attachment URL fallback."},"scope":{"type":"string","enum":["app","skill","temp","project"],"description":"For source=file in Code mode, the target file area."},"path":{"type":"string","description":"For source=file in Code mode, a relative path in a managed area or an absolute/relative path inside authorized project directories."}},"required":["source"],"additionalProperties":false}`),
			Capability:  store.ModeChat,
		},
		{
			Name:        AttachmentExport,
			Description: "Export one session attachment, such as a browser screenshot, desktop screenshot, or camera photo, to an authorized project file. Use the exact attachmentKey returned by the capture tool; never guess the attachment's internal filesystem path.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"scope":{"type":"string","enum":["project"]},"attachmentKey":{"type":"string","description":"Exact attachmentKey returned by a capture or upload tool."},"path":{"type":"string","description":"Destination file path inside an authorized project root. Relative paths use the first authorized project root."},"overwrite":{"type":"boolean","description":"Replace an existing destination file. Defaults false."}},"required":["scope","attachmentKey","path"],"additionalProperties":false}`),
			Capability:  store.ModeCode,
		},
		{
			Name:        FileStat,
			Description: "Return metadata for one file or directory: exists, type, size, mtime, and MIME when available.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"scope":{"type":"string","enum":["app","skill","temp","project"],"description":"Target file area. The app scope is read-only and excludes connection data and hidden runtime overrides."},"path":{"type":"string","description":"Relative path inside a managed area, or an absolute/relative path inside authorized project directories."}},"required":["scope","path"],"additionalProperties":false}`),
			Capability:  store.ModeCode,
		},
		{
			Name:        FileSearch,
			Description: "Search UTF-8 text files by literal text or RE2-compatible regular expression. Supports case control, project-relative include/exclude globs, and bounded context lines. Skips binary files and common generated directories.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"scope":{"type":"string","enum":["app","skill","temp","project"],"description":"Target file area. Use app to search visible installed App package files."},"path":{"type":"string","description":"Search root. Relative path inside a managed area, or an absolute/relative path inside authorized project directories. Use . for the root."},"query":{"type":"string","description":"Text or regular expression to search for."},"mode":{"type":"string","enum":["literal","regex"],"description":"Search mode. Defaults to literal."},"case_sensitive":{"type":"boolean","description":"Whether matching is case-sensitive. Defaults to true."},"include_globs":{"type":"array","items":{"type":"string"},"maxItems":32,"description":"Optional project-relative path globs to include. Supports ** directory segments."},"exclude_globs":{"type":"array","items":{"type":"string"},"maxItems":32,"description":"Optional project-relative path globs to exclude. Supports ** directory segments."},"context_lines":{"type":"integer","minimum":0,"maximum":5,"description":"Context lines before and after each matching line. Defaults to 0."},"max_results":{"type":"integer","minimum":1,"maximum":500,"description":"Optional maximum matching lines, default 100 and cap 500."}},"required":["scope","path","query"],"additionalProperties":false}`),
			Capability:  store.ModeCode,
		},
		{
			Name:        FileSlice,
			Description: "Read a focused line slice from a UTF-8 text file. Supports origin=start for line ranges and origin=end for tail-style reads, with optional reverse order.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"scope":{"type":"string","enum":["app","skill","temp","project"],"description":"Target file area. Use app to inspect visible installed App package files."},"path":{"type":"string","description":"Relative file path inside a managed area, or an absolute/relative path inside authorized project directories."},"origin":{"type":"string","enum":["start","end"],"description":"start reads from a 1-based line range. end reads the last N lines after optional skip. Default start."},"start":{"type":"integer","description":"1-based start line for origin=start. Default 1."},"end":{"type":"integer","description":"Inclusive end line for origin=start. If omitted, lines controls the range length."},"lines":{"type":"integer","description":"Line count for origin=end or when end is omitted. Default 100 and cap 500."},"skip":{"type":"integer","description":"For origin=end, skip this many lines from the file end before taking lines. Default 0."},"order":{"type":"string","enum":["natural","reverse"],"description":"natural returns file order. reverse returns newest/end-most lines first. Default natural."}},"required":["scope","path"],"additionalProperties":false}`),
			Capability:  store.ModeCode,
		},
		{
			Name:        FileWrite,
			Description: "Overwrite one text file in a writable Pudding-managed file area or authorized project directory.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"scope":{"type":"string","enum":["skill","temp","project"],"description":"Target writable file area. Use skill for global user Skills and project for authorized local project directories."},"path":{"type":"string","description":"Relative file path inside a managed area, or an absolute/relative path inside authorized project directories."},"content":{"type":"string","description":"New file content."}},"required":["scope","path","content"],"additionalProperties":false}`),
			Capability:  store.ModeCode,
		},
		{
			Name:        FileDelete,
			Description: "Delete a file or, when recursive is true, a directory from a writable Pudding-managed file area or authorized project directory.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"scope":{"type":"string","enum":["skill","temp","project"],"description":"Target writable file area. Use skill for global user Skills and project for authorized local project directories."},"path":{"type":"string","description":"Relative path inside a managed area, or an absolute/relative path inside authorized project directories."},"recursive":{"type":"boolean","description":"Required to delete a directory."}},"required":["scope","path"],"additionalProperties":false}`),
			Capability:  store.ModeCode,
		},
		{
			Name:        FileMove,
			Description: "Move or rename a file or directory inside the same writable managed area or authorized project directories.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"scope":{"type":"string","enum":["skill","temp","project"]},"from_path":{"type":"string"},"to_path":{"type":"string"}},"required":["scope","from_path","to_path"],"additionalProperties":false}`),
			Capability:  store.ModeCode,
		},
		{
			Name:        FileCopy,
			Description: "Copy a file or directory inside the same writable managed area or authorized project directory. Directories require recursive=true; existing destinations require overwrite=true.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"scope":{"type":"string","enum":["skill","temp","project"],"description":"Target writable file area."},"from_path":{"type":"string","description":"Source path in the same scope."},"to_path":{"type":"string","description":"Destination path in the same scope."},"recursive":{"type":"boolean","description":"Required when copying a directory."},"overwrite":{"type":"boolean","description":"Replace an existing destination. Defaults false."}},"required":["scope","from_path","to_path"],"additionalProperties":false}`),
			Capability:  store.ModeCode,
		},
		{
			Name:        CommandRun,
			Description: "Run one fixed-shell command in an authorized project directory. The command may use pipelines, redirects, heredocs, and simple compound expressions. Use $TMPDIR instead of the shared /tmp directory for temporary files. Python packages needed only by sandboxed commands can be installed with python3 -m pip install --user; Pudding redirects that user base into isolated project state and supplies the system CA bundle. Do not disable TLS verification with --trusted-host. Foreground commands return bounded output and verification diagnostics. Set background=true for a persistent session and receive a process_id; set tty=true as well only for an interactive CLI or REPL. Background sessions have no runtime deadline and must be managed with builtin_command_session. In Auto, Pudding parses each command and keeps approved risk operations inside the project sandbox unless the invocation explicitly needs host access.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"scope":{"type":"string","enum":["project"],"description":"Commands can run only with project access."},"command":{"type":"string","minLength":1,"maxLength":65536,"description":"Complete command line executed by Pudding's fixed shell."},"cwd":{"type":"string","description":"Absolute or relative directory inside authorized project roots. Defaults to the first project root."},"env":{"type":"object","additionalProperties":{"type":"string"},"description":"Optional environment values for this command. Pudding otherwise inherits only a minimal safe environment."},"timeout_ms":{"type":"integer","minimum":100,"maximum":600000,"description":"Foreground timeout in milliseconds. Defaults to 60000; unavailable when background=true."},"background":{"type":"boolean","description":"Keep the command running as a session-owned process and return process_id. Defaults false."},"tty":{"type":"boolean","description":"Allocate a PTY for an interactive CLI. Requires background=true and is unavailable on Windows."}},"required":["scope","command"],"additionalProperties":false}`),
			Capability:  store.ModeCode,
		},
		{
			Name:        CommandSession,
			Description: "Manage one session-owned process returned by builtin_command_run with background=true. Use action=poll to read bounded ordered output, action=write to send exact stdin bytes (include a newline when needed), or action=stop to terminate the process group. Input is serialized and limited to 65536 bytes per call. PTY sessions support terminal control characters; ordinary background sessions receive pipe input.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"action":{"type":"string","enum":["poll","write","stop"]},"process_id":{"type":"string","description":"Process id returned by builtin_command_run."},"offset":{"type":"integer","minimum":0,"description":"For poll, continue from the previous nextOffset. Defaults to the oldest retained output."},"max_bytes":{"type":"integer","minimum":1024,"maximum":262144,"description":"For poll, maximum returned output bytes. Defaults to 65536."},"wait_ms":{"type":"integer","minimum":0,"maximum":600000,"description":"For poll, optionally wait for process exit, up to 600000 ms."},"data":{"type":"string","maxLength":65536,"description":"For write, exact input bytes as text. Include \\n to submit a line or \\u0003 to send Ctrl-C to a PTY."}},"required":["action","process_id"],"additionalProperties":false}`),
			Capability:  store.ModeCode,
		},
		{
			Name:        GitStatus,
			Description: "Read structured Git worktree status for the repository containing an authorized project directory. Returns branch, upstream divergence, file states, and summary counts.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"scope":{"type":"string","enum":["project"],"description":"Git tools can read only authorized project repositories."},"cwd":{"type":"string","description":"Absolute or relative directory inside an authorized project root. Defaults to the first project root."}},"required":["scope"],"additionalProperties":false}`),
			Capability:  store.ModeCode,
		},
		{
			Name:        GitDiff,
			Description: "Read the unstaged or staged Git diff for an authorized project repository. Returns a bounded patch plus structured per-file additions and deletions.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"scope":{"type":"string","enum":["project"],"description":"Git tools can read only authorized project repositories."},"cwd":{"type":"string","description":"Absolute or relative directory inside an authorized project root. Defaults to the first project root."},"staged":{"type":"boolean","description":"When true, read the staged diff. Defaults to false for unstaged changes."}},"required":["scope"],"additionalProperties":false}`),
			Capability:  store.ModeCode,
		},
		{
			Name:        GitLog,
			Description: "Read recent commits from an authorized project repository as structured metadata.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"scope":{"type":"string","enum":["project"],"description":"Git tools can read only authorized project repositories."},"cwd":{"type":"string","description":"Absolute or relative directory inside an authorized project root. Defaults to the first project root."},"limit":{"type":"integer","minimum":1,"maximum":100,"description":"Maximum commits to return. Defaults to 20."}},"required":["scope"],"additionalProperties":false}`),
			Capability:  store.ModeCode,
		},
		{
			Name:        GitStage,
			Description: "Stage explicit project file paths in Git after approval. Paths are literal repository-relative or absolute paths; arbitrary pathspecs and Git options are not accepted.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"scope":{"type":"string","enum":["project"],"description":"Git writes can target only authorized project repositories."},"cwd":{"type":"string","description":"Absolute or relative directory inside an authorized project root. Defaults to the first project root."},"paths":{"type":"array","items":{"type":"string"},"minItems":1,"maxItems":128,"description":"Explicit files to stage. Paths returned by builtin_git_status are repository-relative and can be passed directly."}},"required":["scope","paths"],"additionalProperties":false}`),
			Capability:  store.ModeCode,
		},
		{
			Name:        GitUnstage,
			Description: "Remove explicit project file paths from the Git index after approval without changing worktree files. Arbitrary pathspecs and Git options are not accepted.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"scope":{"type":"string","enum":["project"],"description":"Git writes can target only authorized project repositories."},"cwd":{"type":"string","description":"Absolute or relative directory inside an authorized project root. Defaults to the first project root."},"paths":{"type":"array","items":{"type":"string"},"minItems":1,"maxItems":128,"description":"Explicit repository files to unstage."}},"required":["scope","paths"],"additionalProperties":false}`),
			Capability:  store.ModeCode,
		},
		{
			Name:        GitCommit,
			Description: "Commit the currently staged Git changes after showing their status and diff for approval. The commit is rejected if HEAD or the index changes while approval is pending. Hooks and signing are disabled.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"scope":{"type":"string","enum":["project"],"description":"Git writes can target only authorized project repositories."},"cwd":{"type":"string","description":"Absolute or relative directory inside an authorized project root. Defaults to the first project root."},"message":{"type":"string","minLength":1,"maxLength":16384,"description":"Commit message. Amend, signing, hooks, and arbitrary commit options are not supported."}},"required":["scope","message"],"additionalProperties":false}`),
			Capability:  store.ModeCode,
		},
		{
			Name:        FilePatch,
			Description: "Apply one atomic multi-file text patch directly to authorized project files. Prefer ordered exact-match edits for existing files; use new_text for creates or intentional full replacement, and delete=true for deletion. All files are validated before writing, and any failure leaves the worktree unchanged.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"scope":{"type":"string","enum":["project"],"description":"Patches can target only authorized project files."},"files":{"type":"array","minItems":1,"maxItems":16,"items":{"type":"object","properties":{"path":{"type":"string","description":"Absolute or relative file path inside one authorized project root."},"new_text":{"type":"string","description":"Complete desired UTF-8 file content. Use for creating a file or an intentional full replacement."},"edits":{"type":"array","minItems":1,"maxItems":64,"items":{"type":"object","properties":{"old_text":{"type":"string","minLength":1,"description":"Exact existing text to replace in the current in-memory snapshot."},"new_text":{"type":"string","description":"Replacement UTF-8 text."},"replace_all":{"type":"boolean","description":"Replace every exact match. Defaults to false and requires old_text to be unique."}},"required":["old_text","new_text"],"additionalProperties":false},"description":"Ordered exact replacements for an existing file. Mutually exclusive with new_text and delete."},"delete":{"type":"boolean","description":"Set true to delete an existing regular text file. Mutually exclusive with new_text and edits."}},"required":["path"],"additionalProperties":false},"description":"All file changes. Every file must resolve inside the same project root."}},"required":["scope","files"],"additionalProperties":false}`),
			Capability:  store.ModeCode,
		},
		{
			Name:        SkillValidate,
			Description: "Validate one global user Skill after creating or editing it in the skill file scope.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"skill_id":{"type":"string","description":"Global user Skill id and directory name."}},"required":["skill_id"],"additionalProperties":false}`),
			Capability:  store.ModeCode,
		},
		{
			Name:        AppSave,
			Description: "Create or update one installed Pudding App from a complete text package. The package is validated in isolation and replaces the installed version only after validation succeeds. Use only when the user explicitly asks to create or modify an App; never include credentials or connection secrets.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"operation":{"type":"string","enum":["create","update"],"description":"create refuses to replace an installed App; update requires an existing installed App."},"app_id":{"type":"string","description":"Lowercase kebab-case App id. It must match id in app.yaml."},"version":{"type":"string","description":"Non-empty App package version. It must match version in app.yaml when declared."},"files":{"type":"array","minItems":1,"maxItems":64,"description":"Complete set of package-managed UTF-8 text files. app.yaml is required. Include every App file that should remain managed after this save.","items":{"type":"object","properties":{"path":{"type":"string","description":"Relative package path such as app.yaml, assets/icon.svg, or skills/issues/SKILL.md."},"content":{"type":"string","description":"Complete UTF-8 file content."}},"required":["path","content"],"additionalProperties":false}}},"required":["operation","app_id","version","files"],"additionalProperties":false}`),
			Capability:  store.ModeCode,
		},
		{
			Name:        RESTRequest,
			Description: "Send one HTTP request to a configured REST endpoint. Pass an endpoint name plus a relative path; authorization and configured headers are injected by Pudding. Omit connection when there is one configured connection.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"endpoint":{"type":"string","description":"Configured endpoint name, for example github_rest. Required; there is no default endpoint."},"connection":{"type":"string","description":"Optional connection name or id. Only pass this when the endpoint reports multiple configured connections."},"method":{"type":"string","enum":["GET","POST","PUT","PATCH","DELETE"],"description":"HTTP method. Defaults to GET."},"path":{"type":"string","description":"Relative path under the endpoint base URL, for example /repos/owner/repo/issues. Must not be a full URL."},"query":{"type":"object","additionalProperties":{"type":["string","number","boolean"]},"description":"Optional query parameters."},"body_json":{"description":"Optional JSON body. Mutually exclusive with body_text."},"body_text":{"type":"string","description":"Optional text body. Mutually exclusive with body_json."}},"required":["endpoint","path"],"additionalProperties":false}`),
			Capability:  store.ModeWork,
		},
		{
			Name:        GraphQLRequest,
			Description: "Send one GraphQL query or mutation to a configured GraphQL endpoint. Authorization is injected by Pudding. Omit connection when there is one configured connection.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"endpoint":{"type":"string","description":"Configured GraphQL endpoint name. Required; there is no default endpoint."},"connection":{"type":"string","description":"Optional connection name or id. Only pass this when the endpoint reports multiple configured connections."},"query":{"type":"string","description":"GraphQL query or mutation text."},"variables":{"description":"Optional GraphQL variables object or JSON object string."}},"required":["endpoint","query"],"additionalProperties":false}`),
			Capability:  store.ModeWork,
		},
		{
			Name:        GraphQLIntrospect,
			Description: "Inspect a configured GraphQL endpoint schema. Without type_name returns Query/Mutation/Subscription fields; with type_name returns that type's one-level structure.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"endpoint":{"type":"string","description":"Configured GraphQL endpoint name."},"connection":{"type":"string","description":"Optional connection name or id."},"type_name":{"type":"string","description":"Optional GraphQL type name to inspect, for example Query, User, or UserInput."},"force_refresh":{"type":"boolean","description":"Ignore cached schema and fetch again."}},"required":["endpoint"],"additionalProperties":false}`),
			Capability:  store.ModeWork,
		},
		{
			Name:        GraphQLSearch,
			Description: "Search names and descriptions in a configured GraphQL endpoint schema. Use before writing GraphQL when field/type names are uncertain.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"endpoint":{"type":"string","description":"Configured GraphQL endpoint name."},"connection":{"type":"string","description":"Optional connection name or id."},"query":{"type":"string","description":"Case-insensitive keywords. Space-separated keywords are ANDed."},"max_results":{"type":"integer","description":"Optional result count, default 30 and cap 50."},"force_refresh":{"type":"boolean","description":"Ignore cached schema and fetch again."}},"required":["endpoint","query"],"additionalProperties":false}`),
			Capability:  store.ModeWork,
		},
		{
			Name:        WeatherGet,
			Description: "Get current weather and optional short forecast for a location using wttr.in JSON. Useful for weather answers without doing web search.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"location":{"type":"string","description":"City/location. Empty uses IP-based approximate location."},"lang":{"type":"string","description":"Optional language, for example en, zh, zh-cn, zh-tw. Defaults to en."},"days":{"type":"integer","description":"Forecast days to include, 0-3. Defaults to 0 current-only."}},"additionalProperties":false}`),
			Capability:  store.ModeChat,
		},
		{
			Name:        CameraCapture,
			Description: "Take one photo from the local camera. The result is displayed automatically in the current conversation and includes displayMarkdown for explicitly showing the same photo again. The photo bytes are not routed to the model; call builtin_media_read with source=attachment only if the image content must be inspected.",
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
			Capability:  store.ModeWork,
		},
		{
			Name:        BrowserOpen,
			Description: "Open a URL in a managed browser tab. Pass tabID to reuse a tab, or newTab=true to create a separate tab. With multiple tabs, omitting both is an error.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"url":{"type":"string","description":"URL to open. http and https are supported; scheme defaults to https. file:// is allowed only for an existing regular file inside the current session project."},"tabID":{"type":"string","description":"Existing tab ID returned by builtin_browser_status."},"newTab":{"type":"boolean","description":"Create a new tab before opening the URL."}},"required":["url"],"additionalProperties":false}`),
			Capability:  store.ModeWork,
		},
		{
			Name:        BrowserObserve,
			Description: "Observe a managed browser tab: title, URL, visible page text, and interactive elements.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"tabID":{"type":"string","description":"Tab ID. Required when more than one tab exists."},"maxTextChars":{"type":"integer","description":"Optional max page text characters, default 6000, cap 20000."},"maxElements":{"type":"integer","description":"Optional max interactive elements, default 30, cap 100."}},"additionalProperties":false}`),
			Capability:  store.ModeWork,
		},
		{
			Name:        BrowserScreenshot,
			Description: "Capture a screenshot of a managed browser tab and route it as an image attachment.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"tabID":{"type":"string","description":"Tab ID. Required when more than one tab exists."},"fullPage":{"type":"boolean","description":"Capture beyond the viewport when supported. Defaults false."}},"additionalProperties":false}`),
			Capability:  store.ModeWork,
		},
		{
			Name:        BrowserBack,
			Description: "Navigate a managed browser tab back in history.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"tabID":{"type":"string","description":"Tab ID. Required when more than one tab exists."}},"additionalProperties":false}`),
			Capability:  store.ModeWork,
		},
		{
			Name:        BrowserForward,
			Description: "Navigate a managed browser tab forward in history.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"tabID":{"type":"string","description":"Tab ID. Required when more than one tab exists."}},"additionalProperties":false}`),
			Capability:  store.ModeWork,
		},
		{
			Name:        BrowserReload,
			Description: "Reload a managed browser tab.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"tabID":{"type":"string","description":"Tab ID. Required when more than one tab exists."}},"additionalProperties":false}`),
			Capability:  store.ModeWork,
		},
		{
			Name:        BrowserClose,
			Description: "Close one managed browser tab by tabID, or close all tabs in the current session when tabID is omitted.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"tabID":{"type":"string","description":"Optional tab ID to close. Omit to close all tabs in the session."}},"additionalProperties":false}`),
			Capability:  store.ModeWork,
		},
		{
			Name:        BrowserClick,
			Description: "Click an element in a managed browser tab by CSS selector or viewport coordinates using focus-preserving CDP target activation.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"tabID":{"type":"string","description":"Tab ID. Required when more than one tab exists."},"selector":{"type":"string","description":"CSS selector to click."},"x":{"type":"number","description":"Viewport X coordinate, used when selector is omitted."},"y":{"type":"number","description":"Viewport Y coordinate, used when selector is omitted."},"method":{"type":"string","enum":["auto","pointer"],"description":"Click implementation. Both values use focus-preserving target activation through CDP; auto is the default."}},"additionalProperties":false}`),
			Capability:  store.ModeWork,
		},
		{
			Name:        BrowserType,
			Description: "Type text into an input, textarea, or contenteditable element in a managed browser tab.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"tabID":{"type":"string","description":"Tab ID. Required when more than one tab exists."},"selector":{"type":"string","description":"CSS selector for an editable element. If omitted, uses the active element focused by a prior click."},"text":{"type":"string","description":"Text to type."},"clear":{"type":"boolean","description":"Replace existing text before typing."}},"required":["text"],"additionalProperties":false}`),
			Capability:  store.ModeWork,
		},
		{
			Name:        BrowserScroll,
			Description: "Scroll a managed browser tab or one of its scrollable elements.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"tabID":{"type":"string","description":"Tab ID. Required when more than one tab exists."},"selector":{"type":"string","description":"Optional CSS selector for a scrollable element."},"deltaX":{"type":"number","description":"Horizontal scroll delta in pixels."},"deltaY":{"type":"number","description":"Vertical scroll delta in pixels. Defaults to 600 when both deltas are omitted."}},"additionalProperties":false}`),
			Capability:  store.ModeWork,
		},
		{
			Name:        ComputerListApps,
			Description: "List installed user-facing macOS applications with appID, name, running state, active state, and controllable state. This returns no window titles or contents and is used only to discover an appID before builtin_computer_use_app. It is an inventory, not an authorization or allowlist. Apps with controllable=false are blocked by native safety policy. This is read-only.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`),
			Capability:  store.ModeWork,
		},
		{
			Name:        ComputerUseApp,
			Description: "Use one installed macOS application by appID: start it when stopped or refresh its current PID-bound windows when already running. It runs in the background by default and does not activate or raise an already-running app. Set foreground=true when the user explicitly asks to show, focus, or switch to the app, or before necessary pointer input; semantic Accessibility actions remain background-capable. This is the only supported entry point for an app that will be used through Computer Use; never substitute command_run, open, osascript, or AppleScript. The result is the only source of target windows and includes windowStatus plus PID-bound window IDs. ready means windows can be used; none means the app has no discoverable on-screen window; permission_required means Screen Recording must be enabled; failed includes windowError. Use returned windows directly for observation or action; call this tool again, not builtin_computer_list_apps, when a stale window must be reacquired. If the user only asked to open the app, stop after this tool succeeds. Opening and operating the same app share one session approval. If this session newly starts it, the result includes a launchID and only that launchID may later be quit by this session. If it was already running, no launchID is returned and Pudding does not own or close it.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"appID":{"type":"string","minLength":1,"maxLength":512,"description":"Installed application bundle identifier, such as com.apple.calculator."},"foreground":{"type":"boolean","description":"Activate and show the app. Defaults false; required before coordinate pointer input."}},"required":["appID"],"additionalProperties":false}`),
			Capability:  store.ModeWork,
		},
		{
			Name:        ComputerQuitApp,
			Description: "Request a normal quit for an application newly launched by this exact session, using its launchID. Never force-quits. If closed=false, the app is still open, commonly because it needs user attention such as an unsaved-changes confirmation; stop and ask the user to handle it.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"launchID":{"type":"string","minLength":1,"description":"Session-owned launchID returned by builtin_computer_use_app."}},"required":["launchID"],"additionalProperties":false}`),
			Capability:  store.ModeWork,
		},
		{
			Name:        ComputerObserve,
			Description: "Observe one explicit macOS application window through Accessibility. Returns a short-lived observationID and stable element IDs. Optionally capture one screenshot frame; no keyboard or mouse input is monitored.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"appID":{"type":"string","minLength":1,"maxLength":512,"description":"Application bundle identifier returned by builtin_computer_use_app."},"windowID":{"type":"integer","minimum":1,"description":"Explicit PID-bound window ID returned by builtin_computer_use_app."},"maxElements":{"type":"integer","minimum":1,"maximum":1000,"description":"Maximum accessibility elements, default 200. Increase only when a truncated observation omitted the target."},"includeScreenshot":{"type":"boolean","description":"Capture one frame in the same native request as the accessibility observation. Defaults false and requires Screen Recording permission. Use for model inspection only when the current model supports image input; otherwise use only when the user explicitly asks to receive a screenshot."}},"required":["appID","windowID"],"additionalProperties":false}`),
			Capability:  store.ModeWork,
		},
		{
			Name:        ComputerAct,
			Description: "Perform one approved Computer Use action from a fresh observation. Semantic actions press, set_value, select, and submit require elementID and may run in the background. Pointer actions click, drag, and scroll require the target app to remain foreground plus coordinates from the exact screenshot returned with that observation; they use top-left screenshot pixels, move the system pointer, and reject plain or post-action observations. Click supports one left or right click, or a left double-click. Drag uses the left button. Scroll uses pixel deltas where positive deltaY scrolls down and positive deltaX scrolls right. Never guess coordinates. Pointer success confirms input delivery only; obtain a fresh screenshot before claiming a visual effect when Accessibility cannot expose it. Use select for selectable rows or items. Use submit only when the observation exposes it on a focused, enabled, editable single-line text control; it invokes AXConfirm when available, otherwise sends exactly one Return key event directly to that app process with no modifiers or repeat. The input observation is consumed once and actions are never retried automatically. A successful result normally includes a fresh semantic observation; use it for the next semantic action, but obtain a new screenshot observation before another pointer action. For computer_app_not_foreground, call builtin_computer_use_app with foreground=true and obtain a fresh screenshot before deciding again. For outcome=unknown, never repeat the action and inspect current state. For outcome=not_started, obtain a fresh observation before deciding again. If observationError reports computer_window_not_found, reacquire windows with builtin_computer_use_app instead of observing the stale window ID.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"appID":{"type":"string","minLength":1,"maxLength":512},"windowID":{"type":"integer","minimum":1},"observationID":{"type":"string","minLength":1,"description":"Fresh observationID returned by builtin_computer_observe or a prior successful action."},"elementID":{"type":"string","minLength":1,"description":"Required for press, set_value, select, and submit; omit for pointer actions."},"action":{"type":"string","enum":["press","set_value","select","submit","click","drag","scroll"]},"value":{"type":"string","maxLength":20000,"description":"Required only for set_value."},"x":{"type":"number","minimum":0,"description":"Required for pointer actions: x in pixels from the screenshot's left edge."},"y":{"type":"number","minimum":0,"description":"Required for pointer actions: y in pixels from the screenshot's top edge."},"toX":{"type":"number","minimum":0,"description":"Required only for drag: destination x in screenshot pixels."},"toY":{"type":"number","minimum":0,"description":"Required only for drag: destination y in screenshot pixels."},"button":{"type":"string","enum":["left","right"],"description":"Click button; defaults to left."},"clickCount":{"type":"integer","minimum":1,"maximum":2,"description":"Click count; defaults to 1. Two is allowed only for the left button."},"deltaX":{"type":"integer","minimum":-5000,"maximum":5000,"description":"Scroll pixels; positive moves right. Defaults to 0."},"deltaY":{"type":"integer","minimum":-5000,"maximum":5000,"description":"Scroll pixels; positive moves down. Defaults to 0."}},"required":["appID","windowID","observationID","action"],"additionalProperties":false}`),
			Capability:  store.ModeWork,
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
	case PlanUpdate:
		return planUpdate(call)
	case CodeSymbols:
		return r.codeSymbols(ctx, call)
	case CodeDefinition:
		return r.codeDefinition(ctx, call)
	case CodeReferences:
		return r.codeReferences(ctx, call)
	case CodeDiagnostics:
		return r.codeDiagnostics(ctx, call)
	case CodeRename:
		return r.codeRename(ctx, call)
	case FileList:
		return r.fileList(call)
	case FileRead:
		return r.fileRead(call)
	case MediaRead:
		return r.mediaRead(call)
	case AttachmentExport:
		return r.attachmentExport(call)
	case FileStat:
		return r.fileStat(call)
	case FileSearch:
		return r.fileSearch(ctx, call)
	case FileSlice:
		return r.fileSlice(call)
	case FileWrite:
		return r.fileWrite(call)
	case FileDelete:
		return r.fileDelete(call)
	case FileMove:
		return r.fileMove(call)
	case FileCopy:
		return r.fileCopy(call)
	case CommandRun:
		return r.commandRun(ctx, call)
	case CommandSession:
		return r.commandSession(ctx, call)
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
	case FilePatch:
		return r.filePatch(call)
	case SkillValidate:
		return r.skillValidate(ctx, call)
	case AppSave:
		return r.appSave(ctx, call)
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
	case ComputerListApps:
		return r.computerListApps(ctx, call)
	case ComputerUseApp:
		return r.computerUseApp(ctx, call)
	case ComputerQuitApp:
		return r.computerQuitApp(ctx, call)
	case ComputerObserve:
		return r.computerObserve(ctx, call)
	case ComputerAct:
		return r.computerAct(ctx, call)
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
