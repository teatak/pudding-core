package searchtext

import (
	"slices"
	"testing"
)

func TestTermsSupportsUnspacedMixedText(t *testing.T) {
	terms := QueryTerms("DeepSeek模型GPT4配置")
	for _, want := range []string{"deepseek", "模型", "gpt", "4", "配置"} {
		if !slices.Contains(terms, want) {
			t.Fatalf("Terms() = %v, missing %q", terms, want)
		}
	}
}

func TestQueryTermsRemovesDuplicates(t *testing.T) {
	terms := QueryTerms("法国 法国")
	count := 0
	for _, term := range terms {
		if term == "法国" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("QueryTerms() = %v, want one 法国 term", terms)
	}
}

func TestQueryTermsNormalizesLetterNumberBoundaries(t *testing.T) {
	plain := QueryTerms("GPT4模型")
	hyphenated := QueryTerms("GPT-4模型")
	if !slices.Equal(plain, hyphenated) {
		t.Fatalf("plain terms %v != hyphenated terms %v", plain, hyphenated)
	}
}

func TestHighlightTermsPrefersCompleteWords(t *testing.T) {
	terms := HighlightTerms("法国比赛")
	for _, want := range []string{"法国", "比赛"} {
		if !slices.Contains(terms, want) {
			t.Fatalf("HighlightTerms() = %v, missing %q", terms, want)
		}
	}
	for _, unwanted := range []string{"法", "国", "比", "赛"} {
		if slices.Contains(terms, unwanted) {
			t.Fatalf("HighlightTerms() = %v, contains redundant %q", terms, unwanted)
		}
	}
}
