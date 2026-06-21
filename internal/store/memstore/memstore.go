// Package memstore 是 store.Store 的内存实现,服务 M0 wiring 与 engine 单测;
// 持久化的 SQLite 实现由轨道 A 交付后在 main 中替换(docs/phase-1-plan.md 第 3 节)。
// 语义以 store 接口注释与 schema.sql 为准,两个实现必须可互换。
package memstore

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/teatak/pudding-core/internal/event"
	"github.com/teatak/pudding-core/internal/store"
)

type Memstore struct {
	mu       sync.Mutex
	sessions map[string]*store.Session
	turns    map[string]*store.Turn
	messages map[string][]*store.Message // sessionID → 时间升序
	queued   map[string][]*store.QueuedInput
	events   map[string][]event.Event // sessionID → seq 升序
	seq      map[string]int64
	settings map[string]string
	profiles map[string]*store.ProviderProfile
}

func New() *Memstore {
	return &Memstore{
		sessions: make(map[string]*store.Session),
		turns:    make(map[string]*store.Turn),
		messages: make(map[string][]*store.Message),
		queued:   make(map[string][]*store.QueuedInput),
		events:   make(map[string][]event.Event),
		seq:      make(map[string]int64),
		settings: make(map[string]string),
		profiles: make(map[string]*store.ProviderProfile),
	}
}

var _ store.Store = (*Memstore)(nil)

func (m *Memstore) CreateSession(_ context.Context, s *store.Session) error {
	if err := store.NormalizeSessionProviderModel(s); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	s.CreatedAt, s.UpdatedAt, s.LastActivityAt = now, now, now
	cp := *s
	m.sessions[s.ID] = &cp
	return nil
}

func (m *Memstore) GetSession(_ context.Context, id string) (*store.Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[id]
	if !ok {
		return nil, store.ErrNotFound
	}
	cp := *s
	cp.Running = m.runningLocked(id)
	return &cp, nil
}

func (m *Memstore) ListSessions(_ context.Context) ([]*store.Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]*store.Session, 0, len(m.sessions))
	for _, s := range m.sessions {
		cp := *s
		cp.Running = m.runningLocked(s.ID)
		out = append(out, &cp)
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].LastActivityAt.Equal(out[j].LastActivityAt) {
			return out[i].LastActivityAt.After(out[j].LastActivityAt)
		}
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})
	return out, nil
}

// runningLocked 派生 session 运行态;调用方必须已持锁。turns 是唯一事实源。
func (m *Memstore) runningLocked(sessionID string) bool {
	for _, t := range m.turns {
		if t.SessionID == sessionID && t.Status == store.TurnRunning {
			return true
		}
	}
	return false
}

func (m *Memstore) UpdateSession(_ context.Context, id string, upd store.SessionUpdate) (*store.Session, error) {
	if err := store.NormalizeSessionUpdate(&upd); err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[id]
	if !ok {
		return nil, store.ErrNotFound
	}
	if upd.Title != nil {
		s.Title = *upd.Title
	}
	if upd.Provider != nil {
		s.Provider = *upd.Provider
	}
	if upd.Model != nil {
		s.Model = *upd.Model
	}
	if upd.Pinned != nil {
		s.Pinned = *upd.Pinned
	}
	if upd.PinnedOrder != nil {
		s.PinnedOrder = *upd.PinnedOrder
	}
	s.UpdatedAt = time.Now()
	cp := *s
	return &cp, nil
}

func (m *Memstore) DeleteSession(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.sessions[id]; !ok {
		return store.ErrNotFound
	}
	delete(m.sessions, id)
	delete(m.messages, id)
	delete(m.queued, id)
	delete(m.events, id)
	delete(m.seq, id)
	for tid, t := range m.turns {
		if t.SessionID == id {
			delete(m.turns, tid)
		}
	}
	return nil
}

