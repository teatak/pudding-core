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
	FileList          = "builtin_file_list"
	FileRead          = "builtin_file_read"
	FileStat          = "builtin_file_stat"
	FileSearch        = "builtin_file_search"
	FileSlice         = "builtin_file_slice"
	FileWrite         = "builtin_file_write"
	FilePatch         = "builtin_file_patch"
	FileDelete        = "builtin_file_delete"
	FileMove          = "builtin_file_move"
	FileCopy          = "builtin_file_copy"
	SkillValidate     = "builtin_skill_validate"
	SkillSubmit       = "builtin_skill_submit"
	RESTRequest       = "builtin_rest_request"
	GraphQLRequest    = "builtin_graphql_request"
	GraphQLIntrospect = "builtin_graphql_introspect"
	GraphQLSearch     = "builtin_graphql_search"
	WeatherGet        = "builtin_weather_get"
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

type BuiltinOption func(*BuiltinRunner)

type BuiltinRunner struct {
	webConfig       WebConfigSource
	appEndpoints    AppEndpointSource
	appSkills       AppSkillReader
	skillReader     SkillReader
	skillDrafts     SkillDraftSource
	history         HistorySearchSource
	historyMessages HistoryMessageSource
	homeDir         string
	webHTTPClient   *http.Client
	tavilySearch    string
	tavilyExtract   string
	weatherEndpoint string
	weatherMu       sync.Mutex
	weatherCache    map[string]weatherCacheEntry
	graphqlSchemaMu sync.Mutex
	graphqlSchemas  map[string]*graphqlSchemaCache
}

