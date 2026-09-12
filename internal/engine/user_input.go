package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/tool"
)

// Waiting is turn-scoped and cancellable. Only canonical tool calls/results
// survive a turn; this map is not a second history or persistent question store.
type UserInputRequest struct {
	ID        string          `json:"id"`
	SessionID string          `json:"sessionID"`
	TurnID    string          `json:"turnID"`
	Title     string          `json:"title"`
	Args      json.RawMessage `json:"args"`
	Status    string          `json:"status"`
	Deadline  *time.Time      `json:"deadline,omitempty"`
}

type pendingUserInput struct {
	UserInputRequest
	ctx  context.Context
	wake chan struct{}
}

type UserInputAction struct {
	Action string              `json:"action"`
	Text   string              `json:"text,omitempty"`
	Parts  []store.ContentPart `json:"parts,omitempty"`
}

type UserInputReply struct {
	Request UserInputRequest `json:"request"`
	*SubmitResult
}

func (e *Engine) requestUserInput(ctx context.Context, call tool.Call) tool.Result {
	var args struct {
		Title       string `json:"title"`
		WaitSeconds *int   `json:"waitSeconds"`
	}
	seconds := 60
	if err := json.Unmarshal(call.Args, &args); err != nil {
		return tool.Result{CallID: call.CallID, Name: call.Name, Content: "invalid question arguments"}
	}
	if args.WaitSeconds != nil {
		seconds = *args.WaitSeconds
	}
	if seconds < 0 || seconds > 300 {
		return tool.Result{CallID: call.CallID, Name: call.Name, Content: "waitSeconds must be an integer from 0 to 300"}
	}
	p := &pendingUserInput{UserInputRequest: UserInputRequest{
		ID: call.TurnID + ":" + call.CallID, SessionID: call.SessionID, TurnID: call.TurnID,
		Title: args.Title, Args: append(json.RawMessage(nil), call.Args...), Status: "awaiting_user",
	}, ctx: ctx, wake: make(chan struct{}, 1)}
	if seconds > 0 {
		// 截止时间在提问时固定：交互不续期，waitSeconds 是本次等待的硬上限。
		deadline := time.Now().Add(time.Duration(seconds) * time.Second)
		p.Status, p.Deadline = "waiting", &deadline
	}
	e.mu.Lock()
	if e.inputRequests == nil {
		e.inputRequests = make(map[string]*pendingUserInput)
	}
	e.inputRequests[call.SessionID+":"+p.ID] = p
	e.mu.Unlock()
	// The UI RPC only acknowledges display; human waiting is not subject to
	// the ordinary tool RPC timeout and does not occupy a provider request.
	uiCtx, cancel := context.WithTimeout(ctx, toolCallTimeout)
	result := e.tools.Call(uiCtx, call)
	cancel()
	if !result.Ok {
		e.mu.Lock()
		answered := p.Status == "answered"
		if !answered {
			p.Status, p.Deadline = "failed", nil
		}
		e.mu.Unlock()
		if !answered {
			return result
		}
	}
	for {
		e.mu.Lock()
		settleUserInputWait(p, time.Now())
		if p.Status != "waiting" {
			payload := map[string]any{"ok": true, "requestID": p.ID, "turnID": p.TurnID, "title": p.Title, "status": p.Status}
			raw, _ := json.Marshal(payload)
			e.mu.Unlock()
			return tool.Result{CallID: call.CallID, Name: call.Name, Ok: true, Content: string(raw)}
		}
		remaining := time.Until(*p.Deadline)
		e.mu.Unlock()
		timer := time.NewTimer(remaining)
		select {
		case <-ctx.Done():
		case <-p.wake:
		case <-timer.C:
		}
		timer.Stop()
	}
}

func settleUserInputWait(p *pendingUserInput, now time.Time) {
	if p.Status != "waiting" {
		return
	}
	if p.ctx.Err() != nil {
		p.Status, p.Deadline = "cancelled", nil
	} else if p.Deadline != nil && !now.Before(*p.Deadline) {
		p.Status, p.Deadline = "timeout", nil
	}
}