func (m *Memstore) BeginTurn(_ context.Context, in store.BeginTurnInput) (*store.BeginTurnResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.sessions[in.SessionID]; !ok {
		return nil, store.ErrNotFound
	}
	// 幂等优先于并发检查:重放同一 clientMessageID 永远拿到同一结果,
	// 即使该 turn 仍在 running。
	for _, t := range m.turns {
		if t.SessionID == in.SessionID && t.ClientMessageID == in.ClientMessageID {
			return &store.BeginTurnResult{
				Duplicate:   true,
				Turn:        cloneTurn(t),
				UserMessage: m.findUserMessage(in.SessionID, in.ClientMessageID),
			}, nil
		}
	}
	for _, t := range m.turns {
		if t.SessionID == in.SessionID && t.Status == store.TurnRunning {
			return nil, store.ErrTurnRunning
		}
	}

	now := time.Now()
	turn := &store.Turn{
		ID:              in.TurnID,
		SessionID:       in.SessionID,
		ClientMessageID: in.ClientMessageID,
		Status:          store.TurnRunning,
		Provider:        in.Provider,
		Model:           in.Model,
		ModelConfig:     append([]byte(nil), in.ModelConfig...),
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	msg := &store.Message{
		ID:              in.UserMessageID,
		SessionID:       in.SessionID,
		TurnID:          in.TurnID,
		Role:            store.RoleUser,
		Kind:            store.MessageKindText,
		Text:            in.UserText,
		Parts:           store.TextPart(in.UserText),
		TurnIndex:       0,
		ClientMessageID: in.ClientMessageID,
		CreatedAt:       now,
	}
	ev := event.Event{
		Seq:             m.nextSeq(in.SessionID),
		SessionID:       in.SessionID,
		Kind:            event.TurnStarted,
		TurnID:          in.TurnID,
		ClientMessageID: in.ClientMessageID,
		UserMessageID:   in.UserMessageID,
	}
	m.turns[turn.ID] = turn
	m.messages[in.SessionID] = append(m.messages[in.SessionID], msg)
	m.appendEventLocked(in.SessionID, ev)
	m.sessions[in.SessionID].LastActivityAt = now

	ec := ev
	return &store.BeginTurnResult{Turn: cloneTurn(turn), UserMessage: cloneMessage(msg), StartedEvent: &ec}, nil
}

func (m *Memstore) QueueInput(_ context.Context, in store.QueueInputInput) (*store.QueueInputResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.sessions[in.SessionID]; !ok {
		return nil, store.ErrNotFound
	}
	for _, t := range m.turns {
		if t.SessionID == in.SessionID && t.ClientMessageID == in.ClientMessageID {
			return &store.QueueInputResult{Duplicate: true, ExistingTurn: cloneTurn(t)}, nil
		}
	}
	for _, input := range m.queued[in.SessionID] {
		if input.ClientMessageID == in.ClientMessageID {
			return &store.QueueInputResult{Duplicate: true, Input: cloneQueuedInput(input)}, nil
		}
	}
	now := time.Now()
	input := &store.QueuedInput{
		SessionID:       in.SessionID,
		ClientMessageID: in.ClientMessageID,
		Text:            in.Text,
		Status:          store.QueuedInputQueued,
		Provider:        in.Provider,
		Model:           in.Model,
		ModelConfig:     append([]byte(nil), in.ModelConfig...),
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	ev := event.Event{
		Seq:             m.nextSeq(in.SessionID),
		SessionID:       in.SessionID,
		Kind:            event.InputQueued,
		ClientMessageID: input.ClientMessageID,
		Text:            input.Text,
		Status:          string(input.Status),
	}
	m.queued[in.SessionID] = append(m.queued[in.SessionID], input)
	m.appendEventLocked(in.SessionID, ev)
	m.sessions[in.SessionID].LastActivityAt = now
	ec := ev
	return &store.QueueInputResult{Input: cloneQueuedInput(input), QueuedEvent: &ec}, nil
}

func (m *Memstore) ListQueuedInputs(_ context.Context, sessionID string) ([]*store.QueuedInput, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.sessions[sessionID]; !ok {
		return nil, store.ErrNotFound
	}
	out := make([]*store.QueuedInput, 0)
	for _, input := range m.queued[sessionID] {
		if input.Status == store.QueuedInputQueued || input.Status == store.QueuedInputEditing {
			out = append(out, cloneQueuedInput(input))
		}
	}
	return out, nil
}

func (m *Memstore) HasQueuedInputs(_ context.Context, sessionID string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.sessions[sessionID]; !ok {
		return false, store.ErrNotFound
	}
	for _, input := range m.queued[sessionID] {
		if input.Status == store.QueuedInputQueued || input.Status == store.QueuedInputEditing {
			return true, nil
		}
	}
	return false, nil
}

func (m *Memstore) UpdateQueuedInput(_ context.Context, in store.UpdateQueuedInputInput) (*store.UpdateQueuedInputResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	input := m.findQueuedInput(in.SessionID, in.ClientMessageID)
	if input == nil || input.Status == store.QueuedInputPromoted {
		return nil, store.ErrNotFound
	}
	if in.Text != nil {
		input.Text = *in.Text
	}
	if in.Status != nil {
		input.Status = *in.Status
	}
	if !validQueuedInputStatus(input.Status) || input.Status == store.QueuedInputPromoted {
		return nil, store.ErrNotFound
	}
	input.UpdatedAt = time.Now()
	ev := event.Event{
		Seq:             m.nextSeq(in.SessionID),
		SessionID:       in.SessionID,
		Kind:            event.InputUpdated,
		ClientMessageID: input.ClientMessageID,
		Text:            input.Text,
		Status:          string(input.Status),
	}
	m.appendEventLocked(in.SessionID, ev)
	ec := ev
	return &store.UpdateQueuedInputResult{Input: cloneQueuedInput(input), Event: &ec}, nil
}

func (m *Memstore) PromoteNextQueuedInput(_ context.Context, in store.PromoteQueuedInputInput) (*store.PromoteQueuedInputResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.sessions[in.SessionID]; !ok {
		return nil, store.ErrNotFound
	}
	if m.runningLocked(in.SessionID) {
		return nil, store.ErrTurnRunning
	}
	for _, input := range m.queued[in.SessionID] {
		switch input.Status {
		case store.QueuedInputPromoted:
			continue
		case store.QueuedInputCancelled:
			input.Status = store.QueuedInputPromoted
			input.UpdatedAt = time.Now()
			continue
		case store.QueuedInputEditing:
			return nil, store.ErrQueueBlocked
		case store.QueuedInputQueued:
			now := time.Now()
			turn := &store.Turn{
				ID:              in.TurnID,
				SessionID:       input.SessionID,
				ClientMessageID: input.ClientMessageID,
				Status:          store.TurnRunning,
				Provider:        input.Provider,
				Model:           input.Model,
				ModelConfig:     append([]byte(nil), input.ModelConfig...),
				CreatedAt:       now,
				UpdatedAt:       now,
			}
			msg := &store.Message{
				ID:              in.UserMessageID,
				SessionID:       input.SessionID,
				TurnID:          turn.ID,
				Role:            store.RoleUser,
				Kind:            store.MessageKindText,
				Text:            input.Text,
				Parts:           store.TextPart(input.Text),
				TurnIndex:       0,
				ClientMessageID: input.ClientMessageID,
				CreatedAt:       now,
			}
			ev := event.Event{
				Seq:             m.nextSeq(input.SessionID),
				SessionID:       input.SessionID,
				Kind:            event.TurnStarted,
				TurnID:          turn.ID,
				ClientMessageID: input.ClientMessageID,
				UserMessageID:   msg.ID,
			}
			m.turns[turn.ID] = turn
			m.messages[input.SessionID] = append(m.messages[input.SessionID], msg)
			input.Status = store.QueuedInputPromoted
			input.TurnID = turn.ID
			input.UpdatedAt = now
			m.appendEventLocked(input.SessionID, ev)
			m.sessions[input.SessionID].LastActivityAt = now
			ec := ev
			return &store.PromoteQueuedInputResult{
				Input:        cloneQueuedInput(input),
				Turn:         cloneTurn(turn),
				UserMessage:  cloneMessage(msg),
				StartedEvent: &ec,
			}, nil
		}
	}
	return nil, store.ErrNotFound
}

func (m *Memstore) QueuedSessions(_ context.Context) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	seen := make(map[string]bool)
	for sessionID, inputs := range m.queued {
		for _, input := range inputs {
			if input.Status == store.QueuedInputQueued {
				seen[sessionID] = true
				break
			}
		}
	}
	out := make([]string, 0, len(seen))
	for sessionID := range seen {
		out = append(out, sessionID)
	}
	sort.Strings(out)
	return out, nil
}

func (m *Memstore) FinishTurn(_ context.Context, in store.FinishTurnInput) (*store.FinishTurnResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	turn, ok := m.turns[in.TurnID]
	if !ok || turn.Status != store.TurnRunning {
		return nil, store.ErrNotFound
	}
	now := time.Now()
	turn.Status = in.Status
	turn.Error = in.Error
	turn.UpdatedAt = now

	res := &store.FinishTurnResult{}
	ev := event.Event{
		Seq:         m.nextSeq(turn.SessionID),
		SessionID:   turn.SessionID,
		TurnID:      turn.ID,
		Interrupted: in.Interrupted,
		Error:       in.Error,
	}
	switch in.Status {
	case store.TurnCompleted:
		ev.Kind = event.TurnCompleted
	case store.TurnFailed:
		ev.Kind = event.TurnFailed
	case store.TurnCancelled:
		ev.Kind = event.TurnCancelled
	default:
		return nil, store.ErrNotFound
	}
	segments := store.FinishAssistantOutputSegments(in)
	for i, segment := range segments {
		msg := &store.Message{
			ID:          assistantMessageID(turn.ID, i),
			SessionID:   turn.SessionID,
			TurnID:      turn.ID,
			Role:        segment.Role,
			Kind:        segment.Kind,
			Text:        segment.Text,
			Parts:       store.CloneContentParts(segment.Parts),
			TurnIndex:   i + 1,
			Interrupted: in.Interrupted && i == len(segments)-1,
			CreatedAt:   now,
		}
		m.messages[turn.SessionID] = append(m.messages[turn.SessionID], msg)
		if i == 0 {
			ev.AssistantMessageID = msg.ID
			res.AssistantMessage = cloneMessage(msg)
		}
		res.AssistantMessages = append(res.AssistantMessages, cloneMessage(msg))
	}
	m.appendEventLocked(turn.SessionID, ev)
	m.sessions[turn.SessionID].LastActivityAt = now
	res.FinalEvent = &ev
	return res, nil
}

func assistantMessageID(turnID string, index int) string {
	if index == 0 {
		return "msg_" + turnID
	}
	return fmt.Sprintf("msg_%s_%03d", turnID, index+1)
}

// appendEventLocked 追加事件并按保留窗口滚动清理;调用方必须已持锁。
func (m *Memstore) appendEventLocked(sessionID string, ev event.Event) {
	evs := append(m.events[sessionID], ev)
	if len(evs) > store.EventsRetainPerSession {
		evs = evs[len(evs)-store.EventsRetainPerSession:]
	}
	m.events[sessionID] = evs
}

func (m *Memstore) RunningTurn(_ context.Context, sessionID string) (*store.Turn, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, t := range m.turns {
		if t.SessionID == sessionID && t.Status == store.TurnRunning {
			return cloneTurn(t), nil
		}
	}
	return nil, store.ErrNotFound
}

func (m *Memstore) RunningTurns(_ context.Context) ([]*store.Turn, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]*store.Turn, 0)
	for _, t := range m.turns {
		if t.Status == store.TurnRunning {
			out = append(out, cloneTurn(t))
		}
	}
	return out, nil
}

