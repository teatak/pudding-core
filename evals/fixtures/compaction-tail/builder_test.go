package compactiontail

import (
	"reflect"
	"testing"
)

func TestEffectiveMessagesUsesLatestSummary(t *testing.T) {
	messages := []Message{{"user", "old"}, {"summary", "first"}, {"user", "middle"}, {"summary", "latest"}, {"user", "tail"}}
	want := []Message{{"summary", "latest"}, {"user", "tail"}}
	got := EffectiveMessages(messages)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("EffectiveMessages = %v, want %v", got, want)
	}
	got[0].Text = "changed"
	if messages[3].Text != "latest" {
		t.Fatal("EffectiveMessages mutated the input backing array")
	}
}

func TestEffectiveMessagesWithoutSummaryReturnsCopy(t *testing.T) {
	messages := []Message{{"user", "hello"}}
	got := EffectiveMessages(messages)
	got[0].Text = "changed"
	if messages[0].Text != "hello" {
		t.Fatal("EffectiveMessages mutated the input backing array")
	}
}
