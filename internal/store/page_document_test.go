package store

import (
	"reflect"
	"strings"
	"testing"
)

func TestPageDocumentLosslessSource(t *testing.T) {
	for _, body := range []string{"", "\r\n\t\n", "# 标题🙂\r\n\r\n正文 **原文**\r\n", "---\ntitle: hello\n---\n\n正文\n", "```go\n\n```\n\nAfter", "```\n```\n", "- a\n  - nested\n\n- b\n\nAfter\n", "> quote\n>\n> second\n\nAfter", "| a | b |\n| - | - |\n| c | d |\n\nAfter", "[a]: /url\n\n[link][a]\n\n[a]: /two\n", "<div>\ncustom\n</div>\n", "Heading\n=======\n\n  literal\n\n---\n"} {
		d := LegacyPageDocument("test", body)
		if d.Markdown() != body {
			t.Fatalf("source changed: %q => %q", body, d.Markdown())
		}
		if !reflect.DeepEqual(d, LegacyPageDocument("test", body)) {
			t.Fatal("legacy IDs unstable")
		}
	}
	d := LegacyPageDocument("test", "# Heading\n\nFirst\n\n```\n```\n\nLast")
	if len(d.Blocks) != 4 || d.Blocks[2].Type != "code" {
		t.Fatalf("bad boundaries: %+v", d.Blocks)
	}
}
func TestPageDocumentStableIDsAndVersions(t *testing.T) {
	initial := LegacyPageDocument("test", "One\n\nTwo\n\nThree\n")
	changed, err := ReconcilePageDocument("One\n\nRewritten\n\nThree\n", initial, nil)
	if err != nil {
		t.Fatal(err)
	}
	for i, b := range initial.Blocks {
		if b.ID != changed.Blocks[i].ID {
			t.Fatal("edit changed identity")
		}
	}
	if changed.Blocks[0].Version != 1 || changed.Blocks[1].Version <= 1 || changed.Blocks[2].Version != 1 {
		t.Fatal("wrong versions")
	}
	moved, _ := ReconcilePageDocument("Three\n\nOne\n\nRewritten\n", changed, nil)
	if moved.Blocks[0].ID != initial.Blocks[2].ID || moved.Blocks[1].ID != initial.Blocks[0].ID {
		t.Fatal("move lost identity")
	}
	split, _ := ReconcilePageDocument("First half\n\nSecond half\n\nOne\n\nRewritten\n", moved, nil)
	if split.Blocks[0].ID != moved.Blocks[0].ID || split.Blocks[1].ID == moved.Blocks[0].ID {
		t.Fatal("split duplicated identity")
	}
	hints := split.Hints()
	hints[1].ID = hints[0].ID
	if _, err = ReconcilePageDocument(split.Markdown(), split, hints); err == nil {
		t.Fatal("duplicate hint accepted")
	}
	target := PageBlockTarget{BlockID: changed.Blocks[0].ID, Version: 1}
	if _, err = changed.ResolveTargets([]PageBlockTarget{target}); err != nil {
		t.Fatal("unrelated change conflicted", err)
	}
	target.BlockID = changed.Blocks[1].ID
	if _, err = changed.ResolveTargets([]PageBlockTarget{target}); err == nil {
		t.Fatal("stale block accepted")
	}
}
func TestPageBlockHintsDistinguishDuplicates(t *testing.T) {
	old := LegacyPageDocument("p", "Same\n\nSame\n\nEnd")
	body := "Changed\n\nSame\n\nEnd"
	hints := []PageBlockHint{{ID: old.Blocks[1].ID, From: 0, To: len("Changed\n\n")}, {ID: old.Blocks[0].ID, From: len("Changed\n\n"), To: len("Changed\n\nSame\n\n")}}
	next, err := ReconcilePageDocument(body, old, hints)
	if err != nil {
		t.Fatal(err)
	}
	if next.Blocks[0].ID != old.Blocks[1].ID || next.Blocks[1].ID != old.Blocks[0].ID {
		t.Fatal("mapped identities were ignored")
	}
	if strings.Contains(next.Markdown(), "block_") {
		t.Fatal("identity leaked into Markdown")
	}
}

func BenchmarkReconcilePageBlocks(b *testing.B) {
	body := strings.Repeat("A paragraph with **formatting**, 中文🙂 and a [link](./relative.md).\n\n", 1000)
	d := LegacyPageDocument("benchmark", body)
	changed := "Changed first paragraph.\n\n" + strings.Join(strings.Split(body, "\n\n")[1:], "\n\n")
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if _, err := ReconcilePageDocument(changed, d, nil); err != nil {
			b.Fatal(err)
		}
	}
}