func (m *Memstore) ListMessages(_ context.Context, sessionID string, limit int) ([]*store.Message, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.sessions[sessionID]; !ok {
		return nil, store.ErrNotFound
	}
	msgs := m.messages[sessionID]
	if limit > 0 && len(msgs) > limit {
		msgs = msgs[len(msgs)-limit:]
	}
	out := make([]*store.Message, 0, len(msgs))
	for _, msg := range msgs {
		out = append(out, cloneMessage(msg))
	}
	return out, nil
}

func (m *Memstore) ListMessagesPage(_ context.Context, sessionID string, beforeMessageID string, limit int) (*store.MessagePage, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.sessions[sessionID]; !ok {
		return nil, store.ErrNotFound
	}
	msgs := m.messages[sessionID]
	end := len(msgs)
	if beforeMessageID != "" {
		end = -1
		for i, msg := range msgs {
			if msg.ID == beforeMessageID {
				end = i
				break
			}
		}
		if end < 0 {
			return nil, store.ErrNotFound
		}
	}
	start := 0
	hasMore := false
	if limit > 0 && end > limit {
		start = end - limit
		hasMore = start > 0
	}
	out := make([]*store.Message, 0, end-start)
	for _, msg := range msgs[start:end] {
		out = append(out, cloneMessage(msg))
	}
	return &store.MessagePage{Messages: out, HasMore: hasMore}, nil
}

