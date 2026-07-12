// Package provider 定义 LLM provider 契约。
//
// 边界(AGENTS.md 硬约束 9 / 17):
//   - Client 只产出模型流(delta / finish / error);turn lifecycle 事件由 engine 生成。
//   - 实现不得保存跨 turn 事实源,每次请求由 canonical messages + current input 构造。
package provider

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strconv"

	"github.com/teatak/pudding-core/internal/store"
)

type Client interface {
	Name() string
	// Stream 发起一次补全并流式返回。返回的 channel 由实现负责关闭;
	// 终止条件是 Done 或 Err 的终止 chunk,或 ctx 取消(实现必须尽快收流)。
	Stream(ctx context.Context, req Request) (<-chan Chunk, error)
}

// Request 是 provider 无关的输入形状。System 独立成字段而不混入 Messages,
// 因为 Gemini 等 provider 的 system instruction 与对话内容不同构。
type Request struct {
	Model    string
	System   string
	Messages []Message
	Config   ModelConfig
	Tools    []ToolDef
}

// ModelConfig 是 provider-neutral 的 resolved model 配置快照。各 provider
// 只读取自己命名空间下支持的字段;未知字段保留在 snapshot 中供后续能力使用。
type ModelConfig struct {
	ContextWindow   int                   `json:"contextWindow,omitempty"`
	Capabilities    *ModelCapabilities    `json:"capabilities,omitempty"`
	Limits          *ModelLimits          `json:"limits,omitempty"`
	ProviderOptions *ModelProviderOptions `json:"providerOptions,omitempty"`
}

type ModelCapabilities struct {
	Image bool `json:"image"`
	Audio bool `json:"audio"`
	Tools bool `json:"tools"`
}

type ModelLimits struct {
	MaxOutputTokens int `json:"maxOutputTokens,omitempty"`
	MaxToolLoops    int `json:"maxToolLoops,omitempty"`
}

type ModelProviderOptions struct {
	OpenAI    map[string]any `json:"openai,omitempty"`
	Google    map[string]any `json:"google,omitempty"`
	Anthropic map[string]any `json:"anthropic,omitempty"`
}

func (c ModelConfig) OpenAIOptions() map[string]any {
	if c.ProviderOptions == nil {
		return nil
	}
	return c.ProviderOptions.OpenAI
}

func (c ModelConfig) GoogleOptions() map[string]any {
	if c.ProviderOptions == nil {
		return nil
	}
	return c.ProviderOptions.Google
}

func (c ModelConfig) AnthropicOptions() map[string]any {
	if c.ProviderOptions == nil {
		return nil
	}
	return c.ProviderOptions.Anthropic
}

func (c ModelConfig) MaxOutputTokens() (int, bool) {
	if c.Limits == nil || c.Limits.MaxOutputTokens <= 0 {
		return 0, false
	}
	return c.Limits.MaxOutputTokens, true
}

func (c ModelConfig) MaxToolLoops() (int, bool) {
	if c.Limits == nil || c.Limits.MaxToolLoops <= 0 {
		return 0, false
	}
	return c.Limits.MaxToolLoops, true
}

type Role string

const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
)

type Message struct {
	Role  Role
	Text  string
	Parts []Part
}

type ToolDef struct {
	Name        string
	Description string
	InputSchema json.RawMessage
	Capability  store.AgentMode
	// AppID is engine routing metadata and is never sent to the provider.
	AppID string `json:"-"`
}

type PartType string

const (
	PartText       PartType = "text"
	PartThought    PartType = "thought"
	PartImage      PartType = "image"
	PartAudio      PartType = "audio"
	PartToolUse    PartType = "tool_use"
	PartToolResult PartType = "tool_result"
)

type Part struct {
	Type    PartType        `json:"type"`
	Text    string          `json:"text,omitempty"`
	CallID  string          `json:"id,omitempty"`
	Name    string          `json:"name,omitempty"`
	Args    json.RawMessage `json:"args,omitempty"`
	Ok      bool            `json:"ok,omitempty"`
	Content string          `json:"content,omitempty"`
	MIME    string          `json:"mime,omitempty"`
	Data    []byte          `json:"-"`
}

func ImageDataURL(mime string, data []byte) string {
	if mime == "" {
		mime = "image/png"
	}
	return "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(data)
}

func AudioFormat(mime string) string {
	switch mime {
	case "audio/mpeg", "audio/mp3":
		return "mp3"
	case "audio/mp4", "audio/m4a":
		return "m4a"
	case "audio/aac":
		return "aac"
	case "audio/ogg":
		return "ogg"
	case "audio/flac":
		return "flac"
	case "audio/opus":
		return "opus"
	case "audio/webm":
		return "webm"
	default:
		return "wav"
	}
}

type FinishReason string

const (
	FinishStop      FinishReason = "stop"
	FinishToolCalls FinishReason = "tool_calls"
)

type ToolCallChunk struct {
	Index     int
	CallID    string
	Name      string
	ArgsDelta string
}

type UsageInfo struct {
	InputUncachedTokens   int
	InputCachedTokens     int
	CacheCreationTokens   int
	OutputContentTokens   int
	OutputReasoningTokens int
}

func (u UsageInfo) Empty() bool {
	return u.InputUncachedTokens == 0 &&
		u.InputCachedTokens == 0 &&
		u.CacheCreationTokens == 0 &&
		u.OutputContentTokens == 0 &&
		u.OutputReasoningTokens == 0
}

// Chunk 是模型流的最小单元:Delta 增量文本;Done 正常收尾;Err 异常终止。
// Done 与 Err 之后不得再有 chunk。
type Chunk struct {
	Part   PartType
	Delta  string
	Tool   *ToolCallChunk
	Usage  *UsageInfo
	Done   bool
	Finish FinishReason
	Err    error
}

func FloatOption(opts map[string]any, names ...string) (float64, bool) {
	v, ok := optionValue(opts, names...)
	if !ok {
		return 0, false
	}
	switch x := v.(type) {
	case float64:
		return x, true
	case float32:
		return float64(x), true
	case int:
		return float64(x), true
	case int64:
		return float64(x), true
	case json.Number:
		f, err := x.Float64()
		return f, err == nil
	case string:
		f, err := strconv.ParseFloat(x, 64)
		return f, err == nil
	default:
		return 0, false
	}
}

func IntOption(opts map[string]any, names ...string) (int, bool) {
	v, ok := optionValue(opts, names...)
	if !ok {
		return 0, false
	}
	switch x := v.(type) {
	case int:
		return x, true
	case int64:
		return int(x), true
	case int32:
		return int(x), true
	case float64:
		return int(x), x == float64(int(x))
	case float32:
		return int(x), x == float32(int(x))
	case json.Number:
		i, err := x.Int64()
		return int(i), err == nil
	case string:
		i, err := strconv.Atoi(x)
		return i, err == nil
	default:
		return 0, false
	}
}

func StringOption(opts map[string]any, names ...string) (string, bool) {
	v, ok := optionValue(opts, names...)
	if !ok {
		return "", false
	}
	switch x := v.(type) {
	case string:
		return x, x != ""
	default:
		return "", false
	}
}

func optionValue(opts map[string]any, names ...string) (any, bool) {
	for _, name := range names {
		v, ok := opts[name]
		if ok && v != nil {
			return v, true
		}
	}
	return nil, false
}
