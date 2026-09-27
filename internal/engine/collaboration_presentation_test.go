package engine

import (
	"testing"

	"github.com/teatak/pudding-core/internal/store"
)

func TestChildTextExcerpt(t *testing.T) {
	for _, tc := range []struct{ text, want string }{
		{"请查询成都今天的天气（当前实况 + 今天预报）。要求：使用 builtin_weather", "请查询成都今天的天气（当前实况 + 今天预报）"},
		{"## 结果\n```json\n{\"tool\":\"builtin_weather\"}\n```\n- **多云，26.5°C**。", "多云，26.5°C"},
		{"Use builtin_weather.\nSunny, 26°C. More details.", "Sunny, 26°C"},
		{"[All checks passed](https://example.test). Details.", "All checks passed"},
		{"```\nbuiltin_weather\n```", ""},
	} {
		if got := childTextExcerpt(tc.text, 96); got != tc.want {
			t.Errorf("excerpt %q = %q, want %q", tc.text, got, tc.want)
		}
	}
	if got := childTextExcerpt("杭州天气晴朗", 4); got != "杭州天气…" {
		t.Fatalf("unicode truncation: %q", got)
	}
	if childTaskInput(&store.Message{Role: store.RoleUser, Text: "已填写：方案 B", Parts: []store.ContentPart{{Type: store.ContentPartFormResult}}}) {
		t.Fatal("an answer must not rename the task")
	}
}
