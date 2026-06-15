package engine

import (
	"strings"
	"testing"

	"github.com/teatak/pudding-core/internal/event"
	"github.com/teatak/pudding-core/internal/provider"
	"github.com/teatak/pudding-core/internal/provider/mock"
	"github.com/teatak/pudding-core/internal/provider/registry"
	"github.com/teatak/pudding-core/internal/store/memstore"
)

func TestTitlerPromptConstrainsTitleLength(t *testing.T) {
	want := []string{
		"Chinese titles must be 2-8 Chinese characters",
		"English titles must be 2-5 words",
		"Output only the title",
	}
	for _, s := range want {
		if !strings.Contains(titlerSystemPrompt, s) {
			t.Fatalf("titler prompt missing %q:\n%s", s, titlerSystemPrompt)
		}
	}
}

func TestSanitizeTitle(t *testing.T) {
	tests := map[string]string{
		"“讲个古寺”":         "讲个古寺",
		"Resize Handle.": "Resize Handle",
	}
	for input, want := range tests {
		if got := sanitizeTitle(input); got != want {
			t.Fatalf("sanitizeTitle(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestGenerateTitleIgnoresThoughtChunks(t *testing.T) {
	eng := New(
		memstore.New(),
		event.NewHub(),
		registry.Static(mock.New(mock.WithChunks([]provider.Chunk{
			{Part: provider.PartThought, Delta: "We need to generate"},
			{Part: provider.PartText, Delta: "Greeting"},
			{Done: true, Finish: provider.FinishStop},
		}))),
		memstore.New(),
		"mock-model",
	)
	got, err := eng.generateTitle("default", "mock-model", provider.ModelConfig{}, "hi")
	if err != nil {
		t.Fatal(err)
	}
	if got != "Greeting" {
		t.Fatalf("generateTitle = %q, want Greeting", got)
	}
}

func TestProvisionalTitleFromText(t *testing.T) {
	tests := map[string]string{
		"你好啊，帮我看下这个问题。第二句不用": "你好啊，帮我看下这个问题",
		"“讲一个故事”":            "讲一个故事",
		"第一行\n第二行":           "第一行",
	}
	for input, want := range tests {
		if got := provisionalTitleFromText(input); got != want {
			t.Fatalf("provisionalTitleFromText(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestFinalTitleLimit(t *testing.T) {
	got := truncateRunes(
		sanitizeTitle("这是一个非常非常非常长的标题应该被截断"),
		finalTitleRunes,
	)
	if len([]rune(got)) > finalTitleRunes {
		t.Fatalf("final title too long: %q", got)
	}
}
