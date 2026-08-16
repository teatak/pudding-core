// Package memstore 是 store.Store 的内存实现,服务 M0 wiring 与 engine 单测;
// 持久化的 SQLite 实现由轨道 A 交付后在 main 中替换(docs/phase-1-plan.md 第 3 节)。
// 语义以 store 接口注释与 schema.sql 为准,两个实现必须可互换。
package memstore

import (
	"bytes"
	"context"
	"encoding/json"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/teatak/pudding-core/internal/event"
	"github.com/teatak/pudding-core/internal/searchtext"
	"github.com/teatak/pudding-core/internal/store"
)

type Memstore struct {
	mu               sync.Mutex
	sessions         map[string]*store.Session
	projects         map[string]*store.Project
	turns            map[string]*store.Turn
	fileChanges      map[string][]*store.TurnFileChange // turnID → root/path order
	fileChangeStates map[string]store.TurnFileChangeState
	messages         map[string][]*store.Message // sessionID → 时间升序
	queued           map[string][]*store.QueuedInput
	usage            map[usageKey]*store.UsageHourlyStat // (UTC hour unix ms, model) → global stats
	ucalibration     map[usageCalibrationKey]*store.UsageCalibrationStat
	susage           map[string]*store.SessionUsageStat        // sessionID → session stats
	canvas           map[string]*store.CanvasItem              // sessionID/itemID → session canvas item
	closed           map[string]*store.ClosedCanvasItem        // sessionID/id → recently closed canvas item
	savedCanvas      map[string]*store.SavedCanvasItem         // id → globally saved canvas item
	browser          map[string]map[string]*store.BrowserState // sessionID → tabID → browser state
	browserHistory   map[string]*store.BrowserHistoryEntry     // id → global browser history
	computerGrants   map[string]map[string]struct{}            // sessionID → approved app IDs
	events           map[string][]event.Event                  // sessionID → seq 升序
	seq              map[string]int64
	settings         map[string]string
	profiles         map[string]*store.ProviderProfile
}

func New() *Memstore {
	return &Memstore{
		sessions:         make(map[string]*store.Session),
		projects:         make(map[string]*store.Project),
		turns:            make(map[string]*store.Turn),
		fileChanges:      make(map[string][]*store.TurnFileChange),
		fileChangeStates: make(map[string]store.TurnFileChangeState),
		messages:         make(map[string][]*store.Message),
		queued:           make(map[string][]*store.QueuedInput),
		usage:            make(map[usageKey]*store.UsageHourlyStat),
		ucalibration:     make(map[usageCalibrationKey]*store.UsageCalibrationStat),
		susage:           make(map[string]*store.SessionUsageStat),
		canvas:           make(map[string]*store.CanvasItem),
		closed:           make(map[string]*store.ClosedCanvasItem),
		savedCanvas:      make(map[string]*store.SavedCanvasItem),
		browser:          make(map[string]map[string]*store.BrowserState),
		browserHistory:   make(map[string]*store.BrowserHistoryEntry),
		computerGrants:   make(map[string]map[string]struct{}),
		events:           make(map[string][]event.Event),
		seq:              make(map[string]int64),
		settings:         make(map[string]string),
		profiles:         make(map[string]*store.ProviderProfile),
	}
}

var _ store.Store = (*Memstore)(nil)

func (m *Memstore) CreateProject(_ context.Context, p *store.Project) error {
	if err := store.NormalizeProject(p); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	p.CreatedAt, p.UpdatedAt = now, now
	m.projects[p.ID] = cloneProject(p)
	return nil
}

func (m *Memstore) GetProject(_ context.Context, id string) (*store.Project, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.projects[id]
	if !ok {
		return nil, store.ErrNotFound
	}
	return m.projectWithActivityLocked(p), nil
}

func (m *Memstore) ListProjects(_ context.Context) ([]*store.Project, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]*store.Project, 0, len(m.projects))
	for _, p := range m.projects {
		out = append(out, m.projectWithActivityLocked(p))
	}
	sort.Slice(out, func(i, j int) bool {
		left, right := projectActivityAt(out[i]), projectActivityAt(out[j])
		if !left.Equal(right) {
			return left.After(right)
		}
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})
	return out, nil
}

func (m *Memstore) UpdateProject(_ context.Context, id string, upd store.ProjectUpdate) (*store.Project, error) {
	if err := store.NormalizeProjectUpdate(&upd); err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.projects[id]
	if !ok {
		return nil, store.ErrNotFound
	}
	if upd.Name != nil {
		p.Name = *upd.Name
	}
	if upd.RootDirs != nil {
		p.RootDirs = append([]string(nil), (*upd.RootDirs)...)
	}
	if upd.ApprovalMode != nil {
		p.ApprovalMode = *upd.ApprovalMode
	}
	p.UpdatedAt = time.Now()
	return m.projectWithActivityLocked(p), nil
}

func (m *Memstore) DeleteProject(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.projects[id]; !ok {
		return store.ErrNotFound
	}
	delete(m.projects, id)
	for _, session := range m.sessions {
		if session.ProjectID == id {
			session.ProjectID = ""
		}
	}
	return nil
}

func (m *Memstore) CreateSession(_ context.Context, s *store.Session) error {
	if err := store.NormalizeSessionProviderModel(s); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if s.ProjectID != "" {
		if _, ok := m.projects[s.ProjectID]; !ok {
			return store.ErrNotFound
		}
	}
	now := time.Now()
	s.CreatedAt, s.UpdatedAt, s.LastActivityAt = now, now, now
	s.ArchivedAt = nil
	m.sessions[s.ID] = cloneSession(s)
	return nil
}

func (m *Memstore) GetSession(_ context.Context, id string) (*store.Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[id]
	if !ok || s.ArchivedAt != nil {
		return nil, store.ErrNotFound
	}
	cp := cloneSession(s)
	cp.ActiveMode = store.NormalizeAgentMode(cp.ActiveMode)
	if cp.ActiveMode == "" {
		cp.ActiveMode = store.ModeChat
	}
	cp.Running = m.runningLocked(id)
	return cp, nil
}