func (m *Memstore) ListTurnsPage(_ context.Context, sessionID string, beforeTurnID string, limit int) (*store.TurnPage, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.sessions[sessionID]; !ok {
		return nil, store.ErrNotFound
	}
	turns := make([]*store.Turn, 0)
	for _, turn := range m.turns {
		if turn.SessionID == sessionID {
			turns = append(turns, turn)
		}
	}
	sort.Slice(turns, func(i, j int) bool {
		if turns[i].CreatedAt.Equal(turns[j].CreatedAt) {
			return turns[i].ID < turns[j].ID
		}
		return turns[i].CreatedAt.Before(turns[j].CreatedAt)
	})
	end := len(turns)
	if beforeTurnID != "" {
		end = -1
		for i, turn := range turns {
			if turn.ID == beforeTurnID {
				end = i
				break
			}
		}
		if end < 0 {
			return nil, store.ErrNotFound
		}
	}
	start := 0
	hasMore := false
	if limit > 0 && end > limit {
		start = end - limit
		hasMore = start > 0
	}
	out := make([]*store.ConversationTurn, 0, end-start)
	for _, turn := range turns[start:end] {
		out = append(out, conversationTurnFromMem(turn, m.messages[sessionID]))
	}
	return &store.TurnPage{Turns: out, HasMore: hasMore}, nil
}

