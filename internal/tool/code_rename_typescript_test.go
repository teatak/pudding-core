package tool

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/teatak/pudding-core/internal/lsp"
)

func TestCodeRenameTypeScriptIdentifierContexts(t *testing.T) {
	for _, name := range []string{"target", "$value", "_value", "变量", "café", "a\u0301", "a\u200c", "class", "let"} {
		if !typeScriptIdentifierName(name) {
			t.Errorf("valid IdentifierName rejected: %q", name)
		}
	}
	for _, name := range []string{"", "1bad", "user-name", "a = 1", "a,b", "\u0301a", "has space", "a\\u0062"} {
		if typeScriptIdentifierName(name) {
			t.Errorf("invalid IdentifierName accepted: %q", name)
		}
	}
	text := "const obj = {'old': 1};"
	start := strings.Index(text, "old")
	edit := codeRenameTextEdit{Range: lsp.Range{Start: lsp.Position{Character: start}, End: lsp.Position{Character: start + 3}}}
	for _, name := range []string{"user-name", "1bad", "two words"} {
		edit.NewText = name
		if !safeQuotedTypeScriptRename(text, edit, name, "utf-16") {
			t.Errorf("safe key rejected: %q", name)
		}
	}
	for _, name := range []string{"a' + 'b", `a\u0062`, "${sideEffect()}", "`template`", "line\nend"} {
		edit.NewText = name
		if safeQuotedTypeScriptRename(text, edit, name, "utf-16") {
			t.Errorf("literal injection accepted: %q", name)
		}
	}
}

type renamePreviewService struct {
	fakeCodeLanguageService
	syncContext func(context.Context, lsp.Document) (lsp.DocumentState, error)
	aborted     bool
}

func (s *renamePreviewService) SyncDocument(ctx context.Context, _ lsp.ServerSpec, document lsp.Document) (lsp.DocumentState, error) {
	return s.syncContext(ctx, document)
}

func (s *renamePreviewService) Abort() { s.aborted = true }

func TestCodeRenameTypeScriptRestoresPreviewAfterCancellation(t *testing.T) {
	for _, restoreFails := range []bool{false, true} {
		t.Run(map[bool]string{false: "restored", true: "aborted"}[restoreFails], func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "main.ts")
			uri := codeFileURI(path)
			before := "export const old = 1\n"
			after := "export const next = 1\n"
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			current := before
			service := &renamePreviewService{}
			service.request = func(_ string, _ any, result any) error {
				return assignCodeTestResult(result, map[string]any{"success": true, "body": []any{}})
			}
			service.syncContext = func(syncCtx context.Context, document lsp.Document) (lsp.DocumentState, error) {
				if err := syncCtx.Err(); err != nil {
					t.Fatalf("restore reused canceled call context: %v", err)
				}
				if document.Text == addTypeScriptRenameCheckDirective(after) {
					current = document.Text
					cancel()
					return lsp.DocumentState{}, context.Canceled
				}
				if restoreFails && document.Text == before && ctx.Err() != nil {
					return lsp.DocumentState{}, errors.New("restore transport failed")
				}
				current = document.Text
				return lsp.DocumentState{}, nil
			}
			err := testCodeRunner(service).validateTypeScriptRename(ctx, resolvedCodeTarget{language: "typescript"}, "next", "utf-16", nil,
				map[string]codeRenameSnapshot{uri: {Path: path, Text: before}}, []patchFileArg{{Path: path, Content: &after}})
			if err == nil {
				t.Fatal("canceled validation succeeded")
			}
			if restoreFails && !service.aborted || !restoreFails && (service.aborted || current != before) {
				t.Fatalf("speculative overlay leaked: aborted=%v current=%q", service.aborted, current)
			}
		})
	}
}

func TestCodeRenameTypeScriptValidatesSemanticNameGrammar(t *testing.T) {
	for _, code := range []int{1100, 1214, 1215, 2368, 2398, 2414, 2452, 2457, 18006, 18012} {
		raw, _ := json.Marshal(code)
		if !typeScriptNameGrammarDiagnostic(raw) {
			t.Errorf("missed binding grammar code %d", code)
		}
	}
	for _, code := range []int{2304, 2322, 2339} {
		raw, _ := json.Marshal(code)
		if typeScriptNameGrammarDiagnostic(raw) {
			t.Errorf("ordinary type error blocks rename: %d", code)
		}
	}
}

func TestCodeRenameTypeScriptNeutralizesOnlyClassifiedComments(t *testing.T) {
	text := "const emoji = '😀';\n// @TS-NOCHECK\nconst literal = '@ts-ignore';\nconst template = `@ts-check`;\nconst regexp = /@ts-expect-error/;\nimport type { Item } from '@ts-nocheck/module';\n/** @ts-ignore */\nexport const count = 1;\n"
	spans := []int{}
	add := func(token string, kind int) {
		start := strings.Index(text, token)
		if start < 0 {
			t.Fatalf("missing fixture token %q", token)
		}
		spans = append(spans, codeUTF16TextLength(text[:start]), codeUTF16TextLength(token), kind)
	}
	add("// @TS-NOCHECK", 1)
	add("'@ts-ignore'", 6)
	add("`@ts-check`", 6)
	add("/@ts-expect-error/", 7)
	add("'@ts-nocheck/module'", 6)
	// JSDoc's @ punctuation and tag name have separate classifications.
	tagStart := strings.LastIndex(text, "@ts-ignore") + 1
	spans = append(spans, codeUTF16TextLength(text[:tagStart]), len("ts-ignore"), 18)
	got, err := neutralizeTypeScriptRenameDirectives(text, spans)
	if err != nil {
		t.Fatal(err)
	}
	want := strings.ReplaceAll(text, "// @TS-NOCHECK", "// _TS-NOCHECK")
	want = strings.ReplaceAll(want, "/** @ts-ignore */", "/** _ts-ignore */")
	if got != want {
		t.Fatalf("validation changed a non-comment token:\nwant %q\ngot  %q", want, got)
	}
	for _, malformed := range [][]int{{1}, {-1, 1, 1}, {0, codeUTF16TextLength(text) + 1, 1}, {codeUTF16TextLength(text[:strings.Index(text, "😀")]) + 1, 1, 1}} {
		if _, err := neutralizeTypeScriptRenameDirectives(text, malformed); err == nil {
			t.Fatalf("malformed classification accepted: %v", malformed)
		}
	}
}

func TestCodeRenameTypeScriptCheckDirectivePreservesBOMAndShebang(t *testing.T) {
	for _, prefix := range []string{"", "\ufeff", "#!/usr/bin/env node\n", "\ufeff#!/usr/bin/env node\n"} {
		text := prefix + "export const value = 1\n"
		if got, want := addTypeScriptRenameCheckDirective(text), prefix+"// @ts-check\nexport const value = 1\n"; got != want {
			t.Fatalf("check directive displaced file prefix: got %q, want %q", got, want)
		}
	}
}
