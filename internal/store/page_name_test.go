package store

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestPageAutomaticName(t *testing.T) {
	for _, tt := range []struct{ body, name string }{
		{"\ufeff# Heading", "Heading"}, {"\tIndented paragraph", "Indented paragraph"}, {"", ""}, {"\n\n", ""}, {"# **最后一夜**\n\n正文", "最后一夜"},
		{"Summary before heading.\n\n# [Real title](https://example.test)", "Real title"},
		{"Title\n=====\n\nBody", "Title"},
		{"第一段 **粗体** &amp; [链接][id]\n第二行\n\n下段\n\n[id]: https://example.test", "第一段 粗体 & 链接 第二行"},
		{"---\ntitle: Private YAML\n---\n\n```md\n# Fake\n```\n\nVisible", "Visible"},
		{"<!-- Hidden -->\n\n# <b>Visible</b> `&amp;` \\*text\\*", "Visible &amp; *text*"},
		{"- First item\n- Second item", "First item"},
		{"![Image label](assets/test.png)", "Image label"},
		{"```txt\nOnly code\n```", ""},
	} {
		if got := PageAutomaticName(tt.body); got != tt.name {
			t.Errorf("%q: got %q want %q", tt.body, got, tt.name)
		}
	}
	for _, body := range []string{strings.Repeat("中🙂", 100), "# " + strings.Repeat("中🙂", 100)} {
		name := PageAutomaticName(body)
		if !utf8.ValidString(name) || len(name) > 200 || !strings.HasSuffix(name, "…") {
			t.Fatal(name)
		}
	}
}
