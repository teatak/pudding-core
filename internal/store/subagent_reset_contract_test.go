package store

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"
)

func resetContractInput() SubagentResetInput {
	return SubagentResetInput{
		ParentSessionID: "parent", ParentTurnID: "parent-turn", CallID: "call",
		SessionID: "child", ExpectedTurnID: "child-turn",
	}
}

func resetContractCheckpoint(in SubagentResetInput) *Message {
	turnID, messageID := SubagentResetIDs(in.ParentSessionID, in.ParentTurnID, in.CallID)
	return &Message{
		ID: messageID, SessionID: in.SessionID, TurnID: turnID,
		Role: RoleSummary, Kind: MessageKindSummary,
		Metadata: NewSubagentResetMetadata(in).JSON(),
	}
}

func TestSubagentResetContractIdentity(t *testing.T) {
	in := resetContractInput()
	turn, message := SubagentResetIDs(in.ParentSessionID, in.ParentTurnID, in.CallID)
	turn2, message2 := SubagentResetIDs(in.ParentSessionID, in.ParentTurnID, in.CallID)
	if turn != turn2 || message != message2 || turn == message {
		t.Fatal("reset identity must be stable and distinguish turn from message")
	}
	for _, ids := range [][3]string{{"other", in.ParentTurnID, in.CallID}, {in.ParentSessionID, "other", in.CallID}, {in.ParentSessionID, in.ParentTurnID, "other"}} {
		nextTurn, nextMessage := SubagentResetIDs(ids[0], ids[1], ids[2])
		if nextTurn == turn || nextMessage == message {
			t.Fatal("different parent, turn or call reused a reset identity")
		}
	}
	a, _ := SubagentResetIDs("a:b", "c", "d")
	b, _ := SubagentResetIDs("a", "b:c", "d")
	if a == b {
		t.Fatal("ambiguous concatenation produced the same identity")
	}
}

func TestSubagentResetContractLatestBoundaryPreservesHistory(t *testing.T) {
	first := resetContractCheckpoint(resetContractInput())
	secondInput := resetContractInput()
	secondInput.CallID = "second"
	second := resetContractCheckpoint(secondInput)
	messages := []*Message{
		{ID: "old-user", Role: RoleUser, Text: "old task"},
		{ID: "old-summary", Role: RoleSummary, Text: "old task summary"},
		first,
		{ID: "middle", Role: RoleAssistant, Text: "previous context"},
		second,
		{ID: "new-user", Role: RoleUser, Text: "independent new task"},
	}
	before, err := json.Marshal(messages)
	if err != nil {
		t.Fatal(err)
	}
	got := SubagentContextMessages(messages)
	if len(got) != 1 || got[0].ID != "new-user" {
		t.Fatalf("context crossed its latest reset boundary: %+v", got)
	}
	after, _ := json.Marshal(messages)
	if string(before) != string(after) {
		t.Fatal("reset projection rewrote canonical history")
	}
	if got := SubagentContextMessages(messages[:5]); len(got) != 0 {
		t.Fatal("reset without a new task must have an empty task context")
	}
	if got := SubagentContextMessages(messages[:2]); !reflect.DeepEqual(got, messages[:2]) {
		t.Fatal("ordinary history changed without a reset")
	}
	if got := SubagentContextMessages(nil); got != nil {
		t.Fatal("nil history must remain nil")
	}
}

func TestSubagentResetContractRejectsForgedOrIncompleteBoundaries(t *testing.T) {
	valid := resetContractCheckpoint(resetContractInput())
	if _, ok := SubagentResetFromMessage(valid); !ok {
		t.Fatal("valid reset checkpoint rejected")
	}
	cases := []*Message{nil, {Role: RoleUser, Kind: MessageKindSummary, Metadata: valid.Metadata},
		{Role: RoleSummary, Kind: MessageKindText, Metadata: valid.Metadata},
		{Role: RoleSummary, Kind: MessageKindSummary, Metadata: json.RawMessage(`not-json`)},
		{Role: RoleSummary, Kind: MessageKindSummary, Metadata: json.RawMessage(`{"subagent_reset":{"version":1}}`)}}
	for _, version := range []int{0, 2} {
		meta := NewSubagentResetMetadata(resetContractInput())
		meta.Version = version
		cases = append(cases, &Message{Role: RoleSummary, Kind: MessageKindSummary, Metadata: meta.JSON()})
	}
	for i, message := range cases {
		if _, ok := SubagentResetFromMessage(message); ok {
			t.Fatalf("invalid reset boundary %d was trusted", i)
		}
	}
}

