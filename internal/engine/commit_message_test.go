package engine

import (
	"context"
	"testing"

	"github.com/teatak/pudding-core/internal/event"
	"github.com/teatak/pudding-core/internal/provider"
	"github.com/teatak/pudding-core/internal/provider/mock"
	"github.com/teatak/pudding-core/internal/provider/registry"
	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/store/storetest"
)

func TestCleanCommitMessage(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{
			input: "feat(git): add commit message generation",
			want:  "feat(git): add commit message generation",
		},
		{
			input: "\"feat(ui): update styles\"",
			want:  "feat(ui): update styles",
		},
		{
			input: "```\nfix(auth): fix token expiration\n```",
			want:  "fix(auth): fix token expiration",
		},
		{
			input: "```git\nfeat(core): support streaming\n\n- item 1\n- item 2\n```",
			want:  "feat(core): support streaming\n\n- item 1\n- item 2",
		},
	}

	for _, tc := range tests {
		got := cleanCommitMessage(tc.input)
		if got != tc.want {
			t.Errorf("cleanCommitMessage(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestGenerateCommitMessageWithMock(t *testing.T) {
	st := storetest.New(t)
	ctx := context.Background()
	if err := st.CreateSession(ctx, &store.Session{
		ID:       "s",
		Provider: "mock",
		Model:    "mock-model",
	}); err != nil {
		t.Fatal(err)
	}

	eng := New(
		st,
		event.NewHub(),
		registry.Static(mock.New(mock.WithChunks([]provider.Chunk{
			{Part: provider.PartThought, Delta: "Thinking about changes..."},
			{Part: provider.PartText, Delta: "feat(project): support commit generation"},
			{Done: true, Finish: provider.FinishStop},
		}))),
		st,
	)

	got, err := eng.GenerateCommitMessage(ctx, "s", "diff --git a/test.txt b/test.txt\n+hello", "en")
	if err != nil {
		t.Fatal(err)
	}
	want := "feat(project): support commit generation"
	if got != want {
		t.Fatalf("GenerateCommitMessage = %q, want %q", got, want)
	}
}