func (m *Memstore) ListSessions(_ context.Context, options ...store.SessionListOptions) ([]*store.Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	resolved := store.ResolveSessionListOptions(options)
	if resolved.Scope != store.SessionListActive && resolved.Scope != store.SessionListArchived && resolved.Scope != store.SessionListAll {
		return nil, store.ErrInvalidSession
	}
	query := strings.ToLower(resolved.Query)
	out := make([]*store.Session, 0, len(m.sessions))
	for _, s := range m.sessions {
		if resolved.Scope == store.SessionListActive && s.ArchivedAt != nil {
			continue
		}
		if resolved.Scope == store.SessionListArchived && s.ArchivedAt == nil {
			continue
		}
		if query != "" {
			projectName := ""
			if project := m.projects[s.ProjectID]; project != nil {
				projectName = project.Name
			}
			if !strings.Contains(strings.ToLower(s.Title), query) && !strings.Contains(strings.ToLower(projectName), query) {
				continue
			}
		}
		cp := cloneSession(s)
		cp.ActiveMode = store.NormalizeAgentMode(cp.ActiveMode)
		if cp.ActiveMode == "" {
			cp.ActiveMode = store.ModeChat
		}
		cp.Running = m.runningLocked(s.ID)
		out = append(out, cp)
	}
	sort.Slice(out, func(i, j int) bool {
		if resolved.Scope == store.SessionListArchived && out[i].ArchivedAt != nil && out[j].ArchivedAt != nil && !out[i].ArchivedAt.Equal(*out[j].ArchivedAt) {
			return out[i].ArchivedAt.After(*out[j].ArchivedAt)
		}
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
	modelChanged := false
	if upd.Provider != nil {
		s.Provider = *upd.Provider
		modelChanged = true
	}
	if upd.Model != nil {
		s.Model = *upd.Model
		modelChanged = true
	}
	if modelChanged {
		s.ReasoningEffort = ""
		s.ReasoningModelKey = ""
	}
	if upd.ReasoningEffort != nil {
		s.ReasoningEffort = *upd.ReasoningEffort
		s.ReasoningModelKey = ""
		if s.ReasoningEffort != "" {
			s.ReasoningModelKey = sessionModelKey(s.Provider, s.Model)
		}
	}
	if upd.ActiveMode != nil {
		s.ActiveMode = *upd.ActiveMode
	}
	if upd.ModeLease != nil {
		s.ModeLease = *upd.ModeLease
	}
	if upd.ProjectID != nil {
		if *upd.ProjectID != "" {
			if _, ok := m.projects[*upd.ProjectID]; !ok {
				return nil, store.ErrNotFound
			}
		}
		s.ProjectID = *upd.ProjectID
	}
	if upd.LoadedAppIDs != nil {
		s.LoadedAppIDs = append([]string(nil), (*upd.LoadedAppIDs)...)
	}
	if upd.Pinned != nil {
		s.Pinned = *upd.Pinned
	}
	if upd.PinnedOrder != nil {
		s.PinnedOrder = *upd.PinnedOrder
	}
	s.UpdatedAt = time.Now()
	return cloneSession(s), nil
}

func sessionModelKey(providerName, model string) string {
	return strings.TrimSpace(providerName) + ":" + strings.TrimSpace(model)
}

func cloneProject(p *store.Project) *store.Project {
	if p == nil {
		return nil
	}
	cp := *p
	cp.RootDirs = append([]string(nil), p.RootDirs...)
	return &cp
}

func (m *Memstore) projectWithActivityLocked(project *store.Project) *store.Project {
	cloned := cloneProject(project)
	for _, session := range m.sessions {
		if session.ProjectID != project.ID || session.ArchivedAt != nil {
			continue
		}
		if cloned.LastActivityAt == nil || session.LastActivityAt.After(*cloned.LastActivityAt) {
			value := session.LastActivityAt
			cloned.LastActivityAt = &value
		}
	}
	return cloned
}

func projectActivityAt(project *store.Project) time.Time {
	if project.LastActivityAt != nil {
		return *project.LastActivityAt
	}
	return project.UpdatedAt
}

func cloneSession(s *store.Session) *store.Session {
	if s == nil {
		return nil
	}
	cp := *s
	cp.LoadedAppIDs = append([]string(nil), s.LoadedAppIDs...)
	if s.ArchivedAt != nil {
		archivedAt := *s.ArchivedAt
		cp.ArchivedAt = &archivedAt
	}
	return &cp
}

func (m *Memstore) ArchiveSession(_ context.Context, id string) (*store.Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	session, ok := m.sessions[id]
	if !ok || session.ArchivedAt != nil {
		return nil, store.ErrNotFound
	}
	now := time.Now()
	session.ArchivedAt = &now
	session.UpdatedAt = now
	for _, input := range m.queued[id] {
		if input.Status == store.QueuedInputQueued || input.Status == store.QueuedInputEditing {
			input.Status = store.QueuedInputCancelled
			input.UpdatedAt = now
		}
	}
	return cloneSession(session), nil
}

func (m *Memstore) RestoreSession(_ context.Context, id string) (*store.Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	session, ok := m.sessions[id]
	if !ok || session.ArchivedAt == nil {
		return nil, store.ErrNotFound
	}
	session.ArchivedAt = nil
	session.UpdatedAt = time.Now()
	return cloneSession(session), nil
}

func (m *Memstore) ListExpiredArchivedSessionIDs(_ context.Context, cutoff time.Time) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	ids := make([]string, 0)
	for id, session := range m.sessions {
		if session.ArchivedAt != nil && !session.ArchivedAt.After(cutoff) {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	return ids, nil
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
	delete(m.susage, id)
	delete(m.browser, id)
	delete(m.computerGrants, id)
	delete(m.events, id)
	delete(m.seq, id)
	for key, item := range m.canvas {
		if item.SessionID == id {
			delete(m.canvas, key)
		}
	}
	for key, item := range m.closed {
		if item.SessionID == id {
			delete(m.closed, key)
		}
	}
	for tid, t := range m.turns {
		if t.SessionID == id {
			delete(m.turns, tid)
			delete(m.fileChanges, tid)
			delete(m.fileChangeStates, tid)
		}
	}
	return nil
}

func (m *Memstore) HasComputerAppGrant(_ context.Context, sessionID, appID string) (bool, error) {
	sessionID = strings.TrimSpace(sessionID)
	appID = strings.TrimSpace(appID)
	if sessionID == "" || appID == "" {
		return false, store.ErrInvalidComputerAppGrant
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.sessions[sessionID]; !ok {
		return false, store.ErrNotFound
	}
	_, ok := m.computerGrants[sessionID][appID]
	return ok, nil
}

func (m *Memstore) GrantComputerApp(_ context.Context, sessionID, appID string) error {
	sessionID = strings.TrimSpace(sessionID)
	appID = strings.TrimSpace(appID)
	if sessionID == "" || appID == "" {
		return store.ErrInvalidComputerAppGrant
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.sessions[sessionID]; !ok {
		return store.ErrNotFound
	}
	grants := m.computerGrants[sessionID]
	if grants == nil {
		grants = make(map[string]struct{})
		m.computerGrants[sessionID] = grants
	}
	grants[appID] = struct{}{}
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
	mode := in.Mode
	if mode == "" {
		mode = store.ModeChat
	}
	mode = store.NormalizeAgentMode(mode)
	if mode == "" {
		mode = store.ModeChat
	}
	turn := &store.Turn{
		ID:              in.TurnID,
		SessionID:       in.SessionID,
		ClientMessageID: in.ClientMessageID,
		Status:          store.TurnRunning,
		Provider:        in.Provider,
		Model:           in.Model,
		Mode:            mode,
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
		Parts:           store.UserInputParts(in.UserText, in.UserParts),
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
		Text:            in.UserText,
	}
	m.turns[turn.ID] = turn
	m.messages[in.SessionID] = append(m.messages[in.SessionID], msg)
	m.appendEventLocked(in.SessionID, ev)
	m.sessions[in.SessionID].LastActivityAt = now

	ec := ev
	return &store.BeginTurnResult{Turn: cloneTurn(turn), UserMessage: cloneMessage(msg), StartedEvent: &ec}, nil
}

func (m *Memstore) BeginSystemTurn(_ context.Context, in store.BeginSystemTurnInput) (*store.BeginSystemTurnResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.sessions[in.SessionID]; !ok {
		return nil, store.ErrNotFound
	}
	for _, t := range m.turns {
		if t.SessionID == in.SessionID && t.ClientMessageID == in.ClientMessageID {
			return &store.BeginSystemTurnResult{
				Duplicate: true,
				Turn:      cloneTurn(t),
			}, nil
		}
	}
	for _, t := range m.turns {
		if t.SessionID == in.SessionID && t.Status == store.TurnRunning {
			return nil, store.ErrTurnRunning
		}
	}

	now := time.Now()
	mode := in.Mode
	if mode == "" {
		mode = store.ModeChat
	}
	mode = store.NormalizeAgentMode(mode)
	if mode == "" {
		mode = store.ModeChat
	}
	turn := &store.Turn{
		ID:              in.TurnID,
		SessionID:       in.SessionID,
		ClientMessageID: in.ClientMessageID,
		Status:          store.TurnRunning,
		Provider:        in.Provider,
		Model:           in.Model,
		Mode:            mode,
		ModelConfig:     append([]byte(nil), in.ModelConfig...),
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	msg := &store.Message{
		ID:        in.SystemMessageID,
		SessionID: in.SessionID,
		TurnID:    in.TurnID,
		Role:      store.RoleSystem,
		Kind:      store.MessageKindText,
		Text:      in.Text,
		Parts:     store.TextPart(in.Text),
		TurnIndex: 0,
		CreatedAt: now,
	}
	ev := event.Event{
		Seq:             m.nextSeq(in.SessionID),
		SessionID:       in.SessionID,
		Kind:            event.TurnStarted,
		TurnID:          in.TurnID,
		ClientMessageID: in.ClientMessageID,
	}
	m.turns[turn.ID] = turn
	m.messages[in.SessionID] = append(m.messages[in.SessionID], msg)
	m.appendEventLocked(in.SessionID, ev)
	m.sessions[in.SessionID].LastActivityAt = now

	ec := ev
	return &store.BeginSystemTurnResult{Turn: cloneTurn(turn), SystemMessage: cloneMessage(msg), StartedEvent: &ec}, nil
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
	mode := in.Mode
	if mode == "" {
		mode = store.ModeChat
	}
	mode = store.NormalizeAgentMode(mode)
	if mode == "" {
		mode = store.ModeChat
	}
	input := &store.QueuedInput{
		SessionID:       in.SessionID,
		ClientMessageID: in.ClientMessageID,
		Text:            in.Text,
		Parts:           store.UserInputParts(in.Text, in.Parts),
		Status:          store.QueuedInputQueued,
		Provider:        in.Provider,
		Model:           in.Model,
		Mode:            mode,
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
	input.Parts = store.ReplaceUserInputText(input.Parts, input.Text)
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

func (m *Memstore) SteerQueuedInput(_ context.Context, in store.SteerQueuedInputInput) (*store.SteerQueuedInputResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	turn, ok := m.turns[in.TurnID]
	if !ok || turn.SessionID != in.SessionID || turn.Status != store.TurnRunning {
		return nil, store.ErrNotFound
	}
	input := m.findQueuedInput(in.SessionID, in.ClientMessageID)
	if input == nil {
		return nil, store.ErrNotFound
	}
	for _, message := range m.messages[in.SessionID] {
		if message.Role != store.RoleUser || message.ClientMessageID != in.ClientMessageID {
			continue
		}
		if message.TurnID != in.TurnID {
			return nil, store.ErrNotFound
		}
		return &store.SteerQueuedInputResult{
			Duplicate:   true,
			Input:       cloneQueuedInput(input),
			UserMessage: cloneMessage(message),
		}, nil
	}
	if input.Status == store.QueuedInputEditing {
		return nil, store.ErrQueueBlocked
	}
	if input.Status != store.QueuedInputQueued {
		return nil, store.ErrNotFound
	}

	now := time.Now()
	maxIndex, _ := m.turnOutputStatsLocked(in.SessionID, in.TurnID)
	message := &store.Message{
		ID:              in.UserMessageID,
		SessionID:       in.SessionID,
		TurnID:          in.TurnID,
		Role:            store.RoleUser,
		Kind:            store.MessageKindText,
		Text:            store.TextFromParts(input.Parts),
		Parts:           store.UserInputParts(input.Text, input.Parts),
		TurnIndex:       maxIndex + 1,
		ClientMessageID: input.ClientMessageID,
		CreatedAt:       now,
	}
	input.Status = store.QueuedInputPromoted
	input.TurnID = in.TurnID
	input.UpdatedAt = now
	updatedEvent := event.Event{
		Seq:             m.nextSeq(in.SessionID),
		SessionID:       in.SessionID,
		Kind:            event.InputUpdated,
		ClientMessageID: input.ClientMessageID,
		Text:            input.Text,
		Status:          string(input.Status),
	}
	steeredEvent := event.Event{
		SessionID:       in.SessionID,
		Kind:            event.InputSteered,
		TurnID:          in.TurnID,
		ClientMessageID: input.ClientMessageID,
		UserMessageID:   message.ID,
		Text:            message.Text,
	}
	m.messages[in.SessionID] = append(m.messages[in.SessionID], message)
	turn.UpdatedAt = now
	m.sessions[in.SessionID].LastActivityAt = now
	m.appendEventLocked(in.SessionID, updatedEvent)
	updatedCopy := updatedEvent
	steeredCopy := steeredEvent
	return &store.SteerQueuedInputResult{
		Input:        cloneQueuedInput(input),
		UserMessage:  cloneMessage(message),
		UpdatedEvent: &updatedCopy,
		SteeredEvent: &steeredCopy,
	}, nil
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
				Mode:            input.Mode,
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
				Parts:           store.UserInputParts(input.Text, input.Parts),
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
				Text:            input.Text,
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
		if session := m.sessions[sessionID]; session == nil || session.ArchivedAt != nil {
			continue
		}
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
	if in.Mode != "" {
		mode := store.NormalizeAgentMode(in.Mode)
		if mode != "" {
			turn.Mode = mode
		}
	}
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
	maxIndex, firstOutputID := m.turnOutputStatsLocked(turn.SessionID, turn.ID)
	segments := store.FinishAssistantOutputSegments(in)
	if firstOutputID != "" && len(in.AssistantParts) == 0 {
		segments = nil
	}
	messages := m.appendTurnOutputSegmentsLocked(turn, maxIndex, segments, in.Interrupted, now)
	if firstOutputID != "" {
		ev.AssistantMessageID = firstOutputID
	}
	for i, msg := range messages {
		if ev.AssistantMessageID == "" && i == 0 {
			ev.AssistantMessageID = msg.ID
		}
		if i == 0 {
			res.AssistantMessage = cloneMessage(msg)
		}
		res.AssistantMessages = append(res.AssistantMessages, cloneMessage(msg))
	}
	for _, input := range in.FileChanges {
		rootPath := strings.TrimSpace(input.RootPath)
		path := strings.TrimSpace(input.Path)
		if rootPath == "" || path == "" || !validTurnFileChangeKind(input.Kind) {
			continue
		}
		m.fileChanges[turn.ID] = append(m.fileChanges[turn.ID], &store.TurnFileChange{
			ID: store.NewID("change"), SessionID: turn.SessionID, TurnID: turn.ID,
			RootPath: rootPath, Path: path, OriginalPath: strings.TrimSpace(input.OriginalPath), Kind: input.Kind,
			Origin:    store.NormalizeFileChangeOrigin(input.Origin),
			Additions: input.Additions, Deletions: input.Deletions, Binary: input.Binary, TooLarge: input.TooLarge,
			OldSize: input.OldSize, NewSize: input.NewSize, OldContent: input.OldContent, NewContent: input.NewContent,
			SnapshotVersion: input.SnapshotVersion, OldDigest: input.OldDigest, NewDigest: input.NewDigest,
			OldMode: input.OldMode, NewMode: input.NewMode, OldType: input.OldType, NewType: input.NewType,
			OldBinary: input.OldBinary, NewBinary: input.NewBinary,
			OldData: append([]byte(nil), input.OldData...), NewData: append([]byte(nil), input.NewData...),
			CreatedAt: now,
		})
	}
	if len(m.fileChanges[turn.ID]) > 0 {
		m.fileChangeStates[turn.ID] = store.TurnFileChangesApplied
		for _, change := range m.fileChanges[turn.ID] {
			change.Reversible = memTurnFileChangeReversible(change)
		}
		store.MarkUnsafeTurnFileChangeLayouts(m.fileChanges[turn.ID])
	}
	sort.Slice(m.fileChanges[turn.ID], func(i, j int) bool {
		left, right := m.fileChanges[turn.ID][i], m.fileChanges[turn.ID][j]
		if left.RootPath != right.RootPath {
			return left.RootPath < right.RootPath
		}
		if left.Path != right.Path {
			return left.Path < right.Path
		}
		return left.ID < right.ID
	})
	m.appendEventLocked(turn.SessionID, ev)
	m.sessions[turn.SessionID].LastActivityAt = now
	res.FinalEvent = &ev
	return res, nil
}

func validTurnFileChangeKind(kind store.FileChangeKind) bool {
	switch kind {
	case store.FileChangeAdded, store.FileChangeModified, store.FileChangeDeleted, store.FileChangeRenamed:
		return true
	default:
		return false
	}
}

func (m *Memstore) AppendTurnOutput(_ context.Context, in store.AppendTurnOutputInput) (*store.AppendTurnOutputResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	turn, ok := m.turns[in.TurnID]
	if !ok || turn.Status != store.TurnRunning {
		return nil, store.ErrNotFound
	}
	segments := store.AssistantOutputSegments(in.Parts)
	state := store.CloneProviderState(in.ProviderState)
	segments = store.EnsureProviderStateAssistantSegment(segments, state)
	if len(segments) == 0 && !store.ValidProviderState(state) {
		return &store.AppendTurnOutputResult{}, nil
	}
	now := time.Now()
	maxIndex, _ := m.turnOutputStatsLocked(turn.SessionID, turn.ID)
	messages := m.appendTurnOutputSegmentsLocked(turn, maxIndex, segments, in.Interrupted, now)
	if store.ValidProviderState(state) {
		target := lastAssistantMessageLocked(messages)
		if target == nil {
			target = m.latestAssistantMessageForTurnLocked(turn.SessionID, turn.ID)
		}
		if target != nil {
			target.ProviderState = state
		}
	}
	turn.UpdatedAt = now
	m.sessions[turn.SessionID].LastActivityAt = now
	out := make([]*store.Message, 0, len(messages))
	for _, msg := range messages {
		out = append(out, cloneMessage(msg))
	}
	return &store.AppendTurnOutputResult{Messages: out}, nil
}

func (m *Memstore) AppendTurnSteer(_ context.Context, in store.AppendTurnSteerInput) (*store.AppendTurnSteerResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	turn, ok := m.turns[in.TurnID]
	if !ok || turn.SessionID != in.SessionID || turn.Status != store.TurnRunning {
		return nil, store.ErrNotFound
	}
	for _, message := range m.messages[in.SessionID] {
		if message.Role != store.RoleUser || message.ClientMessageID != in.ClientMessageID {
			continue
		}
		if message.TurnID != in.TurnID {
			return nil, store.ErrNotFound
		}
		return &store.AppendTurnSteerResult{
			Duplicate:   true,
			UserMessage: cloneMessage(message),
		}, nil
	}
	now := time.Now()
	maxIndex, _ := m.turnOutputStatsLocked(in.SessionID, in.TurnID)
	parts := store.UserInputParts(in.UserText, in.UserParts)
	message := &store.Message{
		ID:              in.UserMessageID,
		SessionID:       in.SessionID,
		TurnID:          in.TurnID,
		Role:            store.RoleUser,
		Kind:            store.MessageKindText,
		Text:            store.TextFromParts(parts),
		Parts:           parts,
		TurnIndex:       maxIndex + 1,
		ClientMessageID: in.ClientMessageID,
		CreatedAt:       now,
	}
	ev := event.Event{
		SessionID:       in.SessionID,
		Kind:            event.InputSteered,
		TurnID:          in.TurnID,
		ClientMessageID: in.ClientMessageID,
		UserMessageID:   in.UserMessageID,
		Text:            message.Text,
	}
	m.messages[in.SessionID] = append(m.messages[in.SessionID], message)
	turn.UpdatedAt = now
	m.sessions[in.SessionID].LastActivityAt = now
	eventCopy := ev
	return &store.AppendTurnSteerResult{
		UserMessage: cloneMessage(message),
		Event:       &eventCopy,
	}, nil
}

func (m *Memstore) ApplyTurnSteers(_ context.Context, in store.ApplyTurnSteersInput) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(in.MessageIDs) != len(in.Events) {
		return store.ErrNotFound
	}
	turn, ok := m.turns[in.TurnID]
	if !ok || turn.Status != store.TurnRunning {
		return store.ErrNotFound
	}
	for _, ev := range in.Events {
		if ev == nil || ev.Kind != event.InputSteered || ev.SessionID != turn.SessionID || ev.TurnID != turn.ID {
			return store.ErrNotFound
		}
	}
	maxIndex, _ := m.turnOutputStatsLocked(turn.SessionID, turn.ID)
	now := time.Now()
	for _, message := range m.messages[turn.SessionID] {
		if message.TurnID == turn.ID && !message.CreatedAt.Before(now) {
			now = message.CreatedAt.Add(time.Nanosecond)
		}
	}
	for i, messageID := range in.MessageIDs {
		found := false
		for _, message := range m.messages[turn.SessionID] {
			if message.ID != messageID || message.TurnID != turn.ID || message.Role != store.RoleUser {
				continue
			}
			message.TurnIndex = maxIndex + i + 1
			message.CreatedAt = now
			found = true
			break
		}
		if !found {
			return store.ErrNotFound
		}
	}
	sort.SliceStable(m.messages[turn.SessionID], func(i, j int) bool {
		left, right := m.messages[turn.SessionID][i], m.messages[turn.SessionID][j]
		if left.CreatedAt.Equal(right.CreatedAt) {
			if left.TurnID == right.TurnID && left.TurnIndex != right.TurnIndex {
				return left.TurnIndex < right.TurnIndex
			}
			return false
		}
		return left.CreatedAt.Before(right.CreatedAt)
	})
	for _, ev := range in.Events {
		ev.Seq = m.nextSeq(turn.SessionID)
		m.appendEventLocked(turn.SessionID, *ev)
	}
	return nil
}

func (m *Memstore) AppendCompactSummary(_ context.Context, in store.AppendCompactSummaryInput) (*store.AppendCompactSummaryResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.sessions[in.SessionID]; !ok {
		return nil, store.ErrNotFound
	}
	if m.runningLocked(in.SessionID) {
		return nil, store.ErrTurnRunning
	}
	now := time.Now()
	mode := in.Mode
	if mode == "" {
		mode = store.ModeChat
	}
	mode = store.NormalizeAgentMode(mode)
	if mode == "" {
		mode = store.ModeChat
	}
	turn := &store.Turn{
		ID:              in.TurnID,
		SessionID:       in.SessionID,
		ClientMessageID: in.ClientMessageID,
		Status:          store.TurnCompleted,
		Provider:        in.Provider,
		Model:           in.Model,
		Mode:            mode,
		ModelConfig:     normalizeJSON(in.ModelConfig),
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	msg := &store.Message{
		ID:        in.MessageID,
		SessionID: in.SessionID,
		TurnID:    in.TurnID,
		Role:      store.RoleSummary,
		Kind:      store.MessageKindSummary,
		Text:      in.Text,
		Parts:     store.TextPart(in.Text),
		TurnIndex: 1,
		Metadata:  normalizeJSON(in.Metadata),
		CreatedAt: now,
	}
	ev := event.Event{
		Seq:                m.nextSeq(in.SessionID),
		SessionID:          in.SessionID,
		Kind:               event.TurnCompleted,
		TurnID:             in.TurnID,
		AssistantMessageID: in.MessageID,
	}
	m.turns[turn.ID] = turn
	m.messages[in.SessionID] = append(m.messages[in.SessionID], msg)
	m.appendEventLocked(in.SessionID, ev)
	m.sessions[in.SessionID].LastActivityAt = now
	ec := ev
	return &store.AppendCompactSummaryResult{Turn: cloneTurn(turn), Message: cloneMessage(msg), FinalEvent: &ec}, nil
}

func (m *Memstore) RecordUsage(_ context.Context, in store.UsageRecordInput) (*store.UsageHourlyStat, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	when := in.OccurredAt
	if when.IsZero() {
		when = time.Now()
	}
	hour := when.UTC().Truncate(time.Hour)
	key := usageKey{hourMS: unixMS(hour), model: in.Model}
	requestCount := in.RequestCount
	if requestCount <= 0 {
		requestCount = 1
	}
	stat := m.usage[key]
	if stat == nil {
		stat = &store.UsageHourlyStat{HourStartAt: hour, Model: in.Model}
		m.usage[key] = stat
	}
	stat.RequestCount += requestCount
	stat.InputUncachedTokens += clampNonNegative(in.InputUncachedTokens)
	stat.InputCachedTokens += clampNonNegative(in.InputCachedTokens)
	stat.CacheCreationTokens += clampNonNegative(in.CacheCreationTokens)
	stat.OutputContentTokens += clampNonNegative(in.OutputContentTokens)
	stat.OutputReasoningTokens += clampNonNegative(in.OutputReasoningTokens)
	stat.UpdatedAt = time.Now()
	return cloneUsageHourlyStat(stat), nil
}

func (m *Memstore) UsageHourlyStats(_ context.Context, from, to time.Time) ([]*store.UsageHourlyStat, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	from = from.UTC().Truncate(time.Hour)
	if !to.IsZero() {
		to = to.UTC().Truncate(time.Hour)
	}
	keys := make([]usageKey, 0, len(m.usage))
	fromMS := unixMS(from)
	toMS := int64(0)
	if !to.IsZero() {
		toMS = unixMS(to)
	}
	for key := range m.usage {
		if key.hourMS < fromMS {
			continue
		}
		if toMS > 0 && key.hourMS >= toMS {
			continue
		}
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].hourMS != keys[j].hourMS {
			return keys[i].hourMS < keys[j].hourMS
		}
		return keys[i].model < keys[j].model
	})
	out := make([]*store.UsageHourlyStat, 0, len(keys))
	for _, key := range keys {
		out = append(out, cloneUsageHourlyStat(m.usage[key]))
	}
	return out, nil
}

func (m *Memstore) RecordUsageCalibration(_ context.Context, providerName, model string, estimatedInputTokens, actualInputTokens int) (*store.UsageCalibrationStat, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	providerName = strings.TrimSpace(providerName)
	model = strings.TrimSpace(model)
	key := usageCalibrationKey{provider: providerName, model: model}
	stat := m.ucalibration[key]
	if stat == nil {
		stat = &store.UsageCalibrationStat{Provider: providerName, Model: model, InputRatioEWMA: 1}
		m.ucalibration[key] = stat
	}
	if estimatedInputTokens <= 0 || actualInputTokens <= 0 || providerName == "" || model == "" {
		return cloneUsageCalibrationStat(stat), nil
	}
	stat.InputRatioEWMA = store.NextUsageCalibrationRatio(
		stat.InputRatioEWMA,
		stat.SampleCount,
		estimatedInputTokens,
		actualInputTokens,
	)
	stat.SampleCount++
	stat.LastEstimatedInputTokens = estimatedInputTokens
	stat.LastActualInputTokens = actualInputTokens
	stat.UpdatedAt = time.Now()
	return cloneUsageCalibrationStat(stat), nil
}

func (m *Memstore) UsageCalibration(_ context.Context, providerName, model string) (*store.UsageCalibrationStat, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	providerName = strings.TrimSpace(providerName)
	model = strings.TrimSpace(model)
	stat := m.ucalibration[usageCalibrationKey{provider: providerName, model: model}]
	if stat == nil {
		return &store.UsageCalibrationStat{Provider: providerName, Model: model, InputRatioEWMA: 1}, nil
	}
	return cloneUsageCalibrationStat(stat), nil
}

func (m *Memstore) RecordSessionUsage(_ context.Context, sessionID string, in store.UsageRecordInput) (*store.SessionUsageStat, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.sessions[sessionID]; !ok {
		return nil, store.ErrNotFound
	}
	requestCount := in.RequestCount
	if requestCount <= 0 {
		requestCount = 1
	}
	inputUncached := clampNonNegative(in.InputUncachedTokens)
	inputCached := clampNonNegative(in.InputCachedTokens)
	cacheCreation := clampNonNegative(in.CacheCreationTokens)
	outputContent := clampNonNegative(in.OutputContentTokens)
	outputReasoning := clampNonNegative(in.OutputReasoningTokens)

	stat := m.susage[sessionID]
	if stat == nil {
		stat = &store.SessionUsageStat{SessionID: sessionID}
		m.susage[sessionID] = stat
	}
	stat.RequestCount += requestCount
	stat.LastInputUncachedTokens = inputUncached
	stat.LastInputCachedTokens = inputCached
	stat.LastCacheCreationTokens = cacheCreation
	stat.LastOutputContentTokens = outputContent
	stat.LastOutputReasoningTokens = outputReasoning
	stat.CumulativeInputUncachedTokens += inputUncached
	stat.CumulativeInputCachedTokens += inputCached
	stat.CumulativeCacheCreationTokens += cacheCreation
	stat.CumulativeOutputContentTokens += outputContent
	stat.CumulativeOutputReasoningTokens += outputReasoning
	stat.UpdatedAt = time.Now()
	return cloneSessionUsageStat(stat), nil
}

func (m *Memstore) SessionUsage(_ context.Context, sessionID string) (*store.SessionUsageStat, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.sessions[sessionID]; !ok {
		return nil, store.ErrNotFound
	}
	stat := m.susage[sessionID]
	if stat == nil {
		return &store.SessionUsageStat{SessionID: sessionID}, nil
	}
	return cloneSessionUsageStat(stat), nil
}

func (m *Memstore) ListCanvasItems(_ context.Context, actorSessionID string) ([]*store.CanvasItem, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.sessions[actorSessionID]; !ok {
		return nil, store.ErrNotFound
	}
	out := make([]*store.CanvasItem, 0, len(m.canvas))
	for _, item := range m.canvas {
		if item.SessionID == actorSessionID && item.Visible {
			out = append(out, cloneCanvasItem(item))
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].CreatedAt.Before(out[j].CreatedAt)
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}

func (m *Memstore) PutCanvasItem(_ context.Context, in store.CanvasItemInput) (*store.CanvasItem, error) {
	if err := store.NormalizeCanvasItemInput(&in); err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.sessions[in.ActorSessionID]; !ok {
		return nil, store.ErrNotFound
	}
	now := time.Now()
	sourceSessionID := in.SourceSessionID
	if sourceSessionID == "" {
		sourceSessionID = in.ActorSessionID
	}
	item := &store.CanvasItem{
		ID:                 in.ID,
		SessionID:          in.ActorSessionID,
		CanvasID:           in.CanvasID,
		SourceSessionID:    sourceSessionID,
		CreatedBySessionID: in.ActorSessionID,
		UpdatedBySessionID: in.ActorSessionID,
		Kind:               in.Kind,
		Title:              in.Title,
		Item:               append([]byte(nil), in.Item...),
		Window:             append([]byte(nil), in.Window...),
		SourceSavedItemID:  in.SourceSavedItemID,
		BaseSavedRevision:  in.BaseSavedRevision,
		Visible:            true,
		CreatedAt:          now,
		UpdatedAt:          now,
	}
	key := canvasMapKey(in.ActorSessionID, in.ID)
	if existing := m.canvas[key]; existing != nil {
		item.SourceSessionID = existing.SourceSessionID
		item.CreatedBySessionID = existing.CreatedBySessionID
		item.SourceSavedItemID = existing.SourceSavedItemID
		item.BaseSavedRevision = existing.BaseSavedRevision
		item.SavedDirty = existing.SavedDirty || (existing.SourceSavedItemID != "" && (existing.Kind != in.Kind || existing.Title != in.Title ||
			!bytes.Equal(existing.Item, in.Item) || !bytes.Equal(existing.Window, in.Window)))
		item.CreatedAt = existing.CreatedAt
	}
	m.canvas[key] = item
	return cloneCanvasItem(item), nil
}

func (m *Memstore) UpdateCanvasItemWindow(_ context.Context, patch store.CanvasItemWindowPatch) (*store.CanvasItem, error) {
	if err := store.NormalizeCanvasItemWindowPatch(&patch); err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.sessions[patch.ActorSessionID]; !ok {
		return nil, store.ErrNotFound
	}
	item := m.canvas[canvasMapKey(patch.ActorSessionID, patch.ItemID)]
	if item == nil || !item.Visible {
		return nil, store.ErrNotFound
	}
	item.Window = append([]byte(nil), patch.Window...)
	item.UpdatedBySessionID = patch.ActorSessionID
	if item.SourceSavedItemID != "" {
		item.SavedDirty = true
	}
	item.UpdatedAt = time.Now()
	return cloneCanvasItem(item), nil
}

func (m *Memstore) DeleteCanvasItem(_ context.Context, actorSessionID, itemID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.sessions[actorSessionID]; !ok {
		return store.ErrNotFound
	}
	key := canvasMapKey(actorSessionID, itemID)
	if _, ok := m.canvas[key]; !ok {
		return store.ErrNotFound
	}
	delete(m.canvas, key)
	return nil
}

func (m *Memstore) ListSavedCanvasItems(_ context.Context, actorSessionID string) ([]*store.SavedCanvasItem, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.sessions[actorSessionID]; !ok {
		return nil, store.ErrNotFound
	}
	out := make([]*store.SavedCanvasItem, 0, len(m.savedCanvas))
	for _, item := range m.savedCanvas {
		out = append(out, cloneSavedCanvasItem(item))
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].UpdatedAt.Equal(out[j].UpdatedAt) {
			return out[i].UpdatedAt.After(out[j].UpdatedAt)
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}

func (m *Memstore) SaveCanvasItem(_ context.Context, actorSessionID, itemID, savedItemID string) (*store.CanvasSaveResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.sessions[actorSessionID]; !ok {
		return nil, store.ErrNotFound
	}
	item := m.canvas[canvasMapKey(actorSessionID, itemID)]
	if item == nil {
		return nil, store.ErrNotFound
	}
	now := time.Now()
	targetID := item.SourceSavedItemID
	var saved *store.SavedCanvasItem
	if targetID == "" {
		if strings.TrimSpace(savedItemID) == "" {
			return nil, store.ErrInvalidCanvas
		}
		targetID = savedItemID
		if m.savedCanvas[targetID] != nil {
			return nil, store.ErrCanvasConflict
		}
		saved = &store.SavedCanvasItem{
			ID: targetID, SourceSessionID: actorSessionID, SourceItemID: item.ID,
			Kind: item.Kind, Title: item.Title, Item: append([]byte(nil), item.Item...), Window: append([]byte(nil), item.Window...),
			Revision: 1, CreatedAt: now, UpdatedAt: now,
		}
		m.savedCanvas[targetID] = saved
	} else {
		saved = m.savedCanvas[targetID]
		if saved == nil {
			return nil, store.ErrNotFound
		}
		if item.SavedDirty {
			if saved.Revision != item.BaseSavedRevision {
				return nil, store.ErrCanvasConflict
			}
			saved.Kind = item.Kind
			saved.Title = item.Title
			saved.Item = append([]byte(nil), item.Item...)
			saved.Window = append([]byte(nil), item.Window...)
			saved.Revision++
			saved.UpdatedAt = now
		}
	}
	item.SourceSavedItemID = targetID
	item.BaseSavedRevision = saved.Revision
	item.SavedDirty = false
	item.UpdatedAt = now
	return &store.CanvasSaveResult{Item: cloneCanvasItem(item), SavedItem: cloneSavedCanvasItem(saved)}, nil
}

func (m *Memstore) OpenSavedCanvasItem(_ context.Context, actorSessionID, savedItemID, itemID string) (*store.CanvasItem, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.sessions[actorSessionID]; !ok {
		return nil, store.ErrNotFound
	}
	saved := m.savedCanvas[savedItemID]
	if saved == nil {
		return nil, store.ErrNotFound
	}
	for _, item := range m.canvas {
		if item.SessionID == actorSessionID && item.SourceSavedItemID == savedItemID {
			return cloneCanvasItem(item), nil
		}
	}
	if strings.TrimSpace(itemID) == "" {
		return nil, store.ErrInvalidCanvas
	}
	now := time.Now()
	item := &store.CanvasItem{
		ID: itemID, SessionID: actorSessionID, CanvasID: store.DefaultCanvasID,
		SourceSessionID: actorSessionID, CreatedBySessionID: actorSessionID, UpdatedBySessionID: actorSessionID,
		Kind: saved.Kind, Title: saved.Title, Item: append([]byte(nil), saved.Item...), Window: append([]byte(nil), saved.Window...),
		SourceSavedItemID: saved.ID, BaseSavedRevision: saved.Revision, Visible: true, CreatedAt: now, UpdatedAt: now,
	}
	m.canvas[canvasMapKey(actorSessionID, itemID)] = item
	return cloneCanvasItem(item), nil
}

func (m *Memstore) DeleteSavedCanvasItem(_ context.Context, actorSessionID, savedItemID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.sessions[actorSessionID]; !ok {
		return store.ErrNotFound
	}
	if m.savedCanvas[savedItemID] == nil {
		return store.ErrNotFound
	}
	delete(m.savedCanvas, savedItemID)
	for _, item := range m.canvas {
		if item.SourceSavedItemID == savedItemID {
			item.SourceSavedItemID = ""
			item.BaseSavedRevision = 0
			item.SavedDirty = false
		}
	}
	return nil
}

func (m *Memstore) ListClosedCanvasItems(_ context.Context, actorSessionID string, limit int) ([]*store.ClosedCanvasItem, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.sessions[actorSessionID]; !ok {
		return nil, store.ErrNotFound
	}
	limit = normalizeClosedLimit(limit)
	out := make([]*store.ClosedCanvasItem, 0, len(m.closed))
	for _, item := range m.closed {
		if item.SessionID == actorSessionID {
			out = append(out, cloneClosedCanvasItem(item))
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].ClosedAt.Equal(out[j].ClosedAt) {
			return out[i].ClosedAt.After(out[j].ClosedAt)
		}
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (m *Memstore) PutClosedCanvasItem(_ context.Context, in store.ClosedCanvasItemInput, keepLimit int) (*store.ClosedCanvasItem, error) {
	if err := store.NormalizeClosedCanvasItemInput(&in); err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.sessions[in.ActorSessionID]; !ok {
		return nil, store.ErrNotFound
	}
	now := time.Now()
	created := now
	for key, existing := range m.closed {
		if existing.SessionID == in.ActorSessionID && existing.SourceItemID == in.SourceItemID {
			created = existing.CreatedAt
			delete(m.closed, key)
			break
		}
	}
	item := &store.ClosedCanvasItem{
		ID:             in.ID,
		SessionID:      in.ActorSessionID,
		SourceItemID:   in.SourceItemID,
		ActorSessionID: in.ActorSessionID,
		Kind:           in.Kind,
		Title:          in.Title,
		Item:           append([]byte(nil), in.Item...),
		Window:         append([]byte(nil), in.Window...),
		ClosedAt:       in.ClosedAt,
		CreatedAt:      created,
		UpdatedAt:      now,
	}
	m.closed[canvasMapKey(in.ActorSessionID, in.ID)] = item
	m.trimClosedCanvasItemsLocked(in.ActorSessionID, normalizeKeepLimit(keepLimit))
	return cloneClosedCanvasItem(item), nil
}

func (m *Memstore) DeleteClosedCanvasItem(_ context.Context, actorSessionID, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.sessions[actorSessionID]; !ok {
		return store.ErrNotFound
	}
	key := canvasMapKey(actorSessionID, id)
	if _, ok := m.closed[key]; !ok {
		return store.ErrNotFound
	}
	delete(m.closed, key)
	return nil
}

func (m *Memstore) ClearClosedCanvasItems(_ context.Context, actorSessionID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.sessions[actorSessionID]; !ok {
		return store.ErrNotFound
	}
	for key, item := range m.closed {
		if item.SessionID == actorSessionID {
			delete(m.closed, key)
		}
	}
	return nil
}

func (m *Memstore) GetBrowserState(_ context.Context, sessionID string) (*store.BrowserState, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.sessions[sessionID]; !ok {
		return nil, store.ErrNotFound
	}
	var latest *store.BrowserState
	for _, state := range m.browser[sessionID] {
		if latest == nil || state.UpdatedAt.After(latest.UpdatedAt) {
			latest = state
		}
	}
	if latest == nil {
		return nil, store.ErrNotFound
	}
	return cloneBrowserState(latest), nil
}

func (m *Memstore) GetBrowserTabState(_ context.Context, sessionID, tabID string) (*store.BrowserState, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.sessions[sessionID]; !ok {
		return nil, store.ErrNotFound
	}
	state := m.browser[sessionID][tabID]
	if state == nil {
		return nil, store.ErrNotFound
	}
	return cloneBrowserState(state), nil
}

func (m *Memstore) ListBrowserStates(_ context.Context, sessionID string) ([]*store.BrowserState, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.sessions[sessionID]; !ok {
		return nil, store.ErrNotFound
	}
	out := make([]*store.BrowserState, 0, len(m.browser[sessionID]))
	for _, state := range m.browser[sessionID] {
		out = append(out, cloneBrowserState(state))
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].UpdatedAt.Equal(out[j].UpdatedAt) {
			return out[i].UpdatedAt.After(out[j].UpdatedAt)
		}
		return out[i].CreatedAt.Before(out[j].CreatedAt)
	})
	return out, nil
}

func (m *Memstore) PutBrowserState(_ context.Context, in store.BrowserStateInput) (*store.BrowserState, error) {
	if err := store.NormalizeBrowserStateInput(&in); err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.sessions[in.SessionID]; !ok {
		return nil, store.ErrNotFound
	}
	now := time.Now()
	created := now
	if m.browser[in.SessionID] == nil {
		m.browser[in.SessionID] = make(map[string]*store.BrowserState)
	}
	if existing := m.browser[in.SessionID][in.TabID]; existing != nil {
		created = existing.CreatedAt
	}
	state := &store.BrowserState{
		SessionID:  in.SessionID,
		TabID:      in.TabID,
		URL:        in.URL,
		Title:      in.Title,
		FaviconURL: in.FaviconURL,
		Mode:       in.Mode,
		CreatedAt:  created,
		UpdatedAt:  now,
	}
	m.browser[in.SessionID][in.TabID] = state
	return cloneBrowserState(state), nil
}

func (m *Memstore) DeleteBrowserState(_ context.Context, sessionID, tabID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.sessions[sessionID]; !ok {
		return store.ErrNotFound
	}
	delete(m.browser[sessionID], tabID)
	if len(m.browser[sessionID]) == 0 {
		delete(m.browser, sessionID)
	}
	return nil
}

func (m *Memstore) ClearBrowserState(_ context.Context, sessionID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.sessions[sessionID]; !ok {
		return store.ErrNotFound
	}
	delete(m.browser, sessionID)
	return nil
}

func (m *Memstore) ListBrowserHistory(_ context.Context, query string, limit int) ([]*store.BrowserHistoryEntry, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	query = strings.ToLower(strings.TrimSpace(query))
	limit = store.NormalizeBrowserHistoryLimit(limit)
	out := make([]*store.BrowserHistoryEntry, 0, len(m.browserHistory))
	for _, entry := range m.browserHistory {
		if query != "" && !strings.Contains(strings.ToLower(entry.URL), query) && !strings.Contains(strings.ToLower(entry.Title), query) {
			continue
		}
		out = append(out, cloneBrowserHistoryEntry(entry))
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].VisitedAt.Equal(out[j].VisitedAt) {
			return out[i].VisitedAt.After(out[j].VisitedAt)
		}
		return out[i].ID > out[j].ID
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (m *Memstore) PutBrowserHistory(_ context.Context, in store.BrowserHistoryInput) (*store.BrowserHistoryEntry, error) {
	if err := store.NormalizeBrowserHistoryInput(&in); err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now().UTC()
	if m.browserHistory == nil {
		m.browserHistory = make(map[string]*store.BrowserHistoryEntry)
	}
	entry := &store.BrowserHistoryEntry{
		ID:         store.NewID("history"),
		URL:        in.URL,
		Title:      in.Title,
		FaviconURL: in.FaviconURL,
		VisitedAt:  in.VisitedAt,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	for _, existing := range m.browserHistory {
		if existing.URL != in.URL {
			continue
		}
		entry.ID = existing.ID
		entry.CreatedAt = existing.CreatedAt
		if entry.Title == "" {
			entry.Title = existing.Title
		}
		if entry.FaviconURL == "" {
			entry.FaviconURL = existing.FaviconURL
		}
		break
	}
	m.browserHistory[entry.ID] = entry
	m.trimBrowserHistoryLocked()
	return cloneBrowserHistoryEntry(entry), nil
}

func (m *Memstore) UpdateBrowserHistoryMetadata(_ context.Context, in store.BrowserHistoryInput) error {
	if err := store.NormalizeBrowserHistoryInput(&in); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, entry := range m.browserHistory {
		if entry.URL != in.URL {
			continue
		}
		if in.Title != "" {
			entry.Title = in.Title
		}
		if in.FaviconURL != "" {
			entry.FaviconURL = in.FaviconURL
		}
		entry.UpdatedAt = time.Now().UTC()
		break
	}
	return nil
}

func (m *Memstore) DeleteBrowserHistory(_ context.Context, historyID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	historyID = strings.TrimSpace(historyID)
	if historyID == "" {
		return store.ErrInvalidBrowserHistory
	}
	if _, ok := m.browserHistory[historyID]; !ok {
		return store.ErrNotFound
	}
	delete(m.browserHistory, historyID)
	return nil
}

func (m *Memstore) ClearBrowserHistory(_ context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.browserHistory = make(map[string]*store.BrowserHistoryEntry)
	return nil
}

func (m *Memstore) trimBrowserHistoryLocked() {
	if len(m.browserHistory) <= store.BrowserHistoryRetainLimit {
		return
	}
	entries := make([]*store.BrowserHistoryEntry, 0, len(m.browserHistory))
	for _, entry := range m.browserHistory {
		entries = append(entries, entry)
	}
	sort.Slice(entries, func(i, j int) bool {
		if !entries[i].VisitedAt.Equal(entries[j].VisitedAt) {
			return entries[i].VisitedAt.After(entries[j].VisitedAt)
		}
		return entries[i].ID > entries[j].ID
	})
	for _, entry := range entries[store.BrowserHistoryRetainLimit:] {
		delete(m.browserHistory, entry.ID)
	}
}

func (m *Memstore) trimClosedCanvasItemsLocked(sessionID string, limit int) {
	out := make([]*store.ClosedCanvasItem, 0, len(m.closed))
	for _, item := range m.closed {
		if item.SessionID == sessionID {
			out = append(out, item)
		}
	}
	if len(out) <= limit {
		return
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].ClosedAt.Equal(out[j].ClosedAt) {
			return out[i].ClosedAt.After(out[j].ClosedAt)
		}
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})
	for _, item := range out[limit:] {
		delete(m.closed, canvasMapKey(sessionID, item.ID))
	}
}

func canvasMapKey(sessionID, itemID string) string { return sessionID + "\x00" + itemID }

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

func (m *Memstore) GetMessage(_ context.Context, sessionID string, messageID string) (*store.Message, error) {
	sessionID = strings.TrimSpace(sessionID)
	messageID = strings.TrimSpace(messageID)
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.sessions[sessionID]; !ok {
		return nil, store.ErrNotFound
	}
	for _, msg := range m.messages[sessionID] {
		if msg.ID == messageID {
			return cloneMessage(msg), nil
		}
	}
	return nil, store.ErrNotFound
}

func (m *Memstore) SearchMessages(_ context.Context, in store.MessageSearchInput) ([]*store.Message, error) {
	sessionID := strings.TrimSpace(in.SessionID)
	query := strings.TrimSpace(in.Query)
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.sessions[sessionID]; !ok {
		return nil, store.ErrNotFound
	}
	if query == "" {
		return nil, nil
	}
	limit := in.Limit
	unlimited := in.Exact && in.NoLimit
	if !unlimited && limit <= 0 {
		limit = 20
	}
	if !unlimited && limit > 100 {
		limit = 100
	}
	needles := []string{strings.ToLower(query)}
	queryTerms := []string(nil)
	if in.Literal && !in.Exact {
		needles = strings.Fields(strings.ToLower(query))
		queryTerms = searchtext.QueryTerms(query)
	}
	out := make([]*store.Message, 0)
	for i := len(m.messages[sessionID]) - 1; i >= 0 && (unlimited || len(out) < limit); i-- {
		msg := m.messages[sessionID][i]
		if in.VisibleTranscriptOnly {
			visibleRole := msg.Role == store.RoleUser || msg.Role == store.RoleAssistant
			visibleKind := msg.Kind == store.MessageKindText || msg.Kind == store.MessageKindSummary
			if !visibleRole || !visibleKind {
				continue
			}
		}
		text := strings.ToLower(msg.Text)
		literalMatched := true
		for _, needle := range needles {
			if !strings.Contains(text, needle) {
				literalMatched = false
				break
			}
		}
		tokenMatched := false
		if in.Literal && !in.Exact && len(queryTerms) > 0 {
			indexedTerms := make(map[string]struct{})
			for _, term := range searchtext.Terms(msg.Text) {
				indexedTerms[term] = struct{}{}
			}
			tokenMatched = true
			for _, term := range queryTerms {
				if _, ok := indexedTerms[term]; !ok {
					tokenMatched = false
					break
				}
			}
		}
		if literalMatched || tokenMatched {
			out = append(out, cloneMessage(msg))
		}
	}
	return out, nil
}

func (m *Memstore) RemoveAttachmentsByOrigin(_ context.Context, sessionID, origin string) (*store.AttachmentCleanupResult, error) {
	sessionID = strings.TrimSpace(sessionID)
	origin = strings.TrimSpace(origin)
	out := &store.AttachmentCleanupResult{}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.sessions[sessionID]; !ok {
		return nil, store.ErrNotFound
	}
	if origin == "" {
		return out, nil
	}
	for _, msg := range m.messages[sessionID] {
		next, removed, changed := store.RemoveAttachmentPartsByOrigin(msg.Parts, origin)
		if !changed {
			continue
		}
		msg.Parts = next
		out.MessageCount++
		for _, item := range removed {
			out.Attachments = append(out.Attachments, store.AttachmentCleanupItem{SessionID: sessionID, Attachment: item})
		}
	}
	for _, input := range m.queued[sessionID] {
		next, removed, changed := store.RemoveAttachmentPartsByOrigin(input.Parts, origin)
		if !changed {
			continue
		}
		input.Parts = next
		out.QueuedInputCount++
		for _, item := range removed {
			out.Attachments = append(out.Attachments, store.AttachmentCleanupItem{SessionID: sessionID, Attachment: item})
		}
	}
	return out, nil
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
		out = append(out, conversationTurnFromMem(turn, m.messages[sessionID], m.fileChanges[turn.ID], m.fileChangeStates[turn.ID]))
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
	return conversationTurnFromMem(turn, m.messages[sessionID], m.fileChanges[turn.ID], m.fileChangeStates[turn.ID]), nil
}

func (m *Memstore) GetTurnFileChange(_ context.Context, sessionID string, turnID string, changeID string) (*store.TurnFileChange, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	turn, ok := m.turns[turnID]
	if !ok || turn.SessionID != sessionID {
		return nil, store.ErrNotFound
	}
	for _, change := range m.fileChanges[turnID] {
		if change.ID == changeID {
			return cloneTurnFileChange(change, true), nil
		}
	}
	return nil, store.ErrNotFound
}

func conversationTurnFromMem(turn *store.Turn, messages []*store.Message, fileChanges []*store.TurnFileChange, fileChangeState store.TurnFileChangeState) *store.ConversationTurn {
	out := &store.ConversationTurn{
		ID:              turn.ID,
		SessionID:       turn.SessionID,
		ClientMessageID: turn.ClientMessageID,
		Status:          turn.Status,
		Provider:        turn.Provider,
		Model:           turn.Model,
		Mode:            turn.Mode,
		Error:           turn.Error,
		CreatedAt:       turn.CreatedAt,
		UpdatedAt:       turn.UpdatedAt,
		Messages:        make([]*store.Message, 0, 2),
		FileChanges:     make([]*store.TurnFileChange, 0, len(fileChanges)),
		FileChangeState: fileChangeState,
	}
	for _, change := range fileChanges {
		out.FileChanges = append(out.FileChanges, cloneTurnFileChange(change, false))
	}
	for _, msg := range messages {
		if msg.TurnID != turn.ID || store.IsProtocolOnlyMessage(msg) {
			continue
		}
		out.Messages = append(out.Messages, cloneMessage(msg))
	}
	return out
}

func cloneTurnFileChange(change *store.TurnFileChange, withContent bool) *store.TurnFileChange {
	if change == nil {
		return nil
	}
	copy := *change
	copy.OldData = append([]byte(nil), change.OldData...)
	copy.NewData = append([]byte(nil), change.NewData...)
	if !withContent {
		copy.OldContent = ""
		copy.NewContent = ""
	}
	return &copy
}

func memTurnFileChangeReversible(change *store.TurnFileChange) bool {
	if change == nil || change.SnapshotVersion != 1 || change.TooLarge {
		return false
	}
	valid := func(kind, digest string) bool { return kind == "" || (kind == "file" && digest != "") }
	return valid(change.OldType, change.OldDigest) && valid(change.NewType, change.NewDigest)
}

func (m *Memstore) UpdateTurnFileChangeState(_ context.Context, sessionID, turnID string, expected, next store.TurnFileChangeState) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	turn, ok := m.turns[turnID]
	if !ok || turn.SessionID != sessionID {
		return store.ErrNotFound
	}
	if m.fileChangeStates[turnID] != expected {
		return store.ErrTurnFileChangeConflict
	}
	m.fileChangeStates[turnID] = next
	return nil
}

func (m *Memstore) turnOutputStatsLocked(sessionID, turnID string) (int, string) {
	maxIndex := 0
	firstIndex := 0
	firstID := ""
	for _, msg := range m.messages[sessionID] {
		if msg.TurnID != turnID || msg.TurnIndex <= 0 {
			continue
		}
		if msg.TurnIndex > maxIndex {
			maxIndex = msg.TurnIndex
		}
		if msg.Role == store.RoleUser || msg.Role == store.RoleSystem {
			continue
		}
		if firstID == "" || msg.TurnIndex < firstIndex {
			firstIndex = msg.TurnIndex
			firstID = msg.ID
		}
	}
	return maxIndex, firstID
}

func (m *Memstore) appendTurnOutputSegmentsLocked(turn *store.Turn, maxIndex int, segments []store.AssistantOutputSegment, interrupted bool, now time.Time) []*store.Message {
	out := make([]*store.Message, 0, len(segments))
	for i, segment := range segments {
		turnIndex := maxIndex + i + 1
		msg := &store.Message{
			ID:          store.NewID("msg"),
			SessionID:   turn.SessionID,
			TurnID:      turn.ID,
			Role:        segment.Role,
			Kind:        segment.Kind,
			Text:        segment.Text,
			Parts:       store.CloneContentParts(segment.Parts),
			TurnIndex:   turnIndex,
			Interrupted: interrupted && i == len(segments)-1,
			CreatedAt:   now,
		}
		m.messages[turn.SessionID] = append(m.messages[turn.SessionID], msg)
		out = append(out, msg)
	}
	return out
}

func lastAssistantMessageLocked(messages []*store.Message) *store.Message {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i] != nil && messages[i].Role == store.RoleAssistant {
			return messages[i]
		}
	}
	return nil
}

func (m *Memstore) latestAssistantMessageForTurnLocked(sessionID, turnID string) *store.Message {
	var latest *store.Message
	for _, msg := range m.messages[sessionID] {
		if msg == nil || msg.TurnID != turnID || msg.Role != store.RoleAssistant {
			continue
		}
		if latest == nil || msg.TurnIndex > latest.TurnIndex {
			latest = msg
		}
	}
	return latest
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
	cp.Metadata = append([]byte(nil), msg.Metadata...)
	cp.ProviderState = store.CloneProviderState(msg.ProviderState)
	return &cp
}

func cloneQueuedInput(input *store.QueuedInput) *store.QueuedInput {
	if input == nil {
		return nil
	}
	cp := *input
	cp.Parts = store.CloneContentParts(input.Parts)
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

func cloneUsageHourlyStat(stat *store.UsageHourlyStat) *store.UsageHourlyStat {
	if stat == nil {
		return nil
	}
	cp := *stat
	return &cp
}

type usageKey struct {
	hourMS int64
	model  string
}

type usageCalibrationKey struct {
	provider string
	model    string
}

func cloneUsageCalibrationStat(stat *store.UsageCalibrationStat) *store.UsageCalibrationStat {
	if stat == nil {
		return nil
	}
	cp := *stat
	return &cp
}

func cloneSessionUsageStat(stat *store.SessionUsageStat) *store.SessionUsageStat {
	if stat == nil {
		return nil
	}
	cp := *stat
	return &cp
}

func cloneCanvasItem(item *store.CanvasItem) *store.CanvasItem {
	if item == nil {
		return nil
	}
	cp := *item
	cp.Item = append([]byte(nil), item.Item...)
	cp.Window = append([]byte(nil), item.Window...)
	return &cp
}

func cloneClosedCanvasItem(item *store.ClosedCanvasItem) *store.ClosedCanvasItem {
	if item == nil {
		return nil
	}
	cp := *item
	cp.Item = append([]byte(nil), item.Item...)
	cp.Window = append([]byte(nil), item.Window...)
	return &cp
}

func cloneSavedCanvasItem(item *store.SavedCanvasItem) *store.SavedCanvasItem {
	if item == nil {
		return nil
	}
	cp := *item
	cp.Item = append([]byte(nil), item.Item...)
	cp.Window = append([]byte(nil), item.Window...)
	return &cp
}

func cloneBrowserState(state *store.BrowserState) *store.BrowserState {
	if state == nil {
		return nil
	}
	cp := *state
	return &cp
}

func cloneBrowserHistoryEntry(entry *store.BrowserHistoryEntry) *store.BrowserHistoryEntry {
	if entry == nil {
		return nil
	}
	cp := *entry
	return &cp
}

func normalizeClosedLimit(limit int) int {
	if limit <= 0 {
		return store.ClosedCanvasDefaultLimit
	}
	if limit > store.ClosedCanvasMaxLimit {
		return store.ClosedCanvasMaxLimit
	}
	return limit
}

func normalizeKeepLimit(limit int) int {
	if limit <= 0 {
		return store.ClosedCanvasKeepLimit
	}
	return limit
}

func unixMS(t time.Time) int64 { return t.UnixNano() / int64(time.Millisecond) }

func clampNonNegative(v int) int {
	if v < 0 {
		return 0
	}
	return v
}

func validQueuedInputStatus(status store.QueuedInputStatus) bool {
	switch status {
	case store.QueuedInputQueued, store.QueuedInputEditing, store.QueuedInputCancelled, store.QueuedInputPromoted:
		return true
	default:
		return false
	}
}

func normalizeJSON(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 || !json.Valid(raw) {
		return json.RawMessage(`{}`)
	}
	return append(json.RawMessage(nil), raw...)
}