func TestSubagentResetContractValidation(t *testing.T) {
	in := resetContractInput()
	if err := ValidateSubagentReset(in); err != nil {
		t.Fatal(err)
	}
	for _, clear := range []func(*SubagentResetInput){
		func(v *SubagentResetInput) { v.ParentSessionID = "" },
		func(v *SubagentResetInput) { v.ParentTurnID = " " },
		func(v *SubagentResetInput) { v.CallID = "" },
		func(v *SubagentResetInput) { v.SessionID = "" },
		func(v *SubagentResetInput) { v.ExpectedTurnID = "" },
	} {
		invalid := in
		clear(&invalid)
		if !errors.Is(ValidateSubagentReset(invalid), ErrInvalidSessionRelation) {
			t.Fatal("incomplete reset input was accepted")
		}
	}
	for _, input := range []*QueueInputInput{
		{SessionID: "other", ClientMessageID: "new", Text: "task"},
		{SessionID: in.SessionID, Text: "task"},
		{SessionID: in.SessionID, ClientMessageID: "new", Text: " "},
	} {
		invalid := in
		invalid.Input = input
		if !errors.Is(ValidateSubagentReset(invalid), ErrInvalidSessionRelation) {
			t.Fatal("invalid reset-and-send task was accepted")
		}
	}
}

func TestSubagentResetContractReplay(t *testing.T) {
	for _, send := range []bool{false, true} {
		in := resetContractInput()
		if send {
			in.Input = &QueueInputInput{SessionID: in.SessionID, ClientMessageID: "new-task", Text: "new independent task"}
		}
		checkpoint := resetContractCheckpoint(in)
		result, err := SubagentResetReplay(checkpoint, in)
		if err != nil || !result.Duplicate || !result.ContextReset || result.SessionID != in.SessionID || result.ResetTurnID != checkpoint.TurnID || result.Queued != send {
			t.Fatalf("reset replay changed its accepted outcome: %+v %v", result, err)
		}
		if send && result.ClientMessageID != in.Input.ClientMessageID {
			t.Fatal("reset replay lost its queued task identity")
		}
		for _, mutate := range []func(*SubagentResetInput){
			func(v *SubagentResetInput) { v.SessionID = "other" },
			func(v *SubagentResetInput) { v.ExpectedTurnID = "newer-turn" },
			func(v *SubagentResetInput) { v.CallID = "other-call" },
			func(v *SubagentResetInput) { v.ParentTurnID = "other-turn" },
			func(v *SubagentResetInput) { v.ParentSessionID = "other-parent" },
			func(v *SubagentResetInput) {
				v.Input = &QueueInputInput{SessionID: v.SessionID, ClientMessageID: "different", Text: "different task"}
			},
		} {
			changed := in
			mutate(&changed)
			if _, err := SubagentResetReplay(checkpoint, changed); !errors.Is(err, ErrSubagentResetConflict) {
				t.Fatalf("changed reset request accepted as a replay: %v", err)
			}
		}
	}
}

func TestSubagentSettledArchivePolicy(t *testing.T) {
	if SubagentTotalLimit != 24 {
		t.Fatalf("unexpected total subsession limit: %d", SubagentTotalLimit)
	}
	for _, status := range []string{"completed", "failed", "cancelled"} {
		if !SubagentSettledStatus(status) {
			t.Fatalf("settled state %q rejected", status)
		}
	}
	for _, status := range []string{"", "idle", "running", "queued", "awaiting_approval", "unknown"} {
		if SubagentSettledStatus(status) {
			t.Fatalf("non-terminal state %q accepted", status)
		}
	}
}