func NewBuiltinRunner(opts ...BuiltinOption) *BuiltinRunner {
	r := &BuiltinRunner{
		webHTTPClient:   &http.Client{Timeout: webDefaultTimeout},
		tavilySearch:    tavilySearchEndpoint,
		tavilyExtract:   tavilyExtractEndpoint,
		weatherEndpoint: weatherDefaultEndpoint,
		weatherCache:    map[string]weatherCacheEntry{},
		graphqlSchemas:  map[string]*graphqlSchemaCache{},
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
			Name:        FileList,
			Description: "List files in a Pudding-managed file area or an authorized workspace directory.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"scope":{"type":"string","enum":["skill_draft","skill_published","temp","workspace"],"description":"Target file area. Use workspace for authorized local workspace directories."},"path":{"type":"string","description":"Relative path inside a managed area, or an absolute/relative path inside authorized workspace directories. Use . to list the root."},"max_entries":{"type":"integer","description":"Optional maximum entries, 1-1000, default 200."}},"required":["scope","path"],"additionalProperties":false}`),
			Capability:  store.ModeWorkspace,
		},
		{
			Name:        FileRead,
			Description: "Read one small UTF-8 text file from a Pudding-managed file area or an authorized workspace directory. For large files use builtin_file_slice or builtin_file_search first.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"scope":{"type":"string","enum":["skill_draft","skill_published","temp","workspace"],"description":"Target file area. Use workspace for authorized local workspace directories."},"path":{"type":"string","description":"Relative file path inside a managed area, or an absolute/relative path inside authorized workspace directories."},"max_chars":{"type":"integer","description":"Optional max characters, default 20000 and cap 100000."}},"required":["scope","path"],"additionalProperties":false}`),
			Capability:  store.ModeWorkspace,
		},
		{
			Name:        FileStat,
			Description: "Return metadata for one file or directory: exists, type, size, mtime, and MIME when available.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"scope":{"type":"string","enum":["skill_draft","skill_published","temp","workspace"],"description":"Target file area. Use workspace for authorized local workspace directories."},"path":{"type":"string","description":"Relative path inside a managed area, or an absolute/relative path inside authorized workspace directories."}},"required":["scope","path"],"additionalProperties":false}`),
			Capability:  store.ModeWorkspace,
		},
		{
			Name:        FileSearch,
			Description: "Search UTF-8 text files by literal case-sensitive substring under one file or directory. Skips binary files and common generated directories.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"scope":{"type":"string","enum":["skill_draft","skill_published","temp","workspace"],"description":"Target file area. Use workspace for authorized local workspace directories."},"path":{"type":"string","description":"Search root. Relative path inside a managed area, or an absolute/relative path inside authorized workspace directories. Use . for the root."},"query":{"type":"string","description":"Literal case-sensitive substring to search for. Not regex."},"max_results":{"type":"integer","description":"Optional maximum matches, default 100 and cap 500."}},"required":["scope","path","query"],"additionalProperties":false}`),
			Capability:  store.ModeWorkspace,
		},
		{
			Name:        FileSlice,
			Description: "Read a focused line slice from a UTF-8 text file. Supports origin=start for line ranges and origin=end for tail-style reads, with optional reverse order.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"scope":{"type":"string","enum":["skill_draft","skill_published","temp","workspace"],"description":"Target file area. Use workspace for authorized local workspace directories."},"path":{"type":"string","description":"Relative file path inside a managed area, or an absolute/relative path inside authorized workspace directories."},"origin":{"type":"string","enum":["start","end"],"description":"start reads from a 1-based line range. end reads the last N lines after optional skip. Default start."},"start":{"type":"integer","description":"1-based start line for origin=start. Default 1."},"end":{"type":"integer","description":"Inclusive end line for origin=start. If omitted, lines controls the range length."},"lines":{"type":"integer","description":"Line count for origin=end or when end is omitted. Default 100 and cap 500."},"skip":{"type":"integer","description":"For origin=end, skip this many lines from the file end before taking lines. Default 0."},"order":{"type":"string","enum":["natural","reverse"],"description":"natural returns file order. reverse returns newest/end-most lines first. Default natural."}},"required":["scope","path"],"additionalProperties":false}`),
			Capability:  store.ModeWorkspace,
		},
		{
			Name:        FileWrite,
			Description: "Overwrite one text file in a writable Pudding-managed file area or authorized workspace directory.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"scope":{"type":"string","enum":["skill_draft","temp","workspace"],"description":"Target writable file area. Use workspace for authorized local workspace directories."},"path":{"type":"string","description":"Relative file path inside a managed area, or an absolute/relative path inside authorized workspace directories."},"content":{"type":"string","description":"New file content."}},"required":["scope","path","content"],"additionalProperties":false}`),
			Capability:  store.ModeWorkspace,
		},
		{
			Name:        FilePatch,
			Description: "Replace text in one file in a writable Pudding-managed file area or authorized workspace directory by exact string match.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"scope":{"type":"string","enum":["skill_draft","temp","workspace"],"description":"Target writable file area. Use workspace for authorized local workspace directories."},"path":{"type":"string","description":"Relative file path inside a managed area, or an absolute/relative path inside authorized workspace directories."},"old_string":{"type":"string","description":"Exact text to replace."},"new_string":{"type":"string","description":"Replacement text."},"replace_all":{"type":"boolean","description":"Replace all matches. Defaults false; without this, exactly one match is required."}},"required":["scope","path","old_string","new_string"],"additionalProperties":false}`),
			Capability:  store.ModeWorkspace,
		},
		{
			Name:        FileDelete,
			Description: "Delete a file or, when recursive is true, a directory from a writable Pudding-managed file area or authorized workspace directory.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"scope":{"type":"string","enum":["skill_draft","temp","workspace"],"description":"Target writable file area. Use workspace for authorized local workspace directories."},"path":{"type":"string","description":"Relative path inside a managed area, or an absolute/relative path inside authorized workspace directories."},"recursive":{"type":"boolean","description":"Required to delete a directory."}},"required":["scope","path"],"additionalProperties":false}`),
			Capability:  store.ModeWorkspace,
		},
		{
			Name:        FileMove,
			Description: "Move or rename a file or directory inside the same writable managed area or authorized workspace directories.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"scope":{"type":"string","enum":["skill_draft","temp","workspace"]},"from_path":{"type":"string"},"to_path":{"type":"string"}},"required":["scope","from_path","to_path"],"additionalProperties":false}`),
			Capability:  store.ModeWorkspace,
		},
		{
			Name:        FileCopy,
			Description: "Copy a file or directory inside the same writable managed area or authorized workspace directory. Directories require recursive=true; existing destinations require overwrite=true.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"scope":{"type":"string","enum":["skill_draft","temp","workspace"],"description":"Target writable file area."},"from_path":{"type":"string","description":"Source path in the same scope."},"to_path":{"type":"string","description":"Destination path in the same scope."},"recursive":{"type":"boolean","description":"Required when copying a directory."},"overwrite":{"type":"boolean","description":"Replace an existing destination. Defaults false."}},"required":["scope","from_path","to_path"],"additionalProperties":false}`),
			Capability:  store.ModeWorkspace,
		},
		{
			Name:        SkillValidate,
			Description: "Validate one staged skill package before asking the user to review it.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"draft_id":{"type":"string","description":"Staged skill package id."}},"required":["draft_id"],"additionalProperties":false}`),
			Capability:  store.ModeWorkspace,
		},
		{
			Name:        SkillSubmit,
			Description: "Submit one valid staged skill package for user review in Settings. This does not publish the skill.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"draft_id":{"type":"string","description":"Staged skill package id."}},"required":["draft_id"],"additionalProperties":false}`),
			Capability:  store.ModeWorkspace,
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
	case FileList:
		return r.fileList(call)
	case FileRead:
		return r.fileRead(call)
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
