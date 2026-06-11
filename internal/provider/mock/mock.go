// Package mock 按脚本回放模型流,延迟、失败、cancel 时序可注入
// (docs/phase-1-plan.md 第 4 节)。engine 单测与 puddingd --mock 共用。
package mock

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/teatak/pudding-core/internal/provider"
)

type Option func(*Client)

// WithScript 固定回放的 delta 序列;不设置时按输入回显(echo)生成。
func WithScript(deltas []string) Option {
	return func(c *Client) { c.script = deltas }
}

// WithDelay 设置相邻 chunk 之间的间隔,用于模拟流式节奏与测试 cancel 时序。
func WithDelay(d time.Duration) Option {
	return func(c *Client) { c.delay = d }
}

// WithError 在产出 n 个 delta 后注入失败终止(n 可为 0)。
func WithError(afterDeltas int, err error) Option {
	return func(c *Client) { c.failAfter, c.failErr = afterDeltas, err }
}

type Client struct {
	script    []string
	delay     time.Duration
	failAfter int
	failErr   error
}

func New(opts ...Option) *Client {
	c := &Client{delay: 50 * time.Millisecond, failAfter: -1}
	for _, o := range opts {
		o(c)
	}
	return c
}

var _ provider.Client = (*Client)(nil)

func (c *Client) Name() string { return "mock" }

func (c *Client) Stream(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	deltas := c.script
	if deltas == nil {
		deltas = echoDeltas(req)
	}
	out := make(chan provider.Chunk)
	go func() {
		defer close(out)
		for i, d := range deltas {
			if c.failAfter >= 0 && i >= c.failAfter {
				out <- provider.Chunk{Err: c.failErr}
				return
			}
			select {
			case <-ctx.Done():
				out <- provider.Chunk{Err: ctx.Err()}
				return
			case <-time.After(c.delay):
			}
			select {
			case out <- provider.Chunk{Delta: d}:
			case <-ctx.Done():
				out <- provider.Chunk{Err: ctx.Err()}
				return
			}
		}
		if c.failAfter >= 0 && c.failAfter >= len(deltas) {
			out <- provider.Chunk{Err: c.failErr}
			return
		}
		out <- provider.Chunk{Done: true}
	}()
	return out, nil
}

// echoDeltas 把最后一条 user 输入切成词级 delta 回显,让 --mock 模式下
// 的流式行为肉眼可辨。
func echoDeltas(req provider.Request) []string {
	last := ""
	for _, m := range req.Messages {
		if m.Role == provider.RoleUser {
			last = m.Text
		}
	}
	words := strings.Fields(fmt.Sprintf("mock(%s) 收到 %d 条消息,回显: %s", req.Model, len(req.Messages), last))
	deltas := make([]string, 0, len(words))
	for i, w := range words {
		if i > 0 {
			w = " " + w
		}
		deltas = append(deltas, w)
	}
	return deltas
}