func (m *Memstore) GetConversationTurn(_ context.Context, sessionID string, turnID string) (*store.ConversationTurn, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.sessions[sessionID]; !ok {
		return nil, store.ErrNotFound
	}
	turn, ok := m.turns[turnID]
	if !ok || turn.SessionID != sessionID {
		return nil, store.ErrNotFound
	}
	return conversationTurnFromMem(turn, m.messages[sessionID]), nil
}

func conversationTurnFromMem(turn *store.Turn, messages []*store.Message) *store.ConversationTurn {
	out := &store.ConversationTurn{
		ID:              turn.ID,
		SessionID:       turn.SessionID,
		ClientMessageID: turn.ClientMessageID,
		Status:          turn.Status,
		Error:           turn.Error,
		CreatedAt:       turn.CreatedAt,
		UpdatedAt:       turn.UpdatedAt,
		Messages:        make([]*store.Message, 0, 2),
	}
	for _, msg := range messages {
		if msg.TurnID != turn.ID {
			continue
		}
		out.Messages = append(out.Messages, cloneMessage(msg))
	}
	return out
}

func (m *Memstore) EventsAfter(_ context.Context, sessionID string, afterSeq int64, limit int) ([]event.Event, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.sessions[sessionID]; !ok {
		return nil, store.ErrNotFound
	}
	out := make([]event.Event, 0)
	for _, ev := range m.events[sessionID] {
		if ev.Seq > afterSeq {
			out = append(out, ev)
			if limit > 0 && len(out) >= limit {
				break
			}
		}
	}
	return out, nil
}