func (e *Engine) UserInputRequest(ctx context.Context, sessionID, requestID string) (*UserInputRequest, error) {
	var found *UserInputRequest
	e.mu.Lock()
	p := e.inputRequests[sessionID+":"+requestID]
	if p != nil {
		settleUserInputWait(p, time.Now())
		out := p.UserInputRequest
		if p.Status == "waiting" || p.Status == "answered" {
			e.mu.Unlock()
			return &out, nil
		}
		found = &out
	}
	e.mu.Unlock()
	// A reload/restart recovers from canonical messages, never UI storage.
	messages, err := e.store.ListMessages(ctx, sessionID, 0)
	if err != nil {
		return nil, err
	}
	answered := false
	calls := make(map[string]store.ContentPart)
	for _, message := range messages {
		if message.ClientMessageID == "input-flow-"+requestID {
			answered = true
		}
		for _, part := range message.Parts {
			if part.Type != store.ContentPartToolUse || part.Name != tool.RequestUserInput {
				continue
			}
			key := message.TurnID + ":" + part.CallID
			calls[key] = part
			if key != requestID || found != nil {
				continue
			}
			var args struct {
				Title string `json:"title"`
			}
			if json.Unmarshal(part.Args, &args) != nil {
				continue
			}
			found = &UserInputRequest{ID: requestID, SessionID: sessionID, TurnID: message.TurnID, Title: args.Title, Args: part.Args, Status: "cancelled"}
		}
		for _, part := range message.Parts {
			if part.Type != store.ContentPartToolResult || part.Name != tool.RequestUserInput {
				continue
			}
			var payload struct {
				RequestID string `json:"requestID"`
				Status    string `json:"status"`
			}
			if json.Unmarshal([]byte(part.Content), &payload) != nil || payload.RequestID != requestID {
				continue
			}
			if found == nil {
				call, ok := calls[message.TurnID+":"+part.CallID]
				if !ok {
					continue
				}
				var args struct {
					Title string `json:"title"`
				}
				if json.Unmarshal(call.Args, &args) != nil {
					continue
				}
				found = &UserInputRequest{ID: requestID, SessionID: sessionID, TurnID: message.TurnID, Title: args.Title, Args: call.Args, Status: "cancelled"}
			}
			if payload.Status != "" {
				found.Status = payload.Status
			}
		}
	}
	if found == nil {
		return nil, store.ErrNotFound
	}
	queued, err := e.store.ListQueuedInputs(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	for _, input := range queued {
		if input.ClientMessageID == "input-flow-"+requestID && input.Status != "cancelled" {
			answered = true
		}
	}
	if answered {
		found.Status = "answered"
	}
	return found, nil
}

func (e *Engine) ActOnUserInput(ctx context.Context, sessionID, requestID string, in UserInputAction) (*UserInputReply, error) {
	if in.Action != "dismiss" && in.Action != "answer" {
		return nil, fmt.Errorf("invalid input action")
	}
	if in.Action == "answer" {
		if strings.TrimSpace(in.Text) == "" || len(in.Parts) == 0 {
			return nil, ErrEmptyInput
		}
		for _, part := range in.Parts {
			if part.Type != store.ContentPartFormResult && part.Type != store.ContentPartText {
				return nil, ErrEmptyInput
			}
		}
		return e.answerUserInput(ctx, sessionID, requestID, in)
	}
	e.mu.Lock()
	p := e.inputRequests[sessionID+":"+requestID]
	if p != nil {
		settleUserInputWait(p, time.Now())
		if p.Status == "waiting" {
			// answer 已在上面分支返回，等待中的请求这里只可能是 dismiss。
			p.Status, p.Deadline = "dismissed", nil
			select {
			case p.wake <- struct{}{}:
			default:
			}
			out := &UserInputReply{Request: p.UserInputRequest}
			e.mu.Unlock()
			return out, nil
		}
		if p.Status == "answered" {
			out := &UserInputReply{Request: p.UserInputRequest}
			e.mu.Unlock()
			return out, nil
		}
	}
	e.mu.Unlock()
	req, err := e.UserInputRequest(ctx, sessionID, requestID)
	if err != nil {
		return nil, err
	}
	return &UserInputReply{Request: *req}, nil
}

func (e *Engine) answerUserInput(ctx context.Context, sessionID, requestID string, in UserInputAction) (*UserInputReply, error) {
	// All answers are canonical user messages. Serialize the snapshot and route
	// choice so concurrent retries cannot steer once, then submit again when the
	// original turn ends. No answer body is retained in the waiting/tool state.
	e.inputAnswerMu.Lock()
	defer e.inputAnswerMu.Unlock()
	req, err := e.UserInputRequest(ctx, sessionID, requestID)
	if err != nil {
		return nil, err
	}
	out := &UserInputReply{Request: *req}
	if req.Status == "answered" {
		return out, nil
	}
	// A synchronous answer uses the same steer path as a late answer. The turn
	// loop persists the tool result before applying steers and sampling again.
	// Never steer an unrelated running turn; Submit queues behind it instead.
	clientID := "input-flow-" + requestID
	steer, err := e.Steer(ctx, SteerInput{SessionID: sessionID, TurnID: req.TurnID, ClientMessageID: clientID, Text: in.Text, Parts: in.Parts})
	if err == nil {
		out.SubmitResult = &SubmitResult{TurnID: steer.TurnID, UserMessageID: steer.UserMessageID, Duplicate: steer.Duplicate, ClientMessageID: clientID}
	} else if errors.Is(err, ErrTurnNotActive) {
		out.SubmitResult, err = e.Submit(ctx, SubmitInput{SessionID: sessionID, ClientMessageID: clientID, Text: in.Text, Parts: in.Parts})
	}
	if err != nil {
		return nil, err
	}
	if out.Queued && out.Status == string(store.QueuedInputCancelled) {
		// The user explicitly reopened and submitted the question again. Reuse
		// the original queue identity with this answer, not a cancelled duplicate
		// that would be acknowledged but never delivered.
		queued := store.QueuedInputQueued
		parts := store.UserInputParts(in.Text, in.Parts)
		updated, err := e.store.UpdateQueuedInput(ctx, store.UpdateQueuedInputInput{
			SessionID: sessionID, ClientMessageID: clientID, Text: &in.Text, Parts: &parts,
			Status: &queued, RequeueCancelled: true,
		})
		if err != nil {
			return nil, err
		}
		e.hub.Publish(*updated.Event)
		out.Status, out.Duplicate = string(queued), false
		e.TryDrainQueued(sessionID)
	}
	out.Request.Status, out.Request.Deadline = "answered", nil
	if !out.Queued {
		e.mu.Lock()
		if p := e.inputRequests[sessionID+":"+requestID]; p != nil {
			// Wake only after the message is durably accepted. An RPC failure or
			// cancellation cannot then discard an acknowledged user answer.
			p.Status, p.Deadline = "answered", nil
			select {
			case p.wake <- struct{}{}:
			default:
			}
		}
		e.mu.Unlock()
	}
	return out, nil
}
