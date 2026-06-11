// Package provider 定义 LLM provider 契约。
//
// 边界(AGENTS.md 硬约束 9 / 17):
//   - Client 只产出模型流(delta / finish / error);turn lifecycle 事件由 engine 生成。
//   - 实现不得保存跨 turn 事实源,每次请求由 canonical messages + current input 构造。
package provider

import "context"

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
}

type Role string

const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
)

type Message struct {
	Role Role
	Text string
}

// Chunk 是模型流的最小单元:Delta 增量文本;Done 正常收尾;Err 异常终止。
// Done 与 Err 之后不得再有 chunk。
type Chunk struct {
	Delta string
	Done  bool
	Err   error
}