func (m *Memstore) LatestSeq(_ context.Context, sessionID string) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.sessions[sessionID]; !ok {
		return 0, store.ErrNotFound
	}
	return m.seq[sessionID], nil
}

func (m *Memstore) Settings(_ context.Context) (map[string]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make(map[string]string, len(m.settings))
	for k, v := range m.settings {
		out[k] = v
	}
	return out, nil
}

func (m *Memstore) SetSettings(_ context.Context, kv map[string]string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for k, v := range kv {
		m.settings[k] = v
	}
	return nil
}

func (m *Memstore) ListProviderProfiles(_ context.Context) ([]*store.ProviderProfile, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]*store.ProviderProfile, 0, len(m.profiles))
	for _, p := range m.profiles {
		cp := *p
		cp.Models = append([]store.ProviderModel(nil), p.Models...)
		out = append(out, &cp)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ProfileID() < out[j].ProfileID() })
	return out, nil
}

func (m *Memstore) GetProviderProfile(_ context.Context, name string) (*store.ProviderProfile, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.profiles[name]
	if !ok {
		return nil, store.ErrNotFound
	}
	cp := *p
	cp.Models = append([]store.ProviderModel(nil), p.Models...)
	return &cp, nil
}

func (m *Memstore) PutProviderProfile(_ context.Context, p *store.ProviderProfile) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	id := p.ProfileID()
	if id == "" {
		return store.ErrNotFound
	}
	now := time.Now()
	if existing, ok := m.profiles[id]; ok {
		p.CreatedAt = existing.CreatedAt
	} else {
		p.CreatedAt = now
	}
	p.ID = id
	p.UpdatedAt = now
	cp := *p
	cp.Models = append([]store.ProviderModel(nil), p.Models...)
	m.profiles[id] = &cp
	return nil
}

func (m *Memstore) DeleteProviderProfile(_ context.Context, name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.profiles[name]; !ok {
		return store.ErrNotFound
	}
	delete(m.profiles, name)
	return nil
}

func (m *Memstore) Close() error { return nil }

func (m *Memstore) nextSeq(sessionID string) int64 {
	m.seq[sessionID]++
	return m.seq[sessionID]
}

func (m *Memstore) findUserMessage(sessionID, clientMessageID string) *store.Message {
	for _, msg := range m.messages[sessionID] {
		if msg.Role == store.RoleUser && msg.ClientMessageID == clientMessageID {
			return cloneMessage(msg)
		}
	}
	return nil
}

func (m *Memstore) findQueuedInput(sessionID, clientMessageID string) *store.QueuedInput {
	for _, input := range m.queued[sessionID] {
		if input.ClientMessageID == clientMessageID {
			return input
		}
	}
	return nil
}

func cloneMessage(msg *store.Message) *store.Message {
	if msg == nil {
		return nil
	}
	cp := *msg
	cp.Parts = store.CloneContentParts(msg.Parts)
	return &cp
}

func cloneQueuedInput(input *store.QueuedInput) *store.QueuedInput {
	if input == nil {
		return nil
	}
	cp := *input
	cp.ModelConfig = append([]byte(nil), input.ModelConfig...)
	return &cp
}

func cloneTurn(t *store.Turn) *store.Turn {
	if t == nil {
		return nil
	}
	cp := *t
	cp.ModelConfig = append([]byte(nil), t.ModelConfig...)
	return &cp
}

func validQueuedInputStatus(status store.QueuedInputStatus) bool {
	switch status {
	case store.QueuedInputQueued, store.QueuedInputEditing, store.QueuedInputCancelled, store.QueuedInputPromoted:
		return true
	default:
		return false
	}
}
