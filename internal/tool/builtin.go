package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/teatak/pudding-core/internal/app"
	"github.com/teatak/pudding-core/internal/provider"
	"github.com/teatak/pudding-core/internal/skill"
	"github.com/teatak/pudding-core/internal/store"
)

const (
	TimeGetCurrent = "builtin_time_get_current"
	WebSearch      = "builtin_web_search"
	WebFetch       = "builtin_web_fetch"
	WorkspaceList  = "builtin_workspace_list"
	SkillRead      = "builtin_skill_read"
	RESTRequest    = "builtin_rest_request"
	GraphQLRequest = "builtin_graphql_request"
)

type WebConfigSource interface {
	TavilyAPIKey(ctx context.Context) (string, bool, error)
}

type AppEndpointSource interface {
	ResolveEndpoint(ctx context.Context, sessionID, endpointName string) (*app.EndpointBinding, error)
}

type SkillSource interface {
	ReadSkill(ctx context.Context, id string) (*skill.Document, error)
}

type BuiltinOption func(*BuiltinRunner)

type BuiltinRunner struct {
	webConfig     WebConfigSource
	appEndpoints  AppEndpointSource
	skills        SkillSource
	webHTTPClient *http.Client
	tavilySearch  string
	tavilyExtract string
}

func NewBuiltinRunner(opts ...BuiltinOption) *BuiltinRunner {
	r := &BuiltinRunner{
		webHTTPClient: &http.Client{Timeout: webDefaultTimeout},
		tavilySearch:  tavilySearchEndpoint,
		tavilyExtract: tavilyExtractEndpoint,
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

func WithSkills(source SkillSource) BuiltinOption {
	return func(r *BuiltinRunner) {
		r.skills = source
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
			Capability:  store.ModeResearch,
		},
		{
			Name:        WebFetch,
			Description: "Fetch readable body text from one URL. Use for page reading and summarization, not API calls.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"url":{"type":"string","description":"Full URL starting with http:// or https://."},"depth":{"type":"string","enum":["basic","advanced"],"description":"Optional extract depth. Defaults to basic."},"max_chars":{"type":"integer","description":"Optional maximum body characters, default 4000 and cap 20000."}},"required":["url"],"additionalProperties":false}`),
			Capability:  store.ModeResearch,
		},
		{
			Name:        WorkspaceList,
			Description: "List files and directories inside authorized workspace directories.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string","description":"Relative path from an authorized workspace root, or an absolute path inside one. Defaults to ."},"maxEntries":{"type":"integer","description":"Optional maximum entries, 1-1000, default 200."}},"additionalProperties":false}`),
			Capability:  store.ModeWorkspace,
		},
		{
			Name:        SkillRead,
			Description: "Read the full SKILL.md body for one registered skill after the user's intent clearly matches the skill index.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"skill_id":{"type":"string","description":"Registered skill id from the Available Skills index, for example skill-creator."}},"required":["skill_id"],"additionalProperties":false}`),
			Capability:  store.ModeChat,
		},
		{
			Name:        RESTRequest,
			Description: "Send one HTTP request to a session-granted REST endpoint. Pass an endpoint name plus a relative path; authorization and configured headers are injected by Pudding.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"endpoint":{"type":"string","description":"Configured endpoint name, for example github_rest. Required; there is no default endpoint."},"method":{"type":"string","enum":["GET","POST","PUT","PATCH","DELETE"],"description":"HTTP method. Defaults to GET."},"path":{"type":"string","description":"Relative path under the endpoint base URL, for example /repos/owner/repo/issues. Must not be a full URL."},"query":{"type":"object","additionalProperties":{"type":["string","number","boolean"]},"description":"Optional query parameters."},"body_json":{"description":"Optional JSON body. Mutually exclusive with body_text."},"body_text":{"type":"string","description":"Optional text body. Mutually exclusive with body_json."}},"required":["endpoint","path"],"additionalProperties":false}`),
			Capability:  store.ModeResearch,
		},
		{
			Name:        GraphQLRequest,
			Description: "Send one GraphQL query or mutation to a session-granted GraphQL endpoint. Authorization is injected by Pudding.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"endpoint":{"type":"string","description":"Configured GraphQL endpoint name. Required; there is no default endpoint."},"query":{"type":"string","description":"GraphQL query or mutation text."},"variables":{"description":"Optional GraphQL variables object or JSON object string."}},"required":["endpoint","query"],"additionalProperties":false}`),
			Capability:  store.ModeResearch,
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
	case WorkspaceList:
		return workspaceList(call)
	case SkillRead:
		return r.skillRead(ctx, call)
	case RESTRequest:
		return r.restRequest(ctx, call)
	case GraphQLRequest:
		return r.graphqlRequest(ctx, call)
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
