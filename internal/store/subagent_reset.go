package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
)

var (
	ErrSubagentResetBusy         = errors.New("subsession cannot be reset while work, approvals, unanswered questions, background processes or compaction are pending")
	ErrSubagentStateChanged      = errors.New("subsession state changed; list again and supply its current latest_turn_id as expected_turn_id")
	ErrSubagentResultUncollected = errors.New("subsession result has not been collected; wait for result collection before resetting")
	ErrSubagentResetConflict     = errors.New("subsession reset request identity was already used with different arguments")
)

// SubagentLifecycleStore is the transaction contract for a context boundary and
// an optional queued task. Implementations must never split reset and enqueue.
// SQLite implements it alongside Store; no second writable status store exists.
type SubagentLifecycleStore interface {
	ResetSubagent(context.Context, SubagentResetInput) (*SubagentResetResult, error)
	SubagentFreshContextCount(context.Context, string, string) (int, error)
}

type SubagentResetInput struct {
	ParentSessionID string
	ParentTurnID    string
	CallID          string
	SessionID       string
	ExpectedTurnID  string
	// Nil means reset only. Non-nil means reset, restore archive and queue
	// this input in the same transaction. Runtime admission is owned by Engine.
	Input *QueueInputInput
}

type SubagentResetResult struct {
	SessionID       string `json:"session_id"`
	ResetTurnID     string `json:"reset_turn_id"`
	ContextReset    bool   `json:"context_reset"`
	Queued          bool   `json:"queued"`
	ClientMessageID string `json:"client_message_id,omitempty"`
	Duplicate       bool   `json:"duplicate"`
}

// A reset is a maintenance checkpoint, not a user task or an LLM compaction.
// It uses the existing summary envelope with distinct, versioned metadata, so
// canonical history, pagination and reload all retain the same boundary.
// The checkpoint has no model execution and cannot be retried as a failed task.
type SubagentResetMetadata struct {
	Version               int    `json:"version"`
	ParentSessionID       string `json:"parent_session_id"`
	ParentTurnID          string `json:"parent_turn_id"`
	CallID                string `json:"call_id"`
	ExpectedTurnID        string `json:"expected_turn_id"`
	RequestHash           string `json:"request_hash"`
	QueuedClientMessageID string `json:"queued_client_message_id,omitempty"`
}

func SubagentResetIDs(parentID, turnID, callID string) (string, string) {
	raw, _ := json.Marshal([]string{parentID, turnID, callID})
	sum := sha256.Sum256(raw)
	key := hex.EncodeToString(sum[:])
	return "subagent_reset_turn_" + key, "subagent_reset_message_" + key
}

func NewSubagentResetMetadata(in SubagentResetInput) SubagentResetMetadata {
	text, action, clientID := "", "reset", ""
	if in.Input != nil {
		text, action, clientID = in.Input.Text, "send", in.Input.ClientMessageID
	}
	raw, _ := json.Marshal([]string{in.SessionID, in.ExpectedTurnID, action, text, clientID})
	sum := sha256.Sum256(raw)
	return SubagentResetMetadata{Version: 1, ParentSessionID: in.ParentSessionID,
		ParentTurnID: in.ParentTurnID, CallID: in.CallID, ExpectedTurnID: in.ExpectedTurnID,
		RequestHash: hex.EncodeToString(sum[:]), QueuedClientMessageID: clientID}
}

func (m SubagentResetMetadata) JSON() json.RawMessage {
	raw, _ := json.Marshal(map[string]SubagentResetMetadata{"subagent_reset": m})
	return raw
}

func SubagentResetFromMessage(msg *Message) (*SubagentResetMetadata, bool) {
	if msg == nil || msg.Role != RoleSummary || msg.Kind != MessageKindSummary {
		return nil, false
	}
	var envelope struct {
		Reset *SubagentResetMetadata `json:"subagent_reset"`
	}
	if json.Unmarshal(msg.Metadata, &envelope) != nil || envelope.Reset == nil {
		return nil, false
	}
	m := envelope.Reset
	if m.Version != 1 || m.ParentSessionID == "" || m.ParentTurnID == "" || m.CallID == "" || m.ExpectedTurnID == "" || m.RequestHash == "" {
		return nil, false
	}
	return m, true
}

// SubagentContextMessages excludes the latest reset checkpoint and everything
// before it. It does not mutate or delete the canonical input. Apply this before
// compaction-tail lookup AND before historical skill-reference reinjection.
func SubagentContextMessages(messages []*Message) []*Message {
	for i := len(messages) - 1; i >= 0; i-- {
		if _, ok := SubagentResetFromMessage(messages[i]); ok {
			return messages[i+1:]
		}
	}
	return messages
}

func ValidateSubagentReset(in SubagentResetInput) error {
	for _, value := range []string{in.ParentSessionID, in.ParentTurnID, in.CallID, in.SessionID, in.ExpectedTurnID} {
		if strings.TrimSpace(value) == "" {
			return ErrInvalidSessionRelation
		}
	}
	if in.Input != nil && (in.Input.SessionID != in.SessionID || strings.TrimSpace(in.Input.ClientMessageID) == "" || strings.TrimSpace(in.Input.Text) == "") {
		return ErrInvalidSessionRelation
	}
	return nil
}

func SubagentResetReplay(msg *Message, in SubagentResetInput) (*SubagentResetResult, error) {
	meta, ok := SubagentResetFromMessage(msg)
	want := NewSubagentResetMetadata(in)
	if !ok || msg.SessionID != in.SessionID || meta.ParentSessionID != in.ParentSessionID || meta.ParentTurnID != in.ParentTurnID || meta.CallID != in.CallID || meta.RequestHash != want.RequestHash {
		return nil, ErrSubagentResetConflict
	}
	return &SubagentResetResult{SessionID: msg.SessionID, ResetTurnID: msg.TurnID,
		ContextReset: true, Queued: meta.QueuedClientMessageID != "", ClientMessageID: meta.QueuedClientMessageID, Duplicate: true}, nil
}
